// Package service provides service management and HTTP APIs for the SemStreams platform.
package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"reflect"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/c360studio/semstreams/component"
	"github.com/c360studio/semstreams/component/flowgraph"
	"github.com/c360studio/semstreams/config"
	"github.com/c360studio/semstreams/internal/componentadmission"
	"github.com/c360studio/semstreams/internal/lifecyclejoin"
	"github.com/c360studio/semstreams/model"
	"github.com/c360studio/semstreams/natsclient"
	"github.com/c360studio/semstreams/payloadregistry"
	"github.com/c360studio/semstreams/pkg/errs"
	"github.com/c360studio/semstreams/pkg/lifecycle"
	"github.com/c360studio/semstreams/pkg/security"
	"github.com/c360studio/semstreams/storage/storeregistry"
	"github.com/c360studio/semstreams/types"
)

// ComponentManager handles lifecycle management of all components (inputs, processors, outputs)
// through the unified component system.
//
// ComponentManager follows lifecycle:
//
//	Initialize() - Create components but don't start them
//	Start(ctx)   - Start initialized components with context
//	Stop()       - Stop components in reverse order
type ComponentManager struct {
	*BaseService

	// Configuration
	config ComponentManagerConfig // Consistent config field

	// core component management
	registry         *component.Registry
	toolRegistry     component.ToolRegistryReader           // Shared tool registry plumbed via deps.ToolRegistry to managed components
	payloadRegistry  *payloadregistry.Registry              // Shared payload registry plumbed via deps.PayloadRegistry to managed components
	lifecycleManager *lifecycle.Manager                     // Shared Lifecycle harness Manager plumbed via deps.LifecycleManager (ADR-047). Nil when no app workflows are registered.
	componentConfigs config.ComponentConfigs                // Component configurations
	platform         types.PlatformMeta                     // Platform identity for components
	components       map[string]*component.ManagedComponent // Track managed components
	runtimes         map[string]*componentRuntime           // Private generation-scoped lifecycle authority
	startOrder       []string                               // Track start order for reverse stop

	// storeRegistry is the shared {StorageInstance → store} resolver (ADR-063),
	// populated from storage components' store-provide ports at Start and cleared
	// at Stop. Plumbed to every managed component via deps.StoreRegistry so
	// content-fetch consumers resolve refs against the same live handles the
	// storage components own. storeProvided tracks which instances each component
	// registered, so a stopping component is deregistered without re-reading its
	// (possibly already-closed) store; storeMu guards it.
	storeRegistry *storeregistry.Registry
	storeProvided map[string][]string
	storeMu       sync.Mutex

	// Config is desired-state authority for future boots. Runtime construction
	// below uses only componentConfigs, the snapshot captured by the constructor.
	natsClient        *natsclient.Client
	bootSecurity      security.Config
	bootModelRegistry model.RegistryReader

	// FlowGraph caching for thread-safe analysis
	graphCache flowGraphCache

	// Thread safety for component operations
	mu          sync.RWMutex
	initialized atomic.Bool
	initMu      sync.Mutex
	started     atomic.Bool
	startMu     sync.Mutex

	// Shutdown coordination for proper lifecycle
	shutdown chan struct{}
	done     chan struct{}

	stopMu            sync.Mutex
	stopping          bool
	borrows           sync.WaitGroup
	managerGeneration *lifecyclejoin.Generation
}

type componentRuntime struct {
	generation   *lifecyclejoin.Generation
	startDone    chan struct{}
	startInvoked bool
	startErr     error
	shutdownMode atomic.Uint32
}

type componentGenerationShutdownMode uint32

const (
	componentShutdownPending componentGenerationShutdownMode = iota
	componentShutdownCancelFirst
	componentShutdownGraceful
)

func (r *componentRuntime) selectCancelFirstShutdown() {
	if r == nil {
		return
	}
	r.shutdownMode.CompareAndSwap(uint32(componentShutdownPending), uint32(componentShutdownCancelFirst))
}

func (r *componentRuntime) admitGracefulShutdown() {
	if r == nil {
		return
	}
	r.shutdownMode.CompareAndSwap(uint32(componentShutdownPending), uint32(componentShutdownGraceful))
}

func (r *componentRuntime) selectShutdownModeForStop() componentGenerationShutdownMode {
	r.selectCancelFirstShutdown()
	return componentGenerationShutdownMode(r.shutdownMode.Load())
}

// ComponentManagerOption removed - we now use Dependencies pattern instead

// NewComponentManager creates a new ComponentManager using the standard constructor pattern
func NewComponentManager(rawConfig json.RawMessage, deps *Dependencies) (Service, error) {
	// Parse config - handle empty or invalid JSON properly
	var cfg ComponentManagerConfig
	if len(rawConfig) > 0 {
		if err := decodeStrictServiceJSON(rawConfig, &cfg); err != nil {
			return nil, fmt.Errorf("parse component-manager config: %w", err)
		}
	}

	// Apply defaults - clear and visible in constructor.
	if cfg.EnabledComponents == nil {
		cfg.EnabledComponents = []string{}
	}

	// Validate configuration
	if err := cfg.Validate(); err != nil {
		return nil, fmt.Errorf("validate component-manager config: %w", err)
	}

	if deps == nil || deps.Manager == nil {
		return nil, fmt.Errorf("component-manager requires config manager")
	}

	// Runtime construction reads the config manager's sealed boot authority once.
	var componentsConfig config.ComponentConfigs
	var bootSecurity security.Config
	var bootModelRegistry model.RegistryReader
	bootConfig := deps.Manager.BootConfig()
	if bootConfig == nil {
		return nil, fmt.Errorf("component-manager config manager has no sealed boot config")
	}
	componentsConfig = bootConfig.Components
	bootSecurity = bootConfig.Security
	if bootConfig.ModelRegistry != nil {
		bootModelRegistry = bootConfig.ModelRegistry
	}

	if componentsConfig == nil {
		componentsConfig = make(config.ComponentConfigs)
	}

	// Create base service
	var opts []Option
	if deps != nil {
		if deps.Logger != nil {
			opts = append(opts, WithLogger(deps.Logger))
		}
		if deps.MetricsRegistry != nil {
			opts = append(opts, WithMetrics(deps.MetricsRegistry))
		}
	}

	baseService := NewBaseServiceWithOptions("component-manager", nil, opts...) // Config is now service-specific

	// Get platform and registry from dependencies
	var platform types.PlatformMeta
	var registry *component.Registry
	var toolRegistry component.ToolRegistryReader
	var payloadRegistry *payloadregistry.Registry
	var lifecycleManager *lifecycle.Manager
	if deps != nil {
		platform = deps.Platform
		registry = deps.ComponentRegistry
		toolRegistry = deps.ToolRegistry
		payloadRegistry = deps.PayloadRegistry
		lifecycleManager = deps.LifecycleManager
	}

	// Fallback to creating a new registry if not provided
	if registry == nil {
		registry = component.NewRegistry()
	}

	cm := &ComponentManager{
		BaseService:       baseService,
		config:            cfg, // Store config as field
		registry:          registry,
		toolRegistry:      toolRegistry,
		payloadRegistry:   payloadRegistry,
		lifecycleManager:  lifecycleManager,
		componentConfigs:  componentsConfig,
		platform:          platform,
		components:        make(map[string]*component.ManagedComponent),
		runtimes:          make(map[string]*componentRuntime),
		startOrder:        make([]string, 0),
		storeRegistry:     storeregistry.New(),
		storeProvided:     make(map[string][]string),
		bootSecurity:      bootSecurity,
		bootModelRegistry: bootModelRegistry,
	}

	// Store NATS client if available
	if deps != nil && deps.NATSClient != nil {
		cm.natsClient = deps.NATSClient
	}

	// Set health check
	cm.SetHealthCheck(cm.healthCheck)

	// Initialize the component manager to create components
	// This follows the unified Pattern A lifecycle where creation is separate from starting
	if err := cm.Initialize(); err != nil {
		return nil, fmt.Errorf("initialize component manager: %w", err)
	}

	return cm, nil
}

// Initialize creates all configured components but does not start them
// This follows the unified Pattern A lifecycle where creation is separate from starting
func (cm *ComponentManager) Initialize() error {
	cm.initMu.Lock()
	defer cm.initMu.Unlock()

	if cm.initialized.Load() {
		cm.logger.Debug("ComponentManager.Initialize: Already initialized")
		return nil
	}

	if cm.componentConfigs == nil {
		cm.logger.Debug("ComponentManager.Initialize: No component configs, marking as initialized")
		cm.registry.SealComposition(componentadmission.Access{})
		cm.initialized.Store(true)
		return nil
	}

	cm.logger.Debug("ComponentManager.Initialize: Initializing with component configs",
		"count", len(cm.componentConfigs))

	// Reset component tracking
	if cm.components == nil {
		cm.components = make(map[string]*component.ManagedComponent)
	}
	cm.startOrder = make([]string, 0)

	// Create components from configuration
	if len(cm.componentConfigs) > 0 {
		cm.logger.Debug("ComponentManager.Initialize: Creating components from config",
			"count", len(cm.componentConfigs))

		type rulePackInitializationFailure struct {
			instance string
			err      error
		}
		var rulePackFailures []rulePackInitializationFailure

		// Iterate through component configs and create each one
		for instanceName, componentConfig := range cm.componentConfigs {
			// Skip disabled components
			if !componentConfig.Enabled {
				cm.logger.Debug("ComponentManager.Initialize: Skipping disabled component",
					"instance", instanceName)
				continue
			}

			// Build dependencies for the component
			deps := cm.buildComponentDependencies()

			// Create the component
			if err := cm.createComponent(instanceName, componentConfig, deps); err != nil {
				cm.logger.Error("Failed to create component from config",
					"instance", instanceName,
					"factory", componentConfig.Name,
					"type", componentConfig.Type,
					"error", err)
				// Generic components retain the established best-effort cold
				// boot posture. Enabled rule packs are different: dropping one
				// here would hide it from ProjectionBinders and let a partial
				// ownership composition bind. Collect every rule-pack failure,
				// finish the creation pass, then fail deterministically.
				if componentConfig.Name == "rule-processor" {
					rulePackFailures = append(rulePackFailures, rulePackInitializationFailure{
						instance: instanceName,
						err:      err,
					})
				}
				continue
			}

			cm.logger.Debug("Component created from config",
				"instance", instanceName,
				"factory", componentConfig.Name,
				"type", componentConfig.Type)
		}

		cm.logger.Debug("ComponentManager.Initialize: Finished creating components",
			"created", len(cm.components))
		if len(rulePackFailures) > 0 {
			sort.Slice(rulePackFailures, func(i, j int) bool {
				return rulePackFailures[i].instance < rulePackFailures[j].instance
			})
			failures := make([]error, 0, len(rulePackFailures))
			for _, failure := range rulePackFailures {
				failures = append(failures, fmt.Errorf(
					"rule processor component %q: %w",
					failure.instance,
					failure.err,
				))
			}
			return fmt.Errorf(
				"enabled rule-processor cold-boot initialization failed: %w",
				errors.Join(failures...),
			)
		}
	} else {
		cm.logger.Debug("ComponentManager.Initialize: No component configs to create")
	}

	cm.registry.SealComposition(componentadmission.Access{})
	cm.initialized.Store(true)
	return nil
}

// Start starts all initialized components with proper context flow-through.
//
// A failed boot is not retryable in-process: Start marks the manager started
// before returning the joined failure (so Stop tears down what did start), and
// a second Start call returns nil by design — the StateFailed truth lives in
// the health check, and the process is expected to exit on the boot error.
//
// The constructor-captured componentConfigs snapshot is the complete boot
// transaction. Desired writes committed after that snapshot wait for a fresh
// process; Start has no drain, reconcile, or post-boot component-start lane.
func (cm *ComponentManager) Start(ctx context.Context) error {
	if ctx == nil {
		return errs.WrapInvalid(errs.ErrInvalidData, "ComponentManager", "Start", "nil context")
	}
	cm.startMu.Lock()
	defer cm.startMu.Unlock()

	if !cm.initialized.Load() {
		return fmt.Errorf("component manager not initialized")
	}

	if cm.started.Load() {
		return nil
	}

	// Create shutdown channels for coordinated shutdown
	cm.shutdown = make(chan struct{})
	cm.done = make(chan struct{})
	supervisorCtx, supervisorCancel := context.WithCancel(ctx)
	supervisorDone := make(chan struct{})
	managerGeneration := lifecyclejoin.NewGeneration(supervisorCancel, func() { <-supervisorDone })
	cm.stopMu.Lock()
	cm.stopping = false
	cm.managerGeneration = managerGeneration
	cm.stopMu.Unlock()
	cm.startOrder = make([]string, 0)

	// Initialize NATS-backed capability discovery
	cm.initCapabilityDiscovery(ctx)

	// Start all components through the provider-first barriers. Providers launch
	// concurrently and register before the concurrent consumer phase begins;
	// each phase joins all of its failures. Mark started even on failure so a
	// subsequent Stop tears down the components that DID start, then fail boot
	// closed — the composition root must not proceed to post-start setup on a
	// partially failed component set.
	startErr := cm.startAllComponents(supervisorCtx)

	cm.started.Store(true)

	if startErr != nil {
		close(supervisorDone)
		return fmt.Errorf("start components: %w", startErr)
	}

	cm.stopMu.Lock()
	if cm.stopping {
		cm.stopMu.Unlock()
		close(supervisorDone)
		return fmt.Errorf("component manager stopping before supervisor launch")
	}
	cm.stopMu.Unlock()
	go cm.supervise(supervisorCtx, supervisorDone)

	// Start the base service after components are started to avoid health check deadlocks
	if err := cm.BaseService.Start(ctx); err != nil {
		return fmt.Errorf("failed to start base service: %w", err)
	}

	return nil
}

func (cm *ComponentManager) supervise(ctx context.Context, done chan<- struct{}) {
	defer close(done)
	cm.publishHealthLoop(ctx)
}

// initCapabilityDiscovery initializes NATS-backed capability discovery if available.
func (cm *ComponentManager) initCapabilityDiscovery(ctx context.Context) {
	if cm.natsClient == nil {
		return
	}

	nodeID := fmt.Sprintf("%s.%s", cm.platform.Org, cm.platform.Platform)
	if nodeID == "." {
		nodeID = "default-node"
	}
	if err := cm.registry.InitNATS(ctx, cm.natsClient, nodeID); err != nil {
		cm.logger.Warn("Failed to initialize capability discovery, continuing without it",
			"error", err)
		return
	}
	cm.logger.Info("Capability discovery initialized", "node_id", nodeID)
	cm.registry.StartHeartbeat(ctx, 30*time.Second)
}

// componentToStart holds component info for the parallel launch batch.
type componentToStart struct {
	name    string
	runtime *componentRuntime
	start   func() error
}

// startAllComponents starts all lifecycle components in the provider-first
// component-start barriers required by the framework-composition spec.
func (cm *ComponentManager) startAllComponents(ctx context.Context) error {
	cm.mu.RLock()
	names := make([]string, 0, len(cm.components))
	for name := range cm.components {
		names = append(names, name)
	}
	cm.mu.RUnlock()
	return cm.startComponentsBarrier(ctx, names)
}

// startComponentsBarrier starts the named components with barrier semantics:
// existing StoreProvider components first, then all remaining consumers. Each
// phase launches concurrently and completes before the next begins. It is the
// shared core of the cold-boot batch AND the boot-boundary config drain, so
// drain-created components get the same fail-closed ordering.
func (cm *ComponentManager) startComponentsBarrier(ctx context.Context, names []string) error {
	cm.mu.RLock()
	providers := make([]string, 0, len(names))
	consumers := make([]string, 0, len(names))
	for _, name := range names {
		mc, exists := cm.components[name]
		if !exists {
			continue
		}
		if _, ok := mc.Component.(component.StoreProvider); ok {
			providers = append(providers, name)
		} else {
			consumers = append(consumers, name)
		}
	}
	cm.mu.RUnlock()

	if err := cm.startComponentsPhase(ctx, providers); err != nil {
		return err
	}
	return cm.startComponentsPhase(ctx, consumers)
}

// startComponentsPhase starts one parallel launch batch, returns only after
// every launched Start has returned, and joins every component-named failure.
// The WaitGroup is deliberately scoped here rather than reusing cm.wg, which
// tracks long-lived loops that outlive a launch batch.
func (cm *ComponentManager) startComponentsPhase(ctx context.Context, names []string) error {
	cm.mu.Lock()
	componentsToStart := make([]componentToStart, 0, len(names))
	for _, name := range names {
		mc, exists := cm.components[name]
		if !exists {
			continue
		}
		if lifecycle, ok := component.AsLifecycleComponent(mc.Component); ok {
			childCtx, cancel := context.WithCancel(ctx)
			startDone := make(chan struct{})
			runtime := &componentRuntime{
				startDone:    startDone,
				startInvoked: true,
			}
			runtime.generation = lifecyclejoin.NewGeneration(cancel, func() { <-startDone })
			if cm.runtimes == nil {
				cm.runtimes = make(map[string]*componentRuntime)
			}
			cm.runtimes[name] = runtime
			componentName := name
			managed := mc
			lifecycleComponent := lifecycle
			launchCtx := childCtx
			componentsToStart = append(componentsToStart, componentToStart{
				name:    componentName,
				runtime: runtime,
				start: func() error {
					return cm.startComponent(launchCtx, componentName, managed, runtime, lifecycleComponent)
				},
			})
			mc.StartOrder = len(cm.startOrder)
			cm.startOrder = append(cm.startOrder, name)
		}
	}
	cm.mu.Unlock()

	var (
		batch     sync.WaitGroup
		errMu     sync.Mutex
		startErrs []error
	)
	for _, comp := range componentsToStart {
		batch.Add(1)
		go func(c componentToStart) {
			defer batch.Done()
			if err := c.start(); err != nil {
				errMu.Lock()
				startErrs = append(startErrs, fmt.Errorf("component %q: %w", c.name, err))
				errMu.Unlock()
			}
		}(comp)
	}
	batch.Wait()
	return errors.Join(startErrs...)
}

// startComponent runs a single component's Start (on a launch goroutine) and
// records the resulting state. The returned error propagates through the
// startAllComponents barrier so boot fails closed on it.
func (cm *ComponentManager) startComponent(
	ctx context.Context,
	name string,
	mc *component.ManagedComponent,
	runtime *componentRuntime,
	lc component.LifecycleComponent,
) error {
	defer close(runtime.startDone)
	cm.logger.Debug("Starting component", "name", name, "type", mc.Component.Meta().Type)

	if err := lc.Start(ctx); err != nil {
		runtime.selectCancelFirstShutdown()
		runtime.startErr = err
		cm.updateComponentState(name, component.StateFailed, err)
		cm.logger.Error("Component failed to start",
			"name", name, "type", mc.Component.Meta().Type, "error", err)
		return err
	}

	cm.updateComponentState(name, component.StateStarted, nil)
	if err := cm.registerProvidedStores(name, mc.Component); err != nil {
		runtime.selectCancelFirstShutdown()
		cm.updateComponentState(name, component.StateFailed, err)
		cm.logger.Error("Component store registration failed",
			"name", name, "type", mc.Component.Meta().Type, "error", err)
		return err
	}
	runtime.admitGracefulShutdown()
	cm.logger.Debug("Component started successfully", "name", name, "type", mc.Component.Meta().Type)
	return nil
}

// Stop gracefully stops all components in reverse order of startup.
func (cm *ComponentManager) Stop(ctx context.Context) error {
	if ctx == nil {
		return errs.WrapInvalid(errs.ErrInvalidData, "ComponentManager", "Stop", "nil context")
	}

	cm.stopMu.Lock()
	generation := cm.managerGeneration
	if generation == nil {
		cm.stopMu.Unlock()
		return cm.BaseService.Stop(ctx)
	}
	cm.stopping = true
	cm.stopMu.Unlock()

	stopErr := generation.StopWithQuiesce(ctx, func() error {
		// stopping was published under stopMu before quiesce began, so no new
		// callback borrows can enter. Wait without holding manager locks.
		cm.borrows.Wait()
		if cm.shutdown != nil {
			select {
			case <-cm.shutdown:
			default:
				close(cm.shutdown)
			}
		}
		if cm.registry != nil {
			cm.registry.StopHeartbeat()
		}
		return nil
	}, func(ctx context.Context) error {
		return errors.Join(cm.stopAllComponents(ctx)...)
	}, func(ctx context.Context) error {
		var stopErrors []error
		if baseErr := cm.BaseService.Stop(ctx); baseErr != nil {
			stopErrors = append(stopErrors, fmt.Errorf("failed to stop base service: %w", baseErr))
		}
		return errors.Join(stopErrors...)
	})
	stopErr = attributeShutdownError("component-manager", errs.PhaseJoinRuntime, stopErr)
	if ctxErr := ctx.Err(); ctxErr != nil && errors.Is(stopErr, ctxErr) {
		return stopErr
	}
	cm.started.Store(false)
	if cm.done != nil {
		select {
		case <-cm.done:
		default:
			close(cm.done)
		}
	}
	return stopErr
}

// withComponents lends a same-generation snapshot of live component handles
// to framework composition code. The callback runs without manager locks and
// must not synchronously invoke ComponentManager.Stop: Stop closes admission
// and waits for this borrow to return.
func (cm *ComponentManager) withComponents(
	callback func(map[string]*component.ManagedComponent) error,
) error {
	if callback == nil {
		return nil
	}
	cm.stopMu.Lock()
	if cm.stopping {
		cm.stopMu.Unlock()
		return errs.WrapTransient(
			fmt.Errorf("component manager is stopping"),
			"ComponentManager", "withComponents", "borrow admission")
	}
	cm.borrows.Add(1)
	cm.stopMu.Unlock()
	defer cm.borrows.Done()

	cm.mu.RLock()
	components := make(map[string]*component.ManagedComponent, len(cm.components))
	for name, managed := range cm.components {
		copyOfManaged := *managed
		components[name] = &copyOfManaged
	}
	cm.mu.RUnlock()
	return callback(components)
}

// stopAllComponents stops all components in reverse startup order and returns
// every error. Shutdown stays on the caller's stack: Stop must not launch
// cleanup goroutines that can outlive a caller whose context expires.
func (cm *ComponentManager) stopAllComponents(ctx context.Context) []error {
	// Snapshot state under the write lock, then release it before calling
	// component code, which may re-enter manager state updates.
	cm.mu.Lock()
	type target struct {
		name    string
		mc      *component.ManagedComponent
		runtime *componentRuntime
	}
	targets := make([]target, 0, len(cm.startOrder))
	for i := len(cm.startOrder) - 1; i >= 0; i-- {
		name := cm.startOrder[i]
		if mc, exists := cm.components[name]; exists {
			targets = append(targets, target{name: name, mc: mc, runtime: cm.runtimes[name]})
		}
	}
	cm.mu.Unlock()

	var stopErrors []error
	for _, t := range targets {
		if err := cm.stopSingleComponent(ctx, t.name, t.mc, t.runtime); err != nil {
			stopErrors = append(stopErrors, err)
		}
	}
	return stopErrors
}

// stopSingleComponent stops a single component and updates its state
func (cm *ComponentManager) stopSingleComponent(
	ctx context.Context, name string, mc *component.ManagedComponent, runtime *componentRuntime,
) error {
	// Try to stop component if it supports lifecycle
	if lifecycle, ok := component.AsLifecycleComponent(mc.Component); ok && runtime != nil {
		return cm.stopLifecycleComponent(ctx, name, runtime, lifecycle)
	}

	// Component doesn't support lifecycle, just mark as stopped
	cm.updateComponentState(name, component.StateStopped, nil)
	return nil
}

// stopLifecycleComponent stops a component that supports the lifecycle interface
func (cm *ComponentManager) stopLifecycleComponent(
	ctx context.Context, name string, runtime *componentRuntime, lifecycle component.LifecycleComponent,
) error {
	if runtime != nil {
		if runtime.selectShutdownModeForStop() != componentShutdownGraceful {
			// Pending Stop, failed Start, partial admission, and prior rollback all
			// select the generation's cancel-first operation permanently. Repeated
			// calls rejoin that same operation even after startDone closes.
			stopErr := runtime.generation.Stop(ctx, nil, func(ctx context.Context) error {
				cm.deregisterProvidedStores(name)
				if err := lifecycle.Stop(ctx); err != nil {
					cm.updateComponentState(name, component.StateFailed, err)
					return errs.NewShutdownError(
						"component/"+name,
						errs.PhaseDrainConsumers,
						fmt.Errorf("component '%s': %w", name, err),
					)
				}
				cm.updateComponentState(name, component.StateStopped, nil)
				return nil
			})
			return attributeShutdownError("component/"+name, errs.PhaseJoinRuntime, stopErr)
		}
		// Only a generation whose Start and store registration both completed
		// before any abort selection may quiesce with live Start authority.
		stopErr := runtime.generation.StopWithQuiesce(ctx, func() error {
			// Clear this component's stores from the shared registry before Stop closes
			// them (ADR-063), so no consumer resolves a closing handle.
			cm.deregisterProvidedStores(name)
			return nil
		}, func(ctx context.Context) error {
			if err := lifecycle.Stop(ctx); err != nil {
				cm.updateComponentState(name, component.StateFailed, err)
				return errs.NewShutdownError(
					"component/"+name,
					errs.PhaseDrainConsumers,
					fmt.Errorf("component '%s': %w", name, err),
				)
			}
			return nil
		}, func(context.Context) error {
			cm.updateComponentState(name, component.StateStopped, nil)
			return nil
		})
		return attributeShutdownError("component/"+name, errs.PhaseJoinRuntime, stopErr)
	}
	return nil
}

// updateComponentState safely updates component state with proper locking
func (cm *ComponentManager) updateComponentState(name string, state component.State, err error) {
	cm.mu.Lock()
	defer cm.mu.Unlock()

	if mc, exists := cm.components[name]; exists {
		mc.State = state
		mc.LastError = err
	}
}

func (cm *ComponentManager) createComponent(
	instanceName string, cfg types.ComponentConfig, deps component.Dependencies,
) error {
	if instanceName == "" {
		return fmt.Errorf("instance name cannot be empty")
	}
	if cfg.Name == "" {
		return fmt.Errorf("component factory name cannot be empty")
	}
	if cfg.Type == "" {
		return fmt.Errorf("component type cannot be empty")
	}

	// Check if component already exists
	cm.mu.RLock()
	if _, exists := cm.components[instanceName]; exists {
		cm.mu.RUnlock()
		return fmt.Errorf("component '%s' already exists", instanceName)
	}
	cm.mu.RUnlock()

	// Create component with the new factory pattern
	state := component.StateCreated
	comp, err := cm.registry.CreateComponent(
		componentadmission.Access{}, instanceName, cfg, deps,
		func(comp component.Discoverable) error {
			if lifecycle, ok := component.AsLifecycleComponent(comp); ok {
				if err := lifecycle.Initialize(); err != nil {
					return fmt.Errorf("initialize component %q: %w", instanceName, err)
				}
				state = component.StateInitialized
			}
			return nil
		},
	)
	if err != nil {
		return err
	}

	// Track as managed component. Retain the effective config so a later
	// per-component config update can be compared and skipped when unchanged
	// (gh#520).
	mc := &component.ManagedComponent{
		Component: comp,
		State:     state,
		Config:    cfg,
	}

	cm.mu.Lock()
	cm.components[instanceName] = mc
	cm.mu.Unlock()

	// Invalidate FlowGraph cache when components change
	cm.invalidateFlowGraph()

	return nil
}

// IsInitialized returns true if the component manager is initialized
func (cm *ComponentManager) IsInitialized() bool {
	return cm.initialized.Load()
}

// IsStarted returns true if the component manager is started
func (cm *ComponentManager) IsStarted() bool {
	return cm.started.Load()
}

// healthCheck performs a health check for the ComponentManager
// This is called from the BaseService health monitoring and should be lightweight and non-blocking
func (cm *ComponentManager) healthCheck() error {
	// Basic checks that don't require locks
	if !cm.initialized.Load() {
		return fmt.Errorf("component manager not initialized")
	}

	if !cm.started.Load() {
		return nil // Still starting up, assume healthy
	}

	// performDetailedHealthCheck is non-blocking (best-effort TryRLock), so it is
	// safe to call directly — no goroutine/timeout wrapper needed.
	return cm.performDetailedHealthCheck()
}

// performDetailedHealthCheck performs the actual health check with locks.
//
// It is best-effort and MUST never block: if the read lock cannot be taken
// immediately (a writer holds or is pending it, e.g. during the cold-boot
// startup window), it assumes healthy rather than waiting. We use TryRLock
// rather than a goroutine + timeout to acquire the lock, because abandoning a
// lock-acquiring goroutine on a timeout leaks a phantom reader that later
// deadlocks every writer and, in turn, Stop (gh#508).
func (cm *ComponentManager) performDetailedHealthCheck() error {
	if !cm.mu.TryRLock() {
		// Under write contention - assume healthy to avoid blocking.
		return nil
	}
	defer cm.mu.RUnlock()

	// Check for any failed components
	for name, comp := range cm.components {
		if comp.Component == nil {
			return fmt.Errorf("component %s has nil implementation", name)
		}

		// A boot start or terminal stop error leaves the component in StateFailed.
		// Health must report it, not silently skip it.
		if comp.State == component.StateFailed {
			if comp.LastError != nil {
				return fmt.Errorf("component %s failed: %w", name, comp.LastError)
			}
			return fmt.Errorf("component %s failed", name)
		}
	}

	return nil
}

// shutdownCallback is called during graceful shutdown
func (cm *ComponentManager) shutdownCallback(ctx context.Context) error {
	return cm.Stop(ctx)
}

// buildComponentDependencies creates Dependencies from ComponentManager's context
func (cm *ComponentManager) buildComponentDependencies() component.Dependencies {
	deps := component.Dependencies{
		NATSClient:      cm.natsClient,
		MetricsRegistry: cm.BaseService.metricsRegistry,
		Logger:          cm.BaseService.logger,
		Platform: component.PlatformMeta{
			Org:      cm.platform.Org,
			Platform: cm.platform.Platform,
		},
		Security:         cm.bootSecurity,
		ModelRegistry:    cm.bootModelRegistry,
		ToolRegistry:     cm.toolRegistry,
		PayloadRegistry:  cm.payloadRegistry,
		LifecycleManager: cm.lifecycleManager,
		StoreRegistry:    cm.storeRegistry,
	}

	return deps
}

// registerProvidedStores registers a just-started component's provided stores
// into the shared StoreRegistry (ADR-063). Invalid and duplicate claims are
// provider startup errors. Registrations from the failing provider are rolled
// back while a duplicate incumbent remains untouched. The component call
// happens OUTSIDE any manager lock; only storeProvided is guarded by storeMu.
func (cm *ComponentManager) registerProvidedStores(name string, comp component.Discoverable) error {
	if cm.storeRegistry == nil {
		return nil
	}
	provider, ok := comp.(component.StoreProvider)
	if !ok {
		return nil
	}
	// Liveness guard: skip if terminal Stop halted this component between Start
	// and registration. Read under cm.mu; registration otherwise holds no manager
	// lock, so the ordering remains acyclic.
	cm.mu.RLock()
	mc, tracked := cm.components[name]
	live := tracked && mc.State == component.StateStarted
	cm.mu.RUnlock()
	if !live {
		return nil
	}
	provided := provider.ProvidedStores()
	if len(provided) == 0 {
		return nil
	}

	instances := make([]string, 0, len(provided))
	for instance, store := range provided {
		if strings.TrimSpace(instance) == "" {
			return fmt.Errorf("store registry: component %q claimed an empty StorageInstance", name)
		}
		storeIsNil := store == nil
		if !storeIsNil {
			storeValue := reflect.ValueOf(store)
			switch storeValue.Kind() {
			case reflect.Chan, reflect.Func, reflect.Interface, reflect.Map, reflect.Ptr, reflect.Slice:
				storeIsNil = storeValue.IsNil()
			}
		}
		if storeIsNil {
			return fmt.Errorf("store registry: component %q claimed nil store for instance %q", name, instance)
		}
		instances = append(instances, instance)
	}
	sort.Strings(instances)

	registered := make([]string, 0, len(provided))
	for _, instance := range instances {
		store := provided[instance]
		if err := cm.storeRegistry.Register(instance, store); err != nil {
			for _, registeredInstance := range registered {
				cm.storeRegistry.Deregister(registeredInstance)
			}
			return fmt.Errorf("store registry: component %q instance %q: %w", name, instance, err)
		}
		registered = append(registered, instance)
		cm.logger.Debug("store registry: registered store", "component", name, "instance", instance)
	}
	if len(registered) > 0 {
		cm.storeMu.Lock()
		if cm.storeProvided == nil {
			cm.storeProvided = make(map[string][]string)
		}
		cm.storeProvided[name] = append(cm.storeProvided[name], registered...)
		cm.storeMu.Unlock()
	}
	return nil
}

// deregisterProvidedStores clears a stopping component's stores from the
// shared StoreRegistry (ADR-063). Idempotent and keyed by the tracked instance
// names, so it never re-reads a component whose store may already be closed. Call
// BEFORE the component's Stop() closes the underlying store, so the registry does
// not briefly point at a closing handle. A no-op when the component provided no
// store.
func (cm *ComponentManager) deregisterProvidedStores(name string) {
	if cm.storeRegistry == nil {
		return
	}
	cm.storeMu.Lock()
	instances := cm.storeProvided[name]
	delete(cm.storeProvided, name)
	cm.storeMu.Unlock()
	for _, instance := range instances {
		cm.storeRegistry.Deregister(instance)
		cm.logger.Debug("store registry: deregistered store", "component", name, "instance", instance)
	}
}

// GetComponentHealth returns current health status for all managed components
// Direct component health queries using the component.Health() interface
func (cm *ComponentManager) GetComponentHealth() map[string]component.HealthStatus {
	cm.mu.RLock()
	defer cm.mu.RUnlock()

	result := make(map[string]component.HealthStatus)
	for name, mc := range cm.components {
		if mc.Component != nil {
			// Query component's own Health() method directly
			result[name] = mc.Component.Health()
		}
	}
	return result
}

// GetHealthyComponents returns names of components that report healthy status
func (cm *ComponentManager) GetHealthyComponents() []string {
	health := cm.GetComponentHealth()
	var healthy []string
	for name, h := range health {
		if h.Healthy {
			healthy = append(healthy, name)
		}
	}
	return healthy
}

// GetUnhealthyComponents returns names of components that report unhealthy status
func (cm *ComponentManager) GetUnhealthyComponents() []string {
	health := cm.GetComponentHealth()
	var unhealthy []string
	for name, h := range health {
		if !h.Healthy {
			unhealthy = append(unhealthy, name)
		}
	}
	return unhealthy
}

// GetComponentStatus returns combined lifecycle state and health status for all components
func (cm *ComponentManager) GetComponentStatus() map[string]ComponentStatus {
	cm.mu.RLock()
	result := make(map[string]ComponentStatus)
	for name, mc := range cm.components {
		result[name] = ComponentStatus{
			Name:      name,
			State:     mc.State,
			LastError: mc.LastError,
		}
	}
	cm.mu.RUnlock()

	_ = cm.withComponents(func(components map[string]*component.ManagedComponent) error {
		for name, managed := range components {
			status := result[name]
			if managed.Component != nil {
				status.Health = managed.Component.Health()
				status.DataFlow = managed.Component.DataFlow()
			}
			result[name] = status
		}
		return nil
	})
	return result
}

// ComponentStatus combines lifecycle state with health and flow metrics
type ComponentStatus struct {
	Name      string                 `json:"name"`
	State     component.State        `json:"state"`
	Health    component.HealthStatus `json:"health"`
	DataFlow  component.FlowMetrics  `json:"data_flow"`
	LastError error                  `json:"last_error,omitempty"`
}

// Flow validation types for ComponentManager operational validation

// ComponentPortInfo represents port information extracted from a component
type ComponentPortInfo struct {
	ComponentName string                `json:"component_name"`
	InputPorts    []ComponentPortDetail `json:"input_ports"`
	OutputPorts   []ComponentPortDetail `json:"output_ports"`
}

// ComponentPortDetail represents detailed information about a single port
type ComponentPortDetail struct {
	Name      string              `json:"name"`
	Direction component.Direction `json:"direction"`
	Subject   string              `json:"subject"`
	PortType  string              `json:"port_type"`
}

// FlowConnection represents a connection between publisher and subscriber
type FlowConnection struct {
	Publisher  ComponentPortReference `json:"publisher"`
	Subscriber ComponentPortReference `json:"subscriber"`
	Subject    string                 `json:"subject"`
}

// ComponentPortReference references a specific port on a component
type ComponentPortReference struct {
	ComponentName string `json:"component_name"`
	PortName      string `json:"port_name"`
}

// FlowGap represents a disconnected port (no matching publisher/subscriber)
type FlowGap struct {
	ComponentName string `json:"component_name"`
	PortName      string `json:"port_name"`
	Subject       string `json:"subject"`
	Direction     string `json:"direction"` // "input" or "output"
	Issue         string `json:"issue"`     // "no_publishers" or "no_subscribers"
}

// extractComponentPortInfo extracts retained port information for flow validation.
func (cm *ComponentManager) extractComponentPortInfo(instanceName string) (*ComponentPortInfo, error) {
	snapshot, ok := cm.registry.Snapshot(componentadmission.Access{}, instanceName)
	if !ok {
		return nil, fmt.Errorf("component %q has no admitted declaration", instanceName)
	}
	portInfo := &ComponentPortInfo{
		ComponentName: instanceName,
		InputPorts:    []ComponentPortDetail{},
		OutputPorts:   []ComponentPortDetail{},
	}

	inputs := snapshot.Inputs()
	inputFacts := snapshot.InputDeclarationFacts()
	for index, port := range inputs {
		portInfo.InputPorts = append(portInfo.InputPorts, cm.extractPortDetail(port, inputFacts[index]))
	}

	outputs := snapshot.Outputs()
	outputFacts := snapshot.OutputDeclarationFacts()
	for index, port := range outputs {
		portInfo.OutputPorts = append(portInfo.OutputPorts, cm.extractPortDetail(port, outputFacts[index]))
	}

	return portInfo, nil
}

// extractPortDetail extracts subject and type information from a port
func (cm *ComponentManager) extractPortDetail(
	port component.Port, facts component.PortFacts,
) ComponentPortDetail {
	detail := &ComponentPortDetail{
		Name:      port.Name,
		Direction: port.Direction,
		PortType:  string(facts.Kind()),
	}
	if subjects := facts.NATSSubjects(); len(subjects) > 0 {
		detail.Subject = subjects[0]
	}
	return *detail
}

// analyzeFlowConnections identifies connections between components based on subject matching
func (cm *ComponentManager) analyzeFlowConnections(instanceNames []string) ([]FlowConnection, error) {
	var connections []FlowConnection

	// Build lists of publishers and subscribers
	var publishers []publisherInfo
	var subscribers []subscriberInfo

	for _, instanceName := range instanceNames {
		portInfo, err := cm.extractComponentPortInfo(instanceName)
		if err != nil {
			return nil, err
		}

		// Collect publishers (output ports)
		for _, outPort := range portInfo.OutputPorts {
			publishers = append(publishers, publisherInfo{
				ComponentName: portInfo.ComponentName,
				PortName:      outPort.Name,
				Subject:       outPort.Subject,
			})
		}

		// Collect subscribers (input ports)
		for _, inPort := range portInfo.InputPorts {
			subscribers = append(subscribers, subscriberInfo{
				ComponentName: portInfo.ComponentName,
				PortName:      inPort.Name,
				Subject:       inPort.Subject,
			})
		}
	}

	// Match publishers to subscribers based on exact subject match (simple implementation)
	for _, pub := range publishers {
		for _, sub := range subscribers {
			if pub.Subject == sub.Subject {
				connections = append(connections, FlowConnection{
					Publisher: ComponentPortReference{
						ComponentName: pub.ComponentName,
						PortName:      pub.PortName,
					},
					Subscriber: ComponentPortReference{
						ComponentName: sub.ComponentName,
						PortName:      sub.PortName,
					},
					Subject: pub.Subject,
				})
			}
		}
	}

	return connections, nil
}

// Helper types for flow analysis
type publisherInfo struct {
	ComponentName string
	PortName      string
	Subject       string
}

type subscriberInfo struct {
	ComponentName string
	PortName      string
	Subject       string
}

// =============================================================================
// FlowGraph Integration
// =============================================================================

// flowGraphCache provides efficient caching of FlowGraph analysis results
type flowGraphCache struct {
	mu           sync.RWMutex
	currentGraph *flowgraph.FlowGraph
	lastAnalysis *flowgraph.FlowAnalysisResult
	cacheValid   bool
	lastUpdate   time.Time
}

// GetFlowGraph returns the current FlowGraph, using cache if valid.
func (cm *ComponentManager) GetFlowGraph() (*flowgraph.FlowGraph, error) {
	// Check cache validity under read lock
	cm.graphCache.mu.RLock()
	if cm.graphCache.cacheValid && cm.graphCache.currentGraph != nil {
		graph := cm.graphCache.currentGraph
		cm.graphCache.mu.RUnlock()
		return graph, nil
	}
	cm.graphCache.mu.RUnlock()

	// Need to rebuild graph - acquire write lock
	cm.graphCache.mu.Lock()
	defer cm.graphCache.mu.Unlock()

	// Double-check after acquiring write lock
	if cm.graphCache.cacheValid && cm.graphCache.currentGraph != nil {
		return cm.graphCache.currentGraph, nil
	}

	// Build new graph
	graph, err := cm.buildFlowGraph()
	if err != nil {
		return nil, err
	}

	// Update cache
	cm.graphCache.currentGraph = graph
	cm.graphCache.cacheValid = true
	cm.graphCache.lastUpdate = time.Now()

	return graph, nil
}

// buildFlowGraph creates a new FlowGraph from current components
func (cm *ComponentManager) buildFlowGraph() (*flowgraph.FlowGraph, error) {
	return flowgraph.BuildFromRegistry(componentadmission.Access{}, cm.registry)
}

// invalidateFlowGraph marks the cached FlowGraph as invalid
func (cm *ComponentManager) invalidateFlowGraph() {
	cm.graphCache.mu.Lock()
	defer cm.graphCache.mu.Unlock()

	cm.graphCache.cacheValid = false
	cm.graphCache.currentGraph = nil
	cm.graphCache.lastAnalysis = nil
}

// ValidateFlowConnectivity performs FlowGraph connectivity analysis with caching.
func (cm *ComponentManager) ValidateFlowConnectivity() (*flowgraph.FlowAnalysisResult, error) {
	// Check if we have a cached analysis
	cm.graphCache.mu.RLock()
	if cm.graphCache.cacheValid && cm.graphCache.lastAnalysis != nil {
		analysis := cm.graphCache.lastAnalysis
		cm.graphCache.mu.RUnlock()
		return analysis, nil
	}
	cm.graphCache.mu.RUnlock()

	// Get graph (may trigger rebuild)
	graph, err := cm.GetFlowGraph()
	if err != nil {
		return nil, err
	}

	// Perform analysis
	analysis := graph.AnalyzeConnectivity()

	// Cache the analysis result
	cm.graphCache.mu.Lock()
	cm.graphCache.lastAnalysis = analysis
	cm.graphCache.mu.Unlock()

	return analysis, nil
}

// GetFlowPaths returns data paths from input components to all reachable components
func (cm *ComponentManager) GetFlowPaths() (map[string][]string, error) {
	graph, err := cm.GetFlowGraph()
	if err != nil {
		return nil, err
	}

	paths := make(map[string][]string)

	// Find all input components (components with no input ports or external input ports)
	inputComponents := cm.findInputComponents(graph)

	for _, inputComponent := range inputComponents {
		// Use graph traversal to find all reachable components
		reachable := cm.depthFirstTraversal(graph, inputComponent)
		paths[inputComponent] = reachable
	}

	return paths, nil
}

// DetectObjectStoreGaps identifies disconnected storage components
func (cm *ComponentManager) DetectObjectStoreGaps() ([]ComponentGap, error) {
	graph, err := cm.GetFlowGraph()
	if err != nil {
		return nil, err
	}
	var gaps []ComponentGap

	nodes := graph.GetNodes()

	for componentName, node := range nodes {
		// Check if this is a storage component
		if cm.isStorageComponent(componentName, node) {
			// Check if storage component has input connections
			if !cm.hasIncomingEdges(graph, componentName) {
				gaps = append(gaps, ComponentGap{
					ComponentName: componentName,
					Issue:         "no_input_connections",
					Description:   "Storage component configured but not receiving data",
					Suggestions: []string{
						"Configure input ports to subscribe to data streams",
						"Verify subject routing from processors to storage",
						"Check component configuration and port subjects",
					},
				})
			}
		}
	}

	return gaps, nil
}

// Helper methods for FlowGraph analysis

// findInputComponents identifies components that serve as data inputs
func (cm *ComponentManager) findInputComponents(graph *flowgraph.FlowGraph) []string {
	var inputs []string
	nodes := graph.GetNodes()

	for componentName, node := range nodes {
		// Check if component type is "input" or has external input ports
		if cm.isInputComponent(componentName, node) {
			inputs = append(inputs, componentName)
		}
	}

	return inputs
}

// isInputComponent determines if a component is an input component
func (cm *ComponentManager) isInputComponent(componentName string, node *flowgraph.ComponentNode) bool {
	// Check component configuration for type
	if cm.componentConfigs != nil {
		if compCfg, ok := cm.componentConfigs[componentName]; ok {
			if compCfg.Type == "input" {
				return true
			}
		}
	}

	// Check if component has external input ports (network listener or outbound
	// HTTP-client). Both patterns indicate the component is the data source for
	// the internal graph: PatternNetwork binds a local listener, PatternHTTPClient
	// initiates an outbound poll. Either makes the component an input origin.
	for _, port := range node.InputPorts {
		if port.Pattern == component.PatternNetwork || port.Pattern == component.PatternHTTPClient {
			return true
		}
	}

	return false
}

// isStorageComponent determines if a component is a storage component
func (cm *ComponentManager) isStorageComponent(componentName string, _ *flowgraph.ComponentNode) bool {
	// Check component configuration for type
	if cm.componentConfigs != nil {
		if compCfg, ok := cm.componentConfigs[componentName]; ok {
			if compCfg.Type == "storage" || compCfg.Type == "output" {
				return true
			}
		}
	}

	// Check for storage-related component names
	return strings.Contains(strings.ToLower(componentName), "store") ||
		strings.Contains(strings.ToLower(componentName), "storage")
}

// hasIncomingEdges checks if a component has any incoming edges
func (cm *ComponentManager) hasIncomingEdges(graph *flowgraph.FlowGraph, componentName string) bool {
	edges := graph.GetEdges()

	for _, edge := range edges {
		if edge.To.ComponentName == componentName {
			return true
		}
	}

	return false
}

// depthFirstTraversal performs DFS to find all reachable components from a starting component
func (cm *ComponentManager) depthFirstTraversal(graph *flowgraph.FlowGraph, start string) []string {
	visited := make(map[string]bool)
	var result []string

	// Build adjacency list from edges
	adj := make(map[string][]string)
	edges := graph.GetEdges()

	for _, edge := range edges {
		from := edge.From.ComponentName
		to := edge.To.ComponentName
		adj[from] = append(adj[from], to)
	}

	// DFS traversal
	cm.dfsVisit(start, adj, visited, &result)

	return result
}

// dfsVisit performs the actual DFS traversal
func (cm *ComponentManager) dfsVisit(node string, adj map[string][]string, visited map[string]bool, result *[]string) {
	visited[node] = true
	*result = append(*result, node)

	for _, neighbor := range adj[node] {
		if !visited[neighbor] {
			cm.dfsVisit(neighbor, adj, visited, result)
		}
	}
}

// ComponentGap represents a connectivity gap in the component flow
type ComponentGap struct {
	ComponentName string   `json:"component_name"`
	Issue         string   `json:"issue"`
	Description   string   `json:"description"`
	Suggestions   []string `json:"suggestions,omitempty"`
}

// publishHealthLoop publishes component health to JetStream every 5s.
// Each component's health is published to health.component.{name} for granular filtering.
// Gracefully handles NATS being unavailable - skips publish, doesn't block.
//
// Fires an immediate first publish before entering the tick loop so the HEALTH
// JetStream stream is seeded as soon as ComponentManager.Start completes —
// same warm-up rationale as Manager.publishHealthLoop. Eliminates the
// cold-start race that caused the verify-websocket-stream e2e flake.
func (cm *ComponentManager) publishHealthLoop(ctx context.Context) {
	cm.publishComponentHealth(ctx) // seed stream immediately
	ticker := time.NewTicker(5 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-cm.shutdown:
			return
		case <-ticker.C:
			cm.publishComponentHealth(ctx)
		}
	}
}

// publishComponentHealth publishes health for each component to NATS JetStream.
func (cm *ComponentManager) publishComponentHealth(ctx context.Context) {
	// Graceful fallback: skip if NATS unavailable
	if cm.natsClient == nil {
		return
	}

	cm.mu.RLock()
	components := make(map[string]*component.ManagedComponent, len(cm.components))
	for name, mc := range cm.components {
		components[name] = mc
	}
	cm.mu.RUnlock()

	timestamp := time.Now().UnixMilli()

	for name, mc := range components {
		if mc.Component == nil {
			continue
		}

		health := mc.Component.Health()
		data, err := json.Marshal(map[string]any{
			"timestamp": timestamp,
			"name":      name,
			"health":    health,
		})
		if err != nil {
			continue
		}

		// Publish to health.component.{name} for granular filtering
		subject := "health.component." + name
		_ = cm.natsClient.PublishToStream(ctx, subject, data)
	}
}

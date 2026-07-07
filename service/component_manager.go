// Package service provides service management and HTTP APIs for the SemStreams platform.
package service

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/c360studio/semstreams/component"
	"github.com/c360studio/semstreams/component/flowgraph"
	"github.com/c360studio/semstreams/config"
	"github.com/c360studio/semstreams/model"
	"github.com/c360studio/semstreams/natsclient"
	"github.com/c360studio/semstreams/payloadregistry"
	"github.com/c360studio/semstreams/pkg/lifecycle"
	"github.com/c360studio/semstreams/pkg/retry"
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
	startOrder       []string                               // Track start order for reverse stop
	resources        map[string][]string                    // resourceID → component names

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

	// Config management
	natsClient           *natsclient.Client
	configManager        *config.Manager
	configUpdates        <-chan config.Update // Channel for components.* updates
	modelRegistryUpdates <-chan config.Update // Channel for model_registry KV key updates

	// FlowGraph caching for thread-safe analysis
	graphCache flowGraphCache

	// Lifecycle hooks for debugging and monitoring
	onComponentStart func(ctx context.Context, name string, comp component.Discoverable)
	onComponentStop  func(ctx context.Context, name string, reason string)
	onComponentError func(ctx context.Context, name string, err error)
	onHealthChange   func(ctx context.Context, name string, healthy bool, details string)

	// Thread safety for component operations
	mu          sync.RWMutex
	initialized atomic.Bool
	initMu      sync.Mutex
	started     atomic.Bool
	startMu     sync.Mutex

	// Shutdown coordination for proper lifecycle
	shutdown chan struct{}
	done     chan struct{}
	wg       sync.WaitGroup
}

// ComponentManagerOption removed - we now use Dependencies pattern instead

// NewComponentManager creates a new ComponentManager using the standard constructor pattern
func NewComponentManager(rawConfig json.RawMessage, deps *Dependencies) (Service, error) {
	// Parse config - handle empty or invalid JSON properly
	var cfg ComponentManagerConfig
	if len(rawConfig) > 0 {
		if err := json.Unmarshal(rawConfig, &cfg); err != nil {
			return nil, fmt.Errorf("parse component-manager config: %w", err)
		}
	}

	// Apply defaults - clear and visible in constructor
	// WatchConfig defaults to false (zero value)
	if cfg.EnabledComponents == nil {
		cfg.EnabledComponents = []string{}
	}

	// Validate configuration
	if err := cfg.Validate(); err != nil {
		return nil, fmt.Errorf("validate component-manager config: %w", err)
	}

	// Get initial component configs from Manager if available
	var componentsConfig config.ComponentConfigs
	var configUpdates <-chan config.Update
	var modelRegistryUpdates <-chan config.Update
	var configManager *config.Manager

	if deps != nil && deps.Manager != nil {
		configManager = deps.Manager
		fullConfig := configManager.GetConfig()
		if fullConfig != nil {
			componentsConfig = fullConfig.Get().Components
		}
		// Subscribe to config changes if watching is enabled. Two subjects:
		//   components.*    — individual component adds/removes/config edits
		//   model_registry  — shared registry that dependent components
		//                     (declared via Registration.Dependencies) must
		//                     be restarted for to pick up, since some cache
		//                     derived clients at Start time.
		if cfg.WatchConfig {
			configUpdates = configManager.OnChange("components.*")
			modelRegistryUpdates = configManager.OnChange("model_registry")
		}
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
		BaseService:          baseService,
		config:               cfg, // Store config as field
		registry:             registry,
		toolRegistry:         toolRegistry,
		payloadRegistry:      payloadRegistry,
		lifecycleManager:     lifecycleManager,
		componentConfigs:     componentsConfig,
		platform:             platform,
		components:           make(map[string]*component.ManagedComponent),
		startOrder:           make([]string, 0),
		resources:            make(map[string][]string),
		storeRegistry:        storeregistry.New(),
		storeProvided:        make(map[string][]string),
		configManager:        configManager,
		configUpdates:        configUpdates,
		modelRegistryUpdates: modelRegistryUpdates,
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
		cm.initialized.Store(true)
		return nil
	}

	cm.logger.Debug("ComponentManager.Initialize: Initializing with component configs",
		"count", len(cm.componentConfigs))

	// Reset component tracking
	if cm.components == nil {
		cm.components = make(map[string]*component.ManagedComponent)
	}
	if cm.resources == nil {
		cm.resources = make(map[string][]string)
	}
	cm.startOrder = make([]string, 0)

	// Manager handles config watching now, no need for separate ConfigWatcher initialization

	// Create components from configuration
	if len(cm.componentConfigs) > 0 {
		cm.logger.Debug("ComponentManager.Initialize: Creating components from config",
			"count", len(cm.componentConfigs))

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
			if err := cm.CreateComponent(context.Background(), instanceName, componentConfig, deps); err != nil {
				cm.logger.Error("Failed to create component from config",
					"instance", instanceName,
					"factory", componentConfig.Name,
					"type", componentConfig.Type,
					"error", err)
				// Continue with other components instead of failing entirely
				continue
			}

			cm.logger.Debug("Component created from config",
				"instance", instanceName,
				"factory", componentConfig.Name,
				"type", componentConfig.Type)
		}

		cm.logger.Debug("ComponentManager.Initialize: Finished creating components",
			"created", len(cm.components))
	} else {
		cm.logger.Debug("ComponentManager.Initialize: No component configs to create")
	}

	cm.initialized.Store(true)
	return nil
}

// Start starts all initialized components with proper context flow-through
func (cm *ComponentManager) Start(ctx context.Context) error {
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

	// Start watching for config updates if channel is available
	if cm.configUpdates != nil {
		cm.wg.Add(1)
		go func() {
			defer cm.wg.Done()
			cm.watchConfigUpdates(ctx)
		}()
	}

	cm.startOrder = make([]string, 0)

	// Initialize NATS-backed capability discovery
	cm.initCapabilityDiscovery(ctx)

	// Start all components
	cm.startAllComponents(ctx)

	cm.started.Store(true)

	// Start health publishing loop (publishes to health.component.{name})
	cm.wg.Add(1)
	go func() {
		defer cm.wg.Done()
		cm.publishHealthLoop(ctx)
	}()

	// Start the base service after components are started to avoid health check deadlocks
	if err := cm.BaseService.Start(ctx); err != nil {
		return fmt.Errorf("failed to start base service: %w", err)
	}

	return nil
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

// componentToStart holds component info for async startup.
type componentToStart struct {
	name      string
	mc        *component.ManagedComponent
	lifecycle component.LifecycleComponent
}

// startAllComponents prepares and starts all lifecycle components asynchronously.
func (cm *ComponentManager) startAllComponents(ctx context.Context) {
	cm.mu.Lock()
	componentsToStart := make([]componentToStart, 0, len(cm.components))
	for name, mc := range cm.components {
		if lifecycle, ok := component.AsLifecycleComponent(mc.Component); ok {
			childCtx, cancel := context.WithCancel(ctx)
			mc.Context = childCtx
			mc.Cancel = cancel
			componentsToStart = append(componentsToStart, componentToStart{name, mc, lifecycle})
			mc.StartOrder = len(cm.startOrder)
			cm.startOrder = append(cm.startOrder, name)
		}
	}
	cm.mu.Unlock()

	for _, comp := range componentsToStart {
		cm.wg.Add(1)
		go cm.startComponentAsync(comp.name, comp.mc, comp.lifecycle)
	}
}

// startComponentAsync starts a single component in a goroutine.
func (cm *ComponentManager) startComponentAsync(name string, mc *component.ManagedComponent, lc component.LifecycleComponent) {
	defer cm.wg.Done()

	cm.logger.Debug("Starting component", "name", name, "type", mc.Component.Meta().Type)

	if err := lc.Start(mc.Context); err != nil {
		cm.updateComponentState(name, component.StateFailed, err)
		cm.logger.Error("Component failed to start",
			"name", name, "type", mc.Component.Meta().Type, "error", err)
		if cm.onComponentError != nil {
			cm.onComponentError(mc.Context, name, err)
		}
		return
	}

	cm.updateComponentState(name, component.StateStarted, nil)
	cm.registerProvidedStores(name, mc.Component)
	cm.logger.Debug("Component started successfully", "name", name, "type", mc.Component.Meta().Type)
	if cm.onComponentStart != nil {
		cm.onComponentStart(mc.Context, name, mc.Component)
	}
}

// Stop gracefully stops all components in reverse order of startup
func (cm *ComponentManager) Stop(timeout time.Duration) error {
	// Create context with timeout for component shutdown
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	// Check if started
	if !cm.started.Load() {
		return cm.BaseService.Stop(timeout)
	}

	// Signal shutdown
	select {
	case <-cm.shutdown:
		// Already shutting down
		return nil
	default:
		close(cm.shutdown)
	}

	// Stop capability discovery heartbeat
	cm.registry.StopHeartbeat()

	// Config watching is now handled by Manager, no need to stop it here

	// Stop all components in reverse order.
	//
	// Do NOT hold cm.mu across stopAllComponents: it spawns parallel
	// shutdown goroutines that call updateComponentState / markComponent
	// Stopped, both of which acquire cm.mu. Holding the lock here would
	// deadlock on the first lifecycle component. stopAllComponents
	// snapshots the startOrder under its own brief lock acquisition.
	errors := cm.stopAllComponents(ctx)

	// Wait for all goroutines to finish with timeout
	doneChan := make(chan struct{})
	go func() {
		cm.wg.Wait()
		close(doneChan)
	}()

	select {
	case <-doneChan:
		close(cm.done)
	case <-ctx.Done():
		slog.Warn("Component stop timeout, forcing shutdown", slog.Duration("timeout", timeout))
		return fmt.Errorf("timeout waiting for components to stop: %w", ctx.Err())
	}

	cm.started.Store(false)

	// Stop the base service
	if baseErr := cm.BaseService.Stop(timeout); baseErr != nil {
		errors = append(errors, fmt.Errorf("failed to stop base service: %w", baseErr))
	}

	if len(errors) > 0 {
		return fmt.Errorf("failed to stop %d components: %v", len(errors), errors)
	}

	return nil
}

// stopAllComponents stops all components in parallel and returns any errors.
//
// Acquires cm.mu briefly to snapshot startOrder + managed components,
// then releases it before spawning parallel shutdown goroutines. The
// goroutines need to re-acquire the lock (via updateComponentState) to
// mark state transitions, so holding it across them would deadlock.
func (cm *ComponentManager) stopAllComponents(ctx context.Context) []error {
	// Snapshot state under the lock so the parallel goroutines below can
	// operate against an immutable view while re-acquiring the lock for
	// per-component state updates.
	cm.mu.RLock()
	type target struct {
		name string
		mc   *component.ManagedComponent
	}
	targets := make([]target, 0, len(cm.startOrder))
	for i := len(cm.startOrder) - 1; i >= 0; i-- {
		name := cm.startOrder[i]
		if mc, exists := cm.components[name]; exists {
			targets = append(targets, target{name: name, mc: mc})
		}
	}
	cm.mu.RUnlock()

	// Cancel all component contexts first to signal shutdown intent. This
	// walks the snapshot; concurrent state changes are irrelevant because
	// cancel is idempotent and mc pointers stay valid until removal.
	for _, t := range targets {
		cm.cancelComponentContext(t.mc)
	}

	errorChan := make(chan error, len(targets))
	var wg sync.WaitGroup

	for _, t := range targets {
		wg.Add(1)
		go func(componentName string, managedComp *component.ManagedComponent) {
			defer wg.Done()
			if err := cm.stopSingleComponent(ctx, componentName, managedComp); err != nil {
				errorChan <- err
			}
		}(t.name, t.mc)
	}

	wg.Wait()
	close(errorChan)

	// Collect all errors
	var errors []error
	for err := range errorChan {
		errors = append(errors, err)
	}

	return errors
}

// cancelComponentContext cancels the component's context if it exists
func (cm *ComponentManager) cancelComponentContext(mc *component.ManagedComponent) {
	if mc.Cancel != nil {
		mc.Cancel()
		// Clean up references to prevent resource leaks
		// This is safe during shutdown when no other operations should be using the context
		mc.Cancel = nil
		mc.Context = nil
	}
}

// stopSingleComponent stops a single component and updates its state
func (cm *ComponentManager) stopSingleComponent(
	ctx context.Context, name string, mc *component.ManagedComponent,
) error {
	// Try to stop component if it supports lifecycle
	if lifecycle, ok := component.AsLifecycleComponent(mc.Component); ok {
		return cm.stopLifecycleComponent(ctx, name, mc, lifecycle)
	}

	// Component doesn't support lifecycle, just mark as stopped
	cm.markComponentStopped(ctx, name, mc, "no-lifecycle")
	return nil
}

// stopLifecycleComponent stops a component that supports the lifecycle interface
func (cm *ComponentManager) stopLifecycleComponent(
	ctx context.Context, name string, mc *component.ManagedComponent,
	lifecycle component.LifecycleComponent,
) error {
	// Calculate timeout from context deadline
	timeout := 30 * time.Second // Default timeout
	if deadline, ok := ctx.Deadline(); ok {
		remaining := time.Until(deadline)
		if remaining > 0 && remaining < timeout {
			timeout = remaining
		}
	}

	// Clear this component's stores from the shared registry before Stop closes
	// them (ADR-063), so no consumer resolves a closing handle.
	cm.deregisterProvidedStores(name)

	// Call Stop with timeout - interface now supports it properly
	if err := lifecycle.Stop(timeout); err != nil {
		cm.updateComponentState(name, component.StateFailed, err)

		// Call error hook if registered
		if cm.onComponentError != nil {
			go cm.onComponentError(ctx, name, err)
		}

		return fmt.Errorf("component '%s': %w", name, err)
	}

	cm.markComponentStopped(ctx, name, mc, "graceful")
	return nil
}

// markComponentStopped marks a component as stopped and calls the stop hook
func (cm *ComponentManager) markComponentStopped(
	ctx context.Context, name string, _ *component.ManagedComponent, reason string,
) {
	cm.updateComponentState(name, component.StateStopped, nil)

	// Call stop hook if registered and context not cancelled
	if cm.onComponentStop != nil {
		select {
		case <-ctx.Done():
			cm.logger.Warn("Skipping stop hook due to context cancellation", "component", name)
		default:
			go cm.onComponentStop(ctx, name, reason)
		}
	}
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

// Component retrieves a specific component instance by name
func (cm *ComponentManager) Component(name string) component.Discoverable {
	cm.mu.RLock()
	defer cm.mu.RUnlock()

	return cm.registry.Component(name)
}

// ListComponents returns all registered component instances
func (cm *ComponentManager) ListComponents() map[string]component.Discoverable {
	cm.mu.RLock()
	defer cm.mu.RUnlock()

	return cm.registry.ListComponents()
}

// GetRegistry returns the component registry for schema introspection
// This is used by the schema API to access component schemas
func (cm *ComponentManager) GetRegistry() *component.Registry {
	return cm.registry
}

// CreateComponent creates a new component instance and registers it
// This is for runtime component creation, not part of the normal Initialize/Start flow
func (cm *ComponentManager) CreateComponent(
	ctx context.Context, instanceName string, cfg types.ComponentConfig, deps component.Dependencies,
) error {
	// Check for cancellation before expensive operation
	select {
	case <-ctx.Done():
		return ctx.Err()
	default:
	}

	if instanceName == "" {
		return fmt.Errorf("instance name cannot be empty")
	}
	if cfg.Name == "" {
		return fmt.Errorf("component factory name cannot be empty")
	}
	if cfg.Type == "" {
		return fmt.Errorf("component type cannot be empty")
	}

	cm.mu.Lock()
	defer cm.mu.Unlock()

	// Check if component already exists
	if _, exists := cm.components[instanceName]; exists {
		return fmt.Errorf("component '%s' already exists", instanceName)
	}

	// Create component with the new factory pattern
	comp, err := cm.registry.CreateComponent(instanceName, cfg, deps)
	if err != nil {
		return err
	}

	// Check for port conflicts using existing abstractions
	if err := cm.checkPortConflicts(comp); err != nil {
		// Rollback component creation
		cm.registry.UnregisterInstance(instanceName)
		return fmt.Errorf("port conflict for component '%s': %w", instanceName, err)
	}

	// Register resource usage
	cm.registerPorts(instanceName, comp)

	// Track as managed component
	mc := &component.ManagedComponent{
		Component: comp,
		State:     component.StateCreated,
	}

	// Initialize if supported
	if lifecycle, ok := component.AsLifecycleComponent(comp); ok {
		if err := lifecycle.Initialize(); err != nil {
			// Release the port ownership registered just above (the component
			// never makes it into cm.components, so unregisterPorts-by-name
			// can't find it) and remove from registry on init failure (gh#417).
			cm.unregisterPortsForComp(instanceName, comp)
			cm.registry.UnregisterInstance(instanceName)
			return fmt.Errorf("failed to initialize component '%s': %w", instanceName, err)
		}
		mc.State = component.StateInitialized
	}

	cm.components[instanceName] = mc

	// Invalidate FlowGraph cache when components change
	cm.invalidateFlowGraph()

	return nil
}

// RemoveComponent stops and removes a component instance
func (cm *ComponentManager) RemoveComponent(instanceName string) error {
	if instanceName == "" {
		return fmt.Errorf("instance name cannot be empty")
	}

	cm.mu.Lock()
	defer cm.mu.Unlock()

	// Get the managed component
	mc, exists := cm.components[instanceName]
	if !exists {
		return fmt.Errorf("component '%s' not found", instanceName)
	}

	// Cancel context if running
	if mc.Cancel != nil {
		mc.Cancel()
		// Clean up references to prevent resource leaks
		mc.Cancel = nil
		mc.Context = nil
	}

	// Clear this component's stores from the shared registry before Stop (ADR-063).
	cm.deregisterProvidedStores(instanceName)

	// Stop it if it supports stopping
	if lifecycle, ok := component.AsLifecycleComponent(mc.Component); ok {
		if err := lifecycle.Stop(30 * time.Second); err != nil {
			cm.updateComponentState(instanceName, component.StateFailed, err)
			return fmt.Errorf("failed to stop component '%s': %w", instanceName, err)
		}
	}

	// Unregister ports before removal
	cm.unregisterPorts(instanceName)

	// Remove from tracking
	delete(cm.components, instanceName)

	// Invalidate FlowGraph cache when components change
	cm.invalidateFlowGraph()

	// Remove from start order if present
	for i, name := range cm.startOrder {
		if name == instanceName {
			cm.startOrder = append(cm.startOrder[:i], cm.startOrder[i+1:]...)
			break
		}
	}

	// Remove from registry
	cm.registry.UnregisterInstance(instanceName)
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

// GetManagedComponents returns a copy of all managed components with their state
func (cm *ComponentManager) GetManagedComponents() map[string]*component.ManagedComponent {
	cm.mu.RLock()
	defer cm.mu.RUnlock()

	// Return a copy to prevent external modification
	result := make(map[string]*component.ManagedComponent, len(cm.components))
	for name, mc := range cm.components {
		// Create a copy of the managed component
		result[name] = &component.ManagedComponent{
			Component:  mc.Component,
			State:      mc.State,
			Context:    mc.Context, // Component's individual context
			Cancel:     mc.Cancel,  // Note: this is just a function pointer
			StartOrder: mc.StartOrder,
			LastError:  mc.LastError,
		}
	}

	return result
}

// checkPortConflicts checks for conflicts with existing port registrations
func (cm *ComponentManager) checkPortConflicts(comp component.Discoverable) error {
	allPorts := append(comp.InputPorts(), comp.OutputPorts()...)

	for _, port := range allPorts {
		if port.Config != nil && port.Config.IsExclusive() {
			resourceID := port.Config.ResourceID()
			if owners, exists := cm.resources[resourceID]; exists && len(owners) > 0 {
				return fmt.Errorf("exclusive resource %s already used by %v",
					resourceID, owners)
			}
		}
	}
	return nil
}

// registerPorts registers all ports from a component to track resource usage
func (cm *ComponentManager) registerPorts(name string, comp component.Discoverable) {
	allPorts := append(comp.InputPorts(), comp.OutputPorts()...)

	for _, port := range allPorts {
		if port.Config == nil {
			continue
		}
		resourceID := port.Config.ResourceID()
		cm.resources[resourceID] = append(cm.resources[resourceID], name)
	}
}

// unregisterPorts removes all port registrations for a component still tracked
// in cm.components. Caller must hold cm.mu.
func (cm *ComponentManager) unregisterPorts(name string) {
	mc, exists := cm.components[name]
	if !exists || mc.Component == nil {
		return
	}
	cm.unregisterPortsForComp(name, mc.Component)
}

// unregisterPortsForComp releases cm.resources ownership entries for comp's
// ports. It takes the component directly (rather than looking it up in
// cm.components) so it can also clean up a component that registerPorts already
// recorded but that was never committed to cm.components — e.g. a
// CreateComponent that fails at Initialize. Caller must hold cm.mu.
func (cm *ComponentManager) unregisterPortsForComp(name string, comp component.Discoverable) {
	allPorts := append(comp.InputPorts(), comp.OutputPorts()...)
	for _, port := range allPorts {
		if port.Config == nil {
			continue
		}
		resourceID := port.Config.ResourceID()
		cm.removeFromSlice(resourceID, name)
	}
}

// removeFromSlice removes a component name from the resource owners slice
func (cm *ComponentManager) removeFromSlice(resourceID, name string) {
	owners := cm.resources[resourceID]
	for i, owner := range owners {
		if owner == name {
			cm.resources[resourceID] = append(owners[:i], owners[i+1:]...)
			break
		}
	}

	if len(cm.resources[resourceID]) == 0 {
		delete(cm.resources, resourceID)
	}
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

	// Try to perform detailed health check with timeout to avoid deadlocks
	done := make(chan error, 1)
	go func() {
		done <- cm.performDetailedHealthCheck()
	}()

	select {
	case err := <-done:
		return err
	case <-time.After(100 * time.Millisecond):
		// Timeout - avoid blocking the health check
		// Return healthy if basic checks pass to prevent false alarms during high contention
		return nil
	}
}

// performDetailedHealthCheck performs the actual health check with locks
func (cm *ComponentManager) performDetailedHealthCheck() error {
	// Try to acquire read lock with timeout context
	acquired := make(chan struct{})
	go func() {
		cm.mu.RLock()
		close(acquired)
	}()

	select {
	case <-acquired:
		defer cm.mu.RUnlock()

		// Check for any failed components
		for name, comp := range cm.components {
			if comp.Component == nil {
				return fmt.Errorf("component %s has nil implementation", name)
			}

			// Check if component context is cancelled (indicates failure)
			if comp.Context != nil && comp.Context.Err() != nil {
				return fmt.Errorf("component %s context cancelled: %w", name, comp.Context.Err())
			}
		}

		return nil
	case <-time.After(50 * time.Millisecond):
		// Could not acquire lock quickly - assume healthy to avoid blocking
		return nil
	}
}

// shutdownCallback is called during graceful shutdown
func (cm *ComponentManager) shutdownCallback(ctx context.Context) error {
	// Calculate timeout from context
	var timeout time.Duration
	if deadline, ok := ctx.Deadline(); ok {
		timeout = time.Until(deadline)
		if timeout <= 0 {
			timeout = 5 * time.Second // Default fallback
		}
	} else {
		timeout = 5 * time.Second // Default fallback
	}
	return cm.Stop(timeout)
}

// RegisterComponentStartHook registers a callback for component start events
func (cm *ComponentManager) RegisterComponentStartHook(
	hook func(ctx context.Context, name string, comp component.Discoverable),
) {
	cm.mu.Lock()
	defer cm.mu.Unlock()
	cm.onComponentStart = hook
}

// RegisterComponentStopHook registers a callback for component stop events
func (cm *ComponentManager) RegisterComponentStopHook(hook func(ctx context.Context, name string, reason string)) {
	cm.mu.Lock()
	defer cm.mu.Unlock()
	cm.onComponentStop = hook
}

// RegisterComponentErrorHook registers a callback for component error events
func (cm *ComponentManager) RegisterComponentErrorHook(hook func(ctx context.Context, name string, err error)) {
	cm.mu.Lock()
	defer cm.mu.Unlock()
	cm.onComponentError = hook
}

// WithNATSClient is a functional option to provide NATS client for config watching
// WithNATSClient removed - NATS client now comes from Dependencies

// RegisterHealthChangeHook registers a callback for health change events
func (cm *ComponentManager) RegisterHealthChangeHook(
	hook func(ctx context.Context, name string, healthy bool, details string),
) {
	cm.mu.Lock()
	defer cm.mu.Unlock()
	cm.onHealthChange = hook
}

// handleComponentConfigChange handles dynamic component configuration changes
// watchConfigUpdates monitors for configuration changes from Manager
func (cm *ComponentManager) watchConfigUpdates(ctx context.Context) {
	// First update on both channels is the initial config snapshot emitted
	// by OnChange — we don't want a boot-time restart storm, so drain once
	// per channel before the main loop.
	if cm.modelRegistryUpdates != nil {
		select {
		case <-cm.modelRegistryUpdates:
		default:
		}
	}

	for {
		select {
		case <-cm.shutdown:
			return
		case update, ok := <-cm.configUpdates:
			if !ok {
				// Channel closed
				return
			}

			// Debug logging
			cm.logger.Debug("Received config update",
				"path", update.Path,
				"components_in_config", len(update.Config.Get().Components))

			// Extract component name from path (e.g., "components.udp-sensor")
			parts := strings.Split(update.Path, ".")
			if len(parts) == 2 && parts[0] == "components" {
				componentName := parts[1]

				// Wildcard path "components.*" signals a bulk update (e.g., PushToKV).
				// Reconcile all components against the full config to catch any
				// individual notifications that were dropped.
				if componentName == "*" {
					cm.logger.Debug("Bulk update detected, reconciling components",
						"path", update.Path)
					cm.reconcileComponents(ctx, update.Config)
					continue
				}

				// Get the new config for this component
				fullConfig := update.Config.Get()
				if compConfig, exists := fullConfig.Components[componentName]; exists {
					cm.logger.Debug("Processing component config update",
						"component", componentName,
						"enabled", compConfig.Enabled)
					cm.handleComponentConfigUpdate(ctx, componentName, compConfig)
				} else {
					// Component was removed
					cm.logger.Debug("Component removed from config", "component", componentName)
					cm.handleComponentRemoval(ctx, componentName)
				}
			}

		case update, ok := <-cm.modelRegistryUpdates:
			if !ok {
				// Channel closed
				return
			}
			cm.logger.Debug("model_registry changed, routing to declared dependents",
				"path", update.Path)
			cm.restartDependentsOf(ctx, component.DepModelRegistry, update.Config)

		case <-ctx.Done():
			return
		}
	}
}

// restartDependentsOf restarts every currently-managed component whose
// factory registered with the given runtime dependency. Used to propagate
// top-level KV key changes (currently just model_registry) to components
// that cache derived state at Start time and need a full reconstruction to
// see the new config. Non-dependent components are untouched.
//
// safeConfig provides the latest ComponentConfigs so each restart uses
// the current on-disk component config alongside the refreshed
// ModelRegistry that deps.buildComponentDependencies will pull in.
func (cm *ComponentManager) restartDependentsOf(ctx context.Context, dep string, safeConfig *config.SafeConfig) {
	if safeConfig == nil {
		return
	}
	fullConfig := safeConfig.Get()

	// Snapshot component names under the read lock so we don't hold it
	// during the actual restarts (which take the write lock internally).
	cm.mu.RLock()
	targets := make([]string, 0)
	for name := range cm.components {
		deps := cm.registry.InstanceDependencies(name)
		if slicesContains(deps, dep) {
			targets = append(targets, name)
		}
	}
	cm.mu.RUnlock()

	if len(targets) == 0 {
		cm.logger.Debug("no components declared dependency; nothing to restart",
			"dep", dep)
		return
	}

	cm.logger.Info("restarting components after dep change",
		"dep", dep,
		"components", targets)

	for _, name := range targets {
		compConfig, exists := fullConfig.Components[name]
		if !exists {
			cm.logger.Warn("dependent component has no current config; skipping restart",
				"component", name, "dep", dep)
			continue
		}

		cm.mu.RLock()
		existing := cm.components[name]
		cm.mu.RUnlock()
		if existing == nil {
			// Raced with a concurrent remove — nothing to do.
			continue
		}

		if err := cm.restartComponentWithNewConfig(ctx, name, compConfig, existing); err != nil {
			cm.logger.Error("failed to restart dependent component on dep change",
				"component", name, "dep", dep, "error", err,
				"action", "component_continues_with_old_config")
			// Others still get their shot — no early return.
		}
	}
}

// slicesContains is a tiny local helper to avoid a dep on the slices
// package here; every call site knows the slice is small.
func slicesContains(haystack []string, needle string) bool {
	for _, s := range haystack {
		if s == needle {
			return true
		}
	}
	return false
}

// handleComponentConfigUpdate handles configuration updates for a specific component
func (cm *ComponentManager) handleComponentConfigUpdate(ctx context.Context, name string, cfg types.ComponentConfig) {
	// Check if component exists - need lock for this
	cm.mu.Lock()
	existingComp, exists := cm.components[name]
	cm.mu.Unlock()

	if cfg.Enabled {
		if exists {
			// Component exists - attempt graceful restart with new config
			cm.logger.Debug("Component config update detected",
				"component", name,
				"action", "restart")

			// Don't hold lock while restarting
			if err := cm.restartComponentWithNewConfig(ctx, name, cfg, existingComp); err != nil {
				// Log error but don't fail entire config update
				cm.logger.Error("Failed to restart component with new config",
					"component", name,
					"error", err,
					"action", "component_continues_with_old_config")
				// Component continues running with old config - system remains operational
			}
		} else {
			// New component to create
			cm.logger.Debug("New component configuration detected",
				"component", name,
				"action", "create")

			// Don't hold lock while creating
			if err := cm.createAndStartComponent(ctx, name, cfg); err != nil {
				// Log error but don't fail entire config update
				cm.logger.Error("Failed to create new component",
					"component", name,
					"error", err,
					"action", "will_retry_on_next_config_update")
				// Other components continue - this one can be retried later
			}
		}
	} else if exists {
		// Component should be disabled - graceful shutdown
		cm.logger.Debug("Component disabled via config",
			"component", name,
			"action", "disable")

		if err := cm.stopAndRemoveComponent(ctx, name, existingComp); err != nil {
			// Log error but continue - worst case component keeps running
			cm.logger.Error("Failed to stop component cleanly",
				"component", name,
				"error", err,
				"action", "component_may_continue_running")
		}
	}
}

// handleComponentRemoval handles when a component is removed from configuration
func (cm *ComponentManager) handleComponentRemoval(ctx context.Context, name string) {
	// Check if component exists - need lock for this
	cm.mu.Lock()
	existingComp, exists := cm.components[name]
	cm.mu.Unlock()

	if exists {
		cm.logger.Debug("Component removed from configuration",
			"component", name,
			"action", "remove")

		// Don't hold lock while stopping
		if err := cm.stopAndRemoveComponent(ctx, name, existingComp); err != nil {
			// Log error but continue - worst case component keeps running
			cm.logger.Error("Failed to remove component cleanly",
				"component", name,
				"error", err,
				"action", "component_may_continue_running")
		}
	}
}

// reconcileComponents compares running components against the desired state in
// SafeConfig and corrects any drift. This handles cases where individual KV
// watcher notifications were dropped during bulk updates (e.g., PushToKV writing
// 20+ component configs in rapid succession overflows the buffer-1 subscriber channel).
//
// Reconciliation is conservative: it creates missing enabled components and stops
// running disabled/removed ones, but does NOT restart already-running components
// (individual notifications handle config-change restarts).
//
// IMPORTANT: Must only be called from watchConfigUpdates (single-goroutine consumer).
// The snapshot-then-operate pattern is safe because no concurrent individual
// notifications can interleave within the same consumer goroutine.
func (cm *ComponentManager) reconcileComponents(ctx context.Context, safeConfig *config.SafeConfig) {
	if safeConfig == nil {
		return
	}

	desiredComponents := safeConfig.Get().Components

	// Snapshot current running components under lock
	cm.mu.RLock()
	running := make(map[string]bool, len(cm.components))
	for name := range cm.components {
		running[name] = true
	}
	cm.mu.RUnlock()

	var created, stopped int

	// Phase 1: Create missing enabled components
	for name, cfg := range desiredComponents {
		if !cfg.Enabled {
			continue
		}
		if running[name] {
			continue // Already running — don't restart
		}

		cm.logger.Debug("Reconcile: creating missing component",
			"component", name)

		if err := cm.createAndStartComponent(ctx, name, cfg); err != nil {
			cm.logger.Error("Reconcile: failed to create component",
				"component", name,
				"error", err)
			continue
		}
		created++
	}

	// Phase 2: Stop components that are disabled or removed from config
	for name := range running {
		cfg, inConfig := desiredComponents[name]
		if inConfig && cfg.Enabled {
			continue // Should be running
		}

		cm.mu.RLock()
		existingComp := cm.components[name]
		cm.mu.RUnlock()

		if existingComp == nil {
			continue // Already removed by another goroutine
		}

		if !inConfig {
			cm.logger.Debug("Reconcile: removing orphaned component",
				"component", name)
		} else {
			cm.logger.Debug("Reconcile: stopping disabled component",
				"component", name)
		}

		if err := cm.stopAndRemoveComponent(ctx, name, existingComp); err != nil {
			cm.logger.Error("Reconcile: failed to stop component",
				"component", name,
				"error", err)
		}
		stopped++
	}

	if created > 0 || stopped > 0 {
		cm.logger.Info("Reconciliation complete",
			"created", created,
			"stopped", stopped)
	}
}

// restartComponentWithNewConfig gracefully restarts a component with new configuration
func (cm *ComponentManager) restartComponentWithNewConfig(
	ctx context.Context, name string, cfg types.ComponentConfig, existingComp *component.ManagedComponent,
) error {
	// Check for nil component
	if existingComp == nil {
		return fmt.Errorf("cannot restart component %s: component not found", name)
	}

	// Deregister the OLD instance's stores before Stop closes them, so the
	// new instance can re-register the same StorageInstance without colliding
	// (ADR-063: this is what makes reconfig swap the live handle).
	cm.deregisterProvidedStores(name)

	// Step 1: Gracefully stop the existing component
	if lifecycle, ok := component.AsLifecycleComponent(existingComp.Component); ok {
		if err := lifecycle.Stop(30 * time.Second); err != nil {
			return fmt.Errorf("failed to stop existing component: %w", err)
		}
	}

	// Step 2: Cancel the component's context
	if existingComp.Cancel != nil {
		existingComp.Cancel()
	}

	// Step 3: Remove from tracking and unregister from registry
	cm.mu.Lock()
	cm.unregisterPorts(name) // free exclusive port ownership (gh#417)
	delete(cm.components, name)
	cm.removeFromStartOrder(name)
	cm.mu.Unlock()

	// Unregister from registry to allow re-registration (thread-safe)
	cm.registry.UnregisterInstance(name)

	// Step 4: Create new component with new config
	deps := cm.buildComponentDependencies()
	if err := cm.CreateComponent(ctx, name, cfg, deps); err != nil {
		return fmt.Errorf("failed to create component with new config: %w", err)
	}

	// Step 5: Start the new component if the system is running
	if cm.started.Load() {
		if err := cm.startSingleComponent(ctx, name); err != nil {
			return fmt.Errorf("failed to start restarted component: %w", err)
		}
	}

	// Step 6: Invalidate FlowGraph cache (always safe to do)
	cm.invalidateFlowGraph()

	cm.logger.Debug("Component successfully restarted with new config",
		"component", name)
	return nil
}

// createAndStartComponent creates and optionally starts a new component
func (cm *ComponentManager) createAndStartComponent(ctx context.Context, name string, cfg types.ComponentConfig) error {
	// Step 1: Create the component
	deps := cm.buildComponentDependencies()
	if err := cm.CreateComponent(ctx, name, cfg, deps); err != nil {
		return fmt.Errorf("failed to create component: %w", err)
	}

	// Step 2: Start the component if the system is running
	if cm.started.Load() {
		if err := cm.startSingleComponent(ctx, name); err != nil {
			// If start fails, remove the component to keep state clean
			cm.mu.Lock()
			if mc, exists := cm.components[name]; exists {
				cm.unregisterPorts(name) // free exclusive port ownership (gh#417)
				delete(cm.components, name)
				cm.removeFromStartOrder(name)
				if mc.Cancel != nil {
					mc.Cancel()
				}
			}
			cm.mu.Unlock()
			return fmt.Errorf("failed to start new component: %w", err)
		}
	}

	// Step 3: Invalidate FlowGraph cache
	cm.invalidateFlowGraph()

	cm.logger.Debug("Component successfully created and started",
		"component", name)
	return nil
}

// stopAndRemoveComponent gracefully stops and removes a component
func (cm *ComponentManager) stopAndRemoveComponent(
	ctx context.Context, name string, existingComp *component.ManagedComponent,
) error {
	// Check for cancellation before stopping
	select {
	case <-ctx.Done():
		return ctx.Err()
	default:
	}

	// Check for nil component
	if existingComp == nil {
		return fmt.Errorf("cannot stop component %s: component not found", name)
	}

	// Clear this component's stores from the shared registry before Stop (ADR-063).
	cm.deregisterProvidedStores(name)

	// Step 1: Gracefully stop the component
	if lifecycle, ok := component.AsLifecycleComponent(existingComp.Component); ok {
		if err := lifecycle.Stop(30 * time.Second); err != nil {
			cm.logger.Warn("Component stop returned error, continuing with removal",
				"component", name,
				"error", err)
			// Continue with removal even if stop failed
		}
	}

	// Step 2: Cancel the component's context
	if existingComp.Cancel != nil {
		existingComp.Cancel()
	}

	// Step 3: Remove from tracking and unregister from registry
	cm.mu.Lock()
	cm.unregisterPorts(name) // free exclusive port ownership (gh#417)
	delete(cm.components, name)
	cm.removeFromStartOrder(name)
	cm.mu.Unlock()

	// Unregister from registry (thread-safe, has its own lock)
	cm.registry.UnregisterInstance(name)

	// Step 4: Invalidate FlowGraph cache
	cm.invalidateFlowGraph()

	cm.logger.Debug("Component successfully stopped and removed",
		"component", name)
	return nil
}

// removeFromStartOrder removes a component from the start order slice
func (cm *ComponentManager) removeFromStartOrder(name string) {
	for i, n := range cm.startOrder {
		if n == name {
			// Remove from slice
			cm.startOrder = append(cm.startOrder[:i], cm.startOrder[i+1:]...)
			break
		}
	}
}

// startSingleComponent starts a single component (assumes it's already created).
//
// Concurrency: cm.components + cm.startOrder reads/writes are guarded by
// cm.mu. The actual component.Start runs in a detached goroutine launched
// without the lock so retry loops and slow Start methods don't serialize
// the whole manager.
func (cm *ComponentManager) startSingleComponent(ctx context.Context, name string) error {
	cm.mu.Lock()
	mc, exists := cm.components[name]
	if !exists {
		cm.mu.Unlock()
		return fmt.Errorf("component %s not found", name)
	}

	lifecycle, ok := component.AsLifecycleComponent(mc.Component)
	if !ok {
		// Component doesn't have lifecycle - nothing to start
		cm.mu.Unlock()
		return nil
	}

	// Create child context for this component
	childCtx, cancel := context.WithCancel(ctx)
	mc.Context = childCtx
	mc.Cancel = cancel

	// Update tracking state under lock so a concurrent stopAllComponents
	// sees a consistent startOrder + per-mc StartOrder. The goroutine
	// spawn below runs without the lock.
	mc.StartOrder = len(cm.startOrder)
	cm.startOrder = append(cm.startOrder, name)
	cm.mu.Unlock()

	// Start the component in a goroutine for non-blocking operation
	cm.wg.Add(1)
	go func() {
		defer cm.wg.Done()

		// Use retry for component startup to handle transient failures
		// Components may fail to start due to dependencies not being ready
		retryConfig := retry.Quick() // 10 attempts over ~1 second
		startErr := retry.Do(mc.Context, retryConfig, func() error {
			if err := lifecycle.Start(mc.Context); err != nil {
				cm.logger.Debug("Component start attempt failed, will retry",
					"component", name,
					"error", err)
				return err
			}
			return nil
		})

		if startErr != nil {
			// Update component state but don't fail the entire system
			cm.updateComponentState(name, component.StateFailed, startErr)

			// Call error hook if registered
			if cm.onComponentError != nil {
				cm.onComponentError(mc.Context, name, startErr)
			}

			cm.logger.Error("Component start failed after retries",
				"component", name,
				"error", startErr)
			return
		}

		// Update component state
		cm.updateComponentState(name, component.StateStarted, nil)
		cm.registerProvidedStores(name, mc.Component)

		// Call start hook if registered
		if cm.onComponentStart != nil {
			cm.onComponentStart(mc.Context, name, mc.Component)
		}

		cm.logger.Debug("Component started successfully",
			"component", name)
	}()

	return nil
}

// CreateComponentsFromConfig creates and initializes components based on configuration
func (cm *ComponentManager) CreateComponentsFromConfig(ctx context.Context, cfg *config.Config) error {
	if cfg == nil || cfg.Components == nil {
		return nil
	}

	// Create components from the config map
	for instanceName, componentConfig := range cfg.Components {
		// Skip disabled components
		if !componentConfig.Enabled {
			continue
		}

		// Build dependencies for the component
		deps := cm.buildComponentDependencies()

		// Create the component
		if err := cm.CreateComponent(ctx, instanceName, componentConfig, deps); err != nil {
			slog.Error("Failed to create component from config",
				"instance", instanceName,
				"factory", componentConfig.Name,
				"type", componentConfig.Type,
				"error", err)
			// Continue with other components
			continue
		}

		slog.Debug("Component created from config",
			"instance", instanceName,
			"factory", componentConfig.Name,
			"type", componentConfig.Type)
	}

	return nil
}

// buildComponentDependencies creates Dependencies from ComponentManager's context
func (cm *ComponentManager) buildComponentDependencies() component.Dependencies {
	// Get current security configuration and model registry
	var securityCfg security.Config
	var modelReg model.RegistryReader
	if cm.configManager != nil {
		fullConfig := cm.configManager.GetConfig()
		if fullConfig != nil {
			cfg := fullConfig.Get()
			securityCfg = cfg.Security
			if cfg.ModelRegistry != nil {
				modelReg = cfg.ModelRegistry
			}
		}
	}

	deps := component.Dependencies{
		NATSClient:      cm.natsClient,
		MetricsRegistry: cm.BaseService.metricsRegistry,
		Logger:          cm.BaseService.logger,
		Platform: component.PlatformMeta{
			Org:      cm.platform.Org,
			Platform: cm.platform.Platform,
		},
		Security:          securityCfg,
		ModelRegistry:     modelReg,
		ToolRegistry:      cm.toolRegistry,
		PayloadRegistry:   cm.payloadRegistry,
		ComponentRegistry: cm.registry,
		LifecycleManager:  cm.lifecycleManager,
		StoreRegistry:     cm.storeRegistry,
	}

	return deps
}

// registerProvidedStores registers a just-started component's provided stores
// into the shared StoreRegistry (ADR-063). Called after a component reaches
// StateStarted. A duplicate-ownership collision (two live components claiming the
// same StorageInstance) is logged loudly and skipped rather than clobbering the
// incumbent. The component call happens OUTSIDE any manager lock; only the
// storeProvided tracking map is guarded (storeMu).
func (cm *ComponentManager) registerProvidedStores(name string, comp component.Discoverable) {
	if cm.storeRegistry == nil {
		return
	}
	provider, ok := comp.(component.StoreProvider)
	if !ok {
		return
	}
	// Liveness guard: skip if a concurrent stop/reconfig already removed or
	// halted this component between Start and here. Without it, a reconfig that
	// deregistered the old instance could be shadowed by this late register,
	// leaving the registry pointing at a store the teardown is closing (ADR-063
	// late-register window). Read under cm.mu (register otherwise holds no cm
	// lock, so cm.mu stays outermost — no ordering inversion). A vanishingly
	// small TOCTOU remains after this check; it is self-healing (the stale
	// handle's next fetch errors loudly via content_resolve_error and per-fetch
	// resolution retries) and is bounded per the lazy no-cache contract.
	cm.mu.RLock()
	mc, tracked := cm.components[name]
	live := tracked && mc.State == component.StateStarted
	cm.mu.RUnlock()
	if !live {
		return
	}
	provided := provider.ProvidedStores()
	if len(provided) == 0 {
		return
	}
	registered := make([]string, 0, len(provided))
	for instance, store := range provided {
		if store == nil || instance == "" {
			continue
		}
		if err := cm.storeRegistry.Register(instance, store); err != nil {
			cm.logger.Error("store registry: refusing duplicate store ownership",
				"component", name, "instance", instance, "error", err)
			continue
		}
		registered = append(registered, instance)
		cm.logger.Debug("store registry: registered store", "component", name, "instance", instance)
	}
	if len(registered) > 0 {
		cm.storeMu.Lock()
		cm.storeProvided[name] = append(cm.storeProvided[name], registered...)
		cm.storeMu.Unlock()
	}
}

// deregisterProvidedStores clears a stopping/removed component's stores from the
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
	defer cm.mu.RUnlock()

	result := make(map[string]ComponentStatus)
	for name, mc := range cm.components {
		status := ComponentStatus{
			Name:      name,
			State:     mc.State,
			LastError: mc.LastError,
		}
		if mc.Component != nil {
			status.Health = mc.Component.Health()
			status.DataFlow = mc.Component.DataFlow()
		}
		result[name] = status
	}
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

// extractComponentPortInfo extracts port information from a component for flow validation
func (cm *ComponentManager) extractComponentPortInfo(comp component.Discoverable) *ComponentPortInfo {
	metadata := comp.Meta()

	portInfo := &ComponentPortInfo{
		ComponentName: metadata.Name,
		InputPorts:    []ComponentPortDetail{},
		OutputPorts:   []ComponentPortDetail{},
	}

	// Extract input ports
	for _, port := range comp.InputPorts() {
		detail := cm.extractPortDetail(port)
		if detail != nil {
			portInfo.InputPorts = append(portInfo.InputPorts, *detail)
		}
	}

	// Extract output ports
	for _, port := range comp.OutputPorts() {
		detail := cm.extractPortDetail(port)
		if detail != nil {
			portInfo.OutputPorts = append(portInfo.OutputPorts, *detail)
		}
	}

	return portInfo
}

// extractPortDetail extracts subject and type information from a port
func (cm *ComponentManager) extractPortDetail(port component.Port) *ComponentPortDetail {
	detail := &ComponentPortDetail{
		Name:      port.Name,
		Direction: port.Direction,
		Subject:   "",
		PortType:  "",
	}

	// Extract subject based on port type
	switch portCfg := port.Config.(type) {
	case component.NATSPort:
		detail.Subject = portCfg.Subject
		detail.PortType = "nats"
	case component.NATSRequestPort:
		detail.Subject = portCfg.Subject
		detail.PortType = "nats-request"
	default:
		// For now, only handle NATS ports (simple implementation)
		return nil
	}

	return detail
}

// analyzeFlowConnections identifies connections between components based on subject matching
func (cm *ComponentManager) analyzeFlowConnections(components []component.Discoverable) []FlowConnection {
	var connections []FlowConnection

	// Build lists of publishers and subscribers
	var publishers []publisherInfo
	var subscribers []subscriberInfo

	for _, comp := range components {
		portInfo := cm.extractComponentPortInfo(comp)

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

	return connections
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

// GetFlowGraph returns the current FlowGraph, using cache if valid
func (cm *ComponentManager) GetFlowGraph() *flowgraph.FlowGraph {
	// Check cache validity under read lock
	cm.graphCache.mu.RLock()
	if cm.graphCache.cacheValid && cm.graphCache.currentGraph != nil {
		graph := cm.graphCache.currentGraph
		cm.graphCache.mu.RUnlock()
		return graph
	}
	cm.graphCache.mu.RUnlock()

	// Need to rebuild graph - acquire write lock
	cm.graphCache.mu.Lock()
	defer cm.graphCache.mu.Unlock()

	// Double-check after acquiring write lock
	if cm.graphCache.cacheValid && cm.graphCache.currentGraph != nil {
		return cm.graphCache.currentGraph
	}

	// Build new graph
	graph := cm.buildFlowGraph()

	// Update cache
	cm.graphCache.currentGraph = graph
	cm.graphCache.cacheValid = true
	cm.graphCache.lastUpdate = time.Now()

	return graph
}

// buildFlowGraph creates a new FlowGraph from current components
func (cm *ComponentManager) buildFlowGraph() *flowgraph.FlowGraph {
	cm.mu.RLock()
	defer cm.mu.RUnlock()

	graph := flowgraph.NewFlowGraph()

	// Phase 1: Add all components as nodes
	for name, mc := range cm.components {
		if mc.Component != nil {
			err := graph.AddComponentNode(name, mc.Component)
			if err != nil {
				cm.logger.Warn("Failed to add component to FlowGraph",
					"component", name, "error", err)
				continue
			}
		}
	}

	// Phase 2: Build edges by matching connection patterns
	err := graph.ConnectComponentsByPatterns()
	if err != nil {
		cm.logger.Error("Failed to connect components in FlowGraph", "error", err)
	}

	return graph
}

// invalidateFlowGraph marks the cached FlowGraph as invalid
func (cm *ComponentManager) invalidateFlowGraph() {
	cm.graphCache.mu.Lock()
	defer cm.graphCache.mu.Unlock()

	cm.graphCache.cacheValid = false
	cm.graphCache.currentGraph = nil
	cm.graphCache.lastAnalysis = nil
}

// ValidateFlowConnectivity performs FlowGraph connectivity analysis with caching
func (cm *ComponentManager) ValidateFlowConnectivity() *flowgraph.FlowAnalysisResult {
	// Check if we have a cached analysis
	cm.graphCache.mu.RLock()
	if cm.graphCache.cacheValid && cm.graphCache.lastAnalysis != nil {
		analysis := cm.graphCache.lastAnalysis
		cm.graphCache.mu.RUnlock()
		return analysis
	}
	cm.graphCache.mu.RUnlock()

	// Get graph (may trigger rebuild)
	graph := cm.GetFlowGraph()

	// Perform analysis
	analysis := graph.AnalyzeConnectivity()

	// Cache the analysis result
	cm.graphCache.mu.Lock()
	cm.graphCache.lastAnalysis = analysis
	cm.graphCache.mu.Unlock()

	return analysis
}

// GetFlowPaths returns data paths from input components to all reachable components
func (cm *ComponentManager) GetFlowPaths() map[string][]string {
	graph := cm.GetFlowGraph()

	paths := make(map[string][]string)

	// Find all input components (components with no input ports or external input ports)
	inputComponents := cm.findInputComponents(graph)

	for _, inputComponent := range inputComponents {
		// Use graph traversal to find all reachable components
		reachable := cm.depthFirstTraversal(graph, inputComponent)
		paths[inputComponent] = reachable
	}

	return paths
}

// DetectObjectStoreGaps identifies disconnected storage components
func (cm *ComponentManager) DetectObjectStoreGaps() []ComponentGap {
	graph := cm.GetFlowGraph()
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

	return gaps
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
		if port.Pattern == flowgraph.PatternNetwork || port.Pattern == flowgraph.PatternHTTPClient {
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

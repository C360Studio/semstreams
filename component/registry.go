package component

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"math"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/nats-io/nats.go/jetstream"

	"github.com/c360studio/semstreams/internal/componentadmission"
	"github.com/c360studio/semstreams/natsclient"
	"github.com/c360studio/semstreams/pkg/errs"
	"github.com/c360studio/semstreams/types"
)

// Info holds metadata about an available component type
type Info struct {
	Type        string `json:"type"`        // "input", "processor", "output", "storage"
	Protocol    string `json:"protocol"`    // Technical protocol (udp, tcp, mavlink, etc.)
	Domain      string `json:"domain"`      // Business domain (robotics, semantic, network, storage)
	Description string `json:"description"` // Human-readable description
	Version     string `json:"version"`     // Component version
}

// Factory creates a component instance from configuration following service pattern
// The factory function receives raw JSON configuration and dependencies, parses its own config,
// and returns a properly initialized component that implements the Discoverable interface.
// All I/O operations should be performed in the component's Start() method, not in the factory.
// This pattern matches service constructors: func(rawConfig json.RawMessage, deps Dependencies) (Service, error)
type Factory func(rawConfig json.RawMessage, deps Dependencies) (Discoverable, error)

// Dependency identifiers used by Registration.Dependencies. Components
// declare these at registration to opt into framework-driven behavior
// (e.g., restart when a named runtime dependency changes). Kept as
// typed constants so refactors are compiler-checked.
const (
	// DepModelRegistry signals that a component consumes
	// Dependencies.ModelRegistry at construction or run time. Components
	// that declare this are restarted by ComponentManager when the
	// model_registry KV key changes, so they rebuild any registry-
	// derived state (LLM clients, embedder clients, summarizers) against
	// the new config.
	DepModelRegistry = "model-registry"
)

// Registration holds factory and metadata for a component type
type Registration struct {
	Name         string       `json:"name"`         // Factory name (e.g., "udp-input")
	Type         string       `json:"type"`         // Component type (input/processor/output/storage)
	Protocol     string       `json:"protocol"`     // Technical protocol (udp, mavlink, websocket, etc.)
	Domain       string       `json:"domain"`       // Business domain (robotics, semantic, network, storage)
	Description  string       `json:"description"`  // Human-readable description
	Version      string       `json:"version"`      // Component version
	Schema       ConfigSchema `json:"schema"`       // Schema as static metadata (Feature 011)
	Factory      Factory      `json:"-"`            // Factory function (not serializable)
	Dependencies []string     `json:"dependencies"` // Runtime dependencies (e.g., DepModelRegistry) — used by ComponentManager for restart routing
}

// RegistrationConfig provides a clean API for component registration.
// This config struct replaces the previous 7-8 parameter function signatures.
// It maps 1:1 to Registration struct fields for simplicity.
type RegistrationConfig struct {
	Name         string       // Component name (e.g., "udp", "websocket", "graph-processor")
	Factory      Factory      // Factory function to create component instances
	Schema       ConfigSchema // Configuration schema for validation and discovery
	Type         string       // Component type: "input", "processor", "output", "storage"
	Protocol     string       // Technical protocol (udp, tcp, websocket, file, etc.)
	Domain       string       // Business domain (network, storage, processing, robotics, semantic)
	Description  string       // Human-readable description of the component
	Version      string       // Component version (semver recommended)
	Dependencies []string     // Runtime deps declared via constants like DepModelRegistry
}

// CapabilityAnnouncement is published to NATS when components register.
type CapabilityAnnouncement struct {
	InstanceName string           `json:"instance_name"`
	Component    string           `json:"component"`
	Type         string           `json:"type"`
	Version      string           `json:"version"`
	InputPorts   []PortCapability `json:"input_ports,omitempty"`
	OutputPorts  []PortCapability `json:"output_ports,omitempty"`
	Timestamp    time.Time        `json:"timestamp"`
	TTL          time.Duration    `json:"ttl"`
	NodeID       string           `json:"node_id"`
}

// PortCapability describes an input or output port for discovery.
type PortCapability struct {
	Name        string `json:"name"`
	Subject     string `json:"subject"`
	Type        string `json:"type"`
	Interface   string `json:"interface,omitempty"`
	Description string `json:"description,omitempty"`
}

// componentGeneration is the immutable declaration captured for one admitted
// component generation. It describes admitted shape only; it carries no
// lifecycle, health, readiness, grouping, or orchestration state.
type componentGeneration struct {
	InstanceName       string
	FactoryIdentity    string
	InputPorts         []Port
	OutputPorts        []Port
	InputFacts         []PortFacts
	OutputFacts        []PortFacts
	ExclusiveResources []string
	Generation         uint64
}

// preparedComponent exists only between factory construction and boot
// admission. The Registry retains the immutable declaration, never the live
// component handle.
type preparedComponent struct {
	declaration componentGeneration
	component   Discoverable
}

// generationSnapshot is the framework-internal defensive read view of one
// admitted generation. Registry read methods require the root internal access
// token; downstream adopters cannot obtain this otherwise-unexported type.
type generationSnapshot struct {
	record componentGeneration
}

func (s generationSnapshot) Name() string    { return s.record.InstanceName }
func (s generationSnapshot) Factory() string { return s.record.FactoryIdentity }
func (s generationSnapshot) ID() uint64      { return s.record.Generation }

func (s generationSnapshot) Inputs() []Port {
	return cloneResolvedPorts(s.record.InputPorts)
}

func (s generationSnapshot) Outputs() []Port {
	return cloneResolvedPorts(s.record.OutputPorts)
}

func (s generationSnapshot) InputDeclarationFacts() []PortFacts {
	return clonePortFactsSlice(s.record.InputFacts)
}

func (s generationSnapshot) OutputDeclarationFacts() []PortFacts {
	return clonePortFactsSlice(s.record.OutputFacts)
}

type generationObserver struct {
	pending chan []componentGeneration
}

// Registry manages component factories and instances
// It provides thread-safe registration and lookup of both factories (for creation)
// and instances (for discovery and management).
type Registry struct {
	factories   map[string]*Registration       // Factory registry by name
	generations map[string]componentGeneration // Admitted generation by instance name
	mu          sync.RWMutex                   // Protects all registry state

	nextGeneration uint64
	sealed         bool
	nextObserverID uint64
	observers      map[uint64]generationObserver

	// NATS-backed capability discovery (new)
	remoteCapabilities map[string]*CapabilityAnnouncement
	nodeID             string
	natsClient         *natsclient.Client // NATS client for capability operations
	heartbeatCancel    context.CancelFunc // Cancel heartbeat goroutine
	logger             *slog.Logger       // Logger for non-fatal operations
}

// NewRegistry creates a new empty component registry
// Optionally accepts a logger; defaults to slog.Default() if none provided.
// This maintains backwards compatibility with existing callers.
func NewRegistry(opts ...func(*Registry)) *Registry {
	r := &Registry{
		factories:   make(map[string]*Registration),
		generations: make(map[string]componentGeneration),
		observers:   make(map[uint64]generationObserver),
		logger:      slog.Default(),
	}

	// Apply optional configuration
	for _, opt := range opts {
		opt(r)
	}

	return r
}

// WithLogger sets a custom logger for the registry
func WithLogger(logger *slog.Logger) func(*Registry) {
	return func(r *Registry) {
		if logger != nil {
			r.logger = logger
		}
	}
}

// RegisterFactory registers a component factory with the given name
// Returns an error if a factory with the same name is already registered.
func (r *Registry) RegisterFactory(name string, registration *Registration) error {
	if name == "" {
		return errs.WrapInvalid(errs.ErrInvalidConfig, "Registry", "RegisterFactory", "factory name validation")
	}
	if registration == nil {
		return errs.WrapInvalid(errs.ErrInvalidConfig, "Registry", "RegisterFactory", "registration validation")
	}
	if registration.Factory == nil {
		return errs.WrapInvalid(errs.ErrInvalidConfig, "Registry", "RegisterFactory", "factory function validation")
	}
	if registration.Type == "" {
		return errs.WrapInvalid(errs.ErrInvalidConfig, "Registry", "RegisterFactory", "component type validation")
	}

	r.mu.Lock()
	defer r.mu.Unlock()

	if _, exists := r.factories[name]; exists {
		msg := fmt.Errorf("factory '%s' is already registered", name)
		return errs.WrapInvalid(msg, "Registry", "RegisterFactory", "duplicate factory check")
	}

	r.factories[name] = registration
	return nil
}

// CreateComponent is the sole framework-internal boot-admission seam.
// prepare must finish all fallible owner-local setup before the Registry
// publishes the immutable declaration. Downstream adopters cannot obtain the
// internal access token and configure components through ComponentManager.
func (r *Registry) CreateComponent(
	_ componentadmission.Access,
	instanceName string,
	config types.ComponentConfig,
	deps Dependencies,
	prepare func(Discoverable) error,
) (Discoverable, error) {
	return r.createComponent(instanceName, config, deps, prepare)
}

func (r *Registry) createComponent(
	instanceName string,
	config types.ComponentConfig,
	deps Dependencies,
	prepare func(Discoverable) error,
) (Discoverable, error) {
	r.mu.RLock()
	sealed := r.sealed
	r.mu.RUnlock()
	if sealed {
		return nil, errs.WrapInvalid(
			errs.ErrInvalidConfig,
			"Registry", "CreateComponent", "component composition is sealed for this process",
		)
	}
	prepared, err := r.prepareComponent("CreateComponent", instanceName, config, deps)
	if err != nil {
		return nil, err
	}
	if prepare != nil {
		if err := prepare(prepared.component); err != nil {
			return nil, errs.Wrap(err, "Registry", "CreateComponent", "prepare managed component")
		}
	}
	if err := r.admitPrepared(prepared.declaration); err != nil {
		return nil, errs.Wrap(err, "Registry", "CreateComponent", "instance registration")
	}
	r.publishGenerationCapabilities(prepared.declaration)
	return prepared.component, nil
}

func (r *Registry) prepareComponent(
	operation, instanceName string, config types.ComponentConfig, deps Dependencies,
) (preparedComponent, error) {
	// Security: Validate instance name
	if err := ValidateComponentName(instanceName); err != nil {
		return preparedComponent{}, errs.Wrap(err, "Registry", operation, "instance name validation")
	}
	if !config.Enabled {
		return preparedComponent{}, errs.WrapInvalid(
			errs.ErrInvalidConfig, "Registry", operation, "disabled component admission")
	}
	if config.Type == "" {
		return preparedComponent{}, errs.WrapInvalid(
			errs.ErrInvalidConfig, "Registry", operation, "component type validation")
	}
	// Security: Validate factory name
	if err := ValidateComponentName(config.Name); err != nil {
		return preparedComponent{}, errs.Wrap(err, "Registry", operation, "factory name validation")
	}
	if deps.NATSClient == nil {
		return preparedComponent{}, errs.WrapInvalid(errs.ErrInvalidConfig, "Registry", operation, "NATS client validation")
	}

	// CRITICAL SECURITY: Comprehensive validation before factory execution
	// This prevents injection attacks, resource exhaustion, and malformed input
	if err := ValidateFactoryConfig(config.Config); err != nil {
		return preparedComponent{}, errs.Wrap(err, "Registry", operation, "config security validation")
	}

	// Look up factory by the component/factory name (e.g., "udp", "websocket")
	r.mu.RLock()
	registration, exists := r.factories[config.Name]
	r.mu.RUnlock()

	if !exists {
		msg := fmt.Errorf("unknown component factory '%s'", config.Name)
		return preparedComponent{}, errs.WrapInvalid(msg, "Registry", operation, "factory lookup")
	}

	// Validate that the factory type matches the requested type
	if registration.Type != string(config.Type) {
		msg := fmt.Errorf("component '%s' is type '%s', not '%s'",
			config.Name, registration.Type, config.Type)
		return preparedComponent{}, errs.WrapInvalid(msg, "Registry", operation, "type validation")
	}

	// Create the component using the factory with service pattern
	// Pass the component-specific config (config.Config) to the factory
	component, err := registration.Factory(config.Config, deps)
	if err != nil {
		return preparedComponent{}, errs.Wrap(err, "Registry", operation, "factory execution")
	}

	// Defensive check: factory should never return (nil, nil)
	if component == nil {
		return preparedComponent{}, errs.WrapInvalid(errs.ErrInvalidConfig, "Registry", operation,
			"factory returned nil component without error")
	}

	prepared, err := captureComponentGeneration(instanceName, config.Name, component)
	if err != nil {
		return preparedComponent{}, errs.Wrap(err, "Registry", operation, "capture declaration")
	}
	return preparedComponent{declaration: prepared, component: component}, nil
}

func (r *Registry) admitPrepared(prepared componentGeneration) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.sealed {
		return errs.WrapInvalid(
			errs.ErrInvalidConfig,
			"Registry", "admitPrepared", "component composition is sealed for this process",
		)
	}

	_, exists := r.generations[prepared.InstanceName]
	if exists {
		return errs.WrapInvalid(
			fmt.Errorf("instance '%s' is already registered", prepared.InstanceName),
			"Registry", "admitPrepared", "duplicate instance check")
	}
	if err := r.checkResourceConflictsLocked(prepared.InstanceName, prepared.ExclusiveResources); err != nil {
		return errs.Wrap(err, "Registry", "admitPrepared", "resource conflict check")
	}
	r.nextGeneration++
	prepared.Generation = r.nextGeneration
	r.generations[prepared.InstanceName] = prepared
	r.notifyObserversLocked()
	return nil
}

// SealComposition closes boot admission for the current process. The internal
// access token prevents downstream callers from treating the seal as a public
// runtime-composition control.
func (r *Registry) SealComposition(_ componentadmission.Access) {
	r.mu.Lock()
	r.sealed = true
	r.mu.Unlock()
}

// GetComponentSchema retrieves a component's schema directly from Registration metadata
// This method retrieves schemas without component instantiation (Feature 011 - Option 1)
// Schema is stored as static metadata during registration, avoiding dependency validation issues
func (r *Registry) GetComponentSchema(name string) (ConfigSchema, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()

	// Look up by factory name (same as component type)
	registration, exists := r.factories[name]
	if !exists {
		return ConfigSchema{}, errs.WrapInvalid(
			fmt.Errorf("component type %q not found", name),
			"Registry", "GetComponentSchema", "type lookup")
	}

	// Return schema directly from Registration metadata (no instantiation needed)
	return registration.Schema, nil
}

// GetComponent retrieves a component instance by factory type name (for schema retrieval)
// DEPRECATED: Use GetComponentSchema() instead for schema retrieval.
// This method creates a temporary component instance, which fails for components with dependency validation.
func (r *Registry) GetComponent(name string) (Discoverable, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()

	// Look up by factory name (same as component type)
	registration, exists := r.factories[name]
	if !exists {
		return nil, errs.WrapInvalid(
			fmt.Errorf("component type %q not found", name),
			"Registry", "GetComponent", "type lookup")
	}

	// Create a temporary instance just to get the schema
	// ConfigSchema() doesn't perform I/O, so this is safe
	// NOTE: This will fail if factory validates dependencies
	deps := Dependencies{} // Empty deps for schema retrieval
	component, err := registration.Factory(json.RawMessage("{}"), deps)
	if err != nil {
		return nil, errs.Wrap(err, "Registry", "GetComponent", "factory execution")
	}

	return component, nil
}

// ListComponentTypes returns all registered component factory type names
// This returns factory names (e.g., "udp-input", "websocket-output") not instance names
func (r *Registry) ListComponentTypes() []string {
	r.mu.RLock()
	defer r.mu.RUnlock()

	names := make([]string, 0, len(r.factories))
	for name := range r.factories {
		names = append(names, name)
	}

	return names
}

// ListFactories returns all registered component factories
// This provides information about what types of components can be created.
func (r *Registry) ListFactories() map[string]*Registration {
	r.mu.RLock()
	defer r.mu.RUnlock()

	// Return a copy to prevent external modification
	result := make(map[string]*Registration, len(r.factories))
	for name, registration := range r.factories {
		// Create a copy of the registration without the factory function
		// to avoid potential issues with function pointers
		result[name] = &Registration{
			Name:         registration.Name,
			Type:         registration.Type,
			Protocol:     registration.Protocol,
			Domain:       registration.Domain,
			Description:  registration.Description,
			Version:      registration.Version,
			Schema:       registration.Schema,
			Dependencies: registration.Dependencies,
			// Factory is intentionally not copied for safety
		}
	}

	return result
}

// GetFactory returns a specific factory by name
// Unlike ListFactories, this returns the actual Factory function for creating components
func (r *Registry) GetFactory(name string) (Factory, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()

	registration, exists := r.factories[name]
	if !exists {
		return nil, false
	}
	return registration.Factory, true
}

// RegisterWithConfig registers a component using a configuration struct.
// This is the recommended registration method that replaces the multi-parameter functions.
//
// Example usage:
//
//	registry.RegisterWithConfig(component.RegistrationConfig{
//	    Name:        "udp",
//	    Factory:     CreateUDPInput,
//	    Schema:      udpSchema,
//	    Type:        "input",
//	    Protocol:    "udp",
//	    Domain:      "network",
//	    Description: "UDP input component for receiving network data",
//	    Version:     "1.0.0",
//	})
func (r *Registry) RegisterWithConfig(config RegistrationConfig) error {
	registration := &Registration{
		Name:         config.Name,
		Factory:      config.Factory,
		Schema:       config.Schema,
		Type:         config.Type,
		Protocol:     config.Protocol,
		Domain:       config.Domain,
		Description:  config.Description,
		Version:      config.Version,
		Dependencies: config.Dependencies,
	}

	return r.RegisterFactory(config.Name, registration)
}

// InstanceDependencies returns the declared runtime dependencies for a
// running component instance (e.g., []string{DepModelRegistry}). Returns
// nil if the instance is not tracked or its factory didn't declare any
// dependencies.
//
// Used by ComponentManager to route config-change events (e.g., a
// model_registry KV update) to the components that opted in.
func (r *Registry) InstanceDependencies(instanceName string) []string {
	reg := r.getRegistrationForInstance(instanceName)
	if reg == nil {
		return nil
	}
	return reg.Dependencies
}

// ListAvailable returns information about all available component types
// This provides metadata about what types of components can be created.
func (r *Registry) ListAvailable() map[string]Info {
	factories := r.ListFactories()
	result := make(map[string]Info, len(factories))

	for name, registration := range factories {
		result[name] = Info{
			Type:        registration.Type,
			Protocol:    registration.Protocol,
			Domain:      registration.Domain,
			Description: registration.Description,
			Version:     registration.Version,
		}
	}

	return result
}

// Config validation constants - security limits
const (
	MaxStringLength = 1024          // Maximum length for string values
	MaxJSONSize     = 1024 * 1024   // Maximum JSON size (1MB)
	MinPort         = 1             // Minimum valid port number
	MaxPort         = 65535         // Maximum valid port number
	MaxInt          = math.MaxInt32 // Maximum safe integer value
	MinInt          = math.MinInt32 // Minimum safe integer value
)

// ValidateConfigKey checks if a configuration key is valid
func ValidateConfigKey(key string) error {
	if key == "" {
		return errs.WrapInvalid(errs.ErrInvalidConfig, "ConfigValidator", "ValidateConfigKey", "empty key")
	}
	if len(key) > MaxStringLength {
		return errs.WrapInvalid(errs.ErrInvalidConfig, "ConfigValidator", "ValidateConfigKey", "key too long")
	}
	// Check for potentially dangerous characters
	if strings.ContainsAny(key, "\x00\n\r\t") {
		return errs.WrapInvalid(
			errs.ErrInvalidConfig,
			"ConfigValidator",
			"ValidateConfigKey",
			"invalid key characters",
		)
	}
	return nil
}

// ValidateJSONSize checks if JSON input is within safe limits
func ValidateJSONSize(data json.RawMessage) error {
	if len(data) > MaxJSONSize {
		return errs.WrapInvalid(
			errs.ErrInvalidConfig, "ConfigValidator", "ValidateJSONSize", "JSON too large")
	}
	return nil
}

// ValidateComponentName validates component/instance names for security
func ValidateComponentName(name string) error {
	if name == "" {
		return errs.WrapInvalid(errs.ErrInvalidConfig, "ConfigValidator", "ValidateComponentName", "empty name")
	}
	if len(name) > MaxStringLength {
		return errs.WrapInvalid(errs.ErrInvalidConfig, "ConfigValidator", "ValidateComponentName", "name too long")
	}
	// Check for potentially dangerous characters - allow alphanumeric, dash, underscore , dot
	for _, r := range name {
		if !((r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') ||
			(r >= '0' && r <= '9') || r == '-' || r == '_' || r == '.') {
			return errs.WrapInvalid(
				errs.ErrInvalidConfig, "ConfigValidator", "ValidateComponentName",
				"invalid name characters")
		}
	}
	return nil
}

// ValidatePortNumber validates port numbers are within valid range
func ValidatePortNumber(port int) error {
	if port < MinPort || port > MaxPort {
		msg := fmt.Errorf("port %d outside valid range %d-%d", port, MinPort, MaxPort)
		return errs.WrapInvalid(msg, "ConfigValidator", "ValidatePortNumber",
			"port range validation")
	}
	return nil
}

func (r *Registry) checkResourceConflictsLocked(instanceName string, resources []string) error {
	for existingName, generation := range r.generations {
		if existingName == instanceName {
			continue
		}
		for _, existingResource := range generation.ExclusiveResources {
			for _, resource := range resources {
				if resource == existingResource {
					msg := fmt.Errorf("resource conflict: %s already used by component '%s'", resource, existingName)
					return errs.WrapInvalid(msg, "Registry", "checkResourceConflicts", "exclusive resource check")
				}
			}
		}
	}
	return nil
}

func captureComponentGeneration(
	instanceName, factoryIdentity string, discoverable Discoverable,
) (componentGeneration, error) {
	inputs, inputFacts, err := cloneAndProjectPorts(discoverable.InputPorts())
	if err != nil {
		return componentGeneration{}, err
	}
	outputs, outputFacts, err := cloneAndProjectPorts(discoverable.OutputPorts())
	if err != nil {
		return componentGeneration{}, err
	}
	resources := make([]string, 0, len(inputFacts)+len(outputFacts))
	for _, facts := range append(append([]PortFacts(nil), inputFacts...), outputFacts...) {
		if facts.IsExclusive() {
			resources = append(resources, facts.ResourceID())
		}
	}
	sort.Strings(resources)
	resources = compactStrings(resources)
	return componentGeneration{
		InstanceName:       instanceName,
		FactoryIdentity:    factoryIdentity,
		InputPorts:         inputs,
		OutputPorts:        outputs,
		InputFacts:         inputFacts,
		OutputFacts:        outputFacts,
		ExclusiveResources: resources,
	}, nil
}

func cloneAndProjectPorts(ports []Port) ([]Port, []PortFacts, error) {
	cloned := make([]Port, len(ports))
	facts := make([]PortFacts, len(ports))
	for index, port := range ports {
		resolved, projected, err := resolveAndProjectPort(PortDefinition{
			Name:        port.Name,
			Required:    port.Required,
			Description: port.Description,
			Config:      port.Config,
		}, port.Direction)
		if err != nil {
			return nil, nil, err
		}
		cloned[index] = resolved
		facts[index] = projected
	}
	return cloned, facts, nil
}

func compactStrings(values []string) []string {
	if len(values) < 2 {
		return values
	}
	write := 1
	for read := 1; read < len(values); read++ {
		if values[read] == values[write-1] {
			continue
		}
		values[write] = values[read]
		write++
	}
	return values[:write]
}

// generation returns a defensive clone of one admitted generation for
// package-internal verification. Generation inspection is deliberately not an
// adopter-facing Registry API until a runtime consumer exists.
func (r *Registry) generation(instanceName string) (componentGeneration, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	generation, ok := r.generations[instanceName]
	if !ok {
		return componentGeneration{}, false
	}
	return cloneComponentGeneration(generation), true
}

// generationsSnapshot returns a deterministic defensive clone of the complete current
// admission set.
func (r *Registry) generationsSnapshot() []componentGeneration {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.generationsLocked()
}

// Snapshot returns one defensive admitted-generation view to a root-internal
// framework consumer.
func (r *Registry) Snapshot(
	_ componentadmission.Access, instanceName string,
) (
	//revive:disable-next-line:unexported-return Framework callers consume this internal-token-gated opaque snapshot.
	generationSnapshot,
	bool,
) {
	generation, ok := r.generation(instanceName)
	if !ok {
		return generationSnapshot{}, false
	}
	return generationSnapshot{record: generation}, true
}

// Snapshots returns a deterministic defensive complete admission set to a
// root-internal framework consumer.
//
//revive:disable:unexported-return Framework callers range this internal-token-gated opaque snapshot set.
func (r *Registry) Snapshots(
	_ componentadmission.Access,
) []generationSnapshot {
	generations := r.generationsSnapshot()
	result := make([]generationSnapshot, len(generations))
	for index, generation := range generations {
		result[index] = generationSnapshot{record: generation}
	}
	return result
}

// ObserveSnapshots delivers latest-state defensive complete admission sets to
// a root-internal framework consumer.
func (r *Registry) ObserveSnapshots(
	ctx context.Context, _ componentadmission.Access,
) <-chan []generationSnapshot {
	return observeRegistryState(ctx, r, func(generations []componentGeneration) []generationSnapshot {
		snapshots := make([]generationSnapshot, len(generations))
		for index, generation := range generations {
			snapshots[index] = generationSnapshot{record: generation}
		}
		return snapshots
	})
}

//revive:enable:unexported-return

func observeRegistryState[T any](
	ctx context.Context, r *Registry, project func([]componentGeneration) T,
) <-chan T {
	updates := make(chan T)
	pending := make(chan []componentGeneration, 1)
	r.mu.Lock()
	r.nextObserverID++
	id := r.nextObserverID
	initial := r.generationsLocked()
	r.observers[id] = generationObserver{pending: pending}
	r.mu.Unlock()

	go func(observerCtx context.Context) {
		defer close(updates)
		defer func() {
			r.mu.Lock()
			delete(r.observers, id)
			r.mu.Unlock()
		}()

		select {
		case updates <- project(initial):
		case <-observerCtx.Done():
			return
		}

		for {
			var latest []componentGeneration
			select {
			case latest = <-pending:
			case <-observerCtx.Done():
				return
			}

			for {
			drainPending:
				for {
					select {
					case latest = <-pending:
					default:
						break drainPending
					}
				}
				select {
				case latest = <-pending:
				case updates <- project(latest):
					goto delivered
				case <-observerCtx.Done():
					return
				}
			}
		delivered:
		}
	}(ctx)
	return updates
}

func (r *Registry) generationsLocked() []componentGeneration {
	names := make([]string, 0, len(r.generations))
	for name := range r.generations {
		names = append(names, name)
	}
	sort.Strings(names)
	result := make([]componentGeneration, 0, len(names))
	for _, name := range names {
		result = append(result, cloneComponentGeneration(r.generations[name]))
	}
	return result
}

func (r *Registry) notifyObserversLocked() {
	snapshot := r.generationsLocked()
	for _, observer := range r.observers {
		select {
		case observer.pending <- snapshot:
		default:
			select {
			case <-observer.pending:
			default:
			}
			select {
			case observer.pending <- snapshot:
			default:
			}
		}
	}
}

func cloneComponentGeneration(generation componentGeneration) componentGeneration {
	clone := generation
	clone.InputPorts = cloneResolvedPorts(generation.InputPorts)
	clone.OutputPorts = cloneResolvedPorts(generation.OutputPorts)
	clone.InputFacts = clonePortFactsSlice(generation.InputFacts)
	clone.OutputFacts = clonePortFactsSlice(generation.OutputFacts)
	clone.ExclusiveResources = append([]string(nil), generation.ExclusiveResources...)
	return clone
}

func cloneResolvedPorts(ports []Port) []Port {
	cloned := make([]Port, len(ports))
	for index, port := range ports {
		resolved, _, err := resolveAndProjectPort(PortDefinition{
			Name: port.Name, Required: port.Required, Description: port.Description, Config: port.Config,
		}, port.Direction)
		if err != nil {
			panic(fmt.Sprintf("clone retained port %q: %v", port.Name, err))
		}
		cloned[index] = resolved
	}
	return cloned
}

func clonePortFactsSlice(facts []PortFacts) []PortFacts {
	cloned := make([]PortFacts, len(facts))
	for index, fact := range facts {
		cloned[index] = clonePortFacts(fact)
	}
	return cloned
}

func clonePortFacts(facts PortFacts) PortFacts {
	clone := facts
	clone.interfaceContract = cloneInterfaceContract(facts.interfaceContract)
	clone.connectionIDs = append([]string(nil), facts.connectionIDs...)
	clone.natsSubjects = append([]string(nil), facts.natsSubjects...)
	if facts.stream != nil {
		stream := *facts.stream
		stream.subjects = append([]string(nil), facts.stream.subjects...)
		clone.stream = &stream
	}
	if facts.network != nil {
		network := *facts.network
		clone.network = &network
	}
	return clone
}

// Config helper functions for components

// GetString safely extracts a string value from config with a default fallback and validation
func GetString(config map[string]any, key string, defaultValue string) string {
	// Validate the key first
	if err := ValidateConfigKey(key); err != nil {
		// Log warning but return default to maintain API compatibility
		return defaultValue
	}

	if value, exists := config[key]; exists {
		if str, ok := value.(string); ok {
			// Validate string length for security
			if len(str) > MaxStringLength {
				// Return default for oversized strings
				return defaultValue
			}
			// Sanitize string - remove null bytes and control characters except basic whitespace
			cleaned := strings.Map(func(r rune) rune {
				if r == '\x00' || (r < 32 && r != '\t' && r != '\n' && r != '\r') {
					return -1 // Remove invalid characters
				}
				return r
			}, str)
			return cleaned
		}
	}
	return defaultValue
}

// GetInt safely extracts an integer value from config with a default fallback and bounds checking
func GetInt(config map[string]any, key string, defaultValue int) int {
	// Validate the key first
	if err := ValidateConfigKey(key); err != nil {
		return defaultValue
	}

	if value, exists := config[key]; exists {
		switch v := value.(type) {
		case int:
			// Check bounds for integer overflow protection
			if v < MinInt || v > MaxInt {
				return defaultValue
			}
			return v
		case float64:
			// Check for NaN, Inf, and bounds
			if math.IsNaN(v) || math.IsInf(v, 0) {
				return defaultValue
			}
			// Check if conversion would overflow
			if v < float64(MinInt) || v > float64(MaxInt) {
				return defaultValue
			}
			// Safe conversion
			result := int(v)
			// Double-check the conversion didn't introduce errors
			if float64(result) != v {
				return defaultValue
			}
			return result
		case int64:
			// Check bounds for int64 to int conversion
			if v < int64(MinInt) || v > int64(MaxInt) {
				return defaultValue
			}
			return int(v)
		}
	}
	return defaultValue
}

// GetBool safely extracts a boolean value from config with a default fallback and validation
func GetBool(config map[string]any, key string, defaultValue bool) bool {
	// Validate the key first
	if err := ValidateConfigKey(key); err != nil {
		return defaultValue
	}

	if value, exists := config[key]; exists {
		if b, ok := value.(bool); ok {
			return b
		}
	}
	return defaultValue
}

// GetFloat64 safely extracts a float64 value from config with a default fallback and validation
func GetFloat64(config map[string]any, key string, defaultValue float64) float64 {
	// Validate the key first
	if err := ValidateConfigKey(key); err != nil {
		return defaultValue
	}

	if value, exists := config[key]; exists {
		switch v := value.(type) {
		case float64:
			// Check for NaN and Inf values
			if math.IsNaN(v) || math.IsInf(v, 0) {
				return defaultValue
			}
			return v
		case float32:
			// Check for NaN and Inf values
			if math.IsNaN(float64(v)) || math.IsInf(float64(v), 0) {
				return defaultValue
			}
			return float64(v)
		case int:
			// Safe conversion from int to float64
			if v < MinInt || v > MaxInt {
				return defaultValue
			}
			return float64(v)
		case int64:
			// Check bounds for int64 to float64 conversion
			if v < int64(MinInt) || v > int64(MaxInt) {
				return defaultValue
			}
			return float64(v)
		}
	}
	return defaultValue
}

// Note: Component registration functions have been removed.
// Components now use explicit Register(*Registry) methods for registration.
//
// Payload registration moved to the payloadregistry package
// (alongside PayloadRegistration, PayloadRegistry, the global
// singleton, and Register/Create/Build helpers). Migration is a
// straightforward import-path swap; the surface is preserved.

// matchesPattern checks if subject matches NATS-style pattern with wildcards.
// "*" matches exactly one token, ">" matches one or more tokens (only at end).
// Returns true if subject matches pattern, false otherwise.
// Edge case: both empty returns true.
func (r *Registry) matchesPattern(subject, pattern string) bool {
	// Edge case: both empty
	if subject == "" && pattern == "" {
		return true
	}

	// One empty, one not
	if subject == "" || pattern == "" {
		return false
	}

	subjectTokens := strings.Split(subject, ".")
	patternTokens := strings.Split(pattern, ".")

	// Check for multi-level wildcard (">") - only allowed at end
	if len(patternTokens) > 0 && patternTokens[len(patternTokens)-1] == ">" {
		// Multi-level wildcard must match prefix
		prefixPattern := patternTokens[:len(patternTokens)-1]

		// ">" at root matches everything
		if len(prefixPattern) == 0 {
			return true
		}

		// Subject must have at least as many tokens as prefix
		if len(subjectTokens) < len(prefixPattern) {
			return false
		}

		// Match prefix tokens
		for i, pToken := range prefixPattern {
			if pToken != "*" && pToken != subjectTokens[i] {
				return false
			}
		}
		return true
	}

	// Validate ">" only appears at end (if at all)
	for i, token := range patternTokens {
		if token == ">" && i != len(patternTokens)-1 {
			return false // ">" in middle is invalid
		}
	}

	// Exact token count match required (no multi-level wildcard)
	if len(subjectTokens) != len(patternTokens) {
		return false
	}

	// Match each token
	for i := 0; i < len(subjectTokens); i++ {
		if patternTokens[i] != "*" && patternTokens[i] != subjectTokens[i] {
			return false
		}
	}

	return true
}

// GetCapabilities returns capabilities matching the subject pattern.
// Supports NATS wildcards: "*" matches one token, ">" matches one or more tokens at end.
// Returns empty slice (not nil) when no matches or cache empty.
// Thread-safe for concurrent access.
func (r *Registry) GetCapabilities(subjectPattern string) []*CapabilityAnnouncement {
	r.mu.RLock()
	defer r.mu.RUnlock()

	// Always return empty slice, never nil
	result := []*CapabilityAnnouncement{}

	// If cache is not initialized, return empty slice
	if r.remoteCapabilities == nil {
		return result
	}

	// Convert cache keys to subject format and match
	// Cache key format: "type.instance" (e.g., "processor.graph-ingest")
	// Subject format: "type.capabilities.instance" (e.g., "processor.capabilities.graph-ingest")
	for key, cap := range r.remoteCapabilities {
		// Convert cache key to subject by inserting "capabilities"
		parts := strings.SplitN(key, ".", 2)
		if len(parts) != 2 {
			continue // Invalid key format
		}
		subject := parts[0] + ".capabilities." + parts[1]

		if r.matchesPattern(subject, subjectPattern) {
			result = append(result, cap)
		}
	}

	return result
}

// WaitForCapabilities waits until minimum capabilities matching pattern are discovered.
// Returns immediately if len(GetCapabilities(pattern)) >= minCount.
// Returns ctx.Err() on context cancellation.
// Returns nil on timeout (proceed anyway - NOT an error per plan).
// Polls every 100ms.
func (r *Registry) WaitForCapabilities(ctx context.Context, pattern string, minCount int, timeout time.Duration) error {
	// Check immediately
	if len(r.GetCapabilities(pattern)) >= minCount {
		return nil
	}

	deadline := time.After(timeout)
	ticker := time.NewTicker(100 * time.Millisecond)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-deadline:
			return nil // Timeout - proceed anyway (not an error)
		case <-ticker.C:
			if len(r.GetCapabilities(pattern)) >= minCount {
				return nil
			}
		}
	}
}

// updateCapabilityCache updates the capability cache with a new announcement.
// Thread-safe for concurrent updates.
// Cache key format: "type.instance" (e.g., "processor.graph-ingest").
func (r *Registry) updateCapabilityCache(ann *CapabilityAnnouncement) {
	r.mu.Lock()
	defer r.mu.Unlock()

	// Build cache key: "type.instance"
	key := ann.Type + "." + ann.InstanceName
	r.remoteCapabilities[key] = ann
}

// capabilitiesStreamConfig declares the COMPONENT_CAPABILITIES stream.
//
// It is a named function rather than a literal inside InitNATS so the declaration
// is reachable by a test. That matters here specifically: this stream was the
// framework's own violation of the bounds requirement it asks sister repos to
// honor. It carried MaxMsgsPerSubject 1 and a one-hour MaxAge — bounded per
// subject in practice — while the live server reported `max_bytes=-1 discard=old`,
// neither of which anyone chose.
//
// Explicit component-type subjects avoid overlapping with JetStream API subjects.
//
// The size ceiling is generous relative to the data: one retained announcement per
// component subject, each a small JSON document. 64 MiB is far above any plausible
// fleet and is still a real ceiling on a memory-backed stream, whose bytes count
// against the account's memory tier.
//
// DiscardOld is deliberate and is the only correct choice here. An announcement is
// a FACT about a component's current capabilities, so the newest is the one worth
// keeping; DiscardNew would refuse the fresh announcement at the ceiling and leave
// discovery serving stale capabilities indefinitely.
func capabilitiesStreamConfig() jetstream.StreamConfig {
	return jetstream.StreamConfig{
		Name: "COMPONENT_CAPABILITIES",
		Subjects: []string{
			"processor.capabilities.*",
			"input.capabilities.*",
			"output.capabilities.*",
			"storage.capabilities.*",
			"gateway.capabilities.*",
		},
		Storage:           jetstream.MemoryStorage,
		MaxMsgsPerSubject: 1,
		MaxAge:            time.Hour,
		MaxBytes:          64 << 20,
		Discard:           jetstream.DiscardOld,
	}
}

// InitNATS initializes NATS JetStream capability discovery using natsclient.Client.
// Creates the COMPONENT_CAPABILITIES stream if it doesn't exist.
func (r *Registry) InitNATS(ctx context.Context, client *natsclient.Client, nodeID string) error {
	r.mu.Lock()
	r.natsClient = client
	r.nodeID = nodeID
	r.remoteCapabilities = make(map[string]*CapabilityAnnouncement)
	r.mu.Unlock()

	_, err := client.EnsureStream(ctx, capabilitiesStreamConfig())
	if err != nil {
		return errs.Wrap(err, "Registry", "InitNATS", "ensure capabilities stream")
	}

	return nil
}

type capabilityPublication struct {
	client       *natsclient.Client
	instanceName string
	subject      string
	data         []byte
}

// prepareCapabilityPublication captures the complete immutable publish input
// before any asynchronous work begins.
func (r *Registry) prepareCapabilityPublication(
	generation componentGeneration,
) (*capabilityPublication, error) {
	r.mu.RLock()
	natsClient := r.natsClient
	nodeID := r.nodeID
	registration := r.factories[generation.FactoryIdentity]
	if natsClient == nil {
		r.mu.RUnlock()
		return nil, nil
	}
	if registration == nil {
		r.mu.RUnlock()
		return nil, errs.WrapInvalid(
			fmt.Errorf("no registration found for component %s", generation.InstanceName),
			"Registry", "prepareCapabilityPublication", "lookup registration")
	}
	componentName := registration.Name
	componentType := registration.Type
	componentVersion := registration.Version
	r.mu.RUnlock()

	inputPorts, err := portsToCapabilities(generation.InputPorts, generation.InputFacts)
	if err != nil {
		return nil, errs.Wrap(err, "Registry", "prepareCapabilityPublication", "resolve input port capabilities")
	}
	outputPorts, err := portsToCapabilities(generation.OutputPorts, generation.OutputFacts)
	if err != nil {
		return nil, errs.Wrap(err, "Registry", "prepareCapabilityPublication", "resolve output port capabilities")
	}
	announcement := CapabilityAnnouncement{
		InstanceName: generation.InstanceName,
		Component:    componentName,
		Type:         componentType,
		Version:      componentVersion,
		InputPorts:   inputPorts,
		OutputPorts:  outputPorts,
		Timestamp:    time.Now(),
		TTL:          60 * time.Second,
		NodeID:       nodeID,
	}

	data, err := json.Marshal(announcement)
	if err != nil {
		return nil, errs.Wrap(err, "Registry", "prepareCapabilityPublication", "marshal announcement")
	}
	return &capabilityPublication{
		client:       natsClient,
		instanceName: generation.InstanceName,
		subject:      fmt.Sprintf("%s.capabilities.%s", componentType, generation.InstanceName),
		data:         append([]byte(nil), data...),
	}, nil
}

func publishCapabilities(ctx context.Context, publication *capabilityPublication) error {
	_, err := publication.client.PublishToStreamWithAck(ctx, publication.subject, publication.data)
	if err != nil {
		return errs.Wrap(err, "Registry", "publishCapabilities", "publish to NATS")
	}

	return nil
}

func (r *Registry) publishGenerationCapabilities(generation componentGeneration) {
	publication, err := r.prepareCapabilityPublication(generation)
	if err != nil {
		r.logger.Debug("failed to prepare capabilities", "component", generation.InstanceName, "error", err)
		return
	}
	if publication == nil {
		return
	}
	go func() {
		if err := publishCapabilities(context.Background(), publication); err != nil {
			r.logger.Debug("failed to publish capabilities", "component", publication.instanceName, "error", err)
		}
	}()
}

// portsToCapabilities projects retained ports and their admission-time facts.
func portsToCapabilities(ports []Port, facts []PortFacts) ([]PortCapability, error) {
	if len(ports) != len(facts) {
		return nil, fmt.Errorf("retained port/fact count mismatch: %d ports, %d facts", len(ports), len(facts))
	}
	capabilities := make([]PortCapability, 0, len(ports))
	for index, port := range ports {
		portFacts := facts[index]
		capability := PortCapability{
			Name:        port.Name,
			Description: port.Description,
			Type:        string(portFacts.InteractionPattern()),
		}
		subjects := portFacts.NATSSubjects()
		if len(subjects) > 0 {
			capability.Subject = subjects[0]
		}
		if contract, ok := portFacts.Interface(); ok {
			capability.Interface = contract.Type
		}

		capabilities = append(capabilities, capability)
	}
	return capabilities, nil
}

// SubscribeCapabilities subscribes to capability announcements from NATS.
// If no patterns provided, defaults to "*.capabilities.*" (all components).
func (r *Registry) SubscribeCapabilities(ctx context.Context, patterns ...string) error {
	r.mu.RLock()
	natsClient := r.natsClient
	nodeID := r.nodeID
	r.mu.RUnlock()

	if natsClient == nil {
		return errs.WrapInvalid(
			fmt.Errorf("NATS client not initialized"),
			"Registry", "SubscribeCapabilities", "check NATS client")
	}

	if len(patterns) == 0 {
		// Default: subscribe to all component types
		patterns = []string{"processor.capabilities.>"}
	}

	// Use natsclient's consumer management
	// Note: Currently using first pattern only. For multiple patterns, we would need
	// to create multiple consumers or use a more complex filter.
	err := natsClient.ConsumeInternalStreamWithConfig(ctx, natsclient.StreamConsumerConfig{
		StreamName:    "COMPONENT_CAPABILITIES",
		ConsumerName:  fmt.Sprintf("cap-registry-%s", nodeID),
		FilterSubject: patterns[0],
		DeliverPolicy: "all",
		AckPolicy:     "explicit",
	}, func(_ context.Context, msg jetstream.Msg) {
		var ann CapabilityAnnouncement
		if err := json.Unmarshal(msg.Data(), &ann); err == nil {
			r.updateCapabilityCache(&ann)
		}
		msg.Ack()
	})

	return err
}

// StartHeartbeat starts periodic republishing of all component capabilities.
func (r *Registry) StartHeartbeat(ctx context.Context, interval time.Duration) {
	// Create cancelable context
	heartbeatCtx, cancel := context.WithCancel(ctx)

	r.mu.Lock()
	r.heartbeatCancel = cancel
	r.mu.Unlock()

	// Start heartbeat goroutine
	go func() {
		ticker := time.NewTicker(interval)
		defer ticker.Stop()

		for {
			select {
			case <-heartbeatCtx.Done():
				return
			case <-ticker.C:
				r.republishAllCapabilities(heartbeatCtx)
			}
		}
	}()
}

// StopHeartbeat stops the heartbeat goroutine.
func (r *Registry) StopHeartbeat() {
	r.mu.Lock()
	cancel := r.heartbeatCancel
	r.mu.Unlock()

	if cancel != nil {
		cancel()
	}
}

// republishAllCapabilities republishes capabilities for all registered instances.
func (r *Registry) republishAllCapabilities(ctx context.Context) {
	for _, generation := range r.generationsSnapshot() {
		publication, err := r.prepareCapabilityPublication(generation)
		if err != nil {
			r.logger.Debug("failed to prepare capabilities", "component", generation.InstanceName, "error", err)
			continue
		}
		if publication == nil {
			return
		}
		if err := publishCapabilities(ctx, publication); err != nil {
			// Log warning but continue - NATS publish is non-fatal
			r.logger.Debug("failed to publish capabilities", "component", generation.InstanceName, "error", err)
		}
	}
}

// getRegistrationForInstance finds the registration for a component instance by name.
// Returns nil if no factory tracking exists for this instance.
func (r *Registry) getRegistrationForInstance(instanceName string) *Registration {
	r.mu.RLock()
	defer r.mu.RUnlock()

	// Look up factory name for this instance
	generation, exists := r.generations[instanceName]
	if !exists {
		return nil
	}

	// Return the registration for this factory
	return r.factories[generation.FactoryIdentity]
}

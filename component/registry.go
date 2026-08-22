package component

import (
	"encoding/json"
	"fmt"
	"math"
	"sort"
	"strings"
	"sync"

	"github.com/c360studio/semstreams/internal/componentadmission"
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
	// that declare this consume the model registry selected at boot.
	// Later model_registry writes require process restart.
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
	Dependencies []string     `json:"dependencies"` // Boot dependencies such as DepModelRegistry
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

// componentDeclaration is the immutable shape captured for one admitted
// component. It carries no
// lifecycle, health, readiness, grouping, or orchestration state.
type componentDeclaration struct {
	InstanceName       string
	FactoryIdentity    string
	InputPorts         []Port
	OutputPorts        []Port
	InputFacts         []PortFacts
	OutputFacts        []PortFacts
	ExclusiveResources []string
}

// preparedComponent exists only between factory construction and boot
// admission. The Registry retains the immutable declaration, never the live
// component handle.
type preparedComponent struct {
	declaration componentDeclaration
	component   Discoverable
}

// declarationSnapshot is the framework-internal defensive read view of one
// admitted component declaration. Registry read methods require the root internal access
// token; downstream adopters cannot obtain this otherwise-unexported type.
type declarationSnapshot struct {
	record componentDeclaration
}

func (s declarationSnapshot) Name() string    { return s.record.InstanceName }
func (s declarationSnapshot) Factory() string { return s.record.FactoryIdentity }

func (s declarationSnapshot) Inputs() []Port {
	return cloneResolvedPorts(s.record.InputPorts)
}

func (s declarationSnapshot) Outputs() []Port {
	return cloneResolvedPorts(s.record.OutputPorts)
}

func (s declarationSnapshot) InputDeclarationFacts() []PortFacts {
	return clonePortFactsSlice(s.record.InputFacts)
}

func (s declarationSnapshot) OutputDeclarationFacts() []PortFacts {
	return clonePortFactsSlice(s.record.OutputFacts)
}

// Registry manages component factories and immutable admitted declarations.
// Runtime component handles remain private to ComponentManager.
type Registry struct {
	factories    map[string]*Registration        // Factory registry by name
	declarations map[string]componentDeclaration // Admitted declaration by instance name
	mu           sync.RWMutex                    // Protects all registry state

	sealed bool
}

// NewRegistry creates a new empty component registry
func NewRegistry() *Registry {
	return &Registry{
		factories:    make(map[string]*Registration),
		declarations: make(map[string]componentDeclaration),
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

	prepared, err := captureComponentDeclaration(instanceName, config.Name, component)
	if err != nil {
		return preparedComponent{}, errs.Wrap(err, "Registry", operation, "capture declaration")
	}
	return preparedComponent{declaration: prepared, component: component}, nil
}

func (r *Registry) admitPrepared(prepared componentDeclaration) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.sealed {
		return errs.WrapInvalid(
			errs.ErrInvalidConfig,
			"Registry", "admitPrepared", "component composition is sealed for this process",
		)
	}

	_, exists := r.declarations[prepared.InstanceName]
	if exists {
		return errs.WrapInvalid(
			fmt.Errorf("instance '%s' is already registered", prepared.InstanceName),
			"Registry", "admitPrepared", "duplicate instance check")
	}
	if err := r.checkResourceConflictsLocked(prepared.InstanceName, prepared.ExclusiveResources); err != nil {
		return errs.Wrap(err, "Registry", "admitPrepared", "resource conflict check")
	}
	r.declarations[prepared.InstanceName] = prepared
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
	return cloneConfigSchema(registration.Schema), nil
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
			Schema:       cloneConfigSchema(registration.Schema),
			Dependencies: append([]string(nil), registration.Dependencies...),
			// Factory is intentionally not copied for safety
		}
	}

	return result
}

func cloneConfigSchema(schema ConfigSchema) ConfigSchema {
	clone := ConfigSchema{Required: append([]string(nil), schema.Required...)}
	if schema.Properties != nil {
		clone.Properties = make(map[string]PropertySchema, len(schema.Properties))
		for name, property := range schema.Properties {
			clone.Properties[name] = clonePropertySchema(property)
		}
	}
	return clone
}

func clonePropertySchema(property PropertySchema) PropertySchema {
	clone := property
	clone.Enum = append([]string(nil), property.Enum...)
	clone.Required = append([]string(nil), property.Required...)
	if property.Minimum != nil {
		value := *property.Minimum
		clone.Minimum = &value
	}
	if property.Maximum != nil {
		value := *property.Maximum
		clone.Maximum = &value
	}
	if property.MinLength != nil {
		value := *property.MinLength
		clone.MinLength = &value
	}
	if property.MaxLength != nil {
		value := *property.MaxLength
		clone.MaxLength = &value
	}
	if property.AdditionalProperties != nil {
		value := *property.AdditionalProperties
		clone.AdditionalProperties = &value
	}
	if property.Items != nil {
		value := clonePropertySchema(*property.Items)
		clone.Items = &value
	}
	if property.Properties != nil {
		clone.Properties = make(map[string]PropertySchema, len(property.Properties))
		for name, nested := range property.Properties {
			clone.Properties[name] = clonePropertySchema(nested)
		}
	}
	return clone
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
	for existingName, declaration := range r.declarations {
		if existingName == instanceName {
			continue
		}
		for _, existingResource := range declaration.ExclusiveResources {
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

func captureComponentDeclaration(
	instanceName, factoryIdentity string, discoverable Discoverable,
) (componentDeclaration, error) {
	inputs, inputFacts, err := cloneAndProjectPorts(discoverable.InputPorts())
	if err != nil {
		return componentDeclaration{}, err
	}
	outputs, outputFacts, err := cloneAndProjectPorts(discoverable.OutputPorts())
	if err != nil {
		return componentDeclaration{}, err
	}
	resources := make([]string, 0, len(inputFacts)+len(outputFacts))
	for _, facts := range append(append([]PortFacts(nil), inputFacts...), outputFacts...) {
		if facts.IsExclusive() {
			resources = append(resources, facts.ResourceID())
		}
	}
	sort.Strings(resources)
	resources = compactStrings(resources)
	return componentDeclaration{
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

// declaration returns a defensive clone of one admitted component declaration.
func (r *Registry) declaration(instanceName string) (componentDeclaration, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	declaration, ok := r.declarations[instanceName]
	if !ok {
		return componentDeclaration{}, false
	}
	return cloneComponentDeclaration(declaration), true
}

// declarationsSnapshot returns a deterministic defensive clone of the complete
// boot admission set.
func (r *Registry) declarationsSnapshot() []componentDeclaration {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.declarationsLocked()
}

// Snapshot returns one defensive admitted-declaration view to a root-internal
// framework consumer.
func (r *Registry) Snapshot(
	_ componentadmission.Access, instanceName string,
) (
	//revive:disable-next-line:unexported-return Framework callers consume this internal-token-gated opaque snapshot.
	declarationSnapshot,
	bool,
) {
	declaration, ok := r.declaration(instanceName)
	if !ok {
		return declarationSnapshot{}, false
	}
	return declarationSnapshot{record: declaration}, true
}

// Snapshots returns a deterministic defensive complete admission set to a
// root-internal framework consumer.
//
//revive:disable:unexported-return Framework callers range this internal-token-gated opaque snapshot set.
func (r *Registry) Snapshots(
	_ componentadmission.Access,
) []declarationSnapshot {
	declarations := r.declarationsSnapshot()
	result := make([]declarationSnapshot, len(declarations))
	for index, declaration := range declarations {
		result[index] = declarationSnapshot{record: declaration}
	}
	return result
}

//revive:enable:unexported-return

func (r *Registry) declarationsLocked() []componentDeclaration {
	names := make([]string, 0, len(r.declarations))
	for name := range r.declarations {
		names = append(names, name)
	}
	sort.Strings(names)
	result := make([]componentDeclaration, 0, len(names))
	for _, name := range names {
		result = append(result, cloneComponentDeclaration(r.declarations[name]))
	}
	return result
}

func cloneComponentDeclaration(declaration componentDeclaration) componentDeclaration {
	clone := declaration
	clone.InputPorts = cloneResolvedPorts(declaration.InputPorts)
	clone.OutputPorts = cloneResolvedPorts(declaration.OutputPorts)
	clone.InputFacts = clonePortFactsSlice(declaration.InputFacts)
	clone.OutputFacts = clonePortFactsSlice(declaration.OutputFacts)
	clone.ExclusiveResources = append([]string(nil), declaration.ExclusiveResources...)
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

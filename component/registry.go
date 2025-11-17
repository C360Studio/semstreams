package component

import (
	"encoding/json"
	"fmt"
	"maps"
	"math"
	"strings"
	"sync"

	"github.com/c360/semstreams/errors"
	"github.com/c360/semstreams/types"
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
	Dependencies []string     `json:"dependencies"` // Optional: other required components
}

// RegistrationConfig provides a clean API for component registration.
// This config struct replaces the previous 7-8 parameter function signatures.
// It maps 1:1 to Registration struct fields for simplicity.
type RegistrationConfig struct {
	Name        string       // Component name (e.g., "udp", "websocket", "graph-processor")
	Factory     Factory      // Factory function to create component instances
	Schema      ConfigSchema // Configuration schema for validation and discovery
	Type        string       // Component type: "input", "processor", "output", "storage"
	Protocol    string       // Technical protocol (udp, tcp, websocket, file, etc.)
	Domain      string       // Business domain (network, storage, processing, robotics, semantic)
	Description string       // Human-readable description of the component
	Version     string       // Component version (semver recommended)
}

// Registry manages component factories and instances
// It provides thread-safe registration and lookup of both factories (for creation)
// and instances (for discovery and management).
type Registry struct {
	factories       map[string]*Registration // Factory registry by name
	instances       map[string]Discoverable  // Instance registry by name
	payloadRegistry *PayloadRegistry         // Registry for message payloads
	resourceTracker map[string]string        // Resource ID -> Component instance name mapping
	mu              sync.RWMutex             // Protects all maps
}

// NewRegistry creates a new empty component registry
func NewRegistry() *Registry {
	return &Registry{
		factories:       make(map[string]*Registration),
		instances:       make(map[string]Discoverable),
		payloadRegistry: NewPayloadRegistry(),
		resourceTracker: make(map[string]string),
	}
}

// RegisterFactory registers a component factory with the given name
// Returns an error if a factory with the same name is already registered.
func (r *Registry) RegisterFactory(name string, registration *Registration) error {
	if name == "" {
		return errors.WrapInvalid(errors.ErrInvalidConfig, "Registry", "RegisterFactory", "factory name validation")
	}
	if registration == nil {
		return errors.WrapInvalid(errors.ErrInvalidConfig, "Registry", "RegisterFactory", "registration validation")
	}
	if registration.Factory == nil {
		return errors.WrapInvalid(errors.ErrInvalidConfig, "Registry", "RegisterFactory", "factory function validation")
	}
	if registration.Type == "" {
		return errors.WrapInvalid(errors.ErrInvalidConfig, "Registry", "RegisterFactory", "component type validation")
	}

	r.mu.Lock()
	defer r.mu.Unlock()

	if _, exists := r.factories[name]; exists {
		msg := fmt.Errorf("factory '%s' is already registered", name)
		return errors.WrapInvalid(msg, "Registry", "RegisterFactory", "duplicate factory check")
	}

	r.factories[name] = registration
	return nil
}

// CreateComponent creates a component instance using the specified factory
// The componentType parameter specifies which factory to use, instanceName
// gives the created component a unique identifier, and config provides all
// component configuration including dependencies.
// CreateComponent creates and registers a new component instance.
// The instanceName parameter is the unique identifier for this instance
// (e.g., "udp-sensor-main").
// The config contains the factory name, type, and component-specific configuration.
// Factory functions don't do I/O, so no context is needed.
func (r *Registry) CreateComponent(
	instanceName string, config types.ComponentConfig, deps Dependencies,
) (Discoverable, error) {
	// Security: Validate instance name
	if err := ValidateComponentName(instanceName); err != nil {
		return nil, errors.Wrap(err, "Registry", "CreateComponent", "instance name validation")
	}
	if config.Type == "" {
		return nil, errors.WrapInvalid(
			errors.ErrInvalidConfig, "Registry", "CreateComponent", "component type validation")
	}
	// Security: Validate factory name
	if err := ValidateComponentName(config.Name); err != nil {
		return nil, errors.Wrap(err, "Registry", "CreateComponent", "factory name validation")
	}
	if deps.NATSClient == nil {
		return nil, errors.WrapInvalid(errors.ErrInvalidConfig, "Registry", "CreateComponent", "NATS client validation")
	}

	// CRITICAL SECURITY: Comprehensive validation before factory execution
	// This prevents injection attacks, resource exhaustion, and malformed input
	if err := ValidateFactoryConfig(config.Config); err != nil {
		return nil, errors.Wrap(err, "Registry", "CreateComponent", "config security validation")
	}

	// Look up factory by the component/factory name (e.g., "udp", "websocket")
	r.mu.RLock()
	registration, exists := r.factories[config.Name]
	r.mu.RUnlock()

	if !exists {
		msg := fmt.Errorf("unknown component factory '%s'", config.Name)
		return nil, errors.WrapInvalid(msg, "Registry", "CreateComponent", "factory lookup")
	}

	// Validate that the factory type matches the requested type
	if registration.Type != string(config.Type) {
		msg := fmt.Errorf("component '%s' is type '%s', not '%s'",
			config.Name, registration.Type, config.Type)
		return nil, errors.WrapInvalid(msg, "Registry", "CreateComponent", "type validation")
	}

	// Create the component using the factory with service pattern
	// Pass the component-specific config (config.Config) to the factory
	component, err := registration.Factory(config.Config, deps)
	if err != nil {
		return nil, errors.Wrap(err, "Registry", "CreateComponent", "factory execution")
	}

	// Register the instance with the unique instance name
	if err := r.RegisterInstance(instanceName, component); err != nil {
		return nil, errors.Wrap(err, "Registry", "CreateComponent", "instance registration")
	}

	return component, nil
}

// RegisterInstance registers a component instance with the given name
// This allows the instance to be discovered and managed.
// Returns an error if an instance with the same name is already registered.
func (r *Registry) RegisterInstance(name string, component Discoverable) error {
	if name == "" {
		return errors.WrapInvalid(errors.ErrInvalidConfig, "Registry", "RegisterInstance", "instance name validation")
	}
	if component == nil {
		return errors.WrapInvalid(errors.ErrInvalidConfig, "Registry", "RegisterInstance", "component validation")
	}

	r.mu.Lock()
	defer r.mu.Unlock()

	if _, exists := r.instances[name]; exists {
		msg := fmt.Errorf("instance '%s' is already registered", name)
		return errors.WrapInvalid(msg, "Registry", "RegisterInstance", "duplicate instance check")
	}

	// Check for resource conflicts before registering
	if err := r.checkResourceConflicts(name, component); err != nil {
		return errors.Wrap(err, "Registry", "RegisterInstance", "resource conflict check")
	}

	// Register the instance
	r.instances[name] = component

	// Track component resources
	r.trackComponentResources(name, component)

	return nil
}

// UnregisterInstance removes a component instance from the registry
// This is typically called when a component is stopped or destroyed.
func (r *Registry) UnregisterInstance(name string) {
	if name == "" {
		return
	}

	r.mu.Lock()
	defer r.mu.Unlock()

	// Get component before removing to clean up resources
	if component, exists := r.instances[name]; exists {
		// Clean up resource tracking
		r.untrackComponentResources(name, component)
	}

	delete(r.instances, name)
}

// ListComponents returns all registered component instances
// This is used by the discovery service to provide information about
// currently running components.
func (r *Registry) ListComponents() map[string]Discoverable {
	r.mu.RLock()
	defer r.mu.RUnlock()

	// Return a copy to prevent external modification
	result := make(map[string]Discoverable, len(r.instances))
	maps.Copy(result, r.instances)

	return result
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
		return ConfigSchema{}, errors.WrapInvalid(
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
		return nil, errors.WrapInvalid(
			fmt.Errorf("component type %q not found", name),
			"Registry", "GetComponent", "type lookup")
	}

	// Create a temporary instance just to get the schema
	// ConfigSchema() doesn't perform I/O, so this is safe
	// NOTE: This will fail if factory validates dependencies
	deps := Dependencies{} // Empty deps for schema retrieval
	component, err := registration.Factory(json.RawMessage("{}"), deps)
	if err != nil {
		return nil, errors.Wrap(err, "Registry", "GetComponent", "factory execution")
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

// Component retrieves a specific component instance by name
// Returns nil if the component is not found.
func (r *Registry) Component(name string) Discoverable {
	r.mu.RLock()
	defer r.mu.RUnlock()

	return r.instances[name]
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
			Type:         registration.Type,
			Protocol:     registration.Protocol,
			Domain:       registration.Domain,
			Description:  registration.Description,
			Version:      registration.Version,
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
		Name:        config.Name,
		Factory:     config.Factory,
		Schema:      config.Schema,
		Type:        config.Type,
		Protocol:    config.Protocol,
		Domain:      config.Domain,
		Description: config.Description,
		Version:     config.Version,
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

// RegisterPayload registers a payload factory with the registry.
// This allows typed payloads to be recreated during message deserialization.
func (r *Registry) RegisterPayload(registration *PayloadRegistration) error {
	return r.payloadRegistry.RegisterPayload(registration)
}

// CreatePayload creates a payload instance using the registered factory.
// Returns nil if the message type is not registered.
func (r *Registry) CreatePayload(domain, category, version string) any {
	return r.payloadRegistry.CreatePayload(domain, category, version)
}

// ListPayloads returns all registered payload types.
func (r *Registry) ListPayloads() map[string]*PayloadRegistration {
	return r.payloadRegistry.ListPayloads()
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
		return errors.WrapInvalid(errors.ErrInvalidConfig, "ConfigValidator", "ValidateConfigKey", "empty key")
	}
	if len(key) > MaxStringLength {
		return errors.WrapInvalid(errors.ErrInvalidConfig, "ConfigValidator", "ValidateConfigKey", "key too long")
	}
	// Check for potentially dangerous characters
	if strings.ContainsAny(key, "\x00\n\r\t") {
		return errors.WrapInvalid(
			errors.ErrInvalidConfig,
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
		return errors.WrapInvalid(
			errors.ErrInvalidConfig, "ConfigValidator", "ValidateJSONSize", "JSON too large")
	}
	return nil
}

// ValidateComponentName validates component/instance names for security
func ValidateComponentName(name string) error {
	if name == "" {
		return errors.WrapInvalid(errors.ErrInvalidConfig, "ConfigValidator", "ValidateComponentName", "empty name")
	}
	if len(name) > MaxStringLength {
		return errors.WrapInvalid(errors.ErrInvalidConfig, "ConfigValidator", "ValidateComponentName", "name too long")
	}
	// Check for potentially dangerous characters - allow alphanumeric, dash, underscore , dot
	for _, r := range name {
		if !((r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') ||
			(r >= '0' && r <= '9') || r == '-' || r == '_' || r == '.') {
			return errors.WrapInvalid(
				errors.ErrInvalidConfig, "ConfigValidator", "ValidateComponentName",
				"invalid name characters")
		}
	}
	return nil
}

// ValidatePortNumber validates port numbers are within valid range
func ValidatePortNumber(port int) error {
	if port < MinPort || port > MaxPort {
		msg := fmt.Errorf("port %d outside valid range %d-%d", port, MinPort, MaxPort)
		return errors.WrapInvalid(msg, "ConfigValidator", "ValidatePortNumber",
			"port range validation")
	}
	return nil
}

// checkResourceConflicts checks if any of the component's ports conflict with existing resources
func (r *Registry) checkResourceConflicts(_ string, component Discoverable) error {
	// Get all input and output ports for conflict checking
	allPorts := append(component.InputPorts(), component.OutputPorts()...)

	for _, port := range allPorts {
		if port.Config != nil && port.Config.IsExclusive() {
			resourceID := port.Config.ResourceID()

			// Special validation for network ports
			if networkPort, ok := port.Config.(NetworkPort); ok {
				if err := ValidatePortNumber(networkPort.Port); err != nil {
					return errors.Wrap(err, "Registry", "checkResourceConflicts", "network port validation")
				}
			}

			// Check if this exclusive resource is already in use
			if existingInstance, exists := r.resourceTracker[resourceID]; exists {
				msg := fmt.Errorf("resource conflict: %s already used by component '%s'",
					resourceID, existingInstance)
				return errors.WrapInvalid(msg, "Registry", "checkResourceConflicts",
					"exclusive resource check")
			}
		}
	}

	return nil
}

// trackComponentResources adds component resources to the tracker
func (r *Registry) trackComponentResources(instanceName string, component Discoverable) {
	allPorts := append(component.InputPorts(), component.OutputPorts()...)

	for _, port := range allPorts {
		if port.Config != nil && port.Config.IsExclusive() {
			resourceID := port.Config.ResourceID()
			r.resourceTracker[resourceID] = instanceName
		}
	}
}

// untrackComponentResources removes component resources from the tracker
func (r *Registry) untrackComponentResources(instanceName string, component Discoverable) {
	allPorts := append(component.InputPorts(), component.OutputPorts()...)

	for _, port := range allPorts {
		if port.Config != nil && port.Config.IsExclusive() {
			resourceID := port.Config.ResourceID()
			// Only remove if it belongs to this instance
			if trackedInstance, exists := r.resourceTracker[resourceID]; exists && trackedInstance == instanceName {
				delete(r.resourceTracker, resourceID)
			}
		}
	}
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
// Payload registration remains global as payloads are data types, not components.

// Global payload registry for message deserialization
var globalPayloadRegistry = NewPayloadRegistry()

// RegisterPayload registers a payload factory globally.
// This allows typed payloads to be recreated during message deserialization.
// Payloads use init() registration as they are data types, not lifecycle components.
func RegisterPayload(registration *PayloadRegistration) error {
	return globalPayloadRegistry.RegisterPayload(registration)
}

// CreatePayload creates a payload instance using the globally registered factory.
// Returns nil if no factory is registered for the given type.
func CreatePayload(domain, category, version string) any {
	return globalPayloadRegistry.CreatePayload(domain, category, version)
}

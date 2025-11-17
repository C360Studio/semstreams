// Package component provides the core component infrastructure for SemStreams,
// enabling dynamic component discovery, registration, lifecycle management, and
// instance creation.
//
// # Overview
//
// The component package defines fundamental abstractions for all SemStreams components,
// supporting four component types: inputs (data sources), processors (data transformers),
// outputs (data sinks), and storage (persistence). Components are self-describing units
// that can be discovered at runtime, configured through schemas, and managed through
// their lifecycle.
//
// The Registry serves as the central component management system, handling both factory
// registration and instance management with thread-safe operations and proper lifecycle
// control.
//
// # Component Registration Pattern
//
// SemStreams uses EXPLICIT registration rather than init() self-registration. This provides:
//   - Testability: Can create isolated registries for testing
//   - Explicitness: Clear component dependency graph
//   - Control: Main application controls what gets registered
//   - No side effects: No global state modification during package initialization
//
// Registration Flow:
//
//  1. Each component package exports a Register(*Registry) error function
//  2. componentregistry.RegisterAll() orchestrates all registrations
//  3. main.go explicitly calls RegisterAll() with a created Registry
//  4. Components are now available for instantiation
//
// Example component registration:
//
//	// In pkg/input/udp.go
//	func Register(registry *component.Registry) error {
//		return registry.RegisterWithConfig(component.RegistrationConfig{
//			Name:        "udp",
//			Factory:     CreateUDPInput,
//			Schema:      udpSchema,
//			Type:        "input",
//			Protocol:    "udp",
//			Domain:      "network",
//			Description: "UDP input component for network data",
//			Version:     "1.0.0",
//		})
//	}
//
//	// In pkg/componentregistry/register.go
//	func RegisterAll(registry *component.Registry) error {
//		if err := input.Register(registry); err != nil {
//			return err
//		}
//		if err := robotics.Register(registry); err != nil {
//			return err
//		}
//		// ... more registrations
//		return nil
//	}
//
//	// In cmd/semstreams/main.go
//	registry := component.NewRegistry()
//	if err := componentregistry.RegisterAll(registry); err != nil {
//		log.Fatal(err)
//	}
//
// # Quick Start
//
// Creating and using a component:
//
//	// Create component registry and register all components
//	registry := component.NewRegistry()
//	if err := componentregistry.RegisterAll(registry); err != nil {
//		return err
//	}
//
//	// Create component configuration
//	config := types.ComponentConfig{
//		Type:    types.ComponentTypeInput,
//		Name:    "udp",
//		Enabled: true,
//		Config:  json.RawMessage(`{"port": 8080, "bind": "0.0.0.0"}`),
//	}
//
//	// Prepare component dependencies
//	deps := component.Dependencies{
//		NATSClient: natsClient,
//		Platform: component.PlatformMeta{
//			Org:      "c360",
//			Platform: "platform1",
//		},
//		Logger: slog.Default(),
//	}
//
//	// Create component instance
//	instance, err := registry.CreateComponent("udp-input-1", config, deps)
//	if err != nil {
//		return err
//	}
//
//	// Component is now ready to use
//	meta := instance.Meta()
//	health := instance.Health()
//
// # core Concepts
//
// Discoverable Interface:
//
// Every component must implement Discoverable, providing metadata, port definitions,
// configuration schema, health status, and data flow metrics. This enables runtime
// introspection and management.
//
// Registry Pattern:
//
// The Registry manages component factories and instances with thread-safe operations.
// Components register explicitly via Register() functions called by componentregistry,
// and the Registry handles creation and lifecycle management.
//
// Dependencies:
//
// All external dependencies (NATS client, object store, metrics, logger, platform
// identity) are injected through Dependencies struct, following clean
// dependency injection patterns.
//
// Port Types:
//
// Components declare their inputs and outputs using strongly-typed ports that
// implement the Portable interface:
//
//   - NATSPort: core pub/sub messaging on NATS subjects
//   - JetStreamPort: Durable streaming with JetStream for reliable delivery
//   - KVWatchPort: Watch KV bucket changes for real-time state observation
//   - KVWritePort: Declare writes to KV buckets for flow validation
//   - NATSRequestPort: Request/reply pattern with timeouts
//   - NetworkPort: TCP/UDP network bindings for external connectivity
//
// Example port configuration:
//
//	func (c *MyComponent) OutputPorts() []component.Port {
//		return []component.Port{
//			{
//				Name:      "data_stream",
//				Direction: component.DirectionOutput,
//				Required:  true,
//				Config:    component.NATSPort{Subject: "data.output"},
//			},
//			{
//				Name:      "entity_states",
//				Direction: component.DirectionOutput,
//				Required:  false,
//				Config: component.KVWritePort{
//					Bucket: "ENTITY_STATES",
//					Interface: &component.InterfaceContract{
//						Type:    "graph.EntityState",
//						Version: "v1",
//					},
//				},
//			},
//		}
//	}
//
// # Configuration Schema
//
// Components define their configuration through ConfigSchema, enabling:
//   - Schema-driven UI generation with type-specific form inputs
//   - Client and server-side validation before config persistence
//   - Property categorization (basic vs advanced) for progressive disclosure
//   - Default value population for improved user experience
//
// Schema Definition Example:
//
//	func (u *UDPInput) ConfigSchema() component.ConfigSchema {
//		return component.ConfigSchema{
//			Properties: map[string]component.PropertySchema{
//				"port": {
//					Type:        "int",
//					Description: "UDP port to listen on",
//					Default:     14550,
//					Minimum:     ptrInt(1),
//					Maximum:     ptrInt(65535),
//					Category:    "basic",  // Shown by default in UI
//				},
//				"bind_address": {
//					Type:        "string",
//					Description: "IP address to bind to (0.0.0.0 for all interfaces)",
//					Default:     "0.0.0.0",
//					Category:    "basic",
//				},
//				"buffer_size": {
//					Type:        "int",
//					Description: "UDP receive buffer size in bytes",
//					Default:     8192,
//					Minimum:     ptrInt(512),
//					Category:    "advanced",  // Hidden in collapsible section
//				},
//			},
//			Required: []string{"port", "bind_address"},
//		}
//	}
//
// Property Types:
//   - "string": Text input, optional pattern validation
//   - "int": Number input with min/max constraints
//   - "bool": Checkbox input
//   - "float": Number input allowing decimals
//   - "enum": Dropdown select with predefined values
//   - "object": Complex nested configuration (JSON editor fallback in MVP)
//   - "array": List of values (JSON editor fallback in MVP)
//
// Schema Validation:
//
// Configurations are validated both client-side (instant feedback) and server-side
// (before persistence) using the ValidateConfig() function:
//
//	config := map[string]any{
//		"port": 99999,  // Exceeds maximum
//	}
//
//	errors := component.ValidateConfig(config, schema)
//	if len(errors) > 0 {
//		// Returns: [{Field: "port", Message: "port must be <= 65535", Code: "max"}]
//		// Frontend displays error next to the port field
//	}
//
// Graceful Degradation:
//
// Components without schemas still work - the UI falls back to a JSON editor.
// The system logs warnings when schemas are missing but continues operating:
//
//	// Component without schema - still functional
//	func (c *MyComponent) ConfigSchema() component.ConfigSchema {
//		return component.ConfigSchema{}  // Empty schema
//	}
//	// UI will show: "Schema not available, using JSON editor"
//
// Property Categorization:
//
// The Category field organizes properties for progressive disclosure:
//   - "basic": Common settings shown by default (port, host, enabled)
//   - "advanced": Expert settings in collapsible section (buffer sizes, timeouts)
//   - Empty/unset: Defaults to "advanced"
//
// UI renders basic properties first, then advanced in a collapsible <details> element.
// Properties within each category are sorted alphabetically for consistency.
//
// Helper Functions:
//   - GetProperties(schema, category): Filter properties by category
//   - SortedPropertyNames(schema): Get property names in UI display order
//   - IsComplexType(propType): Identify object/array types needing special handling
//   - ValidateConfig(config, schema): Validate configuration against schema
//
// # Discoverable Interface
//
// All components must implement the Discoverable interface:
//
//	type Discoverable interface {
//		Meta() Metadata           // Component metadata (name, type, version)
//		InputPorts() []Port       // Input port definitions
//		OutputPorts() []Port      // Output port definitions
//		ConfigSchema() ConfigSchema // Configuration schema for validation
//		Health() HealthStatus     // Current health status
//		DataFlow() FlowMetrics    // Data flow metrics (messages, bytes)
//	}
//
// This interface enables:
//   - Runtime introspection of component capabilities
//   - Dynamic configuration validation
//   - Health monitoring and metrics collection
//   - Data flow visualization and debugging
//
// # Dependencies
//
// Dependencies are injected through a structured dependencies object:
//
//	type Dependencies struct {
//		NATSClient      *natsclient.Client      // Required: messaging
//		ObjectStore     ObjectStore             // Optional: persistence
//		MetricsRegistry *metric.MetricsRegistry // Optional: Prometheus metrics
//		Logger          *slog.Logger            // Optional: structured logging
//		Platform        PlatformMeta            // Required: platform identity
//	}
//
// Benefits:
//   - Clean dependency injection
//   - Easy testing with mock dependencies
//   - Avoids parameter proliferation in factory functions
//   - Follows service architecture patterns
//
// # Factory Pattern
//
// Component factories follow a consistent signature:
//
//	type Factory func(rawConfig json.RawMessage, deps Dependencies) (Discoverable, error)
//
// Example factory implementation:
//
//	func CreateUDPInput(rawConfig json.RawMessage, deps Dependencies) (component.Discoverable, error) {
//		// Parse component-specific configuration
//		var config UDPConfig
//		if err := json.Unmarshal(rawConfig, &config); err != nil {
//			return nil, fmt.Errorf("parse UDP config: %w", err)
//		}
//
//		// Validate configuration
//		if err := config.Validate(); err != nil {
//			return nil, fmt.Errorf("invalid UDP config: %w", err)
//		}
//
//		// Create component with dependencies
//		return &UDPInput{
//			config:     config,
//			natsClient: deps.NATSClient,
//			logger:     deps.Logger,
//			platform:   deps.Platform,
//		}, nil
//	}
//
// Factories:
//   - Receive raw JSON configuration and parse it themselves
//   - Validate configuration before creating instances
//   - Return initialized components ready to use
//   - Follow service constructor patterns for consistency
//
// # Registry Thread Safety
//
// All Registry operations are thread-safe:
//   - Factory registration uses write locks
//   - Component creation uses read locks for factory lookup
//   - Instance tracking uses write locks
//   - Listing operations use read locks
//
// Concurrency characteristics:
//   - Multiple goroutines can create components concurrently
//   - Factory registration blocks component creation temporarily
//   - ListAvailable() is safe to call during component creation
//   - No deadlocks due to ordered lock acquisition
//
// # Error Handling
//
// The package defines specific error types for different failure modes:
//
//	ErrFactoryAlreadyExists // Attempted to register duplicate factory
//	ErrInvalidFactory       // Invalid factory registration
//	ErrFactoryNotFound      // Attempted to create from unknown factory
//	ErrComponentCreation    // Factory returned error during creation
//	ErrInstanceExists       // Instance name already in use
//	ErrInstanceNotFound     // Attempted to access unknown instance
//
// Error checking:
//
//	_, err := registry.CreateComponent("instance-1", config, deps)
//	if errors.Is(err, component.ErrFactoryNotFound) {
//		// Handle missing factory - configuration error
//	}
//	if errors.Is(err, component.ErrComponentCreation) {
//		// Handle factory failure - component-specific error
//	}
//
// # Testing
//
// The explicit registration pattern makes testing straightforward:
//
//	// Create isolated test registry
//	registry := component.NewRegistry()
//
//	// Register only components needed for test
//	if err := input.Register(registry); err != nil {
//		t.Fatal(err)
//	}
//
//	// Create test dependencies with mocks
//	deps := component.Dependencies{
//		NATSClient: natsclient.NewTestClient(t),
//		Platform: component.PlatformMeta{
//			Org:      "test",
//			Platform: "test-platform",
//		},
//		Logger: slog.Default(),
//	}
//
//	// Test component creation
//	instance, err := registry.CreateComponent("test-1", config, deps)
//	if err != nil {
//		t.Fatal(err)
//	}
//
//	// Verify component behavior through Discoverable interface
//	assert.Equal(t, "udp", instance.Meta().Type)
//	assert.True(t, instance.Health().Healthy)
//
// Testing patterns:
//   - Use real NATS client via natsclient.NewTestClient() for integration tests
//   - Create isolated registries per test to avoid global state
//   - Mock external dependencies that cannot be containerized
//   - Test component behavior through Discoverable interface methods
//   - Verify factory registration and component creation separately
//
// # Performance Considerations
//
// Registry Performance:
//   - Factory lookup: O(1) with map-based storage
//   - Component creation: Factory execution time + O(1) registry overhead
//   - Memory: Components maintain references in Registry until unregistered
//   - Concurrency: Read-write mutex allows concurrent component creation
//
// Component Lifecycle:
//   - Components are created on-demand, not pre-instantiated
//   - Registry holds strong references to created instances
//   - Memory is released when components are unregistered
//   - No automatic garbage collection of unused components
//
// # Architecture Decisions
//
// Explicit Registration vs init() Self-Registration:
//
// Decision: Use explicit Register() functions called by componentregistry
//
// Benefits:
//   - Testability: Can create isolated registries without global state
//   - Explicitness: Clear component dependency graph in componentregistry
//   - Control: Main application controls what gets registered and when
//   - No side effects: Package imports don't modify global state
//   - Deterministic: Registration order is explicit and controllable
//
// Tradeoffs:
//   - Requires componentregistry orchestration package
//   - Registration must be explicitly called in main()
//   - New components must update componentregistry.RegisterAll()
//
// Registry-Based Architecture vs Distributed Catalog:
//
// Decision: Use centralized Registry for component management
//
// Benefits:
//   - Simpler to reason about and test
//   - Single source of truth for component management
//   - Thread-safe operations with minimal overhead
//   - No network dependencies for component discovery
//
// Dependency Injection via Struct:
//
// Decision: Use Dependencies struct instead of individual parameters
//
// Benefits:
//   - Avoids parameter proliferation in factory functions
//   - Easy to add new dependencies without breaking existing factories
//   - Enables easy testing with mock dependencies
//   - Follows service architecture patterns
//
// Factory Pattern for Component Creation:
//
// Decision: Service-like constructors that parse their own configuration
//
// Benefits:
//   - Components handle their own configuration parsing
//   - Enables flexible configuration validation per component
//   - Matches service constructor signatures for consistency
//   - Centralizes configuration knowledge in component packages
//
// # Integration Points
//
// Dependencies:
//   - pkg/natsclient: Required for NATS messaging
//   - pkg/storage/objectstore: Optional for persistence
//   - pkg/metric: Optional for Prometheus metrics
//   - log/slog: Optional for structured logging (defaults to slog.Default())
//
// Used By:
//   - pkg/service: Manager uses Registry for component lifecycle
//   - pkg/componentregistry: Orchestrates component registration
//   - cmd/semstreams: Application entry point creates and populates Registry
//
// Data Flow:
//
//	Configuration → Factory Lookup → Factory Execution → Component Instance → Registry
//
// # Examples
//
// See component_test.go and registry_test.go for comprehensive examples including:
//   - Component registration and creation
//   - Factory validation
//   - Instance management
//   - Error handling patterns
//   - Testing with isolated registries
package component

// Package flowstore provides persistence for visual flow definitions.
//
// # Overview
//
// The flowstore package manages the storage and retrieval of Flow entities,
// which represent visual flow configurations created by users in the flow builder UI.
// Flows contain canvas layout information (node positions, connections) and metadata,
// but do not contain runtime component instances.
//
// # Architecture
//
// Flow entities are stored in NATS KV bucket "semstreams_flows" with optimistic
// concurrency control via version numbers. This is separate from the
// "semstreams_config" bucket used for runtime component configurations.
//
// Design-time (flowstore):
//   - User creates/edits flows in UI
//   - Canvas layout and connections stored as Flow entities
//   - Metadata: name, description, runtime state
//
// Runtime (config package):
//   - FlowEngine translates Flow → ComponentConfigs
//   - ComponentConfigs stored in semstreams_config KV
//   - Manager watches and triggers ComponentManager
//
// # Key Concepts
//
// Flow Entity:
//   - ID: Unique flow identifier
//   - Nodes: Visual components on canvas (with positions)
//   - Connections: Edges between node ports
//   - RuntimeState: not_deployed, deployed_stopped, running, error
//   - Version: Optimistic concurrency control
//
// Node vs Component:
//   - Node.Type: Factory name (e.g., "udp", "graph-processor")
//   - Node.Name: Instance name (e.g., "udp-input-1")
//   - Node.Config: Component-specific configuration
//
// # Validation
//
// Flow.Validate() checks:
//   - Required fields (ID, Name, RuntimeState)
//   - Valid RuntimeState values
//   - Node completeness (ID, Type, Name)
//   - No duplicate node IDs
//   - Connection validity (IDs, ports, node references)
//
// All validation errors use errors.WrapInvalid for consistent error handling.
//
// # Optimistic Concurrency
//
// The Store uses version-based conflict detection:
//   - Create: Sets version to 1
//   - Update: Checks current version matches, increments on success
//   - Conflict: Returns errors.WrapInvalid with "conflict" message
//
// Example workflow:
//
//	flow, _ := store.Get(ctx, "my-flow")
//	// flow.Version = 5
//
//	// Another user updates
//	// Version is now 6 in KV
//
//	flow.Name = "Updated Name"
//	err := store.Update(ctx, flow) // FAILS - version 5 != 6
//	// Error contains "conflict"
//
// # Integration with FlowEngine
//
// The flowstore package is used by flowengine for deployment:
//
//  1. FlowEngine.Deploy(flowID) retrieves Flow from flowstore
//  2. Validates flow structure
//  3. Translates to ComponentConfigs (using component registry)
//  4. Writes to semstreams_config KV
//  5. Updates Flow.RuntimeState to deployed_stopped
//
// # Testing
//
// Integration tests use testcontainers with real NATS:
//   - TestCreateAndGet: Basic CRUD
//   - TestOptimisticConcurrency: Version conflicts
//   - TestComplexFlow: Nodes and connections
//
// All tests follow Constitutional Principle II (Real Dependencies).
//
// # Error Classification
//
// Following pkg/errors patterns:
//   - WrapInvalid: Bad input, validation failures, version conflicts
//   - WrapTransient: NATS KV errors, network issues
//   - WrapFatal: Marshaling errors, nil flow pointer
//
// # Example Usage
//
//	// Create store
//	store, err := flowstore.NewStore(natsClient)
//
//	// Create flow
//	flow := &flowstore.Flow{
//		ID:   "my-flow",
//		Name: "My First Flow",
//		RuntimeState: flowstore.StateNotDeployed,
//		Nodes: []flowstore.FlowNode{
//			{
//				ID:       "node-1",
//				Type:     "udp",
//				Name:     "udp-input-1",
//				Position: flowstore.Position{X: 100, Y: 100},
//				Config:   map[string]any{"port": 5000},
//			},
//		},
//		Connections: []flowstore.FlowConnection{},
//	}
//
//	err = store.Create(ctx, flow)
//	// flow.Version now = 1, timestamps set
//
//	// Update flow
//	flow.Name = "Updated Name"
//	err = store.Update(ctx, flow)
//	// flow.Version now = 2
//
// # Package Structure
//
//	flowstore/
//	├── doc.go                       # This file
//	├── flow.go                      # Flow entity and validation
//	├── flow_test.go                 # Unit tests for validation
//	├── store.go                     # KV-based Store implementation
//	└── store_integration_test.go   # Integration tests with real NATS
package flowstore

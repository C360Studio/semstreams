// Package flowengine translates Flow entities to ComponentConfigs and manages deployment.
//
// # Overview
//
// The flowengine package bridges the gap between design-time flows (flowstore)
// and runtime components (config/component packages). It translates visual flow
// definitions into deployable component configurations and manages the full
// deployment lifecycle.
//
// # Architecture
//
// The Engine integrates with existing SemStreams infrastructure:
//
//	┌─────────────┐
//	│   FlowUI    │ (Future)
//	└──────┬──────┘
//	       │ HTTP POST /flows/deployment/{id}/deploy
//	       ▼
//	┌─────────────┐
//	│ FlowService │ (pkg/service/flow_service.go)
//	└──────┬──────┘
//	       │
//	       ▼
//	┌─────────────┐     Reads      ┌──────────────┐
//	│ FlowEngine  │ ──────────────> │  FlowStore   │
//	│             │                 │ (flowstore)  │
//	│ - Deploy()  │                 └──────────────┘
//	│ - Start()   │
//	│ - Stop()    │     Translates
//	│ - Undeploy()│ ───────────────────────┐
//	└──────┬──────┘                        │
//	       │                               ▼
//	       │ Writes                 Flow → ComponentConfigs
//	       │                        (uses ComponentRegistry)
//	       ▼
//	┌──────────────────┐
//	│ Manager KV │ (semstreams_config)
//	│ (already exists) │
//	└────────┬─────────┘
//	         │ Watches
//	         ▼
//	┌──────────────────┐
//	│ ComponentManager │
//	│   (existing)     │
//	└──────────────────┘
//
// # Key Innovation: Dynamic Component Registry Lookup
//
// Unlike hardcoded factory-to-type mappings, the Engine uses the component
// registry to dynamically determine component types. This means:
//
//   - Adding new components requires NO changes to flowengine
//   - Component type mapping is automatic and correct
//   - Follows existing component registration patterns
//
// Implementation:
//
//	func (e *Engine) mapFactoryToComponentType(factoryName string) (types.ComponentType, error) {
//		// Look up factory in registry (NOT hardcoded switch)
//		factories := e.componentRegistry.ListFactories()
//		registration := factories[factoryName]
//		// Return type from registration metadata
//		return registration.Type, nil
//	}
//
// # Deployment Lifecycle
//
// The Engine manages four operations that map to flow runtime states:
//
// 1. Deploy (not_deployed → deployed_stopped):
//   - Retrieve Flow from flowstore
//   - Validate flow structure
//   - Translate nodes to ComponentConfigs (using registry)
//   - Write to semstreams_config KV
//   - Update Flow.RuntimeState
//
// 2. Start (deployed_stopped → running):
//   - Set all component configs Enabled = true
//   - Write to semstreams_config KV
//   - Update Flow.RuntimeState
//
// 3. Stop (running → deployed_stopped):
//   - Set all component configs Enabled = false
//   - Write to semstreams_config KV
//   - Update Flow.RuntimeState
//
// 4. Undeploy (deployed_stopped → not_deployed):
//   - Delete all component configs from KV
//   - Update Flow.RuntimeState
//   - Cannot undeploy running flows (validation error)
//
// # Translation Logic
//
// Flow nodes are translated to ComponentConfigs:
//
//	FlowNode {                      ComponentConfig {
//	  ID:       "node-1"               (not used)
//	  Type:     "udp"          →      Type:    "input"  (via registry lookup)
//	  Name:     "udp-input-1"  →      Name:    "udp"    (factory name)
//	  Position: {X:100, Y:100}        (not used)
//	  Config:   {...}          →      Config:  json.RawMessage
//	}                           →      Enabled: true
//	                                 }
//
// Key fields:
//   - Node.Name becomes the config key: "components.udp-input-1"
//   - Node.Type (factory name) looked up in registry for ComponentType
//   - Node.Config marshaled to ComponentConfig.Config
//   - ComponentConfig.Enabled set based on operation (deploy=true)
//
// # State Transitions
//
// Valid state transitions enforced by the Engine:
//
//	not_deployed ──Deploy()──> deployed_stopped ──Start()──> running
//	      ▲                           │                        │
//	      │                           │                        │
//	      └──────Undeploy()───────────┘                        │
//	                                                            │
//	                       deployed_stopped <──Stop()──────────┘
//
// Invalid transitions return errors.WrapInvalid.
//
// # Integration with Existing Systems
//
// The Engine reuses 100% of existing deployment infrastructure:
//
// Manager:
//   - Already watches semstreams_config KV
//   - Fires OnChange("components.*") when Engine writes
//   - No changes needed
//
// ComponentManager:
//   - Already creates/starts/stops components
//   - Already respects Enabled field
//   - No changes needed
//
// ComponentRegistry:
//   - Already contains factory metadata
//   - Engine queries for type information
//   - No changes needed
//
// # Error Handling
//
// Following pkg/errors patterns:
//
//   - WrapInvalid: Flow not found, validation errors, wrong state transitions
//   - WrapTransient: NATS KV errors, Manager errors
//   - WrapFatal: Marshaling errors (should never happen with valid Go types)
//
// # Testing Strategy
//
// Integration tests use real NATS and component registry:
//
//   - TestDeployFlow: Full deploy → verify configs written
//   - TestFullLifecycle: Deploy → Start → Stop → Undeploy
//   - TestStartNotDeployedFlow: Invalid state transition
//   - TestUndeployRunningFlow: Must stop before undeploy
//
// All tests use testcontainers (Constitutional Principle II).
//
// # Example Usage
//
//	// In FlowService (pkg/service/flow_service.go):
//
//	engine := flowengine.NewEngine(configMgr, flowStore, componentRegistry, natsClient, logger, metricsRegistry)
//
//	// User clicks "Deploy" in UI
//	err := engine.Deploy(ctx, "my-flow")
//	// Flow state: not_deployed → deployed_stopped
//	// Components created but not started
//
//	// User clicks "Start" in UI
//	err = engine.Start(ctx, "my-flow")
//	// Flow state: deployed_stopped → running
//	// Components now processing data
//
//	// User clicks "Stop" in UI
//	err = engine.Stop(ctx, "my-flow")
//	// Flow state: running → deployed_stopped
//
//	// User clicks "Undeploy" in UI
//	err = engine.Undeploy(ctx, "my-flow")
//	// Flow state: deployed_stopped → not_deployed
//	// Components deleted from runtime
//
// # Future Enhancements
//
// Validation integration:
//   - Currently validateFlow() is a stub
//   - Future: Integrate with existing flowgraph package
//   - Check connectivity, cycles, port compatibility
//   - Use component registry for port metadata
//
// # Package Structure
//
//	flowengine/
//	├── doc.go                       # This file
//	├── engine.go                    # Engine implementation
//	└── engine_integration_test.go  # Integration tests (9 passing)
package flowengine

# FlowEngine - Flow Deployment & Lifecycle Management

The engine package provides flow deployment and lifecycle management for SemStreams applications. It orchestrates component lifecycle, validates flows, and manages the complete deploy → start → stop → undeploy workflow.

## Overview

FlowEngine is the runtime execution environment for SemStreams flows. It coordinates with the component registry, configuration manager, and flow store to deploy and manage data processing pipelines.

## Quick Start

```go
package main

import (
    "context"
    "log"

    "github.com/c360/semstreams/component"
    "github.com/c360/semstreams/config"
    "github.com/c360/semstreams/engine"
    "github.com/c360/semstreams/flowstore"
    "github.com/c360/semstreams/natsclient"
)

func main() {
    ctx := context.Background()

    // Initialize dependencies
    natsClient, _ := natsclient.NewClient(ctx, cfg.NATS, logger)
    configMgr, _ := config.NewConfigManager(cfg, natsClient, logger)
    flowStore := flowstore.NewStore(natsClient, logger)
    registry := component.NewRegistry()

    // Create engine with metrics
    metricsRegistry := metric.NewMetricsRegistry()
    flowEngine := engine.NewEngine(configMgr, flowStore, registry, natsClient, logger, metricsRegistry)

    // Deploy a flow
    if err := flowEngine.Deploy(ctx, "my-flow-id"); err != nil {
        log.Fatal(err)
    }

    // Start the flow
    if err := flowEngine.Start(ctx, "my-flow-id"); err != nil {
        log.Fatal(err)
    }

    // ... flow is now running ...

    // Stop the flow
    flowEngine.Stop(ctx, "my-flow-id")

    // Undeploy (cleanup)
    flowEngine.Undeploy(ctx, "my-flow-id")
}
```

## Lifecycle Operations

FlowEngine enforces a strict state machine with four lifecycle operations:

### 1. Deploy
**Transition:** `not_deployed → deployed_stopped`

Deploys a flow by:
- Loading flow definition from flow store
- Validating flow structure and connections
- Translating flow nodes to component configurations
- Adding components to configuration manager
- Keeping components disabled (not started)

```go
err := flowEngine.Deploy(ctx, "flow-123")
```

### 2. Start
**Transition:** `deployed_stopped → running`

Starts all components in the flow:
- Enables components in topological order (inputs first, outputs last)
- Components begin processing data
- Metrics recorded for runtime monitoring

```go
err := flowEngine.Start(ctx, "flow-123")
```

### 3. Stop
**Transition:** `running → deployed_stopped`

Stops all running components:
- Disables components in reverse topological order (outputs first, inputs last)
- Graceful shutdown with context timeout
- Flow definition remains in configuration

```go
err := flowEngine.Stop(ctx, "flow-123")
```

### 4. Undeploy
**Transition:** `deployed_stopped → not_deployed`

Removes flow from configuration:
- Removes all component configurations
- Cleans up resources
- Flow must be stopped before undeploying

```go
err := flowEngine.Undeploy(ctx, "flow-123")
```

## State Machine

```
┌─────────────┐
│             │
│ not_deployed│
│             │
└──────┬──────┘
       │ Deploy
       ▼
┌─────────────────┐
│                 │
│ deployed_stopped│◄─┐
│                 │  │
└────┬────────────┘  │
     │ Start      Stop
     ▼               │
┌──────────┐         │
│          │         │
│  running │─────────┘
│          │
└──────────┘
```

## Flow Validation

The engine includes comprehensive flow validation before deployment:

### Structural Validation
- All nodes reference registered component types
- All connections specify valid ports
- No dangling connections (source/target must exist)
- No self-loops (component connected to itself)

### Type Validation
- Port types are compatible across connections
- Source output ports match target input ports
- Data type conversions are possible

### Semantic Validation
- Flows have at least one input component
- Flows have at least one output component
- No isolated components (must be connected)
- Topological sort possible (no cycles)

### Example Validation

```go
validator := engine.NewValidator(registry, natsClient, logger)
result, err := validator.ValidateFlow(ctx, flow)
if err != nil {
    log.Fatal("Validation failed:", err)
}

if len(result.Errors) > 0 {
    for _, e := range result.Errors {
        log.Printf("ERROR: %s - %s", e.Type, e.Message)
    }
}

if len(result.Warnings) > 0 {
    for _, w := range result.Warnings {
        log.Printf("WARN: %s - %s", w.Type, w.Message)
    }
}
```

## Architecture

The engine package consists of three main components:

### Engine
Core orchestration logic for lifecycle operations. Coordinates between:
- **Manager:** Runtime component configuration
- **FlowStore:** Persistent flow definitions
- **ComponentRegistry:** Available component types
- **NATS Client:** Message bus connectivity

### Validator
Flow validation logic using `component/flowgraph` for:
- Graph analysis and cycle detection
- Port compatibility checking
- Type validation
- Semantic correctness

### Translator
Converts flow definitions to component configurations:
- Maps flow nodes to component configs
- Resolves port connections to NATS subjects
- Generates unique component names
- Preserves flow metadata

## Error Handling

The engine uses SemStreams's error classification:

```go
import "github.com/c360/semstreams/errors"

err := flowEngine.Deploy(ctx, "invalid-flow")
if err != nil {
    var validationErr *engine.ValidationError
    if errors.As(err, &validationErr) {
        // Flow structure is invalid
        log.Println("Validation errors:", validationErr.Errors)
    }

    if errors.IsInvalid(err) {
        // Invalid input (e.g., flow not found)
        log.Println("Invalid request:", err)
    }

    if errors.IsTransient(err) {
        // Temporary failure (e.g., NATS timeout)
        log.Println("Retry possible:", err)
    }
}
```

## Configuration Integration

The engine integrates with the config package for dynamic component management:

```go
// Component configurations written to NATS KV
// Key pattern: components.{component-name}

{
  "type": "input/udp",
  "name": "flow-123-udp-input-1",
  "enabled": true,  // Controlled by Start/Stop
  "config": {
    "bind_address": "0.0.0.0:14550",
    "output_subject": "flow-123.udp.output"
  }
}
```

## Testing

The engine package includes comprehensive integration tests using testcontainers:

```bash
# Run all tests
go test ./engine/... -v

# Run with race detection
go test ./engine/... -race

# Run integration tests only
INTEGRATION_TESTS=1 go test ./engine/... -v
```

## Performance Considerations

- **Deploy/Undeploy:** O(n) where n = number of components
- **Start/Stop:** O(n) with sequential component state transitions
- **Validation:** O(n + e) where n = nodes, e = edges (topological sort)
- **Memory:** Flow definition size + component configs cached in Manager

For large flows (100+ components):
- Consider batching component configuration updates
- Monitor NATS KV operation latency
- Use context timeouts for bounded operations

## Related Packages

- [component](../component) - Component interface and registry
- [component/flowgraph](../component/flowgraph) - Flow graph analysis
- [config](../config) - Configuration management
- [flowstore](../flowstore) - Flow persistence
- [service](../service) - Service framework (includes FlowService HTTP API)

## Examples

See [engine_integration_test.go](engine_integration_test.go) for complete examples including:
- Full lifecycle testing (Deploy → Start → Stop → Undeploy)
- Error handling for invalid states
- Validation with real NATS connections
- Multi-component flow orchestration

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

## Desired Activation Operations

FlowEngine enforces a strict desired-state transition graph. These operations
persist configuration for the next successful boot; they never mutate the
currently running component topology.

### 1. Deploy

**Transition:** `absent → disabled`

Deploys a flow by:

- Loading flow definition from flow store
- Validating flow structure and connections
- Translating flow nodes to component configurations
- Adding components to configuration manager
- Keeping desired components disabled

```go
err := flowEngine.Deploy(ctx, "flow-123")
```

### 2. Start

**Transition:** `disabled → enabled`

Requests all components in the flow at the next boot:

- Sets desired component configuration to enabled
- Leaves the current process unchanged
- Reports `restart_required`

```go
err := flowEngine.Start(ctx, "flow-123")
```

### 3. Stop

**Transition:** `enabled → disabled`

Requests the flow disabled at the next boot:

- Sets desired component configuration to disabled
- Leaves current components running until normal process shutdown
- Preserves the flow definition

```go
err := flowEngine.Stop(ctx, "flow-123")
```

### 4. Undeploy

**Transition:** `disabled → absent`

Removes the desired component set:

- Removes all component configurations
- Does not tear down current runtime resources
- Desired flow must be disabled before removal

```go
err := flowEngine.Undeploy(ctx, "flow-123")
```

## State Machine

```text
┌─────────────┐
│             │
│    absent   │
│             │
└──────┬──────┘
       │ Deploy
       ▼
┌─────────────────┐
│                 │
│     disabled    │◄─┐
│                 │  │
└────┬────────────┘  │
     │ Start      Stop
     ▼               │
┌──────────┐         │
│          │         │
│  enabled │─────────┘
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

The engine writes desired component configuration for selection at the next
successful process boot:

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
# Run unit tests
go test ./engine/...

# Run with race detection
go test -race ./engine/...

# Run integration tests (requires Docker for testcontainers)
go test -tags=integration ./engine/...

# Run integration tests with verbose output
go test -tags=integration -v ./engine/...
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

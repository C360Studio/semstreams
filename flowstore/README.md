# FlowStore - Visual Flow Persistence

Persistent storage for visual flow definitions in the SemStreams framework.

## Overview

FlowStore manages the design-time representation of flows (canvas layouts, node positions, connections) separately from runtime component configurations. Flows are stored in NATS KV with optimistic concurrency control.

## Installation

```go
import "github.com/c360/semstreams/flowstore"
```

## Quick Start

```go
import "github.com/c360/semstreams/flowstore"

// Create store
store, err := flowstore.NewStore(natsClient)

// Create flow
flow := &flowstore.Flow{
    ID:   "my-flow",
    Name: "My First Flow",
    RuntimeState: flowstore.StateNotDeployed,
    Nodes: []flowstore.FlowNode{
        {
            ID:       "node-1",
            Type:     "udp",
            Name:     "UDP Input",
            Position: flowstore.Position{X: 100, Y: 100},
            Config:   map[string]any{"port": 5000},
        },
    },
}

err = store.Create(ctx, flow)
// flow.Version is now 1

// Update with optimistic concurrency
flow.Name = "Updated Name"
err = store.Update(ctx, flow)
// flow.Version is now 2
```

## Features

- **CRUD Operations**: Create, Read, Update, Delete flows
- **Optimistic Concurrency**: Version-based conflict detection
- **Validation**: Structural validation of nodes and connections
- **Integration**: Works with FlowEngine for deployment

## Architecture

**Design-time (flowstore):**
- User creates/edits flows in UI
- Canvas layout stored as Flow entities in `semstreams_flows` KV bucket

**Runtime (config package):**
- FlowEngine translates Flow → ComponentConfigs
- Deployed configs stored in `semstreams_config` KV bucket

## API Reference

### Core Types

```go
type Flow struct {
    ID           string          // Unique flow identifier
    Name         string          // Human-readable name
    Description  string          // Optional description
    Version      uint64          // Optimistic concurrency version
    RuntimeState RuntimeState    // Current deployment state
    Nodes        []FlowNode      // Visual nodes on canvas
    Connections  []FlowConnection // Edges between nodes
    CreatedAt    time.Time       // Creation timestamp
    UpdatedAt    time.Time       // Last update timestamp
}

type FlowNode struct {
    ID       string                 // Unique node ID within flow
    Type     string                 // Component type (udp, http, etc)
    Name     string                 // Display name
    Position Position               // Canvas coordinates
    Config   map[string]any // Component configuration
}

type FlowConnection struct {
    ID     string // Unique connection ID
    Source string // Source node ID
    Target string // Target node ID
}
```

### Store Operations

```go
// Create a new flow (version starts at 1)
func (s *Store) Create(ctx context.Context, flow *Flow) error

// Get flow by ID
func (s *Store) Get(ctx context.Context, id string) (*Flow, error)

// Update existing flow (optimistic concurrency via version)
func (s *Store) Update(ctx context.Context, flow *Flow) error

// Delete flow
func (s *Store) Delete(ctx context.Context, id string) error

// List all flows
func (s *Store) List(ctx context.Context) ([]*Flow, error)
```

## Optimistic Concurrency

FlowStore uses version-based optimistic concurrency control:

```go
// User A retrieves flow
flowA, _ := store.Get(ctx, "flow-1")  // Version: 5

// User B retrieves same flow
flowB, _ := store.Get(ctx, "flow-1")  // Version: 5

// User A updates successfully
flowA.Name = "Updated by A"
store.Update(ctx, flowA)  // Version becomes 6

// User B's update fails with version conflict
flowB.Name = "Updated by B"
err := store.Update(ctx, flowB)  // Error: version mismatch
// err contains: "expected 6, got 5"
```

## Validation

Flows are validated before Create/Update:

```go
flow := &flowstore.Flow{
    ID:   "",  // ❌ Empty ID
    Name: "My Flow",
}

err := store.Create(ctx, flow)
// Error: "flow ID cannot be empty"
```

**Validation Rules:**
- Flow ID and Name cannot be empty
- Node IDs must be unique within flow
- Node Type and ID cannot be empty
- Connection Source and Target must reference existing nodes
- No circular dependencies (future enhancement)

## Testing

```bash
# Unit tests
go test ./flowstore -v -short

# Integration tests (requires Docker)
go test ./flowstore -v

# Coverage
go test ./flowstore -coverprofile=cover.out
go tool cover -html=cover.out
```

Current coverage: **81.6%**

## Error Handling

FlowStore uses SemStreams's error classification system:

```go
err := store.Update(ctx, flow)
if err != nil {
    if errors.IsInvalid(err) {
        // Validation error or version conflict - fix input
        log.Printf("Invalid update: %v", err)
    } else if errors.IsTransient(err) {
        // Network/NATS error - retry with backoff
        log.Printf("Transient error: %v", err)
    }
}
```

**Error Classes:**
- `Invalid`: Empty IDs, version conflicts, validation failures
- `Transient`: NATS connectivity issues, context timeouts
- `Fatal`: JSON marshaling errors (should never occur)

## Integration with FlowEngine

```go
import (
    "github.com/c360/semstreams/flowstore"
    "github.com/c360/semstreams/engine"
)

// Store design-time flow
flow := &flowstore.Flow{...}
flowStore.Create(ctx, flow)

// Deploy to runtime
engine := engine.NewEngine(flowStore, configMgr, compRegistry, natsClient)
err := engine.DeployFlow(ctx, flow.ID)
// FlowEngine translates flowstore.Flow → config.ComponentConfig
```

## Example: Complete Workflow

```go
package main

import (
    "context"
    "log"

    "github.com/c360/semstreams/flowstore"
    "github.com/c360/semstreams/natsclient"
)

func main() {
    ctx := context.Background()

    // Setup
    natsClient, _ := natsclient.New("nats://localhost:4222")
    store, _ := flowstore.NewStore(natsClient)

    // Create flow with UDP input and HTTP output
    flow := &flowstore.Flow{
        ID:   "sensor-flow",
        Name: "Sensor Data Pipeline",
        RuntimeState: flowstore.StateNotDeployed,
        Nodes: []flowstore.FlowNode{
            {
                ID:       "udp-input",
                Type:     "udp",
                Name:     "Sensor Input",
                Position: flowstore.Position{X: 100, Y: 100},
                Config:   map[string]any{"port": 5000},
            },
            {
                ID:       "http-output",
                Type:     "httppost",
                Name:     "API Output",
                Position: flowstore.Position{X: 400, Y: 100},
                Config:   map[string]any{"url": "https://api.example.com/data"},
            },
        },
        Connections: []flowstore.FlowConnection{
            {ID: "conn-1", Source: "udp-input", Target: "http-output"},
        },
    }

    // Persist flow
    if err := store.Create(ctx, flow); err != nil {
        log.Fatalf("Failed to create flow: %v", err)
    }
    log.Printf("Created flow %s with version %d", flow.ID, flow.Version)

    // Update flow
    flow.Name = "Updated Sensor Pipeline"
    if err := store.Update(ctx, flow); err != nil {
        log.Fatalf("Failed to update flow: %v", err)
    }
    log.Printf("Updated flow to version %d", flow.Version)

    // List all flows
    flows, _ := store.List(ctx)
    log.Printf("Total flows: %d", len(flows))
}
```

## Documentation

- [Package Documentation](https://pkg.go.dev/github.com/c360/semstreams/flowstore)
- [Architecture Guide](../doc.go)
- [FlowEngine Integration](../engine/README.md)

## License

Copyright 2025 C360

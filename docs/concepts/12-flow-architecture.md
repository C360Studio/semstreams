# Flow Architecture

SemStreams supports two operational modes that share a unified Flow abstraction. Understanding how these modes work helps you choose the right approach for your deployment.

## Operation Modes

### Headless Mode (Static Config)

Start SemStreams with a JSON/YAML config file. Components load and run automatically:

```bash
semstreams --config config.json
```

- No UI required
- Components start on boot
- Ideal for production deployments, CI/CD pipelines
- Config changes require restart (or use KV override)

### UI Mode (Visual Flow Builder)

> **WIP**: The visual flow builder UI is under active development in the `semstreams-ui` repository, planned for beta release. Backend APIs are available now.

Design and manage flows through a visual interface:

- Drag-and-drop component placement
- Visual connection of ports
- Real-time deploy/start/stop control
- Live metrics and health monitoring

Both modes use the same underlying Flow abstraction, enabling seamless transitions.

## Two KV Buckets

SemStreams persists state in two NATS JetStream KV buckets:

| Bucket | Contents | Used By |
|--------|----------|---------|
| `semstreams_config` | Component configs, service configs | ComponentManager (runtime) |
| `semstreams_flows` | Visual flow definitions (canvas layout) | FlowService (UI API) |

The config bucket stores the runtime configuration that the ComponentManager watches and reacts to. The flows bucket stores visual flow definitions that the UI displays and modifies.

Runtime configuration can change operational behavior, but governed projection ownership is data-plane authority rather
than ordinary component config. Changes to entity patterns, predicate ownership, write modes, producer identity, or
foreign-edge behavior should follow the contract-migration model in
[Governed Semantic State](28-governed-semantic-state.md).

## Static Config → Flow Bridge

When you start with a static config file, SemStreams automatically creates a Flow entry in the flows bucket. This bridges the gap between headless and UI modes:

```text
First Boot (static config):
┌─────────────┐     ┌──────────────────┐     ┌────────────────┐
│ config.json │ ──► │ semstreams_config│ ──► │ ComponentMgr   │
└─────────────┘     │     KV bucket    │     │ (runs them)    │
                    └──────────────────┘     └────────────────┘
                              │
                    ┌─────────▼────────┐
                    │  Auto-converted  │
                    │  to Flow         │
                    └─────────┬────────┘
                              │
                    ┌─────────▼────────┐     ┌────────────────┐
                    │ semstreams_flows │ ◄── │ FlowService    │
                    │     KV bucket    │     │ (UI reads)     │
                    └──────────────────┘     └────────────────┘
```

This automatic conversion happens in the FlowService during startup, making static configs visible to the UI without manual intervention.

## KV Wins Precedence

On subsequent boots, **KV wins** over static config:

| Scenario | Behavior |
|----------|----------|
| First boot, static config has components | Create flow in KV |
| Subsequent boot, flow exists in KV | Use KV flow (ignore static config) |
| Flow deleted from KV | Re-create from static config |

This precedence pattern:
- Preserves UI customizations across restarts
- Allows "reset" by deleting the flow from KV
- Matches the existing config.Manager behavior

## Flow Lifecycle

Flows progress through defined states:

```text
not_deployed → deployed_stopped → running → deployed_stopped → not_deployed
                     ↓                            ↑
                   error ─────────────────────────┘
```

| State | Description | Available Actions |
|-------|-------------|-------------------|
| `not_deployed` | Design phase, not in runtime | Deploy |
| `deployed_stopped` | Pushed to config KV, not running | Start, Undeploy |
| `running` | Components actively processing | Stop |
| `error` | Deployment or runtime failure | Fix and redeploy |

Static configs start in `running` state since components are already active at startup.

## Flow Engine Operations

The FlowEngine handles lifecycle transitions:

### Deploy

Converts Flow → ComponentConfigs and pushes to config KV bucket:
1. Validate flow structure and connections
2. Build FlowGraph for port analysis
3. Convert nodes to ComponentConfigs
4. Persist to `semstreams_config` bucket
5. ComponentManager watches and creates components

### Start

Enables components and begins processing:
1. Update component `enabled` flags in config KV
2. ComponentManager reacts and starts components
3. Data begins flowing through the pipeline

### Stop

Pauses processing while preserving deployment:
1. Disable components in config KV
2. ComponentManager stops components gracefully
3. State preserved for restart

### Undeploy

Removes components from runtime:
1. Delete component configs from config KV
2. ComponentManager removes components
3. Flow returns to `not_deployed` state

## Visual Flow Concepts

### Nodes

Each FlowNode represents a component instance:

```json
{
  "id": "udp-input",
  "type": "udp",
  "name": "UDP Input",
  "position": {"x": 100, "y": 50},
  "config": {"port": 14550}
}
```

- `id`: Unique instance identifier
- `type`: Component factory name (e.g., "udp", "graph-processor")
- `position`: Canvas coordinates for UI layout
- `config`: Component-specific configuration

### Connections

FlowConnections define data paths between component ports:

```json
{
  "id": "conn-1",
  "source_node_id": "udp-input",
  "source_port": "data",
  "target_node_id": "processor",
  "target_port": "input"
}
```

At deployment, connections are validated against component port definitions.

### Automatic Layout

When converting static configs to flows, nodes receive automatic grid positions. Users can rearrange nodes in the UI as needed.

## When to Use Each Mode

| Use Case | Recommended Mode |
|----------|------------------|
| Production deployment | Headless (static config) |
| Development/debugging | UI mode |
| CI/CD pipelines | Headless (static config) |
| New flow design | UI mode |
| Operational monitoring | UI mode |

You can start in headless mode for initial deployment, then connect the UI later to monitor and adjust the running flow.

## Key Files

| File | Purpose |
|------|---------|
| `flowstore/store.go` | Flow persistence (NATS KV) |
| `flowstore/flow.go` | Flow, FlowNode, FlowConnection types |
| `flowstore/converter.go` | Static config → Flow conversion |
| `engine/engine.go` | FlowEngine lifecycle operations |
| `service/flow_service.go` | HTTP API for flow operations |
| `config/manager.go` | Config KV watching and precedence |

## Related Documentation

- [Configuration Guide](../basics/06-configuration.md) - Static config structure

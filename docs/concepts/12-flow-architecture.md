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

Runtime configuration can change operational behavior. Graph writers declare copied local projection contracts with
entity patterns plus reconcile or append groups; those contracts validate component intent and do not grant global
predicate ownership. Wire changes follow the typed mutation-port contract in
[Graph Mutation Contracts](28-governed-semantic-state.md).

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

Flows carry desired activation for the next successful boot:

```text
absent → disabled → enabled → disabled → absent
```

| Desired state | Description | Available Actions |
|-------|-------------|-------------------|
| `absent` | No desired component configuration | Deploy |
| `disabled` | Desired component configuration exists but is disabled | Start, Undeploy |
| `enabled` | Components are requested for the next boot | Stop |

Flow reads report `effective_state` independently. Without an authoritative
runtime observer it is `unknown`; it is never inferred from desired state,
admission, or health. `restart_required` compares the desired digest with the
sealed boot-applied digest and is `null` when no boot selection is available.

## Flow Engine Operations

The FlowEngine handles lifecycle transitions:

### Deploy

Converts Flow → ComponentConfigs and pushes to config KV bucket:
1. Validate flow structure and connections
2. Build FlowGraph for port analysis
3. Convert nodes to ComponentConfigs
4. Persist to `semstreams_config` bucket
5. Return `runtime_unchanged: true`; the next process boot selects the change

### Start

Enables desired components for the next boot:
1. Update component `enabled` flags in config KV
2. Current runtime remains unchanged
3. Response reports whether restart is required

### Stop

Disables desired components while preserving the definition:
1. Disable components in config KV
2. Current runtime remains unchanged
3. Normal process shutdown still drains and joins boot-owned work

### Undeploy

Removes components from desired configuration:
1. Delete component configs from config KV
2. Current runtime remains unchanged
3. Flow returns to `absent` desired state

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

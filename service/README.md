# Service package

The `service` package provides SemStreams service composition, the shared HTTP server, component boot orchestration,
health reporting, OpenAPI aggregation, and saved flow-diagram APIs.

## Lifecycle contract

A service receives its lifetime through `Start(ctx)` and joins its work in `Stop(ctx)`. Production structs do not
store contexts. The composition root starts the Config Manager before constructing runtime owners, then starts the
fixed service and component set selected for that process.

Configuration writes after boot are durable desired state for a future process. They do not create, remove, restart,
or replace services or components in the current process. Rule-definition hot reload is a separate processor-owned
capability and is not a generic service/component configuration hook.

## Dependencies

Constructors receive a `*Dependencies` value. Important shared dependencies include:

```go
type Dependencies struct {
    NATSClient        *natsclient.Client
    MetricsRegistry   *metric.MetricsRegistry
    Logger            *slog.Logger
    Platform          types.PlatformMeta
    Manager           *config.Manager
    FlowManager       *flowstore.Manager
    ComponentRegistry *component.Registry
    ServiceManager    *Manager
}
```

The Config Manager must have completed `Start` before ComponentManager or FlowService construction. Those owners read
the manager's defensive `BootConfig()` snapshot once.

## ComponentManager

ComponentManager constructs and starts enabled components from the sealed boot component map. It owns concrete
component handles, Start/Stop ordering, joins, and component observation. It does not watch desired component config
for live replacement.

Representative observation endpoints include component lists, health, admitted port facts, and connectivity analysis.
Runtime mutation APIs are intentionally absent.

## Saved flow diagrams

FlowService persists authoring artifacts: metadata, nodes, connections, audit fields, and CAS versions. A diagram does
not own runtime lifecycle.

```text
GET    /flowbuilder/flows
POST   /flowbuilder/flows
GET    /flowbuilder/flows/{id}
PUT    /flowbuilder/flows/{id}
DELETE /flowbuilder/flows/{id}
POST   /flowbuilder/flows/{id}/validate
POST   /flowbuilder/flows/{id}/publish-component-configs
```

Publishing validates and compiles the saved diagram, sorts component instance names, and upserts each candidate into
Config Manager. It never deletes a desired component because a node is absent. The response reports exact persisted
names, `runtime_unchanged: true`, and whether the desired component map differs from the sealed boot map.

Best-effort observations can be requested for the component names declared by a saved diagram:

```text
GET /flowbuilder/flows/{id}/observations/health
GET /flowbuilder/flows/{id}/observations/metrics
GET /flowbuilder/flows/{id}/observations/messages
```

These endpoints do not claim the diagram activated or owns those components. There is no diagram status stream or
diagram-associated log stream.

## Registration

Services register constructors explicitly. A typical service owns its configuration parsing and uses the injected
logger, metrics registry, NATS client, and platform identity:

```go
func Register(registry *service.Registry) error {
    return registry.Register("my-service", NewMyService)
}

func NewMyService(raw json.RawMessage, deps *service.Dependencies) (service.Service, error) {
    var cfg Config
    if err := json.Unmarshal(raw, &cfg); err != nil {
        return nil, fmt.Errorf("parse my-service config: %w", err)
    }
    return &MyService{logger: deps.Logger, config: cfg}, nil
}
```

Services that expose HTTP endpoints implement `RegisterHTTPHandlers` and `OpenAPISpec`. The service Manager mounts
handlers under the configured prefix and aggregates schemas into the generated OpenAPI document.

## Verification

Use explicit synchronization in lifecycle tests; arbitrary sleeps are not accepted. Run unit tests with the race
detector, integration tests against project-owned testcontainers, schema generation, lint, build, and contract tests
before integration. Breaking changes also require a relevant E2E tier before merge.

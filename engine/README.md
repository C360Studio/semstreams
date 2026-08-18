# Flow validation and compilation

The `engine` package validates saved flow diagrams and compiles their nodes into
detached component-configuration candidates.

```go
engine := flowengine.NewEngine(registry, natsClient, logger, metricsRegistry)

result, err := engine.ValidateFlowDefinition(flow)
configs, result, err := engine.Compile(flow)
```

Validation uses component factory declarations and `component/flowgraph` to
check node configuration, ports, connections, and resource conflicts. Compile
performs the same validation, then produces one enabled `ComponentConfig` per
node, keyed by component instance name.

Neither operation persists configuration or changes a running process. The
engine intentionally has no deploy, start, stop, or undeploy API. Publishing
compiled candidates is an explicit service operation, and published values are
eligible for composition only on a later process boot.

Run package tests with:

```bash
go test ./engine
```

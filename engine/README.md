# Flow Diagram Compiler

The `engine` package validates saved flow diagrams and compiles their nodes into component configuration candidates.
It does not deploy, start, stop, or otherwise own component lifecycle.

## Responsibilities

- validate diagram structure, component factories, ports, and connections;
- compile each node into an enabled `config.ComponentConfig` candidate;
- reject duplicate component names rather than silently overwriting a candidate;
- record validation metrics.

The package does not read or write configuration storage. The flow service owns the explicit
`publish-component-configs` operation that persists compiled candidates for selection at a later process boot.

## Validation

```go
validator := engine.NewValidator(registry, natsClient, logger, metricsRegistry)
result, err := validator.ValidateFlow(ctx, flow)
if err != nil {
    return err
}
if !result.Valid {
    // Present result.Errors and result.Warnings to the diagram author.
}
```

Validation covers node identity, registered component factories, port compatibility, connection endpoints, and graph
structure. It reports authoring feedback; it does not predict whether a running process owns the diagram's component
names.

## Compilation

```go
flowEngine := engine.NewEngine(registry, natsClient, logger, metricsRegistry)
candidates, result, err := flowEngine.Compile(flow)
```

`Compile` validates first and returns no candidates when the diagram is invalid. On success, the map is suitable for
an explicit upsert-only publish. Diagram omission never implies deletion from desired component configuration.

## Lifecycle boundary

Component topology is selected once at boot. Editing or compiling a diagram has no runtime effect. Publishing
candidates updates desired configuration only; the process must restart before a changed component map can become the
running topology.

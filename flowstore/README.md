# FlowStore - Visual Flow Persistence

`flowstore` persists design-time flow diagrams in the `semstreams_flows` NATS KV bucket. A diagram contains canvas
metadata, nodes, connections, audit timestamps, and a compare-and-set version. It does not contain deployment,
activation, provenance, component-bundle, or runtime status fields.

## Example

```go
store, err := flowstore.NewStore(ctx, natsClient, logger)
if err != nil {
    return err
}

flow := &flowstore.Flow{
    ID:   "my-flow",
    Name: "My First Flow",
    Nodes: []flowstore.FlowNode{
        {
            ID:        "node-1",
            Component: "udp",
            Name:      "telemetry-input",
            Type:      types.ComponentTypeInput,
            Position:  flowstore.Position{X: 100, Y: 100},
            Config:    map[string]any{"port": 5000},
        },
    },
}

if err := store.Create(ctx, flow); err != nil {
    return err
}
```

## Store contract

- `Create` persists a new diagram.
- `Get` returns one diagram by ID.
- `List` returns saved diagrams in deterministic order.
- `Update` uses the diagram version for optimistic concurrency.
- `Delete` removes only the saved diagram.
- `ImportFromConfig` creates an authoring diagram from component configuration without claiming lifecycle ownership.

Deleting a diagram does not delete component configuration. Omitting a node from a diagram does not disable or delete
the component with the same name. Publishing compiled candidates is a separate, explicit, upsert-only flow-service
operation.

## Concurrency

Two editors may read the same version. The first successful update advances the KV revision; the second receives a
conflict and must reload before retrying. This CAS version is diagram persistence metadata, not a runtime generation.

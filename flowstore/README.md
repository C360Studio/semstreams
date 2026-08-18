# Flow diagram persistence

The `flowstore` package stores author-authored diagrams in the
`semstreams_flows` NATS KV bucket. A saved flow contains identity, display
metadata, nodes, connections, version, and audit metadata. It does not contain
deployment or runtime lifecycle state.

```go
manager, err := flowstore.NewManager(ctx, natsClient)

flow := &flowstore.Flow{
    ID:   "example",
    Name: "Example",
    Nodes: []flowstore.FlowNode{
        {
            ID:        "input-node",
            Component: "udp",
            Type:      types.ComponentTypeInput,
            Name:      "udp-input",
            Config:    map[string]any{"port": 5000},
        },
    },
}
err = manager.Create(ctx, flow)
```

`Create`, `Get`, `List`, `Update`, and `Delete` affect diagram state only.
Updates use optimistic concurrency: the caller supplies the version it read,
and a stale update is rejected. A separate explicit service operation may
validate, compile, and publish component-config candidates for a later boot.

```bash
go test ./flowstore
```

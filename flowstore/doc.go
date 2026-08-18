// Package flowstore persists saved flow diagrams in NATS KV.
//
// A Flow contains authoring metadata, canvas nodes, connections, audit fields,
// and an optimistic-concurrency version. It does not contain runtime lifecycle
// state and does not own component instances. Creating, updating, or deleting a
// Flow changes only the semstreams_flows bucket.
//
// Flow.Validate checks required diagram fields, unique node IDs, complete node
// declarations, and valid connection references. Manager.Update uses the Flow
// version as a compare-and-swap guard.
//
// FromComponentConfigs is a one-way import helper for making an immutable boot
// component map visible as an editable diagram. It does not make the resulting
// Flow an activation authority.
package flowstore

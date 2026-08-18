// Package flowstore persists author-authored flow diagrams in NATS KV.
//
// Flows contain diagram identity, nodes, connections, version, and audit
// metadata. They deliberately contain no deployment or runtime lifecycle
// state. Create, Get, List, Update, and Delete operate only on this saved
// authoring state.
//
// Updates use optimistic concurrency: callers supply the version they read,
// and Manager rejects a stale version rather than overwriting a concurrent
// author. A separate explicit service operation may validate and compile a
// saved diagram into component-configuration candidates. Diagram persistence
// alone never changes the running component set.
package flowstore

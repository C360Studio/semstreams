# Graph Retention Guardrail (ADR-068 D1)

## Why

ADR-068 (D1) bans NATS KV TTL/MaxBytes/MaxAge as a lifecycle mechanism on the
live graph: age/size eviction is reachability-blind and would silently drop an
entity that still has live inbound edges — the "NATS dumbly deletes entities"
catastrophe.

Today the danger is latent but LIVE. `graph/query/client.go` `DefaultConfig()`
defaults `ENTITY_STATES.TTL = 24h` (and `INCOMING_INDEX.TTL = 24h`,
`SPATIAL_INDEX.TTL = 1h`). NATS KV bucket creation is get-or-create, so whichever
process creates the bucket first wins its config. `graph-ingest` (the sole
authoritative writer) currently creates `ENTITY_STATES` with no retention and
wins the race — so nothing expires. But if boot order ever flips (the query
client boots first), every entity silently expires 24h after its last write.
Nothing detects this today.

This is the first increment of ADR-068 and the first exercise of the OpenSpec
loop for this repo.

## What Changes

- **Remove the landmine.** `DefaultConfig()` sets the shared graph-bucket TTLs
  (`ENTITY_STATES`, `SPATIAL_INDEX`, `INCOMING_INDEX`) to `0`. The query client is
  a reader; `graph-ingest` owns the authoritative bucket's retention.
- **Add a boot-time guardrail.** After `graph-ingest` ensures `ENTITY_STATES`, it
  reads the bucket's actual backing-stream retention and **fails to start** if
  `MaxAge` (TTL) or a binding `MaxBytes` is set — catching the case where some
  other process won the create race with retention config. Fail-closed: a graph
  with a TTL is a catastrophe; refuse to boot rather than silently expire data.
- **Seed the `graph-retention` capability spec** with the invariant.

## Non-goals

- The rest of ADR-068 (delete-as-refuse/cascade, tombstones, per-entity reverse
  index, GC worker) — later increments.
- A static/lint guard covering *arbitrary future* graph buckets — the boot
  assertion covers `ENTITY_STATES` at runtime; a broader structural guard (a
  graph-bucket-creation helper that cannot express retention) is a follow-up.
- `History` tuning — History bounds revisions per key (audit tail), not
  lifecycle eviction; out of scope for D1 (ADR-068 D2/D4 may raise it later).

## Consumers

`graph-ingest` (owner), `graph-query` (the landmine source). No product-facing
API change; entirely internal correctness + a boot invariant.

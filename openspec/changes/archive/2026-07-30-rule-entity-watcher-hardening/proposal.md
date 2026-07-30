## Why

Rule entity watches combine overlapping NATS KV subscriptions, debounce, live configuration replacement, bootstrap
replay, deletion transitions, and stateful actions. The implementation in `f3adabb8` hardened those mechanics while
also enforcing canonical watch patterns. The pattern language belongs to `entity-id-contract`; the concurrency and
ordering behavior does not. Keeping both in one review would hide engine-level failure modes behind an identity
contract.

The generalized hardening already exists on `codex/entity-id-contract-completion`. This change extracts a fixed
review frame for that code: prove authoritative-state gating, watcher-generation authority, per-entity ordering,
bounded deduplication state, and shutdown cleanup before the implementation is allowed to land.

## What Changes

- Gate every pattern watcher behind one authoritative `ENTITY_STATES` WatchAll validator and its revision watermark.
  Contract poison latches rule evaluation off as reset-required; unexpected watcher loss degrades the lane.
- Replace watcher ownership by transport presence with exact watcher generations. Dynamic configuration prepares all
  additions before committing the desired set, retires authority before physical Stop, and rejects stale decoded or
  queued callbacks.
- Carry watcher-generation provenance through debounce. Overlapping active watchers coalesce to one entity fetch and
  evaluation, while a retired generation cannot be laundered through a newly added identical pattern.
- Serialize fetch, evaluation, delete transition, and cleanup per entity. Suppress same/lower revisions, process one
  delete revision once, and remove queued updates only after the delete passes the revision fence.
- Bound idle revision watermarks by a 15-minute TTL and 65,536-entry LRU cap while never evicting queued or in-flight
  entries. Drain queued references and clear idle watermarks at shutdown.

## Non-goals

- Defining or validating entity-ID, watch-pattern, predicate, or PackID syntax.
- Changing rule condition/action semantics or inventing a new delivery guarantee beyond the NATS KV revisions used by
  the watcher.
- Making the fence TTL an operator retention setting or a substitute for KV history.
- Generalizing the typed watcher to arbitrary operational KV buckets.

## Capabilities

### New Capabilities

- `rule-entity-watching`: fail-closed authoritative gating, atomic watcher replacement, provenance-aware coalescing,
  per-entity revision ordering, and bounded cleanup.

## Dependencies

- `entity-id-contract` owns canonical ENTITY_STATES values and watch-pattern validation. This change consumes those
  typed inputs and does not reopen their grammar.

## Impact

- **Framework code:** `processor/rule/entity_watcher.go`, `entity_evaluation_fence.go`, `message_handler.go`, processor
  lifecycle state, `pkg/cache/coalescing_set.go`, entity-watcher tests, and entity-watching documentation.
- **Runtime behavior:** malformed authoritative state disables the rule graph lane; unexpected watcher closure reports
  degraded; dynamic watcher replacement and deletes have stronger ordering guarantees.
- **Operations:** the idle fence is bounded internal deduplication state, not retained graph data or a tunable policy.

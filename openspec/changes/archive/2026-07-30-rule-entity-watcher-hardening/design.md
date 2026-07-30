## Context

One entity update may arrive through several overlapping pattern watchers. A debounce callback may run after its
watcher was removed, a delete may race an already-started current-state fetch, and bootstrap pattern streams may
overtake discovery of malformed state outside their patterns. The previous transport-list ownership and entity-key
coalescing did not carry enough provenance or ordering state to close those races.

The implementation exists in `f3adabb8`; this design records the invariants it must satisfy during extraction and
review. Pattern validation and configuration migrations remain with `entity-id-contract`.

## Goals / Non-Goals

**Goals:** no rule work from unvalidated authoritative state, no work from retired watcher generations, one ordered
evaluation stream per entity, exactly-once handling of an observed delete revision across overlapping watchers,
bounded idle dedupe memory, and complete release of queued references at shutdown.

**Non-goals:** exactly-once delivery across process loss, configurable retention, arbitrary-bucket decoding, or a new
entity/predicate identity contract.

## Decisions

### 1. Authoritative WatchAll is the validation and revision barrier

The processor starts an `ENTITY_STATES` WatchAll guard even when no pattern-specific rule watches exist. Pattern
watchers wait for the guard's clean bootstrap sentinel, and revision `R` cannot evaluate until the guard has processed
cleanly through at least `R`. A typed state-contract error latches reset-required, stops all later evaluation and
actions, and requires operator wipe/restart/reseed. An unexpected transport close is degraded, not misclassified as
poison; cancellation and intentional retirement are expected closes.

The final rule-evaluation seam rechecks readiness because poison can race work that has already passed an earlier
guard. No evaluation metric, state transition, or action derives after reset-required or degraded is visible.

### 2. Watcher authority is an exact generation, not a live transport

Each registered `(bucket, pattern)` watcher receives a monotonically increasing generation and a private context.
Dynamic replacement validates the requested set and prepares every addition before publishing any change. Preparation
failure stops prepared transports and leaves the prior configuration and authority intact.

Commit holds the dispatch write gate while it registers additions, publishes the cloned desired configuration, and
retires removed generation records. Physical Stop happens after authority is removed. A Stop error is reported but
cannot keep the retired generation authoritative. Callback dispatch holds the corresponding read gate through the
final authority check and queue insertion.

### 3. Debounced work carries watcher provenance

Managed pending keys encode entity ID, watcher key, and generation as internal work identity. The coalescer still
deduplicates exact work items, and its callback groups authorized items by entity so overlapping active patterns cause
one current-state fetch and one evaluation. At least one exact still-active provenance is required before fetch.
Re-adding the same pattern creates a new generation and cannot authorize work queued by the retired generation.

Bootstrap entries bypass debounce so `Bootstrap=true` reaches stateful OnRecovery behavior. Live work is coalesced;
An admitted delete removes both legacy entity-only keys and all provenance-bearing keys for that entity before delete
evaluation. The revision fence runs first: a stale delete cannot purge newer queued work from an overlapping watcher.

### 4. One per-entity fence orders fetch, evaluation, delete, and cleanup

Queued and in-flight work retains a per-entity fence entry. The entry lock is held across current-state fetch,
evaluation, delete transition, and state-tracker cleanup. Its watermark admits only revisions newer than the latest
completed revision; a revisioned delete is therefore processed once across overlapping watchers. Revision-zero
synthesized deletes are separately deduplicated until a later revisioned non-delete resets that state.

The lock order is dispatch gate, then entity fence. Deletes use the same fence and cannot overtake a fetch that has
already started; if delete completes first, its watermark suppresses an older fetched snapshot.

### 5. Active state is never evicted; idle state is bounded

Fence entries with queued or in-flight references are active and cannot be evicted. When the last reference leaves,
the watermark enters an idle LRU retained for 15 minutes and capped at 65,536 entities. TTL and LRU bound memory while
covering ordinary overlap and reconnect replay; eviction may shorten dedupe history and does not change source truth.

Shutdown retires every watcher generation, closes the coalescer, drains pending keys without callbacks, releases their
fence references, clears idle watermarks, and fails cleanup if any active reference remains.

## Risks / Trade-offs

- The authoritative WatchAll duplicates delivery beside pattern watchers, trading bandwidth for fail-closed coverage
  of poison outside configured patterns.
- Fixed idle limits make dedupe best-effort beyond the horizon; correctness continues to come from current KV state
  and revisions, not retained fence entries.
- Generation/provenance keys are internal and contain NUL separators; they must never become NATS keys, logs, metrics,
  or public API values.
- Lock-order regressions can deadlock configuration replacement against delete/evaluation; race tests must exercise
  the gates rather than rely on sleeps.

## Validation Strategy

Use deterministic seams around guard progress, pre-dispatch, pre-fence acquisition, and current-state fetch. Prove
both poison orderings during bootstrap, a later valid pattern revision blocked behind earlier poison, expected versus
unexpected watcher closure, prepared-addition rollback, retirement despite Stop failure, stale-generation rejection
before fetch, overlapping watcher coalescing, fetch/delete total ordering, concurrent and sequential delete dedupe,
same/lower revision suppression, active-entry non-eviction, TTL/LRU bounds, and shutdown drain to zero references.

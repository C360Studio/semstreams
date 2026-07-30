## Why

SemStreams can defer physical semantic GC for v1, but it cannot ship with storage growth that is
unbounded, invisible, and discovered only when JetStream starts rejecting writes. Today there is no
account-wide view of storage at all: `natsclient/jetstream_metrics.go:14` tracks "only streams and
consumers that are created/accessed through this client", there is no `KV_*`/`OBJ_*` enumeration
anywhere, and no storage doctor exists in the repo.

**This change supersedes `bounded-storage-operability`, which is retired.** That change proposed the
same operational envelope but rested on a premise that direct measurement falsified: that a verified
`DiscardNew` `MaxBytes` ceiling on graph KV is a safe fail-closed circuit breaker, "permitted only
when SemStreams... reserves configured replacement/recovery headroom."

Measured against real NATS 2.12 (KV bucket, `MaxBytes` 128 KiB, `History` 1, 1 KiB values, filled to
rejection):

| operation at the ceiling | result |
|---|---|
| write a new key | rejected — `code=503 err_code=10077 maximum bytes exceeded` |
| **replace an existing key** | **rejected — same error** |
| delete a key | succeeded |
| purge a key | succeeded |
| write a new key after purge | succeeded |

`DiscardNew` evicts nothing, so the retired change was right about eviction and wrong about
consequences. Three findings kill the ceiling as a graph-KV primitive:

1. **The reserved-headroom mechanism does not exist in NATS.** Replacement is the *first* operation
   to fail. With `History` 1 a same-size replacement is net-zero bytes — the superseded revision
   compacts away — but the append is checked against `MaxBytes` before that compaction, so it is
   refused anyway. `MaxBytes` is a single limit checked on append with no reservation concept, so
   "reserve headroom for replacement" is not expressible. Keeping replacement alive would require a
   separate mechanism holding the bucket below its ceiling, at which point that mechanism is doing
   the work and the ceiling is decoration.
2. **Per-bucket ceilings tear cross-bucket consistency.** graph-ingest writes one entity across many
   independently-ceilinged catalog buckets (`ENTITY_STATES` plus the predicate, incoming, outgoing,
   name, alias, and context indexes). They fill at different rates, so the first to reach its ceiling
   denies its half of the write while the others accept theirs, and NATS has no cross-bucket
   transaction to make that atomic. The result is not "growth stopped" but authoritative state and
   derived indexes diverging silently at a nondeterministic point — worse than disk-full, which
   fails uniformly and loudly.
3. **It inverts the ADR-068 contract.** At the ceiling, deletion — the semantic operation the graph
   reserves to itself — still works, while updating current state is denied by storage policy. The
   storage layer makes the lifecycle decision after all, in the opposite shape from the one the ban
   was written to catch.

`docs/adr/073-graph-ingestion-retention-contract.md:77-79` already banned all `MaxBytes>0` on the
identity tier. That decision stands and is now independently confirmed; the retired change's task to
"update ADR-068/073 wording" would have re-opened it.

What survives is the operationally useful half, and it is entirely unbuilt: **see the growth, project
the exhaustion, and warn the operator with enough lead time to correct resources.** Denying writes is
not a capacity strategy — it is what happens when the capacity strategy was never given a chance to
run.

## What Changes

- Add an account-wide storage inventory covering ordinary JetStream streams, `KV_*` backing streams,
  and `OBJ_*` ObjectStores: logical owner, configured limits, actual usage, growth rate, headroom,
  and projected time-to-threshold from one SemStreams surface. KV owner attribution is read from the
  bucket descriptor catalog (`graph.KVCatalog`), not re-derived.
- Derive `normal`/`warning`/`high`/`critical` pressure states from configurable thresholds and
  projected time-to-full, and surface them through Prometheus metrics, component health, and logs.
  **Pressure is REPORT-ONLY in this change** — nothing rejects, throttles, or degrades on it.
- Publish an operator-facing storage doctor report: every resource, its bound (or its absence), its
  headroom, its projected time-to-threshold, and the owner responsible.
- **Refuse to provision, bound, or reconcile any `KV_*` or `OBJ_*` backing stream from the stream
  provisioner, failing closed by name prefix.** This is the load-bearing safety boundary of the
  change: the provisioner accepts operator-authored stream names with no filter today
  (`config/streams.go:254`, `:289`, `createStream` at `:358`), and extending its reconciler to
  retention fields is exactly what would let an operator typo stamp age eviction onto graph state.
  The refusal is by prefix rather than by catalog membership, because every downstream safety net has
  a hole — the retention reconciler never clears a discard policy, it does not run at all for a
  descriptor declared retention-unmanaged, and buckets outside the catalog have no seam.
- Require **explicitly declared** finite `MaxAge`, finite `MaxBytes`, and discard policy on ordinary
  time-shaped streams — the lane where age eviction is semantically correct — and reconcile editable
  retention drift on existing streams. Today `config/streams.go:23` treats `MaxBytes 0` as unlimited,
  `:430` hardcodes `Discard: DiscardOld` so the policy is never the operator's choice, `:387,390`
  silently defaults `MaxAge` to 7d, and the drift reconciler at `:435-471` repairs only subjects and
  the duplicate window while ignoring retention drift entirely.
- Add an **expiring migration override** so the bounds requirement is landable rather than a flag
  day: every component-derived stream is created today with no declared bounds
  (`config/streams.go:303-306`). An override names the resource, its owner, and an expiry; readiness
  reports active overrides and fails once one expires, and an override without an expiry is rejected
  so a bridge cannot become permanent.
- Add an **archival classification** for a stream whose contract is permanence (#729), structurally
  distinct from the expiring override. Forcing an archive through the override would mean renewing it
  forever, which trains operators to renew without reading and destroys the signal value that makes
  genuinely time-limited overrides worth having. Archival streams stay fully inventoried and are
  measured against the account tier ceiling, since that is their only remaining limit.
- Follow the bounds requirement to **every provisioning seam at creation**, and make an existing-stream
  bind **report** declared-versus-observed divergence instead of discarding the declaration (#730).
  `natsclient/stream.go:141-145` currently returns an existing stream and drops the caller's config in
  silence, so a stream two components declare has its limits fixed permanently by boot order with no
  diagnostic. Binding never restamps — a non-owner rewriting another owner's configuration is worse
  than the drift it would correct.
- Report unknown capacity as **unknown** — never as unlimited, and never as healthy.

## Non-goals

- **NATS-level `MaxAge` or binding `MaxBytes` on graph KV buckets or content ObjectStores.** The
  ADR-068/073 ban stands, reaffirmed by the measurement above. This change adds no retention Kind to
  the bucket descriptor catalog and changes no catalog row's declared policy.
- **Any enforcement action derived from pressure state.** No admission control, no throttling, no
  write rejection, no readiness degradation. Pressure observability must exist and be trusted before
  anything is allowed to act on it.
- Physical semantic entity purge, cascade delete, mark/sweep, or ObjectStore reachability GC.
- Product-specific retention periods, entity quotas, or data classifications.
- Fixing the ObjectStore write-acknowledgement bug (`storage/objectstore/component.go:768-779` acks
  unconditionally after a call that cannot report failure). It is a live correctness defect,
  independent of capacity, and is filed separately.

## Deliberately deferred, and what unblocks it

**Application-level capacity admission** — rejecting an entity write in graph-ingest *before* any
bucket is touched, so the rejection is entity-atomic and cross-bucket-coherent — is the correct home
for backpressure, and is the thing NATS `MaxBytes` structurally cannot do. It is deferred, not
rejected, and it is gated on this change landing: admission is only safe once operators have a
trustworthy pressure signal with enough lead time to add capacity, and only if the rejection path is
proven to classify transient and NAK rather than ack-and-drop. Designing it against an unmeasured
system is how the retired change went wrong.

## Capabilities

### New Capabilities

- `storage-observability`: account-wide inventory, capacity budgets, pressure states, alerting, and
  operator diagnostics across streams, KV, and ObjectStores — report-only.
- `stream-provisioning`: which streams SemStreams provisions, the prohibition on touching KV and
  ObjectStore backing streams, explicit bounds and discard policy, expiring migration overrides, and
  drift reconciliation.

### Modified Capabilities

None. Stream provisioning is deliberately NOT filed under `nats-streaming`, which is a publish-path
capability (PubAck blocking, MsgID dedup, async futures, batching, circuit breaker) whose Purpose is
still an unfilled stub. Provisioning lives in `config/streams.go`, not `natsclient`, and giving it
its own capability keeps the publish-path spec from silently widening.

## Impact

- **Framework code:** `config/streams.go` (finite bounds, operator-chosen discard, drift
  reconciliation), `natsclient/jetstream_metrics.go` (inventory beyond client-touched streams),
  new inventory/pressure surface in `natsclient`, `graph/kvcatalog.go` (read-only: owner attribution
  for `KV_*` resources), `storage/objectstore` (capacity reporting only).
- **Operator surface:** storage doctor output, Prometheus metrics for usage/headroom/growth/
  time-to-threshold, pressure-state gauges, and pressure runbooks.
- **Consumers:** every product creating persistent graph identities or large content — SemSource,
  SemBoids, SemConnect, SemOps.
- **Not touched:** the bucket descriptor catalog's declared policies, the retention guard, the
  acquisition seam, and ADR-068/073.
- **Verification:** unit tests for growth projection and pressure transitions (including unknown
  capacity), real-NATS integration tests for inventory enumeration and stream drift repair, and a
  JSON round-trip test for the operator-facing threshold configuration.

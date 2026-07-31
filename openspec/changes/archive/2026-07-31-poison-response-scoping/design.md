# Design — Poison Response Scoping

> Revised 2026-07-18 post-adversarial-review. Verdicts and finding dispositions:
> `adversarial-review.md`. Decisions D7–D10 are new; D1–D6 carry review-mandated corrections.

## Context

The fail-closed wave (beta.147–.150) gave ENTITY_STATES layered poison defense. Two layers are
enforcement: the marshal-site write gate (all 9 write sites verified — nothing invalid commits)
and per-read validating decode (every query lane and every external consumer — nothing invalid
is served; the review could not construct a serve or launder counterexample). The rest is
detection and response: a boot snapshot sweep, **three dedicated live `WatchAll` contract
guards** added by the same commit (`cba784ea`) — graph-ingest `component.go:1184`, rule
`entity_watcher.go:50` (unconditional even with zero entity-watch patterns), clustering
`component.go:1095` (second watcher beside its input watcher) — and graph-ingest's
surface-global sticky latch. Each guard re-delivers every entity write full-payload on the
shared connection and re-runs the full validating decode: three deliveries + three decodes per
mutation in a semboids-shaped deployment, matching their measured ~+3 msgs/entity (gh#562).

Grounding facts (all review-verified): the old latch only ever gated graph-ingest's own query
lanes; aggregates already fail-the-batch and materialize every per-item error;
`DeleteEntity` never decodes (works on poison); `Component.UpdateEntity` has zero production
callers (delete+recreate is the only wire repair); ENTITY_STATES History=1; mutation read seams
at `mutations.go:717/:1060/:552` misclassify resident poison as retryable-internal; arrivals
for a poisoned entity are Term'd (permanent loss); `graph/query/client.go` is a fourth
in-window watcher + whole-client latch whose cache depends on its watcher (carved out, D10);
no in-repo consumer restarts anything on component `Healthy=false`.

## Goals / Non-Goals

**Goals:**

- Remove all three dedicated contract-guard watchers (zero read-shaped per-mutation delta vs
  beta.146 in a semboids-shaped deployment) while keeping boot-time and consume-time detection.
- Scope graph-ingest's poison response per-entity with an alertable, enumerable, self-healing
  operator signal; correct the mutation-seam classifications and arrival disposition.
- Keep every enforcement point byte-authoritative and fail-closed.

**Non-Goals:** auto-delete/auto-repair; weakening write gate or per-read decode; projection
owners' sticky contract (incl. the carved-out query client, D10); query-client migration
(follow-up); inventory persistence (boot sweep rebuilds; bytes are the durable truth).

## Decisions

### D1 — Snapshot-then-stop, with drain-to-close (graph-ingest)

`startEntityStateGuard` keeps its synchronous snapshot drain (validate every pre-marker entry),
then stops the watcher. **Mandatory shape** (review C3b — the naive forms are a connection
wedge or a misclassification): after the nil marker, call `watcher.Stop()`, then **continue
reading the updates channel until it closes**, discarding entries, and never treat that closure
as watch loss. Rationale: the nats.go update callback blocks on a full channel holding `w.mu`;
an unread channel wedges the connection's async-callback dispatcher via `SetClosedHandler`.
The watcher becomes a local of `startEntityStateGuard` (the `entityStateWatcher` component
field and its Stop() teardown are deleted — nothing else reads them). `runEntityStateGuard` is
deleted. `entityWatchLost` remains only for genuine boot transport failure (create failure or
pre-marker closure), preserving today's recovery contract.

Drain bookkeeping is **last-revision-wins per key**: a key whose poisoned snapshot revision is
superseded by a valid pre-marker delivery ends with no inventory entry. Snapshot-marker
completeness leans on ENTITY_STATES History=1 (per-subject limit 1 means gap-resets cannot
inflate the received count) — recorded as an explicit assumption; raising history invalidates
the marker math. Known window (accepted, documented): a revision committed by **another
process** after the marker is unvalidated until first-touch or next boot — the out-of-band
class. The drain runs with no deadline and Start holds `c.mu` throughout (Health blocked) —
pre-existing, unchanged; progress logging only if it ever bites.

*Rejected:* dedicated-connection watcher (keeps fan-out + decode, buys half); MetaOnly (cannot
validate).

### D2 — Enforcement stays byte-authoritative; the inventory is observability-only; the dead latch protocol is deleted

Reads and RMW enforce against actual stored bytes at their own seams; the per-entity inventory
is consulted by **no** read or write decision. With the global latch gone, the entire
commit-point ceremony it existed to serialize is dead code and is **deleted outright** (not
narrowed): `entityQueryMu`, `finalizeEntityQueryResponse`, `checkEntityQueryReady`'s lock
discipline, `latchEntityStatePoison`, `beforeEntityQueryResponse` test hook. Readiness becomes
an atomic-bool check at handler entry (`entityWatchLost`/bootstrap flags settle inside Start
before any subscription registers — review-verified no ordering window).

Steady-state hot-path cost is **required** to be a single atomic load: the inventory keeps an
`atomic.Int64` size; clear-on-commit fast-paths out when zero before touching the mutex
(check-then-lock race benign — re-check under lock). Known accepted imperfection: cache-hit
reads (30s TTL) don't decode bytes; on inventory-record the entity's cache entry is deleted
(hygiene, like existing write-path invalidations — not an enforcement use of the inventory),
bounding the out-of-band stale-serve window to detection latency instead of TTL.

### D3 — Inventory hygiene: revision-stamped entries; clear on delete, newer commit, or successful validating read

Each inventory record carries the KV revision whose decode failed (every detection site holds
the entry or CAS revision). Clear paths: (a) `DeleteEntity`; (b) a successful commit to the key
with a **newer revision** than the recorded one — the revision guard closes the
record-after-clear interleaving (concurrent repair Put vs in-flight RMW classification) that
would otherwise leave a stale-unhealthy entry; (c) **any successful validating read** of the
key — closes the out-of-band-valid-overwrite and out-of-band-purge stuck-Health holes for
free (the read already ran the decode). Repair therefore recovers Health without restart in
both in-band and out-of-band directions. The wire repair path is **delete + recreate only**
(`UpdateEntity` has no production callers; every other mutation verb refuses a poisoned
resident). Divergence from projection owners' process-lifetime sticky contract is deliberate
and per D10's classification.

### D4 — `StateContractError.EntityID`, stamped where identity actually lives

Additive field. Stamping happens **inside the closures/goroutines where the entity ID is in
scope**, not at the outer classification helpers (which receive bare errors): the enumerated
sweep list is `query.go:579` (batch fetch goroutine), MergeEntity closure (both branches),
AddTriple closure, AddTriples closure (whose current `casErr.Error()` stringification also
loses the type — fixed to propagate the typed error), RemoveTriple closure,
`update_with_triples` closure, `fetchEntityState` callers (the three mutation seams), boot
sweep (`entry.Key()`). Sweep-all-emitters applies. Wire shape: unchanged (only class/code
headers cross the wire — review-verified; no consumer string-matches beyond the code).

### D5 — Aggregates fail loudly, completely, in one attempt

A multi-entity read that encounters poisoned entities fails as a whole with the typed error
naming **every** poisoned entity encountered (bounded list), and records **all** of them into
the inventory in that same attempt. Free: the batch already materializes every per-item error;
the fix is an O(n) walk at the merge point. Kills the one-repair-per-round-trip discovery
loop. The three mutation read seams (`entity.update`, `update_with_triples` CAS,
`create_with_triples` restamp) return the typed fatal classification, not retryable-internal.
The suffix lane resolves IDs without decoding entity bytes — explicitly accepted and specced
(resolution is not serving state).

### D6 — Operator signal: Health, gauge, enumeration

While the inventory is non-empty: `Healthy=false`, `Status="degraded"` (NOT `reset_required`,
which stays a per-read error code), message carries count + first-10 IDs + bounded reasons.
One gauge, never per-entity labels. Full enumeration via `DebugStatus()`
(`DebugStatusProvider` exists) so a 10k-event is enumerable in-band. Alerting guidance points
at the gauge and Health message — `/components/health` collapses to a binary 503 and cannot
distinguish one-poisoned-entity from component-down (noted in runbook). Detection is
first-touch or boot, not write-time — the runbook says so plainly; write-time detection for
consumed values persists naturally at the consumers that read them (D7).

### D7 — One principle retires all three guards: validation rides existing read points

- **rule**: `startGraphStateGuard` (full-firehose, unconditional) is removed. Rules validate
  the entity values they actually consume on their existing input path; the sticky
  rule-evaluation kill switch fires on **consumed** poison exactly as before (the sibling
  change's "action/evaluation consumers MUST emit no derived output from poisoned state" is
  satisfied — a value never consumed cannot produce derived output). With zero entity-watch
  patterns the rule processor no longer pays ENTITY_STATES firehose fan-out for a feature it
  doesn't use.
- **clustering**: `startEntityContractWatch` is removed. Implementation correction to the
  original premise (proved by TDD red run): clustering never held an input watcher — the
  contract guard was its ONLY ENTITY_STATES watcher, and its real input path is timer-driven
  polled reads whose decode errors were previously swallowed. The sticky projection latch is
  now wired at the consuming querier seam (every authoritative decode failure latches), giving
  consume-time detection within one detection interval (default 30s; first cycle covers the
  boot snapshot). Post-change clustering holds ZERO ENTITY_STATES watchers and pays zero
  per-write deliveries.
- **graph-ingest**: D1.

### D8 — Resident-poison arrivals are Nak'd, not Term'd

`processIngest` disposition split by fault: a structurally-invalid **candidate** (the message's
own fault, can never succeed) stays Term; a **resident-poison** classification
(`StateContractError` from the RMW read — the environment's fault, repairable) becomes Nak,
bounded by the consumer's MaxDeliver. Valid arrivals during a repair window survive to apply
after repair instead of being silently destroyed. MaxDeliver exhaustion is the loss backstop
and is visible in stream metrics.

### D9 — agentic-loop rescope: per-loop failure, not component latch

`graph.IsStateContractError` on a query result currently latches component-wide
`graphStateReset` and holds task intake until restart. Under per-entity semantics the code
means "this entity", so the reaction becomes per-loop: the loop that touched the poisoned
entity fails with the typed error; task intake and other loops continue. The
hold-until-restart machinery for this class is removed.

### D10 — The direct query client is classified, not migrated

`graph/query/client.go` serves reads from a cache maintained by its own ENTITY_STATES watcher —
that makes it a **watch-maintained derived-view reader**: projection-owner semantics
(whole-client sticky latch, restart recovery) are legitimate for it, exactly like graph-index.
It is out of scope here because snapshot-then-stop does not transplant (its cache would go
incoherent without the watcher). Its per-write delivery+decode tax across five embedding
consumers (graph-query, graph-gateway, agentic-tools, research-graph-classify, fusionnats; and
semsource supersession downstream) is real and filed as follow-up, named in ADR-079. semboids
embeds no query client, so the gh#562 A/B measures the three retired guards cleanly.

## Risks / Trade-offs

- [Out-of-band poison between boots on untouched keys] → detected at first-touch or next boot;
  containment unchanged (cannot be served or merged). Accepted; contract violation with its
  own defenses. Detection-latency change stated plainly in runbook and spec.
- [Cross-process repair after the drain marker] → inventory entry clears on next successful
  validating read (D3c) instead of staying stuck. Residual: a never-again-touched key keeps a
  stale-unhealthy entry until reboot — accepted, observability-only.
- [Rule kill switch now fires at consume-time, not write-time] → for consumed values this is
  at most one evaluation later than today; for never-consumed values it never fires — which is
  the point. The sibling contract's no-derived-output guarantee is preserved.
- [Nak-on-resident-poison can redeliver hot] → MaxDeliver bounds it; backoff is the consumer's
  existing policy; poison windows are operator-active periods by construction (Health degraded).
- [Operators who relied on whole-surface outage as the alarm] → gauge + Health degraded +
  DebugStatus enumeration + runbook alerting guidance; no in-repo auto-remediation keys on
  Healthy=false (verified), so no crash-loop.
- [Existing tests assert the global latch] → rewritten; the named inversion:
  `TestQueryDiscoveredPoisonBlocksConcurrentReadyResponse` must assert the concurrent valid
  response now SERVES. The mock ingest-guard watcher's Stop() must close its updates channel
  (real nats.go closes; drain-to-close would hang against the current mock). Also rewrite the
  `entityStatePoison.Load()` assertions in `keyed_ingest_test.go` and
  `merge_entity_write_gate_test.go`.
- [Perf recovery falls short] → with all three guards retired, the read-shaped per-mutation
  delta vs beta.146 is zero in semboids' deployment; a shortfall in their A/B now cleanly
  indicts something outside the watcher class.

## Migration Plan

In-process behavior only — no data migration, no bucket/schema/config changes. **Ordering
dependency**: `predicate-contract-enforcement`'s `graph-state-contract` delta syncs/archives
before this change archives (our requirements reference its sticky projection contract).
Deploy is a normal release; rollback is a revert. Sequence: implement → full gates
(`task lint`, race suite, integration sweep, e2e:structural) → gh#562 reply offering the
candidate build → semboids A/B → tag.

## Open Questions

- Health sample bound N=10 and boot-sweep ERROR cap 100 — proposed constants, adjustable at
  review of the implementation PR.
- Nak backoff for resident-poison redeliveries: consumer default vs explicit delay — decide at
  implementation with the existing consumer config.

# Design — readiness distribution and staleness contract

## Context

The ADR-066 readiness envelope is computed by `computeIndexStatus`
(`processor/graph-index/watermark.go`) and today reaches consumers only two ways:
a `graph.index.query.status` request/reply (per-decision, 5s timeout, fail-closed
on transport) and, since #596, producer-side Prometheus gauges. gh#590's post-close
evidence exposed three structural problems:

1. **Distribution**: the status *request* travels the same NATS connection as the
   ENTITY_STATES firehose in single-binary deployments (semboids: one connection,
   `cmd/semboids/main.go:329`). An in-process requester pays the saturated read
   stream twice per round-trip, times out, and fails closed — indistinguishable in
   logs from a genuine not-ready. The gate dies under exactly the load that makes
   readiness interesting.
2. **Unit**: ADR-082's `index_lag_tolerance` counts revisions. semboids' coalescer
   table shows the correct revision tolerance shifts 2–4× with `coalesce_ms` alone
   (and linearly with write rate) — any fixed count is wrong at some load. The
   operator-meaningful bound is time.
3. **Policy sprawl**: four consumers implement four gate semantics — sticky-forever
   (`ensureQueryReady`), per-tick exact-unless-configured (clustering), per-call
   exact with no knob (`graph/query/client.go`), degrade-honest (fusion).

Constraints: the exact `Ready` contract is a load-bearing authoritative-absence
license (ADR-066) and must not move; graph-ingest remains the sole ENTITY_STATES
writer (readiness status is NOT a graph write and must not go through it); the
live graph never uses NATS TTL (ADR-068) — the status bucket is not the live
graph, but we avoid TTL anyway (freshness is judged consumer-side). Stakeholders:
semboids (starving clustering), semsource (read-path ergonomics), any future
envelope consumer (graphview G5 parked).

## Goals / Non-Goals

**Goals:**

- Readiness reaches consumers as watchable last-known state whose *freshness* is
  known, so transport loss is distinguishable from not-ready.
- A view-rate consumer bounds staleness in wall time, invariant to write rate and
  coalesce settings.
- One place owns gate semantics; consumers declare a mode.
- A defer is diagnosable from one log line / one counter.

**Non-Goals:** see proposal Non-goals (exact `Ready` untouched; no throughput
work; read-query subjects and the #592 retry contract unchanged — only the
STATUS subject is removed; G5 parked).

## Decisions

### D1 — Readiness is published to the dedicated `GRAPH_STATUS` KV bucket (the KV twofer applied to our own signal); the status subject is removed

Producers (graph-index, graph-embedding) marshal the same
`graph.IndexStatusResponse` they compute today and `Put` it to the `GRAPH_STATUS`
KV bucket (**decided**: that name, History **3** — small bounded bucket; enough
replay to see the last few transitions after an incident without hoarding), one
key per producer (`graph-index`, `graph-embedding`), on the existing 5s status
tick (`statusMetricsInterval` loop already computes the envelope for #596 gauges
— compute once, set gauges, publish KV). Publish every tick unconditionally: the
write doubles as a liveness heartbeat, and one small write per 5s is negligible.

**Clean break (decided by owner)**: the `graph.index.query.status` request/reply
subject and `handleQueryStatusNATS` are REMOVED — no dual distribution paths, no
consumer fallback poll. sem\* is the only consumer family and all of it is
house-managed, so migration is documentation plus lockstep upgrades, not
compatibility shims. The twofer makes the subject redundant: `Get` on the key is
the point-in-time probe (`nats kv get GRAPH_STATUS graph-index`), `Watch` is the
event feed, History 3 is the trajectory. Only the STATUS subject goes; the
read-query subjects (`graph.index.query.incoming`, `byName`, `outgoing`, …) and
their classified-transient contract are untouched.

- *Why a new bucket*: this is operational component status, not domain entity
  state — routing it through ENTITY_STATES would violate the graph-ingest
  sole-writer and ADR-055 envelope invariants and pollute the graph. Defended
  against the bucket-ownership rubric: different writer, different lifecycle,
  different consumers, no semantic envelope.
- *Why not only request/reply hardening* (longer timeouts, dedicated
  connection): treats the symptom; polling still couples every consumer decision
  to a live round-trip, and a dedicated-connection requirement pushes complexity
  into every consumer. Watching inverts it: producers pay one write, N consumers
  hold state.
- *Wiring*: producers create the bucket at Start, before any consumer binding is
  required (eager-create-before-register). These producers are KV-native
  (they exist to consume/serve JetStream KV), so JetStream presence is a given
  wherever they run; no separate resourceless gating is needed beyond the
  components simply not being deployed.
- *Consumer access*: one shared status-watcher helper (see D2) is the single
  consumer-side code path — including `pkg/fusion/fusionnats`, whose `Status`
  call moves from the subject to the KV key.

### D2 — Status freshness is judged by consumer-local arrival time (no clocks compared)

A consumer watching the status key records *its own* wall-clock time at each
received update. Status is **fresh** while `now − lastArrival ≤ 3 ×` the
producer's heartbeat interval; otherwise **unknown**. Fresh not-ready defers on
its merits; unknown fails closed exactly as today, with the existing
`allow_ungated_reads` escape for standalone deploys (a deployment that runs
clustering without graph-index has no key and is permanently "unknown" — the
escape covers it, same as it covers the unreachable-subject case today). KV
watch delivers the current value immediately on start, so a restarted consumer
is fresh within one delivery, not one heartbeat. This lives in one shared
status-watcher helper used by every consumer — clustering, the `graph/query`
client, and fusion — so there is exactly one consumer-side code path.

- *Why not a `published_at` field compared to consumer clock*: cross-process
  clock skew for zero benefit; arrival-time freshness needs no clock agreement.
- *Consequence*: the semboids failure mode inverts. Under connection saturation
  the watcher may lag, making status *unknown* → an explicit, logged,
  counted defer reason — not a silent timeout wearing not-ready's log line.

### D3 — Staleness is `now − commit-time of the newest fully-covered revision`, carried in the envelope

`pkg/revlag.Watermark` learns the KV entry commit timestamp (`entry.Created()`)
alongside each observed revision and exposes the commit time of the current
`Indexed()` floor. The envelope gains one additive field, `staleness_ms`: `0`
when `Ready`, else `now − commitTime(indexed floor)` — "the view reflects the
world as of T". Commit timestamps, not arrival times, so server-side delivery
backlog is included once entries deliver.

- *Why this metric and not `last_synced` recency*: `last_synced` measures indexer
  *movement* and is always-recent under a firehose — ADR-082's rejection of it
  stands. Age-of-view measures what the operator cares about ("served, ~1–3s
  stale").
- *Caveat (documented in spec)*: while a revision sits undelivered server-side,
  it cannot age the metric; total stalls are still caught by the §4
  stuck-detector (`degraded`). The metric is a floor, not an oracle.
- *Why not revisions*: load- and coalesce-dependent (Context §2).

### D4 — One gate, four declared modes; `max_staleness` replaces `index_lag_tolerance`

A canonical helper in `graph` (beside `ReadyWithinLag` today) evaluates
`(status, statusFreshness, mode, config)` → `(proceed, reason)`:

| Mode | Semantics | Adopters |
|------|-----------|----------|
| `exact` | fresh `Ready == true`, else defer/error | `graph/query` client (gains the mode it hard-codes today), fusion's top gate |
| `bounded-staleness` | no hard stop AND (`Ready` OR `staleness_ms ≤ max_staleness`), status fresh | graph-clustering |
| `sticky-bootstrap` | exact until first pass, then open; local hard stops (failedCount, reset) still override | graph-index `ensureQueryReady` (internal state folded in) |
| `degrade-honest` | exact, but the caller degrades to an honest-empty result instead of erroring | fusion `Fuse` |

Hard stops (`degraded`, `reset_required`, empty/pre-enumeration) defer under
every mode and every tolerance — the ADR-082 invariant carries over unchanged.
Clustering's `index_lag_tolerance` (revisions) is **replaced** by
`max_staleness` (duration string). Pre-1.0 break-now: the field shipped in .156
and no known deployment sets it. `ReadyWithinLag` is removed with its only
consumer; the per-key `IndexedRevision >= myRev` contract for point readers is
unchanged and remains the exact-read escape hatch.

- *Why replace rather than deprecate-alongside*: two tolerance knobs in different
  units on one component is an operator trap; N=2 greenfield rule applies.
- *Why the helper lives in `graph`*: the envelope type and its interpretations
  change together (same reason `ReadyWithinLag` lives there today).

### D5 — Defers become evidence

The clustering defer path logs structured fields — `status_known`,
`status_age`, `state`, `lag`, `staleness_ms`, `reason`, and the watch/bucket
error when present — and increments a
`defer_total{reason}` counter (`hard_stop | over_staleness | status_unknown |
empty`). The gh#590 observer-discrepancy investigation required three comment
cycles because the current line is a bare constant; this makes it one grep.

### D6 — ADR-083 records the two genuine decisions

(1) Readiness is distributed as state (KV, watchable, heartbeat-fresh) and the
status request/reply is REMOVED — the clean break the owner decided, superseding
this section's earlier "request/reply retained as a query convenience" wording;
(2) the view-rate staleness unit is time (age-of-view), superseding ADR-082's
revision count for the clustering gate. Mechanics live in the
`graph-index-readiness` spec; the ADR is one page.

## Risks / Trade-offs

- [Staleness under-reports during pure server-side delivery backlog] → documented
  as a floor; the wall-clock stuck-detector still flips `degraded` on total
  stall; the #596 `lag` gauge remains the volume signal.
- [Status KV write fails silently → consumers hold aging state] → freshness
  window flips them to `status_unknown` (fail-closed) within 3 heartbeats;
  producer logs and a publish-failure counter make it operator-visible.
- [Two distribution channels could drift] → both are one `computeIndexStatus`
  call; the KV publisher and gauge setter consume the same struct in the same
  tick.
- [Removing `index_lag_tolerance` breaks an unknown adopter] → searched: shipped
  one release ago (.156), semboids/semsource confirmed unset; release notes call
  it out; config decode of the removed field fails loudly, not silently.
- [Removing the status subject breaks an unswept requester] → mitigation is the
  sister-repo sweep (task 5.4): grep semboids/semsource/semteams/semconnect for
  `graph.index.query.status` before merge (sweep-all-emitters discipline), plus
  a migration doc. A missed external requester gets a no-responders transient —
  loud, not silent.
- [Mixed-version window: old consumer + new producer (or vice versa) during
  rollout] → accepted: sem\* upgrades in lockstep, and the failure mode is an
  explicit no-responders / missing-key "unknown", both fail-closed and logged.
- [Multiple instances of one producer would fight over one key] → out of scope;
  single-instance-per-component is the current deployment model, noted in the
  spec.

## Migration Plan

1. One PR, complete-system: envelope field + revlag commit-time tracking +
   producers publish `GRAPH_STATUS` + shared status-watcher helper + gate helper
   adopted by all four consumers + status subject/handler removal (including
   `fusionnats.Status` → KV) + config swap + structured defer observability +
   spec deltas + ADR-083 + migration doc.
2. Pre-merge sweep: grep all sister repos for `graph.index.query.status`
   requesters and `index_lag_tolerance` configs (sweep-all-emitters discipline);
   file lockstep PRs where hits exist.
3. Gates: full `-race` suite, `task lint`, `task schema:generate` no-drift, and —
   breaking wire + config surface — `task e2e:statistical` AND
   `task e2e:semantic` green before the tag (clustering tier + fusion path).
4. Rollout: next tag; sem\* upgrades in lockstep (house-managed). semboids sets
   `max_staleness` (starting point decided with the held soak, which now
   validates a time bound) and retargets its probe to
   `nats kv get GRAPH_STATUS graph-index`.
5. Rollback: pre-tag, revert the PR. Post-tag there is no compat path by design
   (clean break); both one-way doors — the removed subject and the removed
   config field — fail loudly (no-responders transient / decode error), never
   silently.

## Decided (owner, 2026-07-20)

- Bucket name: **`GRAPH_STATUS`**; History **3** (small bounded bucket).
- **Clean break**: no deprecated code paths — status subject removed, no
  consumer fallback poll; migration ships as solid docs + lockstep sem\* PRs
  (sem\* is the only user and all of it is house-managed).

## Open Questions

- Reference-config `max_staleness` value for `configs/statistical.json` — owner
  decision, informed by the held semboids soak (suggest starting at 2–5s).
- Does `graph/query`'s client expose mode selection to its callers now, or stay
  hard-wired `exact`? (Recommend: stay `exact`; the enum exists internally.)
- Freshness multiplier (3× heartbeat) — constant or config? (Recommend:
  constant until someone needs otherwise.)

# ADR-083: Readiness Is Distributed State, and View Staleness Is Measured in Time

## Status

**Gate-mode table superseded by ADR-084 — 2026-07-20.** D4's four declared consumer
modes collapse into health + a freshness parameter; `GateMode`/`GateConfig` are
removed. The distribution decision (KV state, heartbeat, freshness judged
consumer-locally) and the staleness unit are unchanged and still current.

Accepted — 2026-07-20. Decision-recording for the
`readiness-distribution-and-staleness-contract` change. Addresses the post-close
evidence on #590 (semboids coalescer table + observer discrepancy). Builds on
ADR-066 (honest graph-index readiness) and **supersedes ADR-082's staleness
*unit*** for the clustering gate — ADR-082's consumer-class split stands
unchanged; only the quantity it bounds moves from revisions to time.

## Context

ADR-066's readiness envelope reached consumers by a `graph.index.query.status`
request/reply: one round-trip per gate decision, 5s timeout, fail-closed on
transport failure. Three structural problems surfaced after #590 closed.

**The transport dies under the load that makes readiness interesting.** In a
single-binary deployment the status request travels the same NATS connection as
the ENTITY_STATES firehose, so an in-process requester pays the saturated read
stream twice per round-trip — once for the request, once for the reply. It times
out and fails closed, emitting the *same* log line as a genuine not-ready. That
ambiguity cost three investigation cycles on #590 and explained an
8/25-vs-0/35 observer discrepancy that had no statistical explanation.

**Revisions are the wrong unit for a view-rate consumer.** ADR-082 bounded
clustering's tolerance in ENTITY_STATES revisions. semboids' coalescer table
shows the correct revision tolerance shifts 2–4× with `coalesce_ms` alone, and
linearly with write rate — any fixed count is wrong at some load. What an
operator can actually reason about is "the view reflects the world as of ~3s
ago".

**Four consumers had grown four gate semantics** over one envelope —
sticky-forever, per-tick-exact-unless-configured, per-call-exact-with-no-knob,
and degrade-honest — with no single place where the hard stops were stated, and
therefore no way to change them once.

## Decision

**1. Readiness is published as state, not answered as a query.** Producers
(graph-index, graph-embedding) write the ADR-066 envelope to a dedicated
`GRAPH_STATUS` KV bucket, one key per producer, on every status tick. The tick
publishes unconditionally, so the write doubles as a liveness heartbeat. This is
the KV twofer applied to our own operational signal: `Get` is the point-in-time
probe, `Watch` is the event feed, and bucket history is the trajectory.

**The `graph.index.query.status` subject and its handler are REMOVED — a clean
break, no dual distribution path and no consumer fallback poll.** Every `sem*`
consumer is house-managed, so migration is documentation plus lockstep upgrades
rather than compatibility shims. Only the *status* subject goes; the read-query
subjects and their classified-transient retry contract (#592) are untouched.

A dedicated bucket rather than ENTITY_STATES: this is operational component
status, not domain entity state. Routing it through the graph would violate the
graph-ingest sole-writer and ADR-055 envelope invariants for no benefit.

**2. Freshness is judged by consumer-local arrival time.** A consumer stamps its
*own* clock when an update arrives and treats the held envelope as authoritative
only while `now − lastArrival ≤ 3 × heartbeat`; past that it is UNKNOWN and the
consumer fails closed. No producer timestamp is compared against a consumer
clock, so the contract needs no clock agreement between processes.

The one exception is a value that was *already* older than the window when it
was delivered: a watch hands over the key's current value the instant it binds,
so a consumer starting against a dead producer would otherwise receive that
producer's final — possibly `Ready=true` — envelope and stamp it as brand new.
Rejecting that one case closes a fail-open that request/reply never had.

**3. The view-rate staleness unit is time.** The envelope carries an additive
`staleness_ms` — `0` when `Ready`, else `now − commit-time of the newest
fully-covered revision`. Commit timestamps, not arrival times, so server-side
delivery backlog is counted once entries deliver. Clustering's
`index_lag_tolerance` (revisions) is **replaced** by `max_staleness` (duration).

**4. One helper owns gate semantics; consumers declare a mode.** A canonical
evaluator in `graph` takes `(status, fresh, mode, config)` and returns
`(proceed, reason)`. The hard stops — `degraded`, `reset_required`, empty — defer
under every mode and every tolerance, stated once. Consumers name a mode
(`exact`, `bounded-staleness`, `sticky-bootstrap`, `degrade-honest`) instead of
re-deriving a predicate.

## Alternatives rejected

1. **Harden the request/reply** (longer timeout, dedicated connection). Treats
   the symptom. Polling still couples every gate decision to a live round-trip,
   and a dedicated-connection requirement pushes complexity into every consumer.
   Watching inverts the cost: the producer pays one write per heartbeat, and N
   consumers hold state.
2. **Keep the subject alongside the bucket** for a deprecation window. Rejected
   by the owner: two distribution paths for one fact is exactly the drift the
   twofer removes, and every consumer is house-managed. Both removals — the
   subject and the config field — fail *loudly* (no-responders transient, config
   decode error), never silently.
3. **A `published_at` field compared against the consumer's clock.** Buys
   cross-process clock skew for no benefit over arrival-time freshness.
4. **`last_synced` recency as the staleness metric.** ADR-082's rejection
   stands: it measures indexer *movement*, which is always-recent under a
   firehose — that is the stuck-detector's job, not a staleness signal.
5. **Keeping revisions as the tolerance unit.** Load- and coalesce-dependent;
   see Context.

## Consequences

The semboids failure mode inverts: under connection saturation a watcher lags
and reports `status_unknown` — an explicit, logged, counted defer reason — rather
than a silent timeout wearing not-ready's log line. Consumers get one shared
code path, so the freshness rule cannot drift per consumer.

**This decision moves distribution only. No gate predicate changes.** Every
consumer's proceed/defer behavior is bit-for-bit what it was on the subject,
including fail-closed on unknown and the `allow_ungated_reads` escape that
covers a deployment running a consumer without its producer.

That restraint is deliberate, because the word "readiness" currently answers four
different questions: *is the index healthy / not mid-rebuild*, *is the view fresh
enough*, *is my own write visible yet*, and *may I treat an empty result as
authoritative absence*. Only the first two are settled here. The third is
answerable per-caller today (`IndexedRevision >= myRev`, revision supplied by the
caller). **The fourth is not answerable by readiness at all** — coverage of a
source says nothing about whether the source ever published the thing being
looked for — and ADR-066's "authoritative not-found" license should not be leaned
on further. The four gate modes above are best understood as a *symptom* of that
conflation rather than as good design; collapsing them requires re-gating reads
on health instead of coverage, which reopens the #592 read-path contract and
therefore belongs to its own decision, not to a transport move.

Costs accepted: a new KV bucket and one small write per producer per heartbeat; a
one-shot bounded wait on a lazily-bound consumer's first gated call (so a cold
client is not unknown merely because its watch has not delivered yet); a
mixed-version rollout window where an un-upgraded consumer sees no responders;
and the loss of a synchronous probe, replaced by `nats kv get GRAPH_STATUS
graph-index`.

One further cost surfaced during review and is resolved by this ADR's third
break. The envelope's revision resolution is now the heartbeat: two reads of
`IndexedRevision` inside one heartbeat return the *same* published value, where
the removed handler computed it live per request and could differ between two
calls milliseconds apart. Anything that inferred a fact from *comparing two
samples* therefore loses its signal. The known instance was fusion's
`ViewRevision.Coherent` (`pkg/fusion/engine_graph.go`), which sampled readiness
before and after a facet's fetch phase and reported coherence when the two
agreed — under held state they agree by construction, so it would have asserted
"every read behind this projection happened at one indexed revision" for
projections spanning an unknown number of revisions. A downstream consumer used
that claim to license deleting entities absent from the projection — the
authoritative-absence claim these Consequences say readiness must not license.

**Resolution: the `Coherent` field is DELETED, not re-tuned.** The claim was
never soundly provable, before or after this change: fusion assembles from N
reads across stores at different instants with no snapshot and no consistent
cut, so two samples agreeing can sometimes *catch* an advance but can never
*prove* the absence of one. Restoring a "provable" signal by carrying sample
identity was considered and rejected — it buys a better heuristic wearing the
same absolute-sounding word, adding mechanism in defense of a claim that should
not exist. `ViewRevision.Start`/`End` remain as plain observations of the
assembly window. A consumer that needs a genuinely coherent single-revision
view uses `pkg/graphview` (ADR-081), which has real snapshot semantics;
retrieval fusion is best-effort ranked evidence and says so. The general
lesson stands for future work: moving a signal from computed-per-request to
published-on-a-tick silently breaks anything that inferred a fact by comparing
two samples — sweep for such inferences when changing a signal's publication
cadence.

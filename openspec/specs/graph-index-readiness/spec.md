# graph-index-readiness Specification

## Purpose

Defines how graph-index reports readiness and how consumers gate on it.

**A readiness gate asks one question: is this index sound to read from?** Four
conditions answer it, none consumer-specific and none optional — the status
reading is fresh, its `state` is one the consumer recognizes, there is no hard
stop (`degraded`, `reset_required`), and the producer's initial build is
complete (`bootstrap_complete`). Anything else proceeds. The index is a
materialized view: it is either still building, broken, or
working-and-N-behind, and only the first two justify withholding an answer
(ADR-085).

**View age is reported, never gating.** `staleness_ms` rides the envelope so a
consumer can stamp on its own output how current the input was; nothing
consults it to decide whether to answer. There is no freshness, staleness, or
tolerance parameter anywhere on the gate surface.

**`Ready` licenses nothing about absence** (ADR-084). It reports coverage —
every committed ENTITY_STATES revision up to a query-time target has been
applied — and coverage cannot answer whether a source ever published the thing
being sought, so no consumer may treat an empty result as an authoritative
not-found under any envelope state. `Ready` is also inert at the gate: it
neither licenses a proceed nor withholds one. Its remaining job is to be the
value a caller compares its own revision against via `IndexedRevision`, which
is the one sound per-entity check (read-your-writes).

A known-incomplete index (a failed required write) reports `degraded`
regardless of lag. Consumers needing a snapshot-consistent view use graph-view
subscriptions (ADR-081), which have real snapshot semantics.
## Requirements
### Requirement: Ready reports exact revision coverage
The readiness envelope's `Ready` bool SHALL be true only when the index has
applied every committed ENTITY_STATES revision at compute time (`target > 0 &&
indexed >= target`, or the authoritatively-empty override) AND no required
index write is unresolved. `Ready` is observability only, and is INERT at the
gate: it neither licenses a proceed nor withholds one, because coverage
answers no question the health gate asks. No read path SHALL defer on
`!Ready` alone — with one carve-out:
graph-index's private pre-bootstrap exactness gate, which is the
`bootstrap_complete` condition evaluated in-process. No consumer SHALL treat
an empty result as an authoritative not-found under any envelope state —
coverage says nothing about whether the source ever published the thing being
looked for (ADR-084 retires ADR-066's authoritative-absence license).
`IndexedRevision >= myRev` remains the caller-supplied read-your-writes
check, the only legitimately per-entity one; the mutation response's
`kv_revision` and the envelope's revision space SHALL be pinned as comparable
by a test.

#### Scenario: Ready stays exact and observable under continuous write
- **GIVEN** a bucket under continuous write so `Lag > 0` at compute time
- **WHEN** any consumer decodes the envelope from `GRAPH_STATUS`
- **THEN** `Ready` is false while `staleness_ms` reports the view's age when
  computable (>= 1ms)
- **AND** healthy read paths still serve, with that staleness observable

#### Scenario: Empty results license nothing
- **GIVEN** a query returning no rows under any envelope state
- **WHEN** a consumer interprets the result
- **THEN** no correctness argument may treat the emptiness as proof of absence

### Requirement: A known-incomplete index defers regardless of lag
A known-incomplete index (`FailedCount > 0`) SHALL report `State = degraded`
regardless of revision lag, and the shared readiness projection
(`ComputeIndexStatus`) SHALL enforce this through a `FailedCount` input evaluated
BEFORE the "ready wins" branch — so the `FailedCount > 0 → degraded` rule holds
even for a producer whose watermark has already reached the target (where `Ready`
would otherwise be `true`). When a required index write or delete has failed and
is not yet repaired the reverse index can report a smaller graph than exists, so
the `FailedCount → degraded` projection MUST be unconditional (not gated on
`Ready`). With coverage inert at the gate (ADR-085), `degraded` is the only signal
that withholds a read from a known-incomplete index: a projection gated on `Ready`
would leave that index serving silently truncated answers. `Ready` remains
coverage-accurate (a full-coverage index that also holds failures is still
covered); the health verdict lives in `State`, on which consumers gate.

#### Scenario: Failed required write with small lag is degraded, not building
- **GIVEN** a required index write failed (`FailedCount = 1`) and continuous write
  keeps `Lag` small
- **WHEN** the status is computed
- **THEN** `State` is `degraded` (not `building`)
- **AND** every consumer defers on the hard stop, at any view age

#### Scenario: Incompleteness defers on state, not on coverage
- **GIVEN** `FailedCount > 0` and `Lag > 0`
- **WHEN** a consumer evaluates the canonical gate
- **THEN** it defers with reason `hard_stop` on the `degraded` state — not
  because `Ready` is false, which defers no one

#### Scenario: A producer caught up with failures is degraded, not ready
- **GIVEN** a producer (e.g. graph-embedding) whose watermark advances on every
  terminal outcome, so `Indexed >= Target` while `FailedCount > 0`
- **WHEN** the status is computed
- **THEN** `State` is `degraded` — the `FailedCount` input wins over the "ready
  wins" branch — never `ready` over unusable coverage
- **AND** `Ready` may still be `true` (coverage is complete); consumers gate on
  `State`

### Requirement: Community detection runs whenever the index is healthy
Community detection SHALL gate each pass through the canonical gate on health
alone, SHALL defer only on hard stops, incomplete bootstrap, unrecognized
state, and unknown status, and SHALL surface every defer through the
structured defer log and `defer_total{reason}` counter. It SHALL expose no
staleness, lag, or tolerance configuration. Every verified run SHALL record
the view age it ran at (`staleness_at_detection_ms`), so an operator can
correlate community churn with view age — the age is stamped on the output,
never used to withhold one (ADR-085). A configuration carrying a removed
tolerance key (`index_lag_tolerance`, `max_staleness`) SHALL fail startup
loudly naming what happened, and SHALL NOT be silently ignored.

#### Scenario: Continuous write no longer defers detection
- **GIVEN** a deployment under continuous write where `Lag > 0` essentially
  always and the index is otherwise healthy
- **WHEN** the detection tick fires
- **THEN** detection runs, and `staleness_at_detection_ms` records the view
  age — the gh#590 symptom is unreachable because no tolerance exists to
  misconfigure

#### Scenario: A carried-forward tolerance key fails startup
- **GIVEN** a graph-clustering config still carrying `index_lag_tolerance` or
  `max_staleness` from a prior release
- **WHEN** the component starts
- **THEN** startup fails with an error naming the removed key and stating
  that readiness now gates on health alone

### Requirement: Clustering under lag is observable
When community detection runs with a stale view, the staleness it ran at SHALL
be operator-visible (metric + info-level or stage/output surface), and every
**defer** SHALL be attributable: the defer log line carries structured fields
(`status_known`, `status_age`, `state`, `lag`, `staleness_ms`, `reason`, and
the watch/bucket error when present) and a `defer_total{reason}` counter
distinguishes the four surviving reasons — `hard_stop`, `status_unknown`,
`unrecognized_state`, and `bootstrap_incomplete`. Clustering on a stale view
can never become silent staleness, and a transport failure can never be
mistaken for index state from the logs (the gh#590 investigation cost three
comment cycles because the defer line was a bare constant).

#### Scenario: A stale partition is visible
- **GIVEN** detection runs on a healthy index at `staleness_ms = 1500`
- **WHEN** an operator inspects metrics / logs / status after the run
- **THEN** they can determine the last partition ran at ~1.5s staleness, not
  only that it "ran", and the signal is not confined to a debug log

#### Scenario: Defer reasons are countable and grep-able
- **GIVEN** a deployment where detection is deferring
- **WHEN** an operator reads `defer_total` by reason and any single defer log
  line
- **THEN** they can distinguish a broken index (`hard_stop`), a dead status
  feed (`status_unknown`), an uninterpretable envelope
  (`unrecognized_state`), and a producer still building
  (`bootstrap_incomplete`) without correlating multiple log lines
- **AND** no `over_staleness` or `staleness_unknown` reason exists to count

### Requirement: Read consumers retry the readiness transient
Reverse-index and by-name read handlers SHALL return the classified transient
`ErrorCodeIndexNotReady` only for health failures — hard stops, status-
unknown, and bootstrap-incomplete (the gh#474 cutover window) — and SHALL NOT
return it for ordinary catch-up lag on a healthy, built index. Consumers
SHALL detect it via `errs.IsTransient` (never by message text) and retry;
bounded retry converges once the health condition clears, with two stated
carve-outs: `reset_required` is fatal at the responder
(`ErrorCodeGraphStateResetRequired`) and never self-clears, and emitters
outside the reverse-index read contract (lifecycle, rule, spatial, temporal,
embedding, ingest — responder-up / watcher-health semantics) keep their
existing meanings. This deliberately supersedes the #592 close-out for read
paths: the transient no longer fires on plain lag, so retrying it stops being
the response to plain lag.

#### Scenario: A read during ordinary catch-up serves
- **GIVEN** a healthy, built index catching up after a write burst
- **WHEN** a reverse-index or by-name read arrives
- **THEN** it is served (no transient), with staleness observable on the
  GRAPH_STATUS envelope

#### Scenario: A read during the cutover window is retryable
- **GIVEN** the index is bootstrap-incomplete or degraded by unresolved write
  failures
- **WHEN** a reverse-index or by-name read arrives
- **THEN** it returns the classified `ErrorCodeIndexNotReady` transient
- **AND** a consumer that retries converges once health is restored

#### Scenario: The transient is programmatically detectable
- **GIVEN** a read consumer
- **WHEN** it inspects the error
- **THEN** `errs.IsTransient` classifies it without matching any message string

### Requirement: Fusion degrades consistently on the readiness transient
The fusion engine SHALL gate `Fuse` through the canonical health gate
(adopting it — the current top gate is a hand-rolled `!Ready` check): it
SHALL proceed under ordinary catch-up lag,
reporting `staleness_ms` on the envelope, and SHALL return the empty-honest
envelope (fail closed, carrying the last-known `IndexStatus`) only on health
defers — status-unknown, hard stops, or incomplete bootstrap. A status
*wiring* failure (the transport cannot watch GRAPH_STATUS) SHALL remain a
loud error, never degraded to an eternal empty envelope; only a quiet or
stale feed defers. Fuse SHALL NOT offer an ungated-reads escape (it is a
shared product surface, not a standalone deployment — the asymmetry with
`allow_ungated_reads` is deliberate). When `Resolve`, `Entities`, or the
relations neighbor expansion returns the classified
`ErrorCodeIndexNotReady` (now health-scoped), `Fuse` SHALL degrade to the
same empty-honest envelope rather than propagating a hard error, and a
degraded envelope SHALL NOT carry `State="ready"`. Genuine, non-transient
errors SHALL still propagate. The facet walks (impact / paths / graph
projection) remain out of scope: they carry their own per-facet honesty
markers (`Truncated`; the graph facet carries no coherence claim — see the
fusion capability spec).

#### Scenario: Fuse serves ranked evidence under lag
- **GIVEN** a healthy, built index with `Lag > 0` and a query with matching
  entities
- **WHEN** `Fuse` runs
- **THEN** it returns ranked results with `staleness_ms` reported, not an
  empty envelope

#### Scenario: A quiet status feed defers; a wiring failure errors
- **GIVEN** a fusion deployment whose status feed goes quiet past the
  freshness window
- **WHEN** `Fuse` runs
- **THEN** it returns the empty-honest envelope (fail closed)
- **AND** a transport that cannot watch GRAPH_STATUS at all yields a loud
  error instead, never an eternal empty envelope

#### Scenario: A health-scoped transient degrades, not errors
- **GIVEN** a core read inside `Fuse` returns the classified readiness
  transient
- **WHEN** `Fuse` handles it
- **THEN** it returns the empty-honest envelope, the same degrade as its top
  gate

#### Scenario: A genuine error still propagates
- **GIVEN** an internal read returns a non-transient error (e.g. a real decode or
  connection failure)
- **WHEN** `Fuse` handles it
- **THEN** it propagates the error (not degraded to an empty envelope)

### Requirement: The readiness envelope is exposed as Prometheus metrics
Every ADR-066 envelope producer (graph-index, graph-embedding) SHALL expose
the envelope as scrapeable Prometheus gauges in addition to the
`GRAPH_STATUS` KV key. At minimum the gauges are `readiness` (1 when Ready else
0), `lag` (revisions behind target), `indexed_revision`, and `target_revision`,
plus a `state`-labeled gauge distinguishing building / ready / degraded /
reset_required. The gauges MUST reflect the same values `computeIndexStatus`
returns and stay fresh independent of query traffic (refreshed on the same
periodic tick that publishes the KV key — one compute feeds both).

#### Scenario: Readiness and lag are scrapeable without a KV read
- **GIVEN** graph-index is running and catching up under continuous write
- **WHEN** Prometheus scrapes the component
- **THEN** the `readiness`, `lag`, `indexed_revision`, and `target_revision`
  gauges are present and reflect the current `computeIndexStatus` values
- **AND** no KV read is required to observe them

#### Scenario: State distinguishes catching-up from broken
- **GIVEN** the index is `building` with lag, versus `degraded` or `reset_required`
- **WHEN** an operator inspects the `state`-labeled gauge
- **THEN** the current state is identifiable (so "catching up" can be alerted
  differently from "broken"), not collapsed into `readiness=0`

#### Scenario: Metrics and the KV key stay in agreement
- **GIVEN** the periodic status tick
- **WHEN** the envelope is computed
- **THEN** the same struct is written to the gauges and to `GRAPH_STATUS`,
  never two divergent computations

### Requirement: Readiness is published as watchable KV state in a dedicated bucket
Every ADR-066 envelope producer (graph-index, graph-embedding) SHALL publish
its `IndexStatusResponse` JSON to its key in the dedicated `GRAPH_STATUS` KV
bucket (History 3; one key per producer; owned by the envelope producers,
separate from ENTITY_STATES and every graph-data bucket) on a fixed heartbeat
tick (the existing 5s status tick), unconditionally each tick so the write
doubles as a liveness heartbeat. The bucket SHALL be created by the producer at
Start, before any consumer binding is required. The former
`graph.index.query.status` request/reply subject and its handler are REMOVED
(**BREAKING**, clean break — no fallback poll path); the KV twofer replaces it:
`Get` for point-in-time probes, `Watch` for the event feed, history for the
trajectory. Readiness status is operational component state, NOT a graph write:
it never routes through graph-ingest and carries no ADR-055 semantic envelope.

#### Scenario: Consumers hold last-known readiness without polling
- **GIVEN** graph-index is running and publishing the envelope on its heartbeat tick
- **WHEN** a consumer watches the producer's status key
- **THEN** it receives the current envelope immediately on watch start and every
  subsequent update, and can gate decisions on held state with no per-decision
  NATS request

#### Scenario: A point-in-time probe is a KV Get
- **GIVEN** an operator or debug tool wanting current readiness
- **WHEN** it reads the producer's key (e.g. `nats kv get GRAPH_STATUS graph-index`)
- **THEN** it receives the same `computeIndexStatus` projection the gauges and
  watchers see (one compute feeds gauges and the KV publish)

#### Scenario: The removed status subject fails loudly, never silently
- **GIVEN** an unmigrated requester of the former `graph.index.query.status` subject
- **WHEN** it issues the request
- **THEN** it receives a no-responders transport error (loud), never a stale or
  fabricated envelope

### Requirement: Consumers distinguish not-ready from status-unknown
A readiness consumer SHALL judge status freshness by consumer-local arrival time
(no cross-process clock comparison): the held status is fresh while the time
since the last received update is within a bounded multiple (3×) of the
producer's heartbeat interval, and **unknown** otherwise. A fresh not-ready
status SHALL defer on its merits; an unknown status SHALL fail closed, with the
existing `allow_ungated_reads` escape for standalone deployments. The two
outcomes SHALL be distinguishable in logs and metrics (the gh#590 observer
discrepancy was a transport failure wearing not-ready's log line).

#### Scenario: A stalled status feed fails closed as unknown, not as not-ready
- **GIVEN** the producer stops publishing (crash, connection loss) while a
  consumer holds a last-known `Ready = true`
- **WHEN** 3× the heartbeat interval elapses with no update
- **THEN** the consumer treats readiness as unknown and fails closed
- **AND** the defer is attributed to `status_unknown`, not to index state

#### Scenario: A restarted consumer is fresh within one delivery
- **GIVEN** a consumer restarts while the producer is healthy
- **WHEN** its watch binds
- **THEN** the current value is delivered immediately and status is fresh without
  waiting a heartbeat

### Requirement: The envelope carries view staleness in time
The readiness envelope SHALL carry an additive `staleness_ms` field: `0` when
`Ready`, otherwise the age of the view — now minus the commit timestamp of the
newest fully-covered ENTITY_STATES revision (from KV entry timestamps, not
delivery-arrival times, so delivered backlog ages the metric). The field is a
floor: a revision not yet delivered to the producer cannot age it, and a total
stall is still surfaced by the wall-clock stuck-detector flipping `State` to
`degraded`. Wire compatibility: the field is additive; existing fields are
unchanged.

#### Scenario: Staleness reflects how old the served view is
- **GIVEN** continuous write with the watermark N revisions behind, the oldest
  covered revision committed at time T
- **WHEN** the envelope is computed at time `now`
- **THEN** `staleness_ms ≈ now − T` and grows if catch-up stalls, independent of
  write rate and `coalesce_ms`

#### Scenario: Caught up means zero staleness
- **GIVEN** `Ready = true`
- **WHEN** the envelope is computed
- **THEN** `staleness_ms = 0`

### Requirement: The envelope reports bootstrap completion
The readiness envelope SHALL carry `bootstrap_complete`: true once the
producer's initial build — enumeration plus replay to the enumeration-time
target, including the authoritatively-empty outcome (enumeration delivered,
zero entities) — has completed in this process lifetime, and false again
after a restart until the rebuild completes, so a restart into a format
cutover re-gates. This makes the gh#474 cutover window wire-observable: a
rebuild in progress is no longer byte-identical to ordinary catch-up on a
built index. An absent field (older producer) SHALL read as false (fail
closed); this is an accepted lockstep-upgrade cost.

#### Scenario: A cutover rebuild is distinguishable from ordinary lag
- **GIVEN** graph-index restarts into a format cutover and is re-materializing
  indexes from ENTITY_STATES
- **WHEN** it publishes the envelope during the replay
- **THEN** `bootstrap_complete` is false while `State` is building
- **AND** after the replay completes, subsequent envelopes report
  `bootstrap_complete` true — including under later ordinary lag

#### Scenario: An authoritatively empty graph reports bootstrap complete
- **GIVEN** a fresh deployment whose initial enumeration delivers zero entities
- **WHEN** the envelope is published
- **THEN** `bootstrap_complete` is true and `Ready` is true (the empty-graph
  encoding), so gated reads serve

### Requirement: Consumers gate on health alone through the canonical gate
The canonical readiness gate SHALL evaluate exactly one question — is this
index sound to read from — over four conditions, none of them consumer-
specific and none of them optional: the status reading is fresh; its `State`
is recognized (an allow-list over the known states, never a deny-list for the
hard stops); there is no hard stop (`degraded`, `reset_required`); and
`bootstrap_complete` is true. Anything else SHALL proceed. The gate SHALL
accept no freshness, staleness, or tolerance parameter, and coverage (`Ready`)
SHALL NOT defer any consumer: view age is REPORTED on results, never used as
admission control (ADR-085). Status-unknown SHALL fail closed
(`allow_ungated_reads` remains the explicit deployment escape, applying to
exactly the unknown branch). The gate SHALL live in one canonical helper
beside the envelope type; per-consumer hand-rolled gate logic is prohibited.
The typed defer reasons remain closed and number four — `hard_stop`,
`status_unknown`, `unrecognized_state`, and `bootstrap_incomplete` — each
naming a distinct operator action, and none answerable by tuning a value.

#### Scenario: Ordinary catch-up lag never defers any consumer
- **GIVEN** a healthy, built index catching up under continuous write
  (`Lag > 0`, `bootstrap_complete` true, no hard stop)
- **WHEN** any consumer evaluates the gate
- **THEN** it proceeds, and the envelope reports the current `staleness_ms`
  for the consumer to record on its own output

#### Scenario: An arbitrarily stale but healthy view still serves
- **GIVEN** a healthy, built index whose view age exceeds any value an
  operator would previously have configured as a tolerance
- **WHEN** a view-rate consumer evaluates the gate
- **THEN** it proceeds — no age defers a healthy index, and the age is
  recorded on the run rather than used to withhold it

#### Scenario: Hard stops, unknown status, and incomplete bootstrap always defer
- **GIVEN** `State ∈ {degraded, reset_required}`, or a stale/absent status
  feed, or `bootstrap_complete` false
- **WHEN** the gate is evaluated
- **THEN** it defers (fail closed) with the typed reason

#### Scenario: An unrecognized state fails closed rather than reading as healthy
- **GIVEN** an envelope whose `State` is blank or outside the known set
  (version skew — the producer is talking, and saying something this consumer
  does not understand)
- **WHEN** the gate is evaluated
- **THEN** it defers with reason `unrecognized_state`, and SHALL NOT proceed
  on the grounds that the state is merely "not degraded"

### Requirement: The degraded envelope carries bounded failure detail
A producer that tracks per-entity failures SHALL report, on both the
`GRAPH_STATUS` envelope and its Prometheus gauges, enough bounded-cardinality
detail to distinguish a whole-dependency outage from a few persistently-failing
entities, WITHOUT placing any unbounded per-entity list on the watched key. The
envelope SHALL carry `failed_count`, a `failed_reasons` map from a fixed reason
enum to counts, and a `first_failure_at` timestamp (all additive and omitted when
zero, preserving wire compatibility). The producer SHALL expose a `failed` gauge
(current failed count) and a failures counter labeled by the same fixed reason
enum; the raw error message SHALL NOT be used as a metric label.

#### Scenario: Degraded distinguishes outage from poison entities
- **GIVEN** the dependency is down and every entity's write fails
- **WHEN** an operator reads the `GRAPH_STATUS` envelope
- **THEN** `failed_count` is high and `failed_reasons` is dominated by a single
  connectivity reason — distinguishable from a small stable `failed_count` under
  a content reason

#### Scenario: The watched key stays compact
- **WHEN** the failure-detail envelope is published on the heartbeat tick
- **THEN** it carries only bounded-cardinality aggregates (a count, a fixed-key
  reason map, a timestamp), never a per-entity list

#### Scenario: Failure reasons are a bounded metric label
- **WHEN** a failure is recorded
- **THEN** the failures counter increments under a value from the fixed reason
  enum, never under the raw error text


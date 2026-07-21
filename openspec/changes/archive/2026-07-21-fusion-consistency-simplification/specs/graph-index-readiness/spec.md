# graph-index-readiness — delta

## REMOVED Requirements

### Requirement: Consumers gate through the canonical readiness gate with a declared mode

**Reason**: the four modes were four policies over conflated questions
(ADR-083 Consequences). `exact` and `degrade-honest` are one evaluation whose
only difference is the caller's reaction (degrade-honest has zero callers —
it existed as call-site documentation), and `sticky-bootstrap` is
graph-index's private bootstrap concern, not a shared policy. The mode
taxonomy is superseded by the two-question gate below (ADR-084).

**Migration**: consumers call the collapsed gate (health alone); graph-index
keeps bootstrap exactness internally; callers that degraded instead of
erroring keep doing so at the call site.

### Requirement: View-rate readiness interpretation with hard stops

**Reason**: view-rate consumers no longer have a distinct readiness
interpretation. This requirement existed to give them a tolerance
(`index_lag_tolerance`, then `max_staleness`) that no other consumer had —
the last surviving piece of ADR-082's consumer-class split. ADR-085 deletes
the tolerance outright: the freshness machinery existed only to serve the
absence license ADR-084 retired, it had exactly one call site, and its
required satisfiability floor (a bound below the publish heartbeat is
unsatisfiable — measured at ~52% of ticks at 3s) was the evidence that the
question it asked cannot be answered at the resolution it was asked. Health
is now the whole gate for every consumer.

**Migration**: delete `index_lag_tolerance` / `max_staleness` from
graph-clustering configs; there is no replacement key. Detection runs
whenever the index is healthy and records the view age it ran at on
`staleness_at_detection_ms`. A config carrying either key fails startup
loudly rather than being silently ignored.

### Requirement: Community detection runs under bounded lag

**Reason**: bounded lag no longer exists as a concept. This requirement
mandated gating each pass "via the canonical `bounded-staleness` mode" with a
configured tolerance, and its default-preserves-the-exact-gate scenario is the
behavior ADR-085 deliberately reverses. Replaced by "Community detection runs
whenever the index is healthy" (ADDED above), which is a rename in substance
as well as name — the gate condition, the configuration surface, and the
default behavior all change, so it is recorded as a removal plus an addition
rather than a modification.

**Migration**: see the ADDED requirement. Operationally: delete the tolerance
key, expect detection to run on every tick, and read the view age off
`staleness_at_detection_ms` rather than inferring it from whether a run
happened.

## ADDED Requirements

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

## MODIFIED Requirements

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

- **GIVEN** an internal read returns a non-transient error (e.g. a real
  decode or connection failure)
- **WHEN** `Fuse` handles it
- **THEN** it propagates the error (not degraded to an empty envelope)

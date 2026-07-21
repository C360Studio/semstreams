# graph-view-subscription Specification

## Purpose

A shared read-side fan-out primitive (`pkg/graphview`) that lets many in-process
consumers tail one current-state projection of a busy KV bucket through a single
`WatchAll`, instead of one JetStream consumer per consumer. It collapses
O(N × writeRate) redundant decode/buffer work to O(writeRate), coalesces the
write firehose to a view-rate delta stream, and gives each subscriber an atomic
snapshot consistent with the deltas it then receives — with a coherence,
readiness, and poison contract that the hand-rolled per-client watchers it
replaces never shared. See ADR-081.

## Requirements

### Requirement: Single-watcher shared projection
A graph view SHALL maintain one authoritative in-memory current-state projection
per bucket from a single `WatchAll`, decoding and contract-validating each write
exactly once regardless of subscriber count — the fan-out win is
decode-once-amortized-across-N, never validation skipped. The view is a
non-owner reader: owner-only trusted decode (`UnmarshalEntityStateTrusted`,
gh#562) is forbidden on the view path by that API's own contract.

#### Scenario: One watcher serves many subscribers
- **GIVEN** a view over ENTITY_STATES with N attached subscribers
- **WHEN** a single entity write lands
- **THEN** the write is decoded and validated exactly once by the view
- **AND** no per-subscriber JetStream consumer exists (fan-out is in-process)

#### Scenario: Busy bucket does not stall the watcher
- **GIVEN** a bucket under a high sustained write rate
- **WHEN** the view processes the live tail
- **THEN** the view applies every delivered revision to the projection at write rate
- **AND** the view watcher never blocks on subscriber delivery

### Requirement: View-rate coalescing
A graph view SHALL coalesce fan-out to a configurable view-rate tick, emitting at
most one delta per changed key per window carrying the greatest-revision
operation for that key, so serve-rate is decoupled from write-rate. A delta is
`upsert(key, value, revision)`, `delete(key, revision)`, or
`poison(key, revision, error)`; tombstones and poison flow through the same
coalescing lane as upserts, last-writer-wins by revision (an out-of-band lane
could race the coalesced stream).

#### Scenario: Multiple writes to one key within a window
- **GIVEN** key K is written five times within one window
- **WHEN** the tick fires
- **THEN** subscribers receive exactly one delta for K carrying the newest value
- **AND** intermediate revisions of K are not delivered

#### Scenario: Coalescing never reorders
- **GIVEN** key K transitions revision R then R' where R' is greater than R
- **WHEN** deltas are delivered
- **THEN** no subscriber ever receives R after having received R'
- **AND** a tombstone counts as the value at its revision for this ordering

#### Scenario: Delete inside a window coalesces by revision
- **GIVEN** key K is deleted at revision R2 then recreated at R3 within one window
- **WHEN** the tick fires
- **THEN** subscribers receive only the R3 upsert (intermediate tombstone coalesced away)
- **AND** a window ending after R2 with no R3 delivers the R2 tombstone, never a pre-R2 value

### Requirement: Tombstone propagation to attached subscribers
A graph view SHALL deliver key deletion (`KeyValueDelete` and `KeyValuePurge`)
to every attached subscriber as a tombstone delta through the same ordered lane
as upserts; a subscriber's projection converges to key-absence after a delete.
Deletes MUST NOT be silently swallowed by coalescing or by a separate
out-of-band lane that can race the coalesced stream.

#### Scenario: Attached subscriber learns of the delete
- **GIVEN** subscriber X holds key K at revision R1 from its snapshot
- **WHEN** K is deleted at revision R2 and the next tick fires
- **THEN** X receives a tombstone delta for K at R2
- **AND** X's materialized state no longer contains K

#### Scenario: No resurrection across the delete
- **GIVEN** X received the tombstone for K at R2
- **WHEN** any earlier-captured delta batch containing K at R1 would be delivered
- **THEN** the R1 value is suppressed (revision-guarded), never delivered after R2

### Requirement: Snapshot-and-delta consistency
A graph view SHALL, on subscriber attach, produce an initial snapshot and register
the subscriber for deltas atomically at a single view sequence number S, such that
the snapshot reflects every key at sequence at or before S and the subscriber
receives every delta at sequence after S — no gap, no duplicate, no
stale-snapshot-over-newer-delta inversion. Snapshot capture MAY hold the
projection lock (bounded copy); snapshot delivery MUST NOT occur under the
projection lock.

#### Scenario: Attach during concurrent writes
- **GIVEN** writes are landing concurrently with a new subscriber attaching
- **WHEN** the subscriber receives its snapshot at sequence S and then deltas
- **THEN** every key changed at sequence at or before S is present in the snapshot
- **AND** every key changed at sequence after S arrives as a delta
- **AND** no change at sequence at or before S is also delivered as a delta

#### Scenario: No gap across the seam
- **GIVEN** a write at sequence S+1 is in flight while the snapshot is taken at S
- **WHEN** the subscriber begins tailing
- **THEN** the S+1 change is delivered as a delta and never silently dropped

### Requirement: Read-after-write coherence across the view
A graph view SHALL guarantee that no subscriber observes, for any key, a value
older than one the view has already applied at or before that subscriber's
snapshot sequence. Delta enqueue is part of the coherence contract: a delta
batch captured before a subscriber attached MUST NOT be delivered to that
subscriber past a newer value it already holds — either enqueue happens in the
same critical section as value-capture, or every enqueue is revision-guarded
against the subscriber's per-key high-water.

#### Scenario: No stale resurrection under a racing attach
- **GIVEN** the view has applied revision R for key K
- **WHEN** a subscriber attaches while an apply of R is racing snapshot capture
- **THEN** the subscriber observes K at revision R or newer, never a pre-R value

#### Scenario: No stale delivery across the tick seam
- **GIVEN** a tick detaches a coalesced batch holding K at R5, then the view applies K at R6,
  then subscriber X attaches and snapshots K at R6
- **WHEN** the detached batch is subsequently delivered
- **THEN** X does not receive the R5 value (suppressed or superseded before delivery)

#### Scenario: Deleted key is coherent
- **GIVEN** key K is deleted at revision R and the view has applied the delete
- **WHEN** a subscriber attaches after R
- **THEN** K is absent from the snapshot
- **AND** no later delta reintroduces a pre-R value of K

### Requirement: Readiness gate on initial replay
A graph view SHALL NOT serve an unmarked snapshot before its initial `WatchAll`
replay has completed (end-of-replay marker observed): attach before caught-up
either blocks until caught-up, fails with a typed not-ready error, or returns a
snapshot explicitly marked not-caught-up. The view SHALL expose its caught-up
state and applied-revision watermark so consumers and readiness surfaces can
gate on it (readiness = caught-up, not started).

#### Scenario: Attach during bootstrap is not silently partial
- **GIVEN** the view is mid-initial-replay over a populated bucket
- **WHEN** a subscriber attaches
- **THEN** it observes blocking, a typed not-ready error, or a snapshot marked not-caught-up
- **AND** it never mistakes a partial bootstrap snapshot for the full current state

#### Scenario: Watermark reflects applied revisions
- **GIVEN** the view has applied through bucket revision R
- **WHEN** a consumer reads the view's watermark
- **THEN** the watermark is at least R only after every revision at or before R was applied

### Requirement: Watcher loss fails closed
A graph view SHALL treat loss of its single shared watcher (updates channel
close, unrecoverable consumer error, context cancellation) as a fail-closed
event: attached subscribers receive an explicit staleness/error signal, new
attaches observe not-ready semantics, and the view never silently serves a
frozen projection as live. On re-bootstrap the view SHALL reconcile its
projection against the fresh replay — removing keys that no longer exist —
before reporting caught-up again.

#### Scenario: Subscribers learn the view went stale
- **GIVEN** N subscribers attached to a healthy view
- **WHEN** the shared watcher dies
- **THEN** every subscriber receives an explicit staleness/error signal
- **AND** no subscriber continues consuming the frozen projection as if live

#### Scenario: Re-bootstrap removes ghost keys
- **GIVEN** key K was deleted (and its tombstone purge-cleaned) while the watcher was down
- **WHEN** the view re-bootstraps and reports caught-up
- **THEN** K is absent from the projection and from post-recovery snapshots

### Requirement: Decode failure surfaces as poison, never silent skip
A graph view SHALL validate on decode and surface decode/contract failures to
its consumers as a typed per-key poison signal (per the ADR-079 authoritative
read-surface contract) rather than silently skipping the value or halting
delivery for unrelated keys. Projection-owner consumers (e.g. the graph-query
client's sticky poison latch, the lifecycle guard) SHALL be able to implement
their reset-required semantics from this signal alone — migrating a consumer
onto the view MUST NOT silently retire poison detection.

#### Scenario: Poisoned value does not launder through the view
- **GIVEN** a stored value that fails the canonical entity-state contract
- **WHEN** the view decodes it
- **THEN** subscribers receive a typed poison signal identifying the key
- **AND** the value is not delivered as a normal upsert
- **AND** deltas for other keys continue to flow

#### Scenario: Consumer poison latch survives migration
- **GIVEN** a consumer that today latches sticky poison from its own validating watcher
- **WHEN** that consumer reads through a shared view instead
- **THEN** the view's poison signal carries enough context to drive the same latch

### Requirement: Per-subscriber at-most-once backpressure
A graph view SHALL isolate subscribers such that a slow subscriber degrades to
staleness — its pending deltas coalesced last-writer-wins per key into a buffer
bounded by live changed-key cardinality (no count-based eviction; decoded values
shared, not copied per subscriber) — and never blocks the view watcher, the
projection apply, or any other subscriber. Slowness alone MUST NOT disconnect a
subscriber.

#### Scenario: Slow subscriber does not stall others
- **GIVEN** subscriber A stops draining while subscribers B and C drain normally
- **WHEN** the view continues to tick and fan out
- **THEN** B and C keep receiving fresh deltas at view rate
- **AND** the view watcher and projection apply are unaffected

#### Scenario: Slow subscriber coalesces rather than unbounded-buffers
- **GIVEN** subscriber A has an undrained pending delta and new ticks fire
- **WHEN** further changes accumulate for A
- **THEN** A's pending delta coalesces to newest-per-key
- **AND** when A resumes it converges to current state without replaying intermediate revisions

### Requirement: Coherent point reads
A graph view SHALL offer a point-read surface (`Get(key)` / bounded list) over
the same projection, coherent with the delta stream: a point read never returns
a value older than one already applied by the view, and honors the readiness
gate and poison semantics above. A point read of a poisoned key surfaces the
typed poison error; list surfaces enumerate kept entries (poisoned keys are
observable via point reads and the poison signal, not silently invented).

#### Scenario: Point read is not staler than the stream
- **GIVEN** the view applied K at revision R
- **WHEN** any caller point-reads K
- **THEN** the result is K at R or newer (or key-absent if deleted at or after R)

### Requirement: View lifecycle and ownership
A graph view SHALL be explicitly constructed and owned (injected into its
consumers — no ambient process-global registry); subscriber detach SHALL release
that subscriber's buffered state; view shutdown SHALL terminate every
subscription with an explicit terminal signal (never a silent hang).

#### Scenario: Detach releases resources
- **GIVEN** a subscriber detaches (or its context is cancelled)
- **WHEN** subsequent ticks fire
- **THEN** the view retains no pending buffer for it

#### Scenario: Shutdown is observable
- **GIVEN** a view with attached subscribers is stopped
- **WHEN** shutdown completes
- **THEN** every subscriber observes an explicit terminal close, not a hang

### Requirement: Coexistence with raw WatchAll
The graph view primitive SHALL NOT replace raw `WatchAll`; consumers needing
independent filters, historical replay, per-revision delivery, or independent
ack semantics continue to open real JetStream consumers.

#### Scenario: Independent consumer is unaffected
- **GIVEN** a consumer that needs a distinct filter and historical replay
- **WHEN** a shared view exists over the same bucket
- **THEN** that consumer still opens its own `WatchAll`
- **AND** the view imposes no constraint on it

### Requirement: The view reports its currency without gating on it
The view SHALL expose the KV server write time of the newest revision it has
applied, alongside the applied-revision watermark, as a single atomic pair read
under one critical section — a caller reading the two values separately can
observe a torn pair. The same timestamp SHALL be carried on the snapshot handed
to an attaching subscriber, so a subscriber can tell how current the snapshot it
received was. No view API SHALL gate on this value: it exists so a consumer can
stamp currency on its own output, the same way community detection records
`staleness_at_detection_ms` (ADR-085). The view's existing gates — not-ready
until the initial replay completes, and fail-closed on watcher loss — are
unchanged and remain the only conditions that withhold a read.

The value SHALL be reported honestly as a floor rather than an oracle: a write
not yet delivered to the watcher cannot age it, so a stalled feed looks
arbitrarily current by this measure alone, and total-stall detection remains the
watcher-loss path's job. Before any revision has been applied there SHALL be no
timestamp — the zero `time.Time`, never a fabricated present instant — so a
consumer computing an age must test for it, mirroring the "not computable"
encoding the readiness envelope uses for the same situation.

#### Scenario: Currency advances with the applied watermark
- **GIVEN** a live view that has applied revisions up to R
- **WHEN** a subsequent write at revision R+1 is applied
- **THEN** the reported pair advances to R+1 together with that entry's KV
  server write time, in one atomic read

#### Scenario: A stale view still serves, and says how stale
- **GIVEN** a live, healthy view whose newest applied write is arbitrarily old
- **WHEN** a consumer performs a point read or attaches
- **THEN** the read succeeds — age never withholds a read — and the reported
  currency lets the consumer stamp the age on its own output

#### Scenario: Nothing applied yet reports no timestamp
- **GIVEN** a view that has become live without having applied any revision
  (an authoritatively empty bucket)
- **WHEN** a consumer reads the currency pair
- **THEN** the timestamp is the zero value rather than the current time, and
  the consumer can distinguish "no basis to compute an age" from "current"

#### Scenario: The snapshot carries the currency it was captured at
- **GIVEN** a subscriber attaching to a live view
- **WHEN** it receives its snapshot
- **THEN** the snapshot carries both the applied revision and the applied
  timestamp the view held at the atomic capture instant

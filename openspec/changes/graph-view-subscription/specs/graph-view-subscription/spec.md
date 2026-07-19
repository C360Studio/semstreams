# graph-view-subscription — delta

## ADDED Requirements

### Requirement: Single-watcher shared projection
A graph view SHALL maintain one authoritative in-memory current-state projection
per bucket from a single `WatchAll`, decoding each write once regardless of
subscriber count, and SHALL use the trusted-decode fast path so the lone watcher
does not slow-consume on a busy bucket.

#### Scenario: One watcher serves many subscribers
- **GIVEN** a view over ENTITY_STATES with N attached subscribers
- **WHEN** a single entity write lands
- **THEN** the write is decoded exactly once by the view
- **AND** no per-subscriber JetStream consumer exists (fan-out is in-process)

#### Scenario: Busy bucket does not stall the watcher
- **GIVEN** a bucket under a high sustained write rate
- **WHEN** the view processes the live tail
- **THEN** the watcher applies the newest value per key per window
- **AND** the view watcher never blocks on subscriber delivery

### Requirement: View-rate coalescing
A graph view SHALL coalesce the write firehose to a configurable view-rate tick,
emitting at most the newest value per changed key per window, so serve-rate is
decoupled from write-rate.

#### Scenario: Multiple writes to one key within a window
- **GIVEN** key K is written five times within one 250ms window
- **WHEN** the tick fires
- **THEN** subscribers receive exactly one delta for K carrying the newest value
- **AND** intermediate revisions of K are not delivered

#### Scenario: Coalescing never reorders
- **GIVEN** key K transitions revision R then R' where R' is greater than R within a window
- **WHEN** the delta is delivered
- **THEN** the delivered value is R'
- **AND** no subscriber ever receives R after having received R'

### Requirement: Snapshot-and-delta consistency
A graph view SHALL, on subscriber attach, produce an initial snapshot and register
the subscriber for deltas atomically under one lock at a single view sequence
number S, such that the snapshot reflects every key at sequence at or before S and
the subscriber receives every delta at sequence after S — no gap, no duplicate, no
stale-snapshot-over-newer-delta inversion.

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
older than one the view has already applied at or before that subscriber's snapshot
sequence; a subscriber attaching after the view processed revision R for key K
SHALL NOT receive a snapshot missing R nor a later delta regressing K below R.

#### Scenario: No stale resurrection under a racing invalidate
- **GIVEN** the view has applied revision R for key K
- **WHEN** a subscriber attaches while an apply of R is racing snapshot capture
- **THEN** the subscriber observes K at revision R or newer, never a pre-R value
- **AND** a slow apply cannot resurrect a pre-write value past a newer applied revision

#### Scenario: Deleted key is coherent
- **GIVEN** key K is deleted at revision R and the view has applied the delete
- **WHEN** a subscriber attaches after R
- **THEN** K is absent from the snapshot
- **AND** no later delta reintroduces a pre-R value of K

### Requirement: Per-subscriber at-most-once backpressure
A graph view SHALL isolate subscribers such that a slow subscriber degrades to
staleness — its pending delta coalesced last-writer-wins into a bounded buffer —
and never blocks the view watcher, the projection apply, or any other subscriber.

#### Scenario: Slow subscriber does not stall others
- **GIVEN** subscriber A stops draining while subscribers B and C drain normally
- **WHEN** the view continues to tick and fan out
- **THEN** B and C keep receiving fresh deltas at view rate
- **AND** the view watcher and projection apply are unaffected

#### Scenario: Slow subscriber coalesces rather than unbounded-buffers
- **GIVEN** subscriber A has an undrained pending delta and new ticks fire
- **WHEN** further changes accumulate for A
- **THEN** A's pending delta coalesces to newest-per-key with bounded memory
- **AND** when A resumes it converges to current state without replaying every intermediate revision

### Requirement: Coexistence with raw WatchAll
The graph view primitive SHALL NOT replace raw `WatchAll`; consumers needing
independent filters, historical replay, or independent ack semantics continue to
open real JetStream consumers.

#### Scenario: Independent consumer is unaffected
- **GIVEN** a consumer that needs a distinct filter and historical replay
- **WHEN** a shared view exists over the same bucket
- **THEN** that consumer still opens its own `WatchAll`
- **AND** the view imposes no constraint on it

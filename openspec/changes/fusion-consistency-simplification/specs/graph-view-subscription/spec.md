# graph-view-subscription — delta

## ADDED Requirements

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

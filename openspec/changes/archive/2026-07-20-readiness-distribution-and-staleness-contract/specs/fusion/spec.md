# fusion — delta

## REMOVED Requirements

### Requirement: The projection carries a view-revision consistency contract

**Reason**: `ViewRevision.Coherent` was never soundly provable — the engine
assembles a projection from N independent reads with no snapshot and no
consistent cut, so two revision samples agreeing can never prove the reads
between them hit one revision. ADR-083's heartbeat distribution turned the
unsound signal into a vacuous one (both samples read the same held value), and
a downstream consumer used the claim to license deleting entities absent from
the projection — an authoritative-absence claim readiness must not license.
The claim is deleted (ADR-083, third break), not re-tuned.

**Migration**: consumers drop the `coherent` key from `view_revision` decoders;
any reconciliation that deleted items absent from a projection moves to the
graph-view-subscription capability (`pkg/graphview`, ADR-081), which has real
snapshot semantics. See
`docs/operations/migration-readiness-distribution-adr083.md` Break 3.

## ADDED Requirements

### Requirement: The projection reports view-revision observations, never a coherence claim

The graph projection SHALL report the indexed revision sampled before resolution
(start) and re-sampled after the fetch phase (end) as plain observations, and
SHALL NOT carry any field claiming the projection reflects a single indexed
revision — such a claim is not provable from samples of a heartbeat-published
status feed (ADR-083). A failed re-sample SHALL report end=0 rather than a
guessed revision. A consumer that needs a genuinely coherent single-revision
view uses the graph-view-subscription capability (ADR-081); the fusion
projection is best-effort ranked evidence.

#### Scenario: the observed span is reported verbatim

- **GIVEN** the sampled indexed revision differs between the pre-resolution
  sample and the post-fetch re-sample
- **WHEN** the response is returned
- **THEN** the view revision reports the unequal start and end bounds verbatim

#### Scenario: the wire carries no coherence claim

- **GIVEN** a response built entirely at one observed revision
- **WHEN** the graph projection is serialized
- **THEN** the view revision reports equal start and end bounds
- **AND** no coherent field exists on the wire

#### Scenario: a failed re-sample degrades honestly

- **GIVEN** the post-fetch status re-sample fails
- **WHEN** the response is returned
- **THEN** the view revision reports end=0, never a guessed revision

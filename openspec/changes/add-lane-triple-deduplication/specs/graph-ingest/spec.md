# graph-ingest — Delta

## ADDED Requirements

### Requirement: The add lane MUST NOT append a triple already stored with an identical tuple

The add lane MUST suppress any triple whose six-field identity tuple — subject, predicate,
object, datatype, source, context — already exists on the target entity. Suppression is
unconditional: it does not depend on `RequestID`, on `Context` carrying a request identifier, on
the caller, or on which entry point was used. It MUST cover every add-lane emitter from a single
implementation, including the `graph.mutation.triple.add` and `graph.mutation.triple.add_batch`
handlers, hierarchy inference's in-process adder, foreign-edge regroup, and the projection
mutation client's append-evidence path. Duplicates appearing more than once within a single
request MUST also collapse, preserving first-input order, so one request commits at most one copy
of a tuple.

#### Scenario: a stored tuple is not appended again

- **GIVEN** an entity already carries a triple with a given subject, predicate, object, datatype,
  source, and context
- **WHEN** an add request submits a triple with that same six-field tuple
- **THEN** the entity still carries exactly one copy of that tuple

#### Scenario: duplicates within one batch collapse to one

- **GIVEN** an entity that carries none of the submitted tuples
- **WHEN** a single batch submits the same six-field tuple three times, interleaved with two
  distinct tuples
- **THEN** the entity gains exactly three triples
- **AND** their relative order matches first appearance in the request

#### Scenario: suppression does not depend on a request identifier

- **GIVEN** an entity already carries a triple stamped with context `inference.hierarchy` and no
  request identifier
- **WHEN** the identical triple is submitted again with no request identifier
- **THEN** it is suppressed
- **AND** the outcome is the same as for a request that carries one

### Requirement: A fully-duplicate add request MUST advance no ENTITY_STATES revision

A request whose triples are all suppressed as duplicates MUST commit nothing. The check MUST
short-circuit inside the compare-and-swap closure, before the revision-checked write, so no
`ENTITY_STATES` revision advances, no watcher re-fires, and the entity's version and update
timestamp are unchanged. Returning the unchanged entity from the closure is NOT sufficient: that
is an identity rewrite, which bumps the revision and re-fires every watcher — the precise
behavior that makes a restart's replay observable downstream. The request MUST report success,
not an error, and MUST NOT increment the component error counter.

#### Scenario: a duplicate-only write leaves the revision untouched

- **GIVEN** an entity at a known KV revision whose triples include every tuple about to be
  submitted
- **WHEN** the add request is processed
- **THEN** the request succeeds
- **AND** the entity's KV revision is unchanged
- **AND** the entity's version and update timestamp are unchanged

#### Scenario: a restart's replay is invisible to watchers

- **GIVEN** a component that re-submits the identical derived triples for unchanged entities on
  restart
- **WHEN** the component restarts against the same store with no source data changed
- **THEN** no `ENTITY_STATES` watcher observes an update for those entities
- **AND** stored triple cardinality is identical before and after the restart

#### Scenario: a partially-duplicate write commits only the new tuples

- **GIVEN** an entity carrying one of two submitted tuples
- **WHEN** the add request is processed
- **THEN** exactly one triple is appended
- **AND** the KV revision advances exactly once

### Requirement: Deduplication MUST be atomic against concurrent identical requests

Deduplication MUST run inside the same compare-and-swap closure whose write is revision-checked,
so a request that loses the CAS re-evaluates against the winner's committed state and suppresses
on the retry. A read performed outside the CAS loop MUST NOT be used to decide suppression: it
reintroduces the time-of-check-to-time-of-use window in which two concurrent identical requests
both observe the tuple absent and both append.

#### Scenario: two concurrent identical appends store one tuple

- **GIVEN** an entity that does not yet carry the tuple
- **WHEN** two identical add requests are issued concurrently
- **THEN** exactly one copy of the tuple is stored
- **AND** both requests report success

#### Scenario: a late commit followed by an identical retry stores one tuple

- **GIVEN** an add request whose response is lost after the write commits
- **WHEN** the caller retries with identical provenance
- **THEN** exactly one copy of the tuple is stored
- **AND** the retry reports success

### Requirement: Add-lane responses MUST count only newly appended tuples

`WrittenCount` MUST report the number of tuples newly appended, excluding every suppressed
duplicate. A request in which everything was suppressed MUST return `WrittenCount` zero with an
empty failed-subject set and no error — success with nothing written is a valid outcome, and a
suppressed subject MUST NOT appear among failed subjects. The response MUST additionally report
how many tuples were suppressed, so a caller can distinguish "already present" from "no traffic"
without an authoritative read-back. Any reported KV revision MUST be the live, unchanged revision
of the entity, never zero.

#### Scenario: an all-duplicate batch reports zero written and no failures

- **GIVEN** an entity carrying every tuple in the batch
- **WHEN** the batch is submitted
- **THEN** the response reports zero written and no failed subjects
- **AND** the response reports a suppressed count equal to the batch size
- **AND** no error is returned

#### Scenario: written plus suppressed accounts for the whole request

- **GIVEN** a batch of tuples of which some are already stored
- **WHEN** the batch is submitted
- **THEN** written plus suppressed equals the number of tuples submitted

### Requirement: Suppressed duplicates MUST be observable

Suppressed duplicates MUST be counted on a metric labeled by the lane that submitted them, so an
operator can tell a silently-skipped write from absent traffic, and can attribute a sustained
suppression rate to the component producing it. A suppression MUST NOT be logged per occurrence:
replay traffic makes that unbounded.

#### Scenario: suppression is attributable to its lane

- **GIVEN** hierarchy inference re-submits derived triples on restart
- **WHEN** those triples are suppressed
- **THEN** the suppressed-duplicate counter rises with a label identifying that lane

### Requirement: Duplicate suppression MUST NOT suppress redelivery side effects

Suppression MUST cover the KV write only. The applied-sequence redelivery guard remains the sole
gate on whether a redelivered message is re-applied, and every post-commit side effect of the
lanes that own them — suffix-index maintenance, relationship-target creation, foreign-edge
routing — MUST still run when their lane runs, so a crash mid-apply is re-driven on redelivery
rather than being masked by a suppression that looks like a completed write.

#### Scenario: a redelivered message still re-drives its post-commit work

- **GIVEN** a message whose triples are all already stored
- **WHEN** it is redelivered and its triples are suppressed
- **THEN** the lane's post-commit side effects still execute

### Requirement: Stored duplicate triples MUST remain readable and MUST NOT be removed

Entities that already accumulated duplicate triples before this contract MUST continue to read
and serve normally. No backfill, sweep, or migration removes them, and the stored-state
structural contract MUST NOT gain a duplicate-rejection rule — such a rule would reclassify every
already-affected entity as invalid, converting a cosmetic accumulation into a service outage.

#### Scenario: a pre-existing duplicate does not poison its entity

- **GIVEN** an entity whose stored state carries two identical triples written before this
  contract
- **WHEN** the entity is read
- **THEN** it serves normally with both triples present

#### Scenario: a new add against a duplicated entity still suppresses

- **GIVEN** an entity carrying two identical copies of a tuple
- **WHEN** that tuple is submitted again
- **THEN** nothing is appended and the revision is unchanged

## MODIFIED Requirements

### Requirement: A re-arriving entity's triples merge by predicate-level replacement

graph-ingest MUST merge the incoming triples of a re-arriving (already-existing)
entity by replacing per `(subject, predicate)`, not by appending, when the write
comes through the Graphable (JetStream) ingest lane. A predicate carried by the
incoming entity MUST replace that `(subject, predicate)`'s prior triples, so the
entity does not accumulate duplicate triples across repeated arrivals.

This is NOT the same rule the mutation add lane applies. The add lane is append-only and
deduplicates by exact six-field tuple, so it preserves multiple distinct values under one
predicate — which is what multi-valued predicates such as hierarchy containment and sibling edges
require. Predicate-level replacement on that lane would delete them. The two lanes converge only
on the outcome that a repeated identical write accumulates nothing.

#### Scenario: republishing the same entity does not accumulate duplicates

- **GIVEN** an entity previously ingested with `flock.position.x = 1`
- **WHEN** the same entity is ingested again with `flock.position.x = 2`
- **THEN** the stored entity has exactly one `flock.position.x` triple
- **AND** its value is `2`

#### Scenario: the add lane preserves multiple values under one predicate

- **GIVEN** an entity carrying two `hierarchy.type.contains` triples with distinct objects
- **WHEN** a third distinct `hierarchy.type.contains` triple is added through the mutation add
  lane
- **THEN** the entity carries three `hierarchy.type.contains` triples

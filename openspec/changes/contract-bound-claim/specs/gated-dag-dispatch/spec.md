# gated-dag-dispatch — delta (contract-bound-claim)

## MODIFIED Requirements

### Requirement: A failed publish rolls the claim back

The executor MUST clear a unit's durable claim when its dispatch publish fails
(the ack did not return), because a non-acked publish is proof the message was not
persisted and will not be delivered. The unit is then re-selected on the next
evaluation instead of being stranded until a manual reset.

The rollback MUST be a conditional release: it MUST verify the claim it clears is this
executor's own, so a rollback racing another instance's newer claim cannot clear it. A
rollback finding the claim already absent MUST succeed as a typed no-op.

#### Scenario: publish-ack failure re-arms the unit

- **GIVEN** the executor committed a unit's claim and then the dispatch publish
  failed to ack
- **WHEN** the next evaluation runs
- **THEN** the unit's claim has been cleared
- **AND** the unit is re-selected for dispatch (not skipped as claimed)

#### Scenario: rollback cannot clear a foreign claim

- **GIVEN** another instance claimed the unit between this executor's failed publish and its
  rollback
- **WHEN** the rollback runs
- **THEN** the foreign claim remains and the rollback returns a typed failure this executor
  treats as "unit no longer mine"

## ADDED Requirements

### Requirement: The unit claim MUST be contract-bound and CAS-conditioned, not a raw graph write

The gated-DAG claim path MUST acquire and release unit claims through the public claim
capability — bound to a declared contract, carrying the executor's owner token, conditioned
on the unit's authoritative revision, with the claimant identified in the claim value. The
component MUST NOT marshal graph mutation wire requests or declare graph mutation subject
constants for claiming. Two concurrent claimers of one unit revision MUST NOT both dispatch.

#### Scenario: concurrent executors, one dispatch

- **WHEN** two executor instances select the same unit from the same read revision
- **THEN** exactly one claim commits and dispatches; the other observes a typed non-committed
  outcome and does not dispatch

### Requirement: An ambiguous claim outcome MUST be resolved before re-selection

When a claim attempt's outcome is transport-ambiguous, the executor MUST resolve it through
the claim capability's read-back before treating the unit as unclaimed: a resolved committed
claim proceeds to dispatch; only resolved not-committed re-arms selection. The executor MUST
NOT overwrite an ambiguous claim with a fresh claim attempt.

#### Scenario: timed-out claim that committed still dispatches once

- **GIVEN** a claim request times out but actually committed
- **WHEN** the executor resolves the outcome by read-back
- **THEN** the unit proceeds to dispatch exactly once and is not re-claimed as if unclaimed

# projection-mutation-client — delta (contract-bound-claim)

> Written against the post-`entity-read-with-revision` text of this spec; re-verify wording
> if that change's deltas shift before this one archives.

## MODIFIED Requirements

### Requirement: Narrow public capabilities

The framework MUST expose narrow interfaces for authoritative entity creation, owned replacement, evidence append,
authoritative read-back, and CAS claim transition. The concrete client MAY satisfy all five interfaces.

The public API MUST NOT expose rule identifiers, rule action types, arbitrary removal predicates, or raw
owner-token strings. Entity KV revisions MUST be exposed only as typed values: returned by the revision-bearing
authoritative read and by mutation receipts, and accepted as an optional condition on owned replacement — never as
raw lifecycle-internal state.

#### Scenario: Least-privilege consumer

- **WHEN** a component only appends evidence
- **THEN** it can accept the evidence-appender interface without receiving create or replace capabilities

#### Scenario: Revisions are typed contract values

- **WHEN** a caller obtains a revision from the revision-bearing read or a receipt
- **THEN** the only public use of that value is as an expected-revision condition, and no API accepts or returns
  raw owner-token strings alongside it

#### Scenario: Claim capability is separately acceptable

- **WHEN** a component performs only claim transitions
- **THEN** it can accept the claim interface without receiving create, replace, or append capabilities

## ADDED Requirements

### Requirement: A claim transition MUST be contract-bound, token-bearing, and revision-conditioned

The client MUST offer a claim transition over a predicate resolved through a declared
cas-transition group of the bound contract, transported with the bound owner token, and
conditioned on the authoritative revision of the target entity. The committed claim value
MUST identify the claimant. Two concurrent claimers of the same entity and revision MUST NOT
both receive committed success; the loser MUST receive the typed revision-conflict or
already-claimed outcome. Under fail-closed lease enforcement, a stale owner token MUST fail
with the public typed stale-owner-token error.

#### Scenario: Exactly one winner

- **WHEN** two claimers read the same entity revision and both submit claim transitions
- **THEN** exactly one receives a committed receipt carrying the committed revision, and the
  other receives a typed non-committed outcome

#### Scenario: Resident foreign claim refused locally

- **WHEN** a claim transition targets an entity whose claim predicate already carries another
  claimant's value
- **THEN** the client returns a typed already-claimed outcome without publishing a mutation

#### Scenario: Stale token under enforcement

- **WHEN** lease enforcement is fail-closed and a claim transition carries a stale owner token
- **THEN** the outcome is the public typed stale-owner-token error

### Requirement: An ambiguous claim outcome MUST be resolved by authoritative read-back

When the claim transition's transport outcome is unknown, the client MUST resolve it by
authoritative read: the claim predicate carrying the claimant's own value resolves to
committed with the read revision; absent or foreign resolves to not-committed. Only a failed
resolution read MAY yield the unknown commit state, and the receipt MUST say so via the
typed commit-state taxonomy — the caller MUST never be left to guess whether the claim
committed.

#### Scenario: Lost response, claim actually committed

- **WHEN** a claim transition times out but the write committed
- **THEN** read-back resolves the receipt to committed and the caller may proceed as the
  claim holder

#### Scenario: Lost response, claim did not commit

- **WHEN** a claim transition times out and the write did not commit
- **THEN** read-back resolves the receipt to not-committed and the caller may safely retry
  from a fresh read

### Requirement: Unclaim MUST be conditional on claimant and revision

The claim capability's release MUST verify, under the same revision condition, that the
resident claim value identifies the releasing claimant before removing it. A newer or
different claim MUST NOT be cleared by another worker's release. Releasing an absent claim
MUST be a typed no-op success so rollback replay converges.

#### Scenario: Foreign claim protected from release

- **WHEN** worker A releases a unit whose claim predicate now carries worker B's value
- **THEN** the release fails typed, worker B's claim is untouched

#### Scenario: Release replay converges

- **WHEN** a release is retried after its first attempt already removed the claim
- **THEN** the retry returns a typed no-op success

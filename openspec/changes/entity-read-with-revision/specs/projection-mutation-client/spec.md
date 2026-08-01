# projection-mutation-client — delta (entity-read-with-revision)

## MODIFIED Requirements

### Requirement: Narrow public capabilities

The framework MUST expose narrow interfaces for authoritative entity creation, owned replacement, evidence append,
and authoritative read-back. The concrete client MAY satisfy all four interfaces.

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

### Requirement: Authoritative read-back

The framework MUST expose authoritative entity read-back using the existing graph-ingest entity query subject and
entity representation.

Mutation verification MUST use this authoritative path rather than watcher, index, or cache state.

The client MUST additionally offer a revision-bearing authoritative read returning the entity together with the KV
revision of the exact entry whose bytes produced it, suitable for use unchanged as an expected revision in a
conditional replacement. A bare reply from a server that does not support the revision opt-in MUST surface as a
typed revision-unavailable outcome, never as a zero or fabricated revision.

#### Scenario: Verification bypasses projection cache

- **WHEN** a mutation result requires verification
- **THEN** the client queries graph-ingest authoritative state even if a local watcher contains the entity

#### Scenario: Revision-bearing read feeds a conditional write

- **WHEN** a caller performs the revision-bearing read of an existing entity
- **THEN** the returned revision is non-zero and a conditional replacement expecting it either commits or returns
  the typed revision-conflict outcome, never an unconditioned overwrite

#### Scenario: Old server without the opt-in

- **WHEN** the revision-bearing read receives a bare entity reply
- **THEN** the client returns a typed revision-unavailable outcome and does not invent a revision

### Requirement: Schema-derived owned replacement

`ReplaceOwnedMutation` MUST expose an optional `Group` selector. A non-empty selector MUST resolve exactly one named
`replace-owned` group in the named contract.

An omitted selector MUST be accepted only when the contract has exactly one `replace-owned` group. It MUST be
rejected when the contract has none or more than one. Existing unnamed groups remain usable through this
single-group omission rule but cannot be selected by name.

The owned replacer MUST derive the complete removal set from only the selected group. Desired triples MUST be
limited to that group. Omitted predicates in the selected group MUST be removed, and sibling groups, birth
predicates, foreign predicates, and append-only predicates MUST be preserved.

Authoritative replace verification MUST compare complete canonical `message.Triple` values for the selected group,
including `Confidence` and `ExpiresAt`, and MUST prove omitted selected-group facts absent. It MUST ignore sibling
groups.

`ReplaceOwnedMutation` MUST additionally expose an optional expected revision. Zero MUST preserve today's
unconditioned replacement exactly. A non-zero expected revision MUST be transported unchanged to the conditional
update lane; a mismatch MUST surface as the typed revision-conflict outcome with commit state not-committed, from
which the caller's documented recovery is refetch-then-retry.

#### Scenario: Delete on omission

- **WHEN** desired state omits a predicate declared in the selected replace-owned group
- **THEN** the existing update-with-triples request includes that predicate in its removal set

#### Scenario: Named group preserves siblings

- **WHEN** a caller selects one named replace-owned group in a contract with multiple groups
- **THEN** the removal set contains every predicate in only that selected group
- **AND** predicates in sibling groups are not removed or considered during verification

#### Scenario: Selector omitted for one group

- **WHEN** a contract has exactly one replace-owned group and the caller omits `Group`
- **THEN** that group is selected whether or not it has a name

#### Scenario: Selector omitted for multiple groups

- **WHEN** a contract has multiple replace-owned groups and the caller omits `Group`
- **THEN** the client returns an invalid error before publishing

#### Scenario: Unknown or non-replace group

- **WHEN** `Group` names no group, an unnamed group, or a group in another write mode
- **THEN** the client returns an invalid error before publishing

#### Scenario: Caller attempts foreign removal

- **WHEN** desired state or a requested operation would replace a foreign or append-only predicate
- **THEN** the client returns an invalid error before publishing

#### Scenario: Replace transport retry

- **WHEN** replacement encounters a retryable transport failure within its context and retry budget
- **THEN** the client may resend the identical replacement request
- **AND** the owner token and schema-derived removal set remain unchanged

#### Scenario: Conditional replacement loses the race

- **WHEN** a replacement carries a non-zero expected revision and the entity's revision has advanced past it
- **THEN** the client returns the typed revision-conflict outcome with commit state not-committed
- **AND** a refetch returns the advanced revision from which a rebuilt replacement can succeed

#### Scenario: Zero expected revision is unconditioned

- **WHEN** a replacement omits the expected revision
- **THEN** behavior is identical to today's unconditioned owned replacement

## MODIFIED Requirements

### Requirement: The authority pair is bounded at configuration load

Configuration load MUST reject a `platform.org`/`platform.id` pair that cannot carry the framework-minted entropy
suffix: `len(org) + len(platform) + 7` MUST NOT exceed the budget derived from the longest fixed-suffix framework
family — `256 − 88 = 168` bytes while the agent-run family (`chain.agent.execution.` + 64 hex + two separators) is
the longest — naming the binding family in the error. The seven reserved bytes are the suffix
`component-runtime-config` mints onto `platform.id` (`-` plus six hex bytes); reserving them at load is what stops a
pair that fits only unsuffixed from being durably recorded and then refused forever, which ADR-102 decision 7 makes
unrepairable. A declared pair may therefore be at most 161 bytes.

The reserve MUST apply only where a pair is DECLARED, and a configuration MUST declare the stem — never a minted
identifier — so that one field carries one kind of value. An effective pair — a minted identifier, an adopted
identity record's, or the running configuration's — already carries whatever suffix it will ever carry and MUST be
bounded at the full 168-byte budget; reserving the same seven bytes against it as well would refuse, after Start, a
declaration that had already passed load. Every declaration boundary MUST apply the same 161-byte bound and every
effective-pair boundary the same 168-byte bound, so no path can admit a pair another path rejects. The budget MUST
be derived from the framework's own family table, never configured by the operator.
Framework constructors MUST keep fail-closed canonical validation as the second layer. This amends ADR-076 decision
2: framework identities are bounded, not fixed-length.

#### Scenario: an oversized authority pair does not boot

- **GIVEN** a configuration whose `platform.org` and `platform.id` total 162 bytes
- **WHEN** configuration load runs
- **THEN** it returns an error naming the agent-run family, the 168-byte budget, and the seven bytes reserved for
  the minted suffix
- **AND** the test that verifies this is `TestConfigRejectsOversizedAuthorityPair`

#### Scenario: a pair that fits only unsuffixed is refused before anything is minted

- **GIVEN** a configuration whose `platform.org` and `platform.id` total 163 bytes, which would fit the 168-byte
  budget unsuffixed but not once seven bytes are minted onto it
- **WHEN** configuration load runs
- **THEN** the load fails and no identity record is created
- **AND** the test that verifies this is `TestConfigRejectsPairThatOnlyFitsUnsuffixed`

#### Scenario: a pair at the declarable budget boots

- **GIVEN** a configuration whose `platform.org` and `platform.id` total exactly 161 bytes
- **WHEN** the deployment loads that configuration and starts against an empty bucket
- **THEN** the load succeeds, the entropy suffix is minted, the effective pair is 168 bytes, and Start succeeds
- **AND** the tests that verify this are `TestMaximumDeclarablePairMintsAndStarts` and
  `TestEffectivePairIsBoundedWithoutTheDeclarationReserve`

## ADDED Requirements

### Requirement: A framework family minted from another entity derives a collision-free instance

A framework builder that mints an entity from another entity MUST derive the instance segment as the lowercase hex
SHA-256 of a length-framed byte sequence over a versioned digest domain and that origin's full canonical
identifier, truncated to the family's declared `InstanceBytes`, composed through
`pkg/types.FrameworkIdentityFamily.DerivedEntityID(org, platform, digestDomain, frames...)` under the deployment's
own authority — the one home for framed-digest derivation. `DerivedEntityID` MUST refuse an empty frame and MUST
refuse a family whose `InstanceBytes` is 0 or greater than 64, so a family with a shorter fixed instance truncates
deterministically rather than failing or composing an invalid identity. The agent-run family
(`chain.agent.execution`, 64-byte instance, digest domain `semstreams.agent.run.v1`) MUST be a member of the family
table, and `agentrun.Mint(ctx, mgr, org, platform, originEntityID)` MUST be its only minter. No exported builder
MAY compose a derived family's identity from a fragment of its origin; the corpus audit MUST report
`derived_family_composed` for any production Go format builder, constructor, or prefix constant whose positions 3–5
equal a derived family outside the family table's own file. A consumer of a derived identity MUST read it from
where the framework carried it (the run entity, `agent.run.entity-id`, `RunEntityID` on the wire, tool metadata)
and MUST NOT recompute it.

#### Scenario: two foreign origins sharing an instance token stay distinct

- **GIVEN** a deployment with authority `acme`/`dep1`
- **AND** two imported origins `peer1.x.agentic-loop.agent.execution.abc` and
  `peer2.y.agentic-loop.agent.execution.abc`
- **WHEN** the framework mints its run entity for each origin
- **THEN** the two local identities differ and each is `acme.dep1.chain.agent.execution.<64 hex>`
- **AND** neither mint returns the entity the other origin's mint produced
- **AND** the test that verifies this is `TestMint_TwoOriginsAtOneInstanceMintDistinctRuns`

#### Scenario: a shorter fixed-instance family truncates deterministically

- **GIVEN** a family whose `InstanceBytes` is 16
- **WHEN** `DerivedEntityID` composes an identity for it
- **THEN** the instance is the first 16 hex bytes of the framed digest, and a family declaring `InstanceBytes` 0 or
  greater than 64 is refused
- **AND** the test that verifies this is `TestDerivedEntityIDTruncatesToFamilyInstanceBytes`

#### Scenario: the derivation has one home

- **GIVEN** a production Go file composing `fmt.Sprintf("%s.%s.chain.agent.execution.%s", …)` outside
  `pkg/types/framework_identity_families.go`
- **WHEN** `task entity-id:audit` runs
- **THEN** it reports the occurrence with reason `derived_family_composed`
- **AND** the test that verifies this is `TestAuditFlagsDerivedFamilyComposedOutsideItsHome`

## MODIFIED Requirements

### Requirement: The authority pair is bounded at configuration load

Configuration load MUST reject a `platform.org`/`platform.id` pair whose combined length exceeds the budget derived
from the longest fixed-suffix framework family — `256 − 88 = 168` bytes for `len(org) + len(platform)` while the
agent-run family (`chain.agent.execution.` + 64 hex + two separators) is the longest — naming the binding family in
the error. The budget MUST be derived from the framework's own family table, never configured by the operator, and it
MUST be re-applied to the effective pair after the entropy suffix is minted (`component-runtime-config`). Framework
constructors MUST keep fail-closed canonical validation as the second layer. This amends ADR-076 decision 2: framework
identities are bounded, not fixed-length.

#### Scenario: an oversized authority pair does not boot

- **GIVEN** a configuration whose `platform.org` and `platform.id` total 169 bytes
- **WHEN** configuration load runs
- **THEN** it returns an error naming the agent-run family and the 168-byte budget
- **AND** the test that verifies this is `TestConfigRejectsOversizedAuthorityPair`

### Requirement: Prefix lengths have fixed meanings and the instance position is last

`pkg/types` MUST export the named prefix levels `SourcePrefix` (three positions), `TaxonomyPrefix` (four), and
`TypePrefix` (five), and MUST NOT export a helper whose meaning depends on a position order other than the canonical
one. The two-position deployment level MUST remain expressible as a query prefix; it has no exported accessor because
no consumer exists (owner greenfield ruling, #1168). A query prefix of length n MUST mean exactly the level named for
n. Grouping by a non-prefix combination (a taxonomy across sources) MUST be expressed as an exact-arity wildcard
pattern or KV filter, never as a prefix. The `instance` position MUST remain last so that every grouping token
precedes the only unbounded-cardinality token; the suffix index, loop-id extraction, and rule `$entity.instance`
substitution MAY depend on that placement.

#### Scenario: the federation triple is a prefix

- **GIVEN** entity `acme.dep1.src.git.commit.a1`
- **WHEN** `SourcePrefix()` runs
- **THEN** it returns `acme.dep1.src`
- **AND** the test that verifies this is `TestPrefixLevelsAreNamed`

#### Scenario: a taxonomy across sources is a pattern, not a prefix

- **GIVEN** a caller wanting every `git` entity of deployment `acme.dep1` regardless of source
- **WHEN** it expresses the selector
- **THEN** the selector is the declaration pattern `acme.dep1.*.git.*.*` or the KV filter `acme.dep1.*.git.>`
- **AND** `ValidateEntityIDPrefix` rejects any attempt to express it as a prefix
- **AND** the test that verifies this is `TestTaxonomyAcrossSourcesIsPatternNotPrefix`

## ADDED Requirements

### Requirement: A framework family minted from another entity derives a collision-free instance

A framework builder that mints an entity from another entity MUST derive the instance segment as the lowercase
64-hex SHA-256 of a length-framed byte sequence over a versioned digest domain and that origin's full canonical
identifier, composed through `pkg/types.FrameworkIdentityFamily.DerivedEntityID(org, platform, digestDomain,
frames...)` under the deployment's own authority — the one home for framed-digest derivation. The agent-run family
(`chain.agent.execution`, 64-byte instance, digest domain `semstreams.agent.run.v1`) MUST be a member of the family
table, and `agentrun.Mint(ctx, mgr, org, platform, originEntityID)` MUST be its only minter. No exported builder MAY
compose a derived family's identity from a fragment of its origin; the corpus audit MUST report
`derived_family_composed` for any production Go format builder, constructor, or prefix constant whose positions 3–5
equal a derived family outside the family table's own file. A consumer of a derived identity MUST read it from where
the framework carried it (the run entity, `agent.run.entity-id`, `RunEntityID` on the wire, tool metadata) and MUST
NOT recompute it.

#### Scenario: two foreign origins sharing an instance token stay distinct

- **GIVEN** a deployment with authority `acme`/`dep1`
- **AND** two imported origins `peer1.x.agentic-loop.agent.execution.abc` and `peer2.y.agentic-loop.agent.execution.abc`
- **WHEN** the framework mints its run entity for each origin
- **THEN** the two local identities differ and each is `acme.dep1.chain.agent.execution.<64 hex>`
- **AND** neither mint returns the entity the other origin's mint produced
- **AND** the test that verifies this is `TestMint_TwoOriginsAtOneInstanceMintDistinctRuns`

#### Scenario: the derivation has one home

- **GIVEN** a production Go file composing `fmt.Sprintf("%s.%s.chain.agent.execution.%s", …)` outside
  `pkg/types/framework_identity_families.go`
- **WHEN** `task entity-id:audit` runs
- **THEN** it reports the occurrence with reason `derived_family_composed`
- **AND** the test that verifies this is `TestAuditFlagsDerivedFamilyComposedOutsideItsHome`

### Requirement: A deployment provisioned from a cloned template does not share its authority pair

The framework MUST NOT let two deployments provisioned from one configuration template silently mint under the same
`org.platform` pair. `platform.id` MUST receive a framework-minted entropy suffix (`-` followed by six lowercase hex
bytes from `crypto/rand`) on the deployment's first boot unless the configuration declares `platform.unique: true`,
in which case the operator owns uniqueness. The suffixed value is the deployment's `platform` position from that
boot on; the mechanics of minting, persisting and adopting it are specified by `component-runtime-config`. The
framework MUST NOT decide "already minted" by inspecting the value's grammar.

#### Scenario: two fresh boots from one template mint distinct authorities

- **GIVEN** two deployments whose configuration files are byte-identical copies of one template with `platform.id` `dep`
- **WHEN** each boots for the first time against its own NATS server
- **THEN** the `org.platform` pair each mints under differs from the other's and each `platform` position is `dep-` followed by six hex bytes
- **AND** each deployment's pair is stable across its own later restarts
- **AND** the test that verifies this is `TestFirstBootMintsDistinctSuffixesPerDeployment`

#### Scenario: an operator-unique identifier is not suffixed

- **GIVEN** a configuration declaring `platform.id` `field-ops-7` and `platform.unique: true`
- **WHEN** the deployment boots for the first time
- **THEN** its `platform` position is exactly `field-ops-7`
- **AND** the test that verifies this is `TestUniquePlatformIDIsNotSuffixed`

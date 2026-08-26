## MODIFIED Requirements

### Requirement: Every entity ID has one canonical six-segment ASCII form

An entity ID MUST contain exactly six non-empty dot-separated segments in
`org.platform.system.domain.type.instance` order. Each segment MUST begin with one ASCII alphanumeric byte and every
remaining byte MUST be ASCII alphanumeric, `_`, or `-`. The complete serialized key, including five dots, MUST be no
longer than 256 bytes. There MUST be no independent per-segment length maximum. The `instance` position MUST be the
sixth and last position.

Validation MUST inspect and preserve the exact input bytes. It MUST NOT trim, case-fold, Unicode-normalize, escape,
encode, replace, or otherwise rewrite identity. Unicode, whitespace, slash, control bytes, wildcard tokens, leading
`_`/`-`, empty segments, and any arity other than six MUST be invalid.

#### Scenario: the exact 256-byte boundary is accepted

- **GIVEN** a six-segment entity ID whose serialized key is exactly 256 ASCII bytes
- **AND** one segment is 246 bytes while each other segment is one byte
- **WHEN** canonical entity-ID validation runs
- **THEN** validation succeeds without rewriting the key
- **AND** parsing and serializing returns the exact original bytes

#### Scenario: the total bound is the only size bound

- **GIVEN** a syntactically valid six-segment entity ID whose serialized key is 257 bytes
- **WHEN** canonical entity-ID validation runs
- **THEN** validation rejects it because the complete key exceeds 256 bytes
- **AND** the failure does not claim an independent per-segment maximum

#### Scenario: a segment must start alphanumeric

- **GIVEN** a six-segment value with one segment beginning with `_` or `-`
- **WHEN** canonical entity-ID validation runs
- **THEN** it fails with a typed structural reason
- **AND** no normalized replacement is returned

#### Scenario: the typed struct serializes in the canonical order

- **GIVEN** `EntityID{Org: "acme", Platform: "dep1", System: "src", Domain: "git", Type: "commit", Instance: "a1"}`
- **WHEN** `Key()` and `ParseEntityID` run
- **THEN** the serialized key is `acme.dep1.src.git.commit.a1`
- **AND** parsing that key assigns each field from its named position, never from a raw index elsewhere
- **AND** the test that verifies this is `TestEntityIDKeyOrderIsSystemBeforeDomain`

## ADDED Requirements

### Requirement: Each entity-ID position has one defined meaning and one owner

Each position MUST carry exactly the meaning below and MUST be supplied only by its owner. `org` is the organization
namespace from `platform.org`. `platform` is the minting deployment authority: the composition root's `platform.id`,
carried to components as `deps.Platform`, and MUST NOT be taken from a payload, a constant, a product name, or a
firing entity. `system` is the source that produced the entity (subsystem, feed, repository, world, board, API, or
framework component) and MUST NOT be the producing product's name; the product is provenance carried by
`Triple.Source` and the envelope `source`. `domain` and `type` are a delegated taxonomy. `instance` is the
producer's leaf identifier. Every framework-derived family, including rule alerts and triggers, MUST carry the
deployment's own `org.platform`; a fixed framework literal in positions 1–2 is not a valid authority.

#### Scenario: a framework builder mints under the deployment's own authority

- **GIVEN** a deployment whose `deps.Platform` is `acme`/`dep1`
- **WHEN** a loop execution, chain execution, lesson, web observation, diagnosis, rule alert, or rule trigger entity is minted
- **THEN** positions 1–2 of the minted ID are `acme.dep1`
- **AND** position 3 names the minting framework component and position 4 a framework-reserved domain
- **AND** the test that verifies this is `agentic/entity_ids_semantics_test.go`

#### Scenario: a product name in the platform position is a corpus finding

- **GIVEN** a production builder whose platform position is a literal product name
- **WHEN** the entity-ID corpus audit runs
- **THEN** it reports the occurrence with reason `authority_literal`

### Requirement: Entity-domain authority is delegated on the predicate-namespace pattern

`pkg/types` MUST export an `EntityDomainAuthority` built from explicit `EntityDomainDelegation{Producer, Domain,
Type}` values, mirroring `vocabulary.PredicateAuthority`: a framework-reserved domain (`agent`, `ops`, `gateddag`,
`graph`) MUST pass for every producer; an unreserved domain MUST require a non-empty producer with an exact matching
`domain` or `domain.type` delegation; producer identity MUST come from the trusted composition boundary and MUST NOT
be inferred from `Triple.Source` or a payload type. Authorization MUST run at declaration surfaces (framework
builders, entity-ID pattern declarations, projection contracts, lifecycle workflows) and MUST NOT run on the
graph-ingest persistence hot path. A duplicate delegation of one domain by two producers in one composition MUST be
a composition rejection before binding. `system` and `instance` values MUST NOT be registered.

#### Scenario: a reserved domain passes for every producer

- **GIVEN** an authority with no delegations
- **WHEN** `Authorize("", "agent", "execution")` runs
- **THEN** it returns nil

#### Scenario: an undelegated domain is a coded rejection

- **GIVEN** an authority delegating only `git` to producer `semsource`
- **WHEN** `Authorize("semsource", "media", "video")` runs
- **THEN** it returns code `entity_id_authority_invalid` with reason `domain_undelegated`
- **AND** the test that verifies this is `TestEntityDomainAuthorityMirrorsPredicateAuthority`

### Requirement: Authority mismatch is a coded rejection distinct from structural rejection

`pkg/types` MUST export `ErrorCodeEntityIDAuthorityInvalid = "entity_id_authority_invalid"`, reasons
`EntityIDReasonForeignAuthority = "foreign_authority"`, `EntityIDReasonLocalAuthorityClaimed =
"local_authority_claimed"`, and `EntityIDReasonDomainUndelegated = "domain_undelegated"`, and the detail key
`EntityIDDetailLane = "lane"`. `ValidateEntityIDAuthority(candidate, local, importLane)` MUST return
`foreign_authority` when `importLane` is false and positions 1–2 differ from `local`, MUST return
`local_authority_claimed` when `importLane` is true and positions 1–2 equal `local`, and MUST return nil otherwise.
Details MUST contain only `reason`, `segment_index`, and `lane`; they MUST NOT echo any identity bytes. Structural
validation MUST run first; an authority reason MUST never mask a structural one.

#### Scenario: a foreign authority on a local lane is rejected without identity in details

- **GIVEN** local authority `acme`/`dep1` and candidate `acme.dep2.src.git.commit.a1` on a non-import lane
- **WHEN** authority validation runs
- **THEN** it returns code `entity_id_authority_invalid` with reason `foreign_authority` and `segment_index` 1
- **AND** no detail value contains a dot-joined identity
- **AND** the test that verifies this is `TestAuthorityRejectionIsCodedAndIdentityFree`

#### Scenario: a local claim on an import lane is rejected

- **GIVEN** the same local authority and candidate `acme.dep1.src.git.commit.a1` on an import lane
- **WHEN** authority validation runs
- **THEN** it returns reason `local_authority_claimed`

### Requirement: Prefix lengths have fixed meanings and the instance position is last

`pkg/types` MUST export the named prefix levels `DeploymentPrefix` (two positions), `SourcePrefix` (three),
`TaxonomyPrefix` (four), and `TypePrefix` (five) plus `PrefixLevel(n)`, and MUST NOT export a helper whose meaning
depends on a position order other than the canonical one. A query prefix of length n MUST mean exactly the level
named for n. Grouping by a non-prefix combination (a taxonomy across sources) MUST be expressed as an exact-arity
wildcard pattern or KV filter, never as a prefix. The `instance` position MUST remain last so that every grouping
token precedes the only unbounded-cardinality token; the suffix index, loop-id extraction, and rule `$entity.instance`
substitution MAY depend on that placement.

#### Scenario: the federation triple is a prefix

- **GIVEN** entity `acme.dep1.src.git.commit.a1`
- **WHEN** `SourcePrefix()` runs
- **THEN** it returns `acme.dep1.src`
- **AND** `PrefixLevel(3)` returns the same value
- **AND** the test that verifies this is `TestPrefixLevelsAreNamed`

#### Scenario: a taxonomy across sources is a pattern, not a prefix

- **GIVEN** a caller wanting every `git` entity of deployment `acme.dep1` regardless of source
- **WHEN** it expresses the selector
- **THEN** the selector is the declaration pattern `acme.dep1.*.git.*.*` or the KV filter `acme.dep1.*.git.>`
- **AND** `ValidateEntityIDPrefix` rejects any attempt to express it as a prefix

### Requirement: Segment semantics are enforced by the entity-ID corpus audit

The entity-ID corpus audit MUST report, in addition to lexical findings, `authority_literal` for any literal,
non-wildcard, non-template value in positions 1–2 of a production builder or declaration pattern, and
`domain_unregistered` for any literal position-4 value in production Go that is outside the framework-reserved set
and not a registered delegation. The tracked corpus MUST have zero unclassified findings, and the audit MUST run in
the CI lint job. The container padding tokens `group`, `container`, and `level` MUST be exported as reserved
instance tokens; a production instance value equal to one of them MUST be a finding.

#### Scenario: a literal authority in a builder is a finding

- **GIVEN** a production Go file constructing `fmt.Sprintf("semstreams.framework.%s.%s.%s.%s", …)`
- **WHEN** the audit runs
- **THEN** it reports the occurrence with reason `authority_literal`
- **AND** the CI lint job exits nonzero

#### Scenario: the corpus is clean at the landing head

- **GIVEN** the tracked source at the landing head
- **WHEN** `task entity-id:audit` runs
- **THEN** it reports zero invalid or unclassified candidates

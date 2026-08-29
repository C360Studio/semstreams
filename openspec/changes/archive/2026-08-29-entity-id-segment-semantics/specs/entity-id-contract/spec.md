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
- **AND** the test that verifies this is `TestAuditFlagsAuthorityLiteral`

### Requirement: Entity domains are declared by their producer and read only by the corpus audit

`pkg/types` MUST export `EntityDomainDelegation{Producer, Domain, Type}` as the declaration a product makes of the
entity domains it mints under, and MUST NOT ship a runtime or composition-time authorization policy over it (owner
ruling 2026-08-28, superseding O-5). The framework MUST reserve the domains `agent`, `ops`, and `graph` — the
gated-DAG family is re-slotted under `agent` — and MUST expose that set and the reserved instance tokens as
predicates. Producer identity MUST come from the trusted composition boundary and MUST NOT be inferred from
`Triple.Source` or a payload type. Two or more producers MUST be permitted to declare one domain: the taxonomy
vocabulary is shared, `system` at position 3 keeps the entity IDs distinct, and ADR-099 level 0 is source x taxonomy
so the derived communities stay distinct. An overlap MUST NOT be a boot refusal, a runtime log line, or a
composition finding — a token two products mean different things by is a vocabulary question, not one the framework
answers. The declarations MUST be retained as composite literals in production Go: they are the registered set the
corpus audit's `domain_unregistered` rule reads, and no other consumer exists. `system` and `instance` values MUST
NOT be registered.

#### Scenario: the reserved domain set is closed

- **GIVEN** the framework-reserved domain set, reachable outside its package only through `IsFrameworkEntityDomain`
- **WHEN** it is compared against `{agent, ops, graph}`
- **THEN** it is exactly that set, and `gateddag` is NOT reserved because the gated-DAG family re-slots under `agent`
- **AND** the test that verifies this is `TestFrameworkEntityDomainsIsTheClosedReservedSet`

#### Scenario: the audit's registered set is the declarations themselves

- **GIVEN** a production builder minting position 4 `environmental`, declared by an `EntityDomainDelegation` literal
- **WHEN** that literal is removed and the entity-ID corpus audit runs
- **THEN** the builder is reported with reason `domain_unregistered`
- **AND** restoring the literal returns the corpus to zero findings
- **AND** the test that verifies this is `TestAuditFlagsUnregisteredDomain`

#### Scenario: two producers may declare one domain with nothing reporting it

- **GIVEN** a declaration of `web` by producer `semsource` and a declaration of `web` by producer `semdragon`
- **WHEN** the composition is validated and the corpus audit runs
- **THEN** neither reports the overlap, and `web` is registered for both
- **AND** the test that verifies this is `TestEntityDomainDelegationIsADeclarationNotAPolicy`

### Requirement: Authority mismatch is a coded rejection distinct from structural rejection

`pkg/types` MUST export `ErrorCodeEntityIDAuthorityInvalid = "entity_id_authority_invalid"`, reasons
`EntityIDReasonForeignAuthority = "foreign_authority"` and `EntityIDReasonLocalAuthorityClaimed =
"local_authority_claimed"`, and the detail key
`EntityIDDetailLane = "lane"`. `ValidateEntityIDAuthority(candidate, org, platform string, importLane bool)` MUST return
`foreign_authority` when `importLane` is false and positions 1–2 differ from `org`/`platform`, MUST return
`local_authority_claimed` when `importLane` is true and positions 1–2 equal them, and MUST return nil otherwise. It
takes strings, not `types.PlatformMeta`, because that type lives in the root `types` package.
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
- **AND** the test that verifies this is `TestAuthorityRejectionLocalClaimOnImportLane`

### Requirement: Prefix lengths have fixed meanings and the instance position is last

`pkg/types` MUST export the named prefix levels `DeploymentPrefix` (two positions), `SourcePrefix` (three),
`TaxonomyPrefix` (four), and `TypePrefix` (five), and MUST NOT export a helper whose meaning
depends on a position order other than the canonical one. A query prefix of length n MUST mean exactly the level
named for n. Grouping by a non-prefix combination (a taxonomy across sources) MUST be expressed as an exact-arity
wildcard pattern or KV filter, never as a prefix. The `instance` position MUST remain last so that every grouping
token precedes the only unbounded-cardinality token; the suffix index, loop-id extraction, and rule `$entity.instance`
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

### Requirement: The authority pair is bounded at configuration load

Configuration load MUST reject a `platform.org`/`platform.id` pair whose combined length exceeds the budget derived
from the longest fixed-suffix framework family — `256 − 86 = 170` bytes for `len(org) + len(platform)` while the rule
trigger family (`rules.graph.trigger.` + 64 hex + two separators) is the longest — naming the binding family in the
error. The budget MUST be derived from the framework's own family table, never configured by the operator. Framework
constructors MUST keep fail-closed canonical validation as the second layer. This amends ADR-076 decision 2: framework
identities are bounded, not fixed-length.

#### Scenario: an oversized authority pair does not boot

- **GIVEN** a configuration whose `platform.org` and `platform.id` total 171 bytes
- **WHEN** configuration load runs
- **THEN** it returns an error naming the trigger family and the 170-byte budget
- **AND** the test that verifies this is `TestConfigRejectsOversizedAuthorityPair`

### Requirement: A component that carried an authority config key refuses a configuration still declaring it

A component that once accepted the deployment authority as its own configuration MUST refuse to load a
configuration that still declares the retired key, with a coded error naming the key, the decision that retired it,
and the replacement. The refusal MUST fire on every entry path that reads the component's raw configuration,
including the offline port declaration and the boot-time factory. Silently ignoring the key is prohibited:
`encoding/json` drops a key with no matching struct field, so an operator who carried the key forward would see no
error while every identity the component mints moved to a different authority.

#### Scenario: a component configuration carrying a retired authority key does not load

- **GIVEN** an `iot_sensor`, `document_processor`, or `weather_station` component configuration that declares
  `org_id` or `platform`
- **WHEN** either the port declaration or the component factory reads it
- **THEN** the load fails with an error naming the field, ADR-102 d2, and `platform.org` / `platform.id` as the
  replacement
- **AND** the test that verifies this is `TestRetiredAuthorityKeysAreRefused` in each of the three packages

#### Scenario: an example processor mints from the composition root and nothing else

- **GIVEN** a deployment whose `deps.Platform` is `c360`/`semstreams-e2e-structural`
- **WHEN** an example processor built by its own factory transforms an input record
- **THEN** positions 1–2 of every entity it mints — the reading, its zone, and every document family — are
  `c360.semstreams-e2e-structural`
- **AND** the minted identity survives a JSON round-trip, because the payload carries the minted value rather than
  the authority it was minted from
- **AND** the test that verifies this is `TestComponentMintsUnderDeploymentAuthority` in each of the three packages

### Requirement: Segment semantics are enforced by the entity-ID corpus audit

The entity-ID corpus audit MUST report, in addition to lexical findings, `authority_literal` for any literal,
non-wildcard, non-template value in positions 1–2 of a production builder, declaration pattern, or prefix constant,
and `domain_unregistered` for any literal position-4 value in production Go that is outside the framework-reserved
set and not a registered delegation. To see builders the audit MUST add two surfaces it lacks today:
`go-format-prefix` (a `fmt.Sprintf` format string whose dot-separated tokens are read as positions, with `%s` as a
template position) and `go-dotted-constant` (a string constant of two or more dotted tokens ending in `.`). The tracked corpus MUST have zero unclassified findings, and the audit MUST run in
the CI lint job. The container padding tokens `group`, `container`, and `level` MUST be exported as reserved
instance tokens; a production instance value equal to one of them MUST be a finding.

#### Scenario: a literal authority in a builder is a finding

- **GIVEN** a production Go file constructing `fmt.Sprintf("semstreams.framework.%s.%s.%s.%s", …)` and another
  declaring `const alertEntityPrefix = "semstreams.framework.graph.rules.alert."`
- **WHEN** the audit runs with the `go-format-prefix` and `go-dotted-constant` surfaces
- **THEN** it reports both occurrences with reason `authority_literal`
- **AND** the CI lint job exits nonzero
- **AND** the test that verifies this is `TestAuditFlagsFormatPrefixAuthorityLiteral`

#### Scenario: a literal minted identity is a finding

- **GIVEN** a production Go file constructing `EntityID{Org: "acme", Platform: "fixed-product", …}` and another whose
  `EntityID()` method returns a six-segment constant, both under the framework-reserved `graph` domain
- **WHEN** the audit runs
- **THEN** it reports both with reason `authority_literal`
- **AND** a triple subject or typed reference naming another deployment's authority is NOT reported
- **AND** the constructor surface resolves only when ALL SIX fields are statically resolvable
  (`entityIDConstructorValue`), so a partial-literal mint such as `EntityID{Org: deps.Org, Platform: "semsource", …}`
  yields no candidate and is outside this rule — a stated limit of the extraction, not of the rule
- **AND** the test that verifies this is `TestAuditFlagsAuthorityLiteralInAMintingLiteral`

#### Scenario: the corpus is clean at the landing head

- **GIVEN** the tracked source at the landing head
- **WHEN** `task entity-id:audit` runs
- **THEN** it reports zero invalid or unclassified candidates
- **AND** the recorded result is `tasks.md` 7.1

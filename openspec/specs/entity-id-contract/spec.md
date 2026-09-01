# entity-id-contract Specification

## Purpose

`entity-id-contract` governs **entity identity**: the canonical six-segment ASCII form
(`org.platform.system.domain.type.instance`), who is allowed to parse and validate it, and what
happens when something fails to conform. `pkg/types` is the **sole** parser and validator authority
— that single-authority rule is the point, because identity that each caller re-derives is identity
that drifts. Enforcement is unconditional at graph boundaries, with no permissive mode and no
compatibility alias.

It also owns the surfaces that shade into identity but are not it: wildcard **patterns** are
separate exact-arity values rather than loose globs, query **prefixes** are their own bounded
language, rejection carries an executable stable error contract rather than an ad-hoc string, and
ObjectStore validates identity before doing entity-derived object I/O.

**What it does NOT cover.** Predicate grammar belongs to the predicate contract; what a triple means
and how it merges belongs to `graph-ingest`; index key encoding belongs to `nats-kv-keys`. This
capability answers one question — is this a legal entity ID, and who says so.
## Requirements
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

### Requirement: `pkg/types` is the sole entity-ID parser and validator authority

`pkg/types` MUST own the coded `ValidateEntityID(string) error` and
`ParseEntityID(string) (EntityID, error)` surfaces, boolean `IsValidEntityID`, serialized-size constant, segment rules,
and structured `EntityID.IsValid` behavior. The coded surfaces MUST enforce the complete canonical contract before
returning success or typed fields. Boolean `pkg/types.IsValidEntityID`, `message.IsValidEntityID`, and
`EntityID.IsValid` MUST return false for every coded validation error with exact parity; their boolean signatures MUST
NOT claim to return a coded error.

Existing `message` parser and validator entry points MUST delegate to `pkg/types` and MUST NOT retain an independent
regex, alphabet, arity check, or size limit. Graph-ingest MUST delete its private entity-ID regex and 255-byte limit
and MUST delegate authoritative validation to the shared `pkg/types` contract.

#### Scenario: every public entry point agrees at the byte boundary

- **GIVEN** canonical and malformed fixtures at 255, 256, and 257 serialized bytes
- **WHEN** `pkg/types`, `message`, and graph-ingest validation entry points inspect them
- **THEN** every entry point returns the same validity result
- **AND** 256 bytes is accepted while 257 bytes is rejected

#### Scenario: a hand-constructed typed ID cannot bypass syntax

- **GIVEN** an `EntityID` struct with six non-empty fields but one field begins with `-`
- **WHEN** `EntityID.IsValid` runs
- **THEN** it returns false through the canonical serialized-key validator

### Requirement: Entity-ID rejection has an executable stable error contract

`pkg/types` coded literal surfaces MUST export and return this stable serialized contract:

```text
ErrorCodeEntityIDInvalid        = "entity_id_invalid"
EntityIDReasonEmpty             = "empty"
EntityIDReasonBytes             = "bytes"
EntityIDReasonArity             = "arity"
EntityIDReasonEmptySegment      = "empty_segment"
EntityIDReasonFirstByte         = "first_byte"
EntityIDReasonAlphabet          = "alphabet"
EntityIDDetailReason            = "reason"
EntityIDDetailMeasuredBytes     = "measured_bytes"
EntityIDDetailAllowedBytes      = "allowed_bytes"
EntityIDDetailMeasuredParts     = "measured_parts"
EntityIDDetailAllowedParts      = "allowed_parts"
EntityIDDetailSegmentIndex      = "segment_index"
```

`ValidateEntityID` and `ParseEntityID` MUST apply fault precedence as empty input, byte limit, arity, empty segment,
invalid first byte, then invalid segment alphabet, reporting the first left-to-right segment fault within a reason
class. Details MUST contain only non-sensitive measurements and limits; they MUST NOT echo the full rejected identity.
At an operational boundary, one rejected operation MUST record exactly one rejection metric with stable bounded
lane/reason labels. Pure parser/validator helpers MUST remain side-effect free. Rejection MUST NOT increment a
success, business, storage, publication, or other operation-success metric and MUST NOT put rejected identity bytes in
metric labels.

#### Scenario: a multi-fault input has one deterministic classification

- **GIVEN** an entity-ID candidate that exceeds 256 bytes and also has the wrong arity and invalid segment bytes
- **WHEN** canonical validation runs through any public delegator
- **THEN** it returns the exported invalid-entity code and byte-limit reason
- **AND** details use exported measured/allowed-byte keys without including the rejected identity

#### Scenario: segment fault precedence is stable

- **GIVEN** a six-position, in-budget candidate with an empty segment before a later invalid-first-byte segment
- **WHEN** canonical validation runs
- **THEN** it returns the exported empty-segment reason and first failing segment index
- **AND** callers need not parse error prose to branch on the failure

### Requirement: Entity-ID patterns are separate exact-arity wildcard values

An entity-ID pattern MUST contain exactly six non-empty dot-separated tokens and MUST be no longer than 256 bytes.
Each token MUST be either the complete token `*` or one canonical literal entity-ID segment. A pattern MUST NOT accept
`>`, embedded or partial wildcards, empty tokens, Unicode, or a literal token beginning with `_` or `-`.

Pattern validation MUST use the distinct coded `ValidateEntityIDPattern(string) error` API with
`ErrorCodeEntityIDPatternInvalid = "entity_id_pattern_invalid"`. It MUST reuse applicable literal reason/detail
constants without requiring a parallel pattern-only reason taxonomy. A pattern containing `*` MUST NOT be a valid
entity ID or persisted as an identity. A pattern containing six literal tokens MUST be valid if and only if the same
bytes are a canonical entity ID.

#### Scenario: a mixed literal and wildcard pattern is valid only as a pattern

- **GIVEN** the value `acme.*.robotics.gcs.drone.*`
- **WHEN** pattern and literal validation run
- **THEN** pattern validation succeeds with all six token positions preserved
- **AND** literal entity-ID validation rejects the value

#### Scenario: general NATS wildcard syntax is not an entity pattern

- **GIVEN** a six-position-looking value containing `>`, `foo*`, or `*bar`
- **WHEN** entity-ID-pattern validation runs
- **THEN** validation rejects it before registration, matching, lister creation, or watcher creation

### Requirement: Entity-ID query prefixes are a distinct bounded language

An entity-ID query prefix MUST contain one through six dot-separated canonical literal segments and MUST be no longer
than 256 bytes. It MUST reject `*`, `>`, partial wildcards, Unicode, empty or trailing positions, and invalid literal
segments. Empty input MUST mean match-all only on a public surface whose existing contract explicitly promises that
behavior; a required scoped input MUST reject empty rather than silently widen to a global query.

Non-empty prefix validation MUST use the distinct coded `ValidateEntityIDPrefix(string) error` API with
`ErrorCodeEntityIDPrefixInvalid = "entity_id_prefix_invalid"`. It MUST reuse applicable literal reason/detail constants
without requiring prefix-only exported reasons and MUST run before a prefix becomes a KV filter, embedding/fusion
scope, graph-query resolution input, or gateway query operation. A surface that promises empty means match-all MUST
handle empty before calling this non-empty validator.

#### Scenario: a partial canonical prefix remains a query selector

- **GIVEN** the value `acme.ops.robotics`
- **WHEN** literal, declaration-pattern, and query-prefix validation run
- **THEN** query-prefix validation accepts its three canonical literal positions
- **AND** literal and six-position declaration-pattern validation reject it

#### Scenario: empty is match-all only where promised

- **GIVEN** one graph-query surface that documents empty prefix as match-all and one required scoped input
- **WHEN** both receive an empty prefix
- **THEN** the match-all surface preserves its existing global-query behavior
- **AND** the required scoped input rejects empty before query or NATS I/O

#### Scenario: an impossible prefix never becomes a filter

- **GIVEN** a prefix with a wildcard, Unicode segment, trailing dot, seventh segment, or 257 serialized bytes
- **WHEN** graph prefix, embedding/fusion scope, or gateway validation runs
- **THEN** it returns a typed non-retryable structural error
- **AND** no filter, watcher, lister, or downstream query request is created

### Requirement: Canonical entity-ID enforcement is unconditional at graph boundaries

Every framework graph boundary MUST apply the canonical literal contract to literal producers, Graphable subjects,
classified entity references, mutation requests, final ENTITY_STATES candidates, authoritative replay decoders,
derived-index key builders, schemas, tools, and reference configurations. Every lifecycle, projection,
rule-watch, gateway, and other entity-pattern declaration MUST use the canonical pattern contract before activation.

The authoritative final-candidate marshal seam and every independent authoritative replay decoder MUST validate the
`EntityState.ID` and every persisted `Triple.Subject` as canonical explicit entity IDs. The Graphable fact-arrival lane
MAY replace an empty projected triple subject with the envelope `EntityState.ID` before final-candidate validation.
That fill is projection semantics, not identity normalization: it MUST copy the exact envelope ID and MUST NOT alter a
non-empty subject. Mutation requests, direct persistence callers, and replay decoders MUST NOT receive this fill, and
marshal or replay MUST reject every remaining empty or malformed subject.

The repository MUST expose the stable marker `message.EntityReferenceDatatype = "@id"`. A string object that already
has canonical entity-ID shape MUST remain structurally recognized as an entity relationship for current behavior.
When `Triple.Datatype` equals `message.EntityReferenceDatatype`, its object MUST be a string and MUST pass canonical
entity-ID validation. An explicitly marked non-string or malformed string MUST fail the complete candidate. Reference
classification MUST NOT depend on the global vocabulary registry or on dot-count/six-dot guessing. Other datatype
values retain their literal datatype semantics.

Invalid new input MUST fail with a typed non-retryable structural error before graph or NATS I/O. SemStreams MUST NOT
expose a permissive mode, legacy validator, normalization shim, alias table, or dual literal/pattern interpretation.

#### Scenario: a malformed Graphable cannot partially persist

- **GIVEN** a Graphable whose complete candidate contains an invalid entity subject or classified entity reference
- **WHEN** graph-ingest reaches the authoritative final-candidate persistence seam
- **THEN** the candidate is rejected before ENTITY_STATES or required projection I/O
- **AND** no partial graph mutation is visible

#### Scenario: Graphable omission is filled before the authoritative seam

- **GIVEN** a Graphable with a canonical envelope entity ID and one projected triple whose subject is empty
- **WHEN** the fact-arrival projection is normalized before final-candidate validation
- **THEN** the triple subject is filled with the exact envelope entity-ID bytes
- **AND** the authoritative candidate contains an explicit canonical subject without rewriting any non-empty identity

#### Scenario: mutation and replay do not inherit fact-lane subject fill

- **GIVEN** a mutation, direct persistence candidate, or stored replay record with an empty or malformed triple subject
- **WHEN** authoritative marshal or replay validation runs
- **THEN** the candidate is rejected through the canonical typed contract
- **AND** no envelope-derived subject is supplied and no state or projection I/O follows

#### Scenario: an explicitly marked reference cannot degrade into a literal

- **GIVEN** a triple whose datatype is `message.EntityReferenceDatatype` with value `"@id"`
- **WHEN** its object is non-string or a malformed entity-ID string
- **THEN** complete-candidate validation rejects the triple
- **AND** it is not reclassified as a literal by vocabulary lookup or dot-count guessing

#### Scenario: existing canonical string references remain relationships

- **GIVEN** an unmarked string object whose exact bytes are a canonical entity ID
- **WHEN** relationship classification runs
- **THEN** it remains structurally recognized as an entity reference
- **AND** existing graph traversal and referential-integrity behavior is preserved

#### Scenario: configuration cannot disable the contract

- **WHEN** a deployment loads graph-ingest, lifecycle, projection, or rule configuration
- **THEN** no option exists to accept noncanonical entity IDs or patterns

### Requirement: ObjectStore validates entity identity before entity-derived object I/O

ObjectStore `StoreContent` MUST validate `ContentStorable.EntityID()` through the canonical literal contract before
generating or writing any binary or content object name. Invalid identity MUST return a typed non-retryable structural
error with no binary, content-envelope, event, success/business/storage metric, callback, or stored-message side
effect. Its operational boundary MUST record exactly one bounded lane/reason rejection metric without identity bytes
in labels. This requirement MUST NOT select ObjectStore retention, reachability, reference-counting, or reclamation
policy.

#### Scenario: invalid content identity leaves no orphan

- **GIVEN** a `ContentStorable` whose entity ID is malformed or 257 bytes
- **WHEN** ObjectStore processes it before graph-ingest
- **THEN** `StoreContent` rejects it through the canonical error contract
- **AND** no binary or content object name is generated or written

#### Scenario: ObjectStore validation does not expand lifecycle policy

- **GIVEN** canonical content has been stored successfully
- **WHEN** entity retention or reference reachability is evaluated
- **THEN** this contract makes no reclamation or ownership decision
- **AND** the separately governed ObjectStore lifecycle remains authoritative

### Requirement: Entity-ID test fixtures are canonical or exactly classified negatives

The tracked local entity-ID corpus MUST include production sources, every `*_test.go` file, and every structured
artifact beneath `testdata`. Positive runtime fixtures SHOULD use the grammar-only `internal/semantictest` entity-ID
builder. The builder MUST accept all six semantic positions explicitly, MUST join and validate them through
`pkg/types` without normalization or defaults, and MUST return only the validated string. It MUST NOT construct
`graph.EntityState`, triples, Graphable values, or other behavior-bearing graph fixtures. Production Go files MUST NOT
import this test helper. Grammar-authority tests and literal constants MAY remain raw source values, but MUST remain in
the checked corpus.

The blocking corpus MUST consist of concrete typed or statically identifiable entity-ID literals, patterns, prefixes,
known configuration fields, triple subjects, typed `@id` references, and canonical semantic fixture calls. A
repository-wide name-only inventory of generic KV methods, match-named functions, string builders, or `strings.Split`
calls MUST NOT be treated as proof of contract coverage. The audit MAY emit the complete corpus as diagnostic JSON,
but the release gate MUST run against current tracked source and MUST NOT rely on a checked-in generated corpus dump.

Every intentional invalid fixture MUST be classified at one exact occurrence with its contract kind, exact value, and
authoritative stable reason. A commentless structured fixture MUST remain canonical positive data; an intentional
negative MUST move to a comment-capable native rejection test rather than a separate classification manifest.
File-wide or directory-wide invalid allowances MUST NOT satisfy the corpus. Missing, stale, duplicate, unmatched,
broad, or reason-mismatched classifications MUST fail, and every classification MUST resolve to exactly one candidate.

An empty value that has explicitly documented non-entity semantics, such as a public match-all prefix or an
unavailable optional projection, MUST use a distinct exact intentional-sentinel classification. Sentinel
classification MUST NOT make empty entity IDs valid and MUST NOT apply to an entire field, file, or value family.

A statically visible pre-substitution expression in an entity-ID position MUST use a distinct exact
intentional-template classification. The classification MUST bind one source occurrence and authoritative reason,
MUST identify actual substitution syntax, and MUST NOT exempt the resolved runtime value from canonical validation.

#### Scenario: the shared helper preserves exact invalid input

- **GIVEN** explicit entity-ID positions containing a byte that violates the canonical grammar
- **WHEN** the test fixture builder joins and validates them
- **THEN** it fails through the `pkg/types` authority
- **AND** it does not trim, normalize, replace, default, or return a repaired identity

#### Scenario: production code cannot depend on semantic test fixtures

- **GIVEN** a non-test Go file imports `internal/semantictest`
- **WHEN** repository contract checks run
- **THEN** the check fails and identifies the production import
- **AND** moving graph construction into the test helper is not accepted as a fix

#### Scenario: an exact intentional negative is fail-closed evidence

- **GIVEN** one malformed fixture occurrence classified with its exact value and authoritative reason
- **WHEN** the entity-ID corpus audit resolves the classification
- **THEN** it accepts the exception only when exactly one candidate matches and validation returns that reason
- **AND** a missing, stale, duplicate, broad, unmatched, or wrong-reason classification fails the audit

### Requirement: The pre-v1 beta cutover is a clean owned-source break

The breaking stable release MUST announce the exact entity-ID contract change and update every in-repo source,
schema, tool, configuration, fixture, and exact-query expectation to zero violations. Every downstream adoption MUST
start on newly provisioned NATS storage and MUST rerun affected framework and product E2E while readiness remains
fail-closed through initial replay.

This change MUST NOT require or provide persisted-state export, preservation, old-state audit, destructive release
wipe or reseed, online or in-place migration, compatibility readers, alias or rename ledgers, permissive dual
contracts, or rollback to beta state. Downstream adoption and product proof occur after publication and MUST NOT block
local framework graph-index work after its named current-layout prerequisites pass. Discovery of retained deployed
state MUST stop only that adoption and require a separate owner-reviewed migration or recovery design.

Malformed current writes or entity data injected directly into NATS MUST still fail through the canonical typed
contract before state or derived output. This fail-closed behavior and scoped typed poison recovery MUST NOT be
presented as support for upgrading old persisted state.

#### Scenario: the owned reference fleet cuts over from clean state

- **GIVEN** every owned source, configuration, schema, tool, fixture, and expected query is canonical
- **AND** the downstream has newly provisioned NATS storage
- **WHEN** the stable release starts and replays canonical sources
- **THEN** every newly persisted identity satisfies the canonical contract
- **AND** readiness and affected product E2E pass without reading or translating beta state

#### Scenario: Retained state blocks only the affected adoption

- **GIVEN** a downstream intends to adopt the stable release
- **WHEN** retained deployed NATS state is discovered
- **THEN** that adoption stops
- **AND** a separate owner-reviewed migration or recovery design is required
- **AND** the framework does not activate a compatibility, preservation, wipe, reseed, or rollback path

#### Scenario: directly injected malformed current data fails closed

- **GIVEN** a malformed entity record is written directly to an authoritative NATS input after fresh adoption
- **WHEN** an authoritative decoder observes it
- **THEN** the decoder returns the canonical typed structural error before state or projection I/O
- **AND** no compatibility reader, sanitizer, or partial derived result exposes the malformed identity

### Requirement: The entity-ID bound gates graph-index fixed-arity activation

Graph-index MUST treat the canonical maximum as `E = 256` when proving complete current-layout keys and filters
against the shared 1,024-byte NATS KV contract. The maximum INCOMING layout MUST be proven as
`2E + 390 = 902` bytes and 13 tokens. Maximum keys and exact-position filters for every affected layout MUST pass the
shared validators and pinned real-NATS conformance before fixed-arity owner reconciliation activates.

This dependency MUST NOT authorize entity-ID encoding, predicate-layout selection, or graph-index activation before
its separate correctness, performance, readiness, and ADR gates pass.

Graph-index framework activation MUST depend on the completed local entity-ID contract/API, local zero-violation
source corpus, ObjectStore zero-I/O, newly provisioned NATS storage, cold-start/readiness proof, key/filter proof, and
breaking E2E evidence. It MUST NOT depend on this change being archived and MUST NOT add persistence migration or
legacy compatibility. Retained deployed state MUST stop adoption for separate owner review.

#### Scenario: the worst current key fits the shared storage contract

- **GIVEN** canonical source and target entity IDs of 256 bytes each
- **AND** the maximum current predicate token contribution used by INCOMING
- **WHEN** graph-index constructs and validates the complete INCOMING key
- **THEN** the key is 902 bytes and 13 tokens
- **AND** the shared NATS key validator accepts it below the 1,024-byte and 64-token limits

#### Scenario: arithmetic does not bypass real-NATS proof

- **GIVEN** the 902-byte calculation passes unit validation
- **WHEN** graph-index fixed-arity activation is evaluated
- **THEN** activation remains blocked until maximum key/filter match sets pass pinned real-NATS conformance
- **AND** the dependent graph-index correctness, performance, readiness, fresh-start, and ADR gates also pass

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

Configuration load MUST reject a `platform.org`/`platform.id` pair that cannot carry the framework-minted entropy
suffix: `len(org) + len(platform) + 7` MUST NOT exceed the budget derived from the longest fixed-suffix framework
family — `256 − 86 = 170` bytes while the rule trigger family (`rules.graph.trigger.` + 64 hex + two separators) is
the longest — naming the binding family in the error. The seven reserved bytes are the suffix
`component-runtime-config` mints onto `platform.id` (`-` plus six hex bytes); reserving them at load is what stops a
pair that fits only unsuffixed from being durably recorded and then refused forever, which ADR-102 decision 7 makes
unrepairable. A declared pair may therefore be at most 163 bytes.

The reserve MUST apply only where a pair is DECLARED, and a configuration MUST declare the stem — never a minted
identifier — so that one field carries one kind of value. An effective pair — a minted identifier, an adopted
identity record's, or the running configuration's — already carries whatever suffix it will ever carry and MUST be
bounded at the full 170-byte budget; reserving the same seven bytes against it as well would refuse, after Start, a
declaration that had already passed load. Every declaration boundary MUST apply the same 163-byte bound and every effective-pair
boundary the same 170-byte bound, so no path can admit a pair another path rejects. The budget MUST be derived from
the framework's own family table, never configured by the operator.
Framework constructors MUST keep fail-closed canonical validation as the second layer. This amends ADR-076 decision
2: framework identities are bounded, not fixed-length.

#### Scenario: an oversized authority pair does not boot

- **GIVEN** a configuration whose `platform.org` and `platform.id` total 164 bytes
- **WHEN** configuration load runs
- **THEN** it returns an error naming the trigger family, the 170-byte budget, and the seven bytes reserved for the
  minted suffix
- **AND** the test that verifies this is `TestConfigRejectsOversizedAuthorityPair`

#### Scenario: a pair that fits only unsuffixed is refused before anything is minted

- **GIVEN** a configuration whose `platform.org` and `platform.id` total 165 bytes, which would fit the 170-byte
  budget unsuffixed but not once seven bytes are minted onto it
- **WHEN** configuration load runs
- **THEN** the load fails and no identity record is created
- **AND** the test that verifies this is `TestConfigRejectsPairThatOnlyFitsUnsuffixed`

#### Scenario: a pair at the declarable budget boots

- **GIVEN** a configuration whose `platform.org` and `platform.id` total exactly 163 bytes
- **WHEN** the deployment loads that configuration and starts against an empty bucket
- **THEN** the load succeeds, the entropy suffix is minted, the effective pair is 170 bytes, and Start succeeds
- **AND** the tests that verify this are `TestMaximumDeclarablePairMintsAndStarts` and
  `TestEffectivePairIsBoundedWithoutTheDeclarationReserve`

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

### Requirement: A deployment provisioned from a cloned template does not share its authority pair

The framework MUST NOT let two deployments provisioned from one configuration template silently mint under the same
`org.platform` pair. `platform.id` MUST receive a framework-minted entropy suffix — `-` followed by six lowercase hex
bytes from `crypto/rand` — on the deployment's genuine first boot; the suffixed value is the deployment's `platform`
position from that boot on, and the mechanics of minting, persisting and adopting it are specified by
`component-runtime-config`. The framework MUST NOT decide "already minted" by inspecting the value's grammar, and no
configuration key, environment variable, or other value carried inside the cloned document MAY disable the mint: an
operator who owns global uniqueness declares it by pre-creating the deployment's identity record, which is
per-deployment by construction and cannot be cloned through a template.

#### Scenario: two fresh boots from one template mint distinct authorities

- **GIVEN** two deployments whose configuration files are byte-identical copies of one template with `platform.id` `dep`
- **WHEN** each boots for the first time against its own NATS server
- **THEN** the `org.platform` pair each mints under differs from the other's and each `platform` position is `dep-` followed by six hex bytes
- **AND** each deployment's pair is stable across its own later restarts
- **AND** the test that verifies this is `TestFirstBootMintsDistinctSuffixesPerDeployment`

#### Scenario: an operator-provisioned identity record is adopted unsuffixed

- **GIVEN** a configuration declaring `platform.id` `field-ops-7`
- **AND** an operator who created `semstreams_config/platform_identity` as `{"org":"acme","stem":"field-ops-7","id":"field-ops-7"}` before the deployment's first boot
- **WHEN** the deployment boots
- **THEN** its effective `platform` position is exactly `field-ops-7` and no suffix is minted
- **AND** the test that verifies this is `TestPreCreatedIdentityRecordIsAdoptedUnsuffixed`

### Requirement: A loop instance token is a framework-minted UUID

Every loop-execution instance token — dispatch conversations, rule-spawned loops, subagent loops, and
research-pipeline loops alike — MUST be minted by the framework as a version 4 UUID and carried in canonical RFC
4122 text form: 36 bytes, lowercase hexadecimal, hyphenated. No component, config, client, or tool call MAY author
a loop instance token, and no adopter-facing knob MAY configure or relax the contract; the validation predicate
MUST live module-internal with no exported surface.

**Enforcement is FORM, not provenance — and the difference is load-bearing.** The framework validates that a
supplied token is a canonical UUID. It does not, and with a form predicate cannot, detect who minted it: a client
that authors a fresh canonical UUID and supplies it as `reply_to` is ACCEPTED. "Author no token" is therefore the
contract asked of adopters, not a property any seam verifies. Two consequences a reader MUST NOT infer away.
First, possession of a loop token confers control of that loop to any holder, so a multi-tenant deployment MUST
NOT rely on loop tokens for isolation until attach-seam authorization lands (#1227) — the gap is authorization at
the seam that ATTACHES to a loop, not provenance at the seam that MINTS one, and perfect mint-provenance would
close none of it. Second, the backstop against adopting a token this deployment did not mint is `agentrun.Mint`'s
origin-entity-ID mismatch refusal, not the form predicate.

Exactly four seams enforce the form refusal, each with a classified invalid error — a declared refusal, counted
where an intake counter exists, never a silent skip or truncated fallback:

- `TaskMessage.Validate` MUST refuse a task carrying ANY loop-token field — `loop_id`, `parent_loop_id`,
  `in_reply_to`, or `run_id` — that is present and non-canonical (enforced by the rule engine before publishing,
  and by agentic-loop intake, which terminates delivery and counts the intake rejection), so no NON-CANONICAL
  token reaches the graph write path, whose parent and reply stamping composes through the panicking entity-ID
  builder.
- `LoopManager.CreateLoopWithID` MUST refuse before registering any loop state.
- Dispatch MUST refuse a non-canonical resolved continuation token, `run_id`, or `in_reply_to` on an inbound
  submission, with a typed error response naming the offending field — synchronous on the HTTP submit path,
  published to the response subject on the channel path — validating after auto-continue resolution and BEFORE
  minting, before the loop is tracked, and before the loop-started metric is recorded, so that a refused
  submission leaves neither a tracked loop nor a moved active-loops gauge, and so both the client's `reply_to`
  and an auto-continued value pass one check.
- `agentrun.Mint` MUST refuse a non-canonical firing-loop instance (its scenario lives in the graph-ingest
  capability, which owns Mint's refusal behavior).

Other payloads carrying a loop token — `UserSignal`, `ApprovalResponse`, and any control or query request whose
census is not yet taken — validate only non-emptiness and are OUTSIDE this requirement; extending the refusal to
them is #1228. Seam validation checks canonical form, not the version bits; that a framework mint is v4 is
asserted at the mint sites, not at the accepting seams.

#### Scenario: a new conversation mints a full canonical UUID on every dispatch intake path

- **GIVEN** a running agentic-dispatch component
- **WHEN** a user message with no `reply_to` arrives via the HTTP submit path, and another via the channel path
- **THEN** each minted `loop_id` is a canonical 36-byte lowercase hyphenated UUID, with no prefix and no truncation
- **AND** the test that verifies this is `TestNewConversationMintsCanonicalUUID`

#### Scenario: a pre-filled non-UUID loop token is refused at loop intake, loudly

- **GIVEN** a decoded task message whose `loop_id` is `workflow-7`
- **WHEN** agentic-loop intake validates it
- **THEN** the delivery is terminated with a classified invalid error, the intake-rejection counter increments,
  and no loop state or context manager exists for the token
- **AND** a direct `CreateLoopWithID` call with the same token is refused before any state is registered
- **AND** the tests that verify this are `TestNonUUIDLoopIDIsTerminatedAtIntake` and
  `TestCreateLoopWithIDRefusesNonUUIDToken`

#### Scenario: a non-canonical reply_to fails at the client boundary on both intake paths

- **GIVEN** a client submitting a message whose `reply_to` is `loop_ab12cd34`
- **WHEN** dispatch handles it on the HTTP submit path
- **THEN** the client receives a synchronous error response naming `reply_to`, and no task is published
- **AND WHEN** the same message arrives on the channel path
- **THEN** an error response naming `reply_to` is published to the response subject, and no task is published
- **AND** the tests that verify this are `TestNonUUIDReplyToHTTPGetsSynchronousError` and
  `TestNonUUIDReplyToChannelGetsErrorResponse`

#### Scenario: a client-authored run_id or in_reply_to is refused at dispatch, before any state is recorded

- **GIVEN** a client submitting a message whose `reply_to` is absent but whose `run_id` is `run-42`
- **WHEN** dispatch handles it on the HTTP submit path
- **THEN** the client receives a synchronous error response naming `run_id`, no task is published, no loop is
  tracked, and the active-loops gauge does not move
- **AND WHEN** a message whose `in_reply_to` is non-canonical arrives on the channel path
- **THEN** an error response naming `in_reply_to` is published to the response subject rather than the submitter
  being left without an answer, and no loop is tracked
- **AND** the tests that verify this are `TestNonUUIDRunIDHTTPGetsSynchronousError` and
  `TestNonUUIDInReplyToChannelGetsErrorResponse`

#### Scenario: a canonical token is accepted on its form alone, whoever authored it

- **GIVEN** a client submitting a message whose `reply_to` is a canonical UUID that this framework never minted
- **WHEN** dispatch handles it
- **THEN** it is ACCEPTED and continues that loop token — form is the whole check, and provenance is not verified
  at any seam
- **AND** the test that verifies this is `TestCanonicalReplyToContinuesTheLoop`

#### Scenario: a task carrying any non-canonical loop-token field is refused

- **GIVEN** three decoded tasks: one whose `in_reply_to` is `workflow-7`, one whose `run_id` is `loop_ab12cd34`,
  one whose `parent_loop_id` is `e2e-parent-1`
- **WHEN** `TaskMessage.Validate` runs on each
- **THEN** each is refused with an error naming the offending field, and no loop state, triple, or run
  association is created for any of them
- **AND** the test that verifies this is `TestTaskMessageRefusesNonCanonicalLoopTokenFields`

#### Scenario: the research pipeline mints canonical UUIDs

- **GIVEN** the `research_graph` tool
- **WHEN** it creates a research-pipeline loop
- **THEN** the loop token is a canonical UUID (no `rg_` prefix), in the AGENT_LOOPS key, the trigger key, and the
  loop-execution entity instance alike — the generator-injection option is deleted, so no path can author one
- **AND** the test that verifies this is `TestResearchLoopIDIsCanonicalUUID`


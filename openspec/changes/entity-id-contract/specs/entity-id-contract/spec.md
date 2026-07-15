## ADDED Requirements

### Requirement: Every entity ID has one canonical six-segment ASCII form

An entity ID MUST contain exactly six non-empty dot-separated segments in
`org.platform.domain.system.type.instance` order. Each segment MUST begin with one ASCII alphanumeric byte and every
remaining byte MUST be ASCII alphanumeric, `_`, or `-`. The complete serialized key, including five dots, MUST be no
longer than 256 bytes. There MUST be no independent per-segment length maximum.

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
derived-index key builders, schemas, tools, and reference configurations. Every lifecycle, ownership, projection,
rule-watch, gateway, and other entity-pattern declaration MUST use the canonical pattern contract before activation.

Invalid new input MUST fail with a typed non-retryable structural error before graph or NATS I/O. SemStreams MUST NOT
expose a permissive mode, legacy validator, normalization shim, alias table, or dual literal/pattern interpretation.

#### Scenario: a malformed Graphable cannot partially persist

- **GIVEN** a Graphable whose complete candidate contains an invalid entity subject or classified entity reference
- **WHEN** graph-ingest reaches the authoritative final-candidate persistence seam
- **THEN** the candidate is rejected before ENTITY_STATES or required projection I/O
- **AND** no partial graph mutation is visible

#### Scenario: configuration cannot disable the contract

- **WHEN** a deployment loads graph-ingest, lifecycle, ownership, projection, or rule configuration
- **THEN** no option exists to accept noncanonical entity IDs or patterns

### Requirement: ObjectStore validates entity identity before entity-derived object I/O

ObjectStore `StoreContent` MUST validate `ContentStorable.EntityID()` through the canonical literal contract before
generating or writing any binary or content object name. Invalid identity MUST return a typed non-retryable structural
error with no binary, content-envelope, event, metric, callback, or stored-message side effect. This requirement MUST
NOT select ObjectStore retention, reachability, reference-counting, or reclamation policy.

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

### Requirement: The beta cutover resets and reingests incompatible persisted identities

The breaking beta release MUST audit and migrate SemStreams, participating owned sister repositories, schemas,
tools, configurations, reference deployments, and exact query fixtures to the canonical contract. A checked-in
rename ledger MAY document source changes but MUST NOT be loaded as a runtime alias or transformation table.

Participating sister-repository migration MUST remain a coordinated v1 release and archive gate. It MUST NOT block
local framework graph-index reconciliation after the entity contract's named local implementation, corpus,
ObjectStore, replay/readiness, storage-proof, and breaking e2e evidence has passed.

If persisted ENTITY_STATES contains a noncanonical entity ID, authoritative and derived graph consumers MUST remain
not-ready with a typed reset/reingest requirement. They MUST NOT rewrite malformed state in place, serve a partial
view, or become ready after a later valid event. Operators MUST export if needed, clear incompatible graph and
derived-index buckets, restart, and reingest from canonical sources.

#### Scenario: invalid beta state poisons readiness

- **GIVEN** ENTITY_STATES contains an entity whose stored identity violates the canonical contract
- **WHEN** graph components start or replay in any order
- **THEN** graph and query readiness remains reset-required
- **AND** no compatibility reader or partial derived result exposes the invalid identity

#### Scenario: clean reingest starts a new readiness lifetime

- **GIVEN** the operator exported if needed and cleared incompatible graph/index buckets
- **WHEN** the process restarts and canonical sources are reingested
- **THEN** every persisted identity satisfies the canonical contract
- **AND** ordinary replay watermarks, not a legacy alias or rewrite, determine readiness

### Requirement: The entity-ID bound gates graph-index fixed-arity activation

Graph-index MUST treat the canonical maximum as `E = 256` when proving complete current-layout keys and filters
against the shared 1,024-byte NATS KV contract. The maximum INCOMING layout MUST be proven as
`2E + 390 = 902` bytes and 13 tokens. Maximum keys and exact-position filters for every affected layout MUST pass the
shared validators and pinned real-NATS conformance before fixed-arity owner reconciliation activates.

This dependency MUST NOT authorize entity-ID encoding, predicate-layout selection, or graph-index activation before
its separate correctness, performance, readiness, and ADR gates pass.

Graph-index production activation MUST depend on the completed local entity-ID contract/API, local zero-violation
corpus, ObjectStore zero-I/O, invalid-state replay/readiness, key/filter proof, and breaking e2e evidence. It MUST NOT
depend on this change being archived or on sister-repository migration completion.

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
- **AND** the dependent graph-index correctness, performance, readiness, and ADR gates also pass

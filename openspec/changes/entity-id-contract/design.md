## Context

Entity IDs are both semantic identities and raw NATS KV keys. Their six positions are already framework-wide, but
validation is split across `pkg/types`, `message`, and graph-ingest. The current split is observably inconsistent:

- `pkg/types.ParseEntityID` accepts any non-empty text in six segments;
- `pkg/types.EntityID.IsValid` checks only non-empty struct fields;
- `message.IsValidEntityID` accepts ASCII alphanumeric, `_`, and `-` in every position but has no total bound;
- graph-ingest requires an ASCII alphanumeric first byte and caps the full ID at 255 bytes through a private regex;
- pattern consumers commonly check only six-part arity and then pass the value to a watcher or glob matcher.

The shared NATS KV contract now limits a literal key or wildcard filter to 1,024 bytes and 64 tokens, but it
deliberately left existing semantic axes unchanged. Graph-index's current worst key is INCOMING, whose maximum is
`2E + 390`, where `E` is serialized entity-ID bytes. A representative corpus cannot prove that formula safe while
`E` is unbounded.

## Goals / Non-Goals

**Goals:**

- establish one exact identity grammar plus distinct exact pattern and query-prefix grammars;
- make `pkg/types` the semantic authority while preserving delegating `message` APIs for callers;
- reject malformed values before authoritative state or derived-key I/O;
- make maximum complete graph-index keys and owner filters provable;
- update every owned beta producer/source/configuration/fixture, then wipe and reseed incompatible NATS state.

**Non-Goals:**

- invent a new identifier shape, normalization scheme, codec, or semantic equivalence;
- add per-position business meaning beyond the existing six named fields;
- implement graph-index reconciliation, retention, or predicate-layout decisions;
- support legacy invalid IDs after the breaking cutover.

## Decisions

### 1. Canonical literals are six exact ASCII segments bounded as one serialized key

The canonical serialized form is:

```text
org.platform.domain.system.type.instance
```

It has exactly six non-empty segments separated by five literal `.` bytes. For each segment:

- byte zero is one of `A-Z`, `a-z`, or `0-9`;
- subsequent bytes are one of `A-Z`, `a-z`, `0-9`, `_`, or `-`.

The serialized form is at most 256 bytes, measured on the original input including dots. There is deliberately no
independent segment maximum. For example, with five one-byte segments and five separators, the remaining segment
may contain 246 bytes and the resulting 256-byte ID is valid. A 247-byte segment in that same shape fails only
because the complete key is 257 bytes.

Validation is byte-exact and non-mutating. It does not trim, case-fold, normalize Unicode, escape, encode, or replace
characters. Since the accepted alphabet is ASCII, every non-ASCII byte sequence is invalid. Literal `*`, `>`, slash,
whitespace, control bytes, leading `_`/`-`, empty segments, and any arity other than six are invalid.

### 2. `pkg/types` is the single parser and validator authority

`pkg/types` owns the exported 256-byte constant plus the coded error-returning `ValidateEntityID(string) error` and
`ParseEntityID(string) (EntityID, error)` surfaces. Parsing performs the complete grammar and size check before
constructing the six-field `EntityID`. `pkg/types.IsValidEntityID`, `message.IsValidEntityID`, and
`EntityID.IsValid` are boolean conveniences over the same authority: they return false for every canonical error but
do not promise a coded error because their signatures return no error. `EntityID.IsValid` validates `EntityID.Key()`
so hand-constructed structs cannot bypass segment syntax or the total bound.

The existing `message.ParseEntityID` and `message.IsValidEntityID` surfaces remain source-compatible delegators to
`pkg/types`; they contain no regex, alphabet helper, size constant, or alternate parsing rule. This is API
delegation, not a compatibility mode: all entry points accept and reject exactly the same bytes.

`ValidateEntityID` and `ParseEntityID` retain the repository's typed invalid classification and pin this exported
contract:

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

Fault precedence is whole-input empty, whole-input byte limit, arity, empty segment, invalid first byte, then invalid
alphabet, with segment faults reported at the first left-to-right position. Details are non-sensitive measurements
and limits only and never echo the full rejected ID. Callers of coded surfaces branch on the exported code/reason
constants rather than parsing prose; boolean helpers are tested only for true/false parity.

Graph-ingest deletes `entityIDRegex`, its `regexp` dependency, and the private 255-byte branch. Its local seam, if
retained for call-site context, delegates to the `pkg/types` parser. Every final ENTITY_STATES candidate is checked
at the authoritative persistence boundary; earlier handler checks are optional diagnostics, not separate authority.

That complete-candidate check validates the `EntityState.ID` and every persisted `Triple.Subject` as canonical
explicit literals. The Graphable fact-arrival lane has one narrowly defined projection convenience: before the
authoritative seam, it may replace an empty projected `Triple.Subject` with the envelope `EntityState.ID`. This is not
identity-byte normalization: it copies an already-governed identity into an omitted projection field and never trims,
rewrites, or repairs a non-empty subject. Mutation requests, direct persistence callers, and replay decoders do not
receive this fill. `MarshalEntityState` and `UnmarshalEntityState` reject any empty or malformed subject that remains.

Entity-reference intent uses the smallest explicit marker already supported by the triple shape. `message` exposes
`EntityReferenceDatatype = "@id"`, aligned with the JSON-LD resource identifier keyword. A string object that is
already a canonical entity ID remains structurally recognized as a relationship for current behavior. When
`Triple.Datatype == message.EntityReferenceDatatype`, the object must be a string and must pass canonical entity-ID
validation; an explicitly marked non-string or malformed string is a candidate error rather than a literal. No
vocabulary-global relationship registry or dot-count/six-dot heuristic participates in classification. Other datatype
values retain their literal datatype meaning.

### 3. Patterns are a separate six-token language

An entity-ID pattern is not an entity ID and is never accepted by the literal parser. The pattern serializer accepts
exactly six non-empty dot-separated tokens and at most 256 total bytes. Each token is either:

- exactly `*`; or
- one canonical literal segment using the same initial and remaining ASCII rules as an ID segment.

No `>` token, embedded wildcard, partial glob, empty token, Unicode, or literal token beginning with `_`/`-` is
accepted. A six-literal-token pattern is valid only when the same bytes are also a canonical entity ID. Pattern
validation lives beside the literal contract in `pkg/types` as `ValidateEntityIDPattern(string) error`, with distinct
code `ErrorCodeEntityIDPatternInvalid = "entity_id_pattern_invalid"`, so callers cannot confuse declaration syntax
with stored identity. It reuses applicable canonical reason/detail constants rather than introducing a parallel
exported reason taxonomy.

Lifecycle, ownership, projection, rules, graph watchers, gateways, and configuration/schema registration validate
patterns before registration or watcher creation. Glob intersection and match algorithms consume only prevalidated
patterns; they do not redefine syntax.

### 4. Query prefixes are a third bounded language

An entity-ID query prefix is neither a literal identity nor a wildcard declaration pattern. A non-empty prefix has
one through six dot-separated tokens. Every token is a canonical literal entity-ID segment; `*`, `>`, partial
wildcards, empty/trailing positions, Unicode, and invalid initial bytes are rejected. The complete prefix is at most
256 bytes.

Empty is accepted only by a public surface whose existing contract explicitly defines empty as match-all, such as
`graph.query.prefix`, `graph.MatchesAnyIDPrefix`, or an absent/empty semantic-search `Scope`. A required scoped input
cannot silently reinterpret empty as global access. Prefix validation occurs before a prefix becomes a KV filter,
embedding scope, fusion scope, GraphQL/gateway request, or other query operation. The validator shares the literal
segment grammar but has its own non-empty coded API, `ValidateEntityIDPrefix(string) error`, and distinct code
`ErrorCodeEntityIDPrefixInvalid = "entity_id_prefix_invalid"`. Surfaces that promise empty means match-all handle
empty before calling the non-empty validator. Prefix failures reuse applicable canonical reason/detail constants;
this change does not add prefix-only exported reasons.

### 5. The 256-byte semantic bound completes the graph-index storage proof

Let `E` be maximum serialized entity-ID bytes. This change fixes `E <= 256`. Using the already-reviewed predicate
bound and current untagged predicate-hex layouts from `graph-index-fixed-arity-reconciliation`, complete current keys
remain:

| Layout | Maximum key bytes at `E = 256` |
|---|---:|
| PREDICATE | `65 + E = 321` |
| NAME / CONTEXT | `E + 454 = 710` |
| INCOMING | `2E + 390 = 902` |
| OUTGOING | `E = 256` |
| raw PREDICATE candidate | `E + 195 = 451` |

INCOMING is the maximum at 902 bytes, leaving 122 bytes beneath the project 1,024-byte key/filter contract. Its
layout has 13 tokens, also beneath the 64-token contract. Owner and forward filters replace one or more literal
positions with one-byte complete-token `*`, so none exceeds the corresponding maximum literal layout. Tests still
construct every maximum key and filter and pass it through the shared NATS validators; arithmetic does not replace
real-NATS conformance.

Graph-index benchmark scaffolding may consume the contract for proof, but framework fixed-arity reconciliation MUST
remain inactive until the named local contract/API, corpus, ObjectStore zero-I/O, clean wipe/reseed, key-budget, and
breaking e2e tasks in this change pass, and the dependent graph-index activation gates pass. It does not wait for
this change to archive. This change does not select raw versus hashed PREDICATE representation.

### 6. Enforcement is unconditional and the beta cutover is clean

The contract applies to literal constructors, parsers, Graphable subjects, triple entity references when classified
as such, mutation requests, ENTITY_STATES persistence/replay, derived-index key construction, ownership/projection
declarations, lifecycle registrations, rule watch patterns, query-prefix/scope inputs, schemas, tools, and reference
configurations.

Graphable projection normalization may supply only an omitted triple subject from the envelope identity before final
candidate validation. All mutation/direct candidates must provide explicit canonical subjects, and replay must accept
only already-canonical stored candidates. Explicit `@id` objects are always reference-validated; canonical-ID-shaped
string objects preserve the existing structural relationship behavior.

ObjectStore's `StoreContent` path validates `ContentStorable.EntityID()` before generating or writing any binary or
content object name. This closes the pre-graph-ingest orphan path but does not select ObjectStore retention,
reachability, reference-counting, or reclamation policy.

Malformed current writes and malformed data injected directly into NATS fail at the authoritative decoder before
state or projection I/O. That fail-fast contract is not an old-state migration path. Before the breaking pre-v1
binary/configuration is used, operators wipe every incompatible NATS resource, restart, and reseed only from updated
canonical owned sources.

There is no runtime flag, legacy validator, alias/rename table, lossy sanitizer, compatibility reader, dual
read/write path, beta persisted-state migration exporter/inspector, or in-place state rewriter. The cutover has no
persisted-state preservation or rollback obligation.

### 7. The pre-v1 clean break is source-driven across every owned reference

The implementation begins with a deterministic source corpus over Go constructors/constants, configs, schemas,
fixtures, generated tools, and owned reference deployments. It reports literal IDs, declaration patterns, and query
prefixes separately and identifies source location plus failure reason. It does not inspect persisted beta state.

SemStreams, SemSource, SemOps, SemConnect, SemTeams, SemSpec, SemDragon, SemLink, and every additional owned producer
update source/configuration/fixtures against the same SemStreams version. The exact breaking release procedure wipes
all incompatible NATS state, reseeds the updated references, and reruns their product e2e. There is no participation
ledger because every reference design is owned and required before v1.

Those coordinated owned-reference gates block the v1 release and archive of this change, not local framework
graph-index activation after its named clean pre-v1 prerequisites have passed.

## Implementation checkpoint: first reviewed slice

The first reviewer-approved implementation slice establishes the canonical `pkg/types` literal, pattern, and prefix
APIs, stable error constants, byte-exact grammar, and boolean/parser delegation. Graph-ingest's existing private
literal gate now delegates to that authority. This is not yet proof that every authoritative persistence or replay
lane reaches the gate.

Pattern enforcement is currently routed through lifecycle workflow/reference validation and ownership
owner/foreign-edge claims. Prefix enforcement is currently routed through the public graph query client,
graph-ingest prefix handler, graph-query forwarding boundary, graph-embedding scope, and FusionNATS prefix/scope
resolution. Their documented empty-match-all cases are preserved outside the non-empty prefix validator. Projection,
rule-watch, gateway, schema, tool, other fusion-engine, and reference-design surfaces remain subject to the complete
inventory and source-update tasks.

ObjectStore now validates `ContentStorable.EntityID()` before binary/content extraction, object-name generation,
operation metrics, or NATS I/O for both binary and non-binary content. The previous invalid-input log that exposed
the raw ID was removed. This closes only the `StoreContent` preflight requirement; it does not close retention or
reclamation policy.

The graph-index unit matrix now uses `E = 256` to prove the current maximum formulas and shared-validator acceptance.
Inactive PREDICATE, NAME, and both INCOMING entity axes also reject a 257-byte ID before lister, Put, or Delete I/O.
Production CONTEXT/OUTGOING semantic preflight, malformed complete-axis controls, and real-NATS maximum key/filter
operations and match sets remain open, so production fixed-arity reconciliation remains blocked.

The reviewed slice passed `task lint`, `go test -race ./...`, and `go test ./test/contract/...`; task 6.4 records the
green breaking e2e evidence. This checkpoint does not claim completion of the full local inventory/corpus, schema
generation, owned-reference source/configuration/fixture updates, clean wipe/reseed proof, documentation, or
real-NATS integration.

## Risks / Trade-offs

- **A valid beta identifier may become invalid.** Leading `_`/`-`, over-256-byte, wildcard-like, Unicode, or malformed
  values require source repair and clean reingest. This is intentional before v1.
- **A single large segment remains legal.** The total bound protects storage while avoiding arbitrary semantic limits
  on one position. Consumers must not assume balanced or short segments.
- **Prefix and pattern semantics can be confused.** Separate APIs and tests pin literal, six-position wildcard, and
  one-to-six-position query-prefix behavior, including which surfaces promise empty as match-all.
- **ObjectStore can persist before graph-ingest.** `StoreContent` rejects an invalid entity ID before binary or
  content-object I/O; broader ObjectStore lifecycle policy remains separately governed.
- **Delegating APIs can look like duplicate authority.** Tests pin identical results across `pkg/types`, `message`,
  graph-ingest, and pattern registration; only `pkg/types` owns grammar code.
- **Wipe/reseed is operationally disruptive.** It is acceptable because no product is in production and every
  reference design is owned. Release docs name the exact NATS resources, reseed commands, and e2e gates; they do not
  promise export, old-state inspection, or rollback.

## Validation Strategy

Use TDD at each boundary: first pin failing literal/pattern/prefix tables and fuzz properties, then migrate delegators,
authoritative persistence, and ObjectStore preflight, then run local and owned-reference source corpora. Prove
255/256/257-byte
boundaries, a legal 246-byte segment, leading-character failures, every allowed remaining byte, Unicode/wildcard
rejection, exact round-trip, and literal/pattern separation.

Storage proof includes shared-validator unit tests, invalid-input no-I/O tests, and pinned real-NATS Put/Get/Delete,
ListKeysFiltered, and Watch match sets at maximum shapes. Cutover proof updates owned source/configuration/fixtures,
wipes incompatible NATS state, reseeds canonical data, and checks exact fresh-state query results. Direct malformed
NATS data is separately rejected without partial output. ObjectStore proof records zero binary and content-object
writes for invalid IDs. Prefix proof covers graph query, semantic-search/fusion scopes, and gateway inputs before any
NATS filter or query I/O. Final gates include lint, full `-race`, schema no-drift, contract suites, real-NATS
integration, and every affected product e2e tier before the BREAKING release lands.

Authoritative-state tests separately prove Graphable empty-subject fill before marshal, rejection of the same empty
subject on mutation/direct/replay candidates, canonical validation of every explicit subject, current structural
recognition of canonical-ID-shaped strings, and strict string-plus-canonical validation for `@id` objects. Direct NATS
poison tests exercise replay failure independently of writer-side checks.

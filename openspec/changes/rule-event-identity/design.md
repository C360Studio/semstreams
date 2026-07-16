## Context

Extracted from `entity-id-contract` design §6a so the contract can be reviewed on its merits (ADR-076 carries the
decision and the open review questions; this document carries the mechanics). The implementation exists on
`codex/entity-id-contract-completion`; amendments from the ADR review are applied there, not designed twice.

## Constructor contract

Every exported graph-event constructor returns `(*Event, error)`, validates the complete candidate before
returning, and returns `nil` plus a typed error for invalid input — never a partial event. `Event.Validate` is
pure: it recognizes only the five declared `EventType` values, validates the primary ID through
`pkg/types.ValidateEntityID`, requires a canonical `TargetID` on relationship events and an empty one on entity
events, requires finite confidence in `[0,1]`, and requires complete metadata with version `1.0.0`. Constructors
apply the version default only to their local metadata copy, so a direct struct literal with an empty version
fails validation rather than publishing an ambiguous wire value.

Envelope fields (`entity_id`, `target_id`, `confidence`, `metadata`) and constructor-owned properties
(`alert_type`, `source_entity`, `status`, `edge_type`, `type` per constructor) are exclusive: a caller-supplied
collision is rejected, never silently resolved. Constructors copy the top-level properties map and never mutate
the caller's; nested reference values remain caller-owned and immutable-by-contract after construction — no
reflection-based deep copy of arbitrary `any` values.

## Derived identities

Alert: `semstreams.framework.graph.rules.alert.<sha256hex>`. Trigger:
`semstreams.framework.graph.rules.trigger.<sha256hex>`. Rationale for the framework namespace and the digest
framing (length-prefixed fields, versioned domain separators, excluded mutable fields, fixed 103/105-byte
identities) is in ADR-076. Identical semantic occurrences replay to the same entity; any framed input change
changes the digest. Authoring identifiers (pack IDs, rule IDs) never enter NATS key positions raw on the entity
axis.

Trigger identity makes replicas of one pack converge on one entity while letting different composed packs reuse a
local rule ID. Legacy `rule.<id>.triggered` and `test.entity.<id>` shapes are replaced with no compatibility path.

## PackID

`Config.PackID` is the producer identity for both trigger digests and projection ownership
(`rule-pack.<PackID>` owner ID). Universally required in every integration mode, runtime-static, 1–246 exact
ASCII bytes of `[A-Za-z0-9_=-]+` (246 = 256-byte owner-key budget minus `rule-pack.`), schema-pinned
(`minLength: 1`, `maxLength: 246`, pattern), and one literal KV token so the owner key has fixed arity. There is no
default, fallback, or normalization. Because two enabled
processors with one PackID are one semantic producer collision, composition validates the complete enabled set
and hard-fails duplicates before binding, watching, activation, or publication. The public default-config
construction API takes PackID explicitly and returns an error; an unexported defaults value may support config
overlay internally.

## Extension and digest versioning

The five event types are the closed v1 set. A sixth type is additive only with an explicit constructor, target-shape
validation, subject mapping, schema/documentation, and consumer support; `Event.Validate` continues to reject unknown
strings. A digest-domain v2 creates a new entity family and requires an explicit cutover plus retention/migration
plan. It never aliases or rewrites v1 identities in place.

## Batch preflight

The rule publisher structurally validates the complete event batch before marshalling any member, then encodes every
member before the first publication, in both integration modes. `[valid, invalid]`, `[invalid, valid]`, and typed-nil
members fail before marshal or external side effects. JSON-unencodable property values can fail only during the
encoding pass; already-encoded frames remain process-local and the whole batch is discarded before NATS, retry,
callback, success/business/publication metric, or counter side effects. The designated boundary emits exactly one
bounded lane/reason rejection metric with no identity bytes in labels. A valid disabled-integration batch is still
encoded, then returns success without emission — disabled mode cannot hide a malformed producer.

## Risks / Trade-offs

- Digest instances are operator-opaque; provenance lives in properties. Deliberate: author strings stay out of
  key positions.
- Timestamp-in-digest intentionally couples alert cardinality to occurrence frequency. ADR-076 resolves alerts as
  occurrence records and makes their growth an explicit ADR-073 operational-retention obligation before v1.
- No in-repo consumer currently owns `graph.events.*`. The owned-repository cutover must select and prove consumer
  create/upsert/update semantics: rule-trigger producers emit only `entity_update`, so a must-exist update consumer
  would reject the first occurrence and append semantics would violate the one-stable-entity contract.
- The constructor break touches every call site at once; the batch preflight and owned-repo compile audit are the
  containment.

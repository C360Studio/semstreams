# ADR-076: Framework-Derived Entity Identity for Rule Events

## Status

**Accepted (2026-07-16).** Accepted after adversarial review of identity semantics, key shape, extension,
versioning, publication side effects, and composition lifecycle against the extracted implementation.

## Context

The canonical six-part entity-ID contract is enforced fail-closed at every authoritative write. Rule-engine-derived
entities (alerts, trigger state) previously used generated shapes that violate it (`alert_...`,
`rule.<id>.triggered`, `test.entity.<id>`); those writes now fail. The framework needs a canonical identity scheme
for entities it derives itself, where no natural six-part identity exists and authoring identifiers (rule IDs, pack
names) are arbitrary strings that must not leak into NATS key positions.

## Decision

1. **Framework namespace, not source namespace.** Derived entities live under
   `semstreams.framework.graph.rules.<type>.<instance>` (`type` ∈ {`alert`, `trigger`}). The affected entity is
   carried as a property (`source_entity`), never as the derived entity's namespace — a 256-byte source can spend
   252 bytes in its first four positions, leaving no room for suffixing, so namespace reuse is invalid over the
   complete input domain. No length-dependent fallback.
2. **Digest instances.** The instance segment is the full 64-char lowercase SHA-256 hex of a versioned,
   length-framed byte sequence (each field: unsigned 64-bit big-endian length, then exact bytes; no
   normalization). Alerts frame `semstreams.graph.alert.v1`, source ID, alert type, rule name, source component,
   and the timestamp (int64 BE seconds + uint32 BE nanos). Triggers frame `semstreams.graph.rule-trigger.v1`,
   PackID, rule ID. Fixed identity lengths: alert 103 bytes, trigger 105 bytes — in-budget for every accepted
   input.
3. **Fail-closed constructors.** Every exported graph-event constructor returns `(*Event, error)` and validates
   the complete candidate; `Event.Validate` is pure; envelope-reserved and constructor-owned property keys are
   collision-rejected; top-level property maps are copied, nested values stay caller-owned and immutable by
   contract. Batch publication preflights the complete batch atomically in both integration modes.
4. **PackID is the producer identity.** Universally required, static, 1–246 ASCII bytes of `[A-Za-z0-9_=-]+`
   (246 = 256-byte owner-key budget minus `rule-pack.`), used raw in the ownership owner ID `rule-pack.<PackID>`
   and framed into trigger digests. PackID is one literal KV token, so `rule-pack.<PackID>` is always a fixed
   two-position owner key; `.` is forbidden while `=` remains legal. Duplicate enabled PackIDs are one semantic
   producer collision → composition rejection before binding. No default, fallback, random identity, or
   config-derived hash.
5. **The five event types are a closed v1 set.** A new event type is an additive contract change only when it ships
   with an explicit constructor, target-shape validation, subject mapping, schema/documentation, and consumer
   support. Unknown values remain invalid; accepting arbitrary strings is not the extension mechanism.
6. **Digest versions create new identities.** Changing either `v1` domain separator creates a distinct entity
   family. A future v2 requires an explicit cutover and retention/migration plan; it never rewrites or aliases v1
   identities in place.

## Consequences

- Identical semantic occurrences replay to the same entity (KV-twofer dedup); repeated triggers from one
  pack/rule update one deterministic entity.
- BREAKING for every constructor call site, owned-repo alert-ID assertion, and rule-processor config
  (explicit `pack_id` and explicit `enable_graph_integration`; its default changes from true to false). Pre-v1 clean
  cutover, no dual identity.
- Operator-opaque instances (digests): tracing an alert to its inputs requires the properties, not the key. This
  is deliberate — inputs include arbitrary author strings that must not enter key positions.
- Alerts are occurrence entities: the timestamp remains identity-bearing. Repeated observations at different
  instants therefore increase `ENTITY_STATES` cardinality. This is deliberate audit semantics, not condition-state
  replacement, and makes alert growth an explicit ADR-073 operational-retention obligation before v1.

## Review resolutions

1. Timestamp remains in the alert digest: alerts are occurrence records. The retention epic must bound or clean
   their resulting entity cardinality; callers needing current condition state require a separate state contract.
2. PackID drops `.` to preserve one literal token and fixed owner-key arity. `=` remains because it is legal in the
   shared literal-token contract and does not change arity.
3. The event type set is closed for v1; extension follows Decision 5 and is additive only with the complete shape
   and consumer contract.
4. Digest v2 creates new entities and requires an explicit cutover; no in-place migration or alias is implied.

## Supersession

Extends ADR-074 (canonical predicates) and the entity-ID contract with the framework-derived identity family.
Replaces the implicit legacy alert/trigger naming with no compatibility path.

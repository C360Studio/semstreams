## Why

Enforcing the canonical six-part entity-ID contract surfaced that rule-engine-derived entities carried
non-canonical generated identities: legacy `alert_...` values and three-part `rule.<id>.triggered` /
`test.entity.<id>` shapes. Those writes now fail closed at the authoritative seam, so a replacement identity
scheme is mandatory — this is the enforcement working as intended, not a regression.

The replacement is a durable cross-repo contract: owned products assert on alert entity IDs today, and the
producer identity (`Config.PackID`) participates in both trigger identity and projection ownership. A contract of
this reach gets its own change, its own ADR (ADR-076), and adversarial review — it was originally drafted as a
completion task of `entity-id-contract` (design §6a, tasks 3.4a/3.4b/3.6c) and is extracted here so it can be
reviewed on its merits. The implementation already exists on `codex/entity-id-contract-completion`; this change
frames it for review, not rewrite.

## What Changes

- Every exported graph-event constructor returns `(*Event, error)`, validates the complete candidate before
  return, and never returns a partial event. `Event.Validate` is pure. No error-dropping constructor,
  permissive overload, or deprecated wrapper remains. Clean pre-v1 API break.
- Framework-derived alert entities use `semstreams.framework.graph.rules.alert.<sha256>`; the affected entity
  stays in `source_entity` rather than being misrepresented as the alert's owner namespace. The instance digest
  is a length-framed, versioned byte sequence (domain separator `semstreams.graph.alert.v1`, source ID, alert
  type, rule name, source component, timestamp) — identical occurrences replay to the same entity.
- Rule trigger entities use `semstreams.framework.graph.rules.trigger.<sha256>` (domain separator
  `semstreams.graph.rule-trigger.v1`, PackID, rule ID): one stable entity per pack/rule pair; different packs may
  reuse local rule IDs without collision.
- `Config.PackID` becomes universally required and runtime-static: one literal KV token of 1–246 ASCII bytes
  matching `[A-Za-z0-9_=-]+`, no default, no component-name fallback, no normalization; duplicate enabled PackIDs are a
  composition error before binding, watching, activation, or publication. Schema requires `pack_id`
  unconditionally.
- Batch preflight: the rule publisher structurally validates every member, then encodes the complete event batch
  before publishing any member, in both integration modes. Structural-invalid, typed-nil, and JSON-unencodable
  batches produce no publication side effects and one bounded rejection metric.

**BREAKING:** constructor signatures, generated alert/trigger identities, PackID schema requirement, the
default-config construction API, and `enable_graph_integration` defaulting from true to false. Pre-v1 clean cutover;
owned repos migrate assertions and configs; no
compatibility constructor or dual identity.

## Non-goals

- Changing the entity-ID or predicate grammar (owned by their contracts).
- Alert lifecycle/retention policy.
- New event types beyond the five declared; those require an additive constructor, shape, subject, schema, and
  consumer contract.

## Capabilities

### Modified Capabilities

- `graph-events` (seeded by this change): constructor contract, derived identities, batch preflight.
- Rule-processor configuration: universal PackID.

## Dependencies

- Canonical entity-ID contract enforced at authoritative seams (shipped).
- ADR-076 accepted after adversarial review — the review gate for this change's design, including the open
  questions listed in the ADR.

## Impact

- **Framework code:** `graph/event*`, `pkg/rulepack`, `processor/rule` (constructors, publisher, config, schema),
  fixtures.
- **Owned repos:** migrate graph-event constructor calls to `(*Event, error)`, replace old alert-ID assertions
  with the framework digest identity, declare stable PackIDs in every rule-processor config, prove composition
  uniqueness, rerun product e2e.
- **Operators:** every rule-processor config needs an explicit `pack_id`; changelog names the identity changes
  and the wipe/reseed coverage.

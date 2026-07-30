# graph-events Specification

## Purpose

`graph-events` governs how a **graph event is constructed**: canonically, deterministically, and
free of side effects. Construction is a pure function of its inputs, so the same inputs yield the
same event, and building one never writes, publishes, or mutates anything as a byproduct.

That matters because graph events are identity-bearing — downstream consumers correlate and
deduplicate on them. A constructor that varied its output, or that emitted as a side effect of being
called, would make replay and recovery unreliable in ways that surface far from the cause.

**What it does NOT cover.** Event delivery, subscription, and stream semantics live with the
transport capabilities; rule-side event identity and PackID lineage live with the rule capabilities;
entity identity itself belongs to `entity-id-contract`. This capability is narrowly the construction
contract.
## Requirements
### Requirement: Graph-event construction is canonical, deterministic, and side-effect free

Every exported graph-event constructor MUST return `(*Event, error)`, MUST validate its complete candidate before
returning, and MUST return `nil` plus the typed error for invalid input. No deprecated, permissive, or error-dropping
constructor MAY remain. `Event.Validate` MUST be pure and MUST reject unknown event types, noncanonical primary IDs,
non-finite or out-of-range confidence, incomplete metadata, a metadata version other than `1.0.0`, and property keys
that collide with the event envelope. Relationship create/delete events MUST require a canonical target ID; entity
create/update/delete events MUST reject a non-empty target ID.

The five declared event types are the closed v1 set. A new type MAY be added without changing existing wire values
only when the same change defines its constructor, target shape, validation, subject mapping, schema/documentation,
and consumer support; unknown values MUST remain rejected. Changing either v1 digest domain separator MUST create a
new identity family and MUST carry an explicit cutover plus retention/migration plan. A digest v2 MUST NOT rewrite,
alias, or silently reinterpret v1 identities in place.

Constructors MUST default an empty metadata version to `1.0.0` only on their local metadata copy before validation.
They MUST create a distinct top-level map for caller-owned properties, MUST reject caller values for
constructor-owned or envelope-reserved property keys, and MUST NOT add, remove, or replace keys in the caller's map on
either success or failure. Later top-level mutation of either map MUST NOT affect the other. Nested reference-bearing
property values are caller-owned, are not deep-copied, and MUST be treated as immutable after successful
construction; construction MUST NOT mutate them. A nil `*Event`, including a typed nil behind the rule-event
interface, MUST return an error rather than panic. The publishing boundary MUST preflight the complete event batch
even when graph integration is disabled. It MUST structurally validate every member before marshalling any member and
MUST encode every member before publishing the first. Any structurally invalid or nil member MUST reject the whole
batch before marshal or external side effects. Any JSON-encoding failure MUST discard every prepared frame before
NATS, retry, callback, success/business/publication metric, or counter side effects. The designated validation
boundary MUST record exactly one rejection metric with stable bounded lane/reason labels and MUST NOT put identity or
predicate bytes in metric labels.

`NewAlertEvent` MUST validate its canonical source entity and non-empty alert type and MUST derive the created entity
under the exact six-position namespace `semstreams.framework.graph.rules.alert.<instance>`. The instance MUST be the
full lowercase hexadecimal SHA-256 digest of a domain-separated, length-framed sequence containing the exact source
entity ID, alert type, metadata rule name, metadata source, and timestamp instant. The domain separator MUST be
`semstreams.graph.alert.v1`; it and every string MUST be framed by an unsigned 64-bit big-endian byte length; the
timestamp MUST be signed 64-bit big-endian Unix seconds plus unsigned 32-bit big-endian nanoseconds. Properties,
metadata reason, and metadata version MUST NOT participate in identity. Source namespace positions MUST NOT be
copied or selected by length, because a valid 256-byte source can leave no room for an alert suffix. The resulting
alert ID MUST be the fixed 103-byte canonical form for every accepted source ID.

Direct expression and test-rule trigger producers MUST require a non-empty stable rule-pack producer identity and
MUST update the exact six-position identity
`semstreams.framework.graph.rules.trigger.<instance>`. The instance MUST be the full lowercase hexadecimal SHA-256
digest of the unsigned 64-bit big-endian byte-length-framed domain separator
`semstreams.graph.rule-trigger.v1`, followed by the equally framed exact `Config.PackID` bytes and exact rule-ID bytes.
Pack and rule IDs MUST NOT be normalized or embedded directly into an entity-ID segment, and trigger producers MUST
NOT reuse the alert namespace. Every rule processor MUST declare a non-empty PackID regardless of whether graph
integration is enabled. Empty identity MUST fail configuration and factory construction before rule activation.
PackID MUST contain 1–246 ASCII bytes and MUST match exactly `[A-Za-z0-9_=-]+`; the 246-byte maximum MUST keep the
derived `rule-pack.<PackID>` owner key within 256 bytes. It MUST be one literal KV token, so the owner key always has
exactly two positions. Empty, 247-byte, Unicode, whitespace, colon, dot, slash, wildcard,
and every other outside-alphabet value MUST fail without normalization. PackID MUST have no implicit default,
component-name fallback, random value, normalization, or config-derived hash, and MUST remain static across runtime
updates. Replicas using the same exact pack/rule pair MUST resolve to one stable canonical 105-byte entity; changing
either exact pack ID or rule ID MUST resolve to a different identity, so independently composed packs may reuse local
rule IDs without colliding.

The operator schema MUST require `pack_id` unconditionally and MUST expose `minLength: 1`, `maxLength: 246`, and
pattern `^[A-Za-z0-9_=-]+$`. The public default-config construction API MUST require the caller to supply and validate
PackID rather than return an activation-ready anonymous configuration. Config validation, direct rule factories, and
composition activation MUST enforce the same byte bound and alphabet. Two enabled rule processors in one composition
MUST NOT declare the same PackID: duplicate identity MUST fail the complete composition before projection binding,
watcher creation, rule activation, event construction, or graph publication. A processor declaring projection
contracts without PackID MUST fail configuration rather than having its ownership binding silently skipped.

#### Scenario: a maximum canonical source produces a bounded deterministic alert ID

- **GIVEN** an exact 256-byte canonical source entity ID and complete alert metadata
- **WHEN** `NewAlertEvent` constructs the same semantic occurrence more than once
- **THEN** each call returns the same 103-byte canonical `semstreams.framework.graph.rules.alert.<full-sha256>` ID
- **AND** no source byte is copied into a variable-length identity position or discarded from the digest input

#### Scenario: alert identity includes occurrence fields but excludes mutable description

- **GIVEN** one valid alert input
- **WHEN** the source ID, alert type, rule name, source component, or timestamp changes one at a time
- **THEN** each changed occurrence has a different alert entity ID
- **AND** changing properties or metadata reason does not change the identity digest
- **AND** an empty constructor metadata version and explicit `1.0.0` produce the same identity because the empty value
  is defaulted only on the local copy
- **AND** every other metadata version is rejected rather than compared as a successful identity input

#### Scenario: constructor failure cannot mutate caller state or publish

- **GIVEN** a caller property map and an alert input with a malformed source, incomplete metadata, or reserved-key
  collision
- **WHEN** graph-event construction and the publishing boundary run
- **THEN** construction returns `nil` with a typed non-retryable error and leaves the caller map unchanged
- **AND** no event, marshal, NATS call, retry, callback, success/business/publication metric, or published-event
  counter is produced
- **AND** the designated boundary records exactly one bounded rejection metric without identity bytes in labels

#### Scenario: one invalid batch member prevents every publication side effect in either integration mode

- **GIVEN** an event batch with an invalid member first or after a valid member, or a typed-nil member
- **AND** graph integration is enabled or disabled
- **WHEN** the rule publisher preflights the batch
- **THEN** it validates every member before marshalling or publishing any member and rejects invalid or nil input
- **AND** no batch member produces marshal, NATS, retry, callback, success/business/publication metric, or
  published-event counter side effects
- **AND** the designated boundary records exactly one bounded rejection metric without identity bytes in labels

#### Scenario: one JSON-unencodable member cannot cause partial publication

- **GIVEN** a structurally valid event batch whose later member contains a JSON-unencodable property value such as
  non-finite `NaN`
- **AND** graph integration is enabled or disabled
- **WHEN** the rule publisher prepares the complete batch
- **THEN** it may encode earlier members internally but publishes none of them
- **AND** it performs no NATS, retry, callback, success/business/publication metric, or published-event counter side
  effect
- **AND** the designated boundary records exactly one bounded marshal rejection without identity bytes in labels

#### Scenario: every rule processor has an explicit stable pack identity

- **GIVEN** graph integration is enabled or disabled
- **WHEN** a rule processor configuration omits or empties `pack_id`, or a direct rule factory receives empty PackID
- **THEN** configuration or factory construction fails before any rule activates or constructs an event
- **AND** no default, component-name fallback, random identity, normalization, or config-derived hash is substituted

#### Scenario: PackID grammar and schema bounds agree at activation

- **GIVEN** PackIDs of one ASCII byte and 246 ASCII bytes matching `[A-Za-z0-9_=-]+`
- **WHEN** operator-schema validation, config construction, direct rule-factory construction, and composition
  activation validate them
- **THEN** every boundary accepts them and a 246-byte value produces a 256-byte `rule-pack.<PackID>` owner key
- **AND** the schema requires `pack_id` with `minLength: 1`, `maxLength: 246`, and pattern
  `^[A-Za-z0-9_=-]+$`
- **AND** empty, 247-byte, Unicode, whitespace, colon, dot, slash, wildcard, or otherwise outside-alphabet values are
  rejected by every boundary before rule activation

#### Scenario: duplicate enabled packs fail composition atomically

- **GIVEN** two enabled rule processors declare the same exact PackID
- **WHEN** composition validates rule-pack identities
- **THEN** the complete composition fails before projection binding, watcher creation, rule activation, event
  construction, or graph publication
- **AND** the collision is not reduced to a log-and-skip ownership warning

#### Scenario: explicit PackID remains stable across disabled publication

- **GIVEN** a rule processor has an explicit PackID and graph integration is disabled
- **WHEN** a built-in rule constructs a trigger event and the publisher receives its batch
- **THEN** the trigger identity uses the exact PackID and the complete batch is preflighted
- **AND** a valid batch is dropped with no publication side effect while an invalid batch is rejected atomically
- **AND** enabling graph integration later does not change the producer identity

#### Scenario: a valid disabled-integration batch is validated but not emitted

- **GIVEN** a batch whose every event is valid and graph integration is disabled
- **WHEN** the rule publisher preflights the batch
- **THEN** validation succeeds and the disabled publisher returns success
- **AND** it encodes the complete batch but performs no NATS, retry, callback, success/business/publication metric, or
  published-event counter side effect

#### Scenario: direct event validation is pure and shape aware

- **GIVEN** a direct graph event with an unknown type, malformed primary or target ID, invalid target shape, NaN or
  infinite confidence, incomplete metadata, unsupported version, or envelope-shadowing property
- **WHEN** `Event.Validate` runs
- **THEN** it rejects the event without filling defaults or mutating any field or map
- **AND** the publisher rejects the same event before external side effects


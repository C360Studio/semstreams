# user-response-subject-ownership Specification

## Purpose

Define framework ownership of `user.response.>` as the single registered `agentic.user_response.v1` family, including framework rule guards and governance removal.

## Requirements
### Requirement: The user-response subject family SHALL carry one registered type

Every message published under `user.response.>` SHALL be a registered `BaseMessage` with message type
`agentic.user_response.v1` and concrete `*agentic.UserResponse` payload. Its resolved subject SHALL identify the
payload's channel type and channel ID.

The dispatch default and each of the eight shipped configurations that explicitly redeclare this output SHALL name
interface `agentic.user_response/v1`. Production merge SHALL therefore expose nine typed declarations.

Raw rule envelopes, generic JSON verdicts, agent task messages, governance notifications, and product requests SHALL
NOT use this family. A diagnostic decoder SHALL NOT be described as an end-user delivery adapter.

#### Scenario: typed response is observed

- **GIVEN** a production registry containing the agentic built-ins
- **AND** a valid `agentic.user_response.v1` BaseMessage on `user.response.<channel_type>.<channel_id>`
- **WHEN** production message-logger decodes the message
- **THEN** its concrete payload is `*agentic.UserResponse`
- **AND** the proof claims typed decoding and observation, not external delivery

#### Scenario: shipped declarations preserve the interface

- **WHEN** all shipped configurations are constructed through production merge
- **THEN** eight explicit declarations plus the default-only ninth expose `agentic.user_response/v1`

#### Scenario: flat payload is forbidden

- **GIVEN** a flat rule or governance payload
- **WHEN** its writer attempts to target `user.response.>`
- **THEN** the writer is rejected before publication

### Requirement: Every arbitrary rule publisher SHALL enforce the reservation twice

Definition validation and post-substitution execution SHALL reject each of `publish`, `publish_agent`, and `approve`
when its subject targets `user.response.>`. Static validation SHALL catch literal and fixed-prefix template subjects.
Execution SHALL catch fully dynamic subjects after substitution, including direct executor calls that bypass config
loading.

Rejection SHALL occur before publisher invocation or action-specific side effects. The classifier SHALL distinguish
the exact token family from unrelated prefixes and SHALL NOT expose an adopter override.

#### Scenario: fixed-prefix template fails at load

- **GIVEN** any of the three actions with subject `user.response.$entity.instance`
- **WHEN** its rule definition is validated
- **THEN** validation fails with the action location and reserved family
- **AND** no rule is activated

#### Scenario: dynamic subject fails after substitution

- **GIVEN** any of the three actions whose declared subject does not reveal its final family
- **AND** substitution resolves it to `user.response.cli.channel-1`
- **WHEN** the action executes
- **THEN** execution fails before publishing or performing action-specific side effects

#### Scenario: unrelated prefix remains valid

- **GIVEN** an otherwise valid action targeting `user.responses.audit`
- **WHEN** reservation validation runs
- **THEN** that distinct token family is not rejected by this requirement

### Requirement: Governance SHALL expose no orphan user-notification surface

Agentic-governance SHALL NOT declare `notify_user`, a `user_errors` port, or a user-notification publisher. Any raw
configuration containing exact nested key `violations.notify_user` SHALL fail with a breaking-migration error before
default merge, port resolution, filter construction, or NATS I/O, regardless of the key's JSON value.

Governance logging, metrics, KV audit storage, admin severity alerts, and `governance.violation.*` publication SHALL
remain. Audit storage SHALL use canonical valid key `violation.<id>`. No reader, alias, or conversion SHALL accept
the prior `violation:<id>` spelling because NATS KV rejected it before any legacy record could persist. No replacement
user-notification subject or typed response SHALL be added.

The canonical key SHALL pass through the shared KV literal-key validator before bucket lookup or any NATS I/O.

#### Scenario: null retired key fails

- **GIVEN** agentic-governance raw config containing `"violations":{"notify_user":null}`
- **WHEN** the component is constructed
- **THEN** construction fails with a migration error naming `violations.notify_user`
- **AND** no runtime resource or NATS I/O is created

#### Scenario: governance audit behavior remains

- **GIVEN** a policy violation after the breaking change
- **WHEN** governance handles it
- **THEN** configured audit storage persists under `violation.<id>` and metrics, logs, admin alerting, and violation
  event behavior remain
- **AND** no user response is published

#### Scenario: invalid audit-key spelling has no compatibility path

- **GIVEN** the retired NATS-invalid `violation:<id>` key spelling
- **WHEN** the fresh-state breaking version starts
- **THEN** it installs no reader, alias, or state conversion for that spelling
- **AND** new audit records use only `violation.<id>`

### Requirement: Message-logger declaration truth SHALL reflect governance removal

Across the frozen 21 shipped configurations, raw message-logger census truth SHALL remain 395 rows, 243 per-config
exact keys, and 54 global strings. Effective truth SHALL be 579 rows, 380 keys, and 70 strings. Effective-minus-raw
SHALL be 184 rows, 137 keys, and 16 strings, with 27 added NATS outputs and exactly 47 loop/dispatch collapses and zero
governance collapses.

#### Scenario: shipped census is recomputed

- **WHEN** every enabled component in the frozen configuration scope is constructed through production factories
- **THEN** the raw, effective, delta, added-kind, and collapse counts equal the target measurements
- **AND** no governance `user_errors` declaration exists

### Requirement: The breaking cut SHALL use fresh state without compatibility paths

SemStreams-owned sources, ports, schemas, configurations, fixtures, and tests SHALL use the typed response contract
and start on newly provisioned NATS storage. No legacy reader, flat/typed union, dual format, dual subscription, alias,
bridge, forwarding subject, retained-state migration, or rollback lane SHALL exist. Downstream product migration and
validation belong to the downstream owner and SHALL NOT block SemStreams capability archive. Discovery of retained
deployed state SHALL stop only that adoption for separate owner-reviewed migration or recovery design.

#### Scenario: unmigrated rule pack starts

- **GIVEN** an old rule still targeting `user.response.$entity.instance`
- **WHEN** the breaking SemStreams version loads it
- **THEN** boot fails visibly during rule validation
- **AND** no compatibility path forwards or decodes the message

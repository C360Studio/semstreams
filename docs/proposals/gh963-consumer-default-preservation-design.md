# GitHub #963 consumer-default preservation design

Baseline: `1002a0513a8aab3dd4d28faf4f0ee6c8d50a49ab`

Current: `809f1807958e6be416f61f3e75d114d193a39d29`

Status: `owner accepted; independent design and implementation reviews approved; verification complete`

Inventory body SHA-256: `3e48022be1b262518fd75de78df04bd3323abd7fb3307039e52466f582fb3d68`

Design body SHA-256: `452b7a96fcdf86d011ded4dc6a5491d3bb6b23c1be0d3764c165e4692404fe05`

Hash method: `sed -n '/^## Design body$/,$p' <file> | tail -n +2 | shasum -a 256`

## Design body

# GitHub #963 consumer-default preservation correction design

## Authority and dependency

This design depends on the independently reviewable correction inventory identified in the header. The owner's fallback
ruling is binding, but implementation remains gated on independent `INVENTORY PASS` and design review of these exact
artifact bodies.

The completed `honor-jetstream-max-ack-pending` OpenSpec archive and accepted #963 proposal artifacts remain unchanged.
This correction receives a new active change and new artifact identities rather than rewriting history.

## Owner-approved ruling

For the document and IoT example processors only:

- omitted `deliver_policy` resolves to `all`;
- omitted `ack_policy` remains `explicit`;
- omitted or explicit zero `max_deliver` resolves to `5`;
- only a positive explicit `max_deliver` overrides `5`;
- explicit valid delivery and ack declarations win for their fields;
- `MaxAckPending` forwards independently through the accepted #963 path; and
- neither `component.GetConsumerConfig` nor `natsclient.buildConsumerConfig` changes globally.

## Target invariant and behavior matrix

Canonical extraction remains the validator and value carrier. Each affected component applies its historical local
default after extraction. Runtime facts intentionally represent omitted and explicit-zero `max_deliver` identically.

| Port declaration | Final document/IoT consumer behavior |
|---|---|
| Delivery omitted | `DeliverAll` |
| Valid delivery explicit | Declared delivery policy |
| Ack omitted | Explicit ack |
| Valid ack explicit | Declared ack policy |
| Max delivery omitted or explicit zero | `5` |
| Positive max delivery explicit | Declared positive maximum delivery count |
| `max_ack_pending` omitted or zero | Zero request; observed NATS policy |
| Positive or `-1` `max_ack_pending` | Exact declared value forwarded and observed |

The fields are independent. Restoring the delivery empty fallback and redelivery zero/default must not suppress,
replace, or infer `MaxAckPending`.

## Implementation design

Each affected package gains one small private resolver used by `setupJetStreamConsumer`:

1. Call `component.GetConsumerConfig(port)` so canonical validation and carried values remain authoritative, subject to
   the local zero rule for `max_deliver` below.
2. Read the already-resolved immutable stream facts from the port.
3. If `stream.DeliverPolicy()` is empty, set only `consumerConfig.DeliverPolicy = "all"`.
4. If `stream.MaxDeliver()` is zero, set only `consumerConfig.MaxDeliver = 5`. This covers omission and explicit zero;
   implementation must not claim to distinguish them.
5. Leave the extracted ack policy unchanged; its omission default is already `explicit`.
6. Leave `consumerConfig.MaxAckPending` unchanged and forward it to `natsclient.StreamConsumerConfig`.

The resolver returns the final local defaults plus eligible overrides in one value. Consumer creation and policy
observation continue through the existing #963 managed-consumer operation and owner context.

This intentionally mirrors graph-ingest and ObjectStore without adding a shared exported helper. A global helper would
make a two-component lifecycle policy look like universal port grammar and would expand the adopter surface.

## Exact file plan

Runtime and focused tests:

- `examples/processors/document/component.go`
- `examples/processors/document/component_test.go`
- `examples/processors/document/component_integration_test.go` if the real-NATS proof is kept separate
- `examples/processors/iot_sensor/component.go`
- `examples/processors/iot_sensor/component_test.go`
- `examples/processors/iot_sensor/component_integration_test.go` if the real-NATS proof is kept separate

New active OpenSpec change:

- `openspec/changes/preserve-example-consumer-defaults/proposal.md`
- `openspec/changes/preserve-example-consumer-defaults/design.md`
- `openspec/changes/preserve-example-consumer-defaults/tasks.md`
- `openspec/changes/preserve-example-consumer-defaults/specs/jetstream-consumer-policy/spec.md`

No config, generated schema, global component extractor, natsclient builder, archived OpenSpec, or accepted #963
proposal file is expected to change.

## Exact follow-up OpenSpec delta draft

The active delta targets the current `jetstream-consumer-policy` capability and adds exactly this requirement:

```markdown
## ADDED Requirements

### Requirement: Component-specific consumer defaults survive canonical extraction

The document and IoT example processors SHALL retain their established local consumer defaults. Omitted
`deliver_policy` SHALL resolve to `all`, and omitted `ack_policy` SHALL resolve to `explicit`. A zero `max_deliver`,
whether produced by omission or explicit JSON zero, SHALL resolve to `5`; only a positive explicit `max_deliver` SHALL
override `5`. Explicit valid delivery and acknowledgement declarations SHALL win for their own fields.
`max_ack_pending` SHALL remain independent and SHALL forward exactly according to the ordinary-input policy.

#### Scenario: Omission preserves replay-safe cold-start behavior

- **GIVEN** a document or IoT JetStream input omits delivery and acknowledgement policy
- **AND** its `max_deliver` resolves to zero from omission or explicit JSON zero
- **WHEN** the component constructs its final consumer configuration
- **THEN** delivery is `all`, acknowledgement is `explicit`, and maximum delivery is `5`
- **AND** retained input published before consumer creation remains eligible for delivery

#### Scenario: Positive maximum delivery overrides the local default

- **GIVEN** a document or IoT JetStream input declares a positive `max_deliver`
- **WHEN** the component constructs its final consumer configuration
- **THEN** the positive value is preserved exactly
- **AND** zero is never treated as an override of the local value `5`

#### Scenario: Explicit delivery and acknowledgement declarations win independently

- **GIVEN** a document or IoT JetStream input declares valid delivery or acknowledgement policy
- **WHEN** the component constructs its final consumer configuration
- **THEN** each explicit value is preserved exactly
- **AND** a zero `max_deliver` still resolves to `5`

#### Scenario: Acknowledgement admission remains orthogonal

- **GIVEN** a document or IoT input declares positive or `-1` `max_ack_pending`
- **WHEN** component-specific empty and zero/default policies are applied
- **THEN** the exact acknowledgement-admission value reaches the final consumer request
- **AND** initial observation and lifecycle metrics remain governed by the existing policy contract
```

The proposal must state that this is a corrective follow-up to archived #963, not an amendment of archived history. The
active design must carry the class invariant, two-site census, adopter seam, and cold-start proof below.

## TDD plan

### Focused resolver tests

Write failing table tests in each affected package before the runtime correction. They must prove:

- omitted delivery and ack resolve to `all` and `explicit`;
- omitted and explicit-zero JSON `max_deliver` inputs both resolve to `5`, without a runtime distinction claim;
- positive explicit `max_deliver` values win exactly;
- explicit valid delivery and ack declarations win;
- zero `MaxAckPending` stays zero;
- positive and `-1` `MaxAckPending` forward unchanged;
- changing one field does not alter a sibling field; and
- invalid declarations still fail through canonical port resolution rather than being normalized by the helper.

The tests assert the returned final policy, not helper implementation details.

### Real-NATS cold-start proof

Add one discriminating integration test per affected processor:

1. Create the real input and output streams.
2. Publish a uniquely identified raw document or sensor message before the processor starts.
3. Start the actual component with an input port that omits delivery and redelivery fields.
4. Bind an output observer before or with a replay-safe policy so observation itself cannot lose the result.
5. Wait on message delivery, consumer state, or an explicit channel with a bounded context; use no arbitrary sleep.
6. Assert the first retained raw message is transformed and published.
7. Inspect `ConsumerInfo` and assert delivery `all`, ack `explicit`, max delivery `5`, and requested/effective
   `MaxAckPending` behavior.

The test must fail against `809f1807` because `DeliverNew` excludes the pre-start message. It must not rely on
concurrent component scheduling, file-loader timing, entity-count thresholds, or a longer polling timeout.

## Contract and schema proof

The correction changes no public port spelling or schema constraint. Verification must still prove that claim:

- run the focused port and component contract tests;
- run `task schema:generate` and require no schema drift;
- run `go test ./test/contract/...`;
- validate the new OpenSpec change and all current specs/changes with `--strict`; and
- confirm the only current spec delta is the added `jetstream-consumer-policy` requirement after archive preview.

Any generated schema or unrelated current-spec change is a stop condition, not expected output.

## Verification gates

Before handoff for independent implementation review:

```text
go test -race -count=1 ./examples/processors/document ./examples/processors/iot_sensor
go test -tags=integration -race -count=1 -timeout=10m \
  ./examples/processors/document ./examples/processors/iot_sensor
task lint
go test -race ./...
task schema:generate
git diff -- schemas/ specs/
go test ./test/contract/...
openspec validate preserve-example-consumer-defaults --strict
openspec validate --all --strict
task e2e:structural
task e2e:statistical
task e2e:semantic
git diff --check
```

Statistical E2E is the direct regression gate. Structural and semantic exercise the same E2E-only processors and their
other shipped tier configurations. E2E runs are serial and must inspect authoritative component/NATS state before owned
stack teardown on failure.

## Adopter seam

The adopter supplies only intent. Empty delivery and ack retain replay-safe component behavior. Empty or explicit-zero
`max_deliver` selects five, while a positive value overrides it. The adopter is never asked to predict component startup
order or add a sleep so a consumer might bind first.

Startup policy observation remains the explanation surface for `MaxAckPending`. The delivery/redelivery fallback is
documented in component configuration and the capability spec; no new knob or status plane is introduced.

## Stop gates

Stop implementation or review if any of these occurs:

- the affected caller census is not exactly document plus IoT;
- an independent inventory review does not return `INVENTORY PASS` for the recorded body hash;
- an independent design review does not approve the recorded design body hash;
- implementation changes global `GetConsumerConfig`, `consumerConfigFromFacts`, or `buildConsumerConfig` behavior;
- implementation edits configs merely to compensate for a component omission regression;
- an explicit valid delivery or ack declaration does not win exactly;
- omitted or explicit-zero `max_deliver` does not resolve to `5`;
- a positive explicit `max_deliver` does not win exactly;
- implementation or tests claim runtime facts distinguish omitted from explicit-zero `max_deliver`;
- restoring delivery or redelivery changes `MaxAckPending` request or observation;
- a test uses `time.Sleep` or depends on scheduler order;
- the pre-start real-NATS message is not the discriminating proof;
- schema generation changes checked-in schemas;
- OpenSpec archive preview modifies or removes an existing requirement or scenario;
- any accepted #963 proposal or archived artifact changes byte identity;
- statistical E2E is not green; or
- the final independent implementation review has unresolved findings.

## Review and handoff

Independent review must first validate the inventory identity and return `INVENTORY PASS`. It must then review this
exact design identity for scope, class invariant, OpenSpec delta, deterministic cold-start proof, adopter seam, and
gates.

Only after both reviews pass may the developer create the active OpenSpec change and implement the two local resolvers
with TDD. Every nontrivial implementation diff then requires an independent SemStreams reviewer before integration.

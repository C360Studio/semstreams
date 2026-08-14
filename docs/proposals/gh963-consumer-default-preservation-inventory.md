# GitHub #963 consumer-default preservation inventory

Baseline: `1002a0513a8aab3dd4d28faf4f0ee6c8d50a49ab`

Current: `809f1807958e6be416f61f3e75d114d193a39d29`

Status: `independent inventory review passed; correction implemented and verified`

Body SHA-256: `3e48022be1b262518fd75de78df04bd3323abd7fb3307039e52466f582fb3d68`

Hash method: `sed -n '/^## Inventory body$/,$p' <file> | tail -n +2 | shasum -a 256`

## Inventory body

# GitHub #963 consumer-default preservation correction inventory

## Scope and authority

This inventory measures the class-level default regression discovered after the accepted GitHub #963 implementation.
It does not reopen or rewrite the archived `honor-jetstream-max-ack-pending` change. The accepted #963 inventory,
design, and archive remain historical authority for that completed change.

The owner has ruled the target fallback behavior. This artifact records the measured surface for independent review;
it is not implementation authority until that inventory review passes.

No repository mutation beyond this new proposal artifact, test execution, Docker operation, or sister-repository access
was used to produce the inventory.

## Trigger and discriminating evidence

PR #968 statistical E2E passed once at `4c7bb4af` and then failed twice at amended head `809f1807`. The only change
between those heads is `output/otel/component_terminal_integration_test.go`; the built production program is identical.
The repeated failure therefore exposed a timing-sensitive behavior already present in the first head.

The failed `verify-entity-count` stage exhausted its 30-second validation budget while checking this required set:

- `c360.logistics.content.document.operations.doc-ops-001`
- `c360.logistics.environmental.sensor.temperature.temp-sensor-001`
- `c360.logistics.maintenance.work.completed.maint-001`

That diagnostic does not prove all three were absent. `verifyCriticalEntities` returns on the first failed lookup at
`test/e2e/scenarios/validate_entity.go:216-223`, while `validateEntityLoadResult` prints the complete required set at
`:225-236`. Because the document ID is first, it is the one definite missing entity from the error text.

Waiting longer cannot recover an event excluded by a `DeliverNew` consumer start sequence. This is not a slow graph
ingest or insufficient timeout mechanism.

## Exact affected sites

Exactly two call sites changed their class fallback as part of #963:

| Site | Baseline | Current | Regression |
|---|---|---|---|
| Document | `all`, `explicit`, `5` | Empty/zero becomes `new`, `explicit`, `3` | Retained raw documents can be lost |
| IoT sensor | `all`, `explicit`, `5` | Empty/zero becomes `new`, `explicit`, `3` | Retained raw sensors can be lost |

The exact current sites are `examples/processors/document/component.go:356-372` and
`examples/processors/iot_sensor/component.go:356-372`. The baseline lines are `:357-367` in both files at `1002a051`.
The current extractor defaults are defined at
`component/port_jetstream.go:93-115`, and `natsclient.buildConsumerConfig` maps `new` to
`jetstream.DeliverNewPolicy` at `natsclient/stream.go:463-475`.

The statistical JSON text omits `deliver_policy`, `ack_policy`, and `max_deliver` at
`configs/statistical.json:258-269,295-306`. At runtime, `max_deliver` is zero for both omission and explicit JSON zero;
the immutable facts do not and need not distinguish those encodings.

## Cold-start mechanism

`ComponentManager` starts all non-store providers in one concurrent phase at
`service/component_manager.go:538-563,566-605`. File inputs return from `Start` immediately after spawning their read
goroutine at `input/file/file.go:379-408`. Each reader publishes a line before applying its configured 10 ms interval at
`:524-575`.

The document and IoT processors concurrently perform stream lookup, consumer creation, policy observation, and consume
startup. A newly created `DeliverNew` consumer excludes raw records published before its creation. The old `DeliverAll`
fallback replayed those retained records. The two outcomes are scheduler-dependent even though both are deterministic
JetStream semantics.

#963 policy observation is not the selection cause. Observation runs after `CreateOrUpdateConsumer` at
`natsclient/stream.go:374-403` and `natsclient/consumer_policy.go:132-158`. It cannot change the start sequence already
chosen during consumer creation. The regression is the caller's fallback change from `all` to `new`.

## Exhaustive current caller proof

The non-test, non-documentation search for `GetConsumerConfig(` has 17 runtime callers. The two rows above are the only
callers whose delivery and redelivery defaults changed from baseline. The other 15 are unaffected by this correction.

| Current caller | Why it is outside the correction |
|---|---|
| `output/file/file.go:343` | Existing defaults remain; #963 adds admission forwarding and owner context. |
| `output/httppost/httppost.go:348` | Same: existing delivery defaults are unchanged. |
| `output/otel/component.go:256` | Still fixes delivery and ack directly; extraction supplies admission only. |
| `output/websocket/websocket.go:933` | Already used canonical defaults; no fallback change. |
| `processor/agentic-dispatch/component.go:1071` | Per-port policies remain fixed; extraction carries admission. |
| `processor/agentic-governance/component.go:412` | Already used canonical defaults; no fallback change. |
| `processor/agentic-loop/component.go:955` | Fixed component admission remains; nonzero declaration is rejected. |
| `processor/agentic-model/component.go:389` | Component-owned admission remains fixed at 1; declaration is rejected. |
| `processor/agentic-tools/component.go:368` | Component-owned admission remains fixed at 3; declaration is rejected. |
| `processor/graph-ingest/component.go:1374` | Already restores omitted delivery to `all` at `:1378-1380`. |
| `processor/json_filter/json_filter.go:305` | Already used canonical defaults; no fallback change. |
| `processor/json_generic/json_generic.go:282` | Already used canonical defaults; no fallback change. |
| `processor/json_map/json_map.go:327` | Already used canonical defaults; no fallback change. |
| `processor/rule/processor.go:1113` | Already used canonical defaults; no fallback change. |
| `storage/objectstore/component.go:762` | Already restores omitted delivery to `all` at `:777-779`. |

`component.GetConsumerConfig` itself is the shared definition, not an eighteenth caller. The correction must not alter
that global default because every unaffected ordinary consumer currently relies on its `new`, explicit-ack, three-try
semantics. `natsclient.buildConsumerConfig` is likewise a final carrier and must not gain component-specific policy.

## Registration and configuration reach

The two processors are registered only by the E2E binary at `cmd/e2e-semstreams/main.go:745-758`; they are not part of
the core production registry. Their declarations occur in these shipped reference configurations:

| Configuration | Components present |
|---|---|
| `configs/e2e-structural.json` | document and IoT |
| `configs/hello-world.json` | IoT only |
| `configs/semantic-8b.json` | document and IoT |
| `configs/semantic-frontier.json` | document and IoT |
| `configs/semantic.json` | document and IoT |
| `configs/statistical.json` | document and IoT |
| `configs/structural.json` | document and IoT |

No configuration edit is required. Local empty/zero fallback preserves every existing declaration. Explicit valid
delivery and ack values win, and only a positive explicit `max_deliver` overrides the local value of five.

## Existing patterns and specification gap

Graph ingest and ObjectStore already implement the same class pattern: extract canonical fields, then replace only an
omitted delivery policy with their idempotent replay-safe `all` fallback. Their comments explicitly name the startup
first-message race at `processor/graph-ingest/component.go:1368-1380` and
`storage/objectstore/component.go:756-779`.

Current `openspec/specs/jetstream-consumer-policy/spec.md` governs acknowledgement admission and policy observation. It
does not state how component-specific delivery and redelivery fallbacks survive canonical extraction. A focused added
requirement belongs there because the correction preserves the same final consumer-policy construction seam without
changing global port grammar.

The only active OpenSpec change is `semantic-tier-split`; no active change currently owns this correction.

## Adopter seam inventory

Specific adopter: a developer using the document or IoT example processor in a flow, who has not read either component
implementation.

- What must they know? Only explicit JetStream fields they intentionally choose. `max_deliver: 0` has the same local
  result as leaving it out: five attempts. A positive value is the only max-delivery override.
- What happens if they do nothing? Retained raw messages are replayed with delivery `all`, explicit ack, and five
  delivery attempts, exactly as before #963. `MaxAckPending` remains omitted and NATS owns its effective default.
- Where do they find out? Component configuration describes the local zero/default and positive override. Runtime policy
  observation reports requested and effective `MaxAckPending`.
- What should they have to know? Nothing about component startup scheduling, `GetConsumerConfig`, consumer creation
  timing, or NATS start-sequence internals.

The framework must observe explicit declarations, not ask the adopter to predict whether a file input can publish before
the processor consumer binds.

## Class invariant

Canonical extraction validates and carries port policy. It does not erase a component's established local default.
For the document and IoT processor class:

- omitted `deliver_policy` means `all`;
- omitted `ack_policy` remains `explicit`;
- zero `max_deliver`, whether omitted or explicitly encoded as zero, means `5`;
- only positive explicit `max_deliver` values override `5`;
- explicit valid delivery and ack declarations win for their own fields; and
- `MaxAckPending` is orthogonal and independently forwards exactly as accepted by #963.

No field's zero/default or positive declaration may silently change another field's effective policy.

## Owner ruling recorded for design

The owner approved the class invariant above and explicitly prohibited a global `GetConsumerConfig` or
`buildConsumerConfig` change. The correction is local to the two affected component consumer builders.

## Reproducible searches

```text
git diff 1002a051..809f1807 -- \
  examples/processors/document/component.go \
  examples/processors/iot_sensor/component.go

rg -n --glob '!**/*_test.go' --glob '!docs/**' --glob '!openspec/**' \
  'GetConsumerConfig\(' --type go .

rg -n '"name": "(document_processor|iot_sensor)"' configs --glob '*.json'

git diff 4c7bb4af..809f1807 --name-status
```

The first command proves the two default changes. The second returns the exhaustive 17-caller class. The third returns
the seven configuration files above. The fourth returns only the OTEL integration test amendment.

## Review handoff

Independent inventory review must verify:

- baseline and current identities;
- the exact two affected sites and 15 unaffected caller rows;
- the concurrent cold-start mechanism;
- the diagnostic's required-set versus actual-missing limitation;
- the seven-config and E2E-only registration reach;
- the existing graph-ingest and ObjectStore omission-fallback precedents;
- the adopter seam and class invariant; and
- byte identity of the accepted #963 inventory, design, and archived change.

Stop for `INVENTORY PASS`. No implementation or active OpenSpec mutation is authorized by this artifact.

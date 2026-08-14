# Design

## Existing surface inventory

| Surface | Existing owner and premise | Consumer | Change |
|---|---|---|---|
| `processor/agentic-tools/component.go:341` | `agentic-tools` creates the durable `tool.execute` consumer and delegates settlement to heartbeat | JetStream delivery | Return the real handler disposition; remove the void-success adapter |
| `processor/agentic-tools/component.go:465` | `handleToolCall` owns decode, admission, execution, and result publication | tool callers | Add pre-read, immutable completion, replay, and permanent poison handling |
| `processor/agentic-tools/component.go:984` | result publication resolves the declared output subject and publishes | agentic loop | Use synchronous deterministic-message-ID publication |
| `natsclient/heartbeat.go:49` | heartbeat owns ACK/NAK/Term and preserves long work | component consumers | Surface every settlement failure without changing shutdown timing |
| `graph/kvcatalog.go:103` | one central catalog declares framework KV ownership and retention | owners, readers, generic write guard | Add the outcomes owner row; derive collision protection automatically |
| `natsclient/kvspec.go:226` | owner acquisition creates/adopts, reconciles, and verifies declared KV policy | framework bucket owners | Reuse unchanged |
| `natsclient/test_client.go:500` | canonical repository-owned NATS integration substrate already owns the broker `maxPayload` fixture setting | external integration tests | Promote the existing package-local option as `WithTestMaxPayload`; non-positive/default behavior is unchanged |
| `agentic/tools.go:207` | `ToolCall` defines all request identity fields | dispatch and executors | Fingerprint every ordered V1 field |
| `component/dependencies.go:43` | exported executor dependency contract | component implementations | Document downstream `ToolCall.ID` idempotency obligation |

There was no current completed-outcome store, no replay consumer, and no predictive tool-result size knob to retire.
`natsclient.MaxPayload` exists but this design deliberately does not consult it. Bounds are learned only from the real
Create or publish attempt.

## Adopter seam inventory

The adopter is a developer outside this repository implementing `ToolExecutor`.

- **What must they know?** If their executor can cause an external effect, it must pass `ToolCall.ID` as the
  downstream idempotency key.
- **What happens if they do nothing?** A crash or transient KV Create failure after the effect but before COMPLETED
  persistence can redeliver and repeat that effect. The framework cannot infer whether it happened.
- **Where do they find out?** The exported `component.ToolRegistryReader.Execute` and
  `agentictools.ToolExecutor` documentation.
- **What should they have to know?** Nothing about bucket names, keys, subjects, fingerprints, payload ceilings,
  retries, or result message IDs. Those are framework-owned observations and mechanics.

## Options considered

1. **COMPLETED-only immutable KV (selected).** Small state machine, safe authoritative replay, honest ambiguity.
2. **Claim/lease before execution.** Predicts liveness and requires takeover/expiry policy; rejected for this change.
3. **Result stream as the ledger.** Requires consumer-side search/replay and does not provide atomic key collision
   detection; rejected.
4. **Predict payload size before acting.** Makes callers predict a transport fact they do not own; rejected. Observe
   the real typed rejection instead.

## Ruling to implementation

| Owner ruling | Implementation |
|---|---|
| Central owner-created KV, H1/R1/no lifecycle/unlimited bytes | policy at `graph/kvcatalog.go:40`, declaration at `graph/kvcatalog.go:111`, owner acquisition before consumers at `processor/agentic-tools/component.go:181` |
| Opaque V1 key and complete ordered fingerprint | key at `processor/agentic-tools/outcomes.go:66`; V1 ordered fingerprint at `processor/agentic-tools/outcomes.go:83` |
| COMPLETED-only pre-read/replay and collision/corruption Term | record at `processor/agentic-tools/outcomes.go:21`; pre-read at `processor/agentic-tools/component.go:499`; validation and Term at `processor/agentic-tools/component.go:604` |
| Create-CAS winner is authoritative | CAS and winner read at `processor/agentic-tools/component.go:643`; real-NATS proof at `processor/agentic-tools/outcomes_integration_test.go:101` |
| Deterministic MsgID and PubAck before request ACK | ID at `processor/agentic-tools/outcomes.go:75`; synchronous publication at `processor/agentic-tools/component.go:984`; handler result feeds settlement at `processor/agentic-tools/component.go:341`; real-NATS publication-failure/restart/replay/ACK proof at `processor/agentic-tools/outcomes_integration_test.go:229` |
| Approval-required is nonterminal with a phase-distinct MsgID | phase ID at `processor/agentic-tools/outcomes.go:79`; nonterminal path at `processor/agentic-tools/component.go:527`; loop-to-tools proof at `processor/agentic-loop/approval_integration_test.go:54` |
| Storage oversize creates compact authority; publication oversize uses one compact transport surrogate | observed classification at `processor/agentic-tools/outcomes.go:170`; storage fallback at `processor/agentic-tools/component.go:674`; publication fallback at `processor/agentic-tools/component.go:699` |
| Panic recovery and downstream idempotency obligation | recovery at `processor/agentic-tools/component.go:587`; exported obligations at `component/dependencies.go:52` and `processor/agentic-tools/executor.go:13` |
| Exact bounded telemetry | vocabularies at `processor/agentic-tools/metrics.go:11`; five families at `processor/agentic-tools/metrics.go:108`; exact-vocabulary proof at `processor/agentic-tools/outcome_metrics_test.go:15` |

## Owner clarification and deviations

The owner clarification of 2026-08-12 resolves the earlier publication-only oversize tension: the full immutable
COMPLETED record remains authoritative, while the rejected delivery receives exactly one compact transport-surrogate
publication using the same call-derived message ID. Redelivery retries full then compact. Storage-side oversize still
stores compact authority because no full record exists.

The typed observed set is `nats.ErrMaxPayload`, `jetstream.ErrMaxBytesExceeded`, and JetStream API error code 10054.
No error-text matching or #857 general payload-classification expansion is introduced.

The owner approved the exact test-only exported surface `natsclient.WithTestMaxPayload(int64) TestOption` so external
integration tests can use the canonical substrate rather than direct container APIs. It is not production runtime
configuration; callers that omit it, or pass a non-positive value, retain the server default.

There are no implementation deviations from the owner ruling or clarification.

## Verification evidence

- Focused race gate: `go test -race ./processor/agentic-tools ./natsclient ./graph
  ./processor/agentic-loop ./test/e2e/scenarios/agentic` — green on 2026-08-12.
- Real-NATS tool outcomes: `go test -race -tags=integration ./processor/agentic-tools -run
  'TestIntegration(ConcurrentReplicas|LowMaxPayload|AckFailureRestart|PostEffectCreateFailure|ExecutorPanic)'
  -count=1` — green on 2026-08-12.
- Real-NATS post-COMPLETED publication failure and restart replay: `go test -race -tags=integration
  ./processor/agentic-tools -run TestIntegrationResultPublishFailureRestartReplaysStoredOutcome -count=1` — green on
  2026-08-12.
- Real-NATS approval protocol: `go test -race -tags=integration ./processor/agentic-loop -run
  TestIntegration_ApprovalFlow_Approve -count=1` — green on 2026-08-12.
- Real-NATS settlement paths: `go test -race -tags=integration ./natsclient -run
  'TestIntegrationConsumeWithHeartbeat(AckFailureLeavesDeliveryForRedelivery|ShutdownDelayedNAKRedelivers|
  FailureLeavesDeliveryUnsettled)' -count=1` — green on 2026-08-12.
- An independent reviewer reported `task e2e:agentic` green after the owner clarification and approval-protocol
  correction. The parent-owned final rerun remains pending, so the final gate stays unchecked.

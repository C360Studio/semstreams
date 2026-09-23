# #1249 `tasks.md` — pin substitutions at `b7ce8727` (tasks are NOT rewritten here; apply these when editing)

Rule: an unmarked task pin is unchanged because its file is byte-identical to `0053183d` (`git diff --stat` empty, see
`inventory.md` § Searches). "→" gives the `main` line; the appendix prints every cited line with `sed -n "${n}p"`.

## Header and § 1

- Header paragraph: "the three files that moved at L1 head `c2a9cef6` … carry `base :n (L1 :m)`. The developer stands
  at the L1 head or above" → "every pin is a `main` line at `b7ce8727`; the L1 attributions are retired".
- 1.1 (stale L1-branch wording) restated for a `main` base: **"The claim exists: draft PR #1360 against `main` (base
  `b7ce8727`) with `Closes #1249`, `Closes #759`, and `implemented-by: pending (design-phase claim)`. Before the first
  implementation push, set `implemented-by: <persona>` in the body and keep the Tier 1 declaration (§ 7, wording per
  reconciliation B4: one added incompatible line under the already-counted `natsclient`, package count unchanged at 15).
  There is no L1 branch to target: L1 is `94cd8e4c` on `main`."**
- 1.2 `agentic/agentrun/agentrun.go:812` unchanged; the expected result on `main` is `:812` plus the declaration
  `natsclient/heartbeat.go:84` (the grep matches `func ConsumeWithHeartbeat(`); anything else stops.

## § 2 — unchanged

- 2.1 `internal/agentterminal/terminal.go:67`, `agentrun.go:586`, `test/compat/semteams/agentrun_terminal_compat_test.go:75`.
- 2.2 `agentrun.go:490`.

## § 3

- 3.1 `agentrun.go:810-819` unchanged. `consumeAdmittedDelivery` no longer exists → `deliverylane.Consume`
  (`internal/deliverylane/deliverylane.go:105`) under the `admitted` guard (reconciliation B1); `ValidateHeartbeatDeliveryPolicy`
  at `natsclient/delivery_settlement.go:155`; `DeliveryWork` at `:35` (same signature).
- 3.2 `pkg/lifecycle/manager.go:205`, `:244`, `:169`, `:198`, `:250`; `pkg/errs/errs.go:280`; `agentrun.go:637`, `:641`,
  `:653`, `:657` — unchanged.
- 3.3 `pkg/errs/errs.go:435`; `agentrun.go:393`, `:405`, `:436`, `:455`, `:462`; `agentic/agentrun/nats_reader.go:68` — unchanged.
- 3.4 `agentrun.go:646-647` unchanged; `natsclient/delivery_settlement.go` base `:392`/`:396` (L1 `:414`/`:418`) → `:403`/`:407`.
- 3.5 `agentrun.go:605`, `:612` unchanged. 3.8 `:586` unchanged.

## § 4 — superseded by the 2026-09-19 amendment; substitutions

- 4.1 STRUCK as written: no `agentic/agentrun/delivery_owner.go`; `processor/agentic-loop/delivery_owner.go:29-63`, `:74-86`,
  `:65`, `:88`, `:99` — file deleted at `b7ce8727` (#1357). Substitute: per lane `deliverylane.NewAdmission(s.recordDeliveryOwnerFatal, nil)`
  (`deliverylane.go:45`), `deliverylane.Consume` (`:105`), `deliverylane.NewBinding(handle)` (`:194`),
  `deliverylane.Observe(runCtx, binding, admission, react)` (`:225`, `react` log-only, non-nil). `recordDeliveryOwnerFatal`
  keeps its `*MilestoneSubscriber` receiver as the `onFatal` feeding `DeliveryFatal()`.
- 4.2 `agentrun.go:679-689`, `:712-713`, `:718`, `:727` unchanged lines, superseded text: `complete`/`failed` (`:681-682`)
  and `completeDrained`/`failedDrained` (`:683-684`) become two `*deliverylane.Binding`; no observer goroutine is
  hand-rolled (`Observe` owns it); `stop()` = `Drain()` both → await both `Closed()` (`:727`, `:730`) → `o.cancel()` (`:743`)
  → join both `Done()` (`deliverylane.go:216`) — reconciliation B3. The force `Stop()` fallback (`:734-737`) is owner
  question OQ2. Tests as named (`-race`).
- 4.3 `natsclient/delivery_settlement.go` base `:361`/`:366` (L1 `:383`/`:388`) → `:372`/`:377`; base `:379` (L1 `:401`) → `:390`.

## § 5 — unchanged

- 5.1 `service/base.go:209`; `service/milestone_service.go:21-22`; `service/service_manager.go:1302`, `:1722`, `:1736`.
- 5.2 `metric/registry.go:216`; `agentrun.go:521`; `cmd/semstreams/main.go:347`, `:170`; `cmd/e2e-semstreams/main.go:272`,
  `:159`; `service/base.go:389`.

## § 6

- 6.1 `agentrun.go:832`, `:833` unchanged; `natsclient/delivery_settlement.go:191` → `:181`.
- 6.2 `internal/maxdelivery/observer.go:148` unchanged.

## § 7

- 7.1 `natsclient/heartbeat.go:79` → `:84`; `:37` unchanged; "`Deprecated:` notice (`:75`) if present" → there is no
  marker on `main`; the doc comment `:75-83` (names #1249 as the deleting PR) is deleted with the function; `:18` unchanged.
- 7.2 `natsclient/consumer_policy_callsite_test.go:426` → `:428`; `:444` → `:446`; `:395` unchanged; `:423` → `:425`.
- 7.3 `heartbeat_test.go` 407 lines, `heartbeat_integration_test.go` 133 — unchanged.
- 7.4 `scripts/api-compat.sh:174` unchanged; the PR-body claim changes per reconciliation B4 (15 packages at HEAD,
  `natsclient` already counted; this layer adds the `ConsumeWithHeartbeat: removed` line).
- 7.5 `storage/objectstore/component_ack_integration_test.go:41` unchanged; `processor/agentic-tools/outcomes_integration_test.go:202` → `:216`.

## § 8

- 8.1 MODIFIED `jetstream-consumer-policy` "against L0's head text (`759bd596`, `:73-103`)" → against the LIVE spec
  `openspec/specs/jetstream-consumer-policy/spec.md:380-413` (three scenarios, names identical). REMOVED `nats-streaming`
  "(L0 `:61-77`)" → live `openspec/specs/nats-streaming/spec.md:239-255`. ADD a second REMOVED: "Heartbeat consumption
  SHALL expose settlement failure", live `:158-181` (`:169` names this PR) — reconciliation B5. The sentence "expected
  red until L0's blocks are in the tree" is void: L0's blocks are in the tree; PR #1360 reports `--strict` 56/56 at seed.
- 8.3 `docs/operations/migration-beta162-to-beta163.md:1050-1065` → `:1216-1231` (rewrite from `:1226`);
  `docs/operations/migration-restart-safe-nats-client.md:95-97` → `:104-108`; `docs/concepts/33-semantic-settlement.md:99` → `:107-108`.
- 8.4 O5 is not yet recorded on #1155 (latest comment 2026-09-14) — coordinator action, not a task edit.

## § 9, § 10

- 9.1/9.2 no pins; boot root shape unchanged (`cmd/e2e-semstreams/main.go:261`, `:272`); `verify-streaming-metrics` stage
  exists (`test/e2e/scenarios/agentic/scenario.go:249`).
- 10.2 `docs/contributing/02-e2e-tests.md:299` unchanged.

## Appendix — every cited line at `b7ce8727`, printed by `sed -n "${n}p"` (generated, not transcribed)

- `agentic/agentrun/agentrun.go:812` — `handleErr := natsclient.ConsumeWithHeartbeat(msgCtx, msg, 10*time.Second, func(workCtx context.Context) error {`
- `internal/agentterminal/terminal.go:67` — `SourceMessageID string`
- `agentic/agentrun/agentrun.go:586` — `Role:        normalized.Role,`
- `test/compat/semteams/agentrun_terminal_compat_test.go:75` — `if capture.events[i].Category != wantCategories[i] || capture.events[i].Outcome != wantOutcomes[i] {`
- `agentic/agentrun/agentrun.go:490` — `OnLoopTerminal(ctx context.Context, ev LoopTerminalEvent, run *AgentRun) error`
- `agentic/agentrun/agentrun.go:810` — `handleMsg := func(subject string) func(ctx context.Context, msg jetstream.Msg) {`
- `agentic/agentrun/agentrun.go:819` — `s.logger.Warn("agentrun: MilestoneSubscriber: HandleEvent error",`
- `internal/deliverylane/deliverylane.go:105` — `func Consume(`
- `natsclient/delivery_settlement.go:155` — `func ValidateHeartbeatDeliveryPolicy(`
- `natsclient/delivery_settlement.go:35` — `type DeliveryWork func(context.Context, []byte) (DeliveryDecision, error)`
- `pkg/lifecycle/manager.go:205` — `return nil, 0, fmt.Errorf("%w: entity_id=%q", ErrEntityNotFound, entityID)`
- `pkg/lifecycle/manager.go:244` — `return nil, 0, fmt.Errorf("%w: workflow=%q entity_id=%q (no %s triple)",`
- `pkg/lifecycle/manager.go:169` — `return nil, fmt.Errorf("%w: %q", ErrWorkflowNotRegistered, workflow)`
- `pkg/lifecycle/manager.go:198` — `if m.exactReader == nil {`
- `pkg/lifecycle/manager.go:250` — `return nil, 0, fmt.Errorf("lifecycle: project entity %q (workflow %q): %w",`
- `pkg/errs/errs.go:280` — `return ErrorTransient`
- `pkg/errs/errs.go:435` — `func WrapInvalid(err error, component, method, action string) error {`
- `agentic/agentrun/agentrun.go:637` — `return nil, nil //nolint:nilerr // deliberate: non-run loops have no run entity`
- `agentic/agentrun/agentrun.go:641` — `return nil, fmt.Errorf("Manager.Get returned unexpected type %T", participant)`
- `agentic/agentrun/agentrun.go:653` — `if errors.Is(err, lifecycle.ErrEntityNotFound) {`
- `agentic/agentrun/agentrun.go:657` — `return nil, err`
- `agentic/agentrun/agentrun.go:393` — `loopEntityID, err := agentic.TryLoopExecutionEntityID(org, platform, loopID)`
- `agentic/agentrun/agentrun.go:405` — `if err != nil {`
- `agentic/agentrun/agentrun.go:436` — `runEntityID, err := agentic.TryChainExecutionEntityID(org, platform, currentLoopID)`
- `agentic/agentrun/agentrun.go:455` — `return nil, fmt.Errorf("agentrun.ResolveRun: ancestry walk hop %d: parent entity %q is not a loop-execution entity ID; cannot continue walk",`
- `agentic/agentrun/agentrun.go:462` — `return nil, fmt.Errorf("agentrun.ResolveRun: ancestry walk exceeded %d hops without reaching root for loop %q", maxAncestryHops, loopID)`
- `agentic/agentrun/nats_reader.go:68` — `return "", false, fmt.Errorf("agentrun: NATSLoopTripleReader: predicate %q on entity %q has non-string value %T", predicate, entityID, val)`
- `agentic/agentrun/agentrun.go:646` — `if ev.LoopID == "" {`
- `agentic/agentrun/agentrun.go:647` — `return nil, nil`
- `natsclient/delivery_settlement.go:403` — `valid = work.cause == nil`
- `natsclient/delivery_settlement.go:407` — `if !valid {`
- `agentic/agentrun/agentrun.go:605` — `if r := recover(); r != nil {`
- `agentic/agentrun/agentrun.go:612` — `if handlerErr := handler.OnLoopTerminal(ctx, ev, run); handlerErr != nil {`
- `internal/deliverylane/deliverylane.go:45` — `func NewAdmission(`
- `internal/deliverylane/deliverylane.go:194` — `func NewBinding(handle jetstream.ConsumeContext) *Binding {`
- `internal/deliverylane/deliverylane.go:225` — `func Observe(`
- `internal/deliverylane/deliverylane.go:207` — `func (b *Binding) Drain() { b.drainOnce.Do(b.handle.Drain) }`
- `internal/deliverylane/deliverylane.go:211` — `func (b *Binding) Closed() <-chan struct{} { return b.handle.Closed() }`
- `internal/deliverylane/deliverylane.go:216` — `func (b *Binding) Done() <-chan struct{} { return b.done }`
- `internal/deliverylane/deliverylane.go:240` — `react(result)`
- `internal/deliverylane/deliverylane.go:241` — `binding.Drain()`
- `agentic/agentrun/agentrun.go:679` — `type milestoneConsumerOwner struct {`
- `agentic/agentrun/agentrun.go:681` — `complete        jetstream.ConsumeContext`
- `agentic/agentrun/agentrun.go:682` — `failed          jetstream.ConsumeContext`
- `agentic/agentrun/agentrun.go:683` — `completeDrained bool`
- `agentic/agentrun/agentrun.go:684` — `failedDrained   bool`
- `agentic/agentrun/agentrun.go:689` — `}`
- `agentic/agentrun/agentrun.go:712` — `drainComplete := complete != nil && !o.completeDrained`
- `agentic/agentrun/agentrun.go:713` — `drainFailed := failed != nil && !o.failedDrained`
- `agentic/agentrun/agentrun.go:718` — `// Both running handles begin Drain before either exact Closed wait.`
- `agentic/agentrun/agentrun.go:727` — `stopErrors = append(stopErrors, waitMilestoneConsumerClosed(ctx, complete.Closed(), "complete"))`
- `agentic/agentrun/agentrun.go:734` — `// Running Stop is terminal. Force local closure best-effort and never`
- `agentic/agentrun/agentrun.go:737` — `complete.Stop()`
- `agentic/agentrun/agentrun.go:743` — `o.cancel()`
- `agentic/agentrun/agentrun.go:806` — `runCtx, cancel := context.WithCancel(ctx)`
- `natsclient/delivery_settlement.go:372` — `if err := msg.InProgress(); err != nil {`
- `natsclient/delivery_settlement.go:377` — `result.ownerStopNeeded = true`
- `natsclient/delivery_settlement.go:390` — `func unavailableDeliveryMetadata(cause error) DeliveryResult {`
- `service/base.go:209` — `// Services that embed BaseService can override Health() for more detail`
- `service/milestone_service.go:21` — `type milestoneStarter interface {`
- `service/milestone_service.go:22` — `Start(ctx context.Context, client *natsclient.Client, cfg agentrun.StartConfig) (func(context.Context) error, error)`
- `service/service_manager.go:1302` — `mux.HandleFunc("/health", m.handleSystemHealth)`
- `service/service_manager.go:1722` — `func (m *Manager) handleSystemHealth(w http.ResponseWriter, _ *http.Request) {`
- `service/service_manager.go:1736` — `subStatuses = append(subStatuses, service.Health())`
- `metric/registry.go:216` — `func (r *MetricsRegistry) RegisterCounterVec(serviceName, metricName string, counterVec *prometheus.CounterVec) error {`
- `agentic/agentrun/agentrun.go:521` — `func NewMilestoneSubscriber(`
- `cmd/semstreams/main.go:347` — `agentrun.NewMilestoneSubscriber(`
- `cmd/e2e-semstreams/main.go:272` — `agentrun.NewMilestoneSubscriber(`
- `cmd/semstreams/main.go:170` — `metricsRegistry, phaseLogging, err := bootstrapobservability.NewProductionPhaseA(`
- `cmd/e2e-semstreams/main.go:159` — `metricsRegistry, phaseLogging, err := bootstrapobservability.NewE2EPhaseA(`
- `service/base.go:389` — `func (s *BaseService) RegisterMetrics(_ metric.MetricsRegistrar) error {`
- `agentic/agentrun/agentrun.go:832` — `MaxDeliver:    5,`
- `agentic/agentrun/agentrun.go:833` — `AckWait:       30 * time.Second,`
- `natsclient/delivery_settlement.go:181` — `ceiling := effective / 2`
- `internal/maxdelivery/observer.go:148` — `if err := registry.RegisterCounterVec("max-delivery-observer", "exhaustions", occurrences); err != nil {`
- `natsclient/heartbeat.go:84` — `func ConsumeWithHeartbeat(`
- `natsclient/heartbeat.go:37` — `func nonCancellationWorkError(err error) error {`
- `natsclient/heartbeat.go:75` — `// New bindings use ConsumeDeliveryWithHeartbeat, which returns a typed`
- `natsclient/heartbeat.go:83` — `// removal for adopters.`
- `natsclient/heartbeat.go:18` — `// this exact message. ConsumeWithHeartbeat terminates the JetStream delivery`
- `natsclient/consumer_policy_callsite_test.go:428` — `"natsclient/heartbeat.go": "func(ctx context.Context, msg jetstream.Msg, heartbeatInterval time.Duration, work func(context.Context) error) error",`
- `natsclient/consumer_policy_callsite_test.go:446` — `"agentic/agentrun/agentrun.go": 1,`
- `natsclient/consumer_policy_callsite_test.go:395` — `if violations := newDurableHandlerRetirementViolations(parseProductionGoFiles(t, root)); len(violations) == 0 {`
- `natsclient/consumer_policy_callsite_test.go:425` — `t.Fatalf("legacy ConsumeWithHeartbeat surface violations: %v", scan.violations)`
- `scripts/api-compat.sh:174` — `fail_count=$((n_incompatible + n_removed))`
- `storage/objectstore/component_ack_integration_test.go:41` — `// constant (natsclient.ConsumeWithHeartbeat), so redelivery-observing tests`
- `processor/agentic-tools/outcomes_integration_test.go:216` — `// ConsumeWithHeartbeat can ACK the request.`
- `openspec/specs/jetstream-consumer-policy/spec.md:380` — `### Requirement: semantic heartbeat settlement has one permanent exported surface`
- `openspec/specs/jetstream-consumer-policy/spec.md:413` — `- **AND** no raw message settlement or exported no-heartbeat interpreter is introduced`
- `openspec/specs/nats-streaming/spec.md:239` — `### Requirement: the legacy helper is a shrinking remainder, never a compatibility surface`
- `openspec/specs/nats-streaming/spec.md:255` — `- **AND** the recorded caller set is never widened, only reduced by the migrating PRs`
- `openspec/specs/nats-streaming/spec.md:158` — `### Requirement: Heartbeat consumption SHALL expose settlement failure`
- `openspec/specs/nats-streaming/spec.md:169` — `This requirement is deleted together with the helper by the PR that migrates its last caller (#1249).`
- `openspec/specs/nats-streaming/spec.md:181` — `- **THEN** the returned error chain contains context cancellation and the settlement failure`
- `docs/operations/migration-beta162-to-beta163.md:1226` — ``ConsumeWithHeartbeat` is still exported at this tag but is **removed without a deprecation period**: it is deleted`
- `docs/operations/migration-beta162-to-beta163.md:1231` — `a lane that does not want a heartbeat keeps owning its own `msg` settlement, as it does today.`
- `docs/operations/migration-restart-safe-nats-client.md:104` — ``ConsumeWithHeartbeat` is still exported while its last callers migrate — #1327 takes model and loop, #1249 takes`
- `docs/operations/migration-restart-safe-nats-client.md:108` — `use only the typed API above. (Owner ruling 2026-09-18 on #759.)`
- `docs/concepts/33-semantic-settlement.md:107` — `replacement proof. Model and loop work continues under #1146. AgentRun fanout needs its own design because one source`
- `docs/concepts/33-semantic-settlement.md:108` — `delivery currently fans out to multiple outward-facing handlers without a durable per-handler completion contract.`
- `docs/contributing/02-e2e-tests.md:299` — `Any commit or tag marked **BREAKING** in the changelog or commit message (a `!` after the type/scope) MUST have at`

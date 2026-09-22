# #1249 inventory — AgentRun milestone-fanout settlement (round 5: re-derived at `main` `b7ce8727` for implementation)

base: b7ce8727a770c9f880049def24abe887545bccbe

Base = `origin/main` at `b7ce8727` (#1357 merged; L0 `f4d66934`, L1 `94cd8e4c`, L2 #1335, L3 `3faca84f` are all on
`main`). [R5] Every pin below is a `main` line printed by `sed -n "${n}p"` from this tree; nothing is transcribed. The
round-1 base `0053183d` and the L1-head map are history: over the pinned files `git diff --stat 0053183d..b7ce8727`
touched 18 and deleted the three `delivery_owner.go` copies, and the seeded inventory read `pins=162 ok=117 moved=22
ambiguous=4 drift=19` at HEAD before this re-derivation (§ Searches). `agentic/agentrun/`, `internal/agentterminal/`,
`service/`, both `cmd/` roots, `pkg/lifecycle`, `pkg/errs`, `metric`, `config/streams.go`, `internal/maxdelivery`,
`release/tier1-packages.txt`, and `scripts/api-compat.sh` are byte-identical to the round-1 base. Read-only; sisters
via `git -C` only. One pin per bullet; commentary in prose. Round tags [R2], [R3], [R4], [R5].

## Problem statement

AgentRun owns the last production caller of `natsclient.ConsumeWithHeartbeat` at the base (owner 2026-09-18 on #759:
no deprecation; the last-caller PR removes the helper without alias and declares `Closes #759`). The 2026-09-02 ruling
(#759 items 3, 6) forbids a mechanical typed conversion. The fanout publishes nothing; its outputs are product
`MilestoneHandler` calls, and every production composition registers zero handlers.

## 1. The claimed gap

Claim A, "the legacy transport can ACK a partially completed fanout": true. Handler error → WARN and continue; panic →
ERROR and continue; `HandleEvent` returns nil; the helper ACKs nil. Every `HandleEvent` error is wrapped permanent.
- `agentic/agentrun/agentrun.go:812` — `handleErr := natsclient.ConsumeWithHeartbeat(msgCtx, msg, 10*time.Second, func(workCtx context.Context) error {`
- `agentic/agentrun/agentrun.go:814` — `return natsclient.TerminateDelivery(err)`
- `agentic/agentrun/agentrun.go:612` — `if handlerErr := handler.OnLoopTerminal(ctx, ev, run); handlerErr != nil {`
- `agentic/agentrun/agentrun.go:605` — `if r := recover(); r != nil {`
- `agentic/agentrun/agentrun.go:621` — `return nil`
- `natsclient/heartbeat.go:145` — `return executeTerminalMethod(msg, terminalMethodAck, 0)`

Claim B, "source identity is normalized before fanout but discarded from `LoopTerminalEvent`": true. Claim C, "no
production product handlers": true in-tree and in every sister (§ 2g). Claim D (the brief), "each output is a publish
that can carry `Nats-Msg-Id`": FALSE — the outputs are handler calls.
- `internal/agentterminal/terminal.go:67` — `SourceMessageID string`
- `internal/agentterminal/terminal.go:123` — `event := Event{SourceMessageID: base.ID(), Category: base.Type().Category}`
- `agentic/agentrun/agentrun.go:467` — `type LoopTerminalEvent struct {`
- `agentic/agentrun/agentrun.go:586` — `Role:        normalized.Role,`
- `agentic/agentrun/agentrun.go:490` — `OnLoopTerminal(ctx context.Context, ev LoopTerminalEvent, run *AgentRun) error`

## 2. Every current spelling of the fact

### 2a. The two JetStream bindings [R3: exhaustion IS observed by the framework]

Names `agentrun-milestone-complete` / `-failed`; one closure; internal consumers; explicit ack, deliver new, MaxDeliver
5, AckWait 30s; absent (`git grep` → 0): `MaxAckPending`, `MessageTimeout`, `BackOff`, `DisableMessageTimeout`, so the
handler context carries the 30s default deadline. Heartbeat 10s under the typed ceiling 15s. Redelivery: uniform 30s,
at most 5 deliveries. Round 1 said the fifth drop had "no in-tree observer"; that search was scoped to `natsclient
agentic`. The framework observes it: the MAX_DELIVERIES advisory is captured into a fixed framework stream whose
declaration warns that changing it "could silently remove the failure signal", and `internal/maxdelivery` registers
`semstreams_nats_max_delivery_exhaustions_total{domain,stream,consumer}`, started by both binaries in phase A. The
advisory fires only when `MaxDeliver` is finite. Serial per handle; exact-handle ownership, both-drain-before-Closed,
and the stream-absent skip exist. [R5] `Start` derives `runCtx` (`:806`), builds the owner around its cancel (`:807`),
and both acquisitions (`:835`, `:886`) return the exact handles the owner retains (`:681-682`).
- `agentic/agentrun/agentrun.go:782` — `func (s *MilestoneSubscriber) Start(`
- `agentic/agentrun/agentrun.go:810` — `handleMsg := func(subject string) func(ctx context.Context, msg jetstream.Msg) {`
- `agentic/agentrun/agentrun.go:832` — `MaxDeliver:    5,`
- `agentic/agentrun/agentrun.go:833` — `AckWait:       30 * time.Second,`
- `natsclient/stream.go:531` — `messageTimeout = 30 * time.Second`
- `natsclient/delivery_settlement.go:181` — `ceiling := effective / 2`
- `natsclient/stream.go:524` — `consumeCtx, err := guarded.Consume(func(msg jetstream.Msg) {`
- `config/streams.go:187` — `// could silently remove the failure signal the framework is promising to expose.`
- `config/streams.go:189` — `Subjects:  []string{"$JS.EVENT.ADVISORY.CONSUMER.MAX_DELIVERIES.>"},`
- `internal/maxdelivery/observer.go:36` — `advisorySubjectPrefix = "$JS.EVENT.ADVISORY.CONSUMER.MAX_DELIVERIES"`
- `internal/maxdelivery/observer.go:134` — `}, []string{"domain", "stream", "consumer"})`
- `internal/maxdelivery/observer.go:148` — `if err := registry.RegisterCounterVec("max-delivery-observer", "exhaustions", occurrences); err != nil {`
- `cmd/semstreams/main.go:232` — `rootResources.stopMaxDeliveryObserver, err = maxdelivery.Start(runtimeCtx, natsClient, metricsRegistry, logger)`
- `agentic/agentrun/agentrun.go:718` — `// Both running handles begin Drain before either exact Closed wait.`
- `agentic/agentrun/agentrun.go:863` — `if errors.Is(err, natsclient.ErrStreamNotVisible) {`
- `agentic/agentrun/agentrun.go:806` — `runCtx, cancel := context.WithCancel(ctx)`
- `agentic/agentrun/agentrun.go:807` — `owner := &milestoneConsumerOwner{cancel: cancel}`
- `agentic/agentrun/agentrun.go:835` — `completeHandle, err := client.ConsumeInternalStreamWithConfig(`
- `agentic/agentrun/agentrun.go:886` — `failedHandle, err := client.ConsumeInternalStreamWithConfig(`

### 2b. What the fanout does; the full set of resolution exits and their error origins [R2, R3]

(1) decode + normalize — the only non-nil return, mapped to Term; (2) build the exported event without the source id;
(3) resolve the run; (4) fan out in registration order under a per-handler recover; (5) return nil. Exits: fast path
`Get` error of ANY class → run=nil at Debug (`:637`); non-`*AgentRun` (`:641`, deterministic); no `RunEntityID` and no
`LoopID` → run=nil, silent (`:646-647`) — dead for production envelopes because `Decode` runs `Validate()` first and
every terminal rejects an empty `LoopID`; slow path `ErrEntityNotFound` → nil run (`:653`); other errors propagate
(`:657`) to WARN-and-return-nil (`:598`) → ACK. `Manager.Get`'s error set: `ErrWorkflowNotRegistered` (deterministic,
process-composition), [R4] a nil exact reader (`errors.New`, no sentinel — unclassifiable by callers), `ErrEntityNotFound`,
the exact-read wrap (the reader forwards classified errors; `errs.Classify` defaults unknown to transient),
`ErrEntityNotLifecycleManaged` (entity exists, no phase triple — its doc calls it the ADR-049 Q5 forward-reference case),
projection errors (time parse, type assign — deterministic `fmt.Errorf`, no sentinel, `%v` not `%w`). [R4] `projectTriples`
has four production call sites, three outside `Get`; `pkg/lifecycle` is Tier 1. `ResolveRun` (exported; one in-tree
caller `:651`; zero sister callers) adds entity-ID grammar failures, a parent that is not a loop entity, the 32-hop
bound, and reader errors: exact-read I/O and a non-string predicate value. `pkg/errs` has no `New*` constructor, only
`Wrap*`.
- `agentic/agentrun/agentrun.go:576` — `normalized, err := agentterminal.Decode(s.decoder, data)`
- `agentic/agentrun/agentrun.go:590` — `run, err := s.resolveRunForEvent(ctx, ev)`
- `agentic/agentrun/agentrun.go:630` — `participant, err := s.runs.Get(ctx, WorkflowName, ev.RunEntityID)`
- `agentic/agentrun/agentrun.go:637` — `return nil, nil //nolint:nilerr // deliberate: non-run loops have no run entity`
- `agentic/agentrun/agentrun.go:641` — `return nil, fmt.Errorf("Manager.Get returned unexpected type %T", participant)`
- `agentic/agentrun/agentrun.go:646` — `if ev.LoopID == "" {`
- `agentic/agentrun/agentrun.go:647` — `return nil, nil`
- `internal/agentterminal/terminal.go:119` — `if err := base.Payload().Validate(); err != nil {`
- `agentic/agentrun/agentrun.go:653` — `if errors.Is(err, lifecycle.ErrEntityNotFound) {`
- `agentic/agentrun/agentrun.go:657` — `return nil, err`
- `agentic/agentrun/agentrun.go:598` — `return nil`
- `pkg/lifecycle/manager.go:169` — `return nil, fmt.Errorf("%w: %q", ErrWorkflowNotRegistered, workflow)`
- `pkg/lifecycle/manager.go:198` — `if m.exactReader == nil {`
- `pkg/lifecycle/manager.go:205` — `return nil, 0, fmt.Errorf("%w: entity_id=%q", ErrEntityNotFound, entityID)`
- `pkg/lifecycle/manager.go:207` — `return nil, 0, fmt.Errorf("lifecycle: exact read for %q: %w", entityID, err)`
- `pkg/lifecycle/manager.go:244` — `return nil, 0, fmt.Errorf("%w: workflow=%q entity_id=%q (no %s triple)",`
- `pkg/lifecycle/manager.go:250` — `return nil, 0, fmt.Errorf("lifecycle: project entity %q (workflow %q): %w",`
- `pkg/lifecycle/manager.go:249` — `if err := projectTriples(reg.meta, entityID, state.Triples, target); err != nil {`
- `pkg/lifecycle/manager.go:599` — `if err := projectTriples(reg.meta, entityID, state.Triples, projected); err != nil {`
- `pkg/lifecycle/manager.go:1034` — `if err := projectTriples(reg.meta, target.EntityID(), outcome.Entity.Triples, instance); err != nil {`
- `pkg/lifecycle/manager_query.go:336` — `if err := projectTriples(reg.meta, entityID, state.Triples, target); err != nil {`
- `pkg/lifecycle/errors.go:42` — `ErrEntityNotLifecycleManaged = errors.New("lifecycle: entity not lifecycle-managed (no phase triple)")`
- `pkg/lifecycle/errors.go:40` — `// "exists but not lifecycle-managed yet" (forward-reference case`
- `release/tier1-packages.txt:69` — `github.com/c360studio/semstreams/pkg/lifecycle`
- `pkg/lifecycle/projection.go:114` — `return fmt.Errorf("lifecycle: project field %q: parse time %q: %v",`
- `pkg/lifecycle/projection.go:136` — `return fmt.Errorf("lifecycle: project field %q: cannot assign %v to %v",`
- `agentic/agentrun/agentrun.go:393` — `loopEntityID, err := agentic.TryLoopExecutionEntityID(org, platform, loopID)`
- `agentic/agentrun/agentrun.go:405` — `if err != nil {`
- `agentic/agentrun/agentrun.go:436` — `runEntityID, err := agentic.TryChainExecutionEntityID(org, platform, currentLoopID)`
- `agentic/agentrun/agentrun.go:455` — `return nil, fmt.Errorf("agentrun.ResolveRun: ancestry walk hop %d: parent entity %q is not a loop-execution entity ID; cannot continue walk",`
- `agentic/agentrun/agentrun.go:462` — `return nil, fmt.Errorf("agentrun.ResolveRun: ancestry walk exceeded %d hops without reaching root for loop %q", maxAncestryHops, loopID)`
- `agentic/agentrun/nats_reader.go:59` — `return "", false, fmt.Errorf("agentrun: NATSLoopTripleReader: exact read %q: %w", entityID, err)`
- `agentic/agentrun/nats_reader.go:68` — `return "", false, fmt.Errorf("agentrun: NATSLoopTripleReader: predicate %q on entity %q has non-string value %T", predicate, entityID, val)`
- `graph/exact_entity.go:65` — `response, err := r.requester.RequestClassified(ctx, exactEntityQuerySubject, request, r.timeout)`
- `pkg/errs/errs.go:280` — `return ErrorTransient`
- `pkg/errs/errs.go:435` — `func WrapInvalid(err error, component, method, action string) error {`
- `agentic/agentrun/agentrun.go:651` — `run, err := ResolveRun(ctx, s.runs, s.reader, s.org, s.platform, ev.LoopID)`

### 2c. Where ACK sits; disposition today; control-plane outcomes the typed API adds with no handler involved [R3]

ACK after the handler loop regardless of outcome. Decode failure → Term. Cancellation → NAK 5s. InProgress failure →
WARN only, lane keeps running. Under the typed API an InProgress failure sets `ownerStopNeeded`; missing or zero
delivery metadata returns Quarantine + owner stop. `interpretDeliveryWork` requires a nil cause for Ack and a non-nil
cause for every other decision, else Quarantine + owner stop. [R5] The `delivery_settlement.go` pins are `main`
lines (the L1 map is retired); the four typed-surface signatures the design composes — `DeliveryWork`,
`ValidateHeartbeatDeliveryPolicy`, `ConsumeDeliveryWithHeartbeat`, `DelayedDeliveryRetry` — are pinned below.
- `natsclient/heartbeat.go:135` — `if termErr := executeTerminalMethod(msg, terminalMethodTerm, 0); termErr != nil {`
- `natsclient/heartbeat.go:127` — `if nakErr := executeTerminalMethod(msg, terminalMethodNakWithDelay, 5*time.Second); nakErr != nil {`
- `natsclient/heartbeat.go:118` — `return heartbeatErr`
- `agentic/agentrun/agentrun.go:819` — `s.logger.Warn("agentrun: MilestoneSubscriber: HandleEvent error",`
- `natsclient/delivery_settlement.go:341` — `if metadata.NumDelivered == 0 {`
- `natsclient/delivery_settlement.go:372` — `if err := msg.InProgress(); err != nil {`
- `natsclient/delivery_settlement.go:377` — `result.ownerStopNeeded = true`
- `natsclient/delivery_settlement.go:390` — `func unavailableDeliveryMetadata(cause error) DeliveryResult {`
- `natsclient/delivery_settlement.go:403` — `valid = work.cause == nil`
- `natsclient/delivery_settlement.go:407` — `if !valid {`
- `natsclient/delivery_settlement.go:35` — `type DeliveryWork func(context.Context, []byte) (DeliveryDecision, error)`
- `natsclient/delivery_settlement.go:155` — `func ValidateHeartbeatDeliveryPolicy(`
- `natsclient/delivery_settlement.go:323` — `func ConsumeDeliveryWithHeartbeat(`
- `natsclient/delivery_settlement.go:119` — `func DelayedDeliveryRetry(delay time.Duration) (DeliveryRetryPolicy, error) {`

### 2d. Redelivery treatment and source identity

Wire identity is `base.ID()`, required non-empty by the normalizer (`:107`). [R5] The loop still publishes terminals
WITHOUT `Nats-Msg-Id`: `publishResults` now routes through `PublishToStreamWithMsgID` (`:2328`; its comment cites the
#1330 Q5 ruling) but only `agent.request` messages carry a `MsgID` (`handlers.go:1174`, `:2194`, `:2958`, all
`request.RequestID`), and `agent.failed` goes through plain `PublishToStream` (`:1831`). `Duplicates` is a stream-config
field (`config/streams.go:54`, applied `:514`) that no tracked config sets (`git grep` → 0). AgentRun performs no
dedupe. Dispatch keys on the same wire id.
- `internal/agentterminal/terminal.go:107` — `if base.ID() == "" {`
- `processor/agentic-loop/component.go:2328` — `if err := c.natsClient.PublishToStreamWithMsgID(ctx, msg.Subject, msg.Data, msg.MsgID); err != nil {`
- `processor/agentic-loop/component.go:1831` — `if pubErr := c.natsClient.PublishToStream(errorCtx, msg.Subject, msg.Data); pubErr != nil {`
- `processor/agentic-dispatch/terminal_settlement.go:245` — `ResponseID:  terminalResponseIDPrefix + event.SourceMessageID,`
- `processor/agentic-loop/handlers.go:1174` — `MsgID:   request.RequestID,`
- `config/streams.go:54` — `Duplicates string `json:"duplicates,omitempty"``
- `config/streams.go:514` — `Duplicates: duplicates,`

### 2e. Tests that freeze today's behavior

- `agentic/agentrun/agentrun_test.go:721` — `assert.True(t, secondHandlerCalled, "second handler must run even when first panics")`
- `agentic/agentrun/agentrun_test.go:730` — `func TestSubscriber_NonRunLoop_HandlersCalledWithNilRun(t *testing.T) {`
- `test/compat/semteams/agentrun_terminal_compat_test.go:75` — `if capture.events[i].Category != wantCategories[i] || capture.events[i].Outcome != wantOutcomes[i] {`

### 2f. Health and metrics [R2: seam + read sites; R3: the live registration path]

No health or metric surface today. `Service.Health()` read sites: `/health` (`:1302` → `handleSystemHealth :1722` →
`:1736`), NATS-published service health (`:790`), `/services/health` (`:1933`). The wrapper-to-subscriber seam is the
unexported `milestoneStarter` (`Start` only); the wrapper's `BaseService` is built with a nil registry. Latch-to-health
exists in five components. Metrics: components receive `Dependencies.MetricsRegistry` at construction and register
immediately through `MetricsRegistrar.RegisterCounterVec`. `Service.RegisterMetrics` is a phantom seam: declared, an
interface member, called by nothing; the repo says so and registers at construction instead. Both composition roots
hold `metricsRegistry` inside `run()` where the subscriber is constructed.
- `service/milestone_service.go:21` — `type milestoneStarter interface {`
- `service/milestone_service.go:22` — `Start(ctx context.Context, client *natsclient.Client, cfg agentrun.StartConfig) (func(context.Context) error, error)`
- `service/base.go:209` — `// Services that embed BaseService can override Health() for more detail`
- `service/base.go:389` — `func (s *BaseService) RegisterMetrics(_ metric.MetricsRegistrar) error {`
- `service/service_manager.go:1302` — `mux.HandleFunc("/health", m.handleSystemHealth)`
- `service/service_manager.go:1722` — `func (m *Manager) handleSystemHealth(w http.ResponseWriter, _ *http.Request) {`
- `service/service_manager.go:1736` — `subStatuses = append(subStatuses, service.Health())`
- `service/service_manager.go:790` — `"health":    svc.Health(),`
- `service/service_manager.go:1933` — `serviceStatuses = append(serviceStatuses, service.Health())`
- `service/storage_observability.go:248` — `// the /metrics endpoint scrapes, which is the phantom-signal class this`
- `service/component_manager.go:1208` — `MetricsRegistry: cm.BaseService.metricsRegistry,`
- `processor/agentic-dispatch/component.go:196` — `metrics:       getMetrics(deps.MetricsRegistry),`
- `metric/registry.go:216` — `func (r *MetricsRegistry) RegisterCounterVec(serviceName, metricName string, counterVec *prometheus.CounterVec) error {`
- `cmd/semstreams/main.go:170` — `metricsRegistry, phaseLogging, err := bootstrapobservability.NewProductionPhaseA(`
- `cmd/semstreams/main.go:347` — `agentrun.NewMilestoneSubscriber(`
- `cmd/e2e-semstreams/main.go:159` — `metricsRegistry, phaseLogging, err := bootstrapobservability.NewE2EPhaseA(`
- `cmd/e2e-semstreams/main.go:272` — `agentrun.NewMilestoneSubscriber(`
- `processor/agentic-loop/component.go:465` — `if c.deliveryFatalErr != nil {`
- `processor/agentic-governance/component.go:683` — `if c.deliveryFatalErr != nil {`

### 2g. Production handler set and composition roots

Zero production handlers: `AddHandler(` → 12 test call sites; `OnLoopTerminal(` → 4 test implementers. semteams
composes the subscriber (`cmd/semteams/main.go:939`) and adds none; semdev registers the workflow only. Constructors
take no registry. No e2e assertion mentions agentrun. [R4] `WorkflowName` is a package constant and both roots call
`agentrun.Register` before constructing the subscriber, so `ErrWorkflowNotRegistered` fires for every message or none.
- `cmd/semstreams/main.go:333` — `// code (semteams, etc.) adds MilestoneHandlers via AddHandler.`
- `agentic/agentrun/agentrun.go:105` — `const WorkflowName = "agent-run"`
- `cmd/semstreams/main.go:334` — `if err := agentrun.Register(svcDeps.LifecycleManager); err != nil {`
- `cmd/e2e-semstreams/main.go:261` — `if err := agentrun.Register(svcDeps.LifecycleManager); err != nil {`
- `agentic/agentrun/agentrun.go:521` — `func NewMilestoneSubscriber(`
- `agentic/agentrun/agentrun.go:559` — `func (s *MilestoneSubscriber) AddHandler(h MilestoneHandler) {`
- `service/milestone_service.go:107` — `stop, err := s.subscriber.Start(ctx, s.client, s.cfg)`

### 2h. Readers of the subjects; sibling `MaxDeliver` postures [R3]

[R5] agentic-dispatch: terminal lanes `MaxDeliver 0` (`:595`, `:636`) by a stated choice — on `main` the sentence is
"Both terminal lanes run MaxDeliver=0, so a blind NAK here is an unbounded retry of an effect whose commit state is
unproven" (`:755`); the approval-pending lane and its `MaxDeliver 10` were deleted with the in-process loop (`:725`);
user.message 3 (`:564`). agentic-tools: port-derived, default 3, BackOff 15s/60s. agentic-loop: `validateLoopRetryPolicy`
refuses `MaxDeliver < len(BackOff)` (`:1203`); `657c4734` deleted the auto-floor
(`lane.maxDeliver = len(lane.backOff)`), so the lane takes the port default 3. Sisters read the subjects through their own
subscriptions (semteams `chainpause/subscriber.go:21`, semmachina `stage/loopfailure.go:31`, semsage
`tools/spawn/executor.go:184`); none reads anything AgentRun writes.
- `processor/agentic-dispatch/component.go:595` — `MaxDeliver:    0,`
- `processor/agentic-dispatch/component.go:636` — `MaxDeliver:    0,`
- `processor/agentic-dispatch/component.go:755` — `// terminal lanes run MaxDeliver=0, so a blind NAK here is an unbounded retry`
- `processor/agentic-dispatch/component.go:725` — `// and agent.approval_pending lanes were deleted with the in-process loop`
- `processor/agentic-dispatch/component.go:564` — `MaxDeliver:    3,`
- `processor/agentic-dispatch/component.go:551` — `terminalRetryPolicy, err := natsclient.DelayedDeliveryRetry(30 * time.Second)`
- `component/port_jetstream.go:130` — `MaxDeliver:    3,`
- `processor/agentic-tools/component.go:407` — `MaxDeliver:     consumerCfg.MaxDeliver,`
- `processor/agentic-loop/component.go:1203` — `if cfg.MaxDeliver >= len(cfg.BackOff) {`
- `openspec/specs/agentic-terminal-events/spec.md:242` — `- **THEN** `MaxDeliver=0` does not preserve the evicted terminal`
- `openspec/specs/agentic-terminal-events/spec.md:247` — `Dispatch, AgentRun, and OTel SHALL consume the repo-internal normalized terminal projection. AgentRun SHALL retain its`

### 2i. Removal blast radius; Tier 1 [R2; R3: guard behavior measured]

[R5] At `main`: the declaration (`:84`), its doc comment — no `Deprecated:` marker survives; the nine lines at `:75-83`
name #1249 as the deleting PR — and `nonCancellationWorkError` (`:37`) go; `ErrHeartbeatFailed`,
`PermanentDeliveryError`, `TerminateDelivery` stay; `PermanentDeliveryError`'s doc names the helper (`:18`). Ratchet:
exact-declaration and exact-caller-set assertions, the surface guard, and the `NewDurableHandler` retirement shape.
Live specs: consumer-policy's old heartbeat requirement is gone; L0's ADDED "semantic heartbeat settlement has one
permanent exported surface" is live at `:380-414` (helper named `:385-386`, `:401`; exactly three scenarios, names
unchanged) and is the block the change's MODIFIED delta targets. nats-streaming keeps TWO requirements naming the
helper: L0 landed "Heartbeat consumption SHALL expose settlement failure" as a MODIFIED block — not the REMOVED the
design read at L0 head — so it is live at `:158-181` and its last paragraph assigns its deletion to #1249 (`:169`);
and the ADDED "shrinking remainder" at `:239-256`. The change package carries a REMOVED block for the second only.
Tier 1: `agentic/agentrun`, `natsclient`, `service` are frozen. `scripts/api-compat.sh` runs
apidiff over that list; a removed export is an "Incompatible changes" package; no allowlist, waiver, or exemption
exists (`grep -i 'allow|waiver|exempt'` → 0); CI runs report mode (exit 0, count printed); strict mode exits 1.
ADR-106 §5: an incompatible Tier 1 change resets an RC; pre-RC the count is expected non-zero and descending. A `!`
commit requires a relevant e2e tier green. `heartbeat.go` 156 lines (≈40 retained); tests 407 + 133 (unchanged).
[R5] `task api:compat:report` at HEAD (base `v1.0.0-beta.162`): 62 compared, 15 incompatible, exit 0 — `agentic/agentrun`
(`EntityIDPattern`, `Mint`) and `natsclient` (`NewDurableHandler: removed`) are ALREADY counted, so this layer adds
one incompatible line (`ConsumeWithHeartbeat: removed`) inside an already-counted package, not a sixteenth package.
- `natsclient/heartbeat.go:84` — `func ConsumeWithHeartbeat(`
- `natsclient/heartbeat.go:80` — `// the PR that migrates that caller to the typed API (#1249, in this stack)`
- `natsclient/heartbeat.go:37` — `func nonCancellationWorkError(err error) error {`
- `natsclient/heartbeat.go:18` — `// this exact message. ConsumeWithHeartbeat terminates the JetStream delivery`
- `natsclient/consumer_policy_callsite_test.go:425` — `t.Fatalf("legacy ConsumeWithHeartbeat surface violations: %v", scan.violations)`
- `natsclient/consumer_policy_callsite_test.go:428` — `"natsclient/heartbeat.go": "func(ctx context.Context, msg jetstream.Msg, heartbeatInterval time.Duration, work func(context.Context) error) error",`
- `natsclient/consumer_policy_callsite_test.go:446` — `"agentic/agentrun/agentrun.go": 1,`
- `natsclient/consumer_policy_callsite_test.go:395` — `if violations := newDurableHandlerRetirementViolations(parseProductionGoFiles(t, root)); len(violations) == 0 {`
- `openspec/specs/jetstream-consumer-policy/spec.md:385` — ``NewDurableHandler` SHALL NOT exist or have an alias. `ConsumeWithHeartbeat` SHALL have no alias and no new`
- `openspec/specs/jetstream-consumer-policy/spec.md:400` — `- **AND** `NewDurableHandler` and every alias are absent with zero production callers`
- `openspec/specs/jetstream-consumer-policy/spec.md:401` — `- **AND** `ConsumeWithHeartbeat` carries only its ratcheted remaining callers and no alias`
- `openspec/specs/jetstream-consumer-policy/spec.md:380` — `### Requirement: semantic heartbeat settlement has one permanent exported surface`
- `openspec/specs/jetstream-consumer-policy/spec.md:386` — `production caller, and SHALL be deleted by the PR that migrates its last one (#1249).`
- `openspec/specs/nats-streaming/spec.md:160` — ``ConsumeWithHeartbeat` SHALL return ACK, delayed NAK, and Term settlement errors to its caller while preserving the`
- `openspec/specs/nats-streaming/spec.md:158` — `### Requirement: Heartbeat consumption SHALL expose settlement failure`
- `openspec/specs/nats-streaming/spec.md:169` — `This requirement is deleted together with the helper by the PR that migrates its last caller (#1249).`
- `openspec/specs/nats-streaming/spec.md:239` — `### Requirement: the legacy helper is a shrinking remainder, never a compatibility surface`
- `openspec/specs/nats-streaming/spec.md:241` — `While `ConsumeWithHeartbeat` still has production callers, its coexistence with the typed surface SHALL NOT be`
- `storage/objectstore/component_ack_integration_test.go:41` — `// constant (natsclient.ConsumeWithHeartbeat), so redelivery-observing tests`
- `processor/agentic-tools/outcomes_integration_test.go:216` — `// ConsumeWithHeartbeat can ACK the request.`
- `release/tier1-packages.txt:40` — `github.com/c360studio/semstreams/agentic/agentrun`
- `release/tier1-packages.txt:57` — `github.com/c360studio/semstreams/natsclient`
- `release/tier1-packages.txt:89` — `github.com/c360studio/semstreams/service`
- `scripts/api-compat.sh:16` — `#   - A package present at base and ABSENT at head is a REMOVAL — the hardest break there is, since the adopter`
- `scripts/api-compat.sh:33` — `# Env:   API_COMPAT_MODE=report  report findings but exit 0 — the pre-RC posture, where the count is expected to be`
- `scripts/api-compat.sh:174` — `fail_count=$((n_incompatible + n_removed))`
- `scripts/api-compat.sh:190` — `if [ "${API_COMPAT_MODE:-}" = "report" ]; then`
- `scripts/api-compat.sh:193` — `exit 0`
- `taskfiles/apicompat.yml:10` — `desc: Same as api:compat but exits 0 — the pre-RC posture, where the count is expected to descend`
- `.github/workflows/ci.yml:238` — `run: task api:compat:report`
- `docs/adr/106-beta-rc-v1-exit-criteria-and-the-two-tier-surface-freeze.md:50` — `- **Tier 1 — frozen, semver-binding at 1.0.** The Go packages any sister imports (`release/tier1-packages.txt`,`
- `docs/adr/106-beta-rc-v1-exit-criteria-and-the-two-tier-surface-freeze.md:78` — `- An **incompatible change inside Tier 1** forced by a sister migration **resets to a new RC**. That is the honest`
- `docs/contributing/02-e2e-tests.md:299` — `Any commit or tag marked **BREAKING** in the changelog or commit message (a `!` after the type/scope) MUST have at`
- `docs/operations/migration-beta162-to-beta163.md:1216` — `## Semantic JetStream settlement (#759) — `ConsumeWithHeartbeat` is removed without a deprecation period`
- `docs/operations/migration-beta162-to-beta163.md:1226` — ``ConsumeWithHeartbeat` is still exported at this tag but is **removed without a deprecation period**: it is deleted`

## 3. Same-class collision table [R2: two spellings; R3: the copyable half; R5: one home, AgentRun the sixth consumer]

[R5] By behavior (`deliveryFatalErr|OwnerStopRequired`) the five owners remain dispatch, loop, model, tools, governance,
but since #1357 (`b7ce8727`) the latch has ONE home: every `delivery_owner.go` copy and governance's inline
`admissionOpen` are deleted; all five construct `deliverylane.NewAdmission(onFatal, onRefused)` before acquisition, wrap
the exact handle in `deliverylane.NewBinding`, run `deliverylane.Observe(ctx, binding, admission, react)`, and keep
only a per-component `recordDeliveryOwnerFatal` (the health writer, passed as `onFatal`) plus a log-only `react`;
`Observe` itself drains the handle after `react` returns, and `react` must be non-nil (panics at wiring). Stop is
Drain → await Closed → cancel → join `Done()` (loop `:761`, `:781`). `Consume`/`Settle` return `(DeliveryResult, bool)`;
a refusal is the zero result whose `Err()` is non-nil by construction, so every consumer guards on `admitted` (loop
`:1111` conjunct, `:1136` early return; dispatch `:613`). `test/contract/delivery_lane_one_home_contract_test.go` forbids
a production `chan natsclient.DeliveryResult` field outside the home; it enumerates no importer set, so a new consumer
needs no guard edit. AgentRun is therefore the sixth CONSUMER (design § 2.6 as amended 2026-09-19), not a sixth copy.
What `milestoneConsumerOwner` holds that the package replaces: two raw `jetstream.ConsumeContext` (`:681-682`), two
drained flags (`:683-684`), the both-drain-first ordering (`:718`), the Closed waits (`:727`). What it holds that the
package does NOT offer: the running-Stop force `Stop()` fallback (`:734-737`) — `Binding` exposes `Drain`/`Closed`/`Done`
only, and none of the five consumers keeps a `.Stop()` fallback (`git grep` → 0); no agentrun test exercises it.

| Dimension | Evidence |
|---|---|
| Semantic class | (a) admission latch + owner stop after unsafe settlement; (b) carry a terminal's wire identity |
| Owners | (a) dispatch, loop, model, tools, governance — all through `internal/deliverylane` (one home); (b) `agentterminal.Event.SourceMessageID`, dispatch `ResponseID` |
| Catalogs | none (`deliveryLaneAdmission|admissionOpen` over `*.go` non-test → 0 on `main`; the home is `internal/deliverylane`, importable only inside this module) |
| Status / readers / writers | `deliveryFatalErr` → each component's `Health()`; written by `recordDeliveryOwnerFatal` as `onFatal`, synchronously inside `Admission.Latch` before the result is buffered |
| Lifecycle / ownership | one-way until process replacement; one `Admission` per handle; the owner retains the `Binding`, decides Stop, awaits `Closed`, joins `Done`; AgentRun's handle owner is `milestoneConsumerOwner` |
| Recovery | process replacement re-acquires the durable (#1155); no in-process reconstruction |

- `internal/deliverylane/deliverylane.go:27` — `type Admission struct {`
- `internal/deliverylane/deliverylane.go:45` — `func NewAdmission(`
- `internal/deliverylane/deliverylane.go:67` — `func (a *Admission) Latch(result natsclient.DeliveryResult) {`
- `processor/agentic-loop/component.go:1026` — `func (c *Component) recordDeliveryOwnerFatal(result natsclient.DeliveryResult) {`
- `internal/deliverylane/deliverylane.go:105` — `func Consume(`
- `internal/deliverylane/deliverylane.go:194` — `func NewBinding(handle jetstream.ConsumeContext) *Binding {`
- `internal/deliverylane/deliverylane.go:225` — `func Observe(`
- `processor/agentic-model/component.go:402` — `admission := deliverylane.NewAdmission(c.recordDeliveryOwnerFatal, nil)`
- `processor/agentic-tools/component.go:428` — `admission := deliverylane.NewAdmission(c.recordDeliveryOwnerFatal, func(subject string) {`
- `processor/agentic-governance/component.go:490` — `admission := deliverylane.NewAdmission(c.recordDeliveryOwnerFatal, nil)`
- `processor/agentic-governance/component.go:700` — `func (c *Component) recordDeliveryOwnerFatal(result natsclient.DeliveryResult) {`
- `agentic/agentrun/agentrun.go:679` — `type milestoneConsumerOwner struct {`
- `agentic/agentrun/agentrun.go:683` — `completeDrained bool`
- `agentic/agentrun/agentrun.go:712` — `drainComplete := complete != nil && !o.completeDrained`
- `agentic/agentrun/agentrun.go:727` — `stopErrors = append(stopErrors, waitMilestoneConsumerClosed(ctx, complete.Closed(), "complete"))`
- `internal/deliverylane/deliverylane.go:12` — `// agentic/agentrun once #1249 adopts it; exporting it is a Tier 1 widening gated`
- `internal/deliverylane/deliverylane.go:58` — `func (a *Admission) Admit() bool {`
- `internal/deliverylane/deliverylane.go:115` — `result := natsclient.ConsumeDeliveryWithHeartbeat(ctx, msg, policy)`
- `internal/deliverylane/deliverylane.go:126` — `// because a refusal returns the zero natsclient.DeliveryResult and a zero`
- `internal/deliverylane/deliverylane.go:132` — `// form, `if !admitted { return }`, keeps the branches below it unchanged.`
- `internal/deliverylane/deliverylane.go:207` — `func (b *Binding) Drain() { b.drainOnce.Do(b.handle.Drain) }`
- `internal/deliverylane/deliverylane.go:211` — `func (b *Binding) Closed() <-chan struct{} { return b.handle.Closed() }`
- `internal/deliverylane/deliverylane.go:216` — `func (b *Binding) Done() <-chan struct{} { return b.done }`
- `internal/deliverylane/deliverylane.go:232` — `panic("deliverylane: Observe requires a non-nil react")`
- `internal/deliverylane/deliverylane.go:240` — `react(result)`
- `internal/deliverylane/deliverylane.go:241` — `binding.Drain()`
- `processor/agentic-loop/component.go:1110` — `result, admitted := deliverylane.Consume(msgCtx, msg, policy, admission)`
- `processor/agentic-loop/component.go:1136` — `if !admitted {`
- `processor/agentic-loop/component.go:1161` — `deliverylane.Observe(consumerCtx, binding, admission, func(result natsclient.DeliveryResult) {`
- `processor/agentic-loop/component.go:761` — `binding.Drain()`
- `processor/agentic-loop/component.go:781` — `case <-binding.Done():`
- `processor/agentic-dispatch/component.go:568` — `userMessageAdmission := deliverylane.NewAdmission(c.recordDeliveryOwnerFatal, nil)`
- `processor/agentic-dispatch/component.go:608` — `agentCompleteAdmission := deliverylane.NewAdmission(c.recordAgentCompleteFatal, func(subject string) {`
- `processor/agentic-dispatch/component.go:612` — `result, admitted := deliverylane.Consume(msgCtx, msg, agentCompletePolicy, agentCompleteAdmission)`
- `processor/agentic-governance/component.go:508` — `deliverylane.Observe(ctx, binding, admission, func(result natsclient.DeliveryResult) {`
- `processor/agentic-model/component.go:419` — `deliverylane.Observe(ctx, binding, admission, c.reactDeliveryFatal)`
- `test/contract/delivery_lane_one_home_contract_test.go:16` — `const deliveryLaneHome = "internal/deliverylane"`
- `openspec/specs/jetstream-consumer-policy/spec.md:606` — `Refusal by closed admission is a declared event, not a silent drop. Each refused delivery SHALL emit a substrate log`
- `openspec/specs/jetstream-consumer-policy/spec.md:610` — `Within this module the owner-side reaction has exactly one home: one shared package that provides the per-lane`
- `openspec/specs/jetstream-consumer-policy/spec.md:625` — `#### Scenario: closed admission refuses a buffered delivery`
- `agentic/agentrun/agentrun.go:681` — `complete        jetstream.ConsumeContext`
- `agentic/agentrun/agentrun.go:682` — `failed          jetstream.ConsumeContext`
- `agentic/agentrun/agentrun.go:734` — `// Running Stop is terminal. Force local closure best-effort and never`
- `agentic/agentrun/agentrun.go:737` — `complete.Stop()`
- `agentic/agentrun/agentrun.go:743` — `o.cancel()`
- `agentic/agentrun/agentrun.go:756` — `func waitMilestoneConsumerClosed(ctx context.Context, closed <-chan struct{}, name string) error {`

## Adjacent claims

- #1249, #759 (rulings 2026-09-02 items 3/6/7; placement 2026-09-18), #1146 (L-stack), #1155 (Stage D), #1301 (two boot roots), #1330 (L4).
- [R5] L0 merged as `f4d66934` (PR #1331; archive `2026-09-19-semantic-jetstream-settlement`): consumer-policy ADDED "semantic heartbeat settlement has one permanent exported surface", live at `:380-414` — its helper sentence `:385-386` reads "SHALL have no alias and no new production caller, and SHALL be deleted by the PR that migrates its last one (#1249)", its third paragraph still says model and loop "keep the legacy helper under the ratchet" (stale since L1, replaced whole by the MODIFIED block); scenarios "public surface at this layer" (`:396`), "binding migration requires semantic authority", "fast lane lacks an admitted settlement route" — the three the delta restates. nats-streaming ADDED "the legacy helper is a shrinking remainder, never a compatibility surface" (live `:239-256`) and landed "Heartbeat consumption SHALL expose settlement failure" as a MODIFIED block (archive delta `:79-81`), NOT the REMOVED the design read at L0 head: live `:158-181`, last paragraph `:169` "This requirement is deleted together with the helper by the PR that migrates its last caller (#1249)". `DeliveryWork` live at `delivery_settlement.go:35`; `DeliveryAttempt`/`ServerConfirmed` remain withdrawn (`NumDelivered` read at `:341`, discarded). The `Deprecated:` marker was replaced by a doc comment naming #1249 (`heartbeat.go:75-83`). `migration-beta162-to-beta163.md:1216-1231` still says "still exported at this tag".
- [R5] L1 merged as `94cd8e4c` (PR #1334; archive `2026-09-19-settle-after-durable-effect`): MODIFIED "shared settlement remains stateless and heartbeat-specific"; `SettleDeliveryWithRetry` at `delivery_settlement.go:303`; loop auto-floor deleted; model and loop off the helper — `git grep -n -E 'ConsumeWithHeartbeat\(' -- '*.go' ':!**/*_test.go'` on `main` (stderr visible) → `agentic/agentrun/agentrun.go:812` and the declaration `natsclient/heartbeat.go:84` only; `consume_durable.go` does not exist.
- [R5] L2 #1335 (archive `2026-09-21-stable-request-identity`) and L3 `3faca84f` (#1338; archive `2026-09-21-durable-loop-authority`): neither touches `agentic/agentrun` or `internal/agentterminal`; loop `component.go` renumbered (`:465`, `:1203`, `:1831`, `:2328`); dispatch's approval-pending lane deleted (`:725`).
- [R5] #1357 `b7ce8727` (#1341; archive `2026-09-21-delivery-lane-admission-package`): `internal/deliverylane` (§ 3). Its `design.md` § 6 (`:363-375`) writes the agentrun shape the § 2.6 amendment consumes: two `*deliverylane.Binding` replace the raw handles and drained flags; `stop()` drains both, awaits both `Closed()`, cancels `runCtx`, joins both `Done()` — "a join draft-4 never specified". #1342 (OPEN, `class:advertised-absent`): five lanes pass `nil` `onRefused` against consumer-policy `:606-608`; issuecomment-5763070246 records that a per-lane wiring-level refusal test is owed by the change that wires the declarer. Claim: draft PR #1360 (`Closes #1249`, `Closes #759`; `implemented-by: pending`).
- semdev direct callers `internal/conversationchannel/component.go:476`, `internal/intake/component.go:378`; comment-only sites sizing `max_deliver 10` on the removed 30s NAK: `conversationchannel/apply.go:113`, `:202`, `conversationchannel/component.go:435`, `intake/component.go:355`.
- `docs/adr/053-agent-run-substrate.md:171` — `observation-only and does not stamp milestones or terminal phases. If a product`
- `docs/adr/053-agent-run-substrate.md:188` — `### D6 — Adapter: subscriber + lifted resolution`
- `docs/concepts/33-semantic-settlement.md:107` — `replacement proof. Model and loop work continues under #1146. AgentRun fanout needs its own design because one source`
- `docs/operations/migration-restart-safe-nats-client.md:104` — ``ConsumeWithHeartbeat` is still exported while its last callers migrate — #1327 takes model and loop, #1249 takes`
- `processor/agentic-tools/outcomes.go:21` — `// completedOutcome is the immutable COMPLETED record. There is deliberately`

## 4. Consumer at birth

`LoopTerminalEvent.SourceMessageID`: the #1155 stage-D proof handler and the compat test; required by ruling item 3.
`MilestoneSubscriber.DeliveryFatal()`: the `MilestoneService.Health()` override reaching the three read sites.
`MilestoneSubscriber.RegisterMetrics(metric.MetricsRegistrar)`: both composition roots (§ 2f); semteams' root has an
unregistered counter until it adds the call. The decisions counter: the e2e `verify-streaming-metrics` stage and
operators. The stuck-milestone signal is the existing framework counter (§ 2a): no new consumer.

## 5. Problem shape

One durable input at-least-once → N adopter-owned side effects the framework cannot observe → one settlement for all N.
Closest: dispatch terminal (one output, identity-keyed); tools `completedOutcome` (the receipt shape #1249 forbids);
rule `for_each` / loop `publishResults` (fail-at-first); the owner-stop latch. None fans one delivery to N external
handlers; N handlers share one failure domain (design § 6).

## Adopter seam inventory (a product developer writing the first `MilestoneHandler`)

1. Must know today: (a) `run` may be nil for two reachable causes (not-found, any fast-path `Get` error) plus a dead
   silent-identity guard; (b) a returned error is logged and ACKed; (c) a panic is logged and ACKed; (d) the same event
   can arrive twice with no identity handed over; (e) one 30s deadline per delivery; (f) serial in registration order;
   (g) [R3] an InProgress failure leaves the lane running with a WARN. Seven facts.
2. Do nothing: a transient failure is silently ratified and its consequence lost; nothing in `/health`, metrics, or
   delivery state shows it. 3. Where found: (b),(c),(g) log line; (a) doc/test; (d),(e),(f) nowhere.
4. Should know: return nil only after your durable consequence, keyed on the identity the framework hands you. The
   framework observes the return class; it must not ask the adopter to predict retry timing, deadlines, or lane
   concurrency. [R3] Under the typed API an InProgress failure or missing metadata stops the lane and marks the
   service unhealthy — an operator-visible change learned from `/health`, rank "typed runtime state".

## Searches

- `git rev-parse origin/claude/gh1327-settle-after-effect` → 0053183d…; `git -C <L1> rev-parse HEAD` → c2a9cef6; `git -C <L0> rev-parse HEAD` → da6a70c0; `git -C <L1> diff --stat 0053183d… HEAD -- <every pinned file>` → 3 files (`delivery_settlement.go` 26±, governance `component.go` 33±, loop `component.go` 336±); per moved pin: `git -C <L1> grep -n -F -- "$(git show base:$f | sed -n ${n}p)" HEAD -- $f`; `git -C <L1> log --oneline f420128a..HEAD` → 5 (`657c4734`, `f1c7278d`, `5ca936ac`, `cafe3731`, `192e6035`); `git -C <L0> log --oneline --stat 29bd7dae..HEAD` → 1 (`da6a70c0`, dispatch + tools tests + spec/tasks; spec lines `:78`/`:91`/`:63`/`:83` unchanged)
- `git diff --stat main 0053183d… -- agentic/agentrun/ internal/agentterminal/` → empty
- `git grep -n ConsumeWithHeartbeat 0053183d… -- .` → 1 production caller; `… origin/main -- '*.go' ':!**/*_test.go'` → 6; `git -C <L0> grep -n ConsumeWithHeartbeat HEAD -- openspec/changes/semantic-jetstream-settlement/specs docs/operations/migration-beta162-to-beta163.md` → consumer-policy `:78`, `:91`; nats-streaming `:63`, `:83`; migration `:1050`, `:1060`
- `git -C <L0> grep -n -E 'type DeliveryWork|NumDelivered' HEAD -- natsclient/delivery_settlement.go` → `:35`, `:306`
- `git grep -n -E 'deliveryFatalErr|OwnerStopRequired' 0053183d… -- '*.go' ':!**/*_test.go'` → dispatch, governance, loop, model, tools; `git -C <L1> grep -ln deliveryLaneAdmission HEAD -- '*.go' ':!**/*_test.go'` → 5 `delivery_owner.go` + loop `component.go`
- `git grep -n -E 'RegisterMetrics\(' 0053183d… -- '*.go' ':!**/*_test.go'` → base.go:389/:494 decl, storage_observability.go:351 impl, dispatch metrics.go:65/:78/:84 — no `Service.RegisterMetrics` caller
- `git grep -n -E 'HandleFunc\("/health|handleServicesHealth|handleSystemHealth' 0053183d… -- service/service_manager.go` → `:1302`, `:1703`, `:1709`
- `git grep -n maxdelivery 0053183d… -- cmd` → 2 mains; `git grep -n -E 'MAX_DELIVERIES|MaxDeliveries' 0053183d… -- natsclient agentic` → 0 (round-1 scope; superseded)
- `git grep -n MaxDeliver 0053183d… -- processor/agentic-dispatch/component.go processor/agentic-tools/component.go processor/agentic-loop/component.go component`, the same at `<L0> HEAD` (dispatch) and `<L1> HEAD` (loop `:968`, `:988`, `:1049`, `:1154`)
- `git grep -n -i -E 'allow|waiver|exempt' 0053183d… -- scripts/api-compat.sh` → 0; `git ls-tree -r 0053183d… -- release/ scripts/ | grep -i -E 'compat|allow|waiver'` → tier1 list, `api-compat.sh`, its fixture test, `verify-semteams-agentrun-compat.sh`; `.github/workflows/ci.yml:207-252`
- `git grep -n -E 'func \(m \*Manager\) (lookupByWorkflow|projectTriples|getWithRevision)|func projectTriples' 0053183d… -- pkg/lifecycle` → `manager.go:164`, `:234`, `projection.go:45`; `git grep -n 'projectTriples(' 0053183d… -- pkg/lifecycle` → `manager.go:249`, `:599`, `:1034`, `manager_query.go:336` (+ decl, test); `git grep -n ErrEntityNotLifecycleManaged 0053183d… -- agentic processor service` → 0; `grep -n -E '^func (New|Wrap)[A-Za-z]*\(' pkg/errs/errs.go` → `Wrap`, `WrapTransient`, `WrapFatal`, `WrapInvalid` only; `git grep -n 'ResolveRun(' 0053183d… -- '*.go'` → decl `:390`, caller `:651`; sisters (`git -C` over every `/Users/coby/Code/c360/*/.git`, stderr visible) → 0
- `git grep -n -E 'Publish|kv\.|Put\(|Create\(' 0053183d… -- agentic/agentrun/agentrun.go` → only `mgr.Create` in Mint; `… -i 'health|metric|prometheus' -- agentic/agentrun` → 0; `… 'MaxAckPending|MessageTimeout|BackOff|DisableMessageTimeout' -- agentic/agentrun` → 0; `… -i 'agentrun|milestone' -- test/e2e taskfiles/e2e` → 0
- `git grep -n -E 'func .*OnLoopTerminal\(' 0053183d… -- .` → 4 test implementers; `git grep -c 'AddHandler(' 0053183d… -- .` → 12 test call sites
- sister sweeps (23 repos, `git -C`; non-repo dirs by `grep -rl`) → semteams composes (main.go:939), semdev registers only (+2 direct helper callers, 4 comment sites); `gh issue view 1327 --json comments`, `gh pr view 1334 --json body,comments` grep `admission|latch|consolidat|shared|sixth` → 0
- [R5] `scripts/inventory-verify.sh openspec/changes/agentrun-fanout-settlement/inventory.md` at seed `8ddc8974` → `pins=162 ok=117 moved=22 ambiguous=4 drift=19 malformed=0 unparsed=0`, exit 1; 18 pinned files changed since `0053183d`, three deleted (`processor/agentic-{loop,model,tools}/delivery_owner.go`)
- [R5] `git diff --stat 0053183d..b7ce8727 -- agentic/agentrun internal/agentterminal service/milestone_service.go service/base.go service/service_manager.go service/component_manager.go service/storage_observability.go cmd/semstreams/main.go cmd/e2e-semstreams/main.go pkg/lifecycle pkg/errs metric/registry.go config/streams.go internal/maxdelivery release/tier1-packages.txt scripts/api-compat.sh taskfiles/apicompat.yml test/compat/semteams graph/exact_entity.go component/port_jetstream.go natsclient/stream.go docs/adr/053-agent-run-substrate.md docs/adr/106-*.md docs/contributing/02-e2e-tests.md openspec/specs/agentic-terminal-events/spec.md storage/objectstore/component_ack_integration_test.go` → empty
- [R5] `git grep -n -E 'ConsumeWithHeartbeat\(' -- '*.go' ':!**/*_test.go'` (stderr visible, rc 0) → `agentic/agentrun/agentrun.go:812`, `natsclient/heartbeat.go:84`; `git ls-files natsclient | grep -E 'consume_durable|heartbeat'` → `heartbeat.go`, `heartbeat_integration_test.go`, `heartbeat_test.go`; `wc -l` → 156 / 133 / 407
- [R5] `git grep -n 'deliverylane\.' -- '*.go' ':!**/*_test.go'` → dispatch `:124,:568,:570,:582,:583,:608,:612,:623,:624,:649,:653,:664,:665`, governance `:58,:490,:492,:507,:508`, loop `:77,:1102,:1108,:1110,:1130,:1132,:1154,:1161`, model `:65,:402,:409,:418,:419`, tools `:85,:428,:443,:453,:457`; `git grep -n -E '\.Stop\(\)' -- <the five component.go>` minus `Component.Stop`/dispatcher/timers → 0; `git grep -n -E 'force|stopErr != nil && running|\.Stop\(\)' -- agentic/agentrun/*_test.go` → 0
- [R5] `git grep -n -E 'agentrun_milestone|milestone_decisions' -- .` → this change's docs only; `git grep -n -E 'Subsystem: *"agentrun"' -- '*.go'` → 0; `git grep -n -o -E 'RegisterCounterVec\("[a-z-]+", "[a-z_-]+"' -- '*.go' ':!**/*_test.go'` → no `"agentrun"` service name
- [R5] `git grep -n -E 'MsgID:|\.MsgID' -- 'processor/agentic-loop/*.go' ':!**/*_test.go'` → `component.go:2328`, `handlers.go:1174`, `:2194`, `:2958` (all `request.RequestID`); `git grep -n 'PublishToStream' -- processor/agentic-loop/component.go` → `:1831` (failure events), `:2328` (results, with-MsgID), `:2359` (context event), `:2668` (cancel completion); `git grep -n -i -E '"duplicates"|duplicates:' -- '*.json' '*.yaml' '*.yml' ':!openspec' ':!docs'` → 0
- [R5] `git log -S'This requirement is deleted together with the helper' --format='%h %s' -- openspec/specs/nats-streaming/spec.md` → `f4d66934`; `grep -n -E '^## |^### Requirement' openspec/changes/archive/2026-09-19-semantic-jetstream-settlement/specs/nats-streaming/spec.md` → `:79 ## MODIFIED Requirements`, `:81 ### Requirement: Heartbeat consumption SHALL expose settlement failure`; `grep -n '^### Requirement' openspec/specs/nats-streaming/spec.md` → `:158`, `:239` name the helper; `git grep -n -E 'ConsumeWithHeartbeat|NewDurableHandler' -- openspec/specs/jetstream-consumer-policy/spec.md` → `:72`, `:105`, `:385`, `:400`, `:401`; `ls openspec/specs | grep -i -E 'agent-run|milestone'` → 0 (new capability)
- [R5] `task api:compat:report` at `8ddc8974` (51 s; base `v1.0.0-beta.162`, 62 packages) → `compared 62, clean 47, incompatible 15, removed 0, added 0`, exit 0; `agentic/agentrun`: `EntityIDPattern` value changed, `Mint` signature changed; `natsclient`: `NewDurableHandler: removed`
- [R5] `git grep -n -E 'agentrun\.(Register|NewMilestoneSubscriber)|MilestoneHandler|AddHandler' -- cmd/` → `semstreams/main.go:333-334`, `:347`; `e2e-semstreams/main.go:261`, `:272`; no handler registered in either root (Q5 handler absent, as designed); `gh issue view 1155 --comments` → latest comment 2026-09-14, the O5 amendment is not yet recorded there; `gh api .../issues/comments/5763070246` → the #1342 wiring-test obligation, scoped to "the change that wires the declarer"

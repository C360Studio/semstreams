# #1249 inventory — AgentRun milestone-fanout settlement (inventory-only; round 3 after DESIGN CHANGES REQUESTED)

base: 0053183d02669aa8e159bc28b36be256ffb075e2

Base = `origin/claude/gh1327-settle-after-effect` at round 1 = PR #1334's then-head. Heads move while agents build:
L1 is `c2a9cef6` at this read, L0 `da6a70c0`. Pins are BASE lines (the verifier runs at the base). [R4] At L1 head
`git diff --stat base..HEAD` over the pinned files is non-empty for exactly three: `natsclient/delivery_settlement.go`,
`processor/agentic-governance/component.go`, `processor/agentic-loop/component.go`; every other pinned file is
byte-identical. The L1-head position of each pin in those three files is in § Adjacent claims, found by exact-text
match and printed with `sed -n` from the L1 tree. Verify from `semstreams-wt/verify-0053183d`. Read-only; sisters via
`git -C` only. One pin per bullet; commentary in prose. Round tags [R2], [R3], [R4].

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
- `natsclient/heartbeat.go:140` — `return executeTerminalMethod(msg, terminalMethodAck, 0)`

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
and the stream-absent skip exist.
- `agentic/agentrun/agentrun.go:782` — `func (s *MilestoneSubscriber) Start(`
- `agentic/agentrun/agentrun.go:810` — `handleMsg := func(subject string) func(ctx context.Context, msg jetstream.Msg) {`
- `agentic/agentrun/agentrun.go:832` — `MaxDeliver:    5,`
- `agentic/agentrun/agentrun.go:833` — `AckWait:       30 * time.Second,`
- `natsclient/stream.go:531` — `messageTimeout = 30 * time.Second`
- `natsclient/delivery_settlement.go:191` — `ceiling := effective / 2`
- `natsclient/stream.go:524` — `consumeCtx, err := guarded.Consume(func(msg jetstream.Msg) {`
- `config/streams.go:187` — `// could silently remove the failure signal the framework is promising to expose.`
- `config/streams.go:189` — `Subjects:  []string{"$JS.EVENT.ADVISORY.CONSUMER.MAX_DELIVERIES.>"},`
- `internal/maxdelivery/observer.go:36` — `advisorySubjectPrefix = "$JS.EVENT.ADVISORY.CONSUMER.MAX_DELIVERIES"`
- `internal/maxdelivery/observer.go:134` — `}, []string{"domain", "stream", "consumer"})`
- `internal/maxdelivery/observer.go:148` — `if err := registry.RegisterCounterVec("max-delivery-observer", "exhaustions", occurrences); err != nil {`
- `cmd/semstreams/main.go:232` — `rootResources.stopMaxDeliveryObserver, err = maxdelivery.Start(runtimeCtx, natsClient, metricsRegistry, logger)`
- `agentic/agentrun/agentrun.go:718` — `// Both running handles begin Drain before either exact Closed wait.`
- `agentic/agentrun/agentrun.go:863` — `if errors.Is(err, natsclient.ErrStreamNotVisible) {`

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
cause for every other decision, else Quarantine + owner stop. [R4] L1-head lines for the `delivery_settlement.go`
pins: § Adjacent claims.
- `natsclient/heartbeat.go:130` — `if termErr := executeTerminalMethod(msg, terminalMethodTerm, 0); termErr != nil {`
- `natsclient/heartbeat.go:122` — `if nakErr := executeTerminalMethod(msg, terminalMethodNakWithDelay, 5*time.Second); nakErr != nil {`
- `natsclient/heartbeat.go:113` — `return heartbeatErr`
- `agentic/agentrun/agentrun.go:819` — `s.logger.Warn("agentrun: MilestoneSubscriber: HandleEvent error",`
- `natsclient/delivery_settlement.go:329` — `if metadata.NumDelivered == 0 {`
- `natsclient/delivery_settlement.go:361` — `if err := msg.InProgress(); err != nil {`
- `natsclient/delivery_settlement.go:366` — `result.ownerStopNeeded = true`
- `natsclient/delivery_settlement.go:379` — `func unavailableDeliveryMetadata(cause error) DeliveryResult {`
- `natsclient/delivery_settlement.go:392` — `valid = work.cause == nil`
- `natsclient/delivery_settlement.go:396` — `if !valid {`

### 2d. Redelivery treatment and source identity

Wire identity is `base.ID()`, required non-empty by the normalizer (`:107`). The loop publishes terminals WITHOUT
`Nats-Msg-Id`; no stream declares a duplicates window. AgentRun performs no dedupe. Dispatch keys on the same wire id.
- `internal/agentterminal/terminal.go:107` — `if base.ID() == "" {`
- `processor/agentic-loop/component.go:1956` — `if err := c.natsClient.PublishToStream(ctx, msg.Subject, msg.Data); err != nil {`
- `processor/agentic-dispatch/terminal_settlement.go:146` — `ResponseID:  terminalResponseIDPrefix + event.SourceMessageID,`

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
- `processor/agentic-dispatch/component.go:185` — `metrics:       getMetrics(deps.MetricsRegistry),`
- `metric/registry.go:216` — `func (r *MetricsRegistry) RegisterCounterVec(serviceName, metricName string, counterVec *prometheus.CounterVec) error {`
- `cmd/semstreams/main.go:170` — `metricsRegistry, phaseLogging, err := bootstrapobservability.NewProductionPhaseA(`
- `cmd/semstreams/main.go:347` — `agentrun.NewMilestoneSubscriber(`
- `cmd/e2e-semstreams/main.go:159` — `metricsRegistry, phaseLogging, err := bootstrapobservability.NewE2EPhaseA(`
- `cmd/e2e-semstreams/main.go:272` — `agentrun.NewMilestoneSubscriber(`
- `processor/agentic-loop/component.go:436` — `if c.deliveryFatalErr != nil {`
- `processor/agentic-governance/component.go:724` — `if c.deliveryFatalErr != nil {`

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

agentic-dispatch: terminal lanes `MaxDeliver 0` by a stated choice ("unlimited, retention-bounded settlement"),
approval-pending 10, user.message / agent.created 3 — identical at L0 head `da6a70c0` (`:552`, `:576`, `:617`, `:641`,
`:695`). agentic-tools: port-derived, default 3, BackOff 15s/60s. agentic-loop: `validateLoopRetryPolicy` refuses
`MaxDeliver < len(BackOff)` (base `:1129`; L1 head `:1154`); `657c4734` deleted the auto-floor
(`lane.maxDeliver = len(lane.backOff)`), so the lane takes the port default 3. Sisters read the subjects through their own
subscriptions (semteams `chainpause/subscriber.go:21`, semmachina `stage/loopfailure.go:31`, semsage
`tools/spawn/executor.go:184`); none reads anything AgentRun writes.
- `processor/agentic-dispatch/component.go:575` — `MaxDeliver:    0,`
- `processor/agentic-dispatch/component.go:684` — `// MaxDeliver is intentionally finite while terminal sibling subscriptions`
- `processor/agentic-dispatch/component.go:688` — `// cycle. Terminal siblings instead use unlimited, retention-bounded`
- `processor/agentic-dispatch/component.go:698` — `MaxDeliver:    10,`
- `processor/agentic-dispatch/component.go:530` — `terminalRetryPolicy, err := natsclient.DelayedDeliveryRetry(30 * time.Second)`
- `component/port_jetstream.go:130` — `MaxDeliver:    3,`
- `processor/agentic-tools/component.go:413` — `MaxDeliver:     consumerCfg.MaxDeliver,`
- `processor/agentic-loop/component.go:1129` — `if cfg.MaxDeliver >= len(cfg.BackOff) {`
- `openspec/specs/agentic-terminal-events/spec.md:242` — `- **THEN** `MaxDeliver=0` does not preserve the evicted terminal`
- `openspec/specs/agentic-terminal-events/spec.md:247` — `Dispatch, AgentRun, and OTel SHALL consume the repo-internal normalized terminal projection. AgentRun SHALL retain its`

### 2i. Removal blast radius; Tier 1 [R2; R3: guard behavior measured]

At the base: declaration + `Deprecated:` notice (present at base and L1 head; absent at L0 head) and
`nonCancellationWorkError` go; `ErrHeartbeatFailed`, `PermanentDeliveryError`, `TerminateDelivery` stay;
`PermanentDeliveryError`'s doc names the helper. Ratchet: exact-declaration and exact-caller-set assertions, the
surface guard, and the `NewDurableHandler` retirement shape. Current specs name the helper at consumer-policy
`:301/:329/:333` and nats-streaming `:160`; L0's delta REMOVES both requirements and ADDS helper-still-exists text
(§ Adjacent claims). Tier 1: `agentic/agentrun`, `natsclient`, `service` are frozen. `scripts/api-compat.sh` runs
apidiff over that list; a removed export is an "Incompatible changes" package; no allowlist, waiver, or exemption
exists (`grep -i 'allow|waiver|exempt'` → 0); CI runs report mode (exit 0, count printed); strict mode exits 1.
ADR-106 §5: an incompatible Tier 1 change resets an RC; pre-RC the count is expected non-zero and descending. A `!`
commit requires a relevant e2e tier green. `heartbeat.go` 151 lines (≈35 retained); tests 407 + 133.
- `natsclient/heartbeat.go:79` — `func ConsumeWithHeartbeat(`
- `natsclient/heartbeat.go:75` — `// Deprecated: use ConsumeDeliveryWithHeartbeat. This export exists only on the`
- `natsclient/heartbeat.go:37` — `func nonCancellationWorkError(err error) error {`
- `natsclient/heartbeat.go:18` — `// this exact message. ConsumeWithHeartbeat terminates the JetStream delivery`
- `natsclient/consumer_policy_callsite_test.go:423` — `t.Fatalf("legacy ConsumeWithHeartbeat surface violations: %v", scan.violations)`
- `natsclient/consumer_policy_callsite_test.go:426` — `"natsclient/heartbeat.go": "func(ctx context.Context, msg jetstream.Msg, heartbeatInterval time.Duration, work func(context.Context) error) error",`
- `natsclient/consumer_policy_callsite_test.go:444` — `"agentic/agentrun/agentrun.go": 1,`
- `natsclient/consumer_policy_callsite_test.go:395` — `if violations := newDurableHandlerRetirementViolations(parseProductionGoFiles(t, root)); len(violations) == 0 {`
- `openspec/specs/jetstream-consumer-policy/spec.md:301` — `InProgress, cancellation, heartbeat failure, and work join to `ConsumeWithHeartbeat`. Every nonnil result SHALL emit`
- `openspec/specs/jetstream-consumer-policy/spec.md:329` — `- **THEN** `ConsumeWithHeartbeat` exclusively controls InProgress and terminal settlement`
- `openspec/specs/jetstream-consumer-policy/spec.md:333` — `- **WHEN** `ConsumeWithHeartbeat` returns a nonnil result`
- `openspec/specs/nats-streaming/spec.md:160` — ``ConsumeWithHeartbeat` SHALL return ACK, delayed NAK, and Term settlement errors to its caller while preserving the`
- `storage/objectstore/component_ack_integration_test.go:41` — `// constant (natsclient.ConsumeWithHeartbeat), so redelivery-observing tests`
- `processor/agentic-tools/outcomes_integration_test.go:202` — `// ConsumeWithHeartbeat can ACK the request.`
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

## 3. Same-class collision table [R2: two spellings; R3: the copyable half and the existing handle owner]

By behavior (`deliveryFatalErr|OwnerStopRequired`): five owners at the base — four `delivery_owner.go` copies and
governance inline (`:510` `admissionOpen`). [R4] At L1 head `c2a9cef6` the inline spelling is gone: governance has
`processor/agentic-governance/delivery_owner.go` (`:20` type, `:27` ctor; commit `1b8598ce`), `recordDeliveryOwnerFatal`
at `component.go:718`, Health read at `:701` — five copies in one spelling; AgentRun's is the sixth. In the loop copy only `:29-63` (type, ctor, `admit`, `latch`) and `:74-86` (`consumeAdmittedDelivery`) are
receiver-free; `:65` has a `*Component` receiver and `:88` returns the component-local binding. AgentRun already owns
both handles, both drained flags, the Closed waits, and the both-drain-before-Closed ordering in `milestoneConsumerOwner`.

| Dimension | Evidence |
|---|---|
| Semantic class | (a) admission latch + owner stop after unsafe settlement; (b) carry a terminal's wire identity |
| Owners | (a) dispatch, loop, model, tools, governance; (b) `agentterminal.Event.SourceMessageID`, dispatch `ResponseID` |
| Catalogs | none (`deliveryLaneAdmission|admissionOpen` over `natsclient pkg internal` → 0) |
| Status / readers / writers | `deliveryFatalErr` → `Health()` (three sites); written by `latch(result)` only |
| Lifecycle / ownership | one-way until process replacement; one latch per handle; AgentRun's handle owner is `milestoneConsumerOwner` |
| Recovery | process replacement re-acquires the durable (#1155); no in-process reconstruction |

- `processor/agentic-loop/delivery_owner.go:29` — `type deliveryLaneAdmission struct {`
- `processor/agentic-loop/delivery_owner.go:36` — `func newDeliveryLaneAdmission(onFatal func(natsclient.DeliveryResult)) *deliveryLaneAdmission {`
- `processor/agentic-loop/delivery_owner.go:48` — `func (a *deliveryLaneAdmission) latch(result natsclient.DeliveryResult) {`
- `processor/agentic-loop/delivery_owner.go:65` — `func (c *Component) recordDeliveryOwnerFatal(result natsclient.DeliveryResult) {`
- `processor/agentic-loop/delivery_owner.go:74` — `func consumeAdmittedDelivery(`
- `processor/agentic-loop/delivery_owner.go:88` — `func newStreamConsumerBinding(handle jetstream.ConsumeContext) streamConsumerBinding {`
- `processor/agentic-loop/delivery_owner.go:99` — `func (c *Component) observeDeliveryLane(`
- `processor/agentic-model/delivery_owner.go:14` — `type deliveryLaneAdmission struct {`
- `processor/agentic-tools/delivery_owner.go:14` — `type deliveryLaneAdmission struct {`
- `processor/agentic-governance/component.go:510` — `admissionOpen := true`
- `processor/agentic-governance/component.go:741` — `func (c *Component) recordDeliveryOwnerFatal(result natsclient.DeliveryResult) {`
- `agentic/agentrun/agentrun.go:679` — `type milestoneConsumerOwner struct {`
- `agentic/agentrun/agentrun.go:683` — `completeDrained bool`
- `agentic/agentrun/agentrun.go:712` — `drainComplete := complete != nil && !o.completeDrained`
- `agentic/agentrun/agentrun.go:727` — `stopErrors = append(stopErrors, waitMilestoneConsumerClosed(ctx, complete.Closed(), "complete"))`

## Adjacent claims

- #1249, #759 (rulings 2026-09-02 items 3/6/7; placement 2026-09-18), #1146 (L-stack), #1155 (Stage D), #1301 (two boot roots), #1330 (L4).
- L0 head `da6a70c0` (`semstreams-wt/claude/gh759-semantic-settlement`): `natsclient/delivery_settlement.go:35` — `type DeliveryWork func(context.Context, []byte) (DeliveryDecision, error)`; `DeliveryAttempt` withdrawn (`266453eb`), `design.md:309-310` rejects re-exposing the count; `tasks.md:27-29` `ServerConfirmed` withdrawn; `NumDelivered` read at `:306` and discarded. Consumer-policy delta ADDS "semantic heartbeat settlement has one permanent exported surface" (`:73-103`): text `:75-84` (`:78-79` "`ConsumeWithHeartbeat` SHALL have no alias and no new production caller, and SHALL be deleted by the PR that migrates its last one (#1249)"), scenarios `:86-91` (`:91` "carries only its ratcheted remaining callers and no alias"), `:93-97`, `:99-103`; REMOVES "Durable settlement composition is stateless" (`:340-342`). Nats-streaming delta ADDS "the legacy helper is a shrinking remainder, never a compatibility surface" (`:61-77`; `:66-67` "Adopters get the removal from `docs/operations/migration-beta162-to-beta163.md`"), scenario `:72-77`; REMOVES "Heartbeat consumption SHALL expose settlement failure" (`:79-84`, Reason `:83` "the removed `ConsumeWithHeartbeat` export"). `docs/operations/migration-beta162-to-beta163.md:1050-1065`: helper "is still exported at this tag" and is deleted by #1249.
- L1 head `c2a9cef6` (`semstreams-wt/claude/gh1327-settle-after-effect`): `5ca936ac` MODIFIES L0's ADDED "shared settlement remains stateless and heartbeat-specific" and records why targeting a stacked layer's text is safe (`design.md` D8) — the pattern § 4 reuses; `657c4734` deletes the loop auto-floor; `1b8598ce` governance `delivery_owner.go`; `SettleDeliveryWithRetry` added. `git -C <L1> grep -n ConsumeWithHeartbeat HEAD -- '*.go' ':!**/*_test.go'` → `agentrun.go:812` + `heartbeat.go` decl/comments only.
- [R4] Base pin → L1 head `c2a9cef6` line, exact-text match, text printed by `sed -n` from the L1 tree (base pins in the three moving files): `natsclient/delivery_settlement.go` `:191`→`:191`, `:329`→`:351` (`if metadata.NumDelivered == 0 {`), `:361`→`:383` (`if err := msg.InProgress(); err != nil {`), `:366`→`:388` (`result.ownerStopNeeded = true`), `:379`→`:401` (`func unavailableDeliveryMetadata(cause error) DeliveryResult {`), `:392`→`:414` (`valid = work.cause == nil`), `:396`→`:418` (`if !valid {`); `processor/agentic-loop/component.go` `:436`→`:436`, `:1129`→`:1154` (`if cfg.MaxDeliver >= len(cfg.BackOff) {`), `:1956`→`:2074` (`PublishToStream`); `processor/agentic-governance/component.go` `:510`→absent (inline latch replaced), `:724`→`:701` (Health; a second `deliveryFatalErr != nil` at `:721` is the idempotence guard inside `recordDeliveryOwnerFatal`), `:741`→`:718`.
- `origin/main` (`4eeaf568`): six production callers incl. `natsclient/consume_durable.go:39`; the removal enumerates at the actual rebase base.
- semdev direct callers `internal/conversationchannel/component.go:476`, `internal/intake/component.go:378`; comment-only sites sizing `max_deliver 10` on the removed 30s NAK: `conversationchannel/apply.go:113`, `:202`, `conversationchannel/component.go:435`, `intake/component.go:355`.
- `docs/adr/053-agent-run-substrate.md:171` — `observation-only and does not stamp milestones or terminal phases. If a product`
- `docs/adr/053-agent-run-substrate.md:188` — `### D6 — Adapter: subscriber + lifted resolution`
- `docs/concepts/33-semantic-settlement.md:99` — `replacement proof. Model and loop work continues under #1146. AgentRun fanout needs its own design because one source`
- `docs/operations/migration-restart-safe-nats-client.md:95` — `The non-default #759 integration branch temporarily retains `ConsumeWithHeartbeat` only as a removal boundary while`
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

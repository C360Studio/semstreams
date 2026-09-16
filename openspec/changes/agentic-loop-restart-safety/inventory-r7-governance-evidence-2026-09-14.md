# Inventory: R7 governance settlement and correlation evidence
base: c347eff487f50b93bc338d764f43ef5b5ea5e133

snapshot: local dirty worktree `/Users/coby/Code/c360/semstreams-wt/codex/gh1146-agentic-loop-restart`; source pins include approved, uncommitted R6 changes. Starting hashes below distinguish the source from base.

## Claimed gap

- `openspec/changes/agentic-loop-restart-safety/tasks.md:229` — `- [ ] R7 Close governance settlement/correlation proof (old 8.1–8.3). Cover allowed/denied/filter-error/panic/budget,`
- `openspec/changes/agentic-loop-restart-safety/tasks.md:230` — `missing/full waiters, exact retained verdicts, and replacement before/after proposal/verdict/tool publication.`
- `openspec/changes/agentic-loop-restart-safety/tasks.md:231` — `All three validation handlers propagate classified outcomes and required PubAcks. Ordinary output remains`
- `openspec/changes/agentic-loop-restart-safety/tasks.md:232` — `at-least-once with no general committed-output lookup. Match RequestID, ExecutionID and proposal fingerprint.`
- `openspec/changes/agentic-loop-restart-safety/tasks.md:234` — `failpoint before proposing additional state. Policy content and framework-wide wire enforcement remain separate.`
- `openspec/changes/agentic-loop-restart-safety/tasks.md:233` — `If retained evidence is insufficient, report the concrete`

## Spellings of the fact

- `processor/agentic-governance/component.go:318` — `case "task_validation":`
- `processor/agentic-governance/component.go:319` — `msgType = MessageTypeTask`
- `processor/agentic-governance/component.go:320` — `outputPortName = "agent.task.validated"`
- `processor/agentic-governance/component.go:321` — `case "request_validation":`
- `processor/agentic-governance/component.go:322` — `msgType = MessageTypeRequest`
- `processor/agentic-governance/component.go:323` — `outputPortName = "agent.request.validated"`
- `processor/agentic-governance/component.go:324` — `case "response_validation":`
- `processor/agentic-governance/component.go:325` — `msgType = MessageTypeResponse`
- `processor/agentic-governance/component.go:326` — `outputPortName = "agent.response.validated"`
- `processor/agentic-governance/component.go:332` — `handler := c.createHandler(msgType, outputPortName)`
- `processor/agentic-governance/component.go:343` — `func (c *Component) createHandler(msgType MessageType, outputPortName string) func(context.Context, []byte) (natsclient.DeliveryDecision, error) {`
- `processor/agentic-governance/component.go:344` — `return func(ctx context.Context, data []byte) (natsclient.DeliveryDecision, error) {`
- `processor/agentic-governance/component.go:345` — `return c.handleMessage(ctx, data, msgType, outputPortName)`
- `processor/agentic-governance/component.go:349` — `func runGovernanceDeliveryWork(`
- `processor/agentic-governance/component.go:352` — `work func(context.Context, []byte) (natsclient.DeliveryDecision, error),`
- `processor/agentic-governance/component.go:353` — `) (decision natsclient.DeliveryDecision, cause error) {`
- `processor/agentic-governance/component.go:356` — `decision = natsclient.DeliveryDecisionQuarantine`
- `processor/agentic-governance/component.go:365` — `func (c *Component) handleMessage(ctx context.Context, data []byte, msgType MessageType, outputPortName string) (natsclient.DeliveryDecision, error) {`
- `processor/agentic-governance/component.go:370` — `return natsclient.DeliveryDecisionTerminate, fmt.Errorf("decode governance message: %w", err)`
- `processor/agentic-governance/component.go:379` — `result, err := c.chain.Process(ctx, &msg)`
- `processor/agentic-governance/component.go:382` — `return natsclient.DeliveryDecisionRetry, fmt.Errorf("process governance filter chain: %w", err)`
- `processor/agentic-governance/component.go:411` — `if !result.Allowed {`
- `processor/agentic-governance/component.go:418` — `return natsclient.DeliveryDecisionAck, nil`
- `processor/agentic-governance/component.go:422` — `result.AddGovernanceMetadata()`
- `processor/agentic-governance/component.go:432` — `outputSubject, resolveErr := component.ResolveSubject(c.outputPortDefs(), outputPortName, msg.ID)`
- `processor/agentic-governance/component.go:435` — `return natsclient.DeliveryDecisionQuarantine, fmt.Errorf("resolve validated output subject: %w", resolveErr)`
- `processor/agentic-governance/component.go:438` — `outputData, err := json.Marshal(outputMsg)`
- `processor/agentic-governance/component.go:441` — `return natsclient.DeliveryDecisionTerminate, fmt.Errorf("marshal validated output: %w", err)`
- `processor/agentic-governance/component.go:444` — `if err := c.natsClient.PublishToStream(ctx, outputSubject, outputData); err != nil {`
- `processor/agentic-governance/component.go:446` — `return natsclient.DeliveryDecisionQuarantine,`
- `processor/agentic-governance/component.go:450` — `return natsclient.DeliveryDecisionAck, nil`
- `processor/agentic-governance/component.go:454` — `func (c *Component) setupConsumer(ctx context.Context, port component.Port, handler func(context.Context, []byte) (natsclient.DeliveryDecision, error)) error {`
- `processor/agentic-governance/component.go:519` — `decision, cause := runGovernanceDeliveryWork(msgCtx, msg.Data(), handler)`
- `processor/agentic-governance/component.go:520` — `result := natsclient.SettleDelivery(msg, decision, cause)`
- `processor/agentic-governance/component.go:521` — `if result.OwnerStopRequired() {`
- `processor/agentic-governance/component.go:524` — `admissionOpen = false`
- `processor/agentic-governance/component.go:525` — `c.recordDeliveryOwnerFatal(result)`
- `processor/agentic-governance/component.go:530` — `if result.Err() != nil && !result.OwnerStopRequired() {`
- `processor/agentic-governance/component.go:545` — `binding.drain()`
- `processor/agentic-loop/governance_dispatcher.go:115` — `RequestID           string         `json:"request_id"``
- `processor/agentic-loop/governance_dispatcher.go:116` — `ExecutionID         string         `json:"execution_id"``
- `processor/agentic-loop/governance_dispatcher.go:119` — `ProposalFingerprint string         `json:"proposal_fingerprint"``
- `processor/agentic-loop/governance_dispatcher.go:137` — `Decision            string         `json:"decision,omitempty"``
- `processor/agentic-loop/governance_dispatcher.go:140` — `RequestID           string         `json:"request_id,omitempty"``
- `processor/agentic-loop/governance_dispatcher.go:141` — `ExecutionID         string         `json:"execution_id,omitempty"``
- `processor/agentic-loop/governance_dispatcher.go:142` — `ProposalFingerprint string         `json:"proposal_fingerprint,omitempty"``
- `processor/agentic-loop/governance_dispatcher.go:144` — `Reason              string         `json:"reason,omitempty"``
- `processor/agentic-loop/governance_dispatcher.go:155` — `if executionID, ok := v.Properties["execution_id"].(string); ok {`
- `processor/agentic-loop/governance_dispatcher.go:185` — `if d, ok := v.Properties["decision"].(string); ok {`
- `processor/agentic-loop/governance_dispatcher.go:199` — `if r, ok := v.Properties["reason"].(string); ok {`
- `processor/agentic-loop/governance_dispatcher.go:278` — `func (d *disabledDispatcher) Propose(_ context.Context, _, _ string, calls []agentic.ToolCall) (DispatcherResult, error) {`
- `processor/agentic-loop/governance_dispatcher.go:282` — `func (d *disabledDispatcher) HandleVerdict(decision, executionID string, _ []byte) (natsclient.DeliveryDecision, error) {`
- `processor/agentic-loop/governance_dispatcher.go:302` — `func (d *auditDispatcher) Propose(ctx context.Context, loopID, parentLoopID string, calls []agentic.ToolCall) (DispatcherResult, error) {`
- `processor/agentic-loop/governance_dispatcher.go:308` — `if err := publishProposed(ctx, d.publisher, loopID, parentLoopID, call, d.logger); err != nil {`
- `processor/agentic-loop/governance_dispatcher.go:319` — `func (d *auditDispatcher) HandleVerdict(decision, executionID string, data []byte) (natsclient.DeliveryDecision, error) {`
- `processor/agentic-loop/governance_dispatcher.go:324` — `_ = json.Unmarshal(data, &payload)`
- `processor/agentic-loop/governance_dispatcher.go:329` — `slog.String("reason", payload.EffectiveReason()))`
- `processor/agentic-loop/governance_dispatcher.go:388` — `func (d *enforceDispatcher) Propose(ctx context.Context, loopID, parentLoopID string, calls []agentic.ToolCall) (DispatcherResult, error) {`
- `processor/agentic-loop/governance_dispatcher.go:399` — `channels[call.ExecutionID] = d.registerWaiter(call.ExecutionID)`
- `processor/agentic-loop/governance_dispatcher.go:413` — `if err := publishProposed(ctx, d.publisher, loopID, parentLoopID, call, d.logger); err != nil {`
- `processor/agentic-loop/governance_dispatcher.go:414` — `publishFailures[call.ExecutionID] = err`
- `processor/agentic-loop/governance_dispatcher.go:426` — `if pubErr, failed := publishFailures[call.ExecutionID]; failed {`
- `processor/agentic-loop/governance_dispatcher.go:435` — `decision, reason := d.awaitVerdict(ctx, channels[call.ExecutionID], call, loopID)`
- `processor/agentic-loop/governance_dispatcher.go:477` — `case <-timer.C:`
- `processor/agentic-loop/governance_dispatcher.go:484` — `case <-ctx.Done():`
- `processor/agentic-loop/governance_dispatcher.go:489` — `func (d *enforceDispatcher) HandleVerdict(decision, executionID string, data []byte) (natsclient.DeliveryDecision, error) {`
- `processor/agentic-loop/governance_dispatcher.go:502` — `_ = json.Unmarshal(data, &payload)`
- `processor/agentic-loop/governance_dispatcher.go:504` — `ch, ok := d.lookupWaiter(executionID)`
- `processor/agentic-loop/governance_dispatcher.go:514` — `return natsclient.DeliveryDecisionRetry, fmt.Errorf("no active governance waiter for execution_id %q", executionID)`
- `processor/agentic-loop/governance_dispatcher.go:520` — `case ch <- verdictArrival{decision: decision, reason: payload.EffectiveReason(), ruleID: payload.RuleID}:`
- `processor/agentic-loop/governance_dispatcher.go:525` — `return natsclient.DeliveryDecisionQuarantine, fmt.Errorf("governance waiter for execution_id %q is full", executionID)`
- `processor/agentic-loop/governance_dispatcher.go:542` — `func publishProposed(ctx context.Context, publisher VerdictPublisher, loopID, parentLoopID string, call agentic.ToolCall, logger *slog.Logger) error {`
- `processor/agentic-loop/governance_dispatcher.go:562` — `RequestID:    call.RequestID,`
- `processor/agentic-loop/governance_dispatcher.go:563` — `ExecutionID:  call.ExecutionID,`
- `processor/agentic-loop/governance_dispatcher.go:585` — `payload.ProposalFingerprint = fingerprint`
- `processor/agentic-loop/governance_dispatcher.go:601` — `subject := "agent.toolcall.proposed." + loopID`
- `processor/agentic-loop/governance_dispatcher.go:602` — `if err := publisher.PublishToStream(ctx, subject, data); err != nil {`
- `processor/agentic-loop/governance_dispatcher.go:608` — `func fingerprintProposedToolCall(payload ProposedToolCallPayload) (string, error) {`
- `processor/agentic-loop/governance_dispatcher.go:609` — `payload.ProposalFingerprint = ""`
- `processor/agentic-loop/governance_dispatcher.go:614` — `sum := sha256.Sum256(data)`
- `processor/agentic-loop/governance_dispatcher.go:615` — `return "sha256:" + hex.EncodeToString(sum[:]), nil`
- `processor/agentic-loop/governance_dispatcher.go:211` — `type GovernanceDispatcher interface {`
- `processor/agentic-loop/governance_dispatcher.go:274` — `type disabledDispatcher struct {`
- `processor/agentic-loop/governance_dispatcher.go:296` — `type auditDispatcher struct {`
- `processor/agentic-loop/governance_dispatcher.go:352` — `type enforceDispatcher struct {`
- `processor/agentic-loop/recovery_test.go:14` — `type contextCapturingGovernanceDispatcher struct {`

- `processor/agentic-governance/component.go:400` — `for _, violation := range result.Violations {`
- `processor/agentic-governance/component.go:401` — `if err := c.violations.Handle(ctx, violation); err != nil {`
- `processor/agentic-governance/component.go:402` — `c.logger.Error("Failed to handle violation",`
- `processor/agentic-governance/component.go:404` — `"violation_id", violation.ID,`
- `processor/agentic-governance/filter_chain.go:72` — `for _, filter := range fc.Filters {`
- `processor/agentic-governance/filter_chain.go:74` — `case <-ctx.Done():`
- `processor/agentic-governance/filter_chain.go:75` — `return nil, ctx.Err()`
- `processor/agentic-governance/filter_chain.go:81` — `filterResult, err := filter.Process(ctx, result.ModifiedMessage)`
- `processor/agentic-governance/filter_chain.go:83` — `return nil, errs.Wrap(err, "FilterChain", "Process", fmt.Sprintf("process filter %s", filter.Name()))`
- `processor/agentic-governance/violation.go:129` — `if err := h.storeViolation(ctx, violation); err != nil {`
- `processor/agentic-governance/violation.go:130` — `h.logger.Error("Failed to store violation", "error", err, "violation_id", violation.ID)`
- `processor/agentic-governance/violation.go:131` — `// Don't fail on storage errors; continue with remaining audit paths.`
- `processor/agentic-governance/violation.go:139` — `if h.shouldAlertAdmin(violation.Severity) {`
- `processor/agentic-governance/violation.go:140` — `if err := h.alertAdmin(ctx, violation); err != nil {`
- `processor/agentic-governance/violation.go:141` — `h.logger.Error("Failed to alert admin", "error", err, "violation_id", violation.ID)`
- `processor/agentic-governance/violation.go:148` — `subject, err := component.ResolveSubject(h.outputs, "violations", violation.FilterName+"."+violation.UserID)`
- `processor/agentic-governance/violation.go:150` — `return errs.WrapInvalid(err, "ViolationHandler", "Handle", "resolve violation subject")`
- `processor/agentic-governance/violation.go:152` — `violationJSON, err := json.Marshal(violation)`
- `processor/agentic-governance/violation.go:154` — `return errs.Wrap(err, "ViolationHandler", "Handle", "marshal violation")`
- `processor/agentic-governance/violation.go:157` — `if err := h.natsClient.Publish(ctx, subject, violationJSON); err != nil {`
- `processor/agentic-governance/violation.go:158` — `return errs.WrapTransient(err, "ViolationHandler", "Handle", "publish violation")`

- `processor/agentic-governance/config.go:189` — `Name: "task_validation", Config: component.JetStreamPort{Subjects: []string{"agent.task.*"}, StreamName: "AGENT"}, Required: true,`
- `processor/agentic-governance/config.go:193` — `Name: "request_validation", Config: component.JetStreamPort{Subjects: []string{"agent.request.*"}, StreamName: "AGENT"}, Required: true,`
- `processor/agentic-governance/config.go:197` — `Name: "response_validation", Config: component.JetStreamPort{Subjects: []string{"agent.response.*"}, StreamName: "AGENT"}, Required: true,`
- `processor/agentic-governance/config.go:204` — `Name: "agent.task.validated", Config: component.JetStreamPort{Subjects: []string{"agent.task.validated.*"}, StreamName: "AGENT"}, Required: true,`
- `processor/agentic-governance/config.go:208` — `Name: "agent.request.validated", Config: component.JetStreamPort{Subjects: []string{"agent.request.validated.*"}, StreamName: "AGENT"}, Required: true,`
- `processor/agentic-governance/config.go:212` — `Name: "agent.response.validated", Config: component.JetStreamPort{Subjects: []string{"agent.response.validated.*"}, StreamName: "AGENT"}, Required: true,`

- `processor/rule/publisher.go:40` — `func (p *actionPublisher) Publish(ctx context.Context, subject string, data []byte) error {`
- `processor/rule/publisher.go:46` — `if p.processor.isJetStreamPortBySubject(subject) {`
- `processor/rule/publisher.go:47` — `publishErr = p.processor.natsClient.PublishToStream(ctx, subject, data)`
- `processor/rule/publisher.go:49` — `publishErr = p.processor.natsClient.Publish(ctx, subject, data)`
- `processor/rule/publisher.go:53` — `return errs.WrapTransient(publishErr, "actionPublisher", "Publish", fmt.Sprintf("publish to %s", subject))`
- `processor/rule/publisher.go:66` — `func (rp *Processor) isJetStreamPortBySubject(subject string) bool {`
- `processor/rule/publisher.go:67` — `for _, port := range rp.outputPorts {`
- `processor/rule/publisher.go:68` — `facts, err := port.Facts()`
- `processor/rule/publisher.go:72` — `if subjects := facts.NATSSubjects(); len(subjects) == 1 && subjects[0] == subject {`
- `processor/rule/publisher.go:73` — `return facts.Kind() == component.PortKindJetStream`
- `processor/rule/publisher.go:76` — `return false`
- `processor/rule/actions.go:1103` — `properties := substituteStringPropertiesContext(ctx, action.Properties, ec)`
- `processor/rule/actions.go:1106` — `payload := map[string]any{`
- `processor/rule/actions.go:1108` — `"subject":    subject,`
- `processor/rule/actions.go:1111` — `"properties": properties,`
- `processor/rule/actions.go:1127` — `data, err := json.Marshal(payload)`
- `processor/rule/actions.go:1132` — `if err := e.publisher.Publish(ctx, subject, data); err != nil {`
- `processor/rule/actions.go:2049` — `func (e *ActionExecutor) emitVerdictAudit(ctx context.Context, decision, ruleID, reason string, ec *ExecutionContext) {`
- `processor/rule/actions.go:2050` — `if e.verdictAuditor == nil {`
- `processor/rule/actions.go:2053` — `ev := governance.VerdictEvent{`
- `processor/rule/actions.go:2055` — `RuleID:    ruleID,`
- `processor/rule/actions.go:2056` — `Reason:    reason,`
- `processor/rule/actions.go:2065` — `ev.LoopID = v`
- `processor/rule/actions.go:2068` — `ev.CallID = v`
- `processor/rule/actions.go:2071` — `if err := e.verdictAuditor.EmitVerdict(ctx, ev); err != nil && e.logger != nil {`
- `processor/rule/actions.go:2072` — `e.logger.Error("governance verdict audit emit failed; verdict still applies",`
- `processor/rule/actions.go:2099` — `subject := ec.SubstituteVariables(ctx, action.Subject)`
- `processor/rule/actions.go:2105` — `reason := ec.SubstituteVariables(ctx, action.Reason)`
- `processor/rule/actions.go:2113` — `e.emitVerdictAudit(ctx, governance.DecisionApprove, ruleID, reason, ec)`
- `processor/rule/actions.go:2161` — `baseMsg := message.NewBaseMessage(generic.Schema(), generic, "rule_engine")`
- `processor/rule/actions.go:2162` — `data, err := json.Marshal(baseMsg)`
- `governance/verdict.go:44` — `const SubjectPrefix = "governance.verdict"`
- `governance/verdict.go:46` — `// VerdictEvent is the registered audit payload for a governance verdict.`
- `governance/verdict.go:53` — `type VerdictEvent struct {`
- `governance/verdict.go:55` — `Decision string `json:"decision"``
- `governance/verdict.go:58` — `RuleID string `json:"rule_id"``
- `governance/verdict.go:60` — `Reason string `json:"reason"``
- `governance/verdict.go:63` — `EntityID string `json:"entity_id,omitempty"``
- `governance/verdict.go:65` — `Timestamp time.Time `json:"timestamp"``
- `governance/verdict.go:67` — `LoopID string `json:"loop_id,omitempty"``
- `governance/verdict.go:69` — `CallID string `json:"call_id,omitempty"``
- `governance/verdict.go:72` — `// Validate implements message.Payload. It is deliberately lenient: the only hard`
- `governance/verdict.go:73` — `// requirements are a recognized decision and a non-empty rule ID. The audit emit`
- `governance/verdict.go:74` — `// is best-effort and must never block a structural verdict, so a permissive`
- `governance/verdict.go:77` — `func (e *VerdictEvent) Validate() error {`
- `governance/verdict.go:79` — `case DecisionDeny, DecisionApprove:`
- `governance/verdict.go:83` — `if e.RuleID == "" {`
- `governance/verdict.go:84` — `return fmt.Errorf("rule_id required")`
- `governance/verdict.go:86` — `return nil`
- `processor/agentic-loop/governance_dispatcher.go:394` — `channels := make(map[string]chan verdictArrival, len(calls))`
- `processor/agentic-loop/governance_dispatcher.go:395` — `for _, call := range calls {`
- `processor/agentic-loop/governance_dispatcher.go:403` — `d.releaseWaiter(call.ExecutionID)`
- `processor/agentic-loop/governance_dispatcher.go:407` — `// Publish all proposed calls. Publish failure on any call is`
- `processor/agentic-loop/governance_dispatcher.go:411` — `publishFailures := map[string]error{}`
- `processor/agentic-loop/governance_dispatcher.go:412` — `for _, call := range calls {`
- `processor/agentic-loop/governance_dispatcher.go:497` — `// Payload is optional audit context — routing relies on the`
- `processor/agentic-loop/governance_dispatcher.go:498` — `// (decision, executionID) pair supplied by the caller. An unmarshal`
- `processor/agentic-loop/governance_dispatcher.go:499` — `// failure here downgrades the verdict reason but doesn't block`

- `processor/rule/message_handler.go:49` — `func (rp *Processor) handleMessage(ctx context.Context, subject string, data []byte) {`
- `processor/rule/message_handler.go:142` — `transition, err := rp.statefulEvaluator.Evaluate(ctx, Evaluation{`
- `processor/rule/message_handler.go:146` — `MessageData:       extractMessageData(msg),`
- `processor/rule/message_handler.go:148` — `if err != nil {`
- `processor/rule/message_handler.go:149` — `rp.logger.Warn("Stateful evaluation failed", "rule_name", ruleName, "error", err)`
- `processor/rule/stateful_evaluator.go:233` — `actions := e.selectActions(ev.Rule, transition, currentlyMatching, recovering, ev.EntityID, ev.RelatedID, iteration)`
- `processor/rule/stateful_evaluator.go:236` — `matchState.FieldValues = captureTransitionFields(ev.Rule, ev.Entity)`
- `processor/rule/stateful_evaluator.go:247` — `persistCtx, cancelPersist := context.WithTimeout(context.WithoutCancel(ctx), statePersistTimeout)`
- `processor/rule/stateful_evaluator.go:249` — `if err := e.stateTracker.Set(persistCtx, *matchState); err != nil {`
- `processor/rule/stateful_evaluator.go:254` — `return transition, err`
- `processor/rule/stateful_evaluator.go:257` — `return transition, nil`
- `processor/rule/stateful_evaluator.go:359` — `// runActions executes each action in the list, skipping those whose When`
- `processor/rule/stateful_evaluator.go:360` — `// clause does not match. Action errors are logged and do not stop subsequent`
- `processor/rule/stateful_evaluator.go:361` — `// actions — the rule engine prefers best-effort execution so one failing`
- `processor/rule/stateful_evaluator.go:368` — `func (e *StatefulEvaluator) runActions(`
- `processor/rule/stateful_evaluator.go:377` — `) {`
- `processor/rule/stateful_evaluator.go:426` — `if err := e.actionExecutor.Execute(ctx, action, ec); err != nil {`
- `processor/rule/stateful_evaluator.go:427` — `if errors.Is(err, ErrDenyVerdict) {`
- `processor/rule/stateful_evaluator.go:435` — `break`
- `processor/rule/stateful_evaluator.go:437` — `e.logger.Error("Failed to execute action",`
- `processor/rule/stateful_evaluator.go:443` — `// Every non-deny action-execution failure is otherwise only a`
- `processor/rule/stateful_evaluator.go:447` — `if e.metrics != nil {`
- `processor/rule/stateful_evaluator.go:448` — `e.metrics.actionFailuresTotal.WithLabelValues(action.Type).Inc()`

## Adjacent claims

- `openspec/changes/agentic-loop-restart-safety/design.md:595` — `Each proposal carries LoopID, RequestID, execution identity, and a proposal fingerprint. Verdict subjects use the`
- `openspec/changes/agentic-loop-restart-safety/design.md:596` — `NATS-safe execution identity. A replacement response handler first checks for an exact matching retained verdict`
- `openspec/changes/agentic-loop-restart-safety/design.md:600` — `completed work. No governance bucket is admitted unless a real replacement failpoint proves that retained verdict`
- `openspec/changes/agentic-loop-restart-safety/design.md:619` — `| 9 | loop `agent.toolcall.approved`; fast | verdict reaches waiter or remains recoverable for response replay | invalid → Terminate; retained lookup unavailable → Retry; mismatch/panic → Quarantine | execution identity, proposal fingerprint, exact retained verdict at waiter-loss boundary |`
- `openspec/changes/agentic-loop-restart-safety/design.md:768` — `Happy-path done is validated proposal identity and delivery to the waiter or exact retained verdict recoverable by`
- `openspec/changes/agentic-loop-restart-safety/design.md:1073` — `8. Governance replacement reads the exact retained verdict or safely re-obtains it. Failure returns for new design.`
- `openspec/changes/agentic-loop-restart-safety/design.md:1117` — `- Governance retained-verdict recovery at the waiter-loss boundary requires the complete R7 evidence. Ordinary`
- `openspec/changes/agentic-loop-restart-safety/specs/agentic-governance/spec.md:58` — `Every proposal SHALL carry LoopID, RequestID, execution identity, and proposal fingerprint. Verdict subjects SHALL`
- `openspec/changes/agentic-loop-restart-safety/specs/agentic-governance/spec.md:60` — `retained verdict before republishing a proposal. Missing or full waiter channels SHALL NOT authorize completed`
- `openspec/changes/agentic-loop-restart-safety/specs/agentic-governance/spec.md:61` — `log-and-drop. No governance bucket SHALL be added unless a named replacement failpoint proves retained verdict and`
- `openspec/changes/agentic-loop-restart-safety/specs/agentic-governance/spec.md:72` — `- **WHEN** retained verdict identity or fingerprint conflicts with the proposal`
- `openspec/changes/agentic-loop-restart-safety/specs/agentic-governance/spec.md:81` — `The exact retained-verdict read exists only at the governance waiter-loss boundary. Ordinary validated outputs and`
- `openspec/changes/agentic-loop-restart-safety/review-r6-fastlane-evidence-2026-09-14.md:3` — `Status: **R6 CLOSEOUT PASS** — independent implementation, evidence and task-truth review complete. Not whole-PR approval.`
- `openspec/changes/agentic-loop-restart-safety/review-r6-fastlane-evidence-2026-09-14.md:28` — `| Approved/rejected verdict ready, missing and full waiter | Six new rows use registered envelopes, actual setup callbacks and the real enforce dispatcher; verify exact execution routing, ACK/NAK/unsettled result, retained queued verdict and exact owner drain. |`
- `openspec/changes/agentic-loop-restart-safety/review-r6-fastlane-evidence-2026-09-14.md:31` — `| Missing-waiter verdict replacement | Native rejected-verdict source is NAKed; a fresh component with the existing waiter receives the same source and routes the decision before ACK. Both physical ports are covered by the six callback rows. |`
- `openspec/changes/agentic-loop-restart-safety/review-r6-fastlane-evidence-2026-09-14.md:35` — `Optional verdict reason decoding and retained-governance proposal/fingerprint proof remain R7.`

- `openspec/changes/agentic-loop-restart-safety/specs/agentic-governance/spec.md:9` — `Each physical subscription SHALL invoke its typed business handler using the callback installed by its production`
- `openspec/changes/agentic-loop-restart-safety/specs/agentic-governance/spec.md:10` — `setup branch. All delivery-derived work SHALL join before the private callback passes its decision and cause to`
- `openspec/changes/agentic-loop-restart-safety/specs/agentic-governance/spec.md:11` — ``natsclient.SettleDelivery`. JetStream consumer configuration owns AckWait and redelivery; governance SHALL NOT`
- `openspec/changes/agentic-loop-restart-safety/specs/agentic-governance/spec.md:12` — `derive a universal work deadline from AckWait. An operation MAY use an ordinary business timeout. A physical`
- `openspec/changes/agentic-loop-restart-safety/specs/agentic-governance/spec.md:16` — `For an allowed message, done SHALL require durable at-least-once publication through the declared JetStream output`
- `openspec/changes/agentic-loop-restart-safety/specs/agentic-governance/spec.md:17` — `and synchronous PubAck. For a blocked message, done SHALL be the completed policy decision and deliberate`
- `openspec/changes/agentic-loop-restart-safety/specs/agentic-governance/spec.md:18` — `non-forwarding. The existing audit contract remains nonblocking, but decode, filter, output-subject, marshal, and`
- `openspec/changes/agentic-loop-restart-safety/specs/agentic-governance/spec.md:19` — `required publication failures SHALL NOT become ACK.`
- `openspec/changes/agentic-loop-restart-safety/specs/agentic-governance/spec.md:29` — `- **WHEN** policy allows a message`
- `openspec/changes/agentic-loop-restart-safety/specs/agentic-governance/spec.md:30` — `- **AND** its declared validated output does not receive PubAck`
- `openspec/changes/agentic-loop-restart-safety/specs/agentic-governance/spec.md:31` — `- **THEN** the source retries and the validated output may repeat`
- `openspec/changes/agentic-loop-restart-safety/specs/agentic-governance/spec.md:32` — `- **AND** no core-NATS fallback authorizes ACK`
- `openspec/changes/agentic-loop-restart-safety/specs/agentic-governance/spec.md:36` — `- **WHEN** policy completes and refuses forwarding`
- `openspec/changes/agentic-loop-restart-safety/specs/agentic-governance/spec.md:37` — `- **THEN** source may be acknowledged because non-forwarding is the terminal consequence`
- `openspec/changes/agentic-loop-restart-safety/specs/agentic-governance/spec.md:38` — `- **AND** audit failure remains observable without reversing the policy decision`
- `openspec/changes/agentic-loop-restart-safety/specs/agentic-governance/spec.md:42` — `- **WHEN** a transient dependency prevents the filter chain from completing`
- `openspec/changes/agentic-loop-restart-safety/specs/agentic-governance/spec.md:43` — `- **THEN** source retries and no log-only return becomes ACK`
- `openspec/changes/agentic-loop-restart-safety/specs/agentic-governance/spec.md:52` — `- **WHEN** a delivery-owned governance operation reaches a timeout required by that operation`
- `openspec/changes/agentic-loop-restart-safety/specs/agentic-governance/spec.md:53` — `- **THEN** its context is cancelled`
- `openspec/changes/agentic-loop-restart-safety/specs/agentic-governance/spec.md:54` — `- **AND** all operation work joins before the callback settles or returns`
- `openspec/changes/agentic-loop-restart-safety/design.md:958` — `| Governance author | Supply approve/reject policy only | Late verdict remains recoverable; invalid conflict is refused | typed outcome and telemetry | No waiter, subject, or replay mechanics |`

- `processor/agentic-governance/README.md:33` — `- `agent.task.validated.*` - Approved tasks`
- `processor/agentic-governance/README.md:34` — `- `agent.request.validated.*` - Approved requests`
- `processor/agentic-governance/README.md:35` — `- `agent.response.validated.*` - Approved responses`
- `openspec/changes/agentic-loop-restart-safety/specs/agentic-governance/spec.md:21` — `The first owner-fatal result across governance validation owners SHALL synchronously latch before exact-handle drain.`
- `openspec/changes/agentic-loop-restart-safety/specs/agentic-governance/spec.md:22` — `Existing Health SHALL report `Healthy=false`, status `delivery ownership lost`, and the exact first cause in`
- `openspec/changes/agentic-loop-restart-safety/specs/agentic-governance/spec.md:23` — ``LastError`. The existing cumulative error count SHALL increase exactly once for owner loss, independently of prior`
- `openspec/changes/agentic-loop-restart-safety/specs/agentic-governance/spec.md:24` — `business-error counts; later owner-fatal results SHALL not increment it again. No new metric family, public state,`
- `openspec/changes/agentic-loop-restart-safety/specs/agentic-governance/spec.md:25` — `durable state, or communication path is added.`
- `openspec/changes/agentic-loop-restart-safety/specs/agentic-governance/spec.md:77` — `Every validated task, request, response, proposal, and verdict publication SHALL carry its lane's required`
- `openspec/changes/agentic-loop-restart-safety/specs/agentic-governance/spec.md:78` — `correlation and receive PubAck before source ACK. PubAck uncertainty MAY repeat a publication. `Nats-Msg-Id` MAY`
- `openspec/changes/agentic-loop-restart-safety/specs/agentic-governance/spec.md:79` — `provide bounded duplicate suppression but SHALL NOT be treated as permanent publication identity.`
- `openspec/changes/agentic-loop-restart-safety/specs/agentic-governance/spec.md:82` — `proposals require no exact committed-output lookup. Conflicting proposal or verdict correlation SHALL quarantine;`
- `openspec/changes/agentic-loop-restart-safety/specs/agentic-governance/spec.md:88` — `- **WHEN** validation input redelivers after its validated output was published`
- `openspec/changes/agentic-loop-restart-safety/specs/agentic-governance/spec.md:89` — `- **THEN** governance may publish the correlated validated output again`
- `openspec/changes/agentic-loop-restart-safety/specs/agentic-governance/spec.md:90` — `- **AND** acknowledges only after the required publication receives PubAck`
- `openspec/changes/agentic-loop-restart-safety/specs/agentic-governance/spec.md:94` — `- **WHEN** the first validated-output PubAck is uncertain`
- `openspec/changes/agentic-loop-restart-safety/specs/agentic-governance/spec.md:95` — `- **THEN** retry may repeat the correlated validated output`
- `openspec/changes/agentic-loop-restart-safety/specs/agentic-governance/spec.md:96` — `- **AND** source ACK still waits for PubAck`

- `configs/agentic.json:240` — `"kind": "jetstream",`
- `configs/agentic.json:241` — `"stream_name": "AGENT",`
- `configs/agentic.json:243` — `"agent.toolcall.approved.*"`
- `configs/agentic.json:247` — `"name": "toolcall_approved_out"`
- `configs/agentic.json:251` — `"kind": "jetstream",`
- `configs/agentic.json:252` — `"stream_name": "AGENT",`
- `configs/agentic.json:254` — `"agent.toolcall.rejected.*"`
- `configs/agentic.json:258` — `"name": "toolcall_rejected_out"`

- `configs/agentic.json:195` — `"kind": "jetstream",`
- `configs/agentic.json:196` — `"stream_name": "AGENT",`
- `configs/agentic.json:198` — `"agent.toolcall.proposed.>"`
- `configs/agentic.json:202` — `"name": "rule_toolcall_proposed_in"`
- #935 — root-supplied live `gh issue view 935`: best-effort action continuation; `horizon:post-v1`; no milestone. No independent GitHub query in this supplement.
- #1145 — root-supplied live scope: restart metadata/conformance, beta.165. No independent GitHub query in this supplement.
- #1146 — root-supplied scope reconciliation: active parent proposal lists nine heartbeat bindings and excludes the rule input; frozen fifteen-binding census likewise excludes the rule input. No ownership ruling recorded by this inventory.

## Consumers

- `processor/agentic-loop/component.go:2565` — `payload, ok := decodeVerdictPayload(c.decoder, data)`
- `processor/agentic-loop/component.go:2570` — `decision := payload.EffectiveDecision()`
- `processor/agentic-loop/component.go:2571` — `executionID := payload.effectiveExecutionID()`
- `processor/agentic-loop/component.go:2577` — `return dispatcher.HandleVerdict(decision, executionID, data)`
- `processor/agentic-loop/component.go:2580` — `// decodeVerdictPayload reads a VerdictPayload from wire bytes,`
- `processor/agentic-loop/component.go:2593` — `func decodeVerdictPayload(decoder *message.Decoder, data []byte) (VerdictPayload, bool) {`
- `processor/agentic-loop/component.go:2596` — `if baseMsg, err := decoder.Decode(data); err == nil {`
- `processor/agentic-loop/component.go:2597` — `if generic, ok := baseMsg.Payload().(*message.GenericJSONPayload); ok {`
- `processor/agentic-loop/component.go:2598` — `return verdictPayloadFromMap(generic.Data), true`
- `processor/agentic-loop/component.go:2607` — `if err := json.Unmarshal(data, &raw); err == nil {`
- `processor/agentic-loop/component.go:2614` — `// verdictPayloadFromMap translates a GenericJSONPayload.Data map into`
- `processor/agentic-loop/component.go:2618` — `func verdictPayloadFromMap(data map[string]any) VerdictPayload {`
- `processor/agentic-loop/component.go:2620` — `if v, ok := data["decision"].(string); ok {`
- `processor/agentic-loop/component.go:2621` — `p.Decision = v`
- `processor/agentic-loop/component.go:2629` — `if v, ok := data["request_id"].(string); ok {`
- `processor/agentic-loop/component.go:2630` — `p.RequestID = v`
- `processor/agentic-loop/component.go:2632` — `if v, ok := data["execution_id"].(string); ok {`
- `processor/agentic-loop/component.go:2633` — `p.ExecutionID = v`
- `processor/agentic-loop/component.go:2635` — `if v, ok := data["proposal_fingerprint"].(string); ok {`
- `processor/agentic-loop/component.go:2636` — `p.ProposalFingerprint = v`
- `processor/agentic-loop/component.go:2641` — `if v, ok := data["reason"].(string); ok {`
- `processor/agentic-loop/component.go:2642` — `p.Reason = v`
- `processor/agentic-loop/handlers.go:1407` — `govResult, gErr := h.governanceDispatcher.Propose(ctx, loopID, parentLoopID, toolCalls)`
- `processor/agentic-loop/handlers.go:1415` — `for _, rejection := range govResult.Rejected {`
- `processor/agentic-loop/handlers.go:1420` — `Error:       fmt.Sprintf("tool call rejected by governance: %s", rejection.Reason),`
- `processor/agentic-loop/handlers.go:1430` — `approved = govResult.Approved`
- `processor/agentic-loop/handlers.go:1693` — `func (h *MessageHandler) dispatchToolCall(result *HandlerResult, loopID string, tc agentic.ToolCall) error {`
- `processor/rule/actions.go:2148` — `for _, field := range []string{"request_id", "execution_id", "proposal_fingerprint"} {`
- `processor/rule/actions.go:2150` — `payloadData[field] = v`
- `processor/rule/actions.go:2160` — `generic := message.NewGenericJSON(payloadData)`
- `processor/rule/actions.go:2164` — `return fmt.Errorf("marshal approve verdict payload: %w", err)`
- `processor/rule/actions.go:2166` — `if err := e.publisher.Publish(ctx, subject, data); err != nil {`
- `processor/agentic-loop/execution_identity_test.go:165` — `decision, err := dispatcher.HandleVerdict("approved", executionID, payload)`
- `processor/agentic-loop/execution_identity_test.go:170` — `result, err := dispatcher.Propose(context.Background(), "loop-a", "", calls)`
- `processor/agentic-loop/governance_dispatcher.go:581` — `fingerprint, err := fingerprintProposedToolCall(payload)`
- `processor/agentic-loop/governance_dispatcher.go:608` — `func fingerprintProposedToolCall(payload ProposedToolCallPayload) (string, error) {`
- `processor/agentic-loop/governance_dispatcher_test.go:39` — `decision, err := disabled.HandleVerdict("approved", "call-disabled", nil)`
- `processor/agentic-loop/governance_dispatcher_test.go:44` — `decision, err = audit.HandleVerdict("approved", "call-audit", nil)`
- `processor/agentic-loop/governance_dispatcher_test.go:53` — `decision, err = enforce.HandleVerdict("approved", "missing", nil)`
- `processor/agentic-loop/governance_dispatcher_test.go:58` — `decision, err = enforce.HandleVerdict("approved", "delivered", nil)`
- `processor/agentic-loop/governance_dispatcher_test.go:66` — `decision, err = enforce.HandleVerdict("rejected", "full", nil)`
- `processor/agentic-loop/governance_dispatcher_test.go:109` — `result, err := d.Propose(context.Background(), "loop-1", "", calls)`
- `processor/agentic-loop/governance_dispatcher_test.go:132` — `result, err := d.Propose(context.Background(), "loop-abc", "parent-loop", calls)`
- `processor/agentic-loop/governance_dispatcher_test.go:191` — `result, err := d.Propose(context.Background(), "loop-1", "", calls)`
- `processor/agentic-loop/governance_dispatcher_test.go:218` — `d.HandleVerdict("approved", "execution-call-001", payload)`
- `processor/agentic-loop/governance_dispatcher_test.go:221` — `result, err := d.Propose(context.Background(), "loop-1", "", calls)`
- `processor/agentic-loop/governance_dispatcher_test.go:245` — `d.HandleVerdict("rejected", "execution-call-001", payload)`
- `processor/agentic-loop/governance_dispatcher_test.go:248` — `result, err := d.Propose(context.Background(), "loop-1", "", calls)`
- `processor/agentic-loop/governance_dispatcher_test.go:273` — `result, err := d.Propose(context.Background(), "loop-1", "", calls)`
- `processor/agentic-loop/governance_dispatcher_test.go:313` — `d.HandleVerdict("approved", "execution-c3", approvedPayload)`
- `processor/agentic-loop/governance_dispatcher_test.go:314` — `d.HandleVerdict("rejected", "execution-c2", rejectedPayload)`
- `processor/agentic-loop/governance_dispatcher_test.go:315` — `d.HandleVerdict("approved", "execution-c1", approvedPayload)`
- `processor/agentic-loop/governance_dispatcher_test.go:318` — `result, err := d.Propose(context.Background(), "loop-1", "", calls)`
- `processor/agentic-loop/governance_dispatcher_test.go:353` — `d.HandleVerdict("approved", "execution-c1", payload)`
- `processor/agentic-loop/governance_dispatcher_test.go:356` — `result, err := d.Propose(context.Background(), "loop-1", "", calls)`
- `processor/agentic-loop/governance_dispatcher_test.go:385` — `d.HandleVerdict("approved", "execution-fast-call", payload)`
- `processor/agentic-loop/governance_dispatcher_test.go:388` — `result, err := d.Propose(context.Background(), "loop-1", "", calls)`
- `processor/agentic-loop/governance_dispatcher_test.go:407` — `result, err := d.Propose(context.Background(), "loop-1", "",`
- `processor/agentic-loop/governance_dispatcher_test.go:414` — `d.HandleVerdict("approved", "execution-late-call", payload)`
- `processor/agentic-loop/governance_dispatcher_test.go:467` — `d.HandleVerdict("approved", "execution-c1", payload)`
- `processor/agentic-loop/governance_dispatcher_test.go:470` — `_, err := d.Propose(context.Background(), "loop-1", "", calls)`
- `processor/agentic-loop/governance_dispatcher_test.go:491` — `_, err := d.Propose(context.Background(), "loop-1", "", calls)`
- `processor/agentic-loop/governance_dispatcher_test.go:514` — `_, err := d.Propose(context.Background(), "loop-1", "",`
- `processor/agentic-loop/governance_dispatcher_test.go:521` — `d.HandleVerdict("approved", "execution-late-call", payload)`
- `processor/agentic-governance/delivery_settlement_integration_test.go:25` — `func TestIntegrationGovernanceProductionCallbacksPublishBeforeAck(t *testing.T) {`
- `processor/agentic-governance/delivery_settlement_integration_test.go:57` — `{port: "task_validation", msgType: MessageTypeTask, subject: "agent.task.validated.message-0", messageID: "message-0"},`
- `processor/agentic-governance/delivery_settlement_integration_test.go:58` — `{port: "request_validation", msgType: MessageTypeRequest, subject: "agent.request.validated.message-1", messageID: "message-1"},`
- `processor/agentic-governance/delivery_settlement_integration_test.go:59` — `{port: "response_validation", msgType: MessageTypeResponse, subject: "agent.response.validated.message-2", messageID: "message-2"},`
- `processor/agentic-governance/delivery_settlement_integration_test.go:67` — `require.Equal(t, int32(1), msg.acks.Load())`
- `processor/agentic-governance/delivery_settlement_integration_test.go:68` — `require.Zero(t, msg.naks.Load()+msg.terms.Load())`
- `processor/agentic-governance/delivery_settlement_integration_test.go:71` — `require.Equal(t, row.subject, published.subject)`
- `processor/agentic-governance/delivery_settlement_integration_test.go:72` — `require.Equal(t, row.msgType, published.message.Type)`
- `processor/agentic-governance/delivery_settlement_integration_test.go:73` — `require.Equal(t, row.messageID, published.message.ID)`
- `processor/agentic-governance/delivery_settlement_test.go:25` — `func TestGovernanceAllowedPublicationFailureQuarantinesExactOwner(t *testing.T) {`
- `processor/agentic-governance/delivery_settlement_test.go:45` — `require.Zero(t, msg.acks.Load()+msg.naks.Load()+msg.terms.Load())`
- `processor/agentic-governance/delivery_settlement_test.go:49` — `require.Zero(t, handle.drains.Load(), "publish failure drained unrelated owner %s", port)`
- `processor/agentic-governance/delivery_settlement_test.go:52` — `require.Contains(t, c.Health().LastError, "unknown durable publication state")`
- `processor/agentic-governance/delivery_settlement_test.go:82` — `func TestGovernanceProductionCallbacksTerminateMalformedInputs(t *testing.T) {`
- `processor/agentic-governance/delivery_settlement_test.go:99` — `require.Zero(t, msg.acks.Load(), "%s must not ACK malformed input", port)`
- `processor/agentic-governance/delivery_settlement_test.go:100` — `require.Zero(t, msg.naks.Load(), "%s immutable malformed input must not retry", port)`
- `processor/agentic-governance/delivery_settlement_test.go:101` — `require.Equal(t, int32(1), msg.terms.Load(), "%s immutable malformed input must terminate", port)`
- `processor/agentic-governance/delivery_settlement_test.go:106` — `func TestGovernanceProductionCallbackPanicLatchesFirstFatalAndDrainsExactOwner(t *testing.T) {`
- `processor/agentic-governance/delivery_settlement_test.go:127` — `require.Zero(t, msg.acks.Load()+msg.naks.Load()+msg.terms.Load())`
- `processor/agentic-governance/delivery_settlement_test.go:131` — `require.Zero(t, handle.drains.Load(), "panic drained unrelated owner %s", port)`
- `processor/agentic-governance/delivery_settlement_test.go:136` — `require.Equal(t, "delivery ownership lost", health.Status)`
- `processor/agentic-governance/delivery_settlement_test.go:137` — `require.Equal(t, 5, health.ErrorCount)`
- `processor/agentic-governance/delivery_settlement_test.go:138` — `require.Contains(t, health.LastError, "governance delivery work panicked")`
- `processor/agentic-governance/delivery_settlement_test.go:143` — `require.Equal(t, health.LastError, later.LastError)`
- `processor/agentic-governance/delivery_settlement_test.go:144` — `require.Equal(t, health.ErrorCount, later.ErrorCount)`
- `processor/agentic-governance/filter_chain_test.go:34` — `func TestFilterChain_FailFastPolicy(t *testing.T) {`
- `processor/agentic-governance/filter_chain_test.go:55` — `func TestFilterChain_ContinuePolicy(t *testing.T) {`
- `processor/agentic-governance/filter_chain_test.go:80` — `func TestFilterChain_LogOnlyPolicy(t *testing.T) {`
- `processor/agentic-governance/filter_chain_test.go:115` — `func TestFilterChain_AllFiltersPass(t *testing.T) {`
- `processor/agentic-governance/filter_chain_test.go:130` — `func TestFilterChain_ContextCancellation(t *testing.T) {`
- `processor/agentic-loop/execution_identity_test.go:132` — `func TestGovernanceProposalCarriesFrameworkExecutionCorrelation(t *testing.T) {`
- `processor/agentic-loop/execution_identity_test.go:142` — `require.Equal(t, call.RequestID, payload.RequestID)`
- `processor/agentic-loop/execution_identity_test.go:143` — `require.Equal(t, call.ExecutionID, payload.ExecutionID)`
- `processor/agentic-loop/execution_identity_test.go:144` — `require.Equal(t, call.CallOrdinal, payload.CallOrdinal)`
- `processor/agentic-loop/execution_identity_test.go:149` — `func TestGovernanceWaitersSeparateRepeatedProviderCallID(t *testing.T) {`
- `processor/agentic-loop/execution_identity_test.go:167` — `require.Equal(t, natsclient.DeliveryDecisionAck, decision)`
- `processor/agentic-loop/execution_identity_test.go:173` — `require.Equal(t, "provider-call", result.Approved[0].ID)`
- `processor/agentic-loop/execution_identity_test.go:174` — `require.Equal(t, "provider-call", result.Approved[1].ID)`
- `processor/agentic-loop/governance_dispatcher_test.go:35` — `func TestGovernanceDispatcherHandleVerdictDeclaresDeliveryOutcome(t *testing.T) {`
- `processor/agentic-loop/governance_dispatcher_test.go:99` — `func TestDispatcher_DisabledModePassThroughNoPublish(t *testing.T) {`
- `processor/agentic-loop/governance_dispatcher_test.go:120` — `func TestDispatcher_AuditModePublishesAndPassesThrough(t *testing.T) {`
- `processor/agentic-loop/governance_dispatcher_test.go:184` — `func TestDispatcher_AuditModeIgnoresPublishFailure(t *testing.T) {`
- `processor/agentic-loop/governance_dispatcher_test.go:198` — `func TestDispatcher_EnforceModeWaitsForApproveVerdict(t *testing.T) {`
- `processor/agentic-loop/governance_dispatcher_test.go:229` — `func TestDispatcher_EnforceModeRejectsOnDenyVerdict(t *testing.T) {`
- `processor/agentic-loop/governance_dispatcher_test.go:254` — `assert.Contains(t, result.Rejected[0].Reason, "bash disallowed")`
- `processor/agentic-loop/governance_dispatcher_test.go:255` — `assert.Contains(t, result.Rejected[0].Reason, "block-bash")`
- `processor/agentic-loop/governance_dispatcher_test.go:261` — `func TestDispatcher_EnforceModeFailsClosedOnTimeout(t *testing.T) {`
- `processor/agentic-loop/governance_dispatcher_test.go:279` — `assert.Contains(t, result.Rejected[0].Reason, "timeout")`
- `processor/agentic-loop/governance_dispatcher_test.go:289` — `func TestDispatcher_EnforceModeMixedVerdictsPreserveOrder(t *testing.T) {`
- `processor/agentic-loop/governance_dispatcher_test.go:332` — `func TestDispatcher_EnforceModePartialPublishFailure(t *testing.T) {`
- `processor/agentic-loop/governance_dispatcher_test.go:363` — `assert.Contains(t, result.Rejected[0].Reason, "publish failed")`
- `processor/agentic-loop/governance_dispatcher_test.go:369` — `func TestDispatcher_EnforceModeVerdictBeforeSelectArrival(t *testing.T) {`
- `processor/agentic-loop/governance_dispatcher_test.go:397` — `func TestDispatcher_EnforceModeLateVerdictIsNoOp(t *testing.T) {`
- `processor/agentic-loop/governance_dispatcher_test.go:453` — `func TestDispatcher_EnforceModeRecordsApprovedVerdictMetric(t *testing.T) {`
- `processor/agentic-loop/governance_dispatcher_test.go:480` — `func TestDispatcher_EnforceModeRecordsTimeoutVerdictMetric(t *testing.T) {`
- `processor/agentic-loop/governance_dispatcher_test.go:503` — `func TestDispatcher_LateVerdictIncrementsMissingWaiterMetric(t *testing.T) {`
- `processor/agentic-loop/governance_dispatcher_test.go:529` — `func TestDecisionFromVerdictSubject(t *testing.T) {`
- `processor/agentic-loop/governance_dispatcher_test.go:558` — `func TestVerdictPayload_EffectiveAccessors(t *testing.T) {`
- `processor/agentic-loop/governance_dispatcher_test.go:561` — `t.Run("top-level shape (approve action)", func(t *testing.T) {`
- `processor/agentic-loop/governance_dispatcher_test.go:570` — `assert.Equal(t, "policy permits", p.EffectiveReason())`
- `processor/agentic-loop/governance_dispatcher_test.go:573` — `t.Run("nested shape (publish action)", func(t *testing.T) {`
- `processor/agentic-loop/governance_dispatcher_test.go:584` — `assert.Equal(t, "blocked", p.EffectiveReason())`
- `processor/agentic-loop/governance_dispatcher_test.go:587` — `t.Run("top-level wins over nested", func(t *testing.T) {`
- `processor/agentic-loop/governance_dispatcher_test.go:595` — `assert.Equal(t, "top-level", p.EffectiveCallID())`
- `processor/agentic-loop/delivery_owner_test.go:673` — `func TestLoopProductionVerdictCallbacksSettleRealWaiterOutcomes(t *testing.T) {`
- `processor/agentic-loop/handlers.go:1783` — `toolMsg := message.NewBaseMessage(tc.Schema(), &tc, "agentic-loop")`
- `processor/agentic-loop/handlers.go:1788` — `toolSubject, err := component.ResolveSubject(h.config.Ports.Outputs, "tool.execute", tc.Name)`
- `processor/agentic-loop/handlers.go:1804` — `result.PublishedMessages = append(result.PublishedMessages, PublishedMessage{`
- `processor/agentic-loop/component.go:2298` — `for _, msg := range result.PublishedMessages {`
- `processor/agentic-loop/component.go:2300` — `if err := c.natsClient.PublishToStream(ctx, msg.Subject, msg.Data); err != nil {`

- `processor/agentic-governance/delivery_settlement_integration_test.go:43` — `c.waitForStreamInput = func(context.Context, string) error { return nil }`
- `processor/agentic-governance/delivery_settlement_integration_test.go:45` — `c.consumeStream = func(_ context.Context, owner natsclient.PortConsumerContext, _ natsclient.StreamConsumerConfig, callback func(context.Context, jetstream.Msg)) (jetstream.ConsumeContext, error) {`
- `processor/agentic-governance/delivery_settlement_integration_test.go:46` — `callbacks[owner.Port] = callback`
- `processor/agentic-governance/delivery_settlement_integration_test.go:47` — `return &governanceSettlementHandle{closed: make(chan struct{})}, nil`
- `processor/agentic-governance/delivery_settlement_integration_test.go:49` — `require.NoError(t, c.setupInputConsumers(ctx))`
- `processor/agentic-governance/delivery_settlement_integration_test.go:64` — `for attempt := range 2 {`
- `processor/agentic-governance/delivery_settlement_integration_test.go:65` — `msg := &governanceSettlementMsg{data: data}`
- `processor/agentic-governance/delivery_settlement_integration_test.go:66` — `callbacks[row.port](ctx, msg)`
- `processor/agentic-governance/delivery_settlement_integration_test.go:69` — `select {`
- `processor/agentic-governance/delivery_settlement_integration_test.go:70` — `case published := <-outputs:`
- `processor/agentic-governance/filter_chain_test.go:134` — `ctx, cancel := context.WithCancel(context.Background())`
- `processor/agentic-governance/filter_chain_test.go:135` — `cancel() // Cancel immediately`
- `processor/agentic-governance/filter_chain_test.go:138` — `_, err := chain.Process(ctx, msg)`
- `processor/agentic-governance/filter_chain_test.go:141` — `assert.ErrorIs(t, err, context.Canceled)`

- `processor/rule/processor.go:737` — `publisher := newActionPublisher(rp)`
- `processor/rule/processor.go:741` — `actionExecutor = NewActionExecutorComplete(rp.logger, mutator, publisher, kvWriter, rp.platform)`
- `processor/rule/processor.go:744` — `actionExecutor = NewActionExecutorComplete(rp.logger, nil, publisher, kvWriter, rp.platform)`
- `processor/rule/publisher.go:104` — `if rp.isJetStreamPortBySubject(event.subject) {`
- `processor/rule/publisher.go:215` — `if rp.isJetStreamPortBySubject(subject) {`
- `processor/agentic-loop/settlement_recovery.go:37` — `func (r natsLoopSettlementEvidenceReader) ReadAgentRequest(`
- `processor/agentic-loop/settlement_recovery.go:40` — `return r.readExact(ctx, streamName, subject)`
- `processor/agentic-loop/settlement_recovery.go:43` — `func (r natsLoopSettlementEvidenceReader) ReadAgentResponse(`
- `processor/agentic-loop/settlement_recovery.go:46` — `return r.readExact(ctx, streamName, subject)`
- `processor/agentic-loop/execution_identity_test.go:145` — `require.NotEmpty(t, payload.ProposalFingerprint)`
- `processor/rule/actions_test.go:593` — `Subject: "agent.toolcall.rejected.$message.execution_id",`
- `processor/rule/actions_test.go:596` — `"request_id":           "$message.request_id",`
- `processor/rule/actions_test.go:597` — `"execution_id":         "$message.execution_id",`
- `processor/rule/actions_test.go:599` — `"proposal_fingerprint": "$message.proposal_fingerprint",`
- `processor/rule/actions_test.go:602` — `"reason":               "writes outside worktree blocked",`
- `processor/rule/actions_test.go:608` — `require.NoError(t, executor.executePublish(ctx, action, ec))`
- `processor/rule/actions_test.go:613` — `"agent.toolcall.rejected.tool-exec-001",`
- `processor/rule/actions_test.go:618` — `require.NoError(t, json.Unmarshal(got.data, &payload))`
- `processor/rule/actions_test.go:620` — `props, ok := payload["properties"].(map[string]any)`
- `processor/rule/actions_test.go:623` — `assert.Equal(t, "request-001", props["request_id"])`
- `processor/rule/actions_test.go:624` — `assert.Equal(t, "tool-exec-001", props["execution_id"])`
- `processor/rule/actions_test.go:627` — `assert.Equal(t, "sha256:proposal", props["proposal_fingerprint"])`
- `processor/rule/actions_test.go:3351` — `Subject: "agent.toolcall.approved.$message.execution_id",`
- `processor/rule/actions_test.go:3352` — `Reason:  "policy permits",`
- `processor/rule/actions_test.go:3355` — `err := executor.executeApprove(ctx, action, ec)`
- `processor/rule/actions_test.go:3360` — `assert.Equal(t, "agent.toolcall.approved.execution-001", got.subject,`
- `processor/rule/actions_test.go:3372` — `require.NoError(t, json.Unmarshal(got.data, &envelope))`
- `processor/rule/actions_test.go:3376` — `assert.Equal(t, "approved", envelope.Payload.Data["decision"])`
- `processor/rule/actions_test.go:3377` — `assert.Equal(t, "approve-rule-pub", envelope.Payload.Data["rule_id"])`
- `processor/rule/actions_test.go:3378` — `assert.Equal(t, "policy permits", envelope.Payload.Data["reason"])`
- `processor/rule/actions_test.go:3379` — `assert.Equal(t, "request-001", envelope.Payload.Data["request_id"])`
- `processor/rule/actions_test.go:3380` — `assert.Equal(t, "execution-001", envelope.Payload.Data["execution_id"])`
- `processor/rule/actions_test.go:3381` — `assert.Equal(t, "sha256:proposal", envelope.Payload.Data["proposal_fingerprint"])`

- `processor/rule/processor.go:1151` — `sub, err := rp.natsClient.Subscribe(ctx, subject, func(msgCtx context.Context, msg *nats.Msg) {`
- `processor/rule/processor.go:1152` — `rp.handleMessage(msgCtx, msg.Subject, msg.Data)`
- `processor/rule/processor.go:1214` — `handle, err := rp.natsClient.ConsumeStreamWithConfig(ctx, natsclient.PortConsumerContext{Component: rp.Meta().Name, Port: port.Name}, cfg, func(msgCtx context.Context, msg jetstream.Msg) {`
- `processor/rule/processor.go:1215` — `rp.handleMessage(msgCtx, subject, msg.Data())`
- `processor/rule/processor.go:1216` — `if ackErr := msg.Ack(); ackErr != nil {`
- `processor/rule/processor.go:1217` — `rp.logger.Error("Failed to ack JetStream message", "error", ackErr)`
- `processor/rule/stateful_evaluator.go:234` — `e.runActions(ctx, ev.Rule, ec, actions, ev.Entity, stateFields, expression.MessageFields(ev.MessageData), ev.EntityID, ev.RelatedID)`
- `processor/rule/stateful_evaluator_test.go:1379` — `// TestRunActions_NonDenyErrorContinues verifies that a non-deny error from one`
- `processor/rule/stateful_evaluator_test.go:1380` — `// action does NOT stop subsequent actions. Today's best-effort semantics preserved.`
- `processor/rule/stateful_evaluator_test.go:1383` — `func TestRunActions_NonDenyErrorContinues(t *testing.T) {`
- `processor/rule/stateful_evaluator_test.go:1391` — `errors.New("transient publish error"), // action 0: non-deny failure`
- `processor/rule/stateful_evaluator_test.go:1402` — `{Type: ActionTypePublish, Subject: "first.fails"},`
- `processor/rule/stateful_evaluator_test.go:1403` — `{Type: ActionTypePublish, Subject: "second.runs"}, // must still run`
- `processor/rule/stateful_evaluator_test.go:1407` — `_, err := evaluator.Evaluate(ctx, Evaluation{`
- `processor/rule/stateful_evaluator_test.go:1413` — `t.Fatalf("Evaluate() error = %v, want nil", err)`
- `processor/rule/stateful_evaluator_test.go:1417` — `if exec.calls != 2 {`
- `processor/rule/actions_test.go:3463` — `// TestExecuteApprove_PublishFailureReturnsError diverges from audit-failure`
- `processor/rule/actions_test.go:3464` — `// handling: publish failure DOES return an error because downstream`
- `processor/rule/actions_test.go:3465` — `// consumers never learned the verdict. The caller (rule processor) will`
- `processor/rule/actions_test.go:3466` — `// log + retry per its action-error policy.`
- `processor/rule/actions_test.go:3467` — `func TestExecuteApprove_PublishFailureReturnsError(t *testing.T) {`
- `processor/rule/actions_test.go:3472` — `pub := &mockPublisher{err: errors.New("jetstream timeout")}`
- `processor/rule/actions_test.go:3481` — `err := executor.executeApprove(ctx, action, ec)`
- `processor/rule/actions_test.go:3482` — `require.Error(t, err)`
- `processor/rule/actions_test.go:3483` — `assert.Contains(t, err.Error(), "publish approve verdict")`

## Problem shape

- `processor/agentic-loop/settlement_recovery.go:29` — `ReadAgentRequest(context.Context, string, string) (retainedLoopMessage, bool, error)`
- `processor/agentic-loop/settlement_recovery.go:30` — `ReadAgentResponse(context.Context, string, string) (retainedLoopMessage, bool, error)`
- `processor/agentic-loop/settlement_recovery.go:49` — `func (r natsLoopSettlementEvidenceReader) readExact(`
- `processor/agentic-loop/settlement_recovery.go:56` — `raw, err := stream.GetLastMsgForSubject(ctx, subject)`
- `processor/agentic-loop/settlement_recovery.go:57` — `if errors.Is(err, jetstream.ErrMsgNotFound) {`
- `processor/agentic-loop/settlement_recovery.go:63` — `return retainedLoopMessage{subject: raw.Subject, data: append([]byte(nil), raw.Data...)}, true, nil`
- `natsclient/client.go:970` — `func (m *Client) publishToStream(ctx context.Context, subject string, data []byte, msgID string) error {`
- `natsclient/client.go:1005` — `_, err = js.PublishMsg(ctx, msg)`
- `natsclient/client.go:943` — `return m.publishToStream(ctx, subject, data, "")`

## Searches

- Contract read: `cat .agents/contracts/semstreams-explorer.md` in `/Users/coby/Code/c360/semstreams`.
- Snapshot: `git rev-parse HEAD`; `git status --short`; 7 modified source/spec files and 3 untracked R6 artifacts observed; inventory output was not present.
- Project read: `sed -n '1,95p' openspec/project.md`; Purpose/Product Boundary used; no design judgment.
- Brief read: `sed -n '220,242p' openspec/changes/agentic-loop-restart-safety/tasks.md`.
- Successful structural calls after initial attempts used `GOCACHE=/private/tmp/semstreams-r7-gopls-cache GOPLSCACHE=/private/tmp/semstreams-r7-gopls-cache`; the two initial GOCACHE-only retries are recorded explicitly.
- Count suffix for grouped literal searches: `| awk '{print} END {print "HITS=" NR}'`; three initial searches were separately recounted with `| wc -l`.
- `git ls-files 'processor/agentic-governance/*' 'processor/agentic-loop/*governance*' 'openspec/changes/agentic-loop-restart-safety/*review*' 'openspec/changes/agentic-loop-restart-safety/*inventory*'` → 79; recounted with wc -l.
- `gopls workspace_symbol -matcher=fuzzy handleValidation` → FAILED: default Go build-cache permission; no usable enumeration.
- `gopls workspace_symbol -matcher=fuzzy Governance` → FAILED: default Go build-cache permission; no usable enumeration.
- `GOCACHE=/private/tmp/semstreams-r7-gopls-cache gopls workspace_symbol -matcher=fuzzy handleValidation` → 2; GOPLS cache-write diagnostics; neither pin on named validation surface.
- `GOCACHE=/private/tmp/semstreams-r7-gopls-cache gopls workspace_symbol -matcher=fuzzy Governance` → 100; workspace-symbol result limit; GOPLS cache-write diagnostics.
- `git grep -n -E 'decision|reason|request_id|execution_id|proposal_fingerprint|fingerprint|Lookup|lookup|Publish|Outcome' -- processor/agentic-governance/component.go processor/agentic-loop/governance_dispatcher.go` → 99; recounted with wc -l.
- `git grep -n -E 'func .*handle|func Test|retained|Retained|lookup|Lookup|PublishToStream|DeliveryDecision|budget|panic' -- processor/agentic-governance/component.go processor/agentic-governance/delivery_settlement_test.go processor/agentic-governance/delivery_settlement_integration_test.go processor/agentic-loop/governance_dispatcher_test.go processor/agentic-loop/delivery_owner_test.go processor/agentic-loop/trajectory_handler_wiring.go processor/agentic-loop/component.go` → 231; initial output truncated, count-only repeat 231; R2–R6 surrounding matches not inventoried.
- `git grep -n -E 'retained.*[Vv]erdict|[Vv]erdict.*retained|decodeVerdict|fingerprintProposed|agent\.toolcall\.(approved|rejected|proposed)' -- processor/agentic-loop agentic processor/rule` → 50.
- `gopls implementation processor/agentic-loop/governance_dispatcher.go:211:6` → 5; includes test implementer delivery_owner_test.go:661 and recovery_test.go:14.
- `gopls references processor/agentic-governance/component.go:343:21` → 1; component.go:332.
- `git grep -n -i -E 'retained.*verdict|verdict.*retained|lookup.*verdict|verdict.*lookup' -- '*.go'` → 2; governance_dispatcher.go:381 and unrelated rule/matches_test.go:658.
- `git grep -n -E 'createHandler|MessageType|Timeout|budget|Budget' -- processor/agentic-governance/component.go processor/agentic-governance/filter_chain.go processor/agentic-governance/filter_chain_test.go processor/agentic-governance/delivery_settlement_test.go processor/agentic-governance/delivery_settlement_integration_test.go` → 13.
- `git grep -n -E 'Verdict|verdict' -- natsclient` → 25; unrelated storage vocabulary only.
- `git grep -n -E 'ProposalFingerprint|proposal_fingerprint|proposal-fingerprint|PROPOSAL_FINGERPRINT|RequestID|request_id|ExecutionID|execution_id' -- governance/verdict.go processor/rule/actions.go processor/rule/action_payloads.go processor/agentic-loop/governance_dispatcher.go` → 41.
- `git grep -n -E 'GetLastMsg|GetMsg|Retained|retained|lookup|Lookup' -- processor/agentic-loop/continuation*.go processor/agentic-loop/*authority*.go` → FAILED: zsh no matches found for continuation*.go; printed HITS=0 is not a search result.
- `git grep -n -E 'governance|verdict' -- openspec/changes/agentic-loop-restart-safety/design.md openspec/changes/agentic-loop-restart-safety/specs/agentic-loop/spec.md openspec/specs/agentic-loop/spec.md` → 40.
- `gopls references processor/agentic-loop/governance_dispatcher.go:222:2` → 15.
- `gopls references processor/agentic-loop/governance_dispatcher.go:230:2` → 17.
- `gopls references processor/agentic-loop/governance_dispatcher.go:608:6` → 1.
- `gopls workspace_symbol -matcher=fuzzy LookupVerdict` → 0.
- `git grep -n -E 'func .*PublishToStream|js.Publish\(' -- 'natsclient/*.go' ':!natsclient/*_test.go'` → 6.
- `gh issue list --search 'governance' --state open --json number,title` → FAILED: error connecting to api.github.com; 0 records returned is not an absence result.
- `gh pr list --state open --json number,title,body --jq '.[] | select(.body | test("governance|1146";"i"))'` → FAILED: error connecting to api.github.com; 0 records returned is not an absence result.
- `openspec list` → 2 active changes: agentic-loop-restart-safety 13/22; semantic-jetstream-settlement 44/67.
- `git grep -n -E 'case "(task_validation|request_validation|response_validation)"|msgType =|outputPortName =|handler := c.createHandler|return c.handleMessage|DeliveryDecision|c.chain.Process|if !result.Allowed|result.AddGovernanceMetadata|component.ResolveSubject|json.Marshal\(outputMsg\)|PublishToStream|runGovernanceDeliveryWork|SettleDelivery|OwnerStopRequired|admissionOpen = false|recordDeliveryOwnerFatal|binding.drain\(' -- processor/agentic-governance/component.go` → 42.
- `git grep -n -E 'json:"(decision|reason|request_id|execution_id|proposal_fingerprint)|v.Properties\["(decision|reason|execution_id)"\]|func .*Dispatcher.*(Propose|HandleVerdict)|func publishProposed|func fingerprintProposedToolCall|channels\[call.ExecutionID\]|publishFailures\[call.ExecutionID\]|publishProposed\(ctx|d.awaitVerdict|ctx.Done\(\)|timer.C|json.Unmarshal\(data, &payload\)|lookupWaiter\(executionID\)|case ch <-|DeliveryDecision(Retry|Quarantine)|RequestID:|ExecutionID:|ProposalFingerprint =|sha256|payloadBytes|sum\[:\]|subject :=|publisher.PublishToStream|payload.EffectiveReason' -- processor/agentic-loop/governance_dispatcher.go` → 43.
- `git grep -n -E 'decodeVerdictPayload|verdictPayloadFromMap|payload.EffectiveDecision|payload.effectiveExecutionID|dispatcher.HandleVerdict|decoder.Decode\(data\)|baseMsg.Payload\(\).*GenericJSONPayload|json.Unmarshal\(data, &raw\)|data\["(decision|reason|request_id|execution_id|proposal_fingerprint)"\]|p\.(Decision|Reason|RequestID|ExecutionID|ProposalFingerprint) =' -- processor/agentic-loop/component.go` → 26.
- `git grep -n -E 'governanceDispatcher.Propose|govResult.Rejected|govResult.Approved|tool call rejected by governance|func .*dispatchToolCall|h.toolPublisher|PublishToolCall|publisher.Publish\(ctx, subject, data\)|\[\]string\{"request_id", "execution_id", "proposal_fingerprint"\}|payloadData\[field\]|message.NewGenericJSON\(payloadData\)|marshal approve verdict' -- processor/agentic-loop/handlers.go processor/rule/actions.go` → 12.
- `git grep -n -E '^func Test|ports :=|port:|require\.(Equal|Zero|Contains)|assert\.(Equal|Zero|Contains)|t.Run\(' -- processor/agentic-governance/delivery_settlement_test.go processor/agentic-governance/delivery_settlement_integration_test.go processor/agentic-governance/filter_chain_test.go processor/agentic-loop/governance_dispatcher_test.go processor/agentic-loop/execution_identity_test.go` → 154.
- `git grep -n -E 'GetLastMsg|GetMsg|retained.*[Vv]erdict|[Vv]erdict.*retained|[Ll]ookup.*[Vv]erdict|[Vv]erdict.*[Ll]ookup' -- 'processor/agentic-loop/*.go' ':!processor/agentic-loop/*_test.go'` → 2.
- `git grep -n -i -E 'budget|deadline|filter.?error|filter.?failure|replacement|restart|before.*pub|after.*pub' -- 'processor/agentic-governance/*test.go' 'processor/agentic-loop/*governance*test.go'` → 10.
- `git grep -n -E 'func .*PublishToStream|js.Publish\(|func .*SettleDelivery|DeliveryDecisionQuarantine|DeliveryDecisionRetry|OwnerStopRequired' -- natsclient/jetstream.go natsclient/delivery.go` → 0.
- `git grep -n -F -e 'R7 Close governance' -e 'missing/full waiters, exact retained verdicts' -e 'All three validation handlers' -e 'Match RequestID, ExecutionID and proposal fingerprint.' -e 'failpoint before proposing additional state.' -- openspec/changes/agentic-loop-restart-safety/tasks.md` → 5.
- `git grep -n -i -E 'retained.verdict|proposal.fingerprint' -- openspec/specs docs/adr openspec/changes 'docs/operations/migration-*.md' ':!**/inventory*.md' ':!**/review*.md'` → 20.
- `git grep -n -E 'requestID|requestId|request-id|REQUEST_ID|executionID|executionId|execution-id|EXECUTION_ID|proposalFingerprint|proposal-fingerprint|PROPOSAL_FINGERPRINT' -- processor/agentic-loop/governance_dispatcher.go` → 17.
- `git grep -n -F -e 'func (r natsLoopSettlementEvidenceReader) readExact(' -e 'ReadAgentRequest(context.Context' -e 'ReadAgentResponse(context.Context' -e 'raw, err := stream.GetLastMsgForSubject(ctx, subject)' -e 'errors.Is(err, jetstream.ErrMsgNotFound)' -e 'retainedLoopMessage{subject:' -- processor/agentic-loop/settlement_recovery.go` → 6.
- `git grep -n -E '\.Propose\(|\.HandleVerdict\(|fingerprintProposedToolCall\(' -- processor/agentic-loop/governance_dispatcher.go processor/agentic-loop/governance_dispatcher_test.go processor/agentic-loop/execution_identity_test.go processor/agentic-loop/handlers.go processor/agentic-loop/component.go` → 34.
- `git grep -n -F -e 'type GovernanceDispatcher interface' -e 'type disabledDispatcher struct' -e 'type auditDispatcher struct' -e 'type enforceDispatcher struct' -e 'type fastLaneCapturingDispatcher struct' -e 'type contextCapturingGovernanceDispatcher struct' -- processor/agentic-loop/governance_dispatcher.go processor/agentic-loop/delivery_owner_test.go processor/agentic-loop/recovery_test.go` → 5.
- `git grep -n -E 'func .*publishToStream|PublishMsg\(ctx|pubAck|PubAck' -- natsclient/client.go` → 8.

Starting SHA-256, captured before enumeration of the approved dirty files (`shasum -a 256` with these exact paths):

```text
f825eab0036e8fe3dccfcd296f1213ed25ae5299c1e0ed208cbe196220059165  processor/agentic-governance/component.go
cab3e9fc7672ca8e72e07b30e35db671fc0ba9e27201930f84cddd82253e493f  processor/agentic-governance/delivery_settlement_test.go
1b45323a3ecce000e7241da51690a4a68ee5e486a275d3770244f8dcae2630f4  processor/agentic-governance/delivery_settlement_integration_test.go
934d1c6905626eaa65640062e5542fad0953cdcfede21c4d309e59a224e6b67e  processor/agentic-governance/filter.go
fb4bcfb17fecac3e58c1eeaa33d58ce167353288f17576470735aee432552e58  processor/agentic-governance/filter_chain.go
9ff88f288c6013deec20de0de58606b903b43be0edfd2c61e02d31b3d8fa5008  processor/agentic-loop/component.go
4bcc9d02b38c8d57cecff74959a1ccf064bcf1a70898df2b4b8b3c78ca99c863  processor/agentic-loop/governance_dispatcher.go
9e7ec99b964596a63a986e3abeb585524f6c4143f987e994a6338b6ab2bd5ed1  processor/agentic-loop/governance_dispatcher_test.go
76c124c18e0787735469b0f9b97efe2cc71e1bcc4488f437da50eb8174b1ac80  processor/agentic-loop/trajectory_handler_wiring.go
a3b646ea2e1844038b120d594225e9281bc0b47b6c7dfa1f49b52e664bcab557  processor/agentic-loop/delivery_owner_test.go
68adfb63d9de2ba1855e7eb398db4a5820018b32eabacd9a7788448289c0000c  processor/agentic-loop/fastlane_replacement_integration_test.go
842ef9e6407d63e3841d8938fa2a5ba16eee89572e17c3fa7abb3d718e85e79e  openspec/changes/agentic-loop-restart-safety/tasks.md
77c9f8bca171701f5e9133735578be4b185577d319358447b160c7e16826bf2f  openspec/changes/agentic-loop-restart-safety/design.md
5c9991222962e8ab60a10cd8745c082cf54f6924475308cc835a2585c14395dc  openspec/changes/agentic-loop-restart-safety/specs/agentic-loop/spec.md
e60a692cb5a6111e825733c396fb70157a4246afc36f6e415bb948878ec77d02  openspec/changes/agentic-loop-restart-safety/review-r6-fastlane-evidence-2026-09-14.md
```

Additional read-time hashes (`shasum -a 256`), no changes observed or made to these files:

```text
46e59e01e016498c0516f1d34667f6675e4cfa56cfaa3f9d8dc2f42e8db3e2ec  processor/agentic-loop/settlement_recovery.go
32d3694527023fd91d506cba201d2ab35201f4a57c71f96c63c5087eb76c969f  processor/agentic-loop/handlers.go
c50068059fb3ce4aff217ed552d02a3796c3896c691565ab4453e77d786d6941  processor/rule/actions.go
9f07a68b0ad8785fc68d025ab9c35335573bd996e8eb1361d48ca957960ac776  natsclient/client.go
```

Pin-window reads, all recorded:

- `sed -n '340,570p' processor/agentic-governance/component.go`.
- `sed -n '105,215p' processor/agentic-loop/governance_dispatcher.go`.
- `sed -n '2555,2655p' processor/agentic-loop/component.go`.
- `sed -n '1380,1460p' processor/agentic-loop/handlers.go`.
- `sed -n '2050,2165p' processor/rule/actions.go`.
- `sed -n '1,40p' openspec/changes/agentic-loop-restart-safety/review-r6-fastlane-evidence-2026-09-14.md`.
- `sed -n '1,85p' processor/agentic-loop/fastlane_replacement_integration_test.go`.
- `nl -ba processor/agentic-loop/settlement_recovery.go | sed -n '20,100p'`.
- `nl -ba processor/agentic-loop/handlers.go | sed -n '1690,1775p'`.
- `nl -ba processor/agentic-loop/component.go | sed -n '2285,2337p'`.
- `nl -ba natsclient/client.go | sed -n '940,960p'`.
- `nl -ba processor/agentic-loop/handlers.go | sed -n '1780,1815p'`.


### Bounded validation-owner supplement, 2026-09-14

- Prior 39 search entries preserved. Supplements add two literal searches; total search entries: 41.
- `git grep -n -E 'PubAck|publish|publication|validation|Governance author|Governance' -- openspec/changes/agentic-loop-restart-safety/specs/agentic-governance/spec.md openspec/changes/agentic-loop-restart-safety/design.md | awk '{print} END {print "HITS=" NR}'` → 190; output truncated. Only the named validation requirement and existing governance-author seam are pinned in this supplement; other matches were not inventoried.
- `nl -ba processor/agentic-governance/component.go | sed -n '396,419p'` → 24-line pin window.
- `nl -ba processor/agentic-governance/filter_chain.go | sed -n '63,87p'` → 25-line pin window.
- `nl -ba processor/agentic-governance/violation.go | sed -n '129,169p'` → 41-line pin window.
- `nl -ba processor/agentic-governance/delivery_settlement_integration_test.go | sed -n '37,75p'` → 39-line pin window.
- `nl -ba processor/agentic-governance/filter_chain_test.go | sed -n '128,143p'` → 16-line pin window.
- `nl -ba openspec/changes/agentic-loop-restart-safety/specs/agentic-governance/spec.md | sed -n '1,58p'` → 58-line pin window.
- `nl -ba openspec/changes/agentic-loop-restart-safety/design.md | sed -n '950,963p'` → 14-line pin window.
- `tail -16 openspec/changes/agentic-loop-restart-safety/inventory-r7-governance-evidence-2026-09-14.md` → prior NOT RUN entries read unchanged.
- Existing cancellation test invocation is `chain.Process(ctx, msg)` at `filter_chain_test.go:138`; setup uses immediate caller cancellation at lines 134–135. No production callback or business-budget proof is attributed to that direct-chain test.
- Initial pin verification before supplement: `task inventory:verify -- openspec/changes/agentic-loop-restart-safety/inventory-r7-governance-evidence-2026-09-14.md` → pins=251 ok=251 moved=0 ambiguous=0 drift=0 malformed=0 unparsed=0.
- Supplement-start hashes: `shasum -a 256` with the exact paths below.

```text
f825eab0036e8fe3dccfcd296f1213ed25ae5299c1e0ed208cbe196220059165  processor/agentic-governance/component.go
fb4bcfb17fecac3e58c1eeaa33d58ce167353288f17576470735aee432552e58  processor/agentic-governance/filter_chain.go
012f715b83adaa42aae07d4a7952f543f0a3fd692f8505874027a3b545743203  processor/agentic-governance/violation.go
1b45323a3ecce000e7241da51690a4a68ee5e486a275d3770244f8dcae2630f4  processor/agentic-governance/delivery_settlement_integration_test.go
95f3b5a23516ffd08b2d85f64537297089aaf4684ff403a7f0997c3b01ba8724  processor/agentic-governance/filter_chain_test.go
cc101634201f9208150ed714a0c0afa94cd1d183718bbb0f1ce9b884f3feb161  openspec/changes/agentic-loop-restart-safety/inventory-r7-governance-evidence-2026-09-14.md
f3bb69f2dabc6028451dd8ce1b29a7ece5a29e2e8c12a31d279a3790f022c554  openspec/changes/agentic-loop-restart-safety/specs/agentic-governance/spec.md
77c9f8bca171701f5e9133735578be4b185577d319358447b160c7e16826bf2f  openspec/changes/agentic-loop-restart-safety/design.md
```



- Adopter-supplement literal search: `git grep -n -E 'task_validation|request_validation|response_validation|agent.task.validated|agent.request.validated|agent.response.validated|NATS client|natsClient == nil' -- processor/agentic-governance/component.go processor/agentic-governance/config.go processor/agentic-governance/README.md docs/operations/migration-agentic-loop-restart-safety.md | awk '{print} END {print "HITS=" NR}'` → 16. No migration-file absence conclusion.
- `nl -ba openspec/changes/agentic-loop-restart-safety/specs/agentic-governance/spec.md | sed -n '75,96p'` → 22-line pin window.
- `nl -ba processor/agentic-governance/config.go | sed -n '187,215p'` → 29-line pin window.
- Adopter-supplement hashes: `shasum -a 256 processor/agentic-governance/config.go processor/agentic-governance/README.md openspec/changes/agentic-loop-restart-safety/specs/agentic-governance/spec.md`.

```text
0a4245440a759eba86b9ca06bf3c2d28fc1e4efddb9313000fbc75acc523c948  processor/agentic-governance/config.go
08d3269b07261d914e6bff8608638ebef66bf1d10341d8d8ca4d8f123374ce7e  processor/agentic-governance/README.md
f3bb69f2dabc6028451dd8ce1b29a7ece5a29e2e8c12a31d279a3790f022c554  openspec/changes/agentic-loop-restart-safety/specs/agentic-governance/spec.md
```


- Supplement verification: `task inventory:verify -- openspec/changes/agentic-loop-restart-safety/inventory-r7-governance-evidence-2026-09-14.md` → pins=331 ok=331 moved=0 ambiguous=0 drift=0 malformed=0 unparsed=0.
- Post-supplement `shasum -a 256 processor/agentic-governance/component.go processor/agentic-governance/filter_chain.go processor/agentic-governance/violation.go processor/agentic-governance/delivery_settlement_integration_test.go processor/agentic-governance/filter_chain_test.go` → all five hashes equal supplement-start values above.


### Bounded full-R7 boundary supplement, 2026-09-14

- Starting base: `git rev-parse HEAD` → c347eff487f50b93bc338d764f43ef5b5ea5e133.
- Starting inventory SHA-256: `f1d4c1c00e26f1cc7d17fb71ebd1ae3ee37c8ae7515c911273b829f206d67289`. Root-provided scoped-review copy: `/Users/coby/Code/c360/semstreams-wt/gh1146-rescue-checkpoint.y3bWGc/r7-governance.xEUmEf/inventory-reviewed.md`; no write to that path.
- Prior validation snapshot pins remain unchanged while another agent owns `processor/agentic-governance/component.go` and `delivery_settlement_test.go`. They may drift from the live worktree; this supplement does not refresh them.
- Four structural queries and three literal searches appended to the prior 41 entries: total search entries 48. Prior NOT RUN entries remain historical; the exact `ProposalFingerprint` field-reference and `readExact` caller questions are now enumerated below.
- `GOCACHE=/private/tmp/semstreams-r7-gopls-cache GOPLSCACHE=/private/tmp/semstreams-r7-gopls-cache gopls references processor/rule/publisher.go:66:22` → 3.
- `GOCACHE=/private/tmp/semstreams-r7-gopls-cache GOPLSCACHE=/private/tmp/semstreams-r7-gopls-cache gopls references processor/agentic-loop/settlement_recovery.go:49:43` → 2.
- `GOCACHE=/private/tmp/semstreams-r7-gopls-cache GOPLSCACHE=/private/tmp/semstreams-r7-gopls-cache gopls references processor/agentic-loop/governance_dispatcher.go:119:2` → 3.
- `GOCACHE=/private/tmp/semstreams-r7-gopls-cache GOPLSCACHE=/private/tmp/semstreams-r7-gopls-cache gopls references processor/agentic-loop/governance_dispatcher.go:142:2` → 1.
- `git grep -n -F -e 'isJetStreamPortBySubject(' -- processor/rule/publisher.go | awk '{print} END {print "HITS=" NR}'` → 4.
- `git grep -n -E 'RequestID|request_id|ExecutionID|execution_id|ProposalFingerprint|proposal_fingerprint|proposal-fingerprint' -- governance/verdict.go | awk '{print} END {print "HITS=" NR}'` → 0.
- `git grep -n -F -e 'ProposalFingerprint' -e 'proposal_fingerprint' -- processor/agentic-loop/component.go processor/agentic-loop/governance_dispatcher.go processor/agentic-loop/execution_identity_test.go | awk '{print} END {print "HITS=" NR}'` → 7.
- Existing pins re-read without replacement: loop `component.go:2577`, `2636`; `governance_dispatcher.go:399`, `413`, `502`, `520`, `585`, `609`; retained reader `settlement_recovery.go:49`, `56`.
- `nl -ba processor/rule/publisher.go | sed -n '35,80p'` → 46-line pin window.
- `nl -ba processor/rule/processor.go | sed -n '732,746p'` → 15-line pin window.
- `nl -ba configs/agentic.json | sed -n '237,258p'` → 22-line pin window.
- `nl -ba processor/rule/actions.go | sed -n '1095,1138p'` → 44-line pin window.
- `nl -ba processor/agentic-loop/component.go | sed -n '2559,2578p'` → 20-line pin window.
- `nl -ba processor/agentic-loop/governance_dispatcher.go | sed -n '388,420p'` → 33-line pin window.
- `nl -ba processor/agentic-loop/governance_dispatcher.go | sed -n '489,522p'` → 34-line pin window.
- `nl -ba governance/verdict.go | sed -n '48,88p'` → 41-line pin window.
- `nl -ba processor/agentic-loop/settlement_recovery.go | sed -n '35,47p'` → 13-line pin window.
- `nl -ba governance/verdict.go | sed -n '24,47p'` → 24-line pin window.
- `nl -ba processor/rule/actions.go | sed -n '2047,2077p'` → 31-line pin window.
- `nl -ba processor/rule/actions.go | sed -n '2092,2114p'` → 23-line pin window.
- `nl -ba processor/rule/actions.go | sed -n '2160,2170p'` → 11-line pin window.
- `nl -ba processor/rule/actions_test.go | sed -n '580,627p'` → 48-line pin window.
- `nl -ba processor/rule/actions_test.go | sed -n '3340,3381p'` → 42-line pin window.
- Full-boundary supplement starting hashes: `shasum -a 256` with the exact paths below. `processor/rule/actions_test.go` hash was captured with its first pin-window reads.

```text
f1d4c1c00e26f1cc7d17fb71ebd1ae3ee37c8ae7515c911273b829f206d67289  openspec/changes/agentic-loop-restart-safety/inventory-r7-governance-evidence-2026-09-14.md
f1bc6d11b4b3ac204f607c32c02ab9a82e0863ddc47bc46df66fb518cf3f8f42  processor/rule/publisher.go
304483872688a57a1bc76a504aad3cbf75a8729c03b13ec93e6ef514cd02ed8b  processor/rule/processor.go
c50068059fb3ce4aff217ed552d02a3796c3896c691565ab4453e77d786d6941  processor/rule/actions.go
144a85fbcfd0dfe63f73cfd50ef4ca20344e635ea84ae2fd0fa83cbbf808f4d1  configs/agentic.json
9ff88f288c6013deec20de0de58606b903b43be0edfd2c61e02d31b3d8fa5008  processor/agentic-loop/component.go
4bcc9d02b38c8d57cecff74959a1ccf064bcf1a70898df2b4b8b3c78ca99c863  processor/agentic-loop/governance_dispatcher.go
46e59e01e016498c0516f1d34667f6675e4cfa56cfaa3f9d8dc2f42e8db3e2ec  processor/agentic-loop/settlement_recovery.go
a4e5d64803d379c14c92475c69189a539b3c4e0dfcfd7fadaddde779936d3153  governance/verdict.go
f825eab0036e8fe3dccfcd296f1213ed25ae5299c1e0ed208cbe196220059165  processor/agentic-governance/component.go
cab3e9fc7672ca8e72e07b30e35db671fc0ba9e27201930f84cddd82253e493f  processor/agentic-governance/delivery_settlement_test.go
4b43f56ed0bf6a6613a10e0a2af51474094749f414685ce248b733a1d0ee6e7f  processor/rule/actions_test.go
```


- Full-boundary verification: `task inventory:verify -- openspec/changes/agentic-loop-restart-safety/inventory-r7-governance-evidence-2026-09-14.md` → pins=430 ok=413 moved=14 ambiguous=0 drift=3 malformed=0 unparsed=0; task exit 201 / verifier exit 1. All reported mismatches are in the separately owned validation files; no validation pin was refreshed.

```text
MOVED processor/agentic-governance/component.go:446→435
DRIFT processor/agentic-governance/delivery_settlement_test.go:25
MOVED processor/agentic-governance/delivery_settlement_test.go:45→139
DRIFT processor/agentic-governance/delivery_settlement_test.go:49
DRIFT processor/agentic-governance/delivery_settlement_test.go:52
MOVED processor/agentic-governance/delivery_settlement_test.go:82→94
MOVED processor/agentic-governance/delivery_settlement_test.go:99→111
MOVED processor/agentic-governance/delivery_settlement_test.go:100→112
MOVED processor/agentic-governance/delivery_settlement_test.go:101→113
MOVED processor/agentic-governance/delivery_settlement_test.go:106→118
MOVED processor/agentic-governance/delivery_settlement_test.go:127→139
MOVED processor/agentic-governance/delivery_settlement_test.go:131→143
MOVED processor/agentic-governance/delivery_settlement_test.go:136→148
MOVED processor/agentic-governance/delivery_settlement_test.go:137→149
MOVED processor/agentic-governance/delivery_settlement_test.go:138→150
MOVED processor/agentic-governance/delivery_settlement_test.go:143→155
MOVED processor/agentic-governance/delivery_settlement_test.go:144→156
```

- Final inventory digest command: `shasum -a 256 openspec/changes/agentic-loop-restart-safety/inventory-r7-governance-evidence-2026-09-14.md`; digest is returned in the handoff, with no subsequent inventory write.


### Final bounded rule-input settlement supplement, 2026-09-14

- `git rev-parse HEAD` → c347eff487f50b93bc338d764f43ef5b5ea5e133. Starting inventory SHA-256: `4dd984d5a676ba8ffbe9bc559fd36b7754d36bd80348dc3dc4772645e9a611e7`.
- Prior 48 search entries preserved; two structural queries and one literal search appended: total search entries 51. Historical validation pins remain untouched.
- `GOCACHE=/private/tmp/semstreams-r7-gopls-cache GOPLSCACHE=/private/tmp/semstreams-r7-gopls-cache gopls references processor/rule/stateful_evaluator.go:234:4` → 1, `stateful_evaluator.go:234`.
- `GOCACHE=/private/tmp/semstreams-r7-gopls-cache GOPLSCACHE=/private/tmp/semstreams-r7-gopls-cache gopls references processor/rule/processor.go:1215:6` → 2, `processor.go:1152`, `processor.go:1215`.
- `git grep -n -F -e 'rule_toolcall_proposed_in' -e 'transient publish error' -e 'TestRunActions_NonDenyErrorContinues' -e 'TestExecuteApprove_PublishFailureReturnsError' -- configs/agentic.json processor/rule/stateful_evaluator_test.go processor/rule/actions_test.go | awk '{print} END {print "HITS=" NR}'` → 6.
- `nl -ba processor/rule/processor.go | sed -n '1198,1222p'` → 25-line pin window.
- `nl -ba configs/agentic.json | sed -n '185,207p'` → 23-line pin window.
- `nl -ba processor/rule/message_handler.go | sed -n '126,158p'` → 33-line pin window.
- `nl -ba processor/rule/stateful_evaluator.go | sed -n '228,261p'` → 34-line pin window.
- `nl -ba processor/rule/stateful_evaluator.go | sed -n '414,451p'` → 38-line pin window.
- `nl -ba processor/rule/stateful_evaluator_test.go | sed -n '1376,1418p'` → 43-line pin window.
- `nl -ba processor/rule/actions_test.go | sed -n '3459,3497p'` → 39-line pin window.
- `nl -ba processor/rule/message_handler.go | sed -n '1,29p'` → 29-line pin window.
- `nl -ba processor/rule/stateful_evaluator.go | sed -n '320,365p'` → 46-line pin window.
- `nl -ba processor/rule/processor.go | sed -n '1148,1156p'` → 9-line pin window.
- `nl -ba processor/rule/message_handler.go | sed -n '35,53p'` → 19-line pin window.
- `nl -ba processor/rule/stateful_evaluator.go | sed -n '365,381p'` → 17-line pin window.
- External issue/scope entries in this supplement reproduce root-supplied reads only. No `gh` query, issue update, scope assignment, or ownership ruling was performed here.
- Starting hashes: `shasum -a 256` with the exact paths below.

```text
4dd984d5a676ba8ffbe9bc559fd36b7754d36bd80348dc3dc4772645e9a611e7  openspec/changes/agentic-loop-restart-safety/inventory-r7-governance-evidence-2026-09-14.md
304483872688a57a1bc76a504aad3cbf75a8729c03b13ec93e6ef514cd02ed8b  processor/rule/processor.go
bd4958b5b570cf937b35c34a85c6d1f583d4d3460a502748f41597968c9ee0cb  processor/rule/message_handler.go
d99301763e2ed76fde3e445284c1a62e09cc748e7810f105d109ed5b511c9053  processor/rule/stateful_evaluator.go
4ae32ef9243e3de0da0b1b597ccc6371563d24265f7d90adf5ffedf73bf068bf  processor/rule/stateful_evaluator_test.go
4b43f56ed0bf6a6613a10e0a2af51474094749f414685ce248b733a1d0ee6e7f  processor/rule/actions_test.go
144a85fbcfd0dfe63f73cfd50ef4ca20344e635ea84ae2fd0fa83cbbf808f4d1  configs/agentic.json
```

- Final check commands after this last inventory edit: `task inventory:verify -- openspec/changes/agentic-loop-restart-safety/inventory-r7-governance-evidence-2026-09-14.md`; `shasum -a 256 openspec/changes/agentic-loop-restart-safety/inventory-r7-governance-evidence-2026-09-14.md`. Results are returned in the handoff; no further inventory writes.


NOT RUN:

- NOT RUN — gopls references for every individual VerdictPayload RequestID/ExecutionID/ProposalFingerprint/Decision/Reason field, decodeVerdictPayload, verdictPayloadFromMap and effective accessors (literal readers enumerated above).
- NOT RUN — gopls references/call_hierarchy for a named exact retained-governance-verdict lookup: no declaration located by the recorded LookupVerdict/retained-verdict/GetLastMsg searches; callers unresolved.
- NOT RUN — gopls references/call_hierarchy for natsLoopSettlementEvidenceReader.readExact and rule ActionExecutor.executeApprove, plus rule publisher implementers; bounded adjacent pins only.
- NOT RUN — Budget and filter-error production-callback proof searches beyond the recorded governance test paths; deny/allowed direct-handler tests in component_test.go.
- NOT RUN — Replacement before/after each proposal, verdict and tool publication across all loop integration-test files; recorded R6 review reused, no new replacement matrix enumeration.
- NOT RUN — docs/adr governance/tool-call spelling sweep beyond retained.verdict and proposal.fingerprint; docs/operations/migration subject/field spellings; no zero-hit absence claim.
- NOT RUN — Open GitHub issues under additional tool-call/verdict/restart spellings and draft PR bodies after failed network reads; root owns live claim reconciliation.
- NOT RUN — Sister-repository asks on this surface; no sister repository searched.

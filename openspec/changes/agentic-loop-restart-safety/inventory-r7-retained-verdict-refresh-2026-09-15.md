# R7 retained-verdict and originating-proposal inventory refresh

base: c347eff487f50b93bc338d764f43ef5b5ea5e133

## Scope and checkpoint

Worktree: `/Users/coby/Code/c360/semstreams-wt/codex/gh1146-agentic-loop-restart`.
Frozen #759 parent: `417beae5552f8f15ad3540edd7d8504c87174c13`.
The measured source includes preserved uncommitted R6/R7/R8 work and the implemented, reviewed wire correction.
Root owns writes. This supplement changes no code, specification, task truth, Git state, or sister repository.

Owner bounded-intake ruling:
https://github.com/C360Studio/semstreams/issues/1146#issuecomment-5679435736

The current proposal, design, tasks, applicable governance requirements, and directly relevant evidence were read.
This refresh preserves rather than replaces these historical inventories:

1. `inventory-r7-governance-evidence-2026-09-14.md`:
  `96d9c252c96d938a19629cf7c59da95da67815f6e4b9f5a2d612d2bffe65904a`.
2. `inventory-r7-verdict-wire-2026-09-15.md`:
  `e47d02766f636eaf49ed6dea560906ce4488457c113912c5cab1daaf1c4bfcaa`.
3. `inventory-r7-verdict-wire-architect-audit-2026-09-15.md`:
  `a2ab756776ba073fce894808bd95bbd7a2a63baef84affdd716201cff796e7f9`.

The old broad R7 inventory verifier reported:
`pins=482 ok=247 moved=152 ambiguous=27 drift=56 malformed=0 unparsed=0`.
Its base-to-HEAD file comparison reported no committed changes; that does not erase dirty-source changes.
Only this retained-verdict/expected-proposal slice is refreshed here. Historical pins are not rewritten.

## Existing obligations and adjacent ownership

- `openspec/changes/agentic-loop-restart-safety/specs/agentic-governance/spec.md:58` — `Every proposal SHALL carry LoopID, RequestID, execution identity, and proposal fingerprint. Verdict subjects SHALL`
- `openspec/changes/agentic-loop-restart-safety/specs/agentic-governance/spec.md:59` — `use the NATS-safe execution identity. A response handler without a process waiter SHALL validate and read the exact`
- `openspec/changes/agentic-loop-restart-safety/specs/agentic-governance/spec.md:60` — `retained verdict before republishing a proposal. Missing or full waiter channels SHALL NOT authorize completed`
- `openspec/changes/agentic-loop-restart-safety/specs/agentic-governance/spec.md:72` — `- **WHEN** retained verdict identity or fingerprint conflicts with the proposal`
- `openspec/changes/agentic-loop-restart-safety/specs/agentic-governance/spec.md:73` — `- **THEN** the delivery quarantines rather than selecting one value`
- `openspec/changes/agentic-loop-restart-safety/specs/agentic-governance/spec.md:77` — `Every validated task, request, response, proposal, and verdict publication SHALL carry its lane's required`
- `openspec/changes/agentic-loop-restart-safety/specs/agentic-governance/spec.md:78` — `correlation and receive PubAck before source ACK. PubAck uncertainty MAY repeat a publication. `Nats-Msg-Id` MAY`
- `openspec/changes/agentic-loop-restart-safety/specs/agentic-governance/spec.md:81` — `The exact retained-verdict read exists only at the governance waiter-loss boundary. Ordinary validated outputs and`
- `openspec/changes/agentic-loop-restart-safety/specs/agentic-governance/spec.md:82` — `proposals require no exact committed-output lookup. Conflicting proposal or verdict correlation SHALL quarantine;`
- `openspec/changes/agentic-loop-restart-safety/specs/agentic-governance/spec.md:83` — `absence outside admitted retention SHALL remain unknown. No general stream scan or new verdict authority is`
- `openspec/changes/agentic-loop-restart-safety/tasks.md:235` — `- [ ] R7 Close governance settlement/correlation proof (old 8.1–8.3). Cover allowed/denied/filter-error/panic/budget,`
- `openspec/changes/agentic-loop-restart-safety/tasks.md:236` — `missing/full waiters, exact retained verdicts, and replacement before/after proposal/verdict/tool publication.`
- `openspec/changes/agentic-loop-restart-safety/tasks.md:238` — `at-least-once with no general committed-output lookup. Match RequestID, ExecutionID and proposal fingerprint.`
- `openspec/changes/agentic-loop-restart-safety/tasks.md:250` — `R7's full source-to-verdict guarantee remains blocked on that separately designed/proven boundary; the guarantee`

Root independently verified the current adjacent claims:

1. #1311/#1312 own rule proposal-source settlement after durable verdict publication. #1312 remains open,
  draft, main-based at `25ae71ab`; source-settlement implementation/restack is held. They do not replace
  #1146 correlation, retained-verdict recovery, or R8 publisher ownership.
2. #1158 remains the separate repository-wide framework-subject publisher/codec census and migration.
3. #1238's remaining proof concerns three carrier-refusal validations, not retained governance recovery.
  This claim's R7/R11 continue to own the retained-governance proof.
4. The accepted registered-envelope/typed-wire correction is implemented and independently reviewed.
  Its original inventories, authoring census, and adopter evidence remain frozen; this is not a new wire sweep.

## Surface inventory: originating proposal and live waiter

The expected proposal is presently constructed inside the publication operation. The live waiter stores
only a channel keyed by ExecutionID, not that proposal or its expected correlation.

- `processor/agentic-loop/execution_identity.go:15` — `func stampToolExecutionCorrelation(requestID string, calls []agentic.ToolCall) error {`
- `processor/agentic-loop/execution_identity.go:24` — `calls[i].RequestID = requestID`
- `processor/agentic-loop/execution_identity.go:25` — `calls[i].CallOrdinal = ordinal`
- `processor/agentic-loop/execution_identity.go:26` — `calls[i].ExecutionID = deriveToolExecutionID(requestID, calls[i].ID, ordinal)`
- `processor/agentic-loop/execution_identity.go:39` — `return "tool-exec-" + toolExecutionIdentityVersion + "-" + digest`
- `processor/agentic-loop/handlers.go:1361` — `if err := stampToolExecutionCorrelation(requestID, toolCalls); err != nil {`
- `processor/agentic-loop/governance_dispatcher.go:120` — `ProposalFingerprint string         `json:"proposal_fingerprint"``
- `processor/agentic-loop/governance_dispatcher.go:137` — `ProposalFingerprint string         `json:"proposal_fingerprint,omitempty"``
- `processor/agentic-loop/governance_dispatcher.go:275` — `func NewGovernanceDispatcher(cfg ToolCallGovernanceConfig, publisher VerdictPublisher, logger *slog.Logger, metrics DispatcherMetrics) GovernanceDispatcher {`
- `processor/agentic-loop/governance_dispatcher.go:292` — `waiters:   map[string]chan verdictArrival{},`
- `processor/agentic-loop/governance_dispatcher.go:380` — `type verdictArrival struct {`
- `processor/agentic-loop/governance_dispatcher.go:381` — `decision string`
- `processor/agentic-loop/governance_dispatcher.go:382` — `reason   string`
- `processor/agentic-loop/governance_dispatcher.go:383` — `ruleID   string`
- `processor/agentic-loop/governance_dispatcher.go:393` — `waiters map[string]chan verdictArrival`
- `processor/agentic-loop/governance_dispatcher.go:402` — `ch := make(chan verdictArrival, 1)`
- `processor/agentic-loop/governance_dispatcher.go:404` — `d.waiters[callID] = ch`
- `processor/agentic-loop/governance_dispatcher.go:411` — `delete(d.waiters, callID)`
- `processor/agentic-loop/governance_dispatcher.go:417` — `ch, ok := d.waiters[callID]`
- `processor/agentic-loop/governance_dispatcher.go:422` — `func (d *enforceDispatcher) Propose(ctx context.Context, loopID, parentLoopID string, calls []agentic.ToolCall) (DispatcherResult, error) {`
- `processor/agentic-loop/governance_dispatcher.go:430` — `if call.ExecutionID == "" {`
- `processor/agentic-loop/governance_dispatcher.go:433` — `channels[call.ExecutionID] = d.registerWaiter(call.ExecutionID)`
- `processor/agentic-loop/governance_dispatcher.go:435` — `defer func() {`
- `processor/agentic-loop/governance_dispatcher.go:437` — `d.releaseWaiter(call.ExecutionID)`
- `processor/agentic-loop/governance_dispatcher.go:447` — `if err := publishProposed(ctx, d.publisher, loopID, parentLoopID, call, d.logger); err != nil {`
- `processor/agentic-loop/governance_dispatcher.go:448` — `publishFailures[call.ExecutionID] = err`
- `processor/agentic-loop/governance_dispatcher.go:463` — `Reason: fmt.Sprintf("governance publish failed: %v", pubErr),`
- `processor/agentic-loop/governance_dispatcher.go:469` — `decision, reason := d.awaitVerdict(ctx, channels[call.ExecutionID], call, loopID)`
- `processor/agentic-loop/governance_dispatcher.go:487` — `return DispatcherResult{Approved: approved, Rejected: rejected}, nil`
- `processor/agentic-loop/governance_dispatcher.go:497` — `timer := time.NewTimer(d.timeout)`
- `processor/agentic-loop/governance_dispatcher.go:518` — `case <-ctx.Done():`
- `processor/agentic-loop/governance_dispatcher.go:519` — `return "timeout", fmt.Sprintf("governance wait cancelled: %v", ctx.Err())`

The helper parameter name `callID` does not describe its production key: Propose passes ExecutionID.
Registration assigns over any existing key; release deletes by key. An empty ExecutionID encountered after
earlier registrations returns before the release defer is installed. No stronger ownership claim is inferred.

Publication failure is currently converted to a rejected call and Propose ultimately returns a nil error.
Cancellation while waiting is likewise represented as a timeout/rejection, not a returned context error.
These are measured caller-propagation facts relevant to the existing PubAck/settlement obligations.

- `processor/agentic-loop/governance_dispatcher.go:568` — `func publishProposed(ctx context.Context, publisher VerdictPublisher, loopID, parentLoopID string, call agentic.ToolCall, logger *slog.Logger) error {`
- `processor/agentic-loop/governance_dispatcher.go:569` — `if call.RequestID == "" || call.ExecutionID == "" || call.CallOrdinal == 0 {`
- `processor/agentic-loop/governance_dispatcher.go:572` — `if publisher == nil {`
- `processor/agentic-loop/governance_dispatcher.go:585` — `payload := ProposedToolCallPayload{`
- `processor/agentic-loop/governance_dispatcher.go:586` — `LoopID:       loopID,`
- `processor/agentic-loop/governance_dispatcher.go:587` — `ParentLoopID: parentLoopID,`
- `processor/agentic-loop/governance_dispatcher.go:588` — `RequestID:    call.RequestID,`
- `processor/agentic-loop/governance_dispatcher.go:589` — `ExecutionID:  call.ExecutionID,`
- `processor/agentic-loop/governance_dispatcher.go:590` — `CallID:       call.ID,`
- `processor/agentic-loop/governance_dispatcher.go:591` — `CallOrdinal:  call.CallOrdinal,`
- `processor/agentic-loop/governance_dispatcher.go:592` — `ToolName:     call.Name,`
- `processor/agentic-loop/governance_dispatcher.go:593` — `Arguments:    coerceArguments(call.Arguments),`
- `processor/agentic-loop/governance_dispatcher.go:601` — `if cmd, ok := payload.Arguments["command"].(string); ok {`
- `processor/agentic-loop/governance_dispatcher.go:604` — `if url, ok := payload.Arguments["url"].(string); ok {`
- `processor/agentic-loop/governance_dispatcher.go:607` — `fingerprint, err := fingerprintProposedToolCall(payload)`
- `processor/agentic-loop/governance_dispatcher.go:611` — `payload.ProposalFingerprint = fingerprint`
- `processor/agentic-loop/governance_dispatcher.go:622` — `data, err := payloadToBaseMessageBytes(payload, "agentic-loop")`
- `processor/agentic-loop/governance_dispatcher.go:627` — `subject := "agent.toolcall.proposed." + loopID`
- `processor/agentic-loop/governance_dispatcher.go:628` — `if err := publisher.PublishToStream(ctx, subject, data); err != nil {`
- `processor/agentic-loop/governance_dispatcher.go:635` — `payload.ProposalFingerprint = ""`
- `processor/agentic-loop/governance_dispatcher.go:636` — `data, err := json.Marshal(payload)`
- `processor/agentic-loop/governance_dispatcher.go:640` — `sum := sha256.Sum256(data)`
- `processor/agentic-loop/governance_dispatcher.go:641` — `return "sha256:" + hex.EncodeToString(sum[:]), nil`

The fingerprint covers the full constructed ProposedToolCallPayload, with its own fingerprint field cleared.
Construction includes parent loop, provider call/ordinal, tool name, arguments and lifted command/URL.
The nil-publisher branch returns before constructing this payload. There is one production fingerprint calculation.

## Surface inventory: verdict interpretation and production callers

The implemented wire path validates registered payload shape, required correlation, duplicate correlation
consistency, and actual subject agreement. It does not compare the verdict against the original proposal.

- `processor/agentic-loop/governance_dispatcher.go:238` — `type GovernanceDispatcher interface {`
- `processor/agentic-loop/governance_dispatcher.go:249` — `Propose(ctx context.Context, loopID, parentLoopID string, calls []agentic.ToolCall) (DispatcherResult, error)`
- `processor/agentic-loop/governance_dispatcher.go:258` — `HandleVerdict(payload VerdictPayload) (natsclient.DeliveryDecision, error)`
- `processor/agentic-loop/governance_dispatcher.go:524` — `payload, disposition, err := normalizeVerdictPayload(payload)`
- `processor/agentic-loop/governance_dispatcher.go:528` — `decision, executionID := payload.Decision, payload.ExecutionID`
- `processor/agentic-loop/governance_dispatcher.go:530` — `ch, ok := d.lookupWaiter(executionID)`
- `processor/agentic-loop/governance_dispatcher.go:540` — `return natsclient.DeliveryDecisionRetry, fmt.Errorf("no active governance waiter for execution_id %q", executionID)`
- `processor/agentic-loop/governance_dispatcher.go:546` — `case ch <- verdictArrival{decision: decision, reason: payload.Reason, ruleID: payload.RuleID}:`
- `processor/agentic-loop/governance_dispatcher.go:551` — `return natsclient.DeliveryDecisionQuarantine, fmt.Errorf("governance waiter for execution_id %q is full", executionID)`
- `processor/agentic-loop/component.go:312` — `handler.SetGovernanceDispatcher(NewGovernanceDispatcher(`
- `processor/agentic-loop/handlers.go:435` — `func (h *MessageHandler) SetGovernanceDispatcher(d GovernanceDispatcher) {`
- `processor/agentic-loop/handlers.go:442` — `func (h *MessageHandler) GovernanceDispatcher() GovernanceDispatcher {`
- `processor/agentic-loop/handlers.go:1407` — `govResult, gErr := h.governanceDispatcher.Propose(ctx, loopID, parentLoopID, toolCalls)`
- `processor/agentic-loop/component.go:918` — `handler = c.handleResponseMessage`
- `processor/agentic-loop/component.go:931` — `settleHandlerFn = c.handleToolCallVerdictMessage`
- `processor/agentic-loop/component.go:1077` — `deliveredSubject := msg.Subject()`
- `processor/agentic-loop/component.go:1079` — `return settleHandlerFn(ctx, deliveredSubject, data)`
- `processor/agentic-loop/component.go:1081` — `result := natsclient.SettleDelivery(msg, decision, cause)`
- `processor/agentic-loop/component.go:1082` — `admission.latch(result)`
- `processor/agentic-loop/component.go:2557` — `payload, decision, err := decodeVerdictPayload(c.decoder, data)`
- `processor/agentic-loop/component.go:2561` — `if subject != "agent.toolcall."+payload.Decision+"."+payload.ExecutionID {`
- `processor/agentic-loop/component.go:2565` — `return dispatcher.HandleVerdict(payload)`
- `processor/agentic-loop/component.go:2570` — `func decodeVerdictPayload(decoder *message.Decoder, data []byte) (VerdictPayload, natsclient.DeliveryDecision, error) {`

Current structural results:

1. GovernanceDispatcher: five implementations — disabled, audit, enforce, delivery-owner test stub,
  recovery test stub. No sixth retained-recovery implementation.
2. NewGovernanceDispatcher: 19 references, one production composition caller at component.go312.
3. enforceDispatcher.Propose call hierarchy: 15 caller functions, one production caller at handlers.go1407.
  Its outgoing calls include publication, waiter operations, await and metrics; no retained read.
4. ProposedToolCallPayload.ProposalFingerprint: three references — test145, publication611, hash clearing635.
5. VerdictPayload.ProposalFingerprint: component projection2601, normalizer172, test fixture36.
  No expected-proposal comparison is among these references.
6. fingerprintProposedToolCall: one caller, publishProposed607.
7. decodeVerdictPayload: production verdict callback2557 and wire-test304.

## Surface inventory: replacement reconstruction and exact evidence

Replacement response processing already restores loop/request authority before executing the response handler.
The tool-call response itself supplies the original ordered calls; deterministic execution stamping is reused.

- `processor/agentic-loop/component.go:1530` — `entity, revision, err := c.ensureResponseLoop(ctx, *response)`
- `processor/agentic-loop/component.go:1554` — `result, err := c.handler.HandleModelResponse(ctx, loopID, *response)`
- `processor/agentic-loop/component.go:1575` — `return loopSettlementDecision(err), err`
- `processor/agentic-loop/component.go:1584` — `if err := c.persistHandlerResult(ctx, result, revision); err != nil {`
- `processor/agentic-loop/settlement_recovery.go:372` — `cold = true`
- `processor/agentic-loop/settlement_recovery.go:398` — `request, found, err := c.readRetainedAgentRequest(ctx, loopID)`
- `processor/agentic-loop/settlement_recovery.go:405` — `if request.RequestID != response.RequestID {`
- `processor/agentic-loop/settlement_recovery.go:420` — `if err = c.handler.loopManager.restoreLoopFromRequest(entity, request, nil); err != nil {`
- `processor/agentic-loop/settlement_recovery.go:471` — `calls := append([]agentic.ToolCall(nil), response.Message.ToolCalls...)`
- `processor/agentic-loop/settlement_recovery.go:472` — `if err := stampToolExecutionCorrelation(response.RequestID, calls); err != nil {`
- `processor/agentic-loop/settlement_recovery.go:480` — `if call.ID != result.CallID || call.CallOrdinal != result.CallOrdinal ||`
- `processor/agentic-loop/state.go:475` — `if err := stampToolExecutionCorrelation(response.RequestID, calls); err != nil {`

The existing private evidence seam offers request and response operations only:

- `processor/agentic-loop/component.go:85` — `settlementEvidence       loopSettlementEvidenceReader`
- `processor/agentic-loop/settlement_recovery.go:28` — `type loopSettlementEvidenceReader interface {`
- `processor/agentic-loop/settlement_recovery.go:29` — `ReadAgentRequest(context.Context, string, string) (retainedLoopMessage, bool, error)`
- `processor/agentic-loop/settlement_recovery.go:30` — `ReadAgentResponse(context.Context, string, string) (retainedLoopMessage, bool, error)`
- `processor/agentic-loop/settlement_recovery.go:40` — `return r.readExact(ctx, streamName, subject)`
- `processor/agentic-loop/settlement_recovery.go:46` — `return r.readExact(ctx, streamName, subject)`
- `processor/agentic-loop/settlement_recovery.go:52` — `stream, err := r.client.GetStream(ctx, streamName)`
- `processor/agentic-loop/settlement_recovery.go:56` — `raw, err := stream.GetLastMsgForSubject(ctx, subject)`
- `processor/agentic-loop/settlement_recovery.go:57` — `if errors.Is(err, jetstream.ErrMsgNotFound) {`
- `processor/agentic-loop/settlement_recovery.go:63` — `return retainedLoopMessage{subject: raw.Subject, data: append([]byte(nil), raw.Data...)}, true, nil`
- `processor/agentic-loop/settlement_recovery.go:67` — `subject, err := component.ResolveSubject(definitions, "agent.request", loopID)`
- `processor/agentic-loop/settlement_recovery.go:93` — `subject, err := component.ResolveSubject(definitions, "agent.response", requestID)`
- `processor/agentic-loop/settlement_recovery.go:170` — `evidence, found, err := reader.ReadAgentRequest(ctx, streamName, subject)`
- `processor/agentic-loop/settlement_recovery.go:195` — `if evidence.subject != subject || request.LoopID != loopID ||`
- `processor/agentic-loop/settlement_recovery.go:222` — `evidence, found, err := reader.ReadAgentResponse(ctx, streamName, subject)`
- `processor/agentic-loop/settlement_recovery.go:236` — `if !ok || evidence.subject != subject || response.RequestID != requestID {`

Structural results: loopSettlementEvidenceReader has three implementations (native reader, approval-order test
wrapper, settlement test reader), and two type references. readExact has only its two request/response callers.
Loop readRetainedAgentResponse has two production callers, recoverToolResult458 and recoverApprovalResponse762.

No retained-verdict reader was located. A spec requiring one is not evidence that its runtime exists.

## Retention and address/admission boundary

- `processor/agentic-loop/config.go:421` — `Name: "agent.toolcall.approved", Config: component.JetStreamPort{Subjects: []string{"agent.toolcall.approved.>"}, StreamName: "AGENT"}, Required: false,`
- `processor/agentic-loop/config.go:425` — `Name: "agent.toolcall.rejected", Config: component.JetStreamPort{Subjects: []string{"agent.toolcall.rejected.>"}, StreamName: "AGENT"}, Required: false,`
- `processor/agentic-loop/config.go:465` — `Name: "agent.toolcall.proposed", Config: component.JetStreamPort{Subjects: []string{"agent.toolcall.proposed.*"}, StreamName: "AGENT"}, Description: "Proposed tool calls awaiting rule-driven governance verdict (ADR-039). Emitted in audit and enforce modes.",`
- `processor/agentic-loop/component.go:958` — `stream, ok := facts.Stream()`
- `processor/agentic-loop/component.go:962` — `streamName := stream.Name()`
- `processor/agentic-loop/component.go:969` — `if err := waitForStream(setupCtx, streamName); err != nil {`
- `processor/agentic-loop/component.go:974` — `consumerName := fmt.Sprintf("agentic-loop-%s", sanitizeSubject(subject))`
- `processor/agentic-loop/component.go:987` — `consumerCfg, componentMaxAckPending, consumerErr := agenticLoopConsumerPolicy(port)`
- `natsclient/stream.go:891` — `switch autoConfig.Retention {`
- `natsclient/stream.go:893` — `streamCfg.Retention = jetstream.InterestPolicy`
- `natsclient/stream.go:895` — `streamCfg.Retention = jetstream.WorkQueuePolicy`
- `natsclient/stream.go:897` — `streamCfg.Retention = jetstream.LimitsPolicy`
- `natsclient/stream.go:900` — `streamCfg.Discard = autoConfig.Discard`

Existing exact request/response addressing reads declared port facts, not a universal hard-coded stream.
The two default verdict inputs name AGENT, but that alone does not prove a live stream's retention admission.
Consumer existence/wait-for-stream and delivery-lane admission are not retained-evidence admission.

The loop-side retention observation located in this refresh belongs to human approval absence:

- `processor/agentic-loop/settlement_recovery.go:800` — `// settleAbsentApprovalEvidence observes retention only for a missing exact`
- `processor/agentic-loop/settlement_recovery.go:810` — `info, err := stream.Info(ctx)`
- `processor/agentic-loop/settlement_recovery.go:818` — `age := time.Since(entity.StartedAt)`
- `processor/agentic-loop/settlement_recovery.go:819` — `if retention.Retention != jetstream.LimitsPolicy || retention.Discard != jetstream.DiscardNew ||`
- `processor/agentic-loop/settlement_recovery.go:820` — `(retention.MaxMsgsPerSubject > 0 && !retention.DiscardNewPerSubject) || retention.AllowMsgTTL ||`
- `processor/agentic-loop/settlement_recovery.go:821` — `entity.StartedAt.IsZero() || age < 0 || (retention.MaxAge > 0 && age >= retention.MaxAge) {`
- `processor/agentic-loop/settlement_recovery.go:822` — `return false, natsclient.DeliveryDecisionRetry, fmt.Errorf("approval evidence %q retention is not proven for loop %q", subject, entity.ID)`

That helper does not establish governance admission, and its human-approval policy is not adopted here.
readExact's `ErrMsgNotFound` is an observed absence, not proof that prior verdict publication never happened.
The existing R7/R8 obligation that absence outside admitted retention remains unknown is therefore an
unimplemented/evidence boundary in this slice, not permission to add another authority or widen the claim.

## Closest existing problem shape and collision table

Semantic job: validate exact retained evidence before repeating work after loss of process-local state.

- `processor/agentic-model/provider_settlement.go:89` — `evidence, found, err := reader.ReadRetainedResponse(ctx, streamName, subject)`
- `processor/agentic-model/provider_settlement.go:98` — `decoded, err := c.decoder.Decode(evidence.data)`
- `processor/agentic-model/provider_settlement.go:103` — `if err := decoded.Validate(); err != nil {`
- `processor/agentic-model/provider_settlement.go:113` — `if evidence.subject != subject || response.RequestID != requestID {`
- `processor/agentic-model/component.go:616` — `_, found, err := c.readRetainedAgentResponse(ctx, req.RequestID)`
- `processor/agentic-model/component.go:619` — `return natsclient.DeliveryDecisionQuarantine, err`
- `processor/agentic-model/component.go:622` — `return natsclient.DeliveryDecisionRetry, err`
- `processor/agentic-model/component.go:624` — `if found {`
- `processor/agentic-model/component.go:628` — `return natsclient.DeliveryDecisionAck, nil`
- `processor/agentic-model/component.go:636` — `client, endpoint, capability, endpointName, err := c.getClientForRequest(req)`

The model path already reads exact retained response before repeating provider work, validates it,
quarantines conflicting evidence, retries read failure, and continues ordinary work when absent.
This is an existing shape, not an assertion that all of its semantics apply to governance.

| Dimension | Existing governance owner | Existing loop settlement owners | Existing model settlement owner |
| --- | --- | --- | --- |
| Semantic class | Proposal-to-verdict gating | Restore current response/tool/approval work from exact authority | Avoid repeated provider work when exact response exists |
| Owners | enforceDispatcher, MessageHandler, Component | Component evidence reader; LoopManager batch restore | Component, private response-evidence reader |
| Catalogs | Governance config; proposed/approved/rejected port definitions | Request/response port definitions and AGENT_LOOPS authority | Response output port definition |
| Status | Decisions, timeout, missing waiter, full waiter, metrics | found/error, classified settlement, current loop revision | found/error, quarantine/retry/ACK |
| Lifecycle | Register before publish; release when Propose returns; process-local map | Cold restoration from retained request/response and loop state | Exact read on request delivery before provider call |
| Ownership | ExecutionID key; mutex; assignment/deletion; no durable waiter claim | Exact subject/request correlation and current loop authority | Exact subject/request correlation |
| Readers | One production Propose caller; installed verdict callback | Response/tool/approval recovery callers; test readers | handleRequest plus direct test |
| Writers | publishProposed; verdict channel writer; rule writers already inventoried | Existing request/response publishers; process restoration, no read-side stream write | Existing response publisher; read-side operation does not write |
| Recovery | Missing waiter retries; no retained lookup or expected-proposal comparison located | Existing exact-read native adapter and deterministic batch stamping | Exact retained response reused before repeating provider work |

TOOL_CALL_OUTCOMES/external tool-effect ambiguity remains the separately inventoried execution authority.
This supplement does not turn a governance verdict into tool-effect proof or propose a replacement durable owner.
No new reusable primitive is established by this inventory.

## Adopter seam inventory

Specific person: an external developer using the normal agentic-loop component or authoring subject-mode rules,
without reading governance_dispatcher.go.

| Surface/person | What they currently must know | Default/no-action path | Where discovered | Burden gap |
| --- | --- | --- | --- | --- |
| Normal component/config author | Governance mode, rule availability, timeout; declared verdict ports must point to the actual evidence stream | Default disabled is pass-through; audit does not gate; enforce publishes then waits | Config/schema and runtime outcomes; retained-verdict recovery is not currently supplied | Replacement recovery is a framework-owned fact, but current composition has no retained verdict operation |
| External rule author | Execution subject plus required loop/request/execution/fingerprint echo; explicit publish and automatic approve have different authoring responsibility | Old/missing correlation is refused by the reviewed wire path | Classified delivery failure and migration documentation | Wire inventory already records these debts; this refresh adds no authoring format |
| Direct GovernanceDispatcher/MessageHandler user | Install before Start; provide correlated calls, publisher and typed verdict; use correct lifetime/context | Public constructor provides only publisher/config/logger/metrics; it cannot currently obtain retained verdict evidence | Compile-time interface plus runtime retry/timeout; no retained-reader argument or recovery operation | Direct surface and normal component composition are distinct consumers; no new exported surface is proposed here |
| Operator replacing a component | Durable source consumer and retained evidence must survive | Native verdict redelivery survives; current test recreates waiter manually; response path still republishes proposals | Existing source-settlement observations, not an enforce-recovery E2E assertion | Whether exact saved verdict suffices must be observed by the framework, not predicted by operator timing |

Current public seams are pinned above: constructor275, interface249/258, setter435/getter442, composition312.
The frozen wire inventory/audit retain the completed 23 sister-path searches: no Go symbol users were found
there, while SemSpec rule/config adopters were found. Those searches were not rerun as a global migration census.
Dynamic/out-of-tree users remain unknown. No sister writes or claims of universal absence are made.

## Context and lifecycle evidence

- `processor/agentic-loop/component.go:57` — `cancel         context.CancelFunc`
- `processor/agentic-loop/component.go:82` — `consumeStream            func(context.Context, context.Context, natsclient.PortConsumerContext, natsclient.StreamConsumerConfig, func(context.Context, jetstream.Msg)) (jetstream.ConsumeContext, error)`
- `processor/agentic-loop/component.go:91` — `sweeperCancel context.CancelFunc`

The measured dispatcher and exact-reader structs retain publisher/client/logger/metrics, timeout, mutex,
channels or bytes, not context.Context. Their operations receive context explicitly.
The scoped context search in governance_dispatcher.go, settlement_recovery.go and provider_settlement.go
found operation/interface parameters only and no Background/TODO/WithoutCancel or CancelFunc.
This is not a repository-wide context audit and does not supersede R10.

## Proof boundary

- `processor/agentic-loop/fastlane_replacement_integration_test.go:209` — `func TestIntegrationVerdictMissingWaiterRetriesAfterComponentReplacement(t *testing.T) {`
- `processor/agentic-loop/fastlane_replacement_integration_test.go:260` — `require.Equal(t, 1, failed.naks)`
- `processor/agentic-loop/fastlane_replacement_integration_test.go:276` — `require.Equal(t, 1, info.NumAckPending)`
- `processor/agentic-loop/fastlane_replacement_integration_test.go:281` — `waiter := dispatcher.registerWaiter(executionID)`
- `processor/agentic-loop/fastlane_replacement_integration_test.go:282` — `unrelated := dispatcher.registerWaiter("other-execution")`
- `processor/agentic-loop/fastlane_replacement_integration_test.go:299` — `return fmt.Errorf("source ACK preceded delivery to the recreated waiter")`
- `configs/agentic.json:319` — `"mode": "audit",`
- `test/e2e/scenarios/agentic/scenario.go:250` — `{name: "verify-tool-call-governance", fn: s.verifyToolCallGovernance, asserts: true},`
- `test/e2e/scenarios/agentic/scenario.go:254` — `{name: "walk-approval-after-restart", fn: s.walkApprovalAfterRestart, asserts: true},`
- `test/e2e/scenarios/agentic/scenario.go:1117` — `// Audit mode means dispatch is NOT gated; this stage validates the`
- `test/e2e/scenarios/agentic/scenario.go:1146` — `map[string]string{"decision": "approved", "mode": "audit"})`

The native missing-waiter test proves retry, retained source delivery, and ACK after a manually recreated waiter.
It does not call replacement Propose to read an existing verdict before deciding whether to republish.
Current agentic E2E audit metrics and human approval-after-restart are different paths; neither proves
enforce retained-verdict recovery. No tests were run by this architect refresh.

## Searches and measured limits

All structural commands used:
`GOPLSCACHE=/private/tmp/semstreams-r7-gopls-cache GOCACHE=/private/tmp/semstreams-r7-test-cache
GOPROXY=off GOSUMDB=off GOFLAGS=-mod=readonly`.

Commands below ran from the claim worktree unless stated otherwise:

1. `git status --short`; `git rev-parse HEAD`; `wc -l` on current proposal/design/tasks/broad R7 inventory.
   HEAD unchanged; dirty and untracked work preserved.
2. `task inventory:verify -- openspec/changes/agentic-loop-restart-safety/inventory-r7-governance-evidence-2026-09-14.md`
   — result recorded above.
3. `rg --files openspec/changes/agentic-loop-restart-safety | rg 'wire|r7-governance|r7-rule'`
   — located existing bounded artifacts.
4. `rg -n '^#|retained|Retained|Propose|fingerprint|recoveryReader|Reader' openspec/changes/agentic-loop-restart-safety/inventory-r7-governance-evidence-2026-09-14.md`
   — located prior evidence and full-boundary supplement.
5. `gopls call_hierarchy processor/agentic-loop/governance_dispatcher.go:422:29`
   — 15 caller functions; one production caller; no retained-read outgoing call.
6. `gopls references processor/agentic-loop/governance_dispatcher.go:120:2` — 3.
7. `gopls references processor/agentic-loop/settlement_recovery.go:28:6` — 2.
8. `gopls implementation processor/agentic-loop/settlement_recovery.go:28:6` — 3.
9. `git grep -n -E 'GetLastMsgForSubject|retained.*[Vv]erdict|[Vv]erdict.*retained|agent\.toolcall|proposal_fingerprint' -- processor/agentic-loop processor/agentic-model`
   — request/response exact reads, wire/proposal declarations and tests; no retained-verdict implementation.
10. `rg --files processor/agentic-model | rg 'replay|recovery|response'`
    — response files; existing same-shape owner subsequently resolved as provider_settlement.go.
11. `rg -n 'verdict|Waiter|registerWaiter' processor/agentic-loop/fastlane_replacement_integration_test.go`
    — manual replacement waiter located. This direct read includes the untracked test.
12. `rg -n 'retention|AGENT|Retention|MaxAge' processor/agentic-loop/retention_admission.go`
    — file does not exist; not treated as proof of global absence.
13. `gopls workspace_symbol -matcher=fuzzy Admit`
    — broad fuzzy results included deliveryLaneAdmission, not a retained-verdict reader; no count-based absence claim.
14. `gopls references processor/agentic-loop/governance_dispatcher.go:275:6` — 19.
15. `gopls references processor/agentic-loop/settlement_recovery.go:206:21` — 2.
16. `gopls references processor/agentic-loop/governance_dispatcher.go:137:2` — 3.
17. `rg --files processor/agentic-loop | rg 'admission|retention'` — zero path-name matches.
18. `gopls references processor/agentic-model/provider_settlement.go:76:21` — production616, test152.
19. `git grep -n -E 'LimitsPolicy|MaxAge|MaxMsgs|DiscardNew|retention' -- processor/agentic-loop/component.go processor/agentic-loop/config.go`
    — trajectory-retention text only.
20. Attempted unquoted `processor/agentic-loop/*settle* processor/agentic-loop/*start*` retention search:
    zsh rejected the unmatched start glob; rerun below used quoted tracked pathspecs.
21. `git grep -n 'AssignToolExecution' -- agentic processor/agentic-loop` — zero; actual owner found via stamped identity.
22. `git grep -n -E 'context\.(Context|Background|TODO|WithoutCancel|CancelFunc)' -- processor/agentic-loop/governance_dispatcher.go processor/agentic-loop/settlement_recovery.go processor/agentic-model/provider_settlement.go`
    — operation/interface context parameters only.
23. `rg -n 'LimitsPolicy|MaxAge|admission|Admit|retention|EvidenceStream' openspec/changes/agentic-loop-restart-safety/inventory-r7-governance-evidence-2026-09-14.md`
    — prior governance delivery-admission entries, not verdict-retention admission.
24. `git grep -n -E 'LimitsPolicy|MaxAge|DiscardNew|retention' -- 'processor/agentic-loop/*.go' ':!**/*_test.go'`
    — trajectory text and the human-approval absence observer.
25. `git grep -n -E 'stampToolExecutionCorrelation|ensureResponseLoop|handleResponseMessage' -- processor/agentic-loop/execution_identity.go processor/agentic-loop/component.go`
    — located current callback/restoration seam.
26. Attempted unquoted `service/*retention* natsclient/*retention*` search:
    zsh rejected unmatched service glob; no absence conclusion.
27. `git grep -n -E 'Read.*Verdict|Retained.*Verdict|Verdict.*Reader|retained.*verdict' -- '*.go'`
    — only two unrelated graph-gateway readiness test lines; no matching governance reader.
    This tracked search excludes untracked files, which were separately inspected where relevant.
28. `git grep -n -E 'retention|DiscardNew|LimitsPolicy' -- service/message_logger.go natsclient/stream.go`
    — stream configuration/adoption text; not governance absence admission.
29. `git grep -n -E 'retention|admi' -- openspec/changes/agentic-loop-restart-safety/specs/message-logger/spec.md`
    — zero matching lines in that pathspec.
30. `git grep -n -E 'Retention.*Limits|MaxAge:|Discard.*New|MaxMsgsPerSubject' -- service/message_logger.go agentic natsclient | head -70`
    — bounded locator output only, not a census; stream configuration and unrelated storage tests.
31. `gopls implementation processor/agentic-loop/governance_dispatcher.go:238:6` — 5.
32. `gopls references processor/agentic-loop/governance_dispatcher.go:634:6` — 1.
33. `gopls references processor/agentic-loop/settlement_recovery.go:49:46` — 2.
34. `gopls references processor/agentic-loop/execution_identity.go:15:6` — 26:
    production handlers1361, settlement_recovery472/778, state475; remaining test calls.
35. `gopls references processor/agentic-loop/component.go:2570:6` — 2.
36. `git grep -n -E 'approval-after-restart|verify-tool-call-governance|approval_required|tool_call_governance|agentic_governance_tool' -- configs/agentic.json taskfiles/e2e/agentic.yml test/e2e/scenarios/agentic/scenario.go test/e2e/scenarios/agentic/approval_restart.go`
    — located audit mode, human approval config and distinct scenario stages.
37. `shasum -a 256` on the measured files listed below, plus preserved inventories.
    Numbered source ranges were then read around the located declarations/callers; no source changes occurred.

## Measured file SHA-256

Paths in the first group have prefix `processor/agentic-loop/`.

| File | SHA-256 |
| --- | --- |
| governance_dispatcher.go | 5056a7155efcefb4495c90ae13a95c65f3670d81ed33aefa00806f5dd4d35a05 |
| component.go | 6b8d0e247ecca71bb0570e87fa3aee1e0bb2bf6c11a06a116e0f8f34758972db |
| handlers.go | 7b43f2b1398a49d12022171085069e86938273d74d545b5e26c811c8ecc817d5 |
| settlement_recovery.go | 46e59e01e016498c0516f1d34667f6675e4cfa56cfaa3f9d8dc2f42e8db3e2ec |
| execution_identity.go | bd3f80c54874173208a2c0a51a3d2cb373dd50916a57438ea2df629759697b74 |
| config.go | 3fd0b3e0802025803f01c9c144e7b8fa2cf3809417678e9d75e4a1b7f3ba5604 |
| state.go | bf29ce8cdda9af3cce5ed24903406c9e89ddeabc01325c229f9adb349cd44de9 |
| fastlane_replacement_integration_test.go | 466b8261eff3d71f4929afad582eff9df89274ad85137bafe290a08a94f8f7d8 |

| Other measured file | SHA-256 |
| --- | --- |
| processor/agentic-model/provider_settlement.go | 01a3a52580af9f9066e6995785d9f98e15e249c7617369beded4a882ce19b26a |
| processor/agentic-model/component.go | 2d486b6fc3e0cb46bc63a3d87a28e0e5186d18ff4e8a3f6f73089266c0b1859e |
| natsclient/stream.go | 44092bfbcab1c3a7c242c17fdb4988a1ac37664a4d6cf5703a8b56083d5ce204 |
| configs/agentic.json | 144a85fbcfd0dfe63f73cfd50ef4ca20344e635ea84ae2fd0fa83cbbf808f4d1 |
| taskfiles/e2e/agentic.yml | 3b3649f3f313b324e9e8e25c7d5a1b5044c50b8bf4959fe70870ea2a5a792c56 |
| test/e2e/scenarios/agentic/scenario.go | 0959c768b77cbc92e387b76b348388f5ab4a6fdec637e5ce87982c495651d108 |
| test/e2e/scenarios/agentic/approval_restart.go | da9f6c7cfab8a07c7f8350a0012dc2d4167581f61fb98467e6950d050c527774 |

Current-change paths below have prefix `openspec/changes/agentic-loop-restart-safety/`.

| Artifact | SHA-256 |
| --- | --- |
| proposal.md | 23c732653d3aa5358d4acb6ac381e3b115206689e619e784f88db8cc5b7b6bf4 |
| design.md | f61f471f86ecb43049688bba59e76d829f0639862331458d80974360ebedf358 |
| tasks.md | 79c315389598702caf6b74febe29278d788fdb44299c3ee311f44253e88dc6a0 |
| specs/agentic-governance/spec.md | c09e36440d0a6136e4700e4da3f11165dcb84237fc7428e0f3b4fccbe7f26c76 |

The tasks digest above is the architect's intake snapshot. Root subsequently appended the separately completed
native validation-output evidence; the task pins cited above are unchanged. At materialization the tasks digest
is `9347cbc1e71afd17dc50f1d3cf1344398b4d8b1b9084bb190f68c87c80017284`.
Root changed prose list markers to numbered lists so the verifier treats only actual source pins as pin entries;
the complete architect handoff content is otherwise preserved.

## Independent-review factual supplement

The initial inventory review verified the source identity and all 164 pins, and requested only these two
bounded evidence additions. Root read the named source ranges and materialized them; no target state is added.

### Parent-loop value used by the fingerprint

- `processor/agentic-loop/handlers.go:1406` — `parentLoopID := h.resolveParentLoopID(loopID)`
- `processor/agentic-loop/handlers.go:415` — `func (h *MessageHandler) resolveParentLoopID(loopID string) string {`
- `processor/agentic-loop/handlers.go:416` — `if h.loopManager == nil {`
- `processor/agentic-loop/handlers.go:417` — `return ""`
- `processor/agentic-loop/handlers.go:419` — `entity, err := h.loopManager.GetLoop(loopID)`
- `processor/agentic-loop/handlers.go:420` — `if err != nil {`
- `processor/agentic-loop/handlers.go:421` — `return ""`
- `processor/agentic-loop/handlers.go:423` — `return entity.ParentLoopID`
- `processor/agentic-loop/state.go:399` — `restored := entity`
- `processor/agentic-loop/state.go:400` — `m.loops[entity.ID] = &restored`

The live proposal obtains ParentLoopID from the process loop entity. A nil manager or failed lookup yields empty;
that default is not proof of top-level ancestry. Cold reconstruction copies the durable entity into the process
map, preserving its parent value before the already-pinned response handler constructs the proposal. This identifies
the expected fingerprint's input owner and default; it does not introduce an ancestry policy.

### Existing adjacent replay-admission contract and startup ordering

- `openspec/changes/agentic-loop-restart-safety/specs/agentic-loop/spec.md:794` — `### Requirement: Restart-safe replay observes and admits local stream bounds`
- `openspec/changes/agentic-loop-restart-safety/specs/agentic-loop/spec.md:797` — ``agentstreamadmission.ObserveAndValidate` after resolving its own PortFacts and before its own first dependent`
- `openspec/changes/agentic-loop-restart-safety/specs/agentic-loop/spec.md:803` — `Admission SHALL require observed DiscardNew, sufficient MaxAge, and no earlier message bound. Refusal SHALL be typed`
- `openspec/changes/agentic-loop-restart-safety/specs/agentic-loop/spec.md:804` — ``agent_stream_replay_inadmissible`, name observed/required values, leave only the affected closure not ready, and`
- `openspec/changes/agentic-loop-restart-safety/specs/agentic-loop/spec.md:805` — `allocate or positively settle nothing. It SHALL mutate no stream and persist no state. Approval lifetime is excluded`
- `processor/agentic-loop/component.go:524` — `if err := c.initializeKVBucketsForStart(runCtx); err != nil {`
- `processor/agentic-loop/component.go:527` — `if err := c.restoreApprovalDeadlines(runCtx); err != nil {`
- `processor/agentic-loop/component.go:532` — `if err := c.setupSubscriptions(runCtx, runCtx); err != nil {`
- `openspec/changes/agentic-loop-restart-safety/tasks.md:271` — `- [ ] R8 Close replay admission, first-party producer and loop-authority proof (old 9.2–9.10). Preserve local observed`

The absence/admission gap has an already-approved adjacent owner and contract in R8; it is not an unowned new
design request. Current Start initializes loop storage, restores approval deadlines, then installs subscriptions.
R8 remains open. These pins do not claim the named admission operation is implemented or borrow human approval's
separate absence policy for governance.

## Open evidence, not additional policy proposals

1. Exact expected-proposal correlation is not stored or compared at the live waiter boundary.
2. Exact retained-verdict lookup and reuse are not wired into replacement response work.
3. Governance-specific absence/admitted-retention observation is not established by the measured readers,
   default AGENT port name, or the separate human-approval helper.
4. Publication/wait cancellation errors currently become rejection results with nil Propose error;
   the caller propagation boundary must be considered against already accepted R7/R8 source settlement.
5. Existing native/E2E evidence does not prove enforce retained-verdict recovery through response replacement.
6. Dynamic downstream callers and arbitrary externally configured destinations remain unknown;
   this bounded refresh is not the #1158 census.

Stop for independent inventory review. This artifact proposes no API, implementation, specification delta,
new storage, new runtime, or new owner-policy decision.

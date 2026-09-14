# Inventory: R5 tool-result and completed-effect evidence

base: 5e0e2259aa7392f7f3255d7f01533869862d8174

Worktree: `/Users/coby/Code/c360/semstreams-wt/codex/gh1146-agentic-loop-restart`.
Pins are against the preserved working files at that base; source hashes below disambiguate the dirty tree.
Scope: R5 / old 5.1–5.3 only. Root owns GitHub reads, test execution, task truth, and all non-inventory writes.

## Claimed gap

- `openspec/changes/agentic-loop-restart-safety/tasks.md:186` — `- [ ] R5 Close tool-result and completed-effect recovery proof (old 5.1–5.3). Preserve globally correlated execution`
- `openspec/changes/agentic-loop-restart-safety/tasks.md:187` — `identity, ordered partial batches, once-only iteration charging, prior exchange order, result persistence before`
- `openspec/changes/agentic-loop-restart-safety/tasks.md:188` — `downstream output, repeated publication until PubAck, and exact completed-outcome reuse without executor calls.`
- `openspec/changes/agentic-loop-restart-safety/tasks.md:189` — `Account for missing/conflicting evidence, post-effect ambiguity, executor panic, observed output bounds, and all`
- `openspec/changes/agentic-loop-restart-safety/tasks.md:190` — `replacement boundaries. Update the three named current tool requirements without losing unaffected scenarios;`
- `openspec/changes/agentic-loop-restart-safety/specs/agentic-loop/spec.md:360` — `- **WHEN** ToolResult carries RequestID and execution identity`
- `openspec/changes/agentic-loop-restart-safety/specs/agentic-loop/spec.md:361` — `- **THEN** agentic-loop reads the originating AgentResponse`
- `openspec/changes/agentic-loop-restart-safety/specs/agentic-loop/spec.md:362` — `- **AND** reconstructs the ordered batch from response and accumulated durable results`
- `openspec/changes/agentic-loop-restart-safety/specs/agentic-loop/spec.md:367` — `The framework SHALL preserve provider ToolCall ID for conversation semantics and stamp a distinct execution identity`
- `openspec/changes/agentic-loop-restart-safety/specs/agentic-loop/spec.md:368` — `derived from RequestID, provider CallID, and positive call ordinal. Tool, approval, governance, and completed-outcome`
- `openspec/changes/agentic-loop-restart-safety/specs/agentic-loop/spec.md:369` — `correlation SHALL use the framework identity.`
- `openspec/changes/agentic-loop-restart-safety/specs/agentic-loop/spec.md:476` — `#### Scenario: Terminal loop does not prove a tool result applied`
- `openspec/changes/agentic-loop-restart-safety/specs/agentic-loop/spec.md:478` — `- **WHEN** a cold `ToolResult` is durably correlated but only a bare terminal `LoopEntity` is present`
- `openspec/changes/agentic-loop-restart-safety/specs/agentic-loop/spec.md:479` — `- **THEN** agentic-loop retries until execution-specific applied proof exists`
- `openspec/changes/agentic-loop-restart-safety/specs/agentic-loop/spec.md:480` — `- **AND** task 4 neither acknowledges the tool result nor reconstructs its ordered batch`
- `openspec/changes/agentic-loop-restart-safety/specs/agentic-tools/spec.md:72` — `### Requirement: Tool-call completion SHALL be durable before request acknowledgement`
- `openspec/changes/agentic-loop-restart-safety/specs/agentic-tools/spec.md:74` — ``agentic-tools` SHALL own one immutable COMPLETED outcome per framework execution identity derived from RequestID,`
- `openspec/changes/agentic-loop-restart-safety/specs/agentic-tools/spec.md:75` — `provider CallID, and positive call ordinal. Provider `ToolCall.ID` SHALL remain conversation data and SHALL NOT be`
- `openspec/changes/agentic-loop-restart-safety/specs/agentic-tools/spec.md:76` — `the completed-outcome key. Before execution, agentic-tools SHALL read the exact outcome and validate its version,`
- `openspec/changes/agentic-loop-restart-safety/specs/agentic-tools/spec.md:77` — `execution identity, RequestID, provider CallID, ordinal, complete V1 request fingerprint, and result correlation. A`
- `openspec/changes/agentic-loop-restart-safety/specs/agentic-tools/spec.md:78` — `matching outcome SHALL be published without executor invocation. Missing state SHALL permit execution only under the`
- `openspec/changes/agentic-loop-restart-safety/specs/agentic-tools/spec.md:79` — `executor's admitted retry contract. Corrupt, colliding, or mismatched state SHALL quarantine the delivery.`
- `openspec/changes/agentic-loop-restart-safety/specs/agentic-tools/spec.md:81` — `After execution or terminal policy rejection, the component SHALL Create-CAS the complete outcome. On a Create`
- `openspec/changes/agentic-loop-restart-safety/specs/agentic-tools/spec.md:82` — `collision it SHALL read and validate the winner and publish that authoritative winner. A transient read, Create,`
- `openspec/changes/agentic-loop-restart-safety/specs/agentic-tools/spec.md:83` — `winner-read, or result-publication failure SHALL return Retry. The request SHALL positively settle only after`
- `openspec/changes/agentic-loop-restart-safety/specs/agentic-tools/spec.md:84` — `synchronous result publication receives PubAck.`
- `openspec/changes/agentic-loop-restart-safety/specs/agentic-tools/spec.md:86` — `An initial `approval_required` result SHALL be nonterminal coordination and SHALL NOT be persisted as COMPLETED. It`
- `openspec/changes/agentic-loop-restart-safety/specs/agentic-tools/spec.md:88` — `provider CallID, ordinal, and execution identity; its approved arguments and `ApprovedBy` form the terminal`
- `openspec/changes/agentic-loop-restart-safety/specs/agentic-tools/spec.md:107` — `### Requirement: Tool-result bounds SHALL be observed rather than predicted`
- `openspec/changes/agentic-loop-restart-safety/specs/agentic-tools/spec.md:109` — `The component SHALL first attempt the complete authoritative record and result. A typed observed full-record storage`
- `openspec/changes/agentic-loop-restart-safety/specs/agentic-tools/spec.md:110` — `rejection SHALL cause exactly one attempt to persist and publish a fixed compact correlated authority with`
- `openspec/changes/agentic-loop-restart-safety/specs/agentic-tools/spec.md:111` — ``ErrorKind=internal` and `Error=too_large`. The compact result SHALL retain only RequestID, execution identity,`
- `openspec/changes/agentic-loop-restart-safety/specs/agentic-tools/spec.md:112` — `provider call, loop, and trace correlation and SHALL contain no original content, error, metadata, or measured size.`
- `openspec/changes/agentic-loop-restart-safety/specs/agentic-tools/spec.md:113` — `A compact rejection SHALL emit loud bounded telemetry and terminate. The component SHALL NOT inspect configured`
- `openspec/changes/agentic-loop-restart-safety/specs/agentic-tools/spec.md:116` — `If only publication of an already-stored full authority returns typed oversize, the component SHALL preserve that`
- `openspec/changes/agentic-loop-restart-safety/specs/agentic-tools/spec.md:119` — `permits request ACK. Surrogate failure SHALL terminate without recursion. Redelivery SHALL repeat the full attempt`
- `openspec/changes/agentic-loop-restart-safety/specs/agentic-tools/spec.md:129` — `### Requirement: Executor panic and ambiguous pre-completion effects SHALL be explicit`
- `openspec/changes/agentic-loop-restart-safety/specs/agentic-tools/spec.md:131` — `An executor panic SHALL be recovered into a compact correlated internal result and follow normal completion.`
- `openspec/changes/agentic-loop-restart-safety/specs/agentic-tools/spec.md:132` — `Effectful executor contracts SHALL declare their operation-specific idempotency or reconciliation key and behavior;`
- `openspec/changes/agentic-loop-restart-safety/specs/agentic-tools/spec.md:133` — `the framework SHALL NOT claim that provider `ToolCall.ID` alone makes an external effect idempotent. If an executor`
- `openspec/changes/agentic-loop-restart-safety/specs/agentic-tools/spec.md:134` — `cannot reconcile an effect after failure between the effect and COMPLETED persistence, the ambiguity SHALL remain a`
- `openspec/changes/agentic-loop-restart-safety/specs/agentic-tools/spec.md:135` — `typed, metered retry risk and SHALL NOT be presented as exactly-once execution.`
- `openspec/changes/agentic-loop-restart-safety/specs/agentic-tools/spec.md:144` — `#### Scenario: effect completed but durable outcome is unknown`
- `openspec/changes/agentic-loop-restart-safety/specs/agentic-tools/spec.md:148` — `- **THEN** agentic-tools returns Retry and records the ambiguity`

## Spellings of the fact

- `agentic/tools.go:207` — `type ToolCall struct {`
- `agentic/tools.go:214` — `RequestID   string         `json:"request_id,omitempty"``
- `agentic/tools.go:215` — `ExecutionID string         `json:"execution_id,omitempty"``
- `agentic/tools.go:216` — `CallOrdinal uint32         `json:"call_ordinal,omitempty"``
- `agentic/tools.go:629` — `type ToolResult struct {`
- `agentic/tools.go:639` — `RequestID   string         `json:"request_id,omitempty"``
- `agentic/tools.go:640` — `ExecutionID string         `json:"execution_id,omitempty"``
- `agentic/tools.go:641` — `CallOrdinal uint32         `json:"call_ordinal,omitempty"``
- `graph/constants.go:47` — `// BucketToolCallOutcomes is agentic-tools' immutable COMPLETED ledger for`
- `graph/constants.go:48` — `// durable tool-result replay. Keys are opaque v1 hashes of ToolCall.ID.`
- `graph/constants.go:49` — `BucketToolCallOutcomes = "TOOL_CALL_OUTCOMES"`
- `graph/kvcatalog.go:150` — `owned(BucketToolCallOutcomes, "agentic-tools",`
- `graph/kvcatalog.go:151` — `"Immutable completed tool-call outcomes for result replay",`
- `graph/kvcatalog.go:152` — `natsclient.ClassOperational),`
- `processor/agentic-tools/outcomes.go:19` — `const completedOutcomeVersion = "v1"`
- `processor/agentic-tools/outcomes.go:25` — `type completedOutcome struct {`
- `processor/agentic-tools/outcomes.go:26` — `Version     string             `json:"version"``
- `processor/agentic-tools/outcomes.go:27` — `ExecutionID string             `json:"execution_id"``
- `processor/agentic-tools/outcomes.go:28` — `RequestID   string             `json:"request_id"``
- `processor/agentic-tools/outcomes.go:29` — `CallID      string             `json:"call_id"``
- `processor/agentic-tools/outcomes.go:30` — `CallOrdinal uint32             `json:"call_ordinal"``
- `processor/agentic-tools/outcomes.go:31` — `Fingerprint string             `json:"fingerprint"``
- `processor/agentic-tools/outcomes.go:32` — `Result      agentic.ToolResult `json:"result"``
- `processor/agentic-tools/outcomes.go:35` — `type completedOutcomeStore interface {`
- `processor/agentic-tools/outcomes.go:36` — `Get(context.Context, string) ([]byte, error)`
- `processor/agentic-tools/outcomes.go:37` — `Create(context.Context, string, []byte) error`
- `processor/agentic-tools/outcomes.go:40` — `type jetStreamCompletedOutcomeStore struct{ bucket jetstream.KeyValue }`
- `processor/agentic-tools/outcomes.go:42` — `func (s jetStreamCompletedOutcomeStore) Get(ctx context.Context, key string) ([]byte, error) {`
- `processor/agentic-tools/outcomes.go:50` — `func (s jetStreamCompletedOutcomeStore) Create(ctx context.Context, key string, value []byte) error {`
- `processor/agentic-tools/outcomes.go:55` — `type irrecoverableOutcomeError struct{ err error }`
- `processor/agentic-tools/outcomes.go:56` — `type outcomeCollisionError struct{ err error }`
- `processor/agentic-tools/outcomes.go:57` — `type ambiguousOutcomeCreateError struct{ err error }`
- `processor/agentic-tools/outcomes.go:77` — `func outcomeIdentityDigest(executionID string) string {`
- `processor/agentic-tools/outcomes.go:78` — `sum := sha256.Sum256([]byte(executionID))`
- `processor/agentic-tools/outcomes.go:82` — `func toolCallOutcomeKey(executionID string) string {`
- `processor/agentic-tools/outcomes.go:83` — `return completedOutcomeVersion + "." + outcomeIdentityDigest(executionID)`
- `processor/agentic-tools/outcomes.go:86` — `func toolResultMessageID(executionID string) string {`
- `processor/agentic-tools/outcomes.go:87` — `return "tool-result/" + completedOutcomeVersion + "/" + outcomeIdentityDigest(executionID)`
- `processor/agentic-tools/outcomes.go:90` — `func toolApprovalRequiredMessageID(executionID string) string {`
- `processor/agentic-tools/outcomes.go:91` — `return toolResultMessageID(executionID) + "/approval-required"`
- `processor/agentic-tools/outcomes.go:94` — `func toolCallFingerprintV1(call agentic.ToolCall) (string, error) {`
- `processor/agentic-tools/outcomes.go:97` — `// keys recursively, normalizing map insertion order.`
- `processor/agentic-tools/outcomes.go:98` — `canonical := struct {`
- `processor/agentic-tools/outcomes.go:99` — `ID          string         `json:"id"``
- `processor/agentic-tools/outcomes.go:100` — `Name        string         `json:"name"``
- `processor/agentic-tools/outcomes.go:101` — `Arguments   map[string]any `json:"arguments"``
- `processor/agentic-tools/outcomes.go:102` — `Metadata    map[string]any `json:"metadata"``
- `processor/agentic-tools/outcomes.go:103` — `LoopID      string         `json:"loop_id"``
- `processor/agentic-tools/outcomes.go:104` — `TraceID     string         `json:"trace_id"``
- `processor/agentic-tools/outcomes.go:105` — `RequestID   string         `json:"request_id"``
- `processor/agentic-tools/outcomes.go:106` — `ExecutionID string         `json:"execution_id"``
- `processor/agentic-tools/outcomes.go:107` — `CallOrdinal uint32         `json:"call_ordinal"``
- `processor/agentic-tools/outcomes.go:108` — `ApprovedBy  string         `json:"approved_by"``
- `processor/agentic-tools/outcomes.go:109` — `}{call.ID, call.Name, call.Arguments, call.Metadata, call.LoopID, call.TraceID, call.RequestID, call.ExecutionID, call.CallOrdinal, call.ApprovedBy}`
- `processor/agentic-tools/outcomes.go:118` — `func newCompletedOutcome(call agentic.ToolCall, result agentic.ToolResult) (completedOutcome, error) {`
- `processor/agentic-tools/outcomes.go:129` — `if result.RequestID != call.RequestID || result.ExecutionID != call.ExecutionID || result.CallOrdinal != call.CallOrdinal {`
- `processor/agentic-tools/outcomes.go:138` — `func validateToolExecutionCorrelation(call agentic.ToolCall) error {`
- `processor/agentic-tools/outcomes.go:151` — `func correlateToolResult(call agentic.ToolCall, result agentic.ToolResult) agentic.ToolResult {`
- `processor/agentic-tools/outcomes.go:153` — `result.RequestID = call.RequestID`
- `processor/agentic-tools/outcomes.go:154` — `result.ExecutionID = call.ExecutionID`
- `processor/agentic-tools/outcomes.go:155` — `result.CallOrdinal = call.CallOrdinal`
- `processor/agentic-tools/outcomes.go:165` — `func decodeCompletedOutcome(data []byte, call agentic.ToolCall) (completedOutcome, error) {`
- `processor/agentic-tools/outcomes.go:177` — `if outcome.ExecutionID != call.ExecutionID {`
- `processor/agentic-tools/outcomes.go:180` — `if outcome.RequestID != call.RequestID {`
- `processor/agentic-tools/outcomes.go:183` — `if outcome.CallID != call.ID {`
- `processor/agentic-tools/outcomes.go:186` — `if outcome.CallOrdinal != call.CallOrdinal {`
- `processor/agentic-tools/outcomes.go:189` — `if outcome.Fingerprint != wantFingerprint {`
- `processor/agentic-tools/outcomes.go:192` — `if outcome.Result.CallID != call.ID || outcome.Result.RequestID != call.RequestID ||`
- `processor/agentic-tools/outcomes.go:193` — `outcome.Result.ExecutionID != call.ExecutionID || outcome.Result.CallOrdinal != call.CallOrdinal {`
- `processor/agentic-tools/outcomes.go:199` — `func compactTooLargeResult(call agentic.ToolCall) agentic.ToolResult {`
- `processor/agentic-tools/outcomes.go:200` — `return correlateToolResult(call, agentic.ToolResult{Error: "too_large", ErrorKind: agentic.ToolErrorInternal})`
- `processor/agentic-tools/outcomes.go:203` — `func compactPanicResult(call agentic.ToolCall) agentic.ToolResult {`
- `processor/agentic-tools/outcomes.go:204` — `return correlateToolResult(call, agentic.ToolResult{Error: "tool executor panicked", ErrorKind: agentic.ToolErrorInternal})`
- `processor/agentic-tools/outcomes.go:212` — `func isObservedOversize(err error) bool {`
- `processor/agentic-tools/outcomes.go:213` — `if errors.Is(err, nats.ErrMaxPayload) || errors.Is(err, jetstream.ErrMaxBytesExceeded) {`
- `processor/agentic-tools/outcomes.go:217` — `return errors.As(err, &apiErr) && apiErr != nil && apiErr.ErrorCode == jetstream.ErrorCode(10054)`
- `processor/agentic-loop/execution_identity.go:15` — `func stampToolExecutionCorrelation(requestID string, calls []agentic.ToolCall) error {`
- `processor/agentic-loop/execution_identity.go:24` — `calls[i].RequestID = requestID`
- `processor/agentic-loop/execution_identity.go:25` — `calls[i].CallOrdinal = ordinal`
- `processor/agentic-loop/execution_identity.go:26` — `calls[i].ExecutionID = deriveToolExecutionID(requestID, calls[i].ID, ordinal)`
- `processor/agentic-loop/execution_identity.go:31` — `func deriveToolExecutionID(requestID, callID string, ordinal uint32) string {`
- `processor/agentic-loop/execution_identity.go:32` — `hash := sha256.New()`
- `processor/agentic-loop/execution_identity.go:33` — `writeIdentityPart(hash, requestID)`
- `processor/agentic-loop/execution_identity.go:34` — `writeIdentityPart(hash, callID)`
- `processor/agentic-loop/execution_identity.go:35` — `var ordinalBytes [4]byte`
- `processor/agentic-loop/execution_identity.go:36` — `binary.BigEndian.PutUint32(ordinalBytes[:], ordinal)`
- `processor/agentic-loop/state.go:435` — `func validatedToolBatchResults(entity agentic.LoopEntity, requestID string, calls []agentic.ToolCall, incoming agentic.ToolResult, requirePreceding bool) (map[string]agentic.ToolResult, bool, error) {`
- `processor/agentic-loop/state.go:438` — `stored, ok := entity.PendingToolResults[call.ExecutionID]`
- `processor/agentic-loop/state.go:445` — `if stored.RequestID != call.RequestID || stored.ExecutionID != call.ExecutionID || stored.CallID != call.ID ||`
- `processor/agentic-loop/state.go:453` — `for key, stored := range entity.PendingToolResults {`
- `processor/agentic-loop/state.go:463` — `if stored.StopLoop || agentic.IsApprovalRequired(stored.Error) {`
- `processor/agentic-loop/state.go:473` — `func (m *LoopManager) restoreToolBatch(entity agentic.LoopEntity, request agentic.AgentRequest, response agentic.AgentResponse, incoming agentic.ToolResult) error {`
- `processor/agentic-loop/state.go:475` — `if err := stampToolExecutionCorrelation(response.RequestID, calls); err != nil {`
- `processor/agentic-loop/state.go:478` — `results, ordinaryBatch, err := validatedToolBatchResults(entity, request.RequestID, calls, incoming, true)`
- `processor/agentic-loop/state.go:482` — `// A full persisted ordinary batch is the pre-publication next-turn`
- `processor/agentic-loop/state.go:483` — `// checkpoint: handleToolsComplete already incremented the budget, but the`
- `processor/agentic-loop/state.go:484` — `// originating request is still current. Replaying that transition must not`
- `processor/agentic-loop/state.go:485` — `// spend another iteration. Approval waits never performed that increment.`
- `processor/agentic-loop/state.go:486` — `if len(results) == len(calls) && ordinaryBatch && entity.PendingApproval == nil && entity.Iterations > 0 {`
- `processor/agentic-loop/state.go:487` — `entity.Iterations--`
- `processor/agentic-loop/state.go:489` — `entity.PendingToolResults = results`
- `processor/agentic-loop/state.go:492` — `if err := m.restoreLoopFromRequest(entity, request, &assistant); err != nil {`
- `processor/agentic-loop/state.go:498` — `m.executionIDToName[call.ExecutionID] = call.Name`
- `processor/agentic-loop/state.go:499` — `m.executionIDToOrdinal[call.ExecutionID] = call.CallOrdinal`
- `processor/agentic-loop/state.go:500` — `m.executionIDToArguments[call.ExecutionID] = call.Arguments`
- `processor/agentic-loop/state.go:501` — `if call.ExecutionID == incoming.ExecutionID {`
- `processor/agentic-loop/state.go:502` — `m.toolCallToLoop[call.ExecutionID] = entity.ID`
- `processor/agentic-loop/state.go:503` — `m.pendingTools[entity.ID][call.ID] = true`
- `processor/agentic-loop/state.go:506` — `if _, complete := results[call.ExecutionID]; !complete {`
- `processor/agentic-loop/state.go:517` — `m.queuedToolCalls[entity.ID] = append(m.queuedToolCalls[entity.ID], call)`
- `processor/agentic-loop/state.go:1093` — `if entity.PendingToolResults == nil {`
- `processor/agentic-loop/state.go:1114` — `entity.PendingToolResults[resultKey] = result`
- `processor/agentic-loop/state.go:1139` — `func (m *LoopManager) GetAndClearToolResults(loopID string) []agentic.ToolResult {`
- `processor/agentic-loop/state.go:1156` — `for executionID, r := range entity.PendingToolResults {`
- `processor/agentic-loop/state.go:1161` — `entity.PendingToolResults = nil`
- `processor/agentic-tools/executor.go:11` — `)`
- `processor/agentic-tools/executor.go:13` — `// ToolExecutor defines the interface for tool executors. Execute`
- `processor/agentic-tools/executor.go:14` — `// implementations that may cause external effects MUST use ToolCall.ExecutionID`
- `processor/agentic-tools/executor.go:15` — `// as the framework identity supplied to any downstream idempotency contract.`
- `processor/agentic-tools/executor.go:16` — `// agentic-tools writes only COMPLETED outcomes; failure after the effect but`
- `processor/agentic-tools/executor.go:17` — `// before that write can redeliver the same execution.`
- `processor/agentic-tools/executor.go:18` — `type ToolExecutor interface {`
- `processor/agentic-tools/executor.go:19` — `Execute(ctx context.Context, call agentic.ToolCall) (agentic.ToolResult, error)`
- `processor/agentic-tools/executor.go:20` — `ListTools() []agentic.ToolDefinition`
- `processor/agentic-tools/executor.go:23` — `// ExecutorRegistry manages tool executors and provides thread-safe registration and execution`

## Consumers

- `processor/agentic-loop/state.go:446` — `stored.CallOrdinal != call.CallOrdinal || (stored.Name != call.Name && call.Name != "") ||`
- `processor/agentic-loop/settlement_recovery.go:575` — `retained.CallOrdinal != call.CallOrdinal || retained.ID != call.ID || retained.Name != call.Name ||`
- `processor/agentic-loop/settlement_recovery.go:611` — `if stored.Role == "tool" && stored.ToolCallID == result.CallID && stored.Name == result.Name {`
- `processor/agentic-tools/component.go:55` — `outcomes      completedOutcomeStore`
- `processor/agentic-tools/component.go:182` — `comp.publishStream = deps.NATSClient.PublishToStreamWithMsgID`
- `processor/agentic-tools/component.go:258` — `bucket, err = graph.EnsureCatalogBucket(runCtx, c.natsClient, graph.BucketToolCallOutcomes)`
- `processor/agentic-tools/component.go:259` — `c.outcomes = jetStreamCompletedOutcomeStore{bucket: bucket}`
- `processor/agentic-tools/component.go:424` — `deliveryPolicy, err := natsclient.ValidateHeartbeatDeliveryPolicy(`
- `processor/agentic-tools/component.go:427` — `return c.handleToolDelivery(workCtx, data)`
- `processor/agentic-tools/component.go:510` — `func (c *Component) handleToolDelivery(ctx context.Context, data []byte) (natsclient.DeliveryDecision, error) {`
- `processor/agentic-tools/component.go:511` — `err := c.handleToolCall(ctx, data)`
- `processor/agentic-tools/component.go:513` — `return natsclient.DeliveryDecisionAck, nil`
- `processor/agentic-tools/component.go:517` — `case errors.As(err, &permanent):`
- `processor/agentic-tools/component.go:518` — `return natsclient.DeliveryDecisionTerminate, err`
- `processor/agentic-tools/component.go:519` — `case isAmbiguousOutcomeCreateError(err):`
- `processor/agentic-tools/component.go:520` — `return natsclient.DeliveryDecisionQuarantine, err`
- `processor/agentic-tools/component.go:522` — `return natsclient.DeliveryDecisionRetry, err`
- `processor/agentic-tools/component.go:707` — `if c.outcomes == nil {`
- `processor/agentic-tools/component.go:710` — `if outcome, found, err := c.loadCompletedOutcome(ctx, call, storeOperationGet); err != nil {`
- `processor/agentic-tools/component.go:713` — `return c.publishCompletedResult(ctx, call, outcome.Result, outcomePathReplay)`
- `processor/agentic-tools/component.go:726` — `result := correlateToolResult(call, agentic.ToolResult{Error: rejection.message, ErrorKind: rejection.kind})`
- `processor/agentic-tools/component.go:748` — `// It also gets a distinct message ID so stream dedup cannot suppress`
- `processor/agentic-tools/component.go:750` — `err := c.publishResultWithMsgID(ctx, result, toolApprovalRequiredMessageID(call.ExecutionID))`
- `processor/agentic-tools/component.go:760` — `result, err := c.executeWithPanicRecovery(ctx, call)`
- `processor/agentic-tools/component.go:763` — `c.classifyToolOutcome(ctx, call, &result, err, duration)`
- `processor/agentic-tools/component.go:766` — `result = correlateToolResult(call, result)`
- `processor/agentic-tools/component.go:773` — `if err := c.persistAndPublishOutcome(ctx, call, result, outcomePathNew, true); err != nil {`
- `processor/agentic-tools/component.go:789` — `if recovered := recover(); recovered != nil {`
- `processor/agentic-tools/component.go:790` — `c.logger.Error("Tool executor panicked", "tool", call.Name, "ambiguous_effect", true)`
- `processor/agentic-tools/component.go:791` — `c.incrementErrors()`
- `processor/agentic-tools/component.go:793` — `c.metrics.recordAmbiguous(ambiguousCausePanic)`
- `processor/agentic-tools/component.go:795` — `result = compactPanicResult(call)`
- `processor/agentic-tools/component.go:799` — `return c.executeWithTimeout(ctx, call)`
- `processor/agentic-tools/component.go:802` — `func (c *Component) loadCompletedOutcome(`
- `processor/agentic-tools/component.go:805` — `data, err := c.outcomes.Get(ctx, toolCallOutcomeKey(call.ExecutionID))`
- `processor/agentic-tools/component.go:806` — `if errors.Is(err, jetstream.ErrKeyNotFound) {`
- `processor/agentic-tools/component.go:813` — `return completedOutcome{}, false, fmt.Errorf("read tool-call outcome: %w", err)`
- `processor/agentic-tools/component.go:815` — `outcome, err := decodeCompletedOutcome(data, call)`
- `processor/agentic-tools/component.go:822` — `c.metrics.recordStoreFailure(operation, storeReasonCorrupt)`
- `processor/agentic-tools/component.go:825` — `c.logger.Error("Irrecoverable tool-call outcome", "error", err)`
- `processor/agentic-tools/component.go:826` — `return completedOutcome{}, false, natsclient.TerminateDelivery(err)`
- `processor/agentic-tools/component.go:832` — `ctx context.Context, call agentic.ToolCall, result agentic.ToolResult, path outcomePath, effectful bool,`
- `processor/agentic-tools/component.go:834` — `winner, finalPath, err := c.persistCompletedOutcome(ctx, call, result, path, false, effectful)`
- `processor/agentic-tools/component.go:838` — `return c.publishCompletedResult(ctx, call, winner.Result, finalPath)`
- `processor/agentic-tools/component.go:841` — `func (c *Component) persistCompletedOutcome(`
- `processor/agentic-tools/component.go:844` — `record, err := newCompletedOutcome(call, result)`
- `processor/agentic-tools/component.go:852` — `err = c.outcomes.Create(ctx, toolCallOutcomeKey(call.ExecutionID), data)`
- `processor/agentic-tools/component.go:859` — `if errors.Is(err, jetstream.ErrKeyExists) {`
- `processor/agentic-tools/component.go:860` — `winner, found, readErr := c.loadCompletedOutcome(ctx, call, storeOperationReadWinner)`
- `processor/agentic-tools/component.go:870` — `return winner, outcomePathReplay, nil`
- `processor/agentic-tools/component.go:880` — `return c.persistCompletedOutcome(ctx, call, compactTooLargeResult(call), outcomePathCompact, true, effectful)`
- `processor/agentic-tools/component.go:887` — `c.metrics.recordAmbiguous(ambiguousCauseStoreFailure)`
- `processor/agentic-tools/component.go:889` — `c.logger.Error("Tool outcome persistence failed after execution", "error", err, "ambiguous_effect", true)`
- `processor/agentic-tools/component.go:891` — `// A failed Create after external execution is ambiguous: the lane must stop`
- `processor/agentic-tools/component.go:892` — `// without settlement because replay safety is not proven. Pre-effect Create`
- `processor/agentic-tools/component.go:893` — `// failures remain retryable. External-effect recovery remains governed by`
- `processor/agentic-tools/component.go:894` — `// each executor's operation-specific idempotency contract.`
- `processor/agentic-tools/component.go:895` — `createErr := fmt.Errorf("create tool-call outcome: %w", err)`
- `processor/agentic-tools/component.go:897` — `return completedOutcome{}, path, &ambiguousOutcomeCreateError{err: createErr}`
- `processor/agentic-tools/component.go:899` — `return completedOutcome{}, path, createErr`
- `processor/agentic-tools/component.go:902` — `func (c *Component) publishCompletedResult(`
- `processor/agentic-tools/component.go:905` — `err := c.publishResult(ctx, result)`
- `processor/agentic-tools/component.go:916` — `// exactly one compact transport surrogate using the same execution-derived MsgID.`
- `processor/agentic-tools/component.go:917` — `compact := compactTooLargeResult(call)`
- `processor/agentic-tools/component.go:918` — `if compactErr := c.publishResult(ctx, compact); compactErr != nil {`
- `processor/agentic-tools/component.go:920` — `return natsclient.TerminateDelivery(fmt.Errorf("publish compact tool result: %w", compactErr))`
- `processor/agentic-tools/component.go:1184` — `return c.publishResultWithMsgID(ctx, result, toolResultMessageID(result.ExecutionID))`
- `processor/agentic-tools/component.go:1187` — `func (c *Component) publishResultWithMsgID(ctx context.Context, result agentic.ToolResult, msgID string) error {`
- `processor/agentic-tools/component.go:1196` — `subject, err := component.ResolveSubject(c.outputPortDefs(), "tool.result", result.ExecutionID)`
- `processor/agentic-tools/component.go:1203` — `if err := c.publishStream(ctx, subject, data, msgID); err != nil {`
- `processor/agentic-tools/component.go:1205` — `reason := publishReasonTransport`
- `processor/agentic-tools/component.go:1226` — `func (c *Component) incrementErrors() {`
- `processor/agentic-loop/component.go:1745` — `func (c *Component) persistHandlerResult(ctx context.Context, result HandlerResult, revision uint64) error {`
- `processor/agentic-loop/component.go:1754` — `_, err := c.persistTerminalOutcome(ctx, result, candidate, revision)`
- `processor/agentic-loop/component.go:1758` — `if err := c.persistLoopState(ctx, result.LoopID); err != nil {`
- `processor/agentic-loop/component.go:1761` — `return c.publishResults(ctx, result)`
- `processor/agentic-loop/component.go:2085` — `func (c *Component) handleToolResultMessage(ctx context.Context, data []byte) (natsclient.DeliveryDecision, error) {`
- `processor/agentic-loop/component.go:2113` — `loopID, observedRevision, err = c.recoverToolResult(ctx, toolResult)`
- `processor/agentic-loop/component.go:2246` — `// persistHandlerResult covers publishResults + persistLoopState for all states,`
- `processor/agentic-loop/component.go:2249` — `if err := c.persistHandlerResult(ctx, result, observedRevision); err != nil {`
- `processor/agentic-loop/component.go:2250` — `c.releaseLoopTransientState(loopID)`
- `processor/agentic-loop/component.go:2282` — `func (c *Component) publishResults(ctx context.Context, result HandlerResult) error {`
- `processor/agentic-loop/settlement_recovery.go:434` — `func (c *Component) recoverToolResult(`
- `processor/agentic-loop/settlement_recovery.go:437` — `if result.RequestID == "" || result.ExecutionID == "" || result.CallOrdinal == 0 {`
- `processor/agentic-loop/settlement_recovery.go:451` — `entity, revision, err := c.readLoopEntityRevision(ctx, result.LoopID)`
- `processor/agentic-loop/settlement_recovery.go:458` — `response, found, err := c.readRetainedAgentResponse(ctx, result.RequestID)`
- `processor/agentic-loop/settlement_recovery.go:471` — `calls := append([]agentic.ToolCall(nil), response.Message.ToolCalls...)`
- `processor/agentic-loop/settlement_recovery.go:472` — `if err := stampToolExecutionCorrelation(response.RequestID, calls); err != nil {`
- `processor/agentic-loop/settlement_recovery.go:477` — `if call.ExecutionID != result.ExecutionID {`
- `processor/agentic-loop/settlement_recovery.go:480` — `if call.ID != result.CallID || call.CallOrdinal != result.CallOrdinal || call.Name != result.Name {`
- `processor/agentic-loop/settlement_recovery.go:501` — `request, found, err := c.readRetainedAgentRequest(ctx, result.LoopID)`
- `processor/agentic-loop/settlement_recovery.go:513` — `if c.config.ToolResultMaxBytes > 0 && len(result.Content) > c.config.ToolResultMaxBytes {`
- `processor/agentic-loop/settlement_recovery.go:531` — `// Approval-required history was classified above, with current gate`
- `processor/agentic-loop/settlement_recovery.go:535` — `applied, err := c.toolResultProvenInLaterRequest(request, calls, result)`
- `processor/agentic-loop/settlement_recovery.go:543` — `return "", 0, fmt.Errorf("later request %q lacks execution-specific applied proof for %q", request.RequestID, result.ExecutionID)`
- `processor/agentic-loop/settlement_recovery.go:545` — `if entity.State.IsTerminal() {`
- `processor/agentic-loop/settlement_recovery.go:546` — `return "", 0, proveTerminalToolResultApplied(entity, request.RequestID, calls, result)`
- `processor/agentic-loop/settlement_recovery.go:551` — `if err := c.handler.loopManager.restoreToolBatch(entity, request, response, result); err != nil {`
- `processor/agentic-loop/settlement_recovery.go:565` — `func (c *Component) toolResultProvenInLaterRequest(request agentic.AgentRequest, calls []agentic.ToolCall, result agentic.ToolResult) (bool, error) {`
- `processor/agentic-loop/settlement_recovery.go:568` — `if msg.Role != "assistant" || len(msg.ToolCalls) != len(calls) {`
- `processor/agentic-loop/settlement_recovery.go:573` — `retained := msg.ToolCalls[ordinal]`
- `processor/agentic-loop/settlement_recovery.go:574` — `if retained.ExecutionID != call.ExecutionID || retained.RequestID != call.RequestID ||`
- `processor/agentic-loop/settlement_recovery.go:601` — `// ordinal selects its tool message; a sibling cannot supply proof.`
- `processor/agentic-loop/settlement_recovery.go:602` — `resultIndex := index + int(result.CallOrdinal)`
- `processor/agentic-loop/settlement_recovery.go:614` — `} else if reflect.DeepEqual(stored, want) {`
- `processor/agentic-loop/settlement_recovery.go:615` — `return true, nil`
- `processor/agentic-loop/settlement_recovery.go:700` — `func proveTerminalToolResultApplied(entity agentic.LoopEntity, requestID string, calls []agentic.ToolCall, result agentic.ToolResult) error {`
- `processor/agentic-loop/settlement_recovery.go:701` — `results, ordinaryBatch, err := validatedToolBatchResults(entity, requestID, calls, result, true)`
- `processor/agentic-loop/settlement_recovery.go:705` — `stored, found := results[result.ExecutionID]`
- `processor/agentic-loop/settlement_recovery.go:713` — `if !reflect.DeepEqual(stored, result) {`
- `processor/agentic-loop/settlement_recovery.go:720` — `if entity.PendingApproval == nil && !agentic.IsApprovalRequired(result.Error) &&`
- `processor/agentic-loop/settlement_recovery.go:721` — `result.StopLoop && entity.State == agentic.LoopStateComplete &&`
- `processor/agentic-loop/settlement_recovery.go:722` — `entity.Outcome == agentic.OutcomeSuccess && entity.Result == result.Content {`
- `processor/agentic-loop/settlement_recovery.go:725` — `if entity.PendingApproval == nil && ordinaryBatch && len(results) == len(calls) &&`
- `processor/agentic-loop/settlement_recovery.go:726` — `entity.State == agentic.LoopStateFailed && entity.Outcome == agentic.OutcomeFailed &&`
- `processor/agentic-loop/settlement_recovery.go:727` — `entity.Iterations >= entity.MaxIterations && entity.Error == toolIterationLimitError(entity.MaxIterations) {`
- `processor/agentic-loop/settlement_recovery.go:730` — `return fmt.Errorf("terminal loop lacks execution-specific applied proof for %q", result.ExecutionID)`
- `processor/agentic-loop/handlers.go:2391` — `// Context manager reference for handleToolsComplete (tool results are added`
- `processor/agentic-loop/handlers.go:2400` — `return h.handleToolsComplete(ctx, loopID, entity, cm, &result)`
- `processor/agentic-loop/handlers.go:2539` — `func (h *MessageHandler) handleToolsComplete(`
- `processor/agentic-loop/handlers.go:2552` — `err := h.loopManager.IncrementIteration(loopID)`

## Adjacent claims

- `openspec/changes/agentic-loop-restart-safety/specs/agentic-tools/spec.md:3` — `### Requirement: Tool outcomes preserve framework execution correlation`
- `openspec/changes/agentic-loop-restart-safety/specs/agentic-tools/spec.md:5` — `Agentic-tools SHALL preserve RequestID and framework execution identity from `ToolCall` onto every `ToolResult`,`
- `openspec/changes/agentic-loop-restart-safety/specs/agentic-tools/spec.md:14` — `### Requirement: Completed tool outcome identity is globally unambiguous`
- `openspec/changes/agentic-loop-restart-safety/specs/agentic-tools/spec.md:16` — ``TOOL_CALL_OUTCOMES` SHALL key and fingerprint completed outcomes using framework execution identity while retaining`
- `openspec/changes/agentic-loop-restart-safety/specs/agentic-tools/spec.md:25` — `### Requirement: Tool replay remains the sole tool-effect recovery authority`
- `openspec/changes/agentic-loop-restart-safety/specs/agentic-tools/spec.md:27` — `Agentic-tools SHALL NOT add a claimed, started, checkpoint, or second outcome ledger for #1146. Post-effect and`
- `openspec/changes/agentic-loop-restart-safety/specs/agentic-tools/spec.md:28` — `pre-completion ambiguity remains governed by the executor's operation-specific idempotency contract.`
- `openspec/changes/agentic-loop-restart-safety/specs/agentic-tools/spec.md:36` — `### Requirement: Tool delivery retains the permanent typed owner contract`
- `openspec/changes/agentic-loop-restart-safety/specs/agentic-tools/spec.md:38` — `The existing `tool.execute` binding SHALL continue to use the permanent typed heartbeat owner, retain`
- `openspec/changes/agentic-loop-restart-safety/specs/agentic-tools/spec.md:40` — `its owner-stop observer. Correlation changes SHALL NOT introduce a second outcome owner or expose native settlement`
- `openspec/changes/agentic-loop-restart-safety/specs/agentic-tools/spec.md:49` — `### Requirement: Tool-result publication is durably at-least-once`
- `openspec/changes/agentic-loop-restart-safety/specs/agentic-tools/spec.md:51` — `Every required `ToolResult` publication SHALL carry framework execution identity and receive PubAck before source`
- `openspec/changes/agentic-loop-restart-safety/specs/agentic-tools/spec.md:52` — `ACK. PubAck uncertainty MAY repeat a result. `Nats-Msg-Id` MAY provide bounded duplicate suppression but SHALL NOT be`
- `openspec/changes/agentic-loop-restart-safety/specs/agentic-tools/spec.md:55` — `The exact immutable `TOOL_CALL_OUTCOMES` read exists only at the executor-effect boundary. Before executor invocation,`
- `openspec/changes/agentic-loop-restart-safety/specs/agentic-tools/spec.md:56` — `a matching outcome is replayed and a conflicting fingerprint quarantines. Ordinary ToolResult republication requires`
- `openspec/changes/agentic-loop-restart-safety/specs/agentic-tools/spec.md:57` — `no second exact output lookup, general stream scan, or second tool authority.`
- `openspec/changes/agentic-loop-restart-safety/specs/agentic-tools/spec.md:59` — `#### Scenario: Completed result publication repeats`
- `openspec/changes/agentic-loop-restart-safety/specs/agentic-tools/spec.md:65` — `#### Scenario: Completed outcome content conflicts`
- `openspec/changes/agentic-loop-restart-safety/specs/agentic-tools/spec.md:91` — `#### Scenario: completed call is redelivered after result publication failure`
- `openspec/changes/agentic-loop-restart-safety/specs/agentic-tools/spec.md:99` — `#### Scenario: same provider call ID carries different execution content`
- `openspec/changes/agentic-loop-restart-safety/specs/agentic-tools/spec.md:122` — `#### Scenario: full outcome exceeds the observed KV transport bound`
- `openspec/changes/agentic-loop-restart-safety/specs/agentic-tools/spec.md:137` — `#### Scenario: executor panics`
- `openspec/changes/agentic-loop-restart-safety/design.md:370` — ``ToolCall` and `ToolResult` carry RequestID and framework execution identity. `agentic-tools` stamps those fields on`
- `openspec/changes/agentic-loop-restart-safety/design.md:373` — `During an active tool batch, the latest `agent.request.<loopID>` remains the request that produced the exact`
- `openspec/changes/agentic-loop-restart-safety/design.md:374` — ``agent.response.<requestID>`. The response contains the ordered assistant tool calls. `LoopEntity` contains the`
- `openspec/changes/agentic-loop-restart-safety/design.md:375` — `accumulated pending results. These sources reconstruct the queue and next transition without a new ledger.`
- `openspec/changes/agentic-loop-restart-safety/design.md:377` — ``TOOL_CALL_OUTCOMES` remains the sole completed tool-outcome authority. Its identity evolves to include framework`
- `openspec/changes/agentic-loop-restart-safety/design.md:378` — `execution identity without creating another bucket.`
- `openspec/changes/agentic-loop-restart-safety/design.md:612` — `| 11 | tools `tool.execute`; heartbeat | immutable completed outcome exists and ToolResult receives PubAck | permanent invalid → Terminate; transient Store/publish → Retry; collision → Quarantine | `TOOL_CALL_OUTCOMES` is the executor-effect boundary; completed replay invokes no executor; result publication is at-least-once |`
- `openspec/changes/agentic-loop-restart-safety/design.md:706` — `Happy-path done follows #949: an immutable completed outcome exists and exact `ToolResult` has PubAck. Existing`
- `openspec/changes/agentic-loop-restart-safety/design.md:707` — `CallID or external-effect ambiguity remains operation-specific; #1146 adds no second ledger.`
- `openspec/changes/agentic-loop-restart-safety/design.md:709` — `### `tool.result``
- `openspec/changes/agentic-loop-restart-safety/design.md:711` — `Happy-path done is hydrated loop, reconstructed originating request and response, persisted result, and PubAck for`
- `openspec/changes/agentic-loop-restart-safety/design.md:712` — `the next queued tool, request, approval event, or terminal event. Missing continuation during a live turn retries. A`
- `openspec/changes/agentic-loop-restart-safety/design.md:713` — `bare terminal `LoopEntity` does not prove which `ToolResult` applied; cold tool-result delivery retries until task 5`
- `openspec/specs/agentic-tools/spec.md:435` — `### Requirement: Tool-call completion SHALL be durable before request acknowledgement`
- `openspec/specs/agentic-tools/spec.md:437` — ``agentic-tools` SHALL own one immutable COMPLETED outcome per logical `ToolCall.ID`. It SHALL read that outcome before`
- `openspec/specs/agentic-tools/spec.md:438` — `execution, validate its version, stored call ID, complete V1 request fingerprint, and result correlation, and publish a`
- `openspec/specs/agentic-tools/spec.md:439` — `matching stored result without invoking an executor. Missing state SHALL permit execution. Corrupt, colliding, or`
- `openspec/specs/agentic-tools/spec.md:440` — `mismatched state SHALL terminate the delivery.`
- `openspec/specs/agentic-tools/spec.md:442` — `After execution or policy rejection, the component SHALL Create-CAS the complete outcome. On a Create collision it`
- `openspec/specs/agentic-tools/spec.md:443` — `SHALL read and validate the winner and publish that authoritative winner. A transient read, Create, winner-read, or`
- `openspec/specs/agentic-tools/spec.md:444` — `result-publication failure SHALL delayed-NAK. The request SHALL ACK only after synchronous result publication receives`
- `openspec/specs/agentic-tools/spec.md:447` — `An initial `approval_required` result SHALL be nonterminal coordination and SHALL NOT be persisted as COMPLETED. It`
- `openspec/specs/agentic-tools/spec.md:448` — `SHALL use a phase-distinct deterministic message ID. An approved re-dispatch retains the original CallID; its approved`
- `openspec/specs/agentic-tools/spec.md:452` — `#### Scenario: completed call is redelivered after result publication failure`
- `openspec/specs/agentic-tools/spec.md:460` — `#### Scenario: same call ID carries different request content`
- `openspec/specs/agentic-tools/spec.md:467` — `### Requirement: Tool-result bounds SHALL be observed rather than predicted`
- `openspec/specs/agentic-tools/spec.md:469` — `The component SHALL first attempt the complete authoritative record and result. A typed observed full-record storage`
- `openspec/specs/agentic-tools/spec.md:470` — `rejection SHALL cause exactly one attempt to persist and publish a fixed compact correlated authority with`
- `openspec/specs/agentic-tools/spec.md:471` — ``ErrorKind=internal` and `Error=too_large`. The compact result SHALL retain only call, loop, and trace correlation`
- `openspec/specs/agentic-tools/spec.md:472` — `and SHALL contain no original content, error, metadata, or measured size. A compact rejection SHALL emit loud bounded`
- `openspec/specs/agentic-tools/spec.md:473` — `telemetry and terminate. The component SHALL NOT inspect configured payload limits or match error text.`
- `openspec/specs/agentic-tools/spec.md:475` — `If only publication of an already-stored full authority returns typed oversize, the component SHALL preserve that`
- `openspec/specs/agentic-tools/spec.md:477` — `surrogate PubAck permits request ACK. Surrogate failure SHALL terminate without recursion. Redelivery SHALL repeat the`
- `openspec/specs/agentic-tools/spec.md:480` — `#### Scenario: full outcome exceeds the observed KV transport bound`
- `openspec/specs/agentic-tools/spec.md:487` — `### Requirement: Executor panic and ambiguous pre-completion effects SHALL be explicit`
- `openspec/specs/agentic-tools/spec.md:489` — `An executor panic SHALL be recovered into a compact correlated internal result and follow normal completion. Exported`
- `openspec/specs/agentic-tools/spec.md:490` — `executor contracts SHALL state that effectful implementations use `ToolCall.ID` for downstream idempotency because a`
- `openspec/specs/agentic-tools/spec.md:493` — `#### Scenario: executor panics`
- `openspec/specs/agentic-tools/spec.md:499` — `### Requirement: Durable outcome telemetry SHALL use a closed bounded vocabulary`
- `openspec/specs/agentic-tools/spec.md:512` — `#### Scenario: an effect completes but outcome Create fails`
- `openspec/specs/agentic-tools/spec.md:514` — `- **WHEN** the executor returns after a possible effect and outcome Create fails transiently`
- `openspec/specs/agentic-tools/spec.md:515` — `- **THEN** `ambiguous_redeliveries_total{cause="store_failure"}` increments`
- `openspec/specs/agentic-tools/spec.md:516` — `- **AND** the error log carries `ambiguous_effect=true``
- `openspec/specs/agentic-tools/spec.md:517` — `- **AND** the delivery is delayed-NAKed`
- `openspec/changes/semantic-jetstream-settlement/design.md:243` — `completed-outcome replay publication may Retry; post-execution outcome-Create ambiguity quarantines. Dispatch retries`
- `openspec/changes/semantic-jetstream-settlement/tasks.md:66` — `- [x] 4.2 Encode tools done matrix: completed-outcome plus result PubAck ACK; completed replay publication Retry;`
- `openspec/changes/semantic-jetstream-settlement/tasks.md:67` — `immutable poison Term; post-execution outcome-Create ambiguity Quarantine.`

## Problem shape

- `processor/agentic-loop/component.go:1922` — `key := "COMPLETE_" + loopID`
- `processor/agentic-loop/component.go:1923` — `if _, err := c.loopsBucket.Create(ctx, key, data); err == nil {`
- `processor/agentic-loop/component.go:1925` — `} else if !errors.Is(err, jetstream.ErrKeyExists) {`
- `processor/agentic-loop/component.go:1928` — `entry, err := c.loopsBucket.Get(ctx, key)`
- `processor/agentic-loop/component.go:1932` — `data = entry.Value()`
- `processor/agentic-tools/component.go:710` — `if outcome, found, err := c.loadCompletedOutcome(ctx, call, storeOperationGet); err != nil {`
- `processor/agentic-tools/component.go:713` — `return c.publishCompletedResult(ctx, call, outcome.Result, outcomePathReplay)`
- `processor/agentic-tools/component.go:852` — `err = c.outcomes.Create(ctx, toolCallOutcomeKey(call.ExecutionID), data)`
- `processor/agentic-tools/component.go:859` — `if errors.Is(err, jetstream.ErrKeyExists) {`
- `processor/agentic-tools/component.go:860` — `winner, found, readErr := c.loadCompletedOutcome(ctx, call, storeOperationReadWinner)`

## Existing proof declarations and assertions

- `processor/agentic-tools/outcomes_test.go:23` — `type memoryOutcomeStore struct {`
- `processor/agentic-tools/outcomes_test.go:71` — `func TestPersistCompletedOutcomeDispositionTable(t *testing.T) {`
- `processor/agentic-tools/outcomes_test.go:114` — `type oversizeOnceStore struct {`
- `processor/agentic-tools/outcomes_test.go:126` — `func TestPersistCompletedOutcomeConcurrentCASConverges(t *testing.T) {`
- `processor/agentic-tools/outcomes_test.go:168` — `func TestExecuteWithPanicRecoveryProducesCompactInternalResult(t *testing.T) {`
- `processor/agentic-tools/outcomes_test.go:186` — `func TestApprovalGateSameIDRedispatchExecutesOnceAndPublishesTerminalResult(t *testing.T) {`
- `processor/agentic-tools/outcomes_test.go:244` — `func TestHandleToolCallPublishFailureReplaysWithoutExecutor(t *testing.T) {`
- `processor/agentic-tools/outcomes_test.go:276` — `func TestHandleToolCallPermanentDispositionTable(t *testing.T) {`
- `processor/agentic-tools/outcomes_test.go:308` — `func TestHandleToolDeliveryDecisionMatrix(t *testing.T) {`
- `processor/agentic-tools/outcomes_test.go:368` — `func TestToolCallOutcomeIdentityV1(t *testing.T) {`
- `processor/agentic-tools/outcomes_test.go:411` — `func TestDecodeCompletedOutcomeValidatesImmutableIdentity(t *testing.T) {`
- `processor/agentic-tools/outcomes_test.go:455` — `func TestCompactTooLargeResultDropsSensitiveAndSizeFields(t *testing.T) {`
- `processor/agentic-tools/outcomes_test.go:484` — `func TestObservedOversizeUsesTypedErrorsOnly(t *testing.T) {`
- `processor/agentic-tools/outcomes_test.go:491` — `func TestPublicationOversizeUsesOneCompactSurrogateWithoutReplacingAuthority(t *testing.T) {`
- `processor/agentic-tools/execution_identity_test.go:11` — `func TestCompletedOutcomeIdentitySeparatesRepeatedProviderCallID(t *testing.T) {`
- `processor/agentic-tools/execution_identity_test.go:20` — `require.NotEqual(t, toolCallOutcomeKey(first.ExecutionID), toolCallOutcomeKey(second.ExecutionID))`
- `processor/agentic-tools/execution_identity_test.go:26` — `record, err := newCompletedOutcome(first, firstResult)`
- `processor/agentic-tools/outcomes_integration_test.go:41` — `func TestIntegrationPostEffectCreateFailureIsAmbiguousAndLeavesNoAuthority(t *testing.T) {`
- `processor/agentic-tools/outcomes_integration_test.go:70` — `_, err = bucket.Get(ctx, toolCallOutcomeKey(call.ExecutionID))`
- `processor/agentic-tools/outcomes_integration_test.go:77` — `func TestIntegrationExecutorPanicCompletesWithCorrelatedInternalResult(t *testing.T) {`
- `processor/agentic-tools/outcomes_integration_test.go:97` — `entry, err := bucket.Get(ctx, toolCallOutcomeKey(call.ExecutionID))`
- `processor/agentic-tools/outcomes_integration_test.go:107` — `assert.Equal(t, agentic.ToolErrorInternal, outcome.Result.ErrorKind)`
- `processor/agentic-tools/outcomes_integration_test.go:113` — `func TestIntegrationConcurrentReplicasConvergeOnOneCompletedOutcome(t *testing.T) {`
- `processor/agentic-tools/outcomes_integration_test.go:162` — `restarted := &Component{outcomes: store, logger: slog.Default()}`
- `processor/agentic-tools/outcomes_integration_test.go:169` — `func TestIntegrationAckFailureRestartReplaysWithoutSecondExecution(t *testing.T) {`
- `processor/agentic-tools/outcomes_integration_test.go:215` — `// PubAck has arrived; sever the request consumer's connection before`
- `processor/agentic-tools/outcomes_integration_test.go:238` — `return testutil.ToFloat64(second.metrics.outcomeTotal.WithLabelValues(string(outcomePathReplay))) > replayBefore`
- `processor/agentic-tools/outcomes_integration_test.go:239` — `}, 20*time.Second, 100*time.Millisecond, "redelivery must traverse durable replay after configured 15s backoff")`
- `processor/agentic-tools/outcomes_integration_test.go:244` — `func TestIntegrationResultPublishFailureRestartReplaysStoredOutcome(t *testing.T) {`
- `processor/agentic-tools/outcomes_integration_test.go:353` — `assert.Equal(t, authority.Result, replayed, "replay must publish the exact stored authority")`
- `processor/agentic-tools/outcomes_integration_test.go:361` — `}, 5*time.Second, 50*time.Millisecond, "successful replay publication must permit request ACK")`
- `processor/agentic-tools/outcomes_integration_test.go:364` — `func TestIntegrationLowMaxPayloadStoresAndPublishesCompactAuthority(t *testing.T) {`
- `processor/agentic-tools/outcomes_integration_test.go:388` — `authority, err := decodeCompletedOutcome(entry.Value(), call)`
- `processor/agentic-loop/tool_result_recovery_test.go:17` — `func TestColdToolResultOrderedBatchCheckpoints(t *testing.T) {`
- `processor/agentic-loop/tool_result_recovery_test.go:18` — `for _, tc := range []struct {`
- `processor/agentic-loop/tool_result_recovery_test.go:19` — `prefix      int`
- `processor/agentic-loop/tool_result_recovery_test.go:20` — `taskRestore bool`
- `processor/agentic-loop/tool_result_recovery_test.go:21` — `priorTool   bool`
- `processor/agentic-loop/tool_result_recovery_test.go:22` — `}{{prefix: 0}, {prefix: 1}, {prefix: 2}, {prefix: 3}, {prefix: 3, taskRestore: true}, {priorTool: true}} {`
- `processor/agentic-loop/tool_result_recovery_test.go:24` — `t.Run(fmt.Sprintf("durable_prefix_%d/task_restored_%t/prior_tool_%t", prefix, tc.taskRestore, tc.priorTool), func(t *testing.T) {`
- `processor/agentic-loop/tool_result_recovery_test.go:90` — `require.Equal(t, natsclient.DeliveryDecisionRetry, decision, "missing preceding result must not skip a serial execution")`
- `processor/agentic-loop/tool_result_recovery_test.go:95` — `require.Equal(t, natsclient.DeliveryDecisionQuarantine, decision)`
- `processor/agentic-loop/tool_result_recovery_test.go:99` — `require.Equal(t, natsclient.DeliveryDecisionRetry, decision, "retention absence is not applied proof")`
- `processor/agentic-loop/tool_result_recovery_test.go:101` — `require.Equal(t, before, bucket.values[loopID], "unresolved or conflicting recovery must not advance durable state")`
- `processor/agentic-loop/tool_result_recovery_test.go:103` — `require.Error(t, err, "rejected recovery must not install a speculative loop")`
- `processor/agentic-loop/tool_result_recovery_test.go:115` — `require.NoError(t, c.persistHandlerResult(t.Context(), transition, revision))`
- `processor/agentic-loop/tool_result_recovery_test.go:130` — `require.False(t, durable.State.IsTerminal(), "replaying a charged checkpoint must not exhaust the budget again")`
- `processor/agentic-loop/tool_result_recovery_test.go:132` — `require.Equal(t, results[completed], durable.PendingToolResults[calls[completed].ExecutionID])`
- `processor/agentic-loop/tool_result_recovery_test.go:135` — `require.Equal(t, uint32(i+2), c.handler.loopManager.GetToolOrdinal(calls[i+1].ExecutionID))`
- `processor/agentic-loop/tool_result_recovery_test.go:137` — `require.True(t, routed, "next serial execution must be dispatched, despite repeated provider IDs")`
- `processor/agentic-loop/tool_result_recovery_test.go:142` — `require.Equal(t, 8, final.Iterations, "a replayed full checkpoint must not spend the iteration twice")`
- `processor/agentic-loop/tool_result_recovery_test.go:145` — `require.Equal(t, request.Messages, messages[:len(request.Messages)], "complete conversational history must survive replacement in order")`
- `processor/agentic-loop/tool_result_recovery_test.go:147` — `require.Equal(t, result.Content, messages[len(request.Messages)+1+i].Content, "results must follow originating call order")`
- `processor/agentic-loop/tool_result_recovery_test.go:159` — `require.Equal(t, messages, emittedContext[start:], "emitted request must contain intact prior and current exchanges in order")`
- `processor/agentic-loop/tool_result_recovery_test.go:169` — `require.Equal(t, natsclient.DeliveryDecisionAck, decision)`
- `processor/agentic-loop/tool_result_recovery_test.go:171` — `require.Error(t, err, "applied replay must not restore or mutate the current loop")`
- `processor/agentic-loop/tool_result_recovery_test.go:176` — `require.Equal(t, natsclient.DeliveryDecisionRetry, decision, "same provider ID cannot borrow a sibling's applied proof")`
- `processor/agentic-loop/terminal_tool_recovery_test.go:17` — `func TestColdTerminalToolResultRequiresExactAppliedEvidence(t *testing.T) {`
- `processor/agentic-loop/terminal_tool_recovery_test.go:53` — `require.Equal(t, natsclient.DeliveryDecisionRetry, decision)`
- `processor/agentic-loop/terminal_tool_recovery_test.go:55` — `require.Contains(t, bucket.values, "COMPLETE_"+loopID)`
- `processor/agentic-loop/terminal_tool_recovery_test.go:63` — `require.Equal(t, entity.MaxIterations, terminal.Iterations)`
- `processor/agentic-loop/terminal_tool_recovery_test.go:64` — `require.Equal(t, results[1], terminal.PendingToolResults[results[1].ExecutionID])`
- `processor/agentic-loop/terminal_tool_recovery_test.go:86` — `require.Equal(t, natsclient.DeliveryDecisionAck, decision)`
- `processor/agentic-loop/terminal_tool_recovery_test.go:92` — `t.Run("timeout at cap is not max iteration proof", func(t *testing.T) {`
- `processor/agentic-loop/terminal_tool_recovery_test.go:122` — `require.Equal(t, natsclient.DeliveryDecisionRetry, decision, "a timeout-at-cap marker is not the max-iteration consequence")`
- `processor/agentic-loop/terminal_tool_recovery_test.go:138` — `{name: "missing exact result", change: func(e *agentic.LoopEntity) { delete(e.PendingToolResults, results[1].ExecutionID) }, want: natsclient.DeliveryDecisionRetry},`
- `processor/agentic-loop/terminal_tool_recovery_test.go:139` — `{name: "missing batch prefix", change: func(e *agentic.LoopEntity) { delete(e.PendingToolResults, results[0].ExecutionID) }, want: natsclient.DeliveryDecisionRetry},`
- `processor/agentic-loop/terminal_tool_recovery_test.go:140` — `{name: "missing terminal consequence", change: func(e *agentic.LoopEntity) {`
- `processor/agentic-loop/terminal_tool_recovery_test.go:148` — `{name: "terminal with pending approval is malformed", change: func(e *agentic.LoopEntity) {`
- `processor/agentic-loop/terminal_tool_recovery_test.go:151` — `{name: "conflicting exact result", change: func(e *agentic.LoopEntity) {`
- `processor/agentic-loop/terminal_tool_recovery_test.go:156` — `{name: "conflicting ordinal", change: func(e *agentic.LoopEntity) {`
- `processor/agentic-loop/tool_result_redelivery_integration_test.go:43` — `func TestIntegrationColdToolResultRedeliveryUnblocksLaterApproval(t *testing.T) {`
- `processor/agentic-loop/terminal_tool_redelivery_integration_test.go:35` — `func TestIntegrationTerminalToolResultAppliedAfterReplacement(t *testing.T) {`
- `processor/agentic-loop/delivery_owner_test.go:74` — `func TestResponseAndToolResultPersistenceFailureCannotAck(t *testing.T) {`
- `processor/agentic-loop/settlement_recovery_test.go:283` — `func TestColdToolResultRestoresOriginatingBatch(t *testing.T) {`
- `processor/agentic-loop/settlement_recovery_test.go:412` — `func TestToolPersistenceRetryDiscardsWarmRoutingBeforeColdRedelivery(t *testing.T) {`
- `processor/agentic-loop/settlement_recovery_test.go:693` — `func TestTerminalToolSignalsFollowFinalMarker(t *testing.T) {`
- `processor/agentic-loop/settlement_recovery_test.go:751` — `func TestWarmToolResultRequiresExactExecutionCorrelationBeforeMutation(t *testing.T) {`

## Command candidates — NOT RUN

```sh
go test -race ./processor/agentic-tools -count=1
go test -race ./processor/agentic-loop -count=1
scripts/run-integration-tests.sh ./processor/agentic-tools -run '^TestIntegration(PostEffectCreateFailureIsAmbiguousAndLeavesNoAuthority|ExecutorPanicCompletesWithCorrelatedInternalResult|ConcurrentReplicasConvergeOnOneCompletedOutcome|AckFailureRestartReplaysWithoutSecondExecution|ResultPublishFailureRestartReplaysStoredOutcome|LowMaxPayloadStoresAndPublishesCompactAuthority)$' -v
scripts/run-integration-tests.sh ./processor/agentic-loop -run '^TestIntegration(ColdToolResultRedeliveryUnblocksLaterApproval|TerminalToolResultAppliedAfterReplacement)$' -v
```

## Source SHA-256

```text
3f3d13f67785cdae5d1cc292f61cfd93921fa0d62b915af83d6a744ff2f7d7f8  openspec/changes/agentic-loop-restart-safety/tasks.md
334b40a5f0d3ffbd79d07a17861147b4a463ef3de69142124c9e5c12f481c182  openspec/changes/agentic-loop-restart-safety/specs/agentic-loop/spec.md
b8cda089543a51956e620f6b677025540ff35b567e7ae06f033c3ec1d05890c0  openspec/changes/agentic-loop-restart-safety/specs/agentic-tools/spec.md
7786373006e6300dd2f3b2c3acfafa8fab1eb8618db690c58439bfdfea981835  agentic/tools.go
e248ccfaf6dae4f7758df6531bf681bab9c79028cb1d1b5baa45a7b71684d1d5  graph/constants.go
1d11cd6efe08b3af6bb7b7a7315e95b2b76164a31c8f709c4a63b1a986ad05c1  graph/kvcatalog.go
3118aec7367c44018038efdf36d82057b0fea23a7f6db5c52807fae3822ce7fb  processor/agentic-tools/outcomes.go
bd3f80c54874173208a2c0a51a3d2cb373dd50916a57438ea2df629759697b74  processor/agentic-loop/execution_identity.go
bf29ce8cdda9af3cce5ed24903406c9e89ddeabc01325c229f9adb349cd44de9  processor/agentic-loop/state.go
90faabe449bf1acf7d5f07a6206976c39a6c65d6f9c0943c969ff11e67db9aa8  processor/agentic-tools/executor.go
1bd566e56604eb2a8dfd09039a1ee4cff9810ed1beaface3a1e839825f34f895  processor/agentic-tools/component.go
6c1e7b44cba2c53e49e55e1f9f40c73c379922c8eed41fd08c76f6b44bc7a8b7  processor/agentic-loop/component.go
ba9524ddaa2d27a2e0668d6524491ec6646cc6ffaeff6a7acddbc0b56b3816a8  processor/agentic-loop/settlement_recovery.go
df31b69c7501384ac7e262aa5324ccb404543d81d4558ae2b699b3bf349a29fb  processor/agentic-loop/handlers.go
5f240fc0ef6cc903770e606a3426e00df3cd37d726b2e99909a2ded581bd8fe3  openspec/changes/agentic-loop-restart-safety/design.md
c887d939fc302a25d378468e0b0df704ee4bbd1886c69a822a7b3acf9c3eb429  openspec/specs/agentic-tools/spec.md
d9e816a16bbc0fb650a3d8fe278f27aa51f4201b3a7a3e8ddcfb8b790fbef2a4  openspec/changes/semantic-jetstream-settlement/design.md
c83ef13fbc1af33bd0ddaa9d0a055bbf25689d36b1fe72cd8dd3e2656ef1d722  openspec/changes/semantic-jetstream-settlement/tasks.md
8e52ef0896081c7178df5fdcd243aa4d9bd132704f767b7a613d0037711c3fc4  processor/agentic-tools/outcomes_test.go
5dc95377614502429b113d797dddbdd00813be2a34fd63f788ed4a4e94eb60b8  processor/agentic-tools/execution_identity_test.go
e097fa69757eb1e304105964367a99c55b07b60a3ea7796bb3528f5e20e5b3d0  processor/agentic-tools/outcomes_integration_test.go
f59a2119a6e5b8bc841b259f672b5ac662783943975846172447ac46f7c32122  processor/agentic-loop/tool_result_recovery_test.go
7864b6aa25e7e330df7d9a7f56798582ebbc7f396b49a8c432a810d73f3458ca  processor/agentic-loop/terminal_tool_recovery_test.go
78ca87c759e14f6e05f3f78c82ab77f307bf4c5e79e9541ae26ecb7968583c9a  processor/agentic-loop/tool_result_redelivery_integration_test.go
30e50c17189d163cf859e68eee897be68f2207d356564095b172038a54e12b63  processor/agentic-loop/terminal_tool_redelivery_integration_test.go
bda6bac4a7e31348286df19ea61858a3d2f1264b1cdac42455da9a9284400b04  processor/agentic-loop/delivery_owner_test.go
aa6dc93889f2a8cb7fd77bba03519319f8876a5b7f9432917c93c9157741a5dd  processor/agentic-loop/settlement_recovery_test.go
```

## Searches

All commands used the worktree above. Literal queries below were first read for pins and then replayed through
`| wc -l` for exact counts; truncated broad outputs are not exhaustive proof-name inventories.
The explicit R4 inventory path is untracked, so its tracked-only zero is not a source-absence assertion.

- `git grep -n -e 'R5' -e '5.1' -e '5.2' -e '5.3' -- openspec/changes/agentic-loop-restart-safety/tasks.md` → 13 hits; count projection also run.
- `git grep -n -e '^### .*Requirement' -e '^## MODIFIED' -- openspec/changes/agentic-loop-restart-safety/specs/agentic-tools/spec.md` → 9 hits; count projection also run.
- `git grep -n -E 'TOOL_CALL_OUTCOMES|completedTool|CompletedTool|toolOutcome|ToolOutcome|toolCallFingerprint|tool_call_fingerprint|executionFingerprint|ExecutionFingerprint' -- processor/agentic-tools processor/agentic-loop agentic` → 11 hits; count projection also run.
- `git grep -n -E 'tool result|ToolResult|completed-effect|partial batch|iteration' -- openspec/changes/agentic-loop-restart-safety/specs/agentic-loop/spec.md openspec/changes/agentic-loop-restart-safety/design.md` → 35 hits; count projection also run.
- `git grep -n -E '^func |^type |^const |^var ' -- processor/agentic-tools/outcomes.go processor/agentic-tools/replay.go processor/agentic-loop/tool_result_recovery.go` → 31 hits; count projection also run.
- `git grep -n -E '^func Test.*(Tool|Outcome|Executor|Panic|Oversize|Replay|Partial|Iteration|Batch)' -- processor/agentic-tools processor/agentic-loop` → 398 hits; count projection also run.
- `git grep -n -E 'outcomes|tool_result|terminal_tool|PendingToolResults' -- openspec/changes/agentic-loop-restart-safety/inventory-r4-evidence-2026-09-14.md` → 0 hits; count projection also run.
- `git grep -n -E 'tool.call.outcomes|tool-call-outcomes|tool_call_outcomes|TOOL_CALL_OUTCOMES|completed_outcomes|CompletedOutcomes|outcomeStore|outcomesBucket' -- processor/agentic-tools component agentic config openspec/specs/agentic-tools docs/adr docs/operations/migration-beta162-to-beta163.md` → 19 hits; count projection also run.
- `git grep -n -E 'Retire|immutable|panic|oversize|post-effect|ToolCall.ID|fingerprint' -- openspec/specs/agentic-tools/spec.md` → 15 hits; count projection also run.
- `git grep -n -E 'outcome|Outcome|replay|Replay|PubAck|oversize|panic' -- processor/agentic-tools/outcomes_integration_test.go processor/agentic-tools/completed_outcome_integration_test.go processor/agentic-tools/replay_integration_test.go` → 50 hits; count projection also run.
- `git grep -n -E 'BucketName|bucketName|EnsureKeyValueBucket|completedOutcomes|completedStore|resultPublisher|publishToolResult' -- processor/agentic-tools/component.go processor/agentic-tools/config.go` → 0 hits; count projection also run.
- `git grep -n -E 'outcomes:|outcomes |outcomes =|BucketToolCallOutcomes|publishResultWithMsgID|PublishToStream|handleToolDelivery|Heartbeat|isAmbiguousOutcomeCreateError|Quarantine|Abandon' -- processor/agentic-tools/component.go graph/bucket_catalog.go graph/buckets.go` → 19 hits; count projection also run.
- `git grep -n -E 'CallOrdinal|ExecutionID|RequestID|Fingerprint|sha256|ApprovedBy|too_large|recover\(|MaxPayload|ErrMax|ErrorKind' -- processor/agentic-tools/outcomes.go processor/agentic-loop/execution_identity.go` → 40 hits; count projection also run.
- `git grep -n -E 'tool.execute|tool.result|tool-call-outcomes|TOOL_CALL_OUTCOMES|tool_call_outcomes|execution_id|call_ordinal' -- processor/agentic-tools/config.go agentic/payload.go agentic/tool_call.go agentic/tool_result.go` → 3 hits; count projection also run.
- `git grep -n -E 'PendingToolResults|restoreToolBatch|ToolBatch|ToolCallOrder|ToolResults|IncrementIteration|GetAndClearToolResults|handleToolsComplete|persistHandlerResult|publishResults|recoverToolResult|toolResultProvenInLaterRequest|proveTerminalToolResultApplied' -- processor/agentic-loop/state.go processor/agentic-loop/handlers.go processor/agentic-loop/component.go processor/agentic-loop/settlement_recovery.go` → 92 hits; count projection also run.
- `git grep -n -E 'BucketToolCallOutcomes|TOOL_CALL_OUTCOMES|tool_call_outcomes|tool-call-outcomes' -- graph` → 5 hits; count projection also run.
- `git grep -n -E 'RequestID|ExecutionID|CallOrdinal|ToolCall|ToolResult' -- agentic/messages.go agentic/tool.go agentic/tools.go` → 47 hits; count projection also run.
- `git grep -n -E 't.Run\(|name:|before|after|require\.|assert\.' -- processor/agentic-loop/tool_result_recovery_test.go processor/agentic-loop/terminal_tool_recovery_test.go` → 98 hits; count projection also run.
- `git grep -n -E '^### Requirement:|^#### Scenario:|SHALL|typed|retry|idempot|CallID|ToolCall.ID' -- openspec/changes/agentic-loop-restart-safety/specs/agentic-tools/spec.md openspec/specs/agentic-tools/spec.md` → 160 hits; count projection also run.
- `git grep -n -E 'R5|5.1|5.2|5.3|tool_result_recovery|outcomes_integration|immutable|idempotency' -- openspec/changes/agentic-loop-restart-safety/proposal.md docs/operations/migration-beta162-to-beta163.md` → 15 hits; count projection also run.
- `git grep -n -E 'identity|ordinal|batch|completed|ToolResult|PendingToolResults|iteration|request_id|execution_id|tool.execute|tool.result' -- openspec/changes/agentic-loop-restart-safety/specs/agentic-loop/spec.md` → 73 hits; count projection also run.
- `git grep -n -E 'TOOL_CALL_OUTCOMES|framework execution identity|operation-specific idempotency|completed-outcome' -- docs/adr openspec/changes ':(exclude)openspec/changes/archive/**' ':(exclude)openspec/changes/agentic-loop-restart-safety/**'` → 9 hits; count projection also run.
- `git grep -n -E 'idempotency|reconciliation|ambiguous|ToolCall.ID' -- processor/agentic-tools/executor.go processor/agentic-tools/doc.go agentic/executor.go agentic/tool_executor.go` → 1 hits; count projection also run.
- `git grep -n -E 'ErrKeyExists|COMPLETE_|Create\(' -- processor/agentic-loop/component.go` → 5 hits; count projection also run.

### Structure queries

- `git grep -n -E 'toolResult.Name|result.Name|stored.Name|call.Name' -- processor/agentic-loop/settlement_recovery.go processor/agentic-loop/state.go processor/agentic-tools/outcomes.go` → 8 hits; final bounded Name spelling read requested by root.
- `gopls workspace_symbol -matcher=fuzzy completedToolOutcome` → 0 usable results; sandbox Go-cache access denied.
- `gopls workspace_symbol -matcher=fuzzy completedOutcome` → 28 result lines, authorized existing-cache read.
- `gopls call_hierarchy processor/agentic-tools/outcomes.go:118:6` → 4 callers, 3 callees, 1 identifier.
- `gopls implementation processor/agentic-tools/outcomes.go:35:6` → 3 implementers.
- `gopls workspace_symbol -matcher=fuzzy recoverToolResult` → 1 result.
- `gopls call_hierarchy processor/agentic-loop/settlement_recovery.go:434:21` → 1 caller, 22 callees, 1 identifier.
- `gopls references processor/agentic-tools/outcomes.go:82:6` → 6 references.
- `gopls workspace_symbol -matcher=fuzzy TestIntegrationOutcome` → 0 results in default build configuration.
- `gopls symbols processor/agentic-tools/outcomes_integration_test.go` → 11 symbol lines, including 6 test names.
- `gopls symbols processor/agentic-tools/outcomes_test.go` → 32 symbol lines, including 12 test names.
- `gopls symbols processor/agentic-loop/tool_result_recovery_test.go` → 1 test name.
- `gopls symbols processor/agentic-loop/tool_result_redelivery_integration_test.go` → 6 symbol lines, including 1 test name.
- `gopls symbols processor/agentic-loop/terminal_tool_redelivery_integration_test.go` → 4 symbol lines, including 1 test name.

### Pinned reads and baseline checks

- `sed -n '1,260p' .agents/contracts/semstreams-explorer.md` → read completed.
- `git rev-parse HEAD` → read completed.
- `sed -n '1,100p' openspec/project.md` → read completed.
- `git status --short` → read completed.
- `sed -n '186,199p' openspec/changes/agentic-loop-restart-safety/tasks.md` → read completed.
- `sed -n '1,155p' openspec/changes/agentic-loop-restart-safety/specs/agentic-tools/spec.md` → read completed.
- `sed -n '350,421p' openspec/changes/agentic-loop-restart-safety/design.md` → read completed.
- `sed -n '432,560p' processor/agentic-loop/settlement_recovery.go` → read completed.
- `sed -n '689,927p' processor/agentic-tools/component.go` → read completed.
- `sed -n '470,536p' processor/agentic-loop/state.go` → read completed.
- `sed -n '563,619p' processor/agentic-loop/settlement_recovery.go` → read completed.
- `sed -n '698,742p' processor/agentic-loop/settlement_recovery.go` → read completed.
- `sed -n '509,530p' processor/agentic-tools/component.go` → read completed.
- `sed -n '787,838p' processor/agentic-tools/component.go` → read completed.
- `sed -n '1187,1218p' processor/agentic-tools/component.go` → read completed.
- `sed -n '435,469p' processor/agentic-loop/state.go` → read completed.
- `sed -n '2240,2265p' processor/agentic-loop/component.go` → read completed.
- `sed -n '2538,2556p' processor/agentic-loop/handlers.go` → read completed.
- `sed -n '1,31p' processor/agentic-tools/executor.go` → read completed.
- `sed -n '1,32p' processor/agentic-loop/tool_result_recovery_test.go` → read completed.
- `sed -n '512,518p' openspec/specs/agentic-tools/spec.md` → read completed.
- `awk 'FNR==186 || FNR==187 || FNR==188 || FNR==189 || FNR==190 {print FILENAME ":" FNR ":" $0}' 'openspec/changes/agentic-loop-restart-safety/tasks.md'` → read completed.
- `awk 'FNR==360 || FNR==361 || FNR==362 || FNR==367 || FNR==368 || FNR==369 || FNR==476 || FNR==478 || FNR==479 || FNR==480 {print FILENAME ":" FNR ":" $0}' 'openspec/changes/agentic-loop-restart-safety/specs/agentic-loop/spec.md'` → read completed.
- `awk 'FNR==3 || FNR==5 || FNR==14 || FNR==16 || FNR==25 || FNR==27 || FNR==28 || FNR==36 || FNR==38 || FNR==40 || FNR==49 || FNR==51 || FNR==52 || FNR==55 || FNR==56 || FNR==57 || FNR==59 || FNR==65 || FNR==72 || FNR==74 || FNR==75 || FNR==76 || FNR==77 || FNR==78 || FNR==79 || FNR==81 || FNR==82 || FNR==83 || FNR==84 || FNR==86 || FNR==88 || FNR==91 || FNR==99 || FNR==107 || FNR==109 || FNR==110 || FNR==111 || FNR==112 || FNR==113 || FNR==116 || FNR==119 || FNR==122 || FNR==129 || FNR==131 || FNR==132 || FNR==133 || FNR==134 || FNR==135 || FNR==137 || FNR==144 || FNR==148 {print FILENAME ":" FNR ":" $0}' 'openspec/changes/agentic-loop-restart-safety/specs/agentic-tools/spec.md'` → read completed.
- `awk 'FNR==207 || FNR==214 || FNR==215 || FNR==216 || FNR==629 || FNR==639 || FNR==640 || FNR==641 {print FILENAME ":" FNR ":" $0}' 'agentic/tools.go'` → read completed.
- `awk 'FNR==47 || FNR==48 || FNR==49 {print FILENAME ":" FNR ":" $0}' 'graph/constants.go'` → read completed.
- `awk 'FNR==150 || FNR==151 || FNR==152 {print FILENAME ":" FNR ":" $0}' 'graph/kvcatalog.go'` → read completed.
- `awk 'FNR==19 || FNR==25 || FNR==26 || FNR==27 || FNR==28 || FNR==29 || FNR==30 || FNR==31 || FNR==32 || FNR==35 || FNR==36 || FNR==37 || FNR==40 || FNR==42 || FNR==50 || FNR==55 || FNR==56 || FNR==57 || FNR==77 || FNR==78 || FNR==82 || FNR==83 || FNR==86 || FNR==87 || FNR==90 || FNR==91 || FNR==94 || FNR==97 || FNR==98 || FNR==99 || FNR==100 || FNR==101 || FNR==102 || FNR==103 || FNR==104 || FNR==105 || FNR==106 || FNR==107 || FNR==108 || FNR==109 || FNR==118 || FNR==129 || FNR==138 || FNR==151 || FNR==153 || FNR==154 || FNR==155 || FNR==165 || FNR==177 || FNR==180 || FNR==183 || FNR==186 || FNR==189 || FNR==192 || FNR==193 || FNR==199 || FNR==200 || FNR==203 || FNR==204 || FNR==212 || FNR==213 || FNR==217 {print FILENAME ":" FNR ":" $0}' 'processor/agentic-tools/outcomes.go'` → read completed.
- `awk 'FNR==15 || FNR==23 || FNR==24 || FNR==25 || FNR==26 || FNR==31 || FNR==32 || FNR==33 || FNR==34 || FNR==35 || FNR==36 {print FILENAME ":" FNR ":" $0}' 'processor/agentic-loop/execution_identity.go'` → read completed.
- `awk 'FNR==435 || FNR==438 || FNR==443 || FNR==445 || FNR==453 || FNR==460 || FNR==463 || FNR==473 || FNR==475 || FNR==478 || FNR==482 || FNR==483 || FNR==484 || FNR==485 || FNR==486 || FNR==487 || FNR==489 || FNR==492 || FNR==498 || FNR==499 || FNR==500 || FNR==501 || FNR==502 || FNR==503 || FNR==506 || FNR==517 || FNR==1093 || FNR==1114 || FNR==1139 || FNR==1156 || FNR==1161 {print FILENAME ":" FNR ":" $0}' 'processor/agentic-loop/state.go'` → read completed.
- `awk 'FNR==11 || FNR==12 || FNR==13 || FNR==14 || FNR==15 || FNR==16 || FNR==17 {print FILENAME ":" FNR ":" $0}' 'processor/agentic-tools/executor.go'` → read completed.
- `awk 'FNR==55 || FNR==182 || FNR==258 || FNR==259 || FNR==424 || FNR==427 || FNR==510 || FNR==511 || FNR==513 || FNR==517 || FNR==518 || FNR==519 || FNR==520 || FNR==522 || FNR==707 || FNR==710 || FNR==713 || FNR==725 || FNR==726 || FNR==748 || FNR==750 || FNR==760 || FNR==763 || FNR==766 || FNR==773 || FNR==791 || FNR==792 || FNR==798 || FNR==802 || FNR==805 || FNR==806 || FNR==810 || FNR==817 || FNR==822 || FNR==829 || FNR==832 || FNR==836 || FNR==841 || FNR==844 || FNR==852 || FNR==859 || FNR==860 || FNR==869 || FNR==871 || FNR==880 || FNR==889 || FNR==895 || FNR==897 || FNR==902 || FNR==905 || FNR==916 || FNR==917 || FNR==918 || FNR==1184 || FNR==1187 || FNR==1196 || FNR==1205 || FNR==1226 {print FILENAME ":" FNR ":" $0}' 'processor/agentic-tools/component.go'` → read completed.
- `awk 'FNR==1745 || FNR==1754 || FNR==1758 || FNR==1761 || FNR==1922 || FNR==1923 || FNR==1925 || FNR==1928 || FNR==1932 || FNR==2085 || FNR==2113 || FNR==2246 || FNR==2249 || FNR==2282 {print FILENAME ":" FNR ":" $0}' 'processor/agentic-loop/component.go'` → read completed.
- `awk 'FNR==434 || FNR==437 || FNR==451 || FNR==458 || FNR==471 || FNR==472 || FNR==477 || FNR==480 || FNR==501 || FNR==513 || FNR==531 || FNR==535 || FNR==543 || FNR==545 || FNR==546 || FNR==551 || FNR==565 || FNR==568 || FNR==573 || FNR==574 || FNR==599 || FNR==601 || FNR==602 || FNR==613 || FNR==700 || FNR==701 || FNR==705 || FNR==713 || FNR==720 || FNR==721 || FNR==722 || FNR==725 || FNR==726 || FNR==727 || FNR==730 {print FILENAME ":" FNR ":" $0}' 'processor/agentic-loop/settlement_recovery.go'` → read completed.
- `awk 'FNR==2391 || FNR==2400 || FNR==2539 || FNR==2552 {print FILENAME ":" FNR ":" $0}' 'processor/agentic-loop/handlers.go'` → read completed.
- `awk 'FNR==370 || FNR==373 || FNR==374 || FNR==375 || FNR==377 || FNR==378 || FNR==612 || FNR==706 || FNR==707 || FNR==708 || FNR==709 || FNR==710 || FNR==711 || FNR==712 || FNR==713 {print FILENAME ":" FNR ":" $0}' 'openspec/changes/agentic-loop-restart-safety/design.md'` → read completed.
- `awk 'FNR==435 || FNR==437 || FNR==438 || FNR==439 || FNR==440 || FNR==442 || FNR==443 || FNR==444 || FNR==447 || FNR==448 || FNR==452 || FNR==460 || FNR==467 || FNR==469 || FNR==470 || FNR==471 || FNR==472 || FNR==473 || FNR==475 || FNR==477 || FNR==480 || FNR==487 || FNR==489 || FNR==490 || FNR==493 || FNR==499 || FNR==512 {print FILENAME ":" FNR ":" $0}' 'openspec/specs/agentic-tools/spec.md'` → read completed.
- `awk 'FNR==243 {print FILENAME ":" FNR ":" $0}' 'openspec/changes/semantic-jetstream-settlement/design.md'` → read completed.
- `awk 'FNR==66 || FNR==67 {print FILENAME ":" FNR ":" $0}' 'openspec/changes/semantic-jetstream-settlement/tasks.md'` → read completed.
- `awk 'FNR==23 || FNR==71 || FNR==114 || FNR==126 || FNR==168 || FNR==186 || FNR==244 || FNR==276 || FNR==308 || FNR==368 || FNR==411 || FNR==455 || FNR==484 || FNR==491 {print FILENAME ":" FNR ":" $0}' 'processor/agentic-tools/outcomes_test.go'` → read completed.
- `awk 'FNR==11 || FNR==20 || FNR==26 {print FILENAME ":" FNR ":" $0}' 'processor/agentic-tools/execution_identity_test.go'` → read completed.
- `awk 'FNR==41 || FNR==70 || FNR==77 || FNR==97 || FNR==107 || FNR==113 || FNR==162 || FNR==169 || FNR==215 || FNR==238 || FNR==239 || FNR==244 || FNR==353 || FNR==361 || FNR==364 || FNR==388 {print FILENAME ":" FNR ":" $0}' 'processor/agentic-tools/outcomes_integration_test.go'` → read completed.
- `awk 'FNR==17 || FNR==24 || FNR==90 || FNR==95 || FNR==99 || FNR==101 || FNR==103 || FNR==115 || FNR==130 || FNR==132 || FNR==135 || FNR==137 || FNR==142 || FNR==145 || FNR==147 || FNR==159 || FNR==169 || FNR==171 || FNR==176 {print FILENAME ":" FNR ":" $0}' 'processor/agentic-loop/tool_result_recovery_test.go'` → read completed.
- `awk 'FNR==17 || FNR==53 || FNR==55 || FNR==63 || FNR==64 || FNR==86 || FNR==92 || FNR==122 || FNR==138 || FNR==139 || FNR==140 || FNR==148 || FNR==151 || FNR==156 {print FILENAME ":" FNR ":" $0}' 'processor/agentic-loop/terminal_tool_recovery_test.go'` → read completed.
- `awk 'FNR==43 {print FILENAME ":" FNR ":" $0}' 'processor/agentic-loop/tool_result_redelivery_integration_test.go'` → read completed.
- `awk 'FNR==35 {print FILENAME ":" FNR ":" $0}' 'processor/agentic-loop/terminal_tool_redelivery_integration_test.go'` → read completed.
- `awk 'FNR==74 {print FILENAME ":" FNR ":" $0}' 'processor/agentic-loop/delivery_owner_test.go'` → read completed.
- `awk 'FNR==283 || FNR==412 || FNR==693 || FNR==751 {print FILENAME ":" FNR ":" $0}' 'processor/agentic-loop/settlement_recovery_test.go'` → read completed.
- `awk 'FNR==789 || FNR==790 || FNR==793 || FNR==795 || FNR==796 || FNR==799 || FNR==813 || FNR==815 || FNR==825 || FNR==826 || FNR==834 || FNR==838 || FNR==870 || FNR==887 || FNR==890 || FNR==891 || FNR==892 || FNR==893 || FNR==894 || FNR==899 || FNR==920 || FNR==1203 {print FILENAME ":" FNR ":" $0}' 'processor/agentic-tools/component.go'` → read completed.
- `awk 'FNR==614 || FNR==615 {print FILENAME ":" FNR ":" $0}' 'processor/agentic-loop/settlement_recovery.go'` → read completed.
- `awk 'FNR==2250 || FNR==2252 || FNR==2257 {print FILENAME ":" FNR ":" $0}' 'processor/agentic-loop/component.go'` → read completed.
- `awk 'FNR==18 || FNR==19 || FNR==20 || FNR==21 || FNR==22 {print FILENAME ":" FNR ":" $0}' 'processor/agentic-loop/tool_result_recovery_test.go'` → read completed.
- `awk 'FNR==18 || FNR==19 || FNR==20 || FNR==21 || FNR==22 || FNR==23 {print FILENAME ":" FNR ":" $0}' 'processor/agentic-tools/executor.go'` → read completed.
- `awk 'FNR==514 || FNR==515 || FNR==516 || FNR==517 {print FILENAME ":" FNR ":" $0}' 'openspec/specs/agentic-tools/spec.md'` → read completed.
- `shasum -a 256 'openspec/changes/agentic-loop-restart-safety/tasks.md' 'openspec/changes/agentic-loop-restart-safety/specs/agentic-loop/spec.md' 'openspec/changes/agentic-loop-restart-safety/specs/agentic-tools/spec.md' 'agentic/tools.go' 'graph/constants.go' 'graph/kvcatalog.go' 'processor/agentic-tools/outcomes.go' 'processor/agentic-loop/execution_identity.go' 'processor/agentic-loop/state.go' 'processor/agentic-tools/executor.go' 'processor/agentic-tools/component.go' 'processor/agentic-loop/component.go' 'processor/agentic-loop/settlement_recovery.go' 'processor/agentic-loop/handlers.go' 'openspec/changes/agentic-loop-restart-safety/design.md' 'openspec/specs/agentic-tools/spec.md' 'openspec/changes/semantic-jetstream-settlement/design.md' 'openspec/changes/semantic-jetstream-settlement/tasks.md' 'processor/agentic-tools/outcomes_test.go' 'processor/agentic-tools/execution_identity_test.go' 'processor/agentic-tools/outcomes_integration_test.go' 'processor/agentic-loop/tool_result_recovery_test.go' 'processor/agentic-loop/terminal_tool_recovery_test.go' 'processor/agentic-loop/tool_result_redelivery_integration_test.go' 'processor/agentic-loop/terminal_tool_redelivery_integration_test.go' 'processor/agentic-loop/delivery_owner_test.go' 'processor/agentic-loop/settlement_recovery_test.go'` → read completed.
- `git diff --stat -- processor/agentic-tools processor/agentic-loop` → read completed.

### NOT RUN

- `gh issue list --search "tool outcome" --state open --json number,title` — root-owned live claim check.
- `gh pr list --json number,title,body` — root-owned live claim check.
- `openspec list` — root-owned active-change reconciliation.
- All unit, integration, E2E, and race test commands — root-owned execution; declarations only here.
- Additional tagged workspace-symbol and complete ToolExecutor implementer/caller expansion — outside this bounded evidence inventory.
- Additional environment-variable spellings, every executor-specific idempotency contract, and sister-repository asks — not enumerated.
- Additional whole-repository ToolResult/ExecutionID consumers, R7 governance, R8 admission, R10 lifecycle, and R4 task/response foundation — not swept.
- Direct R4-inventory pin reuse — tracked-only query returned zero; independently located overlapping proof pins are present above.

### Inventory check

- `task inventory:verify -- openspec/changes/agentic-loop-restart-safety/inventory-r5-tool-evidence-2026-09-14.md` → initial 444/444 PASS, no drift; repeated after three bounded Name pins.
- `shasum -a 256 openspec/changes/agentic-loop-restart-safety/inventory-r5-tool-evidence-2026-09-14.md` → read completed; final hash returned in handoff.

## Bounded Name-owner supplement

Root materialized the architect/reviewer-requested omission pins before implementation. The original source hashes
above still apply. This supplements the existing inventory; it does not extend R5's scope.

- `agentic/tools.go:631` — `Name        string         `json:"name,omitempty"` // Tool function name (required by Gemini on tool result messages)`
- `agentic/tools.go:645` — `// Validate checks if the ToolResult is valid`
- `agentic/tools.go:646` — `func (t ToolResult) Validate() error {`
- `agentic/tools.go:647` — `if t.CallID == "" {`
- `agentic/tools.go:648` — `return fmt.Errorf("tool result call_id required")`
- `agentic/tools.go:650` — `return nil`
- `processor/agentic-tools/outcomes.go:152` — `result.CallID = call.ID`
- `processor/agentic-tools/outcomes.go:156` — `result.LoopID = call.LoopID`
- `processor/agentic-tools/outcomes.go:157` — `result.TraceID = call.TraceID`
- `processor/agentic-tools/outcomes.go:158` — `return result`
- `processor/agentic-loop/component.go:2143` — `wantExecutionID := deriveToolExecutionID(toolResult.RequestID, toolResult.CallID, toolResult.CallOrdinal)`
- `processor/agentic-loop/component.go:2144` — `if toolResult.ExecutionID != wantExecutionID ||`
- `processor/agentic-loop/component.go:2145` — `c.handler.loopManager.GetToolName(toolResult.ExecutionID) != toolResult.Name ||`
- `processor/agentic-loop/component.go:2146` — `c.handler.loopManager.GetToolOrdinal(toolResult.ExecutionID) != toolResult.CallOrdinal {`
- `processor/agentic-loop/component.go:2151` — `return natsclient.DeliveryDecisionQuarantine, err`
- `processor/agentic-loop/handlers.go:2111` — `// resolveToolName resolves the function name of a tool result through the`
- `processor/agentic-loop/handlers.go:2112` — `// loop's name-fallback chain: the name tracked for this execution ID`
- `processor/agentic-loop/handlers.go:2113` — `// first, then the name carried on the result envelope itself. The fallback`
- `processor/agentic-loop/handlers.go:2114` — `// is what survives a LoopManager cache loss (process restart) — agentic-tools`
- `processor/agentic-loop/handlers.go:2115` — `// stamps Name on every result before publishing, so the envelope always`
- `processor/agentic-loop/handlers.go:2116` — `// carries it. Returns "" only when neither source knows the name.`
- `processor/agentic-loop/handlers.go:2117` — `func (h *MessageHandler) resolveToolName(toolResult agentic.ToolResult) string {`
- `processor/agentic-loop/handlers.go:2118` — `if tracked := h.loopManager.GetToolName(toolResult.ExecutionID); tracked != "" {`
- `processor/agentic-loop/handlers.go:2119` — `return tracked`
- `processor/agentic-loop/handlers.go:2121` — `return toolResult.Name`
- `processor/agentic-loop/state.go:1006` — `func (m *LoopManager) GetToolName(executionID string) string {`
- `processor/agentic-loop/state.go:1009` — `return m.executionIDToName[executionID]`

Adopter seam: an executor author returns a ToolResult while agentic-tools stamps framework correlation. Name is
optional on the registered payload and the compact result deliberately omits it. The current loop guard refuses
that valid result despite owning the dispatched name. No new author-facing field obligation follows from this
inventory; exact execution identity and any present name remain independently checked.

Supplement reads: `nl -ba agentic/tools.go | sed -n '625,654p'`,
`nl -ba processor/agentic-tools/outcomes.go | sed -n '151,158p'`,
`nl -ba processor/agentic-loop/component.go | sed -n '2110,2165p'`,
`nl -ba processor/agentic-loop/handlers.go | sed -n '2111,2122p'`, and
`nl -ba processor/agentic-loop/settlement_recovery.go | sed -n '445,505p'`, and
`nl -ba processor/agentic-loop/state.go | sed -n '1003,1014p'` — completed, no additional sweep.

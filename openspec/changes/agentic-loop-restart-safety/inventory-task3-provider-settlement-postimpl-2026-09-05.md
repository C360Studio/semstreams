# Inventory: task 3 provider settlement post-implementation
base: db00ad492864d04a7a841831edd600e6f800092f
working-tree: current uncommitted task 3 implementation and tests

## Claimed gap

- `openspec/changes/agentic-loop-restart-safety/tasks.md:121` — `- [x] 3.1 RED: add first-delivery, matching-retained-response, typed-absence redelivery, retained-read failure,`
- `openspec/changes/agentic-loop-restart-safety/tasks.md:127` — `- [x] 3.2 Implement the operation-specific exact retained-response read before provider invocation. Reuse a validated`
- `openspec/changes/agentic-loop-restart-safety/tasks.md:133` — `- [x] 3.3 GREEN: prove by counter that a matching retained response invokes the provider zero times and typed absence`
- `openspec/changes/agentic-loop-restart-safety/specs/agentic-model/spec.md:3` — `### Requirement: Model request settlement is bound to a durable response`
- `openspec/changes/agentic-loop-restart-safety/specs/agentic-model/spec.md:24` — `#### Scenario: Response publication succeeds`
- `openspec/changes/agentic-loop-restart-safety/specs/agentic-model/spec.md:31` — `#### Scenario: Matching response already exists`
- `openspec/changes/agentic-loop-restart-safety/specs/agentic-model/spec.md:37` — `#### Scenario: No matching response exists`
- `openspec/changes/agentic-loop-restart-safety/specs/agentic-model/spec.md:43` — `#### Scenario: Retained response lookup fails`
- `openspec/changes/agentic-loop-restart-safety/specs/agentic-model/spec.md:48` — `#### Scenario: Response identity collides`
- `openspec/changes/agentic-loop-restart-safety/specs/agentic-model/spec.md:77` — `### Requirement: Started markers do not claim invocation certainty`
- `openspec/changes/agentic-loop-restart-safety/specs/agentic-model/spec.md:82` — `#### Scenario: Process stops after a started marker`
- `openspec/changes/agentic-loop-restart-safety/specs/agentic-model/spec.md:101` — `### Requirement: Model response publication is durably at-least-once`
- `openspec/changes/agentic-loop-restart-safety/specs/agentic-model/spec.md:112` — `#### Scenario: Response publication is uncertain`

## Spellings of the fact

- `processor/agentic-model/component.go:69` — `responseEvidence   retainedResponseEvidenceReader`
- `processor/agentic-model/provider_settlement.go:15` — `type retainedResponseEvidence struct {`
- `processor/agentic-model/provider_settlement.go:20` — `type retainedResponseEvidenceReader interface {`
- `processor/agentic-model/provider_settlement.go:21` — `ReadRetainedResponse(context.Context, string, string) (retainedResponseEvidence, bool, error)`
- `processor/agentic-model/provider_settlement.go:24` — `type natsRetainedResponseEvidenceReader struct {`
- `processor/agentic-model/provider_settlement.go:28` — `func (r natsRetainedResponseEvidenceReader) ReadRetainedResponse(`
- `processor/agentic-model/provider_settlement.go:33` — `stream, err := r.client.GetStream(ctx, streamName)`
- `processor/agentic-model/provider_settlement.go:37` — `raw, err := stream.GetLastMsgForSubject(ctx, subject)`
- `processor/agentic-model/provider_settlement.go:38` — `if errors.Is(err, jetstream.ErrMsgNotFound) {`
- `processor/agentic-model/provider_settlement.go:39` — `return retainedResponseEvidence{}, false, nil`
- `processor/agentic-model/provider_settlement.go:50` — `func responseAddress(ports []component.PortDefinition, requestID string) (string, string, error) {`
- `processor/agentic-model/provider_settlement.go:51` — `subject, err := component.ResolveSubject(ports, "agent.response", requestID)`
- `processor/agentic-model/provider_settlement.go:71` — `return subject, stream.Name(), nil`
- `processor/agentic-model/provider_settlement.go:76` — `func (c *Component) readRetainedAgentResponse(`
- `processor/agentic-model/provider_settlement.go:80` — `subject, streamName, err := responseAddress(c.outputPortDefs(), requestID)`
- `processor/agentic-model/provider_settlement.go:89` — `evidence, found, err := reader.ReadRetainedResponse(ctx, streamName, subject)`
- `processor/agentic-model/provider_settlement.go:94` — `if !found {`
- `processor/agentic-model/provider_settlement.go:107` — `response, ok := decoded.Payload().(*agentic.AgentResponse)`
- `processor/agentic-model/provider_settlement.go:113` — `if evidence.subject != subject || response.RequestID != requestID {`
- `processor/agentic-model/component.go:604` — `func (c *Component) handleRequest(ctx context.Context, data []byte) (natsclient.DeliveryDecision, error) {`
- `processor/agentic-model/component.go:616` — `_, found, err := c.readRetainedAgentResponse(ctx, req.RequestID)`
- `processor/agentic-model/component.go:619` — `return natsclient.DeliveryDecisionQuarantine, err`
- `processor/agentic-model/component.go:622` — `return natsclient.DeliveryDecisionRetry, err`
- `processor/agentic-model/component.go:628` — `return natsclient.DeliveryDecisionAck, nil`
- `processor/agentic-model/component.go:662` — `resp, err := c.executeRequest(ctx, client, req, endpoint, capability)`
- `processor/agentic-model/component.go:669` — `return natsclient.DeliveryDecisionRetry, publishErr`
- `processor/agentic-model/component.go:671` — `return natsclient.DeliveryDecisionAck, nil`
- `processor/agentic-model/component.go:676` — `return natsclient.DeliveryDecisionRetry, publishErr`
- `processor/agentic-model/component.go:678` — `return natsclient.DeliveryDecisionAck, nil`
- `processor/agentic-model/component.go:761` — `func (c *Component) handleModelError(ctx context.Context, req agentic.AgentRequest, resp agentic.AgentResponse, err error, duration float64) error {`
- `processor/agentic-model/component.go:780` — `publishErr := c.publishErrorResponseWithTokens(errorCtx, req.RequestID, errorMsg, resp.TokenUsage)`
- `processor/agentic-model/component.go:800` — `func (c *Component) handleModelSuccess(ctx context.Context, req agentic.AgentRequest, resp agentic.AgentResponse, duration float64) error {`
- `processor/agentic-model/component.go:821` — `if err := c.publishResponse(ctx, resp); err != nil {`
- `processor/agentic-model/component.go:1084` — `func (c *Component) publishResponse(ctx context.Context, resp agentic.AgentResponse) error {`
- `processor/agentic-model/component.go:1095` — `if err := c.natsClient.PublishToStream(ctx, subject, data); err != nil {`
- `processor/agentic-model/component.go:1114` — `func (c *Component) publishErrorResponse(ctx context.Context, requestID string, errMsg string) error {`
- `processor/agentic-model/component.go:1119` — `func (c *Component) publishErrorResponseWithTokens(ctx context.Context, requestID string, errMsg string, tokens agentic.TokenUsage) error {`
- `agentic/types.go:169` — `type AgentResponse struct {`
- `agentic/types.go:170` — `RequestID    string      `json:"request_id"``
- `agentic/types.go:180` — `func (r AgentResponse) Validate() error {`
- `natsclient/client.go:942` — `func (m *Client) PublishToStream(ctx context.Context, subject string, data []byte) error {`
- `natsclient/client.go:1005` — `_, err = js.PublishMsg(ctx, msg)`

## Adjacent claims

- `openspec/changes/agentic-loop-restart-safety/proposal.md:99` — `response receives PubAck before source ACK. This adds no provider ambiguity config, failure-kind wire field,`
- `openspec/changes/agentic-loop-restart-safety/proposal.md:100` — `reconciliation capability, endpoint census, ledger, outbox, or provider dependency on AGENT replay admission.`
- `openspec/changes/agentic-loop-restart-safety/specs/agentic-model/spec.md:79` — `Agentic-model SHALL NOT use a pre-call started marker as proof that a provider was invoked or as an exactly-once`
- `openspec/changes/agentic-loop-restart-safety/specs/agentic-model/spec.md:110` — `ambiguity policy, or replay-admission prerequisite is admitted.`
- #1146 — agentic-loop: prevent silent ACK and active-state loss across process restart
- #1147 — epic: make framework restart behavior explicit and provable
- #1155 — e2e(agentic): prove semantic-settlement quarantine and AgentRun redelivery across process replacement
- #759 — natsclient: establish semantic JetStream settlement as the restart-safety foundation
- PR #1159 — fix(agentic-loop): preserve durable work across process restart
- PR #1156 — refactor(natsclient): add semantic delivery settlement

## Consumers

- `processor/agentic-model/component.go:397` — `return c.handleRequest(workCtx, data)`
- `processor/agentic-model/component.go:415` — `result, admitted := consumeAdmittedDelivery(msgCtx, msg, policy, admission)`
- `processor/agentic-model/component.go:616` — `_, found, err := c.readRetainedAgentResponse(ctx, req.RequestID)`
- `processor/agentic-model/provider_settlement.go:80` — `subject, streamName, err := responseAddress(c.outputPortDefs(), requestID)`
- `processor/agentic-model/provider_settlement.go:89` — `evidence, found, err := reader.ReadRetainedResponse(ctx, streamName, subject)`
- `processor/agentic-model/component.go:639` — `publishErr := c.publishErrorResponse(ctx, req.RequestID, err.Error())`
- `processor/agentic-model/component.go:780` — `publishErr := c.publishErrorResponseWithTokens(errorCtx, req.RequestID, errorMsg, resp.TokenUsage)`
- `processor/agentic-model/component.go:821` — `if err := c.publishResponse(ctx, resp); err != nil {`
- `processor/agentic-model/component.go:1127` — `if err := c.publishResponse(ctx, resp); err != nil {`
- `processor/agentic-model/delivery_owner.go:69` — `result := natsclient.ConsumeDeliveryWithHeartbeat(ctx, msg, policy)`
- `natsclient/delivery_settlement.go:419` — `case DeliveryDecisionAck:`
- `natsclient/delivery_settlement.go:450` — `return msg.Ack()`

## Problem shape

- `processor/agentic-dispatch/task_recovery.go:34` — `type retainedTaskEvidenceReader interface {`
- `processor/agentic-dispatch/task_recovery.go:35` — `ReadRetainedTask(context.Context, string, string) ([]byte, bool, error)`
- `processor/agentic-dispatch/task_recovery.go:51` — `raw, err := stream.GetLastMsgForSubject(ctx, subject)`
- `processor/agentic-dispatch/task_recovery.go:52` — `if errors.Is(err, jetstream.ErrMsgNotFound) {`
- `processor/agentic-dispatch/task_recovery.go:156` — `raw, found, err := reader.ReadRetainedTask(ctx, streamName, subject)`
- `natsclient/delivery_settlement.go:370` — `return settleDeliveryDecision(msg, policy.retry, interpretDeliveryWork(joined))`
- `natsclient/delivery_settlement.go:412` — `func settleDeliveryDecision(msg jetstream.Msg, retry DeliveryRetryPolicy, result DeliveryResult) DeliveryResult {`

## Verification proofs

- `processor/agentic-model/provider_settlement_test.go:59` — `// spec: agentic-model / Model request settlement is bound to a durable response`
- `processor/agentic-model/provider_settlement_test.go:60` — `func TestMatchingRetainedResponseAcknowledgesWithoutProviderWork(t *testing.T) {`
- `processor/agentic-model/provider_settlement_test.go:79` — `require.Equal(t, natsclient.DeliveryDecisionAck, decision)`
- `processor/agentic-model/provider_settlement_test.go:84` — `func TestRetainedResponseCorrelationConflictQuarantinesBeforeProviderWork(t *testing.T) {`
- `processor/agentic-model/provider_settlement_test.go:125` — `require.Equal(t, natsclient.DeliveryDecisionQuarantine, decision)`
- `processor/agentic-model/provider_settlement_test.go:132` — `func TestRetainedResponseLookupFailureRetriesBeforeProviderWork(t *testing.T) {`
- `processor/agentic-model/provider_settlement_test.go:142` — `require.Equal(t, natsclient.DeliveryDecisionRetry, decision)`
- `processor/agentic-model/provider_settlement_test.go:147` — `func TestTypedRetainedResponseAbsencePermitsProviderPath(t *testing.T) {`
- `processor/agentic-model/provider_settlement_integration_test.go:155` — `type blockingRetainedResponseReader struct {`
- `processor/agentic-model/provider_settlement_integration_test.go:160` — `func (r blockingRetainedResponseReader) ReadRetainedResponse(`
- `processor/agentic-model/provider_settlement_integration_test.go:167` — `return retainedResponseEvidence{}, false, errors.New("process replaced before retained lookup completed")`
- `processor/agentic-model/provider_settlement_integration_test.go:171` — `func TestIntegrationMatchingRetainedResponseSkipsProviderAndAcknowledgesSource(t *testing.T) {`
- `processor/agentic-model/provider_settlement_integration_test.go:193` — `require.Zero(t, calls.Load())`
- `processor/agentic-model/provider_settlement_integration_test.go:197` — `func TestIntegrationTypedAbsenceInvokesProviderAndPubAckPrecedesSourceAck(t *testing.T) {`
- `processor/agentic-model/provider_settlement_integration_test.go:211` — `raw, readErr := responseStream.GetLastMsgForSubject(t.Context(), "agent.response."+req.RequestID)`
- `processor/agentic-model/provider_settlement_integration_test.go:215` — `require.Equal(t, int32(1), calls.Load())`
- `processor/agentic-model/provider_settlement_integration_test.go:219` — `func TestIntegrationProviderErrorPubAckPrecedesSourceAck(t *testing.T) {`
- `processor/agentic-model/provider_settlement_integration_test.go:253` — `require.Equal(t, agentic.StatusError, response.Status)`
- `processor/agentic-model/provider_settlement_integration_test.go:260` — `func TestIntegrationRetainedResponseRequestIDConflictQuarantinesWithoutProvider(t *testing.T) {`
- `processor/agentic-model/provider_settlement_integration_test.go:283` — `require.Zero(t, calls.Load())`
- `processor/agentic-model/provider_settlement_integration_test.go:287` — `func TestIntegrationRetainedResponseLookupFailureRetriesWithoutProvider(t *testing.T) {`
- `processor/agentic-model/provider_settlement_integration_test.go:298` — `require.NoError(t, js.DeleteStream(t.Context(), providerSettlementResponseStream))`
- `processor/agentic-model/provider_settlement_integration_test.go:305` — `require.Zero(t, calls.Load())`
- `processor/agentic-model/provider_settlement_integration_test.go:308` — `// spec: agentic-model / Model request settlement is bound to a durable response`
- `processor/agentic-model/provider_settlement_integration_test.go:309` — `// spec: agentic-model / Started markers do not claim invocation certainty`
- `processor/agentic-model/provider_settlement_integration_test.go:310` — `func TestIntegrationPreProviderReplacementSeesAbsenceAndInvokesOnce(t *testing.T) {`
- `processor/agentic-model/provider_settlement_integration_test.go:352` — `require.Zero(t, calls.Load())`
- `processor/agentic-model/provider_settlement_integration_test.go:364` — `raw, readErr := responseStream.GetLastMsgForSubject(t.Context(), "agent.response."+req.RequestID)`
- `processor/agentic-model/provider_settlement_integration_test.go:378` — `require.Equal(t, int32(1), calls.Load())`
- `processor/agentic-model/provider_settlement_integration_test.go:381` — `// spec: agentic-model / Model response publication is durably at-least-once`
- `processor/agentic-model/provider_settlement_integration_test.go:382` — `// spec: agentic-model / Started markers do not claim invocation certainty`
- `processor/agentic-model/provider_settlement_integration_test.go:383` — `func TestIntegrationPostProviderPrePubAckReplacementMayInvokeAgain(t *testing.T) {`
- `processor/agentic-model/provider_settlement_integration_test.go:419` — `require.NoError(t, js.DeleteStream(t.Context(), providerSettlementResponseStream))`
- `processor/agentic-model/provider_settlement_integration_test.go:436` — `_, err = js.CreateStream(t.Context(), jetstream.StreamConfig{`
- `processor/agentic-model/provider_settlement_integration_test.go:457` — `require.Equal(t, int32(2), calls.Load())`

## Forbidden mechanisms

(none — see Searches)

## Searches

- `git grep -n -E '^## (Purpose|Product Boundary)|^# ' -- openspec/project.md` → 3
- `gopls workspace_symbol -matcher=fuzzy <term>` for `handleRequest`, `readRetainedAgentResponse`, `retainedResponseEvidenceReader`, `responseAddress`, `publishResponse`, `handleModelSuccess`, and `handleModelError` → 0 each; all seven failed during initial workspace load with `package unsafe is not in std (/usr/local/go/src/unsafe)`
- `git grep -n --untracked -E <term> -- processor/agentic-model openspec/changes/agentic-loop-restart-safety` → `retainedResponseEvidenceReader` 8; `ReadRetainedResponse` 11; `GetLastMsgForSubject` 26; `publishResponse` 30; `DeliveryAck|DeliveryRetry|DeliveryQuarantine|DeliveryTerminate` 7; `RequestID` 299; `PostProviderReturnBeforeResponsePubAck` 2; `pre-call|post-return|replacement|retained` 339; `fingerprint|content comparison|ambiguity|commit-unknown|reconciler|started marker|outbox|ledger|supervisor|replay admission|horizon` 174
- `git grep -n --untracked -E 'handleRequest(' -- processor/agentic-model openspec/changes/agentic-loop-restart-safety` → command error, unbalanced parenthesis
- `git grep -n --untracked -E 'Completion(ctx' -- processor/agentic-model openspec/changes/agentic-loop-restart-safety` → command error, unbalanced parenthesis
- `git grep -n --untracked -F 'handleRequest(' -- processor/agentic-model` → 5
- `git grep -n --untracked -F 'Completion(ctx' -- processor/agentic-model` → 50
- `git grep -n --untracked -F 'Delivery' -- processor/agentic-model/component.go processor/agentic-model/provider_settlement.go processor/agentic-model/provider_settlement_test.go processor/agentic-model/provider_settlement_integration_test.go` → 18
- `git grep -n -E '^### Requirement: (Model request settlement is bound to a durable response|Model response publication is durably at-least-once|Started markers do not claim invocation certainty)|^#### Scenario: (Matching retained response skips provider work|Typed retained-response absence permits provider invocation|Retained-response lookup failure retries without provider work|Retained response correlation conflict quarantines|Provider returns before response PubAck and process replacement|Provider error response is committed before source ACK)' -- openspec/changes/agentic-loop-restart-safety/specs/agentic-model/spec.md` → 3
- `git grep -n -E '^  (3\.1|3\.2|3\.3)|^- \[[ x]\] 3\.' -- openspec/changes/agentic-loop-restart-safety/tasks.md` → 3
- `git grep -n -F <scenario> -- openspec/changes/agentic-loop-restart-safety/specs/agentic-model/spec.md` for `Response publication succeeds`, `Matching response already exists`, `No matching response exists`, `Retained response lookup fails`, `Response identity collides`, `Process stops after a started marker`, and `Response publication is uncertain` → 1 each
- `git grep -n --untracked -F <term> -- processor/agentic-model agentic` → `readRetainedAgentResponse` 9; `responseAddress(` 2; `executeRequest(` 2; `publishErrorResponse(` 2; `publishErrorResponseWithTokens(` 3; `AgentResponse struct` 1; `func (r *AgentResponse) Validate` 0; `RequestID string` 3
- `git grep -n --untracked -E 'func \([^)]*AgentResponse[^)]*\) Validate|RequestID[[:space:]]+string' -- agentic/types.go processor/agentic-model` → 4
- `git grep -n -E '^func \(c \*Client\) (PublishToStream|PublishToStreamWithAck|GetStream)' -- natsclient` → 1
- `git grep -n -E 'PublishMsg\(|return.*PubAck|PublishToStreamWithAck' -- natsclient/client.go` → 3
- `git grep -n -F 'PublishToStream(ctx' -- natsclient` → 15
- `git grep -n -F 'func (c *Client) PublishToStream' -- natsclient` → 1
- `git grep -n -E 'func settleDeliveryDecision|case DeliveryDecisionAck|msg\.Ack\(|case DeliveryDecisionRetry|case DeliveryDecisionQuarantine' -- natsclient/delivery_settlement.go` → 6
- `git grep -n -F 'func consumeAdmittedDelivery' -- processor/agentic-model` → 1
- `git grep -n -F 'policy.Handle' -- processor/agentic-model natsclient` → 0
- `git grep -n -F 'settleDeliveryDecision' -- natsclient` → 4
- `git grep -n --untracked -E '^func Test(Integration)?(MatchingRetainedResponse|RetainedResponse|TypedRetainedResponse|IntegrationTypedAbsence|IntegrationProviderError|IntegrationRetained|IntegrationPreProvider|IntegrationPostProvider)' -- processor/agentic-model/provider_settlement*_test.go` → 11
- `git grep -n --untracked -F '// spec: agentic-model /' -- processor/agentic-model/provider_settlement*_test.go` → 13
- `git grep -n --untracked -i -F <term> -- 'processor/agentic-model/*.go' ':(exclude)processor/agentic-model/*_test.go'` for `fingerprint`, `ambiguity`, `commit_unknown`, `commit-unknown`, `reconciler`, `reconciliation`, `endpoint census`, `started marker`, `started_marker`, `ledger`, `outbox`, `supervisor`, `replay admission`, `replay_admission`, and `horizon` → 0 each
- `openspec list` → 2 active changes
- `gh issue list --search 'repo:C360Studio/semstreams provider settlement' --state open --json number,title` → 5
- `gh issue list --search 'repo:C360Studio/semstreams 1146 in:title,body' --state open --json number,title` → 7
- `gh pr list --state open --draft --json number,title,body` → 5
- `git grep -n -E 'type retainedTaskEvidenceReader|ReadRetainedTask|GetLastMsgForSubject|func \(c \*Component\) loadCompletedOutcome|func \(c \*Component\) publishResultWithMsgID' -- processor/agentic-dispatch/task_recovery.go processor/agentic-tools` → 9
- `git grep -n --untracked -E '^func \(c \*Component\) (handleModelError|handleModelSuccess|publishResponse|publishErrorResponse|handleRequest)|return natsclient\.DeliveryDecision(Ack|Retry|Quarantine|Terminate)' -- processor/agentic-model/component.go` → 15
- `git grep -n --untracked -E 'require\.(Zero|Equal|NoError|Error|True|False|Less)|calls\.Load\(\)|DeleteStream|CreateStream|drainIssued|GetLastMsgForSubject' -- processor/agentic-model/provider_settlement_test.go processor/agentic-model/provider_settlement_integration_test.go` → 74
- Repeated count-only `git grep -n --untracked -E <term> -- processor/agentic-model openspec/changes/agentic-loop-restart-safety` searches → `retainedResponseEvidenceReader` 8; `ReadRetainedResponse` 11; `GetLastMsgForSubject` 26; `handleRequest(` command error; `Completion(ctx` command error; `publishResponse` 30; `DeliveryAck|DeliveryRetry|DeliveryQuarantine|DeliveryTerminate` 7; `RequestID` 299; `PostProviderReturnBeforeResponsePubAck` 2; `pre-call|post-return|replacement|retained` 339; forbidden-mechanism alternation 174
- `git grep -n --untracked -E '^func TestIntegration.*Replacement|pre-call|pre-provider|replacement' -- processor/agentic-model/provider_settlement_integration_test.go` → 17
- `git grep -n --untracked -E 'type blockingRetainedResponseReader|func \(r blockingRetainedResponseReader\) ReadRetainedResponse|return retainedResponseEvidence\{\}, false, nil|TestIntegrationPreProviderReplacement' -- processor/agentic-model/provider_settlement_integration_test.go` → 3
- `git grep -n --untracked -F 'requireProviderSourceUnacked(' -- processor/agentic-model/provider_settlement_integration_test.go` → 5
- `git grep -n --untracked -F 'requireProviderSourceAck(' -- processor/agentic-model/provider_settlement_integration_test.go` → 6
- `git grep -n --untracked -F 'return retainedResponseEvidence{}, false, errors.New("process replaced before retained lookup completed")' -- processor/agentic-model/provider_settlement_integration_test.go` → 1

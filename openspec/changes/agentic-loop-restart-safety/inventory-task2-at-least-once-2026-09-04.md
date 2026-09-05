# Task 2.5–2.6 At-Least-Once Publication Inventory

## Task 2.5–2.6 current-worktree refresh (2026-09-04)

base: 3d6cab9f023cee960744b740459ef6a8819ca1ca

### Claimed gap

- `openspec/changes/agentic-loop-restart-safety/tasks.md:103` — `- [ ] 2.5 RED/GREEN: prove ordinary task/control, created, request, response, approval, terminal, governance, and`
- `openspec/changes/agentic-loop-restart-safety/tasks.md:104` — `result publications are at-least-once, source ACK waits for required PubAck, and uncertain PubAck may republish.`
- `openspec/changes/agentic-loop-restart-safety/tasks.md:113` — `- [ ] 2.6 Remove general exact committed-output lookup and canonical-output fingerprint work. Retain exact reads only`
- `openspec/changes/agentic-loop-restart-safety/design.md:142` — `- Ordinary created, request, approval, continuation, terminal, validated, verdict, ToolResult, and user-response`
- `openspec/changes/agentic-loop-restart-safety/design.md:147` — `proof. There is no general exact-output layer or canonical-output fingerprint system.`
- `openspec/changes/agentic-loop-restart-safety/specs/agentic-dispatch/spec.md:142` — `NOT add exact committed-output lookup for ordinary publications.`
- `openspec/changes/agentic-loop-restart-safety/specs/agentic-model/spec.md:211` — `retention SHALL remain unknown. No general stream scan or exact-read requirement for ordinary response publication`
- `openspec/changes/agentic-loop-restart-safety/specs/agentic-governance/spec.md:82` — `proposals require no exact committed-output lookup. Conflicting proposal or verdict correlation SHALL quarantine;`
- `openspec/changes/agentic-loop-restart-safety/specs/agentic-tools/spec.md:57` — `no second exact output lookup, general stream scan, or second tool authority.`

### Ordinary production publication sites

- `processor/agentic-dispatch/commands.go:179` — `if err := c.natsClient.PublishToStream(ctx, subject, signalData); err != nil {`
- `processor/agentic-dispatch/component.go:1058` — `if err := c.natsClient.PublishToStream(ctx, prepared.subject, prepared.data); err != nil {`
- `processor/agentic-dispatch/component.go:1206` — `if err := c.natsClient.PublishToStream(ctx, subject, data); err != nil {`
- `processor/agentic-dispatch/http.go:375` — `if err := c.natsClient.PublishToStream(ctx, prepared.subject, prepared.data); err != nil {`
- `processor/agentic-dispatch/http.go:857` — `if err := c.natsClient.PublishToStream(ctx, subject, data); err != nil {`
- `processor/agentic-dispatch/terminal_settlement.go:173` — `if err := c.natsClient.PublishToStreamWithMsgID(ctx, subject, data, msgID); err != nil {`
- `processor/agentic-model/component.go:1069` — `if err := c.natsClient.PublishToStream(ctx, subject, data); err != nil {`
- `processor/agentic-loop/approval_sweeper.go:151` — `if err := c.natsClient.PublishToStream(ctx, subject, data); err != nil {`
- `processor/agentic-loop/component.go:1621` — `if pubErr := c.natsClient.PublishToStream(errorCtx, msg.Subject, msg.Data); pubErr != nil {`
- `processor/agentic-loop/component.go:1957` — `if err := c.natsClient.PublishToStream(ctx, msg.Subject, msg.Data); err != nil {`
- `processor/agentic-loop/component.go:1988` — `if err := c.natsClient.PublishToStream(ctx, subject, data); err != nil {`
- `processor/agentic-loop/component.go:2263` — `if err := c.natsClient.PublishToStream(ctx, subject, completionData); err != nil {`
- `processor/agentic-loop/governance_dispatcher.go:607` — `if err := publisher.PublishToStream(ctx, subject, data); err != nil {`
- `processor/agentic-governance/component.go:444` — `if err := c.natsClient.PublishToStream(ctx, outputSubject, outputData); err != nil {`
- `processor/agentic-tools/component.go:1203` — `if err := c.publishStream(ctx, subject, data, msgID); err != nil {`

### Current uncommitted production and test pins

- `processor/agentic-dispatch/commands.go:179` — `if err := c.natsClient.PublishToStream(ctx, subject, signalData); err != nil {`
- `processor/agentic-dispatch/component.go:881` — `if errs.IsFatal(err) || errs.IsTransient(err) {`
- `processor/agentic-dispatch/loop_signal_integration_test.go:135` — `// spec: agentic-dispatch / Every dispatch durable input settles through its owner`
- `processor/agentic-dispatch/loop_signal_integration_test.go:136` — `func TestIntegrationCancelRequiresJetStreamPubAck(t *testing.T) {`
- `processor/agentic-dispatch/loop_signal_integration_test.go:147` — `func TestIntegrationCancelPublishFailureCannotBecomeSuccessfulCommand(t *testing.T) {`
- `processor/agentic-dispatch/loop_signal_integration_test.go:161` — `"a published error response cannot turn missing cancel PubAck into source ACK")`
- `processor/agentic-dispatch/restart_identity_integration_test.go:229` — `"current replay publishes a second task after the duplicate window")`
- `processor/agentic-dispatch/terminal_settlement_integration_test.go:240` — `}, 3*time.Second, 25*time.Millisecond, "source ACK must follow successful synchronous response PubAck")`
- `processor/agentic-model/publication_semantics_integration_test.go:15` — `// spec: agentic-model / Model response publication is durably at-least-once`
- `processor/agentic-model/publication_semantics_integration_test.go:16` — `func TestIntegrationModelResponsePublicationMayRepeat(t *testing.T) {`
- `processor/agentic-model/publication_semantics_integration_test.go:28` — `require.NoError(t, c.publishResponse(ctx, response))`
- `processor/agentic-model/publication_semantics_integration_test.go:29` — `require.NoError(t, c.publishResponse(ctx, response))`
- `processor/agentic-model/publication_semantics_integration_test.go:47` — `require.Equal(t, 2, count, "ordinary response publication may repeat with the same RequestID")`
- `processor/agentic-loop/publication_semantics_integration_test.go:15` — `// spec: agentic-loop / Loop task, request, and tool work use only required correlation`
- `processor/agentic-loop/publication_semantics_integration_test.go:16` — `func TestIntegrationOrdinaryLoopPublicationsMayRepeat(t *testing.T) {`
- `processor/agentic-loop/publication_semantics_integration_test.go:23` — `{Subject: "agent.created.at-least-once-created", Data: []byte(`{"kind":"created"}`)},`
- `processor/agentic-loop/publication_semantics_integration_test.go:24` — `{Subject: "agent.request.at-least-once-request", Data: []byte(`{"kind":"request"}`)},`
- `processor/agentic-loop/publication_semantics_integration_test.go:25` — `{Subject: "agent.approval_pending.at-least-once-approval", Data: []byte(`{"kind":"approval"}`)},`
- `processor/agentic-loop/publication_semantics_integration_test.go:26` — `{Subject: "agent.request.at-least-once-continuation", Data: []byte(`{"kind":"continuation"}`)},`
- `processor/agentic-loop/publication_semantics_integration_test.go:27` — `{Subject: "agent.complete.at-least-once-terminal", Data: []byte(`{"kind":"terminal"}`)},`
- `processor/agentic-loop/publication_semantics_integration_test.go:30` — `require.NoError(t, c.publishResults(ctx, HandlerResult{PublishedMessages: messages}))`
- `processor/agentic-loop/publication_semantics_integration_test.go:31` — `require.NoError(t, c.publishResults(ctx, HandlerResult{PublishedMessages: messages}))`
- `processor/agentic-loop/publication_semantics_integration_test.go:50` — `require.Equal(t, 2, count, "%s may repeat after publication uncertainty", published.Subject)`
- `processor/agentic-governance/delivery_settlement_integration_test.go:24` — `// spec: agentic-governance / Governance publications are durably at-least-once`
- `processor/agentic-governance/delivery_settlement_integration_test.go:25` — `func TestIntegrationGovernanceProductionCallbacksPublishBeforeAck(t *testing.T) {`
- `processor/agentic-governance/delivery_settlement_integration_test.go:64` — `for attempt := range 2 {`
- `processor/agentic-tools/outcomes_integration_test.go:243` — `// spec: agentic-tools / Tool-result publication is durably at-least-once`
- `processor/agentic-tools/outcomes_integration_test.go:244` — `func TestIntegrationResultPublishFailureRestartReplaysStoredOutcome(t *testing.T) {`
- `processor/agentic-tools/outcomes_test.go:243` — `// spec: agentic-tools / Tool-result publication is durably at-least-once`
- `processor/agentic-tools/outcomes_test.go:244` — `func TestHandleToolCallPublishFailureReplaysWithoutExecutor(t *testing.T) {`
- `natsclient/publish_msgid_integration_test.go:22` — `const duplicateWindow = 250 * time.Millisecond`
- `natsclient/publish_msgid_integration_test.go:66` — `"the same Nats-Msg-Id must store again after the configured window")`

### Exact committed-output and canonical-output-fingerprint reads in production

- `processor/agentic-dispatch/task_recovery.go:51` — `raw, err := stream.GetLastMsgForSubject(ctx, subject)`
- `processor/agentic-dispatch/task_recovery.go:156` — `raw, found, err := reader.ReadRetainedTask(ctx, streamName, subject)`
- `processor/agentic-dispatch/terminal_settlement.go:106` — `entry, err := kv.Get(ctx, loopID)`
- `processor/agentic-loop/component.go:2387` — `if v, ok := data["proposal_fingerprint"].(string); ok {`
- `processor/agentic-loop/governance_dispatcher.go:586` — `fingerprint, err := fingerprintProposedToolCall(payload)`
- `processor/agentic-loop/governance_dispatcher.go:613` — `func fingerprintProposedToolCall(payload ProposedToolCallPayload) (string, error) {`
- `processor/agentic-tools/component.go:710` — `if outcome, found, err := c.loadCompletedOutcome(ctx, call, storeOperationGet); err != nil {`
- `processor/agentic-tools/component.go:805` — `data, err := c.outcomes.Get(ctx, toolCallOutcomeKey(call.ExecutionID))`
- `processor/agentic-tools/component.go:815` — `outcome, err := decodeCompletedOutcome(data, call)`
- `processor/agentic-tools/component.go:860` — `winner, found, readErr := c.loadCompletedOutcome(ctx, call, storeOperationReadWinner)`
- `processor/agentic-tools/outcomes.go:31` — `Fingerprint string             `json:"fingerprint"``
- `processor/agentic-tools/outcomes.go:94` — `func toolCallFingerprintV1(call agentic.ToolCall) (string, error) {`
- `processor/agentic-tools/outcomes.go:170` — `wantFingerprint, err := toolCallFingerprintV1(call)`
- `processor/agentic-tools/outcomes.go:189` — `if outcome.Fingerprint != wantFingerprint {`

General committed-output stream-read symbols in agentic-model, agentic-loop, agentic-governance, and agentic-tools:

(none — see Searches)

Canonical-output and output-fingerprint spellings in the five production packages:

(none — see Searches)

### Consumers

- `processor/agentic-model/component.go:796` — `if err := c.publishResponse(ctx, resp); err != nil {`
- `processor/agentic-model/component.go:1101` — `if err := c.publishResponse(ctx, resp); err != nil {`
- `processor/agentic-loop/approval_sweeper.go:94` — `c.publishResults(ctx, result)`
- `processor/agentic-loop/component.go:1354` — `c.publishResults(ctx, result)`
- `processor/agentic-loop/component.go:1742` — `if err := c.publishResults(ctx, result); err != nil {`
- `processor/agentic-tools/component.go:750` — `err := c.publishResultWithMsgID(ctx, result, toolApprovalRequiredMessageID(call.ExecutionID))`
- `processor/agentic-tools/component.go:1184` — `return c.publishResultWithMsgID(ctx, result, toolResultMessageID(result.ExecutionID))`
- `processor/agentic-tools/outcomes.go:122` — `fingerprint, err := toolCallFingerprintV1(call)`
- `processor/agentic-tools/outcomes.go:170` — `wantFingerprint, err := toolCallFingerprintV1(call)`

### Problem shape

- `natsclient/publish_msgid_integration_test.go:54` — `"same Nats-Msg-Id within the window must dedup to one stored message")`
- `natsclient/publish_msgid_integration_test.go:66` — `"the same Nats-Msg-Id must store again after the configured window")`
- `processor/agentic-tools/component.go:805` — `data, err := c.outcomes.Get(ctx, toolCallOutcomeKey(call.ExecutionID))`
- `processor/agentic-tools/component.go:852` — `err = c.outcomes.Create(ctx, toolCallOutcomeKey(call.ExecutionID), data)`
- `processor/agentic-tools/outcomes_test.go:272` — `assert.Equal(t, int32(2), publishes.Load())`

## Searches

### Task 2.5–2.6 refresh searches (2026-09-04)

- `git rev-parse HEAD` → 1 (`3d6cab9f023cee960744b740459ef6a8819ca1ca`)
- `git status --short -- openspec/changes/agentic-loop-restart-safety processor/agentic-dispatch processor/agentic-model processor/agentic-loop processor/agentic-governance processor/agentic-tools agentic natsclient message` → 8 paths
- `git status --short -- natsclient/publish_msgid_integration_test.go processor/agentic-dispatch processor/agentic-model processor/agentic-loop processor/agentic-governance processor/agentic-tools openspec/changes/agentic-loop-restart-safety/inventory-task2-stable-identity-2026-09-03.md` → 9 paths
- `git diff --unified=0 -- natsclient/publish_msgid_integration_test.go processor/agentic-dispatch/commands.go processor/agentic-dispatch/loop_signal_integration_test.go processor/agentic-governance/delivery_settlement_integration_test.go processor/agentic-tools/outcomes_integration_test.go processor/agentic-tools/outcomes_test.go` → 6 paths
- `git grep -n -E '^# (Purpose|Product Boundary)|^## (Purpose|Product Boundary)' -- openspec/project.md` → 2
- `git grep -n -E '2\\.5|2\\.6|ordinary|canonical output|committed-output|fingerprint|exact' -- openspec/changes/agentic-loop-restart-safety/tasks.md openspec/changes/agentic-loop-restart-safety/design.md openspec/changes/agentic-loop-restart-safety/specs/agentic-dispatch/spec.md openspec/changes/agentic-loop-restart-safety/specs/agentic-model/spec.md openspec/changes/agentic-loop-restart-safety/specs/agentic-loop/spec.md openspec/changes/agentic-loop-restart-safety/specs/agentic-governance/spec.md openspec/changes/agentic-loop-restart-safety/specs/agentic-tools/spec.md` → 258
- `git grep -n -E 'Publish(ToStream|ToStreamWithMsgID|ToStreamWithAck|Msg)?\\(' -- processor/agentic-dispatch processor/agentic-model processor/agentic-loop processor/agentic-governance processor/agentic-tools ':!*_test.go'` → 18
- `git grep -n -E 'publish(Result|Response|Results|Failure|Proposed|Approval)|PublishToStreamWithMsgID|toolResultMessageID|terminalResponseIDPrefix' -- processor/agentic-dispatch processor/agentic-model processor/agentic-loop processor/agentic-governance processor/agentic-tools ':!*_test.go'` → 48
- `git grep -n -E 'GetLastMsgForSubject|GetMsg\\(|DirectGet|GetMessage\\(|ReadRetained|readRetained|exact.*(output|response|request|verdict|task)|fingerprint|Fingerprint|canonical.*output|committed.*output' -- processor/agentic-dispatch processor/agentic-model processor/agentic-loop processor/agentic-governance processor/agentic-tools ':!*_test.go'` → 25
- `git grep -n -E '\\.(Get|Create)\\(ctx,|outcomes\\.|GetStream\\(|GetKeyValue\\(|KeyValue\\(' -- processor/agentic-dispatch processor/agentic-model processor/agentic-loop processor/agentic-governance processor/agentic-tools ':!*_test.go'` → 29
- `git grep -n -E '// spec: agentic-(dispatch|model|loop|governance|tools) / (Every dispatch durable input settles through its owner|Dispatch task redelivery recovers the committed LoopID|Model response publication is durably at-least-once|Loop task, request, and tool work use only required correlation|Governance publications are durably at-least-once|Tool-result publication is durably at-least-once)|at-least-once|PubAck|may repeat|duplicate window|Nats-Msg-Id' -- processor/agentic-dispatch '*_test.go' processor/agentic-model '*_test.go' processor/agentic-loop '*_test.go' processor/agentic-governance '*_test.go' processor/agentic-tools '*_test.go' natsclient/publish_msgid_integration_test.go` → 52
- `git grep -n -E '^func Test.*(Publish|Publication|PubAck|Redeliver|Repeat|Dedup|Outcome|Retained|Recovery|Restart|Settlement)|// spec: agentic-' -- processor/agentic-dispatch processor/agentic-model processor/agentic-loop processor/agentic-governance processor/agentic-tools ':!*README.md' ':!*doc.go' ':!*component.go' ':!*commands.go' ':!*config.go' ':!*outcomes.go' ':!*task_recovery.go' ':!*terminal_settlement.go' ':!*governance_dispatcher.go' ':!*approval_sweeper.go' ':!*violation.go'` → 121
- `git grep -n -E 'ResolveSubject\\([^\\n]*(agent\\.(task|signal|approval_response|request|response|created|complete|failed|approval_pending)|tool\\.(execute|result))|Name: "(agent\\.(task|signal|approval_response|request|response|created|complete|failed|approval_pending)|tool\\.(execute|result)|agent\\.task\\.validated|agent\\.request\\.validated|agent\\.response\\.validated)"|Subjects: \\[\\]string\\{"agent\\.(task|signal|approval_response|request|response|created|complete|failed|approval_pending|task\\.validated|request\\.validated|response\\.validated)|Subject: "agent\\.(created|request|approval_pending|task|complete)' -- processor/agentic-dispatch processor/agentic-model processor/agentic-loop processor/agentic-governance processor/agentic-tools ':!*_test.go'` → 34
- `git grep -n -E 'user\\.response|agent\\.approval_response|agent\\.signal|agent\\.task|agent\\.created|agent\\.complete|agent\\.failed|agent\\.approval_pending|agent\\.request|agent\\.response|tool\\.execute|tool\\.result|agent\\.(task|request|response)\\.validated' -- processor/agentic-dispatch processor/agentic-model processor/agentic-loop processor/agentic-governance processor/agentic-tools ':!*_test.go' ':!*README.md' ':!*doc.go'` → 95
- `git grep -n -i -E 'canonical[_ -]?output|output[_ -]?fingerprint|committed[_ -]?output|read.*committed|lookup.*committed|reconcil.*output' -- processor/agentic-dispatch processor/agentic-model processor/agentic-loop processor/agentic-governance processor/agentic-tools ':!*_test.go'` → 3; canonical/output-fingerprint spellings → 0
- `git grep -n -E 'GetLastMsgForSubject|GetMsg\\(|DirectGet|GetMessage\\(' -- processor/agentic-model processor/agentic-loop processor/agentic-governance processor/agentic-tools ':!*_test.go'` → 0
- `git grep -n -E 'func .*([Ff]ingerprint|Canonical)|[Ff]ingerprint\\(|Fingerprint[[:space:]]+string|fingerprint[[:space:]]+string' -- processor/agentic-dispatch processor/agentic-model processor/agentic-loop processor/agentic-governance processor/agentic-tools ':!*_test.go'` → 5
- `git grep -n -E '^func Test.*(Task|Cancel|Approval|Terminal|UserResponse|Response|Created|Request|Complete|Failed|Governance|Result).*(Publish|PubAck|Repeat|Redeliver|Retry|Settlement|Dedup)|PublishToStream|publishResults|publishResponse|publishResult|handleCancelCommand' -- processor/agentic-dispatch/*_test.go processor/agentic-model/*_test.go processor/agentic-loop/*_test.go processor/agentic-governance/*_test.go processor/agentic-tools/*_test.go` → 62
- `git grep -n -E '^### Requirement: (Every dispatch durable input settles through its owner|Dispatch task redelivery recovers the committed LoopID|Model response publication is durably at-least-once|Loop task, request, and tool work use only required correlation|Governance publications are durably at-least-once|Tool-result publication is durably at-least-once)|ordinary publications|general exact-output|canonical-output fingerprint|No general stream scan|no second exact output lookup|no exact committed-output lookup' -- openspec/changes/agentic-loop-restart-safety/specs openspec/changes/agentic-loop-restart-safety/design.md` → 17
- `git grep -n -E 'handleCommand|handleCancelCommand|CancelPublishFailureCannotBecomeSuccessfulCommand|CancelRequiresJetStreamPubAck|command handler|error response' -- processor/agentic-dispatch/component.go processor/agentic-dispatch/loop_signal_integration_test.go` → 8
- `git grep -n -E 'ProposalFingerprint|Fingerprint !=|Fingerprint ==|proposal_fingerprint|toolCallFingerprintV1|decodeCompletedOutcome|loadCompletedOutcome|ReadRetainedTask|GetLastMsgForSubject|kv.Get\\(ctx, loopID\\)' -- processor/agentic-dispatch processor/agentic-model processor/agentic-loop processor/agentic-governance processor/agentic-tools ':!*_test.go'` → 20
- `git grep -n -E 'publishStream\\(|PublishToStream\\(|PublishToStreamWithMsgID\\(' -- processor/agentic-dispatch processor/agentic-model processor/agentic-loop processor/agentic-governance processor/agentic-tools ':!*_test.go'` → 18
- `git grep -n -E 'TestIntegrationCancelRequiresJetStreamPubAck|TestIntegrationCancelPublishFailureCannotBecomeSuccessfulCommand|TestIntegrationGovernanceProductionCallbacksPublishBeforeAck|TestIntegrationResultPublishFailureRestartReplaysStoredOutcome|TestHandleToolCallPublishFailureReplaysWithoutExecutor|same Nats-Msg-Id must store again after|for attempt := range 2|current replay publishes a second task after the duplicate window|source ACK must follow successful synchronous response PubAck' -- natsclient/publish_msgid_integration_test.go processor/agentic-dispatch processor/agentic-governance processor/agentic-tools` → 9
- `git grep -n -E 'errs\\.IsFatal\\(err\\) \\|\\| errs\\.IsTransient\\(err\\)|PublishToStream\\(ctx, subject, signalData\\)' -- processor/agentic-dispatch/component.go processor/agentic-dispatch/commands.go` → 3
- `gopls workspace_symbol -matcher=fuzzy PublishToStream` → sandbox load failed; escalated rerun → 25
- `gopls references natsclient/client.go:942:18` → 35
- `gopls workspace_symbol -matcher=fuzzy ReadRetainedTask` → 14
- `gopls workspace_symbol -matcher=fuzzy toolCallFingerprintV1` → 1
- `gopls workspace_symbol -matcher=fuzzy publishResponse` → 12
- `gopls workspace_symbol -matcher=fuzzy publishResults` → 6
- `gopls workspace_symbol -matcher=fuzzy TestIntegrationOrdinaryLoopPublicationsMayRepeat` → 0
- `gopls references processor/agentic-loop/governance_dispatcher.go:613:6` → 1
- `gopls references processor/agentic-tools/outcomes.go:94:6` → 5
- `gopls call_hierarchy processor/agentic-model/component.go:1058:21` → 2 callers, 9 callees
- `gopls call_hierarchy processor/agentic-loop/component.go:1951:21` → 3 callers, 4 callees
- `gopls call_hierarchy processor/agentic-tools/component.go:1187:21` → 2 callers, 10 callees
- `sed -n '100,120p' openspec/changes/agentic-loop-restart-safety/tasks.md` → 21 lines
- `sed -n '1,260p' processor/agentic-model/publication_semantics_integration_test.go` → 48 lines
- `sed -n '1,360p' processor/agentic-loop/publication_semantics_integration_test.go` → 52 lines
- `nl -ba processor/agentic-model/publication_semantics_integration_test.go; nl -ba processor/agentic-loop/publication_semantics_integration_test.go` → 100 lines

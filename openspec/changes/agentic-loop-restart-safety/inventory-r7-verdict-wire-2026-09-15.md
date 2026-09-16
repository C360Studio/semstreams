# Inventory: R7 governance VERDICT wire and caller seam
base: c347eff487f50b93bc338d764f43ef5b5ea5e133

Worktree: `/Users/coby/Code/c360/semstreams-wt/codex/gh1146-agentic-loop-restart`

Snapshot: 2026-09-15; existing R6/R7/R8 worktree modifications retained. Pins use the current file bytes recorded below, not an assertion that dirty files equal HEAD. Root owns this worktree; this supplement owns only this file.

## Snapshot hashes

SHA-256 of `git diff --binary`: `2b22b83e2cf1f5ccc5a2b8178c2114930ab82602239278ec3aef008ebeac6ea0`.

```text
9ff88f288c6013deec20de0de58606b903b43be0edfd2c61e02d31b3d8fa5008  processor/agentic-loop/component.go
4bcc9d02b38c8d57cecff74959a1ccf064bcf1a70898df2b4b8b3c78ca99c863  processor/agentic-loop/governance_dispatcher.go
a3b646ea2e1844038b120d594225e9281bc0b47b6c7dfa1f49b52e664bcab557  processor/agentic-loop/delivery_owner_test.go
2abc258ac88a08106e9072309a0a521f114385fdbd34719bf134b4158155e26b  processor/agentic-loop/recovery_test.go
9e7ec99b964596a63a986e3abeb585524f6c4143f987e994a6338b6ab2bd5ed1  processor/agentic-loop/governance_dispatcher_test.go
8ca7ae2c2310bbafbf8d7237d45b461a87add51e28dd47b86bb2ecd66276f472  processor/agentic-loop/execution_identity_test.go
3fd0b3e0802025803f01c9c144e7b8fa2cf3809417678e9d75e4a1b7f3ba5604  processor/agentic-loop/config.go
c50068059fb3ce4aff217ed552d02a3796c3896c691565ab4453e77d786d6941  processor/rule/actions.go
4b43f56ed0bf6a6613a10e0a2af51474094749f414685ce248b733a1d0ee6e7f  processor/rule/actions_test.go
7be4cec158ac7e1228901c81173007bacccb5a3d9b39e65413de3bdeed263410  processor/rule/publisher.go
78edd2d46eb6018d821ff7dadc938112e6bb669125628b69c85c96a4755a4554  message/generic_json.go
25718d7d3ca9de0dd4600462cb63a324f17430771dd288b0722c2e45d13029f2  message/decoder.go
1b9d8236bf993953490470ea4fd9ff88977ebd9ab1dee7ef08e46687beebb8da  payloadbuiltins/register.go
d81af91b3167eedaf5626c4e2ddb9bc7593f36b021b43b60c405d0fc7b38aa04  cmd/semstreams/main.go
3b4093a6e1e8b34fa1fe9cbd762ee29c5302fc7fec8a622c30ecbef9bc053c01  cmd/e2e-semstreams/main.go
144a85fbcfd0dfe63f73cfd50ef4ca20344e635ea84ae2fd0fa83cbbf808f4d1  configs/agentic.json
77c9f8bca171701f5e9133735578be4b185577d319358447b160c7e16826bf2f  openspec/changes/agentic-loop-restart-safety/design.md
f3bb69f2dabc6028451dd8ce1b29a7ece5a29e2e8c12a31d279a3790f022c554  openspec/changes/agentic-loop-restart-safety/specs/agentic-governance/spec.md
96d9c252c96d938a19629cf7c59da95da67815f6e4b9f5a2d612d2bffe65904a  openspec/changes/agentic-loop-restart-safety/inventory-r7-governance-evidence-2026-09-14.md
54e20f241c932b8e751ff9f193ba6dca1ff2e615312f2292e4cdada7e57939af  openspec/changes/agentic-loop-restart-safety/inventory-r7-rule-replay-2026-09-14.md
```

Current Git blob hashes, read without `-w`:

```text
f4e7586e5c923f87c704ebef048e6a7ae99a5639  processor/agentic-loop/component.go
2ec82e83e0eb9f59015ca9fa59afabf50a29557c  processor/agentic-loop/governance_dispatcher.go
7127986617b888ababe11a81ecb8cdca4a66f45f  processor/rule/publisher.go
```

Frozen references: `inventory-r7-governance-evidence-2026-09-14.md`, `inventory-r7-rule-replay-2026-09-14.md`, and the child inventory under `/Users/coby/Code/c360/semstreams-wt/codex/gh1311-governance-proposal-settlement` (not reopened; exact inventory-relative path UNVERIFIED).

## Claimed gap

- `processor/agentic-loop/component.go:2577` — `return dispatcher.HandleVerdict(decision, executionID, data)`
- `processor/agentic-loop/governance_dispatcher.go:324` — `_ = json.Unmarshal(data, &payload)`
- `processor/agentic-loop/governance_dispatcher.go:502` — `_ = json.Unmarshal(data, &payload)`
- `processor/agentic-loop/component.go:2607` — `if err := json.Unmarshal(data, &raw); err == nil {`
- `processor/rule/actions.go:1127` — `data, err := json.Marshal(payload)`
- `processor/agentic-loop/governance_dispatcher.go:230` — `HandleVerdict(decision, executionID string, data []byte) (natsclient.DeliveryDecision, error)`

## Spellings of the fact

- `processor/agentic-loop/governance_dispatcher.go:211` — `type GovernanceDispatcher interface {`
- `processor/agentic-loop/governance_dispatcher.go:136` — `type VerdictPayload struct {`
- `processor/agentic-loop/governance_dispatcher.go:150` — `func (v VerdictPayload) effectiveExecutionID() string {`
- `processor/agentic-loop/governance_dispatcher.go:165` — `func (v VerdictPayload) EffectiveCallID() string {`
- `processor/agentic-loop/governance_dispatcher.go:180` — `func (v VerdictPayload) EffectiveDecision() string {`
- `processor/agentic-loop/governance_dispatcher.go:194` — `func (v VerdictPayload) EffectiveReason() string {`
- `processor/agentic-loop/governance_dispatcher.go:155` — `if executionID, ok := v.Properties["execution_id"].(string); ok {`
- `processor/agentic-loop/governance_dispatcher.go:170` — `if cid, ok := v.Properties["call_id"].(string); ok {`
- `processor/agentic-loop/governance_dispatcher.go:185` — `if d, ok := v.Properties["decision"].(string); ok {`
- `processor/agentic-loop/governance_dispatcher.go:199` — `if r, ok := v.Properties["reason"].(string); ok {`
- `processor/agentic-loop/component.go:2620` — `if v, ok := data["decision"].(string); ok {`
- `processor/agentic-loop/component.go:2623` — `if v, ok := data["call_id"].(string); ok {`
- `processor/agentic-loop/component.go:2626` — `if v, ok := data["loop_id"].(string); ok {`
- `processor/agentic-loop/component.go:2629` — `if v, ok := data["request_id"].(string); ok {`
- `processor/agentic-loop/component.go:2632` — `if v, ok := data["execution_id"].(string); ok {`
- `processor/agentic-loop/component.go:2635` — `if v, ok := data["proposal_fingerprint"].(string); ok {`
- `processor/agentic-loop/component.go:2638` — `if v, ok := data["rule_id"].(string); ok {`
- `processor/agentic-loop/component.go:2641` — `if v, ok := data["reason"].(string); ok {`
- `processor/agentic-loop/component.go:2644` — `if v, ok := data["entity_id"].(string); ok {`
- `processor/agentic-loop/component.go:2647` — `if v, ok := data["timestamp"].(string); ok {`
- `processor/agentic-loop/component.go:2650` — `if v, ok := data["properties"].(map[string]any); ok {`
- `processor/rule/actions.go:1091` — `subject := ec.SubstituteVariables(ctx, action.Subject)`
- `processor/rule/actions.go:1103` — `properties := substituteStringPropertiesContext(ctx, action.Properties, ec)`
- `processor/rule/actions.go:1111` — `"properties": properties,`
- `processor/rule/actions.go:2099` — `subject := ec.SubstituteVariables(ctx, action.Subject)`
- `processor/rule/actions.go:2142` — `if v, ok := ec.MessageData["call_id"].(string); ok && v != "" {`
- `processor/rule/actions.go:2145` — `if v, ok := ec.MessageData["loop_id"].(string); ok && v != "" {`
- `processor/rule/actions.go:2148` — `for _, field := range []string{"request_id", "execution_id", "proposal_fingerprint"} {`
- `processor/rule/actions.go:2161` — `baseMsg := message.NewBaseMessage(generic.Schema(), generic, "rule_engine")`

## Adjacent claims

- `openspec/changes/agentic-loop-restart-safety/specs/agentic-governance/spec.md:58` — `Every proposal SHALL carry LoopID, RequestID, execution identity, and proposal fingerprint. Verdict subjects SHALL`
- `openspec/changes/agentic-loop-restart-safety/specs/agentic-governance/spec.md:60` — `retained verdict before republishing a proposal. Missing or full waiter channels SHALL NOT authorize completed`
- `openspec/changes/agentic-loop-restart-safety/specs/agentic-governance/spec.md:77` — `Every validated task, request, response, proposal, and verdict publication SHALL carry its lane's required`
- `openspec/changes/agentic-loop-restart-safety/design.md:595` — `Each proposal carries LoopID, RequestID, execution identity, and a proposal fingerprint. Verdict subjects use the`
- `openspec/changes/agentic-loop-restart-safety/design.md:596` — `NATS-safe execution identity. A replacement response handler first checks for an exact matching retained verdict`
- `docs/adr/039-tool-call-governance-rule-driven.md:184` — `"subject": "agent.toolcall.rejected.$message.loop_id.$message.call_id",`
- `docs/operations/migration-beta68-to-beta70.md:259` — `| `agenticloop.VerdictPayload` | Wire shape accepted on `agent.toolcall.approved/rejected.>` |`

- #1146 / PR #1159 — brief identifiers; live titles/bodies UNVERIFIED (GitHub API connection failed).
- #1146 — owner comment supplied by root: https://github.com/C360Studio/semstreams/issues/1146#issuecomment-5677581095 (comment body NOT READ).
- #1158 — general publisher migration; adjacent identifier supplied by brief; no census.
- OpenSpec list — `agentic-loop-restart-safety` 13/22; `semantic-jetstream-settlement` 44/67.

## Consumers

- `processor/agentic-loop/governance_dispatcher.go:282` — `func (d *disabledDispatcher) HandleVerdict(decision, executionID string, _ []byte) (natsclient.DeliveryDecision, error) {`
- `processor/agentic-loop/governance_dispatcher.go:319` — `func (d *auditDispatcher) HandleVerdict(decision, executionID string, data []byte) (natsclient.DeliveryDecision, error) {`
- `processor/agentic-loop/governance_dispatcher.go:489` — `func (d *enforceDispatcher) HandleVerdict(decision, executionID string, data []byte) (natsclient.DeliveryDecision, error) {`
- `processor/agentic-loop/delivery_owner_test.go:666` — `func (d *settlementVerdictDispatcher) HandleVerdict(decision, callID string, _ []byte) (natsclient.DeliveryDecision, error) {`
- `processor/agentic-loop/recovery_test.go:29` — `func (*contextCapturingGovernanceDispatcher) HandleVerdict(string, string, []byte) (natsclient.DeliveryDecision, error) {`
- `processor/agentic-loop/execution_identity_test.go:165` — `decision, err := dispatcher.HandleVerdict("approved", executionID, payload)`
- `processor/agentic-loop/governance_dispatcher_test.go:39` — `decision, err := disabled.HandleVerdict("approved", "call-disabled", nil)`
- `processor/agentic-loop/governance_dispatcher_test.go:44` — `decision, err = audit.HandleVerdict("approved", "call-audit", nil)`
- `processor/agentic-loop/governance_dispatcher_test.go:53` — `decision, err = enforce.HandleVerdict("approved", "missing", nil)`
- `processor/agentic-loop/governance_dispatcher_test.go:58` — `decision, err = enforce.HandleVerdict("approved", "delivered", nil)`
- `processor/agentic-loop/governance_dispatcher_test.go:66` — `decision, err = enforce.HandleVerdict("rejected", "full", nil)`
- `processor/agentic-loop/governance_dispatcher_test.go:218` — `d.HandleVerdict("approved", "execution-call-001", payload)`
- `processor/agentic-loop/governance_dispatcher_test.go:245` — `d.HandleVerdict("rejected", "execution-call-001", payload)`
- `processor/agentic-loop/governance_dispatcher_test.go:313` — `d.HandleVerdict("approved", "execution-c3", approvedPayload)`
- `processor/agentic-loop/governance_dispatcher_test.go:314` — `d.HandleVerdict("rejected", "execution-c2", rejectedPayload)`
- `processor/agentic-loop/governance_dispatcher_test.go:315` — `d.HandleVerdict("approved", "execution-c1", approvedPayload)`
- `processor/agentic-loop/governance_dispatcher_test.go:353` — `d.HandleVerdict("approved", "execution-c1", payload)`
- `processor/agentic-loop/governance_dispatcher_test.go:385` — `d.HandleVerdict("approved", "execution-fast-call", payload)`
- `processor/agentic-loop/governance_dispatcher_test.go:414` — `d.HandleVerdict("approved", "execution-late-call", payload)`
- `processor/agentic-loop/governance_dispatcher_test.go:467` — `d.HandleVerdict("approved", "execution-c1", payload)`
- `processor/agentic-loop/governance_dispatcher_test.go:521` — `d.HandleVerdict("approved", "execution-late-call", payload)`
- `processor/agentic-loop/component.go:925` — `case "agent.toolcall.approved", "agent.toolcall.rejected":`
- `processor/agentic-loop/component.go:2565` — `payload, ok := decodeVerdictPayload(c.decoder, data)`
- `processor/agentic-loop/config.go:421` — `Name: "agent.toolcall.approved", Config: component.JetStreamPort{Subjects: []string{"agent.toolcall.approved.>"}, StreamName: "AGENT"}, Required: false,`
- `processor/agentic-loop/config.go:425` — `Name: "agent.toolcall.rejected", Config: component.JetStreamPort{Subjects: []string{"agent.toolcall.rejected.>"}, StreamName: "AGENT"}, Required: false,`
- `configs/agentic.json:243` — `"agent.toolcall.approved.*"`
- `configs/agentic.json:254` — `"agent.toolcall.rejected.*"`
- `configs/agentic.json:295` — `"subject": "agent.toolcall.approved.$message.execution_id",`
- `service/message_logger.go:394` — `{"agent.toolcall.approved.>", "agent.toolcall.approved.*"},`
- `service/message_logger.go:395` — `{"agent.toolcall.rejected.>", "agent.toolcall.rejected.*"},`
- `service/message_logger.go:329` — `return ml.decoder.Decode(data)`
- `/Users/coby/Code/c360/semspec/configs/e2e.json:264` — `"subject": "agent.toolcall.rejected.$message.loop_id.$message.call_id",`
- `/Users/coby/Code/c360/semspec/configs/e2e.json:267` — `"call_id": "$message.call_id",`
- `/Users/coby/Code/c360/semspec/configs/e2e.json:268` — `"loop_id": "$message.loop_id",`
- `/Users/coby/Code/c360/semspec/configs/e2e.json:417` — `"type": "publish",`
- `/Users/coby/Code/c360/semspec/configs/e2e.json:418` — `"subject": "agent.toolcall.approved.$message.loop_id.$message.call_id",`
- `/Users/coby/Code/c360/semspec-ui-bmad/configs/e2e-gemini.json:409` — `"subject": "agent.toolcall.rejected.$message.loop_id.$message.call_id",`
- `/Users/coby/Code/c360/semspec-ui-run-visibility/configs/e2e-gemini.json:409` — `"subject": "agent.toolcall.rejected.$message.loop_id.$message.call_id",`

## Problem shape

- `processor/agentic-loop/component.go:2596` — `if baseMsg, err := decoder.Decode(data); err == nil {`
- `processor/agentic-loop/component.go:2597` — `if generic, ok := baseMsg.Payload().(*message.GenericJSONPayload); ok {`
- `processor/agentic-loop/component.go:2598` — `return verdictPayloadFromMap(generic.Data), true`
- `message/decoder.go:45` — `msg := &BaseMessage{registry: d.registry}`
- `message/generic_json.go:28` — `func RegisterPayloads(reg *payloadregistry.Registry) error {`
- `message/generic_json.go:127` — `func (g *GenericJSONPayload) RuleFields() map[string]any {`
- `message/generic_json.go:128` — `return g.Data`
- `payloadbuiltins/register.go:44` — `track(message.RegisterPayloads(reg))`
- `cmd/semstreams/main.go:788` — `if err := payloadbuiltins.Register(reg); err != nil {`
- `cmd/e2e-semstreams/main.go:406` — `if err := payloadbuiltins.Register(reg); err != nil {`
- `processor/rule/actions.go:1092` — `if targetsReservedUserResponseSubject(subject) {`
- `processor/rule/publisher.go:74` — `if flowgraph.SubjectCovers(declaredFilter, subject) {`

## Local adopter search baselines

Direct tracked literals only: Go, JSON, YAML, TypeScript, Svelte. No downstream writes.

| Checkout under `/Users/coby/Code/c360/` | HEAD | Matching lines |
|---|---|---:|
| semboids | 8c03cc53836ced93a5df7064473c63ff144e64f1 | 0 |
| semconnect | d0d06e00bf05a545f30ceea798db1c2b1ee47d4f | 0 |
| semdev | ca3956af2ed87d5fa5bdb8183cdb506f7beb7240 | 0 |
| semdev-test | 39ac17adf2e2eeb16e672cd445274625f1cb611d | 0 |
| semdev-test-sub | c8ab7b61d214b9a5ae8c3a3f533b1333fe124393 | 0 |
| semdocs | 2b96f6008a02b23ded7bb2665280c6a7317314d6 | 0 |
| semdragon | 07f4de9b65887801ff18a7273d14233023049321 | 0 |
| semembed | 7ceb5281c96b3664321f3f28c9d7f96acbb41843 | 0 |
| seminstruct | 7f9135a99cd27a6c63a2a60db5daeee9f5622be4 | 0 |
| semlink | 985e97d8d2181a2eef6caae7ba640195e96ecd58 | 0 |
| semmachina | 841c45e8bb01af19495d4294a7f510a8a0c2e8c2 | 0 |
| semmem | b909cbf1abe771aaf733379d92d6706a846a2125 | 0 |
| semmem-test | UNVERIFIED: no HEAD | 0 |
| semops | 602c619a9f1caa8adac624cffda9d1afa9ad80f3 | 0 |
| semsage | 4d28b4dc1210f47da84a3031125167d164de9290 | 0 |
| semsource | 4093d3ce421371f4a99d7168e372552899bf6795 | 0 |
| semspec | 5a9496eecc453747f4bc557b95444db6304c1420 | 130 |
| semspec-ui-bmad | c8308d7e258587f3d96e59b8d2a8b3f8acc922b7 | 48 |
| semspec-ui-run-visibility | e30cbf78691d1a185033903869d9e6fd92ac3356 | 48 |
| semstreams-ui | 39f5f04030e54cd7e5ac1b20490b877bb7b7f2dd | 0 |
| semsummarize | UNVERIFIED: not a Git repository | 0 |
| semteams | ce22c961d30014c463a09f8f8a2a90044ee1a1cf | 0 |
| servicesim | aeb86e135d89699bb10141707ddb63f5f4559f42 | 0 |

Sister pinned-file SHA-256:

```text
d25ca6cb95c5de02b705ce434665340fc6672ae6ca3a4e68e0f98b5164ebeaad  /Users/coby/Code/c360/semspec/configs/e2e.json
65cf005e2b48e8cba3cc59d124309b64d122b68450563abe6fa3c0c0b88e092f  /Users/coby/Code/c360/semspec-ui-bmad/configs/e2e-gemini.json
65cf005e2b48e8cba3cc59d124309b64d122b68450563abe6fa3c0c0b88e092f  /Users/coby/Code/c360/semspec-ui-run-visibility/configs/e2e-gemini.json
```

## Searches

All commands ran in the worktree unless explicitly qualified. Gopls environment: `GOCACHE=/private/tmp/semstreams-r7-test-cache GOPROXY=off GOSUMDB=off GOFLAGS=-mod=readonly`; after the initial three symbol searches, also `GOPLSCACHE=/private/tmp/semstreams-r7-gopls-cache`. The initial searches emitted cache-write permission errors; later searches used the temporary cache.

- `ls /Users/coby/Code/c360` → 28 directory entries (local checkout discovery).
- `gopls workspace_symbol -matcher=fuzzy ToolCallDispatcher` → 0 symbol hits; cache-write errors.
- `gopls workspace_symbol -matcher=fuzzy HandleVerdict` → 8 symbol hits; cache-write errors.
- `gopls workspace_symbol -matcher=fuzzy VerdictPayload` → 20 symbol hits; cache-write errors.
- `gopls implementation processor/agentic-loop/governance_dispatcher.go:211:6` → 5 hits.
- `gopls references processor/agentic-loop/governance_dispatcher.go:230:2` → 17 hits.
- `gopls references processor/agentic-loop/governance_dispatcher.go:136:6` → 25 hits.
- `gopls references processor/agentic-loop/component.go:2593:6` → 1 hits.

```sh
git grep -n -E 'ToolCallDispatcher|GovernanceDispatcher|HandleVerdict|handle_verdict|tool_call_dispatcher' -- processor/agentic-loop
```

Result: 74 matching lines.

```sh
git grep -n -E 'VerdictPayload|verdictPayloadFromMap|decodeVerdictPayload|agent\.toolcall\.(approved|rejected)|verdict_subject|verdictSubject' -- processor/rule/actions.go processor/agentic-loop/component.go processor/agentic-loop/governance_dispatcher.go processor/agentic-governance config configs examples rules schemas specs message pkg/payloadbuiltins agentic cmd
```

Result: 33 matching lines.

```sh
git grep -n -E 'ToolCallDispatcher|toolCallDispatcher|tool_call_dispatcher|tool-call-dispatcher|TOOL_CALL_DISPATCHER|HandleVerdict|handleVerdict|handle_verdict|handle-verdict|HANDLE_VERDICT' -- ':!processor/agentic-loop' ':!go.sum'
```

Result: 9 matching lines.

```sh
git grep -n -E 'agent\.toolcall\.(approved|rejected)|verdict_subject|verdictSubject|VERDICT_SUBJECT|verdict-subject' -- configs config processor/agentic-loop/config.go processor/agentic-loop/config_test.go processor/agentic-loop/component_test.go processor/agentic-loop/delivery_owner_test.go processor/agentic-loop/handlers_rehydration.go processor/agentic-loop/recovery.go test examples rules
```

Result: 16 matching lines.

```sh
git grep -n -Ei 'verdict|approved|rejected' -- processor/agentic-loop/*recover* processor/agentic-loop/*rehydrat*
```

Result: FAILED before search: zsh unmatched unquoted glob; 0 searched files.

```sh
git grep -n -E 'DecodePayload|Normalize|normaliz|RegisterPayload|RegisterCore|NewDecoder|core.json' -- message/decoder.go message/generic_json.go message/payload_registry.go pkg/payloadbuiltins cmd/semstreams/main.go cmd/e2e-semstreams/main.go processor/agentic-loop/component.go processor/agentic-governance/component.go processor/rule/component.go
```

Result: 25 matching lines.

```sh
git grep -n -E 'agent\.toolcall\.(approved|rejected)|retained.*verdict|verdict.*retained' -- processor/agentic-loop '*.go' ':!processor/agentic-loop/governance_dispatcher_test.go' ':!processor/agentic-loop/component_test.go' ':!processor/agentic-loop/delivery_owner_test.go'
```

Result: 37 matching lines.

```sh
git grep -n -E 'normaliz|payloadbuiltins.Register|message.RegisterPayloads|PayloadRegistry' -- message processor/rule/publisher.go payloadbuiltins cmd/semstreams/main.go cmd/e2e-semstreams/main.go processor/agentic-governance/component.go
```

Result: 30 matching lines.

```sh
git grep -n -E 'agent\.toolcall\.(approved|rejected)|verdict|Verdict|custom.*subject' -- openspec/changes/agentic-loop-restart-safety/specs/agentic-governance/spec.md openspec/changes/agentic-loop-restart-safety/design.md openspec/specs/agentic-governance/spec.md docs/adr/039* docs/operations/migration-*
```

Result: 102 matching lines.

```sh
git grep -n -E 'GenericJSONPayload|\.Payload\(\)|decoder.Decode|json.Unmarshal' -- processor/rule/message*.go processor/rule/publisher.go message/decoder.go message/generic_json.go service/message_logger.go
```

Result: 28 matching lines.

```sh
git grep -n -E 'retained|Retained|verdict|Verdict' -- 'processor/agentic-loop/*recover*' 'processor/agentic-loop/*rehydrat*' processor/agentic-loop/governance_dispatcher.go
```

Result: 186 matching lines.

```sh
gh issue list --search 'verdict' --state open --json number,title --limit 100
openspec list
 gh pr list --draft --json number,title,body --limit 100
```

Result: GitHub issue query FAILED (API connection); OpenSpec list 2 changes; GitHub draft PR query FAILED (API connection).

```sh
for repo in semboids semconnect semdev semdev-test semdev-test-sub semdocs semdragon semembed seminstruct semlink semmachina semmem semmem-test semops semsage semsource semspec semspec-ui-bmad semspec-ui-run-visibility semstreams-ui semsummarize semteams servicesim; do printf '\n%s\n' "/Users/coby/Code/c360/$repo"; git -C "/Users/coby/Code/c360/$repo" rev-parse HEAD; git -C "/Users/coby/Code/c360/$repo" grep -n -E 'ToolCallDispatcher|GovernanceDispatcher|HandleVerdict|VerdictPayload|agent\.toolcall\.(approved|rejected)' -- '*.go' '*.json' '*.yaml' '*.yml' '*.ts' '*.svelte'; done
```

Result: 226 matching lines across 23 attempted checkout paths; per-checkout counts and failures above; 0 direct Go/TypeScript/Svelte symbol uses.

```sh
GOPLSCACHE=/private/tmp/semstreams-r7-gopls-cache GOCACHE=/private/tmp/semstreams-r7-test-cache GOPROXY=off GOSUMDB=off GOFLAGS=-mod=readonly gopls workspace_symbol -matcher=fuzzy RuleFields
```

Result: 100 fuzzy workspace-symbol results (including dependencies); only GenericJSONPayload.RuleFields retained.

```sh
GOPLSCACHE=/private/tmp/semstreams-r7-gopls-cache GOCACHE=/private/tmp/semstreams-r7-test-cache GOPROXY=off GOSUMDB=off GOFLAGS=-mod=readonly gopls workspace_symbol -matcher=fuzzy RetainedVerdict
```

Result: 0 symbol hits.

```sh
git grep -n -Ei 'normaliz|RuleFields|RuleReadable' -- processor/rule/payload_projection.go message/base.go message/base_message.go message/rule_readable.go
 git grep -n -A 3 -B 1 -E 'Type:.*ActionTypeApprove|"type": *"approve"' -- processor/rule/actions_test.go configs examples
```

Result: 10 matching projection/normalization lines; 8 approve-action matches (context printed separately).

```sh
git grep -n -Ei 'normaliz|decoder.Decode|GenericJSON|json.Unmarshal' -- 'processor/agentic-governance/*.go'
 git grep -n -E 'inventory:verify|inventory_verify|verify_inventory' -- Taskfile.yml Taskfile.yaml scripts
```

Result: 14 governance normalization/decode matching lines; 2 inventory-task matches.

```sh
 git grep -n -E 'ToolCallDispatcher|toolCallDispatcher|tool_call_dispatcher|tool-call-dispatcher|TOOL_CALL_DISPATCHER|ReadVerdict|readVerdict|retainedVerdict|retained_verdict' -- '*.go'
```

Result: 0 matching lines.

- `gopls references processor/agentic-loop/governance_dispatcher.go:670:6` → 1 hit: `processor/agentic-loop/governance_dispatcher_test.go:547:11`.

### Pin reads and snapshot commands

```sh
cat .agents/contracts/semstreams-explorer.md
# Above contract read ran in /Users/coby/Code/c360/semstreams.
git rev-parse HEAD
git status --short
sed -n '1,115p' openspec/project.md
command -v gopls
```

Results: contract read in full; HEAD recorded above; 10 modified tracked paths and 11 untracked paths; Purpose and Product Boundary read; gopls `/Users/coby/go/bin/gopls`.

```sh
sed -n '1080,1155p' processor/rule/actions.go
sed -n '2085,2200p' processor/rule/actions.go
sed -n '110,240p' processor/agentic-loop/governance_dispatcher.go
sed -n '2530,2665p' processor/agentic-loop/component.go
```

```sh
nl -ba processor/agentic-loop/governance_dispatcher.go | sed -n '315,340p;485,528p;660,700p'
nl -ba message/generic_json.go | sed -n '10,45p;70,100p;120,145p'
nl -ba message/decoder.go | sed -n '26,60p'
nl -ba processor/rule/message_handler.go | sed -n '55,83p;418,463p'
nl -ba configs/agentic.json | sed -n '235,302p'
nl -ba processor/rule/actions_test.go | sed -n '580,625p;3330,3388p'
```

```sh
nl -ba processor/rule/publisher.go | sed -n '40,135p'
nl -ba openspec/changes/agentic-loop-restart-safety/design.md | sed -n '589,605p'
nl -ba openspec/changes/agentic-loop-restart-safety/specs/agentic-governance/spec.md | sed -n '56,85p'
nl -ba /Users/coby/Code/c360/semspec/configs/e2e.json | sed -n '260,282p;413,422p'
nl -ba payloadbuiltins/register.go | sed -n '32,48p'
nl -ba cmd/semstreams/main.go | sed -n '783,799p'
```

```sh
git diff --binary | shasum -a 256
shasum -a 256 processor/agentic-loop/component.go processor/agentic-loop/governance_dispatcher.go processor/agentic-loop/delivery_owner_test.go processor/agentic-loop/recovery_test.go processor/agentic-loop/governance_dispatcher_test.go processor/agentic-loop/execution_identity_test.go processor/agentic-loop/config.go processor/rule/actions.go processor/rule/actions_test.go processor/rule/publisher.go message/generic_json.go message/decoder.go payloadbuiltins/register.go cmd/semstreams/main.go cmd/e2e-semstreams/main.go configs/agentic.json openspec/changes/agentic-loop-restart-safety/design.md openspec/changes/agentic-loop-restart-safety/specs/agentic-governance/spec.md openspec/changes/agentic-loop-restart-safety/inventory-r7-governance-evidence-2026-09-14.md openspec/changes/agentic-loop-restart-safety/inventory-r7-rule-replay-2026-09-14.md
 git hash-object processor/agentic-loop/component.go processor/agentic-loop/governance_dispatcher.go processor/rule/publisher.go
```

```sh
nl -ba processor/rule/actions.go | sed -n '1082,1135p;2093,2109p;2130,2178p'
nl -ba processor/agentic-loop/component.go | sed -n '918,930p;2560,2660p'
```

```sh
sed -n '100,111p' Taskfile.yml
shasum -a 256 /Users/coby/Code/c360/semspec/configs/e2e.json /Users/coby/Code/c360/semspec-ui-bmad/configs/e2e-gemini.json /Users/coby/Code/c360/semspec-ui-run-visibility/configs/e2e-gemini.json
```

### NOT RUN / UNVERIFIED

- NOT RUN: broader R7 source/replay, rule input/state/reload/effect retry, retention, child admission/reload, general publisher migration (#1158), or all-history sweeps; frozen by brief.
- NOT RUN: network retry/escalation for open issues or draft PR bodies; GitHub claim contents remain UNVERIFIED.
- NOT RUN: external deployments, dynamically constructed verdict/custom destinations, untracked sister configuration, nested sister worktrees, or consumers outside the 23 listed paths; their destination/wire impact remains UNVERIFIED.
- NOT RUN: sibling structural gopls analysis; direct tracked symbol/literal search only, per brief.
- NOT RUN: alias/import-driven downstream use beyond the literal spellings searched; no claim of complete external consumer coverage.
- NOT RUN: Go unit/integration/E2E/registry compatibility tests, runtime subscriptions, or retained NATS-message reads; this is an inventory.
- NOT LOCATED: `ToolCallDispatcher` (all recorded Go literal spellings returned 0); enumerated exported interface is `GovernanceDispatcher`.
- NOT LOCATED: `RetainedVerdict` workspace symbol or Go `ReadVerdict|readVerdict|retainedVerdict|retained_verdict` literals (0); no existing retained-verdict interpreter pin collected.
- NOT LOCATED: `verdict_subject|verdictSubject|VERDICT_SUBJECT|verdict-subject` in the recorded configuration/path search; configured subjects and caller-supplied `action.Subject` pins are above.
- UNVERIFIED: custom approve destinations outside the protocol families; the scoped approve-action search returned only family destinations and a missing-subject test.

### Inventory verification

```sh
task inventory:verify -- openspec/changes/agentic-loop-restart-safety/inventory-r7-verdict-wire-2026-09-15.md
shasum -a 256 openspec/changes/agentic-loop-restart-safety/inventory-r7-verdict-wire-2026-09-15.md
git diff --binary | shasum -a 256
```

First verification: `pins=91 ok=91 moved=0 ambiguous=0 drift=0 malformed=0 unparsed=0`.
The tracked-diff hash remained `2b22b83e2cf1f5ccc5a2b8178c2114930ab82602239278ec3aef008ebeac6ea0`.
The interface declaration pin was then added; final verification and final file hash are in the handoff.

## Inventory counts

Pins: Claimed gap 6; Spellings of the fact 29; Adjacent claims 7; Consumers 38; Problem shape 12; total 92.

Discovery query templates: 30 (52 executions when the 23-path sister loop is expanded), plus one directory listing. Failed attempts and zero-hit searches are included. Bounded command-tool calls before file creation: 32. No source or Git mutations.

# R7 governance wire: architect inventory supplement
base: c347eff487f50b93bc338d764f43ef5b5ea5e133

## Scope and checkpoint

This inventory supplements, without replacing or changing,
`inventory-r7-verdict-wire-2026-09-15.md`, SHA-256
`e47d02766f636eaf49ed6dea560906ce4488457c113912c5cab1daaf1c4bfcaa`.

The original 92 pins remain the surface census. Its five GovernanceDispatcher implementations and 17
HandleVerdict references include one production caller. The independent reviewer separately confirmed those counts.

The owner permits bounded intake for this slice:
https://github.com/C360Studio/semstreams/issues/1146#issuecomment-5677867478.
The existing verdict-family boundary is recorded at:
https://github.com/C360Studio/semstreams/issues/1146#issuecomment-5677581095.
The root session verified those live rulings; this architect did not independently fetch their bodies.

Intake comprised the full current proposal, design and tasks; both active governance and rule-agent-publishing
deltas; current rule-engine and message-logger specs; ADR-039 in full; and directly relevant source/caller ranges.
Current `openspec/specs/agentic-governance/spec.md` and
`openspec/specs/rule-agent-publishing/spec.md` do not exist in this worktree.
Historical broad R7 inventories, replay/source work and child inventory remain frozen.

This is an inventory, not a target-state draft or implementation approval.

## Necessary surface additions

### Publish authoring and its wrapper

- `processor/rule/actions.go:1108` — `"subject":    subject,`
- `processor/rule/actions.go:1110` — `"source":     "rule_engine",`
- `processor/rule/actions.go:1114` — `payload["related_id"] = ec.RelatedID`
- `processor/rule/actions.go:1132` — `if err := e.publisher.Publish(ctx, subject, data); err != nil {`
- `processor/rule/actions.go:2041` — `return &DenyVerdict{RuleID: ruleID, Reason: reason}`
- `processor/rule/actions.go:2113` — `e.emitVerdictAudit(ctx, governance.DecisionApprove, ruleID, reason, ec)`
- `processor/rule/actions.go:2124` — `return nil`

`publish` builds entity_id, subject, timestamp, source, properties and optional related_id, then serializes that
whole wrapper as raw JSON. Its properties come from the existing substitution path. `approve` already wraps its
flat verdict map in registered GenericJSON/BaseMessage and copies the proposal correlation fields when present.
`deny` produces the separate structural DenyVerdict and audit consequence; it does not publish the routing rejection.
An absent publisher currently returns success for both publish and approve. That is observed existing behavior,
not permission to redefine settlement in this codec slice.

### Existing exported component-author seam

- `processor/agentic-loop/governance_dispatcher.go:247` — `func NewGovernanceDispatcher(cfg ToolCallGovernanceConfig, publisher VerdictPublisher, logger *slog.Logger, metrics DispatcherMetrics) GovernanceDispatcher {`
- `processor/agentic-loop/handlers.go:429` — `// it — the setter must be called between NewComponent and Start.`
- `processor/agentic-loop/handlers.go:435` — `func (h *MessageHandler) SetGovernanceDispatcher(d GovernanceDispatcher) {`
- `processor/agentic-loop/handlers.go:442` — `func (h *MessageHandler) GovernanceDispatcher() GovernanceDispatcher {`

GovernanceDispatcher is externally constructible/installable/readable. Its HandleVerdict signature is therefore
an adopter surface even though the bounded sister search found no direct Go-symbol users. Normal NewComponent
composition supplies the dispatcher automatically. Its setter documentation says nil reverts to disabled
pass-through, whereas the current verdict handler quarantines a nil dispatcher; this documentation conflict is
existing evidence, not an additional change requested here.

### Decode, validation and propagation are distinct

- `message/decoder.go:49` — `return msg, nil`
- `message/base_message.go:195` — `if err := m.payload.Validate(); err != nil {`
- `message/base_message.go:301` — `payload := m.registry.Create(m.msgType.Domain, m.msgType.Category, m.msgType.Version)`
- `message/base_message.go:314` — `m.payload = msgPayload`
- `message/base_message.go:319` — `return nil`
- `message/generic_json.go:98` — `if g.Data == nil {`
- `processor/agentic-loop/governance_dispatcher.go:520` — `case ch <- verdictArrival{decision: decision, reason: payload.EffectiveReason(), ruleID: payload.RuleID}:`

Decoder resolves the registered concrete payload and unmarshals it; it does not itself call BaseMessage.Validate.
GenericJSON.Validate checks only that Data is nonnil. Neither proves governance required correlation.

The current component projects registered GenericJSON.Data into VerdictPayload, but hands original wire bytes to
HandleVerdict. Audit and enforce then unmarshal those envelope bytes directly into VerdictPayload and ignore the
error. The envelope's inner verdict fields consequently do not supply their reason/rule_id to those paths.
For raw publish bytes, the wrapper/properties shape is available to that second unmarshal. This explains the
authorship-dependent loss without asserting any retained-verdict reader exists.

The current component requires only nonempty effective decision and execution_id before dispatch. Its helpers
prefer nonempty top-level values over properties; they do not detect conflicting duplicate representations.
RequestID, LoopID and proposal fingerprint are represented but are not validated against a waiting proposal on
this path. Existing waiters store channels, not an expected-correlation record.

### Physical subject information at the callback boundary

- `processor/agentic-loop/component.go:910` — `settleHandlerFn func(context.Context, []byte) (natsclient.DeliveryDecision, error)`
- `processor/agentic-loop/component.go:933` — `settleHandlerFn = c.handleToolCallVerdictMessage`
- `processor/agentic-loop/component.go:1077` — `decision, cause := runLoopDeliveryWork(msgCtx, msg.Data(), settleHandlerFn)`

Both verdict subscriptions install the same byte-only business handler. The actual message subject is not handed
to that handler. No subject/payload equality check may be claimed from the existing dispatch path.

### Existing problem shape on another lane

- `processor/agentic-loop/component.go:2442` — `baseMsg, err := c.decoder.Decode(data)`
- `processor/agentic-loop/component.go:2447` — `signalPtr, ok := baseMsg.Payload().(*agentic.UserSignal)`
- `processor/agentic-loop/component.go:2462` — `return c.handleCancelSignal(ctx, signal)`
- `processor/rule/payload_projection.go:48` — `if readable, ok := payload.(message.RuleReadable); ok {`
- `processor/rule/payload_projection.go:49` — `return readable.RuleFields(), true`
- `openspec/specs/rule-engine/spec.md:243` — `Projection SHALL have exactly one implementation in the rule engine. A payload's rule-readable`

The closest existing wire-to-business shape is registry decode, concrete-type admission, then typed value handoff
on the signal lane. The rule lane separately owns one projection from declared payload fields; GenericJSON exposes
its Data map. These are existing shapes, not an absence claim requiring establishment of another reusable primitive.

No new durable, communication or runtime-coordination primitive is proposed in this inventory, so no new
same-class collision-table trigger is asserted.

## Adopter seam inventory

| Specific person | What they currently must know | If they do nothing | Discovery | What they should have to know |
|---|---|---|---|---|
| SemSpec rule author using publish plus deny | Verdict destination; decision property; opaque proposal correlation echoes; routing publish versus structural deny; framework wrapper/codec behavior | Inventoried configs use loop_id/call_id subjects and omit request_id, execution_id and fingerprint. Current intake terminates for missing execution_id; codec changes alone do not migrate that correlation. | Runtime settlement error/log or enforce timeout; config still loads | Policy, destination and opaque fields the admitted rule contract requires. No envelope construction or dispatcher parsing knowledge. |
| Component author using standard approve | Configure subject/reason and provide proposal-derived MessageData | Framework already echoes available correlation and produces an envelope, but reason/rule_id disappear in dispatcher re-decoding | Audit output or rejection explanation; no compile error | Policy and observed proposal identity, without knowing the carrier format. |
| External Go author implementing/installing GovernanceDispatcher | Exported interface; routing strings separately from bytes; both byte shapes; install before Start | Existing callers compile. Signature change requires implementation/call-site adaptation even when no literal local sister use was found | Compile error for a signature change; installation ordering documented only | One explicit input contract and lifecycle ordering; no redundant decoding. |
| Raw external verdict publisher or subscriber | Registered carrier, inner wrapper/properties, required correlation and subject grammar | Deployed behavior unmeasured. Raw publisher meets fallback but fails strict registry admission; raw-map subscriber needs the envelope payload after adoption | Runtime decode/refusal, or nowhere if errors ignored | Final wire contract and opaque correlation, without predicting framework serialization. |
| Ordinary component/config author using NewComponent | Governance mode and policy configuration | Framework creates and wires dispatcher | Existing configuration/runtime surfaces | No dispatcher construction, byte conversion or waiter knowledge. |

The first and third rows carry more than two correctness facts: these are seam findings. The audit does not turn
them into a documentation-only solution or select an additional API.

Observation-versus-prediction finding: authors can echo proposal identities they receive; they cannot observe the
consumer's process waiter or reliably reconstruct framework envelope details. Current behavior splits responsibility
for those details between framework and author.

Supporting downstream shape:

- `/Users/coby/Code/c360/semspec/configs/e2e.json:266` — `"decision": "rejected",`
- `/Users/coby/Code/c360/semspec/configs/e2e.json:269` — `"reason": "bash 'cd /workspace' blocked — stay in worktree"`
- `/Users/coby/Code/c360/semspec/configs/e2e.json:420` — `"decision": "approved",`
- `/Users/coby/Code/c360/semspec/configs/e2e.json:422` — `"loop_id": "$message.loop_id"`

The original supplement supplies adjacent subject/property pins and the full 23-path search bounds.
No sister file changed. External deployments, dynamic destinations, aliases and untracked configurations remain
unmeasured; direct literal search is not a complete external-consumer census.

## Searches — additions and empty-category checks

Commands ran in the #1146 worktree unless stated otherwise:

- `rg --files openspec/changes | rg 'inventory-r7-verdict-wire-2026-09-15|agentic-loop-restart/(proposal|design|tasks)\.md$'`
  found the wire supplement.
- `rg --files openspec/changes/agentic-loop-restart-safety/specs` found eight active capability deltas.
- `git grep -n -E 'GenericJSON|GovernanceDispatcher|VerdictPayload|agent\.toolcall\.(approved|rejected)|core.json' -- openspec/specs`
  returned four lines: message-logger containment and rule-engine GenericJSON projection.
- `git grep -n -E 'decoder.Decode|handleSignalMessage|handleApprovalResponse' -- processor/agentic-loop/component.go`
  located registered decode and installed handler paths.
- `rg --files message | rg 'base|wire'` located base_message.go and its test.
- `git grep -n -E 'UnmarshalJSON|Validate\(\)' -- message/base_message.go`
  located the separate decode and validation operations.
- `git grep -n -E 'func .*executeDeny|return.*DenyVerdict|targetsReservedUserResponseSubject' -- processor/rule/actions.go`
  located deny and existing destination refusal.
- `git grep -n -E 'targetsReservedUserResponseSubject|reservedUserResponseSubjectFamily' -- processor/rule`
  located user_response_subject_reservation.go and existing action/load consumers.
- `GOPLSCACHE=/private/tmp/semstreams-r7-gopls-cache GOCACHE=/private/tmp/semstreams-r7-test-cache GOPROXY=off GOSUMDB=off GOFLAGS=-mod=readonly gopls workspace_symbol -matcher=fuzzy RetainedVerdict`
  returned zero hits, independently spot-checking the explorer's negative symbol result.

Failed candidate reads: message/base.go and processor/rule/reserved_subjects.go were absent; discovery above
located actual files. Missing current governance and rule-agent-publishing specs were reported explicitly.
Source ranges read with nl/sed establish the pins. No tests, code edits, Git mutations or sister writes occurred.

The original hash was rechecked and remained e47d02766f636eaf49ed6dea560906ce4488457c113912c5cab1daaf1c4bfcaa.
Root must materialize and mechanically verify this supplemental artifact before independent review.

## Adjacent claims — root verification

Root read live issue bodies using `gh issue view 1158 --json number,title,state,body,labels,milestone` and the
same command for 1311 on 2026-09-15:

- #1158 — OPEN, message/nats: enforce registered payload envelopes on framework-owned subjects; pre-v1,
  unmilestoned. Owns repository-wide publisher/subject/codec classification and migration, not this bounded fix.
- #1311 — OPEN, rule: settle governance proposal work only after durable verdict publication; beta.163.
  Explicitly leaves correlation, retained-verdict and R8 work in #1146. Its original design-phase issue text is
  superseded by the accepted contract ruling at issuecomment-5676602813 and published PR #1312 checkpoint 25ae71ab.

## Gate and remaining evidence

No target state, options, contract delta or TDD tasks are supplied here. Wait for independent INVENTORY PASS.

The exact exported caller shape remains a design-review question. Already-accepted retained lookup/correlation
obligations remain unimplemented evidence work; no existing retained-verdict interpreter is supplied.
#1311 source settlement and the frozen R7 replay inventory remain separate prerequisites.
Broader generic publishing remains #1158.

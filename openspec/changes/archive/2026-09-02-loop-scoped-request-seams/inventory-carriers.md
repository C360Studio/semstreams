# Inventory: every carrier of a loop token, and what happens when its shape is wrong

base: 24ab736e4a0325736a9a7f4b3fe6f0941c675c92

Scope: the #1228 axis (form validation census) + #1225 (Validate failure dropped silently, gauge leak).
LoopTracker internals, permissions/ownership, and restart/rehydration behaviour are a sibling explorer's
surface — crossed only where unavoidable to trace an outcome, flagged under Adjacent claims.

## Claimed gap

The one exported symbol in `internal/looptoken`, and its four production callers re-derived by
`gopls references` at this base (matches the issue's claimed count of four):

- `internal/looptoken/looptoken.go:33` — `func Valid(s string) bool {`
- `agentic/agentrun/agentrun.go:302` — `if !looptoken.Valid(rootLoopID) {`
- `agentic/user_types.go:423` — `if !looptoken.Valid(token.value) {`
- `processor/agentic-dispatch/component.go:894` — `if token.value == "" || looptoken.Valid(token.value) {`
- `processor/agentic-loop/state.go:152` — `if !looptoken.Valid(loopID) {`

The `agentic/user_types.go:423` call site lives inside `TaskMessage.validateLoopTokens`, called from
`TaskMessage.Validate`:

- `agentic/user_types.go:358` — `func (t TaskMessage) Validate() error {`
- `agentic/user_types.go:384` — `if err := t.validateLoopTokens(); err != nil {`

Every other payload type carrying a loop-token field, and its `Validate()`, pinned at the check line (or
the line proving the check stops at non-emptiness, or that no shape check exists at all):

- `agentic/user_types.go:124` — `func (s UserSignal) Validate() error {`
- `agentic/user_types.go:134` — `if s.LoopID == "" {`

`UserSignal.Validate` checks only `s.LoopID == ""` (line above); no `looptoken.Valid` call anywhere in the
function body (lines 124-141). (Brief's line estimate of ~:124 for `UserSignal` re-pinned exactly.)

- `agentic/approval.go:122` — `func (r *ApprovalResponse) Validate() error {`
- `agentic/approval.go:123` — `if r.LoopID == "" {`

`ApprovalResponse.Validate` checks only `r.LoopID == ""`; no `looptoken.Valid` call. This type lives in
`agentic/approval.go`, not `agentic/user_types.go` where the brief's prior-commit estimate placed it —
re-pinned at the correct file.

- `agentic/approval.go:66` — `func (e *ApprovalPendingEvent) Validate() error {`
- `agentic/approval.go:67` — `if e.LoopID == "" {`

`ApprovalPendingEvent.Validate` checks only `e.LoopID == ""`; no `looptoken.Valid` call. (Not named in the
brief; found via the same struct-field sweep — an outbound event, not an intake path, but it carries the
same field.)

- `agentic/user_types.go:65` — `func (m UserMessage) Validate() error {`

`UserMessage.Validate` (body lines 66-81) checks `MessageID`, `ChannelType`, `ChannelID`, `UserID`,
`Content`/`Attachments`. It never references `ReplyTo`, `RunID`, or `InReplyTo` — no non-emptiness check, no
shape check, no check at all on any of the three loop-token-carrying fields this type holds.

- `processor/agentic-dispatch/loop_tracker.go:594` — `func (s *SignalMessage) Validate() error {`
- `processor/agentic-dispatch/loop_tracker.go:595` — `return nil`

`SignalMessage` (agentic-dispatch's own control-signal type, distinct from `agentic.UserSignal`) carries a
`LoopID` field (`loop_tracker.go:587`) and its `Validate()` is an unconditional `return nil` — no check of
any kind, not even non-emptiness.

- `processor/agentic-dispatch/http.go:606` — `loopID := r.PathValue("id")`
- `processor/agentic-dispatch/http.go:646` — `loopID := r.PathValue("id")`
- `processor/agentic-dispatch/http.go:647` — `if loopID == "" {`
- `processor/agentic-dispatch/http.go:743` — `loopID := r.PathValue("id")`
- `processor/agentic-dispatch/http.go:744` — `if loopID == "" {`

Three HTTP handlers (`handleGetLoop` at 606, `handleLoopSignal` at 646-647, `handleLoopApproval` at
743-744) take the loop token from the URL path, not a struct field or JSON tag. Both `handleLoopSignal` and
`handleLoopApproval` check only non-emptiness (400 if empty), then existence via
`c.loopTracker.Get(loopID)` (404 if not found). `internal/looptoken.Valid` is never called on this value at
either handler — a spelling neither the `#1228` issue body nor the current `entity-id-contract` spec text
(Adjacent claims, below) names explicitly.

- `processor/agentic-dispatch/http.go:32` — `type HTTPMessageRequest struct {`
- `processor/agentic-dispatch/http.go:37` — `ReplyTo     string            `json:"reply_to,omitempty"``
- `processor/agentic-dispatch/http.go:45` — `RunID     string `json:"run_id,omitempty"``
- `processor/agentic-dispatch/http.go:46` — `InReplyTo string `json:"in_reply_to,omitempty"``

`HTTPMessageRequest` carries `ReplyTo`, `RunID`, `InReplyTo`; no `Validate()` method exists on this type
(confirmed: `grep -n '^type \|^func (.*) Validate(' processor/agentic-dispatch/http.go` has no
`HTTPMessageRequest` receiver among its hits). These fields are copied onto a `UserMessage` and only then
reach a check, at `refuseNonCanonicalLoopTokens` — see Consumers.

## Spellings of the fact

Where loop-token shape is computed, and each of the four enforced seams:

- `internal/looptoken/looptoken.go:34` — `if len(s) != 36 {`

The whole check: 36-byte length, `uuid.Parse`, round-trip equality against the canonical re-render (doc
comment, same file, explains why: `uuid.Parse` alone also accepts uppercase/braced/`urn:uuid:` spellings).
Version bits are not read.

- `agentic/agentrun/agentrun.go:303` — `return nil, semerrs.WrapInvalid(`

`agentrun.Mint` refuses a non-canonical `rootLoopID` (the line above the shape check) before building the
entity ID.

- `agentic/user_types.go:409` — `func (t TaskMessage) validateLoopTokens() error {`
- `agentic/user_types.go:414` — `{"loop_id", t.LoopID},`
- `agentic/user_types.go:415` — `{"parent_loop_id", t.ParentLoopID},`
- `agentic/user_types.go:416` — `{"in_reply_to", t.InReplyTo},`
- `agentic/user_types.go:417` — `{"run_id", t.RunID},`
- `agentic/user_types.go:420` — `if token.value == "" {`

`validateLoopTokens` iterates all four `TaskMessage` token fields, skips empty ones (line 420-422), calls
`looptoken.Valid` on the rest (line 423, pinned under Claimed gap).

- `processor/agentic-dispatch/component.go:885` — `func (c *Component) refuseNonCanonicalLoopTokens(msg agentic.UserMessage, loopID string) error {`
- `processor/agentic-dispatch/component.go:891` — `{"run_id", msg.RunID},`
- `processor/agentic-dispatch/component.go:892` — `{"in_reply_to", msg.InReplyTo},`

`refuseNonCanonicalLoopTokens` iterates `{reply_to: loopID, run_id: msg.RunID, in_reply_to: msg.InReplyTo}`
— three fields. `TaskMessage.ParentLoopID` is not among them; `UserMessage` (this function's parameter type)
has no `ParentLoopID` field at all, and dispatch's `buildTaskMessage` never sets `TaskMessage.ParentLoopID`
from user input (confirmed: `git grep -n "ParentLoopID" processor/agentic-dispatch/*.go` — every hit is a
wire/read-side type, never a write from `UserMessage`), so the field gap does not appear to be reachable
through either submission path.

- `processor/agentic-loop/state.go:151` — `func (m *LoopManager) CreateLoopWithID(loopID, taskID, role, model string, maxIterations ...int) (string, error) {`

`CreateLoopWithID` refuses (line 152, pinned under Claimed gap) before the map write that would otherwise
overwrite an existing loop's record and context manager on a collision (doc comment, same file).

No hand-rolled shape check (regex, length-only, prefix comparison) other than the two HTTP path-param
handlers above was found: `git grep -n "regexp.*[Ll]oop"` — 0 hits repo-wide.

## Adjacent claims

- `openspec/specs/entity-id-contract/spec.md:651` — `### Requirement: A loop instance token is a framework-minted UUID`
- `openspec/specs/entity-id-contract/spec.md:659` — `Enforcement is FORM, not provenance`
- `openspec/specs/entity-id-contract/spec.md:663` — `possession of a loop token confers control of that loop to any holder`
- `openspec/specs/entity-id-contract/spec.md:672` — `MUST refuse a task carrying ANY loop-token field`
- `openspec/specs/entity-id-contract/spec.md:687` — `Other payloads carrying a loop token`

Current wording (post-#1210 narrowing): "Exactly four seams enforce the form refusal" (line 659 area names
the four: `TaskMessage.Validate`, dispatch's `refuseNonCanonicalLoopTokens`, `LoopManager.CreateLoopWithID`,
`agentrun.Mint`). Line 663-664 (not independently pinned above; same paragraph as line 663) names the #1227
authorization gap in the same breath. Line 687-689 states verbatim: *"Other payloads carrying a loop token —
`UserSignal`, `ApprovalResponse`, and any control or query request whose census is not yet taken — validate
only non-emptiness and are OUTSIDE this requirement; extending the refusal to them is #1228."* This
inventory is that uncensused set; it adds the URL-path handlers (`handleLoopSignal`, `handleLoopApproval`)
and `SignalMessage` (which validates nothing, not even non-emptiness) to what the spec calls "not yet
taken."

- `docs/adr/105-loop-instance-tokens-are-framework-minted-uuids.md:14` — `A loop instance token confers control of its loop to any holder`
- `docs/adr/105-loop-instance-tokens-are-framework-minted-uuids.md:26` — `Seams outside the four enforced ones`

ADR-105, Accepted 2026-09-01. The "Carve-out: a loop token is NOT an authorization token" section states the
#1227 gap in the same terms as the spec, and states the #1228 gap verbatim: *"Seams outside the four
enforced ones (`UserSignal`, `ApprovalResponse`, uncensused control requests) accept non-canonical tokens
today: #1228."*

- `docs/operations/migration-beta162-to-beta163.md:848` — `Loop tokens become full UUIDs (ADR-105, #1192)`
- `docs/operations/migration-beta162-to-beta163.md:874` — `Other loop-token carriers`
- `docs/operations/migration-beta162-to-beta163.md:906` — `refused by`

This document already carries the same #1228 disclosure ("Other loop-token carriers — `UserSignal`,
`ApprovalResponse`, and control requests not yet censused — still accept a non-canonical token; that is
#1228, not part of this wave," line 874-875) and, at line 906, an operational consequence this inventory's
Consumers section independently confirms below: a peer deployment's non-canonical `loop_xxxxxxxx`-shaped
token is refused by `task.Validate()` in the rule engine's `publish_agent` path.

- `openspec/specs/graph-ingest/spec.md:1060` — `MUST refuse a firing-loop instance token that is not in canonical UUID form`

`agentrun.Mint`'s refusal scenario, on the graph-write-path sibling surface (not re-read beyond this line).

- `openspec/changes/loop-scoped-request-seams/proposal.md` — this change's own proposal (investigation
  stage; no target state written); `openspec list` shows it as the only active change (0/7 tasks).
- #1225 — OPEN — "agentic-dispatch: a non-token TaskMessage.Validate failure is a silent drop — no
  response, no metric, and a leaked activeLoops gauge." Body pins `component.go:974-978` (channel-path
  marshal-failure bare return), `component.go:957`/`:971` (`Track`/`recordLoopStarted`, before the marshal),
  `http.go:351-363` (HTTP path's generic non-field-naming error). Body states the loop-token class is
  already gated ahead of this by `refuseNonCanonicalLoopTokens`; what remains reaching this branch is the
  non-token class (`Model`, `Prompt`, `Role` empty).
- #1226 — OPEN — "rule/expression: isValidEntityID is a hand-rolled reimplementation..." — entity-ID
  grammar, a different predicate from the loop-token one; adjacent, not this surface.
- #1227 — OPEN — "agentic-dispatch: continuing a loop by reply_to has no ownership check..." — the
  sibling explorer's axis (ownership/lifecycle); crossed above only where the spec/ADR text ties it to the
  same requirement paragraph as #1228.
- #1228 — OPEN — "agentic: loop-token carriers outside the four enforced seams accept non-canonical tokens
  (UserSignal, ApprovalResponse, control requests)" — this file's primary claimed gap.
- #1230 — OPEN — "process: a landing task cannot tick itself..." — process/tooling, not this surface.
- PR #1159 — OPEN, draft, `codex/gh1146-agentic-loop-restart`, "fix(agentic-loop): preserve durable work
  across process restart" — not read beyond title/branch (sibling explorer's restart-rehydration axis).
- PR #1231 — OPEN, `claude/gh1227-loop-seams` (this branch), "spec(loop-seams): settle whether
  #1227/#1228/#1225 share one missing primitive" — this inventory's own PR.

## Consumers

`agentic.UserSignal` — constructed at `processor/agentic-dispatch/commands.go:112`:

- `processor/agentic-dispatch/commands.go:112` — `signal := agentic.UserSignal{`
- `processor/agentic-dispatch/commands.go:127` — `subject, err := component.ResolveSubject(c.outputPortDefs(), "agent.signal", targetLoopID)`
- `processor/agentic-dispatch/config.go:139` — `Name: "agent.signal", Config: component.JetStreamPort{Subjects: []string{"agent.signal.*"}, StreamName: "AGENT"}, Description: "Agent control signals",`
- `processor/agentic-loop/component.go:2070` — `func (c *Component) handleSignalMessage(ctx context.Context, data []byte) {`
- `processor/agentic-loop/component.go:2077` — `signalPtr, ok := baseMsg.Payload().(*agentic.UserSignal)`

Published on `agent.signal.<loopID>`. Decoded and cast at `handleSignalMessage`; `Validate()` is never
called anywhere in that function's body (no `.Validate()` call appears between lines 2070 and its
`switch signal.Type` dispatch, and no call site targeting `agentic.UserSignal.Validate` appears anywhere in
a whole-repo, non-test `.Validate()` call-site sweep). `signal.LoopID` flows unchecked into
`handleCancelSignal`/`handlePauseSignal`/`handleResumeSignal`.

`agentic.TaskMessage` — constructed by `processor/agentic-dispatch/component.go`'s `buildTaskMessage` (user
submissions) and by `processor/rule/actions.go`'s `publishAgentOnce` (rule-engine `publish_agent` spawns):

- `processor/agentic-dispatch/component.go:981` — `subject, err := component.ResolveSubject(c.outputPortDefs(), "agent.task", taskID)`
- `processor/agentic-dispatch/config.go:136` — `Name: "agent.task", Config: component.JetStreamPort{Subjects: []string{"agent.task.*"}, StreamName: "AGENT"}, Description: "Agent task requests",`
- `processor/agentic-loop/component.go:1161` — `func (c *Component) handleTaskMessage(ctx context.Context, data []byte) error {`
- `processor/agentic-loop/component.go:1173` — `related, hasLineage, err := c.preflightDecodedTask(task)`
- `processor/agentic-loop/component.go:1299` — `func (c *Component) preflightDecodedTask(task *agentic.TaskMessage) (map[string]any, bool, error) {`
- `processor/agentic-loop/component.go:1300` — `if err := task.Validate(); err != nil {`
- `processor/agentic-loop/component.go:1176` — `c.metrics.recordTaskIntakeRejection(taskIntakeRejectionLane, taskIntakeRejectionReason)`
- `processor/agentic-loop/component.go:1178` — `return natsclient.TerminateDelivery(err)`

Published on `agent.task.<taskID>`. Decoded at `handleTaskMessage`, which calls `preflightDecodedTask` →
`task.Validate()`. On error, `handleTaskMessage` (lines 1174-1179) both increments
`recordTaskIntakeRejection` (a counter — outcome b) and returns `natsclient.TerminateDelivery(err)` (a
classified terminal NAK at the JetStream layer — outcome a-adjacent, though it is not a per-field typed
response to the original submitter's channel/HTTP client, which has already returned by this point).

- `processor/rule/actions.go:1885` — `if err := task.Validate(); err != nil {`
- `processor/rule/stateful_evaluator.go:437` — `e.logger.Error("Failed to execute action",`
- `processor/rule/stateful_evaluator.go:448` — `e.metrics.actionFailuresTotal.WithLabelValues(action.Type).Inc()`

The rule engine's `publish_agent` path calls `task.Validate()` inside `publishAgentOnce`
(`processor/rule/actions.go:1885`); the migration doc (Adjacent claims) independently confirms the error
propagates up to the action-executor's generic failure handling: a loud ERROR log ("Failed to execute
action") plus the `actionFailuresTotal{action_type="publish_agent"}` counter — outcomes (a)-adjacent (loud,
not silent) and (b) (counted), not a typed per-field response to any external caller (the rule engine has no
synchronous caller waiting on a response).

`agentic.ApprovalResponse` — constructed at `processor/agentic-dispatch/http.go`'s `publishApprovalResponse`
(called from `handleLoopApproval`) from the HTTP `ApprovalRequest` body plus the path `loopID`:

- `processor/agentic-dispatch/config.go:148` — `Name: "agent.approval_response", Config: component.JetStreamPort{Subjects: []string{"agent.approval_response.*"}, StreamName: "AGENT"}, Description: "Approval responses submitted via the dispatch HTTP /loops/{id}/approval endpoint, consumed by agentic-loop's approval-response handler",`
- `processor/agentic-loop/approval_response_handler.go:156` — `baseMsg, err := c.decoder.Decode(data)`
- `processor/agentic-loop/approval_response_handler.go:51` — `if vErr := response.Validate(); vErr != nil {`
- `processor/agentic-loop/approval_response_handler.go:175` — `result, err := c.handler.HandleApprovalResponse(ctx, response)`
- `processor/agentic-loop/approval_response_handler.go:177` — `c.logger.Error("Failed to handle approval response",`
- `processor/agentic-loop/approval_response_handler.go:181` — `return`
- `processor/agentic-loop/approval_sweeper.go:86` — `result, err := c.handler.HandleApprovalResponse(ctx, response)`

Published on `agent.approval_response.<loopID>`. Decoded at `handleApprovalResponseMessage`
(`approval_response_handler.go:156` region), which calls `HandleApprovalResponse` →
`response.Validate()` (line 51), returning a wrapped `errs.WrapInvalid` error on a non-emptiness failure.
The caller (`handleApprovalResponseMessage`, lines 175-182) logs the error (line 177) and bare-returns (line
181) — no metric call and no response in this branch: a silent-drop shape matching #1225's class, found
independently here for `ApprovalResponse` rather than `TaskMessage`. Also consumed at
`approval_sweeper.go:86` (timeout-driven auto-reject path — not traced further).

`agentic.ApprovalPendingEvent` — published by agentic-loop:

- `processor/agentic-dispatch/config.go:126` — `Name: "agent.approval_pending", Config: component.JetStreamPort{Subjects: []string{"agent.approval_pending.*"}, StreamName: "AGENT"}, Required: false,`

On `agent.approval_pending.<loopID>`; consumed by product-layer approval UIs per its doc comment
(`agentic/approval.go:39-44`) — outside this repo, not traced.

`processor/agentic-dispatch/loop_tracker.go:587` `SignalMessage` — a second, distinct control-signal type:

- `agentic/payload_registry.go:36` — `{Domain: Domain, Category: CategorySignal, Version: SchemaVersion, Description: "User control signal", Factory: func() any { return &UserSignal{} }, IndexingProfile: signal},`
- `processor/agentic-dispatch/payload_registry.go:13` — `msg := &SignalMessage{}`
- `processor/agentic-dispatch/payload_registry.go:32` — `if err := msg.Validate(); err != nil {`

`agentic.UserSignal` is registered under `CategorySignal` (the type actually decoded at the `agent.signal.*`
intake above). `SignalMessage` is registered separately, in `processor/agentic-dispatch/payload_registry.go`,
under `CategorySignalMessage` (not independently grepped — see Searches, NOT RUN); its `Builder`
(`buildSignalMessage`) calls `msg.Validate()` at line 32, which — per the Claimed gap section — always
returns nil, so this call can never actually reject anything.

`processor/agentic-dispatch/loop_wire.go:14` `type Loop struct` and `:105` `type completionWire struct` —
read/reflection-shaped wire types, not an intake-of-untrusted-input path:

- `processor/agentic-dispatch/loop_wire.go:14` — `type Loop struct {`
- `processor/agentic-dispatch/loop_wire.go:105` — `type completionWire struct {`

Both carry `LoopID`/`ParentLoopID`/`RunID` JSON tags (`git grep` hits under Claimed-gap-adjacent struct
sweeps) but neither has a `Validate()` method (same `grep -n '^type \|^func (.*) Validate('` sweep as
`HTTPMessageRequest`, above). `Loop` is the KV-projected view behind `ActivityEvent`/SSE; `completionWire`
decodes `agent.complete.*` completion events. Noted for completeness only.

### Gauge/counter increments before validation (#1225 axis)

- `processor/agentic-dispatch/metrics.go:16` — `activeLoops         prometheus.Gauge`
- `processor/agentic-dispatch/metrics.go:304` — `func (m *routerMetrics) recordLoopStarted() {`
- `processor/agentic-dispatch/metrics.go:305` — `m.activeLoops.Inc()`
- `processor/agentic-dispatch/metrics.go:309` — `func (m *routerMetrics) recordLoopEnded() {`
- `processor/agentic-dispatch/metrics.go:310` — `m.activeLoops.Dec()`
- `processor/agentic-dispatch/terminal_settlement.go:203` — `c.metrics.recordLoopEnded()`

Subsystem `router`'s `activeLoops` gauge. `recordLoopEnded` (the only `Dec()`) has exactly one call site:
`terminal_settlement.go:203`, which fires on a terminal completion event received back from agentic-loop
(`agent.complete.*` — not traced further, sibling lifecycle surface). A loop that is counted as started here
but never produces a terminal event on that stream has no decrement path.

- `processor/agentic-dispatch/component.go:931` — `if err := c.refuseNonCanonicalLoopTokens(msg, loopID); err != nil {`
- `processor/agentic-dispatch/component.go:966` — `ContextRequestID: msg.ContextRequestID,`
- `processor/agentic-dispatch/component.go:971` — `c.metrics.recordLoopStarted()`
- `processor/agentic-dispatch/component.go:976` — `if err != nil {`
- `processor/agentic-dispatch/component.go:977` — `c.logger.Error("Failed to marshal task", slog.String("error", err.Error()))`
- `processor/agentic-dispatch/component.go:978` — `return`

Channel-path ordering: the loop-token check (line 931) runs before `loopTracker.Track` (line ~957,
containing the `ContextRequestID` field at 966) and before `recordLoopStarted` (line 971) — a loop-token
rejection never reaches the gauge increment. The marshal step (line 975, `json.Marshal`) runs *after* both;
on marshal failure the handler logs (977) and bare-returns (978) — no counter call, no response — leaving
the gauge (already incremented at 971) with no decrement path on this branch. This is #1225's claim,
reproduced by pin at this base.

- `processor/agentic-dispatch/http.go:336` — `c.loopTracker.Track(&LoopInfo{`
- `processor/agentic-dispatch/http.go:349` — `c.metrics.recordLoopStarted()`
- `processor/agentic-dispatch/http.go:353` — `taskData, err := json.Marshal(baseMsg)`
- `processor/agentic-dispatch/http.go:361` — `Type:        agentic.ResponseTypeError,`

Same ordering on the HTTP path: `Track` (336) then `recordLoopStarted` (349) then `json.Marshal` (353); on
marshal failure the handler returns a typed `agentic.UserResponse{Type: ResponseTypeError, ...}` (line
361 area, content "Failed to create task. Please try again.") — not silent, but names no field, and the
gauge is still not decremented on this branch.

- `processor/agentic-loop/metrics.go:19` — `activeLoops    prometheus.Gauge`
- `processor/agentic-loop/metrics.go:421` — `m.activeLoops.Inc()`

A second, separate `activeLoops` gauge, subsystem `agentic-loop`. Its increment (line 421) sits inside
`loopMetrics`; the exact calling function was not traced within budget (see Searches, NOT RUN). Because
`preflightDecodedTask`/`task.Validate()` (`component.go:1300`) runs before `HandleTask` in
`handleTaskMessage`, this second gauge is not exposed to the loop-token-shape failure mode the way the
dispatch-side gauge is.

## Searches

- `git grep -n "func Valid" internal/looptoken/` → 1
- `git grep -n "^func " internal/looptoken/` → 1
- `ls internal/looptoken/` → 1 file (`looptoken.go`)
- `gopls references internal/looptoken/looptoken.go:33:6` → 4
- `git grep -n "looptoken\." -- '*.go'` (excluding `_test.go`) → 4
- `git grep -n "looptoken\." -- '*_test.go'` → 3
- `git grep -n "LoopID\s\+string" -- '*.go'` (excluding `_test.go`) → 39
- `git grep -n "ReplyTo\s\+string\|InReplyTo\s\+string\|ParentLoopID\s\+string\|RunID\s\+string" -- '*.go'` (excluding `_test.go`) → 27
- `git grep -n 'json:"loop_id\|json:"reply_to\|json:"in_reply_to\|json:"run_id\|json:"parent_loop_id' -- '*.go'` (excluding `_test.go`) → 47
- `grep -n "^type \|^func (.*) Validate(" agentic/user_types.go` → 9
- `grep -n "^type \|^func (.*) Validate(" agentic/approval.go processor/agentic-dispatch/http.go processor/agentic-dispatch/loop_wire.go` → 12
- `git grep -n "refuseNonCanonicalLoopTokens" -- '*.go'` → 4
- `git grep -n "\.Validate()" -- 'processor/agentic-dispatch/*.go' 'agentic/*.go'` (excluding `_test.go`) → 14
- `git grep -n "\.Validate()" -- '*.go'` (excluding `_test.go`, whole repo) → ~140 (full list captured; only
  the loop-token-carrier-relevant subset is cited above by file:line)
- `git grep -n "type SignalMessage\|^func.*SignalMessage.*Validate" -- '*.go'` → 2
- `git grep -n "HandleApprovalResponse(" -- '*.go'` (excluding `_test.go`) → 3
- `git grep -n "handleSignalMessage\|SignalMessage{" -- '*.go'` (excluding `_test.go`) → 7
- `grep -n "func.*handleSignalMessage" -A 40 processor/agentic-dispatch/component.go` → 0 (function lives in
  `processor/agentic-loop/component.go`, not `agentic-dispatch`; wasted search, recorded for honesty)
- `git grep -n "c.decoder\b" -- 'processor/agentic-loop/*.go'` (excluding `_test.go`) → 5
- `git grep -n "decoder\s*.*Decoder\|type.*Decoder" -- 'message/*.go' 'processor/agentic-loop/*.go'` (excluding `_test.go`) → 3
- `git grep -n "UserSignal" -- '*.go'` (excluding `_test.go`) → 19
- `git grep -n "preflightDecodedTask" -- '*.go'` → 6 (1 def + 1 production call + 4 test hits)
- `grep -n "handleTaskMessageWithLifecycle\|func (c \*Component) handleTaskMessage" processor/agentic-loop/component.go` → 5
- `git grep -n "activeLoops\|recordLoopStarted\|recordLoopCompleted\|recordLoopFailed" -- 'processor/agentic-dispatch/*.go'` (excluding `_test.go`) → 12
- `git grep -n "activeLoops\|recordLoopStarted\|recordLoopCompleted\|recordLoopFailed\|recordTaskIntakeRejection" -- 'processor/agentic-loop/*.go'` (excluding `_test.go`) → 13
- `git grep -n "activeLoops\.\(Dec\|Inc\)\|m\.activeLoops" -- 'processor/agentic-dispatch/*.go'` (excluding `_test.go`) → 4
- `git grep -n "recordLoopEnded" -- '*.go'` (excluding `_test.go`) → 2
- `git grep -n "ParentLoopID" -- 'processor/agentic-dispatch/*.go'` (excluding `_test.go`) → 8
- `grep -n "func (c \*Component) handleSignal\|func (c \*Component) handleApproval" processor/agentic-dispatch/http.go` → 0 (names differ: `handleLoopSignal`/`handleLoopApproval`; wasted search, recorded)
- `git grep -n "type ControlRequest\|ControlRequest{" -- '*.go'` → 0 (no type by this name exists; the
  proposal's "control requests" phrase maps to `SignalRequest`/`ApprovalRequest`/the URL-path handlers above)
- `grep -n "^func (c \*Component) handle" processor/agentic-dispatch/http.go` → 8
- `grep -n 'loopID := r.PathValue("id")' processor/agentic-dispatch/http.go` → 3
- `git grep -n "entity-id-contract\|loop.*token" openspec/specs/` → 10 (sample; not exhaustive)
- `ls docs/adr/ | grep -i 105` → 1
- `ls docs/operations/ | grep -i beta162` → 1
- `git grep -n "loop.*token\|looptoken\|ADR-105" -- docs/operations/migration-beta162-to-beta163.md` → 9
  (an earlier run of this same search in-session mis-reported 0 hits due to a missing `--` separator;
  re-run and corrected here — the file has a full "Loop tokens become full UUIDs" section, lines 848-911)
- `gh issue view 1225/1226/1227/1228/1230 --json title,state` → 5 (all OPEN)
- `gh pr view 1159/1231 --json title,state,headRefName` → 2 (both OPEN)
- `gh issue view 1225 --json body` → 1 (body read, pinned above)
- `openspec list` → 1 change (`loop-scoped-request-seams`, this one)
- `git grep -n '"agent\.\(task\|signal\|approval_response\|approval_pending\|complete\)' -- '*.go'` (excluding `_test.go`, filtered to subject/subscribe lines) → 17
- `git grep -n "AGENT_LOOPS" -- '*.go'` (excluding `_test.go`, filtered to bucket/const lines) → 11
- `grep -n "actionFailuresTotal\|Failed to execute action" processor/rule/*.go` (excluding `_test.go`) → 7
- `git grep -n "regexp.*[Ll]oop" -- '*.go'` (excluding `_test.go`) → 0
- `git grep -n "CategorySignalMessage"` → NOT RUN — cited in Consumers as a claim, not independently searched
- Exact caller of `processor/agentic-loop/metrics.go:421` `activeLoops.Inc()` → NOT RUN — grep hit only, not
  traced to its calling function
- `agentic/user_types_test.go` token-literal fixtures the brief named (~:143-154) → NOT RUN — located by the
  `looptoken\.` test grep above (0 direct hits for that file specifically; the file was not independently
  grepped for `LoopID: "loop-` style literals) — the brief's named line range was not re-opened
- `frameworkcapabilities/graphresearch/executor_test.go:506` / `processor/agentic-dispatch/loop_token_test.go:57,70` → located via the `looptoken\.` test-file grep above; contents not read beyond the grep line
- `processor/agentic-dispatch/http.go` `handleHTTPMessage`'s exact construction of `UserMessage` from
  `HTTPMessageRequest` → NOT RUN — referenced by line range only (`http.go:311` call site), field-by-field
  copy not read
- Deeper trace of `agentic/agentrun/agentrun.go` `RunID`/`ParentLoopID` consumers past `Mint` → NOT RUN —
  out of budget

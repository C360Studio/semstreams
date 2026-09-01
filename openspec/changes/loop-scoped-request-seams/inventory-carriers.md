# Inventory: every carrier of a loop token, and what happens when its shape is wrong

base: 24ab736e4a0325736a9a7f4b3fe6f0941c675c92

Scope: the #1228 axis (form validation census) + #1225 (Validate failure dropped silently, gauge leak).
LoopTracker internals, permissions/ownership, and restart/rehydration behaviour are a sibling explorer's
surface — crossed only where unavoidable to trace an outcome, flagged under Adjacent claims.

## Claimed gap

- `internal/looptoken/looptoken.go:33` — `func Valid(s string) bool {` — the one exported symbol in the
  package; re-derived via `gopls references` at this base: exactly 4 production callers (matches the
  issue's claimed count):
  - `agentic/agentrun/agentrun.go:302` — `if !looptoken.Valid(rootLoopID) {`
  - `agentic/user_types.go:423` — `if !looptoken.Valid(token.value) {` (inside `TaskMessage.validateLoopTokens`)
  - `processor/agentic-dispatch/component.go:894` — `if token.value == "" || looptoken.Valid(token.value) {` (inside `refuseNonCanonicalLoopTokens`)
  - `processor/agentic-loop/state.go:152` — `if !looptoken.Valid(loopID) {` (inside `LoopManager.CreateLoopWithID`)
- `agentic/user_types.go:358` — `func (t TaskMessage) Validate() error {` — calls `validateLoopTokens()` (line 384), which is the
  `agentic/user_types.go:423` call site above. Checks all four TaskMessage token fields: `loop_id`,
  `parent_loop_id`, `in_reply_to`, `run_id` (fields enumerated at `agentic/user_types.go:414-417`).
- `agentic/user_types.go:124` — `func (s UserSignal) Validate() error {` — checks `s.LoopID == ""` only
  (line 134-136); no `looptoken.Valid` call anywhere in this function.
- `agentic/approval.go:122` — `func (r *ApprovalResponse) Validate() error {` — checks `r.LoopID == ""` only
  (line 123-125); no `looptoken.Valid` call anywhere in this function. (Brief said this type lives in
  `agentic/user_types.go` at a prior-commit line number; re-pinned — it is in `agentic/approval.go`, not
  `user_types.go`, at this base.)
- `agentic/approval.go:66` — `func (e *ApprovalPendingEvent) Validate() error {` — checks `e.LoopID == ""` only
  (line 67-69); no `looptoken.Valid` call.
- `agentic/user_types.go:65` — `func (m UserMessage) Validate() error {` — checks `MessageID`, `ChannelType`,
  `ChannelID`, `UserID`, `Content`/`Attachments`; does **not** reference `ReplyTo`, `RunID`, or `InReplyTo`
  anywhere in the function body (lines 66-81) — no non-emptiness check, no shape check, no check at all.
- `processor/agentic-dispatch/loop_tracker.go:594` — `func (s *SignalMessage) Validate() error {` — body is
  `return nil` (line 595) unconditionally; `SignalMessage` (`loop_tracker.go:587`) carries `LoopID` and this
  method never inspects it. This is `agentic-dispatch`'s own control-signal type, distinct from
  `agentic.UserSignal`.
- `processor/agentic-dispatch/http.go:606,646,743` — `loopID := r.PathValue("id")` — three HTTP handlers
  (`handleGetLoop`, `handleLoopSignal`, `handleLoopApproval`) extract the loop token from the URL path with
  no struct field or JSON tag at all. `handleLoopSignal` (line 647-650) and `handleLoopApproval` (line
  744-747) check only `loopID == ""` (400) then existence via `c.loopTracker.Get(loopID)` (404 at
  line 654-658 / 751-755). `internal/looptoken.Valid` is never called on this value at either handler — a
  spelling neither `#1228`'s issue body nor the current `entity-id-contract` spec text (below) names.
- `processor/agentic-dispatch/http.go:32` — `type HTTPMessageRequest struct` — carries `ReplyTo`, `RunID`,
  `InReplyTo` (lines 37,45,46); no `Validate()` method exists on this type (confirmed by
  `grep -n '^type \|^func (.*) Validate(' processor/agentic-dispatch/http.go`, 0 matches for
  `HTTPMessageRequest`) — its fields reach `refuseNonCanonicalLoopTokens` only after being copied onto a
  `UserMessage` (`http.go:311`).

## Spellings of the fact

- `internal/looptoken/looptoken.go:21-42` — the one place shape is computed: 36-byte length check +
  `uuid.Parse` + round-trip-equality (`parsed.String() == s`), form only, version bits unchecked (doc
  comment lines 30-32).
- `agentic/agentrun/agentrun.go:302-306` — `agentrun.Mint` refuses a non-canonical `rootLoopID` before
  building the entity ID (`errRootLoopIDNotCanonical`).
- `agentic/user_types.go:409-430` — `TaskMessage.validateLoopTokens`: iterates `{loop_id, parent_loop_id,
  in_reply_to, run_id}`, skips empty (line 420-422), calls `looptoken.Valid` on the rest (line 423).
- `processor/agentic-dispatch/component.go:885-899` — `refuseNonCanonicalLoopTokens`: iterates `{reply_to:
  loopID, run_id: msg.RunID, in_reply_to: msg.InReplyTo}` — **does not include `ParentLoopID`** (`UserMessage`
  has no such field; only `TaskMessage` does, and dispatch's `buildTaskMessage` never sets it from user
  input — confirmed by `git grep -n "ParentLoopID" processor/agentic-dispatch/*.go`, only wire/read-side
  hits, no write from `UserMessage`).
- `processor/agentic-loop/state.go:150-158` — `CreateLoopWithID` refuses before `m.mu.Lock()` / map write
  (doc comment lines 145-150 explains why: the write below overwrites an existing record).
- `agentic/user_types_test.go` — test fixtures asserting token literals valid/invalid: re-pinned search
  below; not read line-by-line beyond the grep hit (budget).
- `frameworkcapabilities/graphresearch/executor_test.go:506` and `processor/agentic-dispatch/loop_token_test.go:57,70`
  — comments referencing `looptoken.Valid`'s version-bit-agnostic behaviour and a separate v4 assertion in
  tests; not traced further (out of the 4-seam production surface).
- No hand-rolled shape check (regex, length-only, prefix check) other than the URL-path handlers above (no
  form check at all, not even a home-rolled one) was found; searched via `git grep -n "len(.*loopID\|regexp.*[Ll]oop"` — see Searches (0 hits beyond looptoken.go itself).

## Adjacent claims

- `openspec/specs/entity-id-contract/spec.md:651-687` — **Requirement: A loop instance token is a
  framework-minted UUID.** Current wording (post-#1210 narrowing) names "Exactly four seams" (line 659) and
  lists them (lines 665-679: `TaskMessage.Validate`, `LoopManager.CreateLoopWithID`, dispatch's
  `refuseNonCanonicalLoopTokens`, `agentrun.Mint`). Line 663-664 states the #1227 authorization gap
  explicitly. Line 687-689 states: *"Other payloads carrying a loop token — `UserSignal`,
  `ApprovalResponse`, and any control or query request whose census is not yet taken — validate only
  non-emptiness and are OUTSIDE this requirement; extending the refusal to them is #1228."* — this
  inventory is that uncensused set; it adds the URL-path handlers (`handleLoopSignal`,
  `handleLoopApproval`) and `SignalMessage` (which validates nothing, not even non-emptiness) to the set the
  spec calls "not yet taken."
- `docs/adr/105-loop-instance-tokens-are-framework-minted-uuids.md:1-24` — Accepted 2026-09-01. "Carve-out:
  a loop token is NOT an authorization token" section (lines 9-17) states the #1227 gap in the same terms as
  the spec; line 22 states the #1228 gap: *"Seams outside the four enforced ones (`UserSignal`,
  `ApprovalResponse`, uncensused control requests) accept non-canonical tokens today: #1228."*
- `docs/operations/migration-beta162-to-beta163.md` — searched for `loop.*token`/`looptoken`/`ADR-105`:
  0 hits (see Searches) — not yet updated for this contract.
- `openspec/specs/graph-ingest/spec.md:1060-1156` — `agentrun.Mint` refusal scenario, references the
  entity-id-contract loop-token contract; not re-read beyond the grep hit (sibling surface: graph write
  path, not intake).
- `openspec/changes/loop-scoped-request-seams/proposal.md` — this change's own proposal (investigation
  stage; no target state written).
- #1225 — OPEN — "agentic-dispatch: a non-token TaskMessage.Validate failure is a silent drop — no
  response, no metric, and a leaked activeLoops gauge." Body pins `component.go:974-978` (channel-path
  marshal-failure bare return), `component.go:957`/`:971` (`Track`/`recordLoopStarted`, before the marshal),
  `http.go:351-363` (HTTP path's generic non-field-naming error). Body states the loop-token class is
  already gated ahead of this by `refuseNonCanonicalLoopTokens`; what remains is the non-token class
  (`Model`, `Prompt`, `Role` empty).
- #1226 — OPEN — "rule/expression: isValidEntityID is a hand-rolled reimplementation..." — entity-ID
  grammar, not loop-token grammar; adjacent but a different predicate.
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

- `agentic.UserMessage` — constructed by `processor/agentic-dispatch/commands.go` (CLI/channel path) and
  from `HTTPMessageRequest` at `processor/agentic-dispatch/http.go:~280-311` (not read field-by-field
  beyond the `refuseNonCanonicalLoopTokens` call site). Consumed by
  `processor/agentic-dispatch/component.go:903` `handleTaskSubmission` and the HTTP equivalent in
  `http.go`. Travels in-process only (not marshaled to a NATS subject as `UserMessage` itself).
- `agentic.UserSignal` — constructed at `processor/agentic-dispatch/commands.go:112`. Published on
  `agent.signal.<loopID>` (subject built at `processor/agentic-dispatch/loop_tracker.go:634` and
  registered as port `agent.signal` with subject pattern `agent.signal.*`,
  `processor/agentic-dispatch/config.go:139` / `processor/agentic-loop/config.go:408`). Decoded and cast at
  `processor/agentic-loop/component.go:2077` inside `handleSignalMessage` (line 2070) — **`Validate()` is
  never called in this function** (confirmed: no `.Validate()` call appears in the function body read from
  line 2070 to the `switch signal.Type` at line 2090; also absent from the whole-repo `.Validate()` call-site
  grep). `signal.LoopID` flows unchecked into `handleCancelSignal`/`handlePauseSignal`/`handleResumeSignal`.
- `agentic.TaskMessage` — constructed by `processor/agentic-dispatch/component.go` `buildTaskMessage` (user
  submissions) and by `processor/rule/actions.go` `publishAgentOnce` (rule-engine `publish_agent` spawns,
  `ParentLoopID` set here for nested agents — not traced further, budget). Published on `agent.task.<taskID>`
  (port `agent.task`, `processor/agentic-dispatch/config.go:136`). Decoded at
  `processor/agentic-loop/component.go:1163` `handleTaskMessage`, which calls `preflightDecodedTask`
  (line 1173) → `task.Validate()` (line 1300).
- `agentic.ApprovalResponse` — constructed at `processor/agentic-dispatch/http.go` `publishApprovalResponse`
  (called from `handleLoopApproval`, line ~808) from the HTTP `ApprovalRequest` body + path `loopID`.
  Published on `agent.approval_response.<loopID>` (port `agent.approval_response`,
  `processor/agentic-dispatch/config.go:148`). Decoded at
  `processor/agentic-loop/approval_response_handler.go:156` `handleApprovalResponseMessage`, which calls
  `HandleApprovalResponse` (line 175) → `response.Validate()` (line 51). Also consumed by
  `processor/agentic-loop/approval_sweeper.go:86` (same `HandleApprovalResponse` call, timeout-driven
  auto-reject path — not traced further).
- `agentic.ApprovalPendingEvent` — published by agentic-loop on `agent.approval_pending.<loopID>` (port
  `agent.approval_pending`, `processor/agentic-dispatch/config.go:126`); consumed by product-layer approval
  UIs per its doc comment (`agentic/approval.go:39-44`) — outside this repo, not traced.
- `processor/agentic-dispatch/loop_tracker.go:587` `SignalMessage` — registered via
  `processor/agentic-dispatch/payload_registry.go` `RegisterPayloads` (category `CategorySignalMessage`);
  its `Builder` (`buildSignalMessage`, `payload_registry.go:13`) calls `msg.Validate()` (line 32), which
  always returns nil — a validation call that can never reject. No production decode-side consumer of this
  specific registered type was found within budget (search: `git grep -n "CategorySignalMessage"` — see
  Searches); `agentic.UserSignal` (a different type, same shape) is the one actually decoded at the
  `agent.signal.*` intake in `processor/agentic-loop/component.go:2070`.
- `processor/agentic-dispatch/http.go:14` `type Loop struct` / `:105` `type completionWire struct` — carry
  `LoopID`/`ParentLoopID`/`RunID` JSON tags but are read/reflection-shaped (KV-projected view for
  `ActivityEvent`/SSE, and `agent.complete.*` completion-event decode) — no `Validate()` method on either
  (confirmed by the same `grep -n '^type \|^func (.*) Validate('` sweep); not an intake-of-untrusted-input
  path in the sense the brief asks about, noted for completeness only.

### Gauge/counter increments before validation (#1225 axis)

- `processor/agentic-dispatch/metrics.go:16,103,242,265` — `activeLoops prometheus.Gauge` (subsystem
  `router`). Incremented (`Inc()`) at `metrics.go:305` via `recordLoopStarted()`; decremented (`Dec()`) at
  `metrics.go:310` via `recordLoopEnded()`. **Only one call site for `recordLoopEnded`**:
  `processor/agentic-dispatch/terminal_settlement.go:203` (fires on a terminal completion event received
  from agentic-loop — `agent.complete.*` — not traced further, sibling lifecycle surface).
- Increment-before-check ordering, channel path (`processor/agentic-dispatch/component.go`): permission
  check → determine `loopID` → `refuseNonCanonicalLoopTokens` (line 931, **before** tracking) → mint if
  empty → `buildTaskMessage` → `loopTracker.Track` (line 966) → `c.metrics.recordLoopStarted()` (line 971,
  **after** the loop-token check, so a loop-token-shape rejection never leaks this gauge) → `json.Marshal`
  (line 975) → on marshal error: `c.logger.Error(...); return` (lines 976-978) with **no counter call and
  no response** — the gauge, already incremented at line 971, is never decremented on this path (matches
  #1225's claim, still present at this base).
- Same ordering, HTTP path (`processor/agentic-dispatch/http.go`): `loopTracker.Track` (line 336) →
  `recordLoopStarted()` (line 349) → `json.Marshal` (line 352) → on marshal error (lines 353-364): returns a
  typed `agentic.UserResponse{Type: ResponseTypeError, Content: "Failed to create task. Please try again."}`
  (not silent, but names no field) — gauge still not decremented on this path.
- `processor/agentic-loop/metrics.go:19,104,275,303` — a **second, separate** `activeLoops` gauge (subsystem
  `agentic-loop`). Incremented at `metrics.go:421` (not traced to its exact caller within budget — inside
  `loopMetrics`, presumably `HandleTask`'s loop-creation success path, which runs only *after*
  `preflightDecodedTask`/`task.Validate()` succeeds at `component.go:1173`, so this second gauge is not
  exposed to the loop-token-shape failure mode). Decremented at `recordLoopCompleted` (`:427`),
  `recordLoopFailed` (`:435`), and one more site (`:443`, function name not captured — budget).

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
- `grep -n "^type \|^func (.*) Validate(" agentic/user_types.go` → 9 matches (4 types, 4 Validate methods, 1 alias line group)
- `grep -n "^type \|^func (.*) Validate(" agentic/approval.go processor/agentic-dispatch/http.go processor/agentic-dispatch/loop_wire.go` → 12
- `git grep -n "refuseNonCanonicalLoopTokens" -- '*.go'` → 4
- `git grep -n "\.Validate()" -- 'processor/agentic-dispatch/*.go' 'agentic/*.go'` (excluding `_test.go`) → 14
- `git grep -n "\.Validate()" -- '*.go'` (excluding `_test.go`, whole repo) → ~140 (full list captured; only
  the loop-token-carrier-relevant subset is cited above by file:line)
- `git grep -n "SignalMessage" ...` / `git grep -n "type SignalMessage\|^func.*SignalMessage.*Validate"` → 2
- `git grep -n "HandleApprovalResponse(" -- '*.go'` (excluding `_test.go`) → 3
- `git grep -n "handleSignalMessage\|SignalMessage{" -- '*.go'` (excluding `_test.go`) → 7
- `grep -n "func.*handleSignalMessage" -A 40 processor/agentic-dispatch/component.go` → 0 (function lives in
  `processor/agentic-loop/component.go`, not `agentic-dispatch`; wasted search, recorded for honesty)
- `git grep -n "c.decoder\b" -- 'processor/agentic-loop/*.go'` (excluding `_test.go`) → 5
- `git grep -n "decoder\s*.*Decoder\|type.*Decoder" -- 'message/*.go' 'processor/agentic-loop/*.go'` (excluding `_test.go`) → 3
- `git grep -n "UserSignal" -- '*.go'` (excluding `_test.go`) → 19
- `git grep -n "preflightDecodedTask" -- '*.go'` → 6 (1 production def + 1 call site + 4 test hits)
- `grep -n "handleTaskMessageWithLifecycle\|func (c \*Component) handleTaskMessage" processor/agentic-loop/component.go` → 5
- `git grep -n "activeLoops\|recordLoopStarted\|recordLoopCompleted\|recordLoopFailed" -- 'processor/agentic-dispatch/*.go'` (excluding `_test.go`) → 12
- `git grep -n "activeLoops\|recordLoopStarted\|recordLoopCompleted\|recordLoopFailed\|recordTaskIntakeRejection" -- 'processor/agentic-loop/*.go'` (excluding `_test.go`) → 13
- `git grep -n "activeLoops\.\(Dec\|Inc\)\|m\.activeLoops" -- 'processor/agentic-dispatch/*.go'` (excluding `_test.go`) → 4
- `git grep -n "recordLoopEnded" -- '*.go'` (excluding `_test.go`) → 2
- `git grep -n "ParentLoopID" -- 'processor/agentic-dispatch/*.go'` (excluding `_test.go`) → 8
- `grep -n "func (c \*Component) handleSignal\|func (c \*Component) handleApproval" processor/agentic-dispatch/http.go` → 0 (names differ: `handleLoopSignal`/`handleLoopApproval`; wasted search, recorded)
- `git grep -n "type ControlRequest\|ControlRequest{" -- '*.go'` → 0 (no type by this name exists; the
  proposal's "control requests" phrase maps to `SignalRequest`/`ApprovalRequest`/URL-path handlers above)
- `grep -n "^func (c \*Component) handle" processor/agentic-dispatch/http.go` → 8
- `grep -n 'loopID := r.PathValue("id")' processor/agentic-dispatch/http.go` → 3
- `git grep -n "entity-id-contract\|loop.*token" openspec/specs/` → 10 (sample; not exhaustive count)
- `ls docs/adr/ | grep -i 105` → 1
- `ls docs/operations/ | grep -i beta162` → 1
- `git grep -n "loop.*token\|looptoken\|ADR-105" docs/operations/migration-beta162-to-beta163.md` → 0
- `gh issue view 1225/1226/1227/1228/1230 --json title,state` → 5 (all OPEN)
- `gh pr view 1159/1231 --json title,state,headRefName` → 2 (both OPEN)
- `gh issue view 1225 --json body` → 1 (body read, pinned above)
- `sed -n '1,40p' docs/adr/105-...md` → 1 (read, pinned above)
- `openspec list` → 1 change (`loop-scoped-request-seams`, this one)
- `git grep -n '"agent\.\(task\|signal\|approval_response\|approval_pending\|complete\)' -- '*.go'` (excluding `_test.go`, filtered to subject/subscribe lines) → 17
- `git grep -n "AGENT_LOOPS" -- '*.go'` (excluding `_test.go`, filtered to bucket/const lines) → 11
- `git grep -n "CategorySignalMessage"` → NOT RUN (referenced in Consumers section as a claim to verify; not
  independently searched — recorded honestly as not run)
- `git grep -n "len(.*loopID\|regexp.*[Ll]oop"` (hand-rolled shape check sweep) → 0

## NOT RUN

- `git grep -n "CategorySignalMessage"` — cited in Consumers but not independently searched.
- Exact caller/line of `processor/agentic-loop/metrics.go:421` `activeLoops.Inc()` (which function invokes
  it) — not traced past the grep hit.
- Full read of `agentic/user_types_test.go` around the token-literal fixtures the brief named (~:143-154) —
  located by the `looptoken\.` test grep above but not opened; line range not re-pinned.
- `processor/rule/actions.go:1885` `task.Validate()` error propagation beyond `publishAgentOnce`'s own
  return — not traced into the rule engine's action-executor error handling (metric/log/response
  classification at that layer unproven).
- `processor/agentic-dispatch/http.go` `handleHTTPMessage`'s exact construction of `UserMessage` from
  `HTTPMessageRequest` (field-by-field) — referenced by line range only, not read.
- Deeper trace of `agentic/agentrun/agentrun.go` `RunID`/`ParentLoopID` consumers past `Mint` — out of
  budget; flagged, not enumerated.

## ADDED Requirements

### Requirement: Creating a loop that already exists is refused; a continuation attaches to it

`LoopManager.CreateLoopWithID` MUST refuse, rather than overwrite, when the loop token it is given already names
a registered loop. It currently writes the loop entity, the pending-tool set, and a freshly constructed context
manager into their maps unconditionally, which destroys the conversation of any loop already registered under
that token. Refusal MUST happen before any of those three writes, and MUST leave all of them exactly as they
were. The refusal MUST be a distinguishable already-exists condition that a caller branches on — the same shape
the framework's Lifecycle harness already uses for create-versus-exists — not a generic invalid error a caller
can only log.

Ordering is fixed: the loop-token FORM check runs first, and the already-exists check second, so a malformed
token is never reported as a collision.

Task intake MUST use that distinction. A task carrying a loop token that already names a registered loop is a
**continuation**: intake attaches to the existing loop and MUST reuse its context manager, so the conversation
accumulated so far is preserved and the new user turn is appended to it rather than replacing it. Attaching MUST
NOT re-seed the conversation with a fresh system prompt, and MUST NOT clear the loop's pending-tool set.
A continuation whose existing loop is in a terminal state MUST be refused rather than attached, and MUST NOT
mint a replacement loop under the same token.

A continuation whose existing loop has **work in flight** MUST also be refused rather than attached. Work is in
flight when the loop holds outstanding tool calls, or when the loop is awaiting a human approval decision.
Attaching in that window is not a continuation of the conversation, it is a second round on top of a half-written
one: the assistant turn carrying `tool_calls` is already in the conversation and the matching `tool` results are
not, so the assembled request carries orphan `tool_calls`; two rounds then advance the one loop and the one
context manager concurrently; and an attach to a loop awaiting approval moves it off that state, so the human's
later decision is dropped as stale and the gated call is abandoned. The refusal MUST be distinguishable from the
terminal refusal, because the two mean opposite things to the caller — terminal is final, in-flight is answerable
once the round finishes — and it MUST leave the loop's conversation, its pending-tool set, and its recorded state
exactly as it found them. Queuing the turn for later delivery is deliberately NOT the answer: a queued turn is new
semantics this capability does not have. The refusal is ordinary user behaviour — someone typed while the agent
was still working — and MUST NOT be reported at a severity reserved for operator-actionable faults; the other
intake failures keep theirs.

Attaching MUST preserve the redelivery-dedup property that intake already relies on: after a continuation is
accepted, a redelivery of that same continuation MUST be recognised as a duplicate rather than processed twice.

**Preserve, do not restore.** This requirement is about a loop still held by the running process. Reconstructing
a conversation whose process was replaced is explicitly NOT in scope and is claimed separately (#1146).

#### Scenario: a continuation preserves the conversation instead of discarding it

- **GIVEN** a running loop whose conversation already holds a system prompt, a user turn, an assistant turn, and
  a completed tool pair, and which holds no outstanding tool call
- **WHEN** a task carrying that loop's token arrives
- **THEN** the loop's existing context manager is the one used, the prior turns are still present, and the new
  prompt is appended after them
- **AND** no second system prompt is added and no other per-loop state is replaced
- **AND** the tests that verify this are `TestContinuationReusesContextManager` and
  `TestContinuationDoesNotReseedSystemPrompt`

#### Scenario: a direct create against an existing token is refused without touching its state

- **GIVEN** a registered loop with a context manager holding conversation turns
- **WHEN** `CreateLoopWithID` is called directly with that same token
- **THEN** it returns the already-exists condition, and the loop entity, its pending-tool set, and its context
  manager are all unchanged and hold the same values as before the call
- **AND** the test that verifies this is `TestCreateLoopWithIDRefusesExistingTokenWithoutMutation`

#### Scenario: a malformed token is reported as malformed, not as a collision

- **GIVEN** a non-canonical loop token
- **WHEN** `CreateLoopWithID` is called with it, whether or not any loop is registered
- **THEN** the refusal names the token form, not an already-exists condition
- **AND** the test that verifies this is `TestFormRefusalPrecedesAlreadyExists`

#### Scenario: a continuation naming a terminal loop is refused rather than silently restarted

- **GIVEN** a registered loop in a terminal state
- **WHEN** a task carrying that loop's token arrives at intake
- **THEN** the task is refused, no new loop is minted under that token, and the terminal loop's recorded outcome
  is unchanged
- **AND** the test that verifies this is `TestContinuationOfTerminalLoopIsRefused`

#### Scenario: a continuation naming a loop with an outstanding tool call is refused

- **GIVEN** a running loop that has dispatched a tool call whose result has not arrived, so its conversation holds
  an assistant turn with `tool_calls` and no matching `tool` result
- **WHEN** a task carrying that loop's token arrives at intake
- **THEN** the task is refused with the in-flight condition, which is distinguishable from the terminal refusal
- **AND** no user turn is appended to the conversation, no request is published, and the outstanding tool call is
  still outstanding
- **AND** the test that verifies this is `TestContinuationOfLoopWithToolsInFlightIsRefused`

#### Scenario: a continuation naming a loop awaiting a human approval is refused

- **GIVEN** a registered loop in `awaiting_approval` holding a pending approval for a gated tool call
- **WHEN** a task carrying that loop's token arrives at intake
- **THEN** the task is refused with the in-flight condition and the loop is still `awaiting_approval`, so the
  human's later decision still resolves the gated call rather than being dropped as stale
- **AND** the test that verifies this is `TestContinuationOfLoopAwaitingApprovalIsRefused`

#### Scenario: the in-flight refusal is not reported as an operator fault

- **GIVEN** a running loop with an outstanding tool call
- **WHEN** a task carrying its token arrives at the intake seam
- **THEN** the seam declares the refusal without raising it to the severity its other intake failures use, and
  the message is acknowledged rather than redelivered
- **AND** the test that verifies this is `TestBusyRefusalIsWarnedNotErrored`

#### Scenario: a redelivered continuation is deduplicated

- **GIVEN** a continuation that has already been accepted and attached
- **WHEN** the same task message is redelivered
- **THEN** it is recognised as a duplicate and does not produce a second attach or a second user turn
- **AND** the test that verifies this is `TestRedeliveredContinuationIsDeduplicated`

### Requirement: Per-loop in-process state is released at terminal, through the one release point

Every per-loop map the loop manager holds MUST be released when a loop reaches a terminal state. Today the loop
entity, its context manager, its pending-tool set, its queued tool calls, its cached tool definitions, tool
choice, metadata, request timeout and response format, its task prompt, and its truncation-retry counter are
retained for the lifetime of the process: the only method that clears them has no production caller. Growth is
therefore unbounded in the number of loops the process has ever run, and each entry is sized by its
conversation. A conversation the loop can no longer advance is not state; it is a leak.

The release MUST happen at the component's existing single terminal-release point — the one that already frees
the trajectory step aggregate and the observed-audit-loss marker after the loop's terminal observation and
terminal graph write have returned. It MUST NOT be a second release site: one home is what stops a future
terminal path from freeing one aggregate and leaking another. The release MUST remain idempotent.

The release is admissible ONLY under this invariant, which MUST hold for every reader: **after a loop settles,
the absence of its in-process entity is indistinguishable from its presence in a terminal state.** A message
that arrives for a settled loop — a late or duplicate approval response, a late tool result, a late model
response — MUST be treated as an expected settled-drop and MUST NOT be reported as an unexpected failure. That
case is already reachable today whenever a process replacement precedes the late message; this requirement makes
an existing path common rather than introducing a new one, and requires it to be handled deliberately rather
than by accident.

The durable loop record remains the authority for a settled loop's result, and reading it MUST NOT depend on
the in-process maps. Approval-timeout sweeping MUST be unaffected, because a candidate is by definition not
terminal. Nothing in this release MAY read agent execution evidence.

The already-exists fence above and this release interact and the interaction is stated so it is not later read
as a defect: once a terminal loop's in-process entity is released, a direct create against its token no longer
observes a collision in process memory. The refusal of an attach to a settled loop is therefore owned by the
admission gate, which decides from the durable record, and the in-process fence is defence in depth for the
window before release.

#### Scenario: a completed loop's per-loop state is released

- **GIVEN** a loop that has run several iterations with a populated conversation, cached tool definitions, and a
  task prompt
- **WHEN** it reaches a terminal state and its terminal observation and terminal graph write have returned
- **THEN** every per-loop entry the loop manager held for that token is gone
- **AND** the tests that verify this are `TestTerminalReleaseClearsEveryPerLoopMap` and
  `TestTerminalReleaseIsIdempotent`

#### Scenario: releasing does not run before the terminal readers have finished

- **GIVEN** a loop reaching a terminal state
- **WHEN** its terminal trajectory observation, its terminal graph write, and its durable persistence run
- **THEN** each of them observes the loop entity it needs, and the release happens after all of them
- **AND** the test that verifies this is `TestTerminalReleaseHappensAfterTerminalReaders`

#### Scenario: a late approval response for a settled loop is a quiet expected drop

- **GIVEN** a loop that has settled and whose per-loop state has been released
- **WHEN** an approval response for it arrives
- **THEN** it is dropped as stale with the same observability a stale response for a still-present terminal loop
  produces, and not reported as an unexpected failure
- **AND** the same holds for a late tool result and a late model response
- **AND** the tests that verify this are `TestLateApprovalResponseForSettledLoopIsExpectedDrop`,
  `TestLateToolResultForSettledLoopIsExpectedDrop`, and
  `TestLateModelResponseForSettledLoopIsExpectedDrop`

#### Scenario: a settled loop's result is still readable from the durable record

- **GIVEN** a completed loop whose per-loop in-process state has been released
- **WHEN** another agent reads that loop's result through the loop-result tool
- **THEN** the full result is returned from the durable loop record
- **AND** the test that verifies this is `TestSettledLoopResultReadableAfterRelease`

#### Scenario: approval-timeout sweeping is unaffected

- **GIVEN** a loop awaiting approval past its timeout and a set of already-settled loops
- **WHEN** the approval sweeper snapshots expired approvals
- **THEN** the awaiting loop is still a candidate and the settled loops contribute nothing
- **AND** the test that verifies this is `TestApprovalSweepUnaffectedByTerminalRelease`

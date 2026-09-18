# R8 finite retention and task intent: bounded architect handoff

base: 68c14c8eb25c512e988f740cbf7ea14b6815976f

Status: complete advisory handoff for independent review; no wire/spec/runtime change approved.

## Bounded conclusion

A finite-retention solution remains viable, but is **not yet proved end to end**. Permanent identity records are not
established as necessary.

One missing producer-known distinction is **whether the task starts a new loop or attaches to an existing loop**.
Dispatch knows this before publishing but discards that distinction. The loop currently infers it from existence—the
exact inference that becomes unsafe when an older loop expires.

### Corrected evidence

- USER and HTTP now skip publication when the retained task exists. The earlier equal-retention republication witness
  no longer follows.
- Rule `publishAgentOnce` mints a new LoopID, serializes once and calls its publisher once:
  `processor/rule/actions.go:1721,1965–1970`.
- `natsclient` makes one `js.PublishMsg` call: `natsclient/client.go:1005`.
- Pinned NATS v1.52.0 retries synchronous publication only for `ErrNoResponders`, not arbitrary errors/timeouts:
  module `jetstream/publish.go:232–247`. This is narrower than the previously assumed general republisher.
- Dispatch's attachment decision exists at `processor/agentic-dispatch/component.go:934–953` and
  `processor/agentic-dispatch/http.go:351–367`; `buildTaskMessage:850–869` does not carry it.
- Loop intake infers attachment when CreateLoopWithID reports an existing loop:
  `processor/agentic-loop/handlers.go:907–921`.
- Ordinary displayed-history chat already uses a new LoopID and supplied PriorMessages. It need not attach to an old
  execution: loop delta `spec.md:17–25`.

## Chosen recommendation

Prefer **finite observed retention plus explicit task intent**, not a new permanent bucket.

The next bounded wire-contract change would add one required field to existing TaskMessage:

`loop_mode: "new" | "attach"`

Two first-party constructor owners set it: dispatch and rule. Loop intake consumes it; registry/type remain unchanged.
Missing/unknown values fail validation—no legacy inference.

- `new`: eligible for legitimate birth or recovery of that task's birth.
- `attach`: requires existing authoritative execution; absence must never become new-loop creation.
- PriorMessages remains independent conversational input for `new`, not permission to recreate an expired attachment.
- A matching retained terminal authority still suppresses execution.

Cost: one payload field, two constructor owners, one validation owner, one intake/recovery owner, fixture/adopter
updates. Zero new buckets, stored records, timers, Store dependencies or retention extensions.

This field is a **proposed outward-contract change**, not yet implementation approval.

## What it does—and does not—prove

For original retained sources, publication order supports checking these existing-owner relationships:

- task evidence outlives its USER source;
- loop authority outlives its task source;
- request evidence outlives its task source.

Those are concrete retention relationships, not a computed recovery horizon. They still require production-path proof.

The field alone does **not** prove:

1. Safe absence inference when an already-fetched delivery outlives the retained broker record.
2. Every supported same-identity publication retry preserves the necessary ordering.
3. A caller-visible refusal for an attachment whose LoopEntity disappeared after dispatch accepted it.

Do not replace these obligations with an Iterations guard or claim the seeded RED establishes production reachability.

## Refusal and settlement boundaries

| Existing route | Supported consequence |
|---|---|
| Missing attachment at USER dispatch admission | Existing UserResponse/error; require PubAck before source ACK; publication failure retries. |
| Missing attachment at HTTP admission | Existing synchronous refusal; preserve optional-mirror semantics. |
| Retained nonterminal authority, definitively unavailable required history | Existing terminal-failure path can carry the approved refusal; required publications/state must complete before ACK. |
| Authority lost after task publication | Never create replacement execution. Existing UserResponse could serve routed callers with explicit loop output wiring, but activity SSE and routeless-task result paths remain unproved. Quarantine is not a substitute. |

### Exact stopping point

Proceed with the **explicit birth/attachment contract lowering and focused proof**, not permanent-storage
implementation. Keep the in-flight/publication proof and post-admission refusal route as named blockers to claiming the
complete finite solution. No broader state system is justified by the current evidence.

## Post-publication refusal: bounded follow-up

The existing terminal path cannot represent refusal for every valid attachment. It records the outcome of the
**loop**, not merely refusal of the newly submitted task.

### Concrete conflict

Suppose attachment task **B** names loop **L**, previously owned by task **A**:

- `AGENT_LOOPS/L` is absent.
- `COMPLETE_L` still contains A's selected outcome.

Creating a failed LoopEntity for B would avoid overwriting a currently present record, but it would not establish
B's authority over L. Terminal selection then correctly rejects the different TaskID at
`processor/agentic-loop/component.go:1974–1975`.

Replacing that selected outcome violates the one-terminal-outcome contract at loop delta `spec.md:405–409`.
Replaying A's outcome as B's refusal would violate task correlation.

This state is relevant to existing ordering: COMPLETE is selected before the final bare-loop marker, and the spec
explicitly retains selected completion when final-marker persistence fails (`spec.md:418–429`). Equal bucket TTLs
do not make both records disappear together.

Even when both records are absent, Create proves only present key vacancy—not that this attachment owns a new
execution under the old LoopID.

### Why the existing helpers do not resolve it

- `persistTerminalOutcome` requires observed nonterminal authority, its revision, and matching TaskID
  (`processor/agentic-loop/component.go:1833–1843`).
- `handleSpawnIdentityFailure` can Create authority because it handles a known legitimate birth, not attachment to
  an unknown prior execution (`processor/agentic-loop/component.go:1516–1539`).
- Publishing LoopFailed without that authority does not solve routing: dispatch first loads the loop record and
  returns an error when it is absent (`processor/agentic-dispatch/terminal_settlement.go:206–215`).
- A synthesized failure also reaches the UI as “Loop L failed,” not “Task B could not attach”
  (`processor/agentic-dispatch/terminal_settlement.go:158–160`).

Create/CAS protects concurrent records but cannot supply the missing semantic ownership.

### Explicit product alternative

If minimizing this PR is the priority, consider this product restriction:

**Do not support asynchronous attachment to an existing execution; use a new task/LoopID with PriorMessages for each
conversational turn.**

Ordinary multi-turn chat already has that contract and production-path scenario (`spec.md:17–25`). This restriction
would:

- Refuse explicit attachment and AutoContinue before task publication through existing USER/HTTP refusal paths.
- Remove existence-based attachment from raw Task intake.
- Leave ordinary task redelivery, within-turn model/tool iterations, and fresh rule-produced loops intact.
- Require no new recovery store, refusal record, or fabricated terminal state. Raw-wire migration and detection of
  obsolete attachment intent remain unresolved; a zero-new-field solution is not established.

The removed capability includes reuse of the live execution's ContextManager and execution budget. PriorMessages
accepts displayed user/assistant text, not the live run's non-displayed tool, system and working context. Preserving
ordinary chat does not preserve that live-execution continuation capability.

This needs explicit owner approval: current spec lines `391–396` permit continuation producers to echo an existing
LoopID. It is not already authorized by approving missing-history refusal.

If same-execution attachment must remain, refusal must be represented as a **task-admission outcome distinct from
L's terminal outcome**. Existing UserResponse can serve routed callers, but the routeless and activity-SSE paths
require an explicit extension. Reusing LoopFailed without that distinction is not a safe shortcut.

This conclusion resolves the carrier question only; it does not claim the remaining finite-retention proof complete.

## Root's proposed owner choice

No target-state delta or runtime behavior is changed by this proposal. The earlier `loop_mode` field remains an
unselected option. Retiring attachment does not yet establish that raw-wire intent/migration can avoid a field or
other contract change.

1. Retain asynchronous attachment: carry explicit task intent and design its task-specific refusal/result contract.
   This preserves the current feature but expands the outward contract beyond the proposed field alone.
2. Retire asynchronous attachment: each conversational turn is a new execution with explicitly supplied history.
   Reject targeted prompt attachment/AutoContinue, preserve cancel/approval control and ordinary restart/redelivery.
   This removes reuse of the live execution's non-displayed tool/system/working context and existing execution budget;
   PriorMessages preserves only displayed user/assistant text. It needs migration notes and production-path proof.
3. Change neither: keep the safety obligation open; the current absence inference is not approved as correct.

Root recommends option 2 if injecting new work into an already-running execution is not a product requirement.
Adopters using attachment must submit a new task with conversation history; prior messages are supplied through the
existing contract, not reconstructed magically after expiry. Identifiable USER ReplyTo/AutoContinue and HTTP
attachment entry points can refuse through existing routes. An unchanged raw-task producer intending attachment to
an expired LoopID is not distinguishable from legitimate birth using the existing bytes alone. Raw-wire migration
and detection therefore remain explicit design obligations, not a promised zero-field refusal guarantee.
Exact caller/fixture/configuration lowering follows only after owner selection. The remaining source-lifetime and
republication proof stays open under either active option.

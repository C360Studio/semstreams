# Design draft: small operational LoopEntity state contract

Status: design-review draft. Not owner accepted; no runtime edits or spec promotion authorized.

## Evidence and scope

This draft relies on the unchanged, independently approved inventory:

`inventory-loop-state-contract-2026-09-13.md`
SHA-256: `6f1c33a257ee6c0e6174c2cdcb95c005b67b6d426dcc7c42270dfed7970924c6`

Its independent INVENTORY PASS is recorded in:

`review-loop-state-inventory-2026-09-13.md`
SHA-256: `01e3ef2a110214ddfb94e8a8e0c405d84712fa46aaceb7f5d7ad79c58bcd5627`

The inventory's owners, collision table, source fingerprints, adopter seams and coverage limits remain applicable.
This design does not reopen its enumeration.

The orchestration-check skill places this contract inside the existing component-owned execution state. No Lifecycle
Manager adoption, graph-backed second loop authority, runtime, supervisor, scheduler, bucket or ledger is proposed.

## Decision requested

Approve one small contract: replace developer-phase labels with five operational states; enforce their edges and
record coherence at existing mutation/restoration boundaries; reuse `lifecycle.Transitions` privately while
retaining AGENT_LOOPS authority and the existing settlement machinery.

The direct import's measured dependency cost and the outward vocabulary/field retirements are part of this
decision—not implementation details to assume later.

## Options

| Option | Benefit | Cost / limitation |
|---|---|---|
| Do nothing to the state contract; continue individual durability corrections | Smallest immediate diff; preserves current API behavior. | Validation, TransitionTo and restoration continue to disagree. Each correction must independently defend those gaps; developer-phase vocabulary remains. |
| Tighten the existing owners while retaining current names | Can reject unknown targets and guard restoration without renaming the wire vocabulary. | Retains five labels with no distinct measured operational branches and the misleading public phase examples. Does not meet the requested simplification. |
| Recommended: five operational states, existing owners, private reuse of `lifecycle.Transitions` | One concrete vocabulary and edge declaration; no second generic validator or state-machine runtime. | Breaking vocabulary/field cleanup; direct lifecycle import adds eleven in-module packages. Durability corrections still remain necessary. |

Extracting a lightweight package, changing lifecycle's exported behavior, or adding a new public state abstraction
is not an alternative silently included here. Those surfaces have not received the required design inventory.

## Recommended vocabulary

Keep `LoopState` as the existing exported string type. Its admitted values become:

| Go symbol | Wire value | Meaning |
|---|---|---|
| `LoopStateRunning` | `running` | This loop has not terminated and is not waiting for human approval. Includes model/tool execution, waiting for results and an admissible continuation boundary. |
| `LoopStateAwaitingApproval` | `awaiting_approval` | A specific current tool execution is gated on a human decision. |
| `LoopStateComplete` | `complete` | Successful terminal loop outcome. |
| `LoopStateFailed` | `failed` | Failed terminal loop outcome. |
| `LoopStateCancelled` | `cancelled` | Cancelled terminal loop outcome. |

Remove `LoopStateExploring`, `LoopStatePlanning`, `LoopStateArchitecting`, `LoopStateExecuting`,
`LoopStateReviewing` and the remaining `LoopStatePaused`. Do not retain aliases or translate retired wire values
at runtime.

Running does not mean a goroutine is currently executing, a model request is outstanding, or a delivery has settled.
Those are different facts already carried by consumer bookkeeping and operation-specific correlation.

## Concrete transition contract

Declare one private `loopTransitions` value in `agentic`, using the existing `lifecycle.Transitions` type:

```text
running           → awaiting_approval | complete | failed | cancelled
awaiting_approval → running | failed | cancelled
complete          → no outgoing edges
failed            → no outgoing edges
cancelled         → no outgoing edges
```

These are business-state edges, not effect-completion steps.

The existing methods retain their signatures. State membership and legal edges come from the private table.
A private domain-specific coherence check enforces the State/PendingApproval relationship described below;
it is not a second generic transition validator.

`TransitionTo` checks source and target membership and source state-field coherence before considering a no-op
or mutation. Its direct-call contract is:

| Request | Behavior |
|---|---|
| Unknown/retired source or target | Refuse without mutation. |
| Any source with contradictory State/PendingApproval fields | Refuse without mutation, including a same-state request. |
| Same known state with coherent state fields | Return nil without mutation. This proves no delivery was applied. |
| running→awaiting_approval | Refuse without mutation: this state-only signature cannot construct the gate. The caller uses the existing `BeginAwaitingApproval` method. |
| awaiting_approval→running | Clear PendingApproval and set running together. `ResolveApproval` exposes this same local operation with an explicit awaiting-approval precondition. |
| awaiting_approval→failed or cancelled | Clear PendingApproval and set the requested state together. |
| running→complete, failed or cancelled | Set the requested state. PendingApproval is already absent by the source coherence check. |
| Any other differing-state request | Refuse without mutation according to the table. |

The table admits running→awaiting_approval as a business edge; `BeginAwaitingApproval` is the existing operation
that supplies its required data. Its implementation uses that same edge declaration rather than a second edge list.

`TransitionTo` changes only State and, when leaving approval, PendingApproval. It does not populate, clear,
validate or infer Outcome, Result, Error, CompletedAt, cancellation metadata or PendingToolResults. Those fields
remain owned by their existing builders and settlement operations. A locally terminal value is not a committed
terminal outcome.

The local mutation methods do not introduce new ID/budget prerequisites beyond their existing signatures:
their common precondition is known state and coherent state fields. Whole-record `Validate` additionally retains
its existing ID and MaxIterations checks. Every failed mutation leaves the entire receiver unchanged.

`LoopState.IsTerminal` preserves its current unknown-state behavior: unknown returns false. For known values it
consults the table. This explicit membership guard avoids importing lifecycle's different unknown-is-terminal
policy into existing raw-JSON consumers.

Vocabulary membership comes from the same private declaration, replacing the separate allowlist. Table structure
is checked using the existing validator. Do not export the table, a phase-list API, a configurable workflow or a
new generic validator.

### Refusals

Unknown/retired state, illegal edge, contradictory current record and identity conflict are errors—not successful
no-ops or evidence of staleness. Existing accepting boundaries retain their typed classification: malformed input
is not confused with contradictory durable authority or uncertain effects.

A terminal loop cannot be reopened by continuation, approval resolution or ordinary state mutation. A different
terminal outcome cannot replace a committed terminal merely because the requested edge or replacement record is
individually valid.

Duplicate delivery handling remains the delivery owner's job. Same-state success, terminality and process absence
do not authorize ACK.

## Record coherence and existing methods

Retire `LoopEntity.StateBeforeApproval`. With only one non-gated nonterminal state, it stores no independent fact:
successful approval resolution returns to running.

Local entity validity and delivery correlation are separate contracts.

### Local entity validity

`LoopEntity.Validate` retains its existing nonempty ID and positive MaxIterations checks, derives state membership
from the private table, and checks exactly this State/PendingApproval relationship:

| Record state | Local state-field coherence |
|---|---|
| running | PendingApproval is nil. |
| awaiting_approval | PendingApproval is nonnil; its CallID and ToolName are nonempty. |
| complete, failed or cancelled | PendingApproval is nil. |

Local validation does not require RequestID, ExecutionID or CallOrdinal, inspect retained messages, or impose
new timeout/timestamp rules. It does not require terminal Outcome or CompletedAt: a local terminal candidate can
exist before the settlement owner has constructed and committed its final outcome.

`BeginAwaitingApproval` is an existing exported API, not a private staging helper. It accepts only a known running
state with no PendingApproval and retains its existing CallID/tool-name argument checks. On success it constructs
PendingApproval from its existing arguments and existing time/default behavior, and installs that value with
awaiting_approval together. The result satisfies local state-field coherence immediately. Given an otherwise
valid receiver, `BeginAwaitingApproval` followed immediately by `Validate` succeeds without any identity stamping.

A second Begin call while already awaiting approval is refused without mutation, even with the same CallID.
Duplicate-gate replay remains the delivery owner's operation over existing gate evidence.

`ResolveApproval` requires a locally coherent awaiting_approval state. It clears PendingApproval and returns to
running together. It requires no RequestID, ExecutionID, ordinal or stream lookup. A failed Begin or Resolve
leaves the entire receiver unchanged.

Generic callers therefore retain the ordinary public sequence:

NewLoopEntity → BeginAwaitingApproval → Validate → ResolveApproval → Validate

They need no new hidden call order, loop-specific request grammar or framework execution-ID calculation.
Callers deliberately participating in the delivery protocol still satisfy that protocol's existing correlation
requirements; this design does not broaden them to every user of LoopEntity.

### Delivery-owner validation

Before accepting a gate-related delivery or committing a new durable gate, the existing private lane owner
continues to validate the complete framework identity and applicable retained evidence: RequestID, ExecutionID,
ordinal, provider CallID, tool name and the relevant request/result relationship.

The production handler already copies this correlation from the actual ToolResult. It continues doing so;
neither the public state methods nor outside callers must predict those identities. Missing or conflicting
required correlation retains the existing classified refusal. Local `Validate` success is not permission to
dispatch a tool, publish a prompt, settle a delivery or skip these checks.

Process installation and startup approval-deadline hydration use local entity validation plus their existing
record/deadline checks. This design adds no stream lookup or retained-evidence prerequisite to startup
installation. Actual delivery recovery still obtains the lane evidence required before effects or settlement.

At the final terminal write, the existing settlement owner additionally ensures that outcome, timestamps and
other terminal fields agree with the selected ordinary terminal payload. This is not a requirement of the
public state-only mutation methods. Preserve the existing distinction between a truncated LoopEntity outcome
and a failed terminal event.

Do not clear `PendingToolResults` merely because state becomes terminal: exact execution-result evidence remains
relevant to lane-specific application proof. All process replacements and multi-field state changes remain
protected by the existing manager synchronization. No new public helper, type or method signature is required.

## Creation, restoration and persistence

These remain different operations.

### Creation

A validated task creates a new running loop. Absence is not another LoopState.

The task owner establishes its initial AGENT_LOOPS record through creation semantics before taking a path that
requires an existing durable loop to settle a later birth failure. An already-existing record is read and checked;
it is not overwritten as another birth.

Form checks precede collision checks. Unreadable authority is not absence. A legitimate pre-birth failure must not
enter recovery that can only retry forever because its required record was never created.

This ordering belongs to existing R4. It adds neither a reservation record nor a second creation owner.

### Restoration

Restoration installs a validated observation of existing authority; it is not a permissive transition from an
arbitrary process snapshot.

The existing restore and UpdateLoop boundaries must check candidate coherence and the current protected process
entry before replacing it. A stale nonterminal snapshot cannot overwrite a newer terminal or incompatible gate.
Startup approval-deadline hydration follows the same guarded installation rule through its existing
CreateLoopWithID→UpdateLoop path. Installation requires local entity coherence and the startup owner's existing
record/deadline checks, not a newly introduced read of retained request or response evidence. A later delivery
still performs its existing lane-specific correlation and evidence validation before effects or settlement.

Process snapshots are speculative. Discarding a failed speculative candidate and reconstructing from freshly
observed durable authority is not a reverse business edge. It must not be implemented by treating an old saved
snapshot as current authority.

### Persistence

Keep AGENT_LOOPS as current loop authority. Carry the observed revision through the existing operation, without a
new durable field or coordinator. Changed authority requires reread/reclassification; unconditional Put cannot turn
a stale candidate into current truth.

Keep the accepted terminal sequence:

```text
select/reuse COMPLETE
→ finish required effects
→ obtain required terminal PubAck
→ revision-conditioned final LoopEntity marker
→ owner-specific source settlement
```

The selected terminal payload—not a later competing proposal—drives terminal effects. Required SyntheticDecide
behavior remains exactly the approved builder-owned obligation. Best-effort audit and graph batches retain their
existing nonblocking status.

Before the final marker, failed attempts discard speculative terminal process state and retain the selected
completion for replay. Already-cancelled durable authority must not regress when COMPLETE is absent.

A valid edge proves none of these effects completed. The held V4 candidate still needs assessment against this
contract; this design neither accepts it nor assumes its arbitration disappears.

## How ordinary work fits

| Situation | State behavior and existing mechanics |
|---|---|
| Sequential chat | A completed turn stays terminal. The approved fresh-loop/PriorMessages path carries the next turn's conversation. Existing admitted same-loop continuation remains available only under its current nonterminal/busy rules; this design does not remove that API. |
| Model and tool work | Remains running. Existing request identities, pending/queued tools, ordered results and budgets determine applicable work; no planning/reviewing states are needed. |
| Approval required | Running→awaiting_approval after a coherent gate is constructed. The durable gate precedes required prompt publication under the existing ordering. |
| Approval accepted | Awaiting_approval→running; dispatch the exact approved execution using existing correlation. |
| Approval rejected or deadline reached | Resolve the gate to running and feed the existing rejection result through normal continuation logic. Rejection alone does not mean the loop failed. Existing terminal failure cases use failed. |
| Missing required continuation evidence | Preserve the existing classified retry/refusal rules; where the accepted retained-absence contract permits terminal failure, awaiting_approval→failed is legal. |
| Cancel | Running or awaiting_approval→cancelled, subject to current authority and selected terminal outcome. |
| Restart | Read the exact current record and lane evidence, then reconstruct only required process material. No restarting state or startup replay scheduler. |

AgentRun remains separate: one run may span multiple loops, and a completed child or root loop does not determine
the run's phase. Its vocabulary, table, graph authority and fanout settlement are unchanged.

## Keep / change / separate

| Keep | Change | Separate |
|---|---|---|
| LoopEntity and LoopManager ownership; AGENT_LOOPS authority; current locks and delivery owners | Five-state vocabulary; one private table; aligned state admission | AgentRun lifecycle and #1249 |
| Exact request/execution/gate evidence and source-specific settlement | Guard whole-record replacement/restoration; enforce state-field coherence | R3 approval Store ruling |
| Selected COMPLETE reuse, required effects/PubAck and final-marker CAS | Retire StateBeforeApproval and remaining paused/developer-phase surface | Research mechanism #1288 |
| Published R1 classification/projection proof and existing approval validation correction | Minimum producer/example/UI-contract migration | Wider #1244 liveness/refusal consequences, watchdogs and orchestration |

The sole research source adjustment needed by symbol retirement is its inventoried state seed: use
`LoopStateRunning`. Its ownership, key classes, terminal representation and R1 behavior do not change.

## Implementation boundary and migration

Minimum source families are `agentic/state.go`, existing loop state/handler/recovery/persistence boundaries, their
focused tests, dispatch state documentation/projection validation, and the inventoried research seed. Existing
terminal builders may require coherent candidate construction; no new public HandlerResult abstraction is authorized.

Update README/Go examples, HTTP/OpenAPI state descriptions and the SemStreams-owned migration document. State explicitly:

- New running symbol/value; retired constants/values and StateBeforeApproval removal.
- Tightened errors and unchanged unknown `IsTerminal=false`.
- This pre-v1 contract targets freshly provisioned storage. Prove cold start on empty storage and process
  replacement using records produced under this contract. Do not invent a legacy-record rewrite, translation,
  drain or disposal procedure. If an actual retained deployment requiring migration or recovery is discovered,
  stop that deployment's upgrade work for a separately owner-reviewed, evidence-based plan.
- SemTeams must update its active-state union, normalizer fallback and column mapping.
- SemSpec must compile against the new surface and validate its terminal/backstop behavior; SemSage must validate
  its string-state presentation.
- Sister changes are implemented and tested by their owners. Broader third-party/alias coverage remains the
  inventory's stated limit.

A breaking release requires the relevant E2E gate under repository policy. No new compatibility promise is inferred.

## Existing task mapping and focused proofs

| Group | Required proof |
|---|---|
| R6 | Exhaustive table and direct-method tests: unknown/retired source and target refusal; coherent same-state no-op; contradictory same-state refusal; terminal absorption; every failed call leaves the entire receiver unchanged. |
| R6 | Direct running→awaiting_approval refuses without constructing a gate; Begin constructs a locally valid gate; Begin→Validate and Resolve→Validate require no RequestID/ExecutionID/ordinal stamping; second Begin is refused. |
| R6 | Direct awaiting_approval→running/failed/cancelled clears PendingApproval atomically; awaiting_approval→complete refuses unchanged. State-only methods leave outcome/timestamps, cancellation metadata and PendingToolResults untouched. |
| R2/R6 | Local gate validity does not bypass the lane's existing full correlation checks. Startup approval-deadline installation requires no new retained-stream evidence; actual delivery recovery still enforces its evidence contract. |
| Existing regression gates | Freshly provisioned-storage cold start and relevant E2E; process replacement from records created under this contract. No imagined retained-deployment migration proof. |
| R2 | Existing cancellation-overwrite RED through restoration and real KV revision change; warm/cold effect uncertainty retains the correct delivery decision; selected terminal reuse and required synthetic action survive replacement. |
| R4 | Creation versus existing-record recovery, including pre-birth failure; continuation/busy/terminal behavior unchanged; no ACK from process absence or same-state success. |
| R2/R4 | Failure before each required effect/PubAck/final-marker boundary cannot publish a false durable terminal marker or regress changed authority. |
| Existing regression gates | Preserve the published R1 proof, existing approval validation proof, sequential-chat path and exact tool-result application evidence. |

Tests must assert behavior and durable/transport consequences, not that a helper was called. The state matrix
complements these persistence proofs; it cannot replace them.

## Owner acceptance boundary

The recommended acceptance is the complete five-state contract above, including private direct reuse of
`lifecycle.Transitions`, its measured import cost, StateBeforeApproval retirement and explicit migration obligations.

If the dependency cost is unacceptable, stop at that decision. Do not silently copy a validator or extract a
package. Independent DESIGN REVIEW PASS and explicit owner acceptance remain required before implementation or
promotion into the active spec.

No tests or edits were performed for this design draft.

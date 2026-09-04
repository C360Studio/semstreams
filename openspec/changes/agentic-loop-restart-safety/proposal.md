# Change: agentic-loop restart-safe settlement

## Why

Agentic-dispatch currently keeps a second process-local interpretation of agentic-loop state. That state drives
approval, listing, AutoContinue, custom commands, debug output, and metrics even though `AGENT_LOOPS` is current loop
authority and dispatch already owns a shared authority-backed view over that bucket. Replacement therefore makes
valid loops appear absent or incomplete.

This change makes dispatch an edge gateway. It admits external requests and publishes task, cancel, and approval
work; performs exact authority reads for explicit LoopID operations; serves reverse-lookup conveniences from one
caught-up `AGENT_LOOPS` view; and bridges terminal events to durable user responses. Agentic-loop owns every
transition between those edges.

Approval continuation storage remains evidence-gated. The implementation first proves whether current `LoopEntity`
plus exact retained request/response evidence reconstructs approve, modify, reject, and timeout after complete process
replacement. The previously approved ObjectStore design is removed only after that proof and an explicit owner
revocation; otherwise it remains the approved fallback.

## Claim scope

The accepted settlement scope is 15 physical durable-input subscriptions across dispatch, governance, model, loop,
and tools. Dispatch no longer consumes `agent.created` or `agent.approval_pending` as correctness inputs. Those
agentic-loop outputs and their external subscribers remain unchanged.

The eight non-heartbeat production callbacks and every heartbeat owner retain the settlement-first contract
established by the earlier accepted design.

AgentRun complete/failed fanout is transferred intact to #1249 from the exact post-#1146 staged checkpoint `A`.
#1146 neither narrows nor ratifies its partial-fanout behavior.

## What Changes

- Durable agentic inputs settle only after their owner-specific durable consequence. Ordinary publications are
  durably at-least-once: source ACK waits for every required JetStream PubAck, and `Nats-Msg-Id` supplies only
  duplicate-window suppression. Exact reconstruction is limited to named task-birth, provider-invocation, approval-
  continuation, governance-verdict, tool-effect, explicit-LoopID, and terminal-route boundaries.
- Agentic-dispatch is exclusively an edge gateway. It admits external requests and publishes task, cancel, and
  approval work; exposes exact reads and one caught-up current-state projection; and bridges terminal complete/failed
  events to user responses when validated authority carries a user route. Agentic-loop exclusively owns loop birth
  and every intermediate and terminal transition. `LoopTracker`, its pending cache, and dispatch's created/pending
  correctness inputs are removed.
- Approval replacement first proves whether current loop state plus exact current request/response evidence is
  sufficient. The approved ObjectStore continuation remains a conditional fallback pending that proof and a second
  owner ruling.
- AutoContinue uses exact `(UserID, ChannelType, ChannelID)` identity and remains a convenience. During the gap after
  task PubAck and before first `LoopEntity` birth, a second route-only message may create another loop; callers needing
  continuity carry the minted LoopID returned by the first task path.
- Breaking tracker, graphview restart, debug-readiness, and metric migrations are documented and covered by the
  relevant agentic E2E before landing.

## Holds

- The remote #759 parent remains frozen at exact `F=417beae5552f8f15ad3540edd7d8504c87174c13` while #1159 implements,
  proves, and receives review.
  Any unexpected parent advance requires a new pin, rebase, inventory verification, test, and re-review.
- Eight physical non-heartbeat subscriptions return typed decisions from business work and settle only through their
  existing private binding callbacks. JetStream consumer configuration owns AckWait and redelivery; the framework
  derives no universal work deadline from AckWait.
- Components may apply an ordinary `context.WithTimeout` where their actual operation requires a business timeout.
  All delivery-derived work joins before settlement. A subscription moves to the existing heartbeat owner only when
  measured legitimate work can exceed its configured acknowledgement interval.
- Native messages and settlement methods remain outside business work. The narrow shared `SettleDelivery` transport
  helper validates an already-computed decision and attempts its terminal method; it invokes no work and owns no
  context, deadline, heartbeat, consumer handle, or lifecycle.
- Model uses AckWait 120s / heartbeat 60s. Loop task/response/tool-result use heartbeat 15s against shortest BackOff
  30s. Loop MaxDeliver is at least the fixed two-entry BackOff length, so omitted/zero defaults to 2 and explicit 1
  refuses before allocation. Exact acquisition configuration is validated before consumer allocation.
- The first dispatch, governance, model, or loop delivery-owner fatal result latches into the existing negative
  health/error-count surface before exact-handle drain. Metadata loss performs no work, heartbeat, or settlement,
  and later fatal observations neither overwrite the first cause nor recount it. No new health surface or state
  authority is added.
- Each recovery-dependent agentic component validates only its own resolved stream facts and local typed requirement
  before its dependent allocation. Non-agentic components perform no admission lookup.
- A rule processor whose resolved local outputs declare the AGENT task family uses the same internal admission
  validator before its action evaluator can publish. Runtime classification calls
  `component/flowgraph.SubjectCovers(declaredFilter, concreteSubject)` in that exact direction, not exact subject
  equality, and publishes registered `TaskMessage` envelopes through JetStream with PubAck.
- Agentic-loop separately acquires and observes the existing `AGENT_LOOPS` authority before publishing its handle or
  starting dependent consumers and the approval sweeper.
- The nested #1239 work is already integrated as PR #1251. Pause/resume handlers and dead signal verbs are gone;
  #1146 settles the surviving cancel lane and the separate approval-response lane.
- Binding product ruling #1239 comment `5526837992`, linked from #1146 comment `5526837994`, removes `paused` from
  the state vocabulary, API, schema, docs, and persisted-value acceptance with no compatibility shim or migration.
  The user-facing contract is cancel; durable ApprovalResponse wait; retry/restart from the last durable boundary;
  and operational quiesce through stop-admission, drain, cooperative cancel, and join. Arbitrary execution
  pause/resume is not a current API.
- No supervisor, generic state machine, checkpoint bucket, outbox, event-sourced loop, CQRS path, or universal
  exactly-once claim is admitted.
- Error propagation does not land separately from the durable authority that makes redelivery safe. Any additional
  durable state requires a named replacement failpoint proving existing Streams/KV/Store authority insufficient.
- Current-spec conflicts are replaced through full MODIFIED requirements. `AGENT_LOOPS` becomes dispatch's sole
  current-state authority and `LoopTracker` is deleted; tool completion/replay uses framework execution identity
  rather than provider `ToolCall.ID`; terminal loop release requires durable applied-state proof for late deliveries
  rather than process-memory inference.
- Model config adds exact `provider_ambiguity_policy` with default `fail_commit_unknown`; unsupported
  `provider_reconcile` refuses before consumer allocation. `AgentResponse.failure_kind` is a closed optional JSON
  string whose only new value is `provider_commit_unknown` on error responses.
- `AGENT_LOOPS` is the sole current-state authority for loops. Dispatch retains no `LoopTracker` or pending-approval
  cache.
- One caught-up authority-backed view serves `/activity`, `/loops`, `/debug/state`, and AutoContinue. Explicit LoopID
  operations exact-read `AGENT_LOOPS`.
- AutoContinue matches only exact `(UserID, ChannelType, ChannelID)` and is a convenience over currently authoritative
  records, not a continuity guarantee during the task-publication-to-`LoopEntity`-birth interval.
- Dispatch retains complete/failed consumption only to bridge terminal outcomes to durable user responses within the
  intersection of source-event and loop-state retention.
- A complete/failed event whose validated loop authority has no user route is a routeless non-user terminal. Dispatch
  settles it without publishing `user.response`; absence of a user route on a system-lane loop is not a retryable
  routing failure. Conflicting or unreadable route evidence still refuses settlement.
- Approval continuation uses no CallID-indexed `ToolResult` lookup. The settled approval-required result already lives
  in `LoopEntity.PendingToolResults`.
- The approved ObjectStore continuation plan remains conditional on the replacement evidence gate.

## Impact

- Tracking issue: #1146; parent epic: #1147.
- Staged prerequisite: #759; PR #1159 is stacked on its non-default branch.
- AgentRun successor: #1249 from post-#1146 checkpoint `A`; transition-contract successor: #1244.
- #1239 provenance remains with merged nested PR #1251; default-branch closing authority remains in PR #1156.
- Blocks restart-safe approval/enforcement claims in #1140.
- Seven capability deltas: `agentic-dispatch`, `agentic-governance`, `agentic-loop`, `agentic-model`, `agentic-tools`,
  `graph-view-subscription`, and `rule-agent-publishing`.
- Verification includes the #1146-owned tranche of #1155's real-NATS process-replacement matrix and serialized
  agentic E2E. #1155 remains open until #1249 supplies transferred AgentRun complete/failed proof; the combined
  matrix gate is completed later.
- Breaking adopter migrations: `CommandContext.LoopTracker` becomes the narrow `LookupLoopOwner` operation; exported
  `graphview.View.Restart` is removed; `router_active_loops` is removed with no authoritative Prometheus replacement.
  `/loops` and `/debug/state` preserve the `LoopInfo` JSON schema through an immutable view-derived DTO.

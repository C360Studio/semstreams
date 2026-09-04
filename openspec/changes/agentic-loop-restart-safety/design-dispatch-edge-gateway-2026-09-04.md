# Design checkpoint: dispatch is an agentic edge gateway

base: 79b0f29f82ce5391013f6c931fae69a28216ac93

## Accepted inventories

- `inventory-dispatch-bridge-boundary-2026-09-04.md`: SHA-256
  `cf5660a3b4196324a3695dc1174dacfb804cef56e2336536d4a9f7d8f4197daa`, independent `INVENTORY PASS`, 249/249
  pins.
- `inventory-task-loop-cardinality-2026-09-04.md`: SHA-256
  `22d593d5de5eea2d15a94da36162cae8b5a3a36cbfcc7790003c13a52ba7d340`, 134/134 pins.

## Owner direction being tested

Dispatch publishes work and bridges external callers and terminal results. Agentic-loop owns every transition and
piece of execution state between those edges. Intermediate events may notify other consumers, but dispatch must not
require them for correctness.

## Decision-skill outcomes

- `orchestration-check`: dispatch is an edge component. It accepts external commands, publishes work, and translates
  terminal outcomes; it does not own loop transitions or intermediate state.
- `kv-or-stream`: task, cancellation, and approval requests remain JetStream work. `AGENT_LOOPS` remains the current
  loop-state KV authority and its watch supplies current-state fan-out.
- `query-pattern`: explicit loop reads use a private operation-specific typed adapter. No general embedded query,
  raw-KV adopter surface, or graph front door is introduced.

## Measured premises

1. Dispatch already owns one `graphview.View[activityRecord]` over `AGENT_LOOPS`, with exact reads, lists, readiness,
   poison, watcher-loss, and restart behavior (`processor/agentic-dispatch/http_activity.go:28-121`, `:182-276`).
2. `LoopTracker` is independently mutated by task submission, `agent.created`, `agent.approval_pending`, and terminal
   consumption (`component.go:1037`, `:1113`, `:1160-1184`; `terminal_settlement.go:181-218`).
3. Dispatch's created and pending handlers only update that process-local projection; neither owns loop execution.
4. Explicit admission already reads exact `AGENT_LOOPS/<loopID>` state (`terminal_settlement.go:85-114`), but the
   decoded entity must additionally pass `LoopEntity.Validate` and key/ID identity validation.
5. `LoopEntity.PendingApproval` already contains CallID, tool name, arguments, reason, requested time, timeout, and
   trace ID (`agentic/state.go:142-157`). Dispatch does not need the pending event to approve after replacement.
6. `CommandContext.LoopTracker` has one real sister consumer: semteams `implementspec` authorization
   (`semteams/cmd/semteams/commands/implementspec/command.go:228-238`). `Component.LoopTracker()` has no production
   caller.
7. `agent.approval_pending` and `agent.created` have non-dispatch consumers. Loop continues to publish both.
8. `router_active_loops` is a process-local increment/decrement gauge and is false after replacement
   (`processor/agentic-dispatch/metrics.go:107`, `:307-314`). The agentic-loop active-loop gauge is also local
   execution telemetry: it is not hydrated from `AGENT_LOOPS` and is not authoritative after replacement
   (`processor/agentic-loop/metrics.go:110`, `:285`).
9. The existing graph view retains context indirectly through `restartWatcher func() error`, assigned a closure over
   `runCtx` (`pkg/graphview/view.go:147`, `:220-257`). This violates the repository context rule and cannot become
   precedent for broader dispatch use.
10. AutoContinue has a task-publication-to-LoopEntity-birth gap. The tracker masks it only in one process and never
    made it replacement-safe.
11. Semteams and semdev configure `auto_continue: true`; removing it has a real migration cost.
12. Semteams still calls the removed generic signal endpoint for cancellation. That adopter debt does not authorize
    restoring the endpoint.

## Options

| Option | Shape | Cost |
|---|---|---|
| A. Hydrate `LoopTracker` | Add snapshot/update recovery and retain created/pending consumers | Two projections of one fact, more races and lifecycle, and no durable fix for the AutoContinue birth gap |
| B. Consolidate on the existing graph view | One validated authority-backed projection for list/activity/AutoContinue; exact authority reads for explicit IDs; delete tracker | Enriches one private view record, migrates one command seam, and must state AutoContinue's real limit |
| C. Require explicit LoopID | Retain activity but delete AutoContinue and reverse-lookup/list conveniences | Smallest semantic surface, but breaks current semteams/semdev behavior and discards a useful listing surface the view can already serve |
| D. Do nothing | Keep tracker and event-driven shadow state | Replacement continues to fabricate absence; approval and metrics remain timing-dependent; task 6 adds duplicate machinery |

## Recommendation

Choose B. If the owner requires AutoContinue to guarantee continuity across the pre-LoopEntity birth gap, choose C
instead; neither A nor B provides that stronger guarantee without a new durable route authority.

Delete `LoopTracker` rather than retain it as a compatibility or performance cache:

- `/activity`, `/loops`, `/debug`, and AutoContinue share the existing `AGENT_LOOPS` graph view.
- The private decoded record retains a validated `LoopEntity`, its current public `Loop` wire projection, and the KV
  server timestamp. No public wire field is added.
- `/loops/{id}`, `/status <id>`, approval, explicit continuation, and cancellation read exact authoritative state.
- `agent.created` and `agent.approval_pending` disappear only from dispatch inputs. Their loop outputs and external
  subscribers remain.
- `agent.complete` and `agent.failed` remain dispatch inputs solely to bridge terminal outcomes to durable
  `user.response` publications.
- Remove `router_active_loops` without adding another projection-maintenance hook. The loop-owned gauge remains local
  execution telemetry, not durable current state. Operators use `/loops` only while the view is caught up.
- Replace `CommandContext.LoopTracker` with the narrow context-aware `LookupLoopOwner` operation measured from its
  one present consumer. Delete `Component.LoopTracker()`.
- Retain `LoopInfo` only as an immutable view-derived response DTO so `/loops` and `/debug/state` preserve their JSON
  and OpenAPI shape. It is no longer mutable tracker state.

## AutoContinue contract

AutoContinue selects only from a caught-up authority-backed view and matches the exact
`(UserID, ChannelType, ChannelID)` tuple. Empty fields, partial matches, UserID-only matches, ChannelID-only matches,
and cross-channel fallback do not match:

- zero nonterminal matches: create new work;
- one match: continue it;
- more than one match: refuse as ambiguous;
- unavailable, stale, or poisoned view: Retry for durable intake or 503 for HTTP;
- incomplete state is never interpreted as empty.

AutoContinue remains a convenience, not a continuity guarantee. A second message after task PubAck but before the
loop writes its first `LoopEntity` may create another loop. Callers requiring deterministic continuity echo the
LoopID returned by task submission. No route-claim bucket is added to conceal that boundary.

## Lifecycle

Delete exported `graphview.View.Restart()` and its retained `restartWatcher` closure. Do not replace it with another
public lifecycle method. On view failure, dispatch sends a restart command to its existing activity-view control
goroutine. That goroutine stops the failed view, constructs and starts a replacement with the `runCtx` already on
its stack, and swaps the replacement into its control-owned slot. Callers wait until the replacement catches up.
`Stop` cancels the control owner and joins the current view. Because `pkg/graphview` is exported, removing `Restart`
still requires explicit owner acceptance and a migration note.

## Retention and eviction

The view adds no retention. It mirrors `AGENT_LOOPS`; tombstones and expiry remove local entries. `COMPLETE_` records
remain activity records but are excluded from active-loop selection, active listing, and AutoContinue. There is no
second cleanup timer or cache retention.

## Approval continuation evidence gate

Do not revoke the approved ObjectStore design yet. Before implementing approval continuation storage, prove whether
retained framework evidence already reconstructs the approval boundary.

The real-NATS test shall:

1. run through an approval-required `ToolResult` until that delivery is fully settled: the awaiting-approval
   `LoopEntity` is persisted and `ApprovalPendingEvent` receives PubAck;
2. stop and replace agentic-loop and agentic-dispatch, discarding every loop manager, context manager, request/call
   map, pending-tool map, dispatch tracker, and approval cache;
3. preserve `AGENT` and `AGENT_LOOPS`;
4. independently exercise approve, modify, reject, and timeout;
5. redeliver the approval response and any resulting tool result; and
6. prove one declared transition and no duplicate tool execution.

In-retention reconstruction may use only operation-specific exact reads:

- latest `agent.request.<LoopID>`;
- exact `agent.response.<RequestID>` obtained from that request;
- current `AGENT_LOOPS/<LoopID>`.

It shall not list or scan `AGENT`. A provider-authored CallID is not globally unique and does not identify an AGENT
stream record. Approval reconstruction therefore performs no `ToolResult` stream lookup and adds no CallID index,
durable identifier, mapping, or message type.

The settled approval-required `ToolResult` is already retained in
`LoopEntity.PendingToolResults[PendingApproval.CallID]` before the awaiting-approval entity is persisted. The latest
request supplies the current RequestID, conversation, and model request contract. Its exact response must contain
exactly one tool call matching `PendingApproval.CallID`. The persisted result must be approval-required and agree
with `PendingApproval` on CallID, tool name, LoopID when present, and trace identity when present. The current
response's call supplies the authoritative original arguments and must agree with `PendingApproval.Arguments`.

An older response carrying the same provider CallID under another RequestID is irrelevant: reconstruction follows
the current request's RequestID and never searches by CallID. A real-NATS collision fixture retains two responses
with the same CallID and different RequestIDs and arguments, then proves only the response named by the current
request can reconstruct the boundary.

Approval evidence settles as follows:

| Evidence result | Settlement |
|---|---|
| Transport/read failure, unobservable retention, or unresolved absence | Retry; publish nothing and make no transition |
| Observed retention policy confirms the referenced exact record should remain, but it is absent | Durably fail `continuation_unavailable` |
| Malformed envelope/payload, duplicate current-response call, or any identity/content conflict | Quarantine |
| Exact validated match | Commit or prove the one stable branch output, then continue |
| Matching canonical output already committed | Replay/prove it; do not duplicate work or transition |
| Stable output identity exists with different canonical content | Quarantine |

These dispositions apply identically to approve, modify, reject, and sweeper-driven timeout. A missing exact read is
not automatically permanent. `continuation_unavailable` is durable only after observing the relevant retention
policy and confirming absence. No branch clears `PendingApproval` before its exact next output is committed or
proven. Approve and modify publish one approved tool output; reject and timeout publish no tool execution and make
one rejection-driven transition.

If all four branches pass, stop for an owner ruling to revoke approval comment `5463183450`, then remove
`ApprovalContinuationV1`, its Store configuration, digest, and cleanup protocol. If any branch needs a scan or a new
durable fact, retain the already-approved ObjectStore plan unchanged. Do not invent a third continuation mechanism.

## Mixed `AGENT_LOOPS` classification

The shared view classifies keys before decoding values:

1. A bare canonical LoopID is current loop state. Decode `LoopEntity`, call `Validate`, and require entity ID to
   equal the key.
2. `COMPLETE_<LoopID>` is activity-only terminal/result state. First require a canonical LoopID suffix, then apply
   completion-family-specific validation. A typed agentic terminal payload validates and its LoopID must equal the
   suffix. A registered research `SearchResult` has no LoopID; its envelope and payload validate, and the suffix
   supplies its loop identity. The view projects only fields the existing activity wire can represent faithfully:
   LoopID from the suffix, state `complete`, outcome `success`, synthesis as result, and completed research iterations
   as iterations. It omits aggregate `TokensUsed` because the wire has only distinct `tokens_in` and `tokens_out`
   fields; it never mislabels the aggregate. Unknown, malformed, unregistered, or invalid completion payloads become
   activity poison.
3. Current research-pipeline namespaces are non-loop records and return `keep=false`:
   `research.request.received.`, `classify.complete.`, `classify.snapshot.`, `route.complete.`, `route.snapshot.`,
   `execute.complete.`, `execute.snapshot.`, `assess.complete.`, `assess.snapshot.`, `search_result.complete.`, and
   `synthesize.snapshot.`.
4. Every other key is a malformed would-be loop key and becomes poison.

This is the current bucket grammar, not a legacy compatibility allowlist. Namespace spellings have one shared
declaration rather than another set of package-local literals.

Activity exposes completion poison per key. Active-loop listing and AutoContinue ignore valid activity-only
completion records but fail closed while the poison set contains any current-loop or unknown-key poison. A clean
replacement write or tombstone at a greater revision heals the key; readiness returns only after applying the
healing revision.

A real-NATS mixed-bucket test seeds a valid bare `LoopEntity`, both supported `COMPLETE_` payload families,
representative records from every current research namespace, malformed bare/unknown and completion records, and a
healing write/delete. It proves research records never appear as loops, poison disables AutoContinue, completion
poison remains visible to activity, and healing restores readiness.

## Narrow command operation and response projection

The present external command consumer needs ownership, not a general loop read:

```go
type LoopOwner struct {
	LoopID string
	UserID string
}

type LoopOwnerLookup func(context.Context, string) (LoopOwner, error)
```

`CommandContext` exposes this operation as `LookupLoopOwner`. It validates the canonical LoopID, exact-reads and
validates `AGENT_LOOPS/<LoopID>`, verifies key/ID agreement, and returns only `LoopID` and `UserID`. It exposes no
`LoopEntity`, KV handle, bucket name, or projection. Errors are classified as `invalid_loop_id`, `loop_not_found`,
`loop_owner_absent`, `loop_record_invalid`, or `loop_state_unavailable`.

`LoopTracker` and `Component.LoopTracker()` delete completely. Preserve existing `/loops` and `/debug/state` JSON
and OpenAPI shapes by retaining `LoopInfo` only as a view-derived response DTO. Populate it from validated
`LoopEntity` plus KV server time. `PendingApprovalInfo` projects from `LoopEntity.PendingApproval`.
`context_request_id` remains optional and empty because it was never durable and is not an authority field.
`/debug/state` changes from false `200` with empty loops to `503` when the loop projection is unavailable, and adds
`loop_projection_ready` and `loop_projection_poisoned` diagnostics.

## Terminal response retention boundary

Dispatch guarantees terminal user-response reconstruction only inside the intersection of:

1. retention of the complete/failed source delivery; and
2. retention of the exact `AGENT_LOOPS` record carrying routing and ownership state.

The event and current loop record must agree on every nonempty route field. Dispatch claims neither resource's full
configured horizon as the response horizon. A temporarily absent record retries because the event may precede the
authority read becoming visible. A deleted, purged, expired, or capacity-evicted record is outside the guarantee:
dispatch emits classified `terminal_route_unavailable` evidence and never fabricates a route from process memory.
Source expiry may then end redelivery without a user response; documentation states this bounded delivery contract.

## Adopter seam

| Adopter | Must know | If unchanged | Discovery | Should know |
|---|---|---|---|---|
| Custom command author | Use `LookupLoopOwner(ctx,id)` | Compile failure | Compile error and migration note | Only LoopID |
| semteams implementation command | Replace tracker owner check with the exact lookup | Build fails | Compile error | Only selected run ID |
| semteams API/UI | `LoopInfo` JSON remains; debug can return 503 | Reads continue; code assuming debug is always 200 fails | HTTP status and migration note | Projection readiness |
| AutoContinue caller | Match requires the exact user/type/channel tuple | Explicit `true` remains, without partial fallback | Typed ambiguity/unavailable result | Only whether convenience is wanted |
| Dashboard/operator | `router_active_loops` is removed | Dashboard series disappears | Metric migration note | Use `/loops` while view-ready |
| graphview Go caller | `Restart()` is removed | Compile failure | Compile error and migration note | Lifecycle owner recreates the view |
| semteams UI | Cancel uses the admitted cancel endpoint, not removed generic signal | Existing calls continue returning 404 | Migration note and client tests | Only "cancel this loop" |
| Approval-pending subscriber | Nothing changes | Continues receiving the same event | No migration | Nothing |
| Approval UI | Nothing changes before the evidence-gate ruling | Existing approval endpoint continues | No migration | Nothing |

## Target-state artifact deltas

### Proposal

Replace dispatch projection language with:

> Dispatch is an edge gateway. It publishes admitted work, reads exact authority for explicit LoopID operations,
> serves reverse-lookup conveniences from one caught-up current-state view, and bridges terminal events to durable
> user responses. `AGENT_LOOPS` is the sole current-state authority. Dispatch retains no LoopTracker or pending-
> approval cache. AutoContinue is a convenience; explicit LoopID is the deterministic continuity surface.

The physical settlement scope becomes 15 subscriptions. Remove dispatch's `agent.created` and
`agent.approval_pending` rows; their loop outputs remain.

### Design

Replace the dispatch projection section with three jobs:

1. admit an external request and publish its task, cancellation, or approval work;
2. answer explicit reads from exact `AGENT_LOOPS` and reverse-lookup reads from one caught-up view; and
3. translate terminal complete/failed events into durable user responses.

Agentic-loop owns loop creation, pending approval, intermediate transitions, and terminal state. Delete LoopTracker,
its approval buffer, created/pending dispatch inputs, and the dispatch active-loop gauge. Preserve the corresponding
loop outputs.

### Agentic-dispatch specification

Add or replace requirements with:

#### Requirement: Dispatch is an edge gateway over loop authority

Dispatch SHALL publish admitted task, cancellation, and approval work, SHALL perform explicit LoopID operations
against exact validated `AGENT_LOOPS` state, and SHALL bridge terminal complete/failed events to durable user
responses. Agentic-loop SHALL own every intermediate transition. Dispatch SHALL NOT retain `LoopTracker`, pending-
approval cache state, or created/pending subscriptions as correctness inputs.

- Approval after dispatch replacement reads the exact pending state and publishes its recorded CallID without a
  prior pending event.
- Unreadable or invalid exact state refuses transiently; process memory never admits an operation.

#### Requirement: Dispatch has one authority-backed current-state projection

Dispatch SHALL use one caught-up view over `AGENT_LOOPS` for activity, active-loop listing, debug enumeration, and
AutoContinue. It SHALL apply the declared mixed-key grammar before decoding. Partial, stale, or current-loop/unknown
poisoned state SHALL NOT be treated as empty. Valid `COMPLETE_` records remain activity records and are excluded from
active-loop listing and AutoContinue. Valid research namespaces are ignored rather than poisoned.

- One exact active `(UserID, ChannelType, ChannelID)` match continues it.
- Multiple matches refuse as ambiguous.
- Bootstrapping, stale, lost, or poisoned state yields Retry for durable work or service-unavailable for HTTP.

#### Requirement: Terminal events bridge only durable user routing

Dispatch SHALL retain complete and failed consumers only to translate typed terminal events plus exact loop routing
state into durable user responses. A routeless non-user loop settles without inventing a route. Replacement dispatch
requires the stable response PubAck before source ACK and mutates no LoopTracker. The guarantee is bounded by the
intersection of terminal-source retention and exact loop-state retention.

### Graph-view-subscription specification

Add:

#### Requirement: View lifecycle does not retain context

A graph view SHALL accept context as the first argument to `Start`, SHALL NOT retain a context or closure/provider
that recovers one, and SHALL NOT expose `Restart`. A lifecycle owner recreates a failed view with the run context
already on its control goroutine's stack. `Stop` joins all watchers and subscribers.

### Tasks section 6

Replace with:

- 6.1 RED: add the real-NATS approval replacement gate. Settle an approval-required `ToolResult`, replace loop and
  dispatch with all process maps discarded, retain `AGENT` and `AGENT_LOOPS`, and independently prove approve,
  modify, reject, timeout, and redelivery.
- 6.2 Reconstruct from current `LoopEntity`, latest exact `agent.request.<LoopID>`, and exact
  `agent.response.<RequestID>` only. Validate `PendingToolResults[CallID]` against pending approval and the current
  response call. Perform no `ToolResult` stream read, CallID index, stream list, or scan.
- 6.3 Add a same-CallID/different-RequestID real-NATS fixture with conflicting arguments. Prove reconstruction follows
  the current request's RequestID and neither inspects nor selects the older response.
- 6.4 Table-test all four decisions across unresolved Retry, confirmed retained absence to durable
  `continuation_unavailable`, malformed/conflict to Quarantine, exact match, committed-output replay, and committed-
  content conflict to Quarantine.
- 6.5 Stop for an owner ruling on the gate result. On PASS, obtain explicit revocation of comment `5463183450`
  before deleting the Store plan. On FAIL, retain `ApprovalContinuationV1` and the ObjectStore plan unchanged.
- 6.6 Implement the mixed `AGENT_LOOPS` key classifier and real mixed-bucket proof, including research `keep=false`
  namespaces, both completion families and their distinct identity rules, poison, readiness disablement, and
  healing. Assert the exact `SearchResult` activity JSON/OpenAPI representation and that aggregate `TokensUsed` is
  not projected into `tokens_in` or `tokens_out`.
- 6.7 Delete `LoopTracker` and its created/pending dispatch consumers. Preserve those loop outputs and external
  subscribers.
- 6.8 Replace `CommandContext.LoopTracker` with `LookupLoopOwner` and its five classified outcomes. Preserve
  `LoopInfo` only as an immutable view-derived `/loops` and `/debug/state` response DTO.
- 6.9 Serve `/loops`, `/debug/state`, `/activity`, and AutoContinue from one caught-up view. AutoContinue matches the
  exact `(UserID, ChannelType, ChannelID)` tuple with no fallback.
- 6.10 Remove `router_active_loops`; add no replacement Prometheus count. Document `/loops` plus view readiness and
  the non-authoritative nature of local loop execution telemetry.
- 6.11 Delete graphview `Restart` and its retained context closure. Recreate a failed view inside dispatch's existing
  lifecycle-control goroutine and prove `Stop` joins it.
- 6.12 Prove terminal user routing only within source-delivery intersection loop-state retention, including
  replacement, conflicts, transient absence, deletion/purge, and expiry.
- 6.13 Add SemStreams-owned migration notes for `LookupLoopOwner`, tracker removal, preserved `LoopInfo` DTO
  semantics, `/debug/state` 503, metric removal, graphview `Restart` deletion, exact AutoContinue tuple, and
  semteams' obsolete signal endpoint.

### Stale target-state deletions

- Change 17 subscriptions to 15 and ten non-heartbeat callbacks to eight.
- Delete dispatch created/pending rows and "projection update or exact proof" settlement language.
- Delete every claim that LoopTracker remains a cache or receives a second hydration path.
- Retain the approved continuation Store claims until the evidence gate passes and the owner explicitly revokes the
  prior approval. On gate failure, keep them unchanged.
- Preserve random framework-minted LoopID and the approved TaskID/redelivery correlation plan.

## Risks and unproven gates

- AutoContinue's birth gap remains unless a new durable route claim is separately designed.
- The shared classifier must validate key grammar, `LoopEntity`, key/ID identity, supported completion payloads, and
  poison healing in a real mixed bucket.
- Terminal routing is deliberately bounded by the intersection of source and exact loop-state retention.
- Removing approval ObjectStore support needs all four real-NATS decision branches green and a second owner ruling.
- Removing exported `graphview.Restart()` requires owner acceptance and a migration note.
- Breaking changes require the agentic E2E tier green before landing.

## Owner docket

1. Choose B, or choose C if AutoContinue must guarantee continuity across the pre-`LoopEntity` birth gap.
2. Authorize the approval reconstruction gate; revoke comment `5463183450` and delete the Store plan only if all
   four branches pass.
3. Approve deletion of exported `graphview.View.Restart()` with no replacement; dispatch recreates the view under its
   lifecycle owner.
4. Approve `CommandContext.LoopTracker` to `LookupLoopOwner`, while preserving `LoopInfo` only as the immutable HTTP
   response DTO.
5. Approve `/debug/state` returning 503 when projection truth is unavailable and adding readiness/poison fields.
6. Approve removal of `router_active_loops` with no authoritative Prometheus replacement.
7. Accept the terminal bridge's source-retention intersection loop-state-retention boundary rather than a
   full-horizon guarantee.

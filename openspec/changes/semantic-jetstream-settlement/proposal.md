# Change: semantic JetStream settlement

## Why

Five direct owners represent nine heartbeat delivery bindings. The legacy helper infers settlement from nil/error,
uses hidden fixed retry delays, lets cancellation replace a joined work result, and cannot tell the exact delivery
owner when heartbeat control has become unsafe.

The bindings do not share one definition of done. Tools and dispatch have bounded durable consequences that can be
encoded in the foundation. Model, loop, and AgentRun require owner-specific restart and fanout contracts before they
can migrate. JetStream already owns durable delivery and restart redelivery, and components already own exact native
handles; a shared supervisor, state-machine runtime, lifecycle gate, or durable quarantine store would duplicate
those authorities.

SemStreams is pre-v1 and greenfield. The permanent typed API and removal of the old export must reach `main` in one
atomic transition rather than establish a compatibility period as accepted framework surface.

## What changes

- Add validated ACK, Retry, Terminate, and Quarantine decisions plus an error-last work contract.
- Observe the delivery number without exposing native settlement authority.
- Separate semantic retry timing from the consumer's AckWait and BackOff lease policy.
- Validate heartbeat policy from the exact consumer configuration before acquisition.
- Preserve semantic, heartbeat-control, and local-settlement evidence in an inspectable result.
- Stop the exact existing owner after heartbeat control loss or quarantine.
- Migrate tools, dispatch complete/failed, model, loop task/response/tool-result, and AgentRun complete/failed through
  separately accepted owner-specific definitions of done.
- Prove all nine bindings across SemStreams process replacement while retaining NATS.
- Remove `NewDurableHandler` and `ConsumeWithHeartbeat` without aliases.
- Correct gated-DAG publish ambiguity and bounded deduplication claims, while keeping adopter-specific done/replay in
  `gated-dag-dispatch` and generic transport mechanics in `jetstream-consumer-policy`.
- Document the message-pump and lease-watchdog pattern and the measured gated-DAG adopter migration seams.

## Atomic public landing

#759 owns the complete public API transaction: introduce the permanent typed settlement surface, integrate the nine
owner-specific migrations, and remove exported `ConsumeWithHeartbeat` without alias. PR #1156 remains draft and does
not merge until the old symbol and every production caller are absent.

#1146 retains its full accepted restart-safety scope and implements model plus loop task/response/tool-result against
the staged #759 foundation through PR #1159. #1249 independently designs and implements AgentRun complete/failed
fanout settlement against the post-#1146 staged foundation. Both PRs target the non-default #759 branch and receive
their own reviews. Their work reaches `main` only through the final reviewed #1156 squash merge.

The three current production caller files form a zero-growth branch-staging guard only. They are not an API
allowlist, compatibility promise, current capability, or merge gate.

No binding migration is a mechanical nil-to-ACK/error-to-Retry conversion. Each ACK requires its accepted
owner-specific durable definition of done. A fast lane does not gain raw settlement authority or an exported
no-heartbeat workaround.

## Impact

- Breaking pre-v1 API replacement: the default branch receives `ConsumeDeliveryWithHeartbeat` and removal of
  `ConsumeWithHeartbeat` in one final PR.
- No accepted default-branch interval exposes both APIs.
- #1146 and #1249 are separately claimed and reviewed on the non-default #759 integration branch.
- Final PR #1156 carries default-branch closing authority for #759, #1146, #1249, and, only after complete proof,
  #1155.
- `NewDurableHandler` and `ConsumeWithHeartbeat` are both absent from final current truth.
- Tools heartbeat changes from 120 seconds to 5 seconds while BackOff remains 15/60 seconds.
- SemStreams records SemSpec and SemDragon migration requirements without mutating either sister repository.
- Deterministic `Nats-Msg-Id` deduplication is claimed only within the configured `Duplicates` window. Beyond that
  horizon, each adopter's durable already-complete or idempotent replay check is authoritative.

## Non-goals

- No merge of PR #1156 while exported `ConsumeWithHeartbeat` or any production caller remains.
- No API allowlist or compatibility status derived from the branch-staging zero-growth guard.
- No mechanical ACK conversion.
- No raw-message settlement escape or unreviewed exported no-heartbeat API.
- No child-PR merge directly to `main` and no intermediate accepted dual-API state.
- No shared admission gate, handle owner, supervisor, rule, workflow, state-machine runtime, checkpoint, outbox,
  CQRS layer, or event-sourced loop.
- No durable quarantine bucket, stream, subject, payload, entity, ObjectStore record, or unapproved AgentRun receipt
  ledger.
- No generic gated-DAG nil/error definition of done or universal heartbeat API in the domain capability.
- No claim that deterministic message-ID deduplication provides unbounded exactly-once delivery.
- No sister-repository mutation.

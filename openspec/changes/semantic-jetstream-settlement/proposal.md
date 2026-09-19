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

SemStreams is pre-v1 and greenfield, so the old export gets no deprecation period: it is deleted by the PR that
migrates its last caller (#1249) rather than kept alive as accepted framework surface. The typed API lands first, in
this change, with the two bindings whose durable consequences the foundation can encode.

## What changes

- Add validated ACK, Retry, Terminate, and Quarantine decisions plus an error-last work contract.
- Validate server delivery metadata before work without exposing the delivery count or native settlement authority.
- Separate semantic retry timing from the consumer's AckWait and BackOff lease policy.
- Validate heartbeat policy from the exact consumer configuration before acquisition.
- Preserve semantic, heartbeat-control, and local-settlement evidence in an inspectable result.
- Stop the exact existing owner after heartbeat control loss or quarantine.
- Migrate tools and dispatch complete/failed in this change through accepted owner-specific definitions of done;
  model, loop task/response/tool-result (#1327) and AgentRun complete/failed (#1249) migrate in their own layers.
- Prove the three migrated bindings across SemStreams process replacement while retaining NATS; the remaining six are
  proven by their own layers.
- Remove `NewDurableHandler` without alias here; `ConsumeWithHeartbeat` is removed by #1249 with its last caller.
- Correct gated-DAG publish ambiguity and bounded deduplication claims, while keeping adopter-specific done/replay in
  `gated-dag-dispatch` and generic transport mechanics in `jetstream-consumer-policy`.
- Document the message-pump and lease-watchdog pattern and the measured gated-DAG adopter migration seams.

## Layered public landing

#759 owns the complete public API transaction, landed in layers rather than one squash. This change is the
foundation: the permanent typed settlement surface, the tools and dispatch migrations, and removal of
`NewDurableHandler`. It promotes only what it implements.

The remaining migrations land as their own claimed, reviewed, and archived PRs stacked above it: #1327 takes model
and loop task/response/tool-result, #1249 takes AgentRun complete/failed fanout and deletes `ConsumeWithHeartbeat`
with its last caller. Neither closes #759; the layer that finishes the migration does.

The remaining production caller files form a zero-growth ratchet only. They are not an API allowlist, compatibility
promise, current capability, or merge gate.

No binding migration is a mechanical nil-to-ACK/error-to-Retry conversion. Each ACK requires its accepted
owner-specific durable definition of done. A fast lane does not gain raw settlement authority or an exported
no-heartbeat workaround.

## Impact

- Breaking pre-v1 API replacement: the default branch receives `ConsumeDeliveryWithHeartbeat`, and
  `ConsumeWithHeartbeat` is removed by the PR that migrates its last caller (#1249). No deprecation period is
  offered to adopters; `docs/operations/migration-beta162-to-beta163.md` names the removal. Until that PR merges,
  `main` carries the helper unadvertised and the AST ratchet forbids any new caller. (Owner ruling 2026-09-18,
  recorded on #759.)
- #1146/#1327 and #1249 are separately claimed and reviewed as PRs stacked above the foundation PR #1331; none of
  them closes #759.
- `NewDurableHandler` and `ConsumeWithHeartbeat` are both absent from final current truth.
- Tools heartbeat changes from 120 seconds to 5 seconds while BackOff remains 15/60 seconds.
- SemStreams records SemSpec and SemDragon migration requirements without mutating either sister repository.
- Deterministic `Nats-Msg-Id` deduplication is claimed only within the configured `Duplicates` window. Beyond that
  horizon, each adopter's durable already-complete or idempotent replay check is authoritative.

## Non-goals

- No removal of exported `ConsumeWithHeartbeat` at this layer, and therefore no zero-production-caller archive gate
  here: that gate belongs to the #1249 removal layer (owner ruling 2026-09-18 on #759).
- No API allowlist or compatibility status derived from the zero-growth ratchet.
- No mechanical ACK conversion.
- No raw-message settlement escape or unreviewed exported no-heartbeat API.
- No intermediate state that advertises two settlement APIs: `main` carries the legacy helper unadvertised and
  ratcheted until #1249 deletes it. (The earlier "no child-PR merge directly to `main`" restriction belonged to the
  abandoned integration-trunk topology and is superseded 2026-09-18; every layer now merges to `main` on its own.)
- No shared admission gate, handle owner, supervisor, rule, workflow, state-machine runtime, checkpoint, outbox,
  CQRS layer, or event-sourced loop.
- No durable quarantine bucket, stream, subject, payload, entity, ObjectStore record, or unapproved AgentRun receipt
  ledger.
- No generic gated-DAG nil/error definition of done or universal heartbeat API in the domain capability.
- No claim that deterministic message-ID deduplication provides unbounded exactly-once delivery.
- No sister-repository mutation.

# Change: semantic JetStream settlement

## Why

Five direct owners represent nine heartbeat delivery bindings. The current helper infers settlement from nil/error,
uses hidden fixed retry delays, lets cancellation replace a joined work result, and cannot tell the exact delivery
owner when heartbeat control has become unsafe.

The bindings also do not share one definition of done. Tools and dispatch have bounded durable consequences that can
be encoded now. Model, loop, and AgentRun erase or lack evidence needed to prove restart-safe replay, so implementation
must not invent their policy.

JetStream already owns durable delivery and restart redelivery. Components already own exact native handles. A shared
supervisor, lifecycle gate, or durable quarantine store would duplicate those authorities.

## What changes

- Add a validated ACK, Retry, Terminate, or Quarantine decision plus error-last work contract.
- Add explicit immediate or fixed-delay semantic retry policy, independent of consumer BackOff.
- Validate heartbeat policy from the exact consumer configuration before acquisition.
- Add an inspectable result that preserves semantic, heartbeat-control, and local settlement evidence.
- Observe JetStream delivery number at the native-message boundary and pass an immutable,
  settlement-authority-free `DeliveryAttempt` to typed work.
- Fail closed before payload access or work when delivery metadata is unavailable, using typed
  `delivery_metadata_unavailable` quarantine and the existing exact-owner stop path.
- Make heartbeat control loss require exact-owner shutdown without moving lifecycle into natsclient.
- Add permanent `ConsumeDeliveryWithHeartbeat` while held callers retain characterized legacy behavior.
- Migrate tools and dispatch first; require process-replacement proof in #1155.
- Keep model, each loop binding, and each AgentRun binding non-authorizing until fresh reviewed addenda receive named
  owner acceptance.
- Remove the zero-adopter builder after Stage A proof; remove the legacy helper only at the approved zero-caller gate.

## Scope

#759 owns the shared foundation and exactly nine heartbeat bindings:

- model: one;
- tools: one;
- dispatch complete/failed: two;
- loop task/response/tool-result: three; and
- AgentRun complete/failed: two.

Stage A is the foundation, tools, and dispatch. The remaining six bindings stay in scope but are not executable work
until their individual evidence gates pass. PR #1148 clears only AgentRun's file collision.

The other 22 production raw bindings and two examples remain #1145 production scope. OTEL remains unchanged and no
pull settlement API is proposed.

## Impact

- Modified capabilities: `jetstream-consumer-policy`, `nats-streaming`.
- Additive pre-v1 natsclient API plus later removal of two zero/contained-adopter exports.
- Tools heartbeat default changes from 120 seconds to 5 seconds while BackOff remains 15/60 seconds.
- Model and loop defaults change only when their separately held migrations are approved.
- Required breaking gate: `task e2e:agentic` after every admitted stage plus #1155 process-replacement evidence.
- Two measured SemDev legacy callers require a SemStreams-owned migration record before final legacy removal.
- `DeliveryWork` gains one value argument. The three Stage A policy bindings and focused tests migrate together;
  measured external typed adopters are zero.

## Non-goals

- No exported `SettleDelivery` or OTEL/pull settlement API.
- No shared admission gate, handle owner, supervisor, rule, workflow, or state machine.
- No durable quarantine bucket, stream, subject, payload, entity, or ObjectStore record.
- No inference of semantic retry timing from consumer AckWait or BackOff.
- No universal `DoubleAck` contract or claim of server confirmation.
- No production migration of the other 22 bindings or two examples.
- No sister-repository mutation.
- No native message, header, reply, stream sequence, consumer sequence, stream identity, consumer identity, or
  settlement method escapes through `DeliveryAttempt`.
- Delivery number is an observation, not proof that a prior invocation started or committed an effect.

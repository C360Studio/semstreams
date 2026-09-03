# Change: agentic-loop restart-safe settlement

## Why

Agentic-loop persists current loop state but retains material execution and correlation state only in process memory.
Several durable-input handlers also turn missing correlation or downstream failure into a normal callback return. A
replacement process can therefore ACK an input whose required durable transition, result, or downstream publication
did not complete, leaving a loop stranded or silently losing the transition.

This owner-classified critical beta.163 vertical begins implementation from exact post-#1251 checkpoint
`P=09ba38b1de5e7200e72281c8e4b8941d81be1da2`, whose merge base with the frozen staged #759 parent is exact
`F=417beae5552f8f15ad3540edd7d8504c87174c13`. PR #1159 targets `codex/gh759-semantic-settlement`; #759 does not
merge first.

The first-party publisher addendum at the same checkpoint is independently accepted at SHA-256
`0adba4f0092017d84f1ef181ebaf3299323f5cc75b999825bd1e16d6e292930f`, 226/226 pins. It closes the
framework-owned rule `publish_agent` producer seam that feeds the row-15 `agent.task.*` subscription.

## Claim scope

The full accepted scope is all 17 physical durable-input subscriptions across user-message intake and commands,
dispatch projections and terminal routing, governance validation and verdict correlation, model invocation, loop
task/response/tool-result transitions, cancel signals, approval responses, tool execution, replay admission, and
context/lifecycle closure. Model plus loop task/response/tool-result migration onto the staged typed settlement
foundation is additive to that scope.

AgentRun complete/failed fanout is transferred intact to #1249 from the exact post-#1146 staged checkpoint `A`.
#1146 neither narrows nor ratifies its partial-fanout behavior.

## Holds

- The remote #759 parent remains frozen at exact `F` while #1159 implements, proves, and receives review.
  Any unexpected parent advance requires a new pin, rebase, inventory verification, test, and re-review.
- Ten physical fast subscriptions begin with strict per-subscription AckWait 30s, work budget 25s, and join margin
  5s. Only a subscription whose legitimate context-cooperative work fails that boundary proof may migrate to the
  admitted typed heartbeat owner. Proof-group membership never authorizes sibling migration.
- Dispatch fallback is bounded work 30s / heartbeat 10s. Loop fast fallback is bounded work 120s / heartbeat 15s.
  Governance fallback is bounded work 30s; accepted evidence proves only a heartbeat ceiling of 15s, so a concrete
  value requires review if and only if a governance subscription triggers fallback.
- Native messages and settlement methods remain inside private binding owners. No exported no-heartbeat settlement
  API or raw settlement escape is added.
- Model uses AckWait 120s / heartbeat 60s. Loop task/response/tool-result use heartbeat 15s against shortest BackOff
  30s. Loop MaxDeliver is at least the fixed two-entry BackOff length, so omitted/zero defaults to 2 and explicit 1
  refuses before allocation. Exact acquisition configuration is validated before consumer allocation.
- The first model or loop delivery-owner fatal result latches into the existing negative health/error-count surface
  before exact-handle drain. Metadata loss performs no work, heartbeat, or settlement, and later fatal observations
  neither overwrite the first cause nor recount it. No new health surface or state authority is added.
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
- Three current-spec truths are replaced through full MODIFIED requirements: dispatch treats `AGENT_LOOPS` as
  authority and `LoopTracker` only as a projection; tool completion/replay uses framework execution identity rather
  than provider `ToolCall.ID`; and terminal loop release requires exact applied proof for late deliveries rather than
  unconditional quiet settled-drop.
- Model config adds exact `provider_ambiguity_policy` with default `fail_commit_unknown`; unsupported
  `provider_reconcile` refuses before consumer allocation. `AgentResponse.failure_kind` is a closed optional JSON
  string whose only new value is `provider_commit_unknown` on error responses.
- Approval continuation is registered as `agentic.approval_continuation.v1` with the control indexing floor and uses
  exact config key `approval_continuation_storage_instance` (default `objectstore`), not the trajectory-evidence key.

## Impact

- Tracking issue: #1146; parent epic: #1147.
- Staged prerequisite: #759; PR #1159 is stacked on its non-default branch.
- AgentRun successor: #1249 from post-#1146 checkpoint `A`; transition-contract successor: #1244.
- #1239 provenance remains with merged nested PR #1251; default-branch closing authority remains in PR #1156.
- Blocks restart-safe approval/enforcement claims in #1140.
- Capability deltas: `agentic-dispatch`, `agentic-loop`, `agentic-model`, `agentic-tools`, `agentic-governance`, and
  `rule-agent-publishing`.
- Verification includes the #1146-owned tranche of #1155's real-NATS process-replacement matrix and serialized
  agentic E2E. #1155 remains open until #1249 supplies transferred AgentRun complete/failed proof; the combined
  matrix gate is completed later.

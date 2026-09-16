# R8 local admission — incomplete evidence checkpoint

base: c347eff487f50b93bc338d764f43ef5b5ea5e133

Worktree: `/Users/coby/Code/c360/semstreams-wt/codex/gh1146-agentic-loop-restart`.
Branch/upstream identity and unchanged live-match manifest were verified by root.
Architect read-only checkpoint: no source/document/Git writes or runtime tests; no R8 readiness claim.
Root materialized the handoff and adapted list/heading formatting for the existing inventory verifier.

## Adjacent claims — preserved inventories
- Prerequisite SHA `4b3dc5f0131e5069623e6d231d8174eb78ae5f6ccad00bb386cab674aca6302d`; current verifier: **41/41 exact**.
- Retained-verdict SHA `8b73908fd59f17708f9c5602fd9c4be0f4bb532f4c30bab792deade735c09a1c`; unchanged identity, not re-enumerated.
- Current proposal SHA `23c732653d3aa5358d4acb6ac381e3b115206689e619e784f88db8cc5b7b6bf4`.
- Current design SHA `f61f471f86ecb43049688bba59e76d829f0639862331458d80974360ebedf358`.
- Tasks intake SHA `88ace259ea06cf33b40c9431abc5018069ed5e8a13ac9a7b79a4c097292b4a41`.

## Strongest measured facts

- `configs/agentic.json:17` — `"agent.>"`
- `configs/agentic.json:19` — `"max_age": "24h",`
- `configs/agentic.json:20` — `"max_bytes": 268435456,`
- `configs/agentic.json:21` — `"discard": "old"`
- `openspec/changes/agentic-loop-restart-safety/design.md:869` — `first dependent consumer, subscription, observer, worker, or publisher success. Model, dispatch, governance, and loop`
- `openspec/changes/agentic-loop-restart-safety/proposal.md:152` — `reconciliation capability, endpoint census, ledger, outbox, or provider dependency on AGENT replay admission.`
- `openspec/changes/agentic-loop-restart-safety/specs/agentic-model/spec.md:110` — `ambiguity policy, or replay-admission prerequisite is admitted.`
- `processor/agentic-model/component.go:303` — `if err := c.setupConsumer(runCtx, port); err != nil {`
- `processor/agentic-model/component.go:397` — `return c.handleRequest(workCtx, data)`
- `processor/agentic-model/component.go:389` — `MessageTimeout: 30 * time.Minute,`
- `processor/agentic-model/component.go:1037` — `if d, ok := tryParse(req.Timeout, "task"); ok {`
- `processor/agentic-dispatch/component.go:543` — `MaxDeliver:    3,`
- `processor/agentic-dispatch/component.go:575` — `MaxDeliver:    0,`
- `processor/agentic-dispatch/component.go:614` — `MaxDeliver:    0,`
- `processor/agentic-dispatch/component.go:530` — `terminalRetryPolicy, err := natsclient.DelayedDeliveryRetry(30 * time.Second)`
- `natsclient/stream.go:37` — `// MaxDeliver is the maximum number of delivery attempts (0 = unlimited).`
- `natsclient/stream.go:826` — `consumerCfg.AckWait = 30 * time.Second // Default`
- `natsclient/stream.go:617` — `messageTimeout = 30 * time.Second`
- `processor/agentic-loop/component.go:1019` — `ackWait = c.config.Consumer.ParsedAckWait()`
- `processor/agentic-loop/component.go:1023` — `backOff = []time.Duration{30 * time.Second, 2 * time.Minute}`
- `processor/agentic-governance/component.go:463` — `streamName := stream.Name()`
- `processor/agentic-governance/component.go:432` — `outputSubject, resolveErr := component.ResolveSubject(c.outputPortDefs(), outputPortName, msg.ID)`
- `processor/agentic-model/component.go:1091` — `subject, err := component.ResolveSubject(c.outputPortDefs(), "agent.response", resp.RequestID)`
- `natsclient/client.go:1005` — `_, err = js.PublishMsg(ctx, msg)`
- `processor/rule/processor.go:1216` — `if ackErr := msg.Ack(); ackErr != nil {`

The shipped declaration is demonstrably DiscardOld, not the accepted DiscardNew posture. It is not a measurement of a running server.
Input stream identity and output subject identity have separate existing resolution homes. The example AGENT declaration covers `agent.>`; universal source/output co-location was not established.
Dispatch terminal retries are unlimited while its USER lane is finite. Governance and rule acquisition literals omit AckWait/BackOff/MessageTimeout; the shared wrapper supplies defaults.
These configured deadlines are not proof of a maximum elapsed recovery need, nor proof that arbitrary callback work finishes by cancellation.
Rule currently calls handleMessage and then ACKs; action publication's PubAck path does not establish the separately held #1311 source-settlement contract.

## Adjacent claims — owner authority supplied by root's live reads

- [5550778818](https://github.com/C360Studio/semstreams/issues/1146#issuecomment-5550778818), 2026-09-05: explicitly removes provider replay-horizon arithmetic and dependency on AGENT replay admission. Absence permits reinvocation under at-least-once semantics.
- Therefore the blanket model inclusion is stale contract text, not a new model policy choice. Refusing model startup before its request consumer would actually block provider invocation; naming the check “startup” does not remove that dependency.
- [5463183450](https://github.com/C360Studio/semstreams/issues/1146#issuecomment-5463183450): accepts reviewed `b8e2c031`, observed DiscardNew, full-stream backpressure, and readiness refusal without mutation.
- [5654729986](https://github.com/C360Studio/semstreams/issues/1146#issuecomment-5654729986): preserves admission/DiscardNew while expressly allowing evidence disappearance before approval deadline.
- Bounded intake remains [5679435736](https://github.com/C360Studio/semstreams/issues/1146#issuecomment-5679435736).

## Searches and measurement record

- Fully read current proposal/design/tasks and applicable deltas under bounded intake; unchanged historical inventories were preserved.
- `task inventory:verify -- openspec/changes/agentic-loop-restart-safety/inventory-r7-replay-admission-prerequisite-2026-09-15.md` → 41 exact, zero moved/drift.
- gopls environment: `GOPLSCACHE=/private/tmp/semstreams-r7-gopls-cache GOCACHE=/private/tmp/semstreams-r7-test-cache GOPROXY=off GOSUMDB=off GOFLAGS=-mod=readonly`.
- `workspace_symbol -matcher=caseSensitive` searches: each named component's `Start`, model `resolveTimeout`, dispatch `setupSubscriptions`/`resolveAndWaitForSubscriptionBindings`, governance `setupConsumer`, rule `setupSubscriptions`/`actionPublisher`, and model/dispatch/governance `Health`.
- Same structural command: `StreamConsumerConfig`, `GetConsumerConfig`, `Client.buildConsumerConfig`, `Client.PublishToStream`, `Client.observePortConsumerPolicy`.
- `ValidateStreamBounds`, `validateDispatchStream`, `StreamConfigError`, and `agenticdispatch.Component.publishTask` yielded zero under those exact searches; no broader absence claim.
- Fuzzy `StreamBounds` output was truncated and used only to locate `CheckStreamBounds`; `DispatchStreamRetention` located gated-DAG declarations.
- `gopls references natsclient/stream.go:66:2` located shared MessageTimeout application and loop/model/tools configurations.
- `gopls call_hierarchy natsclient/stream_bounds.go:82:6` located CreateStream, EnsureStream, auto-create and test callers.
- `git grep -n -E 'agentstreamadmission|ObserveAndValidate|agent_stream_replay_inadmissible' -- '*.go'` → zero tracked matches.
- String searches covered `AGENT`, discard/retention/size bounds in `config/streams.go` and `configs/agentic.json`; timeout/consumer keys in the four agentic configs; retention/policy/Info in gated-DAG. Truncated locators were not completeness evidence.
- `rg --files` located preserved prerequisites; numbered range reads and `shasum -a 256` measured relevant owners. One attempted `natsclient/stream_consumer.go` read failed: file absent; gopls located `stream.go`.
- A tracked `git grep` against the untracked retained inventory returned zero and was not used as content-absence evidence.

## Adjacent claims — explicitly unproven remainder

- No executable local horizon or safety-margin formula has been established from the measured settings.
- Unlimited MaxDeliver defeats a finite attempt-count formula; it does **not** establish that retention-scoped safety is impossible. MaxAge bounds source availability, and later output in the same stream may survive its source.
- Exact source/output stream relationships and evidence-age ordering for every relevant owner remain incomplete.
- Startup observations of actual StreamInfo, all early-eviction fields, and the precise maximum local replay need remain unproven.
- Surface, same-class collision, and adopter inventories are incomplete: existing provisioning checks, post-allocation consumer-policy observation, classified authority refusal, and component health were located, but their complete integration/adopter implications were not closed.
- No new API, universal deadline, state, recovery mechanism, or policy has been proposed. Source-error propagation remains coupled to durable authority; #1311/#1312 and frozen #1156 remain unchanged.
- This stops at incomplete evidence under the architect contract and the applied SemStreams development/orchestration skills; it is not an implementation handoff.

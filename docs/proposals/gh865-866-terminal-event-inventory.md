# GitHub #865/#866 Terminal Event Inventory

Baseline: `6eb8646992b55aa3c08a695e89db4bfea6b3b000`

Phase: `inventory-only`

Body SHA-256: `ae27e5111ee10e531ffe90c4505687367ea534e80c816bc401bf4b7168804676`

## Inventory body

No files were changed and no tests were run during the read-only inventory. The worktree was clean.

### Baseline

- Inventory baseline: `6eb8646992b55aa3c08a695e89db4bfea6b3b000`.
- Compared with `v1.0.0-beta.160`; the only touched file across the inventoried terminal surface is
  `processor/agentic-loop/approval_integration_test.go`. The #865/#866 runtime paths are unchanged since beta.160.
- Current issue state observed through `gh`:
  - #865: open success-outcome/result projection defect.
  - #866: open cancellation terminal-event decode/drop defect.
  - #857 remains the adjacent payload-bound/object-storage program; its beta.160 ledger treats terminal
    result-by-reference as a separate slice.
  - `durable-tool-call-outcomes` explicitly excludes changes owned by #865/#866 at
    `openspec/changes/durable-tool-call-outcomes/proposal.md:29-33`.

### Mandatory surface inventory

#### Vocabulary and event types

- Message categories are distinct and typed:
  - `loop_completed`, `loop_failed`, `loop_cancelled`: `agentic/constants.go:9-27`.
- Outcome vocabulary is:
  - `success`, `failed`, `cancelled`, `truncated`: `agentic/constants.go:37-43`.
- Adjacent lifecycle vocabularies use different success spellings:
  - loop state `complete`: `agentic/state.go:11-42`.
  - model response status `complete`: `agentic/constants.go:45-51`.
  - trajectory status `completed`: `agentic/trajectory_fact.go:85-97`.
  - agent-run phase `completed`: `agentic/agentrun/agentrun.go:39-53`.
- The three terminal payloads are separate concrete types:
  - `LoopCompletedEvent`: `agentic/events.go:59-114`.
  - `LoopFailedEvent`: `agentic/events.go:116-181`.
  - `LoopCancelledEvent`: `agentic/events.go:183-231`.
- Cancellation lacks `Role`, `Model`, result/error/reason, and user-routing fields present on success/failure:
  `agentic/events.go:59-86`, `:116-153`, `:183-203`.
- All three `Validate` methods require only loop and task IDs; they do not validate category/outcome agreement:
  `agentic/events.go:88-97`, `:155-164`, `:205-214`.
- All three payloads are registered: `agentic/payload_registry.go:29-32`.

#### Production wire shape

- Production `BaseMessage` serializes:
  - `id`
  - nested `type: {domain, category, version}`
  - `payload`
  - `meta`

  at `message/base_message.go:207-249`.
- Production decoding requires a registry and constructs the concrete payload from the nested type discriminator:
  `message/base_message.go:252-319`.
- Dispatch receives that registry-aware decoder during construction: `processor/agentic-dispatch/component.go:206-219`.

#### Producers and ordering

Success:

- Transitions loop state to `complete`, records outcome `success`, and constructs
  `LoopCompletedEvent{Outcome: OutcomeSuccess, Result: responseContent}`:
  `processor/agentic-loop/handlers.go:1952-1988`.
- Wraps the event in `BaseMessage`, resolves `agent.complete.<loopID>`, and adds it to the publication result:
  `processor/agentic-loop/handlers.go:2037-2053`.
- Terminal persistence ordering is trajectory observation -> loop state/`COMPLETE_` -> graph stamp -> publish:
  `processor/agentic-loop/component.go:1458-1501`.

Failure:

- Builds `LoopFailedEvent` with `OutcomeFailed`: `processor/agentic-loop/handlers.go:2545-2586`.
- Publishes it on `agent.failed.<loopID>`: `processor/agentic-loop/handlers.go:2594-2615`.
- Failure path persists loop/`COMPLETE_`, stamps graph, then publishes:
  `processor/agentic-loop/component.go:1307-1385`.

Cancellation:

- Builds `LoopCancelledEvent{Outcome: OutcomeCancelled}` and publishes it on `agent.complete.<loopID>`:
  `processor/agentic-loop/component.go:1958-2028`.
- Its ordering differs: loop entity and trajectory first, terminal event publication, graph stamp, then `COMPLETE_` KV:
  `processor/agentic-loop/component.go:1983-2036`.

Ports:

- Agentic-loop declares both `agent.complete.*` and `agent.failed.*`:
  `processor/agentic-loop/config.go:425-463`.
- Dispatch declares `agent.complete.*` required and `agent.failed.*` optional:
  `processor/agentic-dispatch/config.go:62-94`.
- Checked runtime config retains `agent.>` for 24 hours with a 256 MiB stream bound and discard-old:
  `configs/agentic.json:15-23`.

#### Dispatch projection: #865 and #866

- Dispatch installs separate durable, explicit-ack, DeliverNew consumers:
  - completion: `processor/agentic-dispatch/component.go:402-424`.
  - failure: `processor/agentic-dispatch/component.go:450-472`.
- Each callback invokes a void handler and then ACKs unconditionally, independent of decode acceptance, tracker update,
  or downstream user-response publication: `processor/agentic-dispatch/component.go:412-417`, `:460-465`.

#865:

- The completion handler correctly updates the tracker through the package's canonical `success -> complete` mapping:
  `processor/agentic-dispatch/component.go:821-838`; mapping at
  `processor/agentic-dispatch/loop_tracker.go:399-420`.
- The subsequent user-response switch tests the raw outcome against `"complete"` rather than
  `agentic.OutcomeSuccess`/`"success"`: `processor/agentic-dispatch/component.go:846-866`.
- Consequently a production success reaches the default branch and emits `ResponseTypeStatus` with
  `Loop <id>: success`, rather than `ResponseTypeResult` carrying `completion.Result`.
- Response types and their public meaning are defined at `agentic/user_types.go:168-176`.

#866:

- `agent.complete.*` legally carries both `LoopCompletedEvent` and `LoopCancelledEvent`.
- `handleAgentComplete` asserts only `*agentic.LoopCompletedEvent`:
  `processor/agentic-dispatch/component.go:797-811`.
- A cancellation is logged as unexpected, returns, and is then ACKed by the subscriber callback.
- The `"cancelled"` and `"failed"` cases later in the same switch are unreachable for production
  cancellation/failure because the concrete assertion precedes them and failure has a separate lane:
  `processor/agentic-dispatch/component.go:846-866`.
- No dispatch cancellation handler exists.

Tracker/user-response consequences:

- The tracker is process-local and constructed fresh: `processor/agentic-dispatch/component.go:207-214`.
- Unknown-loop terminal messages return without projection: `processor/agentic-dispatch/component.go:814-819`,
  `:961-966`.
- Both terminal consumers are DeliverNew, so restart does not rebuild tracker state from retained earlier creation
  events.
- `sendResponse` is void; marshal, subject-resolution, and publication errors are logged only:
  `processor/agentic-dispatch/component.go:1020-1040`.
- A successful terminal event can therefore be ACKed even if `user.response.*` is never published.
- Each response gets a fresh UUID: `processor/agentic-dispatch/component.go:1056-1065`; there is no stable
  idempotency key on this projection.

Metrics:

- Dispatch records the raw successful event outcome `success`: `processor/agentic-dispatch/component.go:840-844`.
- Metric tests assert the separate spelling `completed`:
  `processor/agentic-dispatch/metrics_test.go:265-306`.
- The counter accepts arbitrary status labels: `processor/agentic-dispatch/metrics.go:117-122`, `:308-311`.
- Cancellation is recorded by agentic-loop through its failed-loop metric path, reason `cancelled`, with duration
  status `failed`: `processor/agentic-loop/component.go:1986-1990`;
  `processor/agentic-loop/metrics.go:432-438`.

#### Adjacent producer settlement/publish-loss behavior

- Agentic-loop's generic terminal publisher logs stream publication failures and returns no error:
  `processor/agentic-loop/component.go:1695-1709`.
- Failure publication likewise logs and continues: `processor/agentic-loop/component.go:1375-1385`.
- Cancellation returns early on terminal-event publication failure, before graph and `COMPLETE_` writes:
  `processor/agentic-loop/component.go:2023-2036`.
- Response/tool-result/signal handlers are adapted through `adaptVoidInputHandler`, which always returns nil:
  `processor/agentic-loop/component.go:157-161`, `:783-804`.
- Therefore the source-consumer settlement machinery cannot observe these terminal publication failures; the input
  may ACK despite a missing terminal event.
- These are adjacent delivery facts, not part of either issue's stated outcome/type mismatch.

#### Additional same-class finding: `agentrun` production-envelope incompatibility

- ADR-053 defines one semantic terminal union and explicitly requires category demux because cancellation rides
  `agent.complete`: `docs/adr/053-agent-run-substrate.md:188-204`, `:224-231`.
- `agentrun.LoopTerminalEvent` exposes that normalized category/outcome shape:
  `agentic/agentrun/agentrun.go:368-386`.
- `MilestoneSubscriber.HandleEvent` claims to decode a `BaseMessage`, but actually expects flat top-level `domain`,
  `category`, and `version`: `agentic/agentrun/agentrun.go:460-479`.
- Production has those values nested under `type`, so a real producer envelope leaves `envelope.Category` empty and
  reaches the silent default return: `agentic/agentrun/agentrun.go:480-533`.
- A nil result is ACKed: `agentic/agentrun/agentrun.go:669-679`.
- Its tests fabricate the same non-production flat envelope instead of using `message.NewBaseMessage`:
  `agentic/agentrun/agentrun_test.go:682-695`.
- The subscriber's two durable consumers are at `agentic/agentrun/agentrun.go:682-706`.
- Both framework binaries wire it, and semteams beta.159 also wires its version at
  `/Users/coby/Code/c360/semteams/cmd/semteams/main.go:946-961`.
- This corrects the prior inventory's statement that agentrun successfully demuxes production envelopes
  (`docs/proposals/agentic-trajectory-contract-inventory.md:183-187`, `:359`).

#### Other concrete terminal readers

- Dispatch KV/activity projection uses a minimal structural union for all three raw `COMPLETE_` terminal shapes:
  `processor/agentic-dispatch/loop_wire.go:101-147`; activity entry at
  `processor/agentic-dispatch/http_activity.go:19-66`.
- HTTP/SSE documents terminal outcome as `success|failed|cancelled`, with no terminal `state`:
  `processor/agentic-dispatch/http.go:507-528`, `:1137`.
- `flow_monitor` category-discriminates the three raw terminal shapes:
  `processor/agentic-tools/flow_monitor_executor.go:103-182`.
- `read_loop_result` always decodes `COMPLETE_` as `LoopCompletedEvent`:
  `processor/agentic-tools/loop_result.go:101-161`.
- OTel correctly reads the production nested discriminator and category-demuxes all three:
  `output/otel/span_collector.go:182-240`; its consumer ACKs even processing errors.
- Research graph synthesis is a fourth `COMPLETE_` writer and stores a BaseMessage-wrapped `SearchResult`:
  `processor/research-graph-synthesize/component.go:380-419`,
  `processor/research-graph-synthesize/adapters.go:146-171`.

### Same-class collision inventory

#### Terminal graph-fact definitions and owner

- `agent.loop.outcome` is defined as a terminal string predicate with documented values `success`, `failed`, and
  `cancelled`: `vocabulary/agentic/predicates.go:390-398`; registered as `string`:
  `vocabulary/agentic/register.go:433-437`.
- `agent.loop.ended-at` is defined as the completion, failure, or cancellation timestamp:
  `vocabulary/agentic/predicates.go:494-497`; registered as `time.Time`:
  `vocabulary/agentic/register.go:495-497`.
- The retired underscore spelling appears only in the rename ledger and one test error string:
  `docs/operations/24-predicate-breaking-rename-ledger.md:58`,
  `processor/agentic-loop/graph_writer_integration_test.go:688`.
- `agentic-loop` is the semantic author. Its writer source is `"agentic-loop"` and it submits append requests through
  the canonical graph-mutation client: `processor/agentic-loop/graph_writer.go:22-24`, `:80-120`.
- `graph-ingest` is the sole physical `ENTITY_STATES` writer; any component may request an admitted mutation, and no
  predicate claims, leases, heartbeats, or runtime semantic-owner enforcement remain:
  `docs/adr/091-graph-mutation-authority-without-semantic-ownership.md:17-20`, `:38-45`.
- `ENTITY_STATES` is authoritative current state, owner-created by `graph-ingest`, history 1,
  `RetentionNoLifecycle`, and `WriteOwnerOnly`; enforcement is call-site/review based rather than caller-identity
  enforcement: `graph/kvcatalog.go:1-17`, `:37-67`.

#### Success, failure, and cancellation graph writers

- Success graph facts write `agent.loop.outcome=event.Outcome` and `agent.loop.ended-at=CompletedAt`:
  `processor/agentic-loop/graph_writer.go:255-284`, `:541-583`.
- Failure graph facts write `agent.loop.outcome=event.Outcome`, `agent.loop.ended-at=FailedAt`, and optionally
  `agent.loop.terminal-reason`: `processor/agentic-loop/graph_writer.go:286-309`, `:585-637`.
- Cancellation graph facts write only `agent.loop.outcome=event.Outcome` and
  `agent.loop.ended-at=CancelledAt`: `processor/agentic-loop/graph_writer.go:476-497`, `:639-662`.

#### Catalog inventory

- Terminal payload categories and values are centrally declared at `agentic/constants.go:9-27`, `:37-43`; the three
  event structures and schemas are at `agentic/events.go:59-114`, `:116-181`, `:183-231`.
- All three terminal payloads are in the payload registry: `agentic/payload_registry.go:19-44`, specifically
  `:30-32`; builtin boot registration invokes that catalog: `payloadbuiltins/register.go:41-46`.
- Component factories are catalogued as `agentic-loop` and `agentic-dispatch`:
  `processor/agentic-loop/factory.go:7-25`, `processor/agentic-dispatch/factory.go:20-30`.
- Agentic-loop declares `AGENT_LOOPS`, `graph_mutations`, `agent.complete.*`, and `agent.failed.*` output surfaces:
  `processor/agentic-loop/config.go:425-463`.
- Dispatch declares required `agent.complete.*`, optional `agent.failed.*`, and `user.response.>`:
  `processor/agentic-dispatch/config.go:62-99`.
- Generated component-schema catalog entries exist at `schemas/agentic-loop.v1.json:3-6`, `:162-173` and
  `schemas/agentic-dispatch.v1.json:3-6`, `:96-105`; OpenAPI references both at
  `specs/openapi.v3.yaml:1258-1264`.
- The checked runtime stream catalog configures `AGENT` over `agent.>` with 24-hour age, 256 MiB maximum bytes, and
  discard-old: `configs/agentic.json:15-23`.

#### Replacement same-class collision table

| Semantic class / surface | Owners and writers | Catalogs | Status surfaces | Lifecycle | Readers | Ownership | Recovery and collision evidence |
|---|---|---|---|---|---|---|---|
| Canonical loop terminal graph facts: `agent.loop.outcome`, `agent.loop.ended-at` | `agentic-loop` success, failure, and cancellation builders submit graph mutations: `processor/agentic-loop/graph_writer.go:255-309`, `:541-662` | Vocabulary definitions and registrations above; graph mutation output port at `processor/agentic-loop/config.go:434-438` | No predicate-specific readiness. Completion/failure budget expiry has `graph_write_publish_timeout_total{state}`: `processor/agentic-loop/metrics.go:221-226`, `:366-371`. Loop health only reflects started state and trajectory degradation: `processor/agentic-loop/component.go:383-414` | `ENTITY_STATES` current authority, history 1, no lifecycle expiry: `graph/kvcatalog.go:37-67` | Rule configs, generic rule watcher, ops persona, graph API, and E2E | Semantic author: `agentic-loop`; physical KV writer: `graph-ingest`; no runtime predicate authority: ADR-091 evidence above | Success/failure publication continues after graph-budget expiry; cancellation publishes before graph write and returns early on publish failure. No repair/reconciliation path was found |
| `AGENT_LOOPS/<loopID>` | Agentic-loop persists `LoopEntity`: `processor/agentic-loop/component.go:1315-1321`, `:1480-1482`, `:1983-1984` | `loops` KV output: `processor/agentic-loop/config.go:433-435` | Dispatch HTTP `/loops` and activity SSE project state; loop and dispatch active-loop gauges are process metrics | Bucket is created with history 10 and TTL 24 hours: `processor/agentic-loop/component.go:680-693` | Dispatch HTTP/SSE and loop-state readers | Agentic-loop writes; this application bucket is outside the framework KV ownership catalog by rule: `graph/kvcatalog.go:6-9` | SSE reconstructs from current KV snapshot and live watch. Expiry removes both current and `COMPLETE_` keys |
| `AGENT_LOOPS/COMPLETE_<loopID>` | Success, failure, cancellation, plus research synthesis wrapped `SearchResult`: `processor/research-graph-synthesize/component.go:380-419`, `adapters.go:146-171` | Same `loops` KV port and bucket | HTTP activity exposes a structural terminal projection; no per-shape status | Same history-10/24-hour TTL | Dispatch activity structural union, `flow_monitor` union, and success-only `read_loop_result` | Multiple writers share one overwrite key | Restarting SSE replays current snapshot. Heterogeneous values collide by overwrite; success-only readers zero-fill other shapes |
| `agent.complete.<loopID>` / `agent.failed.<loopID>` | Agentic-loop publishes wrapped success/cancellation on complete and wrapped failure on failed | Payload registry, component ports, and `AGENT` stream catalog above | Loop metrics separate successful completion from failure; cancellation increments failure with reason `cancelled`: `processor/agentic-loop/metrics.go:76-109`, `:418-438`; `processor/agentic-loop/component.go:1986-1990` | AGENT stream retention is 24 hours/256 MiB/discard-old. Stable durables are normally retained on component Stop: `processor/agentic-loop/component.go:643-667` | Dispatch, AgentRun milestone subscriber, OTel, and product subscribers | Agentic-loop authors messages; each downstream durable independently owns acknowledgement state | Consumers use explicit ACK and `DeliverNew`; durable offsets preserve unacked work on ordinary restart, but a newly created durable does not replay older retained terminal events |
| Dispatch terminal projection and `LoopTracker` | Dispatch consumes terminal events, mutates a process-local loop map, and emits `UserResponse`: `processor/agentic-dispatch/component.go:797-874`, `:925-975` | Dispatch ports and schema above | `completions_received_total{status}` accepts raw status labels; active-loop gauge decrements on terminal handling: `processor/agentic-dispatch/metrics.go:11-19`, `:117-122`, `:293-310`. Health is only started/running: `processor/agentic-dispatch/component.go:255-276` | Process-local tracker; no retained reconstruction | HTTP loop APIs and user-channel adapters | Dispatch owns its projection; terminal stream remains external input | Completion/failure consumers use explicit ACK, `DeliverNew`, max-deliver 3, and ACK after void handlers regardless of decode, unknown-loop, or response-publication failure: `processor/agentic-dispatch/component.go:402-424`, `:450-472`. Stop retains durable consumers unless test cleanup is enabled: `:329-367` |
| Dispatch activity SSE | Dispatch projects `AGENT_LOOPS` once into a shared graph view | HTTP handler and activity decoder | View metrics expose revision, caught-up, watcher loss, poison, subscribers, and fan-out: `processor/agentic-dispatch/http_activity.go:68-118` | Lazy component-owned view; Stop closes it and nils it: `:121-190` | External HTTP/SSE adopters | Dispatch owns projection; agentic-loop owns source writes | Attach waits for caught-up state, retries a lost watcher once, then snapshot-replays before live deltas: `:193-215`, `:218-303`, `:305-338`. Watcher loss terminates clients rather than serving frozen state: `:340-383` |
| OTel span projection | OTel category-demuxes all three terminal payloads: `output/otel/span_collector.go:182-240`, `:280-362` | OTel input-port consumers | Span status is `ok` for completion and `error` for failure/cancellation; outcome is an attribute | Active spans are process memory; terminal updates require an existing created span: `output/otel/span_collector.go:243-310`, `:313-362` | OTel exporter | OTel owns span projection only | Consumers use explicit ACK and `DeliverNew`; processing errors increment a metric but are still ACKed: `output/otel/component.go:233-253`, `:259-293`. Restart loses active spans, and a terminal event without its creation event is ignored |
| AgentRun milestone projection | `MilestoneSubscriber` normalizes the three terminal categories for product handlers; it explicitly does not mutate lifecycle: `agentic/agentrun/agentrun.go:368-408` | `StartConfig` supplies stream and consumer suffix: `agentic/agentrun/agentrun.go:612-623` | AgentRun phase graph declares completed, failed, and cancelled terminal: `agentic/agentrun/agentrun.go:39-53`; milestone projection itself has logs but no phase/status mutation | Two stable durable consumers; Stop preserves offsets: `agentic/agentrun/agentrun.go:625-638`, `:712-716` | Product-registered milestone handlers | Lifecycle manager/coordinator owns phase; subscriber is read-only: `agentic/agentrun/agentrun.go:388-408` | Explicit ACK, `DeliverNew`, max-deliver 5; infrastructure decode errors NAK, but handler errors are logged and swallowed before ACK: `agentic/agentrun/agentrun.go:460-471`, `:669-705`. Absent stream disables subscriber at boot: `:643-658`. Existing flat-envelope decoder does not match production nested `type.category` |
| Typed channel response on `user.response.<channel_type>.<channel_id>` | Dispatch publishes `BaseMessage<UserResponse>` through USER JetStream: `processor/agentic-dispatch/component.go:1020-1041` | Payload registry; dispatch `user.response.>` output; USER stream retains one hour in checked configs | Dispatch logs publication failures; no delivery or reader status is coupled back to the terminal consumer | USER file-backed stream, one-hour age, 16 MiB, discard-old in checked configurations | No in-repo production reader. Semdev beta.160 broadly consumes the subject family but expects the flat rule envelope | Dispatch owns typed response projection; downstream adapters independently own delivery ACK state | Upstream dispatch input ACK is independent of response publication. Semdev ACKs the typed message as malformed; no repair/redrive exists |
| Rule wake-up / park notification on `user.response.<entity-instance>` | Rule engine emits flat `{entity_id,subject,timestamp,source,properties,...}`: `processor/rule/actions.go:872-920` | Generic `publish` action; USER `user.>` captures it even when core NATS is selected | Semdev park-post errors/health expose transport failures, but missing `entity_id` returns nil | Same USER retention when captured; semdev uses a durable `DeliverNew` consumer | Semdev park-post reader; semteams declares future routers but no current payload reader | Rule owns flat publish; semdev owns forge-post side effect | Semdev ACKs definitive malformed/no-channel/no-target cases and NAKs transient graph/forge failures. No message ID or side-effect dedup marker |
| Governance user error on `user.response.<channel>.<user>` | Governance emits flat `{type,timestamp,message,severity,details}`: `processor/agentic-governance/violation.go:192-216` | Optional core-NATS `user_errors` port `user.response.*`: `processor/agentic-governance/config.go:217-223` | Governance returns publish errors; no shape-specific delivery status | Core publication can still be captured by USER `user.>`; retention then follows USER | No shape-specific production reader found; semdev broad consumer also matches this family | Governance owns notification construction; downstream adapters own ACK | Semdev would classify it as missing `entity_id` and ACK. Violation ID is nested; no common discriminator/dedup contract |
| Immutable `loop.terminal` trajectory fact | Agentic-loop records success, failure, cancellation observations before adjacent terminal outputs | `trajectories` KV port: `processor/agentic-loop/config.go:425-431` | Audit failures can degrade loop health and have stage/kind/reason metrics: `processor/agentic-loop/metrics.go:378-415`; `processor/agentic-loop/component.go:398-405` | `AGENT_TRAJECTORIES` requires history 1 and no TTL: `processor/agentic-loop/component.go:695-760` | Trajectory query/read surfaces and agentic E2E | Agentic-loop owns the immutable observation | Best-effort audit evidence; current spec does not make it authority or use it to repair graph, stream, or `COMPLETE_` state |

#### Graph readers and graph-fact tests

- Shipped rules read `agent.loop.outcome` through configuration:
  - `configs/rules/example-fan-out/02-stamp-completion-on-parent.json:8-18`
  - `configs/rules/deep-research/01-spawn-researcher.json:8-18`
  - `configs/rules/deep-research/02-collect-evidence.json:8-18`
  - `configs/rules/deep-research/05-retry-insufficient.json:8-18`
  - `configs/rules/deep-research/06-timeout-partial.json:8-23`
  - `configs/rules/deep-research/07-spawn-coordinator.json:8-18`
- `05-retry-insufficient.json` compares outcome with `"insufficient"`, which is absent from the declared outcome
  constants at `agentic/constants.go:37-43`.
- The rule processor validates and replays authoritative `ENTITY_STATES` before evaluation, coalesces notifications,
  then fetches current state once: `openspec/specs/rule-entity-watching/spec.md:1-23`, `:67-80`. Its restart source is
  current graph state, not terminal-stream replay.
- The ops persona instructs an external model/tool user to query by `outcome` and `ended_at`, then use
  `read_loop_result`: `configs/personas/fragments/ops/00-identity.md:1-13`.
- Unit and integration tests require both predicates for success, failure, and cancellation:
  `processor/agentic-loop/graph_writer_test.go:165-195`, `:455-495`, `:650-675`;
  `processor/agentic-loop/graph_writer_integration_test.go:262-318`, `:433-470`, `:663-695`.
- Agentic E2E exercises `agentic-loop -> graph mutation -> graph-ingest -> ENTITY_STATES` and requires both facts:
  `test/e2e/scenarios/agentic/scenario.go:680-740`.
- Ops E2E directly puts an `ENTITY_STATES` value with raw `agent.loop.outcome` and omits `agent.loop.ended-at`:
  `test/e2e/scenarios/ops/scenario.go:421-460`.
- Vocabulary tests cover both constants and their datatypes: `vocabulary/agentic/agentic_test.go:73-85`, `:340-352`.

#### Exact collision closing searches

At baseline `6eb86469`, the production search for `LoopOutcome`, `LoopEndedAt`, both canonical predicate spellings,
and the retired underscore spelling returned only the two vocabulary definitions, their registrations, a writer
comment, and the success/failure/cancellation writer pairs in `processor/agentic-loop/graph_writer.go`. It returned no
production typed Go reader and no additional writer:

```text
rg -n --glob '*.go' --glob '!**/*_test.go' --glob '!test/**' \
  'LoopOutcome|LoopEndedAt|agent\.loop\.outcome|agent\.loop\.ended-at|agent\.loop\.ended_at' .
```

The following recovery search returned no matches; there is no predicate-specific repair, reconciliation, rebuild,
redrive, replay, or recovery implementation:

```text
rg -n --glob '*.go' --glob '!**/*_test.go' --glob '!test/**' \
  '(repair|reconcile|rebuild|redrive|replay|recover).*(LoopOutcome|LoopEndedAt|loop outcome|loop ended)|(LoopOutcome|LoopEndedAt|loop outcome|loop ended).*(repair|reconcile|rebuild|redrive|replay|recover)' .
```

The following ownership search returned no matches; there is no terminal-predicate claim, lease, heartbeat, or
semantic-ownership implementation:

```text
rg -n --glob '*.go' --glob '!**/*_test.go' --glob '!test/**' \
  '(Claim|Lease|Heartbeat|Ownership).*(LoopOutcome|LoopEndedAt|agent\.loop)|(LoopOutcome|LoopEndedAt|agent\.loop).*(Claim|Lease|Heartbeat|Ownership)' .
```

#### `user.response` wire and adopter collision

- `CategoryUserResponse` is `"user_response"`: `agentic/constants.go:9-15`.
- `UserResponse` carries response ID, channel/user/reply correlation, response type, content, optional blocks/actions,
  and timestamp: `agentic/user_types.go:168-198`. Validation requires response ID, channel type/id, and one of six
  declared response types: `agentic/user_types.go:200-217`; schema is `agentic.user_response.v1` at `:220-223`.
- The payload is registered at `agentic/payload_registry.go:19-25`; builtin registration calls
  `agentic.RegisterPayloads`: `payloadbuiltins/register.go:41-46`.
- Dispatch wraps the payload in a production `BaseMessage`, not a raw `UserResponse`:
  `processor/agentic-dispatch/component.go:1020-1027`. The wire therefore has top-level `id`, nested `type`,
  `payload`, and `meta`: `message/base_message.go:207-249`.
- Both payload `ResponseID` and BaseMessage ID are independently generated UUIDs:
  `processor/agentic-dispatch/component.go:1056-1065`, `message/base_message.go:121-128`.

Three writers share the subject family with incompatible wire shapes:

1. Dispatch funnels asynchronous errors/results, task acknowledgements, terminal success/failure, and synchronous HTTP
   command/task responses through `sendResponse`: `processor/agentic-dispatch/component.go:581-648`, `:698-789`,
   `:797-874`, `:925-975`, `:1020-1065`; `processor/agentic-dispatch/http.go:192-385`. Its default output is
   JetStream `USER` / `user.response.>` and resolves
   `user.response.<channel_type>.<channel_id>`: `processor/agentic-dispatch/config.go:85-94`,
   `processor/agentic-dispatch/component.go:1033-1039`.
2. Rule `publish` emits a flat `{entity_id,subject,timestamp,source,properties,related_id}` map without BaseMessage:
   `processor/rule/actions.go:872-927`. Publisher selection can use core NATS, which a USER `user.>` stream still
   captures: `processor/rule/publisher.go:26-76`.
3. Governance emits a third flat shape with top-level type/timestamp/message/severity/details on optional core-NATS
   `user.response.*`: `processor/agentic-governance/violation.go:192-216`,
   `processor/agentic-governance/config.go:217-223`.

Checked USER catalogs are file-backed `user.>` streams with one-hour age, 16 MiB maximum bytes, and discard-old:
`configs/flows/deep-research.json:77-85`; semdev beta.160 has the same shape at
`/Users/coby/Code/c360/semdev/configs/semdev-bootstrap.json:53-60`.

Semdev beta.160 collision:

- Semdev pins beta.160: `/Users/coby/Code/c360/semdev/go.mod:6`.
- Conversation-channel input and dispatch output both use JetStream `USER` / `user.response.>`:
  `/Users/coby/Code/c360/semdev/configs/semdev-bootstrap.json:318-326`, `:637-650`, `:687-695`.
- The beta.160 migration deliberately restored the dispatch component-default JetStream shape after strict port
  merging rejected a core-NATS override:
  `/Users/coby/Code/c360/semdev/openspec/changes/archive/2026-08-12-migrate-semstreams-beta160/design.md:241-247`.
- Semdev park rules publish flat rule envelopes on this family, including
  `/Users/coby/Code/c360/semdev/configs/rules/run-lifecycle/03-park-awaiting-human.json:14-27` and
  `/Users/coby/Code/c360/semdev/configs/rules/dev-from-task/08b-delivery-park.json:18-30`.
- The reader defines only `publishEnvelope{entity_id,properties}` and unmarshals the whole body into it:
  `/Users/coby/Code/c360/semdev/internal/conversationchannel/parkpost.go:36-47`, `:56-71`.
- A production BaseMessage is valid JSON, so unmarshal succeeds with empty `EntityID`; the reader logs malformed,
  returns nil, and the surrounding handler ACKs it:
  `/Users/coby/Code/c360/semdev/internal/conversationchannel/component.go:471-504`.
- Every park-post test supplies the flat shape; no BaseMessage/user-response fixture exists:
  `/Users/coby/Code/c360/semdev/internal/conversationchannel/parkpost_test.go:108-180`, `:205-249`.

Settlement, lifecycle, and recovery:

- Dispatch terminal and user-message consumers call void handlers and ACK independently of response publication:
  `processor/agentic-dispatch/component.go:377-424`, `:450-472`.
- `sendResponse` swallows marshal, resolution, and publication errors after logging:
  `processor/agentic-dispatch/component.go:1020-1041`.
- Semdev uses a durable, explicit-ACK, `DeliverNew` consumer with max-deliver 10, one-minute AckWait, eight pending,
  and three-minute handler timeout:
  `/Users/coby/Code/c360/semdev/internal/conversationchannel/component.go:414-470`.
- `ConsumeWithHeartbeat` ACKs nil, delayed-NAKs transient errors, and delayed-NAKs cancellation:
  `natsclient/heartbeat.go:37-48`, `:81-112`. The incompatible BaseMessage path returns nil and is permanently ACKed.
- An existing durable resumes unacked work; a new durable starts after retained history. USER retention bounds replay to
  one hour. Semdev Stop cancels its poll goroutine but does not delete durable consumers:
  `/Users/coby/Code/c360/semdev/internal/conversationchannel/component.go:510-525`; SemStreams preserves durable state
  when local consumption stops: `natsclient/stream.go:662-677`.
- No `user.response` repair, redrive, reconciliation, or post-side-effect dedup implementation was found. Once the
  typed delivery is ACKed, neither dispatch nor conversation-channel reconstructs it.

Reader/adopter census:

- SemStreams has no production `user.response` subscriber or `UserResponse` decoder.
- Among beta.160 adopters (`semboids`, `semconnect`, `semdev`, `semmachina`, `semsource`), only semdev references
  `user.response`, `UserResponse`, or `CategoryUserResponse`; its only production reader is the flat park-post reader.
- Semteams beta.159 writes flat rule envelopes on `user.response.$entity.instance` but states there is no UI consumer:
  `/Users/coby/Code/c360/semteams/configs/rules/coordinator/03-ask-user.json:25-43`,
  `03b-respond-direct.json:25-44`.
- No current OpenSpec or ADR requirement enumerates the `user.response` wire union; current docs name the subject and
  type separately: `processor/agentic-dispatch/README.md:151`, `agentic/README.md:74`.

Specific adopter seam: an external developer implementing a broad `user.response.>` adapter must know that one prefix
carries at least three unrelated shapes and must predict writer/wire shape from subject arity and deployment config.
If they do nothing, semdev demonstrates the default: typed responses parse as syntactically valid JSON, appear to have
an empty entity ID, and are ACKed as malformed. No common discriminator understood by all three shapes exists.

Exact closing searches:

```text
rg -n --glob '*.go' --glob '!**/*_test.go' \
  'user\.response|UserResponse|CategoryUserResponse' .
```

Production hits were limited to type/category/registry, dispatch response construction/writer, rule/governance
subject writers, and port/docs declarations.

```text
rg -n --glob '*.go' --glob '!**/*_test.go' \
  '(Consume|Subscribe|Handle|Decode|Unmarshal).*(user\.response|UserResponse)|(user\.response|UserResponse).*(Consume|Subscribe|Handle|Decode|Unmarshal)' .
```

No SemStreams production `user.response` consumer was returned.

Across the beta.160 adopter cohort, a repository-wide search for `user.response`, `UserResponse`, and
`CategoryUserResponse` returned only semdev. A production-Go-only search over semboids, semconnect, semmachina, and
semsource returned no matches. In semdev, this fixture search returned no matches:

```text
rg -n 'BaseMessage|user_response|response_id|channel_type|"payload"' \
  semdev/internal/conversationchannel/parkpost.go \
  semdev/internal/conversationchannel/parkpost_test.go
```

The semdev repair/dedup search over `internal`, `configs`, and `openspec/specs` also returned no runtime repair,
replay, redrive, or dedup implementation for `user.response`/park posts.

### Tests and coverage inventory

- Dispatch's helper creates a real production `BaseMessage`:
  `processor/agentic-dispatch/agent_complete_handler_test.go:16-72`.
- Success tests assert tracker state/outcome/result and active-loop clearing only:
  `processor/agentic-dispatch/agent_complete_handler_test.go:74-140`.
- Failure test asserts tracker state/error:
  `processor/agentic-dispatch/agent_complete_handler_test.go:142-181`.
- No test in that file asserts captured user-response type/content.
- No dispatch terminal-handler test exercises `LoopCancelledEvent`.
- KV projection cancellation is tested separately: `processor/agentic-dispatch/loop_wire_test.go:237-268`.
- Agentrun tests exercise cancellation/category demux but with the incompatible flat test envelope:
  `agentic/agentrun/agentrun_test.go:422-457`, `:682-695`.
- E2E closing search returned no references to `LoopCancelledEvent`, `handleAgentComplete`, `handleAgentFailed`,
  `user.response`, `agent.complete`, or `agent.failed` under `test/e2e/**/*.go`.
- Agentic E2E uses the trajectory terminal fact as authority; ops E2E directly seeds synthetic `COMPLETE_` values and
  does not decode production terminal events.
- OTel has completion/failure coverage but no cancellation-focused test.

### Current specification and documentation ownership

- No current OpenSpec requirement owns the terminal-event union or dispatch user-response projection.
- The current agentic-loop spec explicitly places `COMPLETE_` polymorphism/collisions and terminal-event correctness
  out of scope: `openspec/specs/agentic-loop/spec.md:414-447`.
- ADR-053 is the clearest existing semantic declaration: terminal subjects plus category demux, including cancellation
  on `agent.complete`: `docs/adr/053-agent-run-substrate.md:188-204`, `:224-231`.
- ADR-028 says terminal content may live in `COMPLETE_`, `agent.complete`, or ObjectStore and be retrieved through
  `read_loop_result`: `docs/adr/028-orchestration-architecture.md:55-65`. This is adjacent to #857 and not a
  correctness contract for #865/#866.
- ADR-080's reference ops flow fires per `agent.complete.*`, so category correctness is outwardly consequential:
  `docs/adr/080-push-based-agent-memory-and-lesson-artifacts.md:68-75`.
- Documentation is not coherent:
  - dispatch README lists `agent.complete` but omits the failure/cancellation union:
    `processor/agentic-dispatch/README.md:154`.
  - agentic README calls `agent.complete.*` only "Completions": `agentic/README.md:176`.
  - loop README/config example and output table omit `agent.failed`: `processor/agentic-loop/README.md:96`, `:144`.
  - `docs/concepts/23-parallel-agents.md:77-83` names all three event types, but many examples assert only
    `LoopCompletedEvent`.

### Adopter seam inventory

Specific adopter: a developer outside semstreams who subscribes to loop terminal outcomes or embeds
dispatch/agentrun without reading their implementations.

What they must know today:

- Terminal semantics span two subjects and three concrete categories.
- Subject name does not determine payload type: cancellation shares `agent.complete.*`.
- Production uses the nested, registry-discriminated `BaseMessage` envelope.
- Successful outcome is `success`, while loop state is `complete` and trajectory/run display status is `completed`.
- Cancellation lacks role and user-routing fields, so some projections depend on prior process-local state.
- Consumer ACK policy is not coupled to successful state/user-response projection.
- `COMPLETE_` and terminal stream records are separate, differently shaped surfaces.
- Large successful results remain inline in the terminal event and `COMPLETE_`; #857's result-by-reference work is
  separate.

What happens if they do nothing:

- Using dispatch yields a status response instead of successful result content.
- Dispatch cancellation is logged, ACKed, and omitted from tracker/user-response/dispatch completion metrics.
- A subject-based success-only consumer can drop or silently reinterpret cancellation.
- Using the current agentrun milestone subscriber yields no terminal callbacks for real production envelopes while
  messages are ACKed.
- After a dispatch restart, terminal events for loops absent from the fresh tracker can be ACKed as unknown.
- A downstream user response can be lost after the terminal event has been ACKed.

Where they can find out:

- Category/outcome constants and event structs in `agentic/constants.go` and `agentic/events.go`.
- ADR-053 states category demux.
- Port configs expose both subjects.
- OpenAPI documents only the KV/SSE terminal projection.
- #865/#866 and the historical trajectory inventory describe the dispatch defects.
- No single current adopter document describes the full producer -> durable event -> decoder -> tracker/user-response
  -> metrics seam.

What they should have to know:

- Ideally only the stable semantic terminal outcome and fields relevant to their handler. Internal subject
  partitioning, registry-envelope mechanics, spelling translation, tracker recovery, and payload-size/storage
  mechanics are framework-owned facts. Today the adopter must predict several of those facts rather than observe one
  accepted terminal projection.

External adopter evidence:

- beta.160 pins: semdev, semboids, semsource, semconnect, semmachina.
- Production-Go closing searches found no typed terminal consumers in semdev, semboids, semsource, or semconnect.
- semmachina consumes only the failure seam and uses the registry decoder correctly:
  `/Users/coby/Code/c360/semmachina/internal/stage/loopfailure.go:19-40`, `:499-512`.
- semteams beta.159 depends on `MilestoneSubscriber`:
  `/Users/coby/Code/c360/semteams/cmd/semteams/main.go:946-961`.
- semdragon beta.135 carries its own explicit complete/cancel demux:
  - questbridge: `/Users/coby/Code/c360/semdragon/processor/questbridge/handler.go:432-535`, `:677-727`.
  - quest DAG executor: `/Users/coby/Code/c360/semdragon/processor/questdagexec/events.go:231-315`, `:325-357`.
- semsage alpha.3 subscribes to both subjects but its completion path decodes only `LoopCompletedEvent`:
  `/Users/coby/Code/c360/semsage/tools/spawn/executor.go:179-208`, `:277-313`; its DAG rules likewise split
  completion/failure without cancellation: `/Users/coby/Code/c360/semsage/workflow/dag/definition.go:196-284`.
- Those older adopters demonstrate the concrete bill imposed by the current outward seam; they are not blockers
  reported by the five beta.160 migrations.

### Relationship between #865 and #866

Observed result: they are separable defects and acceptance assertions within one semantic contract.

- #865 is a value-vocabulary/projection defect after successful concrete decoding.
- #866 is a terminal-union/type-demux defect before tracker, response, and metrics projection.
- They share the same producer-to-dispatch contract, durable completion lane, ACK boundary, tracker, user-response
  output, metrics surface, documentation gap, and missing production-envelope test seam.
- Failure remains physically partitioned onto `agent.failed.*`, but ADR-053 and the registered event vocabulary already
  treat success, failure, and cancellation as one semantic terminal union.
- The newly surfaced agentrun envelope incompatibility is the same contract class but a distinct consumer defect.
- #857 payload bounds/result-by-reference is adjacent: repairing #865 makes successful result content reachable
  through dispatch, increasing the practical relevance of #857, but no size/storage change is required to state or
  test either #865 or #866.

Binding scope and artifact rulings remain with the owner.

### Closing searches performed

- `git diff v1.0.0-beta.160..HEAD` across terminal event, dispatch, loop, agentrun, OTel, ADR, and current-spec paths.
- Production type census:
  - `rg -l 'LoopCompletedEvent|LoopFailedEvent|LoopCancelledEvent' --glob '*.go' --glob '!**/*_test.go' .`
- Dispatch cancellation-handler closure:
  - `rg -n 'handleAgentCancel|handleAgentCancelled|LoopCancelledEvent' processor/agentic-dispatch --glob '*.go'`
    `--glob '!**/*_test.go'`
  - returned only KV/HTTP union comments; no NATS cancellation handler.
- E2E closure:
  - `rg -n 'LoopCancelledEvent|handleAgentComplete|handleAgentFailed|user\.response|agent\.complete|agent\.failed'`
    `test/e2e --glob '*.go'`
  - no matches.
- Current-spec closure:
  - `rg -n 'agent\.complete|agent\.failed|LoopCompletedEvent|LoopFailedEvent|LoopCancelledEvent|terminal-event'`
    `openspec/specs --glob '*.md'`
  - only the explicit agentic-loop out-of-scope statement.
- Sister-repository `go.mod` pin census plus production Go/JSON searches for both subjects, all three event types, and
  `MilestoneSubscriber`.

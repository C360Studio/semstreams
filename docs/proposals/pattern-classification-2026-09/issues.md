# The open `class:*` symptom issues, re-derived (#1234)

Base: `8e41e46f2dfd757e9350fe27f2004b00c40388b4` · derived 2026-09-13 by the union of five label queries (each `gh issue list --state open --label class:<l> --limit 100`), unique by number.

Unique issues: 60 · unmilestoned: 38 · milestoned: 22

Milestones: NONE=38 · v1.0.0-beta.163=4 · v1.0.0-beta.164=1 · v1.0.0-beta.165=15 · v1.0.0-beta.166=2

Horizon / status: horizon:post-v1=4 · horizon:pre-v1=50 · status:blocked=3 · status:needs-decision=2

Class labels (overlapping): advertised-absent=15 · dead-surface=13 · payload-size=1 · phantom-config=6 · silent-noop-surface=16 · swallowed-degrade=16 · unobserved-skip=11

| issue | milestone | class | horizon / status | title |
|---|---|---|---|---|
| #348 | NONE | swallowed-degrade | pre-v1 | graph-query/graphrag: strategy + loadEntities seams WrapTransient collapses downstream Invalid/Fatal to Transient (gh#326 follow-up) |
| #383 | v1.0.0-beta.165 | silent-noop-surface, phantom-config | pre-v1 | processor/rule: make rule-level max_iterations impossible to mistake for enforcement |
| #436 | NONE | unobserved-skip | pre-v1 | gated-dag: ignore hierarchy container entities under unit prefix |
| #472 | NONE | swallowed-degrade | pre-v1 | message-logger /entries applies limit before subject filter — agent/tool queries return 0 under graph-ingest storms |
| #589 | NONE | dead-surface | pre-v1 | graph/inference: remove or repurpose dead storage.Watch (no production caller) |
| #608 | NONE | swallowed-degrade | pre-v1 | graph-clustering: LLM down at startup disables enhancement permanently (no retry); level-blind GetCommunity scan remains |
| #618 | v1.0.0-beta.164 | swallowed-degrade | pre-v1 | anomaly: FindSimilar fails open when the embedding index is not ready |
| #620 | v1.0.0-beta.165 | silent-noop-surface, phantom-config, dead-surface | pre-v1 | Delete phantom signals and inert config across the graph core (~400-500 LOC, no behavior change) |
| #621 | NONE | unobserved-skip | pre-v1 | fusion + pathrag: truncation at maxRelationsPerNode/maxPaths is unlabeled; pathrag maxNodes has no ceiling |
| #659 | v1.0.0-beta.165 | silent-noop-surface, phantom-config | pre-v1 | Schema generator drops type:number (float64) config fields from generated schemas |
| #734 | v1.0.0-beta.165 | silent-noop-surface, phantom-config | pre-v1 | An unrecognized schema Type spelling silently becomes "string" AND skips runtime validation |
| #746 | NONE | unobserved-skip | pre-v1 | processor/rule + research-graph: first-wins resolution makes a round-2 companion predicate permanently invisible |
| #764 | NONE | dead-surface | pre-v1 | pkg/dispatch: BoundedDispatcher.Stats() chain dead-ends — no consumer outside the package |
| #810 | NONE | silent-noop-surface, advertised-absent | blocked, pre-v1 | agentic-tools: tool.list discovery is silently swallowed when a JetStream stream covers tool.> |
| #824 | v1.0.0-beta.165 | advertised-absent | pre-v1 | lifecycle-gateway: workflows that cannot be created through the operator route are still advertised on it |
| #857 | NONE | payload-size, unobserved-skip | needs-decision, pre-v1 | payload-size class: framework writes that scale with data volume — one site handles the limit, ten can silently lose data |
| #980 | NONE | swallowed-degrade | pre-v1 | lessons: NewNATSLessonStore swallows the graphmutation client-construction error |
| #1002 | NONE | advertised-absent | pre-v1 | pkg/logging: doc.go documents the NATS log subject backwards (logs.{source}.{level}); the handler emits logs.{level}.{source} |
| #1007 | v1.0.0-beta.165 | silent-noop-surface, phantom-config, advertised-absent | pre-v1 | rule: fire_every_n_events silently does not gate stateful actions (on_enter/publish_agent), and submit_work is a ghost tool |
| #1029 | NONE | swallowed-degrade | pre-v1 | projection: a copied contract listing a birth predicate in a mutable group silently deletes it on reconcile |
| #1035 | NONE | silent-noop-surface, swallowed-degrade | pre-v1 | agentic-loop: a task rejected at preflight notifies nobody — routing fields carried 'for error notifications' go unused |
| #1041 | NONE | swallowed-degrade | pre-v1 | rule: an entity-watcher start failure permanently disables message-path rule evaluation too |
| #1042 | v1.0.0-beta.165 | silent-noop-surface, phantom-config | pre-v1 | rule: per-rule entity.watch_buckets is parsed and validated but never drives a watcher |
| #1043 | NONE | unobserved-skip | pre-v1 | rule: $prev.* transition conditions silently no-op when the evaluation has no EntityState |
| #1045 | NONE | silent-noop-surface, advertised-absent, unobserved-skip | pre-v1 | agentic-governance: the ADR-043 verdict is published but unreachable — dotted detail keys cannot be walked by $message paths |
| #1049 | NONE | unobserved-skip | pre-v1 | rule: rule-opacity is enforced on conditions only — action templates can emit opaque predicate values unguarded |
| #1076 | NONE | dead-surface | pre-v1 | audit: KV bucket bindings at boot — WaitForBucket has zero callers; which consumers bind without a readiness wait? |
| #1121 | NONE | dead-surface | post-v1 | test/e2e/client/websocket.go still targets the ADR-096-retired /flowbuilder/status/stream — ~250 lines of e2e harness with no caller |
| #1123 | NONE | dead-surface | pre-v1 | Service.RegisterMetrics has no production caller — the interface method is a phantom; wire it into service composition or delete it |
| #1124 | v1.0.0-beta.165 | unobserved-skip | pre-v1 | ExecutorRegistry: a tool registered under a dispatch key its executor does not advertise dispatches but never lists — advertised vs dispatchable divergence |
| #1125 | NONE | dead-surface | pre-v1 | testutil/flow.go FlowBuilder/NewFlowBuilder/FlowComponentConfig have zero callers — dead config-shaped builder outside the ADR-100 D5 surface |
| #1126 | NONE | swallowed-degrade | pre-v1 | metric.Registry.RegisterGaugeVec discards the existing collector on a duplicate registration — a second registrant writes to a detached GaugeVec /metrics never scrapes |
| #1132 | NONE | swallowed-degrade | pre-v1 | graph_writer calls the panicking ModelEndpointEntityID before endpoint.Validate() — an identity fault crashes startup instead of taking the WARN-and-continue path |
| #1135 | NONE | dead-surface | post-v1 | cmd/e2e: the {{ template-variable exclusion in scenarioNamesInTaskfileContent is unreachable dead code |
| #1136 | NONE | advertised-absent | pre-v1 | graph-query/docs: distinguish result attribution from audited evidence and reconcile GraphRAG claims |
| #1138 | NONE | silent-noop-surface, advertised-absent | pre-v1 | agentic-tools/http_request: advertised readable-text contract returns raw HTML |
| #1140 | NONE | silent-noop-surface, advertised-absent | blocked, pre-v1 | agentic-governance: untrusted tool results bypass injection filtering before model context |
| #1143 | v1.0.0-beta.163 | silent-noop-surface, swallowed-degrade | pre-v1 | graph.ingest.* has no reserved boundary between the RPC plane and a consumer's persisted subjects, so the obvious wildcard binding silently shadows request/reply |
| #1146 | v1.0.0-beta.163 | silent-noop-surface, swallowed-degrade | needs-decision, blocked, pre-v1 | agentic-loop: prevent silent ACK and active-state loss across process restart |
| #1152 | NONE | dead-surface | post-v1 | test/e2e/scenarios/stages: dead package with zero importers carries 7 stale c360.logistics entity IDs |
| #1170 | NONE | swallowed-degrade | pre-v1 | rule: Start warn-swallows a state-tracker failure that leaves NO action executor — the processor boots healthy and dispatches nothing |
| #1172 | NONE | unobserved-skip | pre-v1 | graph/inference: hierarchy skips a foreign-authority entity with no log, metric or counter — the last unobservable omission in the ADR-102 boundary |
| #1187 | NONE | dead-surface | pre-v1 | greenfield: delete three zero-caller exported surfaces not caused by #1168 |
| #1201 | v1.0.0-beta.165 | advertised-absent | pre-v1 | composition lint: refuse a declared request/reply subject captured by a declared stream filter |
| #1202 | NONE | advertised-absent | post-v1 | sweep: description-vs-behavior fidelity across the tool/port/doc description corpus |
| #1203 | NONE | dead-surface | pre-v1 | triage: dead-surface candidate list — wanted-vs-wired ruled per site |
| #1204 | NONE | swallowed-degrade | pre-v1 | batch: boot honesty — Start paths that swallow a failure and report healthy |
| #1206 | v1.0.0-beta.166 | advertised-absent | pre-v1 | rule: $caller.* substitution has no production populator — a shipped policy DSL for a caller that never exists |
| #1212 | v1.0.0-beta.165 | silent-noop-surface | pre-v1 | graph-ingest: ENTITY_SUFFIX_INDEX collides a loop with its run — same instance, same 'execution' type token; bySuffix is nondeterministic and removal cross-deletes |
| #1222 | NONE | unobserved-skip | pre-v1 | e2e: agentic and research-graph never populate assertions_run, so the #1195 'measured nothing' check is blind to them |
| #1223 | NONE | silent-noop-surface | pre-v1 | test(e2e): no guard stops a scenario predicting the deployment authority — #1168's migration left two behind and nothing went red |
| #1224 | NONE | unobserved-skip | pre-v1 | e2e(research-graph): the direct fixture is green while the synthesizer LLM is never invoked — the degraded path stamps every triple the assertions check |
| #1239 | v1.0.0-beta.163 | advertised-absent, dead-surface |  | agentic-loop: pause/resume are advertised and unimplemented — PauseRequested is written twice, read never, and its comment promises a checkpoint that does not exist |
| #1244 | v1.0.0-beta.165 | swallowed-degrade |  | agentic-loop: adopt the StopAll exit contract for loop state — two silent stalls leave a loop wedged with no transition and no observer |
| #1249 | v1.0.0-beta.163 | swallowed-degrade |  | agentrun: make milestone fanout settlement replay-safe without partial ACK |
| #1252 | v1.0.0-beta.166 | dead-surface | pre-v1 | retire: delete agentic/identity — ADR-075's removal was executed for its siblings and missed this package |
| #1255 | v1.0.0-beta.165 | advertised-absent |  | test hygiene: 48 test files claim "Code generated by Tester Agent. DO NOT EDIT." and no generator exists |
| #1270 | v1.0.0-beta.165 | advertised-absent | pre-v1 | agentic-tools: HintEmpty adoption sweep — three executors still answer 'nothing matched' as English prose while #1261 makes the graph-read tools its first typed adopters |
| #1286 | v1.0.0-beta.165 | advertised-absent |  | flake-guard(graph-index): PredicateLayoutSmoke's 10s operationBudget is unreachable dead code, widened on a millisecond value read as seconds |
| #1288 | v1.0.0-beta.165 | silent-noop-surface |  | research-graph: completion envelopes do not match read_loop_result and current loop state |

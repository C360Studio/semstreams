# Inventory: #1234 slice sweeps-vocabulary
base: 32aeddf73612d157261ace65d9810750d2d602e0

## #620 — Delete phantom signals and inert config across the graph core (~400-500 LOC, no behavior change)
Named sites:
processor/graph-embedding/component.go:555 — original site gone: `Validate` no longer creates/requires EMBEDDINGS_CACHE, it now rejects any declared output port; only comments/tests still name the string (17 hits, none production creation code — see Searches)
- `processor/graph-embedding/component.go:116` — `// configured output is a stale declaration (the EMBEDDINGS_CACHE surface`
- `docs/operations/embeddings-cache-removal.md:1` — `# EMBEDDINGS_CACHE removal (BREAKING) — adopter migration checklist`
graph/embedding/cache.go — gone (file removed; zero-hit — see Searches)
- `graph/embedding/worker.go:147` — `SetPending(count float64)`
no non-test caller of `.SetPending(` anywhere in the repo (zero-hit — see Searches); still dead
processor/graph-query/component.go:663 — gone: no `IsAvailable` method exists on `communityCache` (zero-hit — see Searches)
processor/graph-query/community_cache.go:302 — gone: the file was restructured around `communityCache`/`communityLease`/`communityGeneration`, no `IsReady` function remains (zero-hit — see Searches)
- `processor/graph-clustering/component.go:62` — `MinCommunitySize     int                   `json:"min_community_size" schema:"type:int,description:Minimum number of entities to form a community,category:advanced"``
- `graph/clustering/lpa.go:653` — `MinCommunitySize:        2,`
- `graph/clustering/lpa.go:680` — `config.MinCommunitySize = 2`
processor/graph-clustering/component.go:74 — gone: the field itself was removed (ADR-090), only a removed-field rejection message remains
- `processor/graph-clustering/component.go:254` — `"max_hop_distance":    `removed (ADR-090, BREAKING): structural distance behavior is internal to anomaly detection. Delete the field`,`
processor/graph-clustering/structural.go:35-36 — gone: no hop-distance reference remains in the file (zero-hit — see Searches)
- `processor/graph-clustering/component.go:59` — `BatchSize            int                   `json:"batch_size" schema:"type:int,description:Event count threshold for triggering detection,category:basic"``
- `processor/graph-index/component.go:45` — `BatchSize int                   `json:"batch_size" schema:"type:int,description:Batch size for index updates,category:advanced"``
graph/query/client.go:75-99 — gone: no `client.go` exists under `graph/query/` and no `TTL` symbol remains in that package (zero-hit — see Searches)
- `graph/embedding/http_embedder.go:45` — `dimensions atomic.Int64`
- `graph/embedding/http_embedder.go:207` — `func (h *HTTPEmbedder) Dimensions() int {`
- `docs/advanced/01-clustering.md:290` — `"min_embedding_coverage": 0.5`
- `docs/concepts/07-community-detection.md:132` — `Control how many levels are computed via configuration. Default is 3 levels (0, 1, 2). Maximum is 10. More levels provide finer granularity options at query time but increase computation during detection.`
- `graph/clustering/lpa.go:640` — `type InferenceConfig struct {`
- `graph/clustering/lpa.go:673` — `func (d *LPADetector) InferRelationshipsFromCommunities(`
- `graph/clustering/lpa.go:760` — `type InferredTriple struct {`
- `graph/clustering/lpa.go:773` — `func (d *LPADetector) computeCommunityTightness(ctx context.Context, community *Community) float64 {`
- `graph/clustering/lpa.go:808` — `func (d *LPADetector) hasExplicitEdge(ctx context.Context, entityA, entityB string) bool {`
- `docs/advanced/01-clustering.md:354` — `When enabled, entities in the same community receive inferred `community.co_member` triples connecting them. This creates explicit edges where only semantic similarity existed.`
- `graph/clustering/lpa.go:730` — `Predicate:   "inferred.cluster.clustered-with",`
- `docs/concepts/07-community-detection.md:114` — `Communities can nest at multiple levels:`
- `docs/concepts/07-community-detection.md:148` — `**Tip**: Start with level 1 for general queries. Drop to level 0 when you need specific details. Use level 2+ when asking about overall system patterns.`
- `docs/proposals/prev1-graph-core-audit.md:20` — `## 0. The unifying defect: phantom signals`
- `processor/graph-clustering/component.go:1386` — `WithLevels(3).`
graph/embedding: `WithWorkers(BatchSize / 10)` gone; graph-embedding now calls `WithWorkers(c.config.Workers)` (`processor/graph-embedding/component.go:1018`); the `BatchSize` field itself remains:
- `processor/graph-embedding/component.go:77` — `BatchSize    int                   `json:"batch_size" schema:"type:int,description:Batch size for embedding generation,category:advanced"``

## #1202 — sweep: description-vs-behavior fidelity across the tool/port/doc description corpus
Named sites:
processor/agentic-tools/executors/ — exists, 41 entries (`ls processor/agentic-tools/executors/ | wc -l` → 41)
no other path/line named — "categories/prompt assembler", "port Description: strings", and "package doc.go claims" are generic corpus descriptions, not specific paths

## #1203 — triage: dead-surface candidate list — wanted-vs-wired ruled per site
Named sites:
metric/ Record* cluster — 9 functions, all declared in metric/core.go, all zero-hit for a non-test caller outside the package (see Searches):
- `metric/core.go:162` — `func (c *Metrics) RecordMessageReceived(service, messageType string) {`
- `metric/core.go:167` — `func (c *Metrics) RecordMessageProcessed(service, messageType, status string) {`
- `metric/core.go:172` — `func (c *Metrics) RecordMessagePublished(service, subject string) {`
- `metric/core.go:177` — `func (c *Metrics) RecordProcessingDuration(service, operation string, duration time.Duration) {`
- `metric/core.go:187` — `func (c *Metrics) RecordHealthStatus(service string, healthy bool) {`
- `metric/core.go:196` — `func (c *Metrics) RecordNATSStatus(connected bool) {`
- `metric/core.go:205` — `func (c *Metrics) RecordNATSRTT(rtt time.Duration) {`
- `metric/core.go:210` — `func (c *Metrics) RecordNATSReconnect() {`
- `metric/core.go:215` — `func (c *Metrics) RecordCircuitBreakerState(state int) {`

## #1204 — batch: boot honesty — Start paths that swallow a failure and report healthy
Named sites:
(none — see Searches)

## Adjacent claims
- #620: none of the five draft PRs name it; body names no other 60-set issue
- #1202: none of the five draft PRs name it; body names #824, #1002, #1007, #1136, #1138, #1140, #1201 (also cites PR #1197, not an issue in the 60-set)
- #1203: none of the five draft PRs name it; body names #589, #620, #764, #1076, #1121, #1123, #1125, #1135, #1152, #1187 (also cites #761, not in the 60-set)
- #1204: none of the five draft PRs name it; body names #608, #980, #1041, #1170 (also cites PR #1197, not an issue in the 60-set)

## Vocabulary sites
Re-located pins of `openspec/changes/archive/2026-09-02-loop-scoped-request-seams/inventory-precedent.md` (base `0a40ddf347db325c8fc34924b61260f3dc316e68`) against current HEAD. `task inventory:verify` on that file today: pins=41 ok=25 moved=5 ambiguous=0 drift=8 malformed=3 unparsed=0. This section covers the 16 non-`ok` pins only; the 25 `ok` pins are unchanged and not repeated here.
MOVED (5):
- `processor/graph-ingest/authority_gate.go:44` — `// It is NEVER called for an @id OBJECT: a relationship target keeps structural`
- `processor/graph-ingest/authority_gate.go:55` — `// authorityMetricReason maps an authority rejection to its mutation_rejections`
- `processor/graph-ingest/authority_gate.go:30` — `// authorityRejectionLogMessage is the single WARN a refused candidate produces`
- `processor/graph-ingest/authority_gate.go:31` — `// on any lane. Named so the test pinning the requirement's "loud log" matches`
- `processor/graph-ingest/authority_gate.go:79` — `func (c *Component) recordAuthorityRejection(arrival, reason string, err error) {`
DRIFT (8):
processor/graph-ingest/authority_gate.go:59 (was `// One home for the mapping so the fact lane and the mutation lane cannot disagree.`) — text reflowed across two lines by an intervening edit; zero-hit for the original single-line text (see Searches); current lines carrying the same sentence:
- `processor/graph-ingest/authority_gate.go:56` — `// reason label, or returns ok=false for any other error. One home for the`
- `processor/graph-ingest/authority_gate.go:57` — `// mapping so the fact lane and the mutation lane cannot disagree.`
processor/graph-ingest/authority_gate.go:79 (was `// the whole point of the gate is that a foreign identity is not this`) — reworded in place, re-found by the core phrase (see Searches); current text:
- `processor/graph-ingest/authority_gate.go:76` — `// identity: the whole point of the gate is that a foreign identity is not this`
- `processor/agentic-dispatch/command_registry.go:14` — `"github.com/c360studio/semstreams/pkg/errs"`
- `processor/agentic-dispatch/commands.go:14` — `"github.com/c360studio/semstreams/pkg/errs"`
- `processor/agentic-dispatch/component.go:19` — `"github.com/c360studio/semstreams/pkg/errs"`
- `processor/agentic-dispatch/config.go:7` — `"github.com/c360studio/semstreams/pkg/errs"`
- `processor/agentic-dispatch/global.go:25` — `"github.com/c360studio/semstreams/pkg/errs"`
- `processor/agentic-dispatch/loop_tracker.go:10` — `"github.com/c360studio/semstreams/pkg/errs"`
MALFORMED (3, now pinned with the required `— text` form):
- `graph/inference/hierarchy.go:217` — `if semtypes.ValidateEntityIDAuthority(entityID, h.config.Org, h.config.Platform, false) != nil {`
- `processor/graph-ingest/authority_gate.go:52` — `return semtypes.ValidateEntityIDAuthority(subject, c.org, c.platform, importLane)`
- `processor/rule/actions.go:633` — `err := semtypes.ValidateEntityIDAuthority(entityID, e.platform.Org, e.platform.Platform, false)`

## Adoption counts
Per-directory, non-test counts of `errs\.Classified[A-Za-z]*` and `errs\.Wrap[A-Za-z]*` at HEAD (`git grep -o -E 'errs\.(Classified[A-Za-z]*|Wrap[A-Za-z]*)' -- '*.go' ':!*_test.go'`, grouped by directory):

| directory | errs.Classified* | errs.Wrap* |
|---|---|---|
| agentic/agentrun | 1 | 4 |
| cmd/e2e-semstreams/mission | 0 | 13 |
| component | 0 | 77 |
| component/flowgraph | 0 | 5 |
| componentregistry | 0 | 28 |
| config | 0 | 7 |
| examples/processors/document | 0 | 16 |
| examples/processors/iot_sensor | 0 | 16 |
| examples/processors/weather_station | 0 | 14 |
| frameworkcapabilities/graphresearch | 0 | 7 |
| gateway | 0 | 11 |
| gateway/graph-gateway | 4 | 23 |
| gateway/http | 1 | 8 |
| gateway/lifecycle-gateway | 0 | 16 |
| graph | 7 | 13 |
| graph/clustering | 0 | 85 |
| graph/embedding | 0 | 44 |
| graph/inference | 0 | 90 |
| graph/llm | 0 | 7 |
| graph/structural | 0 | 7 |
| input/file | 0 | 28 |
| input/http | 0 | 36 |
| input/udp | 0 | 31 |
| input/websocket | 0 | 24 |
| internal/entityidaudit | 1 | 0 |
| internal/graphmutation | 1 | 0 |
| internal/maxdelivery | 0 | 1 |
| message | 0 | 14 |
| metric | 0 | 22 |
| natsclient | 18 | 81 |
| output/file | 0 | 31 |
| output/httppost | 0 | 32 |
| output/otel | 0 | 15 |
| output/websocket | 0 | 43 |
| payloadregistry | 0 | 14 |
| persona | 0 | 17 |
| pkg/acme | 0 | 35 |
| pkg/buffer | 0 | 5 |
| pkg/cache | 0 | 15 |
| pkg/errs | 1 | 11 |
| pkg/fusion | 1 | 0 |
| pkg/fusion/fusionnats | 2 | 0 |
| pkg/lifecycle | 3 | 0 |
| pkg/projection | 4 | 0 |
| pkg/tlsutil | 0 | 12 |
| pkg/types | 1 | 0 |
| processor/agentic-dispatch | 3 | 30 |
| processor/agentic-governance | 0 | 56 |
| processor/agentic-loop | 18 | 93 |
| processor/agentic-model | 0 | 35 |
| processor/agentic-tools | 2 | 85 |
| processor/agentic-tools/executors | 1 | 10 |
| processor/gated-dag | 0 | 2 |
| processor/graph-clustering | 3 | 102 |
| processor/graph-embedding | 7 | 53 |
| processor/graph-index | 34 | 111 |
| processor/graph-index-spatial | 3 | 39 |
| processor/graph-index-temporal | 3 | 36 |
| processor/graph-ingest | 32 | 62 |
| processor/graph-query | 8 | 69 |
| processor/json_filter | 0 | 22 |
| processor/json_generic | 0 | 23 |
| processor/json_map | 0 | 23 |
| processor/research-graph-assess | 0 | 18 |
| processor/research-graph-classify | 0 | 16 |
| processor/research-graph-execute | 1 | 16 |
| processor/research-graph-route | 0 | 18 |
| processor/research-graph-synthesize | 0 | 18 |
| processor/rule | 3 | 107 |
| processor/rule/expression | 0 | 10 |
| service | 0 | 53 |
| storage | 0 | 3 |
| storage/objectstore | 0 | 58 |
| test/e2e/client | 1 | 0 |
| test/e2e/harness/lessoncuration | 0 | 2 |
| test/e2e/scenarios | 1 | 0 |
| types | 0 | 3 |
| **TOTAL** | **165** | **2131** |

Directories: 77. Directories with zero `errs.Classified*`: 49.

## Searches
- `gh issue view 620 1202 1203 1204 --json number,title,body,labels,milestone` (one loop, one file) → 4 bodies, 81 lines
- `git grep -n "EMBEDDINGS_CACHE" -- '*.go'` → 17 (all comments/tests, no production creation code)
- `ls graph/embedding/cache.go` → 0 (no such file)
- `git grep -n "NATSCache" -- '*.go'` → 0
- `git grep -n "SetPending\|graph_embedding_pending" -- '*.go'` → 42
- `git grep -n "\.SetPending(" -- '*.go' ':!*_test.go'` → 0
- `git grep -n "IsAvailable" -- processor/graph-query` → 0
- `git grep -n "func.*IsReady\|\.IsReady(" -- processor/graph-query` → 0
- `git grep -n "IsAvailable\b" -- '*.go'` → 19 (all `pkg/resource`/`test/e2e` — unrelated `Watcher`/`ProfileClient` types, none named `communityCache`)
- `git grep -n "communityCache" -- processor/graph-query` → 96
- `git grep -n "min_community_size\|MinCommunitySize" -- '*.go'` → 53
- `git grep -n "max_hop_distance\|MaxHopDistance" -- '*.go'` → 28
- `grep -n "hop\|Hop" processor/graph-clustering/structural.go` → 0
- `git grep -n "batch_size" -- processor/graph-clustering processor/graph-index` → 15
- `find . -path "./graph/query/*" -name "client.go"` → 0
- `git grep -n "TTL" -- "graph/query/*.go"` → 0
- `grep -n "min_embedding_coverage" docs/advanced/01-clustering.md` → 2
- `grep -n "InferRelationshipsFromCommunities\|InferenceConfig\|InferredTriple\|computeCommunityTightness\|hasExplicitEdge" graph/clustering/lpa.go` → 24
- `git grep -n "InferRelationshipsFromCommunities" -- '*.go' | grep -v lpa.go` → 4 (doc example, interface decl, one test override — no production caller)
- `grep -n "clustered-with" graph/clustering/lpa.go` → 2
- `grep -n "ParentID\|start with level 1\|level 1" docs/concepts/07-community-detection.md` → 1
- `git grep -n "WithLevels("` → 14
- `git grep -n "WithWorkers("` → 20
- `git grep -n "RecordMessageProcessed\|RecordMessagePublished\|RecordMessageReceived\|RecordNATSReconnect\|RecordNATSRTT\|RecordNATSStatus\|RecordProcessingDuration\|RecordHealthStatus\|RecordCircuitBreakerState" -- "metric/*.go"` → 33 (9 declarations + 24 test call sites)
- `git grep -n "<same 9 names>" -- '*.go' ':!metric/*_test.go' ':!*_test.go' | grep -v metric/core.go` → 0 (no non-test caller outside `metric/`)
- `ls processor/agentic-tools/executors/ | wc -l` → 41
- `for n in 1201 1138 1007 1140 824 1002 1136 1197 1123 1125 1152 1187 764 589 1121 1135 1076 620 761 1170 1041 980 608; do grep -c "| #$n |" issues.md; done` → all 1 except #1197 and #761 → 0
- `for p in 1141 1156 1159 1254 1297; do gh pr view $p --json number,body; done | grep -n "#620\|#1202\|#1203\|#1204"` → 0
- `task inventory:verify -- openspec/changes/archive/2026-09-02-loop-scoped-request-seams/inventory-precedent.md` → pins=41 ok=25 moved=5 ambiguous=0 drift=8 malformed=3 unparsed=0
- `git grep -n "ValidateEntityIDAuthority" -- '*.go' | grep -v _test` → 7
- `git grep -nF "One home for the mapping so the fact lane and the mutation lane cannot disagree." -- processor/graph-ingest/authority_gate.go` → 0
- `git grep -nF "the whole point of the gate is that a foreign identity is not this" -- processor/graph-ingest/authority_gate.go` → 1 (line 76, reworded with an inserted "identity:")
- `grep -n "pkg/errs" processor/agentic-dispatch/{command_registry,commands,component,config,global,loop_tracker}.go` → 6 (each file, one import line, at the current lines pinned above)
- `git grep -o -E "errs\.(Classified[A-Za-z]*|Wrap[A-Za-z]*)" -- '*.go' ':!*_test.go'` → 2296 (165 Classified*, 2131 Wrap*, across 77 directories)
NOT RUN: refusal/observation and nearest-pattern-instance passes for #620/#1202/#1203/#1204 — out of scope for this slice (task narrows Half 1 to Named sites + Adjacent claims only)
NOT RUN: `gopls` structural passes — this slice is a literal-search sweep over sweep/meta issues and a re-pin of an existing literal-search inventory; no interface/implementer question was posed

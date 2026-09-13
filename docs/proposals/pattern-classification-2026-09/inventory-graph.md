# Inventory: #1234 slice graph
base: 32aeddf73612d157261ace65d9810750d2d602e0

## #348 — graph-query/graphrag: strategy + loadEntities seams WrapTransient collapses downstream Invalid/Fatal to Transient (gh#326 follow-up)
Named sites:
- `processor/graph-query/graphrag.go:1196` — `func (c *Component) handleStrategyTemporal(ctx context.Context, cr *query.ClassificationResult, req *GlobalSearchRequest, startTime time.Time) ([]byte, error) {`
- `processor/graph-query/graphrag.go:1264` — `func (c *Component) handleStrategySpatial(ctx context.Context, cr *query.ClassificationResult, req *GlobalSearchRequest, startTime time.Time) ([]byte, error) {`
- `processor/graph-query/graphrag.go:1501` — `func (c *Component) loadEntities(ctx context.Context, entityIDs []string) ([]*gtypes.EntityState, error) {`
Refusal and observation:
- `processor/graph-query/graphrag.go:1223` — `return nil, errs.WrapTransient(err, "GraphQuery", "handleStrategyTemporal", "temporal query")`
- `processor/graph-query/graphrag.go:1236` — `return nil, errs.WrapTransient(loadErr, "GraphQuery", "handleStrategyTemporal", "load entities")`
- `processor/graph-query/graphrag.go:1293` — `return nil, errs.WrapTransient(err, "GraphQuery", "handleStrategySpatial", "spatial query")`
- `processor/graph-query/graphrag.go:1306` — `return nil, errs.WrapTransient(loadErr, "GraphQuery", "handleStrategySpatial", "load entities")`
- `processor/graph-query/graphrag.go:1517` — `return nil, errs.WrapTransient(errors.New("entityBatch query routing not available"), "GraphQuery", "loadEntities", "route query")`
- `processor/graph-query/graphrag.go:1526` — `return nil, errs.WrapTransient(err, "GraphQuery", "loadEntities", "request entities")`
Nearest pattern instance:
- `processor/graph-query/query.go:624` — `// ADR-060: propagate the downstream classified error UNWRAPPED (see handleQueryEntity).`
- `processor/graph-query/query.go:628` — `return nil, err`
- `processor/graph-query/summary.go:92` — `summary.EntitySampleTruncated = len(entityIDs) >= req.EntitySampleLimit`

## #436 — gated-dag: ignore hierarchy container entities under unit prefix
Named sites:
- `processor/gated-dag/config.go:79` — `UnitEntityPrefix string `json:"unit_entity_prefix"``
- `processor/gated-dag/config.go:301` — `return fmt.Errorf("unit_entity_prefix is required (it scopes the unit set this executor reads)")`
- `processor/gated-dag/schema.go:23` — `"unit_entity_prefix": {`
- `processor/gated-dag/reader.go:155` — `func extractGraph(states []graph.EntityState, cfg Config) graphView {`
- `graph/inference/container_entity.go:21` — `Category: "hierarchy_container",`
- `graph/inference/hierarchy.go:489` — `Object:     "hierarchy.container",`
No `entity.type.class` (or `EntityTypeClass`) literal exists anywhere under `processor/gated-dag/` — zero hits.
Refusal and observation:
(none — see Searches)
Nearest pattern instance:
- `processor/gated-dag/reader.go:147` — `// Referential stubs are EXCLUDED (gh#429): graph-ingest materializes an`

## #589 — graph/inference: remove or repurpose dead storage.Watch (no production caller)
Named sites:
- `graph/inference/storage.go:417` — `func (s *NATSAnomalyStorage) Watch(ctx context.Context) (<-chan *StructuralAnomaly, error) {`
- `graph/inference/storage_test.go:268` — `ch, err := storage.Watch(ctx)`
- `graph/inference/review_worker.go:31` — `// ReviewWorker watches ANOMALY_INDEX and processes pending anomalies.`
`storage.go` `Watch` has exactly one caller anywhere in the tree, and it is the test file above.
Refusal and observation:
- `graph/inference/storage.go:427` — `return nil, errs.WrapTransient(err, "NATSAnomalyStorage", "Watch", "create watcher")`
Nearest pattern instance:
- `graph/inference/hierarchy.go:203` — `return nil, errs.WrapInvalid(errHierarchyAuthorityUnset, "HierarchyInference", "GetHierarchyTriples",`

## #608 — graph-clustering: LLM down at startup disables enhancement permanently (no retry); level-blind GetCommunity scan remains
Named sites:
- `graph/clustering/enhancement_worker.go:470` — `func (w *EnhancementWorker) markFailed(ctx context.Context, community *Community, hash string, level int) {`
- `graph/clustering/storage.go:195` — `func (s *NATSCommunityStorage) GetCommunity(ctx context.Context, id string) (*Community, error) {`
- `graph/clustering/storage.go:109` — `func (s *NATSCommunityStorage) SaveCommunity(ctx context.Context, community *Community) error {`
- `graph/clustering/storage.go:521` — `return fmt.Sprintf("%d.%s", level, communityID)`
- `graph/clustering/storage.go:320` — `func (s *NATSCommunityStorage) GetEntityCommunity(ctx context.Context, entityID string, level int) (*Community, error) {`
- `processor/graph-clustering/query.go:278` — `func (c *Component) getEntityCommunity(ctx context.Context, entityID string, level int) (*clustering.Community, error) {`
- `processor/graph-clustering/component.go:1074` — `if err := c.startEnhancementWorker(ctx); err != nil {`
- `processor/graph-clustering/component.go:2268` — `func (c *Component) startEnhancementWorker(ctx context.Context) error {`
- `processor/graph-clustering/component.go:2351` — `go c.monitorLLMHealth(ctx, resolved.URL, worker)`
- `graph/clustering/lpa.go:673` — `func (d *LPADetector) InferRelationshipsFromCommunities(`
- `processor/graph-clustering/component.go:2246` — `func (p *kvProvider) GetEdgeWeight(ctx context.Context, fromID, toID string) (float64, error) {`
Refusal and observation:
- `graph/clustering/enhancement_worker.go:478` — `if err := w.summaries.PutFailedUnlessEnhanced(ctx, rec); err != nil {`
- `graph/clustering/enhancement_worker.go:479` — `w.logger.Error("Failed to write llm-failed record", "community_id", community.ID, "error", err)`
- `processor/graph-clustering/component.go:1075` — `c.logger.Warn("failed to start enhancement worker, continuing without LLM",`
- `processor/graph-clustering/component.go:2284` — `return errs.WrapTransient(err, "Component", "startEnhancementWorker",`
Nearest pattern instance:
- `processor/graph-clustering/query.go:284` — `entityKey := fmt.Sprintf("entity.%d.%s", level, entityID)`

## #618 — anomaly: FindSimilar fails open when the embedding index is not ready
Named sites:
- `processor/graph-embedding/component.go:995` — `c.statusPublisher = readiness.NewPublisher(bucket, readiness.KeyGraphEmbedding)`
- `processor/graph-embedding/query.go:38` — `// gates on embedding.ready until it is proven; this exposes it for observability.`
- `processor/graph-clustering/component.go:1509` — `watcher := readiness.NewWatcher(c.natsClient, readiness.KeyGraphIndex,`
- `processor/graph-clustering/component.go:1524` — `embWatcher := readiness.NewWatcher(c.natsClient, readiness.KeyGraphEmbedding,`
- `pkg/fusion/fusionnats/client.go:139` — `watch := readiness.NewWatcher(src, readiness.KeyGraphIndex)`
- `processor/graph-clustering/similarity.go:77` — `func (f *querySimilarityFinder) FindSimilar(`
- `processor/graph-embedding/component.go:1729` — `return errs.ClassifiedCode(errs.ErrorTransient, graph.ErrorCodeIndexNotReady,`
The path `graph/query/client.go` the issue names does not exist in this tree — zero hits, confirmed by `git grep` erroring "no such path".
Refusal and observation:
- `processor/graph-clustering/similarity.go:99` — `f.logger.Debug("similarity query failed (transport or handler)",`
- `processor/graph-clustering/similarity.go:102` — `return nil, nil`
Nearest pattern instance:
- `processor/graph-clustering/component.go:1524` — `embWatcher := readiness.NewWatcher(c.natsClient, readiness.KeyGraphEmbedding,`

## #621 — fusion + pathrag: truncation at maxRelationsPerNode/maxPaths is unlabeled; pathrag maxNodes has no ceiling
Named sites:
- `processor/graph-query/graphrag.go:33` — `MaxTotalEntitiesInSearch = 10000`
- `processor/graph-query/graphrag.go:1423` — `entityIDs, truncated := sortAndCapEntityIDs(entityIDSet, MaxTotalEntitiesInSearch)`
- `pkg/fusion/engine_lens.go:34` — `maxRelationsPerNode = 12`
- `pkg/fusion/engine_lens.go:495` — `if role == "" || len(rels[role]) >= maxRelationsPerNode {`
- `pkg/fusion/engine_facets.go:39` — `maxPaths = 10`
- `pkg/fusion/engine_facets.go:117` — `if len(out) >= maxPaths {`
- `config/streams.go:41` — `Discard string `json:"discard,omitempty"``
- `processor/graph-query/pathrag.go:200` — `maxNodes = reqNodes`
- `processor/graph-query/pathrag.go:202` — `maxNodes = 100`
Refusal and observation:
- `processor/graph-query/graphrag.go:1424` — `if truncated {`
- `processor/graph-query/graphrag.go:1425` — `c.logger.Warn("globalSearchTextBased: candidate corpus truncated",`
Nearest pattern instance:
- `pkg/fusion/engine_graph.go:71` — `Truncated bool `json:"truncated"``
- `processor/graph-query/summary.go:92` — `summary.EntitySampleTruncated = len(entityIDs) >= req.EntitySampleLimit`

## #1029 — projection: a copied contract listing a birth predicate in a mutable group silently deletes it on reconcile
Named sites:
- `pkg/projection/mutation_client.go:198` — `response, err := c.wire.Reconcile(ctx, graph.ReconcilePredicatesRequest{`
- `processor/graph-ingest/canonical_mutations.go:662` — `func reconcileSelectedPredicates(current, desired []message.Triple, predicates map[string]struct{}) []message.Triple {`
- `processor/agentic-tools/emit_lesson.go:632` — `func canonicalLessonContent(args emitLessonArgs) string {`
- `agentic/agent_lesson_entity.go:396` — `func LessonContract() contract.Contract {`
- `processor/agentic-tools/lesson_promotion.go:58` — `func NewLessonCurator(writer projection.PredicateReconciler, reader projection.AuthoritativeReader, logger *slog.Logger) *LessonCurator {`
- `processor/agentic-tools/lesson_promotion.go:71` — `func (c *LessonCurator) Promote(ctx context.Context, lessonEntityID string) error {`
`internal/builtinprojection/contracts.go`, the authoritative-contract home the issue names, no longer exists — `git grep` reports "no such path in the working tree" (deleted by PR #1109; the canonical contract now lives at `agentic/agent_lesson_entity.go` `LessonContract()`).
`docs/concepts/32-agent-memory.md:318` no longer reads "The composition root constructs the client from copied local projection contracts." — zero hits for that literal string anywhere under `docs/`; its current line 318 is pinned below under Adjacent claims.
Refusal and observation:
(none — see Searches)
Nearest pattern instance:
- `processor/graph-ingest/mutation_runtime.go:160` — `func (c *Component) recordMutationRejection(subject, reason, detail string) {`
- `processor/graph-ingest/mutation_runtime.go:165` — `c.logger.Warn("graph mutation rejected",`

## #1132 — graph_writer calls the panicking ModelEndpointEntityID before endpoint.Validate() — an identity fault crashes startup instead of taking the WARN-and-continue path
Named sites:
- `processor/agentic-loop/graph_writer.go:239` — `if w.platform.Org == "" || w.platform.Platform == "" {`
- `processor/agentic-loop/graph_writer.go:250` — `entityID := agentic.ModelEndpointEntityID(w.platform.Org, w.platform.Platform, name)`
- `processor/agentic-loop/graph_writer.go:254` — `if err := endpoint.Validate(); err != nil {`
- `agentic/entity_ids.go:19` — `func ModelEndpointEntityID(org, platform, endpointName string) string {`
- `agentic/entity_ids.go:29` — `func tryModelEndpointEntityID(org, platform, endpointName string) (string, error) {`
No exported `TryModelEndpointEntityID` exists anywhere in the tree — zero hits; the error-returning form (`tryModelEndpointEntityID`) is unexported to the `agentic` package, so `processor/agentic-loop` cannot call it directly.
Refusal and observation:
- `processor/agentic-loop/graph_writer.go:240` — `w.logger.Warn("graph_writer: cannot write model endpoints, platform identity missing",`
- `processor/agentic-loop/graph_writer.go:255` — `w.logger.Warn("graph_writer: model endpoint entity fails its contract; not born",`
Nearest pattern instance:
- `agentic/entity_ids.go:88` — `func TryLoopExecutionEntityID(org, platform, loopID string) (string, error) {`

## #1136 — graph-query/docs: distinguish result attribution from audited evidence and reconcile GraphRAG claims
Named sites:
- `processor/graph-query/graphrag.go:197` — `Sources            []Source              `json:"sources,omitempty"``
- `docs/concepts/09-graphrag-pattern.md:69` — `Starts from a known entity and explores its community. You provide an entity ID, and the search returns that entity's community membership, nearby entities within a hop radius, and their relationships.`
- `docs/concepts/09-graphrag-pattern.md:145` — `| `include_sources` | false | Include source-attribution records |`
- `docs/concepts/09-graphrag-pattern.md:171` — `| `relationships`, `sources` | Optional requested relationship and attribution data |`
- `docs/concepts/09-graphrag-pattern.md:238` — `1. Community summaries are cached—first query is slowest`
The breaking rename this issue's owner ruling calls for (`sources` → `attributions`) has not landed: `Sources`/`IncludeSources` are still the live field names in `processor/graph-query/graphrag.go`; no `Attributions` spelling exists anywhere under `processor/graph-query/`.
Refusal and observation:
(none — see Searches)
Nearest pattern instance:
(none — see Searches)

## #1143 — graph.ingest.* has no reserved boundary between the RPC plane and a consumer's persisted subjects, so the obvious wildcard binding silently shadows request/reply
Named sites:
- `processor/graph-ingest/query.go:27` — `sub, err := c.natsClient.SubscribeForRequests(ctx, "graph.ingest.query.entity", c.handleQueryEntityNATS)`
- `processor/graph-ingest/query.go:34` — `sub, err = c.natsClient.SubscribeForRequests(ctx, "graph.ingest.query.batch", c.handleQueryBatchNATS)`
- `processor/graph-ingest/query.go:41` — `sub, err = c.natsClient.SubscribeForRequests(ctx, "graph.ingest.query.prefix", c.handleQueryPrefixNATS)`
- `processor/graph-ingest/query.go:48` — `sub, err = c.natsClient.SubscribeForRequests(ctx, "graph.ingest.query.suffix", c.handleQuerySuffixNATS)`
- `processor/graph-query/router.go:19` — `"entity":       "graph.ingest.query.entity",`
- `processor/graph-query/entity_resolver.go:102` — `respData, err := c.natsClient.RequestClassified(queryCtx, "graph.ingest.query.suffix", reqData, 2*time.Second)`
- `processor/agentic-loop/lessons.go:20` — `const queryPrefixSubject = "graph.ingest.query.prefix"`
- `processor/gated-dag/reader.go:64` — `const prefixQuerySubject = "graph.ingest.query.prefix"`
- `composition/analyze.go:114` — `func explicitStreamCovers(streams config.StreamConfigs, streamName string, subjects []string) bool {`
- `service/component_manager.go:396` — `func (cm *ComponentManager) analyzeBootComposition() error {`
All seven literal-subject sites the issue names still exist unchanged at the cited spellings.
Refusal and observation:
(none — see Searches)
Nearest pattern instance:
- `composition/analyze.go:114` — `func explicitStreamCovers(streams config.StreamConfigs, streamName string, subjects []string) bool {`

## #1172 — graph/inference: hierarchy skips a foreign-authority entity with no log, metric or counter — the last unobservable omission in the ADR-102 boundary
Named sites:
- `graph/inference/hierarchy.go:191` — `func (h *HierarchyInference) GetHierarchyTriples(ctx context.Context, entityID string) ([]message.Triple, error) {`
- `graph/inference/hierarchy.go:217` — `if semtypes.ValidateEntityIDAuthority(entityID, h.config.Org, h.config.Platform, false) != nil {`
- `graph/inference/hierarchy.go:530` — `func (h *HierarchyInference) GetMetrics() (containersCreated, edgesCreated, edgesFailed int64) {`
Refusal and observation:
- `graph/inference/hierarchy.go:215` — `// No warning: for a federated deployment this is the ordinary case, and a`
- `graph/inference/hierarchy.go:218` — `return nil, nil`
Nearest pattern instance:
- `processor/rule/actions.go:689` — `func (e *ActionExecutor) foreignFiringSkipRecorder(ec *ExecutionContext, reason string) (record func(string), flush func()) {`

## #1212 — graph-ingest: ENTITY_SUFFIX_INDEX collides a loop with its run — same instance, same 'execution' type token; bySuffix is nondeterministic and removal cross-deletes
Named sites:
- `processor/graph-ingest/component.go:2780` — `func entitySuffixKeys(entityID string) (instance, typeInstance string) {`
- `processor/graph-ingest/component.go:2792` — `func (c *Component) updateSuffixIndex(ctx context.Context, entityID string) {`
- `processor/graph-ingest/component.go:2816` — `func (c *Component) removeSuffixIndex(ctx context.Context, entityID string) {`
- `processor/graph-ingest/query.go:594` — `func (c *Component) suffixFallbackScan(ctx context.Context, suffix string) (string, error) {`
The literal subject `graph.query.bySuffix` named in the title does not exist anywhere in the tree — zero hits; the live subject for suffix lookup is `graph.ingest.query.suffix` (see #1143 above).
Refusal and observation:
- `processor/graph-ingest/component.go:2801` — `if _, err := c.suffixBucket.Put(ctx, instance, indexValue); err != nil {`
- `processor/graph-ingest/component.go:2803` — `slog.String("key", instance), slog.Any("error", err))`
Nearest pattern instance:
- `pkg/types/entity_id_authority.go:35` — `func ValidateEntityIDAuthority(candidate, org, platform string, importLane bool) error {`

## Adjacent claims
- #348: none of the five draft PRs (1141, 1156, 1159, 1254, 1297) name it
- #348: body names gh#326, gh#337 — neither is in the 60-issue set (`docs/proposals/pattern-classification-2026-09/issues.md`)
- `docs/adr/060-unified-rpc-error-contract.md:1` — `# ADR-060: Unified RPC Error Contract — One Typed, Wrappable Error Over the Wire`
- #436: none of the five draft PRs name it
- #436: body names #429 — not in the 60-issue set
- #589: none of the five draft PRs name it
- #589: body names ADR-081, PR #585 — neither an issue in the 60-set
- `docs/adr/081-graph-view-subscription.md:1` — `# ADR-081: Graph View Subscription — Shared Read-Side Fan-Out Primitive`
- #608: none of the five draft PRs name it
- #608: body names #606 — not in the 60-issue set
- #618: none of the five draft PRs name it
- #618: body names #606, #613 — neither is in the 60-issue set
- #621: none of the five draft PRs name it
- #621: body names #603 — not in the 60-issue set
- #1029: none of the five draft PRs name it
- #1029: body names #979, #980, #981, #982, #582 — only #980 is in the 60-issue set
- `docs/proposals/gh1029-historical-constructor-correction.md:1` — `# Issue #1029 Historical Constructor Correction`
- `docs/proposals/gh1029-historical-constructor-correction.md:3` — `Status: accepted owner correction pending independent design review.`
- `openspec/changes/archive/2026-08-25-own-lesson-curator-contract/design.md:18` — `func NewNATSLessonCurator(client *natsclient.Client, logger *slog.Logger) *LessonCurator {`
- `docs/concepts/32-agent-memory.md:318` — `validated path is `LessonCurator.Promote`, which resolves that **every** cited`
- #1132: none of the five draft PRs name it
- #1132: body names #1112 — not in the 60-issue set
- #1136: none of the five draft PRs name it
- #1136: body names #606, #823, #829, ADR-059/#213 — none of #606/#823/#829/#213 is in the 60-issue set
- `docs/adr/059-epistemic-trust-tier.md:1` — `# ADR-059: Epistemic Trust Tier — Claim/Evidence Lifecycle for LLM-Derived Assertions`
- #1143: none of the five draft PRs name it
- #1143: body names PR #1148, and (as prior work, not open issues) #1101, #1095 — none of #1148/#1101/#1095 is in the 60-issue set; `status:blocked` on the issue itself, sequenced behind PR #1148
- #1172: none of the five draft PRs name it
- #1172: body names PR #1148 (its own round-2 recommendation, declined)
- `openspec/changes/archive/2026-08-29-entity-id-segment-semantics/conformance.md:445` — `### The recommended hierarchy counter — declined, and why (gap filed as #1172)`
- #1212: none of the five draft PRs name it
- #1212: body names PR #1210, and #1192 (owner ruling) — neither #1210 nor #1192 is in the 60-issue set

## Searches
- `gh issue view 348,436,589,608,618,621,1029,1132,1136,1143,1172,1212 --json number,title,body,labels,milestone` (one loop, one file) → 12 bodies fetched
- `gh pr view 1141,1156,1159,1254,1297 --json number,body` (one loop, one file) → 5 bodies fetched; grepped for all 12 issue numbers → 0 matches in any
- `git grep -n "func.*handleStrategyTemporal" processor/graph-query/graphrag.go` → 1
- `git grep -n "func.*handleStrategySpatial" processor/graph-query/graphrag.go` → 1
- `git grep -n "func.*loadEntities" processor/graph-query/graphrag.go` → 1
- `git grep -n "WrapTransient" processor/graph-query/graphrag.go` → 11
- `git grep -n "return nil, err" processor/graph-query/query.go` → 55 (sampled)
- `git grep -n "propagate" processor/graph-query/query.go` → 6
- `git grep -n "EntitySampleTruncated" processor/graph-query/summary.go` → 1
- `git grep -n "^# " docs/adr/060*.md` → 2 files
- `grep -n "348" docs/proposals/pattern-classification-2026-09/issues.md` → 1
- `git grep -n "unit_entity_prefix" processor/gated-dag/` → 14
- `git grep -n "hierarchy.container\|hierarchy.type.contains" processor/gated-dag/ pkg/ graph/` → 6
- `git grep -rn "429" processor/gated-dag/*.go` → 1
- `grep -n "436" docs/proposals/pattern-classification-2026-09/issues.md` → 1
- `git grep -n "entity.type.class\|EntityTypeClass" processor/gated-dag/` → 0
- `git grep -n "^func" processor/gated-dag/reader.go` → 3
- `git grep -n "StubMessageType" processor/gated-dag/reader.go` → 1 (comment only)
- `git grep -n "StubMessageType" processor/gated-dag/ graph/` → 1 (same, comment only)
- `git grep -n "errs\." processor/gated-dag/reader.go` → 0
- `git grep -n "slog\.\|logger\." processor/gated-dag/reader.go` → 0
- `git grep -n "metric\|Counter\|Gauge" processor/gated-dag/reader.go` → 0
- `git grep -n "func.*Stalled" processor/gated-dag/*.go` → 0
- `git grep -rn "Stalled" processor/gated-dag/*.go | grep -v _test` → 9
- `git grep -n "func.*Watch" graph/inference/storage.go` → 1
- `git grep -n "\.Watch(" graph/inference/*.go` → 1
- `git grep -n "func.*Watch\|ANOMALY_INDEX" graph/inference/review_worker.go` → 2
- `grep -n "589" docs/proposals/pattern-classification-2026-09/issues.md` → 1
- `ls docs/adr | grep -i 081` → 1
- `git grep -n "func.*markFailed" graph/clustering/enhancement_worker.go` → 1
- `git grep -n "func.*GetCommunity\b" graph/clustering/storage.go` → 1
- `git grep -n "SaveCommunity\|func.*SaveCommunity" graph/clustering/storage.go` → 5 (sampled)
- `git grep -n "func.*Start\b\|startEnhancementWorker\|monitorLLMHealth" processor/graph-clustering/component.go` → 9
- `git grep -n "func.*GetEntityCommunity" graph/clustering/storage.go` → 1
- `git grep -n "func.*getEntityCommunity" processor/graph-clustering/query.go` → 1
- `git grep -n "InferRelationshipsFromCommunities" graph/clustering/lpa.go graph/clustering/types.go graph/clustering/doc.go` → 5
- `git grep -n "GetEdgeWeight" processor/graph-clustering/component.go graph/clustering/lpa.go` → 12 (sampled)
- `git grep -n 'fmt.Sprintf("%d.%s"' graph/clustering/storage.go` → 1
- `grep -n "^| #608" docs/proposals/pattern-classification-2026-09/issues.md` → 1
- `grep -n "^| #606" docs/proposals/pattern-classification-2026-09/issues.md` → 0
- `git grep -n "GRAPH_STATUS\|KeyGraphEmbedding\|KeyGraphIndex" processor/graph-embedding/component.go graph/query/client.go pkg/fusion/fusionnats/client.go processor/graph-clustering/component.go` → error: `graph/query/client.go` does not exist (0 for that path)
- `git grep -n "readiness.NewWatcher" -- '*.go'` → 5
- `git grep -n "KeyGraphIndex\|KeyGraphEmbedding" -- '*.go' | grep -v _test` → 13 (sampled)
- `git grep -n "func.*FindSimilar" processor/graph-clustering/similarity.go` → 1
- `git grep -n "ErrorCodeIndexNotReady" processor/graph-embedding/component.go` → 3
- `grep -n "No consumer" processor/graph-embedding/query.go` → 1
- `grep -n "gates on embedding.ready" processor/graph-embedding/query.go` → 1
- `grep -n "^| #618" docs/proposals/pattern-classification-2026-09/issues.md` → 1
- `git grep -n "MaxTotalEntitiesInSearch" processor/graph-query/graphrag.go` → 4
- `git grep -n "entityIDSet" processor/graph-query/graphrag.go` → 4
- `git grep -n "maxRelationsPerNode" pkg/fusion/engine_lens.go` → 2
- `git grep -n "Truncated" pkg/fusion/engine_lens.go pkg/fusion/engine_graph.go pkg/fusion/engine_facets.go` → 30 (sampled)
- `git grep -n "maxPaths" pkg/fusion/engine_facets.go` → 5
- `git grep -n "DiscardOld\|type StreamConfig struct" config/streams.go` → 14 (sampled)
- `git grep -n "maxNodes\|MaxNodes" processor/graph-query/pathrag.go` → 8
- `grep -n "Discard string" config/streams.go` → 1
- `grep -n "^	Truncated bool" pkg/fusion/engine_graph.go` → 3 (sampled)
- `git grep -n "Reconcile(ctx, graph.ReconcilePredicatesRequest" pkg/projection/mutation_client.go` → 1
- `git grep -n "func reconcileSelectedPredicates" processor/graph-ingest/canonical_mutations.go` → 1
- `git grep -n "lesson-record\|lesson-lifecycle" internal/builtinprojection/contracts.go` → error: path does not exist (0)
- `git grep -n "composition root constructs the client from copied" docs/` → 0
- `git grep -n "func canonicalLessonContent" processor/agentic-tools/emit_lesson.go` → 1
- `git grep -rn "func.*LessonCurator.*Promote" .` → 1
- `git grep -rn "func NewLessonCurator\|func NewNATSLessonCurator" .` → 3
- `grep -n "^| #1029" docs/proposals/pattern-classification-2026-09/issues.md` → 1
- `grep -n "^| #1112" docs/proposals/pattern-classification-2026-09/issues.md` → 0
- `git grep -n "func ModelEndpointEntityID\|func TryModelEndpointEntityID" agentic/*.go` → 1
- `git grep -n "ModelEndpointEntityID\|endpoint.Validate" processor/agentic-loop/graph_writer.go` → 4
- `git grep -n "TryModelEndpointEntityID" -- '*.go'` → 0
- `grep -n 'w.platform.Org == ""\|logger.Warn("graph_writer' processor/agentic-loop/graph_writer.go` → 16
- `grep -n "^func tryModelEndpointEntityID\|^func TryLoopExecutionEntityID" agentic/entity_ids.go` → 2
- `git grep -in "sources" processor/graph-query/graphrag.go` → 13 (sampled)
- `git grep -in "attributions" processor/graph-query/*.go` → 0
- `git grep -iln "graphrag\|GraphRAG" docs/concepts/*.md docs/operations/*.md` → 9 (sampled)
- `grep -n "sources\|cached\|hop\|semantic communit" docs/concepts/09-graphrag-pattern.md` → 4
- `git grep -n "^# " docs/adr/059*.md` → 1
- `grep -n "^| #823\|^| #829\|^| #606" docs/proposals/pattern-classification-2026-09/issues.md` → 0
- `git grep -n "SubscribeForRequests(ctx, \"graph.ingest" processor/graph-ingest/query.go` → 4
- `git grep -n "graph.ingest.query" processor/graph-query/router.go processor/graph-query/entity_resolver.go processor/agentic-loop/lessons.go processor/gated-dag/reader.go` → 9 (sampled)
- `git grep -n "func Analyze\|func explicitStreamCovers" composition/analyze.go` → 2
- `git grep -n "analyzeBootComposition" service/component_manager.go` → 3
- `grep -n "^| #1143" docs/proposals/pattern-classification-2026-09/issues.md` → 1
- `grep -n "^| #1148\|^| #1101\|^| #1095" docs/proposals/pattern-classification-2026-09/issues.md` → 0
- `git grep -n "func.*GetHierarchyTriples" graph/inference/hierarchy.go` → 1
- `git grep -n "foreign\|Authority" graph/inference/hierarchy.go` → 6 (sampled)
- `git grep -n "func.*GetMetrics" graph/inference/hierarchy.go` → 1
- `git grep -n "foreign_authority" processor/rule/*.go` → 10 (sampled)
- `grep -n "^| #1172" docs/proposals/pattern-classification-2026-09/issues.md` → 1
- `find openspec/changes -iname "conformance.md" | xargs grep -l "1172\|foreign_authority\|hierarchy_entities_skipped"` → 1
- `grep -n "hierarchy\|foreign_authority\|GetHierarchyTriples\|1172" openspec/changes/archive/2026-08-29-entity-id-segment-semantics/conformance.md` → 13 (sampled)
- `grep -n "No warning" graph/inference/hierarchy.go` → 1
- `git grep -n "func.*foreignFiringSkipReason\|firingSkip.*Inc\|foreign_authority\"" processor/rule/*.go` → 1
- `grep -n "foreignFiringSkipReason(" processor/rule/actions.go` → 3
- `grep -n "func.*foreignFiringSkipRecorder" processor/rule/actions.go` → 1
- `git grep -n "func.*entitySuffixKeys" processor/graph-ingest/component.go` → 1
- `git grep -n "func.*updateSuffixIndex\|func.*removeSuffixIndex\|func.*suffixFallbackScan" processor/graph-ingest/*.go` → 3
- `git grep -n "graph.query.bySuffix\|bySuffix" processor/graph-ingest/query.go processor/graph-query/*.go` → 0
- `grep -n "^| #1210\|^| #1192" docs/proposals/pattern-classification-2026-09/issues.md` → 0
- `grep -n "^| #1212" docs/proposals/pattern-classification-2026-09/issues.md` → 1
- `git grep -n "^func\|^// " pkg/types/entity_id_authority.go` → 10 (sampled)
- `grep -n "^| #979\|^| #980\|^| #981\|^| #982\|^| #582\|^| #613\|^| #603\|^| #824\|^| #1007\|^| #429\|^| #606" docs/proposals/pattern-classification-2026-09/issues.md` → 3 (only #824, #980, #1007 matched; #979/#981/#982/#582/#613/#603/#429/#606 = 0)
- `grep -n "^| #213\|^| #603\|^| #979\|^| #981\|^| #982\|^| #582\|^| #326\|^| #337\|^| #429\|^| #980" docs/proposals/pattern-classification-2026-09/issues.md` → 1 (only #980)

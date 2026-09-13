# Inventory: #1234 slice framework-config
base: 32aeddf73612d157261ace65d9810750d2d602e0

## #659 — Schema generator drops type:number (float64) config fields from generated schemas
Named sites:
- `processor/graph-clustering/component.go:113` — `SiblingWeight      float64 `json:"sibling_weight,omitempty" schema:"type:number,description:Edge weight for synthesized sibling edges (default 0.7)"``
- `processor/graph-clustering/component.go:151` — `SemanticSimilarityThreshold float64 `json:"semantic_similarity_threshold,omitempty" schema:"type:number,description:Minimum cosine similarity for a candidate to count toward an entity's top-k semantic neighbors (starting value 0.75)"``
- `processor/graph-clustering/component.go:153` — `SemanticEdgeWeight          float64 `json:"semantic_edge_weight,omitempty" schema:"type:number,description:Weight of a synthesized mutual-kNN semantic edge (starting value 0.9; below explicit 1.0, above the rebalanced structural tiers)"``
- `component/schema_tags.go:318` — `func isValidType(t string) bool {`
- `component/schema_tags.go:320` — `"string", "int", "bool", "float",`
- `component/schema_tags.go:406` — `directives, err := ParseSchemaTag(schemaTag)`
- `component/schema_tags.go:410` — `continue`
Refusal and observation:
(none — see Searches) — `GenerateConfigSchema`'s field loop (`component/schema_tags.go:406-411`) calls `ParseSchemaTag`, and on error just `continue`s; no `errs.`, `slog.`, or metric call anywhere in the loop body.
Nearest pattern instance:
- `natsclient/kvspec.go:154` — `func (s BucketSpec) Validate() error {`

## #734 — An unrecognized schema Type spelling silently becomes "string" AND skips runtime validation
Named sites:
- `component/schema.go:128` — `func validateType(fieldName string, value any, propSchema PropertySchema) *ValidationError {`
- `component/schema.go:158` — `case "float":`
- `component/schema.go:171` — `return nil`
- `cmd/openapi-generator/main.go:405` — `func mapTypeToJSONSchema(propType string) string {`
- `cmd/openapi-generator/main.go:409` — `case "float":`
- `cmd/openapi-generator/main.go:418` — `return "string"`
- `component/schema_tags.go:318` — `func isValidType(t string) bool {`
- `component/schema_tags.go:320` — `"string", "int", "bool", "float",`
Refusal and observation:
(none — see Searches) — `validateType` has no `default` arm (falls to the unconditional `return nil` at `component/schema.go:171`); `mapTypeToJSONSchema` has no `default` case beyond `return "string"`; neither calls `errs.`, `slog.`, or increments a metric.
Nearest pattern instance:
- `natsclient/kvspec.go:154` — `func (s BucketSpec) Validate() error {`

## #764 — pkg/dispatch: BoundedDispatcher.Stats() chain dead-ends — no consumer outside the package
Named sites:
- `pkg/dispatch/dispatcher.go:244` — `func (d *BoundedDispatcher[W]) Stats() worker.PoolStats {`
- `pkg/dispatch/dispatcher.go:245` — `return d.pool.Stats()`
- `pkg/dispatch/keyed_pool.go:419` — `func (p *KeyedPool[W]) Stats() KeyedStats {`
Refusal and observation:
(none — see Searches) — `Stats()` is a one-line passthrough; no `errs.`, `slog.`, or metric call in either function.
Nearest pattern instance:
(none — see Searches) — `pkg/dispatch` has `slog.` observation (`pkg/dispatch/completion_watcher.go:153-154`, `:172-173`) but no `errs.Classified*` refusal; none of the six shapes present in-package.

## #980 — lessons: NewNATSLessonStore swallows the graphmutation client-construction error
Named sites:
- `processor/agentic-tools/emit_lesson.go:160` — `func NewNATSLessonStore(client *natsclient.Client) LessonStore {`
- `processor/agentic-tools/emit_lesson.go:161` — `wire, _ := graphmutation.NewClient(client, lessonQueryTimeout)`
- `processor/agentic-tools/emit_lesson.go:166` — `if s == nil || s.client == nil || s.reader == nil {`
Refusal and observation:
(none — see Searches) — `NewNATSLessonStore` discards the `graphmutation.NewClient` error with `_`; no `errs.`, `slog.`, or metric call in the function.
Nearest pattern instance:
- `processor/agentic-tools/emit_lesson.go:180` — `var classified *errs.ClassifiedError`
- `processor/agentic-tools/emit_lesson.go:181` — `if !errors.As(err, &classified) || classified.Code != graph.ErrorCodeEntityExists {`

## #1123 — Service.RegisterMetrics has no production caller — the interface method is a phantom; wire it into service composition or delete it
Named sites:
- `service/base.go:389` — `func (s *BaseService) RegisterMetrics(_ metric.MetricsRegistrar) error {`
- `service/base.go:494` — `RegisterMetrics(registrar metric.MetricsRegistrar) error`
- `service/storage_observability.go:246` — `// RegisterMetrics, because nothing in the framework calls that method`
- `service/storage_observability.go:351` — `func (s *StorageObservabilityService) RegisterMetrics(registrar metric.MetricsRegistrar) error {`
- `service/component_manager.go:249` — `// rather than only through the Service interface's RegisterMetrics:`
Refusal and observation:
(none — see Searches) — `BaseService.RegisterMetrics` returns `nil` unconditionally; `StorageObservabilityService.RegisterMetrics` delegates to `s.metrics.register(registrar)` with no `errs.`, `slog.`, or metric call of its own.
Nearest pattern instance:
- `service/component_manager.go:249` — `// rather than only through the Service interface's RegisterMetrics:`

## #1126 — metric.Registry.RegisterGaugeVec discards the existing collector on a duplicate registration — a second registrant writes to a detached GaugeVec /metrics never scrapes
Named sites:
- `metric/registry.go:245` — `func (r *MetricsRegistry) RegisterGaugeVec(serviceName, metricName string, gaugeVec *prometheus.GaugeVec) error {`
- `metric/registry.go:246` — `_, err := r.RegisterOrGetGaugeVec(serviceName, metricName, gaugeVec)`
- `metric/registry.go:56` — `return nil, errs.WrapFatal(err, "MetricsRegistry", "RegisterOrGetGaugeVec",`
Refusal and observation:
- `metric/registry.go:56` — `return nil, errs.WrapFatal(err, "MetricsRegistry", "RegisterOrGetGaugeVec",`
Uncoded (`errs.WrapFatal`), inside `RegisterOrGetGaugeVec`; `RegisterGaugeVec` itself only propagates `err`, no `errs.`/`slog.`/metric call of its own.
Nearest pattern instance:
- `natsclient/jetstream_metrics.go:150` — `if m.policyRequested, err = registry.RegisterOrGetGaugeVec("jetstream", "consumer_max_ack_pending_requested", m.policyRequested); err != nil {`

## #1187 — greenfield: delete three zero-caller exported surfaces not caused by #1168
Named sites:
- `message/base_message.go:81` — `func WithFederation(pcfg platform.Config) Option {`
- `message/base_message.go:91` — `func WithFederationAndTime(pcfg platform.Config, createdAt time.Time) Option {`
- `message/federation.go:22` — `type FederationMeta interface {`
- `message/federation.go:50` — `func NewFederationMeta(source string, pcfg platform.Config) *DefaultFederationMeta {`
- `message/federation.go:60` — `func NewFederationMetaWithTime(`
- `message/federation.go:90` — `//	    globalID := BuildGlobalID(entityID, platform)`
- `pkg/types/entity_id.go:268` — `func (eid EntityID) DeploymentPrefix() string {`
- `pkg/types/entity_id.go:275` — `return eid.DeploymentPrefix() + "." + eid.System`
- `config/minimal_config.go:11` — `type MinimalConfig struct {`
- `config/minimal_config.go:39` — `func LoadMinimalConfig(path string) (*MinimalConfig, error) {`
- `openspec/specs/entity-id-contract/spec.md:488` — `### Requirement: Prefix lengths have fixed meanings and the instance position is last`
- `openspec/specs/entity-id-contract/spec.md:490` — ``pkg/types` MUST export the named prefix levels `DeploymentPrefix` (two positions), `SourcePrefix` (three),`
Caller counts (re-derived at base, `gopls references`):
`WithFederation` (`message/base_message.go:81:6`) — 0 references.
`WithFederationAndTime` (`message/base_message.go:91:6`) — 0 references.
`FederationMeta` (`message/federation.go:22:6`) — 2 references, both internal to `message/federation.go` (`GetPlatform` at :96, `GetUID` at :106); `BuildGlobalID` named in the doc comment at `:90` has no matching `func BuildGlobalID` anywhere (zero-hit search).
`message.GetPlatform`/`message.GetUID` (the only production users of `FederationMeta`) — 0 external callers (zero-hit search).
`DeploymentPrefix` (`pkg/types/entity_id.go:268:21`) — 4 references: 1 production, in-package (`SourcePrefix` at `pkg/types/entity_id.go:275`), and 3 test references (`message/parse_entity_id_test.go:190`, `pkg/types/entity_id_semantics_test.go:42,51,55`). Not a zero-caller surface — it has a live in-package caller.
`MinimalConfig` (`config/minimal_config.go:11:6`) — 4 references, all internal to `config/minimal_config.go` (`Validate`, `LoadMinimalConfig`'s return type, `LoadMinimalConfig`'s local var, `ToJSON`); zero references outside the file.
`LoadMinimalConfig` (`config/minimal_config.go:39:6`) — 0 references (the one apparent call site, `service/doc.go:249`, is inside a `//` doc-comment example, not code).
Refusal and observation:
(none — see Searches) — none of the three named surfaces call `errs.`, `slog.`, or a metric.
Nearest pattern instance:
- `pkg/types/entity_id_authority.go:35` — `func ValidateEntityIDAuthority(candidate, org, platform string, importLane bool) error {`

## Adjacent claims
- #659: none of the five draft PRs (1141, 1156, 1159, 1254, 1297) name it.
- #659: body names #734 only in reverse (see #734's own line below); #659's own body names no other issue.
- #734: body names #659.
- #734: none of the five draft PRs name it.
- #764: body names #712, #732, #761.
- #764: none of the five draft PRs name it.
- #980: body names none of the 60-set.
- #980: none of the five draft PRs name it.
- #1123: body names PR #1116 and #1093 (not in the five-PR set checked).
- #1123: none of the five draft PRs (1141, 1156, 1159, 1254, 1297) name it.
- #1126: body names PR #1116 and #1093 (not in the five-PR set checked).
- #1126: none of the five draft PRs name it.
- #1187: body names #1168 (repeatedly) and #1186.
- #1187: none of the five draft PRs name it.
- #1187: cites `openspec/specs/entity-id-contract/spec.md:488` (`### Requirement: Prefix lengths have fixed meanings and the instance position is last`) and `:490` (the `MUST export ... DeploymentPrefix` line) as the live finding that the deletion would hole.
- #1187: cites ADR-102 by name (no heading line given in the body; not independently re-pinned — see Searches).

## Searches
- `git grep -n "mapTypeToJSONSchema" -- '*.go'` → 5 (4 non-test)
- `git grep -rn "func validateType" -- '*.go'` → 1
- `git grep -rln "schema:generate\|schema.generate" Taskfile.yml taskfiles` → 2
- `find . -iname "*schemagen*"` → 0
- `git grep -n '"type:' -- '*.go'` → 13 (sampled first 20 lines)
- `git grep -n 'case "float"'` → 4 (`cmd/openapi-generator/main.go:409`, `component/schema.go:158`, `component/schema_tags.go:680`, `output/otel/otlp_exporter_test.go:407`; plus `test/contract/schema_contract_test.go:665` has `case "float", "float64"`)
- `git grep -n '"float64"' -- '*.go' | grep -v _test` → 1 (`processor/rule/expression/types.go:85`, unrelated `String()`-style helper)
- `git grep -n "func isValidType" -- '*.go'` → 1
- `git grep -n "semantic_similarity_threshold\|semantic_edge_weight\|sibling_weight\|system_peer_weight\|enable_semantic_edges\|semantic_max_neighbors" -- '*.go' | grep -v _test` → 12
- sed inspection of `component/schema_tags.go:331-420` (`GenerateConfigSchema`) → confirms `directives, err := ParseSchemaTag(schemaTag); if err != nil { continue }` with no log/errs/metric call
- `grep -n "case \"string\"\|case \"int\"\|...\|switch.*[Tt]ype" cmd/openapi-generator/main.go` → 6 (all inside `mapTypeToJSONSchema`; no other type-switch in the generator)
- `sed -n '127,172p' component/schema.go | grep -n "errs\.\|slog\.\|log\.\|metric"` → 0
- `sed -n '364,430p' component/schema_tags.go | grep -n "errs\.\|slog\.\|log\.\|metric"` → 0
- `sed -n '405,419p' cmd/openapi-generator/main.go | grep -n "errs\.\|slog\.\|log\.\|metric"` → 0
- `git grep -n "func.*BucketSpec.*Validate\|unknown RetentionKind" -- '*.go' | grep -v _test` → 2
- `grep -n "func.*Stats\|BoundedDispatcher" pkg/dispatch/dispatcher.go` → 10
- `git grep -n "func.*KeyedPool.*Stats\|KeyedPool) Stats" pkg/dispatch/` → 1
- `git grep -n "\.Stats()" -- '*.go' | grep -v _test` → 7 (only `pkg/dispatch/dispatcher.go:245` is the chain in question; the rest are unrelated `cache`/`historyCache`/`regex_cache` hits)
- `gopls references pkg/dispatch/dispatcher.go:244:32` → 2 (both `pkg/dispatch/dispatcher_test.go`, no production caller)
- `grep -n "errs\.Classified\|slog\." pkg/dispatch/*.go | grep -v _test` → 8 (all `slog.`, none `errs.Classified`)
- `grep -n "func NewNATSLessonStore\|graphmutation.NewClient" processor/agentic-tools/emit_lesson.go` → 2
- `grep -n "errs\.\|slog\.\|metric" processor/agentic-tools/emit_lesson.go` → many (sampled first 30; none inside `NewNATSLessonStore` itself)
- `grep -rn "func.*NewClient" processor/agentic-tools graphmutation graph` → 1 (unrelated `runner/client.go`; `graphmutation.NewClient`'s own definition not independently re-opened — see NOT RUN)
- `git grep -n "^func New.*Store(" processor/agentic-tools/*.go` → 3
- `grep -n "RegisterMetrics" service/storage_observability.go` → 2
- `git grep -n "RegisterMetrics" -- '*.go' | grep -v _test` → 8
- `grep -rn '\.RegisterMetrics(' --include='*.go' .` → 9 (all `metric/integration_test.go`)
- `gopls references service/base.go:494:2` → 0 (interface method decl only; retried, exit 0, empty)
- `grep -n "func.*RegisterGaugeVec\|func.*RegisterOrGetGaugeVec" metric/registry.go` → 2
- `grep -n "errs\.\|slog\." metric/registry.go` → 10 (sampled first 20)
- `grep -rn "RegisterOrGetGaugeVec(" --include='*.go' . | grep -v _test` → 5 (3 in `natsclient/jetstream_metrics.go`, 2 in `metric/registry.go`)
- `grep -n "func.*RegisterOrGet" metric/registry.go` → 1 (no `RegisterOrGetCounterVec` sibling exists)
- `git grep -n "FederationMeta\|WithFederation\b\|WithFederationAndTime\|BuildGlobalID" -- '*.go' | grep -v _test` → 19
- `git grep -n "func.*DeploymentPrefix\|\.DeploymentPrefix(" -- '*.go' | grep -v _test` → 2
- `git grep -n "MinimalConfig\|LoadMinimalConfig" -- '*.go' | grep -v _test` → 8
- `git grep -n "^func BuildGlobalID\|func BuildGlobalID" -- '*.go'` → 0
- `git grep -n "NewFederationMeta(\|NewFederationMetaWithTime(" -- '*.go'` → 4 (2 declarations, 2 call sites, both inside `message/base_message.go`'s `WithFederation`/`WithFederationAndTime`)
- `git grep -n "message\.GetPlatform(\|message\.GetUID(\|GetPlatform(msg\|GetUID(msg" -- '*.go' | grep -v "message/federation.go"` → 0
- `gopls references message/base_message.go:81:6` (WithFederation) → 0
- `gopls references message/base_message.go:91:6` (WithFederationAndTime) → 0
- `gopls references message/federation.go:22:6` (FederationMeta) → 2
- `gopls references pkg/types/entity_id.go:268:21` (DeploymentPrefix) → 4
- `gopls references config/minimal_config.go:11:6` (MinimalConfig) → 4
- `gopls references config/minimal_config.go:39:6` (LoadMinimalConfig) → 0
- `ls config/*.go; grep -n "^func " config/*.go | grep -v _test` → scan of 10 files, no create-vs-exists/admission-gate shape found for `MinimalConfig`'s package
- `ls message/*.go` → scan of 16 files; no create-vs-exists/admission-gate shape found beyond `federation.go` itself
- `grep -n "errs\.\|slog\." config/minimal_config.go message/federation.go pkg/types/entity_id.go` → 1 (unrelated, `pkg/types/entity_id.go:256`, a different function)
- `grep -n "^### Requirement: Prefix lengths" openspec/specs/entity-id-contract/spec.md` → 1
- `sed -n '485,495p' openspec/specs/entity-id-contract/spec.md` → confirms the MUST-export line
- `for n in 659 734 764 980 1123 1126 1187; do gh issue view $n ...; done` → 7 bodies fetched, 1 file
- `grep -oE "#[0-9]+" <scratch issues file> | sort -u` → 14 distinct issue/PR numbers found across the 7 bodies
- `for p in 1141 1156 1159 1254 1297; do gh pr view $p --json number,body; done` → 5 bodies fetched, 1 file; grepped for each of the 7 issue numbers → 0 matches for all seven
- `grep -n "#659\|#734\|#764\|#980\|#1123\|#1126\|#1187" docs/proposals/pattern-classification-2026-09/issues.md` → 7 (the 60-set table row for each, one per row)

NOT RUN:
- `graphmutation.NewClient`'s own definition/signature was not independently opened (only its call site and return-value-discard confirmed).
- ADR-102 was not independently re-pinned by heading line (the #1187 body names it without a file:line; not located under any spelling in the time budget).
- `schemas/graph-clustering.v1.json`'s current generated content was not re-opened to confirm the four float64 fields are still absent from the generated output today — the source-side cause (`isValidType` missing `"number"`) was confirmed instead.

# Explorer inventory — graph-read-tools-signal-absence (#1261)

base: 5b7c3db3a149cc62e90beb2a3f4d41622b65db53

Materialized 2026-09-05 from the collapsed comment on #1261 (`semstreams-explorer`, 2026-09-04) so `task inventory:verify`
has a line-addressable artifact; body unchanged below. The architect's verification of it is `inventory-verification.md`.

Explorer inventory behind the findings above (`semstreams-explorer`, 2026-09-04, base `5b7c3db3`, 60 recorded searches; enumeration only, no judgment). Sister-repo pins are read-only observations.

<details><summary>inventory-ontology-shape.md (317 lines)</summary>

# Inventory: how a domain's vocabulary and graph shape reach an agent

base: 5b7c3db3a149cc62e90beb2a3f4d41622b65db53 (branch main)

## 1. The Triple primitive

- `message/triple.go:15` — `const EntityReferenceDatatype = "@id"`
- `message/triple.go:37` — `type Triple struct {` — fields at:
  - `message/triple.go:41` — `Subject string \`json:"subject"\`` (EntityID.Key() format)
  - `message/triple.go:46` — `Predicate string \`json:"predicate"\`` (three-level dotted `domain.category.property`)
  - `message/triple.go:51` — `Object any \`json:"object"\`` (literal primitive OR entity-ID string reference)
  - `message/triple.go:56` — `Source string`
  - `message/triple.go:61` — `Timestamp time.Time`
  - `message/triple.go:70` — `Confidence float64`
  - `message/triple.go:80` — `Context string \`json:"context,omitempty"\`` (correlation ID)
  - `message/triple.go:86` — `Datatype string \`json:"datatype,omitempty"\`` — "optional RDF datatype hint for the Object value" (e.g. `xsd:float`, `xsd:dateTime`, or `EntityReferenceDatatype`/`@id`)
  - `message/triple.go:92` — `ExpiresAt *time.Time \`json:"expires_at,omitempty"\``
- `message/triple.go:123` — `type TripleGenerator interface { Triples() []Triple }` (superseded in practice by `graph.Graphable`, see below; doc comment at `message/triple.go:97-121` shows the same "use vocabulary predicate constants" pattern)
- `message/triple.go:135` — `func (t Triple) IsRelationship() bool` — returns true only when `Object` is a `string` AND (`Datatype == ""` or `Datatype == EntityReferenceDatatype`) AND `IsValidEntityID(object)`; an explicit non-`@id` `Datatype` makes the object a literal even if canonical-ID-shaped (`message/triple.go:161-165`)
- `message/triple.go:149` — `func IsValidEntityID(s string) bool` — delegates to `pkg/types`
- `message/triple.go:164` — `func (t Triple) IsExpired() bool`
- `graph/graphable.go:54` — `type Graphable interface {` — the interface actually implemented across the repo (29 implementers via `gopls implementation graph/graphable.go:54:6`):
  - `graph/graphable.go:56` — `EntityID() string`
  - `graph/graphable.go:59` — `Triples() []message.Triple`
  - Doc comment `graph/graphable.go:8-52` is the adopter-facing "how to shape triples" guidance: triple-based design, example implementation, explicit statement that payloads self-declare entities/relationships instead of infrastructure guessing
- `graph/entity_predicate_contract.go:19-27` — `type EntityStateContractField string` with three named identity-bearing surfaces: `EntityStateContractFieldID` ("id"), `EntityStateContractFieldSubject` ("subject" — a persisted `Triple.Subject`), `EntityStateContractFieldReference` ("reference" — "an explicitly marked @id object")
- Adopter-facing predicate-shaping doc comments:
  - `vocabulary/predicates.go:1-19` — "Predicate naming conventions" (domain/category/property, no underscores)
  - `docs/basics/04-vocabulary.md:1-40` — "Vocabulary: Designing Your Predicates" tutorial (dotted notation, avoid underscores/camelCase/abbreviations)
  - `vocabulary/doc.go` — package doc referenced by `vocabulary.GetPredicateMetadata` example at `vocabulary/doc.go:87`

### JSON-LD / @context / IRI / CURIE handling
- `vocabulary/export/jsonld.go:13` — `func writeJSONLD(w io.Writer, triples []message.Triple, opts *options) error` — writes `@context`+`@graph`
- `vocabulary/export/jsonld.go:35` — `node := map[string]any{"@id": subIRI}`
- `vocabulary/export/jsonld.go:90` — `return map[string]any{"@id": obj.iri}` (object side, when the object resolves to an IRI)
- `vocabulary/predicates.go:373` (`PredicateMetadata.StandardIRI` field) — "W3C/RDF equivalent IRI for standards compliance (optional)"
- `vocabulary/registry.go:142` — `func WithIRI(iri string) Option` — sets `StandardIRI`
- `vocabulary/export/prefix.go`, `vocabulary/export/turtle.go`, `vocabulary/export/ntriples.go`, `vocabulary/export/object.go` — sibling exporters (Turtle, N-Triples) in the same `vocabulary/export` package (not read line-by-line; located via `ls`)
- `vocabulary/iris.go:114,168` — CURIE/kebab-case naming-convention conversion helpers (`toKebabCase`)
- No repo-wide CURIE type or `@context`-consuming parser was found (see Not found).

## 2. The vocabulary registry

Package: `vocabulary` (root package `github.com/c360studio/semstreams/vocabulary`), sub-packages `vocabulary/agentic`, `vocabulary/governance`, `vocabulary/rulepacks`, `vocabulary/examples`, `vocabulary/bfo`, `vocabulary/cco`, `vocabulary/export`.

- `vocabulary/predicates.go:351` — `type PredicateMetadata struct {` — carries (line refs are struct-field doc comments, all in this file):
  - `Name string` (predicate constant)
  - `Description string`
  - `DataType string` (expected Go type)
  - `Units string`
  - `Range string`
  - `Domain string`, `Category string`
  - `StandardIRI string` (RDF/OWL/SKOS equivalent — see `vocabulary/standards.go`)
  - `IsAlias bool`, `AliasType AliasType`, `AliasPriority int`
  - `InverseOf string` — "Both predicates in an inverse pair should be registered with their InverseOf pointing to each other. The registry stores relationships; it does not auto-generate inverse triples at runtime."
  - `IsSymmetric bool`
  - `RuleOpaque bool` — rule-validator rejects rule conditions naming a rule-opaque predicate
  - `Role PredicateRole` (`vocabulary/predicates.go:447`) — enum `RoleUnspecified|RoleIdentity|RoleLabel|RoleRelationship|RoleMetric|RoleDescriptive|RoleMetadata`
  - `Weight float64` — signed ranking salience
  - No explicit `Range`-as-type-of-object-entity field (Range is a free-text description, not a domain/range typing contract distinguishing entity-typed vs literal-typed edges) — no `Cardinality` field found (see Not found)
- `vocabulary/registry.go:12` — `type AliasType string` with constants `AliasTypeIdentity`, `AliasTypeLabel`, `AliasTypeAlternate`, `AliasTypeExternal`, `AliasTypeCommunication` (`vocabulary/registry.go:14-73`), each doc-commented with W3C/RDF standard mappings (owl:sameAs, skos:prefLabel, etc.) and a resolution flag (`CanResolveToEntityID`, `vocabulary/registry.go:78`)
- `vocabulary/registry.go:101` — `type Option func(*PredicateMetadata)` functional-option registration API:
  - `WithDescription` (`:104`), `WithDataType` (`:112`), `WithUnits` (`:120`), `WithRange` (`:128`), `WithIRI` (`:142`), `WithAlias` (`:159`), `WithInverseOf` (`:184`), `WithRuleOpaque` (`:201`), `WithSymmetric` (`:219`), `WithRole` (`:234`), `WithWeight` (`:253`)
- `vocabulary/registry.go:287` — `func Register(name string, opts ...Option)` — the primary registration entry point (panics on invalid metadata per file doc)
- `vocabulary/registry.go:353` — `func RegisterPredicate(meta PredicateMetadata)` — lower-level struct-literal registration
- `vocabulary/registry.go:418` — `func GetPredicateMetadata(predicate string) *PredicateMetadata`
- `vocabulary/registry.go:433` — `func ListRegisteredPredicates() []string`
- `vocabulary/registry.go:450` — `func DiscoverAliasPredicates() map[string]int`
- `vocabulary/registry.go:475` — `func DiscoverLabelPredicates() map[string]int`
- `vocabulary/registry.go:501` — `func GetInversePredicate(predicate string) string`
- `vocabulary/registry.go:520` — `func IsRuleOpaque(predicate string) bool`
- `vocabulary/registry.go:536` — `func IsSymmetricPredicate(predicate string) bool`
- `vocabulary/registry.go:549` — `func HasInverse(predicate string) bool`
- `vocabulary/registry.go:576` — `func DiscoverInversePredicates() map[string]string`
- `vocabulary/predicate_contract.go:40` — `type PredicateParts struct { Domain, Category, Property string }`
- `vocabulary/predicate_contract.go:99` — `func ParsePredicate(predicate string) (PredicateParts, error)` — enforces exactly 3 dot-segments, memoized (`memoizeParsedPredicate`, `:135`)
- `vocabulary/predicates.go:469` — `func IsValidPredicate(predicate string) bool` — wraps `ParsePredicate`

### Callers of `vocabulary.Register(...)` / `vocabulary.RegisterPredicate(...)` (201 call sites for `vocabulary.Register(` across the tree; representative registrants)
- `vocabulary/agentic/register.go` — ~90 calls (agentic domain vocabulary: lessons, scratchpad, intent, capability, delegation, accountability, execution, action, task, model, loop, web-observation, step, identity predicates) e.g. `vocabulary/agentic/register.go:75` (`LessonCategory`), `:435` (`LoopOutcome`)
- `vocabulary/governance/register.go:21-35` — injection-signal predicates (`InjectionSignal`, `InjectionTier`, `InjectionScore`, `InjectionTopMatchID`)
- `vocabulary/rulepacks/predicates.go:60` — loop-registers a slice of predicates
- `vocabulary/examples/robotics.go:21-34`, `vocabulary/examples/semantic.go:30-49` — framework EXAMPLE predicates (file doc: "these predicates are EXAMPLES for demonstration purposes", `vocabulary/predicates.go:19-22`)
- `examples/processors/document/vocabulary.go`, `examples/processors/iot_sensor/vocabulary.go`, `examples/processors/weather_station/vocabulary.go` — example-processor domain vocabularies (Dublin Core, sensor, weather predicates), several via `vocabulary.WithIRI(...)` (e.g. `examples/processors/document/vocabulary.go:63`)
- `agentic/agentrun/agentrun.go:99`, `agentic/research/predicates.go:179`, `cmd/e2e-semstreams/mission/state.go:67`, `processor/gated-dag/config.go:62` — loop-registration call sites in production/e2e code
- No `vocabulary.RegisterPredicate(` call sites found outside the function's own declaration (0 hits — see Searches)

### Readers of the registry (who exposes vocabulary to an LLM / tool schema / prompt / query)
- `processor/agentic-tools/executors/rules.go:119` — `actionPredicates := vocabulary.ListRegisteredPredicates()` and `:123` `vocabulary.IsRuleOpaque(predicate)` — used inside `ruleAuthoringSchema()` (`processor/agentic-tools/executors/rules.go:114-140`) to build the JSON-Schema `enum` for the `field` property of the `create_rule`/`update_rule` tool's `condition` schema (comment at `:112-114`: "constrains the concrete predicate-bearing fields in an agent-authored rule... a tool client cannot submit an arbitrary graph predicate through the JSON Schema") — this is the one confirmed path where the vocabulary registry's predicate list reaches an LLM-visible tool schema
- `processor/graph-index/component.go:679-680` — `c.aliasPredicates = vocabulary.DiscoverAliasPredicates()`, `c.namePredicates = vocabulary.DiscoverLabelPredicates()` (drives ALIAS_INDEX/NAME_INDEX construction, not agent-facing directly)
- `pkg/fusion/fusionvocab/signals.go:47` — `vocabulary.GetPredicateMetadata(predicate)` (ranking, not agent-facing)
- `graph/inference/hierarchy.go:422` — `vocabulary.GetInversePredicate(predicate)` (structural inference)
- `vocabulary/export/export.go:209` — `vocabulary.GetPredicateMetadata(predicate)` (drives JSON-LD/Turtle/N-Triples export, not the agent tool surface)
- `processor/rule/config_validation.go:214,298` — `vocabulary.IsRuleOpaque` (rule config-load-time validation)
- No file under `processor/agentic-tools/executors/graph_query.go`, `frameworkcapabilities/graphresearch/`, or `processor/agentic-model/` imports `vocabulary` (0 hits — see Searches). The direct graph-read tool surface (section 4) does not consult the vocabulary registry for descriptions, predicate lists, or schema shaping.

## 3. The derived indexes

Package: `processor/graph-index` (component that builds them); bucket-name constants in `graph/constants.go`.

- `graph/constants.go:9` — `BucketPredicateIndex = "PREDICATE_INDEX"`
- `graph/constants.go:10` — `BucketIncomingIndex = "INCOMING_INDEX"`
- `graph/constants.go:11` — `BucketOutgoingIndex = "OUTGOING_INDEX"`
- `graph/constants.go:14` — `BucketAliasIndex = "ALIAS_INDEX"`
- `graph/constants.go:19` — `BucketNameIndex = "NAME_INDEX"` (comment `:16-19`: complements ALIAS_INDEX; excludes display-name/`AliasTypeLabel` predicates; drives `graph.query.byName`, gh#376)
- `graph/constants.go:22` — `BucketEntitySuffixIndex = "ENTITY_SUFFIX_INDEX"` (partial-ID resolution; not named in the brief's four but adjacent)
- `processor/graph-index/doc.go:19-22` — architecture diagram: `ENTITY_STATES` (KV watch) → graph-index → `OUTGOING_INDEX`, `INCOMING_INDEX`, `ALIAS_INDEX`, `PREDICATE_INDEX`
- `processor/graph-index/doc.go:29-32` — what each makes reachable in one lookup:
  - `OUTGOING_INDEX`: entity ID → outgoing relationships (subject → predicate → object)
  - `INCOMING_INDEX`: entity ID → incoming relationships (object ← predicate ← subject)
  - `ALIAS_INDEX`: alias string → entity ID(s)
  - `PREDICATE_INDEX`: predicate → entity IDs
- `processor/graph-index/component.go:1609-1611` — the component computes, per entity-state change: the full distinct predicate set (PREDICATE_INDEX writes), `(namePredicate, name)` pairs (NAME_INDEX), `(aliasPredicate, alias)` pairs (ALIAS_INDEX)
- `graph/kvcatalog.go:155-159` — catalog registration of all five as `derived(...)` buckets owned by `graph-index`
- Consumers OTHER than graph-index itself: `processor/graph-clustering/component.go` reads `OUTGOING_INDEX`/`INCOMING_INDEX` for structural analysis (`processor/graph-clustering/doc.go:112`, `component.go:1303,1313,1471,1580,2008-2178`); `test/e2e/client/nats.go:508-511` (test infra)
- **Consumer gap found**: `git grep -n "BucketPredicateIndex\|BucketIncomingIndex\|BucketOutgoingIndex\|BucketAliasIndex\|BucketNameIndex"` restricted to `processor/agentic-tools/*`, `frameworkcapabilities/**`, `agentic/**` → 0 hits. The agent's graph-read tool surface (section 4) does not read any of these five derived indexes; `query_by_type`'s own comment (`processor/agentic-tools/executors/graph_query.go:531-532`) says a type-based query "requires a type index to be efficient... would query an index like ENTITY_TYPE_INDEX" — no such bucket exists among the five above.

## 4. The agent's graph-read tool surface

### `processor/agentic-tools/executors/graph_query.go` — `GraphQueryExecutor`, five tools, reads `ENTITY_STATES` only (not the derived indexes)

Registration: `processor/agentic-tools/executors/register_graph_query.go:34-45` — `registerGraphQuery(...)` is called unconditionally at boot; binds `ENTITY_STATES` lazily via `graph.CatalogReader` (comment `:19-33`).

| Tool name (`graph_query.go:line`) | Description string | Parameters | On found | On entity absent | On predicate/edge that doesn't exist vs exists-but-no-neighbors | Cap / truncation |
|---|---|---|---|---|---|---|
| `query_entity` (`:45`) | `"Query an entity from the knowledge graph by its ID. Returns the entity's properties, relationships, and metadata as JSON."` (`:46`) | `entity_id` string, required (`:48-57`) | Returns raw/pretty-printed JSON entity value + `revision` in `Metadata` (`:202-221`) | `ErrorKind: agentic.ToolErrorNotFound`, `Error: "entity not found: <id>"` (`:186-190`) when `err == ErrKeyNotFound` or NATS "key not found" | not distinguished at this tool — no relationship extraction happens here | none (single-entity fetch) |
| `query_entities` (`:60`) | `"Query multiple entities... in a single batch operation. More efficient than multiple query_entity calls."` (`:61`) | `entity_ids` array of string, required (`:63-73`) | `{"entities": {...}, "count": n}` (`:287-290`) | per-ID: silently collected into a `not_found` array (`:270-272,291-293`), NOT a per-call error — batch continues | n/a | no explicit array-size cap found in this tool (see Not found — cf. gh#839 "entity batch requests and responses have no 1 MiB bound") |
| `query_relationships` (`:76`) | `"Query relationships for an entity, optionally filtering by direction and type."` (`:77`) | `entity_id` required; `direction` enum `[outgoing,incoming,both]` default both (`:86-90`); `relationship_type` optional string filter (`:91-94`) | `{"relationships": [...], "count": n, "direction": ...}` (`:365-373`) | `ErrorKind: ToolErrorNotFound`, `"entity not found: <id>"` (`:339-343`) | **no distinction**: a `relationship_type` filter that matches zero relationships returns `count: 0` with no signal of whether the type is unknown-vocabulary or simply absent on this entity (`extractRelationships`, `:562-621`, filters by string equality against `entityData["relationships"]`/`entityData["triples"]`) | none |
| `query_neighbors` (`:100`) | `"Query neighboring entities within N hops of a given entity."` (`:101`) | `entity_id` required; `depth` integer, default 1, **min 1 / max 3** (`:110-115`); `filter_type` optional string (`:116-119`) | `{"neighbors": {...}, "count": n, "depth": n}` (`:462-467`) | entities that fail `kvGetter.Get` inside the BFS are silently `continue`d (`:428-431`) — not surfaced as errors, not distinguished from "no such entity" vs "transient fetch failure" | `filter_type` non-match silently skips the candidate (`:441-445`); no signal for "type exists nowhere in this neighborhood" vs "type unknown" | **fan-out cap: `depth` clamped to [1,3]**; no cap on frontier width or total neighbor count per hop found in this function |
| `query_by_type` (`:125`) | `"Query all entities of a specific type with optional limit."` (`:126`) | `entity_type` required string; `limit` integer, default 10, **min 1 / max 100** (`:135-140`) | **STUB**: always returns `{"entities": [], "count": 0, "note": "Type-based queries require entity type index. Use query_entity or query_entities with known IDs.", "suggested_ids": []}` regardless of input (`:533-540`) — comment at `:515`: `"queryByType queries entities by type (placeholder - requires index)"` | n/a (never queries KV) | n/a | `limit` clamped to [1,100] but unused (no real query executes) |

- `agentic.ToolEffectReadOnly` is declared on all five (`graph_query.go:47,62,78,102,127`)
- `graph_query.go:491-505` — `validateAuthoritativeEntity`/`decodeAuthoritativeEntityData` gate every read through `graph.UnmarshalEntityState`; a structurally-invalid stored entity produces `graphStateToolFailure` → `ErrorKind: ToolErrorInternal`, `"graph state reset required: %v"` (`:507-513`) — a third failure class distinct from not-found
- Object-vs-relationship extraction (`extractRelationships`, `:562-621`) reads BOTH a legacy `entityData["relationships"]` array shape AND a `entityData["triples"]` (subject/predicate/object) shape — two different on-disk relationship representations coexist in this reader

### `frameworkcapabilities/graphresearch/executor.go` — `ResearchGraphExecutor`, ADR-045 graph-research capability (async, not a direct traversal tool)
- `frameworkcapabilities/graphresearch/executor.go:23` — `const ResearchGraphToolName = "research_graph"`
- `:144` — `Effect: agentic.ToolEffectMutating` (worst-effect claim; ADR-089) even though "the answer it returns is a read"
- `:151-156` — Description: `"Spawn an asynchronous graph-research operation. The chain runs the existing graph classifier, routes to one of {synthesize_directly, retighten, walk_seeds, decompose}, executes multi-tier subqueries, assesses sufficiency, and synthesises an answer with provenance refs. Returns immediately and terminates this iteration; the SearchResult arrives on a subsequent iteration via the continuation rule. Call this for non-trivial questions where you don't already know the entities or predicates to query; for direct lookups by ID, use query_entity instead."`
- `:157-176` — Parameters: `topic` (string, required), `hints` (free-form string-map), `budget_tokens` (int, default `research.DefaultBudgetTokens`), `max_iterations` (int, default `research.DefaultMaxIterations`)
- Result is NOT returned synchronously — this tool is explicitly the escape hatch for "don't already know the entities or predicates," i.e. the entry-point problem the research paper names
- Registration gating: `frameworkcapabilities/graphresearch/register_tool.go:24` — "Unlike optional core tools, a selected graph-research capability fails boot" if misconfigured

### Rule-authoring tool exposing vocabulary (adjacent, not a graph-READ tool but the one confirmed vocabulary→schema path)
- `processor/agentic-tools/executors/rules.go:41-107` — `create_rule`/`update_rule`/`delete_rule`/`list_rules`/`get_rule` tool definitions; `ruleAuthoringSchema()` (`:114-140`) embeds `vocabulary.ListRegisteredPredicates()` filtered by `!vocabulary.IsRuleOpaque(...)` as a JSON-Schema `enum` on the condition `field` property (`:126-133`)

### Gateway admitted operations / MCP
- `openspec/specs/graph-query/spec.md:270-272` — "Graph-query SHALL own one internal inventory containing exactly these sixteen operations: `entity`, `entityByAlias`, `batch`, `relationships`, `pathSearch`, `hierarchyStats`, `prefix`, `spatial`, `temporal`, `semantic`, `similar`, `globalSearch`, `summary`, `searchGraph`, `byName`, and `localSearch`." — this is the GraphQL-shaped HTTP facade's admitted operation family, a SEPARATE surface from the agentic-tools `query_*` tools above
- `openspec/specs/graph-query/spec.md:255-263` — Requirement "Dereference reports unresolved object IDs without hiding source edges": "Missing objects MUST NOT be silently omitted, fabricated as stubs, treated as source poison, or interpreted as permission to delete the source edge" — the absence-vs-nonexistent-edge distinguishability guarantee exists at the GraphQL/gateway operation layer, not (per the table above) inside the agentic-tools `query_relationships`/`query_neighbors` tools
- `openspec/specs/graph-query/spec.md:158-162` — Requirement "Exact predicate lookup and namespace enumeration have distinct semantics": exact `domain.category.property` lookup vs. explicit `domain`/`domain.category` namespace enumeration are different operations; "MUST NOT be implemented by ambiguous string-prefix matching"
- `openspec/specs/agentic-tools/spec.md:267-291` — Requirement "Framework-owned shared builtins exclude the unowned graph-query wrappers": the framework SHALL NOT supply `search_graph`/`summarize_graph` as shared builtins; explicitly preserves "GraphQL `searchGraph` and `graphSummary`... direct `query_*` tools, and selected `research_graph`" — i.e. spec truth confirms the five `query_*` tools and `research_graph` are the intended surviving direct agent-facing graph-read surface
- `docs/concepts/11-query-access.md:46` — `"MCP graph access is unavailable."`
- `docs/concepts/11-query-access.md:84-88` — "### MCP: No Implemented Graph Contract" — "SemStreams does not currently implement an MCP graph endpoint and graph tool set. Do not route an AI agent to MCP, claim that MCP wraps GraphQL, or promise GraphRAG or PathRAG through MCP."
- `docs/concepts/11-query-access.md:124-131` — decision matrix step 1 front-door table: `"AI agent -> no canonical MCP graph surface yet"`
- Open PR **#211** — `feat(graph-gateway): implement read-only MCP server` (state: open, per `gh pr list`)

## 5. Prior rulings

### ADRs
- `docs/adr/045-graph-search-rule-chain.md` — the graph-research chain design; `:65-72` cites "MCP tool discovery context-blowing" as a named failure shape motivating the design; `:75-76` "multi-gateway query access (GraphQL, MCP, NATS Direct)"; `:249` names `execute_subqueries` fan-out via "existing GraphQL/MCP/NATS-Direct gateways"
- `docs/adr/075-framework-package-admission-and-composition.md:6,20` — reaffirms ADR-045's graph-research decision as a framework-admitted capability
- `docs/adr/102-entity-id-segment-semantics.md:54-55` — `org.platform.system.domain.type.instance`; `domain.type` = "delegated taxonomy" (entity typing ruling)
- `docs/adr/036-agent-private-observable-state.md:237-244` — "small models drown when the tool surface widens" (semteams smoke #7, beta.40/41/44) named as the reference case; persona-level tool opt-out is "the right lever," not framework enforcement
- `docs/adr/036-agent-private-observable-state.md:421-424` — persona doc quoted: "small models degrade with tool sprawl, and bash is the most heavily-trained-on tool surface"
- `docs/adr/028-orchestration-architecture.md:28,41-43,167` — small-model rulings behind the two-layer (rules trigger / components execute) orchestration design; structured output vs. small-model context-window pressure
- `docs/adr/034-structured-output-response-format.md` — small-model-deployment (Ollama/vLLM/sparky-hosted Qwen/DeepSeek) structured-output reliability decision
- `docs/adr/026-coordinator-agent-dynamic-flow-composition.md:46,50,52` — "requiring every agent... to submit via a schema-enforced terminal tool breaks on small models"; per-tool retry declared on `decide` specifically for "the small-model-shaped failure mode"
- `docs/adr/090-authoritative-current-state-and-materialized-views.md:48` — "No MCP graph-read contract is claimed until tools exist"
- `docs/adr/091-graph-mutation-authority-without-semantic-ownership.md:75,91` — rules out "a general embedded graph client, MCP read contract, or raw-KV application fallback" and any use of the mutation seam "as ...an MCP surface"
- No ADR found using the literal words "reification" or "property-bag" (0 hits — see Not found)

### Concept docs
- `docs/concepts/04-knowledge-graphs.md:92-99` — "SemStreams Approach": "RDF-like triples with RDF* (RDF-Star) extensions... No SPARQL or OWL reasoning... Predicate naming conventions (dotted notation) instead of formal ontologies... Practical balance: semantic clarity without specification overhead" — the closest thing to an explicit flat-vs-formal-ontology ruling
- `docs/concepts/09-graphrag-pattern.md`, `docs/concepts/10-pathrag-pattern.md` — GraphRAG/PathRAG pattern docs; `10-pathrag-pattern.md:135` reiterates "MCP graph contract" absence
- `docs/concepts/11-query-access.md` — query-access pattern doc (cited above in section 4)
- `docs/concepts/24-tool-result-hints-and-pagination.md:1-14` — origin story: "mid-tier models (qwen3.6-27b, llama-3.3-70b) retried the same 102KB-overflowing graph query 3+ times because the 'use more specific queries' advice was buried in a free-form error string the small model ignored" (beta.63) — directly names result-size/truncation signaling as a small-model-suitability lever
- `docs/basics/04-vocabulary.md` — predicate-design tutorial (cited in section 1)

### Specs (`openspec/specs/`)
- `openspec/specs/predicate-contract/spec.md` — 8 requirements: canonical 3-segment predicate syntax (`:45`), vocabulary declaration/namespace-authority separation (`:70`), unconditional canonical enforcement (`:102`), fixture classification (`:122`), **"An agent tool MUST NOT accept a caller-controlled predicate"** (`:160-193` — a tool must construct any predicate it writes; compliance verified against the tool REGISTRY, not a maintained list), mutation-lane trust boundary (`:194`), beta-cutover producer update (`:223`), authoritative-replay readiness gating (`:258`)
- `openspec/specs/graph-query/spec.md` — 22 requirements including the sixteen-operation inventory (`:268-291`), exact-vs-namespace predicate semantics (`:158`), dereference/absence guarantee (`:255`), embedded-adapter-only access (`:494`)
- `openspec/specs/agentic-tools/spec.md:267-291` — graph-query-wrapper exclusion requirement (cited in section 4)
- No `openspec/changes/` (active proposal) directory currently targets vocabulary/predicate/ontology/graph-shape (checked via `openspec list`, see Searches)

### GitHub issues (searches below use `--limit 100`; `gh issue list --search "vocabulary"` alone returned 76 results — a representative subset relevant to the named levers, not the full set)
- **#211** — `feat(graph-gateway): implement read-only MCP server` (open) — direct hit on lever 4 (MCP tool surface)
- **#1137** — `Epic — production GraphRAG: truthful retrieval, grounded synthesis, and standing proof` (open)
- **#1136** — `graph-query/docs: distinguish result attribution from audited evidence and reconcile GraphRAG claims` (open)
- **#176** — `graph-query: write the bulk-reads & pagination design doc (wire pagination shipped via #303/#307)` (open) — fan-out/pagination design gap
- **#839** — `graph-query: entity batch requests and responses have no 1 MiB bound` — directly bears on `query_entities`' unbounded batch (section 4 finding)
- **#306** — `Refine graph.query.prefix byte-budget / count-cap for routinely-large entities`
- **#430** — `graph-index: UpdatePredicateIndex is O(N²) at scale (monolithic JSON list per predicate + CAS read-modify-write)` — PREDICATE_INDEX scaling
- **#410** — `vocabulary/graph-index: label-predicate role silently lost on re-Register → breaks graph.query.byName + fusion readiness`
- **#396** — `ADR-062 increment 5: ontology ranking inputs — BFO/CCO subclass helper + predicate salience`
- **#212** — `vocabulary/cco: define minimal BFO/CCO alignment profile for agentic harness`
- **#798** — `vocabulary: derive ownership contracts from predicate registration — one declaration site`
- **#546** — `vocabulary: add error-returning TryRegister — the panicking Register is the only declaration form`
- **#1142** — `vocabulary/export: absolute-IRI objects render as string literals, so rdf:type triples export as invalid RDF`
- **#683** — `graph/message: define repeated structured values under fixed predicate grammar`
- **#217** — `graph: add schema-pattern index for claim and fact shapes`
- **#216** — `graph: add claim/evidence entities for LLM-derived assertions` — closest hit to "reified attribute/relation node" shape
- **#1095** — `entity-id: six positions with a lexical contract and no semantic one — no authority for segment values`
- **#785** — `graph: migrate all query-reply shape-knowers onto the canonical decoder #782 introduces` (only hit for `--search "graph_query"`; `--search "query_by_type"` returned 0)
- **#1057** — `agentic: four exposed classification fields document a closed vocabulary that nothing validates`
- No issue found with title/body matching "entity typing" or "traversal" as an exact phrase in a title (0 title hits under those literal searches — see Searches; `gh issue list --search "traversal"` returned unrelated fan-out/pagination issues by loose term matching, not exact-phrase hits)

### Pointer (not read in full, per brief)
- `/Users/coby/.claude/projects/-Users-coby-Code-c360-semstreams/memory/project_design_centers_index.md:16` — "ADR-055/056 predicate-group ownership over ENTITY_STATES" is the one vocabulary-adjacent line in that index

## 6. Sister examples (read-only; predicate strings as emitted, no repo mutated)

| Repo | File:line | Shape observed |
|---|---|---|
| semsource | `/Users/coby/Code/c360/semsource/handler/doc/entities.go:76-90` (`Entity.Triples`) | Flat property-bag: `source.DocType`, `source.DocFilePath`, `source.DocMimeType`, `source.DocFileHash`, `source.DcTitle`, `source.DocChunkCount`, `source.EntityRoleNavigational` — all literal-valued predicates on one subject |
| semsource | `/Users/coby/Code/c360/semsource/handler/doc/entities.go:174-197` (`PassageEntity.Triples`) | Mixed: literals (`source.DocType`, `source.DocChunkIndex`, `source.DocSection`) PLUS one explicit relationship — `source.CodeBelongs` with `Object: p.ParentID` (entity reference, line 183) and ObjectStore-ref predicates `source.DocBodyStore`/`source.DocBodyKey` (line 196-197) |
| semconnect | `/Users/coby/Code/c360/semconnect/message/oms/graphable.go:38-64` (`Observation.Triples`) | Flat/typed OGC-OMS predicates: `PredType`, `PredUsedProcedure`, `PredObservedProperty`, `PredHasFeatureOfInterest`, `PredResultTime`, `PredPhenomenonTime`, `PredHasSimpleResult` — property-bag shape, one subject, no explicit `Datatype: @id` tagging observed in the grepped lines |
| semdragon | `/Users/coby/Code/c360/semdragon/processor/partycoord/party.go:87-135` (`Party.Triples`) | Genuinely mixed shape: literal predicates (`party.identity.name`, `party.status.state`, `party.membership.count`, `party.lifecycle.formed-at`) alongside EXPLICIT typed relationship edges using `Datatype: semcompat.EntityReferenceDatatype` — `party.assignment.quest` → quest entity (line 100), `party.membership.lead` → agent entity (line 103) — and dynamically-named per-member predicates (`"party.member." + AgentID + ".role"`, line 119) |
| semboids | `/Users/coby/Code/c360/semboids/internal/boidgraph/payload.go:57-79` (`Entity.Triples`) | Data-driven flat predicates via a `mk(predicate, object)` closure: `flock.position.x/y`, `flock.velocity.x/y`, `flock.neighbor.count` (literals) plus one relationship predicate `flock.neighbor.of` emitted once per neighbor with an entity-ID-shaped object (line 77) — relationship typing relies on canonical-ID-shape detection, not an explicit `Datatype` tag |
| semteams | `/Users/coby/Code/c360/semteams/cmd/semteams/semsource/payload.go:98-100` (`EntityPayload.Triples`) | Pure passthrough: `return p.TripleData` — this payload does not itself define predicates; it carries pre-built `[]message.Triple` from upstream (semsource), so it is not a representative "domain vocabulary producer" example |
| semops, semdev, semmem | — | 0 `Triples() []` implementations found in each repo (see Searches) |

## Searches

- `git rev-parse HEAD && git branch --show-current` → `5b7c3db3a149cc62e90beb2a3f4d41622b65db53`, `main`
- `Read openspec/project.md` (Purpose + Product Boundary only)
- `ls message/` → 25 files
- `git grep -n "^type Triple" message/` → 2 (`Triple` struct, `TripleGenerator` interface)
- `git grep -n "^type Graphable" message/` → 0
- `git grep -rn "Graphable interface" -- '*.go'` → 9
- `sed -n '1,140p' message/triple.go`, `sed -n '140,200p' message/triple.go`, `sed -n '1,90p' graph/graphable.go` — read for pins
- `git grep -n "@context\|JSON-LD\|jsonld\|CURIE\|IRI\b" -- '*.go' | grep -v _test.go` → 60+ (capped head)
- `git grep -n "@id\b" -- '*.go' | grep -v _test.go` → 12
- `ls vocabulary/`, `ls vocabulary/export/` → 25 + 12 files
- `sed -n '1,60p' vocabulary/export/jsonld.go`
- `grep -n "^type \|^func \|^const " vocabulary/registry.go` → 30 symbols
- `grep -n "^type \|^func " vocabulary/predicate_contract.go` → 10 symbols
- `grep -n "^type \|^func " vocabulary/predicates.go` → 4 symbols
- `sed -n '325,470p' vocabulary/predicates.go`, `sed -n '1,120p' vocabulary/registry.go` — read for pins
- `git grep -n "vocabulary\.Register(" -- '*.go' | grep -v _test.go` → 201
- `git grep -n "vocabulary\.RegisterPredicate(" -- '*.go' | grep -v _test.go` → 0
- `sed -n '1,140p' processor/agentic-tools/executors/rules.go` — read for pins
- `git grep -n "vocabulary\.GetPredicateMetadata(\|vocabulary\.ListRegisteredPredicates(\|vocabulary\.DiscoverAliasPredicates(\|vocabulary\.DiscoverLabelPredicates(\|vocabulary\.DiscoverInversePredicates(\|vocabulary\.GetInversePredicate(\|vocabulary\.IsRuleOpaque(\|vocabulary\.IsSymmetricPredicate(\|vocabulary\.HasInverse(" -- '*.go' | grep -v _test.go` → 13
- `git grep -ln "vocabulary\." -- 'processor/agentic-tools/*.go' 'processor/agentic-model/*.go' 'processor/agentic-dispatch/*.go'` → 3 (2 are `_test.go`; only `executors/rules.go` non-test)
- `git grep -n "PREDICATE_INDEX\|NAME_INDEX\|ALIAS_INDEX\|INCOMING_INDEX\|OUTGOING_INDEX" -- '*.go' | grep -v _test.go` → 60+ (capped head)
- `sed -n '1,25p' graph/constants.go`, `sed -n '1,50p' processor/graph-index/doc.go` — read for pins
- `git grep -n "BucketPredicateIndex\|BucketIncomingIndex\|BucketOutgoingIndex\|BucketAliasIndex\|BucketNameIndex" -- '*.go' | grep -v _test.go | grep -v "processor/graph-index/\|processor/graph-clustering/\|graph/constants.go"` → 6
- `git grep -n "BucketPredicateIndex\|..." -- 'processor/agentic-tools/*' 'processor/agentic-tools/**/*' 'frameworkcapabilities/**/*' 'agentic/**/*'` → 0
- `git grep -n "graph.BucketOutgoingIndex\|graph.BucketIncomingIndex\|graph.BucketAliasIndex\|graph.BucketPredicateIndex\|graph.BucketNameIndex" -- '*.go' | grep -v _test.go | grep -vi "graph-index\|graph-clustering\|kvcatalog\|test/e2e"` → 0
- `which gopls && gopls version` → v0.20.0
- `gopls implementation graph/graphable.go:54:6` → 29 implementer sites
- `ls processor/agentic-tools/`, `ls processor/agentic-tools/executors/` → 46 + 41 files
- `git grep -n "Name:.*\"" -- 'processor/agentic-tools/executors/*.go' | grep -v _test.go` → 18 tool-name declarations
- `sed -n '1,60p' processor/agentic-tools/executors/register_graph_query.go`
- `git grep -n "NewGraphQueryExecutor\|RegisterGraphQuery" -- '*.go' | grep -v _test.go` → 2
- `Read processor/agentic-tools/executors/graph_query.go` (full file, 666 lines — the entire tool surface's contract)
- `git grep -n "graph-research\|GraphResearch\|graph_research" -- '*.go' 'docs/adr/*.md' | grep -v _test.go` → 13
- `ls docs/adr/ | grep -i "045\|075"` → 2 files
- `grep -n "^func \|Name:\s*\"\|Description:" frameworkcapabilities/graphresearch/executor.go` → 10
- `sed -n '1,50p' frameworkcapabilities/graphresearch/executor.go`, `sed -n '142,200p' frameworkcapabilities/graphresearch/executor.go`
- `git grep -n "MCP\b" -- '*.go' 'docs/**/*.md' 'openspec/**/*.md' | grep -v _test.go` → 30+ (capped head)
- `grep -n "MCP graph access is unavailable" -r .agents/skills/` → 0 (term lives in `docs/concepts/11-query-access.md`, not the skill file)
- `sed -n '1,140p' docs/concepts/11-query-access.md` (two reads covering 1-60, 60-140)
- `git grep -ln "predicate naming\|naming convention\|vocabulary contract\|ontology\|reificat\|flat.*graph\|property-bag\|property bag" -- 'docs/adr/*.md'` → 3
- `git grep -ln "small model\|cheap model\|small-model\|cheap-model\|tool surface\|tool count\|fan-out\|fan out" -- 'docs/adr/*.md' 'docs/concepts/*.md'` → 32
- `git grep -n "small model\|small-model\|cheap model\|cheap-model\|weak model\|weaker model" -- '*.md'` → 25 (capped head, excludes `.claude/worktrees`)
- `sed -n '1,40p' docs/concepts/24-tool-result-hints-and-pagination.md`
- `sed -n '230,250p' docs/adr/036-agent-private-observable-state.md`, `sed -n '415,430p' docs/adr/036-agent-private-observable-state.md`
- `git grep -n "reificat" -- 'docs/adr/*.md' 'docs/concepts/*.md'` → 0
- `grep -n "^#\|domain.type\|^Requirement" docs/adr/102-entity-id-segment-semantics.md` → 8
- `git grep -n "property-bag\|property bag\|flat graph\|typed edge\|typed vertex\|typed vertices" -- '*.md'` → 0
- `git grep -n "predicate naming\|naming convention" -- '*.md' 'vocabulary/*.go'` → 13
- `sed -n '1,40p' docs/basics/04-vocabulary.md`, `sed -n '1,40p' vocabulary/predicates.go`
- `sed -n '85,110p' docs/concepts/04-knowledge-graphs.md`, `sed -n '1,40p' docs/concepts/09-graphrag-pattern.md`
- `gh issue list --search "vocabulary" --state all --limit 100 --json number,title` → 76
- `gh issue list --search "ontology" --state all --limit 100 --json number,title` → 8
- `gh issue list --search "small model tool surface" --state all --limit 100 --json number,title` → 13
- `gh issue list --search "query_by_type" --state all --limit 100 --json number,title` → 0
- `gh issue list --search "graph_query" --state all --limit 100 --json number,title` → 1
- `gh issue list --search "entity typing" --state all --limit 100 --json number,title` → 0
- `gh issue list --search "entity type index" --state all --limit 100 --json number,title` → 39 (loose term matching, not exact phrase)
- `gh issue list --search "traversal" --state all --limit 100 --json number,title` → 9
- `gh pr list --search "graph_query OR vocabulary OR ontology" --state open --json number,title,headRefName --limit 50` → 9
- `ls openspec/specs/` → 51 capability directories
- `git grep -ln "vocabulary\|predicate" -- 'openspec/specs/*/spec.md'` → 24
- `grep -n "^### Requirement" openspec/specs/predicate-contract/spec.md` → 8
- `grep -n "^### Requirement" openspec/specs/agentic-tools/spec.md` → 19
- `grep -n "^### Requirement" openspec/specs/graph-query/spec.md` → 22
- `sed -n '267,300p' openspec/specs/agentic-tools/spec.md`
- `sed -n '160,225p' openspec/specs/predicate-contract/spec.md`
- `sed -n '158,180p' openspec/specs/graph-query/spec.md`, `sed -n '255,270p' openspec/specs/graph-query/spec.md`, `sed -n '268,300p' openspec/specs/graph-query/spec.md`
- `sed -n '60,80p' docs/adr/045-graph-search-rule-chain.md`
- `ls /Users/coby/Code/c360/` → 22 sibling directories
- `for repo in semsource semsage semconnect semops semdev semteams semdragon semboids semmem; do grep -rn "func.*Triples() \[\]" $repo --include='*.go' | wc -l; done` → semsource 16, semsage 0, semconnect 3, semops 0, semdev 0, semteams 4, semdragon 30, semboids 2, semmem 0
- `grep -rln "func.*Triples() \[\]" <repo> --include='*.go'` (per repo, non-worktree files identified) — semsource 5 files, semconnect 2, semteams 4 (3 are `.claude/worktrees` duplicates), semdragon 5+ (capped head), semboids 2
- `grep -n "func.*Triples() \[\]\|Predicate:" semsource/handler/doc/entities.go` → 17
- `grep -n "func.*Triples() \[\]\|Predicate:" semconnect/message/oms/graphable.go` → 7
- `grep -n "func.*Triples() \[\]\|Predicate:" semdragon/processor/partycoord/party.go` → 10
- `grep -n "func.*Triples() \[\]\|Predicate:" semboids/internal/boidgraph/payload.go` → 2
- `grep -n "func.*Triples() \[\]\|Predicate:" semteams/cmd/semteams/semsource/payload.go` → 1
- `sed -n '40,75p' semboids/internal/boidgraph/payload.go`, `sed -n '75,95p' semboids/internal/boidgraph/payload.go`, `sed -n '90,130p' semteams/cmd/semteams/semsource/payload.go`
- `grep -n "vocabulary\|ontology\|predicate\|graph shape\|reificat" /Users/coby/.claude/projects/-Users-coby-Code-c360-semstreams/memory/project_design_centers_index.md` → 1
- `sed -n '1,30p' graph/entity_predicate_contract.go`, `sed -n '99,135p' vocabulary/predicate_contract.go`
- `gopls workspace_symbol -matcher=fuzzy Triple` → 20 symbols
- `gopls workspace_symbol -matcher=fuzzy Graphable` → 10 symbols

## Not found

- **Reification / reified attribute-relation nodes**: no ADR, concept doc, or spec uses the literal term "reification"; no in-repo pattern was found that models a Triple's own metadata (Source/Confidence/Timestamp) as a first-class graph NODE rather than as sidecar Triple fields (`git grep -n "reificat"` → 0)
- **"property-bag" / "flat graph" / "typed edge" / "typed vertex" as repo vocabulary**: 0 hits for the exact phrases; the closest ruling is `docs/concepts/04-knowledge-graphs.md:92-99`'s "Predicate naming conventions... instead of formal ontologies"
- **Cardinality on `PredicateMetadata`**: no field for cardinality (one-vs-many object per predicate) was found on `vocabulary/predicates.go:351` `PredicateMetadata` struct — only `Range` (free-text) and `DataType` (Go type name)
- **`RegisterVocabulary` as a literal function name**: not found; the actual entry points are `vocabulary.Register` and `vocabulary.RegisterPredicate` (see section 2)
- **A `Domain`/`Range`-typed edge-vs-literal declaration on `PredicateMetadata`**: not found; whether an object is an entity reference is determined dynamically per-Triple via `Datatype`/`IsRelationship()` (section 1), not declared once per predicate in the registry
- **Vocabulary registry consulted inside `processor/agentic-tools/executors/graph_query.go` or `frameworkcapabilities/graphresearch/`**: 0 imports of `vocabulary` in either file/package (see Searches) — tool descriptions and parameter schemas for the direct graph-read tools are hand-written prose, not registry-derived
- **A type index (`ENTITY_TYPE_INDEX` or equivalent) backing `query_by_type`**: does not exist among the five derived-index buckets (section 3); the tool is a stub (section 4)
- **An exact-title GH issue on "entity typing" or "traversal" as named concepts**: 0 title matches under those literal search strings (loose `gh search` term-matching returned unrelated issues instead — recorded above, not counted as a hit)
- **MCP graph tool set**: explicitly ruled ABSENT (`docs/concepts/11-query-access.md:46,84-88`; `docs/adr/090`; `docs/adr/091`), tracked as future work in open PR #211
- **`vocabulary.RegisterPredicate` callers**: 0 call sites outside its own declaration — all production registration goes through `vocabulary.Register` (functional-options form)

</details>


# Documentation audit search and coverage record

base: ea22e6a4e75d12bf7f6050c6d8121de1d089b191

Collector records below preserve their search expressions, reported scopes, counts and limitations.
Shorthand scope descriptions are the collectors' original records; they are not shell-ready commands.
The full-corpus reproducible check is in [methods.md](methods.md). Searches enumerate candidates;
zero matches in a guessed filename or incomplete structural query do not establish absence.

## Onboarding collector

### Searches

All expressions below used `git grep -n -E` unless marked `-F`; counts include comments. Numbered searches were repeated to recover exact counts after truncated displays.
Q1 `^#|\]\(` → README and attempted basics/README: **55**.
Q2 `^#|init\(|Register|NewBase|BaseMessage|go run|semstreams|workflow|publish_agent|subject|template` → attempted 01-hello-world/02-first-processor, basics07/08: **133**.
Q3 `^#|init\(|Register|NewBase|BaseMessage|go run|task |payload|Payload|func |Stream|Subject` → first-processor, IoT/component/message/ObjectStore READMEs: **501**.
Q4 `dev:start|hello-world|cmd/semstreams|cmd/e2e-semstreams|iot-sensor|iot_sensor` → Taskfile, taskfiles, hello-world config, both mains, componentregistry, payloadbuiltins: **34**.
Q5 `subject.*required|Subject.*==|publish_agent|substituteTemplate|\$entity|\{\{` → rule actions and attempted action_executor/action_publish_agent files: **66**.
Q6 `Register|iot|examples|Wire|Assert|Analyze` → componentregistry register/core, payloadbuiltins, both mains: **129**.
Q7 `unknown|not found|not registered|factory` → component registry, component manager: **40**.
Q8 `RegisterPayloads|func Register|NewBaseMessage|func init` → IoT payload/payload_registry, base_message, payload registry: **13**.
Q9 `^#|Register|registry|go run|go build|config|Graphable` → IoT README: **23**.
Q10 `type PortDefinition|type PortConfig|type Dependencies|type InterfaceContract|json:"(type|config|kind|subject)|StreamName|Subjects` → component ports/port/port_jetstream/port_codec: **21**.
Q11 `func .*Put|func .*Get|func .*List|func .*Delete|version|history|immutable` → ObjectStore store/config and attempted storage/store.go: **11**.
Q12 `registry|WithRegistry|Err|Deserialize` → message base_message/decoder: **27**.
Q13 `^#|RegisterPayloads|payloadregistry.New|message.NewBaseMessage|no.*init|init\(|explicit|WithRegistry` → payload-registry guide: **55**.
Q14 `^go |^module ` → go.mod: **2**.
Q15 `^#|init\(|payloadregistry.Register|NewBaseMessage\(|Graphable|context.Background|processor/reactive|go 1\.|1.25|events.graph|graph.mutation|ENTITY_STATES|semstreams/client|RegisterAll|must|automatically` → basics00/01/02/03/04/06/09: **255**.
Q16 `Unknown|unknown|DisallowUnknown|decoder|legacy|top-level` → port_codec/ports: **7**.
Q17 `Arguments|json.Unmarshal|RegisterTool\(|RegisterExecutor\(` → agentic/tools, executor, graph_query executor: **21**.
Q18 `Ports:|DeclarePorts|NewComponent|ResolveConfig` → IoT register/component: **20**.
Q19 `-F 'SubstituteVariables(action.Prompt'` → processor/rule: **0**.
Q20 `register.*[Pp]ayload|RegisterPayloads|PayloadRegistry|NewDecoder` → IoT README, first-processor, component/message README: **4**.
Q21 `NewBaseMessage|NewGenericJSON|payloadregistry.Register|github.com/c360/semstreams|init\(` → message README: **25**.
Q22 `workflow|reactive|Lifecycle|MaxIterations|two layers|two.*layers` → orchestration catalog: **29**.
Q23 `SubstituteVariables|Prompt` → actions.go: **35**; initial display used `tail -14`.
Q24 `unknown field|reject|decodeStrictJSON` → ports.go: **1**.
Q25 `^#|New|Build|Initialize|Discover|Register|ports|validate|graph` → flowgraph README: **72**.
Q26 `PortDefinition\{|RegisterWithConfig|RegisterPayloads|NewDecoder|NewBaseMessage|type Component|func .*Start|func .*Stop` → component/message/storage/ObjectStore doc.go and payloadregistry/registry.go: **7**.
Q27 `type .*Store|PutBytes|func .*Delete|History|version|history` → attempted natsclient/objectstore.go: **0**.
Q28 `NewGenericJSON|func Register\(` → generic_json.go, payloadregistry/registry.go: **2**.
Q29 `-F '{{.EntityID}}'` → basics, processor/rule: **1**.
Q30 `variablePattern|regexp.MustCompile|\$entity.id|entity\.id` → execution_context.go: **7**.
Q31 `^#|Register|Decoder|WithRegistry|Payload` → component/message/storage/ObjectStore doc.go: **44**.
Q32 `json.Unmarshal|NewDecoder|UnmarshalJSON` → message/doc.go: **5**.
Q33 `History|history|version|PutBytes|Delete` → attempted natsclient/object_store.go/objectstore_managed.go: **0**.
Q34 `nats_request_port|request_port|requests` → graph-ingest config/ports and basics06: **1**.
`gopls symbols`: payloadregistry/registry.go **32**, attempted message/base.go **0**, base_message.go **25**, port_jetstream.go **30**, agentic/tools.go **82**, executor.go **12**, component/registry.go **106**.
`git ls-files`: owned README/basic/package-doc inventories and Go counterpart filename inventories; `processor/reactive/*`, `pkg/componentregistry/*`, `cmd/e2e-semstreams/workflows.go` each **0**.
Python read-only counts: 19 Markdown files plus four package doc.go files; line/fence counts reported above.

Meaningful targeted reads: README first-run; basics00 prerequisite claims; basics03 implementation; basics04 registration; basics05 payload/wrapper/registration/tests/checklist; basics06 ports; basics07 action/custom-tool sections; basics08 label/import/tutorial claims; basics09 composition/retrieval/evidence/adopter exercise; IoT/component/message/ObjectStore READMEs; flowgraph construction claims; payload-registry and orchestration counterparts.
Heading/literal inventory only: basics01/02; remaining package doc.go sections; advanced reactive guide measured only.
NOT RUN: snippet compilation or execution, complete tutorial lifecycle validation, first-run acceptance test, external storage-history verification, full basics01/02 bodies, generic link checking, live issues/PRs (root owns mapping).

## Graph and agentic collector

### Searches

All commands ran in the named worktree. `G` below expands exactly to `git grep -n`; quoted globs are Git pathspecs.

- Catalog: `git ls-files -- 'docs/concepts/*.md' 'docs/advanced/*.md' 'graph/**/README.md' graph/README.md 'processor/**/README.md'` → 62; Python `splitlines()` supplied page/group counts.
- S1: `G -i -E 'processor/reactive|workflow engine|workflow processor|WatchAll|parallel:|"parallel"' -- docs/concepts docs/advanced 'graph/**/README.md' graph/README.md 'processor/**/README.md'` → 28.
- S2: `G -i -E 'exact token|TokenCount|ContextSource|Sources|provenance|trajectory|capture' -- docs/concepts/22-context-construction.md docs/concepts/32-agent-memory.md processor/agentic-loop/README.md docs/advanced/08-agentic-components.md` → 36.
- S3: `G -i -E 'GraphClient|NewClient|query.Client|graphClient|Execute|GraphQL|MCP|gRPC|StreamKit|tier|Tier' -- docs/concepts/11-query-access.md graph/README.md docs/advanced/07-graph-components.md processor/graph-query/README.md` → 38.
- S4: `G -E 'Context\.Sources|Context\.Entities|Context\.TokenCount|Context\.ConstructedAt|ConstructedContext|EstimateTokens|TokensPerChar|charsPerToken|tiktoken|TokenCount:' -- agentic pkg/context pkg/types processor/agentic-loop ':!*_test.go'` → 47.
- S5: `G -E 'parallel|WatchAll|watch_all|guard|bootstrap' -- processor/rule/config.go processor/rule/component.go processor/rule/types.go processor/graph-index-temporal 'openspec/specs/rule*/spec.md' openspec/specs/graph-index-temporal/spec.md ':!*_test.go'` → 26.
- S6: `G -E 'AGENT_TRAJECTORIES|no TTL|no automatic|evidence_capture|attribution|coverage: observed|ConstructedContext|context source' -- openspec/specs/agentic-loop/spec.md openspec/specs/stream-provisioning/spec.md processor/agentic-loop/trajectory_evidence.go processor/agentic-loop/trajectory_recorder.go` → 10.
- S7: `G -i -E 'Tier [012]|[0-9]+ ?MB|[0-9]+ ?GB|memory|resource|download|neural|BM25|search_graph|QueryManager|QueryClient' -- docs/concepts/00-real-time-inference.md docs/concepts/05-embeddings.md docs/concepts/09-graphrag-pattern.md docs/advanced/01-clustering.md docs/advanced/02-llm-enhancement.md docs/advanced/03-performance.md graph/embedding/README.md graph/llm/README.md processor/graph-embedding/README.md` → 81.
- S8: `G -E '^#{1,3} |Historical|historical|retired|Current|current' -- docs/advanced/06-rules-engine.md docs/advanced/09-workflow-configuration.md docs/advanced/10-reactive-workflows.md docs/concepts/14-orchestration-layers.md docs/concepts/23-parallel-agents.md docs/advanced/08-agentic-components.md docs/concepts/13-agentic-systems.md processor/agentic-loop/README.md` → 267.
- S9: `G -E 'hand-written|schema executor|general.*GraphQL|graph/query.Client|parallel.*(support|remov)|not.*parallel|ActionType.*Parallel|workflow.*retired|ForEach' -- openspec/specs processor/rule/actions.go processor/rule/action_executor.go processor/rule/types.go gateway/graph-gateway` → 33.
- S10: `G -i -E 'fallback|embedder_type|NewBM25Embedder|dimens' -- processor/graph-embedding graph/embedding ':!*_test.go'` → 117.
- S11: `G -E 'NewPredicateGraphProvider|QueryManager|querymanager|GlobalSearch' -- graph/clustering graph/llm processor/graph-query ':!*_test.go'` → 125.
- S12: `G -i -E 'WatchAll|watch.*ENTITY_STATES|ENTITY_STATES.*watch|prefetch|predicate|pattern' -- docs/concepts/02-kv-twofer.md docs/concepts/03-streams-vs-kv-watches.md processor/rule/README.md docs/advanced/06-rules-engine.md` → 26.
- S13: `G -E 'Context\.(Sources|ConstructedAt)|NewPredicateGraphProvider|NewQueryManagerGraphProvider' -- '*.go' ':!*_test.go'` → 0.
- Reads: explorer contract; project Purpose/Product Boundary; bounded excerpts surrounding the pins above.
- NOT RUN: `gopls` structural enumeration, following the previously observed prohibited-cache-write failure; full metadata/alias propagation; runtime fallback trace; example compilation; benchmark/resource validation; every assertion on every catalogued page; issue/PR coverage.

### Checked pages and limits

Semantic counterpart checks: concepts **05, 11, 13, 14, 22, 23, 32**; advanced **01, 06, 07, 08, 09, 10**; graph **clustering, embedding, llm** READMEs; processor **agentic-loop, graph-embedding, graph-index-temporal, graph-query, rule** READMEs.

Additional targeted searches only: concepts **00, 02, 03, 09**; advanced **02, 03**; graph root README. Remaining catalogued pages received scope-wide literal scans/counting only. No page received a full semantic audit. Historical labels, constructor spellings, token estimation, trajectory storage, and query/WatchAll counterparts were checked separately; none is a whole-page verdict.

Root separately verified #765's actual target, `processor/rule/docs/entity-watching.md`, and the discrepancy between its authoritative WatchAll guard claim and the current implementation/spec.

## Operations and contributor collector

### Meaningfully read / not read

Read fully: architect and explorer contracts; `openspec/project.md`; `.agents/README.md`; `.agents/protocol.md`; contributor 06 and 07; deployment README and production-small JSON; ten role adapters; nine shared-skill adapters.

Read relevant sections: operations 05 and 23; lifecycle migration restore and restart-safe pages; docs index; contributor 01, 03, 04, 05; OpenSpec archive command/skill; `openspec/config.yaml`; current registry/lifecycle/schema parser; composition spec; CI.

Mechanical census/comparison or targeted matches only: remaining operations pages, remaining canonical skills/contracts, root instruction prose outside inspected matches, other OpenSpec command/skill bodies. Contributor 02 was not meaningfully read. Package-document census found 92 Markdown files outside docs/agent/OpenSpec trees; their broader semantics were not audited in this bounded pass.

### Searches

Scopes below are repository-relative. `O=docs/operations`, `C=docs/contributing`, `A=.agents`, `L=.claude`, `X=.codex`, `R=AGENTS.md CLAUDE.md`; counts are matching lines, not occurrences.

- R0 `rg --files O C A L X configs/deployments`: **163 files**.
- R1 `rg --files component | rg 'discoverable|schema|lifecycle'`: **8 files**.
- G1/G2 `gopls workspace_symbol -matcher=fuzzy RegisterFactory` / `WatchModelRegistry`: **0 output each**; not accepted as absence evidence.
- S1 `@latest|processor/reactive|ErrAlreadyStarted|ErrNotStarted|ErrAlreadyStopped|context\.Background|context\.TODO|context\.Context|Start\(|Stop\(`, O C A L X R: **81**.
- S2 `Taskfile|task [a-zA-Z_:]+|go test|go run`, C: **103**.
- S3 `canonical|source of truth|source-of-truth|authority|mirrors|adapter`, A L X R: **123**; first 115 printed.
- S4 `live|hot.reload|automatic|refresh|watch|atomic.Pointer`, operations 05: **15**.
- S5 `migration-restore-go|migration-restart-safe|migration-post-beta160|PENDING|CURRENT|Supersed|supersed`, docs index and selected lifecycle migration pages: **5**.
- S6 `func|Configurable|schema|get.*Schema|Schema\(|Initialize|Process`, contributor 03: **169**.
- S7 `1\.2[0-9]|test:integration|go test|check:push|integration-runner|revive|golangci|lint`, contributor 05, R, protocol, preflight, CI, `taskfiles/test.yml`: **60**.
- S8 `resources|max_memory_mb|max_cpu_percent|docker-compose.production|docs/deployment|2GB|2 cores`, deployment configs: **17**.
- S9 `(^#|[Ll]egacy|[Hh]istorical|[Rr]etired|[Pp]ending|[Ll]anded|[Ss]tatus|[Ss]uperseded)`, operations 21/22/24/25/27: **143**; first 100 printed.
- S10 `max_memory_mb|max_cpu_percent`, repository-wide tracked text: **6**.
- S11 `@latest|go1\.2[0-9]|1\.2[0-9]|register.*init|RegisterFactory|GetLifecycleState|LifecycleState|ErrStopped|ErrRunning`, R A L X C O: **19**.
- S12 `generate-types|core:debug|^  debug:|^  e2e:|^  integration:|^  check:push:`, Taskfile/taskfiles: **27**.
- S13 `Go [0-9]|^go |^toolchain|GO_VERSION`, go.mod, Dockerfile, CI: **10**.
- S14 `generated|Generated|generator|OpenSpec|version|Copyright`, OpenSpec commands, apply/archive skills, agent README, OpenSpec config/README: **41**.
- S15 `case "desc"|case "description"|unknown directive|ExtractSchema|RegisterFactory`, component schema_tags/schema/registry: **11**.
- S16 `AGENTS.md`, `.gitignore`, A, scripts, CLAUDE.md: **4**.
- S17 `21-adr044-framework-primitives-reference|22-adr045-phase1-plan|24-predicate-breaking-rename-ledger|migration-restart-safe-nats-client`, docs index, O, C: **3**.
- S18 `generatedBy|author: openspec`, `.claude/skills`: **20**.
- S19 `Stop\(5\*time.Second\)|Go 1.25`, operations 23 and R: **4**.
- Additional non-search reads: `git ls-files` census, `git ls-tree` baseline file modes, Python line counts/exact-block comparison/path existence, numbered file excerpts.
- Two unsuccessful direct reads: nonexistent `input/udp/register.go` and mistyped `docs/contributing/06-openspec-workflow.md`; correct contributor 06 was subsequently read.

## Root reconciliation

The root session independently ran the corpus helper preserved in methods.md and verified selected current
missing targets with tracked filename inventories. It counted `doc.go` files with `git ls-tree`, and used Python
`splitlines()` and `difflib.SequenceMatcher` for page sizes and exact shared instruction blocks. Matching blocks
include blank lines; repeated topics and identical paragraphs do not establish dispensable content.

Additional targeted searches and reads:

- `git grep -n -E 'WatchAll|guard|bootstrap|barrier' -- processor/rule openspec/specs/rule-entity-watching/spec.md`
  located #765's nested guide; the implementation and current spec disagree about a dedicated guard.
- `git grep -n -E 'logs\.|subject :=|RequireDeclaredPredicate|FREE-FORM' -- pkg/logging processor/gated-dag`
  located the logging subject and predicate admission counterparts.
- `git grep -n -E 'max_memory_mb|max_cpu_percent'` returned six baseline lines in three deployment JSON files.
  This literal scan found declarations, not a production enforcement reader; no runtime enforcement test ran.
- `rg --files docs specs` checked missing schema guides and the actual root OpenAPI path.
- `gh issue list --state open --limit 300 --json number,title,labels,milestone,url` captured 258 summaries.
  Full issue bodies were read for #457, #486, #668, #765, #828, #1002, #1034, #1133, #1136, #1163, #1218 and #1260.
- `gh pr list` and selected PR file lists checked existing claims. Separate active PRs were recorded as proposals,
  not shipped behavior. Issue and PR state is a dated snapshot, not a new task ledger.

Meaningful root reads covered the logging and gated-DAG doc/code pairs, rule entity-watching guide/code/spec,
selected broken-link source/target locations, roadmap authority, and the twelve issue bodies. The base was checked
against origin/main after #1300 merged. No runtime, model, Docker, offline, federation, external URL availability,
or sister-repository write was part of this audit.

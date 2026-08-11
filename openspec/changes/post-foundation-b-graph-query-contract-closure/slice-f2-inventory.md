# F2 inventory-only handoff

**Baseline:** `aa9d3704628e844c1f10b2c3627365c52e2b4685`
**Scope:** Existing `search_graph` / `summarize_graph` surfaces only. Read-only inventory; no target state, options,
recommendation, or implementation tasks.

## Problem statement

F2 concerns two agent-facing graph-query wrappers already present in the shared tool registry. The measured gap is not
missing graph-query functionality: the wrappers duplicate admitted graph-query operations through undeclared agentic
access seams.

## Surface inventory

### Claimed gap

Both wrappers exist and are callable:

- `SearchGraphExecutor`, `SearchGraphOption`, `WithSearchGraphTimeout`, and `NewSearchGraphExecutor`:
  `processor/agentic-tools/executors/search_graph.go:39-62`.
- `SummarizeGraphExecutor`, `SummarizeGraphOption`, `WithSummarizeGraphTimeout`, `NewSummarizeGraphExecutor`, and
  shared exported `NATSQuerier`: `processor/agentic-tools/executors/summarize_graph.go:72-114`.
- Definitions advertise `search_graph` and `summarize_graph` as `read_only`; execution sends request/reply directly to
  `graph.query.searchGraph` and `graph.query.summary`: `search_graph.go:13-28,64-151`;
  `summarize_graph.go:41-89,116-190`.
- Private support includes argument/response DTOs, formatting, timeout constants, `classifyRequestError`, and
  predicate sorting.
- Registration functions are in `register_search_graph.go:11-25` and `register_summarize_graph.go:11-25`.
- `RegisterBuiltins` registers both whenever `NATSClient != nil`: `register.go:184-207`.
- Both production binaries call `RegisterBuiltins` with a NATS client and no `SkipBuiltins`:
  `cmd/semstreams/main.go:170-201`; `cmd/e2e-semstreams/main.go:165-188`.

The wrappers are therefore shared builtins in both production compositions.

### Current spellings

| Meaning | Current spellings/evidence |
|---|---|
| Agent tools | `search_graph`, `summarize_graph` |
| Exported Go surface | `SearchGraph*`, `SummarizeGraph*`, `NATSQuerier` |
| Query operations | `searchGraph`, `summary` |
| NATS subjects | `graph.query.searchGraph`, `graph.query.summary` |
| GraphQL fields | `searchGraph`, `graphSummary`: `gateway/graph-gateway/component.go:983-988,1685-1688` |
| Skip/group keys | `search_graph`, `summarize_graph`: `register.go:101-123` |
| Category catalog | `graph_search`, `graph_summary`, which do not match either registered name: `processor/agentic-tools/categories.go:34-46` |
| Adjacent tool family | `graph_query` group exposes `query_entity`, `query_entities`, `query_relationships`, `query_neighbors`, `query_by_type`: `register_graph_query.go:14-44`; `graph_query.go:45-159` |
| Research capability | `research_graph`, an optional mutating orchestration tool: `frameworkcapabilities/graphresearch/executor.go:20-24,63-78,162-180` |
| Research provenance | `search_graph` remains a candidate-source spelling independent of the wrapper, e.g. `processor/research-graph-classify/adapters.go:134-169`; generated description at `schemas/research-graph-classify.v1.json:16` |

The category catalog contains `graph_search` and `graph_summary`, not the registered `search_graph` and
`summarize_graph` spellings (`processor/agentic-tools/categories.go:34-46`). Consequently,
`GetToolCategory("search_graph")` and `GetToolCategory("summarize_graph")` miss the map and return the exported default
`CategoryCore` (`categories.go:71-79`). `ReadOnlyCategories` includes both `CategoryCore` and `CategoryKnowledge`
(`categories.go:90-97`), so the mismatch does not make either name non-read-only under that helper.

Production category filtering is unwired at this baseline: the only non-test references are the category
implementation and the `EnableCategories` configuration field (`processor/agentic-tools/config.go:31`). External
consumers of exported `GetToolCategory`, `RegisterToolCategory`, or `ReadOnlyCategories` cannot be measured from this
repository.

Thus other agent graph access exists: five direct `query_*` tools and the optional `research_graph` orchestration tool.
No other `summarize_graph` implementation was found.

### Registries, discovery, dispatch, configuration, model path

- `BuiltinGroupKeys` is exported as a stable external iteration/validation surface; its golden test requires both keys:
  `register.go:92-123`; `register_test.go:434-506`.
- Unknown skip keys fail before registration; nil/empty skips register everything: `register.go:225-247`.
- Shared registry aggregation re-invokes each executor's `ListTools`, normalizes effect, and serves definitions to
  dispatch, loop discovery, rules, and component discovery: `processor/agentic-tools/executor.go:135-197`.
- Component discovery merges shared and local definitions; local names override shared discovery entries:
  `component.go:803-859`.
- Execution tries the local registry first and falls back to shared only on typed not-found: `component.go:675-687`.
- `AllowedTools` nil/empty permits every registered tool: `config.go:13-21,95-98,148-156`;
  `component.go:552-558`.
- All nine shipped `allowed_tools` lists omit both wrapper names. This does not establish non-reachability because an
  external or default component configuration may omit or empty the list.
- `default_tools` names are resolved through the shared registry; nil leaves task tools unset, and agentic-loop then
  performs global discovery: `agentic-dispatch/component.go:23-75`; `agentic-loop/handlers.go:908-919`.
- The discovered definitions enter `AgentRequest.Tools`; model adapters translate name, description, parameters, and
  strictness into provider tools: `agentic-model/client.go:393-410`; `client_wire.go:40-52`;
  `translate_responses.go:63-69`.
- Schemas enumerate no tool-name vocabulary. `allowed_tools` and `default_tools` accept arbitrary strings; the former
  documents nil/empty as allow-all: `schemas/agentic-tools.v1.json:8-15`;
  `schemas/agentic-dispatch.v1.json:25-31`.

### Tests and documentation

Dedicated wrapper suites cover definitions, formatting, arguments, response compatibility, errors, and wrong-name
dispatch: `search_graph_test.go:24-294`; `summarize_graph_test.go:82-274`. Registration tests pin both group keys and
call them "direct graph groups": `register_test.go:434-506`. F1's preservation guard explicitly recognizes their
exported symbols: `graph/query/slice_f1_surface_contract_test.go:95-98`.

Current documentation advertises both in tool discovery and effect tables:
`docs/operations/adopter-tool-effect-metadata.md:62-80,111-126`. `docs/concepts/32-agent-memory.md:11-15` says pull
tools were removed, while lines `35-49` say `search_graph` still exists. ADR-045 and frontier mapping describe empirical
non-use despite allowlisting: `docs/adr/045-graph-search-rule-chain.md:85-94`;
`docs/concepts/27-frontier-harness-mapping.md:250-260`.

Adjacent claims:

- Active F2 ruling/change: `approval.md:37-42,108-113`; `proposal.md:37`; agentic-tools delta
  `specs/agentic-tools/spec.md:3-29`.
- Suspended `semantic-tier-split` cites gh#823 and records zero scenario references to `search_graph`:
  `proposal.md:90-94`; `tasks.md:82-87`.
- Archived tool-effect change classifies both read-only:
  `openspec/changes/archive/2026-08-01-tool-effect-metadata/tasks.md:35-37`.
- #819/#823 concern `searchGraph` representation/strategy; #211 is deferred MCP access: active inventory
  `inventory.md:470,492`.
- Query-pattern classification: AI-agent MCP graph access is not implemented; remote access is admitted HTTP
  operations and embedded access requires an operation-specific typed adapter:
  `.agents/skills/query-pattern/SKILL.md:15-43,64-79`.
- Current graph-query inventory explicitly labels only these agentic consumers "unadmitted":
  `processor/graph-query/query.go:62-63`.

## Consumer at birth

Present consumers are the two production shared registries, global tool discovery, model-provider tool declarations in
permissive deployments, and direct dispatch by name. There are no in-repo production constructors beyond builtin
registration. External Go importers of the exported executor/option/querier surface are possible but not measurable
from this repository.

The underlying operations have independent consumers and implementations:

- `summary` is admitted for graph-gateway and returns `graph.QueryResponse[graph.SummaryData]`: `query.go:62`;
  `summary.go:58-124`.
- `searchGraph` is admitted for graph-gateway, research-classify, and research-execute and composes global search plus
  semantic fallback: `query.go:63`; `searchgraph.go:29-120`.
- GraphQL routing, all sixteen graph-query responders, research adapters, fusion, exact reads, projection, classifier
  code, and `research_graph` are separate current surfaces.

## Same-class collision table

| Dimension | Existing evidence |
|---|---|
| Semantic class | Agent/model access to graph reads |
| Owners | Shared `search_graph`/`summarize_graph` wrappers; five-tool `GraphQueryExecutor` (`processor/agentic-tools/executors/register_graph_query.go:14-44`); research-classify's operation-specific `searchGraphRetriever.FetchCandidates` (`processor/research-graph-classify/adapters.go:109-169`); research-execute's `graphQueryAdapter.BM25` over `graph.query.searchGraph` (`processor/research-graph-execute/adapters.go:268-304`); operation-specific `fusionnats.Client` (`pkg/fusion/fusionnats/client.go:46-81`); `graph.ExactEntityReader` and constructor (`graph/exact_entity.go:25-48`); projection `MutationClient`, which retains an `ExactEntityReader` (`pkg/projection/mutation_client.go:29-37`); retained natural-language `query.Classifier`/`KeywordClassifier` (`graph/query/classifier.go:12-24`); sixteen graph-query responders (`processor/graph-query/query.go:45-66`); GraphQL gateway; optional `research_graph`. |
| Catalogs | Shared `ExecutorRegistry`; exported `BuiltinGroupKeys`; category map; internal sixteen-operation graph-query inventory (`processor/graph-query/query.go:45-66`); GraphQL root-field inventory; retained query-classification contract represented by `Classifier`, `SearchOptions`, and `NewKeywordClassifier` (`graph/query/classifier.go:12-24`). The operation inventory records `summary` consumers as graph-gateway plus unadmitted agentic-tools, and `searchGraph` consumers as graph-gateway, research-classify, research-execute, plus unadmitted agentic-tools (`processor/graph-query/query.go:62-63`). |
| Status | Tool discovery reports `available:true`; wrapper failures map classified handler errors to external and transport errors to network; readiness belongs to underlying responders |
| Lifecycle | Wrappers register once at boot with non-nil NATS; no cache or durable state; each call resamples current responders |
| Ownership | Shared registry owns builtins; component-local registrations override shared dispatch/discovery by name |
| Readers | Tool discovery/default-tool/loop/model consumers; research-classify reads `searchGraph` and projects responses to candidates (`processor/research-graph-classify/adapters.go:109-169`); research-execute reads `searchGraph` for BM25 evidence (`processor/research-graph-execute/adapters.go:268-304`); `fusionnats.Client` is the production `fusion.RetrievalClient` (`pkg/fusion/fusionnats/client.go:46-81`); embedded exact-authority consumers use `graph.ExactEntityReader` (`graph/exact_entity.go:25-48`), including projection `MutationClient` through its `reader` field (`pkg/projection/mutation_client.go:29-37`); graph-query strategy owners consume the retained classifier (`graph/query/classifier.go:12-24`). External readers of exported Go/category APIs remain unmeasured. |
| Writers | Both binary roots, builtin registration, external local registration, allow/default/skip config authors |
| Recovery | Component retry policy may retry transient tool errors; wrappers own no replay, rebuild, repair, status, or reconciliation |

## Adopter seam inventory

Specific adopter: an external component/config author who has never opened the executor files.

- **Must know:** nil/empty `allowed_tools` permits all shared tools; nil `default_tools` causes global discovery; the
  exact tool and skip-key spellings; the two wrappers depend on graph-query responders despite declaring no agentic
  graph-query ports.
- **If they do nothing:** both wrappers register in production, can be advertised to the model, and can be invoked in
  permissive deployments. Explicit shipped allowlists happen to exclude them.
- **Where discovered:** names via runtime `tool.list`; permissive allowlist semantics via schema/docs; dependency
  subjects and skip-key closure only through source/comments and runtime failures.
- **Should have to know:** current framework contracts state AI-agent MCP access is unavailable and embedded query
  access is admitted only through named typed adapters. The observable gap is that these shared wrappers expose copied
  subject/decoder knowledge outside those declared seams.
- **Prediction check:** the caller need not predict graph size or readiness, but must predict deployment admission and
  hidden responder availability from configuration and copied names.

## Exact closure searches

```text
rg -n '"(search_graph|summarize_graph)"' configs schemas
# zero matches: no exact JSON string value names either wrapper.
# This deliberately excludes prose substrings such as
# "search_graph response" and the distinct "research_graph" tool.

rg -n '"(search_graph|summarize_graph)"' .github
# zero matches.
```

```text
rg -n '"(graph_search|graph_summary)"' agentic processor --glob '*.go'
# processor/agentic-tools/categories.go:44: "graph_search"
# processor/agentic-tools/categories.go:45: "graph_summary"
# processor/agentic-loop/context_compaction_test.go:585: "graph_search"
# processor/agentic-loop/state_test.go:579,584,609: "graph_search"
# No exact "graph_summary" occurrence outside categories.go.
```

```text
rg -n '\b(graph_search|graph_summary)\b' \
  agentic/tools.go processor/agentic-loop --glob '*.go'
# agentic/tools.go:532: graph_search-style pagination comment
# processor/agentic-loop/context_compaction_test.go:585: graph_search fixture
# processor/agentic-loop/state_test.go:579,584,609-610: graph_search fixtures/assertions
# These are generic tool-name/history tests, not registered graph-query owners.
```

```text
rg -n \
  'GetToolCategory|RegisterToolCategory|EnableCategories|ReadOnlyCategories' \
  processor agentic --glob '*.go'
# Non-test production matches:
# processor/agentic-tools/categories.go:71-97
# processor/agentic-tools/config.go:31
# Remaining matches are categories tests and one write_todos category assertion;
# no production filtering caller was found.
```

```text
rg -n 'graph\.query\.(searchGraph|summary)' \
  processor/agentic-tools --glob '*.go'
# Matches only:
# executors/search_graph.go
# executors/summarize_graph.go
# their register-file comments
# RegisterBuiltins comments in executors/register.go
```

```text
rg -n '"(search_graph|summarize_graph)"' \
  gateway/graph-gateway pkg/fusion graph --glob '*.go'
# zero exact wrapper-name matches.
# This does not erase the differently named retained GraphQL fields,
# graph-query operations, fusion client, exact reader, or classifier.
```

**Open evidence unknown:** downstream Go imports/configurations are outside this repository and were not audited.

---

**Checkpoint:** Frozen at baseline `aa9d3704628e844c1f10b2c3627365c52e2b4685`; the final artifact checksum is recorded in the adjacent
`slice-f2-inventory.md.sha256` sidecar.

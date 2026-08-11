# DESIGN ARTIFACT — Slice F2 unadmitted agentic wrapper deletion

**Status:** Owner approved on 2026-08-10 after independent `DESIGN REVIEW PASS`.
**Repository baseline:** `aa9d3704628e844c1f10b2c3627365c52e2b4685`
**Accepted inventory checkpoint:** SHA-256 `eb136b5b6b818d84a0eec4744cdc9bc0fadd7883735c82738fae5dc506494ebf`
**Inventory review:** `INVENTORY PASS`
**Governing rulings:** Owner-approved rulings 5 and 14; approved F2 sequencing clarification.
**Scope boundary:** F2 only. F1 and the admitted graph-query contract remain closed.

## Accepted inventory identity

The complete accepted, line-addressable F2 inventory is incorporated without amendment by the checkpoint above. Its
measured findings include:

- both wrappers are shared builtins in both production binaries;
- nil/empty `AllowedTools` permits every registered shared tool;
- no shipped exact allowlist/default-tool value names either wrapper;
- the wrappers copy undeclared graph-query subject/decoder knowledge;
- `graph_search`/`graph_summary` are stale category spellings;
- direct `query_*`, `research_graph`, GraphQL, research adapters, fusion, exact reads, projection, and classifier
  surfaces are distinct retained owners;
- MCP graph access is not an implemented contract;
- downstream Go/config consumers are unknown.

## Design premises and measurements

| Premise | Measurement |
|---|---|
| Both wrappers are production-reachable | `RegisterBuiltins` installs them with non-nil NATS at `processor/agentic-tools/executors/register.go:184-207`; both binaries call it without skips at `cmd/semstreams/main.go:170-201` and `cmd/e2e-semstreams/main.go:165-188`. |
| Empty deployment policy does not hide them | `AllowedTools` nil/empty means allow-all at `processor/agentic-tools/config.go:20,95-98,155` and `component.go:552-558`. |
| Shipped configuration has no exact adopter | `rg -n '"(search_graph|summarize_graph)"' configs schemas` returned zero matches. |
| Skip keys are closed and boot-validated | `BuiltinGroupKeys` and `resolveSkipSet` are at `register.go:92-123,225-247`. |
| The wrappers export externally compilable API | `SearchGraph*` at `search_graph.go:39-62`; `SummarizeGraph*` and `NATSQuerier` at `summarize_graph.go:72-114`. In-repo production construction exists only in their registration functions; external importers are unmeasured. |
| Category aliases do not classify the real tools | The map contains `graph_search`/`graph_summary` at `categories.go:34-46`; the real names fall through to `CategoryCore` at lines 71-79. Production category filtering is unwired; external API consumers are unknown. |
| Admitted operations exist independently | `summary` and `searchGraph` are in the internal operation inventory at `processor/graph-query/query.go:62-63`; their implementations are `summary.go:58-124` and `searchgraph.go:29-120`. |
| Retained embedded access is operation-specific | Research-classify: `adapters.go:109-169`; research-execute: `adapters.go:268-304`; fusion: `pkg/fusion/fusionnats/client.go:46-81`; exact read: `graph/exact_entity.go:25-48`; projection exact-read seam: `pkg/projection/mutation_client.go:29-37`. |
| Query classification remains independent | `graph/query.Classifier` and `NewKeywordClassifier` remain at `graph/query/classifier.go:12-24`. |
| Provider presentation flows from registry discovery | Registry aggregation: `processor/agentic-tools/executor.go:135-197`; task fallback discovery: `processor/agentic-loop/handlers.go:908-919`; provider translation: `processor/agentic-model/client.go:393-410`. |
| Local extension precedence is independent | Discovery is local-over-shared at `processor/agentic-tools/component.go:803-859`; dispatch is local-first with shared not-found fallback at lines 675-687. |
| Schemas do not enumerate tool names | `schemas/agentic-tools.v1.json:8-15` and `schemas/agentic-dispatch.v1.json:25-31` accept string arrays. |
| No graph MCP replacement exists | `.agents/skills/query-pattern/SKILL.md:15-43,64-79`; `docs/concepts/11-query-access.md:84-88`. |

## Query-pattern application

- Remote application: existing admitted GraphQL `searchGraph` or `graphSummary`.
- Embedded framework service: its existing named operation-specific adapter.
- AI agent/LLM: no canonical graph MCP surface.
- Exact entity inspection: retained `query_*` tools or `graph.ExactEntityReader`, according to the existing caller
  boundary.
- Multi-stage graph research: selected `research_graph` capability.

The two shared wrappers are not an admitted pattern: they embed raw operation subjects and copied response projections
without agentic component ports or an operation-specific adapter contract. F2 introduces no MCP surface, client, port,
subject, config, or replacement tool.

## Options considered

| Option | Shape | Cost | Contract result |
|---|---|---:|---|
| 0. Do nothing | Retain both wrappers and current documentation | No migration cost | Leaves permissive discovery/execution, copied subjects/decoders, stale category aliases, and conflict with rulings 5/14. |
| 1. Hide through shipped allowlists or default skips | Add exclusions/configuration while retaining code and exports | Low code cost; permanent config and discovery debt | Nil/empty external configurations remain permissive; exported and copied-query surfaces remain; skip keys become compatibility controls. |
| 2. Extend an existing agent tool owner | Add search/summary definitions to `GraphQueryExecutor` or replace wrappers through another existing executor | Moderate; new tool definitions, projections, tests, and adopter contract | Still creates framework-owned agent graph-search/summary tools and conflicts with the approved complete deletion. |
| 3. Replace through admitted graph access | Route callers to existing GraphQL operations, exact-reader/query tools, research adapters, or `research_graph`; delete only the two shared wrappers and stale catalog expectations | Breaking Go/config migration; focused code/docs/test work | Uses existing owners without adding a surface and satisfies the approved clean break. |
| 4. Add MCP/general client/ports | Create a new canonical AI or embedded graph front door | Large cross-cutting contract and adoption cost | Outside F2 and expressly excluded. |

## Recommendation

Adopt Option 3 as the smallest complete F2 closure.

Delete the two framework-owned shared wrappers, their registration functions,
exported API, shared skip keys, stale alternate category entries, wrapper-specific
tests, and framework-owned documentation/schema expectations. Update the
graph-query operation inventory to remove only the two
`agentic-tools (unadmitted)` consumer claims.

The former names are not reserved. An application may register a genuinely local
executor under either name through the unchanged extension seam. Such a tool is
application-owned: SemStreams supplies no shared executor, alias, definition,
configuration default, dependency inference, or compatibility behavior for it.

## Target behavior

### Shared registry and post-upgrade configuration behavior

After F2, the framework shared registry does not register or advertise
`search_graph` or `summarize_graph`. Application-local registration under either
name remains possible through the existing local registry.

The exact post-upgrade behavior is:

| Input/state | Outcome |
|---|---|
| Shared discovery, no application-local executor | Former name is absent. |
| Nil/empty `AllowedTools` and an admitted direct `Component.Execute` call, with no local executor and no approval interception | Existing typed tool-not-found outcome. Nil/empty remains permissive; it does not create an executor. |
| Non-empty `AllowedTools` omits the former name | Existing global `not_allowed` rejection occurs before approval or registry dispatch. |
| Per-loop advertised set omits the former name | Existing `not_advertised` permission rejection occurs before approval or registry dispatch. |
| `default_tools` names a former name but no local executor exists | Resolver warns and drops it; it is not advertised to the model. |
| `approval_required` names a former name and an otherwise admitted wire call arrives | ApprovalFilter intercepts before registry dispatch and produces the existing approval-required/permission/pause behavior. No not-found result occurs until a later approved/bypassed dispatch reaches the registry. |
| Approved/bypassed or otherwise non-intercepted admitted call, no local executor | Local registry misses, shared registry misses, and the existing typed tool-not-found result is returned. |
| `tool_retries` names a former name, no executor | Policy entry creates no executor; it affects only a call that reaches execution. |
| Application registers a local executor under the former name | Existing allowlist, advertised-set, approval, retry, local discovery, and local dispatch rules apply to that application-owned tool. |

`allowed_tools`, `default_tools`, `approval_required`, and `tool_retries` remain
open vocabularies because they also name application-local tools. F2 adds no
closed framework-tool enum and no stale-name compatibility handling.

### SkipBuiltins

`BuiltinGroupKeys` no longer contains either deleted name. A stale `SkipBuiltins` entry fails boot through the existing
unknown-key validation. No accepted no-op, alias, compatibility key, or deprecated value remains.

### Exported Go and category APIs

Delete:

- `SearchGraphExecutor`
- `SearchGraphOption`
- `WithSearchGraphTimeout`
- `NewSearchGraphExecutor`
- `SummarizeGraphExecutor`
- `SummarizeGraphOption`
- `WithSummarizeGraphTimeout`
- `NewSummarizeGraphExecutor`
- `NATSQuerier`

External Go code selecting any symbol fails compilation.

Delete private category entries `graph_search` and `graph_summary`. Consequently, external `GetToolCategory` calls for
those alternate spellings change from `CategoryKnowledge` to the existing unknown-name default `CategoryCore`. The
exported category functions, enum values, registration extension, and `ReadOnlyCategories` remain unchanged. No
production category-filter behavior changes because none is wired at this baseline.

### Local registration, discovery, approval, and dispatch

Preserve:

- application registration through `RegisterTool`/`RegisterExecutor`;
- component-local executor registration under any non-reserved name, including
  either former framework name;
- local-over-shared discovery by name;
- admission checks before approval or execution;
- ApprovalFilter interception before registry dispatch on the wire path;
- local-first registry execution and shared fallback only on typed not-found;
- aggregation effect normalization;
- per-loop advertised-tool admission;
- all remaining allowlist, approval, and retry behavior.

SemStreams does not register, advertise, configure, or execute a shared
compatibility implementation under either former name. If an application reuses
one, discovery and execution come solely from that application-local executor.

### Query and research preservation

The graph-query operation inventory becomes:

- `summary`: graph-gateway consumer only;
- `searchGraph`: graph-gateway, research-graph-classify, and research-graph-execute consumers.

Preserve unchanged:

- all sixteen `graph.query/v1` responders and subjects;
- GraphQL `graphSummary` and `searchGraph`;
- research-classify `searchGraphRetriever`;
- research-execute graph-query adapter;
- `pkg/fusion/fusionnats.Client`;
- `graph.ExactEntityReader`;
- projection `MutationClient`;
- graph query classifier/search options;
- five direct `query_*` tools;
- optional `research_graph`;
- research candidate provenance value `Source: "search_graph"`;
- generic historical `graph_search` test fixtures and pagination terminology where they do not claim a registered
  wrapper.

## Exact implementation manifest

### Delete exactly

1. `processor/agentic-tools/executors/search_graph.go`
2. `processor/agentic-tools/executors/search_graph_test.go`
3. `processor/agentic-tools/executors/register_search_graph.go`
4. `processor/agentic-tools/executors/summarize_graph.go`
5. `processor/agentic-tools/executors/summarize_graph_test.go`
6. `processor/agentic-tools/executors/register_summarize_graph.go`

### Modify in production/catalog code

1. `processor/agentic-tools/executors/register.go`: remove both group keys, gates, and wrapper registration comment.
2. `processor/agentic-tools/categories.go`: remove stale `graph_search` and `graph_summary` entries only.
3. `processor/graph-query/query.go`: remove only `agentic-tools (unadmitted)` from the two consumer lists.
4. `processor/research-graph-classify/config.go`: replace wrapper-shaped “search_graph response” schema prose with
   the retained `graph.query.searchGraph` operation spelling.
5. `processor/research-graph-classify/adapters.go`: remove comments that cite the deleted executor file; retain
   adapter behavior, error/provenance semantics, and `Source: "search_graph"`.
6. `agentic/research/classifier_output.go`: replace wrapper-shaped type commentary with the retained
   operation/response contract.

### Modify tests/contracts

1. `processor/agentic-tools/executors/register_test.go`: update group golden; remove the assertion that the two wrappers
   are required core graph groups.
2. `processor/agentic-tools/categories_test.go`: pin both alternate names to the unknown-name `CategoryCore` result
   after catalog removal.
3. `processor/graph-query/operation_inventory_test.go`: update only the two consumer lists.
4. `graph/query/slice_f1_surface_contract_test.go`: remove F1's temporary preservation assertions for the four F2
   executor constructors/types while retaining every F1 preservation guard.
5. Existing effect-classification structural checks: allow the source-derived tool count to shrink; do not weaken
   recognized-effect validation.
6. `processor/agentic-tools/executors/predicate_authority_contract_test.go`: remove wrapper-only commentary if it
   names `summarize_graph`; preserve the predicate authority assertion.

### Documentation and generated schema

Update current guidance:

- `docs/operations/adopter-tool-effect-metadata.md`
- `docs/concepts/32-agent-memory.md`
- current query/tool catalogs that claim either wrapper remains available
- research-classify generated description in `schemas/research-graph-classify.v1.json`

Preserve historical ADRs, archived OpenSpec artifacts, issue evidence, and empirical incident text as history. Where a
current tutorial embeds historical evidence, mark it as historical rather than rewriting the event.

`schemas/agentic-tools.v1.json` and `schemas/agentic-dispatch.v1.json` receive no name-enum change. Schema generation is
expected to change only the research-classify description if its source schema prose changes.

## Failing-first tests

1. `TestSliceF2FrameworkWrapperSurfaceIsDeleted`: AST/source guard covering all nine exported names, both registration
   functions, both builtin keys, and framework-owned definitions.
2. `TestSliceF2DeletedSkipKeysFailClosed`: table over both names; existing unknown-key boot error before registration.
3. `TestSliceF2PermissiveAllowedToolsDoNotCreateDeletedExecutors`
   - cover nil and empty `AllowedTools`;
   - assert former names are absent from shared discovery;
   - invoke through direct `Component.Execute`, where ApprovalFilter does not
     intercept;
   - assert the existing typed not-found result;
   - prove a surviving shared builtin still executes.

4. `TestSliceF2ApprovalRequiredFormerNameInterceptsBeforeRegistryMiss`
   - configure a former name in `approval_required`, admit it through global and
     advertised checks, and supply no local/shared executor;
   - send it through `handleToolCall`;
   - assert the existing approval-required/permission/pause result and approval
     request;
   - use a registry spy to prove no dispatch occurred;
   - re-dispatch with the existing approval bypass and assert typed not-found
     when no local executor exists.

5. `TestSliceF2LocalFormerNameUsesExistingPrecedence`
   - register an application-local executor under each former name;
   - assert local discovery exposes only the local definition;
   - assert an unapproved wire call still follows existing ApprovalFilter
     ordering when configured;
   - assert approved/bypassed dispatch executes the local executor;
   - prove no shared alias, reserved-name rule, or compatibility executor
     participates.
6. `TestSliceF2RetainedGraphAccessSurfacesRemain`: source/compile guard for GraphQL, sixteen operations, research,
   fusion, exact, projection, classifier, five `query_*`, and `research_graph`.
7. `TestSliceF2OperationConsumersMatchRuntime`: exact remaining `summary`/`searchGraph` consumers.
8. `TestSliceF2CategoryAliasesFallBackToCore`: stale alternate spellings use exported unknown-name behavior.

Mutation checks must demonstrate that restoring one deleted registration/key/export or removing one preservation
symbol fails the relevant guard.

## Adopter migration

Release notes identify four independent breaks:

- Go importers remove deleted executor, option, constructor, or `NATSQuerier`
  use.
- Config authors remove the two names from `SkipBuiltins`; leaving either now
  fails boot.
- Config authors remove stale framework references from `default_tools`,
  `allowed_tools`, `approval_required`, and `tool_retries`. In particular, a
  stale `approval_required` entry is not inert: an otherwise admitted wire call
  can pause for approval before registry dispatch reveals that no executor
  exists.
- An application intentionally supplying its own local executor under a former
  name may retain matching allow/default/approval/retry entries; that is a local
  tool contract, not framework compatibility.
- Framework tool callers move according to the existing access contract:
  remote application → GraphQL `searchGraph`/`graphSummary`; embedded service →
  its named operation-specific adapter; exact graph reads → retained exact/query
  tools; multi-stage agent research → selected `research_graph`.

No migration guidance suggests MCP, raw NATS subjects, raw KV, a general client,
a shared alias, or a compatibility local executor.

## Verification gates

```text
go test -race ./processor/agentic-tools/... ./processor/graph-query \
  ./processor/research-graph-classify ./processor/research-graph-execute \
  ./graph/query ./pkg/fusion/fusionnats ./pkg/projection
go test -race ./...
task lint
task schema:generate
git diff -- schemas/ specs/
go test ./test/contract/...
task openspec:validate
task e2e:agentic
task e2e:research-graph
```

Both relevant E2E tiers must be green before the breaking F2 commit lands.

## Stop conditions

Stop F2 and return for owner ruling if deleting a wrapper requires changing a graph-query subject/response/responder,
GraphQL field, or research adapter; any production constructor exists outside the manifest; a shipped exact config
names either wrapper; category filtering is wired into production; local precedence changes; research provenance must
be renamed; schema generation adds a tool enum or unrelated drift; completion needs a shim, alias, reserved name, MCP,
general client, port, subject, config, or downstream implementation; or either E2E lacks claimed coverage.

- observed wire behavior dispatches to the registry before ApprovalFilter, or a
  former name in `approval_required` does not produce the existing
  approval-required/permission/pause result;
- implementation requires making allow/default/approval/retry fields closed
  enums to distinguish framework from application-local names;
- application-local reuse of a former name cannot be preserved without a shared
  alias, reservation, or special-case dispatch rule.

## OpenSpec clarification draft

Replace the existing requirement beginning
`### Requirement: The admitted builtin set excludes...` and all of its scenarios with:

```markdown
### Requirement: Framework-owned shared builtins exclude the unowned graph-query wrappers

The framework SHALL NOT supply shared builtin tools named `search_graph` or
`summarize_graph`. Their framework-owned shared registrations,
`BuiltinGroupKeys`, accepted `SkipBuiltins` keys, registration functions,
implementations, exported executor/option/constructor/querier symbols, tests,
schemas, documentation, discovery defaults, operation-consumer claims, and
alternate framework category entries `graph_search`/`graph_summary` SHALL be
absent.

This requirement does not reserve either former name or prohibit an application
from registering its own component-local executor under that name through the
existing general extension seam. An application-local executor SHALL remain
subject to the existing allowlist, per-loop advertised set, approval, retry,
local-over-shared discovery, and local-first dispatch behavior. SemStreams SHALL
add no shared alias, compatibility executor, reserved-name rule, dependency
inference, or special configuration behavior for such a local tool.

GraphQL `searchGraph` and `graphSummary`, their graph-query responders, research
consumers, exact reads, fusion, projection, classifier/search options, direct
`query_*` tools, and selected `research_graph` SHALL remain.

Open-vocabulary `allowed_tools`, `default_tools`, `approval_required`, and
`tool_retries` SHALL NOT become a closed framework-tool enum. Nil or empty
`AllowedTools` SHALL remain permissive for surviving or application-local
registered tools, but SHALL NOT create an absent executor. Stale deleted
`SkipBuiltins` values SHALL fail through existing closed-set validation.

#### Scenario: framework shared discovery excludes the deleted wrappers

- **WHEN** framework shared builtin registration and discovery run
- **THEN** neither former name has a framework-supplied definition or executor
- **AND** neither shared registration, skip key, exported implementation, or
  alternate category entry is present

#### Scenario: permissive allowlist does not create a deleted executor

- **GIVEN** nil or empty `AllowedTools`
- **AND** no application-local executor uses the former name
- **WHEN** shared discovery runs
- **THEN** the former name is absent
- **AND** an admitted direct call that is not intercepted for approval reaches
  the registries and returns the existing typed not-found outcome

#### Scenario: approval interception precedes registry miss

- **GIVEN** a former name remains in `approval_required`
- **AND** the wire call passes global and per-loop admission
- **AND** no executor is registered under that name
- **WHEN** the unapproved call is handled
- **THEN** ApprovalFilter produces the existing approval-required permission and
  pause behavior before registry dispatch
- **AND** a later approved or bypassed dispatch returns typed not-found if no
  local executor exists

#### Scenario: application-local reuse remains ordinary local extension

- **GIVEN** an application registers a local executor under a former name
- **WHEN** discovery and dispatch run
- **THEN** the local definition is discovered through existing local precedence
- **AND** existing admission, approval, retry, and dispatch rules apply
- **AND** no shared alias, reservation, or compatibility executor participates

#### Scenario: stale skip configuration fails visibly

- **GIVEN** `SkipBuiltins` contains either deleted key
- **WHEN** builtin configuration is validated
- **THEN** existing closed-set validation rejects it
- **AND** the framework does not silently accept a compatibility no-op
```

Add to `specs/graph-query/spec.md`:

```markdown
After removal of the unadmitted agentic wrappers, the operation inventory SHALL
record `summary` with graph-gateway as its sole in-repo consumer and
`searchGraph` with graph-gateway, research-graph-classify, and
research-graph-execute as its exact in-repo consumers. Subjects, handlers,
success shapes, availability behavior, and GraphQL fields SHALL remain unchanged.
```

Replace F2 tasks with:

```markdown
## F2. Unadmitted agentic wrapper deletion

- [ ] F2.1 Add failing source and behavior guards for the complete exported,
  registration, skip-key, category-alias, discovery, dispatch, permissive-
  allowlist, and operation-consumer surfaces.
- [ ] F2.2 Delete exactly the six framework wrapper implementation,
  registration, and test files; remove both shared builtin keys/gates, all nine
  exported symbols, and stale `graph_search`/`graph_summary` category entries.
  Do not prohibit application-local reuse of either former name. Add no shared
  replacement, reserved name, alias, shim, port, subject, client, MCP surface,
  or config field.
- [ ] F2.3 Make stale deleted `SkipBuiltins` values fail through existing
  closed-set validation. Preserve open-vocabulary
  allow/default/approval/retry fields, nil/empty `AllowedTools` semantics for
  registered tools, admission-before-approval ordering, ApprovalFilter-before-
  registry ordering on the wire path, application-local registration,
  local-over-shared discovery, and local-first dispatch.
- [ ] F2.4 Update the graph-query operation consumer inventory, temporary F1
  preservation guard, current docs, research adapter comments, and generated
  research-classify description. Preserve historical ADR/archive evidence and
  the independent research provenance spelling `Source: "search_graph"`.
- [ ] F2.5 Prove GraphQL fields, all sixteen responders, research adapters,
  fusionnats, exact reads, projection, classifier/search options, five direct
  `query_*` tools, and selected `research_graph` remain unchanged.
- [ ] F2.6 Run focused and full race tests, lint, schema/no-drift review,
  contract tests, strict OpenSpec validation, `task e2e:agentic`, and
  `task e2e:research-graph`; obtain independent SemStreams review before the
  breaking F2 commit lands.
```

No other OpenSpec capability delta is required.

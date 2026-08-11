# Migration: post-Foundation-B graph query contract closure

This notice covers adopter-visible breaks from the completed A-F2 graph-query contract-closure slices. It is
communicate-only: downstream teams own compilation, configuration changes, flow validation, and product E2E.
SemStreams does not audit or modify downstream repositories.

No compatibility aliases, deprecated wrappers, dual paths, new public subject catalog, or general embedded client are
provided.

## Required migrations

| Adopter surface | Required action | If unchanged | Discovery |
|---|---|---|---|
| GraphQL `capabilities` | Remove the field and select an admitted operation. | GraphQL validation fails. | Schema introspection and GraphQL error. |
| GraphQL `similaritySearch` | Use exact `semanticSearch` in both selection and response decoding. | GraphQL validation fails; there is no alias. | Schema introspection and GraphQL error. |
| GraphQL `localSearch` | Treat classified `index_not_ready` as retryable eventual availability. | The caller receives the typed transient while the optional community view is absent, synchronizing, or unusable. | GraphQL error extensions. |
| Go `graph/query.Client` importer | Use an admitted GraphQL operation for remote access or the existing named operation-specific adapter for embedded framework access. | Compilation fails at the deleted client/config/constructor/path/cache surface. | Compiler. |
| Go `graph.QueryResponse.RequestID` user | Remove field selection or keyed-literal entries; query success contains `Data` and `Timestamp`. | Compilation fails; no compatibility field exists. | Compiler and query-success specification. |
| Go importer of `SearchGraph*`, `SummarizeGraph*`, or `NATSQuerier` | Remove the framework wrapper use or own a distinct application-local executor. | Compilation fails because the complete shared wrapper surface is deleted. | Compiler. |
| `SkipBuiltins` containing `search_graph` or `summarize_graph` | Remove the deleted key. | Existing closed-set validation rejects startup before registration. | Boot error. |
| `allowed_tools`, `default_tools`, `approval_required`, or `tool_retries` naming a deleted shared wrapper | Remove the stale framework reference unless an application-local executor intentionally owns the same name. These fields remain open vocabulary. | `default_tools` warns and drops an unresolved name; allow/retry policy creates no executor; `approval_required` can pause an admitted call before registry miss; a non-intercepted call returns typed not-found. | Config review, warning, approval pause, or typed not-found. |
| Application-local executor intentionally named `search_graph` or `summarize_graph` | Keep the local executor and any matching open-vocabulary policy. | Existing local admission, discovery, approval, retry, and dispatch behavior applies; no shared compatibility executor participates. | Local registration and ordinary discovery. |
| Exported category API queried with `graph_search` or `graph_summary` | Stop treating these deleted alternate aliases as framework `CategoryKnowledge`. Accept the existing unknown-name `CategoryCore` result or explicitly register a category for an application-local tool. | `GetToolCategory` silently returns `CategoryCore`; exported category functions and `ReadOnlyCategories` otherwise remain. | Go behavior and tests. |
| Component declaring or consuming graph query | Declare interface `graph.query/v1` and the required named operation outputs in component ports. | Missing, old, or mismatched declarations fail Registry validation. | Registry validation and generated schema. |

## Stable surfaces

- All sixteen admitted `graph.query/v1` request/reply operations retain their existing subjects and success shapes.
- The fourteen surviving graph-query-backed GraphQL fields remain admitted.
- `pkg/fusion/fusionnats.Client`, `graph.ExactEntityReader`, `pkg/projection.MutationClient`, research adapters, direct
  `query_*` tools, and selected `research_graph` remain.
- Direct external NATS callers receive no wire migration in this program, but copied subject literals gain no public
  catalog or separate API promise.
- Nil or empty `AllowedTools` remains permissive for registered tools; it does not create an absent executor.

## Discovery order is surface-specific

There is no single truthful migration-discovery rank. Depending on the surface, the first signal is a compiler error,
boot or Registry rejection, GraphQL validation error, classified transient, default-tool warning/drop, approval pause,
typed not-found, or the silent `CategoryCore` fallback. The table above is authoritative for each migration.

## Downstream boundary

Downstream projects may break and migrate at their own pace. This notice does not require SemStreams to inspect,
compile, patch, or validate them, and it does not authorize unrelated runtime or issue-queue work.

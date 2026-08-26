# Proposal — flow-invalid-handling-contract

Slice C of the accepted flow-CRUD design (`docs/proposals/gh1008-1010-flow-crud-design.md`, owner-accepted
2026-08-23; §"Slice C: #1008 coherent invalid handling" `:126-209`; rulings 4, 5, 6, 7, 8 directly, 9 already landed
in Slice A, 12 through #1087). Closes #1008. Every `file:line` below was re-measured at `5cc0c7fb`.

## Why

`POST /flows` discards a classified client error. `flowstore.Flow.Validate` returns `errs.ErrorInvalid` for every
structural fault (`flowstore/flow.go:58-132`), `Manager.Create` returns it untouched (`flowstore/manager.go:83-85`),
and `handleCreateFlow` maps every Create failure to `500 {"error":"Failed to create flow"}`
(`service/flow_service.go:360-363`). `errs.IsInvalid` exists (`pkg/errs/errs.go:242-261`) and is called by no
production code under `service/` (`rg -n 'errs\.IsInvalid\(' service --glob '*.go'` → 0). A caller that omits a node's
`component` is told the server broke; retry logic keyed on 5xx loops on a request that can never succeed (#1008: 68
semstreams-ui E2E failures read as backend instability).

The same collapse repeats on every Flow mutation path:

- `PUT /flows/{id}`: invalid → 500 `Failed to update flow` (`:396`); a missing target → 500 (the Manager wraps the
  absence transient, `flowstore/manager.go:161-164`); the 409 body leaks the framework attribution prefix because it
  is `err.Error()` (`:393`).
- `POST /flows/{id}/validate`: a structurally invalid draft → `500 {"error":"Validation failed: flowengine.…"}`
  (`:539-543`), because `Engine.ValidateFlowDefinition` returns the structural failure as an error, not a result
  (`engine/engine.go:67-70`); a connection-pattern conflict is likewise an error, not a finding
  (`engine/validator.go:137-141`); the malformed-body and ID-mismatch responses echo decoder text and both IDs
  (`:522`, `:526`).
- `DELETE /flows/{id}`: `KVStore.Delete` publishes a DEL marker with no existence check (nats.go v1.52.0
  `jetstream/kv.go:1125-1172`; the not-found branch at `natsclient/kv.go:450-452` is unreachable on this path), so a
  repeat delete is `204` and a client cannot tell "deleted" from "never existed".
- `GET /flows`: Slice B preserved the Manager's failure class precisely so it could be projected; the handler still
  answers an opaque `500` for every class (`:267-274`).
- Publication: `Compile` on a structurally invalid saved Flow returns a nil result, so the 400 body carries
  `"validation_result": null` and `err.Error()` (`:426-432`); the 400 has no schema (`:230`).

The validation wire has no stable vocabulary. The structural faults carry no type — message text is the only detail
(`flowstore/flow.go:61-127`, thirteen `WrapInvalid` sites); the graph findings carry string literals
(`engine/validator.go:103,208,226,254,269,304,368,560,592`) that no constant, spec, or schema enumerates; `Nodes` and
`DiscoveredConnections` are nil on the empty-flow and build-error returns (`:99-113`, `:120-133`) and `Suggestions` is
`omitempty` (`:83`), so a client cannot rely on any array being present. Branching on message text is the shape
`openspec/specs/nats-kv-keys/spec.md:167` forbids.

## What Changes

Grouped by the design's own headings.

### Scope and canonical vocabulary (`:128-155`)

- **`flowstore` owns the validation data definition.** `ValidationResult`, `ValidationIssue`, `ValidatedNode`,
  `ValidatedPort`, and `DiscoveredConnection` move from `engine/validator.go:36-84` to `flowstore` — the closure is
  five types, not one, because `ValidationResult` embeds the other four and `engine` imports `flowstore`
  (`engine/validator.go:11`), so the result cannot live in `flowstore` without them. `engine` keeps every name as a
  type alias (`type ValidationResult = flowstore.ValidationResult`, …), so `flowengine.ValidationResult` in
  `service/flow_service.go` and any external importer keeps compiling unchanged. `engine.ValidationError`
  (`engine/engine.go:115-126`) moves the same way: `flowstore.ValidationError{Result *ValidationResult}` is the
  classified-invalid carrier of a result, aliased in `engine`.
- **Twenty stable issue types as exported `flowstore` constants**, the closed vocabulary: the twelve structural types
  in the design table (`flow_id_required`, `flow_name_required`, `node_id_required`, `node_component_required`,
  `node_type_required`, `node_name_required`, `duplicate_node_id`, `connection_id_required`,
  `connection_source_port_required`, `connection_target_port_required`, `connection_source_node_unknown`,
  `connection_target_node_unknown`) and the eight graph types already emitted as literals (`empty_flow`,
  `graph_build_error`, `unknown_component`, `connection_pattern_error`, `disconnected_node`, `orphaned_port`,
  `interface_mismatch`, `missing_interface`). `engine` emits through the constants; the literals go.
- **`Flow.Validate()` aggregates instead of failing fast.** Same signature (`error`), so `Manager.Create`,
  `Manager.Update`, `Engine.ValidateFlowDefinition`, and the converter test keep calling it; the returned error is a
  classified `errs.ErrorInvalid` that carries a `*flowstore.ValidationError` with the complete structural result:
  every fault as one `ValidationIssue` of severity `error`, in the deterministic order Flow fields → nodes in input
  order (required fields, then duplicate) → connections in order; `component_name` is the Flow ID or `(flow)`, the
  node name then ID then index, the connection ID then index; the first non-empty duplicate stays referenceable;
  `Nodes` and `DiscoveredConnections` are non-nil empty (no graph work ran); every `Suggestions` is non-nil.
  `ValidationError.Error()` enumerates every issue's type, component identity, and message, so text-only consumers —
  the FlowExecutor `create_flow`/`update_flow` tools (`processor/agentic-tools/executors/flows.go:142,156`) and logs —
  keep the actionable detail they have today. `Manager.Create`/`Update` run `Validate` before their empty-ID guard
  (the guard at `flowstore/manager.go:78-80,151-153` currently pre-empts the `flow_id_required` finding); the nil
  guard stays. Empty `nodes` remains structurally valid (a saved authoring draft may be empty); the Engine keeps
  emitting `empty_flow`.
- **Every `ValidationResult` has four non-null arrays and every issue has non-null suggestions.** `Nodes` and
  `DiscoveredConnections` are allocated at construction; `Suggestions` loses `omitempty` and is allocated at every
  emission site; the two registry-level `graph_build_error` sites with no component (`engine/validator.go:207-210,
  268-271`) and `empty_flow` (`:105`, today `(none)`) carry the Flow identity (Flow ID or `(flow)`), the same rule
  structural findings use. `validation_status` derives errors → warnings → valid, as today (`:184-189`).
- **A connection-pattern error is a finding, not an error.** `Validator.ValidateFlow` converts the classified
  invalid error from `FlowGraph.ConnectComponentsByPatterns` (`component/flowgraph/flowgraph.go:216-247`) into
  exactly one `connection_pattern_error` issue (severity `error`, Flow identity as component, the pattern error's text
  as the message, non-nil suggestions), sets status `errors`, and returns `(result, nil)` with all arrays non-nil.
  `Engine.Compile` then fails with the existing `ValidationError` because `len(result.Errors) > 0`
  (`engine/engine.go:92-94`).
- **`Engine.ValidateFlowDefinition` returns structural invalidity as a result.** A `Validate` failure becomes
  `(structuralResult, nil)` instead of `(nil, WrapInvalid(err))` (`engine/engine.go:67-70`), matching its own doc
  comment ("validation findings are returned in the result; infrastructure failures are returned as errors"). Only a
  nil Flow or a validator execution failure remains an error.

### HTTP contract (`:157-186`)

- **`FlowErrorResponse`** — a `service` HTTP-boundary type: `Error string` (required, non-empty) and
  `ValidationResult flowstore.ValidationResult` tagged `json:"validation_result,omitzero"` — a value, not a pointer,
  because the generator renders a pointer as `anyOf [..., null]` (`service/schema.go:13-21`) and the design requires
  the member to be optional and non-null. `omitzero` (encoding/json, Go ≥ 1.24; `go.mod` is `go 1.26.3`) omits the
  member when no result was attached. The generator learns `omitzero` as an optional marker beside `omitempty`
  (`service/schema.go:107`). `validation_result` is present for Create/Update validation and for publication
  validation; it is omitted on every other failure.
- **One classified mapper**, `projectFlowError(err) (status, FlowErrorResponse)`, is the single home for turning a
  Flow-path error into a status and a public message, in this order, each decided by classification or sentinel and
  never by message text: `errors.As` → `*flowstore.ValidationError` → `400 Flow validation failed` with the result;
  `errors.Is(err, errs.ErrRevisionMismatch)` → `409 Flow version conflict` (before any `IsInvalid` check, per
  `pkg/errs/errs.go:378-380`); `errors.Is(err, natsclient.ErrKVKeyExists)` → `409 Flow already exists`;
  `errors.Is(err, natsclient.ErrKVKeyNotFound)` → `404 Flow not found` (before the transient check, because
  `Manager.Update`/`Delete` wrap absence transient — `flowstore/manager.go:161-164`, and the must-exist read below);
  `errors.Is(err, context.DeadlineExceeded)` → `504 Flow storage request timed out` (before the transient check —
  nothing in `pkg/errs` classifies a deadline distinctly; `errs.go:172` only consults it for unclassified errors);
  `errs.IsInvalid` → `400` (message per open question 1); `errs.IsTransient` → `503 Flow storage temporarily
  unavailable`; everything else (`errs.IsFatal`, unclassified) → `500 Internal server error`. The original error is
  logged at the handler; raw NATS text and stored malformed content never reach a body.
- **Handler outcomes.** Create: `201`; `400 Invalid request body` (malformed JSON); `400` with result (validation);
  `409 Flow already exists`; `503/504/500`. Update: `200`; `400 Invalid request body`; `400 Flow ID does not match
  request path`; `400` with result; `404 Flow not found`; `409 Flow version conflict`; `503/504/500`. Validate:
  `200 ValidationResult` for valid, warning, structural-invalid, graph-invalid, and pattern-error drafts;
  `400 Invalid request body`; `400 Flow ID does not match request path`; `500` for an execution failure; the
  saved-Flow pre-read (`:531-538`) is untouched (Slice D). List: `200` per Slice B; a Manager failure projects through
  the mapper — deadline → `504`, transient → `503`, corrupt (fatal) or unknown → `500` — always a sanitized
  `FlowErrorResponse`. The `PUT` conflict body stops leaking `err.Error()`.
- **DELETE is must-exist.** `Manager.Delete` reads the record first (`KVStore.Get`; typed absence survives the
  transient wrap as `natsclient.ErrKVKeyNotFound`) and only then deletes. HTTP: existing → `204` with no body and no
  `Content-Type`; absent or repeated → `404 Flow not found`; deadline/transient/fatal → `504/503/500`. The read and
  the delete are not fenced: two deleters that both observed the record both succeed; no atomicity is promised. The
  FlowExecutor `delete_flow` tool code is unchanged and now reports a missing Flow as a tool error over a real
  Manager (design: "FlowExecutor Delete retains missing as error").
- **Publication.** `200` progress (unchanged). A `Compile` failure that returned a result — every validation failure,
  structural ones included now that `ValidateFlowDefinition` returns them as results — → `400 {"error":"Flow
  validation failed","validation_result":{…}}` with the result present and non-null. A `Compile` failure with no
  result → the mapper (an execution failure → `500` `FlowErrorResponse`). Component-config persistence failure keeps
  today's `500` progress body (`:433-439`). The pre-read stays at its current unconditional `404 Flow not found`
  (`:421-425`), which already has the `FlowErrorResponse` shape; Slice D routes it through the mapper for
  `503/504/500`.
- **OpenAPI.** `ValidationResult`, `ValidationIssue`, and `FlowErrorResponse` are registered response types and become
  named component schemas (`ValidationIssue` also appears inline inside `ValidationResult` — the generator inlines
  nested structs, Slice B's accepted precedent). Operation rows: `POST /flows` 201/400/409/503/504/500; `PUT` 200/400/
  404/409/503/504/500; `DELETE` 204/404/503/504/500; `GET /flows` 200/503/504/500; `POST …/validate` 200
  `ValidationResult`/400/404/500; `POST …/publish-component-configs` 200/400/404 and `500` as `oneOf`
  [`FlowErrorResponse`, `publishComponentConfigsResponse`]. `service.ResponseSpec` cannot express more than one
  schema today (`service/openapi_types.go:47-52`); it gains `SchemaRefs []string`, and `cmd/openapi-generator`'s
  builder (`openapi_builder.go:126-165`) renders a multi-ref response as `oneOf` through the output model's existing
  `SchemaRef.OneOf` (`openapi_operations.go:41-46`). The dangling-ref validator already walks `oneOf` lists
  (`validate.go:38-62`).

### Actual adopter outcome (`:188-192`)

Editor live validation (`+page.svelte:245-263`, per the accepted inventory) and MCP `validate_flow`
(`tools.ts:105-123`) consume structural-invalid drafts automatically, because they already parse the 2xx result.
Editor save still shows only the top-level `error`; the Create UI shows status text while `validation_result` is
retained in `APIError`; publication UI already consumes `validation_result`; generated clients gain the three
schemas. No claim is made that the Create or Save UI renders issue detail. semstreams-ui was inspected read-only at
`39f5f04` for the DELETE seam only: `flowApi.deleteFlow` throws on any non-2xx (`src/lib/services/flowApi.ts:142-155`)
and the E2E orphan reaper already treats a `404` on delete as success (`e2e/helpers/backend-helpers.ts:359-363`).

### Consumers

- semstreams-ui Flow list, editor, live validation, MCP validation, publication UI, and the E2E helpers (locations
  per the accepted inventory §2, §8; the sister repo is hands-off).
- FlowExecutor `create_flow`/`update_flow`/`delete_flow` through every composition (`cmd/semstreams/main.go:245`,
  `cmd/e2e-semstreams/main.go:185`): unchanged tool code; `Error()` text keeps per-issue detail; `delete_flow` of a
  missing Flow becomes a tool error.
- `service/flow_service.go:15` is the only in-tree importer of `engine`; the sister importer found by a read-only
  grep of sibling checkouts is semteams `cmd/semteams/main.go:24,595`, pinned to `semstreams v1.0.0-beta.160` and
  calling a six-argument `flowengine.NewEngine` that no longer exists at head — it references none of the moved
  types, and the aliases keep any `flowengine.ValidationResult` reference compiling regardless.
- Generated OpenAPI clients gain `ValidationResult`, `ValidationIssue`, `FlowErrorResponse`.

## Non-goals

- Slice D (#1060): the six Get projections — direct `GET /flows/{id}` (`:371-378`), publication pre-read
  (`:421-425`), Validate-without-body pre-read (`:531-538`), and the messages/health/metrics pre-reads
  (`service/flow_runtime_messages.go:70`, `flow_runtime_health.go:76`, `flow_runtime_metrics.go:107`). Their
  `503/504/500` rows and `TestFlowOpenAPIResponseMatrix` (all 54 cells) land with D, reusing this change's mapper and
  `FlowErrorResponse`. `Manager.Get`'s classification is untouched.
- No ADR (rulings 4–8 are the owner's recorded decisions; the mechanics live in the spec). No NATS migration, bucket
  change, or stored-record change.
- No new structural type beyond the twelve, so a duplicate component instance NAME (`engine/engine.go:98-100`) stays a
  `Compile`-level failure — see open question 2.
- No fenced (`DeleteAtRevision`) must-exist delete; no change to `FlowManager` (`executors/flows.go:16-22`) or its
  in-memory fake.
- No ordering promise for `ValidationResult.Nodes` (built from a map at `engine/validator.go:405`); no `$ref` reuse
  inside generated schemas; no change to `Flow`, `FlowCreateRequest`, `FlowUpdateRequest`, `FlowListResponse`.
- The gateway's package-local classified-error mapper (`gateway/http/http.go:300-377`: unexported, substring
  `"timeout"`, `{"error","status"}` body) is a second home for the same semantic job. Consolidating both into one
  shared projection is a filing, not Slice C; this mapper is written as a pure function so it can be lifted.
- `jetstream.ErrInvalidKey` from an unusable path ID reaches every Manager operation wrapped transient (→ `503` under
  the mapper). Classifying it invalid at the flowstore boundary is a filing, not Slice C — see open question 1.
- The `flow-authoring-http-contract` core E2E scenario (14 assertions) is #1087's — but see the BREAKING assessment.
- semstreams-ui candidate validation is the owner-run tag gate (ruling 11).
- No new exported surface on `natsclient`, `graph`, `message`, or `pkg/*`. New exported surface in `flowstore`
  (types, constants, `ValidationError`), `service` (`FlowErrorResponse`, `ResponseSpec.SchemaRefs`), and the
  `engine` aliases is named here for the owner rather than treated as approved by drafting.

## Open questions requiring an owner ruling

1. **Public message for the mapper's generic-invalid branch.** The design's table (`:165-173`) has no row for an
   `errs.IsInvalid` failure that is neither a validation result nor a conflict, and its DELETE text (`:182-183`)
   says "invalid direct ID → 400" while its final matrix (`:225`) lists no `400` for Delete. At head there is no
   reachable HTTP producer: the mux never yields an empty `{id}`, Create generates an ID, Update checks the mismatch
   first, and an SDK-invalid key arrives wrapped transient. The branch still exists for Slice D's reuse and for
   completeness. Recommended: `Invalid request`, and no OpenAPI `400` cell for Delete.
2. **Publication projection of `Compile` failures that are not validation findings** — duplicate component instance
   name (`engine/engine.go:98-100`) and node-config marshal (`:101-104`), both returned with a non-nil result. The
   delta encodes the literal reading of "400 validation with result": every `Compile` failure that returned a result
   is `400 Flow validation failed` with that result present — which for a duplicate instance name is a `400` whose
   result reports `valid`. Recommended alternative for a separate ruling: have the validator emit a `graph_build_error`
   finding for a duplicate instance name (the registry already rejects duplicates at
   `component/flowgraph/flowgraph.go:157`), so Validate reports it at `200` and publication's `400` is coherent. That
   is an extension of the accepted text and is not encoded here.
3. **Is Slice C BREAKING?** See the assessment below. The owner's answer decides whether the commit carries `!` and
   whether the core E2E scenario is pulled forward into this PR.

## BREAKING assessment

The design does not label any slice BREAKING and places every E2E scenario on the combined candidate (`:275-277`);
Slices A and B declared "Not breaking" on additive grounds. Slice C is not additive. Grounding sentence: **nats.go
v1.52.0 `jetstream/kv.go:1125-1172` publishes a DEL marker with no existence check, so at head `DELETE /flows/{id}` on
an absent Flow is `204` (`flowstore/manager.go:215-225` → `natsclient/kv.go:444-459`), and this change makes it `404`
— a response-contract change on an existing operation, which is the preflight rubric's "wire-contract change …
error envelope" (`.claude/skills/preflight/SKILL.md:41-44`).** The status re-mapping (`500` → `400/404/409/503/504`
on five operations; `500` → `200` for a structurally invalid Validate draft) and the exact-message changes
(`ID mismatch` → `Flow ID does not match request path`; `Invalid JSON in request body: …` → `Invalid request body`;
`Failed to … flow` → the table) are the same class. Schema changes are additive (`FlowErrorResponse` is a superset of
`{"error"}`; `ValidationIssue.suggestions` becomes always-present; the `engine` names are aliases).

Recommendation: treat Slice C as BREAKING for the changelog (`feat(flow)!:` or a `BREAKING CHANGE:` footer naming
must-exist DELETE and the status projection). The hard rule then requires a covering E2E tier green before the commit
lands. No tier exercises `/flowbuilder/flows` at head (`grep -rn 'flowbuilder/flows' test/e2e` →
`test/e2e/client/observability.go:87-114`, an unused client helper); the covering tier is `task e2e:core` with the
`flow-authoring-http-contract` scenario, which exists only as #1087's description. So a BREAKING ruling means one of:
(a) author `flow-authoring-http-contract` inside this PR and run `task e2e:core` green — the scenario's fourteen
assertions are exactly this slice's behaviour (invalid Create/Validate, non-null result arrays, must-exist Delete) plus
Slice A's timestamps and Slice D's deleted Get; or (b) an explicit owner waiver recorded as a PR comment, with #1087
named as the filed coverage gap. Not self-certified here: the owner rules, and `tasks.md` §6.7 records the answer.

## Impact

- **Affected spec:** `flow-authoring` — ADDED requirements (vocabulary, result shape, Validate status, error mapper,
  must-exist DELETE, List projection, publication responses, OpenAPI); MODIFIED "Saved Flow mutations are
  authoring-only" (its invalid-rejection scenario now names the status, body, and tests) and Slice A's "Concurrent
  Updates are revision-fenced and exactly one wins" (the 409 body is now the exact `FlowErrorResponse`).
- **Affected code:** `flowstore/flow.go`, `flowstore/validation.go` (new), `flowstore/manager.go` (Create/Update
  guard order, Delete), `flowstore/flow_test.go`, `flowstore/manager_integration_test.go`; `engine/validator.go`,
  `engine/engine.go`, `engine/validator_test.go`, `engine/engine_test.go` (new or extended);
  `service/flow_service.go`, `service/schema.go`, `service/openapi_types.go`, `service/flow_service_test.go`,
  `service/flow_surface_test.go`, `service/flow_error_test.go` (new, unit);
  `processor/agentic-tools/executors/flows_integration_test.go`; `cmd/openapi-generator/openapi_builder.go` and its
  test; `specs/openapi.v3.yaml` (regenerated).
- **Rollback boundary:** one PR (or the two seams recorded in the handoff's size assessment, if the owner splits).
  Reverting restores fail-fast `Validate`, the literal issue types, the opaque statuses, and unconditional-success
  DELETE; stored bytes are the same `Flow` JSON either way. Slice D depends on this change's mapper and
  `FlowErrorResponse`; the design's "Slice 4 … can be reverted without reverting actionable invalid responses" holds in
  the other direction only.

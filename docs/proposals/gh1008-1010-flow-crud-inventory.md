# Flow CRUD release-blocker surface inventory: #1008, #1009, #1010

## Checkpoint identity

- Artifact path:
  `docs/proposals/gh1008-1010-flow-crud-inventory.md`
- SemStreams code baseline:
  `774c85dcf75bdce242f1f15ee2a5a310991ecf0d`
- PR #1052 inspected head:
  `bb680f13d9e59db40f72649fee2951b19b42fd61`
- semstreams-ui inspected baseline:
  `39f5f04030e54cd7e5ac1b20490b877bb7b7f2dd`
- Focused verification:
  `go test ./flowstore ./processor/agentic-tools/executors` — PASS
- The shared SemStreams worktree contains unrelated parallel-agent edits and artifacts, including lifecycle-owner test
  edits and #1054 proposal artifacts. This inventory did not edit or incorporate them.
- This replacement text supersedes the currently materialized artifact content and its prior content hash.
- SHA-256 is recorded in the independent review request with this artifact path and the baseline above.
- Review state: independent re-review required; not `INVENTORY PASS`.
- No target state, TDD grouping, implementation plan, or recommendation is included.

## Problem statement

The three issues reach the same outward Flow CRUD contract but arise in distinct existing owners:

- #1008: classified validation information is discarded by HTTP projections.
- #1009: server-owned creation time is overwritten inside `flowstore.Manager.Update`.
- #1010: `flowstore.Manager.List` treats empty state and ordinary deletion churn as failures on some consumer paths.

The inventory also identifies adjacent Get projection, validation-vocabulary, manager-topology, schema, and concurrency
claims. Their presence does not automatically place them inside any issue’s implementation scope.

## Surface inventory

### 1. Classified Flow errors lost at HTTP boundaries

#### Create

- Flow structural validation returns `errs.ErrorInvalid` at `flowstore/flow.go:57-131`.
- Missing component is detected at `flowstore/flow.go:75-79`.
- `Manager.Create` preserves validation errors at `flowstore/manager.go:49-60`.
- Existing-key conflicts are also classified invalid at `flowstore/manager.go:75-80`.
- Other Create persistence failures are classified transient at `flowstore/manager.go:80`.
- `POST /flows` maps malformed JSON to 400 at `service/flow_service.go:274-279`.
- It maps every `Manager.Create` error to opaque 500 at `service/flow_service.go:283-285`.

#### Update

- `Manager.Update` preserves structural validation failures at `flowstore/manager.go:106-117`.
- Version mismatch is classified invalid at `flowstore/manager.go:125-130`.
- The HTTP handler maps malformed JSON and ID mismatch to 400 at `service/flow_service.go:303-312`.
- It recognizes only the message substring `"conflict"` as 409 at `service/flow_service.go:313-317`.
- Every other update error becomes opaque 500 at `service/flow_service.go:318-319`.

#### Validate operation

- `Engine.ValidateFlowDefinition` returns classified invalid errors for:
  - nil Flow at `engine/engine.go:63-66`;
  - structural `Flow.Validate` failures at `engine/engine.go:67-70`;
  - graph-validator execution errors at `engine/engine.go:72-77`.
- `POST /flows/{id}/validate` calls that operation at `service/flow_service.go:461`.
- Every returned error becomes `500 {"error":"Validation failed: ..."}` at `service/flow_service.go:462-464`.
- The operation advertises 400 for invalid requests at `service/flow_service.go:228` and
  `specs/openapi.v3.yaml:304-330`.

The #1008 failure class therefore exists in CRUD persistence and the dedicated validation endpoint.

#### Existing classification and HTTP interpretation

- `pkg/errs/errs.go:241-260` exposes `errs.IsInvalid`.
- `WrapInvalid` includes component, method, action, and inner cause in `err.Error()` at
  `pkg/errs/errs.go:392-443`.
- No production `errs.IsInvalid` call exists under `service/`.
- A package-local classified-error mapper exists at `gateway/http/http.go:299-343`.
- Its sanitizer maps invalid errors to `"invalid request"` at `gateway/http/http.go:346-376`.
- `natsclient/errors.go:20-36` documents invalid classification as a 4xx concern and assigns precise distinctions to
  stable codes rather than message parsing.
- Flow validation errors currently carry no Flow-specific stable error code.
- OpenAPI advertises:
  - POST 400 at `service/flow_service.go:219-222`;
  - PUT 400/409 at `service/flow_service.go:223-226`;
  - validate 400 at `service/flow_service.go:228`.

### 2. Existing structured validation-result owner

A structured, adopter-consumed Flow validation shape already exists.

#### Framework owner

- `engine/validator.go:36-43` defines `ValidationResult` with:
  - validation status;
  - errors;
  - warnings;
  - validated nodes;
  - discovered connections.
- `engine/validator.go:76-84` defines `ValidationIssue` with:
  - issue type;
  - severity;
  - component name;
  - optional port;
  - human-readable message;
  - suggestions.
- `Validator.ValidateFlow` constructs the result at `engine/validator.go:86-92`.
- Empty diagrams become a structured `empty_flow` issue with correction suggestions at
  `engine/validator.go:98-112`.
- `Engine.ValidateFlowDefinition` returns graph findings in `ValidationResult`; findings do not become a returned Go
  error at `engine/engine.go:72-81`.
- `engine.ValidationError` summarizes a structured result at `engine/engine.go:115-126`.

#### Existing HTTP projections

- The validation endpoint returns `ValidationResult` directly on its success path at
  `service/flow_service.go:461-466`.
- Explicit publication calls `Engine.Compile` at `service/flow_service.go:348`.
- Compile/validation failure is projected as HTTP 400 with:
  - `"error": err.Error()`;
  - `"validation_result": validation`;
  at `service/flow_service.go:349-353`.
- Structural `Flow.Validate` failure returns no `ValidationResult` from `Engine.ValidateFlowDefinition`, so publication
  can emit 400 with `validation_result: null`.
- The publication 400 response has only a description and no response schema at `service/flow_service.go:229` and
  `specs/openapi.v3.yaml:280-303`.

#### semstreams-ui validation spellings

There are three current validation/error type homes in semstreams-ui.

1. Publication validation:

   - `/Users/coby/Code/c360/semstreams-ui/src/lib/services/publishApi.ts:18-26` hand-types the HTTP 400 body because
     OpenAPI omits its schema.
   - It exposes validation failure as `PublishOutcome.kind == "invalid"` at
     `/Users/coby/Code/c360/semstreams-ui/src/lib/services/publishApi.ts:28-40`.
   - It parses HTTP 400 and preserves `validation_result` at
     `/Users/coby/Code/c360/semstreams-ui/src/lib/services/publishApi.ts:84-106`.

2. Full editor/port validation:

   - `/Users/coby/Code/c360/semstreams-ui/src/lib/types/port.ts:170-182` models status, errors, warnings, validated
     nodes, and discovered connections.
   - Its issue type is at `/Users/coby/Code/c360/semstreams-ui/src/lib/types/port.ts:213-233`.

3. Save API validation/error spelling:

   - `/Users/coby/Code/c360/semstreams-ui/src/lib/api/flows.ts:29-36` defines a third `ValidationResult` using:
     - `valid: boolean`;
     - `errors[].field`;
     - `errors[].message`;
     - `errors[].code`.
   - This shape does not match `engine.ValidationResult`.
   - `/Users/coby/Code/c360/semstreams-ui/src/lib/api/flows.ts:38-44` defines `APIError` with optional
     `validation_result`.
   - `/Users/coby/Code/c360/semstreams-ui/src/lib/api/flows.ts:46-57` defines `ValidationError`.
   - The save path parses `APIError` but throws a plain `Error`, not `ValidationError`, at
     `/Users/coby/Code/c360/semstreams-ui/src/lib/api/flows.ts:67-84`.
   - Its `isValidationError` type guard is at
     `/Users/coby/Code/c360/semstreams-ui/src/lib/api/flows.ts:87-95`.

Additional reduced validation vocabulary exists at
`/Users/coby/Code/c360/semstreams-ui/src/lib/types/validation.ts:29-64`.

#### semstreams-ui validation consumers

- Publication E2E proves structured invalid detail at
  `/Users/coby/Code/c360/semstreams-ui/e2e/publish-config.spec.ts:13-47`.
- Invalid publication UI behavior is exercised at
  `/Users/coby/Code/c360/semstreams-ui/e2e/publish-config.spec.ts:166-189`.
- The production editor constructs an unsaved Flow draft at
  `/Users/coby/Code/c360/semstreams-ui/src/routes/flows/[id]/+page.svelte:234-241`.
- It POSTs that draft to `/flowbuilder/flows/{id}/validate` at that file’s `:245-251`.
- Every non-2xx validation response is logged and returned as `null` at `:253-256`.
- Transport failures, JSON parse failures, and other exceptions are also logged and returned as `null` at `:258-263`.
- Structured graph findings are therefore consumed only on the 2xx path. Structural invalidity currently projected
  by SemStreams as 500 becomes the same `null` value as transport or parse failure.
- The model-facing MCP tool `validate_flow` is defined at
  `/Users/coby/Code/c360/semstreams-ui/src/lib/server/mcp/tools.ts:79-98`.
- It rejects missing input locally at `:99-103`, POSTs the Flow to the validation endpoint at `:105-114`, converts
  every non-2xx response into a generic `HTTP <status>: <statusText>` exception at `:116-118`, and returns structured
  JSON only on success at `:120-123`.
- `MCPServer.validateFlow` exposes that tool execution at
  `/Users/coby/Code/c360/semstreams-ui/src/lib/server/mcp/server.ts:197-208`.

The actionable invalid-response vocabulary is not absent. It has a framework owner, incomplete OpenAPI projection,
multiple downstream spellings, a browser consumer that collapses non-2xx and transport into `null`, and a model-facing
consumer that collapses non-2xx detail into a status exception.

### 3. Update accepts client authority over `created_at`

- Flow audit fields are declared at `flowstore/flow.go:25-29`.
- `Manager.Create` owns:
  - `Version`;
  - `CreatedAt`;
  - `UpdatedAt`;
  - `LastModified`;
  at `flowstore/manager.go:62-67`.
- `FromComponentConfigs` initializes those timestamps at `flowstore/converter.go:27-38`, but its result later passes
  through `Manager.Create`, which re-stamps them.
- `Manager.Update` loads the stored Flow at `flowstore/manager.go:119-123`.
- It uses the stored record only for version comparison at `flowstore/manager.go:125-130`.
- It increments/stamps `Version`, `UpdatedAt`, and `LastModified` at `flowstore/manager.go:132-135`.
- It never restores `current.CreatedAt`.
- It marshals the caller-supplied Flow wholesale at `flowstore/manager.go:137-145`.
- The HTTP handler decodes the request directly into `flowstore.Flow` at `service/flow_service.go:303-313`.
- An omitted timestamp becomes Go’s zero `time.Time`.
- An arbitrary supplied timestamp remains authoritative.

#### Schema and tool claims

- The shared Flow request/response schema marks `created_at`, `updated_at`, and `last_modified` required at
  `specs/openapi.v3.yaml:1295-1379`.
- `service/flow_surface_test.go:43-67` asserts property presence but not request ownership.
- `create_flow` says version and timestamps are system-managed at
  `processor/agentic-tools/executors/flows.go:41-57`.
- `update_flow` mentions only current version at `processor/agentic-tools/executors/flows.go:58-72`.
- `CreatedBy` remains client-preserved at `service/flow_service_test.go:90-129`.

#### Concrete semstreams-ui editor consumer

- The editor update DTO omits all audit fields at
  `/Users/coby/Code/c360/semstreams-ui/src/lib/api/flows.ts:17-24`.
- It serializes that DTO verbatim at
  `/Users/coby/Code/c360/semstreams-ui/src/lib/api/flows.ts:67-84`.
- The editor constructs exactly that timestamp-free body at
  `/Users/coby/Code/c360/semstreams-ui/src/routes/flows/[id]/+page.svelte:503-524`.
- It updates only returned version and `updated_at` locally at that file’s `:526-532`.
- E2E asserts `created_at` preservation at
  `/Users/coby/Code/c360/semstreams-ui/e2e/flow-crud.spec.ts:502-543`.

#### Second semstreams-ui CRUD client

`flowApi` is a separate full CRUD client:

- Create: `/Users/coby/Code/c360/semstreams-ui/src/lib/services/flowApi.ts:56-80`.
- List: `/Users/coby/Code/c360/semstreams-ui/src/lib/services/flowApi.ts:82-97`.
- Get: `/Users/coby/Code/c360/semstreams-ui/src/lib/services/flowApi.ts:99-114`.
- Update sends the complete `Flow`: `/Users/coby/Code/c360/semstreams-ui/src/lib/services/flowApi.ts:116-140`.
- Delete: `/Users/coby/Code/c360/semstreams-ui/src/lib/services/flowApi.ts:142-155`.
- It wraps non-2xx outcomes in `FlowApiError`, preserving status and optional response detail at
  `/Users/coby/Code/c360/semstreams-ui/src/lib/services/flowApi.ts:17-25`.

This client’s full-Flow update preserves current timestamps only when its input originated from a successful full Flow
read. The editor’s dedicated `saveFlow` path omits them.

### 4. Concurrent deletion and empty state abort Flow List

#### Flowstore behavior

- `flowstore.Manager` retains both raw `jetstream.KeyValue` and wrapped `natsclient.KVStore` at
  `flowstore/manager.go:21-24`.
- Both handles are initialized at `flowstore/manager.go:42-45`.
- `Manager.List` calls raw `bucket.Keys` at `flowstore/manager.go:164-168`.
- It then calls `Manager.Get` for every key at `flowstore/manager.go:170-173`.
- It aborts on every per-key error at `flowstore/manager.go:173-176`.
- `Manager.Get` delegates to `KVStore.Get` and wraps every failure transient at
  `flowstore/manager.go:86-100`.

#### Sentinel chain

- `KVStore.Get` maps SDK absence/deletion to `natsclient.ErrKVKeyNotFound` at
  `natsclient/kv.go:75-92`.
- `ClassifiedError.Unwrap` preserves the inner chain at `pkg/errs/errs.go:121-124`.
- `natsclient.IsKVNotFoundError` recognizes:
  - `ErrKVKeyNotFound`;
  - `jetstream.ErrKeyNotFound`;
  - `jetstream.ErrKeyDeleted`;
  at `natsclient/kv.go:603-629`.
- Project KV sentinels are declared at `natsclient/kv.go:648-653`.

#### Empty-bucket behavior

- Raw `bucket.Keys` returns `jetstream.ErrNoKeysFound` for a real empty bucket.
- Flow List wraps it transient at `flowstore/manager.go:164-168`.
- `KVStore.Keys` already normalizes `ErrNoKeysFound` to `nil, nil` at
  `natsclient/kv.go:493-508`, but Flow List bypasses it.
- HTTP List string-matches `"no keys found"` into `{"flows":[]}` at
  `service/flow_service.go:261-270`.
- Startup default import repeats that string match at `service/flow_service.go:125-130`.

#### Production FlowExecutor behavior

- `FlowExecutor.listFlows` returns a ToolResult error when `Manager.List` fails at
  `processor/agentic-tools/executors/flows.go:178-182`.
- `"No flows configured"` is reachable only after successful empty List at
  `processor/agentic-tools/executors/flows.go:183-184`.
- The real production `flowstore.Manager` returns an error for an empty bucket before that branch.
- The empty-list executor test uses only the fake at
  `processor/agentic-tools/executors/flows_test.go:212-235`.
- The fake returns an empty slice successfully at `processor/agentic-tools/executors/flows_test.go:84-95`.
- No production-manager-to-executor empty-bucket test was found.

HTTP callers therefore receive an empty list while `list_flows` receives a tool error for the same underlying state.

#### Sibling Pattern-B behavior

- Persona List uses `KVStore.Keys` and skips failed Gets at `persona/manager.go:142-161`.
- Flow-template List uses `KVStore.Keys` and skips failed Gets at `flowtemplate/manager.go:109-124`.
- Rule ConfigManager uses `KVStore.Keys`, logs and skips failed Get/unmarshal at
  `processor/rule/kv_config_integration.go:460-504`.

Those managers also skip corrupt records. Their behavior is precedent, not accepted Flow target state.

### 5. All Flow Get-to-HTTP projections

`flowstore.Manager.Get` currently collapses key absence, transport failure, timeout, and malformed stored JSON into an
error at `flowstore/manager.go:92-100`:

- KV read failures are wrapped transient at `:92-95`.
- JSON decode failures are wrapped fatal at `:97-100`.

All six HTTP consumers map every such error to 404:

1. Direct Flow GET:
   `service/flow_service.go:294-299`.
2. Explicit publication pre-read:
   `service/flow_service.go:342-346`.
3. Validate-without-body pre-read:
   `service/flow_service.go:453-458`.
4. Message observation pre-read:
   `service/flow_runtime_messages.go:64-71`.
5. Health observation pre-read:
   `service/flow_runtime_health.go:72-77`.
6. Metrics observation pre-read:
   `service/flow_runtime_metrics.go:97-109`.

Current consequences:

- missing key → 404;
- NATS transport failure → 404;
- deadline/timeout → 404;
- corrupt stored Flow JSON → 404.

The metrics endpoint emits a structured `RuntimeMetricsResponse` for the 404. Other boundaries use Flow-local JSON
errors or `http.NotFound`.

Downstream effects include:

- editor route load maps every direct-GET non-2xx, including these projected 404s, to `Flow not found`;
- Ops summary maps all three observation 404s to unavailable endpoint state;
- Health and Metrics tabs map observation failures to disconnected/error state;
- Messages tab maps the messages observation failure to a history-load error;
- model-facing `executeFlowStatus` maps every direct-GET HTTP 404 to the stable attachment code `FLOW_NOT_FOUND`.

### 6. Same-bucket, separate-manager topology

Flow HTTP and Flow agent tools do not share one `flowstore.Manager` instance.

#### FlowService manager

- `NewFlowServiceFromConfig` validates dependencies at `service/flow_service.go:55-74`.
- It constructs its own Manager with `flowstore.NewManager(deps.NATSClient)` at
  `service/flow_service.go:75-78`.
- That Manager is stored privately on `FlowService` at `service/flow_service.go:92-100`.

#### Production FlowExecutor manager

- Production tool registration independently calls `buildFlowManager` at
  `cmd/semstreams/main.go:227-252`, specifically `FlowManager` at `:245`.
- `buildFlowManager` independently calls `flowstore.NewManager` at
  `cmd/semstreams/main.go:707-717`.

#### E2E FlowExecutor manager

- The E2E binary independently supplies `FlowManager` at
  `cmd/e2e-semstreams/main.go:169-193`, specifically `:185`.
- Its `buildFlowManager` independently calls `flowstore.NewManager` at
  `cmd/e2e-semstreams/main.go:418-428`.

#### Shared authority

Every Manager constructor opens/creates the same `semstreams_flows` KV bucket at `flowstore/manager.go:26-45`,
specifically bucket name/config at `:32-37`.

The topology is therefore:

- one Manager instance privately owned by FlowService;
- a separate Manager instance privately owned by the FlowExecutor composition;
- both backed by the same NATS KV authority;
- no shared in-memory synchronization or error-policy object between them.

This explains why HTTP and tool consumers can project the same store behavior differently.

### 7. Adjacent version-concurrency claim

- Flow declares a version for optimistic concurrency at `flowstore/flow.go:18-20`.
- ADR-096 describes a compare-and-swap version at
  `docs/adr/096-flow-diagrams-are-not-lifecycle-authority.md:18-22`.
- OpenAPI advertises optimistic concurrency at `service/flow_service.go:223-226`.
- Current Update performs Get/version comparison then `KVStore.Put` at `flowstore/manager.go:119-145`.
- `KVStore.Put` is explicitly last-writer-wins at `natsclient/kv.go:184-198`.

Two simultaneous updates through either Manager instance can both pass the local comparison. This is adjacent existing
evidence, not presumed #1009 scope.

### 8. Complete current consumer inventory

#### SemStreams consumers

- HTTP routes: `service/flow_service.go:185-201`.
- Startup default import: `service/flow_service.go:123-151`.
- Six Get consumers listed above.
- Agentic `FlowManager`: `processor/agentic-tools/executors/flows.go:12-22`.
- Five Flow tools: `processor/agentic-tools/executors/flows.go:36-112`.
- Tool-to-manager calls: `processor/agentic-tools/executors/flows.go:136-228`.
- Tool registration:
  - `processor/agentic-tools/executors/register_flows.go:10-30`;
  - `processor/agentic-tools/executors/register.go:198-204`.
- Production and E2E manager composition listed above.

#### semstreams-ui Flow list route

- List route load calls `GET /flowbuilder/flows` at
  `/Users/coby/Code/c360/semstreams-ui/src/routes/flows/+page.ts:7-22`.
- A non-2xx becomes a user-facing error plus an empty Flow list at that file’s `:23-36`.
- Flow-list page imports `flowApi` at
  `/Users/coby/Code/c360/semstreams-ui/src/routes/flows/+page.svelte:1-7`.
- UI creation calls `flowApi.createFlow` at
  `/Users/coby/Code/c360/semstreams-ui/src/routes/flows/+page.svelte:20-32`.

#### semstreams-ui Flow editor route

- Editor load calls `GET /flowbuilder/flows/{id}` at
  `/Users/coby/Code/c360/semstreams-ui/src/routes/flows/[id]/+page.ts:7-9`.
- Every non-2xx becomes `Flow not found` at that file’s `:10-12`.
- It normalizes nodes/connections at `:14-26`.
- Editor save imports the timestamp-omitting `saveFlow` at
  `/Users/coby/Code/c360/semstreams-ui/src/routes/flows/[id]/+page.svelte:22-24`.
- Its update/error path is enumerated above.
- The editor’s production live-validation path builds its current unsaved definition at
  `/Users/coby/Code/c360/semstreams-ui/src/routes/flows/[id]/+page.svelte:234-241`.
- It POSTs to `/flowbuilder/flows/{id}/validate` at `:245-251`.
- Non-2xx responses return `null` at `:253-256`; transport, parse, and other exceptions also return `null` at
  `:258-263`.

#### semstreams-ui CRUD service

Full `flowApi` consumption is:

- Create: `src/lib/services/flowApi.ts:56-80`.
- List: `src/lib/services/flowApi.ts:82-97`.
- Get: `src/lib/services/flowApi.ts:99-114`.
- Update: `src/lib/services/flowApi.ts:116-140`.
- Delete: `src/lib/services/flowApi.ts:142-155`.

#### semstreams-ui Ops summary

- `opsSummaryApi.fetchSummary` fetches Flow List concurrently with health, graph, and trajectory summaries at
  `/Users/coby/Code/c360/semstreams-ui/src/lib/services/opsSummaryApi.ts:150-165`.
- `fetchFlowList` calls `GET /flowbuilder/flows` at
  `/Users/coby/Code/c360/semstreams-ui/src/lib/services/opsSummaryApi.ts:196-215`.
- List failure becomes `status: "unavailable"` with zero Flows at `:204-210`.
- Active Flow selection drives runtime observation at
  `/Users/coby/Code/c360/semstreams-ui/src/lib/services/opsSummaryApi.ts:273-320`.
- With an active Flow, Ops summary fetches all three observation endpoints concurrently at `:315-320`.
- Health observation request:
  `/Users/coby/Code/c360/semstreams-ui/src/lib/services/opsSummaryApi.ts:385-417`.
- Metrics observation request:
  `/Users/coby/Code/c360/semstreams-ui/src/lib/services/opsSummaryApi.ts:420-439`.
- Message observation request:
  `/Users/coby/Code/c360/semstreams-ui/src/lib/services/opsSummaryApi.ts:442-461`.
- Any non-2xx observation becomes an unavailable runtime endpoint at `:394-396`, `:429-431`, or `:451-453`.
- Since SemStreams maps every underlying Get failure to 404, Ops summary reports transport, timeout, corrupt storage,
  and actual absence through the same unavailable/not-found-shaped path.

#### semstreams-ui direct observation services and tabs

- `observationsApi` obtains generated health/metrics wire types at
  `/Users/coby/Code/c360/semstreams-ui/src/lib/services/observationsApi.ts:10-21`.
- `ObservationsApiError` preserves Flow ID and HTTP status at `:25-34`.
- Its shared reader selects health or metrics at `:36-42`, turns every non-2xx response into a status-only
  `ObservationsApiError` at `:44-50`, and returns JSON at `:52`.
- `fetchHealth` and `fetchMetrics` expose that reader at `:55-68`.
- `HealthTab` imports the service at
  `/Users/coby/Code/c360/semstreams-ui/src/lib/components/runtime/HealthTab.svelte:15-19`.
- It polls `fetchHealth` at `:30-40`; success updates connection and health state at `:41-44`, while any error marks
  the runtime disconnected and stores the error message at `:45-50`.
- `MetricsTab` imports the service at
  `/Users/coby/Code/c360/semstreams-ui/src/lib/components/runtime/MetricsTab.svelte:14-20`.
- It polls `fetchMetrics` at `:43-53`; success updates connection and metrics state at `:54-60`, while any error marks
  the runtime disconnected and stores the error message at `:61-66`.
- `messagesApi` defines `MessagesApiError`, preserving Flow ID, status, and parsed response detail, at
  `/Users/coby/Code/c360/semstreams-ui/src/lib/services/messagesApi.ts:21-31`.
- It calls the messages observation endpoint at `:33-48`.
- Every non-2xx response is parsed if possible and thrown as `MessagesApiError` at `:50-57`; success returns JSON at
  `:60`.
- `MessagesTab` imports `messagesApi` and `MessagesApiError` at
  `/Users/coby/Code/c360/semstreams-ui/src/lib/components/runtime/MessagesTab.svelte:23-26`.
- It calls `fetchMessages` at `:243-250`; `MessagesApiError` becomes its displayed message and all other errors become
  `Failed to load history` at `:261-267`.

The direct clients preserve an HTTP status, and the messages client additionally preserves a body. They cannot recover
the original backend cause after SemStreams has already projected every Flow Get failure as 404.

#### semstreams-ui model-facing validation and Flow status

- MCP `validate_flow` is defined at
  `/Users/coby/Code/c360/semstreams-ui/src/lib/server/mcp/tools.ts:79-123`.
- It sends a caller-provided Flow to the validation endpoint at `:105-114`.
- Any non-2xx becomes the generic exception `HTTP <status>: <statusText>` at `:116-118`; structured JSON is returned
  only on the 2xx path at `:120`.
- `MCPServer.validateFlow` delegates through the named tool at
  `/Users/coby/Code/c360/semstreams-ui/src/lib/server/mcp/server.ts:197-208`.
- `executeFlowStatus` is a separate model-facing GET consumer at
  `/Users/coby/Code/c360/semstreams-ui/src/lib/server/ai/toolExecutors.ts:336-377`.
- It calls `/flowbuilder/flows/{id}` at `:351-356`.
- Every HTTP 404 becomes an error attachment with code `FLOW_NOT_FOUND` at `:358-365`.
- Other non-2xx responses become `FLOW_STATUS_ERROR` at `:367-376`.
- Because SemStreams maps absence, NATS read failure, timeout, and corrupt stored JSON to HTTP 404, all four backend
  causes reach this model-facing consumer as `FLOW_NOT_FOUND`.

#### semstreams-ui E2E helpers

- Create-fixture documentation records missing-component opaque 500 at
  `/Users/coby/Code/c360/semstreams-ui/e2e/helpers/flow-setup.ts:136-169`.
- List helper fails on any non-2xx at
  `/Users/coby/Code/c360/semstreams-ui/e2e/helpers/backend-helpers.ts:57-80`.
- Orphan cleanup lists and then deletes, creating legitimate deletion churn at
  `/Users/coby/Code/c360/semstreams-ui/e2e/helpers/backend-helpers.ts:339-374`.

No new consumer is proposed; these are existing consumers of the current surface.

### 9. Tests and coverage gaps

Existing SemStreams tests:

- Structural validation classification:
  `flowstore/flow_test.go:24-66`.
- Normal manager CRUD/version:
  `flowstore/manager_integration_test.go:12-51`.
- Authoring-only HTTP CRUD:
  `service/flow_service_test.go:90-143`.
- Flow schema property presence:
  `service/flow_surface_test.go:43-67`.
- Fake-manager FlowExecutor:
  `processor/agentic-tools/executors/flows_test.go:15-95`.
- Fake empty-list behavior:
  `processor/agentic-tools/executors/flows_test.go:212-235`.
- Generic tool error conversion:
  `processor/agentic-tools/executors/flows_test.go:250-291`.

Existing semstreams-ui tests:

- Structured publication validation:
  `/Users/coby/Code/c360/semstreams-ui/e2e/publish-config.spec.ts:21-47`.
- `observationsApi` request paths and mocked 404 status preservation:
  `/Users/coby/Code/c360/semstreams-ui/src/lib/services/observationsApi.test.ts:22-70`.
- `messagesApi` mocked 404, 500, and 400 error projections:
  `/Users/coby/Code/c360/semstreams-ui/src/lib/services/messagesApi.test.ts:111-173`.
- MCP validation success/structured-result cases:
  `/Users/coby/Code/c360/semstreams-ui/src/lib/server/mcp/tools.test.ts:377-570`.
- MCP validation mocked non-2xx exception behavior:
  `/Users/coby/Code/c360/semstreams-ui/src/lib/server/mcp/tools.test.ts:660-741`.
- `executeFlowStatus` mocked 404 attachment behavior:
  `/Users/coby/Code/c360/semstreams-ui/src/lib/server/ai/toolExecutors.phase6.test.ts:578-616`.
- `executeFlowStatus` mocked transport and 500 behavior:
  `/Users/coby/Code/c360/semstreams-ui/src/lib/server/ai/toolExecutors.phase6.test.ts:619-664`.
- Health and Metrics component tests mock their observation service at:
  - `/Users/coby/Code/c360/semstreams-ui/src/lib/components/runtime/HealthTab.test.ts:71-72`;
  - `/Users/coby/Code/c360/semstreams-ui/src/lib/components/runtime/MetricsTab.test.ts:91-92`.
- MessagesTab exercises service invocation and displayed service errors at
  `/Users/coby/Code/c360/semstreams-ui/src/lib/components/runtime/MessagesTab.test.ts:1517-1543` and `:1858-1897`.

No in-repo test was found for:

- invalid Create HTTP status/body;
- invalid Update outside version mismatch;
- structural invalidity on `/validate`;
- missing versus transport/corrupt Get across all six HTTP consumers;
- omitted or forged `created_at`;
- real-manager empty bucket through FlowExecutor;
- deletion between `Keys` and `Get`;
- preserving non-not-found per-key failures while tolerating a vanished key;
- publication validation response schema;
- parity between the separately constructed HTTP and tool Manager consumers.

No inspected cross-repo test was found that traces:

- SemStreams structural-invalid 500 through the production editor validation path to `null`;
- SemStreams structural-invalid 500 through the MCP tool to its status-only exception;
- real SemStreams transport/timeout/corrupt-storage Get failure through a 404 into `ObservationsApiError`,
  `MessagesApiError`, Health/Metrics disconnected state, or Messages history error;
- real SemStreams transport/timeout/corrupt-storage Get failure through a 404 into model-facing `FLOW_NOT_FOUND`;
- differentiation of actual absence from those backend causes at any of those adopter paths.

The cited semstreams-ui tests mock HTTP or service outcomes after SemStreams’ cause-to-status projection. They verify
local mappings but cannot establish that the original backend cause was classified correctly.

Focused baseline:

```text
go test ./flowstore ./processor/agentic-tools/executors
ok github.com/c360studio/semstreams/flowstore
ok github.com/c360studio/semstreams/processor/agentic-tools/executors
```

### 10. Specs, ADRs, and active changes

- `openspec/specs/flow-authoring/spec.md:7-28` requires authoring-only CRUD and invalid Flow rejection before persistence.
- It does not specify:
  - HTTP error projection;
  - timestamp ownership;
  - empty-list parity;
  - concurrent-list behavior;
  - Get failure classification;
  - shared Manager topology.
- ADR-096 owns Flow authoring/audit:
  `docs/adr/096-flow-diagrams-are-not-lifecycle-authority.md:18-39`.
- ADR-029 owns Pattern-B CRUD:
  `docs/adr/029-instance-type-patterns.md:78-104`, `:127-157`.
- `openspec/specs/nats-kv-keys/spec.md:148-167` requires classification/constants rather than message parsing for its
  owned failures.
- That spec preserves existing KV boundaries until their owning changes at `:188-204`.
- These issues do not change Flow key construction.

Current active OpenSpec directories:

- `agentic-loop-evidence-integrity-condition`
- `align-standard-lifecycle-tests`
- `close-graph-index-partial-start-subscriptions`
- `cover-product-lesson-write-e2e`
- `own-lesson-curator-contract`

No Flow CRUD overlap was found.

The archived next-tag inventory mentions #1008–#1010 at
`openspec/changes/archive/2026-08-21-simplify-one-shot-lifecycle-ownership/next-tag-closeout-inventory.md:1560-1573`.
It was treated as an adjacent claim, not repository-first evidence.

### 11. PR #1052 collision

At inspected head `bb680f13d9e59db40f72649fee2951b19b42fd61`, PR #1052 touches:

- `agentic`;
- `message`;
- `processor/rule`;
- agentic configuration;
- rule documentation/tests;
- its rule-readable OpenSpec change.

It touches no:

- `flowstore`;
- FlowService;
- `engine`;
- generic HTTP gateway mapper;
- generated OpenAPI;
- semstreams-ui editor validation;
- semstreams-ui MCP tools;
- semstreams-ui observation services or tabs;
- semstreams-ui model-facing Flow status executor.

No semantic or file collision was found. Shared CI/test-load exposure is the only observed interaction.

## Adopter seam inventory

Concrete adopters:

- a semstreams-ui Flow-list developer;
- a semstreams-ui editor developer;
- a semstreams-ui runtime-observation developer;
- a semstreams-ui Ops-summary developer;
- a model/product author using SemStreams FlowExecutor;
- a model/product author using semstreams-ui MCP validation or Flow-status tools;
- an operator using validation/publication.

| Surface | What they must know today | If they do nothing | Discovery point | What they should need to know |
|---|---|---|---|---|
| Create validation | Required fields; 500 may mean invalid input | Permanent request is treated as server instability | Schema plus opaque runtime error | Only actual actionable outcome |
| Validate | Some findings return structured 200; structural failures return 500 | Similar mistakes have incompatible shapes | Observation of both paths | One discoverable contract |
| Editor live validation | `null` means non-2xx, transport failure, parse failure, or another exception—not a structured finding | Structural invalidity is indistinguishable from backend unavailability | Browser console and missing validation result | Actual typed outcome |
| MCP `validate_flow` | Only 2xx returns structured validation; every non-2xx becomes status text | A model cannot distinguish invalid input from server failure beyond status | Tool exception | Actual typed outcome |
| Publication validation | Useful 400 body is absent from OpenAPI | Generated clients cannot learn its shape | Hand-typed UI code | Published schema |
| Validation types | Three incompatible downstream spellings exist | Client checks can miss real backend shape | Separate local type files | One authoritative vocabulary |
| Update timestamp | Caller must echo stored `created_at` | Omission stores year 1; arbitrary value rewrites provenance | Shared schema, no ownership declaration | Nothing |
| Empty HTTP list | Handler recognizes raw SDK message | Works through message text | Runtime only | Nothing |
| Empty agent list | Real Manager errors; fake test says “No flows” | Model sees failure for normal empty authority | Tool error | Nothing |
| Concurrent list/delete | Snapshot key can vanish | Random 500/tool failure | Nowhere | Nothing |
| Direct Get | All failures become 404 | Infrastructure/corruption looks absent | 404 only | Actual typed failure |
| Editor route load | Every direct-GET non-2xx becomes `Flow not found` | Backend failure is presented as user-selected missing Flow | Route error | Actual typed failure |
| Direct observations clients | Status survives, but SemStreams already made all Get failures 404 | Actual absence, timeout, transport, and corruption share one client error status | Typed runtime error after information loss | Actual typed failure |
| Health/Metrics tabs | Any observation error means disconnected | Stored-data corruption or a transient read failure looks like disconnected runtime | UI error state | Actual typed failure |
| Messages tab | Any projected 404 becomes a history-load failure | Backend failure looks like missing/unavailable Flow history | UI error text | Actual typed failure |
| Observations/Ops | All Flow-read failures look unavailable/not found | Ops summary misattributes backend state | Endpoint status only | Actual typed failure |
| Model `executeFlowStatus` | Every HTTP 404 is encoded as `FLOW_NOT_FOUND` | Transport, timeout, or corrupt storage can cause the model to assert absence | Tool attachment | Actual typed failure |
| Manager topology | HTTP and tools use separate Manager handles | Consumers diverge despite same bucket | Internal composition files | Nothing |
| Editor error API | `APIError` has validation detail, but save throws plain Error | Structured detail becomes inaccessible | Source inspection | Stable typed response |

Timestamp handling asks the caller to predict and preserve a server-owned value already known by the framework.

List handling asks callers to predict whether a key snapshot will remain valid rather than observing a vanished key as
ordinary concurrent state.

The editor, observation, and model-facing paths do not predict backend causes themselves; they consume HTTP
classifications after the framework has collapsed distinct causes into the same status or null-shaped outcome.

## Current-owner collision table

No new durable, communication, or runtime-coordination primitive is proposed, so the mandatory proposed-primitive
collision table is not triggered.

| Dimension | Existing evidence |
|---|---|
| Semantic class | Flow authoring CRUD, validation, audit ownership, HTTP/tool projection, current-list observation |
| Owners | Two or more `flowstore.Manager` instances, `flowengine.Engine`/`Validator`, FlowService, KVStore, FlowExecutor |
| Catalogs | `semstreams_flows`; Flow OpenAPI schema; structured validation types |
| Status | HTTP status/body, ToolResult strings, Ops availability, runtimeStore connection/error, MCP exception, `FLOW_NOT_FOUND`; no Flow lifecycle status |
| Lifecycle | Create/Get/Update/Delete/List, validation, publication, History 10 |
| Ownership | Manager create-stamps audit fields; update accepts client `CreatedAt`; version check non-atomic |
| Readers | HTTP CRUD/observations, startup import, agent tools, Flow routes, editor live validation, CRUD clients, Ops summary, observation tabs, MCP validation, model Flow status |
| Writers | HTTP CRUD, agent tools, startup default import, editor save and validation request clients |
| Recovery | Bucket History 10; no Flow restore/replay API found |
| Failure handling | Classified errors, structured validation, custom/SDK sentinels, message substrings, multiple downstream type homes, editor `null`, typed observation errors after HTTP collapse, MCP status exception, model `FLOW_NOT_FOUND` |
| Topology | Separate HTTP/tool Manager handles over the same NATS KV authority |

## Searches closing empty categories

```text
rg -n 'errs\.IsInvalid\(' service --glob '*.go'
```

Result: no production Service consumer.

```text
rg -n 'ValidateFlowDefinition|ValidationResult|ValidationIssue' engine service --glob '*.go'
```

Result: structured validation owner and projections enumerated above.

```text
rg -n 'flowStore\.Get|flowstore\.Manager.Get' service --glob '*.go'
```

Result: all six HTTP Get consumers enumerated above.

```text
rg -n 'CreatedAt\s*=|CreatedAt:|created_at' \
  flowstore service processor/agentic-tools test/e2e/client schemas specs/openapi.v3.yaml
```

Result: no Update restoration.

```text
rg -n 'IsKVNotFoundError|ErrKVKeyNotFound|jetstream\.Err(KeyNotFound|NoKeysFound|KeyDeleted)|no keys found' \
  --glob '*.go'
```

Result: sentinel and empty-key behavior enumerated above.

```text
rg -n 'listFlows|No flows configured|Manager.List' \
  processor/agentic-tools/executors flowstore
```

Result: production error-before-empty and fake-only empty success enumerated above.

```text
rg -n 'flowbuilder/flows|flowApi|saveFlow|validation_result|ValidationResult|APIError' \
  /Users/coby/Code/c360/semstreams-ui/src \
  /Users/coby/Code/c360/semstreams-ui/e2e
```

Result: CRUD routes, Ops summary, validation spellings, and E2E consumers enumerated above.

```text
rg -n 'runFlowValidation|validate_flow|createValidateFlowTool|executeFlowStatus|FLOW_NOT_FOUND' \
  /Users/coby/Code/c360/semstreams-ui/src
```

Result: editor live validation, MCP validation, MCP server delegation, model-facing Flow status, and their tests
enumerated above.

```text
rg -n 'observationsApi|ObservationsApiError|messagesApi|MessagesApiError|fetchHealth|fetchMetrics|fetchMessages' \
  /Users/coby/Code/c360/semstreams-ui/src
```

Result: direct observation services, Health/Metrics/Messages tabs, Ops summary routes, and their tests enumerated above.

```text
rg -n 'flowstore.NewManager|buildFlowManager|FlowManager:' \
  service cmd processor/agentic-tools
```

Result: separate FlowService/tool Manager topology enumerated above.

```text
find openspec/changes -mindepth 1 -maxdepth 1 -type d -not -name archive -print
rg -n 'flow(author|store|builder)|/flows|created_at|ErrNoKeysFound|IsKVNotFound|errs.IsInvalid' \
  openspec/changes --glob '!archive/**'
```

Result: no active-change overlap.

```text
gh pr view 1052 --json headRefOid,files
```

Result: no Flow CRUD or downstream-adopter file overlap.

## Open evidence questions

- The relationship between structural `Flow.Validate` failures and existing `ValidationIssue` vocabulary is not
  specified.
- Safe external projection of uncoded classified errors remains unresolved:
  raw text exposes framework attribution; generic sanitization loses actionable detail.
- #1008’s scope does not yet rule on:
  - duplicate Create conflict;
  - invalid Update;
  - missing Update target;
  - missing Delete target;
  - validate structural errors;
  - six Get-to-404 collapses.
- Request and response share one Flow schema; schema separation is not presumed #1009 scope.
- `CreatedBy` remains client-preserved; timestamp ownership is not expanded to it.
- Non-atomic version checking conflicts with CAS claims but remains separate adjacent evidence.
- Corrupt-record list behavior must remain distinguishable from benign vanished-key behavior.
- HTTP and FlowExecutor disagree on real empty-bucket behavior.
- Publication validation has a current consumer but no OpenAPI response schema.
- semstreams-ui carries multiple incompatible validation/error type spellings.
- Editor live validation erases the distinction between non-2xx, transport failure, parse failure, and other
  exceptions by returning `null`.
- Model-facing MCP validation retains only HTTP status and status text on non-2xx.
- Direct observation services preserve status only after SemStreams has collapsed distinct Get failures to 404.
- Health/Metrics tabs convert all such observation errors into disconnected runtime state.
- MessagesTab preserves its service’s message but cannot recover the backend cause erased by the upstream 404.
- Model-facing `executeFlowStatus` maps every SemStreams-projected 404 to `FLOW_NOT_FOUND`.
- Separate HTTP/tool Manager handles share one bucket but not one in-memory policy owner.
- This replacement requires independent re-review before target-state or TDD grouping work begins.

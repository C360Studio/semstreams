# Flow CRUD release-blocker design: #1008, #1009, #1010

## Checkpoint and recommendation

- Baseline: `774c85dcf75bdce242f1f15ee2a5a310991ecf0d`.
- Accepted inventory: `docs/proposals/gh1008-1010-flow-crud-inventory.md`, SHA-256
  `815b0c256de362056f807245b0ff0495256f28c3d27d26249e65c10cd8903ac0`, `INVENTORY PASS`.
- Supersedes design SHA `fd9c98935dfa3b13c11bc9b96dc849f2d536e101cd290b945689c312c871e4af`.
- Status: **ACCEPTED — owner approved 2026-08-23**.
- Owner acceptance covers all twelve rulings in this artifact, including Option 2A, four pre-tag slices, CAS repair,
  the validation/error contracts, must-exist DELETE, mandatory downstream validation, and named E2E gates.

Recommended Option 2A: four sequential, independently reversible pre-tag slices:

1. #1009 timestamp ownership plus the already-promised atomic version fence.
2. #1010 current-state List semantics.
3. #1008 coherent invalid handling across Create, Update, Validate, publication, and DELETE.
4. A separately tracked truthful Get projection across all six read consumers.

Slice 4 shares the Flow error schema from #1008 but can be reverted without reverting actionable invalid responses.

## Measured premises

| Premise | Measurement |
|---|---|
| #1009 persistence owner | `flowstore/manager.go:119-145` |
| Audit fields already stored | `flowstore/flow.go:25-29` |
| Editor omits audit timestamps | semstreams-ui `src/lib/api/flows.ts:17-24,67-84`; editor `:503-524` |
| Atomic concurrency is promised | `flowstore/flow.go:18-20`; ADR-096 `:18-22`; `service/flow_service.go:223-226` |
| Update is unfenced | Get/check/Put at `flowstore/manager.go:119-145`; last-writer-wins Put at `natsclient/kv.go:184-198` |
| Revision fencing exists | `natsclient.KVStore.Update` and `ErrKVRevisionMismatch` at `natsclient/kv.go:200-223,648-653` |
| HTTP/tools use distinct Managers sharing one KV | `service/flow_service.go:55-100`; binary compositions; `flowstore/manager.go:26-45` |
| #1010 List owner | `flowstore/manager.go:163-180` |
| KV empty/vanished-key support exists | `natsclient/kv.go:493-508,603-629` |
| HTTP/tool empty behavior differs | `service/flow_service.go:261-270`; `processor/agentic-tools/executors/flows.go:178-184` |
| Invalid loss spans Create/Update/Validate | `service/flow_service.go:274-319,438-467` |
| Validation wire owner | `engine/validator.go:36-43,76-84` |
| Pattern connection returns error, not finding | `engine/validator.go:125-130` |
| Six reads map every Get error to 404 | FlowService plus messages/health/metrics handlers in the accepted inventory |
| UI Validate/MCP benefit from 200 structured invalidity | editor `:229-264`; MCP `tools.ts:79-123` |
| UI load still rewrites all backend non-2xx to 404 | editor `+page.ts:7-12` |
| Save/Create UIs do not render structured details | `flows.ts:67-84`; editor `:558-564`; list page `:20-32` |
| #1052 has no Flow overlap | inspected head `bb680f13d9e59db40f72649fee2951b19b42fd61` |

## Options

- **0 — do nothing:** preserves all three defects, false 404s, and a false CAS guarantee.
- **1 — literal issues:** Create-only invalid mapping, timestamp-only restoration, and narrow List fix. Small, but
  leaves Update/Validate opaque, false 404s, and CAS false.
- **2 — middle #1008 plus separate Get decision:** invalid submissions/publication share one vocabulary; reads retain
  a separate rollback. 2A makes both pre-tag; 2B defers Get only with an explicit owner acceptance. **2A chosen.**
- **3 — broad #1008:** one API mapper but one rollback spanning submissions, reads, observations, and UI seams.
- **4 — everything combined:** one oversized rollback and review boundary; rejected.

## Slice A: #1009 timestamps and revision-fenced Update

### Contract

- Create owns version and audit timestamps.
- Update reads current Flow plus KV revision; request logical version must match stored version.
- Persist with `KVStore.Update(ctx, key, bytes, observedRevision)`.
- Logical mismatch and `ErrKVRevisionMismatch` map to one typed Flow version conflict.
- Candidate copies the request, restores stored `CreatedAt`, increments stored version, and sets one `now` for both
  update timestamps.
- Caller input is untouched until commit succeeds; every failure leaves it deeply equal to its pre-call value.
- `CreatedBy` remains caller-preserved.

Sequence: validate → read value/revision → decode → logical compare → copy candidate → restore server fields → marshal
→ revision-fenced Update → classify revision conflict → success-only caller assignment.

### Two-Manager proof

Use real NATS and two Managers against one bucket. Explicitly hold both after reading the same revision but before
write. Exactly one succeeds and one conflicts; version advances once; stored content is the winner; loser content is
absent; loser input is unchanged; winner input changes only after commit. No sleeps/retry probability.

### Schemas and compatibility

- `FlowCreateRequest`: requires name, non-null nodes/connections; optional ID/description/created_by; no version or
  timestamps.
- `FlowUpdateRequest`: requires ID/version/name/non-null nodes/connections; optional description/created_by; no
  timestamps.
- `Flow`: requires ID/name/version/nodes/connections/all timestamps; description/created_by optional.

Legacy full-Flow bodies decode. Create ignores version/timestamps; Update uses version only as precondition and ignores
timestamps. Unknown fields remain ignored. Editor remains compatible but only shows a top-level conflict error.

### TDD and gate

Named tests: `TestManagerUpdatePreservesStoredCreatedAt`, `TestManagerUpdateIgnoresForgedCreatedAt`,
`TestManagerUpdateTwoManagersExactlyOneWins`, `TestManagerUpdateFailedWriteDoesNotMutateInput`,
`TestManagerUpdateSuccessMutatesInputAfterCommit`, `TestFlowUpdateRequestSchemaOmitsServerAuditFields`.

Capture RED for omitted/forged timestamps, two writers, and failed-write immutability. Isolated forced omissions of the
fence and copy-on-write must fail concurrency/immutability tests. Slice cannot merge without real-NATS proof.

Generate/commit only Slice A schema deltas, regenerate to no drift, then run lint, full repository race unit and
integration tests, build, and contract tests.

## Slice B: #1010 current-state List

### Contract

- Empty bucket → successful non-nil empty `[]*Flow`.
- Key deleted between enumeration/Get → omitted only on typed absence.
- Transport, permission, deadline, corrupt JSON, and other failures abort.
- No message substring; no new ordering promise.
- HTTP returns `{"flows":[]}` present/non-null; startup import accepts empty; real FlowExecutor returns exact
  `No flows configured` with no error attachment.

Actual adopter outcome: list UI/Ops/tool see normal empty state; ordinary deletion churn does not 500; real failures
remain errors.

Named tests: `TestManagerListEmptyBucketReturnsNonNilEmpty`, `TestManagerListSkipsOnlyVanishedKey`,
`TestManagerListPreservesPerKeyTransientFailure`, `TestManagerListPreservesCorruptRecordFailure`,
`TestFlowExecutorListFlowsRealManagerEmpty`, `TestHandleListFlowsEmptyResponseIsNonNullArray`, and
`TestEnsureDefaultFlowEmptyListUsesTypedOutcome`.

At least 14 assertions cover empty semantics, deterministic vanished key/survivor, real failures, HTTP array, tool,
startup, and schema. Capture baseline RED for real empty Manager/tool and vanished key; forced omission must fail the
three core regressions.

Introduce `FlowListResponse` with required non-null `flows: Flow[]`; generate/commit only Slice B deltas and run the
same full slice gate.

## Slice C: #1008 coherent invalid handling

### Scope and canonical vocabulary

Own structural vocabulary; Create/Update invalid/conflict; Validate result behavior; publication validation; DELETE
absence/failure; List failure projection; one Flow error schema; related OpenAPI. Six Get projections remain Slice D.

`ValidationResult` requires status and non-null errors, warnings, nodes, and discovered_connections. Status derives
errors → warnings → valid. `ValidationIssue` requires stable type, severity, non-empty component_name/message, and
non-null suggestions; port_name optional. Move its data definition to `flowstore`; keep `engine` alias.

Structural validation aggregates deterministically: Flow fields, nodes in input order, duplicate after required fields,
then connections in order. All are severity error:

| Condition | Stable type |
|---|---|
| Empty Flow ID/name | `flow_id_required` / `flow_name_required` |
| Empty node ID/component/type/name | `node_id_required` / `node_component_required` / `node_type_required` / `node_name_required` |
| Duplicate node ID | `duplicate_node_id` |
| Empty connection ID | `connection_id_required` |
| Empty source/target port | `connection_source_port_required` / `connection_target_port_required` |
| Unknown source/target node | `connection_source_node_unknown` / `connection_target_node_unknown` |

Component identity is deterministic: Flow ID or `(flow)`; node name then ID then index; connection ID then index.
First non-empty duplicate remains referenceable. Structural issues stop before graph work and yield non-null empty
node/connection arrays. Empty nodes remain saved-authoring-valid; Engine separately emits `empty_flow`.

Stable graph types: `empty_flow`, `graph_build_error`, `unknown_component`, `connection_pattern_error`,
`disconnected_node`, `orphaned_port`, `interface_mismatch`, `missing_interface`. Pattern errors become exactly one safe
error finding, status errors, non-null arrays, nil execution error. Compile then yields existing validation failure.

### HTTP contract

Validate: 200 `ValidationResult` for valid, warning, structural-invalid, graph-invalid, and pattern-error drafts; 400
for malformed JSON/ID mismatch; Slice D owns saved-flow pre-read; unexpected execution failure 500.

`FlowErrorResponse` requires non-empty `error`; optional, non-null `validation_result`. Result present for
Create/Update/publication validation and omitted otherwise. Exact messages:

| Outcome | Public error |
|---|---|
| Malformed body | `Invalid request body` |
| ID mismatch | `Flow ID does not match request path` |
| Validation | `Flow validation failed` |
| Existing ID | `Flow already exists` |
| Logical/revision conflict | `Flow version conflict` |
| Missing/deleted | `Flow not found` |
| Deadline/transient/fatal | `Flow storage request timed out` / `Flow storage temporarily unavailable` / `Internal server error` |

Create: 201; 400 malformed/validation; 409 existing; 503/504/500. Update: 200; 400 malformed/mismatch/validation;
404 missing; 409 conflict; 503/504/500.

List retains Slice B's successful empty/current-state behavior and projects preserved Manager failures through the
Slice C mapper: deadline → 504, transient storage → 503, corrupt/fatal/unknown → 500. Every failure uses a sanitized
`FlowErrorResponse`; raw NATS or stored malformed content remains log-only.

DELETE is must-exist: existing → 204 with no body/content-type; absent/repeated → 404; invalid direct ID → 400;
deadline/transient/fatal → 504/503/500. FlowExecutor Delete retains missing as error.

Publication: 200 progress; 400 validation with result; 404/503/504 pre-read; corrupt/internal 500 Flow error;
component-config persistence 500 retains progress response. OpenAPI 500 is `oneOf` both schemas.

### Actual adopter outcome

Editor live validation and MCP automatically consume structural-invalid 200 results. Editor save still displays only
the top-level error; Create UI displays status text while details are retained; publication UI already consumes result;
generated clients gain schemas. No claim that Create/Save UI renders issue detail.

### TDD and gate

Named tests cover structural contract/aggregation, non-null arrays, pattern error conversion, Create/Update projection,
Validate 200 versus malformed 400, must-exist Delete, publication response, List failure projection, and OpenAPI.
`TestHandleListFlowsFailureProjectionMatrix` table-tests deadline→504, transient→503, corrupt/fatal→500, sanitized
bodies, and preservation of the Manager's classified cause.

Minimum assertions cover all 12 types/order, no graph work, issue safety, pattern Validate 200/publication 400,
invalid mutations, typed conflicts, DELETE 204 then 404, shared schema, and sanitized 5xx.

Capture baseline RED for invalid Create/Update/Validate, pattern error, null arrays, and repeat Delete. Forced omissions
of opaque Create, fail-fast validation, returned pattern error, nil arrays, and string conflict must fail at least five
named tests.

Generate `ValidationIssue`, `ValidationResult`, `FlowErrorResponse`, and operation rows per Slice C; commit only its
deltas, regenerate to no drift, and run the full slice gate.

## Slice D: truthful Flow Get projection

Separate issue/change for direct GET, publication pre-read, Validate-without-body, and messages/health/metrics pre-read.
Reuse Slice C response and mapper. Typed absence → 404; deadline → 504; transient → 503; corrupt/unknown → 500.
Metrics never emits a normal success body for lookup failure.

Complete endpoint matrix after all slices:

| Operation | Statuses/schemas |
|---|---|
| List | 200 FlowListResponse; 503/504/500 Flow error |
| Create | 201 Flow; 400/409/503/504/500 Flow error |
| Get | 200 Flow; 404/503/504/500 Flow error |
| Update | 200 Flow; 400/404/409/503/504/500 Flow error |
| Delete | 204; 404/503/504/500 Flow error |
| Validate | 200 ValidationResult; 400/404/503/504/500 Flow error |
| Publish | 200 progress; 400/404/503/504 Flow error; 500 oneOf Flow/progress error |
| Messages/health/metrics | 200 route response; 404/503/504/500 Flow error |

`TestFlowOpenAPIResponseMatrix` asserts all 54 operation/status cells and schemas.

Actual adopter outcome: model status distinguishes true absence from backend failure; editor load still rewrites all
backend failures to local 404 and requires sister-owner correction; Ops/runtime tabs retain unavailable UX; code-level
clients preserve numeric statuses; generated clients get truthful contracts.

Named tests cover the six consumers × four classified failures: at least 24 status and 24 body assertions, plus metrics
not-success and OpenAPI. A private unexported Flow-store interface supplies deterministic failures. Baseline RED and
forced omission restoring unconditional 404 must each fail at least 12 transient/corrupt cases.

Generate only Slice D rows, regenerate to no drift, and run the full slice gate.

## Named E2E, schema, and downstream gates

Core E2E scenarios:

- `flow-authoring-http-contract`: 14 assertions for invalid Create/Validate, non-null result arrays, timestamps,
  must-exist Delete, and deleted Get; baseline fails 7.
- `flow-list-current-state`: 4 assertions for empty list and controlled churn/survivor; baseline fails empty semantics.
- `flow-get-corrupt-projection`: malformed stored bytes through List plus six Get-backed routes, each 500 Flow error
  without leakage; 14 status/body assertions, baseline fails all seven statuses.

Extend `task e2e:crud-tools` with `flow-crud-tools-empty`: completion, no error attachment, exact empty content;
3 assertions, baseline fails 2.

Keep `flow-update-two-manager-cas` as real-NATS integration: 9 assertions covering same revision, exactly-one-wins,
one increment, content, and caller mutation; baseline fails at least 4.

Schema proof: 54 endpoint/status cells; exact required sets for request/response/list/validation/error schemas;
non-null arrays; omitted server request fields; legacy body acceptance; 204 no body; publication 500 `oneOf`.
Regenerate and commit per slice, not only at the end.

Semstreams-ui validation by its owner is a mandatory tag gate against the candidate:

```text
npm run generate-types:check
npm run check
npm run test:unit
npx playwright test e2e/flow-crud.spec.ts e2e/publish-config.spec.ts
```

Evidence must cover validation, save/publication errors, model 404 versus 5xx, observation statuses, editor load not
rewriting 5xx to 404, timestamp preservation, and generated schemas. Editor-load correction requires sister-owner
work; SemStreams remains read-only.

Every slice independently runs lint, full repository race unit/integration, build, schema/no-drift, and contract tests.
The exact combined candidate repeats them plus core and CRUD-tools E2E. Report totals: HTTP 14/14, List 4/4, tools
3/3, corrupt Flow reads 14/14, CAS 9/9, OpenAPI 54/54, downstream 9/9. Generic green tiers are insufficient.

## OpenSpec, documentation, landing, and owner rulings

Update Flow-authoring truth for server timestamps/revision fencing, at-most-one competing Update, failed-input
immutability, current-state List, stable validation/non-null arrays, validation results, must-exist Delete, typed
mutation errors, and truthful reads.

Generate contracts per slice; add SemStreams-owned migration and release notes for request ownership, atomic CAS,
validation 200, Delete, status projection, regeneration, and editor-load migration. No NATS migration or new ADR:
Slice A conforms to ADR-096. Weakening CAS needs a superseding ADR.

Land A, B, C, D; run combined proof and downstream validation; tag only after evidence.

Owner rulings required:

1. Option 2A and four pre-tag slices.
2. CAS repair inside #1009.
3. Copy-on-write, success-only caller mutation.
4. Stable issue types and aggregate structural validation.
5. HTTP 200 for well-formed invalid drafts.
6. `connection_pattern_error` as validation finding.
7. Flow error response without a new machine-code field.
8. Must-exist DELETE.
9. Request-schema separation and legacy bodies.
10. Separately reversible but mandatory pre-tag Get projection.
11. Mandatory semstreams-ui candidate validation.
12. Named E2E scenarios and assertion totals.

No shared decision skill triggers: no new communication path, orchestration, payload, or query front door.

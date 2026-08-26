## ADDED Requirements

### Requirement: Structural validation is an aggregated, stable vocabulary owned by flowstore

`flowstore.Flow.Validate` SHALL evaluate every structural condition and SHALL return one classified `errs.ErrorInvalid`
error that carries a `*flowstore.ValidationError` whose `Result` lists every fault; it SHALL NOT stop at the first
fault. Each fault SHALL be one `ValidationIssue` of severity `error` whose `type` is the exported `flowstore`
constant for exactly one of these twelve conditions: empty Flow ID → `flow_id_required`; empty Flow name →
`flow_name_required`; empty node ID/component/type/name → `node_id_required` / `node_component_required` /
`node_type_required` / `node_name_required`; duplicate node ID → `duplicate_node_id`; empty connection ID →
`connection_id_required`; empty source/target port → `connection_source_port_required` /
`connection_target_port_required`; unknown source/target node → `connection_source_node_unknown` /
`connection_target_node_unknown`. The order SHALL be deterministic: Flow fields, then nodes in input order (required
fields, then the duplicate check), then connections in input order. `component_name` SHALL be the Flow ID or `(flow)`
for a Flow-level fault, the node name, else the node ID, else the zero-based index for a node fault, and the
connection ID, else the zero-based index for a connection fault; the first non-empty duplicate SHALL remain
referenceable. The result's `nodes` and `discovered_connections` SHALL be non-nil and empty because no graph work
runs; every issue's `suggestions` SHALL be non-nil. `ValidationError.Error()` SHALL name every issue's type,
component identity, and message so text-only consumers keep the detail. `Manager.Create` and `Manager.Update` SHALL
run `Validate` before any empty-ID guard so `flow_id_required` is reported as a finding; a nil Flow remains a plain
classified invalid error. An empty `nodes` array SHALL remain structurally valid.

#### Scenario: every structural fault is reported at once, in order, with its identity

- **GIVEN** a Flow with an empty name, a first node missing its component and type, a second node whose ID
  duplicates the first, and a connection with an empty source port that targets an unknown node
- **WHEN** `Validate` runs
- **THEN** it returns a non-nil error and the carried result lists exactly `flow_name_required`,
  `node_component_required`, `node_type_required`, `duplicate_node_id`, `connection_source_port_required`,
  `connection_target_node_unknown`, in that order, every one severity `error`
- **AND** the Flow-level issue names the Flow ID, the node issues name the node name (or ID, or index when those are
  empty), and the connection issues name the connection ID (or index)
- **AND** `nodes` and `discovered_connections` are non-nil and empty and every `suggestions` is non-nil
- **AND** a Flow with every one of the twelve faults yields twelve issues whose types are the twelve constants
- **AND** the test that verifies this is `TestFlowValidateAggregatesStructuralIssuesInOrder`

#### Scenario: the structural error is classified invalid and carries its result

- **GIVEN** any structurally invalid Flow
- **WHEN** a caller inspects the error from `Validate`
- **THEN** `errs.IsInvalid(err)` is true and `errs.IsTransient(err)` is false
- **AND** `errors.As(err, &ve)` for `ve *flowstore.ValidationError` succeeds and `ve.Result.Status` is `errors`
- **AND** `err.Error()` contains every issue's type and component identity
- **AND** the tests that verify this are `TestFlowValidateErrorCarriesResultAndIsInvalid` and the existing
  `TestFlowValidate`

#### Scenario: Create and Update return the structural result, empty ID included

- **GIVEN** a Flow whose ID is empty and whose only node has no component
- **WHEN** `Manager.Create` (and, for a saved Flow, `Manager.Update`) is called with it
- **THEN** the returned error carries a `*flowstore.ValidationError` whose result lists `flow_id_required` and
  `node_component_required`
- **AND** nothing is written to the bucket
- **AND** the test that verifies this is `TestManagerCreateAndUpdateReturnStructuralResult`

### Requirement: The validation result is one data definition with stable types and non-null arrays

`flowstore` SHALL own `ValidationResult`, `ValidationIssue`, `ValidatedNode`, `ValidatedPort`, and
`DiscoveredConnection`; `engine` SHALL keep each name as a type alias of the `flowstore` type, and
`engine.ValidationError` SHALL be an alias of `flowstore.ValidationError`. `ValidationResult` SHALL require
`validation_status`, `errors`, `warnings`, `nodes`, and `discovered_connections`, and all four arrays SHALL be non-nil
on every return path (empty Flow, build errors, pattern failure, success). `validation_status` SHALL derive errors →
warnings → valid. `ValidationIssue` SHALL require `type`, `severity` (`error` or `warning`), a non-empty
`component_name`, a non-empty `message`, and a non-nil `suggestions`; `port_name` SHALL be optional. `type` SHALL be
one of the twenty exported constants: the twelve structural types above and the eight graph types `empty_flow`,
`graph_build_error`, `unknown_component`, `connection_pattern_error`, `disconnected_node`, `orphaned_port`,
`interface_mismatch`, `missing_interface`; the engine SHALL emit through the constants, never a string literal. A
Flow-scoped graph finding (`empty_flow`, a registry-level `graph_build_error`, `connection_pattern_error`) SHALL carry
the Flow ID or `(flow)` as its `component_name`.

#### Scenario: every result has four non-null arrays and non-null suggestions

- **GIVEN** an empty Flow, a Flow whose node names an unknown component, and a valid Flow
- **WHEN** each is validated through `Engine.ValidateFlowDefinition`
- **THEN** each result's `errors`, `warnings`, `nodes`, and `discovered_connections` are non-nil
- **AND** every issue in every result has a non-nil `suggestions` and a non-empty `component_name`
- **AND** the raw JSON of each result carries `[]`, never `null`, for an empty array
- **AND** the test that verifies this is `TestValidationResultArraysAreNonNull`

#### Scenario: the engine names are aliases of the flowstore definition

- **WHEN** a caller compares `reflect.TypeOf(flowengine.ValidationResult{})` with
  `reflect.TypeOf(flowstore.ValidationResult{})`, and likewise for `ValidationIssue`, `ValidatedNode`,
  `ValidatedPort`, `DiscoveredConnection`, and `ValidationError`
- **THEN** each pair is the same type
- **AND** the test that verifies this is `TestEngineValidationTypesAreFlowstoreAliases`

#### Scenario: emitted types are the closed vocabulary

- **GIVEN** the twenty exported type constants
- **WHEN** they are collected
- **THEN** they are twenty distinct non-empty strings equal to the names above
- **AND** every issue emitted by the empty-Flow, unknown-component, pattern-failure, and structural cases has a
  `type` in that set
- **AND** the test that verifies this is `TestValidationIssueTypesAreTheClosedVocabulary`

### Requirement: A connection-pattern failure is one validation finding

`Validator.ValidateFlow` SHALL convert a classified invalid error from `FlowGraph.ConnectComponentsByPatterns` into
exactly one `ValidationIssue` of type `connection_pattern_error`, severity `error`, with the Flow identity as
`component_name`, the pattern error's text as `message`, and a non-nil `suggestions`; it SHALL set
`validation_status` to `errors`, SHALL return the result with all four arrays non-nil, and SHALL return a nil error.
`Engine.Compile` SHALL then fail with the existing `ValidationError` because the result has errors.

#### Scenario: a pattern conflict is a finding, not an execution error

- **GIVEN** a Flow whose components produce a connection-pattern conflict (two network ports on the same address)
- **WHEN** the Flow is validated
- **THEN** the error is nil, `validation_status` is `errors`, and `errors` holds exactly one issue of type
  `connection_pattern_error` naming the Flow identity
- **AND** `nodes`, `discovered_connections`, and `warnings` are non-nil
- **AND** `Engine.Compile` of the same Flow returns a `ValidationError` carrying that result
- **AND** the tests that verify this are `TestValidatorPatternErrorBecomesFinding` and
  `TestHandleValidateFlowPatternErrorIsOK`

### Requirement: Validate returns 200 for every well-formed draft

`Engine.ValidateFlowDefinition` SHALL return a structurally invalid Flow's aggregated result with a nil error; only
a nil Flow or a validator execution failure SHALL be an error. `POST /flows/{id}/validate` with a body SHALL respond
`200` with the `ValidationResult` for a valid, warning, structural-invalid, graph-invalid, or pattern-error draft;
`400 {"error":"Invalid request body"}` for a body that does not decode; `400 {"error":"Flow ID does not match
request path"}` when the body's ID differs from the path; and `500` through the Flow error mapper for an execution
failure. Neither `400` body SHALL echo decoder text or either ID. The saved-Flow pre-read path is unchanged by this
requirement.

#### Scenario: a structurally invalid draft is a 200 with findings

- **GIVEN** a draft whose node has no component
- **WHEN** it is posted to `POST /flows/{id}/validate`
- **THEN** the status is `200` and the body decodes into a fresh `ValidationResult` with `validation_status`
  `errors` and one `node_component_required` issue
- **AND** the raw body's `nodes` and `discovered_connections` members are `[]`
- **AND** the tests that verify this are `TestHandleValidateFlowStructuralInvalidIsOK` and
  `TestEngineValidateFlowDefinitionStructuralInvalidIsResult`

#### Scenario: malformed and mismatched bodies are exact 400s

- **WHEN** a client posts a body that does not decode, or a body whose `id` differs from the path
- **THEN** the status is `400` and the body decodes into a fresh `FlowErrorResponse` whose `error` is exactly
  `Invalid request body`, or exactly `Flow ID does not match request path`, with no `validation_result` member
- **AND** the tests that verify this are `TestHandleValidateFlow_InvalidJSON` and `TestHandleValidateFlow_IDMismatch`

### Requirement: Flow HTTP failures project through one classified mapper into FlowErrorResponse

The saved-flow HTTP contract SHALL declare `FlowErrorResponse` with a required non-empty `error` string and an
optional, non-null `validation_result` (`ValidationResult`) that is present for Create, Update, and publication
validation failures and omitted on every other failure. One mapper SHALL decide status and public message for every
Flow-path error by classification or sentinel, in this order, never by message text: a `*flowstore.ValidationError`
→ `400` `Flow validation failed` with the result; `errors.Is(err, errs.ErrRevisionMismatch)` → `409` `Flow version
conflict`; `errors.Is(err, natsclient.ErrKVKeyExists)` → `409` `Flow already exists`;
`errors.Is(err, natsclient.ErrKVKeyNotFound)` → `404` `Flow not found`; `errors.Is(err, context.DeadlineExceeded)` →
`504` `Flow storage request timed out`; other `errs.IsInvalid` → `400`; `errs.IsTransient` → `503` `Flow storage
temporarily unavailable`; everything else → `500` `Internal server error`. A body that does not decode SHALL be `400`
`Invalid request body`; an Update whose body ID differs from the path SHALL be `400` `Flow ID does not match request
path`. Raw NATS text, framework attribution, and stored malformed content SHALL be log-only. Create SHALL respond
`201`, `400`, `409`, `503`, `504`, or `500`; Update `200`, `400`, `404`, `409`, `503`, `504`, or `500`.

#### Scenario: the mapper's class matrix

- **GIVEN** one error per row: a `ValidationError`; a `flowstore` typed version conflict; a `WrapInvalid` carrying
  `natsclient.ErrKVKeyExists`; a `WrapTransient` carrying `natsclient.ErrKVKeyNotFound`; a `WrapTransient` carrying
  `context.DeadlineExceeded`; a `WrapTransient` of a connection failure; a `WrapFatal`; an unclassified error
- **WHEN** each is projected
- **THEN** the statuses are `400`, `409`, `409`, `404`, `504`, `503`, `500`, `500` and the messages are the exact
  table entries; only the first row carries `validation_result`; no message contains the input error's text
- **AND** the test that verifies this is `TestFlowErrorMapperClassMatrix`

#### Scenario: an invalid Create is a 400 that carries the findings

- **GIVEN** a `POST /flows` body whose node has no `component`
- **WHEN** the request is sent
- **THEN** the status is `400`, the body decodes into a fresh `FlowErrorResponse` with `error` exactly
  `Flow validation failed` and a `validation_result` whose `errors` holds one `node_component_required` issue
- **AND** nothing was saved; a subsequent `GET /flows` lists no Flow
- **AND** a body that does not decode is `400` `Invalid request body` with no `validation_result`
- **AND** the test that verifies this is `TestHandleCreateFlowInvalidIsBadRequestWithResult`

#### Scenario: creating an existing ID is a 409

- **GIVEN** a saved Flow
- **WHEN** `POST /flows` is sent again with the same `id`
- **THEN** the status is `409` and the body's `error` is exactly `Flow already exists`
- **AND** the stored record is unchanged
- **AND** the test that verifies this is `TestHandleCreateFlowExistingIDIsConflict`

#### Scenario: an invalid Update is a 400 that carries the findings

- **GIVEN** a saved Flow and a `PUT /flows/{id}` body at the current version whose node has no `component`
- **WHEN** the request is sent
- **THEN** the status is `400`, `error` is exactly `Flow validation failed`, and `validation_result.errors` holds one
  `node_component_required` issue
- **AND** the stored record is unchanged and still at its version
- **AND** the test that verifies this is `TestHandleUpdateFlowInvalidIsBadRequestWithResult`

#### Scenario: updating a missing Flow is a 404

- **GIVEN** no saved Flow with the path ID
- **WHEN** a well-formed `PUT /flows/{id}` is sent
- **THEN** the status is `404` and `error` is exactly `Flow not found`
- **AND** the test that verifies this is `TestHandleUpdateFlowMissingIsNotFound`

#### Scenario: the result member is absent unless a result was attached

- **WHEN** the mapper projects an error with no result
- **THEN** the raw JSON body has no `validation_result` member
- **WHEN** it projects a `ValidationError`
- **THEN** the raw body's `validation_result` is an object, never `null`
- **AND** the test that verifies this is `TestFlowErrorResponseOmitsAbsentResult`

### Requirement: DELETE is must-exist

`flowstore.Manager.Delete` SHALL read the record before deleting and SHALL return an error carrying
`natsclient.ErrKVKeyNotFound` when the key is absent or tombstoned; no read-then-delete fence is promised, so two
deleters that both observed the record both succeed. `DELETE /flows/{id}` SHALL respond `204` with no body and no
`Content-Type` when the Flow existed, `404` `Flow not found` when it is absent or already deleted, and `504`, `503`,
or `500` through the mapper otherwise. The FlowExecutor `delete_flow` tool over a real Manager SHALL report a missing
Flow as a tool error.

#### Scenario: delete once, then 404

- **GIVEN** a saved Flow
- **WHEN** `DELETE /flows/{id}` is sent
- **THEN** the status is `204`, the body is empty, and no `Content-Type` header is set
- **WHEN** the same request is sent again
- **THEN** the status is `404` and the body decodes into a fresh `FlowErrorResponse` with `error` exactly
  `Flow not found`
- **AND** `GET /flows/{id}` is `404`
- **AND** the test that verifies this is `TestHandleDeleteFlowMustExist`

#### Scenario: the Manager reports absence as typed absence

- **GIVEN** a never-created ID and a deleted ID
- **WHEN** `Manager.Delete` is called with each
- **THEN** each returns a non-nil error for which `errors.Is(err, natsclient.ErrKVKeyNotFound)` is true
- **AND** the test that verifies this is `TestManagerDeleteMissingReportsTypedAbsence`

#### Scenario: the delete_flow tool reports a missing Flow as an error

- **GIVEN** a FlowExecutor over a real `flowstore.Manager` and an ID that was never saved
- **WHEN** `delete_flow` is executed
- **THEN** the result's `Error` is non-empty and its `Content` does not claim deletion
- **AND** the test that verifies this is `TestFlowExecutorDeleteMissingIsError`

### Requirement: List failures project by class

`GET /flows` SHALL keep Slice B's `200` behaviour and SHALL project a `Manager.List` failure through the Flow error
mapper: a deadline → `504`, another transient failure → `503`, a record that does not decode or any other failure →
`500`, each a sanitized `FlowErrorResponse` with no `validation_result`; the class the Manager assigned SHALL be the
class projected.

#### Scenario: the List failure projection matrix

- **GIVEN** three requests: one under an already-expired deadline, one under a cancelled context, and one against a
  bucket holding a record whose bytes are not Flow JSON
- **WHEN** each `GET /flows` is served
- **THEN** the statuses are `504`, `503`, and `500`, the `error` members are exactly `Flow storage request timed out`,
  `Flow storage temporarily unavailable`, and `Internal server error`, no body carries `validation_result`, and no
  body contains the words `nats`, `invalid character`, or `flowstore`
- **AND** the corrupt-record row is `500`, not `503`: the fatal class the Manager assigned survives to the status
- **AND** the test that verifies this is `TestHandleListFlowsFailureProjectionMatrix`

### Requirement: Publication responses carry the shared error schema

`POST /flows/{id}/publish-component-configs` SHALL respond `200` with the progress body on success; `400
{"error":"Flow validation failed","validation_result":{…}}` with a present, non-null result whenever `Compile`
returned a result and an error — structural findings included, now that `ValidateFlowDefinition` returns them as a
result; `500` `FlowErrorResponse` through the mapper when `Compile` failed with no result; and the existing `500`
progress body when component-config persistence fails. The pre-read keeps its current `404` `Flow not found`, whose
body already has the `FlowErrorResponse` shape. The OpenAPI `500` SHALL be `oneOf` `FlowErrorResponse` and
`publishComponentConfigsResponse`.

#### Scenario: a saved Flow that fails graph validation publishes as 400 with findings

- **GIVEN** a saved Flow whose node names an unknown component
- **WHEN** publication is requested
- **THEN** the status is `400`, `error` is exactly `Flow validation failed`, and `validation_result.errors` holds an
  `unknown_component` issue
- **AND** no component configuration was written
- **AND** the test that verifies this is `TestHandlePublishComponentConfigsInvalidIsBadRequestWithResult`

#### Scenario: a stored record that is structurally invalid publishes as 400 with a non-null result

- **GIVEN** a record written directly to the bucket whose node has no component
- **WHEN** publication is requested
- **THEN** the status is `400` and the raw body's `validation_result` is an object holding a
  `node_component_required` issue, never `null`
- **AND** the test that verifies this is `TestHandlePublishComponentConfigsStructuralInvalidHasResult`

### Requirement: OpenAPI declares the validation and error schemas and the Slice C operation rows

The generated OpenAPI SHALL declare `ValidationResult` (required exactly `validation_status`, `errors`, `warnings`,
`nodes`, `discovered_connections`), `ValidationIssue` (required exactly `type`, `severity`, `component_name`,
`message`, `suggestions`; `port_name` optional), and `FlowErrorResponse` (required exactly `error`;
`validation_result` present as a property with no `anyOf`/null alternative). It SHALL declare `POST /flows`
201/400/409/503/504/500, `PUT /flows/{id}` 200/400/404/409/503/504/500, `DELETE /flows/{id}` 204/404/503/504/500,
`GET /flows` 200/503/504/500, `POST /flows/{id}/validate` 200 `ValidationResult`/400/404/500, and
`POST /flows/{id}/publish-component-configs` 200/400/404/500, every non-2xx referencing `FlowErrorResponse` except the
publication `500`, which SHALL be `oneOf` `FlowErrorResponse` and `publishComponentConfigsResponse`. The schema
generator SHALL treat an `omitzero` field as optional, and `service.ResponseSpec` SHALL be able to name more than
one schema for a response, rendered as `oneOf`.

#### Scenario: the schemas and rows are generated from the registered types

- **WHEN** the Flow OpenAPI fragment and the three schemas are generated
- **THEN** the required sets are exactly as stated, `FlowErrorResponse.validation_result` carries no `anyOf`, the
  three types are in `ResponseTypes`, every listed operation/status cell carries the stated `SchemaRef`, and the
  publication `500` names both schemas
- **AND** the test that verifies this is `TestFlowOpenAPIErrorAndValidationSchemas`

#### Scenario: an omitzero field is optional in the generated schema

- **GIVEN** a struct with a value-typed field tagged `json:"x,omitzero"`
- **WHEN** `SchemaFromType` runs
- **THEN** `x` is a property and is not in `required`
- **AND** the test that verifies this is `TestSchemaFromTypeTreatsOmitzeroAsOptional`

#### Scenario: a multi-schema response renders as oneOf

- **GIVEN** a `ResponseSpec` naming two schema refs
- **WHEN** the OpenAPI document is built
- **THEN** the response's `application/json` schema is a `oneOf` of exactly those two `$ref`s and the dangling-ref
  check resolves both
- **AND** the test that verifies this is `TestOpenAPIBuilderRendersOneOfResponse`

## MODIFIED Requirements

### Requirement: Saved Flow mutations are authoring-only

Flow create, read, update, and delete SHALL operate on saved authoring data in flowstore. Create and update SHALL
retain the existing validation behavior. Saving or updating a Flow SHALL NOT publish component configuration or mutate
the running process.

Flowstore SHALL NOT persist or claim current component lifecycle state, runtime activation, or current runtime
membership.

#### Scenario: Save does not publish

- **GIVEN** a valid Flow diagram
- **WHEN** an author creates or updates the saved Flow
- **THEN** flowstore contains the authoring change
- **AND** no component configuration write occurs
- **AND** the running process remains unchanged

#### Scenario: Invalid Flow is rejected before persistence

- **WHEN** a Flow fails structural validation on create or update
- **THEN** the author receives `400` with a `FlowErrorResponse` whose `error` is `Flow validation failed` and whose
  `validation_result` lists every structural fault
- **AND** neither flowstore nor component configuration changes
- **AND** the tests that verify this are `TestHandleCreateFlowInvalidIsBadRequestWithResult` and
  `TestHandleUpdateFlowInvalidIsBadRequestWithResult`

### Requirement: Concurrent Updates are revision-fenced and exactly one wins

`Manager.Update` SHALL read the stored value together with its KV revision and SHALL persist with a revision-fenced
write (`natsclient.KVStore.Update` with the observed revision), never with an unfenced Put. When two or more Updates
observe the same revision — through one Manager or through separate Managers over the same bucket — exactly one SHALL
commit and every other SHALL fail with the typed version conflict; the stored version advances exactly once and the
stored content is the winner's.

A logical version mismatch and a KV revision mismatch (`natsclient.ErrKVRevisionMismatch`) SHALL surface as one typed
conflict: a classified `errs.ErrorInvalid` error carrying code `revision_mismatch`, such that
`errors.Is(err, errs.ErrRevisionMismatch)` is true and `errs.IsTransient(err)` is false. No new exported sentinel is
introduced. Callers SHALL branch on that classification, never on message text.

`PUT /flows/{id}` SHALL respond `409` when Update returns the typed conflict, determined by classification. The
response body SHALL be a `FlowErrorResponse` whose `error` is exactly `Flow version conflict` and which carries no
`validation_result`; the framework's attribution text SHALL NOT appear in the body.

#### Scenario: two Managers observe the same revision and exactly one wins

- **GIVEN** two `flowstore.Manager` instances over one real NATS `semstreams_flows` bucket
- **AND** both have read the same saved Flow at the same KV revision and are held before their write
- **WHEN** both are released
- **THEN** exactly one Update returns nil and the other returns the typed version conflict
- **AND** the stored version advanced by exactly one
- **AND** the stored content is the winner's and contains none of the loser's changes
- **AND** no sleep or retry probability is relied on; the hold and release are explicit synchronization
- **AND** the test that verifies this is `TestManagerUpdateTwoManagersExactlyOneWins`

#### Scenario: logical mismatch and revision mismatch are one typed conflict

- **GIVEN** an Update that fails the logical version comparison
- **AND** an Update that passes the comparison but loses the revision-fenced write
- **WHEN** a caller inspects either error
- **THEN** `errors.Is(err, errs.ErrRevisionMismatch)` is true for both
- **AND** `errs.IsInvalid(err)` is true and `errs.IsTransient(err)` is false for both
- **AND** the tests that verify this are `TestManagerUpdateTwoManagersExactlyOneWins` and
  `TestManagerDiagramCRUDAndVersioning`

#### Scenario: HTTP projects the typed conflict as 409 by classification

- **GIVEN** a saved Flow whose stored version has advanced past the version a client holds
- **WHEN** the client sends `PUT /flows/{id}` with its stale version
- **THEN** the response status is `409`
- **AND** the body decodes into a fresh `FlowErrorResponse` whose `error` is exactly `Flow version conflict`
- **AND** the handler determined that status from the error's classification, not from its message text
- **AND** the test that verifies this is `TestFlowCRUDDoesNotPublishAndExplicitPublicationRetriesThroughConfigManager`

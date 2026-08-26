# flow-authoring Specification

## Purpose
Defines saved Flow diagrams as authoring artifacts and explicit, next-boot-only publication of compiled component
configuration.
## Requirements
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

- **WHEN** a Flow fails the existing validation contract
- **THEN** the author receives the validation failure
- **AND** neither flowstore nor component configuration changes

### Requirement: Component configuration publication is explicit and next-boot-only

`POST /flows/{id}/publish-component-configs` SHALL load the saved Flow, apply the existing validator and compiler,
sort compiled component instance names, and call the existing Config Manager component write operation sequentially.

Publication SHALL be upsert-only. A component omitted from the compiled Flow SHALL NOT cause deletion of an existing
component configuration. Publication SHALL NOT mutate the running component set or automatically restart the process.

#### Scenario: Successful publication reports observed outcome

- **GIVEN** a valid saved Flow that compiles to component instances B and A
- **WHEN** the author explicitly publishes component configuration
- **THEN** Config Manager receives upserts for A and then B
- **AND** the response reports A and B as persisted
- **AND** the response reports the running process unchanged and reboot required

#### Scenario: Saving alone never publishes

- **GIVEN** a valid saved Flow that has not been explicitly published
- **WHEN** the process continues running or later reads the Flow
- **THEN** no component configuration is inferred from the saved authoring record

#### Scenario: Omission does not delete

- **GIVEN** an existing component configuration for A
- **AND** a saved Flow compiles without A
- **WHEN** the Flow is explicitly published
- **THEN** publication does not delete A
- **AND** any desired removal is handled outside this upsert-only operation

### Requirement: Partial publication reports exact retry-safe progress

If a sequential component write fails, publication SHALL stop and report the exact sorted prefix already persisted and
the component instance whose write failed. It SHALL NOT report unattempted instances as persisted. Retrying the same
publication SHALL be safe because every attempted operation is an upsert of the same compiled configuration.

#### Scenario: Middle write fails

- **GIVEN** a valid Flow compiling to sorted instances A, B, and C
- **AND** Config Manager accepts A and rejects B
- **WHEN** publication runs
- **THEN** the response reports persisted instances `[A]`
- **AND** it reports B as the failed instance
- **AND** it does not report C as persisted
- **AND** retry may safely upsert A again before retrying B

### Requirement: Flow lifecycle surfaces are absent

Flow runtime lifecycle state, operations, agent tools, metrics, timestamps, logs, and streams SHALL NOT exist. Retired
surfaces SHALL have no compatibility aliases and no replacement monitor.

Name-keyed Flow health, metrics, or message observations MAY remain when they report current component observations.
Their presence SHALL NOT claim Flow ownership of component lifecycle, runtime activation, or authoring publication.

#### Scenario: Lifecycle operation is not routed

- **WHEN** a caller attempts a retired Flow runtime lifecycle operation
- **THEN** no supported HTTP or agent-tool route provides that operation
- **AND** the caller uses authoring CRUD, optional explicit publication, and process supervision instead

#### Scenario: Observation does not imply lifecycle ownership

- **GIVEN** a saved Flow whose component names match current runtime observations
- **WHEN** a caller reads retained Flow observation data
- **THEN** the response reports only those observations
- **AND** it does not claim the Flow authoring record activated or owns the components

### Requirement: Update owns the audit timestamps and treats the request version as a precondition

`flowstore.Manager.Update` SHALL restore `CreatedAt` from the stored record, SHALL set `Version` to the stored
version plus one, and SHALL assign one server-observed instant to both `UpdatedAt` and `LastModified`. The request's
`created_at`, `updated_at`, and `last_modified` values SHALL NOT be persisted, whether omitted or supplied. The
request's `version` SHALL be compared to the stored version as a precondition and SHALL NOT be persisted as sent.
`CreatedBy` SHALL be persisted exactly as the caller supplied it; the framework does not restore it from the stored
record.

#### Scenario: an omitted created_at is restored from the stored record

- **GIVEN** a saved Flow created at time T0
- **WHEN** an author updates it with a body carrying no `created_at`
- **THEN** the stored record's `created_at` is still T0
- **AND** the returned Flow reports `created_at` T0
- **AND** the test that verifies this is `TestManagerUpdatePreservesStoredCreatedAt`

#### Scenario: a supplied created_at is ignored

- **GIVEN** a saved Flow created at time T0
- **WHEN** an author updates it with `created_at` set to any other value
- **THEN** the stored record's `created_at` is still T0
- **AND** the test that verifies this is `TestManagerUpdateIgnoresForgedCreatedAt`

#### Scenario: update timestamps are one server instant

- **GIVEN** a saved Flow
- **WHEN** an Update commits
- **THEN** the stored `updated_at` and `last_modified` are equal to each other
- **AND** that instant is server-observed: the `updated_at`/`last_modified` values the request carried are not what was
  stored (one server instant is the accepted guarantee; monotonicity against a prior stored value is not promised)
- **AND** the test that verifies this is `TestManagerUpdateSuccessMutatesInputAfterCommit`

#### Scenario: a stale logical version is rejected without a write

- **GIVEN** a saved Flow at version 2
- **WHEN** an author updates it with `version` 1
- **THEN** Update returns the typed version conflict
- **AND** the stored record is unchanged and still at version 2
- **AND** the tests that verify this are `TestManagerDiagramCRUDAndVersioning` and
  `TestManagerUpdateFailedWriteDoesNotMutateInput`

#### Scenario: created_by is caller-preserved

- **GIVEN** a saved Flow whose stored `created_by` is `a`
- **WHEN** an author updates it with `created_by` `b`
- **THEN** the stored record's `created_by` is `b`
- **AND** the test that verifies this is `TestManagerUpdatePreservesStoredCreatedAt`

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
response body remains a JSON object with an `error` string; its exact text is not specified by this requirement.

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
- **AND** the handler determined that status from the error's classification, not from its message text
- **AND** the test that verifies this is `TestFlowCRUDDoesNotPublishAndExplicitPublicationRetriesThroughConfigManager`

### Requirement: Update leaves the caller's Flow untouched until commit

`Manager.Update` SHALL build its persisted candidate as a copy of the caller's `*Flow`. On every failure — nil or
empty-ID input, structural validation, read failure, decode failure, logical version mismatch, marshal failure, or a
failed fenced write — the caller's value SHALL be deeply equal to its pre-call value. Only after the fenced write
succeeds SHALL Update assign the committed record to the caller's value, so the caller observes the restored
`CreatedAt`, the incremented `Version`, and the server timestamps exactly when the store does. A stored record that
does not decode SHALL be classified fatal (`errs.IsFatal`), never transient, matching `Manager.Get`.

#### Scenario: a stored record that does not decode is fatal and does not mutate the input

- **GIVEN** a saved Flow whose stored bytes are not valid Flow JSON
- **WHEN** an author updates it
- **THEN** Update returns an error for which `errs.IsFatal` is true and `errs.IsTransient` is false
- **AND** the caller's `*Flow` is deeply equal to the value passed in
- **AND** the test that verifies this is `TestManagerUpdateFailedWriteDoesNotMutateInput/decode_failure_on_a_corrupt_record`

#### Scenario: a failed write does not mutate the input

- **GIVEN** an Update whose persist step fails (a lost revision fence or an unavailable store)
- **WHEN** Update returns its error
- **THEN** the caller's `*Flow` is deeply equal to the value passed in, including `Version` and every timestamp
- **AND** the test that verifies this is `TestManagerUpdateFailedWriteDoesNotMutateInput`

#### Scenario: the loser of a concurrent Update keeps its input

- **GIVEN** the two-Manager race above
- **WHEN** the losing Update returns the typed conflict
- **THEN** the loser's `*Flow` is deeply equal to its pre-call value
- **AND** the test that verifies this is `TestManagerUpdateTwoManagersExactlyOneWins`

#### Scenario: a successful Update mutates the input only after commit

- **GIVEN** an Update held immediately before its fenced write
- **WHEN** it is held
- **THEN** the caller's `*Flow` is still deeply equal to its pre-call value
- **WHEN** it is released and the write commits
- **THEN** the caller's `*Flow` equals the stored record: stored `CreatedAt`, previous version plus one, and equal
  `UpdatedAt` and `LastModified`
- **AND** the test that verifies this is `TestManagerUpdateSuccessMutatesInputAfterCommit`

### Requirement: Flow create and update request schemas omit server-owned fields and legacy bodies keep decoding

The saved-flow HTTP contract SHALL declare `FlowCreateRequest` as the `POST /flows` request body and
`FlowUpdateRequest` as the `PUT /flows/{id}` request body in the generated OpenAPI. `FlowCreateRequest` SHALL require
`name`, `nodes`, and `connections`, SHALL allow optional `id`, `description`, and `created_by`, and SHALL declare no
`version` and no timestamp properties. `FlowUpdateRequest` SHALL require `id`, `version`, `name`, `nodes`, and
`connections`, SHALL allow optional `description` and `created_by`, and SHALL declare no timestamp properties. `Flow`
SHALL remain the response schema for create and update and the request schema for the validate draft.

Both handlers SHALL continue to decode a legacy full-`Flow` body: unknown fields are ignored, create ignores
`version` and timestamps, and update uses `version` only as the precondition and ignores timestamps.

#### Scenario: the update request schema omits server audit fields

- **WHEN** the OpenAPI schema for `FlowUpdateRequest` is generated
- **THEN** it has no `created_at`, `updated_at`, or `last_modified` property
- **AND** its required set is exactly `id`, `version`, `name`, `nodes`, `connections`
- **AND** `PUT /flows/{id}` references `#/components/schemas/FlowUpdateRequest` as its request body
- **AND** the test that verifies this is `TestFlowUpdateRequestSchemaOmitsServerAuditFields`

#### Scenario: the create request schema omits version and timestamps

- **WHEN** the OpenAPI schema for `FlowCreateRequest` is generated
- **THEN** it has no `version`, `created_at`, `updated_at`, or `last_modified` property
- **AND** its required set is exactly `name`, `nodes`, `connections`
- **AND** `POST /flows` references `#/components/schemas/FlowCreateRequest` as its request body
- **AND** the test that verifies this is `TestFlowUpdateRequestSchemaOmitsServerAuditFields`

#### Scenario: a legacy full-Flow update body decodes and its timestamps are ignored

- **GIVEN** a client that round-trips the full `Flow` it read, with any `created_at` value
- **WHEN** it sends that body to `PUT /flows/{id}` with the current `version`
- **THEN** the response is `200` and the stored `created_at` is unchanged
- **AND** the test that verifies this is `TestFlowCRUDDoesNotPublishAndExplicitPublicationRetriesThroughConfigManager`

#### Scenario: the Flow response schema is unchanged

- **WHEN** the OpenAPI schema for `Flow` is generated
- **THEN** it still declares `id`, `version`, `created_at`, `updated_at`, `created_by`, and `last_modified`
- **AND** create `201` and update `200` still reference `#/components/schemas/Flow`
- **AND** the test that verifies this is `TestFlowOpenAPIPreservesFlowCRUDWireSchema`

### Requirement: List returns the current saved Flows and treats an absent key as ordinary state

`flowstore.Manager.List` SHALL enumerate keys through `natsclient.KVStore.Keys` and SHALL return a non-nil empty
`[]*Flow` with a nil error for an empty bucket. For each enumerated key it SHALL read the record through `Manager.Get`;
a key whose read reports typed absence — `errors.Is(err, natsclient.ErrKVKeyNotFound)`, which `KVStore.Get` returns
for a never-created and for a tombstoned key — SHALL be omitted from the result and SHALL NOT fail the list. Every
other per-key failure (transport, permission, deadline or cancellation, a stored record that does not decode) SHALL
abort the list with a nil result and SHALL be returned with the classification `Manager.Get` assigned: a record that
does not decode stays `errs.IsFatal`, a read that cannot complete stays `errs.IsTransient`; List SHALL NOT re-wrap
the failure under a different class. A context that is done when the key enumeration RETURNS SHALL likewise abort the
list — whether or not the enumeration reported any key — with a nil result and a transient error carrying the
cancellation cause, so a cancellation that raced the key watcher is never reported as an authoritative empty list. No
message text SHALL be inspected anywhere on the List path, and List SHALL promise no ordering.

#### Scenario: an empty bucket is a successful empty list

- **GIVEN** a `semstreams_flows` bucket with no keys
- **WHEN** a caller lists Flows
- **THEN** List returns a nil error
- **AND** the result is a non-nil slice of length zero
- **AND** the test that verifies this is `TestManagerListEmptyBucketReturnsNonNilEmpty`

#### Scenario: a key deleted between enumeration and its read is omitted

- **GIVEN** saved Flows A and B
- **AND** B is deleted after the keys are enumerated and immediately before B is read, through an explicit
  package-private seam
- **WHEN** List runs
- **THEN** it returns a nil error and exactly A
- **AND** no sleep or retry probability is relied on; the seam is explicit synchronization
- **AND** a subsequent List with no seam also returns exactly A
- **AND** the test that verifies this is `TestManagerListSkipsOnlyVanishedKey`

#### Scenario: a per-key read that cannot complete aborts the list with its transient class

- **GIVEN** saved Flows A and B
- **AND** the context List runs under is cancelled immediately before B is read
- **WHEN** List runs
- **THEN** it returns a non-nil error for which `errs.IsTransient` is true and
  `errors.Is(err, natsclient.ErrKVKeyNotFound)` is false
- **AND** the result is nil, not a partial list
- **AND** the test that verifies this is `TestManagerListPreservesPerKeyTransientFailure`

#### Scenario: a stored record that does not decode aborts the list as fatal

- **GIVEN** a saved Flow A and a key whose stored bytes are not valid Flow JSON
- **WHEN** List runs
- **THEN** it returns a non-nil error for which `errs.IsFatal` is true and `errs.IsTransient` is false
- **AND** the result is nil
- **AND** the test that verifies this is `TestManagerListPreservesCorruptRecordFailure`

#### Scenario: cancellation during enumeration aborts

- **GIVEN** a context that is cancelled while the keys are being enumerated, whether or not any key was reported —
  `natsclient.KVStore.Keys` maps the SDK's `jetstream.ErrNoKeysFound` to `(nil, nil)`, and a cancelled key watcher
  produces that same `(nil, nil)` shape
- **WHEN** List returns
- **THEN** the result is nil, never an empty success
- **AND** the error is `errs.IsTransient` and `errors.Is(err, context.Canceled)` is true
- **AND** the guard runs before any empty result is built, so `GET /flows` cannot answer `{"flows":[]}` and
  `list_flows` cannot answer `No flows configured.` for an enumeration the caller's context cut short
- **AND** the test that verifies this is `TestManagerListRejectsCancellationDuringEnumeration` (subtests
  `empty bucket` and `populated bucket`)

### Requirement: Empty saved-flow state is a normal outcome for every List consumer

Every consumer of `Manager.List` SHALL treat the typed empty result as ordinary state and SHALL branch on the returned
error's classification, never on its message text. `GET /flows` SHALL respond `200` with a body whose `flows` member
is present and is a JSON array — `[]` when there are no saved Flows, never `null`. The startup default-flow import
SHALL proceed on the empty list; a List failure keeps its existing outcome (import skipped, warning logged). The
FlowExecutor `list_flows` tool over a real `flowstore.Manager` SHALL return a completion whose `Content` is exactly
`No flows configured.` with no `Error` and no `ErrorKind` when the bucket is empty.

#### Scenario: HTTP list of an empty store is a non-null empty array

- **GIVEN** an empty `semstreams_flows` bucket
- **WHEN** a client sends `GET /flows`
- **THEN** the status is `200` and the content type is `application/json`
- **AND** the raw body's `flows` member is exactly `[]`
- **AND** decoding the body into a fresh `FlowListResponse` yields a non-nil `Flows` of length zero
- **AND** the test that verifies this is `TestHandleListFlowsEmptyResponseIsNonNullArray`

#### Scenario: startup imports the default Flow from the typed empty list

- **GIVEN** an empty bucket and a boot configuration with one enabled component
- **WHEN** the Flow service starts
- **THEN** `Start` returns nil and logs no default-flow import warning
- **AND** the store then holds exactly one Flow named `default` with one node
- **AND** the test that verifies this is `TestEnsureDefaultFlowEmptyListUsesTypedOutcome`

#### Scenario: the list_flows tool reports an empty store as a completion

- **GIVEN** a FlowExecutor over a real `flowstore.Manager` whose bucket is empty
- **WHEN** `list_flows` is executed
- **THEN** the executor returns no error, the result's `Error` and `ErrorKind` are empty, and its `Content` is
  exactly `No flows configured.`
- **AND** the test that verifies this is `TestFlowExecutorListFlowsRealManagerEmpty`

### Requirement: The list response schema declares a required non-null flows array

The saved-flow HTTP contract SHALL declare `FlowListResponse` as the `GET /flows` `200` response schema in the
generated OpenAPI. Its required set SHALL be exactly `flows`; `flows` SHALL be typed as an array whose items are the
Flow object schema and SHALL NOT be declared nullable. A populated list SHALL carry every saved Flow with its `id`,
`name`, `version`, and `created_by` as stored.

#### Scenario: the list response schema is generated from the registered type

- **WHEN** the OpenAPI schema for `FlowListResponse` is generated
- **THEN** its required set is exactly `flows`
- **AND** `flows` is `type: array` whose `items` is an object schema declaring `id`, `name`, `version`, `nodes`, and
  `connections` and carrying no `anyOf`/null alternative
- **AND** `GET /flows` `200` references `#/components/schemas/FlowListResponse`
- **AND** the test that verifies this is `TestFlowOpenAPIPreservesFlowCRUDWireSchema`

#### Scenario: a saved Flow appears in the list

- **GIVEN** a Flow created through `POST /flows`
- **WHEN** the client sends `GET /flows` and decodes the body into a fresh `FlowListResponse`
- **THEN** the list holds exactly that Flow with the created `id`, `name`, `version` 1, and `created_by`
- **AND** the test that verifies this is `TestFlowCRUDDoesNotPublishAndExplicitPublicationRetriesThroughConfigManager`


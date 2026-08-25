## ADDED Requirements

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

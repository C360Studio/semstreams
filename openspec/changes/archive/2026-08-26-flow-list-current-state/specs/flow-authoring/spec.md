## ADDED Requirements

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

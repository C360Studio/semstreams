## REMOVED Requirements

### Requirement: Saved Flow mutations are authoring-only

**Reason**: The framework no longer owns an authoring store. `flowstore` (`flowstore/manager.go`, bucket
`semstreams_flows`) and the `flow-builder` service (`service/flow_service.go`; `service/register.go:15`) are removed
under ADR-100: a composition is `config.Components` plus the binary's catalog, and a diagram is a projection that is
never saved.

**Migration**: Author the composition in the product's configuration file; validate it with the `validate` verb or
`composition.Validate` (see `composition-validation`). Delete any client code that calls `POST /flowbuilder/flows` or
`PUT /flowbuilder/flows/{id}`. Retained `semstreams_flows` buckets are inert and may be deleted.

### Requirement: Component configuration publication is explicit and next-boot-only

**Reason**: Publication compiled a saved diagram into `components.*` desired state through
`config.Manager.PutComponentToKV` (`service/flow_service.go:463-536`). With no saved diagram there is nothing to
compile; the framework exposes no next-boot component-configuration write verb (ADR-100 decision 4).

**Migration**: Edit the product's configuration and restart. `POST /flowbuilder/flows/{id}/publish-component-configs`
is removed without an alias.

### Requirement: Partial publication reports exact retry-safe progress

**Reason**: Publication is removed (previous requirement).

**Migration**: None; there is no partial publication to retry.

### Requirement: Flow lifecycle surfaces are absent

**Reason**: This requirement recorded ADR-096's retirement of lifecycle surfaces and the retention of name-keyed
observation routes. The observation routes (`/flowbuilder/flows/{id}/observations/{health,metrics,messages}`,
`service/flow_runtime_*.go`) are now removed with the diagram that keyed them; the absence of lifecycle surfaces is
carried by `service-composition` ("Running service and component composition is fixed at boot") and needs no
Flow-scoped restatement.

**Migration**: Read component health at `GET <components>/health` and `GET <components>/status/{name}`, metrics at
`/metrics`, and message observations from the message-logger service. Projections of the running composition come
from `GET <components>/flowgraph` (`composition-validation`).

### Requirement: Update owns the audit timestamps and treats the request version as a precondition

**Reason**: `flowstore.Manager.Update` is removed with `flowstore`. The named tests
(`TestManagerUpdatePreservesStoredCreatedAt`, `TestManagerUpdateIgnoresForgedCreatedAt`,
`TestManagerUpdateSuccessMutatesInputAfterCommit`, `TestManagerDiagramCRUDAndVersioning`,
`TestManagerUpdateFailedWriteDoesNotMutateInput`) are deleted with the package.

**Migration**: None; the store has no successor.

### Requirement: Concurrent Updates are revision-fenced and exactly one wins

**Reason**: `flowstore.Manager` is removed. The revision-fenced write primitive it used (`natsclient.KVStore.Update`,
`ErrKVRevisionMismatch`) stays in `natsclient` and is unaffected. `TestManagerUpdateTwoManagersExactlyOneWins` and
`TestFlowCRUDDoesNotPublishAndExplicitPublicationRetriesThroughConfigManager` are deleted with the package.

**Migration**: None.

### Requirement: Update leaves the caller's Flow untouched until commit

**Reason**: `flowstore.Manager.Update` is removed.

**Migration**: None.

### Requirement: Flow create and update request schemas omit server-owned fields and legacy bodies keep decoding

**Reason**: `FlowCreateRequest`, `FlowUpdateRequest`, and `Flow` leave the generated OpenAPI with the `flow-builder`
service (`service/flow_service.go:22-24,296-336`). `TestFlowUpdateRequestSchemaOmitsServerAuditFields` and
`TestFlowOpenAPIPreservesFlowCRUDWireSchema` are deleted.

**Migration**: Regenerate downstream clients from the new `specs/openapi.v3.yaml`; the `Flow*` schemas are gone.

### Requirement: List returns the current saved Flows and treats an absent key as ordinary state

**Reason**: `flowstore.Manager.List` is removed. The typed-absence discipline it established
(`errors.Is(err, natsclient.ErrKVKeyNotFound)`, never message text) stays as `nats-kv-keys` truth; the tests
`TestManagerListEmptyBucketReturnsNonNilEmpty`, `TestManagerListSkipsOnlyVanishedKey`,
`TestManagerListPreservesPerKeyTransientFailure`, `TestManagerListPreservesCorruptRecordFailure`,
`TestManagerListRejectsCancellationDuringEnumeration` are deleted with the package.

**Migration**: None.

### Requirement: Empty saved-flow state is a normal outcome for every List consumer

**Reason**: All three List consumers are removed: `GET /flows` (`handleListFlows`), the startup default-flow import
(`ensureDefaultFlowFromConfig` — its config→graph derivation moves to `composition-validation`'s projection), and
the `list_flows` tool (`processor/agentic-tools/executors/flows.go`). `TestHandleListFlowsEmptyResponseIsNonNullArray`,
`TestEnsureDefaultFlowEmptyListUsesTypedOutcome`, and `TestFlowExecutorListFlowsRealManagerEmpty` are deleted.

**Migration**: Agents use `catalog`, `validate_composition`, and `composition_graph` (`composition-validation`).

### Requirement: The list response schema declares a required non-null flows array

**Reason**: `FlowListResponse` leaves the generated OpenAPI with the service.

**Migration**: Regenerate downstream clients.

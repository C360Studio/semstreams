## ADDED Requirements

### Requirement: The framework owns no composition authoring store

The framework SHALL register no `flow-builder` service, SHALL serve no `/flowbuilder/*` route, SHALL register no
`create_flow`, `update_flow`, `delete_flow`, `list_flows`, `get_flow`, `create_flow_template`,
`update_flow_template`, `delete_flow_template`, `list_flow_templates`, `get_flow_template`, or
`instantiate_flow_template` tool, SHALL create no `semstreams_flows` or `FLOW_TEMPLATES` bucket, and SHALL provide no
compatibility alias for any of them. The stream-override expiry metric formerly hosted by the flow-builder service
SHALL be registered by a retained service so its removal does not remove the metric.

#### Scenario: the removed surfaces are absent

- **WHEN** the service registry, the tool registry, and the generated OpenAPI document are inspected
- **THEN** none names a flow-builder service, a flow or flow-template tool, or a `/flowbuilder` or `/flows` path
- **AND** the tests that verify this are `TestServiceRegistryHasNoFlowBuilder`, `TestToolRegistryHasNoFlowTools`, and
  `TestOpenAPIHasNoFlowRoutes`

#### Scenario: the override-expiry metric survives the removal

- **GIVEN** a boot configuration with a stream override and no flow-builder service
- **WHEN** the process composes its services
- **THEN** the stream-override expiry metric is registered and reports the override
- **AND** the test that verifies this is `TestStreamOverrideExpiryReporterRegistersWithoutFlowService`

## MODIFIED Requirements

### Requirement: The framework serves one composition judgment and no second gap analysis

The framework SHALL serve exactly one operation that judges the running composition — `GET <components>/validate`,
which serves the retained boot `composition.Result` verbatim — and SHALL serve no second connectivity, gap, or orphan
analysis that applies a severity vocabulary of its own. `GET <components>/gaps` and its response body
(`disconnected_nodes`, `orphaned_ports`, `objectstore_gaps`, and the `summary` object carrying `total_gaps`,
`critical_gaps`, `optional_gaps`, `critical_port_count`, and `has_issues`) SHALL be absent from the routed surface and
from the generated OpenAPI document, with no alias; the Go surface it reached — `ComponentManager.ValidateFlowConnectivity`,
`ComponentManager.DetectObjectStoreGaps`, and the `ComponentGap` type — SHALL be absent too. That operation classified a
required input declared `external` as a critical orphan (`no_publishers`, `critical_port_count: 1`, `has_issues: true`)
while the canonical judgment raised no finding for the same port; a second interpreter of one analysis is refused rather
than re-projected. Pre-v1 fresh-state policy applies: no compatibility view and no legacy reader.

`POST <flowbuilder>/flows/{id}/validate` — the served second severity table, which applied its own error rule to
required stream inputs with no publisher and performed no `External` check — SHALL be absent, together with the
`engine` package that computed it. `flowgraph.FlowGraph.AnalyzeConnectivity` SHALL report connected components,
disconnected nodes, and orphaned ports and SHALL derive no status of its own; `composition.Result.Status` is the one
status. `flowgraph.BuildFromRegistry` SHALL be absent: `flowgraph.BuildFromDeclarations` is the one construction seam,
and `composition.Analyze` is its production caller.

A projection that derives no severity is not a judgment and is unaffected, but SHALL be derived from the retained
`composition.Result` rather than from a second graph build. `GET <components>/paths` SHALL serve reachability computed
from `composition.Result.Graph`; `ComponentManager.GetFlowGraph` and its Registry rebuild and cache SHALL be absent.

#### Scenario: the gap operation is absent from the routed and advertised surface

- **GIVEN** the ComponentManager HTTP handlers registered on a fresh mux
- **WHEN** `<components>/gaps` is requested with GET, POST, and DELETE
- **THEN** each request is unrouted (404) or refused (405), and the ComponentManager OpenAPI document — the source the
  generated `specs/openapi.v3.yaml` is emitted from — advertises no `/gaps` operation
- **AND** the test that verifies this is `TestComponentGapsOperationIsAbsent`

#### Scenario: an externally fed input is never a critical orphan on any component operation

- **GIVEN** an admitted component whose only input is a required JetStream port declared `external: true` with no
  publisher in the composition
- **WHEN** every operation the ComponentManager OpenAPI document advertises is requested
- **THEN** no response body carries `no_publishers`, `orphaned_port`, `critical`, or `has_issues` for that port, the
  retained boot result has no error finding, and the projection shows the marker on the port
- **AND** the test that verifies this is `TestExternalInputIsNeverACriticalOrphanOnAnyComponentOperation`

#### Scenario: the saved-diagram validation route is absent with its engine

- **WHEN** the service registry, the generated OpenAPI document, and the Go surface are inspected
- **THEN** no service named `flow-builder` is registered, no `/flows/{id}/validate` path is advertised, and no `engine`
  package exists to compute a second severity table
- **AND** the tests that verify this are `TestServiceRegistryHasNoFlowBuilder` and `TestOpenAPIHasNoFlowRoutes`

#### Scenario: the paths projection serves the retained graph

- **GIVEN** a ComponentManager whose live component instances were mutated after admission
- **WHEN** `<components>/paths` is requested alongside `<components>/flowgraph` and `<components>/validate`
- **THEN** all three answer from the composition result retained at Initialize, and no second graph is built from the
  Registry
- **AND** the tests that verify this are `TestComponentManagerFlowReportingUsesRetainedPortsAfterComponentMutation`
  and `TestComponentManagerProjectionCarriesOnlyAdmittedInstances`

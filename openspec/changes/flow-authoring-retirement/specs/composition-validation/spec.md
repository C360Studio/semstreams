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

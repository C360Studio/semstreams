## Purpose

Defines composition validation as the framework's one judgment of whether a set of components is correctly wired:
`config.Components` plus the binary's catalog is the composed artifact, connections are derived from static port
declarations rather than authored, and a diagram is a read-only projection the framework never stores (ADR-100). The
same pure library produces the same closed findings vocabulary at both boundaries that matter — offline over a
configuration file, as a prediction of the next boot, and at boot over the admitted composition, as an observation at
the real boundary, where an error finding refuses to start. No second interpreter of that analysis is admitted: not a
handler with its own severity table, not a saved-diagram validator, not a caller re-deriving connections. The capability
owns the findings vocabulary and its severities, the static port-declaration contract and its boot parity check, the
graph projection and its Mermaid rendering, and the surfaces that carry them — the `catalog` / `validate` / `graph`
verbs, the `AssertValid` test helper, the ComponentManager read operations, and the read-only agent tools. It owns no
authoring store, no diagram CRUD, no template store, and no verb that writes a composition: writing a composition is
writing the product's configuration.

## ADDED Requirements

### Requirement: Port declarations are static facts of a registration

Every `component.Registration` SHALL carry a `Ports` declarer — a pure function of the raw component configuration
and the instance name that returns the component's `component.PortConfig` without dependencies, I/O, or
construction — and `Registry.RegisterFactory` SHALL reject a registration whose declarer is nil exactly as it rejects
a nil `Factory`. The framework SHALL resolve the declared definitions through the one canonical
`PortDefinition.Resolve` path; no second interpreter of a port declaration is admitted. At boot admission the
Registry SHALL evaluate the declarer for the admitted instance and compare the resolved declaration (name, direction,
required, kind, resource identity, NATS subjects, interface contract, in order) with the ports captured from the
constructed component; any difference SHALL fail admission with a classified invalid error that names the factory,
the instance, and the first differing port. The generated catalog (`schemas/<factory>.v1.json`, the `types` HTTP
operations, and the `list_components` tool) SHALL carry the declarer's output for an empty configuration as
`default_ports` (each resolved port with its `external` marker when declared), or `ports_require_config: true` with
the declarer's error text when an empty configuration does not declare.

> `[~]` DEVIATION (tasks 3.1a, 2026-08-26): `objectstore`'s declarer does not honour the instance-name parameter of the
> declarer contract above. Its factory has no instance name to give the constructor (`component.Factory` and
> `Dependencies` carry none) and stamps the literal `objectstore` into its `store-provide` port, so the admitted
> declaration never carries the real instance name either; the declarer mirrors the constructor so parity holds.
> Threading the real name through changes the store-provide resource identity at runtime — an owner ruling, FILED
> #1106; neither side is codified in a test. Every shipped instance is named `objectstore`.

#### Scenario: a registration without a port declarer is rejected at registration

- **GIVEN** a `RegistrationConfig` with a valid `Factory` and `Schema` and a nil `Ports`
- **WHEN** it is registered
- **THEN** `RegisterFactory` returns a classified invalid error naming the factory
- **AND** the registry contains no factory of that name
- **AND** the test that verifies this is `TestRegisterFactoryRejectsNilPortDeclarer`

#### Scenario: every shipped factory's declaration equals its constructed ports

- **GIVEN** the full registry the framework binary composes (core, graph-research, OTEL) and a real NATS test client
- **WHEN** each factory is constructed through boot admission with its default configuration (or the smallest
  configuration its schema requires) and its declarer is evaluated for the same input
- **THEN** for all 33 factories the resolved declared ports equal the captured ports, port for port
- **AND** the test that verifies this is `TestDeclaredPortsMatchConstructedPortsForEveryRegisteredFactory`
  (`-tags=integration`)

#### Scenario: a lying declarer fails admission

- **GIVEN** a test factory whose declarer returns one output port and whose constructed component returns two
- **WHEN** ComponentManager admits it
- **THEN** admission fails with a classified invalid error that names the factory, the instance, and the differing port
- **AND** the Registry holds no declaration for that instance
- **AND** the test that verifies this is `TestAdmissionRefusesPortDeclarationMismatch`

#### Scenario: the catalog carries default ports or says the factory needs configuration

- **WHEN** the catalog is produced for the full registry
- **THEN** every entry whose declarer accepts an empty configuration carries `default_ports` with resolved inputs and
  outputs
- **AND** every entry whose declarer rejects an empty configuration carries `ports_require_config: true` and the
  declarer's error text, and no `default_ports`
- **AND** the tests that verify this are `TestCatalogCarriesDefaultPortsOrRequiresConfig` and
  `TestSchemaExportCarriesDefaultPorts`

### Requirement: Composition validation is a pure function with one closed findings vocabulary

`composition.Validate(catalog, cfg)` SHALL be a pure, deterministic function of a `*component.Registry` and a
`*config.Config` that performs no I/O, opens no connection, and constructs no component, and
`composition.Analyze(declarations, streams)` SHALL be the graph-level half of the same function over admitted
declarations and the configuration's explicit `streams` declarations, so that the offline and the boot-time judgments
share one interpreter. A JetStream input SHALL NOT be a `stream_requirement` finding, even when its only publishers use
core NATS, when the configuration's explicit `streams` block declares the stream the input binds to BY NAME and one of
that stream's subjects COVERS (every concrete subject of the input's subject also matches it — not merely overlaps) each
of the input's subjects: provisioning creates exactly the explicit and JetStream-output-derived streams, the consumer
binds by name, and core-NATS publishes on covered subjects land in it, so the subscriber is fed. A stream declared
under another name, or one whose subjects only overlap the input's, SHALL NOT satisfy it. The result SHALL carry `status`
(`valid` | `warnings` | `errors`, derived errors → warnings → valid), `errors`, `warnings`, and `graph`, with every
array non-nil; each finding SHALL carry `type`, `severity`, a non-empty `component`, optional `port`, a non-empty
`message`, and a non-nil `suggestions`. `type` SHALL be one of the exported constants `config_invalid`,
`unknown_component`, `component_type_mismatch`, `component_config_invalid`, `port_declaration_error`,
`exclusive_resource_conflict`, `connection_pattern_error`, `stream_requirement`, `disconnected_node`,
`orphaned_port`, `interface_mismatch`, `missing_interface`, `empty_composition`; nothing SHALL emit a type outside
that set. Severity SHALL be error for `config_invalid`, `unknown_component`, `component_type_mismatch`,
`component_config_invalid`, `port_declaration_error`, `exclusive_resource_conflict`, `connection_pattern_error`,
`stream_requirement`, `interface_mismatch`, and for an `orphaned_port` that is a required stream input with no
publisher; warning otherwise. An input declared `external` (`component.PortDefinition.External` — fed from outside the
composition, an operator statement) SHALL raise no `orphaned_port` finding for its missing in-graph publisher; every
other finding on that port, and every unmarked required orphan, is unaffected. The required-orphan finding SHALL
carry the remedy among its suggestions ("declare `external: true` on the port if it is fed from outside the
composition"), and the boot refusal SHALL print each error finding with its suggestions, so an operator whose named
override dropped the marker is told the one-line fix rather than "connect an output". Components SHALL be visited in instance-name order and edges SHALL be emitted in a
stable order, so two runs over equal inputs produce byte-equal JSON.

#### Scenario: the vocabulary is closed

- **GIVEN** the thirteen exported type constants
- **WHEN** compositions exhibiting each condition are validated
- **THEN** every emitted finding's `type` is one of the thirteen and every constant is emitted by at least one case
- **AND** the test that verifies this is `TestValidateFindingsVocabularyIsClosed`

#### Scenario: an unknown factory is a finding, not an error return

- **GIVEN** a configuration naming a factory the catalog does not register
- **WHEN** it is validated
- **THEN** the result carries one `unknown_component` error naming the instance and the factory
- **AND** every other component is still analyzed
- **AND** the test that verifies this is `TestValidateReportsUnknownComponent`

#### Scenario: a required stream input with no publisher is an error

- **GIVEN** a composition whose only consumer declares a required JetStream input that no output feeds
- **WHEN** it is validated
- **THEN** the result carries one `orphaned_port` error naming the component and the port
- **AND** an optional unfed input in the same composition is an `orphaned_port` warning
- **AND** the test that verifies this is `TestValidateReportsRequiredStreamInputWithoutPublisher`

#### Scenario: an externally fed required input is not an orphan

- **GIVEN** a required stream input declared `external: true` with no publisher in the composition, an unmarked
  required stream input with no publisher, and an `external` input whose in-graph publisher's interface differs
- **WHEN** the composition is validated
- **THEN** the marked input raises no `orphaned_port` finding, the unmarked orphan is still an `orphaned_port` error,
  and the marked, fed input still carries its `interface_mismatch` error
- **AND** the projection shows the marker on the port
- **AND** the test that verifies this is `TestValidateSuppressesOrphanOnlyForExternallyFedInput`

#### Scenario: interface contracts are checked on every derived edge

- **GIVEN** an output whose interface type differs from the input it feeds, and a second edge whose source declares
  no interface while its target requires one
- **WHEN** the composition is validated
- **THEN** the result carries one `interface_mismatch` error and one `missing_interface` warning, each naming both
  ends
- **AND** the test that verifies this is `TestValidateReportsInterfaceMismatch`

#### Scenario: a JetStream subscriber fed only by core-NATS publishers is an error

- **GIVEN** a JetStream input port whose only publishers are core NATS outputs, and no explicit stream covering its subjects
- **WHEN** the composition is validated
- **THEN** the result carries one `stream_requirement` error naming the subscriber port and every publisher
- **AND** the test that verifies this is `TestValidateReportsStreamRequirement`

#### Scenario: an explicit stream declaration satisfies a JetStream subscriber

- **GIVEN** the same ports and a `streams` declaration whose subjects cover the subscriber's subjects
- **WHEN** the composition is validated offline, and when the same composition boots with the same `streams`
- **THEN** neither result carries a `stream_requirement` finding and the edge is still derived
- **AND** a stream declared under a name other than the one the subscriber binds to does not satisfy it, even when
  its subjects cover the subscriber's
- **AND** a stream whose subjects only overlap the subscriber's (`data.raw` against a `data.*` subscriber) does not
  satisfy it
- **AND** the tests that verify this are `TestValidateStreamRequirementSatisfiedByExplicitStream`,
  `TestValidateStreamRequirementNeedsTheNamedStream`, `TestValidateStreamRequirementNeedsCoverNotOverlap`, and
  `TestComponentManagerBootFindingsHonourExplicitStreams` (`-tags=integration`)

#### Scenario: pattern conflicts and exclusive resources are findings

- **GIVEN** two components that write the same KV bucket, and two that bind the same network address (an exclusive
  resource, refused at admission before any graph is built)
- **WHEN** the composition is validated
- **THEN** the result carries one `connection_pattern_error` and one `exclusive_resource_conflict`, both errors
- **AND** validation returns a result, not an error
- **AND** the tests that verify this are `TestValidateReportsConnectionPatternConflict` and
  `TestValidateReportsExclusiveResourceConflict`

#### Scenario: validation is deterministic

- **GIVEN** the same catalog and configuration
- **WHEN** validation runs twice
- **THEN** the two results marshal to byte-equal JSON
- **AND** the test that verifies this is `TestValidateIsDeterministic`

### Requirement: Every binary can expose the composition verbs through one exported entry point

The framework SHALL export a CLI entry point that a product binary calls from its own `main` to serve
`catalog`, `validate <config-path>`, and `graph <config-path> [--mermaid]`, taking only the product's
`*component.Registry`, an output writer, and the arguments; `validate` SHALL print the findings and exit non-zero
when the result has errors; `catalog` SHALL print every registered factory with its schema and default ports;
`graph` SHALL print the projection as JSON or Mermaid. `cmd/semstreams` SHALL serve the same three verbs, and its
existing `--validate` flag SHALL report the same findings by calling the same function.

#### Scenario: validate exits non-zero on an error finding

- **GIVEN** a configuration file that yields one error finding
- **WHEN** `validate <path>` runs through the exported entry point against the framework registry
- **THEN** the findings are printed, the exit code is non-zero, and no NATS connection is attempted
- **AND** a configuration with only warnings exits zero and prints the warnings
- **AND** the test that verifies this is `TestCLIValidateExitsNonZeroOnErrorFindings`

#### Scenario: catalog lists every registered factory

- **WHEN** `catalog` runs against the full framework registry
- **THEN** exactly 33 entries are printed, each with schema and default ports or `ports_require_config`
- **AND** the test that verifies this is `TestCLICatalogPrintsEveryRegisteredFactory`

#### Scenario: graph renders every derived edge in Mermaid

- **GIVEN** a configuration whose validation derives N edges
- **WHEN** `graph <path> --mermaid` runs
- **THEN** the output contains one Mermaid edge line per derived edge and one node per enabled component
- **AND** the test that verifies this is `TestCLIGraphMermaidRendersEveryEdge`

#### Scenario: the legacy flag reports the same findings

- **GIVEN** a configuration with one error finding
- **WHEN** `semstreams --validate --config <path>` runs
- **THEN** the same findings are printed and the process exits non-zero
- **AND** the test that verifies this is `TestValidateFlagReportsCompositionFindings`

### Requirement: Product CI can assert a composition with one call

`composition.AssertValid(t, catalog, cfg)` SHALL fail the test with every finding of severity error printed and
SHALL pass on warnings, following the `natsclient.NewTestClient(t, ...)` precedent for `testing.TB`-taking framework
helpers.

#### Scenario: the helper fails on an error finding and passes on warnings

- **GIVEN** one configuration with an error finding and one with only warnings
- **WHEN** `AssertValid` runs for each under a recording `testing.TB`
- **THEN** the first records a failure whose message names the finding type and component and the second records none
- **AND** the test that verifies this is `TestAssertValidFailsOnErrorFinding`

### Requirement: Boot validates the admitted composition at the real boundary

ComponentManager SHALL run `composition.Analyze` over the admitted Registry declarations and the boot configuration's
explicit `streams` after the fixed boot set is constructed and before the Registry seals, SHALL log every finding, SHALL fail `Initialize` (and therefore boot) when
the result has an error, and SHALL retain the result as the boot composition's findings. `GET <components>/validate`
SHALL serve that retained result verbatim and SHALL NOT compute a status of its own; `GET <components>/flowgraph`
SHALL serve the retained result's `graph`, as JSON by default and as Mermaid when `format=mermaid` is requested.

#### Scenario: an error finding refuses boot

- **GIVEN** a boot configuration whose admitted composition yields a `stream_requirement` error
- **WHEN** ComponentManager initializes against a real NATS test client
- **THEN** `Initialize` returns an error that names the finding type and component
- **AND** no component is started and the Registry is not sealed as a running composition
- **AND** the test that verifies this is `TestComponentManagerRefusesBootOnErrorFinding` (`-tags=integration`)

#### Scenario: the HTTP validate operation projects the boot result

- **GIVEN** a booted composition with one `disconnected_node` warning
- **WHEN** a client reads `<components>/validate` and decodes the body into a fresh `composition.Result`
- **THEN** the decoded result equals the result ComponentManager retained at boot, including `status: warnings`
- **AND** the handler contains no severity or status logic of its own
- **AND** the tests that verify this are `TestComponentManagerExposesBootFindings` and
  `TestFlowValidationHandlerProjectsLibraryResult`

#### Scenario: the projection of the running composition matches the admitted declarations

- **GIVEN** a booted composition
- **WHEN** a client reads `<components>/flowgraph` as JSON and as Mermaid
- **THEN** the JSON `graph` names every admitted instance with its resolved ports and every derived edge, and the
  Mermaid output renders the same node and edge set
- **AND** the tests that verify this are `TestGraphProjectionMatchesAdmittedComposition` (`-tags=integration`) and
  `TestMermaidIsDeterministic`

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
than re-projected. Pre-v1 fresh-state policy applies: no compatibility view and no legacy reader. A projection that
derives no severity is not a judgment and is unaffected.

> `[~]` DELIBERATE NOT-DONE (2026-08-26, this change): the shared primitives under the retired operation are RETAINED
> because callers outside this change still reach them. `flowgraph.FlowGraph.AnalyzeConnectivity` is the analysis
> `composition.Analyze` itself runs (`composition/analyze.go`) and the `engine` oracle still calls it;
> `ComponentManager.GetFlowGraph` and `GET <components>/paths` (reachability from input components — a projection with no
> severity) still build a graph from the admitted registry. The `engine` caller leaves with #1093; whether `/paths`
> should serve the retained `composition.Result.Graph` instead of rebuilding is #1093's scope, not this change's, and is
> recorded there. Only the judging operation is removed here.

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

### Requirement: Agents read the catalog, validate, and project through read-only tools

The agentic-tools registry SHALL register, under the existing component-catalog gate that requires only a
`*component.Registry`, the read-only tools `list_components` (now carrying `default_ports`),
`validate_composition` (input: a configuration document as a JSON object; output: the `composition.Result`), and
`composition_graph` (input: a configuration document and `format` `json` | `mermaid`; output: the projection). None
of the three SHALL require a NATS client, write anything, or carry a new payload type.

#### Scenario: validate_composition returns the same findings as the library

- **GIVEN** a configuration document that yields one `interface_mismatch` error
- **WHEN** an agent calls `validate_composition` with that document
- **THEN** the tool result's content decodes into a `composition.Result` equal to `composition.Validate` over the
  same input, with no `Error` attachment
- **AND** the test that verifies this is `TestValidateCompositionToolReturnsFindings`

#### Scenario: composition_graph renders Mermaid and list_components carries ports

- **WHEN** an agent calls `composition_graph` with `format: mermaid`, and `list_components` with a `type_filter`
- **THEN** the first returns the Mermaid projection and the second's entry carries `default_ports`
- **AND** the tests that verify this are `TestCompositionGraphToolReturnsMermaid` and `TestListComponentsCarriesPorts`

### Requirement: Shipped compositions carry no error finding

Every checked-in composition the framework ships or tests with (`configs/**/*.json`, the e2e compose configurations
under `docker/` and `test/e2e`) SHALL validate with no error finding against the registry its binary composes, and
that assertion SHALL be a unit test so a configuration change that introduces an error is caught before boot.

#### Scenario: the shipped configurations validate clean

- **WHEN** every shipped configuration is validated against the registry its binary composes
- **THEN** no result has an error finding
- **AND** the test that verifies this is `TestValidateShippedConfigsHaveNoErrorFindings`

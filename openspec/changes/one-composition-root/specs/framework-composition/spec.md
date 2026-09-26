# framework-composition Delta

## ADDED Requirements

### Requirement: One framework boot composes both framework binaries

`cmd/semstreams` and `cmd/e2e-semstreams` MUST boot through one framework-owned composition function that performs
the pre-`ComponentManager.Start` steps — vocabulary, components, payloads, graph runtime, personas, tools, lifecycle,
services — in the order their dependencies require, parses one command line with one set of defaults, and composes
one Phase-A logging shape. The two binaries MUST differ only by the value of one options seam: the production binary's
options carry no extension, and the E2E binary's extensions are enabled only by `SEMSTREAMS_E2E_*` environment
variables its tier's compose service sets, a nonempty value enabling. The production binary's dependency closure MUST
hold no E2E harness, example, fixture, or mission package, and no build tag MAY gate an E2E-only registration or hook.

#### Scenario: the E2E binary's boot options with nothing enabled are the production options

- **GIVEN** the E2E options constructor with an environment holding no `SEMSTREAMS_E2E_*` variable
- **WHEN** its options are compared with the production options for the same command-line inputs and build metadata
- **THEN** every extension slice is empty in both and every scalar field is equal
- **AND** the comparison enumerates the options struct's fields by reflection, so a field added later is compared
  without editing the test
- **AND** the test that verifies this is `TestE2EBootWithNoOptionsIsTheProductionOptions`

#### Scenario: an option is enabled by exactly its variable

- **GIVEN** the E2E options constructor
- **WHEN** one `SEMSTREAMS_E2E_*` variable from the tier table's Gate column is set to a nonempty value
- **THEN** exactly the extension slices that option declares grow, by exactly the declared count
- **AND** the same variable set to the empty string enables nothing
- **AND** the test that verifies this is `TestE2EBootOptionAppendsExactlyItsExtensions`

#### Scenario: the production binary links no E2E package and the E2E binary links every hook

- **GIVEN** the non-test import closure of each binary as `go list -deps` reports it
- **WHEN** the production binary's closure is read
- **THEN** it holds no package under `test/e2e/harness`, `internal/e2eboot`, `internal/e2eslowconsumer`,
  `examples/processors`, or `cmd/e2e-semstreams`
- **AND** the E2E binary's closure holds every package under `test/e2e/harness`, plus `internal/e2eboot`,
  `internal/e2eslowconsumer`, `cmd/e2e-semstreams/fixtures` and `cmd/e2e-semstreams/mission`, so no hook is stranded
- **AND** the test that verifies this is `TestProductionRootClosureHoldsNoE2EHarness`

#### Scenario: the variables the E2E binary accepts are the ones the tier table declares

- **GIVEN** the tier table in the payload-registry specification
- **WHEN** the union of its Gate column's `SEMSTREAMS_E2E_*` names is compared with the names the E2E options
  constructor accepts
- **THEN** the two sets are equal
- **AND** the test that verifies this is `TestE2EBootVariableSetMatchesTierTable`

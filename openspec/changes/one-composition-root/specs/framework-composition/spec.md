# framework-composition Delta

## ADDED Requirements

### Requirement: One framework boot composes both framework binaries

`cmd/semstreams` and `cmd/e2e-semstreams` MUST boot through one framework-owned composition function that performs
the pre-`ComponentManager.Start` steps — vocabulary, components, payloads, graph runtime, personas, tools, lifecycle,
services — in the order their dependencies require. The two binaries MUST differ only by the value of one options
seam: the production binary's options carry no extension, and the E2E binary's extensions are enabled only by
`SEMSTREAMS_E2E_*` environment variables its tier's compose service sets. The production binary's dependency closure
MUST hold no E2E harness, example, fixture, or mission package, and no build tag MAY gate an E2E-only registration or
hook.

#### Scenario: the E2E binary with no option enabled is the production composition

- **GIVEN** the E2E options constructor with an environment holding no `SEMSTREAMS_E2E_*` variable
- **WHEN** its options are compared with the production options for the same command-line inputs
- **THEN** every extension slice is empty in both and every scalar field is equal
- **AND** the comparison enumerates the options struct's fields by reflection, so a field added later is compared
  without editing the test
- **AND** the test that verifies this is `TestE2EBootWithNoOptionsIsTheProductionComposition`

#### Scenario: an option is enabled by exactly its variable and an unknown variable is refused

- **GIVEN** the E2E options constructor
- **WHEN** one `SEMSTREAMS_E2E_*` variable from the tier table's Gate column is set
- **THEN** exactly the extension slices that option declares grow, by exactly the declared count
- **AND** `SEMSTREAMS_E2E_LIFECYCLE_SEED` without `SEMSTREAMS_E2E_MISSION` is refused with an error naming both
- **AND** any `SEMSTREAMS_E2E_*` name the constructor does not know is refused with an error naming it, so a misspelled
  variable cannot leave a tier's proof silently unarmed
- **AND** the tests that verify this are `TestE2EBootOptionAppendsExactlyItsExtensions` and
  `TestE2EBootRefusesUnknownE2EVariable`

#### Scenario: the production binary links no E2E package and the E2E binary links every one

- **GIVEN** the non-test import closure of each binary as `go list -deps` reports it
- **WHEN** the production binary's closure is read
- **THEN** it holds no package under `test/e2e/harness`, `internal/e2eboot`, `internal/e2eslowconsumer`,
  `examples/processors`, or `cmd/e2e-semstreams`
- **AND** the E2E binary's closure holds every such package that exists, so no hook is stranded
- **AND** the test that verifies this is `TestProductionRootClosureHoldsNoE2EHarness`

#### Scenario: the variables the E2E binary accepts are the ones the tier table declares

- **GIVEN** the tier table in the payload-registry specification
- **WHEN** the union of its Gate column's `SEMSTREAMS_E2E_*` names is compared with the names the E2E options
  constructor accepts
- **THEN** the two sets are equal
- **AND** the test that verifies this is `TestE2EBootVariableSetMatchesTierTable`

# payload-registry Delta

## MODIFIED Requirements

### Requirement: A message type is a type of the deployment only if it is registered in the binary's payload registry

The payload registry MUST be the single authority for which `message.Type` keys (`domain.category.version`) exist in a
deployment. `Register` MUST reject a nil registration, a nil factory, an empty domain, category, or version, a factory whose
payload `Schema()` disagrees with the registration, and a key already registered; there MUST be no second catalogue of
types, and no global registry — each binary constructs its own and injects it through `Dependencies.PayloadRegistry`. A type
registered in one binary is not thereby a type of another: the attributes registered with it (floor, contracts) exist only
where the type is registered.

A production-target e2e tier stamps only what the production binary registers (owner ruling on #1100, 2026-08-27):
`cmd/semstreams` registers no test type, so a scenario that births a synthetic type runs against `cmd/e2e-semstreams`, whose
composition root registers them through `cmd/e2e-semstreams/fixtures.RegisterPayloads`.

That choice generalises to one rule for every E2E tier (owner rulings on #1249 and #1301, 2026-09-22). Both binaries boot
through one framework composition function; they differ only by the options the E2E binary enables from
`SEMSTREAMS_E2E_*` environment variables (the framework-composition specification). A tier that proves the shipped
binary boots `cmd/semstreams` through the `production` Dockerfile target, which can enable no option. Every E2E-only
registration or hook — examples and fixtures, the mission workflow, a control responder, a probe, a barrier, a fault
injector — is a boot option that its tier's compose service enables by setting exactly that option's variable on the
`e2e` target, and a tier that proves the production composition WITH a hook inside it boots the `e2e` target with only
that hook's variable set: with nothing set, the `e2e` binary's boot options are the production options (the parity
scenario in the framework-composition specification). No build tag gates an E2E-only registration or hook.

The tier's binary, target and gate are READ from the artifacts that boot it — `build.target` in the tier's compose service,
the Go package of that target in `docker/Dockerfile`, the `SEMSTREAMS_E2E_*` variables the service sets — never predicted
from the word "test". This table is the observation, and a contract test re-reads it against those artifacts:

| Tier (`task e2e:<tier>`) | Compose service | Target → binary | Gate | E2E-only registrations and hooks | Synthetic types stamped on `entity.create` |
|---|---|---|---|---|---|
| core — phase 1 (`core-health`, `core-dataflow`) | `e2e.yml` `semstreams` | `production` → `cmd/semstreams` | none | none | none |
| core — phase 2 (`core-graph-roundtrip`), lessons | `e2e.yml` `semstreams-fixtures` (profile fixtures) | `e2e` → `cmd/e2e-semstreams` | `SEMSTREAMS_E2E_EXAMPLES=1` | fixture payloads | `test.fixture.v1` (evidence fixture for lessons) |
| structural | `tiered.yml` `semstreams-structural` (profile structural) | `e2e` → `cmd/e2e-semstreams` | `SEMSTREAMS_E2E_EXAMPLES=1` | example components, fixture payloads | `e2e.eventtime.v1`, `e2e.canonical_create_contract.v1`, `e2e.relationship_contract.v1` |
| statistical, throughput | `tiered.yml` `semstreams` (profile statistical) | `e2e` → `cmd/e2e-semstreams` | `SEMSTREAMS_E2E_EXAMPLES=1` | example components | none |
| semantic (and its `:8b` / `:frontier` overlays) | `tiered.yml` `semstreams-ml` (profile semantic) | `e2e` → `cmd/e2e-semstreams` | `SEMSTREAMS_E2E_EXAMPLES=1` | example components | none |
| lifecycle | `lifecycle.yml` `semstreams` | `e2e` → `cmd/e2e-semstreams` | `SEMSTREAMS_E2E_MISSION=1`, `SEMSTREAMS_E2E_LIFECYCLE_SEED=<suffix>` | mission component and workflow, post-start seed | none (`lifecycle.harness.v1` is a framework type) |
| ops | `ops.yml` `semstreams` | `e2e` → `cmd/e2e-semstreams` | `SEMSTREAMS_E2E_LESSON_CURATION=1` | lesson-curation control responder | none (its seed is the framework type `agentic.loop_completed.v1`, written by direct `PutKV` — `ops/scenario.go:439,484`) |
| research-graph | `research-graph.yml` `semstreams` | `e2e` → `cmd/e2e-semstreams` | `SEMSTREAMS_E2E_EXAMPLES=1` | fixture payloads | `research.e2e_search_seed.v1` |
| crud-tools | `crud-tools.yml` `semstreams` | `production` → `cmd/semstreams` | none | none | none on create (`e2e.probe.v1` is a direct `PutKV`) |
| deep-research | `deep-research.yml` `semstreams` | `production` → `cmd/semstreams` | none | none | none |
| agentic | `agentic.yml` `semstreams` | `e2e` → `cmd/e2e-semstreams` | `SEMSTREAMS_E2E_PROCESS_BARRIER=1`, `SEMSTREAMS_E2E_MILESTONE_PROBE=1` | process barrier (tool executor), milestone settlement probe (`MilestoneHandler`) | none |
| slow-consumer | `e2e-slow-consumer.yml` `semstreams` | `e2e` → `cmd/e2e-semstreams` | `SEMSTREAMS_E2E_SLOW_CONSUMER=1` | slow-consumer boot probe | none |

#### Scenario: a colliding key is refused at registration

- **GIVEN** a registry holding `agentic.agent_lesson.v1`
- **WHEN** a second registration with the same domain, category, and version is registered
- **THEN** `Register` returns an error naming the key
- **AND** the first registration is unchanged
- **AND** the test that verifies this is `TestRegistry_RegisterPayload_DuplicateError`

#### Scenario: a type is known only where it is registered

- **GIVEN** a binary that does not select graph research
- **WHEN** `IndexingProfileFor("research.result.v1")` is read from its registry
- **THEN** it reports the type as unregistered with no floor
- **AND** the test that verifies this is `TestIndexingProfileFor`

#### Scenario: a component holding the key separator is refused at registration

- **WHEN** a registration declares `Domain: "bad.domain"` (or a category or version containing `.`)
- **THEN** `Register` returns an error naming the separator and stores nothing — the key could never round-trip through
  `Key()`, so the error belongs at boot, not at the first `Create`
- **AND** `types.Type.Validate` is the one owner of that component grammar
- **AND** the test that verifies this is `TestRegisterRejectsMalformedComponent` (and `TestTypeValidateOwnsComponentGrammar`)

#### Scenario: a factory that disagrees with its registration is refused

- **WHEN** a registration's factory produces a payload whose `Schema()` returns a different domain, category, or version
- **THEN** `Register` returns an error naming both tuples
- **AND** the test that verifies this is `TestRegisterRejectsSchemaMismatch`

#### Scenario: every tier's target, binary and gate are the ones its artifacts carry

- **GIVEN** the tier table above
- **WHEN** each row is read against `docker/compose/*.yml` and `docker/Dockerfile`
- **THEN** the named service's `build.target` is the row's target, that target's image runs a binary built from the row's Go
  package, and the service sets exactly the row's `SEMSTREAMS_E2E_*` variables
- **AND** `docker/Dockerfile` carries exactly two runnable targets, `production` and `e2e`, and no `-tags=` anywhere, so no
  third binary shape exists for a tier to boot
- **AND** every compose service built from `docker/Dockerfile` appears in the table and every row names a service that exists
- **AND** no compose file mentions a `SEMSTREAMS_E2E_*` variable its rows do not declare — including in an overlay service
  with no `build:` block, since compose merges `environment:` across `-f` files, and including in a comment
- **AND** each declared variable carries a nonempty LITERAL value, every value containing `$` counting as empty because the
  guard cannot establish what the host interpolates it to, since the E2E binary enables an option only on a nonempty
  value, so `NAME=` leaves the tier's proof silently unarmed
- **AND** no two Dockerfile targets share an `image:` tag, so a build for one tier cannot leak into another
- **AND** the test that verifies this is `TestE2ETierTableMatchesComposeAndDockerfile`

#### Scenario: an E2E-only hook cannot reach the production build

- **GIVEN** the production root named by the table's `production` rows
- **WHEN** its non-test import closure is read as `go list -deps` reports it
- **THEN** it holds no package under `test/e2e/harness`, `internal/e2eboot`, `internal/e2eslowconsumer`,
  `examples/processors`, or `cmd/e2e-semstreams`, so the shipped binary links no harness and can enable no option
- **AND** the converse holds for the E2E root: its closure holds every package under `test/e2e/harness`, plus
  `internal/e2eboot`, `internal/e2eslowconsumer`, `cmd/e2e-semstreams/fixtures` and `cmd/e2e-semstreams/mission`, so a
  hook cannot be left stranded where no binary reaches it
- **AND** the test that verifies this is `TestProductionRootClosureHoldsNoE2EHarness`


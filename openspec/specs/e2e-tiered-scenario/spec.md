# e2e-tiered-scenario Specification

## Purpose
The tiered e2e scenario (`test/e2e/scenarios/tiered*.go`, `cmd/e2e --scenario tiered --variant <structural|statistical|semantic>`)
is the framework's own end-to-end gate over the ingest → entity → graph store → query path. It exists so that a green
per-PR tier is evidence: a stage that runs in a per-PR variant either asserts the outcome it exists to detect or is
declared a recorder, and a probe's deadline is never mistaken for an empty result. Which variants are per-PR is the
E2E Ladder's decision (`.github/workflows/e2e-ladder.yml`); which stages the semantic path-only run skips is #1117's.
## Requirements
### Requirement: A per-PR stage never passes on the outcome it exists to detect

Every stage the tiered scenario runs in a per-PR variant MUST return a non-nil error when the outcome the stage exists
to detect occurs, so that `Result.Success` is false and the e2e binary exits 1. A `Result.Warnings` entry SHALL NOT be
a per-PR stage's only record of that outcome. A stage whose detected outcome is owned by a model's latency or answer
quality, or by an engine the tier's configuration disables, SHALL leave the per-PR variant's stage table (by its
`variants` list or by its row) with the reason recorded in the stage-table comment, rather than warn.

Recorder exception: a stage, or one arm of a stage, declared RECORDER in its stage-table comment records its
measurement into `Result.Metrics`/`Result.Details` without gating, and MUST fail only on an unreachable transport or a
failed read. The declared recorders are B0 `validate-thematic-answer-eval`, B2 `validate-partition-colocation`,
`validate-llm-enhancement`'s enhancement-throughput and summary-quality arms, `validate-community-structure`'s
ground-truth arm, and `verify-search-quality`'s average-score arm (every variant) and known-answer arm (semantic only).
A recorder SHALL NOT be added without the declaration.

#### Scenario: The detected outcome occurs in a per-PR variant
- **GIVEN** a stage in a per-PR variant whose probe returns nothing, or whose read reports the framework outcome the stage exists to detect
- **WHEN** the tiered scenario executes that stage
- **THEN** the stage returns a non-nil error, `Result.Success` is false, `Result.Error` names the stage, and the e2e binary exits 1.

#### Scenario: A declared recorder observes a poor measurement
- **GIVEN** a stage or arm declared RECORDER in the stage table
- **WHEN** its measurement is poor (a low enhancement count, a ground-truth violation)
- **THEN** the tier stays green, the measurement is present in `Result.Metrics` or `Result.Details`, and the violation is in `Result.Warnings`
- **AND** an unreachable transport or a failed read still fails the stage.

#### Scenario: A model-owned or config-disabled outcome is not graded per-PR
- **GIVEN** a stage whose detected outcome depends on the small model's latency or answer, or on an engine the tier config disables
- **WHEN** the per-PR variant's stage list is built
- **THEN** that stage is absent from it, and the stage-table comment records why.

### Requirement: A probe's deadline is reported distinctly from an empty result

When a stage's probe fails at its client deadline, the stage's error MUST carry the transport error verbatim; when the
probe returns an empty result, the error MUST be the stage's own sentence. The two outcomes SHALL never share a
message. A `globalSearch` probe whose assertions do not read `communitySummaries` or `answer` SHALL send
`includeSummaries: false`, so it never pays for synthesis it does not check. Probes that do assert on synthesized fields
SHALL take their client deadline from the shared `globalSearchClientTimeout` helper so a variant overlay can raise it
from a measurement.

#### Scenario: The probe hits its client deadline
- **GIVEN** a probe whose `http.Client` deadline expires before headers arrive
- **WHEN** the stage fails
- **THEN** the error contains "Client.Timeout exceeded" and does not read as an empty result.

#### Scenario: The probe returns nothing
- **GIVEN** a probe that returns HTTP 200 with zero entities
- **WHEN** the stage fails
- **THEN** the error states that the query returned no entities and carries no deadline text.
```


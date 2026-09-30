# e2e-tiered-scenario

## MODIFIED Requirements

### Requirement: A per-PR stage never passes on the outcome it exists to detect

Every stage the tiered scenario runs in a per-PR variant MUST return a non-nil error when the outcome the stage exists
to detect occurs, so that `Result.Success` is false and the e2e binary exits 1. A `Result.Warnings` entry SHALL NOT be
a per-PR stage's only record of that outcome. A stage whose detected outcome is owned by a model's latency or answer
quality, or by an engine the tier's configuration disables, SHALL leave the per-PR variant's stage table (by its
`variants` list, by its row, or — for the semantic variant's path-only run — by its `quality` marker) with the reason
recorded in the stage-table comment, rather than warn.

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
- **WHEN** the per-PR variant's stage list is built (for the semantic variant, with `PathOnly` set)
- **THEN** that stage is absent from it, and the stage-table comment records why.

## ADDED Requirements

### Requirement: The semantic path-only run skips its quality stages and records them as skipped

When `TieredConfig.PathOnly` is set (`--path-only`, or a non-empty `E2E_PATH_ONLY`), the tiered scenario MUST omit from
the semantic variant's stage list every row marked `quality` in the stage table and no other row, record the omitted
names in `Result.Details["path_only_skipped_stages"]` with the reason in `Result.Details["path_only_skip_reason"]`, and
write no `<stage>_duration_ms` metric for an omitted stage. The marked rows SHALL be `validate-llm-enhancement`,
`validate-thematic-answer-eval`, and `validate-globalsearch-known-answer`. With `PathOnly` unset the stage list SHALL be
the variant's full list.

#### Scenario: The path-only run skips exactly the marked stages
- **GIVEN** `--variant semantic` with `PathOnly` set
- **WHEN** the stage list is built
- **THEN** it is the full semantic list minus the three marked rows, in the same order, and the run prints `[PATH-ONLY] skipping 3 quality stages` naming them.

#### Scenario: A skipped stage is recorded as skipped, never as passed
- **GIVEN** a path-only run that completes
- **WHEN** the results JSON is read
- **THEN** `details.path_only_skipped_stages` lists the three names, `config.path_only` is true, and no `validate-thematic-answer-eval_duration_ms` (or sibling) metric exists.

#### Scenario: The flag unset is the full variant
- **GIVEN** `--variant semantic` with `PathOnly` unset and `E2E_PATH_ONLY` empty
- **WHEN** the stage list is built
- **THEN** it is byte-identical to the list before this change (44 rows), and `details.path_only_skipped_stages` is absent.

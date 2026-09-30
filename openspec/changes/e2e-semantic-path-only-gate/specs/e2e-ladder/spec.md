# e2e-ladder

## Purpose
The E2E Ladder (`.github/workflows/e2e-ladder.yml`) is the per-PR end-to-end gate over the ingest → entity → graph
store → query path this repository owns: which tier variants run on every pull request, what each job sets, and what a
green job is evidence of. Which stages a variant runs belongs to `e2e-tiered-scenario`; which checks the `main`
ruleset requires is the owner's edit outside the tree.

## ADDED Requirements

### Requirement: The semantic path-only job runs once on every pull request

The ladder MUST run job `e2e semantic (path-only)` on every `pull_request` and `workflow_dispatch`, executing
`task e2e:semantic` exactly once with `E2E_PATH_ONLY=1` after reserving the e2e host ports, with `timeout-minutes` set
from a recorded measurement whose margin the job comment states, and uploading the results JSON as an artifact whether
or not the run passed. A green job is evidence that the framework-owned semantic path (compose boot of semembed and
seminstruct, ingest, embeddings, communities, gateway, batch read, rules) completed with every path assertion passing;
it SHALL NOT be read as evidence of answer or summary quality, which the full `task e2e:semantic`, `:8b`, and
`:frontier` runs measure pre-tag.

#### Scenario: A pull request is opened
- **GIVEN** a pull request against `main`
- **WHEN** the ladder runs
- **THEN** `e2e semantic (path-only)` runs once, alongside `e2e statistical` and `e2e slow consumer attribution`, and no measurement-only semantic job exists.

#### Scenario: The job's results show what it skipped
- **GIVEN** a completed job
- **WHEN** its artifact is read
- **THEN** the results JSON has `config.path_only: true` and lists the three skipped quality stages.

#### Scenario: A path assertion fails
- **GIVEN** a framework-owned stage returns an error
- **WHEN** the job runs
- **THEN** the job is red and the log's `Result.Error` names the stage.

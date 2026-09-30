# e2e-ladder

## Purpose
The E2E Ladder (`.github/workflows/e2e-ladder.yml`) is the per-PR end-to-end gate over the ingest → entity → graph
store → query path this repository owns: which tier variants run on every pull request, what each job sets, and what a
green job is evidence of. Which stages a variant runs belongs to `e2e-tiered-scenario`; which checks the `main`
ruleset requires is the owner's edit outside the tree.

## ADDED Requirements

### Requirement: The semantic path-only job runs once on every pull request

The ladder MUST run job `e2e semantic (path-only)` on every `pull_request` and `workflow_dispatch`, executing `task
e2e:semantic` exactly once with `E2E_PATH_ONLY=1` after reserving the e2e host ports, with `timeout-minutes` set from
a recorded measurement whose margin the job comment states. A green job is evidence that the framework-owned semantic
path (compose boot of semembed and seminstruct, ingest, embeddings, communities, gateway, batch read, rules) completed
with every path assertion passing. It SHALL NOT be read as evidence of answer or summary quality, nor that the
framework's model-client calls (the community summarizer into COMMUNITY_SUMMARIES, `synthesizeQueryAnswer`) returned —
seminstruct is proven per-PR only by its compose boot; and `searchGraph` and the summarized-branch entity-digest
labels are not covered per-PR. The full `task e2e:semantic`, `:8b`, and `:frontier` runs measure those pre-tag.

#### Scenario: A pull request is opened
- **GIVEN** a pull request against `main`
- **WHEN** the ladder runs
- **THEN** `e2e semantic (path-only)` runs once, alongside `e2e statistical` and `e2e slow consumer attribution`, and no measurement-only semantic job exists.

#### Scenario: The job's log shows what it skipped
- **GIVEN** a completed job
- **WHEN** its log is read
- **THEN** it contains one `[PATH-ONLY] skipping 3 quality stages` line naming them and a `[41/41]` final stage counter, and no stage named there has a `completed in` line.

#### Scenario: A path assertion fails
- **GIVEN** a framework-owned stage returns an error
- **WHEN** the job runs
- **THEN** the job is red and the log's `Result.Error` names the stage.

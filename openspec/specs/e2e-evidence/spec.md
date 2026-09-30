# e2e-evidence Specification

## Purpose

Define how selected E2E checks become run-bound required proof, including explicit scope, behavioral observations,
failure propagation, provenance, incomplete evidence and diagnostic outcomes.

## Requirements
### Requirement: Selection determines proof scope

The E2E runner SHALL resolve a selection to explicit execution members, named required checks, diagnostics and
intentional exclusions before execution. Unknown selections and unknown variants SHALL fail. A requested required
suite with zero required members or an unadopted required member SHALL fail before costly execution. An exclusion
SHALL NOT count as passed or completed evidence.

CLI empty selection and `all` SHALL mean the two core scenarios, distinct from the larger `task e2e:core`.
`semantic` SHALL resolve to the explicit semantic variant and `rules` to structural through the same flag-preserving
resolver as `tiered --variant ...`. Adopted task suites SHALL preserve caller output and endpoint configuration.

The initial adopted task composite SHALL contain core, structural, statistical, semantic and agentic, with a
membership-descriptive name. `e2e:all` MAY remain an alias only when it prints that exact scope and exclusions.
Slow-consumer SHALL remain separately adopted in its existing CI job. Composite exclusions SHALL explicitly name
slow-consumer, lessons, research-graph direct/execute, deep-research, CRUD-tools, ops, lifecycle, throughput,
heavy/fallback semantic variants and the live OpenAI adapter. Exclusion from this composite SHALL NOT remove an
independent CI or release obligation.

An unadapted individual scenario MAY execute with its existing failure semantics only when its pre-execution and
completion output and artifact identify its evidence as unattested. Its successful execution SHALL NOT satisfy a
named required selection or be reported as complete required proof. Existing release obligations and capability
owners SHALL remain unchanged; this bounded adoption SHALL NOT assert full legacy migration.

#### Scenario: A special alias preserves its selected variant

- **GIVEN** `--scenario semantic` with explicit endpoint and output options
- **WHEN** selection is resolved
- **THEN** semantic is explicit rather than auto-detected
- **AND** the same options reach execution and writing as for `tiered --variant semantic`.

#### Scenario: Required membership is missing

- **GIVEN** an unknown, empty or partly unadopted required suite
- **WHEN** the runner resolves it
- **THEN** the command fails before expensive scenario setup
- **AND** no excluded or legacy execution is substituted as passed required evidence.

#### Scenario: Legacy execution remains visible

- **GIVEN** an individually selected legacy scenario outside the initial adopted required sets
- **WHEN** its existing checks complete successfully
- **THEN** execution may exit successfully with explicit unattested disposition
- **AND** neither its count nor legacy Success establishes complete required proof.

### Requirement: Required observations determine success

One shared finalizer on the existing scenario Result SHALL own required-evidence acceptance. Expected check IDs and
required/diagnostic classification SHALL be declared before observations. Duplicate declarations, unknown IDs,
duplicate or contradictory observations and empty required membership SHALL fail required finalization.

Complete required-proof success SHALL require exactly one passed behavioral observation for each selected required
ID, valid invocation identity and no setup, execution, validation, cleanup or required-write failure. Missing,
failed or skipped required observations SHALL prevent success. A callback returning nil SHALL NOT constitute a
behavioral observation. Required observation errors SHALL be failed observations with a reason.

Scenario observation sites SHALL compare the independently expected input identity/value with the observed consumer
result before recording passed. Shell-owned checks SHALL record the actual comparison at the existing assertion
site; banners or a successful shell block SHALL NOT manufacture behavioral proof. Task SHALL retain execution and
lifecycle authority; the reporting seam SHALL only serialize and finalize records.

`AssertionsRun` SHALL be supplemental, derived for adopted Results from evaluated required observations (passed plus
failed), excluding skipped, absent and diagnostic-only observations. It SHALL NOT decide required acceptance or
claim equivalence with historical successful-stage totals.

#### Scenario: Warning-only observation cannot satisfy a required check

- **GIVEN** an adopted required tool or streaming-chunk check
- **WHEN** observation fails, the required metric is absent, or required chunks are zero
- **THEN** that check is failed and the required result is unsuccessful
- **AND** a nil callback return cannot convert it into passed evidence.

#### Scenario: Current core pass-through output is observed

- **GIVEN** protocol-flow uses empty criteria and mappings and the test retains its successfully sent run/sequence/value set
- **WHEN** output for that exact run is observed
- **THEN** at least MinProcessed distinct sent sequence IDs must be present
- **AND** every selected record must be valid JSON with a sent sequence and its expected fields and values
- **AND** duplicate lines cannot inflate the distinct minimum
- **AND** count/retrieval failure cannot substitute component health for dataflow proof.

#### Scenario: Required identity comes from the consumer

- **GIVEN** a controlled statistical fixture/query or agentic task/tool request
- **WHEN** its required check executes
- **THEN** the observed consumer result must match the fixture entity or task/loop/request identity
- **AND** unrelated aggregate counts cannot satisfy that obligation.

### Requirement: Evidence cannot improve by omission

For a fixed declared required set and invocation, deleting or renaming a required observation, replacing passed
with failed or skipped, or omitting a selected member SHALL NOT create success. Diagnostic observations SHALL NOT
fill missing required IDs. A later pass SHALL NOT overwrite an earlier failed observation; repeated intentional
rounds SHALL have distinct declared IDs. Missing evidence SHALL be derived from declarations versus observations,
not hidden by a second mutable count.

The initial named proof SHALL cover core health, current pass-through count/content/identity, core task
readiness/heartbeat/authority/shutdown/early-cancel/preidentity/graph-roundtrip observations, inference components
and graph-roundtrip identity, structural zero-ML execution, statistical controlled-search identity, semantic
semembed/controlled-search identity, agentic terminal/task-loop/tool-request/streaming-chunk observations, and the
existing individual slow-consumer assertions. Existing fatal checks outside this initial proof set SHALL retain
fatal behavior and SHALL NOT be silently deleted or reclassified by adopting named evidence.

#### Scenario: Removing an observation cannot make a failing run pass

- **GIVEN** a fixed nonempty required set and a failed or missing required observation
- **WHEN** an observation is removed, renamed or replaced by a diagnostic result
- **THEN** finalization remains unsuccessful
- **AND** the missing/failed obligation remains identifiable.

#### Scenario: Attempted overwrite preserves the failure

- **GIVEN** a failed observation for a declared check ID
- **WHEN** another observation attempts to record passed for that same ID
- **THEN** the duplicate is rejected
- **AND** the original failure remains in the retained record.

### Requirement: Observation identity is preserved

Run and member identity SHALL bind declarations, observations and persisted records. A foreign run, scenario,
variant or round SHALL NOT satisfy a selected member. Capability-specific expected entity, loop, request and
sequence identities SHALL originate from controlled inputs and be compared with actual observations. Unknown or
unavailable identity SHALL be explicit and SHALL NOT be replaced with an unrelated inferred identity.

The core pass-through proof SHALL NOT imply selective filtering, mapping, complete UDP delivery, exactly-once
output or ordering. Structural zero-clustering proof SHALL mean absent clustering execution/component activity,
not absence of all stored community records.

#### Scenario: A prior run leaves matching-looking output

- **GIVEN** old output or an artifact from another invocation
- **WHEN** the current run checks its required membership or core output
- **THEN** foreign run identity cannot satisfy its required observation or count.

#### Scenario: The configured core path is pass-through

- **GIVEN** inputs both below and above value50 under empty filtering criteria
- **WHEN** output is validated
- **THEN** expected content comes from the sent input set
- **AND** no selective greater-than50 rejection rule is inferred from historical comments.

### Requirement: Failure reaches the outer gate

For adopted required proof, scenario/process/required-check/cleanup/required-write failures SHALL produce nonzero
proof commands and propagate through existing Task exit handling to the calling job. Finalization SHALL NOT clear
an earlier failure when cleanup or writing also fails. Required artifact absence SHALL prevent complete proof.
Task SHALL remain execution authority; no reporting command SHALL execute arbitrary work or coordinate processes.

Reporting initialization SHALL occur after existing prerequisite dependencies succeed. Build and check-ports
prerequisites, including cleanup-before-port-probe, SHALL preserve their existing order. Controlled failures before
report initialization SHALL retain outer status/logs without a promise of structured JSON. An initialized composite
SHALL retain a child's prerequisite failure as a failed member with missing report and available status/log reference.
After initialization, existing rc handling SHALL perform cleanup before final report completion and preserve all
observed failures. Uncatchable process/host death MAY leave only incomplete artifacts and logs.

#### Scenario: Direct task fails before its writer can start

- **GIVEN** build, prerequisite cleanup, port preflight or report initialization fails
- **WHEN** a direct adopted task terminates
- **THEN** its outer status is nonzero and available logs are retained
- **AND** no structured run artifact is claimed to exist or to prove success.

#### Scenario: Composite child fails during prerequisites

- **GIVEN** a composite initialized its report and a selected child fails before child initialization
- **WHEN** the composite finalizes
- **THEN** it records the child's failure and absent child report with the available log reference
- **AND** required suite acceptance fails.

#### Scenario: Cleanup fails after behavioral checks passed

- **GIVEN** all required observations passed
- **WHEN** cleanup fails or required final writing fails
- **THEN** the outer proof task fails
- **AND** successful behavioral observations do not mask the failure.

### Requirement: Run evidence binds one invocation

The existing Result/TestRun writer SHALL retain explicit selection, expected/completed checks, outcomes, invocation
identity and provenance. After successful initialization it SHALL write an incomplete record before the test body
and attempt terminal evidence for every controlled body return, including failed setup/execution/validation/cleanup.
Failed partial observations SHALL be retained. Writer failures SHALL be visible and SHALL NOT claim successful
persistence. Unique member records and atomic aggregate writes SHALL prevent sibling/prior-run overwrite; no restart,
resume or guaranteed final write after host/process death is implied.

Evidence SHALL record exact executed E2E and owned child-command argv, absolute working/output paths, UTC times,
run/member IDs, source SHA and dirty status, patch/relevant-untracked-input digests where applicable, runner
binary/build identity, observed application
image/binary identity, selected Compose/profile/config/fixture digests, effective nonsecret settings, exit/status,
and log/artifact digests. Secrets SHALL NOT be retained. Unavailable provenance SHALL remain explicit, rather than
using runner identity as application identity. Scripted dependencies and their proof scope SHALL be named.

Task aggregates SHALL distinguish their resolved Task target from an observed original launcher command line.
When original outer Task arguments are not observed, the report SHALL explicitly identify that launcher history as
unavailable; it SHALL NOT label reconstructed arguments as exact. This launcher-history limit alone SHALL NOT prevent
complete evidence when the required child execution, source, configuration, application and behavioral evidence is
complete. Other unavailable required provenance SHALL still prevent complete proof.

Existing CI jobs SHALL retain available run artifacts on failures as well as success and expose artifact absence.
Log-only bootstrap and incomplete/crashed/legacy evidence SHALL NOT be accepted as complete required proof.
Release-candidate authorization SHALL remain governed by release-candidate-proof; local JSON SHALL NOT authorize a tag.

#### Scenario: Execution fails with partial observations

- **GIVEN** a successfully initialized invocation
- **WHEN** a controlled execution failure occurs after some observations
- **THEN** the final artifact retains those observations, the missing set and failure
- **AND** the runner attempts serialization before returning the failed outcome.

#### Scenario: Original Task launcher arguments are unavailable

- **GIVEN** a Task wrapper knows the resolved suite but did not observe the original launcher arguments
- **WHEN** it emits a report
- **THEN** the resolved target and unavailable launcher history are explicitly distinguished
- **AND** actual executed test/child commands and required source/configuration evidence remain recorded
- **AND** that declared launcher-history limit alone does not change behavioral or evidence acceptance.

#### Scenario: Application provenance is unavailable

- **GIVEN** a remote or local invocation cannot observe application/configuration identity
- **WHEN** evidence is emitted
- **THEN** the unavailable fields are explicit and full proof is incomplete
- **AND** a guessed tag, empty value or runner SHA does not establish application provenance.

### Requirement: Domain projections follow final outcome

TestRun required-proof summaries and emitted Result/TieredResults metadata SHALL derive from the same finalized
required and lifecycle outcome. Typed tier data SHALL remain an analysis projection, not an independent acceptance
owner. Legacy TestRun/typed files SHALL remain readable as unattested when required-proof identity is absent;
old Success or AllPassed values SHALL NOT imply complete named evidence. Required empty membership SHALL NOT
produce vacuous AllPassed proof.

#### Scenario: Final semantic validation changes the result

- **GIVEN** domain observations were built before the final validation step
- **WHEN** final validation fails
- **THEN** emitted Result, TestRun proof summary and TieredResults metadata agree on failure
- **AND** no earlier copied success escapes through the writer.

#### Scenario: An old report has only aggregate success

- **GIVEN** a legacy file without the named required-proof contract
- **WHEN** a reader loads or compares it
- **THEN** it remains distinguishable as unattested
- **AND** its aggregate success cannot satisfy a current required selection.

### Requirement: Diagnostics remain explicit

Declared diagnostic observations SHALL be reported with actual passed, failed or skipped outcome and reason.
A diagnostic failure MAY leave required acceptance successful when all required evidence and lifecycle obligations
are satisfied. It SHALL NOT be relabeled passed or silently promoted to required. B0 thematic quality, B2 partition
co-location and agentic TTFT SHALL remain diagnostic under this change. No new model-quality or selective-filter
threshold is authorized by the shared evidence contract.

#### Scenario: A diagnostic observation fails

- **GIVEN** all required checks pass and a declared diagnostic cannot be observed
- **WHEN** the run finalizes
- **THEN** the diagnostic failure and reason remain visible
- **AND** it does not fill a required ID or independently turn the required checks red.

#### Scenario: Streaming and TTFT have different obligations

- **GIVEN** streaming chunks are required and TTFT is diagnostic
- **WHEN** TTFT observation fails while required chunk evidence passes
- **THEN** TTFT is reported failed or skipped with a reason
- **AND** required success is decided by its actual declared obligations, not by silently changing TTFT classification.

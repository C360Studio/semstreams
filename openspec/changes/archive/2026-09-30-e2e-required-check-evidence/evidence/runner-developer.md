# #1222 runner slice handoff

Workspace: `/Users/coby/.codex/worktrees/e2e-required-proof/semstreams` at base
`fe6e2cc03e16f5db47e293f55939548572f204cc`, with uncommitted shared changes.
Owned source: `cmd/e2e/main.go`, `main_test.go`, `runner.go`, `runner_test.go`,
`selection.go`, `selection_test.go` only. No Taskfile, workflow, shared scenario,
OpenSpec, GitHub or commit mutation was made by this slice.

## Behavior implemented

- One CLI resolver makes default/all mean core-health + core-dataflow; semantic
  maps to explicit tiered semantic; rules maps to structural. Conflicting or
  unknown variants fail before Setup. The known semantic-fallback diagnostic
  remains selectable but unattested. All resolved paths preserve caller options.
- The CLI allocates a run ID before constructors, and the selected-run path
  persists one initial incomplete and one terminal aggregate through the
  existing `results.Writer`. It uses Result declarations/finalization, retains
  partial/nil/setup/execute/teardown failures, and returns the Writer-finalized
  exit. A required selection without output directory or declared checks
  refuses before Setup. Legacy execution remains explicitly unattested.
- Synthetic direct-run fixtures cover complete proof with valid synthetic
  provenance; missing provenance or observation; teardown failure after a
  passed observation; initial write failure before Setup; nil Result and
  required observation omission. Failed typed projection follows final status.

## Verification actually observed

- Initial selection RED: `go test ./cmd/e2e -run '^TestResolveScenarioSelection' -count=1`
  failed to compile because `resolveScenarioSelection` did not exist. This was
  API absence only, not a compiled behavioral sensitivity check.
- Nil-result RED: `go test ./cmd/e2e -run '^TestRunScenarioFailsClosedOnNilResultAndTeardownFailure' -count=1`
  panicked at the prior `main.go:532` Result dereference.
- Compiled omission RED: `go test ./cmd/e2e -run '^TestRunScenarioRejectsDeclaredButUnobservedRequiredCheck' -count=1`
  returned `actual: 0` despite a declared required `controlled-output` check
  having no observation. The old log said `Scenario completed successfully`
  with `assertions_run=0`.
- Initial selected-run RED: `go test ./cmd/e2e -run '^TestSelectedRunner' -count=1`
  failed to compile because `runSelectedScenarios` did not exist. API absence.
- Current GREEN: `go test ./cmd/e2e -count=1` exit 0,
  retained in `/private/tmp/semstreams-1222-runner-green.log`.
- `git diff --check -- cmd/e2e` exit 0. Formatting was applied with `gofmt`.

The RED outputs above were visible in tool returns but were not separately
saved as standalone files at the time. They must not be presented as a retained
baseline/mutant/restored experiment. The GREEN log is retained. A later
compiled mutation with cp backup/hash and stable tests remains needed for the
accepted mutation requirement.

Snapshot SHA-256 (uncommitted content, including new files):

```text
8181c5950fe1295bf56d4726391254733cbb106d6ff383244871d648bd2c0873  cmd/e2e/main.go
ba9a256823d901f7fe8909f1902b651eec9e3bce72bd8f6b03c7a730e5bb9a29  cmd/e2e/main_test.go
eee05b0c830a04d7bd1391c89058516da89d2a3ce1076649e8c97f1228f86243  cmd/e2e/runner.go
85a03436b5d0cf8614624bb8779c3d1a13bc998fef3091d0ae2b100e25166408  cmd/e2e/runner_test.go
2615ed372246af83ae6b3e7d0cf072ea511601edcd38f01cdc1d450728515de4  cmd/e2e/selection.go
35e94d6fdc4f16decec6486dabe33e9bf1b87889688166e02725643112c4bdcb  cmd/e2e/selection_test.go
```

## Explicit limits and next integration

Required production selections currently refuse because real scenario types
have not yet implemented `CheckRequirements` and explicit EvidenceRunID /
EvidenceMemberID config fields. The evidence developer owns those scenario
adoptions and was told the exact `core-health`, `core-dataflow` default/all
member IDs. CLI direct runs without observed app/config provenance emit
unavailable values and cannot claim complete required proof. Task must observe
and supply actual provenance; no app digest was guessed.

No Docker, integration, paid LLM, full push or E2E gate was run. These unit
fixtures prove runner/writer propagation only. They do not establish real core,
statistical, agentic or Task wrapper behavior. CLI `--scenario all` retains its
two-member scope; `task e2e:core` has additional shell and graph obligations.

PBT decision for this slice: the Result/Writer owner uses bounded generated
properties for required-set acceptance. This runner slice uses deterministic
process-path examples and does not add a second property model.

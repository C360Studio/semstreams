# Post-GS-01 R1c retry-truth control record

Status: **current successor control; R1c complete**.

## Identity and merged prerequisites

- baseline: `313579fd8b66f0af9c62a8a05d9b9f9fffa486b4`
- PR #901 / `f11f03b9`: catalog read-only acquisition merged
- PR #902 / `dd02a715`: R1a package-local reader narrowing merged
- PR #904 / `313579fd`: R1b lifecycle poison localization merged
- passed post-R1b inventory:
  `docs/proposals/post-gs01-post-r1b-foundation-inventory.md`
- inventory SHA-256: `e0f6479e1ace79a60b487fb32d7e001eb5192874fa9c7c81742ff3a984b74720`
- accepted decomposed design SHA-256:
  `e1d7c47898824b4bfdca33a4e53da75dd4d59af147315ba2871f2cbebe2c017f`
- accepted roadmap amendment SHA-256:
  `85c837aca8ccbf38483848f322c85aba929596f24f5e517b125b6bc42a883e5b`
- design review SHA-256:
  `ed1fc0a4ae4cd87225ff8ca6d6728e07e84f56deb8e59891bebf6d0f5a8b15d2`
- owner acceptance: `post-gs01-r1-decomposed-execution-design-approval.md`

The accepted design, amendment, review, and approval remain frozen historical artifacts. Their stale draft headers are
not edited; this record states their accepted identities and present execution status.

## R1c boundary

R1c is a retry-truth closure, not runtime implementation:

- characterize projection mismatch as exactly one exact read plus one mutation, with no third request;
- characterize rule mismatch as one reconciler invocation, visible classified failure, and no replay or recomputation
  of the old `ExecutionContext`;
- characterize lifecycle's existing owner-local transition loop against changed authority, including fresh phase,
  edge, occurrence chain, projection, mutator output, desired request, and expected revision;
- correct current rule, lifecycle, and adopter-facing retry truth; and
- add no production code, API, configuration, metric, helper, knob, coordinator, E2E, shim, or deprecated path.

Rule and lifecycle intentionally differ. Rule has no safe authority-independent replay. Lifecycle can reconstruct its
transition intent from current authority and therefore retains its bounded owner-local loop. `commit_unknown` is never
automatically retried by either path.

## Materialized change-set census

Authored Go changes are confined to these test files:

- `pkg/projection/mutation_client_test.go`;
- `processor/rule/actions_reconcile_test.go`; and
- `pkg/lifecycle/manager_test.go`.

Current-truth changes are confined to the canonical rule and lifecycle specs, the governed-state concept guide, this
successor control record, and the R1c implementation report. The passed post-R1b inventory remains unchanged at its
recorded content identity.
There is no production Go delta and no API, configuration, metric, retry helper, retry knob, coordinator, E2E, shim,
deprecated path, bucket, stream, service, or status-key addition.

## Ruling conformance

| Ruling | Exact evidence | Status / deviation |
|---|---|---|
| Projection: one read and one mutation | `pkg/projection/mutation_client_test.go:270` | PASS |
| Rule: visible mismatch and no replay | `processor/rule/actions_reconcile_test.go:350` | PASS |
| Lifecycle: rebuilt fresh intent | `pkg/lifecycle/manager_test.go:560` | PASS |
| Lifecycle: reject changed bad chain | `pkg/lifecycle/manager_test.go:657` | PASS |
| Rule current truth | `openspec/specs/rule-projection-mutations/spec.md:82` | PASS |
| Lifecycle current truth | `openspec/specs/lifecycle/spec.md:77` | PASS |
| Adopter-facing truth | `docs/concepts/28-governed-semantic-state.md:42` | PASS |
| Gate execution record | `docs/proposals/post-gs01-r1c-implementation-report.md:61` | PASS |
| Zero production-runtime delta | `docs/proposals/post-gs01-r1c-retry-truth-control.md:52` | PASS |
| Prohibited surfaces absent | `docs/proposals/post-gs01-r1c-retry-truth-control.md:52` | PASS |
| Deviations | none | NONE |

## Verification record

The durable integrator-observed execution evidence is recorded in
`docs/proposals/post-gs01-r1c-implementation-report.md:61`. The full unrestricted gate and audit correction are at
`docs/proposals/post-gs01-r1c-implementation-report.md:79`; reviewer approval and the reviewed no-E2E disposition are
recorded in that report's review and E2E disposition.

## Program stop and remap

The old mechanical `R1c -> R1d -> R1e -> R2` inheritance stops here.

- Old R1d is not executable as accepted: checked-in phantom `kv_read` rows and the #859/#862 declaration-model work
  falsify its configuration census and surface boundary.
- Old R1e remains a distinct message-logger access-boundary candidate, but it requires a fresh owner-approved slice
  after R1d remapping; it must not absorb #472, #587, framework authentication, or a generic diagnostics runtime.
- R2 MUST NOT begin from the old prerequisite wording. The owner must first record the remapped post-R1c order,
  reservations, and evidence gates.

No active OpenSpec task checkbox is amended by R1c. The active foundation task file records the earlier coordinated
GS-01 cutover and mixes historical merge truth with later successor notes; using it to track this test/spec-only slice
would make task truth less precise. This successor record is the current control surface until the owner approves the
remap.

## Rejected extraction ledger delta

| Candidate | Compared owners | Failed dimension | Disposition | Revisit evidence |
|---|---|---|---|---|
| Shared CAS retry | rule/lifecycle | policies differ | rejected | three identical owners |

## Satisfied completion gate and actual next gate

R1c's focused, contract, strict OpenSpec, full push, diff, and independent-review gates are satisfied. The actual next
gate is an owner-approved post-R1c remap. The inherited old R1d, R1e, and R2 sequence remains stopped until that remap
records executable boundaries and prerequisites.

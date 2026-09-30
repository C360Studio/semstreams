# #1222 consolidated Writer review

Mode: bounded implementation review of the four frozen Writer files, supplementing
approved R1/R2. Worktree `/Users/coby/.codex/worktrees/e2e-required-proof/semstreams`,
base `fe6e2cc03e16f5db47e293f55939548572f204cc`. Read against the accepted design,
active implementation handoff, Task/report clarification, and reconciled manifest
shape. No source edits, tests, gates, Git mutations or GitHub actions.

## Verdict and examined boundaries

**CHANGES REQUESTED: W1 and adjacent caller finding C5 below.** The mutation deferral
below is accepted independently. This is not approval of evolving reporters, Task
producers, constituent truth, assembled E2E, or the whole feature.

### HIGH W1 — writer.go:278 — terminal acceptance can change initialized checks

Mechanism: `writeRunAtomically` compares run identity and Config, but not the stored
members' CheckRequirements. `checkRequiredEvidence` subsequently validates only the
submitted Result requirements. Initialize a member with two required checks, then
submit the same run/config/member with only one check and its passing observation:
the missing obligation disappears and complete proof can be serialized. Changing a
Required flag or check identity has the same boundary gap. This is a static trace,
not an executed reproduction. The accepted Task clarification requires final records
to match initialized declarations; root confirmed that this includes the aggregate
acceptance boundary when initial declarations are stored.

Smallest correction: compare each submitted member's identity and exact requirement
set, including Required flags, against the retained initial declarations before
granting terminal complete proof. Drift must produce failed proof/nonzero exit while
retaining diagnostic terminal evidence. Preserve serialization of missing/failed
members; do not require equal observations/outcomes or add a selector/check catalog.
Test a removed check, changed identity/Required flag, unchanged valid transition, and
missing/failed members that still retain a failed terminal report.

Refutation: WriteMember/LoadMember enforce this and current report-finalize uses them,
so that caller has an additional guard. It does not close the exported WriteRun
acceptance boundary, which already binds initialization configuration and owns final
proof acceptance. Same-run aggregate correction permits outcome correction, not
shrinking initialized obligations.

### HIGH C5 — cmd/e2e/runner.go:157 — CLI initialization discards declared checks

Adjacent evolving consumer, outside the four-file freeze: the adopted CLI constructs
and validates each declaration at lines 157–160 but discards it. Initial WriteRun at
line 200 therefore receives no member declarations; execution later appends Results
at line 217. The private expected set protects that execution path, but the initial
artifact cannot express its declared proof obligations. Root confirmed the original
runner handoff requires that initialized representation.

Smallest correction: retain adopted declarations in their member slots before initial
WriteRun, then replace each slot with its observed Result rather than appending a
second member. Preserve legacy behavior and failed/nil/setup paths. Verify the actual
initial persisted report contains exact expected checks before Setup, and terminal
replacement neither drops obligations nor creates duplicate members. This also gives
Writer's W1 check a retained comparison source for direct CLI runs.

- `writer_task.go:170–205` binds member/manifest operations to the stored initialized
  nonterminal run, exact configuration and member declarations, and the Writer's
  resolved output directory. `:259–313` binds member identity and declarations, uses
  existing Result finalization, retains failed partial observations, rejects duplicate
  creation and loads the declared path rather than discovering a replacement.
  Tests at `writer_task_test.go:277` and `:321` distinguish retained failure from a
  declaration that legitimately becomes passed after its observation.
- `writer.go:149`, `:278`, and `:444–449` enforce paired parent identity, unchanged
  configuration including Task status applicability, and both observed zero Task
  command/cleanup statuses for complete proof. Nonzero statuses survive serialization;
  absent applicable status cannot mean successful zero. Direct/legacy callers retain
  their existing applicability and unattested semantics. `Compare` at `:641` carries
  both input evidence dispositions without converting execution comparisons to proof.
- `writer_task.go:66–127` verifies decoded child proof, exact parent/slot/selection,
  required member set and Task applicability; it hashes the same report bytes it
  decoded. Live verification also opens the child's referenced log/manifest and
  compares exact digests. Historical LoadRun continues without reopening machine paths.
  Child references are relative to the emitting Writer's absolute output directory;
  embedded references use the child's output directory. Relative Writer construction
  is normalized before Rel and covered by `writer_task_test.go:249`.
- `writer_task.go:210–255` retains exact JSON-object bytes at a Writer-derived basename,
  syncs/closes the temporary file, and atomically links without replacing an existing
  member/manifest. Duplicate attempts, including identical manifest bytes, fail.
  Every manifest error returns a zero reference. Manifest domain content remains
  with the reporter; no new generic persistence owner or interpretation layer appears.

Immediate present consumers were enumerated: `cmd/e2e/report.go:631,675,691,744,773`.
They use initialized declarations, retain a failed child observation, separately
reject nonzero observed child process exit, preserve the initialized snapshot for
manifest/member operations, and still attempt terminal aggregate writing after
manifest failure. These evolving consumers were inspected, not frozen or approved.

## Exact frozen snapshot

Hashes checked before and after review, under `test/e2e/results/`:

| File | SHA-256 |
| --- | --- |
| writer.go | 42f603b03daf46184ae80809b994771a073bedb5a315ed05251b48ecb732ab20 |
| writer_task.go | b70e3763a4f1f911b192fc6c300740dfc0ce53030f8fea4d68eaae3100a8c5bd |
| writer_test.go | 60262a27fe72f3affe1b60919b59963f5395df4c7aa30dfdf71c8a8f02195fbf |
| writer_task_test.go | cd313b10443862ebf2079a507b42583fa31a62f3336c02c96b61e11bb5c7681c |

Author checkpoint `/private/tmp/semstreams-1222-writer-consolidated.md` SHA-256
`ea4f36e80bd36119436da9bd75571524a6ac603734aa249c910af52c51dc4eae`.
The retained `writer_task.go` backup matches its frozen hash; rejected mutation
attempts did not alter source. Prior gopls workspace loading was unavailable because
of build-cache permission; bounded source searches and direct caller reads were used.

## Evidence and mutation disposition

Raw logs were read and hashed. Paths have prefix `/private/tmp/semstreams-1222-`:

| Artifact | SHA-256 |
| --- | --- |
| child-retained-red.log | 73c9a90d62b79ca915289ee0fb31625ae6706c8788fd337167e79ef04fd67b2b |
| child-retained-green.log | b521a68f178a14eff409cf8c565b4d39985f28467bd9a655cd0d3a0bbc62d7c4 |
| manifest-api-red.log | 79c2ef731e29adda6b7bf4a7df1a9e571784023d37451a6999fcb577f68f05af |
| manifest-api-green.log | 0c4674b09aceb880a494beca30423efb39b335ae7b2c2c9035758fa6386f77dd |
| writer-consumer.log | b52a6458678e727765aeda8aaeec0984fb4e1ddf44e9a5a67f64aa38c6c76793 |
| writer-consumer-race.log | b0c9ee449849d0088cfe075ff9ec561cb56c8d486fbaa669ac472663dac8895c |

Child pre-fix RED compiled and reached all four missing/tampered-byte assertions;
post-fix GREEN reached them successfully. This is behavioral regression evidence,
not an executed mutation. Manifest's first RED is missing-symbol compilation only.
Package GREEN reaches duplicate manifest refusal, retained failed-member refusal,
identity/status controls and native fuzz seeds. Consumer/race commands are recorded
by the author; consumer bytes were not frozen at those runs. Fuzz evidence is seed
replay, not exploration. The new fuzz targets assert byte/identity/object invariants;
fixed examples separately cover the bounded parent/status/duplicate cases.

Consequential integrity and failed-evidence retention trigger the canonical mutation
policy (`docs/contributing/01-testing.md:140–165`). **ACCEPTED BOUNDED DEFERRAL** of
the two denied experiments: replacing Link with Rename, and disabling the digest
comparison. Auto-review rejected each before edit because it would respectively
permit overwriting retained artifacts and weaken integrity verification. Neither
patch or mutant test ran; sensitivity to those exact mutations is **UNVERIFIED**.
The checkpoint retains both verbatim denials. No workaround is authorized here.

Deferral rationale: current guards are direct, source integrity is verified, compiled
pre-fix RED establishes that the retained-byte test assertions can fail, and passing
duplicate tests assert both refusal and preservation of failed/original bytes. These
reduce the concrete risk sufficiently to accept this bounded evidence deferral, without
claiming exact mutant detection. Remaining risk is unmeasured test sensitivity if
those two guards are later weakened. This is not a deferral of producer correctness,
required failing-path behavior, or assembled acceptance gates. No source-copy/overlay
experiment was performed or approved.

Retain raw evidence durably with the change before integration; local logs and this
review do not establish CI, Docker, integration, whole-consumer freeze or merge readiness.

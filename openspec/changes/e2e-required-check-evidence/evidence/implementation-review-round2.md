# #1222 Result/Writer implementation review — round 2

Mode: independent bounded implementation re-review, read-only. **APPROVE the R1/R2 corrections at this snapshot.**
No new BLOCKING/HIGH finding in this bounded correction review. This is not whole-change or merge approval.
The separate CLI C1/C2 findings, comparison disposition, actual producers, and adoption/proof remain pending.
No tests, source changes, Git mutations, GitHub writes, Docker or integration runs were performed by this reviewer.

## Scope and exact identity

Worktree: `/Users/coby/.codex/worktrees/e2e-required-proof/semstreams`.
Base: `fe6e2cc03e16f5db47e293f55939548572f204cc`.
Authority: accepted design `a31a5c690da763b124a7d1e1727836e57a270f943b5b61576216d7f386b9b48c`, active
e2e-evidence delta and implementation-handoff, plus root's explicit bounded reconciliation: Writer/LoadRun enforce
retained-reference shape; historical LoadRun does not reopen machine-local paths. Actual Task producers and parent
verification must establish retained bytes/digests and constituent manifest content in their later slice.

Compared the original R1/R2 mechanisms and declaration-boundary gaps with the full current Writer diff, focused
Result/finalizer ranges, and all current tests in these five files. Source hashes matched before and after:

| File | SHA-256 |
| --- | --- |
| test/e2e/scenarios/scenario.go | 55944b35bb995c94b72d2f3376c78d170d4bbb8b128e2f7a3149f2ec1c5248e4 |
| test/e2e/scenarios/evidence.go | 5ffa1c2f0416a3bdcd672f03c1ad4952cfa9998bd9c68e6ad270285f61069c60 |
| test/e2e/scenarios/evidence_test.go | ba466d6166d5cc6424a3ca608c36c43706b93cf26b99622ccb4c0849b0c9ac84 |
| test/e2e/results/writer.go | d958b35ee54ace7df3ca4c6bb0d11b01e7322989d87c4f251e7efd88dcd8a61e |
| test/e2e/results/writer_test.go | 60262a27fe72f3affe1b60919b59963f5395df4c7aa30dfdf71c8a8f02195fbf |

Author handoff: `/private/tmp/semstreams-1222-result-fixes.md`, SHA-256
`de46b428672f30c612cf09aaad3bfe717cc9c72a4e8375119e3e174e96d18f5a`.
Scenario.go/evidence.go are unchanged from round one. Other scenario/CLI files are outside this freeze and verdict.

## R1 disposition — corrected

- `writer.go:436–440` clears Structured on the shallow validation copy before FinalizeChecks. Inspected
  FinalizeChecks: all remaining changed scalar fields are on the copy; the requirements/observations/errors are
  read, and sorting applies to the locally built problems slice. Validation no longer writes through Structured
  into the retained child. Both evaluateRequiredProof's write-time and read-time callers use this same path.
- Terminal required WriteRun now finalizes the retained child at `writer.go:170–179`, while saving its previous
  Success value. A previously false result remains false and unattested even when every named check passed and
  Error/Errors are empty. A stale true result with an omitted observation becomes false through the finalizer.
- Metadata is projected after that decision at `writer.go:180–190`, followed by summary computation and required
  exit finalization before marshal. Child success and persisted typed success therefore agree for both original
  R1 examples. The existing check at `writer.go:444–445` refuses contradictory typed success for complete proof.
- Refutation: merely detaching the validation copy would have left stale true Results uncorrected; the retained
  finalization addresses that second direction. Merely re-finalizing without preserving prior false would have
  erased a false/no-error execution failure; the explicit snapshot prevents it. Ignoring FinalizeChecks' return
  at :174 does not discard its failure here: it retains Error/Success/EvidenceStatus and feeds the proof decision.

Tests `writer_test.go:233–249` inspect nonmutation in both directions. `:251–270` exercises false/no-error through
WriteRun/LoadRun, and `:272–289` exercises stale true with missing observation. Earlier `:212–231` covers a named
execution Error. All four are visible as passed in the retained author Writer log. These tests use independent
expected outcomes rather than calling the validator to manufacture expected values.

The standalone typed-file ordering defect identified in the CLI review (C2) is separate: changing Writer cannot
repair a typed file already emitted by CLI before Writer. R1 approval does not close C2.

## R2 disposition — corrected at the accepted reference-shape boundary

- `writer.go:144–151` resolves and records the Writer's actual absolute output directory in the existing
  Environment map; the author cannot substitute an unrelated output_dir on write.
- `requiredProvenanceProblems`, `writer.go:494–505`, requires absolute output_dir and both log and artifact-manifest
  path/digest pairs. Missing/unavailable paths and malformed SHA-256 values cannot establish complete proof.
  The permitted key set includes output_dir at :534; unknown keys remain refused.
- Both terminal WriteRun (:193–200) and decodeRun (:377–385) reach evaluateRequiredProof, checkRequiredEvidence
  and this same provenance predicate. A zero-exit required record with removed references is downgraded on write
  and refused on read if presented as successful/complete. Nonzero/incomplete records remain loadable evidence.
- Refutation: legacy RequireEvidence=false short-circuits required proof at :397–399; the new references do not
  require old reports to synthesize provenance or fail previously successful diagnostic execution. Schema-zero
  reports still become unattested at :355–366. Required references are checked before a complete claim, rather
  than inferred from opaque combined source/config digests.

`writer_test.go:76–84` now retains synthetic files and their actual hashes; :291–310 individually removes each of
the four log/manifest fields and verifies incomplete nonzero records. :88–127 is the positive same-run complete
control. The log records all these cases passing.

Limits: the synthetic manifest content is a fixture string, not a validated constituent manifest. This review
does not claim content verification, actual invocation provenance, retention durability, or application identity
from those files. Historical readers intentionally validate report shape without reopening local references.
Production collection and parent-side content/digest verification remain required before whole-issue acceptance.
The negative tests exercise writer downgrade followed by readback, not independent forged zero-exit read refusal
for every removed field. Nor do they explicitly assert absent/relative output_dir rejection or unavailable/malformed
reference values. The common code path statically establishes those decisions; add named read-boundary controls
when finalizing proof so a future split between write and read predicates cannot silently weaken them.

## Declaration boundary evidence and retained limits

`evidence_test.go:133–152` adds deterministic empty, duplicate and diagnostic-only declaration refusals followed by
unsuccessful finalization. This addresses the round-one missing declaration cases through the actual DeclareChecks
boundary. It does not claim direct malformed struct coverage of every branch. Shared validation in FinalizeChecks
remains unchanged at evidence.go:92 and :183–206.

The existing generated tests are appropriate for the required-set invariant: 1–6 required IDs with independent
passed/failed/skipped/missing status vectors, and a deliberately omitted selected index. Each activates a nonempty
set; deterministic cases now cover the zero/duplicate/diagnostic-only boundaries. Active spec citations resolve to
Required observations determine success and Evidence cannot improve by omission. No second CLI property oracle is
required to duplicate those responsibilities.

Author logs were read and hashes verified:

| Evidence | SHA-256 / observed scope |
| --- | --- |
| /private/tmp/semstreams-1222-result-writer-green.log | b90389b6f0f13747457365e319ccf4d0e0d1c4979e5ec8affdaa73215c73ee6d — Writer package tests and six fuzz seeds passed |
| /private/tmp/semstreams-1222-result-scenario-green.log | 132862a66093fef59068b577711338c4188d13744f9f2d00bc4e825567805421 — focused evidence examples, two Rapid properties with 100 cases each, six fuzz seeds passed |

The author's full scenarios attempt did not pass: `/private/tmp/semstreams-1222-result-r1r2-green.log` records
TestCoreHealthRecordsComponentObservation panicking because httptest could not bind `[::1]:0` under the sandbox.
That file's name is not a green verdict. No current full scenarios-package green is inferred. The author explicitly
reports that behavioral RED outputs were not saved separately; their assertion claims remain UNVERIFIED as raw
retained RED evidence. The present logs establish example/property execution and seed replay, not fuzz exploration
or mutation sensitivity.

Mutation criteria still apply. A temporary deferral remains accepted for this bounded read-only correction
checkpoint; it is not accepted as final verification. Missing-required, overwrite-failure, projection and outer-exit
sensitivity require the planned compiled/reached baseline-mutant-restored evidence and checksum restoration.
Race, actual producer/Task/CLI integration, assembled E2E and release gates remain unrun by this review.

No new exported owner was introduced by the corrections: the prior private provenance predicate is replaced by
one private problems-producing helper, reached only through existing Writer acceptance. Previously attempted
gopls structural enumeration in this session failed to load packages due to restricted Go cache access; bounded
`rg` confirms the shared write/read predicate callers. No complete compiler-backed enumeration is claimed.

**APPROVE — bounded R1/R2 corrections only.** Preserve the exact snapshot/evidence, continue the separate CLI
fixes and producer/adoption work, and perform final verification and review before claiming #1222 complete.

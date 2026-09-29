# Rule test cleanup pre-owner design review

Mode: pre-owner design review, separate from inventory review.
Verdict: **DESIGN CHANGES REQUESTED** at checkpoint `6176ba76c9be2f9300cb45cb69fff7660f78c14e`.

Worktree: `/Users/coby/.codex/worktrees/gh1397-runner-tests/semstreams`.
Accepted inventory checkpoint: `047be916111816f353d2998a06b40e24bcecb0e6`.
Frozen source: `caa98f5acae60efbc669ad1e1795ab6e903abd42`.

Verified artifact SHA256 identities:

| Artifact under openspec/changes/rule-test-cleanup | SHA256 |
|---|---|
| design.md | `1e2036ec3c7129c63f607b886c1f2e6a41169dfaa8a2d2866a7605be842b3e2c` |
| spec-disposition.md | `5cdcda7a84285df5b0fd02f4e6c02cd81b774940d3baf9453dacae6525799527` |
| tasks.md | `200daec9ed2b00fc9f5598fcc11f6f0b3b50d94621f32e981bc1f4941f5e6c3a` |
| proposal.md | `321a78b01a31754216420ca30a16cbc33d8b3e572cdd68362ee3b96d3efdfd40` |
| review/inventory-review-final.md | `3246b5de8904b6ab5aa2755d0b1720b794d973bab01a0f76b7348566b2cd4a4f` |

The accepted inventory, ledger, baseline exposure and helper-caller companion bytes remain unchanged.
Read the complete four design/change documents and checkpoint manifest, applicable cleanup/context/lifecycle specs,
canonical testing/PBT/mutation policy, relevant existing owner/proof ranges, and the installed OpenSpec validation and
archive paths. No new census, tests, Docker, repository edits or commits were performed.

## HIGH finding

**HIGH openspec/changes/rule-test-cleanup/design.md:221 — Final resolution total conflicts with the adopted stored-cancel shape**

- Mechanism: the design adopts a private stored `context.CancelFunc` and defers its invocation after native Stop,
  but requires the final reviewed-resolution total to remain 90. The exact existing graph-ingest model already needs
  a source-reviewed resolution for `(*graphIngestTestOwner).stop|defer|o.cancelStart|unresolved callback`, at
  `test/testinfra/cleanup_baseline.json:3651`. The shared lifecycle owner has the same limitation at line 2333.
  The guard records a function-valued selector on a cleanup path as unresolved at
  `test/testinfra/cleanup_analyzer_test.go:1183–1188`; a concrete Stop receiver does not resolve that separate edge.
  Consequently a faithful local adoption can produce new required cancellation records while preserving every
  existing record. Task 4.4 at tasks.md:44 repeats the fixed-total constraint, and design.md:224–226 currently implies
  every such analyzer uncertainty must be eliminated at the seam.
- Smallest correction: distinguish preservation of all 90 existing resolutions/fingerprints from the final count.
  Permit only additional exact, independently reviewed **non-lifecycle native cancellation** records when the actual
  guard requires them, with finite explicit source/symbol/dependency evidence. Keep the expected 297-to-273 debt
  removal, all existing records, and the five exposed resolution boundaries unchanged. No new unbounded cleanup
  approval, blanket exception, analyzer change or broad fingerprint regeneration is authorized. Record actual new
  identities/count only after implementation and exact source review; do not preapprove them by copying precedent.
- Verification and attempted refutation: read the actual existing resolution, both concrete owner implementations,
  analyzer selector handling, and `TestCleanupRootGuardUnresolvedSelectorCallbackFailsClosed` plus
  `TestCleanupRootGuardUnresolvedCallbackThroughResolvedWrapperFailsClosed` at cleanup_guard_test.go:1758–1808.
  Wrapping the field call does not establish its native provenance. CancelFunc's type alone is insufficient because
  arbitrary functions can be converted to that type. No runtime shape should be invented merely to evade this
  intentional analyzer limit. No new implementation exists yet, so the precise added record count remains unproven.

## Accepted design assessment

Apart from that bounded reconciliation correction, the recommendation is supported by the accepted inventory.
Do-nothing and finite-argument-only options leave the measured ownership/authority/attempt gaps; private concrete
owners apply an existing local shape without inventing an exported framework. The external-package duplicate keeps
its concrete receiver and existing package boundary. Current rule support has no owner with these responsibilities;
the nearest established owners are shared lifecycle and graph-ingest test support. Their different native contracts
remain explicit. No new durable, communication, orchestration, payload or remote-query decision is triggered.

All B00–B23/H01–H37 remain mapped; helper transfer, early assertions, two-owner restart fences, operation authority,
private Start cancellation, hardening's explicit Stop, parent/subtest substrate lifetimes, and support cleanup order
are addressed. The recommendation retains constructor/probe adjacencies and current exported contracts. Its
five-second terminal allowance matches current owner support and is expressly cooperative. Existing work budgets
are retained; new work budgets remain implementation-time measurement obligations, not demonstrated performance.

The proposed minimum proof is feasible through current native seams. Processor lifecycle authority, command lane,
consumer Drain/Closed and synchronous Stop provide an order/settlement witness without changing production or the
protected lifecycle-runtime helper. CronScheduler's existing blocking executor exposes admission and causal release;
its native awaitStop context branch permits a real error/expiry witness. Failure-path release/join must remain owned
by the test without invoking a second component Stop. At least one actual returning-helper escape and an executed
external-package case are required; a duplicate proof-only helper cannot substitute for that wiring.

PBT is applicable because shutdown/transfer histories affect results. The recorded named-example choice is accepted
for these finite order distinctions: setup exit, transfer/body exit, explicit success with later operation, explicit
failure followed by fallback, and ended work/terminal authority. Expectations come from the existing cleanup spec.
Required assertion activation and concrete native signals remain pending implementation evidence. No generated-test
quota or arbitrary-history claim is added.

Targeted mutation is required for this previously admitted regression class and shutdown ownership. The selected
omitted-finalization, premature-cancellation and failure-path retry faults are proportional. Reusing old evidence
requires the exact same reviewed fault/source/checks; copied adapters are not automatically covered. The design
correctly requires compiled mutants reaching intended assertions, unchanged checks, cp/checksum restoration and a
restored pass. No evidence deferral or completed sensitivity result is accepted at this design checkpoint.

## OpenSpec disposition and archive feasibility

The existing lexical-ownership requirement already governs these legacy repairs. No new normative requirement is
needed. `openspec validate rule-test-cleanup --strict --no-interactive --json`, using installed version 1.7.0,
currently reports `valid: false` solely because the active change has no delta. This is a real current validation
result, not an implementation defect or a reason to fabricate a duplicate requirement.

The planned final archive path is viable with validation enabled:

`openspec archive rule-test-cleanup --skip-specs --yes`

This command was **not executed**. `archive --help` documents --skip-specs. Installed archive.js:182–270 treats
proposal/no-delta issues as non-blocking warnings and validates actual delta specs when present; archive.js:345–351
skips spec writes with --skip-specs. It does not require --no-validate. Active-change discovery in
`dist/utils/item-discovery.js:18–22` excludes the archive directory, so the final hosted strict validation does not
validate this archived packet as an active zero-delta change; current capability specs still receive validation.

Local Taskfile.yml:151–162 check:push does not invoke OpenSpec validation. Hosted `.github/workflows/ci.yml:132–136`
installs version 1.7.0 and runs `openspec validate --all --strict --no-interactive`. Complete implementation review and
in-scope tasks first, then archive as the last content commit, narrowly review archive/spec disposition, and assess
actual hosted results. This review does not assert future CI success or run archive to bypass incomplete tasks.
No metadata/tooling change, fabricated delta or validation bypass is required by this disposition.

## Gates and conclusion

The tasks describe branch-checkable obligations rather than future hosted/merge facts. The #1421 merge hold remains;
the #1404-only waiver does not extend to this PR. No graph-index or other agent scope is transferred here.
Accepted inventory remains frozen; implementation and coordinator design acceptance are still pending.

**DESIGN CHANGES REQUESTED — exact blocking list: correct the fixed-final-90 resolution premise and its task wording.**
No other design correction is requested. A narrow re-review of that correction is sufficient; do not repeat inventory,
expand the proof matrix, invent a normative delta, or require a production redesign.

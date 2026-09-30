# #1222 documentation clarification review

Mode: bounded IMPLEMENTATION-documentation review. Verdict: CHANGES REQUESTED for one HIGH and one MEDIUM
correction below. This is not code approval, independently renewed approval of my API handoff, or evidence that
any implementation checks have run. Reviewed these files as target documentation awaiting code conformance.

Baseline: fe6e2cc03e16f5db47e293f55939548572f204cc. Accepted design SHA
 a31a5c690da763b124a7d1e1727836e57a270f943b5b61576216d7f386b9b48c, accepted active e2e-evidence delta.
Only documentation diff, surrounding guide context, added handoff clarifications and immutable baseline workflow
were inspected. Active implementation files were not reviewed; no tests, Docker, paid or GitHub operations.

## Exact reviewed files

- `docs/contributing/01-testing.md`: `96535659940b1d1122f5a6df1aebf5e76a191d41a2ec3d9ab3b75f489c2f9af2`
- `docs/contributing/02-e2e-tests.md`: `08d58daa8df757e9a9d2e564922c5c43c296a5581fe13a8cd96cbd724265288a`
- `.agents/skills/semstreams-preflight/SKILL.md`: `e91f4f8cb6c8a2ad7028a5dc3ee9dfccbd07dfcebceeeb5a02965f713e86e701`
- `openspec/changes/e2e-required-check-evidence/implementation-handoff.md`: `7b364cb1d3f19d5327cf7affaf2cfd50742eb19e526fe52ae164d5041a05ec76`

## Findings

### HIGH docs/contributing/02-e2e-tests.md:395 — Retained CI/release examples contradict the newly authoritative scope

Mechanism: the new named-evidence guide correctly states exact suite membership and preserves independent CI/release
obligations, but this same guide still labels core+structural as PR checks, core+structural+statistical as main-branch
CI, and semantic alone as Release. The Pre-Merge heading at219 similarly calls its hand-written trio “Full CI
validation”. A reader following the canonical guide can select the wrong required set or treat one semantic tier
as release proof. Line447 additionally calls #769 a nightly run despite the accepted ownership/ruling that functional
E2E is per-PR or explicitly invoked, not nightly. These are relevant existing surrounding claims because this diff
makes the page the canonical selection/evidence author guide.

Smallest correction: replace these unlabeled examples with the actual existing ladder scope (statistical and
slow-consumer on pull_request/workflow_dispatch), or label examples as illustrative commands rather than current
CI/release gates. Point release proof to release-candidate-proof rather than the one-command list. Replace the stale
nightly characterization with the accepted #769/#1128 ownership wording. No workflow expansion or gate change is
requested; the fix is truthful documentation of existing authority.

Verification/refutation: inspected immutable baseline e2e-ladder.yml25–34/44/72/74/122 via git show, not the actively
edited working workflow. It explicitly rules out functional nightly runs and executes statistical/slow-consumer.
Accepted design preserves those current jobs and delegates release decisions to the existing release spec. The old
examples are not labeled hypothetical; the nearby “Full CI validation” wording strengthens the misleading reading.
The added paragraph at195–197 correctly preserves release authority but does not neutralize the conflicting list.

### MEDIUM docs/contributing/01-testing.md:68 — Exit-propagation sentence does not restrict itself to required failures

Mechanism: “A failed observation must remain visible through ... runner exit, Task exit” includes diagnostics, while
the accepted Diagnostics remain explicit requirement permits a failed diagnostic alongside successful required
acceptance. The detailed E2E guide correctly preserves TTFT/B0/B2 as diagnostic; the short canonical policy can be
read as imposing contradictory nonzero exit behavior. This is a wording ambiguity, not evidence of a code bug.

Smallest correction: say a failed REQUIRED observation must propagate to failed scenario/runner/Task outcome;
separately require diagnostics to retain actual outcomes in reports/artifacts without independently failing required
acceptance. Keep the existing link to bootstrap/artifact exceptions rather than duplicating that mechanism here.

Verification/refutation: active delta Diagnostics remain explicit and guide diagnostic paragraph explicitly allow
that distinction. The next sentence “Diagnostics keep their actual outcome” does not specify which exit wins, so it
does not fully resolve the first sentence's all-observations wording.

## Confirmed strengths and limits

The new guide supplies reusable author/reviewer behavior, not only a feature list: declaration before observation,
input-derived oracle, actual consumer check, typed outcome/identity, sticky recording errors, lower-tier failure
controls and composed application proof. It correctly bounds complete to the selected set, retains existing fatal
checks, distinguishes legacy unattested reports, names composite exclusions, and preserves unrelated release gates.

Core's current pass-through oracle is accurately stated without selective filtering, exactly-once or full UDP-delivery
claims. Structural absence concerns clustering execution, not stored communities. TTFT/B0/B2 diagnostics, foreign
identity refusal, partial failed evidence, finalized typed projections and unavailable provenance are consistent with
the accepted design. Bootstrap explicitly starts after build/cleanup/preflight; log-only direct failure, initialized
parent missing-child evidence and abrupt-death incompleteness are preserved. There is no new production obligation.

The preflight change is one canonical pointer with scope/identity/disposition/artifact checks; its existing no-waiver
breaking-change rule remains intact. Relative links resolve by repository location. I did not invoke this skill to
run gates; its text is the review subject.

The appended implementation clarifications at168–177 stay within the accepted boundary: observed run/member identity
before constructor configuration; one resolved Selection distinct from Variant; private execution helper; one
invocation-owned aggregate writer. No mutable context binding or Go task coordinator is introduced by the prose.
This is a conformance observation about root's appended clarification, not independent reapproval of my authored API.
Whether code actually follows it remains for stable-snapshot implementation review.

Inherited timing estimates (notably semantic~90s) are not newly measured facts; #1117 still owns cost calibration.
No new timing or adoption-completion evidence is inferred from present-tense target documentation. Final merge review
must check all documented commands/scope/artifacts against completed implementation and its retained verification.

## Read-only evidence

Read complete changed diff for the three tracked docs/skill files, hashes before/after review, testing policy1–80,
E2E guide surrounding selection/CI/release sections, full preflight skill as a document, implementation-handoff130–177,
active delta relevant clauses, accepted design and approval from existing context. Immutable workflow read used:
`git show fe6e2cc03e16f5db47e293f55939548572f204cc:.github/workflows/e2e-ladder.yml` bounded to relevant ranges.
No source mutations and no execution claims.

CHANGES REQUESTED: correct the guide's misleading current CI/release/nightly claims and clarify required versus
diagnostic exit propagation. Recheck can be bounded to those paragraphs and new file hashes.

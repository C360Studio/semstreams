# Independent design review

Mode: pre-owner design review.

**DESIGN REVIEW PASS — focused repair plan.**

Reviewed `/private/tmp/semstreams-test-audit-20260928/design-checkpoint.md`, SHA-256 `fdbae52d2a9f3e44bc5de5483e15225cd189b933175bc22318b36077f0b81f0c`, at baseline `3dc4ccbef32e87096c6d998fde7e76e896cf2f3c`. Mechanically verified that the complete accepted inventory, inventory review, adopter addendum and addendum review are reproduced verbatim, and that the checkpoint preserves the complete repair-plan text.

No blocking or high findings.

The recommendation fits the user’s narrowed scope. It repairs existing tests, demonstrates reusable testing discipline through concrete examples, and keeps broader pipeline changes and the repository-wide cleanup census with their existing owners. It introduces no exported API or generic asynchronous-test primitive.

The strongest objection is that removing the three-second waits could merely exchange false failures for long hangs. The plan answers this with explicit child-exit/EOF observation, separately bounded containment, named failure diagnostics, termination and reaping, and a deliberately missing-signal acceptance case. Inheriting the package alarm alone explicitly fails acceptance.

The plan also preserves the distinction between test containment and production correctness. A finite cleanup context cannot preempt a production receive that ignores cancellation. The runtime-command fence defect, observed cron settlement failure and PR #1404 fixture correction retain separate evidence and ownership. A green rerun cannot resolve an unproven production dependency.

Implementation review must verify the following already-required evidence:

- A declared containment bound with its measured basis; this review approves no numerical replacement timeout.
- Detection of premature lock release, missing progress and failed cleanup, with honest accounting for remaining owned work.
- Applicable mutation sensitivity under the canonical testing policy. RED/GREEN evidence satisfies that obligation only when it discriminates the relevant fault with the same checks; otherwise a controlled mutation or explicitly reviewed deferral remains required.
- Existing helpers’ complete behavior before reuse. In particular, `commandWaiter.killAndWait` contains a bare receive at `test/testinfra/integration_runner_contract_test.go:901`; its existence alone does not prove bounded process-tree containment.

The proposed examples-based approach is adequate for planning the named ordering regressions. Any implementation adding broader state-history obligations must revisit the existing PBT decision, as the plan requires.

The prepared `/private/tmp/semstreams-test-audit-20260928/issue-1293-note.md` is factually and scope-consistent. I independently checked its cited job durations against saved job timestamps and the duplicate local race invocation against `Taskfile.yml:161`. It correctly labels design-claim validation failures, preserves rc.1 placement, makes no unsupported speedup claim and records pipeline considerations without absorbing them into this repair.

No tests, code edits or GitHub mutations were performed. This is independent design-review approval, not owner approval or implementation verification.

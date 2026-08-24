# Tasks: GH-1063 tool wire terminal-scope correction

- [x] 1. Record the accepted #1063 inventory/design hashes and owner rulings in proposal/design.
- [x] 2. Add the full agentic-tools MODIFIED requirement scoping terminal correlation to execution, terminal policy
  rejection, and completed replay while preserving nonterminal approval_required.
- [x] 3. Align README, package GoDoc, concepts guide, and JetStream tuning wording with that exact boundary and no
  local-parallelism promise.
- [x] 4. Verify `TestApprovalGateSameIDRedispatchExecutesOnceAndPublishesTerminalResult` proves pause nonterminality and
  approved terminal completion without production changes.
- [x] 5. Record the historical timing RED and causal multiple-result GREEN with exact selected test output.
- [x] 6. Record the race count-20 Docker integration gate with exact command, result, duration, and
  missing/duplicate/unexpected result state.
- [x] 7. Record policy-guard RED/GREEN and all forced-omission results, including restoration evidence.
- [x] 8. Record production-diff inspection proving no agentic-tools runtime behavior change.
- [x] 9. Run and record contract tests and `openspec validate --all --strict --no-interactive`.
- [x] 10. Landed via PR #1068 (`dc25bcc0`, merged 2026-08-24T00:10Z by the owner under the branch ruleset). Archived
      in PR #1071 under `openspec/README.md` rule 2 — the original wording asserted a post-merge fact and could not
      be ticked before merge. The conformance note's "independent re-review pending" stands as written; the merge
      recorded owner acceptance, not a further review.

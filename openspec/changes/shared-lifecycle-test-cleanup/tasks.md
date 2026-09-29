# Tasks

## 1. Reviewed evidence and design

- [x] 1.1 Retain the accepted surface and adopter inventories plus baseline/sister companions at checkpoint32c01357.
- [x] 1.2 Obtain INVENTORY PASS for SHA2561d225f08a8c1148a16e130ef4d00ff04728b932467753086d31558bbf003c424.
- [x] 1.3 Materialize exact design/spec/proposal drafts, obtain independent DESIGN PASS and owner/coordinator acceptance.

## 2. Developer: shared ownership and causal tests

- [x] 2.1 Write focused red tests for early-exit cleanup and injected-wrapper bypass, using independent base events.
  Use selected self-reexecuted test-binary cases for intended fatal failures; no shell or nested go-build/full suite.
- [x] 2.2 Implement one private lexical ownership path in component support. Preserve exported APIs, finite work and
  cleanup contexts, controlled/abort distinctions, terminal-attempt tracking, and exact operation/cleanup diagnostics.
- [x] 2.3 Route portable cases, failed acquisition, workers and NoLeaks through that path. Stop new iteration admission
  on failure, preserve accepted Start authority of in-flight work, drain results and join workers.
- [x] 2.4 Finalize ErrorInjection's base directly; check prerequisites and expected injected operation separately.
  Apply checked per-iteration finalization to benchmarks while preserving intended timing regions.
- [x] 2.5 Cover the named transition matrix, exact ordering, error preservation and failure admission behavior.
  Record focused race duration and bounded child ownership/output/cleanup evidence.

## 3. Developer and independent reviewer: real adoption and guard truth

- [x] 3.1 Preserve rule's real production factory, platform, watcher configuration and one TestClient; replace duplicate
  fallback cleanup with assertion-only forwarding observations proving terminal attempts precede substrate cleanup.
- [x] 3.2 Check all four default/tagged suite adopters and WebSocket injection/one-iteration benchmark paths. Run rule
  and graph-index integration through the focused canonical runner, coordinating the shared host and recording cost.
- [x] 3.3 Run current cleanup guard; reconcile exactly the four exposed resolutions and any new helper-origin sites.
  Remove obsolete evidence; obtain independent source review for replacements. Do not mutate unrelated334 debt.
- [x] 3.4 Run targeted mutations with cp backups/checksums: remove ownership installation, cancel before Stop, restore
  wrapper finalization, discard cleanup error. Each must fail its causal assertion promptly; restore and prove green.

## 4. Root/coordinator: review, records and landing

- [x] 4.1 Obtain independent implementation review, including supported timeout claims and before/after baseline counts.
- [ ] 4.2 Run required check:push once ready; preserve focused versus full evidence distinctions and measured budgets.
- [ ] 4.3 Update existing testing guidance and #1417 with this first batch's proof and remaining package work.
  Record the exact #1416 source/proof disposition without claiming a reproduced failure or changing issue authority.
- [ ] 4.4 Archive the completed change and synchronize its spec as the final content commit; review that narrow delta.
  Keep #1417 open and follow the existing CI/merge protocol for #1418.

Developer owns component support/tests and the rule test fixture. Root coordinates artifacts, review, gate execution
and issue truth; the reviewer independently owns acceptance of source evidence. Production files owned by #1404
remain untouched. Task 1.3 is complete; `review/design-review.md` records the independent verdict and coordinator acceptance.

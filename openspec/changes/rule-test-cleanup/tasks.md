# Rule test cleanup execution tasks

## 1. Accepted inventory

- [x] 1.1 Materialize exact roots/callers, authority/ownership ledger, adjacent claims and source evidence.
- [x] 1.2 Obtain INVENTORY PASS at `047be916111816f353d2998a06b40e24bcecb0e6`; retain accepted bytes and hashes.

## 2. Design gate

- [x] 2.1 Materialize design, specification-disposition and execution drafts as one exact review checkpoint.
- [ ] 2.2 HOLD: Obtain independent design review and coordinator acceptance within the owner's authorized test-only scope.
  Implementation remains held until both are recorded. This does not require a new owner permission round.

## 3. Test-first evidence and concrete ownership

- [ ] 3.1 Preserve the passing pre-change unit evidence and select the smallest additional rule-specific causal
  witnesses. Record PBT named-example rationale and the exact existing #1419/#1424 evidence reused.
- [ ] 3.2 Establish failing/sensitive witnesses for provisional/caller finalization, live Start through concrete
  native Stop, and failure-path attempted state. Use actual Processor/CronScheduler Stop seams and the applicable
  returning-helper/external integration wiring; do not create a generic assertion framework or fake-only Stop proof.
- [ ] 3.3 Add private concrete test owners in internal rule and external rule_test with synchronous finite checked
  Stop, private Start cancellation, pre-call attempted state and provisional transfer only where needed.
- [ ] 3.4 Convert B02–B04/H01–H24 scheduler paths; protect before fallible registration/Start and preserve named probes
  and explicit test-owned fire goroutine joins.
- [ ] 3.5 Convert B01/H25–H30 cron Processor helper/callers; preserve two distinct restart owners, explicit phase fences
  and operation authority after the first Stop. A failed explicit attempt cannot arm fallback retry.
- [ ] 3.6 Convert B00/B23/H31–H37 graph-ingest harnesses with provisional setup protection and immediate caller lexical
  finalization. Retain operation context separately from private Start authority.
- [ ] 3.7 Convert B05–B22 direct internal/external Processor cases, including parent/subtest lifetimes and hardening
  explicit Stop. External tests remain external; no exported test adapter is added.
- [ ] 3.8 Reconcile touched support ordering: keep canonical NewTestClient cleanup, remove B21/B22 duplicate termination,
  check shared-client/subscription cleanup, and keep NATS/observers alive through component finalization. Preserve
  existing 10/15/20/30-second budgets; record scopes for newly finite roots and any measured failure honestly.

## 4. Reconciliation and focused verification

- [ ] 4.1 Reconcile all 24 B rows and 37 H call sites to the implemented owner/transfer/terminal paths. Retain the 25
  constructor/support adjacencies and five resolution dependency boundaries; no incidental scope expansion.
- [ ] 4.2 Complete necessary bounded mutation runs with cp/checksum preservation, unchanged assertions, baseline pass,
  compiled mutant assertion failure and restored pass. Link reused evidence; record survivors/invalid runs explicitly.
- [ ] 4.3 Run focused `-race` rule unit checks and canonical focused rule integration through the existing runner and
  host lock. Include readiness/#1062, owner-lane/#1283, bounded Stop/#1404, both graph-ingest harness families, cron
  restart, hardening and external-package wiring. Record actual executed/skipped tests and named completion signals.
- [ ] 4.4 Remove only the repaired exact 24 baseline identities. Verify expected 297-to-273 debt change while
  preserving all 90 existing reviewed resolutions, approvals and dependency fingerprints unchanged. Preserve unrelated
  debt; do not regenerate the baseline or add new unbounded cleanup approvals.
- [ ] 4.5 Run the actual cleanup guard and applicable contract checks. If stored native cancelStart callbacks require
  manual classification, add only exact independently reviewed non-lifecycle cancellation resolutions with finite,
  explicit source/dependency proof and fingerprints under the existing policy. Keep other uncertainty explicit; do
  not change the guard or ownership shape to bypass it. Measure and report the final resolution total after guard
  reconciliation, with no assumed count. Preserve the accepted inventory checkpoint and record current source-to-case
  mapping separately.

## 5. Review, final gates and handoff

- [ ] 5.1 Obtain independent SemStreams implementation review of source, assertions, case coverage, mutation evidence,
  native limitations and exact baseline diff. Fix concrete findings and rerun only affected checks.
- [ ] 5.2 Use semstreams-preflight and run `task check:push` with canonical runner/lock ownership before pushing.
  Green focused tests alone are not whole-repository gate evidence.
- [ ] 5.3 Complete final OpenSpec archive/spec reconciliation as the last content commit; retain the justified absence
  of normative spec delta and exact review/evidence identities. Update PR/ticket truth and implementation persona.
- [ ] 5.4 Record final verification handoff and the #1421 merge hold in the PR. Identify required hosted checks and
  the fix-or-explicit-PR-waiver condition without asserting a future hosted pass. The #1404 waiver does not transfer.
  Do not expand into graph-index or Claude's #1426/#1427 work.
- [ ] 5.5 Prepare the branch-level scope/evidence/limitations handoff and reconcile parent #1417 claim status.
  Leave #1416 open for owner disposition. No task asserts future merge, hosted success or issue closure.

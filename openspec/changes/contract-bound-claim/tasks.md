# Tasks — contract-bound-claim (gh#689)

**SEQUENCING: lands after `entity-read-with-revision` (gh#851).** Its deltas modify the same
`projection-mutation-client` requirements; task 1.1 re-verifies wording before archive.

**Amend a task line when the work HAPPENS, not only when it succeeds.** A deliberate
not-done gets `[~]`, its reasoning, AND propagation into the spec delta. Run
`task openspec:queue` before archiving.

## 1. Preconditions

- [ ] 1.1 Confirm `entity-read-with-revision` archived; diff this change's
      `projection-mutation-client` delta against the live spec text and rebase wording if it
      shifted (a pointer is not the fact — open the archived spec, don't trust this note).

## 2. Claim capability in pkg/projection

- [ ] 2.1 New narrow interface + claim method: resolve the cas-transition group through the
      bound contract (mirror `ReplaceOwned`'s group resolution), require the bound owner
      token, read-with-revision → local foreign-claim refusal → conditional transition with
      claimant-identifying value. Failing tests first for: winner path, revision-conflict
      loser, already-claimed local refusal, stale-token under enforcement flag.
- [ ] 2.2 Ambiguity resolution: transport-unknown → authoritative read-back → committed /
      not-committed / (read failed) unknown, via the existing `CommitState`. Test both
      lost-response scenarios; mutation-check by disabling the read-back and confirming the
      committed-lost-response test FAILS (the guard must detect the dropped step).
- [ ] 2.3 Conditional unclaim: verify claimant value + revision; foreign claim protected;
      absent-claim replay is typed no-op success. Failing tests first.
- [ ] 2.4 Concurrency integration test over the production wire: two claimers, one read
      revision, exactly one committed success (spec scenario), against real graph-ingest.

## 3. gated-dag migration

- [ ] 3.1 Replace `natsClaimer` behind the existing `claimer` interface with the primitive;
      delete the local `subjectUpdateWithTriples` constant and manual marshaling
      (`claim.go:17, 56-91, 97-110`). Acceptance: `claim.go` owns no graph wire requests or
      mutation subject constants — grep proves it.
- [ ] 3.2 Executor ambiguity path: resolve-then-act replaces clear-and-reselect
      (`executor.go:400-450`); rollback becomes conditional unclaim with the
      "unit no longer mine" typed branch. Failing tests for ambiguous-committed
      dispatch-once and foreign-claim-protected rollback.
- [ ] 3.3 Amend `doc.go` invariant #1 (single-flight = performance model, not correctness)
      and REMOVE the `CAS-UPGRADE POINT` comment in the same diff that fulfills it (a code
      comment may be a stale prediction).
- [ ] 3.4 Old-shape resident claims (object = unitID): verify the value check treats them as
      foreign and the stranded-unit detector reaps them; integration test with a pre-seeded
      old-shape claim.

## 4. Downstream and gates

- [ ] 4.1 Reply on gh#689 with the shipped primitive; note on gh#851's thread that the
      "reusable CAS surface" question resolved as read-half (#851) + claim primitive (this).
      Reply to SemMachina's active-active ladder note if they adopt the claim capability
      (communicate, do not edit).
- [ ] 4.2 `gofmt`, `task lint`, `go vet ./...` plain + `-tags=integration`.
- [ ] 4.3 BOTH suites: `go test -race ./...` AND
      `go test -race -tags=integration -p 2 -count=1 ./...`; grep `^FAIL`.
- [ ] 4.4 `go test ./test/contract/...`; `task schema:generate` + diff clean.
- [ ] 4.5 `task e2e:structural` (gated-dag rides the rule/graph path).
- [ ] 4.6 `semstreams-reviewer` pass on the full diff.
- [ ] 4.7 Owner-run Codex round; arm `--auto` only AFTER it closes.
- [ ] 4.8 Owner CONFIRM-CLOSE before closing gh#689.

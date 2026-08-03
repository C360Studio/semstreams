# Tasks — lifecycle-create-ownership-proof

**Amend a task line when the work HAPPENS, not only when it succeeds.** A gate that ran and
was never recorded is indistinguishable from one that was skipped. A deliberate not-done gets
`[~]` AND its reasoning AND propagation into the spec delta.

## 1. Gate the design premise before implementing it

- [x] 1.1 **Prove the no-responders class is real on this server config.** `natsclient.IsNoResponders`
      documents that "whether an absent responder surfaces as `ErrNoResponders` vs a plain
      timeout is server-config dependent" — if it were a timeout here, narrowing create's retry
      would DELETE gh#170 cold-start protection rather than narrow it. Measured against the
      repo-pinned `nats:2.14-alpine` testcontainer: a request to an unsubscribed
      `graph.mutation.entity.create_with_triples` returned `nats: no responders available for
      request` in **733 µs** of a 3 s deadline, `IsNoResponders=true`. Pinned as
      `TestIntegration_AbsentResponderIsNoRespondersNotTimeout`.

## 2. Delete the ownership reconstruction

- [x] 2.1 Remove `Manager.committedByThisRequest` and its call site; report the emitter's
      `ErrAlreadyExists` as a conflict.
- [x] 2.2 Rewrite the rationale comment at the call site — a deleted mechanism must not leave
      its justification behind for the next reader to trust.
- [x] 2.3 Correct `CreateFromOperator`'s docstring: it claimed the lost-reply case was NOT
      closed while the code below it closed it wrongly. It now scopes the closure to the
      absent→create branch and ENUMERATES both residuals — the unknown outcome after a single
      timed-out delivery, and the attach branch's conservative false-409 (gh#870). Review HIGH-2:
      the first version said "this lane never reconstructs ownership from stored state", which
      the attach branch of the same function falsifies — the same shape of authoritative-comment
      falsehood that let the original defect survive review.
- [x] 2.4 **Review MEDIUM-6.** Put the three-outcome contract on `Manager.Create`'s docstring —
      the method an adopter actually calls. It previously lived only on `CreateFromOperator` and
      an unexported emitter docstring, so the caller most likely to mishandle `ErrAlreadyExists`
      (or to read an unknown outcome as a failure) had nothing to read.
- [x] 2.5 Record the attach-branch gap AT THE CODE (`createWithRegistration`'s revision-mismatch
      arm), not only in the issue: it is the same shape as the deleted defect, erring
      conservative rather than liberal, and the comment says why it stands (deleting it would
      call every unrelated concurrent update a duplicate birth).

## 3. Narrow the create retry to the classes that PROVE non-delivery

- [x] 3.1 `graphEmitterNATS.create` re-sends only on failures that prove non-delivery — first
      `natsclient.IsNoResponders`, extended by 3.4 — preserving the 15 s gh#170 budget for those
      classes (same schedule as `lifecycleEmitRetryConfig`).
- [x] 3.2 `update` and `delete` are UNCHANGED — `Transition`'s cold-start protection depends on
      retrying a sub-case that presents as a timeout, and delete is idempotent at the handler.
- [x] 3.3 Rewrite create's docstring to state the new contract: only a failure that proves
      non-delivery is re-sent; every other transport failure is an unknown outcome the caller
      resolves by reading authoritative state.
- [x] 3.4 **Review HIGH-3 — two more classes belong in the set.** A per-attempt
      `RequestClassified` re-checks connection and breaker state on EVERY call
      (`natsclient/request.go` `RequestWithHeaders`), where the replaced `requestMsgWithRetry`
      checked once at entry; `ErrCircuitOpen` and `ErrNotConnected` are bare `stderrors.New`, so
      `IsNoResponders` is false for both and the loop aborted, discarding the cold-start budget.
      Reachable, not theoretical: `circuitThreshold` is 15 CONSECUTIVE failures on the SHARED
      client and one cold-start create burns up to 11, so two concurrent creates (gated-dag calls
      `Manager.Create` per node) trip it partway through the second. Both classes are decided
      BEFORE `conn.RequestMsgWithContext`, so both prove non-delivery and admitting them is
      consistent with the ruling. `createSafeToResend` keeps the attempt-0 fast-fail so an
      ALREADY-open breaker still fails immediately — parity with the replaced loop's entry check,
      not a new policy.

## 4. Tests

Four of the six below fail without the fix and are labelled **GATE**. The other two are
CONTRACT PINS whose value is stating what must stay true — 4.4 is measured green against the
pre-fix code and 4.5 verifies consumers rather than the fix. Calling all six "gates" would
have been the false-coverage claim this change exists to remove.

- [x] 4.1 **GATE.** `TestManager_ConcurrentCreateOnlyOneWins` is the regression gate. **Run the POWERED
      form**: `go test -race -count=10 -cpu 1,2,4 ./pkg/lifecycle/ -run TestManager_ConcurrentCreateOnlyOneWins`.
      `-count=5` is UNDER-POWERED and passes on the unmutated defect (measured 3/3); `-race` is
      load-bearing and `-cpu 1` alone can never fire the interleaving. Pre-fix: RED 2/5 on the
      powered form and 2/3 on `-count=40 -cpu 2`. Post-fix: 0 RED in 8 powered batches, 0 RED in
      5 × `-count=40 -cpu 2` and 2 × `-count=40 -cpu 8`.
- [x] 4.2 **GATE (sub-case 1).** Re-pin the lost-reply test as `TestCreate_LostReplyIsNeitherADuplicateNorASuccess`.
      The fake emitter now scripts ATTEMPTS (`createAttempt`) instead of collapsing "committed"
      and "answered a failure" into one call — a fake that cannot represent a second delivery
      cannot falsify a retry policy. Sub-case 1 (already-exists answered for a write carrying
      this request's own audit stamp → still a conflict) is the fails-without-fix gate for the
      deletion; sub-case 2 pins the unknown-outcome contract.
- [x] 4.3 **GATE.** `TestIntegration_CreateDoesNotResendOnTimeout` — real NATS, a handler slower
      than the per-attempt deadline receives the create EXACTLY ONCE. Mutation-checked by
      deleting the guard CALL (the wiring, not the primitive): RED, `create was delivered 11
      times to a live handler`.
- [x] 4.6 **GATE.** `TestIntegration_CreateSurvivesBreakerOpeningMidColdStart` — drives the
      shared client's breaker to its threshold explicitly (an unfired guard and a green test look
      identical), then converges the create only by riding through the open breaker. The run logs
      `Circuit breaker opened circuit_failures=15 backoff=1s`. Mutation-checked at the wiring by
      deleting the `ErrCircuitOpen`/`ErrNotConnected` arm: RED, `create abandoned its cold-start
      budget ... 0 deliveries reached the handler`.
- [x] 4.4 **CONTRACT PIN.** `TestIntegration_ConcurrentCreateOnlyOneWins` — eight concurrent births arbitrated by
      an atomic KV create answering the production classified `entity_already_exists`, driving
      the production emitter rather than a mutex in a fake. **Recorded as a contract pin, NOT a
      regression gate**: measured green 3/3 against the pre-fix manager, because a real NATS
      round trip spreads each goroutine's timestamp and the deleted re-read only false-positives
      on a stamp collision. The gate that fails without the fix is 4.1 (in-process, no I/O
      between the stamps).
- [x] 4.5 **CONTRACT PIN.** Verify the consumers. `agentic/agentrun.Mint` takes its idempotent
      `ErrAlreadyExists → Get` branch (`TestMint_Idempotent_ErrAlreadyExistsFallsBackToGet`);
      the lifecycle gateway renders the conflict as 409 via the existing
      `ErrAlreadyExists → StatusConflict` arm (`TestCreateInstance_IsCreateOrFail`) instead of
      201-with-`degraded:true`-and-no-body. No gateway code change was required.

## 5. Truth-keeping

- [x] 5.1 Correct the stale concurrent-mint comment in `agentic/agentrun/agentrun.go` — it
      described a fallback the code did not reach when a loser was told it had succeeded.
- [x] 5.5 **Review HIGH-1.** The spec delta asserted a universal ownership MUST that unchanged
      code in the same function falsifies, and excused the must-exist lanes with a re-validation
      claim the attach caller does not honour. The ownership rule is now scoped to the
      absent→create branch, the attach branch's deviation is recorded IN THE DELTA with its
      gh#870 citation and its own scenario, and the CAS-update scenario names which callers
      re-validate. A deliberate not-done must reach the delta: the proposal is archived, the
      delta becomes permanent current truth.
- [x] 5.6 **Review MEDIUM-5.** The requirement title "A committed birth MUST NOT be reported as a
      failure" is contradicted by its own body (a concurrent loser's conflict and an unknown
      outcome are neither). REMOVED + ADDED under "Lifecycle creation MUST report what THIS
      request committed", carrying the original content unchanged.
- [x] 5.2 File the follow-ups rather than widening this change: **gh#869** request-scoped
      idempotency primitive on the graph mutation seam (three consumers: this residual, gh#689,
      gh#807 — ADR deferred because gh#807 has four open shape questions); **gh#870** the
      attach-path false-409 mirror-image bug; **gh#871** the two other
      content-equality-as-ownership sites; **gh#872** the e2e coverage gap (no tier exercises
      concurrent lifecycle creates or the 409 a loser now receives); **gh#874** (review MEDIUM-4)
      the FIFTH hand-rolled "retry only what proves non-delivery" loop — three in
      `pkg/projection/mutation_client.go`, one in `processor/gated-dag`, one added here. The copy
      was not avoidable (no `natsclient` primitive; `pkg/projection`'s helpers are unexported and
      welded to its receipt model) but each copy gets to be wrong about the class boundary
      independently — which is precisely what HIGH-3 found.
- [x] 5.3 Record on gh#178 that its proposed mechanism — the audit-triple re-read — is the one
      being removed, so the issue does not re-license the defect
      (github.com/C360Studio/semstreams/issues/178#issuecomment-5160832936).
- [x] 5.4 Re-sync the retry doctrine that now licenses the defect. `docs/operations/07-nats-request-retry.md`
      said mutations use `RequestWithRetry` (retry on ANY error) and separately said "never
      optimize the loop to retry only `IsNoResponders`" — read straight, that instructs the next
      implementer to rebuild this bug on the next non-idempotent create. Added the create
      carve-out, scoped the never-narrow rule to READS (re-reading is free; re-sending is not),
      and recorded what the narrowing costs on a server that does not fast-fail. Durable-doc
      ownership stays with the technical writer — flagged in the handoff.

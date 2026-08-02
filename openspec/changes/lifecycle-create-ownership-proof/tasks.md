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
      closed while the code below it closed it wrongly. It now states what IS closed (the
      retry path) and what remains open (an unknown outcome after a single timed-out delivery).

## 3. Narrow the create retry to the provably-pre-commit class

- [x] 3.1 `graphEmitterNATS.create` retries only `natsclient.IsNoResponders`, preserving the
      ~13 s gh#170 budget for that class (same schedule as `lifecycleEmitRetryConfig`).
- [x] 3.2 `update` and `delete` are UNCHANGED — `Transition`'s cold-start protection depends on
      retrying a sub-case that presents as a timeout, and delete is idempotent at the handler.
- [x] 3.3 Rewrite create's docstring to state the new contract: only the provably-pre-commit
      failure is re-sent; every other transport failure is an unknown outcome the caller
      resolves by reading authoritative state.

## 4. Tests — each must fail without the fix

- [x] 4.1 `TestManager_ConcurrentCreateOnlyOneWins` is the regression gate. **Run the POWERED
      form**: `go test -race -count=10 -cpu 1,2,4 ./pkg/lifecycle/ -run TestManager_ConcurrentCreateOnlyOneWins`.
      `-count=5` is UNDER-POWERED and passes on the unmutated defect (measured 3/3); `-race` is
      load-bearing and `-cpu 1` alone can never fire the interleaving. Pre-fix: RED 2/5 on the
      powered form and 2/3 on `-count=40 -cpu 2`. Post-fix: 0 RED in 8 powered batches, 0 RED in
      5 × `-count=40 -cpu 2` and 2 × `-count=40 -cpu 8`.
- [x] 4.2 Re-pin the lost-reply test as `TestCreate_LostReplyIsNeitherADuplicateNorASuccess`.
      The fake emitter now scripts ATTEMPTS (`createAttempt`) instead of collapsing "committed"
      and "answered a failure" into one call — a fake that cannot represent a second delivery
      cannot falsify a retry policy. Sub-case 1 (already-exists answered for a write carrying
      this request's own audit stamp → still a conflict) is the fails-without-fix gate for the
      deletion; sub-case 2 pins the unknown-outcome contract.
- [x] 4.3 `TestIntegration_CreateDoesNotResendOnTimeout` — real NATS, a handler slower than the
      per-attempt deadline receives the create EXACTLY ONCE. Mutation-checked by deleting the
      `IsNoResponders` guard CALL (the wiring, not the primitive).
- [x] 4.4 `TestIntegration_ConcurrentCreateOnlyOneWins` — eight concurrent births arbitrated by
      an atomic KV create answering the production classified `entity_already_exists`, driving
      the production emitter rather than a mutex in a fake. **Recorded as a contract pin, NOT a
      regression gate**: measured green 3/3 against the pre-fix manager, because a real NATS
      round trip spreads each goroutine's timestamp and the deleted re-read only false-positives
      on a stamp collision. The gate that fails without the fix is 4.1 (in-process, no I/O
      between the stamps).
- [x] 4.5 Verify the consumers. `agentic/agentrun.Mint` takes its idempotent
      `ErrAlreadyExists → Get` branch (`TestMint_Idempotent_ErrAlreadyExistsFallsBackToGet`);
      the lifecycle gateway renders the conflict as 409 via the existing
      `ErrAlreadyExists → StatusConflict` arm (`TestCreateInstance_IsCreateOrFail`) instead of
      201-with-`degraded:true`-and-no-body. No gateway code change was required.

## 5. Truth-keeping

- [x] 5.1 Correct the stale concurrent-mint comment in `agentic/agentrun/agentrun.go` — it
      described a fallback the code did not reach when a loser was told it had succeeded.
- [x] 5.2 File the follow-ups rather than widening this change: **gh#869** request-scoped
      idempotency primitive on the graph mutation seam (three consumers: this residual, gh#689,
      gh#807 — ADR deferred because gh#807 has four open shape questions); **gh#870** the
      attach-path false-409 mirror-image bug; **gh#871** the two other
      content-equality-as-ownership sites; **gh#872** the e2e coverage gap (no tier exercises
      concurrent lifecycle creates or the 409 a loser now receives).
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

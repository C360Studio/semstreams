# Tasks — an unreachable body is an exclusion, not a failure

> Amend a task line when the work HAPPENS, not only when it succeeds. For an owner-run gate the task line is
> the only durable evidence.

## 1. Prove the defect before fixing it

- [x] 1.1 Test: an entity whose `StorageRef` names an unregistered instance, in a component that owns an
      unrelated content store, produces a **durable failed record** and `IndexStateDegraded`. RED-first — it
      must pass against `origin/main`, documenting today's behaviour, then be inverted by the fix.
      **Ran against unmodified `origin/main` (2dce8258) on the real wire (component → ENTITY_STATES → worker
      → EMBEDDING_INDEX, testcontainers NATS): PASSED, documenting the defect.** Observed:
      `durable record: status="failed" reason="content_error" err="text extraction failed: failed to open
      content from instance \"AGENT_CONTENT\": Store.Open: open object stream failed: nats: object not found"`
      and `readiness: state="degraded" failed=1 reasons=map[content_error:1]`. The test now carries the
      INVERTED (post-fix) assertions, with that pre-fix observation quoted in its doc comment:
      `processor/graph-embedding/storageref_unresolvable_integration_test.go`
      (`TestIntegration_GH875_UnresolvableInstanceIsExcludedNotFailed`).
- [x] 1.2 Prove the stickiness rather than assuming it: after the failed record exists, **re-deliver the
      entity** and confirm it fails again, and confirm the failed map is re-seeded across a restart. This is
      the claim that makes it permanent; if it does not reproduce, stop and re-derive before continuing.
      **BOTH halves reproduced against unmodified `origin/main`.** Re-delivery: writing the entity again at a
      new revision re-emitted `ERROR Failed to get source text ... entity_id=c360.platform.docs.sys.doc.001`
      and the record stayed `failed` at the newer `SourceRevision`; readiness stayed
      `degraded / FailedCount=1`. Restart: a FRESH component over the same NATS logged
      `INFO seeded current-failed map from durable EMBEDDING_INDEX failed_count=1` and booted degraded with no
      entity write at all — and then re-failed the re-delivered entity again. The premise holds; nothing was
      re-derived.
- [x] 1.3 Confirm the background repair loop does NOT touch `failReasonContentError` — the fix depends on
      there being no give-up path to add.
      **Confirmed from code, not from the godoc alone:** `repairTargets`
      (`processor/graph-embedding/component.go:1297-1308`) switches on exactly
      `embedding.ReasonDeleteFailed`, `ReasonPendingWriteFailed`, `ReasonEntityReadFailed`.
      `failReasonContentError` = `"content_error"` (`graph/embedding/worker.go:1088`) is not among them, and
      it is unexported so no other package can add it to that set. No give-up path exists or was added.

## 2. Bound the gate

- [x] 2.1 `shouldFetchViaStorageRef` (`processor/graph-embedding/component.go:1974-1984`): fall through to the
      owned store only when `state.StorageRef.StorageInstance == c.contentStore.InstanceName()`.
      Implemented at `processor/graph-embedding/component.go:1964-2000` (the gate now ends
      `return c.contentStore != nil && c.contentStore.InstanceName() == state.StorageRef.StorageInstance`).
- [x] 2.2 Test: the owned-store fallback does not answer for a foreign instance. **Mutation-check the
      WIRING** — restore the bare `return c.contentStore != nil` and confirm the test goes red.
      Test: `TestShouldFetchViaStorageRef/StorageRef naming an instance the owned store does NOT serve`
      (`processor/graph-embedding/storageref_fallback_test.go`).
      **MUTATION 1 applied** (gate restored to the bare `return c.contentStore != nil`, `cp` backup,
      md5 `d55068a7e031c2bec35811b8c1dfccbc` before/after): that subtest FAILED, and so did
      `TestShouldFetchViaStorageRef_RegistryArmUnchanged/an instance the registry does not hold does not
      resolve`. Restored by `cp` from the backup; md5 matched; `git status --porcelain` empty.
      **Recorded honestly:** under mutation 1 alone the INTEGRATION test still passes, because hop 2's
      reclassification absorbs the re-widened gate — the two halves are independently load-bearing, so the
      gate needs its own unit-level detector, which is this one.
- [x] 2.3 Confirm the registry arm is untouched: a registered instance still resolves exactly as today.
      `TestShouldFetchViaStorageRef_RegistryArmUnchanged` (three sub-cases: resolves with no owned store;
      resolves ahead of an unrelated owned store; an unregistered instance does not resolve) plus
      `TestFetchText_RegistryResolvesByInstance` (unchanged, still green) and
      `TestIntegration_GH875_GateResolvableThenDeregisteredExcludes`, whose precondition asserts the
      production gate returns true for a registered instance.

## 3. Reclassify at the fetch

- [x] 3.1 `resolveStore` / `fetchTextFromStorage` (`graph/embedding/worker.go:986-1008`): return a
      **distinguishable** no-store-for-this-instance condition, matchable with `errors.Is` — not a string
      match on the message.
      `errNoStoreForInstance` (`graph/embedding/worker.go`, unexported — the branch is in-package), wrapped
      with `%w` at the single `store == nil` return so `errors.Is` matches through the caller's chain.
      `resolveStore` now returns nil for an instance it cannot serve, including the owned fallback, which is
      bounded by `ownedStoreServes` (narrow `InstanceName() string` assertion; **fail-closed** when the store
      cannot state its instance). Tests: `TestFetchText_OwnedStoreDoesNotAnswerForAForeignInstance`,
      `TestFetchText_OwnedStoreThatCannotNameItsInstanceDoesNotAnswer`, `TestFetchText_NoStoreConfigured`,
      `TestResolveStore_BoundsTheOwnedFallback` (4 cases).
- [x] 3.2 Route only that condition to `reportOffloadedContentExcluded` + a terminal outcome from inline text;
      no durable failed record is written.
      `handleKVEntry` switches on `errors.Is(err, errNoStoreForInstance)` and, only there, reports the
      exclusion and continues with the record's inline `IdentityText` (`w.capText`), reaching the ordinary
      terminal below — generated when there is identity text, the no-text delete when there is not. The report
      reaches the component's existing `reportOffloadedContentExcluded` through
      `WorkerMetrics.ReportContentExcluded` → `workerMetricsAdapter.reportExcluded`, wired in `Start` as
      `newWorkerMetricsAdapter(c.metrics, c.failuresVec, c.reportOffloadedContentExcluded)` — ONE home for the
      condition, not a second spelling in the worker. Tests:
      `TestHop2_UnresolvableInstanceExcludesAndEmbedsInlineText`,
      `TestHop2_UnresolvableInstanceWithNoInlineTextIsTerminalNotFailed`,
      `TestWorkerMetricsAdapter_ContentExcludedRoutesToTheComponentReporter`.
- [x] 3.3 A *resolved* store's `Open`/read failure still produces `failReasonContentError` with its existing
      recovery-on-re-delivery behaviour. Test both branches — a test that only covers the new path cannot
      detect that the old one was swallowed with it.
      **Both branches tested, in the same file, through the same production drive helper (`runHop2`):**
      `TestHop2_ResolvedStoreReadFailureStillFails` asserts terminal `(OutcomeFailed, content_error)`, a
      DURABLE `StatusFailed` record, `failures_total{reason}=[content_error]`,
      `content_resolve_error_total`+1, and `excluded == 0`; the exclusion test asserts the mirror image
      (no failed record, no failure reason, `resolveErr == 0`, `excluded == 1`).
- [x] 3.4 Test the gate/fetch race explicitly: resolvable at the gate, deregistered before the fetch → exclusion,
      not a durable failure. This is the reason the fix is in two places rather than one; without this test that
      decision is unevidenced.
      Two levels. Unit: `TestHop2_StoreDeregisteredBetweenGateAndFetchExcludes` (real `*storeregistry.Registry`;
      asserts the instance resolves, then `Deregister`s before hop 2 runs). Component-level:
      `TestIntegration_GH875_GateResolvableThenDeregisteredExcludes` — asserts the PRODUCTION gate
      (`c.shouldFetchViaStorageRef`) returns true, writes hop 1's own pending record through
      `c.storage.SavePendingWithStorageRef`, then `Deregister`s, so the deregistration lands deterministically
      between the gate's decision and hop 2's read.
      **MUTATION 2** (worker fallback un-bounded AND the exclusion route disabled): both went RED, and the
      integration race test failed with `Condition never satisfied`. Under mutation 2 the flagship exclusion
      integration test still PASSED (the unmutated gate absorbs it) — the mirror of 2.2, and the concrete
      evidence that neither half alone is sufficient.
      **MUTATION 3** (both halves reverted = pre-fix code):
      `TestIntegration_GH875_UnresolvableInstanceIsExcludedNotFailed` FAILED —
      `an entity whose body is unreachable must still embed the inline text it carries`. Restored both files by
      `cp`; md5s matched (`d55068a7…`, `413297d5…`); `git status --porcelain` empty.

## 4. Observability — the cost ledger's mitigation

- [x] 4.1 Confirm `content_unresolved_total` increments for the new path and carries enough label to identify
      the instance.
      Increment CONFIRMED for both hops: `TestIntegration_GH875_UnresolvableInstanceIsExcludedNotFailed`
      asserts a `+1` delta on `c.metrics.contentUnresolved` for the gate path, and
      `TestWorkerMetricsAdapter_ContentExcludedRoutesToTheComponentReporter` asserts `+2` for two hop-2
      reports through the production adapter.
      **REWORK (review blocking item 1): the WIRING was unguarded.** Nilling the third argument to
      `newWorkerMetricsAdapter` left every test green while hop 2's exclusion emitted no counter and no
      warning — the silent skip the acceptance criteria forbid. The assertion now lives in
      `TestIntegration_GH875_GateResolvableThenDeregisteredExcludes`, which snapshots the counter before hop
      1's pending write and asserts `+1` after; that test never writes to ENTITY_STATES, so hop 1 cannot
      contribute the increment. Mutation-checked — see 4.4.
      **DEVIATION, recorded not executed:** the counter carries NO label — it is a bare
      `prometheus.NewCounter` (`processor/graph-embedding/metrics.go:123-128`), and the instance is identified
      only in the one-shot warning's `storage_instance` attribute. Adding a `storage_instance` label would
      change the excluded-content reporting shape, which `design.md` Non-Goals rules out explicitly ("Any
      change to the excluded-content reporting shape. It is fit for purpose and already has an operator
      metric"). Rather than execute against a design non-goal, the gap is filed with the cost ledger as
      **gh#881** and left for the owner to re-decide.
- [x] 4.2 Confirm the one-shot warning names the instance and the remedy (wire a `store-read` port for it),
      not just the failure.
      Confirmed the warning carries `slog.String("storage_instance", …)`, fires once (no flood — locked by
      `TestWorkerMetricsAdapter_ContentExcludedRoutesToTheComponentReporter` and
      `TestReportOffloadedContentExcluded_LoudNotSilent`), and now covers hop 2 as well as hop 1.
      **REWORK (review blocking item 2): the REMEDY it named was unexecutable.** "Wire a store-read port for
      its StorageInstance" cannot be done — a store-read port declares a BUCKET, and `createContentStore`
      passes only `BucketName`, so an owned store's `InstanceName()` is always the bucket name. It could never
      match a reference stamped by an objectstore *Component* (which stamps its component instance name). The
      warning now names the two remedies that work — run a storage component owning that instance in this
      process (it self-registers, ADR-063; the only remedy for a cross-process split), or rely on the
      bare-store case where the instance IS a bucket name — and logs `owned_store_instance` beside
      `storage_instance` so the mismatch is one line, not an investigation.
- [x] 4.3 Do NOT add a readiness gauge. **File** the "how many entities currently have unreachable bodies"
      question with the cost-ledger items from proposal.md — record the issue number on this line.
      Not added. Filed as **gh#881** ("graph-embedding: no current count of entities whose offloaded body is
      unreachable (the gh#875 cost ledger)") carrying all three cost-ledger items, the three options with
      their costs, and the note that gh#873 is what makes the population real enough to judge it.
      **Post-rework note:** the qualifier makes the current count DERIVABLE from `EMBEDDING_INDEX` without any
      new surface, which lowers gh#881's urgency but does not close it — a derived scan is not a gauge.

- [x] 4.4 **Mutation-check the wiring guard added for blocking item 1.** Method: commit first, `cp` backup,
      `[applied]` marker, md5-verified restore.
      **MUTATION 4** — `newWorkerMetricsAdapter(c.metrics, c.failuresVec, nil)` at
      `processor/graph-embedding/component.go:941` (the exact mutation review reported as invisible):
      `TestIntegration_GH875_GateResolvableThenDeregisteredExcludes` **FAILED** on the counter assertion
      (`hop 2's exclusion must increment content_unresolved_total through the wired reporter`), where before
      the rework it passed. Restored by `cp`; md5 matched; `git status --porcelain` empty.

## 5. Gates

> Run what CI runs — BOTH suites. Check `^FAIL` rather than trusting a pipeline exit code.

- [x] 5.1 `go build ./...`, `gofmt -l .` empty, `go vet` plain AND `-tags=integration`.
      All four clean: `go build ./...` OK; `gofmt -l .` printed nothing; `go vet ./...` and
      `go vet -tags=integration ./...` both silent.
- [x] 5.2 `task lint` — revive warnings are failures.
      Clean: `go vet`, `go fmt`, `revive -config revive.toml` (no output), the fixed-port lint guard, and
      `go test ./test/natsclient/` (ok, 0.564s). Zero revive warnings.
- [x] 5.3 `go test -race ./...` — record ok/FAIL counts.
      **135 `ok`, 0 `^FAIL`** (26 packages with no test files); process exit 0. Counted with
      `grep -c "^ok"` / `grep "^FAIL"`, not from the pipeline exit code.
- [x] 5.4 `go test -race -tags=integration -p 2 -count=1 ./...` — record counts. Take a `docker info` latency
      reading first; >1s is host debt, not a code signal.
      `docker info` latency **0.56s** (and 0.61s at session start), 0 pre-existing containers — no host debt,
      no other session's containers.
      **Result: 131 `ok`, 5 `^FAIL` packages** — `pkg/ownership`, `processor/agentic-loop`,
      `processor/graph-index`, `processor/graph-ingest`, `processor/rule`. **Both touched packages passed**
      (`ok graph/embedding 1.701s`, `ok processor/graph-embedding 23.809s`).
      Attributed to Docker substrate pressure under `-p 2` on THREE measurements, not on a "known flake"
      label: (1) four of the five failed inside testcontainers provisioning —
      `resolve mapped port 4222: still unresolved after 178 attempt(s) over 10s — the container is likely gone`
      and `wait until ready: context deadline exceeded: port "8222/tcp" not found` (the gh#736 class); the
      fifth is a load-sensitive debounce timing assertion (`TestEntityWatcher_RuleTriggerDebouncing/deletion
      cancels pending evaluation`). (2) **All five pass in isolation**, re-run individually with `-race
      -tags=integration`: ownership 2.058s, rule 3.622s, agentic-loop 2.511s, graph-index 3.084s,
      graph-ingest 2.767s. (3) **Four of the five cannot see the changed code at all.**
      **CORRECTED (review finding) — the original leg 3 claimed "none of the five", measured with `go list
      -deps -test` and NO build tag, while the failures happened under `-tags=integration`. Re-measured under
      the tag set that actually ran:**

      | package | untagged | `-tags=integration` |
      |---|---|---|
      | pkg/ownership | 0 | 0 |
      | processor/agentic-loop | 0 | 0 |
      | **processor/graph-index** | 0 | **1** (via `graph/query`, an XTestImport) |
      | processor/graph-ingest | 0 | 0 |
      | processor/rule | 0 | 0 |

      So `processor/graph-index` DOES link `graph/embedding` in its integration test binary. Its two failures
      are still substrate: `Failed to get mapped port: resolve mapped port 4222 … the container is likely
      gone` and `Failed to start NATS container: … port "8222/tcp" not found` — neither reaches assertion
      code — and the package passes in isolation. The conclusion stands on legs 1 and 2; leg 3 is now correct
      for the four packages it actually covers. Recorded rather than re-run to green.
- [x] 5.5 `task schema:generate` then `git diff schemas/ specs/` showing zero drift.
      Ran; `git diff --stat schemas/ specs/` empty and `git status --porcelain` empty. Zero drift (expected —
      no operator-facing config field changed).
- [x] 5.6 `go test ./test/contract/...`.
      `ok github.com/c360studio/semstreams/test/contract 1.950s`.
- [x] 5.7 Not BREAKING and no wire-surface change, so no e2e tier is owed. **If that judgement is wrong, say so
      rather than skipping quietly** — the touched path is embedding readiness, which `task e2e:semantic`
      covers.
      **Judgement re-derived, and it holds with one caveat stated rather than skipped.** No payload, subject,
      bucket, config field, or envelope field changed; the readiness envelope's SHAPE is untouched (only which
      events reach `FailedCount`). `schemas/`+`specs/` show zero drift, which is the mechanical check for wire
      surface. The exported-surface delta is one method added to `embedding.WorkerMetrics` — a compile-time
      break for any out-of-tree implementer, loud rather than silent, and in-tree the only implementers are
      the component's adapter and two test doubles, all updated.
      **Caveat:** all four in-tree graph-embedding configs declare `ports` explicitly and register their
      instances, so no e2e tier can observe this class at all (this is why the defect shipped) — running
      `task e2e:semantic` would prove no regression but could not prove the fix. Not run here; the reviewer
      may still want it as a no-regression check on embedding readiness.

## 6. Review

- [x] 6.0 **Rework after review (owner ruling, 2026-08-03 — gh#875 comment 5166387449).** Review found the fix
      correct but the record it leaves a wart, and the owner ruled the wart is the MODEL. Recorded here because
      the ruling changed the shape, not just the details:
      - **6.0a Reframe: an unreachable body is a QUALIFIED SUCCESS.** `Record.Reason` generalizes from a
        failure classification to a bounded qualifier of the terminal state, valid on any `Status`
        (`graph/embedding/storage.go` — the field doc, `ReasonContentExcluded`, `SaveGenerated`'s new
        `qualifier` argument). Rejected alternative, per the ruling: a `ContentExcluded bool`, which would add
        a THIRD axis to a two-axis model already misaligned with the runtime.
      - **6.0b Enumerability.** The stored record is `generated + content_excluded`, so the population is a
        scan away. Cost-ledger item 2 DISSOLVED rather than accepted.
      - **6.0c Repair.** `SavePendingGuarded`'s skip is now "a same-or-newer **unqualified** generated vector
        stands" (`graph/embedding/storage.go:362`), so a qualified record re-queues and self-heals when the
        store is wired. Cost-ledger item 4 DISSOLVED. Proven end-to-end by
        `TestIntegration_GH875_WiringTheStoreSelfHealsTheQualifiedRecord`, which writes NOTHING to
        ENTITY_STATES — only the restart's same-revision re-delivery heals it.
      - **6.0d Safety re-measured, not inherited** (the ruling said to confirm it myself). Three production
        readers of `Record.Reason`, every one Status-gated first: `storage.go:362` (inside
        `Status == StatusGenerated`), `storage.go:826` `ScanFailed` (inside `Status == StatusFailed`),
        `processor/graph-embedding/component.go:1026` (reads only `FailedEntry` values produced inside that
        gate). `incFailure` — the sole path to `failures_total{reason}` — is called from `markFailed` and
        nowhere else (three call sites, all within it). Grep for anything inferring failure from a non-empty
        reason across `graph/` and `processor/`: zero hits on an embedding record. Locked by
        `TestQualifiedSuccess_NeverReachesFailureAccounting`, because a grep is not a regression guard.
      - **6.0e A gap the rework itself found.** The first qualifier implementation wrote it only on hop 2's
        race path. The self-heal integration test failed on the DOMINANT path and exposed why: hop 1's gate
        refuses the offloaded lane, so hop 2 receives a pending record with no `StorageRef` and cannot observe
        the condition. Hop 1 now writes the qualifier onto the pending record
        (`processor/graph-embedding/component.go`) and hop 2 carries it forward, accepting only the known
        value so an unrecognized reason fails closed to unqualified.
      - **6.0f Not expanded into gh#887** (the `Status`/`TerminalOutcome` asymmetry), as ruled. No new
        `Status` value, no change to `TerminalOutcome`, no change to the terminal callback's contract.
- [ ] 6.1 `semstreams-reviewer` on the full diff.
- [ ] 6.2 Codex round, then `--auto`. Record that it ran on this line.
- [x] 6.3 Confirm the spec delta's MODIFIED requirement matches the archived text exactly apart from the
      intended carve-out — a partial MODIFIED loses detail silently at archive time.
      **Confirmed by diff, not by reading.** Extracted the live requirement
      (`openspec/specs/graph-embedding/spec.md:217`ff, 50 lines) and diffed it against the delta: the diff is
      **additions only** — one new paragraph and four new scenarios. Zero deleted or altered lines, so every
      existing sentence and all five existing scenarios survive archive verbatim.

## 7. Sequencing — do not lose this

- [x] 7.1 **This must land and be OBSERVED before gh#873's store-registration step.** Between gh#873 repairing
      the reference and this landing, every trajectory-step entity would carry a reference most deployments
      cannot resolve — the permanent-degraded case, at trajectory-step cardinality. Record on gh#873 that this
      is its prerequisite.
      Recorded: https://github.com/C360Studio/semstreams/issues/873#issuecomment-5165730597 — carries the
      measured pre-fix evidence, the ordering requirement, and the two decisions gh#873 inherits (registering
      `AGENT_CONTENT` is the actual reachability fix; gh#881 is the count question).
- [x] 7.2 Confirm at review that nothing from gh#873 — evidence, trajectory steps, retention — leaked into
      this diff. This change must be observable entirely on its own.
      `git diff 4b9f8896..HEAD` touches 11 files, all in `graph/embedding/` and `processor/graph-embedding/`.
      Zero added lines match `agentic-loop|trajectory|evidence|retention|873`. No `processor/agentic-loop`
      file is touched and no dependency on it exists.
      **One thing to see rather than miss:** the string literal `"AGENT_CONTENT"` appears in tests as the name
      of an unresolvable instance. It is a test fixture value chosen because it is the instance this will
      actually meet in production — not a code dependency on gh#873. Every gh#875 test passes with no
      agentic-loop involvement whatsoever.

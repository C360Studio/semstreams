# Tasks — an unreachable body is an exclusion, not a failure

> Amend a task line when the work HAPPENS, not only when it succeeds. For an owner-run gate the task line is
> the only durable evidence.

## 1. Prove the defect before fixing it

- [ ] 1.1 Test: an entity whose `StorageRef` names an unregistered instance, in a component that owns an
      unrelated content store, produces a **durable failed record** and `IndexStateDegraded`. RED-first — it
      must pass against `origin/main`, documenting today's behaviour, then be inverted by the fix.
- [ ] 1.2 Prove the stickiness rather than assuming it: after the failed record exists, **re-deliver the
      entity** and confirm it fails again, and confirm the failed map is re-seeded across a restart. This is
      the claim that makes it permanent; if it does not reproduce, stop and re-derive before continuing.
- [ ] 1.3 Confirm the background repair loop does NOT touch `failReasonContentError` — the fix depends on
      there being no give-up path to add.

## 2. Bound the gate

- [ ] 2.1 `shouldFetchViaStorageRef` (`processor/graph-embedding/component.go:1974-1984`): fall through to the
      owned store only when `state.StorageRef.StorageInstance == c.contentStore.InstanceName()`.
- [ ] 2.2 Test: the owned-store fallback does not answer for a foreign instance. **Mutation-check the
      WIRING** — restore the bare `return c.contentStore != nil` and confirm the test goes red.
- [ ] 2.3 Confirm the registry arm is untouched: a registered instance still resolves exactly as today.

## 3. Reclassify at the fetch

- [ ] 3.1 `resolveStore` / `fetchTextFromStorage` (`graph/embedding/worker.go:986-1008`): return a
      **distinguishable** no-store-for-this-instance condition, matchable with `errors.Is` — not a string
      match on the message.
- [ ] 3.2 Route only that condition to `reportOffloadedContentExcluded` + a terminal outcome from inline text;
      no durable failed record is written.
- [ ] 3.3 A *resolved* store's `Open`/read failure still produces `failReasonContentError` with its existing
      recovery-on-re-delivery behaviour. Test both branches — a test that only covers the new path cannot
      detect that the old one was swallowed with it.
- [ ] 3.4 Test the gate/fetch race explicitly: resolvable at the gate, deregistered before the fetch → exclusion,
      not a durable failure. This is the reason the fix is in two places rather than one; without this test that
      decision is unevidenced.

## 4. Observability — the cost ledger's mitigation

- [ ] 4.1 Confirm `content_unresolved_total` increments for the new path and carries enough label to identify
      the instance.
- [ ] 4.2 Confirm the one-shot warning names the instance and the remedy (wire a `store-read` port for it),
      not just the failure.
- [ ] 4.3 Do NOT add a readiness gauge. **File** the "how many entities currently have unreachable bodies"
      question with the cost-ledger items from proposal.md — record the issue number on this line.

## 5. Gates

> Run what CI runs — BOTH suites. Check `^FAIL` rather than trusting a pipeline exit code.

- [ ] 5.1 `go build ./...`, `gofmt -l .` empty, `go vet` plain AND `-tags=integration`.
- [ ] 5.2 `task lint` — revive warnings are failures.
- [ ] 5.3 `go test -race ./...` — record ok/FAIL counts.
- [ ] 5.4 `go test -race -tags=integration -p 2 -count=1 ./...` — record counts. Take a `docker info` latency
      reading first; >1s is host debt, not a code signal.
- [ ] 5.5 `task schema:generate` then `git diff schemas/ specs/` showing zero drift.
- [ ] 5.6 `go test ./test/contract/...`.
- [ ] 5.7 Not BREAKING and no wire-surface change, so no e2e tier is owed. **If that judgement is wrong, say so
      rather than skipping quietly** — the touched path is embedding readiness, which `task e2e:semantic`
      covers.

## 6. Review

- [ ] 6.1 `semstreams-reviewer` on the full diff.
- [ ] 6.2 Codex round, then `--auto`. Record that it ran on this line.
- [ ] 6.3 Confirm the spec delta's MODIFIED requirement matches the archived text exactly apart from the
      intended carve-out — a partial MODIFIED loses detail silently at archive time.

## 7. Sequencing — do not lose this

- [ ] 7.1 **This must land and be OBSERVED before gh#873's store-registration step.** Between gh#873 repairing
      the reference and this landing, every trajectory-step entity would carry a reference most deployments
      cannot resolve — the permanent-degraded case, at trajectory-step cardinality. Record on gh#873 that this
      is its prerequisite.
- [ ] 7.2 Confirm at review that nothing from gh#873 — evidence, trajectory steps, retention — leaked into
      this diff. This change must be observable entirely on its own.

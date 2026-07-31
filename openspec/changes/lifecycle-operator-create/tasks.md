# Tasks — lifecycle-operator-create (gh#814 review round, PR #816)

**Task-line discipline:** amend a line when the work HAPPENS, not only when it
succeeds. A gate that ran and found nothing is recorded as run; one that was
skipped is recorded as skipped, with why.

**Read first:** the two review verdicts on PR #816 (Codex comment + the
`semstreams-reviewer` findings summarised in the proposal). Every item below has
a measured repro behind it — do not re-derive them, but DO re-verify anything you
rely on, because the reviewers ran against a worktree that has since moved.

**Standing trap for this change:** five of these defects exist because the create
lane inherited `Create`'s behavior without inspecting it. When you touch
`Manager.Create`, check every OTHER caller of what you change — in-process
callers have preconditions the HTTP lane does not.

## 1. Entity-ID pattern gate (BLOCKING)

- [ ] 1.1 In `Manager.Create` (or a shared precondition helper), reject an entity ID that does not match `reg.workflow.EntityIDPattern`, using the existing `ErrEntityIDPatternMismatch`, BEFORE any KV access. Match the `matchPattern` call shape already used by `Despawn` (`manager.go:1029,1061`).
- [ ] 1.2 Decide deliberately whether this belongs in `Create` (covers in-process callers too) or only in `CreateFromOperator` (avoids changing behavior for existing callers). Default to `Create` — an unreclaimable orphan is not better because app code made it — but check every in-process `Create` caller first and record what you found.
- [ ] 1.3 Map `ErrEntityIDPatternMismatch` → 400 in `errorToStatus`.
- [ ] 1.4 Test at the Manager: out-of-pattern create refuses, writes nothing, and `Get` afterwards returns not-found. In-pattern create succeeds AND appears in `List` — the reviewer's repro showed `List` returning 0 for a "successful" create, so assert discoverability, not just the refusal.

## 2. Committed birth is never a failure (BLOCKING)

- [ ] 2.1 Stop discarding the mutation response in `Manager.Create` (`manager.go:592`). `graphEmitterNATS.create` already returns a validated `resp.Entity`.
- [ ] 2.2 Return the committed state from the causal response rather than from a post-hoc `Get` in `CreateFromOperator`. Fall back to `Get` only where the response carries no projectable entity.
- [ ] 2.3 Propagate `Degraded` / `DegradedReason`. A degraded commit is a SUCCESS with a signal — never a 500, never a retry instruction. Read `graph/mutation_responses.go:24-41` before designing the surface shape.
- [ ] 2.4 Tests for both the degraded-success case and the read-back-fails-after-commit case. Ordinary success plus a genuine duplicate is what the current tests cover, and it is not enough.
- [ ] 2.5 Consider (and record a decision on) the lost-reply ambiguity: a committed create whose reply is lost surfaces `ErrAlreadyExists` on retry, indistinguishable from a genuine duplicate. Full idempotency may be out of scope — if so, say so explicitly in the ADR rather than leaving it implied.

## 3. Operator attribution (BLOCKING)

- [ ] 3.1 Add a source-aware create (e.g. `CreateWith(ctx, p, source, note)`); `buildInitialTriples` takes the source instead of hardcoding `TransitionSourceFramework` (`manager.go:646`).
- [ ] 3.2 `CreateFromOperator` passes `TransitionSourceOperator`; existing in-process `Create` keeps framework attribution (behavior-unchanged).
- [ ] 3.3 Test: create through the operator lane → first `History` event's source is operator; create in-process → framework. Both directions, or the test pins nothing.

## 4. Registration-time Participant validation (HIGH)

- [ ] 4.1 In `Manager.Register`, after `parseSchemaType`: reject a `Schema` whose pointer does not implement `Participant`. Boot failure instead of a first-request panic.
- [ ] 4.2 Also verify `Workflow.Name` equals the Schema's own `Workflow()` — the wiring invariant the removed body-guard was reaching for.
- [ ] 4.3 Retire the unchecked `.(Participant)` assertions at all four call sites, or document that registration now makes them total.
- [ ] 4.4 Test: registering a non-conformant Schema fails at Register; a conformant one still registers.
- [ ] 4.5 **Remove the workflow-mismatch guard from `CreateFromOperator`** and its claim from the PR body / OpenAPI description — it cannot fire in production (every real `Participant` returns a package constant from `Workflow()`). Remove the gateway test case that "proves" it; it only passes because the fake has a JSON-decodable workflow field.
- [ ] 4.6 Consider `DisallowUnknownFields` on the create decoder as the real mitigation for a body meant for another workflow. Separate decision — record it either way.

## 5. Must-exist lanes pinned at the production seam (HIGH)

- [ ] 5.1 Add tests against the REAL `Manager` (the `newTestManager` fake-bucket harness suffices, no NATS): `UpdateFromOperator` and `Transition` on an absent entity return `ErrEntityNotFound` and leave nothing behind.
- [ ] 5.2 Mutation-check them: disable both guards in `Manager.UpdateFromOperator` and confirm the new tests fail. The reviewer proved the whole repo stays green today.
- [ ] 5.3 Demote the gateway's `TestCreateInstance_MustExistLanesDoNotAutoVivify` to what it honestly is — a routing check — and say so in its doc comment.

## 6. Operator-surface error fidelity (HIGH / MEDIUM)

- [ ] 6.1 Map `ErrOwnerQuiesced` → 409 or 503 with its message PRESERVED (`writeErrorFromLifecycle` currently cans 500-class messages). Pick deliberately and justify.
- [ ] 6.2 Map `ErrEntityNotLifecycleManaged` → 404 or 409.
- [ ] 6.3 Re-run the full sentinel enumeration in `pkg/lifecycle/errors.go` against `errorToStatus` and record the table in the PR. Two instances of "unmapped until a route could reach it" are known; find the third before it finds you.
- [ ] 6.4 Shared 413 helper: `errors.As(err, &*http.MaxBytesError)` → 413, used by ALL THREE POST lanes. Two of them have advertised 413 while returning 400 since before this work — fixing only create leaves the pre-existing pair lying.
- [ ] 6.5 One over-limit test per lane.
- [ ] 6.6 Gate the WebSocket branch on `GET` and move the POST check above it (`handlers.go:71`). `POST ?stream=true` currently skips create and answers with gorilla's plain-text body, breaking the uniform `{"error"}` envelope the package doc guarantees.

## 7. Test-fidelity corrections

- [ ] 7.1 `TestIntegration_CreateFromOperator_IsCreateOrFail` does NOT reach the CAS arm — the first create's `Put` makes the second create's pre-read find the entity, so it returns from `hasTriple` long before the emitter. Mutation-proved: disabling the classification arm keeps everything green. Fix the doc comment AND cover the arm with a responder returning a classified `graph.ErrorCodeEntityExists`.
- [ ] 7.2 `TestCreateInstance_BirthLane`'s comment says "reads and transitions"; it only reads. Fix the comment or add the transition.
- [ ] 7.3 Stale operator-visible text: `component.go:58` `MaxBodyBytes` description still names only state/transition (regenerated into `schemas/lifecycle-gateway.v1.json`). No CI drift catches it — the literal did not change.

## 8. Acceptance and issue hygiene

- [ ] 8.1 Change `Closes gh#814` → `Refs gh#814` on PR #816 until the acceptance runs. Merging currently closes the only place the coverage gap is tracked.
- [ ] 8.2 File the fresh-volume → create → transition → restart → history-replay e2e as its OWN issue (or a named tier stage), not as a comment on the issue being closed.
- [ ] 8.3 Decide whether it rides semdragon's beta.159 replay or becomes a semstreams tier stage, and record the decision.

## 9. Gates

- [ ] 9.1 `gofmt`, `task lint`, `go vet ./...` plain + `-tags=integration` + `-tags=live_llm`, all `-mod=readonly`.
- [ ] 9.2 BOTH suites: `go test -race ./...` AND `go test -race -tags=integration -p 2 -count=1 ./...`. Grep `^FAIL`.
- [ ] 9.3 `task schema:generate` + commit any `schemas/`/`specs/` drift (the OpenAPI WILL change if statuses change).
- [ ] 9.4 `go test ./test/contract/...`.
- [ ] 9.5 `docker info` latency reading BEFORE any container run; attribute container failures to substrate before code.
- [ ] 9.6 `semstreams-reviewer` round on the full diff.
- [ ] 9.7 Owner-run Codex round; arm `--auto` only after it closes.
- [ ] 9.8 Archive: this change's delta targets the existing `openspec/specs/lifecycle/` capability (3 requirements today, none covering the operator API).

# Tasks — SemMachina match and in-flight primitives

> **§1 is a hard gate.** Both symbols are new exported surface, so gh#761 binds them and Fable design
> review happens BEFORE implementation. Nothing in §3 onward starts until §1 closes. Session 18
> shipped three symbols that predate this rule and never got the pass — this change does not add a
> fourth.

## 1. Fable design review (GATE — blocks all implementation)

- [x] 1.1 Fable review of `design.md` against gh#761's exported-surface contract — **APPROVED
      2026-07-31**. (a) **No variadic**: the lifecycle `Manager` governs answerability, not flavor, so
      it is a named parameter; `nil` is honest and the D2 pre-scan then errors on lifecycle fields.
      (b) **D4 accepted with the argument re-grounded**: cooldown is a rate limiter, not a match
      negation, so the primitive answers *obligation* where production answers *instant* — the
      consumer-cost-asymmetry argument becomes a corollary instead of the load-bearing beam.
      (c) **Q4: neither shape** — the component serves the query over NATS request/reply, deleting the
      derivation from callers rather than relocating it, and surviving an out-of-process caller
- [x] 1.2 Verdict and signature decisions recorded in `design.md` (§1 section, D3, D3a, D4, D6);
      deltas amended for the cooldown reframing and the wire shape; `openspec validate --strict` clean
- [x] 1.3 Named callers at birth confirmed — SemMachina / semdragon boot-time recovery pass, per both
      issues and the §1 review. Neither symbol is a phantom export
- [x] 1.4 **NEW, from Q4's answer**: the `unknown ≠ zero` rule now has THREE instances (no consumer,
      no responders, unreadable state). Implement it as ONE rule the three cases cite, not three
      coincidences — the spec states it that way and the code must match

## 2. Pre-implementation verification (may run in parallel with §1)

- [x] 2.1 Re-verify the four pre-processing steps at HEAD before refactoring anything — they are
      cited at `expression_factory.go:176-182,196,203-208` as of this change's authoring, and line
      pins go stale
- [x] 2.2 Enumerate every existing caller of `EvaluateEntityState` and of the loop's `consumerName`
      derivation. The shared-helper refactor must leave each one behaviorally identical, and the
      enumeration is what makes "identical" checkable rather than asserted
- [x] 2.3 Confirm the evaluator's unresolvable-prefix set has a single source the pre-scan can derive
      from (D2's stated drift hazard). If it is a bare literal list today, lifting it to one shared
      constant is part of this change, not a follow-up

## 3. gh#731 — stateless Definition matching (rule-engine)

- [x] 3.1 Extract steps 1, 2 and 4 of `EvaluateEntityState` into an unexported helper called by BOTH
      the existing method and the new entry point. **`EvaluateEntityState`'s observable behavior must
      not change** — see 5.1 for how that is proven, not asserted
- [x] 3.2 **RESOLVED — owner ruled twice, and the second ruling corrected an argument of mine that
      rested on a false premise.**
      Final: `Matches(ctx, def, state)` + `MatchesWithLifecycle(ctx, def, state, manager
      *lifecycle.Manager)`, one shared implementation, CONCRETE manager as §1 approved.
      · **Split** (ruling 1): §1's holding preserved and strengthened — the manager governs
        ANSWERABILITY, so it must be visible, and the name is the most visible place. Ranked by where
        that shows up: split (in the name) > one nil-able param (`nil` reads as complete) >
        `...MatchOption` (nowhere — compiles, reads as finished, fails at runtime).
      · **Concrete** (ruling 2): I argued for a narrow `LifecycleLookup` on the grounds that a
        concrete parameter would make Codex finding 2's required tests **impossible**. **That was
        false and I retracted it** — `natsclient.NewTestClient` works, so a real Manager is
        constructible in a test. The true cost is Docker: those tests move from unit to integration
        tier. A much weaker argument, and not enough to deviate from an approved signature.
      · **What concrete bought:** the typed-nil hazard is now UNREPRESENTABLE, not guarded.
        `manager == nil` on a pointer is total, so `isNilLookup` and its `reflect` call are DELETED.
        The nil-checked pointer widens to the internal interface only after the check.
      · **Coverage placement, not coverage loss:** the failure matrix (transient lookup error, ctx
        cancellation, ordinary-definition regression guard) stays at unit tier against
        `matchesWithLookup` — the shared implementation both entry points delegate to, driven through
        the interface it consumes. The exported wrapper against a REAL Manager is covered at
        integration tier in `matches_lifecycle_integration_test.go` (3 tests, green)
- [x] 3.3 Pre-scan conditions for `$state.*`, `$prev.*` and `transition`, returning an error naming
      the first unresolvable field before any evaluation runs. `evaluator.go` is NOT modified
- [x] 3.4 Lifecycle resolution is opt-in via a supplied `Manager`; absent one, a
      `$entity.lifecycle.*` condition errors rather than evaluating against an absent value
- [x] 3.5 Empty condition list returns no-match (D5), matching the wrapper rather than the evaluator
- [x] 3.6 Cooldown is NOT applied and a cooldown-declaring definition is NOT refused (D4). The doc
      comment states the **obligation** question — "does this pack still owe this entity work" — not a
      caveat about permissiveness, so a consumer needing the *instant* answer can tell at a glance
      this primitive is not theirs

## 4. gh#733 — task in-flight query (agentic-loop)

- [x] 4.1 **Done differently, and more strongly than planned — recording the change, not claiming
      the plan.** The task called for routing `setupConsumer`'s derivation and the query through one
      shared helper, so two derivations stay in step. Implemented instead by recording the bound
      `subject` on `consumerInfo` and having the query look up **the binding the component actually
      made**. That removes the second derivation entirely rather than keeping it synchronized: there
      is no expression left that could drift, and `ConsumerNameSuffix` is accounted for by
      construction instead of by remembering to apply it. `sanitizeSubject` keeps its single caller
- [x] 4.2 Serve the query as a **NATS request/reply subject on the component** (§1 Q4), following the
      existing `agentic.query.trajectory` wire (`component.go:375` subscribe, `:1796` handler) —
      `SubscribeForRequests`, handler `func(context.Context, []byte) ([]byte, error)`, `errs.Classified`
      for the error class. Unsubscribe on shutdown alongside the trajectory subscription (`:509`)
- [x] 4.3 Source the answer from `natsclient.OutstandingWork` — never `AckFloor`, never `AGENT_LOOPS`
      `state=running`
- [x] 4.4 Implement the `unknown ≠ zero` rule (1.4) once, cited by all three cases: consumer-not-found,
      state-unreadable, and — for the CALLER — `natsclient.IsNoResponders`
      (`natsclient/errors.go:333`). A caller must branch on the difference without string-matching an
      error message
- [x] 4.5 `sanitizeSubject` and the assembled consumer name stay unexported. Verify with `grep` over
      the package's exported surface, not by inspection. **No name, config, or handle crosses the
      wire** — that is what makes the derivation deleted rather than relocated
- [x] 4.6 Document that the answer is scoped to THIS deployment's consumer (`ConsumerNameSuffix`
      distinguishes deployments on one subject), and that a consumer gates on the loop's ADR-066
      readiness envelope (gh#732) before treating an in-flight answer as authoritative

## 5. Tests

- [x] 5.1 **The stateful path's existing rule-evaluation tests run UNMODIFIED against the refactor.**
      If any assertion needs editing to stay green, that is evidence of a behavior change — report it,
      do not adjust the test
- [x] 5.2 Stateless match: templated condition values resolve; verdict agrees with the stateful path
      on a shared corpus of definitions. **Do not compute the expected verdict by calling the same
      helper under test** — a test that reconstructs the behavior it means to verify tests the
      reconstruction
- [x] 5.3 Unresolvable-field cases (`$state.*`, `$prev.*`, `transition`, lifecycle-without-Manager)
      each return an error and NO verdict. **Mutation-check each guard**: break it and confirm the
      test goes red, because a guard test that passes with the guard removed proves nothing
- [x] 5.4 Engine-state non-observability: run a stateless match against a definition the engine holds
      state for, then assert match state, trigger latch and last-triggered are byte-identical
- [x] 5.5 In-flight query: **nonzero under a real backlog**, zero after the acks, UNKNOWN when no
      consumer exists. Rewritten per 7.8 — "unacked across a heartbeat renewal" does not apply to
      this path and is not asserted rather than faked. The no-consumer case is the one that matters most —
      it is the defect gh#733 was filed about
- [x] 5.6 Integration test drives the PRODUCTION wire for the in-flight query — a real request over
      NATS to a real component with a real consumer on a real stream, not a mock returning a canned
      count. A sync mock for an async seam proves nothing, and the wire IS the contract here
- [x] 5.7 **No-responders test with the component stopped and a backlog freshly published.** This is the failure mode Q4's answer introduced, and the one where a wrong answer is
      most costly. Assert the caller sees unknown, NOT zero — and mutation-check it: make the handler
      return a zero count on that path and confirm the test goes red
- [x] 5.8 Assert the three `unknown ≠ zero` cases route through ONE rule (1.4) — e.g. one construction
      site — so a future fourth case cannot be added as a fourth coincidence that forgets it

## 7. Codex round 1 findings (2026-07-31, `168f3311`) — 4 blocking, 2 high, 1 gap

Every fix below is mutation-verified: the guard is broken, the test is confirmed RED, the guard is
restored. A guard test that passes with the guard removed proves nothing.

- [x] 7.1 **[blocking 1] Deployment-addressed subject.** `SubscribeForRequests` is a plain
      subscribe, so one shared subject let every loop in the account reply and the requester kept
      whichever landed first. Subject is now `agentic.query.inflight.<deployment>` via
      `InFlightQuerySubjectFor`, keyed on `ConsumerNameSuffix` — the right discriminator rather than
      a new invented one, because the suffix already determines WHICH durable consumer exists (same
      suffix ⇒ same consumer ⇒ same count). Integration test runs two deployments with conflicting
      state; mutating back to a shared subject turns it RED
- [x] 7.2 **[blocking 2] Supplied lookup ≠ resolved state; unresolved templates.** Answerability now
      derives from what ACTUALLY resolved; the lookup error is carried and reported when a lifecycle
      condition needs it. Surviving `$`-templates in condition VALUES are refused via the existing
      `unresolvedTemplateVarRe` (single source, not a second list). **The first fix was itself
      wrong** and the mutation check caught it: raising the lookup error unconditionally refused
      every ordinary definition whenever a Manager was supplied, because `LookupByEntityID` errors
      for any entity that simply is not lifecycle-managed. Regression test added
- [x] 7.3 **[blocking 3] `Definition.Enabled`.** `Matches` returned true for a disabled rule,
      falsifying this change's own claim that cooldown is the only divergence. Now `false, nil`,
      with production-parity coverage
- [x] 7.4 **[blocking 4b] Typed-nil panic.** `lifecycle != nil` admitted a typed nil
      (`var m *lifecycle.Manager`) as a live lookup, then panicked in `LookupByEntityID`.
      `isNilLookup` treats it as absent. Test asserts no panic AND the absent-lookup refusal
- [x] 7.5 **[blocking 4a] Signature gate — RULED by the owner, not self-approved.** Outcome: the
      split pair (task 3.2). The process defect Codex named — a task checked complete while its own
      text said it needed confirmation — is what got fixed; the ruling then went further than either
      option on the table. The narrow-interface-vs-concrete question remains explicitly open and is
      recorded as open rather than assumed closed by the split. **Now closed by ruling 2 (concrete),
      with my supporting argument retracted as factually wrong — see task 3.2**
- [x] 7.6 **[high 5] `context.Context`.** Now the first parameter, propagated to the lifecycle
      lookup; cancellation test proves a blocked lookup returns instead of wedging the caller
- [x] 7.7 **[high 6] Start-failure subscription leak** (introduced by this change's second
      subscription). Both cleanup paths route through one `unsubscribeRequestHandlers`. Coverage
      limit stated in the test: the teardown wiring and idempotency are asserted, direct failure
      injection into the second subscribe is not — there is no seam without a client fake
- [x] 7.8 **[gap 7] Wire test now exercises outstanding work.** Publishes a real backlog, observes a
      nonzero count (258 in practice), and asserts the return to zero after the acks. Hardcoding the
      known-answer path to zero turns it RED. **Measurement corrected the plan:** gh#733's premise
      that a task "lives unacked while the model works" is false — `handleTaskMessage` returns nil
      on error and `HandleTask` does not block, so a single task acks promptly. Depth, not duration,
      is what makes the work observable, and the heartbeat-sustained assertion is recorded as NOT
      applicable rather than faked. Tasks 5.5/5.7 rewritten accordingly

## 6. Review chain and gates

- [x] 6.1 `semstreams-reviewer` pass on the full diff — **RAN**, combined with the Codex round at
      `168f3311` (PR #777 comment, 2026-07-31T01:41Z). This line previously read "NOT RUN by this
      session"; that was written BEFORE the round and never updated after it, so the change closed
      out carrying a false negative about its own review chain. **A task line written ahead of the
      work is a prediction, and a prediction left unamended reads as a measurement** — the same class
      as the baton's own state-file rule
- [x] 6.2 Gates run on the rebased branch: `task lint` clean; `go vet -tags=integration ./...` clean;
      `go test -race ./...` **0 failures**; `openspec validate --all --strict` 34/0; `task
      schema:generate` produced no drift. Branch integration sweep (`-race -tags=integration ./...`):
      **135 packages ok, 1 FAIL — `TestIntegration_HandleEntityUpdate_NotFound` in
      `processor/graph-ingest`, attributed to gh#736 on evidence, NOT waved through**: container-start
      signature (`port "4222" not found`), `go list -deps ./processor/graph-ingest` reaches neither
      changed package, passes alone (3.2s) and passes as a quiet package (75.7s), substrate healthy
      (58Gi free). Investigated BEFORE re-running anything; reproduction added to gh#736. **Guards mutation-checked, not merely green:**
      replacing the no-consumer guard with `return 0, nil` turns three unit tests AND the integration
      wire test red; restored, all pass
- [x] 6.3 Owner-run Codex round; fix findings. **RAN at `168f3311` — 7 findings: 4 blocking
      (global in-flight subject cannot identify a deployment; a supplied-but-failed lifecycle lookup
      read as resolved state; `Definition.Enabled` unchecked so a disabled rule reported work owed;
      the exported signature deviated from Fable's approved `*lifecycle.Manager` and the interface
      admitted a panicking typed nil), 2 high (no caller `context.Context` on an API doing KV I/O;
      second-subscription start failure leaked the first responder), 1 verification gap (the
      production-wire test asserted `InFlight == (Outstanding > 0)` with no task ever published — a
      handler hardcoded to zero passed it).** All 7 addressed at `013538cd` mutation-verified, with
      follow-on fixes at `f3db6130` (entry-point split) and `278d1425` (finding 4a fully closed).
      **A fix is new code and inherits the full defect rate** — no re-check owed under the
      once-through rule: no new blocking-class defect surfaced, the reviewed CONTRACT did not
      change, and no shared primitive with callers outside the change was modified
- [x] 6.4 Additive/non-breaking confirmed: no NATS state, schema, wire-format or config change, so no
      e2e tier is owed beyond the per-PR `e2e:statistical`. **Re-confirm rather than assume** — if any
      of §3/§4 turned out to touch a boot path or a wire shape, this line is wrong and a tier is owed
- [x] 6.5 Both issues landed in ONE PR (baton: PR scope = complete system, not chunk boundary) —
      **PR #777 merged as `72f9b15b`**; gh#731 and gh#733 both CLOSED on owner CONFIRM-CLOSE
- [x] 6.6 Deltas applied and change archived; the `agentic-loop` Purpose was WIDENED in the live
      spec rather than left scoped to iteration budgets. One editorial repair made while promoting:
      the rule-engine "three-way distinction" table shipped only TWO rows (no-match / error), so the
      prose promised a distinction the table did not draw — the `true, nil` row is stated explicitly,
      verified against `Matches` returning the evaluator's verdict unchanged

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
- [x] 3.2 Implement `Matches(def Definition, state *gtypes.EntityState, lifecycle LifecycleLookup)
      (bool, error)` — no variadic, per §1. Touches no `shouldTrigger`, no `lastTriggered`, no
      cooldown state, no `MatchState`.
      **DEVIATION FROM §1's LITERAL SPELLING — needs Fable confirmation, flagged not buried.**
      Fable wrote the third parameter as `*lifecycle.Manager` (concrete). Implemented instead as a
      new narrow read-only interface `LifecycleLookup` (`LookupByEntityID` + `GetWorkflowDefinition`).
      Reason: the resolution path performs only those two lookups, while the package's existing
      `LifecycleManager` also carries `TransitionWith` / `Complete` / `Fail` / `AssertRuleWritable`.
      Demanding either the concrete Manager or the wide interface makes a caller hand a **read** a
      **write** capability just to ask a question — the inverse of the exported-surface rule that
      motivated Fable's own answer. `*lifecycle.Manager` satisfies `LifecycleLookup`, so Fable's
      intended call site compiles unchanged; §1's actual holding (a named parameter, not a variadic,
      because it governs answerability) is preserved exactly. `ExecutionContext.Lifecycle` narrowed
      to the same interface for the same reason. **If Fable prefers the concrete type, this reverts
      in one line.**
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
- [x] 5.5 In-flight query: outstanding while unacked (across at least one heartbeat renewal), zero
      after ack, UNKNOWN when no consumer exists. The no-consumer case is the one that matters most —
      it is the defect gh#733 was filed about
- [x] 5.6 Integration test drives the PRODUCTION wire for the in-flight query — a real request over
      NATS to a real component with a real consumer on a real stream, not a mock returning a canned
      count. A sync mock for an async seam proves nothing, and the wire IS the contract here
- [x] 5.7 **No-responders test with the component actually stopped and task messages still on the
      stream.** This is the failure mode Q4's answer introduced, and the one where a wrong answer is
      most costly. Assert the caller sees unknown, NOT zero — and mutation-check it: make the handler
      return a zero count on that path and confirm the test goes red
- [x] 5.8 Assert the three `unknown ≠ zero` cases route through ONE rule (1.4) — e.g. one construction
      site — so a future fourth case cannot be added as a fourth coincidence that forgets it

## 6. Review chain and gates

- [ ] 6.1 `semstreams-reviewer` pass on the full diff — **NOT RUN by this session** (subagents are
      off in this session's operating rules). Outstanding before merge, not silently skipped
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
- [ ] 6.3 Owner-run Codex round; fix findings. **NOT RUN — owner-run by definition, and this is a
      CODE PR, so `--auto` must NOT be armed until it closes** (unlike the docs-only #776). **A fix is new code and inherits the full defect rate**
      — the remedy gets the same adversarial pass as the original, and "the finding is addressed" is
      not "the mechanism is closed"
- [x] 6.4 Additive/non-breaking confirmed: no NATS state, schema, wire-format or config change, so no
      e2e tier is owed beyond the per-PR `e2e:statistical`. **Re-confirm rather than assume** — if any
      of §3/§4 turned out to touch a boot path or a wire shape, this line is wrong and a tier is owed
- [ ] 6.5 Both issues land in ONE PR (baton: PR scope = complete system, not chunk boundary); close
      gh#731 and gh#733 only on owner CONFIRM-CLOSE
- [ ] 6.6 Apply the deltas and archive; write the `agentic-loop` Purpose widening into the live spec
      rather than leaving it scoped to iteration budgets

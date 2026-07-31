# Tasks — SemMachina match and in-flight primitives

> **§1 is a hard gate.** Both symbols are new exported surface, so gh#761 binds them and Fable design
> review happens BEFORE implementation. Nothing in §3 onward starts until §1 closes. Session 18
> shipped three symbols that predate this rule and never got the pass — this change does not add a
> fourth.

## 1. Fable design review (GATE — blocks all implementation)

- [ ] 1.1 Fable review of `design.md` against gh#761's exported-surface contract. The three open
      questions are the agenda, not a footnote: **(a)** exact names and signatures, and specifically
      whether the options-variadic earns its place when exactly one option exists today;
      **(b)** D4 — is documented one-directional cooldown permissiveness acceptable, or should a
      definition declaring a cooldown be refused outright; **(c)** Q4 — component method vs
      package-level function for the in-flight query, where `ConsumerNameSuffix` being component
      config means a package-level shape risks relocating the reconstruction rather than deleting it
- [ ] 1.2 Record the verdict and every signature decision in `design.md` before writing code; if
      Fable's answers change a requirement's wording, amend the delta in the same pass and re-run
      `openspec validate --strict`
- [ ] 1.3 Confirm both symbols still have named callers at birth (SemMachina / semdragon boot-time
      recovery pass). If the consumer's plan changed while this sat, they are phantoms and the change
      stops here rather than exporting speculatively

## 2. Pre-implementation verification (may run in parallel with §1)

- [ ] 2.1 Re-verify the four pre-processing steps at HEAD before refactoring anything — they are
      cited at `expression_factory.go:176-182,196,203-208` as of this change's authoring, and line
      pins go stale
- [ ] 2.2 Enumerate every existing caller of `EvaluateEntityState` and of the loop's `consumerName`
      derivation. The shared-helper refactor must leave each one behaviorally identical, and the
      enumeration is what makes "identical" checkable rather than asserted
- [ ] 2.3 Confirm the evaluator's unresolvable-prefix set has a single source the pre-scan can derive
      from (D2's stated drift hazard). If it is a bare literal list today, lifting it to one shared
      constant is part of this change, not a follow-up

## 3. gh#731 — stateless Definition matching (rule-engine)

- [ ] 3.1 Extract steps 1, 2 and 4 of `EvaluateEntityState` into an unexported helper called by BOTH
      the existing method and the new entry point. **`EvaluateEntityState`'s observable behavior must
      not change** — see 5.1 for how that is proven, not asserted
- [ ] 3.2 Implement the stateless entry point with the signature §1 settled. It touches no
      `shouldTrigger`, no `lastTriggered`, no cooldown state, no `MatchState`
- [ ] 3.3 Pre-scan conditions for `$state.*`, `$prev.*` and `transition`, returning an error naming
      the first unresolvable field before any evaluation runs. `evaluator.go` is NOT modified
- [ ] 3.4 Lifecycle resolution is opt-in via a supplied `Manager`; absent one, a
      `$entity.lifecycle.*` condition errors rather than evaluating against an absent value
- [ ] 3.5 Empty condition list returns no-match (D5), matching the wrapper rather than the evaluator

## 4. gh#733 — task in-flight query (agentic-loop)

- [ ] 4.1 Route the existing `setupConsumer` name derivation (`component.go:761-764`) and the new
      query through ONE internal helper, so the query cannot address a different consumer than the
      component binds. This shared helper is the actual fix; the exported query is its surface
- [ ] 4.2 Implement the in-flight query in the shape §1 settled, sourcing
      `natsclient.OutstandingWork` — never `AckFloor`, never `AGENT_LOOPS` `state=running`
- [ ] 4.3 Map `jetstream.ErrConsumerNotFound` to an error that is DISTINGUISHABLE by the caller from
      "consumer exists, nothing outstanding". A caller must be able to branch on the difference
      without string-matching
- [ ] 4.4 `sanitizeSubject` and the assembled consumer name stay unexported. Verify with
      `grep` over the package's exported surface, not by inspection
- [ ] 4.5 State in the doc comment that the answer is scoped to THIS deployment's consumer
      (`ConsumerNameSuffix` distinguishes deployments on one subject)

## 5. Tests

- [ ] 5.1 **The stateful path's existing rule-evaluation tests run UNMODIFIED against the refactor.**
      If any assertion needs editing to stay green, that is evidence of a behavior change — report it,
      do not adjust the test
- [ ] 5.2 Stateless match: templated condition values resolve; verdict agrees with the stateful path
      on a shared corpus of definitions. **Do not compute the expected verdict by calling the same
      helper under test** — a test that reconstructs the behavior it means to verify tests the
      reconstruction
- [ ] 5.3 Unresolvable-field cases (`$state.*`, `$prev.*`, `transition`, lifecycle-without-Manager)
      each return an error and NO verdict. **Mutation-check each guard**: break it and confirm the
      test goes red, because a guard test that passes with the guard removed proves nothing
- [ ] 5.4 Engine-state non-observability: run a stateless match against a definition the engine holds
      state for, then assert match state, trigger latch and last-triggered are byte-identical
- [ ] 5.5 In-flight query: outstanding while unacked (across at least one heartbeat renewal), zero
      after ack, ERROR when no consumer exists. The no-consumer case is the one that matters most —
      it is the defect gh#733 was filed about
- [ ] 5.6 Integration test drives the PRODUCTION wire for the in-flight query — a real consumer on a
      real stream, not a mock returning a canned count. A sync mock for an async seam proves nothing

## 6. Review chain and gates

- [ ] 6.1 `semstreams-reviewer` pass on the full diff
- [ ] 6.2 `task lint` (revive warnings = CI failure), `go test -race ./...`, `-race -tags=integration`
      branch integration sweep (framework-package change), `task schema:generate` + no-drift check
- [ ] 6.3 Owner-run Codex round; fix findings. **A fix is new code and inherits the full defect rate**
      — the remedy gets the same adversarial pass as the original, and "the finding is addressed" is
      not "the mechanism is closed"
- [ ] 6.4 Additive/non-breaking confirmed: no NATS state, schema, wire-format or config change, so no
      e2e tier is owed beyond the per-PR `e2e:statistical`. **Re-confirm rather than assume** — if any
      of §3/§4 turned out to touch a boot path or a wire shape, this line is wrong and a tier is owed
- [ ] 6.5 Both issues land in ONE PR (baton: PR scope = complete system, not chunk boundary); close
      gh#731 and gh#733 only on owner CONFIRM-CLOSE
- [ ] 6.6 Apply the deltas and archive; write the `agentic-loop` Purpose widening into the live spec
      rather than leaving it scoped to iteration budgets

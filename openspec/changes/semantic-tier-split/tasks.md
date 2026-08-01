# Tasks — semantic-tier-split

**Amend a task line when the work HAPPENS, not only when it succeeds.** A gate that
ran and was never recorded is indistinguishable from one that was skipped — that
misreading cost three separate sessions. A deliberate not-done gets `[~]` AND its
reasoning AND propagation into the spec delta: a `[~]` stops the implementer and
does not stop the archiver.

## 1. Measure before designing the split (the gate on everything below)

- [ ] 1.1 **Measure the proposed CI stack under a 4-vCPU constraint** — `semembed` +
      `seminstruct-fast` (0.6b), run locally with a `--cpus=4` / `cpuset` limit to
      simulate a free GitHub runner (public repo: 4 vCPU / 16GB / ~14GB disk). Record
      wall time, whether community-summary enhancement completes within its timeout,
      and the count of `answer_synthesis_timeout` / LLM-retry warnings. This is the
      same empirical method that root-caused gh#830 — do not wire CI on an
      unconstrained run.
- [ ] 1.2 Record the baseline for comparison: the same stack **unconstrained**, so the
      constrained numbers have something to be read against. A single constrained run
      cannot distinguish "too slow for 4 vCPU" from "slow everywhere".
- [ ] 1.3 If 1.1 fails: measure `semembed`-only under the same constraint, and record
      explicitly which assertion (`tiered_semantic.go:543`, community-summary LLM
      enhancement) is being dropped from the per-PR gate and where it lands instead.
      **No silent omission** — a gate that stops covering something must say so.
- [ ] 1.4 Verify the image-pull budget on a cold runner: `semembed` 205MB + 0.6b 997MB.
      Disk was measured as not-binding locally (~3.6GB of ~14GB) but a cold CI runner
      pulls rather than reuses, so pull time is a distinct cost from disk.

## 2. Split the compose profile

- [ ] 2.1 Split the single `semantic` profile in `docker/compose/tiered.yml` into
      `semantic` (retrieval) and `semantic-rag` (generation). The scenario code is
      already split — `tiered_semantic.go` vs `tiered_semantic_known_answer.go` — so
      this is the only place the two are welded together.
- [ ] 2.2 The app service's `depends_on` currently requires all three `seminstruct`
      services. Make the generation-only dependencies conditional on the profile, and
      **verify the retrieval tier actually starts with them absent** — a `depends_on`
      left in place would make "runs without an LLM" false while looking correct.
- [ ] 2.3 Confirm the retrieval tier fails honestly when `semembed` is absent: it must
      report a missing embedding service, not silently degrade to Tier 1 and pass. A
      tier that passes without the capability it exists to test is the worst outcome
      of this change.

## 3. Diagnostic contracts (the deliverable, not a side effect)

- [ ] 3.1 Write the diagnostic contract for the retrieval tier: what it exercises, and
      **what a red result rules out**. Store it where the tier is defined, not only in
      docs, so it is read by whoever is looking at the failure.
- [ ] 3.2 Same for the generation tier, including that its assertions are
      quality-graded and non-deterministic, and what that means for reading a failure.
- [ ] 3.3 Audit the OTHER tiers for the same gap. gh#830 proved the cost of a tier with
      no diagnostic contract; fixing one instance and leaving the class is the pattern
      this repo has been bitten by repeatedly. Record which tiers lack one even if they
      are not fixed here.

## 3b. Quality assertions — derived from adopter ground truth, and GATED on gh#829

**Do not author anything in this section until gh#829 lands.** Today's grader reports
`grounded=true, recall=1.00` on summaries composed entirely of entity-ID and predicate
segments. Writing assertions against current output would pin the defect as the
contract — the exact failure this repo keeps hitting, where a test encodes behaviour
instead of the requirement.

- [ ] 3b.1 **BLOCKED ON gh#829.** Assert a community summary contains corpus content, not
      an ID taxonomy. Falsifiable form: a summary MUST NOT be composed solely of tokens
      derivable from entity IDs and predicate names. Today's live output
      (`Key themes: completed, content.classification.tag, maintenance.work.status`) is
      the negative fixture — it must FAIL the new assertion, and that must be
      demonstrated, not assumed.
- [ ] 3b.2 Separate the two things the current grader conflates: **retrieval recall**
      (did the expected entity IDs come back) from **generation groundedness** (is the
      prose supported by corpus content). Reporting the first as the second is why gh#829
      shipped under a green tier. The retrieval half belongs to the retrieval tier; the
      generation half to `semantic-rag`.
- [ ] 3b.3 Add a **sub-threshold** probe. Current probes return 0, 5 and 68 entities
      against `DefaultSummarizeThreshold = 50`, so the at-or-below-threshold path — which
      gh#823 measures as the common case on a normal corpus — is exercised but never
      asserted end to end.
- [ ] 3b.4 Cover the **agent-facing** path. Neither semantic scenario file references
      `search_graph` (0 occurrences), so gh#823's second direction — the formatter
      rendering only `EntityDigests` and silently dropping a populated `Entities` set — is
      covered by no tier at all. Decide whether it belongs here or in the agentic tier,
      and record the decision either way.
- [ ] 3b.5 Assert `Strategy` reaches the wire (gh#819). Reproduced: `strategy=` is empty
      on all five graded probes. Cheap, and it is a wire-contract assertion rather than a
      quality one, so it can land in the retrieval tier ahead of gh#829.
- [ ] 3b.6 Read semsource's tracker (`docs/upstream/semstreams-asks.md`) for adjacent
      *candidate* asks before finalising the assertion set — solve the family, not the
      instance. **Read only; semsource owns that file.** Confirm each ask is
      framework-shaped rather than product-shaped before deriving an assertion from it.

## 4. Documentation — the plain-language tier ladder

- [ ] 4.1 Rewrite the tier section of `docs/concepts/00-real-time-inference.md` in terms
      of **what a user can now find**, not the mechanism: Tier 1 finds entities whose
      text contains your words; Tier 2 also finds entities that use *different* words
      for the same thing (the doc's own example: "Machine" matches "equipment" and
      "device"). State the external-service cost of Tier 2 next to its benefit.
- [ ] 4.2 State explicitly that **LLM answer synthesis is not a tier on this ladder** —
      retrieval decides what is relevant, generation decides how to say it. This is the
      confusion the split exists to resolve; leaving it undocumented would ship the
      split and keep the ambiguity.
- [ ] 4.3 Keep the existing telemetry-only guidance prominent: tiers do nothing for
      entities without text, so a higher tier on a sensor-only graph is pure cost.
- [ ] 4.4 Correct the stale tier timings in `CLAUDE.md` — it advertises `e2e:semantic`
      as "~90s" against a **measured 11m16s–11m54s** across five runs. Re-measure each
      tier's line rather than fixing only the one that was noticed; a table with one
      corrected number and four stale ones is not more trustworthy than before.

## 5. CI wiring — only if section 1 supports it

- [ ] 5.1 Wire the retrieval tier into CI **only** if 1.1 passed under constraint. If it
      did not, wire what did and record the rest as still-manual, referencing gh#769.
- [ ] 5.2 Add the generation tier to a nightly, per gh#769. A 2-in-5 intermittent rate
      (gh#830's measured rate) is invisible per-PR and needs repetition to surface.
- [ ] 5.3 **Verify the new CI stage can actually fail.** gh#811 found five tiers that
      exit 0 on scenario failure via `ignore_error: true`; a new stage that inherits
      that shape would look like coverage and gate nothing. Drive it to a failing state on purpose
      once and confirm the job fails.
- [ ] 5.4 Confirm the required-checks ruleset is updated if this stage is meant to gate,
      or explicitly record that it is advisory. A stage nobody requires is a slow test.

## 6. Gates

- [ ] 6.1 `gofmt`, `task lint`, `go vet ./...` plain + `-tags=integration` + `-tags=live_llm`, `-mod=readonly`.
- [ ] 6.2 BOTH suites: `go test -race ./...` AND `go test -race -tags=integration -p 2 -count=1 ./...`. Grep `^FAIL` — pipeline exit codes report the tail stage.
- [ ] 6.3 `task schema:generate` + `git diff schemas/ specs/` clean.
- [ ] 6.4 Both split tiers run green locally before CI wiring, and the retrieval tier is
      run **at least three times** — gh#830's failure was 2-in-5, so a single green run
      is not evidence of stability for a tier being promoted into a gate.
- [ ] 6.5 `semstreams-reviewer` pass on the full diff.
- [ ] 6.6 Owner-run Codex round; arm `--auto` only AFTER it closes.
- [ ] 6.7 Archive: seed `openspec/specs/e2e-tiers/` with a WRITTEN Purpose (not the
      `TBD - created by archiving` stub) plus an explicit statement of what it does NOT
      cover.

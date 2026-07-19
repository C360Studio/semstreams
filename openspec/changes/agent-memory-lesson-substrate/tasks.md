# Tasks: agent-memory-lesson-substrate

## 1. Gate: adversarial review of ADR-080

- [x] 1.1 Run the 5-lens adversarial review against this change (ADR-079 precedent); record
      verdicts in `adversarial-review.md` (DONE 2026-07-19 — 5× READY-WITH-CHANGES, all
      findings folded into the drafts)
- [x] 1.2 Owner accepts the three review pivots (brief-assembly delivery with deferred fusion
      facet; proposed→active lifecycle; `agent.lesson.*` namespace); flip ADR-080 Status to
      Accepted (DONE 2026-07-19)

## 2. Vocabulary

- [ ] 2.1 Define the `lesson.*` predicate family in `vocabulary/agentic/predicates.go`
      (closed enums: polarity/severity/status; OPEN category; opaque text:
      summary/detail/injection_form; refs: evidence/applies_to; lifecycle:
      retired_at/superseded_by; observed_role) with when-to-use doc comments
- [ ] 2.2 Register the family in `vocabulary/agentic/register.go` — explicit
      `WithRuleOpaque(true)` on authored text, `WithIRI(vocabulary.ProvWasDerivedFrom)` on
      `lesson.evidence` (constants already exist in `standards.go:156-399` — registration
      only, nothing added there); extend registry tests (rule-visibility split, open
      category, StandardIRI assertions)

## 3. emit_lesson executor

- [ ] 3.1 Implement `processor/agentic-tools/emit_lesson.go` mirroring `emit_diagnosis.go`
      for the create path: `create_with_triples` mint of
      `{org}.{platform}.agent.lesson.record.{uuid5(content)}` (content-derived identity:
      category + sorted applies_to + summary + sorted evidence), `status=proposed` at birth,
      envelope, attribution via `TryLoopExecutionEntityID`, `StopLoop:false`; add the
      `AgentLesson*` entity-ID/message-type twins beside `ops_diagnosis_entity.go`
- [ ] 3.2 Writer gates with instructive rejections: ≥1 well-formed 6-part evidence entity ID
      (shape at emit; existence checks live at promotion), injection-form byte bound
      (320-byte constant), typed `applies_to` grammar (`id:` ≥3 segments / `tag:`), per-loop
      emission cap (default 20); table-driven unit tests for every accept/reject path,
      idempotent re-emit, derived attribution
- [ ] 3.3 Register in `executors/register.go` (RegisterBuiltins, NATSClient-gated, beside
      emit_diagnosis); tool schema asks intent only; registry enumeration test confirms no
      dedicated lesson search/list/query tool
- [ ] 3.4 Integration-tier test driving `emit_lesson` through the production tool wire
      (`tool.execute` → `tool.result`, not helper-direct), including the idempotency path
      against a real NATS container

## 4. Lifecycle lane + brief-assembly injection

- [ ] 4.1 Promotion/retirement lane: document + test the `update_with_triples` replace path
      for `lesson.status`/`lesson.superseded_by` (rule `replace_owned` example config +
      product-writer path via the owned-fact writer); promotion validates every cited
      evidence entity exists (refuse and stay `proposed` otherwise)
- [ ] 4.2 Deterministic lesson matcher as a reusable package (input: loop scope
      entity-IDs/tags; output: bounded ordered active lessons): segment-boundary id-prefix +
      tag matching, severity → stored emit-timestamp → entity-ID ordering, K ceiling (≤25,
      default 10) + total-byte budget, proposed/retired/superseded exclusion,
      matched-vs-included counts (designed for reuse by the future `want:[lessons]` facet)
- [ ] 4.3 Brief-assembly injection step in `processor/agentic-loop` prompt construction:
      render matcher output (injection forms + entity IDs + counts) into the system prompt at
      dispatch; unit tests per spec scenarios — proposed excluded, bounded+observable,
      replay-stable ordering (emit-time triples, not UpdatedAt/KV revision)
- [ ] 4.4 Observability: rejection counter on `emit_lesson` (reason label:
      evidence/bound/grammar/cap) and an injection counter (matched/included) on the
      brief-assembly step

## 5. First consumer: ops flow + e2e

- [ ] 5.1 Add `emit_lesson` to the ops allowlist in `configs/flows/ops-agent.json` +
      `-test.json`; extend the ops persona with the emit contract (evidence, bound, typed
      scope, cap) in decision-criteria phrasing
- [ ] 5.2 Extend `test/e2e/scenarios/ops` to the full gated round-trip, hard-fail at every
      step: ops loop emits lesson → `proposed` entity queryable via `/graph/triples` →
      promotion write flips it `active` → a subsequent loop's brief contains the injection
      form; run the ops e2e tier green

## 6. Retire processor/agentic-memory

- [ ] 6.1 Pre-delete sweep (corrected enumeration): package imports (expect zero),
      `processor/agentic-loop/component.go:1575` context-event publish leg (prune with this
      change), `agentic-loop/doc.go` lines 316-321/415, `configs/flows/deep-research.json`
      dead block, `config/rules/agentic-memory/` rulepack (coincidental name — ADR-017
      extraction rules, out of scope, leave untouched), component tables in `AGENTS.md` +
      `CLAUDE.md`, `docs/basics/07-agentic-quickstart.md`,
      `docs/concepts/13-agentic-systems.md` (§"agentic-memory Integration"),
      `processor/agentic-loop/README.md` (§"agentic-memory Integration"), `docs/ROADMAP.md`,
      ADR-043 references
- [ ] 6.2 Delete the package; prune the publish leg and every stale reference from 6.1;
      update ADR-027's status paragraph to name `emit_lesson` beside `emit_diagnosis`
- [ ] 6.3 Framework-change branch integration sweep: `go test -race -tags=integration ./...`

## 7. User-facing docs

- [ ] 7.1 New concepts page `docs/concepts/26-agent-memory.md`: push-vs-pull principle (why
      pull memory tools failed; ADR-080), the three layers mapped to framework surfaces, the
      symptom→layer→provider decision matrix (incl. semsource as the reference
      semantic-content producer for source-grounded agents), the lesson lifecycle
      (proposed→active→retired) and "lessons carry policy, not facts" (facts live in the
      graph; lessons cite them)
- [ ] 7.2 Cross-link from `docs/concepts/13-agentic-systems.md` and ADR-027/028; example
      category taxonomies documented here (docs, never framework enums)

## 8. Handoff + ship gates

- [ ] 8.1 File the semteams upstream issue: adopt the lesson primitive (load/fix the dormant
      ops pack incl. the stale `reviewer-qa` trigger, own chain-terminal trigger rules and
      promotion policy), referencing this change and ADR-080
- [ ] 8.2 `/preflight` — lint (revive clean), `-race` unit + integration, schema no-drift,
      contract tests; classify any failure fix-now / file-with-Skip / document
- [ ] 8.3 semstreams-reviewer pre-merge review; fix findings
- [ ] 8.4 PR (complete-system scope: vocab + executor + lifecycle lane + injection +
      consumer + removal); merge; verify `openspec list` before any completion claim

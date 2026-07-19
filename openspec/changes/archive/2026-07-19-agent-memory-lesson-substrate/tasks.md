# Tasks: agent-memory-lesson-substrate

## 1. Gate: adversarial review of ADR-080

- [x] 1.1 Run the 5-lens adversarial review against this change (ADR-079 precedent); record
      verdicts in `adversarial-review.md` (DONE 2026-07-19 — 5× READY-WITH-CHANGES, all
      findings folded into the drafts)
- [x] 1.2 Owner accepts the three review pivots (brief-assembly delivery with deferred fusion
      facet; proposed→active lifecycle; `agent.lesson.*` namespace); flip ADR-080 Status to
      Accepted (DONE 2026-07-19)

## 2. Vocabulary

- [x] 2.1 Define the `lesson.*` predicate family in `vocabulary/agentic/predicates.go`
      (closed enums: polarity/severity/status; OPEN category; opaque text:
      summary/detail/injection_form; refs: evidence/applies_to; lifecycle:
      retired_at/superseded_by; observed_role) with when-to-use doc comments
      (DONE 2026-07-19 — realized as `agent.lesson.*` per ADR-080's mandated namespace,
      NOT flat `lesson.*`: the canonical predicate contract [PR #532] requires 3-part
      lower-kebab, so 2-part/underscored shorthand panics at registration. 12 consts,
      hyphenated multi-word properties, mirrors `agent.todo.*`/`agent.scratch.*` siblings)
- [x] 2.2 Register the family in `vocabulary/agentic/register.go` — explicit
      `WithRuleOpaque(true)` on authored text, `WithIRI(vocabulary.ProvWasDerivedFrom)` on
      `lesson.evidence` (constants already exist in `standards.go:156-399` — registration
      only, nothing added there); extend registry tests (rule-visibility split, open
      category, StandardIRI assertions) (DONE 2026-07-19 — `registerLessonPredicates` wired
      after `registerOpsPredicates`; TestLessonPredicatesRegistered [3 opaque / 9 matchable],
      TestRegistration IRI row, TestPredicateCount 112→124; `-race`+vet+gofmt green. Spec
      delta + design.md reconciled to `agent.lesson.*`; design.md carries a post-review banner)

## 3. emit_lesson executor

- [x] 3.1 Implement `processor/agentic-tools/emit_lesson.go` mirroring `emit_diagnosis.go`
      for the create path: `create_with_triples` mint of
      `{org}.{platform}.agent.lesson.record.{uuid5(content)}` (content-derived identity:
      category + sorted applies_to + summary + sorted evidence), `status=proposed` at birth,
      envelope, attribution via `TryLoopExecutionEntityID`, `StopLoop:false`; add the
      `AgentLesson*` entity-ID/message-type twins beside `ops_diagnosis_entity.go`
- [x] 3.2 Writer gates with instructive rejections: ≥1 well-formed 6-part evidence entity ID
      (shape at emit; existence checks live at promotion), injection-form byte bound
      (320-byte constant), typed `applies_to` grammar (`id:` ≥3 segments / `tag:`), per-loop
      emission cap (default 20); table-driven unit tests for every accept/reject path,
      idempotent re-emit, derived attribution
- [x] 3.3 Register in `executors/register.go` (RegisterBuiltins, NATSClient-gated, beside
      emit_diagnosis); tool schema asks intent only; registry enumeration test confirms no
      dedicated lesson search/list/query tool
- [x] 3.4 Integration-tier test driving `emit_lesson` through the production tool wire
      (`tool.execute` → `tool.result`, not helper-direct), including the idempotency path
      against a real NATS container

> Section 3 DONE 2026-07-19. Content-derived identity = `uuid.NewSHA1(lessonNamespaceUUID,
> category+sorted(applies_to)+summary+sorted(evidence))`; first-write-wins dedup
> (`EntityExists`→nil). `AgentLessonEntityID`/`AgentLessonMessageType` twins (mutation-only,
> not payload-registered). semstreams-reviewer: CHANGES REQUESTED (1 HIGH), all 3 findings
> fixed: (1) HIGH — `observed_role` was inert in prod; now a framework fact stamped at the
> `dispatchToolCall` seam via `MetadataKeyAgentRole` + `LoopManager.GetRole` (OVERWRITE/DELETE,
> spoof-proof), so "attribution is derived" holds end-to-end; (2) MED — idempotent re-emit now
> reads back the true persisted status + a `created` flag (new `LessonStore` iface), never a
> contradicting hardcoded `proposed`; (3) LOW — `rejectControlBytes` on identity fields makes the
> canonical string injective by construction. Ratified: idempotency typed-origin skip, polarity-
> reject/severity-clamp, required detail. Gates (independently re-verified): `-race` unit + vet
> (+integration tag) + gofmt + revive + `build ./...` all green; ops-wire integration test green
> with Docker. NIT for docs (task 7.x): `agent.lesson.severity` uses `warning` vs diagnosis `warn`.

## 4. Lifecycle lane + brief-assembly injection

- [x] 4.1 Promotion/retirement lane: document + test the `update_with_triples` replace path
      for `lesson.status`/`lesson.superseded_by` (rule `replace_owned` example config +
      product-writer path via the owned-fact writer); promotion validates every cited
      evidence entity exists (refuse and stay `proposed` otherwise)
- [x] 4.2 Deterministic lesson matcher as a reusable package (input: loop scope
      entity-IDs/tags; output: bounded ordered active lessons): segment-boundary id-prefix +
      tag matching, severity → stored emit-timestamp → entity-ID ordering, K ceiling (≤25,
      default 10) + total-byte budget, proposed/retired/superseded exclusion,
      matched-vs-included counts (designed for reuse by the future `want:[lessons]` facet)
- [x] 4.3 Brief-assembly injection step in `processor/agentic-loop` prompt construction:
      render matcher output (injection forms + entity IDs + counts) into the system prompt at
      dispatch; unit tests per spec scenarios — proposed excluded, bounded+observable,
      replay-stable ordering (emit-time triples, not UpdatedAt/KV revision)
- [x] 4.4 Observability: rejection counter on `emit_lesson` (reason label:
      evidence/bound/grammar/cap) and an injection counter (matched/included) on the
      brief-assembly step

> Section 4 DONE 2026-07-19. Amendment: 13th predicate `agent.lesson.created-at` (immutable
> birth triple, NOT in identity or replace-owned set) — the replay-stable ordering key, because
> triple `Timestamp`/entity `UpdatedAt`/KV-revision are all re-stamped on promotion. Matcher =
> pure `processor/agentic-loop/lessonmatch` (segment-boundary id-prefix + tag; severity→created-at→ID
> order; K≤25/byte-budget bounds; active-only exclusion). Injection via `LessonReader` (nil-safe,
> fail-open) into `assembleSystemPrompt`; tag-by-role v1 scope. Promotion = `LessonCurator`
> (Promote/Retire/Supersede) — evidence-existence gate uses `found && !IsStub()` (bare citation
> auto-stubs, so plain existence is a no-op); single-valued replace via `graph.MergeTriples`.
> `lessonRecordProjectionContract()` (3 mutable predicates only) wired in BOTH cmd binaries.
> Reference rulepack `configs/rules/lessons/` (honest birth-time suppression example, LessonCurator
> as primary path). semstreams-reviewer: APPROVE, no BLOCKING/HIGH; 3 MEDIUM + 2 NIT all fixed
> (page-cap Warn, injection_form control-byte hygiene, reference-config reframe, slices.Contains,
> range-over-int). Consolidated gates green: build/vet/-race/gofmt/openspec-validate/integration
> (Docker). Ratified: reader-returns-all+matcher-filters, tag-by-role v1, !IsStub() existence.

## 5. First consumer: ops flow + e2e

- [x] 5.1 Add `emit_lesson` to the ops allowlist in `configs/flows/ops-agent.json` +
      `-test.json`; extend the ops persona with the emit contract (evidence, bound, typed
      scope, cap) in decision-criteria phrasing
- [x] 5.2 Extend `test/e2e/scenarios/ops` to the full gated round-trip, hard-fail at every
      step: ops loop emits lesson → `proposed` entity queryable via `/graph/triples` →
      promotion write flips it `active` → a subsequent loop's brief contains the injection
      form; run the ops e2e tier green

> Section 5 DONE 2026-07-19. `task e2e:ops` GREEN — all 9 stages incl. `inject-and-verify-lesson`
> (loop-1 emits → `proposed` via /graph/triples → `LessonCurator.Promote` [evidence gate on a
> seeded entity] → `active` → loop-2 [scope tag:ops] brief renders the injection form; the mock's
> entry-4 response is GATED on the injection_form string as marker, so no injection ⇒ no proof
> finding ⇒ hard-fail). PROD `ops-agent.json` gained ONLY `emit_lesson` (max_tokens untouched);
> test config bumped mock context window 4096→200000 (test-only). Injection confirmed live:
> `lesson_injection_total{matched=1,included=1}`. Two root causes fixed (diagnosed vs real
> container state): (1) mock `max_tokens` resolves as CONTEXT WINDOW → 5-call emit loop tripped
> 60% compaction and lost the persona marker (test-config fix only); (2) `submit_work` is
> allowlisted but NOT advertised to ops loops → preset redesigned around natural completion.
> FOLLOW-UP for §8.3 reviewer/architect: allowlisted-≠-advertised `submit_work` may be a latent
> gap (ops loops rely on terminal-tool-less completion) — outside this slice.

## 6. Retire processor/agentic-memory

- [x] 6.1 Pre-delete sweep (corrected enumeration): package imports (expect zero),
      `processor/agentic-loop/component.go:1575` context-event publish leg (prune with this
      change), `agentic-loop/doc.go` lines 316-321/415, `configs/flows/deep-research.json`
      dead block, `config/rules/agentic-memory/` rulepack (coincidental name — ADR-017
      extraction rules, out of scope, leave untouched), component tables in `AGENTS.md` +
      `CLAUDE.md`, `docs/basics/07-agentic-quickstart.md`,
      `docs/concepts/13-agentic-systems.md` (§"agentic-memory Integration"),
      `processor/agentic-loop/README.md` (§"agentic-memory Integration"), `docs/ROADMAP.md`,
      ADR-043 references
- [x] 6.2 Delete the package; prune the publish leg and every stale reference from 6.1;
      update ADR-027's status paragraph to name `emit_lesson` beside `emit_diagnosis`
- [x] 6.3 Framework-change branch integration sweep: `go test -race -tags=integration ./...`

> Section 6 DONE 2026-07-19. `processor/agentic-memory/` (19 files) DELETED — verified orphan
> (0 importers, 0 cmd registration). `go build ./...` exit 0, ZERO dangling .go refs.
> **IMPORTANT — the ContextEvents publish leg was KEPT, not pruned:** the disposition assumed it
> was dead, but `publishContextEvent` (`agentic-loop/component.go:1590` → `agent.context.compaction.*`)
> has a LIVE consumer — the OTel span collector (`output/otel/span_collector.go:232`,
> unit-tested). Pruning would silently kill OTel compaction-span enrichment. Only the stale
> comment (naming the deleted pkg) was rewritten to name the OTel consumer; the `emitContextMetrics`
> range (~1610, Prometheus) is independent and survives. component.go:208 (ADR-080 lesson reader)
> untouched. Pruned: doc.go/README/deep-research.json/AGENTS.md/CLAUDE.md/quickstart/concepts-13/
> ROADMAP. UPDATED: ADR-027 status para names emit_lesson beside emit_diagnosis. LEAVE list
> untouched (coincidental `config/rules/agentic-memory/` = ADR-017 expression rules, confirmed).
> Gates: build/vet/lint/`task test`/gofmt/schema-no-drift all green. 6.3 sweep: 129 ok, 2 FAIL —
> both PRE-EXISTING host-saturation substrate flakes (pkg/ownership NATS-container-start-timeout;
> gated-dag timing Eventually), classified DOCUMENT: pass isolated (cached-green, re-confirmed),
> causally independent (`go list -deps`: neither imports agentic-memory/agentic-loop), host was
> loaded by the semsource stack. RE-CONFIRM the full sweep on a quiet host at §8.2 preflight.
> Minor observations flagged (not acted): decision-patterns.json notes still names deleted
> llm_extractor; agentic-superpowers.md:27 (Draft, 2026-04-06) marks agentic-memory "Exists" — left.

## 7. User-facing docs

- [x] 7.1 New concepts page `docs/concepts/32-agent-memory.md` (26 was taken): push-vs-pull principle (why
      pull memory tools failed; ADR-080), the three layers mapped to framework surfaces, the
      symptom→layer→provider decision matrix (incl. semsource as the reference
      semantic-content producer for source-grounded agents), the lesson lifecycle
      (proposed→active→retired) and "lessons carry policy, not facts" (facts live in the
      graph; lessons cite them)
- [x] 7.2 Cross-link from `docs/concepts/13-agentic-systems.md` and ADR-027/028; example
      category taxonomies documented here (docs, never framework enums)

> Section 7 DONE 2026-07-19. **Filename: `docs/concepts/32-agent-memory.md`** — `26-` was already
> taken (`26-typed-artifact-entities.md`; 27–31 also occupied), used next free number. Page: push-
> not-pull (Mermaid rejected-pull vs shipped-push), three-layer table (episodic/semantic/procedural →
> framework surfaces + 24h/7d retention cliff), symptom→layer→provider matrix (incl. semsource
> content-producer row w/ OGC/coordinate example + Java/Gradle-AST caveat), lifecycle stateDiagram
> (proposed→active→retired/superseded), gates + UUIDv5 identity + `!IsStub()` promotion + matcher
> bounds, "policy not facts" (PROV-O), open-category examples labeled docs-only. Cross-links added to
> 13-agentic-systems.md + ADR-027 + ADR-028. Every claim grep-verified vs code by the writer; I
> re-read + confirmed accuracy (incl. honest "worker-role exclusion = convention, not enforced
> invariant" framing). schema no-drift; build green; all markdown link targets exist.

## 8. Handoff + ship gates

- [x] 8.1 File the semteams upstream issue: adopt the lesson primitive (load/fix the dormant
      ops pack incl. the stale `reviewer-qa` trigger, own chain-terminal trigger rules and
      promotion policy), referencing this change and ADR-080
      (DONE 2026-07-19 — filed C360Studio/semteams#245; names the ops-pack revival, the
      stale reviewer-qa trigger [ADR-041], promotion policy ownership, and the
      submit_work allowlisted-≠-advertised gap)
- [x] 8.2 `/preflight` — lint (revive clean), `-race` unit + integration, schema no-drift,
      contract tests; classify any failure fix-now / file-with-Skip / document
      (DONE 2026-07-19 — lint exit 0, unit `-race` 130 ok/0 FAIL, contract ok, schema no-drift,
      gofmt clean, `task e2e:ops` GREEN, integration `-race -tags=integration -p 1 ./...`
      **131 ok/0 FAIL**. The only failures — 2 in the concurrent full sweep (pkg/ownership
      NATS-container-start-timeout, gated-dag timing) — classified DOCUMENT: pre-existing
      host-saturation substrate flakes, pass isolated + serialized, causally independent
      [`go list -deps`], vanished at `-p 1`.)
- [x] 8.3 semstreams-reviewer pre-merge review; fix findings (DONE 2026-07-19 — verdict
      **APPROVE, ship-ready**; no BLOCKING/HIGH. Ratified: OTel context-event leg correctly KEPT
      [live wired+tested consumer]; test-only max_tokens bump masks nothing [injection form lives
      in never-evicted RegionSystemPrompt]; whole-change coherence verified across every seam;
      not-breaking confirmed. 1 LOW fixed: `emit_lesson` mislabeled "terminal tool" [it's
      StopLoop:false] reworded to "emission/distillation tool" in 32-agent-memory.md + ADR-027 +
      ADR-028. `submit_work` allowlisted-≠-advertised = pre-existing benign wart [not a registered
      executor; ops loops end on natural terminal-tool-less completion] → deferred to §8.1 handoff.
      NITs [stale llm_extractor note in coincidental rulepack; 2 draft proposal docs] left per
      LEAVE-list scope.)
- [x] 8.4 PR (complete-system scope: vocab + executor + lifecycle lane + injection +
      consumer + removal); merge; verify `openspec list` before any completion claim
      (DONE 2026-07-19 — PR #580 squash-merged to main as 338e847e on green CI [Test 12m +
      lint/build/schema]; branch deleted. Follow-up example: PR #581. openspec list verified.)

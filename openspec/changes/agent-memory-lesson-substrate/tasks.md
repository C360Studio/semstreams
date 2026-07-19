# Tasks: agent-memory-lesson-substrate

## 1. Gate: adversarial review of ADR-080

- [ ] 1.1 Run the 5-lens adversarial review against this change (ADR-079 precedent); record
      verdicts in `adversarial-review.md` in this change directory
- [ ] 1.2 Resolve/widen scope per surviving findings; flip ADR-080 Status to Accepted (or
      revise Decision sections and re-run the broken lens)

## 2. Vocabulary

- [ ] 2.1 Add PROV-O constants (`ProvWasDerivedFrom`, `ProvWasGeneratedBy`,
      `ProvWasAttributedTo`) to `vocabulary/standards.go`
- [ ] 2.2 Define the `lesson.*` predicate family in `vocabulary/agentic/predicates.go`
      (enums: category/polarity/severity/status; opaque text: summary/detail/injection_form;
      refs: evidence/applies_to; lifecycle: retired_at/superseded_by; optional confidence;
      observed_role) with doc comments carrying when-to-use guidance
- [ ] 2.3 Register the family in `vocabulary/agentic/register.go` — rule-opaque flags on
      authored text, `StandardIRI` on `lesson.evidence` — and extend the registry tests
      (rule-visibility split + StandardIRI assertions per spec scenarios)

## 3. emit_lesson executor

- [ ] 3.1 Implement `processor/agentic-tools/emit_lesson.go` mirroring `emit_diagnosis.go`:
      `create_with_triples` mint of `{org}.{platform}.ops.lesson.record.{uuid}`, envelope,
      attribution via `TryLoopExecutionEntityID`, `StopLoop: false`
- [ ] 3.2 Writer gates: reject zero-evidence calls and over-bound injection forms (320-byte
      default constant) with instructive errors; table-driven unit tests for accept/reject
      paths and derived attribution
- [ ] 3.3 Register in `executors/register.go` (RegisterBuiltins, NATSClient-gated, beside
      emit_diagnosis); tool schema asks intent only (no identity/structure params); confirm
      registry enumeration test shows no lesson search/list/query tool

## 4. Fusion lessons facet

- [ ] 4.1 Implement `want:[lessons]` in `pkg/fusion`: deterministic `lesson.applies_to`
      scope matching (entity-ID prefix + tag), severity/recency/ID ordering, K-bound
      (default 10, request-declarable), matched-vs-returned counts, retired/superseded
      exclusion, absent-not-fabricated
- [ ] 4.2 Unit tests per spec scenarios: identical-inputs determinism, absent when
      undeclared, absent when unmatched, observable truncation, retirement exclusion,
      entity-ID provenance on entries
- [ ] 4.3 `task schema:generate` — commit projection-schema changes; no-drift gate clean

## 5. First consumer: ops flow + e2e

- [ ] 5.1 Add `emit_lesson` to the ops role allowlist in `configs/flows/ops-agent.json`
      (+ `-test.json`); extend the ops persona/prompt contract with the evidence and
      injection-form bounds (instructive, decision-criteria phrasing)
- [ ] 5.2 Extend `test/e2e/scenarios/ops` to a full round-trip: ops loop emits lesson →
      entity queryable → `want:[lessons]` facet returns its injection form; run
      `task e2e` ops tier green

## 6. Retire processor/agentic-memory

- [ ] 6.1 Pre-delete sweep: grep `agentic-memory`/package imports across repo and all
      `cmd/` binaries; confirm only the `agentic-loop/doc.go` comment and the dead
      `configs/flows/deep-research.json` block reference it
- [ ] 6.2 Delete the package, prune the doc comment and the dead config block; update any
      docs/concepts inventory mentioning the component
- [ ] 6.3 Framework-change branch integration sweep: `go test -race -tags=integration ./...`

## 7. Ship gates

- [ ] 7.1 `/preflight` — lint (revive clean), `-race` unit + integration, schema no-drift,
      contract tests; classify any failure fix-now / file-with-Skip / document (no
      "clean except" hand-waves)
- [ ] 7.2 semstreams-reviewer pre-merge review; fix findings
- [ ] 7.3 PR (complete-system scope: vocab + executor + facet + consumer + removal);
      merge; verify `openspec list` shows this change ready to archive before any
      completion claim

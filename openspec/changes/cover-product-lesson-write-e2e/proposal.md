# Change: Cover the product-shaped lesson writer end to end

Status: independent-product-path correction accepted by the owner on 2026-08-22 after independent SemStreams
CORRECTION DESIGN PASS.

> **Superseded plan:** The ops-stage extension below is retained as accepted history but MUST NOT be implemented.
> The active correction is the independent `lessons` scenario/task described in
> `docs/proposals/gh1030-product-lesson-write-e2e-design-correction.md`, SHA-256
> `299c1adfa94a13af551fc34729f2374a5707fc88d6baf70f2345497ef0f1b8ff`, and the correction sections below.

## Why

SemStreams proves lesson birth end to end only through the configured model/tool lane:

`emit_lesson → LessonStore → proposed → Promote → active → later brief injection`

The first observed product adopter instead constructs lesson identity and triples itself and calls
`LessonStore.CreateLesson` directly. SemStreams has no assembled-runtime proof for that exported direct-store path,
even though downstream semdev exercises it in production composition.

The accepted inventory is
`docs/proposals/gh1030-product-lesson-write-e2e-inventory.md` at SHA-256
`dccdb50e8e2875ce1cec2b0e2b3f44c15a451a4a6454626e6daeaf2ef1c2c634`.

The accepted design is
`docs/proposals/gh1030-product-lesson-write-e2e-design.md` at SHA-256
`e55324ae80fbf2d43f62535d7867bf6190c054a7498c9320fa64eb8dae313a36`.

## What changes

- Extend the existing ops E2E scenario from nine to twelve ordered, fail-closed stages.
- Reuse the existing scenario-owned `NATSValidationClient` connection and construct `NewNATSLessonStore` over
  `s.nats.Client()`; add no second NATS connection or owner.
- Direct-create one fully valid product-authored lesson through `LessonStore.CreateLesson`.
- Authoritatively compare the complete caller-supplied message type and happy-path triple set, including provenance,
  timestamps, and confidence.
- Promote through the existing `e2e.control.lesson.promote` contract unchanged.
- Repeat the exact original create after promotion and prove convergence without overwriting active status.
- Prove the direct-product injection form reaches a later assembled loop brief through a distinct mock marker.
- Clean exact scenario-tracked lesson IDs through generic authoritative read plus revision-fenced delete; use no direct
  lesson KV cleanup.
- Correct the agent-memory guide's E2E task name and the ops task's stale mock narrative.
- Record all focused, race, integration, lint, schema, strict OpenSpec, and assembled E2E verification evidence.

## Non-goals

- No bespoke framework agent, role, persona, persona fragment, prompt contract, or framework agent type.
- No automatic completion-to-ops trigger repair.
- No ops observability or reporting change.
- No new create adapter, request subject, handler, payload, bucket, stream, store, or graph authority.
- No raw KV or raw subject fallback for the new lesson path.
- No #979 semantic-hardening, identity, validation, malformed-input, or exported-constant ruling.
- No #818 generic immutable-birth or graph-ingest policy change.
- No change to #1029's local-client, narrow-curator, public snapshot, or retired-factory rulings.
- No capability spec delta or ADR.
- No CI/nightly/e2e-ladder expansion.
- No sister-repository write or downstream-adoption claim.

## Corrected change

- Add a standalone `lessons` E2E scenario and `task e2e:lessons`.
- Reuse the existing production-target core compose stack and protocol-flow configuration unchanged.
- Compose the existing direct lesson store, local canonical-contract projection client, narrow curator, lesson
  reader, and deterministic matcher over one scenario-owned NATS client.
- Prove valid product-authored birth, proposed exclusion, evidence-gated promotion, active matcher inclusion,
  identical recreate convergence, authoritative full-tuple preservation including datatype, and exact-ID cleanup.
- Add no agent, ops role, mock LLM, user message, prompt, persona, diagnosis, reportable condition, request handler,
  flow config, compose file, durable primitive, capability-spec delta, ADR, or CI/nightly wiring.

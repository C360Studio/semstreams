# Change: Cover the product-shaped lesson writer end to end

Status: corrected independent-product-path design accepted by the owner, independently reviewed, and implemented in
PR #1038.

## Why

SemStreams proved lesson birth end to end only through the configured model/tool lane:

`emit_lesson → LessonStore → proposed → Promote → active → later brief injection`

The first observed product adopter instead constructs lesson identity and triples itself and calls
`LessonStore.CreateLesson` directly. SemStreams had no assembled-runtime proof for that exported direct-store path.

The accepted inventory is `docs/proposals/gh1030-product-lesson-write-e2e-inventory.md` at SHA-256
`dccdb50e8e2875ce1cec2b0e2b3f44c15a451a4a6454626e6daeaf2ef1c2c634`.

The corrected accepted design is `docs/proposals/gh1030-product-lesson-write-e2e-design-correction.md` at SHA-256
`299c1adfa94a13af551fc34729f2374a5707fc88d6baf70f2345497ef0f1b8ff`. It supersedes the earlier ops-extension
design retained in `docs/proposals/gh1030-product-lesson-write-e2e-design.md`.

## What Changes

- Add a standalone `lessons` E2E scenario and `task e2e:lessons` over the unchanged production-target core stack.
- Compose the existing direct lesson store, local canonical-contract projection client, narrow curator, lesson reader,
  and deterministic matcher over one scenario-owned NATS client.
- Prove valid product-authored birth, proposed exclusion, evidence-gated promotion, active matcher inclusion, identical
  recreate convergence, authoritative full-tuple preservation including datatype, and exact-ID cleanup.
- Correct the agent-memory guide to name the direct product birth/lifecycle/reader-matcher gate.

## Non-goals

- No bespoke framework agent, role, persona, persona fragment, prompt contract, or framework agent type.
- No agent loop, ops scenario, mock LLM, user message, diagnosis, reportable condition, request handler, flow config,
  compose file, durable primitive, capability-spec delta, ADR, or CI/nightly wiring.
- No new create adapter, request subject, handler, payload, bucket, stream, store, or graph authority.
- No #979 semantic-hardening, identity, validation, malformed-input, or exported-constant ruling.
- No #1029 downstream-adoption claim or sister-repository write.

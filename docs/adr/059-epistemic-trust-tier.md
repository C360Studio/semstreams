# ADR-059: Epistemic Trust Tier — Claim/Evidence Lifecycle for LLM-Derived Assertions

## Status

**Accepted (scope/consolidation) — v1 commitment 2026-06-22 (Coby).** The v1-gate below is
**resolved YES**: v1 commits to the epistemic trust tier — the keystone #216 (claim/evidence
model) and the #214 extraction gate at minimum, with #213/#217/#215 sequenced per the dependency
order. This ADR **names the responsibility and consolidates the scope**; it does **not** decide
the mechanism. The entity model, status machine, promotion rules, index, conflict payloads, and
retrieval contract are deferred to an **implementing ADR**, which carries the full
adversarial-design-review gate (per the repo discipline: adversarial multi-lens review of
framework ADRs before Accept). The substrate-overlap claims in this scope ADR were verified
against code on 2026-06-22 (auto-vivify removed; `pkg/projection.Contract` wired at boot; ADR-057
still a stub; agentic-governance is content-safety only). The one pre-v1 honesty fix in §"The live
defect carve-out" ships independently and immediately.

This ADR **consolidates and will close** five issues filed 2026-06-03 — #216 (keystone), #214,
#217, #215, #213 — all seeded by [MemGraphRAG](https://github.com/XMUDeepLIT/MemGraphRAG). The
issues predate three substrate layers (ADR-055/056, ADR-057, agentic-governance) that landed
between filing and now; the most valuable part of this ADR is §"Relationship to the substrate,"
which explains how the trust tier rides those existing seams instead of reinventing a from-scratch
governance system. MemGraphRAG is cited throughout as a **warning/reminder** about the shape of the
problem, never as an implementation model.

## Context: the undernamed responsibility

The responsibility this ADR names is the **epistemic trust tier of an assertion** — the property
that distinguishes *an LLM guessed this* from *this is ground truth*. It is NOT "claim entities."
"Claim entities" is one candidate mechanism; the responsibility is the trust tier those entities
would carry. Naming the mechanism instead of the responsibility is exactly the trap
[ADR-056](056-authoritative-semantic-state.md) was written to escape (name the responsibility, push
the mechanism down into the implementing layer).

A `message.Triple` carries `Source`, `Timestamp`, and `Confidence`
(`message/triple.go:48-67`) but has **no status, no lifecycle, and no claim concept** — there is no
field that says "this assertion is proposed, not yet supported, possibly contradicted, or promoted
to canonical." `message/triple.go` defines `Triple`, `TripleGenerator`, `IsRelationship`,
`IsValidEntityID`, and `IsExpired`; grep confirms no `Status`, `Claim`, or `Evidence` type exists
there. The consequence: an LLM-extracted triple and a direct-telemetry triple look identical to
every downstream reader and to merge logic. They have **different trust semantics** but **one
representation**.

The trust tier matters most where assertions are *guessed*: LLM fact extraction from agent memory,
research synthesis, and any future "the model inferred this relationship" path. Direct telemetry,
operator-confirmed entries, and deterministic processor output do not need it — their trust tier is
"ground truth" by construction.

### The acute risk this was filed against has already been closed

When #214/#216 were filed (2026-06-03), the acute risk was: an LLM extractor could silently mint
canonical graph entities and facts via auto-vivifying `triple.add`. That risk is **gone.** The
ADR-055/056 must-exist flip (2026-06-19) removed both auto-vivify-create branches
(`AddTriple` `processor/graph-ingest/component.go`, `AddTriples` ditto); a triple targeting an
absent entity is now **rejected** with `entity_not_found` instead of birthing a metadata-less
entity (`docs/adr/055-graph-write-intent-taxonomy.md:33-46`). An LLM extractor can no longer
silently conjure a canonical entity. This **lowers urgency** and **narrows scope** — see
§"Relationship to the substrate." This ADR is therefore a thin epistemic layer on governed writes,
not the from-scratch governance system #216 originally sketched.

## The five consolidated issues and their dependency order

| # | Title | Role | Depends on |
|---|---|---|---|
| **#216** | claim/evidence entities for LLM-derived assertions | **KEYSTONE** | — |
| #214 | resolve the LLM extraction contract before canonical writes | gates extraction onto #216 | #216 |
| #217 | schema-pattern index for claim/fact shapes | indexes over #216 (index, not source of truth) | #216 |
| #215 | conflict-finding workflow for competing claims | consumes #216 + #217 (detection-only) | #216, #217 |
| #213 | audited retrieval mode | consumes #216 | #216 |

```text
            #216  (claim/evidence entity model — the trust-tier shape)
           /  |  \  \
      #214  #217 #213 \
        |     |        \
   extraction index    #215 (conflict finding, needs #216 + #217)
   gate              (detection-only, never auto-rewrites facts)
```

- **#216 (keystone).** A first-class `claim` entity with a status lifecycle
  (`proposed → supported → contradicted → promoted → rejected → expired`), a confidence, and
  extractor metadata; `evidence` as a typed artifact (source span, content hash, ObjectStore ref);
  `claim → evidence`, `claim → subject`, `claim → generating-activity` relations; and **promotion
  rules** that turn a claim into a canonical triple. Nothing else can exist without the trust-tier
  shape this defines.
- **#214 (extraction gate).** Resolves the contract for LLM fact extraction so extracted output
  routes through the claim surface, never directly to canonical `add_triples`. The live defect this
  must also fix is carved out below.
- **#217 (schema-pattern index).** A `(subject_type, predicate, object_type)` frequency/support
  index that routes conflict checks without full-graph scans. **An index over state, not a source
  of truth** — it derives from claims/facts, it does not author them.
- **#215 (conflict finding).** A coordinator reads claim/evidence refs and emits structured
  `conflict.finding` entities in classes `mutual`, `temporal`, `granularity`, `source-disagreement`,
  `unresolved`. **Detection-only — it never auto-rewrites a canonical fact.**
- **#213 (audited retrieval).** A retrieval mode that returns ranked facts/claims **with** evidence
  refs, status, provenance, and confidence, with on-demand ObjectStore fetch for bulky evidence
  bodies (refs in rule payloads, never content — per the repo's "rules carry references, not
  payloads" rule).

## Relationship to ADR-055/056, ADR-057, and agentic-governance

The trust tier is now **much narrower** than #216 scoped, because three substrate layers landed
after the issues were filed. Each claim below was verified against code on 2026-06-22.

### 1. ADR-055/056 (authoritative semantic state) — write AUTHORIZATION

ADR-055/056 establish *who is ALLOWED to write which predicate group, and that entity birth
requires a semantic envelope.* The must-exist flip (2026-06-19) removed auto-vivify
(`docs/adr/055-graph-write-intent-taxonomy.md:33-46`); ownership is predicate-group-granular via the
`OWNER_CLAIMS` registry (`docs/adr/056-authoritative-semantic-state.md` Decision 1, 2); and claims
over predicate groups **derive from a registered graph projection contract**, not a hand-maintained
list (`docs/adr/056-...md` Decision 6).

**Implication for the trust tier:**

- An LLM extractor can no longer silently mint canonical entities — the acute risk is **already
  closed** by the substrate, not by this ADR.
- `claim` and `evidence` entities should live in their **own predicate group(s) under their own
  owner** (e.g. an extraction/claim owner), declared via a projection contract per Decision 6.
- **"Promotion" of a claim to canonical fact is then a governed cross-group write** — the claim
  owner writes the claim group; promotion is an authorized write into the *canonical* group by a
  promoter that holds (or is delegated) that group's claim. The trust tier does not need its own
  authorization model; it reuses ADR-056's. This is the single largest scope reduction versus #216.

ADR-059 does not modify the four-lane taxonomy or `pkg/ownership`. A `claim` is born through a
normal envelope-bearing lane (Fact arrival or Entity create); its triples are append-evidence or
replace-owned within the claim's own group; promotion is a separate authorized write.

### 2. ADR-057 (cryptographic provenance) — AUTHENTICITY seam (still a stub)

ADR-057 is a **scope-only stub** — verified: Status "Proposed (scope-only)"
(`docs/adr/057-cryptographic-provenance.md:5`) and "Decision: None yet" (`:73`). It reserves the
seam for proving a write *actually originated* from the owner it claims (signed envelopes,
`key_id`/`signature`/`algorithm`/`signed_at`).

**Implication for the trust tier:** a claim's `claim → generating-activity` provenance (back to the
loop/run/tool/result that produced it) is an *epistemic* property — "what produced this guess." When
and if a *cryptographic* authenticity requirement materializes, the claim's provenance can **ride
the ADR-057 envelope seam** rather than reinventing signing. The two are adjacent, not identical:
ADR-057 answers "did the claimed owner author these bytes," ADR-059 answers "how much do we trust
what the assertion says." ADR-059 does not design signing and does not block on ADR-057.

### 3. agentic-governance — CONTENT safety (orthogonal axis)

`processor/agentic-governance/` enforces **content** policy on the agent message path — PII
redaction, prompt-injection/jailbreak detection, content moderation, and rate limiting
(`processor/agentic-governance/doc.go`, "Filters"). It intercepts `agent.task.*`/`agent.request.*`/
`agent.response.*` and emits `*.validated.*` / `governance.violation.*`.

**Implication for the trust tier:** "garbage in / malicious in" is already filtered upstream on a
**different axis.** Governance asks "is this content safe/allowed"; the trust tier asks "how much do
we trust what this (already-safe) assertion claims." The trust tier **never touches** content
safety, never re-implements PII/injection filtering, and assumes governed content as its input.

### Net

The trust tier is a **thin epistemic layer** sitting on top of (1) governed, envelope-bearing,
ownership-checked writes, (2) a reserved authenticity seam, and (3) upstream content safety. It is
not the from-scratch claim-governance system #216 first imagined. It adds exactly one thing the
substrate does not: a *status/trust-tier representation* for an assertion plus the lifecycle and
retrieval/conflict tooling around it.

## The live defect carve-out (#214) — the one pre-v1 deliverable

`configs/flows/deep-research.json:165` sets `extraction.llm_assisted.enabled = true`, but the wiring
makes that a **silent no-op.** Verified chain:

1. `processor/agentic-memory/component.go:76` constructs the extractor as
   `NewLLMExtractor(config.Extraction, nil)` — a **nil** `LLMClient`. No later injection exists;
   grep of `Start`/`Initialize` finds no client assignment.
2. `processor/agentic-memory/llm_extractor.go:53` only calls the client `if e.llmClient != nil`;
   otherwise `:66-67` returns `[]message.Triple{}` (empty, no error).
3. `processor/agentic-memory/handlers.go:133` calls `ExtractFacts`; `:144` short-circuits on
   `len(triples) == 0` and never reaches the `add_triples` publish at `:148`
   (`processor/agentic-memory/publisher.go:30` confirms the operation is `add_triples`).

So a **shipped reference config advertises a live feature that produces zero triples.** This is a
trust/honesty defect independent of the larger trust-tier design.

**Contract decision (#214): option 1 — keep extraction dormant/experimental.** When/if activated,
LLM-derived facts route through the claim surface defined here, **never** direct canonical
`add_triples`. This is consistent with the substrate: even if a client were wired, an LLM extractor
writing canonical facts would now be a predicate-group ownership violation unless it holds the
canonical group's claim — which it must not.

**The one pre-v1 deliverable** (the only thing in this ADR that is not deferred): make
`enabled=true` with a nil client a **loud startup error** (fail-fast at component construction or
config validation) **or** correct the reference config and docs so they do not advertise a silent
no-op. A shipped reference config must never claim a feature is live when the wiring guarantees zero
output. Everything else in ADR-059 stays Proposed/deferred.

## Decision

1. **Name the responsibility.** The epistemic trust tier of an assertion is a first-class framework
   concern, distinct from write authorization (ADR-056), write authenticity (ADR-057), and content
   safety (agentic-governance). It is **reserved here, not designed.**
2. **Defer the mechanism to v1.x**, gated on the v1-gate decision below. The implementing ADR owns
   the entity model, status machine, promotion rules, index shape, conflict payloads, and retrieval
   contract.
3. **Bind to the substrate, do not reinvent it.** Claims live in their own owned predicate group(s)
   via an ADR-056 Decision-6 projection contract; promotion is a governed cross-group write;
   provenance may ride the ADR-057 envelope seam if/when authenticity is required; content safety
   stays in agentic-governance.
4. **Resolve #214 now (option 1)** and ship the one pre-v1 honesty fix above.
5. **Consolidate and close** #213, #214, #215, #216, #217 into this ADR; the implementing ADR (when
   commissioned) carries the design across the dependency-ordered sketch.

## The v1-gate decision

> **This layer activates iff v1 promises auditable LLM-extracted memory OR evidence-cited agent
> answers. Otherwise it stays Proposed/deferred.**

**Resolved 2026-06-22 (Coby): YES — v1 commits to the epistemic trust tier.** The keystone #216
(claim/evidence model) and #214 (extraction routed to the claim surface, no canonical bypass) are
pre-v1. #213 (audited retrieval) is pre-v1 to the extent v1 promises evidence-cited answers. #217
(index) and #215 (conflict finding) remain post-v1 unless v1 depends on automated contradiction
detection. The implementing ADR makes the precise per-piece cut and carries the design-review gate.

- If v1 makes **graph-backed LLM-extracted memory** a user-facing feature, a minimal #216 + #214
  boundary (claim entity + extraction routed to it, no canonical bypass) becomes pre-v1.
- If v1 promises **evidence-cited / audited agent answers** (#213's "which claim, which evidence
  span, what status"), #216 + #213 become pre-v1.
- #217 (index) and #215 (conflict finding) are post-v1 unless v1 specifically depends on automated
  contradiction detection.
- If v1 promises **neither**, the entire layer stays deferred and only the #214 honesty fix ships.

The gate is deliberately tied to a *user-facing v1 promise*, not to internal appetite, so the layer
is built when a consumer needs auditable epistemic provenance and not as speculative substrate.

## Design sketch (dependency-ordered, for the implementing ADR — NOT decided here)

This sketch records *intent* and *boundaries*. The implementing ADR makes the mechanism choices.

- **#216 — claim/evidence shape.** `claim` is an envelope-bearing entity in an extraction/claim
  owner's predicate group, carrying status, confidence, extractor metadata (model/prompt/input
  hashes, extractor version), and the asserted subject/predicate/object. `evidence` is a **typed
  artifact entity** (`docs/concepts/26-typed-artifact-entities.md`) — source span + content hash +
  ObjectStore `StorageRef` via `ContentStorable` (`message/content_storable.go`), keeping bulky
  source text out of triples. Relations: `claim → evidence` (support), `claim → subject`
  (about/asserts), `claim → generating-activity` (PROV-O). Status lifecycle:
  `proposed → supported → contradicted → promoted → rejected → expired`. **Promotion** = an
  authorized cross-group write into the canonical predicate group (ADR-056), not a status flip
  in place.
- **#214 — extraction gate.** Extraction emits `proposed` claims, never canonical triples. The
  honesty fix (above) ships regardless of whether the full gate is built.
- **#217 — schema-pattern index.** Derive `(subject_type, predicate, object_type)` patterns; track
  frequency/support, linked claim/fact IDs, example evidence refs, first/last seen, source
  distribution. KV-backed index over graph state — **never the source of truth.** Routes #215's
  conflict checks without full-graph scans.
- **#215 — conflict finding.** A rule chain identifies candidates (by subject/object, predicate,
  type, embedding similarity, or #217 pattern accumulation); a coordinator reads claim/evidence refs
  on demand and emits structured `conflict.finding` entities in classes `mutual`/`temporal`/
  `granularity`/`source-disagreement`/`unresolved`. **Detection-only**; canonical facts are never
  rewritten by detection alone (human/agent review promotes, rejects, or leaves unresolved). This
  honors ADR-028: rules trigger, the coordinator does the reasoning, findings are structured triples
  a later rule can match deterministically.
- **#213 — audited retrieval.** A query mode returning ranked facts/claims **with** evidence refs
  (ObjectStore pointers or bounded previews), source/activity/model provenance, status, and
  confidence, layered on existing `graph-query`/`graph-embedding`/ObjectStore fetchers — not a new
  RAG engine. Callers fetch long evidence bodies by reference, never embedded in rule payloads.

## What is deliberately NOT decided here

- The concrete entity/vocabulary predicate set for `claim` and `evidence`, and which PROV-O /
  CCO / BFO alignment predicates to use on export.
- The exact status-machine transitions and which transitions require human vs agent vs rule
  authority, and the promotion rule's precise authorization shape.
- The schema-pattern derivation function, the index's KV storage layout, and its update behavior.
- The `conflict.finding` payload schema and the candidate-generation rules.
- The audited-retrieval request/response contract and its GraphRAG/PathRAG routing integration.
- Whether claims ever auto-promote, or promotion is always review-gated.
- Whether cryptographic provenance (ADR-057) is required for any trust-tier transition — left open
  until an authenticity requirement materializes.
- Confidence calibration / scoring semantics beyond the existing `Triple.Confidence` field.

## Consequences

- **Positive.** The responsibility is named before a consumer forces an ad-hoc implementation; the
  scope is correctly narrowed against the post-2026-06-19 substrate (a thin epistemic layer, not a
  governance system); the #214 honesty defect is queued for a pre-v1 fix; five issues collapse to
  one design seam with an explicit activation gate.
- **Negative / risk.** Deferring the full design means a v1 promise of auditable extracted memory
  would put the whole #216 keystone on a critical path with little lead time — the v1-gate decision
  must be made deliberately and early, not discovered late. The single pre-v1 fix (#214 honesty)
  must not be deferred along with the rest.
- **Neutral.** No code changes are committed by this ADR except the #214 honesty fix, which is small
  and substrate-independent.

## References

- [MemGraphRAG](https://github.com/XMUDeepLIT/MemGraphRAG) — cited as a *warning/reminder* about
  the schema/fact/passage boundary and LLM conflict classification, NOT an implementation model.
- [ADR-055: Write-Intent Taxonomy](055-graph-write-intent-taxonomy.md) — entity birth requires an
  envelope; the must-exist flip closed auto-vivify (`:33-46`).
- [ADR-056: Authoritative Semantic State](056-authoritative-semantic-state.md) — predicate-group
  ownership; Decision 6 (claims derive from projection contracts) is where the claim owner declares
  its group.
- [ADR-057: Cryptographic Provenance](057-cryptographic-provenance.md) — the scope-only authenticity
  seam a claim's provenance can ride later.
- [ADR-028: Orchestration Architecture](028-orchestration-architecture.md) — rules trigger,
  coordinators reason; the #215 conflict-finding pattern.
- `processor/agentic-governance/doc.go` — content-safety scope (orthogonal axis).
- `message/triple.go:48-67` — `Triple` has Source/Timestamp/Confidence but **no** status/claim
  concept (the gap this ADR names).
- `processor/agentic-memory/component.go:76`, `llm_extractor.go:53,66-67`, `handlers.go:133,144,148`,
  `publisher.go:30`, `configs/flows/deep-research.json:165` — the #214 silent-no-op defect.
- `docs/concepts/26-typed-artifact-entities.md`, `message/content_storable.go` — the evidence-artifact
  substrate.

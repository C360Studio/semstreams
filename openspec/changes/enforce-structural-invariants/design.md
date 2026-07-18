## Context

The graph's two dotted-token namespaces are foundational and load-bearing:

- **Entity IDs** — 6-part `org.platform.domain.system.type.instance`. Validated by
  `message.IsValidEntityID` (`message/triple.go:138`): exactly 6 parts, each non-empty, charset
  `[a-zA-Z0-9_-]` (note: **no `.`**), explicitly "safe as a NATS KV key." Wired into ~10 sites
  (graph-index INCOMING/CONTEXT/NAME indexes, graph-query, rule `entity_substitution`/`evaluator`).
- **Predicates** — 3-part `domain.category.property` (`docs/basics/04-vocabulary.md`). "Validated" by
  `vocabulary.IsValidPredicate` (`vocabulary/predicates.go:458`): counts dots `== 2` only — no
  empty-segment or charset check — and has **zero callers** (dead code).

Consumers assume the structure holds: `test/e2e/client/nats.go` reads a predicate's `parts[1]`/`parts[2]`
to parse an inference band (`inferred.semantic.high` → band `high`); the gh#474 reverse-index composite
keys embed the predicate as a whole hex-encoded token; the vocabulary is built for `domain.category.*`
wildcard queries (`docs/basics/04-vocabulary.md`). Entity IDs are positionally split too (`parseEntityID`
`parts[2]` = domain; `pkg/types/entity_id.go`). Nothing at any write boundary rejects a non-conforming
token, so a malformed predicate (LLM-authored, sister-repo product, rule-stamped) enters silently and
corrupts these deterministic splits.

Every first-party token conforms today (all vocabulary constants, parsers, and reference configs are
3-part / 6-part), so this is a **latent** foundational gap — an unenforced invariant the rest of the
system already relies on. Maintainer decision (2026-07-14): this is a blocker; 6-part IDs and 3-part
predicates are non-negotiable because they must be deterministic for KV keys and prefix/wildcard/domain
semantics.

## Goals / Non-Goals

**Goals:**
- One authoritative, rigorous validator per namespace (predicate + entity ID), same standard as today's
  `IsValidEntityID`.
- Fail-closed enforcement at the single write boundary (graph-ingest), with a loud, source-naming log.
- No silent flip: a dry-run audit + reject metric proves the corpus is clean (or surfaces violators to
  fix) before enforcement rejects live writes.
- Kill the 2-part-predicate fixture pattern and lint against its return.

**Non-Goals:**
- Predicate *semantics* / ontology / mandatory registration (structure only; Product Boundary).
- Renaming or migrating conforming tokens (no churn to the ~all-conforming corpus).
- A second in-agent predicate gate (agent-authored predicates are caught at the same ingest boundary).

## Decisions

### D1 — One rigorous validator per namespace, mirrored on `IsValidEntityID`

Harden `vocabulary.IsValidPredicate` to the same rigor as `IsValidEntityID`: exactly 3 dot-separated
parts, each part non-empty, each part a safe token. Expose it (and the entity-ID validator) as the single
source of truth both the ingest gate and the lint call.

**Charset nuance (predicate vs entity ID).** Entity IDs are used **raw** as KV keys, so `IsValidEntityID`
forbids `.` and exotic chars. Predicates are **hex-encoded** before they enter KV keys (gh#474
`graph.EncodePredicateToken`), so KV-safety is already handled downstream — the predicate validator
enforces *structure* (3 non-empty parts, no interior dots beyond the 2 separators) and rejects clearly
broken segments, but does not need the full raw-KV-safe charset. The structural guarantee (3 parts) is
what `domain.category.*` wildcard-ability and the positional band-parse (`parts[1]`/`parts[2]`) actually
depend on.

*Alternative considered:* enforce only at `vocabulary.Register`. Rejected — registration covers only
framework-declared predicates; rule-stamped, product, and LLM-authored predicates bypass it entirely.
The invariant must hold for *every* token that reaches the graph, so the gate belongs at the write
boundary, not the declaration site.

**Reality found during apply — the entity-ID gate already exists; this change adds the PREDICATE arm.**
graph-ingest already validates entity IDs at the mutation boundary via `validateEntityID`
(`entityIDRegex`, exactly-6) at every entity handler, and already records rejections via
`recordMutationRejection` (a `mutation_rejections{subject,reason}` counter + WARN log). So the missing
piece is **predicate** structural validation on triple writes, wired through the *existing*
`recordMutationRejection` mechanism — not a greenfield gate. `IsValidPredicate` was still dead and is now
wired in (D1).

**Entity-ID validator unification is out of scope (filed separately).** Three definitions of the 6-part
contract now exist and *diverge* — `message.IsValidEntityID`, `pkg/types.EntityID.IsValid`, and
graph-ingest's `entityIDRegex` (stricter: each segment must start alphanumeric). Collapsing them to one
authoritative validator is real D1 debt, but it changes entity-ID accept/reject behavior on a *working*
gate, so it ships as its own cleanup issue rather than riding this predicate-focused change.

### D2 — Enforce at graph-ingest, fail-closed, log loudly

graph-ingest is the sole writer to `ENTITY_STATES` (ADR-055) and the choke point every product mutation
flows through. The mutation handler validates each entity ID and each triple predicate before persistence;
on any violation it **rejects the whole mutation** with a classified validation error
(`ErrorInvalid` / a `structural_invalid` code) and emits a **loud WARN/ERROR** naming the offending
token, its kind (entity-id vs predicate), the source (rule/caller/subject), and the reason. Nothing
structurally invalid is persisted.

*Alternative considered:* quarantine the bad triple, accept the rest. Rejected by the maintainer — a
mutation carrying a malformed token is a defect at its source; accepting a partial write hides it and
lets the bad pattern persist. Fail-closed + loud log forces the fix upstream.

### D3 — Entity IDs are exactly 6 parts; fix `parseEntityID` drift

The contract is **exactly 6 parts, no dotted segments** — already what `IsValidEntityID` enforces.
`parseEntityID` (`graph/clustering/summarizer.go`) drifted to `len(parts) >= 6` with
`strings.Join(parts[5:], ".")` (a dotted instance). That leniency is removed so the invariant has one
definition. A `>= 6`-part ID is now invalid, not a 6-part ID with a dotted instance.

### D4 — Dry-run audit gates the fail-closed flip (cost-ledger)

Before enforcement rejects live writes: run the validators in **observe-only** mode over (a) the
reference-config + vocabulary corpus (a test/lint pass) and (b) live ingest, incrementing
`graph_ingest_structural_rejects_total{kind,reason}` **without** rejecting. A non-zero count is a violator
to fix/backfill first. Only once the audit is clean does the gate flip to fail-closed. This mirrors the
strict-mode-flip discipline (dry-run + SKIP metric + clean gate before enforcing).

**Reality found during apply — post-rebase (2026-07-18).** While this change was in flight, main
independently landed a STRONGER predicate contract (`vocabulary/predicate_contract.go` `ParsePredicate`:
canonical lower-kebab, per-segment charset + byte bounds) and wired it into the **authoritative
persistence seam**: `graph.MarshalEntityState` / `ValidateEntityStateContract` — called by every
`ENTITY_STATES` write path — now rejects any non-canonical predicate, and the mutation preflights
(`validateMutationEntityState` / `validateMutationPredicates`, `prepareFactProjection` on the Graphable
lane) reject it before the handler-level gate on the create_with_triples / update_with_triples / ingest
lanes. Consequences reconciled into this change:

- `IsValidPredicate` is now a thin delegate to `ParsePredicate` (still the single boolean surface the
  gate + lint call) — strictly stronger than the "3 non-empty parts" floor this change specified.
- The handler-level gate (`validateTriplePredicates`) is the FIRST predicate authority only on
  `triple.add` / `triple.add_batch` (specific `structural_invalid` code + metric, no KV I/O spent); on
  the other lanes it is a defense-in-depth backstop behind the seam, whose rejection classifies as the
  generic `invalid_request` + `predicate_contract_rejections{lane,reason}`.
- The planned observe-only **default** phase was overtaken: enforcement shipped fail-closed (the audit
  in tasks 3.x ran as a static corpus pass, not a live dry-run). D4's "still persisted" dry-run
  semantics are therefore unattainable in the current tree.
- **Escape hatch REMOVED pre-release (2026-07-18).** An observe-only config escape hatch
  (`allow_nonconforming_predicates`) was prototyped to downgrade the handler-level gate to
  meter-and-log. Implementation proved it inert: the authoritative seam rejects unconditionally, so
  the hatch could never permit persistence — it only swapped the caller-visible error code
  (`invalid_request` instead of `structural_invalid`) while the rejection metric fired in both modes.
  Since it had never been released, it was removed rather than shipped as dead operator surface: the
  gate is unconditionally fail-closed and no bypass configuration exists (see the graph-ingest spec
  delta's fail-closed requirement).
- **Not a BREAKING change vs beta.149.** Reviewer-verified: no write that beta.149 accepted is newly
  rejected by this change — the fail-closed enforcement (the authoritative seam) shipped upstream in
  beta.149. The true caller-visible surface here is (a) the triple.add / triple.add_batch error-code
  string (`structural_invalid` where the seam alone would classify `invalid_request`), (b) the
  single-metric surface (`mutation_rejections{reason="structural_invalid"}`, metered exactly once by
  the shared wrapper), and (c) the clustering `parseEntityID` display fix (D3). The e2e tier in task
  6.2 runs as prudence, not as a BREAKING mandate; the semteams research-predicate lockstep attaches
  to its beta.149 adoption, not this PR.

### D5 — Retire the 2-part fixture debt + lint it

Migrate test fixtures using 2-part predicates (`agent.role`, `battery.level`, `source.value`, …) to
conforming 3-part predicates, and extend `test/reference_configs_test.go` with a predicate-structure lint
that fails on any non-3-part predicate reference in fixtures or reference configs — so the bad pattern
cannot return.

## Risks / Trade-offs

- **[A hidden non-conforming producer rejects at flip time]** → D4's dry-run audit + metric surfaces it
  before the flip; the loud log names the source so it is fixable, not silent.
- **[Predicate charset too strict rejects a legitimate token]** → the predicate validator enforces
  structure, not raw-KV-safety (hex-encoding handles the latter), so unusual-but-3-part predicates still
  pass; only genuinely malformed (wrong part count, empty segment) tokens reject.
- **[`parseEntityID` tightening breaks a real dotted-instance ID]** → the audit checks entity IDs too;
  maintainer confirms no dotted-instance IDs are intended.
- **[Fixture migration churns many test files]** → mechanical, covered by the new lint; done once.

## Migration Plan

1. Land the hardened validators (D1) + the entity-ID drift fix (D3) — pure additions/tightenings, no
   enforcement yet.
2. Add the observe-only reject metric at the ingest gate (D4) — dry-run.
3. Run the audit (reference configs + vocabulary lint + a live/e2e dry-run pass); fix any violator.
4. Flip the gate to fail-closed + loud log (D2) once the audit is clean.
5. Migrate fixtures + enable the lint (D5).

**Rollback:** the fail-closed flip (step 4) is the only behavior-visible step; revert it to observe-only
if an unforeseen conforming-token rejection appears, fix the validator, re-flip.

## Open Questions

- Exact classified error code/name for a structural rejection (`ErrorCodeStructuralInvalid`?) — settle
  during spec.
- Whether the predicate validator's per-segment charset should match `isEntityIDChar` exactly or be
  looser (given hex-encoding) — settle during D1 implementation; default to structural-only.

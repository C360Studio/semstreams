# ADR-033: Polar-Based Observability — Drift Detection Primitives for Agentic Systems

## Status

**Proposed (2026-05-04).** Awaiting SemTeams review as first consumer.

The conceptual foundation lives in
[docs/concepts/19-observability-as-polars.md](../concepts/19-observability-as-polars.md).
This ADR distills that framing into the structural commitments
SemStreams takes on, the framework/product boundary, and the
ops-agent → coordinator signal contract that the framing doc
deferred (§7.4).

When this ADR is accepted:

- ADR-027 (ops-agent meta-harness) is **refined**, not superseded.
  Phase 1 (read-only diagnosis via `emit_diagnosis`) stays. The
  per-axis Pareto-frontier framing in ADR-027 Phase 3 is reframed
  here as polar-library curves with regime annotations. The seven
  tunable harness axes from ADR-027 §"Tunable harness elements" stay
  valid; this ADR adds the discipline that bounds *how* the ops-agent
  reasons over them.
- ADR-032 (policy/tenancy/cluster) is **composed with**, not in
  competition with. ADR-032 is the IPS layer (gate known-bad). This
  ADR is the IDS layer (detect drift). Both share the OWASP/ASI
  catalog vocabulary.
- ADR-031 (time-trigger primitive / cron rule type) is **the
  scheduler primitive** sentinel runs ride on. No new scheduler
  needed.

## Context

The ops-agent design (ADR-027) and the governance posture SemTeams
must defend (OWASP ASI 2026, Microsoft AGT, EU AI Act, Colorado AI
Act) keep colliding into the same shape of question: **what should
we measure, against what baseline, with what discipline against
drift?**

ADR-027 answered "what we measure" (113+ predicates across 11
categories). The objective-spec README answered "against what
baseline, per flow" (single-scalar primary metric, secondary Pareto
axes, immutable guardrails). What was missing — and what the
framing doc names — is the **frame** in which that per-flow
discipline is the right discipline, scaled to a system of flows
that change underneath you.

Without the frame, the ops-agent risks becoming an
auto-optimiser (the failure mode of every well-intentioned tuner
since the 1960s). With it, the ops-agent does what aerospace
engineers have done for ninety years: **maintains a regime-indexed
library of empirically calibrated curves and knows when those curves
are stale.**

Two parallel observations forced this ADR:

1. **SemSpec already built the prototype.** `semspec/cmd/semspec
   watch --bundle | --live` polls the live system on a timer, runs
   pure detectors (`EmptyStopAfterToolCalls`, `JSONInText`,
   `ThinkingSpiral`, `RapidShallowToolCalls`, `GraphToolFailure`,
   `RepeatToolFailure`), and emits `Diagnosis` records with
   `EvidenceRef` payloads. The detector interface
   (`semspec/pkg/health/detector.go`) is already framework-shaped:
   pure, table-driven, no I/O. This is the primitive layer.
2. **Quality and governance are the same machinery.** Section 3 of
   the framing doc collapses the apparent quality-vs-governance
   split into one observability discipline with traceability axes.
   ADR-032's enforcement gates and the ops-agent's drift detection
   answer different questions over the same vocabulary.

The framing doc carries one explicit deferral (§7.4): the
ops-agent → coordinator signal/action contract was acknowledged but
left for a later ADR. This ADR resolves it.

## Decision

SemStreams commits to **four structural decisions** that scope the
ops-agent and shape the framework/product boundary.

### D1. Polars are the observability frame; single scalars live inside regimes

The objective-spec README's single-scalar discipline is correct
*within a regime*. Across regimes (model upgrade, persona-fragment
edit, capability-surface change), single scalars hide axis
divergence and load Goodhart proxies. Polars — curves indexed by
operating point and tagged with regime — are the discipline above
the per-flow objective spec.

**Polars do not solve the single-measure problem; they make it
visible.** That visibility is what the ops-agent earns its keep on.
This is layered on top of objective specs, not in competition with
them.

### D2. Separation principle — the measurer is structurally distinct from the measured

The ops-agent is a **stateless reader of the predicate stream.**
It proposes sweeps, identifies inflection points, flags drift, emits
structured signals. It does not write into flow configuration. It
cannot rewrite its own scope. Flows do not see the polar library
and do not optimise against it.

This forecloses semdragon's character-XP failure mode (closed-loop
survivorship bias) and matches the structural-validator-not-LLM-
judgment posture from ADR-031's addendum. Any design that lets the
agent that's accumulating evidence also act on it is rejected on
this ground.

### D3. Tuning — when authorised — is a coordinator action under objective-spec bounds

The ops-agent emits signals. The coordinator (ADR-026, ADR-028
Layer 3) acts on them, bounded by per-flow objective-spec
declarations. The opt-in, the gating predicate, and the bounds
are all human-authored and extrinsic.

This preserves D2: the *measurer* never shapes its own bounds or
acts on its own measurements. Action-by-a-different-role on signals
the measurer emits, within bounds something else authored, is
allowed and necessary.

### D4. Governance is a curated axis class — same machinery, different traceability

The OWASP ASI 2026 catalog and Microsoft AGT enforcement set the
governance vocabulary. That vocabulary feeds the same predicate
stream and the same polar library as quality axes. The split exists
for traceability and for conversations with auditors. Internally,
quality axes (Q1–Q5 in the catalog) and governance axes (G1–G10)
share the same Detector / Diagnosis / Polar machinery.

ADR-032 is the IPS (block at the gate). This ADR is the IDS (watch
for drift toward the gate). They compose. The framing doc's §3 is
the durable argument.

## The framework/product boundary

The separation principle (D2) and the framing doc's deliberate
catalog-as-living-document posture force a sharp framework/product
cut. SemStreams ships **opinion-free machinery**. Products ship
**axes, detectors, personas, and bounds.**

| Concern | Lives in | Why |
|---|---|---|
| `Detector` interface, `Diagnosis` shape, `EvidenceRef` shape, `Bundle` shape | SemStreams (framework) | Pure, deterministic, no product knowledge. Working precedent in `semspec/pkg/health`. |
| `Polar`, `RegimeAnnotation`, `SentinelRun` data shapes | SemStreams (framework) | Mechanical observability infrastructure. Storage layout for the polar library. |
| Sentinel-run scheduler | SemStreams (framework, reusing ADR-031 cron rule) | A sentinel run is a cron rule with a fixture-controlled prompt set and a regime-tagged result write-back. No new scheduler. |
| Stationarity-check primitive (variance comparison vs. established curve) | SemStreams (framework) | Mechanical, no LLM judgment. Pure function over `Polar` + new sample. |
| Signal taxonomy as agvocab predicates: `ops.polar_departure.*`, `ops.threshold_crossed.*`, `ops.regime_expired.*` | SemStreams (framework) | Vocabulary, not opinion. Same shape as `coordinator.next_action`. |
| Ops-agent state machine (read predicate stream → mechanical detect → emit signal) | SemStreams (framework) | Generic orchestration loop. Reuses `agentic-loop`. |
| The axis catalog itself (Q1–Q5, G1–G10, future axes) | Product (semteams, semspec) | Curated against this product's failure modes. Different products care about different axes. |
| Detector implementations (`ThinkingSpiral`, `RapidShallowToolCalls`, `EmptyStopAfterToolCalls`, terminal-artifact validators, etc.) | Product | Each detects a failure mode specific to product flows. |
| Ops-agent diagnosis persona (LLM prompt for "why did this polar drift") | Product | Voice, tone, evidence framing — product-shaped. SemTeams ≠ SemSpec here. |
| Objective spec (per-flow declarations) | Product | Per-flow product authoring. |
| Sentinel prompt curation (which N prompts represent the system) | Product | Product-specific calibration set. |

**SemSpec's `pkg/health` is the proof-of-concept.** Its `Detector`,
`Diagnosis`, `EvidenceRef`, and `Bundle` types hoist into SemStreams
as `pkg/observability` (or equivalent — naming is a Phase 1 decision,
not pre-committed here). The detector *implementations* stay
product-side. This is the standard "framework with reference
implementations" pattern from earlier reviewer-cycle feedback.

## The ops-agent → coordinator signal contract (resolves framing §7.4)

The framing doc deferred three pieces. This ADR commits to all
three.

### Signal taxonomy (predicates)

The ops-agent emits three families of typed agvocab predicates:

| Predicate family | Fields | Emitted when |
|---|---|---|
| `ops.polar_departure` | `axis`, `magnitude`, `duration`, `polar_id`, `evidence_refs[]` | A new sample sits off the established polar by more than the configured variance, sustained across N consecutive runs |
| `ops.threshold_crossed` | `metric`, `direction`, `sustained_for`, `threshold_value`, `evidence_refs[]` | A scalar metric tracked alongside polars (cost, latency, fallback rate) crosses a declared threshold |
| `ops.regime_expired` | `polar_id`, `expiration_cause`, `last_validated`, `evidence_refs[]` | A regime-annotated polar's underlying regime invariants change (model upgrade, persona-fragment edit, tool-execution-path change) |

These predicates are **stable and enumerable** — coordinator rules
fire on them via the existing rule-DSL pattern (ADR-028 Layer 2).
Adding a new signal family is an additive ADR; the three above
cover the full §7.4 working list.

`evidence_refs[]` re-uses the `EvidenceRef` shape from
`semspec/pkg/health` (kind, ID, field, value), pointing at the
underlying loop, message, metric sample, or trajectory step that
triggered the signal.

### Adjustment record schema

When the coordinator acts on a signal under objective-spec bounds,
it emits a typed `ops.adjustment` record (per ADR-031's
`emit_*_artifact` pattern):

```
ops.adjustment.id         — adjustment identifier
ops.adjustment.parameter  — what was tuned (matches a tunable axis from the objective spec)
ops.adjustment.from       — prior value
ops.adjustment.to         — new value
ops.adjustment.signal_id  — predicate this responds to
ops.adjustment.bound_id   — objective-spec bound the action falls within
ops.adjustment.timestamp  — when applied
ops.adjustment.regime     — regime annotation snapshot (model, persona versions, etc.)
```

This record is diff-able, audit-friendly, and **becomes the regime
annotation** that §5 of the framing doc requires. Pre-adjustment
data on the affected polar is preserved and tagged with the prior
regime; post-adjustment data starts a new sub-curve. Successful
adjustment cannot mask underlying drift because the regime boundary
is visible in the polar library.

### Rollback

Rollback is **a separate signal**, not an automatic property. If a
post-adjustment sentinel run shows the adjustment made things worse
on the same axis, the ops-agent emits `ops.polar_departure` against
the new sub-curve. The coordinator's rule for that axis decides
whether to roll back, subject to the same objective-spec bounds.
Rollback is symmetric to forward adjustment — same machinery, same
bounds, same audit trail.

This keeps the contract minimal: signals in, structured records out.
The coordinator's policy logic lives where coordinator policy logic
lives (ADR-026, ADR-028 Layer 3), not in the ops-agent.

## Stationarity discipline

Sentinel runs are how the polar library survives non-stationary
underpinnings. This ADR formalises:

- **Curated set, not full corpus.** Single-digit count of prompts
  per flow class, chosen for diagnostic clarity (per framing doc §5).
- **Schedule via cron rule (ADR-031).** Daily for fast-moving axes
  (cost, latency); weekly for slower axes (validator pass rate); on
  trigger for explicit regime shifts.
- **Mechanical comparison.** Stationarity check is a pure function
  over the polar's variance envelope. No LLM judgment.
- **LLM judgment for diagnosis only.** When a sentinel falls off the
  curve, the *why* earns LLM reasoning — but only after the
  mechanical detector flagged.
- **Polars expire; mark them.** Every polar carries a regime
  annotation. The ops-agent invariant: *no polar is consulted past
  its regime boundary without flagging.*
- **Coordinator-driven adjustments are regime annotations.** The
  `ops.adjustment` record above is the regime annotation. Pre- and
  post-adjustment data are tagged distinctly.

The framing doc's §2.4 honest concession applies: sentinel runs
share infrastructure with measured runs (cost prohibits a
structurally separate environment). The mitigation is
fixture-controlled prompts and regime annotations, not a separate
substrate. Worth naming this as a known limitation rather than
hiding it.

## Consequences

### Positive

- **Framework boundary preserved.** No product opinions cross into
  SemStreams. Detector implementations, axes, personas, and bounds
  stay product-side. New adopters bring their own catalog.
- **No new scheduler.** Sentinel runs ride on ADR-031's cron rule
  type. The substrate already exists; the discipline is what's new.
- **ADR-027 Phase 1 stays valid.** `emit_diagnosis` and
  `ops.diagnosis.*` predicates remain. This ADR adds the polar-
  library substrate they live within. No code retracted.
- **OWASP ASI 2026 / Microsoft AGT integration is structural, not
  bolted-on.** Governance is a curated axis class within the same
  machinery, not a parallel subsystem.
- **SemSpec's prototype validates the data shapes.** Hoisting
  `pkg/health` types upstream reduces parallel implementations
  rather than starting fresh.
- **Adjustment auditability.** Every coordinator-driven tuning
  decision lands as a structured record with provenance, regime
  snapshot, and signal lineage.

### Negative

- **The structural validator is upstream of multiple axes.** Q1, Q2,
  G5, and G9 in the catalog all depend on a structural validator
  that has not yet shipped (called out in framing doc §4.1, §4.2).
  Until it ships, those axes are uninstrumentable. This ADR does not
  ship the validator; it depends on it.
- **Catalog is a living document.** The Q/G axis list will grow and
  retire over time. SemStreams must not lock the catalog into
  framework code or schemas. The framework holds the *machinery* to
  add axes; products hold the *catalog*.
- **Sentinel-vs-live conflation.** Sentinel runs share infrastructure
  with measured runs. Aerospace doesn't have this problem; we do.
  Mitigated by fixture-controlled prompts and regime annotations,
  but never fully eliminated within a single deployment.
- **Polar storage cost.** Regime-indexed curves accumulate over time.
  Retention policy (when does an expired polar get archived?) is a
  Phase 1 decision deferred from this ADR.
- **Coordinator's policy surface grows.** Each new signal predicate
  family is one more thing the coordinator's rule set may fire on.
  Mitigated by the three families being stable and enumerable;
  monitored as a complexity budget.

### Neutral

- **Opt-in for products.** Products that don't author objective specs
  for tunable axes get drift detection (the IDS layer) without
  coordinator-driven adjustment (the action layer). The framework
  works at either level of adoption.
- **No new payload-registry types beyond the three signal families
  and one adjustment record.** Reuses existing infrastructure.
- **Reference implementations published, not prescribed.** SemStreams
  may ship one or two universal detectors (e.g., the
  fallback-rate-as-universal-early-warning pattern from §4.3) as
  reference, but products are free to ignore them.

## Alternatives Considered

### A. Single-scalar autoresearcher (Stanford Meta-Harness, Karpathy lineage)

Optimise the harness against one declared scalar, single-axis Pareto
tracking. **Rejected** as the overall posture (§2.2 of framing
doc). Right inside a regime, wrong as the system-level discipline.
The framing doc's polar reframe is the point of departure from this
lineage — it does not optimise; it observes.

This ADR's signal taxonomy is structurally incompatible with a
scalar autoresearcher: `ops.polar_departure` requires a curve; a
single scalar cannot depart from a curve.

### B. Move ops-agent out of SemStreams (sidecar pattern, semspec watcher writ large)

Ship the polar library as a separate `semops` binary that polls live
systems from outside. **Rejected:** every product ends up
reinventing the polar machinery, sentinel scheduler, and
stationarity-check primitive. Worse: framework-level integration
points (predicate stream consumption, regime annotation write-back)
become external API surface, harder to evolve.

The framework/product cut in this ADR addresses the underlying
concern (no product opinions in framework) without paying the cost
of an external boundary.

### C. Bake the OWASP/ASI catalog into framework code

Ship G1–G10 as built-in detector implementations in SemStreams.
**Rejected:** violates D2 separation and the framing doc's §4
"living document" posture. The catalog will grow, retire, and
re-prioritise based on real failure-mode observations. Catalogs
that ship in framework code calcify; the framework should hold the
machinery to add catalog entries, not the entries themselves.

The bridging vocabulary (the OWASP ASI namespace) can ship as
agvocab constants without locking in the detectors that emit them.

### D. Tuning as an automatic property of the ops-agent (closed-loop)

Let the ops-agent both detect drift and apply adjustments directly,
gated by some internal confidence threshold. **Rejected** on D2
grounds. This is exactly the failure mode the separation principle
exists to foreclose. Tuning happens in coordinator land under
objective-spec bounds, or it does not happen.

## Open questions for SemTeams (first consumer)

1. **Which axes from §4 are highest-leverage to instrument first?**
   Working hypothesis: Q1 (substance vs. ceremony) and Q3 (fallback
   rate) — both reuse triples we mostly already have. G2/G3/G4/G8 are
   next because the enforcement already exists and only metric
   emission is missing. Q4 (role efficacy) requires the persona-
   contract change ("cite-what-you-challenge"). **Validate or
   redirect.**

2. **Are SemTeams comfortable owning the detector implementations
   and ops-agent diagnosis persona?** This is the framework/product
   cut in concrete form. If SemTeams pushes back ("we want the
   framework to ship our axis catalog"), the cut moves and the ADR
   needs revision before implementation.

3. **What does the objective-spec authoring surface look like
   today, and what bounds/predicates would you declare?** Sizes the
   schema for the §7.5 framing-doc question (template-vs-expressive).
   Template is safer; expressive is more powerful. ADR-030's
   allowlist machinery is closer to template — worth understanding
   why before deciding.

4. **Do you have a sentinel prompt set, even informally?** If yes:
   we have an empirical data point on curation size. If no: that's
   product work that has to land before sentinels mean anything.

5. **What's your tolerance for the "structural validator must ship
   first" dependency?** Q1 / Q2 / G5 / G9 all wait on it. If you're
   willing to instrument without those four axes initially, framework
   primitives can ship and those axes earn their slots over time.

6. **Stationarity sentinel cadence — daily for cost/latency, weekly
   for validation?** Or different? The cron rule (ADR-031) handles
   any cadence; the question is what's worth the operational cost
   given your tolerance for drift.

## Implementation sequencing

This ADR does not ship code. After SemTeams sign-off and any
revisions from the open-questions discussion, the implementation
phases below are the proposed sequence.

### Phase 0 — review and revise (this ADR)

- SemTeams reviews the ADR, with focus on the framework/product cut
  and the six open questions above.
- Revisions land here.
- ADR moves Status: Proposed → Accepted.
- Memory entries updated to reflect any cut adjustments.

### Phase 1 — framework primitives (gated on Phase 0 + ADR-032 governance gates landing)

- Hoist `Detector`, `Diagnosis`, `EvidenceRef`, `Bundle` types from
  `semspec/pkg/health` into a new SemStreams package
  (`pkg/observability` is the working name; finalised at hoist time).
- Add `Polar`, `RegimeAnnotation`, `SentinelRun` data shapes.
- Wire the three signal predicate families
  (`ops.polar_departure.*`, `ops.threshold_crossed.*`,
  `ops.regime_expired.*`) into the agvocab constants.
- Wire the `ops.adjustment` record schema.
- Sentinel-run scheduler: a thin shim over ADR-031 cron rule that
  also tags results with regime metadata.
- Stationarity-check primitive: pure function over `Polar` + sample.
- Migration guide: how SemSpec converges its watcher onto upstream
  primitives without breaking its existing detectors.

Sequenced **after** ADR-032 governance work resumes. The framing
doc's §4.3 "existing enforcement, missing metric" claim depends on
ADR-032's enforcement gates existing for G2/G3/G4/G8/G9.

### Phase 2 — SemTeams adopts as first consumer

- SemTeams writes detector implementations for Q1, Q3, and the
  G-axes whose enforcement is already in place.
- SemTeams declares objective specs per flow with tunable axes,
  bounds, and gating predicates.
- SemTeams ships the ops-agent diagnosis persona.
- One sentinel prompt set wired up; one sentinel cadence configured
  via cron rule.
- This is the first real test of whether the Phase 1 primitives
  are sufficient. If they're not, this ADR is back open.

### Phase 3 — SemSpec converges and a second adopter validates

- SemSpec's `cmd/semspec watch` retires its parallel implementation
  in favour of the upstream primitives. Its detector
  implementations stay in `semspec/pkg/health` (they're product-
  shaped); only the type definitions hoist.
- A second adopter (TBD) authors their own catalog from scratch. If
  they can do it without touching framework code, the cut held.

### Phase 4 — coordinator-driven tuning (deepest deferral)

- Coordinator gains a rule that fires on `ops.polar_departure` /
  `ops.threshold_crossed` / `ops.regime_expired` and, when an
  objective-spec bound permits, emits `ops.adjustment`.
- Adjustment apply path uses the existing ADR-026 runtime
  composition tools — same approval gates, same audit trail.
- Rollback symmetric: post-adjustment polar departure → coordinator
  rule → reverse adjustment within the same bounds.

Phase 4 can ship incrementally per axis. Not all axes need
coordinator-driven tuning; some stay diagnosis-only.

## Related decisions

- [ADR-027](027-ops-agent-meta-harness.md) — refined by this ADR.
  Phase 1 (`emit_diagnosis`, `ops.diagnosis.*` predicates) stays;
  the polar-library substrate is added underneath.
- [ADR-028](028-orchestration-architecture.md) — names the ops-agent
  as Layer 4. This ADR scopes Layer 4's reasoning discipline.
- [ADR-026](026-coordinator-agent-dynamic-flow-composition.md) — the
  coordinator's runtime composition tooling is what Phase 4
  adjustment apply rides on.
- [ADR-031](031-time-trigger-primitive.md) — cron rule type is the
  scheduler primitive sentinel runs ride on. No new scheduler.
- [ADR-032](032-policy-tenancy-cluster.md) — IPS layer (gate). This
  ADR is the IDS layer (drift detection). They compose; OWASP ASI
  2026 is the shared vocabulary.
- [ADR-030](030-http-middleware-and-identity-pattern.md) — provides
  G3 (identity-abuse) and G9 (human-agent trust) enforcement; this
  ADR adds the metric emission layer.
- [docs/concepts/19-observability-as-polars.md](../concepts/19-observability-as-polars.md)
  — the conceptual foundation. This ADR distills the structural
  commitments; the concept doc holds the full reasoning.
- [docs/objectives/README.md](../objectives/README.md) — per-flow
  objective spec discipline that polars layer on top of, not replace.

## References

- OWASP Agentic AI Top 10 (December 2025) / ASI 2026 taxonomy.
- Microsoft Agent Governance Toolkit (April 2026),
  `github.com/microsoft/agent-governance-toolkit`.
- Stanford Meta-Harness, Lee et al. — single-scalar lineage this
  ADR's polar discipline reframes.
- `semspec/pkg/health/detector.go` — working prototype of the
  detector primitives this ADR proposes hoisting.
- `semspec/cmd/semspec/watch.go`, `watch_live.go` — operator-facing
  bundle/live capture pattern.

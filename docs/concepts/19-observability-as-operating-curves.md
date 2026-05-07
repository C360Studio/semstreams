# Observability as Operating Curves: Framing for the Ops Agent and Governance

**Status:** Concept doc — the conceptual foundation for
[ADR-033](../adr/033-operating-curve-based-observability.md)
(Operating-Curve-Based Observability). Sections 1–3 are framing;
section 4 is the working axis catalog and is expected to grow as
new failure modes are observed.

ADR-033 distills the structural commitments and resolves the §7
ops-agent → coordinator signal contract that this doc deferred.
This concept doc remains the durable home for the full reasoning
behind the operating-curve / coefficient / stationarity / separation
framing.

**Terminology note (2026-05-07):** Earlier drafts of this doc and
ADR-033 used the aerodynamics term *polar* for what we now call
*operating curves*. The aero term is historical (Eiffel ran early
lift/drag experiments in polar coordinates; the name stuck even
after the field switched to Cartesian L/D plots) and confused
readers without aero fluency. *Operating curve* carries the same
discipline — measure these systematically across regimes, treat
them as engineering artifacts — without the historical baggage.
Where this doc needs the original aero term for clarity (drag
polar, lift polar), it uses it explicitly with the aero qualifier.

**Date:** 2026-05-04 (terminology revised 2026-05-07)

## 1. Why this doc exists

The ops-agent design (ADR-027 Phase 1) and the governance posture
SemTeams will need to defend (OWASP Agentic Top 10 / ASI 2026,
Microsoft AGT, EU AI Act, Colorado AI Act) keep colliding into the
same shape of question: **what should we measure, against what
baseline, with what discipline against drift?**

The objective-spec README (`docs/objectives/README.md`) already
points at the right answer for any single flow — name a primary
metric, name secondary Pareto axes, name immutable guardrails, back
all of it with predicates. That discipline scales to one flow.
This doc is about how it scales to a *system of flows that change
underneath you* — different models, different personas, different
adopters with different priorities, an underlying capability surface
that ships new versions on a quarterly cadence.

The thing that's been missing is the *frame* in which the
objective-spec discipline is the right discipline. Without that
frame, the ops-agent risks becoming a tuner — the failure mode of
every well-intentioned auto-optimiser since the 1960s. With it, the
ops-agent is doing what aerospace engineers have been doing for
ninety years: maintaining a regime-indexed library of empirically
calibrated curves, and knowing when those curves have gone stale.

This doc names the frame. It is deliberately written before the
ops-agent is built so that the ops-agent's design can be evaluated
*against* the frame, rather than the frame being reverse-engineered
from whatever the ops-agent ends up doing.

## 2. The framing: coefficients, operating curves, and stationarity

### 2.1 Why this is empirical, not derivational

Aerodynamics has Navier–Stokes — a complete, exact mathematical
description of fluid flow. You can write it on a napkin. It is
almost useless for designing an actual wing, because the equations
are intractable for real geometries at real Reynolds numbers. So
the field runs on empirical coefficients calibrated against
wind-tunnel data: lift, drag, moment, a different family of curves
for every flow regime. The math is the foundation. The coefficients
are what ship.

LLM agentic flow design is in the same place. We have the clean
theory — *give the agent the goal, the tools, the context, let it
reason.* That is our Navier–Stokes. We have already discovered, in
the smoke runs that produced ADR-031 §addendum 2026-05-02, that
asking one role to enumerate completeness *and* find adversarial
weaknesses produces neither, so we split them. That split is a
coefficient. It is not derivable from first principles. It came
from running the wind tunnel.

Other coefficients we've already calibrated and shipped:

- The substance-over-format pivot from R3.4b (smoke #5 converged
  in 6 loops where R3.4a's format-compliance chain spent 22 and
  never terminated).
- The decision to keep architect as curator, not reasoner.
- The structural-validator-not-LLM-judge discipline for
  cross-grounding (ADR-031 §addendum 2026-05-02 final note).
- The refusal to forward-prop entities through the rule engine
  even though the chain occasionally wants to.

None of these fall out of a theory of agency. They fall out of *we
ran it and watched what broke.* This is engineering, not derivation,
and it is fine — every interesting engineering domain ends up here.
The domains that pretend otherwise are the ones that haven't been
pushed to ship at scale yet.

### 2.2 Why a single scalar is not enough

Stanford Meta-Harness, Karpathy's autoresearch lineage, and most
LLM-system optimisation work assume a single scalar to optimise
against. The objective-spec README leans into the same discipline:
*"A single scalar is preferred. If a flow genuinely needs a composite,
state the weights explicitly."*

That discipline is right inside a regime. It is wrong as the
overall posture, because agentic flow quality is genuinely
multi-dimensional and the dimensions trade off non-linearly.
SWE-bench score, cost per task, latency, hallucination rate,
scope-creep rate, integration-faithfulness, structural-validator
pass rate — these are not reducible to a weighted sum without losing
information that matters for downstream decisions. A flow that's 95%
accurate at $0.50 is not strictly worse than one that's 97% accurate
at $5.00. It depends on what the accuracy gap is *made of*. The 2%
might be the 2% you can't tolerate.

The deepest unsolved problem in the space: **the metric you can
measure cheaply isn't the metric that matters, and the metric that
matters can't be measured cheaply.** Single-scalar approaches paper
over this by demanding a number. They are right to demand it —
without one you can't optimise — but the number they get is often a
Goodhart loader for the actual goal.

### 2.3 Operating curves as the reframe

A *polar* in aerodynamics is not a number. It is a curve —
typically lift coefficient plotted against drag coefficient across
angle of attack. The term is historical: Eiffel ran his early
lift/drag experiments at Champ-de-Mars using polar coordinates, and
the name stuck through Lilienthal and into NACA's systematic
airfoil studies even after the field switched to Cartesian L/D
plots. So today's "drag polar" is polar-only-by-history; the
geometry isn't polar in any meaningful sense. The discipline the
term carries — *measure these curves systematically across regimes
and treat them as engineering artifacts* — is what we want. The
historical name itself confuses readers outside aerospace.

We'll call them **operating curves**. Same engineering discipline,
less historical baggage, parses correctly for software and
governance audiences without aero fluency. Where this doc needs
the original aerospace term for clarity (drag polar, lift polar),
it uses it explicitly with the aero qualifier; everywhere else,
operating curves.

The interesting engineering knowledge in any operating curve is
not a single point on it. It is the curve's *shape*: where the
relationship is linear, where it rolls off, where it stalls, how
stall behaves (gentle vs. abrupt), what the optimum trade-off is
and at what operating condition. Different missions select
different operating points on the same curve.

The agentic analog: don't ask *what's the quality of this flow.*
Ask *what does the quality-vs-cost curve look like, and where do
you want to operate on it.* Same flow, same prompts, same personas
— but a sweep across reasoning_effort, model tier, persona-fragment
variants, max_iteration caps produces a *curve*, not a point. The
adopter who wants throughput operates at one point; the adopter who
wants maximum spec fidelity operates at another.

Operating curves don't solve the single-measure problem. They
reframe it. The field still has to commit to *which axes to plot.*
The honest gain is narrower:

> Operating curves push the unsolved part to a place where it's
> visible rather than hidden, and visibility is most of what you
> need to maintain a system over time.

Single-scalar approaches blind you to axis divergence.
Curve-based approaches at least *show you* the loader as it forms.
When two metrics that historically tracked stop tracking, that is
the early signal that one of them has become a Goodhart proxy for
the other. That visibility is what the ops-agent is for.

### 2.4 Stationarity is the meta-axis

There is one disanalogy with aerodynamics that we have to take
seriously. Aerodynamic polars are stationary. The lift curve of a
NACA 0012 airfoil at Re=6M is the same today as it was in 1947.

Agentic system operating curves are not stationary. The curve for
*claude-sonnet-4-6 with this persona on this flow* will be a
different curve when 4-7 ships, when a persona fragment is edited,
when an underlying tool-execution path changes its retry behaviour.
Operating curves have shelf life.

This is the deepest job of the ops-agent, and the place where most
of its long-term value sits: **maintain a living, regime-indexed
operating-curve library that knows when it is stale.** Aerospace
doesn't have to do this because air doesn't change. We do, because
models do.

The mechanism is sentinel runs. A curated, fixed set of prompts
re-executed on schedule against the live system, with their results
compared against the established curve. Stationary regime: results
sit on the curve within variance. Regime shift: a step. Drift: a
slope. The stationarity check itself is mechanical and requires no
LLM judgment. The diagnosis of *why* a curve drifted is where LLM
reasoning earns its keep, but only after the mechanical detector
has flagged that something drifted.

### 2.5 The separation principle

One more piece of structural discipline, learned from a sibling
project (semdragon's character-XP system, which accumulates
evidence of what worked and lets characters update from their own
runs). That model has a known failure mode: **the agent that's
accumulating evidence is also the agent acting on it,** which
creates a closed loop where success bias compounds. Characters that
survive long enough to accumulate XP are by definition characters
whose configurations didn't get them killed early, which is not the
same as configurations that are good.

Aerospace doesn't have this problem because the wind tunnel runs
the airfoil; the airfoil doesn't run itself. The measurement
apparatus is structurally separated from the artifact being
measured.

Same principle goes here, and it is the same principle ADR-031's
addendum has already invoked twice (structural-validator-not-LLM-
judgment; tester-from-builder collapse rejected on the same
grounds): **separate the measurer from the measured.** The
ops-agent should be a stateless reader of the predicate stream. It
proposes sweeps, identifies inflection points, flags stationarity
drift. The agents running the actual flows do not see the
operating-curve library, do not optimise against accumulated XP,
do not "level up." They do the job. A separate observer
infrastructure builds operating curves from outside.

This is also why the objective-spec model is structurally better
than any intrinsic evidence-accumulation model. The spec is
*extrinsic* — human-authored, lives in markdown, cited by the
ops-agent on every diagnosis but doesn't modify the flow's
behaviour. Extrinsicness is what keeps the measurer separated from
the measured.

## 3. Governance is just a curated axis class

The OWASP Agentic Top 10, renamed to ASI01–ASI10 under the 2026
taxonomy, gives us an externally-validated catalog of failure modes
we should have observability for. Microsoft's Agent Governance
Toolkit (AGT, shipped April 2026) provides deterministic policy
enforcement at sub-millisecond p99 covering all ten. That layer is
necessary and sufficient for *gating known-bad actions*. It is not
sufficient for *detecting drift toward bad actions before they
cross the gate.*

This is the classic IDS-vs-IPS distinction from network security.
AGT is largely an IPS — it blocks the action. The ops-agent +
operating-curve library this doc proposes is largely an IDS — it
watches for patterns suggesting a previously-good system is
degrading. They answer different questions:

- AGT: *is this specific action allowed right now?*
- Ops-agent: *is this system's behaviour pattern stable, or is it
  drifting?*

A system with AGT and not the second is well-defended against
known threats and blind to novel drift. A system with the second
and not AGT detects drift but lets bad actions through while it's
diagnosing them. **You want both, and they compose.** AGT is the
policy floor; the operating-curve library is the observability
ceiling. The OWASP/ASI catalog is the bridge — it gives both
layers a shared vocabulary for what "known bad" means.

The structural claim this doc makes: **governance is not a
separate concern; it is a curated set of axes within the existing
observability discipline.** Quality axes (the failure modes we have
directly observed in our own smoke runs) and governance axes (the
externally-curated failure catalog we adopt) feed the same
predicate stream, the same operating-curve library, the same
ops-agent diagnoses. The split exists for traceability and for the
conversation with auditors and adopters. Internally, it is the
same machinery.

## 4. Working axis catalog

This section is *living*. Each axis is anchored to a failure mode
— either one we've observed directly (cited to a smoke run or
code-review pivot) or one we've adopted from an external catalog
(cited to OWASP/ASI). New axes earn their slot when a new failure
mode is observed; existing axes get retired when their failure mode
no longer matters under the current regime.

The catalog is split into *quality axes* and *governance axes* for
traceability. The split is documentary, not structural — both
classes feed the same predicate stream and the same operating-curve
library.

### 4.1 Quality axes

Failure modes observed directly in SemTeams smoke runs and
code-review pivots, with the axis pair that surfaces each one and
the triples required to compute it.

**Q1. Substance vs. ceremony**

- Failure history: R3.4a (22 loops, $8, no terminal artifact)
  vs. R3.4b (6 loops, $1.50, terminal artifact). ADR-031
  §addendum 2026-05-02.
- Axis pair: cumulative cost (or loops) vs. terminal-artifact
  structural-validator pass rate.
- Healthy regime: cost and validation rate track. More cost buys
  more validated artifacts.
- Failure signature: cost climbs, validation rate flat. Curve goes
  horizontal.
- Triples needed: per-loop cost; per-loop completion outcome;
  terminal-artifact validator result keyed to chain ID. The third
  is the structural validator from ADR-031 §addendum 2026-05-02
  final note — *not yet shipped.* Shipping it is partly motivated
  by needing it as a curve y-axis.

**Q2. Self-grounding vs. real grounding**

- Failure history: semteams builder smoke (this morning) — code
  authored against builder-authored mock; "tests pass" satisfied
  without exercising any real integration boundary. The Goodhart
  case study that motivated the test-harness-as-flow conversation.
- Axis pair: tests-passing claims vs. integration-point coverage
  against non-builder-authored fixtures.
- Healthy regime: claims and coverage track.
- Failure signature: tests-passing high, integration-point coverage
  flat or zero.
- Triples needed: per-test-run, what fixture was hit (real
  Mosquitto vs. builder-authored stub), keyed against the spec
  artifact's `integration_points[]`. This is the structural
  validator ADR-032 R3.6.3 will need to ship. Same shape as Q1.

**Q3. Tool-use compliance / fallback rate**

- Failure history: SemSpec's small-LLM tool-use wall — tool-call
  parse rate degrading silently until Semantic Aligned Parsers
  were added with loud triggering. Not directly a SemTeams
  failure, but the pattern transfers.
- Axis pair: tool calls attempted vs. tool calls successfully
  parsed without fallback intervention.
- Healthy regime: parse-clean rate stable near 1.0.
- Failure signature: parse-clean rate falls, fallback-rescue rate
  rises. *The middle state matters more than the failure state* —
  a 5% outright failure rate is obviously broken; a system parsing
  cleanly 80% of the time and fallback-rescuing 20% is silently
  degrading and that is the harder failure to catch.
- Triples needed: per-tool-call, three-state outcome (parsed
  clean / fallback rescued / failed). The three-state is critical;
  collapsing rescued and clean into "succeeded" hides the early
  warning.
- Generalisation: this pattern recurs anywhere a graceful fallback
  exists (retry-on-malformed, reviewer-rejection-retry, approval
  re-prompts). **Instrument fallback rate as a first-class metric
  for every fallback path.**

**Q4. Role efficacy**

- Failure history: not yet observed; this is the axis that will
  settle the challenger-earns-its-keep question raised in the
  early-adopter / BMAD conversation.
- Axis pair: per-role invocations vs. role-induced revisions
  citing concerns the predecessor role didn't catch.
- Healthy regime for an earning-its-keep role: a non-trivial
  fraction of `concerns_raised` (or `insufficient`) outcomes cite
  things the predecessor missed.
- Failure signature for a not-earning-its-keep role: rubber-stamp
  rate near 1.0, or `concerns_raised` rate non-trivial but the
  concerns repeat what the predecessor already flagged.
- Triples needed: per-role-invocation, decide outcome; *plus a
  marker for "did this role surface something its predecessor
  missed."* The second triple requires the role to cite what
  it's challenging — a small persona-contract change that buys a
  major observability axis.
- Applies to: reviewer (does it catch real planner gaps or
  rubber-stamp), challenger (the 2026-05-04 question), planner
  (do its decompositions hold up downstream), architect (does
  curation introduce drift from what the chain produced).

**Q5. Stationarity / regime drift (meta-axis)**

- Failure history: not yet observed in SemTeams; *will* be
  observed when claude-sonnet-4-7 ships, when persona fragments
  are edited at scale, or when underlying capability behaviour
  changes.
- Axis pair: time vs. sentinel-run results on any other axis the
  catalog tracks.
- Healthy regime: flat line within variance.
- Failure signatures: step function (regime shift), gradual slope
  (drift).
- Triples needed: sentinel-run identifier, timestamp, full
  predicate-stream replay capability. This is observability
  *infrastructure* — not metric emission. The flows don't need to
  know they're sentinels; the ops-agent does.
- This is the axis that protects the operating-curve library from itself.
  Without it, the library accumulates evidence that silently mixes
  across regimes.

### 4.2 Governance axes

Adopted from OWASP Agentic Top 10 / ASI 2026 taxonomy. Each is
mapped to the axis pair that surfaces it, the triples needed, and
the relationship to existing SemTeams enforcement (so we know
where we already have the gate but lack the metric).

**G1. Goal hijacking (ASI01) — prompt injection in tool results**

- Axis pair: instructions-from-user acted upon vs. instructions-
  from-tool-results acted upon.
- Failure signature: actions-on-tool-result-instructions rate rises
  above baseline.
- Triples: per-action, instruction provenance (user message vs.
  tool result content), action taken.
- Existing enforcement: critical-injection-defense and
  injection-defense-layer guidance is documented at the persona
  level. Not currently observable as a metric.

**G2. Tool misuse (ASI02)**

- Axis pair: persona-allowed tool calls vs. allowlist denials.
- Failure signature: denial rate rising trend.
- Triples: per-tool-call, persona at call time, allowlist outcome.
- Existing enforcement: `agentic-governance.enable_tool_governance`
  + per-persona allowlists. Already enforces; needs metric
  emission.

**G3. Identity abuse (ASI03)**

- Axis pair: header-verified identity rate vs. body-fallback
  identity rate.
- Failure signature: body-fallback rate rising — identical pattern
  to Q3's fallback-rate-as-early-warning.
- Triples: per-request, identity source.
- Existing enforcement: `xUserIDIdentityMiddleware` chain (cmd/
  semteams/middleware.go) including the `FallsThroughOnAbsent`
  test. Structural signal already emitted; needs metric.

**G4. Supply chain (ASI04)**

- Axis pair: source ingestion from approved namespaces vs.
  ingestion attempts blocked.
- Failure signature: blocked-ingestion rate rising; sources from
  unfamiliar namespaces appearing in the trusted-source set.
- Triples: per-ingest, namespace, outcome.
- Existing enforcement: `add_source` executor's namespace allowlist
  (cmd/semteams/tools/addsource/executor.go) + ADR-030 approval
  gate on side-effecting source mutation. Already enforces;
  needs metric.

**G5. Cascading failures (ASI05)**

- Axis pair: chain depth vs. terminal-artifact validator pass rate.
- Failure signature: long chains with falling validation rate —
  cascading retry without convergence.
- Triples: same as Q1; this is the same machinery, different axis
  pair.
- Existing enforcement: `max_iterations` caps on rules.

**G6. Memory poisoning (ASI06)**

- Axis pair: predicates from trusted sources vs. predicates with
  provenance to ingested external content acted upon downstream.
- Failure signature: cross-grounding rate against untrusted sources
  rising; downstream artifacts citing predicates whose lineage
  traces to recently-ingested external content.
- Triples: per-triple, provenance, source trust tier;
  per-downstream-artifact, set of predicates cited.
- Existing enforcement: namespace-scoped ingestion (G4) is the
  upstream gate. Memory-poisoning observability is downstream of
  that — the gate prevents the worst case; the metric catches
  drift.

**G7. Insecure communication (ASI07)**

- Axis pair: inter-agent messages signed vs. unsigned.
- Failure signature: any unsigned message in a signed-only
  pathway.
- Triples: per-message, signature presence.
- Existing enforcement: *none yet.* SemTeams does not currently
  ship inter-agent message signing. This axis is forward-looking;
  it earns its slot only if/when message signing becomes a
  requirement.

**G8. Code execution (ASI08)**

- Axis pair: sandboxed-bash invocations within default capability
  set vs. invocations with non-default capabilities.
- Failure signature: non-default-capability rate rising.
- Triples: per-bash-execution, capability set, sandbox profile.
- Existing enforcement: ADR-032 R3.6.1's sandbox primitive (in
  design). The capability-restriction enforcement and the
  capability-emission metric should ship together.

**G9. Human-agent trust exploitation (ASI09)**

- Axis pair: approvals granted vs. approval outcomes correlated
  with downstream artifact validity.
- Failure signature: high approval rate, low correlation between
  approved actions and validated downstream artifacts —
  rubber-stamp human-in-the-loop.
- Triples: per-approval, approval outcome; per-chain, terminal
  validator outcome; correlation computed offline.
- Existing enforcement: ADR-030 approval flow. Approval *acts*
  are observable today; correlation with downstream validity
  requires Q1's terminal validator to be shipped.

**G10. Rogue agents (ASI10)**

- Axis pair: loops spawned by user/rule vs. loops with anomalous
  spawn provenance (no rule trigger, no user message, no
  recognised parent).
- Failure signature: any non-zero rate of anomalous spawn
  provenance is a hard alert.
- Triples: per-loop, spawn cause, spawn-chain depth.
- Existing enforcement: rules are the only legitimate non-user
  spawn path; ADR-031's rule-driven persona swaps are the
  authority surface. This axis is the early warning that something
  is spawning outside that surface.

### 4.3 Cross-axis observations

A few patterns worth naming once rather than repeating per-axis:

- **Fallback rate as universal early warning.** Q3 (tool-use SAP),
  G3 (identity body-fallback), and any future "graceful
  degradation" path share a structural pattern: the system keeps
  *working* while a hidden quality dimension degrades, and the
  fallback rate is the only visible signal until something
  actually breaks. Every fallback path SemTeams ships should emit
  fallback-rate as a first-class metric. Small instrumentation
  cost, large diagnostic payoff.

- **Cite-what-you-challenge as a small persona-contract change with
  major observability payoff.** Q4 needs it. Several governance
  axes would benefit. The change is one line in each role's
  `decide` contract: when emitting `concerns_raised` or
  `insufficient`, include the specific predecessor statement being
  challenged. Costs nothing in chain efficiency; gives the
  ops-agent a structural way to compute "did this role surface
  something the predecessor missed."

- **Existing enforcement, missing metric.** G2, G3, G4, G8, G9 all
  represent gates SemTeams already enforces in code. The
  ops-agent's near-term workstream is largely **making existing
  enforcement observable** — emitting predicates from gate
  decisions that already happen — rather than building new
  enforcement. This is also a clean ninety-day shape for a
  governance-engineering role: a defensible, scoped, *additive*
  workstream that doesn't require touching the enforcement
  machinery.

## 5. Stationarity discipline

Sentinel runs are how the operating-curve library survives non-stationary
underpinnings. The discipline:

- **Curated set, not full corpus.** A handful (single-digit) of
  prompts that exercise each major flow class, chosen for
  diagnostic clarity, not for coverage. Sentinels are not test
  suites; they are calibration points.
- **Re-execute on schedule.** Daily for fast-moving axes (cost,
  latency); weekly for slower axes (validator pass rate); on
  trigger for explicit regime shifts (model upgrade, persona
  fragment edit, capability surface change).
- **Compare against established curve.** Mechanical: did the
  result sit on the curve within established variance? This check
  uses no LLM judgment.
- **LLM judgment only for diagnosis.** When a sentinel falls off
  the curve, *why* is the question that earns LLM reasoning. The
  detection is mechanical; the diagnosis is interpretive. Keep
  the boundary clean.
- **Operating curves expire; mark them.** Every curve in the
  library has a regime annotation: which model, which prompt,
  which persona versions, which capability surface. When any of
  those change, the curve is suspect until re-validated. The
  ops-agent's invariant: *no curve is consulted past its regime
  boundary without flagging.*
- **Coordinator-driven adjustments are regime annotations too.**
  When the coordinator acts on an ops-agent signal to tune a
  parameter within objective-spec bounds (§6), the adjustment is
  recorded on the affected curves exactly the same way a model
  upgrade is. Pre-adjustment data is preserved and tagged;
  post-adjustment data starts a new sub-curve. This is what
  prevents successful adjustment from masking underlying drift —
  the symptom may be compensated, but the regime boundary is
  visible in the operating-curve library, and the next sentinel
  pass reads the post-adjustment curve on its own terms.

## 6. What this is not

Worth marking the negative space, because the field has well-known
nearby attractors that this design deliberately rejects:

- **Not a tuner.** The ops-agent does not adjust live parameters.
  It proposes sweeps, identifies inflection points, flags drift,
  emits structured signals (triples, threshold events, regime-
  expiration flags). It does not write into flow configuration. It
  cannot rewrite its own scope.
- **Tuning, when authorised, is a coordinator action.** The
  coordinator is already the role with authority over side-
  effecting actions (ADR-030 approval flow) and the meta-reviewer
  responsibility for cross-flow decisions individual personas
  can't make from inside their loops (ADR-031 §addendum 2026-05-02
  R3.5). Tuning fits that shape. *If* the human has opted in via
  the objective spec, *and* the ops-agent has emitted a signal
  matching a declared gating predicate, *and* the proposed
  adjustment is within declared bounds, the coordinator may act.
  Otherwise it cannot. The opt-in, the predicate, and the bounds
  are all human-authored, extrinsic, mechanical, and per-flow —
  scoped the same way ADR-030's approval allowlists are scoped.
  The ops-agent emits the signal; the coordinator decides whether
  to act on it; the human authored both the bounds and the opt-in.
  This preserves the separation principle (§2.5) — what the
  principle prohibits is the *measurer* shaping its own bounds and
  acting on its own measurements, not action-by-a-different-role
  on signals the measurer emits within bounds something else
  authored.
- **Not an autoresearcher.** The ops-agent does not optimise
  against a single scalar. It maintains operating curves and
  respects the unsolved-ness of single-measure as documented in
  §2.2.
- **Not a tester.** Tests gate; operating curves observe. The
  structural validators that already exist (and the ones ADR-031
  §addendum proposes shipping) are the gating layer. The ops-agent
  reads the predicates the validators emit, but it does not
  replace them.
- **Not a replacement for AGT-style policy enforcement.** §3
  argues the layered position. The operating-curve library complements
  policy enforcement; it does not substitute.
- **Not intrinsic to the flows.** The separation principle (§2.5)
  means flows do not see the operating-curve library and do not optimise
  against it. Any design that violates this is reverting to
  semdragon's XP coupling and inherits its survivorship-bias
  failure mode.

## 7. Open questions

- **Which axes from §4 are highest-leverage to instrument first?**
  Working hypothesis: Q1 (substance vs. ceremony) and Q3 (fallback
  rate) buy the most observability per unit instrumentation cost,
  because both reuse triples we mostly already have or are about
  to need anyway. G2/G3/G4/G8 are next because the enforcement
  already exists and only metric emission is missing. Q4 (role
  efficacy) requires the persona-contract change and pays off
  most in conversations with skeptical adopters.

- **What is the right schedule for sentinel runs?** Daily seems
  cheap for cost/latency; weekly for validation; the model-upgrade
  trigger is what keeps the operating-curve library current. Open: whether
  to run sentinels in a dedicated environment vs. piggybacking on
  the live system. Aerospace runs in a dedicated wind tunnel for a
  reason — measurement environment matters. Equivalent here may
  be a fixture-controlled compose profile.

- **How does this interact with ADR-027 Phase 1's ops-agent
  scope?** The objective-spec README is shaped for per-flow specs.
  This doc proposes a meta-layer above per-flow specs (the
  operating-curve library, regime-indexed). They compose, but the
  relationship needs to be explicit in whatever ADR formalises the
  ops-agent design.

- **What does the ops-agent → coordinator signal/action contract
  look like?** §6 establishes that ops-agent emits signals and
  coordinator (when authorised) acts on them. The contract between
  them needs explicit design before this lands as ADR. Three
  pieces:

  - *Signal taxonomy.* What predicates does ops-agent emit? Working
    list: curve-departure (axis, magnitude, duration), threshold-
    crossed (metric, direction, sustained-for), regime-expired
    (curve identity, expiration cause). These need to be enumerable
    and stable because the coordinator's rules will fire on them.
  - *Adjustment record schema.* Probably a typed payload the
    coordinator emits per ADR-031's `emit_*_artifact` pattern —
    structured record of "what was tuned, from what to what, citing
    which signal, against which objective-spec bound, at which
    timestamp." Diff-able, audit-friendly, machine-readable. The
    same record becomes the regime annotation §5 requires.
  - *Rollback story.* If the coordinator tunes a parameter and the
    next sentinel run shows the adjustment made things worse, what
    happens? Probably auto-rollback within the same bounds, but the
    rollback predicate is itself something the objective spec needs
    to declare. Open: whether rollback is a separate coordinator
    action against a separate signal, or an automatic property of
    the adjustment-with-bounds machinery.

- **What is the right opt-in surface in the objective spec?** The
  per-flow declaration has to enumerate (a) which parameters are
  tunable, (b) the bound on each, (c) the gating predicate that
  permits adjustment, (d) the rollback predicate. This is a
  non-trivial schema. Open: whether to bias the schema toward
  expressive (every flow can declare arbitrary predicates) or
  toward template (flows pick from a small library of pre-vetted
  bound/predicate patterns). Template is safer; expressive is more
  powerful. ADR-030's allowlist machinery is closer to template;
  worth understanding why before deciding.

- **Does the operating-curve library become a topology with
  enough data?** Volume alone doesn't drive this. What would drive
  it is systematic sweep coverage across the *design choices*
  themselves — persona-set variants, role decompositions,
  rule-gating shapes — not just sweeps of continuous knobs
  (reasoning_effort, model tier, max_iterations) within a fixed
  design choice.

  The shape this points at: each design choice is a *node*.
  Inside a node, the continuous knobs trace out a *surface* of
  curves — vary reasoning_effort smoothly, the curve moves
  smoothly. Between nodes, the transitions are *discrete* — you
  either have the challenger role or you don't; there is no
  smooth path between four-role and three-role chains. So the
  right object isn't a single connected manifold; it's a graph
  of design-choice nodes with a surface attached at each node.

  Some §4 observations already gesture at this. Q1 and G5
  sharing machinery is two slices that are *the same slice* —
  same surface, different label. Fallback-rate-as-universal-
  early-warning (§4.3) is a *structural pattern* that recurs
  across multiple surfaces (Q3, G3, future graceful-degradation
  paths) — the same local shape appearing at different nodes of
  the design graph. These are the kinds of regularities a
  topology view makes legible that a flat list of curves doesn't.

  Three sub-questions before this earns its keep:

  - *At what coverage threshold does the graph-of-surfaces
    framing pay off over independent curves?* Probably when we've
    actually run sweeps that vary discrete design choices (not
    just continuous knobs) — and we mostly haven't yet. The
    R3.4a → R3.4b pivot is one such sweep; we need more before
    the topology view is anything other than aspirational.
  - *How does the non-stationarity from §2.4 propagate?* A model
    upgrade doesn't just shift one curve; it potentially deforms
    every surface at every node. The operating-curve library
    going stale becomes "the whole graph going stale."
    Stationarity discipline scales, but the bookkeeping gets
    harder and the sentinel-run schedule may need to be node-
    aware rather than curve-aware.
  - *How do we distinguish measured shape from fitted-and-
    therefore-asserted shape?* Reaching for topology too early
    is its own Goodhart trap — an asserted surface implies
    relationships between points that might not be real, and
    decisions made against the asserted surface optimise against
    something the measurements don't actually support. Same
    discipline as §6's "not an autoresearcher" — the framing
    must stay descriptive of what was measured, not predictive
    of what would happen at points not yet measured.

  Probably premature to commit on any of these before the per-
  curve discipline has shipped and stabilised. *But:* every
  smoke run should already record its full design-point
  coordinates — not just measured outcomes — so future topology
  assembly is possible without re-running. That's a small
  instrumentation discipline that costs nothing now and preserves
  the option. Worth flagging in whatever ADR formalises predicate
  emission.

  The deeper reason to hold this open question even while
  deferring it: the topology framing is what would let governance
  arguments become structural rather than per-axis. *"This region
  of the design space satisfies these governance bounds; this
  region doesn't; the boundary between them has these
  properties"* is a different kind of argument than *"axis G3
  looks healthy."* Auditors and policy reviewers will eventually
  want the structural argument. Knowing that's the destination
  affects what data we collect now in ways that make the
  topology recoverable later.

- **At what point does this become an ADR?** Probably when §4
  has been validated against two or three real failure-mode
  observations rather than the one (R3.4a→b) it currently rests
  on most heavily. Until then, the catalog is a working document
  and the framing is the durable contribution.

## 8. References

- ADR-027 — ops-agent Phase 1 design.
- ADR-028 — agentic primitives, Layer 2 rules-as-mechanical-routing.
- ADR-030 — approval flow and identity (G3, G9 enforcement).
- ADR-031 — research flow and dev-via-spec; §addendum 2026-05-02
  (R3.4b smoke #5 convergence; substance-over-format pivot;
  structural-validator-not-LLM-judgment posture).
- ADR-032 — R3.6 sandbox design (G8 enforcement, builder
  test-harness substrate).
- `docs/objectives/README.md` — per-flow objective spec discipline.
- OWASP Agentic AI Top 10 (December 2025) / ASI 2026 taxonomy.
- Microsoft Agent Governance Toolkit (April 2026), `github.com/
  microsoft/agent-governance-toolkit`.
- Stanford Meta-Harness, Lee et al. — referenced in
  `docs/objectives/README.md`.
- Karpathy autoresearch lineage — single-scalar optimisation
  framing this doc reframes via operating curves.
- Conversational lineage: 2026-05-04 framing conversation
  (aerodynamics analogy, polar/coefficient distinction,
  separation principle from semdragon).

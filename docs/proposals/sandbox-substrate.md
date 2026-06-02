# Sandbox + Testevidence Substrate — Execution and Evidence as Framework Primitives

**Status**: Proposed — 2026-05-31. Pre-ADR. **Updated 2026-05-31** to
split into two co-shipping primitives per Gemini design review (see
§Why two primitives, not one). **Updated 2026-05-31 PM** to fold in
Gemini round 2 refinements: (1) lease mode as `sandbox.LeaseProfile`
property block (`lease.mode`, `lease.audit`, `lease.ttl`), not
separate ephemeral primitive (see §Lease modes); (2) Contract-vs-Run
separation on testevidence (see §EvidenceContract, §EvidenceRun); (3)
assertion-result provenance triples stamped on the EvidenceRun entity,
not the sandbox entity (see §Per-assertion result protocol).
**Updated 2026-05-31 evening** for Gemini rounds 3+4: catalog YAML
reshape with `realization` block + `lease.mode` rename; Renderer
interface with `HandleRef` + `Handle` + `Capabilities` +
`ResultCollector` extension; `LeaseOptions.Owner *lifecycle.Ref`
framework-enforced minimal-audit owner check; `SecretResolver`
abstraction with per-renderer materialization + evidence redaction.
See the canonical design exercise at
[`sandbox-substrate-design-exercise.md`](sandbox-substrate-design-exercise.md)
for the full interface specifications. This is the **last critical
substrate add before considering exit-from-beta** — see §Exit-beta
significance for what that constrains.

**Predecessors**: [`workflow-primitives-decision.md`](workflow-primitives-decision.md) (the rules-vs-workflow-vs-lifecycle layering this proposal slots into), [ADR-049](../adr/049-lifecycle-prime.md) (the schema-over-ENTITY_STATES Lifecycle Manager this proposal probably extends).

**Drives**: ADR-052 (TBD) + possibly ADR-053 — `pkg/sandbox` substrate + `pkg/testevidence` substrate + render-plugin interfaces. One-ADR-bundle vs two is an open cross-cutting question (Q-X1).

**Trigger**: Two sister-product threads independently arrived at the same shape on 2026-05-31 — capability-aware execution environments AND tier-bound evidence ownership — with multiple downstream render targets. Per [[feedback_greenfield_cross_product_break_now]] (with the parked-on-substrate refinement from this turn): act on N=2 arrival when greenfield + we own every consumer.

## Summary

**Two primitives, co-shipping:**

| Primitive | Owns | Renders to (examples) |
|---|---|---|
| `sandbox` (runtime environment) | What env: capability tokens, admission, lifecycle, leases | devcontainer.json, qa.yml/act, docker-compose, k8s job |
| `testevidence` (test evidence ownership) | What evidence: tier, facets, scenario↔env binding, required assertions | Cucumber tags, OpenSpec scenarios, future SemTeams scenario surface |

**The thesis**: lift the structured contracts, not the friendly projections. Cucumber/OpenSpec/qa_level are projections of testevidence; devcontainer.json/qa.yml are projections of sandbox. Lifting projections ships one product's surface as if it were universal — the same failure mode as lifting devcontainer.json as the "env" contract.

**The connective tissue**: `scenario → required_assertions[] → environment_profile_ids[] → sandbox.CapabilityContract`. sandbox is leased BY testevidence (or product code) via the Lifecycle handle; sandbox doesn't know its consumer. testevidence reads assertion outcomes from triples stamped on the leased sandbox entity.

## Why two primitives, not one

The original (single-primitive) proposal bundled `verification:` commands inside the `sandbox.CapabilityContract`. Gemini's review surfaced the structural error: that conflates two distinct framework concerns.

### Conflation, named

- **"What env do I need"** — capability tokens, admission, render targets, lease lifecycle. This is shape A.
- **"What evidence proves the assertion, and where can it be observed"** — tier, facets, scenario↔env binding, required assertions. This is shape B.

Many-to-many relationships expose the bundling as wrong:

- One env can satisfy 30 scenarios' evidence (an integration env that 30 unrelated scenarios verify against).
- One scenario may need multiple envs (a requirement with unit + integration + e2e tiers, each in a different env profile).
- The (scenario, evidence-tier, environment-profile) graph is structurally many-to-many, not 1:1.

The single-primitive design forces 1:1 coupling by stuffing verification commands inside the env contract. That doesn't model the actual shape consumers need.

### The friendly-projection-vs-structured-contract principle

Cucumber tags, qa_level policy, devcontainer.json, qa.yml — these are *projections* over typed contracts, not the contracts themselves. When N products converge on similar surfaces, factor on the underlying typed contract, not on the projection they happen to share.

If we lift the projection, we ship one product's surface as the universal abstraction:

- Lifting Cucumber tags → SemTeams' future scenario surface has to inherit Cucumber's mental model.
- Lifting devcontainer.json → k8s-targeted consumers have to translate into devcontainer's mental model.
- Lifting qa.yml → SemTeams' dev-via-spec has to inherit SemSpec's GitHub Actions assumptions.

If we lift the contract:

- Each product renders the contract to its preferred projection.
- Cross-product reasoning operates on the typed contract, not the projection.
- New projections (k8s, AWS Batch, future surfaces) slot in without redesigning the primitive.

See [[feedback_lift_structured_contract_not_friendly_projection]] for the general principle.

### The agent-cost framing

The strongest reason for the split is downstream of both products' agent surfaces: **agents need to reason about WHERE evidence can be observed BEFORE they spend tokens trying to prove it.**

- SemSpec QA-reviewer asked to verify a requirement should know upfront "this needs e2e tier" so it doesn't burn tokens trying to prove it from a unit-context conversation.
- SemTeams dev-via-spec architect/scenario-generator needs to declare "this requirement → unit + integration evidence in these env profiles" so the sandbox manager prepares the right env BEFORE the dev loop fires.
- Coordinator routing needs the same upfront read: "this chain produces evidence at tier T in env E."

The runtimeenv (sandbox) primitive alone doesn't answer this question — it just provides envs. The testevidence primitive is what lets agents pre-route, pre-scope, and pre-cost their reasoning. Without it, we ship a substrate that products will wrap with redundant tier-binding logic per consumer.

## Why "parked on substrate" IS a current N=2 data point

(Refinement to [[feedback_greenfield_cross_product_break_now]] surfaced in this turn.)

The naive read of N=2 is "two products actively using this today." That undercounts. A consumer that is **parked specifically because the substrate doesn't exist yet** is a stronger N signal than an active consumer:

1. **Named blocker.** SemTeams dev-via-spec is parked. Not "might someday want it." Currently paused while design issues like this one resolve.
2. **We are the blocker.** If we hold the substrate back, SemTeams continues to wait. Shipping the substrate unblocks them; not shipping it perpetuates the block.
3. **Design alignment is the work.** SemTeams already designed around the primitive's existence. Their consumer shape will form to match what we ship — meaning we have to get the shape right NOW, not when they thaw.

The discipline implication: when applying [[feedback_greenfield_cross_product_break_now]]'s "act on N=2," the N=2 count includes parked-on-substrate consumers. Treating them as "anticipated, defer" inverts the framework's responsibility — the framework exists to unblock consumers, not to wait for them to demonstrate need by working around its absence.

For this proposal specifically:

- **N=2 for sandbox**: SemSpec qa.yml + SemTeams Coordinator devcontainer pre-routing (both active).
- **N=2 for testevidence**: SemSpec scenarios (active) + SemTeams dev-via-spec scenario-generator (parked on substrate). The parked consumer is the second data point, not anticipated future demand.

This unblocks shipping both primitives in coordination. The single-primitive shape would have shipped sandbox only and forced SemSpec to grow testevidence-shape locally, which is exactly the parallel-path failure mode the substrate exists to prevent.

## Exit-beta significance

This proposal lands the **last critical framework primitive** before we consider moving out of beta. That changes the discipline calculus in three ways:

1. **No backward-compat shims later.** Post-1.0, breaking changes to substrate primitives become costly. We have one shot to get the shape right; this is the architectural-clarity tax we pay before stability promises.
2. **Bundle, don't sequence.** The previous instinct to ship sandbox first and defer testevidence assumed we could reshape later. Once exit-beta lands, "later" is expensive. Co-ship per [[feedback_pr_complete_system_unit]] discipline.
3. **The friendly-projection-vs-contract decision is permanent.** If we lift the projection now (Cucumber tags as the contract), reversing that post-1.0 means breaking every consumer's projection-layer assumptions. Lifting the typed contract gives projections room to evolve independently — that flexibility is load-bearing for post-1.0 stability.

The corollary: this proposal's Q-X1 (one ADR vs two) should lean toward one ADR-052 covering both primitives as a co-shipping substrate bundle. Splitting into ADR-052 + ADR-053 invites sequencing, which we explicitly don't want.

## Background

### How both threads arrived at the same shape

- **SemSpec**: needs to express "this spec requires Go 1.26 + Node 20 + Playwright + a Postgres + restricted network + these verification commands pass at tier T" in a form the QA-reviewer can adjudicate AND the GH Actions runner can execute. Today's path-of-least-resistance: write `qa.yml` per spec and call it portability. Tomorrow's problem: `qa_level` policy can't introspect the YAML — it's frozen text. Without the testevidence layer, every assertion is bound to its env at write-time; cross-tier reasoning is impossible.
- **SemTeams sandbox side**: Coordinator routing wants to pre-allocate execution environments to agent chains based on what tools they'll need. Today's path-of-least-resistance: devcontainer profiles per role. Tomorrow's problem: devcontainer JSON isn't introspectable by Coordinator policy either, and every new transport target (compose for local debugging, k8s for prod) means duplicating the profile.
- **SemTeams testevidence side (parked on substrate)**: dev-via-spec architect/scenario-generator needs to declare "this requirement needs evidence at unit + integration tier in env profiles X and Y" so the sandbox manager prepares the matching environment, and reviewers stop asking a dev sandbox to prove behavior that only a QA/integration harness can observe. Currently parked because the substrate isn't there.

All three end up writing semi-structured YAML/JSON that some downstream policy layer can't actually reason about. The framework smell is clear: **two missing abstractions** — the capability contract (sandbox) and the evidence contract (testevidence).

### Why not just keep them product-local

Three reasons:

1. **Render target multiplication.** Each product is already eyeing 2-3 render targets per primitive. The combinatorial explosion of `N products × M targets × 2 primitives` is the carve-out-parallel-path failure mode flagged in `CLAUDE.md` Orchestration Boundaries.
2. **Cross-product reasoning.** A SemTeams Coordinator that knows "this chain runs the SemSpec QA verifications at integration tier" needs to read the *same* contracts SemSpec writes. Product-local contracts can't be composed.
3. **N=2 greenfield arrival.** Per [[feedback_greenfield_cross_product_break_now]] (parked-on-substrate refinement), pre-1.0 + we own every consumer + N=2 for both primitives today = act on the second arrival, not the third.

## Proposed framework primitive A — `sandbox` (runtime environment substrate)

### Core contract

```yaml
# CapabilityContract (data shape, framework-owned types)
# Note: no `verification:` field — that belongs to testevidence.
schema_version: "1"
owner: "semspec-team"

requirements:
  tools:
    - {class: tool.go, version: "1.26"}
    - {class: tool.node, version: "20"}
    - {class: tool.task, version: "3"}
    - {class: tool.playwright, version: "1.40"}
  services: [service.nats, service.postgres]
  network: restricted                       # admission policy hint (enum)
  filesystem: workspace-write               # admission policy hint (enum)
  secrets: [openai_api_key]                 # reference name only; value via SecretResolver

# Realization — renderer choice + per-renderer config
realization:
  renderer: docker-compose
  config:
    compose_file: docker/test-stack.yml
    service: app

# Lease mode — determines audit depth and Lifecycle ceremony
# (see §Lease modes for the heuristic table)
lease:
  mode: ephemeral                           # reusable | ephemeral (renamed from `realization` to avoid collision with the realization block)
  audit: minimal                            # full | minimal
  ttl: 30s                                  # lease duration hint
```

### Lifecycle (verifying phase removed)

```
requested → provisioning → ready → leased → released
                                        ↘ expired
                                        ↘ failed
```

`verifying` is GONE from sandbox. sandbox provides envs. Whoever runs verification commands inside the leased env owns the verification lifecycle separately — that's testevidence's job. The sandbox lifecycle ends at "I have a usable env you can lease and tear down."

### What sandbox owns

- Typed `CapabilityContract` payload + capability catalog types (`vocabulary/sandbox/`)
- Lifecycle: requested → provisioning → ready → leased → released state machine
- Readiness attestations (triple-stamped on the sandbox entity)
- NATS subjects + KV state (probably ENTITY_STATES via ADR-049 — see Q-W1)
- Admission policy for risky capabilities (the primitive gates: network egress, FS write scope, secret materialization)
- Renderer/plugin interface (`sandbox.Renderer` — see Q-W2)
- Lease handle protocol: how consumers acquire, hold, and release envs

### What products keep on sandbox

- **SemSpec** keeps `qa.yml` render target as a *renderer plugin* (not a source-of-truth file). The renderer produces the YAML from the CapabilityContract.
- **SemTeams** keeps Coordinator routing policy. Devcontainer becomes a renderer plugin; Coordinator pre-routes by reading the CapabilityContract, not by parsing the JSON.
- Both call the same framework substrate manager: `sandbox.Manager`.

### Lease modes (Gemini round 2 refinement)

Sandbox ships **one** lease protocol with mode-as-property, not a separate `sandbox.Ephemeral` primitive. The mode determines audit depth and Lifecycle ceremony, not the substrate shape — same `sandbox.Handle`, same renderer interface, same admission gates apply regardless. The difference is whether the framework keeps a named entity + Participant phase history (full) or just an opaque lease record with TTL cleanup (minimal).

| Profile | Use when | Lifecycle |
|---|---|---|
| `mode: reusable, audit: full` | Shared, long-lived, expensive, privileged, secret-bearing, or agent-routed env | Full Lifecycle Participant — ENTITY_STATES, named instance, audit history, restart recovery |
| `mode: ephemeral, audit: minimal` | One-shot cheap env (local Postgres for a 30s test, no secrets, no public network) | Minimal lease record — handle-only, no Participant ceremony; visible lifecycle lives on the consumer (e.g., `testevidence.EvidenceRun`) |
| `mode: ephemeral, audit: full` | One-shot but privileged, public-networked, or costly | Full Lifecycle Participant despite short duration — promote per heuristic |

**Default: `mode: reusable, audit: full`.** Minimal-audit ephemeral is the opt-in for cases where audit cost isn't justified. Default per [[feedback_bucket_ownership_rubric]] is graph-visible Participant; the opt-out has to defend itself on the heuristic.

Consumers that need visible lifecycle on an ephemeral env carry that lifecycle themselves — testevidence does this via `EvidenceRun` (see Primitive B). The substrate stays small; specific-scope lifecycle visibility lives on the consumer that has the context to name it.

**Why not a separate `sandbox.Ephemeral` primitive:** that shape would become the discipline-escape-hatch everyone reaches for to avoid lifecycle ceremony. Mode-as-property keeps the discipline in one place and forces the promote-to-full decision per the heuristic, not per developer instinct.

## Proposed framework primitive B — `testevidence` (test evidence ownership)

### Contract vs Run separation (Gemini round 2 refinement)

The first draft conflated stable spec data with execution state — lifecycle `declared → bound → verifying → satisfied/failed/skipped` was attached to the contract. Gemini caught the trap: those are *run* states; the contract is stable obligation data. Same contract may run many times (PR run, retry, nightly, release-candidate); putting satisfied/failed on the contract makes it act like mutable run state, which it isn't.

**Two types, clearly separated:**

- **`EvidenceContract`** — stable spec data. Authored in scenario frontmatter, projected as triples on the scenario entity. Never mutates after authorship.
- **`EvidenceRun`** — Lifecycle Participant. Keyed by `(scenario_id, tier, run_id)`. Owns the visible execution lifecycle. N runs per contract is normal.

This is the same Contract-vs-Run discipline pattern that recurs in other substrate areas (lease records vs lease executions, mission specs vs mission runs, scheduled-job vs job-instance). See [[feedback_separate_contract_from_run]] for the general principle.

### EvidenceContract (stable spec)

```yaml
# EvidenceContract (frontmatter authorship → triples on scenario entity)
scenario_id: scenario.weather.heartbeat
tier: integration                       # vocabulary.testevidence.Tier
facets: [slow, network-required]        # operator-named bag
environment_profile_ids:                # sandbox profile references
  - mavlink.px4-sitl.mavsdk-smoke
required_assertions:                    # framework looks for these in run results
  - HEARTBEAT
  - mavsdk_core_connected
verification:                           # commands to execute in the leased env
  - "mavsdk-smoke --probe heartbeat"
  - "mavsdk-smoke --probe core_connected"
```

No lifecycle on the contract. Authoring projects it to triples on the scenario entity; `testevidence.Manager` reads it when scheduling an `EvidenceRun`. Contract mutation is a spec edit, not a state transition — and tracked via git/spec-history, not the lifecycle layer.

### EvidenceRun (Lifecycle Participant)

Keyed by `(scenario_id, tier, run_id)` so N runs per contract are distinct entities with full audit history.

```
scheduled → bound → verifying → satisfied
                              ↘ failed
                              ↘ skipped
```

Where:
- `scheduled`: run requested; contract resolved; no env leased yet.
- `bound`: sandbox has been leased per `environment_profile_ids[]`; verification commands ready to inject.
- `verifying`: verification commands running in the leased env; per-assertion results streaming back as triples on the EvidenceRun entity.
- `satisfied`: all `required_assertions[]` present in the run's assertion triples.
- `failed`: at least one required assertion missing or contradicted.
- `skipped`: tier policy excluded this run (e.g., qa_level=draft skips e2e).

### Per-assertion result protocol (Q-T2 — Gemini round 2 refinement)

Each assertion stamps triples on the **EvidenceRun entity** (not the sandbox entity) with full provenance fanout:

```
evidence_run:scenario.weather.heartbeat.integration.run-abc123
  evidence_run.assertion.HEARTBEAT.outcome           satisfied
  evidence_run.assertion.HEARTBEAT.observed_at       2026-05-31T15:42:18Z
  evidence_run.assertion.HEARTBEAT.sandbox_lease_id  lease-xyz789
  evidence_run.assertion.HEARTBEAT.evidence          {stdout: "...", parsed: {...}}
  evidence_run.scenario_id                           scenario.weather.heartbeat
  evidence_run.tier                                  integration
  evidence_run.environment_profile_id                mavlink.px4-sitl.mavsdk-smoke
  evidence_run.sandbox_lease_id                      lease-xyz789
```

**Why not stamp on the sandbox entity:** one env can satisfy 30 scenarios' assertions concurrently, and assertion names will collide (`HEARTBEAT` from scenario A vs scenario B both writing `sandbox.assertion.HEARTBEAT.outcome` would overwrite). Stamping on EvidenceRun avoids collision and preserves the multi-scenario reality where one env produces assertion outcomes for N runs.

**Optional ops mirror:** summary triples may mirror on the sandbox entity (e.g., `sandbox.last_evidence_run_outcome.satisfied=15, .failed=2`) for ops dashboards. Load-bearing record lives on the run; mirror is for operator UX only.

### What testevidence owns

- Typed `EvidenceContract` payload + tier/facet vocabulary (`vocabulary/testevidence/`)
- `EvidenceRun` Lifecycle Participant (keyed by `(scenario_id, tier, run_id)`)
- Run scheduling: contract resolution → EvidenceRun creation → sandbox lease acquisition → verification injection
- Per-assertion result protocol (provenance triples on EvidenceRun per the shape above)
- Scenario↔sandbox binding resolution (the linkage layer)
- Renderer/plugin interface (`testevidence.Renderer` — Cucumber tags, OpenSpec scenarios, future surfaces)
- Tier vocabulary: closed set initially (unit/integration/smoke/e2e), extensibility via vocabulary additions

### What products keep on testevidence

- **SemSpec** keeps `qa_level` policy (tier→approved-contracts mapping), QA-reviewer prompts, BMAD personas, release-readiness verdicts. Cucumber tag rendering becomes a renderer plugin.
- **SemTeams** (when dev-via-spec returns) keeps Coordinator routing policy that maps chain → required tiers, scenario-generator prompts, the dev-sandbox-vs-integration-harness adjudication. Whatever scenario surface they render becomes a renderer plugin.
- Both call the same framework substrate manager: `testevidence.Manager`.

## The linkage layer

The two primitives meet at run scheduling — `EvidenceRun` is the linker:

```
scenario entity
  └─ EvidenceContract (stable spec, projected as triples)
       ├─ tier: integration
       ├─ environment_profile_ids: [mavlink.px4-sitl.mavsdk-smoke]
       ├─ required_assertions: [HEARTBEAT, mavsdk_core_connected]
       └─ verification: [...]
            ↓ scheduled by testevidence.Manager (per run cadence: PR, retry, nightly, release-candidate)
       EvidenceRun (Lifecycle Participant — keyed by (scenario_id, tier, run_id))
            ↓ acquires sandbox via
       sandbox.Manager.Lease(profile_id, profile_mode)
            ↓ profile_id resolves to
       sandbox.CapabilityContract (tools, services, profile mode, ...)
            ↓ provisioned via renderer, returns
       sandbox.Handle (env is now running, opaque to testevidence)
            ↓ EvidenceRun injects verification commands via handle.Exec
            ↓ per-assertion results stamp provenance triples on the EvidenceRun entity
       EvidenceRun reads its own assertion triples → satisfied/failed/skipped
            ↓ on terminal transition, releases lease
       sandbox.Manager.Release(handle) → handle teardown (or TTL cleanup if ephemeral)
```

**Key invariants (sharpened per Gemini round 2):**

- **sandbox has no knowledge of testevidence.** A sandbox lease is opaque from the framework side; it's a leased env. Who consumes the lease is the consumer's problem.
- **testevidence references sandbox by profile ID** (operator-named, vocabulary-validated). The resolution from profile-id → CapabilityContract is a `sandbox.Catalog.Resolve(profile_id)` call.
- **Verification commands live in `testevidence.EvidenceContract`**, not `sandbox.CapabilityContract`. EvidenceRun injects them into the leased env via the handle's exec interface.
- **Verification results stamp triples on the EvidenceRun entity**, not the sandbox entity. Provenance fanout (`evidence_run_id, scenario_id, tier, environment_profile_id, sandbox_lease_id`) preserves the multi-scenario reality where one env may satisfy 30 scenarios' assertions concurrently. Sandbox entity may carry summary mirror triples for ops dashboards only.
- **Contract is stable; EvidenceRun is the Participant.** Per [[feedback_separate_contract_from_run]] — the obligation does not mutate; runs against it do. N runs per contract is normal.
- **Lease mode is sandbox's property, not a separate primitive.** EvidenceRun for short-lived verification leases `mode: ephemeral, audit: minimal` sandboxs and carries the visible lifecycle itself. Long-lived shared envs lease `mode: reusable, audit: full` and get full Participant ceremony. See §Lease modes.

## Open design questions

These determine whether this is one ADR or two and how the substrate ships. **Proposal-stage; resolutions go in `sandbox-substrate-resolutions.md` before ADR drafts.**

### sandbox questions

**Q-W1. Lifecycle Participant or new substrate?**

The lifecycle `requested → provisioning → ready → leased → released/expired/failed` is textbook Lifecycle Participant shape (ADR-049). Lean: **Lifecycle Participant.** Same rationale as Q1 in the original proposal — ENTITY_STATES storage per [[feedback_bucket_ownership_rubric]], graph-visible workflow state, audit history at no extra cost, operator gateway shape inherited.

**Q-W2. Render-plugin contract — what's the smallest common interface?**

Tentative `sandbox.Renderer` interface (from original proposal, unchanged):

```go
type Renderer interface {
    Capabilities() RendererCapabilities
    Render(ctx context.Context, contract CapabilityContract) (Artifact, error)
    Provision(ctx context.Context, artifact Artifact) (Handle, error)
    Verify(ctx context.Context, handle Handle) (VerifyResult, error)  // ← rename: ProbeReady; not test-verification
    Release(ctx context.Context, handle Handle) error
}
```

Note `Verify` here is **readiness probing**, NOT test verification. Renamed in the split to avoid the testevidence overlap.

**Q-W3. Capability catalog — typed or string-token?**

Lean: **typed.** `vocabulary/sandbox/` owns capability IRIs (`tool.go.v1.26`, `service.postgres`, etc.). Same rationale as Q3 in original proposal — versioning is necessary for reproducibility; vocabulary pattern already established.

**Q-W4. Admission policy — framework gates vs. product composition?**

Lean: **two-layer admission.** Framework declares primitive gates (network/filesystem/secrets enums). Products compose policy on top. Unchanged from Q4 in original.

### testevidence questions

**Q-T1. Lifecycle Participant or new substrate?**

**Answered per Gemini round 2 refinement.** Lifecycle Participant **per EvidenceRun**, keyed by `(scenario_id, tier, run_id)`. The EvidenceContract is stable spec data with no lifecycle (authored, projected as triples on the scenario entity, never mutates). EvidenceRun carries the visible execution state. N runs per contract is the normal case (PR run, retry, nightly, release-candidate); each is its own Participant with full audit history. See §EvidenceRun and [[feedback_separate_contract_from_run]].

**Q-T2. Per-assertion result protocol (the old Q5, now scoped here).**

**Answered per Gemini round 2 refinement.** Option B (typed per-assertion result) with provenance fanout, stamped on the **EvidenceRun entity** (not the sandbox entity). See §Per-assertion result protocol for the triple shape.

Provenance triples per assertion include `evidence_run_id, scenario_id, tier, environment_profile_id, sandbox_lease_id` so cross-scenario reasoning preserves which env produced which assertion outcome and avoids the assertion-name-collision class (one env, 30 scenarios writing assertion `HEARTBEAT` on the same sandbox entity would overwrite). Sandbox entity may carry an optional summary mirror for ops dashboards only; load-bearing record lives on EvidenceRun.

Option C (streamed) deferred until production patterns demand it. Integrates with [[feedback_llm_authored_predicates_rule_opaque]]: evidence triples are framework-stamped (rule-opaque false by default) so QA rules can match deterministically.

**Q-T3. Tier vocabulary.**

Lean: **closed set initially.** Tiers = `unit | integration | smoke | e2e`. Add via vocabulary additions when a consumer needs a new tier (`stress`, `chaos`, `accessibility`, ...) — but tiers being a closed-and-named set is itself a discipline gate (prevents tier-of-the-week sprawl). Facets stay open-ended for free-form operator-named tagging.

**Q-T4. Renderer interface for testevidence.**

Symmetric to `sandbox.Renderer` but for projections of EvidenceContract:

```go
type Renderer interface {
    Capabilities() RendererCapabilities
    Render(ctx context.Context, contract EvidenceContract) (Artifact, error)
    // No Provision/Release — testevidence renderers don't run; they project.
    // The Cucumber tag string IS the artifact; how it gets consumed is the renderer's host (Cucumber CLI, OpenSpec runner, SemTeams scenario reader).
}
```

Lighter shape than sandbox.Renderer because testevidence doesn't provision — it adjudicates. The artifact is a string (Cucumber tags) or a struct (OpenSpec scenario) that consumers read.

### Cross-cutting

**Q-X1. One ADR or two?**

Per §Exit-beta significance, lean: **one ADR-052** covering both primitives as a co-shipping substrate bundle. Splitting into ADR-052 + ADR-053 invites sequencing risk; we don't want that pre-1.0. The ADR can have two clearly-named substrate sections internally.

**Q-X2. Linkage layer location.**

Three options:

- **A. testevidence owns the linkage** (lean). EvidenceContract references sandbox profile IDs; testevidence.Manager calls sandbox.Manager.Lease. sandbox stays unaware.
- **B. Third "coordination" layer.** A new `pkg/scenarioenv` (or similar) holds the linkage between testevidence and sandbox. Adds a primitive without a forcing function; rejected on rubric grounds.
- **C. sandbox-aware testevidence + bidirectional callback.** sandbox calls back to testevidence on lifecycle transitions. Couples the primitives; rejected.

Lean: **A.** Keeps sandbox pure; the linkage lives where the consumer reasoning happens.

## Naming

| Primitive | Package | Vocabulary |
|---|---|---|
| `sandbox` | `pkg/sandbox/` | `vocabulary/sandbox/` (tools, services, policy enums) |
| `testevidence` | `pkg/testevidence/` | `vocabulary/testevidence/` (tiers, assertion classes) |

### Decision history — why "sandbox" (not "workenv")

The original proposal landed under the working name `workenv` to avoid collision with the existing `processor/agentic-tools/sandbox/` package (an HTTP client to a remote tool-execution sandbox container, used by `BashExecutor`). User raised the naming question on 2026-05-31: industry has converged on "sandbox" for this exact shape (E2B, Anthropic computer use, Daytona, Modal, Cloudflare Workers, OpenAI playground all use "sandbox" for capability-aware execution environments). Shipping `pkg/workenv` post-1.0 would mean perpetually explaining "we call it workenv for historical reasons."

Resolution: **rename the existing `agentic-tools/sandbox/` to `agentic-tools/runner/`** (small, internal scope — 2 files + 1 caller) to free the term, then use `sandbox` for the substrate primitive. The renamed `runner` package describes the role accurately (HTTP client to a remote runner service that operates a sandbox container); external operator-facing surfaces unchanged (SANDBOX_URL env var, server-side wire protocol).

Tracked as part of this proposal's prep work (no separate ADR — internal rename, ADDITIVE to the substrate work). Done before ADR-052 lands so consumers see the substrate's intended naming from the first tag.

### One-liner anchors

- *"sandbox is the framework primitive for capability-aware execution environments — Lifecycle-managed leases, render plugins for the live target (devcontainer, act, k8s, ...), admission gates per capability class."*
- *"testevidence is the framework primitive for tier-bound test evidence ownership — what evidence proves what assertion, where it can be observed, who adjudicates. Render plugins project to Cucumber/OpenSpec/future scenario surfaces."*

## Two-product walkthrough (sanity check)

### SemSpec

1. Spec author declares two contracts in spec frontmatter:
   - `sandbox.CapabilityContract` per env profile the spec needs (with `profile.realization` + `audit` mode per the env's persistence class — short-lived test env = `ephemeral + minimal`; long-lived shared env = `reusable + full`).
   - `testevidence.EvidenceContract` per (scenario, tier) the spec asserts.
2. SemSpec's `qa.yml` renderer (sandbox plugin) translates the CapabilityContract → `.github/workflows/qa.yml`.
3. SemSpec's Cucumber renderer (testevidence plugin) translates the EvidenceContract → `@integration @slow` tag bag in `.feature` files.
4. QA-reviewer adjudicates the EvidenceContract (typed). `qa_level=production` maps to allowed (tier, env-profile) pairs per Q-W4 + product policy.
5. CI scheduling: per PR (or nightly, retry, release-candidate cadence), `testevidence.Manager` schedules an `EvidenceRun` keyed by `(scenario_id, tier, run_id)`, leases sandbox per profile-id, injects verification commands. Per-assertion results stamp provenance triples on the EvidenceRun entity.
6. EvidenceRun reads its own assertion triples → required_assertions evaluation → transitions to satisfied/failed/skipped.
7. Release-readiness verdict: predicate query against EvidenceRun entities (`evidence_run.scenario_id=X, .tier=integration, .outcome=satisfied, .run_id=<release-candidate-run>`).

### SemTeams (parked-on-substrate consumer)

When dev-via-spec returns:

1. architect/scenario-generator emits scenarios with EvidenceContracts declaring tier + env profile per requirement.
2. Coordinator reads the EvidenceContracts → derives required sandbox profiles → schedules EvidenceRuns via `testevidence.Manager`. For long-running agent chains, the EvidenceRun leases `mode: reusable, audit: full` sandboxs (Coordinator owns the chain's lifecycle); for ad-hoc tool execution inside the chain, ephemeral mode applies per the lease-modes heuristic.
3. sandbox lifecycle (full mode): `requested → provisioning → ready` → devcontainer renderer provisions → EvidenceRun leases (`ready → leased`).
4. Coordinator routes the dev agent chain into the leased env. Agent loops run; produce results that stamp per-assertion provenance triples on the EvidenceRun entity.
5. EvidenceRun adjudicates against required_assertions. Reviewers don't ask a dev sandbox to prove behavior only the integration harness can observe — the tier binding makes the boundary explicit BEFORE the dev loop fires (the agent-cost cut from §Why two primitives, not one).
6. On chain completion: EvidenceRun transitions to satisfied/failed; sandbox `leased → released`. Devcontainer teardown via `sandbox.Renderer.Release` (or TTL cleanup if ephemeral).

Both call `pkg/sandbox.Manager` and `pkg/testevidence.Manager`; both compose policy on top; neither owns substrate types or lifecycle.

## What this proposal does NOT decide

- **The render-plugin registry mechanism** (sandbox OR testevidence). Static linker-time registration vs. config-driven plugin loading vs. external-process plugins — deferred to design exercise.
- **Multi-tenant sandbox lease semantics.** A single contract can be satisfied by multiple realizers (lease pool); selection policy is out of scope for v1.
- **Resource quotas / cost accounting.** Lifecycle phases include `expires_at` (operator-writable per ADR-049 patterns), but quota enforcement is a Phase 2 layer.
- **Secret materialization protocol.** v1 treats `secrets:` as references; how they flow into the live env is renderer-specific. Cross-renderer secret abstraction is its own ADR.
- **Cross-tier scenario rollup policy.** "If unit + integration pass but e2e fails, is the requirement satisfied?" is product policy (SemSpec qa_level, SemTeams Coordinator rules), not framework substrate.
- **Whether testevidence renderers can drive consumers themselves.** Current shape: testevidence renderers project artifacts; consumers (Cucumber CLI, OpenSpec, etc.) drive. Some products may want the renderer to invoke directly — defer until a consumer asks.

## Next moves

1. **Design exercise** (`sandbox-substrate-design-exercise.md`): walk through Q-W1..Q-T4 + Q-X1..Q-X2 with SemSpec + SemTeams stakeholders. Resolve Q-W1 / Q-T1 / Q-X1 first — they gate substrate shape and ADR structure.
2. **Consumer sketches**:
   - `sandbox-semspec-sketch.md` — qa.yml renderer migration.
   - `sandbox-semteams-sketch.md` — Coordinator pre-routing.
   - `testevidence-semspec-sketch.md` — Cucumber renderer + qa_level policy migration.
   - `testevidence-semteams-sketch.md` — dev-via-spec scenario-generator integration (resolves the parked-on-substrate dependency).
3. **ADR-052** drafts once Q-W1+Q-T1+Q-X1 resolve. Phased PR plan follows the ADR-049 / ADR-048 model: substrate first, then first renderer (devcontainer + Cucumber probably), then first product migration, then operator gateway. Per [[feedback_pr_complete_system_unit]], bundle substrate + first consumer migration in the tagged release.
4. **Exit-beta gate.** Pre-1.0 tag candidate cuts after both substrates land + at least one consumer of each migrates + e2e green per [[feedback_e2e_required_for_breaking_changes]].

## Discipline anchors

- [[feedback_greenfield_cross_product_break_now]] — N=2 arrival of recurring shape; we own every consumer; act now, not on N=3+. **Refined this turn**: parked-on-substrate consumers count as current N, not future N.
- [[feedback_lift_structured_contract_not_friendly_projection]] — when N products converge on similar surfaces, factor on the underlying typed contract, not the projection they happen to share. New memory captured this turn from the Gemini review insight.
- [[feedback_separate_contract_from_run]] — the obligation is stable spec data; the execution is the Participant. Caught by Gemini round 2 on testevidence (EvidenceContract vs EvidenceRun). Applies wherever the same spec runs N times — mission specs vs mission runs, lease records vs lease executions, scheduled-job spec vs job-instance.
- [[feedback_bucket_ownership_rubric]] — default architectural answer is "live in ENTITY_STATES via Lifecycle Manager"; private buckets need defense on the rubric (Q-W1, Q-T1).
- [[feedback_reactive_patches_vs_engine_completion]] — design the primitive set deliberately; this proposal is the deliberate-completion frame against per-product runner + per-product evidence-tier churn.
- [[feedback_pr_complete_system_unit]] — when ADR-052 lands, bundle substrate + first renderer + first consumer migration as one tag. Substrate alone is a chunk boundary, not a system. Doubly important pre-exit-beta.
- [[feedback_e2e_required_for_breaking_changes]] — exit-beta-class tag needs full e2e green; both `cmd/semstreams` and `cmd/e2e-semstreams` wired for both substrates before tag.

## References

- ADR-049 (Lifecycle Prime substrate) — `docs/adr/049-lifecycle-prime.md`
- ADR-048 (BoundedDispatcher substrate primitive) — `docs/adr/048-bounded-dispatcher-and-triples-substrate.md`
- `workflow-primitives-decision.md` — the prior framework-vs-product layering exercise this proposal extends
- CLAUDE.md § Orchestration Boundaries — the substrate-not-engine discipline
- Gemini design review (2026-05-31, surfaced the sandbox↔testevidence split that this revision folds in)

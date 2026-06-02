# Sandbox + Testevidence Substrate — Design Exercise

**Status**: Proposed — 2026-05-31. Pre-ADR (ADR-052). Companion to
[`sandbox-substrate.md`](sandbox-substrate.md), which captures the
two-primitive thesis, the lease-mode + contract-vs-run refinements
from two Gemini review rounds, and the lean answers this exercise
resolves.

**Gate**: This document is the design-exercise artifact. Until it
lands on `main`, ADR-052 drafts do not begin and no `pkg/sandbox`
or `pkg/testevidence` code is written. The exercise resolves the
two gating questions (Q-W1, Q-X1), affirms the three answers
already landed by Gemini rounds (Q-T1, Q-T2, Q-X2), and lands shape
decisions for Q-W2/3/4 + Q-T3/4 with concrete enough specifics that
the ADR can quote-cite them. It also surfaces seven shape questions
the proposal didn't anticipate, each with a recommended resolution.

**Reading order at the bottom** — if you're skimming for a single
question, jump to its section.

## Summary

The proposal landed two co-shipping substrate primitives
(`pkg/sandbox` for runtime envs + `pkg/testevidence` for tier-bound
assertion contracts) with a connective layer (`scenario →
EvidenceContract → EvidenceRun → sandbox.Lease`). Two Gemini review
rounds folded in: split-into-two (round 1), lease-mode-as-property
+ contract-vs-run separation + provenance-on-run (round 2). The
substrate naming switched from `workenv` → `sandbox`; the existing
`agentic-tools/sandbox/` package was renamed to
`agentic-tools/runner/` (commit `fd73d217`) to free the term.

What's left before code: **resolve Q-W1 + Q-X1 (the gating
questions), land shape decisions for the six shape questions, and
answer the seven new questions this exercise surfaces.** That's
what this document does.

The high-order conclusions, in one paragraph: sandbox is a
Lifecycle Participant; ADR-052 covers both primitives in one
decision document with two clearly-named substrate sections;
profiles live in operator-config (`configs/sandbox-profiles.yaml`);
EvidenceRun entity IDs follow the standard 6-part shape;
agentic-tools/runner stays a distinct primitive (tool-level
isolation) explicitly out of scope for v1; secret materialization
is per-renderer in v1 with explicit per-renderer language in the
ADR so consumers know what they're committing to.

## Background

### How we got here

- **2026-05-31 morning**: proposal landed as `workenv-substrate.md`
  with single-primitive scope. Gemini round 1 surfaced the
  test-evidence-ownership split (don't lift Cucumber tags — lift
  the structured contract underneath). Proposal revised to two
  co-shipping primitives.
- **2026-05-31 afternoon**: Gemini round 2 surfaced three more
  refinements — lease mode as property (not a separate Ephemeral
  primitive), Contract-vs-Run separation on testevidence
  (`EvidenceContract` stable + `EvidenceRun` Participant), and
  per-assertion provenance on EvidenceRun (not on sandbox entity).
  All folded into the proposal.
- **2026-05-31 evening**: naming question — industry has converged
  on "sandbox" for this shape; `agentic-tools/sandbox/` collision
  resolved by renaming to `agentic-tools/runner/` (commit
  `fd73d217`); proposal file + memory updates swept; canonical
  state is now `docs/proposals/sandbox-substrate.md`.

### Why this exercise matters more than per-question framing suggests

The proposal lists 10 open questions (Q-W1..Q-W4, Q-T1..Q-T4,
Q-X1..Q-X2). Three are already answered (Q-T1, Q-T2 by round 2;
Q-X2 by the linkage-layer treatment). Two gate ADR shape (Q-W1,
Q-X1). The remaining five are shape questions.

What this exercise adds beyond walking the proposal's questions:

1. **It concretizes the Lifecycle-Participant declaration.** Q-W1's
   lean is "Lifecycle Participant," but the proposal didn't sketch
   what `sandbox.WorkflowDeclaration()` actually looks like against
   ADR-049's Manager API. Without that sketch, the lean is hand-
   waving; with it, the lean is implementable.
2. **It surfaces the new shape questions ADR-052 has to answer.**
   The proposal's "What this proposal does NOT decide" section
   listed deferrals (multi-tenant lease semantics, resource quotas,
   etc.) — but several decisions the ADR *does* have to make
   weren't named. Seven of them surfaced while preparing this
   exercise; they're in §New shape questions below.
3. **It locks the leans before consumer-sketch work begins.** The
   four consumer sketches (sandbox-semspec, sandbox-semteams,
   testevidence-semspec, testevidence-semteams) need stable
   substrate shape to write against. This exercise is the freeze
   point.

### The discipline lesson worth crystallizing

This proposal cycle is the cleanest application yet of
[[feedback_reactive_patches_vs_engine_completion]] +
[[feedback_greenfield_cross_product_break_now]] +
[[feedback_lift_structured_contract_not_friendly_projection]] +
[[feedback_separate_contract_from_run]] in concert. The pattern:

1. Two products converge on a shape (workenv proposal originator).
2. One product is parked-on-substrate (SemTeams dev-via-spec).
3. We lift the typed contract, not the projection (the round 1
   fix from Gemini).
4. We separate stable spec from execution (the round 2 fix).
5. We resist mode-as-separate-primitive when mode-as-property
   works (round 2 lease modes).
6. We rename other names to claim the right name for the
   substrate (the sandbox naming sweep).

Each of those discipline gates triggered the right call. Worth
referencing in ADR-052 as the deliberate-completion-frame case
study; future substrate work that skips this discipline cycle
should be flagged.

## Central questions

The exercise has two gating questions plus eight shape questions.
The gating questions block ADR structure; the shape questions
resolve in the ADR draft itself but want concrete answers here so
the draft can quote-cite.

| Question | Type | Status entering exercise | Resolved here |
|---|---|---|---|
| Q-W1 | **Gating** — sandbox Lifecycle Participant or new substrate? | Lean: Participant | Yes — see §Q-W1 |
| Q-X1 | **Gating** — one ADR-052 or two? | Lean: one ADR | Yes — see §Q-X1 |
| Q-T1 | Shape — testevidence Participant per (scenario, tier, run_id)? | Answered (round 2) | Affirmed — see §Q-T1 |
| Q-T2 | Shape — per-assertion result protocol? | Answered (round 2) | Affirmed — see §Q-T2 |
| Q-X2 | Shape — linkage layer location? | Lean: testevidence owns | Affirmed — see §Q-X2 |
| Q-W2 | Shape — render-plugin interface? | Sketched | Concretized — see §Q-W2 |
| Q-W3 | Shape — typed vs string-token capability catalog? | Lean: typed | Decided — see §Q-W3 |
| Q-W4 | Shape — admission policy layering? | Lean: two-layer | Decided — see §Q-W4 |
| Q-T3 | Shape — tier vocabulary scope? | Lean: closed initially | Decided — see §Q-T3 |
| Q-T4 | Shape — testevidence renderer interface? | Sketched | Concretized — see §Q-T4 |
| **New** | sandbox.Catalog shape | Not in proposal | Decided — see §New 1 |
| **New** | EvidenceRun entity-ID format | Not in proposal | Decided — see §New 2 |
| **New** | Operator gateway shape | Not in proposal | Decided — see §New 3 |
| **New** | First-tag vocabulary scope | Not in proposal | Decided — see §New 4 |
| **New** | agentic-tools/runner relationship | Not in proposal | Decided — see §New 5 |
| **New** | Cross-tier rollup ownership | Deferred in proposal | Decided — see §New 6 |
| **New** | Secret materialization v1 | Deferred in proposal | Decided — see §New 7 |

## Q-W1: sandbox Lifecycle Participant or new substrate?

### The question

The sandbox lifecycle (`requested → provisioning → ready → leased →
released/expired/failed`) is textbook Lifecycle Participant shape.
The choice is between:

- **Path A**: sandbox is a `Participant` against ADR-049's existing
  `lifecycle.Manager`. `sandbox.Manager` is a thin wrapper that
  adds render-plugin dispatch + admission policy + lease semantics.
- **Path B**: sandbox is its own substrate with its own Manager,
  its own KV/storage choices, its own admission, its own audit
  history.

### What Path A looks like

Per ADR-049's Manager API, sandbox declares a Workflow:

```go
// pkg/sandbox/workflow.go

package sandbox

import "github.com/c360studio/semstreams/pkg/lifecycle"

// SandboxState is the Lifecycle Participant payload for a sandbox
// lease. ENTITY_STATES-stored as per ADR-049; phase transitions
// emit triples that downstream rules / operator gateways can match.
type SandboxState struct {
    LeaseID            string             `lifecycle:"id"`
    ProfileID          string             `lifecycle:"profile_id"`
    RealizationMode    RealizationMode    `lifecycle:"realization"`
    AuditMode          AuditMode          `lifecycle:"audit"`
    TTL                time.Duration      `lifecycle:"ttl,operator-writable"`
    ExpiresAt          time.Time          `lifecycle:"expires_at,operator-writable"`
    LeaseHolder        string             `lifecycle:"lease_holder,operator-writable"`
    RendererName       string             `lifecycle:"renderer"`
    Handle             json.RawMessage    `lifecycle:"handle"` // renderer-opaque
    CapabilityContract CapabilityContract `lifecycle:"contract"`
}

// WorkflowDeclaration returns the Lifecycle Workflow spec for
// sandbox leases. Registered with lifecycle.Manager at component
// startup.
func WorkflowDeclaration() lifecycle.Workflow {
    return lifecycle.Workflow{
        Name:   "sandbox",
        Schema: reflect.TypeOf(SandboxState{}),
        Phases: []string{
            "requested", "provisioning", "ready",
            "leased", "released", "expired", "failed",
        },
        Transitions: lifecycle.Transitions{
            {From: "requested",   To: "provisioning"},
            {From: "provisioning", To: "ready"},
            {From: "provisioning", To: "failed"},
            {From: "ready",       To: "leased"},
            {From: "ready",       To: "expired"},  // TTL elapsed before lease
            {From: "leased",      To: "released"},
            {From: "leased",      To: "expired"},  // TTL elapsed mid-lease
        },
        OperatorWritablePredicates: []string{
            "sandbox.lease_holder",
            "sandbox.expires_at",
            "sandbox.ttl",
        },
    }
}
```

`sandbox.Manager` then wraps `lifecycle.Manager`:

```go
// pkg/sandbox/manager.go

type Manager struct {
    lc        *lifecycle.Manager
    catalog   *Catalog              // profile resolution
    renderers map[string]Renderer   // pluggable render targets
    admission *AdmissionPolicy      // capability gates
}

// Lease requests a sandbox env. Returns a Handle once the lifecycle
// reaches `leased`. Lease mode (reusable vs ephemeral) is a property
// of the profile, not a Manager parameter.
func (m *Manager) Lease(ctx context.Context, profileID, holder string) (*Handle, error) {
    contract, err := m.catalog.Resolve(profileID)
    if err != nil { return nil, err }
    if err := m.admission.Check(contract); err != nil { return nil, err }
    
    leaseID := generateLeaseID(profileID)
    state := SandboxState{
        LeaseID:            leaseID,
        ProfileID:          profileID,
        RealizationMode:    contract.Profile.Realization,
        AuditMode:          contract.Profile.Audit,
        TTL:                contract.Profile.TTL,
        LeaseHolder:        holder,
        RendererName:       selectRenderer(contract),
        CapabilityContract: contract,
    }
    
    if state.AuditMode == AuditMinimal {
        return m.leaseMinimal(ctx, state)   // skip Participant ceremony
    }
    return m.leaseFull(ctx, state)          // full Lifecycle Participant
}
```

Three things this gets us for free:

1. **ENTITY_STATES storage** per [[feedback_bucket_ownership_rubric]]
   — no private bucket; sandbox state is graph-visible.
2. **Operator gateway shape** inherited from ADR-049 — `GET
   /workflows?name=sandbox` returns leases; History via KV revision
   replay; operator patches via `OperatorWritablePredicates` tags.
3. **Restart recovery** — Lifecycle Manager already replays KV
   state to reconstruct in-flight workflows on restart.

### What Path B (new substrate) looks like

`sandbox.Manager` owns its own KV bucket (`SANDBOXES`), its own
state machine, its own audit history layer, its own operator
gateway endpoints. Roughly the shape ADR-047 originally proposed
for lifecycle before ADR-049 redirected it.

### Why Path A wins

The bucket-ownership rubric from
[[feedback_bucket_ownership_rubric]] applies directly:

| Rubric criterion | Sandbox satisfies? |
|---|---|
| Workflow state benefits from being a queryable graph fact? | **Yes** — operators want to query "which sandboxes are leased by which consumers" via graph; rules may want to match on `sandbox.realization=ephemeral AND sandbox.holder=X` |
| Audit history matters? | **Yes** — phase transitions are operator-debug-load-bearing (which lease provisioned when, why it failed) |
| Restart recovery matters? | **Yes** — in-flight provisioning sandboxes must replay on Manager restart |
| Cross-consumer composition? | **Yes** — testevidence.EvidenceRun consumes sandbox leases; their joint reasoning is graph-native |
| Volume / cardinality justifies private bucket? | **No** — sandbox leases are bounded (one per active consumer-task); no 10K-per-second write pressure that would justify bypassing graph-ingest |

All five criteria push toward Path A. The fifth (volume) is the
escape hatch in the rubric; sandbox doesn't meet it.

The minimal-audit ephemeral mode (where the audit cost isn't
justified) handles itself via `leaseMinimal` — no Participant
ceremony, no graph entity, just an opaque lease record with TTL
cleanup. The consumer (EvidenceRun) carries the visible lifecycle.
That's the round 2 lease-mode refinement working as designed; it
doesn't require Path B.

### What would change the lean

The lean would shift to Path B only if:

- We discovered that sandbox leases have write-amplification
  patterns (10K+ leases/sec) that graph-ingest can't absorb without
  back-pressure — but this is implausible at scale (sandbox leases
  are per agent chain or per CI run, not per request).
- ADR-049's Manager couldn't express one of sandbox's required
  semantics — but the WorkflowDeclaration sketch above shows it
  can.
- A consumer needed sandbox state to be queryable but NOT
  graph-visible — which contradicts the rubric.

None of these are credible. The lean holds.

### Decision: Path A — sandbox is a Lifecycle Participant

`sandbox.Manager` wraps `lifecycle.Manager` with render-plugin
dispatch + admission policy + lease semantics. Full-audit leases
get the Participant ceremony; minimal-audit leases bypass it and
carry visible lifecycle on the consumer (per round 2's lease
modes). ENTITY_STATES storage; operator gateway inherited; restart
recovery inherited.

ADR-052 should quote-cite the `WorkflowDeclaration` sketch as the
canonical sandbox-side wire-up.

### Minimal-audit eligibility — structurally enforced (round 4 refinement)

The round 2 lease-mode decision (`mode-as-property`) made minimal
audit cheap to opt into. Without guardrails, that turns into the
default-escape-hatch failure mode. Two enforcement layers — both
framework-side:

**Layer 1: contract-side hard eligibility rule.** `MinimalAuditEligible(c CapabilityContract) error` is admission-checked
when `LeaseOptions.Audit == AuditMinimal`. Hard denials:

| Condition | Rejected because |
|---|---|
| Any `secrets:` in contract | Secret materialization needs full audit trail |
| `network: open` | Open egress needs full audit trail |
| `filesystem: host_write` | Host write needs full audit trail |
| Any privileged flag (docker socket, etc.) | Privileged operation needs full audit trail |
| TTL > `MaxMinimalAuditTTL` (default 10 min) | Long-lived leases need full audit |

**Layer 2: owner verification via `lifecycle.Ref`** (round 4 — closes
the discipline-gate soft spot). The consumer claims a visible-lifecycle
owner via `LeaseOptions.Owner *lifecycle.Ref`. The Manager structurally
verifies via `lifecycle.Manager.Get(workflow, id)` before granting the
lease — no "trust me, I have an owner" claim:

```go
func (m *Manager) Lease(ctx context.Context, profileID string, opts LeaseOptions) (Handle, error) {
    contract, err := m.catalog.Resolve(profileID)
    if err != nil { return nil, err }

    if opts.Audit == AuditMinimal {
        // Layer 1: hard contract eligibility
        if err := MinimalAuditEligible(contract); err != nil {
            return nil, err
        }
        // Layer 2: framework-verified owner
        if opts.Owner == nil {
            return nil, ErrMinimalAuditRequiresOwner
        }
        if _, err := m.lc.Get(opts.Owner.Workflow, opts.Owner.ID); err != nil {
            return nil, fmt.Errorf(
                "minimal-audit owner %s/%s not found in lifecycle: %w",
                opts.Owner.Workflow, opts.Owner.ID, err,
            )
        }
        // Stamp lease_ref on owner so the bidirectional link is graph-visible
        if err := m.lc.UpdateWithTriples(opts.Owner.Workflow, opts.Owner.ID,
            []message.Triple{
                {Predicate: "owner.sandbox_lease_ref", Value: leaseID},
            },
        ); err != nil {
            return nil, err
        }
        return m.leaseMinimal(ctx, contract, leaseID, *opts.Owner)
    }
    return m.leaseFull(ctx, contract, leaseID, opts.Holder)
}
```

**`lifecycle.Ref` is a new framework-level primitive.** Small (two-field
struct, lives in `pkg/lifecycle/`), generic enough that other future
consumers (SemTeams chain entities holding agent-loop sandbox leases,
mission planners owning sensor-calibration envs) reuse the same Ref
pattern. ADR-052 treats this as a one-struct Lifecycle-layer addition,
NOT a sandbox-specific primitive:

```go
// pkg/lifecycle/ref.go

// Ref identifies a Lifecycle Participant by (workflow, id). Used by
// consumers that need to reference Participants across substrate
// boundaries — e.g., sandbox's minimal-audit owner check verifies
// the Ref via lifecycle.Manager.Get.
type Ref struct {
    Workflow string  // "evidence_run", "mission", "chain"
    ID       string
}
```

**Cleanup model.** Framework does **not** auto-release leases on
owner-terminal — would couple sandbox lifecycle to owner lifecycle in
ways that hurt long-lived envs surviving multiple owners. Per audit
mode:

- **`AuditFull`**: explicit `handle.Release()` by holder is required;
  TTL is the fallback safety net for crashed/leaked holders.
- **`AuditMinimal`**: explicit `handle.Release()` by owner is the
  expected path on terminal transition; TTL is the safety net if the
  owner crashes before transitioning. Owner crash → TTL cleanup,
  bounded by `MaxMinimalAuditTTL` (10 min default) so leaked
  ephemeral leases never linger.

Documented explicitly in the substrate doc so consumers know the
contract before they design around it.

## Q-T1: testevidence Participant shape — affirm

**Already answered by Gemini round 2.** Affirming here for the
record:

`EvidenceContract` is stable spec data (no lifecycle), authored in
scenario frontmatter and projected as triples on the scenario
entity. `EvidenceRun` is the Lifecycle Participant, keyed by
`(scenario_id, tier, run_id)`. N runs per contract is normal (PR /
nightly / retry / release-candidate).

The parallel `WorkflowDeclaration` for testevidence:

```go
// pkg/testevidence/workflow.go

type EvidenceRunState struct {
    RunID              string         `lifecycle:"id"`
    ScenarioID         string         `lifecycle:"scenario_id"`
    Tier               TierIRI        `lifecycle:"tier"`
    ContractRef        string         `lifecycle:"contract_ref"`
    ProfileIDs         []string       `lifecycle:"profile_ids"`
    SandboxLeaseIDs    map[string]string `lifecycle:"lease_ids"` // profile_id → lease_id
    AssertionResults   []AssertionResult `lifecycle:"results"`   // streamed via UpdateWithTriples
}

func WorkflowDeclaration() lifecycle.Workflow {
    return lifecycle.Workflow{
        Name:   "evidence_run",
        Schema: reflect.TypeOf(EvidenceRunState{}),
        Phases: []string{
            "scheduled", "bound", "verifying",
            "satisfied", "failed", "skipped",
        },
        Transitions: lifecycle.Transitions{
            {From: "scheduled", To: "bound"},
            {From: "scheduled", To: "skipped"},
            {From: "bound",     To: "verifying"},
            {From: "verifying", To: "satisfied"},
            {From: "verifying", To: "failed"},
        },
        OperatorWritablePredicates: []string{
            "evidence_run.skip_reason",
        },
    }
}
```

Note: `OperatorWritablePredicates` is small — operators don't
typically rewrite assertion results; the skip reason is the one
override surface (manual skip during incident response).

**EvidenceRun verification flow** depends on the leased sandbox's
renderer capability (round 4 refinement — see §Q-W2 for the
Renderer interface):

- **Inline-exec renderers** (`SupportsInlineExec=true`,
  devcontainer + docker-compose): EvidenceRun acquires Handle via
  `sandbox.Manager.Lease`, calls `handle.Exec(cmd)` per assertion
  in the `verification:` block, collects results directly,
  transitions `verifying → satisfied|failed`.
- **Scheduled-run renderers** (`SupportsScheduledRun=true`,
  qa.yml/act): EvidenceRun acquires Handle, then enters a polling
  loop: `collector.CollectResults(handleRef, runRef)` on a
  configurable cadence (default ~30s) until results arrive or
  timeout. Renderer-side may later wire webhooks under the same
  interface without breaking the EvidenceRun-side contract.

Capability mismatch (e.g., scheduled-run renderer leased without a
ResultCollector impl) is caught at admission, not mid-verification.

## Q-T2: per-assertion result protocol — affirm

**Already answered by Gemini round 2.** Affirming with one
implementation specific that emerged from sketching:

Per-assertion triples stamp on the EvidenceRun entity with
provenance fanout. The schema per assertion (concretized):

```
evidence_run:{scenario_id}.{tier}.run-{run_id}
  evidence_run.assertion.{name}.outcome           "satisfied" | "failed"
  evidence_run.assertion.{name}.observed_at       <RFC3339>
  evidence_run.assertion.{name}.sandbox_lease_id  <lease_id>
  evidence_run.assertion.{name}.evidence_ref      <object-store ref OR inline JSON>
  evidence_run.assertion.{name}.duration_ms       <int>
```

Plus per-run provenance (stamped once on entity creation):

```
  evidence_run.scenario_id          scenario.weather.heartbeat
  evidence_run.tier                 testevidence.tier.integration
  evidence_run.contract_ref         scenario.weather.heartbeat#evidence-contract
  evidence_run.run_id               <ULID or similar>
  evidence_run.scheduled_at         <RFC3339>
```

**Evidence ref pattern**: per-assertion `evidence_ref` is either
inline JSON (for small evidence like `{exit: 0, stdout_summary:
"..."}`) or an ObjectStore reference (for large evidence like full
stdout/stderr capture). The threshold defaults to 1KB; renderers
can override.

This integrates with ADR-050's `ContentStorable` pattern for the
ObjectStore-backed case, and with the standard graph-emit pattern
for the inline case.

## Q-X1: One ADR-052 or two?

### The question

ADR shape:
- **Path A**: One ADR-052 covering both `pkg/sandbox` and
  `pkg/testevidence` as a co-shipping substrate bundle, with two
  clearly-named substrate sections.
- **Path B**: ADR-052 covers sandbox; ADR-053 covers testevidence.
  Phased rollout — sandbox first, testevidence after.

### What Path B would look like

ADR-052 ships pkg/sandbox + devcontainer renderer + first consumer
migration (probably SemSpec qa.yml renderer). Tag cuts. Then ADR-053
ships pkg/testevidence + Cucumber renderer + scenario migration.
Tag cuts. SemTeams' parked-on-substrate work waits for ADR-053.

### Why Path A wins pre-exit-beta

Three reasons converge:

1. **Co-shipping is load-bearing per the proposal's framing.** The
   proposal explicitly argues that shipping sandbox without
   testevidence forces SemSpec to grow testevidence-shape locally —
   the parallel-path failure mode the substrate exists to prevent.
   Path B reintroduces that failure mode.
2. **Exit-beta calculus.** Path B means two ADR cycles, two
   release-candidate tags, two sister-repo migrations, two reviewer
   passes. Pre-1.0, each cycle is cheap; the architecture-clarity
   tax is bounded. Post-1.0, each cycle becomes expensive (deprecation
   windows, back-compat shims). Bundling now is the cheap shot.
3. **SemTeams parked-on-substrate.** Path B leaves SemTeams blocked
   for the duration of the sandbox-only cycle. Per
   [[feedback_greenfield_cross_product_break_now]] (parked-on-substrate
   refinement), the parked consumer counts as current N; not
   shipping the substrate they're parked on is the framework being
   the blocker.

### What would change the lean

Path B would win if:

- Sandbox and testevidence were structurally unrelated and shipping
  them together added cognitive load to the ADR — but they share
  the linkage layer (testevidence references sandbox by profile_id;
  EvidenceRun leases sandbox via Manager.Lease). They're a system.
- Sandbox needed a longer soak before testevidence depended on it —
  but the consumer dependency is one-way (testevidence consumes
  sandbox), and the soak applies to the bundle.
- The two primitives had different stakeholders who couldn't
  coordinate — but both primitives serve the same two products
  (SemSpec + SemTeams) and the design exercise is already joint.

None are credible. Path A holds.

### Decision: Path A — one ADR-052, two substrate sections

ADR-052 structure (recommended outline for the eventual draft):

```
# ADR-052: Sandbox + Testevidence Substrate Bundle

## Status
## Context
  ### Forcing function (both primitives)
  ### Why two primitives, not one
  ### Why co-shipping pre-exit-beta

## Decision
  ### D1. sandbox substrate (pkg/sandbox)
  ### D2. testevidence substrate (pkg/testevidence)
  ### D3. The linkage layer
  ### D4. First-tag scope (which renderers, which consumers)
  ### D5. Operator gateway shape

## Phasing
  Phase 1: substrate types + Managers (ADDITIVE)
  Phase 2: first renderers (devcontainer + Cucumber) (ADDITIVE)
  Phase 3: first consumer migration per primitive (ADDITIVE per primitive, may force MILD breaking for the migrating consumer)
  Phase 4: operator gateway shape (ADDITIVE)
  Phase 5 (deferred): hosted-tool renderers, multi-tenant lease pools, quota enforcement

## Consequences
## Alternatives Considered
  ### A. Single primitive bundling env + evidence (rejected — round 1)
  ### B. Separate ADR-052 (sandbox) + ADR-053 (testevidence) (rejected — this question)
  ### C. Mode-as-separate-primitive (rejected — round 2)
## Open implementation questions
## Sister-agent handoff
## References
```

## Shape questions

### Q-W2: render-plugin interface — concretized (Gemini rounds 3 + 4 applied)

The proposal sketched the interface; the concrete shape (sharpened
across two further Gemini review rounds):

**Persisted vs runtime split** (round 3 — closes service-locator
anti-pattern on `Handle.Exec`): the persisted form is `HandleRef`
(opaque bytes per renderer); the runtime form is `Handle`
(interface, rehydrated by Manager from HandleRef with the live
renderer registry attached). Execution dispatches through Manager,
not through a global registry lookup on the persisted struct.

**Structured readiness** (round 3): `ProbeReady` returns
`*ReadyResult, error` instead of `error` so degraded readiness is
first-class.

**Capabilities discoverable at admission** (round 3): `Capabilities()`
exposes structured flags so testevidence can fail at admission for
unsupported combinations, not mid-verification.

**Lease options + lifecycle.Ref-based owner check** (round 4 —
closes the minimal-audit owner soft spot): `Manager.Lease` takes a
`LeaseOptions` struct including `Owner *lifecycle.Ref`. For
`AuditMinimal`, Manager structurally verifies the owner via
`lifecycle.Manager.Get(workflow, id)` before granting the lease —
no discipline-gate "trust me, I have an owner" claim.

**Scheduled-run result-return path** (round 4 — closes the
"hopeful side channel" anti-pattern): `ResultCollector` is an
extension interface for renderers exposing results via a separate
collection path (CI runs, scheduled batch jobs) rather than inline
exec. Capability-set claim of `SupportsScheduledRun=true` without
`ResultCollector` impl is an admission error.

```go
// pkg/sandbox/renderer.go

// Renderer translates a sandbox.CapabilityContract into a live env.
// Each renderer targets one realization (devcontainer, qa.yml/act,
// docker-compose, k8s job).
type Renderer interface {
    // Name returns the unique renderer identifier (matches
    // CapabilityContract.realization.renderer entries).
    Name() string

    // Capabilities returns structured flags so consumers (testevidence)
    // can verify supported execution patterns at admission time,
    // not mid-verification. Renderers that claim SupportsScheduledRun
    // MUST also implement the ResultCollector extension interface;
    // mismatch fails at lease admission.
    Capabilities() RendererCapabilities

    // Render produces the artifact for the contract. Pure: no side
    // effects, no I/O. Used by the Manager before Provision so
    // admission can inspect the artifact.
    Render(ctx context.Context, contract CapabilityContract) (Artifact, error)

    // Provision instantiates the artifact. Returns a HandleRef
    // (persistable data); the live Handle is rehydrated by Manager
    // via Acquire(). May start containers, allocate cloud resources,
    // create temp directories — renderer-specific.
    Provision(ctx context.Context, artifact Artifact) (HandleRef, error)

    // ProbeReady checks whether the provisioned env is usable.
    // Returns structured ReadyResult so degraded readiness (e.g.,
    // "env is up but Postgres took 30s; you can proceed but slow")
    // is first-class. error is reserved for probe-itself-errored
    // (network failure, etc.). Renderers that provision synchronously
    // return {Ready: true, State: ReadyStateReady}.
    ProbeReady(ctx context.Context, ref HandleRef) (*ReadyResult, error)

    // Release tears down. Idempotent. Called on lease release,
    // expiry, or Manager shutdown.
    Release(ctx context.Context, ref HandleRef) error

    // Acquire rehydrates a runtime Handle from a persisted HandleRef.
    // The Handle holds a live reference to this Renderer; consumers
    // call handle.Exec / handle.Release without service-locator
    // lookups. After Manager restart, Acquire is called to
    // reconstitute Handles for in-flight leases discovered from
    // ENTITY_STATES.
    Acquire(ctx context.Context, ref HandleRef) (Handle, error)
}

// RendererCapabilities exposes structured flags per renderer so
// consumers (testevidence) can pick the right execution path at
// admission time. Adding a new capability is a vocabulary-additive
// change; renderers that don't set a new flag default to false.
type RendererCapabilities struct {
    // SupportsInlineExec: handle.Exec(cmd) returns results directly.
    // True for devcontainer, docker-compose. False for qa.yml/act.
    SupportsInlineExec bool

    // SupportsScheduledRun: results come back via ResultCollector
    // (polling-shaped from consumer side). True for qa.yml/act.
    // Renderer MUST also implement ResultCollector.
    SupportsScheduledRun bool

    // SupportsLeaseRefresh: lease TTL can be extended without
    // re-provisioning. True for long-running renderers (devcontainer);
    // false for short-lived (CI runs).
    SupportsLeaseRefresh bool
}

// HandleRef is the persisted form — opaque bytes per renderer.
// Stored as part of SandboxState in ENTITY_STATES. Recoverable
// across Manager restart because HandleRef carries lease_id +
// renderer_name + serialized payload.
type HandleRef struct {
    LeaseID      string
    RendererName string
    Payload      json.RawMessage  // renderer-opaque
}

// Handle is the runtime form, rehydrated by Manager from HandleRef.
// Carries the live renderer reference so Exec dispatches without
// service-locator lookups. Consumers hold a Handle for the lease
// duration; on Manager restart, Manager.Acquire reconstitutes from
// HandleRef.
type Handle interface {
    Ref() HandleRef

    // Exec runs a command inside the provisioned env. Used by
    // EvidenceRun for inline-exec renderers (SupportsInlineExec=true).
    // Returns ErrExecNotSupported on renderers that don't support
    // inline execution — but testevidence should never reach this
    // path because capabilities are admission-checked.
    Exec(ctx context.Context, req ExecRequest) (*ExecResult, error)

    // Release ends the lease. Idempotent. Cleaner ergonomic than
    // Manager.Release(ref) when the consumer already holds the
    // Handle.
    Release(ctx context.Context) error
}

// ResultCollector is the extension interface for renderers whose
// results return via a separate collection path (CI runs, scheduled
// batch jobs). Renderers claiming SupportsScheduledRun=true MUST
// implement this; admission rejects the lease otherwise.
type ResultCollector interface {
    Renderer

    // CollectResults returns assertion outcomes for the named run.
    // Polling-shaped: returns (nil, nil) when no results yet; the
    // consumer (EvidenceRun) drives the poll cadence (default ~30s,
    // configurable). Renderers may later wire webhooks under this
    // interface without breaking the consumer contract — they just
    // return faster when notified rather than polling-driven.
    CollectResults(ctx context.Context, ref HandleRef, runRef RunRef) ([]AssertionResult, error)
}

// RunRef carries the EvidenceRun identity + renderer-side correlation
// hints so the renderer can find the right scheduled run (CI workflow
// input, label, etc.).
type RunRef struct {
    EvidenceRunID string
    Correlation   map[string]string  // renderer-specific
}

// AssertionResult is what renderers (inline or collected) hand back.
// Maps to per-assertion provenance triples stamped on EvidenceRun.
type AssertionResult struct {
    Name           string
    Outcome        Outcome  // satisfied | failed
    ObservedAt     time.Time
    Duration       time.Duration
    Evidence       Evidence  // inline JSON or ObjectStore ref
    SandboxLeaseID string
}

// LeaseOptions are the parameters consumers pass to sandbox.Manager.Lease.
// Audit + Owner pairing is structurally enforced: AuditMinimal requires
// Owner != nil AND lifecycle.Manager.Get(Owner.Workflow, Owner.ID)
// succeeds; AuditFull accepts Owner == nil (lease itself is the
// Participant).
type LeaseOptions struct {
    Holder string         // attribution for AuditFull leases
    Audit  AuditMode      // AuditFull | AuditMinimal
    Owner  *lifecycle.Ref // required when AuditMinimal; framework-verified
}

// ReadyResult exposes structured readiness so consumers can react
// to degraded state. Reserved values for State; renderer-specific
// data in Details.
type ReadyResult struct {
    Ready   bool
    State   ReadyState  // ReadyStateReady | ReadyStateDegraded | ReadyStateNotReady
    Reason  string      // human-readable
    Details map[string]any  // renderer-specific
}

// Artifact is renderer-opaque content + metadata. Different
// renderers produce different shapes (YAML for qa.yml, JSON for
// devcontainer, k8s manifest for k8s, etc.).
type Artifact struct {
    RendererName string
    Content      []byte
    ContentType  string  // "application/x-yaml", "application/json", etc.
    Metadata     map[string]string  // renderer-specific hints
}
```

**Resolution**: this interface. ADR-052 quote-cites it; the three
v1 sandbox renderers (devcontainer, qa.yml/act, docker-compose)
implement against it. **Cucumber is NOT a sandbox renderer** —
it's testevidence-renderer-only (projection-only, no provisioning).
Same correction applies anywhere the proposal/exercise listed
Cucumber under sandbox v1 renderers.

**EvidenceRun-side flow** by capability:

- **Inline-exec renderers** (`SupportsInlineExec=true`,
  e.g., devcontainer + docker-compose): EvidenceRun calls
  `handle.Exec(cmd)` per assertion; collects results directly;
  transitions to terminal.
- **Scheduled-run renderers** (`SupportsScheduledRun=true`,
  e.g., qa.yml/act): EvidenceRun polls
  `collector.CollectResults(handleRef, runRef)` on a configurable
  cadence (default ~30s) until results arrive or timeout.
  testevidence-side polling loop; renderer-side collection.

testevidence rejects contracts at admission whose required renderer
capabilities don't match the renderer's actual capability set —
mismatch never reaches lease time.

### Q-W3: typed vs string-token capability catalog — decided typed

Lean was typed; affirming. Concrete shape:

```go
// vocabulary/sandbox/iris.go

const (
    // Tool capability tokens
    ToolGo     = Namespace + "tool.go"      // "go" toolchain, version specified inline
    ToolNode   = Namespace + "tool.node"
    ToolTask   = Namespace + "tool.task"
    ToolPlaywright = Namespace + "tool.playwright"
    // ... v1 set lands ~10 tokens; see Q-New 4

    // Service capability tokens
    ServiceNATS     = Namespace + "service.nats"
    ServicePostgres = Namespace + "service.postgres"
    // ... v1 set lands ~5 tokens

    // Policy enums (typed, not free-form strings)
    NetworkOpen      = Namespace + "network.open"
    NetworkRestricted = Namespace + "network.restricted"
    NetworkAirgapped = Namespace + "network.airgapped"
    FilesystemReadOnly      = Namespace + "filesystem.read_only"
    FilesystemWorkspaceWrite = Namespace + "filesystem.workspace_write"
    FilesystemHostWrite     = Namespace + "filesystem.host_write"
    SecretsReferenceOnly = Namespace + "secrets.reference_only"
    SecretsMaterialize  = Namespace + "secrets.materialize"
)
```

`Tool*` constants are the capability class; version goes in the
contract as a structured field (`tools: [{class: tool.go, version:
"1.26"}]`). This avoids combinatorial explosion of IRIs per
version.

### Q-W4: admission policy layering — decided two-layer

Lean was two-layer; affirming. Concrete shape:

```go
// pkg/sandbox/admission.go

// AdmissionPolicy validates a CapabilityContract against framework
// primitive gates. Framework-level rejections are absolute: a
// contract that fails framework admission cannot proceed regardless
// of product policy.
type AdmissionPolicy struct {
    // PrimitiveGates are the framework's hard limits — e.g.,
    // FilesystemHostWrite always requires explicit operator opt-in
    // via the SANDBOX_ALLOW_HOST_WRITE env var.
    PrimitiveGates GateSet
}

func (p *AdmissionPolicy) Check(contract CapabilityContract) error {
    // Framework gates: hard rejections
    if contract.Network == NetworkOpen && !p.PrimitiveGates.AllowOpenNetwork {
        return ErrAdmissionDenied{Reason: "network=open requires SANDBOX_ALLOW_OPEN_NETWORK=1"}
    }
    // ... per-gate checks
}

// Product policy layers on top — products compose their own
// admission rules using the sandbox.AdmissionPolicy as the floor.
// Example (SemSpec): qa_level=production allows only
// network=restricted, filesystem=workspace-write, secrets=[].
// This is NOT framework code; it lives in SemSpec.
```

### Q-T3: tier vocabulary — decided closed initially

Lean was closed; affirming. Concrete v1 set:

```go
// vocabulary/testevidence/tiers.go

const (
    TierUnit        = Namespace + "tier.unit"
    TierIntegration = Namespace + "tier.integration"
    TierSmoke       = Namespace + "tier.smoke"
    TierE2E         = Namespace + "tier.e2e"
)

// Facets are open-ended operator-named tags (not a closed vocabulary).
// Examples: "slow", "network-required", "gpu-required", "flaky-quarantine".
type Facet string
```

Tier set extends via vocabulary additions (filed when a consumer
needs a new tier). Facets stay open — they're for operator filtering,
not framework dispatch.

### Q-T4: testevidence renderer interface — concretized

Lighter than `sandbox.Renderer` because testevidence projects but
doesn't provision:

```go
// pkg/testevidence/renderer.go

type Renderer interface {
    Name() string

    // Render produces the artifact projecting the EvidenceContract.
    // Pure: no side effects. The artifact is consumed by the
    // renderer's host (Cucumber CLI, OpenSpec runner, SemTeams
    // scenario reader, etc.).
    Render(ctx context.Context, contract EvidenceContract) (Artifact, error)
}

// Artifact has the same shape as sandbox.Artifact for consistency,
// but testevidence Artifacts are always consumed externally
// (no Provision/Release on this side).
```

V1 renderers: Cucumber (for SemSpec), OpenSpec (for SemSpec if they
use it). SemTeams' renderer ships when they unfreeze dev-via-spec.

### Q-X2: linkage layer location — affirm

**Already answered.** testevidence owns the linkage; sandbox stays
consumer-blind. EvidenceRun acquires sandbox leases via
`sandbox.Manager.Lease(profile_id, holder)`. Affirming.

## New shape questions (surfaced by this exercise)

### New 1: sandbox.Catalog shape

The Catalog resolves profile IDs (`mavlink.px4-sitl.mavsdk-smoke`)
to CapabilityContract instances. Where do profile definitions live?

**Options**:
- A. Operator-config file (`configs/sandbox-profiles.yaml`),
  framework reads at startup.
- B. Vocabulary package (`vocabulary/sandbox/profiles/`), Go code.
- C. Sister-repo-owned (each consumer ships its own profiles).

**Decision: A.** Operator-config file. Reasons:
- Profiles are operator artifacts — version-controlled per
  deployment, not framework code.
- Sister-repos can contribute profile fragments via their own
  `sandbox-profiles.yaml` snippets that the framework concatenates
  at startup (analogous to the rules-config pattern).
- New profiles don't require a framework tag.

The catalog interface:

```go
// pkg/sandbox/catalog.go

type Catalog struct {
    profiles map[string]CapabilityContract
}

func LoadCatalog(paths []string) (*Catalog, error) {
    // Read all YAML files; merge profile definitions.
    // Duplicate profile_id is a startup error.
}

func (c *Catalog) Resolve(profileID string) (CapabilityContract, error) {
    contract, ok := c.profiles[profileID]
    if !ok {
        return CapabilityContract{}, ErrProfileNotFound{ID: profileID}
    }
    return contract, nil
}
```

Profile file format (round 3 reshape — renderer-specific config
embedded; round 3 also introduced the `realization` block / `lease`
block split to disambiguate the previously-overloaded `realization`
word):

```yaml
# configs/sandbox-profiles.yaml
profiles:
  mavlink.px4-sitl.mavsdk-smoke:
    schema_version: "1"
    owner: "semspec-team"        # operator attribution
    requirements:
      tools:
        - {class: tool.go, version: "1.26"}
        - {class: tool.python, version: "3.12"}
      services: [service.mavlink, service.px4-sitl]
      network: restricted
      filesystem: workspace-write
      secrets: []
    realization:                  # renderer choice + per-renderer config
      renderer: docker-compose
      config:
        compose_file: docker/mavlink-smoke.yml
        service: px4-sitl
    lease:                        # lease-mode property block
      mode: ephemeral             # reusable | ephemeral
      audit: minimal              # full | minimal
      ttl: 5m
```

**Naming-collision resolution**: Gemini round 2 used `profile.realization: ephemeral` (lease-mode field); round 3 introduced top-level `realization:` (renderer block). Same word, two meanings. Resolution: rename the lease-mode field to `lease.mode`, keeping `realization` exclusively for the renderer-binding block. Final structure groups lease properties (mode/audit/ttl) under `lease:` and renderer properties (renderer/config) under `realization:`.

Go side mapping:

```go
type CapabilityContract struct {
    SchemaVersion string
    Owner         string
    Requirements  Requirements
    Realization   Realization     // renderer + per-renderer config
    Lease         LeaseProfile    // mode + audit + ttl
}

type Realization struct {
    Renderer string           // "docker-compose", "devcontainer", "github-actions-act"
    Config   json.RawMessage  // renderer-opaque; each renderer defines its config schema
}

type LeaseProfile struct {
    Mode  LeaseMode      // ReusableMode | EphemeralMode
    Audit AuditMode      // AuditFull | AuditMinimal
    TTL   time.Duration
}
```

Each renderer defines its `Realization.Config` schema; framework validation enforces structural conformance at catalog-load time. Sister-repo profile contributions are validated against the registered renderer's config schema; mismatch fails at startup with a clear error pointing at the file/line.

### New 2: EvidenceRun entity-ID format

The 6-part EntityID convention (`org.platform.domain.system.type.instance`)
needs to extend to EvidenceRuns.

**Decision**: `{org}.{platform}.testevidence.{tier}.evidence_run.{scenario_short}-{run_id}`

Example: `c360.semspec.testevidence.integration.evidence_run.weather-heartbeat-01HXQ4`

Where:
- `{org}` and `{platform}` follow the host product's namespace.
- `testevidence` is the domain (fixed).
- `{tier}` is the system segment (per-tier separation in the ID
  enables tier-scoped graph queries without joining triples).
- `evidence_run` is the type segment (fixed).
- `{scenario_short}-{run_id}` is the instance segment: scenario
  short-name + ULID run ID, hyphen-joined.

Similarly for sandbox leases:

`{org}.{platform}.sandbox.{realization}.sandbox_lease.{profile_short}-{lease_id}`

Example: `c360.semspec.sandbox.ephemeral.sandbox_lease.mavlink-smoke-01HXQ4`

### New 3: Operator gateway shape

ADR-049's Lifecycle harness established the operator gateway
pattern (`GET /workflows`, History via KV revision replay,
operator-writable patches via struct tags). Both new primitives
inherit it.

Concrete endpoints:

```
GET /workflows?name=sandbox              # list sandbox leases
GET /workflows?name=sandbox&id={lease_id} # one lease
GET /workflows?name=sandbox&id={lease_id}/history # revision replay

GET /workflows?name=evidence_run          # list runs
GET /workflows?name=evidence_run&id={run_id}
GET /workflows?name=evidence_run&id={run_id}/history

PATCH /workflows?name=sandbox&id={lease_id}  # operator patches (only OperatorWritablePredicates)
PATCH /workflows?name=evidence_run&id={run_id}
```

No new operator endpoints; the existing Lifecycle gateway handles
both. ADR-052 references ADR-049 §Operator gateway for the
canonical surface.

### New 4: First-tag vocabulary scope

V1 vocabulary lands:

**`vocabulary/sandbox/`** (~15 tokens):
- Tools: `tool.go`, `tool.node`, `tool.task`, `tool.python`,
  `tool.playwright` (5 — covers SemSpec + SemTeams v1 needs)
- Services: `service.nats`, `service.postgres`, `service.redis` (3
  — covers common test deps)
- Policy enums: `network.{open, restricted, airgapped}`,
  `filesystem.{read_only, workspace_write, host_write}`,
  `secrets.{reference_only, materialize}` (9 across three classes)

**`vocabulary/testevidence/`** (~7 tokens):
- Tiers: `tier.{unit, integration, smoke, e2e}` (4)
- Outcomes: `outcome.{satisfied, failed, skipped}` (3)

Additions via vocabulary PRs when consumers ask. No upfront
exhaustive enumeration.

### New 5: agentic-tools/runner relationship to sandbox

The renamed `agentic-tools/runner/` is a tool-level isolation
primitive (HTTP client to a remote sandbox container, used by
`BashExecutor`). The new `pkg/sandbox` is an env-level capability-
aware substrate.

**Question**: do they ever converge? E.g., does `agentic-tools/runner`
become a `sandbox.Renderer` so the same substrate provisions
both env-level (CI) and tool-level (bash exec) isolation?

**Decision: explicitly out of scope for v1.** Reasons:
- Convergence is plausible but speculative.
- Forcing the convergence now constrains both primitives in
  awkward ways (tool-level runner has its own lifecycle managed by
  BashExecutor; substrate-level lease lifecycle is heavier).
- Future work: file an issue post-ADR-052 to revisit if a real use
  case emerges (e.g., SemTeams wanting agent chains to share a
  long-lived sandbox via the substrate Lease mechanism instead of
  the per-call HTTP client).

**Phrasing for ADR-052** (lifted verbatim from Gemini round 3):

> runner may later become an exec backend or sandbox renderer, but
> v1 keeps tool-call isolation separate from environment leasing.

That keeps the convergence door open without making ADR-052 pay
for the design now.

### New 6: Cross-tier rollup policy ownership

Proposal said: product policy, not framework substrate. Affirming
with explicit ADR language:

> The framework does not provide a "cross-tier satisfaction"
> evaluator. SemSpec's `qa_level=production` deciding that a
> requirement requires unit + integration + e2e all satisfied is
> product policy. Framework primitives expose per-(scenario, tier,
> run_id) outcomes; products compose rollup rules on top using
> standard predicate queries.

Reason: rollup is product-policy-shaped. SemSpec's qa_level mapping
is one shape; SemTeams' Coordinator-tier-routing is a different
shape; future products will have their own. Forcing one rollup
abstraction is the friendly-projection-vs-contract failure mode
(see [[feedback_lift_structured_contract_not_friendly_projection]]).

### New 7: Secret materialization v1 — `SecretResolver` abstraction (round 3 reshape)

The original v1 sketch mounted `~/.config/sandbox/secrets.json` into
containers as env vars. Gemini round 3 caught the foot-gun: that's
file-mount-all-secrets-into-every-container, which violates
least-privilege and makes evidence-redaction infeasible.

**Replaced with a `SecretResolver` abstraction.** Framework contract
references secret names only; renderers ask the resolver for exactly
the requested refs; materialization happens per-lease, not
global-mount. Evidence redaction is structurally enforced before
`evidence_ref` persistence.

```go
// pkg/sandbox/secrets.go

// SecretResolver is the framework-side abstraction for secret lookup.
// Operators wire concrete implementations at startup (env-var-backed,
// vault-backed, file-backed, k8s-secret-backed). Renderers request
// specific refs; values never live in lifecycle state.
type SecretResolver interface {
    // Resolve returns values for the requested refs. Returns error
    // if any ref is unknown — fail-closed rather than silently
    // returning empty.
    Resolve(ctx context.Context, refs []SecretRef) (map[string]SecretValue, error)
}

// SecretRef is the framework-level secret identity. Matches the
// `secrets: [...]` entries in CapabilityContract.
type SecretRef struct {
    Name string  // "openai_api_key" — name only; no value, no scope
}

// SecretValue is what the resolver hands back. Renderer-side code
// must NEVER write this to SandboxState.Handle (graph-visible) or
// log the .Value bytes. RedactionPatterns are checked against any
// captured stdout/stderr before evidence_ref persistence.
type SecretValue struct {
    Value             []byte
    RedactionPatterns []string  // regexes; renderer scrubs matches
}
```

**Per-renderer materialization** (replaces the per-renderer table from
the v1 sketch):

| Renderer | Materialization path |
|---|---|
| devcontainer | Renderer calls `resolver.Resolve(refs)` right before `Provision`. Materializes values to per-lease temp env file (`/tmp/sandbox-{leaseID}.env`, `0600` perms) mounted into the container; deleted on `Release`. |
| docker-compose | Same per-lease temp env file (`0600`); passed via `--env-file`; deleted on `Release`. |
| qa.yml / act | Two paths documented explicitly: (a) GitHub Actions: renderer emits `${{ secrets.X }}` references in the YAML; GitHub resolves at runtime via repo-secrets. (b) local `act`: operator-provided `.secrets` file path via env config (`ACT_SECRETS_FILE`); renderer doesn't materialize values directly. |
| testevidence renderers (Cucumber etc.) | N/A — no secrets at projection layer. EvidenceRun-injected verification commands run in the leased sandbox's env (secrets materialized there per the sandbox renderer above). |

**Discipline gates** that every renderer must honor:

1. **Never write secret values to `SandboxState.Handle`.** That field
   round-trips through ENTITY_STATES → graph-visible. Materialization
   target is per-lease ephemeral storage only.
2. **Never log secret values.** Including debug logs; including
   error messages with redacted-but-not-scrubbed templates.
3. **Always scrub `RedactionPatterns` from captured stdout/stderr
   before `evidence_ref` persistence.** This is mandatory for any
   renderer with `SupportsInlineExec=true` OR
   `SupportsScheduledRun=true`. testevidence's per-assertion
   evidence pipeline applies the redaction before ObjectStore write.
4. **Per-lease temp file perms = `0600`.** Renderer creates and
   deletes; no shared mount points.

**v1 limitation** carried forward (per the proposal's deferral):
cross-renderer secret abstraction (sealed-secrets-style
cluster-wide secret distribution) is post-1.0 work. Consumers
needing secrets in a renderer not listed here either add the
renderer (implementing the SecretResolver contract per the
discipline gates above) or wait for the future abstraction ADR.

## Open questions for the design session

After this exercise, what's still genuinely open for SemSpec +
SemTeams stakeholder input:

1. **What's the first consumer migration we tag against?** The
   proposal listed four sketches; ADR-052 needs to pick one or two
   to migrate IN the substrate-landing tag per
   [[feedback_pr_complete_system_unit]]. SemSpec qa.yml renderer is
   the lowest-risk candidate (existing code, smallest delta);
   SemTeams Coordinator devcontainer is the highest-value
   (unblocks parked work). Recommend BOTH but defer the call to
   stakeholders.
2. **Operator-config schema for sandbox profiles** — the
   `configs/sandbox-profiles.yaml` shape sketched in §New 1 is the
   v1 proposal; stakeholders should validate the shape against
   their actual profile definitions.
3. **EvidenceRun retention policy.** How long does an EvidenceRun
   entity persist after terminal transition? Operator-debug-load
   suggests "indefinitely" but graph storage cost may push back at
   high run volumes (e.g., 100 PR runs × 50 scenarios × 4 tiers =
   20K runs/month per repo). Lean: TTL on minimal-audit ephemeral
   runs (1 week default); indefinite on full-audit. Stakeholder
   input on default.
4. **First-tag e2e tier definition.** Probably `task e2e:sandbox`
   + `task e2e:testevidence` as new tiers, OR extension of
   `task e2e:agentic`. Stakeholders pick.

## Reading order for the design session

If reading this end-to-end:

1. §Summary + §Background — context for what changed.
2. §Q-W1 — the gating sandbox-shape decision; everything else
   hangs off this.
3. §Q-X1 — the gating ADR-shape decision.
4. §New 1 + §New 2 + §New 3 — the three new infrastructure shapes
   ADR-052 has to nail (catalog, entity-ID, gateway).
5. §New 4 + §New 7 — first-tag scope decisions that lock the v1
   surface.
6. §Open questions for the design session — what's still genuinely
   open for stakeholder input.

If reading for a specific question, jump to its section via the
table in §Central questions.

## Discipline anchors

- [[feedback_greenfield_cross_product_break_now]] — parked-on-substrate
  refinement applies; SemTeams testevidence counts as current N=2.
- [[feedback_lift_structured_contract_not_friendly_projection]] —
  Cross-tier rollup decision (§New 6) applies this principle.
- [[feedback_separate_contract_from_run]] — Q-T1 affirmation; the
  EvidenceContract vs EvidenceRun split is the canonical worked
  example.
- [[feedback_bucket_ownership_rubric]] — Q-W1 decision applies the
  rubric directly.
- [[feedback_pr_complete_system_unit]] — open question 1 (first
  consumer migration) is the bundling discipline.
- [[feedback_e2e_required_for_breaking_changes]] — open question 4
  (e2e tier definition) is the pre-tag gate.

## References

- [`sandbox-substrate.md`](sandbox-substrate.md) — parent proposal;
  this exercise's leans came from there.
- [ADR-049](../adr/049-lifecycle-prime.md) — Lifecycle Prime
  substrate; sandbox + testevidence both wrap it.
- [ADR-048](../adr/048-bounded-dispatcher-and-triples-substrate.md)
  — substrate-primitive pattern model.
- [ADR-050](../adr/050-swe-common-schema-bound-encodings.md) —
  recent ContentStorable usage; testevidence evidence_ref pattern
  follows it.
- [ADR-051](../adr/051-openai-responses-wire-support.md) — recent
  precedent for [[feedback_greenfield_cross_product_break_now]] +
  [[feedback_lift_structured_contract_not_friendly_projection]]
  applied together in one cycle.
- Gemini design reviews (rounds 1 + 2, 2026-05-31) — surfaced the
  split + lease modes + contract-vs-run + provenance-on-run
  refinements that this exercise locks.
- Commit `fd73d217` — `agentic-tools/sandbox` → `agentic-tools/runner`
  rename that freed the `sandbox` name for the substrate.

# SemTeams Consumer Sketch — `pkg/testevidence` (parked-on-substrate)

**Status**: Working draft — 2026-05-31. Pre-ADR (ADR-052).
Companion to [`sandbox-substrate.md`](sandbox-substrate.md),
[`sandbox-substrate-design-exercise.md`](sandbox-substrate-design-exercise.md),
and [`sandbox-semteams-sketch.md`](sandbox-semteams-sketch.md).

**Scope**: Maps SemTeams' (currently parked) dev-via-spec
scenario-generator + Coordinator routing path onto
`pkg/testevidence` + a SemTeams-side renderer. **This is the
highest-value consumer sketch** because SemTeams dev-via-spec is
the parked-on-substrate consumer counted as the testevidence N=2
data point (per the proposal's parked-on-substrate refinement).
Getting the substrate shape right for this consumer is what
justifies co-shipping testevidence with sandbox.

**Stakeholder input needed** on items marked **[STAKEHOLDER]**.
Many sections are speculative because dev-via-spec is parked and
hasn't been re-specified post-substrate decisions; the sketch is
written to validate that the substrate UNBLOCKS dev-via-spec
cleanly, with SemTeams-team free to refine specifics.

## Current state (what's parked, and why)

SemTeams' dev-via-spec is a multi-agent flow where an architect
agent reads a requirement spec, decomposes it into scenarios with
evidence claims, hands those to a scenario-generator agent that
expands each into runnable test scaffolding, hands those to a
dev-loop agent that writes implementation against the scaffolding,
and hands the result to a reviewer agent that adjudicates against
the evidence claims. (Paraphrased; **[STAKEHOLDER]** confirm
exact flow shape.)

**Why it's parked** (per the proposal's §Background "SemTeams
testevidence side (parked on substrate)"):

> architect/scenario-generator needs to declare "this requirement
> needs evidence at unit + integration tier in env profiles X and
> Y" so the sandbox manager prepares the matching environment, and
> reviewers stop asking a dev sandbox to prove behavior that only a
> QA/integration harness can observe. Currently parked because the
> substrate isn't there.

Concretely:

1. **Without sandbox substrate**: scenario-generator can't declare
   "this requirement needs a postgres in restricted-network mode";
   today that's per-chain devcontainer config (per
   sandbox-semteams-sketch.md). dev-via-spec would need to grow
   its own env-declaration logic that duplicates devcontainer
   profiles.
2. **Without testevidence substrate**: scenario-generator can't
   declare "this evidence claim is integration-tier; the dev
   sandbox tier can't prove it." Reviewer ends up asking the
   dev-loop agent to prove integration-tier behavior from a unit-
   tier dev context → wasted tokens, wrong conclusions, agent
   confusion ("I tested it in my dev env and it worked, why does
   the reviewer say it failed?").
3. **Without the linkage**: scenario-generator can't bind specific
   evidence claims to specific env-profiles. The "where can this
   evidence be observed" cut isn't expressible.

All three points trace back to the substrate not existing. Shipping
the substrate unblocks dev-via-spec.

## The agent-cost cut — why this sketch is load-bearing

The proposal's strongest argument for the split is the agent-cost
framing (§Why two primitives, not one):

> Agents need to reason about WHERE evidence can be observed BEFORE
> they spend tokens trying to prove it.

dev-via-spec is the consumer that lives or dies by this cut. A
dev-loop agent that doesn't know "this evidence is integration-
tier; my dev sandbox can't prove it" will burn tokens trying. A
scenario-generator that can't express "this assertion needs the
integration harness" will hand the dev-loop agent an unprovable
task and the loop will spin until budget exhaustion.

Every other consumer (SemSpec qa.yml, SemTeams Coordinator
routing, the eventual k8s renderer) is a polish-and-ergonomics
migration. **dev-via-spec is the consumer whose agent flow
structurally fails without the substrate.** That's what makes it
parked-on-substrate-not-anticipated.

## Proposed migration

### Step 1: scenario-generator emits `EvidenceContract`s

The scenario-generator agent's output today (paraphrased) is a list
of scenario specs with implementation guidance for the dev-loop.
Post-substrate, each scenario carries an `EvidenceContract` that
declares tier + env-profile + required assertions:

```yaml
# scenario-generator output (per requirement)
requirement_id: weather-alerting-v1
scenarios:
  - scenario_id: weather.alert.5s-publish-latency
    description: Alert published within 5s of temperature anomaly
    
    # NEW: typed evidence contract
    evidence_contract:
      tier: testevidence.tier.integration
      facets: [slow, network-required]
      environment_profile_ids:
        - semteams.profile.weather-stack-integration
      required_assertions:
        - alert_published_to_correct_topic
        - alert_latency_within_5s
      verification:
        - "task test:integration -- --scenario=5s-publish-latency"
    
    implementation_guidance: |
      The dev-loop should implement the alert publisher logic.
      The scenario is verified against the integration harness;
      do not attempt to prove integration-tier assertions from the
      dev sandbox.
```

The scenario-generator agent's prompt becomes structured around
this output shape. Per
[[feedback_tool_signature_intent_not_structure]], the scenario-
generator's tool signature is "express the requirement's testable
shape" (intent), not "fill in fields of a YAML template" (structure)
— but the YAML output is what the framework consumes.

### Step 2: Coordinator routes the dev-loop chain per (scenario tier, env profile)

Coordinator reads scenario-generator's output, derives required
sandbox profiles, and pre-routes the dev-loop chain into the right
environment:

```python
# semteams-repo: coordinator/dev_via_spec_routing.py (after migration)

def route_dev_loop_chain(requirement, scenario):
    """Route a dev-loop chain for one scenario.
    
    Critical: the dev-loop runs in the DEV-TIER sandbox profile,
    not the scenario's TARGET tier. The dev-loop implements; the
    EvidenceRun verifies in the target tier.
    """
    
    # Dev-tier env: where the dev-loop agent works
    dev_profile_id = "semteams.profile.dev-go-node"  # standard dev env
    dev_handle = sandbox_client.Lease(
        profile_id=dev_profile_id,
        opts=LeaseOptions(
            holder=f"dev-loop-{requirement.id}-{scenario.id}",
            audit=AuditFull,
        ),
    )
    
    # The chain entity (Lifecycle Participant) is established here:
    chain_entity = lifecycle_client.Create(
        workflow="dev_loop_chain",
        id=f"{requirement.id}/{scenario.id}",
        state=DevLoopChainState{
            RequirementID: requirement.id,
            ScenarioID:    scenario.id,
            DevSandboxLeaseID: dev_handle.Ref().LeaseID,
            EvidenceContractRef: scenario.evidence_contract,  # ← the testevidence-side contract
        },
    )
    
    return DevLoopChain(
        chain_entity=chain_entity,
        dev_sandbox=dev_handle,
        target_evidence_contract=scenario.evidence_contract,
    )
```

The Coordinator-side discipline is enforced: dev-loop runs in
dev-tier; verification runs in the contract's declared tier.
**These are different env-profile leases, and the framework makes
that explicit.**

### Step 3: the dev-loop reads the evidence contract — "what can I prove here?"

The dev-loop agent's first read in its session is the evidence
contract attached to the chain. The agent's persona prose
explicitly handles the agent-cost cut:

```
You are the dev-loop agent. Your scenario has an evidence contract:

  tier: integration
  required_assertions: [alert_published_to_correct_topic, alert_latency_within_5s]
  verification: "task test:integration -- --scenario=5s-publish-latency"

CRITICAL: You are running in the DEV sandbox (semteams.profile.dev-go-node).
The integration-tier assertions in your contract CAN ONLY BE OBSERVED in
the integration harness (semteams.profile.weather-stack-integration).
Do not attempt to prove them locally. Your job is:

1. Implement the code that makes the verification command pass.
2. Run the verification command in your dev sandbox — this catches
   compile errors, smoke-test failures, etc. Inline-exec results are
   informational only; they do not count as evidence.
3. Hand off to the EvidenceRun, which runs in the integration sandbox
   and produces the load-bearing assertion outcomes.
```

This is the **load-bearing prompt design** the substrate enables.
Without testevidence, this prompt could not be written — the dev-
loop agent would have no typed way to know what tier its env is at
vs what tier the assertions require.

Per
[[feedback_persona_prose_needs_decision_criteria]],
every tool sentence in the dev-loop persona carries a when-to-use
criterion. The substrate makes those criteria typed and queryable.

### Step 4: EvidenceRun verifies in the integration sandbox

When the dev-loop transitions to "I think it's ready for evidence"
(typically by emitting a completion triple), the Coordinator
schedules an EvidenceRun against the scenario's contract:

```python
# semteams-repo: coordinator/dev_via_spec_routing.py (continued)

def schedule_evidence_run(chain_entity, scenario):
    """Schedule EvidenceRun in the contract's target env."""
    
    run = testevidence_client.ScheduleRun(testevidence.ScheduleRequest{
        Contract: scenario.evidence_contract,
        RunID:    f"chain-{chain_entity.id}-rev-{chain_entity.revision}",
    })
    
    # The EvidenceRun leases its own sandbox per the contract:
    # - target sandbox: semteams.profile.weather-stack-integration
    # - holder: f"evidence_run-{run.id}" (full audit; production-class)
    # - audit: AuditFull (integration runs are production-class for SemTeams)
    
    # Coordinator watches for EvidenceRun's terminal transition:
    return run
```

EvidenceRun does the substrate dance per the design exercise §Q-T1:
provision the integration sandbox via the renderer; either inline-
exec the `verification:` commands (if `SupportsInlineExec=true`) or
poll `CollectResults` (if `SupportsScheduledRun=true`); collect
per-assertion outcomes; stamp provenance triples.

### Step 5: reviewer reads EvidenceRun outcomes, NOT dev-loop chain output

The reviewer agent's release-readiness adjudication queries
EvidenceRun entities:

```python
# semteams-repo: reviewer/adjudicator.py (after migration)

def adjudicate_chain(chain_entity):
    """Did the chain produce satisfied evidence at the right tier?"""
    
    scenarios = lifecycle_client.GetChain(chain_entity).Scenarios
    
    for scenario in scenarios:
        # Query: for this scenario, for the most recent run of this
        # chain, did the EvidenceRun reach satisfied?
        runs = graph_client.QueryEvidenceRuns(EvidenceRunQuery{
            ScenarioID: scenario.id,
            Tier:       scenario.evidence_contract.tier,
            ChainEntityRef: chain_entity.ref(),  # correlation
        })
        
        if not runs:
            return Verdict(approved=False, reason=f"no EvidenceRun for {scenario.id}")
        
        latest = max(runs, key=lambda r: r.scheduled_at)
        if latest.outcome != OutcomeSatisfied:
            return Verdict(approved=False, reason=f"{scenario.id} tier={latest.tier} outcome={latest.outcome}")
    
    return Verdict(approved=True)
```

Critical discipline: **the reviewer never queries the dev-loop's
inline-exec results.** Those were informational; the load-bearing
record is the EvidenceRun's per-assertion provenance triples. The
substrate makes this distinction structural — dev-loop's
`handle.Exec(cmd)` results stay on the dev-sandbox lease; assertion
outcomes are on the EvidenceRun.

This is the cut that closes the "I tested it in my dev env and it
worked, why does the reviewer say it failed?" failure mode.

### Step 6: ephemeral tool execution within the dev-loop (sandbox-semteams Gap)

(Cross-reference: sandbox-semteams-sketch.md Step 2.)

When the dev-loop agent needs a one-shot tool (psql probe, log
query, etc.), it leases an ephemeral sandbox with the chain entity
as `lifecycle.Ref` owner:

```python
def probe_database_during_dev_loop(chain_entity_ref, query):
    handle = sandbox_client.Lease(
        profile_id="semteams.tool.psql-probe",
        opts=LeaseOptions(
            holder=f"dev-loop-{chain_entity_ref.id}-probe",
            audit=AuditMinimal,
            owner=chain_entity_ref,  # ← chain entity owns the lifecycle
        ),
    )
    # ... handle.Exec, handle.Release, TTL safety net
```

This is the round 4 `lifecycle.Ref` pattern in action. The
ephemeral lease's visible lifecycle is the dev-loop chain; the
sandbox substrate stays consumer-blind.

## Acceptance criteria for SemTeams dev-via-spec unblock

After the migration lands (i.e., dev-via-spec un-parks and ships):

- [ ] scenario-generator emits `EvidenceContract`s as structured
  output; agent prompts express intent (per
  [[feedback_tool_signature_intent_not_structure]]).
- [ ] Coordinator pre-routes dev-loop chains into dev-tier sandbox
  while binding the scenario's evidence contract to the chain
  entity.
- [ ] dev-loop persona prose explicitly handles the dev-vs-target-
  tier distinction; the agent doesn't burn tokens trying to prove
  integration-tier assertions from dev.
- [ ] EvidenceRun lifecycle runs in the contract's target env,
  produces per-assertion triples, transitions to terminal.
- [ ] Reviewer adjudicates against EvidenceRun outcomes; no dev-
  loop output parsing for evidence claims.
- [ ] Ephemeral tool execution within dev-loop uses chain entity as
  `lifecycle.Ref` owner; framework verifies the owner ref.
- [ ] At least one requirement has end-to-end dev-via-spec'd:
  architect decomposes → scenario-generator emits contracts →
  Coordinator routes → dev-loop implements → EvidenceRun verifies
  → reviewer adjudicates → release verdict. Working multi-agent
  flow proof.
- [ ] Token-cost telemetry shows dev-loop chains DO NOT burn tokens
  trying to prove cross-tier evidence (the agent-cost cut is
  observable in production telemetry).

## Open gaps surfaced by this sketch (parked-consumer-specific)

### Gap 1: dev-via-spec flow shape — needs SemTeams-team re-specification [STAKEHOLDER]

The sketch above paraphrases dev-via-spec's flow as
architect → scenario-generator → dev-loop → reviewer. The actual
flow may differ; SemTeams team needs to re-spec dev-via-spec on top
of the substrate before this sketch lands as canonical.

**[STAKEHOLDER]** — SemTeams-team produces a dev-via-spec re-spec
doc post-ADR-052; this sketch is then validated/corrected against
that re-spec. **Do not treat the flow shape in §Step 1-6 as
authoritative.** The substrate-side contract (what `pkg/testevidence`
and `pkg/sandbox` expose) IS authoritative; the consumer-side flow
shape is SemTeams-team's call.

### Gap 2: scenario-generator output format [STAKEHOLDER]

The `evidence_contract:` block in scenario-generator output is one
authorship pattern. Alternatives:

- **Option A**: Block embedded in scenario YAML (the sketch).
- **Option B**: Separate file per scenario referenced by scenario
  ID.
- **Option C**: Triples emitted directly to graph, no YAML
  intermediary.

**Lean**: Option A for consistency with SemSpec's
`scenarios.evidence.yaml` pattern (per testevidence-semspec-sketch
Gap 1); aligns SemTeams + SemSpec authorship surface.
**[STAKEHOLDER]** — SemTeams-team confirms when they re-spec.

### Gap 3: Dev-loop "I think it's ready for evidence" signal [STAKEHOLDER]

How does the dev-loop signal completion such that Coordinator
schedules the EvidenceRun?

- **Option A**: Dev-loop emits a `chain.dev_complete=true` triple
  on the chain entity; Coordinator rule fires on the triple.
- **Option B**: Dev-loop calls a `request_evidence_run` tool;
  Coordinator dispatches.
- **Option C**: Dev-loop reaches its terminal Lifecycle phase;
  Coordinator's transition rule fires.

**Lean**: Option C — leverages the existing Lifecycle Participant
framework (chain entity transitions to `dev_complete`; Coordinator
rule fires on transition). Clean, no new tools. **[STAKEHOLDER]** —
SemTeams-team confirms.

### Gap 4: Multi-scenario chains (one chain implementing N scenarios) [STAKEHOLDER]

A requirement might decompose into multiple scenarios that share
implementation. Does the dev-loop run as:

- **Option A**: One chain per scenario; N chains per requirement.
  N EvidenceRuns per chain.
- **Option B**: One chain per requirement; N EvidenceRuns scheduled
  after the single chain's completion.

**Lean**: Option A for clean attribution (which scenario failed →
which chain produced the failing implementation). **[STAKEHOLDER]**
— SemTeams-team's call on dev-loop granularity.

### Gap 5: Reviewer prompt design — agent-cost framing [STAKEHOLDER]

Reviewer adjudication is graph-query based (per §Step 5). The
reviewer's persona prose needs to explicitly express:

- "Query EvidenceRun entities; don't ask the dev-loop to re-prove
  things in dev tier."
- "Cross-tier rollup is YOUR responsibility (product policy); apply
  the rules per the requirement's qa_level."
- "If an EvidenceRun is missing, request a re-schedule via
  Coordinator; do not infer outcomes from absence."

Per [[feedback_persona_prose_needs_decision_criteria]], each
sentence carries a when-to-use criterion. **[STAKEHOLDER]** —
SemTeams-team owns the reviewer prompt redesign.

### Gap 6: Cross-tier rollup for dev-via-spec — different from SemSpec? [STAKEHOLDER]

SemSpec's rollup is "qa_level=production requires unit + integration
+ e2e all satisfied" (per testevidence-semspec-sketch §Step 5).
SemTeams dev-via-spec rollup may differ:

- **Option A**: Same as SemSpec — qa_level mapping.
- **Option B**: Per-requirement rollup based on scenario-generator's
  output (the scenarios IT emitted define the rollup).
- **Option C**: Reviewer agent's judgment per-requirement, no
  declarative rollup.

**Lean**: Option B (declarative; scenario-generator IS the policy
authority for its requirement). Reviewer applies the rollup; no
implicit defaults. **[STAKEHOLDER]** — SemTeams-team's call on
dev-via-spec's policy model.

### Gap 7: Token-cost telemetry for the agent-cost cut [STAKEHOLDER]

The agent-cost framing is empirical: "dev-loop chains DO NOT burn
tokens trying to prove cross-tier evidence." Validating this
requires telemetry. Post-migration:

- Per-chain token counters (already exist in semteams metrics).
- Per-loop token counters segmented by phase
  (implementation-vs-verification).
- Comparison: pre-substrate token cost per requirement vs post-
  substrate.

If post-substrate dev-loops show LOWER token cost per requirement,
the agent-cost cut is working as designed. If equal or higher, the
prompt redesign needs iteration. **[STAKEHOLDER]** — SemTeams ops
team's call on telemetry shape + acceptance threshold.

### Gap 8: Chain entity Lifecycle Participant — confirmed for dev-loop?

(Cross-reference: sandbox-semteams-sketch Gap 1.)

The round 4 `lifecycle.Ref` owner check requires the dev-loop chain
entity to be a Lifecycle Participant. SemTeams already uses ADR-049
Lifecycle for some chain state per beta.89; **[STAKEHOLDER]**
confirm dev-loop chains specifically are (or will be) Participants
by ADR-052 land time.

If they're not, the ephemeral tool-execution-within-dev-loop pattern
(§Step 6) can't use chain as owner. Workaround would be making the
EvidenceRun the owner instead — possible but more coupled.

## What this sketch validates about the substrate shape

- ✅ **The agent-cost cut works.** dev-via-spec was parked because
  agents couldn't reason about cross-tier evidence; the substrate
  exposes tier + env-profile as typed contracts that prompts can
  reference directly. The cut is structurally enforced (different
  sandbox leases for dev vs verification) and prompt-renderable.
- ✅ **Contract-vs-Run separation** maps cleanly to dev-via-spec's
  cadence: one EvidenceContract per scenario; N EvidenceRuns per
  scenario per chain revision (initial dev-loop completion, retry
  after fix, etc.).
- ✅ **`lifecycle.Ref` owner check** lets dev-loop chains own
  ephemeral tool-execution leases — the same pattern as
  sandbox-semteams-sketch §Step 2.
- ✅ **Reviewer's graph-query adjudication** reads typed
  EvidenceRun outcomes; no dev-loop output parsing for evidence.
- ⚠️ **Most consumer-side specifics are SemTeams-team's call** to
  re-spec post-ADR-052 (Gaps 1-7). The substrate-side contract is
  what's being validated by this sketch.
- ⚠️ **Chain entity Lifecycle Participant readiness** (Gap 8) is
  a precondition; confirm before ADR-052 implementation.

## Cross-references

- [`sandbox-substrate.md`](sandbox-substrate.md) — parent proposal
- [`sandbox-substrate-design-exercise.md`](sandbox-substrate-design-exercise.md) — substrate shape resolutions
- [`sandbox-semteams-sketch.md`](sandbox-semteams-sketch.md) — sandbox side of the same migration (chain Lifecycle Participant + ephemeral tool exec)
- [`testevidence-semspec-sketch.md`](testevidence-semspec-sketch.md) — companion testevidence migration on the SemSpec side
- [[feedback_persona_prose_needs_decision_criteria]] — applies to dev-loop and reviewer prompt redesign
- [[feedback_tool_signature_intent_not_structure]] — applies to scenario-generator output design

---

**Note on parked-on-substrate consumers**: this sketch is
intentionally written before SemTeams has re-specced dev-via-spec
on the new substrate. The framework substrate is the gating
artifact; SemTeams' detailed flow re-spec follows. Per the
[[feedback_greenfield_cross_product_break_now]] parked-on-substrate
refinement, we're not waiting for SemTeams to demonstrate need by
working around the substrate's absence — we're shipping the
substrate so they can ship their re-spec immediately.

If you (SemTeams team) read this sketch and find the substrate-
side contract doesn't unblock your re-spec cleanly, that's a
critical signal — file feedback before ADR-052 lands. The whole
point of the parked-on-substrate framing is that THIS sketch is
the right time to catch substrate-shape mismatches.

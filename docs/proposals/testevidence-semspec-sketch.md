# SemSpec Consumer Sketch — `pkg/testevidence`

**Status**: Working draft — 2026-05-31. Pre-ADR (ADR-052).
Companion to [`sandbox-substrate.md`](sandbox-substrate.md),
[`sandbox-substrate-design-exercise.md`](sandbox-substrate-design-exercise.md),
and [`sandbox-semspec-sketch.md`](sandbox-semspec-sketch.md).

**Scope**: Maps SemSpec's current Cucumber tags + qa_level policy
path onto `pkg/testevidence` + the Cucumber renderer plugin.
Validates the Contract-vs-Run separation (round 2) against
SemSpec's real PR/nightly/release-candidate run cadence.
**Stakeholder input needed** on items marked **[STAKEHOLDER]**
before ADR-052 drafts.

## Current state (what SemSpec does today)

SemSpec scenarios are authored as Cucumber/Gherkin `.feature` files
with tags carrying intent (`@integration`, `@slow`,
`@network-required`). The QA-reviewer agent loop reads spec
frontmatter (`qa_level`) + Cucumber tags + scenario outcomes (after
test execution) to produce release-readiness verdicts.

Roughly (paraphrased; **[STAKEHOLDER]** confirm):

```gherkin
# specs/weather-alerting-v1/scenarios.feature
@integration @slow @network-required
Scenario: Weather alert publishes within 5 seconds of trigger
  Given a configured weather sensor at "denver.weather.alpha"
  When a temperature anomaly is detected
  Then an alert message is published to "alerts.weather.denver"
  And the alert arrives within 5 seconds
```

```yaml
# spec frontmatter
---
spec_id: weather-alerting-v1
qa_level: production
qa:
  profile: semspec.qa.go-node-postgres  # (after sandbox-semspec-sketch migration)
  scenarios:
    - file: scenarios.feature
      tier: integration
---
```

QA-reviewer flow (paraphrased):

1. Read spec frontmatter; resolve `qa_level=production` → strict
   review prompts.
2. Read scenario `.feature` files; parse Cucumber tags as text.
3. After test execution, parse Cucumber output (JSON / JUnit XML)
   for per-scenario pass/fail.
4. Cross-reference: did all scenarios tagged `@integration` pass
   in the integration test run? Did any fail with `@flaky`?
5. Emit release-readiness verdict based on the cross-reference +
   qa_level policy.

### Pain points motivating the migration

Per the workenv-substrate proposal §Background:

1. **Cucumber tags are text-shaped, not typed.** "Did all
   integration-tagged scenarios pass" requires text matching;
   reviewers can't reason about (scenario, tier, env-profile)
   tuples without parsing tag bags.
2. **Per-run cadence not modeled.** PR runs, nightly runs,
   release-candidate runs, retry runs — all produce the same
   Cucumber output shape; cross-run reasoning ("did this scenario
   pass on the release-candidate but fail on PR?") requires
   external bookkeeping.
3. **No structural link from scenario to env-profile.** Cucumber
   tags say *what* tier; they don't say *which env-profile* the
   scenario verifies against. The link is implicit in CI
   configuration; QA-reviewer can't introspect it.
4. **Assertion outcomes don't carry provenance.** "This assertion
   passed in this run" is captured in Cucumber JSON; "in which env,
   with which sandbox lease, at what time, with what stdout" is
   ad-hoc per CI run.
5. **Cross-tier rollup is hand-rolled per spec.** "qa_level=production
   requires unit + integration + e2e all satisfied" is implemented
   per-spec in QA-reviewer prompts, not as composable policy over a
   typed contract.

## Proposed migration

### Step 1: extract scenario evidence into `testevidence.EvidenceContract`

Move from "Cucumber tags as the spec" to "EvidenceContract as the
spec, Cucumber tags as one rendered projection." Each scenario in
the spec gains an `EvidenceContract` block declaring tier, facets,
required assertions, sandbox profile, and verification commands:

```yaml
# specs/weather-alerting-v1/scenarios.evidence.yaml
# Authored alongside the .feature file; framework projects to
# Cucumber tags via the testevidence renderer.

evidence_contracts:
  - scenario_id: weather.alert.5s-publish-latency
    tier: testevidence.tier.integration
    facets: [slow, network-required]
    environment_profile_ids:
      - semspec.qa.go-node-postgres
    required_assertions:
      - alert_published_to_correct_topic
      - alert_payload_well_formed
      - alert_latency_within_5s
    verification:
      - "task test:weather-alerting -- --scenario=5s-publish-latency --report=json"
```

The `.feature` file becomes a *projection* of the EvidenceContract
— the testevidence Cucumber renderer auto-generates the tag bag
from the contract. **Spec authors stop hand-writing tags**:

```gherkin
# scenarios.feature (post-migration; auto-rendered from EvidenceContract)
@integration @slow @network-required  # ← auto-emitted from EvidenceContract
Scenario: Weather alert publishes within 5 seconds of trigger
  # ... scenario body authored by humans; tags managed by renderer
```

### Step 2: write the Cucumber renderer plugin

```go
// semspec-repo: internal/testevidence/renderers/cucumber.go

type Renderer struct {
    fs filesystem.Writer
}

func (r *Renderer) Name() string { return "cucumber" }

func (r *Renderer) Capabilities() testevidence.RendererCapabilities {
    return testevidence.RendererCapabilities{
        SupportsTagProjection: true,
        // Cucumber renderer only projects; it doesn't provision
        // or collect (sandbox + EvidenceRun do that).
    }
}

func (r *Renderer) Render(ctx context.Context, contract testevidence.EvidenceContract) (testevidence.Artifact, error) {
    // Build the Cucumber tag bag from the typed contract:
    tags := []string{}
    tierShort := strings.TrimPrefix(contract.Tier, "testevidence.tier.")
    tags = append(tags, "@"+tierShort)
    for _, facet := range contract.Facets {
        tags = append(tags, "@"+facet)
    }
    
    return testevidence.Artifact{
        RendererName: r.Name(),
        Content:      []byte(strings.Join(tags, " ")),
        ContentType:  "text/plain",
        Metadata: map[string]string{
            "feature_file": fmt.Sprintf("specs/%s/scenarios.feature", contract.ScenarioID),
            "scenario_id":  contract.ScenarioID,
        },
    }, nil
}
```

Note: Cucumber renderer is **testevidence-only** (no `Provision`,
no `Release` — see design exercise §Q-W2 Gemini round 3 correction
that Cucumber was incorrectly listed under sandbox renderers).
testevidence renderers are projection-only; the rendered artifacts
(Cucumber tags in `.feature` files) are consumed externally by
Cucumber CLI.

### Step 3: testevidence.Manager schedules EvidenceRuns

When SemSpec CI fires (PR / nightly / retry / release-candidate),
the build process calls `testevidence.Manager.ScheduleRun` for
each scenario:

```go
// semspec-repo: internal/qa/runner.go

func (r *QARunner) RunScenarios(ctx context.Context, spec Spec, cadence Cadence) error {
    for _, evidenceContract := range spec.EvidenceContracts {
        run, err := r.testevidenceManager.ScheduleRun(ctx, testevidence.ScheduleRequest{
            Contract: evidenceContract,
            RunID:    generateRunID(cadence),  // e.g., "pr-1234", "nightly-2026-05-31", "rc-v1.5.0"
        })
        if err != nil {
            return err
        }
        // ScheduleRun returns immediately; EvidenceRun lifecycle drives
        // through scheduled → bound → verifying → satisfied/failed/skipped
        // independently. QARunner waits on a watch.
        r.activeRuns = append(r.activeRuns, run.EntityID())
    }
    
    return r.waitForCompletion(ctx, r.activeRuns)
}
```

EvidenceRun's lifecycle drives the sandbox lease + verification
flow per the design exercise §Q-T1 EvidenceRun flow:

1. **scheduled**: contract resolved; run entity created.
2. **bound**: `sandbox.Manager.Lease(profile_id,
   LeaseOptions{Holder: "evidence_run-<run_id>", Audit: AuditFull})`
   acquires the sandbox; renderer provisions; ProbeReady returns
   `Ready: true`.
3. **verifying**:
   - For inline-exec renderers (devcontainer, docker-compose):
     EvidenceRun calls `handle.Exec(cmd)` per `verification:`
     command; collects results directly.
   - For scheduled-run renderers (qa.yml/act, used by SemSpec's
     primary CI path): EvidenceRun polls
     `collector.CollectResults(handleRef, runRef)` with `runRef =
     {EvidenceRunID: run.ID, Correlation: {gh_label:
     "evidence_run-pr-1234"}}` on ~30s cadence until results
     arrive.
4. **satisfied/failed**: per-assertion outcomes stamp triples on
   the EvidenceRun entity (see §Step 4 below); lifecycle transitions
   to terminal.
5. **skipped**: if `qa_level=draft` policy excludes this tier
   (e.g., draft skips e2e).

### Step 4: assertion outcomes stamp on EvidenceRun

Per-assertion provenance triples per the design exercise §Q-T2:

```
evidence_run:c360.semspec.testevidence.integration.evidence_run.weather-alert-5s-publish-latency-pr-1234

  evidence_run.scenario_id              weather.alert.5s-publish-latency
  evidence_run.tier                     testevidence.tier.integration
  evidence_run.contract_ref             weather-alerting-v1#scenario:weather.alert.5s-publish-latency
  evidence_run.run_id                   pr-1234
  evidence_run.environment_profile_id   semspec.qa.go-node-postgres
  evidence_run.sandbox_lease_id         lease-abc123
  evidence_run.scheduled_at             2026-05-31T15:42:00Z
  
  evidence_run.assertion.alert_published_to_correct_topic.outcome      satisfied
  evidence_run.assertion.alert_published_to_correct_topic.observed_at  2026-05-31T15:43:12Z
  evidence_run.assertion.alert_published_to_correct_topic.duration_ms  1240
  evidence_run.assertion.alert_published_to_correct_topic.evidence     {"published": true, "topic": "alerts.weather.denver"}
  
  evidence_run.assertion.alert_latency_within_5s.outcome               failed
  evidence_run.assertion.alert_latency_within_5s.observed_at           2026-05-31T15:43:17Z
  evidence_run.assertion.alert_latency_within_5s.duration_ms           5320
  evidence_run.assertion.alert_latency_within_5s.evidence              {"latency_ms": 5320, "threshold_ms": 5000}
```

### Step 5: QA-reviewer reads EvidenceRun, not Cucumber output

The QA-reviewer agent loop's release-readiness verdict becomes a
graph predicate query against EvidenceRun entities. No more
Cucumber JSON parsing; no more cross-referencing tags-to-outcomes
by hand.

```go
// semspec-repo: internal/qa/reviewer.go (after migration)

func (r *QAReviewer) ReleaseVerdict(ctx context.Context, spec Spec, releaseRunID string) (Verdict, error) {
    // qa_level=production: production gates compose on top of framework primitives.
    requirements := r.productionGates.RequiredTiers(spec.QALevel)
    // e.g., for production: [unit, integration, e2e]; for draft: [unit]
    
    for _, tier := range requirements {
        // Predicate query: for this spec's scenarios at this tier,
        // for the named release-candidate run, did all evidence_runs
        // reach satisfied?
        runs, err := r.graphClient.QueryEvidenceRuns(ctx, EvidenceRunQuery{
            ScenarioIDs: spec.ScenarioIDs(),
            Tier:        tier,
            RunID:       releaseRunID,
        })
        if err != nil {
            return Verdict{}, err
        }
        
        for _, run := range runs {
            if run.Outcome != OutcomeSatisfied {
                return Verdict{
                    Approved: false,
                    Reason: fmt.Sprintf(
                        "evidence_run %s tier=%s did not reach satisfied (outcome=%s)",
                        run.ScenarioID, tier, run.Outcome,
                    ),
                }, nil
            }
        }
    }
    
    return Verdict{Approved: true}, nil
}
```

**Cross-tier rollup is product policy** (per design exercise §New
6). SemSpec's `qa_level=production` mapping to "requires unit +
integration + e2e satisfied" lives in semspec code, not in the
framework. Different products (SemTeams, future products) can
compose different rollup rules over the same typed EvidenceRun
outcomes.

## Acceptance criteria for the SemSpec testevidence migration

After the migration lands:

- [ ] All semspec scenarios have a corresponding `EvidenceContract`
  in `scenarios.evidence.yaml` (or equivalent format).
- [ ] Cucumber `.feature` files have their tags auto-rendered from
  EvidenceContracts; no hand-authored tag bags.
- [ ] `testevidence.Manager.ScheduleRun` is called per scenario per
  CI cadence (PR / nightly / retry / release-candidate).
- [ ] EvidenceRun lifecycle drives the sandbox lease + verification
  flow without QARunner needing to coordinate.
- [ ] Per-assertion outcomes stamp on EvidenceRun entities with the
  provenance fanout from §Step 4.
- [ ] QA-reviewer reads EvidenceRun entities via graph predicate
  query; no Cucumber JSON parsing.
- [ ] `qa_level=production` is implemented as a product policy
  layer (RequiredTiers + product-policy gates), composing on
  framework primitives.
- [ ] At least one spec has end-to-end migrated: EvidenceContract
  → ScheduleRun → sandbox lease (via sandbox-semspec-sketch
  renderer) → verification execution → assertion triples →
  ReleaseVerdict.
- [ ] Existing Cucumber CLI integration unchanged from the
  operator's perspective: `task test:cucumber` still runs the
  scenarios using the rendered `.feature` files.

## Open gaps surfaced by this sketch

### Gap 1: scenarios.evidence.yaml authoring vs Cucumber .feature authoring [STAKEHOLDER]

The migration introduces a new file (`scenarios.evidence.yaml`)
that authors maintain alongside `.feature` files. Two authorship
patterns:

- **Option A**: authors write `scenarios.evidence.yaml`; renderer
  generates `.feature` tag bags. Cucumber scenario body still
  authored in `.feature` by humans.
- **Option B**: authors write `.feature` files with structured
  comments (e.g., `# evidence: tier=integration, profile=X`);
  build pass extracts evidence contracts from the comments. Less
  intrusive but more fragile.
- **Option C**: a new combined format (e.g., `.scenario.yaml` that
  contains both contract + Gherkin body in one file). Most
  disruptive; cleanest long-term.

**Lean**: Option A for v1 (clean separation; one file per
concern); revisit if authors find dual-file authorship frictional.
**[STAKEHOLDER]** — semspec authoring-experience team's call.

### Gap 2: BMAD persona / QA-reviewer prompt impact [STAKEHOLDER]

QA-reviewer is currently prompted to "parse Cucumber output and
cross-reference qa_level." Post-migration, the prompt should be
"query EvidenceRun entities and apply qa_level rollup rules." The
prompt redesign is product work (SemSpec-side), but it surfaces a
sub-question: do BMAD personas need awareness of the framework
substrate, or do they stay at the application-policy level?

**Lean**: personas stay application-policy; the QA-reviewer's
graph-query tooling is a framework primitive the persona invokes.
Framework abstraction shields personas from substrate changes.
**[STAKEHOLDER]** — confirm direction.

### Gap 3: Existing Cucumber test results — backfill or fresh-start?

Pre-migration test runs produced Cucumber JSON outputs. Post-
migration, those become EvidenceRun entities. Backfill options:

- **A. Fresh-start**: pre-migration runs aren't retroactively
  represented in EvidenceRun; release-readiness only references
  post-migration runs.
- **B. Backfill script**: parse historical Cucumber JSON; emit
  EvidenceRun entities with reconstructed provenance. Lossy
  (correlation hints may be missing) but preserves history.
- **C. Hybrid**: fresh-start for QA-reviewer's release-readiness
  queries; archive historical Cucumber JSON outside graph for
  operator observability.

**Lean**: Option C for v1 (no backfill effort; historical data
preserved outside graph). **[STAKEHOLDER]** — semspec ops team's
call on what history matters.

### Gap 4: facets vocabulary scope [STAKEHOLDER]

The design exercise §Q-T3 decided tiers are closed
(unit/integration/smoke/e2e), facets are open-ended operator-named
tags. SemSpec's current Cucumber tag usage includes:

- `@slow` — runtime > 10s
- `@network-required` — needs internet
- `@flaky-quarantine` — known-flaky, results don't gate
- `@gpu-required` — needs CUDA-capable runner

Question: does any existing tag NEED to become a typed tier (vs
staying as facet)? E.g., `@flaky-quarantine` might want first-
class typed treatment because rollup rules need to know to exclude
it from gating.

**Lean**: all current SemSpec tags map to facets in v1; if rollup
rules need typed predicates over facets (e.g., "skip facets
matching `flaky-*`"), the predicate logic lives in product policy.
**[STAKEHOLDER]** — semspec team confirms.

### Gap 5: Multi-environment scenarios (one scenario, multiple env-profiles) [STAKEHOLDER]

Some scenarios need to be verified against multiple env-profiles
(e.g., the same weather-alert scenario verified against both
postgres-17 and postgres-15 to validate compatibility). The
EvidenceContract sketch has `environment_profile_ids[]` as a list
suggesting this is supported — but the semantics need clarifying:

- **Option A**: One EvidenceRun per (scenario, tier, profile)
  combination. Three profile_ids = three EvidenceRuns per cadence.
- **Option B**: One EvidenceRun per (scenario, tier) — all profiles
  must satisfy for run to be `satisfied`; one failure = `failed`.
- **Option C**: Per-profile sub-assertions within one EvidenceRun.

**Lean**: Option A for v1 (cleanest semantics; cross-profile
reasoning is a rollup-policy concern). **[STAKEHOLDER]** —
semspec team's call on which fits their scenario authoring.

### Gap 6: ScheduleRun cadence — who triggers and when

`testevidence.Manager.ScheduleRun` requires a `RunID` (the cadence
discriminator). Who generates run IDs and when?

- PR runs: triggered by GH Actions webhook on PR open/sync. RunID
  = `pr-{number}-{sha-short}`.
- Nightly: triggered by cron. RunID = `nightly-{date}`.
- Retry: triggered by operator command. RunID =
  `retry-{base-run}-{seq}`.
- Release-candidate: triggered by tag push. RunID = `rc-{tag}`.

Run IDs need to be globally unique within a (scenario, tier) tuple
to avoid EvidenceRun entity-ID collisions. The framework doesn't
own the trigger source; product owns trigger + RunID generation.

Documenting this explicitly so SemSpec's CI integration knows the
contract.

### Gap 7: EvidenceRun retention TTL — already flagged in design exercise

The design exercise's Open Question 3 raised retention; flagging
again here as a SemSpec-specific data point: estimate volume.

- ~50 specs × ~5 scenarios/spec × 4 tiers = ~1K active
  EvidenceContracts.
- PR cadence: ~10 PRs/day × 1K scenarios = ~10K EvidenceRuns/day.
- Nightly + RCs: ~1K runs/day.
- **~11K EvidenceRuns/day** × 30 days = ~330K entities/month.

If retention is indefinite, that's ~4M EvidenceRun entities/year
in graph storage. Probably fine for KV at that volume (each entity
is ~5KB → ~20GB/year), but worth confirming.

**Lean**: indefinite retention for full-audit runs; minimal-audit
runs (less common in SemSpec; mostly applies to SemTeams' tool-
exec leases) get 1-week TTL. **[STAKEHOLDER]** — semspec ops
team's call on storage budget.

## What this sketch validates about the substrate shape

- ✅ Contract-vs-Run separation (round 2) cleanly handles
  SemSpec's PR/nightly/retry/release-candidate cadence — one
  EvidenceContract, N EvidenceRuns per cadence.
- ✅ Per-assertion provenance triples (round 2) preserve cross-run
  reasoning ("did this scenario pass on RC but fail on PR?") that
  was hand-rolled before.
- ✅ Tier-as-typed-vocabulary (Q-T3) replaces Cucumber tag bags
  for rollup policy; facets handle the rest of the tag surface.
- ✅ Cucumber renderer (§Q-T4) projects tags from the typed
  contract; auto-rendering removes hand-authored tag bags.
- ✅ Cross-tier rollup as product policy (§New 6) cleanly separates
  framework primitives from SemSpec-specific `qa_level` policy.
- ⚠️ scenarios.evidence.yaml authoring pattern (Gap 1) needs
  stakeholder input before locking.
- ⚠️ Multi-environment scenarios (Gap 5) need semantic clarification
  for v1.
- ⚠️ EvidenceRun volume (Gap 7) suggests retention policy is
  load-bearing for storage budget.

## Cross-references

- [`sandbox-substrate.md`](sandbox-substrate.md) — parent proposal
- [`sandbox-substrate-design-exercise.md`](sandbox-substrate-design-exercise.md) — substrate shape resolutions
- [`sandbox-semspec-sketch.md`](sandbox-semspec-sketch.md) — sandbox side of the same migration (the qa.yml/act renderer)
- [`testevidence-semteams-sketch.md`](testevidence-semteams-sketch.md) — companion testevidence migration on the SemTeams side (parked-on-substrate consumer)

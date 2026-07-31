# SemSpec Consumer Sketch — `pkg/sandbox`

**Status**: Working draft — 2026-05-31. Pre-ADR (ADR-052).
Companion to [`sandbox-substrate.md`](sandbox-substrate.md) and
[`sandbox-substrate-design-exercise.md`](sandbox-substrate-design-exercise.md).

**Scope**: Maps SemSpec's current `qa.yml`-render path onto
`pkg/sandbox` + the `qa.yml/act` renderer plugin. Validates the
substrate shape against a real consumer migration. Surfaces gaps
the design exercise didn't catch. **Stakeholder input needed** on
the items marked **[STAKEHOLDER]** before ADR-052 drafts.

## Current state (what SemSpec does today)

SemSpec specs declare runtime + verification needs in spec
frontmatter; a SemSpec build pass renders per-spec `.github/workflows/qa.yml`
files that GitHub Actions or local `act` execute. The QA-reviewer
agent loop reads the `qa.yml` body as text to decide whether a
spec is releasable; `qa_level` policy is applied as a free-form
string in the frontmatter.

Roughly (paraphrased from semspec's docs; **[STAKEHOLDER]** confirm):

```yaml
# spec frontmatter (in semspec repo)
---
spec_id: weather-alerting-v1
qa_level: production
qa:
  runtime:
    go_version: "1.26"
    services: [nats, postgres]
    network: restricted
  verify:
    - go test ./...
    - npm run check
---
```

A semspec build step transforms each spec's `qa:` block into
`.github/workflows/qa-{spec_id}.yml`:

```yaml
# .github/workflows/qa-weather-alerting-v1.yml (rendered)
name: QA — weather-alerting-v1
on:
  pull_request:
    paths: ['specs/weather-alerting-v1/**']
jobs:
  qa:
    runs-on: ubuntu-latest
    services:
      nats:
        image: nats:2.14
      postgres:
        image: postgres:17
    steps:
      - uses: actions/checkout@v4
      - uses: actions/setup-go@v5
        with: { go-version: '1.26' }
      - run: go test ./...
      - run: npm run check
```

QA-reviewer agent reads this rendered YAML, plus the
spec-frontmatter `qa_level`, and produces a release-readiness
verdict. `qa_level=production` triggers stricter review prompts;
`qa_level=draft` skips heavy review.

### Pain points motivating the migration

Per the workenv-substrate proposal §Background:

1. **`qa.yml` is frozen text.** `qa_level=production` review can't
   introspect the structured runtime requirements — the reviewer
   parses YAML or trusts the spec frontmatter. The contract is
   downstream of the renderer.
2. **Per-spec YAML files duplicate runtime declarations.** Two
   specs sharing the same runtime + service set still get two
   nearly-identical YAML files; cross-spec reasoning ("which specs
   need a Postgres? do they all share a config?") is text-matching.
3. **Render targets are locked to GitHub Actions.** Running the
   same verification locally requires `act` (or running steps by
   hand). docker-compose-based local verification isn't a render
   target today; adding it duplicates the YAML logic in a separate
   render path.
4. **`qa_level` policy can't compose across products.** SemTeams'
   Coordinator may want to know "this spec requires runtime X" for
   pre-routing decisions; today, Coordinator would have to parse
   the rendered YAML or trust spec-frontmatter shape.

## Proposed migration

### Step 1: extract runtime declarations into `sandbox.CapabilityContract` profiles

Move runtime declarations out of spec frontmatter and into named
profiles in `configs/sandbox-profiles.yaml`. Specs reference
profile-IDs by name. Schemas are shared across specs that share
runtime needs.

```yaml
# configs/sandbox-profiles.yaml (semspec-owned fragment)
profiles:
  semspec.qa.go-node-postgres:
    schema_version: "1"
    owner: "semspec-team"
    requirements:
      tools:
        - {class: tool.go, version: "1.26"}
        - {class: tool.node, version: "20"}
      services: [service.nats, service.postgres]
      network: restricted
      filesystem: workspace-write
      secrets: []
    realization:
      renderer: github-actions-act
      config:
        workflow_path: .github/workflows/{spec_id}-qa.yml
        runner_label: ubuntu-latest
        nats_image: nats:2.14
        postgres_image: postgres:17
    lease:
      mode: ephemeral         # CI runs are one-shot
      audit: full             # production qa needs full audit history
      ttl: 30m                # CI run cap
```

**Spec frontmatter shrinks to**:

```yaml
---
spec_id: weather-alerting-v1
qa_level: production
qa:
  profile: semspec.qa.go-node-postgres
  verify:
    - go test ./...
    - npm run check
---
```

The `verify:` block stays in the spec because **verification
commands are testevidence-layer, not sandbox-layer** (see
testevidence-semspec-sketch.md for the testevidence side).

### Step 2: write the `qa.yml/act` renderer plugin

SemSpec ships a renderer that translates `sandbox.CapabilityContract`
+ per-renderer `config` → `.github/workflows/*.yml`. It implements
`sandbox.Renderer` from the design exercise §Q-W2:

```go
// semspec-repo: internal/sandbox/renderers/qaml.go

type Renderer struct {
    fs filesystem.Writer
}

func (r *Renderer) Name() string { return "github-actions-act" }

func (r *Renderer) Capabilities() sandbox.RendererCapabilities {
    return sandbox.RendererCapabilities{
        SupportsInlineExec:    false,  // qa.yml runs via GH Actions / act
        SupportsScheduledRun:  true,   // ← must implement ResultCollector
        SupportsLeaseRefresh:  false,
    }
}

func (r *Renderer) Render(ctx context.Context, contract sandbox.CapabilityContract) (sandbox.Artifact, error) {
    var cfg QAMLConfig
    if err := json.Unmarshal(contract.Realization.Config, &cfg); err != nil {
        return sandbox.Artifact{}, fmt.Errorf("qaml renderer: invalid config: %w", err)
    }
    yaml, err := r.renderYAML(contract.Requirements, cfg)
    if err != nil {
        return sandbox.Artifact{}, err
    }
    return sandbox.Artifact{
        RendererName: r.Name(),
        Content:      yaml,
        ContentType:  "application/x-yaml",
        Metadata:     map[string]string{"workflow_path": cfg.WorkflowPath},
    }, nil
}

func (r *Renderer) Provision(ctx context.Context, artifact sandbox.Artifact) (sandbox.HandleRef, error) {
    // Write the rendered YAML to the workflow path. Provisioning
    // here is "the YAML is on disk and committable"; GH Actions
    // picks it up via repo state.
    path := artifact.Metadata["workflow_path"]
    if err := r.fs.WriteFile(path, artifact.Content, 0644); err != nil {
        return sandbox.HandleRef{}, err
    }
    return sandbox.HandleRef{
        LeaseID:      generateLeaseID(),
        RendererName: r.Name(),
        Payload:      mustJSON(map[string]string{"workflow_path": path}),
    }, nil
}

func (r *Renderer) ProbeReady(ctx context.Context, ref sandbox.HandleRef) (*sandbox.ReadyResult, error) {
    // qa.yml is ready when the file exists; no further probing.
    return &sandbox.ReadyResult{
        Ready: true,
        State: sandbox.ReadyStateReady,
        Reason: "qa.yml written to repo",
    }, nil
}

func (r *Renderer) Release(ctx context.Context, ref sandbox.HandleRef) error {
    // Leave the YAML in place (it's source-controlled). Lease release
    // is a no-op on the renderer side; lifecycle state cleans up.
    return nil
}

func (r *Renderer) Acquire(ctx context.Context, ref sandbox.HandleRef) (sandbox.Handle, error) {
    return &qamlHandle{ref: ref, renderer: r}, nil
}

// ResultCollector implementation (required because SupportsScheduledRun=true)
func (r *Renderer) CollectResults(ctx context.Context, ref sandbox.HandleRef, runRef sandbox.RunRef) ([]sandbox.AssertionResult, error) {
    // Poll GitHub Actions API for the workflow run matching runRef.
    // Returns (nil, nil) when run is in progress; results when complete.
    workflowPath := mustExtract(ref.Payload, "workflow_path")
    correlationLabel := runRef.Correlation["gh_label"]
    run, err := r.ghClient.GetLatestRun(ctx, workflowPath, correlationLabel)
    if err != nil {
        return nil, err
    }
    if run == nil || !run.Completed {
        return nil, nil  // polling-shaped: not done yet
    }
    return r.parseAssertionResults(run), nil
}
```

### Step 3: QA-reviewer reads typed `CapabilityContract`, not YAML

The QA-reviewer agent loop currently parses rendered YAML to
determine `runs-on`, services, etc. After migration, it reads the
typed `sandbox.CapabilityContract` (resolved from the spec's
profile ID via `sandbox.Catalog.Resolve`) and adjudicates against
typed fields:

```go
// semspec-repo: internal/qa/reviewer.go (paraphrased)

func (r *QAReviewer) Review(ctx context.Context, spec Spec) (Verdict, error) {
    contract, err := r.catalog.Resolve(spec.QA.Profile)
    if err != nil {
        return Verdict{}, fmt.Errorf("unknown qa profile %q: %w", spec.QA.Profile, err)
    }
    
    // qa_level=production gate (product policy):
    if spec.QALevel == QALevelProduction {
        if err := r.productionGates.Check(contract); err != nil {
            return Verdict{Approved: false, Reason: err.Error()}, nil
        }
    }
    
    // Reviewer can now reason about typed contract:
    // - which tools at which versions
    // - which services
    // - which secrets requested (if any flagged for production)
    // - admission policy (network/filesystem/secrets enums)
    // - lease mode (full vs minimal audit; production = full)
    
    return Verdict{Approved: true}, nil
}
```

`qa_level=production` becomes a *product policy* layer that
composes admission rules on top of framework primitive gates per
the design exercise §Q-W4. SemSpec's policy:

```go
// semspec-repo: internal/qa/production_gates.go

var ProductionGates = ProductPolicy{
    AllowedNetworks:     []sandbox.NetworkPolicy{sandbox.NetworkRestricted, sandbox.NetworkAirgapped},
    AllowedFilesystems:  []sandbox.FilesystemPolicy{sandbox.FilesystemReadOnly, sandbox.FilesystemWorkspaceWrite},
    AllowedSecretsModes: []sandbox.SecretsPolicy{sandbox.SecretsReferenceOnly},
    RequiredAuditMode:   sandbox.AuditFull,
    MaxTTL:              30 * time.Minute,
}
```

### Step 4: testevidence consumes the lease

(Cross-reference: testevidence-semspec-sketch.md for the
testevidence-side flow.)

The `verify:` commands in the spec frontmatter become part of the
testevidence `EvidenceContract`. testevidence's `EvidenceRun`
leases the sandbox profile, injects the verification commands,
collects per-assertion results via `ResultCollector.CollectResults`
(since the qa.yml/act renderer is `SupportsScheduledRun=true`),
and stamps EvidenceRun outcome triples.

The QA-reviewer's release-readiness verdict is then a graph
predicate query: `evidence_run.scenario_id=X AND .tier=integration
AND .outcome=satisfied AND .run_id=<release-candidate-run>`.

## Acceptance criteria for the SemSpec sandbox migration

After the migration lands:

- [ ] All semspec specs reference `qa.profile: <profile-id>` instead
  of inline runtime declarations.
- [ ] At least one profile exists in
  `configs/sandbox-profiles.yaml` per distinct runtime shape (no
  per-spec profile duplication).
- [ ] `semspec build` no longer emits per-spec `qa.yml` files
  directly; instead invokes `sandbox.Manager.Lease(profile_id,
  LeaseOptions{Holder: "semspec-build", Audit: AuditFull})` which
  routes through the qa.yml/act renderer.
- [ ] QA-reviewer reads `sandbox.CapabilityContract` (typed) for
  policy decisions; no YAML-body parsing.
- [ ] `qa_level=production` admission is implemented as a product
  policy layer composing on framework primitive gates.
- [ ] At least one spec has end-to-end migrated: profile ID →
  sandbox lease → testevidence EvidenceRun → assertion outcomes
  → QA-reviewer verdict. Working PR-cycle proof.
- [ ] Existing GH Actions integration unchanged from the operator's
  point of view: the rendered `qa-{spec_id}.yml` files still land
  in `.github/workflows/` and still run on PR events.

## Open gaps surfaced by this sketch

Items the design exercise didn't address that emerged while writing
this:

### Gap 1: Spec frontmatter vs Catalog ownership boundary [STAKEHOLDER]

Where exactly does spec frontmatter end and Catalog config begin?
Two reasonable patterns:

- **Pattern A**: profile-IDs are entirely Catalog-owned; specs
  reference by ID only. New runtime shapes require a Catalog PR
  before the spec can land.
- **Pattern B**: specs can inline a `sandbox.CapabilityContract`
  literal in frontmatter as an alternative to profile reference.
  Catalog promotes commonly-used inline contracts via convention.

Pattern A is simpler but requires more cross-PR coordination.
Pattern B is more ergonomic for one-off specs but blurs the
ownership line.

**Lean (no consensus)**: Pattern A for v1; revisit if friction
emerges. **[STAKEHOLDER input needed]** — semspec team's call on
authorship workflow.

### Gap 2: GH Actions per-run correlation [STAKEHOLDER]

`ResultCollector.CollectResults(handleRef, runRef)` needs the
qa.yml/act renderer to find the right GH Actions run. Today,
correlation is implicit (latest run of this workflow on this
branch). With multi-tenant testevidence (PR + nightly + retry runs
in flight), explicit correlation is needed.

Options:
- **Workflow input**: testevidence injects `evidence_run_id` as a
  workflow_dispatch input or workflow concurrency key.
- **Git label/branch**: testevidence pushes a per-run branch or
  label; renderer correlates by label.
- **Commit SHA + workflow name**: assume one run per commit per
  workflow.

**Lean**: workflow_dispatch input with `evidence_run_id` for
explicit correlation. Falls back to commit SHA + workflow name for
PR-triggered runs. **[STAKEHOLDER]** semspec ops can confirm the
GH Actions usage pattern.

### Gap 3: Secret handling in GH Actions vs local `act`

The design exercise §New 7 documented two paths:

- **GitHub Actions**: renderer emits `${{ secrets.X }}` references;
  GH resolves at runtime via repo-secrets.
- **Local `act`**: operator-provided `.secrets` file path via
  `ACT_SECRETS_FILE` env config; renderer doesn't materialize.

Both paths need explicit renderer-side code that doesn't trip the
"never write secret values" discipline gate. **[STAKEHOLDER]** —
confirm SemSpec doesn't currently inline secret values in qa.yml
files (a quick grep for known secret refs in
`.github/workflows/` should establish baseline).

### Gap 4: Migration cadence — bulk-flip or per-spec opt-in?

After the renderer ships, how does SemSpec migrate existing specs?

- **Bulk-flip**: one PR migrates all specs simultaneously; tag
  v1.0.0 of semspec post-migration.
- **Per-spec opt-in**: specs migrate one-at-a-time over multiple
  PRs; both paths coexist until migration complete; flag
  removed after.

**Lean**: per-spec opt-in for safer rollout. Each migrating spec
proves the substrate against real verification flow before bulk
flip. **[STAKEHOLDER]** — semspec team picks based on their
release cadence.

### Gap 5: SemSpec build-tool integration — Catalog reload on profile changes

`configs/sandbox-profiles.yaml` is operator-config — when does it
reload? Options:

- **Startup-only**: profile changes require restart of any
  long-running semspec process. Simple but inconvenient.
- **Watch-mode**: framework watches the file and reloads
  on change. More moving parts; race conditions on partial writes.
- **Per-call resolution**: `sandbox.Catalog.Resolve` re-reads file
  each call. Latency overhead; bounded by file system cache.

**Lean**: startup-only for v1, watch-mode as a Phase 2 enhancement
if operators need it. **[STAKEHOLDER]** — confirm semspec's build
process won't suffer from startup-only.

### Gap 6: `qa.yml/act` renderer config schema validation

The catalog YAML allows arbitrary `realization.config` per renderer
(opaque `json.RawMessage` on the Go side). Each renderer needs to
publish its config schema for startup validation. Concrete shape:

```go
// pkg/sandbox/renderer.go (added during this sketch)

type Renderer interface {
    // ... existing methods
    
    // ConfigSchema returns a JSON schema describing the renderer's
    // expected Realization.Config shape. Used by Catalog at startup
    // to validate profile fragments; mismatch fails loudly.
    ConfigSchema() json.RawMessage
}
```

**This wasn't in the design exercise §Q-W2** — surfaces as a real
ergonomic gap when writing the qa.yml/act renderer. **Worth adding
to the canonical Renderer interface before consumer sketches lock.**

### Gap 7: Multi-renderer profiles (one logical env, multiple render targets)

Some profiles want to support multiple render targets — e.g., the
same runtime spec might render to `github-actions-act` for CI and
`devcontainer` for local development. The current catalog shape
has ONE `realization.renderer` per profile; supporting multiple
means either:

- **Profile-per-renderer**: `semspec.qa.go-node-postgres.ghactions`
  + `semspec.qa.go-node-postgres.devcontainer` as separate
  profiles with the same requirements.
- **Renderer list per profile**: `realization` becomes a list of
  renderer configs; lease specifies which.

**Lean**: profile-per-renderer for v1 (simpler, explicit). Profile
IDs use the `.{renderer}` suffix convention. Move to renderer
lists in Phase 2 if duplication becomes painful. **[STAKEHOLDER]**
— semspec team's call.

## What this sketch validates about the substrate shape

- ✅ `sandbox.CapabilityContract` covers the semspec qa-runtime
  needs (tools, services, network/filesystem/secrets, mode/audit).
- ✅ Catalog-as-operator-config (§New 1) fits semspec's
  profile-authorship workflow.
- ✅ Renderer interface (§Q-W2) supports the scheduled-run pattern
  semspec needs (qa.yml/act); `ResultCollector` is the right
  extension.
- ✅ Two-layer admission (framework primitive gates + product
  policy) supports `qa_level=production` cleanly.
- ⚠️ **`ConfigSchema()` missing from Renderer interface** (Gap 6) —
  feed back into the canonical interface.
- ⚠️ Multi-renderer profiles (Gap 7) need a decision before v1.

## Cross-references

- [`sandbox-substrate.md`](sandbox-substrate.md) — parent proposal
- [`sandbox-substrate-design-exercise.md`](sandbox-substrate-design-exercise.md) — substrate shape resolutions
- [`testevidence-semspec-sketch.md`](testevidence-semspec-sketch.md) — testevidence side of the same migration
- [`sandbox-semteams-sketch.md`](sandbox-semteams-sketch.md) — companion sandbox migration on the SemTeams side

# SemTeams Consumer Sketch — `pkg/sandbox`

**Status**: Working draft — 2026-05-31. Pre-ADR (ADR-052).
Companion to [`sandbox-substrate.md`](sandbox-substrate.md) and
[`sandbox-substrate-design-exercise.md`](sandbox-substrate-design-exercise.md).

**Scope**: Maps SemTeams' Coordinator devcontainer pre-routing
path onto `pkg/sandbox` + the `devcontainer` renderer plugin.
Validates the substrate's two-lease-mode support (round 2's
`reusable+full` vs `ephemeral+minimal` split) against the SemTeams
chain-vs-tool execution distinction. **Stakeholder input needed**
on items marked **[STAKEHOLDER]** before ADR-052 drafts.

## Current state (what SemTeams does today)

SemTeams Coordinator routes agent chains to execution environments
based on per-chain capability needs (which tools, which
configuration, which model access). Today, Coordinator wires
devcontainer profiles per agent role; the devcontainer JSON files
live in the SemTeams repo and reference docker images, mounted
config, env vars, etc.

Roughly (paraphrased; **[STAKEHOLDER]** confirm):

```json
// semteams-repo: .devcontainer/architect-role/devcontainer.json
{
  "name": "architect-role",
  "image": "ghcr.io/c360studio/semteams-architect:latest",
  "features": {
    "ghcr.io/devcontainers/features/go:1": {"version": "1.26"},
    "ghcr.io/devcontainers/features/node:1": {"version": "20"}
  },
  "remoteEnv": {
    "MODEL_ENDPOINT": "${localEnv:MODEL_ENDPOINT}",
    "NATS_URL": "nats://nats:4222"
  },
  "mounts": [
    "source=${localWorkspaceFolder},target=/workspace,type=bind",
    "source=${localEnv:HOME}/.config/semteams,target=/root/.config/semteams,type=bind"
  ],
  "postCreateCommand": "task setup"
}
```

Coordinator's routing logic (paraphrased):

```python
# semteams-repo: coordinator/routing.py (paraphrased)

def route_chain(chain):
    role = chain.architect_role  # or editor, reviewer, etc.
    profile_path = f".devcontainer/{role}-role/devcontainer.json"
    container = devcontainer_cli.up(profile_path)
    return ChainExecution(chain=chain, container=container)
```

### Pain points motivating the migration

Per workenv-substrate proposal §Background:

1. **devcontainer JSON isn't introspectable by Coordinator policy.**
   Routing decisions ("does this chain need a postgres? does it
   need GPU?") require parsing JSON or trusting role-name
   conventions.
2. **Multiple transport targets duplicate logic.** Devcontainer
   for local IDE; docker-compose for headless agent execution; k8s
   for production agent fleets. Each path re-derives the same
   runtime declarations from different file formats.
3. **No distinction between chain-level and tool-level
   isolation.** Chains today run in one devcontainer for the whole
   chain lifetime; ad-hoc tool execution within the chain (e.g., a
   one-shot `psql` to probe a test database) runs in the same
   container as the agent. No clean way to scope tool execution to
   a smaller env.
4. **Pre-routing decisions can't compose with testevidence.** When
   dev-via-spec returns and scenario-generator wants Coordinator to
   route based on `(scenario.tier, scenario.profile)`, today's
   role-based routing doesn't carry tier metadata. (See
   testevidence-semteams-sketch.md for the testevidence-side.)

## Proposed migration

### Step 1: extract chain capability requirements into `sandbox.CapabilityContract` profiles

Replace per-role devcontainer JSON files with per-role profiles in
`configs/sandbox-profiles.yaml`. The chain-execution path leases
sandboxes via `sandbox.Manager.Lease`; devcontainer becomes one
renderer choice among multiple.

```yaml
# configs/sandbox-profiles.yaml (semteams-owned fragment)
profiles:
  semteams.role.architect:
    schema_version: "1"
    owner: "semteams-team"
    requirements:
      tools:
        - {class: tool.go, version: "1.26"}
        - {class: tool.node, version: "20"}
      services: [service.nats]
      network: restricted
      filesystem: workspace-write
      secrets: [openai_api_key, anthropic_api_key, model_endpoint_url]
    realization:
      renderer: devcontainer
      config:
        image: ghcr.io/c360studio/semteams-architect:latest
        features:
          go: {version: "1.26"}
          node: {version: "20"}
        workspace_mount: ${localWorkspaceFolder}:/workspace
        post_create: "task setup"
    lease:
      mode: reusable          # chains run for the chain's lifetime; env reused across agent loops
      audit: full             # long-lived shared env needs full audit
      ttl: 4h                 # chain time cap

  semteams.role.editor:
    # ... similar shape
    
  semteams.role.reviewer:
    # ... similar shape
```

Coordinator routing becomes:

```python
# semteams-repo: coordinator/routing.py (after migration)

def route_chain(chain):
    profile_id = f"semteams.role.{chain.architect_role}"
    handle = sandbox_client.Lease(
        profile_id=profile_id,
        opts=LeaseOptions(
            holder=f"coordinator-chain-{chain.id}",
            audit=AuditFull,
            owner=None,  # full audit: lease is its own Participant
        ),
    )
    return ChainExecution(chain=chain, sandbox=handle)
```

### Step 2: ephemeral tool execution within chains — the round 2 mode-as-property win

The pain point #3 (no chain-vs-tool isolation distinction)
resolves cleanly via the round 2 lease-mode-as-property design.
Long-running chain envs lease `reusable+full`; ad-hoc tool
execution within the chain (one-shot psql, debugger session, etc.)
leases `ephemeral+minimal` — and the ephemeral lease owner is the
chain entity itself (round 4 `lifecycle.Ref` owner check).

```yaml
# configs/sandbox-profiles.yaml — ephemeral tool profile
profiles:
  semteams.tool.psql-probe:
    schema_version: "1"
    owner: "semteams-team"
    requirements:
      tools:
        - {class: tool.psql, version: "17"}
      services: [service.postgres]
      network: restricted
      filesystem: read-only
      secrets: []  # required for ephemeral+minimal eligibility
    realization:
      renderer: docker-compose
      config:
        compose_file: docker/psql-probe.yml
        service: psql
    lease:
      mode: ephemeral
      audit: minimal   # MinimalAuditEligible (no secrets, network=restricted, fs=read-only, ttl ≤ 10m)
      ttl: 2m          # short — probe-and-done
```

```python
# semteams-repo: agent-tools/probe_db.py (after migration)

def probe_database(chain_entity_ref, query):
    handle = sandbox_client.Lease(
        profile_id="semteams.tool.psql-probe",
        opts=LeaseOptions(
            holder=f"chain-{chain_entity_ref.id}-probe",
            audit=AuditMinimal,
            owner=chain_entity_ref,  # ← Lifecycle.Ref to the chain entity
                                      # framework verifies via lifecycle.Manager.Get
                                      # before granting the lease
        ),
    )
    try:
        result = handle.Exec(ExecRequest(cmd=f"psql -c '{query}'"))
        return result
    finally:
        handle.Release()  # explicit release; TTL is safety net
```

The chain entity is itself a Lifecycle Participant (already, in
SemTeams' existing architecture — chains have lifecycle:
`scheduled → running → complete/failed`). The ephemeral psql lease
is owned by the chain; if the chain crashes mid-probe, the TTL
(2m) cleans up the lease automatically.

### Step 3: write the `devcontainer` renderer plugin

SemTeams ships a renderer that translates `sandbox.CapabilityContract`
+ devcontainer-specific config → `.devcontainer/{profile-id}/devcontainer.json`
+ live container management:

```go
// semteams-repo: internal/sandbox/renderers/devcontainer.go

type Renderer struct {
    dockerClient docker.Client
    devcontainerCLI *devcontainerCLI
}

func (r *Renderer) Name() string { return "devcontainer" }

func (r *Renderer) Capabilities() sandbox.RendererCapabilities {
    return sandbox.RendererCapabilities{
        SupportsInlineExec:    true,   // ← exec into running container
        SupportsScheduledRun:  false,
        SupportsLeaseRefresh:  true,   // ← extend TTL without re-provisioning
    }
}

func (r *Renderer) Render(ctx context.Context, contract sandbox.CapabilityContract) (sandbox.Artifact, error) {
    var cfg DevcontainerConfig
    if err := json.Unmarshal(contract.Realization.Config, &cfg); err != nil {
        return sandbox.Artifact{}, fmt.Errorf("devcontainer renderer: invalid config: %w", err)
    }
    devcontainerJSON, err := r.renderJSON(contract.Requirements, cfg)
    if err != nil {
        return sandbox.Artifact{}, err
    }
    return sandbox.Artifact{
        RendererName: r.Name(),
        Content:      devcontainerJSON,
        ContentType:  "application/json",
        Metadata:     map[string]string{"image": cfg.Image},
    }, nil
}

func (r *Renderer) Provision(ctx context.Context, artifact sandbox.Artifact) (sandbox.HandleRef, error) {
    // Materialize secrets via SecretResolver BEFORE container start.
    // Renderer never writes secret values to SandboxState.Handle.
    secretRefs := extractSecretRefs(artifact)
    secrets, err := r.secretResolver.Resolve(ctx, secretRefs)
    if err != nil {
        return sandbox.HandleRef{}, fmt.Errorf("secret resolution failed: %w", err)
    }
    
    leaseID := generateLeaseID()
    envFile := r.materializeSecretsEnvFile(leaseID, secrets)  // /tmp/sandbox-{leaseID}.env, 0600 perms
    
    containerID, err := r.devcontainerCLI.Up(ctx, artifact.Content, envFile)
    if err != nil {
        return sandbox.HandleRef{}, fmt.Errorf("devcontainer up failed: %w", err)
    }
    
    return sandbox.HandleRef{
        LeaseID:      leaseID,
        RendererName: r.Name(),
        Payload: mustJSON(devcontainerPayload{
            ContainerID:    containerID,
            SecretsEnvFile: envFile,
            // ← NO secret values in Payload; only the file path
        }),
    }, nil
}

func (r *Renderer) ProbeReady(ctx context.Context, ref sandbox.HandleRef) (*sandbox.ReadyResult, error) {
    var payload devcontainerPayload
    json.Unmarshal(ref.Payload, &payload)
    
    state, err := r.dockerClient.ContainerInspect(ctx, payload.ContainerID)
    if err != nil {
        return nil, err  // probe-itself-errored
    }
    
    switch state.State.Status {
    case "running":
        // Optionally probe post-create command health, service readiness, etc.
        return &sandbox.ReadyResult{
            Ready: true,
            State: sandbox.ReadyStateReady,
            Reason: "container running, post-create complete",
        }, nil
    case "starting":
        return &sandbox.ReadyResult{
            Ready: false,
            State: sandbox.ReadyStateNotReady,
            Reason: "container still starting",
        }, nil
    default:
        return &sandbox.ReadyResult{
            Ready: false,
            State: sandbox.ReadyStateNotReady,
            Reason: fmt.Sprintf("container in state %q", state.State.Status),
        }, nil
    }
}

func (r *Renderer) Release(ctx context.Context, ref sandbox.HandleRef) error {
    var payload devcontainerPayload
    json.Unmarshal(ref.Payload, &payload)
    
    // Idempotent: container-not-found is success
    _ = r.devcontainerCLI.Down(ctx, payload.ContainerID)
    
    // Delete the per-lease temp secrets file
    _ = os.Remove(payload.SecretsEnvFile)
    
    return nil
}

func (r *Renderer) Acquire(ctx context.Context, ref sandbox.HandleRef) (sandbox.Handle, error) {
    return &devcontainerHandle{ref: ref, renderer: r}, nil
}

// Exec on Handle dispatches to the renderer's live execution path.
func (h *devcontainerHandle) Exec(ctx context.Context, req sandbox.ExecRequest) (*sandbox.ExecResult, error) {
    var payload devcontainerPayload
    json.Unmarshal(h.ref.Payload, &payload)
    
    result, err := h.renderer.dockerClient.ContainerExec(ctx, payload.ContainerID, req.Command)
    if err != nil {
        return nil, err
    }
    
    // testevidence applies RedactionPatterns before evidence_ref persistence;
    // renderer doesn't need to scrub here because secrets aren't injected
    // per-exec — they're materialized at Provision via env file.
    return result, nil
}
```

### Step 4: Coordinator pre-routing uses typed contract

Coordinator's per-chain routing decisions read typed
`sandbox.CapabilityContract` (resolved from profile-ID) instead of
parsing devcontainer JSON or trusting role-name conventions:

```python
# semteams-repo: coordinator/routing.py (after migration)

def can_chain_run_in_isolated_runner(chain):
    """Coordinator policy: chains that need GPU or special hardware
    route to non-default runners; chains that only need standard
    tools can use the default pool."""
    
    profile_id = f"semteams.role.{chain.architect_role}"
    contract = sandbox_client.Resolve(profile_id)
    
    # Typed policy decisions, not text parsing:
    if any(t.class_ == "tool.cuda" for t in contract.Requirements.Tools):
        return False  # GPU chain — route to GPU pool
    if contract.Requirements.Network == NetworkOpen:
        return False  # open network chain — route to isolated pool
    return True       # default pool OK
```

### Step 5: testevidence integration (cross-reference)

(See testevidence-semteams-sketch.md for the testevidence-side
flow.)

When dev-via-spec returns, scenario-generator emits
`EvidenceContract`s with `environment_profile_ids` pointing at
SemTeams sandbox profiles. EvidenceRun leases those profiles
(`AuditFull` for chain-bound evidence; `AuditMinimal` with the
chain as `lifecycle.Ref` owner for tool-execution-scoped evidence).
Same sandbox substrate, different consumer.

## Acceptance criteria for the SemTeams sandbox migration

After the migration lands:

- [ ] All SemTeams role-based devcontainer profiles migrated to
  `sandbox.CapabilityContract` entries in
  `configs/sandbox-profiles.yaml`.
- [ ] Coordinator routing reads typed contracts; no devcontainer
  JSON parsing.
- [ ] At least one ephemeral-mode tool profile exists (the
  `semteams.tool.psql-probe` example, or equivalent) demonstrating
  the chain-vs-tool isolation distinction.
- [ ] At least one chain has end-to-end migrated: chain start →
  Coordinator routes via `sandbox.Manager.Lease(profile_id,
  AuditFull)` → chain agents run inside the leased env → chain
  terminal → explicit lease release. Working chain-run proof.
- [ ] At least one ephemeral-mode tool execution works inside a
  chain: chain entity exists in lifecycle → tool leases ephemeral
  sandbox with `Owner=chain_entity_ref` → framework verifies owner
  via `lifecycle.Manager.Get` → lease granted → tool executes →
  explicit release OR TTL safety net.
- [ ] Existing devcontainer integration unchanged from the
  operator's IDE perspective: VS Code "Open in Container" still
  works against the renderer's output.

## Open gaps surfaced by this sketch

### Gap 1: Chain entity Lifecycle Participant readiness [STAKEHOLDER]

The round 4 `lifecycle.Ref` owner check requires the chain entity
to be a Lifecycle Participant in semstreams' lifecycle.Manager.
**[STAKEHOLDER]** — confirm SemTeams' chain entities are (or will
be) Lifecycle Participants by the time ADR-052 ships. If they're
not, the ephemeral+minimal lease pattern can't use the chain as
owner; would need a different visible-lifecycle-owner.

Likely true (SemTeams already uses ADR-049 Lifecycle for chain
state per beta.89), but worth confirming explicitly.

### Gap 2: Multi-chain shared environments [STAKEHOLDER]

Some envs serve multiple chains (e.g., a long-running test
database that several chains query). Today, that's a shared
docker-compose service outside any single devcontainer. With
sandbox substrate:

- **Option A**: shared services live in their own
  `mode: reusable, audit: full, ttl: 24h` sandbox lease; chains
  reference via NATS / postgres-URL handed back from the shared
  lease's handle. Operator manages the shared lease.
- **Option B**: each chain leases the shared service standalone;
  framework deduplicates at the Manager level via lease pooling.
- **Option C**: shared services stay outside sandbox substrate
  (operator-managed standalone); chains reference URLs in their
  CapabilityContract.

**Lean**: Option C for v1 (keeps the substrate simple). Pool-
semantics is a Phase 2 enhancement deferred in the proposal.
**[STAKEHOLDER]** — SemTeams team picks based on their shared-
service usage patterns.

### Gap 3: Image management — who owns `ghcr.io/c360studio/semteams-*`?

Devcontainer images live in GHCR; SemTeams CI builds and pushes
them today. With substrate migration:

- Profile config references image name + version (`ghcr.io/c360studio/semteams-architect:v1.5`).
- Renderer pulls the image at `Provision` time.
- Image versioning becomes profile-version coupled.

No semstreams framework change here — but the renderer config
needs to handle pull credentials (private registry). Framework
already has SecretResolver for credentials; renderer asks the
resolver for `secret.ghcr_pull_token` if needed.

### Gap 4: VS Code "Open in Container" integration [STAKEHOLDER]

VS Code uses `.devcontainer/devcontainer.json` directly. The
substrate renders devcontainer JSON from typed contracts — but
does VS Code see the rendered output, or the typed source?

- **Option A**: SemTeams CI step renders devcontainer JSON files to
  `.devcontainer/{profile}/devcontainer.json` from the profile
  catalog; VS Code uses those rendered files; developers commit
  both source (catalog) and rendered (devcontainer JSON).
- **Option B**: VS Code uses the rendered JSON ephemerally; not
  committed; CI regenerates on each commit.

**Lean**: Option A for IDE ergonomics; commit both source and
rendered. **[STAKEHOLDER]** — SemTeams developer workflow call.

### Gap 5: Lease holder identity for AuditFull chains

`LeaseOptions.Holder` is required for `AuditFull` leases as
attribution. SemTeams chains are themselves Lifecycle Participants;
should `Holder` reference the chain entity's ID directly?

```python
handle = sandbox_client.Lease(
    profile_id=profile_id,
    opts=LeaseOptions(
        holder=chain_entity_ref.id,  # ← naturally aligns with chain identity
        audit=AuditFull,
    ),
)
```

This is consistent but bleeds chain identity into sandbox state.
Alternative: Holder is a free-form string ("coordinator-chain-X")
for human-readable attribution; chain entity linkage is via
operator-driven graph queries.

**Lean**: free-form Holder string for v1 (decouples from chain
entity identity); future linkage via graph queries. **[STAKEHOLDER]**
— SemTeams team call on operator observability needs.

### Gap 6: SecretResolver backend — env var vs vault vs file

The framework `SecretResolver` interface needs at least one
concrete implementation shipped. For SemTeams chains running
locally or in agent-cluster deployments:

- **Env-var-backed**: simplest; secrets in process env at startup;
  fits dev + simple prod.
- **File-backed**: secrets in a JSON file at known path;
  Resolver reads on demand.
- **Vault-backed**: HashiCorp Vault client; Resolver fetches on
  demand with token refresh.
- **K8s-secret-backed**: mounts Kubernetes secrets via volume;
  Resolver reads from mount path.

**Lean**: env-var + file-backed in v1 (covers dev + simple prod);
vault + k8s as Phase 2 community extensions. **[STAKEHOLDER]** —
SemTeams ops team's call on what's needed for production.

### Gap 7: Lease pool semantics for `reusable` mode

`mode: reusable` implies the lease *might* be shared across multiple
consumers (e.g., a long-running test postgres that 3 chains all
query). The proposal explicitly defers "multi-tenant workenv lease
semantics" to Phase 2. For v1:

- Each `reusable` lease is single-tenant (one holder).
- Pool semantics (one lease, multiple holders, framework-tracked
  refcount) is Phase 2.
- Chains that need a shared service today use Gap 2's Option C
  (operator-managed standalone).

Documented in the design exercise §What this proposal does NOT
decide; mention in ADR-052 explicitly so SemTeams doesn't expect
pool semantics on day 1.

## What this sketch validates about the substrate shape

- ✅ Two-lease-mode design (round 2) cleanly maps to the
  chain-vs-tool execution distinction SemTeams needs.
- ✅ `lifecycle.Ref` owner check (round 4) lets ephemeral tool
  leases be owned by chain Lifecycle Participants — exactly the
  visible-lifecycle-owner-on-the-consumer pattern the design
  intended.
- ✅ `SupportsInlineExec=true` on devcontainer renderer fits
  inline tool exec.
- ✅ `SupportsLeaseRefresh=true` capability flag matters for long-
  running chains — chain TTL extension without re-provisioning.
- ✅ SecretResolver abstraction (§New 7) fits SemTeams' secret-
  per-role-profile usage.
- ⚠️ Chain entity Lifecycle Participant readiness (Gap 1) is a
  precondition — must confirm before relying on owner ref pattern.
- ⚠️ Multi-chain shared environments (Gap 2) need explicit ADR-052
  language saying "v1 scope is per-chain isolation; shared
  services stay operator-managed."

## Cross-references

- [`sandbox-substrate.md`](sandbox-substrate.md) — parent proposal
- [`sandbox-substrate-design-exercise.md`](sandbox-substrate-design-exercise.md) — substrate shape resolutions
- [`sandbox-semspec-sketch.md`](sandbox-semspec-sketch.md) — companion sandbox migration on the SemSpec side
- [`testevidence-semteams-sketch.md`](testevidence-semteams-sketch.md) — testevidence side (parked-on-substrate consumer)

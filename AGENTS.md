# SemStreams Project Context

A stream processor that builds semantic knowledge graphs from event data using NATS JetStream.

## Tech Stack

- Go 1.25 + NATS JetStream (KV, ObjectStore)
- Prometheus (metrics), slog (logging)
- Task (task runner) — run `task --list` for all commands

## Architecture

```
Events → Graphable Interface → Knowledge Graph → Queries
```

## Semantic Agent Routing

- Design-time work — proposals, designs, spec deltas, ADR drafts, OpenSpec target state — uses
  `semstreams-architect`. Its mandatory first deliverable is a file:line surface inventory of what already exists on
  the touched surface; binding rulings and approval stay with the owner.
- Nontrivial SemStreams backend implementation uses `semstreams-developer`.
- Every nontrivial change is reviewed by `semstreams-reviewer` before integration.
- Spawning these three project role agents is the DEFAULT execution path for nontrivial and spec-based work — no
  user permission needed. (Only massively-parallel Workflow orchestration is opt-in; that restriction does not apply
  to role agents. There is no "don't spawn agents unless asked" rule in this repo.)
- Generic Go agents are only an isolated idiom, concurrency, or runtime second pass; they do not replace any of the
  three roles.
- The technical writer owns durable documentation and conservative OpenSpec task truth.
- Canonical role contracts live in `.agents/contracts/`; platform adapters must remain thin.
- Canonical shared decision skills live in `.agents/skills/` — kv-or-stream (KV Watch vs JetStream
  Stream, 4-test heuristic), entity-or-bucket (graph triples vs private/operational KV, and how a
  rule reads it), orchestration-check (rule vs component vs lifecycle boundary),
  new-payload (payload-registry checklist), query-pattern (admitted remote operation vs
  operation-specific typed adapter; MCP graph access is unavailable). Read the
  canonical `.agents/skills/<name>/SKILL.md` directly; the `.claude/skills/` entries of the same
  names are thin adapters to it.

## Shared work protocol (Claude and Codex)

State that both agents must see lives in the repository's tools — never in a prose document or either agent's private
memory. Each question has one home, and each home is a `gh` or `task` query.

| Question | Home | Rule |
|---|---|---|
| What is wanted, what kind, is it decided | GitHub issue + labels (`type:` / `area:` / `class:` / `status:` / `horizon:`) | `status:needs-decision` is the owner's docket; a ruling is posted as an issue comment and the label removed. `status:blocked` names its blocker in a comment. |
| What gates the next tag | GitHub milestone named for the intended version | Membership is the gate: in or out; an unruled item is out. `horizon:pre-v1` means before v1.0.0, not before the next tag. |
| An epic | A tracking issue labeled `type:epic` whose body carries a task list of `#n` children | GitHub renders the progress; there is no separate epic document. |
| Who has claimed what | A **draft PR** opened at the start of the work, `Closes #n` in its body; the branch prefix names the agent (`claude/…`, `codex/…`) | No draft PR, no claim. Design-phase work claims the same way — the OpenSpec proposal is its first commit. A stop-point goes in the PR description. |
| Target state and task truth | The OpenSpec change inside that PR; `task openspec:queue` reads its holds | Archiving the change is part of the landing PR, not a follow-up. |
| Why | An ADR, or the owner's ruling comment on the issue | — |

Rituals:

- **Start:** `gh issue list --milestone <m> --state open` · `gh pr list` (drafts are claims — skip them) ·
  `task openspec:queue` · `gh run list --branch main --limit 3` · `gh issue list --label status:needs-decision`.
- **Take work:** an unclaimed milestone issue → branch → push → draft PR with `Closes #n` → then work.
- **Land:** undraft → the repo's reviewer role, plus the owner-run cross-agent round where the owner asks for it →
  CI green with **no known unfixed flake in a required job** (a fresh green over a known flake is rerun-to-green:
  fix it, or file it and obtain an explicit owner waiver recorded as a PR comment) → squash merge closes the issue.
  State `implemented-by: <model>` in the PR body.
- **Close:** no issue closes without the owner's explicit CONFIRM-CLOSE.
- **Tag:** milestone at 100% → candidate selection per `openspec/specs/release-candidate-proof/spec.md`. The
  milestone never names the candidate SHA.

There is no program baton document; `docs/proposals/*-program.md` files are retired history.

## Repository ownership boundary (HARD RULE)

SemStreams agents mutate only the SemStreams repository. Sister repositories are read-only inventory sources: agents
may inspect them to measure downstream impact, but must not create branches, edit files, commit, push, open or modify
pull requests or issues, comment, label, tag, release, or otherwise change their state.

When a SemStreams change breaks a downstream adopter, record the exact impact and migration instructions in a
SemStreams-owned migration document. The downstream repository owner implements and validates that migration in its
own repository. The completed one-time SemDev #952 migration is not precedent for future cross-repository writes.

## The adopter seam rule (house rule)

SemStreams is a framework other people build on. Every surface we expose is a bill an adopter pays, and the adopter is
never in the review. So before any design that adds, changes, or exposes an outward-facing surface, answer it for a
specific person — a developer outside this repo, writing a component, who has never opened the file being changed:

**What must they know? What happens if they do nothing? Where do they find out? And what SHOULD they have to know —
ideally nothing?**

The generative half is **prefer observation to prediction.** When a surface makes an adopter compute a value the
framework owns *before* acting — a size limit, a subject, a bucket, a readiness state, a deadline — they will get it
wrong, and wrong silently, because they are predicting a fact they do not hold. A surface that acts, observes the real
outcome, and responds cannot be wrong about a value it never predicted. Where a design asks the caller to predict, the
framework absorbing the failure IS the design, and the adopter-facing knob is what gets deleted.

Full form — the **adopter seam inventory**, a mandatory design deliverable — lives in
`.agents/contracts/semstreams-architect.md`, mirrored as an implementation-time question in the developer contract
and a diff-level check in the reviewer contract.

## Go context ownership (HARD RULE)

Production structs SHALL NOT retain `context.Context`. This prohibition includes embedded fields, renamed imports,
type aliases, wrapper types, interface containers, getters, provider closures, and public configuration knobs that
hide or recover a stored context. Existing violations are removal work, never precedent.

Production root contexts are created only at the process composition boundary. Library constructors, factories,
callbacks, watchers, and goroutines SHALL NOT invent roots with `context.Background`, `context.TODO`, or
`context.WithoutCancel`. For blocking or cancelable operations, use the context-aware standard API when one exists.
`http.Server.BaseContext` is the narrow standard-library lifecycle-injection exception: set it where the server is
composed and have its closure capture the exact `Start` context. Repository-defined generic context getters and
providers remain prohibited.

Callers never pass nil context. Exported context-taking boundaries that can return an error reject nil; private
helpers rely on the caller invariant. No boundary or helper defaults nil to `context.Background`.

Pass `context.Context` as the first argument to operations that need it. An owning `Start` or `Run` may derive a
lifecycle child context locally; pass the exact received or derived operation context directly into every goroutine,
callback, and helper. Component work derives from `Start` or `Run`, and every spawned task joins `Stop`.

Only terminal cleanup or finalization, or an already-accepted durability operation whose invariant requires bounded
completion after owner cancellation, may detach. `context.WithTimeout` is the immediate boundary. With a parent, use
`context.WithTimeout(context.WithoutCancel(parent), budget)`. Inside a timeout-only `Stop` or equivalent terminal
finalizer with no parent contract, `context.WithTimeout(context.Background(), budget)` is allowed. The work completes
synchronously or all tasks join before return; these contexts never feed `Start`, `Run`, `Watch`, or continuing work.

Nothing uses `context.WithoutCancel(parent)` directly or creates an unbounded descendant from it. Nested child
cancellation, including `context.WithCancel` or `context.WithCancelCause`, is allowed beneath the bounded context only
when all tasks join before the terminal operation returns.

A lifecycle owner may retain only a private `context.CancelFunc`, protected by synchronization required for its
start/stop contract; it must not retain the context itself. Exported lifecycle records SHALL NOT expose
`context.CancelFunc`. Existing exported cancel functions are removal debt, never precedent.

Flow-based component architecture:
- **Input**: UDP, WebSocket, File — ingest external data
- **Processor**: Graph, JSONMap, Rule — transform and enrich
- **Output**: File, HTTPPost, WebSocket — export data
- **Storage**: ObjectStore — persist to NATS JetStream
- **Gateway**: admitted HTTP operations; embedded queries need a named typed adapter

## Key Packages

| Package | Purpose |
|---------|---------|
| `component/` | Base component types, lifecycle, ports, schema, payload registry |
| `message/` | Message types, Graphable interface, Triple, BaseMessage |
| `graph/` | Knowledge graph operations, queries |
| `natsclient/` | NATS connection, KV buckets, JetStream |
| `processor/` | Data transformation processors |
| `config/` | Configuration loading and validation |
| `health/` | Health monitoring and status |
| `service/` | Flow service, component orchestration |
| `agentic/` | Agentic types, payload registrations, state machine |
| `processor/agentic-loop/` | Loop orchestrator, state machine, trajectory |
| `processor/agentic-model/` | LLM endpoint caller, retry logic |
| `processor/agentic-tools/` | Tool dispatch, executor registry |
| `processor/agentic-dispatch/` | User message routing, commands |
| `processor/agentic-governance/` | PII filtering, rate limiting, content governance |

## Core Interface

Domain types implement `Graphable` to become graph entities:

```go
type Graphable interface {
    EntityID() string          // 6-part federated identifier
    Triples() []message.Triple // Facts about this entity
}
```

## Entity ID Format

6-part hierarchical: `org.platform.domain.system.type.instance`

Example: `acme.ops.robotics.gcs.drone.001`

## Common Tasks

```bash
task build              # Build binary
task test               # Run unit tests
task test:integration   # Run integration tests (uses testcontainers)
task test:race          # Run tests with race detector
task lint               # Run linters
task check              # Run lint + test
```

## E2E Tests (Requires Docker)

E2E tests are tiered and require Docker infrastructure:

```bash
task e2e:core           # Health + dataflow (~10s)
task e2e:structural     # Rules + structural inference (~30s)
task e2e:statistical    # BM25 + community detection (~60s)
task e2e:semantic       # Neural embeddings + LLM (~90s)
task e2e:agentic        # Agent loop + tools (~30s)
task e2e:all            # Run all tiers sequentially
```

**Agent guidance**: E2E tests require Docker and take significant time. For TDD workflows:
- Use `task test` and `task test:integration` for rapid feedback
- E2E tests are for final validation, not iterative development
- If e2e fails, check `task e2e:check-ports` for port conflicts

## Testing Patterns

- Unit tests: Standard `*_test.go` files
- Integration tests: `//go:build integration` tag, uses testcontainers
- E2E tests: Full Docker stack, tiered by capability
- Always run with `-race` flag for concurrency checks

## CI Requirements (IMPORTANT)

**All CI checks must pass before pushing.** The CI workflow (`.github/workflows/ci.yml`) runs:

1. **Lint** — `go vet`, `go fmt` (must be clean), `revive` (warnings = failure)
2. **Test** — Unit tests with `-race`, integration tests with `-race`
3. **Build** — Cross-compile Linux binary
4. **Schema Validation** — `task schema:generate`, check for uncommitted changes

Before pushing, run these locally:

```bash
task lint                    # Must pass with no warnings (revive warnings = CI fail)
go test -race ./...          # Unit tests with race detector
task schema:generate         # Generate schemas
git diff schemas/ specs/     # Must show no changes (commit if there are)
go test ./test/contract/...  # Contract tests
```

**Common CI failures:**
- Revive lint warnings (fix all warnings, they indicate potential issues)
- Uncommitted schema changes after `task schema:generate`
- Race conditions detected in tests
- Unformatted code (`go fmt` not run)

## Breaking changes — E2E required before merge (HARD RULE)

Any commit/tag marked **BREAKING** in the changelog or commit message
MUST have at least one relevant e2e tier green BEFORE the breaking
commit lands on main. Unit + integration tests do not exercise the
full ingest → entity → graph store → query path; registry singleton
retirements and similar migrations can leave a sister binary
half-migrated and silently break every flow that uses it.

Concrete case (2026-05-07): beta.18 retired the payload-registry
singleton. `cmd/e2e-semstreams/main.go` got the migration;
`cmd/semstreams/main.go` did not. Three months of beta releases
shipped on top of a silently broken Docker semantic stack because no
one ran `task e2e:semantic` on main between the migration and the
forensic discovery.

Before tagging anything labeled BREAKING:

```bash
task e2e:semantic            # Or whichever tier covers the touched path
# Confirm green. If the tier doesn't cover the path, that's a coverage
# gap — file it before tagging.
```

After landing a registry-retirement-style migration (singletons,
init() shims, factory + payload split), grep for every binary that
imports the migrated package and verify each has the explicit
registration call:

```bash
grep -rn "iotsensor\." cmd/   # Or whichever package was migrated
```

If only `cmd/e2e-semstreams` has it, the framework binary is
half-migrated. See `feedback_e2e_required_for_breaking_changes.md`
for the full case study.

## Architectural Identity (Not an Event Bus)

SemStreams is NOT a simple event bus or pub/sub framework. It is a knowledge graph engine where the communication model is a consequence of the data model.

### The KV Twofer

Every NATS KV bucket gives you two core interfaces from one write:

- **State**: `kv.Get(key)` — current value, right now
- **Events**: `kv.Watch(pattern)` — fires on every change (fan-out to all watchers)
- **History when configured**: a bounded number of retained per-key revisions

`ENTITY_STATES` has history 1. It is current authority, not an audit or recovery
ledger. Declared watchers rehydrate current matching values and then observe live
changes. State-reactive owners may watch; periodic owners may read current state
on their cycle. See [KV Twofer](docs/concepts/02-kv-twofer.md).

### Facts vs Requests

| Communication type | Primitive | Restart behavior |
|---|---|---|
| Fact/current state | KV Watch | Hydrates current matching inputs; owner repair/redrive completes recovery |
| Work request | JetStream Stream | Unacked work redelivers; acked work does not |

Use `/kv-or-stream` (Claude) or read `.agents/skills/kv-or-stream/SKILL.md` for the full 4-test decision heuristic. See [Streams vs KV Watches](docs/concepts/03-streams-vs-kv-watches.md).

### Inference Tiers

| Tier | Method | Requires |
|------|--------|----------|
| 0 | Explicit triples + rules only | Nothing extra |
| 1 | + BM25 statistical embeddings | Text content (pure Go) |
| 2 | + Neural semantic embeddings | Text + external embedding service |

Tiers only affect entities with text content. Telemetry-only entities cluster via explicit relationships regardless of tier. See [Real-Time Inference](docs/concepts/00-real-time-inference.md).

## Orchestration Boundaries

Two layers: **Reactive Engine** (conditions + actions + workflows) and **Components** (execute work).

| Pattern | Use |
|---------|-----|
| A completes → B starts (no retry) | Reactive rule (single trigger) |
| A → B → A → B... (max N times) | Reactive workflow (loop limits, timeouts) |
| Execute LLM call, process tools | Component |

**Key rules**: Rules trigger, they don't orchestrate. Workflows coordinate, they don't execute. Components are workflow-agnostic. State ownership is exclusive.

Use `/orchestration-check` (Claude) or read `.agents/skills/orchestration-check/SKILL.md` for the full decision framework. See [Orchestration Layers](docs/concepts/14-orchestration-layers.md).

### Rules don't carry payloads

Rules orchestrate by passing **references** (loop IDs, entity IDs, storage refs), never content. Bulky output from an agent lives in its durable stores — `COMPLETE_{loopID}` in AGENT_LOOPS KV, the `agent.complete.*` JetStream stream, ObjectStore via `ContentStorable`. Downstream agents retrieve on demand via tools like `read_loop_result(loop_id, max_bytes, offset)`.

This matters for two reasons: (1) stuffing content into rule payloads silently truncates or explodes small-model context windows, and (2) rules can't make quality judgments over unstructured text — that's coordinator work. If a rule condition needs to branch on the semantic content of an agent's output, trigger a coordinator; the coordinator's terminal tool emits a structured triple that a subsequent rule can match on deterministically.

The **ops role** runs parallel to the coordinator as the observation/learning layer: it watches completed loops and graph telemetry, emitting `ops.diagnosis.*` findings via `emit_diagnosis` for human review (Phase 1); Phase 2 adds write tools via config-only enablement.

See [ADR-028](docs/adr/028-orchestration-architecture.md) for the full rule-skeleton + coordinator + ops architecture.

## Payload Registry

Polymorphic JSON deserialization via type-discriminated envelopes. Every new message type needs:

1. `init()` registration in `payload_registry.go` with domain/category/version/factory
2. `MarshalJSON` method wrapping payload in `BaseMessage` (use type alias to avoid recursion)
3. Package import (blank import if needed) so `init()` runs

Use `/new-payload` (Claude) or read `.agents/skills/new-payload/SKILL.md` for the step-by-step checklist with code templates. See [Payload Registry Guide](docs/concepts/15-payload-registry.md).

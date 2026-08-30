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
- Enumeration — the raw material of a surface inventory: declarations, implementers, callers, every spelling of a
  fact, adjacent specs/ADRs/issues — uses `semstreams-explorer` (cheap model, enumerate-only, writes
  `openspec/changes/<id>/inventory.md` with every search it ran). The architect may start from its file (owner
  ruling A, 2026-08-30, #1180); the reviewer's independent re-derivation is the check on its blind spots.
- A bounded question that needs the strongest read — a design fork the architect framed, a review finding the
  developer disputes, an owner-docket question — goes to `semstreams-judge`: it answers over collected evidence with a
  recommendation, the strongest case against, and what remains unproven (≤20 tool calls, read-only). It never
  enumerates (explorer) and never rules (owner); it is the one role pinned to Fable — the pin lives in
  `.claude/agents/semstreams-judge.md`, so a spawn never passes a model. A judge answer is
  never posted as the ruling comment and never removes `status:needs-decision` — only the owner's own words do.
  **Spawn it when the alternative is another round of the same model checking its own work** — a fresh instance is
  not a different vantage (#1148 converged HIGH → HIGH → APPROVE, then Codex found blockers). The four triggers and
  the do-not list are `.agents/contracts/semstreams-judge.md` § *When to spawn a judge*; the default is not to spawn.
- Spawning these project role agents is the DEFAULT execution path for nontrivial and spec-based work — no
  user permission needed. (Only massively-parallel Workflow orchestration is opt-in; that restriction does not apply
  to role agents. There is no "don't spawn agents unless asked" rule in this repo.)
- Generic Go agents are only an isolated idiom, concurrency, or runtime second pass; they do not replace any of these
  roles.
- The technical writer owns durable documentation and conservative OpenSpec task truth. When a platform has no mapped
  technical-writer profile, the owner/root session materializes reviewed handoffs and reconciles task truth directly;
  developer and reviewer roles do not absorb that authority.
- Canonical role contracts live in `.agents/contracts/`; platform adapters must remain thin.
- Canonical shared decision skills live in `.agents/skills/` — kv-or-stream (KV Watch vs JetStream
  Stream, 4-test heuristic), entity-or-bucket (graph triples vs private/operational KV, and how a
  rule reads it), orchestration-check (rule vs component vs lifecycle boundary),
  new-payload (payload-registry checklist), query-pattern (admitted remote operation vs
  operation-specific typed adapter; MCP graph access is unavailable). Read the
  canonical `.agents/skills/<name>/SKILL.md` directly; the `.claude/skills/` entries of the same
  names are thin adapters to it.

## Shared work protocol (Claude and Codex)

The canonical protocol — where each kind of shared state lives, the start/take/land/close/tag rituals, worktree
hygiene — is **`.agents/protocol.md`**. Read it before taking, landing, or closing work; the `pickup` and `handoff`
rituals read it. Three gates never become a pointer:

- **Claim:** a draft PR with `Closes #n`, opened before the work, on an agent-prefixed branch in its own worktree
  (`git worktree add ../semstreams-wt/<branch> -b <branch> origin/main`). No draft PR, no claim.
- **Merge:** CI green with no known unfixed flake in a required job; the archive/spec sync is the last content
  commit, reviewed with the code; `implemented-by: <persona>` in the PR body; squash merge closes the issue.
- **Close:** no issue closes without the owner's explicit `CONFIRM-CLOSE`, visible in the issue or PR, naming the
  issues it closes and covering only those. A bare "approved", or approval of adjacent work, is never a close (owner
  ruling, 2026-08-29).

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

6-part hierarchical: `org.platform.system.domain.type.instance` (ADR-102: `org.platform` = the minting deployment
authority from `platform.org`/`platform.id`, never a product name; `system` = the source that produced the entity;
`domain.type` = a delegated taxonomy; `instance` = the leaf, always last)

Example: `acme.ops.gcs.robotics.drone.001`

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

SemStreams is a knowledge-graph engine whose communication model is a consequence of its data model. Every KV bucket
is a twofer — `Get` is state, `Watch` is events; `ENTITY_STATES` has history 1 and is current authority, not an audit
or recovery ledger. Facts and current state travel by KV Watch (restart hydrates current values); work requests travel
by JetStream stream (unacked work redelivers) — `/kv-or-stream` is the 4-test heuristic. Inference tiers 0/1/2 affect
only entities with text content. Read `docs/concepts/02-kv-twofer.md`, `03-streams-vs-kv-watches.md`, and
`00-real-time-inference.md` before designing a communication path.

## Orchestration Boundaries

Two layers only: the **Rule Engine** (conditions + actions + per-action `MaxIterations`) triggers; **components**
execute. There is no workflow engine, DSL, state-machine runtime, or separate event bus (`processor/reactive/` was
retired 2026-03-12). Workflow-shaped patterns compose the Lifecycle harness (`pkg/lifecycle`, ADR-049 — participation
is a property of the ENTITY, not the component); bounded parallel work inside a component composes BoundedDispatcher
(`pkg/dispatch`, ADR-048); rules pass **references, never payloads** (ADR-028) — content lives in AGENT_LOOPS KV, the
`agent.complete.*` stream, or ObjectStore, and a rule that must branch on semantic content triggers a coordinator
instead. Domain and Lifecycle `Participant` current state lives in `ENTITY_STATES` under graph-ingest's authority.
Engine gaps file as engine work, never app-side plumbing. The pattern catalog and decision framework live in
`docs/concepts/14-orchestration-layers.md`, `docs/concepts/25-phased-agentic-chains.md`, and `/orchestration-check`;
read them before adding orchestration logic.

## Payload Registry

Polymorphic JSON deserialization via type-discriminated envelopes. Every new message type needs:

1. `RegisterPayloads(reg *payloadregistry.Registry) error` in `payload_registry.go` — no `init()`, no global
   singleton
2. `MarshalJSON`/`UnmarshalJSON` marshal the payload's own fields via a type alias — never construct a
   `BaseMessage` literal (its fields are unexported)
3. An explicit `RegisterPayloads` call from `payloadbuiltins.Register` or the binary's own composition root
   — nothing runs it automatically

Use `/new-payload` (Claude) or read `.agents/skills/new-payload/SKILL.md` for the step-by-step checklist
with code templates. See [Payload Registry Guide](docs/concepts/15-payload-registry.md).

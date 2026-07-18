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

- Nontrivial SemStreams backend implementation uses `semstreams-developer`.
- Every nontrivial change is reviewed by `semstreams-reviewer` before integration.
- Generic Go agents are only an isolated idiom, concurrency, or runtime second pass; they do not replace either role.
- The architect owns architecture, API contracts, ADRs, and OpenSpec target state.
- The technical writer owns durable documentation and conservative OpenSpec task truth.
- Canonical role contracts live in `.agents/contracts/`; platform adapters must remain thin.
- Canonical shared decision skills live in `.agents/skills/` — kv-or-stream (KV Watch vs JetStream
  Stream, 4-test heuristic), orchestration-check (rule vs component vs lifecycle boundary),
  new-payload (payload-registry checklist), query-pattern (GraphQL vs MCP vs NATS Direct). Read the
  canonical `.agents/skills/<name>/SKILL.md` directly; the `.claude/skills/` entries of the same
  names are thin adapters to it.

Flow-based component architecture:
- **Input**: UDP, WebSocket, File — ingest external data
- **Processor**: Graph, JSONMap, Rule — transform and enrich
- **Output**: File, HTTPPost, WebSocket — export data
- **Storage**: ObjectStore — persist to NATS JetStream
- **Gateway**: HTTP, GraphQL, MCP — expose query APIs

## Spec-driven development (OpenSpec)

SemStreams uses **OpenSpec** (adopted beta.132+; the CLI and `.claude/` skills
are installed — `/opsx:new`, `/opsx:continue`, `/opsx:apply`, `/opsx:archive`;
`openspec list`, `openspec validate`). Three homes, three jobs — put a thing in
the right one:

| Home | Holds | Drifts? |
|------|-------|---------|
| `openspec/specs/<capability>/spec.md` | **Current truth** — what a capability does *today* (`Requirement` + `GIVEN/WHEN/THEN`) | No — every change edits it via a delta |
| `openspec/changes/<id>/` | **Proposed target state** — `proposal.md` + `tasks.md` + spec deltas; archived on completion | Resolves on archive |
| `docs/adr/` | **Genuine decisions only** — irreversible choices + cross-repo contracts (the *why*) | No — history |
| `docs/0X-*.md` | Tutorial / operations / runbooks (retire "how it works" into specs as touched) | Being retired |

Rules of the road:

- **Non-trivial or cross-cutting work starts with a change** (`/opsx:new`):
  proposal + tasks + spec deltas *before* code. Small mechanical fixes don't need
  one.
- **Specs are seeded lazily** — write a capability's spec when a change first
  touches it, distilled from code + existing docs and **verified against code**.
  Do NOT backfill; an unverified spec is just another drifting doc.
- **ADRs are pure decision records now.** Record a decision (irreversible /
  cross-repo contract) as a one-page ADR; the *mechanics* it implies live in the
  capability's spec. Don't write "how it works" as an ADR. Existing ADRs 001–068
  stay as history. See `docs/adr/README.md`.
- **Read `openspec/project.md` first** when scoping anything — it carries the
  Purpose and the **Product Boundary** (SemStreams owns substrate/primitives, not
  product domain semantics).

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
| `processor/agentic-memory/` | Graph-backed persistent memory |
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

Every NATS KV bucket gives you three interfaces from one write:

- **State**: `kv.Get(key)` — current value, right now
- **Events**: `kv.Watch(pattern)` — fires on every change (fan-out to all watchers)
- **History**: Replay from any revision — audit trail at no extra cost

**The write IS the event.** No separate event bus. No dual-write problem. Internal processors react to state changes via KV watch, not pub/sub topics. See [KV Twofer](docs/concepts/02-kv-twofer.md).

### Facts vs Requests

| Communication type | Primitive | Restart behavior |
|---|---|---|
| Fact about the world (entity state, index, current status) | KV Watch | Re-delivers all current values (correct recovery) |
| Request to do something (task, LLM call, tool execution) | JetStream Stream | Resumes from last ack (no re-execution) |

Use `/kv-or-stream` for the full 4-test decision heuristic. See [Streams vs KV Watches](docs/concepts/03-streams-vs-kv-watches.md).

### Inference Tiers

| Tier | Method | Requires |
|------|--------|----------|
| 0 | Explicit triples + rules only | Nothing extra |
| 1 | + BM25 statistical embeddings | Text content (pure Go) |
| 2 | + Neural semantic embeddings | Text + external embedding service |

Tiers only affect entities with text content. Telemetry-only entities cluster via explicit relationships regardless of tier. See [Real-Time Inference](docs/concepts/00-real-time-inference.md).

## Orchestration Boundaries

Two layers: **Rule Engine** (conditions + actions + iteration caps) and **Components** (execute work). There is no separate workflow engine — no DSL, no state-machine runtime, no separate event bus; `processor/reactive/` was retired (2026-03-12). Multi-step patterns are expressed as coordinated rule sets firing components, with per-action `MaxIterations` providing iteration caps and entity triples + KV buckets + ObjectStore providing durable state.

For workflow-shaped patterns (named instance with lifecycle, restart recovery, operator visibility), components compose the **Lifecycle harness** substrate (`pkg/lifecycle`, ADR-047): apps declare state structs implementing `Participant`; the framework provides KV-backed `Manager` (Get/Create/Update/Transition/Complete/Fail), rule integration (`lifecycle_*` actions + `$entity.lifecycle.*` substitutions), and an operator gateway API (`GET /workflows`, history via KV revision replay, operator-writable patches via struct tags). The harness is **substrate convention, not a workflow engine** — apps own work logic, state schema, and phase transitions; the framework provides KV storage, restart recovery, audit history, and a uniform operator API across products. **Lifecycle participation is a property of the ENTITY, not the COMPONENT or REQUEST** — short-lived handlers (HTTP, single-purpose processors) can read/write Participant-implementing entities without claiming participation; long-lived participants (mission-planner, calibration-orchestrator, requirement-executor) implement `Participant` and use `Manager`.

For bounded-concurrency parallel work inside components, compose **BoundedDispatcher** (`pkg/dispatch`, ADR-048) — a KV-twofer-aware bounded worker pool wrapping `pkg/worker.Pool`. NOT for at-the-rule-layer fan-out (use rules' `for_each` for that).

| Pattern | Use |
|---------|-----|
| A completes → B starts (no retry) | Single rule, one action |
| A → B → C → D (no loop) | Rule chain (one rule per transition) |
| A → if X then B else C | One rule, action-level `when` clauses (ADR-041) |
| A → B → A → B... (max N times) | Rule chain with per-action `MaxIterations` cap |
| Fan-out + fan-in synchronization | Fan-out rule (`for_each`) + counter-based join (`.length` / `.triples` / `length_eq`) |
| Named instance with lifecycle (mission, sensor, scenario, plan, request) | Lifecycle harness `Participant` — ADR-047 |
| Bounded parallel work inside a component | BoundedDispatcher — ADR-048 |
| Execute LLM call, graph query, file I/O, etc. | Component |

**Key rules**: Rules trigger, they don't do work inline. Components execute, they don't know their caller. State ownership is exclusive — domain entities in `ENTITY_STATES` (only `graph-ingest` writes), operational results in component-specific KV (e.g., `AGENT_LOOPS` with `COMPLETE_*` prefix), Lifecycle-managed instances in workflow-type KV buckets (e.g., `MISSIONS`, `CSAPI_SYSTEMS`, declared at `Manager.Register` time), events in JetStream streams, bulky payloads in ObjectStore via `ContentStorable` with ref-triples on the owning entity.

**Engine gaps file as engine work, not app-side state plumbing.** semspec's retired `workflow/reactive/` (7,264 LOC) is the cautionary tale on the engine-shape axis; semspec's `workflow/` package (~7,840 LOC of convention hand-rolled because the framework didn't provide one) is the cautionary tale on the convention-shape axis — both are migration blockers when carried per-consumer. The Lifecycle harness exists specifically to retire the next-instance of the second pattern (cross-consumer convention reinvention). If a rule-engine, harness, or substrate primitive is missing, propose adding it; don't carve out a parallel path.

Use `/orchestration-check` for the decision framework. See [Orchestration Layers — How We Do Workflows in semstreams](docs/concepts/14-orchestration-layers.md) for the full pattern catalog. For multi-step agentic workflows specifically (rule chains spawning agent phases), see [Phased Agentic Chains](docs/concepts/25-phased-agentic-chains.md). For Lifecycle-shaped workflows, see [ADR-047](docs/adr/047-lifecycle-harness-substrate.md). For bounded-concurrency parallel work, see [ADR-048](docs/adr/048-bounded-dispatcher-and-triples-substrate.md).

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

Use `/new-payload` for the step-by-step checklist with code templates. See [Payload Registry Guide](docs/concepts/15-payload-registry.md).

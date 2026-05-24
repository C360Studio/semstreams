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

Flow-based component architecture:
- **Input**: UDP, WebSocket, File — ingest external data
- **Processor**: Graph, JSONMap, Rule — transform and enrich
- **Output**: File, HTTPPost, WebSocket — export data
- **Storage**: ObjectStore — persist to NATS JetStream
- **Gateway**: HTTP, GraphQL, MCP — expose query APIs

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

Two layers: **Rule Engine** (conditions + actions + iteration caps) and **Components** (execute work). There is no separate workflow engine — `processor/reactive/` was retired. Multi-step patterns are expressed as coordinated rule sets firing components, with per-action `MaxIterations` providing iteration caps and entity triples + existing KV buckets + ObjectStore providing durable state.

| Pattern | Use |
|---------|-----|
| A completes → B starts (no retry) | Single rule, one action |
| A → B → C → D (no loop) | Rule chain (one rule per transition) |
| A → if X then B else C | One rule, action-level `when` clauses (ADR-041) |
| A → B → A → B... (max N times) | Rule chain with per-action `MaxIterations` cap |
| Fan-out + fan-in synchronization | Fan-out rule + synchronizer-key rule |
| Execute LLM call, graph query, file I/O, etc. | Component |

**Key rules**: Rules trigger, they don't do work inline. Components execute, they don't know their caller. State ownership is exclusive — domain entities in `ENTITY_STATES` (only `graph-ingest` writes), operational results in component-specific KV (e.g., `AGENT_LOOPS` with `COMPLETE_*` prefix), events in JetStream streams, bulky payloads in ObjectStore via `ContentStorable` with ref-triples on the owning entity.

**Engine gaps file as engine work, not app-side state plumbing.** semspec's 7,264 LOC of `workflow/reactive/` (the "semspec trap") is the cautionary tale — app-side state machines around rule-engine limitations become migration blockers the framework can't help dig out of. If a pattern needs a primitive the rule engine doesn't have, propose adding it; don't carve out a parallel path.

Use `/orchestration-check` for the decision framework. See [Orchestration Layers — How We Do Workflows in semstreams](docs/concepts/14-orchestration-layers.md) for the full pattern catalog. For multi-step agentic workflows specifically (rule chains spawning agent phases), see [Phased Agentic Chains](docs/concepts/25-phased-agentic-chains.md) — the application of these discipline patterns to the agentic-loop substrate, with the substrate-vs-capability-vs-application split and the inventory of framework primitives that support it.

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


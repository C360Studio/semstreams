# SemStreams

Guidance for coding agents. Claude Code loads this file as `CLAUDE.md`, Codex as `AGENTS.md`; the two are
byte-identical and `internal/agentprofiles/profile_contract_test.go` keeps them so. It carries the facts an agent cannot
derive from the tree and one line per rule naming where the rule lives. Per-platform command names: `.agents/README.md`.

## What this is for

SemStreams is the governed graph substrate and **framework** for the C360 `sem*` family. It owns primitives and
contracts, never a consumer's domain semantics. Read `openspec/project.md` (Purpose + Product Boundary) before
scoping anything, especially before calling something unused, dead, or deletable: a capability nothing in this
tree reads may still be a first-class purpose whose consumer is a sister repo or a product above us.
A `grep` for callers answers "is this wired", never "is this wanted"; when it matters, read `git log -S` and the
governing ADR. Agent execution evidence is a first-class capability, not trace exhaust (`openspec/project.md`
§ Purpose).

## Stack and layout

Go 1.26, NATS JetStream (KV, ObjectStore), Prometheus metrics, slog logging. Flow-based components: Input (UDP,
WebSocket, File) → Processor (Graph, JSONMap, Rule) → Output (File, HTTPPost, WebSocket), plus Storage (ObjectStore)
and Gateway (admitted HTTP operations; embedded queries need a named typed adapter).

| Package | Purpose |
|---------|---------|
| `component/` | Base component types, lifecycle, ports, schema, payload registry |
| `message/` | Message types, `Graphable`, `Triple`, `BaseMessage` |
| `graph/` | Knowledge graph operations and queries |
| `natsclient/` | NATS connection, KV buckets, JetStream |
| `processor/` | Transformation processors; `agentic-loop`, `agentic-model`, `agentic-tools`, `agentic-dispatch`, `agentic-governance` |
| `config/`, `health/`, `service/` | Configuration, health, flow service and component orchestration |
| `agentic/` | Agentic types, payload registrations, state machine |
| `pkg/lifecycle`, `pkg/dispatch` | Lifecycle harness (ADR-049), BoundedDispatcher (ADR-048) |

Domain types become graph entities by implementing `Graphable`: `EntityID() string` and
`Triples() []message.Triple`. Entity IDs are six-part, `org.platform.system.domain.type.instance` (ADR-102), for
example `acme.ops.gcs.robotics.drone.001`.

## Commands

`task --list` shows every command with its rationale. The ones an agent reaches for:

```bash
task build              # Build binary
task test               # Unit tests
task test:integration   # Integration tests (testcontainers)
task test:race          # Unit tests with -race
task lint               # vet, fmt, pinned revive, fixed-port and raw-Request guards
task check              # lint + test
task schema:generate    # Regenerate schemas; `git diff schemas/ specs/` must be empty before push
task openspec:queue     # In-flight OpenSpec changes and WHY each is still open
task e2e:core           # Docker tiers: core ~10s, structural ~30s, statistical ~60s,
                        # semantic ~90s, agentic ~30s, all = every tier in sequence
```

Before every push run `task check:push`: it mirrors CI (build, lint, tagged vet, schema drift, contract, race unit,
then integration through the canonical runner and its host lock). `task check` is the fast subset. A diff that edits
files tests read, including markdown, is a code change for gate purposes. Revive warnings fail CI and `go fmt` must be
clean. E2E tiers are for final validation, not iteration; `task e2e:check-ports` explains port conflicts.

## Architecture in five sentences

SemStreams is a knowledge-graph engine, not an event bus. Every KV bucket is a twofer: `Get` is state, `Watch` is
events; `ENTITY_STATES` has history 1 and is current authority, never an audit or recovery ledger. Facts travel by KV
Watch, work requests by JetStream stream (the `kv-or-stream` skill). Two orchestration layers only: the
rule engine triggers, components execute; rules pass references, never payloads (ADR-028).
Read `docs/concepts/00-real-time-inference.md`, `02-kv-twofer.md`, `03-streams-vs-kv-watches.md`, and
`14-orchestration-layers.md` before designing a communication path or adding orchestration.

## Specs, changes, decisions

| Home | Holds |
|------|-------|
| `openspec/specs/<capability>/spec.md` | Current truth: what a capability does today, verified against code |
| `openspec/changes/<id>/` | Proposed target state: proposal, tasks, spec deltas; archived on completion |
| `docs/adr/` | Decisions only: irreversible choices and cross-repo contracts, never mechanics |

Non-trivial work starts with a change before code; mechanical fixes do not. Specs are seeded lazily when a change
first touches a capability, never backfilled. Role split: `openspec/project.md` § How we spec; discipline:
`docs/contributing/06-openspec-change-discipline.md`.

## Rules and where they live

The linked file is the rule; this table is only its index.

| Rule | Canonical home | Enforced by |
|------|----------------|-------------|
| Production structs never retain `context.Context`; no invented roots; nil never defaults to `Background` | `.agents/contracts/semstreams-developer.md` § Context ownership | struct half: `test/contract/context_ownership_contract_test.go`; root half: #1012 |
| Every outward-facing surface answers the adopter seam questions; prefer observation to prediction | `.agents/contracts/semstreams-architect.md` § The adopter seam inventory | architect and reviewer contracts |
| A BREAKING change lands only with a relevant E2E tier green | `docs/contributing/02-e2e-tests.md` § Breaking Changes | prose today; #1117 |
| New payload types register via `RegisterPayloads`, no `init()`, no singleton, explicit call from the composition root | `.agents/skills/new-payload/SKILL.md`, `docs/concepts/15-payload-registry.md` | round-trip and registration consistency only (`test/contract/message_contract_test.go`); per-binary parity is prose |
| Rule vs component vs lifecycle boundary; workflow shapes compose the Lifecycle harness | `.agents/skills/orchestration-check/SKILL.md`, `docs/concepts/14-orchestration-layers.md` | review |
| Test fidelity: production seams, `-race`, explicit synchronization, mutation evidence via `cp` backup and checksum | `docs/contributing/01-testing.md`, contracts § Test fidelity | `-race` and structural guards: CI; fidelity and mutation evidence: review |

## Working here (Claude and Codex)

The shared protocol is `.agents/protocol.md`. Read it before filing, taking, landing, or closing work. Three gates
never become a pointer:

- **Claim:** a draft PR with `Closes #n`, opened before the work, on an agent-prefixed branch in its own worktree
  (`git worktree add ../semstreams-wt/<branch> -b <branch> origin/main`). No draft PR, no claim.
- **Merge:** CI green with no known unfixed flake in a required job; `implemented-by: <persona>` in the PR body; the
  archive/spec sync is the last content commit; squash merge closes the issue.
- **Close:** the squash merge of a PR that declared `Closes #n` is the authorization. A close with no merged PR
  behind it takes the owner's word on the issue.

**Agents mutate only this repository.** Sister repositories are read-only inventory: inspect to measure impact; never
branch, edit, push, open, or comment there. A breaking change records its impact and
migration steps in a SemStreams-owned `docs/operations/migration-*.md`; the sister's owner applies it.

Role agents are the default path for nontrivial work; no permission is needed to spawn them, and only
massively-parallel Workflow orchestration is opt-in. `semstreams-architect`
designs, inventory first. `semstreams-developer` implements. `semstreams-reviewer` reviews every nontrivial change
before integration. `semstreams-explorer` enumerates. `semstreams-judge` answers one bounded question over
collected evidence and never rules. Contracts: `.agents/contracts/`; adapters, skills,
and command names: `.agents/README.md`. Binding rulings stay with the owner, on the issue.

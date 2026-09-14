# SemStreams

> A Go framework for explicit semantic context and observable actions.

SemStreams helps developers and coding agents build applications around a live semantic knowledge graph.
Declare domain facts, relationships, component connections and action controls through framework primitives,
so the context needed to understand an application is visible in its contracts.

Start with structural or statistical capabilities, add neural search when useful, and compose agent loops or
human participation where your application needs them. The application owns domain meaning and policy;
SemStreams provides the mechanisms. Local and edge deployments remain central to that design.

```text
Sources → Explicit semantic context → Queries, rules and optional agents → Application behavior
```

See [Building context with SemSource](docs/basics/09-building-semsource.md) for a worked application:
a document becomes typed facts and stored content that a developer or coding agent can retrieve and inspect.

**Built for local and edge systems:**

- **Simple deployment** — Go binary with NATS JetStream; Docker images are available
- **Progressive capabilities** — structural, statistical and semantic retrieval are useful choices in their own right
- **Local dependencies** — keep storage and selected model services at the site when disconnected operation matters
- **Application control** — select the capabilities and human participation appropriate to the work

## Prerequisites

Before starting, verify your environment:

```bash
# Check Go version (1.25+ required)
go version

# Check Docker is running
docker info

# Install Task runner (if not installed)
go install github.com/go-task/task/v3/cmd/task@latest
```

Or run `task dev:check:prerequisites` to verify everything at once.

### Install NATS Server

For local development, we run NATS in Docker:

```bash
# Start NATS with JetStream (managed by task commands)
task dev:nats:start

# Or manually with Docker
docker run -d --name semstreams-nats -p 4222:4222 nats:2.14-alpine -js
```

See [Prerequisites Guide](docs/basics/00-prerequisites.md) for detailed setup instructions.

## Your First 5 Minutes

Get SemStreams running and see data flow through the knowledge graph:

### 1. Build

```bash
task build
```

### 2. Start Everything

```bash
task dev:start
```

This starts NATS and SemStreams with the hello-world config.

### 3. Send Test Data

In another terminal, send a sensor reading via UDP:

```bash
echo '{"device_id":"sensor-001","type":"temperature","reading":23.5,"unit":"celsius","location":"warehouse-7"}' | nc -u localhost 14550
```

Or use the task command:
```bash
task dev:send
```

### 4. Query the Graph

```bash
curl -s http://localhost:8084/graphql \
  -H "Content-Type: application/json" \
  -d '{"query":"{ entitiesByPrefix(prefix: \"demo\", limit: 10) { entities { id } next_cursor } }"}' | jq
```

You should see your sensor entity ID under `data.entitiesByPrefix.entities`. If
`next_cursor` is present, pass it back as the query's `cursor` argument to read
the next page.

### 5. Debug (If Data Doesn't Appear)

```bash
# View recent messages flowing through the system
task dev:messages

# Trace a message through all components
task dev:trace

# View message statistics and stream counts
task dev:stats
```

See [Debugging Data Flow](docs/operations/debugging-data-flow.md) for detailed troubleshooting.

### 6. Stop

```bash
task dev:stop
```

That's it! You've ingested data, transformed it into a semantic graph, and queried
an admitted HTTP facade operation.

## Quick Start (For Experienced Users)

```bash
task build                                      # Build binary
task dev:start                                  # Start NATS + SemStreams
./bin/semstreams --config configs/structural.json  # Or run with a specific config
```

Run `task --list` to see all available commands.

## How It Works: Continuous Intelligence

SemStreams implements the **OODA loop** — a decision-making cycle from military strategy (Boyd, 1986) that also appears in robotics as Sense-Think-Act:

| OODA | Sense-Think-Act | SemStreams |
|------|-----------------|------------|
| Observe | Sense | **Ingest** — events via UDP, WebSocket, file, API |
| Orient | Think | **Graph** — entities with typed relationships |
| Decide | Act | **React** — rules evaluate conditions |
| Act | Act | **Act** — rules trigger, components execute, agents reason |

The graph builds situational awareness; rules and agents close the loop.

Core contracts support this:
- **Graphable** — Your types become graph entities ([docs](docs/basics/03-graphable-interface.md))
- **Payload Registry** — Messages serialize with type discrimination ([docs](docs/concepts/15-payload-registry.md))
- **Typed Graph Mutations** — Components create, reconcile, append, and delete through one revision-aware port
  contract ([docs](docs/adr/091-graph-mutation-authority-without-semantic-ownership.md))

`Graphable` is enough for ordinary event ingestion. Components that intentionally change current graph state use the
typed mutation port: strict create, complete-set reconcile, set-valued append, or revision-fenced delete. CAS exposes
real conflicts without semantic owner leases, and relationship objects may arrive later under eventual consistency.

## Progressive Capabilities

Choose the retrieval capability your application needs:

| Tier | What You Get | What You Need |
|------|--------------|---------------|
| **Structural** | Explicit relationships, graph indexing and rules | NATS JetStream |
| **Statistical** | BM25 lexical search over text | The same local stack; BM25 runs in Go |
| **Semantic** | Neural embedding search over text | An embedding service, which may run locally |

Generative completion and agent loops are separate choices, not requirements of semantic search. Applications can
combine deterministic rules, retrieval and agents, with human participation selected by application policy.
See the [inference tiers](docs/concepts/00-real-time-inference.md) and
[SemSource profile mapping](docs/basics/09-building-semsource.md#choose-capabilities-by-need).

## Architecture

Components connect via NATS subjects in flow-based configurations:

```
Input → Processor → Storage → Graph → Gateway
  │         │          │        │        │
 UDP    iot_sensor  ObjectStore KV+   HTTP facade
 File   document    (raw docs)  Indexes  HTTP
```

| Component Type | Examples | Role |
|----------------|----------|------|
| Input | UDP, WebSocket, File | Ingest external data |
| Processor | Graph, JSONMap, Rule | Transform and enrich |
| Output | File, HTTPPost, WebSocket | Export data |
| Storage | ObjectStore | Persist to NATS JetStream |
| Gateway | HTTP facade | Expose admitted remote operations |

Graph current and derived state commonly lives in NATS KV. Work uses JetStream
streams, and bulky content may live in ObjectStore.

## Agentic AI

When you're ready for LLM-powered automation, SemStreams includes an optional agentic subsystem:

```
                    ┌─────────────────────────────────────────┐
                    │           Agentic Components            │
                    ├─────────────────────────────────────────┤
User Message ───────► agentic-dispatch ─────► agentic-loop   │
                    │       │                      │          │
                    │       │              ┌───────┴───────┐  │
                    │       │              ▼               ▼  │
                    │       │        agentic-model   agentic-tools
                    │       │              │               │  │
                    │       │              ▼               │  │
                    │       │           LLM API    ◄───────┘  │
                    │       │                                 │
                    │       ◄─────── agent.complete.* ────────│
                    └─────────────────────────────────────────┘
```

- **Modular** — 6 components that scale independently
- **OpenAI-compatible** — works with any OpenAI-compatible endpoint
- **Observable** — action identities and observed trajectory evidence for debugging;
  [capture has explicit limits](openspec/specs/agentic-loop/spec.md)

```bash
# Run agentic e2e tests
task e2e:agentic

# Or start the full agentic stack
./bin/semstreams --config configs/agentic.json
```

See [Agentic Quickstart](docs/basics/07-agentic-quickstart.md) to get started.

## Examples

- [Example Processors](examples/processors/) — IoT sensor and document processor implementations
- [Deployment Configs](configs/) — From hello-world to production-ready configurations
- [Tutorial: First Processor](docs/basics/05-first-processor.md) — Step-by-step guide to building your own processor

## Documentation

| Folder | Purpose |
|--------|---------|
| [docs/basics/](docs/basics/) | Getting started, core interfaces, quickstart guides |
| [docs/concepts/](docs/concepts/) | Background knowledge, algorithms, orchestration layers |
| [docs/advanced/](docs/advanced/) | Agentic components, clustering, performance tuning |
| [docs/operations/](docs/operations/) | Monitoring, troubleshooting, deployment |
| [docs/contributing/](docs/contributing/) | Development, testing, CI |

## Development

```bash
# Testing
task test               # Unit tests
task test:integration   # Integration tests (uses testcontainers)
task test:race          # Tests with race detector
task check              # Lint + test

# E2E Tests (requires Docker)
task e2e:core           # Health + dataflow (~10s)
task e2e:structural     # Rules + structural inference (~30s)
task e2e:statistical    # BM25 + community detection (~60s)
task e2e:semantic       # Neural embeddings + LLM (~90s)
task e2e:agentic        # Agent loop + tools (~30s)
task e2e:all            # All tiers sequentially
```

## Requirements

- **Go 1.25+** — [Download](https://go.dev/dl/)
- **Docker** — [Download](https://docker.com) (for NATS, deployment, and E2E tests)
- **Task** — `go install github.com/go-task/task/v3/cmd/task@latest`
- (Optional) Embedding service for Statistical/Semantic tiers
- (Optional) LLM service for Semantic tier and agentic system

See [Prerequisites Guide](docs/basics/00-prerequisites.md) for detailed installation instructions.

## Status

This project is under active development. Expect breaking changes.

## License

See [LICENSE](LICENSE) for details.

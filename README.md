# SemStreams

> A composable stream processing framework designed to run anywhere.

SemStreams is a flow based framework that turns streaming data into a semantic knowledge graph, runs reactive rules, executes workflows, and orchestrates LLM-powered agents. One binary. NATS as the only dependency. Works offline, syncs when connected.

```
Sensors/Events → Knowledge Graph → Rules, Workflows, Agents → Action
```

**Built for the edge:**

- **Simple deployment** — single binary, ships as a Docker image
- **Progressive AI** — start with rules, add LLMs when you're ready. Or run both: deterministic where it matters, intelligent where it helps
- **Offline-first** — works disconnected, syncs when connectivity allows
- **Edge to cluster** — runs on a Raspberry Pi, scales when needed

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
docker run -d --name semstreams-nats -p 4222:4222 nats:2.12-alpine -js
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
  -d '{"query":"{ entitiesByPrefix(prefix: \"demo\", limit: 10) { entityIds } }"}' | jq
```

You should see your sensor entity ID in the response.

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

That's it! You've ingested data, transformed it into a semantic graph, and queried it via GraphQL.

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
| Act | Act | **Act** — rules fire, workflows orchestrate, agents reason |

The graph builds situational awareness; rules and agents close the loop.

Two core patterns power this:
- **Graphable** — Your types become graph entities ([docs](docs/basics/03-graphable-interface.md))
- **Payload Registry** — Messages serialize with type discrimination ([docs](docs/concepts/15-payload-registry.md))
- **Governed Semantic State** — Shared graph facts declare who may replace them, who may append evidence, and how
  cross-entity edges are handled ([docs](docs/concepts/28-governed-semantic-state.md))

`Graphable` is enough for simple fact ingestion. Domains with multiple writers, lifecycle state, regulated audit, or
agentic automation also need ownership for stateful predicate groups. That keeps `ENTITY_STATES` queryable without
letting unrelated components silently overwrite each other's facts.

## Progressive Capabilities

Start simple, add capabilities as your needs grow:

| Tier | What You Get | What You Need |
|------|--------------|---------------|
| **Structural** | Rules engine, explicit relationships, graph indexing | NATS only |
| **Statistical** | + BM25 search, community detection | + Search index |
| **Semantic** | + Neural embeddings, LLM-powered agents | + Embedding service, LLM |

Most deployments start with Structural. Add capabilities when the problem demands it.

Tiers aren't just about resources. Use rules when you need deterministic, auditable outcomes. Use agents when you need judgment and reasoning. Run both in the same flow — each handles what it does best.

## Architecture

Components connect via NATS subjects in flow-based configurations:

```
Input → Processor → Storage → Graph → Gateway
  │         │          │        │        │
 UDP    iot_sensor  ObjectStore KV+   GraphQL
 File   document    (raw docs)  Indexes  MCP
```

| Component Type | Examples | Role |
|----------------|----------|------|
| Input | UDP, WebSocket, File | Ingest external data |
| Processor | Graph, JSONMap, Rule | Transform and enrich |
| Output | File, HTTPPost, WebSocket | Export data |
| Storage | ObjectStore | Persist to NATS JetStream |
| Gateway | HTTP, GraphQL, MCP | Expose query APIs |

All state lives in NATS JetStream KV buckets—portable, syncable, queryable.

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
- **Observable** — full trajectory capture for debugging

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

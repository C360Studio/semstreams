# SemStreams Documentation

## Start Here

**New to SemStreams?** Start with the basics:

**Setting up your environment?** Start with [Prerequisites](basics/00-prerequisites.md) for Go, Docker, and NATS setup.

1. [What is SemStreams?](basics/01-what-is-semstreams.md) - Overview and capabilities
2. [Architecture](basics/02-architecture.md) - System components and data flow
3. [Graphable Interface](basics/03-graphable-interface.md) - Core interface for graph entities

## Documentation Structure

| Folder | Purpose | Audience |
|--------|---------|----------|
| [basics/](basics/) | Getting started, core interfaces, first processor | New users |
| [concepts/](concepts/) | Background knowledge, algorithms, theory | Learning fundamentals |
| [advanced/](advanced/) | Clustering, LLM, performance, GraphQL, rules engine | Production tuning |
| [operations/](operations/) | Local monitoring, deployment, observability | Operators |
| [contributing/](contributing/) | Development, testing, CI | Contributors |

## Learning Paths

**Go/streaming developers** (familiar with NATS, event-driven):
1. [Basics](basics/) - Core interfaces and processors
2. [KV Twofer](concepts/02-kv-twofer.md) - State store and event bus in one
3. [Streams vs KV Watches](concepts/03-streams-vs-kv-watches.md) - Choosing the right primitive
4. [Embeddings](concepts/05-embeddings.md) - Vectors and semantic similarity
5. [GraphRAG Pattern](concepts/09-graphrag-pattern.md) - Community-based retrieval

**ML/LLM developers** (familiar with models and vectors):
1. [Event-Driven Basics](concepts/01-event-driven-basics.md) - Pub/sub, streams
2. [Knowledge Graphs](concepts/04-knowledge-graphs.md) - Triples, SPO model
3. [KV Twofer](concepts/02-kv-twofer.md) - How state and events unify
4. [Graphable Interface](basics/03-graphable-interface.md) - Implementing entity types
5. [First Processor](basics/05-first-processor.md) - Complete working example

**Production operators**:
1. [Local Monitoring](operations/01-local-monitoring.md) - Prometheus + Grafana setup
2. [Configuration](basics/06-configuration.md) - Capability tiers
3. [Clustering](advanced/01-clustering.md) - LPA tuning
4. [Performance](advanced/03-performance.md) - Optimization strategies

## Quick Reference

- [Index Reference](advanced/05-index-reference.md) - The seven indexes
- [Rules Engine](advanced/06-rules-engine.md) - Condition-based actions
- [Community Detection](concepts/07-community-detection.md) - LPA algorithm
- [Query Access](concepts/11-query-access.md) - GraphQL and MCP gateway
- [Governed Semantic State](concepts/28-governed-semantic-state.md) - Ownership for shared graph facts
- [Raw-Lane Plus Current-State Projection](concepts/29-raw-lane-and-current-state-projection.md) - Bounded raw lanes + governed current-state projection for high-rate feeds

## Vocabulary & Standards

- [Vocabulary Guide](basics/04-vocabulary.md) - Predicate design, registration, alias resolution
- [RDF Export](../vocabulary/export/doc.go) - Serialize triples to Turtle, N-Triples, JSON-LD
- [Vocabulary Package](../vocabulary/README.md) - Full API reference, IRI mappings, ontology subpackages

## Agentic Systems

Build LLM-powered autonomous agents with tool use:

- [Agentic Quickstart](basics/07-agentic-quickstart.md) - Get started with agents
- [Agentic Systems](concepts/13-agentic-systems.md) - Concepts: loops, state machine, tools, trajectories
- [Agentic Components](advanced/08-agentic-components.md) - Reference: loop, model, and tools processors
- [Payload Registry](concepts/15-payload-registry.md) - Polymorphic JSON serialization pattern
- [JetStream Tuning](advanced/11-jetstream-tuning.md) - AckWait, backpressure, and heartbeats for agentic consumers

## Workflow Orchestration

Multi-step processes with loops, timeouts, and retries:

- [Workflow Quickstart](basics/08-workflow-quickstart.md) - Get started with workflows
- [Workflow Configuration](advanced/09-workflow-configuration.md) - Complete schema reference
- [Orchestration Layers](concepts/14-orchestration-layers.md) - When to use rules vs. workflows

## Operations

- [Local Monitoring](operations/01-local-monitoring.md) - Prometheus + Grafana setup
- [Troubleshooting](operations/02-troubleshooting.md) - Common issues and solutions

## Federation

Ingesting graph entities from external sources (semsource, other sem* services):

- [Federation](concepts/16-federation.md) - Ingestion patterns, entity namespacing, flow configuration

## Optional Framework Adapters

Optional adapters are selected explicitly and must satisfy the ownership and admission rules in
[ADR-075](adr/075-framework-package-admission-and-composition.md). OpenTelemetry is currently the only optional
framework adapter. Product protocols and standards are composed by their owning product binaries rather than by the
SemStreams production binary.

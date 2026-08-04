// Package semstreams provides a stream processor that builds semantic knowledge graphs
// from event data, with automatic community detection and progressive AI enhancement.
//
// # Overview
//
// SemStreams transforms event streams into a living knowledge graph stored in NATS KV.
// You define a vocabulary of predicates, implement a simple interface, and the system
// maintains entities, relationships, indexes, and communities automatically.
//
// Key characteristics:
//   - Edge-first: Deploy on a Raspberry Pi with just NATS, or scale to clusters
//   - Offline-capable: NATS JetStream provides local persistence and sync
//   - Progressive: Start with rules, add search, then embeddings and LLM as needed
//   - Domain-driven: No mandatory AI dependencies—you enable what you need
//
// # Core Concept
//
//	Events → Graphable Interface → Knowledge Graph → Queries
//
// 1. Events arrive (telemetry, records, notifications)
// 2. Your processor transforms them into entities with triples
// 3. SemStreams maintains the graph, indexes, and communities
// 4. Query by relationships, predicates, or semantic similarity
//
// # The Graphable Interface
//
// Your domain types implement Graphable to become graph entities:
//
//	type Graphable interface {
//	    EntityID() string          // 6-part federated identifier
//	    Triples() []message.Triple // Facts about this entity
//	}
//
// Example:
//
//	func (d *DroneTelemetry) EntityID() string {
//	    return fmt.Sprintf("acme.ops.robotics.gcs.drone.%s", d.DroneID)
//	}
//
//	func (d *DroneTelemetry) Triples() []message.Triple {
//	    id := d.EntityID()
//	    return []message.Triple{
//	        {Subject: id, Predicate: "drone.telemetry.battery", Object: d.Battery},
//	        {Subject: id, Predicate: "fleet.membership.current", Object: d.FleetID},
//	    }
//	}
//
// # Entity ID Format
//
// Use 6-part hierarchical identifiers for federation and queryability:
//
//	org.platform.domain.system.type.instance
//
// Example: acme.ops.robotics.gcs.drone.001
//
// # Predicates
//
// Predicates follow domain.category.property format:
//
//	sensor.measurement.celsius
//	geo.location.zone
//	fleet.membership.current
//
// Dotted notation enables NATS wildcard queries (sensor.measurement.*) and
// provides SQL-like query semantics via prefix matching.
//
// # Progressive Enhancement (Tiers)
//
// SemStreams supports three capability tiers:
//
//	Tier 0: Rules engine, explicit relationships, structural indexing (NATS only)
//	Tier 1: + BM25 search, statistical communities (+ search index)
//	Tier 2: + Neural embeddings, LLM summaries (+ embedding service, LLM)
//
// Start with Tier 0. Add capabilities as resources allow.
//
// # Architecture
//
// Components connect via NATS subjects in flow-based configurations:
//
//	Input → Processor → Storage → Graph → Gateway
//	  │         │          │        │        │
//	 UDP    iot_sensor  ObjectStore KV+   GraphQL
//	 File   document    (raw docs)  Indexes  MCP
//
// Component types:
//   - Input: UDP, WebSocket, File - ingest external data
//   - Processor: Graph, JSONMap, Rule - transform and enrich
//   - Output: File, HTTPPost, WebSocket - export data
//   - Storage: ObjectStore - persist to NATS JetStream
//   - Gateway: HTTP, GraphQL, MCP - expose query APIs
//
// # State: NATS KV Buckets
//
// All state lives in NATS JetStream KV buckets:
//
// Core buckets (always created):
//   - ENTITY_STATES: Entity records with triples and version
//   - PREDICATE_INDEX: Predicate → entity IDs
//   - INCOMING_INDEX: Entity ID → referencing entities
//   - OUTGOING_INDEX: Entity ID → referenced entities
//   - ALIAS_INDEX: Alias → entity ID
//   - SPATIAL_INDEX: Geohash → entity IDs
//   - TEMPORAL_INDEX: Time bucket → entity IDs
//   - RULE_STATE: Rule evaluation state per entity
//
// Optional buckets (created when features enabled):
//   - EMBEDDING_INDEX: Entity ID → embedding vector
//   - COMMUNITY_INDEX: Community records with members and summaries
//
// # Package Organization
//
// Core packages:
//   - component: Component lifecycle, registry, port definitions
//   - componentregistry: Registration of all component types
//   - engine: Component orchestration and lifecycle
//   - flowstore: Flow persistence (NATS KV)
//   - config: Configuration loading and validation
//
// Graph packages:
//   - graph: Knowledge graph processing core
//   - message: Triple and entity message types
//   - vocabulary: Predicate definitions and standards
//
// Infrastructure:
//   - natsclient: NATS connection management
//   - gateway: HTTP, GraphQL, MCP API endpoints
//   - service: Discovery, flow-builder, metrics services
//   - metric: Prometheus metrics
//   - health: Health check system
//
// Components:
//   - input/: UDP, WebSocket, File inputs
//   - output/: File, HTTPPost, WebSocket outputs
//   - processor/: Graph, JSONMap, JSONFilter, Rule processors
//   - storage/: ObjectStore for raw document persistence
//
// Utilities:
//   - pkg/buffer: Ring buffer for streaming
//   - pkg/cache: LRU caching
//   - pkg/retry: Retry policies
//   - pkg/worker: Worker pools
//
// # Usage
//
// Build and run:
//
//	task build
//	./bin/semstreams --config configs/semantic-flow.json
//
// The binary uses componentregistry.Register() to register all component types.
// Flow configuration determines which components are instantiated.
//
// # Documentation
//
// See docs/ for comprehensive documentation:
//   - docs/basics/: Getting started, core interfaces
//   - docs/concepts/: Background knowledge, algorithms
//   - docs/advanced/: Clustering, LLM, performance tuning
//   - docs/rules/: Rules engine reference
//   - docs/contributing/: Development, testing, CI
//
// # Version
//
// Current: v0.5.0-alpha
package semstreams

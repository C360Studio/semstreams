// Package graphingest provides the graph-ingest component for entity and triple ingestion.
//
// # Overview
//
// The graph-ingest component is responsible for ingesting entities and triples into the
// graph system. It subscribes to JetStream subjects for incoming entity data and stores
// them in the ENTITY_STATES KV bucket.
//
// # Tier
//
// Tier: ALL TIERS (Tier 0, 1, 2) - Required for all deployments.
//
// # Architecture
//
// graph-ingest is a core component required for all deployment tiers (Structural,
// Statistical, Semantic). It serves as the entry point for all entity data flowing
// into the graph subsystem.
//
//	                    ┌─────────────────┐
//	objectstore.stored ─┤                 │
//	                    │  graph-ingest   ├──► ENTITY_STATES (KV)
//	sensor.processed   ─┤                 │
//	                    └─────────────────┘
//
// # Features
//
//   - Entity CRUD operations (create, read, update, delete)
//   - Triple mutations (add, remove)
//   - Hierarchy inference (optional) - creates container entities based on 6-part entity ID structure
//   - JetStream subscription with at-least-once delivery semantics
//   - KV storage with atomic updates
//
// # Configuration
//
// The component is configured via JSON with the following structure:
//
//	{
//	  "ports": {
//	    "inputs": [
//	      {"name":"objectstore_in","config":{"kind":"jetstream","subjects":["objectstore.stored.entity"]}},
//	      {"name":"sensor_in","config":{"kind":"jetstream","subjects":["sensor.processed.entity"]}}
//	    ],
//	    "outputs": [
//	      {"name":"entity_states","config":{"kind":"kv-write","bucket":"ENTITY_STATES"}}
//	    ]
//	  },
//	  "enable_hierarchy": true
//	}
//
// # Port Definitions
//
// Inputs:
//   - JetStream subscriptions for entity events (objectstore.stored.entity, sensor.processed.entity)
//
// Outputs:
//   - KV bucket: ENTITY_STATES - stores entity state with triples
//
// # Hierarchy Inference
//
// When enable_hierarchy is true, the component automatically creates container entities
// based on the 6-part entity ID structure (org.platform.system.domain.type.instance).
// This creates edges for:
//   - Type containers (*.group)
//   - System containers (*.container)
//   - Domain containers (*.level)
//
// # Usage
//
// Register the component with the component registry:
//
//	import graphingest "github.com/c360studio/semstreams/processor/graph-ingest"
//
//	func init() {
//	    graphingest.Register(registry)
//	}
//
// # Dependencies
//
// This component has no upstream graph component dependencies. It is the entry point
// for entity data and other graph components (graph-index, graph-embedding, etc.)
// watch its output KV bucket.
package graphingest

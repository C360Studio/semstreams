// Package graphclustering provides the graph-clustering component for community detection,
// structural analysis, and anomaly detection.
//
// # Overview
//
// The graph-clustering component performs community detection on the entity graph
// using Label Propagation Algorithm (LPA), computes ephemeral k-core and pivot
// inputs for enabled anomaly detection, and detects anomalies within community contexts. Optionally enhances
// community descriptions using LLM.
//
// # Tier
//
// Tier: STATISTICAL (Tier 1) without LLM, SEMANTIC (Tier 2) with LLM enhancement.
// Not used in Structural (Tier 0) deployments.
//
// # Architecture
//
// graph-clustering is a Tier 1+ component. It polls ENTITY_STATES on a timer
// and runs community detection each cycle; it holds no live ENTITY_STATES
// watcher (the declared kv-watch input port is discovery metadata).
// Authoritative entity bytes are contract-validated at consume time, and a
// poisoned value latches a sticky whole-view reset-required projection state.
//
//	                    ┌───────────────────┐
//	ENTITY_STATES ─────►│                   │
//	   (KV reads)       │  graph-clustering ├──► COMMUNITY_INDEX (KV)
//	                    │                   ├──► ANOMALY_INDEX (KV)
//	                    └─────────┬─────────┘
//	                              │ (reads)
//	              ┌───────────────┼───────────────┐
//	              ▼               ▼               ▼
//	       OUTGOING_INDEX  INCOMING_INDEX  graph-embedding
//	                                       (query path)
//
// # Features
//
//   - Label Propagation Algorithm (LPA) for community detection
//   - Configurable detection interval and batch thresholds
//   - Optional LLM-based community summarization
//   - Same-cycle structural computation for anomaly detection
//   - Anomaly detection within community contexts
//   - Semantic gap detection via graph-embedding query path
//
// # Configuration
//
// The component is configured via JSON with the following structure:
//
//	{
//	  "ports": {
//	    "inputs": [
//	      {"name":"entity_watch","config":{"kind":"kv-watch","bucket":"ENTITY_STATES"}}
//	    ],
//	    "outputs": [
//	      {"name":"communities","config":{"kind":"kv-write","bucket":"COMMUNITY_INDEX"}},
//	      {"name":"anomalies","config":{"kind":"kv-write","bucket":"ANOMALY_INDEX"}}
//	    ]
//	  },
//	  "detection_interval": "30s",
//	  "batch_size": 100,
//	  "enable_llm": true,
//	  "min_community_size": 2,
//	  "max_iterations": 100,
//	  "enable_anomaly_detection": true,
//	  "anomaly_config": {
//	    "enabled": true,
//	    "core_anomaly": {"enabled": true, "min_core_for_hub_analysis": 2},
//	    "semantic_gap": {"enabled": true, "min_semantic_similarity": 0.7},
//	    "virtual_edges": {
//	      "auto_apply": {"enabled": false, "min_confidence": 0.95},
//	      "review_queue": {"enabled": false, "min_confidence": 0.7, "max_confidence": 0.95}
//	    }
//	  }
//	}
//
// # Detection Cycle
//
// When triggered, the component runs through these phases:
//
//  1. Community Detection (LPA) → COMMUNITY_INDEX
//  2. Ephemeral structural prerequisites (when anomaly detection initialized)
//  3. Anomaly Detection (if enabled) → ANOMALY_INDEX
//
// # Scheduling
//
// Community detection runs on a timer, every detection_interval. (batch_size
// is accepted in config but batch-threshold triggering is not implemented.)
//
// # Port Definitions
//
// Inputs:
//   - KV reads: ENTITY_STATES - polled during each detection cycle (the
//     kv-watch port declaration is discovery metadata; no watcher is held)
//
// Outputs:
//   - KV bucket: COMMUNITY_INDEX - stores detected communities
//   - KV bucket: ANOMALY_INDEX - stores detected anomalies
//
// # Usage
//
// Register the component with the component registry:
//
//	import graphclustering "github.com/c360studio/semstreams/processor/graph-clustering"
//
//	func init() {
//	    graphclustering.Register(registry)
//	}
//
// # Dependencies
//
// Upstream (reads during detection):
//   - graph-ingest: writes ENTITY_STATES (authoritative entity state)
//   - graph-index: reads OUTGOING_INDEX and INCOMING_INDEX for graph structure
//   - graph-embedding: queries for similar entities via NATS request/reply
//
// Downstream:
//   - graph-query: reads COMMUNITY_INDEX (community cache → GraphRAG / search_graph)
//   - graph-gateway: reads ANOMALY_INDEX for the optional inference-review API
package graphclustering

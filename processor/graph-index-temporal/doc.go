// Package graphindextemporal provides the graph-index-temporal component for temporal indexing.
//
// # Overview
//
// The graph-index-temporal component watches the ENTITY_STATES KV bucket and maintains
// a time-based index for entities with temporal data. This enables efficient time-range
// queries like "find entities modified between dates" or "find entities by hour".
//
// # Architecture
//
// graph-index-temporal is an optional component that can be enabled for deployments
// requiring time-based query capabilities.
//
//	                    ┌──────────────────────┐
//	ENTITY_STATES ─────►│                      │
//	   (KV watch)       │  graph-index-temporal├──► TEMPORAL_INDEX (KV)
//	                    │                      │
//	                    └──────────────────────┘
//
// # Features
//
//   - Configurable time resolution (minute, hour, day)
//   - Automatic extraction of timestamps from entity data
//   - Batch processing for efficient index updates
//   - Time bucket keys for range queries
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
//	      {"name":"temporal_index","config":{"kind":"kv-write","bucket":"TEMPORAL_INDEX"}}
//	    ]
//	  },
//	  "time_resolution": "hour",
//	  "workers": 4,
//	  "batch_size": 100
//	}
//
// # Time Resolution
//
// The time_resolution setting controls the granularity of temporal indexing:
//
//   - minute: Index by minute (YYYY-MM-DDTHH:MM)
//   - hour: Index by hour (YYYY-MM-DDTHH) - default
//   - day: Index by day (YYYY-MM-DD)
//
// # Port Definitions
//
// Inputs:
//   - KV watch: ENTITY_STATES - watches for entity state changes
//
// Outputs:
//   - KV bucket: TEMPORAL_INDEX - time-based index
//
// # Usage
//
// Register the component with the component registry:
//
//	import graphindextemporal "github.com/c360studio/semstreams/processor/graph-index-temporal"
//
//	func init() {
//	    graphindextemporal.Register(registry)
//	}
//
// # Dependencies
//
// Upstream:
//   - graph-ingest: produces ENTITY_STATES that this component watches
//
// Downstream:
//   - graph-gateway: reads TEMPORAL_INDEX for time-range queries
package graphindextemporal

// Package graphindexspatial provides the graph-index-spatial component for spatial indexing.
//
// # Overview
//
// The graph-index-spatial component watches the ENTITY_STATES KV bucket and maintains
// a geospatial index for entities with location data. This enables efficient spatial
// queries like "find entities within radius" or "find entities in bounding box".
//
// # Architecture
//
// graph-index-spatial is an optional component that can be enabled for deployments
// requiring geospatial query capabilities.
//
//	                    ┌─────────────────────┐
//	ENTITY_STATES ─────►│                     │
//	   (KV watch)       │  graph-index-spatial├──► SPATIAL_INDEX (KV)
//	                    │                     │
//	                    └─────────────────────┘
//
// # Features
//
//   - Geohash-based spatial indexing with configurable precision
//   - Automatic extraction of location data from entity triples
//   - Batch processing for efficient index updates
//   - Support for point locations (lat/lon)
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
//	      {"name":"spatial_index","config":{"kind":"kv-write","bucket":"SPATIAL_INDEX"}}
//	    ]
//	  },
//	  "geohash_precision": 6,
//	  "workers": 4,
//	  "batch_size": 100
//	}
//
// # Geohash Precision
//
// The geohash_precision setting controls the granularity of spatial indexing:
//
//	Precision 1: ~5,000 km (continental)
//	Precision 2: ~1,250 km (large country)
//	Precision 3: ~156 km (state/province)
//	Precision 4: ~39 km (city)
//	Precision 5: ~4.9 km (neighborhood)
//	Precision 6: ~1.2 km (street) - default
//	Precision 7: ~153 m (block)
//	Precision 8: ~38 m (building)
//	Precision 9-12: increasingly fine precision
//
// # Port Definitions
//
// Inputs:
//   - KV watch: ENTITY_STATES - watches for entity state changes
//
// Outputs:
//   - KV bucket: SPATIAL_INDEX - geohash-based spatial index
//
// # Usage
//
// Register the component with the component registry:
//
//	import graphindexspatial "github.com/c360studio/semstreams/processor/graph-index-spatial"
//
//	func init() {
//	    graphindexspatial.Register(registry)
//	}
//
// # Dependencies
//
// Upstream:
//   - graph-ingest: produces ENTITY_STATES that this component watches
//
// Downstream:
//   - graph-gateway: reads SPATIAL_INDEX for spatial queries
package graphindexspatial

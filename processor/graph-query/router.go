package graphquery

import (
	"log/slog"
)

// StaticRouter routes queries to known graph component subjects.
// Routing is based on query type string, not runtime discovery.
type StaticRouter struct {
	routes map[string]string // queryType -> subject
	logger *slog.Logger
}

// NewStaticRouter creates a router with static routes for all known query types.
func NewStaticRouter(logger *slog.Logger) *StaticRouter {
	return &StaticRouter{
		routes: map[string]string{
			// Entity queries -> graph-ingest
			"entity":       "graph.ingest.query.entity",
			"entityBatch":  "graph.ingest.query.batch",
			"entityPrefix": "graph.ingest.query.prefix",

			// Relationship queries -> graph-index
			"outgoing":          "graph.index.query.outgoing",
			"incoming":          "graph.index.query.incoming",
			"alias":             "graph.index.query.alias",
			"predicate":         "graph.index.query.predicate",
			"predicateList":     "graph.index.query.predicateList",
			"predicateStats":    "graph.index.query.predicateStats",
			"predicateCompound": "graph.index.query.predicateCompound",
			"byName":            "graph.index.query.byName",

			// Spatial/Temporal -> specialized indexes
			"spatial":  "graph.spatial.query.bounds",
			"temporal": "graph.temporal.query.range",

			// ML-based queries -> optional components
			"semantic":  "graph.embedding.query.search",
			"similar":   "graph.embedding.query.similar",
			"community": "graph.clustering.query.community",
			"anomaly":   "graph.anomalies.query.detect",
		},
		logger: logger,
	}
}

// Route returns the NATS subject for a given query type.
// Returns empty string if the query type is unknown or receiver is nil.
func (r *StaticRouter) Route(queryType string) string {
	if r == nil {
		return ""
	}
	if subject, ok := r.routes[queryType]; ok {
		return subject
	}
	if r.logger != nil {
		r.logger.Warn("unknown query type", "type", queryType)
	}
	return ""
}

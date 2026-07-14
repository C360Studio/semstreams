// Package graph provides predicate query data types for the graph query system.
package graph

// --- Predicate Query Types ---

// PredicateSummary represents a predicate with its entity count.
type PredicateSummary struct {
	Predicate   string `json:"predicate"`
	EntityCount int    `json:"entity_count"`
}

// PredicateListData contains all predicates with their entity counts.
type PredicateListData struct {
	Predicates []PredicateSummary `json:"predicates"`
	Total      int                `json:"total"`
}

// PredicateListQuery is the request for graph.index.query.predicateList.
// Namespace, when set, scopes the listing to one exact domain or
// domain.category namespace (for example, "inferred.semantic" matches
// "inferred.semantic.high"). Omitted or empty means "list every predicate"
// (ADR-065).
type PredicateListQuery struct {
	Namespace string `json:"namespace,omitempty"`
}

// PredicateStatsData contains detailed statistics for a single predicate.
type PredicateStatsData struct {
	Predicate      string   `json:"predicate"`
	EntityCount    int      `json:"entity_count"`
	SampleEntities []string `json:"sample_entities"`
}

// CompoundPredicateQuery represents a query combining multiple predicates.
type CompoundPredicateQuery struct {
	Predicates []string `json:"predicates"`
	Operator   string   `json:"operator"` // "AND" or "OR"
	Limit      int      `json:"limit,omitempty"`
}

// CompoundPredicateData contains entities matching a compound predicate query.
type CompoundPredicateData struct {
	Entities []string `json:"entities"`
	Operator string   `json:"operator"`
	Matched  int      `json:"matched"`
}

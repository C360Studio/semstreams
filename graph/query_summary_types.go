// Package graph provides graph summary query data types.
package graph

// --- Graph Summary Query Types ---
//
// GraphSummary is the composite "what's in this graph" overview a
// caller (LLM agent or external dashboard) gets without knowing any
// entity IDs up-front. Solves the discovery chicken-and-egg problem:
// query_entity / query_relationships all need known IDs to start;
// graphSummary tells the caller which IDs and predicates are worth
// knowing.
//
// Composed server-side from existing primitives (entity prefix scan
// for type distribution, predicate-list index for predicate counts).
// Source distribution (Triple.Source aggregation) is deliberately
// deferred to V2 — computing it requires a write-time index or a
// full triple walk, neither of which fits a V1 budget; see the
// project_graph_tools_gateway_first_plan memory.

// SummaryRequest is the request shape for the graph summary
// query. Both fields are optional with sensible V1 defaults.
type SummaryRequest struct {
	// IncludePredicates toggles the predicate counts section.
	// Defaults to true. Setting false is useful when the caller
	// already has the predicate list cached or when the response
	// would otherwise exceed the gateway's body budget at large
	// scales.
	IncludePredicates bool `json:"include_predicates"`

	// EntitySampleLimit caps how many entity IDs the type-distribution
	// aggregation walks. Defaults to 2000. The resulting type counts
	// are approximate when the scan is truncated; the response sets
	// EntitySampleTruncated=true and TotalEntities reflects what was
	// scanned, not what the graph holds. V2 candidates: write-time
	// type-count index for exact counts.
	EntitySampleLimit int `json:"entity_sample_limit"`

	// ExamplesPerType caps how many sample entity IDs to retain per
	// type bucket in the response. Defaults to 2. Example IDs are
	// load-bearing for the discovery use case (semspec's bug log
	// confirms models invent IDs when no examples are provided);
	// keep this >= 1.
	ExamplesPerType int `json:"examples_per_type"`
}

// EntityTypeSummary represents one entity-type bucket — entities
// grouped by their 6-part ID's domain.system.type segments (parts
// 3.4.5 in 1-indexed terms; parts 2-4 in 0-indexed slice terms).
//
// Examples in this codebase:
//
//	acme.platform1.agent.agentic-loop.execution.<loopID>
//	→ type "agent.agentic-loop.execution"
//
//	acme.platform1.agent.web.observation.<hash>
//	→ type "agent.web.observation"
type EntityTypeSummary struct {
	// Type is the domain.system.type triple — the bucket name.
	Type string `json:"type"`

	// Count is how many entities in the scanned sample fall in this
	// bucket. Approximate when EntitySampleTruncated is set on the
	// parent SummaryData.
	Count int `json:"count"`

	// Examples is up to ExamplesPerType real entity IDs from this
	// bucket. The caller uses these as starting points for
	// query_entity / query_relationships without having to invent IDs.
	Examples []string `json:"examples"`
}

// SummaryData is the composite response. Caller-friendly text
// formatting happens at the tool-wrapper layer (agent tools / future
// MCP); this shape stays structured for non-LLM consumers (dashboards,
// audit, programmatic clients).
type SummaryData struct {
	// TotalEntities is the count of entities actually scanned (= sum
	// of EntityTypes counts). When EntitySampleTruncated is true,
	// the underlying graph holds more than this.
	TotalEntities int `json:"total_entities"`

	// EntitySampleTruncated is true when the entity scan hit
	// EntitySampleLimit before draining the bucket. The TotalEntities
	// and per-type counts are then approximate.
	EntitySampleTruncated bool `json:"entity_sample_truncated"`

	// EntityTypes is the sorted (highest-count-first) list of type
	// buckets discovered in the scanned sample.
	EntityTypes []EntityTypeSummary `json:"entity_types"`

	// Predicates is populated when IncludePredicates is true (default).
	// Each entry mirrors PredicateSummary — predicate name + the
	// entity count from the predicate index. Counts here are exact
	// (the predicate index maintains them at write time); they do
	// NOT inherit the entity-scan truncation.
	Predicates []PredicateSummary `json:"predicates,omitempty"`

	// PredicateTotal is len(Predicates). Convenience for callers
	// formatting "N predicates registered" lines.
	PredicateTotal int `json:"predicate_total,omitempty"`
}

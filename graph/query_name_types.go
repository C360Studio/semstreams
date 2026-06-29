package graph

// Types for deterministic name→ranked-IDs lookup (graph.query.byName, gh#376).

// NameMatch is one entity whose name/title matched a graph.query.byName lookup,
// carrying the signals a deterministic caller needs to calibrate trust (gh#376).
type NameMatch struct {
	// EntityID is the matched entity (an opaque handle for the caller).
	EntityID string `json:"entity_id"`
	// MatchedName is the name as stored on the entity (original case).
	MatchedName string `json:"matched_name"`
	// Predicate is the label predicate that carried the name (e.g. dc.terms.title).
	Predicate string `json:"predicate"`
	// ExactCase is true when MatchedName equals the query verbatim (case included);
	// such matches rank above case-folded-only matches.
	ExactCase bool `json:"exact_case"`
}

// NameData contains the ranked entities for a name lookup. Order IS the ranking:
// exact-case matches first, then by label-predicate salience, then entity ID.
type NameData struct {
	Matches []NameMatch `json:"matches"`
}

// NameIndexItem is one entity's claim on a name, stored in NAME_INDEX.
type NameIndexItem struct {
	EntityID  string `json:"entity_id"`
	Name      string `json:"name"`      // original-case name as stored on the entity
	Predicate string `json:"predicate"` // the label predicate carrying it
	Priority  int    `json:"priority"`  // label-predicate salience (lower = higher)
}

// NameIndexEntry is the NAME_INDEX value for one normalized (case-folded) name.
type NameIndexEntry struct {
	Name  string          `json:"name"` // the folded name (key source)
	Items []NameIndexItem `json:"items"`
}

package vocabulary

// Label (display-name) predicate registrations.
//
// These predicates carry human-readable names/titles. They are registered with
// AliasTypeLabel, which CanResolveToEntityID()==false — so the ALIAS_INDEX
// deliberately excludes them (a display name is ambiguous as an identity), while
// the NAME_INDEX keys on exactly this set for name→ranked-IDs lookup
// (graph.query.byName, gh#376 ask #5).
//
// The framework registers the one cross-product convention it knows is emitted
// (dc.terms.title). Products register their own label predicates the same way —
// Register(pred, WithAlias(AliasTypeLabel, priority)) — and the name index picks
// them up automatically via DiscoverLabelPredicates(). Lower priority = higher
// salience in name-match ranking.
func init() {
	Register(DCTermsTitle,
		WithDescription("Human-readable title/name of the entity (display label, not an identity)"),
		WithDataType("string"),
		WithIRI(DcTitle),
		WithAlias(AliasTypeLabel, labelPriorityTitle))
}

// Label-predicate salience priorities (lower = higher salience). A preferred
// label outranks a title, which outranks an alternate label, when the same name
// matches entities through different predicates.
const (
	labelPriorityPreferred = 0
	labelPriorityTitle     = 1
	labelPriorityAlternate = 2
)

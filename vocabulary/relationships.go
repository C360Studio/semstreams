package vocabulary

// Relationship predicate registration with metadata and IRI mappings.
// This file registers the graph.rel.* predicates defined in predicates.go
// with their semantic metadata and standard vocabulary mappings.

func init() {
	// GraphRelContains - Hierarchical containment
	Register(GraphRelContains,
		WithDescription("Hierarchical containment relationship (parent contains child)"),
		WithIRI(ProvHadMember))

	// GraphRelReferences - Directional reference
	Register(GraphRelReferences,
		WithDescription("Directional reference from subject to object"),
		WithIRI(DcReferences))

	// GraphRelInfluences - Causal/impact relationships
	Register(GraphRelInfluences,
		WithDescription("Causal or impact relationship (subject influences object)"))

	// GraphRelCommunicates - Communication/interaction
	Register(GraphRelCommunicates,
		WithDescription("Communication or interaction relationship"))

	// GraphRelNear - Spatial proximity
	Register(GraphRelNear,
		WithDescription("Spatial proximity relationship"))

	// GraphRelTriggeredBy - Event causation
	Register(GraphRelTriggeredBy,
		WithDescription("Event causation (subject triggered by object)"))

	// GraphRelDependsOn - Dependencies
	Register(GraphRelDependsOn,
		WithDescription("Dependency relationship (subject depends on object)"),
		WithIRI(DcRequires))

	// GraphRelImplements - Implementation relationships
	Register(GraphRelImplements,
		WithDescription("Implementation relationship (subject implements object)"))

	// GraphRelDiscusses - Discussion/commentary
	Register(GraphRelDiscusses,
		WithDescription("Discussion or commentary relationship"),
		WithIRI(SchemaAbout))

	// GraphRelSupersedes - Replacement/versioning
	Register(GraphRelSupersedes,
		WithDescription("Replacement or versioning relationship (subject supersedes object)"),
		WithIRI(DcReplaces))

	// GraphRelBlockedBy - Blocking relationships
	Register(GraphRelBlockedBy,
		WithDescription("Blocking relationship (subject blocked by object)"))

	// GraphRelRelatedTo - General association
	Register(GraphRelRelatedTo,
		WithDescription("General association relationship"),
		WithIRI(DcRelation))
}

package vocabulary

// Hierarchy predicate registration with inverse relationships and SKOS mappings.
// This file registers the hierarchy.*.* predicates defined in predicates.go
// with their semantic metadata, inverse relationships, and standard IRIs.
//
// Hierarchy predicates represent relationships derived from 6-part entity ID structure:
//   - Domain level (3-part prefix): hierarchy.domain.member/contains
//   - System level (4-part prefix): hierarchy.system.member/contains
//   - Type level (5-part prefix): hierarchy.type.member/contains/sibling
//
// These predicates use SKOS vocabulary mappings:
//   - member predicates map to skos:broader (entity is narrower than container)
//   - contains predicates map to skos:narrower (container has narrower entities)
//   - sibling predicates map to skos:related (symmetric association)

func init() {
	// Domain-level hierarchy (3-part prefix)

	// HierarchyDomainMember - entity → domain
	Register(HierarchyDomainMember,
		WithDescription("Entity belongs to a domain (3-part prefix match)"),
		WithDataType(DataTypeString),
		WithIRI(SkosBroader),
		WithInverseOf(HierarchyDomainContains))

	// HierarchyDomainContains - domain → entity (inverse)
	Register(HierarchyDomainContains,
		WithDescription("Domain contains entity members (inverse of hierarchy.domain.member)"),
		WithDataType(DataTypeString),
		WithIRI(SkosNarrower),
		WithInverseOf(HierarchyDomainMember))

	// System-level hierarchy (4-part prefix)

	// HierarchySystemMember - entity → system
	Register(HierarchySystemMember,
		WithDescription("Entity belongs to a system (4-part prefix match)"),
		WithDataType(DataTypeString),
		WithIRI(SkosBroader),
		WithInverseOf(HierarchySystemContains))

	// HierarchySystemContains - system → entity (inverse)
	Register(HierarchySystemContains,
		WithDescription("System contains entity members (inverse of hierarchy.system.member)"),
		WithDataType(DataTypeString),
		WithIRI(SkosNarrower),
		WithInverseOf(HierarchySystemMember))

	// Type-level hierarchy (5-part prefix)

	// HierarchyTypeMember - entity → type container
	Register(HierarchyTypeMember,
		WithDescription("Entity belongs to a type container (5-part prefix + .group)"),
		WithDataType(DataTypeString),
		WithIRI(SkosBroader),
		WithInverseOf(HierarchyTypeContains))

	// HierarchyTypeContains - type container → entity (inverse)
	Register(HierarchyTypeContains,
		WithDescription("Type container contains entity members (inverse of hierarchy.type.member)"),
		WithDataType(DataTypeString),
		WithIRI(SkosNarrower),
		WithInverseOf(HierarchyTypeMember))

	// HierarchyTypeSibling - symmetric relationship between entities of same type
	Register(HierarchyTypeSibling,
		WithDescription("Entities share the same type (5-part prefix match, symmetric)"),
		WithDataType(DataTypeString),
		WithIRI(SkosRelated),
		WithSymmetric(true))
}

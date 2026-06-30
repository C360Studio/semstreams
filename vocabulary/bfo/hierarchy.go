package bfo

import "slices"

// BFO 2020 subclass (is-a) hierarchy.
//
// SubClassOf encodes the direct superclass of each BFO class IRI, so a consumer
// can compute ontology distance / subsumption without re-deriving the (fixed,
// standard) BFO tree per product (ADR-062 increment 5, gh#396 / semsource ask
// #1). It retires the per-consumer subclass stopgaps (e.g. semsource's
// source/ontology static subtree).
//
// Scope: CLASS IRIs only — BFO relations (partOf, hasParticipant, …) are object
// properties, not part of the subsumption tree, and are deliberately absent.
// Entity (the root) has no entry. Immaterial-entity intermediates that this
// package does not define as constants are collapsed: Site and SpatialRegion map
// directly to their nearest DEFINED ancestor (IndependentContinuant), which
// keeps IsA transitively correct at the cost of one level of granularity.

// SubClassOf maps a BFO class IRI to its direct superclass IRI. The root
// (Entity) is intentionally absent.
var SubClassOf = map[string]string{
	// Top level.
	Continuant: Entity,
	Occurrent:  Entity,

	// Continuant branch.
	IndependentContinuant:           Continuant,
	SpecificallyDependentContinuant: Continuant,
	GenericallyDependentContinuant:  Continuant,

	// Independent continuants.
	MaterialEntity:  IndependentContinuant,
	Object:          MaterialEntity,
	ObjectAggregate: MaterialEntity,
	FiatObjectPart:  MaterialEntity,
	// Immaterial entities (no ImmaterialEntity constant defined here → nearest
	// defined ancestor).
	Site:                          IndependentContinuant,
	SpatialRegion:                 IndependentContinuant,
	OneDimensionalSpatialRegion:   SpatialRegion,
	TwoDimensionalSpatialRegion:   SpatialRegion,
	ThreeDimensionalSpatialRegion: SpatialRegion,

	// Specifically dependent continuants.
	Quality:          SpecificallyDependentContinuant,
	RealizableEntity: SpecificallyDependentContinuant,
	Role:             RealizableEntity,
	Disposition:      RealizableEntity,
	Function:         Disposition,

	// Occurrent branch.
	Process:                       Occurrent,
	ProcessBoundary:               Occurrent,
	SpatiotemporalRegion:          Occurrent,
	TemporalRegion:                Occurrent,
	ZeroDimensionalTemporalRegion: TemporalRegion,
	OneDimensionalTemporalRegion:  TemporalRegion,
	History:                       Process, // BFO 2020: a history is a process
}

// Parents returns all transitive superclass IRIs of iri, nearest first, by
// walking SubClassOf to the root. Returns nil for the root (Entity) or an IRI
// not in the tree. The walk is cycle-guarded; a well-formed tree never cycles.
func Parents(iri string) []string {
	var out []string
	seen := map[string]bool{iri: true}
	cur := iri
	for {
		parent, ok := SubClassOf[cur]
		if !ok || seen[parent] {
			break
		}
		out = append(out, parent)
		seen[parent] = true
		cur = parent
	}
	return out
}

// IsA reports whether child is the same class as parent or a transitive subclass
// of it. IsA(x, x) is true (reflexive).
func IsA(child, parent string) bool {
	if child == parent {
		return true
	}
	return slices.Contains(Parents(child), parent)
}

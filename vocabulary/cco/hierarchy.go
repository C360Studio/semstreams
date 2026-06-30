package cco

import (
	"slices"

	"github.com/c360studio/semstreams/vocabulary/bfo"
)

// CCO subclass (is-a) hierarchy, anchored into BFO.
//
// SubClassOf encodes the direct superclass of each CCO class IRI (ADR-062
// increment 5, gh#396 / semsource ask #1). CCO classes ultimately subsume under
// BFO, so the roots of each CCO branch map to a BFO class IRI; Parents and IsA
// continue the walk up the BFO tree (vocabulary/bfo) once they cross the
// boundary, giving a single subsumption query across both ontologies.
//
// Scope: CLASS IRIs only — CCO relations (is_about, has_agent, …) are object
// properties and are absent. Parents are taken from published CCO v1.4 + BFO
// 2020, NOT from cco.go's prose (which had at least one wrong "extends BFO:X"
// note — Agent extends MaterialEntity, not Object). Some leaves here
// (Specification, Requirement, Rule, Identifier, SoftwareCode, the ActOf*
// specializations, …) are semstreams extensions over CCO proper, not upstream
// CCO classes; they are placed under their nearest real CCO ancestor — don't
// "correct" them against upstream CCO, which has no such class.

// SubClassOf maps a CCO class IRI to its direct superclass IRI — a CCO class for
// intermediate nodes, or a BFO class IRI where a CCO branch anchors into BFO.
var SubClassOf = map[string]string{
	// Information Content Entities anchor into BFO:GenericallyDependentContinuant.
	InformationContentEntity:            bfo.GenericallyDependentContinuant,
	DescriptiveInformationContentEntity: InformationContentEntity,
	DirectiveInformationContentEntity:   InformationContentEntity,
	DesignativeInformationContentEntity: InformationContentEntity,

	// Directive ICE subtypes.
	PlanSpecification: DirectiveInformationContentEntity,
	Specification:     DirectiveInformationContentEntity,
	Requirement:       DirectiveInformationContentEntity,
	Objective:         DirectiveInformationContentEntity,
	Rule:              DirectiveInformationContentEntity,
	Algorithm:         DirectiveInformationContentEntity,

	// Designative ICE subtypes.
	Identifier: DesignativeInformationContentEntity,
	Name:       DesignativeInformationContentEntity,

	// Descriptive ICE subtypes.
	MeasurementInformationContentEntity: DescriptiveInformationContentEntity,
	DescriptiveStatement:                DescriptiveInformationContentEntity,

	// Software code is an information content entity.
	SoftwareCode: InformationContentEntity,

	// Agents anchor into BFO:MaterialEntity (CCO v1.4: cco:Agent ⊑ BFO_0000040
	// MaterialEntity — NOT Object; a group of agents is an ObjectAggregate, an
	// Object sibling, so the branch must not subsume under Object).
	Agent:                    bfo.MaterialEntity,
	Person:                   Agent,
	Organization:             Agent,
	GroupOfAgents:            Agent,
	SoftwareAgent:            Agent,
	IntelligentSoftwareAgent: SoftwareAgent,

	// Acts anchor into BFO:Process.
	Act:                     bfo.Process,
	IntentionalAct:          Act,
	ActOfCommunication:      IntentionalAct,
	ActOfApproval:           IntentionalAct,
	ActOfArtifactProcessing: IntentionalAct,
	ActOfDecisionMaking:     IntentionalAct,
	ActOfObserving:          IntentionalAct,
	ActOfCommandAndControl:  IntentionalAct,
	ActOfPositionChange:     IntentionalAct,

	// Artifacts anchor into BFO:MaterialEntity.
	Artifact:                   bfo.MaterialEntity,
	InformationBearingArtifact: Artifact,
	Document:                   InformationBearingArtifact,
	Facility:                   Artifact,
	Vehicle:                    Artifact,
	Sensor:                     Artifact,
}

// Parents returns all transitive superclass IRIs of iri, nearest first. It walks
// the CCO map and, on crossing into a BFO class, continues up the BFO tree, so
// the result spans both ontologies. Returns nil for an IRI not in the tree.
func Parents(iri string) []string {
	var out []string
	seen := map[string]bool{iri: true}
	cur := iri
	for {
		parent, ok := SubClassOf[cur]
		if !ok {
			// cur is not a CCO class with a CCO/anchor parent — if it is a BFO
			// class, continue up the BFO tree.
			for _, p := range bfo.Parents(cur) {
				if !seen[p] {
					out = append(out, p)
					seen[p] = true
				}
			}
			break
		}
		if seen[parent] {
			break
		}
		out = append(out, parent)
		seen[parent] = true
		cur = parent
	}
	return out
}

// IsA reports whether child is the same class as parent or a transitive subclass
// of it, across the CCO→BFO boundary (so a CCO class IsA a BFO class). IsA(x, x)
// is true (reflexive).
func IsA(child, parent string) bool {
	if child == parent {
		return true
	}
	return slices.Contains(Parents(child), parent)
}

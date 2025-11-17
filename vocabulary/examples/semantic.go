// Package examples provides reference vocabulary implementations.
// These are NOT part of the core framework - they demonstrate how applications
// should define domain-specific vocabularies and register predicates.
//
// Applications should create similar packages in their own codebases.
package examples

import "github.com/c360/semstreams/vocabulary"

// Example semantic domain predicates for identity and labeling.
// In a real application, this would be in your application's vocabulary package.
const (
	// SemanticIdentityAlias - alternative entity identifier (owl:sameAs)
	SemanticIdentityAlias = "semantic.identity.alias"

	// SemanticIdentityUUID - universally unique identifier
	SemanticIdentityUUID = "semantic.identity.uuid"

	// SemanticLabelPreferred - preferred human-readable name (skos:prefLabel)
	SemanticLabelPreferred = "semantic.label.preferred"

	// SemanticLabelAlternate - alternative display name
	SemanticLabelAlternate = "semantic.label.alternate"
)

// RegisterSemanticVocabulary registers example semantic domain predicates.
// Applications would call this from their init() functions or during startup.
func RegisterSemanticVocabulary() {
	// Identity aliases - resolvable to entity IDs
	vocabulary.Register(SemanticIdentityAlias,
		vocabulary.WithDescription("Alternative entity identifier"),
		vocabulary.WithDataType("string"),
		vocabulary.WithAlias(vocabulary.AliasTypeIdentity, 0), // Highest priority
		vocabulary.WithIRI(vocabulary.OwlSameAs))

	vocabulary.Register(SemanticIdentityUUID,
		vocabulary.WithDescription("Universally unique identifier"),
		vocabulary.WithDataType("string"),
		vocabulary.WithAlias(vocabulary.AliasTypeIdentity, 1),
		vocabulary.WithIRI(vocabulary.DcIdentifier))

	// Display labels - NOT resolvable (ambiguous, display only)
	vocabulary.Register(SemanticLabelPreferred,
		vocabulary.WithDescription("Preferred human-readable name"),
		vocabulary.WithDataType("string"),
		vocabulary.WithAlias(vocabulary.AliasTypeLabel, 999),
		vocabulary.WithIRI(vocabulary.SkosPrefLabel))

	vocabulary.Register(SemanticLabelAlternate,
		vocabulary.WithDescription("Alternative display name"),
		vocabulary.WithDataType("string"),
		vocabulary.WithAlias(vocabulary.AliasTypeLabel, 999),
		vocabulary.WithIRI(vocabulary.SkosAltLabel))
}

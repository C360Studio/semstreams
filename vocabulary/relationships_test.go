package vocabulary

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestRelationshipPredicates(t *testing.T) {
	tests := []struct {
		name             string
		predicate        string
		expectedDomain   string
		expectedCategory string
		expectedIRI      string
	}{
		{
			name:             "GraphRelContains",
			predicate:        GraphRelContains,
			expectedDomain:   "graph",
			expectedCategory: "rel",
			expectedIRI:      ProvHadMember,
		},
		{
			name:             "GraphRelReferences",
			predicate:        GraphRelReferences,
			expectedDomain:   "graph",
			expectedCategory: "rel",
			expectedIRI:      DcReferences,
		},
		{
			name:             "GraphRelInfluences",
			predicate:        GraphRelInfluences,
			expectedDomain:   "graph",
			expectedCategory: "rel",
		},
		{
			name:             "GraphRelCommunicates",
			predicate:        GraphRelCommunicates,
			expectedDomain:   "graph",
			expectedCategory: "rel",
		},
		{
			name:             "GraphRelNear",
			predicate:        GraphRelNear,
			expectedDomain:   "graph",
			expectedCategory: "rel",
		},
		{
			name:             "GraphRelTriggeredBy",
			predicate:        GraphRelTriggeredBy,
			expectedDomain:   "graph",
			expectedCategory: "rel",
		},
		{
			name:             "GraphRelDependsOn",
			predicate:        GraphRelDependsOn,
			expectedDomain:   "graph",
			expectedCategory: "rel",
			expectedIRI:      DcRequires,
		},
		{
			name:             "GraphRelImplements",
			predicate:        GraphRelImplements,
			expectedDomain:   "graph",
			expectedCategory: "rel",
		},
		{
			name:             "GraphRelDiscusses",
			predicate:        GraphRelDiscusses,
			expectedDomain:   "graph",
			expectedCategory: "rel",
			expectedIRI:      SchemaAbout,
		},
		{
			name:             "GraphRelSupersedes",
			predicate:        GraphRelSupersedes,
			expectedDomain:   "graph",
			expectedCategory: "rel",
			expectedIRI:      DcReplaces,
		},
		{
			name:             "GraphRelBlockedBy",
			predicate:        GraphRelBlockedBy,
			expectedDomain:   "graph",
			expectedCategory: "rel",
		},
		{
			name:             "GraphRelRelatedTo",
			predicate:        GraphRelRelatedTo,
			expectedDomain:   "graph",
			expectedCategory: "rel",
			expectedIRI:      DcRelation,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Verify predicate is valid
			assert.True(t, IsValidPredicate(tt.predicate),
				"Predicate %s should be valid", tt.predicate)

			// Verify predicate is registered
			meta := GetPredicateMetadata(tt.predicate)
			require.NotNil(t, meta,
				"Predicate %s should be registered", tt.predicate)

			// Verify metadata
			assert.NotEmpty(t, meta.Description,
				"Predicate %s should have a description", tt.predicate)
			assert.Equal(t, tt.expectedDomain, meta.Domain,
				"Predicate %s should have domain %s", tt.predicate, tt.expectedDomain)
			assert.Equal(t, tt.expectedCategory, meta.Category,
				"Predicate %s should have category %s", tt.predicate, tt.expectedCategory)

			// Verify IRI mapping if expected
			if tt.expectedIRI != "" {
				assert.Equal(t, tt.expectedIRI, meta.StandardIRI,
					"Predicate %s should map to IRI %s", tt.predicate, tt.expectedIRI)
			}
		})
	}
}

func TestGetRelationshipPredicates(t *testing.T) {
	// Get all registered predicates and filter for graph.rel.*
	allPredicates := ListRegisteredPredicates()
	relPredicates := make([]string, 0)
	for _, pred := range allPredicates {
		meta := GetPredicateMetadata(pred)
		if meta != nil && meta.Domain == "graph" && meta.Category == "rel" {
			relPredicates = append(relPredicates, pred)
		}
	}

	// Should have all 12 relationship predicates
	assert.GreaterOrEqual(t, len(relPredicates), 12,
		"Should have at least 12 graph.rel.* predicates")

	// Verify specific predicates exist
	predicateMap := make(map[string]bool)
	for _, pred := range relPredicates {
		predicateMap[pred] = true
	}

	expectedPredicates := []string{
		GraphRelContains,
		GraphRelReferences,
		GraphRelInfluences,
		GraphRelCommunicates,
		GraphRelNear,
		GraphRelTriggeredBy,
		GraphRelDependsOn,
		GraphRelImplements,
		GraphRelDiscusses,
		GraphRelSupersedes,
		GraphRelBlockedBy,
		GraphRelRelatedTo,
	}

	for _, pred := range expectedPredicates {
		assert.True(t, predicateMap[pred],
			"Predicate %s should be in graph.rel category", pred)
	}
}

func TestRelationshipIRIMappings(t *testing.T) {
	tests := []struct {
		predicate   string
		expectedIRI string
	}{
		{GraphRelContains, ProvHadMember},
		{GraphRelReferences, DcReferences},
		{GraphRelDependsOn, DcRequires},
		{GraphRelDiscusses, SchemaAbout},
		{GraphRelSupersedes, DcReplaces},
		{GraphRelRelatedTo, DcRelation},
	}

	for _, tt := range tests {
		t.Run(tt.predicate, func(t *testing.T) {
			meta := GetPredicateMetadata(tt.predicate)
			require.NotNil(t, meta, "Predicate should be registered")

			assert.Equal(t, tt.expectedIRI, meta.StandardIRI,
				"Predicate %s should map to IRI %s", tt.predicate, tt.expectedIRI)
		})
	}
}

func TestRelationshipPredicateFormat(t *testing.T) {
	// All relationship predicates should follow graph.rel.* pattern
	allRelPredicates := []string{
		GraphRelContains,
		GraphRelReferences,
		GraphRelInfluences,
		GraphRelCommunicates,
		GraphRelNear,
		GraphRelTriggeredBy,
		GraphRelDependsOn,
		GraphRelImplements,
		GraphRelDiscusses,
		GraphRelSupersedes,
		GraphRelBlockedBy,
		GraphRelRelatedTo,
	}

	for _, pred := range allRelPredicates {
		assert.Contains(t, pred, "graph.rel.",
			"Relationship predicate %s should start with graph.rel.", pred)
	}
}

package vocabulary

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestHierarchyPredicatesRegistration(t *testing.T) {
	tests := []struct {
		name             string
		predicate        string
		expectedDomain   string
		expectedCategory string
		expectedIRI      string
	}{
		{
			name:             "HierarchyDomainMember",
			predicate:        HierarchyDomainMember,
			expectedDomain:   "hierarchy",
			expectedCategory: "domain",
			expectedIRI:      SkosBroader,
		},
		{
			name:             "HierarchyDomainContains",
			predicate:        HierarchyDomainContains,
			expectedDomain:   "hierarchy",
			expectedCategory: "domain",
			expectedIRI:      SkosNarrower,
		},
		{
			name:             "HierarchySystemMember",
			predicate:        HierarchySystemMember,
			expectedDomain:   "hierarchy",
			expectedCategory: "system",
			expectedIRI:      SkosBroader,
		},
		{
			name:             "HierarchySystemContains",
			predicate:        HierarchySystemContains,
			expectedDomain:   "hierarchy",
			expectedCategory: "system",
			expectedIRI:      SkosNarrower,
		},
		{
			name:             "HierarchyTypeMember",
			predicate:        HierarchyTypeMember,
			expectedDomain:   "hierarchy",
			expectedCategory: "type",
			expectedIRI:      SkosBroader,
		},
		{
			name:             "HierarchyTypeContains",
			predicate:        HierarchyTypeContains,
			expectedDomain:   "hierarchy",
			expectedCategory: "type",
			expectedIRI:      SkosNarrower,
		},
		{
			name:             "HierarchyTypeSibling",
			predicate:        HierarchyTypeSibling,
			expectedDomain:   "hierarchy",
			expectedCategory: "type",
			expectedIRI:      SkosRelated,
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
			assert.Equal(t, tt.expectedIRI, meta.StandardIRI,
				"Predicate %s should map to IRI %s", tt.predicate, tt.expectedIRI)
			assert.Equal(t, DataTypeString, meta.DataType,
				"Hierarchy predicates should have string datatype")
		})
	}
}

func TestHierarchyInversePredicates(t *testing.T) {
	tests := []struct {
		name            string
		predicate       string
		expectedInverse string
		isSymmetric     bool
	}{
		{
			name:            "HierarchyDomainMember has inverse",
			predicate:       HierarchyDomainMember,
			expectedInverse: HierarchyDomainContains,
			isSymmetric:     false,
		},
		{
			name:            "HierarchyDomainContains has inverse",
			predicate:       HierarchyDomainContains,
			expectedInverse: HierarchyDomainMember,
			isSymmetric:     false,
		},
		{
			name:            "HierarchySystemMember has inverse",
			predicate:       HierarchySystemMember,
			expectedInverse: HierarchySystemContains,
			isSymmetric:     false,
		},
		{
			name:            "HierarchySystemContains has inverse",
			predicate:       HierarchySystemContains,
			expectedInverse: HierarchySystemMember,
			isSymmetric:     false,
		},
		{
			name:            "HierarchyTypeMember has inverse",
			predicate:       HierarchyTypeMember,
			expectedInverse: HierarchyTypeContains,
			isSymmetric:     false,
		},
		{
			name:            "HierarchyTypeContains has inverse",
			predicate:       HierarchyTypeContains,
			expectedInverse: HierarchyTypeMember,
			isSymmetric:     false,
		},
		{
			name:            "HierarchyTypeSibling is symmetric",
			predicate:       HierarchyTypeSibling,
			expectedInverse: HierarchyTypeSibling, // symmetric = own inverse
			isSymmetric:     true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Verify GetInversePredicate returns expected value
			inverse := GetInversePredicate(tt.predicate)
			assert.Equal(t, tt.expectedInverse, inverse,
				"GetInversePredicate(%s) should return %s", tt.predicate, tt.expectedInverse)

			// Verify IsSymmetricPredicate
			assert.Equal(t, tt.isSymmetric, IsSymmetricPredicate(tt.predicate),
				"IsSymmetricPredicate(%s) should return %v", tt.predicate, tt.isSymmetric)

			// Verify HasInverse
			assert.True(t, HasInverse(tt.predicate),
				"HasInverse(%s) should return true", tt.predicate)

			// Verify metadata fields
			meta := GetPredicateMetadata(tt.predicate)
			require.NotNil(t, meta)

			if tt.isSymmetric {
				assert.True(t, meta.IsSymmetric,
					"Predicate %s should have IsSymmetric=true", tt.predicate)
			} else {
				assert.Equal(t, tt.expectedInverse, meta.InverseOf,
					"Predicate %s should have InverseOf=%s", tt.predicate, tt.expectedInverse)
			}
		})
	}
}

func TestInversePairConsistency(t *testing.T) {
	// Verify that inverse pairs are consistent (A's inverse is B, B's inverse is A)
	inversePairs := []struct {
		predicateA string
		predicateB string
	}{
		{HierarchyDomainMember, HierarchyDomainContains},
		{HierarchySystemMember, HierarchySystemContains},
		{HierarchyTypeMember, HierarchyTypeContains},
	}

	for _, pair := range inversePairs {
		t.Run(pair.predicateA+"<->"+pair.predicateB, func(t *testing.T) {
			// A's inverse should be B
			inverseOfA := GetInversePredicate(pair.predicateA)
			assert.Equal(t, pair.predicateB, inverseOfA,
				"Inverse of %s should be %s", pair.predicateA, pair.predicateB)

			// B's inverse should be A
			inverseOfB := GetInversePredicate(pair.predicateB)
			assert.Equal(t, pair.predicateA, inverseOfB,
				"Inverse of %s should be %s", pair.predicateB, pair.predicateA)
		})
	}
}

func TestDiscoverInversePredicates(t *testing.T) {
	inverses := DiscoverInversePredicates()

	// Should include all hierarchy predicates with inverses
	expectedPredicates := []string{
		HierarchyDomainMember,
		HierarchyDomainContains,
		HierarchySystemMember,
		HierarchySystemContains,
		HierarchyTypeMember,
		HierarchyTypeContains,
		HierarchyTypeSibling,
	}

	for _, pred := range expectedPredicates {
		inverse, exists := inverses[pred]
		assert.True(t, exists,
			"DiscoverInversePredicates should include %s", pred)
		assert.NotEmpty(t, inverse,
			"Inverse of %s should not be empty", pred)
	}

	// Verify symmetric predicate maps to itself
	assert.Equal(t, HierarchyTypeSibling, inverses[HierarchyTypeSibling],
		"Symmetric predicate should map to itself")
}

func TestGetInversePredicateForUnregistered(t *testing.T) {
	// Should return empty string for unregistered predicates
	inverse := GetInversePredicate("unregistered.predicate.name")
	assert.Empty(t, inverse, "Unregistered predicate should have no inverse")
}

func TestIsSymmetricPredicateForUnregistered(t *testing.T) {
	// Should return false for unregistered predicates
	assert.False(t, IsSymmetricPredicate("unregistered.predicate.name"),
		"Unregistered predicate should not be symmetric")
}

func TestHasInverseForUnregistered(t *testing.T) {
	// Should return false for unregistered predicates
	assert.False(t, HasInverse("unregistered.predicate.name"),
		"Unregistered predicate should not have inverse")
}

func TestPredicateWithoutInverse(t *testing.T) {
	// Sensor predicates should not have inverses
	meta := GetPredicateMetadata(SensorTemperatureCelsius)

	// The sensor predicate may not be registered by default
	// If it is registered, it should not have an inverse
	if meta != nil {
		assert.Empty(t, meta.InverseOf,
			"Sensor predicates should not have InverseOf set")
		assert.False(t, meta.IsSymmetric,
			"Sensor predicates should not be symmetric")
	}

	// GetInversePredicate should return empty for predicates without inverse
	inverse := GetInversePredicate(SensorTemperatureCelsius)
	assert.Empty(t, inverse,
		"GetInversePredicate should return empty for predicate without inverse")
}

func TestHierarchyPredicateFormat(t *testing.T) {
	// All hierarchy predicates should follow hierarchy.*.* pattern
	allHierarchyPredicates := []string{
		HierarchyDomainMember,
		HierarchyDomainContains,
		HierarchySystemMember,
		HierarchySystemContains,
		HierarchyTypeMember,
		HierarchyTypeContains,
		HierarchyTypeSibling,
	}

	for _, pred := range allHierarchyPredicates {
		assert.Contains(t, pred, "hierarchy.",
			"Hierarchy predicate %s should start with hierarchy.", pred)
		assert.True(t, IsValidPredicate(pred),
			"Predicate %s should be valid three-part format", pred)
	}
}

func TestSKOSMappingConsistency(t *testing.T) {
	// Member predicates should map to skos:broader
	memberPredicates := []string{
		HierarchyDomainMember,
		HierarchySystemMember,
		HierarchyTypeMember,
	}
	for _, pred := range memberPredicates {
		meta := GetPredicateMetadata(pred)
		require.NotNil(t, meta)
		assert.Equal(t, SkosBroader, meta.StandardIRI,
			"Member predicate %s should map to skos:broader", pred)
	}

	// Contains predicates should map to skos:narrower
	containsPredicates := []string{
		HierarchyDomainContains,
		HierarchySystemContains,
		HierarchyTypeContains,
	}
	for _, pred := range containsPredicates {
		meta := GetPredicateMetadata(pred)
		require.NotNil(t, meta)
		assert.Equal(t, SkosNarrower, meta.StandardIRI,
			"Contains predicate %s should map to skos:narrower", pred)
	}

	// Sibling should map to skos:related
	meta := GetPredicateMetadata(HierarchyTypeSibling)
	require.NotNil(t, meta)
	assert.Equal(t, SkosRelated, meta.StandardIRI,
		"Sibling predicate should map to skos:related")
}

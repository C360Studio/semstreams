package scenarios

import (
	"testing"

	"github.com/c360studio/semstreams/test/e2e/client"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestValidateHierarchyProvenanceRejectsMissingOrWrongContext(t *testing.T) {
	tests := []struct {
		name    string
		context string
	}{
		{name: "missing"},
		{name: "wrong", context: "source.wrong"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, _, err := validateHierarchyProvenance([]client.AuthorityTripleMatch{{
				EntityID: "acme.ops.robotics.gcs.drone.001",
				Triple: client.Triple{
					Predicate: "hierarchy.type.member",
					Context:   tt.context,
				},
			}})
			require.Error(t, err)
			assert.Contains(t, err.Error(), "hierarchy.type.member")
			assert.Contains(t, err.Error(), "inference.hierarchy")
		})
	}
}

func TestValidateHierarchyProvenanceRequiresEvidenceAndCountsEntities(t *testing.T) {
	_, _, err := validateHierarchyProvenance(nil)
	require.Error(t, err)

	entities, triples, err := validateHierarchyProvenance([]client.AuthorityTripleMatch{
		{
			EntityID: "acme.ops.robotics.gcs.drone.001",
			Triple:   client.Triple{Predicate: "hierarchy.type.member", Context: "inference.hierarchy"},
		},
		{
			EntityID: "acme.ops.robotics.gcs.drone.001",
			Triple:   client.Triple{Predicate: "hierarchy.system.member", Context: "inference.hierarchy"},
		},
	})
	require.NoError(t, err)
	assert.Equal(t, 1, entities)
	assert.Equal(t, 2, triples)
}

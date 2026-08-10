package graphquery

import (
	"testing"

	"github.com/c360studio/semstreams/graph/clustering"
	"github.com/stretchr/testify/assert"
)

func themedCommunity() *clustering.Community {
	return &clustering.Community{
		ID:                 entDrone1,
		Level:              0,
		Members:            []string{entDrone1, entDrone2},
		StatisticalSummary: "statistical baseline about drones",
	}
}

// resolveCommunitySummary returns the LLM summary on a join hit and the community's
// statistical summary on a miss — never an empty string.
func TestResolveCommunitySummary_TieredFloor(t *testing.T) {
	t.Parallel()

	comm := themedCommunity()

	t.Run("miss falls back to statistical, non-empty", func(t *testing.T) {
		c := &Component{communityCache: newTestCache()}
		got := c.resolveCommunitySummary(comm)
		assert.Equal(t, comm.StatisticalSummary, got)
		assert.NotEmpty(t, got, "a summary-less community degrades to the statistical floor, never empty")
	})

	t.Run("nil cache degrades to statistical floor", func(t *testing.T) {
		c := &Component{}
		assert.Equal(t, comm.StatisticalSummary, c.resolveCommunitySummary(comm))
	})
}

// Readiness is gated on the partition bucket ONLY: summary updates never flip
// readiness, and an empty summary store does not hold a partition-ready cache back.
func TestCommunityCache_ReadinessIsPartitionOnly(t *testing.T) {
	t.Parallel()

	c := newTestCache()
	assert.Nil(t, c.acquire(), "a fresh cache is not ready")

	// Partition initial sync completing makes the cache ready even with the summary
	// store contributing nothing to that decision.
	c.publish(newCommunityGeneration(1)) // mirrors the partition watch sentinel publication
	assert.NotNil(t, c.acquire(), "an empty/partial summary store never blocks partition-gated readiness")
}

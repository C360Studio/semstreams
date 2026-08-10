package graphquery

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/c360studio/semstreams/graph/clustering"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// putSummary drives the summary-watch update path with the exact bytes the worker
// writes: json.Marshal of a CommunitySummaryRecord under key {level}.{hash}.
func putSummary(t *testing.T, c *communityCache, rec *clustering.CommunitySummaryRecord) {
	t.Helper()
	data, err := json.Marshal(rec)
	require.NoError(t, err)
	c.handleSummaryUpdate(clustering.SummaryKey(rec.Level, rec.MembershipHash), data)
}

func themedCommunity() *clustering.Community {
	return &clustering.Community{
		ID:                 entDrone1,
		Level:              0,
		Members:            []string{entDrone1, entDrone2},
		StatisticalSummary: "statistical baseline about drones",
	}
}

// An llm-enhanced record for the community's exact membership surfaces via SummaryFor.
func TestCommunityCache_SummaryFor_EnhancedHit(t *testing.T) {
	t.Parallel()

	c := newTestCache()
	comm := themedCommunity()
	putSummary(t, c, &clustering.CommunitySummaryRecord{
		MembershipHash: clustering.MembershipHash(comm.Members),
		Level:          comm.Level,
		LLMSummary:     "an autonomous drone fleet coordinating a survey",
		Status:         clustering.SummaryStatusEnhanced,
		GeneratedAt:    time.Now(),
	})

	got, ok := c.summaryFor(comm)
	require.True(t, ok)
	assert.Equal(t, "an autonomous drone fleet coordinating a survey", got)
}

// A miss, a failed record, and an empty summary all return ok=false so the caller
// degrades to the statistical floor.
func TestCommunityCache_SummaryFor_MissAndFailedAndEmpty(t *testing.T) {
	t.Parallel()

	comm := themedCommunity()
	hash := clustering.MembershipHash(comm.Members)

	t.Run("no record", func(t *testing.T) {
		c := newTestCache()
		_, ok := c.summaryFor(comm)
		assert.False(t, ok)
	})

	t.Run("llm-failed record", func(t *testing.T) {
		c := newTestCache()
		putSummary(t, c, &clustering.CommunitySummaryRecord{
			MembershipHash: hash, Level: comm.Level, Status: clustering.SummaryStatusFailed, GeneratedAt: time.Now(),
		})
		_, ok := c.summaryFor(comm)
		assert.False(t, ok, "an llm-failed record is not a usable summary")
	})

	t.Run("enhanced but empty", func(t *testing.T) {
		c := newTestCache()
		putSummary(t, c, &clustering.CommunitySummaryRecord{
			MembershipHash: hash, Level: comm.Level, LLMSummary: "", Status: clustering.SummaryStatusEnhanced, GeneratedAt: time.Now(),
		})
		_, ok := c.summaryFor(comm)
		assert.False(t, ok, "an empty enhanced summary must not join")
	})

	t.Run("different membership does not join", func(t *testing.T) {
		c := newTestCache()
		putSummary(t, c, &clustering.CommunitySummaryRecord{
			MembershipHash: clustering.MembershipHash([]string{entDrone3}), // different members
			Level:          comm.Level,
			LLMSummary:     "unrelated",
			Status:         clustering.SummaryStatusEnhanced,
			GeneratedAt:    time.Now(),
		})
		_, ok := c.summaryFor(comm)
		assert.False(t, ok, "a summary for a different membership must not join")
	})
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

	t.Run("hit returns the joined LLM summary", func(t *testing.T) {
		cache := newTestCache()
		putSummary(t, cache, &clustering.CommunitySummaryRecord{
			MembershipHash: clustering.MembershipHash(comm.Members),
			Level:          comm.Level,
			LLMSummary:     "joined llm prose",
			Status:         clustering.SummaryStatusEnhanced,
			GeneratedAt:    time.Now(),
		})
		c := &Component{communityCache: cache}
		assert.Equal(t, "joined llm prose", c.resolveCommunitySummary(comm))
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

	// Feeding summary records must NOT flip readiness.
	putSummary(t, c, &clustering.CommunitySummaryRecord{
		MembershipHash: clustering.MembershipHash([]string{entDrone1, entDrone2}),
		Level:          0,
		LLMSummary:     "prose",
		Status:         clustering.SummaryStatusEnhanced,
		GeneratedAt:    time.Now(),
	})
	assert.Nil(t, c.acquire(), "summary updates must not drive readiness")

	// Partition initial sync completing makes the cache ready even with the summary
	// store contributing nothing to that decision.
	c.publish(newCommunityGeneration(1)) // mirrors the partition watch sentinel publication
	assert.NotNil(t, c.acquire(), "an empty/partial summary store never blocks partition-gated readiness")
}

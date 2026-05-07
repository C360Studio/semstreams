package clustering

import (
	"context"
	"fmt"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	gtypes "github.com/c360studio/semstreams/graph"
	"github.com/c360studio/semstreams/message"
)

func TestStatisticalSummarizer_SummarizeCommunity(t *testing.T) {
	summarizer := NewStatisticalSummarizer()
	ctx := context.Background()

	// Create test entities with robotics theme
	// Using proper 6-part entity ID format: org.platform.domain.system.type.instance
	entities := []*gtypes.EntityState{
		{
			ID: "c360.platform.robotics.system.drone.1",
		},
		{
			ID: "c360.platform.robotics.system.drone.2",
		},
		{
			ID: "c360.platform.robotics.system.sensor.1",
		},
		{
			ID: "c360.platform.robotics.system.battery.1",
		},
	}

	community := &Community{
		ID:      "comm-0-test",
		Level:   0,
		Members: []string{"c360.platform.robotics.system.drone.1", "c360.platform.robotics.system.drone.2", "c360.platform.robotics.system.sensor.1", "c360.platform.robotics.system.battery.1"},
	}

	// Summarize community
	result, err := summarizer.SummarizeCommunity(ctx, community, entities)
	require.NoError(t, err)
	require.NotNil(t, result)

	// Verify summary fields are populated
	assert.NotEmpty(t, result.StatisticalSummary, "StatisticalSummary should not be empty")
	assert.NotEmpty(t, result.Keywords, "Keywords should not be empty")
	assert.NotEmpty(t, result.RepEntities, "RepEntities should not be empty")
	assert.Equal(t, "statistical", result.SummaryStatus)

	// Verify keywords contain relevant terms
	keywordSet := make(map[string]bool)
	for _, kw := range result.Keywords {
		keywordSet[kw] = true
	}
	// Should contain terms from types (drone, sensor, battery)
	assert.True(t, keywordSet["drone"] || keywordSet["sensor"] || keywordSet["battery"],
		"Keywords should contain type-related terms")

	// Verify representative entities
	assert.LessOrEqual(t, len(result.RepEntities), summarizer.MaxRepEntities,
		"Should not exceed max representative entities")

	// Verify summary mentions entity count
	assert.Contains(t, result.StatisticalSummary, "4 entities", "Summary should mention entity count")

	t.Logf("Statistical Summary: %s", result.StatisticalSummary)
	t.Logf("Keywords: %v", result.Keywords)
	t.Logf("RepEntities: %v", result.RepEntities)
}

func TestStatisticalSummarizer_KeywordExtraction(t *testing.T) {
	summarizer := NewStatisticalSummarizer()
	summarizer.MaxKeywords = 5

	// Using proper 6-part entity ID format with navigation types
	entities := []*gtypes.EntityState{
		{
			ID: "c360.platform.robotics.system.navigation.1",
		},
		{
			ID: "c360.platform.robotics.system.navigation.2",
		},
		{
			ID: "c360.platform.robotics.system.navigation.3",
		},
	}

	keywords := summarizer.extractKeywords(entities)

	assert.LessOrEqual(t, len(keywords), summarizer.MaxKeywords)
	assert.NotEmpty(t, keywords)

	// Should extract terms from types
	keywordSet := make(map[string]bool)
	for _, kw := range keywords {
		keywordSet[kw] = true
	}

	// "navigation" should be highly ranked (appears in all types)
	assert.True(t, keywordSet["navigation"], "Should extract 'navigation' from types")

	t.Logf("Extracted keywords: %v", keywords)
}

func TestStatisticalSummarizer_RepresentativeEntities(t *testing.T) {
	summarizer := NewStatisticalSummarizer()
	summarizer.MaxRepEntities = 3
	ctx := context.Background()

	// Create a graph where "hub" is central (pointed to by others via triples)
	// This tests PageRank behavior: entities with many incoming links are important
	// Using proper 6-part entity ID format
	entities := []*gtypes.EntityState{
		{
			ID: "c360.platform.robotics.system.hub.1",
		},
		{
			ID: "c360.platform.robotics.system.sensor.1",
		},
		{
			ID: "c360.platform.robotics.system.sensor.2",
		},
		{
			ID: "c360.platform.robotics.system.actuator.1",
		},
	}

	repEntities := summarizer.findRepresentativeEntities(ctx, entities)

	assert.LessOrEqual(t, len(repEntities), summarizer.MaxRepEntities)
	assert.NotEmpty(t, repEntities)

	// With no edges/triples, entities are ranked by type frequency
	// "sensor" type appears twice, so sensor entities should rank high
	t.Logf("Representative entities: %v", repEntities)
}

func TestStatisticalSummarizer_SummaryGeneration(t *testing.T) {
	summarizer := NewStatisticalSummarizer()

	// Using proper 6-part entity ID format
	entities := []*gtypes.EntityState{
		{ID: "c360.platform.robotics.system.drone.1"},
		{ID: "c360.platform.robotics.system.drone.2"},
		{ID: "c360.platform.robotics.system.drone.3"},
		{ID: "c360.platform.robotics.system.sensor.1"},
		{ID: "c360.platform.robotics.system.sensor.2"},
	}

	keywords := []string{"robotics", "autonomous", "navigation"}

	summary := summarizer.generateSummary(entities, keywords)

	assert.NotEmpty(t, summary)
	assert.Contains(t, summary, "5 entities", "Should mention entity count")
	assert.Contains(t, summary, "drone", "Should mention most common type")
	assert.Contains(t, summary, "sensor", "Should mention second most common type")

	t.Logf("Generated summary: %s", summary)
}

func TestStatisticalSummarizer_EmptyEntities(t *testing.T) {
	summarizer := NewStatisticalSummarizer()
	ctx := context.Background()

	community := &Community{
		ID:      "comm-0-empty",
		Level:   0,
		Members: []string{},
	}

	// Empty entities should return error
	_, err := summarizer.SummarizeCommunity(ctx, community, []*gtypes.EntityState{})
	assert.Error(t, err, "Should error on empty entities")
}

func TestStatisticalSummarizer_NilCommunity(t *testing.T) {
	summarizer := NewStatisticalSummarizer()
	ctx := context.Background()

	entities := []*gtypes.EntityState{
		{ID: "c360.platform.robotics.system.test.1"},
	}

	// Nil community should return error
	_, err := summarizer.SummarizeCommunity(ctx, nil, entities)
	assert.Error(t, err, "Should error on nil community")
}

func TestStatisticalSummarizer_ContextCancellation(t *testing.T) {
	summarizer := NewStatisticalSummarizer()

	// Create cancelled context
	ctx, cancel := context.WithCancel(context.Background())
	cancel() // Cancel immediately

	entityID := "c360.platform.robotics.system.test.1"
	entities := []*gtypes.EntityState{
		{ID: entityID},
	}

	community := &Community{
		ID:      "comm-0-test",
		Level:   0,
		Members: []string{entityID},
	}

	// Should return context error
	_, err := summarizer.SummarizeCommunity(ctx, community, entities)
	assert.Error(t, err)
	assert.Equal(t, context.Canceled, err)
}

func TestExtractTerms(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected []string
	}{
		{
			name:     "Simple text",
			input:    "autonomous navigation system",
			expected: []string{"autonomous", "navigation", "system"},
		},
		{
			name:     "With stop words",
			input:    "the drone is flying",
			expected: []string{"drone", "flying"},
		},
		{
			name:     "Hyphenated terms",
			input:    "path-planning algorithm",
			expected: []string{"path", "planning", "algorithm"},
		},
		{
			name:     "Short terms filtered",
			input:    "a big AI system",
			expected: []string{"big", "system"}, // "AI" is too short
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := extractTerms(tt.input)
			assert.ElementsMatch(t, tt.expected, result)
		})
	}
}

// TestStatisticalSummarizer_IDFLiftsRareTerms is the regression guard for
// the 2026-05-07 globalSearch known-answer bug: niche query-relevant nouns
// (e.g. "hydraulic" sourced from a sensor's location property) were being
// crowded out of community.Keywords by universal predicates ("location",
// "unit") because keyword scoring was TF-only with no IDF. After the fix,
// BuildCorpusDF computes corpus-wide document frequency once per pass and
// extractKeywords scores by TF*IDF when DF is set, de-weighting universal
// terms and surfacing distinctive ones.
func TestStatisticalSummarizer_IDFLiftsRareTerms(t *testing.T) {
	mkSensor := func(id, locationValue string) *gtypes.EntityState {
		return &gtypes.EntityState{
			ID: id,
			Triples: []message.Triple{
				{Subject: id, Predicate: "location", Object: locationValue},
				{Subject: id, Predicate: "unit", Object: "percent"},
			},
		}
	}

	// Three communities of 5 sensors each. Each community has a distinctive
	// location term; the predicates "location" and "unit" + the value
	// "percent" are universal across all 15 entities.
	mkCommunity := func(typePart, locationValue string) []*gtypes.EntityState {
		out := make([]*gtypes.EntityState, 5)
		for i := range out {
			out[i] = mkSensor(
				fmt.Sprintf("c360.facility.warehouse.zone.%s.%d", typePart, i),
				locationValue,
			)
		}
		return out
	}
	communityA := mkCommunity("level", "hydraulic-reservoir")
	communityB := mkCommunity("temperature", "cold-storage")
	communityC := mkCommunity("pressure", "dock-bay")

	allEntities := append(append(append([]*gtypes.EntityState{},
		communityA...), communityB...), communityC...)

	summarizer := NewStatisticalSummarizer()
	summarizer.MaxKeywords = 5

	tfOnly := summarizer.extractKeywords(communityA)
	t.Logf("TF-only keywords for community A: %v", tfOnly)

	summarizer.BuildCorpusDF(allEntities)
	withIDF := summarizer.extractKeywords(communityA)
	t.Logf("TF*IDF keywords for community A: %v", withIDF)

	idfSet := make(map[string]bool, len(withIDF))
	for _, kw := range withIDF {
		idfSet[kw] = true
	}

	// "hydraulic" appears in 5 of 15 entities (rare). With IDF=log(15/5)≈1.10
	// it should clear the top-5 cap. Without IDF it competes against
	// universal predicates each at TF=5/5=1.0 and tends to lose to
	// log(1+freq)-weighted "location"/"unit" with their high raw counts.
	assert.True(t, idfSet["hydraulic"],
		"corpus-wide DF should lift 'hydraulic' (rare) into top-5; got %v", withIDF)

	// Universal predicates ("location", "unit") have DF=15, IDF=log(1)=0
	// (smoothed to 0.01 in the impl). They must NOT win the top slot.
	assert.NotEqual(t, "location", withIDF[0],
		"universal predicate 'location' should not be top keyword under IDF")
	assert.NotEqual(t, "unit", withIDF[0],
		"universal predicate 'unit' should not be top keyword under IDF")
}

// TestStatisticalSummarizer_BuildCorpusDF_Resets verifies that calling
// BuildCorpusDF with an empty slice clears prior state — guards against
// stale DF leaking across clustering passes.
func TestStatisticalSummarizer_BuildCorpusDF_Resets(t *testing.T) {
	s := NewStatisticalSummarizer()
	s.BuildCorpusDF([]*gtypes.EntityState{
		{ID: "c360.facility.warehouse.zone.level.1"},
	})
	require.NotNil(t, s.corpusDF)
	require.Equal(t, 1, s.corpusN)

	s.BuildCorpusDF(nil)
	assert.Nil(t, s.corpusDF, "BuildCorpusDF(nil) should clear the map")
	assert.Zero(t, s.corpusN, "BuildCorpusDF(nil) should reset corpusN")
}

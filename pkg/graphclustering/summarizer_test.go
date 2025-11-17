package graphclustering

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	gtypes "github.com/c360/semstreams/graph"
)

func TestStatisticalSummarizer_SummarizeCommunity(t *testing.T) {
	summarizer := NewStatisticalSummarizer()
	ctx := context.Background()

	// Create test entities with robotics theme
	entities := []*gtypes.EntityState{
		{
			Node: gtypes.NodeProperties{
				ID:   "drone1",
				Type: "robotics.drone",
				Properties: map[string]any{
					"name":       "Autonomous Drone A",
					"capability": "navigation",
				},
			},
			Edges: []gtypes.Edge{
				{ToEntityID: "sensor1", EdgeType: "uses"},
				{ToEntityID: "battery1", EdgeType: "powered_by"},
			},
		},
		{
			Node: gtypes.NodeProperties{
				ID:   "drone2",
				Type: "robotics.drone",
				Properties: map[string]any{
					"name":       "Autonomous Drone B",
					"capability": "surveillance",
				},
			},
			Edges: []gtypes.Edge{
				{ToEntityID: "sensor2", EdgeType: "uses"},
			},
		},
		{
			Node: gtypes.NodeProperties{
				ID:   "sensor1",
				Type: "robotics.sensor",
				Properties: map[string]any{
					"name": "LiDAR Sensor",
					"type": "distance",
				},
			},
			Edges: []gtypes.Edge{},
		},
		{
			Node: gtypes.NodeProperties{
				ID:   "battery1",
				Type: "robotics.battery",
				Properties: map[string]any{
					"name":  "Power Cell",
					"level": 85,
				},
			},
			Edges: []gtypes.Edge{},
		},
	}

	community := &Community{
		ID:      "comm-0-test",
		Level:   0,
		Members: []string{"drone1", "drone2", "sensor1", "battery1"},
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
	// Should contain terms from types
	assert.True(t, keywordSet["robotics"] || keywordSet["drone"] || keywordSet["sensor"],
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

	entities := []*gtypes.EntityState{
		{
			Node: gtypes.NodeProperties{
				ID:   "e1",
				Type: "navigation.autonomous",
				Properties: map[string]any{
					"algorithm": "path-planning",
					"sensor":    "fusion",
				},
			},
		},
		{
			Node: gtypes.NodeProperties{
				ID:   "e2",
				Type: "navigation.autonomous",
				Properties: map[string]any{
					"algorithm": "obstacle-avoidance",
					"sensor":    "lidar",
				},
			},
		},
		{
			Node: gtypes.NodeProperties{
				ID:   "e3",
				Type: "navigation.mapping",
				Properties: map[string]any{
					"algorithm": "slam",
				},
			},
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

	// Create a graph where "hub" is central (pointed to by others)
	// This tests PageRank behavior: entities with many incoming links are important
	entities := []*gtypes.EntityState{
		{
			Node: gtypes.NodeProperties{
				ID:   "hub",
				Type: "node",
			},
			// Hub has self-loop to maintain presence
			Edges: []gtypes.Edge{
				{ToEntityID: "hub", EdgeType: "self"},
			},
		},
		{
			Node: gtypes.NodeProperties{
				ID:   "e1",
				Type: "common_type",
			},
			// e1 points to hub (makes hub important in PageRank)
			Edges: []gtypes.Edge{
				{ToEntityID: "hub", EdgeType: "connects"},
			},
		},
		{
			Node: gtypes.NodeProperties{
				ID:   "e2",
				Type: "common_type",
			},
			// e2 points to hub
			Edges: []gtypes.Edge{
				{ToEntityID: "hub", EdgeType: "connects"},
			},
		},
		{
			Node: gtypes.NodeProperties{
				ID:   "e3",
				Type: "rare_type",
			},
			// e3 points to hub
			Edges: []gtypes.Edge{
				{ToEntityID: "hub", EdgeType: "connects"},
			},
		},
	}

	repEntities := summarizer.findRepresentativeEntities(ctx, entities)

	assert.LessOrEqual(t, len(repEntities), summarizer.MaxRepEntities)
	assert.NotEmpty(t, repEntities)

	// "hub" should be ranked first because it receives edges from all other entities
	// This is correct PageRank behavior
	assert.Contains(t, repEntities, "hub", "Entity with many incoming edges should be representative")
	if len(repEntities) > 0 {
		assert.Equal(t, "hub", repEntities[0], "Hub should be ranked first (highest PageRank)")
	}

	t.Logf("Representative entities: %v", repEntities)
}

func TestStatisticalSummarizer_SummaryGeneration(t *testing.T) {
	summarizer := NewStatisticalSummarizer()

	entities := []*gtypes.EntityState{
		{Node: gtypes.NodeProperties{ID: "d1", Type: "robotics.drone"}},
		{Node: gtypes.NodeProperties{ID: "d2", Type: "robotics.drone"}},
		{Node: gtypes.NodeProperties{ID: "d3", Type: "robotics.drone"}},
		{Node: gtypes.NodeProperties{ID: "s1", Type: "robotics.sensor"}},
		{Node: gtypes.NodeProperties{ID: "s2", Type: "robotics.sensor"}},
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
		{Node: gtypes.NodeProperties{ID: "e1", Type: "test"}},
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

	entities := []*gtypes.EntityState{
		{Node: gtypes.NodeProperties{ID: "e1", Type: "test"}},
	}

	community := &Community{
		ID:      "comm-0-test",
		Level:   0,
		Members: []string{"e1"},
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

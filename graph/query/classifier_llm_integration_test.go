//go:build integration

package query

import (
	"context"
	"net"
	"os"
	"testing"
	"time"

	"github.com/c360studio/semstreams/graph/llm"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// These integration tests require a running Ollama instance at localhost:11434.
// The model defaults to qwen3:1.7b but can be overridden with OLLAMA_TEST_MODEL.
//
// Run with: go test -tags integration -run TestLLMClassifier_Integration ./graph/query/...

func requireOllama(t *testing.T) {
	t.Helper()
	conn, err := net.DialTimeout("tcp", "localhost:11434", 2*time.Second)
	if err != nil {
		t.Skip("Ollama not available at localhost:11434, skipping")
	}
	conn.Close()
}

func ollamaTestModel() string {
	if m := os.Getenv("OLLAMA_TEST_MODEL"); m != "" {
		return m
	}
	return "qwen3:1.7b"
}

func newOllamaClient(t *testing.T) llm.Client {
	t.Helper()
	requireOllama(t)
	model := ollamaTestModel()
	client, err := llm.NewOpenAIClient(llm.OpenAIConfig{
		BaseURL:    "http://localhost:11434/v1",
		Model:      model,
		Timeout:    120 * time.Second, // Local inference can be slow
		MaxRetries: 1,
	})
	require.NoError(t, err)
	t.Logf("Using Ollama model: %s", model)
	return client
}

func newIntegrationClassifier(t *testing.T) *LLMClassifier {
	t.Helper()
	adapter := NewLLMClientAdapter(newOllamaClient(t), 0)

	domains := []*DomainExamples{
		{
			Domain:  "iot",
			Version: "1.0",
			Examples: []Example{
				{Query: "Show sensor SENS-001 status", Intent: "entity_lookup"},
				{Query: "Find gateway GW-MAIN", Intent: "entity_lookup"},
				{Query: "List sensors offline since Tuesday", Intent: "temporal_filter"},
				{Query: "How many sensors are reporting?", Intent: "aggregation"},
				{Query: "Count devices in error state", Intent: "aggregation"},
			},
		},
		{
			Domain:  "robotics",
			Version: "1.0",
			Examples: []Example{
				{Query: "Show robot arm position", Intent: "entity_lookup"},
				{Query: "What tasks did robot complete today?", Intent: "temporal_filter"},
				{Query: "Find robots similar to RB-001", Intent: "similarity"},
			},
		},
	}

	return NewLLMClassifier(adapter, domains)
}

func TestLLMClassifier_Integration_EntityLookup(t *testing.T) {
	classifier := newIntegrationClassifier(t)
	ctx, cancel := context.WithTimeout(context.Background(), 120*time.Second)
	defer cancel()

	result, err := classifier.ClassifyQuery(ctx, "Show me sensor SENS-042 details")
	require.NoError(t, err, "LLM call should succeed")
	require.NotNil(t, result)

	t.Logf("Entity lookup — Tier: %d, Intent: %s, Options: %v", result.Tier, result.Intent, result.Options)

	assert.Equal(t, 3, result.Tier)
	assert.NotEmpty(t, result.Intent)
}

func TestLLMClassifier_Integration_TemporalQuery(t *testing.T) {
	classifier := newIntegrationClassifier(t)
	ctx, cancel := context.WithTimeout(context.Background(), 120*time.Second)
	defer cancel()

	result, err := classifier.ClassifyQuery(ctx, "What sensors went offline in the last 24 hours?")
	require.NoError(t, err)
	require.NotNil(t, result)

	t.Logf("Temporal — Tier: %d, Intent: %s, Options: %v", result.Tier, result.Intent, result.Options)

	assert.Equal(t, 3, result.Tier)
	// LLM should recognise the temporal aspect
	strategy, ok := result.Options["strategy"].(string)
	if ok {
		assert.Contains(t, []string{"temporal_graphrag", "graphrag", "hybrid_graphrag"}, strategy,
			"temporal query should use temporal-aware strategy")
	}
}

func TestLLMClassifier_Integration_SimilarityQuery(t *testing.T) {
	classifier := newIntegrationClassifier(t)
	ctx, cancel := context.WithTimeout(context.Background(), 120*time.Second)
	defer cancel()

	result, err := classifier.ClassifyQuery(ctx, "Find devices similar to pump-007")
	require.NoError(t, err)
	require.NotNil(t, result)

	t.Logf("Similarity — Tier: %d, Intent: %s, Options: %v", result.Tier, result.Intent, result.Options)

	assert.Equal(t, 3, result.Tier)
}

func TestLLMClassifier_Integration_PathQuery(t *testing.T) {
	classifier := newIntegrationClassifier(t)
	ctx, cancel := context.WithTimeout(context.Background(), 120*time.Second)
	defer cancel()

	result, err := classifier.ClassifyQuery(ctx, "How is sensor SENS-001 connected to gateway GW-MAIN?")
	require.NoError(t, err)
	require.NotNil(t, result)

	t.Logf("Path — Tier: %d, Intent: %s, Options: %v", result.Tier, result.Intent, result.Options)

	assert.Equal(t, 3, result.Tier)
	// Connection queries should map to a graph traversal strategy. The
	// specific choice (pathrag vs graphrag vs hybrid_graphrag) depends on
	// model size — small Ollama models (qwen3:1.7b) consistently pick
	// the more general graphrag for "How is X connected to Y" phrasing,
	// while frontier models pick the more specific pathrag. Per
	// feedback_frontier_floor_changes_role_split_calculus, we no longer
	// hedge framework defaults for small-model behavior on hard scenarios.
	// Accept the family; trust frontier-floor tests (live_llm tag) for
	// strict strategy assertion.
	strategy, ok := result.Options["strategy"].(string)
	if ok {
		assert.Contains(t, []string{"pathrag", "graphrag", "hybrid_graphrag"}, strategy,
			"connection query should use a graph traversal strategy")
	}
}

func TestLLMClassifier_Integration_AmbiguousQuery(t *testing.T) {
	classifier := newIntegrationClassifier(t)
	ctx, cancel := context.WithTimeout(context.Background(), 120*time.Second)
	defer cancel()

	result, err := classifier.ClassifyQuery(ctx, "What's happening with the fleet?")
	require.NoError(t, err)
	require.NotNil(t, result)

	t.Logf("Ambiguous — Tier: %d, Intent: %s, Options: %v", result.Tier, result.Intent, result.Options)

	assert.Equal(t, 3, result.Tier)
	// Even ambiguous queries should produce a valid classification
	assert.NotEmpty(t, result.Intent)
}

func TestLLMClassifier_Integration_ChainWithLLMFallback(t *testing.T) {
	// Test the full chain: T0 keyword → T1/T2 embedding → T3 LLM
	// An ambiguous query with no keyword cues and no embedding match should reach T3.
	adapter := NewLLMClientAdapter(newOllamaClient(t), 0)
	llmClassifier := NewLLMClassifier(adapter, nil)

	chain := NewClassifierChain(nil, nil, llmClassifier)

	ctx, cancel := context.WithTimeout(context.Background(), 120*time.Second)
	defer cancel()

	result := chain.ClassifyQuery(ctx, "What's the operational health of the system?")
	require.NotNil(t, result)

	t.Logf("Chain fallback — Tier: %d, Intent: %s, Options: %v", result.Tier, result.Intent, result.Options)

	// Should reach T3 since no keyword or embedding match
	assert.Equal(t, 3, result.Tier, "ambiguous query should fall through to T3 LLM")
}

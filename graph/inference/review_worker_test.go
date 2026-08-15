package inference

import (
	"context"
	"log/slog"
	"sync"
	"testing"
	"time"

	"github.com/c360studio/semstreams/graph/llm"
	"github.com/nats-io/nats.go/jetstream"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type reviewLifecycleWatcher struct {
	jetstream.KeyWatcher
	updates chan jetstream.KeyValueEntry
}

func (w *reviewLifecycleWatcher) Updates() <-chan jetstream.KeyValueEntry { return w.updates }
func (*reviewLifecycleWatcher) Stop() error                               { return nil }

type reviewLifecycleKV struct{ jetstream.KeyValue }

func (*reviewLifecycleKV) WatchAll(context.Context, ...jetstream.WatchOpt) (jetstream.KeyWatcher, error) {
	return &reviewLifecycleWatcher{updates: make(chan jetstream.KeyValueEntry)}, nil
}

// mockLLMClient implements llm.Client for testing.
type mockLLMClient struct {
	mu        sync.Mutex
	response  string
	err       error
	callCount int
}

type cancellationAwareReviewClient struct {
	started chan context.Context
}

func (c *cancellationAwareReviewClient) ChatCompletion(ctx context.Context, _ llm.ChatRequest) (*llm.ChatResponse, error) {
	c.started <- ctx
	<-ctx.Done()
	return nil, ctx.Err()
}

func (*cancellationAwareReviewClient) Model() string { return "test-model" }
func (*cancellationAwareReviewClient) Close() error  { return nil }

type reviewContextKey struct{}

func (m *mockLLMClient) ChatCompletion(_ context.Context, _ llm.ChatRequest) (*llm.ChatResponse, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.callCount++
	if m.err != nil {
		return nil, m.err
	}
	return &llm.ChatResponse{Content: m.response}, nil
}

func (m *mockLLMClient) Model() string {
	return "mock-model"
}

func (m *mockLLMClient) Close() error {
	return nil
}

func (m *mockLLMClient) CallCount() int {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.callCount
}

// mockApplier implements RelationshipApplier for testing.
type mockApplier struct {
	mu      sync.Mutex
	applied []*RelationshipSuggestion
	err     error
}

func (m *mockApplier) ApplyRelationship(_ context.Context, suggestion *RelationshipSuggestion) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.err != nil {
		return m.err
	}
	m.applied = append(m.applied, suggestion)
	return nil
}

func (m *mockApplier) AppliedCount() int {
	m.mu.Lock()
	defer m.mu.Unlock()
	return len(m.applied)
}

func (m *mockApplier) GetApplied() []*RelationshipSuggestion {
	m.mu.Lock()
	defer m.mu.Unlock()
	result := make([]*RelationshipSuggestion, len(m.applied))
	copy(result, m.applied)
	return result
}

// mockKVBucket provides a minimal mock for jetstream.KeyValue.
// For ReviewWorker tests, we use the test store in mockStorage instead.

func TestReviewWorker_NewReviewWorker(t *testing.T) {
	storage := newMockStorage()
	applier := &mockApplier{}

	// Test missing anomaly bucket (required field)
	t.Run("missing anomaly bucket", func(t *testing.T) {
		cfg := &ReviewWorkerConfig{
			AnomalyBucket: nil, // Required
			Storage:       storage,
			Applier:       applier,
			Config: ReviewConfig{
				Workers:              1,
				AutoApproveThreshold: 0.9,
				AutoRejectThreshold:  0.3,
			},
		}
		worker, err := NewReviewWorker(cfg)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "anomaly bucket is required")
		assert.Nil(t, worker)
	})

	// Note: Can't easily test missing storage/applier errors without a mock KV bucket
	// since AnomalyBucket is checked first. The validation logic is straightforward
	// and covered by the code review.
}

func TestReviewWorkerStartRejectsNilContext(t *testing.T) {
	w := &ReviewWorker{}

	require.Error(t, w.Start(nil))
}

func TestReviewWorkerPauseStopRestartUsesOneLifecycleGeneration(t *testing.T) {
	w := &ReviewWorker{
		anomalyBucket: &reviewLifecycleKV{},
		config:        ReviewConfig{Workers: 1},
		logger:        slog.New(slog.DiscardHandler),
	}
	require.NoError(t, w.Start(t.Context()))

	start := make(chan struct{})
	stopErr := make(chan error, 1)
	var calls sync.WaitGroup
	calls.Add(2)
	go func() {
		defer calls.Done()
		<-start
		w.Pause()
	}()
	go func() {
		defer calls.Done()
		<-start
		stopErr <- w.Stop()
	}()
	close(start)
	calls.Wait()
	require.NoError(t, <-stopErr)
	require.False(t, w.IsPaused(), "Stop must retire the prior generation's pause state")

	require.NoError(t, w.Start(t.Context()))
	require.False(t, w.IsPaused(), "a restarted worker must begin unpaused")
	require.NoError(t, w.Stop())
}

func TestReviewWorkerLLMChildContextRetainsGenerationAndCancels(t *testing.T) {
	client := &cancellationAwareReviewClient{started: make(chan context.Context, 1)}
	w := &ReviewWorker{
		llmClient: client,
		config: ReviewConfig{
			AutoApproveThreshold: 0.9,
			AutoRejectThreshold:  0.3,
			FallbackToHuman:      true,
			ReviewTimeout:        time.Minute,
		},
		logger: slog.New(slog.DiscardHandler),
	}
	parent, cancel := context.WithCancel(context.WithValue(t.Context(), reviewContextKey{}, "generation-1"))
	done := make(chan struct{})
	go func() {
		w.makeDecision(parent, &StructuralAnomaly{ID: "review", Confidence: 0.6})
		close(done)
	}()
	llmCtx := <-client.started
	require.Equal(t, "generation-1", llmCtx.Value(reviewContextKey{}))
	cancel()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("LLM review did not return after generation cancellation")
	}
	require.ErrorIs(t, llmCtx.Err(), context.Canceled)
}

func TestReviewWorker_MakeDecision_AutoApprove(t *testing.T) {
	storage := newMockStorage()
	applier := &mockApplier{}

	// Create worker manually for testing decision logic
	worker := &ReviewWorker{
		storage: storage,
		applier: applier,
		config: ReviewConfig{
			AutoApproveThreshold: 0.9,
			AutoRejectThreshold:  0.3,
			FallbackToHuman:      true,
		},
	}

	// High confidence anomaly should be auto-approved
	anomaly := &StructuralAnomaly{
		ID:         "high-confidence",
		Confidence: 0.95, // Above 0.9 threshold
		Suggestion: &RelationshipSuggestion{
			FromEntity: "test.inference.graph.review.entity.a",
			ToEntity:   "test.inference.graph.review.entity.b",
			Predicate:  "inference.relation.related-to",
		},
	}

	decision, reason := worker.makeDecision(t.Context(), anomaly)
	assert.Equal(t, DecisionApprove, decision)
	assert.Contains(t, reason, "auto-approved")
}

func TestReviewWorker_MakeDecision_AutoReject(t *testing.T) {
	storage := newMockStorage()
	applier := &mockApplier{}

	worker := &ReviewWorker{
		storage: storage,
		applier: applier,
		config: ReviewConfig{
			AutoApproveThreshold: 0.9,
			AutoRejectThreshold:  0.3,
			FallbackToHuman:      true,
		},
	}

	// Low confidence anomaly should be auto-rejected
	anomaly := &StructuralAnomaly{
		ID:         "low-confidence",
		Confidence: 0.2, // Below 0.3 threshold
	}

	decision, reason := worker.makeDecision(t.Context(), anomaly)
	assert.Equal(t, DecisionReject, decision)
	assert.Contains(t, reason, "auto-rejected")
}

func TestReviewWorker_MakeDecision_HumanReview(t *testing.T) {
	storage := newMockStorage()
	applier := &mockApplier{}

	worker := &ReviewWorker{
		storage:   storage,
		applier:   applier,
		llmClient: nil, // No LLM configured
		config: ReviewConfig{
			AutoApproveThreshold: 0.9,
			AutoRejectThreshold:  0.3,
			FallbackToHuman:      true,
		},
	}

	// Mid-confidence anomaly with no LLM should go to human review
	anomaly := &StructuralAnomaly{
		ID:         "mid-confidence",
		Confidence: 0.6, // Between thresholds
	}

	decision, reason := worker.makeDecision(t.Context(), anomaly)
	assert.Equal(t, DecisionHumanReview, decision)
	assert.Contains(t, reason, "human review")
}

func TestReviewWorker_MakeDecision_LLMApprove(t *testing.T) {
	storage := newMockStorage()
	applier := &mockApplier{}
	llmClient := &mockLLMClient{response: "APPROVE This relationship makes sense."}

	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()

	worker := &ReviewWorker{
		storage:   storage,
		applier:   applier,
		llmClient: llmClient,
		config: ReviewConfig{
			AutoApproveThreshold: 0.9,
			AutoRejectThreshold:  0.3,
			FallbackToHuman:      true,
			ReviewTimeout:        5 * time.Second,
		},
	}

	// Mid-confidence anomaly with LLM configured
	anomaly := &StructuralAnomaly{
		ID:         "llm-review",
		Confidence: 0.6, // Between thresholds, will use LLM
		EntityA:    "test.inference.graph.review.entity.a",
		EntityB:    "test.inference.graph.review.entity.b",
		Type:       AnomalySemanticStructuralGap,
		Suggestion: &RelationshipSuggestion{
			FromEntity: "test.inference.graph.review.entity.a",
			ToEntity:   "test.inference.graph.review.entity.b",
			Predicate:  "inference.relation.related-to",
		},
	}

	decision, reason := worker.makeDecision(ctx, anomaly)
	assert.Equal(t, DecisionApprove, decision)
	assert.Contains(t, reason, "This relationship makes sense")
	assert.Equal(t, 1, llmClient.CallCount())
}

func TestReviewWorker_MakeDecision_LLMReject(t *testing.T) {
	storage := newMockStorage()
	applier := &mockApplier{}
	llmClient := &mockLLMClient{response: "REJECT These entities are unrelated."}

	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()

	worker := &ReviewWorker{
		storage:   storage,
		applier:   applier,
		llmClient: llmClient,
		config: ReviewConfig{
			AutoApproveThreshold: 0.9,
			AutoRejectThreshold:  0.3,
			FallbackToHuman:      true,
			ReviewTimeout:        5 * time.Second,
		},
	}

	anomaly := &StructuralAnomaly{
		ID:         "llm-reject",
		Confidence: 0.6,
		EntityA:    "test.inference.graph.review.entity.a",
		EntityB:    "test.inference.graph.review.entity.b",
		Type:       AnomalySemanticStructuralGap,
	}

	decision, reason := worker.makeDecision(ctx, anomaly)
	assert.Equal(t, DecisionReject, decision)
	assert.Contains(t, reason, "These entities are unrelated")
}

func TestReviewWorker_ParseLLMDecision(t *testing.T) {
	worker := &ReviewWorker{}

	tests := []struct {
		name         string
		response     string
		wantDecision Decision
	}{
		{
			name:         "approve uppercase",
			response:     "APPROVE This is a valid relationship.",
			wantDecision: DecisionApprove,
		},
		{
			name:         "approve lowercase",
			response:     "approve the relationship is semantically valid.",
			wantDecision: DecisionApprove,
		},
		{
			name:         "reject uppercase",
			response:     "REJECT No evidence for this connection.",
			wantDecision: DecisionReject,
		},
		{
			name:         "reject lowercase",
			response:     "reject these entities are unrelated.",
			wantDecision: DecisionReject,
		},
		{
			name:         "unclear response",
			response:     "I'm not sure about this relationship.",
			wantDecision: DecisionHumanReview,
		},
		{
			name:         "empty response",
			response:     "",
			wantDecision: DecisionHumanReview,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			decision, _, err := worker.parseLLMDecision(tt.response)
			require.NoError(t, err)
			assert.Equal(t, tt.wantDecision, decision)
		})
	}
}

func TestReviewWorker_PauseResume(t *testing.T) {
	storage := newMockStorage()
	applier := &mockApplier{}

	worker := &ReviewWorker{
		storage: storage,
		applier: applier,
		config: ReviewConfig{
			Workers: 1,
		},
		started: true,                          // Pretend started for pause/resume testing
		logger:  slog.New(slog.DiscardHandler), // Use discard handler for tests
	}

	// Initially not paused
	assert.False(t, worker.IsPaused())

	// Pause
	worker.Pause()
	assert.True(t, worker.IsPaused())

	// Pause again (idempotent)
	worker.Pause()
	assert.True(t, worker.IsPaused())

	// Resume
	worker.Resume()
	assert.False(t, worker.IsPaused())

	// Resume again (idempotent)
	worker.Resume()
	assert.False(t, worker.IsPaused())
}

func TestReviewWorker_BuildReviewPrompt(t *testing.T) {
	worker := &ReviewWorker{}

	anomaly := &StructuralAnomaly{
		Type:       AnomalySemanticStructuralGap,
		Confidence: 0.75,
		EntityA:    "test.inference.graph.review.company.acme-corp",
		EntityB:    "test.inference.graph.review.person.john-doe",
		Evidence: Evidence{
			Similarity:         0.85,
			StructuralDistance: 4,
		},
		Suggestion: &RelationshipSuggestion{
			FromEntity: "test.inference.graph.review.company.acme-corp",
			ToEntity:   "test.inference.graph.review.person.john-doe",
			Predicate:  "social.relation.employs",
			Reasoning:  "High semantic similarity suggests employment relationship",
		},
	}

	prompt := worker.buildReviewPrompt(anomaly)

	// Verify prompt contains key information
	assert.Contains(t, prompt, "semantic_structural_gap")
	assert.Contains(t, prompt, "0.75")
	assert.Contains(t, prompt, "test.inference.graph.review.company.acme-corp")
	assert.Contains(t, prompt, "test.inference.graph.review.person.john-doe")
	assert.Contains(t, prompt, "0.85") // similarity
	assert.Contains(t, prompt, "4")    // structural distance
	assert.Contains(t, prompt, "social.relation.employs")
	assert.Contains(t, prompt, "APPROVE or REJECT")
}

func TestDecision_String(t *testing.T) {
	assert.Equal(t, "approve", DecisionApprove.String())
	assert.Equal(t, "reject", DecisionReject.String())
	assert.Equal(t, "human_review", DecisionHumanReview.String())
}

func TestStructuralAnomaly_CanAutoApprove(t *testing.T) {
	anomaly := &StructuralAnomaly{Confidence: 0.95}
	assert.True(t, anomaly.CanAutoApprove(0.9))
	assert.True(t, anomaly.CanAutoApprove(0.95))
	assert.False(t, anomaly.CanAutoApprove(0.96))
}

func TestStructuralAnomaly_CanAutoReject(t *testing.T) {
	anomaly := &StructuralAnomaly{Confidence: 0.2}
	assert.True(t, anomaly.CanAutoReject(0.3))
	assert.True(t, anomaly.CanAutoReject(0.2))
	assert.False(t, anomaly.CanAutoReject(0.1))
}

func TestStructuralAnomaly_IsResolved(t *testing.T) {
	tests := []struct {
		status   AnomalyStatus
		resolved bool
	}{
		{StatusPending, false},
		{StatusLLMReviewing, false},
		{StatusHumanReview, false},
		{StatusApproved, true},
		{StatusRejected, true},
		{StatusLLMRejected, true},
		{StatusApplied, true},
	}

	for _, tt := range tests {
		t.Run(string(tt.status), func(t *testing.T) {
			anomaly := &StructuralAnomaly{Status: tt.status}
			assert.Equal(t, tt.resolved, anomaly.IsResolved())
		})
	}
}

func TestStructuralAnomaly_NeedsHumanReview(t *testing.T) {
	tests := []struct {
		status      AnomalyStatus
		needsReview bool
	}{
		{StatusPending, false},
		{StatusHumanReview, true},
		{StatusApproved, false},
	}

	for _, tt := range tests {
		t.Run(string(tt.status), func(t *testing.T) {
			anomaly := &StructuralAnomaly{Status: tt.status}
			assert.Equal(t, tt.needsReview, anomaly.NeedsHumanReview())
		})
	}
}

package graphquery

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/c360studio/semstreams/graph/llm"
)

func TestTemplateAnswerSynthesizer_Synthesize(t *testing.T) {
	s := &TemplateAnswerSynthesizer{}

	t.Run("empty summaries", func(t *testing.T) {
		answer, model, err := s.Synthesize(context.Background(), "test query", nil, 0)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if answer != "" {
			t.Errorf("expected empty answer, got %q", answer)
		}
		if model != "" {
			t.Errorf("expected empty model, got %q", model)
		}
	})

	t.Run("produces template answer", func(t *testing.T) {
		summaries := []CommunitySummary{
			{Summary: "Game quest entities.", MemberCount: 10, Relevance: 0.9},
		}
		answer, model, err := s.Synthesize(context.Background(), "show quests", summaries, 10)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if !strings.Contains(answer, "10 entities") {
			t.Errorf("answer missing entity count: %s", answer)
		}
		if model != "" {
			t.Errorf("template synthesizer should return empty model, got %q", model)
		}
	})
}

func TestLLMAnswerSynthesizer_Synthesize(t *testing.T) {
	t.Run("success", func(t *testing.T) {
		client := &mockLLMClient{
			response: &llm.ChatResponse{Content: "The quests involve dragon slaying and merchant routes."},
		}
		s := NewLLMAnswerSynthesizer(client, "gemini-flash", nil, 0)

		summaries := []CommunitySummary{
			{Summary: "Quest entities on board1.", MemberCount: 10, Relevance: 0.9},
		}
		answer, model, err := s.Synthesize(context.Background(), "what quests exist?", summaries, 10)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if answer != "The quests involve dragon slaying and merchant routes." {
			t.Errorf("unexpected answer: %q", answer)
		}
		if model != "gemini-flash" {
			t.Errorf("model = %q, want gemini-flash", model)
		}

		// Verify the prompt was constructed correctly
		if !strings.Contains(client.LastRequest().UserPrompt, "what quests exist?") {
			t.Error("user prompt should contain the query")
		}
		if !strings.Contains(client.LastRequest().UserPrompt, "Quest entities on board1.") {
			t.Error("user prompt should contain community summary")
		}
		if client.LastRequest().SystemPrompt == "" {
			t.Error("system prompt should be set")
		}
	})

	t.Run("falls back on error", func(t *testing.T) {
		client := &mockLLMClient{
			err: fmt.Errorf("connection refused"),
		}
		s := NewLLMAnswerSynthesizer(client, "gemini-flash", nil, 0)

		summaries := []CommunitySummary{
			{Summary: "Quest entities.", MemberCount: 5, Relevance: 0.8},
		}
		answer, model, err := s.Synthesize(context.Background(), "test", summaries, 5)
		// Error is logged internally, not returned — fallback is transparent
		if err != nil {
			t.Fatalf("expected nil error on fallback, got %v", err)
		}
		// Should fall back to template answer
		if !strings.Contains(answer, "5 entities") {
			t.Errorf("fallback answer missing entity count: %s", answer)
		}
		// Model should be empty on fallback
		if model != "" {
			t.Errorf("model should be empty on fallback, got %q", model)
		}
	})
}

// TestLLMAnswerSynthesizer_SubTimeout_FallsBackTransparently is the
// regression for the seminstruct-too-slow wedge semspec hit: a slow
// upstream model must not consume the parent ctx's full budget. The
// sub-timeout fires, the template fallback runs, and the parent ctx
// retains substantial budget for the rest of the response path.
func TestLLMAnswerSynthesizer_SubTimeout_FallsBackTransparently(t *testing.T) {
	// Delay = 5s, sub-timeout = 50ms → sub-context fires first; the
	// fallback path runs while the parent ctx still has nearly 5s left.
	client := &mockLLMClient{delay: 5 * time.Second}
	s := NewLLMAnswerSynthesizer(client, "slow-model", nil, 50*time.Millisecond)

	parentCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	summaries := []CommunitySummary{
		{Summary: "Quest entities.", MemberCount: 7, Relevance: 0.8},
	}

	start := time.Now()
	answer, model, err := s.Synthesize(parentCtx, "what quests exist?", summaries, 7)
	elapsed := time.Since(start)

	if err != nil {
		t.Fatalf("expected nil error on sub-timeout fallback (transparent to caller), got %v", err)
	}
	if !strings.Contains(answer, "7 entities") {
		t.Errorf("fallback answer missing entity count: %q", answer)
	}
	if model != "" {
		t.Errorf("model should be empty on fallback, got %q", model)
	}
	// Bound elapsed at well under the 5s mock delay — proves the
	// sub-timeout fired rather than the call running to completion.
	if elapsed > time.Second {
		t.Errorf("Synthesize took %v, want < 1s — sub-timeout did not fire", elapsed)
	}
	// And the parent ctx must still have substantial budget so the
	// rest of the response path (NATS reply, gateway HTTP write) can
	// complete cleanly. This is the load-bearing property of the fix.
	if dl, ok := parentCtx.Deadline(); ok {
		remaining := time.Until(dl)
		if remaining < 4*time.Second {
			t.Errorf("parent ctx remaining = %v, want >= 4s — sub-timeout consumed too much", remaining)
		}
	}
}

// TestLLMAnswerSynthesizer_DefaultTimeout_AppliedWhenZero proves the
// constructor honours the framework default when the caller passes 0.
func TestLLMAnswerSynthesizer_DefaultTimeout_AppliedWhenZero(t *testing.T) {
	s := NewLLMAnswerSynthesizer(&mockLLMClient{}, "m", nil, 0)
	if s.timeout != DefaultAnswerSynthesisTimeout {
		t.Errorf("timeout = %v, want DefaultAnswerSynthesisTimeout (%v)", s.timeout, DefaultAnswerSynthesisTimeout)
	}
}

func TestBuildAnswerPrompt(t *testing.T) {
	summaries := []CommunitySummary{
		{
			Summary:     "Game quest entities on board1.",
			MemberCount: 12,
			Relevance:   0.85,
			Keywords:    []string{"quest", "reward", "dragon"},
			Entities: []EntityDigest{
				{ID: "q1", Type: "quest", Label: "Dragon Slayer"},
			},
		},
		{
			Summary:     "Player agents.",
			MemberCount: 8,
			Relevance:   0.6,
		},
	}

	prompt := buildAnswerPrompt("show me all quests", summaries, 20)

	if !strings.Contains(prompt, "show me all quests") {
		t.Error("prompt should contain the query")
	}
	if !strings.Contains(prompt, "20 matching entities") {
		t.Error("prompt should contain total entity count")
	}
	if !strings.Contains(prompt, "2 clusters") {
		t.Error("prompt should contain cluster count")
	}
	if !strings.Contains(prompt, "12 entities") {
		t.Error("prompt should contain member count")
	}
	if !strings.Contains(prompt, "Dragon Slayer [quest]") {
		t.Error("prompt should contain representative entity")
	}
	if !strings.Contains(prompt, "quest, reward, dragon") {
		t.Error("prompt should contain keywords")
	}
	if !strings.Contains(prompt, "Player agents.") {
		t.Error("prompt should contain second cluster summary")
	}
}

func TestBuildAnswerPrompt_LimitsTo5Clusters(t *testing.T) {
	summaries := make([]CommunitySummary, 8)
	for i := range summaries {
		summaries[i] = CommunitySummary{
			Summary:     fmt.Sprintf("Cluster %c.", 'A'+i),
			MemberCount: 5,
			Relevance:   0.5,
		}
	}
	prompt := buildAnswerPrompt("test", summaries, 40)

	if !strings.Contains(prompt, "Cluster E.") {
		t.Error("should include 5th cluster")
	}
	if strings.Contains(prompt, "Cluster F.") {
		t.Error("should not include 6th cluster")
	}
}

// mockLLMClient implements llm.Client for testing.
//
// The ctx-respecting path is the default: ChatCompletion blocks until
// either the configured delay elapses or the ctx is cancelled. Tests
// that need the legacy "instant return" behaviour leave delay zero.
// This shape lets the bounded-timeout tests exercise the real failure
// mode (sub-context cancellation) rather than relying on a synthetic
// pre-canned error.
//
// lastRequest is mutex-guarded so a test that drives ChatCompletion
// from multiple goroutines (e.g. a future parallel t.Run) doesn't race
// on the captured-request field. Existing single-shot tests are
// unaffected.
type mockLLMClient struct {
	response *llm.ChatResponse
	err      error
	delay    time.Duration

	mu          sync.Mutex
	lastRequest llm.ChatRequest
}

func (m *mockLLMClient) ChatCompletion(ctx context.Context, req llm.ChatRequest) (*llm.ChatResponse, error) {
	m.mu.Lock()
	m.lastRequest = req
	m.mu.Unlock()
	if m.delay > 0 {
		select {
		case <-time.After(m.delay):
		case <-ctx.Done():
			return nil, ctx.Err()
		}
	}
	return m.response, m.err
}

// LastRequest returns a snapshot of the most recently observed
// ChatRequest. Tests use this in place of reading lastRequest
// directly so the access is mutex-guarded.
func (m *mockLLMClient) LastRequest() llm.ChatRequest {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.lastRequest
}

func (m *mockLLMClient) Model() string { return "mock" }
func (m *mockLLMClient) Close() error  { return nil }

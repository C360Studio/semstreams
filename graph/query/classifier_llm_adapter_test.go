package query

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/c360studio/semstreams/graph/llm"
)

// adapterMockLLMClient is a ctx-respecting llm.Client fake. ChatCompletion
// blocks until either delay elapses or ctx cancels, so tests can prove
// the bounded sub-timeout fires regardless of how long the upstream
// model would actually take.
type adapterMockLLMClient struct {
	response *llm.ChatResponse
	err      error
	delay    time.Duration
}

func (m *adapterMockLLMClient) ChatCompletion(ctx context.Context, _ llm.ChatRequest) (*llm.ChatResponse, error) {
	if m.delay > 0 {
		select {
		case <-time.After(m.delay):
		case <-ctx.Done():
			return nil, ctx.Err()
		}
	}
	return m.response, m.err
}

func (m *adapterMockLLMClient) Model() string { return "mock" }
func (m *adapterMockLLMClient) Close() error  { return nil }

// TestLLMClientAdapter_DefaultTimeout_AppliedWhenZero proves
// NewLLMClientAdapter substitutes the framework default when given 0.
func TestLLMClientAdapter_DefaultTimeout_AppliedWhenZero(t *testing.T) {
	a := NewLLMClientAdapter(&adapterMockLLMClient{}, 0)
	if a.timeout != DefaultClassificationTimeout {
		t.Errorf("timeout = %v, want DefaultClassificationTimeout (%v)", a.timeout, DefaultClassificationTimeout)
	}
}

// TestLLMClientAdapter_HonoursExplicitTimeout proves a non-zero
// timeout passed to the constructor is preserved as-is.
func TestLLMClientAdapter_HonoursExplicitTimeout(t *testing.T) {
	a := NewLLMClientAdapter(&adapterMockLLMClient{}, 7*time.Second)
	if a.timeout != 7*time.Second {
		t.Errorf("timeout = %v, want 7s", a.timeout)
	}
}

// TestLLMClientAdapter_SubTimeout_BoundsLLMCall is the regression for
// the seminstruct-too-slow class of wedge: when the upstream LLM is
// slower than the configured sub-timeout, ClassifyQuery must return an
// error within the bound, leaving the parent ctx with budget for
// ClassifierChain's keyword fallback to run AND for the rest of the
// response path to reach the HTTP layer.
func TestLLMClientAdapter_SubTimeout_BoundsLLMCall(t *testing.T) {
	client := &adapterMockLLMClient{delay: 5 * time.Second}
	a := NewLLMClientAdapter(client, 50*time.Millisecond)

	parentCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	start := time.Now()
	_, err := a.ClassifyQuery(parentCtx, "test prompt")
	elapsed := time.Since(start)

	if err == nil {
		t.Fatal("expected error from sub-timeout, got nil")
	}
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Errorf("error = %v, want DeadlineExceeded (sub-timeout source)", err)
	}
	if elapsed > time.Second {
		t.Errorf("ClassifyQuery took %v, want < 1s — sub-timeout did not fire", elapsed)
	}
	// Parent ctx must still have substantial budget so the chain's
	// fallback and the rest of the response path can complete.
	if dl, ok := parentCtx.Deadline(); ok {
		remaining := time.Until(dl)
		if remaining < 4*time.Second {
			t.Errorf("parent ctx remaining = %v, want >= 4s — sub-timeout consumed too much", remaining)
		}
	}
}

// TestLLMClientAdapter_FastResponse_StripThinkTags confirms the happy
// path still works under the new bounded-timeout structure: a normal
// response is returned with reasoning-model `<think>...</think>` blocks
// stripped.
func TestLLMClientAdapter_FastResponse_StripThinkTags(t *testing.T) {
	client := &adapterMockLLMClient{
		response: &llm.ChatResponse{
			Content: "<think>weighing options</think>{\"intent\":\"semantic\"}",
		},
	}
	a := NewLLMClientAdapter(client, time.Second)

	out, err := a.ClassifyQuery(context.Background(), "any prompt")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if strings.Contains(out, "<think>") {
		t.Errorf("expected think tags stripped, got %q", out)
	}
	if !strings.Contains(out, `"intent":"semantic"`) {
		t.Errorf("expected JSON content preserved, got %q", out)
	}
}

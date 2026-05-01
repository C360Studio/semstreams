package graphclustering

import (
	"context"
	"log/slog"
	"testing"

	"github.com/c360studio/semstreams/graph/llm"
	"github.com/c360studio/semstreams/model"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// resolveReviewLLMClient is the seam Phase 2 introduced when lifting the
// anomaly review LLM call out of its silent piggyback on the
// community_summary endpoint. These tests pin its three branches:
//
//  1. modelRegistry == nil → falls back to c.llmClient.
//  2. CapabilityAnomalyReview unbound → falls back to c.llmClient
//     (preserves legacy piggyback behavior for unmigrated deployments).
//  3. CapabilityAnomalyReview bound → creates a dedicated client and
//     stores it on c.reviewLLMClient for cleanup.

func TestResolveReviewLLMClient_NilRegistry_ReusesCommunityClient(t *testing.T) {
	communityClient := &fakeLLMClient{}
	c := &Component{
		modelRegistry: nil,
		llmClient:     communityClient,
		logger:        slog.Default(),
	}

	got := c.resolveReviewLLMClient()
	assert.Same(t, communityClient, got, "nil registry should reuse community_summary client")
	assert.Nil(t, c.reviewLLMClient, "no dedicated client should be stored")
}

func TestResolveReviewLLMClient_CapabilityUnbound_ReusesCommunityClient(t *testing.T) {
	communityClient := &fakeLLMClient{}
	c := &Component{
		modelRegistry: &model.Registry{
			Endpoints: map[string]*model.EndpointConfig{
				"seminstruct": {Provider: "openai", URL: "http://seminstruct:8083/v1", Model: "qwen", MaxTokens: 4096},
			},
			// Note: no anomaly_review capability.
			Capabilities: map[string]*model.CapabilityConfig{
				"community_summary": {Preferred: []string{"seminstruct"}},
			},
			Defaults: model.DefaultsConfig{Model: "seminstruct"},
		},
		llmClient: communityClient,
		logger:    slog.Default(),
	}

	got := c.resolveReviewLLMClient()
	assert.Same(t, communityClient, got, "unbound anomaly_review should reuse community_summary client (legacy piggyback)")
	assert.Nil(t, c.reviewLLMClient, "no dedicated client should be stored when capability is unbound")
}

func TestResolveReviewLLMClient_CapabilityBound_CreatesDedicatedClient(t *testing.T) {
	communityClient := &fakeLLMClient{}
	c := &Component{
		modelRegistry: &model.Registry{
			Endpoints: map[string]*model.EndpointConfig{
				"seminstruct": {Provider: "openai", URL: "http://seminstruct:8083/v1", Model: "qwen", MaxTokens: 4096},
				"review-fast": {Provider: "openai", URL: "http://review-fast:8084/v1", Model: "qwen-fast", MaxTokens: 4096},
			},
			Capabilities: map[string]*model.CapabilityConfig{
				"community_summary": {Preferred: []string{"seminstruct"}},
				"anomaly_review":    {Preferred: []string{"review-fast"}},
			},
			Defaults: model.DefaultsConfig{Model: "seminstruct"},
		},
		llmClient: communityClient,
		logger:    slog.Default(),
	}

	got := c.resolveReviewLLMClient()
	require.NotNil(t, got, "bound anomaly_review should produce a client")
	assert.NotSame(t, communityClient, got, "bound anomaly_review should not reuse the community client")
	assert.NotNil(t, c.reviewLLMClient, "dedicated client should be stored for cleanup")
	assert.Same(t, got, c.reviewLLMClient, "returned client and stored client should match")
}

// fakeLLMClient is a stub llm.Client used to verify pointer identity
// without standing up a real OpenAI client. None of these tests exercise
// the LLM call path itself — only the resolution branching.
type fakeLLMClient struct {
	model string
}

func (f *fakeLLMClient) ChatCompletion(_ context.Context, _ llm.ChatRequest) (*llm.ChatResponse, error) {
	return nil, nil
}
func (f *fakeLLMClient) Model() string { return f.model }
func (f *fakeLLMClient) Close() error  { return nil }

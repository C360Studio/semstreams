package graphquery

import (
	"testing"

	"github.com/c360studio/semstreams/model"
)

// TestResolveEndpointWithConfig_KeepalivePlumbing pins the field-by-
// field plumbing contract that semspec depends on: an EndpointConfig
// with DisableKeepAlives / IdleConnTimeout / ResponseHeaderTimeout
// set must survive resolveEndpointWithConfig and reach the LLM
// client builder unchanged. The previous bug (semspec #10) had the
// fields silently stripped on the way through model.ResolveEndpoint;
// this test guards the regression class.
func TestResolveEndpointWithConfig_KeepalivePlumbing(t *testing.T) {
	reg := &model.Registry{
		Endpoints: map[string]*model.EndpointConfig{
			"sparky-qwen": {
				Provider:              "openai",
				URL:                   "http://sparky:8080/v1",
				Model:                 "qwen3-coder:30b",
				DisableKeepAlives:     true,
				IdleConnTimeout:       "10s",
				ResponseHeaderTimeout: "30s",
				MaxTokens:             32000,
			},
		},
		Capabilities: map[string]*model.CapabilityConfig{
			model.CapabilityAnswerSynthesis: {
				Preferred: []string{"sparky-qwen"},
			},
		},
		Defaults: model.DefaultsConfig{Model: "sparky-qwen"},
	}

	resolved, ep, err := resolveEndpointWithConfig(reg, model.CapabilityAnswerSynthesis)
	if err != nil {
		t.Fatalf("resolveEndpointWithConfig: %v", err)
	}

	// The minimal trio still resolves correctly.
	if resolved.URL != "http://sparky:8080/v1" {
		t.Errorf("URL = %q, want sparky URL", resolved.URL)
	}
	if resolved.Model != "qwen3-coder:30b" {
		t.Errorf("Model = %q, want qwen3-coder:30b", resolved.Model)
	}

	// And the full EndpointConfig carries the keepalive fields the
	// LLM client builder needs. These were the bug.
	if !ep.DisableKeepAlives {
		t.Errorf("DisableKeepAlives = false, want true (semspec #10 regression)")
	}
	if ep.IdleConnTimeout != "10s" {
		t.Errorf("IdleConnTimeout = %q, want 10s", ep.IdleConnTimeout)
	}
	if ep.ResponseHeaderTimeout != "30s" {
		t.Errorf("ResponseHeaderTimeout = %q, want 30s", ep.ResponseHeaderTimeout)
	}
}

// TestResolveEndpointWithConfig_NoEndpointsAtAll covers the "registry
// has no endpoints" branch: ResolveEndpoint returns an error, and
// resolveEndpointWithConfig propagates it without a panic on the
// downstream lookup.
func TestResolveEndpointWithConfig_NoEndpointsAtAll(t *testing.T) {
	reg := &model.Registry{
		Endpoints: map[string]*model.EndpointConfig{},
	}

	_, _, err := resolveEndpointWithConfig(reg, model.CapabilityAnswerSynthesis)
	if err == nil {
		t.Fatal("expected error when no endpoints configured, got nil")
	}
}

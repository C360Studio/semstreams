package agenticmodel

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/c360studio/semstreams/agentic"
	"github.com/c360studio/semstreams/component"
	"github.com/c360studio/semstreams/model"
)

// healthChainTestRegistry returns a registry shaped for chain-skipping
// tests: a "fast" capability with preferred → fallback ordering, plus
// a default that shouldn't be selected when the chain has a healthy
// option.
func healthChainTestRegistry() *model.Registry {
	return &model.Registry{
		Endpoints: map[string]*model.EndpointConfig{
			"preferred": {
				Provider: "ollama", URL: "http://p/v1",
				Model: "preferred-model", MaxTokens: 8192,
			},
			"fallback": {
				Provider: "ollama", URL: "http://f/v1",
				Model: "fallback-model", MaxTokens: 8192,
			},
			"default-only": {
				Provider: "ollama", URL: "http://d/v1",
				Model: "default-model", MaxTokens: 8192,
			},
		},
		Capabilities: map[string]*model.CapabilityConfig{
			"fast": {
				Preferred: []string{"preferred"},
				Fallback:  []string{"fallback"},
			},
		},
		Defaults: model.DefaultsConfig{Model: "default-only"},
	}
}

// healthChainTestComponent builds a Component with the given health
// policy injected, bypassing NATS / messaging entirely. Returns the
// component plus the policy so tests can drive state transitions and
// then call getClientForRequest directly.
func healthChainTestComponent(t *testing.T, policy model.HealthPolicy) *Component {
	t.Helper()
	cfg := DefaultConfig()
	raw, err := json.Marshal(cfg)
	if err != nil {
		t.Fatalf("marshal config: %v", err)
	}
	d, err := NewComponentWithOptions(raw, component.Dependencies{
		ModelRegistry: healthChainTestRegistry(),
	}, WithHealthPolicy(policy))
	if err != nil {
		t.Fatalf("NewComponentWithOptions: %v", err)
	}
	return d.(*Component)
}

// recordFailures drives the breaker into Open by feeding it enough
// failures to exceed the threshold given the test BreakerConfig.
func recordFailures(p model.HealthPolicy, endpoint string, n int) {
	for i := 0; i < n; i++ {
		p.RecordResult(endpoint, model.Result{Success: false, Kind: model.ErrorKindServerError})
	}
}

// All chain endpoints healthy → preferred wins.
func TestGetClientForRequest_HealthyChain_PicksPreferred(t *testing.T) {
	c := healthChainTestComponent(t, model.NewAlwaysHealthyPolicy())

	_, ep, _, name, err := c.getClientForRequest(agentic.AgentRequest{Model: "fast"})
	if err != nil {
		t.Fatalf("getClientForRequest: %v", err)
	}
	if name != "preferred" {
		t.Errorf("selected endpoint = %q, want preferred", name)
	}
	if ep.Model != "preferred-model" {
		t.Errorf("ep.Model = %q, want preferred-model", ep.Model)
	}
}

// Preferred unhealthy → chain skips to fallback. This is the core
// behavior change: agentic-model now consults the breaker before
// picking from the chain.
func TestGetClientForRequest_PreferredUnhealthy_PicksFallback(t *testing.T) {
	policy := model.NewRollingWindowBreaker(model.BreakerConfig{
		WindowSize:         10,
		MinRequests:        3,
		ErrorRateThreshold: 0.5,
		Cooldown:           30 * time.Second,
	})
	c := healthChainTestComponent(t, policy)

	// Drive preferred to Open.
	recordFailures(policy, "preferred", 4)
	if policy.IsHealthy("preferred") {
		t.Fatalf("setup: preferred should be Open after 4 failures")
	}

	_, _, _, name, err := c.getClientForRequest(agentic.AgentRequest{Model: "fast"})
	if err != nil {
		t.Fatalf("getClientForRequest: %v", err)
	}
	if name != "fallback" {
		t.Errorf("selected endpoint = %q, want fallback (preferred is Open)", name)
	}
}

// Chain unhealthy + default present → default wins. The default
// is intentionally NOT health-gated so a fully-degraded chain still
// has a guaranteed responder; refusing to dispatch would just queue
// dead air for the user.
func TestGetClientForRequest_ChainUnhealthy_DefaultWins(t *testing.T) {
	policy := model.NewRollingWindowBreaker(model.BreakerConfig{
		WindowSize:         10,
		MinRequests:        3,
		ErrorRateThreshold: 0.5,
		Cooldown:           30 * time.Second,
	})
	c := healthChainTestComponent(t, policy)

	recordFailures(policy, "preferred", 4)
	recordFailures(policy, "fallback", 4)
	recordFailures(policy, "default-only", 4) // default is also Open
	// All three Open; we still expect default-only because step 3 is
	// not health-gated.

	_, _, _, name, err := c.getClientForRequest(agentic.AgentRequest{Model: "fast"})
	if err != nil {
		t.Fatalf("getClientForRequest: %v", err)
	}
	if name != "default-only" {
		t.Errorf("selected = %q, want default-only (default not health-gated)", name)
	}
}

// Last-ditch path: chain unhealthy AND no default configured. Rather
// than hard-failing, retry the chain ignoring health so the breaker
// can record the next result and converge faster on recovery.
func TestGetClientForRequest_AllUnhealthyNoDefault_LastDitchAttempt(t *testing.T) {
	policy := model.NewRollingWindowBreaker(model.BreakerConfig{
		WindowSize:         10,
		MinRequests:        3,
		ErrorRateThreshold: 0.5,
		Cooldown:           30 * time.Second,
	})

	// Build a registry with the chain but no default — exercises the
	// last-ditch retry branch.
	cfg := DefaultConfig()
	raw, err := json.Marshal(cfg)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	reg := &model.Registry{
		Endpoints: map[string]*model.EndpointConfig{
			"preferred": {Provider: "ollama", URL: "http://p/v1", Model: "p", MaxTokens: 8192},
			"fallback":  {Provider: "ollama", URL: "http://f/v1", Model: "f", MaxTokens: 8192},
		},
		Capabilities: map[string]*model.CapabilityConfig{
			"fast": {Preferred: []string{"preferred"}, Fallback: []string{"fallback"}},
		},
		// No Defaults.Model.
	}
	d, err := NewComponentWithOptions(raw, component.Dependencies{ModelRegistry: reg}, WithHealthPolicy(policy))
	if err != nil {
		t.Fatalf("NewComponentWithOptions: %v", err)
	}
	c := d.(*Component)

	recordFailures(policy, "preferred", 4)
	recordFailures(policy, "fallback", 4)

	_, _, _, name, err := c.getClientForRequest(agentic.AgentRequest{Model: "fast"})
	if err != nil {
		t.Fatalf("last-ditch path must not hard-fail: %v", err)
	}
	if name != "preferred" {
		t.Errorf("last-ditch selected = %q, want preferred (chain head)", name)
	}
}

// Direct endpoint name (not a capability) also gets gated. Caller
// asks for "preferred" by name; with breaker open we fall through to
// the default. This protects callers that bypassed the capability
// indirection.
func TestGetClientForRequest_DirectEndpointUnhealthy_FallsThroughToDefault(t *testing.T) {
	policy := model.NewRollingWindowBreaker(model.BreakerConfig{
		WindowSize:         10,
		MinRequests:        3,
		ErrorRateThreshold: 0.5,
		Cooldown:           30 * time.Second,
	})
	c := healthChainTestComponent(t, policy)

	recordFailures(policy, "preferred", 4)

	_, _, _, name, err := c.getClientForRequest(agentic.AgentRequest{Model: "preferred"})
	if err != nil {
		t.Fatalf("getClientForRequest: %v", err)
	}
	if name != "default-only" {
		t.Errorf("direct unhealthy → fallthrough endpoint = %q, want default-only", name)
	}
}

// recordHealthResult must classify successes as ErrorKindNone and
// drop the call when no endpoint name is set (defensive — happens
// only on errored resolution paths).
func TestRecordHealthResult_ClassificationAndGuards(t *testing.T) {
	policy := model.NewRollingWindowBreaker(model.BreakerConfig{})
	c := healthChainTestComponent(t, policy)

	// Empty endpoint name → no-op. No state change.
	c.recordHealthResult(context.Background(), "", true, nil, "", time.Millisecond)
	if policy.EndpointStats("anything").Successes != 0 {
		t.Error("recordHealthResult with empty endpoint should be a no-op")
	}

	// Success path.
	c.recordHealthResult(context.Background(), "ep", true, nil, "", time.Millisecond)
	stats := policy.EndpointStats("ep")
	if stats.Successes != 1 || stats.Failures != 0 {
		t.Errorf("after success: stats = %+v, want 1 success", stats)
	}

	// Failure with rate-limit error message → ErrorKindRateLimit.
	c.recordHealthResult(context.Background(), "ep", false, nil, "rate limit exceeded", time.Millisecond)
	stats = policy.EndpointStats("ep")
	if stats.Failures != 1 {
		t.Errorf("after rate-limit failure: failures = %d, want 1", stats.Failures)
	}

	// Cancelled context → ErrorKindTimeout regardless of message.
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	c.recordHealthResult(ctx, "ep", false, nil, "anything", time.Millisecond)
	stats = policy.EndpointStats("ep")
	if stats.Failures != 2 {
		t.Errorf("after timeout failure: failures = %d, want 2", stats.Failures)
	}
}

// mapErrorKind covers the classification table directly. Drift here
// breaks Prometheus error_type labels' alignment with breaker math.
func TestMapErrorKind(t *testing.T) {
	cancelled, cancel := context.WithCancel(context.Background())
	cancel()

	cases := []struct {
		name   string
		ctx    context.Context
		errMsg string
		want   model.ErrorKind
	}{
		{"timeout via ctx", cancelled, "anything", model.ErrorKindTimeout},
		{"rate limit text", context.Background(), "rate limit exceeded", model.ErrorKindRateLimit},
		{"429 in body", context.Background(), "got 429 from server", model.ErrorKindRateLimit},
		{"connection refused", context.Background(), "connection refused", model.ErrorKindNetwork},
		{"dial failure", context.Background(), "dial tcp: lookup x.invalid", model.ErrorKindNetwork},
		{"500", context.Background(), "got 500 internal server error", model.ErrorKindServerError},
		{"503", context.Background(), "got 503", model.ErrorKindServerError},
		{"unknown", context.Background(), "weird thing happened", model.ErrorKindUnknown},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := mapErrorKind(tc.ctx, tc.errMsg); got != tc.want {
				t.Errorf("mapErrorKind(%q) = %q, want %q", tc.errMsg, got, tc.want)
			}
		})
	}
}

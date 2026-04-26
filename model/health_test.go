package model

import (
	"testing"
)

// EndpointStatus.String must produce the lowercase Prometheus-friendly
// name. Drift here breaks dashboards silently.
func TestEndpointStatus_String(t *testing.T) {
	cases := []struct {
		s    EndpointStatus
		want string
	}{
		{StatusClosed, "closed"},
		{StatusOpen, "open"},
		{StatusHalfOpen, "half_open"},
		{EndpointStatus(99), "unknown"},
	}
	for _, c := range cases {
		if got := c.s.String(); got != c.want {
			t.Errorf("Status(%d).String() = %q, want %q", c.s, got, c.want)
		}
	}
}

// ComposeHealth must combine a RegistryReader and HealthPolicy into a
// single value satisfying both interfaces.
func TestComposeHealth_RoundTrip(t *testing.T) {
	r := testRegistry()
	p := NewAlwaysHealthyPolicy()
	h := ComposeHealth(r, p)

	// RegistryReader half — should defer to wrapped registry.
	if got := h.GetDefault(); got != "qwen" {
		t.Errorf("GetDefault = %q, want qwen", got)
	}
	if h.GetEndpoint("claude-sonnet") == nil {
		t.Error("GetEndpoint should defer to wrapped registry")
	}

	// HealthPolicy half — should defer to wrapped policy.
	if !h.IsHealthy("anything") {
		t.Error("IsHealthy should defer to AlwaysHealthyPolicy")
	}
}

// Nil RegistryReader is a programming error; ComposeHealth must panic
// rather than return a half-broken value.
func TestComposeHealth_NilRegistryPanics(t *testing.T) {
	defer func() {
		if r := recover(); r == nil {
			t.Fatal("expected panic on nil registry")
		}
	}()
	_ = ComposeHealth(nil, NewAlwaysHealthyPolicy())
}

// Nil policy is a soft default — substituted with AlwaysHealthy so
// callers don't have to nil-guard.
func TestComposeHealth_NilPolicySubstitutesAlwaysHealthy(t *testing.T) {
	h := ComposeHealth(testRegistry(), nil)
	if !h.IsHealthy("anything") {
		t.Error("nil policy should fall back to always-healthy")
	}
}

// Compile-time assertion that the composed value satisfies both
// halves of HealthAwareRegistry. Catches accidental signature drift
// between RegistryReader and HealthPolicy.
var _ HealthAwareRegistry = (*healthAwareRegistry)(nil)

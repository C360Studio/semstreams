package agenticmodel

import (
	"log/slog"
	"testing"
	"time"

	"github.com/c360studio/semstreams/agentic"
	"github.com/c360studio/semstreams/model"
)

func newTimeoutTestComponent(t *testing.T, componentTimeout time.Duration, registry *model.Registry) *Component {
	t.Helper()
	return &Component{
		logger:         slog.New(slog.NewTextHandler(discardWriter{}, nil)),
		messageTimeout: componentTimeout,
		modelRegistry:  registry,
	}
}

type discardWriter struct{}

func (discardWriter) Write(p []byte) (int, error) { return len(p), nil }

func TestResolveTimeout_PrecedenceChain(t *testing.T) {
	registry := &model.Registry{
		Capabilities: map[string]*model.CapabilityConfig{
			"fast":       {Timeout: "15s", Preferred: []string{"ep"}},
			"bad-parse":  {Timeout: "not-a-duration", Preferred: []string{"ep"}},
			"no-timeout": {Preferred: []string{"ep"}},
		},
		Endpoints: map[string]*model.EndpointConfig{
			"ep": {URL: "http://x", Model: "m"},
		},
	}

	endpointWithTimeout := &model.EndpointConfig{URL: "http://x", Model: "m", RequestTimeout: "45s"}
	endpointNoTimeout := &model.EndpointConfig{URL: "http://x", Model: "m"}
	endpointBadTimeout := &model.EndpointConfig{URL: "http://x", Model: "m", RequestTimeout: "bogus"}

	tests := []struct {
		name       string
		req        agentic.AgentRequest
		endpoint   *model.EndpointConfig
		capability string
		componentT time.Duration
		want       time.Duration
	}{
		{
			name:       "task wins over everything",
			req:        agentic.AgentRequest{Timeout: "5s"},
			endpoint:   endpointWithTimeout,
			capability: "fast",
			componentT: 120 * time.Second,
			want:       5 * time.Second,
		},
		{
			name:       "endpoint wins when task empty",
			req:        agentic.AgentRequest{},
			endpoint:   endpointWithTimeout,
			capability: "fast",
			componentT: 120 * time.Second,
			want:       45 * time.Second,
		},
		{
			name:       "capability wins when task and endpoint empty",
			req:        agentic.AgentRequest{},
			endpoint:   endpointNoTimeout,
			capability: "fast",
			componentT: 120 * time.Second,
			want:       15 * time.Second,
		},
		{
			name:       "component default wins when task/endpoint/capability empty",
			req:        agentic.AgentRequest{},
			endpoint:   endpointNoTimeout,
			capability: "no-timeout",
			componentT: 90 * time.Second,
			want:       90 * time.Second,
		},
		{
			name:       "hardcoded fallback when component timeout is zero",
			req:        agentic.AgentRequest{},
			endpoint:   endpointNoTimeout,
			capability: "",
			componentT: 0,
			want:       120 * time.Second,
		},
		{
			name:       "invalid task timeout falls through to endpoint",
			req:        agentic.AgentRequest{Timeout: "garbage"},
			endpoint:   endpointWithTimeout,
			capability: "fast",
			componentT: 120 * time.Second,
			want:       45 * time.Second,
		},
		{
			name:       "invalid endpoint timeout falls through to capability",
			req:        agentic.AgentRequest{},
			endpoint:   endpointBadTimeout,
			capability: "fast",
			componentT: 120 * time.Second,
			want:       15 * time.Second,
		},
		{
			name:       "invalid capability timeout falls through to component",
			req:        agentic.AgentRequest{},
			endpoint:   endpointNoTimeout,
			capability: "bad-parse",
			componentT: 77 * time.Second,
			want:       77 * time.Second,
		},
		{
			name:       "nil endpoint skipped cleanly",
			req:        agentic.AgentRequest{},
			endpoint:   nil,
			capability: "fast",
			componentT: 120 * time.Second,
			want:       15 * time.Second,
		},
		{
			name:       "empty capability skipped cleanly",
			req:        agentic.AgentRequest{},
			endpoint:   endpointNoTimeout,
			capability: "",
			componentT: 42 * time.Second,
			want:       42 * time.Second,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			c := newTimeoutTestComponent(t, tc.componentT, registry)
			got := c.resolveTimeout(tc.req, tc.endpoint, tc.capability)
			if got != tc.want {
				t.Errorf("resolveTimeout = %v, want %v", got, tc.want)
			}
		})
	}
}

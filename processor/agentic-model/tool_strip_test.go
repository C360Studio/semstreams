package agenticmodel

import (
	"log/slog"
	"testing"

	"github.com/c360studio/semstreams/agentic"
	"github.com/c360studio/semstreams/model"
)

// TestToolStrip_HonorsResolvedEndpoint pins the doc.go Endpoint Resolution
// contract: the tool-strip decides on the endpoint getClientForRequest actually
// resolved and serves, not the raw req.Model. A capability whose resolved
// endpoint lacks tool support must be stripped; a tool-capable endpoint name
// must keep its tools. Mirrors handleRequest's sequence
// (resolveEndpoint -> stripUnsupportedTools) without NATS/HTTP, matching the
// chain_health_test harness that drives resolveEndpoint directly.
func TestToolStrip_HonorsResolvedEndpoint(t *testing.T) {
	reg := &model.Registry{
		Endpoints: map[string]*model.EndpointConfig{
			"small": {Provider: "ollama", URL: "http://s/v1", Model: "small-model", SupportsTools: false},
			"big":   {Provider: "ollama", URL: "http://b/v1", Model: "big-model", SupportsTools: true},
		},
		Capabilities: map[string]*model.CapabilityConfig{
			// coordinator routes to a NON-tool endpoint on purpose.
			"coordinator": {Preferred: []string{"small"}},
		},
		Defaults: model.DefaultsConfig{Model: "big"},
	}
	comp := &Component{
		logger:        slog.New(slog.NewTextHandler(discardWriter{}, nil)),
		modelRegistry: reg,
		healthPolicy:  model.NewAlwaysHealthyPolicy(),
	}
	tools := []agentic.ToolDefinition{{Name: "do_thing"}}

	t.Run("capability resolving to a non-tool endpoint strips tools", func(t *testing.T) {
		req := agentic.AgentRequest{Model: "coordinator", Tools: tools}
		endpoint, _, _ := comp.resolveEndpoint(req)
		if endpoint == nil || endpoint.SupportsTools {
			t.Fatalf("resolveEndpoint(%q) = %#v, want the non-tool 'small' endpoint", req.Model, endpoint)
		}
		if !stripUnsupportedTools(&req, endpoint, comp.logger) || req.Tools != nil {
			t.Fatalf("req.Tools = %#v, want stripped when the resolved endpoint lacks tool support", req.Tools)
		}
	})

	t.Run("tool-capable endpoint name keeps tools", func(t *testing.T) {
		req := agentic.AgentRequest{Model: "big", Tools: tools}
		endpoint, _, _ := comp.resolveEndpoint(req)
		if endpoint == nil || !endpoint.SupportsTools {
			t.Fatalf("resolveEndpoint(%q) = %#v, want the tool-capable 'big' endpoint", req.Model, endpoint)
		}
		if stripUnsupportedTools(&req, endpoint, comp.logger) || len(req.Tools) != 1 {
			t.Fatalf("req.Tools = %#v, want kept when the resolved endpoint supports tools", req.Tools)
		}
	})
}

package agenticloop

import (
	"testing"

	"github.com/c360studio/semstreams/model"
)

// capabilityRegistry builds a registry whose capability "developer" prefers a
// 200000-token, anthropic-provider endpoint distinct from DefaultContextLimit
// (128000), so a failure to resolve the capability is observable at the limit.
func capabilityRegistry() *model.Registry {
	return &model.Registry{
		Capabilities: map[string]*model.CapabilityConfig{
			"developer": {Preferred: []string{"big"}},
		},
		Endpoints: map[string]*model.EndpointConfig{
			"big": {Provider: "anthropic", Model: "big-model", MaxTokens: 200000},
		},
		Defaults: model.DefaultsConfig{Model: "big"},
	}
}

// TestResolveModelLimit_CapabilityResolvesToEndpoint is the #594 end-to-end
// check at the ContextManager: a loop spawned with a capability name must key
// its context window on the capability's preferred endpoint, not fall to
// DefaultContextLimit (which fires the "model not in registry" WARN). Reads the
// resolved cm.modelLimit directly (internal test package).
func TestResolveModelLimit_CapabilityResolvesToEndpoint(t *testing.T) {
	reg := capabilityRegistry()

	tests := []struct {
		name      string
		model     string
		reg       model.RegistryReader
		wantLimit int
	}{
		{
			name:      "capability resolves to endpoint window (no WARN)",
			model:     "developer",
			reg:       reg,
			wantLimit: 200000,
		},
		{
			name:      "direct endpoint name unchanged",
			model:     "big",
			reg:       reg,
			wantLimit: 200000,
		},
		{
			name:      "unknown name falls to default (WARN path)",
			model:     "totally-unknown",
			reg:       reg,
			wantLimit: DefaultContextLimit,
		},
		{
			name:      "nil registry falls to default",
			model:     "developer",
			reg:       nil,
			wantLimit: DefaultContextLimit,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var opts []ContextManagerOption
			if tt.reg != nil {
				opts = append(opts, WithModelRegistry(tt.reg))
			}
			cm := NewContextManager("loop-1", tt.model, DefaultContextConfig(), opts...)
			if cm.modelLimit != tt.wantLimit {
				t.Fatalf("modelLimit for %q = %d, want %d (DefaultContextLimit=%d)",
					tt.model, cm.modelLimit, tt.wantLimit, DefaultContextLimit)
			}
		})
	}
}

// TestResolveProvider_ResolvesCapability covers the aux-path sibling
// (handlers.go): the provider for a spawned loop's task.Model / entity.Model —
// a capability — must resolve to its endpoint's provider, not return "".
func TestResolveProvider_ResolvesCapability(t *testing.T) {
	reg := capabilityRegistry()

	tests := []struct {
		name     string
		handler  *MessageHandler
		endpoint string
		want     string
	}{
		{
			name:     "capability resolves to endpoint provider",
			handler:  &MessageHandler{modelRegistry: reg},
			endpoint: "developer",
			want:     "anthropic",
		},
		{
			name:     "direct endpoint name unchanged",
			handler:  &MessageHandler{modelRegistry: reg},
			endpoint: "big",
			want:     "anthropic",
		},
		{
			name:     "unknown name returns empty",
			handler:  &MessageHandler{modelRegistry: reg},
			endpoint: "totally-unknown",
			want:     "",
		},
		{
			name:     "empty name returns empty",
			handler:  &MessageHandler{modelRegistry: reg},
			endpoint: "",
			want:     "",
		},
		{
			name:     "nil registry returns empty",
			handler:  &MessageHandler{},
			endpoint: "developer",
			want:     "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.handler.resolveProvider(tt.endpoint); got != tt.want {
				t.Fatalf("resolveProvider(%q) = %q, want %q", tt.endpoint, got, tt.want)
			}
		})
	}
}

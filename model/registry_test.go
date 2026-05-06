package model

import (
	"encoding/json"
	"testing"
	"time"
)

func testRegistry() *Registry {
	return &Registry{
		Capabilities: map[string]*CapabilityConfig{
			"planning": {
				Description: "High-level reasoning",
				Preferred:   []string{"claude-sonnet", "qwen"},
				Fallback:    []string{"qwen-fast"},
			},
			"coding": {
				Description:   "Code generation with tool use",
				Preferred:     []string{"claude-sonnet"},
				Fallback:      []string{"qwen"},
				RequiresTools: true,
			},
			"fast": {
				Description: "Quick tasks",
				Preferred:   []string{"qwen-fast"},
			},
		},
		Endpoints: map[string]*EndpointConfig{
			"claude-sonnet": {
				Provider:      "anthropic",
				Model:         "claude-sonnet-4-20250514",
				MaxTokens:     200000,
				SupportsTools: true,
				ToolFormat:    "anthropic",
				APIKeyEnv:     "ANTHROPIC_API_KEY",
			},
			"qwen": {
				Provider:      "ollama",
				URL:           "http://localhost:11434/v1",
				Model:         "qwen3-coder:30b",
				MaxTokens:     131072,
				SupportsTools: true,
				ToolFormat:    "openai",
			},
			"qwen-fast": {
				Provider:  "ollama",
				URL:       "http://localhost:11434/v1",
				Model:     "qwen3:1.7b",
				MaxTokens: 32768,
			},
		},
		Defaults: DefaultsConfig{
			Model:      "qwen",
			Capability: "planning",
		},
	}
}

func TestValidate(t *testing.T) {
	tests := []struct {
		name    string
		modify  func(*Registry)
		wantErr string
	}{
		{
			name:   "valid registry",
			modify: func(_ *Registry) {},
		},
		{
			name: "no endpoints",
			modify: func(r *Registry) {
				r.Endpoints = nil
			},
			wantErr: "at least one endpoint is required",
		},
		{
			name: "endpoint missing model",
			modify: func(r *Registry) {
				r.Endpoints["bad"] = &EndpointConfig{MaxTokens: 1000}
			},
			wantErr: "endpoint \"bad\": model is required",
		},
		{
			name: "endpoint negative max_tokens",
			modify: func(r *Registry) {
				r.Endpoints["bad"] = &EndpointConfig{Model: "test", MaxTokens: -1}
			},
			wantErr: "endpoint \"bad\": max_tokens must not be negative",
		},
		{
			name: "endpoint negative max_output_tokens",
			modify: func(r *Registry) {
				r.Endpoints["bad"] = &EndpointConfig{
					Model: "test", MaxTokens: 1000, MaxOutputTokens: -1,
				}
			},
			wantErr: "endpoint \"bad\": max_output_tokens must not be negative",
		},
		{
			name: "endpoint unknown provider",
			modify: func(r *Registry) {
				r.Endpoints["bad"] = &EndpointConfig{
					Provider: "unknown", Model: "test", MaxTokens: 1000,
				}
			},
			wantErr: "endpoint \"bad\": unknown provider \"unknown\"",
		},
		{
			name: "endpoint invalid tool_format",
			modify: func(r *Registry) {
				r.Endpoints["bad"] = &EndpointConfig{
					Model: "test", MaxTokens: 1000, ToolFormat: "bad",
				}
			},
			wantErr: "endpoint \"bad\": tool_format must be",
		},
		{
			name: "endpoint invalid reasoning_effort",
			modify: func(r *Registry) {
				r.Endpoints["bad"] = &EndpointConfig{
					Model: "test", MaxTokens: 1000, ReasoningEffort: "extreme",
				}
			},
			wantErr: "endpoint \"bad\": reasoning_effort must be",
		},
		{
			name: "nil endpoint",
			modify: func(r *Registry) {
				r.Endpoints["bad"] = nil
			},
			wantErr: "endpoint \"bad\" is nil",
		},
		{
			name: "endpoint with empty name",
			modify: func(r *Registry) {
				r.Endpoints[""] = &EndpointConfig{Model: "test", MaxTokens: 1000}
			},
			wantErr: "endpoint name must not be empty",
		},
		{
			name: "endpoint name with dot",
			modify: func(r *Registry) {
				r.Endpoints["bad.name"] = &EndpointConfig{Model: "test", MaxTokens: 1000}
			},
			wantErr: "name contains invalid character",
		},
		{
			name: "endpoint name with space",
			modify: func(r *Registry) {
				r.Endpoints["bad name"] = &EndpointConfig{Model: "test", MaxTokens: 1000}
			},
			wantErr: "name contains invalid character",
		},
		{
			name: "endpoint name with slash",
			modify: func(r *Registry) {
				r.Endpoints["org/model"] = &EndpointConfig{Model: "test", MaxTokens: 1000}
			},
			wantErr: "name contains invalid character",
		},
		{
			name: "capability references non-existent preferred",
			modify: func(r *Registry) {
				r.Capabilities["bad"] = &CapabilityConfig{
					Preferred: []string{"nonexistent"},
				}
			},
			wantErr: "capability \"bad\": preferred endpoint \"nonexistent\" does not exist",
		},
		{
			name: "capability references non-existent fallback",
			modify: func(r *Registry) {
				r.Capabilities["bad"] = &CapabilityConfig{
					Preferred: []string{"qwen"},
					Fallback:  []string{"nonexistent"},
				}
			},
			wantErr: "capability \"bad\": fallback endpoint \"nonexistent\" does not exist",
		},
		{
			name: "capability empty preferred",
			modify: func(r *Registry) {
				r.Capabilities["bad"] = &CapabilityConfig{
					Preferred: []string{},
				}
			},
			wantErr: "capability \"bad\": at least one preferred endpoint is required",
		},
		{
			name: "nil capability",
			modify: func(r *Registry) {
				r.Capabilities["bad"] = nil
			},
			wantErr: "capability \"bad\" is nil",
		},
		{
			name: "requires_tools but no tool-capable endpoints",
			modify: func(r *Registry) {
				r.Capabilities["bad"] = &CapabilityConfig{
					Preferred:     []string{"qwen-fast"},
					RequiresTools: true,
				}
			},
			wantErr: "requires_tools is set but no endpoint in the chain supports tools",
		},
		{
			name: "default model references non-existent endpoint",
			modify: func(r *Registry) {
				r.Defaults.Model = "nonexistent"
			},
			wantErr: "defaults.model \"nonexistent\" references non-existent endpoint",
		},
		{
			name: "default capability references non-existent capability",
			modify: func(r *Registry) {
				r.Defaults.Capability = "nonexistent"
			},
			wantErr: "defaults.capability \"nonexistent\" references non-existent capability",
		},
		{
			name: "endpoint invalid idle_conn_timeout",
			modify: func(r *Registry) {
				r.Endpoints["bad"] = &EndpointConfig{
					Model: "test", MaxTokens: 1000, IdleConnTimeout: "30 seconds",
				}
			},
			wantErr: "endpoint \"bad\": idle_conn_timeout \"30 seconds\" is not a valid Go duration",
		},
		{
			name: "endpoint invalid response_header_timeout",
			modify: func(r *Registry) {
				r.Endpoints["bad"] = &EndpointConfig{
					Model: "test", MaxTokens: 1000, ResponseHeaderTimeout: "thirty",
				}
			},
			wantErr: "endpoint \"bad\": response_header_timeout \"thirty\" is not a valid Go duration",
		},
		{
			name: "endpoint invalid request_timeout",
			modify: func(r *Registry) {
				r.Endpoints["bad"] = &EndpointConfig{
					Model: "test", MaxTokens: 1000, RequestTimeout: "5 minutes",
				}
			},
			wantErr: "endpoint \"bad\": request_timeout \"5 minutes\" is not a valid Go duration",
		},
		{
			name: "endpoint valid duration fields",
			modify: func(r *Registry) {
				r.Endpoints["good"] = &EndpointConfig{
					Model: "test", MaxTokens: 1000,
					IdleConnTimeout: "30s", ResponseHeaderTimeout: "45s",
					RequestTimeout: "10m", DisableKeepAlives: true,
				}
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := testRegistry()
			tt.modify(r)
			err := r.Validate()
			if tt.wantErr == "" {
				if err != nil {
					t.Fatalf("unexpected error: %v", err)
				}
				return
			}
			if err == nil {
				t.Fatal("expected error, got nil")
			}
			if got := err.Error(); !contains(got, tt.wantErr) {
				t.Fatalf("error %q does not contain %q", got, tt.wantErr)
			}
		})
	}
}

func TestResolve(t *testing.T) {
	r := testRegistry()

	tests := []struct {
		capability string
		want       string
	}{
		{"planning", "claude-sonnet"},
		{"coding", "claude-sonnet"}, // requires_tools filters, claude-sonnet supports tools
		{"fast", "qwen-fast"},
		{"unknown", "qwen"}, // falls back to default model
	}

	for _, tt := range tests {
		t.Run(tt.capability, func(t *testing.T) {
			got := r.Resolve(tt.capability)
			if got != tt.want {
				t.Fatalf("Resolve(%q) = %q, want %q", tt.capability, got, tt.want)
			}
		})
	}
}

func TestResolve_RequiresToolsFiltering(t *testing.T) {
	r := &Registry{
		Capabilities: map[string]*CapabilityConfig{
			"tools": {
				Preferred:     []string{"no-tools", "has-tools"},
				RequiresTools: true,
			},
		},
		Endpoints: map[string]*EndpointConfig{
			"no-tools": {
				Model: "basic", MaxTokens: 32768,
			},
			"has-tools": {
				Model: "advanced", MaxTokens: 128000, SupportsTools: true,
			},
		},
		Defaults: DefaultsConfig{Model: "no-tools"},
	}

	// Should skip "no-tools" and return "has-tools"
	got := r.Resolve("tools")
	if got != "has-tools" {
		t.Fatalf("Resolve(\"tools\") = %q, want \"has-tools\"", got)
	}
}

func TestGetFallbackChain(t *testing.T) {
	r := testRegistry()

	tests := []struct {
		capability string
		want       []string
	}{
		{"planning", []string{"claude-sonnet", "qwen", "qwen-fast"}},
		{"coding", []string{"claude-sonnet", "qwen"}}, // requires_tools filters out qwen-fast
		{"fast", []string{"qwen-fast"}},
		{"unknown", nil},
	}

	for _, tt := range tests {
		t.Run(tt.capability, func(t *testing.T) {
			got := r.GetFallbackChain(tt.capability)
			if !slicesEqual(got, tt.want) {
				t.Fatalf("GetFallbackChain(%q) = %v, want %v", tt.capability, got, tt.want)
			}
		})
	}
}

func TestGetEndpoint(t *testing.T) {
	r := testRegistry()

	t.Run("existing", func(t *testing.T) {
		ep := r.GetEndpoint("claude-sonnet")
		if ep == nil {
			t.Fatal("expected non-nil endpoint")
		}
		if ep.Model != "claude-sonnet-4-20250514" {
			t.Fatalf("got model %q, want %q", ep.Model, "claude-sonnet-4-20250514")
		}
		if ep.MaxTokens != 200000 {
			t.Fatalf("got max_tokens %d, want %d", ep.MaxTokens, 200000)
		}
		if !ep.SupportsTools {
			t.Fatal("expected supports_tools=true")
		}
	})

	t.Run("non-existent", func(t *testing.T) {
		ep := r.GetEndpoint("nonexistent")
		if ep != nil {
			t.Fatalf("expected nil, got %+v", ep)
		}
	})
}

func TestGetMaxTokens(t *testing.T) {
	r := testRegistry()

	tests := []struct {
		name string
		want int
	}{
		{"claude-sonnet", 200000},
		{"qwen", 131072},
		{"qwen-fast", 32768},
		{"nonexistent", 0},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := r.GetMaxTokens(tt.name)
			if got != tt.want {
				t.Fatalf("GetMaxTokens(%q) = %d, want %d", tt.name, got, tt.want)
			}
		})
	}
}

func TestGetDefault(t *testing.T) {
	r := testRegistry()
	if got := r.GetDefault(); got != "qwen" {
		t.Fatalf("GetDefault() = %q, want %q", got, "qwen")
	}
}

func TestListCapabilities(t *testing.T) {
	r := testRegistry()
	got := r.ListCapabilities()
	want := []string{"coding", "fast", "planning"}
	if !slicesEqual(got, want) {
		t.Fatalf("ListCapabilities() = %v, want %v", got, want)
	}
}

func TestListEndpoints(t *testing.T) {
	r := testRegistry()
	got := r.ListEndpoints()
	want := []string{"claude-sonnet", "qwen", "qwen-fast"}
	if !slicesEqual(got, want) {
		t.Fatalf("ListEndpoints() = %v, want %v", got, want)
	}
}

func TestJSONRoundTrip(t *testing.T) {
	r := testRegistry()
	data, err := json.Marshal(r)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}

	var got Registry
	if err := json.Unmarshal(data, &got); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}

	if err := got.Validate(); err != nil {
		t.Fatalf("round-tripped registry is invalid: %v", err)
	}

	if got.Defaults.Model != r.Defaults.Model {
		t.Fatalf("defaults.model: got %q, want %q", got.Defaults.Model, r.Defaults.Model)
	}
	if len(got.Endpoints) != len(r.Endpoints) {
		t.Fatalf("endpoints count: got %d, want %d", len(got.Endpoints), len(r.Endpoints))
	}
	if len(got.Capabilities) != len(r.Capabilities) {
		t.Fatalf("capabilities count: got %d, want %d", len(got.Capabilities), len(r.Capabilities))
	}
}

func TestEndpointOptions(t *testing.T) {
	r := &Registry{
		Endpoints: map[string]*EndpointConfig{
			"thinking": {
				Provider:  "ollama",
				URL:       "http://localhost:11434/v1",
				Model:     "qwen3:32b",
				MaxTokens: 131072,
				Options: map[string]any{
					"enable_thinking": true,
					"thinking_budget": 4096,
				},
			},
		},
		Defaults: DefaultsConfig{Model: "thinking"},
	}

	if err := r.Validate(); err != nil {
		t.Fatalf("registry with options should be valid: %v", err)
	}

	ep := r.GetEndpoint("thinking")
	if ep.Options == nil {
		t.Fatal("Options should not be nil")
	}
	if ep.Options["enable_thinking"] != true {
		t.Errorf("enable_thinking = %v, want true", ep.Options["enable_thinking"])
	}
	if ep.Options["thinking_budget"] != 4096 {
		t.Errorf("thinking_budget = %v, want 4096", ep.Options["thinking_budget"])
	}

	// JSON round-trip preserves options
	data, err := json.Marshal(r)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var got Registry
	if err := json.Unmarshal(data, &got); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	gotEP := got.GetEndpoint("thinking")
	if gotEP.Options == nil {
		t.Fatal("Options lost after round-trip")
	}
	if gotEP.Options["enable_thinking"] != true {
		t.Errorf("round-trip enable_thinking = %v, want true", gotEP.Options["enable_thinking"])
	}
	if gotEP.Options["thinking_budget"] != float64(4096) {
		t.Errorf("round-trip thinking_budget = %v, want 4096", gotEP.Options["thinking_budget"])
	}
}

func TestStreamFieldRoundTrip(t *testing.T) {
	r := &Registry{
		Endpoints: map[string]*EndpointConfig{
			"streaming": {
				Provider:  "ollama",
				URL:       "http://localhost:11434/v1",
				Model:     "qwen3:32b",
				MaxTokens: 131072,
				Stream:    true,
			},
			"non-streaming": {
				Provider:  "ollama",
				URL:       "http://localhost:11434/v1",
				Model:     "qwen3:1.7b",
				MaxTokens: 32768,
			},
		},
		Defaults: DefaultsConfig{Model: "streaming"},
	}

	if err := r.Validate(); err != nil {
		t.Fatalf("registry with stream should be valid: %v", err)
	}

	// Verify direct access
	if !r.GetEndpoint("streaming").Stream {
		t.Error("streaming endpoint: Stream = false, want true")
	}
	if r.GetEndpoint("non-streaming").Stream {
		t.Error("non-streaming endpoint: Stream = true, want false")
	}

	// JSON round-trip
	data, err := json.Marshal(r)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var got Registry
	if err := json.Unmarshal(data, &got); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if !got.GetEndpoint("streaming").Stream {
		t.Error("round-trip: streaming endpoint Stream = false, want true")
	}
	if got.GetEndpoint("non-streaming").Stream {
		t.Error("round-trip: non-streaming endpoint Stream = true, want false")
	}

	// Verify omitempty: Stream=false should not appear in JSON
	if contains(string(data), `"stream":false`) {
		t.Error("Stream=false should be omitted from JSON")
	}
	if !contains(string(data), `"stream":true`) {
		t.Error("Stream=true should be present in JSON")
	}
}

func TestReasoningEffortFieldRoundTrip(t *testing.T) {
	r := &Registry{
		Endpoints: map[string]*EndpointConfig{
			"reasoning": {
				Provider:        "openai",
				Model:           "o3-mini",
				MaxTokens:       100000,
				APIKeyEnv:       "OPENAI_API_KEY",
				ReasoningEffort: "high",
			},
			"no-reasoning": {
				Provider:  "ollama",
				URL:       "http://localhost:11434/v1",
				Model:     "qwen3:1.7b",
				MaxTokens: 32768,
			},
		},
		Defaults: DefaultsConfig{Model: "reasoning"},
	}

	if err := r.Validate(); err != nil {
		t.Fatalf("registry with reasoning_effort should be valid: %v", err)
	}

	// Verify direct access
	if r.GetEndpoint("reasoning").ReasoningEffort != "high" {
		t.Errorf("reasoning endpoint: ReasoningEffort = %q, want %q",
			r.GetEndpoint("reasoning").ReasoningEffort, "high")
	}
	if r.GetEndpoint("no-reasoning").ReasoningEffort != "" {
		t.Error("no-reasoning endpoint: ReasoningEffort should be empty")
	}

	// JSON round-trip
	data, err := json.Marshal(r)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var got Registry
	if err := json.Unmarshal(data, &got); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if got.GetEndpoint("reasoning").ReasoningEffort != "high" {
		t.Error("round-trip: reasoning_effort lost")
	}
	if got.GetEndpoint("no-reasoning").ReasoningEffort != "" {
		t.Error("round-trip: no-reasoning should have empty reasoning_effort")
	}

	// Verify omitempty: empty reasoning_effort should not appear in JSON
	if contains(string(data), `"reasoning_effort":""`) {
		t.Error("empty reasoning_effort should be omitted from JSON")
	}
	if !contains(string(data), `"reasoning_effort":"high"`) {
		t.Error("reasoning_effort:high should be present in JSON")
	}
}

func TestReasoningEffortValidValues(t *testing.T) {
	for _, effort := range []string{"", "none", "low", "medium", "high"} {
		t.Run("effort="+effort, func(t *testing.T) {
			r := &Registry{
				Endpoints: map[string]*EndpointConfig{
					"test": {
						Model: "o3", MaxTokens: 100000, ReasoningEffort: effort,
					},
				},
				Defaults: DefaultsConfig{Model: "test"},
			}
			if err := r.Validate(); err != nil {
				t.Fatalf("reasoning_effort=%q should be valid: %v", effort, err)
			}
		})
	}
}

func TestMinimalRegistry(t *testing.T) {
	r := &Registry{
		Endpoints: map[string]*EndpointConfig{
			"default": {
				Provider:  "ollama",
				URL:       "http://localhost:11434/v1",
				Model:     "llama3.2",
				MaxTokens: 128000,
			},
		},
		Defaults: DefaultsConfig{Model: "default"},
	}

	if err := r.Validate(); err != nil {
		t.Fatalf("minimal registry should be valid: %v", err)
	}

	if got := r.GetDefault(); got != "default" {
		t.Fatalf("GetDefault() = %q, want %q", got, "default")
	}

	if got := r.GetMaxTokens("default"); got != 128000 {
		t.Fatalf("GetMaxTokens(\"default\") = %d, want %d", got, 128000)
	}

	// Unknown capability falls back to default
	if got := r.Resolve("unknown"); got != "default" {
		t.Fatalf("Resolve(\"unknown\") = %q, want %q", got, "default")
	}
}

func TestPricingFieldsRoundTrip(t *testing.T) {
	r := &Registry{
		Endpoints: map[string]*EndpointConfig{
			"priced": {
				Provider:               "openai",
				Model:                  "gpt-4o",
				MaxTokens:              128000,
				APIKeyEnv:              "OPENAI_API_KEY",
				InputPricePer1MTokens:  2.50,
				OutputPricePer1MTokens: 10.00,
			},
			"free": {
				Provider:  "ollama",
				URL:       "http://localhost:11434/v1",
				Model:     "qwen3:1.7b",
				MaxTokens: 32768,
			},
		},
		Defaults: DefaultsConfig{Model: "priced"},
	}

	if err := r.Validate(); err != nil {
		t.Fatalf("registry with pricing should be valid: %v", err)
	}

	// Verify direct access
	if r.GetEndpoint("priced").InputPricePer1MTokens != 2.50 {
		t.Errorf("InputPricePer1MTokens = %v, want 2.50", r.GetEndpoint("priced").InputPricePer1MTokens)
	}
	if r.GetEndpoint("priced").OutputPricePer1MTokens != 10.00 {
		t.Errorf("OutputPricePer1MTokens = %v, want 10.00", r.GetEndpoint("priced").OutputPricePer1MTokens)
	}
	if r.GetEndpoint("free").InputPricePer1MTokens != 0 {
		t.Error("free endpoint: InputPricePer1MTokens should be zero")
	}

	// JSON round-trip
	data, err := json.Marshal(r)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var got Registry
	if err := json.Unmarshal(data, &got); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if got.GetEndpoint("priced").InputPricePer1MTokens != 2.50 {
		t.Error("round-trip: input price lost")
	}
	if got.GetEndpoint("priced").OutputPricePer1MTokens != 10.00 {
		t.Error("round-trip: output price lost")
	}

	// Verify omitempty: zero prices should not appear in JSON
	if contains(string(data), `"input_price_per_1m_tokens":0`) {
		t.Error("zero input price should be omitted from JSON")
	}
	if !contains(string(data), `"input_price_per_1m_tokens":2.5`) {
		t.Error("input_price_per_1m_tokens:2.5 should be present in JSON")
	}
}

func TestPricingFieldsValidation(t *testing.T) {
	tests := []struct {
		name    string
		input   float64
		output  float64
		wantErr string
	}{
		{"zero prices valid", 0, 0, ""},
		{"positive prices valid", 3.00, 15.00, ""},
		{"negative input price", -1.0, 10.0, "input_price_per_1m_tokens must not be negative"},
		{"negative output price", 3.0, -5.0, "output_price_per_1m_tokens must not be negative"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := &Registry{
				Endpoints: map[string]*EndpointConfig{
					"test": {
						Model:                  "test-model",
						MaxTokens:              100000,
						InputPricePer1MTokens:  tt.input,
						OutputPricePer1MTokens: tt.output,
					},
				},
				Defaults: DefaultsConfig{Model: "test"},
			}
			err := r.Validate()
			if tt.wantErr == "" {
				if err != nil {
					t.Fatalf("unexpected error: %v", err)
				}
				return
			}
			if err == nil {
				t.Fatal("expected error, got nil")
			}
			if !contains(err.Error(), tt.wantErr) {
				t.Fatalf("error %q does not contain %q", err.Error(), tt.wantErr)
			}
		})
	}
}

func TestResolveSummarization(t *testing.T) {
	tests := []struct {
		name     string
		registry *Registry
		want     string
	}{
		{
			name: "explicit summarization capability returns preferred endpoint",
			registry: &Registry{
				Capabilities: map[string]*CapabilityConfig{
					"summarization": {
						Description: "Long context summarization",
						Preferred:   []string{"claude-sonnet"},
					},
				},
				Endpoints: map[string]*EndpointConfig{
					"claude-sonnet": {
						Provider: "anthropic", Model: "claude-sonnet-4-20250514", MaxTokens: 200000,
					},
					"qwen-fast": {
						Provider: "ollama", URL: "http://localhost:11434/v1", Model: "qwen3:1.7b", MaxTokens: 32768,
					},
				},
				Defaults: DefaultsConfig{Model: "qwen-fast"},
			},
			want: "claude-sonnet",
		},
		{
			name: "no capability, multiple endpoints returns largest MaxTokens",
			registry: &Registry{
				Endpoints: map[string]*EndpointConfig{
					"large": {
						Provider: "anthropic", Model: "claude-sonnet-4-20250514", MaxTokens: 200000,
					},
					"medium": {
						Provider: "ollama", URL: "http://localhost:11434/v1", Model: "qwen3:30b", MaxTokens: 131072,
					},
					"small": {
						Provider: "ollama", URL: "http://localhost:11434/v1", Model: "qwen3:1.7b", MaxTokens: 32768,
					},
				},
				Defaults: DefaultsConfig{Model: "small"},
			},
			want: "large",
		},
		{
			name: "no capability, tie in MaxTokens resolves alphabetically",
			registry: &Registry{
				Endpoints: map[string]*EndpointConfig{
					"alpha": {
						Provider: "ollama", URL: "http://localhost:11434/v1", Model: "model-a", MaxTokens: 128000,
					},
					"beta": {
						Provider: "ollama", URL: "http://localhost:11434/v1", Model: "model-b", MaxTokens: 128000,
					},
					"gamma": {
						Provider: "ollama", URL: "http://localhost:11434/v1", Model: "model-c", MaxTokens: 128000,
					},
				},
				Defaults: DefaultsConfig{Model: "gamma"},
			},
			// All tied at 128000; alphabetically first is "alpha".
			want: "alpha",
		},
		{
			name: "single endpoint returns that endpoint",
			registry: &Registry{
				Endpoints: map[string]*EndpointConfig{
					"only": {
						Provider: "ollama", URL: "http://localhost:11434/v1", Model: "llama3.2", MaxTokens: 128000,
					},
				},
				Defaults: DefaultsConfig{Model: "only"},
			},
			want: "only",
		},
		{
			name: "no endpoints falls back to default model",
			registry: &Registry{
				// Bypass Validate() to test the defensive fallback path directly.
				// Endpoints is intentionally empty here.
				Endpoints: map[string]*EndpointConfig{},
				Defaults:  DefaultsConfig{Model: "fallback-default"},
			},
			want: "fallback-default",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := tt.registry.ResolveSummarization()
			if got != tt.want {
				t.Fatalf("ResolveSummarization() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestValidate_ZeroMaxTokens(t *testing.T) {
	r := &Registry{
		Endpoints: map[string]*EndpointConfig{
			"embedder": {
				Provider:  "openai",
				URL:       "http://localhost:8081/v1",
				Model:     "all-MiniLM-L6-v2",
				MaxTokens: 0, // valid: 0 = not applicable (e.g. embedding endpoints)
			},
		},
		Defaults: DefaultsConfig{Model: "embedder"},
	}
	if err := r.Validate(); err != nil {
		t.Fatalf("MaxTokens=0 should be valid: %v", err)
	}
}

func TestResolveEndpoint(t *testing.T) {
	reg := &Registry{
		Capabilities: map[string]*CapabilityConfig{
			"embedding": {
				Preferred: []string{"embedder"},
			},
		},
		Endpoints: map[string]*EndpointConfig{
			"embedder": {
				Provider:  "openai",
				URL:       "http://localhost:8081/v1",
				Model:     "all-MiniLM-L6-v2",
				MaxTokens: 0,
				APIKeyEnv: "TEST_NONEXISTENT_KEY_12345",
			},
		},
		Defaults: DefaultsConfig{Model: "embedder"},
	}

	t.Run("nil registry returns error", func(t *testing.T) {
		_, err := ResolveEndpoint(nil, "embedding")
		if err == nil {
			t.Fatal("expected error for nil registry")
		}
		if !contains(err.Error(), "model registry required") {
			t.Fatalf("error %q should mention model registry required", err.Error())
		}
	})

	t.Run("missing capability falls back to default, resolves endpoint", func(t *testing.T) {
		resolved, err := ResolveEndpoint(reg, "nonexistent_capability")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if resolved.URL != "http://localhost:8081/v1" {
			t.Fatalf("URL = %q, want http://localhost:8081/v1", resolved.URL)
		}
		if resolved.Model != "all-MiniLM-L6-v2" {
			t.Fatalf("Model = %q, want all-MiniLM-L6-v2", resolved.Model)
		}
	})

	t.Run("successful resolve", func(t *testing.T) {
		resolved, err := ResolveEndpoint(reg, "embedding")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if resolved.URL != "http://localhost:8081/v1" {
			t.Fatalf("URL = %q, want http://localhost:8081/v1", resolved.URL)
		}
		if resolved.Model != "all-MiniLM-L6-v2" {
			t.Fatalf("Model = %q, want all-MiniLM-L6-v2", resolved.Model)
		}
		// APIKey should be empty since env var doesn't exist
		if resolved.APIKey != "" {
			t.Fatalf("APIKey = %q, want empty (env var not set)", resolved.APIKey)
		}
	})

	t.Run("no endpoint configured returns error", func(t *testing.T) {
		emptyReg := &Registry{
			Endpoints: map[string]*EndpointConfig{},
			Defaults:  DefaultsConfig{Model: "nonexistent"},
		}
		_, err := ResolveEndpoint(emptyReg, "embedding")
		if err == nil {
			t.Fatal("expected error for missing endpoint")
		}
		if !contains(err.Error(), "no endpoint for capability") {
			t.Fatalf("error %q should mention no endpoint", err.Error())
		}
	})
}

// helpers

func contains(s, substr string) bool {
	return len(s) >= len(substr) && searchString(s, substr)
}

func searchString(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}

func slicesEqual(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

// TestCapabilityConstants_Values pins the wire string for every capability
// constant. The strings appear in production model_registry JSON so they
// are part of the operator-facing config contract — renaming one is a
// breaking change. Locking the value in a test makes accidental drift
// impossible.
func TestCapabilityConstants_Values(t *testing.T) {
	tests := []struct {
		name string
		got  string
		want string
	}{
		{"summarization", CapabilitySummarization, "summarization"},
		{"community_summary", CapabilityCommunitySummary, "community_summary"},
		{"embedding", CapabilityEmbedding, "embedding"},
		{"query_classification", CapabilityQueryClassification, "query_classification"},
		{"answer_synthesis", CapabilityAnswerSynthesis, "answer_synthesis"},
		{"intent_classification", CapabilityIntentClassification, "intent_classification"},
		{"layer_normalization", CapabilityLayerNormalization, "layer_normalization"},
		{"anomaly_review", CapabilityAnomalyReview, "anomaly_review"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.got != tt.want {
				t.Errorf("constant = %q, want %q", tt.got, tt.want)
			}
		})
	}
}

// TestResolveEndpointWithConfig_KeepalivePlumbing pins the field-by-field
// plumbing contract: an EndpointConfig with DisableKeepAlives /
// IdleConnTimeout / ResponseHeaderTimeout set must survive
// ResolveEndpointWithConfig and reach the LLM client builder unchanged.
// The historical bug (semspec smoke-#10) had the fields silently stripped
// on the way through ResolveEndpoint; this guards the regression class
// at the framework level.
func TestResolveEndpointWithConfig_KeepalivePlumbing(t *testing.T) {
	reg := &Registry{
		Endpoints: map[string]*EndpointConfig{
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
		Capabilities: map[string]*CapabilityConfig{
			CapabilityAnswerSynthesis: {
				Preferred: []string{"sparky-qwen"},
			},
		},
		Defaults: DefaultsConfig{Model: "sparky-qwen"},
	}

	resolved, ep, err := ResolveEndpointWithConfig(reg, CapabilityAnswerSynthesis)
	if err != nil {
		t.Fatalf("ResolveEndpointWithConfig: %v", err)
	}

	// The minimal trio still resolves correctly.
	if resolved.URL != "http://sparky:8080/v1" {
		t.Errorf("URL = %q, want sparky URL", resolved.URL)
	}
	if resolved.Model != "qwen3-coder:30b" {
		t.Errorf("Model = %q, want qwen3-coder:30b", resolved.Model)
	}

	// And the full EndpointConfig carries the keepalive fields the LLM
	// client builder needs. These were the bug.
	if !ep.DisableKeepAlives {
		t.Errorf("DisableKeepAlives = false, want true (semspec smoke-#10 regression)")
	}
	if ep.IdleConnTimeout != "10s" {
		t.Errorf("IdleConnTimeout = %q, want 10s", ep.IdleConnTimeout)
	}
	if ep.ResponseHeaderTimeout != "30s" {
		t.Errorf("ResponseHeaderTimeout = %q, want 30s", ep.ResponseHeaderTimeout)
	}
}

// TestResolveEndpointWithConfig_NoEndpointsAtAll covers the registry-empty
// branch: ResolveEndpoint returns an error and ResolveEndpointWithConfig
// propagates it without panicking on the downstream lookup.
func TestResolveEndpointWithConfig_NoEndpointsAtAll(t *testing.T) {
	reg := &Registry{
		Endpoints: map[string]*EndpointConfig{},
	}

	_, _, err := ResolveEndpointWithConfig(reg, CapabilityAnswerSynthesis)
	if err == nil {
		t.Fatal("expected error when no endpoints configured, got nil")
	}
}

// TestResolveCapabilityTimeout pins the precedence chain
// (endpoint > capability > default) — load-bearing for capability config
// to actually reach the wire. The community_summary 300s dead-config bug
// surfaced when graph-clustering bypassed this chain entirely, but the
// chain itself is also a separate failure surface that warrants direct
// coverage.
func TestResolveCapabilityTimeout(t *testing.T) {
	const dflt = 60 * time.Second

	tests := []struct {
		name    string
		reg     *Registry
		want    time.Duration
		comment string
	}{
		{
			name: "endpoint_request_timeout_wins",
			reg: &Registry{
				Endpoints: map[string]*EndpointConfig{
					"e": {URL: "http://e", Model: "m", RequestTimeout: "120s"},
				},
				Capabilities: map[string]*CapabilityConfig{
					CapabilityCommunitySummary: {Preferred: []string{"e"}, Timeout: "240s"},
				},
				Defaults: DefaultsConfig{Model: "e"},
			},
			want:    120 * time.Second,
			comment: "endpoint.request_timeout is most-specific, beats capability.timeout",
		},
		{
			name: "capability_timeout_when_endpoint_empty",
			reg: &Registry{
				Endpoints: map[string]*EndpointConfig{
					"e": {URL: "http://e", Model: "m"},
				},
				Capabilities: map[string]*CapabilityConfig{
					CapabilityCommunitySummary: {Preferred: []string{"e"}, Timeout: "300s"},
				},
				Defaults: DefaultsConfig{Model: "e"},
			},
			want:    300 * time.Second,
			comment: "the bug: capability.timeout=300s must reach the wire when endpoint omits its own",
		},
		{
			name: "default_when_both_empty",
			reg: &Registry{
				Endpoints: map[string]*EndpointConfig{
					"e": {URL: "http://e", Model: "m"},
				},
				Capabilities: map[string]*CapabilityConfig{
					CapabilityCommunitySummary: {Preferred: []string{"e"}},
				},
				Defaults: DefaultsConfig{Model: "e"},
			},
			want:    dflt,
			comment: "fully unconfigured falls through to caller default",
		},
		{
			name: "invalid_endpoint_timeout_falls_through",
			reg: &Registry{
				Endpoints: map[string]*EndpointConfig{
					"e": {URL: "http://e", Model: "m", RequestTimeout: "garbage"},
				},
				Capabilities: map[string]*CapabilityConfig{
					CapabilityCommunitySummary: {Preferred: []string{"e"}, Timeout: "300s"},
				},
				Defaults: DefaultsConfig{Model: "e"},
			},
			want:    300 * time.Second,
			comment: "malformed endpoint.request_timeout warns and falls through to capability — never silently disables",
		},
		{
			name: "invalid_capability_timeout_falls_through_to_default",
			reg: &Registry{
				Endpoints: map[string]*EndpointConfig{
					"e": {URL: "http://e", Model: "m"},
				},
				Capabilities: map[string]*CapabilityConfig{
					CapabilityCommunitySummary: {Preferred: []string{"e"}, Timeout: "garbage"},
				},
				Defaults: DefaultsConfig{Model: "e"},
			},
			want:    dflt,
			comment: "malformed capability.timeout warns and falls through to default",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := ResolveCapabilityTimeout(tt.reg, CapabilityCommunitySummary, dflt, nil)
			if got != tt.want {
				t.Errorf("got %v, want %v (%s)", got, tt.want, tt.comment)
			}
		})
	}

	// Nil interface (not a typed-nil *Registry) returns default. Callers
	// that haven't configured a registry at all hit this path; the guard
	// is defensive against a future caller that forgets the upstream nil
	// check (graph-query / graph-clustering already guard).
	t.Run("nil_registry_returns_default", func(t *testing.T) {
		got := ResolveCapabilityTimeout(nil, CapabilityCommunitySummary, dflt, nil)
		if got != dflt {
			t.Errorf("got %v, want %v", got, dflt)
		}
	})
}

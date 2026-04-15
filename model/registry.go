// Package model provides a unified model registry for centralized endpoint
// configuration, capability-based routing, and tool capability metadata.
package model

import (
	"fmt"
	"os"
	"sort"
)

// Capability name constants for registry-based endpoint resolution.
// Custom capabilities beyond these can be used with Resolve() directly.
const (
	// CapabilitySummarization is used by the agentic loop for context compaction.
	CapabilitySummarization = "summarization"
	// CapabilityCommunitySummary is used by graph-clustering for LLM community summaries.
	CapabilityCommunitySummary = "community_summary"
	// CapabilityEmbedding is used by graph-embedding for HTTP embedding services.
	CapabilityEmbedding = "embedding"
	// CapabilityQueryClassification is used by graph-query for LLM-based query classification.
	CapabilityQueryClassification = "query_classification"
	// CapabilityAnswerSynthesis is used by graph-query to synthesize natural language
	// answers from community summaries and entity digests in globalSearch responses.
	CapabilityAnswerSynthesis = "answer_synthesis"
)

// ResolvedEndpoint holds the resolved connection details for a capability endpoint.
type ResolvedEndpoint struct {
	URL    string
	Model  string
	APIKey string
}

// ResolveEndpoint resolves a capability to its endpoint connection details.
// Returns an error if the registry is nil, or if no endpoint is configured
// for the capability. The API key is read from the environment variable
// specified by EndpointConfig.APIKeyEnv (empty if unset or not configured).
func ResolveEndpoint(reg RegistryReader, capability string) (*ResolvedEndpoint, error) {
	if reg == nil {
		return nil, fmt.Errorf("model registry required for %s", capability)
	}
	name := reg.Resolve(capability)
	ep := reg.GetEndpoint(name)
	if ep == nil {
		return nil, fmt.Errorf("no endpoint for capability %q", capability)
	}
	apiKey := ""
	if ep.APIKeyEnv != "" {
		apiKey = os.Getenv(ep.APIKeyEnv)
	}
	return &ResolvedEndpoint{URL: ep.URL, Model: ep.Model, APIKey: apiKey}, nil
}

// EndpointConfig defines an available model endpoint.
type EndpointConfig struct {
	// Provider identifies the API type: "anthropic", "ollama", "openai", "openrouter".
	Provider string `json:"provider"`
	// URL is the API endpoint. Required for ollama/openai/openrouter, optional for anthropic.
	URL string `json:"url,omitempty"`
	// Model is the model identifier sent to the provider.
	Model string `json:"model"`
	// MaxTokens is the context window size in tokens.
	MaxTokens int `json:"max_tokens"`
	// SupportsTools indicates whether this endpoint supports function/tool calling.
	SupportsTools bool `json:"supports_tools,omitempty"`
	// ToolFormat specifies the tool calling format: "anthropic" or "openai".
	// Empty means auto-detect from provider.
	ToolFormat string `json:"tool_format,omitempty"`
	// APIKeyEnv is the environment variable containing the API key.
	// Required for anthropic/openai/openrouter, ignored for ollama.
	APIKeyEnv string `json:"api_key_env,omitempty"`
	// Options holds provider-specific template parameters passed to the API
	// as chat_template_kwargs. For vLLM/SGLang with thinking models, set
	// "enable_thinking" and "thinking_budget" here.
	//
	// Note: Ollama's OpenAI-compatible endpoint ignores chat_template_kwargs.
	// Ollama thinking models (Qwen3, DeepSeek-R1) always return reasoning_content
	// but the thinking toggle and budget are only controllable via Ollama's
	// native /api/chat endpoint, not the OpenAI-compatible /v1/ endpoint.
	//
	// Do not use for inference parameters (temperature, top_k, etc.) which
	// have dedicated fields in AgentRequest.
	Options map[string]any `json:"options,omitempty"`
	// Stream enables SSE streaming for this endpoint. The client uses
	// CreateChatCompletionStream internally, reducing time-to-first-token.
	// The inter-component protocol remains complete AgentResponse messages.
	Stream bool `json:"stream,omitempty"`
	// ReasoningEffort controls how much effort reasoning models spend thinking.
	// Accepted values: "none" (Gemini only), "low", "medium", "high".
	// Empty means the provider default is used. Forwarded as reasoning_effort
	// on the OpenAI-compatible chat completions request.
	ReasoningEffort string `json:"reasoning_effort,omitempty"`
	// InputPricePer1MTokens is the cost per 1M input tokens in USD.
	// Consumers join this with token usage data to calculate costs.
	InputPricePer1MTokens float64 `json:"input_price_per_1m_tokens,omitempty"`
	// OutputPricePer1MTokens is the cost per 1M output tokens in USD.
	// Consumers join this with token usage data to calculate costs.
	OutputPricePer1MTokens float64 `json:"output_price_per_1m_tokens,omitempty"`
	// RequestsPerMinute limits the rate of requests to this endpoint.
	// 0 means no rate limiting. Applied per-endpoint across all consumers.
	RequestsPerMinute int `json:"requests_per_minute,omitempty"`
	// MaxConcurrent limits concurrent in-flight requests to this endpoint.
	// 0 means no concurrency limit.
	MaxConcurrent int `json:"max_concurrent,omitempty"`
	// RequestTimeout caps a single LLM request to this endpoint.
	// Go duration string (e.g. "45s", "5m"). Empty means no per-endpoint cap;
	// the capability or component-level timeout applies. See agentic-model
	// Timeout Resolution for the full precedence chain.
	RequestTimeout string `json:"request_timeout,omitempty"`
}

// CapabilityConfig defines model preferences for a capability.
type CapabilityConfig struct {
	// Description explains what this capability is for.
	Description string `json:"description,omitempty"`
	// Preferred lists endpoint names in order of preference.
	Preferred []string `json:"preferred"`
	// Fallback lists backup endpoint names if all preferred are unavailable.
	Fallback []string `json:"fallback,omitempty"`
	// RequiresTools filters the chain to only tool-capable endpoints.
	RequiresTools bool `json:"requires_tools,omitempty"`
	// Timeout caps a single LLM request routed via this capability.
	// Go duration string (e.g. "45s", "5m"). Empty means no per-capability
	// cap; the component-level timeout applies. Endpoint and task timeouts
	// take precedence over this value. See agentic-model Timeout Resolution
	// for the full precedence chain.
	Timeout string `json:"timeout,omitempty"`
}

// DefaultsConfig holds default model settings.
type DefaultsConfig struct {
	// Model is the default endpoint name when no capability matches.
	Model string `json:"model"`
	// Capability is the default capability when none specified.
	Capability string `json:"capability,omitempty"`
}

// Registry holds all model endpoint definitions and capability routing.
// It is JSON-serializable for config loading and implements RegistryReader.
type Registry struct {
	Capabilities map[string]*CapabilityConfig `json:"capabilities,omitempty"`
	Endpoints    map[string]*EndpointConfig   `json:"endpoints"`
	Defaults     DefaultsConfig               `json:"defaults"`
}

// RegistryReader provides read-only access to the model registry.
// Components receive this interface via Dependencies.
type RegistryReader interface {
	// Resolve returns the preferred endpoint name for a capability.
	// Returns the first endpoint in the preferred list.
	// If RequiresTools is set, filters to tool-capable endpoints.
	Resolve(capability string) string

	// GetFallbackChain returns all endpoint names for a capability in preference order.
	// Includes both preferred and fallback endpoints.
	GetFallbackChain(capability string) []string

	// GetEndpoint returns the full endpoint configuration for an endpoint name.
	// Returns nil if the endpoint is not configured.
	GetEndpoint(name string) *EndpointConfig

	// GetCapability returns the full capability configuration for a capability
	// name. Returns nil if the name is not a configured capability (e.g. when
	// the caller passed an endpoint name instead).
	GetCapability(name string) *CapabilityConfig

	// GetMaxTokens returns the context window size for an endpoint name.
	// Returns 0 if the endpoint is not configured.
	GetMaxTokens(name string) int

	// GetDefault returns the default endpoint name.
	GetDefault() string

	// ListCapabilities returns all configured capability names sorted alphabetically.
	ListCapabilities() []string

	// ListEndpoints returns all configured endpoint names sorted alphabetically.
	ListEndpoints() []string

	// ResolveSummarization returns the endpoint name to use for context summarization.
	// Resolution order:
	//  1. Explicit "summarization" capability if configured
	//  2. Endpoint with the largest MaxTokens (best suited for long context summarization)
	//  3. The default endpoint as final fallback
	ResolveSummarization() string
}

// Validate checks the registry configuration for consistency.
func (r *Registry) Validate() error {
	if len(r.Endpoints) == 0 {
		return fmt.Errorf("at least one endpoint is required")
	}

	for name, ep := range r.Endpoints {
		if err := validateEndpoint(name, ep); err != nil {
			return err
		}
	}

	for name, cap := range r.Capabilities {
		if err := r.validateCapability(name, cap); err != nil {
			return err
		}
	}

	if r.Defaults.Model != "" {
		if _, ok := r.Endpoints[r.Defaults.Model]; !ok {
			return fmt.Errorf("defaults.model %q references non-existent endpoint", r.Defaults.Model)
		}
	}

	if r.Defaults.Capability != "" {
		if _, ok := r.Capabilities[r.Defaults.Capability]; !ok {
			return fmt.Errorf("defaults.capability %q references non-existent capability", r.Defaults.Capability)
		}
	}

	return nil
}

func validateEndpoint(name string, ep *EndpointConfig) error {
	if ep == nil {
		return fmt.Errorf("endpoint %q is nil", name)
	}
	if ep.Model == "" {
		return fmt.Errorf("endpoint %q: model is required", name)
	}
	if ep.MaxTokens < 0 {
		return fmt.Errorf("endpoint %q: max_tokens must not be negative", name)
	}

	validProviders := map[string]bool{
		"anthropic": true, "ollama": true, "openai": true, "openrouter": true,
	}
	if ep.Provider != "" && !validProviders[ep.Provider] {
		return fmt.Errorf("endpoint %q: unknown provider %q", name, ep.Provider)
	}

	if ep.ToolFormat != "" && ep.ToolFormat != "anthropic" && ep.ToolFormat != "openai" {
		return fmt.Errorf("endpoint %q: tool_format must be \"anthropic\" or \"openai\"", name)
	}

	if ep.ReasoningEffort != "" {
		validEfforts := map[string]bool{
			"none": true, "low": true, "medium": true, "high": true,
		}
		if !validEfforts[ep.ReasoningEffort] {
			return fmt.Errorf("endpoint %q: reasoning_effort must be one of: none, low, medium, high", name)
		}
	}

	if ep.InputPricePer1MTokens < 0 {
		return fmt.Errorf("endpoint %q: input_price_per_1m_tokens must not be negative", name)
	}
	if ep.OutputPricePer1MTokens < 0 {
		return fmt.Errorf("endpoint %q: output_price_per_1m_tokens must not be negative", name)
	}

	return nil
}

func (r *Registry) validateCapability(name string, cap *CapabilityConfig) error {
	if cap == nil {
		return fmt.Errorf("capability %q is nil", name)
	}
	if len(cap.Preferred) == 0 {
		return fmt.Errorf("capability %q: at least one preferred endpoint is required", name)
	}

	// All referenced endpoints must exist
	for _, epName := range cap.Preferred {
		if _, ok := r.Endpoints[epName]; !ok {
			return fmt.Errorf("capability %q: preferred endpoint %q does not exist", name, epName)
		}
	}
	for _, epName := range cap.Fallback {
		if _, ok := r.Endpoints[epName]; !ok {
			return fmt.Errorf("capability %q: fallback endpoint %q does not exist", name, epName)
		}
	}

	// If RequiresTools, at least one endpoint in the chain must support tools
	if cap.RequiresTools {
		chain := append(cap.Preferred, cap.Fallback...)
		hasToolCapable := false
		for _, epName := range chain {
			if ep, ok := r.Endpoints[epName]; ok && ep.SupportsTools {
				hasToolCapable = true
				break
			}
		}
		if !hasToolCapable {
			return fmt.Errorf("capability %q: requires_tools is set but no endpoint in the chain supports tools", name)
		}
	}

	return nil
}

// Resolve returns the preferred endpoint name for a capability.
func (r *Registry) Resolve(capability string) string {
	capCfg, ok := r.Capabilities[capability]
	if !ok {
		return r.Defaults.Model
	}

	chain := r.buildChain(capCfg)
	if len(chain) == 0 {
		return r.Defaults.Model
	}
	return chain[0]
}

// GetFallbackChain returns all endpoint names for a capability in preference order.
func (r *Registry) GetFallbackChain(capability string) []string {
	capCfg, ok := r.Capabilities[capability]
	if !ok {
		return nil
	}
	return r.buildChain(capCfg)
}

// GetEndpoint returns the endpoint configuration for a name, or nil if not found.
func (r *Registry) GetEndpoint(name string) *EndpointConfig {
	ep, ok := r.Endpoints[name]
	if !ok {
		return nil
	}
	return ep
}

// GetCapability returns the capability configuration for a name, or nil if not
// a configured capability.
func (r *Registry) GetCapability(name string) *CapabilityConfig {
	capCfg, ok := r.Capabilities[name]
	if !ok {
		return nil
	}
	return capCfg
}

// GetMaxTokens returns the context window size for an endpoint name.
func (r *Registry) GetMaxTokens(name string) int {
	ep, ok := r.Endpoints[name]
	if !ok {
		return 0
	}
	return ep.MaxTokens
}

// GetDefault returns the default endpoint name.
func (r *Registry) GetDefault() string {
	return r.Defaults.Model
}

// ListCapabilities returns all configured capability names sorted alphabetically.
func (r *Registry) ListCapabilities() []string {
	names := make([]string, 0, len(r.Capabilities))
	for name := range r.Capabilities {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

// ListEndpoints returns all configured endpoint names sorted alphabetically.
func (r *Registry) ListEndpoints() []string {
	names := make([]string, 0, len(r.Endpoints))
	for name := range r.Endpoints {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

// ResolveSummarization returns the endpoint name best suited for summarization.
func (r *Registry) ResolveSummarization() string {
	// 1. Explicit "summarization" capability takes priority.
	if _, ok := r.Capabilities[CapabilitySummarization]; ok {
		if name := r.Resolve(CapabilitySummarization); name != "" {
			return name
		}
	}

	// 2. Find the endpoint with the largest MaxTokens. Sort endpoint names
	// alphabetically before iterating so ties are broken deterministically.
	names := make([]string, 0, len(r.Endpoints))
	for name := range r.Endpoints {
		names = append(names, name)
	}
	sort.Strings(names)

	var bestName string
	var bestTokens int
	for _, name := range names {
		ep := r.Endpoints[name]
		if ep.MaxTokens > bestTokens {
			bestTokens = ep.MaxTokens
			bestName = name
		}
	}
	if bestName != "" {
		return bestName
	}

	// 3. Fall back to the configured default.
	return r.Defaults.Model
}

// buildChain constructs the full endpoint chain for a capability,
// filtering by tool support if RequiresTools is set.
func (r *Registry) buildChain(cap *CapabilityConfig) []string {
	all := make([]string, 0, len(cap.Preferred)+len(cap.Fallback))
	all = append(all, cap.Preferred...)
	all = append(all, cap.Fallback...)

	if !cap.RequiresTools {
		return all
	}

	// Filter to tool-capable endpoints only
	filtered := make([]string, 0, len(all))
	for _, name := range all {
		if ep, ok := r.Endpoints[name]; ok && ep.SupportsTools {
			filtered = append(filtered, name)
		}
	}
	return filtered
}

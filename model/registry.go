// Package model provides a unified model registry for centralized endpoint
// configuration, capability-based routing, and tool capability metadata.
package model

import (
	"fmt"
	"os"
	"sort"
	"time"
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
	// CapabilityIntentClassification is used by agentic-dispatch to classify
	// every incoming user message into an intent (new_task / continue / signal /
	// question / meta) before routing. The most-frequent user-facing LLM call
	// in the system. Unbound deployments fall back to defaults.model.
	CapabilityIntentClassification = "intent_classification"
	// CapabilityLayerNormalization is used by agentic-dispatch to extract
	// structured operating-model entries from freeform onboarding answers.
	// Unbound deployments fall back to defaults.model.
	CapabilityLayerNormalization = "layer_normalization"
	// CapabilityAnomalyReview is used by graph/inference's ReviewWorker to
	// classify suggested missing relationships as approve/reject. Unbound
	// deployments fall back to the community_summary endpoint (the legacy
	// piggyback behavior preserved for backward compatibility).
	CapabilityAnomalyReview = "anomaly_review"
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
	//
	// For provider="ollama", this value is NOT forwarded to the server —
	// Ollama's /v1/chat/completions layer rejects per-request num_ctx by
	// design. Pre-build a Modelfile with PARAMETER num_ctx <N> and point
	// endpoint.model at the derived name. See
	// docs/operations/04-ollama-setup.md. On first request to an Ollama
	// endpoint, agentic-model probes /api/show and WARNs once if the
	// model's num_ctx is below this value.
	//
	// For all other providers this value is used only for summarization
	// routing (picking the largest-context endpoint) and context-budget
	// math in agentic-loop — the provider determines the actual window.
	MaxTokens int `json:"max_tokens"`
	// MaxOutputTokens is the per-request output cap forwarded as the
	// OpenAI-compatible `max_tokens` field on chat completions. It is used
	// only when AgentRequest.MaxTokens is unset (zero); a non-zero per-request
	// value always wins. 0 means no endpoint default — providers fall back to
	// their own (often surprisingly low, e.g. 16384) implicit cap.
	//
	// This is distinct from MaxTokens above (context window). Most providers
	// allow the output cap to be set well below the context window for cost
	// control without changing routing behaviour.
	MaxOutputTokens int `json:"max_output_tokens,omitempty"`
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
	// IdleConnTimeout bounds how long a pooled HTTP connection stays idle
	// before the client closes it. Go duration string (e.g. "30s"). Empty
	// preserves the Go default (90s). Tighten this on endpoints whose
	// upstream gateway silently drops idle connections (Ollama, OpenRouter
	// in some configurations) — a stale pooled connection causes the next
	// Read to block until the request-level timeout fires.
	IdleConnTimeout string `json:"idle_conn_timeout,omitempty"`
	// ResponseHeaderTimeout caps how long the client waits for response
	// headers after the request body is fully written. Go duration string.
	// Empty preserves the Go default (no timeout). DO NOT set this on
	// endpoints that legitimately take longer than the value to generate
	// for non-streaming requests — the server sends headers only after
	// generation completes for non-streaming OpenAI-compat. Safe to set
	// on streaming endpoints (servers send headers immediately for SSE).
	ResponseHeaderTimeout string `json:"response_header_timeout,omitempty"`
	// DisableKeepAlives forces a fresh TCP/TLS connection for every request.
	// Costs a handshake per call (~50-300ms for TLS) but eliminates the
	// stale-pooled-connection class of wedge entirely. Use on known-wedgy
	// gateways where shorter IdleConnTimeout has been insufficient.
	DisableKeepAlives bool `json:"disable_keepalives,omitempty"`
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

// validateEndpointName checks that the endpoint name (registry map key)
// is well-formed enough to be used downstream as part of an entity ID.
// Bad names — empty, dot-bearing, or non-alphanumeric — pass JSON
// unmarshal cleanly but later panic when agentic.ModelEndpointEntityID
// constructs a 6-part ID from them at agentic-loop Start. Catching the
// problem at config-load time instead surfaces a clear error before any
// component starts.
//
// Allowed characters mirror message.IsValidEntityID's per-part rules:
// ASCII letters, digits, hyphen, underscore. Non-empty.
func validateEndpointName(name string) error {
	if name == "" {
		return fmt.Errorf("endpoint name must not be empty")
	}
	for _, c := range name {
		ok := (c >= 'a' && c <= 'z') ||
			(c >= 'A' && c <= 'Z') ||
			(c >= '0' && c <= '9') ||
			c == '-' || c == '_'
		if !ok {
			return fmt.Errorf("endpoint %q: name contains invalid character %q (allowed: alphanumeric, '-', '_')", name, c)
		}
	}
	return nil
}

// validateDurationField parses an optional Go duration string from an endpoint
// config field. Empty values are allowed (treated as "use the default" by the
// HTTP client builder); invalid values are rejected at config load time so
// operators see a parse error before the endpoint is ever invoked.
func validateDurationField(endpointName, field, value string) error {
	if value == "" {
		return nil
	}
	if _, err := time.ParseDuration(value); err != nil {
		return fmt.Errorf("endpoint %q: %s %q is not a valid Go duration: %w",
			endpointName, field, value, err)
	}
	return nil
}

func validateEndpoint(name string, ep *EndpointConfig) error {
	if err := validateEndpointName(name); err != nil {
		return err
	}
	if ep == nil {
		return fmt.Errorf("endpoint %q is nil", name)
	}
	if ep.Model == "" {
		return fmt.Errorf("endpoint %q: model is required", name)
	}
	if ep.MaxTokens < 0 {
		return fmt.Errorf("endpoint %q: max_tokens must not be negative", name)
	}
	if ep.MaxOutputTokens < 0 {
		return fmt.Errorf("endpoint %q: max_output_tokens must not be negative", name)
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

	if err := validateDurationField(name, "idle_conn_timeout", ep.IdleConnTimeout); err != nil {
		return err
	}
	if err := validateDurationField(name, "response_header_timeout", ep.ResponseHeaderTimeout); err != nil {
		return err
	}
	return validateDurationField(name, "request_timeout", ep.RequestTimeout)
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

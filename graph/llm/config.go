package llm

import (
	"fmt"
	"os"
	"time"
)

// Config holds the configuration for LLM services.
type Config struct {
	// Provider specifies which LLM backend to use.
	// Values: "openai" (any OpenAI-compatible API), "none" (disabled)
	Provider string `json:"provider"`

	// BaseURL is the base URL of the LLM service.
	// Examples:
	//   - "http://seminstruct:8083/v1" (local seminstruct, llama.cpp)
	//   - "https://api.openai.com/v1" (OpenAI cloud)
	BaseURL string `json:"base_url"`

	// Model is the model identifier to use.
	// Examples:
	//   - "qwen3-0.6b" (seminstruct hot tier)
	//   - "gpt-4" (OpenAI)
	Model string `json:"model"`

	// APIKey for authentication (optional for local services).
	APIKey string `json:"api_key,omitempty"`

	// TimeoutSeconds for HTTP requests (default: 60).
	TimeoutSeconds int `json:"timeout_seconds,omitempty"`

	// MaxRetries for transient failures (default: 3).
	MaxRetries int `json:"max_retries,omitempty"`

	// PromptsFile is the path to a JSON file with custom prompts.
	// If not specified, uses embedded default prompts.
	PromptsFile string `json:"prompts_file,omitempty"`

	// Domain specifies which prompt domain to use (e.g., "iot", "default").
	// Allows domain-specific prompts registered by processors.
	Domain string `json:"domain,omitempty"`
}

// DefaultConfig returns a config with sensible defaults for local development.
func DefaultConfig() Config {
	return Config{
		Provider:       "none",
		BaseURL:        "http://localhost:8080/v1",
		Model:          "mistral-7b-instruct",
		TimeoutSeconds: 60,
		MaxRetries:     3,
		Domain:         "default",
	}
}

// Timeout returns the timeout as a time.Duration.
func (c Config) Timeout() time.Duration {
	if c.TimeoutSeconds <= 0 {
		return 60 * time.Second
	}
	return time.Duration(c.TimeoutSeconds) * time.Second
}

// IsEnabled returns true if LLM is configured and enabled.
func (c Config) IsEnabled() bool {
	return c.Provider != "" && c.Provider != "none" && c.BaseURL != ""
}

// ToOpenAIConfig converts to OpenAIConfig for client creation.
func (c Config) ToOpenAIConfig() OpenAIConfig {
	return OpenAIConfig{
		BaseURL:    c.BaseURL,
		Model:      c.Model,
		APIKey:     c.GetAPIKey(),
		Timeout:    c.Timeout(),
		MaxRetries: c.MaxRetries,
	}
}

// GetAPIKey returns the API key from config or falls back to LLM_API_KEY env var.
// This allows secure configuration via environment variables in production.
func (c Config) GetAPIKey() string {
	if c.APIKey != "" {
		return c.APIKey
	}
	return os.Getenv("LLM_API_KEY")
}

// String returns a string representation with the API key redacted.
// This prevents accidental logging of sensitive credentials.
func (c Config) String() string {
	masked := c
	if masked.APIKey != "" {
		masked.APIKey = "***REDACTED***"
	}
	return fmt.Sprintf("Config{Provider:%s, BaseURL:%s, Model:%s, APIKey:%s, TimeoutSeconds:%d, MaxRetries:%d, Domain:%s}",
		masked.Provider, masked.BaseURL, masked.Model, masked.APIKey, masked.TimeoutSeconds, masked.MaxRetries, masked.Domain)
}

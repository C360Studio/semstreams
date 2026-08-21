package agenticmodel

import (
	"fmt"
	"time"

	"github.com/c360studio/semstreams/component"
	"github.com/c360studio/semstreams/pkg/errs"
)

// Config holds configuration for agentic-model processor component.
// Model endpoints are resolved from the unified model registry (component.Dependencies.ModelRegistry).
type Config struct {
	Ports              *component.PortConfig `json:"ports"                schema:"type:ports,description:Port configuration,category:basic"`
	ConsumerNameSuffix string                `json:"consumer_name_suffix" schema:"type:string,description:Suffix appended to consumer names for uniqueness,category:advanced"`
	Timeout            string                `json:"timeout"              schema:"type:string,description:Per-request LLM call timeout. Sized 10s below the agentic-model JetStream consumer AckWait (120s) so the LLM context.Done propagates and the call closes cleanly before NATS would otherwise redeliver. Operators raising this past ~115s should also raise the consumer AckWait in lockstep.,category:advanced,default:110s"`
	Retry              RetryConfig           `json:"retry"                schema:"type:object,description:Retry configuration,category:advanced"`
}

// RetryConfig holds retry configuration
type RetryConfig struct {
	MaxAttempts         int    `json:"max_attempts"           schema:"type:int,description:Maximum retry attempts for transient errors (5xx/network),category:advanced,default:3"`
	MaxRateLimitRetries int    `json:"max_rate_limit_retries" schema:"type:int,description:Maximum retry attempts for 429 rate-limit responses (separate from generic retries),category:advanced,default:5"`
	Backoff             string `json:"backoff"                schema:"type:enum,description:Backoff strategy,category:advanced,enum:exponential|linear,default:exponential"`
	InitialDelay        string `json:"initial_delay"          schema:"type:string,description:Initial retry delay for transient errors,category:advanced,default:1s"`
	MaxDelay            string `json:"max_delay"              schema:"type:string,description:Maximum retry delay (caps both generic and rate-limit backoff),category:advanced,default:60s"`
	RateLimitDelay      string `json:"rate_limit_delay"       schema:"type:string,description:Initial delay when rate limited (429) — doubles each attempt,category:advanced,default:15s"`
}

// Validate checks the configuration for errors
func (c *Config) Validate() error {
	if c.Timeout != "" {
		if _, err := time.ParseDuration(c.Timeout); err != nil {
			return errs.WrapInvalid(err, "Config", "Validate", "parse timeout")
		}
	}

	// Apply defaults before validation if Retry is zero value
	if c.Retry.MaxAttempts == 0 {
		c.Retry.MaxAttempts = 3
	}
	if c.Retry.MaxRateLimitRetries == 0 {
		c.Retry.MaxRateLimitRetries = 5
	}
	if c.Retry.Backoff == "" {
		c.Retry.Backoff = "exponential"
	}

	if err := c.Retry.Validate(); err != nil {
		return errs.WrapInvalid(err, "Config", "Validate", "validate retry config")
	}

	return nil
}

// Validate checks the retry configuration for errors
func (r *RetryConfig) Validate() error {
	if r.MaxAttempts < 1 {
		return errs.WrapInvalid(fmt.Errorf("max_attempts must be at least 1"), "RetryConfig", "Validate", "check max_attempts")
	}

	// Empty backoff defaults to exponential
	if r.Backoff != "" && r.Backoff != "exponential" && r.Backoff != "linear" {
		return errs.WrapInvalid(fmt.Errorf("backoff must be 'exponential' or 'linear'"), "RetryConfig", "Validate", "check backoff type")
	}

	if r.InitialDelay != "" {
		if _, err := time.ParseDuration(r.InitialDelay); err != nil {
			return errs.WrapInvalid(err, "RetryConfig", "Validate", "parse initial_delay")
		}
	}
	if r.MaxDelay != "" {
		if _, err := time.ParseDuration(r.MaxDelay); err != nil {
			return errs.WrapInvalid(err, "RetryConfig", "Validate", "parse max_delay")
		}
	}
	if r.RateLimitDelay != "" {
		if _, err := time.ParseDuration(r.RateLimitDelay); err != nil {
			return errs.WrapInvalid(err, "RetryConfig", "Validate", "parse rate_limit_delay")
		}
	}

	return nil
}

// initialDelayDuration returns the parsed InitialDelay, falling back to the given default.
func (r *RetryConfig) initialDelayDuration(defaultDelay time.Duration) time.Duration {
	if r.InitialDelay != "" {
		if d, err := time.ParseDuration(r.InitialDelay); err == nil {
			return d
		}
	}
	return defaultDelay
}

// maxDelayDuration returns the parsed MaxDelay, falling back to the given default.
func (r *RetryConfig) maxDelayDuration(defaultDelay time.Duration) time.Duration {
	if r.MaxDelay != "" {
		if d, err := time.ParseDuration(r.MaxDelay); err == nil {
			return d
		}
	}
	return defaultDelay
}

// maxRateLimitRetriesOrDefault returns MaxRateLimitRetries, falling back to the given default.
func (r *RetryConfig) maxRateLimitRetriesOrDefault(defaultVal int) int {
	if r.MaxRateLimitRetries > 0 {
		return r.MaxRateLimitRetries
	}
	return defaultVal
}

// rateLimitDelayDuration returns the parsed RateLimitDelay, falling back to the given default.
func (r *RetryConfig) rateLimitDelayDuration(defaultDelay time.Duration) time.Duration {
	if r.RateLimitDelay != "" {
		if d, err := time.ParseDuration(r.RateLimitDelay); err == nil {
			return d
		}
	}
	return defaultDelay
}

// DefaultConfig returns default configuration for agentic-model processor
func DefaultConfig() Config {
	inputDefs := []component.PortDefinition{
		{
			Name: "agent.request", Config: component.JetStreamPort{Subjects: []string{"agent.request.>"}, StreamName: "AGENT"}, Required: true,
			Description: "Agent request input (JetStream)",
		},
	}

	outputDefs := []component.PortDefinition{
		{
			Name: "agent.response", Config: component.JetStreamPort{Subjects: []string{"agent.response.*"}, StreamName: "AGENT"}, Required: true,
			Description: "Agent response output (JetStream)",
		},
		{
			Name: "agent.stream", Config: component.NATSPort{Subject: "agent.stream.*"}, Description: "Streaming delta chunks (core NATS, fire-and-forget)",
		},
	}

	return Config{
		Ports: &component.PortConfig{
			Inputs:  inputDefs,
			Outputs: outputDefs,
		},
		Timeout: "110s",
		Retry: RetryConfig{
			MaxAttempts:         3,
			MaxRateLimitRetries: 5,
			Backoff:             "exponential",
			RateLimitDelay:      "15s",
			MaxDelay:            "60s",
		},
	}
}

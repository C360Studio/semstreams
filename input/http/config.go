package http

import (
	"fmt"
	"net/url"
	"strings"
	"time"

	"github.com/c360studio/semstreams/component"
	"github.com/c360studio/semstreams/pkg/errs"
)

// Auth types. Strings rather than typed enums so the JSON wire
// shape stays human-editable; Validate coerces and rejects.
const (
	AuthNone   = "none"
	AuthBearer = "bearer"
	AuthBasic  = "basic"
)

// Decoder modes.
const (
	DecoderJSON  = "json"
	DecoderJSONL = "jsonl"
)

// HTTP methods supported by this component. POST is included so
// callers can hit query-shaped APIs that require a request body;
// streaming POST (chunked uploads) is out of scope.
const (
	MethodGET  = "GET"
	MethodPOST = "POST"
)

// Config holds the operator-facing configuration for the http
// input component.
type Config struct {
	// URL is the endpoint to poll. Required. Must be absolute and
	// use the http or https scheme.
	URL string `json:"url" schema:"type:string,description:Endpoint URL to poll,required:true,category:basic"`

	// Method is the HTTP method (GET or POST). Default GET.
	Method string `json:"method,omitempty" schema:"type:string,description:HTTP method (GET or POST),default:GET,category:basic"`

	// Body is the optional request body sent with POST requests.
	// Ignored for GET.
	Body string `json:"body,omitempty" schema:"type:string,description:Request body (POST only),category:basic"`

	// Interval is the duration between consecutive polls.
	// Default 30s. Minimum 100ms to prevent runaway clients.
	Interval string `json:"interval,omitempty" schema:"type:string,description:Delay between polls,default:30s,category:basic"`

	// Timeout is the per-attempt HTTP request timeout. Retries get
	// a fresh clock — total cycle time is bounded by
	// MaxAttempts * (Timeout + MaxBackoff). Default 10s.
	Timeout string `json:"timeout,omitempty" schema:"type:string,description:Per-attempt HTTP timeout (retries get a fresh clock),default:10s,category:basic"`

	// Headers is a map of additional headers to send with every
	// request. Overrides any default headers but does not override
	// the Authorization header set by Auth.
	Headers map[string]string `json:"headers,omitempty" schema:"type:object,description:Additional request headers,category:advanced"`

	// Auth configures the Authorization header.
	Auth *AuthConfig `json:"auth,omitempty" schema:"type:object,description:Authentication configuration,category:security"`

	// Retry configures retry/backoff behavior.
	Retry *RetryConfig `json:"retry,omitempty" schema:"type:object,description:Retry / backoff configuration,category:reliability"`

	// Decoder configures how response bodies are interpreted.
	Decoder *DecoderConfig `json:"decoder,omitempty" schema:"type:object,description:Response decoder configuration,category:basic"`

	// Ports configures input / output ports. Required: at least
	// one NATS or JetStream output port.
	Ports *component.PortConfig `json:"ports" schema:"type:ports,description:Port configuration,category:basic"`
}

// AuthConfig is the Authorization-header configuration.
type AuthConfig struct {
	Type     string `json:"type" schema:"type:string,description:Auth type (none, bearer, basic),default:none"`
	Token    string `json:"token,omitempty" schema:"type:string,description:Bearer token,category:security"`
	Username string `json:"username,omitempty" schema:"type:string,description:Basic auth username,category:security"`
	Password string `json:"password,omitempty" schema:"type:string,description:Basic auth password,category:security"`
}

// RetryConfig is the retry / backoff configuration.
type RetryConfig struct {
	// MaxAttempts is the total number of attempts (1 = no retry).
	// Default 3.
	MaxAttempts int `json:"max_attempts,omitempty" schema:"type:int,description:Total attempts (1 = no retry),default:3"`

	// InitialBackoff is the first delay before retry. Default 1s.
	InitialBackoff string `json:"initial_backoff,omitempty" schema:"type:string,description:First retry delay,default:1s"`

	// MaxBackoff caps the exponential growth. Default 30s.
	MaxBackoff string `json:"max_backoff,omitempty" schema:"type:string,description:Max retry delay,default:30s"`

	// Multiplier grows the backoff exponentially. Default 2.0.
	Multiplier float64 `json:"multiplier,omitempty" schema:"type:float,description:Backoff multiplier,default:2.0"`
}

// DecoderConfig is the response-body decoding configuration.
type DecoderConfig struct {
	// Mode selects the decoder: "json" (single object per poll) or
	// "jsonl" (line-delimited; one publish per line). Default
	// "json".
	Mode string `json:"mode,omitempty" schema:"type:string,description:Decoder mode (json or jsonl),default:json"`
}

// DefaultConfig returns a Config with field defaults filled in.
// The URL and Ports fields remain zero — callers must supply
// them.
func DefaultConfig() Config {
	return Config{
		Method:   MethodGET,
		Interval: "30s",
		Timeout:  "10s",
		Auth: &AuthConfig{
			Type: AuthNone,
		},
		Retry: &RetryConfig{
			MaxAttempts:    3,
			InitialBackoff: "1s",
			MaxBackoff:     "30s",
			Multiplier:     2.0,
		},
		Decoder: &DecoderConfig{
			Mode: DecoderJSON,
		},
	}
}

// Validate enforces config invariants. Called by the registry
// before the component is constructed.
func (c *Config) Validate() error {
	if c.URL == "" {
		return errs.WrapInvalid(errs.ErrMissingConfig, "Config", "Validate", "url is required")
	}
	u, err := url.Parse(c.URL)
	if err != nil {
		return errs.WrapInvalid(err, "Config", "Validate", "invalid url")
	}
	if u.Scheme != "http" && u.Scheme != "https" {
		return errs.WrapInvalid(errs.ErrInvalidConfig, "Config", "Validate",
			fmt.Sprintf("url scheme must be http or https, got %q", u.Scheme))
	}
	if u.Host == "" {
		return errs.WrapInvalid(errs.ErrInvalidConfig, "Config", "Validate", "url must include a host")
	}

	method := strings.ToUpper(c.Method)
	if method != "" && method != MethodGET && method != MethodPOST {
		return errs.WrapInvalid(errs.ErrInvalidConfig, "Config", "Validate",
			fmt.Sprintf("method must be GET or POST, got %q", c.Method))
	}

	if c.Interval != "" {
		d, err := time.ParseDuration(c.Interval)
		if err != nil {
			return errs.WrapInvalid(err, "Config", "Validate", "invalid interval")
		}
		if d < 100*time.Millisecond {
			return errs.WrapInvalid(errs.ErrInvalidConfig, "Config", "Validate",
				"interval must be at least 100ms to prevent runaway polling")
		}
	}

	if c.Timeout != "" {
		if _, err := time.ParseDuration(c.Timeout); err != nil {
			return errs.WrapInvalid(err, "Config", "Validate", "invalid timeout")
		}
	}

	if c.Auth != nil {
		switch strings.ToLower(c.Auth.Type) {
		case "", AuthNone:
		case AuthBearer:
			if c.Auth.Token == "" {
				return errs.WrapInvalid(errs.ErrMissingConfig, "Config", "Validate",
					"auth.token is required when auth.type is bearer")
			}
		case AuthBasic:
			if c.Auth.Username == "" {
				return errs.WrapInvalid(errs.ErrMissingConfig, "Config", "Validate",
					"auth.username is required when auth.type is basic")
			}
		default:
			return errs.WrapInvalid(errs.ErrInvalidConfig, "Config", "Validate",
				fmt.Sprintf("auth.type must be none, bearer, or basic, got %q", c.Auth.Type))
		}
	}

	if c.Retry != nil {
		if c.Retry.MaxAttempts < 1 {
			return errs.WrapInvalid(errs.ErrInvalidConfig, "Config", "Validate",
				"retry.max_attempts must be >= 1")
		}
		if c.Retry.InitialBackoff != "" {
			if _, err := time.ParseDuration(c.Retry.InitialBackoff); err != nil {
				return errs.WrapInvalid(err, "Config", "Validate", "invalid retry.initial_backoff")
			}
		}
		if c.Retry.MaxBackoff != "" {
			if _, err := time.ParseDuration(c.Retry.MaxBackoff); err != nil {
				return errs.WrapInvalid(err, "Config", "Validate", "invalid retry.max_backoff")
			}
		}
		if c.Retry.Multiplier != 0 && c.Retry.Multiplier < 1.0 {
			return errs.WrapInvalid(errs.ErrInvalidConfig, "Config", "Validate",
				"retry.multiplier must be >= 1.0")
		}
	}

	if c.Decoder != nil {
		switch strings.ToLower(c.Decoder.Mode) {
		case "", DecoderJSON, DecoderJSONL:
		default:
			return errs.WrapInvalid(errs.ErrInvalidConfig, "Config", "Validate",
				fmt.Sprintf("decoder.mode must be json or jsonl, got %q", c.Decoder.Mode))
		}
	}

	if c.Ports == nil || len(c.Ports.Outputs) != 1 {
		return errs.WrapInvalid(errs.ErrMissingConfig, "Config", "Validate",
			"exactly one output port is required")
	}
	for _, output := range c.Ports.Outputs {
		port, err := output.Resolve(component.DirectionOutput)
		if err != nil {
			return errs.WrapInvalid(err, "Config", "Validate", "resolve output port")
		}
		facts, err := port.Facts()
		if err != nil {
			return errs.WrapInvalid(err, "Config", "Validate", "project output port")
		}
		if facts.Kind() != component.PortKindNATS && facts.Kind() != component.PortKindJetStream {
			return errs.WrapInvalid(errs.ErrInvalidConfig, "Config", "Validate",
				fmt.Sprintf("output port %q must be nats or jetstream", port.Name))
		}
		if len(facts.NATSSubjects()) != 1 {
			return errs.WrapInvalid(errs.ErrMissingConfig, "Config", "Validate",
				"NATS output subject is required")
		}
	}

	return nil
}

// resolved holds the parsed / coerced runtime view of a Config.
type resolved struct {
	url            string
	method         string
	body           []byte
	interval       time.Duration
	timeout        time.Duration
	headers        map[string]string
	auth           AuthConfig
	maxAttempts    int
	initialBackoff time.Duration
	maxBackoff     time.Duration
	multiplier     float64
	decoderMode    string
	subject        string
	useJetStream   bool
}

// resolve produces a runtime view of the config, applying
// defaults. Assumes Validate has already succeeded; out-of-range
// values fall back to defaults rather than re-erroring here.
func (c *Config) resolve() (resolved, error) {
	r := resolved{
		url:            c.URL,
		method:         strings.ToUpper(c.Method),
		interval:       30 * time.Second,
		timeout:        10 * time.Second,
		headers:        c.Headers,
		maxAttempts:    3,
		initialBackoff: 1 * time.Second,
		maxBackoff:     30 * time.Second,
		multiplier:     2.0,
		decoderMode:    DecoderJSON,
	}
	if r.method == "" {
		r.method = MethodGET
	}
	if c.Body != "" {
		r.body = []byte(c.Body)
	}
	if c.Interval != "" {
		if d, err := time.ParseDuration(c.Interval); err == nil {
			r.interval = d
		}
	}
	if c.Timeout != "" {
		if d, err := time.ParseDuration(c.Timeout); err == nil {
			r.timeout = d
		}
	}
	if c.Auth != nil {
		r.auth = *c.Auth
		r.auth.Type = strings.ToLower(r.auth.Type)
		if r.auth.Type == "" {
			r.auth.Type = AuthNone
		}
	} else {
		r.auth = AuthConfig{Type: AuthNone}
	}
	if c.Retry != nil {
		if c.Retry.MaxAttempts > 0 {
			r.maxAttempts = c.Retry.MaxAttempts
		}
		if c.Retry.InitialBackoff != "" {
			if d, err := time.ParseDuration(c.Retry.InitialBackoff); err == nil {
				r.initialBackoff = d
			}
		}
		if c.Retry.MaxBackoff != "" {
			if d, err := time.ParseDuration(c.Retry.MaxBackoff); err == nil {
				r.maxBackoff = d
			}
		}
		if c.Retry.Multiplier >= 1.0 {
			r.multiplier = c.Retry.Multiplier
		}
	}
	if c.Decoder != nil && c.Decoder.Mode != "" {
		r.decoderMode = strings.ToLower(c.Decoder.Mode)
	}
	if c.Ports != nil {
		for _, definition := range c.Ports.Outputs {
			output, err := definition.Resolve(component.DirectionOutput)
			if err != nil {
				return resolved{}, err
			}
			facts, err := output.Facts()
			if err != nil {
				return resolved{}, err
			}
			if subjects := facts.NATSSubjects(); len(subjects) == 1 {
				r.subject = subjects[0]
				r.useJetStream = facts.Kind() == component.PortKindJetStream
				break
			}
		}
	}
	return r, nil
}

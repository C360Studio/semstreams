package directorybridge

import (
	"fmt"
	"time"

	"github.com/c360studio/semstreams/component"
)

// Config defines the configuration for the directory bridge component.
type Config struct {
	// Ports defines the input/output port configuration.
	Ports *component.PortConfig `json:"ports" schema:"type:ports,description:Port configuration,category:basic"`

	// Backend selects the directory wire format. One of:
	//   - "http" (default) — POST /v1/agents JSON to DirectoryURL.
	//     The legacy SemStreams-defined HTTP protocol that the in-tree
	//     mock and any privately hosted HTTP directory speak.
	//   - "agntcy_grpc" — AGNTCY agntcy/dir StoreService over gRPC.
	//     Use this for the hosted hub at prod.api.ads.outshift.io and
	//     for any AGNTCY-conformant directory.
	// Empty defaults to "http" for backwards compatibility with
	// pre-PR-C configs.
	Backend string `json:"backend,omitempty" schema:"type:string,description:Wire-format backend (http or agntcy_grpc),category:basic,default:http"`

	// DirectoryURL is the HTTP backend's directory service URL.
	// Ignored when Backend == "agntcy_grpc" — that path uses AgntcyGRPC.Endpoint.
	DirectoryURL string `json:"directory_url" schema:"type:string,description:AGNTCY directory service URL (HTTP backend only),category:basic"`

	// AgntcyGRPC carries the agntcy_grpc backend's settings. Ignored
	// when Backend is not "agntcy_grpc".
	AgntcyGRPC *AgntcyGRPCConfig `json:"agntcy_grpc,omitempty" schema:"type:object,description:agntcy_grpc backend settings,category:basic"`

	// HeartbeatInterval is how often to send heartbeats to the directory.
	HeartbeatInterval string `json:"heartbeat_interval" schema:"type:string,description:Heartbeat interval,category:basic,default:30s"`

	// RegistrationTTL is the time-to-live for registrations.
	RegistrationTTL string `json:"registration_ttl" schema:"type:string,description:Registration time-to-live,category:basic,default:5m"`

	// IdentityProvider specifies which identity provider to use.
	// Values: "local", "agntcy"
	IdentityProvider string `json:"identity_provider" schema:"type:string,description:Identity provider type,category:basic,default:local"`

	// OASFKVBucket is the KV bucket to watch for OASF records.
	OASFKVBucket string `json:"oasf_kv_bucket" schema:"type:string,description:KV bucket for OASF records,category:basic,default:OASF_RECORDS"`

	// RetryCount is the number of retries for failed registrations.
	RetryCount int `json:"retry_count" schema:"type:int,description:Number of registration retries,category:advanced,default:3"`

	// RetryDelay is the initial delay between retries.
	RetryDelay string `json:"retry_delay" schema:"type:string,description:Initial retry delay,category:advanced,default:1s"`

	// ConsumerNameSuffix adds a suffix to consumer names for uniqueness in tests.
	ConsumerNameSuffix string `json:"consumer_name_suffix" schema:"type:string,description:Suffix for consumer names,category:advanced"`

	// DeleteConsumerOnStop enables consumer cleanup on stop (for testing).
	DeleteConsumerOnStop bool `json:"delete_consumer_on_stop,omitempty" schema:"type:bool,description:Delete consumers on Stop,category:advanced,default:false"`
}

// Backend constants accepted by Config.Backend.
const (
	// BackendHTTP is the default — POST JSON to a SemStreams HTTP directory.
	BackendHTTP = "http"
	// BackendAgntcyGRPC is the AGNTCY agntcy/dir StoreService over gRPC.
	BackendAgntcyGRPC = "agntcy_grpc"
)

// AgntcyGRPCConfig carries settings for the agntcy_grpc backend.
type AgntcyGRPCConfig struct {
	// Endpoint is the host:port the gRPC client dials.
	// Example: "prod.api.ads.outshift.io:443".
	Endpoint string `json:"endpoint"`

	// TLS controls whether the client establishes TLS on dial. The
	// hosted hub requires TLS (true); local dev / bufconn tests use
	// false. When false the client uses insecure transport credentials
	// (grpc.WithTransportCredentials(insecure.NewCredentials())) —
	// suitable only for trusted networks and local development.
	TLS bool `json:"tls"`

	// Auth carries optional per-RPC OIDC authentication. nil or
	// Type=="none" disables auth (suitable for local dev / private
	// deployments). The hosted hub generally requires Type=="oidc".
	Auth *AuthConfig `json:"auth,omitempty"`
}

// AuthConfig configures per-RPC authentication for the agntcy_grpc
// backend. The only auth flow supported today is OIDC client
// credentials (the deployment pattern AGNTCY's hosted hub uses).
type AuthConfig struct {
	// Type selects the auth flow. One of "none" or "oidc". Empty
	// defaults to "none".
	Type string `json:"type"`

	// Issuer is the OIDC token endpoint URL (the `token_endpoint` from
	// the issuer's OIDC discovery document). Required when Type=="oidc".
	Issuer string `json:"issuer,omitempty"`

	// ClientID is the OIDC client identifier. Prefer ClientIDEnv for
	// secret management hygiene. Required when Type=="oidc" if
	// ClientIDEnv is not set.
	ClientID string `json:"client_id,omitempty"`

	// ClientIDEnv names an environment variable to read the OIDC
	// client_id from at runtime. Wins over ClientID when both set.
	ClientIDEnv string `json:"client_id_env,omitempty"`

	// ClientSecretEnv names an environment variable to read the OIDC
	// client_secret from at runtime. Required when Type=="oidc".
	// No inline client_secret field — secrets do not live in JSON
	// config on disk; this is intentional.
	ClientSecretEnv string `json:"client_secret_env,omitempty"`

	// Scopes is the OIDC scope list requested at token-endpoint time.
	Scopes []string `json:"scopes,omitempty"`
}

// DefaultConfig returns the default configuration.
func DefaultConfig() Config {
	return Config{
		Ports: &component.PortConfig{
			Inputs: []component.PortDefinition{
				{
					Name:        "oasf_records",
					Subject:     "oasf.record.generated.>",
					Type:        "kv-watch",
					Required:    true,
					Description: "Watch for generated OASF records",
				},
			},
			Outputs: []component.PortDefinition{
				{
					Name:        "registration_events",
					Subject:     "directory.registration.*",
					Type:        "jetstream",
					Required:    false,
					Description: "Registration events",
				},
			},
		},
		HeartbeatInterval: "30s",
		RegistrationTTL:   "5m",
		IdentityProvider:  "local",
		OASFKVBucket:      "OASF_RECORDS",
		RetryCount:        3,
		RetryDelay:        "1s",
	}
}

// Validate validates the configuration.
func (c *Config) Validate() error {
	if c.Ports == nil {
		return fmt.Errorf("ports configuration is required")
	}

	// DirectoryURL empty is allowed for local development/testing

	if c.OASFKVBucket == "" {
		return fmt.Errorf("oasf_kv_bucket is required")
	}

	// Backend selector — empty is valid (defaults to http).
	switch c.Backend {
	case "", BackendHTTP:
		// OK; agntcy_grpc block ignored on this path.
	case BackendAgntcyGRPC:
		if c.AgntcyGRPC == nil {
			return fmt.Errorf("backend=%q requires agntcy_grpc block", BackendAgntcyGRPC)
		}
		if err := c.AgntcyGRPC.Validate(); err != nil {
			return fmt.Errorf("agntcy_grpc: %w", err)
		}
	default:
		return fmt.Errorf("backend must be %q or %q, got %q", BackendHTTP, BackendAgntcyGRPC, c.Backend)
	}

	// Validate heartbeat interval if set
	if c.HeartbeatInterval != "" {
		if _, err := time.ParseDuration(c.HeartbeatInterval); err != nil {
			return fmt.Errorf("invalid heartbeat_interval: %w", err)
		}
	}

	// Validate registration TTL if set
	if c.RegistrationTTL != "" {
		if _, err := time.ParseDuration(c.RegistrationTTL); err != nil {
			return fmt.Errorf("invalid registration_ttl: %w", err)
		}
	}

	// Validate retry delay if set
	if c.RetryDelay != "" {
		if _, err := time.ParseDuration(c.RetryDelay); err != nil {
			return fmt.Errorf("invalid retry_delay: %w", err)
		}
	}

	return nil
}

// Validate validates an AgntcyGRPCConfig.
func (g *AgntcyGRPCConfig) Validate() error {
	if g.Endpoint == "" {
		return fmt.Errorf("endpoint is required")
	}
	if g.Auth != nil {
		if err := g.Auth.Validate(); err != nil {
			return fmt.Errorf("auth: %w", err)
		}
	}
	return nil
}

// Validate validates an AuthConfig.
func (a *AuthConfig) Validate() error {
	switch a.Type {
	case "", "none":
		return nil
	case "oidc":
		if a.Issuer == "" {
			return fmt.Errorf("issuer is required for oidc auth")
		}
		if a.ClientID == "" && a.ClientIDEnv == "" {
			return fmt.Errorf("either client_id or client_id_env is required for oidc auth")
		}
		if a.ClientSecretEnv == "" {
			return fmt.Errorf("client_secret_env is required for oidc auth (secrets must not live in config)")
		}
		return nil
	default:
		return fmt.Errorf("auth.type must be %q or %q, got %q", "none", "oidc", a.Type)
	}
}

// GetHeartbeatInterval returns the heartbeat interval.
func (c *Config) GetHeartbeatInterval() time.Duration {
	if c.HeartbeatInterval == "" {
		return 30 * time.Second
	}
	d, err := time.ParseDuration(c.HeartbeatInterval)
	if err != nil {
		return 30 * time.Second
	}
	return d
}

// GetRegistrationTTL returns the registration TTL.
func (c *Config) GetRegistrationTTL() time.Duration {
	if c.RegistrationTTL == "" {
		return 5 * time.Minute
	}
	d, err := time.ParseDuration(c.RegistrationTTL)
	if err != nil {
		return 5 * time.Minute
	}
	return d
}

// GetRetryDelay returns the retry delay.
func (c *Config) GetRetryDelay() time.Duration {
	if c.RetryDelay == "" {
		return time.Second
	}
	d, err := time.ParseDuration(c.RetryDelay)
	if err != nil {
		return time.Second
	}
	return d
}

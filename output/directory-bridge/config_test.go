package directorybridge

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/c360studio/semstreams/component"
)

func TestDefaultConfig(t *testing.T) {
	cfg := DefaultConfig()

	if cfg.Ports == nil {
		t.Error("expected Ports to be set")
	}
	if len(cfg.Ports.Inputs) != 1 {
		t.Errorf("expected 1 input port, got %d", len(cfg.Ports.Inputs))
	}
	if len(cfg.Ports.Outputs) != 1 {
		t.Errorf("expected 1 output port, got %d", len(cfg.Ports.Outputs))
	}
	if cfg.HeartbeatInterval != "30s" {
		t.Errorf("expected heartbeat interval '30s', got %s", cfg.HeartbeatInterval)
	}
	if cfg.RegistrationTTL != "5m" {
		t.Errorf("expected registration TTL '5m', got %s", cfg.RegistrationTTL)
	}
	if cfg.IdentityProvider != "local" {
		t.Errorf("expected identity provider 'local', got %s", cfg.IdentityProvider)
	}
	if cfg.OASFKVBucket != "OASF_RECORDS" {
		t.Errorf("expected OASF KV bucket 'OASF_RECORDS', got %s", cfg.OASFKVBucket)
	}
	if cfg.RetryCount != 3 {
		t.Errorf("expected retry count 3, got %d", cfg.RetryCount)
	}
}

func TestConfigValidate(t *testing.T) {
	tests := []struct {
		name    string
		config  Config
		wantErr bool
		errMsg  string
	}{
		{
			name:    "valid default config",
			config:  DefaultConfig(),
			wantErr: false,
		},
		{
			name: "missing ports",
			config: Config{
				OASFKVBucket: "OASF_RECORDS",
			},
			wantErr: true,
			errMsg:  "ports configuration is required",
		},
		{
			name: "missing oasf kv bucket",
			config: Config{
				Ports:        DefaultConfig().Ports,
				OASFKVBucket: "",
			},
			wantErr: true,
			errMsg:  "oasf_kv_bucket is required",
		},
		{
			name: "invalid heartbeat interval",
			config: Config{
				Ports:             DefaultConfig().Ports,
				OASFKVBucket:      "OASF_RECORDS",
				HeartbeatInterval: "invalid",
			},
			wantErr: true,
			errMsg:  "invalid heartbeat_interval",
		},
		{
			name: "invalid registration TTL",
			config: Config{
				Ports:           DefaultConfig().Ports,
				OASFKVBucket:    "OASF_RECORDS",
				RegistrationTTL: "invalid",
			},
			wantErr: true,
			errMsg:  "invalid registration_ttl",
		},
		{
			name: "invalid retry delay",
			config: Config{
				Ports:        DefaultConfig().Ports,
				OASFKVBucket: "OASF_RECORDS",
				RetryDelay:   "invalid",
			},
			wantErr: true,
			errMsg:  "invalid retry_delay",
		},
		{
			name: "empty directory URL allowed",
			config: Config{
				Ports:        DefaultConfig().Ports,
				OASFKVBucket: "OASF_RECORDS",
				DirectoryURL: "",
			},
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.config.Validate()
			if tt.wantErr {
				if err == nil {
					t.Error("expected error, got nil")
				} else if tt.errMsg != "" && err.Error() != tt.errMsg && !strings.Contains(err.Error(), tt.errMsg) {
					t.Errorf("expected error containing %q, got %q", tt.errMsg, err.Error())
				}
			} else if err != nil {
				t.Errorf("unexpected error: %v", err)
			}
		})
	}
}

func TestConfigGetHeartbeatInterval(t *testing.T) {
	tests := []struct {
		name     string
		interval string
		want     time.Duration
	}{
		{
			name:     "valid interval",
			interval: "30s",
			want:     30 * time.Second,
		},
		{
			name:     "empty uses default",
			interval: "",
			want:     30 * time.Second,
		},
		{
			name:     "invalid uses default",
			interval: "invalid",
			want:     30 * time.Second,
		},
		{
			name:     "minutes",
			interval: "2m",
			want:     2 * time.Minute,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := Config{HeartbeatInterval: tt.interval}
			got := cfg.GetHeartbeatInterval()
			if got != tt.want {
				t.Errorf("GetHeartbeatInterval() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestConfigGetRegistrationTTL(t *testing.T) {
	tests := []struct {
		name string
		ttl  string
		want time.Duration
	}{
		{
			name: "valid TTL",
			ttl:  "5m",
			want: 5 * time.Minute,
		},
		{
			name: "empty uses default",
			ttl:  "",
			want: 5 * time.Minute,
		},
		{
			name: "invalid uses default",
			ttl:  "invalid",
			want: 5 * time.Minute,
		},
		{
			name: "hours",
			ttl:  "1h",
			want: time.Hour,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := Config{RegistrationTTL: tt.ttl}
			got := cfg.GetRegistrationTTL()
			if got != tt.want {
				t.Errorf("GetRegistrationTTL() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestConfigGetRetryDelay(t *testing.T) {
	tests := []struct {
		name  string
		delay string
		want  time.Duration
	}{
		{
			name:  "valid delay",
			delay: "1s",
			want:  time.Second,
		},
		{
			name:  "empty uses default",
			delay: "",
			want:  time.Second,
		},
		{
			name:  "invalid uses default",
			delay: "invalid",
			want:  time.Second,
		},
		{
			name:  "milliseconds",
			delay: "500ms",
			want:  500 * time.Millisecond,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := Config{RetryDelay: tt.delay}
			got := cfg.GetRetryDelay()
			if got != tt.want {
				t.Errorf("GetRetryDelay() = %v, want %v", got, tt.want)
			}
		})
	}
}

// TestConfig_JSONRoundTrip covers the polymorphic-config discipline:
// every operator-reachable field (including the new Backend selector,
// the AgntcyGRPC block, and its nested AuthConfig) must survive a
// JSON marshal/unmarshal round-trip without silent dropping. See the
// feedback memory feedback_polymorphic_config_needs_json_roundtrip_test.
func TestConfig_JSONRoundTrip(t *testing.T) {
	original := Config{
		Ports: &component.PortConfig{
			Inputs:  []component.PortDefinition{{Name: "i", Subject: "a", Type: "kv-watch"}},
			Outputs: []component.PortDefinition{{Name: "o", Subject: "b", Type: "jetstream"}},
		},
		Backend:      BackendAgntcyGRPC,
		DirectoryURL: "http://legacy-http",
		AgntcyGRPC: &AgntcyGRPCConfig{
			Endpoint: "prod.api.ads.outshift.io:443",
			TLS:      true,
			Auth: &AuthConfig{
				Type:            "oidc",
				Issuer:          "https://issuer.example.com/token",
				ClientID:        "inline-client",
				ClientIDEnv:     "EXAMPLE_CLIENT_ID",
				ClientSecretEnv: "EXAMPLE_CLIENT_SECRET",
				Scopes:          []string{"agntcy.publish", "agntcy.read"},
			},
		},
		HeartbeatInterval:    "30s",
		RegistrationTTL:      "5m",
		IdentityProvider:     "local",
		OASFKVBucket:         "OASF_RECORDS",
		RetryCount:           3,
		RetryDelay:           "1s",
		ConsumerNameSuffix:   "test-suffix",
		DeleteConsumerOnStop: true,
	}

	raw, err := json.Marshal(original)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}

	var decoded Config
	if err := json.Unmarshal(raw, &decoded); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}

	// Spot-check every field that wasn't already in the existing
	// TestConfigValidate / TestDefaultConfig coverage. Catches the
	// "shadow struct" / "field silently dropped" failure mode the
	// polymorphic-config rule exists to prevent.
	if decoded.Backend != original.Backend {
		t.Errorf("Backend = %q, want %q", decoded.Backend, original.Backend)
	}
	if decoded.DirectoryURL != original.DirectoryURL {
		t.Errorf("DirectoryURL = %q, want %q", decoded.DirectoryURL, original.DirectoryURL)
	}
	if decoded.AgntcyGRPC == nil {
		t.Fatal("AgntcyGRPC dropped during round-trip")
	}
	if decoded.AgntcyGRPC.Endpoint != original.AgntcyGRPC.Endpoint {
		t.Errorf("AgntcyGRPC.Endpoint = %q, want %q", decoded.AgntcyGRPC.Endpoint, original.AgntcyGRPC.Endpoint)
	}
	if decoded.AgntcyGRPC.TLS != original.AgntcyGRPC.TLS {
		t.Errorf("AgntcyGRPC.TLS = %v, want %v", decoded.AgntcyGRPC.TLS, original.AgntcyGRPC.TLS)
	}
	if decoded.AgntcyGRPC.Auth == nil {
		t.Fatal("AgntcyGRPC.Auth dropped during round-trip")
	}
	if decoded.AgntcyGRPC.Auth.Type != original.AgntcyGRPC.Auth.Type {
		t.Errorf("Auth.Type = %q, want %q", decoded.AgntcyGRPC.Auth.Type, original.AgntcyGRPC.Auth.Type)
	}
	if decoded.AgntcyGRPC.Auth.Issuer != original.AgntcyGRPC.Auth.Issuer {
		t.Errorf("Auth.Issuer = %q, want %q", decoded.AgntcyGRPC.Auth.Issuer, original.AgntcyGRPC.Auth.Issuer)
	}
	if decoded.AgntcyGRPC.Auth.ClientID != original.AgntcyGRPC.Auth.ClientID {
		t.Errorf("Auth.ClientID = %q, want %q", decoded.AgntcyGRPC.Auth.ClientID, original.AgntcyGRPC.Auth.ClientID)
	}
	if decoded.AgntcyGRPC.Auth.ClientIDEnv != original.AgntcyGRPC.Auth.ClientIDEnv {
		t.Errorf("Auth.ClientIDEnv = %q, want %q", decoded.AgntcyGRPC.Auth.ClientIDEnv, original.AgntcyGRPC.Auth.ClientIDEnv)
	}
	if decoded.AgntcyGRPC.Auth.ClientSecretEnv != original.AgntcyGRPC.Auth.ClientSecretEnv {
		t.Errorf("Auth.ClientSecretEnv = %q, want %q", decoded.AgntcyGRPC.Auth.ClientSecretEnv, original.AgntcyGRPC.Auth.ClientSecretEnv)
	}
	if len(decoded.AgntcyGRPC.Auth.Scopes) != len(original.AgntcyGRPC.Auth.Scopes) {
		t.Errorf("Auth.Scopes len = %d, want %d", len(decoded.AgntcyGRPC.Auth.Scopes), len(original.AgntcyGRPC.Auth.Scopes))
	}
	// Element-wise compare — length parity alone wouldn't catch a slice
	// that round-tripped with swapped/dropped/renamed entries.
	for i, want := range original.AgntcyGRPC.Auth.Scopes {
		if i >= len(decoded.AgntcyGRPC.Auth.Scopes) {
			break
		}
		if got := decoded.AgntcyGRPC.Auth.Scopes[i]; got != want {
			t.Errorf("Auth.Scopes[%d] = %q, want %q", i, got, want)
		}
	}

	// Confirm the decoded config validates (covers the V (validate) leg
	// of the round-trip-and-validate contract).
	if err := decoded.Validate(); err != nil {
		t.Errorf("decoded config failed validate: %v", err)
	}
}

// TestConfigValidate_BackendVariants exercises the new selector and
// per-backend validation branches.
func TestConfigValidate_BackendVariants(t *testing.T) {
	base := DefaultConfig()
	cases := []struct {
		name    string
		mutate  func(*Config)
		wantErr string
	}{
		{
			name:   "empty backend defaults to http",
			mutate: func(c *Config) { c.Backend = "" },
		},
		{
			name:   "explicit http",
			mutate: func(c *Config) { c.Backend = BackendHTTP },
		},
		{
			name: "agntcy_grpc requires block",
			mutate: func(c *Config) {
				c.Backend = BackendAgntcyGRPC
				c.AgntcyGRPC = nil
			},
			wantErr: "requires agntcy_grpc block",
		},
		{
			name: "agntcy_grpc requires endpoint",
			mutate: func(c *Config) {
				c.Backend = BackendAgntcyGRPC
				c.AgntcyGRPC = &AgntcyGRPCConfig{Endpoint: ""}
			},
			wantErr: "endpoint is required",
		},
		{
			name: "agntcy_grpc with none auth ok",
			mutate: func(c *Config) {
				c.Backend = BackendAgntcyGRPC
				c.AgntcyGRPC = &AgntcyGRPCConfig{Endpoint: "h:1", Auth: &AuthConfig{Type: "none"}}
			},
		},
		{
			name: "oidc auth requires issuer",
			mutate: func(c *Config) {
				c.Backend = BackendAgntcyGRPC
				c.AgntcyGRPC = &AgntcyGRPCConfig{
					Endpoint: "h:1",
					Auth:     &AuthConfig{Type: "oidc", ClientID: "x", ClientSecretEnv: "S"},
				}
			},
			wantErr: "issuer is required",
		},
		{
			name: "oidc auth requires client_id",
			mutate: func(c *Config) {
				c.Backend = BackendAgntcyGRPC
				c.AgntcyGRPC = &AgntcyGRPCConfig{
					Endpoint: "h:1",
					Auth:     &AuthConfig{Type: "oidc", Issuer: "iss", ClientSecretEnv: "S"},
				}
			},
			wantErr: "client_id or client_id_env",
		},
		{
			name: "oidc auth requires client_secret_env",
			mutate: func(c *Config) {
				c.Backend = BackendAgntcyGRPC
				c.AgntcyGRPC = &AgntcyGRPCConfig{
					Endpoint: "h:1",
					Auth:     &AuthConfig{Type: "oidc", Issuer: "iss", ClientID: "x"},
				}
			},
			wantErr: "client_secret_env",
		},
		{
			name:    "unknown backend",
			mutate:  func(c *Config) { c.Backend = "nonsense" },
			wantErr: "backend must be",
		},
		{
			name: "unknown auth type",
			mutate: func(c *Config) {
				c.Backend = BackendAgntcyGRPC
				c.AgntcyGRPC = &AgntcyGRPCConfig{Endpoint: "h:1", Auth: &AuthConfig{Type: "kerberos"}}
			},
			wantErr: "auth.type must be",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			cfg := base
			tc.mutate(&cfg)
			err := cfg.Validate()
			if tc.wantErr == "" {
				if err != nil {
					t.Errorf("Validate() error = %v, want nil", err)
				}
				return
			}
			if err == nil {
				t.Fatalf("Validate() = nil, want error containing %q", tc.wantErr)
			}
			if !strings.Contains(err.Error(), tc.wantErr) {
				t.Errorf("Validate() error = %q, want substring %q", err.Error(), tc.wantErr)
			}
		})
	}
}

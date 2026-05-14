package agentic

import (
	"testing"
)

func TestBuildHubConfigFromEnv_DefaultsAndRequiredFields(t *testing.T) {
	cases := []struct {
		name    string
		env     map[string]string
		wantErr string
	}{
		{
			name: "missing issuer",
			env: map[string]string{
				"AGNTCY_HUB_AUTH":          "1",
				"AGNTCY_HUB_CLIENT_ID":     "ci-client",
				"AGNTCY_HUB_CLIENT_SECRET": "s",
			},
			wantErr: "OIDC_ISSUER",
		},
		{
			name: "missing client id",
			env: map[string]string{
				"AGNTCY_HUB_AUTH":          "1",
				"AGNTCY_HUB_OIDC_ISSUER":   "https://idp/token",
				"AGNTCY_HUB_CLIENT_SECRET": "s",
			},
			wantErr: "CLIENT_ID",
		},
		{
			name: "missing client secret",
			env: map[string]string{
				"AGNTCY_HUB_AUTH":        "1",
				"AGNTCY_HUB_OIDC_ISSUER": "https://idp/token",
				"AGNTCY_HUB_CLIENT_ID":   "ci-client",
			},
			wantErr: "CLIENT_SECRET",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			// Clear potential leakage from the host env first; t.Setenv
			// only sets the named keys.
			t.Setenv("AGNTCY_HUB_AUTH", "")
			t.Setenv("AGNTCY_HUB_OIDC_ISSUER", "")
			t.Setenv("AGNTCY_HUB_CLIENT_ID", "")
			t.Setenv("AGNTCY_HUB_CLIENT_SECRET", "")
			t.Setenv("AGNTCY_HUB_ENDPOINT", "")
			t.Setenv("AGNTCY_HUB_TLS", "")
			t.Setenv("AGNTCY_HUB_SCOPES", "")
			for k, v := range tc.env {
				t.Setenv(k, v)
			}

			_, err := buildHubConfigFromEnv()
			if err == nil {
				t.Fatalf("expected error containing %q, got nil", tc.wantErr)
			}
			if !contains(err.Error(), tc.wantErr) {
				t.Errorf("error = %q, want substring %q", err.Error(), tc.wantErr)
			}
		})
	}
}

func TestBuildHubConfigFromEnv_AllFieldsPopulated(t *testing.T) {
	t.Setenv("AGNTCY_HUB_AUTH", "1")
	t.Setenv("AGNTCY_HUB_ENDPOINT", "custom.example.com:9443")
	t.Setenv("AGNTCY_HUB_TLS", "true")
	t.Setenv("AGNTCY_HUB_OIDC_ISSUER", "https://idp.example.com/token")
	t.Setenv("AGNTCY_HUB_CLIENT_ID", "ci-client")
	t.Setenv("AGNTCY_HUB_CLIENT_SECRET", "the-secret")
	t.Setenv("AGNTCY_HUB_SCOPES", "agntcy.publish, agntcy.read")

	cfg, err := buildHubConfigFromEnv()
	if err != nil {
		t.Fatalf("buildHubConfigFromEnv: %v", err)
	}
	if cfg.Endpoint != "custom.example.com:9443" {
		t.Errorf("Endpoint = %q, want override", cfg.Endpoint)
	}
	if !cfg.TLS {
		t.Error("TLS = false, want true (env=\"true\")")
	}
	if cfg.Auth == nil {
		t.Fatal("Auth is nil")
	}
	if cfg.Auth.Type != "oidc" {
		t.Errorf("Auth.Type = %q, want \"oidc\"", cfg.Auth.Type)
	}
	if cfg.Auth.Issuer != "https://idp.example.com/token" {
		t.Errorf("Auth.Issuer = %q", cfg.Auth.Issuer)
	}
	if cfg.Auth.ClientID != "ci-client" {
		t.Errorf("Auth.ClientID = %q", cfg.Auth.ClientID)
	}
	if cfg.Auth.ClientSecretEnv != "AGNTCY_HUB_CLIENT_SECRET" {
		t.Errorf("Auth.ClientSecretEnv = %q, want fixed name", cfg.Auth.ClientSecretEnv)
	}
	if len(cfg.Auth.Scopes) != 2 || cfg.Auth.Scopes[0] != "agntcy.publish" || cfg.Auth.Scopes[1] != "agntcy.read" {
		t.Errorf("Auth.Scopes = %v, want [agntcy.publish agntcy.read]", cfg.Auth.Scopes)
	}
}

func TestBuildHubConfigFromEnv_DefaultEndpoint(t *testing.T) {
	t.Setenv("AGNTCY_HUB_AUTH", "1")
	t.Setenv("AGNTCY_HUB_ENDPOINT", "") // empty triggers default
	t.Setenv("AGNTCY_HUB_OIDC_ISSUER", "https://idp/token")
	t.Setenv("AGNTCY_HUB_CLIENT_ID", "c")
	t.Setenv("AGNTCY_HUB_CLIENT_SECRET", "s")

	cfg, err := buildHubConfigFromEnv()
	if err != nil {
		t.Fatalf("buildHubConfigFromEnv: %v", err)
	}
	if cfg.Endpoint != "prod.api.ads.outshift.io:443" {
		t.Errorf("default endpoint = %q, want prod.api.ads.outshift.io:443", cfg.Endpoint)
	}
}

func TestBuildHubConfigFromEnv_TLSFalse(t *testing.T) {
	t.Setenv("AGNTCY_HUB_AUTH", "1")
	t.Setenv("AGNTCY_HUB_TLS", "false")
	t.Setenv("AGNTCY_HUB_OIDC_ISSUER", "https://idp/token")
	t.Setenv("AGNTCY_HUB_CLIENT_ID", "c")
	t.Setenv("AGNTCY_HUB_CLIENT_SECRET", "s")

	cfg, err := buildHubConfigFromEnv()
	if err != nil {
		t.Fatalf("buildHubConfigFromEnv: %v", err)
	}
	if cfg.TLS {
		t.Error("TLS = true, want false (env=\"false\")")
	}
}

func TestSplitCSV(t *testing.T) {
	cases := []struct {
		in   string
		want []string
	}{
		{"", nil},
		{"a", []string{"a"}},
		{"a,b,c", []string{"a", "b", "c"}},
		{"a, b , c", []string{"a", "b", "c"}},
		{"a,,b", []string{"a", "b"}}, // empty entries dropped
		{",a,", []string{"a"}},
	}
	for _, tc := range cases {
		t.Run(tc.in, func(t *testing.T) {
			got := splitCSV(tc.in)
			if len(got) != len(tc.want) {
				t.Fatalf("splitCSV(%q) len = %d, want %d (got %v)", tc.in, len(got), len(tc.want), got)
			}
			for i, w := range tc.want {
				if got[i] != w {
					t.Errorf("splitCSV(%q)[%d] = %q, want %q", tc.in, i, got[i], w)
				}
			}
		})
	}
}

// contains is a tiny substring helper to keep test imports minimal.
func contains(haystack, needle string) bool {
	for i := 0; i+len(needle) <= len(haystack); i++ {
		if haystack[i:i+len(needle)] == needle {
			return true
		}
	}
	return false
}

package directorybridge

import (
	"testing"
)

func TestNewAuthProvider_NilConfig(t *testing.T) {
	p, err := NewAuthProvider(nil)
	if err != nil {
		t.Fatalf("NewAuthProvider(nil): %v", err)
	}
	if _, ok := p.(NoOpAuthProvider); !ok {
		t.Errorf("got %T, want NoOpAuthProvider for nil config", p)
	}
}

func TestNewAuthProvider_None(t *testing.T) {
	for _, typ := range []string{"", "none"} {
		t.Run("type="+typ, func(t *testing.T) {
			p, err := NewAuthProvider(&AuthConfig{Type: typ})
			if err != nil {
				t.Fatalf("NewAuthProvider(%q): %v", typ, err)
			}
			if _, ok := p.(NoOpAuthProvider); !ok {
				t.Errorf("got %T, want NoOpAuthProvider", p)
			}
			if perRPC := p.PerRPC(); perRPC != nil {
				t.Errorf("NoOpAuthProvider.PerRPC() = %v, want nil", perRPC)
			}
			if err := p.Close(); err != nil {
				t.Errorf("Close: %v", err)
			}
		})
	}
}

func TestNewAuthProvider_OIDC_MissingClientID(t *testing.T) {
	// Neither inline client_id nor env-var set — must error at construction.
	_, err := NewAuthProvider(&AuthConfig{
		Type:            "oidc",
		Issuer:          "https://issuer.example.com/token",
		ClientIDEnv:     "PR_C_UNSET_CLIENT_ID_VAR",
		ClientSecretEnv: "PR_C_UNSET_CLIENT_SECRET_VAR",
	})
	if err == nil {
		t.Fatal("expected error for missing client_id")
	}
}

func TestNewAuthProvider_OIDC_MissingClientSecret(t *testing.T) {
	// ClientID inline is fine; secret env var is unset → construction error.
	t.Setenv("PR_C_UNSET_CLIENT_SECRET_VAR", "") // ensure not set
	_, err := NewAuthProvider(&AuthConfig{
		Type:            "oidc",
		Issuer:          "https://issuer.example.com/token",
		ClientID:        "test-client",
		ClientSecretEnv: "PR_C_UNSET_CLIENT_SECRET_VAR",
	})
	if err == nil {
		t.Fatal("expected error for missing client_secret env var")
	}
}

func TestNewAuthProvider_OIDC_Success(t *testing.T) {
	t.Setenv("PR_C_OIDC_TEST_CLIENT_SECRET", "the-secret")
	t.Setenv("PR_C_OIDC_TEST_CLIENT_ID", "env-client")

	p, err := NewAuthProvider(&AuthConfig{
		Type:            "oidc",
		Issuer:          "https://issuer.example.com/token",
		ClientIDEnv:     "PR_C_OIDC_TEST_CLIENT_ID",
		ClientSecretEnv: "PR_C_OIDC_TEST_CLIENT_SECRET",
		Scopes:          []string{"a", "b"},
	})
	if err != nil {
		t.Fatalf("NewAuthProvider: %v", err)
	}
	if _, ok := p.(*OIDCAuthProvider); !ok {
		t.Errorf("got %T, want *OIDCAuthProvider", p)
	}
	if perRPC := p.PerRPC(); perRPC == nil {
		t.Error("OIDCAuthProvider.PerRPC() = nil, want PerRPCCredentials")
	} else if !perRPC.RequireTransportSecurity() {
		t.Error("OIDC PerRPC must require transport security (bearer tokens over TLS)")
	}
	if err := p.Close(); err != nil {
		t.Errorf("Close: %v", err)
	}
}

func TestNewAuthProvider_UnsupportedType(t *testing.T) {
	_, err := NewAuthProvider(&AuthConfig{Type: "kerberos"})
	if err == nil {
		t.Fatal("expected error for unsupported auth type")
	}
}

func TestBuildGRPCDialOptions_InsecureNoAuth(t *testing.T) {
	opts := buildGRPCDialOptions(&AgntcyGRPCConfig{Endpoint: "h:1", TLS: false}, NoOpAuthProvider{})
	if len(opts) != 1 {
		t.Errorf("len(opts) = %d, want 1 (just insecure transport)", len(opts))
	}
}

func TestBuildGRPCDialOptions_TLSWithAuth(t *testing.T) {
	t.Setenv("PR_C_DIAL_OPTS_SECRET", "s")
	auth, err := NewAuthProvider(&AuthConfig{
		Type:            "oidc",
		Issuer:          "https://issuer.example.com/token",
		ClientID:        "c",
		ClientSecretEnv: "PR_C_DIAL_OPTS_SECRET",
	})
	if err != nil {
		t.Fatalf("NewAuthProvider: %v", err)
	}
	defer auth.Close()

	opts := buildGRPCDialOptions(&AgntcyGRPCConfig{Endpoint: "h:1", TLS: true}, auth)
	if len(opts) != 2 {
		t.Errorf("len(opts) = %d, want 2 (TLS transport + PerRPC OIDC)", len(opts))
	}
}

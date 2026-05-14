package directorybridge

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
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

// TestNewAuthProvider_OIDC_Success exercises the full token-fetch path
// (not just construction) against an httptest.Server that mints a fake
// access token. Construction alone, as the prior version of this test
// did, doesn't catch a misnamed oauth2 import or a broken adapter —
// only an end-to-end GetRequestMetadata call does.
func TestNewAuthProvider_OIDC_Success(t *testing.T) {
	// Fake issuer that satisfies the OAuth2 client-credentials flow.
	// Asserts the client_id / client_secret made it through, then mints
	// a bearer token the provider should surface in Authorization.
	const wantClientID = "env-client"
	const wantSecret = "the-secret"
	const issuedToken = "fake-access-token"

	issuer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if err := r.ParseForm(); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		// clientcredentials sends client_id/secret either in Basic auth
		// or as form params; check both.
		gotID, gotSecret, hasBasic := r.BasicAuth()
		if !hasBasic {
			gotID = r.Form.Get("client_id")
			gotSecret = r.Form.Get("client_secret")
		}
		if gotID != wantClientID || gotSecret != wantSecret {
			http.Error(w, "bad credentials", http.StatusUnauthorized)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"access_token": issuedToken,
			"token_type":   "Bearer",
			"expires_in":   3600,
		})
	}))
	defer issuer.Close()

	t.Setenv("PR_C_OIDC_TEST_CLIENT_SECRET", wantSecret)
	t.Setenv("PR_C_OIDC_TEST_CLIENT_ID", wantClientID)

	p, err := NewAuthProvider(&AuthConfig{
		Type:            "oidc",
		Issuer:          issuer.URL,
		ClientIDEnv:     "PR_C_OIDC_TEST_CLIENT_ID",
		ClientSecretEnv: "PR_C_OIDC_TEST_CLIENT_SECRET",
		Scopes:          []string{"a", "b"},
	})
	if err != nil {
		t.Fatalf("NewAuthProvider: %v", err)
	}
	defer p.Close()

	if _, ok := p.(*OIDCAuthProvider); !ok {
		t.Errorf("got %T, want *OIDCAuthProvider", p)
	}

	perRPC := p.PerRPC()
	if perRPC == nil {
		t.Fatal("OIDCAuthProvider.PerRPC() = nil, want PerRPCCredentials")
	}
	if !perRPC.RequireTransportSecurity() {
		t.Error("OIDC PerRPC must require transport security (bearer tokens over TLS)")
	}

	md, err := perRPC.GetRequestMetadata(context.Background())
	if err != nil {
		t.Fatalf("GetRequestMetadata: %v", err)
	}
	authz, ok := md["authorization"]
	if !ok {
		t.Fatalf("metadata missing authorization header: %v", md)
	}
	if want := "Bearer " + issuedToken; authz != want {
		t.Errorf("authorization = %q, want %q", authz, want)
	}
	if !strings.HasPrefix(authz, "Bearer ") {
		t.Errorf("authorization must start with \"Bearer \", got %q", authz)
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

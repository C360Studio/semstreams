package directorybridge

import (
	"context"
	"fmt"
	"os"

	"golang.org/x/oauth2"
	"golang.org/x/oauth2/clientcredentials"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials"
	"google.golang.org/grpc/credentials/insecure"
)

// AuthProvider supplies per-RPC credentials for the agntcy_grpc backend.
// Implementations either source a bearer token (OIDC) or signal that no
// authentication should be attached to outgoing RPCs (NoOp).
//
// The provider lifecycle parallels the Backend's: the bridge constructs
// one provider per Component.Initialize and closes it via Close at
// Component.Stop. Token caching / refresh lives inside the provider —
// callers see only the wrapped credentials.PerRPCCredentials.
type AuthProvider interface {
	// PerRPC returns the per-RPC credentials to attach to gRPC calls, or
	// nil for "no auth". The bridge feeds the result into
	// grpc.WithPerRPCCredentials at dial time; a nil return is the
	// signal to omit that DialOption.
	PerRPC() credentials.PerRPCCredentials

	// Close releases any provider-held resources (token caches, HTTP
	// clients). Providers without resources to release implement this
	// as a no-op.
	Close() error
}

// NewAuthProvider constructs an AuthProvider from the given config.
// A nil config or Type "" / "none" returns the NoOp provider.
// "oidc" reads the client_id and client_secret from the configured env
// vars (or inline field) and returns an OIDC client-credentials
// provider that auto-refreshes tokens.
func NewAuthProvider(cfg *AuthConfig) (AuthProvider, error) {
	if cfg == nil {
		return NoOpAuthProvider{}, nil
	}
	switch cfg.Type {
	case "", "none":
		return NoOpAuthProvider{}, nil
	case "oidc":
		return newOIDCAuthProvider(cfg)
	default:
		return nil, fmt.Errorf("unsupported auth type %q", cfg.Type)
	}
}

// NoOpAuthProvider attaches no credentials. Suitable for local dev,
// bufconn tests, and privately hosted directories that don't gate on
// auth. The hosted hub at prod.api.ads.outshift.io generally does, so
// production deployments swap this for OIDCAuthProvider.
type NoOpAuthProvider struct{}

// PerRPC returns nil — the bridge skips grpc.WithPerRPCCredentials.
func (NoOpAuthProvider) PerRPC() credentials.PerRPCCredentials { return nil }

// Close is a no-op for NoOpAuthProvider.
func (NoOpAuthProvider) Close() error { return nil }

// OIDCAuthProvider sources per-RPC bearer tokens from an OIDC issuer
// via the client-credentials flow. Tokens are cached and auto-refreshed
// by the underlying clientcredentials.Config.TokenSource, so callers
// don't need to manage lifetimes — each PerRPC().GetRequestMetadata
// call returns whatever the cached source hands out.
type OIDCAuthProvider struct {
	source oauth2.TokenSource
}

func newOIDCAuthProvider(cfg *AuthConfig) (*OIDCAuthProvider, error) {
	clientID := cfg.ClientID
	if cfg.ClientIDEnv != "" {
		if v := os.Getenv(cfg.ClientIDEnv); v != "" {
			clientID = v
		}
	}
	if clientID == "" {
		return nil, fmt.Errorf("oidc: client_id is empty (set client_id or %s env)", cfg.ClientIDEnv)
	}

	clientSecret := os.Getenv(cfg.ClientSecretEnv)
	if clientSecret == "" {
		return nil, fmt.Errorf("oidc: client secret not set in env var %q", cfg.ClientSecretEnv)
	}

	ccCfg := &clientcredentials.Config{
		ClientID:     clientID,
		ClientSecret: clientSecret,
		TokenURL:     cfg.Issuer,
		Scopes:       cfg.Scopes,
	}
	return &OIDCAuthProvider{
		source: ccCfg.TokenSource(context.Background()),
	}, nil
}

// PerRPC wraps the OAuth2 token source as gRPC PerRPCCredentials.
func (p *OIDCAuthProvider) PerRPC() credentials.PerRPCCredentials {
	return &oauthTokenSource{src: p.source}
}

// Close is a no-op — clientcredentials.Config.TokenSource uses
// net/http's default transport, which is reclaimed by GC.
func (p *OIDCAuthProvider) Close() error { return nil }

// oauthTokenSource adapts an oauth2.TokenSource to gRPC's
// PerRPCCredentials interface. We hand-roll the adapter (instead of
// importing google.golang.org/grpc/credentials/oauth) to keep the
// dependency surface small — that package pulls in google.golang.org/api
// transitively.
//
// Known limitation: the gRPC PerRPCCredentials context is intentionally
// dropped — oauth2.TokenSource.Token() pre-dates context plumbing and
// doesn't accept one. If a gRPC RPC deadline trips while a token
// refresh is in-flight, the refresh continues on its own HTTP client
// timeout (90s by net/http default). The window is small but nonzero;
// operators sensitive to it should set tight client timeouts on the
// HTTP client passed via oauth2.HTTPClient context value before
// constructing the provider. Acceptable v1 limitation per ADR-042.
type oauthTokenSource struct {
	src oauth2.TokenSource
}

// GetRequestMetadata returns the Authorization header for one RPC.
// See the type comment for context-handling caveats.
func (o *oauthTokenSource) GetRequestMetadata(_ context.Context, _ ...string) (map[string]string, error) {
	tok, err := o.src.Token()
	if err != nil {
		return nil, fmt.Errorf("fetch OIDC token: %w", err)
	}
	return map[string]string{
		"authorization": tok.Type() + " " + tok.AccessToken,
	}, nil
}

// RequireTransportSecurity reports whether the credentials require
// transport security. Bearer tokens must travel over TLS, so we return
// true — gRPC refuses to attach these credentials over an insecure
// connection unless the caller explicitly overrides. For local dev with
// bufconn, use NoOpAuthProvider instead.
func (o *oauthTokenSource) RequireTransportSecurity() bool { return true }

// buildGRPCDialOptions assembles the grpc.DialOption set for the
// agntcy_grpc backend. TLS is chosen via AgntcyGRPCConfig.TLS (real TLS
// for production hubs, insecure for local dev / bufconn). Per-RPC OIDC
// credentials are attached when the auth provider supplies them; the
// NoOp provider returns nil from PerRPC and we skip the
// WithPerRPCCredentials option entirely.
func buildGRPCDialOptions(cfg *AgntcyGRPCConfig, auth AuthProvider) []grpc.DialOption {
	opts := make([]grpc.DialOption, 0, 2)

	if cfg.TLS {
		// Default OS cert pool + ALPN; sufficient for the hosted hub.
		// Operators needing custom roots or mTLS can extend this when
		// the use case actually shows up.
		opts = append(opts, grpc.WithTransportCredentials(credentials.NewTLS(nil)))
	} else {
		opts = append(opts, grpc.WithTransportCredentials(insecure.NewCredentials()))
	}

	if perRPC := auth.PerRPC(); perRPC != nil {
		opts = append(opts, grpc.WithPerRPCCredentials(perRPC))
	}

	return opts
}

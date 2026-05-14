package agentic

import (
	"context"
	"fmt"
	"os"
	"time"

	directorybridge "github.com/c360studio/semstreams/output/directory-bridge"
	oasfgenerator "github.com/c360studio/semstreams/processor/oasf-generator"
	"github.com/c360studio/semstreams/test/e2e/scenarios"
	"github.com/c360studio/semstreams/vocabulary/oasf"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials"
	"google.golang.org/grpc/credentials/insecure"
)

// buildHubDialOptions assembles transport and per-RPC credentials for
// the hub publish stage. Mirrors directory-bridge.buildGRPCDialOptions
// (which is unexported) without taking a dependency on internals — the
// stage runs outside the bridge's Component shell because it doesn't
// need the KV-watch / heartbeat machinery, just the bare Backend.
func buildHubDialOptions(cfg *directorybridge.AgntcyGRPCConfig, auth directorybridge.AuthProvider) []grpc.DialOption {
	opts := make([]grpc.DialOption, 0, 2)
	if cfg.TLS {
		opts = append(opts, grpc.WithTransportCredentials(credentials.NewTLS(nil)))
	} else {
		opts = append(opts, grpc.WithTransportCredentials(insecure.NewCredentials()))
	}
	if perRPC := auth.PerRPC(); perRPC != nil {
		opts = append(opts, grpc.WithPerRPCCredentials(perRPC))
	}
	return opts
}

// verifyAGNTCYHubPublish is the opt-in PR-D stage from ADR-042. It
// validates the agntcy_grpc backend wire path end-to-end against the
// AGNTCY hosted hub (or any AGNTCY-conformant directory the operator
// points at via env vars) by publishing a small OASF record, asserting
// a CID is returned, then withdrawing the record for cleanup.
//
// Skips silently when AGNTCY_HUB_AUTH is unset — the default CI
// behaviour. Opt in by setting:
//
//	AGNTCY_HUB_AUTH=1
//	AGNTCY_HUB_ENDPOINT="prod.api.ads.outshift.io:443"   # optional override
//	AGNTCY_HUB_OIDC_ISSUER="https://idp.example.com/oauth2/token"
//	AGNTCY_HUB_CLIENT_ID="..."                            # or AGNTCY_HUB_CLIENT_ID_VALUE
//	AGNTCY_HUB_CLIENT_SECRET="..."                        # required
//
// Why opt-in: this stage talks to a real directory and would either
// pollute it with CI-named records or fail without credentials. Default
// CI runs the mock-server stages instead (verifyDirectoryBridge etc.).
//
// What this exercises that no other test does: real TLS, real OIDC
// client-credentials flow, real gRPC dial, real Push and Delete
// streams against AGNTCY's typed schema validation.
func (s *Scenario) verifyAGNTCYHubPublish(ctx context.Context, result *scenarios.Result) error {
	if os.Getenv("AGNTCY_HUB_AUTH") == "" {
		result.Details["agntcy_hub_publish_skipped"] = "AGNTCY_HUB_AUTH not set (opt-in stage)"
		return nil
	}

	cfg, err := buildHubConfigFromEnv()
	if err != nil {
		return fmt.Errorf("AGNTCY hub config: %w", err)
	}

	auth, err := directorybridge.NewAuthProvider(cfg.Auth)
	if err != nil {
		return fmt.Errorf("build auth provider: %w", err)
	}

	dialOpts := buildHubDialOptions(cfg, auth)
	backend, err := directorybridge.NewGRPCBackendWithAuth(cfg.Endpoint, auth, dialOpts...)
	if err != nil {
		_ = auth.Close()
		return fmt.Errorf("dial hub %q: %w", cfg.Endpoint, err)
	}
	defer backend.Close()

	record := buildHubTestRecord()
	pubCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()

	pub, err := backend.Publish(pubCtx, &directorybridge.PublishRequest{
		EntityID: record.Extensions["semstreams_entity_id"].(string),
		AgentDID: "did:semstreams:e2e-hub-publish-test",
		Record:   record,
		TTL:      0, // CID-anchored backend — TTL ignored
		Metadata: map[string]any{
			"source":                  "semstreams-e2e",
			"test_run_purpose":        "PR-D hub publish smoke",
			"safe_to_garbage_collect": true,
		},
	})
	if err != nil {
		return fmt.Errorf("publish to hub: %w", err)
	}
	if pub.RecordID == "" {
		return fmt.Errorf("hub returned empty CID")
	}
	result.Details["agntcy_hub_publish_cid"] = pub.RecordID

	// Cleanup: withdraw the test record so we don't leave CI artifacts
	// in the hub's index. Errors here are logged-not-fatal — the
	// publish succeeded, that's the assertion the stage is making.
	withdrawCtx, cancelW := context.WithTimeout(ctx, 30*time.Second)
	defer cancelW()
	if err := backend.Withdraw(withdrawCtx, &directorybridge.WithdrawRequest{
		RecordID: pub.RecordID,
		AgentDID: "did:semstreams:e2e-hub-publish-test",
	}); err != nil {
		result.Warnings = append(result.Warnings,
			fmt.Sprintf("hub withdraw failed (record left in directory): %v", err))
	} else {
		result.Details["agntcy_hub_publish_cleanup"] = "withdrawn"
	}

	result.Details["agntcy_hub_publish_verified"] = true
	return nil
}

// buildHubConfigFromEnv assembles an AgntcyGRPCConfig from the
// AGNTCY_HUB_* env-var set. Returns an error if any required field is
// missing. The full set, with defaults:
//
//	AGNTCY_HUB_ENDPOINT       (default "prod.api.ads.outshift.io:443")
//	AGNTCY_HUB_TLS            (default "true"; set to "false" only for local mocks)
//	AGNTCY_HUB_OIDC_ISSUER    (required when auth is on)
//	AGNTCY_HUB_CLIENT_ID      (inline OIDC client_id)
//	AGNTCY_HUB_CLIENT_SECRET  (the secret itself; the AuthConfig stores
//	                           the env var *name* — for the stage we
//	                           use a fixed name and read it here)
//	AGNTCY_HUB_SCOPES         (comma-separated; optional)
func buildHubConfigFromEnv() (*directorybridge.AgntcyGRPCConfig, error) {
	endpoint := os.Getenv("AGNTCY_HUB_ENDPOINT")
	if endpoint == "" {
		endpoint = "prod.api.ads.outshift.io:443"
	}

	tls := true
	if v := os.Getenv("AGNTCY_HUB_TLS"); v == "false" || v == "0" {
		tls = false
	}

	issuer := os.Getenv("AGNTCY_HUB_OIDC_ISSUER")
	clientID := os.Getenv("AGNTCY_HUB_CLIENT_ID")
	if issuer == "" || clientID == "" {
		return nil, fmt.Errorf("AGNTCY_HUB_OIDC_ISSUER and AGNTCY_HUB_CLIENT_ID required when AGNTCY_HUB_AUTH is set")
	}
	if os.Getenv("AGNTCY_HUB_CLIENT_SECRET") == "" {
		return nil, fmt.Errorf("AGNTCY_HUB_CLIENT_SECRET env var is empty")
	}

	var scopes []string
	if v := os.Getenv("AGNTCY_HUB_SCOPES"); v != "" {
		scopes = splitCSV(v)
	}

	return &directorybridge.AgntcyGRPCConfig{
		Endpoint: endpoint,
		TLS:      tls,
		Auth: &directorybridge.AuthConfig{
			Type:            "oidc",
			Issuer:          issuer,
			ClientID:        clientID,
			ClientSecretEnv: "AGNTCY_HUB_CLIENT_SECRET",
			Scopes:          scopes,
		},
	}, nil
}

// splitCSV splits a comma-separated env value into trimmed entries.
// Empty entries are dropped.
func splitCSV(s string) []string {
	var out []string
	start := 0
	for i := 0; i <= len(s); i++ {
		if i == len(s) || s[i] == ',' {
			seg := s[start:i]
			// Trim leading/trailing ASCII space without pulling strings.
			for len(seg) > 0 && (seg[0] == ' ' || seg[0] == '\t') {
				seg = seg[1:]
			}
			for len(seg) > 0 && (seg[len(seg)-1] == ' ' || seg[len(seg)-1] == '\t') {
				seg = seg[:len(seg)-1]
			}
			if seg != "" {
				out = append(out, seg)
			}
			start = i + 1
		}
	}
	return out
}

// buildHubTestRecord constructs a minimal OASF record for the hub-
// publish smoke test. The entity ID encodes "this is a SemStreams CI
// artifact" so AGNTCY operators reviewing the directory can identify
// it as safe to delete if our withdraw cleanup ever fails.
func buildHubTestRecord() *oasfgenerator.OASFRecord {
	now := time.Now().UTC()
	entityID := fmt.Sprintf("semstreams.ci.publisher.test.%d", now.Unix())

	rec := &oasfgenerator.OASFRecord{
		Name:          "semstreams-ci-publisher-test",
		Version:       "1.0.0",
		SchemaVersion: "1.0.0",
		CreatedAt:     now.Format(time.RFC3339),
		Description:   "SemStreams CI publisher-mode smoke test — safe to garbage-collect after 1h.",
		Authors:       []string{"semstreams-ci"},
		Skills: []oasfgenerator.OASFSkill{{
			ID:         oasf.CategoryToolInteraction,
			Name:       oasf.Name(oasf.CategoryToolInteraction),
			Confidence: 1.0,
		}},
		Extensions: map[string]any{
			"semstreams_entity_id":          entityID,
			"source":                        "semstreams-e2e",
			"safe_to_garbage_collect_after": now.Add(1 * time.Hour).Format(time.RFC3339),
		},
	}
	return rec
}

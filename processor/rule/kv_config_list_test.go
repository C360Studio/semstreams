//go:build integration

package rule

import (
	"context"
	"io"
	"log/slog"
	"testing"
	"time"

	"github.com/nats-io/nats.go/jetstream"
	"github.com/stretchr/testify/require"

	"github.com/c360studio/semstreams/config"
	"github.com/c360studio/semstreams/natsclient"
)

// startRulesFamily is the production wiring from internal/boot: the one rule
// ConfigManager registers its `rules` family with a real config.Manager, and
// reaches the configuration bucket only through it. It returns the rule
// manager and a raw handle on the same bucket for fixtures that must write
// outside the family.
func startRulesFamily(t *testing.T, ctx context.Context, tc *natsclient.TestClient) (*ConfigManager, jetstream.KeyValue) {
	t.Helper()
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	rcm, err := NewConfigManager(logger)
	require.NoError(t, err)
	cfg := &config.Config{
		Version:  "1.0.0",
		Platform: config.PlatformConfig{Org: "c360", ID: "rule-kv-test"},
	}
	manager, err := config.NewConfigManager(cfg, tc.Client, logger, config.WithKeyFamily(rcm.KeyFamily()))
	require.NoError(t, err)
	require.NoError(t, manager.Start(ctx))
	t.Cleanup(func() { _ = manager.Stop(5 * time.Second) })
	name, err := config.BucketName(cfg.Platform.Org, cfg.Platform.ID)
	require.NoError(t, err)
	bucket, err := tc.Client.GetKeyValueBucket(ctx, name)
	require.NoError(t, err)
	return rcm, bucket
}

// TestListRules_ReadsOnlyTheRulesFamily verifies ListRules reads rule
// definitions from the configuration bucket's `rules.*` family and skips
// every other key in that bucket.
func TestListRules_ReadsOnlyTheRulesFamily(t *testing.T) {
	tc := natsclient.NewTestClient(t,
		natsclient.WithJetStream(),
		natsclient.WithKV())

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	rcm, bucket := startRulesFamily(t, ctx, tc)

	def := Definition{
		ID:          "alpha",
		Type:        "expression",
		Name:        "Alpha",
		Description: "first rule",
		Enabled:     true,
	}
	require.NoError(t, rcm.SaveRule(ctx, "alpha", def))

	def2 := Definition{
		ID:      "beta",
		Type:    "expression",
		Name:    "Beta",
		Enabled: false,
	}
	require.NoError(t, rcm.SaveRule(ctx, "beta", def2))

	// Poison the bucket with a non-rules key. ListRules must skip it; the
	// config manager's own keys (version, platform, platform_identity) are
	// already there and must be skipped too.
	_, err := bucket.Put(ctx, "config.unrelated", []byte(`{"junk": true}`))
	require.NoError(t, err)

	rules, err := rcm.ListRules(ctx)
	require.NoError(t, err)
	require.Len(t, rules, 2, "should return exactly the two rules.* entries")

	alpha, ok := rules["alpha"]
	require.True(t, ok, "alpha must be present")
	require.Equal(t, "expression", alpha.Type)
	require.Equal(t, "Alpha", alpha.Name)
	require.Equal(t, "first rule", alpha.Description)
	require.True(t, alpha.Enabled)

	beta, ok := rules["beta"]
	require.True(t, ok, "beta must be present")
	require.False(t, beta.Enabled, "Enabled=false must round-trip")
}

// TestListRules_EmptyBucketReturnsEmptyMap — nothing under rules.* returns an
// empty map rather than nil or an error. Callers can range over the result
// without nil-checking.
func TestListRules_EmptyBucketReturnsEmptyMap(t *testing.T) {
	tc := natsclient.NewTestClient(t,
		natsclient.WithJetStream(),
		natsclient.WithKV())

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	rcm, _ := startRulesFamily(t, ctx, tc)
	rules, err := rcm.ListRules(ctx)
	require.NoError(t, err)
	require.NotNil(t, rules)
	require.Empty(t, rules)
}

// TestListRules_SkipsUnmarshalFailures — a corrupt entry under rules.*
// should not fail the whole List; it should be logged and skipped so
// one bad record can't take down rule discovery.
func TestListRules_SkipsUnmarshalFailures(t *testing.T) {
	tc := natsclient.NewTestClient(t,
		natsclient.WithJetStream(),
		natsclient.WithKV())

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	rcm, bucket := startRulesFamily(t, ctx, tc)

	require.NoError(t, rcm.SaveRule(ctx, "good", Definition{
		ID: "good", Type: "expression", Name: "Good", Enabled: true,
	}))
	_, err := bucket.Put(ctx, "rules.bad", []byte("{not valid json"))
	require.NoError(t, err)

	rules, err := rcm.ListRules(ctx)
	require.NoError(t, err, "ListRules must not fail on a single corrupt record")
	require.Len(t, rules, 1)
	_, present := rules["good"]
	require.True(t, present)
}

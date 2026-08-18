//go:build integration

package rule

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/c360studio/semstreams/natsclient"
)

// TestListRules_KVStorePath verifies the kvStore branch added in ADR-029
// step 1: after InitializeKVStore the manager reads rule definitions
// directly from the semstreams_config bucket, filtering to the rules.*
// namespace and skipping other keys. Before the change, ListRules called
// processor.GetRuntimeConfig unconditionally and would have panicked
// here (processor is nil in this setup).
func TestListRules_KVStorePath(t *testing.T) {
	tc := natsclient.NewTestClient(t,
		natsclient.WithJetStream(),
		natsclient.WithKV())

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	// Construct a bare ConfigManager — no processor. The CRUD-only
	// usage from cmd/semstreams/main.go:buildRuleManager takes this
	// exact shape so the test mirrors production.
	rcm := NewConfigManager(nil, nil, nil)
	require.NoError(t, rcm.InitializeKVStore(context.Background(), tc.Client))

	// Seed two rule definitions + one non-rule key in the same bucket.
	// The non-rule key (config.other) proves the namespace filter works.
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

	// Poison the bucket with a non-rules key. Manager's ListRules must
	// skip it (StringPrefix check on "rules.").
	_, err := rcm.kvStore.Put(ctx, "config.unrelated", []byte(`{"junk": true}`))
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

// TestListRules_EmptyBucketReturnsEmptyMap — fresh bucket (nothing under
// rules.*) returns an empty map rather than nil or an error. Callers can
// range over the result without nil-checking.
func TestListRules_EmptyBucketReturnsEmptyMap(t *testing.T) {
	tc := natsclient.NewTestClient(t,
		natsclient.WithJetStream(),
		natsclient.WithKV())

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	rcm := NewConfigManager(nil, nil, nil)
	require.NoError(t, rcm.InitializeKVStore(context.Background(), tc.Client))

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

	rcm := NewConfigManager(nil, nil, nil)
	require.NoError(t, rcm.InitializeKVStore(context.Background(), tc.Client))

	// Write a valid rule + a corrupt one.
	require.NoError(t, rcm.SaveRule(ctx, "good", Definition{
		ID: "good", Type: "expression", Name: "Good", Enabled: true,
	}))
	_, err := rcm.kvStore.Put(ctx, "rules.bad", []byte("{not valid json"))
	require.NoError(t, err)

	rules, err := rcm.ListRules(ctx)
	require.NoError(t, err, "ListRules must not fail on a single corrupt record")
	// Only the valid rule appears; corrupt entry is skipped.
	require.Len(t, rules, 1)
	_, present := rules["good"]
	require.True(t, present)
}

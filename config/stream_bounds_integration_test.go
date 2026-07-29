//go:build integration

package config

import (
	"context"
	"io"
	"log/slog"
	"testing"
	"time"

	"github.com/nats-io/nats.go/jetstream"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/c360studio/semstreams/natsclient"
)

// boundsTestManager builds a StreamsManager over a real JetStream-enabled NATS.
func boundsTestManager(t *testing.T) (*StreamsManager, jetstream.JetStream, context.Context) {
	t.Helper()
	client := natsclient.NewTestClient(t, natsclient.WithJetStream())
	sm := NewStreamsManager(client.Client, slog.New(slog.NewTextHandler(io.Discard, nil)))

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	t.Cleanup(cancel)

	js, err := client.Client.JetStream()
	require.NoError(t, err)
	return sm, js, ctx
}

func liveStreamConfig(t *testing.T, js jetstream.JetStream, ctx context.Context, name string) jetstream.StreamConfig {
	t.Helper()
	stream, err := js.Stream(ctx, name)
	require.NoError(t, err)
	info, err := stream.Info(ctx)
	require.NoError(t, err)
	return info.Config
}

// TestEnsureStreams_DeclaredDiscardPolicyIsTheOneApplied is the acceptance
// criterion for making discard policy an operator declaration rather than the
// hardcoded jetstream.DiscardOld it was: the value the operator wrote is the
// value the created stream carries, verified against a real server rather than
// against the struct we built.
func TestEnsureStreams_DeclaredDiscardPolicyIsTheOneApplied(t *testing.T) {
	sm, js, ctx := boundsTestManager(t)

	cfg := &Config{
		Version:  "1.0.0",
		Platform: PlatformConfig{Org: "acme", ID: "test"},
		Streams: StreamConfigs{
			"DISCARDOLD": {
				Subjects: []string{"discardold.>"}, Storage: "memory",
				MaxAge: "1h", MaxBytes: 1024 * 1024, Discard: StreamDiscardOld,
			},
			"DISCARDNEW": {
				Subjects: []string{"discardnew.>"}, Storage: "memory",
				MaxAge: "2h", MaxBytes: 2 * 1024 * 1024, Discard: StreamDiscardNew,
			},
		},
	}

	require.NoError(t, sm.EnsureStreams(ctx, cfg))

	old := liveStreamConfig(t, js, ctx, "DISCARDOLD")
	assert.Equal(t, jetstream.DiscardOld, old.Discard)
	assert.Equal(t, time.Hour, old.MaxAge)
	assert.Equal(t, int64(1024*1024), old.MaxBytes)

	fresh := liveStreamConfig(t, js, ctx, "DISCARDNEW")
	assert.Equal(t, jetstream.DiscardNew, fresh.Discard,
		"the declared policy must be the applied policy — this is what DiscardOld used to override")
	assert.Equal(t, 2*time.Hour, fresh.MaxAge)
	assert.Equal(t, int64(2*1024*1024), fresh.MaxBytes)
}

// TestEnsureStreams_FrameworkStreamsCarryDeclaredBounds proves the framework's
// own guaranteed streams satisfy the contract they impose, on a real server:
// a config declaring nothing at all still boots, and every stream it creates
// carries a finite age, a finite size, and an explicit discard policy.
func TestEnsureStreams_FrameworkStreamsCarryDeclaredBounds(t *testing.T) {
	sm, js, ctx := boundsTestManager(t)

	require.NoError(t, sm.EnsureStreams(ctx, &Config{}),
		"a configuration declaring no streams must still boot")

	for _, name := range []string{"LOGS", "HEALTH", "METRICS", "FLOWS", "GOVERNANCE_VERDICT_AUDIT"} {
		live := liveStreamConfig(t, js, ctx, name)
		assert.Positive(t, live.MaxAge, "%s must carry a finite max_age", name)
		assert.Positive(t, live.MaxBytes, "%s must carry a finite max_bytes", name)
		assert.Equal(t, jetstream.DiscardOld, live.Discard,
			"%s declares discard=old; DiscardNew would refuse writes at the ceiling", name)
	}
}

// TestEnsureStreams_UnboundedStreamCreatesNothing is the fail-closed proof at
// the level that matters: an unbounded declaration does not provision the
// streams that WOULD have been valid. A boot that creates half an account and
// then stops is harder to reason about than one that never started.
func TestEnsureStreams_UnboundedStreamCreatesNothing(t *testing.T) {
	sm, js, ctx := boundsTestManager(t)

	cfg := &Config{
		Version:  "1.0.0",
		Platform: PlatformConfig{Org: "acme", ID: "test"},
		Streams: StreamConfigs{
			"WELLFORMED": {
				Subjects: []string{"wellformed.>"}, Storage: "memory",
				MaxAge: "1h", MaxBytes: 1024, Discard: StreamDiscardOld,
			},
			"UNBOUNDED": {Subjects: []string{"unbounded.>"}, Storage: "memory"},
		},
	}

	err := sm.EnsureStreams(ctx, cfg)

	require.ErrorIs(t, err, ErrStreamBoundsUndeclared)
	for _, name := range []string{"WELLFORMED", "UNBOUNDED", "LOGS"} {
		_, sErr := js.Stream(ctx, name)
		require.Error(t, sErr, "no stream may be created when the declaration set is invalid: %s", name)
	}
}

// TestEnsureStreams_ArchivalStreamIsCreatedWithNoLimit proves the archival
// classification end to end: readiness passes with no finite bounds declared,
// and the created stream carries no age or size limit at all — the contract is
// that nothing may ever be evicted from it.
func TestEnsureStreams_ArchivalStreamIsCreatedWithNoLimit(t *testing.T) {
	sm, js, ctx := boundsTestManager(t)

	cfg := &Config{
		Version:  "1.0.0",
		Platform: PlatformConfig{Org: "acme", ID: "test"},
		Streams: StreamConfigs{
			"CAMPAIGN_LEDGER": {Subjects: []string{"campaign.ledger.>"}, Storage: "file"},
		},
		ArchivalStreams: ArchivalStreams{
			"CAMPAIGN_LEDGER": {
				Owner:  "semmachina",
				Reason: "the campaign ledger is the permanent record a campaign is replayed from",
			},
		},
	}

	require.NoError(t, sm.EnsureStreams(ctx, cfg),
		"an archival declaration must satisfy readiness without finite bounds")

	live := liveStreamConfig(t, js, ctx, "CAMPAIGN_LEDGER")
	assert.Zero(t, live.MaxAge, "nothing may ever age out of an archive")
	assert.Equal(t, int64(-1), live.MaxBytes,
		"NATS reports an absent byte limit as -1; nothing may ever be evicted for size")
	assert.Equal(t, jetstream.DiscardNew, live.Discard,
		`an archive's stream info must not read "discard: old"`)
}

// TestEnsureStreams_MigrationOverrideAdmitsAnUnboundedStream proves the bridge
// works against a real server, and that it stamps nothing the operator did not
// declare: a stream being migrated TO bounds keeps behaving exactly as it did.
func TestEnsureStreams_MigrationOverrideAdmitsAnUnboundedStream(t *testing.T) {
	sm, js, ctx := boundsTestManager(t)

	future := time.Now().AddDate(1, 0, 0).Format(time.DateOnly)
	cfg := &Config{
		Version:  "1.0.0",
		Platform: PlatformConfig{Org: "acme", ID: "test"},
		Streams: StreamConfigs{
			"LEGACY": {Subjects: []string{"legacy.>"}, Storage: "memory"},
		},
		StreamMigrationOverrides: StreamMigrationOverrides{
			"LEGACY": {Owner: "team-legacy", Expires: future, Reason: "sizing study in flight"},
		},
	}

	require.NoError(t, sm.EnsureStreams(ctx, cfg))

	live := liveStreamConfig(t, js, ctx, "LEGACY")
	assert.Zero(t, live.MaxAge, "a bridge invents no bound")
	assert.Equal(t, int64(-1), live.MaxBytes)

	// And the same configuration fails once the bridge expires.
	cfg.StreamMigrationOverrides["LEGACY"] = StreamMigrationOverride{
		Owner: "team-legacy", Expires: time.Now().AddDate(0, 0, -2).Format(time.DateOnly),
	}
	require.ErrorIs(t, sm.EnsureStreams(ctx, cfg), ErrStreamMigrationOverrideExpired,
		"an expired bridge must fail readiness, not quietly keep working")
}

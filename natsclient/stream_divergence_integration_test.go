//go:build integration

package natsclient

import (
	"bytes"
	"context"
	"log/slog"
	"testing"
	"time"

	"github.com/nats-io/nats.go/jetstream"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestEnsureStream_BindDivergence covers #730: binding an existing stream discards
// the caller's configuration, which is correct, and used to do it in total silence,
// which is how a stream two components declare has its limits decided permanently
// by boot order with no diagnostic on either side.
//
// The client's log output is CAPTURED because the report IS the requirement here
// rather than a debugging nicety: a caller whose declaration was discarded has no
// other record that it happened.
//
// One container is shared across subtests; isolation comes from distinct stream
// names. Each subtest resets the log buffer, since they all read the same one.
func TestEnsureStream_BindDivergence(t *testing.T) {
	tc := NewTestClient(t, WithJetStream())

	logs := &bytes.Buffer{}
	client, err := NewClient(tc.URL,
		WithLogger(slog.New(slog.NewTextHandler(logs, &slog.HandlerOptions{Level: slog.LevelDebug}))))
	require.NoError(t, err)

	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	t.Cleanup(cancel)
	require.NoError(t, client.Connect(ctx))
	t.Cleanup(func() { _ = client.Close(context.Background()) })

	js, err := client.JetStream()
	require.NoError(t, err)

	// The acceptance criterion. Two components declaring one stream differently is
	// resolved permanently by boot order; that behavior is correct — a non-owner
	// restamping another owner's stream is worse — but it happened silently.
	t.Run("a bind that discards a declaration reports what it discarded", func(t *testing.T) {
		logs.Reset()

		// The first declarer wins, out of band, exactly as a component that booted
		// earlier would have.
		_, err := js.CreateStream(ctx, jetstream.StreamConfig{
			Name:     "SHARED",
			Subjects: []string{"shared.>"},
			Storage:  jetstream.MemoryStorage,
			MaxAge:   48 * time.Hour,
			MaxBytes: 16 << 20,
		})
		require.NoError(t, err)

		// A second caller declares the same stream with different limits.
		stream, err := client.EnsureStream(ctx, jetstream.StreamConfig{
			Name:     "SHARED",
			Subjects: []string{"shared.>"},
			Storage:  jetstream.MemoryStorage,
			MaxAge:   24 * time.Hour,
			MaxBytes: 8 << 20,
		})
		require.NoError(t, err, "binding succeeds: this is a report, not a refusal")

		logged := logs.String()
		assert.Contains(t, logged, "diverges from this caller's declaration")
		assert.Contains(t, logged, "SHARED")
		assert.Contains(t, logged, "MaxAge: declared=24h0m0s observed=48h0m0s")
		assert.Contains(t, logged, "MaxBytes: declared=8388608 observed=16777216")
		assert.Contains(t, logged, "remedy",
			"an operator has to be told that the limits belong to whoever declared the stream first")

		// And the stream is UNCHANGED. Reporting must not become restamping.
		live := stream.CachedInfo().Config
		assert.Equal(t, 48*time.Hour, live.MaxAge,
			"a non-owner silently rewriting another owner's configuration is worse than the drift")
		assert.Equal(t, int64(16<<20), live.MaxBytes)
	})

	// Keeps the signal worth reading. A report on every bind regardless of
	// divergence would be tuned out inside a week.
	t.Run("an agreeing declaration reports nothing", func(t *testing.T) {
		cfg := jetstream.StreamConfig{
			Name:     "AGREED",
			Subjects: []string{"agreed.>"},
			Storage:  jetstream.MemoryStorage,
			MaxAge:   time.Hour,
			MaxBytes: 4 << 20,
		}

		_, err := client.EnsureStream(ctx, cfg) // creates
		require.NoError(t, err)
		logs.Reset()
		_, err = client.EnsureStream(ctx, cfg) // binds, in agreement
		require.NoError(t, err)

		assert.NotContains(t, logs.String(), "diverges",
			"an agreeing declaration is not a finding")
	})

	// The rule that keeps the report from firing on nearly every bind: a zero field
	// is silence, not a declaration of zero.
	//
	// This case MUST stay quiet for the create/bind split to be usable at all — an
	// under-declared caller legitimately binds an existing stream, and reporting its
	// every unset field as drift would make the report useless exactly where the
	// split sends people.
	t.Run("an undeclared field is not a divergence", func(t *testing.T) {
		_, err := js.CreateStream(ctx, jetstream.StreamConfig{
			Name:       "RICH",
			Subjects:   []string{"rich.>"},
			Storage:    jetstream.MemoryStorage,
			MaxAge:     48 * time.Hour,
			MaxBytes:   16 << 20,
			Duplicates: 3 * time.Minute,
			MaxMsgs:    5000,
		})
		require.NoError(t, err)
		logs.Reset()

		// Declares only the subject it needs. Everything else is someone else's.
		_, err = client.EnsureStream(ctx, jetstream.StreamConfig{
			Name:     "RICH",
			Subjects: []string{"rich.>"},
		})
		require.NoError(t, err)

		assert.NotContains(t, logs.String(), "diverges",
			"omitting a field is not declaring zero, and reporting it would drown the real signal")
	})

	// The divergence with the loudest downstream consequence: a stream that does not
	// capture the caller's subject makes every publish fail with "no stream matches
	// subject", and before this the bind that caused it said nothing.
	t.Run("a subject divergence is reported", func(t *testing.T) {
		_, err := js.CreateStream(ctx, jetstream.StreamConfig{
			Name:     "DISPATCH",
			Subjects: []string{"dispatch.alpha.>"},
			Storage:  jetstream.MemoryStorage,
			MaxAge:   time.Hour,
			MaxBytes: 4 << 20,
		})
		require.NoError(t, err)
		logs.Reset()

		_, err = client.EnsureStream(ctx, jetstream.StreamConfig{
			Name:     "DISPATCH",
			Subjects: []string{"dispatch.beta.>"},
			Storage:  jetstream.MemoryStorage,
			MaxAge:   time.Hour,
			MaxBytes: 4 << 20,
		})
		require.NoError(t, err)

		logged := logs.String()
		assert.Contains(t, logged, "Subjects")
		assert.Contains(t, logged, "dispatch.beta")
		assert.Contains(t, logged, "dispatch.alpha")
	})

	// The contested-ownership signal. A divergence suppressed after the first
	// occurrence would erase the only locally available evidence that two processes
	// are fighting over one stream: the observed value alternating across boots.
	t.Run("every bind reports, not only the first", func(t *testing.T) {
		_, err := js.CreateStream(ctx, jetstream.StreamConfig{
			Name:     "CONTESTED",
			Subjects: []string{"contested.>"},
			Storage:  jetstream.MemoryStorage,
			MaxAge:   48 * time.Hour,
			MaxBytes: 16 << 20,
		})
		require.NoError(t, err)

		declared := jetstream.StreamConfig{
			Name:     "CONTESTED",
			Subjects: []string{"contested.>"},
			Storage:  jetstream.MemoryStorage,
			MaxAge:   24 * time.Hour,
			MaxBytes: 16 << 20,
		}

		for i := range 3 {
			logs.Reset()
			_, err := client.EnsureStream(ctx, declared)
			require.NoError(t, err)
			assert.Contains(t, logs.String(), "MaxAge: declared=24h0m0s observed=48h0m0s",
				"bind %d must report; suppressing repeats erases the contested-ownership signal", i)
		}
	})
}

//go:build integration

package natsclient

import (
	"bytes"
	"context"
	"log/slog"
	"sync/atomic"
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

	// The THIRD bind path, and the one that was still silent after #730: consumer
	// auto-create. A caller that reaches it declares an entire stream, and when the
	// stream already exists that declaration is dropped in favour of the live one —
	// the same discard, through a seam whose name says "create".
	//
	// Its sibling, a create that loses the race and is answered 10058, reports
	// through this same call after resolving the live handle; it is pinned by
	// "a create refused because the stream exists" below, which reaches that
	// branch with firstStreamLookupAbsent.
	t.Run("a consumer auto-create bind reports what it discarded", func(t *testing.T) {
		logs.Reset()

		_, err := js.CreateStream(ctx, jetstream.StreamConfig{
			Name:     "AUTOBIND",
			Subjects: []string{"autobind.>"},
			Storage:  jetstream.MemoryStorage,
			MaxAge:   48 * time.Hour,
			MaxBytes: 16 << 20,
		})
		require.NoError(t, err)

		handle, err := client.ConsumeInternalStreamWithConfig(ctx, StreamConsumerConfig{
			StreamName:    "AUTOBIND",
			ConsumerName:  "autobind-consumer",
			FilterSubject: "autobind.work",
			AckPolicy:     "explicit",
			DeliverPolicy: "all",
			AutoCreate:    true,
			AutoCreateConfig: &StreamAutoCreateConfig{
				Subjects: []string{"autobind.>"},
				MaxAge:   time.Hour,
				MaxBytes: 1 << 20,
				Discard:  jetstream.DiscardOld,
			},
		}, func(context.Context, jetstream.Msg) {})
		require.NoError(t, err, "binding is not the moment to enforce a declaration")
		t.Cleanup(func() {
			handle.Drain()
			<-handle.Closed()
		})

		logged := logs.String()
		assert.Contains(t, logged, "diverges from this caller's declaration")
		assert.Contains(t, logged, "AUTOBIND")
		assert.Contains(t, logged, "MaxAge")
	})

	// The seam's other bind: a create refused because the stream is already there.
	// Reaching it needs a pre-check that misses a stream that exists, which one
	// node never does on its own — hence the decorator. Everything past the first
	// lookup is the real server: a real 10058 refusal and a real live handle.
	t.Run("a create refused because the stream exists reports what it discarded", func(t *testing.T) {
		logs.Reset()

		_, err := js.CreateStream(ctx, jetstream.StreamConfig{
			Name:     "RACED",
			Subjects: []string{"raced.>"},
			Storage:  jetstream.MemoryStorage,
			MaxAge:   48 * time.Hour,
			MaxBytes: 16 << 20,
		})
		require.NoError(t, err)

		lagging := &firstStreamLookupAbsent{JetStream: js}
		err = client.ensureStreamForConsumer(ctx, lagging, StreamConsumerConfig{
			StreamName:    "RACED",
			FilterSubject: "raced.work",
			AutoCreate:    true,
			AutoCreateConfig: &StreamAutoCreateConfig{
				Subjects: []string{"raced.>"},
				MaxAge:   time.Hour,
				MaxBytes: 1 << 20,
				Discard:  jetstream.DiscardOld,
			},
		})
		require.NoError(t, err, "a stream that already exists is bound, not a failure")
		require.Equal(t, int64(2), lagging.lookups.Load(),
			"the pre-check missed, and the report resolved the live handle after the refusal")

		logged := logs.String()
		assert.Contains(t, logged, "diverges from this caller's declaration")
		assert.Contains(t, logged, "RACED")
		assert.Contains(t, logged, "MaxAge")
	})

	// The gate on both bind reports. A caller that writes AutoCreate and nothing
	// else declared nothing: the values a report would compare are the framework's
	// own defaults and a subject derived from the filter, so telling that adopter
	// "two owners are declaring one stream" names a declaration they never wrote.
	// The derived subject here deliberately does NOT match the live one, so the
	// report would fire if the gate were removed.
	t.Run("a consumer auto-create with no declaration reports nothing", func(t *testing.T) {
		logs.Reset()

		_, err := js.CreateStream(ctx, jetstream.StreamConfig{
			Name:     "UNDECLARED",
			Subjects: []string{"undeclared.alpha.>"},
			Storage:  jetstream.MemoryStorage,
			MaxAge:   48 * time.Hour,
			MaxBytes: 16 << 20,
		})
		require.NoError(t, err)

		err = client.ensureStreamForConsumer(ctx, js, StreamConsumerConfig{
			StreamName:    "UNDECLARED",
			FilterSubject: "undeclared.alpha.work",
			AutoCreate:    true,
		})
		require.NoError(t, err)

		assert.NotContains(t, logs.String(), "diverges",
			"a declaration the caller never made cannot be a divergence, and the silence it "+
				"replaces was not a defect")
	})

	// The name guard sits in front of BOTH reports. A KV or ObjectStore backing
	// stream is unbounded by construction and belongs to the bucket catalog, so
	// the unboundedness report's remedy — "set finite limits on it" — is the exact
	// opposite of what CheckOrdinaryStreamName exists to say about it, and the
	// two-owners remedy is nonsense for a bucket nobody declared as a stream.
	t.Run("a backing stream is reported on neither footing", func(t *testing.T) {
		_, err := js.CreateKeyValue(ctx, jetstream.KeyValueConfig{Bucket: "GUARDEDBIND"})
		require.NoError(t, err)
		logs.Reset()

		err = client.ensureStreamForConsumer(ctx, js, StreamConsumerConfig{
			StreamName:    KVStreamPrefix + "GUARDEDBIND",
			FilterSubject: "$KV.GUARDEDBIND.>",
			AutoCreate:    true,
			AutoCreateConfig: &StreamAutoCreateConfig{
				Subjects: []string{"$KV.GUARDEDBIND.>"},
				MaxAge:   time.Hour,
				MaxBytes: 1 << 20,
				Discard:  jetstream.DiscardOld,
			},
		})
		require.NoError(t, err, "binding an existing backing stream is not this seam's refusal to make")

		logged := logs.String()
		assert.NotContains(t, logged, "creating it today would be refused",
			"a backing stream's remedy is never 'set finite limits on it'")
		assert.NotContains(t, logged, "diverges",
			"nobody declares a bucket's backing stream as an ordinary stream")
	})

	// The two reports are gated differently, because only one of them needs a
	// declaration. An under-declared caller — AutoCreate and nothing else — is
	// precisely the caller stream_bounds.go designates the unboundedness report
	// for, so silencing it along with the comparison would remove the coverage
	// from the caller it exists to cover.
	t.Run("an undeclared caller still hears that the live stream is unbounded", func(t *testing.T) {
		_, err := js.CreateStream(ctx, jetstream.StreamConfig{
			Name:     "LEGACY_CONSUMER_BIND",
			Subjects: []string{"legacyconsumerbind.>"},
			Storage:  jetstream.MemoryStorage,
		})
		require.NoError(t, err)
		logs.Reset()

		err = client.ensureStreamForConsumer(ctx, js, StreamConsumerConfig{
			StreamName:    "LEGACY_CONSUMER_BIND",
			FilterSubject: "legacyconsumerbind.work",
			AutoCreate:    true,
		})
		require.NoError(t, err)

		logged := logs.String()
		assert.Contains(t, logged, "declares no finite bounds",
			"this report reads the LIVE stream and needs no declaration to be true")
		assert.Contains(t, logged, "LEGACY_CONSUMER_BIND")
		assert.NotContains(t, logged, "diverges from this caller's declaration",
			"the derived subject is not a declaration this caller made")
	})

	// The bind path's OTHER report, which exists because the one above cannot see
	// this case. An under-declared caller — the shape the create-versus-bind split
	// sends here — declares nothing to compare, so the declared-versus-observed
	// comparison is silent for it by construction. This fires on a property of the
	// LIVE stream instead.
	t.Run("an unbounded live stream is reported on its own terms", func(t *testing.T) {
		_, err := js.CreateStream(ctx, jetstream.StreamConfig{
			Name:     "LEGACY_UNBOUNDED",
			Subjects: []string{"legacyunbounded.>"},
			Storage:  jetstream.MemoryStorage,
		})
		require.NoError(t, err)
		logs.Reset()

		// Declares nothing but its subject, so DiffDeclaredStream finds no divergence.
		_, err = client.EnsureStream(ctx, jetstream.StreamConfig{
			Name:     "LEGACY_UNBOUNDED",
			Subjects: []string{"legacyunbounded.>"},
		})
		require.NoError(t, err, "binding is still not the moment to enforce a declaration")

		logged := logs.String()
		assert.Contains(t, logged, "declares no finite bounds")
		assert.Contains(t, logged, "LEGACY_UNBOUNDED")
		assert.Contains(t, logged, "unlimited")
		assert.Contains(t, logged, "migration condition",
			"the remedy for an unbounded stream is editing it, not renaming this caller's")
		assert.NotContains(t, logged, "diverges from this caller's declaration",
			"nothing was declared, so nothing diverged; the two reports must stay distinct")
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

// firstStreamLookupAbsent decorates a REAL jetstream.JetStream so the first
// Stream() call answers jetstream.ErrStreamNotFound and everything after it —
// every later lookup and every other method — goes to the real server.
//
// It reproduces the one condition that reaches the auto-create seam's 10058
// branch and cannot be produced on a single node: a pre-check answered by a node
// that has not applied the meta assignment, a create the server then refuses
// because the stream is there, and a live handle for the report. Only the missed
// lookup is simulated; the refusal and the handle are the server's own.
type firstStreamLookupAbsent struct {
	jetstream.JetStream
	lookups atomic.Int64
}

func (f *firstStreamLookupAbsent) Stream(ctx context.Context, name string) (jetstream.Stream, error) {
	if f.lookups.Add(1) == 1 {
		return nil, jetstream.ErrStreamNotFound
	}
	return f.JetStream.Stream(ctx, name)
}

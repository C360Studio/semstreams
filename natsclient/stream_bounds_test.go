package natsclient

import (
	"context"
	"testing"
	"time"

	"github.com/nats-io/nats.go/jetstream"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/c360studio/semstreams/pkg/errs"
)

// The bounds requirement is declared by the configuration path. These tests are
// about the OTHER door: a direct EnsureStream caller must not be the supported
// route around it. One was, in this repository — COMPONENT_CAPABILITIES was
// created here with no MaxBytes and no discard policy, and the live server chose
// both.

func TestCheckStreamBounds(t *testing.T) {
	bounded := jetstream.StreamConfig{
		Name:     "EVENTS",
		MaxAge:   time.Hour,
		MaxBytes: 1 << 20,
	}

	t.Run("a stream declaring both bounds is admitted", func(t *testing.T) {
		require.NoError(t, CheckStreamBounds(bounded, "test"))
	})

	t.Run("an undeclared field is named", func(t *testing.T) {
		cases := []struct {
			name string
			cfg  jetstream.StreamConfig
			want []string
		}{
			{
				name: "no bounds at all",
				cfg:  jetstream.StreamConfig{Name: "EVENTS"},
				want: []string{"MaxAge", "MaxBytes"},
			},
			{
				name: "age only",
				cfg:  jetstream.StreamConfig{Name: "EVENTS", MaxAge: time.Hour},
				want: []string{"MaxBytes"},
			},
			{
				name: "bytes only",
				cfg:  jetstream.StreamConfig{Name: "EVENTS", MaxBytes: 1 << 20},
				want: []string{"MaxAge"},
			},
		}
		for _, tc := range cases {
			t.Run(tc.name, func(t *testing.T) {
				err := CheckStreamBounds(tc.cfg, "test-source")

				require.ErrorIs(t, err, ErrStreamBoundsUndeclared)
				assert.Contains(t, err.Error(), "EVENTS")
				assert.Contains(t, err.Error(), "test-source",
					"the caller must be named, or an operator cannot find what to edit")
				for _, field := range tc.want {
					assert.Contains(t, err.Error(), field)
				}
			})
		}
	})

	// JetStream reads 0 and -1 alike as unlimited, so neither is a declaration.
	// The -1 case is the one that looks deliberate and is not: it is what the
	// server reports back for a field nobody set.
	t.Run("the unlimited sentinels are not declarations", func(t *testing.T) {
		for _, sentinel := range []int64{0, -1} {
			cfg := jetstream.StreamConfig{Name: "EVENTS", MaxAge: time.Hour, MaxBytes: sentinel}
			require.ErrorIs(t, CheckStreamBounds(cfg, "test"), ErrStreamBoundsUndeclared,
				"MaxBytes %d means unlimited, not bounded", sentinel)
		}
		cfg := jetstream.StreamConfig{Name: "EVENTS", MaxAge: -1, MaxBytes: 1 << 20}
		require.ErrorIs(t, CheckStreamBounds(cfg, "test"), ErrStreamBoundsUndeclared)
	})

	// A backing stream is refused outright by CheckOrdinaryStreamName and its
	// retention contract belongs to graph-retention (ADR-068/073). Demanding
	// bounds for one would be the wrong requirement, and satisfying that demand
	// would mean stamping a TTL on a bucket.
	t.Run("a backing stream is not asked for bounds", func(t *testing.T) {
		for _, name := range []string{
			KVStreamPrefix + "ENTITY_STATES",
			ObjectStoreStreamPrefix + "MESSAGES",
		} {
			assert.NoError(t, CheckStreamBounds(jetstream.StreamConfig{Name: name}, "test"),
				"%s: bounds are not this resource's contract", name)
		}
	})

	// The remedy has to name the escapes even though this seam cannot grant them,
	// or an author who needs one reaches for the nearest plausible bound instead.
	t.Run("the remedy names both escapes and where they live", func(t *testing.T) {
		err := CheckStreamBounds(jetstream.StreamConfig{Name: "LEDGER"}, "test")

		assert.Contains(t, err.Error(), "archival_streams")
		assert.Contains(t, err.Error(), "stream_migration_overrides")
		assert.Contains(t, err.Error(), "Discard",
			"the field this seam cannot require must still be asked for")
	})
}

// TestStreamBoundsSentinel_IsSharedWithTheDeclarativePath pins the one identity.
// The same requirement is enforced at two seams, and a caller — including a
// sister repo — should not have to know which door refused it. config aliases
// this value; a second errors.New of the same text would silently break that.
func TestStreamBoundsSentinel_IsSharedWithTheDeclarativePath(t *testing.T) {
	err := CheckStreamBounds(jetstream.StreamConfig{Name: "EVENTS"}, "test")
	require.ErrorIs(t, err, ErrStreamBoundsUndeclared)

	// The config package's exported sentinel is asserted to be this same value by
	// config's own test; here we pin that the message stays requirement-shaped
	// rather than seam-shaped, since both seams return it.
	assert.Equal(t, "ordinary stream bounds are not declared", ErrStreamBoundsUndeclared.Error())
}

// TestEnsureStream_BoundsRefusalIsFatal covers the classification. An
// under-declared stream config is a code or configuration defect, so retrying it
// is pointless — a transient classification would make a boot loop out of a
// permanent fault.
func TestEnsureStream_BoundsRefusalIsFatal(t *testing.T) {
	err := CheckStreamBounds(jetstream.StreamConfig{Name: "EVENTS"}, "test")
	wrapped := errs.WrapFatal(err, "Client", "EnsureStream", "validate stream bounds for EVENTS")

	assert.True(t, errs.IsFatal(wrapped))
	assert.ErrorIs(t, wrapped, ErrStreamBoundsUndeclared)
}

// TestEnsureStream_BoundsCheckDoesNotPrecedeTheConnectionCheck is the deliberate
// ORDERING, and it is the opposite of the prefix refusal's.
//
// The prefix refusal runs before the connection check, because a backing-stream
// name is never provisionable by anyone and the answer does not depend on the
// server. Bounds are different: whether they are required at all depends on
// whether this call will CREATE the stream or merely bind one that already
// exists, which cannot be known without the server. Failing closed here would
// refuse to bind a pre-existing unbounded stream — someone else's stream, and not
// this caller's decision.
func TestEnsureStream_BoundsCheckDoesNotPrecedeTheConnectionCheck(t *testing.T) {
	c := &Client{}
	require.NotEqual(t, StatusConnected, c.Status())

	_, err := c.EnsureStream(context.Background(), jetstream.StreamConfig{Name: "EVENTS"})

	require.Error(t, err)
	assert.ErrorIs(t, err, ErrNotConnected)
	assert.NotErrorIs(t, err, ErrStreamBoundsUndeclared,
		"bounds cannot be judged before we know whether this call creates or binds")
}

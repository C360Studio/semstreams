//go:build integration

package natsclient

import (
	"context"
	"testing"
	"time"

	"github.com/nats-io/nats.go/jetstream"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// These run against a real server because the create-versus-bind split is the
// whole design, and it is not observable against a fake: whether EnsureStream
// creates or binds depends on what the server already holds.

func boundsTestClient(t *testing.T) (*Client, jetstream.JetStream, context.Context) {
	t.Helper()
	tc := NewTestClient(t, WithJetStream())

	js, err := tc.Client.JetStream()
	require.NoError(t, err)

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	t.Cleanup(cancel)
	return tc.Client, js, ctx
}

// TestEnsureStream_RefusesToCreateAnUnboundedStream is the acceptance criterion
// for carrying the requirement to this seam. It also asserts the refusal left
// NOTHING behind: a guard that refuses after creating the stream would be worse
// than no guard, because the operator would be told no while holding a yes.
func TestEnsureStream_RefusesToCreateAnUnboundedStream(t *testing.T) {
	client, js, ctx := boundsTestClient(t)

	cases := []struct {
		name string
		cfg  jetstream.StreamConfig
	}{
		{
			name: "NO_BOUNDS",
			cfg:  jetstream.StreamConfig{Name: "NO_BOUNDS", Subjects: []string{"nobounds.>"}},
		},
		{
			name: "AGE_ONLY",
			cfg: jetstream.StreamConfig{
				Name: "AGE_ONLY", Subjects: []string{"ageonly.>"}, MaxAge: time.Hour,
			},
		},
		{
			// The shape the server itself reports for an unset field, which is what
			// makes it the plausible-looking mistake.
			name: "SENTINEL_BYTES",
			cfg: jetstream.StreamConfig{
				Name: "SENTINEL_BYTES", Subjects: []string{"sentinel.>"},
				MaxAge: time.Hour, MaxBytes: -1,
			},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := client.EnsureStream(ctx, tc.cfg)

			require.ErrorIs(t, err, ErrStreamBoundsUndeclared)
			assert.Contains(t, err.Error(), tc.name)

			_, getErr := js.Stream(ctx, tc.name)
			assert.ErrorIs(t, getErr, jetstream.ErrStreamNotFound,
				"a refused creation must not have created the stream")
		})
	}
}

// TestEnsureStream_CreatesWithTheDeclaredBounds proves the declaration reaches the
// server rather than being validated and then dropped.
func TestEnsureStream_CreatesWithTheDeclaredBounds(t *testing.T) {
	client, js, ctx := boundsTestClient(t)

	_, err := client.EnsureStream(ctx, jetstream.StreamConfig{
		Name:     "DECLARED",
		Subjects: []string{"declared.>"},
		Storage:  jetstream.MemoryStorage,
		MaxAge:   2 * time.Hour,
		MaxBytes: 8 << 20,
		Discard:  jetstream.DiscardNew,
	})
	require.NoError(t, err)

	stream, err := js.Stream(ctx, "DECLARED")
	require.NoError(t, err)
	live := stream.CachedInfo().Config

	assert.Equal(t, 2*time.Hour, live.MaxAge)
	assert.Equal(t, int64(8<<20), live.MaxBytes)
	assert.Equal(t, jetstream.DiscardNew, live.Discard,
		"the declared ceiling behavior must be the one the server applies")
}

// TestEnsureStream_BindsAPreExistingUnboundedStream is the other half of the
// split, and the reason the check sits inside the not-found branch.
//
// An unbounded stream someone else created is not this caller's stream and not its
// decision. Refusing to BIND it would make a non-owner unable to read it, which is
// worse than the drift — so the bind succeeds, and reporting the divergence is a
// separate obligation.
func TestEnsureStream_BindsAPreExistingUnboundedStream(t *testing.T) {
	client, js, ctx := boundsTestClient(t)

	// Created out-of-band, exactly as another process or an operator would have.
	_, err := js.CreateStream(ctx, jetstream.StreamConfig{
		Name:     "PREEXISTING",
		Subjects: []string{"preexisting.>"},
		Storage:  jetstream.MemoryStorage,
	})
	require.NoError(t, err)

	// The same under-declared config that would be REFUSED at creation.
	stream, err := client.EnsureStream(ctx, jetstream.StreamConfig{
		Name:     "PREEXISTING",
		Subjects: []string{"preexisting.>"},
	})
	require.NoError(t, err, "binding an existing stream is not the moment to enforce a declaration")
	require.NotNil(t, stream)

	live := stream.CachedInfo().Config
	assert.Equal(t, int64(-1), live.MaxBytes,
		"and it is returned UNCHANGED: a non-owner silently restamping another owner's stream is worse than the drift")
	assert.Zero(t, live.MaxAge)
}

// TestEnsureStream_BackingStreamRefusalStillWins keeps the two guards ordered. A
// KV backing stream must be refused for being a backing stream — the requirement
// that applies to it is graph-retention's, and a bounds diagnostic would send the
// operator to add a TTL to a bucket.
func TestEnsureStream_BackingStreamRefusalStillWins(t *testing.T) {
	client, _, ctx := boundsTestClient(t)

	_, err := client.EnsureStream(ctx, jetstream.StreamConfig{
		Name:     KVStreamPrefix + "ENTITY_STATES",
		Subjects: []string{"$KV.ENTITY_STATES.>"},
	})

	require.ErrorIs(t, err, ErrBackingStreamNotProvisionable)
	assert.NotErrorIs(t, err, ErrStreamBoundsUndeclared,
		"bounds are not this resource's contract; the diagnostic must not send an operator to add one")
}

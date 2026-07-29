package natsclient

import (
	"context"
	"testing"

	"github.com/nats-io/nats.go/jetstream"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/c360studio/semstreams/pkg/errs"
)

// TestCheckOrdinaryStreamName covers the shared guard's classification directly:
// which names are refused, which pass, and what the diagnostic must carry.
//
// The negative cases matter as much as the positive ones. The rule is a PREFIX
// on the reserved backing-stream namespaces, not a substring or a fuzzy match —
// an ordinary stream whose name merely contains "KV_" must still provision, or
// the guard would start refusing legitimate work.
func TestCheckOrdinaryStreamName(t *testing.T) {
	refused := []struct {
		name        string
		stream      string
		wantBucket  string
		wantClauses []string
	}{
		{
			name:       "framework KV backing stream",
			stream:     KVStreamPrefix + "ENTITY_STATES",
			wantBucket: `"ENTITY_STATES"`,
			wantClauses: []string{
				"bucket descriptor catalog's acquisition seam",
				"outside that catalog is provisioned by the component that owns it",
				"graph-retention",
			},
		},
		{
			name:       "non-catalog product KV backing stream",
			stream:     KVStreamPrefix + "SEMSOURCE_DOCUMENTS",
			wantBucket: `"SEMSOURCE_DOCUMENTS"`,
			wantClauses: []string{
				"outside that catalog is provisioned by the component that owns it",
			},
		},
		{
			name:       "ObjectStore backing stream",
			stream:     ObjectStoreStreamPrefix + "MESSAGES",
			wantBucket: `"MESSAGES"`,
			wantClauses: []string{
				"content-store constructor",
				"graph-retention",
			},
		},
		{
			// Decision 4 of the design: strip exactly ONE leading prefix, so a
			// product bucket literally named KV_FOO (backing stream KV_KV_FOO)
			// resolves to KV_FOO, not FOO.
			name:        "doubled prefix strips exactly one",
			stream:      KVStreamPrefix + KVStreamPrefix + "FOO",
			wantBucket:  `"KV_FOO"`,
			wantClauses: []string{"bucket descriptor catalog's acquisition seam"},
		},
	}

	for _, tt := range refused {
		t.Run("refused/"+tt.name, func(t *testing.T) {
			err := CheckOrdinaryStreamName(tt.stream, "unit-test")

			require.ErrorIs(t, err, ErrBackingStreamNotProvisionable)
			assert.Contains(t, err.Error(), tt.stream, "must name the refused resource")
			assert.Contains(t, err.Error(), tt.wantBucket, "must recover the bucket name")
			assert.Contains(t, err.Error(), "unit-test", "must name the declaration source")
			for _, clause := range tt.wantClauses {
				assert.Contains(t, err.Error(), clause)
			}
		})
	}

	allowed := []string{
		"AGENT",
		"LOGS",
		"GOVERNANCE_VERDICT_AUDIT",
		"MY_KV_STREAM",  // contains, but is not prefixed by, KV_
		"SOMEOBJ_THING", // contains, but is not prefixed by, OBJ_
		"KVSTATES",      // no underscore: not the reserved namespace
		"OBJECTS",
		"",
	}
	for _, name := range allowed {
		t.Run("allowed/"+name, func(t *testing.T) {
			assert.NoError(t, CheckOrdinaryStreamName(name, "unit-test"),
				"an ordinary stream name must not be refused")
		})
	}
}

// TestEnsureStream_RefusesBackingStreamName closes the fourth bypass path.
// Client.EnsureStream is operator-reachable: processor/gated-dag exposes
// dispatch_stream as config JSON and passes it straight through with a MaxAge.
// EnsureStream is get-or-create rather than reconcile, so it cannot restamp a
// LIVE bucket — but on a fresh deployment, where the bucket does not exist yet,
// it would name-squat the bucket's reserved stream with a foreign TTL and the
// wrong subjects, which the bucket's later catalog acquisition then collides
// with.
//
// The refusal must precede the connection and circuit-breaker checks: this
// client is not connected, so an unguarded EnsureStream returns ErrNotConnected,
// and that is the negative control below.
func TestEnsureStream_RefusesBackingStreamName(t *testing.T) {
	c := &Client{}
	require.NotEqual(t, StatusConnected, c.Status(),
		"test premise: an unguarded call would be rejected as not-connected")

	for _, name := range []string{
		KVStreamPrefix + "ENTITY_STATES",
		KVStreamPrefix + "SEMSOURCE_DOCUMENTS",
		ObjectStoreStreamPrefix + "MESSAGES",
	} {
		t.Run(name, func(t *testing.T) {
			_, err := c.EnsureStream(context.Background(), jetstream.StreamConfig{
				Name:     name,
				Subjects: []string{"dispatch.>"},
				MaxAge:   24 * 60 * 60, // the gated-dag shape: a TTL on a bucket's stream
			})

			require.ErrorIs(t, err, ErrBackingStreamNotProvisionable)
			assert.True(t, errs.IsFatal(err), "a backing-stream name is unrecoverable, not transient")
			assert.NotErrorIs(t, err, ErrNotConnected,
				"refusal must precede the connection/circuit checks, not depend on them")
			assert.Contains(t, err.Error(), name)
		})
	}

	t.Run("ordinary name reaches the connection check", func(t *testing.T) {
		_, err := c.EnsureStream(context.Background(), jetstream.StreamConfig{Name: "AGENT"})
		require.Error(t, err)
		assert.NotErrorIs(t, err, ErrBackingStreamNotProvisionable,
			"an ordinary stream name must get past the guard")
	})
}

// TestCreateStream_RefusesBackingStreamName is the same contract on the other
// natsclient provisioning seam, so the two cannot drift.
func TestCreateStream_RefusesBackingStreamName(t *testing.T) {
	c := &Client{}

	for _, name := range []string{
		KVStreamPrefix + "ENTITY_STATES",
		ObjectStoreStreamPrefix + "MESSAGES",
	} {
		t.Run(name, func(t *testing.T) {
			_, err := c.CreateStream(context.Background(), jetstream.StreamConfig{Name: name})

			require.ErrorIs(t, err, ErrBackingStreamNotProvisionable)
			assert.True(t, errs.IsFatal(err))
			assert.NotErrorIs(t, err, ErrNotConnected,
				"refusal must precede the connection/circuit checks")
		})
	}

	t.Run("ordinary name reaches the connection check", func(t *testing.T) {
		_, err := c.CreateStream(context.Background(), jetstream.StreamConfig{Name: "AGENT"})
		require.Error(t, err)
		assert.NotErrorIs(t, err, ErrBackingStreamNotProvisionable)
	})
}

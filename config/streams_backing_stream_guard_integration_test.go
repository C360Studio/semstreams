//go:build integration

package config

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"testing"
	"time"

	"github.com/nats-io/nats.go/jetstream"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/c360studio/semstreams/graph"
	"github.com/c360studio/semstreams/natsclient"
)

// TestEnsureStreams_BackingStreamRefusal_IssuesNoNATSCall is the real-NATS half
// of the provisioner prohibition. The unit tests prove the refusal is
// classifiable and names the resource; this proves the clause they cannot —
// "no create, update, or reconcile call is issued against that stream".
//
// The setup is the live hazard, not a proxy for it: an EXISTING KV bucket and an
// EXISTING ObjectStore, each declared into cfg.Streams with subjects that differ
// from the backing stream's real ones. Without the guard, createStream sees
// subject drift and calls UpdateStream with its own retention defaults.
//
// Measured against NATS 2.12 with the guard removed (this test, instrumented):
//
//	OBJ_MESSAGES before: MaxAge=0s  Discard=DiscardNew Subjects=[$O.MESSAGES.C.> $O.MESSAGES.M.>]
//	OBJ_MESSAGES after:  MaxAge=1h  Discard=DiscardOld Subjects=[obj_messages.>]
//	EnsureStreams returned: <nil>
//
// That is silent, total destruction of a content store — age eviction switched
// on, the discard policy flipped to delete-oldest, and the store's real chunk
// and meta subjects replaced, so no existing object is addressable and no new
// write can land. The KV case happens to be refused SERVER-side (the KV backing
// stream carries DenyDelete, and NATS rejects an update that would cancel it
// with an opaque `code=500 err_code=10052`), but that is a NATS implementation
// accident on one field of one resource class, not a contract — and it aborts
// boot with a diagnostic that names neither the resource's owner nor the reason.
//
// The post-conditions below are read back from the server, so a refusal that
// happened after a partial write would fail here.
func TestEnsureStreams_BackingStreamRefusal_IssuesNoNATSCall(t *testing.T) {
	client := natsclient.NewTestClient(t, natsclient.WithJetStream())
	sm := NewStreamsManager(client.Client, slog.New(slog.NewTextHandler(io.Discard, nil)))

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	js, err := client.Client.JetStream()
	require.NoError(t, err)

	// An existing KV bucket, created the ordinary way, with no lifecycle
	// retention — the ADR-068 posture the guard exists to keep intact.
	_, err = js.CreateKeyValue(ctx, jetstream.KeyValueConfig{
		Bucket:  graph.BucketEntityStates,
		History: 1,
	})
	require.NoError(t, err)
	kvBackingStream := natsclient.KVStreamPrefix + graph.BucketEntityStates

	// An existing ObjectStore, likewise.
	const objBucket = "MESSAGES"
	_, err = js.CreateObjectStore(ctx, jetstream.ObjectStoreConfig{Bucket: objBucket})
	require.NoError(t, err)
	objBackingStream := natsclient.ObjectStoreStreamPrefix + objBucket

	configOf := func(name string) jetstream.StreamConfig {
		stream, serr := js.Stream(ctx, name)
		require.NoError(t, serr)
		info, ierr := stream.Info(ctx)
		require.NoError(t, ierr)
		return info.Config
	}

	beforeKV := configOf(kvBackingStream)
	beforeOBJ := configOf(objBackingStream)
	require.Zero(t, beforeKV.MaxAge, "precondition: KV backing stream carries no TTL")
	require.Zero(t, beforeOBJ.MaxAge, "precondition: ObjectStore backing stream carries no TTL")

	cfg := &Config{
		Version:    "1.0.0",
		Platform:   PlatformConfig{Org: "acme", ID: "test"},
		Components: ComponentConfigs{},
		Streams: StreamConfigs{
			// Subjects deliberately unlike the real backing-stream subjects, so
			// an unguarded provisioner would see drift and UpdateStream.
			kvBackingStream: {Subjects: []string{"kv_entity_states.>"}, MaxAge: "1h"},
		},
	}

	err = sm.EnsureStreams(ctx, cfg)
	require.ErrorIs(t, err, ErrBackingStreamNotProvisionable)

	assert.Equal(t, beforeKV, configOf(kvBackingStream),
		"KV backing-stream config must be byte-identical after a refused declaration")

	// Same for the ObjectStore backing stream, declared on its own pass so the
	// map-iteration order cannot decide which one gets reached.
	cfg.Streams = StreamConfigs{
		objBackingStream: {Subjects: []string{"obj_messages.>"}, MaxAge: "1h"},
	}
	err = sm.EnsureStreams(ctx, cfg)
	require.ErrorIs(t, err, ErrBackingStreamNotProvisionable)

	assert.Equal(t, beforeOBJ, configOf(objBackingStream),
		"ObjectStore backing-stream config must be byte-identical after a refused declaration")

	// And a KV name outside the descriptor catalog is refused on the same rule,
	// creating nothing — the prefix-not-membership contract, proven against the
	// server rather than against the error string.
	const productBucket = "SEMSOURCE_DOCUMENTS"
	require.Empty(t, graph.OwnerOf(productBucket), "test premise: bucket is outside the catalog")
	productBackingStream := natsclient.KVStreamPrefix + productBucket

	cfg.Streams = StreamConfigs{
		productBackingStream: {Subjects: []string{"kv_semsource_documents.>"}},
	}
	err = sm.EnsureStreams(ctx, cfg)
	require.ErrorIs(t, err, ErrBackingStreamNotProvisionable)

	_, err = js.Stream(ctx, productBackingStream)
	assert.True(t, errors.Is(err, jetstream.ErrStreamNotFound),
		"a refused non-catalog backing stream must not have been created; got %v", err)
}

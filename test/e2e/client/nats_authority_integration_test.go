//go:build integration

package client

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/c360studio/semstreams/natsclient"
	"github.com/nats-io/nats.go/jetstream"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestIntegration_FindAuthorityTriplesByPredicatePrefix_MatchesDeterministically(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	validation, bucket := newAuthorityProvenanceTestClient(t, ctx)

	putAuthorityEntity(t, ctx, bucket, EntityState{
		ID: "acme.ops.robotics.gcs.drone.002",
		Triples: []Triple{
			{Subject: "acme.ops.robotics.gcs.drone.002", Predicate: "hierarchy.type.member", Context: "inference.hierarchy"},
			{Subject: "acme.ops.robotics.gcs.drone.002", Predicate: "robotics.status.armed", Context: "source.test"},
		},
	})
	putAuthorityEntity(t, ctx, bucket, EntityState{
		ID: "acme.ops.robotics.gcs.drone.001",
		Triples: []Triple{
			{Subject: "acme.ops.robotics.gcs.drone.001", Predicate: "hierarchy.system.member", Context: "inference.hierarchy"},
		},
	})

	matches, err := validation.findAuthorityTriplesByPredicatePrefix(ctx, "hierarchy.", 2)
	require.NoError(t, err)
	require.Len(t, matches, 2)
	assert.Equal(t, "acme.ops.robotics.gcs.drone.001", matches[0].EntityID)
	assert.Equal(t, "hierarchy.system.member", matches[0].Triple.Predicate)
	assert.Equal(t, "acme.ops.robotics.gcs.drone.002", matches[1].EntityID)
	assert.Equal(t, "hierarchy.type.member", matches[1].Triple.Predicate)
}

func TestIntegration_FindAuthorityTriplesByPredicatePrefix_ReturnsBadContextForValidation(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	validation, bucket := newAuthorityProvenanceTestClient(t, ctx)

	putAuthorityEntity(t, ctx, bucket, EntityState{
		ID: "acme.ops.robotics.gcs.drone.001",
		Triples: []Triple{
			{Subject: "acme.ops.robotics.gcs.drone.001", Predicate: "hierarchy.type.member"},
			{Subject: "acme.ops.robotics.gcs.drone.001", Predicate: "hierarchy.system.member", Context: "source.wrong"},
			{Subject: "acme.ops.robotics.gcs.drone.001", Predicate: "robotics.status.armed", Context: "inference.hierarchy"},
		},
	})

	matches, err := validation.findAuthorityTriplesByPredicatePrefix(ctx, "hierarchy.", 1)
	require.NoError(t, err)
	require.Len(t, matches, 2)
	assert.Empty(t, matches[0].Triple.Context)
	assert.Equal(t, "source.wrong", matches[1].Triple.Context)
}

func TestIntegration_FindAuthorityTriplesByPredicatePrefix_RejectsOverflowBeforeDecode(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	validation, bucket := newAuthorityProvenanceTestClient(t, ctx)

	for _, entityID := range []string{
		"acme.ops.robotics.gcs.drone.001",
		"acme.ops.robotics.gcs.drone.002",
		"acme.ops.robotics.gcs.drone.003",
	} {
		_, err := bucket.Put(ctx, entityID, []byte("not-json"))
		require.NoError(t, err)
	}

	matches, err := validation.findAuthorityTriplesByPredicatePrefix(ctx, "hierarchy.", 2)
	require.Error(t, err)
	assert.Nil(t, matches)
	assert.Contains(t, err.Error(), "exceeds entity limit 2")
	assert.NotContains(t, err.Error(), "decode",
		"overflow must fail before any authoritative value is fetched or decoded")
}

func newAuthorityProvenanceTestClient(
	t *testing.T,
	ctx context.Context,
) (*NATSValidationClient, jetstream.KeyValue) {
	t.Helper()
	testClient := natsclient.NewTestClient(t, natsclient.WithKV())
	js, err := testClient.Client.JetStream()
	require.NoError(t, err)
	bucket, err := js.CreateKeyValue(ctx, jetstream.KeyValueConfig{Bucket: BucketEntityStates})
	require.NoError(t, err)
	return &NATSValidationClient{client: testClient.Client}, bucket
}

func putAuthorityEntity(t *testing.T, ctx context.Context, bucket jetstream.KeyValue, entity EntityState) {
	t.Helper()
	data, err := json.Marshal(entity)
	require.NoError(t, err)
	_, err = bucket.Put(ctx, entity.ID, data)
	require.NoError(t, err)
}

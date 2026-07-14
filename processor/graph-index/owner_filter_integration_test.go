//go:build integration

package graphindex

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/c360studio/semstreams/natsclient"
	"github.com/nats-io/nats.go/jetstream"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestOwnerFilters_RealNATSExactnessAndCancellation(t *testing.T) {
	testClient := natsclient.NewTestClient(t, natsclient.WithKV())
	js, err := testClient.Client.JetStream()
	require.NoError(t, err)

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	owner := "acme.ops.robotics.gcs.drone.001"
	other := "acme.ops.robotics.gcs.drone.002"
	target := "acme.ops.robotics.gcs.mission.001"
	tests := []struct {
		name   string
		filter string
		owned  string
		other  string
	}{
		{
			name:   "predicate",
			filter: predicateIndexEntityFilter(owner),
			owned:  predicateIndexKey("robotics.status.armed", owner),
			other:  predicateIndexKey("robotics.status.armed", other),
		},
		{
			name:   "name",
			filter: nameIndexEntityFilter(owner),
			owned:  nameCompositeKey(nameIndexKey("Alpha"), owner, "core.identity.name"),
			other:  nameCompositeKey(nameIndexKey("Alpha"), other, "core.identity.name"),
		},
		{
			name:   "incoming",
			filter: incomingIndexSourceFilter(owner),
			owned:  incomingIndexKey(target, owner, "robotics.assigned.mission"),
			other:  incomingIndexKey(target, other, "robotics.assigned.mission"),
		},
		{
			name:   "context",
			filter: contextIndexEntityFilter(owner),
			owned:  contextIndexKey(owner, contextHashHex("source.alpha"), "robotics.status.armed"),
			other:  contextIndexKey(other, contextHashHex("source.alpha"), "robotics.status.armed"),
		},
	}

	for i, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			bucketName := fmt.Sprintf("OWNER_FILTER_%d", i)
			raw, createErr := js.CreateKeyValue(ctx, jetstream.KeyValueConfig{Bucket: bucketName})
			require.NoError(t, createErr)
			store := testClient.Client.NewKVStore(raw)
			_, err = store.Put(ctx, tt.owned, []byte("owned"))
			require.NoError(t, err)
			_, err = store.Put(ctx, tt.other, []byte("other"))
			require.NoError(t, err)

			keys, listErr := store.KeysByFilter(ctx, tt.filter)
			require.NoError(t, listErr)
			assert.Equal(t, []string{tt.owned}, keys)

			cancelled, cancelNow := context.WithCancel(ctx)
			cancelNow()
			keys, listErr = store.KeysByFilter(cancelled, tt.filter)
			assert.ErrorIs(t, listErr, context.Canceled)
			assert.Nil(t, keys)
		})
	}

	// The raw helper obeys the same cancellation rule used by components that
	// hold jetstream.KeyValue directly.
	raw, err := js.CreateKeyValue(ctx, jetstream.KeyValueConfig{Bucket: "OWNER_FILTER_RAW"})
	require.NoError(t, err)
	cancelled, cancelNow := context.WithCancel(ctx)
	cancelNow()
	keys, err := natsclient.FilteredKeys(cancelled, raw, ">")
	assert.True(t, errors.Is(err, context.Canceled))
	assert.Nil(t, keys)
}

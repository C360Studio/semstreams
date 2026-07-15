//go:build integration

package graphindex

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/c360studio/semstreams/natsclient"
	"github.com/nats-io/nats.go/jetstream"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestOwnerFilters_RealNATSExactnessAndCancellation(t *testing.T) {
	owner := "acme.ops.robotics.gcs.drone.001"
	other := "acme.ops.robotics.gcs.drone.002"
	target := "acme.ops.robotics.gcs.mission.001"
	otherTarget := "acme.ops.robotics.gcs.mission.002"
	ownerParts := strings.Split(owner, ".")
	predicate := "robotics.status.armed"
	predicateToken := encodePredicateToken(predicate)
	predicateHash := predicateHashHex(predicate)
	nameHash := nameIndexKey("Alpha")
	contextHash := contextHashHex("source.alpha")

	ownerTests := []struct {
		name     string
		filter   string
		owned    []string
		controls []string
	}{
		{
			name:   "predicate",
			filter: predicateIndexEntityFilter(owner),
			owned:  []string{predicateIndexKey(predicate, owner)},
			controls: []string{
				predicateIndexKey(predicate, other),
				predicateHash + "." + strings.Join(ownerParts[:5], "."),
				predicateIndexKey(predicate, owner) + ".extra",
				owner + "." + predicateHash,
			},
		},
		{
			name:   "name",
			filter: nameIndexEntityFilter(owner),
			owned:  []string{nameCompositeKey(nameHash, owner, "core.identity.name")},
			controls: []string{
				nameCompositeKey(nameHash, other, "core.identity.name"),
				nameHash + "." + owner,
				nameCompositeKey(nameHash, owner, "core.identity.name") + ".extra",
				owner + "." + nameHash + "." + encodePredicateToken("core.identity.name"),
			},
		},
		{
			name:   "incoming",
			filter: incomingIndexSourceFilter(owner),
			owned: []string{
				incomingIndexKey(target, owner, "robotics.assigned.mission"),
				incomingIndexKey(otherTarget, owner, "robotics.assigned.mission"),
			},
			controls: []string{
				incomingIndexKey(target, other, "robotics.assigned.mission"),
				target + "." + owner,
				incomingIndexKey(target, owner, "robotics.assigned.mission") + ".extra",
				owner + "." + target + "." + encodePredicateToken("robotics.assigned.mission"),
			},
		},
		{
			name:   "context",
			filter: contextIndexEntityFilter(owner),
			owned:  []string{contextIndexKey(owner, contextHash, predicate)},
			controls: []string{
				contextIndexKey(other, contextHash, predicate),
				owner + "." + contextHash,
				contextIndexKey(owner, contextHash, predicate) + ".extra",
				contextHash + "." + owner + "." + predicateToken,
			},
		},
	}
	forwardTests := []struct {
		name     string
		filter   string
		owned    []string
		controls []string
	}{
		{
			name: "predicate", filter: predicateIndexForwardFilter(predicate),
			owned: []string{predicateIndexKey(predicate, owner)},
			controls: []string{
				predicateIndexKey("robotics.status.disarmed", owner),
				predicateHash + "." + strings.Join(ownerParts[:5], "."),
				predicateIndexKey(predicate, owner) + ".extra",
				owner + "." + predicateHash,
			},
		},
		{
			name: "name", filter: nameIndexForwardFilter("Alpha"),
			owned: []string{nameCompositeKey(nameHash, owner, "core.identity.name")},
			controls: []string{
				nameCompositeKey(nameIndexKey("Beta"), owner, "core.identity.name"),
				nameHash + "." + owner,
				nameCompositeKey(nameHash, owner, "core.identity.name") + ".extra",
				owner + "." + nameHash + "." + encodePredicateToken("core.identity.name"),
			},
		},
		{
			name: "incoming", filter: incomingIndexTargetFilter(target),
			owned: []string{incomingIndexKey(target, owner, "robotics.assigned.mission")},
			controls: []string{
				incomingIndexKey(otherTarget, owner, "robotics.assigned.mission"),
				target + "." + owner,
				incomingIndexKey(target, owner, "robotics.assigned.mission") + ".extra",
				owner + "." + target + "." + encodePredicateToken("robotics.assigned.mission"),
			},
		},
	}

	// Reject malformed proof fixtures before opening NATS or writing any row.
	for _, tt := range append(ownerTests, forwardTests...) {
		require.NoError(t, natsclient.ValidateKVWildcardFilter(tt.filter), tt.name)
		for _, key := range append(append([]string(nil), tt.owned...), tt.controls...) {
			require.NoError(t, natsclient.ValidateKVLiteralKey(key), "%s: %s", tt.name, key)
		}
	}

	testClient := natsclient.NewTestClient(t, natsclient.WithKV())
	js, err := testClient.Client.JetStream()
	require.NoError(t, err)

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	for i, tt := range ownerTests {
		t.Run(tt.name, func(t *testing.T) {
			bucketName := fmt.Sprintf("OWNER_FILTER_%d", i)
			raw, createErr := js.CreateKeyValue(ctx, jetstream.KeyValueConfig{Bucket: bucketName})
			require.NoError(t, createErr)
			store := testClient.Client.NewKVStore(raw)
			for _, key := range tt.owned {
				_, err = store.Put(ctx, key, []byte("owned"))
				require.NoError(t, err)
			}
			for _, key := range tt.controls {
				_, err = store.Put(ctx, key, []byte("control"))
				require.NoError(t, err)
			}

			keys, listErr := store.KeysByFilter(ctx, tt.filter)
			require.NoError(t, listErr)
			assert.ElementsMatch(t, tt.owned, keys)

			cancelled, cancelNow := context.WithCancel(ctx)
			cancelNow()
			keys, listErr = store.KeysByFilter(cancelled, tt.filter)
			assert.ErrorIs(t, listErr, context.Canceled)
			assert.Nil(t, keys)
		})
	}

	for i, tt := range forwardTests {
		t.Run("forward "+tt.name, func(t *testing.T) {
			bucketName := fmt.Sprintf("FORWARD_FILTER_%d", i)
			raw, createErr := js.CreateKeyValue(ctx, jetstream.KeyValueConfig{Bucket: bucketName})
			require.NoError(t, createErr)
			store := testClient.Client.NewKVStore(raw)
			for _, key := range tt.owned {
				_, err = store.Put(ctx, key, []byte("owned"))
				require.NoError(t, err)
			}
			for _, key := range tt.controls {
				_, err = store.Put(ctx, key, []byte("control"))
				require.NoError(t, err)
			}

			keys, listErr := store.KeysByFilter(ctx, tt.filter)
			require.NoError(t, listErr)
			assert.ElementsMatch(t, tt.owned, keys)
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

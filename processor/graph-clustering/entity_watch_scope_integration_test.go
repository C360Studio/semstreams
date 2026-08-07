//go:build integration

package graphclustering

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/c360studio/semstreams/component"
	"github.com/c360studio/semstreams/graph"
	"github.com/c360studio/semstreams/message"
	"github.com/c360studio/semstreams/natsclient"
	"github.com/nats-io/nats.go/jetstream"
	"github.com/stretchr/testify/require"
)

// TestIntegration_ClusteringHoldsNoEntityStatesWatcher is the wire-level scope
// assertion for poison-response-scoping (graph-clustering delta): at steady
// state, graph-clustering holds NO live watcher (JetStream consumer) on the
// ENTITY_STATES stream. Contract validation rides the polled input reads, so
// an entity write commits with zero read-shaped deliveries fanned out to
// clustering — the retired dedicated contract watch was clustering's only
// ENTITY_STATES watcher (the declared "entity_watch" kv-watch input port is
// discovery metadata; the input path is timer-driven polled reads).
//
// The probe is validated first against a real watcher so a zero count is a
// proven negative, not a broken filter.
func TestIntegration_ClusteringHoldsNoEntityStatesWatcher(t *testing.T) {
	testClient := natsclient.NewTestClient(t, natsclient.WithKV())
	nc := testClient.Client

	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	js, err := nc.JetStream()
	require.NoError(t, err)

	entityBucket, err := js.CreateKeyValue(ctx, jetstream.KeyValueConfig{
		Bucket: graph.BucketEntityStates,
	})
	require.NoError(t, err)
	_, err = js.CreateKeyValue(ctx, jetstream.KeyValueConfig{
		Bucket: graph.BucketOutgoingIndex,
	})
	require.NoError(t, err)
	_, err = js.CreateKeyValue(ctx, jetstream.KeyValueConfig{
		Bucket: graph.BucketIncomingIndex,
	})
	require.NoError(t, err)

	entityStream, err := js.Stream(ctx, "KV_"+graph.BucketEntityStates)
	require.NoError(t, err)
	consumerCount := func() int {
		info, infoErr := entityStream.Info(ctx)
		require.NoError(t, infoErr)
		return info.State.Consumers
	}

	// Probe sanity: a live ENTITY_STATES watcher must be visible to the count
	// before a zero can prove anything.
	probe, err := entityBucket.WatchAll(ctx)
	require.NoError(t, err)
	require.Eventually(t, func() bool { return consumerCount() == 1 },
		5*time.Second, 50*time.Millisecond, "probe watcher should register one stream consumer")
	require.NoError(t, probe.Stop())
	require.Eventually(t, func() bool { return consumerCount() == 0 },
		5*time.Second, 50*time.Millisecond, "stopped probe watcher should deregister")

	// Long detection interval: no polled Keys() scan (which creates a
	// short-lived ordered consumer) can run inside the observation window, so
	// any consumer observed below would be a held watcher.
	config := Config{
		AllowUngatedReads: true,
		Ports: &component.PortConfig{
			Inputs: []component.PortDefinition{
				{Name: "entity_watch", Config: component.KVWatchPort{Bucket: graph.BucketEntityStates}},
			},
			Outputs: []component.PortDefinition{
				{Name: "communities", Config: component.KVWritePort{Bucket: graph.BucketCommunityIndex}},
			},
		},
		DetectionIntervalStr: "1h",
		MinCommunitySize:     2,
		MaxIterations:        10,
	}
	config.ApplyDefaults()
	configJSON, err := json.Marshal(config)
	require.NoError(t, err)

	comp, err := CreateGraphClustering(configJSON, component.Dependencies{NATSClient: nc})
	require.NoError(t, err)
	clusteringComp := comp.(*Component)
	require.NoError(t, clusteringComp.Initialize())
	require.NoError(t, clusteringComp.Start(ctx))
	defer clusteringComp.Stop(5 * time.Second)

	require.Equal(t, 0, consumerCount(),
		"clustering must hold no ENTITY_STATES watcher after Start")

	// A steady-state entity write must fan out zero deliveries to clustering:
	// with zero consumers on the stream there is no subscription to deliver
	// to. Observe over a bounded window (negative assertions need one; watcher
	// registration is synchronous with Start, so 1s is generous).
	now := time.Now().UTC()
	entityID := "c360.platform.robotics.mav1.drone.001"
	state := graph.EntityState{
		ID: entityID,
		Triples: []message.Triple{{
			Subject: entityID, Predicate: "robotics.status.armed", Object: true,
			Source: "test", Timestamp: now,
		}},
		MessageType: message.Type{Domain: "test", Category: "entity", Version: "v1"},
		Version:     1,
		UpdatedAt:   now,
	}
	stateData, err := graph.MarshalEntityState(&state)
	require.NoError(t, err)
	_, err = entityBucket.Put(ctx, entityID, stateData)
	require.NoError(t, err)

	deadline := time.Now().Add(1 * time.Second)
	for time.Now().Before(deadline) {
		require.Equal(t, 0, consumerCount(),
			"clustering must hold no ENTITY_STATES watcher at steady state")
		time.Sleep(100 * time.Millisecond)
	}

	require.True(t, clusteringComp.Health().Healthy,
		"component must run healthy without a dedicated contract watch")
}

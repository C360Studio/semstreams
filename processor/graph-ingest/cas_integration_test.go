//go:build integration

package graphingest

import (
	"context"
	"encoding/json"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/c360studio/semstreams/component"
	"github.com/c360studio/semstreams/graph"
	"github.com/c360studio/semstreams/message"
	"github.com/c360studio/semstreams/natsclient"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestIntegration_ConcurrentCanonicalAppend verifies the canonical append lane
// preserves every distinct tuple under real JetStream CAS contention.
func TestIntegration_ConcurrentCanonicalAppend(t *testing.T) {
	ctx := context.Background()
	streams := []natsclient.TestStreamConfig{{Name: "ENTITY", Subjects: []string{"entity.>"}}}
	testClient := natsclient.NewTestClient(t, natsclient.WithKV(), natsclient.WithStreams(streams...))
	configJSON, err := json.Marshal(DefaultConfig())
	require.NoError(t, err)
	created, err := CreateGraphIngest(configJSON, component.Dependencies{NATSClient: testClient.Client})
	require.NoError(t, err)
	c := created.(*Component)
	require.NoError(t, c.Initialize())
	require.NoError(t, c.Start(ctx))
	t.Cleanup(func() { _ = c.Stop(5 * time.Second) })

	const entityID = "c360.test.cas.concurrent.drone.001"
	require.NoError(t, c.CreateEntity(ctx, &graph.EntityState{ID: entityID, Version: 1, UpdatedAt: time.Now()}))

	const writers = 20
	var wg sync.WaitGroup
	var successes atomic.Int32
	start := make(chan struct{})
	for index := range writers {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			request, marshalErr := json.Marshal(graph.AppendTriplesRequest{Triples: []message.Triple{{
				Subject: entityID, Predicate: "test.concurrent.value", Object: index,
				Timestamp: time.Now(), Confidence: 1, Source: "cas-test",
			}}})
			if marshalErr != nil {
				return
			}
			body, appendErr := c.handleCanonicalAppend(ctx, request)
			if appendErr != nil {
				return
			}
			var response graph.AppendTriplesResponse
			if json.Unmarshal(body, &response) == nil && len(response.Results) == 1 && response.Results[0].Outcome == graph.MutationApplied {
				successes.Add(1)
			}
		}()
	}
	close(start)
	wg.Wait()

	assert.Equal(t, int32(writers), successes.Load())
	entry, err := c.entityBucket.Get(ctx, entityID)
	require.NoError(t, err)
	var entity graph.EntityState
	require.NoError(t, json.Unmarshal(entry.Value, &entity))
	assert.Equal(t, writers, nonProfileTripleCount(&entity), "no concurrent append may be lost")
}

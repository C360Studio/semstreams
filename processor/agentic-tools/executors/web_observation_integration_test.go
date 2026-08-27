//go:build integration

package executors

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/c360studio/semstreams/agentic"
	"github.com/c360studio/semstreams/component"
	"github.com/c360studio/semstreams/graph"
	"github.com/c360studio/semstreams/message"
	"github.com/c360studio/semstreams/natsclient"
	"github.com/c360studio/semstreams/payloadbuiltins"
	agentictools "github.com/c360studio/semstreams/processor/agentic-tools"
	graphingest "github.com/c360studio/semstreams/processor/graph-ingest"
	agvocab "github.com/c360studio/semstreams/vocabulary/agentic"
)

// TestWebObservationBirthIsRegistered (O-10): until a web-observation e2e tier
// exists, this is the gate that a web observation births through a graph-ingest
// carrying the builtin set with its registered type.
func TestWebObservationBirthIsRegistered(t *testing.T) {
	ctx := context.Background()
	tc := natsclient.NewTestClient(t,
		natsclient.WithKV(),
		natsclient.WithStreams(natsclient.TestStreamConfig{Name: "ENTITY", Subjects: []string{"entity.>"}}),
	)
	client := tc.Client

	configJSON, err := json.Marshal(graphingest.DefaultConfig())
	require.NoError(t, err)
	created, err := graphingest.CreateGraphIngest(configJSON, component.Dependencies{
		NATSClient: client, PayloadRegistry: payloadbuiltins.NewTestRegistry(t),
	})
	require.NoError(t, err)
	ingest := created.(*graphingest.Component)
	require.NoError(t, ingest.Initialize())
	require.NoError(t, ingest.Start(ctx))
	t.Cleanup(func() { _ = ingest.Stop(context.Background()) })
	require.NoError(t, tc.GetNativeConnection().Flush())

	publisher := agentictools.NewNATSTriplePublisher(client)
	urlEntity, canonical, err := agentic.TryWebObservationEntityID("acme", "ops", "https://example.com/gate")
	require.NoError(t, err)
	now := time.Now()
	triples := []message.Triple{{
		Subject: urlEntity, Predicate: agvocab.WebURL, Object: canonical,
		Source: "agent-http-request", Timestamp: now, Confidence: 1.0,
	}}
	require.NoError(t, publishWebObservation(ctx, publisher, urlEntity, triples))

	js, err := client.JetStream()
	require.NoError(t, err)
	kv, err := js.KeyValue(ctx, graph.BucketEntityStates)
	require.NoError(t, err)
	entry, err := kv.Get(ctx, urlEntity)
	require.NoError(t, err)
	var entity graph.EntityState
	require.NoError(t, json.Unmarshal(entry.Value(), &entity))
	assert.Equal(t, agentic.WebObservationMessageType(), entity.MessageType)
	assert.Equal(t, "agentic.web_observation.v1", entity.MessageType.Key())
}

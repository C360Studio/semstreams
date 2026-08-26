//go:build integration

package agentictools_test

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/c360studio/semstreams/component"
	"github.com/c360studio/semstreams/graph"
	"github.com/c360studio/semstreams/natsclient"
	"github.com/c360studio/semstreams/payloadbuiltins"
	"github.com/c360studio/semstreams/pkg/projection"
	graphingest "github.com/c360studio/semstreams/processor/graph-ingest"
	"github.com/c360studio/semstreams/vocabulary/builtins"
)

func startGraphIngestForMutationTest(t *testing.T, client *natsclient.Client) *graphingest.Component {
	t.Helper()

	configJSON, err := json.Marshal(graphingest.DefaultConfig())
	require.NoError(t, err)
	created, err := graphingest.CreateGraphIngest(configJSON, component.Dependencies{NATSClient: client, PayloadRegistry: payloadbuiltins.NewTestRegistry(t)})
	require.NoError(t, err)

	ingest := created.(*graphingest.Component)
	require.NoError(t, ingest.Initialize())
	require.NoError(t, ingest.Start(context.Background()))
	t.Cleanup(func() { require.NoError(t, ingest.Stop(context.Background())) })
	return ingest
}

func readEntityKV(t *testing.T, client *natsclient.Client, entityID string) *graph.EntityState {
	t.Helper()

	js, err := client.JetStream()
	require.NoError(t, err)
	kv, err := js.KeyValue(context.Background(), graph.BucketEntityStates)
	require.NoError(t, err)
	entry, err := kv.Get(context.Background(), entityID)
	require.NoError(t, err)

	var entity graph.EntityState
	require.NoError(t, json.Unmarshal(entry.Value(), &entity))
	return &entity
}

func predicatesPresent(entity *graph.EntityState, predicate string) int {
	count := 0
	for _, triple := range entity.Triples {
		if triple.Predicate == predicate {
			count++
		}
	}
	return count
}

func objectsOf(entity *graph.EntityState, predicate string) []string {
	var objects []string
	for _, triple := range entity.Triples {
		if triple.Predicate != predicate {
			continue
		}
		value, ok := triple.Object.(string)
		if ok {
			objects = append(objects, value)
		}
	}
	return objects
}

func graphMutationTestClient(t *testing.T) *natsclient.Client {
	t.Helper()
	return natsclient.NewTestClient(t,
		natsclient.WithKV(),
		natsclient.WithStreams(natsclient.TestStreamConfig{Name: "ENTITY", Subjects: []string{"entity.>"}}),
	).Client
}

func newTestMutationClient(t *testing.T, client *natsclient.Client) *projection.MutationClient {
	t.Helper()

	builtins.Register()
	mutations, err := projection.NewMutationClient(projection.MutationClientConfig{
		NATS:      client,
		Contracts: payloadbuiltins.NewTestRegistry(t).Contracts(),
	})
	require.NoError(t, err)
	return mutations
}

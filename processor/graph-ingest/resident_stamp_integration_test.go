//go:build integration

package graphingest

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/nats-io/nats.go/jetstream"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/c360studio/semstreams/component"
	"github.com/c360studio/semstreams/graph"
	"github.com/c360studio/semstreams/internal/graphmutation"
	"github.com/c360studio/semstreams/message"
	"github.com/c360studio/semstreams/natsclient"
	"github.com/c360studio/semstreams/payloadbuiltins"
)

// TestResidentUnregisteredStampIsNotPoison documents design §10 and is a
// barrier against a later registry-consulting codec: an entity persisted under
// a key no binary registers is swept without a poison entry, reads back with
// the stamp unchanged, and stays mutable through must-exist operations.
func TestResidentUnregisteredStampIsNotPoison(t *testing.T) {
	ctx := context.Background()
	streams := []natsclient.TestStreamConfig{{Name: "ENTITY", Subjects: []string{"entity.>"}}}
	testClient := natsclient.NewTestClient(t, natsclient.WithKV(), natsclient.WithStreams(streams...))
	nc := testClient.Client

	const id = "c360.test.resident.system.legacy.001"
	now := time.Now()
	kv, err := nc.CreateKeyValueBucket(ctx, jetstream.KeyValueConfig{Bucket: graph.BucketEntityStates})
	require.NoError(t, err)
	resident := &graph.EntityState{
		ID: id, MessageType: message.Type{Domain: "legacy", Category: "gone", Version: "v1"},
		Version: 1, UpdatedAt: now,
		Triples: []message.Triple{{Subject: id, Predicate: "test.state.value", Object: "resident", Timestamp: now, Confidence: 1}},
	}
	encoded, err := graph.MarshalEntityState(resident)
	require.NoError(t, err)
	_, err = kv.Put(ctx, id, encoded)
	require.NoError(t, err)

	configJSON, err := json.Marshal(DefaultConfig())
	require.NoError(t, err)
	comp, err := CreateGraphIngest(configJSON, component.Dependencies{NATSClient: nc, PayloadRegistry: payloadbuiltins.NewTestRegistry(t)})
	require.NoError(t, err)
	c := comp.(*Component)
	require.NoError(t, c.Initialize())
	require.NoError(t, c.Start(ctx))
	t.Cleanup(func() { _ = c.Stop(context.Background()) })
	require.NoError(t, c.ensureEntityQueriesReady())

	_, inventoried := poisonInventoryEntry(c, id)
	assert.False(t, inventoried, "a resident unregistered stamp is not poison")

	exact, err := graph.NewExactEntityReader(nc, 5*time.Second).ReadExactEntity(ctx, id)
	require.NoError(t, err)
	assert.Equal(t, "legacy.gone.v1", exact.Entity.MessageType.Key(), "the stamp reads back unchanged")

	client, err := graphmutation.NewClient(nc, 5*time.Second)
	require.NoError(t, err)
	appended, err := client.Append(ctx, graph.AppendTriplesRequest{Triples: []message.Triple{{
		Subject: id, Predicate: "test.event.value", Object: "appended", Timestamp: now, Confidence: 1,
	}}})
	require.NoError(t, err)
	require.Len(t, appended.Results, 1)
	assert.Equal(t, graph.MutationApplied, appended.Results[0].Outcome, "must-exist mutations ignore the stamp")
}

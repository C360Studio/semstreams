//go:build integration

package graphingest

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/prometheus/client_golang/prometheus/testutil"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/c360studio/semstreams/agentic"
	"github.com/c360studio/semstreams/component"
	"github.com/c360studio/semstreams/graph"
	"github.com/c360studio/semstreams/graph/inference"
	"github.com/c360studio/semstreams/internal/graphmutation"
	"github.com/c360studio/semstreams/message"
	"github.com/c360studio/semstreams/natsclient"
	"github.com/c360studio/semstreams/payloadbuiltins"
	"github.com/c360studio/semstreams/payloadregistry"
	"github.com/c360studio/semstreams/pkg/errs"
	"github.com/c360studio/semstreams/vocabulary"
)

const gateCreateSubject = "graph.mutation.entity.create"

// startGateTestComponent boots graph-ingest over a real NATS testcontainer with
// the supplied payload registry, serving the mutation subjects.
func startGateTestComponent(t *testing.T, reg *payloadregistry.Registry, enableHierarchy bool) (context.Context, *Component, *natsclient.Client) {
	t.Helper()
	ctx := context.Background()

	streams := []natsclient.TestStreamConfig{{Name: "ENTITY", Subjects: []string{"entity.>"}}}
	testClient := natsclient.NewTestClient(t, natsclient.WithKV(), natsclient.WithStreams(streams...))

	config := DefaultConfig()
	config.EnableHierarchy = enableHierarchy
	configJSON, err := json.Marshal(config)
	require.NoError(t, err)

	comp, err := CreateGraphIngest(configJSON, component.Dependencies{NATSClient: testClient.Client, PayloadRegistry: reg})
	require.NoError(t, err)
	c := comp.(*Component)
	require.NoError(t, c.Initialize())
	require.NoError(t, c.Start(ctx))
	t.Cleanup(func() { _ = c.Stop(context.Background()) })
	require.NoError(t, testClient.GetNativeConnection().Flush())
	return ctx, c, testClient.Client
}

func gateMutationClient(t *testing.T, nc *natsclient.Client) *graphmutation.Client {
	t.Helper()
	client, err := graphmutation.NewClient(nc, 5*time.Second)
	require.NoError(t, err)
	return client
}

func gateCreateRequest(id string, mt message.Type) graph.CreateEntityRequest {
	now := time.Now()
	return graph.CreateEntityRequest{
		Entity:  &graph.EntityState{ID: id, MessageType: mt, Version: 1, UpdatedAt: now},
		Triples: []message.Triple{{Subject: id, Predicate: "test.state.value", Object: "born", Timestamp: now, Confidence: 1}},
	}
}

// TestCreateRejectsUnregisteredMessageType: an unregistered stamp never reaches
// ENTITY_STATES — the reply carries the closed code and the key in detail, the
// rejection is metered exactly once, and no key is created.
func TestCreateRejectsUnregisteredMessageType(t *testing.T) {
	ctx, c, nc := startGateTestComponent(t, payloadbuiltins.NewTestRegistry(t), false)
	counter := getMutationRejectionsMetric(nil).WithLabelValues(gateCreateSubject, graph.ErrorCodeMessageTypeUnregistered)
	before := testutil.ToFloat64(counter)

	const id = "c360.test.gate.system.widget.unregistered"
	unknown := message.Type{Domain: "test", Category: "unknown", Version: "v1"}
	_, err := gateMutationClient(t, nc).Create(ctx, gateCreateRequest(id, unknown))
	require.Error(t, err, "an unregistered stamp must be refused over the wire")

	var ce *errs.ClassifiedError
	require.ErrorAs(t, err, &ce, "the reply decodes into a classified error")
	assert.Equal(t, graph.ErrorCodeMessageTypeUnregistered, ce.Code)
	assert.Equal(t, "test.unknown.v1", ce.Detail["message_type"])
	assert.True(t, errs.IsInvalid(err), "the caller registers the type; it does not retry")

	_, getErr := c.entityBucket.Get(ctx, id)
	require.Error(t, getErr, "nothing may be persisted for the rejected create")
	assert.True(t, errors.Is(getErr, natsclient.ErrKVKeyNotFound))

	assert.InDelta(t, before+1, testutil.ToFloat64(counter), 0.0001,
		"mutation_rejections_total{reason=message_type_unregistered} increments exactly once")
}

// TestCreateAcceptsRegisteredMessageType: a registered stamp is born unchanged.
func TestCreateAcceptsRegisteredMessageType(t *testing.T) {
	ctx, c, nc := startGateTestComponent(t, payloadbuiltins.NewTestRegistry(t), false)

	const id = "c360.test.gate.system.lesson.registered"
	response, err := gateMutationClient(t, nc).Create(ctx, gateCreateRequest(id, agentic.AgentLessonMessageType()))
	require.NoError(t, err)
	assert.Equal(t, graph.MutationApplied, response.Outcome)

	stored := storedEntity(t, c, id)
	assert.Equal(t, agentic.AgentLessonMessageType(), stored.MessageType, "the stamp is persisted verbatim")
}

// TestFloorComesFromRegistration: the indexing-profile floor is read from the
// registered type; a registered type with no floor falls to control and is
// metered, a registered floor is not.
func TestFloorComesFromRegistration(t *testing.T) {
	reg := payloadbuiltins.NewTestRegistry(t)
	payloadregistry.RegisterTestType(t, reg, "test.nofloor.v1")
	ctx, c, nc := startGateTestComponent(t, reg, false)
	client := gateMutationClient(t, nc)

	t.Run("registered floor is stamped without a metric", func(t *testing.T) {
		counter := getIndexingProfileDefaultMetric(nil).WithLabelValues("agentic.request.v1")
		before := testutil.ToFloat64(counter)
		const id = "c360.test.gate.system.request.floor"
		_, err := client.Create(ctx, gateCreateRequest(id, message.Type{Domain: "agentic", Category: "request", Version: "v1"}))
		require.NoError(t, err)
		assert.Equal(t, []string{vocabulary.IndexingProfileTrace}, profileValues(storedEntity(t, c, id)))
		assert.InDelta(t, before, testutil.ToFloat64(counter), 0.0001, "a registered floor is not a gap")
	})

	t.Run("registered type with no floor is metered", func(t *testing.T) {
		counter := getIndexingProfileDefaultMetric(nil).WithLabelValues("test.nofloor.v1")
		before := testutil.ToFloat64(counter)
		const id = "c360.test.gate.system.nofloor.001"
		_, err := client.Create(ctx, gateCreateRequest(id, message.Type{Domain: "test", Category: "nofloor", Version: "v1"}))
		require.NoError(t, err)
		assert.Equal(t, []string{vocabulary.IndexingProfileControl}, profileValues(storedEntity(t, c, id)))
		assert.InDelta(t, before+1, testutil.ToFloat64(counter), 0.0001, "a registered type without a floor is the metered gap")
	})
}

// TestHierarchyContainerBirthCarriesRegisteredType (O-16 (a)): a container born
// by graph-ingest's own in-process lane carries graph.hierarchy_container.v1 and
// the unknown-label metric does not fire.
func TestHierarchyContainerBirthCarriesRegisteredType(t *testing.T) {
	reg := payloadbuiltins.NewTestRegistry(t)
	payloadregistry.RegisterTestType(t, reg, "test.entity.v1")
	ctx, c, _ := startGateTestComponent(t, reg, true)
	unknown := getIndexingProfileDefaultMetric(nil).WithLabelValues("unknown")
	before := testutil.ToFloat64(unknown)

	const id = "c360.platform.robotics.mav1.drone.gate001"
	now := time.Now()
	require.NoError(t, c.CreateEntity(ctx, &graph.EntityState{
		ID: id, MessageType: message.Type{Domain: "test", Category: "entity", Version: "v1"},
		Triples: []message.Triple{{Subject: id, Predicate: "entity.type.class", Object: "test.entity", Timestamp: now, Confidence: 1}},
		Version: 1, UpdatedAt: now,
	}))

	container := storedEntity(t, c, "c360.platform.robotics.mav1.drone.group")
	assert.Equal(t, inference.HierarchyContainerMessageType(), container.MessageType)
	assert.Equal(t, "graph.hierarchy_container.v1", container.MessageType.Key())
	assert.Equal(t, []string{vocabulary.IndexingProfileControl}, profileValues(container), "the container takes its registered floor")
	assert.InDelta(t, before, testutil.ToFloat64(unknown), 0.0001,
		"indexing_profile_default_total{message_type=unknown} must not fire for the framework's own writer")
}

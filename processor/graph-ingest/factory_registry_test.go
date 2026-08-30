package graphingest

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/prometheus/client_golang/prometheus/testutil"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/c360studio/semstreams/component"
	"github.com/c360studio/semstreams/graph"
	"github.com/c360studio/semstreams/message"
	"github.com/c360studio/semstreams/natsclient"
	"github.com/c360studio/semstreams/payloadbuiltins"
	"github.com/c360studio/semstreams/payloadregistry"
	"github.com/c360studio/semstreams/pkg/errs"
)

func factoryTestDeps(t *testing.T, reg *payloadregistry.Registry) component.Dependencies {
	t.Helper()
	natsClient, err := natsclient.NewClient("nats://localhost:4222")
	require.NoError(t, err)
	return component.Dependencies{
		NATSClient: natsClient, PayloadRegistry: reg,
		Platform: component.PlatformMeta{Org: "c360", Platform: "test"},
	}
}

func factoryTestConfig(t *testing.T, enableHierarchy bool) json.RawMessage {
	t.Helper()
	config := DefaultConfig()
	config.EnableHierarchy = enableHierarchy
	configJSON, err := json.Marshal(config)
	require.NoError(t, err)
	return configJSON
}

// newRegistryMockKVComponent builds a component through the production factory
// with the supplied registry and swaps in the in-memory mock bucket.
func newRegistryMockKVComponent(t *testing.T, reg *payloadregistry.Registry) *Component {
	t.Helper()
	deps := factoryTestDeps(t, reg)
	comp, err := CreateGraphIngest(factoryTestConfig(t, false), deps)
	require.NoError(t, err)
	c := comp.(*Component)
	c.entityBucket = deps.NATSClient.NewKVStore(newMockKVBucket())
	return c
}

// TestFactoryRejectsNilPayloadRegistry: a missing registry is a construction
// error naming the dependency, not a first-message surprise.
func TestFactoryRejectsNilPayloadRegistry(t *testing.T) {
	_, err := CreateGraphIngest(factoryTestConfig(t, false), factoryTestDeps(t, nil))
	require.Error(t, err)
	assert.Contains(t, err.Error(), "payload registry")

	_, err = CreateGraphIngest(factoryTestConfig(t, false), factoryTestDeps(t, payloadregistry.NewForTest(t)))
	require.NoError(t, err, "with a registry the factory constructs")
}

// TestCreateSeamRejectsWhenRegistryMissing (O-15): a component that somehow
// holds no registry refuses a create at the seam with code internal — never a
// pass-through, never a panic.
func TestCreateSeamRejectsWhenRegistryMissing(t *testing.T) {
	c := &Component{}
	const id = "c360.test.seam.system.widget.001"
	data, err := json.Marshal(graph.CreateEntityRequest{
		Entity: &graph.EntityState{ID: id, MessageType: message.Type{Domain: "test", Category: "widget", Version: "v1"}},
	})
	require.NoError(t, err)

	var reply []byte
	var handlerErr error
	require.NotPanics(t, func() { reply, handlerErr = c.handleCanonicalCreate(context.Background(), data) })
	require.Error(t, handlerErr)
	assert.Nil(t, reply)

	var ce *errs.ClassifiedError
	require.ErrorAs(t, handlerErr, &ce)
	assert.Equal(t, graph.ErrorCodeInternal, ce.Code)
}

// TestInProcessCreateRejectsUnregisteredType: Component.CreateEntity (the
// hierarchy container lane) is gated by the same helper — the classified
// error is returned to the caller, nothing is written, nothing is metered.
func TestInProcessCreateRejectsUnregisteredType(t *testing.T) {
	c := newRegistryMockKVComponent(t, payloadregistry.NewForTest(t))
	counter := getMutationRejectionsMetric(nil).WithLabelValues("graph.mutation.entity.create", graph.ErrorCodeMessageTypeUnregistered)
	before := testutil.ToFloat64(counter)

	const id = "c360.test.inprocess.system.widget.001"
	now := time.Now()
	err := c.CreateEntity(context.Background(), &graph.EntityState{
		ID: id, MessageType: message.Type{Domain: "test", Category: "unknown", Version: "v1"},
		Version: 1, UpdatedAt: now,
	})
	require.Error(t, err)

	var ce *errs.ClassifiedError
	require.ErrorAs(t, err, &ce)
	assert.Equal(t, graph.ErrorCodeMessageTypeUnregistered, ce.Code)
	assert.Equal(t, "test.unknown.v1", ce.Detail["message_type"])
	assert.True(t, errs.IsInvalid(err))

	_, getErr := c.entityBucket.Get(context.Background(), id)
	require.Error(t, getErr, "nothing may be written for the rejected in-process birth")
	assert.True(t, natsclient.IsKVNotFoundError(getErr))
	assert.InDelta(t, before, testutil.ToFloat64(counter), 0.0001, "the in-process lane is not metered")
}

// TestFactoryRejectsHierarchyWithoutContainerType (O-16 (a), F7): hierarchy on
// with a registry that lacks graph.hierarchy_container.v1 is a construction
// error naming the type; with the builtin set it constructs.
func TestFactoryRejectsHierarchyWithoutContainerType(t *testing.T) {
	_, err := CreateGraphIngest(factoryTestConfig(t, true), factoryTestDeps(t, payloadregistry.NewForTest(t)))
	require.Error(t, err)
	assert.Contains(t, err.Error(), "graph.hierarchy_container.v1")

	_, err = CreateGraphIngest(factoryTestConfig(t, true), factoryTestDeps(t, payloadbuiltins.NewTestRegistry(t)))
	require.NoError(t, err, "with the builtin set registered, hierarchy constructs")
}

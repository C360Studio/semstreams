package agentic_test

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/c360studio/semstreams/agentic"
	"github.com/c360studio/semstreams/message"
	"github.com/c360studio/semstreams/payloadbuiltins"
)

// --- TryLoopResultEntityID ---

func TestTryLoopResultEntityID_Valid(t *testing.T) {
	t.Parallel()
	id, err := agentic.TryLoopResultEntityID("c360", "ops", "abc123")
	require.NoError(t, err)
	assert.Equal(t, "c360.ops.agent.agentic-loop.result.abc123", id)
	assert.True(t, message.IsValidEntityID(id))
}

func TestTryLoopResultEntityID_RejectsMalformedParts(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name                string
		org, platform, loop string
	}{
		{"empty org", "", "ops", "abc"},
		{"empty platform", "c360", "", "abc"},
		{"empty loop", "c360", "ops", ""},
		{"dotted org", "c.360", "ops", "abc"},
		{"dotted loop", "c360", "ops", "a.bc"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			_, err := agentic.TryLoopResultEntityID(tc.org, tc.platform, tc.loop)
			assert.Error(t, err, "malformed identity parts must be rejected, not silently accepted")
		})
	}
}

// --- LoopResultEntity ContentStorable contract ---

func TestLoopResultEntity_ContentStorableContract(t *testing.T) {
	t.Parallel()
	e, err := agentic.NewLoopResultEntity("c360", "ops", "loop-1", "the full result body")
	require.NoError(t, err)

	// It must satisfy the interface the ObjectStore consumes.
	var cs message.ContentStorable = e

	assert.Equal(t, "c360.ops.agent.agentic-loop.result.loop-1", cs.EntityID())
	assert.Nil(t, cs.Triples(), "content-only entity: loop semantics live on the execution entity")

	// The body role must resolve through ContentFields to a field present in
	// RawContent — this is the exact lookup path read_loop_result hydration
	// uses on the persisted envelope.
	fieldName := cs.ContentFields()[message.ContentRoleBody]
	require.NotEmpty(t, fieldName, "body role must be mapped")
	body, ok := cs.RawContent()[fieldName]
	require.True(t, ok, "body field %q must exist in RawContent", fieldName)
	assert.Equal(t, "the full result body", body)

	// StorageRef round-trip.
	assert.Nil(t, e.StorageRef())
	ref := &message.StorageReference{StorageInstance: "objectstore", Key: "content_x"}
	e.SetStorageRef(ref)
	assert.Same(t, ref, e.StorageRef())
}

func TestNewLoopResultEntity_InvalidIdentityFailsClosed(t *testing.T) {
	t.Parallel()
	_, err := agentic.NewLoopResultEntity("", "", "loop-1", "body")
	assert.Error(t, err, "missing platform identity must refuse construction (caller skips offload)")
}

// --- LoopCompletedEvent offload triplet: production-decoder round-trip ---

// TestLoopCompletedEvent_OffloadFields_ProductionWireRoundTrip drives the
// ref-bearing completion shape through the production payload decoder
// (feedback_production_decoder_round_trip_required): the {storage_ref,
// preview, size} triplet must survive BaseMessage marshal → registry decode
// intact, and a nil ref must be omitted entirely.
func TestLoopCompletedEvent_OffloadFields_ProductionWireRoundTrip(t *testing.T) {
	t.Parallel()
	ev := &agentic.LoopCompletedEvent{
		LoopID:      "loop-offload-001",
		TaskID:      "task-001",
		Outcome:     agentic.OutcomeSuccess,
		Role:        "coordinator",
		Result:      "", // offloaded: inline result replaced by the triplet
		Model:       "model-x",
		CompletedAt: time.Now().UTC().Truncate(time.Second),
		ResultRef: &message.StorageReference{
			StorageInstance: "objectstore",
			Key:             "content_c360.ops.agent.agentic-loop.result.loop-offload-001",
			ContentType:     "application/json",
			Size:            1234,
		},
		ResultPreview: "the first bytes of the result…",
		ResultSize:    9_999_999,
	}

	baseMsg := message.NewBaseMessage(ev.Schema(), ev, "test")
	data, err := json.Marshal(baseMsg)
	require.NoError(t, err)

	// The wire names are the D4 contract: storage_ref / preview / size.
	assert.Contains(t, string(data), `"storage_ref"`)
	assert.Contains(t, string(data), `"preview"`)
	assert.Contains(t, string(data), `"size"`)

	decoded, err := payloadbuiltins.NewTestDecoder(t).Decode(data)
	require.NoError(t, err)
	got, ok := decoded.Payload().(*agentic.LoopCompletedEvent)
	require.True(t, ok, "expected *agentic.LoopCompletedEvent, got %T", decoded.Payload())

	require.NotNil(t, got.ResultRef)
	assert.Equal(t, ev.ResultRef.Key, got.ResultRef.Key)
	assert.Equal(t, ev.ResultRef.StorageInstance, got.ResultRef.StorageInstance)
	assert.Equal(t, ev.ResultPreview, got.ResultPreview)
	assert.Equal(t, ev.ResultSize, got.ResultSize)
	assert.Empty(t, got.Result)
}

func TestLoopCompletedEvent_OffloadFields_OmittedWhenInline(t *testing.T) {
	t.Parallel()
	ev := &agentic.LoopCompletedEvent{
		LoopID:  "loop-inline-001",
		TaskID:  "task-001",
		Outcome: agentic.OutcomeSuccess,
		Result:  "small inline result",
	}
	data, err := json.Marshal(ev)
	require.NoError(t, err)
	assert.NotContains(t, string(data), `"storage_ref"`,
		"inline completions must not grow phantom offload fields")
	assert.NotContains(t, string(data), `"preview"`)
	assert.NotContains(t, string(data), `"size"`)
}

// --- LoopEntity durability fields: JSON round-trip (KV state shape) ---

// TestLoopEntity_ResultDurabilityFields_RoundTrip pins the KV-persisted
// entity shape: offload mirror (result_ref/result_size) and the typed
// result-not-durable marker survive marshal/unmarshal, and both are omitted
// on the happy path so existing readers see byte-identical values.
func TestLoopEntity_ResultDurabilityFields_RoundTrip(t *testing.T) {
	t.Parallel()
	entity := agentic.LoopEntity{
		ID:            "loop-1",
		TaskID:        "task-1",
		State:         agentic.LoopStateComplete,
		MaxIterations: 5,
		Outcome:       agentic.OutcomeSuccess,
		Result:        "preview text only",
		ResultRef: &message.StorageReference{
			StorageInstance: "objectstore",
			Key:             "content_c360.ops.agent.agentic-loop.result.loop-1",
		},
		ResultSize:             5 << 20,
		ResultNotDurable:       true,
		ResultNotDurableReason: "invalid: payload exceeds the server's maximum payload size",
	}
	data, err := json.Marshal(&entity)
	require.NoError(t, err)

	var got agentic.LoopEntity
	require.NoError(t, json.Unmarshal(data, &got))
	require.NotNil(t, got.ResultRef)
	assert.Equal(t, entity.ResultRef.Key, got.ResultRef.Key)
	assert.Equal(t, entity.ResultSize, got.ResultSize)
	assert.True(t, got.ResultNotDurable)
	assert.Equal(t, entity.ResultNotDurableReason, got.ResultNotDurableReason)
}

func TestLoopEntity_ResultDurabilityFields_OmittedByDefault(t *testing.T) {
	t.Parallel()
	entity := agentic.LoopEntity{
		ID:            "loop-2",
		TaskID:        "task-2",
		State:         agentic.LoopStateExecuting,
		MaxIterations: 5,
	}
	data, err := json.Marshal(&entity)
	require.NoError(t, err)
	for _, field := range []string{`"result_ref"`, `"result_size"`, `"result_not_durable"`, `"result_not_durable_reason"`} {
		assert.NotContains(t, string(data), field,
			"zero-valued durability fields must be omitted (additive contract)")
	}
}

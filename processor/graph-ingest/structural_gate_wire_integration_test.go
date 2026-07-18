//go:build integration

package graphingest

import (
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/c360studio/semstreams/graph"
	"github.com/c360studio/semstreams/message"
	"github.com/c360studio/semstreams/natsclient"
	"github.com/c360studio/semstreams/pkg/errs"
)

// Task 4.3 (enforce-structural-invariants): the structural rejection must reach
// mutation-API callers as a CLASSIFIED error over the real NATS request/reply
// wire — never silently decoded as a zero-valued success. These tests drive the
// production chain end-to-end: SubscribeForRequests-registered handler →
// meteredMutation → validateTriplePredicates → natsclient.RespondError
// (X-Status/X-Error-Class/X-Error-Code headers + {message} body) →
// ClassifyReply/RequestClassified on the caller side.
//
// Wire contract asserted (ADR-060):
//
//	X-Status:      error
//	X-Error-Class: invalid
//	X-Error-Code:  structural_invalid   (triple.add lane — handler gate)
//	               invalid_request      (create_with_triples lane — authoritative
//	                                     contract seam fires first)
//	body:          {"message": "...<offending predicate>..."}
func TestIntegration_StructuralGate_TripleAdd_WireContract(t *testing.T) {
	ctx, c := startBatchTestComponent(t)

	const absentID = "c360.test.structural.wire.drone.404"
	reqBytes, err := json.Marshal(graph.AddTripleRequest{Triple: message.Triple{
		Subject: absentID, Predicate: "agent.role", Object: "researcher", // predicate-audit:invalid {"kind":"stored-predicate","value":"agent.role","reason":"arity"}
		Timestamp: time.Now(), Confidence: 1.0,
	}})
	require.NoError(t, err)

	t.Run("raw_reply_headers", func(t *testing.T) {
		msg, err := c.natsClient.RequestWithHeaders(ctx, SubjectTripleAdd, reqBytes, nil, 2*time.Second)
		require.NoError(t, err, "transport must succeed — the failure is a handler-classified reply")
		require.NotNil(t, msg)

		assert.Equal(t, natsclient.HeaderStatusError, msg.Header.Get(natsclient.HeaderStatus),
			"X-Status must be error — a raw-Request caller that ignores headers would otherwise "+
				"json.Unmarshal the error body into a zero-valued response (the silent-success bug)")
		assert.Equal(t, natsclient.ErrorClassInvalid, msg.Header.Get(natsclient.HeaderErrorClass),
			"structural rejection is invalid-class (do-not-retry)")
		assert.Equal(t, graph.ErrorCodeStructuralInvalid, msg.Header.Get(natsclient.HeaderErrorCode),
			"triple.add lane carries the specific structural_invalid code")

		var body struct {
			Message string `json:"message"`
		}
		require.NoError(t, json.Unmarshal(msg.Data, &body), "error body is the {message} envelope")
		assert.Contains(t, body.Message, "agent.role", "the wire message names the offending predicate")
	})

	t.Run("classified_caller_path", func(t *testing.T) {
		respBytes, err := c.natsClient.RequestClassified(ctx, SubjectTripleAdd, reqBytes, 2*time.Second)
		require.Error(t, err, "RequestClassified must surface the rejection as an error, not a success body")
		assert.Nil(t, respBytes)

		var ce *errs.ClassifiedError
		require.ErrorAs(t, err, &ce, "caller receives a reconstructed *errs.ClassifiedError")
		assert.Equal(t, graph.ErrorCodeStructuralInvalid, ce.Code)
		assert.True(t, errs.IsInvalid(err), "callers branch on errs.IsInvalid for do-not-retry")
		assert.True(t, strings.Contains(err.Error(), "agent.role"), "error text names the predicate")
	})

	// Fail-closed proof on the store: the target entity must not exist.
	_, getErr := c.entityBucket.Get(ctx, absentID)
	require.Error(t, getErr, "nothing may be persisted for the rejected mutation")
	assert.True(t, errors.Is(getErr, natsclient.ErrKVKeyNotFound))
}

// TestIntegration_StructuralGate_CreateWithTriples_WireContract covers the
// create_with_triples lane over the wire: the authoritative entity-state
// contract seam rejects the malformed predicate first, so the caller sees the
// generic invalid_request code — still a classified invalid error naming the
// predicate, still nothing persisted.
func TestIntegration_StructuralGate_CreateWithTriples_WireContract(t *testing.T) {
	ctx, c := startBatchTestComponent(t)

	const createID = "c360.test.structural.wire.drone.777"
	now := time.Now()
	reqBytes, err := json.Marshal(graph.CreateEntityWithTriplesRequest{
		Entity: &graph.EntityState{
			ID:          createID,
			MessageType: message.Type{Domain: "test", Category: "mutation", Version: "v1"},
			Version:     1,
			UpdatedAt:   now,
		},
		Triples: []message.Triple{
			{Subject: createID, Predicate: "agent.role", Object: "researcher", Timestamp: now, Confidence: 1.0}, // predicate-audit:invalid {"kind":"stored-predicate","value":"agent.role","reason":"arity"}
		},
	})
	require.NoError(t, err)

	respBytes, err := c.natsClient.RequestClassified(ctx, SubjectEntityCreateWithTriples, reqBytes, 2*time.Second)
	require.Error(t, err, "the malformed predicate must reject the create over the wire")
	assert.Nil(t, respBytes)

	var ce *errs.ClassifiedError
	require.ErrorAs(t, err, &ce)
	assert.Equal(t, graph.ErrorCodeInvalidRequest, ce.Code,
		"authoritative contract seam precedes the handler gate on this lane")
	assert.True(t, errs.IsInvalid(err))
	assert.True(t, strings.Contains(err.Error(), "agent.role"), "error text names the predicate")

	_, getErr := c.entityBucket.Get(ctx, createID)
	require.Error(t, getErr, "nothing may be persisted for the rejected create")
	assert.True(t, errors.Is(getErr, natsclient.ErrKVKeyNotFound))
}

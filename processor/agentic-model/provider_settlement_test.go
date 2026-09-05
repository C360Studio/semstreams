package agenticmodel

import (
	"context"
	"encoding/json"
	"errors"
	"testing"

	"github.com/c360studio/semstreams/agentic"
	"github.com/c360studio/semstreams/message"
	"github.com/c360studio/semstreams/natsclient"
	"github.com/c360studio/semstreams/payloadbuiltins"
	"github.com/c360studio/semstreams/pkg/errs"
	"github.com/stretchr/testify/require"
)

type stubRetainedResponseReader struct {
	evidence retainedResponseEvidence
	found    bool
	err      error
	calls    int
}

func (r *stubRetainedResponseReader) ReadRetainedResponse(
	context.Context,
	string,
	string,
) (retainedResponseEvidence, bool, error) {
	r.calls++
	return r.evidence, r.found, r.err
}

func encodeModelPayload(t *testing.T, payload message.Payload) []byte {
	t.Helper()
	data, err := json.Marshal(message.NewBaseMessage(payload.Schema(), payload, "provider-settlement-test"))
	require.NoError(t, err)
	return data
}

func providerSettlementRequest() agentic.AgentRequest {
	return agentic.AgentRequest{
		RequestID: "request-provider-settlement",
		LoopID:    "loop-provider-settlement",
		Role:      "general",
		Model:     "test-model",
		Messages:  []agentic.ChatMessage{{Role: "user", Content: "test"}},
	}
}

func providerSettlementComponent(t *testing.T, reader retainedResponseEvidenceReader) *Component {
	t.Helper()
	return &Component{
		config:           DefaultConfig(),
		decoder:          payloadbuiltins.NewTestDecoder(t),
		responseEvidence: reader,
	}
}

// spec: agentic-model / Model request settlement is bound to a durable response
func TestMatchingRetainedResponseAcknowledgesWithoutProviderWork(t *testing.T) {
	req := providerSettlementRequest()
	response := agentic.AgentResponse{
		RequestID: req.RequestID,
		Status:    agentic.StatusComplete,
		Message:   agentic.ChatMessage{Role: "assistant", Content: "retained"},
	}
	reader := &stubRetainedResponseReader{
		evidence: retainedResponseEvidence{
			subject: "agent.response." + req.RequestID,
			data:    encodeModelPayload(t, &response),
		},
		found: true,
	}
	c := providerSettlementComponent(t, reader)

	decision, err := c.handleRequest(t.Context(), encodeModelPayload(t, &req))

	require.NoError(t, err)
	require.Equal(t, natsclient.DeliveryDecisionAck, decision)
	require.Equal(t, 1, reader.calls)
}

// spec: agentic-model / Model request settlement is bound to a durable response
func TestRetainedResponseCorrelationConflictQuarantinesBeforeProviderWork(t *testing.T) {
	req := providerSettlementRequest()
	tests := []struct {
		name     string
		subject  string
		response agentic.AgentResponse
	}{
		{
			name:    "subject RequestID disagrees",
			subject: "agent.response.different-request",
			response: agentic.AgentResponse{
				RequestID: req.RequestID,
				Status:    agentic.StatusComplete,
			},
		},
		{
			name:    "payload RequestID disagrees",
			subject: "agent.response." + req.RequestID,
			response: agentic.AgentResponse{
				RequestID: "different-request",
				Status:    agentic.StatusComplete,
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			reader := &stubRetainedResponseReader{
				evidence: retainedResponseEvidence{
					subject: tt.subject,
					data:    encodeModelPayload(t, &tt.response),
				},
				found: true,
			}
			c := providerSettlementComponent(t, reader)

			decision, err := c.handleRequest(t.Context(), encodeModelPayload(t, &req))

			require.Error(t, err)
			require.True(t, errs.IsFatal(err))
			require.Contains(t, err.Error(), "response correlation conflict")
			require.Equal(t, natsclient.DeliveryDecisionQuarantine, decision)
			require.Equal(t, 1, reader.calls)
		})
	}
}

// spec: agentic-model / Model request settlement is bound to a durable response
func TestRetainedResponseLookupFailureRetriesBeforeProviderWork(t *testing.T) {
	req := providerSettlementRequest()
	lookupErr := errors.New("retained response unavailable")
	reader := &stubRetainedResponseReader{err: lookupErr}
	c := providerSettlementComponent(t, reader)

	decision, err := c.handleRequest(t.Context(), encodeModelPayload(t, &req))

	require.ErrorIs(t, err, lookupErr)
	require.True(t, errs.IsTransient(err))
	require.Equal(t, natsclient.DeliveryDecisionRetry, decision)
	require.Equal(t, 1, reader.calls)
}

// spec: agentic-model / Model request settlement is bound to a durable response
func TestTypedRetainedResponseAbsencePermitsProviderPath(t *testing.T) {
	req := providerSettlementRequest()
	reader := &stubRetainedResponseReader{}
	c := providerSettlementComponent(t, reader)

	_, found, err := c.readRetainedAgentResponse(t.Context(), req.RequestID)

	require.NoError(t, err)
	require.False(t, found)
	require.Equal(t, 1, reader.calls)
}

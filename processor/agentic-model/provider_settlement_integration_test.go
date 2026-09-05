//go:build integration

package agenticmodel

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"github.com/c360studio/semstreams/agentic"
	"github.com/c360studio/semstreams/component"
	"github.com/c360studio/semstreams/model"
	"github.com/c360studio/semstreams/natsclient"
	"github.com/c360studio/semstreams/payloadbuiltins"
	"github.com/nats-io/nats.go/jetstream"
	"github.com/stretchr/testify/require"
)

const (
	providerSettlementRequestStream  = "MODEL_PROVIDER_REQUEST"
	providerSettlementResponseStream = "MODEL_PROVIDER_RESPONSE"
)

func newProviderSettlementNATS(t *testing.T) *natsclient.TestClient {
	t.Helper()
	return natsclient.NewTestClient(t, natsclient.WithJetStream(), natsclient.WithStreams(
		natsclient.TestStreamConfig{Name: providerSettlementRequestStream, Subjects: []string{"agent.request.>"}},
		natsclient.TestStreamConfig{Name: providerSettlementResponseStream, Subjects: []string{"agent.response.>"}},
	))
}

func newProviderSettlementComponent(
	t *testing.T,
	tc *natsclient.TestClient,
	providerURL string,
	suffix string,
) *Component {
	t.Helper()
	config := Config{
		Ports: &component.PortConfig{
			Inputs: []component.PortDefinition{{
				Name: "agent.request",
				Config: component.JetStreamPort{
					StreamName: providerSettlementRequestStream,
					Subjects:   []string{"agent.request.>"},
				},
				Required: true,
			}},
			Outputs: []component.PortDefinition{{
				Name: "agent.response",
				Config: component.JetStreamPort{
					StreamName: providerSettlementResponseStream,
					Subjects:   []string{"agent.response.*"},
				},
				Required: true,
			}},
		},
		ConsumerNameSuffix: suffix,
		Timeout:            "5s",
		Retry: RetryConfig{
			MaxAttempts:         1,
			MaxRateLimitRetries: 1,
			Backoff:             "linear",
		},
	}
	encoded, err := json.Marshal(config)
	require.NoError(t, err)
	discoverable, err := NewComponent(encoded, component.Dependencies{
		NATSClient: tc.Client,
		ModelRegistry: &model.Registry{Endpoints: map[string]*model.EndpointConfig{
			"test-model": {
				URL:         providerURL,
				Model:       "test-model",
				MaxTokens:   1024,
				WireBackend: "wire",
			},
		}},
		PayloadRegistry: payloadbuiltins.NewTestRegistry(t),
	})
	require.NoError(t, err)
	return discoverable.(*Component)
}

func successfulProvider(calls *atomic.Int32) *httptest.Server {
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		calls.Add(1)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{
			"id":"provider-response","object":"chat.completion",
			"choices":[{"index":0,"message":{"role":"assistant","content":"done"},"finish_reason":"stop"}],
			"usage":{"prompt_tokens":3,"completion_tokens":1}
		}`))
	}))
}

func publishProviderSettlementRequest(
	t *testing.T,
	tc *natsclient.TestClient,
	requestID string,
) (*jetstream.PubAck, agentic.AgentRequest) {
	t.Helper()
	req := agentic.AgentRequest{
		RequestID: requestID,
		LoopID:    "loop-" + requestID,
		Role:      "general",
		Model:     "test-model",
		Messages:  []agentic.ChatMessage{{Role: "user", Content: "test"}},
	}
	ack, err := tc.Client.PublishToStreamWithAck(
		t.Context(), "agent.request."+requestID, encodeModelPayload(t, &req),
	)
	require.NoError(t, err)
	return ack, req
}

func requireProviderSourceAck(
	t *testing.T,
	tc *natsclient.TestClient,
	consumerName string,
	sequence uint64,
) {
	t.Helper()
	stream, err := tc.Client.GetStream(t.Context(), providerSettlementRequestStream)
	require.NoError(t, err)
	consumer, err := stream.Consumer(t.Context(), consumerName)
	require.NoError(t, err)
	require.Eventually(t, func() bool {
		info, infoErr := consumer.Info(t.Context())
		return infoErr == nil && info.AckFloor.Stream >= sequence && info.NumAckPending == 0
	}, 5*time.Second, 10*time.Millisecond)
}

func requireProviderSourceUnacked(
	t *testing.T,
	tc *natsclient.TestClient,
	consumerName string,
	sequence uint64,
) {
	t.Helper()
	stream, err := tc.Client.GetStream(t.Context(), providerSettlementRequestStream)
	require.NoError(t, err)
	consumer, err := stream.Consumer(t.Context(), consumerName)
	require.NoError(t, err)
	info, err := consumer.Info(t.Context())
	require.NoError(t, err)
	require.Less(t, info.AckFloor.Stream, sequence)
}

type blockingRetainedResponseReader struct {
	entered chan struct{}
	release chan struct{}
}

func (r blockingRetainedResponseReader) ReadRetainedResponse(
	context.Context,
	string,
	string,
) (retainedResponseEvidence, bool, error) {
	close(r.entered)
	<-r.release
	return retainedResponseEvidence{}, false, errors.New("process replaced before retained lookup completed")
}

// spec: agentic-model / Model request settlement is bound to a durable response
func TestIntegrationMatchingRetainedResponseSkipsProviderAndAcknowledgesSource(t *testing.T) {
	tc := newProviderSettlementNATS(t)
	var calls atomic.Int32
	provider := successfulProvider(&calls)
	defer provider.Close()

	const requestID = "matching-retained"
	retained := agentic.AgentResponse{
		RequestID: requestID,
		Status:    agentic.StatusComplete,
		Message:   agentic.ChatMessage{Role: "assistant", Content: "already committed"},
	}
	require.NoError(t, tc.Client.PublishToStream(
		t.Context(), "agent.response."+requestID, encodeModelPayload(t, &retained),
	))

	component := newProviderSettlementComponent(t, tc, provider.URL, "matching")
	require.NoError(t, component.Start(t.Context()))
	t.Cleanup(func() { require.NoError(t, component.Stop(context.Background())) })
	ack, _ := publishProviderSettlementRequest(t, tc, requestID)

	requireProviderSourceAck(t, tc, "agentic-model-agent-request-all-matching", ack.Sequence)
	require.Zero(t, calls.Load())
}

// spec: agentic-model / Model response publication is durably at-least-once
func TestIntegrationTypedAbsenceInvokesProviderAndPubAckPrecedesSourceAck(t *testing.T) {
	tc := newProviderSettlementNATS(t)
	var calls atomic.Int32
	provider := successfulProvider(&calls)
	defer provider.Close()

	component := newProviderSettlementComponent(t, tc, provider.URL, "absence")
	require.NoError(t, component.Start(t.Context()))
	t.Cleanup(func() { require.NoError(t, component.Stop(context.Background())) })
	ack, req := publishProviderSettlementRequest(t, tc, "typed-absence")

	responseStream, err := tc.Client.GetStream(t.Context(), providerSettlementResponseStream)
	require.NoError(t, err)
	require.Eventually(t, func() bool {
		raw, readErr := responseStream.GetLastMsgForSubject(t.Context(), "agent.response."+req.RequestID)
		return readErr == nil && raw.Subject == "agent.response."+req.RequestID
	}, 5*time.Second, 10*time.Millisecond)
	requireProviderSourceAck(t, tc, "agentic-model-agent-request-all-absence", ack.Sequence)
	require.Equal(t, int32(1), calls.Load())
}

// spec: agentic-model / Model response publication is durably at-least-once
func TestIntegrationProviderErrorPubAckPrecedesSourceAck(t *testing.T) {
	tc := newProviderSettlementNATS(t)
	var calls atomic.Int32
	provider := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		calls.Add(1)
		http.Error(w, "provider unavailable", http.StatusInternalServerError)
	}))
	defer provider.Close()

	component := newProviderSettlementComponent(t, tc, provider.URL, "provider-error")
	require.NoError(t, component.Start(t.Context()))
	t.Cleanup(func() { require.NoError(t, component.Stop(context.Background())) })
	ack, req := publishProviderSettlementRequest(t, tc, "provider-error")

	responseStream, err := tc.Client.GetStream(t.Context(), providerSettlementResponseStream)
	require.NoError(t, err)
	var response agentic.AgentResponse
	decoder := payloadbuiltins.NewTestDecoder(t)
	require.Eventually(t, func() bool {
		raw, readErr := responseStream.GetLastMsgForSubject(t.Context(), "agent.response."+req.RequestID)
		if readErr != nil {
			return false
		}
		decoded, decodeErr := decoder.Decode(raw.Data)
		if decodeErr != nil {
			return false
		}
		payload, ok := decoded.Payload().(*agentic.AgentResponse)
		if !ok {
			return false
		}
		response = *payload
		return true
	}, 5*time.Second, 10*time.Millisecond)
	require.Equal(t, agentic.StatusError, response.Status)
	require.Equal(t, req.RequestID, response.RequestID)
	requireProviderSourceAck(t, tc, "agentic-model-agent-request-all-provider-error", ack.Sequence)
	require.Equal(t, int32(1), calls.Load())
}

// spec: agentic-model / Model request settlement is bound to a durable response
func TestIntegrationRetainedResponseRequestIDConflictQuarantinesWithoutProvider(t *testing.T) {
	tc := newProviderSettlementNATS(t)
	var calls atomic.Int32
	provider := successfulProvider(&calls)
	defer provider.Close()

	const requestID = "correlation-conflict"
	conflict := agentic.AgentResponse{RequestID: "other-request", Status: agentic.StatusComplete}
	require.NoError(t, tc.Client.PublishToStream(
		t.Context(), "agent.response."+requestID, encodeModelPayload(t, &conflict),
	))

	component := newProviderSettlementComponent(t, tc, provider.URL, "conflict")
	require.NoError(t, component.Start(t.Context()))
	t.Cleanup(func() { require.NoError(t, component.Stop(context.Background())) })
	ack, _ := publishProviderSettlementRequest(t, tc, requestID)

	require.Eventually(t, func() bool {
		health := component.Health()
		return !health.Healthy && health.Status == "delivery ownership lost" &&
			health.LastError != "" && health.ErrorCount == 1
	}, 5*time.Second, 10*time.Millisecond)
	requireProviderSourceUnacked(t, tc, "agentic-model-agent-request-all-conflict", ack.Sequence)
	require.Zero(t, calls.Load())
}

// spec: agentic-model / Model request settlement is bound to a durable response
func TestIntegrationRetainedResponseLookupFailureRetriesWithoutProvider(t *testing.T) {
	tc := newProviderSettlementNATS(t)
	var calls atomic.Int32
	provider := successfulProvider(&calls)
	defer provider.Close()

	component := newProviderSettlementComponent(t, tc, provider.URL, "lookup-failure")
	require.NoError(t, component.Start(t.Context()))
	t.Cleanup(func() { require.NoError(t, component.Stop(context.Background())) })
	js, err := tc.Client.JetStream()
	require.NoError(t, err)
	require.NoError(t, js.DeleteStream(t.Context(), providerSettlementResponseStream))
	ack, _ := publishProviderSettlementRequest(t, tc, "lookup-failure")

	require.Eventually(t, func() bool {
		return component.Health().ErrorCount > 0
	}, 5*time.Second, 10*time.Millisecond)
	requireProviderSourceUnacked(t, tc, "agentic-model-agent-request-all-lookup-failure", ack.Sequence)
	require.Zero(t, calls.Load())
}

// spec: agentic-model / Model request settlement is bound to a durable response
// spec: agentic-model / Started markers do not claim invocation certainty
func TestIntegrationPreProviderReplacementSeesAbsenceAndInvokesOnce(t *testing.T) {
	tc := newProviderSettlementNATS(t)
	var calls atomic.Int32
	provider := successfulProvider(&calls)
	defer provider.Close()

	entered := make(chan struct{})
	release := make(chan struct{})
	first := newProviderSettlementComponent(t, tc, provider.URL, "pre-provider-replacement")
	first.responseEvidence = blockingRetainedResponseReader{entered: entered, release: release}
	drainIssued := make(chan struct{})
	first.waitConsumerClosed = func(ctx context.Context, closed <-chan struct{}) error {
		close(drainIssued)
		select {
		case <-closed:
			return nil
		case <-ctx.Done():
			return ctx.Err()
		}
	}
	require.NoError(t, first.Start(t.Context()))
	ack, req := publishProviderSettlementRequest(t, tc, "pre-provider-replacement")
	select {
	case <-entered:
	case <-time.After(5 * time.Second):
		t.Fatal("first process did not enter retained-response lookup")
	}

	stopDone := make(chan error, 1)
	go func() { stopDone <- first.Stop(t.Context()) }()
	select {
	case <-drainIssued:
	case <-time.After(5 * time.Second):
		t.Fatal("first delivery owner did not enter drain before lookup release")
	}
	close(release)
	select {
	case stopErr := <-stopDone:
		require.NoError(t, stopErr)
	case <-time.After(5 * time.Second):
		t.Fatal("first component did not stop after lookup release")
	}
	require.Zero(t, calls.Load())
	requireProviderSourceUnacked(
		t, tc, "agentic-model-agent-request-all-pre-provider-replacement", ack.Sequence,
	)

	replacement := newProviderSettlementComponent(t, tc, provider.URL, "pre-provider-replacement")
	require.NoError(t, replacement.Start(t.Context()))
	t.Cleanup(func() { require.NoError(t, replacement.Stop(context.Background())) })
	responseStream, err := tc.Client.GetStream(t.Context(), providerSettlementResponseStream)
	require.NoError(t, err)
	decoder := payloadbuiltins.NewTestDecoder(t)
	require.Eventually(t, func() bool {
		raw, readErr := responseStream.GetLastMsgForSubject(t.Context(), "agent.response."+req.RequestID)
		if readErr != nil {
			return false
		}
		decoded, decodeErr := decoder.Decode(raw.Data)
		if decodeErr != nil {
			return false
		}
		response, ok := decoded.Payload().(*agentic.AgentResponse)
		return ok && response.RequestID == req.RequestID
	}, 5*time.Second, 10*time.Millisecond)
	requireProviderSourceAck(
		t, tc, "agentic-model-agent-request-all-pre-provider-replacement", ack.Sequence,
	)
	require.Equal(t, int32(1), calls.Load())
}

// spec: agentic-model / Model response publication is durably at-least-once
// spec: agentic-model / Started markers do not claim invocation certainty
func TestIntegrationPostProviderPrePubAckReplacementMayInvokeAgain(t *testing.T) {
	tc := newProviderSettlementNATS(t)
	var calls atomic.Int32
	entered := make(chan struct{}, 2)
	release := make(chan struct{})
	provider := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		call := calls.Add(1)
		entered <- struct{}{}
		if call == 1 {
			<-release
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = fmt.Fprint(w, `{"id":"replacement","object":"chat.completion","choices":[{"index":0,"message":{"role":"assistant","content":"done"},"finish_reason":"stop"}]}`)
	}))
	defer provider.Close()

	first := newProviderSettlementComponent(t, tc, provider.URL, "replacement")
	drainIssued := make(chan struct{})
	first.waitConsumerClosed = func(ctx context.Context, closed <-chan struct{}) error {
		close(drainIssued)
		select {
		case <-closed:
			return nil
		case <-ctx.Done():
			return ctx.Err()
		}
	}
	require.NoError(t, first.Start(t.Context()))
	ack, req := publishProviderSettlementRequest(t, tc, "post-return-pre-puback")
	select {
	case <-entered:
	case <-time.After(5 * time.Second):
		t.Fatal("provider was not invoked")
	}
	js, err := tc.Client.JetStream()
	require.NoError(t, err)
	require.NoError(t, js.DeleteStream(t.Context(), providerSettlementResponseStream))
	stopDone := make(chan error, 1)
	go func() { stopDone <- first.Stop(t.Context()) }()
	select {
	case <-drainIssued:
	case <-time.After(5 * time.Second):
		t.Fatal("first delivery owner did not enter drain before provider release")
	}
	close(release)
	select {
	case stopErr := <-stopDone:
		require.NoError(t, stopErr)
	case <-time.After(5 * time.Second):
		t.Fatal("first component did not stop after provider release")
	}
	requireProviderSourceUnacked(t, tc, "agentic-model-agent-request-all-replacement", ack.Sequence)

	_, err = js.CreateStream(t.Context(), jetstream.StreamConfig{
		Name:     providerSettlementResponseStream,
		Subjects: []string{"agent.response.>"},
	})
	require.NoError(t, err)
	replacement := newProviderSettlementComponent(t, tc, provider.URL, "replacement")
	require.NoError(t, replacement.Start(t.Context()))
	t.Cleanup(func() { require.NoError(t, replacement.Stop(context.Background())) })

	select {
	case <-entered:
	case <-time.After(5 * time.Second):
		t.Fatal("replacement provider invocation did not occur")
	}
	responseStream, err := tc.Client.GetStream(t.Context(), providerSettlementResponseStream)
	require.NoError(t, err)
	require.Eventually(t, func() bool {
		_, readErr := responseStream.GetLastMsgForSubject(t.Context(), "agent.response."+req.RequestID)
		return readErr == nil
	}, 5*time.Second, 10*time.Millisecond)
	requireProviderSourceAck(t, tc, "agentic-model-agent-request-all-replacement", ack.Sequence)
	require.Equal(t, int32(2), calls.Load())
}

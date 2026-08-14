package service

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/c360studio/semstreams/agentic"
	"github.com/c360studio/semstreams/message"
	"github.com/c360studio/semstreams/natsclient"
	"github.com/c360studio/semstreams/payloadregistry"
	"github.com/stretchr/testify/require"
)

func TestProductionMessageLoggerObservesConcreteTypedUserResponse(t *testing.T) {
	registry := payloadregistry.New()
	require.NoError(t, agentic.RegisterPayloads(registry))
	created, err := NewMessageLoggerService(json.RawMessage(`{
		"monitor_subjects":["user.response.>"],
		"max_entries":10,
		"sample_rate":1
	}`), &Dependencies{
		NATSClient:      &natsclient.Client{},
		PayloadRegistry: registry,
	})
	require.NoError(t, err)
	logger := created.(*MessageLogger)

	response := &agentic.UserResponse{
		ResponseID: "response-1", ChannelType: "cli", ChannelID: "channel-1",
		Type: agentic.ResponseTypeText, Content: "observed", Timestamp: time.Now().UTC(),
	}
	wire, err := json.Marshal(message.NewBaseMessage(response.Schema(), response, "typed-response-test"))
	require.NoError(t, err)

	decoded, err := logger.decodeBaseMessage(wire)
	require.NoError(t, err)
	require.Equal(t, "agentic.user_response.v1", decoded.Type().String())
	require.IsType(t, &agentic.UserResponse{}, decoded.Payload())

	logger.handleMessage(context.Background(), "user.response.cli.channel-1", wire)
	entries := logger.GetLogEntries(1)
	require.Len(t, entries, 1)
	require.Equal(t, "agentic.user_response.v1", entries[0].MessageType)
	require.Contains(t, entries[0].Summary, "*agentic.UserResponse")
	// Message-logger stores a typed diagnostic observation. It does not ACK a
	// delivery request or invoke an external channel adapter.
}

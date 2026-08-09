package websocket

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/c360studio/semstreams/component"
	"github.com/c360studio/semstreams/natsclient"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestCreateInputDecodesPartialOverrideIntoDefaults(t *testing.T) {
	rawConfig := json.RawMessage(`{"server":{"http_port":9191}}`)

	discoverable, err := CreateInput(rawConfig, component.Dependencies{NATSClient: &natsclient.Client{}})
	require.NoError(t, err)
	input := discoverable.(*Input)

	assert.Equal(t, ModeServer, input.config.Mode)
	require.NotNil(t, input.config.ServerConfig)
	assert.Equal(t, 9191, input.config.ServerConfig.HTTPPort)
	assert.Equal(t, "/", input.config.ServerConfig.Path)
	assert.Equal(t, 100, input.config.ServerConfig.MaxConnections)
	require.NotNil(t, input.config.Backpressure)
	assert.Equal(t, 1000, input.config.Backpressure.QueueSize)
	require.NotNil(t, input.config.ClientConfig)
	require.NotNil(t, input.config.ClientConfig.Reconnect)
	assert.Equal(t, time.Second, input.config.ClientConfig.Reconnect.InitialInterval)
	assert.Equal(t, 60*time.Second, input.config.ClientConfig.Reconnect.MaxInterval)
	require.NotNil(t, input.config.Bidirectional)
	assert.Equal(t, 5*time.Second, input.config.Bidirectional.RequestTimeout)
	require.Len(t, input.OutputPorts(), 2)
}

func TestCreateInputDecodesDocumentedDurationStrings(t *testing.T) {
	rawConfig := json.RawMessage(`{
		"mode":"client",
		"client":{"reconnect":{"initial_interval":"2s","max_interval":"90s"}},
		"bidirectional":{"request_timeout":"7s"}
	}`)

	discoverable, err := CreateInput(rawConfig, component.Dependencies{NATSClient: &natsclient.Client{}})
	require.NoError(t, err)
	input := discoverable.(*Input)

	require.NotNil(t, input.config.ClientConfig)
	require.NotNil(t, input.config.ClientConfig.Reconnect)
	assert.Equal(t, 2*time.Second, input.config.ClientConfig.Reconnect.InitialInterval)
	assert.Equal(t, 90*time.Second, input.config.ClientConfig.Reconnect.MaxInterval)
	require.NotNil(t, input.config.Bidirectional)
	assert.Equal(t, 7*time.Second, input.config.Bidirectional.RequestTimeout)
}

func TestCreateInputRetainsNumericGoDurationJSON(t *testing.T) {
	rawConfig := json.RawMessage(`{
		"client":{"reconnect":{"initial_interval":2000000000,"max_interval":90000000000}},
		"bidirectional":{"request_timeout":7000000000}
	}`)

	discoverable, err := CreateInput(rawConfig, component.Dependencies{NATSClient: &natsclient.Client{}})
	require.NoError(t, err)
	input := discoverable.(*Input)

	assert.Equal(t, 2*time.Second, input.config.ClientConfig.Reconnect.InitialInterval)
	assert.Equal(t, 90*time.Second, input.config.ClientConfig.Reconnect.MaxInterval)
	assert.Equal(t, 7*time.Second, input.config.Bidirectional.RequestTimeout)
}

func TestCreateInputRejectsInvalidDurationStrings(t *testing.T) {
	tests := []struct {
		name  string
		raw   json.RawMessage
		field string
	}{
		{
			name:  "initial interval",
			raw:   json.RawMessage(`{"client":{"reconnect":{"initial_interval":"soon"}}}`),
			field: "initial_interval",
		},
		{
			name:  "maximum interval",
			raw:   json.RawMessage(`{"client":{"reconnect":{"max_interval":"later"}}}`),
			field: "max_interval",
		},
		{
			name:  "request timeout",
			raw:   json.RawMessage(`{"bidirectional":{"request_timeout":"eventually"}}`),
			field: "request_timeout",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			_, err := CreateInput(test.raw, component.Dependencies{NATSClient: &natsclient.Client{}})
			require.Error(t, err)
			assert.ErrorContains(t, err, test.field)
		})
	}
}

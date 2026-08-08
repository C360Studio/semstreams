package websocket

import (
	"encoding/json"
	"testing"

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
	require.Len(t, input.OutputPorts(), 2)
}

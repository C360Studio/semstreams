package natsclient

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestClientMaxPayloadRequiresActiveConnection(t *testing.T) {
	client, err := NewClient("nats://localhost:4222")
	require.NoError(t, err)

	maxPayload, err := client.MaxPayload()
	require.Zero(t, maxPayload)
	require.ErrorIs(t, err, ErrNotConnected)
}

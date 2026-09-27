package boot

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"testing"
	"time"

	natsserver "github.com/nats-io/nats-server/v2/server"
	"github.com/stretchr/testify/require"

	"github.com/c360studio/semstreams/natsclient"
)

// TestSlowConsumerHookRunsBetweenConnectionAndConfigArbitration pins where an
// after-connect extension (the slow-consumer probe, internal/e2eslowconsumer)
// runs: inside connectNATSWithSpinner, once the client is connected, which is
// before config arbitration — internal/maxdelivery's boot-order guard pins
// connectNATSWithSpinner ahead of StartValidatedConfigManager in Run. That is
// the only window in which the probe can observe the connection callback. A
// failing extension fails the connection step.
func TestSlowConsumerHookRunsBetweenConnectionAndConfigArbitration(t *testing.T) {
	server, err := natsserver.NewServer(&natsserver.Options{Port: -1, NoLog: true, NoSigs: true})
	require.NoError(t, err)
	go server.Start()
	require.True(t, server.ReadyForConnections(5*time.Second))
	t.Cleanup(server.Shutdown)

	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	client, err := natsclient.NewClient(server.ClientURL(), natsclient.WithLogger(logger))
	require.NoError(t, err)
	t.Cleanup(func() { _ = client.Close(context.Background()) })

	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	defer cancel()
	var connectedAtHook bool
	wantErr := errors.New("probe failed")
	err = connectNATSWithSpinner(ctx, client, logger, []func(context.Context, *natsclient.Client) error{
		func(_ context.Context, got *natsclient.Client) error {
			connectedAtHook = got == client && got.GetConnection() != nil && got.GetConnection().IsConnected()
			return wantErr
		},
	})

	require.True(t, connectedAtHook, "the after-connect extension did not see the connected client")
	require.ErrorIs(t, err, wantErr)
}

package main

import (
	"testing"
	"time"

	"github.com/c360studio/semstreams/natsclient"
	natsserver "github.com/nats-io/nats-server/v2/server"
	"github.com/nats-io/nats.go"
	"github.com/stretchr/testify/require"
)

func TestRootResourcesAbortClosesPublishedNATSClientOnce(t *testing.T) {
	server, err := natsserver.NewServer(&natsserver.Options{Port: -1, NoLog: true, NoSigs: true})
	require.NoError(t, err)
	server.Start()
	t.Cleanup(func() {
		server.Shutdown()
		server.WaitForShutdown()
	})
	connection, err := nats.Connect("", nats.InProcessServer(server))
	require.NoError(t, err)
	t.Cleanup(connection.Close)

	client, err := natsclient.NewClient("nats://unused")
	require.NoError(t, err)
	client.SetConnection(connection)
	resources := &semstreamsRootResources{natsClient: client}
	bootErr := error(nil)

	resources.abortOnReturn(time.Second, &bootErr)

	require.NoError(t, bootErr)
	require.True(t, resources.closeAttempted)
	require.True(t, connection.IsClosed())
	require.Nil(t, client.GetConnection())

	resources.abortOnReturn(time.Second, &bootErr)
	require.NoError(t, bootErr)
}

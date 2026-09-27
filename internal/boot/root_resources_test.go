package boot

import (
	"io"
	"testing"
	"time"

	"github.com/c360studio/semstreams/natsclient"
	natsserver "github.com/nats-io/nats-server/v2/server"
	"github.com/nats-io/nats.go"
	"github.com/stretchr/testify/require"
)

// inProcessClient is a natsclient bound to a live in-process server, so
// rootResources.close has a real transport to close.
func inProcessClient(t *testing.T) (*natsclient.Client, *nats.Conn) {
	t.Helper()
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
	return client, connection
}

func TestRootResourcesAbortClosesPublishedNATSClientOnce(t *testing.T) {
	client, connection := inProcessClient(t)
	resources := &rootResources{natsClient: client}
	bootErr := error(nil)

	resources.abortOnReturn(time.Second, &bootErr)

	require.NoError(t, bootErr)
	require.True(t, resources.closeAttempted)
	require.True(t, connection.IsClosed())
	require.Nil(t, client.GetConnection())

	resources.abortOnReturn(time.Second, &bootErr)
	require.NoError(t, bootErr)
}

type countingCloser struct{ calls int }

func (c *countingCloser) Close() error {
	c.calls++
	return nil
}

// A responder extension's closer is owned by the root: the bounded abort
// closes it exactly once, before the transport, and a second abort is a no-op.
func TestRootResourcesAbortClosesRespondersOnce(t *testing.T) {
	client, _ := inProcessClient(t)
	responder := &countingCloser{}
	resources := &rootResources{natsClient: client, responders: []io.Closer{responder}}
	bootErr := error(nil)

	resources.abortOnReturn(time.Second, &bootErr)

	require.NoError(t, bootErr)
	require.Equal(t, 1, responder.calls)

	resources.abortOnReturn(time.Second, &bootErr)
	require.NoError(t, bootErr)
	require.Equal(t, 1, responder.calls)
}

package natsclient

import (
	"context"
	"sync"
	"testing"

	natsserver "github.com/nats-io/nats-server/v2/server"
	"github.com/nats-io/nats.go"
	"github.com/stretchr/testify/require"
)

func TestConnectRejectsCandidateWhenContextCanceledDuringDial(t *testing.T) {
	client, err := NewClient("nats://unused")
	require.NoError(t, err)
	client.healthInterval = 0

	candidate := newConnectCandidate(t)
	dialEntered := make(chan struct{})
	releaseDial := make(chan struct{})
	ctx, cancel := context.WithCancel(context.Background())

	var connectWG sync.WaitGroup
	connectWG.Add(1)
	connectErr := make(chan error)
	go func() {
		defer connectWG.Done()
		connectErr <- client.connectWith(ctx, func(_ string, _ ...nats.Option) (*nats.Conn, error) {
			close(dialEntered)
			<-releaseDial
			return candidate, nil
		})
	}()

	<-dialEntered
	cancel()
	close(releaseDial)
	err = <-connectErr
	connectWG.Wait()

	require.ErrorIs(t, err, context.Canceled)
	require.True(t, candidate.IsClosed(), "canceled Connect must close its rejected native candidate")
	require.Nil(t, client.GetConnection())
	require.Nil(t, client.js)
	require.Equal(t, StatusDisconnected, client.Status())
}

func TestConnectRejectsCandidateWhenCloseWinsAdmission(t *testing.T) {
	client, err := NewClient("nats://unused")
	require.NoError(t, err)
	client.healthInterval = 0

	candidate := newConnectCandidate(t)
	dialEntered := make(chan struct{})
	releaseDial := make(chan struct{})

	var connectWG sync.WaitGroup
	connectWG.Add(1)
	connectErr := make(chan error)
	go func() {
		defer connectWG.Done()
		connectErr <- client.connectWith(context.Background(), func(_ string, _ ...nats.Option) (*nats.Conn, error) {
			close(dialEntered)
			<-releaseDial
			return candidate, nil
		})
	}()

	<-dialEntered
	require.NoError(t, client.Close(context.Background()))
	close(releaseDial)
	err = <-connectErr
	connectWG.Wait()

	require.ErrorIs(t, err, nats.ErrConnectionClosed)
	require.True(t, candidate.IsClosed(), "Connect must close a candidate rejected by terminal Close")
	require.Nil(t, client.GetConnection())
	require.Nil(t, client.js)
	require.Equal(t, StatusDisconnected, client.Status())
}

func TestConnectCommitsCandidateWhenAdmissionRemainsOpen(t *testing.T) {
	client, err := NewClient("nats://unused")
	require.NoError(t, err)
	client.healthInterval = 0

	candidate := newConnectCandidate(t)
	err = client.connectWith(context.Background(), func(_ string, _ ...nats.Option) (*nats.Conn, error) {
		return candidate, nil
	})
	require.NoError(t, err)

	require.Same(t, candidate, client.GetConnection())
	require.NotNil(t, client.js)
	require.False(t, candidate.IsClosed())
	require.Equal(t, StatusConnected, client.Status())

	require.NoError(t, client.Close(context.Background()))
}

func newConnectCandidate(t *testing.T) *nats.Conn {
	t.Helper()

	server, err := natsserver.NewServer(&natsserver.Options{
		Port:   -1,
		NoLog:  true,
		NoSigs: true,
	})
	require.NoError(t, err)
	server.Start()
	t.Cleanup(func() {
		server.Shutdown()
		server.WaitForShutdown()
	})

	candidate, err := nats.Connect("", nats.InProcessServer(server))
	require.NoError(t, err)
	t.Cleanup(candidate.Close)
	return candidate
}

package metric

import (
	"net"
	"strconv"
	"sync"
	"testing"
	"time"

	"github.com/c360studio/semstreams/pkg/security"
	"github.com/stretchr/testify/require"
)

type blockingCloseListener struct {
	net.Listener
	entered chan struct{}
	release chan struct{}
	once    sync.Once
}

func (l *blockingCloseListener) Close() error {
	l.once.Do(func() { close(l.entered) })
	<-l.release
	return l.Listener.Close()
}

func TestServerStartOwnsListenerAndStopAllowsRestart(t *testing.T) {
	port := freeServerPort(t)
	server := NewServer(port, "/metrics", NewMetricsRegistry(), security.Config{})
	address := net.JoinHostPort("127.0.0.1", strconv.Itoa(port))

	require.NoError(t, server.Start())
	connection, err := net.DialTimeout("tcp", address, 250*time.Millisecond)
	require.NoError(t, err, "Start must return only after the listener is owned")
	require.NoError(t, connection.Close())
	require.NoError(t, server.Stop())

	connection, err = net.DialTimeout("tcp", address, 100*time.Millisecond)
	require.Error(t, err, "Stop must close the listener before returning")
	if connection != nil {
		_ = connection.Close()
	}

	require.NoError(t, server.Start(), "a stopped Server must support a new lifecycle")
	require.NoError(t, server.Stop())
}

func TestServerStartWaitsForConcurrentStopToFinish(t *testing.T) {
	server := NewServer(freeServerPort(t), "/metrics", NewMetricsRegistry(), security.Config{})
	require.NoError(t, server.Start())

	listener := &blockingCloseListener{
		Listener: server.listener,
		entered:  make(chan struct{}),
		release:  make(chan struct{}),
	}
	server.mu.Lock()
	server.listener = listener
	server.mu.Unlock()

	stopResult := make(chan error, 1)
	go func() { stopResult <- server.Stop() }()
	<-listener.entered // Stop now owns server.mu through listener close and Serve join.
	if server.mu.TryLock() {
		server.mu.Unlock()
		t.Fatal("Stop released lifecycle ownership before listener close and Serve join")
	}

	startResult := make(chan error, 1)
	go func() { startResult <- server.Start() }()

	close(listener.release)
	require.NoError(t, <-stopResult)
	require.NoError(t, <-startResult)
	require.NoError(t, server.Stop())
}

func freeServerPort(t *testing.T) int {
	t.Helper()
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	port := listener.Addr().(*net.TCPAddr).Port
	require.NoError(t, listener.Close())
	return port
}

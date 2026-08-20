package metric

import (
	"context"
	"errors"
	"net"
	"net/http"
	"runtime"
	"strconv"
	"sync"
	"testing"
	"time"

	"github.com/c360studio/semstreams/pkg/errs"
	"github.com/c360studio/semstreams/pkg/security"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/stretchr/testify/require"
)

type blockingCollector struct {
	desc    *prometheus.Desc
	entered chan struct{}
	release chan struct{}
	once    sync.Once
}

func (c *blockingCollector) Describe(ch chan<- *prometheus.Desc) { ch <- c.desc }

func (c *blockingCollector) Collect(ch chan<- prometheus.Metric) {
	c.once.Do(func() { close(c.entered) })
	<-c.release
	ch <- prometheus.MustNewConstMetric(c.desc, prometheus.GaugeValue, 1)
}

func newBlockingCollector() *blockingCollector {
	return &blockingCollector{
		desc:    prometheus.NewDesc("semstreams_lifecycle_block", "test", nil, nil),
		entered: make(chan struct{}),
		release: make(chan struct{}),
	}
}

func TestServerStartOwnsListenerAndRequiresFreshInstanceForRestart(t *testing.T) {
	port := freeServerPort(t)
	server := NewServer(port, "/metrics", NewMetricsRegistry(), security.Config{})
	address := net.JoinHostPort("127.0.0.1", strconv.Itoa(port))

	require.NoError(t, server.Start(t.Context()))
	require.Same(t, t.Context(), server.server.BaseContext(server.listener))
	connection, err := net.DialTimeout("tcp", address, 250*time.Millisecond)
	require.NoError(t, err, "Start must return only after the listener is owned")
	require.NoError(t, connection.Close())
	require.NoError(t, server.Stop(t.Context()))

	connection, err = net.DialTimeout("tcp", address, 100*time.Millisecond)
	require.Error(t, err, "Stop must close the listener before returning")
	if connection != nil {
		_ = connection.Close()
	}

	err = server.Start(t.Context())
	require.Error(t, err, "a stopped Server is one-shot")
	require.ErrorIs(t, err, errs.ErrAlreadyStarted)

	replacement := NewServer(port, "/metrics", NewMetricsRegistry(), security.Config{})
	require.NoError(t, replacement.Start(t.Context()), "a fresh Server is the restart boundary")
	require.NoError(t, replacement.Stop(t.Context()))
}

func TestServerConcurrentStopIsTypedTransientAndDoesNotHoldLifecycleLock(t *testing.T) {
	registry := NewMetricsRegistry()
	collector := newBlockingCollector()
	registry.PrometheusRegistry().MustRegister(collector)
	server := NewServer(freeServerPort(t), "/metrics", registry, security.Config{})
	require.NoError(t, server.Start(t.Context()))
	requestDone := make(chan error, 1)
	go func() {
		response, err := http.Get(server.Address())
		if response != nil {
			_ = response.Body.Close()
		}
		requestDone <- err
	}()
	<-collector.entered

	stopResult := make(chan error, 1)
	go func() { stopResult <- server.Stop(t.Context()) }()
	for {
		server.mu.Lock()
		stopping := server.stopping
		server.mu.Unlock()
		if stopping {
			break
		}
		runtime.Gosched()
	}
	if !server.mu.TryLock() {
		t.Fatal("Stop held lifecycle mutex while native shutdown blocked")
	}
	server.mu.Unlock()
	concurrentErr := server.Stop(t.Context())
	require.Error(t, concurrentErr)
	require.True(t, errs.IsTransient(concurrentErr))

	close(collector.release)
	require.NoError(t, <-requestDone)
	require.NoError(t, <-stopResult)
	require.NoError(t, server.Stop(t.Context()), "completed Stop must be nil/no-op")
}

func TestServerRejectsNilContextBeforeLifecycleMutation(t *testing.T) {
	server := NewServer(freeServerPort(t), "/metrics", NewMetricsRegistry(), security.Config{})
	require.Error(t, server.Start(nil))
	require.False(t, server.used)
	require.Error(t, server.Stop(nil))
	require.False(t, server.used)
}

func TestServerStopBeforeStartConsumesOneShotInstance(t *testing.T) {
	server := NewServer(freeServerPort(t), "/metrics", NewMetricsRegistry(), security.Config{})
	require.NoError(t, server.Stop(t.Context()))
	require.ErrorIs(t, server.Start(t.Context()), errs.ErrAlreadyStarted)
	require.NoError(t, server.Stop(t.Context()))
}

func TestServerStopIsCallerBounded(t *testing.T) {
	registry := NewMetricsRegistry()
	collector := newBlockingCollector()
	registry.PrometheusRegistry().MustRegister(collector)
	server := NewServer(freeServerPort(t), "/metrics", registry, security.Config{})
	require.NoError(t, server.Start(t.Context()))
	requestDone := make(chan error, 1)
	go func() {
		response, err := http.Get(server.Address())
		if response != nil {
			_ = response.Body.Close()
		}
		requestDone <- err
	}()
	<-collector.entered
	server.mu.Lock()
	serveDone := server.serveDone
	server.mu.Unlock()

	stopCtx, cancel := context.WithCancel(t.Context())
	cancel()
	err := server.Stop(stopCtx)
	require.ErrorIs(t, err, context.Canceled)
	server.mu.Lock()
	require.Nil(t, server.server, "deadline Stop must clear exact server authority")
	require.Nil(t, server.listener, "deadline Stop must clear exact listener authority")
	require.Nil(t, server.serveDone, "deadline Stop must consume exact Serve completion")
	server.mu.Unlock()
	select {
	case _, ok := <-serveDone:
		require.False(t, ok, "Stop returned without consuming the exact Serve result")
	default:
		t.Fatal("Stop returned before the exact Serve goroutine joined")
	}

	address := net.JoinHostPort("127.0.0.1", strconv.Itoa(server.port))
	connection, dialErr := net.DialTimeout("tcp", address, 100*time.Millisecond)
	require.Error(t, dialErr, "deadline Stop must release the listener before returning")
	if connection != nil {
		_ = connection.Close()
	}
	require.NoError(t, server.Stop(t.Context()), "terminal repeat must not replay the deadline error")

	close(collector.release)
	<-requestDone // Force-close may surface EOF; the admitted request must terminate.
}

func TestServerServesHealthOverRealHTTP(t *testing.T) {
	server := NewServer(freeServerPort(t), "/metrics", NewMetricsRegistry(), security.Config{})
	require.NoError(t, server.Start(t.Context()))
	t.Cleanup(func() { require.NoError(t, server.Stop(context.Background())) })

	response, err := http.Get(server.Address()[:len(server.Address())-len("/metrics")] + "/health")
	require.NoError(t, err)
	require.NoError(t, response.Body.Close())
	require.Equal(t, http.StatusOK, response.StatusCode)

	ended, cancel := context.WithCancel(t.Context())
	cancel()
	err = server.Start(ended)
	require.Error(t, err)
	require.True(t, errors.Is(err, context.Canceled) || errors.Is(err, errs.ErrAlreadyStarted))
}

func freeServerPort(t *testing.T) int {
	t.Helper()
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	port := listener.Addr().(*net.TCPAddr).Port
	require.NoError(t, listener.Close())
	return port
}

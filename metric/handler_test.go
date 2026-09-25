package metric

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"encoding/pem"
	"errors"
	"math/big"
	"net"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"sync"
	"sync/atomic"
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

type observedTCPListener struct {
	net.Listener
	closed   atomic.Bool
	accepted atomic.Int32
}

type scopedTCPListener struct {
	net.Listener
	addr *net.TCPAddr
}

func (l *scopedTCPListener) Addr() net.Addr { return l.addr }

func (l *observedTCPListener) Accept() (net.Conn, error) {
	connection, err := l.Listener.Accept()
	if err == nil {
		l.accepted.Add(1)
	}
	return connection, err
}

func (l *observedTCPListener) Close() error {
	l.closed.Store(true)
	return l.Listener.Close()
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

func testServerGET(t *testing.T, address string, client *http.Client) (*http.Response, error) {
	t.Helper()
	request, err := http.NewRequestWithContext(t.Context(), http.MethodGet, address, nil)
	if err != nil {
		return nil, err
	}
	if client == nil {
		client = &http.Client{Timeout: 2 * time.Second}
	}
	return client.Do(request)
}

func registerServerCleanup(t *testing.T, server *Server) {
	t.Helper()
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		require.NoError(t, server.Stop(ctx))
	})
}

func TestServerNativeStartReportsOwnedEphemeralListener(t *testing.T) {
	server := NewServer(9090, "/metrics", NewMetricsRegistry(), security.Config{})
	server.port = 0 // Exercise native acquisition without changing NewServer's public zero default.
	require.NoError(t, server.Start(t.Context()))
	registerServerCleanup(t, server)
	owned := server.listener
	require.NotNil(t, owned)
	wantAddress := "http://" + net.JoinHostPort("localhost", strconv.Itoa(owned.Addr().(*net.TCPAddr).Port)) + "/metrics"
	require.Equal(t, wantAddress, server.Address())
	response, err := testServerGET(t, server.Address(), nil)
	require.NoError(t, err)
	require.NoError(t, response.Body.Close())
	require.Equal(t, http.StatusOK, response.StatusCode)
	require.NoError(t, server.Stop(t.Context()))
	connection, err := net.DialTimeout("tcp", owned.Addr().String(), 100*time.Millisecond)
	require.Error(t, err, "Stop must close its originally acquired listener")
	if connection != nil {
		_ = connection.Close()
	}
}

func TestServerAddressEscapesScopedIPv6Zone(t *testing.T) {
	loopback := boundServerListener(t)
	endpoint := loopback.Addr().(*net.TCPAddr)
	listener := &scopedTCPListener{
		Listener: loopback,
		addr: &net.TCPAddr{
			IP: net.ParseIP("fe80::1"), Port: endpoint.Port, Zone: "en0",
		},
	}
	server := NewServer(9090, "/metrics", NewMetricsRegistry(), security.Config{})
	require.NoError(t, server.StartWithListener(t.Context(), listener))
	registerServerCleanup(t, server)

	address := server.Address()
	require.Equal(t, "http://"+net.JoinHostPort("fe80::1%25en0", strconv.Itoa(endpoint.Port))+"/metrics", address)
	parsed, err := url.Parse(address)
	require.NoError(t, err, "Address must be a valid URL even when TCPAddr has a zone")
	require.Equal(t, "fe80::1%en0", parsed.Hostname())
	require.Equal(t, strconv.Itoa(endpoint.Port), parsed.Port())
	_, err = http.NewRequestWithContext(t.Context(), http.MethodGet, address, nil)
	require.NoError(t, err, "Address must be accepted by the standard HTTP request parser")
}

func TestServerStartOwnsListenerAndRequiresFreshInstanceForRestart(t *testing.T) {
	listener := boundServerListener(t)
	server := NewServer(9090, "/metrics", NewMetricsRegistry(), security.Config{})
	address := listener.Addr().String()

	require.Equal(t, "http://localhost:9090/metrics", server.Address())
	require.NoError(t, server.StartWithListener(t.Context(), listener))
	registerServerCleanup(t, server)
	require.Equal(t, "http://"+address+"/metrics", server.Address())
	require.Same(t, t.Context(), server.server.BaseContext(server.listener))
	connection, err := net.DialTimeout("tcp", address, 250*time.Millisecond)
	require.NoError(t, err, "Start must return only after the listener is owned")
	require.NoError(t, connection.Close())
	response, err := testServerGET(t, server.Address(), nil)
	require.NoError(t, err)
	require.NoError(t, response.Body.Close())
	require.Equal(t, http.StatusOK, response.StatusCode)
	require.False(t, listener.closed.Load(), "Start must retain the exact accepted listener")
	require.Positive(t, listener.accepted.Load(), "Serve must accept through the supplied listener")
	require.NoError(t, server.Stop(t.Context()))
	require.True(t, listener.closed.Load(), "Stop must close the original listener")
	require.Equal(t, "http://localhost:9090/metrics", server.Address())

	connection, err = net.DialTimeout("tcp", address, 100*time.Millisecond)
	require.Error(t, err, "Stop must close the listener before returning")
	if connection != nil {
		_ = connection.Close()
	}

	err = server.Start(t.Context())
	require.Error(t, err, "a stopped Server is one-shot")
	require.ErrorIs(t, err, errs.ErrAlreadyStarted)

	replacement := NewServer(9090, "/metrics", NewMetricsRegistry(), security.Config{})
	replacementListener := boundServerListener(t)
	require.NoError(t, replacement.StartWithListener(t.Context(), replacementListener), "a fresh Server is the restart boundary")
	registerServerCleanup(t, replacement)
	require.NoError(t, replacement.Stop(t.Context()))
}

func TestServerStartWithListenerKeepsOriginalOpenWhileServing(t *testing.T) {
	listener := boundServerListener(t)
	server := NewServer(9090, "/metrics", NewMetricsRegistry(), security.Config{})
	require.NoError(t, server.StartWithListener(t.Context(), listener))
	registerServerCleanup(t, server)
	response, err := testServerGET(t, server.Address(), nil)
	require.NoError(t, err)
	require.NoError(t, response.Body.Close())
	require.Equal(t, http.StatusOK, response.StatusCode)
	require.False(t, listener.closed.Load(), "serving must retain the supplied listener")
	require.Positive(t, listener.accepted.Load(), "serving must use the supplied listener")
}

func TestServerStartWithListenerRefusesWithoutTakingOwnership(t *testing.T) {
	for _, test := range []struct {
		name string
		ctx  func(*testing.T) context.Context
	}{
		{name: "nil context", ctx: func(*testing.T) context.Context { return nil }},
		{name: "ended context", ctx: func(t *testing.T) context.Context {
			ctx, cancel := context.WithCancel(t.Context())
			cancel()
			return ctx
		}},
	} {
		t.Run(test.name, func(t *testing.T) {
			listener := boundServerListener(t)
			server := NewServer(9090, "/metrics", NewMetricsRegistry(), security.Config{})
			require.Error(t, server.StartWithListener(test.ctx(t), listener))
			require.False(t, server.used)
			require.False(t, listener.closed.Load())
			require.Equal(t, "http://localhost:9090/metrics", server.Address())
		})
	}
	server := NewServer(9090, "/metrics", NewMetricsRegistry(), security.Config{})
	require.Error(t, server.StartWithListener(t.Context(), nil))
	require.False(t, server.used)
	require.Error(t, server.StartWithListener(t.Context(), &invalidEndpointListener{}))
	require.False(t, server.used)
	closed, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	require.NoError(t, closed.Close())
	require.Error(t, server.StartWithListener(t.Context(), closed))
	require.False(t, server.used)
}

type invalidEndpointListener struct{}

func (*invalidEndpointListener) Accept() (net.Conn, error) { return nil, net.ErrClosed }
func (*invalidEndpointListener) Close() error              { return nil }
func (*invalidEndpointListener) Addr() net.Addr            { return &net.UnixAddr{Name: "invalid", Net: "unix"} }

func TestServerStartWithListenerPreparationFailuresKeepCallerOwnership(t *testing.T) {
	for _, test := range []struct {
		name     string
		registry *MetricsRegistry
		security security.Config
	}{
		{name: "missing registry"},
		{name: "invalid TLS files", registry: NewMetricsRegistry(), security: security.Config{
			TLS: security.TLSConfig{Server: security.ServerTLSConfig{Enabled: true, CertFile: "missing-cert", KeyFile: "missing-key"}},
		}},
	} {
		t.Run(test.name, func(t *testing.T) {
			listener := boundServerListener(t)
			server := NewServer(9090, "/metrics", test.registry, test.security)
			require.Error(t, server.StartWithListener(t.Context(), listener))
			require.True(t, server.used)
			require.False(t, listener.closed.Load())
			require.Nil(t, server.listener)
			require.ErrorIs(t, server.StartWithListener(t.Context(), listener), errs.ErrAlreadyStarted)
		})
	}
}

func TestServerRejectedSecondStartPreservesOwnedListener(t *testing.T) {
	first := boundServerListener(t)
	server := NewServer(9090, "/metrics", NewMetricsRegistry(), security.Config{})
	require.NoError(t, server.StartWithListener(t.Context(), first))
	registerServerCleanup(t, server)
	want := server.Address()
	second := boundServerListener(t)
	require.ErrorIs(t, server.StartWithListener(t.Context(), second), errs.ErrAlreadyStarted)
	require.Equal(t, want, server.Address())
	require.False(t, first.closed.Load())
	require.False(t, second.closed.Load())
	response, err := testServerGET(t, server.Address(), nil)
	require.NoError(t, err)
	require.NoError(t, response.Body.Close())
	require.Equal(t, http.StatusOK, response.StatusCode)
	require.NoError(t, server.Stop(t.Context()))
	require.True(t, first.closed.Load())
	require.False(t, second.closed.Load())
}

func TestServerStartWithListenerAppliesConfiguredTLSOnce(t *testing.T) {
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	require.NoError(t, err)
	template := &x509.Certificate{
		SerialNumber: big.NewInt(1), NotBefore: time.Now().Add(-time.Hour), NotAfter: time.Now().Add(time.Hour),
		IPAddresses: []net.IP{net.ParseIP("127.0.0.1")}, KeyUsage: x509.KeyUsageDigitalSignature,
		ExtKeyUsage: []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
	}
	certDER, err := x509.CreateCertificate(rand.Reader, template, template, &key.PublicKey, key)
	require.NoError(t, err)
	keyDER, err := x509.MarshalPKCS8PrivateKey(key)
	require.NoError(t, err)
	certPEM := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: certDER})
	dir := t.TempDir()
	certFile, keyFile := filepath.Join(dir, "cert.pem"), filepath.Join(dir, "key.pem")
	require.NoError(t, os.WriteFile(certFile, certPEM, 0600))
	require.NoError(t, os.WriteFile(keyFile, pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: keyDER}), 0600))
	securityCfg := security.Config{TLS: security.TLSConfig{Server: security.ServerTLSConfig{
		Enabled: true, CertFile: certFile, KeyFile: keyFile,
	}}}
	listener := boundServerListener(t)
	server := NewServer(9090, "/metrics", NewMetricsRegistry(), securityCfg)
	require.Equal(t, "https://localhost:9090/metrics", server.Address())
	require.NoError(t, server.StartWithListener(t.Context(), listener))
	registerServerCleanup(t, server)
	require.Equal(t, "https://"+listener.Addr().String()+"/metrics", server.Address())
	pool := x509.NewCertPool()
	require.True(t, pool.AppendCertsFromPEM(certPEM))
	client := &http.Client{Timeout: 2 * time.Second, Transport: &http.Transport{TLSClientConfig: &tls.Config{RootCAs: pool}}} //nolint:gosec // trusted test CA
	response, err := testServerGET(t, server.Address(), client)
	require.NoError(t, err)
	require.NoError(t, response.Body.Close())
	require.Equal(t, http.StatusOK, response.StatusCode)
	require.Positive(t, listener.accepted.Load())
	require.False(t, listener.closed.Load())
	require.NoError(t, server.Stop(t.Context()))
	require.True(t, listener.closed.Load())
	require.Equal(t, "https://localhost:9090/metrics", server.Address())
}

func TestServerConcurrentStopIsTypedTransientAndDoesNotHoldLifecycleLock(t *testing.T) {
	registry := NewMetricsRegistry()
	collector := newBlockingCollector()
	registry.PrometheusRegistry().MustRegister(collector)
	server := NewServer(9090, "/metrics", registry, security.Config{})
	require.NoError(t, server.StartWithListener(t.Context(), boundServerListener(t)))
	var releaseOnce sync.Once
	releaseCollector := func() { releaseOnce.Do(func() { close(collector.release) }) }
	requestDone := make(chan error, 1)
	requestFinished := make(chan struct{})
	var stopFinished <-chan struct{}
	t.Cleanup(func() {
		releaseCollector()
		if stopFinished != nil {
			select {
			case <-stopFinished:
			case <-time.After(5 * time.Second):
				t.Error("concurrent Stop did not finish after collector release")
			}
		}
		select {
		case <-requestFinished:
		case <-time.After(5 * time.Second):
			t.Error("metrics request did not finish after collector release")
		}
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		require.NoError(t, server.Stop(ctx))
	})
	go func() {
		defer close(requestFinished)
		response, err := testServerGET(t, server.Address(), nil)
		if response != nil {
			_ = response.Body.Close()
		}
		requestDone <- err
	}()
	<-collector.entered

	stopResult := make(chan error, 1)
	stopDone := make(chan struct{})
	stopFinished = stopDone
	go func() {
		defer close(stopDone)
		stopResult <- server.Stop(t.Context())
	}()
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

	releaseCollector()
	require.NoError(t, <-requestDone)
	require.NoError(t, <-stopResult)
	require.NoError(t, server.Stop(t.Context()), "completed Stop must be nil/no-op")
}

func TestServerRejectsNilContextBeforeLifecycleMutation(t *testing.T) {
	server := NewServer(9090, "/metrics", NewMetricsRegistry(), security.Config{})
	require.Error(t, server.Start(nil))
	require.False(t, server.used)
	require.Error(t, server.Stop(nil))
	require.False(t, server.used)
}

func TestServerStopBeforeStartConsumesOneShotInstance(t *testing.T) {
	server := NewServer(9090, "/metrics", NewMetricsRegistry(), security.Config{})
	require.NoError(t, server.Stop(t.Context()))
	require.ErrorIs(t, server.Start(t.Context()), errs.ErrAlreadyStarted)
	require.NoError(t, server.Stop(t.Context()))
}

func TestServerStopIsCallerBounded(t *testing.T) {
	registry := NewMetricsRegistry()
	collector := newBlockingCollector()
	registry.PrometheusRegistry().MustRegister(collector)
	server := NewServer(9090, "/metrics", registry, security.Config{})
	listener := boundServerListener(t)
	require.NoError(t, server.StartWithListener(t.Context(), listener))
	var releaseOnce sync.Once
	releaseCollector := func() { releaseOnce.Do(func() { close(collector.release) }) }
	requestDone := make(chan error, 1)
	requestFinished := make(chan struct{})
	t.Cleanup(func() {
		releaseCollector()
		select {
		case <-requestFinished:
		case <-time.After(5 * time.Second):
			t.Error("metrics request did not finish after collector release")
		}
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		require.NoError(t, server.Stop(ctx))
	})
	go func() {
		defer close(requestFinished)
		response, err := testServerGET(t, server.Address(), nil)
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

	address := listener.Addr().String()
	connection, dialErr := net.DialTimeout("tcp", address, 100*time.Millisecond)
	require.Error(t, dialErr, "deadline Stop must release the listener before returning")
	if connection != nil {
		_ = connection.Close()
	}
	require.NoError(t, server.Stop(t.Context()), "terminal repeat must not replay the deadline error")

	releaseCollector()
	<-requestDone // Force-close may surface EOF; the admitted request must terminate.
}

func TestServerServesHealthOverRealHTTP(t *testing.T) {
	server := NewServer(9090, "/metrics", NewMetricsRegistry(), security.Config{})
	require.NoError(t, server.StartWithListener(t.Context(), boundServerListener(t)))
	registerServerCleanup(t, server)

	response, err := testServerGET(t, server.Address()[:len(server.Address())-len("/metrics")]+"/health", nil)
	require.NoError(t, err)
	require.NoError(t, response.Body.Close())
	require.Equal(t, http.StatusOK, response.StatusCode)

	ended, cancel := context.WithCancel(t.Context())
	cancel()
	err = server.Start(ended)
	require.Error(t, err)
	require.True(t, errors.Is(err, context.Canceled) || errors.Is(err, errs.ErrAlreadyStarted))
}

func boundServerListener(t *testing.T) *observedTCPListener {
	t.Helper()
	raw, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	listener := &observedTCPListener{Listener: raw}
	t.Cleanup(func() { _ = listener.Close() })
	return listener
}

package websocket

import (
	"context"
	"errors"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/c360studio/semstreams/natsclient"
	"github.com/c360studio/semstreams/pkg/errs"
	"github.com/c360studio/semstreams/pkg/security"
	"github.com/gorilla/websocket"
)

type inputObservedContext struct {
	context.Context
	seen chan struct{}
	once sync.Once
}

func (c *inputObservedContext) Done() <-chan struct{} {
	c.once.Do(func() { close(c.seen) })
	return c.Context.Done()
}

func TestLifecycleStartReportsServerBindFailureSynchronously(t *testing.T) {
	listener, err := net.Listen("tcp", ":0")
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()

	i := createTestComponent().(*Input)
	i.config.ServerConfig.HTTPPort = listener.Addr().(*net.TCPAddr).Port
	if err := i.Start(t.Context()); err == nil {
		t.Fatal("Start succeeded despite occupied server port")
	}
	if i.started.Load() || i.httpServer != nil {
		t.Fatal("bind failure published running HTTP authority")
	}
	if err := i.Stop(context.Background()); err != nil {
		t.Fatal(err)
	}
}

func TestLifecycleContextAndStopBeforeStartAreImmutable(t *testing.T) {
	i := createTestComponent().(*Input)
	ended, cancel := context.WithCancel(t.Context())
	cancel()
	if i.Start(nil) == nil || i.Start(ended) == nil || i.lifecycleUsed {
		t.Fatal("invalid Start changed authority")
	}
	if i.Stop(nil) == nil || i.lifecycleUsed {
		t.Fatal("nil Stop changed authority")
	}
	if err := i.Stop(t.Context()); err != nil {
		t.Fatal(err)
	}
	if err := i.Start(t.Context()); !errors.Is(err, errs.ErrAlreadyStarted) {
		t.Fatalf("Start after terminal Stop = %v", err)
	}
	if err := i.Stop(t.Context()); err != nil {
		t.Fatal(err)
	}
	if i.Stop(nil) == nil || !i.terminal {
		t.Fatal("nil Stop after terminal changed authority")
	}
}

func TestLifecycleServerStopFencesAndWaitsAdmittedUpgradeBeforeCancel(t *testing.T) {
	i := createTestComponent().(*Input)
	i.config.ServerConfig.HTTPPort = 0
	entered := make(chan context.Context, 1)
	release := make(chan struct{})
	i.requestHook = func(ctx context.Context) {
		entered <- ctx
		<-release
	}
	if err := i.Start(t.Context()); err != nil {
		t.Fatal(err)
	}
	shutdownStarted := make(chan struct{})
	i.httpServer.RegisterOnShutdown(func() { close(shutdownStarted) })
	requestDone := make(chan error, 1)
	go func() {
		resp, err := http.Get("http://" + i.listener.Addr().String() + i.config.ServerConfig.Path)
		if resp != nil {
			_ = resp.Body.Close()
		}
		requestDone <- err
	}()
	handlerCtx := <-entered
	stopDone := make(chan error, 1)
	go func() { stopDone <- i.Stop(t.Context()) }()
	<-shutdownStarted
	if handlerCtx.Err() != nil {
		t.Fatal("handler context canceled before Shutdown drained admission")
	}
	close(release)
	if err := <-stopDone; err != nil {
		t.Fatal(err)
	}
	if err := <-requestDone; err != nil {
		t.Fatal(err)
	}
	if handlerCtx.Err() == nil {
		t.Fatal("handler context remained live after Stop")
	}
	if err := i.Stop(t.Context()); err != nil {
		t.Fatal(err)
	}
}

func TestLifecycleServerDeadlineStopIsTerminalWithoutReplay(t *testing.T) {
	i := createTestComponent().(*Input)
	i.config.ServerConfig.HTTPPort = 0
	entered := make(chan struct{})
	release := make(chan struct{})
	i.requestHook = func(context.Context) {
		close(entered)
		<-release
	}
	if err := i.Start(t.Context()); err != nil {
		t.Fatal(err)
	}
	requestDone := make(chan struct{})
	go func() {
		resp, _ := http.Get("http://" + i.listener.Addr().String() + i.config.ServerConfig.Path)
		if resp != nil {
			_ = resp.Body.Close()
		}
		close(requestDone)
	}()
	<-entered
	stopCtx, cancel := context.WithCancel(t.Context())
	shutdownStarted := make(chan struct{})
	i.httpServer.RegisterOnShutdown(func() { close(shutdownStarted) })
	stopDone := make(chan error, 1)
	go func() { stopDone <- i.Stop(stopCtx) }()
	<-shutdownStarted
	cancel()
	if err := <-stopDone; !errors.Is(err, context.Canceled) {
		t.Fatalf("deadline Stop = %v", err)
	}
	close(release)
	<-requestDone
	if err := i.Stop(t.Context()); err != nil {
		t.Fatalf("terminal Stop replayed cleanup: %v", err)
	}
}

func TestLifecycleClientStopClosesActiveConnectionBeforeJoiningLoops(t *testing.T) {
	upgrader := websocket.Upgrader{CheckOrigin: func(*http.Request) bool { return true }}
	connected := make(chan struct{})
	peerClosed := make(chan struct{})
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			return
		}
		defer conn.Close()
		close(connected)
		_, _, _ = conn.ReadMessage()
		close(peerClosed)
	}))
	defer server.Close()

	config := DefaultConfig()
	config.Mode = ModeClient
	config.ServerConfig = nil
	config.ClientConfig = &ClientConfig{
		URL:       "ws" + strings.TrimPrefix(server.URL, "http"),
		Reconnect: &ReconnectConfig{Enabled: false},
	}
	i, err := NewInput("client-lifecycle", &natsclient.Client{}, config, nil, security.Config{})
	if err != nil {
		t.Fatal(err)
	}
	published := make(chan struct{})
	i.clientPublished = func(*websocket.Conn) { close(published) }
	cancelReached := make(chan struct{})
	i.beforeRuntimeCancel = func() {
		i.clientMu.Lock()
		client := i.wsClient
		i.clientMu.Unlock()
		if client != nil {
			t.Error("runtime cancellation reached before active client was cleared")
		}
		close(cancelReached)
	}
	if err := i.Start(t.Context()); err != nil {
		t.Fatal(err)
	}
	<-connected
	<-published
	stopDone := make(chan error, 1)
	go func() { stopDone <- i.Stop(t.Context()) }()
	<-cancelReached
	<-peerClosed
	if err := <-stopDone; err != nil {
		t.Fatal(err)
	}
	i.clientMu.Lock()
	client := i.wsClient
	i.clientMu.Unlock()
	if client != nil || i.cancel != nil || i.runtimeDone != nil || i.started.Load() {
		t.Fatal("client Stop retained connection or runtime authority")
	}
}

func TestLifecycleClientStopRejectsLateDialAfterReconnectFence(t *testing.T) {
	upgrader := websocket.Upgrader{CheckOrigin: func(*http.Request) bool { return true }}
	peerClosed := make(chan struct{})
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			return
		}
		defer conn.Close()
		_, _, _ = conn.ReadMessage()
		close(peerClosed)
	}))
	defer server.Close()
	url := "ws" + strings.TrimPrefix(server.URL, "http")

	config := DefaultConfig()
	config.Mode = ModeClient
	config.ServerConfig = nil
	config.ClientConfig = &ClientConfig{
		URL: url,
		Reconnect: &ReconnectConfig{
			Enabled:         true,
			MaxRetries:      10,
			InitialInterval: time.Second,
			MaxInterval:     time.Second,
			Multiplier:      1,
		},
	}
	i, err := NewInput("client-late-dial", &natsclient.Client{}, config, nil, security.Config{})
	if err != nil {
		t.Fatal(err)
	}
	dialReady := make(chan struct{})
	releaseDial := make(chan struct{})
	i.dialClient = func(_ context.Context, _ string, headers http.Header) (*websocket.Conn, *http.Response, error) {
		conn, response, dialErr := websocket.DefaultDialer.DialContext(t.Context(), url, headers)
		if dialErr != nil {
			return conn, response, dialErr
		}
		close(dialReady)
		<-releaseDial
		return conn, response, nil
	}
	published := make(chan struct{})
	i.clientPublished = func(*websocket.Conn) { close(published) }
	cancelReached := make(chan struct{})
	i.beforeRuntimeCancel = func() { close(cancelReached) }
	if err := i.Start(t.Context()); err != nil {
		t.Fatal(err)
	}
	<-dialReady
	stopCtx := &inputObservedContext{Context: t.Context(), seen: make(chan struct{})}
	stopDone := make(chan error, 1)
	go func() { stopDone <- i.Stop(stopCtx) }()
	<-cancelReached
	<-stopCtx.seen
	i.lifecycleMu.Lock()
	i.lifecycleMu.Unlock()
	i.clientMu.Lock()
	i.clientMu.Unlock()
	select {
	case err := <-stopDone:
		t.Fatalf("Stop returned before late dial joined: %v", err)
	default:
	}
	close(releaseDial)
	<-peerClosed
	if err := <-stopDone; err != nil {
		t.Fatal(err)
	}
	select {
	case <-published:
		t.Fatal("late client was published after Stop fenced reconnects")
	default:
	}
	i.clientMu.Lock()
	client := i.wsClient
	i.clientMu.Unlock()
	if client != nil || i.cancel != nil || i.runtimeDone != nil || i.started.Load() {
		t.Fatal("terminal client Stop retained runtime authority")
	}
}

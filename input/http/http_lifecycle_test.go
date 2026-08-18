package http

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"
)

type lifecyclePublisher struct {
	entered chan struct{}
	release <-chan struct{}
	once    sync.Once
}

func (p *lifecyclePublisher) Publish(_ context.Context, _ string, _ []byte) error {
	p.once.Do(func() { close(p.entered) })
	if p.release != nil {
		<-p.release
	}
	return nil
}

func newLifecycleHTTPInput(t *testing.T, pub publisher) (*Input, func()) {
	t.Helper()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = io.WriteString(w, `{"ok":true}`)
	}))
	input := newTestInput(t, Config{
		URL:      server.URL,
		Interval: "1h",
		Timeout:  "1s",
	}, pub)
	input.logger = slog.New(slog.NewTextHandler(io.Discard, nil))
	return input, server.Close
}

func waitForHTTPInput(t *testing.T, input *Input) {
	t.Helper()
	done := make(chan struct{})
	go func() {
		input.wg.Wait()
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("HTTP input did not join")
	}
}

func TestInputStartStopAndCompletedRepeat(t *testing.T) {
	release := make(chan struct{})
	pub := &lifecyclePublisher{entered: make(chan struct{}), release: release}
	input, closeServer := newLifecycleHTTPInput(t, pub)
	defer closeServer()
	if err := input.Start(context.Background()); err != nil {
		t.Fatalf("Start: %v", err)
	}
	select {
	case <-pub.entered:
	case <-time.After(2 * time.Second):
		t.Fatal("HTTP input did not begin its first publish")
	}
	close(release)

	stopCtx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	if err := input.Stop(stopCtx); err != nil {
		t.Fatalf("Stop: %v", err)
	}
	if input.running.Load() {
		t.Fatal("input remained running after Stop joined its loop")
	}
	if err := input.Stop(context.Background()); err != nil {
		t.Fatalf("completed repeated Stop: %v", err)
	}
}

func TestInputParentCancellationStopsOwnedLoop(t *testing.T) {
	pub := &lifecyclePublisher{entered: make(chan struct{})}
	input, closeServer := newLifecycleHTTPInput(t, pub)
	defer closeServer()
	parent, cancelParent := context.WithCancel(context.Background())
	if err := input.Start(parent); err != nil {
		t.Fatalf("Start: %v", err)
	}
	select {
	case <-pub.entered:
	case <-time.After(2 * time.Second):
		t.Fatal("HTTP input did not begin its first publish")
	}

	cancelParent()
	waitForHTTPInput(t, input)
	if err := input.Stop(context.Background()); err != nil {
		t.Fatalf("Stop after parent cancellation: %v", err)
	}
}

func TestInputStopDeadlineDoesNotPromiseRejoin(t *testing.T) {
	release := make(chan struct{})
	pub := &lifecyclePublisher{entered: make(chan struct{}), release: release}
	input, closeServer := newLifecycleHTTPInput(t, pub)
	defer closeServer()
	if err := input.Start(context.Background()); err != nil {
		t.Fatalf("Start: %v", err)
	}
	select {
	case <-pub.entered:
	case <-time.After(2 * time.Second):
		t.Fatal("HTTP input did not begin its first publish")
	}

	stopCtx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	err := input.Stop(stopCtx)
	cancel()
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("Stop error = %v, want deadline exceeded", err)
	}
	if err := input.Stop(context.Background()); err != nil {
		t.Fatalf("completed repeated Stop must not rejoin: %v", err)
	}

	close(release)
	waitForHTTPInput(t, input)
}

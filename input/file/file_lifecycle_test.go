package file

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"syscall"
	"testing"
	"time"

	"github.com/c360studio/semstreams/component"
	"github.com/c360studio/semstreams/natsclient"
)

func newLifecycleInput(t *testing.T, path string, loop bool) *Input {
	t.Helper()

	input, err := NewInput(InputDeps{
		Name: "file-input-test",
		Config: Config{
			Path:     path,
			Format:   "jsonl",
			Interval: "1h",
			Loop:     loop,
			Ports: &component.PortConfig{Outputs: []component.PortDefinition{{
				Name: "nats_output", Required: true,
				Config: component.NATSPort{Subject: "test.file.input"},
			}}},
		},
		NATSClient: &natsclient.Client{},
		Logger:     slog.New(slog.NewTextHandler(io.Discard, nil)),
	})
	if err != nil {
		t.Fatalf("NewInput: %v", err)
	}
	return input
}

func writeLifecycleFile(t *testing.T) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "events.jsonl")
	if err := os.WriteFile(path, []byte("{\"test\":\"data\"}\n"), 0o600); err != nil {
		t.Fatalf("write test input: %v", err)
	}
	return path
}

func waitForFileInput(t *testing.T, input *Input) {
	t.Helper()
	done := make(chan struct{})
	go func() {
		input.wg.Wait()
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("file input did not join")
	}
}

func TestInputStartStopAndCompletedRepeat(t *testing.T) {
	input := newLifecycleInput(t, writeLifecycleFile(t), true)
	if err := input.Start(context.Background()); err != nil {
		t.Fatalf("Start: %v", err)
	}

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
	input := newLifecycleInput(t, writeLifecycleFile(t), true)
	parent, cancelParent := context.WithCancel(context.Background())
	if err := input.Start(parent); err != nil {
		t.Fatalf("Start: %v", err)
	}

	cancelParent()
	waitForFileInput(t, input)
	if err := input.Stop(context.Background()); err != nil {
		t.Fatalf("Stop after parent cancellation: %v", err)
	}
}

func TestInputStopDeadlineDoesNotPromiseRejoin(t *testing.T) {
	fifo := filepath.Join(t.TempDir(), "blocked-input")
	if err := syscall.Mkfifo(fifo, 0o600); err != nil {
		t.Fatalf("create FIFO: %v", err)
	}
	input := newLifecycleInput(t, fifo, false)
	if err := input.Start(context.Background()); err != nil {
		t.Fatalf("Start: %v", err)
	}

	writerReady := make(chan *os.File, 1)
	writerErr := make(chan error, 1)
	go func() {
		writer, err := os.OpenFile(fifo, os.O_WRONLY, 0)
		if err != nil {
			writerErr <- err
			return
		}
		writerReady <- writer
	}()

	var writer *os.File
	select {
	case writer = <-writerReady:
	case err := <-writerErr:
		t.Fatalf("open FIFO writer: %v", err)
	case <-time.After(2 * time.Second):
		t.Fatal("file input did not open FIFO")
	}
	t.Cleanup(func() { _ = writer.Close() })

	stopCtx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	err := input.Stop(stopCtx)
	cancel()
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("Stop error = %v, want deadline exceeded", err)
	}
	if err := input.Stop(context.Background()); err != nil {
		t.Fatalf("completed repeated Stop must not rejoin: %v", err)
	}

	if err := writer.Close(); err != nil {
		t.Fatalf("release FIFO reader: %v", err)
	}
	waitForFileInput(t, input)
}

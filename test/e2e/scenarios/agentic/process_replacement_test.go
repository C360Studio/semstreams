package agentic

import (
	"context"
	"errors"
	"reflect"
	"testing"
	"time"

	"github.com/c360studio/semstreams/test/e2e/harness/processbarrier"
)

func TestComposeProcessControllerTargetsOnlySemStreams(t *testing.T) {
	var calls [][]string
	controller := composeProcessController{
		composeFile: "docker/compose/agentic.yml",
		service:     "semstreams",
		run: func(_ context.Context, name string, args ...string) error {
			calls = append(calls, append([]string{name}, args...))
			return nil
		},
	}

	if err := controller.kill(t.Context()); err != nil {
		t.Fatalf("kill() error = %v", err)
	}
	if err := controller.start(t.Context()); err != nil {
		t.Fatalf("start() error = %v", err)
	}

	want := [][]string{
		{"docker", "compose", "-f", "docker/compose/agentic.yml", "kill", "-s", "SIGKILL", "semstreams"},
		{"docker", "compose", "-f", "docker/compose/agentic.yml", "up", "-d", "--wait", "--no-deps", "semstreams"},
	}
	if !reflect.DeepEqual(calls, want) {
		t.Fatalf("commands = %#v, want %#v", calls, want)
	}
}

func TestHarnessFinalizationIsDetachedBoundedAndJoined(t *testing.T) {
	parent, cancel := context.WithCancel(t.Context())
	cancel()
	primaryErr := errors.New("scenario failed")
	cleanupErr := errors.New("cleanup failed")
	runErr := error(primaryErr)

	joinHarnessFinalizationError(parent, &runErr, "restore fixture", func(finalCtx context.Context) error {
		if err := finalCtx.Err(); err != nil {
			t.Fatalf("finalization inherited cancellation: %v", err)
		}
		deadline, ok := finalCtx.Deadline()
		if !ok {
			t.Fatal("finalization context has no deadline")
		}
		remaining := time.Until(deadline)
		if remaining <= 0 || remaining > harnessFinalizationTimeout {
			t.Fatalf("finalization deadline remaining = %v", remaining)
		}
		return cleanupErr
	})

	if !errors.Is(runErr, primaryErr) || !errors.Is(runErr, cleanupErr) {
		t.Fatalf("joined error = %v, want primary and cleanup causes", runErr)
	}
}

func TestFirstBackOffEvidenceExcludesStartupAndSemanticRetry(t *testing.T) {
	base := time.Unix(100, 0).UTC()
	first := processbarrier.Attempt{ProcessInstance: "old", EnteredAt: base}
	tests := []struct {
		name        string
		replacement processbarrier.Attempt
		redelivery  processbarrier.Attempt
		wantErr     bool
	}{
		{
			name:        "15 second server backoff",
			replacement: processbarrier.Attempt{ProcessInstance: "new", EnteredAt: base.Add(8 * time.Second)},
			redelivery:  processbarrier.Attempt{ProcessInstance: "new", EnteredAt: base.Add(15 * time.Second)},
		},
		{
			name:        "30 second semantic retry",
			replacement: processbarrier.Attempt{ProcessInstance: "new", EnteredAt: base.Add(8 * time.Second)},
			redelivery:  processbarrier.Attempt{ProcessInstance: "new", EnteredAt: base.Add(30 * time.Second)},
			wantErr:     true,
		},
		{
			name:        "startup contaminates measurement",
			replacement: processbarrier.Attempt{ProcessInstance: "new", EnteredAt: base.Add(13 * time.Second)},
			redelivery:  processbarrier.Attempt{ProcessInstance: "new", EnteredAt: base.Add(15 * time.Second)},
			wantErr:     true,
		},
		{
			name:        "same process",
			replacement: processbarrier.Attempt{ProcessInstance: "old", EnteredAt: base.Add(8 * time.Second)},
			redelivery:  processbarrier.Attempt{ProcessInstance: "old", EnteredAt: base.Add(15 * time.Second)},
			wantErr:     true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := validateFirstBackOffEvidence(first, tt.replacement, tt.redelivery)
			if (err != nil) != tt.wantErr {
				t.Fatalf("validateFirstBackOffEvidence() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func TestBarrierReleaseFlushDerivesBoundedOperationContext(t *testing.T) {
	parent := t.Context()
	if _, ok := parent.Deadline(); ok {
		t.Fatal("test requires the same no-deadline context shape as the E2E runner")
	}
	called := false
	err := flushBarrierRelease(parent, func(flushCtx context.Context) error {
		called = true
		deadline, ok := flushCtx.Deadline()
		if !ok {
			t.Fatal("flush context has no deadline")
		}
		remaining := time.Until(deadline)
		if remaining <= 0 || remaining > barrierReleaseFlushTimeout {
			t.Fatalf("flush deadline remaining = %v, want (0, %v]", remaining, barrierReleaseFlushTimeout)
		}
		return nil
	})
	if err != nil {
		t.Fatalf("flushBarrierRelease() error = %v", err)
	}
	if !called {
		t.Fatal("flush callback was not called")
	}
}

func TestComposeProcessControllerRejectsIncompleteTarget(t *testing.T) {
	for _, controller := range []composeProcessController{
		{service: "semstreams", run: func(context.Context, string, ...string) error { return nil }},
		{composeFile: "agentic.yml", run: func(context.Context, string, ...string) error { return nil }},
		{composeFile: "agentic.yml", service: "semstreams"},
	} {
		if err := controller.kill(t.Context()); err == nil {
			t.Fatalf("kill() accepted incomplete controller: %#v", controller)
		}
	}
}

package graphingest

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"os/exec"
	"strings"
	"testing"
	"time"
)

const graphIngestOwnerChildEnv = "GRAPH_INGEST_OWNER_CHILD"

func graphIngestOwnerChildRequest(mode string, args []string) (bool, error) {
	if mode == "" {
		return false, nil
	}
	switch mode {
	case "setup-exit", "transfer-exit", "cleanup-error", "fence-error":
	default:
		return false, fmt.Errorf("unknown graph-ingest owner child mode %q", mode)
	}
	if len(args) != 2 || args[0] != "-test.run=^TestGraphIngestOwnerChildFixture$" || args[1] != "-test.count=1" {
		return false, fmt.Errorf("graph-ingest owner child requires exact selected test and count, got %q", args)
	}
	return true, nil
}

func TestGraphIngestOwnerChildRequestRejectsOtherSelections(t *testing.T) {
	for _, tt := range []struct {
		mode string
		args []string
		want bool
	}{
		{"", nil, false},
		{"setup-exit", []string{"-test.run=^TestGraphIngestOwnerChildFixture$", "-test.count=1"}, true},
		{"unknown", []string{"-test.run=^TestGraphIngestOwnerChildFixture$", "-test.count=1"}, false},
		{"setup-exit", []string{"-test.run=TestGraphIngestOwnerChildFixture", "-test.count=1"}, false},
		{"setup-exit", []string{"-test.run=^TestGraphIngestOwnerChildFixture$", "-test.count=1", "-test.list=."}, false},
		{"setup-exit", []string{"-test.run=^TestGraphIngestOwnerChildFixture$", "-test.bench=."}, false},
	} {
		got, err := graphIngestOwnerChildRequest(tt.mode, tt.args)
		wantErr := tt.mode != "" && !tt.want
		if got != tt.want || (err != nil) != wantErr {
			t.Errorf("mode=%q args=%q: selected=%v err=%v, want selected=%v", tt.mode, tt.args, got, err, tt.want)
		}
	}
}

// spec: test-cleanup-policy / Lexical ownership of lifecycle test fixtures
func TestGraphIngestOwnerAssertionExitAndCleanupError(t *testing.T) {
	for _, tt := range []struct {
		mode    string
		witness string
	}{
		{"setup-exit", "intentional setup assertion exit"},
		{"transfer-exit", "intentional caller assertion exit"},
		{"cleanup-error", "graph-ingest terminal cleanup: child terminal sentinel"},
		{"fence-error", "explicit terminal fence failed: child terminal sentinel"},
	} {
		t.Run(tt.mode, func(t *testing.T) {
			childCtx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
			defer cancel()
			cmd := exec.CommandContext(childCtx, os.Args[0],
				"-test.run=^TestGraphIngestOwnerChildFixture$", "-test.count=1")
			cmd.Env = append(os.Environ(), graphIngestOwnerChildEnv+"="+tt.mode)
			output, err := cmd.CombinedOutput() // One owned process and one synchronous wait.
			if childCtx.Err() != nil {
				t.Fatalf("child proof timed out: %v; output:\n%s", childCtx.Err(), output)
			}
			var exit *exec.ExitError
			if !errors.As(err, &exit) || exit.ExitCode() != 1 {
				t.Fatalf("child exit = %v, want expected test-failure code 1; output:\n%s", err, output)
			}
			for _, text := range []string{tt.witness, "GRAPH_INGEST_CHILD_DRAIN_FINITE", "GRAPH_INGEST_CHILD_START_LIVE"} {
				if !strings.Contains(string(output), text) {
					t.Fatalf("child missing witness %q; output:\n%s", text, output)
				}
			}
			if tt.mode == "transfer-exit" && !strings.Contains(string(output), "GRAPH_INGEST_CHILD_TRANSFER_LIVE") {
				t.Fatalf("successful transfer was not observed before caller assertion; output:\n%s", output)
			}
			for _, forbidden := range []string{"panic:", "DATA RACE"} {
				if strings.Contains(string(output), forbidden) {
					t.Fatalf("child emitted forbidden %q; output:\n%s", forbidden, output)
				}
			}
			if tt.mode == "fence-error" && strings.Contains(string(output), "GRAPH_INGEST_CHILD_NEXT_PHASE") {
				t.Fatalf("failed explicit fence admitted the next phase; output:\n%s", output)
			}
		})
	}
}

func TestGraphIngestOwnerChildFixture(t *testing.T) {
	mode := os.Getenv(graphIngestOwnerChildEnv)
	if mode == "" {
		return
	}
	if _, err := graphIngestOwnerChildRequest(mode, os.Args[1:]); err != nil {
		t.Fatal(err)
	}
	operationCtx, cancelOperation := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancelOperation()
	var startCtx context.Context
	drain := &graphIngestTestDrain{drain: func(stopCtx context.Context) error {
		if _, finite := stopCtx.Deadline(); !finite {
			return errors.New("child Stop received no deadline")
		}
		fmt.Println("GRAPH_INGEST_CHILD_DRAIN_FINITE")
		if startCtx.Err() != nil {
			return errors.New("child Start authority ended before Stop")
		}
		fmt.Println("GRAPH_INGEST_CHILD_START_LIVE")
		if mode == "cleanup-error" || mode == "fence-error" {
			return errors.New("child terminal sentinel")
		}
		return nil
	}}
	component := &Component{logger: slog.Default(), lifecycleUsed: true, running: true,
		subscriptions: []graphIngestCoreSubscription{drain}}
	owner := newGraphIngestTestOwner(component)
	startCtx = owner.startContext(operationCtx)
	switch mode {
	case "setup-exit":
		func() {
			defer owner.provisionalFinish(operationCtx, t)
			t.Fatal("intentional setup assertion exit")
		}()
	case "transfer-exit":
		transferred := func() *graphIngestTestOwner {
			defer owner.provisionalFinish(operationCtx, t)
			owner.transfer()
			return owner
		}()
		defer transferred.finish(operationCtx, t)
		if got := drain.called.Load(); got != 0 || startCtx.Err() != nil {
			t.Fatalf("transfer prematurely finalized fixture: drains=%d Start=%v", got, startCtx.Err())
		}
		fmt.Println("GRAPH_INGEST_CHILD_TRANSFER_LIVE")
		t.Fatal("intentional caller assertion exit")
	case "cleanup-error":
		defer owner.finish(operationCtx, t)
		return
	case "fence-error":
		if err := owner.stop(operationCtx); err != nil {
			t.Fatalf("explicit terminal fence failed: %v", err)
		}
	}
	fmt.Println("GRAPH_INGEST_CHILD_NEXT_PHASE")
}

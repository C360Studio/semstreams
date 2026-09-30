package graphquery

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"sync/atomic"
	"testing"
	"testing/synctest"
	"time"

	"github.com/c360studio/semstreams/graph/llm"
	"github.com/stretchr/testify/require"
)

type graphQueryOwnerCloseProbe struct {
	close func() error
	calls atomic.Int32
}

func (*graphQueryOwnerCloseProbe) ChatCompletion(context.Context, llm.ChatRequest) (*llm.ChatResponse, error) {
	return &llm.ChatResponse{}, nil
}
func (*graphQueryOwnerCloseProbe) Model() string { return "owner-proof" }
func (p *graphQueryOwnerCloseProbe) Close() error {
	p.calls.Add(1)
	return p.close()
}

// spec: test-cleanup-policy / Lexical ownership of lifecycle test fixtures
func TestGraphQueryOwnerAssertionExitAndCleanupErrors(t *testing.T) {
	for _, tc := range []struct {
		mode      string
		witnesses []string
	}{
		{"caller-exit", []string{"intentional caller assertion exit", "graph-query terminal cleanup: child terminal sentinel", "GRAPH_QUERY_CHILD_CLOSE_START_LIVE", "GRAPH_QUERY_CHILD_SUBSTRATE_AFTER_TERMINAL"}},
		{"cleanup-error", []string{"graph-query terminal cleanup: child terminal sentinel", "GRAPH_QUERY_CHILD_CLOSE_START_LIVE", "GRAPH_QUERY_CHILD_SUBSTRATE_AFTER_TERMINAL"}},
		{"fence-error", []string{"explicit terminal fence failed: child terminal sentinel", "GRAPH_QUERY_CHILD_CLOSE_START_LIVE", "GRAPH_QUERY_CHILD_CLOSE_ONCE"}},
	} {
		t.Run(tc.mode, func(t *testing.T) {
			childCtx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
			defer cancel()
			cmd := exec.CommandContext(childCtx, os.Args[0],
				"-test.run=^TestGraphQueryOwnerChildFixture$", "-test.count=1")
			cmd.Env = append(os.Environ(), graphQueryOwnerChildEnv+"="+tc.mode)
			output, err := cmd.CombinedOutput()
			if childCtx.Err() != nil {
				t.Fatalf("child proof timed out: %v; output:\n%s", childCtx.Err(), output)
			}
			var exit *exec.ExitError
			if !errors.As(err, &exit) || exit.ExitCode() != 1 {
				t.Fatalf("child exit = %v, want expected test-failure code 1; output:\n%s", err, output)
			}
			for _, witness := range tc.witnesses {
				if !strings.Contains(string(output), witness) {
					t.Fatalf("child missing witness %q; output:\n%s", witness, output)
				}
			}
			for _, forbidden := range []string{"panic:", "DATA RACE", "GRAPH_QUERY_CHILD_NEXT_PHASE", "GRAPH_QUERY_CHILD_CLOSE_RETRIED"} {
				if strings.Contains(string(output), forbidden) {
					t.Fatalf("child emitted forbidden %q; output:\n%s", forbidden, output)
				}
			}
		})
	}
}

func TestGraphQueryOwnerChildFixture(t *testing.T) {
	mode := os.Getenv(graphQueryOwnerChildEnv)
	if mode == "" || mode == "setup-exit" {
		return
	}
	if err := graphQueryOwnerChildRequest(mode, os.Args[1:]); err != nil {
		t.Fatal(err)
	}
	operationCtx, cancelOperation := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancelOperation()
	comp := createTestComponentWithMockClient(t, newMockNATSClient())
	owner := newGraphQueryTestOwner(comp)
	startCtx := owner.startContext(operationCtx)
	defer owner.finish(operationCtx, startCtx, false, t)
	require.NoError(t, comp.Initialize())
	require.NoError(t, comp.Start(startCtx))
	probe := &graphQueryOwnerCloseProbe{close: func() error {
		if startCtx.Err() != nil {
			return errors.New("accepted Start authority ended before concrete Close")
		}
		fmt.Println("GRAPH_QUERY_CHILD_CLOSE_START_LIVE")
		if mode == "caller-exit" || mode == "cleanup-error" || mode == "fence-error" {
			return errors.New("child terminal sentinel")
		}
		return nil
	}}
	comp.llmClient = probe
	t.Cleanup(func() {
		if !comp.lifecycleTerminal {
			t.Error("GRAPH_QUERY_CHILD_COMPONENT_NOT_TERMINAL")
		} else {
			fmt.Println("GRAPH_QUERY_CHILD_SUBSTRATE_AFTER_TERMINAL")
		}
		if got := probe.calls.Load(); got == 1 {
			fmt.Println("GRAPH_QUERY_CHILD_CLOSE_ONCE")
		} else {
			t.Errorf("GRAPH_QUERY_CHILD_CLOSE_RETRIED: calls=%d", got)
		}
	})
	switch mode {
	case "caller-exit":
		t.Fatal("intentional caller assertion exit")
	case "cleanup-error":
		return
	case "fence-error":
		if err := owner.stop(operationCtx, startCtx, false); err != nil {
			t.Fatalf("explicit terminal fence failed: %v", err)
		}
	}
	fmt.Println("GRAPH_QUERY_CHILD_NEXT_PHASE")
}

// spec: test-cleanup-policy / Lexical ownership of lifecycle test fixtures
func TestGraphQueryOwnerTerminalExpiryAndIndependentAuthority(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		operationCtx, cancelOperation := context.WithTimeout(t.Context(), 10*time.Second)
		defer cancelOperation()
		comp := createTestComponentWithMockClient(t, newMockNATSClient())
		owner := newGraphQueryTestOwner(comp)
		startCtx := owner.startContext(operationCtx)
		defer owner.finish(operationCtx, startCtx, false, t)
		require.NoError(t, comp.Initialize())
		require.NoError(t, comp.Start(startCtx))
		probe := &graphQueryOwnerCloseProbe{close: func() error {
			time.Sleep(6 * time.Second) // Fake time crosses Stop's five-second bound.
			return nil
		}}
		comp.llmClient = probe
		err := owner.stop(operationCtx, startCtx, false)
		require.NoError(t, owner.concreteStopErr, "the native Close returned nil")
		require.ErrorIs(t, owner.stopBoundErr, context.DeadlineExceeded)
		require.ErrorIs(t, err, context.DeadlineExceeded)
		require.NoError(t, operationCtx.Err(), "terminal expiry must not consume operation authority")
		require.Equal(t, int32(1), probe.calls.Load())
	})
}

func TestGraphQueryOwnerEndedOperationStillAttemptsTerminal(t *testing.T) {
	operationCtx, cancelOperation := context.WithCancel(t.Context())
	comp := createTestComponentWithMockClient(t, newMockNATSClient())
	owner := newGraphQueryTestOwner(comp)
	startCtx := owner.startContext(operationCtx)
	defer owner.finish(operationCtx, startCtx, false, t)
	require.NoError(t, comp.Initialize())
	require.NoError(t, comp.Start(startCtx))
	probe := &graphQueryOwnerCloseProbe{close: func() error { return nil }}
	comp.llmClient = probe
	cancelOperation()
	err := owner.stop(operationCtx, startCtx, false)
	require.NoError(t, owner.concreteStopErr)
	require.NoError(t, owner.stopBoundErr)
	require.ErrorIs(t, err, context.Canceled)
	require.Equal(t, int32(1), probe.calls.Load())
}

// A retained failed-Start cleanup can run again through Component.Stop, so it
// distinguishes one fixture attempt from a repeated native cleanup attempt.
// spec: test-cleanup-policy / Lexical ownership of lifecycle test fixtures
func TestGraphQueryOwnerFailedStartCleanupIsNotRetried(t *testing.T) {
	operationCtx, cancelOperation := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancelOperation()
	sentinel := errors.New("retained failed-Start cleanup sentinel")
	probe := &graphQueryOwnerCloseProbe{close: func() error { return sentinel }}
	comp := &Component{logger: lifecycleLogger(), initialized: true, lifecycleUsed: true,
		cleanupPending: true, llmClient: probe}
	owner := newGraphQueryTestOwner(comp)
	startCtx := owner.startContext(operationCtx)
	defer func() { require.Equal(t, int32(1), probe.calls.Load(), "one concrete terminal attempt") }()
	defer owner.finish(operationCtx, startCtx, false, t)
	require.ErrorIs(t, owner.stop(operationCtx, startCtx, false), sentinel)
	require.ErrorIs(t, owner.concreteStopErr, sentinel)
}

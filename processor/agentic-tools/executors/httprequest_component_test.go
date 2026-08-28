package executors

import (
	"context"
	"encoding/json"
	"fmt"
	"net"
	"sync/atomic"
	"testing"

	"github.com/c360studio/semstreams/agentic"
	"github.com/c360studio/semstreams/component"
	agentictools "github.com/c360studio/semstreams/processor/agentic-tools"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type countingHTTPRequestExecutor struct {
	delegate *HTTPRequestExecutor
	calls    atomic.Int32
}

func (e *countingHTTPRequestExecutor) ListTools() []agentic.ToolDefinition {
	return e.delegate.ListTools()
}

func (e *countingHTTPRequestExecutor) Execute(
	ctx context.Context,
	call agentic.ToolCall,
) (agentic.ToolResult, error) {
	e.calls.Add(1)
	return e.delegate.Execute(ctx, call)
}

func newHTTPComponentTestHarness(
	t *testing.T,
	executor agentictools.ToolExecutor,
) interface {
	Execute(context.Context, agentic.ToolCall) (agentic.ToolResult, error)
} {
	t.Helper()
	config := agentictools.DefaultConfig()
	config.Timeout = "1s"
	config.ToolRetries = map[string]agentictools.RetryPolicy{
		"http_request": {MaxAttempts: 2, BackoffInitialMs: 1, BackoffMaxMs: 1},
	}
	rawConfig, err := json.Marshal(config)
	require.NoError(t, err)
	discoverable, err := agentictools.NewComponent(rawConfig, component.Dependencies{})
	require.NoError(t, err)
	registrar := discoverable.(interface {
		RegisterToolExecutor(agentictools.ToolExecutor) error
	})
	require.NoError(t, registrar.RegisterToolExecutor(executor))
	return discoverable.(interface {
		Execute(context.Context, agentic.ToolCall) (agentic.ToolResult, error)
	})
}

func TestHTTPRequestExecutor_ComponentClassifiesAndRetriesNetworkFailure(t *testing.T) {
	delegate := NewHTTPRequestExecutor()
	delegate.lookupIP = func(_ context.Context, _ string) ([]net.IP, error) {
		return nil, fmt.Errorf("resolver unavailable")
	}
	counting := &countingHTTPRequestExecutor{delegate: delegate}
	harness := newHTTPComponentTestHarness(t, counting)

	result, err := harness.Execute(context.Background(), agentic.ToolCall{
		ID: "network-retry", Name: "http_request",
		Arguments: map[string]any{"url": "http://retry.example/page"},
	})

	require.NoError(t, err)
	assert.Equal(t, agentic.ToolErrorNetwork, result.ErrorKind)
	assert.Equal(t, int32(2), counting.calls.Load())
}

func TestHTTPRequestExecutor_ComponentClassifiesCancellationWithoutRetry(t *testing.T) {
	lookupStarted := make(chan struct{})
	delegate := NewHTTPRequestExecutor()
	delegate.lookupIP = func(ctx context.Context, _ string) ([]net.IP, error) {
		close(lookupStarted)
		<-ctx.Done()
		return nil, ctx.Err()
	}
	counting := &countingHTTPRequestExecutor{delegate: delegate}
	harness := newHTTPComponentTestHarness(t, counting)

	ctx, cancel := context.WithCancel(context.Background())
	type outcome struct {
		result agentic.ToolResult
		err    error
	}
	resultCh := make(chan outcome, 1)
	go func() {
		result, err := harness.Execute(ctx, agentic.ToolCall{
			ID: "cancel", Name: "http_request",
			Arguments: map[string]any{"url": "http://cancel.example/page"},
		})
		resultCh <- outcome{result: result, err: err}
	}()
	<-lookupStarted
	cancel()
	out := <-resultCh

	require.ErrorIs(t, out.err, context.Canceled)
	assert.Equal(t, agentic.ToolErrorTimeout, out.result.ErrorKind)
	assert.Equal(t, int32(1), counting.calls.Load())
}

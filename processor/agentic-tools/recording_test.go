package agentictools

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/c360studio/semstreams/agentic"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// mockExecutor is a test executor that tracks calls
type mockExecutor struct {
	tools   []agentic.ToolDefinition
	calls   []agentic.ToolCall
	results map[string]agentic.ToolResult
	mu      sync.Mutex
}

func newMockExecutor() *mockExecutor {
	return &mockExecutor{
		tools: []agentic.ToolDefinition{
			{Name: "test_tool", Description: "A test tool"},
		},
		results: make(map[string]agentic.ToolResult),
	}
}

func (m *mockExecutor) Execute(_ context.Context, call agentic.ToolCall) (agentic.ToolResult, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	m.calls = append(m.calls, call)

	if result, ok := m.results[call.ID]; ok {
		return result, nil
	}

	return agentic.ToolResult{
		CallID:  call.ID,
		Content: "mock result",
		LoopID:  call.LoopID,
		TraceID: call.TraceID,
	}, nil
}

func (m *mockExecutor) ListTools() []agentic.ToolDefinition {
	return m.tools
}

func (m *mockExecutor) GetCalls() []agentic.ToolCall {
	m.mu.Lock()
	defer m.mu.Unlock()
	return append([]agentic.ToolCall{}, m.calls...)
}

func TestRecordingExecutor_ExecuteAndRecord(t *testing.T) {
	store := NewInMemoryToolCallStore()
	executor := newMockExecutor()
	recorder := NewRecordingExecutor(executor, store, nil)

	call := agentic.ToolCall{
		ID:      "call-1",
		Name:    "test_tool",
		LoopID:  "loop-123",
		TraceID: "trace-abc",
	}

	result, err := recorder.Execute(context.Background(), call)
	require.NoError(t, err)
	assert.Equal(t, "call-1", result.CallID)
	assert.Equal(t, "mock result", result.Content)

	// Give recording goroutine time to process
	time.Sleep(50 * time.Millisecond)

	err = recorder.Stop(5 * time.Second)
	require.NoError(t, err)

	records := store.Records()
	require.Len(t, records, 1)
	assert.Equal(t, "call-1", records[0].Call.ID)
	assert.Equal(t, "loop-123", records[0].Call.LoopID)
	assert.Equal(t, "trace-abc", records[0].Call.TraceID)
	assert.True(t, records[0].Duration > 0)
}

func TestRecordingExecutor_ListTools(t *testing.T) {
	store := NewInMemoryToolCallStore()
	executor := newMockExecutor()
	recorder := NewRecordingExecutor(executor, store, nil)
	defer func() { _ = recorder.Stop(time.Second) }()

	tools := recorder.ListTools()
	require.Len(t, tools, 1)
	assert.Equal(t, "test_tool", tools[0].Name)
}

func TestRecordingExecutor_GracefulShutdown(t *testing.T) {
	store := NewInMemoryToolCallStore()
	executor := newMockExecutor()
	recorder := NewRecordingExecutor(executor, store, nil)

	// Execute multiple calls
	for i := 0; i < 10; i++ {
		call := agentic.ToolCall{
			ID:   "call-" + string(rune('0'+i)),
			Name: "test_tool",
		}
		_, err := recorder.Execute(context.Background(), call)
		require.NoError(t, err)
	}

	// Stop should wait for all records to be processed
	err := recorder.Stop(5 * time.Second)
	require.NoError(t, err)

	records := store.Records()
	assert.Len(t, records, 10)
}

func TestRecordingExecutor_BufferFull(t *testing.T) {
	// Create a store that blocks on Store
	blockingStore := &blockingStore{
		store:   NewInMemoryToolCallStore(),
		block:   make(chan struct{}),
		blocked: make(chan struct{}),
	}

	executor := newMockExecutor()
	recorder := NewRecordingExecutor(executor, blockingStore, nil)

	// Fill the buffer (100 calls) plus extras
	for i := 0; i < 150; i++ {
		call := agentic.ToolCall{
			ID:   "call-" + string(rune(i)),
			Name: "test_tool",
		}
		_, err := recorder.Execute(context.Background(), call)
		require.NoError(t, err)
	}

	// Unblock the store
	close(blockingStore.block)

	err := recorder.Stop(5 * time.Second)
	require.NoError(t, err)

	// Some records should have been dropped due to buffer full
	records := blockingStore.store.Records()
	assert.Less(t, len(records), 150)
}

// blockingStore wraps InMemoryToolCallStore to simulate slow storage
type blockingStore struct {
	store   *InMemoryToolCallStore
	block   chan struct{}
	blocked chan struct{}
	once    sync.Once
}

func (b *blockingStore) Store(ctx context.Context, record ToolCallRecord) error {
	b.once.Do(func() {
		close(b.blocked)
	})
	select {
	case <-b.block:
	case <-ctx.Done():
		return ctx.Err()
	}
	return b.store.Store(ctx, record)
}

func (b *blockingStore) Close() error {
	return b.store.Close()
}

// TestRecordingExecutor_ListToolsPreservesEffect pins that a WRAPPING executor
// passes tool-definition metadata through unchanged (gh#749).
//
// RecordingExecutor delegates ListTools to the wrapped executor today, so this
// holds by construction — which is exactly why it is asserted rather than left
// to inspection. A wrapper that ever reconstructs definitions field-by-field
// (to add a prefix, mark availability, annotate provenance) would silently drop
// Effect, and the loss would surface downstream as a catalog of "unknown" with
// no failing test pointing at the wrapper. Same shape as the discovery
// projection, which already silently dropped Strict and Paginated.
func TestRecordingExecutor_ListToolsPreservesEffect(t *testing.T) {
	store := NewInMemoryToolCallStore()
	executor := newMockExecutor()
	executor.tools = []agentic.ToolDefinition{
		{Name: "read_tool", Description: "reads", Effect: agentic.ToolEffectReadOnly},
		{Name: "write_tool", Description: "writes", Effect: agentic.ToolEffectMutating},
		{Name: "outside_tool", Description: "reaches out", Effect: agentic.ToolEffectExternal},
		{Name: "quiet_tool", Description: "declares nothing"},
	}
	recorder := NewRecordingExecutor(executor, store, nil)
	defer func() { _ = recorder.Stop(time.Second) }()

	got := recorder.ListTools()
	require.Len(t, got, len(executor.tools))

	for i, want := range executor.tools {
		assert.Equal(t, want.Name, got[i].Name)
		assert.Equal(t, want.Effect, got[i].Effect,
			"wrapping executor changed the effect of %q — a wrapper must pass tool metadata through unchanged", want.Name)
	}
}

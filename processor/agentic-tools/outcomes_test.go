package agentictools

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"strings"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/c360studio/semstreams/agentic"
	"github.com/c360studio/semstreams/message"
	"github.com/c360studio/semstreams/natsclient"
	"github.com/c360studio/semstreams/payloadbuiltins"
	"github.com/nats-io/nats.go"
	"github.com/nats-io/nats.go/jetstream"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type memoryOutcomeStore struct {
	mu        sync.Mutex
	values    map[string][]byte
	getErr    error
	createErr error
}

func (s *memoryOutcomeStore) Get(_ context.Context, key string) ([]byte, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.getErr != nil {
		return nil, s.getErr
	}
	value, ok := s.values[key]
	if !ok {
		return nil, jetstream.ErrKeyNotFound
	}
	return append([]byte(nil), value...), nil
}

func (s *memoryOutcomeStore) Create(_ context.Context, key string, value []byte) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.createErr != nil {
		return s.createErr
	}
	if _, ok := s.values[key]; ok {
		return jetstream.ErrKeyExists
	}
	s.values[key] = append([]byte(nil), value...)
	return nil
}

func TestPersistCompletedOutcomeDispositionTable(t *testing.T) {
	call := agentic.ToolCall{ID: "call", Name: "write", LoopID: "loop", TraceID: "trace"}
	result := agentic.ToolResult{CallID: call.ID, Name: call.Name, Content: "winner"}

	t.Run("new completion", func(t *testing.T) {
		store := &memoryOutcomeStore{values: make(map[string][]byte)}
		component := &Component{outcomes: store, logger: slog.Default()}
		winner, _, err := component.persistCompletedOutcome(context.Background(), call, result, outcomePathNew, false, true)
		require.NoError(t, err)
		assert.Equal(t, result, winner.Result)
	})

	t.Run("matching CAS winner", func(t *testing.T) {
		store := &memoryOutcomeStore{values: make(map[string][]byte)}
		component := &Component{outcomes: store, logger: slog.Default()}
		first, _, err := component.persistCompletedOutcome(context.Background(), call, result, outcomePathNew, false, true)
		require.NoError(t, err)
		loser := result
		loser.Content = "loser"
		winner, _, err := component.persistCompletedOutcome(context.Background(), call, loser, outcomePathNew, false, true)
		require.NoError(t, err)
		assert.Equal(t, first.Result, winner.Result, "CAS loser must publish the authoritative winner")
	})

	t.Run("transient create", func(t *testing.T) {
		want := errors.New("storage unavailable")
		store := &memoryOutcomeStore{values: make(map[string][]byte), createErr: want}
		component := &Component{outcomes: store, logger: slog.Default()}
		_, _, err := component.persistCompletedOutcome(context.Background(), call, result, outcomePathNew, false, true)
		require.ErrorIs(t, err, want)
		assert.False(t, isIrrecoverableOutcomeError(err))
	})

	t.Run("typed oversize compacts once", func(t *testing.T) {
		store := &oversizeOnceStore{memoryOutcomeStore: memoryOutcomeStore{values: make(map[string][]byte)}}
		component := &Component{outcomes: store, logger: slog.Default()}
		winner, _, err := component.persistCompletedOutcome(context.Background(), call, result, outcomePathNew, false, true)
		require.NoError(t, err)
		assert.Equal(t, "too_large", winner.Result.Error)
		assert.Equal(t, int32(2), store.creates.Load())
	})
}

type oversizeOnceStore struct {
	memoryOutcomeStore
	creates atomic.Int32
}

func (s *oversizeOnceStore) Create(ctx context.Context, key string, value []byte) error {
	if s.creates.Add(1) == 1 {
		return nats.ErrMaxPayload
	}
	return s.memoryOutcomeStore.Create(ctx, key, value)
}

func TestPersistCompletedOutcomeConcurrentCASConverges(t *testing.T) {
	store := &memoryOutcomeStore{values: make(map[string][]byte)}
	component := &Component{outcomes: store, logger: slog.Default()}
	call := agentic.ToolCall{ID: "same-call", Name: "write"}
	const replicas = 16
	results := make(chan agentic.ToolResult, replicas)
	errs := make(chan error, replicas)
	var wg sync.WaitGroup
	for i := range replicas {
		wg.Add(1)
		go func() {
			defer wg.Done()
			candidate := agentic.ToolResult{CallID: call.ID, Name: call.Name, Content: string(rune('a' + i))}
			winner, _, err := component.persistCompletedOutcome(context.Background(), call, candidate, outcomePathNew, false, true)
			errs <- err
			results <- winner.Result
		}()
	}
	wg.Wait()
	close(errs)
	close(results)
	for err := range errs {
		require.NoError(t, err)
	}
	var authoritative string
	for result := range results {
		if authoritative == "" {
			authoritative = result.Content
		}
		assert.Equal(t, authoritative, result.Content)
	}
}

type panicExecutor struct{}

func (panicExecutor) Execute(context.Context, agentic.ToolCall) (agentic.ToolResult, error) {
	panic("secret panic detail")
}
func (panicExecutor) ListTools() []agentic.ToolDefinition {
	return []agentic.ToolDefinition{{Name: "panic"}}
}

func TestExecuteWithPanicRecoveryProducesCompactInternalResult(t *testing.T) {
	component := &Component{registry: NewExecutorRegistry(), logger: slog.Default(), config: DefaultConfig()}
	require.NoError(t, component.registry.RegisterTool("panic", panicExecutor{}))
	call := agentic.ToolCall{ID: "call", Name: "panic", LoopID: "loop", TraceID: "trace"}
	result, err := component.executeWithPanicRecovery(context.Background(), call)
	require.NoError(t, err)
	assert.Equal(t, agentic.ToolErrorInternal, result.ErrorKind)
	assert.Equal(t, call.ID, result.CallID)
	assert.NotContains(t, result.Error, "secret")
}

type countingExecutor struct{ calls atomic.Int32 }

func (e *countingExecutor) Execute(_ context.Context, call agentic.ToolCall) (agentic.ToolResult, error) {
	e.calls.Add(1)
	return agentic.ToolResult{CallID: call.ID, Name: call.Name, Content: "executed"}, nil
}

func TestApprovalGateSameIDRedispatchExecutesOnceAndPublishesTerminalResult(t *testing.T) {
	store := &memoryOutcomeStore{values: make(map[string][]byte)}
	executor := &countingExecutor{}
	decoder := payloadbuiltins.NewTestDecoder(t)
	component := &Component{
		config: DefaultConfig(), registry: NewExecutorRegistry(), decoder: decoder,
		logger: slog.Default(), outcomes: store, approvalFilter: NewApprovalFilter([]string{"count"}),
	}
	require.NoError(t, component.registry.RegisterTool("count", executor))
	type publication struct {
		msgID  string
		result agentic.ToolResult
	}
	var publications []publication
	component.publishStream = func(_ context.Context, _ string, data []byte, msgID string) error {
		base, err := decoder.Decode(data)
		require.NoError(t, err)
		result, ok := base.Payload().(*agentic.ToolResult)
		require.True(t, ok)
		publications = append(publications, publication{msgID: msgID, result: *result})
		return nil
	}
	wireCall := func(call agentic.ToolCall) []byte {
		base := message.NewBaseMessage(call.Schema(), &call, "loop")
		data, err := json.Marshal(base)
		require.NoError(t, err)
		return data
	}

	initial := agentic.ToolCall{ID: "same-id", Name: "count", LoopID: "loop", TraceID: "trace"}
	require.NoError(t, component.handleToolCall(context.Background(), wireCall(initial)))
	assert.Equal(t, int32(0), executor.calls.Load())
	assert.Empty(t, store.values, "approval pause must not become a COMPLETED outcome")
	require.Len(t, publications, 1)
	assert.True(t, agentic.IsApprovalRequired(publications[0].result.Error))
	assert.Equal(t, toolApprovalRequiredMessageID(initial.ID), publications[0].msgID)

	approved := initial
	approved.ApprovedBy = "alice@example.com"
	require.NoError(t, component.handleToolCall(context.Background(), wireCall(approved)))
	assert.Equal(t, int32(1), executor.calls.Load())
	require.Len(t, publications, 2)
	assert.Equal(t, "executed", publications[1].result.Content)
	assert.Equal(t, toolResultMessageID(initial.ID), publications[1].msgID)
	assert.NotEqual(t, publications[0].msgID, publications[1].msgID,
		"approval pause dedup identity must not suppress the terminal result")
	assert.Len(t, store.values, 1, "only the terminal approved result is durable")

	require.NoError(t, component.handleToolCall(context.Background(), wireCall(approved)))
	assert.Equal(t, int32(1), executor.calls.Load(), "terminal redelivery must replay")
	assert.Len(t, publications, 3)
	assert.Equal(t, publications[1].result, publications[2].result)
}
func (e *countingExecutor) ListTools() []agentic.ToolDefinition {
	return []agentic.ToolDefinition{{Name: "count", Parameters: map[string]any{"type": "object"}}}
}

func TestHandleToolCallPublishFailureReplaysWithoutExecutor(t *testing.T) {
	store := &memoryOutcomeStore{values: make(map[string][]byte)}
	executor := &countingExecutor{}
	component := &Component{
		config: DefaultConfig(), registry: NewExecutorRegistry(), decoder: payloadbuiltins.NewTestDecoder(t),
		logger: slog.Default(), outcomes: store,
	}
	require.NoError(t, component.registry.RegisterTool("count", executor))
	var publishes atomic.Int32
	var observedMsgID string
	component.publishStream = func(_ context.Context, _ string, _ []byte, msgID string) error {
		observedMsgID = msgID
		if publishes.Add(1) == 1 {
			return errors.New("first PubAck failed")
		}
		return nil
	}
	call := agentic.ToolCall{ID: "durable-replay", Name: "count", LoopID: "loop", TraceID: "trace"}
	base := message.NewBaseMessage(call.Schema(), &call, "test")
	data, err := json.Marshal(base)
	require.NoError(t, err)

	err = component.handleToolCall(context.Background(), data)
	require.Error(t, err, "first publication failure must delayed-NAK")
	assert.Equal(t, int32(1), executor.calls.Load())
	err = component.handleToolCall(context.Background(), data)
	require.NoError(t, err)
	assert.Equal(t, int32(1), executor.calls.Load(), "durable replay must not invoke executor")
	assert.Equal(t, int32(2), publishes.Load())
	assert.Equal(t, toolResultMessageID(call.ID), observedMsgID)
}

func TestHandleToolCallPermanentDispositionTable(t *testing.T) {
	t.Run("malformed", func(t *testing.T) {
		component := &Component{decoder: payloadbuiltins.NewTestDecoder(t), logger: slog.Default()}
		err := component.handleToolCall(context.Background(), []byte("not-json"))
		var permanent *natsclient.PermanentDeliveryError
		assert.ErrorAs(t, err, &permanent)
	})

	t.Run("same key mismatched call", func(t *testing.T) {
		store := &memoryOutcomeStore{values: make(map[string][]byte)}
		original := agentic.ToolCall{ID: "collision", Name: "original"}
		record, err := newCompletedOutcome(original, agentic.ToolResult{CallID: original.ID, Name: original.Name})
		require.NoError(t, err)
		data, err := marshalCompletedOutcome(record)
		require.NoError(t, err)
		store.values[toolCallOutcomeKey(original.ID)] = data

		component := &Component{
			config: DefaultConfig(), registry: NewExecutorRegistry(), decoder: payloadbuiltins.NewTestDecoder(t),
			logger: slog.Default(), outcomes: store,
		}
		changed := original
		changed.Name = "different"
		base := message.NewBaseMessage(changed.Schema(), &changed, "test")
		wire, err := json.Marshal(base)
		require.NoError(t, err)
		err = component.handleToolCall(context.Background(), wire)
		var permanent *natsclient.PermanentDeliveryError
		assert.ErrorAs(t, err, &permanent)
	})
}

func TestToolCallOutcomeIdentityV1(t *testing.T) {
	call := agentic.ToolCall{
		ID: "call-123", Name: "lookup", LoopID: "loop-1", TraceID: "trace-1", ApprovedBy: "operator",
		Arguments: map[string]any{"z": float64(2), "a": map[string]any{"y": true, "x": "v"}},
		Metadata:  map[string]any{"b": []any{"x", float64(1)}, "a": "first"},
	}

	key := toolCallOutcomeKey(call.ID)
	assert.Equal(t, "v1."+strings.TrimPrefix(toolResultMessageID(call.ID), "tool-result/v1/"), key)
	assert.NotContains(t, key, "=")
	assert.Equal(t, strings.ToLower(key), key)

	want, err := toolCallFingerprintV1(call)
	require.NoError(t, err)
	assert.Regexp(t, `^sha256:[0-9a-f]{64}$`, want)
	reordered := call
	reordered.Arguments = map[string]any{"a": map[string]any{"x": "v", "y": true}, "z": float64(2)}
	reordered.Metadata = map[string]any{"a": "first", "b": []any{"x", float64(1)}}
	got, err := toolCallFingerprintV1(reordered)
	require.NoError(t, err)
	assert.Equal(t, want, got, "map insertion order must not change the canonical fingerprint")

	mutations := []func(*agentic.ToolCall){
		func(c *agentic.ToolCall) { c.ID += "x" },
		func(c *agentic.ToolCall) { c.Name += "x" },
		func(c *agentic.ToolCall) { c.Arguments = map[string]any{"different": true} },
		func(c *agentic.ToolCall) { c.Metadata = map[string]any{"different": true} },
		func(c *agentic.ToolCall) { c.LoopID += "x" },
		func(c *agentic.ToolCall) { c.TraceID += "x" },
		func(c *agentic.ToolCall) { c.ApprovedBy += "x" },
	}
	for i, mutate := range mutations {
		changed := call
		mutate(&changed)
		fingerprint, fingerprintErr := toolCallFingerprintV1(changed)
		require.NoError(t, fingerprintErr)
		assert.NotEqual(t, want, fingerprint, "mutation %d must affect the fingerprint", i)
	}
}

func TestDecodeCompletedOutcomeValidatesImmutableIdentity(t *testing.T) {
	call := agentic.ToolCall{ID: "same", Name: "read", Arguments: map[string]any{"x": "y"}}
	result := agentic.ToolResult{CallID: call.ID, Name: call.Name, Content: "authoritative"}
	record, err := newCompletedOutcome(call, result)
	require.NoError(t, err)
	data, err := marshalCompletedOutcome(record)
	require.NoError(t, err)

	decoded, err := decodeCompletedOutcome(data, call)
	require.NoError(t, err)
	assert.Equal(t, result, decoded.Result)

	tests := []struct {
		name   string
		mutate func(*completedOutcome)
	}{
		{"version", func(o *completedOutcome) { o.Version = "v2" }},
		{"call ID", func(o *completedOutcome) { o.CallID = "collision" }},
		{"fingerprint", func(o *completedOutcome) { o.Fingerprint = "bad" }},
		{"result correlation", func(o *completedOutcome) { o.Result.CallID = "other" }},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			bad := record
			tt.mutate(&bad)
			encoded, encodeErr := marshalCompletedOutcome(bad)
			require.NoError(t, encodeErr)
			_, decodeErr := decodeCompletedOutcome(encoded, call)
			require.Error(t, decodeErr)
			assert.True(t, isIrrecoverableOutcomeError(decodeErr))
		})
	}

	_, err = decodeCompletedOutcome([]byte("not-json"), call)
	require.Error(t, err)
	assert.True(t, isIrrecoverableOutcomeError(err))
}

func TestCompactTooLargeResultDropsSensitiveAndSizeFields(t *testing.T) {
	call := agentic.ToolCall{ID: "call", Name: "huge", LoopID: "loop", TraceID: "trace"}
	original := agentic.ToolResult{
		CallID: call.ID, Name: call.Name, Content: "SECRET-CONTENT", Error: "SECRET-ERROR",
		Metadata: map[string]any{"size": 123, "nested": "SECRET"}, LoopID: call.LoopID, TraceID: call.TraceID,
	}
	compact := compactTooLargeResult(call)
	assert.Equal(t, call.ID, compact.CallID)
	assert.Equal(t, call.LoopID, compact.LoopID)
	assert.Equal(t, call.TraceID, compact.TraceID)
	assert.Equal(t, agentic.ToolErrorInternal, compact.ErrorKind)
	assert.Equal(t, "too_large", compact.Error)
	assert.Empty(t, compact.Content)
	assert.NotContains(t, compact.Error, original.Error)
	assert.Nil(t, compact.Metadata)

	encoded, err := marshalToolResult(compact)
	require.NoError(t, err)
	assert.NotContains(t, string(encoded), "SECRET")
	var envelope struct {
		Payload map[string]json.RawMessage `json:"payload"`
	}
	require.NoError(t, json.Unmarshal(encoded, &envelope))
	assert.NotContains(t, envelope.Payload, "metadata")
}

func TestObservedOversizeUsesTypedErrorsOnly(t *testing.T) {
	assert.True(t, isObservedOversize(nats.ErrMaxPayload))
	assert.True(t, isObservedOversize(jetstream.ErrMaxBytesExceeded))
	assert.False(t, isObservedOversize(errors.New(nats.ErrMaxPayload.Error())))
	assert.False(t, isObservedOversize(errors.New(jetstream.ErrMaxBytesExceeded.Error())))
}

func TestPublicationOversizeUsesOneCompactSurrogateWithoutReplacingAuthority(t *testing.T) {
	call := agentic.ToolCall{ID: "large-call", Name: "large", LoopID: "loop", TraceID: "trace"}
	full := agentic.ToolResult{CallID: call.ID, Name: call.Name, Content: strings.Repeat("x", 1024), LoopID: call.LoopID, TraceID: call.TraceID}
	decoder := payloadbuiltins.NewTestDecoder(t)
	component := &Component{config: DefaultConfig(), logger: slog.Default()}
	var attempts []agentic.ToolResult
	var msgIDs []string
	component.publishStream = func(_ context.Context, _ string, data []byte, msgID string) error {
		base, err := decoder.Decode(data)
		require.NoError(t, err)
		published := base.Payload().(*agentic.ToolResult)
		attempts = append(attempts, *published)
		msgIDs = append(msgIDs, msgID)
		if len(attempts) == 1 {
			return nats.ErrMaxPayload
		}
		return nil
	}

	require.NoError(t, component.publishCompletedResult(context.Background(), call, full, outcomePathNew))
	require.Len(t, attempts, 2)
	assert.Equal(t, full, attempts[0])
	assert.Equal(t, "too_large", attempts[1].Error)
	assert.Equal(t, agentic.ToolErrorInternal, attempts[1].ErrorKind)
	assert.Empty(t, attempts[1].Content)
	assert.Nil(t, attempts[1].Metadata)
	assert.Equal(t, call.ID, attempts[1].CallID)
	assert.Equal(t, call.LoopID, attempts[1].LoopID)
	assert.Equal(t, call.TraceID, attempts[1].TraceID)
	assert.Equal(t, []string{toolResultMessageID(call.ID), toolResultMessageID(call.ID)}, msgIDs)

	component.publishStream = func(context.Context, string, []byte, string) error { return nats.ErrMaxPayload }
	err := component.publishCompletedResult(context.Background(), call, full, outcomePathReplay)
	var permanent *natsclient.PermanentDeliveryError
	assert.ErrorAs(t, err, &permanent)
}

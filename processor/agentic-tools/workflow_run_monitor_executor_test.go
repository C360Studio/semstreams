package agentictools

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"strings"
	"testing"
	"time"

	"github.com/c360studio/semstreams/agentic"
	"github.com/c360studio/semstreams/natsclient"
)

type fakeLoopKV struct {
	entries map[string][]byte
	keysErr error
	getErr  map[string]error
}

func newFakeLoopKV() *fakeLoopKV {
	return &fakeLoopKV{
		entries: map[string][]byte{},
		getErr:  map[string]error{},
	}
}

func (f *fakeLoopKV) put(t *testing.T, key string, value any) {
	t.Helper()
	data, err := json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	f.entries[key] = data
}

func (f *fakeLoopKV) Keys(context.Context) ([]string, error) {
	if f.keysErr != nil {
		return nil, f.keysErr
	}
	keys := make([]string, 0, len(f.entries))
	for key := range f.entries {
		keys = append(keys, key)
	}
	return keys, nil
}

func (f *fakeLoopKV) Get(_ context.Context, key string) (*natsclient.KVEntry, error) {
	if err := f.getErr[key]; err != nil {
		return nil, err
	}
	value, ok := f.entries[key]
	if !ok {
		return nil, natsclient.ErrKVKeyNotFound
	}
	return &natsclient.KVEntry{Key: key, Value: value}, nil
}

func parseWorkflowRunResult(t *testing.T, content string) workflowRunMonitorResult {
	t.Helper()
	var result workflowRunMonitorResult
	if err := json.Unmarshal([]byte(content), &result); err != nil {
		t.Fatalf("decode result: %v\n%s", err, content)
	}
	return result
}

func TestWorkflowRunMonitorToolContract(t *testing.T) {
	executor := NewWorkflowRunMonitorExecutor(newFakeLoopKV(), slog.Default())
	tool := executor.ListTools()[0]
	if tool.Name != WorkflowRunMonitorToolName {
		t.Fatalf("name = %q", tool.Name)
	}
	required, _ := tool.Parameters["required"].([]string)
	if len(required) != 1 || required[0] != "workflow_slug" {
		t.Fatalf("required = %#v", required)
	}
	result, err := executor.Execute(context.Background(), agentic.ToolCall{Name: WorkflowRunMonitorToolName})
	if err != nil || !strings.Contains(result.Error, "workflow_slug") {
		t.Fatalf("missing slug result=%#v err=%v", result, err)
	}
}

func TestWorkflowRunMonitorAggregatesOnlyMatchingSlug(t *testing.T) {
	kv := newFakeLoopKV()
	base := time.Date(2026, 4, 20, 10, 0, 0, 0, time.UTC)
	kv.put(t, completeKeyPrefix+"success", agentic.LoopCompletedEvent{
		LoopID: "success", TaskID: "task-success", WorkflowSlug: "wanted", Outcome: agentic.OutcomeSuccess,
		Role: "worker", Iterations: 2, TokensIn: 10, TokensOut: 4, CompletedAt: base,
	})
	kv.put(t, completeKeyPrefix+"failed", agentic.LoopFailedEvent{
		LoopID: "failed", TaskID: "task-failed", WorkflowSlug: "wanted", Outcome: agentic.OutcomeFailed,
		Role: "worker", TokensIn: 3, FailedAt: base.Add(time.Minute),
	})
	kv.put(t, completeKeyPrefix+"cancelled", agentic.LoopCancelledEvent{
		LoopID: "cancelled", TaskID: "task-cancelled", WorkflowSlug: "wanted", Outcome: agentic.OutcomeCancelled,
		CancelledAt: base.Add(2 * time.Minute),
	})
	kv.put(t, completeKeyPrefix+"foreign", agentic.LoopCompletedEvent{
		LoopID: "foreign", TaskID: "task-foreign", WorkflowSlug: "other", Outcome: agentic.OutcomeSuccess,
		TokensIn: 999, CompletedAt: base,
	})

	executor := NewWorkflowRunMonitorExecutor(kv, slog.Default())
	toolResult, err := executor.Execute(context.Background(), agentic.ToolCall{
		ID: "call", Name: WorkflowRunMonitorToolName,
		Arguments: map[string]any{"workflow_slug": "wanted", "recent_limit": 2},
	})
	if err != nil || toolResult.Error != "" {
		t.Fatalf("result=%#v err=%v", toolResult, err)
	}
	result := parseWorkflowRunResult(t, toolResult.Content)
	if result.WorkflowSlug != "wanted" || result.TotalLoops != 3 {
		t.Fatalf("unexpected result: %#v", result)
	}
	if result.TotalTokensIn != 13 || result.TotalTokensOut != 4 || result.ByRole["worker"] != 2 {
		t.Fatalf("unexpected aggregates: %#v", result)
	}
	if len(result.Recent) != 2 || result.Recent[0].LoopID != "cancelled" || result.Recent[1].LoopID != "failed" {
		t.Fatalf("unexpected recent: %#v", result.Recent)
	}
}

func TestWorkflowRunMonitorPropagatesKVScanError(t *testing.T) {
	kv := newFakeLoopKV()
	kv.keysErr = errors.New("disconnected")
	executor := NewWorkflowRunMonitorExecutor(kv, slog.Default())
	result, err := executor.Execute(context.Background(), agentic.ToolCall{
		Name: WorkflowRunMonitorToolName, Arguments: map[string]any{"workflow_slug": "wanted"},
	})
	if err == nil || result.ErrorKind != agentic.ToolErrorNetwork {
		t.Fatalf("result=%#v err=%v", result, err)
	}
}

func TestWorkflowRunMonitorPropagatesPerKeyGetError(t *testing.T) {
	kv := newFakeLoopKV()
	key := completeKeyPrefix + "unreadable"
	kv.entries[key] = []byte(`{"workflow_slug":"wanted"}`)
	kv.getErr[key] = errors.New("read failed")

	executor := NewWorkflowRunMonitorExecutor(kv, slog.Default())
	result, err := executor.Execute(context.Background(), agentic.ToolCall{
		Name: WorkflowRunMonitorToolName, Arguments: map[string]any{"workflow_slug": "wanted"},
	})
	if err == nil || result.ErrorKind != agentic.ToolErrorNetwork || result.Content != "" {
		t.Fatalf("result=%#v err=%v", result, err)
	}
}

func TestWorkflowRunMonitorRejectsMalformedSlugMetadata(t *testing.T) {
	kv := newFakeLoopKV()
	kv.entries[completeKeyPrefix+"malformed-slug"] = []byte(
		`{"loop_id":"malformed-slug","task_id":"task","outcome":"success","workflow_slug":{},"completed_at":"2026-04-20T10:00:00Z"}`,
	)

	assertWorkflowRunMonitorDataFailure(t, kv)
}

func TestWorkflowRunMonitorRejectsMalformedMatchingTerminalEvent(t *testing.T) {
	kv := newFakeLoopKV()
	kv.entries[completeKeyPrefix+"malformed-event"] = []byte(
		`{"loop_id":"malformed-event","outcome":"success","workflow_slug":"wanted","completed_at":"2026-04-20T10:00:00Z"}`,
	)

	assertWorkflowRunMonitorDataFailure(t, kv)
}

func TestWorkflowRunMonitorRejectsUnknownMatchingOutcome(t *testing.T) {
	kv := newFakeLoopKV()
	kv.entries[completeKeyPrefix+"unknown"] = []byte(
		`{"loop_id":"unknown","task_id":"task","outcome":"mystery","workflow_slug":"wanted","completed_at":"2026-04-20T10:00:00Z"}`,
	)

	assertWorkflowRunMonitorDataFailure(t, kv)
}

func TestWorkflowRunMonitorSortsSameSecondByNanoseconds(t *testing.T) {
	kv := newFakeLoopKV()
	base := time.Date(2026, 4, 20, 10, 0, 0, 0, time.UTC)
	for _, event := range []agentic.LoopCompletedEvent{
		{LoopID: "earlier", TaskID: "task-earlier", WorkflowSlug: "wanted", Outcome: agentic.OutcomeSuccess, CompletedAt: base.Add(100 * time.Nanosecond)},
		{LoopID: "later", TaskID: "task-later", WorkflowSlug: "wanted", Outcome: agentic.OutcomeSuccess, CompletedAt: base.Add(900 * time.Nanosecond)},
	} {
		kv.put(t, completeKeyPrefix+event.LoopID, event)
	}

	result := executeWorkflowRunMonitor(t, kv)
	if len(result.Recent) != 2 || result.Recent[0].LoopID != "later" || result.Recent[1].LoopID != "earlier" {
		t.Fatalf("unexpected recent order: %#v", result.Recent)
	}
	if result.Recent[0].CompletedAt != "2026-04-20T10:00:00.0000009Z" {
		t.Fatalf("timestamp = %q", result.Recent[0].CompletedAt)
	}
}

func TestWorkflowRunMonitorBreaksExactTimestampTiesByLoopID(t *testing.T) {
	kv := newFakeLoopKV()
	terminalAt := time.Date(2026, 4, 20, 10, 0, 0, 500, time.UTC)
	for _, loopID := range []string{"loop-b", "loop-a"} {
		kv.put(t, completeKeyPrefix+loopID, agentic.LoopCompletedEvent{
			LoopID: loopID, TaskID: "task-" + loopID, WorkflowSlug: "wanted",
			Outcome: agentic.OutcomeSuccess, CompletedAt: terminalAt,
		})
	}

	result := executeWorkflowRunMonitor(t, kv)
	if len(result.Recent) != 2 || result.Recent[0].LoopID != "loop-a" || result.Recent[1].LoopID != "loop-b" {
		t.Fatalf("unexpected recent order: %#v", result.Recent)
	}
}

func assertWorkflowRunMonitorDataFailure(t *testing.T, kv *fakeLoopKV) {
	t.Helper()
	executor := NewWorkflowRunMonitorExecutor(kv, slog.Default())
	result, err := executor.Execute(context.Background(), agentic.ToolCall{
		Name: WorkflowRunMonitorToolName, Arguments: map[string]any{"workflow_slug": "wanted"},
	})
	if err == nil || result.Error == "" || result.Content != "" {
		t.Fatalf("result=%#v err=%v", result, err)
	}
}

func executeWorkflowRunMonitor(t *testing.T, kv *fakeLoopKV) workflowRunMonitorResult {
	t.Helper()
	executor := NewWorkflowRunMonitorExecutor(kv, slog.Default())
	toolResult, err := executor.Execute(context.Background(), agentic.ToolCall{
		Name: WorkflowRunMonitorToolName, Arguments: map[string]any{"workflow_slug": "wanted"},
	})
	if err != nil || toolResult.Error != "" {
		t.Fatalf("result=%#v err=%v", toolResult, err)
	}
	return parseWorkflowRunResult(t, toolResult.Content)
}

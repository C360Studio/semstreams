package agenticloop

import (
	"context"
	"encoding/json"
	"errors"
	"sync/atomic"
	"testing"
	"time"

	"github.com/c360studio/semstreams/agentic"
	"github.com/c360studio/semstreams/component"
	"github.com/c360studio/semstreams/message"
	"github.com/c360studio/semstreams/payloadbuiltins"
	"github.com/c360studio/semstreams/pkg/errs"
	"github.com/google/uuid"
	"github.com/prometheus/client_golang/prometheus/testutil"
)

func TestPreflightDecodedTaskLineageIdentitySemantics(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name        string
		loopID      string
		metadata    map[string]any
		wantLineage bool
		wantSameID  bool
	}{
		{
			// A caller-supplied identity is a framework-minted token the caller
			// received and echoed (ADR-105, #1192) — preflight must preserve it
			// rather than reserving a second one.
			name: "caller supplied identity is preserved", loopID: uuid.NewString(),
			metadata:    map[string]any{agentic.MetadataKeyRelatedLoops: map[string]any{"researcher": "upstream"}},
			wantLineage: true, wantSameID: true,
		},
		{
			name:        "generated identity uses loop manager UUID semantics",
			metadata:    map[string]any{agentic.MetadataKeyRelatedLoops: map[string]any{"researcher": "upstream"}},
			wantLineage: true,
		},
		{name: "no lineage does not reserve or mutate identity"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			component := newLineagePreflightComponent()
			task := validLineageTask("task-" + test.name)
			task.LoopID = test.loopID
			task.Metadata = test.metadata

			_, hasLineage, err := component.preflightDecodedTask(&task)
			if err != nil {
				t.Fatal(err)
			}
			if hasLineage != test.wantLineage {
				t.Fatalf("hasLineage = %v, want %v", hasLineage, test.wantLineage)
			}
			if test.wantSameID && task.LoopID != test.loopID {
				t.Fatalf("LoopID = %q, want caller identity %q", task.LoopID, test.loopID)
			}
			if test.wantLineage && !test.wantSameID {
				if _, err := uuid.Parse(task.LoopID); err != nil {
					t.Fatalf("generated LoopID %q is not UUID: %v", task.LoopID, err)
				}
			}
			if !test.wantLineage && task.LoopID != "" {
				t.Fatalf("no-lineage preflight mutated LoopID to %q", task.LoopID)
			}
			if _, exists := component.handler.loopManager.HasActiveLoopForTask(task.TaskID); exists {
				t.Fatal("pure preflight created persistent loop state")
			}
		})
	}
}

func TestPreflightDecodedTaskRejectsMalformedLineageWithoutLoopCreation(t *testing.T) {
	t.Parallel()
	component := newLineagePreflightComponent()
	task := validLineageTask("task-invalid-lineage")
	task.Metadata = map[string]any{
		agentic.MetadataKeyRelatedLoops: map[string]any{
			"researcher": "upstream",
			"reviewer":   42,
		},
	}

	_, _, err := component.preflightDecodedTask(&task)
	if err == nil || !errs.IsInvalid(err) {
		t.Fatalf("preflight error = %v, want typed invalid rejection", err)
	}
	if task.LoopID != "" {
		t.Fatalf("malformed metadata reserved LoopID %q before validation", task.LoopID)
	}
	if _, exists := component.handler.loopManager.HasActiveLoopForTask(task.TaskID); exists {
		t.Fatal("malformed preflight created loop state")
	}
}

func TestPreflightGeneratedIdentityPreservesHandleTaskDedup(t *testing.T) {
	t.Parallel()
	component := newLineagePreflightComponent()
	firstTask := validLineageTask("task-redelivery")
	firstTask.Metadata = map[string]any{
		agentic.MetadataKeyRelatedLoops: map[string]any{"researcher": "upstream"},
	}
	if _, _, err := component.preflightDecodedTask(&firstTask); err != nil {
		t.Fatal(err)
	}
	first, err := component.handler.HandleTask(context.Background(), firstTask)
	if err != nil || !first.Created {
		t.Fatalf("first HandleTask = %#v, %v", first, err)
	}

	redelivery := validLineageTask("task-redelivery")
	redelivery.Metadata = firstTask.Metadata
	if _, _, err := component.preflightDecodedTask(&redelivery); err != nil {
		t.Fatal(err)
	}
	if redelivery.LoopID == firstTask.LoopID {
		t.Fatal("independent redelivery unexpectedly reused an unpersisted prospective UUID")
	}
	second, err := component.handler.HandleTask(context.Background(), redelivery)
	if err != nil {
		t.Fatal(err)
	}
	if second.Created || second.LoopID != first.LoopID {
		t.Fatalf("dedup result = %#v, want existing loop %q", second, first.LoopID)
	}
}

func TestInvalidDecodedLineageTerminatesOnceAndHasNoBusinessSideEffects(t *testing.T) {
	configJSON, err := json.Marshal(DefaultConfig())
	if err != nil {
		t.Fatal(err)
	}
	discoverable, err := NewComponent(configJSON, component.Dependencies{
		Platform:        component.PlatformMeta{Org: "acme", Platform: "ops"},
		PayloadRegistry: payloadbuiltins.NewTestRegistry(t),
	})
	if err != nil {
		t.Fatal(err)
	}
	component := discoverable.(*Component)

	task := validLineageTask("task-permanent-invalid")
	task.Metadata = map[string]any{
		agentic.MetadataKeyRelatedLoops: map[string]any{
			"researcher": "upstream",
		},
	}
	envelope := message.NewBaseMessage(task.Schema(), &task, "test")
	data, err := json.Marshal(envelope)
	if err != nil {
		t.Fatal(err)
	}
	// BaseMessage correctly refuses to marshal invalid payloads. Simulate an
	// untrusted producer by corrupting the already-valid wire representation so
	// the decoded-task intake boundary must reject it.
	var wire map[string]any
	if err := json.Unmarshal(data, &wire); err != nil {
		t.Fatal(err)
	}
	payload := wire["payload"].(map[string]any)
	metadata := payload["metadata"].(map[string]any)
	related := metadata[agentic.MetadataKeyRelatedLoops].(map[string]any)
	related["bad_key"] = "other"
	data, err = json.Marshal(wire)
	if err != nil {
		t.Fatal(err)
	}
	msg := &inputAckMsg{data: data}

	beforeRejected := testutil.ToFloat64(component.metrics.taskIntakeRejections.WithLabelValues(
		taskIntakeRejectionLane, taskIntakeRejectionReason))
	beforeCreated := testutil.ToFloat64(component.metrics.loopsCreated)
	err = consumeLongRunningInput(context.Background(), msg, time.Hour,
		component.taskInputHandler(time.Minute))
	if err == nil || !errs.IsInvalid(err) {
		t.Fatalf("consume error = %v, want typed invalid rejection", err)
	}

	if !msg.terminated.Load() || msg.acked.Load() || msg.naked.Load() {
		t.Fatalf("delivery ack state: term=%v ack=%v nak=%v, want Term only",
			msg.terminated.Load(), msg.acked.Load(), msg.naked.Load())
	}
	afterRejected := testutil.ToFloat64(component.metrics.taskIntakeRejections.WithLabelValues(
		taskIntakeRejectionLane, taskIntakeRejectionReason))
	if afterRejected-beforeRejected != 1 {
		t.Fatalf("rejection metric delta = %v, want exactly 1", afterRejected-beforeRejected)
	}
	if delta := testutil.ToFloat64(component.metrics.loopsCreated) - beforeCreated; delta != 0 {
		t.Fatalf("loops-created success metric delta = %v, want 0", delta)
	}
	if _, exists := component.handler.loopManager.HasActiveLoopForTask(task.TaskID); exists {
		t.Fatal("permanently invalid intake created loop business state")
	}
}

func TestTransientLineageWriteNAKsThenResumesPendingSpawnOnRedelivery(t *testing.T) {
	configJSON, err := json.Marshal(DefaultConfig())
	if err != nil {
		t.Fatal(err)
	}
	discoverable, err := NewComponent(configJSON, component.Dependencies{
		Platform:        component.PlatformMeta{Org: "acme", Platform: "ops"},
		PayloadRegistry: payloadbuiltins.NewTestRegistry(t),
	})
	if err != nil {
		t.Fatal(err)
	}
	component := discoverable.(*Component)

	var attempts atomic.Int32
	component.testLineageWriteHook = func(context.Context, string, map[string]any) error {
		if attempts.Add(1) == 1 {
			return errs.WrapTransient(errors.New("injected NATS timeout"),
				"agentic-loop", "WriteLineageTriples", "write batch")
		}
		return nil
	}

	task := validLineageTask("task-transient-lineage")
	task.Metadata = map[string]any{
		agentic.MetadataKeyRelatedLoops: map[string]any{"researcher": "upstream-loop"},
	}
	envelope := message.NewBaseMessage(task.Schema(), &task, "test")
	data, err := json.Marshal(envelope)
	if err != nil {
		t.Fatal(err)
	}

	beforeCreated := testutil.ToFloat64(component.metrics.loopsCreated)
	first := &inputAckMsg{data: data}
	err = consumeLongRunningInput(context.Background(), first, time.Hour,
		component.taskInputHandler(time.Minute))
	if err == nil || !errs.IsTransient(err) {
		t.Fatalf("first delivery error = %v, want transient error", err)
	}
	if first.acked.Load() || !first.naked.Load() || first.terminated.Load() {
		t.Fatalf("first delivery ack state: ack=%v nak=%v term=%v, want NAK only",
			first.acked.Load(), first.naked.Load(), first.terminated.Load())
	}
	loopID, active := component.handler.loopManager.HasActiveLoopForTask(task.TaskID)
	if !active {
		t.Fatal("transient lineage failure removed the pending loop before redelivery")
	}
	if _, ok := component.pendingTaskResult(task.TaskID, loopID); !ok {
		t.Fatal("transient lineage failure did not retain the unpublished spawn result")
	}
	if delta := testutil.ToFloat64(component.metrics.loopsCreated) - beforeCreated; delta != 0 {
		t.Fatalf("loops-created metric after failed attempt = %v, want 0", delta)
	}

	second := &inputAckMsg{data: data}
	if err := consumeLongRunningInput(context.Background(), second, time.Hour,
		component.taskInputHandler(time.Minute)); err != nil {
		t.Fatalf("redelivery error = %v", err)
	}
	if !second.acked.Load() || second.naked.Load() || second.terminated.Load() {
		t.Fatalf("redelivery ack state: ack=%v nak=%v term=%v, want ACK only",
			second.acked.Load(), second.naked.Load(), second.terminated.Load())
	}
	if attempts.Load() != 2 {
		t.Fatalf("lineage write attempts = %d, want 2", attempts.Load())
	}
	if _, ok := component.pendingTaskResult(task.TaskID, loopID); ok {
		t.Fatal("successful redelivery retained stale pending spawn state")
	}
	if delta := testutil.ToFloat64(component.metrics.loopsCreated) - beforeCreated; delta != 1 {
		t.Fatalf("loops-created metric delta = %v, want exactly 1", delta)
	}
}

func newLineagePreflightComponent() *Component {
	return &Component{
		handler: NewMessageHandler(DefaultConfig()),
		deps: component.Dependencies{
			Platform: component.PlatformMeta{Org: "acme", Platform: "ops"},
		},
	}
}

func validLineageTask(taskID string) agentic.TaskMessage {
	return agentic.TaskMessage{TaskID: taskID, Role: "researcher", Model: "model", Prompt: "prompt"}
}

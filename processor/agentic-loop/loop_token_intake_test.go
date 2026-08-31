package agenticloop

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/c360studio/semstreams/agentic"
	"github.com/c360studio/semstreams/component"
	"github.com/c360studio/semstreams/message"
	"github.com/c360studio/semstreams/payloadbuiltins"
	"github.com/c360studio/semstreams/pkg/errs"
	"github.com/prometheus/client_golang/prometheus/testutil"
)

// TestNonUUIDLoopIDIsTerminatedAtIntake is the consume-side half of the
// loop-token contract (ADR-105, #1192): a producer that pre-fills a
// hand-authored loop_id must meet the LOUD lane — the intake-rejection counter
// and TerminateDelivery — not the swallowed one. A CreateLoopWithID failure
// surfacing later inside HandleTask is logged and ACKed with no metric, which is
// precisely why the refusal's home is preflight, upstream of it.
func TestNonUUIDLoopIDIsTerminatedAtIntake(t *testing.T) {
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
	comp := discoverable.(*Component)

	// Build a valid envelope, then corrupt the wire. BaseMessage refuses to
	// marshal an invalid payload, so an untrusted producer is simulated the same
	// way the lineage intake test does it: rewrite the already-valid JSON.
	task := validLineageTask("task-non-uuid-loop")
	envelope := message.NewBaseMessage(task.Schema(), &task, "test")
	data, err := json.Marshal(envelope)
	if err != nil {
		t.Fatal(err)
	}
	var wire map[string]any
	if err := json.Unmarshal(data, &wire); err != nil {
		t.Fatal(err)
	}
	payload, ok := wire["payload"].(map[string]any)
	if !ok {
		t.Fatalf("envelope payload shape = %T, want object", wire["payload"])
	}
	const authoredToken = "workflow-7"
	payload["loop_id"] = authoredToken
	data, err = json.Marshal(wire)
	if err != nil {
		t.Fatal(err)
	}

	beforeRejected := testutil.ToFloat64(comp.metrics.taskIntakeRejections.WithLabelValues(
		taskIntakeRejectionLane, taskIntakeRejectionReason))
	beforeCreated := testutil.ToFloat64(comp.metrics.loopsCreated)

	msg := &inputAckMsg{data: data}
	err = consumeLongRunningInput(context.Background(), msg, time.Hour,
		comp.taskInputHandler(time.Minute))
	if err == nil || !errs.IsInvalid(err) {
		t.Fatalf("consume error = %v, want typed invalid rejection", err)
	}
	if !msg.terminated.Load() || msg.acked.Load() || msg.naked.Load() {
		t.Fatalf("delivery ack state: term=%v ack=%v nak=%v, want Term only",
			msg.terminated.Load(), msg.acked.Load(), msg.naked.Load())
	}

	afterRejected := testutil.ToFloat64(comp.metrics.taskIntakeRejections.WithLabelValues(
		taskIntakeRejectionLane, taskIntakeRejectionReason))
	if delta := afterRejected - beforeRejected; delta != 1 {
		t.Fatalf("intake-rejection metric delta = %v, want exactly 1", delta)
	}
	if delta := testutil.ToFloat64(comp.metrics.loopsCreated) - beforeCreated; delta != 0 {
		t.Fatalf("loops-created metric delta = %v, want 0", delta)
	}
	if _, err := comp.handler.loopManager.GetLoop(authoredToken); err == nil {
		t.Fatalf("refused intake registered loop state for %q", authoredToken)
	}
	if cm := comp.handler.loopManager.GetContextManager(authoredToken); cm != nil {
		t.Fatalf("refused intake created a context manager for %q", authoredToken)
	}
	if _, active := comp.handler.loopManager.HasActiveLoopForTask(task.TaskID); active {
		t.Fatal("refused intake created loop business state")
	}
}

// TestCanonicalLoopIDPassesIntake is the positive control: the intake refusal
// must reject the shape, not the lane. A framework-minted token on the same
// wire path is accepted and creates its loop.
func TestCanonicalLoopIDPassesIntake(t *testing.T) {
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
	comp := discoverable.(*Component)

	task := validLineageTask("task-canonical-loop")
	task.LoopID = comp.handler.loopManager.GenerateLoopID()
	envelope := message.NewBaseMessage(task.Schema(), &task, "test")
	data, err := json.Marshal(envelope)
	if err != nil {
		t.Fatal(err)
	}

	decoded, err := comp.decoder.Decode(data)
	if err != nil {
		t.Fatalf("decode: %v", err)
	}
	decodedTask, ok := decoded.Payload().(*agentic.TaskMessage)
	if !ok {
		t.Fatalf("payload type = %T, want *agentic.TaskMessage", decoded.Payload())
	}
	if _, _, err := comp.preflightDecodedTask(decodedTask); err != nil {
		t.Fatalf("preflight refused a framework-minted token: %v", err)
	}
}

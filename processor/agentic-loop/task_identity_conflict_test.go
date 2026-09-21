package agenticloop

import (
	"testing"
	"time"

	"github.com/c360studio/semstreams/agentic"
	"github.com/c360studio/semstreams/component"
	"github.com/c360studio/semstreams/internal/deliverylane"
	"github.com/c360studio/semstreams/natsclient"
	"github.com/stretchr/testify/require"
)

// The proposal's own sentence is "One TaskID naming two LoopIDs SHALL
// quarantine", and until this the intake returned the existing loop on TaskID
// alone — so a task that named a DIFFERENT loop was answered with a loop it
// never asked for, and the delivery was acknowledged as if the work had
// started. Both outcomes are wrong and neither is recoverable by redelivery:
// the disagreement is in the message.
//
// Driven through the production delivery policy rather than the handler alone,
// because "quarantines" is a settlement claim: the error class only matters if
// the lane reads it, and the lane used to swallow every intake error and ACK.
//
// spec: agentic-loop / Loop task, request, and tool work use only required correlation
func TestTaskNamingADifferentLoopThanItsTaskIDQuarantines(t *testing.T) {
	const (
		taskID  = "task-conflict"
		loopA   = "3f2504e0-4f89-41d3-9a0c-0305e82c3301"
		loopB   = "6ba7b810-9dad-11d1-80b4-00c04fd430c8"
		lane    = "agent.task"
		timeout = time.Minute
	)

	newLaneWithRunningTask := func(t *testing.T) (*Component, *MessageHandler) {
		t.Helper()
		h := NewMessageHandler(DefaultConfig())
		c := releaseTestComponent(t, h)
		// Lineage preflight builds a prospective loop-execution entity ID from
		// the platform meta, so a component without one terminates every
		// lineage task before the identity comparison this test is about.
		c.deps.Platform = component.PlatformMeta{Org: "acme", Platform: "ops"}
		// The loop this TaskID is already running under.
		_, err := h.loopManager.CreateLoopWithID(loopA, taskID, "researcher", "model", 5)
		require.NoError(t, err)
		existing, active := h.loopManager.HasActiveLoopForTask(taskID)
		require.True(t, active, "the fixture must have a live loop for the task, or nothing discriminates")
		require.Equal(t, loopA, existing)
		return c, h
	}

	deliver := func(t *testing.T, c *Component, task agentic.TaskMessage) (natsclient.DeliveryResult, *loopDeliveryOwnerMsg) {
		t.Helper()
		msg := &loopDeliveryOwnerMsg{data: baseMessageBytes(t, &task)}
		result, admitted := deliverylane.Consume(t.Context(), msg,
			heartbeatPolicyForTest(t, lane, c.taskInputHandler(timeout)), deliverylane.NewAdmission(nil, nil))
		require.True(t, admitted)
		return result, msg
	}

	t.Run("a task naming a different loop stops the delivery", func(t *testing.T) {
		c, h := newLaneWithRunningTask(t)

		result, msg := deliver(t, c, agentic.TaskMessage{
			TaskID: taskID, LoopID: loopB, Role: "researcher", Model: "model", Prompt: "prompt",
		})

		require.Equal(t, natsclient.DeliveryDecisionQuarantine, result.Decision(),
			"one task identity naming two loops is not a redelivery and cannot be acknowledged")
		require.Zero(t, msg.acks.Load()+msg.naks.Load()+msg.terms.Load(),
			"a quarantined delivery is settled by its owner, not by this callback")
		require.ErrorContains(t, result.Cause(), loopA, "the cause must name the loop that is running")
		require.ErrorContains(t, result.Cause(), loopB, "and the loop the message named")

		// And the conflicting token did not become a loop on the way past.
		_, err := h.loopManager.GetLoop(loopB)
		require.Error(t, err, "the refused task registered loop state for the token it named")
		existing, active := h.loopManager.HasActiveLoopForTask(taskID)
		require.True(t, active)
		require.Equal(t, loopA, existing, "the running loop must be untouched by the refusal")
	})

	t.Run("the same task with no loop token is still an ordinary redelivery", func(t *testing.T) {
		// L1's intake exemption: a task that names no loop is the dedup case
		// this branch has always served, and it keeps its ACK.
		c, _ := newLaneWithRunningTask(t)

		result, msg := deliver(t, c, agentic.TaskMessage{
			TaskID: taskID, Role: "researcher", Model: "model", Prompt: "prompt",
		})

		require.Equal(t, natsclient.DeliveryDecisionAck, result.Decision(),
			"a redelivery of the same work is deduplicated, not refused")
		require.Equal(t, int32(1), msg.acks.Load())
	})

	t.Run("the same task naming the loop it is already running is a redelivery too", func(t *testing.T) {
		c, _ := newLaneWithRunningTask(t)

		result, _ := deliver(t, c, agentic.TaskMessage{
			TaskID: taskID, LoopID: loopA, Role: "researcher", Model: "model", Prompt: "prompt",
		})

		require.Equal(t, natsclient.DeliveryDecisionAck, result.Decision(),
			"agreement between the message and the running loop is the opposite of a conflict")
	})

	// The reason the comparison reads the token the PRODUCER sent rather than
	// task.LoopID. A lineage task that names no loop has a fresh prospective
	// UUID reserved for it on EVERY delivery (preflightDecodedTask), so by the
	// time the field is read the second delivery carries a different token than
	// the first — a redelivery that would classify itself as a conflict.
	t.Run("a lineage task that named no loop is deduplicated across deliveries", func(t *testing.T) {
		c, h := newLaneWithRunningTask(t)
		lineage := agentic.TaskMessage{
			TaskID: taskID, Role: "researcher", Model: "model", Prompt: "prompt",
			Metadata: map[string]any{
				agentic.MetadataKeyRelatedLoops: map[string]any{"researcher": "upstream"},
			},
		}

		result, msg := deliver(t, c, lineage)

		require.Equal(t, natsclient.DeliveryDecisionAck, result.Decision(),
			"intake's own per-delivery UUID must never read as a producer naming a second loop")
		require.Equal(t, int32(1), msg.acks.Load())
		existing, active := h.loopManager.HasActiveLoopForTask(taskID)
		require.True(t, active)
		require.Equal(t, loopA, existing, "the redelivery was deduplicated to the running loop")
	})
}

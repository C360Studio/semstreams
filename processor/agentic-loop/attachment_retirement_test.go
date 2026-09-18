package agenticloop

import (
	"maps"
	"testing"
	"time"

	"github.com/c360studio/semstreams/agentic"
	"github.com/c360studio/semstreams/natsclient"
	"github.com/c360studio/semstreams/pkg/errs"
	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// This finite history matrix covers warm partial birth, warm retained authority,
// and cold retained authority, with same-task replay and different-task reuse.
// It does not model concurrent delivery or detection after evidence expiry.
// spec: agentic-loop / A task owns one execution without rebinding
// spec: agentic-loop / All six loop input classes settle after owner-specific durable done
func TestTaskIntakeCannotRebindKnownLoop(t *testing.T) {
	for _, residence := range []string{"warm_partial_birth", "warm_retained", "cold_retained"} {
		for _, state := range []agentic.LoopState{
			agentic.LoopStateRunning, agentic.LoopStateAwaitingApproval,
			agentic.LoopStateComplete, agentic.LoopStateFailed, agentic.LoopStateCancelled,
		} {
			for _, sameTask := range []bool{false, true} {
				// A process-local terminal without durable authority is not an
				// applied-state proof; this slice preserves the durable replay path.
				if residence == "warm_partial_birth" && state.IsTerminal() && sameTask {
					continue
				}
				name := residence + "/" + string(state) + "/" + map[bool]string{false: "different_task", true: "same_task"}[sameTask]
				t.Run(name, func(t *testing.T) {
					h := fenceHandler(t)
					task := agentic.TaskMessage{
						LoopID: uuid.NewString(), TaskID: "task-A", Role: "general", Model: "model-a", Prompt: "A's prompt",
					}
					first, err := h.HandleTask(t.Context(), task)
					require.NoError(t, err)
					request := historyRequest(t, first)
					cm := h.loopManager.GetContextManager(task.LoopID)
					require.NoError(t, cm.AddMessage(RegionRecentHistory, agentic.ChatMessage{Role: "assistant", Content: "A's conversation"}))
					outcome := map[agentic.LoopState]string{
						agentic.LoopStateComplete: agentic.OutcomeSuccess, agentic.LoopStateFailed: agentic.OutcomeFailed,
						agentic.LoopStateCancelled: agentic.OutcomeCancelled,
					}[state]
					if state == agentic.LoopStateAwaitingApproval {
						entity, getErr := h.GetLoop(task.LoopID)
						require.NoError(t, getErr)
						require.NoError(t, entity.BeginAwaitingApproval("call-A", "tool-A", nil, "approval", time.Minute, ""))
						require.NoError(t, h.loopManager.UpdateLoop(entity))
					} else if state.IsTerminal() {
						require.NoError(t, h.loopManager.TransitionLoop(task.LoopID, state))
						require.NoError(t, h.loopManager.UpdateCompletion(task.LoopID, outcome, "A's selected answer", ""))
					}
					entity, err := h.GetLoop(task.LoopID)
					require.NoError(t, err)
					beforeEntity := settlementLoopRecord(t, entity)
					beforeContext := cm.GetContext()
					beforeRequests := maps.Clone(h.loopManager.requestToLoop)
					bucket := &settlementBucket{values: map[string][]byte{}}
					if residence != "warm_partial_birth" {
						bucket.values[task.LoopID] = beforeEntity
						if state.IsTerminal() {
							bucket.values["COMPLETE_"+task.LoopID] = settlementEnvelope(t, &agentic.LoopCompletedEvent{
								LoopID: task.LoopID, TaskID: task.TaskID, Outcome: outcome, Result: "A's selected answer",
							})
						}
					}
					beforeDurable := maps.Clone(bucket.values)
					if residence == "cold_retained" {
						h = fenceHandler(t)
					}
					c := releaseTestComponent(t, h)
					c.loopsBucket = bucket
					c.settlementEvidence = &settlementEvidence{
						request: retainedLoopMessage{subject: "agent.request." + task.LoopID, data: settlementEnvelope(t, &request)}, requestFound: true,
					}
					if !sameTask {
						task.TaskID = "task-B"
						task.Prompt = "B must not take over"
					}
					msg := &loopDeliveryOwnerMsg{data: settlementEnvelope(t, &task)}
					result, admitted := consumeAdmittedDelivery(
						t.Context(), msg, task4HeartbeatPolicy(t, "agent.task", c.taskInputHandler(time.Minute)), newDeliveryLaneAdmission(nil),
					)
					require.True(t, admitted)
					if sameTask {
						assert.NoError(t, result.Err())
						assert.Equal(t, natsclient.DeliveryDecisionAck, result.Decision())
						assert.Equal(t, int32(1), msg.acks.Load())
					} else {
						assert.True(t, errs.IsFatal(result.Err()), "different-task collision must be fatal correlation: %v", result.Err())
						assert.Equal(t, natsclient.DeliveryDecisionQuarantine, result.Decision())
						assert.Zero(t, msg.acks.Load()+msg.naks.Load()+msg.terms.Load(), "a collision must not settle the source")
					}
					assert.Zero(t, msg.naks.Load()+msg.terms.Load())
					assert.Equal(t, beforeDurable, bucket.values, "intake changed A's authority or selected COMPLETE")
					if residence != "cold_retained" {
						after, getErr := h.GetLoop(task.LoopID)
						require.NoError(t, getErr)
						assert.Equal(t, beforeEntity, settlementLoopRecord(t, after), "intake rebound or mutated A")
						assert.Same(t, cm, h.loopManager.GetContextManager(task.LoopID))
						assert.Equal(t, beforeContext, cm.GetContext(), "intake appended B's prompt")
						assert.Equal(t, beforeRequests, h.loopManager.requestToLoop, "intake generated task-B provider work")
					} else if !sameTask || state.IsTerminal() {
						assert.Empty(t, h.loopManager.loops, "refusal or terminal replay installed new work")
						assert.Empty(t, h.loopManager.requestToLoop)
					} else {
						restored, getErr := h.GetLoop(task.LoopID)
						require.NoError(t, getErr)
						assert.Equal(t, entity.TaskID, restored.TaskID)
						assert.Equal(t, entity.State, restored.State)
						assert.Equal(t, task.LoopID, h.loopManager.requestToLoop[request.RequestID], "same-task recovery lost its retained request")
					}
				})
			}
		}
	}
}

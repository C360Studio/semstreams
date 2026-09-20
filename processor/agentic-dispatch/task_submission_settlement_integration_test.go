//go:build integration

package agenticdispatch

import (
	"encoding/json"
	"io"
	"log/slog"
	"strings"
	"testing"
	"time"

	"github.com/c360studio/semstreams/agentic"
	"github.com/c360studio/semstreams/message"
	"github.com/c360studio/semstreams/metric"
	"github.com/c360studio/semstreams/natsclient"
	"github.com/c360studio/semstreams/payloadregistry"
	"github.com/prometheus/client_golang/prometheus/testutil"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// A task publication that succeeded followed by a response publication that did
// not is the one shape a unit seam cannot produce: both go through the same
// client, so an unconnected one fails the task first and never reaches the
// acknowledgement. Here a real broker carries the AGENT stream and no USER
// stream at all, so `agent.task.<id>` receives a PubAck and
// `user.response.http.<channel>` has no stream to answer it.
//
// The classification is the point, and its reason moved under #1328. Retry was
// the old answer, and L1 refused it because a redelivered UserMessage minted a
// NEW task UUID and published it with no deduplication id — accepted work
// executed twice. Stable task identity removes exactly that effect: the
// redelivery reads its own committed task back and republishes it under the
// same TaskID and LoopID, which is what downstream deduplication keys on.
// Quarantine still stands, on the effects identity does not cover — the
// redelivery replaces the tracked LoopInfo, resetting a loop that has since
// advanced, and counts the submission a second time. The subtests are the pair:
// the first delivery quarantines with exactly one task on the stream, and the
// counterfactual drives the redelivery Retry would have asked for and observes
// both halves, the identity that is recovered and the tracking that is not.
//
// spec: agentic-dispatch / Every dispatch durable input settles through its owner
func TestIntegrationPublishedTaskWithFailedResponseQuarantines(t *testing.T) {
	newComponent := func(t *testing.T) (*Component, *natsclient.TestClient) {
		t.Helper()
		// AGENT exists; USER deliberately does not.
		tc := natsclient.NewTestClient(t, natsclient.WithStreams(
			natsclient.TestStreamConfig{Name: "AGENT", Subjects: []string{"agent.>"}},
		))
		logger := slog.New(slog.NewTextHandler(io.Discard, nil))
		reg := payloadregistry.NewWithSubset(t, agentic.RegisterPayloads)
		c := &Component{
			config:        DefaultConfig(),
			decoder:       message.NewDecoder(reg),
			logger:        logger,
			loopTracker:   NewLoopTrackerWithLogger(logger),
			metrics:       getMetrics(metric.NewMetricsRegistry()),
			modelRegistry: newTestRegistry(),
			natsClient:    tc.Client,
		}
		c.config.AutoContinue = false
		return c, tc
	}
	userMessageBytes := func(t *testing.T, messageID string) []byte {
		t.Helper()
		msg := &agentic.UserMessage{
			MessageID:   messageID,
			ChannelType: "http",
			ChannelID:   "session-quarantine",
			UserID:      "operator-1",
			Content:     "run the analysis",
		}
		data, err := json.Marshal(message.NewBaseMessage(msg.Schema(), msg, "test"))
		require.NoError(t, err)
		return data
	}
	tasksOnStream := func(t *testing.T, tc *natsclient.TestClient) []agentic.TaskMessage {
		t.Helper()
		stream, err := tc.Client.GetStream(t.Context(), "AGENT")
		require.NoError(t, err)
		info, err := stream.Info(t.Context())
		require.NoError(t, err)
		tasks := []agentic.TaskMessage{}
		for seq := info.State.FirstSeq; seq <= info.State.LastSeq && info.State.Msgs > 0; seq++ {
			raw, err := stream.GetMsg(t.Context(), seq)
			require.NoError(t, err)
			var envelope struct {
				Payload agentic.TaskMessage `json:"payload"`
			}
			require.NoError(t, json.Unmarshal(raw.Data, &envelope))
			tasks = append(tasks, envelope.Payload)
		}
		return tasks
	}

	t.Run("the first delivery quarantines with one task on the stream", func(t *testing.T) {
		c, tc := newComponent(t)

		decision, err := c.handleUserMessage(t.Context(), userMessageBytes(t, "msg-1"))

		require.Equal(t, natsclient.DeliveryDecisionQuarantine, decision,
			"a published task whose acknowledgement failed cannot ask for a redelivery")
		require.Error(t, err)
		require.ErrorContains(t, err, "acknowledgement is not")
		tasks := tasksOnStream(t, tc)
		require.Len(t, tasks, 1, "no second task is published for one user message")
	})

	t.Run("the counterfactual: a redelivery recovers the identity but re-enters the tracking", func(t *testing.T) {
		c, tc := newComponent(t)
		data := userMessageBytes(t, "msg-1")

		first, err := c.handleUserMessage(t.Context(), data)
		require.Equal(t, natsclient.DeliveryDecisionQuarantine, first)
		require.Error(t, err)

		// The loop advances, as it would while an unacknowledged delivery
		// waited to be redelivered. Without this the reset below is invisible.
		tracked := c.loopTracker.GetAllLoops()
		require.Len(t, tracked, 1)
		loopID := tracked[0].LoopID
		c.loopTracker.UpdateState(loopID, "exploring")

		// Exactly what Retry would have caused: the same bytes again.
		second, err := c.handleUserMessage(t.Context(), data)
		require.Equal(t, natsclient.DeliveryDecisionQuarantine, second)
		require.Error(t, err)

		tasks := tasksOnStream(t, tc)
		require.Len(t, tasks, 2, "the redelivery published a second copy")
		require.Equal(t, tasks[0].TaskID, tasks[1].TaskID,
			"#1328: the redelivery republishes the committed task identity, which downstream deduplicates")
		require.Equal(t, tasks[0].LoopID, tasks[1].LoopID,
			"the committed LoopID is recovered, so no second loop is created")

		// The effects identity does not cover, and the reason Quarantine stands.
		require.Equal(t, "pending", c.loopTracker.Get(loopID).State,
			"the redelivery replaced the tracked LoopInfo, resetting a loop that had advanced")
		assert.Equal(t, float64(2), testutil.ToFloat64(c.metrics.tasksSubmitted),
			"the redelivery counted one submission twice")
	})
}

// The command lane's post-effect response failure is the counterpart decision
// to the task lane's, and it goes the other way. `/cancel` publishes its signal
// at commands.go:185 and then builds its success response, so a failed response
// publication is also a failure after an effect — but its redelivery is
// effect-free, and the user has been told nothing, so Retry is what actually
// delivers their answer.
//
// Effect-free has two mechanisms and this test holds the first: on redelivery
// the gate re-reads the loop from merged facts, finds it terminal once the
// cancel took effect, and answers "already settled" without publishing
// anything. The second is the loop's own cancel owner, which drops a signal
// naming an already-terminal loop effect-free (the loop delta's scenario *A
// cancel signal names a loop that cannot be cancelled*), and covers the race
// where a redelivery beats the loop's settlement.
//
// spec: agentic-dispatch / Every dispatch durable input settles through its owner
func TestIntegrationPublishedCancelWithFailedResponseRetries(t *testing.T) {
	const cancelLoopID = "0e0b3f52-6f4a-4c6f-9c3f-1f2d3a4b5c6d"

	newComponent := func(t *testing.T) (*Component, *natsclient.TestClient) {
		t.Helper()
		// AGENT captures agent.signal.<loop>; USER deliberately does not exist,
		// so the user response has no stream to answer it.
		tc := natsclient.NewTestClient(t, natsclient.WithStreams(
			natsclient.TestStreamConfig{Name: "AGENT", Subjects: []string{"agent.>"}},
		))
		logger := slog.New(slog.NewTextHandler(io.Discard, nil))
		reg := payloadregistry.NewWithSubset(t, agentic.RegisterPayloads)
		c := &Component{
			config:        DefaultConfig(),
			decoder:       message.NewDecoder(reg),
			logger:        logger,
			loopTracker:   NewLoopTrackerWithLogger(logger),
			metrics:       getMetrics(metric.NewMetricsRegistry()),
			modelRegistry: newTestRegistry(),
			natsClient:    tc.Client,
			registry:      NewCommandRegistry(),
		}
		c.config.Permissions.CancelOwn = true
		c.registerBuiltinCommands()
		c.loopTracker.Track(&LoopInfo{
			LoopID:      cancelLoopID,
			TaskID:      "task-cancel",
			UserID:      "operator-1",
			ChannelType: "http",
			ChannelID:   "session-cancel",
			State:       "executing",
			CreatedAt:   time.Now(),
		})
		return c, tc
	}
	persistLoopState := func(c *Component, state agentic.LoopState) {
		withPersistedLoops(c, map[string]*agentic.LoopEntity{cancelLoopID: {
			ID:            cancelLoopID,
			UserID:        "operator-1",
			ChannelType:   "http",
			ChannelID:     "session-cancel",
			State:         state,
			MaxIterations: 5,
		}})
	}
	cancelCommandBytes := func(t *testing.T) []byte {
		t.Helper()
		msg := &agentic.UserMessage{
			MessageID:   "msg-cancel",
			ChannelType: "http",
			ChannelID:   "session-cancel",
			UserID:      "operator-1",
			Content:     "/cancel " + cancelLoopID,
		}
		data, err := json.Marshal(message.NewBaseMessage(msg.Schema(), msg, "test"))
		require.NoError(t, err)
		return data
	}
	signalsOnStream := func(t *testing.T, tc *natsclient.TestClient) []agentic.UserSignal {
		t.Helper()
		stream, err := tc.Client.GetStream(t.Context(), "AGENT")
		require.NoError(t, err)
		info, err := stream.Info(t.Context())
		require.NoError(t, err)
		signals := []agentic.UserSignal{}
		for seq := info.State.FirstSeq; seq <= info.State.LastSeq && info.State.Msgs > 0; seq++ {
			raw, err := stream.GetMsg(t.Context(), seq)
			require.NoError(t, err)
			if !strings.HasPrefix(raw.Subject, "agent.signal.") {
				continue
			}
			var envelope struct {
				Payload agentic.UserSignal `json:"payload"`
			}
			require.NoError(t, json.Unmarshal(raw.Data, &envelope))
			signals = append(signals, envelope.Payload)
		}
		return signals
	}

	t.Run("a published cancel whose response failed retries", func(t *testing.T) {
		c, tc := newComponent(t)
		persistLoopState(c, agentic.LoopStateExecuting)

		decision, err := c.handleUserMessage(t.Context(), cancelCommandBytes(t))

		require.Equal(t, natsclient.DeliveryDecisionRetry, decision,
			"the user was told nothing and the redelivery is effect-free, so this is not the task lane's case")
		require.Error(t, err)
		require.ErrorContains(t, err, "publish user response")
		signals := signalsOnStream(t, tc)
		require.Len(t, signals, 1, "the cancel signal was published before the response failed")
		require.Equal(t, cancelLoopID, signals[0].LoopID)
	})

	t.Run("the redelivery publishes no second signal once the loop has settled", func(t *testing.T) {
		c, tc := newComponent(t)
		persistLoopState(c, agentic.LoopStateExecuting)

		first, err := c.handleUserMessage(t.Context(), cancelCommandBytes(t))
		require.Equal(t, natsclient.DeliveryDecisionRetry, first)
		require.Error(t, err)

		// What the first signal causes: the loop cancels and records it. The
		// redelivery Retry asked for now meets a terminal loop.
		c.loopTracker.Remove(cancelLoopID)
		persistLoopState(c, agentic.LoopStateCancelled)

		second, err := c.handleUserMessage(t.Context(), cancelCommandBytes(t))
		require.Equal(t, natsclient.DeliveryDecisionRetry, second,
			"the response publication still fails, so the delivery is still owed")
		require.Error(t, err)

		require.Len(t, signalsOnStream(t, tc), 1,
			"the redelivery published a second cancel signal: Retry is only safe while it is effect-free")
	})
}

// The HTTP submission lane's label value, asserted once. It needs a broker for
// the same reason the task lane above does: the task publish has to succeed
// before the response copy can be the thing that fails, and an unconnected
// client fails the task first and never reaches it. The accepted operation is
// unchanged — that is the whole reason this lane observes rather than settles.
//
// spec: agentic-dispatch / Every dispatch durable input settles through its owner
func TestIntegrationHTTPSubmissionResponseFailureNamesItsOwnLane(t *testing.T) {
	tc := natsclient.NewTestClient(t, natsclient.WithStreams(
		natsclient.TestStreamConfig{Name: "AGENT", Subjects: []string{"agent.>"}},
	))
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	c := &Component{
		config:        DefaultConfig(),
		decoder:       message.NewDecoder(payloadregistry.NewWithSubset(t, agentic.RegisterPayloads)),
		logger:        logger,
		loopTracker:   NewLoopTrackerWithLogger(logger),
		metrics:       getMetrics(metric.NewMetricsRegistry()),
		modelRegistry: newTestRegistry(),
		natsClient:    tc.Client,
	}

	resp := c.processTaskSubmissionSync(t.Context(), agentic.UserMessage{
		MessageID:   "msg-http-submission",
		ChannelType: "http",
		ChannelID:   "session-http",
		UserID:      "operator-1",
		Content:     "run the analysis",
	})

	require.Equal(t, agentic.ResponseTypeStatus, resp.Type,
		"the submission was accepted and its synchronous answer must stand")
	require.Equal(t, 1, testutil.CollectAndCount(c.metrics.responsePublishFailures),
		"exactly one lane series moved")
	require.InDelta(t, 1.0,
		testutil.ToFloat64(c.metrics.responsePublishFailures.WithLabelValues(responseLaneHTTPSubmission)), 0.0001)
}

// Bare `/cancel` is the supported form whose target the message does NOT
// carry: handleCommand resolves it from the tracker (component.go:941-951),
// and that resolution is not stable across a redelivery. GetActiveLoop prefers
// the channel's loop only while it is non-terminal, then falls back to the
// user's most recent one (loop_tracker.go:204-226) — so the effect the first
// delivery had, loop A now terminal, is exactly what makes the redelivery
// resolve to a different live loop B and cancel it.
//
// The redelivery here is conditional on the decision on purpose: that is what
// production does. A quarantined delivery is never redelivered — the lane
// latches and refuses further work — while a Retry is redelivered by
// JetStream. So the assertion "B was never signalled" is a claim about the
// classification, not about the test harness declining to call the function.
//
// spec: agentic-dispatch / Every dispatch durable input settles through its owner
func TestIntegrationBareCancelWithFailedResponseQuarantines(t *testing.T) {
	const (
		loopA = "0e0b3f52-6f4a-4c6f-9c3f-1f2d3a4b5c6d"
		loopB = "7a1d9c24-3b5e-4f81-9d0a-2c6b4e8f1a37"
	)

	// AGENT captures agent.signal.<loop>; USER deliberately does not exist, so
	// the acknowledgement has no stream to answer it.
	tc := natsclient.NewTestClient(t, natsclient.WithStreams(
		natsclient.TestStreamConfig{Name: "AGENT", Subjects: []string{"agent.>"}},
	))
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	c := &Component{
		config:        DefaultConfig(),
		decoder:       message.NewDecoder(payloadregistry.NewWithSubset(t, agentic.RegisterPayloads)),
		logger:        logger,
		loopTracker:   NewLoopTrackerWithLogger(logger),
		metrics:       getMetrics(metric.NewMetricsRegistry()),
		modelRegistry: newTestRegistry(),
		natsClient:    tc.Client,
		registry:      NewCommandRegistry(),
	}
	c.config.Permissions.CancelOwn = true
	// The bare form only resolves a target at all when auto-continue is on,
	// which is the default.
	require.True(t, c.config.AutoContinue, "the bare form needs the default auto_continue")
	c.registerBuiltinCommands()

	// A is this channel's loop; B is the same user's newer loop elsewhere, so
	// the tracker's user fallback points at B from the start.
	track := func(loopID, channelID string) {
		c.loopTracker.Track(&LoopInfo{
			LoopID: loopID, TaskID: "task-" + loopID, UserID: "operator-1",
			ChannelType: "http", ChannelID: channelID, State: "executing", CreatedAt: time.Now(),
		})
	}
	track(loopA, "session-a")
	track(loopB, "session-b")
	persisted := map[string]*agentic.LoopEntity{
		loopA: {
			ID: loopA, UserID: "operator-1", ChannelType: "http", ChannelID: "session-a",
			State: agentic.LoopStateExecuting, MaxIterations: 5,
		},
		loopB: {
			ID: loopB, UserID: "operator-1", ChannelType: "http", ChannelID: "session-b",
			State: agentic.LoopStateExecuting, MaxIterations: 5,
		},
	}
	withPersistedLoops(c, persisted)

	bareCancel, err := json.Marshal(message.NewBaseMessage(
		(&agentic.UserMessage{}).Schema(),
		&agentic.UserMessage{
			MessageID: "msg-bare-cancel", ChannelType: "http", ChannelID: "session-a",
			UserID: "operator-1", Content: "/cancel",
		}, "test"))
	require.NoError(t, err)

	signalledLoops := func(t *testing.T) []string {
		t.Helper()
		stream, err := tc.Client.GetStream(t.Context(), "AGENT")
		require.NoError(t, err)
		info, err := stream.Info(t.Context())
		require.NoError(t, err)
		loops := []string{}
		for seq := info.State.FirstSeq; seq <= info.State.LastSeq && info.State.Msgs > 0; seq++ {
			raw, err := stream.GetMsg(t.Context(), seq)
			require.NoError(t, err)
			if !strings.HasPrefix(raw.Subject, "agent.signal.") {
				continue
			}
			var envelope struct {
				Payload agentic.UserSignal `json:"payload"`
			}
			require.NoError(t, json.Unmarshal(raw.Data, &envelope))
			loops = append(loops, envelope.Payload.LoopID)
		}
		return loops
	}

	decision, err := c.handleUserMessage(t.Context(), bareCancel)

	// assert, not require: a wrong decision must not skip the consequence
	// assertion at the bottom, which is the one that says what the
	// misclassification costs.
	assert.Equal(t, natsclient.DeliveryDecisionQuarantine, decision,
		"the message does not name the loop this delivery cancelled, so its redelivery is not effect-free")
	require.Error(t, err)
	require.Equal(t, []string{loopA}, signalledLoops(t), "the first delivery cancelled this channel's loop")

	// Production redelivers a Retry and never redelivers a Quarantine.
	if decision == natsclient.DeliveryDecisionRetry {
		// What the first delivery's effect does to the next resolution.
		c.loopTracker.UpdateState(loopA, "cancelled")
		persisted[loopA].State = agentic.LoopStateCancelled
		_, err = c.handleUserMessage(t.Context(), bareCancel)
		require.Error(t, err)
	}

	require.NotContains(t, signalledLoops(t), loopB,
		"a redelivery re-resolved the target and cancelled a loop the user never named")
}

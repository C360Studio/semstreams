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
	"github.com/stretchr/testify/require"
)

// A task publication that succeeded followed by a response publication that did
// not is the one shape a unit seam cannot produce: both go through the same
// client, so an unconnected one fails the task first and never reaches the
// acknowledgement. Here a real broker carries the AGENT stream and no USER
// stream at all, so `agent.task.<id>` receives a PubAck and
// `user.response.http.<channel>` has no stream to answer it.
//
// The classification is the point. Retry was the old answer, and a redelivered
// UserMessage mints a NEW task UUID (component.go:1093) and publishes it with no
// deduplication id — accepted work executed twice. The subtests are the pair:
// the first delivery quarantines with exactly one task on the stream, and the
// counterfactual drives the redelivery Retry would have asked for and observes
// the second task it publishes under a different identity.
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

	t.Run("the counterfactual: a redelivery publishes a second task under a new identity", func(t *testing.T) {
		c, tc := newComponent(t)
		data := userMessageBytes(t, "msg-1")

		first, err := c.handleUserMessage(t.Context(), data)
		require.Equal(t, natsclient.DeliveryDecisionQuarantine, first)
		require.Error(t, err)
		// Exactly what Retry would have caused: the same bytes again.
		second, err := c.handleUserMessage(t.Context(), data)
		require.Equal(t, natsclient.DeliveryDecisionQuarantine, second)
		require.Error(t, err)

		tasks := tasksOnStream(t, tc)
		require.Len(t, tasks, 2, "the redelivery published a second task")
		require.NotEqual(t, tasks[0].TaskID, tasks[1].TaskID,
			"the second task carries a new TaskID, so nothing downstream can deduplicate it")
		require.NotEqual(t, tasks[0].LoopID, tasks[1].LoopID,
			"with auto_continue=false the redelivery also creates a second loop")
	})
}

// The command lane's post-effect response failure is the counterpart decision
// to the task lane's, and it goes the other way. `/cancel` publishes its signal
// at commands.go:179 and then builds its success response, so a failed response
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

//go:build integration

package agenticdispatch

import (
	"encoding/json"
	"io"
	"log/slog"
	"testing"

	"github.com/c360studio/semstreams/agentic"
	"github.com/c360studio/semstreams/message"
	"github.com/c360studio/semstreams/metric"
	"github.com/c360studio/semstreams/natsclient"
	"github.com/c360studio/semstreams/payloadregistry"
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

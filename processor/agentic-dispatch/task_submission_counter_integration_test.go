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
	"github.com/prometheus/client_golang/prometheus/testutil"
	"github.com/stretchr/testify/require"
)

// TestTaskSubmissionCounterIsAtLeastOnceUnderRedelivery is docket OQ4's ruling
// as a test (owner ruling 2026-09-22): `tasks_submitted_total` is documented as
// at-least-once rather than armed into an exactly-once count. A reader of the
// metric who takes it for a distinct-task count is reading it wrong, and a
// requirement nothing asserts is a requirement nothing keeps.
//
// The GIVEN is the ORDINARY path: both streams exist, the first delivery
// acknowledges cleanly, and the second is the same bytes again — what a source
// redelivery hands a process. That is deliberately not the shape
// `TestIntegrationPublishedTaskWithFailedResponseQuarantines`
// (`task_submission_settlement_integration_test.go`) already covers, where the
// same second increment is observed as one cost of a quarantine with no USER
// stream. The counter's semantics must not depend on that failure being
// present, so this asserts them where nothing failed.
//
// Integration rather than unit, deliberately: both publications on this path go
// through the NATS client, so a component without a live one fails the task
// publication and never reaches the counter. The alternative was a production
// publish seam that exists only for a test.
//
// spec: agentic-dispatch / The task submission counter is at-least-once under redelivery
func TestTaskSubmissionCounterIsAtLeastOnceUnderRedelivery(t *testing.T) {
	tc := natsclient.NewTestClient(t, natsclient.WithStreams(
		natsclient.TestStreamConfig{Name: "AGENT", Subjects: []string{"agent.>"}},
		natsclient.TestStreamConfig{Name: "USER", Subjects: []string{"user.>"}},
	))
	c := &Component{
		config:        DefaultConfig(),
		decoder:       message.NewDecoder(payloadregistry.NewWithSubset(t, agentic.RegisterPayloads)),
		logger:        slog.New(slog.NewTextHandler(io.Discard, nil)),
		metrics:       getMetrics(metric.NewMetricsRegistry()),
		modelRegistry: newTestRegistry(),
		natsClient:    tc.Client,
	}
	// The retained task is what the redelivery recovers its identity from, so
	// no continuation inference may run ahead of it.
	c.config.AutoContinue = false

	msg := &agentic.UserMessage{
		MessageID:   "msg-at-least-once",
		ChannelType: "http",
		ChannelID:   "session-at-least-once",
		UserID:      "operator-1",
		Content:     "run the analysis",
	}
	data, err := json.Marshal(message.NewBaseMessage(msg.Schema(), msg, "test"))
	require.NoError(t, err)

	first, cause := c.handleUserMessage(t.Context(), data)
	require.Equal(t, natsclient.DeliveryDecisionAck, first)
	require.NoError(t, cause)
	require.Equal(t, float64(1), testutil.ToFloat64(c.metrics.tasksSubmitted))

	// Exactly what a source redelivery is: the same bytes again.
	second, cause := c.handleUserMessage(t.Context(), data)
	require.Equal(t, natsclient.DeliveryDecisionAck, second)
	require.NoError(t, cause)

	require.Equal(t, float64(2), testutil.ToFloat64(c.metrics.tasksSubmitted),
		"the counter is a submission-ATTEMPT signal; nothing suppresses the second increment and no arm was added")

	// Otherwise idempotent: the redelivery read its own committed task back and
	// republished THAT, so both copies are one logical task under one loop. No
	// second task was minted, and dispatch created no loop either time —
	// AGENT_LOOPS is agentic-loop's to write.
	tasks := submittedTasks(t, tc)
	require.Len(t, tasks, 2, "the redelivery republishes the retained task; there is no dedup id on this publish")
	require.Equal(t, tasks[0].TaskID, tasks[1].TaskID, "one UserMessage must not become two logical tasks")
	require.Equal(t, tasks[0].LoopID, tasks[1].LoopID, "the retained LoopID is reused, so no second loop is created")
	require.Equal(t, stableDispatchTaskID(*msg), tasks[1].TaskID,
		"the republished identity is derived from the source message, not minted again")
}

// submittedTasks decodes every task the AGENT stream retains, through the same
// envelope shape dispatch publishes.
func submittedTasks(t *testing.T, tc *natsclient.TestClient) []agentic.TaskMessage {
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

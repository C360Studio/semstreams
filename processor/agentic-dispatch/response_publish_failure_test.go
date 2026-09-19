package agenticdispatch

import (
	"bytes"
	"context"
	"log/slog"
	"testing"
	"time"

	"github.com/c360studio/semstreams/agentic"
	"github.com/c360studio/semstreams/metric"
	"github.com/c360studio/semstreams/natsclient"
	"github.com/prometheus/client_golang/prometheus/testutil"
	"github.com/stretchr/testify/require"
)

// sendResponse returns its publication error now, because the lanes that settle
// a delivery on that response have to classify it. The two HTTP lanes cannot —
// their caller already holds the same response synchronously and the operation
// is accepted — so the error had nowhere to go and was dropped at the call,
// taking with it the diagnostic the old log-only sendResponse emitted. A
// USER-stream capacity rejection is the case that made this concrete: it is
// deliberately circuit-neutral at the client, so nothing else would have seen
// it.
//
// spec: agentic-dispatch / Every dispatch durable input settles through its owner
func TestHTTPResponsePublicationFailureIsObservedWithoutChangingTheResult(t *testing.T) {
	var logged bytes.Buffer
	registry := metric.NewMetricsRegistry()
	c := &Component{
		config:   DefaultConfig(),
		logger:   slog.New(slog.NewTextHandler(&logged, &slog.HandlerOptions{Level: slog.LevelDebug})),
		metrics:  getMetrics(registry),
		registry: NewCommandRegistry(),
		// Constructed, never connected: every publish refuses with
		// ErrNotConnected before it touches a socket.
		natsClient:  &natsclient.Client{},
		loopTracker: NewLoopTracker(),
	}
	require.NoError(t, c.registry.Register("echo", CommandConfig{Pattern: `^/echo$`},
		func(_ context.Context, msg agentic.UserMessage, _ []string, _ string) (agentic.UserResponse, error) {
			return agentic.UserResponse{
				ResponseID:  "response-echo",
				ChannelType: msg.ChannelType,
				ChannelID:   msg.ChannelID,
				UserID:      msg.UserID,
				Type:        agentic.ResponseTypeText,
				Content:     "echo",
				Timestamp:   time.Now(),
			}, nil
		}))

	resp := c.processCommandSync(t.Context(), agentic.UserMessage{
		MessageID:   "msg-echo",
		ChannelType: "http",
		ChannelID:   "session-echo",
		UserID:      "operator-1",
		Content:     "/echo",
	})

	require.Equal(t, agentic.ResponseTypeText, resp.Type,
		"the HTTP command was executed and its answer must stand")
	require.Equal(t, "echo", resp.Content)
	require.Equal(t, 1, testutil.CollectAndCount(c.metrics.responsePublishFailures),
		"an unpublished async response must be counted")
	require.InDelta(t, 1.0,
		testutil.ToFloat64(c.metrics.responsePublishFailures.WithLabelValues(responseLaneHTTPCommand)), 0.0001)
	require.Contains(t, logged.String(), "Failed to publish response",
		"the diagnostic the old log-only path emitted must survive the returned error")
	require.Contains(t, logged.String(), responseLaneHTTPCommand,
		"the log must name the lane the response was lost on")
}

// The loop user-channel copy is the third lane label, and it is the one whose
// call site is void by signature: before this change it discarded the error
// with nothing at all, which is the silence the other two at least used to log
// from inside sendResponse. Its label is asserted here rather than left to the
// other two lanes' tests, because a label value nothing observes is a metric
// nobody can trust the shape of.
//
// spec: agentic-dispatch / Every dispatch durable input settles through its owner
func TestLoopUserChannelResponseFailureNamesItsOwnLane(t *testing.T) {
	var logged bytes.Buffer
	c := &Component{
		config:  DefaultConfig(),
		logger:  slog.New(slog.NewTextHandler(&logged, &slog.HandlerOptions{Level: slog.LevelDebug})),
		metrics: getMetrics(metric.NewMetricsRegistry()),
		// Constructed, never connected: the response publish is the only
		// publish on this path, so it is the one that fails.
		natsClient:  &natsclient.Client{},
		loopTracker: NewLoopTracker(),
	}

	c.sendUserResponseForLoop(t.Context(), &LoopInfo{
		LoopID:      "0e0b3f52-6f4a-4c6f-9c3f-1f2d3a4b5c6d",
		UserID:      "operator-1",
		ChannelType: "http",
		ChannelID:   "session-loop",
	}, agentic.ResponseTypeStatus, "loop completed")

	require.Equal(t, 1, testutil.CollectAndCount(c.metrics.responsePublishFailures),
		"exactly one lane series moved")
	require.InDelta(t, 1.0,
		testutil.ToFloat64(c.metrics.responsePublishFailures.WithLabelValues(responseLaneLoopUserChannel)), 0.0001)
	require.Contains(t, logged.String(), responseLaneLoopUserChannel,
		"the log must name the lane the response was lost on")
}

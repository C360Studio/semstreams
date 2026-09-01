package agenticdispatch

import (
	"context"
	"io"
	"log/slog"
	"strings"
	"testing"

	"github.com/c360studio/semstreams/agentic"
	"github.com/c360studio/semstreams/metric"
	"github.com/c360studio/semstreams/natsclient"
	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// newLoopTokenTestComponent wires the minimal Component both submission paths
// need. The NATS client is a zero-value client: it reports Disconnected, so
// PublishToStream returns ErrNotConnected instead of panicking. That is enough
// for these tests because every publish path in both submission functions runs
// AFTER loopTracker.Track — so an empty tracker is proof the function returned
// before assembling or publishing any task, and a tracked loop is the minted
// token itself.
func newLoopTokenTestComponent() (*Component, *captureSink) {
	sink := &captureSink{}
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	c := &Component{
		config:        DefaultConfig(),
		modelRegistry: newTestRegistry(),
		logger:        logger,
		loopTracker:   NewLoopTrackerWithLogger(logger),
		// Per-test registry, not the process-global one: these tests assert that a
		// refused submission leaves the started-loops gauge alone, which is only
		// readable when no other test shares the instrument.
		metrics:    getMetrics(metric.NewMetricsRegistry()),
		natsClient: &natsclient.Client{},
	}
	c.sendResponseFn = sink.add
	return c, sink
}

func newLoopTokenUserMessage() agentic.UserMessage {
	return agentic.UserMessage{
		MessageID:   "msg-1",
		ChannelType: "http",
		ChannelID:   "session-1",
		UserID:      "operator-1",
		Content:     "start a task",
	}
}

// requireCanonicalUUID asserts the exact wire form ADR-105 requires: 36 bytes,
// lowercase, hyphenated, and equal to its own canonical re-rendering — so an
// uppercase, braced, or urn spelling of the same identity fails here too.
func requireCanonicalUUID(t *testing.T, token, what string) {
	t.Helper()
	require.Len(t, token, 36, "%s must be a 36-byte canonical UUID, got %q", what, token)
	parsed, err := uuid.Parse(token)
	require.NoError(t, err, "%s = %q must parse as a UUID", what, token)
	require.Equal(t, parsed.String(), token, "%s = %q must be in canonical form", what, token)
	require.NotContains(t, token, "_", "%s must carry no mint prefix", what)
}

// TestNewConversationMintsCanonicalUUID pins the mint on BOTH dispatch intake
// paths. Before ADR-105 each minted "loop_" + uuid[:8] — 32 bits, where the
// birthday bound reaches ~1% at ~9.3K loops, and a collision silently merged two
// conversations because CreateLoopWithID overwrites.
func TestNewConversationMintsCanonicalUUID(t *testing.T) {
	t.Parallel()

	t.Run("HTTP submit path", func(t *testing.T) {
		t.Parallel()
		c, _ := newLoopTokenTestComponent()

		c.processTaskSubmissionSync(context.Background(), newLoopTokenUserMessage())

		loops := c.loopTracker.GetAllLoops()
		require.Len(t, loops, 1, "the HTTP path must mint exactly one loop")
		requireCanonicalUUID(t, loops[0].LoopID, "HTTP-minted loop_id")
	})

	t.Run("channel path", func(t *testing.T) {
		t.Parallel()
		c, _ := newLoopTokenTestComponent()

		c.handleTaskSubmission(context.Background(), newLoopTokenUserMessage())

		loops := c.loopTracker.GetAllLoops()
		require.Len(t, loops, 1, "the channel path must mint exactly one loop")
		requireCanonicalUUID(t, loops[0].LoopID, "channel-minted loop_id")
	})
}

// TestNonUUIDReplyToHTTPGetsSynchronousError: a client that authors a
// continuation token learns so in the response it is already waiting on, naming
// the field it got wrong — not "Task submitted" followed by an async TERM it
// never sees.
func TestNonUUIDReplyToHTTPGetsSynchronousError(t *testing.T) {
	t.Parallel()
	c, _ := newLoopTokenTestComponent()

	msg := newLoopTokenUserMessage()
	msg.ReplyTo = "loop_ab12cd34"

	resp := c.processTaskSubmissionSync(context.Background(), msg)

	assert.Equal(t, agentic.ResponseTypeError, resp.Type,
		"an authored continuation token must be answered with an error, not an acknowledgement")
	assert.Contains(t, strings.ToLower(resp.Content), "reply_to",
		"the error must name the field the client got wrong")
	assert.Empty(t, c.loopTracker.GetAllLoops(),
		"a refused submission must not track a loop — nothing was assembled or published")
}

// TestNonUUIDReplyToChannelGetsErrorResponse: the channel path has no
// synchronous return, so its answer goes out on the response subject via
// sendResponse. Same refusal, same named field, different delivery.
func TestNonUUIDReplyToChannelGetsErrorResponse(t *testing.T) {
	t.Parallel()
	c, sink := newLoopTokenTestComponent()

	msg := newLoopTokenUserMessage()
	msg.ReplyTo = "loop_ab12cd34"

	c.handleTaskSubmission(context.Background(), msg)

	responses := sink.all()
	require.Len(t, responses, 1, "exactly one response must be published for a refused submission")
	assert.Equal(t, agentic.ResponseTypeError, responses[0].Type)
	assert.Contains(t, strings.ToLower(responses[0].Content), "reply_to",
		"the error must name the field the client got wrong")
	assert.Equal(t, msg.ChannelType, responses[0].ChannelType)
	assert.Equal(t, msg.ChannelID, responses[0].ChannelID)
	assert.Empty(t, c.loopTracker.GetAllLoops(),
		"a refused submission must not track a loop — nothing was assembled or published")
}

// TestAutoContinuedNonUUIDTokenIsRefused covers the second way a bad token
// reaches the mint: auto-continue resolves it from the tracker rather than the
// client supplying it. The check sits on the RESOLVED token, after the
// auto-continue branch, so one check covers both sources.
func TestAutoContinuedNonUUIDTokenIsRefused(t *testing.T) {
	t.Parallel()
	c, _ := newLoopTokenTestComponent()
	require.True(t, c.config.AutoContinue, "this test exercises the auto-continue branch")

	msg := newLoopTokenUserMessage()
	c.loopTracker.Track(&LoopInfo{
		LoopID:      "loop_ab12cd34",
		TaskID:      "task-legacy",
		UserID:      msg.UserID,
		ChannelType: msg.ChannelType,
		ChannelID:   msg.ChannelID,
		State:       "pending",
	})

	resp := c.processTaskSubmissionSync(context.Background(), msg)

	assert.Equal(t, agentic.ResponseTypeError, resp.Type,
		"an auto-continued non-canonical token must be refused, not minted onto")
	// Naming the field is what separates this refusal from any other error the
	// submission path can return — without it the assertion above passes on a
	// publish failure too.
	assert.Contains(t, strings.ToLower(resp.Content), "reply_to",
		"the refusal must name the continuation field, not read as a generic failure")
	assert.Len(t, c.loopTracker.GetAllLoops(), 1,
		"the refusal must not have created a second loop")
}

// TestCanonicalReplyToContinuesTheLoop is the positive control on the refusal:
// it rejects the shape, not continuation itself. A framework-minted token
// echoed back must still route to its loop.
func TestCanonicalReplyToContinuesTheLoop(t *testing.T) {
	t.Parallel()
	c, _ := newLoopTokenTestComponent()

	existing := uuid.NewString()
	msg := newLoopTokenUserMessage()
	msg.ReplyTo = existing

	resp := c.processTaskSubmissionSync(context.Background(), msg)

	assert.NotContains(t, strings.ToLower(resp.Content), "reply_to",
		"an echoed framework-minted token must not be refused")
	loops := c.loopTracker.GetAllLoops()
	require.Len(t, loops, 1)
	assert.Equal(t, existing, loops[0].LoopID,
		"a canonical reply_to must continue that loop, not mint a new one")
}

// TestNonUUIDRunIDHTTPGetsSynchronousError: run_id is a loop token the CLIENT
// authors (HTTPMessageRequest.RunID, gh#256 resume anchor), so it reaches the
// published task without ever passing through the continuation branch. Refusing
// it at the same seam is what keeps the submission from tracking a loop and
// counting a start for a task that TaskMessage.Validate will later reject inside
// marshal — where the HTTP path can only answer "please try again".
func TestNonUUIDRunIDHTTPGetsSynchronousError(t *testing.T) {
	t.Parallel()
	c, _ := newLoopTokenTestComponent()
	startedBefore := getGaugeValue(t, c.metrics.activeLoops)

	msg := newLoopTokenUserMessage()
	msg.RunID = "run-42"

	resp := c.processTaskSubmissionSync(context.Background(), msg)

	assert.Equal(t, agentic.ResponseTypeError, resp.Type,
		"an authored run_id must be answered with an error, not an acknowledgement")
	assert.Contains(t, strings.ToLower(resp.Content), "run_id",
		"the error must name the field the client got wrong, not read as a generic failure")
	assert.Empty(t, c.loopTracker.GetAllLoops(),
		"a refused submission must not track a loop — the loop it names never exists")
	assert.Equal(t, startedBefore, getGaugeValue(t, c.metrics.activeLoops),
		"a refused submission must not count a started loop")
}

// TestNonUUIDInReplyToChannelGetsErrorResponse: in_reply_to is the second
// client-authored loop token, and the channel path is the one with no
// synchronous return — before this refusal it logged the marshal error and
// returned, leaving the submitter with no answer at all.
func TestNonUUIDInReplyToChannelGetsErrorResponse(t *testing.T) {
	t.Parallel()
	c, sink := newLoopTokenTestComponent()
	startedBefore := getGaugeValue(t, c.metrics.activeLoops)

	msg := newLoopTokenUserMessage()
	msg.InReplyTo = "workflow-7"

	c.handleTaskSubmission(context.Background(), msg)

	responses := sink.all()
	require.Len(t, responses, 1, "exactly one response must be published for a refused submission")
	assert.Equal(t, agentic.ResponseTypeError, responses[0].Type)
	assert.Contains(t, strings.ToLower(responses[0].Content), "in_reply_to",
		"the error must name the field the client got wrong, not read as a generic failure")
	assert.Empty(t, c.loopTracker.GetAllLoops(),
		"a refused submission must not track a loop — the loop it names never exists")
	assert.Equal(t, startedBefore, getGaugeValue(t, c.metrics.activeLoops),
		"a refused submission must not count a started loop")
}

// TestCanonicalResumeAnchorsAreAccepted is the positive control on the two
// refusals above: the seam rejects the shape, not the gh#256 resume feature. A
// reply carrying framework-minted anchors still mints its loop and gets tracked.
func TestCanonicalResumeAnchorsAreAccepted(t *testing.T) {
	t.Parallel()
	c, _ := newLoopTokenTestComponent()

	msg := newLoopTokenUserMessage()
	msg.RunID = uuid.NewString()
	msg.InReplyTo = uuid.NewString()

	resp := c.processTaskSubmissionSync(context.Background(), msg)

	assert.NotContains(t, strings.ToLower(resp.Content), "run_id",
		"an echoed framework-minted run_id must not be refused")
	assert.NotContains(t, strings.ToLower(resp.Content), "in_reply_to",
		"an echoed framework-minted in_reply_to must not be refused")
	loops := c.loopTracker.GetAllLoops()
	require.Len(t, loops, 1, "an accepted submission must still mint and track its loop")
	requireCanonicalUUID(t, loops[0].LoopID, "minted loop_id")
}

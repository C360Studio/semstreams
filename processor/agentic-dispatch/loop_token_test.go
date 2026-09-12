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
	"github.com/c360studio/semstreams/pkg/errs"
	"github.com/google/uuid"
	"github.com/prometheus/client_golang/prometheus/testutil"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// newLoopTokenTestComponent wires the minimal Component both submission paths
// need. The NATS client is a zero-value client: it reports Disconnected, so
// PublishToStream returns ErrNotConnected instead of panicking. That is enough
// for the refusal unit tests. Native publication controls in
// restart_identity_integration_test.go observe minted tokens on the actual wire.
func newLoopTokenTestComponent() (*Component, *captureSink) {
	sink := &captureSink{}
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	c := &Component{
		config:        DefaultConfig(),
		modelRegistry: newTestRegistry(),
		logger:        logger,
		// Per-test counters keep refusal observations independent.
		metrics:      getMetrics(metric.NewMetricsRegistry()),
		natsClient:   &natsclient.Client{},
		taskEvidence: emptyRetainedTaskEvidenceReader{},
	}
	c.sendResponseFn = sink.add
	// An empty durable store: the admission gate reads AGENT_LOOPS, and without
	// this the zero-value client makes every read UNREADABLE rather than
	// reporting the absence these tests mean.
	withPersistedLoops(c, nil)
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
//
// It also asserts version 4. looptoken.Valid deliberately ignores the version
// bits (a seam validating what it received has no business asserting how a peer
// minted it), so a MINT test is the only correct home for that distinction —
// without it a canonical non-v4 UUID would satisfy the spec's "framework-minted
// v4 UUID" clause. Every caller below passes a framework-MINTED token, never an
// echoed one.
func requireCanonicalUUID(t *testing.T, token, what string) {
	t.Helper()
	require.Len(t, token, 36, "%s must be a 36-byte canonical UUID, got %q", what, token)
	parsed, err := uuid.Parse(token)
	require.NoError(t, err, "%s = %q must parse as a UUID", what, token)
	require.Equal(t, parsed.String(), token, "%s = %q must be in canonical form", what, token)
	require.Equal(t, uuid.Version(4), parsed.Version(),
		"%s = %q must be a version 4 UUID — the spec requires framework mints be v4, and looptoken.Valid "+
			"does not check version bits", what, token)
	require.NotContains(t, token, "_", "%s must carry no mint prefix", what)
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

	resp, err := c.processTaskSubmissionSync(context.Background(), msg)
	require.NoError(t, err)

	assert.Equal(t, agentic.ResponseTypeError, resp.Type,
		"an authored continuation token must be answered with an error, not an acknowledgement")
	assert.Contains(t, strings.ToLower(resp.Content), "reply_to",
		"the error must name the field the client got wrong")
	require.Zero(t, testutil.ToFloat64(c.metrics.tasksSubmitted), "a refused submission cannot count a task publication")
}

// TestNonUUIDReplyToChannelGetsErrorResponse: the channel path has no
// synchronous return, so its answer goes out on the response subject via
// sendResponse. Same refusal, same named field, different delivery.
func TestNonUUIDReplyToChannelGetsErrorResponse(t *testing.T) {
	t.Parallel()
	c, sink := newLoopTokenTestComponent()

	msg := newLoopTokenUserMessage()
	msg.ReplyTo = "loop_ab12cd34"

	require.NoError(t, c.handleTaskSubmission(context.Background(), msg))

	responses := sink.all()
	require.Len(t, responses, 1, "exactly one response must be published for a refused submission")
	assert.Equal(t, agentic.ResponseTypeError, responses[0].Type)
	assert.Contains(t, strings.ToLower(responses[0].Content), "reply_to",
		"the error must name the field the client got wrong")
	assert.Equal(t, msg.ChannelType, responses[0].ChannelType)
	assert.Equal(t, msg.ChannelID, responses[0].ChannelID)
	require.Zero(t, testutil.ToFloat64(c.metrics.tasksSubmitted), "a refused submission cannot count a task publication")
}

// spec: agentic-dispatch / The shared view separates current authority from activity
func TestAutoContinueRefusesMalformedCurrentLoop(t *testing.T) {
	source := newFakeActivitySource()
	hooks := newActivityHookChans()
	c := newActivityTestComponent(t, source, hooks.hooks())
	c.metrics = getMetrics(metric.NewMetricsRegistry())
	c.taskEvidence = emptyRetainedTaskEvidenceReader{}
	c.config.AutoContinue = true // Explicit opt-in; ordinary chat turns are independent by default.
	require.True(t, c.config.AutoContinue, "this test exercises the auto-continue branch")
	_, err := c.ensureActivityView(t.Context())
	require.NoError(t, err)
	watcher := source.waitWatcher(t, 1)
	watcher.updates <- fakeActivityEntry{key: admissionLoopA, rev: 1,
		value: loopEntityJSON(t, admissionMalformed, agentic.LoopStateExecuting, 0)}
	watcher.updates <- nil
	hooks.waitCaughtUp(t)
	hooks.waitPoisonKey(t, admissionLoopA)

	resp, err := c.processTaskSubmissionSync(t.Context(), newLoopTokenUserMessage())
	require.Error(t, err)
	require.True(t, errs.IsTransient(err), "poisoned current authority cannot masquerade as an empty route")
	require.Contains(t, err.Error(), "poison")
	require.Empty(t, resp.InReplyTo)
	require.Zero(t, testutil.ToFloat64(c.metrics.tasksSubmitted))
}

// TestNonUUIDRunIDHTTPGetsSynchronousError: run_id is a loop token the CLIENT
// authors (HTTPMessageRequest.RunID, gh#256 resume anchor), so it reaches the
// published task without ever passing through the continuation branch. Refusing
// it at the submission seam gives the caller the offending field before task publication.
func TestNonUUIDRunIDHTTPGetsSynchronousError(t *testing.T) {
	t.Parallel()
	c, _ := newLoopTokenTestComponent()

	msg := newLoopTokenUserMessage()
	msg.RunID = "run-42"

	resp, err := c.processTaskSubmissionSync(context.Background(), msg)
	require.NoError(t, err)

	assert.Equal(t, agentic.ResponseTypeError, resp.Type,
		"an authored run_id must be answered with an error, not an acknowledgement")
	assert.Contains(t, strings.ToLower(resp.Content), "run_id",
		"the error must name the field the client got wrong, not read as a generic failure")
	require.Zero(t, testutil.ToFloat64(c.metrics.tasksSubmitted))
}

// TestNonUUIDInReplyToChannelGetsErrorResponse: in_reply_to is the second
// client-authored loop token, and the channel path is the one with no
// synchronous return — before this refusal it logged the marshal error and
// returned, leaving the submitter with no answer at all.
func TestNonUUIDInReplyToChannelGetsErrorResponse(t *testing.T) {
	t.Parallel()
	c, sink := newLoopTokenTestComponent()

	msg := newLoopTokenUserMessage()
	msg.InReplyTo = "workflow-7"

	require.NoError(t, c.handleTaskSubmission(context.Background(), msg))

	responses := sink.all()
	require.Len(t, responses, 1, "exactly one response must be published for a refused submission")
	assert.Equal(t, agentic.ResponseTypeError, responses[0].Type)
	assert.Contains(t, strings.ToLower(responses[0].Content), "in_reply_to",
		"the error must name the field the client got wrong, not read as a generic failure")
	require.Zero(t, testutil.ToFloat64(c.metrics.tasksSubmitted))
}

// spec: agentic-dispatch / Dispatch task redelivery recovers the committed LoopID
func TestPrepareNewDispatchTaskMintsCanonicalUUID(t *testing.T) {
	c, _ := newLoopTokenTestComponent()
	msg := newLoopTokenUserMessage()
	_, vacant, found, err := c.findRetainedDispatchTask(t.Context(), msg)
	require.NoError(t, err)
	require.False(t, found)
	prepared, err := c.prepareNewDispatchTask(t.Context(), msg, "", vacant)
	require.NoError(t, err)
	requireCanonicalUUID(t, prepared.task.LoopID, "prepared loop_id")
}

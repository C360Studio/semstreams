package agenticdispatch

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/c360studio/semstreams/agentic"
	"github.com/c360studio/semstreams/metric"
	"github.com/c360studio/semstreams/natsclient"
	"github.com/prometheus/client_golang/prometheus/testutil"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Canonical loop tokens shared by every seam test in this package, including
// the HTTP handler tests that predate the gate. They are fixed rather than
// minted so a failure names the same token every run, and canonical because a
// non-canonical one now refuses at the form check before any seam behaviour
// runs.
const (
	seamTestLoopA      = "11111111-1111-4111-8111-111111111111"
	seamTestLoopB      = "22222222-2222-4222-8222-222222222222"
	seamTestLoopAbsent = "99999999-9999-4999-8999-999999999999"
	// seamTestMalformed is the shape #1228 reports: a truncated, prefixed token
	// no framework mint ever produced.
	seamTestMalformed = "loop_ab12cd34"
)

// seamRecorder captures the WARN records a refusal produces so a test can pin
// the single named log constant rather than a copy of its text.
type seamRecorder struct {
	mu      sync.Mutex
	records []slog.Record
}

func (h *seamRecorder) Enabled(context.Context, slog.Level) bool { return true }
func (h *seamRecorder) Handle(_ context.Context, r slog.Record) error {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.records = append(h.records, r.Clone())
	return nil
}
func (h *seamRecorder) WithAttrs([]slog.Attr) slog.Handler { return h }
func (h *seamRecorder) WithGroup(string) slog.Handler      { return h }

func (h *seamRecorder) countMessage(msg string) int {
	h.mu.Lock()
	defer h.mu.Unlock()
	n := 0
	for _, r := range h.records {
		if r.Message == msg {
			n++
		}
	}
	return n
}

// newSeamTestComponent wires the minimal Component every seam in section 4
// needs: both submission paths, both commands, and the three loop endpoints.
//
// The NATS client is a zero-value client — it reports Disconnected, so a
// publish fails cleanly instead of panicking. The durable store starts empty,
// so a loop absent from the tracker is ABSENT rather than unreadable; tests
// that want a durable-only loop install their own records.
//
// The metrics registry is per-component, not the process-global one, so
// CollectAndCount proves that exactly one series moved.
func newSeamTestComponent(t *testing.T) (*Component, *captureSink, *seamRecorder) {
	t.Helper()
	sink := &captureSink{}
	recorder := &seamRecorder{}
	c := &Component{
		config:        DefaultConfig(),
		modelRegistry: newTestRegistry(),
		logger:        slog.New(recorder),
		loopTracker:   NewLoopTrackerWithLogger(slog.New(slog.NewTextHandler(io.Discard, nil))),
		registry:      NewCommandRegistry(),
		metrics:       getMetrics(metric.NewMetricsRegistry()),
		natsClient:    &natsclient.Client{},
	}
	c.sendResponseFn = sink.add
	withPersistedLoops(c, nil)
	c.registerBuiltinCommands()
	return c, sink, recorder
}

func seamUserMessage(userID string) agentic.UserMessage {
	return agentic.UserMessage{
		MessageID:   "msg-1",
		ChannelType: "http",
		ChannelID:   "session-1",
		UserID:      userID,
		Content:     "keep going",
	}
}

// trackLoopOwnedBy seeds a live loop in the tracker AND in the durable store,
// which is what a real running loop looks like once persistLoopState has landed.
func trackLoopOwnedBy(c *Component, loopID, userID string) {
	c.loopTracker.Track(&LoopInfo{
		LoopID:      loopID,
		TaskID:      "task-" + loopID,
		UserID:      userID,
		ChannelType: "http",
		ChannelID:   "session-1",
		State:       "executing",
		CreatedAt:   time.Now(),
	})
	withPersistedLoops(c, map[string]*agentic.LoopEntity{loopID: {
		ID:            loopID,
		UserID:        userID,
		ChannelType:   "http",
		ChannelID:     "session-1",
		State:         agentic.LoopStateExecuting,
		MaxIterations: 5,
	}})
}

// requireSeamRefusal asserts that exactly one refusal was counted, on the named
// seam and reason, and that it produced exactly one refusal log line carrying
// the single named constant (I3).
func requireSeamRefusal(t *testing.T, c *Component, rec *seamRecorder, seam, reason string) {
	t.Helper()
	require.Equal(t, 1, testutil.CollectAndCount(c.metrics.loopAdmissionRefusals),
		"exactly one refusal series moved")
	require.Equal(t, 1.0,
		testutil.ToFloat64(c.metrics.loopAdmissionRefusals.WithLabelValues(seam, reason)),
		"exactly one increment on %s/%s", seam, reason)
	require.Equal(t, 1, rec.countMessage(loopAdmissionRefusalLogMessage),
		"exactly one refusal log line, carrying the single named constant")
}

// seamRefusalDriver drives one seam with a caller-supplied loop token and
// reports the answer text the seam produced. Every seam that accepts a loop
// token has one, so the every-seam test enumerates the CLASS rather than a
// motivating instance.
type seamRefusalDriver struct {
	seam string
	// drive runs the seam with the given token and requester and returns the
	// answer the caller sees (a response body or an error content string).
	drive func(t *testing.T, c *Component, sink *captureSink, loopID, requester string) string
}

func seamRefusalDrivers() []seamRefusalDriver {
	return []seamRefusalDriver{
		{
			seam: seamChannelSubmission,
			drive: func(t *testing.T, c *Component, sink *captureSink, loopID, requester string) string {
				t.Helper()
				msg := seamUserMessage(requester)
				msg.ReplyTo = loopID
				c.handleTaskSubmission(context.Background(), msg)
				responses := sink.all()
				require.Len(t, responses, 1, "the channel lane answers on the response subject")
				require.Equal(t, agentic.ResponseTypeError, responses[0].Type)
				return responses[0].Content
			},
		},
		{
			seam: seamHTTPSubmission,
			drive: func(t *testing.T, c *Component, _ *captureSink, loopID, requester string) string {
				t.Helper()
				msg := seamUserMessage(requester)
				msg.ReplyTo = loopID
				resp := c.processTaskSubmissionSync(context.Background(), msg)
				require.Equal(t, agentic.ResponseTypeError, resp.Type)
				return resp.Content
			},
		},
		{
			seam: seamCancelCommand,
			drive: func(t *testing.T, c *Component, _ *captureSink, loopID, requester string) string {
				t.Helper()
				resp, err := c.handleCancelCommand(context.Background(),
					seamUserMessage(requester), []string{loopID}, "")
				require.NoError(t, err)
				require.Equal(t, agentic.ResponseTypeError, resp.Type)
				return resp.Content
			},
		},
		{
			seam: seamStatusCommand,
			drive: func(t *testing.T, c *Component, _ *captureSink, loopID, requester string) string {
				t.Helper()
				resp, err := c.handleStatusCommand(context.Background(),
					seamUserMessage(requester), []string{loopID}, "")
				require.NoError(t, err)
				require.Equal(t, agentic.ResponseTypeError, resp.Type)
				return resp.Content
			},
		},
		{
			seam: seamHTTPLoopRead,
			drive: func(t *testing.T, c *Component, _ *captureSink, loopID, requester string) string {
				t.Helper()
				rec := seamHTTPCall(t, c.handleGetLoop, http.MethodGet, "/loops/"+loopID, loopID, "", requester)
				require.NotEqual(t, http.StatusOK, rec.Code)
				return seamErrorContent(t, rec)
			},
		},
		{
			seam: seamHTTPLoopApproval,
			drive: func(t *testing.T, c *Component, _ *captureSink, loopID, requester string) string {
				t.Helper()
				rec := seamHTTPCall(t, c.handleLoopApproval, http.MethodPost,
					"/loops/"+loopID+"/approval", loopID, `{"decision":"approve"}`, requester)
				require.NotEqual(t, http.StatusOK, rec.Code)
				return seamErrorContent(t, rec)
			},
		},
	}
}

func seamHTTPCall(t *testing.T, handler http.HandlerFunc, method, target, loopID, body, requester string) *httptest.ResponseRecorder {
	t.Helper()
	var reader io.Reader
	if body != "" {
		reader = strings.NewReader(body)
	}
	req := httptest.NewRequest(method, target, reader)
	req.SetPathValue("id", loopID)
	if requester != "" {
		req = req.WithContext(WithIdentity(req.Context(), requester))
	}
	rec := httptest.NewRecorder()
	handler(rec, req)
	return rec
}

func seamErrorContent(t *testing.T, rec *httptest.ResponseRecorder) string {
	t.Helper()
	var resp HTTPMessageResponse
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &resp))
	return resp.Content
}

// spec: agentic-dispatch / One gate admits every request that names an existing loop
// Every seam that accepts a loop token refuses a non-canonical one through the
// shared gate: same message, same single named log constant, one counted
// refusal labelled with that seam. Enumerating the seams rather than testing
// the motivating one is the point — a second entry path that skipped the gate
// would show up here as a missing driver, not as a passing test.
func TestEverySeamRefusesThroughTheGate(t *testing.T) {
	for _, driver := range seamRefusalDrivers() {
		t.Run(driver.seam, func(t *testing.T) {
			c, sink, rec := newSeamTestComponent(t)

			content := driver.drive(t, c, sink, seamTestMalformed, "user-a")

			assert.Contains(t, content, "is not a loop ID this framework minted",
				"every seam answers with the gate's own refusal, not a local paraphrase")
			requireSeamRefusal(t, c, rec, driver.seam, reasonFormMalformed)
		})
	}
}

// spec: agentic-dispatch / One gate admits every request that names an existing loop
// I3 at the seam level: a refusal moves the counter exactly once, labelled with
// the seam it arrived on — never twice because a seam counted its own answer on
// top of the gate's.
func TestRefusalIsCountedExactlyOncePerSeam(t *testing.T) {
	for _, driver := range seamRefusalDrivers() {
		t.Run(driver.seam, func(t *testing.T) {
			c, sink, rec := newSeamTestComponent(t)

			driver.drive(t, c, sink, seamTestLoopAbsent, "user-a")

			requireSeamRefusal(t, c, rec, driver.seam, reasonExistenceAbsent)
		})
	}
}

// spec: agentic-dispatch / The ownership model binds the user lane, and approval is deliberately not owner-scoped
// The defect #1227 reports: a second holder of the token takes over the loop.
func TestSecondHolderCannotContinueAnotherUsersLoop(t *testing.T) {
	c, sink, rec := newSeamTestComponent(t)
	trackLoopOwnedBy(c, seamTestLoopA, "user-a")

	msg := seamUserMessage("user-b")
	msg.ReplyTo = seamTestLoopA
	c.handleTaskSubmission(context.Background(), msg)

	responses := sink.all()
	require.Len(t, responses, 1)
	assert.Equal(t, agentic.ResponseTypeError, responses[0].Type)
	assert.Contains(t, responses[0].Content, "does not own")
	requireSeamRefusal(t, c, rec, seamChannelSubmission, reasonOwnershipNotOwner)
}

// spec: agentic-dispatch / The ownership model binds the user lane, and approval is deliberately not owner-scoped
// I4: a refused continuation leaves the loop's recorded owner and its
// active-loop indexes pointing where they pointed before — which is what keeps
// the original user's completion routed to the original user.
func TestRefusedContinuationDoesNotRepointOwnership(t *testing.T) {
	c, _, _ := newSeamTestComponent(t)
	trackLoopOwnedBy(c, seamTestLoopA, "user-a")

	msg := seamUserMessage("user-b")
	msg.ReplyTo = seamTestLoopA
	c.processTaskSubmissionSync(context.Background(), msg)

	info := c.loopTracker.Get(seamTestLoopA)
	require.NotNil(t, info, "the refused request must not remove the loop")
	assert.Equal(t, "user-a", info.UserID, "the recorded owner is unchanged")
	assert.Equal(t, seamTestLoopA, c.loopTracker.GetActiveLoop("user-a", "session-1"),
		"user-a's active-loop index still points at the loop")
	assert.Empty(t, c.loopTracker.GetUserLoops("user-b"),
		"the refused requester owns nothing")
}

// spec: agentic-dispatch / The ownership model binds the user lane, and approval is deliberately not owner-scoped
// cancel admits a non-owner on the cancel-any list. The default list is empty,
// so this is the configured-operator case, and it is the ONLY way a non-owner
// cancels someone else's loop. The /cancel command is the one seam that asks
// for this verb: the signal endpoint that used to share it is deleted.
func TestCancelAnyAdmitsNonOwnerCancel(t *testing.T) {
	c, _, rec := newSeamTestComponent(t)
	c.config.Permissions.CancelAny = []string{"operator"}
	trackLoopOwnedBy(c, seamTestLoopA, "user-a")

	t.Run("the operator is admitted", func(t *testing.T) {
		_, err := c.handleCancelCommand(context.Background(),
			seamUserMessage("operator"), []string{seamTestLoopA}, "")
		// Admission passed and the handler went on to publish, which fails on
		// the disconnected test client. A refusal returns a typed response with
		// a nil error, so a publish error is proof the gate admitted.
		require.ErrorContains(t, err, "publish signal",
			"a cancel_any holder reaches the publish")
		assert.Equal(t, 0, testutil.CollectAndCount(c.metrics.loopAdmissionRefusals),
			"nothing was refused")
	})

	t.Run("a non-owner without cancel_any is refused", func(t *testing.T) {
		resp, err := c.handleCancelCommand(context.Background(),
			seamUserMessage("stranger"), []string{seamTestLoopA}, "")
		require.NoError(t, err, "a refusal is answered, not returned as an error")
		assert.Equal(t, agentic.ResponseTypeError, resp.Type)
		assert.Contains(t, resp.Content, "does not own")
		requireSeamRefusal(t, c, rec, seamCancelCommand, reasonOwnershipNotOwner)
	})
}

// spec: agentic-dispatch / The ownership model binds the user lane, and approval is deliberately not owner-scoped
// Approval is deliberately NOT owner-scoped: a second-party reviewer is the
// entire point of an approval. A change that "fixes" this by adding an owner
// check removes the capability, and this test is what fails when it does.
func TestApprovalIsNotOwnerScoped(t *testing.T) {
	c, _, _ := newSeamTestComponent(t)
	c.config.Permissions.Approve = []string{"reviewer-b"}
	trackLoopOwnedBy(c, seamTestLoopA, "user-a")
	c.loopTracker.SetPendingApproval(seamTestLoopA, &PendingApprovalInfo{
		CallID:      "call-001",
		ToolName:    "delete_rule",
		RequestedAt: time.Now().UTC(),
	})

	rec := seamHTTPCall(t, c.handleLoopApproval, http.MethodPost,
		"/loops/"+seamTestLoopA+"/approval", seamTestLoopA, `{"decision":"approve"}`, "reviewer-b")

	assert.NotEqual(t, http.StatusForbidden, rec.Code,
		"an approver who does not own the loop is admitted")
	assert.NotEqual(t, http.StatusConflict, rec.Code,
		"the pending approval was found, so the request reached the publish")
}

// spec: agentic-dispatch / The ownership model binds the user lane, and approval is deliberately not owner-scoped
// The approve permission has been advertised in configuration and read by no
// call site. This is the test that makes it load-bearing.
func TestApprovalRefusedForCallerOutsideApproveList(t *testing.T) {
	c, _, rec := newSeamTestComponent(t)
	c.config.Permissions.Approve = []string{"reviewer-b"}
	trackLoopOwnedBy(c, seamTestLoopA, "user-a")
	c.loopTracker.SetPendingApproval(seamTestLoopA, &PendingApprovalInfo{
		CallID: "call-001", ToolName: "delete_rule", RequestedAt: time.Now().UTC(),
	})

	resp := seamHTTPCall(t, c.handleLoopApproval, http.MethodPost,
		"/loops/"+seamTestLoopA+"/approval", seamTestLoopA, `{"decision":"approve"}`, "stranger-c")

	assert.Equal(t, http.StatusForbidden, resp.Code)
	requireSeamRefusal(t, c, rec, seamHTTPLoopApproval, reasonOwnershipNotPermitted)
}

// The default approve list admits everyone, so enforcing the permission changes
// no default deployment's behaviour. Without this control the test above would
// pass on a gate that refused every approval.
func TestApprovalDefaultAdmitsEveryone(t *testing.T) {
	c, _, _ := newSeamTestComponent(t)
	require.Equal(t, []string{"*"}, c.config.Permissions.Approve,
		"the shipped default admits everyone")
	trackLoopOwnedBy(c, seamTestLoopA, "user-a")
	c.loopTracker.SetPendingApproval(seamTestLoopA, &PendingApprovalInfo{
		CallID: "call-001", ToolName: "delete_rule", RequestedAt: time.Now().UTC(),
	})

	rec := seamHTTPCall(t, c.handleLoopApproval, http.MethodPost,
		"/loops/"+seamTestLoopA+"/approval", seamTestLoopA, `{"decision":"approve"}`, "anybody")

	assert.NotEqual(t, http.StatusForbidden, rec.Code)
}

// spec: agentic-dispatch / The ownership model binds the user lane, and approval is deliberately not owner-scoped
// A settled loop cannot be continued, and no new loop is minted under its
// token — the silent fork #1227 reports. Terminality is fail-closed across the
// two sources, so the durable record alone refuses it.
func TestAttachToTerminalLoopIsRefused(t *testing.T) {
	c, sink, rec := newSeamTestComponent(t)
	withPersistedLoops(c, map[string]*agentic.LoopEntity{seamTestLoopA: {
		ID: seamTestLoopA, UserID: "user-a", State: agentic.LoopStateComplete, MaxIterations: 5,
	}})

	msg := seamUserMessage("user-a")
	msg.ReplyTo = seamTestLoopA
	c.handleTaskSubmission(context.Background(), msg)

	responses := sink.all()
	require.Len(t, responses, 1)
	assert.Equal(t, agentic.ResponseTypeError, responses[0].Type)
	assert.Contains(t, responses[0].Content, "already settled")
	assert.Empty(t, c.loopTracker.GetAllLoops(),
		"no loop is minted under a settled loop's token")
	requireSeamRefusal(t, c, rec, seamChannelSubmission, reasonStateTerminal)
}

// spec: agentic-dispatch / The ownership model binds the user lane, and approval is deliberately not owner-scoped
// A system-lane loop carries no user owner and is never owner-checked, because
// it never traverses this gate: the rule engine's agent-publish action builds a
// task with no loop id and publishes it straight to agent.task.*. The property
// under test is that dispatch's submission seam is not on that path at all.
func TestSystemLaneLoopIsNotOwnerChecked(t *testing.T) {
	t.Run("it runs and settles with no admission refusal anywhere", func(t *testing.T) {
		// The whole dispatch-side path of a system-lane loop: it was published
		// straight to agent.task.* with no loop id and no owner, so the only
		// thing dispatch ever sees is its terminal event. Settling that event
		// must consult no ownership and refuse nothing.
		c := terminalTestComponent(t)
		c.loopTracker.Track(&LoopInfo{
			LoopID: seamTestLoopA, TaskID: "task-sys", ChannelType: "http",
			ChannelID: "session-sys", State: "executing", MaxIterations: 3,
		})
		c.loadPersistedLoopFn = func(context.Context, string) (*agentic.LoopEntity, error) {
			return &agentic.LoopEntity{
				ID: seamTestLoopA, TaskID: "task-sys", State: agentic.LoopStateComplete,
				MaxIterations: 3, ChannelType: "http", ChannelID: "session-sys",
			}, nil
		}
		c.sendTerminalResponseFn = func(context.Context, agentic.UserResponse, string) error { return nil }

		data := completionPayload(t, &agentic.LoopCompletedEvent{
			LoopID: seamTestLoopA, TaskID: "task-sys", Outcome: agentic.OutcomeSuccess,
			Result: "done", CompletedAt: time.Unix(1_700_000_900, 0).UTC(),
		})
		require.NoError(t, c.settleAgentTerminal(context.Background(), data))

		require.Equal(t, 0, testutil.CollectAndCount(c.metrics.loopAdmissionRefusals),
			"a system-lane loop never traverses the user-lane gate")
	})

	t.Run("a USER-lane request naming it is refused, fail-closed (ruling R1)", func(t *testing.T) {
		c, _, _ := newSeamTestComponent(t)
		trackLoopOwnedBy(c, seamTestLoopA, "")

		_, err := c.admitLoopRequest(context.Background(), loopAdmissionRequest{
			Seam:      seamChannelSubmission,
			Field:     "reply_to",
			Operation: loopOpContinue,
			LoopID:    seamTestLoopA,
			Requester: "user-a",
		})
		require.Error(t, err, "an unknown owner fails closed for every owner-consulting operation")
	})
}

// spec: agentic-dispatch / The gate is not authorization, and the spec says so
// Identity on this plane is asserted by the caller and verified by nothing. A
// client that claims the owner's identity passes every check in the capability.
// This test exists so the limit is recorded in the suite and not only in prose.
func TestAssertedIdentityIsNotVerified(t *testing.T) {
	c, _, _ := newSeamTestComponent(t)
	trackLoopOwnedBy(c, seamTestLoopA, "user-a")

	// No authenticating middleware; the claim rides in the body/message.
	impostor := seamUserMessage("user-a")
	impostor.ReplyTo = seamTestLoopA
	resp := c.processTaskSubmissionSync(context.Background(), impostor)

	assert.NotContains(t, resp.Content, "does not own",
		"the gate matches the claimed identity; it does not verify it")
	assert.Equal(t, 0.0,
		testutil.ToFloat64(c.metrics.loopAdmissionRefusals.WithLabelValues(
			seamHTTPSubmission, reasonOwnershipNotOwner)),
		"an unverified claim of the owner's identity passes the ownership check")
	assert.Equal(t, seamTestLoopA, c.loopTracker.Get(seamTestLoopA).LoopID,
		"the impostor's submission was assembled against the real loop")
}

// spec: agentic-dispatch / A refused or unpublishable submission leaves no tracked loop and no moved gauge
// #1225 on the channel lane: a task that fails payload validation inside the
// marshal used to be a logged bare return, leaving the submitter with no answer
// at all.
func TestValidationFailureAnswersChannelSubmitter(t *testing.T) {
	c, sink, rec := newSeamTestComponent(t)

	msg := seamUserMessage("user-a")
	// A client-authored resume anchor. It never passes through the continuation
	// branch, so TaskMessage.Validate inside the marshal is what refuses it.
	msg.RunID = "run-42"
	c.handleTaskSubmission(context.Background(), msg)

	responses := sink.all()
	require.Len(t, responses, 1, "exactly one response for a refused submission")
	assert.Equal(t, agentic.ResponseTypeError, responses[0].Type)
	assert.Contains(t, strings.ToLower(responses[0].Content), "run_id",
		"the answer names the offending field, not a generic retry suggestion")
	requireSeamRefusal(t, c, rec, seamChannelSubmission, reasonSubmissionInvalid)
}

// spec: agentic-dispatch / A refused or unpublishable submission leaves no tracked loop and no moved gauge
// The same failure on the HTTP lane, where the client is already waiting on a
// synchronous body that used to say "Failed to create task. Please try again."
func TestValidationFailureAnswersHTTPSubmitter(t *testing.T) {
	c, _, rec := newSeamTestComponent(t)

	msg := seamUserMessage("user-a")
	msg.InReplyTo = "workflow-7"
	resp := c.processTaskSubmissionSync(context.Background(), msg)

	assert.Equal(t, agentic.ResponseTypeError, resp.Type)
	assert.Contains(t, strings.ToLower(resp.Content), "in_reply_to",
		"the answer names the offending field, not a generic retry suggestion")
	assert.NotContains(t, resp.Content, "try again")
	requireSeamRefusal(t, c, rec, seamHTTPSubmission, reasonSubmissionInvalid)
}

// spec: agentic-dispatch / A refused or unpublishable submission leaves no tracked loop and no moved gauge
// I5: a submission that publishes no task leaves the tracker and the
// active-loops gauge exactly as it found them. Both refusal classes are covered
// — refused at the gate, and refused by the task's own validation — because the
// leak #1225 reports happened between them.
func TestFailedSubmissionLeavesGaugeAndTrackerUnchanged(t *testing.T) {
	cases := []struct {
		name    string
		arrange func(*Component)
		mutate  func(*agentic.UserMessage)
	}{
		{
			name:    "refused by the gate",
			arrange: func(c *Component) { trackLoopOwnedBy(c, seamTestLoopA, "user-a") },
			mutate:  func(m *agentic.UserMessage) { m.ReplyTo = seamTestLoopA },
		},
		{
			name:    "refused by the task's own validation",
			arrange: func(*Component) {},
			mutate:  func(m *agentic.UserMessage) { m.RunID = "run-42" },
		},
		{
			name:    "refused because the prompt is empty",
			arrange: func(*Component) {},
			mutate:  func(m *agentic.UserMessage) { m.Content = "" },
		},
	}

	for _, tc := range cases {
		for _, lane := range []string{"channel", "http"} {
			t.Run(tc.name+"/"+lane, func(t *testing.T) {
				c, _, _ := newSeamTestComponent(t)
				tc.arrange(c)
				before := c.loopTracker.GetAllLoops()
				gaugeBefore := getGaugeValue(t, c.metrics.activeLoops)

				msg := seamUserMessage("user-b")
				tc.mutate(&msg)
				if lane == "channel" {
					c.handleTaskSubmission(context.Background(), msg)
				} else {
					c.processTaskSubmissionSync(context.Background(), msg)
				}

				assert.Len(t, c.loopTracker.GetAllLoops(), len(before),
					"a submission that published no task tracked no loop")
				assert.Equal(t, gaugeBefore, getGaugeValue(t, c.metrics.activeLoops),
					"a submission that published no task moved no gauge")
			})
		}
	}
}

// The publish failure is the one submission failure that still leaves a tracked
// loop: Track runs before the publish so the approval-pending arrival buffer can
// absorb an early event, and untracking on failure is a compensating action a
// later branch can skip. It is still ANSWERED and still COUNTED, which is what
// #1225 asks for; recorded here so the residual is a known shape and not a
// surprise.
func TestPublishFailureAnswersAndCountsButKeepsTheTrackedLoop(t *testing.T) {
	c, _, rec := newSeamTestComponent(t)

	resp := c.processTaskSubmissionSync(context.Background(), seamUserMessage("user-a"))

	assert.Equal(t, agentic.ResponseTypeError, resp.Type)
	assert.Len(t, c.loopTracker.GetAllLoops(), 1)
	requireSeamRefusal(t, c, rec, seamHTTPSubmission, reasonSubmissionUndeliver)
}

// The status mapping has one home, and every refusal the gate can return has an
// answer in it. A code with no mapping would answer 500 at three endpoints at
// once, which is why this enumerates rather than sampling.
func TestEveryRefusalCodeMapsToAnHTTPStatus(t *testing.T) {
	want := map[string]int{
		codeLoopTokenInvalid:  http.StatusBadRequest,
		codeLoopNotFound:      http.StatusNotFound,
		codeLoopNotOwned:      http.StatusForbidden,
		codeLoopNotPermitted:  http.StatusForbidden,
		codeLoopTerminal:      http.StatusConflict,
		codeLoopUnreadable:    http.StatusServiceUnavailable,
		codeLoopOwnerConflict: http.StatusInternalServerError,
	}
	c, _, _ := newSeamTestComponent(t)
	for code, status := range want {
		refusal := c.refuseLoopRequest(loopAdmissionRequest{
			Seam: seamHTTPLoopRead, Field: "id", Operation: loopOpRead, LoopID: seamTestLoopA,
		}, code, assert.AnError)
		got, ok := loopRefusalHTTPStatus(refusal)
		require.True(t, ok, "code %q has no status", code)
		assert.Equal(t, status, got, "code %q", code)
	}
	_, ok := loopRefusalHTTPStatus(assert.AnError)
	assert.False(t, ok, "an unclassified error is not one of this package's refusals")
}

// spec: agentic-dispatch / Loop existence and ownership are merged facts, never process memory alone
// The two read seams do not contradict their own admission. Before the gate,
// each decided existence from the tracker alone, so a loop that outlived the
// process that started it answered "not found" — the shape P2 measures. Now
// existence is merged, and the answer has to come from somewhere.
func TestReadSeamsAnswerFromTheDurableRecordAfterReplacement(t *testing.T) {
	arrange := func(c *Component) {
		// An empty tracker, as after a process replacement, and a live durable
		// record.
		withPersistedLoops(c, map[string]*agentic.LoopEntity{seamTestLoopA: {
			ID: seamTestLoopA, TaskID: "task-x", UserID: "user-a", Role: "assistant",
			ChannelType: "http", ChannelID: "session-1",
			State: agentic.LoopStateExecuting, MaxIterations: 7, Iterations: 3,
		}})
	}

	t.Run("status command", func(t *testing.T) {
		c, _, _ := newSeamTestComponent(t)
		arrange(c)

		resp, err := c.handleStatusCommand(context.Background(),
			seamUserMessage("user-a"), []string{seamTestLoopA}, "")

		require.NoError(t, err)
		assert.Equal(t, agentic.ResponseTypeStatus, resp.Type)
		assert.Contains(t, resp.Content, seamTestLoopA)
		assert.Contains(t, resp.Content, "user-a")
		assert.NotContains(t, resp.Content, "names no loop")
	})

	t.Run("GET /loops/{id}", func(t *testing.T) {
		c, _, _ := newSeamTestComponent(t)
		arrange(c)

		rec := seamHTTPCall(t, c.handleGetLoop, http.MethodGet,
			"/loops/"+seamTestLoopA, seamTestLoopA, "", "user-a")

		require.Equal(t, http.StatusOK, rec.Code)
		var loop Loop
		require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &loop))
		assert.Equal(t, seamTestLoopA, loop.LoopID)
		assert.Equal(t, "executing", loop.State)
		assert.Equal(t, 3, loop.Iterations)
		assert.Equal(t, "user-a", loop.UserID)
	})
}

// The entity-id-contract spec's path-token scenario. Both endpoints that take a
// loop id from the URL path refuse a non-canonical token for its FORM, ahead of
// the existence check, so the caller is told the token is malformed rather than
// that the loop does not exist.
//
// The not-found answer is the bug this pins, not a nicety: `loop_ab12cd34` is a
// token this framework could never have minted, so answering 404 sends the
// caller hunting for a loop that no state could ever have held, and hides a
// client that is authoring loop ids instead of echoing them back.
//
// This test is named by the spec delta and was missing when section 5 was
// ticked; the citation named it before anything wrote it.
func TestLoopEndpointsRefuseNonCanonicalPathToken(t *testing.T) {
	t.Run("GET /loops/{id}", func(t *testing.T) {
		c, _, rec := newSeamTestComponent(t)

		resp := seamHTTPCall(t, c.handleGetLoop, http.MethodGet,
			"/loops/"+seamTestMalformed, seamTestMalformed, "", "user-a")

		require.Equal(t, http.StatusBadRequest, resp.Code,
			"the form refusal answers 400, never the 404 an absent loop earns")
		assert.Contains(t, resp.Body.String(), "not a loop ID this framework minted")
		assert.NotContains(t, resp.Body.String(), "names no loop")
		requireSeamRefusal(t, c, rec, seamHTTPLoopRead, reasonFormMalformed)
	})

	t.Run("POST /loops/{id}/approval", func(t *testing.T) {
		c, _, rec := newSeamTestComponent(t)

		resp := seamHTTPCall(t, c.handleLoopApproval, http.MethodPost,
			"/loops/"+seamTestMalformed+"/approval", seamTestMalformed,
			`{"decision":"approve"}`, "user-a")

		require.Equal(t, http.StatusBadRequest, resp.Code)
		assert.Contains(t, resp.Body.String(), "not a loop ID this framework minted")
		requireSeamRefusal(t, c, rec, seamHTTPLoopApproval, reasonFormMalformed)
	})
}

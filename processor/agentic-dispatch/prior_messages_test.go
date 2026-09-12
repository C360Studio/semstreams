package agenticdispatch

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"

	"github.com/c360studio/semstreams/agentic"
	"github.com/c360studio/semstreams/message"
	"github.com/c360studio/semstreams/metric"
	"github.com/c360studio/semstreams/natsclient"
	"github.com/c360studio/semstreams/payloadbuiltins"
	"github.com/c360studio/semstreams/pkg/errs"
	"github.com/prometheus/client_golang/prometheus/testutil"
	"github.com/stretchr/testify/require"
)

type priorTaskEvidence struct{ data []byte }

func (e priorTaskEvidence) ReadRetainedTask(context.Context, string, string) ([]byte, bool, error) {
	return e.data, true, nil
}

func displayedPriorMessages() []agentic.ChatMessage {
	return []agentic.ChatMessage{{Role: "user", Content: "remember the number 42"}, {Role: "assistant", Content: "I will remember 42."}}
}

// spec: agentic-dispatch / Prior messages accompany an independent chat turn
func TestPriorMessagesTaskWireAndSourceRecovery(t *testing.T) {
	c, _, _ := newSeamTestComponent(t)
	c.decoder = payloadbuiltins.NewTestDecoder(t)
	msg := seamUserMessage("user")
	msg.PriorMessages = displayedPriorMessages()
	_, vacant, found, err := c.findRetainedDispatchTask(t.Context(), msg)
	require.NoError(t, err)
	require.False(t, found)
	prepared, err := c.prepareNewDispatchTask(t.Context(), msg, "", vacant)
	require.NoError(t, err)
	decoded, err := c.decoder.Decode(prepared.data)
	require.NoError(t, err)
	require.Equal(t, msg.PriorMessages, decoded.Payload().(*agentic.TaskMessage).PriorMessages)

	c.taskEvidence = priorTaskEvidence{prepared.data}
	// Current route authority moved; committed source recovery needs no ready view.
	c.config.AutoContinue = true
	trackLoopOwnedBy(c, seamTestLoopB, msg.UserID)
	recovered, _, found, err := c.findRetainedDispatchTask(t.Context(), msg)
	require.NoError(t, err)
	require.True(t, found)
	require.Equal(t, prepared.data, recovered.data)
	require.Equal(t, prepared.task.LoopID, recovered.task.LoopID)
	for _, changed := range [][]agentic.ChatMessage{
		{{Role: "user", Content: "different"}},
		{msg.PriorMessages[1], msg.PriorMessages[0]},
		{{Role: "assistant", Content: msg.PriorMessages[0].Content}, msg.PriorMessages[1]},
	} {
		msg.PriorMessages = changed
		_, _, _, err := c.findRetainedDispatchTask(t.Context(), msg)
		require.True(t, errs.IsFatal(err), "changed source history must quarantine: %v", err)
		require.ErrorContains(t, err, "prior_messages")
	}
}

// spec: agentic-dispatch / Prior messages accompany an independent chat turn
func TestPriorMessagesEmptySourceRecovery(t *testing.T) {
	c, _, _ := newSeamTestComponent(t)
	c.decoder = payloadbuiltins.NewTestDecoder(t)
	msg := seamUserMessage("user")
	_, slot, _, err := c.findRetainedDispatchTask(t.Context(), msg)
	require.NoError(t, err)
	prepared, err := c.prepareNewDispatchTask(t.Context(), msg, "", slot)
	require.NoError(t, err)
	c.taskEvidence = priorTaskEvidence{prepared.data}
	msg.PriorMessages = []agentic.ChatMessage{}
	got, _, found, err := c.findRetainedDispatchTask(t.Context(), msg)
	require.NoError(t, err)
	require.True(t, found)
	require.Equal(t, prepared.data, got.data)
}

// spec: agentic-dispatch / Prior messages accompany an independent chat turn
func TestPriorMessagesInvalidRetainedSourceIsAnswered(t *testing.T) {
	c, sink, _ := newSeamTestComponent(t)
	c.decoder = payloadbuiltins.NewTestDecoder(t)
	msg := seamUserMessage("user")
	msg.PriorMessages = displayedPriorMessages()
	_, slot, _, err := c.findRetainedDispatchTask(t.Context(), msg)
	require.NoError(t, err)
	prepared, err := c.prepareNewDispatchTask(t.Context(), msg, "", slot)
	require.NoError(t, err)
	c.taskEvidence = priorTaskEvidence{prepared.data}
	msg.PriorMessages[0].Name = "must not bypass validation on redelivery"
	data, err := json.Marshal(message.NewBaseMessage(msg.Schema(), &msg, "test"))
	require.NoError(t, err)
	decision, err := c.handleUserMessage(t.Context(), data)
	require.NoError(t, err)
	require.Equal(t, natsclient.DeliveryDecisionAck, decision)
	require.Len(t, sink.all(), 1)
	require.Equal(t, agentic.ResponseTypeError, sink.all()[0].Type)
	require.Contains(t, sink.all()[0].Content, "prior_messages")
	require.Zero(t, testutil.ToFloat64(c.metrics.tasksSubmitted))
}

// spec: agentic-dispatch / Prior messages accompany an independent chat turn
func TestPriorMessagesRoutableInvalidInputIsAnsweredBeforeMutation(t *testing.T) {
	for _, viaHTTP := range []bool{false, true} {
		t.Run(map[bool]string{false: "channel", true: "http"}[viaHTTP], func(t *testing.T) {
			c, sink, _ := newSeamTestComponent(t)
			c.decoder = payloadbuiltins.NewTestDecoder(t)
			msg := seamUserMessage("user")
			msg.PriorMessages = []agentic.ChatMessage{{Role: "system", Content: "not displayed text"}}
			if viaHTTP {
				body := `{"content":"work","prior_messages":[{"role":"system","content":"not displayed text"}]}`
				rr := httptest.NewRecorder()
				c.handleHTTPMessage(rr, httptest.NewRequest(http.MethodPost, "/message", strings.NewReader(body)))
				var response HTTPMessageResponse
				require.NoError(t, json.Unmarshal(rr.Body.Bytes(), &response))
				require.Equal(t, agentic.ResponseTypeError, response.Type)
				require.Contains(t, response.Content, "prior_messages")
			} else {
				data, err := json.Marshal(message.NewBaseMessage(msg.Schema(), &msg, "test"))
				require.NoError(t, err)
				decision, err := c.handleUserMessage(t.Context(), data)
				require.NoError(t, err)
				require.Equal(t, natsclient.DeliveryDecisionAck, decision, "ACK requires the delivered negative response")
				responses := sink.all()
				require.Len(t, responses, 1)
				require.Equal(t, agentic.ResponseTypeError, responses[0].Type)
				require.Contains(t, responses[0].Content, "prior_messages")
				c.sendResponseFn = nil // Exercise the disconnected real publication path.
				decision, err = c.handleUserMessage(t.Context(), data)
				require.Error(t, err)
				require.Equal(t, natsclient.DeliveryDecisionRetry, decision)
			}
			require.Zero(t, testutil.ToFloat64(c.metrics.tasksSubmitted))
		})
	}
}

// spec: agentic-dispatch / Prior messages accompany an independent chat turn
func TestPriorMessagesConflictsWithAdmittedAttachment(t *testing.T) {
	for _, viaHTTP := range []bool{false, true} {
		for _, explicit := range []bool{false, true} {
			source := newFakeActivitySource()
			hooks := newActivityHookChans()
			c := newActivityTestComponent(t, source, hooks.hooks())
			c.metrics = getMetrics(metric.NewMetricsRegistry())
			c.taskEvidence = emptyRetainedTaskEvidenceReader{}
			c.modelRegistry = newTestRegistry()
			c.natsClient = &natsclient.Client{}
			sink := &captureSink{}
			c.sendResponseFn = sink.add
			msg := seamUserMessage("user")
			msg.PriorMessages = displayedPriorMessages()
			record := &agentic.LoopEntity{
				ID: seamTestLoopA, UserID: msg.UserID, ChannelType: msg.ChannelType, ChannelID: msg.ChannelID,
				State: agentic.LoopStateExecuting, MaxIterations: 5,
			}
			withPersistedLoops(c, map[string]*agentic.LoopEntity{record.ID: record})
			before := *record
			_, err := c.ensureActivityView(t.Context())
			require.NoError(t, err)
			encoded, err := json.Marshal(record)
			require.NoError(t, err)
			watcher := source.waitWatcher(t, 1)
			watcher.updates <- fakeActivityEntry{key: record.ID, rev: 1, value: encoded}
			watcher.updates <- nil
			hooks.waitCaughtUp(t)
			if explicit {
				msg.ReplyTo = seamTestLoopA
			} else {
				c.config.AutoContinue = true
			}
			var response agentic.UserResponse
			if viaHTTP {
				response, err = c.processTaskSubmissionSync(t.Context(), msg)
				require.NoError(t, err)
			} else {
				require.NoError(t, c.handleTaskSubmission(t.Context(), msg))
				responses := sink.all()
				require.Len(t, responses, 1)
				response = responses[0]
			}
			require.Equal(t, agentic.ResponseTypeError, response.Type)
			require.Contains(t, response.Content, "prior_messages")
			require.Equal(t, before, *record)
			require.Zero(t, testutil.ToFloat64(c.metrics.tasksSubmitted))
		}
	}
}

// spec: agentic-dispatch / Prior messages accompany an independent chat turn
func TestPriorMessagesCommandAndDefaultTarget(t *testing.T) {
	require.False(t, DefaultConfig().AutoContinue)
	field, ok := reflect.TypeFor[Config]().FieldByName("AutoContinue")
	require.True(t, ok)
	require.Contains(t, field.Tag.Get("schema"), "default:false")
	for _, viaHTTP := range []bool{false, true} {
		for _, command := range []string{"/help", "/status", "/cancel"} {
			c, sink, _ := newSeamTestComponent(t)
			trackLoopOwnedBy(c, seamTestLoopA, "user")
			msg := seamUserMessage("user")
			msg.Content = command
			msg.PriorMessages = displayedPriorMessages()
			var response agentic.UserResponse
			var err error
			if viaHTTP {
				response, err = c.processCommandSync(t.Context(), msg)
				require.NoError(t, err)
			} else {
				require.NoError(t, c.handleCommand(t.Context(), msg))
				response = sink.all()[0]
			}
			require.Equal(t, agentic.ResponseTypeError, response.Type)
			require.Contains(t, response.Content, "prior_messages")
			if command != "/help" {
				msg.PriorMessages = nil
				response, err = c.processCommandSync(t.Context(), msg)
				require.NoError(t, err)
				require.Contains(t, response.Content, "explicit loop_id")
				require.Equal(t, agentic.ResponseTypeError, response.Type)
			}
		}
	}
}

// spec: agentic-dispatch / Prior messages accompany an independent chat turn
func TestPriorMessagesAutoContinueUnavailableDoesNotAssumeNewWork(t *testing.T) {
	for _, viaHTTP := range []bool{false, true} {
		c, sink, _ := newSeamTestComponent(t)
		c.config.AutoContinue = true
		msg := seamUserMessage("user")
		msg.PriorMessages = displayedPriorMessages()
		var err error
		if viaHTTP {
			var response agentic.UserResponse
			response, err = c.processTaskSubmissionSync(t.Context(), msg)
			require.Equal(t, agentic.UserResponse{}, response)
		} else {
			err = c.handleTaskSubmission(t.Context(), msg)
			require.Empty(t, sink.all())
		}
		require.Error(t, err)
		require.True(t, errs.IsTransient(err))
		require.Zero(t, testutil.ToFloat64(c.metrics.tasksSubmitted))
	}
}

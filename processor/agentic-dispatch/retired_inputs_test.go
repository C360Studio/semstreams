package agenticdispatch

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/c360studio/semstreams/agentic"
	"github.com/c360studio/semstreams/message"
	"github.com/c360studio/semstreams/natsclient"
	"github.com/c360studio/semstreams/payloadbuiltins"
	"github.com/nats-io/nats.go/jetstream"
	"github.com/prometheus/client_golang/prometheus/testutil"
	"github.com/stretchr/testify/require"
)

// spec: agentic-dispatch / Prior messages accompany an independent chat turn
func TestDispatchConfig_RejectsRetiredTargetPresence(t *testing.T) {
	for _, field := range []string{
		`"auto_continue":false`, `"auto_continue":true`, `"auto_continue":null`,
		`"auto_continue":""`, `"AUTO_CONTINUE":false`, `"Auto_Continue":null`,
		`"auto_\u0063ontinue":false`, `"auto_continue":true,"auto_continue":false`,
	} {
		t.Run(field, func(t *testing.T) {
			raw := []byte(`{` + field + `}`)
			_, err := NewComponent(raw, componentDependenciesForCausalTest())
			require.ErrorContains(t, err, "auto_continue", "retired presence must fail before construction")
			_, err = DeclarePorts(raw, "test")
			require.ErrorContains(t, err, "auto_continue")
		})
	}
	for _, raw := range []string{`{}`, `{"unrelated_field":false}`} {
		_, err := NewComponent([]byte(raw), componentDependenciesForCausalTest())
		require.NoError(t, err, "omission and unrelated unknown keys retain their behavior")
	}
}

// spec: agentic-dispatch / Prior messages accompany an independent chat turn
func TestHTTPMessage_RejectsRetiredTargetBeforeEffects(t *testing.T) {
	for _, field := range []string{
		`"reply_to":""`, `"reply_to":null`, `"reply_to":false`,
		`"REPLY_TO":""`, `"Reply_To":null`, `"reply_\u0074o":""`,
		`"reply_to":"` + seamTestLoopA + `","reply_to":null`,
	} {
		for _, content := range []string{"/help", "new work"} {
			t.Run(field+"/"+content, func(t *testing.T) {
				c, sink, recorder := newSeamTestComponent(t)
				// No task read can occur before the synchronous retired-key refusal.
				c.taskEvidence = failingRetainedTaskEvidenceReader{err: errors.New("unexpected task lookup")}
				raw := `{"content":"` + content + `",` + field + `}`
				w := httptest.NewRecorder()
				c.handleHTTPMessage(w, httptest.NewRequest(http.MethodPost, "/message", strings.NewReader(raw)))
				require.Equal(t, http.StatusBadRequest, w.Code, w.Body.String())
				require.Contains(t, w.Body.String(), "reply_to")
				require.Contains(t, w.Body.String(), "prior_messages")
				require.Empty(t, sink.all(), "refusal precedes command effects and optional response mirror")
				requireSeamRefusal(t, c, recorder, seamHTTPSubmission, reasonSubmissionInvalid)
			})
		}
	}
}

// spec: agentic-dispatch / Prior messages accompany an independent chat turn
// spec: agentic-dispatch / Every dispatch durable input settles through its owner
func TestUserMessage_RetiredTargetSettlesAfterNegativeResponse(t *testing.T) {
	for _, failResponse := range []bool{false, true} {
		t.Run(map[bool]string{false: "response_commits", true: "response_fails"}[failResponse], func(t *testing.T) {
			deps := componentDependenciesForCausalTest()
			deps.PayloadRegistry = payloadbuiltins.NewTestRegistry(t)
			discoverable, err := NewComponent([]byte(`{}`), deps)
			require.NoError(t, err)
			c := discoverable.(*Component)
			c.waitForStreamInput = func(context.Context, string) error { return nil }
			callbacks := make(map[string]func(context.Context, jetstream.Msg))
			c.consumeStream = func(_ context.Context, owner natsclient.PortConsumerContext, _ natsclient.StreamConsumerConfig, callback func(context.Context, jetstream.Msg)) (jetstream.ConsumeContext, error) {
				callbacks[owner.Port] = callback
				return &causalConsumeHandle{closed: make(chan struct{}), closedCalls: make(chan struct{}, 1)}, nil
			}
			ctx, cancel := context.WithCancel(t.Context())
			require.NoError(t, c.setupSubscriptions(ctx))
			t.Cleanup(func() {
				cancel()
				for _, binding := range c.consumers {
					<-binding.observerDone
				}
			})
			for _, field := range []string{
				`"reply_to":""`, `"reply_to":null`, `"reply_to":false`,
				`"REPLY_TO":null`, `"Reply_To":""`, `"reply_\u0074o":null`,
				`"reply_to":"` + seamTestLoopA + `","reply_to":""`,
			} {
				for _, content := range []string{"/help", "new work"} {
					msg := &dispatchSettlementMsg{data: retiredUserWire(t, content, field)}
					refusals := testutil.ToFloat64(c.metrics.loopAdmissionRefusals.WithLabelValues(seamChannelSubmission, reasonSubmissionInvalid))
					responses := 0
					if !failResponse {
						c.sendResponseFn = func(response agentic.UserResponse) {
							require.Zero(t, msg.acks.Load()+msg.naks.Load()+msg.terms.Load(), "settlement preceded negative publication")
							require.Equal(t, agentic.ResponseTypeError, response.Type)
							require.Contains(t, response.Content, "reply_to")
							require.Contains(t, response.Content, "prior_messages")
							require.NoError(t, response.Validate())
							responses++
						}
					}
					callbacks["user.message"](ctx, msg)
					require.Equal(t, refusals+1, testutil.ToFloat64(c.metrics.loopAdmissionRefusals.WithLabelValues(seamChannelSubmission, reasonSubmissionInvalid)))
					require.Zero(t, msg.acks.Load(), "invalid source must not ACK")
					if failResponse {
						require.Equal(t, int32(1), msg.naks.Load(), "failed negative publication retries")
						require.Zero(t, msg.terms.Load())
					} else {
						require.Equal(t, 1, responses)
						require.Equal(t, int32(1), msg.terms.Load())
						require.Zero(t, msg.naks.Load())
					}
				}
			}
		})
	}
}

func retiredUserWire(t testing.TB, content, field string) []byte {
	t.Helper()
	msg := seamUserMessage("user")
	msg.Content = content
	wire, err := json.Marshal(message.NewBaseMessage(msg.Schema(), &msg, "test"))
	require.NoError(t, err)
	if field == "" {
		return wire
	}
	var envelope map[string]json.RawMessage
	require.NoError(t, json.Unmarshal(wire, &envelope))
	envelope["payload"] = append(append(envelope["payload"][:len(envelope["payload"])-1], ','), []byte(field+`}`)...)
	wire, err = json.Marshal(envelope)
	require.NoError(t, err)
	return wire
}

// spec: agentic-dispatch / Prior messages accompany an independent chat turn
// Known spellings come from the retired-key grammar, not the implementation's
// comparison. The invariant is presence-based refusal and omission acceptance;
// JSON values may vary without becoming compatibility targets.
func FuzzRetiredTargetJSON(f *testing.F) {
	for i, value := range []string{`null`, `false`, `""`, `[]`, `{}`, `42`, `true`, `"target"`} {
		f.Add(uint8(i), value, i%2 == 0)
	}
	f.Fuzz(func(t *testing.T, spelling uint8, value string, duplicate bool) {
		if !json.Valid([]byte(value)) {
			encoded, err := json.Marshal(value)
			require.NoError(t, err)
			value = string(encoded)
		}
		configKeys := []string{"", `auto_continue`, `AUTO_CONTINUE`, `Auto_Continue`, `auto_\u0063ontinue`}
		userKeys := []string{"", `reply_to`, `REPLY_TO`, `Reply_To`, `reply_\u0074o`}
		index := int(spelling) % len(userKeys)
		configField, userField := `"unrelated_field":`+value, `"unrelated_field":`+value
		if index != 0 {
			configField = `"` + configKeys[index] + `":` + value
			userField = `"` + userKeys[index] + `":` + value
			if duplicate {
				configField += `,"auto_continue":null`
				userField += `,"reply_to":null`
			}
		}
		_, err := NewComponent([]byte(`{`+configField+`}`), componentDependenciesForCausalTest())
		if index == 0 {
			require.NoError(t, err)
		} else {
			require.ErrorContains(t, err, "auto_continue")
		}

		c, sink, _ := newSeamTestComponent(t)
		c.decoder = payloadbuiltins.NewTestDecoder(t)
		wire := retiredUserWire(t, "/help", userField)
		decoded, err := c.decoder.Decode(wire)
		require.NoError(t, err, "registered decoding must retain a routable negative response")
		user := decoded.Payload().(*agentic.UserMessage)
		require.Equal(t, "session-1", user.ChannelID)
		decision, cause := c.handleUserMessage(t.Context(), wire)
		responses := sink.all()
		require.Len(t, responses, 1)
		if index == 0 {
			require.NoError(t, user.Validate())
			require.NoError(t, cause)
			require.Equal(t, natsclient.DeliveryDecisionAck, decision)
			require.Equal(t, agentic.ResponseTypeText, responses[0].Type)
		} else {
			require.ErrorContains(t, user.Validate(), "reply_to")
			require.Equal(t, natsclient.DeliveryDecisionTerminate, decision)
			require.ErrorContains(t, cause, "reply_to")
			require.Equal(t, agentic.ResponseTypeError, responses[0].Type)
			require.Contains(t, responses[0].Content, "prior_messages")
		}
		response := httptest.NewRecorder()
		c.handleHTTPMessage(response, httptest.NewRequest(http.MethodPost, "/message",
			strings.NewReader(`{"content":"/help",`+userField+`}`)))
		if index == 0 {
			require.Equal(t, http.StatusOK, response.Code)
		} else {
			require.Equal(t, http.StatusBadRequest, response.Code)
			require.Contains(t, response.Body.String(), "reply_to")
		}
	})
}

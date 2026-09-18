package agentic

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/c360studio/semstreams/agentic"
	"github.com/c360studio/semstreams/message"
	"github.com/c360studio/semstreams/payloadbuiltins"
	agenticdispatch "github.com/c360studio/semstreams/processor/agentic-dispatch"
	"github.com/c360studio/semstreams/test/e2e/scenarios"
	"github.com/nats-io/nats.go/jetstream"
)

type chatHTTPRoundTrip func(*http.Request) (*http.Response, error)

func (f chatHTTPRoundTrip) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

type chatRetainedStreamFixture struct {
	replayFixtureStream
	records []*jetstream.RawStreamMsg
}

func (s chatRetainedStreamFixture) GetMsg(_ context.Context, sequence uint64, _ ...jetstream.GetMsgOpt) (*jetstream.RawStreamMsg, error) {
	for _, record := range s.records {
		if record.Sequence >= sequence {
			return record, nil
		}
	}
	return nil, jetstream.ErrMsgNotFound
}

// spec: agentic-dispatch / Prior messages accompany an independent chat turn
func TestChatTurnReadsRetainedResultBeforeLaterSubmissionStatus(t *testing.T) {
	input := agenticdispatch.HTTPMessageRequest{
		Content: chatFirstPrompt, ChannelType: "e2e-chat", ChannelID: "channel", UserID: "user",
	}
	response := &agentic.UserResponse{
		ResponseID: "terminal-user-response:terminal", InReplyTo: pagedLoopToken,
		ChannelType: input.ChannelType, ChannelID: input.ChannelID,
		Type: agentic.ResponseTypeResult, Content: "Displayed terminal answer.", Timestamp: time.Now(),
	}
	subject := "user.response.e2e-chat.channel"
	terminal := &jetstream.RawStreamMsg{Subject: subject, Sequence: 11, Data: chatFixtureWire(t, response)}
	status := *response
	status.Type, status.Content = agentic.ResponseTypeStatus, "Task submitted."
	last := &jetstream.RawStreamMsg{Subject: subject, Sequence: 12, Data: chatFixtureWire(t, &status)}
	users := chatRetainedStreamFixture{
		replayFixtureStream: replayFixtureStream{wantSubject: subject, stored: last},
		records:             []*jetstream.RawStreamMsg{terminal, last},
	}
	request := &agentic.AgentRequest{
		LoopID: pagedLoopToken, RequestID: "request",
		Messages: []agentic.ChatMessage{{Role: "user", Content: input.Content}},
	}
	agents := replayFixtureStream{
		wantSubject: "agent.request." + pagedLoopToken,
		stored:      &jetstream.RawStreamMsg{Data: chatFixtureWire(t, request)},
	}
	config := DefaultConfig()
	// No future publication exists: latest-only polling must exhaust this
	// bounded observation budget instead of accidentally seeing a later result.
	config.CompleteTimeout = 20 * time.Millisecond
	s := &Scenario{config: config, decoder: payloadbuiltins.NewTestDecoder(t)}
	s.http = &http.Client{Transport: chatHTTPRoundTrip(func(*http.Request) (*http.Response, error) {
		body := fmt.Sprintf(`{"type":"status","in_reply_to":%q}`, pagedLoopToken)
		return &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader(body))}, nil
	})}
	observed, err := s.submitChatTurn(t.Context(), agents, users, input)
	if err != nil || observed.response == nil || observed.response.Content != response.Content {
		t.Fatalf("retained result hidden by later status: observed=%+v error=%v", observed.response, err)
	}
}

// spec: agentic-dispatch / Prior messages accompany an independent chat turn
// spec: agentic-loop / A task carries its prior conversational input
func TestIndependentChatStageObservesDisplayedHistoryAtHTTPAndRegisteredStreams(t *testing.T) {
	for _, fault := range []string{
		"none", "lost history", "raw provider result", "extra history", "reused loop",
		"wrong request correlation", "wrong response route", "unregistered response", "HTTP rejection",
	} {
		t.Run(fault, func(t *testing.T) {
			const displayed = "  Displayed assistant sentence — not raw provider output.\n"
			agents, users := &replayFixtureStream{}, &chatRetainedStreamFixture{}
			posts := 0
			config := DefaultConfig()
			s := &Scenario{config: config, decoder: payloadbuiltins.NewTestDecoder(t)}
			s.http = &http.Client{Transport: chatHTTPRoundTrip(func(r *http.Request) (*http.Response, error) {
				posts++
				if r.Method != http.MethodPost || r.URL.Path != dispatchRoutePrefix+"/message" {
					t.Fatalf("unexpected HTTP operation %s %s", r.Method, r.URL.Path)
				}
				var input agenticdispatch.HTTPMessageRequest
				if err := json.NewDecoder(r.Body).Decode(&input); err != nil {
					t.Fatal(err)
				}
				loopID := pagedLoopToken
				if posts == 1 {
					if input.Content != "Reply with a short greeting." || len(input.PriorMessages) != 0 {
						t.Fatalf("first HTTP turn = %+v", input)
					}
				} else {
					loopID = "dce0441f-79c0-4e72-aae5-6144b7d8ce39"
					want := []agentic.ChatMessage{
						{Role: "user", Content: "Reply with a short greeting."},
						{Role: "assistant", Content: displayed},
					}
					if input.Content != "Briefly acknowledge our previous exchange." || !reflect.DeepEqual(input.PriorMessages, want) {
						t.Fatalf("second HTTP turn did not carry actual displayed history: %+v", input)
					}
					if fault == "reused loop" {
						loopID = pagedLoopToken
					}
				}
				response := &agentic.UserResponse{
					ResponseID: "terminal-user-response:" + loopID, InReplyTo: loopID,
					ChannelType: input.ChannelType, ChannelID: input.ChannelID,
					Type: agentic.ResponseTypeResult, Content: displayed, Timestamp: time.Now(),
				}
				if fault == "wrong response route" {
					response.ChannelID = "other"
				}
				users.wantSubject = "user.response." + input.ChannelType + "." + input.ChannelID
				users.stored = &jetstream.RawStreamMsg{Sequence: 1, Data: chatFixtureWire(t, response)}
				if fault == "unregistered response" {
					users.stored.Data = []byte(`{"type":"unregistered.response.v1","payload":{}}`)
				}
				users.records = []*jetstream.RawStreamMsg{users.stored}
				request := &agentic.AgentRequest{
					LoopID: loopID, RequestID: fmt.Sprintf("request-%d", posts),
					Messages: append([]agentic.ChatMessage{{Role: "system", Content: "Framework prompt"}}, input.PriorMessages...),
				}
				request.Messages = append(request.Messages, agentic.ChatMessage{Role: "user", Content: input.Content})
				if posts == 2 {
					switch fault {
					case "lost history":
						request.Messages = request.Messages[len(request.Messages)-1:]
					case "raw provider result":
						request.Messages[2].Content = "Raw provider output."
					case "extra history":
						request.Messages = append(request.Messages, agentic.ChatMessage{Role: "assistant", Content: "extra"})
					case "wrong request correlation":
						request.LoopID = pagedLoopToken
					}
				}
				agents.wantSubject = "agent.request." + loopID
				agents.stored = &jetstream.RawStreamMsg{Data: chatFixtureWire(t, request)}
				status := http.StatusOK
				if fault == "HTTP rejection" {
					status = http.StatusForbidden
				}
				body, err := json.Marshal(agenticdispatch.HTTPMessageResponse{Type: agentic.ResponseTypeStatus, InReplyTo: loopID})
				if err != nil {
					t.Fatal(err)
				}
				return &http.Response{StatusCode: status, Body: io.NopCloser(strings.NewReader(string(body)))}, nil
			})}
			result := &scenarios.Result{Details: map[string]any{}}
			err := s.walkChatTurns(t.Context(), result, agents, users)
			if fault != "none" {
				if err == nil {
					t.Fatalf("chat stage accepted %s", fault)
				}
				return
			}
			if err != nil || posts != 2 || result.Details["chat_displayed_response"] != displayed {
				t.Fatalf("chat stage posts=%d displayed=%q error=%v", posts, result.Details["chat_displayed_response"], err)
			}
		})
	}
}

func chatFixtureWire(t *testing.T, payload message.Payload) []byte {
	t.Helper()
	wire, err := json.Marshal(message.NewBaseMessage(payload.Schema(), payload, "e2e-test"))
	if err != nil {
		t.Fatal(err)
	}
	return wire
}

// spec: agentic-dispatch / Prior messages accompany an independent chat turn
func TestChatTurnDoesNotMistakeAnAcknowledgementOrAnotherLoopForDisplayedText(t *testing.T) {
	for _, fault := range []string{"submission acknowledgement", "other loop"} {
		t.Run(fault, func(t *testing.T) {
			input := agenticdispatch.HTTPMessageRequest{
				Content: chatFirstPrompt, ChannelType: "e2e-chat", ChannelID: "channel", UserID: "user",
			}
			response := &agentic.UserResponse{
				ResponseID: "response", InReplyTo: pagedLoopToken,
				ChannelType: input.ChannelType, ChannelID: input.ChannelID,
				Type: agentic.ResponseTypeStatus, Content: "Task submitted.", Timestamp: time.Now(),
			}
			if fault == "other loop" {
				response.Type, response.InReplyTo = agentic.ResponseTypeResult, "dce0441f-79c0-4e72-aae5-6144b7d8ce39"
			}
			stored := &jetstream.RawStreamMsg{Sequence: 1, Data: chatFixtureWire(t, response)}
			users := chatRetainedStreamFixture{
				replayFixtureStream: replayFixtureStream{wantSubject: "user.response.e2e-chat.channel", stored: stored},
				records:             []*jetstream.RawStreamMsg{stored},
			}
			s := &Scenario{config: DefaultConfig(), decoder: payloadbuiltins.NewTestDecoder(t)}
			s.http = &http.Client{Transport: chatHTTPRoundTrip(func(*http.Request) (*http.Response, error) {
				body := fmt.Sprintf(`{"type":"status","in_reply_to":%q}`, pagedLoopToken)
				return &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader(body))}, nil
			})}
			ctx, cancel := context.WithCancel(t.Context())
			cancel() // The retained record is read, then waiting for the real result must observe cancellation.
			observed, err := s.submitChatTurn(ctx, replayFixtureStream{}, users, input)
			if !errors.Is(err, context.Canceled) || observed.response != nil {
				t.Fatalf("%s satisfied displayed text: response=%+v error=%v", fault, observed.response, err)
			}
		})
	}
}

// spec: agentic-dispatch / Prior messages accompany an independent chat turn
func TestRetiredTargetEvidenceRejectsAcceptedInputAndTaskSideEffects(t *testing.T) {
	for _, fault := range []string{"none", "HTTP accepted", "USER accepted", "wrong refusal", "task published"} {
		t.Run(fault, func(t *testing.T) {
			details := chatRetirementDetails()
			stream := stageADispatchStreamFixture{
				replayFixtureStream: replayFixtureStream{wantSubject: "agent.task.fixture"}, count: 7,
			}
			before, err := retainedTaskCount(t.Context(), stream)
			if err != nil {
				t.Fatal(err)
			}
			switch fault {
			case "HTTP accepted":
				details["retired_http_status"] = http.StatusOK
			case "USER accepted":
				details["retired_user_response"].(*agentic.UserResponse).Type = agentic.ResponseTypeStatus
			case "wrong refusal":
				details["retired_user_response"].(*agentic.UserResponse).Content = "permission denied"
			case "task published":
				stream.count++
			}
			after, err := retainedTaskCount(t.Context(), stream)
			if err != nil {
				t.Fatal(err)
			}
			details["retired_tasks_before"], details["retired_tasks_after"] = before, after
			if err := validateRetiredTargetEvidence(details); (err == nil) != (fault == "none") {
				t.Fatalf("retired-target evidence fault=%s, error=%v", fault, err)
			}
		})
	}
}

func chatRetirementDetails() map[string]any {
	return map[string]any{
		"chat_first_loop_id":      pagedLoopToken,
		"chat_second_loop_id":     "dce0441f-79c0-4e72-aae5-6144b7d8ce39",
		"chat_displayed_response": "Displayed answer, not the raw provider result.",
		"chat_second_request": &agentic.AgentRequest{
			LoopID: "dce0441f-79c0-4e72-aae5-6144b7d8ce39", RequestID: "second-request",
			Messages: []agentic.ChatMessage{
				{Role: "user", Content: "Reply with a short greeting."},
				{Role: "assistant", Content: "Displayed answer, not the raw provider result."},
				{Role: "user", Content: "Briefly acknowledge our previous exchange."},
			},
		},
		"retired_http_status":   http.StatusBadRequest,
		"retired_user_response": &agentic.UserResponse{Type: agentic.ResponseTypeError, Content: "reply_to is retired"},
		"retired_tasks_before":  uint64(7),
		"retired_tasks_after":   uint64(7),
	}
}

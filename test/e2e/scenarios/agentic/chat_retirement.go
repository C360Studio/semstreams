package agentic

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"reflect"
	"strings"
	"time"

	"github.com/c360studio/semstreams/agentic"
	"github.com/c360studio/semstreams/internal/looptoken"
	"github.com/c360studio/semstreams/message"
	agenticdispatch "github.com/c360studio/semstreams/processor/agentic-dispatch"
	"github.com/c360studio/semstreams/test/e2e/scenarios"
	"github.com/google/uuid"
	"github.com/nats-io/nats.go/jetstream"
)

const (
	chatFirstPrompt  = "Reply with a short greeting."
	chatSecondPrompt = "Briefly acknowledge our previous exchange."
)

func (s *Scenario) walkIndependentChatTurns(ctx context.Context, result *scenarios.Result) error {
	js, err := s.nats.Client().JetStream()
	if err != nil {
		return err
	}
	agents, err := js.Stream(ctx, agentStream)
	if err != nil {
		return err
	}
	users, err := js.Stream(ctx, "USER")
	if err != nil {
		return err
	}
	return s.walkChatTurns(ctx, result, agents, users)
}

// walkChatTurns uses the same route for two independent submissions. Only the
// displayed first response supplies the assistant history for the second.
func (s *Scenario) walkChatTurns(
	ctx context.Context, result *scenarios.Result, agents, users jetstream.Stream,
) error {
	input := agenticdispatch.HTTPMessageRequest{
		UserID: "e2e-chat", ChannelType: "e2e-chat", ChannelID: uuid.NewString(), Content: chatFirstPrompt,
	}
	first, err := s.submitChatTurn(ctx, agents, users, input)
	if err != nil {
		return fmt.Errorf("first chat turn: %w", err)
	}
	input.Content = chatSecondPrompt
	input.PriorMessages = []agentic.ChatMessage{
		{Role: "user", Content: chatFirstPrompt},
		{Role: "assistant", Content: first.response.Content},
	}
	second, err := s.submitChatTurn(ctx, agents, users, input)
	if err != nil {
		return fmt.Errorf("second chat turn: %w", err)
	}
	result.Details["chat_first_loop_id"] = first.loopID
	result.Details["chat_second_loop_id"] = second.loopID
	result.Details["chat_displayed_response"] = first.response.Content
	result.Details["chat_second_request"] = second.request
	return validateChatTurnEvidence(result.Details)
}

type chatTurnObservation struct {
	loopID   string
	response *agentic.UserResponse
	request  *agentic.AgentRequest
}

func (s *Scenario) submitChatTurn(
	ctx context.Context, agents, users jetstream.Stream, input agenticdispatch.HTTPMessageRequest,
) (chatTurnObservation, error) {
	var observed chatTurnObservation
	status, body, err := s.postJSON(ctx, dispatchRoutePrefix+"/message", input)
	if err != nil {
		return observed, err
	}
	var accepted agenticdispatch.HTTPMessageResponse
	if err := json.Unmarshal(body, &accepted); err != nil {
		return observed, fmt.Errorf("decode chat acceptance: %w", err)
	}
	if status != http.StatusOK || accepted.Type != agentic.ResponseTypeStatus || !looptoken.Valid(accepted.InReplyTo) {
		return observed, fmt.Errorf("chat acceptance status=%d body=%s", status, body)
	}
	observed.loopID = accepted.InReplyTo
	// A submission acknowledgement can arrive AFTER the terminal result on
	// this route. Walk retained records in order; the last record alone cannot
	// prove absence of the displayed response for this exact turn.
	deadline := time.Now().Add(s.config.CompleteTimeout)
	subject := "user.response." + input.ChannelType + "." + input.ChannelID
	next := uint64(1)
	for {
		stored, err := users.GetMsg(ctx, next, jetstream.WithGetMsgSubject(subject))
		if err != nil {
			if !isMsgNotFound(err) {
				return observed, err
			}
		} else {
			next = stored.Sequence + 1
			decoded, err := s.decoder.Decode(stored.Data)
			if err != nil {
				return observed, err
			}
			response, ok := decoded.Payload().(*agentic.UserResponse)
			if !ok {
				return observed, fmt.Errorf("chat response payload = %T, want registered UserResponse", decoded.Payload())
			}
			if response.InReplyTo == observed.loopID && response.Type != agentic.ResponseTypeStatus {
				if response.Type != agentic.ResponseTypeResult || response.Content == "" ||
					response.ChannelType != input.ChannelType || response.ChannelID != input.ChannelID ||
					response.ResponseID == "" || response.Timestamp.IsZero() {
					return observed, fmt.Errorf("chat terminal response is not a routed nonempty result: %+v", response)
				}
				observed.response = response
				break
			}
		}
		if !time.Now().Before(deadline) {
			return observed, fmt.Errorf("no displayed result for chat loop %s", observed.loopID)
		}
		if stored != nil {
			continue // Consume already-retained records before waiting for a future publication.
		}
		if err := waitDuration(ctx, 200*time.Millisecond); err != nil {
			return observed, err
		}
	}
	data, err := waitForStreamSubjectData(ctx, agents, "agent.request."+observed.loopID, s.config.TaskTimeout)
	if err != nil {
		return observed, err
	}
	decoded, err := s.decoder.Decode(data)
	if err != nil {
		return observed, err
	}
	request, ok := decoded.Payload().(*agentic.AgentRequest)
	if !ok || request.LoopID != observed.loopID || request.RequestID == "" {
		return observed, fmt.Errorf("chat retained request has wrong type or correlation: %T", decoded.Payload())
	}
	observed.request = request
	return observed, nil
}

func validateChatTurnEvidence(details map[string]any) error {
	first, _ := details["chat_first_loop_id"].(string)
	second, _ := details["chat_second_loop_id"].(string)
	displayed, _ := details["chat_displayed_response"].(string)
	request, _ := details["chat_second_request"].(*agentic.AgentRequest)
	if !looptoken.Valid(first) || !looptoken.Valid(second) || first == second || displayed == "" ||
		request == nil || request.LoopID != second || request.RequestID == "" {
		return fmt.Errorf("independent chat turn identity/displayed response/request evidence is missing or inconsistent")
	}
	// Framework-owned system prompts precede the caller transcript. No other
	// extra, reordered, trimmed, or provider-raw message may satisfy this proof.
	messages := request.Messages
	for len(messages) > 0 && messages[0].Role == "system" {
		messages = messages[1:]
	}
	want := []agentic.ChatMessage{
		{Role: "user", Content: chatFirstPrompt}, {Role: "assistant", Content: displayed},
		{Role: "user", Content: chatSecondPrompt},
	}
	if !reflect.DeepEqual(messages, want) {
		return fmt.Errorf("second retained AgentRequest history = %#v, want exact displayed transcript %#v", messages, want)
	}
	return nil
}

func (s *Scenario) refuseRetiredTargetInputs(ctx context.Context, result *scenarios.Result) error {
	js, err := s.nats.Client().JetStream()
	if err != nil {
		return err
	}
	agents, err := js.Stream(ctx, agentStream)
	if err != nil {
		return err
	}
	users, err := js.Stream(ctx, "USER")
	if err != nil {
		return err
	}
	before, err := retainedTaskCount(ctx, agents)
	if err != nil {
		return err
	}
	status, body, err := s.postJSON(ctx, dispatchRoutePrefix+"/message", map[string]any{
		"content": chatFirstPrompt, "user_id": "e2e-chat", "reply_to": nil,
	})
	if err != nil {
		return err
	}
	if status != http.StatusBadRequest || !strings.Contains(string(body), "reply_to") {
		return fmt.Errorf("retired HTTP input status=%d body=%s, want 400 naming reply_to", status, body)
	}
	result.Details["retired_http_status"] = status

	input := agentic.UserMessage{
		MessageID: uuid.NewString(), ChannelType: "e2e-retired", ChannelID: uuid.NewString(),
		UserID: "e2e-chat", Content: chatFirstPrompt, Timestamp: time.Now(),
	}
	wire, err := json.Marshal(message.NewBaseMessage(input.Schema(), &input, "e2e-test"))
	if err != nil {
		return err
	}
	var envelope map[string]json.RawMessage
	if err := json.Unmarshal(wire, &envelope); err != nil {
		return err
	}
	var payload map[string]json.RawMessage
	if err := json.Unmarshal(envelope["payload"], &payload); err != nil {
		return err
	}
	// Inject on the wire: the retired field intentionally has no Go carrier.
	payload["RePlY_To"] = json.RawMessage("null")
	envelope["payload"], err = json.Marshal(payload)
	if err != nil {
		return err
	}
	wire, err = json.Marshal(envelope)
	if err != nil {
		return err
	}
	consumer, err := users.Consumer(ctx, "agentic-dispatch-user-message")
	if err != nil {
		return err
	}
	baseline, err := consumer.Info(ctx)
	if err != nil {
		return err
	}
	ack, err := js.Publish(ctx, "user.message."+input.ChannelType+"."+input.ChannelID, wire)
	if err != nil {
		return err
	}
	data, err := waitForStreamSubjectData(ctx, users,
		"user.response."+input.ChannelType+"."+input.ChannelID, s.config.TaskTimeout)
	if err != nil {
		return err
	}
	decoded, err := s.decoder.Decode(data)
	if err != nil {
		return err
	}
	response, ok := decoded.Payload().(*agentic.UserResponse)
	if !ok || response.ChannelType != input.ChannelType || response.ChannelID != input.ChannelID ||
		response.UserID != input.UserID || response.ResponseID == "" || response.Timestamp.IsZero() {
		return fmt.Errorf("retired USER response has wrong registered type or route: %T", decoded.Payload())
	}
	// This sequential stage has no other USER publishers. Observe delivery of
	// this source and settlement before checking that it emitted no task.
	if err := waitForConsumerDelivery(ctx, consumer, ack.Sequence, s.config.TaskTimeout); err != nil {
		return err
	}
	if err := waitForConsumerSettled(ctx, consumer, baseline.Delivered.Consumer+1, s.config.TaskTimeout); err != nil {
		return err
	}
	after, err := retainedTaskCount(ctx, agents)
	if err != nil {
		return err
	}
	result.Details["retired_user_response"] = response
	result.Details["retired_tasks_before"] = before
	result.Details["retired_tasks_after"] = after
	return validateRetiredTargetEvidence(result.Details)
}

func retainedTaskCount(ctx context.Context, stream jetstream.Stream) (uint64, error) {
	info, err := stream.Info(ctx, jetstream.WithSubjectFilter("agent.task.>"))
	if err != nil {
		return 0, err
	}
	var count uint64
	for _, retained := range info.State.Subjects {
		count += retained
	}
	return count, nil
}

func validateRetiredTargetEvidence(details map[string]any) error {
	status, _ := details["retired_http_status"].(int)
	response, _ := details["retired_user_response"].(*agentic.UserResponse)
	before, haveBefore := details["retired_tasks_before"].(uint64)
	after, haveAfter := details["retired_tasks_after"].(uint64)
	if status != http.StatusBadRequest || response == nil || response.Type != agentic.ResponseTypeError ||
		!strings.Contains(response.Content, "reply_to") || !haveBefore || !haveAfter || before != after {
		return fmt.Errorf("retired targeting lacks HTTP/registered USER refusal or emitted task work")
	}
	return nil
}

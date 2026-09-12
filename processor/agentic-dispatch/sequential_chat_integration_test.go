//go:build integration

package agenticdispatch

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/c360studio/semstreams/agentic"
	"github.com/c360studio/semstreams/component"
	"github.com/c360studio/semstreams/message"
	"github.com/c360studio/semstreams/model"
	"github.com/c360studio/semstreams/natsclient"
	"github.com/c360studio/semstreams/payloadbuiltins"
	agenticloop "github.com/c360studio/semstreams/processor/agentic-loop"
	agenticmodel "github.com/c360studio/semstreams/processor/agentic-model"
	"github.com/google/uuid"
	"github.com/nats-io/nats.go/jetstream"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// spec: agentic-dispatch / Prior messages accompany an independent chat turn
// spec: agentic-loop / A task carries its prior conversational input
// This is operational chat acceptance: production constructors, started owners,
// HTTP intake, registered envelopes, real NATS, and the real HTTP model client.
// Graph projection and optional trajectory evidence storage are outside this proof.
func TestIntegrationSequentialChatAfterComponentReplacement(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), 45*time.Second)
	defer cancel()
	tc := natsclient.NewTestClient(t, natsclient.WithStreams(
		natsclient.TestStreamConfig{Name: "AGENT", Subjects: []string{"agent.>", "tool.>"}},
		natsclient.TestStreamConfig{Name: "USER", Subjects: []string{"user.>"}},
	))
	const firstPrompt = "My lantern color is amber."
	const firstAnswer = "Your spare token is cobalt."
	const nextPrompt = "What were my lantern color and spare token?"
	providerRequests := make(chan []agentic.ChatMessage, 4)
	provider := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var request struct {
			Messages []agentic.ChatMessage `json:"messages"`
		}
		if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		select {
		case providerRequests <- request.Messages:
		case <-r.Context().Done():
			return
		}
		answer := "missing current prompt"
		if len(request.Messages) > 0 {
			current := request.Messages[len(request.Messages)-1]
			if current.Content == firstPrompt {
				answer = firstAnswer
			} else if current.Content == nextPrompt {
				// This branch computes both facts solely from prior wire messages;
				// it keeps no conversation state for answering the follow-up.
				color, token := "unknown", "unknown"
				for _, prior := range request.Messages[:len(request.Messages)-1] {
					if prior.Role == "user" && strings.HasPrefix(prior.Content, "My lantern color is ") {
						color = strings.TrimSuffix(strings.TrimPrefix(prior.Content, "My lantern color is "), ".")
					}
					if prior.Role == "assistant" && strings.HasPrefix(prior.Content, "Your spare token is ") {
						token = strings.TrimSuffix(strings.TrimPrefix(prior.Content, "Your spare token is "), ".")
					}
				}
				answer = fmt.Sprintf("lantern=%s; token=%s", color, token)
			}
		}
		w.Header().Set("Content-Type", "application/json")
		if err := json.NewEncoder(w).Encode(map[string]any{
			"choices": []map[string]any{{
				"message":       map[string]any{"role": "assistant", "content": answer},
				"finish_reason": "stop",
			}},
			"usage": map[string]int{"prompt_tokens": 40, "completion_tokens": 10},
		}); err != nil {
			t.Errorf("encode fake provider response: %v", err)
		}
	}))
	defer provider.Close()
	models := &model.Registry{
		Endpoints: map[string]*model.EndpointConfig{
			"chat-test": {URL: provider.URL + "/v1/chat/completions", Model: "chat-test", MaxTokens: 128000},
		},
		Defaults: model.DefaultsConfig{Model: "chat-test"},
	}
	userStream, err := tc.Client.GetStream(ctx, "USER")
	require.NoError(t, err)
	responses, err := userStream.CreateOrUpdateConsumer(ctx, jetstream.ConsumerConfig{
		Name: "displayed-chat-responses", FilterSubject: "user.response.>", AckPolicy: jetstream.AckExplicitPolicy,
	})
	require.NoError(t, err)
	agentStream, err := tc.Client.GetStream(ctx, "AGENT")
	require.NoError(t, err)
	decoder := payloadbuiltins.NewTestDecoder(t)

	firstMux, stopFirst := startSequentialChatComponents(t, ctx, tc.Client, models)
	firstSubmission := submitSequentialChatTurn(t, ctx, firstMux, firstPrompt, nil)
	firstDisplayed := awaitSequentialChatResponse(t, ctx, responses, decoder, firstSubmission.InReplyTo)
	require.Equal(t, firstAnswer, firstDisplayed.Content)
	awaitSequentialChatSettled(t, ctx, tc.Client, agentStream, firstSubmission.InReplyTo)

	// The adapter retains exactly what it displayed, independently of component
	// memory, the raw provider response, and the COMPLETE_ record.
	transcript := []agentic.ChatMessage{
		{Role: "user", Content: firstPrompt},
		{Role: "assistant", Content: firstDisplayed.Content},
	}
	stopFirst()
	replacementMux, stopReplacement := startSequentialChatComponents(t, ctx, tc.Client, models)
	secondSubmission := submitSequentialChatTurn(t, ctx, replacementMux, nextPrompt, transcript)
	require.NotEqual(t, firstSubmission.InReplyTo, secondSubmission.InReplyTo,
		"a completed turn must not donate its execution identity to the follow-up")
	secondDisplayed := awaitSequentialChatResponse(t, ctx, responses, decoder, secondSubmission.InReplyTo)
	require.Equal(t, "lantern=amber; token=cobalt", secondDisplayed.Content,
		"the fake provider can answer only from prior user and displayed assistant text")
	awaitSequentialChatSettled(t, ctx, tc.Client, agentStream, secondSubmission.InReplyTo)
	stopReplacement()

	wantConversation := append(append([]agentic.ChatMessage(nil), transcript...),
		agentic.ChatMessage{Role: "user", Content: nextPrompt})
	for turn, loopID := range []string{firstSubmission.InReplyTo, secondSubmission.InReplyTo} {
		raw, readErr := agentStream.GetLastMsgForSubject(ctx, "agent.request."+loopID)
		require.NoError(t, readErr)
		decoded, decodeErr := decoder.Decode(raw.Data)
		require.NoError(t, decodeErr)
		request, ok := decoded.Payload().(*agentic.AgentRequest)
		require.Truef(t, ok, "request payload is %T", decoded.Payload())
		require.Equal(t, loopID, request.LoopID)
		var wireMessages []agentic.ChatMessage
		select {
		case wireMessages = <-providerRequests:
		case <-ctx.Done():
			t.Fatal("provider request observation: ", ctx.Err())
		}
		for _, messages := range [][]agentic.ChatMessage{request.Messages, wireMessages} {
			var conversation []agentic.ChatMessage
			var budgets []string
			for _, msg := range messages {
				if msg.Role == "user" || msg.Role == "assistant" {
					conversation = append(conversation, msg)
				}
				// Provider adapters may merge adjacent system messages. The
				// budget instruction must survive once regardless of that grouping.
				for _, line := range strings.Split(msg.Content, "\n") {
					if strings.HasPrefix(line, "[Iteration Budget]") {
						require.Equal(t, "system", msg.Role)
						budgets = append(budgets, line)
					}
				}
			}
			require.Equal(t, []string{"[Iteration Budget] Iteration 1 of 3 (33% used)."}, budgets,
				"each execution owns one fresh budget instruction")
			if turn == 0 {
				require.Equal(t, []agentic.ChatMessage{{Role: "user", Content: firstPrompt}}, conversation)
			} else {
				require.Equal(t, wantConversation, conversation, "preserve order and include every turn exactly once")
			}
			for _, turn := range conversation {
				occurrences := 0
				for _, msg := range messages {
					occurrences += strings.Count(msg.Content, turn.Content)
				}
				require.Equal(t, 1, occurrences, "turn must not also appear in execution instructions: %s", turn.Content)
			}
		}
	}
	require.Empty(t, providerRequests, "both completed executions made exactly one provider call")
	require.False(t, DefaultConfig().AutoContinue, "ordinary submissions must be independent under defaults")
}

// spec: agentic-dispatch / Prior messages accompany an independent chat turn
// This focused terminal-input proof starts from a persisted completed execution;
// it exercises dispatch's projection and HTTP handoff, not the decide tool itself.
func TestIntegrationSequentialChatCarriesDisplayedDecisionReason(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), 20*time.Second)
	defer cancel()
	tc := natsclient.NewTestClient(t, natsclient.WithKVBuckets("AGENT_LOOPS"), natsclient.WithStreams(
		natsclient.TestStreamConfig{Name: "AGENT", Subjects: []string{"agent.>"}},
		natsclient.TestStreamConfig{Name: "USER", Subjects: []string{"user.>"}},
	))
	config := DefaultConfig()
	config.ConsumerNameSuffix = "displayed-decision"
	rawConfig, err := json.Marshal(config)
	require.NoError(t, err)
	dispatch, err := NewComponent(rawConfig, component.Dependencies{
		NATSClient: tc.Client, ModelRegistry: newTestRegistry(), PayloadRegistry: payloadbuiltins.NewTestRegistry(t),
	})
	require.NoError(t, err)
	owner := dispatch.(component.LifecycleComponent)
	require.NoError(t, owner.Initialize())
	require.NoError(t, owner.Start(ctx))
	t.Cleanup(func() {
		stopCtx, stopCancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer stopCancel()
		require.NoError(t, owner.Stop(stopCtx))
	})
	mux := http.NewServeMux()
	dispatch.(*Component).RegisterHTTPHandlers("/", mux)
	userStream, err := tc.Client.GetStream(ctx, "USER")
	require.NoError(t, err)
	responses, err := userStream.CreateOrUpdateConsumer(ctx, jetstream.ConsumerConfig{
		Name: "displayed-decision-response", FilterSubject: "user.response.>", AckPolicy: jetstream.AckExplicitPolicy,
	})
	require.NoError(t, err)
	agentStream, err := tc.Client.GetStream(ctx, "AGENT")
	require.NoError(t, err)
	tasks, err := agentStream.CreateOrUpdateConsumer(ctx, jetstream.ConsumerConfig{
		Name: "displayed-decision-task", FilterSubject: "agent.task.*", AckPolicy: jetstream.AckExplicitPolicy,
	})
	require.NoError(t, err)
	loop := agentic.LoopEntity{
		ID: uuid.NewString(), TaskID: "displayed-decision-task", State: agentic.LoopStateComplete,
		MaxIterations: 3, UserID: "sequential-chat-user", ChannelType: "http", ChannelID: "sequential-chat-session",
	}
	bucket, err := tc.Client.GetKeyValueBucket(ctx, "AGENT_LOOPS")
	require.NoError(t, err)
	stateJSON, err := json.Marshal(loop)
	require.NoError(t, err)
	_, err = bucket.Put(ctx, loop.ID, stateJSON)
	require.NoError(t, err)
	terminal := &agentic.LoopCompletedEvent{
		LoopID: loop.ID, TaskID: loop.TaskID, Outcome: agentic.OutcomeSuccess,
		Result: `{"internal":"raw terminal result must not become displayed history"}`,
		Decision: &agentic.CoordinatorDecision{
			Action: agentic.DecideActionRespondDirect, Reason: "The displayed answer is saffron.",
		},
		CompletedAt: time.Now(),
	}
	terminalJSON, err := json.Marshal(message.NewBaseMessage(terminal.Schema(), terminal, "sequential-chat-test"))
	require.NoError(t, err)
	require.NoError(t, tc.Client.PublishToStream(ctx, "agent.complete."+loop.ID, terminalJSON))
	decoder := payloadbuiltins.NewTestDecoder(t)
	displayed := awaitSequentialChatResponse(t, ctx, responses, decoder, loop.ID)
	require.Equal(t, terminal.Decision.Reason, displayed.Content)
	require.NotEqual(t, terminal.Result, displayed.Content)
	prior := []agentic.ChatMessage{
		{Role: "user", Content: "Which answer should I retain?"},
		{Role: "assistant", Content: displayed.Content},
	}
	submitted := submitSequentialChatTurn(t, ctx, mux, "Repeat the displayed answer.", prior)
	require.NotEqual(t, loop.ID, submitted.InReplyTo)
	delivery, err := tasks.Next(jetstream.FetchMaxWait(5 * time.Second))
	require.NoError(t, err)
	decoded, err := decoder.Decode(delivery.Data())
	require.NoError(t, err)
	task, ok := decoded.Payload().(*agentic.TaskMessage)
	require.Truef(t, ok, "task payload is %T", decoded.Payload())
	require.Equal(t, prior, task.PriorMessages)
	require.NoError(t, delivery.DoubleAck(ctx))
}

func startSequentialChatComponents(
	t *testing.T, ctx context.Context, client *natsclient.Client, models *model.Registry,
) (*http.ServeMux, func()) {
	t.Helper()
	deps := component.Dependencies{
		NATSClient: client, ModelRegistry: models, PayloadRegistry: payloadbuiltins.NewTestRegistry(t),
	}
	loopConfig := agenticloop.DefaultConfig()
	loopConfig.MaxIterations = 3
	loopConfig.ConsumerNameSuffix = "sequential-chat"
	loopJSON, err := json.Marshal(loopConfig)
	require.NoError(t, err)
	loop, err := agenticloop.NewComponent(loopJSON, deps)
	require.NoError(t, err)
	modelConfig := agenticmodel.DefaultConfig()
	modelConfig.ConsumerNameSuffix = "sequential-chat"
	modelConfig.Timeout = "5s"
	modelJSON, err := json.Marshal(modelConfig)
	require.NoError(t, err)
	modelComponent, err := agenticmodel.NewComponent(modelJSON, deps)
	require.NoError(t, err)
	dispatchConfig := DefaultConfig()
	dispatchConfig.ConsumerNameSuffix = "sequential-chat"
	dispatchJSON, err := json.Marshal(dispatchConfig)
	require.NoError(t, err)
	dispatch, err := NewComponent(dispatchJSON, deps)
	require.NoError(t, err)
	var started []component.LifecycleComponent
	stop := func() {
		t.Helper()
		stopCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		for i := len(started) - 1; i >= 0; i-- {
			require.NoError(t, started[i].Stop(stopCtx), "stop/join component %d", i)
		}
		started = nil
	}
	t.Cleanup(stop)
	for _, discoverable := range []component.Discoverable{modelComponent, loop, dispatch} {
		owner, ok := discoverable.(component.LifecycleComponent)
		require.True(t, ok)
		require.NoError(t, owner.Initialize())
		require.NoError(t, owner.Start(ctx))
		started = append(started, owner)
	}
	mux := http.NewServeMux()
	dispatch.(*Component).RegisterHTTPHandlers("/", mux)
	return mux, stop
}

func submitSequentialChatTurn(
	t *testing.T, ctx context.Context, mux *http.ServeMux, content string, prior []agentic.ChatMessage,
) HTTPMessageResponse {
	t.Helper()
	data, err := json.Marshal(map[string]any{
		"content": content, "channel_type": "http", "channel_id": "sequential-chat-session",
		"prior_messages": prior,
	})
	require.NoError(t, err)
	request := httptest.NewRequest(http.MethodPost, "/message", bytes.NewReader(data))
	request = request.WithContext(WithIdentity(ctx, "sequential-chat-user"))
	recorder := httptest.NewRecorder()
	mux.ServeHTTP(recorder, request)
	require.Equal(t, http.StatusOK, recorder.Code, "%s", recorder.Body.String())
	var response HTTPMessageResponse
	require.NoError(t, json.Unmarshal(recorder.Body.Bytes(), &response))
	require.Equal(t, agentic.ResponseTypeStatus, response.Type, "%s", response.Content)
	require.NotEmpty(t, response.InReplyTo)
	return response
}

func awaitSequentialChatResponse(
	t *testing.T, ctx context.Context, consumer jetstream.Consumer, decoder *message.Decoder, loopID string,
) agentic.UserResponse {
	t.Helper()
	for {
		msg, err := consumer.Next(jetstream.FetchMaxWait(10 * time.Second))
		require.NoError(t, err, "terminal user response for %s", loopID)
		decoded, err := decoder.Decode(msg.Data())
		require.NoError(t, err)
		response, ok := decoded.Payload().(*agentic.UserResponse)
		require.Truef(t, ok, "user response payload is %T", decoded.Payload())
		require.NoError(t, msg.DoubleAck(ctx))
		if response.InReplyTo != loopID || response.Type == agentic.ResponseTypeStatus {
			continue
		}
		require.Equal(t, agentic.ResponseTypeResult, response.Type, "%s", response.Content)
		return *response
	}
}

func awaitSequentialChatSettled(
	t *testing.T, ctx context.Context, client *natsclient.Client, stream jetstream.Stream, loopID string,
) {
	t.Helper()
	bucket, err := client.GetKeyValueBucket(ctx, "AGENT_LOOPS")
	require.NoError(t, err)
	// Poll authoritative state: observing a user response alone does not prove
	// the loop's final applied marker or its source acknowledgements committed.
	require.EventuallyWithT(t, func(collect *assert.CollectT) {
		entry, err := bucket.Get(ctx, loopID)
		require.NoError(collect, err)
		var state agentic.LoopEntity
		require.NoError(collect, json.Unmarshal(entry.Value(), &state))
		require.Equal(collect, agentic.LoopStateComplete, state.State)
		require.Equal(collect, 3, state.MaxIterations)
		lister := stream.ListConsumers(ctx)
		count := 0
		for info := range lister.Info() {
			if strings.HasSuffix(info.Name, "-sequential-chat") {
				count++
				require.Zero(collect, info.NumAckPending, "consumer %s", info.Name)
				require.Zero(collect, info.NumPending, "consumer %s", info.Name)
			}
		}
		require.NoError(collect, lister.Err())
		require.GreaterOrEqual(collect, count, 4, "observe started task, model, response, and terminal owners")
	}, 10*time.Second, 25*time.Millisecond)
}

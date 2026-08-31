// Package mock provides test doubles for external services.
package mock

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
)

// ChatCompletionRequest matches OpenAI API request format.
type ChatCompletionRequest struct {
	Model       string        `json:"model"`
	Messages    []ChatMessage `json:"messages"`
	Tools       []Tool        `json:"tools,omitempty"`
	MaxTokens   int           `json:"max_tokens,omitempty"`
	Temperature float32       `json:"temperature,omitempty"`
	Stream      bool          `json:"stream,omitempty"`
}

// ChatMessage matches OpenAI API message format.
type ChatMessage struct {
	Role       string     `json:"role"`
	Content    string     `json:"content,omitempty"`
	ToolCalls  []ToolCall `json:"tool_calls,omitempty"`
	ToolCallID string     `json:"tool_call_id,omitempty"`
}

// Tool matches OpenAI API tool format.
type Tool struct {
	Type     string      `json:"type"`
	Function FunctionDef `json:"function"`
}

// FunctionDef matches OpenAI API function definition.
type FunctionDef struct {
	Name        string `json:"name"`
	Description string `json:"description"`
	Parameters  any    `json:"parameters"`
}

// ToolCall matches OpenAI API tool call format.
type ToolCall struct {
	ID       string       `json:"id"`
	Type     string       `json:"type"`
	Function FunctionCall `json:"function"`
}

// FunctionCall matches OpenAI API function call format.
type FunctionCall struct {
	Name      string `json:"name"`
	Arguments string `json:"arguments"`
}

// ChatCompletionResponse matches OpenAI API response format.
type ChatCompletionResponse struct {
	ID      string   `json:"id"`
	Object  string   `json:"object"`
	Created int64    `json:"created"`
	Model   string   `json:"model"`
	Choices []Choice `json:"choices"`
	Usage   Usage    `json:"usage"`
}

// Choice matches OpenAI API choice format.
type Choice struct {
	Index        int         `json:"index"`
	Message      ChatMessage `json:"message"`
	FinishReason string      `json:"finish_reason"`
}

// Usage matches OpenAI API usage format.
type Usage struct {
	PromptTokens     int `json:"prompt_tokens"`
	CompletionTokens int `json:"completion_tokens"`
	TotalTokens      int `json:"total_tokens"`
}

// streamChunkResponse matches OpenAI streaming chunk format.
type streamChunkResponse struct {
	ID      string              `json:"id"`
	Object  string              `json:"object"`
	Created int64               `json:"created"`
	Model   string              `json:"model"`
	Choices []streamChunkChoice `json:"choices"`
	Usage   *Usage              `json:"usage,omitempty"`
}

// streamChunkChoice matches OpenAI streaming choice format.
type streamChunkChoice struct {
	Index        int              `json:"index"`
	Delta        streamChunkDelta `json:"delta"`
	FinishReason *string          `json:"finish_reason"`
}

// streamChunkDelta matches OpenAI streaming delta format.
type streamChunkDelta struct {
	Role      string           `json:"role,omitempty"`
	Content   string           `json:"content,omitempty"`
	ToolCalls []streamToolCall `json:"tool_calls,omitempty"`
}

// streamToolCall matches OpenAI streaming tool call delta format.
type streamToolCall struct {
	Index    int          `json:"index"`
	ID       string       `json:"id,omitempty"`
	Type     string       `json:"type,omitempty"`
	Function FunctionCall `json:"function"`
}

// RoleResponse pairs a prompt-content marker with the completion body the
// mock should return when that marker is present. Matching runs against
// concatenated system and user message content in the incoming request, in
// declaration order — first marker to match wins. Use this to give different
// agent roles (researcher, synthesizer, etc.) distinct deterministic outputs
// without coupling the mock to the full prompt.
type RoleResponse struct {
	// Marker is a substring searched for in system+user message content.
	// Match is case-sensitive; keep markers specific enough to avoid overlap
	// between roles.
	Marker string
	// Content is the completion body returned when Marker matches.
	Content string
	// ObserveEntityIDSuffix makes this response OBSERVE an entity ID in the
	// request instead of predicting one. When non-empty, the mock scans the
	// matched request's system+user content for the first token ending in
	// "." + ObserveEntityIDSuffix and substitutes it for every
	// ObservedEntityIDPlaceholder occurrence in Content.
	//
	// No fixture outside a running deployment can spell an entity ID's first
	// two positions: ADR-104 mints an entropy suffix onto platform.id at first
	// boot, so org.platform is knowable only from the running stack. A scripted
	// response that quotes a whole entity ID is predicting a value it does not
	// hold, and the components that validate quote-back silently degrade rather
	// than fail when it is wrong.
	//
	// When the request carries no such token the placeholder is left verbatim
	// and the miss is logged — the mock never invents an ID, so the scenario
	// fails on the artifact that depended on it.
	ObserveEntityIDSuffix string
}

// ObservedEntityIDPlaceholder is the token a RoleResponse.Content puts where an
// entity ID belongs when RoleResponse.ObserveEntityIDSuffix is set.
const ObservedEntityIDPlaceholder = "{{observed_entity_id}}"

// RoleToolCall pairs a prompt-content marker with a tool call the mock
// should emit instead of completion content. Matching runs against
// concatenated system and user message content in the incoming request.
// Use this to force a coordinator-style structured output (e.g. a specific
// decide() call) when the scenario needs determinism at the tool-call
// layer, not just completion-text layer.
//
// Sequence semantics: WithRoleToolCallSequence advances a cursor each time
// a marker match fires, so callers can script "first coordinator call →
// fan_out; second coordinator call → synthesize" as a single slice. When
// the cursor exceeds the slice length, subsequent matches return the last
// entry (sticky behaviour, same as WithResponseSequence).
type RoleToolCall struct {
	// Marker is a substring searched for in system+user message content.
	Marker string
	// ToolName is the function name to call.
	ToolName string
	// Args is serialised to JSON and placed on ToolCall.Function.Arguments.
	// Must be non-nil for JSON marshal to produce "{}" at minimum.
	Args map[string]any
}

// OpenAIServer is a mock OpenAI-compatible server for testing.
type OpenAIServer struct {
	srv      *http.Server
	listener net.Listener
	addr     string
	mu       sync.RWMutex

	// Configurable behavior
	toolArgs          map[string]string // tool name -> default arguments JSON
	completionContent string            // content to return on completion
	requestDelay      time.Duration     // artificial delay per request

	// Response sequencing for multi-turn scenarios
	responseSequence []string // sequence of completion contents
	sequenceIndex    int      // current position in sequence

	// Role-based routing for multi-agent scenarios
	roleResponses []RoleResponse

	// Role-based tool-call routing. When a request's messages match one
	// of these entries and the request advertises the named tool, the
	// mock emits the configured tool call INSTEAD of normal completion
	// or first-tool behaviour. Cursor advances each time a match fires;
	// once exhausted, the last entry sticks.
	roleToolCalls     []RoleToolCall
	roleToolCallIndex int

	// submit_work cadence: after this many completed tool rounds, if the
	// request's tool list contains submit_work the mock emits it as the
	// next tool call. Zero means "never auto-submit" (default).
	submitAfter int

	// Tracking for assertions
	requestCount int
	lastRequest  *ChatCompletionRequest
}

// NewOpenAIServer creates a new mock OpenAI server.
func NewOpenAIServer() *OpenAIServer {
	return &OpenAIServer{
		toolArgs: map[string]string{
			"query_entity": `{"entity_id": "c360.logistics.sensor.environmental.temperature.temp-sensor-001"}`,
		},
		// Return JSON for workflow condition evaluation
		completionContent: `{"valid": true, "summary": "Analysis complete. Temperature sensor reading exceeds threshold. Recommend monitoring."}`,
	}
}

// WithToolArgs configures the arguments returned for a specific tool.
func (s *OpenAIServer) WithToolArgs(toolName, argsJSON string) *OpenAIServer {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.toolArgs[toolName] = argsJSON
	return s
}

// WithCompletionContent configures the content returned on completion.
func (s *OpenAIServer) WithCompletionContent(content string) *OpenAIServer {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.completionContent = content
	return s
}

// WithRequestDelay configures an artificial delay per request.
func (s *OpenAIServer) WithRequestDelay(d time.Duration) *OpenAIServer {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.requestDelay = d
	return s
}

// WithResponseSequence configures a sequence of completion contents.
// Each call to the chat completion endpoint will return the next response
// in the sequence. After the sequence is exhausted, it returns the last response.
func (s *OpenAIServer) WithResponseSequence(responses []string) *OpenAIServer {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.responseSequence = responses
	s.sequenceIndex = 0
	return s
}

// WithRoleResponses configures prompt-content-based routing for the completion
// body. The mock scans the system+user messages in the incoming request for
// each marker in order; the first match wins. When no marker matches (or when
// roleResponses is empty) the server falls back to the response sequence, then
// to the default completion content. The tool-call turn is unaffected.
func (s *OpenAIServer) WithRoleResponses(resps []RoleResponse) *OpenAIServer {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.roleResponses = resps
	return s
}

// WithSubmitAfter configures the mock to emit a submit_work tool call after n
// tool rounds have completed, provided submit_work is advertised in the
// request's tool list. n=0 disables the behaviour (default). n=1 submits after
// the first tool round; use a higher value to exercise multi-tool flows before
// completion. Requires a registered submit_work executor on the agentic-tools
// side to actually terminate the loop — without one the call surfaces as a
// "tool not found" error.
func (s *OpenAIServer) WithSubmitAfter(n int) *OpenAIServer {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.submitAfter = n
	return s
}

// WithRoleToolCallSequence scripts a sequence of tool calls to return when
// specific role markers match incoming requests. Each scenario sees matches
// in declaration order — the cursor advances on each fire, and after the
// sequence is exhausted the final entry sticks. When a request matches an
// entry but does not advertise the named tool (e.g. coordinator prompt
// matches but the request's tools list has no "decide"), the mock falls
// through to its normal completion/tool-call behaviour so the scenario
// author gets a deterministic failure rather than a surprise tool call.
func (s *OpenAIServer) WithRoleToolCallSequence(calls []RoleToolCall) *OpenAIServer {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.roleToolCalls = calls
	s.roleToolCallIndex = 0
	return s
}

// ResetSequence resets the response sequence to the beginning.
func (s *OpenAIServer) ResetSequence() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.sequenceIndex = 0
}

// SequenceIndex returns the current position in the response sequence.
func (s *OpenAIServer) SequenceIndex() int {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.sequenceIndex
}

// Start starts the mock server on the given address.
// If addr is empty or ":0", a random available port is used.
func (s *OpenAIServer) Start(addr string) error {
	if addr == "" {
		addr = ":0"
	}

	listener, err := net.Listen("tcp", addr)
	if err != nil {
		return fmt.Errorf("failed to start listener: %w", err)
	}

	s.listener = listener
	s.addr = listener.Addr().String()

	mux := http.NewServeMux()
	mux.HandleFunc("/v1/chat/completions", s.handleChatCompletion)
	mux.HandleFunc("/health", s.handleHealth)

	s.srv = &http.Server{
		Handler:      mux,
		ReadTimeout:  30 * time.Second,
		WriteTimeout: 30 * time.Second,
	}

	go func() {
		// Serve returns when the server is stopped; ErrServerClosed is expected during graceful shutdown
		_ = s.srv.Serve(listener)
	}()

	return nil
}

// Stop stops the mock server gracefully.
func (s *OpenAIServer) Stop() error {
	if s.srv == nil {
		return nil
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	return s.srv.Shutdown(ctx)
}

// Addr returns the address the server is listening on.
func (s *OpenAIServer) Addr() string {
	return s.addr
}

// URL returns the base URL for the server.
func (s *OpenAIServer) URL() string {
	return "http://" + s.addr
}

// RequestCount returns the number of requests received.
func (s *OpenAIServer) RequestCount() int {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.requestCount
}

// LastRequest returns the last request received (for assertions).
func (s *OpenAIServer) LastRequest() *ChatCompletionRequest {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.lastRequest
}

func (s *OpenAIServer) handleHealth(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(map[string]string{"status": "ok"})
}

func (s *OpenAIServer) handleChatCompletion(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	var req ChatCompletionRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "invalid request body: "+err.Error(), http.StatusBadRequest)
		return
	}

	// Track request
	s.mu.Lock()
	s.requestCount++
	s.lastRequest = &req
	delay := s.requestDelay
	s.mu.Unlock()

	// Apply artificial delay if configured
	if delay > 0 {
		time.Sleep(delay)
	}

	// Determine response based on conversation state. Priority:
	//   1. If submit_work is configured and the tool-round count meets the
	//      threshold, emit a submit_work call (if advertised by the request).
	//   2. Otherwise, tool-call turn vs completion turn follows the
	//      hasToolResults/len(Tools) heuristic.
	resp := s.selectResponse(req)

	if req.Stream {
		s.writeStreamingResponse(w, resp)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(resp)
}

// writeStreamingResponse converts a ChatCompletionResponse into SSE chunks.
func (s *OpenAIServer) writeStreamingResponse(w http.ResponseWriter, resp ChatCompletionResponse) {
	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "streaming not supported", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")

	id := resp.ID
	created := resp.Created
	model := resp.Model

	if len(resp.Choices) == 0 {
		return
	}

	choice := resp.Choices[0]

	if len(choice.Message.ToolCalls) > 0 {
		s.writeStreamingToolCalls(w, flusher, id, created, model, choice)
	} else {
		s.writeStreamingContent(w, flusher, id, created, model, choice)
	}

	// Final chunk: usage with empty choices
	usageChunk := streamChunkResponse{
		ID:      id,
		Object:  "chat.completion.chunk",
		Created: created,
		Model:   model,
		Choices: []streamChunkChoice{},
		Usage:   &resp.Usage,
	}
	s.writeSSEChunk(w, flusher, usageChunk)

	// Done sentinel
	fmt.Fprintf(w, "data: [DONE]\n\n")
	flusher.Flush()
}

// writeStreamingContent splits text content into two chunks.
func (s *OpenAIServer) writeStreamingContent(w http.ResponseWriter, flusher http.Flusher, id string, created int64, model string, choice Choice) {
	content := choice.Message.Content
	mid := len(content) / 2

	finishReason := choice.FinishReason

	// Chunk 1: role + first half of content
	s.writeSSEChunk(w, flusher, streamChunkResponse{
		ID: id, Object: "chat.completion.chunk", Created: created, Model: model,
		Choices: []streamChunkChoice{{
			Index: 0,
			Delta: streamChunkDelta{Role: "assistant", Content: content[:mid]},
		}},
	})

	// Chunk 2: second half + finish_reason
	s.writeSSEChunk(w, flusher, streamChunkResponse{
		ID: id, Object: "chat.completion.chunk", Created: created, Model: model,
		Choices: []streamChunkChoice{{
			Index:        0,
			Delta:        streamChunkDelta{Content: content[mid:]},
			FinishReason: &finishReason,
		}},
	})
}

// writeStreamingToolCalls splits tool calls into delta chunks.
func (s *OpenAIServer) writeStreamingToolCalls(w http.ResponseWriter, flusher http.Flusher, id string, created int64, model string, choice Choice) {
	finishReason := choice.FinishReason

	for i, tc := range choice.Message.ToolCalls {
		args := tc.Function.Arguments
		argMid := len(args) / 2

		// Chunk: tool call start (id, name, first half of args)
		s.writeSSEChunk(w, flusher, streamChunkResponse{
			ID: id, Object: "chat.completion.chunk", Created: created, Model: model,
			Choices: []streamChunkChoice{{
				Index: 0,
				Delta: streamChunkDelta{
					Role: "assistant",
					ToolCalls: []streamToolCall{{
						Index:    i,
						ID:       tc.ID,
						Type:     "function",
						Function: FunctionCall{Name: tc.Function.Name, Arguments: args[:argMid]},
					}},
				},
			}},
		})

		// Chunk: remaining args
		s.writeSSEChunk(w, flusher, streamChunkResponse{
			ID: id, Object: "chat.completion.chunk", Created: created, Model: model,
			Choices: []streamChunkChoice{{
				Index: 0,
				Delta: streamChunkDelta{
					ToolCalls: []streamToolCall{{
						Index:    i,
						Function: FunctionCall{Arguments: args[argMid:]},
					}},
				},
			}},
		})
	}

	// Finish reason chunk
	s.writeSSEChunk(w, flusher, streamChunkResponse{
		ID: id, Object: "chat.completion.chunk", Created: created, Model: model,
		Choices: []streamChunkChoice{{
			Index:        0,
			Delta:        streamChunkDelta{},
			FinishReason: &finishReason,
		}},
	})
}

func (s *OpenAIServer) writeSSEChunk(w http.ResponseWriter, flusher http.Flusher, chunk streamChunkResponse) {
	data, _ := json.Marshal(chunk)
	fmt.Fprintf(w, "data: %s\n\n", data)
	flusher.Flush()
}

// selectResponse decides which response shape to return for the given request.
// Priority:
//  1. Role tool-call match (scripted determinism for coordinator-style flows).
//     Consumes from the configured sequence and overrides normal behaviour
//     when a marker matches AND the request advertises the named tool.
//  2. submit_work cadence — emits submit_work after N tool rounds.
//  3. Tool-call heuristic — first tool if tools present and no prior tool
//     results, completion otherwise.
func (s *OpenAIServer) selectResponse(req ChatCompletionRequest) ChatCompletionResponse {
	if resp, ok := s.tryRoleToolCall(req); ok {
		return resp
	}

	s.mu.RLock()
	submitAfter := s.submitAfter
	s.mu.RUnlock()

	toolRounds := countToolResults(req.Messages)

	if submitAfter > 0 && toolRounds >= submitAfter {
		if submit := findToolByName(req.Tools, "submit_work"); submit != nil {
			return s.buildToolCallResponse(*submit, req.Model)
		}
	}

	if toolRounds > 0 {
		return s.buildCompletionResponse(req)
	}
	if len(req.Tools) > 0 {
		return s.buildToolCallResponse(req.Tools[0], req.Model)
	}
	return s.buildCompletionResponse(req)
}

// tryRoleToolCall checks whether a configured RoleToolCall entry matches
// the request's messages. On match it builds a ToolCall response with the
// entry's Args and advances the cursor. Returns (resp, true) on match,
// (_, false) otherwise — the caller proceeds with normal routing.
//
// Matching only fires when (a) a marker substring appears in system/user
// content and (b) the request advertises the named tool. Mismatch on (b)
// skips the entry without advancing the cursor, so a scenario that
// accidentally mis-scopes a tool name gets deterministic fall-through
// failure rather than silent wrong-tool injection.
func (s *OpenAIServer) tryRoleToolCall(req ChatCompletionRequest) (ChatCompletionResponse, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if len(s.roleToolCalls) == 0 {
		return ChatCompletionResponse{}, false
	}

	idx := s.roleToolCallIndex
	if idx >= len(s.roleToolCalls) {
		idx = len(s.roleToolCalls) - 1 // sticky after exhaustion
	}
	entry := s.roleToolCalls[idx]
	if entry.Marker == "" {
		return ChatCompletionResponse{}, false
	}

	matched := false
	for _, msg := range req.Messages {
		if msg.Role != "system" && msg.Role != "user" {
			continue
		}
		if strings.Contains(msg.Content, entry.Marker) {
			matched = true
			break
		}
	}
	if !matched {
		return ChatCompletionResponse{}, false
	}

	if findToolByName(req.Tools, entry.ToolName) == nil {
		// Tool not in the request's advertised list; don't inject it.
		return ChatCompletionResponse{}, false
	}

	argsJSON, err := json.Marshal(entry.Args)
	if err != nil {
		argsJSON = []byte("{}")
	}
	// Advance only when we're actually about to return this entry —
	// otherwise repeated failed matches would burn the cursor.
	if s.roleToolCallIndex < len(s.roleToolCalls) {
		s.roleToolCallIndex++
	}

	callID := "call_" + uuid.New().String()[:8]
	return ChatCompletionResponse{
		ID:      "chatcmpl-mock-" + uuid.New().String()[:8],
		Object:  "chat.completion",
		Created: time.Now().Unix(),
		Model:   req.Model,
		Choices: []Choice{{
			Index: 0,
			Message: ChatMessage{
				Role: "assistant",
				ToolCalls: []ToolCall{{
					ID:   callID,
					Type: "function",
					Function: FunctionCall{
						Name:      entry.ToolName,
						Arguments: string(argsJSON),
					},
				}},
			},
			FinishReason: "tool_calls",
		}},
		Usage: Usage{PromptTokens: 120, CompletionTokens: 40, TotalTokens: 160},
	}, true
}

// countToolResults returns the number of tool-result messages in the history,
// which equals the number of completed tool rounds.
func countToolResults(messages []ChatMessage) int {
	n := 0
	for _, msg := range messages {
		if msg.Role == "tool" {
			n++
		}
	}
	return n
}

// findToolByName returns a pointer to the matching Tool in the request, or
// nil if absent.
func findToolByName(tools []Tool, name string) *Tool {
	for i := range tools {
		if tools[i].Function.Name == name {
			return &tools[i]
		}
	}
	return nil
}

func (s *OpenAIServer) buildToolCallResponse(tool Tool, model string) ChatCompletionResponse {
	s.mu.RLock()
	args, ok := s.toolArgs[tool.Function.Name]
	if !ok {
		args = "{}"
	}
	s.mu.RUnlock()

	callID := "call_" + uuid.New().String()[:8]

	return ChatCompletionResponse{
		ID:      "chatcmpl-mock-" + uuid.New().String()[:8],
		Object:  "chat.completion",
		Created: time.Now().Unix(),
		Model:   model,
		Choices: []Choice{{
			Index: 0,
			Message: ChatMessage{
				Role: "assistant",
				ToolCalls: []ToolCall{{
					ID:   callID,
					Type: "function",
					Function: FunctionCall{
						Name:      tool.Function.Name,
						Arguments: args,
					},
				}},
			},
			FinishReason: "tool_calls",
		}},
		Usage: Usage{
			PromptTokens:     100,
			CompletionTokens: 50,
			TotalTokens:      150,
		},
	}
}

// buildCompletionResponse returns a completion-turn response. Priority for
// the content body:
//
//  1. First RoleResponse whose Marker is a substring of any system/user
//     message in the request.
//  2. Next entry in the configured response sequence (advances the cursor).
//  3. The default completionContent.
//
// Taking the full request lets us inspect conversation content for role
// routing without the caller pre-extracting a marker.
func (s *OpenAIServer) buildCompletionResponse(req ChatCompletionRequest) ChatCompletionResponse {
	s.mu.Lock()
	content := s.completionContent
	if matched, ok := firstRoleMatch(s.roleResponses, req.Messages); ok {
		content = matched
	} else if len(s.responseSequence) > 0 {
		if s.sequenceIndex < len(s.responseSequence) {
			content = s.responseSequence[s.sequenceIndex]
			s.sequenceIndex++
		} else {
			content = s.responseSequence[len(s.responseSequence)-1]
		}
	}
	s.mu.Unlock()

	return ChatCompletionResponse{
		ID:      "chatcmpl-mock-" + uuid.New().String()[:8],
		Object:  "chat.completion",
		Created: time.Now().Unix(),
		Model:   req.Model,
		Choices: []Choice{{
			Index: 0,
			Message: ChatMessage{
				Role:    "assistant",
				Content: content,
			},
			FinishReason: "stop",
		}},
		Usage: Usage{
			PromptTokens:     150,
			CompletionTokens: 75,
			TotalTokens:      225,
		},
	}
}

// firstRoleMatch scans system+user message content for each role-response
// marker in declaration order and returns the matching content on the first
// hit. Markers are case-sensitive substring matches.
func firstRoleMatch(resps []RoleResponse, messages []ChatMessage) (string, bool) {
	if len(resps) == 0 {
		return "", false
	}
	for _, r := range resps {
		if r.Marker == "" {
			continue
		}
		for _, msg := range messages {
			if msg.Role != "system" && msg.Role != "user" {
				continue
			}
			if strings.Contains(msg.Content, r.Marker) {
				return resolveObservedEntityID(r, messages), true
			}
		}
	}
	return "", false
}

// resolveObservedEntityID substitutes the entity ID the request actually
// carries for the placeholder in a matched response body. See
// RoleResponse.ObserveEntityIDSuffix for why a fixture must not spell one.
func resolveObservedEntityID(r RoleResponse, messages []ChatMessage) string {
	if r.ObserveEntityIDSuffix == "" || !strings.Contains(r.Content, ObservedEntityIDPlaceholder) {
		return r.Content
	}
	observed := findEntityIDBySuffix(messages, r.ObserveEntityIDSuffix)
	if observed == "" {
		log.Printf("mock openai: no entity ID ending in %q appears in the request; leaving %s unresolved",
			"."+r.ObserveEntityIDSuffix, ObservedEntityIDPlaceholder)
		return r.Content
	}
	return strings.ReplaceAll(r.Content, ObservedEntityIDPlaceholder, observed)
}

// findEntityIDBySuffix returns the first whitespace-delimited token in the
// system+user content that ends in "."+suffix, with surrounding punctuation
// trimmed. Empty when the request carries none.
func findEntityIDBySuffix(messages []ChatMessage, suffix string) string {
	want := "." + suffix
	for _, msg := range messages {
		if msg.Role != "system" && msg.Role != "user" {
			continue
		}
		for _, field := range strings.Fields(msg.Content) {
			token := strings.Trim(field, `"',;:()[]{}`)
			if strings.HasSuffix(token, want) && len(token) > len(want) {
				return token
			}
		}
	}
	return ""
}

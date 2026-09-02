package agentic

import (
	"encoding/json"
	"fmt"
	"maps"
	"slices"
	"testing"
	"time"

	"github.com/c360studio/semstreams/message"
	"github.com/c360studio/semstreams/payloadregistry"
)

// TestEveryRegisteredAgenticPayloadIsRuleReadable enumerates the payloads from
// their OWNING registration (RegisterPayloads), not from a hand-written list,
// so a payload added to the registry without a projection fails here instead of
// being silently unreadable to every rule.
func TestEveryRegisteredAgenticPayloadIsRuleReadable(t *testing.T) {
	reg := payloadregistry.New()
	if err := RegisterPayloads(reg); err != nil {
		t.Fatalf("RegisterPayloads: %v", err)
	}

	listed := reg.ListByDomain(Domain)
	// 15 event payloads (#1052) + the 5 entity types born on the mutation
	// lane that ADR-103 registers as Graphable payloads.
	if len(listed) != 20 {
		t.Fatalf("agentic domain registers %d payloads, test expects the 20 covered by the projection table", len(listed))
	}

	for _, registration := range listed {
		msgType := registration.MessageType()
		// Create(), not registration.Factory — ListByDomain deliberately does
		// not copy the factory out of the registry.
		payload := reg.Create(registration.Domain, registration.Category, registration.Version)
		readable, ok := payload.(message.RuleReadable)
		if !ok {
			t.Errorf("%s (%T) does not implement message.RuleReadable", msgType, payload)
			continue
		}
		// A zero-valued payload must still answer without panicking; rule
		// evaluation runs against whatever decoded off the wire.
		if fields := readable.RuleFields(); fields == nil {
			t.Errorf("%s (%T) RuleFields() returned nil for a zero payload", msgType, payload)
		}
	}
}

// TestAgenticRuleFieldsWithholdContent pins the structural-vs-content decision
// per payload. A field listed here that starts appearing in a projection is a
// content leak into the rule lane, not a widening.
func TestAgenticRuleFieldsWithholdContent(t *testing.T) {
	for _, tc := range projectionCases() {
		t.Run(tc.name, func(t *testing.T) {
			fields := tc.payload.RuleFields()
			for key, want := range tc.expose {
				got, present := fields[key]
				if !present {
					t.Errorf("structural field %q missing from projection: %v", key, fields)
					continue
				}
				if got != want {
					t.Errorf("field %q = %v (%T), want %v (%T)", key, got, got, want, want)
				}
			}
			for _, key := range tc.withhold {
				if _, present := fields[key]; present {
					t.Errorf("content field %q leaked into the rule projection: %v", key, fields[key])
				}
			}
		})
	}
}

// projectionCase is one payload, fully populated, with the structural fields a
// rule may match on and the content fields it may not reach.
type projectionCase struct {
	name     string
	payload  message.RuleReadable
	expose   map[string]any
	withhold []string
}

func projectionCases() []projectionCase {
	now := time.Now().UTC()
	maxIter := 7

	cases := []projectionCase{
		{
			name: "LoopCreatedEvent",
			payload: &LoopCreatedEvent{
				LoopID: "l1", TaskID: "t1", Role: "architect", Model: "m",
				MaxIterations: 5, CreatedAt: now,
				Metadata: map[string]any{"secret": "user text"},
			},
			expose:   map[string]any{"loop_id": "l1", "role": "architect", "max_iterations": 5},
			withhold: []string{"metadata"},
		},
		{
			name: "LoopCompletedEvent",
			payload: &LoopCompletedEvent{
				LoopID: "l1", TaskID: "t1", Outcome: OutcomeSuccess, Role: "architect",
				Result: "model output", Prompt: "user text", Model: "m", Iterations: 3,
				TokensIn: 10, TokensOut: 20, CompletedAt: now,
				Metadata: map[string]any{"secret": "x"},
			},
			expose: map[string]any{
				"role": "architect", "outcome": OutcomeSuccess,
				"iterations": 3, "tokens_in": 10, "tokens_out": 20,
			},
			withhold: []string{"result", "prompt", "metadata"},
		},
		{
			name: "LoopFailedEvent",
			payload: &LoopFailedEvent{
				LoopID: "l1", TaskID: "t1", Outcome: OutcomeFailed, Reason: "model_error",
				Error: "provider said: <echoed prompt>", Role: "editor", Prompt: "user text",
				Model: "m", FailedAt: now, Metadata: map[string]any{"secret": "x"},
			},
			expose: map[string]any{"outcome": OutcomeFailed, "role": "editor"},
			// `reason` is WITHHELD, not missing: nothing in the contract
			// constrains it (LoopFailedEvent.Validate checks LoopID/TaskID
			// only) and the exported BuildFailureEvent/BuildFailureMessages
			// take it as a free string, so an adopter can legally put prose
			// there. Restoring it needs a validated enum, not an enumeration
			// of today's callers.
			withhold: []string{"reason", "error", "prompt", "metadata"},
		},
		{
			name: "LoopCancelledEvent",
			payload: &LoopCancelledEvent{
				LoopID: "l1", TaskID: "t1", Outcome: OutcomeCancelled, CancelledBy: "operator",
				CancelledAt: now, Metadata: map[string]any{"secret": "x"},
			},
			expose:   map[string]any{"cancelled_by": "operator", "outcome": OutcomeCancelled},
			withhold: []string{"metadata"},
		},
		{
			name: "ContextEvent",
			payload: &ContextEvent{
				Type: "compaction_complete", LoopID: "l1", Iteration: 4,
				Utilization: 0.9, TokensSaved: 900, Summary: "LLM-authored summary",
			},
			expose:   map[string]any{"type": "compaction_complete", "iteration": 4, "tokens_saved": 900},
			withhold: []string{"summary"},
		},
		{
			name: "TaskMessage",
			payload: &TaskMessage{
				TaskID: "t1", Role: "architect", Model: "m", Prompt: "user task text",
				MaxIterations: &maxIter, Metadata: map[string]any{"secret": "x"},
				Tools: []ToolDefinition{{Name: "bash"}},
			},
			expose:   map[string]any{"task_id": "t1", "role": "architect", "max_iterations": 7},
			withhold: []string{"prompt", "metadata", "tools", "tool_choice", "response_format", "context"},
		},
		{
			name: "UserSignal",
			payload: &UserSignal{
				SignalID: "s1", Type: SignalCancel, LoopID: "l1", UserID: "u1",
				ChannelType: "cli", ChannelID: "c1", Payload: "rejection prose",
				Timestamp: now,
			},
			expose:   map[string]any{"type": SignalCancel, "loop_id": "l1"},
			withhold: []string{"payload"},
		},
		{
			name: "ApprovalPendingEvent",
			payload: &ApprovalPendingEvent{
				LoopID: "l1", CallID: "c1", ToolName: "bash",
				Arguments:   map[string]any{"command": "rm -rf /"},
				Reason:      ApprovalRequiredPrefix + "bash needs approval",
				RequestedAt: now, Timeout: time.Minute,
			},
			expose:   map[string]any{"tool_name": "bash", "call_id": "c1"},
			withhold: []string{"arguments", "reason"},
		},
		{
			name: "ApprovalResponse",
			payload: &ApprovalResponse{
				LoopID: "l1", CallID: "c1", Decision: ApprovalDecisionApprove,
				ApprovedBy: "alice", Reason: "looks fine to me",
				ModifiedArguments: map[string]any{"command": "ls"}, DecidedAt: now,
			},
			expose:   map[string]any{"decision": ApprovalDecisionApprove, "approved_by": "alice"},
			withhold: []string{"reason", "modified_arguments"},
		},
		{
			name: "ToolCall",
			payload: &ToolCall{
				ID: "call-1", Name: "bash", LoopID: "l1", TraceID: "tr1",
				Arguments: map[string]any{"command": "rm -rf /"},
				Metadata:  map[string]any{"secret": "x"},
			},
			expose:   map[string]any{"id": "call-1", "name": "bash", "loop_id": "l1"},
			withhold: []string{"arguments", "metadata"},
		},
		{
			name: "ToolResult",
			payload: &ToolResult{
				CallID: "call-1", Name: "bash", Content: "the whole result body",
				Error: "boom", ErrorKind: ToolErrorTimeout, ResultHint: HintTooLarge,
				Metadata: map[string]any{"has_more": true}, LoopID: "l1", StopLoop: true,
			},
			expose: map[string]any{
				"call_id": "call-1", "name": "bash",
				"error_kind": string(ToolErrorTimeout), "result_hint": string(HintTooLarge),
				"stop_loop": true,
			},
			withhold: []string{"content", "error", "metadata"},
		},
		{
			name: "UserMessage",
			payload: &UserMessage{
				MessageID: "m1", ChannelType: "slack", ChannelID: "c1", UserID: "u1",
				Content:     "what the human typed",
				Attachments: []Attachment{{Name: "secret.txt", Content: "body"}},
				Metadata:    map[string]string{"secret": "x"}, Timestamp: now,
			},
			expose:   map[string]any{"message_id": "m1", "channel_type": "slack"},
			withhold: []string{"content", "attachments", "metadata"},
		},
		{
			name: "UserResponse",
			payload: &UserResponse{
				ResponseID: "r1", ChannelType: "slack", ChannelID: "c1", UserID: "u1",
				Type: ResponseTypeResult, Content: "the response text",
				Blocks:  []ResponseBlock{{Type: "code", Content: "body"}},
				Actions: []ResponseAction{{ID: "a1", Label: "Approve this"}},
			},
			expose:   map[string]any{"type": ResponseTypeResult, "response_id": "r1"},
			withhold: []string{"content", "blocks", "actions"},
		},
		{
			name: "AgentRequest",
			payload: &AgentRequest{
				RequestID: "req1", LoopID: "l1", Role: "architect", Model: "m",
				Messages:  []ChatMessage{{Role: "user", Content: "the prompt"}},
				MaxTokens: 100, Temperature: 0.2,
				Tools: []ToolDefinition{{Name: "bash"}}, ToolChoice: &ToolChoice{Mode: "auto"},
			},
			expose:   map[string]any{"request_id": "req1", "role": "architect", "max_tokens": 100},
			withhold: []string{"messages", "tools", "tool_choice", "response_format"},
		},
		{
			name: "AgentResponse",
			payload: &AgentResponse{
				RequestID: "req1", Status: StatusComplete, FinishReason: "stop",
				Message:    ChatMessage{Role: "assistant", Content: "model output"},
				Error:      "provider prose",
				TokenUsage: TokenUsage{PromptTokens: 11, CompletionTokens: 22},
			},
			expose: map[string]any{"status": StatusComplete, "request_id": "req1"},
			// `finish_reason` is WITHHELD: it carries the provider's raw
			// value, and the two in-repo client lanes already write different
			// vocabularies into it (chat-completions writes stop/length/
			// tool_calls, responses writes completed/incomplete), so a rule
			// matching on it breaks on a config-only endpoint switch. Both
			// lanes feed the same switch that produces `status`, which IS
			// validated against a closed set, so nothing is lost.
			withhold: []string{"finish_reason", "message", "error"},
		},
		// --- Entity types born on the mutation lane (ADR-103) ---
		{
			name: "LoopExecutionEntity",
			payload: &LoopExecutionEntity{
				Org: "acme", Platform: "ops", LoopID: "l1",
				Task: &TaskMessage{TaskID: "t1", Role: "architect", Prompt: "user text"},
			},
			expose:   map[string]any{"org": "acme", "platform": "ops", "loop_id": "l1"},
			withhold: []string{"task"},
		},
		{
			name: "AgentLessonEntity",
			payload: &AgentLessonEntity{
				Org: "acme", Platform: "ops", ID: "lesson-1",
				Category: "retention-policy", Polarity: "avoid", Severity: "warning", Status: "proposed",
				CreatedAt: now, Summary: "authored summary", Detail: "authored detail", InjectionForm: "authored form",
				Evidence: []string{"acme.ops.agentic-loop.agent.execution.l1"}, AppliesTo: []string{"tag:go"},
				ObservedRole: "ops", ExecutedBy: "acme.ops.agentic-loop.agent.execution.l0",
			},
			expose: map[string]any{
				"category": "retention-policy", "polarity": "avoid", "severity": "warning", "status": "proposed",
				"observed_role": "ops", "executed_by": "acme.ops.agentic-loop.agent.execution.l0",
			},
			withhold: []string{"summary", "detail", "injection_form"},
		},
		{
			name: "OpsDiagnosisEntity",
			payload: &OpsDiagnosisEntity{
				Org: "acme", Platform: "ops", ID: "finding-1",
				Finding: "authored finding", Recommendation: "authored recommendation", Confidence: 0.85,
				Evidence: []string{"acme.ops.agentic-loop.agent.execution.l1"}, ObservedRole: "ops", Severity: "warn",
				ExecutedBy: "acme.ops.agentic-loop.agent.execution.l0",
			},
			expose:   map[string]any{"confidence": 0.85, "severity": "warn", "observed_role": "ops"},
			withhold: []string{"finding", "recommendation"},
		},
		{
			name: "ModelEndpointEntity",
			payload: &ModelEndpointEntity{
				Org: "acme", Platform: "ops", Name: "gpt4o", Provider: "openai", Model: "gpt-4o",
				URL: "https://api.openai.com/v1", SupportsTools: true, MaxTokens: 128000,
				InputPricePer1MTokens: 5, OutputPricePer1MTokens: 15, RequestsPerMinute: 60,
			},
			expose: map[string]any{
				"provider": "openai", "model": "gpt-4o", "supports_tools": true, "max_tokens": 128000,
				"requests_per_minute": 60,
			},
			withhold: nil,
		},
		{
			name: "WebObservationEntity",
			payload: &WebObservationEntity{
				Org: "acme", Platform: "ops", CanonicalURL: "https://example.com/x", Tool: WebObservationToolHTTPRequest,
				LoopEntityID: "acme.ops.agentic-loop.agent.execution.l1",
				FetchedAt:    "2026-08-26T12:00:00Z", ContentType: "text/html", StatusCode: 200,
				Text: "page body", Truncated: true, Title: "provider title", Snippet: "provider snippet",
				SourceQuery: "authored query", ObservedAt: "2026-08-26T12:00:01Z",
			},
			expose: map[string]any{
				"tool": "http_request", "status_code": 200, "truncated": true, "content_type": "text/html",
			},
			withhold: []string{"text", "title", "snippet", "source_query"},
		},
	}

	return cases
}

// TestProjectionTableCoversEveryRegisteredPayload keeps the content table
// honest against the registry: a payload added to the agentic domain without a
// row here would otherwise be silently exempt from the content check.
func TestProjectionTableCoversEveryRegisteredPayload(t *testing.T) {
	reg := payloadregistry.New()
	if err := RegisterPayloads(reg); err != nil {
		t.Fatalf("RegisterPayloads: %v", err)
	}

	covered := make(map[string]bool)
	for _, tc := range projectionCases() {
		covered[fmt.Sprintf("%T", tc.payload)] = true
	}
	for _, registration := range reg.ListByDomain(Domain) {
		payload := reg.Create(registration.Domain, registration.Category, registration.Version)
		if !covered[fmt.Sprintf("%T", payload)] {
			t.Errorf("%s (%T) has no row in the content-withholding table", registration.MessageType(), payload)
		}
	}
}

// TestToolResultErrorKindResolvesUnknown pins the documented default: a result
// carrying an Error but no ErrorKind still reports the failure structurally.
func TestToolResultErrorKindResolvesUnknown(t *testing.T) {
	t.Run("error without kind resolves to unknown", func(t *testing.T) {
		fields := (&ToolResult{CallID: "c1", Error: "boom"}).RuleFields()
		if got := fields["error_kind"]; got != string(ToolErrorUnknown) {
			t.Errorf("error_kind = %v, want %q", got, ToolErrorUnknown)
		}
	})
	t.Run("success omits error_kind entirely", func(t *testing.T) {
		fields := (&ToolResult{CallID: "c1", Content: "fine"}).RuleFields()
		if _, present := fields["error_kind"]; present {
			t.Errorf("error_kind present on a successful result: %v", fields)
		}
	})
}

// TestRuleFieldsMirrorWireNames guards the convention that makes the surface
// predictable: every key a projection exposes is a JSON field name the payload
// actually marshals, so a rule author reading an `agent.*` event off the wire
// can predict the `$message.*` path without reading Go.
//
// One documented exception: ToolResult resolves `error_kind` to
// ToolErrorUnknown when the result carries an Error but no ErrorKind, so the
// key can be present when the wire omits it. The table populates ErrorKind, so
// the exception does not mask a drift here.
func TestRuleFieldsMirrorWireNames(t *testing.T) {
	for _, tc := range projectionCases() {
		t.Run(tc.name, func(t *testing.T) {
			raw, err := json.Marshal(tc.payload)
			if err != nil {
				t.Fatalf("marshal: %v", err)
			}
			var wire map[string]any
			if err := json.Unmarshal(raw, &wire); err != nil {
				t.Fatalf("unmarshal: %v", err)
			}
			for key := range tc.payload.RuleFields() {
				if _, onWire := wire[key]; !onWire {
					t.Errorf("projection key %q is not a wire field of %T; wire keys: %v",
						key, tc.payload, slices.Sorted(maps.Keys(wire)))
				}
			}
		})
	}
}

// TestZeroPayloadProjectionCoversMandatoryWireFields is the reverse of
// TestRuleFieldsMirrorWireNames: every field the payload ALWAYS puts on the
// wire (no `omitempty`) must either be in the projection or be a declared
// withholding. A field emitted only when non-empty when the wire emits it
// always is a silent convention break — "absent" would then mean two different
// things to a rule author depending on which field they picked.
//
// A ZERO payload is the probe precisely because its marshal output is exactly
// the set of non-omitempty fields.
func TestZeroPayloadProjectionCoversMandatoryWireFields(t *testing.T) {
	// How encoding/json renders a zero time.Time.
	const zeroTimeWire = "0001-01-01T00:00:00Z"

	withheldByType := map[string]map[string]bool{}
	for _, tc := range projectionCases() {
		set := map[string]bool{}
		for _, key := range tc.withhold {
			set[key] = true
		}
		withheldByType[fmt.Sprintf("%T", tc.payload)] = set
	}

	reg := payloadregistry.New()
	if err := RegisterPayloads(reg); err != nil {
		t.Fatalf("RegisterPayloads: %v", err)
	}

	for _, registration := range reg.ListByDomain(Domain) {
		t.Run(registration.Category, func(t *testing.T) {
			payload := reg.Create(registration.Domain, registration.Category, registration.Version)
			readable, ok := payload.(message.RuleReadable)
			if !ok {
				t.Fatalf("%T does not implement message.RuleReadable", payload)
			}
			withheld := withheldByType[fmt.Sprintf("%T", payload)]
			if withheld == nil {
				t.Fatalf("%T has no row in the content-withholding table", payload)
			}

			raw, err := json.Marshal(payload)
			if err != nil {
				t.Fatalf("marshal zero payload: %v", err)
			}
			var mandatory map[string]any
			if err := json.Unmarshal(raw, &mandatory); err != nil {
				t.Fatalf("unmarshal zero payload: %v", err)
			}

			projection := readable.RuleFields()
			for key, wireValue := range mandatory {
				if withheld[key] {
					continue
				}
				// The one declared exception (see putTime): a zero time is
				// omitted from the projection even though the wire renders it,
				// because year 1 reaching a rule condition as if it were an
				// observation is worse than the field being absent. Narrowly
				// scoped to the exact zero-time rendering so every other
				// mandatory field keeps its teeth.
				if wireValue == zeroTimeWire {
					continue
				}
				if _, present := projection[key]; !present {
					t.Errorf("%T always marshals %q but the zero projection omits it; projection keys: %v",
						payload, key, slices.Sorted(maps.Keys(projection)))
				}
			}
		})
	}
}

// TestEffectiveErrorKind pins the three states the ErrorKind default has to
// distinguish. The empty return is the one that matters: reading ErrorKind
// directly to answer "did this call fail" reads empty for an unclassified
// executor error, which is the fail-open shape this method exists to close.
func TestEffectiveErrorKind(t *testing.T) {
	tests := []struct {
		name   string
		result ToolResult
		want   ToolErrorKind
	}{
		{"classified failure keeps its kind", ToolResult{CallID: "c", Error: "boom", ErrorKind: ToolErrorTimeout}, ToolErrorTimeout},
		{"unclassified failure resolves to unknown", ToolResult{CallID: "c", Error: "boom"}, ToolErrorUnknown},
		{"kind without error message still classifies", ToolResult{CallID: "c", ErrorKind: ToolErrorPermission}, ToolErrorPermission},
		{"success is the empty third state", ToolResult{CallID: "c", Content: "fine"}, ""},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := tc.result.EffectiveErrorKind(); got != tc.want {
				t.Errorf("EffectiveErrorKind() = %q, want %q", got, tc.want)
			}
		})
	}
}

package rule

import (
	"bytes"
	"context"
	"encoding/json"
	"log/slog"
	"os"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/c360studio/semstreams/agentic"
	"github.com/c360studio/semstreams/message"
	"github.com/c360studio/semstreams/payloadbuiltins"
	"github.com/c360studio/semstreams/processor/rule/expression"
)

// unreadablePayload stands in for an adopter payload that is correctly
// registered and correctly decoded but declares no rule-readable fields.
// Deliberately NOT a framework type: pinning this test to a framework
// payload would make it fail the day that payload gains RuleFields.
type unreadablePayload struct {
	Role string `json:"role"`
}

func (p *unreadablePayload) Schema() message.Type {
	return message.Type{Domain: "test", Category: "unreadable", Version: "v1"}
}

func (p *unreadablePayload) Validate() error { return nil }

func (p *unreadablePayload) MarshalJSON() ([]byte, error) {
	type alias unreadablePayload
	return json.Marshal((*alias)(p))
}

func (p *unreadablePayload) UnmarshalJSON(data []byte) error {
	type alias unreadablePayload
	return json.Unmarshal(data, (*alias)(p))
}

// declaredSubsetPayload declares only `role`, deliberately omitting the
// `outcome` struct field, so a rule conditioning on `outcome` proves the
// engine does not fall back to the payload's structure.
type declaredSubsetPayload struct {
	Role    string `json:"role"`
	Outcome string `json:"outcome"`
}

func (p *declaredSubsetPayload) Schema() message.Type {
	return message.Type{Domain: "test", Category: "subset", Version: "v1"}
}

func (p *declaredSubsetPayload) Validate() error { return nil }

func (p *declaredSubsetPayload) MarshalJSON() ([]byte, error) {
	type alias declaredSubsetPayload
	return json.Marshal((*alias)(p))
}

func (p *declaredSubsetPayload) UnmarshalJSON(data []byte) error {
	type alias declaredSubsetPayload
	return json.Unmarshal(data, (*alias)(p))
}

func (p *declaredSubsetPayload) RuleFields() map[string]any {
	return map[string]any{"role": p.Role}
}

func newLoopCompletedRule(t *testing.T, id string) *ExpressionRule {
	t.Helper()
	r, err := NewExpressionRule(testPlatform, "test-pack", Definition{
		ID:      id,
		Type:    "expression",
		Name:    id,
		Enabled: true,
		Logic:   "and",
		Conditions: []expression.ConditionExpression{
			{Field: "$message.role", Operator: "eq", Value: "architect"},
			{Field: "$message.outcome", Operator: "eq", Value: "success"},
		},
	})
	if err != nil {
		t.Fatalf("NewExpressionRule: %v", err)
	}
	return r
}

// Scenario: a typed payload declaring rule-readable fields is matchable.
func TestExpressionRuleMatchesRuleReadableTypedPayload(t *testing.T) {
	r := newLoopCompletedRule(t, "typed-payload-matchable")

	event := &agentic.LoopCompletedEvent{
		LoopID:  "loop-1",
		TaskID:  "task-1",
		Outcome: agentic.OutcomeSuccess,
		Role:    "architect",
		Result:  "the architecture specification body",
	}
	msg := message.NewBaseMessage(event.Schema(), event, "test")

	if !r.Evaluate([]message.Message{msg}) {
		t.Fatal("rule did not fire on a RuleReadable typed payload whose declared fields match")
	}

	// The same values must reach `$message.*` substitution in actions.
	fields := extractMessageData(msg)
	if got := fields["role"]; got != "architect" {
		t.Errorf("extractMessageData role = %v, want architect", got)
	}
	if got := fields["outcome"]; got != agentic.OutcomeSuccess {
		t.Errorf("extractMessageData outcome = %v, want %s", got, agentic.OutcomeSuccess)
	}
}

// Scenario: a generic JSON payload keeps working unchanged.
func TestExpressionRuleGenericJSONPayloadUnchanged(t *testing.T) {
	r := newLoopCompletedRule(t, "generic-unchanged")

	payload := message.NewGenericJSON(map[string]any{
		"role":    "architect",
		"outcome": "success",
	})
	msg := message.NewBaseMessage(payload.Schema(), payload, "test")

	if !r.Evaluate([]message.Message{msg}) {
		t.Fatal("rule did not fire on a core.json.v1 payload it matched before this change")
	}

	miss := message.NewGenericJSON(map[string]any{"role": "editor", "outcome": "success"})
	missMsg := message.NewBaseMessage(miss.Schema(), miss, "test")
	if r.Evaluate([]message.Message{missMsg}) {
		t.Fatal("rule fired on a generic payload whose role does not match")
	}
}

// Scenario: an undeclared field is not reachable.
func TestExpressionRuleUndeclaredFieldNotReachable(t *testing.T) {
	r := newLoopCompletedRule(t, "undeclared-unreachable")

	payload := &declaredSubsetPayload{Role: "architect", Outcome: "success"}
	msg := message.NewBaseMessage(payload.Schema(), payload, "test")

	if r.Evaluate([]message.Message{msg}) {
		t.Fatal("rule fired on a struct field the payload did not declare — the engine fell back to structure")
	}

	fields, readable := ruleFields(msg)
	if !readable {
		t.Fatal("payload implementing RuleReadable must be readable")
	}
	if _, present := fields["outcome"]; present {
		t.Errorf("undeclared field reached the projection: %v", fields)
	}
}

// Scenarios: an unreadable payload reports rather than silently failing, and
// repeated unreadable messages do not flood the signal.
// Not t.Parallel(): slog.SetDefault mutates process-global state.
func TestExpressionRuleReportsUnreadablePayloadOncePerPairing(t *testing.T) {
	var buf bytes.Buffer
	prev := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(&buf, &slog.HandlerOptions{Level: slog.LevelDebug})))
	t.Cleanup(func() { slog.SetDefault(prev) })

	r := newLoopCompletedRule(t, "unreadable-observable")
	other := newLoopCompletedRule(t, "unaffected-neighbour")

	payload := &unreadablePayload{Role: "architect"}
	msg := message.NewBaseMessage(payload.Schema(), payload, "test")

	if r.Evaluate([]message.Message{msg}) {
		t.Fatal("rule fired on an unreadable payload")
	}

	out := buf.String()
	if !strings.Contains(out, "test.unreadable.v1") {
		t.Errorf("unreadable report does not identify the payload type; got:\n%s", out)
	}
	if !strings.Contains(out, "unreadable-observable") {
		t.Errorf("unreadable report does not identify the rule; got:\n%s", out)
	}
	if got := strings.Count(out, "test.unreadable.v1"); got != 1 {
		t.Errorf("pairing reported %d times on first evaluation, want 1", got)
	}

	// Other rules evaluating the same message are unaffected: the neighbour
	// still evaluates (and reports its own pairing) rather than being skipped.
	if other.Evaluate([]message.Message{msg}) {
		t.Fatal("neighbour rule fired on an unreadable payload")
	}
	if !strings.Contains(buf.String(), "unaffected-neighbour") {
		t.Errorf("neighbour rule did not evaluate independently; got:\n%s", buf.String())
	}

	// Repeats of the same pairing are suppressed.
	before := strings.Count(buf.String(), "unreadable-observable")
	for range 50 {
		r.Evaluate([]message.Message{msg})
	}
	if after := strings.Count(buf.String(), "unreadable-observable"); after != before {
		t.Errorf("pairing reported %d more times on repeat; want 0", after-before)
	}

	// A different payload type against the same rule is a different pairing.
	generic := message.NewGenericJSON(map[string]any{"role": "editor"})
	genericMsg := message.NewBaseMessage(generic.Schema(), generic, "test")
	countBefore := strings.Count(buf.String(), "payload_type")
	r.Evaluate([]message.Message{genericMsg})
	if strings.Count(buf.String(), "payload_type") != countBefore {
		t.Errorf("readable generic payload produced an unreadable report:\n%s", buf.String())
	}
}

// A condition that is legitimately false produces no unreadable signal.
func TestExpressionRuleLegitimateFalseProducesNoSignal(t *testing.T) {
	var buf bytes.Buffer
	prev := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(&buf, &slog.HandlerOptions{Level: slog.LevelDebug})))
	t.Cleanup(func() { slog.SetDefault(prev) })

	r := newLoopCompletedRule(t, "legit-false")

	event := &agentic.LoopCompletedEvent{
		LoopID:  "loop-2",
		TaskID:  "task-2",
		Outcome: agentic.OutcomeSuccess,
		Role:    "editor", // does not match "architect"
	}
	msg := message.NewBaseMessage(event.Schema(), event, "test")

	if r.Evaluate([]message.Message{msg}) {
		t.Fatal("rule fired on a non-matching role")
	}
	if strings.Contains(buf.String(), "payload_type") {
		t.Errorf("legitimately-false condition produced an unreadable report:\n%s", buf.String())
	}
}

// Task 5.3: the shipped architect-editor rule can now fire. Loads the real
// config file and drives a wire-encoded LoopCompletedEvent through the
// production decoder — not a hand-built condition list.
func TestShippedArchitectEditorRuleFiresOnLoopCompletedEvent(t *testing.T) {
	raw, err := os.ReadFile("../../configs/rules/agentic-workflow/architect-editor.json")
	if err != nil {
		t.Fatalf("read shipped rule config: %v", err)
	}
	var def Definition
	if err := json.Unmarshal(raw, &def); err != nil {
		t.Fatalf("unmarshal shipped rule config: %v", err)
	}
	if !def.Enabled {
		t.Fatalf("shipped rule %s is disabled; this test asserts the enabled shipped config", def.ID)
	}

	r, err := NewExpressionRule(testPlatform, "agentic-workflow", def)
	if err != nil {
		t.Fatalf("NewExpressionRule from shipped config: %v", err)
	}

	event := &agentic.LoopCompletedEvent{
		LoopID:      "loop-architect-1",
		TaskID:      "task-42",
		Outcome:     agentic.OutcomeSuccess,
		Role:        "architect",
		Result:      "spec body the rule must not be able to read",
		Prompt:      "user task text the rule must not be able to read",
		Model:       "test-model",
		Iterations:  3,
		CompletedAt: time.Now().UTC(),
	}
	wire, err := json.Marshal(message.NewBaseMessage(event.Schema(), event, "agentic-loop"))
	if err != nil {
		t.Fatalf("marshal BaseMessage: %v", err)
	}

	decoded, err := payloadbuiltins.NewTestDecoder(t).Decode(wire)
	if err != nil {
		t.Fatalf("production decode: %v", err)
	}
	if _, ok := decoded.Payload().(*agentic.LoopCompletedEvent); !ok {
		t.Fatalf("decoder produced %T, want *agentic.LoopCompletedEvent", decoded.Payload())
	}

	if !r.Evaluate([]message.Message{decoded}) {
		t.Fatal("shipped architect-editor rule did not fire on a decoded LoopCompletedEvent")
	}

	// Content stays out even though the rule now reads the payload.
	fields, readable := ruleFields(decoded)
	if !readable {
		t.Fatal("decoded LoopCompletedEvent must be rule-readable")
	}
	for _, withheld := range []string{"result", "prompt"} {
		if _, present := fields[withheld]; present {
			t.Errorf("content field %q reached rule projection", withheld)
		}
	}
}

// TestMessagePathSubstitutesTypedPayloadFieldsIntoActions proves the spec's
// second clause: a declared value must reach `$message.*` SUBSTITUTION IN THE
// RULE'S ACTIONS, not merely a condition.
//
// The other tests in this file drive `Evaluate` or call `extractMessageData`
// directly, which proves the projection but not the substitution wire. This one
// runs the message lane:
//
//	typed payload -> BaseMessage wire bytes -> production decoder
//	  -> Processor.handleSemanticMessage -> evaluateRulesForMessage
//	  -> ExpressionRule.Evaluate -> StatefulEvaluator (OnEnter transition)
//	  -> ActionExecutor substitution -> observable publish
//
// SCOPE, stated because an earlier draft of this comment overclaimed it as "the
// real Processor": the decoder is the production one and every stage from
// decode through substitution is production code, but the Processor here is
// HAND-ASSEMBLED as a struct literal and driven through the unexported
// handleSemanticMessage. It does NOT exercise the component factory,
// Initialize/Start, real NATS delivery, or real KV state — newMockKVBucket and
// mockPublisher stand in for those. The real-lifecycle gap is filed as #1058.
//
// Every `$message.*` token in the action comes ONLY from
// LoopCompletedEvent.RuleFields — before this change the payload was
// unreadable, so the subject would have published with the literal tokens
// unresolved.
func TestMessagePathSubstitutesTypedPayloadFieldsIntoActions(t *testing.T) {
	def := Definition{
		ID:      "typed-payload-action-substitution",
		Type:    "expression",
		Name:    "typed-payload-action-substitution",
		Enabled: true,
		Logic:   "and",
		Conditions: []expression.ConditionExpression{
			{Field: "$message.role", Operator: "eq", Value: "architect"},
			{Field: "$message.outcome", Operator: "eq", Value: "success"},
		},
		OnEnter: []Action{{
			Type: ActionTypePublish,
			// Token LAST, deliberately. `messageTokenRe`
			// (message_substitution.go:54) is greedy over dotted paths, so a
			// mid-template token followed by a literal suffix — the shipped
			// architect-editor rule's `agent.task.$entity.task_id.editor`
			// shape — has the suffix swallowed into the path, resolves to
			// nothing, and is left literal with a loud warning. That is
			// existing engine behaviour this change does not touch; testing
			// the supported shape is what proves the projection reaches
			// substitution.
			Subject: "agent.task.editor.$message.task_id",
			Properties: map[string]any{
				"upstream_loop": "$message.loop_id",
				"model":         "$message.model",
				"iterations":    "$message.iterations",
			},
		}},
	}

	rule, err := NewExpressionRule(testPlatform, "test-pack", def)
	if err != nil {
		t.Fatalf("NewExpressionRule: %v", err)
	}

	publisher := &mockPublisher{}
	tracker := NewStateTracker(newMockKVBucket(), nil)
	processor := &Processor{
		logger:            slog.Default(),
		decoder:           payloadbuiltins.NewTestDecoder(t),
		rules:             map[string]Rule{def.ID: rule},
		ruleDefinitions:   map[string]Definition{def.ID: def},
		matchCounters:     map[string]*atomic.Int64{def.ID: {}},
		stateTracker:      tracker,
		statefulEvaluator: NewStatefulEvaluator(tracker, NewActionExecutorFull(slog.Default(), nil, publisher, testExecutorPlatform()), nil),
	}

	event := &agentic.LoopCompletedEvent{
		LoopID:      "loop-architect-7",
		TaskID:      "task-42",
		Outcome:     agentic.OutcomeSuccess,
		Role:        "architect",
		Model:       "test-model",
		Iterations:  3,
		Result:      "spec body the action template must not be able to splice",
		Prompt:      "user task text the action template must not be able to splice",
		CompletedAt: time.Now().UTC(),
	}
	wire, err := json.Marshal(message.NewBaseMessage(event.Schema(), event, "agentic-loop"))
	if err != nil {
		t.Fatalf("marshal BaseMessage: %v", err)
	}

	processor.handleSemanticMessage(context.Background(), "agent.complete.loop-architect-7", wire)

	if len(publisher.published) != 1 {
		t.Fatalf("action published %d messages, want 1 (rule did not fire through the production message lane)", len(publisher.published))
	}
	got := publisher.published[0]

	// The subject token came from the typed payload's projection.
	if got.subject != "agent.task.editor.task-42" {
		t.Errorf("published subject = %q, want %q — $message.task_id did not substitute from the typed payload",
			got.subject, "agent.task.editor.task-42")
	}

	body := string(got.data)
	for token, want := range map[string]string{
		"$message.loop_id":    "loop-architect-7",
		"$message.model":      "test-model",
		"$message.iterations": "3",
	} {
		if strings.Contains(body, token) {
			t.Errorf("action body still carries the unresolved template %q: %s", token, body)
		}
		if !strings.Contains(body, want) {
			t.Errorf("action body missing substituted value %q for %s: %s", want, token, body)
		}
	}

	// Content stays out of the action template as well as out of conditions.
	for _, withheld := range []string{
		"spec body the action template must not be able to splice",
		"user task text the action template must not be able to splice",
	} {
		if strings.Contains(body, withheld) {
			t.Errorf("withheld content reached the published action body: %s", body)
		}
	}
}

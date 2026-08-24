//go:build integration

package rule_test

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/nats-io/nats.go"
	"github.com/stretchr/testify/require"

	"github.com/c360studio/semstreams/agentic"
	"github.com/c360studio/semstreams/component"
	"github.com/c360studio/semstreams/message"
	"github.com/c360studio/semstreams/metric"
	"github.com/c360studio/semstreams/payloadbuiltins"
	"github.com/c360studio/semstreams/processor/rule"
)

// TestIntegration_RuleReadableProjectionProductionLifecycle proves the S5
// ruling of rule-readable-payload-projection through the PRODUCTION component
// lifecycle, closing the seam the hand-assembled unit test
// (TestMessagePathSubstitutesTypedPayloadFieldsIntoActions) deliberately does
// not cover:
//
//	rule.CreateRuleProcessor (production component factory, JSON config)
//	  -> Initialize -> Start (real NATS core subscription on the input port)
//	  -> wire-encoded typed payload published to the input subject
//	  -> production decoder (deps.PayloadRegistry, payloadbuiltins set)
//	  -> ruleFields projection -> ExpressionRule.Evaluate
//	  -> StatefulEvaluator OnEnter against the real RULE_STATE KV bucket
//	  -> ActionExecutor substitution -> actionPublisher -> real NATS output
//
// Every `$message.*` token the rule conditions and the action template use
// exists ONLY via agentic.LoopCompletedEvent.RuleFields — before the
// projection this payload was unreadable on the rule lane, the conditions
// were silently false forever, and the action could never fire.
//
// What this test does NOT claim: the deployed Docker e2e flow. The e2e TIER
// assertion (typed-payload stage in task e2e:agentic) remains #1058's scope.
//
// Synchronization is causal, not sleep-based. The negative (non-matching)
// payload is published BEFORE the positive one on the same connection the
// processor subscribes and publishes with, so total order guarantees that if
// the negative had fired, its output would arrive before the positive's.
// Additionally, the negative message's persisted RULE_STATE entry
// (IsMatching=false) proves it was fully evaluated — not merely lost — before
// the zero-output assertion is made. The 10s Eventually bounds are transport
// budgets for a local container round trip, not evaluation windows.
func TestIntegration_RuleReadableProjectionProductionLifecycle(t *testing.T) {
	nc := getTestNATSClient(t)

	const (
		inputSubject  = "process.itest.ruleproj.complete"
		outputPattern = "events.itest.ruleproj.dispatch.>"
		ruleID        = "typed-projection-itest"
	)

	// Operator-shaped JSON config through the production factory — the same
	// entry the component registry invokes in cmd/semstreams.
	configJSON := fmt.Sprintf(`{
		"pack_id": "ruleproj-itest",
		"ports": {
			"inputs": [
				{"name": "semantic_input", "config": {"kind": "nats", "subject": %q}, "required": true}
			],
			"outputs": [
				{"name": "rule_events", "config": {"kind": "nats", "subject": "events.itest.ruleproj.rule"}, "required": true}
			]
		},
		"inline_rules": [
			{
				"id": %q,
				"type": "expression",
				"name": "Typed projection through production lifecycle",
				"enabled": true,
				"logic": "and",
				"conditions": [
					{"field": "$message.role", "operator": "eq", "value": "architect"},
					{"field": "$message.outcome", "operator": "eq", "value": "success"}
				],
				"on_enter": [
					{
						"type": "publish",
						"subject": "events.itest.ruleproj.dispatch.$message.task_id",
						"properties": {
							"upstream_loop": "$message.loop_id",
							"model": "$message.model",
							"iterations": "$message.iterations"
						}
					}
				]
			}
		]
	}`, inputSubject, ruleID)

	deps := component.Dependencies{
		NATSClient:      nc,
		MetricsRegistry: metric.NewMetricsRegistry(),
		PayloadRegistry: payloadbuiltins.NewTestRegistry(t),
	}

	discoverable, err := rule.CreateRuleProcessor(json.RawMessage(configJSON), deps)
	require.NoError(t, err, "production factory rejected the config")

	lc, ok := component.AsLifecycleComponent(discoverable)
	require.True(t, ok, "rule processor must implement component.LifecycleComponent")

	require.NoError(t, lc.Initialize())

	// Start context stays live until Stop returns; Stop gets its own bounded
	// context (terminal finalizer with no parent contract).
	startCtx, cancelStart := context.WithCancel(context.Background())
	defer cancelStart()
	require.NoError(t, lc.Start(startCtx))
	defer func() {
		stopCtx, cancelStop := context.WithTimeout(context.Background(), 15*time.Second)
		defer cancelStop()
		if err := lc.Stop(stopCtx); err != nil {
			t.Errorf("Stop: %v", err)
		}
	}()

	testCtx, cancelTest := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancelTest()

	// Subscribe on the real output family BEFORE publishing any input. Both
	// the test and the processor use the same connection, so this SUB is
	// server-registered before any output publish can occur.
	type delivery struct {
		subject string
		data    []byte
	}
	var mu sync.Mutex
	var outputs []delivery
	_, err = nc.Subscribe(testCtx, outputPattern, func(_ context.Context, m *nats.Msg) {
		mu.Lock()
		outputs = append(outputs, delivery{subject: m.Subject, data: append([]byte(nil), m.Data...)})
		mu.Unlock()
	})
	require.NoError(t, err)

	const (
		withheldResult = "model output body that must never reach a rule action"
		withheldPrompt = "user prompt text that must never reach a rule action"
	)

	// Negative lane: same payload type, same outcome, wrong role — the
	// $message.role condition must reject it.
	negative := &agentic.LoopCompletedEvent{
		LoopID:      "loop-editor-9",
		TaskID:      "task-neg",
		Outcome:     agentic.OutcomeSuccess,
		Role:        "editor",
		Model:       "model-under-test",
		Iterations:  5,
		Result:      withheldResult,
		Prompt:      withheldPrompt,
		CompletedAt: time.Now().UTC(),
	}
	positive := &agentic.LoopCompletedEvent{
		LoopID:      "loop-architect-7",
		TaskID:      "task-42",
		Outcome:     agentic.OutcomeSuccess,
		Role:        "architect",
		Model:       "model-under-test",
		Iterations:  3,
		Result:      withheldResult,
		Prompt:      withheldPrompt,
		CompletedAt: time.Now().UTC(),
	}

	// Wire-encode through the production envelope: BaseMessage MarshalJSON
	// with the payload's registered type — the exact bytes a producing
	// component publishes.
	negMsg := message.NewBaseMessage(negative.Schema(), negative, "agentic-loop")
	negWire, err := json.Marshal(negMsg)
	require.NoError(t, err)
	posMsg := message.NewBaseMessage(positive.Schema(), positive, "agentic-loop")
	posWire, err := json.Marshal(posMsg)
	require.NoError(t, err)

	// Negative FIRST: single-connection total order means a spurious negative
	// fire would be observed before the positive output.
	require.NoError(t, nc.Publish(testCtx, inputSubject, negWire))
	require.NoError(t, nc.Publish(testCtx, inputSubject, posWire))

	require.Eventually(t, func() bool {
		mu.Lock()
		defer mu.Unlock()
		return len(outputs) >= 1
	}, 10*time.Second, 50*time.Millisecond,
		"rule action output never arrived on the real output subject — projection did not reach the production lifecycle")

	mu.Lock()
	first := outputs[0]
	count := len(outputs)
	mu.Unlock()

	// Exactly one output, and it is the positive one. The negative message was
	// processed strictly before the positive (same input subscription, ordered
	// delivery, synchronous handler), so its output — had it fired — would
	// have been received first.
	require.Equal(t, 1, count, "expected exactly the positive fire; negative payload must not produce output")
	require.Equal(t, "events.itest.ruleproj.dispatch.task-42", first.subject,
		"$message.task_id did not substitute from the typed payload's projection into the action subject")

	var body struct {
		EntityID   string         `json:"entity_id"`
		Source     string         `json:"source"`
		Properties map[string]any `json:"properties"`
	}
	require.NoError(t, json.Unmarshal(first.data, &body))
	require.Equal(t, "rule_engine", body.Source)
	// Message-path state identity for a typed payload is the wire message
	// ID — never the projection, whatever fields it exposes.
	require.Equal(t, posMsg.ID(), body.EntityID)
	require.Equal(t, "loop-architect-7", body.Properties["upstream_loop"],
		"$message.loop_id did not substitute into the action properties")
	require.Equal(t, "model-under-test", body.Properties["model"],
		"$message.model did not substitute into the action properties")
	require.Equal(t, "3", body.Properties["iterations"],
		"$message.iterations did not substitute into the action properties")

	// Withheld content (Result, Prompt — model output and user task text)
	// must not appear anywhere in the published output, and no template may
	// be left unresolved.
	whole := first.subject + " " + string(first.data)
	require.NotContains(t, whole, withheldResult, "withheld model output reached the published action")
	require.NotContains(t, whole, withheldPrompt, "withheld prompt text reached the published action")
	require.NotContains(t, whole, "$message.", "unresolved substitution template reached the wire")

	// Real KV state seam: the OnEnter transition was computed against and
	// persisted to the RULE_STATE bucket the production Start created —
	// positive entered, negative evaluated and not matching. The negative
	// entry existing at all is the proof its message was fully evaluated
	// rather than lost, which is what makes the zero-output assertion above
	// a verdict instead of a timeout.
	js, err := nc.JetStream()
	require.NoError(t, err)
	kv, err := js.KeyValue(testCtx, "RULE_STATE")
	require.NoError(t, err, "production Start did not provision the RULE_STATE bucket")

	readState := func(msgID string) (isMatching bool, lastTransition string, found bool) {
		entry, getErr := kv.Get(testCtx, ruleID+"."+msgID)
		if getErr != nil {
			return false, "", false
		}
		var st struct {
			IsMatching     bool   `json:"is_matching"`
			LastTransition string `json:"last_transition"`
		}
		if json.Unmarshal(entry.Value(), &st) != nil {
			return false, "", false
		}
		return st.IsMatching, st.LastTransition, true
	}

	require.Eventually(t, func() bool {
		_, _, found := readState(posMsg.ID())
		return found
	}, 10*time.Second, 50*time.Millisecond, "positive match state never persisted to RULE_STATE")
	posMatching, posTransition, _ := readState(posMsg.ID())
	require.True(t, posMatching, "positive payload must persist IsMatching=true")
	require.Equal(t, string(rule.TransitionEntered), posTransition)

	require.Eventually(t, func() bool {
		_, _, found := readState(negMsg.ID())
		return found
	}, 10*time.Second, 50*time.Millisecond, "negative match state never persisted to RULE_STATE — negative message was not evaluated")
	negMatching, _, _ := readState(negMsg.ID())
	require.False(t, negMatching, "negative payload must persist IsMatching=false")
}

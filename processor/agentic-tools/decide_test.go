package agentictools

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"strings"
	"testing"

	"github.com/c360studio/semstreams/agentic"
	"github.com/c360studio/semstreams/message"
	"github.com/c360studio/semstreams/types"
	agvocab "github.com/c360studio/semstreams/vocabulary/agentic"
)

// recordingPublisher implements TriplePublisher in-process so tests can
// assert exactly what triples the agent-tool emitted.
//
// Records via both AddTriple (single) and AddTriplesBatch (atomic).
// On error BOTH methods bail before recording — atomic-failure mirror
// of the production graph-ingest CAS path, where a failed batch
// commits nothing for the shared Subject. Tests that need to inspect
// "what triples WOULD have been emitted" can read the migrated tool's
// constructed slice via other means (the executors build the slice
// before calling the publisher).
type recordingPublisher struct {
	triples []message.Triple
	// batchCalls records the number of AddTriplesBatch invocations so
	// migrated callers can assert they made the single atomic call
	// rather than N per-triple calls.
	batchCalls int
	// createCalls records the number of CreateEntityWithTriples (birth)
	// invocations; createdEntityID / createdMsgType capture the last birth's
	// envelope so emit_diagnosis tests can assert the finding entity is born
	// with the ops_diagnosis typed origin (gh#390).
	createCalls     int
	createdEntityID string
	createdMsgType  message.Type
	err             error
}

func (p *recordingPublisher) CreateEntityWithTriples(_ context.Context, entityID string, msgType message.Type, triples []message.Triple) error {
	p.createCalls++
	if p.err != nil {
		return p.err
	}
	p.createdEntityID = entityID
	p.createdMsgType = msgType
	p.triples = append(p.triples, triples...)
	return nil
}

func (p *recordingPublisher) AddTriple(_ context.Context, triple message.Triple) error {
	if p.err != nil {
		return p.err
	}
	p.triples = append(p.triples, triple)
	return nil
}

func (p *recordingPublisher) AddTriplesBatch(_ context.Context, triples []message.Triple) error {
	p.batchCalls++
	if p.err != nil {
		return p.err
	}
	p.triples = append(p.triples, triples...)
	return nil
}

func newDecideExecutor(publisher TriplePublisher) *DecideExecutor {
	return NewDecideExecutor(publisher, types.PlatformMeta{Org: "acme", Platform: "test"}, nil)
}

// newDecideExecutorRestricted builds an executor with a deployment-level
// decide-action restriction policy (gh#239) for the restriction tests.
func newDecideExecutorRestricted(publisher TriplePublisher, restricted ...string) *DecideExecutor {
	return NewDecideExecutor(publisher, types.PlatformMeta{Org: "acme", Platform: "test"}, restricted)
}

func TestDecideExecutor_ListTools(t *testing.T) {
	e := newDecideExecutor(&recordingPublisher{})
	tools := e.ListTools()
	if len(tools) != 1 {
		t.Fatalf("expected 1 tool, got %d", len(tools))
	}
	if tools[0].Name != DecideToolName {
		t.Errorf("expected tool name %q, got %q", DecideToolName, tools[0].Name)
	}
	required, _ := tools[0].Parameters["required"].([]string)
	want := map[string]bool{"action": true, "reason": true}
	for _, r := range required {
		if !want[r] {
			t.Errorf("unexpected required field: %s", r)
		}
		delete(want, r)
	}
	if len(want) > 0 {
		t.Errorf("missing required fields: %v", want)
	}
}

// TestDecideExecutor_UsesBatchPath confirms decide emits via
// AddTriplesBatch (not the legacy per-triple loop). Atomicity matters
// here because downstream rules match on coordinator.decision.next-action
// AND coordinator.decision.reason being co-present on the loop entity;
// pre-2026-05-13 per-triple emission could leave one without the other
// on graph-ingest degradation, breaking deterministic rule routing.
func TestDecideExecutor_UsesBatchPath(t *testing.T) {
	pub := &recordingPublisher{}
	e := newDecideExecutor(pub)
	_, err := e.Execute(context.Background(), agentic.ToolCall{
		ID:        "c",
		Name:      DecideToolName,
		LoopID:    "loop-batch",
		Arguments: map[string]any{"action": "done", "reason": "test"},
	})
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if pub.batchCalls != 1 {
		t.Errorf("AddTriplesBatch call count = %d, want 1 (single atomic batch)", pub.batchCalls)
	}
}

// TestDecideExecutor_DescriptionDoesNotPreloadActions guards the
// shape of the leak that motivated this slice: tool descriptions
// MUST NOT enumerate example action names. Real-LLM models read tool
// descriptions as authoritative and bias their terminal choice toward
// whichever the description names — overriding the persona's enumerated
// vocabulary. The action vocabulary belongs strictly in each role's
// system prompt.
//
// The test is product-agnostic. It does NOT enumerate specific action
// names (those are downstream-flow concerns). Instead it pattern-matches
// the SHAPES that leak example vocabulary into the LLM's context:
//
//  1. `e\.g\.` followed by identifier-shaped tokens — the
//     "give the LLM a worked example" anti-pattern.
//  2. `action=<identifier>` style references in property
//     descriptions — the "tell the LLM when to populate this field"
//     anti-pattern that embeds specific action names.
//
// Future flows that need new action names enumerate them in the
// persona/system prompt. Adding examples here re-opens the smoke-#7
// drift class regardless of which specific names get leaked.
func TestDecideExecutor_DescriptionDoesNotPreloadActions(t *testing.T) {
	e := newDecideExecutor(&recordingPublisher{})
	tools := e.ListTools()
	if len(tools) != 1 {
		t.Fatalf("expected 1 tool, got %d", len(tools))
	}

	// The full text the LLM sees: top-level Description + every nested
	// `description` field on properties. All contribute to the LLM's
	// understanding of when to pick which action.
	type labelled struct {
		label string
		text  string
	}
	var llmVisibleText []labelled
	llmVisibleText = append(llmVisibleText, labelled{"Description", tools[0].Description})
	if props, ok := tools[0].Parameters["properties"].(map[string]any); ok {
		for name, prop := range props {
			if propMap, ok := prop.(map[string]any); ok {
				if desc, ok := propMap["description"].(string); ok {
					llmVisibleText = append(llmVisibleText, labelled{"properties." + name + ".description", desc})
				}
			}
		}
	}

	// Anti-pattern 1: "e.g." followed by identifier-shaped tokens
	// (lowercase or snake_case words). Matches the original leak
	// `(e.g. fan_out, synthesize, retry, done)` regardless of which
	// specific names appear.
	exampleListPattern := regexp.MustCompile(`(?i)e\.g\.\s+[a-z][a-z0-9_]*`)

	// Anti-pattern 2: `action=<identifier>` references (the
	// "when action=X" shape that embedded specific values into the
	// subtopics/retry_hint property descriptions).
	actionEqPattern := regexp.MustCompile(`(?i)action\s*=\s*[a-z][a-z0-9_]*`)

	// snippet returns a bounded slice of text starting at idx for
	// inclusion in the failure message — bounded so a multi-paragraph
	// description doesn't dump 500 chars into the test output.
	snippet := func(text string, start, end int) string {
		stop := end + 30
		if stop > len(text) {
			stop = len(text)
		}
		return text[start:stop]
	}

	for _, lt := range llmVisibleText {
		if loc := exampleListPattern.FindStringIndex(lt.text); loc != nil {
			t.Errorf("decide tool %s leaks an example-action list at %q. The action vocabulary belongs in the role's system prompt, not in framework tool text. See ListTools comment for the smoke-#7 rationale.",
				lt.label, snippet(lt.text, loc[0], loc[1]))
		}
		if loc := actionEqPattern.FindStringIndex(lt.text); loc != nil {
			t.Errorf("decide tool %s embeds action=<identifier> at %q. Property descriptions must not name specific actions — describe the field's purpose flow-agnostically.",
				lt.label, snippet(lt.text, loc[0], loc[1]))
		}
	}
}

// TestDecideExecutor_ActionAllowlist_Accepts verifies that a decide call
// with action in the allowlist (passed via ToolCall.Metadata under
// MetadataKeyDecideActionAllowlist) executes normally — triples are
// published, StopLoop=true, payload returned. Allowlist enforcement is
// silent on the happy path; only rejections produce error responses.
func TestDecideExecutor_ActionAllowlist_Accepts(t *testing.T) {
	pub := &recordingPublisher{}
	e := newDecideExecutor(pub)

	res, err := e.Execute(context.Background(), agentic.ToolCall{
		ID:     "c1",
		Name:   DecideToolName,
		LoopID: "loop-abc",
		Metadata: map[string]any{
			agentic.MetadataKeyDecideActionAllowlist: []any{"planned", "needs_clarification"},
		},
		Arguments: map[string]any{
			"action": "planned",
			"reason": "plan emitted",
		},
	})
	if err != nil {
		t.Fatalf("unexpected err: %v", err)
	}
	if res.Error != "" {
		t.Errorf("tool error on allowed action: %q", res.Error)
	}
	if !res.StopLoop {
		t.Errorf("expected StopLoop=true on allowed action")
	}
	if len(pub.triples) != 2 {
		t.Errorf("expected 2 triples published on allowed action, got %d", len(pub.triples))
	}
}

// TestDecideExecutor_ActionAllowlist_Rejects verifies that a decide call
// with action OUTSIDE the allowlist returns ToolErrorInvalidArgs (so the
// LLM gets a chance to retry with a valid name) and does NOT publish
// triples. Error message names the valid set so the LLM can converge
// without a third-party hint. This is the smoke-#7 fan_out scenario:
// the planner persona enumerates "planned" but the LLM hallucinated
// "fan_out"; allowlist gate rejects, LLM corrects.
func TestDecideExecutor_ActionAllowlist_Rejects(t *testing.T) {
	pub := &recordingPublisher{}
	e := newDecideExecutor(pub)

	res, _ := e.Execute(context.Background(), agentic.ToolCall{
		ID:     "c1",
		Name:   DecideToolName,
		LoopID: "loop-abc",
		Metadata: map[string]any{
			agentic.MetadataKeyDecideActionAllowlist: []any{"planned"},
		},
		Arguments: map[string]any{
			"action": "fan_out",
			"reason": "<plan body>",
		},
	})
	if res.ErrorKind != agentic.ToolErrorInvalidArgs {
		t.Errorf("ErrorKind = %v, want ToolErrorInvalidArgs (allowlist gate)", res.ErrorKind)
	}
	if res.Error == "" {
		t.Errorf("expected non-empty Error message naming the valid action set")
	}
	for _, want := range []string{"fan_out", "planned"} {
		if !strings.Contains(res.Error, want) {
			t.Errorf("expected Error message to mention %q for LLM convergence; got %q", want, res.Error)
		}
	}
	if res.StopLoop {
		t.Errorf("StopLoop should be false on allowlist rejection — let the loop iterate")
	}
	if len(pub.triples) != 0 {
		t.Errorf("no triples should be published on allowlist rejection, got %d", len(pub.triples))
	}
}

// TestDecideExecutor_ActionAllowlist_NoAllowlistMeansFreeform verifies
// that a decide call without MetadataKeyDecideActionAllowlist (the
// pre-F2 wire shape) accepts any action — back-compat for flows that
// haven't adopted the allowlist field on their publish_agent rules.
func TestDecideExecutor_ActionAllowlist_NoAllowlistMeansFreeform(t *testing.T) {
	pub := &recordingPublisher{}
	e := newDecideExecutor(pub)

	res, _ := e.Execute(context.Background(), agentic.ToolCall{
		ID:     "c1",
		Name:   DecideToolName,
		LoopID: "loop-abc",
		// No Metadata at all.
		Arguments: map[string]any{
			"action": "completely_made_up",
			"reason": "no allowlist set",
		},
	})
	if res.Error != "" {
		t.Errorf("free-form mode rejected an action: %q", res.Error)
	}
	if !res.StopLoop {
		t.Errorf("expected StopLoop=true in free-form mode")
	}
}

// TestDecideExecutor_ActionAllowlist_EmptyAllowlistMeansFreeform
// verifies that an explicitly-empty allowlist is also free-form (rule
// authors who set an empty array shouldn't be silently locked out of
// every action). Symmetric with how task.Tools handles its nil-vs-empty
// distinction.
func TestDecideExecutor_ActionAllowlist_EmptyAllowlistMeansFreeform(t *testing.T) {
	pub := &recordingPublisher{}
	e := newDecideExecutor(pub)

	res, _ := e.Execute(context.Background(), agentic.ToolCall{
		ID:     "c1",
		Name:   DecideToolName,
		LoopID: "loop-abc",
		Metadata: map[string]any{
			agentic.MetadataKeyDecideActionAllowlist: []any{},
		},
		Arguments: map[string]any{
			"action": "anything",
			"reason": "empty allowlist",
		},
	})
	if res.Error != "" {
		t.Errorf("empty-allowlist mode rejected: %q", res.Error)
	}
}

// --- gh#239: deployment-level decide-action restriction policy ---

// TestDecideExecutor_RestrictedAction_FrontDoorRejects is the core gh#239
// case: a coordinator that carries NO per-task allowlist (the front door)
// still has a restricted action structurally barred. Returns
// ToolErrorInvalidArgs (so the loop iterates and the model re-picks), names
// the action, and publishes no triples.
func TestDecideExecutor_RestrictedAction_FrontDoorRejects(t *testing.T) {
	pub := &recordingPublisher{}
	e := newDecideExecutorRestricted(pub, "ask_user")

	res, _ := e.Execute(context.Background(), agentic.ToolCall{
		ID:     "c1",
		Name:   DecideToolName,
		LoopID: "loop-abc",
		// No Metadata: the front-door shape gh#239 closes.
		Arguments: map[string]any{
			"action": "ask_user",
			"reason": "need the user to clarify",
		},
	})
	if res.ErrorKind != agentic.ToolErrorInvalidArgs {
		t.Errorf("ErrorKind = %v, want ToolErrorInvalidArgs (restriction gate)", res.ErrorKind)
	}
	if !strings.Contains(res.Error, "ask_user") {
		t.Errorf("expected Error to name the restricted action; got %q", res.Error)
	}
	if res.StopLoop {
		t.Errorf("StopLoop should be false on restriction rejection — let the loop iterate")
	}
	if len(pub.triples) != 0 {
		t.Errorf("no triples should be published on restriction rejection, got %d", len(pub.triples))
	}
}

// TestDecideExecutor_RestrictedAction_TakesPrecedenceOverAllowlist proves
// the restriction beats a per-task allowlist that WOULD admit the action.
// This is the precedence semantics: forbidden ∪ off-allowlist both reject.
func TestDecideExecutor_RestrictedAction_TakesPrecedenceOverAllowlist(t *testing.T) {
	pub := &recordingPublisher{}
	e := newDecideExecutorRestricted(pub, "ask_user")

	res, _ := e.Execute(context.Background(), agentic.ToolCall{
		ID:     "c1",
		Name:   DecideToolName,
		LoopID: "loop-abc",
		Metadata: map[string]any{
			// ask_user IS on the allowlist, yet the deployment policy bars it.
			agentic.MetadataKeyDecideActionAllowlist: []any{"ask_user", "respond_direct"},
		},
		Arguments: map[string]any{
			"action": "ask_user",
			"reason": "allowlist admits it but policy forbids it",
		},
	})
	if res.ErrorKind != agentic.ToolErrorInvalidArgs {
		t.Errorf("ErrorKind = %v, want ToolErrorInvalidArgs (restriction precedence over allowlist)", res.ErrorKind)
	}
	if len(pub.triples) != 0 {
		t.Errorf("no triples on restricted action even when allowlisted, got %d", len(pub.triples))
	}
}

// TestDecideExecutor_RestrictedAction_NormalisedDriftStillRejected proves
// the gate is SAP-normalised on both sides: cased/hyphenated drift in the
// emitted action ("Ask-User") cannot slip a restricted action, and a
// config-side drift in the restriction list ("Ask-User") still bars the
// canonical action ("ask_user"). Either way the gate holds.
func TestDecideExecutor_RestrictedAction_NormalisedDriftStillRejected(t *testing.T) {
	t.Run("drift in emitted action", func(t *testing.T) {
		pub := &recordingPublisher{}
		e := newDecideExecutorRestricted(pub, "ask_user")
		res, _ := e.Execute(context.Background(), agentic.ToolCall{
			ID: "c1", Name: DecideToolName, LoopID: "loop-abc",
			Arguments: map[string]any{"action": "Ask-User", "reason": "drift"},
		})
		if res.ErrorKind != agentic.ToolErrorInvalidArgs {
			t.Errorf("ErrorKind = %v, want ToolErrorInvalidArgs ('Ask-User' must not slip the gate)", res.ErrorKind)
		}
	})
	t.Run("drift in restriction config", func(t *testing.T) {
		pub := &recordingPublisher{}
		e := newDecideExecutorRestricted(pub, "Ask-User", "  ", "")
		res, _ := e.Execute(context.Background(), agentic.ToolCall{
			ID: "c1", Name: DecideToolName, LoopID: "loop-abc",
			Arguments: map[string]any{"action": "ask_user", "reason": "canonical"},
		})
		if res.ErrorKind != agentic.ToolErrorInvalidArgs {
			t.Errorf("ErrorKind = %v, want ToolErrorInvalidArgs (normalised config entry must bar canonical action)", res.ErrorKind)
		}
	})
}

// TestDecideExecutor_RestrictedAction_UnrelatedActionPasses confirms the
// policy is surgical: an action NOT on the restriction list flows through
// normally, publishing its triples and stopping the loop.
func TestDecideExecutor_RestrictedAction_UnrelatedActionPasses(t *testing.T) {
	pub := &recordingPublisher{}
	e := newDecideExecutorRestricted(pub, "ask_user")

	res, err := e.Execute(context.Background(), agentic.ToolCall{
		ID:     "c1",
		Name:   DecideToolName,
		LoopID: "loop-abc",
		Arguments: map[string]any{
			"action": "respond_direct",
			"reason": "resolved autonomously",
		},
	})
	if err != nil {
		t.Fatalf("unexpected err: %v", err)
	}
	if res.Error != "" {
		t.Errorf("unrelated action rejected by restriction policy: %q", res.Error)
	}
	if !res.StopLoop {
		t.Errorf("expected StopLoop=true on a non-restricted action")
	}
	if len(pub.triples) != 2 {
		t.Errorf("expected 2 triples on a non-restricted action, got %d", len(pub.triples))
	}
}

// TestDecideExecutor_RestrictedAction_EmptyPolicyIsPermissive locks the
// gh#239 default: no restriction list ⇒ no behaviour change. An action that
// would be barred under a policy flows through when the policy is empty.
func TestDecideExecutor_RestrictedAction_EmptyPolicyIsPermissive(t *testing.T) {
	pub := &recordingPublisher{}
	e := newDecideExecutorRestricted(pub) // empty restriction list

	res, _ := e.Execute(context.Background(), agentic.ToolCall{
		ID:     "c1",
		Name:   DecideToolName,
		LoopID: "loop-abc",
		Arguments: map[string]any{
			"action": "ask_user",
			"reason": "permissive default",
		},
	})
	if res.Error != "" {
		t.Errorf("permissive default rejected an action: %q", res.Error)
	}
	if !res.StopLoop {
		t.Errorf("expected StopLoop=true under permissive default")
	}
}

// TestDecideExecutor_RestrictedAction_BeatsSAPCoercion proves the
// restriction is evaluated on the canonical (post-SAP) action and takes
// precedence over SAP success: an emitted "ask-user" that SAP would coerce
// to the allowlisted "ask_user" is still rejected, and the SAP-coercion
// audit triple is NOT published (the action never reached the success path).
func TestDecideExecutor_RestrictedAction_BeatsSAPCoercion(t *testing.T) {
	pub := &recordingPublisher{}
	e := newDecideExecutorRestricted(pub, "ask_user")

	res, _ := e.Execute(context.Background(), agentic.ToolCall{
		ID:     "c1",
		Name:   DecideToolName,
		LoopID: "loop-abc",
		Metadata: map[string]any{
			agentic.MetadataKeyDecideActionAllowlist: []any{"ask_user", "respond_direct"},
		},
		Arguments: map[string]any{
			"action": "ask-user", // SAP would coerce → "ask_user"
			"reason": "drift that resolves to a restricted action",
		},
	})
	if res.ErrorKind != agentic.ToolErrorInvalidArgs {
		t.Errorf("ErrorKind = %v, want ToolErrorInvalidArgs (restriction beats SAP coercion)", res.ErrorKind)
	}
	if len(pub.triples) != 0 {
		t.Errorf("no triples (incl. SAP audit) should publish when restriction rejects, got %d", len(pub.triples))
	}
}

// TestDecideExecutor_RestrictedDecideActions_ConfigRoundTrip drives the
// full operator → runtime wire: the restricted_decide_actions JSON an
// operator writes, decoded into the agentic-tools Config, then handed to
// NewDecideExecutor, must enforce the gate. Guards the operator-configurable
// surface per the JSON-round-trip discipline.
func TestDecideExecutor_RestrictedDecideActions_ConfigRoundTrip(t *testing.T) {
	const raw = `{"timeout":"60s","restricted_decide_actions":["ask_user"]}`

	var cfg Config
	if err := json.Unmarshal([]byte(raw), &cfg); err != nil {
		t.Fatalf("unmarshal config: %v", err)
	}
	if len(cfg.RestrictedDecideActions) != 1 || cfg.RestrictedDecideActions[0] != "ask_user" {
		t.Fatalf("config round-trip dropped the field: got %#v", cfg.RestrictedDecideActions)
	}

	pub := &recordingPublisher{}
	e := NewDecideExecutor(pub, types.PlatformMeta{Org: "acme", Platform: "test"}, cfg.RestrictedDecideActions)
	res, _ := e.Execute(context.Background(), agentic.ToolCall{
		ID:     "c1",
		Name:   DecideToolName,
		LoopID: "loop-abc",
		Arguments: map[string]any{
			"action": "ask_user",
			"reason": "config-driven restriction must fire",
		},
	})
	if res.ErrorKind != agentic.ToolErrorInvalidArgs {
		t.Errorf("ErrorKind = %v, want ToolErrorInvalidArgs (config-sourced restriction must enforce)", res.ErrorKind)
	}
	if len(pub.triples) != 0 {
		t.Errorf("no triples on config-sourced restriction rejection, got %d", len(pub.triples))
	}

	// And re-marshal preserves the field (full round-trip).
	out, err := json.Marshal(cfg)
	if err != nil {
		t.Fatalf("marshal config: %v", err)
	}
	if !strings.Contains(string(out), `"restricted_decide_actions":["ask_user"]`) {
		t.Errorf("re-marshal dropped restricted_decide_actions: %s", out)
	}
}

// TestDecideExecutor_HappyPath verifies a valid decide call emits the two
// TestDecideExecutor_SAP_CoercesAndSignalsLoudly is the regression
// for the beta.44 SAP layer: a near-miss action ("fan-out" when the
// allowlist names "fan_out") gets coerced to the canonical form,
// and every one of the four LOUD signals fires before the tool
// returns success.
//
// The user's explicit constraint 2026-05-05: SAP can mask
// model/persona mismatches, so every coercion must be impossible to
// miss in operator dashboards. This test pins all four runtime
// signals (audit triple, tool-result metadata, canonical-action
// triple). The Prometheus counter and slog.Warn fire too but
// aren't asserted here — they're verified by separate
// TestDecideExecutor_SAP_PrometheusCounter and end-to-end smoke
// against operator dashboards.
func TestDecideExecutor_SAP_CoercesAndSignalsLoudly(t *testing.T) {
	pub := &recordingPublisher{}
	e := newDecideExecutor(pub)

	res, _ := e.Execute(context.Background(), agentic.ToolCall{
		ID:     "c-sap-1",
		Name:   DecideToolName,
		LoopID: "loop-coerce",
		Metadata: map[string]any{
			agentic.MetadataKeyDecideActionAllowlist: []any{"fan_out", "synthesize"},
		},
		Arguments: map[string]any{
			"action": "fan-out", // hyphen drift; should coerce to "fan_out"
			"reason": "common LLM normalisation drift",
		},
	})

	// Tool succeeds (no rejection error) and StopLoop fires.
	if res.Error != "" {
		t.Fatalf("SAP coercion should NOT produce a tool error; got %q", res.Error)
	}
	if !res.StopLoop {
		t.Errorf("SAP coercion should still emit StopLoop=true on success")
	}

	// Signal 4: ToolResult metadata flags the coercion + raw input.
	if got := res.Metadata["sap_coerced"]; got != true {
		t.Errorf("metadata.sap_coerced = %v, want true", got)
	}
	if got := res.Metadata["sap_raw_action"]; got != "fan-out" {
		t.Errorf("metadata.sap_raw_action = %v, want %q", got, "fan-out")
	}
	// And the canonical action surfaces in the standard metadata
	// fields downstream consumers already read.
	if got := res.Metadata["action"]; got != "fan_out" {
		t.Errorf("metadata.action = %v, want canonical %q", got, "fan_out")
	}

	// Signal 3 (audit triple) AND the canonical decision triple both
	// land. We expect 3 triples: the next_action canonical
	// "fan_out", the decision_reason, AND the SAP-coerced audit
	// predicate carrying "raw|canonical".
	if len(pub.triples) != 3 {
		t.Fatalf("triples published = %d, want 3 (next_action + reason + sap_coerced audit)", len(pub.triples))
	}
	facts := map[string]any{}
	for _, tr := range pub.triples {
		facts[tr.Predicate] = tr.Object
	}
	if got := facts[agvocab.CoordinatorNextAction]; got != "fan_out" {
		t.Errorf("CoordinatorNextAction = %v, want canonical %q", got, "fan_out")
	}
	if got := facts[agvocab.CoordinatorDecisionSAPCoerced]; got != "fan-out|fan_out" {
		t.Errorf("CoordinatorDecisionSAPCoerced = %v, want %q", got, "fan-out|fan_out")
	}
}

// TestDecideExecutor_SAP_ExactMatchSkipsCoercionPath confirms the
// hot path is unaffected: when the LLM emits the canonical form
// directly, no SAP signals fire and the result shape matches the
// pre-beta.44 happy-path test exactly.
func TestDecideExecutor_SAP_ExactMatchSkipsCoercionPath(t *testing.T) {
	pub := &recordingPublisher{}
	e := newDecideExecutor(pub)

	res, _ := e.Execute(context.Background(), agentic.ToolCall{
		ID:     "c-sap-2",
		Name:   DecideToolName,
		LoopID: "loop-clean",
		Metadata: map[string]any{
			agentic.MetadataKeyDecideActionAllowlist: []any{"fan_out", "synthesize"},
		},
		Arguments: map[string]any{
			"action": "fan_out",
			"reason": "ok",
		},
	})

	if res.Error != "" {
		t.Fatalf("exact match should not produce error; got %q", res.Error)
	}
	if got, present := res.Metadata["sap_coerced"]; present && got == true {
		t.Errorf("metadata.sap_coerced set on exact match; expected absent")
	}
	if got, present := res.Metadata["sap_raw_action"]; present {
		t.Errorf("metadata.sap_raw_action set on exact match (got %v); expected absent", got)
	}

	// Only 2 triples on the exact-match path — no SAP-coerced audit.
	if len(pub.triples) != 2 {
		t.Errorf("triples = %d, want 2 on exact match (no sap_coerced audit)", len(pub.triples))
	}
}

// TestDecideExecutor_SAP_NoNormalisedMatchStillRejects pins the
// rejection path: an action that doesn't match exactly OR after
// normalisation still returns InvalidArgs. SAP is conservative —
// no edit-distance fuzzy matching, no plural stripping. The user's
// 2026-05-05 design constraint: SAP is a runtime safety net for
// expected drift, not a fuzzy-match layer that hides genuinely
// broken outputs.
func TestDecideExecutor_SAP_NoNormalisedMatchStillRejects(t *testing.T) {
	pub := &recordingPublisher{}
	e := newDecideExecutor(pub)

	res, _ := e.Execute(context.Background(), agentic.ToolCall{
		ID:     "c-sap-3",
		Name:   DecideToolName,
		LoopID: "loop-x",
		Metadata: map[string]any{
			agentic.MetadataKeyDecideActionAllowlist: []any{"fan_out", "synthesize"},
		},
		Arguments: map[string]any{
			"action": "branch_out", // not exact, not normaliseable to allowlist
			"reason": "model drift beyond SAP's reach",
		},
	})

	if res.ErrorKind != agentic.ToolErrorInvalidArgs {
		t.Errorf("ErrorKind = %v, want ToolErrorInvalidArgs", res.ErrorKind)
	}
	if !strings.Contains(res.Error, "branch_out") {
		t.Errorf("error %q should name the rejected action", res.Error)
	}
	if len(pub.triples) != 0 {
		t.Errorf("no triples should publish on rejection; got %d", len(pub.triples))
	}
}

// TestNormaliseActionForSAP pins V1 normalisation rules so a future
// expansion (Levenshtein, plural stripping, prefix elision) is a
// loud test failure, not a silent semantic shift.
func TestNormaliseActionForSAP(t *testing.T) {
	tests := []struct{ in, want string }{
		{"fan_out", "fan_out"},
		{"fan-out", "fan_out"},     // hyphen → underscore
		{"FAN_OUT", "fan_out"},     // case
		{"  fan_out  ", "fan_out"}, // whitespace trim
		{"Fan-Out", "fan_out"},     // case + hyphen
		{"deeply_nested-thing", "deeply_nested_thing"},
		// Things SAP V1 explicitly does NOT normalise:
		{"fanout", "fanout"},         // no implicit space-insertion
		{"fan_outs", "fan_outs"},     // no plural stripping
		{"go_fan_out", "go_fan_out"}, // no prefix elision
	}
	for _, tt := range tests {
		t.Run(tt.in, func(t *testing.T) {
			if got := normaliseActionForSAP(tt.in); got != tt.want {
				t.Errorf("normaliseActionForSAP(%q) = %q, want %q", tt.in, got, tt.want)
			}
		})
	}
}

// TestDecideExecutor_HappyPath verifies a valid decide call emits the
// expected triples on the coordinator's loop entity, returns
// StopLoop=true, and puts the full args in the tool result Content so
// downstream agents can fetch subtopics via read_loop_result. ADR-046
// Phase 1 added a third triple (CoordinatorDecisionSubtopics) on the
// fan_out path so downstream rules can iterate via for_each without an
// out-of-band read of the loop's Result.
func TestDecideExecutor_HappyPath(t *testing.T) {
	pub := &recordingPublisher{}
	e := newDecideExecutor(pub)

	res, err := e.Execute(context.Background(), agentic.ToolCall{
		ID:     "c1",
		Name:   DecideToolName,
		LoopID: "loop-abc",
		Arguments: map[string]any{
			"action":    "fan_out",
			"reason":    "three separable subtopics identified",
			"subtopics": []any{"alpha", "beta", "gamma"},
		},
	})
	if err != nil {
		t.Fatalf("unexpected err: %v", err)
	}
	if res.Error != "" {
		t.Fatalf("tool error: %s", res.Error)
	}
	if !res.StopLoop {
		t.Errorf("expected StopLoop=true")
	}

	// next_action + reason + subtopics — three triples on fan_out with
	// non-empty subtopics. The third (subtopics) shipped in ADR-046
	// Phase 1; the test pre-dates it.
	if len(pub.triples) != 3 {
		t.Fatalf("expected 3 triples (next_action + reason + subtopics), got %d", len(pub.triples))
	}

	wantSubject := "acme.test.agent.agentic-loop.execution.loop-abc"
	for _, tr := range pub.triples {
		if tr.Subject != wantSubject {
			t.Errorf("triple subject = %q, want %q", tr.Subject, wantSubject)
		}
	}

	facts := map[string]any{}
	for _, tr := range pub.triples {
		facts[tr.Predicate] = tr.Object
	}
	if got := facts[agvocab.CoordinatorNextAction]; got != "fan_out" {
		t.Errorf("CoordinatorNextAction = %v, want fan_out", got)
	}
	if got := facts[agvocab.CoordinatorDecisionReason]; got != "three separable subtopics identified" {
		t.Errorf("CoordinatorDecisionReason = %v, want reason text", got)
	}

	// Content is JSON of the full args — downstream consumers parse to
	// get subtopics etc.
	var payload decideArgs
	if err := json.Unmarshal([]byte(res.Content), &payload); err != nil {
		t.Fatalf("unmarshal result content: %v", err)
	}
	if payload.Action != "fan_out" {
		t.Errorf("payload action = %q, want fan_out", payload.Action)
	}
	if len(payload.Subtopics) != 3 {
		t.Errorf("payload subtopics = %v, want 3 entries", payload.Subtopics)
	}

	// Metadata surfaces handy summaries for dashboards.
	if got := res.Metadata["action"]; got != "fan_out" {
		t.Errorf("Metadata[action] = %v, want fan_out", got)
	}
	if got := res.Metadata["subtopic_count"]; got != 3 {
		t.Errorf("Metadata[subtopic_count] = %v, want 3", got)
	}
}

// TestDecideExecutor_MissingAction verifies that a call missing the
// required action surfaces InvalidArgs and does NOT publish any triples.
// The framework retry policy (Layer 1 tool_retries) then kicks in and
// gives the model another chance to hit the schema.
func TestDecideExecutor_MissingAction(t *testing.T) {
	pub := &recordingPublisher{}
	e := newDecideExecutor(pub)

	res, err := e.Execute(context.Background(), agentic.ToolCall{
		ID:     "c1",
		Name:   DecideToolName,
		LoopID: "loop-abc",
		Arguments: map[string]any{
			"reason": "forgot to include action",
		},
	})
	if err != nil {
		t.Fatalf("unexpected wrapped err: %v", err)
	}
	if res.ErrorKind != agentic.ToolErrorInvalidArgs {
		t.Errorf("ErrorKind = %v, want ToolErrorInvalidArgs", res.ErrorKind)
	}
	if len(pub.triples) != 0 {
		t.Errorf("no triples should be published on invalid args, got %d", len(pub.triples))
	}
	if res.StopLoop {
		t.Errorf("StopLoop should be false on invalid args — let the loop iterate")
	}
}

// TestDecideExecutor_MissingReason covers the other required field.
func TestDecideExecutor_MissingReason(t *testing.T) {
	pub := &recordingPublisher{}
	e := newDecideExecutor(pub)

	res, _ := e.Execute(context.Background(), agentic.ToolCall{
		ID:     "c1",
		Name:   DecideToolName,
		LoopID: "loop-abc",
		Arguments: map[string]any{
			"action": "done",
		},
	})
	if res.ErrorKind != agentic.ToolErrorInvalidArgs {
		t.Errorf("ErrorKind = %v, want ToolErrorInvalidArgs", res.ErrorKind)
	}
}

// TestDecideExecutor_SubtopicsWrongType verifies that non-string items in
// the subtopics array fail validation cleanly.
func TestDecideExecutor_SubtopicsWrongType(t *testing.T) {
	pub := &recordingPublisher{}
	e := newDecideExecutor(pub)

	res, _ := e.Execute(context.Background(), agentic.ToolCall{
		ID:     "c1",
		Name:   DecideToolName,
		LoopID: "loop-abc",
		Arguments: map[string]any{
			"action":    "fan_out",
			"reason":    "test",
			"subtopics": []any{"ok", 42},
		},
	})
	if res.ErrorKind != agentic.ToolErrorInvalidArgs {
		t.Errorf("ErrorKind = %v, want ToolErrorInvalidArgs", res.ErrorKind)
	}
}

// TestDecideExecutor_MissingLoopID verifies that a malformed call with no
// loop_id surfaces an internal error rather than silently publishing to a
// malformed entity ID.
func TestDecideExecutor_MissingLoopID(t *testing.T) {
	pub := &recordingPublisher{}
	e := newDecideExecutor(pub)

	res, err := e.Execute(context.Background(), agentic.ToolCall{
		ID:   "c1",
		Name: DecideToolName,
		Arguments: map[string]any{
			"action": "done",
			"reason": "ok",
		},
	})
	if err == nil {
		t.Errorf("expected wrapped err for missing loop_id")
	}
	if res.ErrorKind != agentic.ToolErrorInternal {
		t.Errorf("ErrorKind = %v, want ToolErrorInternal", res.ErrorKind)
	}
	if len(pub.triples) != 0 {
		t.Errorf("no triples should be published without a loop_id")
	}
}

// TestDecideExecutor_PublisherFailure verifies that a failure during
// triple publication surfaces ToolErrorNetwork (NATS is a transport-level
// failure) so the retry policy can react, and the loop is NOT terminated
// (StopLoop stays false) so the model sees the failure in the next iteration.
func TestDecideExecutor_PublisherFailure(t *testing.T) {
	pub := &recordingPublisher{err: errors.New("nats broken")}
	e := newDecideExecutor(pub)

	res, err := e.Execute(context.Background(), agentic.ToolCall{
		ID:     "c1",
		Name:   DecideToolName,
		LoopID: "loop-abc",
		Arguments: map[string]any{
			"action": "done",
			"reason": "ok",
		},
	})
	if err == nil {
		t.Errorf("expected wrapped err for publish failure")
	}
	if res.ErrorKind != agentic.ToolErrorNetwork {
		t.Errorf("ErrorKind = %v, want ToolErrorNetwork", res.ErrorKind)
	}
	if res.StopLoop {
		t.Errorf("StopLoop should stay false when the decision didn't actually land")
	}
}

// TestDecideExecutor_UnknownTool verifies routing protection.
func TestDecideExecutor_UnknownTool(t *testing.T) {
	e := newDecideExecutor(&recordingPublisher{})
	res, err := e.Execute(context.Background(), agentic.ToolCall{
		ID:        "c1",
		Name:      "not_decide",
		LoopID:    "loop-abc",
		Arguments: map[string]any{},
	})
	if err == nil {
		t.Errorf("expected err for unknown tool")
	}
	if res.ErrorKind != agentic.ToolErrorNotFound {
		t.Errorf("ErrorKind = %v, want ToolErrorNotFound", res.ErrorKind)
	}
}

// TestDecideExecutor_OptionalFieldsOmitted verifies a minimal valid call
// (action + reason only) publishes both triples and returns cleanly with
// empty Metadata fields for the optional fields.
func TestDecideExecutor_OptionalFieldsOmitted(t *testing.T) {
	pub := &recordingPublisher{}
	e := newDecideExecutor(pub)

	res, err := e.Execute(context.Background(), agentic.ToolCall{
		ID:     "c1",
		Name:   DecideToolName,
		LoopID: "loop-abc",
		Arguments: map[string]any{
			"action": "done",
			"reason": "research complete",
		},
	})
	if err != nil {
		t.Fatalf("unexpected err: %v", err)
	}
	if len(pub.triples) != 2 {
		t.Errorf("expected 2 triples, got %d", len(pub.triples))
	}
	if got := res.Metadata["subtopic_count"]; got != 0 {
		t.Errorf("subtopic_count = %v, want 0", got)
	}
	if got := res.Metadata["has_retry_hint"]; got != false {
		t.Errorf("has_retry_hint = %v, want false", got)
	}
}

// TestDecideExecutor_LoopEntityIDFormat double-checks the subject ID uses
// the same 6-part shape graph_writer and rule actions use. Changes to the
// loop-execution entity ID format should break this test loudly so it's
// clear the whole rule chain needs to be updated.
func TestDecideExecutor_LoopEntityIDFormat(t *testing.T) {
	pub := &recordingPublisher{}
	e := NewDecideExecutor(pub, types.PlatformMeta{Org: "c360", Platform: "deep-research-001"}, nil)

	_, _ = e.Execute(context.Background(), agentic.ToolCall{
		ID:     "c1",
		Name:   DecideToolName,
		LoopID: "xyz",
		Arguments: map[string]any{
			"action": "done",
			"reason": "ok",
		},
	})

	want := "c360.deep-research-001.agent.agentic-loop.execution.xyz"
	if got := pub.triples[0].Subject; got != want {
		t.Errorf("subject = %q, want %q", got, want)
	}
	// Also verify against the shared helper so if it ever changes this
	// test follows.
	if got := pub.triples[0].Subject; got != agentic.LoopExecutionEntityID("c360", "deep-research-001", "xyz") {
		t.Errorf("subject drift: %q not from LoopExecutionEntityID helper", got)
	}
	// Source attribution is preserved so operators can distinguish
	// coordinator decisions from other triple emitters in the graph.
	if got, want := pub.triples[0].Source, decideToolSource; got != want {
		t.Errorf("source = %q, want %q", got, want)
	}
	// Sanity: no fmt.Sprint-shaped junk in the subject.
	_ = fmt.Sprintf("%s", want)
}

// --- #134 / ADR-046 Phase 1: subtopics-triple emission tests ---

// TestDecideExecutor_StampsSubtopicsTriple verifies the new ADR-046
// Phase 1 emission: when args.Subtopics is non-empty, the decide tool
// stamps coordinator.decision.subtopics as a JSON-encoded []string
// triple alongside the canonical next_action + reason triples. Lets
// downstream rules iterate via for_each without out-of-band read of
// the loop's Result JSON.
func TestDecideExecutor_StampsSubtopicsTriple(t *testing.T) {
	pub := &recordingPublisher{}
	e := newDecideExecutor(pub)

	_, _ = e.Execute(context.Background(), agentic.ToolCall{
		ID:     "c1",
		Name:   DecideToolName,
		LoopID: "loop-abc",
		Arguments: map[string]any{
			"action":    "fan_out",
			"reason":    "researcher produced three subtopics",
			"subtopics": []any{"hydraulics", "pneumatics", "electrics"},
		},
	})

	// Expect three triples on the AtomicBatch path: next_action, reason,
	// subtopics. (Allowlist not set, so no SAP-coerced triple.)
	if got := len(pub.triples); got != 3 {
		t.Fatalf("triple count = %d, want 3 (next_action + reason + subtopics)", got)
	}

	var subtopicsTriple *message.Triple
	for i := range pub.triples {
		if pub.triples[i].Predicate == agvocab.CoordinatorDecisionSubtopics {
			subtopicsTriple = &pub.triples[i]
		}
	}
	if subtopicsTriple == nil {
		t.Fatalf("no CoordinatorDecisionSubtopics triple emitted; got predicates: %v", tripPredicates(pub.triples))
	}

	// The Object must be a JSON-encoded []string so the for_each
	// substitution layer can parse it back.
	encoded, ok := subtopicsTriple.Object.(string)
	if !ok {
		t.Fatalf("Subtopics triple Object = %T, want string (JSON-encoded)", subtopicsTriple.Object)
	}
	var decoded []string
	if err := json.Unmarshal([]byte(encoded), &decoded); err != nil {
		t.Fatalf("Subtopics triple Object failed to parse as []string: %v (object: %q)", err, encoded)
	}
	wantSubtopics := []string{"hydraulics", "pneumatics", "electrics"}
	if len(decoded) != len(wantSubtopics) {
		t.Errorf("decoded subtopics len = %d, want %d", len(decoded), len(wantSubtopics))
	}
	for i := range wantSubtopics {
		if i < len(decoded) && decoded[i] != wantSubtopics[i] {
			t.Errorf("subtopic[%d] = %q, want %q", i, decoded[i], wantSubtopics[i])
		}
	}

	// Source attribution preserved.
	if got, want := subtopicsTriple.Source, decideToolSource; got != want {
		t.Errorf("source = %q, want %q", got, want)
	}
}

// TestDecideExecutor_OmitsSubtopicsTripleWhenEmpty verifies back-compat:
// a decide call without subtopics (action=done, retry, etc.) emits ONLY
// the next_action + reason triples. The CoordinatorDecisionSubtopics
// triple is opt-in via args.Subtopics being non-empty.
func TestDecideExecutor_OmitsSubtopicsTripleWhenEmpty(t *testing.T) {
	pub := &recordingPublisher{}
	e := newDecideExecutor(pub)

	_, _ = e.Execute(context.Background(), agentic.ToolCall{
		ID:     "c1",
		Name:   DecideToolName,
		LoopID: "loop-abc",
		Arguments: map[string]any{
			"action": "done",
			"reason": "all subtasks complete",
			// subtopics deliberately omitted
		},
	})

	for _, tr := range pub.triples {
		if tr.Predicate == agvocab.CoordinatorDecisionSubtopics {
			t.Errorf("CoordinatorDecisionSubtopics triple should not emit when args.Subtopics is empty")
		}
	}
}

// tripPredicates is a tiny helper to format the predicate set from a
// triple slice for assertion error messages.
func tripPredicates(triples []message.Triple) []string {
	out := make([]string, len(triples))
	for i, tr := range triples {
		out[i] = tr.Predicate
	}
	return out
}

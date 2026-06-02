package researchroute

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"strings"
	"sync"
	"testing"

	"github.com/c360studio/semstreams/agentic/research"
)

// fakeRouter returns canned content / error per scenario. Records
// the system+user prompts it was called with so prompt-shape tests
// can verify wiring without running the LLM.
type fakeRouter struct {
	mu         sync.Mutex
	called     bool
	gotSystem  string
	gotUser    string
	gotMaxToks int
	content    string
	reason     string
	err        error
}

func (f *fakeRouter) Route(_ context.Context, system, user string, maxTokens int) (string, string, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.called = true
	f.gotSystem = system
	f.gotUser = user
	f.gotMaxToks = maxTokens
	return f.content, f.reason, f.err
}

func TestExtractLoopIDFromSubject(t *testing.T) {
	cases := []struct {
		name    string
		subject string
		want    string
	}{
		{"happy", "component.route_search.loop-123", "loop-123"},
		{"empty suffix", "component.route_search.", ""},
		{"wrong prefix", "component.nl_classify.loop-123", ""},
		{"random", "some.other.subject", ""},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := extractLoopIDFromSubject(c.subject); got != c.want {
				t.Errorf("extractLoopIDFromSubject(%q) = %q, want %q", c.subject, got, c.want)
			}
		})
	}
}

// --- extractJSON ---

func TestExtractJSON(t *testing.T) {
	cases := []struct {
		name    string
		input   string
		want    string
		wantErr bool
	}{
		{
			name:  "bare object",
			input: `{"action":"synthesize_directly","args":{}}`,
			want:  `{"action":"synthesize_directly","args":{}}`,
		},
		{
			name:  "markdown fenced",
			input: "```json\n{\"action\":\"retighten\",\"args\":{\"topic\":\"x\"}}\n```",
			want:  `{"action":"retighten","args":{"topic":"x"}}`,
		},
		{
			name:  "prose preface",
			input: `Sure — here is the decision:` + "\n" + `{"action":"walk_seeds","args":{"seeds":[{"ref":"x","ref_type":"name"}]}}` + "\nLet me know if you need more.",
			want:  `{"action":"walk_seeds","args":{"seeds":[{"ref":"x","ref_type":"name"}]}}`,
		},
		{
			name:  "nested braces preserved",
			input: `{"action":"decompose","args":{"axes":["a","b"],"focus":"x","scope":"narrow"},"rationale":"y"}`,
			want:  `{"action":"decompose","args":{"axes":["a","b"],"focus":"x","scope":"narrow"},"rationale":"y"}`,
		},
		{
			name:    "no object",
			input:   "I cannot answer this.",
			wantErr: true,
		},
		{
			name:    "unbalanced",
			input:   `{"action":"x"`,
			wantErr: true,
		},
		{
			// Regression: brace inside a string value must NOT truncate
			// the extraction. Surfaces as soon as the model writes a
			// rationale like "axes spanning {time, entity_type}".
			name:  "brace inside string value",
			input: `{"action":"synthesize_directly","args":{},"rationale":"axes spanning {time, entity_type}"}`,
			want:  `{"action":"synthesize_directly","args":{},"rationale":"axes spanning {time, entity_type}"}`,
		},
		{
			// Both braces inside a string value — symmetric form of the
			// above. Catches a walker that toggled string-context only
			// on `{` (instead of `"`).
			name:  "both braces inside string value",
			input: `{"action":"x","args":{},"rationale":"oh no a } in prose and another { for good measure"}`,
			want:  `{"action":"x","args":{},"rationale":"oh no a } in prose and another { for good measure"}`,
		},
		{
			// Escaped quote inside a string value must NOT exit
			// string-context. Catches a walker that treats every `"`
			// as a toggle without tracking the escape.
			name:  "escaped quote does not exit string",
			input: `{"action":"retighten","args":{"topic":"the \"quoted\" } term"}}`,
			want:  `{"action":"retighten","args":{"topic":"the \"quoted\" } term"}}`,
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got, err := extractJSON(c.input)
			if c.wantErr {
				if err == nil {
					t.Errorf("extractJSON: want error, got %q", got)
				}
				return
			}
			if err != nil {
				t.Fatalf("extractJSON: unexpected error %v", err)
			}
			if string(got) != c.want {
				t.Errorf("extractJSON = %q, want %q", got, c.want)
			}
		})
	}
}

// --- routeDecision happy paths ---

func TestRouteDecision_SynthesizeDirectly(t *testing.T) {
	router := &fakeRouter{
		content: `{"action":"synthesize_directly","args":{},"rationale":"candidate set is already sufficient"}`,
	}
	intent := &research.Intent{Topic: "drone-001 status"}
	out := &research.ClassifierOutput{
		Topic:      "drone-001 status",
		Tier:       "0",
		Candidates: []research.Candidate{{EntityID: "drone-001", Label: "Drone 001", Tier: "0", Source: "x"}},
	}

	got, err := routeDecision(context.Background(), router, intent, out, 512, 10, nil)
	if err != nil {
		t.Fatalf("routeDecision: %v", err)
	}
	if got.Action != research.ActionSynthesizeDirectly {
		t.Errorf("action = %q, want %q", got.Action, research.ActionSynthesizeDirectly)
	}
	if !router.called {
		t.Error("router not called")
	}
	if router.gotMaxToks != 512 {
		t.Errorf("max_tokens = %d, want 512", router.gotMaxToks)
	}
	if !strings.Contains(router.gotUser, "drone-001 status") {
		t.Errorf("user prompt missing topic: %q", router.gotUser)
	}
}

func TestRouteDecision_DecomposeHappyPath(t *testing.T) {
	router := &fakeRouter{
		content: `{"action":"decompose","args":{"axes":["entity_type","time"],"focus":"sensor maintenance","scope":"medium"},"rationale":"multi-dim"}`,
	}
	got, err := routeDecision(context.Background(), router,
		&research.Intent{Topic: "x"},
		&research.ClassifierOutput{Topic: "x", Tier: "0"},
		512, 10, nil)
	if err != nil {
		t.Fatalf("routeDecision: %v", err)
	}
	if got.Action != research.ActionDecompose {
		t.Fatalf("action: %q", got.Action)
	}
	args, err := got.ParseDecomposeArgs()
	if err != nil {
		t.Fatalf("ParseDecomposeArgs: %v", err)
	}
	if len(args.Axes) != 2 || args.Focus != "sensor maintenance" || args.Scope != research.DecomposeScopeMedium {
		t.Errorf("decompose args drift: %+v", args)
	}
}

func TestRouteDecision_WalkSeedsHappyPath(t *testing.T) {
	router := &fakeRouter{
		content: `{"action":"walk_seeds","args":{"seeds":[{"ref":"drone-001","ref_type":"name"},{"ref":"0","ref_type":"candidate_index"}]},"rationale":"have starts"}`,
	}
	got, err := routeDecision(context.Background(), router,
		&research.Intent{Topic: "x"},
		&research.ClassifierOutput{Topic: "x", Tier: "0", Candidates: []research.Candidate{{EntityID: "drone-001", Tier: "0", Source: "x"}}},
		512, 10, nil)
	if err != nil {
		t.Fatalf("routeDecision: %v", err)
	}
	args, err := got.ParseWalkSeedsArgs()
	if err != nil {
		t.Fatalf("ParseWalkSeedsArgs: %v", err)
	}
	if len(args.Seeds) != 2 || args.Seeds[0].Ref != "drone-001" {
		t.Errorf("walk_seeds args drift: %+v", args)
	}
}

func TestRouteDecision_RetightenHappyPath(t *testing.T) {
	router := &fakeRouter{
		content: `{"action":"retighten","args":{"topic":"refined","hints":{"k":"v"}},"rationale":"vague"}`,
	}
	got, err := routeDecision(context.Background(), router,
		&research.Intent{Topic: "x"},
		&research.ClassifierOutput{Topic: "x", Tier: "0"},
		512, 10, nil)
	if err != nil {
		t.Fatalf("routeDecision: %v", err)
	}
	args, err := got.ParseRetightenArgs()
	if err != nil {
		t.Fatalf("ParseRetightenArgs: %v", err)
	}
	if args.Topic != "refined" || args.Hints["k"] != "v" {
		t.Errorf("retighten args drift: %+v", args)
	}
}

// --- routeDecision error paths ---

func TestRouteDecision_RejectsRouterError(t *testing.T) {
	router := &fakeRouter{err: errors.New("upstream 503")}
	_, err := routeDecision(context.Background(), router,
		&research.Intent{Topic: "x"},
		&research.ClassifierOutput{Topic: "x"},
		512, 10, nil)
	if err == nil || !strings.Contains(err.Error(), "router call") {
		t.Fatalf("want router-call error, got %v", err)
	}
}

func TestRouteDecision_RejectsEmptyContent(t *testing.T) {
	router := &fakeRouter{content: "   ", reason: "length"}
	_, err := routeDecision(context.Background(), router,
		&research.Intent{Topic: "x"},
		&research.ClassifierOutput{Topic: "x"},
		512, 10, nil)
	if err == nil || !strings.Contains(err.Error(), "empty content") {
		t.Fatalf("want empty-content error, got %v", err)
	}
	if !strings.Contains(err.Error(), "length") {
		t.Errorf("error should include finish_reason: %v", err)
	}
}

func TestRouteDecision_RejectsInvalidAction(t *testing.T) {
	router := &fakeRouter{content: `{"action":"bogus","args":{}}`}
	_, err := routeDecision(context.Background(), router,
		&research.Intent{Topic: "x"},
		&research.ClassifierOutput{Topic: "x"},
		512, 10, nil)
	if err == nil {
		t.Fatal("want invalid-action error, got nil")
	}
	// UnmarshalJSON enforces the enum at decode, so the error
	// surfaces as a decode failure, not as the post-decode Validate
	// branch. Either is fine — assert the bogus action shows up.
	if !strings.Contains(err.Error(), "bogus") {
		t.Errorf("error should mention bogus action: %v", err)
	}
}

func TestRouteDecision_RejectsBadDecomposeArgs(t *testing.T) {
	// Missing focus + missing axes — decode succeeds (Args is loose
	// at wire level) but per-action validation rejects.
	router := &fakeRouter{content: `{"action":"decompose","args":{"scope":"narrow"}}`}
	_, err := routeDecision(context.Background(), router,
		&research.Intent{Topic: "x"},
		&research.ClassifierOutput{Topic: "x"},
		512, 10, nil)
	if err == nil || !strings.Contains(err.Error(), "invalid decompose args") {
		t.Fatalf("want invalid-decompose-args error, got %v", err)
	}
}

func TestRouteDecision_RejectsBadWalkSeedsArgs(t *testing.T) {
	router := &fakeRouter{content: `{"action":"walk_seeds","args":{"seeds":[{"ref":"x","ref_type":"full_id"}]}}`}
	_, err := routeDecision(context.Background(), router,
		&research.Intent{Topic: "x"},
		&research.ClassifierOutput{Topic: "x"},
		512, 10, nil)
	if err == nil || !strings.Contains(err.Error(), "invalid walk_seeds args") {
		t.Fatalf("want invalid-walk_seeds-args error, got %v", err)
	}
}

func TestRouteDecision_RejectsBadRetightenArgs(t *testing.T) {
	router := &fakeRouter{content: `{"action":"retighten","args":{"hints":{"k":"v"}}}`}
	_, err := routeDecision(context.Background(), router,
		&research.Intent{Topic: "x"},
		&research.ClassifierOutput{Topic: "x"},
		512, 10, nil)
	if err == nil || !strings.Contains(err.Error(), "invalid retighten args") {
		t.Fatalf("want invalid-retighten-args error, got %v", err)
	}
}

func TestRouteDecision_SynthesizeDirectlyToleratesExtraArgs(t *testing.T) {
	// Frontier models occasionally emit empty args even when told
	// not to. SynthesizeDirectly tolerates this — downstream
	// synthesizer ignores Args for this action. The defense-in-depth
	// Warn for this case is covered in
	// TestRouteDecision_SynthesizeDirectlyWarnsOnNonEmptyArgs.
	router := &fakeRouter{content: `{"action":"synthesize_directly","args":{"extra":"ignored"},"rationale":"x"}`}
	got, err := routeDecision(context.Background(), router,
		&research.Intent{Topic: "x"},
		&research.ClassifierOutput{Topic: "x"},
		512, 10, discardLogger())
	if err != nil {
		t.Fatalf("routeDecision should tolerate extra args on synthesize_directly: %v", err)
	}
	if got.Action != research.ActionSynthesizeDirectly {
		t.Errorf("action drift: %q", got.Action)
	}
}

// discardLogger returns a logger that swallows output; used by
// routeDecision tests that intentionally exercise the
// non-empty-args path but don't want the Warn line in test stderr.
func discardLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}

// TestRouteDecision_SynthesizeDirectlyWarnsOnNonEmptyArgs pins the
// defense-in-depth Warn (post-PR-#187 review follow-up): when the
// model picks synthesize_directly but emits args that would have
// been valid for a different action (likely action-confusion), we
// keep routing but log a Warn so operator trajectory review catches
// it. Otherwise the downstream synthesizer silently loses the
// intent.
func TestRouteDecision_SynthesizeDirectlyWarnsOnNonEmptyArgs(t *testing.T) {
	var logBuf bytes.Buffer
	logger := slog.New(slog.NewTextHandler(&logBuf, &slog.HandlerOptions{Level: slog.LevelWarn}))

	router := &fakeRouter{content: `{"action":"synthesize_directly","args":{"seeds":[{"ref":"x","ref_type":"name"}]},"rationale":"confused"}`}
	got, err := routeDecision(context.Background(), router,
		&research.Intent{Topic: "x"},
		&research.ClassifierOutput{Topic: "x"},
		512, 10, logger)
	if err != nil {
		t.Fatalf("routeDecision: %v", err)
	}
	if got.Action != research.ActionSynthesizeDirectly {
		t.Errorf("action drift: %q", got.Action)
	}
	logged := logBuf.String()
	if !strings.Contains(logged, "synthesize_directly emitted with non-empty args") {
		t.Errorf("expected Warn about non-empty args, log was:\n%s", logged)
	}
	if !strings.Contains(logged, "args_count=1") {
		t.Errorf("Warn should include args count, log was:\n%s", logged)
	}
}

// TestRouteDecision_SynthesizeDirectlyEmptyArgsDoesNotWarn pins
// the negative case: an empty args map (the well-behaved synthesis
// path) must not Warn — otherwise log churn would mask the
// confusion case the Warn exists to catch.
func TestRouteDecision_SynthesizeDirectlyEmptyArgsDoesNotWarn(t *testing.T) {
	var logBuf bytes.Buffer
	logger := slog.New(slog.NewTextHandler(&logBuf, &slog.HandlerOptions{Level: slog.LevelWarn}))

	router := &fakeRouter{content: `{"action":"synthesize_directly","args":{},"rationale":"all good"}`}
	if _, err := routeDecision(context.Background(), router,
		&research.Intent{Topic: "x"},
		&research.ClassifierOutput{Topic: "x"},
		512, 10, logger); err != nil {
		t.Fatalf("routeDecision: %v", err)
	}
	if strings.Contains(logBuf.String(), "non-empty args") {
		t.Errorf("empty args should NOT produce a Warn, log was:\n%s", logBuf.String())
	}
}

// --- prompt wiring ---

func TestBuildUserPrompt_IncludesTopicAndCandidates(t *testing.T) {
	intent := &research.Intent{Topic: "sensor maintenance events", Hints: map[string]string{"entity_type": "sensor"}}
	out := &research.ClassifierOutput{
		Topic:      "sensor maintenance events",
		Tier:       "1",
		Confidence: 0.72,
		Hints:      map[string]any{"k": "v"},
		Candidates: []research.Candidate{
			{EntityID: "sensor-001", Label: "Sensor 001", Type: "sensor", Relevance: 0.9, Tier: "0", Source: "x"},
			{EntityID: "sensor-002", Label: "Sensor 002", Type: "sensor", Relevance: 0.5, Tier: "0", Source: "x"},
		},
	}
	got := buildUserPrompt(intent, out, 10)
	// Substring assertions — not a golden snapshot, to avoid
	// over-pinning the exact wording (prompt iteration is normal).
	for _, want := range []string{
		"sensor maintenance events",
		"entity_type: sensor",
		"Classifier tier: 1",
		"Classifier confidence: 0.72",
		"sensor-001",
		"sensor-002",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("prompt missing %q\nfull:\n%s", want, got)
		}
	}
}

func TestBuildUserPrompt_CapsCandidateCount(t *testing.T) {
	// Cap at 2 — verify only 2 candidates render even when 5 are
	// supplied, and the top-by-relevance ordering is honored.
	out := &research.ClassifierOutput{
		Topic: "x",
		Tier:  "0",
		Candidates: []research.Candidate{
			{EntityID: "low", Relevance: 0.1, Tier: "0", Source: "x"},
			{EntityID: "high", Relevance: 0.9, Tier: "0", Source: "x"},
			{EntityID: "mid", Relevance: 0.5, Tier: "0", Source: "x"},
			{EntityID: "low2", Relevance: 0.2, Tier: "0", Source: "x"},
			{EntityID: "mid2", Relevance: 0.4, Tier: "0", Source: "x"},
		},
	}
	got := buildUserPrompt(&research.Intent{Topic: "x"}, out, 2)
	if !strings.Contains(got, "high") {
		t.Errorf("top-relevance 'high' missing from capped prompt:\n%s", got)
	}
	if !strings.Contains(got, "mid") {
		t.Errorf("second-relevance 'mid' missing from capped prompt:\n%s", got)
	}
	if strings.Contains(got, "low2") || strings.Contains(got, "mid2") {
		t.Errorf("low-relevance candidates leaked past cap:\n%s", got)
	}
}

func TestBuildUserPrompt_NoCandidatesHasGuidance(t *testing.T) {
	// Empty candidate set must produce a usable prompt — the model
	// needs to know "no candidates, consider retighten or
	// synthesize_directly" rather than an empty list section.
	got := buildUserPrompt(
		&research.Intent{Topic: "x"},
		&research.ClassifierOutput{Topic: "x", Tier: "0"},
		10)
	if !strings.Contains(got, "none") {
		t.Errorf("empty candidate prompt should signal 'none': %s", got)
	}
}

// --- buildSystemPrompt sanity ---

func TestBuildSystemPrompt_IncludesAllFourActions(t *testing.T) {
	got := buildSystemPrompt()
	for _, action := range []string{
		research.ActionSynthesizeDirectly,
		research.ActionRetighten,
		research.ActionWalkSeeds,
		research.ActionDecompose,
	} {
		if !strings.Contains(got, action) {
			t.Errorf("system prompt missing action %q", action)
		}
	}
}

func TestBuildSystemPrompt_DescribesIntentShapeArgs(t *testing.T) {
	// Verify the prompt actually instructs intent-shaped args
	// (axes/focus/scope for decompose; ref/ref_type for walk_seeds)
	// rather than the old typed-args shapes. Catches regression
	// where someone edits the prompt back to the v1 shapes without
	// updating the ParseArgs helpers.
	got := buildSystemPrompt()
	for _, want := range []string{
		"axes", "focus", "scope",
		"ref", "ref_type", "name", "partial_id", "candidate_index",
		"Do not emit full 6-part IDs",
		"Do not emit typed sub-query objects",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("system prompt missing intent-shape token %q", want)
		}
	}
}

// --- prompt wiring through routeDecision ---

func TestRouteDecision_PassesSystemAndUserPrompts(t *testing.T) {
	router := &fakeRouter{content: `{"action":"synthesize_directly","args":{}}`}
	intent := &research.Intent{Topic: "my topic", Hints: map[string]string{"a": "b"}}
	out := &research.ClassifierOutput{
		Topic: "my topic", Tier: "0",
		Candidates: []research.Candidate{{EntityID: "ent-1", Tier: "0", Source: "x"}},
	}
	_, err := routeDecision(context.Background(), router, intent, out, 512, 10, nil)
	if err != nil {
		t.Fatalf("routeDecision: %v", err)
	}
	if !strings.Contains(router.gotSystem, "synthesize_directly") {
		t.Errorf("system prompt missing action enum: %s", router.gotSystem)
	}
	if !strings.Contains(router.gotUser, "my topic") {
		t.Errorf("user prompt missing topic: %s", router.gotUser)
	}
	if !strings.Contains(router.gotUser, "ent-1") {
		t.Errorf("user prompt missing candidate: %s", router.gotUser)
	}
}

// --- envelope round-trip sanity ---

// TestDecisionMarshalUnmarshalsThroughEnvelope verifies the
// production marshal path (NewBaseMessage → MarshalJSON) keeps the
// per-action Args shape intact, so the snapshot/trigger writes in
// handleMessage don't drop fields silently.
func TestDecisionMarshalUnmarshalsThroughEnvelope(t *testing.T) {
	d := &research.RouteDecision{
		Action: research.ActionDecompose,
		Args: map[string]any{
			"axes":  []any{"entity_type", "time"},
			"focus": "x",
			"scope": research.DecomposeScopeNarrow,
		},
	}
	raw, err := json.Marshal(d)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var back research.RouteDecision
	if err := json.Unmarshal(raw, &back); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	args, err := back.ParseDecomposeArgs()
	if err != nil {
		t.Fatalf("ParseDecomposeArgs after round-trip: %v", err)
	}
	if len(args.Axes) != 2 || args.Focus != "x" {
		t.Errorf("round-trip lost fields: %+v", args)
	}
}

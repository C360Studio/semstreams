package researchassess

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"strings"
	"sync"
	"testing"

	"github.com/c360studio/semstreams/agentic/research"
	"github.com/c360studio/semstreams/pkg/fusion"
)

// fakeAssessor returns canned content / error per scenario. Records
// the system+user prompts it was called with so prompt-shape tests
// can verify wiring without running the LLM.
type fakeAssessor struct {
	mu         sync.Mutex
	called     bool
	gotSystem  string
	gotUser    string
	gotMaxToks int
	content    string
	reason     string
	err        error
}

func (f *fakeAssessor) Assess(_ context.Context, system, user string, maxTokens int) (string, string, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.called = true
	f.gotSystem = system
	f.gotUser = user
	f.gotMaxToks = maxTokens
	return f.content, f.reason, f.err
}

func quietLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}

func TestExtractLoopIDFromSubject(t *testing.T) {
	cases := []struct {
		name    string
		subject string
		want    string
	}{
		{"happy", "component.assess_sufficiency.loop-abc", "loop-abc"},
		{"empty suffix", "component.assess_sufficiency.", ""},
		{"wrong prefix", "component.route_search.loop-abc", ""},
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

// --- assessSufficiency happy paths ---

func TestAssessSufficiency_SufficientPath(t *testing.T) {
	a := &fakeAssessor{
		content: `{"sufficient":true,"rationale":"evidence covers all named actors","confidence":0.82}`,
	}
	intent := &research.Intent{Topic: "test.research.graph.assess.entity.drone-001 maintenance events"}
	exec := &research.ExecutionOutput{
		Topic:  "test.research.graph.assess.entity.drone-001 maintenance events",
		Action: research.ActionWalkSeeds,
		Evidence: []fusion.Evidence{
			{EntityID: "test.research.graph.assess.entity.drone-001", Tier: "0", Source: "walk_seeds.entity_state", Score: 0.9, SnippetText: "Drone 001 maintenance window 14:32Z"},
		},
	}
	got, err := assessSufficiency(context.Background(), a, intent, exec, 1024, 20, 280, quietLogger())
	if err != nil {
		t.Fatalf("assessSufficiency: %v", err)
	}
	if !got.Sufficient {
		t.Error("Sufficient = false, want true")
	}
	if got.Topic != intent.Topic {
		t.Errorf("topic = %q, want %q", got.Topic, intent.Topic)
	}
	if got.EvidenceCount != 1 {
		t.Errorf("EvidenceCount = %d, want 1 (echoes len(exec.Evidence))", got.EvidenceCount)
	}
	if got.Confidence != 0.82 {
		t.Errorf("Confidence = %v, want 0.82", got.Confidence)
	}
	if !a.called {
		t.Error("assessor not called")
	}
	if a.gotMaxToks != 1024 {
		t.Errorf("maxTokens = %d, want 1024", a.gotMaxToks)
	}
	if !strings.Contains(a.gotUser, "test.research.graph.assess.entity.drone-001 maintenance events") {
		t.Errorf("user prompt missing topic: %q", a.gotUser)
	}
}

func TestAssessSufficiency_RefinePath(t *testing.T) {
	a := &fakeAssessor{
		content: `{"sufficient":false,"refined_queries":["fleet-wide drone status","battery alerts last 24h"],"rationale":"topic spans fleet but evidence is test.research.graph.assess.entity.drone-001 only","confidence":0.4}`,
	}
	intent := &research.Intent{Topic: "drone status across the fleet"}
	exec := &research.ExecutionOutput{
		Topic:    "drone status across the fleet",
		Action:   research.ActionWalkSeeds,
		Evidence: []fusion.Evidence{{EntityID: "test.research.graph.assess.entity.drone-001", Tier: "0", Source: "x"}},
	}
	got, err := assessSufficiency(context.Background(), a, intent, exec, 1024, 20, 280, quietLogger())
	if err != nil {
		t.Fatalf("assessSufficiency: %v", err)
	}
	if got.Sufficient {
		t.Error("Sufficient = true, want false")
	}
	if len(got.RefinedQueries) != 2 {
		t.Fatalf("RefinedQueries len = %d, want 2", len(got.RefinedQueries))
	}
	if got.RefinedQueries[0] != "fleet-wide drone status" {
		t.Errorf("RefinedQueries[0] drift: %q", got.RefinedQueries[0])
	}
}

func TestAssessSufficiency_DropsEmptyRefinedQueries(t *testing.T) {
	a := &fakeAssessor{
		content: `{"sufficient":false,"refined_queries":["valid","","   ","also valid"]}`,
	}
	got, err := assessSufficiency(context.Background(), a,
		&research.Intent{Topic: "x"}, &research.ExecutionOutput{Topic: "x", Action: research.ActionDecompose},
		1024, 20, 280, quietLogger())
	if err != nil {
		t.Fatalf("assessSufficiency: %v", err)
	}
	if len(got.RefinedQueries) != 2 {
		t.Fatalf("RefinedQueries after clean = %d, want 2 (empty + whitespace dropped)", len(got.RefinedQueries))
	}
	if got.RefinedQueries[0] != "valid" || got.RefinedQueries[1] != "also valid" {
		t.Errorf("RefinedQueries content drift: %v", got.RefinedQueries)
	}
}

func TestAssessSufficiency_ClampsConfidence(t *testing.T) {
	cases := []struct {
		name string
		raw  string
		want float64
	}{
		{"over one", `{"sufficient":true,"confidence":1.5}`, 1.0},
		{"negative", `{"sufficient":false,"refined_queries":["x"],"confidence":-0.1}`, 0.0},
		{"valid mid", `{"sufficient":true,"confidence":0.5}`, 0.5},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			a := &fakeAssessor{content: c.raw}
			got, err := assessSufficiency(context.Background(), a,
				&research.Intent{Topic: "x"}, &research.ExecutionOutput{Topic: "x", Action: research.ActionDecompose},
				1024, 20, 280, quietLogger())
			if err != nil {
				t.Fatalf("assessSufficiency: %v", err)
			}
			if got.Confidence != c.want {
				t.Errorf("Confidence = %v, want %v", got.Confidence, c.want)
			}
		})
	}
}

func TestAssessSufficiency_PropagatesDegradedFromExec(t *testing.T) {
	a := &fakeAssessor{
		content: `{"sufficient":false,"refined_queries":["x"],"rationale":"degraded retrieval"}`,
	}
	exec := &research.ExecutionOutput{
		Topic:          "x",
		Action:         research.ActionDecompose,
		Degraded:       true,
		DegradedReason: "spatial axis deferred to Phase 2",
	}
	got, err := assessSufficiency(context.Background(), a,
		&research.Intent{Topic: "x"}, exec, 1024, 20, 280, quietLogger())
	if err != nil {
		t.Fatalf("assessSufficiency: %v", err)
	}
	if !got.Degraded {
		t.Error("Degraded should propagate from upstream ExecutionOutput")
	}
	if got.DegradedReason != exec.DegradedReason {
		t.Errorf("DegradedReason drift: got %q, want %q", got.DegradedReason, exec.DegradedReason)
	}
}

// --- assessSufficiency error paths ---

func TestAssessSufficiency_RejectsAssessorError(t *testing.T) {
	a := &fakeAssessor{err: errors.New("upstream 503")}
	_, err := assessSufficiency(context.Background(), a,
		&research.Intent{Topic: "x"}, &research.ExecutionOutput{Topic: "x", Action: research.ActionDecompose},
		1024, 20, 280, quietLogger())
	if err == nil || !strings.Contains(err.Error(), "assessor call") {
		t.Fatalf("want assessor-call error, got %v", err)
	}
}

func TestAssessSufficiency_RejectsEmptyContent(t *testing.T) {
	a := &fakeAssessor{content: "   ", reason: "length"}
	_, err := assessSufficiency(context.Background(), a,
		&research.Intent{Topic: "x"}, &research.ExecutionOutput{Topic: "x", Action: research.ActionDecompose},
		1024, 20, 280, quietLogger())
	if err == nil || !strings.Contains(err.Error(), "empty content") {
		t.Fatalf("want empty-content error, got %v", err)
	}
	if !strings.Contains(err.Error(), "length") {
		t.Errorf("error should include finish_reason: %v", err)
	}
}

func TestAssessSufficiency_RejectsMalformedJSON(t *testing.T) {
	a := &fakeAssessor{content: `I cannot make a decision today.`}
	_, err := assessSufficiency(context.Background(), a,
		&research.Intent{Topic: "x"}, &research.ExecutionOutput{Topic: "x", Action: research.ActionDecompose},
		1024, 20, 280, quietLogger())
	if err == nil || !strings.Contains(err.Error(), "extract JSON") {
		t.Fatalf("want extract-JSON error, got %v", err)
	}
}

func TestAssessSufficiency_NilGuards(t *testing.T) {
	a := &fakeAssessor{content: `{"sufficient":true}`}
	if _, err := assessSufficiency(context.Background(), a, nil, &research.ExecutionOutput{Topic: "x"}, 1024, 20, 280, nil); err == nil {
		t.Error("want intent-nil error")
	}
	if _, err := assessSufficiency(context.Background(), a, &research.Intent{Topic: "x"}, nil, 1024, 20, 280, nil); err == nil {
		t.Error("want exec-nil error")
	}
	if _, err := assessSufficiency(context.Background(), nil, &research.Intent{Topic: "x"}, &research.ExecutionOutput{Topic: "x"}, 1024, 20, 280, nil); err == nil {
		t.Error("want assessor-nil error")
	}
}

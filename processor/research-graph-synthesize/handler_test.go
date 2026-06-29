package researchsynthesize

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

// fakeSynthesizer returns canned content / error per scenario.
type fakeSynthesizer struct {
	mu         sync.Mutex
	called     bool
	gotSystem  string
	gotUser    string
	gotMaxToks int
	content    string
	reason     string
	err        error
}

func (f *fakeSynthesizer) Synthesize(_ context.Context, system, user string, maxTokens int) (string, string, error) {
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
		{"happy", "component.synthesize_answer.loop-77", "loop-77"},
		{"empty suffix", "component.synthesize_answer.", ""},
		{"wrong prefix", "component.assess_sufficiency.loop-77", ""},
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

// --- synthesizeAnswer happy paths ---

func TestSynthesizeAnswer_HappyPath_QuoteBackKeepsValidRefs(t *testing.T) {
	exec := &research.ExecutionOutput{
		Topic:  "drone fleet maintenance events",
		Action: research.ActionWalkSeeds,
		Evidence: []fusion.Evidence{
			{EntityID: "acme.ops.robotics.gcs.drone.001", Tier: "0", Source: "walk_seeds", Score: 0.9, SnippetText: "Drone 001 maintenance window 14:32Z"},
			{EntityID: "acme.ops.robotics.gcs.drone.014", Tier: "1", Source: "bm25", Score: 0.7, ObjectStoreRef: "objstore://research/evidence-014.txt"},
		},
	}
	s := &fakeSynthesizer{
		content: `{"synthesis":"Two drones reported maintenance: drone.001 and drone.014. See refs.","evidence_refs":["acme.ops.robotics.gcs.drone.001","objstore://research/evidence-014.txt"]}`,
	}
	got, err := synthesizeAnswer(context.Background(), s,
		&research.Intent{Topic: "drone fleet maintenance events"}, exec, nil,
		2048, 30, 480, quietLogger())
	if err != nil {
		t.Fatalf("synthesizeAnswer: %v", err)
	}
	if !strings.Contains(got.Synthesis, "drone.001") {
		t.Errorf("synthesis prose drift: %q", got.Synthesis)
	}
	if len(got.Evidence) != 2 {
		t.Fatalf("Evidence len = %d, want 2 (both refs matched, both echoed)", len(got.Evidence))
	}
	// First evidence item should still be acme.ops.robotics.gcs.drone.001
	if got.Evidence[0].EntityID != "acme.ops.robotics.gcs.drone.001" {
		t.Errorf("Evidence[0].EntityID drift: %q", got.Evidence[0].EntityID)
	}
}

func TestSynthesizeAnswer_QuoteBack_StripsFabricatedRefs(t *testing.T) {
	exec := &research.ExecutionOutput{
		Topic:  "x",
		Action: research.ActionWalkSeeds,
		Evidence: []fusion.Evidence{
			{EntityID: "real-1", Tier: "0", Source: "x"},
			{EntityID: "real-2", Tier: "0", Source: "x"},
		},
	}
	s := &fakeSynthesizer{
		content: `{"synthesis":"answer mentions real-1 and fake-99","evidence_refs":["real-1","fake-99","also-fake","real-2"]}`,
	}
	got, err := synthesizeAnswer(context.Background(), s,
		&research.Intent{Topic: "x"}, exec, nil,
		2048, 30, 480, quietLogger())
	if err != nil {
		t.Fatalf("synthesizeAnswer: %v", err)
	}
	if len(got.Evidence) != 2 {
		t.Fatalf("Evidence len = %d, want 2 (real-1 + real-2 kept; fakes stripped)", len(got.Evidence))
	}
	ids := []string{got.Evidence[0].EntityID, got.Evidence[1].EntityID}
	if ids[0] != "real-1" || ids[1] != "real-2" {
		t.Errorf("kept refs drift: %v", ids)
	}
}

func TestSynthesizeAnswer_QuoteBack_NoValidRefsDegrades(t *testing.T) {
	exec := &research.ExecutionOutput{
		Topic:    "x",
		Action:   research.ActionWalkSeeds,
		Evidence: []fusion.Evidence{{EntityID: "real-1", Tier: "0", Source: "x"}},
	}
	s := &fakeSynthesizer{
		content: `{"synthesis":"answer makes things up","evidence_refs":["fake-only","another-fake"]}`,
	}
	got, err := synthesizeAnswer(context.Background(), s,
		&research.Intent{Topic: "x"}, exec, nil,
		2048, 30, 480, quietLogger())
	if err != nil {
		t.Fatalf("synthesizeAnswer: %v", err)
	}
	if !strings.Contains(got.Synthesis, "[note:") {
		t.Errorf("expected degraded note appended to synthesis when no refs match: %q", got.Synthesis)
	}
	// Fallback echoed top evidence so SearchResult has citations.
	if len(got.Evidence) == 0 {
		t.Error("Evidence empty; expected top-N fallback echo")
	}
}

func TestSynthesizeAnswer_EmptyEvidenceOK(t *testing.T) {
	exec := &research.ExecutionOutput{Topic: "x", Action: research.ActionWalkSeeds}
	s := &fakeSynthesizer{
		content: `{"synthesis":"No evidence available for this topic.","evidence_refs":[]}`,
	}
	got, err := synthesizeAnswer(context.Background(), s,
		&research.Intent{Topic: "x"}, exec, nil,
		2048, 30, 480, quietLogger())
	if err != nil {
		t.Fatalf("synthesizeAnswer: %v", err)
	}
	if len(got.Evidence) != 0 {
		t.Errorf("Evidence len = %d, want 0 (input was empty)", len(got.Evidence))
	}
	if !strings.Contains(got.Synthesis, "No evidence") {
		t.Errorf("synthesis drift: %q", got.Synthesis)
	}
}

func TestSynthesizeAnswer_DecompTrace_FromRouteWalkSeeds(t *testing.T) {
	exec := &research.ExecutionOutput{
		Topic:    "x",
		Action:   research.ActionWalkSeeds,
		Evidence: []fusion.Evidence{{EntityID: "e1", Tier: "0", Source: "x"}},
	}
	route := &research.RouteDecision{
		Action: research.ActionWalkSeeds,
		Args: map[string]any{
			"seeds": []map[string]string{
				{"ref": "drone-001", "ref_type": "name"},
				{"ref": "0", "ref_type": "candidate_index"},
			},
		},
	}
	s := &fakeSynthesizer{content: `{"synthesis":"x","evidence_refs":["e1"]}`}
	got, err := synthesizeAnswer(context.Background(), s,
		&research.Intent{Topic: "x"}, exec, route,
		2048, 30, 480, quietLogger())
	if err != nil {
		t.Fatalf("synthesizeAnswer: %v", err)
	}
	if got.DecompTrace == nil {
		t.Fatal("DecompTrace missing")
	}
	if got.DecompTrace.RouterAction != research.ActionWalkSeeds {
		t.Errorf("RouterAction = %q, want %q", got.DecompTrace.RouterAction, research.ActionWalkSeeds)
	}
	if len(got.DecompTrace.SeedEntities) != 2 {
		t.Errorf("SeedEntities len = %d, want 2", len(got.DecompTrace.SeedEntities))
	}
}

func TestSynthesizeAnswer_DecompTrace_NilRouteFallsBackToExecAction(t *testing.T) {
	exec := &research.ExecutionOutput{
		Topic:    "x",
		Action:   research.ActionSynthesizeDirectly,
		Evidence: []fusion.Evidence{{EntityID: "e1", Tier: "0", Source: "x"}},
	}
	s := &fakeSynthesizer{content: `{"synthesis":"x","evidence_refs":["e1"]}`}
	got, err := synthesizeAnswer(context.Background(), s,
		&research.Intent{Topic: "x"}, exec, nil,
		2048, 30, 480, quietLogger())
	if err != nil {
		t.Fatalf("synthesizeAnswer: %v", err)
	}
	if got.DecompTrace == nil {
		t.Fatal("DecompTrace should not be nil; exec.Action should populate fallback")
	}
	if got.DecompTrace.RouterAction != research.ActionSynthesizeDirectly {
		t.Errorf("RouterAction = %q, want fallback to exec.Action", got.DecompTrace.RouterAction)
	}
}

// --- synthesizeAnswer error paths ---

func TestSynthesizeAnswer_RejectsEmptySynthesis(t *testing.T) {
	s := &fakeSynthesizer{content: `{"synthesis":"   ","evidence_refs":[]}`}
	_, err := synthesizeAnswer(context.Background(), s,
		&research.Intent{Topic: "x"}, &research.ExecutionOutput{Topic: "x", Action: research.ActionDecompose},
		nil, 2048, 30, 480, quietLogger())
	if err == nil || !strings.Contains(err.Error(), "empty synthesis") {
		t.Fatalf("want empty-synthesis error, got %v", err)
	}
}

func TestSynthesizeAnswer_RejectsSynthesizerError(t *testing.T) {
	s := &fakeSynthesizer{err: errors.New("503")}
	_, err := synthesizeAnswer(context.Background(), s,
		&research.Intent{Topic: "x"}, &research.ExecutionOutput{Topic: "x", Action: research.ActionDecompose},
		nil, 2048, 30, 480, quietLogger())
	if err == nil || !strings.Contains(err.Error(), "synthesizer call") {
		t.Fatalf("want synthesizer-call error, got %v", err)
	}
}

func TestSynthesizeAnswer_RejectsMalformedJSON(t *testing.T) {
	s := &fakeSynthesizer{content: `cannot synthesize`}
	_, err := synthesizeAnswer(context.Background(), s,
		&research.Intent{Topic: "x"}, &research.ExecutionOutput{Topic: "x", Action: research.ActionDecompose},
		nil, 2048, 30, 480, quietLogger())
	if err == nil || !strings.Contains(err.Error(), "extract JSON") {
		t.Fatalf("want extract-JSON error, got %v", err)
	}
}

func TestSynthesizeAnswer_DedupRefs(t *testing.T) {
	exec := &research.ExecutionOutput{
		Topic:  "x",
		Action: research.ActionWalkSeeds,
		Evidence: []fusion.Evidence{
			{EntityID: "e1", Tier: "0", Source: "x"},
		},
	}
	s := &fakeSynthesizer{
		content: `{"synthesis":"x","evidence_refs":["e1","e1","e1"]}`,
	}
	got, err := synthesizeAnswer(context.Background(), s,
		&research.Intent{Topic: "x"}, exec, nil,
		2048, 30, 480, quietLogger())
	if err != nil {
		t.Fatalf("synthesizeAnswer: %v", err)
	}
	if len(got.Evidence) != 1 {
		t.Errorf("Evidence len = %d, want 1 (dups dropped)", len(got.Evidence))
	}
}

func TestSynthesizeAnswer_NilGuards(t *testing.T) {
	s := &fakeSynthesizer{content: `{"synthesis":"x"}`}
	if _, err := synthesizeAnswer(context.Background(), s, nil, &research.ExecutionOutput{Topic: "x"}, nil, 2048, 30, 480, nil); err == nil {
		t.Error("want intent-nil error")
	}
	if _, err := synthesizeAnswer(context.Background(), s, &research.Intent{Topic: "x"}, nil, nil, 2048, 30, 480, nil); err == nil {
		t.Error("want exec-nil error")
	}
	if _, err := synthesizeAnswer(context.Background(), nil, &research.Intent{Topic: "x"}, &research.ExecutionOutput{Topic: "x"}, nil, 2048, 30, 480, nil); err == nil {
		t.Error("want synthesizer-nil error")
	}
}

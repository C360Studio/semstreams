package researchclassify

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"

	"github.com/c360studio/semstreams/agentic/research"
	"github.com/c360studio/semstreams/graph/query"
)

// fakeClassifier returns a canned ClassificationResult so each test
// scenario drives a specific tier pathway through classifyAndRetrieve.
type fakeClassifier struct {
	result *query.ClassificationResult
}

// entity-id-audit:classify intentional-malformed "" line=287 column=15 surface=go-field:.EntityID entity_id_invalid:empty empty research candidate ID rejection fixture

func (f *fakeClassifier) ClassifyQuery(_ context.Context, _ string) *query.ClassificationResult {
	return f.result
}

// fakeRetriever records the inputs it was called with and returns
// configured candidates. The thread-safety is overkill for unit
// tests but mirrors what the production NATS adapter would face.
type fakeRetriever struct {
	mu             sync.Mutex
	called         bool
	gotTopic       string
	gotHints       map[string]any
	gotLimit       int
	candidates     []research.Candidate
	degraded       bool
	degradedReason string
	err            error
}

func (f *fakeRetriever) FetchCandidates(_ context.Context, topic string, hints map[string]any, limit int) (CandidateSet, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.called = true
	f.gotTopic = topic
	f.gotHints = hints
	f.gotLimit = limit
	if f.err != nil {
		return CandidateSet{}, f.err
	}
	return CandidateSet{
		Candidates:     f.candidates,
		Degraded:       f.degraded,
		DegradedReason: f.degradedReason,
	}, nil
}

func TestExtractLoopIDFromSubject(t *testing.T) {
	cases := []struct {
		name    string
		subject string
		want    string
	}{
		{"happy path", "component.nl_classify.rg_test001", "rg_test001"},
		{"missing prefix", "graph.query.entity", ""},
		{"prefix only", "component.nl_classify.", ""},
		{"compound loop_id", "component.nl_classify.rg-2026-05-22_001", "rg-2026-05-22_001"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := extractLoopIDFromSubject(c.subject)
			// Empty-from-prefix-only is an acceptable signal of "no
			// loop_id"; the handler reads empty as a bug.
			if c.subject == "component.nl_classify." {
				if got != "" {
					t.Errorf("prefix-only should yield empty loop_id, got %q", got)
				}
				return
			}
			if got != c.want {
				t.Errorf("got %q, want %q", got, c.want)
			}
		})
	}
}

func TestTierLabel(t *testing.T) {
	cases := map[int]string{0: "0", 1: "1", 2: "1", 3: "3", 99: ""}
	for tier, want := range cases {
		if got := tierLabel(tier); got != want {
			t.Errorf("tierLabel(%d) = %q, want %q", tier, got, want)
		}
	}
}

func TestClassifyAndRetrieve_KeywordTier(t *testing.T) {
	classifier := &fakeClassifier{result: &query.ClassificationResult{
		Tier:       0,
		Intent:     "",
		Options:    map[string]any{"path_intent": true, "path_start_node": "drone-001"},
		Confidence: 1.0,
	}}
	retriever := &fakeRetriever{
		candidates: []research.Candidate{
			{EntityID: "acme.ops.robotics.gcs.drone.001", Tier: "0", Source: "search_graph", Relevance: 0.9},
		},
	}
	intent := &research.Intent{Topic: "drones related to drone-001"}

	out, err := classifyAndRetrieve(context.Background(), classifier, retriever, intent, 10)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if out.Tier != "0" {
		t.Errorf("tier = %q, want %q", out.Tier, "0")
	}
	if out.Intent != "" {
		t.Errorf("keyword tier should leave Intent empty, got %q", out.Intent)
	}
	if out.Confidence != 1.0 {
		t.Errorf("confidence = %v, want 1.0", out.Confidence)
	}
	if out.Hints["path_intent"] != true || out.Hints["path_start_node"] != "drone-001" {
		t.Errorf("hints propagation drift: %+v", out.Hints)
	}
	if len(out.Candidates) != 1 || out.Candidates[0].EntityID != "acme.ops.robotics.gcs.drone.001" {
		t.Errorf("candidates drift: %+v", out.Candidates)
	}
	if !retriever.called {
		t.Errorf("retriever was not called")
	}
	if retriever.gotTopic != intent.Topic {
		t.Errorf("retriever topic = %q, want %q", retriever.gotTopic, intent.Topic)
	}
	if retriever.gotLimit != 10 {
		t.Errorf("retriever limit = %d, want 10", retriever.gotLimit)
	}
}

func TestClassifyAndRetrieve_EmbeddingTier(t *testing.T) {
	classifier := &fakeClassifier{result: &query.ClassificationResult{
		Tier:       1,
		Intent:     "similarity_search",
		Options:    map[string]any{"use_embeddings": true},
		Confidence: 0.78,
	}}
	retriever := &fakeRetriever{
		candidates: []research.Candidate{
			{EntityID: "test.research.graph.classify.entity.doc-001", Tier: "1", Source: "search_graph", Relevance: 0.78},
		},
	}
	intent := &research.Intent{Topic: "documents similar to onboarding guide"}

	out, err := classifyAndRetrieve(context.Background(), classifier, retriever, intent, 25)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Chain Tier 1 surfaces as "1" in the ClassifierOutput taxonomy
	// (embedding/BM25 share the slot; T2 neural is Phase 2).
	if out.Tier != "1" {
		t.Errorf("tier = %q, want %q", out.Tier, "1")
	}
	if out.Intent != "similarity_search" {
		t.Errorf("embedding intent lost: %q", out.Intent)
	}
	if out.Confidence != 0.78 {
		t.Errorf("confidence = %v, want 0.78", out.Confidence)
	}
}

func TestClassifyAndRetrieve_LLMTier(t *testing.T) {
	classifier := &fakeClassifier{result: &query.ClassificationResult{
		Tier:       3,
		Intent:     "temporal_aggregate",
		Options:    map[string]any{"time_range": map[string]any{"start": "2026-05-21", "end": "2026-05-22"}},
		Confidence: 0.91,
	}}
	retriever := &fakeRetriever{
		candidates: []research.Candidate{
			{EntityID: "test.research.graph.classify.entity.sensor-42", Tier: "0", Source: "search_graph"},
		},
	}
	intent := &research.Intent{Topic: "average voltage across batteries last 24 hours"}

	out, err := classifyAndRetrieve(context.Background(), classifier, retriever, intent, 5)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if out.Tier != "3" {
		t.Errorf("tier = %q, want %q", out.Tier, "3")
	}
	if out.Intent != "temporal_aggregate" {
		t.Errorf("LLM intent lost: %q", out.Intent)
	}
}

func TestClassifyAndRetrieve_NilClassifierResultFallsThrough(t *testing.T) {
	// Per ClassifierChain semantics: a nil ClassificationResult
	// (defensive nil-chain branch) flows through as an empty-hints
	// output. The handler should still surface the retriever's
	// candidates so the chain can proceed.
	classifier := &fakeClassifier{result: nil}
	retriever := &fakeRetriever{
		candidates: []research.Candidate{
			{EntityID: "test.research.graph.classify.entity.fallback-001", Tier: "0", Source: "search_graph"},
		},
	}
	intent := &research.Intent{Topic: "x"}

	out, err := classifyAndRetrieve(context.Background(), classifier, retriever, intent, 25)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if out.Tier != "" {
		t.Errorf("expected empty tier on nil result, got %q", out.Tier)
	}
	if len(out.Hints) != 0 {
		t.Errorf("expected empty hints on nil result, got %+v", out.Hints)
	}
	if len(out.Candidates) != 1 {
		t.Errorf("expected fallback candidates passed through, got %+v", out.Candidates)
	}
}

func TestClassifyAndRetrieve_DegradedPassthrough(t *testing.T) {
	classifier := &fakeClassifier{result: &query.ClassificationResult{Tier: 0}}
	retriever := &fakeRetriever{
		candidates:     []research.Candidate{{EntityID: "test.research.graph.classify.entity.x", Tier: "0", Source: "search_graph"}},
		degraded:       true,
		degradedReason: "server-side semantic fallback fired",
	}
	intent := &research.Intent{Topic: "x"}

	out, err := classifyAndRetrieve(context.Background(), classifier, retriever, intent, 10)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !out.Degraded || out.DegradedReason == "" {
		t.Errorf("degraded fields lost: %+v", out)
	}
}

func TestClassifyAndRetrieve_RetrieverErrorSurfaces(t *testing.T) {
	classifier := &fakeClassifier{result: &query.ClassificationResult{Tier: 0}}
	retriever := &fakeRetriever{err: errors.New("nats unreachable")}
	intent := &research.Intent{Topic: "x"}

	_, err := classifyAndRetrieve(context.Background(), classifier, retriever, intent, 10)
	if err == nil {
		t.Fatalf("expected error from retriever, got nil")
	}
	if !strings.Contains(err.Error(), "retrieve candidates") {
		t.Errorf("expected wrapped retriever error, got: %v", err)
	}
}

func TestClassifyAndRetrieve_NilInputsRejected(t *testing.T) {
	t.Run("nil intent", func(t *testing.T) {
		_, err := classifyAndRetrieve(context.Background(), &fakeClassifier{}, &fakeRetriever{}, nil, 10)
		if err == nil {
			t.Fatal("expected error for nil intent")
		}
	})
	t.Run("nil classifier", func(t *testing.T) {
		_, err := classifyAndRetrieve(context.Background(), nil, &fakeRetriever{}, &research.Intent{Topic: "x"}, 10)
		if err == nil {
			t.Fatal("expected error for nil classifier")
		}
	})
	t.Run("nil retriever", func(t *testing.T) {
		_, err := classifyAndRetrieve(context.Background(), &fakeClassifier{}, nil, &research.Intent{Topic: "x"}, 10)
		if err == nil {
			t.Fatal("expected error for nil retriever")
		}
	})
}

func TestClassifyAndRetrieve_OutputValidationCatchesBadCandidate(t *testing.T) {
	// The handler validates the assembled output before returning so
	// downstream consumers can rely on Validate-pass payloads.
	classifier := &fakeClassifier{result: &query.ClassificationResult{Tier: 0}}
	retriever := &fakeRetriever{
		candidates: []research.Candidate{
			{EntityID: "", Tier: "0", Source: "search_graph"}, // invalid
		},
	}
	intent := &research.Intent{Topic: "x"}

	_, err := classifyAndRetrieve(context.Background(), classifier, retriever, intent, 10)
	if err == nil {
		t.Fatal("expected validation error for bad candidate, got nil")
	}
	if !strings.Contains(err.Error(), "validation") {
		t.Errorf("expected validation-wrapped error, got: %v", err)
	}
}

// TestClassifyAndRetrieve_HintsAreDefensiveCopy locks in the
// defensive map copy from handler.go so a downstream consumer
// mutating ClassifierOutput.Hints doesn't bleed into the classifier's
// internal Options map.
func TestClassifyAndRetrieve_HintsAreDefensiveCopy(t *testing.T) {
	sourceOptions := map[string]any{"path_intent": true}
	classifier := &fakeClassifier{result: &query.ClassificationResult{
		Tier:    0,
		Options: sourceOptions,
	}}
	retriever := &fakeRetriever{
		candidates: []research.Candidate{{EntityID: "test.research.graph.classify.entity.x", Tier: "0", Source: "search_graph"}},
	}
	intent := &research.Intent{Topic: "x"}

	out, err := classifyAndRetrieve(context.Background(), classifier, retriever, intent, 10)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// Mutate the returned hints; the source must be untouched.
	out.Hints["path_intent"] = false
	out.Hints["mutated_key"] = "leak"
	if sourceOptions["path_intent"] != true {
		t.Errorf("source Options mutated through ClassifierOutput.Hints — defensive copy missing")
	}
	if _, leaked := sourceOptions["mutated_key"]; leaked {
		t.Errorf("source Options gained a key from caller mutation — defensive copy missing")
	}
}

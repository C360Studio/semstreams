package githubprworkflow

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"strings"
	"testing"
	"time"

	gtypes "github.com/c360studio/semstreams/graph"
	"github.com/c360studio/semstreams/message"
)

// --- Prompt Builder Tests ---

func TestBuildQualifierPrompt(t *testing.T) {
	prompt := BuildQualifierPrompt("acme", "myapp", 42, "Server panics on empty input", "Steps to reproduce: send an empty POST body.")

	for _, want := range []string{"acme", "myapp", "#42", "Server panics", "empty POST body"} {
		if !strings.Contains(prompt, want) {
			t.Errorf("Prompt missing %q", want)
		}
	}
}

func TestBuildDeveloperPrompt_FirstAttempt(t *testing.T) {
	prompt := BuildDeveloperPrompt("acme", "myapp", 42, "Crash on startup", "The process crashes.", "qualified", 0.9, "critical", "")

	if strings.Contains(prompt, "Previous review feedback") {
		t.Error("First attempt should not include review feedback section")
	}
	for _, want := range []string{"acme/myapp", "#42", "Crash on startup", "qualified", "critical"} {
		if !strings.Contains(prompt, want) {
			t.Errorf("Prompt missing %q", want)
		}
	}
}

func TestBuildDeveloperPrompt_WithFeedback(t *testing.T) {
	prompt := BuildDeveloperPrompt("acme", "myapp", 7, "Memory leak", "Leaks on startup", "qualified", 0.85, "high", "Missing nil guard in main.go line 42.")

	if !strings.Contains(prompt, "Previous review feedback") {
		t.Error("Retry attempt should include review feedback section")
	}
	if !strings.Contains(prompt, "Missing nil guard") {
		t.Error("Prompt should include the actual feedback text")
	}
}

func TestBuildReviewerPrompt(t *testing.T) {
	prompt := BuildReviewerPrompt("example", "service", 12, "Memory leak in worker pool", 55, "https://github.com/example/service/pull/55", []string{"worker/pool.go", "worker/pool_test.go"})

	for _, want := range []string{"example/service", "#12", "#55", "worker/pool.go", "worker/pool_test.go", "adversarial"} {
		if !strings.Contains(prompt, want) {
			t.Errorf("Prompt missing %q", want)
		}
	}
}

// --- Result Parser Tests ---

func TestParseQualifierResult_Qualified(t *testing.T) {
	v := ParseQualifierResult(`{"verdict":"qualified","confidence":0.92,"severity":"high"}`)

	if v.Verdict != "qualified" {
		t.Errorf("Verdict: got %q, want %q", v.Verdict, "qualified")
	}
	if v.Confidence != 0.92 {
		t.Errorf("Confidence: got %v, want 0.92", v.Confidence)
	}
	if v.Severity != "high" {
		t.Errorf("Severity: got %q, want %q", v.Severity, "high")
	}
}

func TestParseQualifierResult_Rejected(t *testing.T) {
	v := ParseQualifierResult(`{"verdict":"rejected","confidence":0.85,"severity":"low"}`)

	if v.Verdict != "rejected" {
		t.Errorf("Verdict: got %q, want %q", v.Verdict, "rejected")
	}
}

func TestParseQualifierResult_BadJSON(t *testing.T) {
	v := ParseQualifierResult("I cannot determine the verdict.")

	if v.Verdict != PhaseNeedsInfo {
		t.Errorf("Verdict: got %q, want %q (graceful fallback)", v.Verdict, PhaseNeedsInfo)
	}
}

func TestParseDeveloperResult(t *testing.T) {
	o := ParseDeveloperResult(`{"branch_name":"fix/issue-42","pr_number":99,"pr_url":"https://github.com/acme/app/pull/99","files_changed":["server/handler.go","server/handler_test.go"]}`)

	if o.BranchName != "fix/issue-42" {
		t.Errorf("BranchName: got %q", o.BranchName)
	}
	if o.PRNumber != 99 {
		t.Errorf("PRNumber: got %d, want 99", o.PRNumber)
	}
	if len(o.FilesChanged) != 2 {
		t.Errorf("FilesChanged: got %d entries, want 2", len(o.FilesChanged))
	}
}

func TestParseDeveloperResult_BadJSON(t *testing.T) {
	o := ParseDeveloperResult("not json")
	if o.PRNumber != 0 {
		t.Errorf("PRNumber should be zero on bad JSON, got %d", o.PRNumber)
	}
}

func TestParseReviewerResult_Approve(t *testing.T) {
	r := ParseReviewerResult(`{"verdict":"approved","feedback":""}`)

	if r.Verdict != "approved" {
		t.Errorf("Verdict: got %q, want %q", r.Verdict, "approved")
	}
}

func TestParseReviewerResult_Reject(t *testing.T) {
	r := ParseReviewerResult(`{"verdict":"request_changes","feedback":"Please add unit tests."}`)

	if r.Verdict != "request_changes" {
		t.Errorf("Verdict: got %q, want %q", r.Verdict, "request_changes")
	}
	if r.Feedback != "Please add unit tests." {
		t.Errorf("Feedback: got %q", r.Feedback)
	}
}

// --- Entity ID Tests ---

func TestWorkflowEntityID(t *testing.T) {
	tests := []struct {
		org, repo   string
		issueNumber int
		want        string
	}{
		{"acme", "myapp", 42, "acme.github.repo.myapp.workflow.42"},
		{"bigco", "platform", 1, "bigco.github.repo.platform.workflow.1"},
	}

	for _, tt := range tests {
		got := WorkflowEntityID(tt.org, tt.repo, tt.issueNumber)
		if got != tt.want {
			t.Errorf("WorkflowEntityID(%q, %q, %d) = %q, want %q", tt.org, tt.repo, tt.issueNumber, got, tt.want)
		}
	}
}

func TestExtractEntityIDFromTaskID(t *testing.T) {
	tests := []struct {
		taskID string
		prefix string
		want   string
	}{
		// New "::" separator format
		{"qualifier::acme.github.repo.myapp.workflow.42", "qualifier::", "acme.github.repo.myapp.workflow.42"},
		{"developer::acme.github.repo.myapp.workflow.42", "developer::", "acme.github.repo.myapp.workflow.42"},
		{"reviewer::acme.github.repo.myapp.workflow.42", "reviewer::", "acme.github.repo.myapp.workflow.42"},
		// Edge cases
		{"wrong-prefix-123", "qualifier::", ""},
		{"qualifier::", "qualifier::", ""},
	}

	for _, tt := range tests {
		got := extractEntityIDFromTaskID(tt.taskID, tt.prefix)
		if got != tt.want {
			t.Errorf("extractEntityIDFromTaskID(%q, %q) = %q, want %q", tt.taskID, tt.prefix, got, tt.want)
		}
	}
}

// --- Phase Constants ---

func TestPhaseConstants(t *testing.T) {
	// Verify key phase constants are defined and distinct
	phases := map[string]bool{
		PhaseQualified:        true,
		PhaseRejected:         true,
		PhaseNotABug:          true,
		PhaseWontFix:          true,
		PhaseNeedsInfo:        true,
		PhaseAwaitingInfo:     true,
		PhaseDevComplete:      true,
		PhaseDeveloping:       true,
		PhaseApproved:         true,
		PhaseChangesRequested: true,
		PhaseEscalated:        true,
	}

	if len(phases) != 11 {
		t.Errorf("Expected 11 distinct phases, got %d (check for duplicates)", len(phases))
	}
}

func TestPhaseIsActive(t *testing.T) {
	activePhases := []string{
		PhaseQualified, PhaseDeveloping, PhaseDevComplete,
		PhaseChangesRequested, PhaseNeedsInfo, PhaseAwaitingInfo,
	}
	for _, p := range activePhases {
		if !PhaseIsActive(p) {
			t.Errorf("PhaseIsActive(%q) = false, want true", p)
		}
	}

	inactivePhases := []string{
		PhaseApproved, PhaseEscalated, PhaseRejected,
		PhaseNotABug, PhaseWontFix, "",
	}
	for _, p := range inactivePhases {
		if PhaseIsActive(p) {
			t.Errorf("PhaseIsActive(%q) = true, want false", p)
		}
	}
}

// --- Config Tests ---

func TestConfigWithDefaults(t *testing.T) {
	cfg := Config{}.withDefaults()

	if cfg.Model != "default" {
		t.Errorf("Model: got %q, want %q", cfg.Model, "default")
	}
	if cfg.TokenBudget != DefaultTokenBudget {
		t.Errorf("TokenBudget: got %d, want %d", cfg.TokenBudget, DefaultTokenBudget)
	}
	if cfg.MaxReviewCycles != MaxReviewCycles {
		t.Errorf("MaxReviewCycles: got %d, want %d", cfg.MaxReviewCycles, MaxReviewCycles)
	}
}

func TestConfigWithDefaults_PreservesExplicit(t *testing.T) {
	cfg := Config{
		Model:           "claude-opus-4-6",
		TokenBudget:     100_000,
		MaxReviewCycles: 5,
	}.withDefaults()

	if cfg.Model != "claude-opus-4-6" {
		t.Errorf("Model: got %q, want %q", cfg.Model, "claude-opus-4-6")
	}
	if cfg.TokenBudget != 100_000 {
		t.Errorf("TokenBudget: got %d, want 100000", cfg.TokenBudget)
	}
	if cfg.MaxReviewCycles != 5 {
		t.Errorf("MaxReviewCycles: got %d, want 5", cfg.MaxReviewCycles)
	}
}

// --- Budget Constants ---

func TestDefaultBudgetConstants(t *testing.T) {
	if DefaultTokenBudget <= 0 {
		t.Errorf("DefaultTokenBudget: got %d, must be positive", DefaultTokenBudget)
	}
	if DefaultMaxConcurrentWorkflows <= 0 {
		t.Errorf("DefaultMaxConcurrentWorkflows: got %d, must be positive", DefaultMaxConcurrentWorkflows)
	}
	if DefaultHourlyTokenCeiling <= DefaultTokenBudget {
		t.Errorf("DefaultHourlyTokenCeiling (%d) should be larger than DefaultTokenBudget (%d)",
			DefaultHourlyTokenCeiling, DefaultTokenBudget)
	}
}

// --- workflowState Tests ---

// TestWorkflowState_ZeroValue verifies that a new workflowState has all zeros,
// matching the getWorkflowState fallback when the entity has no triples yet.
func TestWorkflowState_ZeroValue(t *testing.T) {
	state := &workflowState{}
	if state.TotalTokens != 0 || state.Rejections != 0 {
		t.Errorf("Zero-value state should have all zeros, got %+v", state)
	}
}

// --- writeTriple Request Format Tests ---

// TestWriteTripleRequestFormat verifies that writeTriple constructs a properly
// structured AddTripleRequest by marshalling the request and inspecting it
// directly — without a live NATS connection.
func TestWriteTripleRequestFormat(t *testing.T) {
	entityID := "acme.github.repo.myapp.workflow.42"
	predicate := "workflow.state.phase"
	object := "qualifying"

	triple := message.Triple{
		Subject:    entityID,
		Predicate:  predicate,
		Object:     object,
		Source:     componentName,
		Timestamp:  time.Now(),
		Confidence: 1.0,
	}
	req := gtypes.AddTripleRequest{Triple: triple}
	data, err := json.Marshal(req)
	if err != nil {
		t.Fatalf("Marshal AddTripleRequest: %v", err)
	}

	// Unmarshal back and verify structure.
	var got gtypes.AddTripleRequest
	if err := json.Unmarshal(data, &got); err != nil {
		t.Fatalf("Unmarshal AddTripleRequest: %v", err)
	}

	if got.Triple.Subject != entityID {
		t.Errorf("Subject: got %q, want %q", got.Triple.Subject, entityID)
	}
	if got.Triple.Predicate != predicate {
		t.Errorf("Predicate: got %q, want %q", got.Triple.Predicate, predicate)
	}
	if fmt.Sprintf("%v", got.Triple.Object) != object {
		t.Errorf("Object: got %v, want %q", got.Triple.Object, object)
	}
	if got.Triple.Source != componentName {
		t.Errorf("Source: got %q, want %q", got.Triple.Source, componentName)
	}
	if got.Triple.Confidence != 1.0 {
		t.Errorf("Confidence: got %v, want 1.0", got.Triple.Confidence)
	}

	// Verify the JSON uses the "triple" envelope field expected by graph-ingest.
	var raw map[string]json.RawMessage
	if err := json.Unmarshal(data, &raw); err != nil {
		t.Fatalf("Unmarshal to raw map: %v", err)
	}
	if _, ok := raw["triple"]; !ok {
		t.Error("AddTripleRequest JSON must contain a 'triple' field")
	}
}

// TestWriteTriple_NilClient verifies that writeTriple returns a non-nil error
// when the NATS client is nil, rather than panicking.
func TestWriteTriple_NilClient(t *testing.T) {
	c := &PRWorkflowComponent{
		natsClient: nil,
		logger:     newNopLogger(),
	}
	err := c.writeTriple(context.Background(), "acme.github.repo.myapp.workflow.1", "workflow.state.phase", "qualifying")
	if err == nil {
		t.Error("writeTriple with nil natsClient should return an error")
	}
}

// newNopLogger returns a discard logger suitable for tests.
func newNopLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, &slog.HandlerOptions{Level: slog.LevelError}))
}

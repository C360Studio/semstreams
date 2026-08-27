package agenticloop

import (
	"context"
	"fmt"
	"math"
	"strings"
	"testing"
	"time"
	"unicode/utf8"

	"github.com/c360studio/semstreams/agentic"
	"github.com/c360studio/semstreams/internal/semantictest"
	"github.com/c360studio/semstreams/message"
	"github.com/c360studio/semstreams/model"
	"github.com/c360studio/semstreams/pkg/errs"
	"github.com/c360studio/semstreams/types"
	agvocab "github.com/c360studio/semstreams/vocabulary/agentic"
)

// predicateSet collects the predicates from a slice of triples for easy membership testing.
func predicateSet(triples []message.Triple) map[string]bool {
	s := make(map[string]bool, len(triples))
	for _, t := range triples {
		s[t.Predicate] = true
	}
	return s
}

// objectFor returns the Object value for the first triple with the given predicate,
// or nil if no such triple exists.
func objectFor(triples []message.Triple, predicate string) any {
	for _, t := range triples {
		if t.Predicate == predicate {
			return t.Object
		}
	}
	return nil
}

// --- modelEndpointEntity (the registered ModelEndpointEntity is the builder) ---

func TestBuildModelEndpointTriples_RequiredFields(t *testing.T) {
	entityID := "acme.ops.model-registry.agent.endpoint.claude"
	ep := model.EndpointConfig{
		Provider:      "anthropic",
		Model:         "claude-opus-4-5",
		SupportsTools: true,
	}

	triples := modelEndpointEntity("acme", "ops", "claude", ep).Triples()

	// All triples must reference the correct entity.
	for _, tr := range triples {
		if tr.Subject != entityID {
			t.Errorf("unexpected subject: got %q, want %q", tr.Subject, entityID)
		}
		if tr.Source != graphWriterSource {
			t.Errorf("unexpected source: got %q, want %q", tr.Source, graphWriterSource)
		}
		if tr.Confidence != 1.0 {
			t.Errorf("unexpected confidence: got %v, want 1.0", tr.Confidence)
		}
	}

	facts := predicateSet(triples)

	required := []string{agvocab.ModelProvider, agvocab.ModelName, agvocab.ModelSupportsTools}
	for _, pred := range required {
		if !facts[pred] {
			t.Errorf("missing required predicate: %s", pred)
		}
	}

	if got := objectFor(triples, agvocab.ModelProvider); got != "anthropic" {
		t.Errorf("%s: got %v, want anthropic", agvocab.ModelProvider, got)
	}
	if got := objectFor(triples, agvocab.ModelName); got != "claude-opus-4-5" {
		t.Errorf("%s: got %v, want claude-opus-4-5", agvocab.ModelName, got)
	}
	if got := objectFor(triples, agvocab.ModelSupportsTools); got != true {
		t.Errorf("%s: got %v, want true", agvocab.ModelSupportsTools, got)
	}
}

func TestBuildModelEndpointTriples_OptionalFieldsOmittedWhenZero(t *testing.T) {
	ep := model.EndpointConfig{
		Provider: "ollama",
		Model:    "llama3.2",
		// MaxTokens, pricing, URL, rate limit all zero/empty
	}

	triples := modelEndpointEntity("acme", "ops", "local", ep).Triples()
	facts := predicateSet(triples)

	optional := []string{
		agvocab.ModelMaxTokens,
		agvocab.ModelInputPrice,
		agvocab.ModelOutputPrice,
		agvocab.ModelEndpointURL,
		agvocab.ModelRateLimit,
	}
	for _, pred := range optional {
		if facts[pred] {
			t.Errorf("expected predicate %s to be omitted when zero, but it was present", pred)
		}
	}
}

func TestBuildModelEndpointTriples_OptionalFieldsPresentWhenSet(t *testing.T) {
	ep := model.EndpointConfig{
		Provider:               "openai",
		Model:                  "gpt-4o",
		MaxTokens:              128000,
		SupportsTools:          true,
		InputPricePer1MTokens:  5.0,
		OutputPricePer1MTokens: 15.0,
		URL:                    "https://api.openai.com/v1",
		RequestsPerMinute:      60,
	}

	triples := modelEndpointEntity("acme", "ops", "gpt4o", ep).Triples()
	facts := predicateSet(triples)

	optional := []string{
		agvocab.ModelMaxTokens,
		agvocab.ModelInputPrice,
		agvocab.ModelOutputPrice,
		agvocab.ModelEndpointURL,
		agvocab.ModelRateLimit,
	}
	for _, pred := range optional {
		if !facts[pred] {
			t.Errorf("expected predicate %s to be present, but it was omitted", pred)
		}
	}

	if got := objectFor(triples, agvocab.ModelMaxTokens); got != 128000 {
		t.Errorf("%s: got %v, want 128000", agvocab.ModelMaxTokens, got)
	}
	if got := objectFor(triples, agvocab.ModelEndpointURL); got != "https://api.openai.com/v1" {
		t.Errorf("%s: got %v, want URL", agvocab.ModelEndpointURL, got)
	}
	if got := objectFor(triples, agvocab.ModelRateLimit); got != 60 {
		t.Errorf("%s: got %v, want 60", agvocab.ModelRateLimit, got)
	}
}

// --- buildLoopCompletionTriples ---

func TestBuildLoopCompletionTriples_RequiredFields(t *testing.T) {
	loopEntityID := "acme.ops.agentic-loop.agent.execution.loop123"
	modelEntityID := "acme.ops.model-registry.agent.endpoint.claude"

	event := &agentic.LoopCompletedEvent{
		LoopID:      "loop123",
		TaskID:      "task-abc",
		Outcome:     "success",
		Role:        "architect",
		Model:       "claude",
		Iterations:  5,
		TokensIn:    1000,
		TokensOut:   500,
		CompletedAt: time.Now(),
	}

	triples := buildLoopCompletionTriples(loopEntityID, event, modelEntityID, 0, false)

	for _, tr := range triples {
		if tr.Subject != loopEntityID {
			t.Errorf("unexpected subject: got %q, want %q", tr.Subject, loopEntityID)
		}
		if tr.Confidence != 1.0 {
			t.Errorf("unexpected confidence: got %v, want 1.0", tr.Confidence)
		}
	}

	facts := predicateSet(triples)
	required := []string{
		agvocab.LoopOutcome,
		agvocab.LoopIterations,
		agvocab.LoopTokensIn,
		agvocab.LoopTokensOut,
		agvocab.LoopEndedAt,
	}
	for _, pred := range required {
		if !facts[pred] {
			t.Errorf("missing required predicate: %s", pred)
		}
	}

	// gh#159: spawn-known predicates (role, task) live on the entity
	// from WriteSpawnIdentity, NOT from the completion stamp — otherwise
	// graph-ingest's append semantics would duplicate them on every
	// completion.
	spawnStamped := []string{
		agvocab.LoopRole,
		agvocab.LoopTask,
	}
	for _, pred := range spawnStamped {
		if facts[pred] {
			t.Errorf("predicate %s should be spawn-stamped only, not in completion triples", pred)
		}
	}

	// LoopModelUsed is conditional on non-empty modelEntityID.
	if !facts[agvocab.LoopModelUsed] {
		t.Errorf("expected LoopModelUsed when modelEntityID is non-empty")
	}
	if got := objectFor(triples, agvocab.LoopModelUsed); got != modelEntityID {
		t.Errorf("%s: got %v, want %q", agvocab.LoopModelUsed, got, modelEntityID)
	}
	if got := objectFor(triples, agvocab.LoopIterations); got != 5 {
		t.Errorf("%s: got %v, want 5", agvocab.LoopIterations, got)
	}
	if got := objectFor(triples, agvocab.LoopTokensIn); got != 1000 {
		t.Errorf("%s: got %v, want 1000", agvocab.LoopTokensIn, got)
	}
}

func TestBuildLoopCompletionTriples_CostCalculation(t *testing.T) {
	loopEntityID := "acme.ops.agentic-loop.agent.execution.loop456"
	modelEntityID := "acme.ops.model-registry.agent.endpoint.claude"

	event := &agentic.LoopCompletedEvent{
		LoopID:      "loop456",
		TaskID:      "task-def",
		Outcome:     "success",
		Role:        "editor",
		Model:       "claude",
		Iterations:  3,
		TokensIn:    1000,
		TokensOut:   500,
		CompletedAt: time.Now(),
	}

	// (1000 * 3.0 + 500 * 15.0) / 1_000_000 = 0.0105
	cost := float64(event.TokensIn)*3.0/1_000_000 + float64(event.TokensOut)*15.0/1_000_000
	triples := buildLoopCompletionTriples(loopEntityID, event, modelEntityID, cost, false)

	facts := predicateSet(triples)
	if !facts[agvocab.LoopCostUSD] {
		t.Fatal("expected LoopCostUSD triple to be present")
	}

	got, ok := objectFor(triples, agvocab.LoopCostUSD).(float64)
	if !ok {
		t.Fatalf("LoopCostUSD object is not float64: %T", objectFor(triples, agvocab.LoopCostUSD))
	}

	want := 0.0105
	if math.Abs(got-want) > 1e-9 {
		t.Errorf("cost: got %.10f, want %.10f", got, want)
	}
}

func TestBuildLoopCompletionTriples_ZeroCostOmitted(t *testing.T) {
	loopEntityID := "acme.ops.agentic-loop.agent.execution.loop789"
	modelEntityID := "acme.ops.model-registry.agent.endpoint.local"

	event := &agentic.LoopCompletedEvent{
		LoopID:      "loop789",
		TaskID:      "task-ghi",
		Outcome:     "success",
		Role:        "reviewer",
		Model:       "local",
		Iterations:  1,
		CompletedAt: time.Now(),
	}

	triples := buildLoopCompletionTriples(loopEntityID, event, modelEntityID, 0, false)
	facts := predicateSet(triples)

	if facts[agvocab.LoopCostUSD] {
		t.Error("expected LoopCostUSD to be omitted when cost is 0")
	}
}

func TestBuildLoopCompletionTriples_OptionalFieldsOmittedWhenEmpty(t *testing.T) {
	loopEntityID := "acme.ops.agentic-loop.agent.execution.loopA"
	modelEntityID := "acme.ops.model-registry.agent.endpoint.claude"

	event := &agentic.LoopCompletedEvent{
		LoopID:      "loopA",
		TaskID:      "task-jkl",
		Outcome:     "success",
		Role:        "architect",
		Model:       "claude",
		Iterations:  2,
		CompletedAt: time.Now(),
		// ParentLoopID, WorkflowSlug, WorkflowStep, UserID all empty
	}

	triples := buildLoopCompletionTriples(loopEntityID, event, modelEntityID, 0, false)
	facts := predicateSet(triples)

	optional := []string{
		agvocab.LoopParent,
		agvocab.LoopWorkflow,
		agvocab.LoopWorkflowStep,
		agvocab.LoopUser,
		agvocab.LoopDescription,
	}
	for _, pred := range optional {
		if facts[pred] {
			t.Errorf("expected predicate %s to be omitted when empty, but it was present", pred)
		}
	}
}

// gh#159: agent.loop.description is now spawn-stamped via
// WriteSpawnIdentity; completion/failure stamps do NOT carry it. Covered
// by TestBuildSpawnIdentityTriples_StampsDescription.
func TestBuildLoopCompletionTriples_DescriptionNotInCompletion(t *testing.T) {
	loopEntityID := "acme.ops.agentic-loop.agent.execution.loopD"
	modelEntityID := "acme.ops.model-registry.agent.endpoint.claude"

	event := &agentic.LoopCompletedEvent{
		LoopID:      "loopD",
		TaskID:      "task-mqtt",
		Outcome:     "success",
		Role:        "researcher",
		Prompt:      "Investigate MQTT retained-message behavior in the telemetry ingress",
		Model:       "claude",
		Iterations:  3,
		CompletedAt: time.Now(),
	}

	triples := buildLoopCompletionTriples(loopEntityID, event, modelEntityID, 0, false)

	if predicateSet(triples)[agvocab.LoopDescription] {
		t.Errorf("%s leaked into completion stamp; should be spawn-only", agvocab.LoopDescription)
	}
}

func TestBuildLoopFailureTriples_DescriptionNotInFailure(t *testing.T) {
	loopEntityID := "acme.ops.agentic-loop.agent.execution.loopE"
	modelEntityID := "acme.ops.model-registry.agent.endpoint.claude"

	event := &agentic.LoopFailedEvent{
		LoopID:     "loopE",
		TaskID:     "task-deploy",
		Outcome:    "failed",
		Reason:     "timeout",
		Error:      "context deadline exceeded",
		Role:       "researcher",
		Prompt:     "Find deployment errors from the last 24 hours",
		Model:      "claude",
		Iterations: 1,
		FailedAt:   time.Now(),
	}

	triples := buildLoopFailureTriples(loopEntityID, event, modelEntityID, 0, false)

	if predicateSet(triples)[agvocab.LoopDescription] {
		t.Errorf("%s leaked into failure stamp; should be spawn-only", agvocab.LoopDescription)
	}
}

func TestTruncateForTriple(t *testing.T) {
	t.Run("short string passes through", func(t *testing.T) {
		if got := truncateForTriple("short", 100); got != "short" {
			t.Errorf("got %q, want %q", got, "short")
		}
	})

	t.Run("ascii long string is truncated with marker", func(t *testing.T) {
		long := strings.Repeat("a", 10_000)
		got := truncateForTriple(long, 8192)
		if len(got) > 8192 {
			t.Errorf("result length %d exceeds cap 8192", len(got))
		}
		if !strings.HasSuffix(got, "…[truncated]") {
			t.Errorf("expected truncation marker suffix, got tail: %q", got[len(got)-20:])
		}
	})

	t.Run("multi-byte runes not split at boundary", func(t *testing.T) {
		// Each Japanese "日" is 3 bytes. Build a string whose natural byte
		// boundary falls mid-rune, then verify the result is still valid
		// UTF-8 and length is under the cap.
		long := strings.Repeat("日", 3000) // 9000 bytes
		for _, maxLen := range []int{200, 201, 202, 1000, 8192} {
			got := truncateForTriple(long, maxLen)
			if len(got) > maxLen {
				t.Errorf("max=%d: len=%d exceeds cap", maxLen, len(got))
			}
			if !utf8.ValidString(got) {
				t.Errorf("max=%d: result is not valid UTF-8: %q", maxLen, got)
			}
			if !strings.HasSuffix(got, "…[truncated]") {
				t.Errorf("max=%d: missing marker suffix", maxLen)
			}
		}
	})

	t.Run("cap smaller than marker yields marker prefix", func(t *testing.T) {
		got := truncateForTriple("some long string", 5)
		if len(got) != 5 {
			t.Errorf("got len %d, want 5", len(got))
		}
	})
}

// gh#159: spawn-stamped predicates (parent, workflow, workflow_step,
// user) live on the entity from WriteSpawnIdentity, NOT the completion
// stamp. Covered by TestBuildSpawnIdentityTriples_OptionalFieldsPresentWhenSet.
func TestBuildLoopCompletionTriples_SpawnFieldsNotInCompletion(t *testing.T) {
	loopEntityID := "acme.ops.agentic-loop.agent.execution.loopB"
	modelEntityID := "acme.ops.model-registry.agent.endpoint.claude"

	event := &agentic.LoopCompletedEvent{
		LoopID:       "loopB",
		TaskID:       "task-mno",
		Outcome:      "success",
		Role:         "architect",
		Model:        "claude",
		Iterations:   4,
		ParentLoopID: "loopA",
		WorkflowSlug: "code-review",
		WorkflowStep: "draft",
		UserID:       "user-xyz",
		CompletedAt:  time.Now(),
	}

	triples := buildLoopCompletionTriples(loopEntityID, event, modelEntityID, 0, false)
	facts := predicateSet(triples)

	spawnOnly := []string{
		agvocab.LoopParent,
		agvocab.LoopWorkflow,
		agvocab.LoopWorkflowStep,
		agvocab.LoopUser,
		agvocab.LoopRole,
		agvocab.LoopTask,
	}
	for _, pred := range spawnOnly {
		if facts[pred] {
			t.Errorf("predicate %s leaked into completion stamp; should be spawn-only", pred)
		}
	}
}

// --- buildLoopFailureTriples ---

func TestBuildLoopFailureTriples_RequiredFields(t *testing.T) {
	loopEntityID := "acme.ops.agentic-loop.agent.execution.loopFail"
	modelEntityID := "acme.ops.model-registry.agent.endpoint.claude"

	event := &agentic.LoopFailedEvent{
		LoopID:     "loopFail",
		TaskID:     "task-fail",
		Outcome:    "failed",
		Role:       "editor",
		Model:      "claude",
		Iterations: 3,
		TokensIn:   800,
		TokensOut:  200,
		FailedAt:   time.Now(),
	}

	triples := buildLoopFailureTriples(loopEntityID, event, modelEntityID, 0, false)
	facts := predicateSet(triples)

	required := []string{
		agvocab.LoopOutcome,
		agvocab.LoopIterations,
		agvocab.LoopTokensIn,
		agvocab.LoopTokensOut,
		agvocab.LoopEndedAt,
	}
	for _, pred := range required {
		if !facts[pred] {
			t.Errorf("missing required predicate: %s", pred)
		}
	}

	// gh#159: role and task are spawn-stamped, not failure-stamped.
	if facts[agvocab.LoopRole] {
		t.Errorf("%s leaked into failure stamp; should be spawn-only", agvocab.LoopRole)
	}
	if facts[agvocab.LoopTask] {
		t.Errorf("%s leaked into failure stamp; should be spawn-only", agvocab.LoopTask)
	}

	// LoopModelUsed is conditional on non-empty modelEntityID.
	if !facts[agvocab.LoopModelUsed] {
		t.Errorf("expected LoopModelUsed when modelEntityID is non-empty")
	}

	if got := objectFor(triples, agvocab.LoopOutcome); got != "failed" {
		t.Errorf("%s: got %v, want failed", agvocab.LoopOutcome, got)
	}
}

func TestBuildLoopFailureTriples_OptionalFieldsOmittedWhenEmpty(t *testing.T) {
	loopEntityID := "acme.ops.agentic-loop.agent.execution.loopFail2"
	modelEntityID := "acme.ops.model-registry.agent.endpoint.claude"

	event := &agentic.LoopFailedEvent{
		LoopID:     "loopFail2",
		TaskID:     "task-fail2",
		Outcome:    "failed",
		Role:       "editor",
		Model:      "claude",
		Iterations: 1,
		FailedAt:   time.Now(),
	}

	triples := buildLoopFailureTriples(loopEntityID, event, modelEntityID, 0, false)
	facts := predicateSet(triples)

	// ParentLoopID belongs to the optional set — when empty (failure on a
	// chain root or a non-chain-fanned spawn), no agent.loop.parent triple
	// should be emitted.
	optional := []string{agvocab.LoopWorkflow, agvocab.LoopWorkflowStep, agvocab.LoopUser, agvocab.LoopParent}
	for _, pred := range optional {
		if facts[pred] {
			t.Errorf("expected predicate %s to be omitted when empty", pred)
		}
	}
}

// gh#159: agent.loop.parent is now spawn-stamped via WriteSpawnIdentity,
// so the chain-aware ancestry walk from semteams ADR-038 PR B reads
// parent from the same entity but stamped at spawn rather than at
// failure. Covered structurally by
// TestBuildSpawnIdentityTriples_StampsParent — failure-side test pins
// the absence so a regression that re-adds the failure-time stamp
// (producing duplicate parent triples after append-semantics) fails
// loud.
func TestBuildLoopFailureTriples_ParentNotInFailure(t *testing.T) {
	loopEntityID := "acme.ops.agentic-loop.agent.execution.loopFailChild"
	modelEntityID := "acme.ops.model-registry.agent.endpoint.claude"

	event := &agentic.LoopFailedEvent{
		LoopID:       "loopFailChild",
		TaskID:       "task-fail-child",
		Outcome:      "failed",
		Role:         "researcher",
		Model:        "claude",
		Iterations:   3,
		ParentLoopID: "loopParentXYZ",
		FailedAt:     time.Now(),
	}

	triples := buildLoopFailureTriples(loopEntityID, event, modelEntityID, 0, false)
	if predicateSet(triples)[agvocab.LoopParent] {
		t.Errorf("%s leaked into failure stamp; should be spawn-only", agvocab.LoopParent)
	}
}

// gh#159: workflow / workflow_step / user are spawn-only; failure stamp
// keeps only the completion-shape signals (outcome, iterations, tokens,
// cost, model_used, ended_at).
func TestBuildLoopFailureTriples_SpawnFieldsNotInFailure(t *testing.T) {
	loopEntityID := "acme.ops.agentic-loop.agent.execution.loopFail3"
	modelEntityID := "acme.ops.model-registry.agent.endpoint.claude"

	event := &agentic.LoopFailedEvent{
		LoopID:       "loopFail3",
		TaskID:       "task-fail3",
		Outcome:      "failed",
		Role:         "editor",
		Model:        "claude",
		Iterations:   2,
		TokensIn:     500,
		TokensOut:    100,
		WorkflowSlug: "code-review",
		WorkflowStep: "revise",
		UserID:       "user-abc",
		FailedAt:     time.Now(),
	}

	cost := 0.005
	triples := buildLoopFailureTriples(loopEntityID, event, modelEntityID, cost, false)
	facts := predicateSet(triples)

	spawnOnly := []string{
		agvocab.LoopRole,
		agvocab.LoopTask,
		agvocab.LoopWorkflow,
		agvocab.LoopWorkflowStep,
		agvocab.LoopUser,
	}
	for _, pred := range spawnOnly {
		if facts[pred] {
			t.Errorf("predicate %s leaked into failure stamp; should be spawn-only", pred)
		}
	}

	// Completion-shape signals must remain.
	if !facts[agvocab.LoopCostUSD] {
		t.Errorf("expected %s to be present in failure stamp", agvocab.LoopCostUSD)
	}
	if !facts[agvocab.LoopModelUsed] {
		t.Errorf("expected %s to be present in failure stamp", agvocab.LoopModelUsed)
	}
	if got := objectFor(triples, agvocab.LoopModelUsed); got != modelEntityID {
		t.Errorf("%s: got %v, want %q", agvocab.LoopModelUsed, got, modelEntityID)
	}
}

func TestBuildLoopFailureTriples_EmptyModelOmitsModelUsed(t *testing.T) {
	loopEntityID := "acme.ops.agentic-loop.agent.execution.loopFail4"

	event := &agentic.LoopFailedEvent{
		LoopID:     "loopFail4",
		TaskID:     "task-fail4",
		Outcome:    "failed",
		Role:       "editor",
		Iterations: 1,
		FailedAt:   time.Now(),
	}

	triples := buildLoopFailureTriples(loopEntityID, event, "", 0, false)
	facts := predicateSet(triples)

	if facts[agvocab.LoopModelUsed] {
		t.Error("expected LoopModelUsed to be omitted when modelEntityID is empty")
	}
}

func TestBuildLoopCompletionTriples_EmptyModelOmitsModelUsed(t *testing.T) {
	loopEntityID := "acme.ops.agentic-loop.agent.execution.loopNoModel"

	event := &agentic.LoopCompletedEvent{
		LoopID:      "loopNoModel",
		TaskID:      "task-nomodel",
		Outcome:     "success",
		Role:        "architect",
		Iterations:  1,
		CompletedAt: time.Now(),
	}

	triples := buildLoopCompletionTriples(loopEntityID, event, "", 0, false)
	facts := predicateSet(triples)

	if facts[agvocab.LoopModelUsed] {
		t.Error("expected LoopModelUsed to be omitted when modelEntityID is empty")
	}
}

// --- buildLoopCancellationTriples ---

func TestBuildLoopCancellationTriples_RequiredFields(t *testing.T) {
	loopEntityID := "acme.ops.agentic-loop.agent.execution.loopCancel"

	event := &agentic.LoopCancelledEvent{
		LoopID:      "loopCancel",
		TaskID:      "task-cancel",
		Outcome:     "cancelled",
		CancelledBy: "user-abc",
		CancelledAt: time.Now(),
	}

	triples := buildLoopCancellationTriples(loopEntityID, event, false)
	facts := predicateSet(triples)

	required := []string{agvocab.LoopOutcome, agvocab.LoopEndedAt}
	for _, pred := range required {
		if !facts[pred] {
			t.Errorf("missing required predicate: %s", pred)
		}
	}

	// gh#159: task is spawn-only; cancellation stamp must not re-emit it.
	if facts[agvocab.LoopTask] {
		t.Errorf("%s leaked into cancellation stamp; should be spawn-only", agvocab.LoopTask)
	}

	if got := objectFor(triples, agvocab.LoopOutcome); got != "cancelled" {
		t.Errorf("%s: got %v, want cancelled", agvocab.LoopOutcome, got)
	}
}

// gh#159: workflow / workflow_step are spawn-only; cancellation stamp
// keeps only the transition signals (outcome, ended_at).
func TestBuildLoopCancellationTriples_SpawnFieldsNotInCancellation(t *testing.T) {
	loopEntityID := "acme.ops.agentic-loop.agent.execution.loopCancel2"

	event := &agentic.LoopCancelledEvent{
		LoopID:       "loopCancel2",
		TaskID:       "task-cancel2",
		Outcome:      "cancelled",
		WorkflowSlug: "feature-impl",
		WorkflowStep: "revise",
		CancelledAt:  time.Now(),
	}

	triples := buildLoopCancellationTriples(loopEntityID, event, false)
	facts := predicateSet(triples)

	spawnOnly := []string{
		agvocab.LoopTask,
		agvocab.LoopWorkflow,
		agvocab.LoopWorkflowStep,
	}
	for _, pred := range spawnOnly {
		if facts[pred] {
			t.Errorf("predicate %s leaked into cancellation stamp; should be spawn-only", pred)
		}
	}
}

func TestComputeCost(t *testing.T) {
	tests := []struct {
		name      string
		reg       model.RegistryReader
		endpoint  string
		tokensIn  int
		tokensOut int
		want      float64
	}{
		{
			name:     "nil registry returns zero",
			reg:      nil,
			endpoint: "claude",
			want:     0,
		},
		{
			name: "unknown endpoint returns zero",
			reg: &model.Registry{
				Endpoints: map[string]*model.EndpointConfig{},
				Defaults:  model.DefaultsConfig{Model: "default"},
			},
			endpoint: "nonexistent",
			want:     0,
		},
		{
			name: "zero token counts produce zero cost",
			reg: &model.Registry{
				Endpoints: map[string]*model.EndpointConfig{
					"claude": {
						Model:                  "claude-opus-4-5",
						InputPricePer1MTokens:  3.0,
						OutputPricePer1MTokens: 15.0,
					},
				},
			},
			endpoint:  "claude",
			tokensIn:  0,
			tokensOut: 0,
			want:      0,
		},
		{
			name: "standard cost calculation",
			reg: &model.Registry{
				Endpoints: map[string]*model.EndpointConfig{
					"claude": {
						Model:                  "claude-opus-4-5",
						InputPricePer1MTokens:  3.0,
						OutputPricePer1MTokens: 15.0,
					},
				},
			},
			endpoint:  "claude",
			tokensIn:  1000,
			tokensOut: 500,
			// (1000 * 3.0 + 500 * 15.0) / 1_000_000 = 0.0105
			want: 0.0105,
		},
		{
			name: "unprice endpoint returns zero cost",
			reg: &model.Registry{
				Endpoints: map[string]*model.EndpointConfig{
					"local": {
						Model: "llama3.2",
						// No pricing configured
					},
				},
			},
			endpoint:  "local",
			tokensIn:  5000,
			tokensOut: 1000,
			want:      0,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := computeCost(tc.reg, tc.endpoint, tc.tokensIn, tc.tokensOut)
			if math.Abs(got-tc.want) > 1e-9 {
				t.Errorf("got %.10f, want %.10f", got, tc.want)
			}
		})
	}
}

// --- resolveModelAccounting (#584: cost-usd + model-used provenance) ---

// TestResolveModelAccounting proves the completion/failure handler resolution
// seam: a loop's model name — a CAPABILITY for spawned loops — is resolved to
// its endpoint ONCE, and BOTH the cost and the model-used entity ID are keyed on
// that resolved endpoint. Pre-fix, a capability name missed GetEndpoint so cost
// was 0 (agent.loop.cost-usd omitted by the >0 gate) and the model-used triple
// pointed at the capability, not the real endpoint.
func TestResolveModelAccounting(t *testing.T) {
	const org, platform = "acme", "ops"

	// developer -> claude (priced); local is unpriced.
	reg := &model.Registry{
		Capabilities: map[string]*model.CapabilityConfig{
			"developer": {Preferred: []string{"claude"}},
			"cheap":     {Preferred: []string{"local"}},
		},
		Endpoints: map[string]*model.EndpointConfig{
			"claude": {
				Model:                  "claude-opus-4-5",
				InputPricePer1MTokens:  3.0,
				OutputPricePer1MTokens: 15.0,
			},
			"local": {Model: "llama3.2"}, // no pricing
		},
		Defaults: model.DefaultsConfig{Model: "claude"},
	}

	tests := []struct {
		name          string
		modelName     string
		tokensIn      int
		tokensOut     int
		wantEntity    string
		wantEntityFor string // endpoint name the entity ID must be keyed on
		wantCost      float64
	}{
		{
			name:          "capability resolves to priced endpoint (spawned loop)",
			modelName:     "developer",
			tokensIn:      1000,
			tokensOut:     500,
			wantEntityFor: "claude",
			// (1000*3.0 + 500*15.0)/1e6 = 0.0105
			wantCost: 0.0105,
		},
		{
			name:          "direct endpoint name unchanged (direct-model loop)",
			modelName:     "claude",
			tokensIn:      1000,
			tokensOut:     500,
			wantEntityFor: "claude",
			wantCost:      0.0105,
		},
		{
			name:          "capability to unpriced endpoint -> zero cost",
			modelName:     "cheap",
			tokensIn:      5000,
			tokensOut:     1000,
			wantEntityFor: "local",
			wantCost:      0,
		},
		{
			name:          "unknown name unchanged, unpriced -> entity for raw name, zero cost",
			modelName:     "mystery-model",
			tokensIn:      1000,
			tokensOut:     500,
			wantEntityFor: "mystery-model",
			wantCost:      0,
		},
		{
			name:       "empty model omits entity and zeroes cost",
			modelName:  "",
			tokensIn:   1000,
			tokensOut:  500,
			wantEntity: "",
			wantCost:   0,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			gotEntity, gotCost := resolveModelAccounting(reg, org, platform, tc.modelName, tc.tokensIn, tc.tokensOut)

			wantEntity := tc.wantEntity
			if tc.wantEntityFor != "" {
				wantEntity = agentic.ModelEndpointEntityID(org, platform, tc.wantEntityFor)
			}
			if gotEntity != wantEntity {
				t.Errorf("modelEntityID = %q, want %q", gotEntity, wantEntity)
			}
			if math.Abs(gotCost-tc.wantCost) > 1e-9 {
				t.Errorf("cost = %.10f, want %.10f", gotCost, tc.wantCost)
			}
		})
	}
}

// --- buildLineageTriples ---

// TestBuildLineageTriples_StampsLineagePredicates verifies that each
// entry in the RelatedLoops map (typed map[string]any after JSON
// round-trip through BaseMessage) becomes one triple of the form
// <loopEntityID> agent.lineage.<role-key> <upstream loop ID>.
func TestBuildLineageTriples_StampsLineagePredicates(t *testing.T) {
	loopEntityID := "acme.ops.agentic-loop.agent.execution.architect-loop-001"
	related := map[string]any{
		"researcher": "loop-research-abc",
		"planner":    "loop-plan-xyz",
	}

	triples, err := buildLineageTriples(loopEntityID, related)
	if err != nil {
		t.Fatal(err)
	}

	if len(triples) != 2 {
		t.Fatalf("expected 2 triples, got %d", len(triples))
	}

	for _, tr := range triples {
		if tr.Subject != loopEntityID {
			t.Errorf("subject = %q, want %q", tr.Subject, loopEntityID)
		}
		if tr.Source != graphWriterSource {
			t.Errorf("source = %q, want %q", tr.Source, graphWriterSource)
		}
		if tr.Confidence != 1.0 {
			t.Errorf("confidence = %v, want 1.0", tr.Confidence)
		}
	}

	facts := predicateSet(triples)
	wantPredicates := []string{
		semantictest.Predicate(t, "agent", "lineage", "researcher"),
		semantictest.Predicate(t, "agent", "lineage", "planner"),
	}
	for _, want := range wantPredicates {
		if !facts[want] {
			t.Errorf("missing expected predicate %q in %v", want, facts)
		}
	}

	// Object pairings — predicate→object must round-trip.
	for _, tr := range triples {
		switch tr.Predicate {
		case semantictest.Predicate(t, "agent", "lineage", "researcher"):
			if tr.Object != "loop-research-abc" {
				t.Errorf("researcher object = %v, want loop-research-abc", tr.Object)
			}
		case semantictest.Predicate(t, "agent", "lineage", "planner"):
			if tr.Object != "loop-plan-xyz" {
				t.Errorf("planner object = %v, want loop-plan-xyz", tr.Object)
			}
		default:
			t.Errorf("unexpected predicate: %q", tr.Predicate)
		}
	}
}

// TestBuildLineageTriples_EmptyAndNilNoops verifies that nil and
// empty maps produce no triples (back-compat: products that don't
// opt into RelatedLoops see no graph mutation).
func TestBuildLineageTriples_EmptyAndNilNoops(t *testing.T) {
	loopEntityID := "acme.ops.agentic-loop.agent.execution.x"

	if got, err := buildLineageTriples(loopEntityID, nil); err != nil || got != nil {
		t.Errorf("nil related: got %d triples, want 0 (nil)", len(got))
	}
	if got, err := buildLineageTriples(loopEntityID, map[string]any{}); err != nil || got != nil {
		t.Errorf("empty related: got %d triples, want 0 (nil)", len(got))
	}
}

// TestBuildLineageTriples_MalformedBatchRejectedAtomically verifies that one
// malformed entry rejects every sibling rather than being silently skipped.
func TestBuildLineageTriples_MalformedBatchRejectedAtomically(t *testing.T) {
	loopEntityID := "acme.ops.agentic-loop.agent.execution.x"
	tests := []struct {
		name    string
		subject string
		related map[string]any
	}{
		{name: "non-string", subject: loopEntityID, related: map[string]any{"researcher": "valid", "reviewer": 42}},
		{name: "empty", subject: loopEntityID, related: map[string]any{"researcher": "valid", "reviewer": ""}},
		{name: "invalid role key", subject: loopEntityID, related: map[string]any{"researcher": "valid", "bad_key": "loop"}},
		{name: "invalid subject", subject: "not-an-entity", related: map[string]any{"researcher": "valid"}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			triples, err := buildLineageTriples(test.subject, test.related)
			if err == nil {
				t.Fatal("buildLineageTriples error = nil, want rejection")
			}
			if triples != nil {
				t.Fatalf("triples = %#v, want no partial batch", triples)
			}
		})
	}
}

// TestBuildLineageTriples_PredicateNamespace verifies that all generated
// predicates use the fixed namespace exposed in agentic/tools.go.
// This is the contract ops-agent (ADR-027) and the operating-curve
// observability primitives (ADR-033) rely on for cross-arc / cross-
// run aggregation. Drift here breaks consumer aggregation queries.
func TestBuildLineageTriples_PredicateNamespace(t *testing.T) {
	loopEntityID := "acme.ops.agentic-loop.agent.execution.x"
	related := map[string]any{
		"researcher":        "loop-research",
		"research-reviewer": "loop-other",
	}

	triples, err := buildLineageTriples(loopEntityID, related)
	if err != nil {
		t.Fatal(err)
	}

	for _, tr := range triples {
		if !strings.HasPrefix(tr.Predicate, agentic.LineageTripleNamespace+".") {
			t.Errorf("predicate %q missing namespace %q", tr.Predicate, agentic.LineageTripleNamespace)
		}
	}
}

func TestWriteLineageTriplesPropagatesTypedPreflightFailureBeforeIO(t *testing.T) {
	w := &graphWriter{platform: types.PlatformMeta{Org: "acme", Platform: "ops"}}
	err := w.WriteLineageTriples(context.Background(), "loop-1", map[string]any{
		"researcher": "upstream",
		"reviewer":   42,
	})
	if err == nil || !errs.IsInvalid(err) {
		t.Fatalf("WriteLineageTriples error = %v, want typed invalid rejection", err)
	}
}

// TestBuildLoopFailureTriples_TerminalReasonDistinguishesFailureClasses pins
// the gh#569 acceptance: budget exhaustion and a transient model error both
// stamp outcome="failed" but MUST carry distinct terminal-reason facts so a
// rule can route on WHY (escalate vs retry). An event with no classified
// reason stamps no terminal-reason triple at all.
func TestBuildLoopFailureTriples_TerminalReasonDistinguishesFailureClasses(t *testing.T) {
	loopEntityID := "acme.ops.agentic-loop.agent.execution.loopReason"

	failedWith := func(reason string) []message.Triple {
		return buildLoopFailureTriples(loopEntityID, &agentic.LoopFailedEvent{
			LoopID:   "loopReason",
			Outcome:  "failed",
			Reason:   reason,
			FailedAt: time.Now(),
		}, "", 0, false)
	}

	exhausted := failedWith("max_iterations")
	if got := objectFor(exhausted, agvocab.LoopTerminalReason); got != "max_iterations" {
		t.Errorf("%s after budget exhaustion: got %v, want max_iterations", agvocab.LoopTerminalReason, got)
	}

	transient := failedWith("model_error")
	if got := objectFor(transient, agvocab.LoopTerminalReason); got != "model_error" {
		t.Errorf("%s after transient model error: got %v, want model_error", agvocab.LoopTerminalReason, got)
	}

	// The two failure classes must be distinguishable at the fact level —
	// the whole point of gh#569.
	if objectFor(exhausted, agvocab.LoopTerminalReason) == objectFor(transient, agvocab.LoopTerminalReason) {
		t.Error("exhaustion and model-error failures carry identical terminal-reason facts")
	}
	// Outcome alone must NOT distinguish them (that's the gap being closed).
	if objectFor(exhausted, agvocab.LoopOutcome) != objectFor(transient, agvocab.LoopOutcome) {
		t.Error("fixture drift: both classes should stamp the same outcome value")
	}

	unclassified := failedWith("")
	if predicateSet(unclassified)[agvocab.LoopTerminalReason] {
		t.Errorf("%s must be absent when the event carries no classified reason", agvocab.LoopTerminalReason)
	}
}

// --- agent.loop.evidence-integrity (observed audit loss) ---

// terminalTripleBuilders names the three terminal paths that carry
// agent.loop.outcome. Every one of them must be able to report observed
// audit loss: a loop that FAILED or was CANCELLED can have lost evidence
// exactly as a completed one can, and the completion path alone would
// leave two thirds of terminal loops silently unclassifiable.
func terminalTripleBuilders(loopEntityID string) map[string]func(evidenceIncomplete bool) []message.Triple {
	now := time.Now()
	return map[string]func(bool) []message.Triple{
		"completion": func(evidenceIncomplete bool) []message.Triple {
			return buildLoopCompletionTriples(loopEntityID, &agentic.LoopCompletedEvent{
				LoopID: "loop123", Outcome: "success", Iterations: 2, CompletedAt: now,
			}, "", 0, evidenceIncomplete)
		},
		"failure": func(evidenceIncomplete bool) []message.Triple {
			return buildLoopFailureTriples(loopEntityID, &agentic.LoopFailedEvent{
				LoopID: "loop123", Outcome: "failed", Reason: "model_error", FailedAt: now,
			}, "", 0, evidenceIncomplete)
		},
		"cancellation": func(evidenceIncomplete bool) []message.Triple {
			return buildLoopCancellationTriples(loopEntityID, &agentic.LoopCancelledEvent{
				LoopID: "loop123", Outcome: agentic.OutcomeCancelled, CancelledAt: now,
			}, evidenceIncomplete)
		},
	}
}

// Spec: "a loop with observed audit loss is machine-readable as incomplete"
// — on EVERY terminal path, and on the same mutation that carries outcome.
func TestBuildLoopTerminalTriples_ObservedAuditLossStampsIncomplete(t *testing.T) {
	loopEntityID := "acme.ops.agentic-loop.agent.execution.loop123"

	for name, build := range terminalTripleBuilders(loopEntityID) {
		t.Run(name, func(t *testing.T) {
			triples := build(true)

			var conditions []message.Triple
			for _, tr := range triples {
				if tr.Predicate == agvocab.LoopEvidenceIntegrity {
					conditions = append(conditions, tr)
				}
			}
			if len(conditions) != 1 {
				t.Fatalf("got %d %s triples, want exactly 1", len(conditions), agvocab.LoopEvidenceIntegrity)
			}
			// Literal, not the constant: a silent value change is the
			// fabrication risk this predicate exists to avoid.
			if conditions[0].Object != "incomplete" {
				t.Errorf("object = %v, want %q", conditions[0].Object, "incomplete")
			}
			if conditions[0].Subject != loopEntityID {
				t.Errorf("subject = %q, want the loop execution entity %q", conditions[0].Subject, loopEntityID)
			}

			// "written on the same mutation that carries agent.loop.outcome,
			// not a separate write" — one slice reaches writeBatch as one
			// append request, so co-membership IS the atomicity assertion.
			if !predicateSet(triples)[agvocab.LoopOutcome] {
				t.Errorf("condition is not in the triple set carrying %s", agvocab.LoopOutcome)
			}
		})
	}
}

// Spec: "a loop with no observed audit loss carries no claim" — absence,
// never a positive completeness assertion.
func TestBuildLoopTerminalTriples_NoObservedLossCarriesNoClaim(t *testing.T) {
	loopEntityID := "acme.ops.agentic-loop.agent.execution.loop123"

	for name, build := range terminalTripleBuilders(loopEntityID) {
		t.Run(name, func(t *testing.T) {
			triples := build(false)

			for _, tr := range triples {
				if tr.Predicate == agvocab.LoopEvidenceIntegrity {
					t.Errorf("unobserved loss stamped %s = %v; absence is the contract",
						agvocab.LoopEvidenceIntegrity, tr.Object)
				}
			}
		})
	}
}

// The framework never writes a completeness claim on ANY path, with or
// without observed loss. "complete" is the value ADR-084 forbids: the
// component can only know the failures it saw.
func TestBuildLoopTerminalTriples_NeverWritesComplete(t *testing.T) {
	loopEntityID := "acme.ops.agentic-loop.agent.execution.loop123"

	for name, build := range terminalTripleBuilders(loopEntityID) {
		for _, observed := range []bool{true, false} {
			t.Run(fmt.Sprintf("%s/observed=%v", name, observed), func(t *testing.T) {
				for _, tr := range build(observed) {
					if tr.Predicate != agvocab.LoopEvidenceIntegrity {
						continue
					}
					if tr.Object == "complete" {
						t.Errorf("%s = \"complete\" written; the framework never asserts completeness",
							agvocab.LoopEvidenceIntegrity)
					}
				}
			})
		}
	}
}

// The condition is unqualified: no stage, kind, reason, or attempt rides
// the triple. Electing one of several failed stages would manufacture a
// claim about which mattered.
func TestBuildLoopTerminalTriples_ConditionCarriesNoQualifier(t *testing.T) {
	loopEntityID := "acme.ops.agentic-loop.agent.execution.loop123"
	qualifiers := []string{
		string(trajectoryStageEvidencePut), string(trajectoryStageFactCreate),
		string(trajectoryReasonBackend), string(trajectoryReasonTimeout),
		string(agentic.TrajectoryKindLoopTerminal),
	}

	for name, build := range terminalTripleBuilders(loopEntityID) {
		t.Run(name, func(t *testing.T) {
			for _, tr := range build(true) {
				if tr.Predicate != agvocab.LoopEvidenceIntegrity {
					continue
				}
				object, ok := tr.Object.(string)
				if !ok {
					t.Fatalf("object type = %T, want string", tr.Object)
				}
				for _, qualifier := range qualifiers {
					if strings.Contains(object, qualifier) {
						t.Errorf("condition object %q carries qualifier %q", object, qualifier)
					}
				}
			}
		})
	}
}

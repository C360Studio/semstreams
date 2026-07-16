package agenticloop

import (
	"math"
	"strings"
	"testing"
	"time"
	"unicode/utf8"

	"github.com/c360studio/semstreams/agentic"
	"github.com/c360studio/semstreams/message"
	"github.com/c360studio/semstreams/model"
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

// --- buildModelEndpointTriples ---

func TestBuildModelEndpointTriples_RequiredFields(t *testing.T) {
	entityID := "acme.ops.agent.model-registry.endpoint.claude"
	ep := model.EndpointConfig{
		Provider:      "anthropic",
		Model:         "claude-opus-4-5",
		SupportsTools: true,
	}

	triples := buildModelEndpointTriples(entityID, ep)

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
	entityID := "acme.ops.agent.model-registry.endpoint.local"
	ep := model.EndpointConfig{
		Provider: "ollama",
		Model:    "llama3.2",
		// MaxTokens, pricing, URL, rate limit all zero/empty
	}

	triples := buildModelEndpointTriples(entityID, ep)
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
	entityID := "acme.ops.agent.model-registry.endpoint.gpt4o"
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

	triples := buildModelEndpointTriples(entityID, ep)
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
	loopEntityID := "acme.ops.agent.agentic-loop.execution.loop123"
	modelEntityID := "acme.ops.agent.model-registry.endpoint.claude"

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

	triples := buildLoopCompletionTriples(loopEntityID, event, modelEntityID, 0)

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
	loopEntityID := "acme.ops.agent.agentic-loop.execution.loop456"
	modelEntityID := "acme.ops.agent.model-registry.endpoint.claude"

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
	triples := buildLoopCompletionTriples(loopEntityID, event, modelEntityID, cost)

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
	loopEntityID := "acme.ops.agent.agentic-loop.execution.loop789"
	modelEntityID := "acme.ops.agent.model-registry.endpoint.local"

	event := &agentic.LoopCompletedEvent{
		LoopID:      "loop789",
		TaskID:      "task-ghi",
		Outcome:     "success",
		Role:        "reviewer",
		Model:       "local",
		Iterations:  1,
		CompletedAt: time.Now(),
	}

	triples := buildLoopCompletionTriples(loopEntityID, event, modelEntityID, 0)
	facts := predicateSet(triples)

	if facts[agvocab.LoopCostUSD] {
		t.Error("expected LoopCostUSD to be omitted when cost is 0")
	}
}

func TestBuildLoopCompletionTriples_OptionalFieldsOmittedWhenEmpty(t *testing.T) {
	loopEntityID := "acme.ops.agent.agentic-loop.execution.loopA"
	modelEntityID := "acme.ops.agent.model-registry.endpoint.claude"

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

	triples := buildLoopCompletionTriples(loopEntityID, event, modelEntityID, 0)
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
	loopEntityID := "acme.ops.agent.agentic-loop.execution.loopD"
	modelEntityID := "acme.ops.agent.model-registry.endpoint.claude"

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

	triples := buildLoopCompletionTriples(loopEntityID, event, modelEntityID, 0)

	if predicateSet(triples)[agvocab.LoopDescription] {
		t.Errorf("%s leaked into completion stamp; should be spawn-only", agvocab.LoopDescription)
	}
}

func TestBuildLoopFailureTriples_DescriptionNotInFailure(t *testing.T) {
	loopEntityID := "acme.ops.agent.agentic-loop.execution.loopE"
	modelEntityID := "acme.ops.agent.model-registry.endpoint.claude"

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

	triples := buildLoopFailureTriples(loopEntityID, event, modelEntityID, 0)

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
	loopEntityID := "acme.ops.agent.agentic-loop.execution.loopB"
	modelEntityID := "acme.ops.agent.model-registry.endpoint.claude"

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

	triples := buildLoopCompletionTriples(loopEntityID, event, modelEntityID, 0)
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
	loopEntityID := "acme.ops.agent.agentic-loop.execution.loopFail"
	modelEntityID := "acme.ops.agent.model-registry.endpoint.claude"

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

	triples := buildLoopFailureTriples(loopEntityID, event, modelEntityID, 0)
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
	loopEntityID := "acme.ops.agent.agentic-loop.execution.loopFail2"
	modelEntityID := "acme.ops.agent.model-registry.endpoint.claude"

	event := &agentic.LoopFailedEvent{
		LoopID:     "loopFail2",
		TaskID:     "task-fail2",
		Outcome:    "failed",
		Role:       "editor",
		Model:      "claude",
		Iterations: 1,
		FailedAt:   time.Now(),
	}

	triples := buildLoopFailureTriples(loopEntityID, event, modelEntityID, 0)
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
	loopEntityID := "acme.ops.agent.agentic-loop.execution.loopFailChild"
	modelEntityID := "acme.ops.agent.model-registry.endpoint.claude"

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

	triples := buildLoopFailureTriples(loopEntityID, event, modelEntityID, 0)
	if predicateSet(triples)[agvocab.LoopParent] {
		t.Errorf("%s leaked into failure stamp; should be spawn-only", agvocab.LoopParent)
	}
}

// gh#159: workflow / workflow_step / user are spawn-only; failure stamp
// keeps only the completion-shape signals (outcome, iterations, tokens,
// cost, model_used, ended_at).
func TestBuildLoopFailureTriples_SpawnFieldsNotInFailure(t *testing.T) {
	loopEntityID := "acme.ops.agent.agentic-loop.execution.loopFail3"
	modelEntityID := "acme.ops.agent.model-registry.endpoint.claude"

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
	triples := buildLoopFailureTriples(loopEntityID, event, modelEntityID, cost)
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
	loopEntityID := "acme.ops.agent.agentic-loop.execution.loopFail4"

	event := &agentic.LoopFailedEvent{
		LoopID:     "loopFail4",
		TaskID:     "task-fail4",
		Outcome:    "failed",
		Role:       "editor",
		Iterations: 1,
		FailedAt:   time.Now(),
	}

	triples := buildLoopFailureTriples(loopEntityID, event, "", 0)
	facts := predicateSet(triples)

	if facts[agvocab.LoopModelUsed] {
		t.Error("expected LoopModelUsed to be omitted when modelEntityID is empty")
	}
}

func TestBuildLoopCompletionTriples_EmptyModelOmitsModelUsed(t *testing.T) {
	loopEntityID := "acme.ops.agent.agentic-loop.execution.loopNoModel"

	event := &agentic.LoopCompletedEvent{
		LoopID:      "loopNoModel",
		TaskID:      "task-nomodel",
		Outcome:     "success",
		Role:        "architect",
		Iterations:  1,
		CompletedAt: time.Now(),
	}

	triples := buildLoopCompletionTriples(loopEntityID, event, "", 0)
	facts := predicateSet(triples)

	if facts[agvocab.LoopModelUsed] {
		t.Error("expected LoopModelUsed to be omitted when modelEntityID is empty")
	}
}

// --- buildLoopCancellationTriples ---

func TestBuildLoopCancellationTriples_RequiredFields(t *testing.T) {
	loopEntityID := "acme.ops.agent.agentic-loop.execution.loopCancel"

	event := &agentic.LoopCancelledEvent{
		LoopID:      "loopCancel",
		TaskID:      "task-cancel",
		Outcome:     "cancelled",
		CancelledBy: "user-abc",
		CancelledAt: time.Now(),
	}

	triples := buildLoopCancellationTriples(loopEntityID, event)
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
	loopEntityID := "acme.ops.agent.agentic-loop.execution.loopCancel2"

	event := &agentic.LoopCancelledEvent{
		LoopID:       "loopCancel2",
		TaskID:       "task-cancel2",
		Outcome:      "cancelled",
		WorkflowSlug: "feature-impl",
		WorkflowStep: "revise",
		CancelledAt:  time.Now(),
	}

	triples := buildLoopCancellationTriples(loopEntityID, event)
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

// --- buildTrajectoryStepTriples ---

func TestBuildTrajectoryStepTriples_NilTrajectory(t *testing.T) {
	triples := buildTrajectoryStepTriples("acme.ops.agent.agentic-loop.execution.loop1", "acme", "ops", "loop1", nil)
	if len(triples) != 0 {
		t.Errorf("expected no triples for nil trajectory, got %d", len(triples))
	}
}

func TestBuildTrajectoryStepTriples_EmptySteps(t *testing.T) {
	traj := &agentic.Trajectory{LoopID: "loop1", Steps: []agentic.TrajectoryStep{}}
	triples := buildTrajectoryStepTriples("acme.ops.agent.agentic-loop.execution.loop1", "acme", "ops", "loop1", traj)
	if len(triples) != 0 {
		t.Errorf("expected no triples for empty steps, got %d", len(triples))
	}
}

func TestBuildTrajectoryStepTriples_ContextCompaction(t *testing.T) {
	loopEntityID := "acme.ops.agent.agentic-loop.execution.loop1"
	traj := &agentic.Trajectory{
		LoopID: "loop1",
		Steps: []agentic.TrajectoryStep{
			{
				Timestamp:   time.Now(),
				StepType:    "context_compaction",
				TokensIn:    12000,
				TokensOut:   800,
				Model:       "claude-haiku",
				Utilization: 0.72,
				Duration:    100,
			},
		},
	}
	triples := buildTrajectoryStepTriples(loopEntityID, "acme", "ops", "loop1", traj)

	stepEntityID := "acme.ops.agent.agentic-loop.step.loop1-0"

	var stepTriples []message.Triple
	var loopTriples []message.Triple
	for _, tr := range triples {
		if tr.Subject == stepEntityID {
			stepTriples = append(stepTriples, tr)
		}
		if tr.Subject == loopEntityID {
			loopTriples = append(loopTriples, tr)
		}
	}

	// Verify compaction-specific predicates
	preds := predicateSet(stepTriples)
	required := []string{
		agvocab.StepType, agvocab.StepIndex, agvocab.StepLoop,
		agvocab.StepTimestamp, agvocab.StepDuration,
		agvocab.StepTokensEvicted, agvocab.StepTokensSummarized,
		agvocab.StepModel, agvocab.StepUtilization,
	}
	for _, pred := range required {
		if !preds[pred] {
			t.Errorf("missing step predicate: %s", pred)
		}
	}

	if got := objectFor(stepTriples, agvocab.StepType); got != "context_compaction" {
		t.Errorf("StepType: got %v, want context_compaction", got)
	}
	if got := objectFor(stepTriples, agvocab.StepTokensEvicted); got != 12000 {
		t.Errorf("StepTokensEvicted: got %v, want 12000", got)
	}
	if got := objectFor(stepTriples, agvocab.StepTokensSummarized); got != 800 {
		t.Errorf("StepTokensSummarized: got %v, want 800", got)
	}
	if got := objectFor(stepTriples, agvocab.StepModel); got != "claude-haiku" {
		t.Errorf("StepModel: got %v, want claude-haiku", got)
	}
	if got := objectFor(stepTriples, agvocab.StepUtilization); got != 0.72 {
		t.Errorf("StepUtilization: got %v, want 0.72", got)
	}

	// Should NOT have model_call or tool_call specific predicates
	if preds[agvocab.StepTokensIn] {
		t.Error("unexpected StepTokensIn on compaction step")
	}
	if preds[agvocab.StepToolName] {
		t.Error("unexpected StepToolName on compaction step")
	}

	// Verify LoopHasStep triple
	if len(loopTriples) != 1 {
		t.Errorf("expected 1 LoopHasStep triple, got %d", len(loopTriples))
	}
}

func TestBuildTrajectoryStepTriples_ToolCallStep(t *testing.T) {
	loopEntityID := "acme.ops.agent.agentic-loop.execution.loop1"
	traj := &agentic.Trajectory{
		LoopID: "loop1",
		Steps: []agentic.TrajectoryStep{
			{
				Timestamp:     time.Date(2026, 3, 17, 14, 0, 0, 0, time.UTC),
				StepType:      "tool_call",
				ToolName:      "web_search",
				ToolArguments: map[string]any{"query": "test"},
				ToolResult:    "some results",
				Duration:      1500,
			},
		},
	}

	triples := buildTrajectoryStepTriples(loopEntityID, "acme", "ops", "loop1", traj)

	// Should have step triples + 1 LoopHasStep triple
	stepEntityID := "acme.ops.agent.agentic-loop.step.loop1-0"

	// Find step triples (Subject == stepEntityID)
	var stepTriples []message.Triple
	var loopTriples []message.Triple
	for _, tr := range triples {
		if tr.Subject == stepEntityID {
			stepTriples = append(stepTriples, tr)
		}
		if tr.Subject == loopEntityID {
			loopTriples = append(loopTriples, tr)
		}
	}

	// Verify step metadata triples
	preds := predicateSet(stepTriples)
	required := []string{
		agvocab.StepType, agvocab.StepIndex, agvocab.StepLoop,
		agvocab.StepTimestamp, agvocab.StepDuration, agvocab.StepToolName,
	}
	for _, pred := range required {
		if !preds[pred] {
			t.Errorf("missing step predicate: %s", pred)
		}
	}

	if got := objectFor(stepTriples, agvocab.StepType); got != "tool_call" {
		t.Errorf("StepType: got %v, want tool_call", got)
	}
	if got := objectFor(stepTriples, agvocab.StepToolName); got != "web_search" {
		t.Errorf("StepToolName: got %v, want web_search", got)
	}
	if got := objectFor(stepTriples, agvocab.StepIndex); got != 0 {
		t.Errorf("StepIndex: got %v, want 0", got)
	}
	if got := objectFor(stepTriples, agvocab.StepLoop); got != loopEntityID {
		t.Errorf("StepLoop: got %v, want %s", got, loopEntityID)
	}

	// Verify LoopHasStep triple
	if len(loopTriples) != 1 {
		t.Fatalf("expected 1 LoopHasStep triple, got %d", len(loopTriples))
	}
	if loopTriples[0].Predicate != agvocab.LoopHasStep {
		t.Errorf("expected LoopHasStep predicate, got %s", loopTriples[0].Predicate)
	}
	if loopTriples[0].Object != stepEntityID {
		t.Errorf("LoopHasStep object: got %v, want %s", loopTriples[0].Object, stepEntityID)
	}
}

func TestBuildTrajectoryStepTriples_ModelCallStep(t *testing.T) {
	loopEntityID := "acme.ops.agent.agentic-loop.execution.loop2"
	traj := &agentic.Trajectory{
		LoopID: "loop2",
		Steps: []agentic.TrajectoryStep{
			{
				Timestamp: time.Date(2026, 3, 17, 14, 0, 0, 0, time.UTC),
				StepType:  "model_call",
				Model:     "claude-sonnet",
				TokensIn:  4832,
				TokensOut: 819,
				Duration:  3200,
			},
		},
	}

	triples := buildTrajectoryStepTriples(loopEntityID, "acme", "ops", "loop2", traj)

	stepEntityID := "acme.ops.agent.agentic-loop.step.loop2-0"
	var stepTriples []message.Triple
	for _, tr := range triples {
		if tr.Subject == stepEntityID {
			stepTriples = append(stepTriples, tr)
		}
	}

	preds := predicateSet(stepTriples)
	required := []string{
		agvocab.StepType, agvocab.StepIndex, agvocab.StepLoop,
		agvocab.StepTimestamp, agvocab.StepDuration,
		agvocab.StepModel, agvocab.StepTokensIn, agvocab.StepTokensOut,
	}
	for _, pred := range required {
		if !preds[pred] {
			t.Errorf("missing step predicate: %s", pred)
		}
	}

	// Tool-specific predicates should NOT be present
	if preds[agvocab.StepToolName] {
		t.Error("StepToolName should not be present for model_call")
	}

	if got := objectFor(stepTriples, agvocab.StepModel); got != "claude-sonnet" {
		t.Errorf("StepModel: got %v, want claude-sonnet", got)
	}
	if got := objectFor(stepTriples, agvocab.StepTokensIn); got != 4832 {
		t.Errorf("StepTokensIn: got %v, want 4832", got)
	}
	if got := objectFor(stepTriples, agvocab.StepTokensOut); got != 819 {
		t.Errorf("StepTokensOut: got %v, want 819", got)
	}
}

func TestBuildTrajectoryStepTriples_MixedSteps(t *testing.T) {
	loopEntityID := "acme.ops.agent.agentic-loop.execution.loop3"
	traj := &agentic.Trajectory{
		LoopID: "loop3",
		Steps: []agentic.TrajectoryStep{
			{Timestamp: time.Now(), StepType: "model_call", Model: "claude", TokensIn: 100, TokensOut: 50, Duration: 1000},
			{Timestamp: time.Now(), StepType: "tool_call", ToolName: "graph_query", ToolResult: "data", Duration: 200},
			{Timestamp: time.Now(), StepType: "context_compaction", Duration: 50},
			{Timestamp: time.Now(), StepType: "model_call", Model: "claude", TokensIn: 200, TokensOut: 100, Duration: 1500},
		},
	}

	triples := buildTrajectoryStepTriples(loopEntityID, "acme", "ops", "loop3", traj)

	// Count LoopHasStep triples — should be 4 (compaction included)
	var loopHasStepCount int
	for _, tr := range triples {
		if tr.Subject == loopEntityID && tr.Predicate == agvocab.LoopHasStep {
			loopHasStepCount++
		}
	}
	if loopHasStepCount != 4 {
		t.Errorf("expected 4 LoopHasStep triples, got %d", loopHasStepCount)
	}

	// Step indices should be 0, 1, 2, 3 (compaction at index 2 now included)
	expectedStepIDs := []string{
		"acme.ops.agent.agentic-loop.step.loop3-0",
		"acme.ops.agent.agentic-loop.step.loop3-1",
		"acme.ops.agent.agentic-loop.step.loop3-2",
		"acme.ops.agent.agentic-loop.step.loop3-3",
	}
	for _, expectedID := range expectedStepIDs {
		found := false
		for _, tr := range triples {
			if tr.Subject == loopEntityID && tr.Object == expectedID {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("missing LoopHasStep for %s", expectedID)
		}
	}
}

// --- computeCost ---

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

// --- buildLineageTriples ---

// TestBuildLineageTriples_StampsLineagePredicates verifies that each
// entry in the RelatedLoops map (typed map[string]any after JSON
// round-trip through BaseMessage) becomes one triple of the form
// <loopEntityID> lineage.<roleKey> <upstream loop ID>.
func TestBuildLineageTriples_StampsLineagePredicates(t *testing.T) {
	loopEntityID := "acme.ops.agent.agentic-loop.execution.architect-loop-001"
	related := map[string]any{
		"researcher": "loop-research-abc",
		"planner":    "loop-plan-xyz",
	}

	triples := buildLineageTriples(loopEntityID, related)

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
		agentic.LineageTriplePredicate("researcher"),
		agentic.LineageTriplePredicate("planner"),
	}
	for _, want := range wantPredicates {
		if !facts[want] {
			t.Errorf("missing expected predicate %q in %v", want, facts)
		}
	}

	// Object pairings — predicate→object must round-trip.
	for _, tr := range triples {
		switch tr.Predicate {
		case agentic.LineageTriplePredicate("researcher"):
			if tr.Object != "loop-research-abc" {
				t.Errorf("researcher object = %v, want loop-research-abc", tr.Object)
			}
		case agentic.LineageTriplePredicate("planner"):
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
	loopEntityID := "acme.ops.agent.agentic-loop.execution.x"

	if got := buildLineageTriples(loopEntityID, nil); got != nil {
		t.Errorf("nil related: got %d triples, want 0 (nil)", len(got))
	}
	if got := buildLineageTriples(loopEntityID, map[string]any{}); got != nil {
		t.Errorf("empty related: got %d triples, want 0 (nil)", len(got))
	}
}

// TestBuildLineageTriples_NonStringValuesSkipped verifies defensive
// dropping of malformed entries. The producer-side type is
// map[string]string so non-strings should never appear, but defensive
// skipping keeps a malformed product / future schema bug from
// polluting the graph with garbage triples.
func TestBuildLineageTriples_NonStringValuesSkipped(t *testing.T) {
	loopEntityID := "acme.ops.agent.agentic-loop.execution.x"
	related := map[string]any{
		"researcher": "loop-research-abc",
		"planner":    42, // wrong type — should be skipped
		"reviewer":   "", // empty string — should be skipped
		"architect":  nil,
	}

	triples := buildLineageTriples(loopEntityID, related)

	if len(triples) != 1 {
		t.Fatalf("expected 1 triple (only valid string entry survives), got %d", len(triples))
	}
	if triples[0].Predicate != agentic.LineageTriplePredicate("researcher") {
		t.Errorf("predicate = %q, want %q",
			triples[0].Predicate, agentic.LineageTriplePredicate("researcher"))
	}
	if triples[0].Object != "loop-research-abc" {
		t.Errorf("object = %v, want loop-research-abc", triples[0].Object)
	}
}

// TestBuildLineageTriples_PredicatePrefix verifies that all generated
// predicates use the LineageTriplePrefix exposed in agentic/tools.go.
// This is the contract ops-agent (ADR-027) and the operating-curve
// observability primitives (ADR-033) rely on for cross-arc / cross-
// run aggregation. Drift here breaks consumer aggregation queries.
func TestBuildLineageTriples_PredicatePrefix(t *testing.T) {
	loopEntityID := "acme.ops.agent.agentic-loop.execution.x"
	related := map[string]any{
		"researcher":             "loop-research",
		"some.dotted.role.label": "loop-other",
	}

	triples := buildLineageTriples(loopEntityID, related)

	for _, tr := range triples {
		if !strings.HasPrefix(tr.Predicate, agentic.LineageTriplePrefix) {
			t.Errorf("predicate %q missing prefix %q", tr.Predicate, agentic.LineageTriplePrefix)
		}
	}
}

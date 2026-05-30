package agenticloop

// graph_writer_ops_test.go — Phase 0a: Ops Agent query-readiness tests.
//
// These tests verify that the triples emitted by graph_writer contain
// the data an ops agent needs to diagnose execution patterns. Each test
// represents a specific query the ops agent must be able to answer.
//
// When semstreams adds new predicates (e.g., tool_status, error_category),
// the corresponding "gap" tests below should be converted from skip→assert.

import (
	"fmt"
	"testing"
	"time"

	"github.com/c360studio/semstreams/agentic"
	"github.com/c360studio/semstreams/message"
	"github.com/c360studio/semstreams/model"
	agvocab "github.com/c360studio/semstreams/vocabulary/agentic"
)

const (
	testOrg      = "acme"
	testPlatform = "ops"
)

func testLoopEntityID(loopID string) string {
	return agentic.LoopExecutionEntityID(testOrg, testPlatform, loopID)
}

func testModelEntityID(name string) string {
	return agentic.ModelEndpointEntityID(testOrg, testPlatform, name)
}

// allTriplesFor filters triples by subject.
func allTriplesFor(triples []message.Triple, subject string) []message.Triple {
	var out []message.Triple
	for _, t := range triples {
		if t.Subject == subject {
			out = append(out, t)
		}
	}
	return out
}

// triplesWithPredicate filters triples by predicate.
func triplesWithPredicate(triples []message.Triple, predicate string) []message.Triple {
	var out []message.Triple
	for _, t := range triples {
		if t.Predicate == predicate {
			out = append(out, t)
		}
	}
	return out
}

// --- Ops Query: "What is the success rate for the researcher role?" ---

func TestOpsQuery_LoopOutcomeByRole(t *testing.T) {
	// Simulate 3 completed loops and 1 failed loop for the researcher role.
	// The ops agent needs: LoopOutcome + LoopRole on every loop entity.
	type loopRun struct {
		loopID     string
		outcome    string
		role       string
		iterations int
	}

	runs := []loopRun{
		{"r1", "success", "researcher", 5},
		{"r2", "success", "researcher", 3},
		{"r3", "failed", "researcher", 10},
		{"r4", "success", "architect", 2},
	}

	var allTriples []message.Triple
	for _, r := range runs {
		entityID := testLoopEntityID(r.loopID)
		// gh#159: role is now spawn-stamped. Production reads filter
		// the loop entity from KV which carries BOTH spawn-time and
		// completion-time triples; simulate the same union here.
		spawnTask := &agentic.TaskMessage{TaskID: "task-" + r.loopID, Role: r.role}
		allTriples = append(allTriples, buildSpawnIdentityTriples(entityID, spawnTask, testOrg, testPlatform)...)
		if r.outcome == "success" {
			event := &agentic.LoopCompletedEvent{
				LoopID:      r.loopID,
				TaskID:      "task-" + r.loopID,
				Outcome:     r.outcome,
				Role:        r.role,
				Iterations:  r.iterations,
				CompletedAt: time.Now(),
			}
			allTriples = append(allTriples, buildLoopCompletionTriples(entityID, event, "", 0)...)
		} else {
			event := &agentic.LoopFailedEvent{
				LoopID:     r.loopID,
				TaskID:     "task-" + r.loopID,
				Outcome:    r.outcome,
				Role:       r.role,
				Iterations: r.iterations,
				FailedAt:   time.Now(),
			}
			allTriples = append(allTriples, buildLoopFailureTriples(entityID, event, "", 0)...)
		}
	}

	// Simulate ops agent graph query: filter by role=researcher, count outcomes
	var researcherSuccess, researcherFailed int
	for _, r := range runs {
		entityID := testLoopEntityID(r.loopID)
		loopTriples := allTriplesFor(allTriples, entityID)

		role := objectFor(loopTriples, agvocab.LoopRole)
		if role != "researcher" {
			continue
		}

		outcome := objectFor(loopTriples, agvocab.LoopOutcome)
		switch outcome {
		case "success":
			researcherSuccess++
		case "failed":
			researcherFailed++
		}
	}

	if researcherSuccess != 2 {
		t.Errorf("researcher success count: got %d, want 2", researcherSuccess)
	}
	if researcherFailed != 1 {
		t.Errorf("researcher failed count: got %d, want 1", researcherFailed)
	}
}

// --- Ops Query: "Which tools were used and how often?" ---

func TestOpsQuery_ToolUsageFrequency(t *testing.T) {
	loopEntityID := testLoopEntityID("research-001")
	traj := &agentic.Trajectory{
		LoopID: "research-001",
		Steps: []agentic.TrajectoryStep{
			{Timestamp: time.Now(), StepType: "model_call", Model: "claude", TokensIn: 500, TokensOut: 200, Duration: 2000},
			{Timestamp: time.Now(), StepType: "tool_call", ToolName: "web_search", Duration: 1500},
			{Timestamp: time.Now(), StepType: "model_call", Model: "claude", TokensIn: 800, TokensOut: 300, Duration: 2500},
			{Timestamp: time.Now(), StepType: "tool_call", ToolName: "web_search", Duration: 1200},
			{Timestamp: time.Now(), StepType: "tool_call", ToolName: "arxiv_search", Duration: 800},
			{Timestamp: time.Now(), StepType: "model_call", Model: "claude", TokensIn: 1000, TokensOut: 500, Duration: 3000},
		},
	}

	triples := buildTrajectoryStepTriples(loopEntityID, testOrg, testPlatform, "research-001", traj)

	// Count tool usage by name
	toolCounts := make(map[string]int)
	toolNameTriples := triplesWithPredicate(triples, agvocab.StepToolName)
	for _, t := range toolNameTriples {
		name, ok := t.Object.(string)
		if !ok {
			continue
		}
		toolCounts[name]++
	}

	if toolCounts["web_search"] != 2 {
		t.Errorf("web_search count: got %d, want 2", toolCounts["web_search"])
	}
	if toolCounts["arxiv_search"] != 1 {
		t.Errorf("arxiv_search count: got %d, want 1", toolCounts["arxiv_search"])
	}
}

// --- Ops Query: "What is the token cost breakdown per model_call step?" ---

func TestOpsQuery_TokenCostPerStep(t *testing.T) {
	loopEntityID := testLoopEntityID("cost-analysis-001")
	traj := &agentic.Trajectory{
		LoopID: "cost-analysis-001",
		Steps: []agentic.TrajectoryStep{
			{Timestamp: time.Now(), StepType: "model_call", Model: "claude-opus", TokensIn: 5000, TokensOut: 1000, Duration: 5000},
			{Timestamp: time.Now(), StepType: "tool_call", ToolName: "web_search", Duration: 800},
			{Timestamp: time.Now(), StepType: "model_call", Model: "claude-opus", TokensIn: 8000, TokensOut: 2000, Duration: 7000},
		},
	}

	triples := buildTrajectoryStepTriples(loopEntityID, testOrg, testPlatform, "cost-analysis-001", traj)

	// Collect token data from model_call steps
	type modelCallData struct {
		tokensIn  int
		tokensOut int
		model     string
	}
	var modelCalls []modelCallData

	// Group triples by step entity
	stepEntities := make(map[string][]message.Triple)
	for _, tr := range triples {
		if tr.Subject != loopEntityID { // skip LoopHasStep triples
			stepEntities[tr.Subject] = append(stepEntities[tr.Subject], tr)
		}
	}

	for _, stepTriples := range stepEntities {
		stepType := objectFor(stepTriples, agvocab.StepType)
		if stepType != "model_call" {
			continue
		}
		tokIn, _ := objectFor(stepTriples, agvocab.StepTokensIn).(int)
		tokOut, _ := objectFor(stepTriples, agvocab.StepTokensOut).(int)
		mod, _ := objectFor(stepTriples, agvocab.StepModel).(string)
		modelCalls = append(modelCalls, modelCallData{tokensIn: tokIn, tokensOut: tokOut, model: mod})
	}

	if len(modelCalls) != 2 {
		t.Fatalf("expected 2 model_call steps, got %d", len(modelCalls))
	}

	totalTokensIn := 0
	for _, mc := range modelCalls {
		totalTokensIn += mc.tokensIn
	}
	if totalTokensIn != 13000 {
		t.Errorf("total tokens_in across model_calls: got %d, want 13000", totalTokensIn)
	}
}

// --- Ops Query: "How many iterations do research loops typically use?" ---

func TestOpsQuery_IterationDistribution(t *testing.T) {
	type loopResult struct {
		loopID     string
		role       string
		iterations int
	}

	results := []loopResult{
		{"iter-1", "researcher", 3},
		{"iter-2", "researcher", 7},
		{"iter-3", "researcher", 5},
		{"iter-4", "researcher", 25}, // hit max_iterations
		{"iter-5", "architect", 2},
	}

	var allTriples []message.Triple
	for _, r := range results {
		entityID := testLoopEntityID(r.loopID)
		// gh#159: role is now spawn-stamped; mirror production flow.
		spawnTask := &agentic.TaskMessage{TaskID: "task-" + r.loopID, Role: r.role}
		allTriples = append(allTriples, buildSpawnIdentityTriples(entityID, spawnTask, testOrg, testPlatform)...)
		event := &agentic.LoopCompletedEvent{
			LoopID:      r.loopID,
			TaskID:      "task-" + r.loopID,
			Outcome:     "success",
			Role:        r.role,
			Iterations:  r.iterations,
			CompletedAt: time.Now(),
		}
		allTriples = append(allTriples, buildLoopCompletionTriples(entityID, event, "", 0)...)
	}

	// Ops agent query: iteration counts for researcher role
	var researcherIterations []int
	for _, r := range results {
		entityID := testLoopEntityID(r.loopID)
		loopTriples := allTriplesFor(allTriples, entityID)

		role := objectFor(loopTriples, agvocab.LoopRole)
		if role != "researcher" {
			continue
		}
		iters, ok := objectFor(loopTriples, agvocab.LoopIterations).(int)
		if !ok {
			t.Errorf("LoopIterations not an int for %s", r.loopID)
			continue
		}
		researcherIterations = append(researcherIterations, iters)
	}

	if len(researcherIterations) != 4 {
		t.Fatalf("expected 4 researcher loops, got %d", len(researcherIterations))
	}

	// Verify the ops agent can compute: min=3, max=25, avg=10
	min, max, sum := researcherIterations[0], researcherIterations[0], 0
	for _, v := range researcherIterations {
		if v < min {
			min = v
		}
		if v > max {
			max = v
		}
		sum += v
	}
	avg := sum / len(researcherIterations)

	if min != 3 {
		t.Errorf("min iterations: got %d, want 3", min)
	}
	if max != 25 {
		t.Errorf("max iterations: got %d, want 25", max)
	}
	if avg != 10 {
		t.Errorf("avg iterations: got %d, want 10", avg)
	}
}

// --- Ops Query: "Which model endpoints are most cost-effective?" ---

func TestOpsQuery_CostByModel(t *testing.T) {
	reg := &model.Registry{
		Endpoints: map[string]*model.EndpointConfig{
			"claude-opus": {
				Model:                  "claude-opus-4-5",
				InputPricePer1MTokens:  15.0,
				OutputPricePer1MTokens: 75.0,
			},
			"claude-haiku": {
				Model:                  "claude-haiku-4-5",
				InputPricePer1MTokens:  0.25,
				OutputPricePer1MTokens: 1.25,
			},
		},
	}

	type loopData struct {
		loopID    string
		model     string
		tokensIn  int
		tokensOut int
	}

	loops := []loopData{
		{"cost-1", "claude-opus", 10000, 5000},
		{"cost-2", "claude-opus", 8000, 3000},
		{"cost-3", "claude-haiku", 10000, 5000},
		{"cost-4", "claude-haiku", 8000, 3000},
	}

	modelCosts := make(map[string]float64)
	for _, l := range loops {
		entityID := testLoopEntityID(l.loopID)
		modelEntityID := testModelEntityID(l.model)
		cost := computeCost(reg, l.model, l.tokensIn, l.tokensOut)

		event := &agentic.LoopCompletedEvent{
			LoopID:      l.loopID,
			TaskID:      "task-" + l.loopID,
			Outcome:     "success",
			Role:        "researcher",
			Model:       l.model,
			Iterations:  5,
			TokensIn:    l.tokensIn,
			TokensOut:   l.tokensOut,
			CompletedAt: time.Now(),
		}

		triples := buildLoopCompletionTriples(entityID, event, modelEntityID, cost)

		modelUsed, _ := objectFor(triples, agvocab.LoopModelUsed).(string)
		loopCost, _ := objectFor(triples, agvocab.LoopCostUSD).(float64)
		modelCosts[modelUsed] += loopCost
	}

	opusID := testModelEntityID("claude-opus")
	haikuID := testModelEntityID("claude-haiku")

	if modelCosts[opusID] <= 0 {
		t.Error("expected non-zero cost for claude-opus")
	}
	if modelCosts[haikuID] <= 0 {
		t.Error("expected non-zero cost for claude-haiku")
	}
	if modelCosts[haikuID] >= modelCosts[opusID] {
		t.Errorf("expected haiku cheaper than opus: haiku=%.6f, opus=%.6f",
			modelCosts[haikuID], modelCosts[opusID])
	}
}

// --- Ops Query: "Can I link trajectory steps back to the parent loop?" ---

func TestOpsQuery_StepToLoopLinkage(t *testing.T) {
	loopID := "linkage-001"
	loopEntityID := testLoopEntityID(loopID)

	traj := &agentic.Trajectory{
		LoopID: loopID,
		Steps: []agentic.TrajectoryStep{
			{Timestamp: time.Now(), StepType: "model_call", Model: "claude", TokensIn: 100, TokensOut: 50, Duration: 1000},
			{Timestamp: time.Now(), StepType: "tool_call", ToolName: "web_search", Duration: 500},
		},
	}

	triples := buildTrajectoryStepTriples(loopEntityID, testOrg, testPlatform, loopID, traj)

	// Every step must have a StepLoop predicate pointing back to the loop entity
	for i := range traj.Steps {
		stepEntityID := fmt.Sprintf("%s.%s.agent.agentic-loop.step.%s-%d", testOrg, testPlatform, loopID, i)
		stepTriples := allTriplesFor(triples, stepEntityID)

		loopRef := objectFor(stepTriples, agvocab.StepLoop)
		if loopRef != loopEntityID {
			t.Errorf("step %d: StepLoop = %v, want %s", i, loopRef, loopEntityID)
		}
	}

	// The loop entity must have LoopHasStep triples pointing to each step
	loopTriples := allTriplesFor(triples, loopEntityID)
	hasStepTriples := triplesWithPredicate(loopTriples, agvocab.LoopHasStep)

	if len(hasStepTriples) != 2 {
		t.Errorf("expected 2 LoopHasStep triples, got %d", len(hasStepTriples))
	}
}

// --- Ops Query: "Which tool calls failed?" ---
// UNBLOCKED: semstreams#3 resolved in v1.0.0-beta.5.

func TestOpsQuery_ToolFailureRate(t *testing.T) {
	loopEntityID := testLoopEntityID("tool-failures-001")
	traj := &agentic.Trajectory{
		LoopID: "tool-failures-001",
		Steps: []agentic.TrajectoryStep{
			{Timestamp: time.Now(), StepType: "tool_call", ToolName: "web_search", ToolStatus: "success", Duration: 1500},
			{Timestamp: time.Now(), StepType: "tool_call", ToolName: "web_search", ToolStatus: "failed", ErrorMessage: "connection timeout", ErrorCategory: "timeout", Duration: 5000},
			{Timestamp: time.Now(), StepType: "tool_call", ToolName: "arxiv_search", ToolStatus: "success", Duration: 800},
		},
	}

	triples := buildTrajectoryStepTriples(loopEntityID, testOrg, testPlatform, "tool-failures-001", traj)

	// Compute failure rate per tool from graph triples
	type toolStats struct {
		success int
		failed  int
	}
	stats := make(map[string]*toolStats)

	// Group triples by step entity
	stepEntities := make(map[string][]message.Triple)
	for _, tr := range triples {
		if tr.Subject != loopEntityID {
			stepEntities[tr.Subject] = append(stepEntities[tr.Subject], tr)
		}
	}

	for _, stepTriples := range stepEntities {
		stepType := objectFor(stepTriples, agvocab.StepType)
		if stepType != "tool_call" {
			continue
		}
		toolName, _ := objectFor(stepTriples, agvocab.StepToolName).(string)
		toolStatus, _ := objectFor(stepTriples, agvocab.StepToolStatus).(string)

		if stats[toolName] == nil {
			stats[toolName] = &toolStats{}
		}
		switch toolStatus {
		case "success":
			stats[toolName].success++
		case "failed":
			stats[toolName].failed++
		}
	}

	// web_search: 1 success, 1 failed → 50% failure rate
	ws := stats["web_search"]
	if ws == nil {
		t.Fatal("no stats for web_search")
	}
	if ws.success != 1 || ws.failed != 1 {
		t.Errorf("web_search: got %d success, %d failed; want 1, 1", ws.success, ws.failed)
	}

	// arxiv_search: 1 success, 0 failed → 0% failure rate
	as := stats["arxiv_search"]
	if as == nil {
		t.Fatal("no stats for arxiv_search")
	}
	if as.success != 1 || as.failed != 0 {
		t.Errorf("arxiv_search: got %d success, %d failed; want 1, 0", as.success, as.failed)
	}
}

// --- Ops Query: "What error categories are most common?" ---
// UNBLOCKED: semstreams#3 resolved in v1.0.0-beta.5.

func TestOpsQuery_ErrorCategories(t *testing.T) {
	loopEntityID := testLoopEntityID("errors-001")
	traj := &agentic.Trajectory{
		LoopID: "errors-001",
		Steps: []agentic.TrajectoryStep{
			{Timestamp: time.Now(), StepType: "tool_call", ToolName: "web_search", ToolStatus: "failed", ErrorMessage: "connection timeout after 30s", ErrorCategory: "timeout", Duration: 30000},
			{Timestamp: time.Now(), StepType: "tool_call", ToolName: "github_search", ToolStatus: "failed", ErrorMessage: "rate limit exceeded", ErrorCategory: "rate_limit", Duration: 100},
			{Timestamp: time.Now(), StepType: "tool_call", ToolName: "web_search", ToolStatus: "failed", ErrorMessage: "DNS resolution failed", ErrorCategory: "network", Duration: 5000},
			{Timestamp: time.Now(), StepType: "tool_call", ToolName: "web_search", ToolStatus: "failed", ErrorMessage: "read timeout", ErrorCategory: "timeout", Duration: 15000},
			{Timestamp: time.Now(), StepType: "tool_call", ToolName: "arxiv_search", ToolStatus: "success", Duration: 800},
		},
	}

	triples := buildTrajectoryStepTriples(loopEntityID, testOrg, testPlatform, "errors-001", traj)

	// Aggregate error categories from graph triples
	categoryCounts := make(map[string]int)
	stepEntities := make(map[string][]message.Triple)
	for _, tr := range triples {
		if tr.Subject != loopEntityID {
			stepEntities[tr.Subject] = append(stepEntities[tr.Subject], tr)
		}
	}

	for _, stepTriples := range stepEntities {
		cat, _ := objectFor(stepTriples, agvocab.StepErrorCategory).(string)
		if cat != "" {
			categoryCounts[cat]++
		}
	}

	if categoryCounts["timeout"] != 2 {
		t.Errorf("timeout count: got %d, want 2", categoryCounts["timeout"])
	}
	if categoryCounts["network"] != 1 {
		t.Errorf("network count: got %d, want 1", categoryCounts["network"])
	}
	if categoryCounts["rate_limit"] != 1 {
		t.Errorf("rate_limit count: got %d, want 1", categoryCounts["rate_limit"])
	}

	// Verify error messages are queryable too
	var timeoutMessages []string
	for _, stepTriples := range stepEntities {
		cat, _ := objectFor(stepTriples, agvocab.StepErrorCategory).(string)
		if cat == "timeout" {
			msg, _ := objectFor(stepTriples, agvocab.StepErrorMessage).(string)
			timeoutMessages = append(timeoutMessages, msg)
		}
	}
	if len(timeoutMessages) != 2 {
		t.Errorf("expected 2 timeout error messages, got %d", len(timeoutMessages))
	}
}

// --- Ops Query: "Are step predicates discoverable via vocabulary registry?" ---
// UNBLOCKED: semstreams#2 resolved in v1.0.0-beta.5.

func TestOpsQuery_StepPredicatesRegistered(t *testing.T) {
	// Verify the new predicates are accessible as constants (compile-time check).
	// If these don't compile, the predicates weren't added to the vocabulary package.
	predicates := []string{
		agvocab.StepType,
		agvocab.StepIndex,
		agvocab.StepLoop,
		agvocab.StepTimestamp,
		agvocab.StepDuration,
		agvocab.StepToolName,
		agvocab.StepModel,
		agvocab.StepTokensIn,
		agvocab.StepTokensOut,
		agvocab.StepCapability,
		agvocab.StepProvider,
		agvocab.StepRetries,
		agvocab.StepTokensEvicted,
		agvocab.StepTokensSummarized,
		agvocab.StepUtilization,
		agvocab.StepToolStatus,
		agvocab.StepErrorMessage,
		agvocab.StepErrorCategory,
		agvocab.LoopHasStep,
	}

	for _, p := range predicates {
		if p == "" {
			t.Errorf("predicate constant is empty string — likely undefined")
		}
	}

	// Verify string values follow the agent.step.* / agent.loop.* convention
	for _, p := range predicates[:18] { // step predicates
		if len(p) < len("agent.step.") {
			t.Errorf("step predicate %q too short", p)
		}
	}
	if agvocab.LoopHasStep != "agent.loop.has_step" {
		t.Errorf("LoopHasStep = %q, want agent.loop.has_step", agvocab.LoopHasStep)
	}
}

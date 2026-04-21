// Package main provides the standalone mock servers for E2E testing.
// It runs OpenAI and AGNTCY mock servers. Multiple e2e scenarios share
// this binary; the --scenario flag selects which response/tool-call
// preset to apply (see applyScenarioPreset).
package main

import (
	"flag"
	"fmt"
	"log"
	"os"
	"os/signal"
	"syscall"

	"github.com/c360studio/semstreams/test/e2e/mock"
	crudtools "github.com/c360studio/semstreams/test/e2e/scenarios/crud-tools"
	opsscenario "github.com/c360studio/semstreams/test/e2e/scenarios/ops"
)

func main() {
	port := flag.Int("port", 8080, "Port for OpenAI mock server")
	agntcyPort := flag.Int("agntcy-port", 8081, "Port for AGNTCY mock server")
	scenario := flag.String("scenario", "default", "Which preset of role responses / tool-call scripts to apply (default, deep-research, crud-tools, ops)")
	flag.Parse()

	openaiAddr := fmt.Sprintf(":%d", *port)
	openaiServer := mock.NewOpenAIServer().
		WithToolArgs("query_entity", `{"entity_id": "c360.logistics.environmental.sensor.temperature.temp-sensor-001"}`).
		WithCompletionContent(`{"valid": true, "summary": "Analysis complete. Temperature sensor reading exceeds threshold. Recommend monitoring HVAC system."}`)

	applyScenarioPreset(openaiServer, *scenario)

	if err := openaiServer.Start(openaiAddr); err != nil {
		log.Fatalf("Failed to start OpenAI mock server: %v", err)
	}
	log.Printf("Mock OpenAI server listening on %s (scenario=%s)", openaiServer.URL(), *scenario)
	log.Printf("  Health endpoint: %s/health", openaiServer.URL())
	log.Printf("  Chat completions: %s/v1/chat/completions", openaiServer.URL())

	// Start AGNTCY mock server
	agntcyAddr := fmt.Sprintf(":%d", *agntcyPort)
	agntcyServer := mock.NewAGNTCYServer()

	if err := agntcyServer.Start(agntcyAddr); err != nil {
		log.Fatalf("Failed to start AGNTCY mock server: %v", err)
	}
	log.Printf("Mock AGNTCY server listening on %s", agntcyServer.URL())
	log.Printf("  Directory: %s/v1/agents/register", agntcyServer.URL())
	log.Printf("  OTEL HTTP: %s/v1/traces", agntcyServer.URL())

	// Wait for shutdown signal
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, os.Interrupt, syscall.SIGTERM)
	<-sigChan

	log.Println("Shutting down...")
	if err := openaiServer.Stop(); err != nil {
		log.Printf("Error stopping OpenAI mock: %v", err)
	}
	if err := agntcyServer.Stop(); err != nil {
		log.Printf("Error stopping AGNTCY mock: %v", err)
	}
}

// applyScenarioPreset layers scenario-specific role responses and
// tool-call scripts onto a pre-configured server. The default server
// already handles agentic-tier's validation JSON; the presets add
// deep-research's researcher/coordinator/synthesizer scripting and
// crud-tools' agent-invokes-create_rule scripting.
//
// Keeping each scenario's wiring in its own case keeps this file the
// single source of truth for "what the mock returns when." Adding a new
// e2e scenario is a new case plus a new taskfile/compose pair.
func applyScenarioPreset(server *mock.OpenAIServer, scenario string) {
	switch scenario {
	case "deep-research":
		applyDeepResearchPreset(server)
	case "crud-tools":
		applyCRUDToolsPreset(server)
	case "ops":
		applyOpsPreset(server)
	default:
		// "default" leaves the server's base configuration in place —
		// agentic tier uses this path, plus any future scenario that just
		// needs the completion-JSON default.
	}
}

func applyDeepResearchPreset(server *mock.OpenAIServer) {
	// Per-role completion text + scripted coordinator decisions. Markers
	// match substrings of the actual rule prompts. As of ADR-029 step 3b
	// the assembler also runs per task, so persona fragments from
	// DefaultFragments (and any KV-backed overrides) arrive on the
	// request as a leading system message. Rule prompts still drive the
	// role-specific responses below — the system message is ambient
	// context that all role responses share.
	server.
		WithRoleResponses([]mock.RoleResponse{
			{
				Marker:  "Research a question that was submitted",
				Content: "Findings: deep-learning embeddings tend to outperform BM25 on semantic similarity but lose the word-level exactness that BM25 preserves for fact lookup. Recent work (2025) shows hybrid rerank consistently beats either alone.",
			},
			{
				Marker:  "research synthesizer",
				Content: "# Synthesis\n\n**Summary**: Hybrid lexical+semantic retrieval wins.\n\n**Key Findings**:\n1. BM25 preserves exact-match precision.\n2. Dense embeddings recover semantic similarity.\n3. Hybrid reranking outperforms either alone in 2025 benchmarks.\n\n**Confidence**: High on the hybrid-wins claim; moderate on the specific benchmark numbers.\n\n**Gaps**: Evaluation on low-resource languages remains thin.",
			},
		}).
		// Deep-research coordinator: first invocation fans out into two
		// subtopics; second invocation synthesizes. Marker matches the
		// phrase "call the decide tool exactly once" — unique to rule
		// 07's coordinator prompt.
		WithRoleToolCallSequence([]mock.RoleToolCall{
			{
				Marker:   "call the decide tool exactly once",
				ToolName: "decide",
				Args: map[string]any{
					"action":    "fan_out",
					"reason":    "two separable subtopics (lexical exactness, semantic similarity) worth dedicated investigation",
					"subtopics": []string{"bm25 exactness", "embedding similarity"},
				},
			},
			{
				Marker:   "call the decide tool exactly once",
				ToolName: "decide",
				Args: map[string]any{
					"action": "synthesize",
					"reason": "enough evidence collected across subtopics",
				},
			},
		})
}

// applyCRUDToolsPreset scripts the crud-tools scenario's agent loop.
// The user prompt asks the agent to author a rule; the mock returns a
// create_rule tool call with a realistic payload. The scenario then
// verifies the rule lands in the semstreams_config KV bucket.
//
// Only one tool call is scripted: the agent's single Create action.
// After it completes, the mock returns completion text (via the
// default completionContent) and the loop terminates naturally.
//
// Marker is crudtools.PersonaMarker — a unique substring that only
// appears in the e2e persona override that the scenario seeds in the
// PERSONAS KV bucket. This forces the mock to rely on the ADR-029
// step-3b assembler plumbing reaching the LLM with the override
// content. If the step-3b wiring regresses (persona doesn't get
// merged into the registry, or the system message never reaches the
// chat completion request), the marker misses, the create_rule call
// is not injected, and the scenario fails loud at
// wait-for-tool-execution rather than silently passing.
func applyCRUDToolsPreset(server *mock.OpenAIServer) {
	server.WithRoleToolCallSequence([]mock.RoleToolCall{
		{
			Marker:   crudtools.PersonaMarker,
			ToolName: "create_rule",
			Args: map[string]any{
				"rule_id": "e2e-crud-rule",
				"rule": map[string]any{
					"id":          "e2e-crud-rule",
					"type":        "expression",
					"name":        "E2E CRUD Rule",
					"description": "Rule created by the crud-tools e2e scenario to verify the create_rule tool end-to-end.",
					"enabled":     true,
					"conditions": []map[string]any{
						{"field": "agent.loop.role", "operator": "eq", "value": "never-matches"},
					},
					"logic": "and",
				},
			},
		},
	})
}

// applyOpsPreset scripts the ops-agent scenario's loop.
// The ops persona fragment contains OpsPersonaMarker ("operations analyst agent"),
// which the marker matches on after the PR3 file loader and ADR-029 step-3b
// assembler wire the persona content into the LLM request.
//
// The sequence scripts three emit_diagnosis calls — each referencing one of the
// seeded synthetic loop entity IDs as evidence — followed by submit_work.
// Four entries in the sequence; once exhausted the last entry sticks (submit_work).
// The loop terminates naturally after submit_work because the executor returns
// StopLoop=true.
//
// Findings are distinguishable by severity and observed_role so the e2e
// assertions can verify the graph contains all three without inspecting prose.
func applyOpsPreset(server *mock.OpenAIServer) {
	server.WithRoleToolCallSequence([]mock.RoleToolCall{
		{
			// Marker matches the ops persona fragment body loaded by PR3's file loader.
			// The substring "operations analyst agent" is the first sentence of
			// configs/personas/fragments/ops/00-identity.md.
			Marker:   opsscenario.OpsPersonaMarker,
			ToolName: "emit_diagnosis",
			Args: map[string]any{
				"finding":        "Elevated failure rate in test-agent role loops",
				"recommendation": "Inspect network errors in seed-loop-003; consider rate-limit backoff",
				"confidence":     0.85,
				"evidence":       []string{opsscenario.SeedLoop3ID},
				"observed_role":  "probe",
				"severity":       "warn",
			},
		},
		{
			Marker:   opsscenario.OpsPersonaMarker,
			ToolName: "emit_diagnosis",
			Args: map[string]any{
				"finding":        "Moderate token burn on test-agent loops despite short iterations",
				"recommendation": "Review prompt length for seed-loop-001 and seed-loop-002",
				"confidence":     0.72,
				"evidence":       []string{opsscenario.SeedLoop1ID},
				"observed_role":  "test-agent",
				"severity":       "info",
			},
		},
		{
			Marker:   opsscenario.OpsPersonaMarker,
			ToolName: "emit_diagnosis",
			Args: map[string]any{
				"finding":        "Probe role loop terminated with network error category",
				"recommendation": "Investigate upstream connectivity for probe-role tasks",
				"confidence":     0.91,
				"evidence":       []string{opsscenario.SeedLoop2ID, opsscenario.SeedLoop3ID},
				"observed_role":  "probe",
				"severity":       "error",
			},
		},
		{
			Marker:   opsscenario.OpsPersonaMarker,
			ToolName: "submit_work",
			Args: map[string]any{
				"summary": "Emitted 3 findings: 1 error-severity probe connectivity issue, 1 warn-severity failure rate, 1 info-severity token burn. All findings reference seeded loop evidence entities. Review recommended for probe-role connectivity before next run.",
			},
		},
	})
}

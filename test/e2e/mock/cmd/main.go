// Package main provides the standalone mock servers for E2E testing.
// It runs OpenAI and AGNTCY mock servers.
package main

import (
	"flag"
	"fmt"
	"log"
	"os"
	"os/signal"
	"syscall"

	"github.com/c360studio/semstreams/test/e2e/mock"
)

func main() {
	port := flag.Int("port", 8080, "Port for OpenAI mock server")
	agntcyPort := flag.Int("agntcy-port", 8081, "Port for AGNTCY mock server")
	flag.Parse()

	// Start OpenAI mock server with behaviour for both the agentic tier
	// (default completion content) and the deep-research tier (role-based
	// content + coordinator tool-call sequence). The role-matchers are
	// prompt-content substrings unique to each scenario so they don't
	// collide.
	openaiAddr := fmt.Sprintf(":%d", *port)
	openaiServer := mock.NewOpenAIServer().
		WithToolArgs("query_entity", `{"entity_id": "c360.logistics.environmental.sensor.temperature.temp-sensor-001"}`).
		// Agentic tier default — returns JSON for workflow validation.
		WithCompletionContent(`{"valid": true, "summary": "Analysis complete. Temperature sensor reading exceeds threshold. Recommend monitoring HVAC system."}`).
		// Deep-research tier: per-role completion text. The researcher
		// and synthesizer produce free-form findings; the coordinator
		// decision is scripted via the tool-call sequence below.
		WithRoleResponses([]mock.RoleResponse{
			// Markers match substrings of the actual rule prompts (not
			// persona fragments from agentic-loop/prompt/assembler.go —
			// those aren't wired into the task dispatcher, so only the
			// rule-supplied prompt reaches the LLM).
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

	if err := openaiServer.Start(openaiAddr); err != nil {
		log.Fatalf("Failed to start OpenAI mock server: %v", err)
	}
	log.Printf("Mock OpenAI server listening on %s", openaiServer.URL())
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

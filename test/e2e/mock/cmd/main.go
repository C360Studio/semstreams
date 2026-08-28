// Package main provides the standalone mock servers for E2E testing.
// It runs an OpenAI-compatible mock server. Multiple e2e scenarios share
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

	researchassess "github.com/c360studio/semstreams/processor/research-graph-assess"
	researchroute "github.com/c360studio/semstreams/processor/research-graph-route"
	researchsynthesize "github.com/c360studio/semstreams/processor/research-graph-synthesize"
	"github.com/c360studio/semstreams/test/e2e/mock"
	crudtools "github.com/c360studio/semstreams/test/e2e/scenarios/crud-tools"
	opsscenario "github.com/c360studio/semstreams/test/e2e/scenarios/ops"
	researchgraph "github.com/c360studio/semstreams/test/e2e/scenarios/research-graph"
)

func main() {
	port := flag.Int("port", 8080, "Port for OpenAI mock server")
	scenario := flag.String("scenario", "default", "Which preset of role responses / tool-call scripts to apply (default, deep-research, crud-tools, ops, research-graph, research-graph-execute)")
	flag.Parse()

	openaiAddr := fmt.Sprintf(":%d", *port)
	openaiServer := mock.NewOpenAIServer().
		WithToolArgs("query_entity", `{"entity_id": "c360.logistics.sensor.environmental.temperature.temp-sensor-001"}`).
		WithCompletionContent(`{"valid": true, "summary": "Analysis complete. Temperature sensor reading exceeds threshold. Recommend monitoring HVAC system."}`)

	applyScenarioPreset(openaiServer, *scenario)

	if err := openaiServer.Start(openaiAddr); err != nil {
		log.Fatalf("Failed to start OpenAI mock server: %v", err)
	}
	log.Printf("Mock OpenAI server listening on %s (scenario=%s)", openaiServer.URL(), *scenario)
	log.Printf("  Health endpoint: %s/health", openaiServer.URL())
	log.Printf("  Chat completions: %s/v1/chat/completions", openaiServer.URL())

	// Wait for shutdown signal
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, os.Interrupt, syscall.SIGTERM)
	<-sigChan

	log.Println("Shutting down...")
	if err := openaiServer.Stop(); err != nil {
		log.Printf("Error stopping OpenAI mock: %v", err)
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
	case "research-graph":
		applyResearchGraphPreset(server)
	case "research-graph-execute":
		applyResearchGraphExecutePreset(server)
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

// opsNeverMatchMarker is a sentinel that never appears in any assembled ops
// prompt. It caps the inject loop: once the injection-proof entry fires, the
// shared cursor advances onto this entry, which cannot match, so the mock falls
// through to a plain completion and the loop terminates naturally.
const opsNeverMatchMarker = "__ops_e2e_never_match_terminator__"

// applyOpsPreset scripts the ops-agent scenario's TWO loops for the full gated
// lesson round-trip (ADR-080, agent-memory-lesson-substrate §5).
//
// The ops persona fragment contains OpsPersonaMarker ("operations analyst agent"),
// which the first four entries match on after the PR3 file loader and ADR-029
// step-3b assembler wire the persona content into the LLM request.
//
// Termination model: submit_work is NOT advertised to the model in this flow, so
// an agent loop cannot terminate by calling it — instead each loop ends the way
// the pre-existing ops scenario already relied on: once no scripted tool-call
// entry matches the request, the mock returns a plain completion and the loop
// finishes on a terminal-tool-less completion. The scripted entries therefore
// only ever use tools the loop advertises (emit_diagnosis, emit_lesson).
//
// The shared cursor advances ONLY when the current entry's marker matches the
// request AND its tool is advertised, giving a clean two-loop hand-off:
//
// Loop 1 (emit) — the first user.message spawns an ops loop. Entries 0-3 all match
// OpsPersonaMarker (present in every ops request), advancing the cursor through:
//   - three emit_diagnosis calls (each citing a seeded loop as evidence),
//   - one emit_lesson call (the first in-repo emit_lesson consumer): a durable
//     lesson scoped tag:<ops role> and citing a REAL seeded entity so the later
//     promotion's evidence-existence gate passes.
//
// The lesson is born status="proposed", so brief assembly does NOT inject it into
// loop 1 (proposed lessons are excluded) — the injection-form string never appears
// in a loop-1 request. After entry 3 fires, the cursor rests on entry 4, whose
// marker is the injection form; loop 1 cannot match it, so loop 1 ends on a
// completion with the cursor parked at entry 4.
//
// Loop 2 (inject) — after the scenario promotes the lesson to active, its second
// user.message spawns a fresh ops loop. Brief assembly now renders the active,
// tag:ops-scoped lesson's injection form into that loop's system prompt, so entry
// 4 (Marker == the injection form) matches and fires emit_diagnosis leaving
// InjectionProofFinding — the deterministic end-to-end proof. The cursor then
// advances onto entry 5 (the never-match sentinel), which cannot match, so loop 2
// ends on a completion instead of spinning. If injection regressed, entry 4's
// marker is absent, it never fires, no InjectionProofFinding appears, and the
// scenario's stage-4 poll hard-fails rather than passing silently.
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
			// Loop 1 emit stage: distil one durable lesson. Evidence cites the REAL
			// seeded entity SeedLoop1ID so promotion's evidence-existence gate resolves;
			// applies_to = tag:<ops role> so a later ops loop matches it; injection_form
			// is under the 320-byte bound and doubles as the loop-2 marker below.
			Marker:   opsscenario.OpsPersonaMarker,
			ToolName: "emit_lesson",
			Args: map[string]any{
				"summary":             opsscenario.LessonSummary,
				"detail":              "Across the observed test-agent loops, network calls retried without a ceiling, exhausting the iteration budget before the task made progress. Bound retries and cap the backoff so a transient upstream outage cannot consume the whole loop.",
				"injection_form":      opsscenario.LessonInjectionForm,
				"category":            opsscenario.LessonCategory,
				"polarity":            "avoid",
				"severity":            "warning",
				"evidence_entity_ids": []string{opsscenario.SeedLoop1ID},
				"applies_to":          []string{opsscenario.LessonAppliesToTag},
			},
		},
		{
			// Loop 2 inject stage: fires ONLY when the promoted lesson's injection form
			// reached this loop's brief (Marker == the injection-form string, which brief
			// assembly renders verbatim into the system prompt). emit_diagnosis IS
			// advertised, so this fires and leaves the deterministic proof triple
			// InjectionProofFinding that the scenario asserts on. Loop 1's brief carries
			// no active lesson, so loop 1 can never match this entry — the cursor parks
			// here between the two loops.
			Marker:   opsscenario.LessonInjectionForm,
			ToolName: "emit_diagnosis",
			Args: map[string]any{
				"finding":        opsscenario.InjectionProofFinding,
				"recommendation": "No action — this finding exists solely to prove the promoted lesson's injection form reached a subsequent loop's brief.",
				"confidence":     0.99,
				"evidence":       []string{opsscenario.SeedLoop1ID},
				"observed_role":  "ops",
				"severity":       "info",
			},
		},
		{
			// Never-match terminator: after the inject-proof fires, the cursor lands
			// here; this marker cannot appear in any assembled prompt, so the mock falls
			// through to a plain completion and loop 2 ends on a terminal-tool-less
			// completion instead of re-firing the inject-proof every iteration to the
			// cap. (submit_work is deliberately NOT advertised in this flow, so it cannot
			// serve as the terminator — natural completion is how ops loops end here.)
			Marker:   opsNeverMatchMarker,
			ToolName: "emit_diagnosis",
			Args:     map[string]any{},
		},
	})
}

// applyResearchGraphPreset scripts the ADR-045 Phase 1 chain end-to-end
// via marker-matched responses for the three LLM-wrapping components
// plus a parent-agent research_graph tool call.
//
// Happy-path coverage: parent fires `research_graph(topic=...)`, the
// chain runs nl_classify (deterministic Go code; no LLM in keyword-tier
// classifier mode), route_search emits `synthesize_directly`, the
// chain skips execute + assess, synthesize_answer emits a SearchResult
// envelope, R6 fires the continuation back to the parent, and the
// parent's continuation iteration completes via the default text path.
//
// The synthesize_directly route is the cheapest path that still
// exercises R0 → R1 → R2 → R6 + every triple-stamp seam — the
// orchestration claim PR #203 made. Refine-loop coverage
// (walk_seeds / decompose + assess + iteration cap) is a follow-up;
// shipping the happy path first locks the wire.
//
// Markers are the SystemPromptMarker constants exported from each
// LLM-wrapping component's prompt.go. Importing rather than inlining
// the substring means a persona-prose edit that severs the marker
// breaks the compile, not the e2e silently (per go-reviewer I3 on
// PR #205). JSON shapes match research.RouteDecision /
// research.SearchResult / research.AssessmentOutput exactly so the
// components' JSON extractor + Validate succeed without retries —
// deterministic e2e timing.
func applyResearchGraphPreset(server *mock.OpenAIServer) {
	server.
		WithRoleResponses([]mock.RoleResponse{
			{
				Marker: researchroute.SystemPromptMarker,
				Content: `{
  "action": "synthesize_directly",
  "args": {},
  "rationale": "Classifier candidate set covers the topic; no graph expansion needed."
}`,
			},
			{
				// Assess is unreachable on the synthesize_directly happy
				// path but the marker is wired here so a route-misroute
				// regression (e.g. route returns decompose by accident)
				// surfaces as "chain hung at assess" rather than "mock
				// returned wrong JSON shape and synthesize never fired."
				Marker: researchassess.SystemPromptMarker,
				Content: `{
  "sufficient": true,
  "refined_queries": []
}`,
			},
			{
				Marker: researchsynthesize.SystemPromptMarker,
				Content: `{
  "synthesis": "Drone hover anomalies cluster around the GCS-001 platform during low-altitude maneuvers. Two of the three surfaced candidates show elevated yaw drift telemetry preceding the anomaly. Recommend correlating with wind speed logs.",
  "evidence": [
    {"entity_id": "acme.ops.robotics.gcs.drone.001", "tier": "0", "source": "classifier", "snippet": "Drone 001 telemetry indicates yaw drift during hover at <50m AGL."},
    {"entity_id": "acme.ops.robotics.gcs.drone.002", "tier": "0", "source": "classifier", "snippet": "Drone 002 sibling unit; identical firmware and GCS link."}
  ],
  "decomp_trace": {
    "router_action": "synthesize_directly",
    "subquery_count": 0,
    "evidence_count_pre_dedup": 2,
    "evidence_count_post_dedup": 2,
    "tier_breakdown": {"0": 2},
    "budget_tokens_used": 0
  },
  "tokens_used": 120,
  "iterations": 0
}`,
			},
		}).
		// Parent agent's task says "use research_graph to investigate
		// the topic". The mock matches on a unique substring from that
		// prompt and emits a research_graph tool call. The chain then
		// runs; when R6's continuation comes back to the parent, the
		// continuation prompt does NOT contain this marker (it carries
		// the SearchResult ref + topic from the rule pack), so the
		// mock falls through to the default completion content + the
		// parent loop terminates naturally.
		WithRoleToolCallSequence([]mock.RoleToolCall{
			{
				Marker:   "Investigate the research topic via research_graph",
				ToolName: "research_graph",
				Args: map[string]any{
					"topic": "drone hover anomalies",
					"hints": map[string]any{
						"domain": "robotics",
					},
				},
			},
		})
}

// applyResearchGraphExecutePreset scripts the isolated walk_seeds fixture.
// The candidate_index reference binds the router output to the classifier's
// controlled nonzero candidate without teaching the mock a graph entity ID.
func applyResearchGraphExecutePreset(server *mock.OpenAIServer) {
	server.
		WithRoleResponses([]mock.RoleResponse{
			{
				Marker: researchroute.SystemPromptMarker,
				Content: `{
  "action": "walk_seeds",
  "args": {"seeds": [{"ref": "0", "ref_type": "candidate_index"}]},
  "rationale": "Walk the controlled classifier candidate to collect graph evidence."
}`,
			},
			{
				Marker: researchassess.SystemPromptMarker,
				Content: `{
  "sufficient": true,
  "refined_queries": [],
  "rationale": "The controlled graph evidence is sufficient.",
  "confidence": 1.0
}`,
			},
			{
				Marker: researchsynthesize.SystemPromptMarker,
				Content: fmt.Sprintf(`{
  "synthesis": "The controlled graph entity records drone hover anomaly evidence.",
  "evidence_refs": [%q]
}`, researchgraph.ControlledSeedEntityID),
			},
		}).
		WithRoleToolCallSequence([]mock.RoleToolCall{
			{
				Marker:   "Investigate the research topic via research_graph",
				ToolName: "research_graph",
				Args: map[string]any{
					"topic": "drone hover anomalies",
					"hints": map[string]any{"domain": "robotics"},
				},
			},
		})
}

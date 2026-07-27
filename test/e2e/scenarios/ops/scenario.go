// Package ops provides an end-to-end test scenario that validates the ADR-027
// Phase 1 ops agent end to end.
//
// The scenario exercises the full observability path:
//   - Seed synthetic completed-loop state into AGENT_LOOPS KV and corresponding
//     entity triples into ENTITY_STATES KV (stand-ins for real loop history).
//   - Inject a user.message asking the ops agent to analyse recent loops.
//   - Mock LLM scripts three emit_diagnosis tool calls + submit_work.
//   - Assert the resulting ops.diagnosis.* triples are queryable via
//     GET /graph/triples (PR4's HTTP endpoint).
//
// All content is framework-neutral: role names are "test-agent" / "probe",
// no product-specific taxonomy.
//
// When a regression breaks the ops path, the scenario fails at the closest
// stage (loop seeding → message injection → loop completion → triple assertion)
// so first-time debuggers see exactly which layer dropped the ball.
package ops

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os/exec"
	"strconv"
	"strings"
	"time"

	"github.com/c360studio/semstreams/agentic"
	"github.com/c360studio/semstreams/graph"
	"github.com/c360studio/semstreams/message"
	"github.com/c360studio/semstreams/test/e2e/client"
	"github.com/c360studio/semstreams/test/e2e/harness/lessoncuration"
	"github.com/c360studio/semstreams/test/e2e/scenarios"
	agvocab "github.com/c360studio/semstreams/vocabulary/agentic"
)

// OpsPersonaMarker is the unique substring planted in the ops persona fragment
// file (configs/personas/fragments/ops/00-identity.md). The mock preset's
// tool-call marker matches this substring — when the PR3 file loader runs at
// startup and the ADR-029 step-3b assembler wiring is correct, the ops persona
// content reaches the LLM and the mock fires its emit_diagnosis sequence.
// Exported so the mock preset and the scenario stay aligned on a single string.
const OpsPersonaMarker = "operations analyst agent"

// loopsBucket is the AGENT_LOOPS KV bucket — matches the agentic-loop component config.
const loopsBucket = "AGENT_LOOPS"

// entityStatesBucket is the ENTITY_STATES KV bucket — the graph HTTP endpoint reads from this.
const entityStatesBucket = "ENTITY_STATES"

// seedLoop1ID / seedLoop2ID / seedLoop3ID are the synthetic loop entity IDs
// seeded by this scenario. They are exported so the mock preset can reference
// them in its canned emit_diagnosis evidence fields.
const (
	SeedLoop1ID = "c360.ops.test.loops.loop.seed-loop-001"
	SeedLoop2ID = "c360.ops.test.loops.loop.seed-loop-002"
	SeedLoop3ID = "c360.ops.test.loops.loop.seed-loop-003"
)

// Lesson round-trip constants (ADR-080 §5). These are the single source of truth
// the mock preset's emit_lesson args and the scenario's assertions share, exactly
// as the SeedLoop* IDs align the diagnosis evidence.
const (
	// OpsRoleTag is the ops-agent loop role. Both scenario loops dispatch with
	// default_role: ops (ops-agent-test.json), so brief assembly derives scope
	// tag:ops for the inject loop — which must equal LessonAppliesToTag.
	OpsRoleTag = "ops"

	// LessonAppliesToTag is the typed scope key the emitted lesson carries. It is
	// tag:<ops role> so a subsequent ops loop's tag:ops scope matches it at brief
	// assembly (lessonmatch tag equality). If this drifts from OpsRoleTag the
	// inject stage cannot fire — they are deliberately kept in lockstep.
	LessonAppliesToTag = "tag:" + OpsRoleTag

	// LessonCategory is the emitted lesson's open category classifier.
	LessonCategory = "retry-policy"

	// LessonSummary is the emitted lesson's one-line gist (part of the lesson's
	// content-derived identity, but the scenario discovers the minted entity ID
	// from the graph rather than recomputing the UUIDv5, so it need not be kept
	// byte-identical to any hashing input here).
	LessonSummary = "Bound retries on network timeouts to avoid burning the iteration budget"

	// LessonInjectionForm is the emitted lesson's bounded injection form (< 320
	// bytes, no control bytes). It is the load-bearing marker for the inject
	// stage: brief assembly renders it VERBATIM into a subsequent ops loop's
	// system prompt, and the mock preset's final two tool-call entries match on
	// this exact string, so they fire only when injection genuinely occurred.
	LessonInjectionForm = "Avoid unbounded retries on network timeouts; cap backoff at 3 attempts."

	// InjectionProofFinding is the emit_diagnosis finding text the inject loop
	// leaves ONLY on the injection-form-gated branch. Its presence in the graph
	// is the deterministic end-to-end proof that the promoted lesson reached a
	// subsequent loop's brief. Kept distinct from every loop-1 finding so the
	// scenario can isolate it.
	InjectionProofFinding = "INJECTION-PROOF: promoted lesson injection form reached a subsequent ops loop brief"
)

// Config holds configuration for the ops scenario.
type Config struct {
	NATSURL         string
	BaseURL         string
	MetricsURL      string
	CompleteTimeout time.Duration

	// AppContainer is the docker container name for the running semstreams binary.
	// verifyRegisteredTools greps its startup logs to confirm emit_diagnosis registered.
	AppContainer string
}

// DefaultConfig returns defaults for the ops mock-LLM path.
// Ports are in the 61xxx/62xxx range so this scenario can run alongside
// agentic (3xxxx), deep-research (5xxxx), and crud-tools (6xxxx).
func DefaultConfig() *Config {
	return &Config{
		NATSURL:         "nats://localhost:61222",
		BaseURL:         "http://localhost:61080",
		MetricsURL:      "http://localhost:61190",
		CompleteTimeout: 30 * time.Second,
		AppContainer:    "semstreams-ops-app",
	}
}

// Scenario validates the ADR-027 Phase 1 ops agent end to end.
type Scenario struct {
	name        string
	description string

	config *Config
	obs    *client.ObservabilityClient

	nats    *client.NATSValidationClient
	metrics *client.MetricsClient

	// seededLoopKeys tracks KV keys written by seedSyntheticLoops so
	// cleanup can remove them on a second run of the scenario.
	seededLoopKeys   []string
	seededEntityKeys []string

	// lessonEntityID is the {org}.{platform}.agent.lesson.record.{uuid5} entity
	// the emit loop mints. Discovered from the graph in verifyLessonProposed
	// (the UUIDv5 is content-derived, so the scenario reads it back rather than
	// recomputing it) and consumed by promoteLesson + injectAndVerifyLesson.
	lessonEntityID string
}

// NewScenario constructs an ops scenario.
func NewScenario(obs *client.ObservabilityClient, config *Config) *Scenario {
	if config == nil {
		config = DefaultConfig()
	}
	return &Scenario{
		name:        "ops",
		description: "Verifies ADR-027 ops agent (observe loops → emit_diagnosis → triples) and the ADR-080 §5 lesson round-trip (emit_lesson → proposed → promote → brief injection)",
		config:      config,
		obs:         obs,
	}
}

// Name returns the scenario name.
func (s *Scenario) Name() string { return s.name }

// Description returns the scenario description.
func (s *Scenario) Description() string { return s.description }

// Setup creates NATS + metrics clients.
func (s *Scenario) Setup(ctx context.Context) error {
	nc, err := client.NewNATSValidationClient(ctx, s.config.NATSURL)
	if err != nil {
		return fmt.Errorf("create NATS client: %w", err)
	}
	s.nats = nc
	s.metrics = client.NewMetricsClient(s.config.MetricsURL)
	return nil
}

// Teardown closes clients.
func (s *Scenario) Teardown(ctx context.Context) error {
	if s.nats != nil {
		return s.nats.Close(ctx)
	}
	return nil
}

// Execute runs the scenario stages in order.
func (s *Scenario) Execute(ctx context.Context) (*scenarios.Result, error) {
	result := &scenarios.Result{
		ScenarioName: s.name,
		StartTime:    time.Now(),
		Success:      false,
		Metrics:      make(map[string]any),
		Details:      make(map[string]any),
		Errors:       []string{},
		Warnings:     []string{},
	}

	// cleanup runs on every exit path — success or failure — to remove seeded
	// state so a second scenario run against the same NATS deployment starts
	// from a clean baseline.
	defer s.cleanup(ctx, result)

	stages := []struct {
		name string
		fn   func(context.Context, *scenarios.Result) error
	}{
		{"verify-components", s.verifyComponents},
		{"verify-registered-tools", s.verifyRegisteredTools},
		{"seed-synthetic-loops", s.seedSyntheticLoops},
		{"inject-user-message", s.injectUserMessage},
		{"wait-for-loop-completion", s.waitForLoopCompletion},
		{"verify-diagnoses-via-http", s.verifyDiagnosesViaHTTP},
		// ADR-080 §5 lesson round-trip, gated at every step:
		{"verify-lesson-proposed", s.verifyLessonProposed}, // emit → proposed entity queryable
		{"promote-lesson", s.promoteLesson},                // operator promotion flips it active
		{"inject-and-verify-lesson", s.injectAndVerifyLesson},
	}

	for _, stage := range stages {
		stageStart := time.Now()
		if err := stage.fn(ctx, result); err != nil {
			result.Errors = append(result.Errors, fmt.Sprintf("%s: %v", stage.name, err))
			result.Error = fmt.Sprintf("%s failed: %v", stage.name, err)
			result.EndTime = time.Now()
			result.Duration = result.EndTime.Sub(result.StartTime)
			return result, nil
		}
		result.Metrics[fmt.Sprintf("%s_duration_ms", stage.name)] = time.Since(stageStart).Milliseconds()
	}

	result.Success = true
	result.EndTime = time.Now()
	result.Duration = result.EndTime.Sub(result.StartTime)
	return result, nil
}

// verifyComponents checks that all required ops-agent components are healthy.
func (s *Scenario) verifyComponents(ctx context.Context, result *scenarios.Result) error {
	components, err := s.obs.GetComponents(ctx)
	if err != nil {
		return fmt.Errorf("get components: %w", err)
	}
	required := []string{
		"agentic-dispatch",
		"agentic-loop",
		"agentic-model",
		"agentic-tools",
		"graph-ingest",
	}
	found := make(map[string]bool, len(components))
	for _, comp := range components {
		found[comp.Name] = comp.Enabled && comp.Healthy
	}
	var missing, unhealthy []string
	for _, req := range required {
		ok, exists := found[req]
		switch {
		case !exists:
			missing = append(missing, req)
		case !ok:
			unhealthy = append(unhealthy, req)
		}
	}
	result.Details["components_found"] = found
	if len(missing) > 0 {
		return fmt.Errorf("missing components: %v", missing)
	}
	if len(unhealthy) > 0 {
		return fmt.Errorf("unhealthy components: %v", unhealthy)
	}
	return nil
}

// verifyRegisteredTools greps container startup logs for the emit_diagnosis
// registration line. This catches wiring regressions where PR1's executor
// registration in executors/register.go is silently dropped.
//
// Also verifies PR4's /graph/triples endpoint responds with 200 — a fast
// smoke-check that the assertion mechanism itself is wired before the scenario
// spends time seeding data.
func (s *Scenario) verifyRegisteredTools(ctx context.Context, result *scenarios.Result) error {
	// Confirm emit_diagnosis registered at startup.
	cmd := exec.CommandContext(ctx, "docker", "logs", s.config.AppContainer)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("docker logs %s: %w", s.config.AppContainer, err)
	}
	logs := string(out)

	const emitDiagMarker = "Registered emit_diagnosis tool"
	if !strings.Contains(logs, emitDiagMarker) {
		return fmt.Errorf(
			"emit_diagnosis not registered at startup: %q not found in logs — "+
				"check processor/agentic-tools/executors/register.go registerEmitDiagnosis wiring",
			emitDiagMarker)
	}
	result.Details["emit_diagnosis_registered"] = true

	// Confirm emit_lesson registered at startup (ADR-080 §5, first in-repo
	// consumer). A missing line means registerEmitLesson was dropped from
	// RegisterBuiltins or the ops allowed_tools no longer grants emit_lesson.
	const emitLessonMarker = "Registered emit_lesson tool"
	if !strings.Contains(logs, emitLessonMarker) {
		return fmt.Errorf(
			"emit_lesson not registered at startup: %q not found in logs — "+
				"check processor/agentic-tools/executors/register.go registerEmitLesson wiring",
			emitLessonMarker)
	}
	result.Details["emit_lesson_registered"] = true

	// Smoke-check the /graph/triples HTTP endpoint (PR4).
	triplesURL := s.config.BaseURL + "/graph/triples?limit=1"
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, triplesURL, nil)
	if err != nil {
		return fmt.Errorf("build /graph/triples request: %w", err)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return fmt.Errorf("GET /graph/triples: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("/graph/triples health-check: expected 200, got %d", resp.StatusCode)
	}
	result.Details["graph_triples_endpoint_reachable"] = true
	return nil
}

// seedSyntheticLoops writes 3 synthetic LoopCompletedEvent records into
// AGENT_LOOPS KV and corresponding minimal entity triples into ENTITY_STATES KV.
//
// Two of the loops record success with role "test-agent"; one records failure
// with error category "network". The ops agent observes these and the mock's
// scripted emit_diagnosis calls reference their entity IDs as evidence.
//
// Writing to ENTITY_STATES directly bypasses graph-ingest, which is the
// right trade-off for test seeding — graph-ingest uses a NATS request/reply
// surface not exposed through NATSValidationClient, and the HTTP triple-query
// endpoint reads ENTITY_STATES directly.
func (s *Scenario) seedSyntheticLoops(ctx context.Context, result *scenarios.Result) error {
	now := time.Now()

	type loopSeed struct {
		loopID   string
		entityID string
		role     string
		outcome  string
		errMsg   string
	}

	seeds := []loopSeed{
		{
			loopID:   "seed-loop-001",
			entityID: SeedLoop1ID,
			role:     "test-agent",
			outcome:  agentic.OutcomeSuccess,
		},
		{
			loopID:   "seed-loop-002",
			entityID: SeedLoop2ID,
			role:     "test-agent",
			outcome:  agentic.OutcomeSuccess,
		},
		{
			loopID:   "seed-loop-003",
			entityID: SeedLoop3ID,
			role:     "probe",
			outcome:  agentic.OutcomeFailed,
			errMsg:   "network timeout: connection refused after 3 retries",
		},
	}

	for _, seed := range seeds {
		// Seed into AGENT_LOOPS KV as a completed-loop record.
		var loopData []byte
		var kvKey string
		var marshalErr error

		if seed.outcome == agentic.OutcomeFailed {
			ev := &agentic.LoopFailedEvent{
				LoopID:     seed.loopID,
				TaskID:     "seed-task-" + seed.loopID,
				Outcome:    seed.outcome,
				Role:       seed.role,
				Reason:     "network",
				Error:      seed.errMsg,
				Model:      "mock-model",
				Iterations: 3,
				TokensIn:   1200,
				TokensOut:  400,
				FailedAt:   now.Add(-5 * time.Minute),
			}
			loopData, marshalErr = json.Marshal(ev)
			kvKey = "COMPLETE_" + seed.loopID
		} else {
			ev := &agentic.LoopCompletedEvent{
				LoopID:      seed.loopID,
				TaskID:      "seed-task-" + seed.loopID,
				Outcome:     seed.outcome,
				Role:        seed.role,
				Result:      "task completed successfully",
				Model:       "mock-model",
				Iterations:  2,
				TokensIn:    800,
				TokensOut:   300,
				CompletedAt: now.Add(-10 * time.Minute),
			}
			loopData, marshalErr = json.Marshal(ev)
			kvKey = "COMPLETE_" + seed.loopID
		}
		if marshalErr != nil {
			return fmt.Errorf("marshal loop %s: %w", seed.loopID, marshalErr)
		}
		if err := s.nats.PutKV(ctx, loopsBucket, kvKey, loopData); err != nil {
			return fmt.Errorf("seed loop %s into %s: %w", kvKey, loopsBucket, err)
		}
		s.seededLoopKeys = append(s.seededLoopKeys, kvKey)

		// Seed a minimal entity into ENTITY_STATES KV with identifying triples.
		// The ops agent and graph HTTP endpoint both read this bucket.
		triples := []message.Triple{
			{
				Subject:   seed.entityID,
				Predicate: "agent.loop.role",
				Object:    seed.role,
				Source:    "e2e-ops-seed",
				Timestamp: now,
			},
			{
				Subject:   seed.entityID,
				Predicate: "agent.loop.outcome",
				Object:    seed.outcome,
				Source:    "e2e-ops-seed",
				Timestamp: now,
			},
		}
		if seed.outcome == agentic.OutcomeFailed {
			triples = append(triples, message.Triple{
				Subject:   seed.entityID,
				Predicate: "agent.step.error-category",
				Object:    "network",
				Source:    "e2e-ops-seed",
				Timestamp: now,
			})
		}
		entityState := graph.EntityState{
			ID:          seed.entityID,
			Triples:     triples,
			MessageType: message.Type{Domain: "agentic", Category: "loop-completed", Version: "1"},
			Version:     1,
			UpdatedAt:   now,
		}
		entityData, err := graph.MarshalEntityState(&entityState)
		if err != nil {
			return fmt.Errorf("marshal entity state %s: %w", seed.entityID, err)
		}
		if err := s.nats.PutKV(ctx, entityStatesBucket, seed.entityID, entityData); err != nil {
			return fmt.Errorf("seed entity %s into %s: %w", seed.entityID, entityStatesBucket, err)
		}
		s.seededEntityKeys = append(s.seededEntityKeys, seed.entityID)
	}

	result.Details["seeded_loop_ids"] = []string{SeedLoop1ID, SeedLoop2ID, SeedLoop3ID}
	result.Details["seeded_loops_count"] = len(seeds)
	return nil
}

// injectUserMessage publishes the emit-loop's user.message asking the ops agent
// to analyse the recent completed loops, emit diagnoses, and distil a lesson.
func (s *Scenario) injectUserMessage(ctx context.Context, result *scenarios.Result) error {
	msgID, err := s.publishUserMessage(ctx,
		"Analyse the recent completed loops, emit diagnoses for anything worth attention, "+
			"and distil a durable lesson for future ops loops.")
	if err != nil {
		return err
	}
	result.Details["message_id"] = msgID
	return nil
}

// publishUserMessage publishes one user.message with the given content and
// returns its message ID. Shared by the emit loop and the later inject loop so
// both dispatch through the identical production wire (agentic-dispatch →
// agent.task → agentic-loop).
func (s *Scenario) publishUserMessage(ctx context.Context, content string) (string, error) {
	msg := agentic.UserMessage{
		MessageID:   fmt.Sprintf("e2e-ops-%d", time.Now().UnixNano()),
		ChannelType: "cli",
		ChannelID:   "e2e-test",
		UserID:      "e2e-test-user",
		Content:     content,
		Timestamp:   time.Now(),
	}
	envelope := message.NewBaseMessage(msg.Schema(), &msg, "e2e-ops")
	data, err := json.Marshal(envelope)
	if err != nil {
		return "", fmt.Errorf("marshal user message: %w", err)
	}
	subject := fmt.Sprintf("user.message.cli.%s", msg.MessageID)
	if err := s.nats.Publish(ctx, subject, data); err != nil {
		return "", fmt.Errorf("publish to %s: %w", subject, err)
	}
	return msg.MessageID, nil
}

// waitForLoopCompletion polls AGENT_LOOPS KV for the ops loop's own COMPLETE_*
// key. Uses the 250ms-poll / deadline pattern matching the crud-tools scenario.
// Records the loop ID in result.Details for debugging.
func (s *Scenario) waitForLoopCompletion(ctx context.Context, result *scenarios.Result) error {
	deadline := time.Now().Add(s.config.CompleteTimeout)
	var lastErr error

	for time.Now().Before(deadline) {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(250 * time.Millisecond):
		}

		// List all AGENT_LOOPS keys looking for any COMPLETE_* key that was NOT
		// one of the seeds — that is the ops agent's own completion record.
		keys, err := s.nats.GetBucketKeysSample(ctx, loopsBucket, 50)
		if err != nil {
			lastErr = err
			continue
		}
		seededKeys := make(map[string]bool)
		for _, k := range s.seededLoopKeys {
			seededKeys[k] = true
		}
		for _, key := range keys {
			if strings.HasPrefix(key, "COMPLETE_") && !seededKeys[key] {
				result.Details["ops_loop_complete_key"] = key
				return nil
			}
		}
	}

	if lastErr != nil {
		return fmt.Errorf("ops loop did not complete within %v (last NATS error: %v)", s.config.CompleteTimeout, lastErr)
	}
	return fmt.Errorf(
		"ops loop did not complete within %v — no non-seed COMPLETE_* key appeared in %s; "+
			"check agentic-loop timeout and that the mock LLM's submit_work call terminated the loop",
		s.config.CompleteTimeout, loopsBucket)
}

// verifyDiagnosesViaHTTP is the load-bearing assertion for this scenario.
// It calls GET /graph/triples?predicate=ops.diagnosis.finding and asserts:
//   - 3 finding triples (one per mock emit_diagnosis call).
//   - Each finding has an ops.diagnosis.evidence triple pointing to one of the
//     seeded loop entity IDs.
//   - Each finding has an ops.diagnosis.confidence triple with a valid float in [0,1].
//
// No completion-text substring matching — this is the framework's strongest
// assertion class for agent effects: verify the knowledge graph directly.
func (s *Scenario) verifyDiagnosesViaHTTP(ctx context.Context, result *scenarios.Result) error {
	findingTriples, err := s.queryTriples(ctx, "ops.diagnosis.finding", "")
	if err != nil {
		return fmt.Errorf("query ops.diagnosis.finding triples: %w", err)
	}
	result.Details["finding_triples_count"] = len(findingTriples)
	if len(findingTriples) != 3 {
		return fmt.Errorf(
			"expected 3 ops.diagnosis.finding triples, got %d — "+
				"check mock LLM preset: applyOpsPreset should script exactly 3 emit_diagnosis calls",
			len(findingTriples))
	}
	if err := assertFindingSubjectsValid(findingTriples); err != nil {
		return err
	}
	result.Details["finding_subjects_valid"] = true

	findingSubjects := findingSubjectSet(findingTriples)
	seededIDs := map[string]bool{SeedLoop1ID: true, SeedLoop2ID: true, SeedLoop3ID: true}

	if err := s.assertEvidenceTriples(ctx, result, findingTriples, findingSubjects, seededIDs); err != nil {
		return err
	}
	return s.assertConfidenceTriples(ctx, result, findingSubjects)
}

// assertFindingSubjectsValid checks each finding triple's subject matches the
// 6-part ops diagnosis entity ID shape: *.ops.diagnosis.finding.*.
func assertFindingSubjectsValid(triples []message.Triple) error {
	for i, t := range triples {
		parts := strings.Split(t.Subject, ".")
		if len(parts) != 6 {
			return fmt.Errorf("finding[%d] subject %q has %d parts, want 6", i, t.Subject, len(parts))
		}
		if parts[2] != "ops" || parts[3] != "diagnosis" || parts[4] != "finding" {
			return fmt.Errorf("finding[%d] subject %q does not match *.ops.diagnosis.finding.*", i, t.Subject)
		}
	}
	return nil
}

// findingSubjectSet builds a set of finding triple subjects for downstream cross-query checks.
func findingSubjectSet(triples []message.Triple) map[string]bool {
	m := make(map[string]bool, len(triples))
	for _, t := range triples {
		m[t.Subject] = true
	}
	return m
}

// assertEvidenceTriples queries ops.diagnosis.evidence and verifies each
// finding cites at least one seeded loop entity ID.
func (s *Scenario) assertEvidenceTriples(
	ctx context.Context,
	result *scenarios.Result,
	findingTriples []message.Triple,
	findingSubjects, seededIDs map[string]bool,
) error {
	evidenceTriples, err := s.queryTriples(ctx, "ops.diagnosis.evidence", "")
	if err != nil {
		return fmt.Errorf("query ops.diagnosis.evidence triples: %w", err)
	}
	evidenceByFinding := make(map[string][]string)
	for _, t := range evidenceTriples {
		if findingSubjects[t.Subject] {
			evidenceByFinding[t.Subject] = append(evidenceByFinding[t.Subject], fmt.Sprintf("%v", t.Object))
		}
	}
	for i, ft := range findingTriples {
		evidences := evidenceByFinding[ft.Subject]
		if len(evidences) == 0 {
			return fmt.Errorf("finding[%d] subject %q has no evidence triples", i, ft.Subject)
		}
		if !hasSeededEvidence(evidences, seededIDs) {
			return fmt.Errorf(
				"finding[%d] %q has no evidence pointing to a seeded loop entity — "+
					"evidence found: %v, seeded IDs: %v",
				i, ft.Subject, evidences, []string{SeedLoop1ID, SeedLoop2ID, SeedLoop3ID})
		}
	}
	result.Details["evidence_triples_count"] = len(evidenceTriples)
	result.Details["evidence_by_finding_count"] = len(evidenceByFinding)
	return nil
}

// hasSeededEvidence reports whether any value in evidences matches a key in seededIDs.
func hasSeededEvidence(evidences []string, seededIDs map[string]bool) bool {
	for _, ev := range evidences {
		if seededIDs[ev] {
			return true
		}
	}
	return false
}

// assertConfidenceTriples queries ops.diagnosis.confidence and verifies each
// finding's confidence is a valid float in [0, 1].
func (s *Scenario) assertConfidenceTriples(
	ctx context.Context,
	result *scenarios.Result,
	findingSubjects map[string]bool,
) error {
	confidenceTriples, err := s.queryTriples(ctx, "ops.diagnosis.confidence", "")
	if err != nil {
		return fmt.Errorf("query ops.diagnosis.confidence triples: %w", err)
	}
	for i, t := range confidenceTriples {
		if !findingSubjects[t.Subject] {
			continue
		}
		conf, err := extractConfidence(t.Object)
		if err != nil {
			return fmt.Errorf("confidence[%d] for %q: %w", i, t.Subject, err)
		}
		if conf < 0 || conf > 1 {
			return fmt.Errorf("confidence[%d] for %q: value %.3f out of [0, 1]", i, t.Subject, conf)
		}
	}
	result.Details["confidence_triples_valid"] = true
	return nil
}

// verifyLessonProposed is stage 2 of the ADR-080 §5 round-trip: the emit loop's
// emit_lesson call must have minted exactly one agent.lesson.record entity, born
// status="proposed" and carrying the injection form + scope key the inject stage
// later depends on. The minted UUIDv5 is content-derived, so the scenario reads
// the entity ID back from the graph (via /graph/triples) rather than recomputing
// the hash — the framework's strongest assertion class.
func (s *Scenario) verifyLessonProposed(ctx context.Context, result *scenarios.Result) error {
	statusTriples, err := s.waitForTriples(ctx, agvocab.LessonStatus, 1)
	if err != nil {
		return fmt.Errorf(
			"agent.lesson.status triple did not appear: %w — check the emit_lesson mock entry "+
				"and that emit_lesson is in the ops allowed_tools", err)
	}
	if len(statusTriples) != 1 {
		return fmt.Errorf("expected exactly 1 agent.lesson.status triple, got %d (subjects: %v)",
			len(statusTriples), tripleSubjects(statusTriples))
	}

	lesson := statusTriples[0]
	parts := strings.Split(lesson.Subject, ".")
	if len(parts) != 6 || parts[2] != "agent" || parts[3] != "lesson" || parts[4] != "record" {
		return fmt.Errorf("lesson subject %q does not match *.*.agent.lesson.record.* (6 parts)", lesson.Subject)
	}
	if status := fmt.Sprintf("%v", lesson.Object); status != "proposed" {
		return fmt.Errorf("lesson %q status = %q, want %q (lessons are born proposed)", lesson.Subject, status, "proposed")
	}
	s.lessonEntityID = lesson.Subject
	result.Details["lesson_entity_id"] = lesson.Subject
	result.Details["lesson_status_born"] = "proposed"

	// The injection form + scope key + evidence must be persisted, since the
	// inject stage and the promotion gate depend on them.
	if err := s.assertLessonObject(ctx, agvocab.LessonInjectionForm, LessonInjectionForm); err != nil {
		return err
	}
	if err := s.assertLessonObject(ctx, agvocab.LessonAppliesTo, LessonAppliesToTag); err != nil {
		return err
	}
	return s.assertLessonObject(ctx, agvocab.LessonEvidence, SeedLoop1ID)
}

// promoteLesson is stage 3: acting as the operator, drive the validated
// LessonCurator promotion path (NOT an agent tool). The E2E binary serves this
// request through its already-bound contract client, so the harness never takes
// over or observes the production owner token.
func (s *Scenario) promoteLesson(ctx context.Context, result *scenarios.Result) error {
	if s.lessonEntityID == "" {
		return fmt.Errorf("no lesson entity id captured from verify-lesson-proposed")
	}
	requestData, err := json.Marshal(lessoncuration.PromoteRequest{EntityID: s.lessonEntityID})
	if err != nil {
		return fmt.Errorf("encode lesson promotion request: %w", err)
	}
	responseData, err := s.nats.Client().RequestClassified(
		ctx, lessoncuration.SubjectPromote, requestData, 5*time.Second,
	)
	if err != nil {
		return fmt.Errorf(
			"promote lesson %s: %w — the evidence-existence gate must resolve the seeded evidence entity %s",
			s.lessonEntityID, err, SeedLoop1ID)
	}
	var response lessoncuration.PromoteResponse
	if err := json.Unmarshal(responseData, &response); err != nil {
		return fmt.Errorf("decode lesson promotion response: %w", err)
	}
	if !response.Promoted {
		return fmt.Errorf("lesson promotion responder did not confirm promotion for %s", s.lessonEntityID)
	}
	result.Details["lesson_promoted"] = true

	// Assert the flip landed and is single-valued: exactly one status triple for
	// this lesson, now "active".
	deadline := time.Now().Add(s.config.CompleteTimeout)
	var lastSeen string
	for time.Now().Before(deadline) {
		triples, err := s.queryTriples(ctx, agvocab.LessonStatus, "")
		if err == nil {
			lastSeen = lessonStatusFor(triples, s.lessonEntityID)
			if lastSeen == "active" {
				result.Details["lesson_status_after_promotion"] = "active"
				return nil
			}
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(250 * time.Millisecond):
		}
	}
	return fmt.Errorf(
		"lesson %s status did not flip to active within %v (last seen %q) — "+
			"check LessonCurator.Promote and the lessonRecordProjectionContract replace lane",
		s.lessonEntityID, s.config.CompleteTimeout, lastSeen)
}

// injectAndVerifyLesson is stage 4: dispatch a SUBSEQUENT ops loop and prove the
// promoted lesson's injection form reached its brief. The loop's role is ops
// (default_role: ops), so brief assembly derives scope tag:ops, which matches the
// lesson's applies_to tag:ops. The mock's injection-form-gated emit_diagnosis
// fires ONLY when that injection form is present in the assembled system prompt,
// leaving InjectionProofFinding in the graph — the deterministic end-to-end proof.
func (s *Scenario) injectAndVerifyLesson(ctx context.Context, result *scenarios.Result) error {
	msgID, err := s.publishUserMessage(ctx,
		"Analyse the latest completed loops and apply any durable guidance from prior work.")
	if err != nil {
		return fmt.Errorf("dispatch inject loop: %w", err)
	}
	result.Details["inject_loop_message_id"] = msgID

	deadline := time.Now().Add(s.config.CompleteTimeout)
	var lastErr error
	for time.Now().Before(deadline) {
		findings, err := s.queryTriples(ctx, "ops.diagnosis.finding", "")
		if err != nil {
			lastErr = err
		} else {
			for _, t := range findings {
				if fmt.Sprintf("%v", t.Object) == InjectionProofFinding {
					result.Details["injection_proof_finding_subject"] = t.Subject
					result.Details["lesson_injection_confirmed"] = true
					return nil
				}
			}
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(250 * time.Millisecond):
		}
	}
	if lastErr != nil {
		return fmt.Errorf(
			"injection-proof finding did not appear within %v (last query error: %v)",
			s.config.CompleteTimeout, lastErr)
	}
	return fmt.Errorf(
		"injection-proof finding %q did not appear within %v — the promoted lesson's injection form "+
			"did not reach the subsequent ops loop's brief; check agentic-loop brief-assembly injection "+
			"(SetLessonReader wiring, lessonmatch tag:%s scope, active-status filter)",
		InjectionProofFinding, s.config.CompleteTimeout, OpsRoleTag)
}

// waitForTriples polls /graph/triples for the given predicate until at least
// wantAtLeast triples are returned or the completion deadline elapses. Returns
// the last non-empty result seen; the caller asserts the exact shape.
func (s *Scenario) waitForTriples(ctx context.Context, predicate string, wantAtLeast int) ([]message.Triple, error) {
	deadline := time.Now().Add(s.config.CompleteTimeout)
	var latest []message.Triple
	var lastErr error
	for time.Now().Before(deadline) {
		triples, err := s.queryTriples(ctx, predicate, "")
		if err != nil {
			lastErr = err
		} else {
			latest = triples
			if len(triples) >= wantAtLeast {
				return triples, nil
			}
		}
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-time.After(250 * time.Millisecond):
		}
	}
	if lastErr != nil {
		return latest, fmt.Errorf("within %v (last error: %w)", s.config.CompleteTimeout, lastErr)
	}
	return latest, fmt.Errorf("within %v: got %d, want >= %d", s.config.CompleteTimeout, len(latest), wantAtLeast)
}

// assertLessonObject asserts the captured lesson entity carries a triple with
// the given predicate whose object equals want. Queries by predicate only (the
// endpoint's subject filter is exercised elsewhere as predicate-only) and filters
// the captured subject in Go.
func (s *Scenario) assertLessonObject(ctx context.Context, predicate, want string) error {
	triples, err := s.queryTriples(ctx, predicate, "")
	if err != nil {
		return fmt.Errorf("query %s: %w", predicate, err)
	}
	for _, t := range triples {
		if t.Subject == s.lessonEntityID && fmt.Sprintf("%v", t.Object) == want {
			return nil
		}
	}
	return fmt.Errorf("lesson %s missing %s == %q (found: %v)", s.lessonEntityID, predicate, want, lessonObjectsFor(triples, s.lessonEntityID))
}

// tripleSubjects lists the subjects of a triple slice for diagnostics.
func tripleSubjects(triples []message.Triple) []string {
	out := make([]string, 0, len(triples))
	for _, t := range triples {
		out = append(out, t.Subject)
	}
	return out
}

// lessonStatusFor returns the object of the status triple for entityID, or "".
func lessonStatusFor(triples []message.Triple, entityID string) string {
	for _, t := range triples {
		if t.Subject == entityID {
			return fmt.Sprintf("%v", t.Object)
		}
	}
	return ""
}

// lessonObjectsFor lists the objects of triples whose subject is entityID.
func lessonObjectsFor(triples []message.Triple, entityID string) []string {
	out := []string{}
	for _, t := range triples {
		if t.Subject == entityID {
			out = append(out, fmt.Sprintf("%v", t.Object))
		}
	}
	return out
}

// extractConfidence parses a triple Object value as a float64 confidence score.
// JSON numbers arrive as float64; json.Number is also handled for decoder variants.
// String values are accepted because emit_diagnosis stores confidence via
// fmt.Sprintf("%g", ...) which round-trips through JSON as a quoted string.
func extractConfidence(obj any) (float64, error) {
	switch v := obj.(type) {
	case float64:
		return v, nil
	case json.Number:
		return v.Float64()
	case string:
		f, err := strconv.ParseFloat(v, 64)
		if err != nil {
			return 0, fmt.Errorf("confidence string %q is not a valid float: %w", v, err)
		}
		return f, nil
	default:
		return 0, fmt.Errorf("unexpected type %T (value %v)", obj, obj)
	}
}

// queryTriples calls GET /graph/triples with optional predicate and subject
// filters and returns the decoded triple slice. This is a thin helper so the
// verification stage can call the endpoint multiple times without repeating
// HTTP wiring.
func (s *Scenario) queryTriples(ctx context.Context, predicate, subject string) ([]message.Triple, error) {
	url := s.config.BaseURL + "/graph/triples"
	params := []string{}
	if predicate != "" {
		params = append(params, "predicate="+predicate)
	}
	if subject != "" {
		params = append(params, "subject="+subject)
	}
	params = append(params, "limit=100")
	if len(params) > 0 {
		url += "?" + strings.Join(params, "&")
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, fmt.Errorf("build request: %w", err)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("GET %s: %w", url, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("GET %s: status %d body=%q", url, resp.StatusCode, string(body))
	}

	var triples []message.Triple
	if err := json.NewDecoder(resp.Body).Decode(&triples); err != nil {
		return nil, fmt.Errorf("decode triples: %w", err)
	}
	return triples, nil
}

// cleanup removes seeded KV entries so a second run of the scenario against
// the same NATS deployment starts from a clean baseline. Errors during cleanup
// are non-fatal — docker-compose down -v wipes all state on next run anyway.
func (s *Scenario) cleanup(ctx context.Context, result *scenarios.Result) {
	for _, key := range s.seededLoopKeys {
		if err := s.nats.DeleteKV(ctx, loopsBucket, key); err != nil {
			result.Warnings = append(result.Warnings,
				fmt.Sprintf("cleanup: delete %s/%s: %v", loopsBucket, key, err))
		}
	}
	for _, key := range s.seededEntityKeys {
		if err := s.nats.DeleteKV(ctx, entityStatesBucket, key); err != nil {
			result.Warnings = append(result.Warnings,
				fmt.Sprintf("cleanup: delete %s/%s: %v", entityStatesBucket, key, err))
		}
	}
}

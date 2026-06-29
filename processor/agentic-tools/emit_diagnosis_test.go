package agentictools

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"testing"

	"github.com/c360studio/semstreams/agentic"
	"github.com/c360studio/semstreams/message"
	"github.com/c360studio/semstreams/types"
	agvocab "github.com/c360studio/semstreams/vocabulary/agentic"
)

func newEmitDiagnosisExecutor(publisher TriplePublisher) *EmitDiagnosisExecutor {
	return NewEmitDiagnosisExecutor(
		publisher,
		types.PlatformMeta{Org: "acme", Platform: "test"},
		slog.Default(),
	)
}

// TestEmitDiagnosisExecutor_BirthsFindingEntity confirms emit_diagnosis BIRTHS
// the finding entity via create_with_triples (gh#390), NOT via add_batch. Each
// call mints a NEW ops.diagnosis.finding.{uuid} entity; graph-ingest enforces
// must-exist on add/add_batch, so a bare batch would be rejected and the finding
// would silently never land (e2e:ops: 0/3 findings). create_with_triples is
// atomic, preserving the no-partial-finding contract.
func TestEmitDiagnosisExecutor_BirthsFindingEntity(t *testing.T) {
	pub := &recordingPublisher{}
	e := newEmitDiagnosisExecutor(pub)
	_, err := e.Execute(context.Background(), validEmitDiagnosisCall())
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	// Exactly one BIRTH, and no append-path call (the entity didn't exist yet).
	if pub.createCalls != 1 {
		t.Errorf("CreateEntityWithTriples call count = %d, want 1 (single atomic birth)", pub.createCalls)
	}
	if pub.batchCalls != 0 {
		t.Errorf("AddTriplesBatch call count = %d, want 0 (the finding entity must be created, not appended)", pub.batchCalls)
	}
	// Born with the ops_diagnosis typed-origin envelope on the finding entity ID.
	if got, want := pub.createdMsgType.Key(), agentic.OpsDiagnosisMessageType().Key(); got != want {
		t.Errorf("birth MessageType = %q, want %q", got, want)
	}
	if !message.IsValidEntityID(pub.createdEntityID) {
		t.Errorf("birth entity ID %q is not a valid 6-part entity ID", pub.createdEntityID)
	}
}

func validEmitDiagnosisCall() agentic.ToolCall {
	return agentic.ToolCall{
		ID:     "c1",
		Name:   EmitDiagnosisToolName,
		LoopID: "loop-ops-abc",
		Arguments: map[string]any{
			"finding":        "researcher loop exceeded token budget",
			"recommendation": "reduce max_tokens for researcher endpoint to 4096",
			"confidence":     0.85,
			"evidence":       []any{"acme.test.agent.agentic-loop.execution.loop-abc"},
			"observed_role":  "researcher",
			"severity":       "warn",
		},
	}
}

// TestEmitDiagnosisExecutor_ListTools verifies the schema exposes required
// and optional fields correctly.
func TestEmitDiagnosisExecutor_ListTools(t *testing.T) {
	e := newEmitDiagnosisExecutor(&recordingPublisher{})
	tools := e.ListTools()
	if len(tools) != 1 {
		t.Fatalf("expected 1 tool, got %d", len(tools))
	}
	if tools[0].Name != EmitDiagnosisToolName {
		t.Errorf("expected tool name %q, got %q", EmitDiagnosisToolName, tools[0].Name)
	}
	required, _ := tools[0].Parameters["required"].([]string)
	wantRequired := map[string]bool{"finding": true, "recommendation": true, "confidence": true, "evidence": true}
	for _, r := range required {
		if !wantRequired[r] {
			t.Errorf("unexpected required field: %s", r)
		}
		delete(wantRequired, r)
	}
	if len(wantRequired) > 0 {
		t.Errorf("missing required fields: %v", wantRequired)
	}
}

// TestEmitDiagnosisExecutor_HappyPath verifies a fully-specified call emits
// the correct triple set, does not stop the loop, and returns the diagnosis
// entity ID in Metadata.
func TestEmitDiagnosisExecutor_HappyPath(t *testing.T) {
	pub := &recordingPublisher{}
	e := newEmitDiagnosisExecutor(pub)

	res, err := e.Execute(context.Background(), validEmitDiagnosisCall())
	if err != nil {
		t.Fatalf("unexpected err: %v", err)
	}
	if res.Error != "" {
		t.Fatalf("tool error: %s", res.Error)
	}
	if res.StopLoop {
		t.Errorf("StopLoop must be false — ops agent emits multiple findings per loop")
	}

	// 1 finding + 1 recommendation + 1 confidence + 1 evidence + 1 observed_role
	// + 1 severity + 1 executed_by = 7 triples
	if len(pub.triples) != 7 {
		t.Fatalf("expected 7 triples, got %d", len(pub.triples))
	}

	byPredicate := map[string][]any{}
	for _, tr := range pub.triples {
		byPredicate[tr.Predicate] = append(byPredicate[tr.Predicate], tr.Object)
	}

	assertTriple := func(pred, wantObj string) {
		t.Helper()
		vals := byPredicate[pred]
		if len(vals) == 0 {
			t.Errorf("no triple with predicate %q", pred)
			return
		}
		got, ok := vals[0].(string)
		if !ok {
			t.Errorf("predicate %q: object is not a string: %T", pred, vals[0])
			return
		}
		if got != wantObj {
			t.Errorf("predicate %q: object = %q, want %q", pred, got, wantObj)
		}
	}

	assertTriple(agvocab.OpsDiagnosisFinding, "researcher loop exceeded token budget")
	assertTriple(agvocab.OpsDiagnosisRecommendation, "reduce max_tokens for researcher endpoint to 4096")
	assertTriple(agvocab.OpsDiagnosisConfidence, "0.85")
	assertTriple(agvocab.OpsDiagnosisEvidence, "acme.test.agent.agentic-loop.execution.loop-abc")
	assertTriple(agvocab.OpsDiagnosisObservedRole, "researcher")
	assertTriple(agvocab.OpsDiagnosisSeverity, "warn")

	// executed_by back-link must point to the ops loop entity.
	wantLoopEntity := "acme.test.agent.agentic-loop.execution.loop-ops-abc"
	assertTriple("agent.action.executed_by", wantLoopEntity)

	// Every triple must share the same diagnosis entity as subject (not the
	// loop entity) — verifies the entity ID shape is correct.
	diagEntity, ok := res.Metadata["diagnosis_id"].(string)
	if !ok || diagEntity == "" {
		t.Fatalf("Metadata[diagnosis_id] must be a non-empty string")
	}
	if !message.IsValidEntityID(diagEntity) {
		t.Errorf("diagnosis entity ID %q failed IsValidEntityID", diagEntity)
	}
	for _, tr := range pub.triples {
		if tr.Subject != diagEntity {
			t.Errorf("triple predicate %q has subject %q, want %q", tr.Predicate, tr.Subject, diagEntity)
		}
	}

	// Diagnosis entity must carry the org and platform prefix.
	if !strings.HasPrefix(diagEntity, "acme.test.ops.diagnosis.finding.") {
		t.Errorf("diagnosis entity %q must start with acme.test.ops.diagnosis.finding.", diagEntity)
	}

	// Source must be the ops-emit-diagnosis tag on every triple.
	for _, tr := range pub.triples {
		if tr.Source != emitDiagnosisSource {
			t.Errorf("triple predicate %q has source %q, want %q", tr.Predicate, tr.Source, emitDiagnosisSource)
		}
	}

	// Triple confidence must match the tool's self-reported confidence.
	for _, tr := range pub.triples {
		if tr.Confidence != 0.85 {
			t.Errorf("triple predicate %q has Confidence %v, want 0.85", tr.Predicate, tr.Confidence)
		}
	}
}

// TestEmitDiagnosisExecutor_MissingFinding verifies that omitting the
// required finding field surfaces ToolErrorInvalidArgs without publishing.
func TestEmitDiagnosisExecutor_MissingFinding(t *testing.T) {
	pub := &recordingPublisher{}
	e := newEmitDiagnosisExecutor(pub)

	res, err := e.Execute(context.Background(), agentic.ToolCall{
		ID:     "c1",
		Name:   EmitDiagnosisToolName,
		LoopID: "loop-ops-abc",
		Arguments: map[string]any{
			"recommendation": "reduce max_tokens",
			"confidence":     0.5,
			"evidence":       []any{"acme.test.agent.agentic-loop.execution.loop-abc"},
		},
	})
	if err != nil {
		t.Fatalf("unexpected wrapped err: %v", err)
	}
	if res.ErrorKind != agentic.ToolErrorInvalidArgs {
		t.Errorf("ErrorKind = %v, want ToolErrorInvalidArgs", res.ErrorKind)
	}
	if len(pub.triples) != 0 {
		t.Errorf("no triples should be published on invalid args, got %d", len(pub.triples))
	}
}

// TestEmitDiagnosisExecutor_MissingRecommendation verifies the other
// required string field.
func TestEmitDiagnosisExecutor_MissingRecommendation(t *testing.T) {
	pub := &recordingPublisher{}
	e := newEmitDiagnosisExecutor(pub)

	res, _ := e.Execute(context.Background(), agentic.ToolCall{
		ID:     "c1",
		Name:   EmitDiagnosisToolName,
		LoopID: "loop-ops-abc",
		Arguments: map[string]any{
			"finding":    "something wrong",
			"confidence": 0.5,
			"evidence":   []any{"acme.test.agent.agentic-loop.execution.loop-abc"},
		},
	})
	if res.ErrorKind != agentic.ToolErrorInvalidArgs {
		t.Errorf("ErrorKind = %v, want ToolErrorInvalidArgs", res.ErrorKind)
	}
}

// TestEmitDiagnosisExecutor_MissingConfidence verifies that omitting
// confidence surfaces ToolErrorInvalidArgs.
func TestEmitDiagnosisExecutor_MissingConfidence(t *testing.T) {
	pub := &recordingPublisher{}
	e := newEmitDiagnosisExecutor(pub)

	res, _ := e.Execute(context.Background(), agentic.ToolCall{
		ID:     "c1",
		Name:   EmitDiagnosisToolName,
		LoopID: "loop-ops-abc",
		Arguments: map[string]any{
			"finding":        "something wrong",
			"recommendation": "fix it",
			"evidence":       []any{"acme.test.agent.agentic-loop.execution.loop-abc"},
		},
	})
	if res.ErrorKind != agentic.ToolErrorInvalidArgs {
		t.Errorf("ErrorKind = %v, want ToolErrorInvalidArgs", res.ErrorKind)
	}
}

// TestEmitDiagnosisExecutor_NonNumericConfidence verifies that a
// non-numeric confidence value (e.g. string) surfaces ToolErrorInvalidArgs.
func TestEmitDiagnosisExecutor_NonNumericConfidence(t *testing.T) {
	pub := &recordingPublisher{}
	e := newEmitDiagnosisExecutor(pub)

	res, _ := e.Execute(context.Background(), agentic.ToolCall{
		ID:     "c1",
		Name:   EmitDiagnosisToolName,
		LoopID: "loop-ops-abc",
		Arguments: map[string]any{
			"finding":        "something wrong",
			"recommendation": "fix it",
			"confidence":     "high", // should be a number
			"evidence":       []any{"acme.test.agent.agentic-loop.execution.loop-abc"},
		},
	})
	if res.ErrorKind != agentic.ToolErrorInvalidArgs {
		t.Errorf("ErrorKind = %v, want ToolErrorInvalidArgs", res.ErrorKind)
	}
}

// TestEmitDiagnosisExecutor_ConfidenceOutOfRange verifies bounds enforcement
// on the confidence field.
func TestEmitDiagnosisExecutor_ConfidenceOutOfRange(t *testing.T) {
	pub := &recordingPublisher{}
	e := newEmitDiagnosisExecutor(pub)

	tests := []struct {
		name       string
		confidence float64
	}{
		{"negative", -0.1},
		{"above one", 1.1},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			res, _ := e.Execute(context.Background(), agentic.ToolCall{
				ID:     "c1",
				Name:   EmitDiagnosisToolName,
				LoopID: "loop-ops-abc",
				Arguments: map[string]any{
					"finding":        "something wrong",
					"recommendation": "fix it",
					"confidence":     tt.confidence,
					"evidence":       []any{"acme.test.agent.agentic-loop.execution.loop-abc"},
				},
			})
			if res.ErrorKind != agentic.ToolErrorInvalidArgs {
				t.Errorf("confidence=%g: ErrorKind = %v, want ToolErrorInvalidArgs", tt.confidence, res.ErrorKind)
			}
		})
	}
}

// TestEmitDiagnosisExecutor_EmptyEvidence verifies that an empty evidence
// array surfaces ToolErrorInvalidArgs.
func TestEmitDiagnosisExecutor_EmptyEvidence(t *testing.T) {
	pub := &recordingPublisher{}
	e := newEmitDiagnosisExecutor(pub)

	res, _ := e.Execute(context.Background(), agentic.ToolCall{
		ID:     "c1",
		Name:   EmitDiagnosisToolName,
		LoopID: "loop-ops-abc",
		Arguments: map[string]any{
			"finding":        "something wrong",
			"recommendation": "fix it",
			"confidence":     0.5,
			"evidence":       []any{}, // empty — must be rejected
		},
	})
	if res.ErrorKind != agentic.ToolErrorInvalidArgs {
		t.Errorf("ErrorKind = %v, want ToolErrorInvalidArgs", res.ErrorKind)
	}
}

// TestEmitDiagnosisExecutor_MissingEvidence verifies that omitting the
// evidence key surfaces ToolErrorInvalidArgs.
func TestEmitDiagnosisExecutor_MissingEvidence(t *testing.T) {
	pub := &recordingPublisher{}
	e := newEmitDiagnosisExecutor(pub)

	res, _ := e.Execute(context.Background(), agentic.ToolCall{
		ID:     "c1",
		Name:   EmitDiagnosisToolName,
		LoopID: "loop-ops-abc",
		Arguments: map[string]any{
			"finding":        "something wrong",
			"recommendation": "fix it",
			"confidence":     0.5,
			// evidence key entirely absent
		},
	})
	if res.ErrorKind != agentic.ToolErrorInvalidArgs {
		t.Errorf("ErrorKind = %v, want ToolErrorInvalidArgs", res.ErrorKind)
	}
}

// TestEmitDiagnosisExecutor_ObservedRoleOmitted verifies that when
// observed_role is absent no triple is emitted for that predicate.
func TestEmitDiagnosisExecutor_ObservedRoleOmitted(t *testing.T) {
	pub := &recordingPublisher{}
	e := newEmitDiagnosisExecutor(pub)

	res, err := e.Execute(context.Background(), agentic.ToolCall{
		ID:     "c1",
		Name:   EmitDiagnosisToolName,
		LoopID: "loop-ops-abc",
		Arguments: map[string]any{
			"finding":        "something wrong",
			"recommendation": "fix it",
			"confidence":     0.5,
			"evidence":       []any{"acme.test.agent.agentic-loop.execution.loop-abc"},
			// observed_role intentionally absent
		},
	})
	if err != nil {
		t.Fatalf("unexpected err: %v", err)
	}
	if res.Error != "" {
		t.Fatalf("tool error: %s", res.Error)
	}

	// Without observed_role: finding + recommendation + confidence + evidence
	// + severity + executed_by = 6 triples (no observed_role triple).
	if len(pub.triples) != 6 {
		t.Errorf("expected 6 triples (no observed_role), got %d", len(pub.triples))
	}

	for _, tr := range pub.triples {
		if tr.Predicate == agvocab.OpsDiagnosisObservedRole {
			t.Errorf("unexpected observed_role triple emitted when field was absent")
		}
	}
}

// TestEmitDiagnosisExecutor_SeverityDefaultsToInfo verifies that when
// severity is omitted the executor emits an "info" severity triple.
func TestEmitDiagnosisExecutor_SeverityDefaultsToInfo(t *testing.T) {
	pub := &recordingPublisher{}
	e := newEmitDiagnosisExecutor(pub)

	_, err := e.Execute(context.Background(), agentic.ToolCall{
		ID:     "c1",
		Name:   EmitDiagnosisToolName,
		LoopID: "loop-ops-abc",
		Arguments: map[string]any{
			"finding":        "something wrong",
			"recommendation": "fix it",
			"confidence":     0.5,
			"evidence":       []any{"acme.test.agent.agentic-loop.execution.loop-abc"},
			// severity intentionally absent
		},
	})
	if err != nil {
		t.Fatalf("unexpected err: %v", err)
	}

	for _, tr := range pub.triples {
		if tr.Predicate == agvocab.OpsDiagnosisSeverity {
			if tr.Object != "info" {
				t.Errorf("severity triple object = %q, want %q (default)", tr.Object, "info")
			}
			return
		}
	}
	t.Errorf("no severity triple emitted")
}

// TestEmitDiagnosisExecutor_SeverityInvalidClampsToInfo verifies the
// decision to clamp invalid severity values to "info" rather than rejecting
// them. Small models occasionally emit free-text severity like "medium" or
// "low"; clamping lets the finding land rather than bouncing.
func TestEmitDiagnosisExecutor_SeverityInvalidClampsToInfo(t *testing.T) {
	pub := &recordingPublisher{}
	e := newEmitDiagnosisExecutor(pub)

	_, err := e.Execute(context.Background(), agentic.ToolCall{
		ID:     "c1",
		Name:   EmitDiagnosisToolName,
		LoopID: "loop-ops-abc",
		Arguments: map[string]any{
			"finding":        "something wrong",
			"recommendation": "fix it",
			"confidence":     0.5,
			"evidence":       []any{"acme.test.agent.agentic-loop.execution.loop-abc"},
			"severity":       "medium", // invalid enum value
		},
	})
	if err != nil {
		t.Fatalf("unexpected err: %v", err)
	}

	for _, tr := range pub.triples {
		if tr.Predicate == agvocab.OpsDiagnosisSeverity {
			if tr.Object != "info" {
				t.Errorf("invalid severity should clamp to %q, got %q", "info", tr.Object)
			}
			return
		}
	}
	t.Errorf("no severity triple emitted")
}

// TestEmitDiagnosisExecutor_PublisherFailure verifies that a publisher error
// on the first triple surfaces ToolErrorNetwork with a non-nil wrapped error.
// StopLoop stays false so the model sees the failure and can retry.
func TestEmitDiagnosisExecutor_PublisherFailure(t *testing.T) {
	pub := &recordingPublisher{err: errors.New("nats broken")}
	e := newEmitDiagnosisExecutor(pub)

	res, err := e.Execute(context.Background(), validEmitDiagnosisCall())
	if err == nil {
		t.Errorf("expected wrapped err for publish failure")
	}
	if res.ErrorKind != agentic.ToolErrorNetwork {
		t.Errorf("ErrorKind = %v, want ToolErrorNetwork", res.ErrorKind)
	}
	if res.StopLoop {
		t.Errorf("StopLoop should stay false when the finding didn't actually land")
	}
}

// TestEmitDiagnosisExecutor_MultiEvidence verifies that 5 evidence entries
// produce exactly 5 distinct evidence triples.
func TestEmitDiagnosisExecutor_MultiEvidence(t *testing.T) {
	pub := &recordingPublisher{}
	e := newEmitDiagnosisExecutor(pub)

	evidence := []any{
		"acme.test.agent.agentic-loop.execution.loop-001",
		"acme.test.agent.agentic-loop.execution.loop-002",
		"acme.test.agent.agentic-loop.execution.loop-003",
		"acme.test.agent.agentic-loop.execution.loop-004",
		"acme.test.agent.agentic-loop.execution.loop-005",
	}

	_, err := e.Execute(context.Background(), agentic.ToolCall{
		ID:     "c1",
		Name:   EmitDiagnosisToolName,
		LoopID: "loop-ops-abc",
		Arguments: map[string]any{
			"finding":        "pattern across loops",
			"recommendation": "investigate shared config",
			"confidence":     0.9,
			"evidence":       evidence,
		},
	})
	if err != nil {
		t.Fatalf("unexpected err: %v", err)
	}

	evidenceTriples := 0
	for _, tr := range pub.triples {
		if tr.Predicate == agvocab.OpsDiagnosisEvidence {
			evidenceTriples++
		}
	}
	if evidenceTriples != 5 {
		t.Errorf("expected 5 evidence triples, got %d", evidenceTriples)
	}

	// Total without observed_role: finding + recommendation + confidence +
	// 5 evidence + severity + executed_by = 10.
	if len(pub.triples) != 10 {
		t.Errorf("expected 10 total triples, got %d", len(pub.triples))
	}
}

// TestEmitDiagnosisExecutor_UnknownTool verifies routing protection.
func TestEmitDiagnosisExecutor_UnknownTool(t *testing.T) {
	e := newEmitDiagnosisExecutor(&recordingPublisher{})
	res, err := e.Execute(context.Background(), agentic.ToolCall{
		ID:        "c1",
		Name:      "not_emit_diagnosis",
		LoopID:    "loop-ops-abc",
		Arguments: map[string]any{},
	})
	if err == nil {
		t.Errorf("expected err for unknown tool")
	}
	if res.ErrorKind != agentic.ToolErrorNotFound {
		t.Errorf("ErrorKind = %v, want ToolErrorNotFound", res.ErrorKind)
	}
}

// TestEmitDiagnosisExecutor_DiagnosisEntityIDShape double-checks that each
// call mints a distinct diagnosis entity and the entity ID follows the
// {org}.{platform}.ops.diagnosis.finding.{uuid} shape.
func TestEmitDiagnosisExecutor_DiagnosisEntityIDShape(t *testing.T) {
	e := NewEmitDiagnosisExecutor(
		&recordingPublisher{},
		types.PlatformMeta{Org: "c360", Platform: "deep-research"},
		slog.Default(),
	)

	call := func() agentic.ToolCall {
		return agentic.ToolCall{
			ID:     "c1",
			Name:   EmitDiagnosisToolName,
			LoopID: "loop-xyz",
			Arguments: map[string]any{
				"finding":        "test",
				"recommendation": "test",
				"confidence":     0.5,
				"evidence":       []any{"acme.test.agent.agentic-loop.execution.loop-abc"},
			},
		}
	}

	res1, _ := e.Execute(context.Background(), call())
	res2, _ := e.Execute(context.Background(), call())

	id1, _ := res1.Metadata["diagnosis_id"].(string)
	id2, _ := res2.Metadata["diagnosis_id"].(string)

	if id1 == "" || id2 == "" {
		t.Fatal("diagnosis_id must be non-empty")
	}
	if id1 == id2 {
		t.Errorf("two calls produced the same diagnosis entity ID — IDs must be unique")
	}

	wantPrefix := "c360.deep-research.ops.diagnosis.finding."
	if !strings.HasPrefix(id1, wantPrefix) {
		t.Errorf("diagnosis entity %q does not have prefix %q", id1, wantPrefix)
	}

	if !message.IsValidEntityID(id1) {
		t.Errorf("diagnosis entity ID %q failed IsValidEntityID", id1)
	}
}

// TestEmitDiagnosisExecutor_LoopEntityBacklink verifies the executed_by
// back-link points to the ops loop (not the diagnosis entity or anything
// else).
func TestEmitDiagnosisExecutor_LoopEntityBacklink(t *testing.T) {
	pub := &recordingPublisher{}
	e := NewEmitDiagnosisExecutor(
		pub,
		types.PlatformMeta{Org: "c360", Platform: "ops"},
		slog.Default(),
	)

	_, _ = e.Execute(context.Background(), agentic.ToolCall{
		ID:     "c1",
		Name:   EmitDiagnosisToolName,
		LoopID: "my-ops-loop",
		Arguments: map[string]any{
			"finding":        "test",
			"recommendation": "test",
			"confidence":     0.5,
			"evidence":       []any{"acme.test.agent.agentic-loop.execution.loop-abc"},
		},
	})

	wantLoopEntity := fmt.Sprintf("c360.ops.agent.agentic-loop.execution.my-ops-loop")
	for _, tr := range pub.triples {
		if tr.Predicate == "agent.action.executed_by" {
			if tr.Object != wantLoopEntity {
				t.Errorf("executed_by object = %q, want %q", tr.Object, wantLoopEntity)
			}
			return
		}
	}
	t.Errorf("no agent.action.executed_by triple emitted")
}

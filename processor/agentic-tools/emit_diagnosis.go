package agentictools

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"time"

	"github.com/google/uuid"

	"github.com/c360studio/semstreams/agentic"
	"github.com/c360studio/semstreams/message"
	"github.com/c360studio/semstreams/pkg/errs"
	"github.com/c360studio/semstreams/types"
	agvocab "github.com/c360studio/semstreams/vocabulary/agentic"
)

// EmitDiagnosisToolName is the name agents use to invoke the ops agent's
// diagnosis emission tool.
const EmitDiagnosisToolName = "emit_diagnosis"

// emitDiagnosisSource is the Source field on triples this tool publishes.
// Lets operators distinguish ops diagnosis triples from other emitters in
// the graph at a glance.
const emitDiagnosisSource = "ops-emit-diagnosis"

// minEmitDiagnosisEvidence is the minimum number of evidence entity IDs
// required per diagnosis call. An ops finding without evidence is unverifiable.
const minEmitDiagnosisEvidence = 1

// emitDiagnosisDefaultSeverity is the severity applied when the ops agent
// omits the field or supplies a value outside the valid enum.
const emitDiagnosisDefaultSeverity = "info"

// emitDiagnosisValidSeverities is the closed set of accepted severity values.
var emitDiagnosisValidSeverities = map[string]bool{
	"info":     true,
	"warn":     true,
	"critical": true,
}

// EmitDiagnosisExecutor is the ops agent's finding emission tool. Each call
// mints a new {org}.{platform}.ops.diagnosis.finding.{uuid} entity and
// publishes one triple per predicate plus an agent.action.executed_by
// back-link to the ops loop entity. StopLoop is false so the agent can emit
// multiple findings per loop before calling submit_work.
type EmitDiagnosisExecutor struct {
	publisher TriplePublisher
	platform  types.PlatformMeta
	logger    *slog.Logger
}

// NewEmitDiagnosisExecutor constructs the executor given a triple publisher,
// the platform identity used to build entity IDs, and a logger for
// instrumentation.
func NewEmitDiagnosisExecutor(publisher TriplePublisher, platform types.PlatformMeta, logger *slog.Logger) *EmitDiagnosisExecutor {
	return &EmitDiagnosisExecutor{
		publisher: publisher,
		platform:  platform,
		logger:    logger,
	}
}

// ListTools describes the emit_diagnosis tool schema. The severity enum is
// enforced during execution (not just in the schema) because small models
// sometimes emit free-text severity values; the executor clamps invalid
// values to "info" rather than rejecting the call outright — see Execute.
func (e *EmitDiagnosisExecutor) ListTools() []agentic.ToolDefinition {
	return []agentic.ToolDefinition{
		{
			Name:        EmitDiagnosisToolName,
			Description: "Emit a structured ops diagnosis finding to the knowledge graph. Call once per finding; you may call multiple times per loop before submit_work. Each call mints a new diagnosis entity with evidence-backed predicates so downstream rules can branch on severity and confidence without parsing prose.",
			Parameters: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"finding": map[string]any{
						"type":        "string",
						"description": "Short textual description of the finding. Treat as prose; be concise.",
					},
					"recommendation": map[string]any{
						"type":        "string",
						"description": "Proposed next step to address the finding.",
					},
					"confidence": map[string]any{
						"type":        "number",
						"minimum":     0,
						"maximum":     1,
						"description": "Your confidence in this finding, 0.0 (speculative) to 1.0 (certain).",
					},
					"evidence": map[string]any{
						"type":        "array",
						"items":       map[string]any{"type": "string"},
						"minItems":    minEmitDiagnosisEvidence,
						"description": "Entity IDs of loops, trajectories, or other graph entities that support this finding. At least one required.",
					},
					"observed_role": map[string]any{
						"type":        "string",
						"description": "Optional. The agent role this finding pertains to (e.g. \"researcher\", \"coordinator\"). Omit when the finding is not role-specific.",
					},
					"severity": map[string]any{
						"type":        "string",
						"enum":        []string{"info", "warn", "critical"},
						"description": "Optional. Urgency of the finding. Defaults to \"info\".",
					},
				},
				"required": []string{"finding", "recommendation", "confidence", "evidence"},
			},
		},
	}
}

// emitDiagnosisArgs is the parsed shape of the emit_diagnosis tool's Arguments.
type emitDiagnosisArgs struct {
	Finding        string   `json:"finding"`
	Recommendation string   `json:"recommendation"`
	Confidence     float64  `json:"confidence"`
	Evidence       []string `json:"evidence"`
	ObservedRole   string   `json:"observed_role,omitempty"`
	Severity       string   `json:"severity,omitempty"`
}

// emitDiagnosisResult is serialised into the tool result Content.
type emitDiagnosisResult struct {
	EntityID       string   `json:"entity_id"`
	Finding        string   `json:"finding"`
	Recommendation string   `json:"recommendation"`
	Confidence     float64  `json:"confidence"`
	Evidence       []string `json:"evidence"`
	ObservedRole   string   `json:"observed_role,omitempty"`
	Severity       string   `json:"severity"`
}

// Execute routes the tool call to emitDiagnosis; any other name is a routing
// bug.
func (e *EmitDiagnosisExecutor) Execute(ctx context.Context, call agentic.ToolCall) (agentic.ToolResult, error) {
	if call.Name != EmitDiagnosisToolName {
		return agentic.ToolResult{
			CallID:    call.ID,
			Error:     fmt.Sprintf("unknown tool: %s", call.Name),
			ErrorKind: agentic.ToolErrorNotFound,
		}, errs.WrapInvalid(fmt.Errorf("unknown tool: %s", call.Name), "EmitDiagnosisExecutor", "Execute", "route tool")
	}
	return e.emitDiagnosis(ctx, call)
}

func (e *EmitDiagnosisExecutor) emitDiagnosis(ctx context.Context, call agentic.ToolCall) (agentic.ToolResult, error) {
	args, err := parseEmitDiagnosisArgs(call.Arguments)
	if err != nil {
		return agentic.ToolResult{
			CallID:    call.ID,
			Error:     err.Error(),
			ErrorKind: agentic.ToolErrorInvalidArgs,
		}, nil
	}

	if call.LoopID == "" {
		return agentic.ToolResult{
			CallID:    call.ID,
			Error:     "emit_diagnosis invoked without a loop_id on the tool call; cannot build the executed_by back-link",
			ErrorKind: agentic.ToolErrorInternal,
		}, errs.WrapInvalid(fmt.Errorf("tool call missing loop_id"), "EmitDiagnosisExecutor", "emitDiagnosis", "resolve loop entity")
	}

	// Clamp severity: invalid or missing values become "info". This is a
	// deliberate policy choice — small models occasionally emit free-text
	// severity like "medium" or "low". Clamping to "info" lets the finding
	// land rather than bouncing, and downstream rules that need severity can
	// use the confidence field to distinguish urgency.
	if !emitDiagnosisValidSeverities[args.Severity] {
		args.Severity = emitDiagnosisDefaultSeverity
	}

	// Mint a new unique ID for this diagnosis entity. We use uuid v4 because
	// it's already established for loop IDs in this codebase (see
	// processor/agentic-loop/state.go). The plan referenced ULID but
	// oklog/ulid is not in go.mod; uuid is equivalent for uniqueness purposes.
	diagnosisID := uuid.New().String()
	diagnosisEntityID := agentic.OpsDiagnosisEntityID(e.platform.Org, e.platform.Platform, diagnosisID)
	loopEntityID := agentic.LoopExecutionEntityID(e.platform.Org, e.platform.Platform, call.LoopID)

	now := time.Now()

	// Build the triple set in deterministic order. Fail-fast on the first
	// publisher error — partial emission would leave an incomplete finding in
	// the graph with no way for the consumer to distinguish "all triples
	// landed" from "some triples landed". The ops agent sees the error in the
	// next iteration and can retry the full call.
	triples := buildEmitDiagnosisTriples(diagnosisEntityID, loopEntityID, args, now)

	for _, triple := range triples {
		if err := e.publisher.AddTriple(ctx, triple); err != nil {
			return agentic.ToolResult{
				CallID:    call.ID,
				Error:     fmt.Sprintf("publish %s triple: %v", triple.Predicate, err),
				ErrorKind: agentic.ToolErrorNetwork,
			}, errs.WrapTransient(err, "EmitDiagnosisExecutor", "emitDiagnosis", "publish triple")
		}
	}

	result := emitDiagnosisResult{
		EntityID:       diagnosisEntityID,
		Finding:        args.Finding,
		Recommendation: args.Recommendation,
		Confidence:     args.Confidence,
		Evidence:       args.Evidence,
		ObservedRole:   args.ObservedRole,
		Severity:       args.Severity,
	}
	payload, err := json.Marshal(result)
	if err != nil {
		return agentic.ToolResult{
			CallID:    call.ID,
			Error:     fmt.Sprintf("marshal result payload: %v", err),
			ErrorKind: agentic.ToolErrorInternal,
		}, errs.WrapInvalid(err, "EmitDiagnosisExecutor", "emitDiagnosis", "marshal payload")
	}

	return agentic.ToolResult{
		CallID:   call.ID,
		Content:  string(payload),
		StopLoop: false, // ops agents emit multiple findings per loop
		Metadata: map[string]any{
			"diagnosis_id": diagnosisEntityID,
		},
	}, nil
}

// buildEmitDiagnosisTriples assembles the full triple set for a single
// diagnosis emission in deterministic order:
//  1. finding
//  2. recommendation
//  3. confidence
//  4. evidence (one per entry)
//  5. observed_role (if set)
//  6. severity
//  7. agent.action.executed_by back-link to the ops loop
func buildEmitDiagnosisTriples(diagnosisEntityID, loopEntityID string, args emitDiagnosisArgs, now time.Time) []message.Triple {
	triples := make([]message.Triple, 0, 6+len(args.Evidence))

	base := func(pred, obj string) message.Triple {
		return message.Triple{
			Subject:    diagnosisEntityID,
			Predicate:  pred,
			Object:     obj,
			Source:     emitDiagnosisSource,
			Timestamp:  now,
			Confidence: args.Confidence,
		}
	}

	triples = append(triples, base(agvocab.OpsDiagnosisFinding, args.Finding))
	triples = append(triples, base(agvocab.OpsDiagnosisRecommendation, args.Recommendation))
	triples = append(triples, base(agvocab.OpsDiagnosisConfidence, fmt.Sprintf("%g", args.Confidence)))

	for _, ev := range args.Evidence {
		triples = append(triples, base(agvocab.OpsDiagnosisEvidence, ev))
	}

	if args.ObservedRole != "" {
		triples = append(triples, base(agvocab.OpsDiagnosisObservedRole, args.ObservedRole))
	}

	triples = append(triples, base(agvocab.OpsDiagnosisSeverity, args.Severity))

	// Back-link from the diagnosis entity to the ops loop that emitted it.
	triples = append(triples, message.Triple{
		Subject:    diagnosisEntityID,
		Predicate:  "agent.action.executed_by",
		Object:     loopEntityID,
		Source:     emitDiagnosisSource,
		Timestamp:  now,
		Confidence: args.Confidence,
	})

	return triples
}

// parseEmitDiagnosisArgs reads the untyped tool arguments into
// emitDiagnosisArgs and enforces required fields and bounds. Returns a
// descriptive error for each validation failure so the framework's retry
// policy can surface it to the model.
func parseEmitDiagnosisArgs(raw map[string]any) (emitDiagnosisArgs, error) {
	finding, ok := raw["finding"].(string)
	if !ok || finding == "" {
		return emitDiagnosisArgs{}, fmt.Errorf("finding is required and must be a non-empty string")
	}

	recommendation, ok := raw["recommendation"].(string)
	if !ok || recommendation == "" {
		return emitDiagnosisArgs{}, fmt.Errorf("recommendation is required and must be a non-empty string")
	}

	// JSON numbers unmarshalled into map[string]any arrive as float64.
	rawConf, present := raw["confidence"]
	if !present || rawConf == nil {
		return emitDiagnosisArgs{}, fmt.Errorf("confidence is required")
	}
	confidence, ok := rawConf.(float64)
	if !ok {
		return emitDiagnosisArgs{}, fmt.Errorf("confidence must be a number")
	}
	if confidence < 0 || confidence > 1 {
		return emitDiagnosisArgs{}, fmt.Errorf("confidence must be between 0.0 and 1.0, got %g", confidence)
	}

	rawEvidence, present := raw["evidence"]
	if !present || rawEvidence == nil {
		return emitDiagnosisArgs{}, fmt.Errorf("evidence is required")
	}
	evidenceSlice, ok := rawEvidence.([]any)
	if !ok {
		return emitDiagnosisArgs{}, fmt.Errorf("evidence must be an array of strings")
	}
	if len(evidenceSlice) < minEmitDiagnosisEvidence {
		return emitDiagnosisArgs{}, fmt.Errorf("evidence must contain at least %d entity ID, got 0", minEmitDiagnosisEvidence)
	}
	evidence := make([]string, 0, len(evidenceSlice))
	for i, v := range evidenceSlice {
		s, ok := v.(string)
		if !ok {
			return emitDiagnosisArgs{}, fmt.Errorf("evidence[%d] must be a string", i)
		}
		evidence = append(evidence, s)
	}

	args := emitDiagnosisArgs{
		Finding:        finding,
		Recommendation: recommendation,
		Confidence:     confidence,
		Evidence:       evidence,
	}

	if rawRole, present := raw["observed_role"]; present && rawRole != nil {
		role, ok := rawRole.(string)
		if !ok {
			return emitDiagnosisArgs{}, fmt.Errorf("observed_role must be a string")
		}
		args.ObservedRole = role
	}

	if rawSev, present := raw["severity"]; present && rawSev != nil {
		sev, ok := rawSev.(string)
		if !ok {
			return emitDiagnosisArgs{}, fmt.Errorf("severity must be a string")
		}
		args.Severity = sev
	}

	return args, nil
}

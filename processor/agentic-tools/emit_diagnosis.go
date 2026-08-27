package agentictools

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"math"

	"github.com/google/uuid"

	"github.com/c360studio/semstreams/agentic"
	"github.com/c360studio/semstreams/pkg/errs"
	"github.com/c360studio/semstreams/types"
)

// EmitDiagnosisToolName is the name agents use to invoke the ops agent's
// diagnosis emission tool.
const EmitDiagnosisToolName = "emit_diagnosis"

// emitDiagnosisDefaultSeverity is the severity applied when the ops agent
// omits the field or supplies a value outside the valid enum.
const emitDiagnosisDefaultSeverity = "info"

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
			Effect:      agentic.ToolEffectMutating,
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
						"minItems":    agentic.OpsDiagnosisMinEvidence,
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
	if !agentic.IsOpsDiagnosisSeverity(args.Severity) {
		args.Severity = emitDiagnosisDefaultSeverity
	}

	// Mint a new unique ID for this diagnosis entity. We use uuid v4 because
	// it's already established for loop IDs in this codebase (see
	// processor/agentic-loop/state.go). The plan referenced ULID but
	// oklog/ulid is not in go.mod; uuid is equivalent for uniqueness purposes.
	diagnosisID := uuid.New().String()
	diagnosisEntityID := agentic.OpsDiagnosisEntityID(e.platform.Org, e.platform.Platform, diagnosisID)
	loopEntityID, err := agentic.TryLoopExecutionEntityID(e.platform.Org, e.platform.Platform, call.LoopID)
	if err != nil {
		return agentic.ToolResult{
			CallID:    call.ID,
			Error:     fmt.Sprintf("construct loop entity ID: %v", err),
			ErrorKind: agentic.ToolErrorInternal,
		}, errs.WrapInvalid(err, "EmitDiagnosisExecutor", "emitDiagnosis", "construct loop entity ID")
	}

	// The registered finding entity is the one builder of its triples
	// (ADR-103). All triples share Subject=diagnosisEntityID (including the
	// agent.action.executed-by back-link FROM the diagnosis TO the loop), so
	// they belong to the one finding entity being born.
	//
	// gh#390: each call mints a NEW ops.diagnosis.finding.{uuid} entity, so this
	// is a BIRTH — the entity must be CREATED via entity.create carrying a
	// typed-origin envelope, not appended. Append is must-exist, so the old
	// append-before-birth path returned not-found and the finding never landed in
	// the graph (e2e:ops: 0/3 findings). entity.create is atomic
	// (all-or-nothing), preserving the no-partial-finding contract.
	finding := &agentic.OpsDiagnosisEntity{
		Org: e.platform.Org, Platform: e.platform.Platform, ID: diagnosisID,
		Finding: args.Finding, Recommendation: args.Recommendation, Confidence: args.Confidence,
		Evidence: args.Evidence, ObservedRole: args.ObservedRole, Severity: args.Severity,
		ExecutedBy: loopEntityID,
	}
	// The finding's contract is the entity's (ADR-103) — the same gate
	// BaseMessage.MarshalJSON applies to every publisher.
	if err := finding.Validate(); err != nil {
		return agentic.ToolResult{
			CallID:    call.ID,
			Error:     err.Error(),
			ErrorKind: agentic.ToolErrorInvalidArgs,
		}, nil
	}
	triples := finding.Triples()
	if err := e.publisher.Create(ctx, diagnosisEntityID, agentic.OpsDiagnosisMessageType(), triples); err != nil {
		return agentic.ToolResult{
			CallID:    call.ID,
			Error:     fmt.Sprintf("publish diagnosis triples: %v", err),
			ErrorKind: agentic.ToolErrorNetwork,
		}, errs.WrapTransient(err, "EmitDiagnosisExecutor", "emitDiagnosis", "birth diagnosis entity")
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

// parseEmitDiagnosisArgs reads the untyped tool arguments into
// emitDiagnosisArgs and enforces required fields and bounds. Returns a
// descriptive error for each validation failure so the framework's retry
// policy can surface it to the model.
func parseEmitDiagnosisArgs(raw map[string]any) (emitDiagnosisArgs, error) {
	finding, err := readString(raw, "finding")
	if err != nil {
		return emitDiagnosisArgs{}, err
	}
	recommendation, err := readString(raw, "recommendation")
	if err != nil {
		return emitDiagnosisArgs{}, err
	}
	// JSON numbers unmarshalled into map[string]any arrive as float64. An
	// absent confidence stays NaN so the entity contract reports it as out of
	// range rather than silently reading a 0.
	confidence := math.NaN()
	if rawConf, present := raw["confidence"]; present && rawConf != nil {
		value, ok := rawConf.(float64)
		if !ok {
			return emitDiagnosisArgs{}, fmt.Errorf("confidence must be a number")
		}
		confidence = value
	}
	evidence, err := readStringArray(raw, "evidence")
	if err != nil {
		return emitDiagnosisArgs{}, err
	}
	observedRole, err := readString(raw, "observed_role")
	if err != nil {
		return emitDiagnosisArgs{}, err
	}
	severity, err := readString(raw, "severity")
	if err != nil {
		return emitDiagnosisArgs{}, err
	}
	return emitDiagnosisArgs{
		Finding:        finding,
		Recommendation: recommendation,
		Confidence:     confidence,
		Evidence:       evidence,
		ObservedRole:   observedRole,
		Severity:       severity,
	}, nil
}

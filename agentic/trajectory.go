package agentic

import (
	"fmt"
	"time"
)

// TrajectoryStep represents a single step in an agentic trajectory
type TrajectoryStep struct {
	Timestamp     time.Time      `json:"timestamp"`
	StepType      string         `json:"step_type"`
	RequestID     string         `json:"request_id,omitempty"`
	Prompt        string         `json:"prompt,omitempty"`
	Response      string         `json:"response,omitempty"`
	TokensIn      int            `json:"tokens_in,omitempty"`
	TokensOut     int            `json:"tokens_out,omitempty"`
	ToolName      string         `json:"tool_name,omitempty"`
	ToolArguments map[string]any `json:"tool_arguments,omitempty"`
	ToolResult    string         `json:"tool_result,omitempty"`
	Duration      int64          `json:"duration"`              // milliseconds
	Messages      []ChatMessage  `json:"messages,omitempty"`    // Full request messages (detail=full)
	ToolCalls     []ToolCall     `json:"tool_calls,omitempty"`  // Assistant tool calls (detail=full)
	Model         string         `json:"model,omitempty"`       // Model used
	Provider      string         `json:"provider,omitempty"`    // LLM provider (anthropic, openai, etc.)
	Capability    string         `json:"capability,omitempty"`  // Role/purpose (coding, planning, reviewing, etc.)
	RetryCount    int            `json:"retry_count,omitempty"` // Number of retries before success
	Utilization   float64        `json:"utilization,omitempty"` // Context utilization at compaction trigger (0.0-1.0)
	// Tool outcome fields, populated for tool_call steps. Emitted as graph
	// triples (agent.step.tool_status / error_message / error_category) so
	// ops agents can query failure rates by tool and category.
	ToolStatus    string `json:"tool_status,omitempty"`    // "success" | "failed"
	ErrorMessage  string `json:"error_message,omitempty"`  // Raw error text; omitted on success
	ErrorCategory string `json:"error_category,omitempty"` // ToolErrorKind string form; omitted on success
	// URLsFetched lists external URLs a bash step fetched (curl/wget/httpie),
	// derived from the command string (gh#146). Lets citation/audit/governance
	// dashboards filter and count external reach independently of generic shell
	// activity. Empty/omitted when the step fetched no URL.
	URLsFetched []string `json:"url_fetched,omitempty"`
}

// Validate checks if the TrajectoryStep is valid
func (s TrajectoryStep) Validate() error {
	if s.StepType != "model_call" && s.StepType != "tool_call" && s.StepType != "context_compaction" {
		return fmt.Errorf("step_type must be one of: model_call, tool_call, context_compaction")
	}
	if s.Timestamp.IsZero() {
		return fmt.Errorf("timestamp required")
	}
	return nil
}

// Trajectory represents the complete execution path of an agentic loop
type Trajectory struct {
	LoopID         string           `json:"loop_id"`
	StartTime      time.Time        `json:"start_time"`
	EndTime        *time.Time       `json:"end_time,omitempty"`
	Steps          []TrajectoryStep `json:"steps"`
	Outcome        string           `json:"outcome,omitempty"`
	TotalTokensIn  int              `json:"total_tokens_in"`
	TotalTokensOut int              `json:"total_tokens_out"`
	Duration       int64            `json:"duration"` // milliseconds
}

// AddStep adds a step to the trajectory and updates totals.
// Compaction steps are excluded from token totals because their TokensIn/Out
// represent evicted/summarized tokens, not new LLM API consumption. Including
// them would double-count tokens already tallied by prior model_call steps.
func (t *Trajectory) AddStep(step TrajectoryStep) {
	t.Steps = append(t.Steps, step)
	if step.StepType != "context_compaction" {
		t.TotalTokensIn += step.TokensIn
		t.TotalTokensOut += step.TokensOut
	}
	t.Duration += step.Duration
}

// NewTrajectory creates a new Trajectory with initialized values
func NewTrajectory(loopID string) Trajectory {
	return Trajectory{
		LoopID:    loopID,
		StartTime: time.Now(),
		Steps:     []TrajectoryStep{},
	}
}

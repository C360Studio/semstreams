package agenticdispatch

import (
	"encoding/json"

	"github.com/c360studio/semstreams/agentic"
)

// Loop is the canonical wire contract for both the /loops and /activity endpoints.
// It is a flat superset: fields absent from a given source remain zero/empty.
// LoopInfo (in-memory tracker) and agentic.LoopEntity (KV entity) each project
// onto this type, so consumers see a single consistent shape regardless of
// whether the data originated from a live tracker record or a KV watch event.
type Loop struct {
	LoopID        string `json:"loop_id"`
	TaskID        string `json:"task_id,omitempty"`
	State         string `json:"state,omitempty"`
	Role          string `json:"role,omitempty"`
	Iterations    int    `json:"iterations,omitempty"`
	MaxIterations int    `json:"max_iterations,omitempty"`
	UserID        string `json:"user_id,omitempty"`
	ChannelType   string `json:"channel_type,omitempty"`
	ParentLoopID  string `json:"parent_loop_id,omitempty"`
	Outcome       string `json:"outcome,omitempty"`
	Result        string `json:"result,omitempty"`
	Error         string `json:"error,omitempty"`
	Prompt        string `json:"prompt,omitempty"`
	TokensIn      int    `json:"tokens_in,omitempty"`
	TokensOut     int    `json:"tokens_out,omitempty"`
	// PendingApproval is populated when the loop is in awaiting_approval state.
	PendingApproval *PendingApprovalInfo `json:"pending_approval,omitempty"`
}

// loopFromInfo projects the dispatch-owned in-memory tracker record onto Loop.
// ParentLoopID stays empty — the tracker does not record spawn relationships today.
func loopFromInfo(in *LoopInfo) Loop {
	return Loop{
		LoopID:          in.LoopID,
		TaskID:          in.TaskID,
		State:           in.State,
		Role:            in.Role,
		Iterations:      in.Iterations,
		MaxIterations:   in.MaxIterations,
		UserID:          in.UserID,
		ChannelType:     in.ChannelType,
		ParentLoopID:    "", // not tracked in LoopInfo today — scoped-out follow-up
		Outcome:         in.Outcome,
		Result:          in.Result,
		Error:           in.Error,
		Prompt:          "", // not on LoopInfo; populated only from completion events
		TokensIn:        0,  // not on LoopInfo
		TokensOut:       0,  // not on LoopInfo
		PendingApproval: in.PendingApproval,
	}
}

// loopFromEntity projects a framework-owned KV LoopEntity (key=<loopID>) onto Loop.
// Token and prompt fields are not stored on live entities — they appear only in
// completion events.
func loopFromEntity(e *agentic.LoopEntity) Loop {
	return Loop{
		LoopID:        e.ID,
		TaskID:        e.TaskID,
		State:         e.State.String(),
		Role:          e.Role,
		Iterations:    e.Iterations,
		MaxIterations: e.MaxIterations,
		UserID:        e.UserID,
		ChannelType:   e.ChannelType,
		ParentLoopID:  e.ParentLoopID,
		Outcome:       e.Outcome,
		Result:        e.Result,
		Error:         e.Error,
		Prompt:        "", // not present on live LoopEntity
		TokensIn:      0,  // not present on live LoopEntity
		TokensOut:     0,  // not present on live LoopEntity
	}
}

// completionWire is a minimal union struct that covers the common fields of
// LoopCompletedEvent, LoopFailedEvent, and LoopCancelledEvent.
// It normalises the "parent_loop" json tag (used by completion events) onto
// ParentLoopID so callers don't need to know about the divergence.
type completionWire struct {
	LoopID       string `json:"loop_id"`
	TaskID       string `json:"task_id"`
	Role         string `json:"role"`
	State        string `json:"state"` // may be absent from some completion events
	Outcome      string `json:"outcome"`
	Result       string `json:"result"`
	Error        string `json:"error"`
	Prompt       string `json:"prompt"`
	Iterations   int    `json:"iterations"`
	TokensIn     int    `json:"tokens_in"`
	TokensOut    int    `json:"tokens_out"`
	ParentLoopID string `json:"parent_loop"` // events.go uses "parent_loop"; normalised onto Loop.ParentLoopID
}

// loopFromCompletion projects a COMPLETE_<loopID> terminal event payload onto Loop.
// Returns ok=false only when the bytes cannot be unmarshalled or yield no loop_id.
func loopFromCompletion(raw []byte) (Loop, bool) {
	var w completionWire
	if err := json.Unmarshal(raw, &w); err != nil {
		return Loop{}, false
	}
	if w.LoopID == "" {
		return Loop{}, false
	}
	return Loop{
		LoopID:       w.LoopID,
		TaskID:       w.TaskID,
		State:        w.State,
		Role:         w.Role,
		Iterations:   w.Iterations,
		Outcome:      w.Outcome,
		Result:       w.Result,
		Error:        w.Error,
		Prompt:       w.Prompt,
		TokensIn:     w.TokensIn,
		TokensOut:    w.TokensOut,
		ParentLoopID: w.ParentLoopID,
	}, true
}

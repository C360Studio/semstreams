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
	// RunID is the bare run loop-id this loop belongs to (ADR-053). RunEntityID
	// is the full 6-part chain.execution entity ID — the form a consumer feeds to
	// graph getEntity() to read chain-level triples, WITHOUT re-deriving it
	// client-side. Both empty for loops not in a run.
	RunID       string `json:"run_id,omitempty"`
	RunEntityID string `json:"run_entity_id,omitempty"`
	Outcome     string `json:"outcome,omitempty"`
	Result      string `json:"result,omitempty"`
	// ResultTruncated marks Result as a bounded PREVIEW of an offloaded
	// result body (payload-size-chokepoints D4): the completion value
	// carried a storage_ref instead of inline text. The full content is
	// retrievable via the read_loop_result tool; ResultSize is the original
	// byte count. Both zero for inline results.
	ResultTruncated bool   `json:"result_truncated,omitempty"`
	ResultSize      int    `json:"result_size,omitempty"`
	Error           string `json:"error,omitempty"`
	Prompt          string `json:"prompt,omitempty"`
	TokensIn        int    `json:"tokens_in,omitempty"`
	TokensOut       int    `json:"tokens_out,omitempty"`
	// PendingApproval is populated when the loop is in awaiting_approval state.
	PendingApproval *PendingApprovalInfo `json:"pending_approval,omitempty"`
}

// loopFromInfo projects the dispatch-owned in-memory tracker record onto Loop.
// ParentLoopID stays empty — the tracker does not record spawn relationships today.
func loopFromInfo(in *LoopInfo) Loop {
	return Loop{
		LoopID:        in.LoopID,
		TaskID:        in.TaskID,
		State:         in.State,
		Role:          in.Role,
		Iterations:    in.Iterations,
		MaxIterations: in.MaxIterations,
		UserID:        in.UserID,
		ChannelType:   in.ChannelType,
		ParentLoopID:  "", // not tracked in LoopInfo today — scoped-out follow-up
		RunID:         "", // not tracked in LoopInfo today — /activity (loopFromEntity) carries it
		RunEntityID:   "", // ditto
		// entity-id-audit:classify intentional-sentinel "" line=61 column=18 surface=go-field:Loop.RunEntityID entity_id_invalid:empty optional projection unavailable from LoopInfo
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
// completion events. org/platform are the dispatch component's platform identity,
// needed to derive RunEntityID (the 6-part chain.execution ID) since LoopEntity
// stores only the bare RunID.
func loopFromEntity(e *agentic.LoopEntity, org, platform string) Loop {
	var runEntityID string
	if e.RunID != "" {
		// Best-effort: a malformed RunID/platform leaves RunEntityID empty rather
		// than failing the projection (the bare RunID still ships).
		if id, err := agentic.TryChainExecutionEntityID(org, platform, e.RunID); err == nil {
			runEntityID = id
		}
	}
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
		RunID:         e.RunID,
		RunEntityID:   runEntityID,
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
	ParentLoopID string `json:"parent_loop"`   // events.go uses "parent_loop"; normalised onto Loop.ParentLoopID
	RunID        string `json:"run_id"`        // ADR-053: present on terminal events
	RunEntityID  string `json:"run_entity_id"` // ADR-053: 6-part, present on terminal events
	// Offload triplet (payload-size-chokepoints D4): present when the
	// result body was offloaded to the content ObjectStore. HasRef is
	// detected via storage_ref being any non-null JSON value.
	Preview    string          `json:"preview"`
	Size       int             `json:"size"`
	StorageRef json.RawMessage `json:"storage_ref"`
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
	loop := Loop{
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
		RunID:        w.RunID,
		RunEntityID:  w.RunEntityID,
	}
	// Offloaded result (payload-size-chokepoints D4): the completion value
	// carries {storage_ref, preview, size} instead of inline text. Surface
	// the preview WITH the truncation marker — never a bare preview a
	// consumer could mistake for the whole result. Full content is the
	// read_loop_result tool's job.
	if loop.Result == "" && len(w.StorageRef) > 0 && string(w.StorageRef) != "null" {
		loop.Result = w.Preview
		loop.ResultTruncated = true
		loop.ResultSize = w.Size
	}
	return loop, true
}

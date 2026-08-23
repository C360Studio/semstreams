package agentic

import "time"

// Rule-readable projections for the framework-owned agentic payloads
// (message.RuleReadable).
//
// These payloads are FRAMEWORK-owned. An adopter who wants a rule on a tool
// call or a loop outcome cannot add a method to them — they would have to file
// a framework PR and wait for a release — so every one of the registry's
// agentic payloads declares its projection here rather than a convenient
// subset. They live in one file, not beside their structs, because the
// structural-vs-content call is a single policy applied fifteen times, and
// keeping the calls adjacent is what lets a reader audit them as a set.
//
// The projection convention, so an adopter can predict it without reading
// every method:
//
//   - Field names and value shapes MIRROR THE JSON WIRE. `loop_id`, not
//     `LoopID`. A time is its RFC3339 nano string, exactly as the payload
//     marshals. A field tagged `omitempty` is omitted when zero, so "absent"
//     in a rule means the same thing it means on the wire; a field without
//     that tag is always present, empty value and all.
//     ONE EXCEPTION, in putTime: a zero time is omitted even when the wire
//     renders it, because `"0001-01-01T00:00:00Z"` reaching a rule condition
//     as if it were an observation is worse than the field being absent.
//   - STRUCTURAL FACTS are exposed: identifiers, roles, outcomes, decisions,
//     classifications, counts, routing addresses, timestamps.
//   - AUTHORED CONTENT IS WITHHELD: prompts, user message text, model output,
//     tool-result bodies, free-form error and reason prose, compaction
//     summaries. ADR-036, and the same discipline the graph plane enforces
//     with rule-opaque predicates. A rule cannot judge unstructured text; a
//     coordinator can, and it emits a structured result a later rule matches
//     on deterministically.
//   - OPEN CALLER-POPULATED MAPS ARE WITHHELD — `Metadata`, tool `Arguments`,
//     `ModifiedArguments`, `UserSignal.Payload`. The framework does not
//     control what a producer or a model puts in them, so admitting them
//     would smuggle the reflective default back in: fields reaching rules
//     that no payload author ever declared. Withholding is the fail-closed
//     answer for a surface whose contents cannot be classified in advance.
//
// WHEN YOU ADD A FIELD to one of these payloads, decide here whether a rule
// may match on it. A field that is absent from a projection and unexplained
// reads as an oversight; the omission comments below exist so it does not.

// putString adds key when value is non-empty, mirroring `omitempty`.
func putString(fields map[string]any, key, value string) {
	if value != "" {
		fields[key] = value
	}
}

// putTime adds key as the payload's own wire representation (RFC3339 with
// nanoseconds, what encoding/json emits for time.Time) when value is set.
// A zero time is omitted rather than rendered as year 1.
func putTime(fields map[string]any, key string, value time.Time) {
	if !value.IsZero() {
		fields[key] = value.Format(time.RFC3339Nano)
	}
}

// RuleFields implements message.RuleReadable.
//
// Withheld: Metadata (open caller-populated map).
func (e *LoopCreatedEvent) RuleFields() map[string]any {
	fields := map[string]any{
		"loop_id":        e.LoopID,
		"task_id":        e.TaskID,
		"role":           e.Role,
		"model":          e.Model,
		"max_iterations": e.MaxIterations,
	}
	putString(fields, "workflow_slug", e.WorkflowSlug)
	putString(fields, "workflow_step", e.WorkflowStep)
	putString(fields, "context_request_id", e.ContextRequestID)
	putString(fields, "run_id", e.RunID)
	putString(fields, "run_entity_id", e.RunEntityID)
	putTime(fields, "created_at", e.CreatedAt)
	return fields
}

// RuleFields implements message.RuleReadable.
//
// `role` and `outcome` are the two fields the shipped architect→editor handoff
// conditions on (configs/rules/agentic-workflow/architect-editor.json); before
// this projection existed that rule could never fire.
//
// Withheld: Result (MODEL OUTPUT — the completion body the agent authored),
// Prompt (the user's task text), Metadata (open caller-populated map). A rule
// branching on what the agent actually said is a quality judgement over
// unstructured text; that belongs to a coordinator, whose terminal tool emits
// a structured triple a later rule can match on. Downstream consumers retrieve
// the body on demand (read_loop_result), which is why omitting it costs
// nothing but the temptation.
func (e *LoopCompletedEvent) RuleFields() map[string]any {
	fields := map[string]any{
		"loop_id":    e.LoopID,
		"task_id":    e.TaskID,
		"outcome":    e.Outcome,
		"role":       e.Role,
		"model":      e.Model,
		"iterations": e.Iterations,
		"tokens_in":  e.TokensIn,
		"tokens_out": e.TokensOut,
	}
	putString(fields, "parent_loop", e.ParentLoopID)
	putString(fields, "workflow_slug", e.WorkflowSlug)
	putString(fields, "workflow_step", e.WorkflowStep)
	putString(fields, "channel_type", e.ChannelType)
	putString(fields, "channel_id", e.ChannelID)
	putString(fields, "user_id", e.UserID)
	putString(fields, "run_id", e.RunID)
	putString(fields, "run_entity_id", e.RunEntityID)
	putTime(fields, "completed_at", e.CompletedAt)
	return fields
}

// RuleFields implements message.RuleReadable.
//
// `reason` IS exposed and `error` is not, and the distinction is the whole
// policy in one payload: Reason is a closed classification the framework
// assigns ("model_error", "length_truncated", "timeout"), which is exactly
// what a rule can branch on; Error is the underlying free-form string from a
// provider, a tool, or a transport, which routinely quotes model or user text
// back at us. A rule that wants to know WHY reads reason.
//
// Withheld: Error, Prompt (the user's task text), Metadata (open map).
func (e *LoopFailedEvent) RuleFields() map[string]any {
	fields := map[string]any{
		"loop_id":    e.LoopID,
		"task_id":    e.TaskID,
		"outcome":    e.Outcome,
		"reason":     e.Reason,
		"role":       e.Role,
		"model":      e.Model,
		"iterations": e.Iterations,
		"tokens_in":  e.TokensIn,
		"tokens_out": e.TokensOut,
	}
	putString(fields, "parent_loop", e.ParentLoopID)
	putString(fields, "workflow_slug", e.WorkflowSlug)
	putString(fields, "workflow_step", e.WorkflowStep)
	putString(fields, "channel_type", e.ChannelType)
	putString(fields, "channel_id", e.ChannelID)
	putString(fields, "user_id", e.UserID)
	putString(fields, "run_id", e.RunID)
	putString(fields, "run_entity_id", e.RunEntityID)
	putTime(fields, "failed_at", e.FailedAt)
	return fields
}

// RuleFields implements message.RuleReadable.
//
// `cancelled_by` is an actor identity, not authored content, so it is exposed:
// a rule distinguishing an operator cancellation from a supervisor one is a
// structural decision.
//
// Withheld: Metadata (open caller-populated map).
func (e *LoopCancelledEvent) RuleFields() map[string]any {
	fields := map[string]any{
		"loop_id":      e.LoopID,
		"task_id":      e.TaskID,
		"outcome":      e.Outcome,
		"cancelled_by": e.CancelledBy,
	}
	putString(fields, "parent_loop", e.ParentLoopID)
	putString(fields, "workflow_slug", e.WorkflowSlug)
	putString(fields, "workflow_step", e.WorkflowStep)
	putString(fields, "run_id", e.RunID)
	putString(fields, "run_entity_id", e.RunEntityID)
	putTime(fields, "cancelled_at", e.CancelledAt)
	return fields
}

// RuleFields implements message.RuleReadable.
//
// Withheld: Summary — the compaction summary is LLM-authored prose about the
// conversation it replaced, which is the clearest case of content there is.
// `utilization` and `tokens_saved` are the measurements a rule reacts to.
func (e *ContextEvent) RuleFields() map[string]any {
	fields := map[string]any{
		"type":      e.Type,
		"loop_id":   e.LoopID,
		"iteration": e.Iteration,
	}
	putString(fields, "user_id", e.UserID)
	if e.Utilization != 0 {
		fields["utilization"] = e.Utilization
	}
	if e.TokensSaved != 0 {
		fields["tokens_saved"] = e.TokensSaved
	}
	return fields
}

// RuleFields implements message.RuleReadable.
//
// Withheld: Prompt (the task text itself), Context (the assembled context
// body), Metadata (open caller-populated map, and the carrier for
// MetadataKeyRelatedLoops and friends — framework keys a rule reads through
// $entity.triple.agent.lineage.* instead).
//
// Also withheld, deliberately and for a different reason: Tools, ToolChoice
// and ResponseFormat. These are structural, not content, but they are nested
// configuration objects with no scalar a rule condition can compare — a rule
// wanting "was tool X offered" needs a shape this projection does not have.
// Widen when a real consumer asks, not before.
func (t *TaskMessage) RuleFields() map[string]any {
	fields := map[string]any{
		"task_id": t.TaskID,
		"role":    t.Role,
		"model":   t.Model,
	}
	putString(fields, "loop_id", t.LoopID)
	putString(fields, "workflow_slug", t.WorkflowSlug)
	putString(fields, "workflow_step", t.WorkflowStep)
	putString(fields, "channel_type", t.ChannelType)
	putString(fields, "channel_id", t.ChannelID)
	putString(fields, "user_id", t.UserID)
	putString(fields, "parent_loop_id", t.ParentLoopID)
	putString(fields, "run_id", t.RunID)
	putString(fields, "in_reply_to", t.InReplyTo)
	putString(fields, "context_request_id", t.ContextRequestID)
	putString(fields, "timeout", t.Timeout)
	if t.Depth != 0 {
		fields["depth"] = t.Depth
	}
	if t.MaxDepth != 0 {
		fields["max_depth"] = t.MaxDepth
	}
	if t.MaxIterations != nil {
		fields["max_iterations"] = *t.MaxIterations
	}
	return fields
}

// RuleFields implements message.RuleReadable.
//
// Withheld: Payload — signal-specific data typed `any`, which carries a
// rejection reason or whatever else the sending channel chose. Unclassifiable
// by construction, so it fails closed. `type` is the closed signal vocabulary
// (cancel/pause/resume/approve/reject/feedback/retry) and is what a rule
// branches on.
func (s *UserSignal) RuleFields() map[string]any {
	fields := map[string]any{
		"signal_id":    s.SignalID,
		"type":         s.Type,
		"loop_id":      s.LoopID,
		"user_id":      s.UserID,
		"channel_type": s.ChannelType,
		"channel_id":   s.ChannelID,
	}
	putTime(fields, "timestamp", s.Timestamp)
	return fields
}

// RuleFields implements message.RuleReadable.
//
// Withheld: Arguments — the tool arguments are MODEL-AUTHORED, the single
// most content-shaped field in the agentic set (a bash command, a file body,
// a search query). Reason is withheld with them: it embeds the executor's
// free-text rejection message. `tool_name` is the structural fact an approval
// routing rule needs.
func (e *ApprovalPendingEvent) RuleFields() map[string]any {
	fields := map[string]any{
		"loop_id":   e.LoopID,
		"call_id":   e.CallID,
		"tool_name": e.ToolName,
	}
	putString(fields, "trace_id", e.TraceID)
	if e.Timeout != 0 {
		// Nanoseconds, matching the wire encoding of time.Duration.
		fields["timeout"] = int64(e.Timeout)
	}
	putTime(fields, "requested_at", e.RequestedAt)
	return fields
}

// RuleFields implements message.RuleReadable.
//
// `decision` is a closed enum and `approved_by` an actor identity — both
// structural, and together they are what an audit or escalation rule matches.
//
// Withheld: ModifiedArguments (open, human- or model-authored map) and Reason
// (human free text).
func (r *ApprovalResponse) RuleFields() map[string]any {
	fields := map[string]any{
		"loop_id":  r.LoopID,
		"call_id":  r.CallID,
		"decision": r.Decision,
	}
	putString(fields, "approved_by", r.ApprovedBy)
	putTime(fields, "decided_at", r.DecidedAt)
	return fields
}

// RuleFields implements message.RuleReadable.
//
// Call identity and tool name, per the governance case this projection serves
// (ADR-039: a rule gating a proposed tool call needs loop_id, call_id and the
// tool name to build its verdict).
//
// Withheld: Arguments (MODEL-AUTHORED content — see ApprovalPendingEvent) and
// Metadata (open caller-populated map; framework keys such as
// MetadataKeyRunID reach rules as loop-entity triples, not through here).
func (t *ToolCall) RuleFields() map[string]any {
	fields := map[string]any{
		"id":   t.ID,
		"name": t.Name,
	}
	putString(fields, "loop_id", t.LoopID)
	putString(fields, "trace_id", t.TraceID)
	putString(fields, "approved_by", t.ApprovedBy)
	return fields
}

// RuleFields implements message.RuleReadable.
//
// Call identity, tool name and OUTCOME. The outcome is `error_kind`, derived
// through ToolResult.EffectiveErrorKind rather than copied: a result carrying
// an Error but no ErrorKind is still a failure, classified as unknown. So the
// presence of `error_kind` means "this call failed" and its value is the
// framework's own classification — a rule never has to string-match an error
// message to learn that much.
//
// This is the one deliberate break from the mirror-the-wire convention on a
// non-time field: the wire omits `error_kind` for an unclassified failure and
// the projection does not. Deriving it is the point; a rule that had to spell
// `error_kind != "" OR error != ""` would be reading the content field the
// projection withholds.
//
// Withheld: Content (THE RESULT BODY — arbitrary size, arbitrary origin, and
// the field the rules-carry-references discipline exists for), Error (free-form
// prose; error_kind is its structural counterpart), Metadata (open map,
// including the pagination keys, which are the loop's business and not a
// rule's).
func (t *ToolResult) RuleFields() map[string]any {
	fields := map[string]any{
		"call_id": t.CallID,
	}
	putString(fields, "name", t.Name)
	putString(fields, "loop_id", t.LoopID)
	putString(fields, "trace_id", t.TraceID)
	if kind := t.EffectiveErrorKind(); kind != "" {
		fields["error_kind"] = string(kind)
	}
	putString(fields, "result_hint", string(t.ResultHint))
	if t.StopLoop {
		fields["stop_loop"] = true
	}
	return fields
}

// RuleFields implements message.RuleReadable.
//
// Routing and threading only. This is the payload the content rule was written
// for: Content is what the human typed, and it does not reach a rule. Nor do
// Attachments, which are files by another name.
//
// Withheld: Content, Attachments, Metadata (channel-specific caller map).
func (m *UserMessage) RuleFields() map[string]any {
	fields := map[string]any{
		"message_id":   m.MessageID,
		"channel_type": m.ChannelType,
		"channel_id":   m.ChannelID,
		"user_id":      m.UserID,
	}
	putString(fields, "reply_to", m.ReplyTo)
	putString(fields, "thread_id", m.ThreadID)
	putString(fields, "context_request_id", m.ContextRequestID)
	putString(fields, "run_id", m.RunID)
	putString(fields, "in_reply_to", m.InReplyTo)
	putTime(fields, "timestamp", m.Timestamp)
	return fields
}

// RuleFields implements message.RuleReadable.
//
// `type` is the closed response vocabulary (text/status/result/error/prompt/
// stream) — the structural fact that says WHAT KIND of response this is
// without saying what it says.
//
// Withheld: Content (the response text), Blocks (content by another name), and
// Actions — Actions look structural but carry human-authored Labels, and a
// rule matching on a button's caption is matching on content.
func (r *UserResponse) RuleFields() map[string]any {
	fields := map[string]any{
		"response_id":  r.ResponseID,
		"channel_type": r.ChannelType,
		"channel_id":   r.ChannelID,
		"user_id":      r.UserID,
		"type":         r.Type,
	}
	putString(fields, "in_reply_to", r.InReplyTo)
	putString(fields, "thread_id", r.ThreadID)
	putTime(fields, "timestamp", r.Timestamp)
	return fields
}

// RuleFields implements message.RuleReadable.
//
// Withheld: Messages — THE PROMPT, in full, including system prompt, user text
// and the whole conversation so far. Nothing in the agentic set is more
// clearly content.
//
// Also withheld as nested configuration with no scalar rule shape, matching
// TaskMessage: Tools, ToolChoice, ResponseFormat.
func (r *AgentRequest) RuleFields() map[string]any {
	fields := map[string]any{
		"request_id": r.RequestID,
		"loop_id":    r.LoopID,
		"role":       r.Role,
		"model":      r.Model,
	}
	putString(fields, "timeout", r.Timeout)
	if r.MaxTokens != 0 {
		fields["max_tokens"] = r.MaxTokens
	}
	if r.Temperature != 0 {
		fields["temperature"] = r.Temperature
	}
	return fields
}

// RuleFields implements message.RuleReadable.
//
// `status` and `finish_reason` are the structural outcome: whether the model
// completed, asked for a tool, errored, or ran out of length. Token usage is
// exposed nested, as it is on the wire, so `$message.token_usage.prompt_tokens`
// resolves — a budget rule needs the numbers, not the text they paid for.
//
// Withheld: Message — MODEL OUTPUT, the content this whole payload exists to
// deliver — and Error, which is free-form provider prose (`status` is its
// structural counterpart).
func (r *AgentResponse) RuleFields() map[string]any {
	fields := map[string]any{
		"request_id": r.RequestID,
		"status":     r.Status,
		"token_usage": map[string]any{
			"prompt_tokens":     r.TokenUsage.PromptTokens,
			"completion_tokens": r.TokenUsage.CompletionTokens,
		},
	}
	putString(fields, "finish_reason", r.FinishReason)
	if r.RetryCount != 0 {
		fields["retry_count"] = r.RetryCount
	}
	return fields
}

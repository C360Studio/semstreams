// Package rule - Action execution for ECA rules
package rule

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"log/slog"

	"github.com/c360studio/semstreams/agentic"
	"github.com/c360studio/semstreams/component"
	"github.com/c360studio/semstreams/message"
	"github.com/c360studio/semstreams/processor/rule/expression"
)

// Action type constants define the supported action types for rule execution.
const (
	// ActionTypePublish publishes a message to a NATS subject
	ActionTypePublish = "publish"
	// ActionTypeAddTriple creates a relationship triple in the graph
	ActionTypeAddTriple = "add_triple"
	// ActionTypeRemoveTriple removes a relationship triple from the graph
	ActionTypeRemoveTriple = "remove_triple"
	// ActionTypeUpdateTriple updates metadata on an existing triple
	ActionTypeUpdateTriple = "update_triple"
	// ActionTypePublishAgent triggers an agentic loop by publishing a TaskMessage
	ActionTypePublishAgent = "publish_agent"
	// ActionTypeTriggerWorkflow triggers a reactive workflow by publishing to workflow.trigger.<workflow_id>
	ActionTypeTriggerWorkflow = "trigger_workflow"
	// ActionTypePublishBoidSignal publishes a Boid steering signal for agent coordination
	ActionTypePublishBoidSignal = "publish_boid_signal"
	// ActionTypeUpdateKV writes JSON to a named KV bucket with optional CAS merge
	ActionTypeUpdateKV = "update_kv"
	// ActionTypeDeny issues a deny verdict that short-circuits subsequent actions
	// and surfaces as *DenyVerdict to the caller. The deny is always the terminal
	// outcome: no later action in the same evaluation cycle runs once a deny fires.
	ActionTypeDeny = "deny"
)

// PredicateRuleDeny is the audit-triple predicate written by deny actions.
// Downstream rules that gate on denials should match against this constant
// so they stay in sync with any future rename.
const PredicateRuleDeny = "rule.deny"

// Action represents an action to execute when a rule fires.
// Actions are triggered by state transitions (OnEnter, OnExit) or
// while a condition remains true (WhileTrue).
type Action struct {
	// Type specifies the action type (publish, add_triple, remove_triple, update_triple, publish_agent)
	Type string `json:"type"`

	// Subject is the NATS subject for publish actions
	Subject string `json:"subject,omitempty"`

	// Predicate is the relationship type for triple actions
	Predicate string `json:"predicate,omitempty"`

	// Object is the target entity or value for triple actions
	Object string `json:"object,omitempty"`

	// TTL specifies optional expiration time for triples (e.g., "5m", "1h")
	TTL string `json:"ttl,omitempty"`

	// Properties contains additional metadata for the action
	Properties map[string]any `json:"properties,omitempty"`

	// Role is the agent role for publish_agent actions (e.g., "general", "architect", "editor")
	Role string `json:"role,omitempty"`

	// Model is the model endpoint name for publish_agent actions
	Model string `json:"model,omitempty"`

	// Prompt is the task prompt template for publish_agent actions
	// Supports variable substitution: $entity.id, $related.id
	Prompt string `json:"prompt,omitempty"`

	// Tools is the per-spawned-agent tool allowlist for publish_agent actions.
	// Tool names are resolved to agentic.ToolDefinition against the global
	// tool registry at dispatch time; unknown names are logged and dropped.
	// Empty/nil leaves TaskMessage.Tools unset, which makes the spawned loop
	// fall back to global tool discovery (existing behaviour).
	//
	// This is the product-layer hook for scoping which tools a given role
	// can see. Putting it on the rule — not in agentic-tools Config — keeps
	// role→tools decisions in the workflow config that already owns the
	// role name, model, and prompt for the spawned agent.
	Tools []string `json:"tools,omitempty"`

	// ActionAllowlist is the closed set of action values the spawned
	// loop's `decide` tool will accept. When non-nil, executePublishAgent
	// stamps it onto the TaskMessage's Metadata under
	// MetadataKeyDecideActionAllowlist; the agentic-loop propagates that
	// onto each ToolCall's Metadata; the decide tool's executor
	// validates the action argument against this list and returns
	// ToolErrorInvalidArgs (with the valid set in the message) if the
	// model picks a name outside it.
	//
	// Empty/nil disables the gate (back-compat: action stays free-form).
	// Belt-and-suspenders for persona prose: the persona enumerates the
	// vocabulary in the LLM's system prompt; this field enforces it
	// structurally on the wire. Putting the gate on the rule (rather
	// than introducing a wrapper tool) keeps role→action decisions in
	// the workflow config that already owns role/model/prompt/tools.
	ActionAllowlist []string `json:"action_allowlist,omitempty"`

	// WorkflowID is the workflow identifier for trigger_workflow actions
	WorkflowID string `json:"workflow_id,omitempty"`

	// ContextData provides additional context passed to the workflow
	ContextData map[string]any `json:"context_data,omitempty"`

	// BoidSignalType specifies the type of boid signal: separation, cohesion, or alignment
	BoidSignalType string `json:"boid_signal_type,omitempty"`

	// BoidStrength specifies the steering strength for boid signals (0.0-1.0)
	BoidStrength float64 `json:"boid_strength,omitempty"`

	// WorkflowSlug identifies the workflow for publish_agent actions (e.g., "github-issue-to-pr")
	WorkflowSlug string `json:"workflow_slug,omitempty"`

	// WorkflowStep identifies the step within the workflow (e.g., "qualify", "develop", "review")
	WorkflowStep string `json:"workflow_step,omitempty"`

	// When is an optional guard clause for conditional action execution.
	// If present, all conditions must match (AND logic) against the entity's current
	// state and $state.* fields for the action to execute. Actions without When always execute.
	When []expression.ConditionExpression `json:"when,omitempty"`

	// Bucket is the KV bucket name for update_kv actions (e.g., "PLAN_STATES").
	// Supports variable substitution.
	Bucket string `json:"bucket,omitempty"`

	// Key is the KV key for update_kv actions. Supports variable substitution.
	Key string `json:"key,omitempty"`

	// Payload is the data to write for update_kv actions.
	// Supports variable substitution in string values (including nested maps).
	Payload map[string]any `json:"payload,omitempty"`

	// Merge controls write semantics for update_kv:
	// true = CAS read-modify-write (merge payload into existing document)
	// false = overwrite entire document (last writer wins)
	Merge bool `json:"merge,omitempty"`

	// Reason is the human-readable denial message for deny actions.
	// It travels with the *DenyVerdict error so callers can record it
	// without a side-channel lookup. Variable substitution is applied
	// (e.g. "$caller.id denied: insufficient role $caller.role").
	Reason string `json:"reason,omitempty"`
}

// ParseTTL parses the TTL string into a duration.
// Returns 0 duration if TTL is empty (no expiration).
// Returns an error if the TTL format is invalid or negative.
func (a Action) ParseTTL() (time.Duration, error) {
	if a.TTL == "" {
		return 0, nil
	}

	duration, err := time.ParseDuration(a.TTL)
	if err != nil {
		return 0, fmt.Errorf("invalid TTL format: %w", err)
	}

	// Reject negative durations
	if duration < 0 {
		return 0, errors.New("TTL cannot be negative")
	}

	return duration, nil
}

// TripleMutator handles triple mutations via NATS request/response.
// The returned uint64 is the KV revision after the write, used for per-rule
// feedback loop prevention. The ruleID identifies the originating rule so
// the revision can be scoped to that rule only — pass an empty ruleID for
// ad-hoc mutations that should not be tracked.
type TripleMutator interface {
	// AddTriple adds a triple via NATS request/response and returns the KV revision.
	AddTriple(ctx context.Context, ruleID string, triple message.Triple) (uint64, error)
	// RemoveTriple removes a triple via NATS request/response and returns the KV revision.
	RemoveTriple(ctx context.Context, ruleID, subject, predicate string) (uint64, error)
}

// Publisher handles publishing messages to NATS subjects.
// It abstracts the decision between core NATS and JetStream publishing.
type Publisher interface {
	// Publish sends a message to a NATS subject.
	// The implementation determines whether to use core NATS or JetStream
	// based on port configuration.
	Publish(ctx context.Context, subject string, data []byte) error
}

// ActionExecutor executes actions for rules.
// It handles triple mutations, NATS publishing, KV writes, and other action types.
type ActionExecutor struct {
	logger        *slog.Logger
	tripleMutator TripleMutator                // Optional: if nil, triple mutations are logged but not persisted
	publisher     Publisher                    // Optional: if nil, publish actions are logged but not sent
	kvWriter      KVWriter                     // Optional: if nil, update_kv actions are logged but not executed
	toolRegistry  component.ToolRegistryReader // Optional: if nil, publish_agent default_tools resolution returns empty
}

// SetToolRegistry installs the shared tool registry used by
// resolveToolNames during publish_agent action execution. nil-valued
// arg disables tool name resolution (tools list passed to the agent
// is left empty). Set explicitly after construction by the rule
// processor when it has access to deps.ToolRegistry.
func (e *ActionExecutor) SetToolRegistry(r component.ToolRegistryReader) {
	e.toolRegistry = r
}

// NewActionExecutor creates a new ActionExecutor with the given logger.
// If logger is nil, uses the default logger.
func NewActionExecutor(logger *slog.Logger) *ActionExecutor {
	if logger == nil {
		logger = slog.Default()
	}
	return &ActionExecutor{
		logger: logger,
	}
}

// NewActionExecutorWithMutator creates a new ActionExecutor with triple mutation support.
// The mutator enables actual persistence of triple operations via NATS request/response.
func NewActionExecutorWithMutator(logger *slog.Logger, mutator TripleMutator) *ActionExecutor {
	if logger == nil {
		logger = slog.Default()
	}
	return &ActionExecutor{
		logger:        logger,
		tripleMutator: mutator,
	}
}

// NewActionExecutorFull creates a new ActionExecutor with full functionality.
// The mutator enables triple persistence, and the publisher enables NATS publishing.
func NewActionExecutorFull(logger *slog.Logger, mutator TripleMutator, publisher Publisher) *ActionExecutor {
	if logger == nil {
		logger = slog.Default()
	}
	return &ActionExecutor{
		logger:        logger,
		tripleMutator: mutator,
		publisher:     publisher,
	}
}

// NewActionExecutorComplete creates an ActionExecutor with all capabilities including KV writes.
func NewActionExecutorComplete(logger *slog.Logger, mutator TripleMutator, publisher Publisher, kvWriter KVWriter) *ActionExecutor {
	if logger == nil {
		logger = slog.Default()
	}
	return &ActionExecutor{
		logger:        logger,
		tripleMutator: mutator,
		publisher:     publisher,
		kvWriter:      kvWriter,
	}
}

// Execute runs the given action using the execution context.
// The ExecutionContext provides the entity ID, related entity ID, full entity state,
// and match state for rich action execution.
func (e *ActionExecutor) Execute(ctx context.Context, action Action, ec *ExecutionContext) error {
	switch action.Type {
	case ActionTypeAddTriple:
		_, err := e.ExecuteAddTriple(ctx, action, ec)
		return err
	case ActionTypeRemoveTriple:
		return e.ExecuteRemoveTriple(ctx, action, ec)
	case ActionTypePublish:
		return e.executePublish(ctx, action, ec)
	case ActionTypeUpdateTriple:
		return e.executeUpdateTriple(ctx, action, ec)
	case ActionTypePublishAgent:
		return e.executePublishAgent(ctx, action, ec)
	case ActionTypeTriggerWorkflow:
		return e.executeTriggerWorkflow(ctx, action, ec)
	case ActionTypePublishBoidSignal:
		return e.executePublishBoidSignal(ctx, action, ec)
	case ActionTypeUpdateKV:
		return e.executeUpdateKV(ctx, action, ec)
	case ActionTypeDeny:
		return e.executeDeny(ctx, action, ec)
	default:
		return fmt.Errorf("unknown action type: %s", action.Type)
	}
}

// ExecuteAddTriple executes an add_triple action, creating a new semantic triple.
// Returns the created triple and any error that occurred.
// If a TripleMutator is configured, the triple is persisted via NATS request/response.
func (e *ActionExecutor) ExecuteAddTriple(ctx context.Context, action Action, ec *ExecutionContext) (message.Triple, error) {
	entityID := ec.EntityID

	// Validate predicate is present
	if action.Predicate == "" {
		return message.Triple{}, errors.New("predicate is required for add_triple action")
	}

	// Substitute variables in predicate and object
	predicate := ec.SubstituteVariables(action.Predicate)
	object := ec.SubstituteVariables(action.Object)

	// Parse TTL
	ttl, err := action.ParseTTL()
	if err != nil {
		return message.Triple{}, fmt.Errorf("parse TTL: %w", err)
	}

	// Calculate expiration time if TTL is set
	var expiresAt *time.Time
	if ttl > 0 {
		expTime := time.Now().Add(ttl)
		expiresAt = &expTime
	}

	// Create the triple
	triple := message.Triple{
		Subject:    entityID,
		Predicate:  predicate,
		Object:     object,
		Source:     "rule_engine",
		Timestamp:  time.Now(),
		Confidence: 1.0,
		ExpiresAt:  expiresAt,
	}

	if e.logger != nil {
		e.logger.Debug("Adding triple",
			"entity_id", entityID,
			"predicate", predicate,
			"object", object,
			"ttl", ttl,
			"expires_at", expiresAt)
	}

	// Persist triple via NATS request/response if mutator is configured
	if e.tripleMutator != nil {
		revision, err := e.tripleMutator.AddTriple(ctx, ec.RuleID(), triple)
		if err != nil {
			return message.Triple{}, fmt.Errorf("persist triple: %w", err)
		}
		if e.logger != nil {
			e.logger.Debug("Triple persisted",
				"entity_id", entityID,
				"predicate", predicate,
				"kv_revision", revision)
		}
	} else if e.logger != nil {
		e.logger.Debug("Triple not persisted (no mutator configured)",
			"entity_id", entityID,
			"predicate", predicate)
	}

	return triple, nil
}

// ExecuteRemoveTriple executes a remove_triple action, removing a semantic triple.
// If a TripleMutator is configured, the triple is removed via NATS request/response.
func (e *ActionExecutor) ExecuteRemoveTriple(ctx context.Context, action Action, ec *ExecutionContext) error {
	entityID := ec.EntityID

	// Validate predicate is present
	if action.Predicate == "" {
		return errors.New("predicate is required for remove_triple action")
	}

	predicate := ec.SubstituteVariables(action.Predicate)
	object := ec.SubstituteVariables(action.Object)

	if e.logger != nil {
		e.logger.Debug("Removing triple",
			"entity_id", entityID,
			"predicate", predicate,
			"object", object)
	}

	// Remove triple via NATS request/response if mutator is configured
	if e.tripleMutator != nil {
		revision, err := e.tripleMutator.RemoveTriple(ctx, ec.RuleID(), entityID, predicate)
		if err != nil {
			return fmt.Errorf("remove triple: %w", err)
		}
		if e.logger != nil {
			e.logger.Debug("Triple removed",
				"entity_id", entityID,
				"predicate", predicate,
				"kv_revision", revision)
		}
	} else if e.logger != nil {
		e.logger.Debug("Triple not removed (no mutator configured)",
			"entity_id", entityID,
			"predicate", predicate)
	}

	return nil
}

// executePublish executes a publish action, sending a message to a NATS subject.
func (e *ActionExecutor) executePublish(ctx context.Context, action Action, ec *ExecutionContext) error {
	entityID := ec.EntityID

	// Validate subject is present
	if action.Subject == "" {
		return errors.New("subject is required for publish action")
	}

	subject := ec.SubstituteVariables(action.Subject)

	// Build the message payload
	payload := map[string]any{
		"entity_id":  entityID,
		"subject":    subject,
		"timestamp":  time.Now().Format(time.RFC3339Nano),
		"source":     "rule_engine",
		"properties": action.Properties,
	}
	if ec.RelatedID != "" {
		payload["related_id"] = ec.RelatedID
	}

	if e.logger != nil {
		e.logger.Debug("Publishing message",
			"subject", subject,
			"entity_id", entityID,
			"related_id", ec.RelatedID,
			"properties", action.Properties)
	}

	// Publish via NATS if publisher is configured
	if e.publisher != nil {
		data, err := json.Marshal(payload)
		if err != nil {
			return fmt.Errorf("marshal publish payload: %w", err)
		}

		if err := e.publisher.Publish(ctx, subject, data); err != nil {
			return fmt.Errorf("publish to %s: %w", subject, err)
		}

		if e.logger != nil {
			e.logger.Debug("Message published",
				"subject", subject,
				"entity_id", entityID,
				"size", len(data))
		}
	} else if e.logger != nil {
		e.logger.Debug("Message not published (no publisher configured)",
			"subject", subject,
			"entity_id", entityID)
	}

	return nil
}

// executeUpdateTriple executes an update_triple action by removing the existing triple
// and adding a new one with the updated values. This is the only way to "update" a triple
// since triples are identified by (subject, predicate, object) - changing any of those
// creates a different triple.
func (e *ActionExecutor) executeUpdateTriple(ctx context.Context, action Action, ec *ExecutionContext) error {
	entityID := ec.EntityID

	// Validate predicate is present
	if action.Predicate == "" {
		return errors.New("predicate is required for update_triple action")
	}

	predicate := ec.SubstituteVariables(action.Predicate)
	object := ec.SubstituteVariables(action.Object)

	if e.logger != nil {
		e.logger.Debug("Updating triple (remove + add)",
			"entity_id", entityID,
			"predicate", predicate,
			"object", object,
			"properties", action.Properties)
	}

	// Step 1: Remove existing triple with this predicate
	if e.tripleMutator != nil {
		_, err := e.tripleMutator.RemoveTriple(ctx, ec.RuleID(), entityID, predicate)
		if err != nil {
			// Log but continue - triple may not exist, which is fine for update
			if e.logger != nil {
				e.logger.Debug("No existing triple to remove (or error)",
					"entity_id", entityID,
					"predicate", predicate,
					"error", err)
			}
		}
	}

	// Step 2: Add the new triple with updated values
	// Parse TTL
	ttl, err := action.ParseTTL()
	if err != nil {
		return fmt.Errorf("parse TTL: %w", err)
	}

	var expiresAt *time.Time
	if ttl > 0 {
		expTime := time.Now().Add(ttl)
		expiresAt = &expTime
	}

	triple := message.Triple{
		Subject:    entityID,
		Predicate:  predicate,
		Object:     object,
		Source:     "rule_engine",
		Timestamp:  time.Now(),
		Confidence: 1.0,
		ExpiresAt:  expiresAt,
	}

	if e.tripleMutator != nil {
		revision, err := e.tripleMutator.AddTriple(ctx, ec.RuleID(), triple)
		if err != nil {
			return fmt.Errorf("add updated triple: %w", err)
		}
		if e.logger != nil {
			e.logger.Debug("Triple updated",
				"entity_id", entityID,
				"predicate", predicate,
				"object", object,
				"kv_revision", revision)
		}
	} else if e.logger != nil {
		e.logger.Debug("Triple not updated (no mutator configured)",
			"entity_id", entityID,
			"predicate", predicate)
	}

	return nil
}

// resolveToolNames looks up the given tool names in the agentictools
// global registry and returns the matching ToolDefinition list. Names not
// found in the registry are logged at Warn and dropped — a missing tool
// shouldn't fail the spawn, just narrow the advertised set.
//
// Called by executePublishAgent when action.Tools is non-empty. Centralised
// here so the executePublishAgent path stays readable and the same resolver
// can be reused by other action types in the future.
func (e *ActionExecutor) resolveToolNames(names []string) []agentic.ToolDefinition {
	if len(names) == 0 {
		return nil
	}
	if e.toolRegistry == nil {
		if e.logger != nil {
			e.logger.Debug("publish_agent: no shared tool registry; default_tools resolution skipped",
				"requested", names)
		}
		return nil
	}
	all := e.toolRegistry.ListTools()
	byName := make(map[string]agentic.ToolDefinition, len(all))
	for _, t := range all {
		byName[t.Name] = t
	}

	resolved := make([]agentic.ToolDefinition, 0, len(names))
	for _, name := range names {
		if def, ok := byName[name]; ok {
			resolved = append(resolved, def)
			continue
		}
		if e.logger != nil {
			e.logger.Warn("publish_agent tool name not found in registry; dropped",
				"tool_name", name)
		}
	}
	return resolved
}

// executePublishAgent executes a publish_agent action, triggering an agentic loop.
// It publishes a TaskMessage to the specified NATS subject.
func (e *ActionExecutor) executePublishAgent(ctx context.Context, action Action, ec *ExecutionContext) error {
	entityID := ec.EntityID
	// Validate required fields
	if action.Subject == "" {
		return errors.New("subject is required for publish_agent action")
	}
	if action.Role == "" {
		return errors.New("role is required for publish_agent action")
	}
	if action.Model == "" {
		return errors.New("model is required for publish_agent action")
	}
	if action.Prompt == "" {
		return errors.New("prompt is required for publish_agent action")
	}

	// Substitute variables in subject and prompt
	subject := ec.SubstituteVariables(action.Subject)
	prompt := ec.SubstituteVariables(action.Prompt)

	// Generate a unique task ID
	taskID := fmt.Sprintf("rule-%s-%d", entityID, time.Now().UnixNano())

	// Build the TaskMessage
	task := agentic.TaskMessage{
		TaskID:       taskID,
		Role:         action.Role,
		Model:        action.Model,
		Prompt:       prompt,
		WorkflowSlug: ec.SubstituteVariables(action.WorkflowSlug),
		WorkflowStep: ec.SubstituteVariables(action.WorkflowStep),
	}

	// Resolve per-agent tool allowlist from the global registry. Nil
	// action.Tools leaves task.Tools unset (loop falls back to global
	// discovery). An explicit empty slice produces non-nil empty
	// task.Tools so the loop respects "no tools for this role" instead
	// of falling back. Unknown names are logged and dropped.
	if action.Tools != nil {
		resolved := e.resolveToolNames(action.Tools)
		if resolved == nil {
			resolved = []agentic.ToolDefinition{}
		}
		task.Tools = resolved
	}

	// Per-spawn action_allowlist for the decide tool. The agentic-loop
	// propagates TaskMessage.Metadata onto each ToolCall.Metadata; the
	// decide tool's executor reads this key, validates the action
	// argument, and returns ToolErrorInvalidArgs (with the valid set in
	// the message) if the model picks a name outside the allowlist.
	// Belt-and-suspenders for persona prose.
	//
	// The slice is stored as []any (not []string) so the JSON round-trip
	// through TaskMessage→agent.task→ToolCall preserves shape; decide's
	// validator coerces back at read time. Nil/empty leaves the gate off.
	if len(action.ActionAllowlist) > 0 {
		if task.Metadata == nil {
			task.Metadata = map[string]any{}
		}
		allowlist := make([]any, 0, len(action.ActionAllowlist))
		for _, a := range action.ActionAllowlist {
			allowlist = append(allowlist, a)
		}
		task.Metadata[agentic.MetadataKeyDecideActionAllowlist] = allowlist
	}

	if e.logger != nil {
		e.logger.Debug("Triggering agent task",
			"subject", subject,
			"task_id", taskID,
			"role", action.Role,
			"model", action.Model,
			"entity_id", entityID)
	}

	// Publish via NATS if publisher is configured
	published := false
	if e.publisher != nil {
		// Wrap task in BaseMessage envelope (required by agentic-loop)
		baseMsg := message.NewBaseMessage(task.Schema(), &task, "rule-engine")
		data, err := json.Marshal(baseMsg)
		if err != nil {
			return fmt.Errorf("marshal task message: %w", err)
		}

		if err := e.publisher.Publish(ctx, subject, data); err != nil {
			return fmt.Errorf("publish agent task to %s: %w", subject, err)
		}
		published = true

		if e.logger != nil {
			e.logger.Debug("Agent task published",
				"subject", subject,
				"task_id", taskID,
				"size", len(data))
		}
	} else if e.logger != nil {
		e.logger.Debug("Agent task not published (no publisher configured)",
			"subject", subject,
			"task_id", taskID)
	}

	// Record the spawned task ID back onto the entity so downstream rules can
	// reference it via $entity.triple.rule.spawned_task. Without this, the
	// generated taskID exists only inside the published TaskMessage and is
	// invisible to the rest of the rule engine. The write is tracked against
	// the originating rule so it does not re-trigger the same rule; sibling
	// rules watching ENTITY_STATES still see the new triple and fire.
	if published && e.tripleMutator != nil {
		spawnedTriple := message.Triple{
			Subject:    entityID,
			Predicate:  "rule.spawned_task",
			Object:     taskID,
			Source:     "rule_engine",
			Timestamp:  time.Now(),
			Confidence: 1.0,
		}
		if _, err := e.tripleMutator.AddTriple(ctx, ec.RuleID(), spawnedTriple); err != nil {
			// The agent task already published; returning an error would cause
			// the rule engine to retry and double-publish. Log at Error so
			// operators see silent breakage of the downstream contract
			// ($entity.triple.rule.spawned_task) — any rule chained off that
			// predicate will not fire for this task.
			if e.logger != nil {
				e.logger.Error("Failed to record spawned task triple",
					"entity_id", entityID,
					"task_id", taskID,
					"rule_id", ec.RuleID(),
					"error", err)
			}
		}
	}

	return nil
}

// executeTriggerWorkflow triggers a reactive workflow by publishing to workflow.trigger.<workflow_id>.
// This enables rules to initiate complex orchestration workflows while keeping rules simple.
// The payload is wrapped in a BaseMessage for proper deserialization by the reactive workflow engine.
func (e *ActionExecutor) executeTriggerWorkflow(ctx context.Context, action Action, ec *ExecutionContext) error {
	entityID := ec.EntityID
	if action.WorkflowID == "" {
		return errors.New("workflow_id is required for trigger_workflow action")
	}

	// Build typed trigger payload (implements message.Payload)
	payload := &WorkflowTriggerPayload{
		WorkflowID:  action.WorkflowID,
		EntityID:    entityID,
		TriggeredAt: time.Now().UTC(),
		RelatedID:   ec.RelatedID,
		Context:     action.ContextData,
	}

	subject := fmt.Sprintf("workflow.trigger.%s", action.WorkflowID)

	if e.logger != nil {
		e.logger.Debug("Triggering workflow",
			"workflow_id", action.WorkflowID,
			"subject", subject,
			"entity_id", entityID)
	}

	// Publish via NATS if publisher is configured
	if e.publisher != nil {
		// Create BaseMessage with proper type info for deserialization
		msgType := message.Type{
			Domain:   WorkflowTriggerDomain,
			Category: WorkflowTriggerCategory,
			Version:  WorkflowTriggerVersion,
		}
		baseMsg := message.NewBaseMessage(msgType, payload, "rule-processor")

		// BaseMessage.MarshalJSON handles the wire format
		data, err := json.Marshal(baseMsg)
		if err != nil {
			return fmt.Errorf("marshal workflow trigger message: %w", err)
		}

		if err := e.publisher.Publish(ctx, subject, data); err != nil {
			return fmt.Errorf("publish workflow trigger to %s: %w", subject, err)
		}

		if e.logger != nil {
			e.logger.Debug("Workflow trigger published",
				"workflow_id", action.WorkflowID,
				"subject", subject,
				"entity_id", entityID,
				"size", len(data))
		}
	} else if e.logger != nil {
		e.logger.Debug("Workflow trigger not published (no publisher configured)",
			"workflow_id", action.WorkflowID,
			"subject", subject,
			"entity_id", entityID)
	}

	return nil
}

// executePublishBoidSignal executes a publish_boid_signal action.
// It publishes a BoidSteeringSignal to the agent.boid.<loopID> subject
// for consumption by the agentic-loop for coordination.
func (e *ActionExecutor) executePublishBoidSignal(ctx context.Context, action Action, ec *ExecutionContext) error {
	entityID := ec.EntityID
	if action.Subject == "" {
		return errors.New("subject is required for publish_boid_signal action")
	}
	if action.BoidSignalType == "" {
		return errors.New("boid_signal_type is required for publish_boid_signal action")
	}

	// Validate signal type
	validSignalTypes := map[string]bool{
		"separation": true,
		"cohesion":   true,
		"alignment":  true,
	}
	if !validSignalTypes[action.BoidSignalType] {
		return fmt.Errorf("invalid boid_signal_type %q: must be one of: separation, cohesion, alignment", action.BoidSignalType)
	}

	// Substitute variables in subject
	subject := ec.SubstituteVariables(action.Subject)

	// Build signal payload
	strength := action.BoidStrength
	if strength <= 0 || strength > 1 {
		strength = 0.5 // Default strength
	}

	signal := map[string]any{
		"loop_id":     entityID,
		"signal_type": action.BoidSignalType,
		"strength":    strength,
		"source_rule": "rule_engine",
		"timestamp":   time.Now().Format(time.RFC3339Nano),
	}

	// Add related entity as context
	if ec.RelatedID != "" {
		signal["related_id"] = ec.RelatedID
	}

	// Include any custom properties
	if action.Properties != nil {
		for k, v := range action.Properties {
			signal[k] = v
		}
	}

	if e.logger != nil {
		e.logger.Debug("Publishing boid signal",
			"subject", subject,
			"signal_type", action.BoidSignalType,
			"entity_id", entityID,
			"strength", strength)
	}

	// Publish via NATS if publisher is configured
	if e.publisher != nil {
		// Wrap in BaseMessage for proper deserialization
		msgType := message.Type{
			Domain:   "boid",
			Category: "signal",
			Version:  "v1",
		}

		// Create a generic payload wrapper
		payload := &message.GenericJSONPayload{Data: signal}
		baseMsg := message.NewBaseMessage(msgType, payload, "rule-processor")

		data, err := json.Marshal(baseMsg)
		if err != nil {
			return fmt.Errorf("marshal boid signal message: %w", err)
		}

		if err := e.publisher.Publish(ctx, subject, data); err != nil {
			return fmt.Errorf("publish boid signal to %s: %w", subject, err)
		}

		if e.logger != nil {
			e.logger.Debug("Boid signal published",
				"subject", subject,
				"signal_type", action.BoidSignalType,
				"entity_id", entityID,
				"size", len(data))
		}
	} else if e.logger != nil {
		e.logger.Debug("Boid signal not published (no publisher configured)",
			"subject", subject,
			"signal_type", action.BoidSignalType,
			"entity_id", entityID)
	}

	return nil
}

// executeDeny issues a deny verdict that short-circuits the current evaluation
// cycle. The deny verdict is the structural outcome — a *DenyVerdict error is
// always returned. Before returning, a best-effort audit triple is written via
// the TripleMutator so the denial is recorded in the knowledge graph.
//
// IMPORTANT: an audit-write failure MUST NOT flip the verdict from deny to
// allow. If AddTriple fails, the error is logged at Error level (so operators
// see the silent audit gap) and executeDeny still returns *DenyVerdict. Callers
// must not retry the action on *DenyVerdict — denial is intentional and terminal.
func (e *ActionExecutor) executeDeny(ctx context.Context, action Action, ec *ExecutionContext) error {
	reason := ec.SubstituteVariables(action.Reason)
	ruleID := ec.RuleID()

	// Best-effort audit triple. Mirror executePublishAgent's triple-write pattern.
	// If AddTriple fails, we log Error but DO NOT return that error — the deny
	// verdict is the structural outcome and must not be flipped to "allow" by
	// an audit-write failure.
	if e.tripleMutator != nil {
		auditTriple := message.Triple{
			Subject:    ruleID,
			Predicate:  PredicateRuleDeny,
			Object:     reason,
			Source:     "rule_engine",
			Timestamp:  time.Now(),
			Confidence: 1.0,
		}
		if _, err := e.tripleMutator.AddTriple(ctx, ruleID, auditTriple); err != nil {
			if e.logger != nil {
				e.logger.Error("deny verdict audit triple write failed; verdict still applies",
					"rule_id", ruleID,
					"reason", reason,
					"error", err)
			}
			// intentionally fall through — verdict is structural
		}
	}

	return &DenyVerdict{RuleID: ruleID, Reason: reason}
}

// executeUpdateKV writes JSON to a named KV bucket with optional CAS merge semantics.
// When Merge is true, the payload is merged into the existing document using CAS
// (read-modify-write with retry). When false, the payload overwrites the entire document.
func (e *ActionExecutor) executeUpdateKV(ctx context.Context, action Action, ec *ExecutionContext) error {
	if action.Bucket == "" {
		return errors.New("bucket is required for update_kv action")
	}
	if action.Key == "" {
		return errors.New("key is required for update_kv action")
	}

	bucket := ec.SubstituteVariables(action.Bucket)
	key := ec.SubstituteVariables(action.Key)
	payload := substitutePayloadVariables(action.Payload, ec)

	if e.logger != nil {
		e.logger.Debug("Executing KV write",
			"bucket", bucket,
			"key", key,
			"merge", action.Merge,
			"entity_id", ec.EntityID)
	}

	if e.kvWriter != nil {
		if action.Merge {
			err := e.kvWriter.UpdateJSON(ctx, bucket, key, func(current map[string]any) error {
				for k, v := range payload {
					current[k] = v
				}
				return nil
			})
			if err != nil {
				return fmt.Errorf("kv merge %s/%s: %w", bucket, key, err)
			}
		} else {
			if err := e.kvWriter.PutJSON(ctx, bucket, key, payload); err != nil {
				return fmt.Errorf("kv put %s/%s: %w", bucket, key, err)
			}
		}

		if e.logger != nil {
			e.logger.Debug("KV write completed",
				"bucket", bucket,
				"key", key,
				"entity_id", ec.EntityID)
		}
	} else if e.logger != nil {
		e.logger.Debug("KV write not executed (no writer configured)",
			"bucket", bucket,
			"key", key,
			"entity_id", ec.EntityID)
	}

	return nil
}

// substitutePayloadVariables performs deep variable substitution on string values
// within a payload map, including nested maps.
func substitutePayloadVariables(payload map[string]any, ec *ExecutionContext) map[string]any {
	if payload == nil {
		return nil
	}
	result := make(map[string]any, len(payload))
	for k, v := range payload {
		result[k] = substituteValue(v, ec)
	}
	return result
}

// substituteValue recursively substitutes template variables in strings,
// maps, and slices. Non-string leaf values are passed through unchanged.
func substituteValue(v any, ec *ExecutionContext) any {
	switch val := v.(type) {
	case string:
		return ec.SubstituteVariables(val)
	case map[string]any:
		return substitutePayloadVariables(val, ec)
	case []any:
		result := make([]any, len(val))
		for i, item := range val {
			result[i] = substituteValue(item, ec)
		}
		return result
	default:
		return v
	}
}

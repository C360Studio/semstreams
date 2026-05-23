// Package rule - Action execution for ECA rules
package rule

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"log/slog"

	"github.com/c360studio/semstreams/agentic"
	"github.com/c360studio/semstreams/component"
	gtypes "github.com/c360studio/semstreams/graph"
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
	// ActionTypeUpdateKV writes JSON to a named KV bucket with optional CAS merge
	ActionTypeUpdateKV = "update_kv"
	// ActionTypeDeny issues a deny verdict that short-circuits subsequent actions
	// and surfaces as *DenyVerdict to the caller. The deny is always the terminal
	// outcome: no later action in the same evaluation cycle runs once a deny fires.
	ActionTypeDeny = "deny"
	// ActionTypeApprove issues an approve verdict — writes an audit triple via
	// the TripleMutator and publishes a verdict to the configured Subject.
	// Asymmetric to deny: approve does NOT short-circuit subsequent actions.
	// Approval doesn't preclude observability/audit/derived-state actions on
	// the same rule firing; the asymmetry is the feature, not an oversight.
	// See ADR-039 §"Why explicit approve, not optimistic absence of deny".
	ActionTypeApprove = "approve"
)

// PredicateRuleDeny is the audit-triple predicate written by deny actions.
// Downstream rules that gate on denials should match against this constant
// so they stay in sync with any future rename.
const PredicateRuleDeny = "rule.deny"

// PredicateRuleApprove is the audit-triple predicate written by approve
// actions. Downstream rules that gate on approvals (rate-limit a caller's
// successful tool-call rate, audit-log every approve, etc.) should match
// against this constant so they stay in sync with any future rename.
const PredicateRuleApprove = "rule.approve"

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

	// ResponseFormat constrains the spawned loop's model output to a
	// JSON object or JSON-schema-conformant JSON. ADR-034. When non-nil,
	// executePublishAgent stamps it onto the TaskMessage.ResponseFormat;
	// the agentic-loop caches it and threads it onto every AgentRequest
	// in the loop. Nil leaves tool-calling behaviour unchanged.
	//
	// Use the agentic.NewJSONSchemaFormat / agentic.NewJSONObjectFormat
	// helpers to construct. See ADR-034 + docs/operations/13-structured-output.md
	// for provider support; small/local models (qwen3, deepseek-r1, gemma3,
	// sub-30B) are the primary use case. Frontier-cloud models keep
	// preferring tool-calling — set this only when the loop is bound to a
	// model that honours response_format.
	ResponseFormat *agentic.ResponseFormat `json:"response_format,omitempty"`

	// ToolChoice constrains the spawned loop's tool selection per
	// iteration. ADR-023. When non-nil, executePublishAgent stamps it
	// onto the TaskMessage.ToolChoice; the agentic-loop caches it and
	// threads it onto every AgentRequest in the loop. Nil leaves
	// model-decides ("auto") behaviour unchanged.
	//
	// Primary use case is the cheap-model substrate (gemini-2.5-flash,
	// small local models) where the model routinely completes a loop
	// with text-only output despite persona prose enforcing a terminal
	// tool call. Mode "required" forces _some_ tool every iteration;
	// Mode "function" with FunctionName forces a specific tool (use on
	// the terminal-forcing iteration where a structured decision is
	// expected). Validation runs as part of TaskMessage.Validate().
	ToolChoice *agentic.ToolChoice `json:"tool_choice,omitempty"`

	// RelatedLoops is cross-arc loop-ID lineage threaded onto the
	// spawned task so a downstream role can read_loop_result against
	// upstream loops without the IDs being baked into the prompt. Map
	// keys are role names (or product-specific lineage labels);
	// values are loop ID strings. When non-empty, executePublishAgent
	// substitutes variables in each value and stamps the resolved map
	// onto TaskMessage.Metadata under MetadataKeyRelatedLoops; the
	// agentic-loop propagates that onto each ToolCall.Metadata.
	//
	// String-to-string only by design. The use case is loop-ID
	// forwarding; non-string values are out of scope and would earn
	// a dedicated typed field (Tools / ToolChoice / Timeout
	// precedent) rather than nesting structured data here.
	//
	// Variable substitution applies to values, so rule authors can
	// write `"researcher": "$entity.triple.research_loop_id"` and
	// the resolved loop ID flows through. Substitution is NOT
	// applied to keys (they are role/lineage labels, not data).
	//
	// Empty/nil leaves no lineage threaded (back-compat: pre-existing
	// flows that don't opt in see no Metadata change).
	RelatedLoops map[string]string `json:"related_loops,omitempty"`

	// WorkflowID is the workflow identifier for trigger_workflow actions
	WorkflowID string `json:"workflow_id,omitempty"`

	// ContextData provides additional context passed to the workflow
	ContextData map[string]any `json:"context_data,omitempty"`

	// WorkflowSlug identifies the workflow for publish_agent actions (e.g., "github-issue-to-pr")
	WorkflowSlug string `json:"workflow_slug,omitempty"`

	// WorkflowStep identifies the step within the workflow (e.g., "qualify", "develop", "review")
	WorkflowStep string `json:"workflow_step,omitempty"`

	// When is an optional guard clause for conditional action execution.
	// All conditions must match (AND logic) for the action to execute.
	// Actions without When always execute.
	//
	// Field resolution follows the same precedence as rule-level
	// conditions (see ADR-041):
	//
	//   - `$state.<field>` / `$prev.<field>` → rule match state
	//   - `$message.<dotted.path>` → inbound message payload (recommended)
	//   - bare name → entity triples first (when entity is non-nil),
	//     falls through to message payload if not on the entity
	//
	// Use `$message.<field>` for unambiguous access in new rules. Bare
	// names work but their resolution source depends on whether the rule
	// fires on an entity-state event or a message-path event.
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

	// ID is an optional, author-supplied stable identifier used by the
	// per-action firing-cap state tracker to key MatchState.ActionIterations.
	// Empty leaves the framework to derive a deterministic auto-generated
	// fingerprint via actionFingerprint (rule_id, action_type,
	// subject_or_predicate, role) — works for the 95% case where one
	// publish_agent per rule branch suffices. Authors set ID explicitly when
	// they need stable counters across action renames or want multiple
	// distinct actions to share a counter.
	ID string `json:"id,omitempty"`

	// MaxIterations is the per-action firing cap: the action fires at most
	// this many times for a given rule+entity match-cycle, regardless of
	// how many times the rule itself fires. Cross-loop bound for the
	// structured-output ping-pong shape (action_allowlist rejects → loop
	// iterates → LLM drifts → rule re-fires).
	//
	// Sentinel values:
	//   - nil / JSON field omitted → framework default (DefaultActionMaxIterations = 3)
	//   - pointer to 0 / `"max_iterations": 0` in JSON → unlimited (operator opts out)
	//   - pointer to N>0 → explicit cap of N
	//
	// Default-on-cap reflects the "structured output retries are the rule,
	// not the exception" reality semspec/semteams confirmed 2026-05-05. If
	// an operator hits the cap repeatedly, the model/persona is wrong for
	// the role — raising the cap papers over the underlying problem; fix
	// the persona prompt or model choice instead.
	//
	// Pointer (rather than int + sentinel) is required to distinguish
	// "unset" from "explicit 0" on the JSON wire. Direct field reads will
	// crash on nil; use the helper effectiveMaxIterations to resolve
	// correctly.
	MaxIterations *int `json:"max_iterations,omitempty"`

	// Reason is the human-readable verdict message for deny AND approve
	// actions. For deny it travels with the *DenyVerdict error so callers
	// can record it without a side-channel lookup; for approve it lands in
	// the audit triple and in the verdict payload published to Subject.
	// Variable substitution is applied (e.g.
	// "$caller.id denied: insufficient role $caller.role" or
	// "$caller.id approved for tool $message.tool_name").
	Reason string `json:"reason,omitempty"`

	// ForEach is a substitution-resolvable reference to a list-typed
	// value the action iterates over. Each iteration binds the current
	// item to the variable named in ForEachVar and re-runs the action's
	// body with that overlay applied to all substituted strings
	// (Prompt, Properties, related_loops values, Subject template).
	// ADR-046 Phase 1; only publish_agent honours this today.
	//
	// Example shape: ForEach=`$entity.triple.coordinator.decision.subtopics`,
	// ForEachVar=`subtopic`. When the trigger entity has that triple set
	// to ["hydraulics", "pneumatics", "electrics"], the rule spawns
	// three publish_agent dispatches with $subtopic bound to each value
	// in turn. Iterations are independent by contract — no DependsOn
	// edges in Phase 1 (gated-DAG dispatch is Phase 2). Each iteration
	// is a non-blocking NATS publish; the agentic-loop's JetStream
	// consumer parallelism gives N concurrent loop executions
	// automatically.
	//
	// Empty/unset disables iteration (back-compat: existing rules with
	// no ForEach fire exactly once, current behaviour).
	//
	// Resolution: the substitution layer extracts list-typed triple
	// objects as []any; non-list resolution (string, missing triple)
	// is logged and treated as a single-element list containing the
	// resolved value so author errors surface loudly rather than
	// silently no-op. Empty list resolves to zero iterations (no
	// dispatch) — a valid degenerate case (decomposer found nothing).
	ForEach string `json:"for_each,omitempty"`

	// ForEachVar names the per-iteration substitution variable the
	// resolved ForEach item binds to. Referenced as `$<name>` in any
	// substituted string on the action body. Required when ForEach is
	// set; ignored otherwise.
	ForEachVar string `json:"for_each_var,omitempty"`
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
	case ActionTypeUpdateKV:
		return e.executeUpdateKV(ctx, action, ec)
	case ActionTypeDeny:
		return e.executeDeny(ctx, action, ec)
	case ActionTypeApprove:
		return e.executeApprove(ctx, action, ec)
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

// substituteStringProperties returns a shallow copy of props with any
// top-level string value run through ec.SubstituteVariables. Non-string
// values (numbers, bools, nested maps, arrays) pass through unchanged.
//
// The shallow-only contract matches the public docs at
// `docs/operations/17-tool-call-governance.md` which describe
// `properties` as a flat map. Deep-nested template strings (e.g.
// `properties.foo.bar = "$message.x"`) are intentionally NOT recursed
// — that's a separate behaviour contract and warrants its own
// substitution-warning story. Returns nil unchanged so callers don't
// have to special-case empty maps.
func substituteStringProperties(props map[string]any, ec *ExecutionContext) map[string]any {
	if props == nil {
		return nil
	}
	out := make(map[string]any, len(props))
	for k, v := range props {
		if s, ok := v.(string); ok {
			out[k] = ec.SubstituteVariables(s)
			continue
		}
		out[k] = v
	}
	return out
}

// executePublish executes a publish action, sending a message to a NATS subject.
func (e *ActionExecutor) executePublish(ctx context.Context, action Action, ec *ExecutionContext) error {
	entityID := ec.EntityID

	// Validate subject is present
	if action.Subject == "" {
		return errors.New("subject is required for publish action")
	}

	subject := ec.SubstituteVariables(action.Subject)
	// Substitute $message.*/$entity.* etc. tokens in string property
	// values before publish. ADR-039's canonical reject pattern relies
	// on `properties.call_id = "$message.call_id"` resolving so the
	// agentic-loop verdict dispatcher can demux via
	// VerdictPayload.EffectiveCallID (falls back to Properties when
	// top-level CallID is empty). Pre-fix, the literal template string
	// reached the wire and broke enforce-mode routing.
	properties := substituteStringProperties(action.Properties, ec)

	// Build the message payload
	payload := map[string]any{
		"entity_id":  entityID,
		"subject":    subject,
		"timestamp":  time.Now().Format(time.RFC3339Nano),
		"source":     "rule_engine",
		"properties": properties,
	}
	if ec.RelatedID != "" {
		payload["related_id"] = ec.RelatedID
	}

	if e.logger != nil {
		e.logger.Debug("Publishing message",
			"subject", subject,
			"entity_id", entityID,
			"related_id", ec.RelatedID,
			"properties", properties)
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

// stampRelatedLoops writes the cross-arc loop-ID lineage map onto
// the TaskMessage.Metadata under agentic.MetadataKeyRelatedLoops.
// Each value goes through ec.SubstituteVariablesWithIterVar so rule
// authors can declare `"researcher": "$entity.triple.research_loop_id"`
// and the resolved loop ID flows through, with the for_each iter-var
// (ADR-046 Phase 1) also bound when set so authors can thread the
// current item into a related-loop label if a use case arises.
// String-to-string by design — see agentic.MetadataKeyRelatedLoops.
// Empty/nil RelatedLoops is a no-op (back-compat: pre-existing flows
// that don't opt in see no Metadata change).
func stampRelatedLoops(task *agentic.TaskMessage, related map[string]string, ec *ExecutionContext, iterVarName, iterVarValue string) {
	if len(related) == 0 {
		return
	}
	if task.Metadata == nil {
		task.Metadata = map[string]any{}
	}
	resolved := make(map[string]any, len(related))
	for label, loopID := range related {
		resolved[label] = ec.SubstituteVariablesWithIterVar(loopID, iterVarName, iterVarValue)
	}
	task.Metadata[agentic.MetadataKeyRelatedLoops] = resolved
}

// stampPerSpawnLLMKnobs threads the rule.Action's per-spawn LLM
// constraints (ResponseFormat — ADR-034; ToolChoice — ADR-023) onto
// the TaskMessage. The agentic-loop caches each on initial build and
// threads it onto every AgentRequest in the loop. Nil on either side
// is a no-op (back-compat: pre-opt-in flows keep their pre-existing
// tool-calling / model-decides behaviour).
func stampPerSpawnLLMKnobs(task *agentic.TaskMessage, action Action) {
	if action.ResponseFormat != nil {
		task.ResponseFormat = action.ResponseFormat
	}
	if action.ToolChoice != nil {
		task.ToolChoice = action.ToolChoice
	}
}

// diagnoseForEachResolutionFailure returns a short human-readable
// reason string describing why ResolveListValue returned ok=false on
// the given reference. Three possibilities the resolver can fail on:
// (a) reference uses a namespace Phase 1 doesn't support, (b) the
// target entity is nil (message-path rule with no trigger entity),
// (c) the predicate is missing from the entity at fire-time, or (d)
// the Object exists but isn't list-shaped. Used in the for_each
// non-list Warn so operators don't have to retrace the resolver to
// figure out which sub-condition failed.
func diagnoseForEachResolutionFailure(reference string, ec *ExecutionContext) string {
	const entityPrefix = "$entity.triple."
	const relatedPrefix = "$related.triple."
	var entity *gtypes.EntityState
	var predicate string
	switch {
	case strings.HasPrefix(reference, entityPrefix):
		entity = ec.Entity
		predicate = strings.TrimPrefix(reference, entityPrefix)
	case strings.HasPrefix(reference, relatedPrefix):
		entity = ec.Related
		predicate = strings.TrimPrefix(reference, relatedPrefix)
	default:
		return "unsupported namespace (Phase 1 supports $entity.triple.* and $related.triple.* only)"
	}
	if entity == nil {
		return "trigger entity is nil (message-path rule with no entity in scope?)"
	}
	for _, triple := range entity.Triples {
		if triple.Predicate == predicate {
			return fmt.Sprintf("predicate found but Object shape %T is not list-coercible", triple.Object)
		}
	}
	return fmt.Sprintf("predicate %q not present on entity at fire time (typo or race with late triple arrival)", predicate)
}

// executePublishAgent executes a publish_agent action, triggering an
// agentic loop. ADR-046 Phase 1: when action.ForEach is set, the body
// runs once per resolved list item with $<ForEachVar> bound to the
// current value. Otherwise it runs exactly once with no iter-var
// overlay (current behaviour). Each iteration is a non-blocking NATS
// publish; the agentic-loop's JetStream consumer gives N concurrent
// loop executions for free.
func (e *ActionExecutor) executePublishAgent(ctx context.Context, action Action, ec *ExecutionContext) error {
	// Validate required fields up front, before any iteration —
	// missing fields are an authoring error, not a per-item failure.
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

	// ADR-046 Phase 1: for_each iteration. Resolve the list once,
	// then dispatch one TaskMessage per item with the iter-var
	// overlay bound. Empty list resolves to zero dispatches (a valid
	// degenerate case: decomposer found nothing). Missing/invalid
	// list reference logs at Warn and degenerates to a single
	// dispatch with the iter-var unbound — author error stays loud
	// (the unresolved-template warning trips on $<ForEachVar>
	// references) rather than silently no-op.
	if action.ForEach != "" {
		if action.ForEachVar == "" {
			return errors.New("for_each_var is required when for_each is set on publish_agent action")
		}
		items, ok := ec.ResolveListValue(action.ForEach)
		if !ok {
			if e.logger != nil {
				// Diagnostic surface: which sub-condition failed so the
				// operator doesn't have to retrace ResolveListValue's
				// branches. Three possibilities — entity nil
				// (message-path rule with no trigger entity), predicate
				// missing (race with late triple arrival or typo), or
				// Object shape wrong (scalar string where a list was
				// expected). The for_each_resolution_failure_reason
				// field lets dashboard grouping pivot on the cause.
				e.logger.Warn("publish_agent for_each: list reference did not resolve to a list; degenerating to single dispatch with iter-var unbound",
					"for_each", action.ForEach,
					"for_each_var", action.ForEachVar,
					"rule_id", ec.RuleID(),
					"entity_id", ec.EntityID,
					"for_each_resolution_failure_reason", diagnoseForEachResolutionFailure(action.ForEach, ec),
					"hint", "verify the predicate name + that it carries a list-typed Object (decide stamps coordinator.decision.subtopics as a JSON-encoded []string)")
			}
			return e.publishAgentOnce(ctx, action, ec, "", "")
		}
		if len(items) == 0 {
			if e.logger != nil {
				e.logger.Info("publish_agent for_each: empty list — no dispatches",
					"for_each", action.ForEach,
					"rule_id", ec.RuleID(),
					"entity_id", ec.EntityID)
			}
			return nil
		}
		for _, item := range items {
			if err := e.publishAgentOnce(ctx, action, ec, action.ForEachVar, item); err != nil {
				return err
			}
		}
		return nil
	}

	return e.publishAgentOnce(ctx, action, ec, "", "")
}

// publishAgentOnce is the per-iteration publish-agent body. Carries
// the iter-var overlay (empty varName disables it — the non-for_each
// path passes "" / ""). Extracted from executePublishAgent so the
// for_each loop can call it N times without duplicating the publish
// + state-stamp logic.
func (e *ActionExecutor) publishAgentOnce(ctx context.Context, action Action, ec *ExecutionContext, iterVarName, iterVarValue string) error {
	entityID := ec.EntityID

	// Substitute variables in subject and prompt — iter-var overlay
	// applies to all substituted strings on this iteration.
	subject := ec.SubstituteVariablesWithIterVar(action.Subject, iterVarName, iterVarValue)
	prompt := ec.SubstituteVariablesWithIterVar(action.Prompt, iterVarName, iterVarValue)

	// Generate a unique task ID
	taskID := fmt.Sprintf("rule-%s-%d", entityID, time.Now().UnixNano())

	// Build the TaskMessage
	task := agentic.TaskMessage{
		TaskID:       taskID,
		Role:         action.Role,
		Model:        action.Model,
		Prompt:       prompt,
		WorkflowSlug: ec.SubstituteVariablesWithIterVar(action.WorkflowSlug, iterVarName, iterVarValue),
		WorkflowStep: ec.SubstituteVariablesWithIterVar(action.WorkflowStep, iterVarName, iterVarValue),
	}

	// Inherit ParentLoopID when the trigger entity is a loop execution. Without
	// this, rule-fanned chains carry no parent linkage natively — only the
	// depth-tracked architect→editor subagent path that sets ParentLoopID
	// at TaskMessage construction time gets the agent.loop.parent triple
	// stamped at completion (handlers.go:264 → SetParentLoopID → state.go).
	// With this wired in, every rule-fanned spawn from a loop entity has a
	// walkable agent.loop.parent ancestry, so product code (semteams chain,
	// future chain consumers) can derive chain_id by walking parent without
	// per-rule lineage threading. Required for semteams ADR-038's chain
	// entity pattern; preserves backward-compat for rule-fanned spawns
	// triggered by non-loop entities (no ParentLoopID set, current behavior).
	if parentLoopID, ok := agentic.LoopIDFromExecutionEntityID(entityID); ok {
		task.ParentLoopID = parentLoopID
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

	stampPerSpawnLLMKnobs(&task, action)

	// Per-spawn cross-arc loop-ID lineage. Mirrors the ActionAllowlist
	// Metadata stamping pattern. See stampRelatedLoops for the why.
	stampRelatedLoops(&task, action.RelatedLoops, ec, iterVarName, iterVarValue)

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

// executeApprove issues an approve verdict — writes a best-effort audit triple
// and publishes a verdict payload to the configured Subject. Asymmetric to
// executeDeny: approve returns nil and does NOT short-circuit subsequent
// actions. Approval is permissive, not terminal; later actions in the same
// rule firing (observability triples, downstream notifications, derived
// state) still run. See ADR-039 §"Why explicit approve, not optimistic
// absence of deny" for the design rationale.
//
// Audit-write failure does NOT flip the verdict from approve to anything
// else: the verdict is the structural outcome, logged at Error level so
// operators see the audit gap, and the publish still attempts. Publish
// failure DOES return an error — unlike audit, publish failure means
// downstream consumers (the agentic-loop subject-mode dispatcher) never
// learn the verdict and would time out.
func (e *ActionExecutor) executeApprove(ctx context.Context, action Action, ec *ExecutionContext) error {
	if action.Subject == "" {
		return errors.New("subject is required for approve action")
	}

	subject := ec.SubstituteVariables(action.Subject)
	reason := ec.SubstituteVariables(action.Reason)
	ruleID := ec.RuleID()
	entityID := ec.EntityID

	// Best-effort audit triple. Mirror executeDeny's pattern: audit-write
	// failure must NOT change the verdict — approve still proceeds to
	// publish so downstream consumers see the decision.
	if e.tripleMutator != nil {
		auditTriple := message.Triple{
			Subject:    ruleID,
			Predicate:  PredicateRuleApprove,
			Object:     reason,
			Source:     "rule_engine",
			Timestamp:  time.Now(),
			Confidence: 1.0,
		}
		if _, err := e.tripleMutator.AddTriple(ctx, ruleID, auditTriple); err != nil {
			if e.logger != nil {
				e.logger.Error("approve verdict audit triple write failed; verdict still applies",
					"rule_id", ruleID,
					"subject", subject,
					"reason", reason,
					"error", err)
			}
			// fall through — verdict is structural, publish below must run
		}
	}

	// Publish the verdict payload. The subject itself carries the routing
	// identity (e.g. `agent.toolcall.approved.<loop_id>.<call_id>`); the
	// payload carries context for audit/ops/replay.
	if e.publisher == nil {
		if e.logger != nil {
			e.logger.Debug("Approve verdict not published (no publisher configured)",
				"subject", subject,
				"rule_id", ruleID)
		}
		return nil
	}

	payloadData := map[string]any{
		"decision":  "approved",
		"rule_id":   ruleID,
		"reason":    reason,
		"entity_id": entityID,
		"timestamp": time.Now().Format(time.RFC3339Nano),
	}
	// Echo call_id / loop_id from the proposed message so downstream
	// consumers (the agentic-loop subject-mode dispatcher) can demux
	// verdicts without parsing the subject. ADR-039: the subject IS
	// authoritative for the decision, but the payload mirrors the
	// routing identifiers so consumers don't need a custom subject
	// parser. No-op when MessageData is nil (cron-fired or entity-
	// state evaluations have no inbound call_id).
	if ec.MessageData != nil {
		if v, ok := ec.MessageData["call_id"].(string); ok && v != "" {
			payloadData["call_id"] = v
		}
		if v, ok := ec.MessageData["loop_id"].(string); ok && v != "" {
			payloadData["loop_id"] = v
		}
	}
	// Wrap in a `core.json.v1` BaseMessage so subscribers using the
	// payload registry (`message.NewDecoder(reg).Decode(data)`) can
	// read this off the wire. Raw `json.Marshal(map)` would deliver
	// silently to the agentic-loop's bespoke handler today but trap
	// any future audit/ops dashboard subscriber that decodes via
	// registry. See feedback_nats_publishes_use_payload_registry.
	generic := message.NewGenericJSON(payloadData)
	baseMsg := message.NewBaseMessage(generic.Schema(), generic, "rule_engine")
	data, err := json.Marshal(baseMsg)
	if err != nil {
		return fmt.Errorf("marshal approve verdict payload: %w", err)
	}
	if err := e.publisher.Publish(ctx, subject, data); err != nil {
		return fmt.Errorf("publish approve verdict to %s: %w", subject, err)
	}

	if e.logger != nil {
		e.logger.Debug("Approve verdict published",
			"subject", subject,
			"rule_id", ruleID,
			"reason", reason,
			"size", len(data))
	}
	return nil
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

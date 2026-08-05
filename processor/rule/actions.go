// Package rule - Action execution for ECA rules
package rule

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	"log/slog"

	"github.com/c360studio/semstreams/agentic"
	"github.com/c360studio/semstreams/agentic/agentrun"
	"github.com/c360studio/semstreams/component"
	"github.com/c360studio/semstreams/governance"
	gtypes "github.com/c360studio/semstreams/graph"
	"github.com/c360studio/semstreams/message"
	"github.com/c360studio/semstreams/pkg/errs"
	"github.com/c360studio/semstreams/pkg/lifecycle"
	"github.com/c360studio/semstreams/pkg/projection"
	"github.com/c360studio/semstreams/processor/rule/expression"
	agvocab "github.com/c360studio/semstreams/vocabulary/agentic"
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
	// ActionTypeReconcilePredicates replaces (or clears) the selected predicate
	// through the rule pack's bound projection.MutationClient. The public
	// client atomically reconciles the complete named ModeReconcile group,
	// preserving predicates outside that group. A raw empty Object clears the
	// entire selected named group; a raw non-empty Object authors one desired
	// triple even when substitution resolves to an empty value. The contract,
	// group, and literal predicate are resolved against the immutable boot-time
	// projection target index at load/hot-reload time (HARD-FAIL on violation).
	ActionTypeReconcilePredicates = "reconcile_predicates"
	// ActionTypePublishAgent triggers an agentic loop by publishing a TaskMessage
	ActionTypePublishAgent = "publish_agent"
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
	// ActionTypeLifecycleTransition moves a lifecycle-managed entity to a new
	// phase via the registered pkg/lifecycle.Manager. Atomic with optional
	// set-ops on the same Update closure. See ADR-047.
	ActionTypeLifecycleTransition = "lifecycle_transition"
	// ActionTypeLifecycleComplete transitions a lifecycle-managed entity to
	// the first terminal phase reachable from its current phase. See ADR-047
	// + Manager.Complete for the selection rule.
	ActionTypeLifecycleComplete = "lifecycle_complete"
	// ActionTypeLifecycleFail transitions a lifecycle-managed entity to the
	// declared "failed" terminal phase, carrying the reason for audit. See
	// ADR-047 + Manager.Fail.
	ActionTypeLifecycleFail = "lifecycle_fail"
)

// NOTE: the former PredicateRuleDeny/PredicateRuleApprove audit-triple
// predicates ("rule.deny"/"rule.approve") were retired in ADR-055 §3a. Deny and
// approve no longer write an audit triple onto a phantom rule-ID entity (that
// rode the graph-ingest auto-vivify path ADR-055 deletes); they emit a
// registered verdict event to the append-only GOVERNANCE_VERDICT_AUDIT stream
// (see VerdictAuditor + governance.VerdictEvent). This amends ADR-039's audit
// mechanism while preserving its explicit-verdict-audit goal.

// Action represents an action to execute when a rule fires.
// Actions are triggered by state transitions (OnEnter, OnExit) or
// while a condition remains true (WhileTrue).
type Action struct {
	// Type specifies the action type (publish, add_triple, remove_triple, update_triple, publish_agent)
	Type string `json:"type"`

	// Subject serves two roles depending on action type:
	//   - publish / publish_agent: the NATS subject the message is sent to.
	//   - add_triple / update_triple / remove_triple (#147 / ADR-046
	//     Phase 1 join gap): override target entity ID the triple is
	//     written to. Substitution-resolved. Empty defaults to the
	//     trigger entity (ec.EntityID) — back-compat for rules that
	//     stamp on themselves. Non-empty Subject that resolves to an
	//     empty string after substitution is an authoring error
	//     (returned from Execute, not silently coerced to EntityID —
	//     matches the discipline from PR #138 for_each resolver where
	//     silent fallback would mask typos in $entity.triple.*
	//     references).
	//
	// The two roles don't overlap operationally — publish writes a
	// NATS subject; triple-writes don't take a NATS subject — so the
	// shared field keeps the Action surface narrow. Tools that ride
	// the rule-engine path (#147's reference config) document
	// "subject = parent loop entity ID" right at the rule definition,
	// which is the readable shape for the counter pattern.
	Subject string `json:"subject,omitempty"`

	// Predicate is the relationship type for triple actions
	Predicate string `json:"predicate,omitempty"`

	// ProjectionContract and ProjectionGroup select the exact immutable
	// reconcile target. Both are required for reconcile_predicates actions.
	ProjectionContract string `json:"projection_contract,omitempty"`
	ProjectionGroup    string `json:"projection_group,omitempty"`

	// Object is the target entity or value for triple actions
	Object string `json:"object,omitempty"`

	// TTL specifies optional expiration time for triples (e.g., "5m", "1h")
	TTL string `json:"ttl,omitempty"`

	// Properties contains additional metadata for the action.
	//
	// For `publish`, substituted properties are carried as the emitted
	// payload's `properties` field. For `publish_agent`, substituted
	// properties (string values are iter-var aware) are stamped onto the
	// dispatched TaskMessage.Metadata, which the agentic-loop fills onto
	// every spawned ToolCall.Metadata — the rule-side authoring surface
	// for flow-specific domain context the agent's tools key off (e.g.
	// `deliverable_type`). Framework-reserved `agent.*` keys cannot be set
	// this way (they are skipped with a Warn). gh#354.
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
	// keys are exact static lower-kebab predicate segments (maximum 64 bytes);
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
	// write `"researcher": "$entity.triple.agent.loop.parent"` and
	// the resolved loop ID flows through. Substitution is NOT
	// applied to keys, and keys are never normalized.
	//
	// Empty/nil leaves no lineage threaded (back-compat: pre-existing
	// flows that don't opt in see no Metadata change).
	RelatedLoops map[string]string `json:"related_loops,omitempty"`

	// FilesystemPolicy is the spawned loop's task-scoped read-only execution
	// policy (ADR-067, gh#443/gh#445). When non-empty, executePublishAgent
	// stamps it onto TaskMessage.Metadata under MetadataKeyFilesystemPolicy;
	// dispatch propagates it authoritatively onto every ToolCall.Metadata, and
	// the bash executor enforces a pre+post git worktree-and-HEAD non-mutation
	// proof (returning a typed violation) when the value is "read_only". This is
	// the rule-authoring surface for the framework enforcement fact — the same
	// framework-owned path as action_allowlist / related_loops, NOT product
	// domain metadata (which `properties` skips for reserved agent.* keys).
	//
	// Valid values (the framework filesystem enum, agentic.IsKnownFilesystemPolicy):
	// "read_only" (enforce) | "workspace_write" (default, permissive) | "host_write"
	// (also permissive here — an environment-level concern the sandbox substrate
	// owns; no v1 enforcement effect at the rule layer). Validated at config-load
	// time (validateActionLists); an unrecognized value fails load. Empty leaves
	// the loop at workspace_write (back-compat). Per-task, NOT inherited by
	// spawned sub-loops — re-declare on each spawned inspect child.
	FilesystemPolicy string `json:"filesystem_policy,omitempty"`

	// ScratchPaths are IN-WORKTREE paths exempt from the read_only proof (e.g.
	// ".probe/"). Out-of-worktree paths (/tmp) are exempt automatically. When
	// non-empty, executePublishAgent stamps them onto TaskMessage.Metadata under
	// MetadataKeyScratchPaths (as []any for JSON-wire shape, like
	// action_allowlist). Only meaningful with FilesystemPolicy "read_only".
	// Static config paths — no variable substitution. Empty means only
	// out-of-worktree paths are writable.
	ScratchPaths []string `json:"scratch_paths,omitempty"`

	// RunScope controls agent-run lifecycle management for publish_agent actions (ADR-053 D4).
	// Three values:
	//   - "new"     — mint a new AgentRun rooted at the FIRING loop. The spawned task carries
	//                 the firing loop's ID as its RunID; the framework mints the run entity
	//                 (idempotent). Use on the coordinator's initial dispatch action.
	//   - "inherit" — propagate the firing loop's existing agent.loop.run triple to the spawned task.
	//                 Default when the firing entity already carries a run; preserves the
	//                 existing run for child loop spawns within the same arc.
	//   - "none"    — do NOT propagate RunID. Use to suppress run association on standalone
	//                 loops (CLI-chat, HTTP dispatch) that should not mint runs.
	//   - ""        — same as "inherit" (backward-compatible default).
	//
	// Validated at rule-load time; invalid values return ErrInvalidRunScope.
	RunScope string `json:"run_scope,omitempty"`

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

	// LoopMaxIterations is the SPAWNED LOOP's iteration budget (gh#528) —
	// entirely distinct from MaxIterations above, which caps how many
	// times THIS ACTION may fire per rule+entity match-cycle. Do not
	// confuse the two: MaxIterations bounds the rule engine's repeated
	// firing of this action; LoopMaxIterations bounds the agentic-loop's
	// iteration count inside the agent it spawns.
	//
	// A string (not *int) because it supports variable substitution —
	// authors can write a literal ("3") or a triple reference
	// (e.g. "$entity.triple.task.spec.budget") so a human-approved or
	// upstream-computed budget can bound the spawned loop. Empty leaves
	// TaskMessage.MaxIterations unset (nil), so the spawned loop falls
	// back to the agentic-loop component's configured default — current
	// behaviour, unchanged for every rule that doesn't opt in.
	//
	// After substitution the value MUST parse as a positive integer
	// (>= 1); a substituted value that doesn't (e.g. "unbounded", or an
	// unresolved template) fails the publish_agent action with a
	// classified error and does not publish a task — never a silent
	// skip. The agentic-loop clamps the effective budget to
	// min(LoopMaxIterations, component ceiling): a spawn may narrow the
	// operator's configured ceiling, never widen past it.
	LoopMaxIterations string `json:"loop_max_iterations,omitempty" description:"Iteration budget for the SPAWNED LOOP (agentic-loop's per-iteration cap on the agent this action spawns) — distinct from the action-level firing cap 'max_iterations' above, which bounds how many times this action itself fires. Supports variable substitution (literal or $entity.triple.* reference); must resolve to a positive integer or the action fails."`

	// Reason is the human-readable verdict message for deny AND approve
	// actions, and the failure cause for lifecycle_fail. For deny it travels
	// with the *DenyVerdict error so callers can record it without a
	// side-channel lookup; for approve it lands in the audit triple and in
	// the verdict payload published to Subject; for lifecycle_fail it travels
	// as the TransitionEvent.Note for audit (Manager.Fail rejects empty
	// reasons — operators need the failure cause in the trail).
	// Variable substitution is applied.
	Reason string `json:"reason,omitempty"`

	// Workflow names the lifecycle workflow this action targets, for the
	// lifecycle_* action family (ADR-047). When empty, the action resolves
	// the workflow by scanning Manager registrations for the trigger
	// entity's ID (O(workflows × bucket-size) per call — fine for the
	// rule-fire path but worth being explicit when authors know the type).
	// Required when the trigger entity's lifecycle workflow is ambiguous
	// across registrations.
	Workflow string `json:"workflow,omitempty"`

	// Phase is the target phase for lifecycle_transition. Must be declared
	// in the registered Transitions table for Workflow. Variable
	// substitution is applied so authors can write
	// `"phase": "$message.next_phase"` when the target is data-driven.
	Phase string `json:"phase,omitempty"`

	// Set is an optional map of field-name → value-or-typed-op applied
	// atomically with the lifecycle_transition phase change. Each entry is
	// either a literal (string / number / bool — set as-is) or a typed
	// operation:
	//
	//   {"op": "set", "value": <v>}   — equivalent to a bare literal v
	//   {"op": "increment"}            — int/uint/float field += 1
	//   {"op": "decrement"}            — int/uint/float field -= 1
	//
	// Field names match JSON tags on the Participant struct (the same
	// surface UpdateFromOperator's patch keys use). Unknown fields surface
	// as authoring errors. Set runs inside the same Manager.Update closure
	// as the phase write — failures abort the whole transition, no partial
	// state lands.
	Set map[string]any `json:"set,omitempty"`

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

// VerdictAuditor records a governance deny/approve verdict to the append-only
// GOVERNANCE_VERDICT_AUDIT stream (ADR-055 §3a). It is a FRAMEWORK-owned
// dependency, distinct from the operator-configured routing Publisher: piggy-
// backing the Publisher would let config drift silently disable the audit trail,
// so the audit emit has its own always-wired path. It REPLACES the prior
// rule-ID audit triple (which rode the graph-ingest auto-vivify path ADR-055
// deletes). The emit is best-effort — the caller must NOT flip a structural
// verdict on an emit error — and increments a failure metric so a lost audit
// record is observable rather than silent.
type VerdictAuditor interface {
	// EmitVerdict publishes a verdict event. Returns an error on emit failure;
	// callers treat the error as a (metered, logged) audit gap, never a verdict
	// change.
	EmitVerdict(ctx context.Context, ev governance.VerdictEvent) error
}

// LifecycleManager is the subset of *lifecycle.Manager used by the
// rule action executor for the lifecycle_* action family (ADR-047) and
// the agent-run mint path (ADR-053 D4).
// Defined as an interface so the executor can be tested without a
// running NATS client.
type LifecycleManager interface {
	TransitionWith(ctx context.Context, workflow, entityID, newPhase string, source lifecycle.TransitionSource, note string, mutator func(lifecycle.Participant) error) error
	Complete(ctx context.Context, workflow, entityID string) error
	Fail(ctx context.Context, workflow, entityID, reason string) error
	LookupByEntityID(ctx context.Context, entityID string) (lifecycle.Participant, error)
	GetWorkflowDefinition(workflow string) (lifecycle.WorkflowDef, error)
	// AssertRuleWritable enforces the rule-vs-operator convergence
	// (ADR-047): lifecycle_transition's `set` clause must respect the
	// same default-deny as UpdateFromOperator. Identity, phase, and
	// non-operator-writable fields are protected.
	AssertRuleWritable(workflow, fieldJSONName string) error
	// Get reads the entity at entityID for the given workflow. Used by the
	// RunScope "new" path to read back a run after idempotent mint (ADR-053 D4).
	Get(ctx context.Context, workflow, entityID string) (lifecycle.Participant, error)
	// Create attaches lifecycle to the entity at initial.EntityID(). Used by the
	// RunScope "new" path to mint an AgentRun (ADR-053 D4). Returns
	// lifecycle.ErrAlreadyExists when already lifecycle-managed.
	Create(ctx context.Context, initial lifecycle.Participant) error
	// Note: Transition is NOT part of the rule LifecycleManager interface. The
	// agent-run subscriber is observation-only and never mutates lifecycle state.
	// A coordinator or component that needs a terminal transition emits the
	// declared lifecycle_transition action through its graph-mutation port.
	// Keeping the interfaces separate prevents an observer from becoming a writer.
}

// ActionExecutor executes actions for rules.
// It handles triple mutations, NATS publishing, KV writes, and other action types.
type ActionExecutor struct {
	logger        *slog.Logger
	tripleMutator TripleMutator                // Optional: if nil, triple mutations are logged but not persisted
	publisher     Publisher                    // Optional: if nil, publish actions are logged but not sent
	kvWriter      KVWriter                     // Optional: if nil, update_kv actions are logged but not executed
	lifecycle     LifecycleManager             // Optional: if nil, lifecycle_* actions return an error explaining no Manager is wired
	toolRegistry  component.ToolRegistryReader // Optional: if nil, publish_agent default_tools resolution returns empty
	// verdictAuditor records governance deny/approve verdicts to the append-only
	// audit stream (ADR-055 §3a). Optional: if nil, verdicts are still applied
	// and logged but no audit event is emitted (e.g. NATS-less test executors).
	verdictAuditor VerdictAuditor
	reconciler     projection.PredicateReconciler
	targetIndex    *projectionTargetIndex
	revisionWriter revisionTracker
}

// SetToolRegistry installs the shared tool registry used by
// resolveToolNames during publish_agent action execution. nil-valued
// arg disables tool name resolution (tools list passed to the agent
// is left empty). Set explicitly after construction by the rule
// processor when it has access to deps.ToolRegistry.
func (e *ActionExecutor) SetToolRegistry(r component.ToolRegistryReader) {
	e.toolRegistry = r
}

// SetLifecycleManager installs the Lifecycle harness Manager used by
// the lifecycle_* action family. nil-valued arg disables the actions
// (executor returns an error for lifecycle_* dispatches). Set
// explicitly after construction by the rule processor when it has
// access to the registered Manager.
func (e *ActionExecutor) SetLifecycleManager(m LifecycleManager) {
	e.lifecycle = m
}

// SetVerdictAuditor installs the framework verdict auditor used by deny/approve
// actions to record governance verdicts (ADR-055 §3a). nil-valued arg leaves
// verdicts un-audited (still applied + logged). Set explicitly after
// construction by the rule processor once it has a NATS client. It is wired
// independently of the operator Publisher so config drift cannot disable audit.
func (e *ActionExecutor) SetVerdictAuditor(a VerdictAuditor) {
	e.verdictAuditor = a
}

// SetPredicateReconciler installs the projection reconcile capability.
func (e *ActionExecutor) SetPredicateReconciler(reconciler projection.PredicateReconciler) {
	e.reconciler = reconciler
}

func (e *ActionExecutor) setProjectionTargets(index *projectionTargetIndex, tracker revisionTracker) {
	e.targetIndex = index
	e.revisionWriter = tracker
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
	case ActionTypeReconcilePredicates:
		return e.executeReconcilePredicates(ctx, action, ec)
	case ActionTypePublishAgent:
		return e.executePublishAgent(ctx, action, ec)
	case ActionTypeUpdateKV:
		return e.executeUpdateKV(ctx, action, ec)
	case ActionTypeDeny:
		return e.executeDeny(ctx, action, ec)
	case ActionTypeApprove:
		return e.executeApprove(ctx, action, ec)
	case ActionTypeLifecycleTransition:
		return e.executeLifecycleTransition(ctx, action, ec)
	case ActionTypeLifecycleComplete:
		return e.executeLifecycleComplete(ctx, action, ec)
	case ActionTypeLifecycleFail:
		return e.executeLifecycleFail(ctx, action, ec)
	default:
		return fmt.Errorf("unknown action type: %s", action.Type)
	}
}

// resolveTripleSubject returns the entity ID a triple-write action
// (add_triple / update_triple / remove_triple) should target. #147 /
// ADR-046 Phase 1 join gap: lets a rule stamp a triple onto an
// entity other than its trigger (e.g. stamping a per-child completion
// counter onto the parent loop entity from a child-completion rule).
//
// Semantics:
//   - Empty action.Subject → fall back to ec.EntityID (back-compat).
//   - Non-empty action.Subject → substitution-resolved.
//   - Substitution-resolved to an empty string → authoring error.
//     Silent fallback to ec.EntityID would mask typos in
//     $entity.triple.* references — same loudness discipline as
//     PR #138's for_each resolver.
//
// Phase-2 footgun note: the unresolved-token check uses
// `unresolvedTemplateVarRe`, which only catches framework-namespace
// tokens ($entity|related|state|schedule|caller|message). Iter-vars
// from `for_each` ($subtopic, $node, etc.) are NOT in the regex
// because today `for_each` is only consumed by `publish_agent`. If
// ADR-046 Phase 2 (#139) extends `for_each` to triple-writes, this
// helper would silently accept a bare iter-var literal that didn't
// resolve and write a triple to an entity literally named
// "$subtopic". Either broaden the regex when that lands or have the
// for_each-on-triple-writes path validate iter-var presence
// upstream.
func resolveTripleSubject(action Action, ec *ExecutionContext) (string, error) {
	if action.Subject == "" {
		return ec.EntityID, nil
	}
	resolved := ec.SubstituteVariables(action.Subject)
	if resolved == "" {
		return "", fmt.Errorf("action.subject %q resolved to empty after substitution; refusing to fall back to trigger entity (likely a typo or a $entity.triple.* reference that the trigger entity didn't carry at fire time)", action.Subject)
	}
	// SubstituteVariables leaves unresolved $entity.*/$related.*/etc.
	// tokens in the output verbatim (logs a Warn but doesn't error).
	// Treat surviving tokens as resolution failure — falling back to
	// ec.EntityID would mask the typo + writing the triple onto an
	// entity whose ID literally contains "$entity.triple.foo" is
	// strictly worse than refusing. Same loudness discipline as the
	// for_each resolver in PR #138.
	if leftovers := unresolvedTemplateVarRe.FindAllString(resolved, -1); len(leftovers) > 0 {
		return "", fmt.Errorf("action.subject %q has unresolved template variables %v after substitution (likely a typo or a reference that the trigger entity didn't carry at fire time); refusing to write triple with garbled subject", action.Subject, leftovers)
	}
	return resolved, nil
}

// ExecuteAddTriple executes an add_triple action, creating a new semantic triple.
// Returns the created triple and any error that occurred.
// If a TripleMutator is configured, the triple is persisted via NATS request/response.
func (e *ActionExecutor) ExecuteAddTriple(ctx context.Context, action Action, ec *ExecutionContext) (message.Triple, error) {
	entityID, err := resolveTripleSubject(action, ec)
	if err != nil {
		return message.Triple{}, fmt.Errorf("resolve add_triple subject: %w", err)
	}

	// Validate predicate is present
	if action.Predicate == "" {
		return message.Triple{}, errors.New("predicate is required for add_triple action")
	}

	// Substitute variables in predicate (always string — Action.Predicate
	// is a name, not a value). For Object, attempt typed single-token
	// resolution first (gh#207): when action.Object is exactly one
	// supported substitution token, propagate the source type
	// unchanged (float64 / bool / int / string) so numeric upserts
	// stay type-faithful. Fall back to string substitution for mixed
	// templates, literal strings, and unrecognized tokens — those
	// require string-concat semantics, and the destination Triple.Object
	// being `any` accepts string Objects without loss.
	predicate := ec.SubstituteVariables(action.Predicate)
	var object any
	if typed, ok := ec.SubstituteVariablesTyped(action.Object); ok {
		object = typed
	} else {
		object = ec.SubstituteVariables(action.Object)
	}

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
	entityID, err := resolveTripleSubject(action, ec)
	if err != nil {
		return fmt.Errorf("resolve remove_triple subject: %w", err)
	}

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
	entityID, err := resolveTripleSubject(action, ec)
	if err != nil {
		return fmt.Errorf("resolve update_triple subject: %w", err)
	}

	// Validate predicate is present
	if action.Predicate == "" {
		return errors.New("predicate is required for update_triple action")
	}

	// Predicate substitution is always string (it's a name). For Object,
	// attempt typed single-token resolution first so numeric upserts
	// round-trip the source type (gh#207). String fallback covers
	// literal Objects, mixed templates, and unrecognized tokens. Same
	// dispatch shape as ExecuteAddTriple — both actions land their
	// Object in a `message.Triple{Object: any}`, so symmetry is correct.
	predicate := ec.SubstituteVariables(action.Predicate)
	var object any
	if typed, ok := ec.SubstituteVariablesTyped(action.Object); ok {
		object = typed
	} else {
		object = ec.SubstituteVariables(action.Object)
	}

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

// executeReconcilePredicates executes a reconcile_predicates action
// through the rule pack's bound public mutation client. The client reconciles
// the complete selected projection group atomically.
//
// Routing decision is made on the RAW action.Object BEFORE substitution
// (per ADR-056 Decision 3): an empty Object clears the entire selected named
// group; a non-empty Object supplies one desired triple. This keeps the
// clear-vs-replace branch independent of what substitution resolves to — a
// non-empty expression that resolves to "" still authors that empty value.
//
// Predicate is always a literal (validation rejects any `$` in the predicate of
// a reconcile_predicates action), so it is NOT run through substitution. Object IS
// substituted through the same typed dispatch as add_triple / update_triple so
// numeric / bool values round-trip their source type. Ownership identity and
// fencing remain encapsulated by projection.MutationClient.
func (e *ActionExecutor) executeReconcilePredicates(ctx context.Context, action Action, ec *ExecutionContext) error {
	entityID, err := resolveTripleSubject(action, ec)
	if err != nil {
		return fmt.Errorf("resolve reconcile subject: %w", err)
	}

	target, err := e.targetIndex.resolve(
		action.ProjectionContract,
		action.ProjectionGroup,
		action.Predicate,
	)
	if err != nil {
		return fmt.Errorf("resolve reconcile target: %w", err)
	}
	if e.reconciler == nil {
		return errors.New("reconcile_predicates action requires a predicate reconciler")
	}

	// Clear vs replace is decided on the RAW Object before substitution.
	// Empty clears the entire selected group; non-empty authors one desired
	// triple even when substitution resolves to an empty value.
	var objects []message.Triple
	timestamp := time.Now().UTC()
	if action.Object != "" {
		var object any
		if typed, ok := ec.SubstituteVariablesTyped(action.Object); ok {
			object = typed
		} else {
			object = ec.SubstituteVariables(action.Object)
		}

		ttl, err := action.ParseTTL()
		if err != nil {
			return fmt.Errorf("parse TTL: %w", err)
		}
		var expiresAt *time.Time
		if ttl > 0 {
			expTime := time.Now().Add(ttl)
			expiresAt = &expTime
		}

		objects = []message.Triple{{
			Subject:    entityID,
			Predicate:  action.Predicate,
			Object:     object,
			Source:     "rule_engine",
			Timestamp:  timestamp,
			Confidence: 1.0,
			ExpiresAt:  expiresAt,
		}}
	}

	if e.logger != nil {
		e.logger.Debug("Reconciling predicate group",
			"entity_id", entityID,
			"predicate", action.Predicate,
			"projection_contract", target.Contract,
			"projection_group", target.Group,
			"clear", len(objects) == 0)
	}

	ruleID := ec.RuleID()
	iteration := 0
	if ec != nil && ec.State != nil {
		iteration = ec.State.Iteration
	}
	requestID := fmt.Sprintf(
		"rule:%s:%s:%d:%s",
		ruleID,
		entityID,
		iteration,
		action.effectiveID(ruleID),
	)
	receipt, err := e.reconciler.Reconcile(ctx, projection.ReconcileMutation{
		Contract: target.Contract,
		Group:    target.Group,
		EntityID: entityID,
		Desired:  objects,
		Metadata: projection.MutationMetadata{
			RequestID: requestID,
			TraceID:   ruleID,
			Source:    "rule_engine",
			Timestamp: timestamp,
		},
	})
	if err != nil {
		return fmt.Errorf(
			"reconcile contract %q group %q predicate %q on %s: %w",
			target.Contract,
			target.Group,
			action.Predicate,
			entityID,
			err,
		)
	}
	if e.revisionWriter != nil && receipt.KVRevision > 0 && ruleID != "" {
		e.revisionWriter.trackRuleRevision(ruleID, entityID, receipt.KVRevision)
	}
	if e.logger != nil {
		e.logger.Debug("Owned predicate replaced",
			"entity_id", entityID,
			"predicate", action.Predicate,
			"projection_contract", target.Contract,
			"projection_group", target.Group,
			"kv_revision", receipt.KVRevision)
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
// authors can declare `"researcher": "$entity.triple.agent.loop.parent"`
// and the resolved loop ID flows through, with the for_each iter-var
// (ADR-046 Phase 1) also bound when set so authors can thread the
// current item into a related-loop value. Keys are exact static lower-kebab
// predicate segments (maximum 64 bytes); they are not substituted or
// normalized.
// String-to-string by design — see agentic.MetadataKeyRelatedLoops.
// Empty/nil RelatedLoops is a no-op (back-compat: pre-existing flows
// that don't opt in see no Metadata change).
func stampRelatedLoops(task *agentic.TaskMessage, related map[string]string, ec *ExecutionContext, iterVarName, iterVarValue string) error {
	if len(related) == 0 {
		return nil
	}
	resolved := make(map[string]any, len(related))
	for label, loopID := range related {
		if _, err := agentic.LineageTriplePredicate(label); err != nil {
			return fmt.Errorf("related_loops role key %q: %w", label, err)
		}
		resolvedLoopID := ec.SubstituteVariablesWithIterVar(loopID, iterVarName, iterVarValue)
		if resolvedLoopID == "" {
			return fmt.Errorf("related_loops role key %q resolved to an empty loop ID", label)
		}
		resolved[label] = resolvedLoopID
	}
	if task.Metadata == nil {
		task.Metadata = map[string]any{}
	}
	task.Metadata[agentic.MetadataKeyRelatedLoops] = resolved
	return nil
}

// isReservedTaskMetadataKey reports whether a key on TaskMessage.Metadata
// is framework-owned and therefore must not be set by author-supplied
// publish_agent `properties` (gh#354). Every framework key the rule
// engine and agentic-loop stamp onto task metadata lives under the
// `agent.` namespace — decide allowlist (agent.decide.action_allowlist),
// cross-arc loop lineage (agent.related_loops), run association
// (agent.run_id / agent.run_entity_id). Reserving the whole namespace
// keeps the guard forward-compatible as new framework keys are added and
// keeps domain keys (deliverable_type, subtopic, …) — which never use the
// `agent.` prefix — free to flow through.
// Load-bearing invariant: every framework-written task.Metadata key
// lives under `agent.`; a future framework key outside that namespace
// would have to be added to this reservation explicitly.
func isReservedTaskMetadataKey(key string) bool {
	return strings.HasPrefix(key, "agent.")
}

// stampAuthorMetadata carries author-supplied domain metadata
// (action.Properties) onto the TaskMessage so it reaches every spawned
// ToolCall.Metadata. The agentic-loop caches task.Metadata at loop start
// and fills it onto each approved call with no-clobber semantics
// (handlers.go), so whatever a dispatcher attaches to the task reaches
// the agent's tools. This is the rule-side authoring surface for
// "flow-specific context the dispatcher attached to the task" (gh#354):
// component-dispatched agents set task.Metadata directly in Go; rule
// authors set it via `properties`, mirroring executePublish which already
// carries substituted properties as its emitted payload's metadata.
//
// Called BEFORE the framework-reserved writes (decide allowlist,
// related-loops) so those are authoritative; reserved `agent.*` keys are
// additionally skipped here (with a Warn) so an author cannot inject them
// even on an iteration where the framework doesn't write them. String
// values are substituted iter-var-aware (like stampRelatedLoops) so
// for_each dispatches can vary metadata per item; non-strings pass
// through unchanged (shallow-only). Empty/nil leaves task.Metadata
// untouched — opt-in, so non-using flows see no change.
func (e *ActionExecutor) stampAuthorMetadata(task *agentic.TaskMessage, action Action, ec *ExecutionContext, iterVarName, iterVarValue string) {
	for k, v := range action.Properties {
		if isReservedTaskMetadataKey(k) {
			if e.logger != nil {
				e.logger.Warn("publish_agent: ignoring reserved framework metadata key in properties",
					slog.String("key", k),
					slog.String("rule_id", ec.RuleID()),
					slog.String("entity_id", ec.EntityID))
			}
			continue
		}
		if task.Metadata == nil {
			task.Metadata = map[string]any{}
		}
		if s, ok := v.(string); ok {
			task.Metadata[k] = ec.SubstituteVariablesWithIterVar(s, iterVarName, iterVarValue)
		} else {
			task.Metadata[k] = v
		}
	}
}

// stampFilesystemPolicy threads the rule.Action's read-only execution policy
// (ADR-067) onto the TaskMessage.Metadata under MetadataKeyFilesystemPolicy /
// MetadataKeyScratchPaths. Mirrors the ActionAllowlist stamping pattern:
// scratch paths are stored as []any so the JSON round-trip through
// TaskMessage→agent.task→ToolCall.Metadata preserves shape (the bash executor's
// FilesystemPolicyFromMetadata coerces back). Empty policy AND empty scratch is
// a no-op (back-compat). The enum is validated at config-load time
// (validateActionLists), so a bad value never reaches here.
func stampFilesystemPolicy(task *agentic.TaskMessage, action Action) {
	if action.FilesystemPolicy == "" && len(action.ScratchPaths) == 0 {
		return
	}
	if task.Metadata == nil {
		task.Metadata = map[string]any{}
	}
	if action.FilesystemPolicy != "" {
		task.Metadata[agentic.MetadataKeyFilesystemPolicy] = action.FilesystemPolicy
	}
	if len(action.ScratchPaths) > 0 {
		scratch := make([]any, 0, len(action.ScratchPaths))
		for _, p := range action.ScratchPaths {
			scratch = append(scratch, p)
		}
		task.Metadata[agentic.MetadataKeyScratchPaths] = scratch
	}
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

// stampLoopMaxIterations resolves action.LoopMaxIterations (a
// substitutable string — literal or a $entity.triple.* reference, e.g. a
// human-approved task.spec.<i>.budget) and stamps it onto
// TaskMessage.MaxIterations as the spawned loop's per-spawn iteration
// budget (gh#528). Deliberately distinct from Action.MaxIterations, the
// rule engine's own action-firing cap — see the field doc on Action.
//
// Empty action.LoopMaxIterations is a no-op: task.MaxIterations stays
// nil, so the spawned loop falls back to the agentic-loop component's
// configured default (current behaviour, unchanged for every rule that
// doesn't opt in).
//
// A non-empty value that does not resolve to a positive integer after
// substitution (an authoring error — a typo, an unresolved template, or
// a triple carrying non-numeric text) returns a loud error instead of
// silently skipping the field or falling back to the default budget;
// the caller aborts the publish entirely (gh#529 loudness discipline,
// matching stampRelatedLoops's role-key validation).
func stampLoopMaxIterations(task *agentic.TaskMessage, action Action, ec *ExecutionContext, iterVarName, iterVarValue string) error {
	if action.LoopMaxIterations == "" {
		return nil
	}
	resolved := strings.TrimSpace(ec.SubstituteVariablesWithIterVar(action.LoopMaxIterations, iterVarName, iterVarValue))
	n, convErr := strconv.Atoi(resolved)
	if convErr != nil || n < 1 {
		return fmt.Errorf("loop_max_iterations %q resolved to %q, which is not a positive integer", action.LoopMaxIterations, resolved)
	}
	task.MaxIterations = &n
	return nil
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

	// Substitute variables in subject, prompt, and role — iter-var
	// overlay applies to all substituted strings on this iteration.
	// Role substitution enables continuation patterns (ADR-045 R6)
	// where the spawned task's role comes from a triple on the
	// triggering entity (e.g., `$entity.triple.research.parent.role`).
	// Existing rule packs that hardcode role values are unaffected —
	// strings without `$`-prefixed tokens pass through unchanged.
	subject := ec.SubstituteVariablesWithIterVar(action.Subject, iterVarName, iterVarValue)
	prompt := ec.SubstituteVariablesWithIterVar(action.Prompt, iterVarName, iterVarValue)
	role := ec.SubstituteVariablesWithIterVar(action.Role, iterVarName, iterVarValue)

	// Generate a unique task ID
	taskID := fmt.Sprintf("rule-%s-%d", entityID, time.Now().UnixNano())

	// Build the TaskMessage
	task := agentic.TaskMessage{
		TaskID:       taskID,
		Role:         role,
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

	// RunScope controls agent-run lifecycle management (ADR-053 D4, Pass B).
	//
	//   "new" (or "") when the trigger entity IS a loop-execution entity:
	//     Mint a new AgentRun rooted at the firing loop. The firing loop's
	//     loop-id becomes the run-id. task.RunID is set to that loop-id so
	//     the spawned child inherits the run. If the trigger entity is NOT a
	//     loop-execution entity, "new" is treated as "inherit" with a warning.
	//
	//   "inherit" or "":
	//     Default Pass A behavior — propagate the agent.loop.run triple from the
	//     firing loop entity to the spawned TaskMessage (the child belongs to
	//     the parent's run). Non-loop trigger entities with no agent.loop.run triple
	//     produce no inheritance (RunID stays empty).
	//
	//   "none":
	//     Suppress RunID propagation entirely. The spawned loop has no run
	//     association. Used for standalone fire-and-forget dispatches.
	var pendingRunMint *struct {
		org          string
		platform     string
		firingLoopID string
	}
	switch action.RunScope {
	case "new":
		// Mint a new AgentRun rooted at the firing loop entity.
		// The firing loop's loop-id is the run-id.
		firingLoopID, isLoop := agentic.LoopIDFromExecutionEntityID(entityID)
		if !isLoop {
			// Trigger entity is not a loop-execution entity — cannot mint a run.
			// Fall through to inherit behavior with a warning.
			if e.logger != nil {
				e.logger.Warn("publish_agent: run_scope=new on non-loop trigger entity — falling back to inherit",
					slog.String("entity_id", entityID),
					slog.String("rule_id", ec.RuleID()))
			}
			// Inherit fallthrough:
			if ec != nil && ec.Entity != nil {
				if runIDVal, ok := ec.Entity.GetPropertyValue(agvocab.LoopRun); ok {
					if runID, ok := runIDVal.(string); ok && runID != "" {
						task.RunID = runID
					}
				}
			}
		} else if e.lifecycle != nil {
			// Parse org and platform from the 6-part entity ID.
			// IsValidEntityID guarantees exactly 6 dot-separated parts; the firing
			// entity has already passed through ec which validates entity IDs.
			idParts := strings.SplitN(entityID, ".", 6)
			if len(idParts) == 6 {
				org, platform := idParts[0], idParts[1]
				task.RunID = firingLoopID
				pendingRunMint = &struct {
					org          string
					platform     string
					firingLoopID string
				}{org: org, platform: platform, firingLoopID: firingLoopID}
			}
		} else {
			// No lifecycle manager wired — log and fall through to inherit.
			if e.logger != nil {
				e.logger.Warn("publish_agent: run_scope=new but no lifecycle manager wired — falling back to inherit",
					slog.String("entity_id", entityID),
					slog.String("rule_id", ec.RuleID()))
			}
			if ec != nil && ec.Entity != nil {
				if runIDVal, ok := ec.Entity.GetPropertyValue(agvocab.LoopRun); ok {
					if runID, ok := runIDVal.(string); ok && runID != "" {
						task.RunID = runID
					}
				}
			}
		}

	case "none":
		// Suppress RunID propagation — no run association on the spawned loop.

	default: // "inherit" or ""
		// Pass A default: propagate the agent.loop.run triple from the firing entity.
		// Non-loop trigger entities that have no agent.loop.run triple produce no
		// inheritance (RunID stays empty), which is correct.
		if ec != nil && ec.Entity != nil {
			if runIDVal, ok := ec.Entity.GetPropertyValue(agvocab.LoopRun); ok {
				if runID, ok := runIDVal.(string); ok && runID != "" {
					task.RunID = runID
				}
			}
		}
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

	// Author-supplied domain metadata (gh#354). Stamped first so the
	// framework-reserved writes below (decide allowlist, related-loops)
	// remain authoritative; reserved `agent.*` keys are skipped inside
	// the helper. This is the rule-side path to TaskMessage.Metadata that
	// component-dispatched agents reach directly in Go.
	e.stampAuthorMetadata(&task, action, ec, iterVarName, iterVarValue)

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
	if err := stampRelatedLoops(&task, action.RelatedLoops, ec, iterVarName, iterVarValue); err != nil {
		return errs.WrapInvalid(err, "RuleActionExecutor", "publishAgentOnce", "validate substituted related_loops")
	}

	// Per-spawn read-only execution policy (ADR-067, gh#445). Same
	// framework-owned Metadata path; dispatch propagates it authoritatively.
	stampFilesystemPolicy(&task, action)

	// Per-spawn loop iteration budget (gh#528). Must run — and fail loud on
	// a bad substitution — before task.Validate() below, which also
	// enforces the >= 1 floor as defense-in-depth for any other caller of
	// TaskMessage.Validate.
	if err := stampLoopMaxIterations(&task, action, ec, iterVarName, iterVarValue); err != nil {
		return errs.WrapInvalid(err, "RuleActionExecutor", "publishAgentOnce", "validate substituted loop_max_iterations")
	}
	if err := task.Validate(); err != nil {
		return errs.WrapInvalid(err, "RuleActionExecutor", "publishAgentOnce", "validate substituted task")
	}

	// run_scope=new is intentionally committed only after the complete,
	// substituted TaskMessage has passed validation. Mint and graph writes are
	// externally visible; an invalid lineage map must produce none of them.
	if pendingRunMint != nil {
		if _, mintErr := agentrun.Mint(ctx, e.lifecycle, pendingRunMint.org, pendingRunMint.platform, pendingRunMint.firingLoopID); mintErr != nil {
			// Mint failure is logged but does not abort dispatch. Remove the
			// prospective run association before publication because no run exists.
			task.RunID = ""
			if e.logger != nil {
				e.logger.Error("publish_agent: agentrun.Mint failed — spawning without run association",
					slog.String("entity_id", entityID),
					slog.String("firing_loop_id", pendingRunMint.firingLoopID),
					slog.String("rule_id", ec.RuleID()),
					slog.Any("error", mintErr))
			}
		} else if e.tripleMutator != nil {
			stampRun := func(predicate, object string) {
				if _, tripleErr := e.tripleMutator.AddTriple(ctx, ec.RuleID(), message.Triple{
					Subject: entityID, Predicate: predicate, Object: object,
					Source: "rule_engine", Timestamp: time.Now(), Confidence: 1.0,
				}); tripleErr != nil && e.logger != nil {
					e.logger.Warn("publish_agent: run_scope=new: failed to stamp run anchor on firing entity",
						slog.String("entity_id", entityID),
						slog.String("predicate", predicate),
						slog.String("firing_loop_id", pendingRunMint.firingLoopID),
						slog.String("rule_id", ec.RuleID()),
						slog.Any("error", tripleErr))
				}
			}
			stampRun(agvocab.LoopRun, pendingRunMint.firingLoopID)
			if runEntityID, idErr := agentic.TryChainExecutionEntityID(
				pendingRunMint.org, pendingRunMint.platform, pendingRunMint.firingLoopID); idErr == nil {
				stampRun(agvocab.LoopRunEntityID, runEntityID)
			}
		}
	}

	if e.logger != nil {
		e.logger.Debug("Triggering agent task",
			"subject", subject,
			"task_id", taskID,
			"role", role,
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
			Predicate:  "rule.task.spawned",
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

	// Best-effort governance audit (ADR-055 §3a): emit a registered verdict event
	// to the append-only GOVERNANCE_VERDICT_AUDIT stream, replacing the prior
	// rule-ID audit triple that rode graph-ingest's auto-vivify path. An emit
	// failure is logged + metered but MUST NOT flip the verdict from deny to allow.
	e.emitVerdictAudit(ctx, governance.DecisionDeny, ruleID, reason, ec)

	return &DenyVerdict{RuleID: ruleID, Reason: reason}
}

// emitVerdictAudit records a governance verdict to the append-only audit stream
// (ADR-055 §3a). Best-effort by construction: a nil auditor (NATS-less executor)
// is a no-op, and an emit error is logged at Error level so operators see the
// audit gap — but the caller's structural verdict is NEVER changed by an audit
// failure. The auditor meters failures via governance_verdict_audit_failures_total.
func (e *ActionExecutor) emitVerdictAudit(ctx context.Context, decision, ruleID, reason string, ec *ExecutionContext) {
	if e.verdictAuditor == nil {
		return
	}
	ev := governance.VerdictEvent{
		Decision:  decision,
		RuleID:    ruleID,
		Reason:    reason,
		EntityID:  ec.EntityID,
		Timestamp: time.Now(),
	}
	// loop_id/call_id are OPTIONAL — echoed from the proposed-call message when
	// the verdict fires on a tool-call. Nil MessageData (entity-state-driven or
	// cron-fired rules) leaves them empty; the audit record stays valid.
	if ec.MessageData != nil {
		if v, ok := ec.MessageData["loop_id"].(string); ok {
			ev.LoopID = v
		}
		if v, ok := ec.MessageData["call_id"].(string); ok {
			ev.CallID = v
		}
	}
	if err := e.verdictAuditor.EmitVerdict(ctx, ev); err != nil && e.logger != nil {
		e.logger.Error("governance verdict audit emit failed; verdict still applies",
			"decision", decision,
			"rule_id", ruleID,
			"reason", reason,
			"error", err)
	}
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

	// Best-effort governance audit (ADR-055 §3a): same mechanism as deny — emit
	// a verdict event to the audit stream. Audit failure must NOT change the
	// verdict; approve still proceeds to the routing publish below so downstream
	// consumers see the decision.
	e.emitVerdictAudit(ctx, governance.DecisionApprove, ruleID, reason, ec)

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
	if gtypes.IsFrameworkOwnedBucket(bucket) {
		return fmt.Errorf("update_kv cannot write framework-owned graph bucket %q (owned by %s); use graph mutation APIs",
			bucket, gtypes.OwnerOf(bucket))
	}

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

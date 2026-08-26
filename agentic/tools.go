package agentic

import (
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/c360studio/semstreams/message"
	"github.com/c360studio/semstreams/vocabulary"
)

// ErrToolNotFound is the sentinel returned by tool registries when a
// requested tool name has no executor. Callers use errors.Is to detect
// the miss without parsing error strings — the previous string-match
// fallback in agentic-tools/component.go was the source of repeated
// extension friction. Lives here (alongside ToolCall/ToolResult)
// rather than in agentic-tools so consumers in other processor
// packages can check it without importing the executor package.
var ErrToolNotFound = errors.New("tool not found")

// ToolDefinition represents the definition of a tool that can be called
type ToolDefinition struct {
	Name        string         `json:"name"`
	Description string         `json:"description"`
	Parameters  map[string]any `json:"parameters"`

	// Strict enables OpenAI's strict-mode tool calling: the model is
	// constrained to emit tool_calls[].function.arguments that conform to
	// Parameters. Symmetric to ResponseFormat.Strict — same provider table
	// applies (see ADR-034): honored on OpenAI proper / vLLM / OpenRouter /
	// sparky / any OpenAI-compat runtime under provider:"openai"; silently
	// ignored on Anthropic and Gemini OpenAI-compat (adapters clear it +
	// Warn). Best-effort on Ollama /v1 (model-dependent — gemma3 ignores
	// per ADR-034 §gh#10001).
	//
	// Requires Parameters to satisfy OpenAI's strict-mode subset:
	// additionalProperties:false at every object level, every property
	// listed in required, no $ref/anyOf at the root, max nesting 5. A
	// non-conforming schema returns 400 from the upstream — caller bug,
	// not a framework concern.
	Strict bool `json:"strict,omitempty"`

	// Paginated declares that this tool supports continuation paging via
	// the agentic.MetadataKey{HasMore,NextOffset,NextCursor} contract.
	// When true, the executor MUST set MetadataKeyHasMore on every
	// successful result (bool false for last page, bool true with one
	// of NextOffset or NextCursor for intermediate pages). The agent
	// loop reads has_more in buildToolMessages and appends a canonical
	// continuation hint to the model's next message — telling the
	// model it can call the same tool again with the supplied
	// continuation token instead of having to re-narrow blind.
	//
	// Informational at the wire level today: the loop branches on the
	// actual has_more value in result metadata, not on this flag. Future
	// uses include operator introspection ("which tools paginate?") and
	// loop-side contract-violation warnings when has_more arrives from
	// a tool that didn't declare Paginated.
	Paginated bool `json:"paginated,omitempty"`

	// Effect declares the worst effect this tool can have (gh#749,
	// ADR-089). Descriptive input to discovery and to a consumer's own
	// default approval policy — it does NOT alter what semstreams
	// admits, gates, or refuses. The authoritative controls remain the
	// configured approval-required and allowed-tool name sets and the
	// per-loop advertised-tool admission check.
	//
	// Empty means undeclared, and undeclared resolves to
	// ToolEffectUnknown — never to ToolEffectReadOnly. Read it through
	// Canonical() rather than comparing the raw value.
	//
	// Does not cross the provider wire: no provider function schema has
	// a slot for it, and the model is not a party this classification is
	// for. Paginated is the precedent.
	Effect ToolEffect `json:"effect,omitempty"`
}

// ToolEffect classifies the worst effect a tool can have. It is an
// ordered severity claim, not a taxonomy of everything a tool does:
// a tool that POSTs to a third party is ToolEffectExternal, full stop,
// rather than "mutating and external". That is what lets one enum
// answer the question, and it is also the answer to argument-dependence
// — a tool whose severity varies with its arguments declares the worst
// case it admits.
//
// The classification is framework-owned canonical metadata so that
// downstream consumers (semdev and the second gh#749 consumer) share
// one vocabulary instead of inventing parallel ones. It is DESCRIPTIVE:
// see ToolDefinition.Effect for the enforcement boundary.
//
// OPEN FOR EXTENSION. Never switch exhaustively over the members
// without a default arm resolving to ToolEffectUnknown — a later member
// must be addable without a coordinated release across consumers.
type ToolEffect string

const (
	// ToolEffectUnknown means NO CLAIM has been made about this tool's
	// effect. It is not a middle rung between read_only and mutating:
	// a consumer mapping effect onto policy must treat it as at least
	// as restrictive as ToolEffectExternal.
	//
	// This is the resolution of an absent, empty, or unrecognized
	// value. Absence of a classification is not evidence of safety —
	// the tool counterpart of the framework rule that an absent
	// measurement must never render as a measurement of absence.
	ToolEffectUnknown ToolEffect = "unknown"

	// ToolEffectReadOnly means the tool observes and changes no state
	// anywhere, inside or outside the deployment. A GET against an
	// external API is read_only: what a query discloses is a governance
	// concern (processor/agentic-governance) and not an effect
	// classification.
	//
	// Distinct from FilesystemPolicyReadOnly (exec_policy.go), which is
	// a task-scoped filesystem WRITE SCOPE, not a tool classification.
	// The two are orthogonal and legitimately disagree: a tool may be
	// ToolEffectExternal while executing under filesystem policy
	// read_only — one classifies effect on the world, the other governs
	// worktree mutation. Same word, different subject.
	ToolEffectReadOnly ToolEffect = "read_only"

	// ToolEffectMutating means the tool can change state within the
	// deployment's own boundary — graph, KV, workspace files, rules,
	// flows, personas.
	ToolEffectMutating ToolEffect = "mutating"

	// ToolEffectExternal means the tool can change state or take
	// irrevocable action OUTSIDE the deployment boundary: a third-party
	// write, an email, a purchase. It DOMINATES ToolEffectMutating under
	// worst-effect semantics.
	//
	// "Spend" here means an irrevocable COMMERCIAL ACTION the tool
	// initiates — an order, a transfer, a booking. It does NOT mean the
	// metered cost of an external read: a query against a paid search or
	// data API consumes quota, and quota consumption is a cost, not an
	// effect on the world. A metered external read stays read_only. (The
	// two doc comments used to answer this differently; this is the
	// ruling.)
	//
	// MEDIATION DOES NOT LAUNDER EFFECT, but one hop through the
	// deployment is not itself external. bash is external_effect because
	// the command it runs can reach anything. A tool that writes a rule
	// or deploys a flow is mutating, even when the flow it deploys later
	// performs an outbound HTTP POST: the tool's own effect is the
	// configuration write, and the outbound action is the deployed
	// component's effect, classified where that component is described.
	// Classify what the tool does, not what a thing it configures might
	// later do — otherwise every configuration tool collapses to
	// external_effect and the enum stops discriminating.
	ToolEffectExternal ToolEffect = "external_effect"
)

// Known reports whether e names a declared enum member.
//
// The empty string is NOT known — it is *undeclared*, which is a third
// state with its own handling on each side: registration ACCEPTS it
// (a producer need not classify itself) while resolution maps it to
// ToolEffectUnknown. Callers must therefore spell out which of the two
// they mean rather than relying on Known alone; overloading Known to
// return true for empty would collapse "declared nothing" into
// "declared something valid" at the one seam that must tell them apart.
func (e ToolEffect) Known() bool {
	switch e {
	case ToolEffectUnknown, ToolEffectReadOnly, ToolEffectMutating, ToolEffectExternal:
		return true
	default:
		return false
	}
}

// Canonical resolves e to a declared enum member. Empty (undeclared)
// and unrecognized values both yield ToolEffectUnknown; a declared
// member returns itself.
//
// Total by construction, and the only correct way to read the field:
// an unrecognized value must never degrade to a permissive answer
// (the IsKnownFilesystemPolicy precedent), and here the fail-safe is
// ToolEffectUnknown, which policy consumers treat as maximally
// restrictive.
func (e ToolEffect) Canonical() ToolEffect {
	if e.Known() {
		return e
	}
	return ToolEffectUnknown
}

// Validate checks if the ToolDefinition is valid
func (t ToolDefinition) Validate() error {
	if t.Name == "" {
		return fmt.Errorf("tool name required")
	}
	if len(t.Parameters) == 0 {
		return fmt.Errorf("tool parameters required")
	}
	// Empty Effect is legal (undeclared, resolves to unknown); a
	// non-empty value must name a member. Registration enforces the
	// same rule directly rather than calling Validate, because
	// Validate additionally requires Parameters and registration
	// deliberately does not — see ExecutorRegistry.RegisterExecutor.
	if t.Effect != "" && !t.Effect.Known() {
		return fmt.Errorf("tool %q: unknown effect %q", t.Name, t.Effect)
	}
	return nil
}

// ToolCall represents a request to call a tool
type ToolCall struct {
	ID        string         `json:"id"`
	Name      string         `json:"name"`
	Arguments map[string]any `json:"arguments,omitempty"`
	Metadata  map[string]any `json:"metadata,omitempty"` // Domain context, propagated from task
	LoopID    string         `json:"loop_id,omitempty"`
	TraceID   string         `json:"trace_id,omitempty"`
	// ApprovedBy is set by the loop when re-dispatching a previously
	// gated tool call after receiving an ApprovalResponse. The
	// agentic-tools approval filter recognises a non-empty ApprovedBy
	// as the explicit bypass token (see C5). Empty means the call has
	// not been through human approval — normal filter rules apply.
	ApprovedBy string `json:"approved_by,omitempty"`
}

// Validate checks if the ToolCall is valid
func (t ToolCall) Validate() error {
	if t.ID == "" {
		return fmt.Errorf("tool call id required")
	}
	if t.Name == "" {
		return fmt.Errorf("tool call function name required")
	}
	return nil
}

// Schema implements message.Payload
func (t *ToolCall) Schema() message.Type {
	return message.Type{Domain: Domain, Category: CategoryToolCall, Version: SchemaVersion}
}

// MarshalJSON implements json.Marshaler
func (t *ToolCall) MarshalJSON() ([]byte, error) {
	type Alias ToolCall
	return json.Marshal((*Alias)(t))
}

// UnmarshalJSON implements json.Unmarshaler
func (t *ToolCall) UnmarshalJSON(data []byte) error {
	type Alias ToolCall
	return json.Unmarshal(data, (*Alias)(t))
}

// DecideToolName is the name agents use to invoke the coordinator's
// terminal decision tool. It lives here — beside the decide metadata
// contract and the reply vocabulary — because agentic-tools (the
// executor), agentic-loop (the terminal observer), and any future
// reader must all spell the same name; before gh#1094 the literal was
// spelled in three places.
const DecideToolName = "decide"

// Reserved decide actions with framework-owned user-facing semantics
// (ADR-101, gh#1094). Every OTHER decide action is a handoff to a rule
// chain and is never delivered to a user channel.
//
// The decide tool stays vocabulary-agnostic: its description enumerates
// no action, products name their own actions in persona prose, and the
// deployment-level restricted_decide_actions policy may still bar
// either reserved name (an autonomous deployment bars ask_user).
const (
	// DecideActionRespondDirect is the coordinator's answer to the
	// user. Delivered as a UserResponse of type result carrying the
	// decision's reason.
	DecideActionRespondDirect = "respond_direct"

	// DecideActionAskUser is the coordinator's clarification request.
	// Delivered as a UserResponse of type prompt carrying the
	// decision's reason.
	DecideActionAskUser = "ask_user"
)

// MetadataKeyDecideAction and MetadataKeyDecideReason are the
// ToolResult.Metadata keys under which the decide executor returns its
// typed decision to the loop. The loop reads them to populate
// LoopCompletedEvent.Decision; nothing parses the Content JSON for the
// same facts (Content stays the canonical payload for read_loop_result).
const (
	// MetadataKeyDecideAction carries the resolved (allowlist-canonical
	// when an allowlist applies) action string.
	MetadataKeyDecideAction = "action"

	// MetadataKeyDecideReason carries the coordinator's reason, which is
	// the user-facing content of a reply decision.
	MetadataKeyDecideReason = "reason"
)

// CoordinatorDecision is the typed decision of a `decide` terminal,
// observed by agentic-loop at completion and carried on
// LoopCompletedEvent (ADR-101 D2). It is populated ONLY when the loop's
// terminal StopLoop tool result came from the decide tool; a
// synthesized needs_clarification decision (a graph triple written
// after completion) never populates it, and no consumer infers a
// decision from the shape of Result.
//
// Both fields are required when the decision is present: an empty
// Action or Reason fails LoopCompletedEvent.Validate so a malformed
// decision is permanently rejected rather than silently classified as
// a handoff.
type CoordinatorDecision struct {
	Action string `json:"action"`
	Reason string `json:"reason"`
}

// IsUserFacingDecideAction reports whether a decide action is one of
// the reserved reply actions — the ONE classifier of the reply
// vocabulary (ADR-101 D1). Comparison is exact: no case folding, no
// separator coercion, no trimming (owner item 7). Any other action,
// including the empty string, is a handoff.
func IsUserFacingDecideAction(action string) bool {
	switch action {
	case DecideActionRespondDirect, DecideActionAskUser:
		return true
	default:
		return false
	}
}

// MetadataKeyDecideActionAllowlist is the TaskMessage.Metadata /
// ToolCall.Metadata key under which a closed action vocabulary for
// the decide tool flows from the spawning rule down to the executor.
//
// When set, the decide executor validates its `action` argument
// against the contained []string and rejects non-members with
// ToolErrorInvalidArgs (the message names the valid set so the LLM
// can correct on retry).
//
// Empty/missing leaves decide free-form (back-compat).
//
// Set by rule.executePublishAgent from rule.Action.ActionAllowlist.
// Belt-and-suspenders for persona prose: the persona enumerates the
// vocabulary in the LLM's system prompt; this allowlist enforces it
// structurally on the wire.
const MetadataKeyDecideActionAllowlist = "agent.decide.action_allowlist"

// MetadataKeyRelatedLoops is the TaskMessage.Metadata /
// ToolCall.Metadata key under which cross-arc loop-ID lineage flows
// from a spawning rule down to the spawned loop and into its tool
// calls. The value is a map[string]string whose keys are exact static
// lower-kebab predicate segments (maximum 64 bytes) and whose values are
// related loop IDs. Keys are neither substituted nor normalized.
//
// Use case: a downstream role needs to read_loop_result against an
// upstream loop without the loop ID being baked into the spawn
// prompt. Architect needing the researcher's loop ID for harness
// selection (semteams smoke #8 run-2 wedge cause); challenger
// cross-grounding back to planner; ops-agent / ADR-033 chain_id
// stability.
//
// String-to-string only by design. Non-string values are out of
// scope: if a future case needs structured data, it earns a
// dedicated typed field (the Tools / ToolChoice / Timeout
// precedent), not a generalized escape hatch through this map.
//
// Empty/missing leaves no lineage threaded (back-compat).
//
// Set by rule.executePublishAgent from rule.Action.RelatedLoops.
// JSON round-trip note: like ActionAllowlist, the value comes back
// from BaseMessage decode as map[string]any (with each value still
// a Go string) — readers coerce on access.
const MetadataKeyRelatedLoops = "agent.related_loops"

// MetadataKeyRunID is the ToolCall.Metadata key under which agentic-loop
// dispatch stamps the loop's run anchor — the bare run loop-id (ADR-053
// D7/D8) — when the loop belongs to a run. A tool executor reads it to
// obtain the run/chain identity directly, instead of re-deriving it by
// walking agent.loop.parent ancestry triples from its LoopID back to the
// chain root over graph.query.entity (the hand-rolled ancestry resolver
// the semteams product shell carried for ADR-053 Phase 5, issue #250).
//
// Empty/absent when the loop is not part of a run — a standalone loop
// stamps neither this nor MetadataKeyRunEntityID (back-compat). Paired
// with MetadataKeyRunEntityID, which carries the resolved 6-part chain
// execution entity ID for the same run.
//
// Stamped authoritatively (overwrite), unlike the loop_id soft-fallback:
// the run anchor is a framework fact derived from the loop's typed RunID
// (set via LoopManager.SetRunID at loop creation), and there is no
// legitimate caller-override use case, so dispatch always supplies it.
const MetadataKeyRunID = "agent.run_id"

// MetadataKeyRunEntityID is the ToolCall.Metadata key carrying the
// resolved 6-part chain execution entity ID
// (org.platform.agent.chain.execution.<runID>) for the loop's run
// anchor. Mirrors LoopCreatedEvent.RunEntityID / LoopCompletedEvent.
// RunEntityID so a tool executor and an event subscriber resolve the
// same run/chain entity.
//
// Stamped alongside MetadataKeyRunID by agentic-loop dispatch when the
// loop belongs to a run AND the handler has a valid platform identity
// (org+platform). Absent when RunID is empty or the platform identity
// is missing; a consumer that needs the entity ID under a missing
// platform can reconstruct it from MetadataKeyRunID plus its own
// org/platform via agentic.ChainExecutionEntityID.
const MetadataKeyRunEntityID = "agent.run_entity_id"

// MetadataKeyAgentRole is the ToolCall.Metadata key under which agentic-loop
// dispatch stamps the emitting loop's role (LoopEntity.Role). A tool executor
// reads it to attribute a role to its output WITHOUT the model supplying (and
// therefore being able to spoof) an identity parameter — e.g. emit_lesson
// derives agent.lesson.observed-role from it (ADR-080: "attribution is derived,
// not supplied").
//
// Stamped authoritatively (overwrite) when the loop has a role, and DELETED
// when the role is empty, exactly like the run anchor: the role is a framework
// fact derived from the loop entity, not a caller-routable hint, so a
// caller/model-injected value must never survive. Absent for a roleless loop.
const MetadataKeyAgentRole = "agent.role"

// LineageTripleNamespace is the fixed framework-owned namespace for
// cross-arc loop-ID lineage triples. Each entry in
// TaskMessage.Metadata[MetadataKeyRelatedLoops] becomes a triple of the form:
//
//	subject:   <spawned loop entity ID>
//	predicate: agent.lineage.<role-key> // e.g. agent.lineage.research-reviewer
//	object:    <upstream loop ID string>
//
// Downstream rules that fire on the spawned entity read these via the
// existing $entity.triple.<predicate> substitution, e.g.
// $entity.triple.agent.lineage.researcher resolves to the upstream loop ID
// without any new substitution-token, tool, or persona-driven echo
// forwarding.
//
// Stable namespace: ops-agent (ADR-027) and the operating-curve
// observability primitives (ADR-033) aggregate cross-arc lineage by
// scanning predicates with this prefix. Codifying as a public constant
// keeps producers and consumers aligned without string-literal drift.
const LineageTripleNamespace = "agent.lineage"

// LineageTripleProducer is the stable trusted producer identity granted the
// exact agent.lineage namespace. It names the framework integration boundary,
// not Triple.Source or caller-controlled task metadata.
const LineageTripleProducer = "agentic-loop-lineage"

var lineageTripleAuthority = mustLineageTripleAuthority()

func mustLineageTripleAuthority() *vocabulary.PredicateAuthority {
	authority, err := vocabulary.NewPredicateAuthority(vocabulary.NamespaceDelegation{
		Producer:  LineageTripleProducer,
		Namespace: LineageTripleNamespace,
	})
	if err != nil {
		panic(fmt.Sprintf("configure lineage predicate authority: %v", err))
	}
	return authority
}

// AuthorizeLineageTriplePredicate applies the fixed lineage namespace policy
// for a producer supplied by a trusted framework boundary.
func AuthorizeLineageTriplePredicate(producer, predicate string) error {
	return lineageTripleAuthority.Authorize(producer, predicate)
}

// LineageTriplePredicate returns the canonical predicate for a
// RelatedLoops role key. The key is one static lower-kebab predicate segment;
// validating the complete candidate through vocabulary.ParsePredicate keeps
// this narrow delegation from becoming unchecked authority to mint arbitrary
// agent predicates.
//
// Centralising construction keeps producers
// (rule.executePublishAgent / agentic-loop loop-creation path) and
// consumers (rule authors using $entity.triple.agent.lineage.<key>,
// ops-agent aggregations) cannot drift on the format.
func LineageTriplePredicate(roleKey string) (string, error) {
	predicate := LineageTripleNamespace + "." + roleKey
	parts, err := vocabulary.ParsePredicate(predicate)
	if err != nil {
		return "", err
	}
	canonical := parts.String()
	if err := AuthorizeLineageTriplePredicate(LineageTripleProducer, canonical); err != nil {
		return "", err
	}
	return canonical, nil
}

// ToolErrorKind classifies the source or nature of a tool execution failure.
// It is the structured counterpart to ToolResult.Error and feeds the
// agent.step.error_category graph predicate for queryable failure analysis.
type ToolErrorKind string

const (
	// ToolErrorTimeout means the tool exceeded its execution deadline
	// (context.DeadlineExceeded observed after the executor returned).
	ToolErrorTimeout ToolErrorKind = "timeout"

	// ToolErrorNotFound means the tool was not registered, the requested
	// resource did not exist, or the caller was not permitted to invoke it
	// via the component allowlist.
	ToolErrorNotFound ToolErrorKind = "not_found"

	// ToolErrorInvalidArgs means tool arguments failed validation
	// (missing required field, wrong type, schema violation).
	ToolErrorInvalidArgs ToolErrorKind = "invalid_args"

	// ToolErrorPermission means the request was refused on authorization
	// grounds — an external system's auth failure (e.g., HTTP 401/403) or an
	// internal framework policy refusal (approval filter, per-loop advertised
	// tool set).
	ToolErrorPermission ToolErrorKind = "permission"

	// ToolErrorNetwork means a transport-level failure occurred
	// (dial error, connection reset, DNS failure).
	ToolErrorNetwork ToolErrorKind = "network"

	// ToolErrorExternal means an external service returned a failure
	// that does not fall into the other categories (5xx, 429 rate limit,
	// operation-specific failure from an upstream API).
	ToolErrorExternal ToolErrorKind = "external"

	// ToolErrorInternal means an executor-internal bug
	// (marshal/unmarshal failure, unexpected nil, invariant violation).
	ToolErrorInternal ToolErrorKind = "internal"

	// ToolErrorUnknown means the failure was not classified. Used as the
	// default when a ToolResult has a non-empty Error but no ErrorKind.
	ToolErrorUnknown ToolErrorKind = "unknown"
)

// ToolResultHint classifies non-error conditions on a SUCCESSFUL tool
// call where the agent should refine its approach before continuing.
// It is the structured sibling of ToolErrorKind: ErrorKind classifies
// errors (the tool failed), Hint classifies successes that returned
// data the agent should treat as a signal to adjust.
//
// Distinct from ToolErrorKind because the call worked — there's
// nothing to "retry" in the failure sense. The cases are advisory:
// the model should narrow, broaden, or introspect on the next turn.
// The agent loop reads Hint in buildToolMessages and prepends a
// canonical hint line to the model's next message so small/mid-tier
// models don't have to parse free-form English advice from a
// successful result body.
//
// Pre-2026-05-11, the only in-band signaling pattern was
// ApprovalRequiredPrefix — a stringly-typed magic-string sniffed off
// the Error field. ResultHint replaces that pattern's spirit with a
// typed enum: producers set it directly; consumers branch on the
// typed value.
type ToolResultHint string

const (
	// HintTooLarge means the call returned more data than the executor
	// or framework permitted and the content was truncated (or the
	// raw response would have exceeded an internal cap). Action: the
	// model should narrow its query — add a filter, an entity_id, or
	// a smaller limit. Composes with the pagination contract
	// (MetadataKeyHasMore) when both are set: the model gets BOTH
	// "narrow your query" AND "or continue with cursor=..." in one
	// shot.
	HintTooLarge ToolResultHint = "too_large"

	// HintEmpty means the call succeeded with an empty result set
	// (no entities matched the filter, search returned zero hits).
	// Action: the model should try a broader filter, drop one of
	// the predicates, or invoke a different tool to find candidate
	// entities. Distinct from ToolErrorNotFound — empty results
	// from a well-formed query is not an error.
	HintEmpty ToolResultHint = "empty"

	// HintSyntaxError means the tool's query-language parser
	// rejected the request. Distinct from ToolErrorInvalidArgs —
	// InvalidArgs is the AGENT's arguments failing JSON-schema
	// validation at the framework boundary; SyntaxError is the
	// TOOL's deeper parse of the argument content (e.g. the
	// graph-query DSL itself rejecting a malformed expression).
	// Action: the model should call an introspect/help facility
	// on the tool before retrying with the same shape.
	HintSyntaxError ToolResultHint = "syntax_error"
)

// MetadataKeyHasMore is the ToolResult.Metadata key set to a bool true
// when more pages of results remain after the current call. Always set
// when an executor opts into pagination (ToolDefinition.Paginated=true);
// absence on a paginated tool's result is a contract violation worth
// a Warn log. Mutually paired with either MetadataKeyNextOffset (for
// byte/index-based paging) or MetadataKeyNextCursor (for opaque-keyset
// paging) — never both.
//
// The agent loop reads this in buildToolMessages and appends a
// canonical continuation hint to the model's next message so the
// model knows it can call the same tool again with the supplied
// continuation token, instead of having to re-narrow blind.
//
// Names are unprefixed because they're the canonical wire shape
// read_loop_result has already shipped under (semspec is already
// integrated against these strings). Lifting the existing names into
// constants is the contract semspec asked for — promotion, not
// rename.
const MetadataKeyHasMore = "has_more"

// MetadataKeyNextOffset is the ToolResult.Metadata key carrying the
// byte/index offset to pass back on the next call to continue paging.
// Use for offset-stable result sources where bytes-from-the-start
// is meaningful (read_loop_result's byte-paging of a single string
// is the canonical example). Mutually exclusive with NextCursor.
const MetadataKeyNextOffset = "next_offset"

// MetadataKeyNextCursor is the ToolResult.Metadata key carrying an
// opaque server-format-controlled cursor token to pass back on the
// next call. Use for keyset-paginated result SETS where there is no
// natural byte offset (graph_search-style result iteration). The
// agent must never inspect or modify the cursor — server owns the
// format, so the backend can change encoding without breaking
// in-flight pagination. Mutually exclusive with NextOffset.
const MetadataKeyNextCursor = "next_cursor"

// MetadataKeyTotalBytes is the OPTIONAL ToolResult.Metadata key
// carrying the total result count, when the executor can compute it
// cheaply (no expensive secondary round-trip). Useful for UIs that
// want progress bars; not load-bearing for the agent. Absent when
// the executor can't compute it without extra work.
//
// Named `total_bytes` rather than `total_available` to match the
// existing read_loop_result wire — promotion of the shipped shape,
// not rename. Executors with non-byte units can set their own
// unit-specific key and document it; this one is reserved for
// byte-paging consistency.
const MetadataKeyTotalBytes = "total_bytes"

// ToolResult represents the result of a tool call
type ToolResult struct {
	CallID     string         `json:"call_id"`
	Name       string         `json:"name,omitempty"` // Tool function name (required by Gemini on tool result messages)
	Content    string         `json:"content,omitempty"`
	Error      string         `json:"error,omitempty"`
	ErrorKind  ToolErrorKind  `json:"error_kind,omitempty"`  // Structured classification of the failure
	ResultHint ToolResultHint `json:"result_hint,omitempty"` // Structured action recommendation when call worked but agent should refine
	Metadata   map[string]any `json:"metadata,omitempty"`
	LoopID     string         `json:"loop_id,omitempty"`
	TraceID    string         `json:"trace_id,omitempty"`
	StopLoop   bool           `json:"stop_loop,omitempty"` // Signal loop termination; Content becomes the completion result
}

// Validate checks if the ToolResult is valid
func (t ToolResult) Validate() error {
	if t.CallID == "" {
		return fmt.Errorf("tool result call_id required")
	}
	return nil
}

// EffectiveErrorKind returns the failure classification a consumer should act
// on, applying the default ToolErrorUnknown documents: a result carrying an
// Error but no ErrorKind is still a failure, classified as unknown.
//
// The empty return is the THIRD state and the load-bearing one: it means the
// call did not fail at all, which is why callers must branch on emptiness
// rather than comparing against a member. Do not read ErrorKind directly to
// answer "did this fail" — an unclassified executor error reads as empty on
// the raw field and that is the fail-open shape.
//
// This is the home for the normalisation. Two older copies predate it
// (processor/agentic-tools/component.go and processor/agentic-loop/handlers.go
// buildToolTrajectoryStep); they agree today and their migration is filed
// separately. New readers call this.
func (t ToolResult) EffectiveErrorKind() ToolErrorKind {
	if t.ErrorKind != "" {
		return t.ErrorKind
	}
	if t.Error != "" {
		return ToolErrorUnknown
	}
	return ""
}

// Schema implements message.Payload
func (t *ToolResult) Schema() message.Type {
	return message.Type{Domain: Domain, Category: CategoryToolResult, Version: SchemaVersion}
}

// MarshalJSON implements json.Marshaler
func (t *ToolResult) MarshalJSON() ([]byte, error) {
	type Alias ToolResult
	return json.Marshal((*Alias)(t))
}

// UnmarshalJSON implements json.Unmarshaler
func (t *ToolResult) UnmarshalJSON(data []byte) error {
	type Alias ToolResult
	return json.Unmarshal(data, (*Alias)(t))
}

// ValidateToolsAllowed validates that all tool calls are in the allowed list
func ValidateToolsAllowed(calls []ToolCall, allowed []string) error {
	if len(calls) == 0 {
		return nil
	}

	// Build allowed set for fast lookup
	allowedSet := make(map[string]bool)
	for _, name := range allowed {
		allowedSet[name] = true
	}

	// Check each call
	var disallowed []string
	for _, call := range calls {
		if !allowedSet[call.Name] {
			disallowed = append(disallowed, call.Name)
		}
	}

	if len(disallowed) > 0 {
		return fmt.Errorf("disallowed tools: %s", strings.Join(disallowed, ", "))
	}

	return nil
}

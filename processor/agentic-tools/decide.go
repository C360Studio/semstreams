package agentictools

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"

	"github.com/c360studio/semstreams/agentic"
	"github.com/c360studio/semstreams/graph"
	"github.com/c360studio/semstreams/message"
	"github.com/c360studio/semstreams/natsclient"
	"github.com/c360studio/semstreams/pkg/errs"
	"github.com/c360studio/semstreams/types"
	agvocab "github.com/c360studio/semstreams/vocabulary/agentic"
)

// decideSAPCoercedTotal counts every action_allowlist SAP coercion the
// decide tool performs, broken down by from→to action pair. Operators
// alert on rate-of-change here: a sudden uptick (or sustained
// non-zero) signals a model/persona fit problem for the role producing
// the drift. Auto-registered with the default registerer so dashboards
// pick it up without component-level wiring.
//
// Cardinality bound: from_action and to_action are drawn from the
// declared allowlist values plus whatever the LLM emitted that
// normalised to one of them. Practical cardinality stays small under
// any sane allowlist.
var decideSAPCoercedTotal = promauto.NewCounterVec(
	prometheus.CounterOpts{
		Namespace: "semstreams",
		Subsystem: "decide_tool",
		Name:      "action_allowlist_sap_coerced_total",
		Help:      "Number of action_allowlist matches resolved via SAP normalisation rather than exact match. High rate signals model/persona drift; fix the persona prompt or model choice rather than raising MaxIterations.",
	},
	[]string{"from_action", "to_action"},
)

// decideActionRestrictedTotal counts every decide call rejected by the
// deployment-level decide-action restriction policy (gh#239). The policy
// is vocabulary-agnostic: the framework restricts whatever action strings
// the deployment names; a consuming product (e.g. SemTeams "autonomous"
// mode) maps its product mode onto the restricted set. Operators alert on
// a sustained non-zero rate — it means the persona prose and the
// restriction policy disagree (coordinators keep reaching for an action
// the deployment bars), so the fix is the persona prompt, not loosening
// the policy.
//
// Cardinality bound: action is the SAP-normalised restricted member that
// was hit, drawn from the configured restriction list, so cardinality
// stays at the size of that list.
var decideActionRestrictedTotal = promauto.NewCounterVec(
	prometheus.CounterOpts{
		Namespace: "semstreams",
		Subsystem: "decide_tool",
		Name:      "action_restricted_total",
		Help:      "Number of decide calls rejected by the deployment-level decide-action restriction policy (gh#239). High rate signals the persona prose and the restriction policy disagree; fix the persona prompt rather than loosening the policy.",
	},
	[]string{"action"},
)

// DecideToolName is the name agents use to invoke the coordinator's
// terminal decision tool.
const DecideToolName = "decide"

// decideMutationSubject is the NATS request/reply subject the graph-ingest
// component handles to add triples. Kept consistent with rule and graph
// writer callers (processor/rule/triple_mutator.go, processor/agentic-loop/
// graph_writer.go).
const decideMutationSubject = "graph.mutation.triple.add"

// decideMutationTimeout bounds each per-triple add request.
const decideMutationTimeout = 5 * time.Second

// decideToolSource is the Source field on triples this tool publishes. It
// lets operators distinguish coordinator decisions from rule-driven triple
// mutations at a glance in graph.
const decideToolSource = "coordinator-decide"

// TriplePublisher is the narrow surface agent-private state tools use to
// write triples. Production satisfies it with a natsclient.Client adapter;
// tests use an in-memory recorder so they don't need a real NATS
// connection.
//
// Two emission shapes:
//
//   - AddTriple — single-triple convenience. Used by callers that emit
//     across multiple subjects per logical operation where per-subject
//     atomicity is not required (web_search and http_request, where
//     URL-entity triples and loop-back-link triples target distinct
//     subjects and a partial write only loses the back-link
//     observably).
//
//   - AddTriplesBatch — atomic per-Subject. The graph-ingest handler
//     groups triples by Subject and CAS-applies them per entity, so a
//     batch sharing one Subject is fully atomic. Used by decide /
//     emit_diagnosis / scratchpad — each emits a co-emitted set on a
//     single entity where partial-write orphans would corrupt the
//     downstream rule-matching contract.
//
// See project_addtriplebatch_migration_followup memory for the
// migration history; write_todos has used the batch path since
// ADR-036 Stage 2 (2026-05-09).
type TriplePublisher interface {
	AddTriple(ctx context.Context, triple message.Triple) error
	AddTriplesBatch(ctx context.Context, triples []message.Triple) error
}

// DecideExecutor is the coordinator's terminal tool. A coordinator agent
// calls decide() exactly once to signal its judgment; on success, the tool
// publishes a small set of metadata triples onto the coordinator's loop
// entity so downstream rules match deterministically, and returns
// StopLoop=true with the full decision payload in Content so downstream
// agents can fetch any bulky fields (subtopics list, retry hint) via
// read_loop_result without them riding in triples.
type DecideExecutor struct {
	publisher TriplePublisher
	platform  types.PlatformMeta
	logger    *slog.Logger
	// restrictedActions is the deployment-level decide-action restriction
	// set (gh#239), keyed by SAP-normalised action so cased/hyphenated
	// drift cannot slip the gate. nil/empty ⇒ permissive (interactive)
	// default: no action is restricted, identical to pre-gh#239 behaviour.
	restrictedActions map[string]struct{}
}

// NewDecideExecutor constructs the executor given a triple publisher, the
// platform identity used to build the coordinator's loop entity ID, and the
// deployment-level decide-action restriction list (gh#239).
//
// restrictedActions is the run/deployment clarification-policy seam: any
// action string named here is rejected at decide-resolution time for EVERY
// coordinator task — front-door and rule-spawned alike — taking precedence
// over (and composing with) the per-task action_allowlist. The list is
// vocabulary-agnostic; the framework knows nothing of "interactive" vs
// "autonomous" — a consuming product maps its mode onto the list (e.g.
// SemTeams autonomous → ["ask_user"]). nil/empty preserves the historical
// permissive default (no restriction).
//
// Entries are SAP-normalised (lowercase, hyphen→underscore, trimmed) into a
// set at construction so the runtime check is O(1) and immune to cased /
// hyphenated drift. Logger defaults to slog.Default(); set explicitly via
// SetLogger when the caller has a structured logger handy.
func NewDecideExecutor(publisher TriplePublisher, platform types.PlatformMeta, restrictedActions []string) *DecideExecutor {
	return &DecideExecutor{
		publisher:         publisher,
		platform:          platform,
		logger:            slog.Default(),
		restrictedActions: newRestrictedActionSet(restrictedActions),
	}
}

// newRestrictedActionSet builds the SAP-normalised restriction lookup from
// the configured list. Empty/whitespace entries are dropped. Returns nil
// for an effectively-empty input so isDecideActionRestricted's len check
// short-circuits the permissive default with zero allocation.
func newRestrictedActionSet(actions []string) map[string]struct{} {
	if len(actions) == 0 {
		return nil
	}
	set := make(map[string]struct{}, len(actions))
	for _, a := range actions {
		n := normaliseActionForSAP(a)
		if n == "" {
			continue
		}
		set[n] = struct{}{}
	}
	if len(set) == 0 {
		return nil
	}
	return set
}

// SetLogger replaces the default logger. nil-safe — a nil argument
// preserves whatever logger the executor already has.
func (e *DecideExecutor) SetLogger(logger *slog.Logger) {
	if logger != nil {
		e.logger = logger
	}
}

// ListTools describes the decide tool. The action string is NOT enumerated
// at the tool level — different flows want different terminal actions. The
// coordinator's system prompt enumerates valid values per flow; downstream
// rules match on specific action values via the CoordinatorNextAction
// predicate.
//
// Description must NOT pre-load example action names. Real-LLM models
// (claude-sonnet-class observed against a downstream product flow,
// 2026-05) treat tool descriptions as more authoritative than persona
// prose. When the description named a handful of example actions,
// models biased their terminal choice toward those examples,
// overriding the persona's enumerated value and wedging chains at an
// unhandled coordinator.decision.next_action triple. The action vocabulary
// lives strictly in the role's system prompt; flows that want
// structural enforcement use the per-spawn allowlist threaded through
// TaskMessage.Metadata under MetadataKeyDecideActionAllowlist (see
// agentic/tools.go).
//
// TestDecideExecutor_DescriptionDoesNotPreloadActions guards the
// regression — see its pattern checks for the leak shapes.
func (e *DecideExecutor) ListTools() []agentic.ToolDefinition {
	return []agentic.ToolDefinition{
		{
			Name:        DecideToolName,
			Description: "Terminal decision tool for coordinator agents. Call exactly once with the action your role's system prompt enumerates. Emits a coordinator.decision.next_action triple on this loop's entity so downstream rules can route; the full args stay in the loop's Result for any agent that needs supporting data.",
			Parameters: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"action": map[string]any{
						"type":        "string",
						"description": "The decision. Valid values are enumerated in your role's system prompt.",
					},
					"reason": map[string]any{
						"type":        "string",
						"description": "Short natural-language justification for the chosen action.",
					},
					"subtopics": map[string]any{
						"type":        "array",
						"items":       map[string]any{"type": "string"},
						"description": "Optional. List of sub-targets when the chosen action represents a multi-target investigation; populate only when your role's contract names this field.",
					},
					"retry_hint": map[string]any{
						"type":        "string",
						"description": "Optional. Free-form guidance for a downstream pass; populate only when your role's contract names this field.",
					},
				},
				"required": []string{"action", "reason"},
			},
		},
	}
}

// decideArgs is the parsed shape of the decide tool's Arguments.
type decideArgs struct {
	Action    string   `json:"action"`
	Reason    string   `json:"reason"`
	Subtopics []string `json:"subtopics,omitempty"`
	RetryHint string   `json:"retry_hint,omitempty"`
}

// Execute routes the tool call to decide; any other tool name is treated
// as a routing bug.
func (e *DecideExecutor) Execute(ctx context.Context, call agentic.ToolCall) (agentic.ToolResult, error) {
	if call.Name != DecideToolName {
		return agentic.ToolResult{
			CallID:    call.ID,
			Error:     fmt.Sprintf("unknown tool: %s", call.Name),
			ErrorKind: agentic.ToolErrorNotFound,
		}, errs.WrapInvalid(fmt.Errorf("unknown tool: %s", call.Name), "DecideExecutor", "Execute", "route tool")
	}
	return e.decide(ctx, call)
}

func (e *DecideExecutor) decide(ctx context.Context, call agentic.ToolCall) (agentic.ToolResult, error) {
	args, err := parseDecideArgs(call.Arguments)
	if err != nil {
		return agentic.ToolResult{
			CallID:    call.ID,
			Error:     err.Error(),
			ErrorKind: agentic.ToolErrorInvalidArgs,
		}, nil
	}

	// Per-spawn action allowlist (set by rule.executePublishAgent via
	// TaskMessage.Metadata, propagated onto ToolCall.Metadata by the
	// agentic-loop). When present, validate args.Action against it.
	//
	// Beta.44 adds an SAP (schema-aligned-parsing) coercion layer on
	// the allowlist. Common drift shapes (lowercase, hyphenation, leading
	// whitespace) are normalised to the allowlist's canonical form
	// instead of producing an InvalidArgs rejection. semspec/semteams
	// confirmed structured-output drift is the rule, not the exception.
	//
	// Coercion fires the five LOUD signals below — the user's explicit
	// design constraint 2026-05-05: SAP can mask model/persona
	// mismatches, so every coercion must be impossible to miss in
	// operator dashboards. High coercion rate is a signal to fix model
	// fit / persona prompt, not a feature to celebrate.
	allowlist := resolveActionAllowlist(call.Metadata, args.Action)
	if allowlist.errMsg != "" {
		return agentic.ToolResult{
			CallID:    call.ID,
			Error:     allowlist.errMsg,
			ErrorKind: agentic.ToolErrorInvalidArgs,
		}, nil
	}

	// Deployment-level decide-action restriction (run clarification
	// policy, gh#239). Applied AFTER allowlist resolution so it sees the
	// canonical (post-SAP-coercion) action, and BEFORE the SAP success
	// signals fire — a restricted action is rejected outright, so emitting
	// "coercion succeeded" telemetry for it would be misleading. The gate
	// takes PRECEDENCE over any per-task action_allowlist (a restricted
	// action is rejected even when the allowlist would admit it) AND
	// covers coordinators that carry no allowlist at all — the front door,
	// the structural hole gh#239 closes. Same ToolErrorInvalidArgs →
	// re-pick treatment as an off-allowlist action: the loop iterates and
	// the model resolves without the restricted action (no dead-end, no
	// extra round-trip).
	if e.isDecideActionRestricted(allowlist.canonicalAction) {
		decideActionRestrictedTotal.WithLabelValues(normaliseActionForSAP(allowlist.canonicalAction)).Inc()
		e.logger.Info("decide tool rejected a deployment-restricted action",
			"loop_id", call.LoopID,
			"action", allowlist.canonicalAction,
			"hint", "barred by the deployment's restricted_decide_actions policy; a sustained rate here means the persona prose and the policy disagree — fix the persona prompt rather than loosening the policy")
		return agentic.ToolResult{
			CallID: call.ID,
			Error: fmt.Sprintf(
				"action %q is restricted by deployment policy and cannot be used in this run; "+
					"resolve without it and re-emit a different action your role's system prompt enumerates.",
				allowlist.canonicalAction,
			),
			ErrorKind: agentic.ToolErrorInvalidArgs,
		}, nil
	}

	sapCoerced := allowlist.coerced
	if sapCoerced {
		// Signal 1: slog.Warn — fires per coercion. Warn level so it
		// shows up in default operator dashboards. e.logger is
		// guaranteed non-nil by NewDecideExecutor, so no defensive
		// check needed.
		e.logger.Warn("decide tool SAP-coerced action",
			"loop_id", call.LoopID,
			"from_action", allowlist.rawAction,
			"to_action", allowlist.canonicalAction,
			"hint", "high coercion rate signals model/persona mismatch — fix the persona prompt or model choice rather than raising MaxIterations")
		// Signal 2: Prometheus counter — joinable in Grafana for
		// alert-rate-on-coercion.
		decideSAPCoercedTotal.WithLabelValues(allowlist.rawAction, allowlist.canonicalAction).Inc()
		// Signal 3 (audit triple) and Signal 4 (ToolResult metadata)
		// fire below alongside the canonical decision triples — they
		// share the publisher and result-metadata code path.
		args.Action = allowlist.canonicalAction
	}

	if call.LoopID == "" {
		return agentic.ToolResult{
			CallID:    call.ID,
			Error:     "decide invoked without a loop_id on the tool call; cannot resolve the coordinator's loop entity",
			ErrorKind: agentic.ToolErrorInternal,
		}, errs.WrapInvalid(fmt.Errorf("tool call missing loop_id"), "DecideExecutor", "decide", "resolve loop entity")
	}

	loopEntityID, err := agentic.TryLoopExecutionEntityID(e.platform.Org, e.platform.Platform, call.LoopID)
	if err != nil {
		return agentic.ToolResult{
			CallID:    call.ID,
			Error:     fmt.Sprintf("construct loop entity ID: %v", err),
			ErrorKind: agentic.ToolErrorInternal,
		}, errs.WrapInvalid(err, "DecideExecutor", "decide", "construct loop entity ID")
	}

	now := time.Now()
	triples := []message.Triple{
		{
			Subject:    loopEntityID,
			Predicate:  agvocab.CoordinatorNextAction,
			Object:     args.Action,
			Source:     decideToolSource,
			Timestamp:  now,
			Confidence: 1.0,
		},
		{
			Subject:    loopEntityID,
			Predicate:  agvocab.CoordinatorDecisionReason,
			Object:     args.Reason,
			Source:     decideToolSource,
			Timestamp:  now,
			Confidence: 1.0,
		},
	}
	// Signal 3: audit triple on the loop entity, queryable alongside
	// the trajectory. Object is "{raw}|{canonical}" so a single triple
	// captures the LLM's drift shape — ops-agent can group by Object
	// to find recurring drift patterns ("everyone's saying fan-out
	// when the allowlist names fan_out") without graph-side surgery.
	if sapCoerced {
		triples = append(triples, message.Triple{
			Subject:    loopEntityID,
			Predicate:  agvocab.CoordinatorDecisionSAPCoerced,
			Object:     allowlist.rawAction + "|" + allowlist.canonicalAction,
			Source:     decideToolSource,
			Timestamp:  now,
			Confidence: 1.0,
		})
	}

	// ADR-046 Phase 1: stamp subtopics as a triple so a downstream rule
	// can iterate via for_each. JSON-encoded []string keeps the Object
	// scalar-typed (graph-ingest's per-triple validators assume scalar
	// Objects); the for_each substitution layer parses it back to
	// []string at resolution time. Skip on empty/nil subtopics — the
	// decoder pattern is "missing predicate ⇒ no fan-out target,"
	// which lines up with how rules match on optional triples.
	if len(args.Subtopics) > 0 {
		subtopicsJSON, marshalErr := json.Marshal(args.Subtopics)
		if marshalErr != nil {
			// Failure to marshal a string slice is a programmer bug, not
			// an LLM-driven error — log loudly and continue without the
			// triple. The next_action + reason triples still publish so
			// rule matching on those is not blocked.
			e.logger.Warn("decide tool failed to marshal subtopics for triple emission",
				"loop_id", call.LoopID,
				"subtopic_count", len(args.Subtopics),
				"error", marshalErr)
		} else {
			triples = append(triples, message.Triple{
				Subject:    loopEntityID,
				Predicate:  agvocab.CoordinatorDecisionSubtopics,
				Object:     string(subtopicsJSON),
				Source:     decideToolSource,
				Timestamp:  now,
				Confidence: 1.0,
			})
		}
	}

	// Atomic batch publish: all triples share loopEntityID, so the
	// graph-ingest per-Subject CAS handler applies them as one unit.
	// Pre-2026-05-13 this was a per-triple loop that could leave the
	// loop entity with the next_action triple but no decision_reason
	// on graph-ingest degradation — downstream rules would match an
	// inconsistent snapshot. The atomic batch eliminates that window.
	if err := e.publisher.AddTriplesBatch(ctx, triples); err != nil {
		return agentic.ToolResult{
			CallID:    call.ID,
			Error:     fmt.Sprintf("publish decide triples: %v", err),
			ErrorKind: agentic.ToolErrorNetwork,
		}, errs.WrapTransient(err, "DecideExecutor", "decide", "publish triples batch")
	}

	// The tool's Content is the canonical decision payload. Downstream
	// agents needing the full args (subtopics list, retry_hint) pull it
	// via read_loop_result on this loop's completion.
	payload, err := json.Marshal(args)
	if err != nil {
		return agentic.ToolResult{
			CallID:    call.ID,
			Error:     fmt.Sprintf("marshal decision payload: %v", err),
			ErrorKind: agentic.ToolErrorInternal,
		}, errs.WrapInvalid(err, "DecideExecutor", "decide", "marshal payload")
	}

	resultMetadata := map[string]any{
		"action":         args.Action,
		"reason":         args.Reason,
		"loop_entity_id": loopEntityID,
		"subtopic_count": len(args.Subtopics),
		"has_retry_hint": args.RetryHint != "",
	}
	// Signal 4: ToolResult metadata. Downstream consumers reading the
	// raw tool result (tracing, replay, dashboards) see the SAP fact
	// without joining against the audit triple.
	if sapCoerced {
		resultMetadata["sap_coerced"] = true
		resultMetadata["sap_raw_action"] = allowlist.rawAction
	}

	return agentic.ToolResult{
		CallID:   call.ID,
		Content:  string(payload),
		StopLoop: true,
		Metadata: resultMetadata,
	}, nil
}

// allowlistResult captures the outcome of action_allowlist resolution
// when SAP (schema-aligned-parsing) is in scope. Three terminal shapes:
//
//   - errMsg != ""               → rejection. Caller returns
//     ToolErrorInvalidArgs with errMsg.
//   - coerced == true            → SAP fired. canonicalAction holds
//     the allowlist-form (e.g. "fan_out"
//     for input "fan-out"); rawAction
//     holds the LLM's original input.
//     Caller MUST emit the five LOUD
//     signals before proceeding.
//   - errMsg == "" && !coerced   → exact match, free-form, or
//     no-allowlist-set. Use args.Action
//     as-is.
type allowlistResult struct {
	canonicalAction string
	coerced         bool
	rawAction       string
	errMsg          string
}

// resolveActionAllowlist enforces the per-spawn action_allowlist gate
// from beta.41 with the beta.44 SAP layer applied.
//
// Algorithm:
//  1. No allowlist set → free-form (back-compat).
//  2. Exact match against allowlist → accepted as-is.
//  3. Normalised match (lowercase + hyphen→underscore + trim against
//     normalised-allowlist) → SAP coercion. canonicalAction is the
//     allowlist member; coerced=true so the caller emits LOUD signals.
//  4. No exact OR normalised match → rejected with InvalidArgs.
//
// SAP is intentionally conservative: V1 does NOT do edit-distance /
// Levenshtein matching. The user's explicit constraint 2026-05-05:
// SAP is a runtime safety net for *expected* drift, not a
// fuzzy-matching layer that hides genuinely broken outputs. Operators
// reading the SAP coercion metric should see "you have model fit
// problems," not "the framework is silently fixing things."
func resolveActionAllowlist(metadata map[string]any, action string) allowlistResult {
	if metadata == nil {
		return allowlistResult{canonicalAction: action}
	}
	raw, ok := metadata[agentic.MetadataKeyDecideActionAllowlist]
	if !ok || raw == nil {
		return allowlistResult{canonicalAction: action}
	}
	allowlist := coerceAllowlist(raw)
	if len(allowlist) == 0 {
		return allowlistResult{canonicalAction: action}
	}

	// Pass 1: exact match. Hot path.
	for _, a := range allowlist {
		if a == action {
			return allowlistResult{canonicalAction: action}
		}
	}

	// Pass 2: SAP — normalise both sides and re-test. The first
	// allowlist member whose normalised form matches the input's
	// normalised form is the canonical coercion target. Order of
	// allowlist entries determines tiebreaks if two members
	// normalise to the same string (rare; typically a config error).
	normalisedInput := normaliseActionForSAP(action)
	for _, a := range allowlist {
		if normaliseActionForSAP(a) == normalisedInput {
			return allowlistResult{
				canonicalAction: a,
				coerced:         true,
				rawAction:       action,
			}
		}
	}

	return allowlistResult{
		errMsg: fmt.Sprintf(
			"action %q is not in the allowed set for this role: [%s]. "+
				"The action vocabulary your role accepts is closed; pick one of these and re-emit.",
			action,
			strings.Join(allowlist, ", "),
		),
	}
}

// isDecideActionRestricted reports whether action is barred by the
// deployment-level decide-action restriction policy (gh#239). The
// comparison is SAP-normalised (the set was normalised at construction)
// so cased/hyphenated drift — "Ask-User", "ask_user", " ask-user " —
// cannot slip a restricted action past the gate. An empty restriction set
// always returns false (the permissive default), short-circuited cheaply.
func (e *DecideExecutor) isDecideActionRestricted(action string) bool {
	if len(e.restrictedActions) == 0 {
		return false
	}
	_, restricted := e.restrictedActions[normaliseActionForSAP(action)]
	return restricted
}

// normaliseActionForSAP applies the V1 normalisation rules: lowercase,
// hyphen-to-underscore, trim. Pure function so both the input and
// allowlist members can run through the same shape.
//
// The normalisation set is deliberately small. Adding fuzzy-matching
// here (Levenshtein distance, plural stripping, prefix elision) starts
// to mask real model/persona problems and is explicitly out of scope
// per the 2026-05-05 design discussion.
//
// Order constraint for future contributors: case-preserving rules
// MUST run before ToLower. Today's rules (TrimSpace, ToLower,
// ReplaceAll) are case-independent so the order doesn't matter, but
// a hypothetical camelCase→snake_case rule would need to fire before
// the input is lowercased (otherwise `fanOut` collapses to `fanout`,
// not `fan_out`). Pin the order if you add such a rule.
func normaliseActionForSAP(s string) string {
	s = strings.TrimSpace(s)
	s = strings.ToLower(s)
	s = strings.ReplaceAll(s, "-", "_")
	return s
}

// coerceAllowlist accepts the JSON-unmarshalled metadata value and
// returns the contained string slice. Handles []any (the round-trip
// shape from BaseMessage), []string (in-process callers), and nil/other
// (treated as empty).
func coerceAllowlist(raw any) []string {
	switch v := raw.(type) {
	case []string:
		return v
	case []any:
		out := make([]string, 0, len(v))
		for _, item := range v {
			if s, ok := item.(string); ok && s != "" {
				out = append(out, s)
			}
		}
		return out
	}
	return nil
}

// parseDecideArgs reads the untyped tool arguments into decideArgs and
// enforces the required fields. Missing or wrong-typed action/reason
// surface as invalid-args errors so the framework retry policy can step
// in — small models that miss the schema on the first try get a second
// chance before the loop fails.
func parseDecideArgs(raw map[string]any) (decideArgs, error) {
	action, ok := raw["action"].(string)
	if !ok || action == "" {
		return decideArgs{}, fmt.Errorf("action is required and must be a non-empty string")
	}
	reason, ok := raw["reason"].(string)
	if !ok || reason == "" {
		return decideArgs{}, fmt.Errorf("reason is required and must be a non-empty string")
	}
	args := decideArgs{Action: action, Reason: reason}

	if rawSubtopics, present := raw["subtopics"]; present && rawSubtopics != nil {
		slice, ok := rawSubtopics.([]any)
		if !ok {
			return decideArgs{}, fmt.Errorf("subtopics must be an array of strings")
		}
		args.Subtopics = make([]string, 0, len(slice))
		for i, v := range slice {
			s, ok := v.(string)
			if !ok {
				return decideArgs{}, fmt.Errorf("subtopics[%d] must be a string", i)
			}
			args.Subtopics = append(args.Subtopics, s)
		}
	}

	if hint, present := raw["retry_hint"]; present && hint != nil {
		s, ok := hint.(string)
		if !ok {
			return decideArgs{}, fmt.Errorf("retry_hint must be a string")
		}
		args.RetryHint = s
	}

	return args, nil
}

// natsTriplePublisher adapts natsclient.Client to TriplePublisher.
// Routes single-triple writes through graph.mutation.triple.add and
// batches through graph.mutation.triple.add_batch (the atomic per-
// Subject CAS path landed in ADR-036 Stage 2). The two subjects
// share the same retry policy — both mutation surfaces are
// idempotent at graph-ingest (a triple is set-of, a batch is
// deduplicated).
type natsTriplePublisher struct {
	client *natsclient.Client
}

// natsTriplesBatchSubject is the atomic per-Subject CAS path. Used
// by AddTriplesBatch — co-located triples (all sharing one Subject)
// land all-or-nothing, eliminating partial-write orphans on
// graph-ingest degradation.
const natsTriplesBatchSubject = "graph.mutation.triple.add_batch"

// natsTriplesBatchTimeout is the round-trip budget for the batch
// mutation. Matches write_todos's existing writeTodosTimeout (5s)
// since both write through the same handler.
const natsTriplesBatchTimeout = 5 * time.Second

// NewNATSTriplePublisher builds a TriplePublisher backed by the shared
// graph.mutation.triple.add NATS surface.
func NewNATSTriplePublisher(client *natsclient.Client) TriplePublisher {
	return &natsTriplePublisher{client: client}
}

func (p *natsTriplePublisher) AddTriple(ctx context.Context, triple message.Triple) error {
	req := graph.AddTripleRequest{Triple: triple}
	reqData, err := json.Marshal(req)
	if err != nil {
		return fmt.Errorf("marshal add-triple request: %w", err)
	}
	// RequestWithRetryClassified handles transient "no responders" errors when
	// graph-gateway is restarting or its subscription hasn't yet
	// propagated. The decide tool's terminal action is the triple
	// downstream rules trigger on — silent failure here breaks the
	// coordinator pattern's workflow. Idempotent (graph is a set of
	// triples), so retry is safe.
	// gh#93 Phase 2: Classified variant surfaces handler errors via err.
	respData, err := p.client.RequestWithRetryClassified(ctx, decideMutationSubject, reqData, decideMutationTimeout, natsclient.DefaultRetryConfig())
	if err != nil {
		return fmt.Errorf("request %s: %w", decideMutationSubject, err)
	}
	// ADR-060: handler failures arrive as the classified err above.
	var resp graph.AddTripleResponse
	if err := json.Unmarshal(respData, &resp); err != nil {
		return fmt.Errorf("unmarshal response: %w", err)
	}
	return nil
}

// AddTriplesBatch routes through graph.mutation.triple.add_batch —
// the atomic per-Subject CAS path. Callers that emit co-located
// triples (all sharing one Subject — decide/emit_diagnosis/scratchpad)
// rely on the all-or-nothing semantics to avoid partial-write
// orphans when graph-ingest degrades. Same retry policy as AddTriple;
// both mutation surfaces are idempotent at graph-ingest.
func (p *natsTriplePublisher) AddTriplesBatch(ctx context.Context, triples []message.Triple) error {
	if len(triples) == 0 {
		// No-op: callers that conditionally accumulate triples may
		// hand an empty slice rather than special-casing the gate.
		return nil
	}
	req := graph.AddTriplesBatchRequest{Triples: triples}
	reqData, err := json.Marshal(req)
	if err != nil {
		return fmt.Errorf("marshal batch-add request: %w", err)
	}
	// gh#93 Phase 2: Classified variant surfaces handler errors via err.
	respData, err := p.client.RequestWithRetryClassified(ctx, natsTriplesBatchSubject, reqData, natsTriplesBatchTimeout, natsclient.DefaultRetryConfig())
	if err != nil {
		return fmt.Errorf("request %s: %w", natsTriplesBatchSubject, err)
	}
	var resp graph.AddTriplesBatchResponse
	if err := json.Unmarshal(respData, &resp); err != nil {
		return fmt.Errorf("unmarshal batch response: %w", err)
	}
	// ADR-060: a whole-batch failure arrives as the classified err above. A
	// PARTIAL batch (some subjects committed) returns a success body with
	// FailedSubjects populated (per-subject errors in the map), handled here.
	if len(resp.FailedSubjects) > 0 {
		return fmt.Errorf("graph-ingest partial batch (written=%d, failed=%v)",
			resp.WrittenCount, resp.FailedSubjects)
	}
	return nil
}

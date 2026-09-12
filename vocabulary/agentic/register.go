package agentic

import "github.com/c360studio/semstreams/vocabulary"

// Register registers all agentic predicates with the vocabulary registry,
// including IRI mappings to W3C S-Agent-Comm ontology for standards compliance.
//
// Call this function during application initialization to enable predicate
// metadata lookup and IRI-based export.
//
// Example:
//
//	func init() {
//	    agentic.Register()
//	}
//
//	// Later, retrieve metadata including IRI mapping
//	meta := vocabulary.GetPredicateMetadata(agentic.IntentGoal)
//	fmt.Println(meta.StandardIRI)  // https://w3id.org/agent-ontology/core#Intent
func Register() {
	registerIntentPredicates()
	registerCapabilityPredicates()
	registerDelegationPredicates()
	registerAccountabilityPredicates()
	registerExecutionPredicates()
	registerActionPredicates()
	registerTaskPredicates()
	registerModelPredicates()
	registerLoopPredicates()
	registerStepPredicates()
	registerCoordinatorPredicates()
	registerOpsPredicates()
	registerLessonPredicates()
	registerOpsConfigPredicates()
	registerIdentityPredicates()
	registerTodoPredicates()
	registerScratchPredicates()
	registerWebPredicates()
}

func registerCoordinatorPredicates() {
	for _, predicate := range []string{
		CoordinatorNextAction,
		CoordinatorDecisionReason,
		CoordinatorDecisionSAPCoerced,
		CoordinatorDecisionSubtopics,
		CoordinatorDecisionSynthetic,
	} {
		vocabulary.Register(predicate)
	}
}

func registerOpsPredicates() {
	for _, predicate := range []string{
		OpsDiagnosisFinding,
		OpsDiagnosisRecommendation,
		OpsDiagnosisConfidence,
		OpsDiagnosisEvidence,
		OpsDiagnosisObservedRole,
		OpsDiagnosisSeverity,
	} {
		vocabulary.Register(predicate)
	}
}

// registerLessonPredicates registers the agent.lesson.* family (ADR-080 —
// push-based agent memory / lesson substrate). The enumerated/structural and
// reference predicates stay rule-matchable so curation rules can gate on
// polarity/severity/status/scope; the three authored-text predicates (summary,
// detail, injection-form) register rule-opaque per the LLM-authored-content
// discipline (ADR-036). agent.lesson.evidence carries StandardIRI
// prov:wasDerivedFrom — annotation only; the PROV-O constant already lives in
// vocabulary/standards.go.
func registerLessonPredicates() {
	vocabulary.Register(LessonCategory,
		vocabulary.WithDescription("Open product-taxonomy classifier for the lesson; no framework-closed value set"),
		vocabulary.WithDataType(vocabulary.DataTypeString))

	vocabulary.Register(LessonPolarity,
		vocabulary.WithDescription("Directional stance of the lesson; closed value set avoid|best_practice"),
		vocabulary.WithDataType(vocabulary.DataTypeString))

	vocabulary.Register(LessonSeverity,
		vocabulary.WithDescription("Urgency classification ordering lessons at brief assembly; closed value set info|warning|critical"),
		vocabulary.WithDataType(vocabulary.DataTypeString))

	vocabulary.Register(LessonStatus,
		vocabulary.WithDescription("Gated-lifecycle state; closed value set proposed|active|retired|superseded; single-valued (replace); only active is injectable"),
		vocabulary.WithDataType(vocabulary.DataTypeString))

	vocabulary.Register(LessonSummary,
		vocabulary.WithDescription("Short LLM-authored gist of the lesson; rule-opaque authored prose"),
		vocabulary.WithDataType(vocabulary.DataTypeString),
		vocabulary.WithRuleOpaque(true))

	vocabulary.Register(LessonDetail,
		vocabulary.WithDescription("Unbounded LLM-authored explanation of the lesson; rule-opaque authored prose"),
		vocabulary.WithDataType(vocabulary.DataTypeString),
		vocabulary.WithRuleOpaque(true))

	vocabulary.Register(LessonInjectionForm,
		vocabulary.WithDescription("Bounded string rendered verbatim into a downstream brief; byte-bounded at the writer; rule-opaque authored prose"),
		vocabulary.WithDataType(vocabulary.DataTypeString),
		vocabulary.WithRuleOpaque(true))

	vocabulary.Register(LessonEvidence,
		vocabulary.WithDescription("Supporting entity ID the lesson was derived from (>=1 at the writer); multi-valued"),
		vocabulary.WithDataType(vocabulary.DataTypeString),
		vocabulary.WithIRI(vocabulary.ProvWasDerivedFrom))

	vocabulary.Register(LessonAppliesTo,
		vocabulary.WithDescription("Deterministic scope key controlling brief injection; grammar id:{prefix}|tag:{token}; multi-valued"),
		vocabulary.WithDataType(vocabulary.DataTypeString))

	vocabulary.Register(LessonObservedRole,
		vocabulary.WithDescription("Agent role the lesson pertains to; optional"),
		vocabulary.WithDataType(vocabulary.DataTypeString))

	vocabulary.Register(LessonRetiredAt,
		vocabulary.WithDescription("Lifecycle timestamp set when a lesson leaves active; single-valued (replace)"),
		vocabulary.WithDataType(vocabulary.DataTypeDateTime))

	vocabulary.Register(LessonSupersededBy,
		vocabulary.WithDescription("Entity ID of the lesson that supersedes this one; single-valued (replace)"),
		vocabulary.WithDataType(vocabulary.DataTypeString))

	vocabulary.Register(LessonCreatedAt,
		vocabulary.WithDescription("Immutable wall-clock birth timestamp of the lesson (RFC3339 UTC); replay-stable ordering key for brief injection; never re-stamped by lifecycle transitions"),
		vocabulary.WithDataType(vocabulary.DataTypeDateTime))
}

func registerOpsConfigPredicates() {
	for _, predicate := range []string{
		OpsConfigAccuracy,
		OpsConfigCostPerTask,
		OpsConfigP95Latency,
		OpsConfigActive,
		OpsConfigParent,
	} {
		vocabulary.Register(predicate)
	}
}

// registerScratchPredicates registers predicates for the scratchpad tool
// (semspec ask 2026-05-12, ADR-036 §Future candidates). ScratchText is
// rule-opaque per the LLM-authored content discipline; the three
// structural predicates stay rule-matchable.
func registerScratchPredicates() {
	vocabulary.Register(ScratchID,
		vocabulary.WithDescription("Stable per-call identifier (UUID); the four triples of one scratchpad call correlate by the shared Context stamp carrying this value"),
		vocabulary.WithDataType(vocabulary.DataTypeString))

	vocabulary.Register(ScratchText,
		vocabulary.WithDescription("Free-form pre-commit reasoning prose; owner-interpretable only, rule-opaque"),
		vocabulary.WithDataType(vocabulary.DataTypeString),
		vocabulary.WithRuleOpaque(true))

	vocabulary.Register(ScratchCreatedAt,
		vocabulary.WithDescription("Wall-clock timestamp of the scratchpad call"),
		vocabulary.WithDataType(vocabulary.DataTypeDateTime))

	vocabulary.Register(ScratchChars,
		vocabulary.WithDescription("Character count of ScratchText; structural fact for size-based dashboards/rules without reading the rule-opaque body"),
		vocabulary.WithDataType(vocabulary.DataTypeInt))
}

// registerTodoPredicates registers the one agent-private todo record
// predicate. The complete JSON record is rule-opaque because raw rule matching
// cannot preserve item correlation.
func registerTodoPredicates() {
	vocabulary.Register(TodoRecord,
		vocabulary.WithDescription("Deterministic JSON record for one todo item (id, content, status, position, updated_at); interpreted through TodoReader"),
		vocabulary.WithDataType(vocabulary.DataTypeString),
		vocabulary.WithRuleOpaque(true))
}

// registerIntentPredicates registers predicates for agent intentions and goals.
func registerIntentPredicates() {
	vocabulary.Register(IntentGoal,
		vocabulary.WithDescription("The objective or goal an agent aims to achieve"),
		vocabulary.WithDataType(vocabulary.DataTypeString),
		vocabulary.WithIRI(IriIntent))

	vocabulary.Register(IntentType,
		vocabulary.WithDescription("Category of intent (e.g., data-analysis, content-generation)"),
		vocabulary.WithDataType(vocabulary.DataTypeString),
		vocabulary.WithIRI(IriHasIntentType))

	vocabulary.Register(IntentParameter,
		vocabulary.WithDescription("Typed parameter for the intent"),
		vocabulary.WithDataType(vocabulary.DataTypeString),
		vocabulary.WithIRI(IriHasParameter))

	vocabulary.Register(IntentAuthorized,
		vocabulary.WithDescription("Delegation authorizing this intent"),
		vocabulary.WithDataType(vocabulary.DataTypeString),
		vocabulary.WithIRI(IriAuthorizedBy))

	vocabulary.Register(IntentProduces,
		vocabulary.WithDescription("Action produced by this intent"),
		vocabulary.WithDataType(vocabulary.DataTypeString),
		vocabulary.WithIRI(IriProducesAction))
}

// registerCapabilityPredicates registers predicates for agent capabilities.
func registerCapabilityPredicates() {
	vocabulary.Register(CapabilityName,
		vocabulary.WithDescription("Identifier for an agent capability"),
		vocabulary.WithDataType(vocabulary.DataTypeString),
		vocabulary.WithIRI(IriCapability))

	vocabulary.Register(CapabilityDescription,
		vocabulary.WithDescription("Human-readable description of the capability"),
		vocabulary.WithDataType(vocabulary.DataTypeString))

	vocabulary.Register(CapabilityExpression,
		vocabulary.WithDescription("Semantic fingerprint for capability matching and embedding"),
		vocabulary.WithDataType(vocabulary.DataTypeString),
		vocabulary.WithIRI(IriCapabilityExpression))

	vocabulary.Register(CapabilityConfidence,
		vocabulary.WithDescription("Agent's self-assessed confidence in capability (0.0-1.0)"),
		vocabulary.WithDataType(vocabulary.DataTypeFloat),
		vocabulary.WithRange("0-1"),
		vocabulary.WithIRI(IriCapabilityConfidence))

	vocabulary.Register(CapabilitySkill,
		vocabulary.WithDescription("Atomic skill implementing the capability"),
		vocabulary.WithDataType(vocabulary.DataTypeString),
		vocabulary.WithIRI(IriHasSkill))

	vocabulary.Register(CapabilityConstraint,
		vocabulary.WithDescription("Execution constraint on the capability"),
		vocabulary.WithDataType(vocabulary.DataTypeString),
		vocabulary.WithIRI(IriCapabilityConstraint))

	vocabulary.Register(CapabilityPermission,
		vocabulary.WithDescription("Permission required for the capability"),
		vocabulary.WithDataType(vocabulary.DataTypeString),
		vocabulary.WithIRI(IriRequiresPermission))
}

// registerDelegationPredicates registers predicates for authority delegation.
func registerDelegationPredicates() {
	vocabulary.Register(DelegationFrom,
		vocabulary.WithDescription("Agent granting delegated authority"),
		vocabulary.WithDataType(vocabulary.DataTypeString),
		vocabulary.WithIRI(IriDelegatedBy),
		vocabulary.WithInverseOf(DelegationTo))

	vocabulary.Register(DelegationTo,
		vocabulary.WithDescription("Agent receiving delegated authority"),
		vocabulary.WithDataType(vocabulary.DataTypeString),
		vocabulary.WithIRI(IriDelegatesTo),
		vocabulary.WithInverseOf(DelegationFrom))

	vocabulary.Register(DelegationScope,
		vocabulary.WithDescription("Boundary of delegated authority"),
		vocabulary.WithDataType(vocabulary.DataTypeString),
		vocabulary.WithIRI(IriDelegationScope))

	vocabulary.Register(DelegationCapability,
		vocabulary.WithDescription("Capability allowed by the delegation"),
		vocabulary.WithDataType(vocabulary.DataTypeString),
		vocabulary.WithIRI(IriAllowedCapability))

	vocabulary.Register(DelegationValidFrom,
		vocabulary.WithDescription("When the delegation becomes valid"),
		vocabulary.WithDataType(vocabulary.DataTypeDateTime),
		vocabulary.WithIRI(IriValidFrom))

	vocabulary.Register(DelegationValidUntil,
		vocabulary.WithDescription("When the delegation expires"),
		vocabulary.WithDataType(vocabulary.DataTypeDateTime),
		vocabulary.WithIRI(IriValidUntil))

	vocabulary.Register(DelegationChain,
		vocabulary.WithDescription("Multi-level delegation chain identifier"),
		vocabulary.WithDataType(vocabulary.DataTypeString),
		vocabulary.WithIRI(IriDelegationChain))
}

// registerAccountabilityPredicates registers predicates for accountability tracking.
func registerAccountabilityPredicates() {
	vocabulary.Register(AccountabilityActor,
		vocabulary.WithDescription("Agent performing the accountable action"),
		vocabulary.WithDataType(vocabulary.DataTypeString),
		vocabulary.WithIRI(IriActor))

	vocabulary.Register(AccountabilityAction,
		vocabulary.WithDescription("Action being accounted for"),
		vocabulary.WithDataType(vocabulary.DataTypeString))

	vocabulary.Register(AccountabilityAssigned,
		vocabulary.WithDescription("Party assigned responsibility"),
		vocabulary.WithDataType(vocabulary.DataTypeString),
		vocabulary.WithIRI(IriAssignedTo))

	vocabulary.Register(AccountabilityRationale,
		vocabulary.WithDescription("Reasoning for the responsibility attribution"),
		vocabulary.WithDataType(vocabulary.DataTypeString),
		vocabulary.WithIRI(IriRationale))

	vocabulary.Register(AccountabilityCompliance,
		vocabulary.WithDescription("Compliance assessment result"),
		vocabulary.WithDataType(vocabulary.DataTypeString),
		vocabulary.WithIRI(IriComplianceAssessment))

	vocabulary.Register(AccountabilityTimestamp,
		vocabulary.WithDescription("When the accountability event occurred"),
		vocabulary.WithDataType(vocabulary.DataTypeDateTime))
}

// registerExecutionPredicates registers predicates for execution context.
func registerExecutionPredicates() {
	vocabulary.Register(ExecutionEnvironment,
		vocabulary.WithDescription("Runtime environment type (sandbox, container, etc.)"),
		vocabulary.WithDataType(vocabulary.DataTypeString),
		vocabulary.WithIRI(IriExecutionEnvironment))

	vocabulary.Register(ExecutionSecurity,
		vocabulary.WithDescription("Security context for execution"),
		vocabulary.WithDataType(vocabulary.DataTypeString),
		vocabulary.WithIRI(IriSecurityContext))

	vocabulary.Register(ExecutionConstraint,
		vocabulary.WithDescription("Resource constraint for execution"),
		vocabulary.WithDataType(vocabulary.DataTypeString),
		vocabulary.WithIRI(IriResourceConstraint))

	vocabulary.Register(ExecutionRateLimit,
		vocabulary.WithDescription("Rate limiting constraint"),
		vocabulary.WithDataType(vocabulary.DataTypeString),
		vocabulary.WithIRI(IriRateLimit))

	vocabulary.Register(ExecutionBudget,
		vocabulary.WithDescription("Cost or resource budget for execution"),
		vocabulary.WithDataType(vocabulary.DataTypeString),
		vocabulary.WithIRI(IriBudget))

	vocabulary.Register(ExecutionInput,
		vocabulary.WithDescription("Input state for execution"),
		vocabulary.WithDataType(vocabulary.DataTypeString))

	vocabulary.Register(ExecutionOutput,
		vocabulary.WithDescription("Output state from execution"),
		vocabulary.WithDataType(vocabulary.DataTypeString))
}

// registerActionPredicates registers predicates for concrete actions.
func registerActionPredicates() {
	vocabulary.Register(ActionType,
		vocabulary.WithDescription("Category of the action"),
		vocabulary.WithDataType(vocabulary.DataTypeString))

	vocabulary.Register(ActionExecutedBy,
		vocabulary.WithDescription("Agent that executed the action"),
		vocabulary.WithDataType(vocabulary.DataTypeString))

	vocabulary.Register(ActionProduced,
		vocabulary.WithDescription("Artifact produced by the action"),
		vocabulary.WithDataType(vocabulary.DataTypeString),
		vocabulary.WithIRI(IriArtifact))

	vocabulary.Register(ActionContext,
		vocabulary.WithDescription("Execution context for the action"),
		vocabulary.WithDataType(vocabulary.DataTypeString))

	vocabulary.Register(ActionTrace,
		vocabulary.WithDescription("Trace or audit record for the action"),
		vocabulary.WithDataType(vocabulary.DataTypeString),
		vocabulary.WithIRI(IriTraceEvent))
}

// registerTaskPredicates registers predicates for task management.
func registerTaskPredicates() {
	vocabulary.Register(TaskAssigned,
		vocabulary.WithDescription("Agent assigned to the task"),
		vocabulary.WithDataType(vocabulary.DataTypeString))

	vocabulary.Register(TaskCapability,
		vocabulary.WithDescription("Capability required for the task"),
		vocabulary.WithDataType(vocabulary.DataTypeString))

	vocabulary.Register(TaskSubtask,
		vocabulary.WithDescription("Child task in hierarchical decomposition"),
		vocabulary.WithDataType(vocabulary.DataTypeString))

	vocabulary.Register(TaskDependency,
		vocabulary.WithDescription("Task that must complete before this one"),
		vocabulary.WithDataType(vocabulary.DataTypeString))

	vocabulary.Register(TaskStatus,
		vocabulary.WithDescription("Current status of the task"),
		vocabulary.WithDataType(vocabulary.DataTypeString))
}

// registerModelPredicates registers predicates for LLM model endpoint entities.
func registerModelPredicates() {
	vocabulary.Register(ModelProvider,
		vocabulary.WithDescription("API provider type (anthropic, ollama, openai, openrouter)"),
		vocabulary.WithDataType(vocabulary.DataTypeString))

	vocabulary.Register(ModelName,
		vocabulary.WithDescription("Model identifier sent to the provider"),
		vocabulary.WithDataType(vocabulary.DataTypeString))

	vocabulary.Register(ModelMaxTokens,
		vocabulary.WithDescription("Context window size in tokens"),
		vocabulary.WithDataType(vocabulary.DataTypeInt))

	vocabulary.Register(ModelSupportsTools,
		vocabulary.WithDescription("Whether the endpoint supports tool calling"),
		vocabulary.WithDataType(vocabulary.DataTypeBool))

	vocabulary.Register(ModelInputPrice,
		vocabulary.WithDescription("Cost per 1M input tokens in USD"),
		vocabulary.WithDataType(vocabulary.DataTypeFloat))

	vocabulary.Register(ModelOutputPrice,
		vocabulary.WithDescription("Cost per 1M output tokens in USD"),
		vocabulary.WithDataType(vocabulary.DataTypeFloat))

	vocabulary.Register(ModelEndpointURL,
		vocabulary.WithDescription("API endpoint URL for the model"),
		vocabulary.WithDataType(vocabulary.DataTypeString))

	vocabulary.Register(ModelRateLimit,
		vocabulary.WithDescription("Requests per minute limit for the endpoint"),
		vocabulary.WithDataType(vocabulary.DataTypeInt))
}

// registerLoopPredicates registers predicates for agentic loop execution entities.
func registerLoopPredicates() {
	vocabulary.Register(LoopOutcome,
		vocabulary.WithDescription("Terminal outcome of the loop execution (success, failed, cancelled)"),
		vocabulary.WithDataType(vocabulary.DataTypeString))

	vocabulary.Register(LoopTerminalReason,
		vocabulary.WithDescription("Classified reason for a failed loop's terminal outcome (max_iterations, model_error, handler_error, graph_state_reset_required); absent on success"),
		vocabulary.WithDataType(vocabulary.DataTypeString))

	// Rule-VISIBLE deliberately: this is a classification a rule must be
	// able to branch on (decline to cite a loop whose audit trail is not
	// there), not LLM-authored prose. Absence is not a completeness claim.
	vocabulary.Register(LoopEvidenceIntegrity,
		vocabulary.WithDescription("Observed trajectory audit loss for this loop (incomplete), whether observed while recording this loop or determined at startup for every loop; absent when no loss was observed, which is not a claim that evidence is complete"),
		vocabulary.WithDataType(vocabulary.DataTypeString))

	vocabulary.Register(LoopRole,
		vocabulary.WithDescription("Role used during this loop execution"),
		vocabulary.WithDataType(vocabulary.DataTypeString))

	vocabulary.Register(LoopModelUsed,
		vocabulary.WithDescription("Entity reference to the model endpoint used"),
		vocabulary.WithDataType(vocabulary.DataTypeString))

	vocabulary.Register(LoopIterations,
		vocabulary.WithDescription("Number of LLM iterations executed in this loop"),
		vocabulary.WithDataType(vocabulary.DataTypeInt))

	vocabulary.Register(LoopTokensIn,
		vocabulary.WithDescription("Total input tokens consumed across all iterations"),
		vocabulary.WithDataType(vocabulary.DataTypeInt))

	vocabulary.Register(LoopTokensOut,
		vocabulary.WithDescription("Total output tokens consumed across all iterations"),
		vocabulary.WithDataType(vocabulary.DataTypeInt))

	vocabulary.Register(LoopCostUSD,
		vocabulary.WithDescription("Computed cost in USD for this loop execution"),
		vocabulary.WithDataType(vocabulary.DataTypeFloat))

	vocabulary.Register(LoopTask,
		vocabulary.WithDescription("Task ID this loop execution served"),
		vocabulary.WithDataType(vocabulary.DataTypeString))

	vocabulary.Register(LoopParent,
		vocabulary.WithDescription("Entity reference to the parent loop entity"),
		vocabulary.WithDataType(vocabulary.DataTypeString))

	vocabulary.Register(LoopRun,
		vocabulary.WithDescription("Bare run loop ID this loop execution belongs to"),
		vocabulary.WithDataType(vocabulary.DataTypeString))

	vocabulary.Register(LoopRunEntityID,
		vocabulary.WithDescription("Entity reference to the chain execution this loop belongs to"),
		vocabulary.WithDataType(vocabulary.DataTypeString))

	vocabulary.Register(RunOriginEntityID,
		vocabulary.WithDescription("Entity reference to the loop execution this run was minted from"),
		vocabulary.WithDataType(vocabulary.DataTypeString))

	vocabulary.Register(LoopReplyTo,
		vocabulary.WithDescription("Reply subject used to return this loop's result"),
		vocabulary.WithDataType(vocabulary.DataTypeString))

	vocabulary.Register(LoopWorkflow,
		vocabulary.WithDescription("Workflow slug this loop belongs to"),
		vocabulary.WithDataType(vocabulary.DataTypeString))

	vocabulary.Register(LoopWorkflowStep,
		vocabulary.WithDescription("Step within the workflow for this loop"),
		vocabulary.WithDataType(vocabulary.DataTypeString))

	vocabulary.Register(LoopEndedAt,
		vocabulary.WithDescription("Terminal timestamp for this loop (completion, failure, or cancellation)"),
		vocabulary.WithDataType(vocabulary.DataTypeDateTime))

	vocabulary.Register(LoopUser,
		vocabulary.WithDescription("User ID who initiated this loop"),
		vocabulary.WithDataType(vocabulary.DataTypeString))

	vocabulary.Register(LoopHasStep,
		vocabulary.WithDescription("Entity reference to a trajectory step within this loop (multi-valued)"),
		vocabulary.WithDataType(vocabulary.DataTypeString))

	vocabulary.Register(LoopDescription,
		vocabulary.WithDescription("User task prompt that initiated this loop; indexed for NL/BM25 search"),
		vocabulary.WithDataType(vocabulary.DataTypeString))

	vocabulary.Register(LoopObservedWeb,
		vocabulary.WithDescription("Entity reference to a web observation seen by this loop via web_search (multi-valued)"),
		vocabulary.WithDataType(vocabulary.DataTypeString))

	vocabulary.Register(LoopFetchedWeb,
		vocabulary.WithDescription("Entity reference to a web observation pulled by this loop via http_request (multi-valued)"),
		vocabulary.WithDataType(vocabulary.DataTypeString))
}

// registerWebPredicates registers predicates for web observation entities
// emitted by the web_search and http_request tools. Title, snippet, text,
// and source_query are flagged rule-opaque per the LLM-authored content
// discipline (rules predicating on LLM-authored content create Goodhart
// feedback loops). Structural fields stay rule-matchable.
func registerWebPredicates() {
	vocabulary.Register(WebURL,
		vocabulary.WithDescription("Canonical URL this observation entity represents"),
		vocabulary.WithDataType(vocabulary.DataTypeString))

	vocabulary.Register(WebTitle,
		vocabulary.WithDescription("Search-result title returned by the provider; rule-opaque external prose"),
		vocabulary.WithDataType(vocabulary.DataTypeString),
		vocabulary.WithRuleOpaque(true))

	vocabulary.Register(WebSnippet,
		vocabulary.WithDescription("Search-result description returned by the provider; rule-opaque external prose"),
		vocabulary.WithDataType(vocabulary.DataTypeString),
		vocabulary.WithRuleOpaque(true))

	vocabulary.Register(WebText,
		vocabulary.WithDescription("Fetched body of an http_request (HTML→text, truncated); rule-opaque external prose"),
		vocabulary.WithDataType(vocabulary.DataTypeString),
		vocabulary.WithRuleOpaque(true))

	vocabulary.Register(WebSourceQuery,
		vocabulary.WithDescription("LLM-authored search query string; rule-opaque to prevent agents optimising queries toward rule triggers"),
		vocabulary.WithDataType(vocabulary.DataTypeString),
		vocabulary.WithRuleOpaque(true))

	vocabulary.Register(WebObservedAt,
		vocabulary.WithDescription("Wall-clock timestamp the web_search call saw this URL"),
		vocabulary.WithDataType(vocabulary.DataTypeDateTime))

	vocabulary.Register(WebFetchedAt,
		vocabulary.WithDescription("Wall-clock timestamp the http_request call pulled this URL's body"),
		vocabulary.WithDataType(vocabulary.DataTypeDateTime))

	vocabulary.Register(WebObservedBy,
		vocabulary.WithDescription("Loop entity ID that observed this URL via web_search"),
		vocabulary.WithDataType(vocabulary.DataTypeString))

	vocabulary.Register(WebFetchedBy,
		vocabulary.WithDescription("Loop entity ID that pulled this URL via http_request"),
		vocabulary.WithDataType(vocabulary.DataTypeString))

	vocabulary.Register(WebContentType,
		vocabulary.WithDescription("HTTP Content-Type header value from an http_request fetch"),
		vocabulary.WithDataType(vocabulary.DataTypeString))

	vocabulary.Register(WebStatusCode,
		vocabulary.WithDescription("HTTP response status code from an http_request fetch (2xx/3xx only)"),
		vocabulary.WithDataType(vocabulary.DataTypeInt))

	vocabulary.Register(WebTruncated,
		vocabulary.WithDescription("Whether the http_request body was truncated; rule-matchable structural flag"),
		vocabulary.WithDataType(vocabulary.DataTypeBool))
}

// registerStepPredicates registers predicates for trajectory step entities.
func registerStepPredicates() {
	vocabulary.Register(StepType,
		vocabulary.WithDescription("Category of the trajectory step (tool_call, model_call, context_compaction)"),
		vocabulary.WithDataType(vocabulary.DataTypeString))

	vocabulary.Register(StepIndex,
		vocabulary.WithDescription("Zero-based position of this step in the trajectory"),
		vocabulary.WithDataType(vocabulary.DataTypeInt))

	vocabulary.Register(StepLoop,
		vocabulary.WithDescription("Entity reference to the parent loop execution"),
		vocabulary.WithDataType(vocabulary.DataTypeString))

	vocabulary.Register(StepTimestamp,
		vocabulary.WithDescription("When this step occurred"),
		vocabulary.WithDataType(vocabulary.DataTypeDateTime))

	vocabulary.Register(StepDuration,
		vocabulary.WithDescription("Execution time of this step in milliseconds"),
		vocabulary.WithDataType(vocabulary.DataTypeInt))

	vocabulary.Register(StepToolName,
		vocabulary.WithDescription("Tool function name for tool_call steps"),
		vocabulary.WithDataType(vocabulary.DataTypeString))

	vocabulary.Register(StepModel,
		vocabulary.WithDescription("Model name for model_call steps"),
		vocabulary.WithDataType(vocabulary.DataTypeString))

	vocabulary.Register(StepTokensIn,
		vocabulary.WithDescription("Input tokens consumed by a model_call step"),
		vocabulary.WithDataType(vocabulary.DataTypeInt))

	vocabulary.Register(StepTokensOut,
		vocabulary.WithDescription("Output tokens produced by a model_call step"),
		vocabulary.WithDataType(vocabulary.DataTypeInt))

	vocabulary.Register(StepCapability,
		vocabulary.WithDescription("Role or purpose of this step (e.g., coding, planning, reviewing)"),
		vocabulary.WithDataType(vocabulary.DataTypeString))

	vocabulary.Register(StepProvider,
		vocabulary.WithDescription("LLM provider for this step's model endpoint"),
		vocabulary.WithDataType(vocabulary.DataTypeString))

	vocabulary.Register(StepRetries,
		vocabulary.WithDescription("Number of retries before this step succeeded"),
		vocabulary.WithDataType(vocabulary.DataTypeInt))

	vocabulary.Register(StepTokensEvicted,
		vocabulary.WithDescription("Tokens evicted during context compaction (context_compaction steps only)"),
		vocabulary.WithDataType(vocabulary.DataTypeInt))

	vocabulary.Register(StepTokensSummarized,
		vocabulary.WithDescription("Tokens in the compaction summary (context_compaction steps only)"),
		vocabulary.WithDataType(vocabulary.DataTypeInt))

	vocabulary.Register(StepUtilization,
		vocabulary.WithDescription("Context utilization ratio (0.0-1.0) at compaction trigger"),
		vocabulary.WithDataType(vocabulary.DataTypeFloat),
		vocabulary.WithRange("0-1"))

	vocabulary.Register(StepToolStatus,
		vocabulary.WithDescription("Terminal status of a tool_call step (success, failed)"),
		vocabulary.WithDataType(vocabulary.DataTypeString))

	vocabulary.Register(StepErrorMessage,
		vocabulary.WithDescription("Raw error text for a failed tool_call step"),
		vocabulary.WithDataType(vocabulary.DataTypeString))

	vocabulary.Register(StepErrorCategory,
		vocabulary.WithDescription("Typed error category (timeout, not_found, invalid_args, permission, network, external, internal, unknown)"),
		vocabulary.WithDataType(vocabulary.DataTypeString))
}

// registerIdentityPredicates registers predicates for DID-based agent identity.
func registerIdentityPredicates() {
	vocabulary.Register(IdentityDID,
		vocabulary.WithDescription("Decentralized identifier for an agent"),
		vocabulary.WithDataType(vocabulary.DataTypeString))

	vocabulary.Register(IdentityCredential,
		vocabulary.WithDescription("Verifiable credential held by the agent"),
		vocabulary.WithDataType(vocabulary.DataTypeString))

	vocabulary.Register(IdentityIssuer,
		vocabulary.WithDescription("DID of an entity that issued a credential"),
		vocabulary.WithDataType(vocabulary.DataTypeString))

	vocabulary.Register(IdentityVerified,
		vocabulary.WithDescription("Whether the identity has been verified"),
		vocabulary.WithDataType(vocabulary.DataTypeBool))

	vocabulary.Register(IdentityDisplayName,
		vocabulary.WithDescription("Human-readable name for the agent"),
		vocabulary.WithDataType(vocabulary.DataTypeString))

	vocabulary.Register(IdentityRole,
		vocabulary.WithDescription("Agent's role in the system (e.g., architect, editor, reviewer)"),
		vocabulary.WithDataType(vocabulary.DataTypeString))
}

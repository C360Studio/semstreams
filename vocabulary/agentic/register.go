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
	registerIdentityPredicates()
}

// registerIntentPredicates registers predicates for agent intentions and goals.
func registerIntentPredicates() {
	vocabulary.Register(IntentGoal,
		vocabulary.WithDescription("The objective or goal an agent aims to achieve"),
		vocabulary.WithDataType("string"),
		vocabulary.WithIRI(IriIntent))

	vocabulary.Register(IntentType,
		vocabulary.WithDescription("Category of intent (e.g., data-analysis, content-generation)"),
		vocabulary.WithDataType("string"),
		vocabulary.WithIRI(IriHasIntentType))

	vocabulary.Register(IntentParameter,
		vocabulary.WithDescription("Typed parameter for the intent"),
		vocabulary.WithDataType("string"),
		vocabulary.WithIRI(IriHasParameter))

	vocabulary.Register(IntentAuthorized,
		vocabulary.WithDescription("Delegation authorizing this intent"),
		vocabulary.WithDataType("string"),
		vocabulary.WithIRI(IriAuthorizedBy))

	vocabulary.Register(IntentProduces,
		vocabulary.WithDescription("Action produced by this intent"),
		vocabulary.WithDataType("string"),
		vocabulary.WithIRI(IriProducesAction))
}

// registerCapabilityPredicates registers predicates for agent capabilities.
func registerCapabilityPredicates() {
	vocabulary.Register(CapabilityName,
		vocabulary.WithDescription("Identifier for an agent capability"),
		vocabulary.WithDataType("string"),
		vocabulary.WithIRI(IriCapability))

	vocabulary.Register(CapabilityDescription,
		vocabulary.WithDescription("Human-readable description of the capability"),
		vocabulary.WithDataType("string"))

	vocabulary.Register(CapabilityExpression,
		vocabulary.WithDescription("Semantic fingerprint for capability matching and embedding"),
		vocabulary.WithDataType("string"),
		vocabulary.WithIRI(IriCapabilityExpression))

	vocabulary.Register(CapabilityConfidence,
		vocabulary.WithDescription("Agent's self-assessed confidence in capability (0.0-1.0)"),
		vocabulary.WithDataType("float64"),
		vocabulary.WithRange("0-1"),
		vocabulary.WithIRI(IriCapabilityConfidence))

	vocabulary.Register(CapabilitySkill,
		vocabulary.WithDescription("Atomic skill implementing the capability"),
		vocabulary.WithDataType("string"),
		vocabulary.WithIRI(IriHasSkill))

	vocabulary.Register(CapabilityConstraint,
		vocabulary.WithDescription("Execution constraint on the capability"),
		vocabulary.WithDataType("string"),
		vocabulary.WithIRI(IriCapabilityConstraint))

	vocabulary.Register(CapabilityPermission,
		vocabulary.WithDescription("Permission required for the capability"),
		vocabulary.WithDataType("string"),
		vocabulary.WithIRI(IriRequiresPermission))
}

// registerDelegationPredicates registers predicates for authority delegation.
func registerDelegationPredicates() {
	vocabulary.Register(DelegationFrom,
		vocabulary.WithDescription("Agent granting delegated authority"),
		vocabulary.WithDataType("string"),
		vocabulary.WithIRI(IriDelegatedBy),
		vocabulary.WithInverseOf(DelegationTo))

	vocabulary.Register(DelegationTo,
		vocabulary.WithDescription("Agent receiving delegated authority"),
		vocabulary.WithDataType("string"),
		vocabulary.WithIRI(IriDelegatesTo),
		vocabulary.WithInverseOf(DelegationFrom))

	vocabulary.Register(DelegationScope,
		vocabulary.WithDescription("Boundary of delegated authority"),
		vocabulary.WithDataType("string"),
		vocabulary.WithIRI(IriDelegationScope))

	vocabulary.Register(DelegationCapability,
		vocabulary.WithDescription("Capability allowed by the delegation"),
		vocabulary.WithDataType("string"),
		vocabulary.WithIRI(IriAllowedCapability))

	vocabulary.Register(DelegationValidFrom,
		vocabulary.WithDescription("When the delegation becomes valid"),
		vocabulary.WithDataType("time.Time"),
		vocabulary.WithIRI(IriValidFrom))

	vocabulary.Register(DelegationValidUntil,
		vocabulary.WithDescription("When the delegation expires"),
		vocabulary.WithDataType("time.Time"),
		vocabulary.WithIRI(IriValidUntil))

	vocabulary.Register(DelegationChain,
		vocabulary.WithDescription("Multi-level delegation chain identifier"),
		vocabulary.WithDataType("string"),
		vocabulary.WithIRI(IriDelegationChain))
}

// registerAccountabilityPredicates registers predicates for accountability tracking.
func registerAccountabilityPredicates() {
	vocabulary.Register(AccountabilityActor,
		vocabulary.WithDescription("Agent performing the accountable action"),
		vocabulary.WithDataType("string"),
		vocabulary.WithIRI(IriActor))

	vocabulary.Register(AccountabilityAction,
		vocabulary.WithDescription("Action being accounted for"),
		vocabulary.WithDataType("string"))

	vocabulary.Register(AccountabilityAssigned,
		vocabulary.WithDescription("Party assigned responsibility"),
		vocabulary.WithDataType("string"),
		vocabulary.WithIRI(IriAssignedTo))

	vocabulary.Register(AccountabilityRationale,
		vocabulary.WithDescription("Reasoning for the responsibility attribution"),
		vocabulary.WithDataType("string"),
		vocabulary.WithIRI(IriRationale))

	vocabulary.Register(AccountabilityCompliance,
		vocabulary.WithDescription("Compliance assessment result"),
		vocabulary.WithDataType("string"),
		vocabulary.WithIRI(IriComplianceAssessment))

	vocabulary.Register(AccountabilityTimestamp,
		vocabulary.WithDescription("When the accountability event occurred"),
		vocabulary.WithDataType("time.Time"))
}

// registerExecutionPredicates registers predicates for execution context.
func registerExecutionPredicates() {
	vocabulary.Register(ExecutionEnvironment,
		vocabulary.WithDescription("Runtime environment type (sandbox, container, etc.)"),
		vocabulary.WithDataType("string"),
		vocabulary.WithIRI(IriExecutionEnvironment))

	vocabulary.Register(ExecutionSecurity,
		vocabulary.WithDescription("Security context for execution"),
		vocabulary.WithDataType("string"),
		vocabulary.WithIRI(IriSecurityContext))

	vocabulary.Register(ExecutionConstraint,
		vocabulary.WithDescription("Resource constraint for execution"),
		vocabulary.WithDataType("string"),
		vocabulary.WithIRI(IriResourceConstraint))

	vocabulary.Register(ExecutionRateLimit,
		vocabulary.WithDescription("Rate limiting constraint"),
		vocabulary.WithDataType("string"),
		vocabulary.WithIRI(IriRateLimit))

	vocabulary.Register(ExecutionBudget,
		vocabulary.WithDescription("Cost or resource budget for execution"),
		vocabulary.WithDataType("string"),
		vocabulary.WithIRI(IriBudget))

	vocabulary.Register(ExecutionInput,
		vocabulary.WithDescription("Input state for execution"),
		vocabulary.WithDataType("string"))

	vocabulary.Register(ExecutionOutput,
		vocabulary.WithDescription("Output state from execution"),
		vocabulary.WithDataType("string"))
}

// registerActionPredicates registers predicates for concrete actions.
func registerActionPredicates() {
	vocabulary.Register(ActionType,
		vocabulary.WithDescription("Category of the action"),
		vocabulary.WithDataType("string"))

	vocabulary.Register(ActionExecutedBy,
		vocabulary.WithDescription("Agent that executed the action"),
		vocabulary.WithDataType("string"))

	vocabulary.Register(ActionProduced,
		vocabulary.WithDescription("Artifact produced by the action"),
		vocabulary.WithDataType("string"),
		vocabulary.WithIRI(IriArtifact))

	vocabulary.Register(ActionContext,
		vocabulary.WithDescription("Execution context for the action"),
		vocabulary.WithDataType("string"))

	vocabulary.Register(ActionTrace,
		vocabulary.WithDescription("Trace or audit record for the action"),
		vocabulary.WithDataType("string"),
		vocabulary.WithIRI(IriTraceEvent))
}

// registerTaskPredicates registers predicates for task management.
func registerTaskPredicates() {
	vocabulary.Register(TaskAssigned,
		vocabulary.WithDescription("Agent assigned to the task"),
		vocabulary.WithDataType("string"))

	vocabulary.Register(TaskCapability,
		vocabulary.WithDescription("Capability required for the task"),
		vocabulary.WithDataType("string"))

	vocabulary.Register(TaskSubtask,
		vocabulary.WithDescription("Child task in hierarchical decomposition"),
		vocabulary.WithDataType("string"))

	vocabulary.Register(TaskDependency,
		vocabulary.WithDescription("Task that must complete before this one"),
		vocabulary.WithDataType("string"))

	vocabulary.Register(TaskStatus,
		vocabulary.WithDescription("Current status of the task"),
		vocabulary.WithDataType("string"))
}

// registerModelPredicates registers predicates for LLM model endpoint entities.
func registerModelPredicates() {
	vocabulary.Register(ModelProvider,
		vocabulary.WithDescription("API provider type (anthropic, ollama, openai, openrouter)"),
		vocabulary.WithDataType("string"))

	vocabulary.Register(ModelName,
		vocabulary.WithDescription("Model identifier sent to the provider"),
		vocabulary.WithDataType("string"))

	vocabulary.Register(ModelMaxTokens,
		vocabulary.WithDescription("Context window size in tokens"),
		vocabulary.WithDataType("int"))

	vocabulary.Register(ModelSupportsTools,
		vocabulary.WithDescription("Whether the endpoint supports tool calling"),
		vocabulary.WithDataType("bool"))

	vocabulary.Register(ModelInputPrice,
		vocabulary.WithDescription("Cost per 1M input tokens in USD"),
		vocabulary.WithDataType("float64"))

	vocabulary.Register(ModelOutputPrice,
		vocabulary.WithDescription("Cost per 1M output tokens in USD"),
		vocabulary.WithDataType("float64"))

	vocabulary.Register(ModelEndpointURL,
		vocabulary.WithDescription("API endpoint URL for the model"),
		vocabulary.WithDataType("string"))

	vocabulary.Register(ModelRateLimit,
		vocabulary.WithDescription("Requests per minute limit for the endpoint"),
		vocabulary.WithDataType("int"))
}

// registerLoopPredicates registers predicates for agentic loop execution entities.
func registerLoopPredicates() {
	vocabulary.Register(LoopOutcome,
		vocabulary.WithDescription("Terminal outcome of the loop execution (success, failed, cancelled)"),
		vocabulary.WithDataType("string"))

	vocabulary.Register(LoopRole,
		vocabulary.WithDescription("Role used during this loop execution"),
		vocabulary.WithDataType("string"))

	vocabulary.Register(LoopModelUsed,
		vocabulary.WithDescription("Entity reference to the model endpoint used"),
		vocabulary.WithDataType("string"))

	vocabulary.Register(LoopIterations,
		vocabulary.WithDescription("Number of LLM iterations executed in this loop"),
		vocabulary.WithDataType("int"))

	vocabulary.Register(LoopTokensIn,
		vocabulary.WithDescription("Total input tokens consumed across all iterations"),
		vocabulary.WithDataType("int"))

	vocabulary.Register(LoopTokensOut,
		vocabulary.WithDescription("Total output tokens consumed across all iterations"),
		vocabulary.WithDataType("int"))

	vocabulary.Register(LoopCostUSD,
		vocabulary.WithDescription("Computed cost in USD for this loop execution"),
		vocabulary.WithDataType("float64"))

	vocabulary.Register(LoopTask,
		vocabulary.WithDescription("Task ID this loop execution served"),
		vocabulary.WithDataType("string"))

	vocabulary.Register(LoopParent,
		vocabulary.WithDescription("Entity reference to the parent loop entity"),
		vocabulary.WithDataType("string"))

	vocabulary.Register(LoopWorkflow,
		vocabulary.WithDescription("Workflow slug this loop belongs to"),
		vocabulary.WithDataType("string"))

	vocabulary.Register(LoopWorkflowStep,
		vocabulary.WithDescription("Step within the workflow for this loop"),
		vocabulary.WithDataType("string"))

	vocabulary.Register(LoopEndedAt,
		vocabulary.WithDescription("Terminal timestamp for this loop (completion, failure, or cancellation)"),
		vocabulary.WithDataType("time.Time"))

	vocabulary.Register(LoopUser,
		vocabulary.WithDescription("User ID who initiated this loop"),
		vocabulary.WithDataType("string"))

	vocabulary.Register(LoopHasStep,
		vocabulary.WithDescription("Entity reference to a trajectory step within this loop (multi-valued)"),
		vocabulary.WithDataType("string"))
}

// registerStepPredicates registers predicates for trajectory step entities.
func registerStepPredicates() {
	vocabulary.Register(StepType,
		vocabulary.WithDescription("Category of the trajectory step (tool_call, model_call, context_compaction)"),
		vocabulary.WithDataType("string"))

	vocabulary.Register(StepIndex,
		vocabulary.WithDescription("Zero-based position of this step in the trajectory"),
		vocabulary.WithDataType("int"))

	vocabulary.Register(StepLoop,
		vocabulary.WithDescription("Entity reference to the parent loop execution"),
		vocabulary.WithDataType("string"))

	vocabulary.Register(StepTimestamp,
		vocabulary.WithDescription("When this step occurred"),
		vocabulary.WithDataType("time.Time"))

	vocabulary.Register(StepDuration,
		vocabulary.WithDescription("Execution time of this step in milliseconds"),
		vocabulary.WithDataType("int64"))

	vocabulary.Register(StepToolName,
		vocabulary.WithDescription("Tool function name for tool_call steps"),
		vocabulary.WithDataType("string"))

	vocabulary.Register(StepModel,
		vocabulary.WithDescription("Model name for model_call steps"),
		vocabulary.WithDataType("string"))

	vocabulary.Register(StepTokensIn,
		vocabulary.WithDescription("Input tokens consumed by a model_call step"),
		vocabulary.WithDataType("int"))

	vocabulary.Register(StepTokensOut,
		vocabulary.WithDescription("Output tokens produced by a model_call step"),
		vocabulary.WithDataType("int"))

	vocabulary.Register(StepCapability,
		vocabulary.WithDescription("Role or purpose of this step (e.g., coding, planning, reviewing)"),
		vocabulary.WithDataType("string"))

	vocabulary.Register(StepProvider,
		vocabulary.WithDescription("LLM provider for this step's model endpoint"),
		vocabulary.WithDataType("string"))

	vocabulary.Register(StepRetries,
		vocabulary.WithDescription("Number of retries before this step succeeded"),
		vocabulary.WithDataType("int"))

	vocabulary.Register(StepTokensEvicted,
		vocabulary.WithDescription("Tokens evicted during context compaction (context_compaction steps only)"),
		vocabulary.WithDataType("int"))

	vocabulary.Register(StepTokensSummarized,
		vocabulary.WithDescription("Tokens in the compaction summary (context_compaction steps only)"),
		vocabulary.WithDataType("int"))

	vocabulary.Register(StepUtilization,
		vocabulary.WithDescription("Context utilization ratio (0.0-1.0) at compaction trigger"),
		vocabulary.WithDataType("float64"),
		vocabulary.WithRange("0-1"))

	vocabulary.Register(StepToolStatus,
		vocabulary.WithDescription("Terminal status of a tool_call step (success, failed)"),
		vocabulary.WithDataType("string"))

	vocabulary.Register(StepErrorMessage,
		vocabulary.WithDescription("Raw error text for a failed tool_call step"),
		vocabulary.WithDataType("string"))

	vocabulary.Register(StepErrorCategory,
		vocabulary.WithDescription("Typed error category (timeout, not_found, invalid_args, permission, network, external, internal, unknown)"),
		vocabulary.WithDataType("string"))
}

// registerIdentityPredicates registers predicates for DID-based agent identity.
func registerIdentityPredicates() {
	vocabulary.Register(IdentityDID,
		vocabulary.WithDescription("Decentralized identifier for an agent"),
		vocabulary.WithDataType("string"))

	vocabulary.Register(IdentityCredential,
		vocabulary.WithDescription("Verifiable credential held by the agent"),
		vocabulary.WithDataType("string"))

	vocabulary.Register(IdentityIssuer,
		vocabulary.WithDescription("DID of an entity that issued a credential"),
		vocabulary.WithDataType("string"))

	vocabulary.Register(IdentityVerified,
		vocabulary.WithDescription("Whether the identity has been verified"),
		vocabulary.WithDataType("bool"))

	vocabulary.Register(IdentityDisplayName,
		vocabulary.WithDescription("Human-readable name for the agent"),
		vocabulary.WithDataType("string"))

	vocabulary.Register(IdentityRole,
		vocabulary.WithDescription("Agent's role in the system (e.g., architect, editor, reviewer)"),
		vocabulary.WithDataType("string"))
}

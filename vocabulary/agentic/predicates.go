package agentic

// Agentic Vocabulary Predicates
//
// These predicates use the SemStreams three-level dotted notation for NATS
// compatibility while mapping to W3C S-Agent-Comm IRIs for standards compliance.
//
// Domain: "agent" - All agentic predicates share this domain prefix.
//
// Categories:
//   - intent: Goals and objectives an agent aims to achieve
//   - capability: Abilities an agent has to perform actions
//   - delegation: Authority transfer between agents
//   - accountability: Responsibility tracking and compliance
//   - execution: Runtime environment and constraints
//   - action: Concrete execution events
//   - task: Work units exchanged between agents

// Intent Predicates
//
// Express what an agent aims to achieve, including goals, parameters, and
// the authorization chain for the intent.
const (
	// IntentGoal is the objective or goal statement an agent aims to achieve.
	// Example: "analyze customer feedback and identify emerging themes"
	// DataType: string
	// IRI: agent-ontology:Intent
	IntentGoal = "agent.intent.goal"

	// IntentType is the category or classification of the intent.
	// Example: "data-analysis", "content-generation", "decision-support"
	// DataType: string
	// IRI: agent-ontology:hasIntentType
	IntentType = "agent.intent.type"

	// IntentParameter is a typed parameter for the intent.
	// Example: "input_dataset=customer_reviews_2024"
	// DataType: string
	// IRI: agent-ontology:hasParameter
	IntentParameter = "agent.intent.parameter"

	// IntentAuthorized is the delegation authorizing this intent.
	// Example: entity ID of the delegation that permits this intent
	// DataType: string (entity ID)
	// IRI: agent-ontology:authorizedBy
	IntentAuthorized = "agent.intent.authorized"

	// IntentProduces is the action produced by this intent.
	// Example: entity ID of the resulting action
	// DataType: string (entity ID)
	// IRI: agent-ontology:producesAction
	IntentProduces = "agent.intent.produces"
)

// Capability Predicates
//
// Express what an agent can do, including skills, confidence levels,
// constraints, and required permissions.
const (
	// CapabilityName is the identifier for a capability.
	// Example: "text-summarization", "code-review", "data-visualization"
	// DataType: string
	// IRI: agent-ontology:Capability
	CapabilityName = "agent.capability.name"

	// CapabilityDescription is a human-readable description of the capability.
	// Example: "Summarizes long documents while preserving key information"
	// DataType: string
	CapabilityDescription = "agent.capability.description"

	// CapabilityExpression is a semantic fingerprint for capability matching.
	// Used for embedding-based capability discovery and matching.
	// Example: "analyze text extract themes identify patterns"
	// DataType: string
	// IRI: agent-ontology:capabilityExpression
	CapabilityExpression = "agent.capability.expression"

	// CapabilityConfidence is the agent's self-assessed confidence (0.0-1.0).
	// Example: 0.95 (high confidence in this capability)
	// DataType: float64
	// Range: 0-1
	// IRI: agent-ontology:capabilityConfidence
	CapabilityConfidence = "agent.capability.confidence"

	// CapabilitySkill is an atomic skill implementing the capability.
	// Example: entity ID of a specific skill
	// DataType: string (entity ID)
	// IRI: agent-ontology:hasSkill
	CapabilitySkill = "agent.capability.skill"

	// CapabilityConstraint is an execution constraint on the capability.
	// Example: "max_tokens=4096", "requires_gpu=true"
	// DataType: string
	// IRI: agent-ontology:CapabilityConstraint
	CapabilityConstraint = "agent.capability.constraint"

	// CapabilityPermission is a required permission for the capability.
	// Example: "file_system_read", "network_access", "tool_execution"
	// DataType: string
	// IRI: agent-ontology:requiresPermission
	CapabilityPermission = "agent.capability.permission"
)

// Delegation Predicates
//
// Express authority transfer between agents, including scope, validity,
// and delegation chains.
const (
	// DelegationFrom is the agent granting delegated authority.
	// Example: entity ID of the delegating agent
	// DataType: string (entity ID)
	// IRI: agent-ontology:delegatedBy
	// InverseOf: DelegationTo
	DelegationFrom = "agent.delegation.from"

	// DelegationTo is the agent receiving delegated authority.
	// Example: entity ID of the delegate agent
	// DataType: string (entity ID)
	// IRI: agent-ontology:delegatesTo
	// InverseOf: DelegationFrom
	DelegationTo = "agent.delegation.to"

	// DelegationScope is the boundary of delegated authority.
	// Example: "repository:acme/project", "domain:customer-service"
	// DataType: string
	// IRI: agent-ontology:DelegationScope
	DelegationScope = "agent.delegation.scope"

	// DelegationCapability is a capability allowed by the delegation.
	// Example: entity ID of an allowed capability
	// DataType: string (entity ID)
	// IRI: agent-ontology:allowedCapability
	DelegationCapability = "agent.delegation.capability"

	// DelegationValidFrom is when the delegation becomes valid.
	// Example: "2024-01-15T09:00:00Z"
	// DataType: time.Time
	// IRI: agent-ontology:validFrom
	DelegationValidFrom = "agent.delegation.valid_from"

	// DelegationValidUntil is when the delegation expires.
	// Example: "2024-12-31T23:59:59Z"
	// DataType: time.Time
	// IRI: agent-ontology:validUntil
	DelegationValidUntil = "agent.delegation.valid_until"

	// DelegationChain is a multi-level delegation chain.
	// Example: entity ID of the delegation chain
	// DataType: string (entity ID)
	// IRI: agent-ontology:DelegationChain
	DelegationChain = "agent.delegation.chain"
)

// Accountability Predicates
//
// Express responsibility attribution, compliance assessment, and audit trails
// for agent actions.
const (
	// AccountabilityActor is the agent performing an accountable action.
	// Example: entity ID of the acting agent
	// DataType: string (entity ID)
	// IRI: agent-ontology:actor
	AccountabilityActor = "agent.accountability.actor"

	// AccountabilityAction is the action being accounted for.
	// Example: entity ID of the action
	// DataType: string (entity ID)
	AccountabilityAction = "agent.accountability.action"

	// AccountabilityAssigned is the party assigned responsibility.
	// Example: entity ID of the responsible agent or person
	// DataType: string (entity ID)
	// IRI: agent-ontology:assignedTo
	AccountabilityAssigned = "agent.accountability.assigned"

	// AccountabilityRationale is the reasoning for the attribution.
	// Example: "Agent executed action under delegated authority from user"
	// DataType: string
	// IRI: agent-ontology:rationale
	AccountabilityRationale = "agent.accountability.rationale"

	// AccountabilityCompliance is the compliance assessment result.
	// Example: "compliant", "non-compliant", "pending-review"
	// DataType: string
	// IRI: agent-ontology:ComplianceAssessment
	AccountabilityCompliance = "agent.accountability.compliance"

	// AccountabilityTimestamp is when the accountability event occurred.
	// Example: "2024-06-15T14:30:00Z"
	// DataType: time.Time
	AccountabilityTimestamp = "agent.accountability.timestamp"
)

// Execution Context Predicates
//
// Express runtime environment, security context, and resource constraints
// for agent execution.
const (
	// ExecutionEnvironment is the runtime environment type.
	// Example: "sandbox", "container", "bare-metal", "cloud-function"
	// DataType: string
	// IRI: agent-ontology:ExecutionEnvironment
	ExecutionEnvironment = "agent.execution.environment"

	// ExecutionSecurity is the security context for execution.
	// Example: "restricted", "elevated", "system"
	// DataType: string
	// IRI: agent-ontology:SecurityContext
	ExecutionSecurity = "agent.execution.security"

	// ExecutionConstraint is a resource constraint for execution.
	// Example: "memory_limit=1GB", "cpu_limit=2cores"
	// DataType: string
	// IRI: agent-ontology:ResourceConstraint
	ExecutionConstraint = "agent.execution.constraint"

	// ExecutionRateLimit is a rate limiting constraint.
	// Example: "100/minute", "1000/hour"
	// DataType: string
	// IRI: agent-ontology:RateLimit
	ExecutionRateLimit = "agent.execution.rate_limit"

	// ExecutionBudget is a cost or resource budget.
	// Example: "tokens=100000", "cost_usd=10.00"
	// DataType: string
	// IRI: agent-ontology:Budget
	ExecutionBudget = "agent.execution.budget"

	// ExecutionInput is the input state for execution.
	// Example: entity ID of the input artifact or state
	// DataType: string (entity ID)
	ExecutionInput = "agent.execution.input"

	// ExecutionOutput is the output state from execution.
	// Example: entity ID of the output artifact or state
	// DataType: string (entity ID)
	ExecutionOutput = "agent.execution.output"
)

// Action Predicates
//
// Express concrete execution events, including the executing agent,
// produced artifacts, and trace records.
const (
	// ActionType is the category of the action.
	// Example: "tool-call", "api-request", "file-write", "decision"
	// DataType: string
	ActionType = "agent.action.type"

	// ActionExecutedBy is the agent that executed the action.
	// Example: entity ID of the executing agent
	// DataType: string (entity ID)
	ActionExecutedBy = "agent.action.executed_by"

	// ActionProduced is an artifact produced by the action.
	// Example: entity ID of the produced artifact
	// DataType: string (entity ID)
	// IRI: agent-ontology:Artifact
	ActionProduced = "agent.action.produced"

	// ActionContext is the execution context for the action.
	// Example: entity ID of the execution context
	// DataType: string (entity ID)
	ActionContext = "agent.action.context"

	// ActionTrace is a trace or audit record for the action.
	// Example: entity ID of the trace event
	// DataType: string (entity ID)
	// IRI: agent-ontology:TraceEvent
	ActionTrace = "agent.action.trace"
)

// Task Predicates
//
// Express work units exchanged between agents, including assignment,
// capability requirements, dependencies, and status.
const (
	// TaskAssigned is the agent assigned to the task.
	// Example: entity ID of the assigned agent
	// DataType: string (entity ID)
	TaskAssigned = "agent.task.assigned"

	// TaskCapability is a capability required for the task.
	// Example: entity ID of the required capability
	// DataType: string (entity ID)
	TaskCapability = "agent.task.capability"

	// TaskSubtask is a child task in hierarchical decomposition.
	// Example: entity ID of the subtask
	// DataType: string (entity ID)
	TaskSubtask = "agent.task.subtask"

	// TaskDependency is a task that must complete before this one.
	// Example: entity ID of the dependency task
	// DataType: string (entity ID)
	TaskDependency = "agent.task.dependency"

	// TaskStatus is the current status of the task.
	// Example: "pending", "in_progress", "completed", "failed", "cancelled"
	// DataType: string
	TaskStatus = "agent.task.status"
)

// Identity Predicates
//
// Express DID-based cryptographic identity for agents, including
// decentralized identifiers, verifiable credentials, and issuers.
const (
	// IdentityDID is the decentralized identifier for an agent.
	// Example: "did:key:z6MkhaXgBZDvotDkL5257faiztiGiC2QtKLGpbnnEGta2doK"
	// DataType: string
	// IRI: agent-ontology:Identity
	IdentityDID = "agent.identity.did"

	// IdentityCredential is a verifiable credential held by the agent.
	// Example: entity ID of the credential
	// DataType: string (entity ID)
	// IRI: agent-ontology:hasCredential
	IdentityCredential = "agent.identity.credential"

	// IdentityIssuer is the DID of an entity that issued a credential.
	// Example: "did:key:z6MkIssuer..."
	// DataType: string
	// IRI: agent-ontology:issuedBy
	IdentityIssuer = "agent.identity.issuer"

	// IdentityVerified indicates if the identity has been verified.
	// Example: true
	// DataType: bool
	// IRI: agent-ontology:verified
	IdentityVerified = "agent.identity.verified"

	// IdentityDisplayName is the human-readable name for the agent.
	// Example: "Code Review Agent"
	// DataType: string
	IdentityDisplayName = "agent.identity.display_name"

	// IdentityRole is the agent's role in the system.
	// Example: "architect", "editor", "reviewer"
	// DataType: string
	IdentityRole = "agent.identity.role"
)

// Model Predicates
//
// Express properties of LLM model endpoints registered in the model registry,
// enabling graph queries about which models are available and their capabilities.
const (
	// ModelProvider is the API provider type for the model endpoint.
	// Example: "anthropic", "ollama", "openai", "openrouter"
	// DataType: string
	ModelProvider = "agent.model.provider"

	// ModelName is the model identifier sent to the provider.
	// Example: "claude-opus-4-5", "llama3.2", "gpt-4o"
	// DataType: string
	ModelName = "agent.model.name"

	// ModelMaxTokens is the context window size in tokens.
	// Example: 200000
	// DataType: int
	ModelMaxTokens = "agent.model.max_tokens"

	// ModelSupportsTools indicates whether the endpoint supports tool calling.
	// Example: true
	// DataType: bool
	ModelSupportsTools = "agent.model.supports_tools"

	// ModelInputPrice is the cost per 1M input tokens in USD.
	// Example: 3.00
	// DataType: float64
	ModelInputPrice = "agent.model.input_price"

	// ModelOutputPrice is the cost per 1M output tokens in USD.
	// Example: 15.00
	// DataType: float64
	ModelOutputPrice = "agent.model.output_price"

	// ModelEndpointURL is the API endpoint URL for the model.
	// Example: "https://api.anthropic.com/v1"
	// DataType: string
	ModelEndpointURL = "agent.model.endpoint_url"

	// ModelRateLimit is the requests per minute limit for the endpoint.
	// Example: 60
	// DataType: int
	ModelRateLimit = "agent.model.rate_limit"
)

// Loop Predicates
//
// Express facts about agentic loop executions, including outcome, resource
// usage, cost, and relationships to model endpoints and parent loops.
const (
	// LoopOutcome is the terminal outcome of the loop execution.
	// Example: "success", "failed", "cancelled"
	// DataType: string
	LoopOutcome = "agent.loop.outcome"

	// LoopRole is the role used during this loop execution.
	// Example: "architect", "editor", "reviewer"
	// DataType: string
	LoopRole = "agent.loop.role"

	// LoopModelUsed is an entity reference to the model endpoint entity used.
	// Example: entity ID of the model endpoint
	// DataType: string (entity ID)
	LoopModelUsed = "agent.loop.model_used"

	// LoopIterations is the number of LLM iterations executed in this loop.
	// Example: 12
	// DataType: int
	LoopIterations = "agent.loop.iterations"

	// LoopTokensIn is the total input tokens consumed across all iterations.
	// Example: 48320
	// DataType: int
	LoopTokensIn = "agent.loop.tokens_in"

	// LoopTokensOut is the total output tokens consumed across all iterations.
	// Example: 8192
	// DataType: int
	LoopTokensOut = "agent.loop.tokens_out"

	// LoopCostUSD is the computed cost in USD for this loop execution.
	// Example: 0.2754
	// DataType: float64
	LoopCostUSD = "agent.loop.cost_usd"

	// LoopTask is the task ID this loop execution served.
	// Example: "task-abc123"
	// DataType: string
	LoopTask = "agent.loop.task"

	// LoopParent is an entity reference to the parent loop entity.
	// Example: entity ID of the parent loop
	// DataType: string (entity ID)
	LoopParent = "agent.loop.parent"

	// LoopWorkflow is the workflow slug this loop belongs to.
	// Example: "code-review", "feature-implementation"
	// DataType: string
	LoopWorkflow = "agent.loop.workflow"

	// LoopWorkflowStep is the step within the workflow for this loop.
	// Example: "draft", "review", "revise"
	// DataType: string
	LoopWorkflowStep = "agent.loop.workflow_step"

	// LoopEndedAt is the terminal timestamp for this loop (completion, failure, or cancellation).
	// Example: "2026-03-13T14:22:00Z"
	// DataType: time.Time
	LoopEndedAt = "agent.loop.ended_at"

	// LoopUser is the user ID who initiated this loop.
	// Example: "user-xyz789"
	// DataType: string
	LoopUser = "agent.loop.user"

	// LoopHasStep is an entity reference to a trajectory step within this loop.
	// Multi-valued: one triple per step.
	// Example: entity ID of a trajectory step
	// DataType: string (entity ID)
	LoopHasStep = "agent.loop.has_step"

	// LoopDescription is the user task prompt that initiated this loop, stored
	// as text so BM25/NL search can find loops by topic. The `.description`
	// suffix is already in the embedding pipeline's default text suffixes,
	// so this triple is auto-indexed.
	// Example: "Investigate MQTT retained-message behavior"
	// DataType: string
	LoopDescription = "agent.loop.description"
)

// Step Predicates
//
// Express facts about individual trajectory steps within a loop execution,
// including step type, ordering, timing, and type-specific metadata.
// Large content (tool arguments, tool results, model responses) is stored
// in ObjectStore via the ContentStorable pattern, not in triples.
const (
	// StepType is the category of the trajectory step.
	// Example: "tool_call", "model_call"
	// DataType: string
	StepType = "agent.step.type"

	// StepIndex is the zero-based position of this step in the trajectory.
	// Example: 0, 1, 2
	// DataType: int
	StepIndex = "agent.step.index"

	// StepLoop is an entity reference to the parent loop execution.
	// Example: entity ID of the loop execution
	// DataType: string (entity ID)
	StepLoop = "agent.step.loop"

	// StepTimestamp is when this step occurred.
	// Example: "2026-03-17T14:22:00Z"
	// DataType: time.Time
	StepTimestamp = "agent.step.timestamp"

	// StepDuration is the execution time of this step in milliseconds.
	// Example: 1234
	// DataType: int64
	StepDuration = "agent.step.duration_ms"

	// StepToolName is the tool function name for tool_call steps.
	// Example: "web_search", "graph_query", "http_request"
	// DataType: string
	StepToolName = "agent.step.tool_name"

	// StepModel is the model name for model_call steps.
	// Example: "claude-sonnet", "gpt-4o"
	// DataType: string
	StepModel = "agent.step.model"

	// StepTokensIn is the input tokens consumed by a model_call step.
	// Example: 4832
	// DataType: int
	StepTokensIn = "agent.step.tokens_in"

	// StepTokensOut is the output tokens produced by a model_call step.
	// Example: 819
	// DataType: int
	StepTokensOut = "agent.step.tokens_out"

	// StepCapability is the role or purpose of this step.
	// For model_call steps: the task role (e.g., "coding", "planning", "reviewing", "reasoning").
	// Example: "coding"
	// DataType: string
	StepCapability = "agent.step.capability"

	// StepProvider is the LLM provider for this step's model endpoint.
	// Example: "anthropic", "openai", "ollama"
	// DataType: string
	StepProvider = "agent.step.provider"

	// StepRetries is the number of retries before this step succeeded.
	// Example: 2
	// DataType: int
	StepRetries = "agent.step.retries"

	// StepTokensEvicted is the number of tokens evicted during context compaction.
	// Only set on context_compaction steps.
	// Example: 12000
	// DataType: int
	StepTokensEvicted = "agent.step.tokens_evicted"

	// StepTokensSummarized is the number of tokens in the compaction summary.
	// Only set on context_compaction steps.
	// Example: 800
	// DataType: int
	StepTokensSummarized = "agent.step.tokens_summarized"

	// StepUtilization is the context utilization ratio (0.0-1.0) at compaction trigger.
	// Only set on context_compaction steps.
	// Example: 0.72
	// DataType: float64
	StepUtilization = "agent.step.utilization"

	// StepToolStatus is the terminal status of a tool_call step.
	// Example: "success", "failed"
	// DataType: string
	StepToolStatus = "agent.step.tool_status"

	// StepErrorMessage is the raw error text for a failed tool_call step.
	// Omitted on success.
	// Example: "entity not found: acme.foo.bar"
	// DataType: string
	StepErrorMessage = "agent.step.error_message"

	// StepErrorCategory is the typed error category for a failed tool_call step.
	// Values: "timeout", "not_found", "invalid_args", "permission", "network",
	// "external", "internal", "unknown". Derived from ToolResult.ErrorKind.
	// Example: "invalid_args"
	// DataType: string
	StepErrorCategory = "agent.step.error_category"
)

// Coordinator Predicates
//
// Emitted by the coordinator's decide terminal tool (see
// processor/agentic-tools/decide.go) onto the coordinator's own loop entity
// so downstream rules can branch deterministically on the coordinator's
// decision without having to parse the loop's result JSON.
//
// The coordinator role is the judgment layer of the three-layer
// orchestration architecture (ADR-028). Its decide() call is structured;
// the decision's lower-bandwidth payload (the action + a short reason)
// lands in triples here; any larger supporting data (subtopics list,
// retry_hint prose) stays in LoopCompletedEvent.Result and is fetched
// on demand via read_loop_result.
const (
	// CoordinatorNextAction is the action the coordinator decided on,
	// constrained by whatever enumeration the specific flow's coordinator
	// persona documents. Stock research-coordinator values:
	// "fan_out", "synthesize", "retry", "done".
	// Example: "fan_out"
	// DataType: string
	CoordinatorNextAction = "coordinator.next_action"

	// CoordinatorDecisionReason is a short natural-language justification
	// the coordinator supplied alongside its action choice. Small enough
	// to inline as a triple — rule-author-friendly for debugging but not
	// meant to carry full reasoning traces.
	// Example: "researcher produced three distinct subtopics worth
	// separate investigation"
	// DataType: string
	CoordinatorDecisionReason = "coordinator.decision_reason"

	// CoordinatorDecideSAPCoerced is an audit triple emitted by the
	// decide tool's SAP (schema-aligned-parsing) layer when it normalises
	// an LLM-emitted action_allowlist drift to the allowlist's canonical
	// form. Object is "{raw}|{canonical}" so a single predicate captures
	// the drift shape — ops-agent / dashboards can group by Object to
	// find recurring patterns (e.g. "fan-out|fan_out" appearing
	// repeatedly indicates a model that consistently hyphenates).
	//
	// High prevalence of this triple for a given role is a model/persona
	// fit problem, not a feature. The 2026-05-05 design constraint:
	// SAP exists because clean structured-output runs are unicorns, but
	// every coercion is impossible to miss in the graph so operators
	// can act on the signal rather than raise MaxIterations.
	// Example: "fan-out|fan_out"
	// DataType: string
	CoordinatorDecideSAPCoerced = "coordinator.decide_sap_coerced"
)

// Ops Predicates
//
// Emitted by the ops agent's emit_diagnosis terminal tool (see
// processor/agentic-tools/emit_diagnosis.go) onto a freshly-minted
// ops.diagnosis entity per finding. Each emit_diagnosis call mints a
// new {org}.{platform}.ops.diagnosis.{id} entity and attaches one triple
// per predicate so downstream rules can branch deterministically on
// severity, role, and confidence without parsing prose.
//
// The ops role is the learning layer of the three-layer orchestration
// architecture (ADR-027 Phase 1 / ADR-028). It observes loop completions,
// queries the graph, and emits structured findings. Phase 2 tool grants +
// Phase 3 config tuning land after beta.
const (
	// OpsDiagnosisFinding is a short textual description of the finding the
	// ops agent identified. Free-text prose; treat as untrusted user content
	// downstream (agentic-governance filters inbound injection).
	// Example: "researcher loop exceeded token budget on first attempt"
	// DataType: string
	OpsDiagnosisFinding = "ops.diagnosis.finding"

	// OpsDiagnosisRecommendation is the proposed next step the ops agent
	// believes would address the finding. Free-text prose.
	// Example: "reduce max_tokens for researcher endpoint to 4096"
	// DataType: string
	OpsDiagnosisRecommendation = "ops.diagnosis.recommendation"

	// OpsDiagnosisConfidence is the ops agent's self-reported confidence in
	// the finding, on a 0.0–1.0 scale. Also used as the triple's graph
	// Confidence field so confidence-weighted queries reflect agent certainty.
	// Example: "0.85"
	// DataType: float64 (serialised as %g string in the triple Object)
	OpsDiagnosisConfidence = "ops.diagnosis.confidence"

	// OpsDiagnosisEvidence is a citation of an entity ID that supports the
	// finding. Multi-valued: one triple per evidence entity. Downstream
	// queries can follow evidence links to the loop or trajectory entities
	// the ops agent examined.
	// Example: "acme.ops.agent.agentic-loop.execution.abc123"
	// DataType: string (entity ID)
	OpsDiagnosisEvidence = "ops.diagnosis.evidence"

	// OpsDiagnosisObservedRole is the agent role the finding pertains to.
	// Optional — omitted when the finding is not role-specific.
	// Example: "researcher"
	// DataType: string
	OpsDiagnosisObservedRole = "ops.diagnosis.observed_role"

	// OpsDiagnosisSeverity is the urgency classification of the finding.
	// Values: "info" | "warn" | "critical". Defaults to "info" when the
	// ops agent omits the field or supplies an unrecognised value.
	// Example: "warn"
	// DataType: string
	OpsDiagnosisSeverity = "ops.diagnosis.severity"
)

// Ops Config Predicates (Phase 3, reserved)
//
// These predicates are declared now to lock the ops.config.* namespace but
// are intentionally unused until Phase 3 (continuous tuning, Pareto
// frontier, rollback lineage). Do not emit or consume them in Phase 1 or 2.
// See ADR-027 for the Phase 3 scope definition.
const (
	// OpsConfigAccuracy is the measured accuracy score for a configuration
	// variant. Reserved for Phase 3 Pareto-frontier tracking.
	// DataType: float64
	OpsConfigAccuracy = "ops.config.accuracy"

	// OpsConfigCostPerTask is the average cost per task for a configuration
	// variant. Reserved for Phase 3 Pareto-frontier tracking.
	// DataType: float64
	OpsConfigCostPerTask = "ops.config.cost_per_task"

	// OpsConfigP95Latency is the p95 latency in milliseconds for a
	// configuration variant. Reserved for Phase 3 Pareto-frontier tracking.
	// DataType: float64
	OpsConfigP95Latency = "ops.config.p95_latency"

	// OpsConfigActive indicates whether a configuration variant is currently
	// active. Reserved for Phase 3 live-rollback tracking.
	// DataType: bool (serialised as "true"/"false")
	OpsConfigActive = "ops.config.active"

	// OpsConfigParent links a configuration variant to its parent variant for
	// rollback lineage. Reserved for Phase 3 rollback tracking.
	// DataType: string (entity ID of parent config entity)
	OpsConfigParent = "ops.config.parent"
)

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
	DelegationValidFrom = "agent.delegation.valid-from"

	// DelegationValidUntil is when the delegation expires.
	// Example: "2024-12-31T23:59:59Z"
	// DataType: time.Time
	// IRI: agent-ontology:validUntil
	DelegationValidUntil = "agent.delegation.valid-until"

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
	ExecutionRateLimit = "agent.execution.rate-limit"

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
	ActionExecutedBy = "agent.action.executed-by"

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
	IdentityDisplayName = "agent.identity.display-name"

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
	ModelMaxTokens = "agent.model.max-tokens"

	// ModelSupportsTools indicates whether the endpoint supports tool calling.
	// Example: true
	// DataType: bool
	ModelSupportsTools = "agent.model.supports-tools"

	// ModelInputPrice is the cost per 1M input tokens in USD.
	// Example: 3.00
	// DataType: float64
	ModelInputPrice = "agent.model.input-price"

	// ModelOutputPrice is the cost per 1M output tokens in USD.
	// Example: 15.00
	// DataType: float64
	ModelOutputPrice = "agent.model.output-price"

	// ModelEndpointURL is the API endpoint URL for the model.
	// Example: "https://api.anthropic.com/v1"
	// DataType: string
	ModelEndpointURL = "agent.model.endpoint-url"

	// ModelRateLimit is the requests per minute limit for the endpoint.
	// Example: 60
	// DataType: int
	ModelRateLimit = "agent.model.rate-limit"
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

	// LoopTerminalReason is the classified reason a loop reached its
	// terminal failure outcome — the graph-side companion to the
	// LoopFailedEvent.Reason classification (gh#569): rules can route on
	// WHY a loop failed (escalate on budget exhaustion vs retry on a
	// transient model error). Stamped only on failure; absent on success.
	// Example: "max_iterations", "model_error", "handler_error",
	// "graph_state_reset_required"
	// DataType: string
	LoopTerminalReason = "agent.loop.terminal-reason"

	// LoopEvidenceIntegrity classifies the loop's OWN trajectory audit
	// trail: the component tried to record required evidence for this loop
	// and watched at least one attempt fail. It is a reportable condition,
	// not a reconstruction — it names nothing about what was lost, elects
	// no stage or reason (those stay in the ERROR log and the bounded
	// {stage,kind,reason} counter), and asserts no repair.
	//
	// Stamped only on observed incompleteness; absent otherwise. Absence
	// means ONLY that no audit loss was observed — never that the evidence
	// IS complete. The framework can observe failures it saw and nothing
	// more, so it never writes a completeness claim (ADR-084: absence
	// licenses nothing).
	//
	// Closed value set: "incomplete". No other value is ever written.
	// Example: "incomplete"
	// DataType: string
	LoopEvidenceIntegrity = "agent.loop.evidence-integrity"

	// LoopRole is the role used during this loop execution.
	// Example: "architect", "editor", "reviewer"
	// DataType: string
	LoopRole = "agent.loop.role"

	// LoopModelUsed is an entity reference to the model endpoint entity used.
	// Example: entity ID of the model endpoint
	// DataType: string (entity ID)
	LoopModelUsed = "agent.loop.model-used"

	// LoopIterations is the number of LLM iterations executed in this loop.
	// Example: 12
	// DataType: int
	LoopIterations = "agent.loop.iterations"

	// LoopTokensIn is the total input tokens consumed across all iterations.
	// Example: 48320
	// DataType: int
	LoopTokensIn = "agent.loop.tokens-in"

	// LoopTokensOut is the total output tokens consumed across all iterations.
	// Example: 8192
	// DataType: int
	LoopTokensOut = "agent.loop.tokens-out"

	// LoopCostUSD is the computed cost in USD for this loop execution.
	// Example: 0.2754
	// DataType: float64
	LoopCostUSD = "agent.loop.cost-usd"

	// LoopTask is the task ID this loop execution served.
	// Example: "task-abc123"
	// DataType: string
	LoopTask = "agent.loop.task"

	// LoopParent is an entity reference to the parent loop entity.
	// Example: entity ID of the parent loop
	// DataType: string (entity ID)
	LoopParent = "agent.loop.parent"

	// LoopReplyTo is an entity reference to the loop this loop is a reply to
	// (gh#256). Stamped at spawn time by LoopExecutionEntity.Triples() when
	// TaskMessage.InReplyTo is non-empty. Distinct from LoopParent (tree
	// ancestry): a reply re-enters a paused run (ADR-053 §4b-2 interactive
	// clarification) rather than nesting under a parent. A resume rule fires on
	// $entity.triple.agent.loop.reply_to to distinguish a reply from any other
	// run loop, then drives the run entity back from awaiting_approval to
	// executing. Grammar-collision-free: agent.loop.* is already a substitution
	// namespace; reply_to adds no new $-prefix token.
	// Example: "org.platform.agent.agentic-loop.execution.<askingLoopID>"
	// DataType: string (6-part entity ID)
	LoopReplyTo = "agent.loop.reply-to"

	// LoopRun is the bare run loop-id this loop belongs to (ADR-053 D7).
	// Stamped at spawn time by LoopExecutionEntity.Triples() when TaskMessage.RunID is non-empty.
	// Rules can read this via $entity.triple.agent.loop.run. Grammar-collision-free:
	// no existing $-regex matches agent.loop.run (audited at ADR-053 implementation).
	// Example: "loop-uuid-of-the-root-coordinator"
	// DataType: string (bare loop UUID, NOT a 6-part entity ID)
	LoopRun = "agent.loop.run"

	// LoopRunEntityID is the FULL 6-part chain.execution entity ID of the run
	// this loop belongs to (ADR-053). Stamped at spawn alongside LoopRun.
	// This is the rule-addressable upsert SUBJECT for run-scoped state: a rule
	// firing on a loop reads $entity.triple.agent.run.entity-id and uses it as
	// the Subject of add_triple/update_triple (the typed replacement for the
	// retired untyped related-loop run-anchor pattern). Rules cannot
	// derive the 6-part from the bare LoopRun (substitution is string interp,
	// not function calls), so the framework stamps the full form directly.
	// For computed run-state, prefer a Go agentrun.MilestoneHandler instead.
	// Example: "org.platform.agent.chain.execution.<runID>"
	// DataType: string (6-part federated entity ID)
	LoopRunEntityID = "agent.run.entity-id"

	// LoopWorkflow is the workflow slug this loop belongs to.
	// Example: "code-review", "feature-implementation"
	// DataType: string
	LoopWorkflow = "agent.loop.workflow"

	// LoopWorkflowStep is the step within the workflow for this loop.
	// Example: "draft", "review", "revise"
	// DataType: string
	LoopWorkflowStep = "agent.loop.workflow-step"

	// LoopEndedAt is the terminal timestamp for this loop (completion, failure, or cancellation).
	// Example: "2026-03-13T14:22:00Z"
	// DataType: time.Time
	LoopEndedAt = "agent.loop.ended-at"

	// LoopUser is the user ID who initiated this loop.
	// Example: "user-xyz789"
	// DataType: string
	LoopUser = "agent.loop.user"

	// LoopHasStep is an entity reference to a trajectory step within this loop.
	// Multi-valued: one triple per step.
	// Example: entity ID of a trajectory step
	// DataType: string (entity ID)
	LoopHasStep = "agent.loop.has-step"

	// LoopDescription is the user task prompt that initiated this loop, stored
	// as text so BM25/NL search can find loops by topic. The `.description`
	// suffix is already in the embedding pipeline's default text suffixes,
	// so this triple is auto-indexed.
	// Example: "Investigate MQTT retained-message behavior"
	// DataType: string
	LoopDescription = "agent.loop.description"

	// LoopObservedWeb is an entity reference to a web observation entity
	// (agent.web.observation) the loop saw in a web_search result.
	// Multi-valued: one triple per result. Paired with the URL entity's
	// agent.web.observed_by back-link. See the Web Predicates block.
	// Example: entity ID of a web.observation entity
	// DataType: string (entity ID)
	LoopObservedWeb = "agent.loop.observed-web"

	// LoopFetchedWeb is an entity reference to a web observation entity
	// (agent.web.observation) the loop pulled via http_request. Multi-valued:
	// one triple per successful 2xx/3xx fetch. Paired with the URL entity's
	// agent.web.fetched_by back-link. See the Web Predicates block.
	// Example: entity ID of a web.observation entity
	// DataType: string (entity ID)
	LoopFetchedWeb = "agent.loop.fetched-web"
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
	StepDuration = "agent.step.duration-ms"

	// StepToolName is the tool function name for tool_call steps.
	// Example: "web_search", "graph_query", "http_request"
	// DataType: string
	StepToolName = "agent.step.tool-name"

	// StepModel is the model name for model_call steps.
	// Example: "claude-sonnet", "gpt-4o"
	// DataType: string
	StepModel = "agent.step.model"

	// StepTokensIn is the input tokens consumed by a model_call step.
	// Example: 4832
	// DataType: int
	StepTokensIn = "agent.step.tokens-in"

	// StepTokensOut is the output tokens produced by a model_call step.
	// Example: 819
	// DataType: int
	StepTokensOut = "agent.step.tokens-out"

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
	StepTokensEvicted = "agent.step.tokens-evicted"

	// StepTokensSummarized is the number of tokens in the compaction summary.
	// Only set on context_compaction steps.
	// Example: 800
	// DataType: int
	StepTokensSummarized = "agent.step.tokens-summarized"

	// StepUtilization is the context utilization ratio (0.0-1.0) at compaction trigger.
	// Only set on context_compaction steps.
	// Example: 0.72
	// DataType: float64
	StepUtilization = "agent.step.utilization"

	// StepToolStatus is the terminal status of a tool_call step.
	// Example: "success", "failed"
	// DataType: string
	StepToolStatus = "agent.step.tool-status"

	// StepErrorMessage is the raw error text for a failed tool_call step.
	// Omitted on success.
	// Example: "entity not found: acme.foo.bar"
	// DataType: string
	StepErrorMessage = "agent.step.error-message"

	// StepErrorCategory is the typed error category for a failed tool_call step.
	// Values: "timeout", "not_found", "invalid_args", "permission", "network",
	// "external", "internal", "unknown". Derived from ToolResult.ErrorKind.
	// Example: "invalid_args"
	// DataType: string
	StepErrorCategory = "agent.step.error-category"
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
	CoordinatorNextAction = "coordinator.decision.next-action"

	// CoordinatorDecisionReason is a short natural-language justification
	// the coordinator supplied alongside its action choice. Small enough
	// to inline as a triple — rule-author-friendly for debugging but not
	// meant to carry full reasoning traces.
	// Example: "researcher produced three distinct subtopics worth
	// separate investigation"
	// DataType: string
	CoordinatorDecisionReason = "coordinator.decision.reason"

	// CoordinatorDecisionSAPCoerced is an audit triple emitted by the
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
	CoordinatorDecisionSAPCoerced = "coordinator.decision.sap-coerced"

	// CoordinatorDecisionSubtopics carries the list of subtopics from a
	// coordinator's fan-out decision as a JSON-encoded []string. ADR-046
	// Phase 1 (#134). Emitted by the decide tool when args.Subtopics is
	// non-empty so a downstream rule can iterate the list via
	// `for_each: "$entity.triple.coordinator.decision.subtopics"`
	// without the rule author having to read the loop's Result JSON
	// out-of-band. The Object stays string-typed on the wire
	// (JSON-encoded) so it round-trips through graph-ingest's per-triple
	// validators which assume scalar Object types; the for_each
	// substitution layer parses the JSON back into []string at
	// resolution time.
	// Example: `["hydraulics", "pneumatics", "electrics"]`
	// DataType: string (JSON-encoded []string)
	CoordinatorDecisionSubtopics = "coordinator.decision.subtopics"

	// CoordinatorDecisionSynthetic is set to "true" when the framework
	// synthesizes a decide on terminal-tool-less completion (#133). The
	// model finished its loop with a text-only response — no `decide`
	// tool_call appeared in the trajectory — so agentic-loop emits the
	// canonical next_action + reason triples (needs_clarification +
	// "[synthetic-no-terminal] {model text}") plus this marker so
	// downstream rule authors can distinguish a model-emitted
	// needs_clarification from a framework-synthesized one. Opt-in via
	// Config.SynthesizeTerminalOnCompletion.
	// Example: "true"
	// DataType: string
	CoordinatorDecisionSynthetic = "coordinator.decision.synthetic"
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
	OpsDiagnosisObservedRole = "ops.diagnosis.observed-role"

	// OpsDiagnosisSeverity is the urgency classification of the finding.
	// Values: "info" | "warn" | "critical". Defaults to "info" when the
	// ops agent omits the field or supplies an unrecognised value.
	// Example: "warn"
	// DataType: string
	OpsDiagnosisSeverity = "ops.diagnosis.severity"
)

// Lesson Predicates (ADR-080 — Push-Based Agent Memory / Lesson Substrate)
//
// Emitted by the ops agent's emit_lesson terminal tool (see
// processor/agentic-tools/emit_lesson.go) onto a freshly-minted lesson record
// entity per distilled lesson. Each emit_lesson call mints a content-derived
// {org}.{platform}.agent.lesson.record.{uuid5} entity and attaches one triple
// per predicate so downstream rules and curation lanes can branch
// deterministically on polarity, severity, status, and scope without parsing
// the LLM-authored prose. Lessons are born status="proposed"; only promotion to
// "active" makes a lesson injectable at brief assembly (ADR-080 gated
// lifecycle).
//
// Rule-visibility split follows the house LLM-authored-content discipline
// (ADR-036): the enumerated/structural and reference predicates below stay
// rule-matchable so rules and curation lanes can gate on them; the three
// authored-text predicates (summary, detail, injection-form) register
// rule-opaque so rules cannot predicate on model-sampled prose (Goodhart
// feedback loop).
//
// Closed enums (polarity/severity/status) are documented here and enforced at
// the emit_lesson writer / rule layer — the vocabulary registry stores no
// allowed-value set, so "closed" means documented-and-writer-enforced.
// category is intentionally OPEN: taxonomies are product vocabulary, not
// framework enums (ADR-080).
//
// Note on the predicate identity: the family roots at the "agent" domain
// (ADR-080 mandates the agent.lesson.* namespace — lesson artifacts unify with
// the rest of agent memory under agent.*, while diagnosis stays ops.*). This
// also matches the sibling agent-artifact predicates in this file
// (agent.todo.*, agent.scratch.*) and aligns the predicate domain with the
// entity's domain+system (agent.lesson) exactly as ops.diagnosis.* aligns with
// its entity. The canonical predicate contract requires exactly three
// lower-kebab segments (domain.category.property), so the flat conceptual
// shorthand in design.md (lesson.category, lesson.injection_form) is realized
// as agent.lesson.category, agent.lesson.injection-form, etc. — multi-word
// properties use hyphens exactly as agent.scratch.created-at / ops.diagnosis.
// observed-role do. Two-segment or underscored strings fail ParsePredicate and
// panic at registration.
const (
	// LessonCategory is an OPEN product-taxonomy classifier for the lesson.
	// No framework-closed value set — products partition their own taxonomy.
	// Rule-matchable structural fact.
	// Example: "retention-policy"
	// DataType: string
	LessonCategory = "agent.lesson.category"

	// LessonPolarity is the lesson's directional stance. Closed value set:
	// "avoid" | "best_practice". Rule-matchable.
	// Example: "avoid"
	// DataType: string
	LessonPolarity = "agent.lesson.polarity"

	// LessonSeverity is the urgency classification used to order lessons at
	// brief assembly. Closed value set: "info" | "warning" | "critical"
	// (note: "warning", not the ops.diagnosis family's "warn"). Rule-matchable.
	// Example: "warning"
	// DataType: string
	LessonSeverity = "agent.lesson.severity"

	// LessonStatus is the gated-lifecycle state. Closed value set:
	// "proposed" | "active" | "retired" | "superseded". Born "proposed"; only
	// "active" lessons are injectable. Single-valued — lifecycle transitions
	// replace, never append (RemoveTriples + AddTriples). Rule-matchable.
	// Example: "proposed"
	// DataType: string
	LessonStatus = "agent.lesson.status"

	// LessonSummary is a short LLM-authored gist of the lesson. Rule-opaque
	// authored prose — treat as untrusted content downstream.
	// Example: "cap retention sweeps to entity-owned buckets"
	// DataType: string
	LessonSummary = "agent.lesson.summary"

	// LessonDetail is the unbounded LLM-authored explanation of the lesson.
	// Rule-opaque authored prose.
	// Example: "When sweeping AGENT_LOOPS, scope deletes to COMPLETE_* keys so ..."
	// DataType: string
	LessonDetail = "agent.lesson.detail"

	// LessonInjectionForm is the bounded string rendered verbatim into a
	// downstream agent's brief when the lesson is active and in scope. Byte-
	// bounded at the emit_lesson writer (rejected, never truncated). Rule-opaque
	// authored prose.
	// Example: "Avoid unscoped retention sweeps; scope deletes to COMPLETE_* keys."
	// DataType: string
	LessonInjectionForm = "agent.lesson.injection-form"

	// LessonEvidence cites a supporting entity ID the lesson was derived from
	// (>=1 enforced at the emit_lesson writer). Multi-valued: one triple per
	// cited entity. Registered with StandardIRI prov:wasDerivedFrom so the
	// citation exports as a PROV-O derivation edge (annotation only).
	// Example: "acme.ops.agent.agentic-loop.execution.abc123"
	// DataType: string (entity ID)
	LessonEvidence = "agent.lesson.evidence"

	// LessonAppliesTo is a deterministic scope key controlling which loops the
	// lesson is briefed into. Grammar: "id:{entity-id-prefix}" | "tag:{token}"
	// (e.g. "id:acme.ops.agent", "tag:researcher"). Multi-valued; rule-matchable.
	// Example: "tag:researcher"
	// DataType: string (scope key)
	LessonAppliesTo = "agent.lesson.applies-to"

	// LessonObservedRole is the agent role the lesson pertains to. Optional —
	// omitted when the lesson is not role-specific. Rule-matchable.
	// Example: "researcher"
	// DataType: string
	LessonObservedRole = "agent.lesson.observed-role"

	// LessonRetiredAt is the lifecycle timestamp set when a lesson transitions
	// out of "active". Single-valued — replace, never append.
	// Example: "2026-07-19T12:00:00Z"
	// DataType: time.Time
	LessonRetiredAt = "agent.lesson.retired-at"

	// LessonSupersededBy is the entity ID of the lesson that replaces this one
	// on supersession. Single-valued — replace, never append.
	// Example: "acme.ops.agent.lesson.record.def456"
	// DataType: string (entity ID)
	LessonSupersededBy = "agent.lesson.superseded-by"

	// LessonCreatedAt is the IMMUTABLE wall-clock birth timestamp of the lesson
	// entity — stamped once by emit_lesson at create time and NEVER re-written
	// by a lifecycle transition. RFC3339 UTC. Rule-matchable, and crucially the
	// replay-stable ordering key for brief-assembly injection: the matcher
	// orders active lessons by severity → created-at → entity-ID. A triple's
	// Timestamp field is re-stamped on every reconcile promotion/retirement
	// (single-valued lifecycle writes carry a fresh Timestamp), and the entity's
	// UpdatedAt / KV revision advance on every write — so neither is a stable
	// sort key. Only this birth triple is invariant across a curator's edits AND
	// an ADR-073 from-zero reingest, so the injected order is identical on
	// replay. NOT part of content-identity (idempotency unchanged; first-write-
	// wins preserves the FIRST emit's created-at) and NOT in any reconcile
	// lifecycle set. Mirrors agent.scratch.created-at.
	// Example: "2026-07-19T12:00:00Z"
	// DataType: time.Time
	LessonCreatedAt = "agent.lesson.created-at"
)

// Todo record predicate (ADR-036 — Agent-Private Observable State).
//
// Each write_todos call reconciles one record triple per item on the owning
// loop entity. The object is a deterministic JSON string with id, content,
// status, position, and updated_at fields. The complete record is rule-opaque:
// TodoState and TodoReader are the supported logical access surface, so rules
// never infer correlations from raw storage fields.
const (
	// TodoRecord stores one complete todo item as deterministic JSON.
	// DataType: string carrying TodoRecordJSONDatatype.
	TodoRecord = "agent.todo.record"

	// TodoRecordJSONDatatype uses the repository's compact RDF datatype
	// convention (for example xsd:string) to identify a JSON literal without
	// changing Triple.Object from its persisted string representation.
	TodoRecordJSONDatatype = "rdf:JSON"
)

// Scratch Predicates (ADR-036 §Future candidates — agent.scratch.*)
//
// Emitted by the scratchpad tool on every call. Append-only on the
// owning loop entity (NOT full-list-replace like write_todos): each
// scratchpad call mints a stable scratch.id and lands four triples
// correlated by the shared Context stamp carried on all four (the id
// is also the Context value). Context, not the scratch.id Object, is
// what groups them — it is one of the six append tuple identity fields, so
// it also keeps two calls with the same text or character count from
// collapsing into one entry. The agent is the sole writer and sole interpreter of
// content; rule-opaque flagging on agent.scratch.text mirrors the
// ADR-036 discipline applied to TodoRecord.
//
// The semspec ask (2026-05-12, scratchpad proposal) framed this as
// pre-commit reasoning runway for mid-tier models that struggle under
// simultaneous strict response_format + strict tool-args. Persona
// pattern: instruct "scratchpad first, then your commit tool" plus
// caller-side tool_choice=required on the first dispatch turn.
//
// Per-call audit / recovery: scratch entries are queryable via
// query_relationships on the loop entity for any agent (recovery,
// debug, ops diagnosis) reconstructing what the model drafted before
// committing.
const (
	// ScratchID is the stable per-call identifier (UUID). The four
	// triples of one call correlate by the shared Context stamp, whose
	// value is this id — correlation is carried by Context, not by this
	// predicate's Object. Rule-matchable.
	// Example: "550e8400-e29b-41d4-a716-446655440000"
	// DataType: string
	ScratchID = "agent.scratch.id"

	// ScratchText is the free-form prose the agent emitted. Owner-
	// interpretable only. Rule-opaque — the rule-validator rejects any
	// rule whose condition.field names this predicate (LLM-authored
	// content; rules predicating on it would create Goodhart feedback
	// loops where agents optimise drafts toward rule triggers).
	// Example: "I need to handle the case where retry hint is empty…"
	// DataType: string
	ScratchText = "agent.scratch.text"

	// ScratchCreatedAt is the wall-clock timestamp of the scratchpad
	// call. Rule-matchable for ordering (no explicit position predicate
	// needed; sort by created_at) and for age-based rules.
	// Example: "2026-05-12T09:15:00Z"
	// DataType: time.Time
	ScratchCreatedAt = "agent.scratch.created-at"

	// ScratchChars is the character count of ScratchText. Rule-matchable
	// structural fact so operators / ops-agent can predicate on size
	// (e.g. "agent called scratchpad with > 8000 chars" — signal of
	// a model that's using the channel as a context dump rather than
	// pre-commit reasoning). Lets dashboards surface size patterns
	// without needing to read the rule-opaque body.
	// Example: 245
	// DataType: int
	ScratchChars = "agent.scratch.chars"
)

// Web Predicates
//
// Emitted by the web_search and http_request tools when an operator has
// wired an optional TriplePublisher into their registration. The URL-side
// predicates land on a per-URL agent.web.observation entity whose instance
// segment is sha256-hex(canonical URL) so the same URL converges across
// loops (natural dedup). The loop-side back-link predicates
// (agent.loop.observed_web and agent.loop.fetched_web) live in the Loop
// Predicates block above.
//
// Discipline: title, snippet, text, and source_query are flagged rule-opaque
// per the LLM-authored-content principle. The first three are external prose
// the tool's primary contract returns; the LLM-authored source_query is opaque
// so rules don't predicate on its content (which would teach agents to optimise
// queries toward rule triggers — Goodhart). Rules match on the structural
// fields (url, content_type, status_code, observed_at, fetched_at, truncated)
// or follow the entity-ID back-links.
const (
	// WebURL is the canonical URL the observation entity represents.
	// The canonicalisation rules are documented in
	// agentic/entity_ids.go:TryWebObservationEntityID (lowercase scheme+host,
	// strip default port, strip fragment, strip trailing slash on bare host,
	// preserve query string). Rule-matchable for allow/deny prefix matching
	// and host-routing rules.
	// Example: "https://pkg.go.dev/net/http"
	// DataType: string
	WebURL = "agent.web.url"

	// WebTitle is the search-result title returned by the search provider.
	// Free-form external prose. Rule-opaque — the rule-validator rejects
	// any rule whose condition.field names this predicate.
	// Example: "net/http package - Go Documentation"
	// DataType: string
	WebTitle = "agent.web.title"

	// WebSnippet is the search-result description returned by the search
	// provider. Free-form external prose. Rule-opaque.
	// Example: "Package http provides HTTP client and server implementations…"
	// DataType: string
	WebSnippet = "agent.web.snippet"

	// WebText is the fetched body of an http_request, after HTML→text
	// extraction, truncated at the executor's httpMaxTextSize cap. Free-form
	// external prose. Rule-opaque. WebTruncated reflects whether the cap was
	// hit so rules can branch on completeness without reading the body.
	// Example: "Package http provides HTTP client and server …"
	// DataType: string
	WebText = "agent.web.text"

	// WebSourceQuery is the search query string passed to the web_search
	// tool. LLM-authored content; rule-opaque so rules don't condition on
	// query text (which would create a feedback loop where agents optimise
	// queries toward rule triggers). Rules wanting to react to specific
	// search activity should match on the observation entity's WebURL or
	// the loop's role/workflow instead.
	// Example: "NATS JetStream KV bucket history retention"
	// DataType: string
	WebSourceQuery = "agent.web.source-query"

	// WebObservedAt is the wall-clock timestamp the web_search call saw
	// this URL. Rule-matchable for age-based predicates.
	// Example: "2026-05-11T14:22:00Z"
	// DataType: time.Time
	WebObservedAt = "agent.web.observed-at"

	// WebFetchedAt is the wall-clock timestamp the http_request call pulled
	// this URL's body. Rule-matchable.
	// Example: "2026-05-11T14:22:30Z"
	// DataType: time.Time
	WebFetchedAt = "agent.web.fetched-at"

	// WebObservedBy is the loop entity ID that observed this URL via
	// web_search. Multi-valued across loops (different loops can converge
	// on the same observation entity). Rule-matchable for relationship
	// walks.
	// Example: entity ID of the observing loop
	// DataType: string (entity ID)
	WebObservedBy = "agent.web.observed-by"

	// WebFetchedBy is the loop entity ID that pulled this URL via
	// http_request. Multi-valued across loops. Rule-matchable.
	// Example: entity ID of the fetching loop
	// DataType: string (entity ID)
	WebFetchedBy = "agent.web.fetched-by"

	// WebContentType is the HTTP Content-Type header value from an
	// http_request fetch. Effectively an enum for rule purposes (route
	// HTML vs JSON vs binary), so rule-matchable.
	// Example: "text/html; charset=utf-8"
	// DataType: string
	WebContentType = "agent.web.content-type"

	// WebStatusCode is the HTTP response status code from an http_request
	// fetch. Only emitted for 2xx/3xx (the tool surfaces ≥400 as an error
	// and does not write to the graph). Rule-matchable for retry-on-redirect
	// style rules.
	// Example: 200
	// DataType: int
	WebStatusCode = "agent.web.status-code"

	// WebTruncated indicates whether the http_request body exceeded the
	// executor's httpMaxTextSize cap and was truncated when written to
	// WebText. Rule-matchable so a future "re-fetch when truncated" rule
	// can act on completeness without reading the body. WebText itself
	// remains rule-opaque.
	// Example: false
	// DataType: bool
	WebTruncated = "agent.web.truncated"
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
	OpsConfigCostPerTask = "ops.config.cost-per-task"

	// OpsConfigP95Latency is the p95 latency in milliseconds for a
	// configuration variant. Reserved for Phase 3 Pareto-frontier tracking.
	// DataType: float64
	OpsConfigP95Latency = "ops.config.p95-latency"

	// OpsConfigActive indicates whether a configuration variant is currently
	// active. Reserved for Phase 3 live-rollback tracking.
	// DataType: bool (serialised as "true"/"false")
	OpsConfigActive = "ops.config.active"

	// OpsConfigParent links a configuration variant to its parent variant for
	// rollback lineage. Reserved for Phase 3 rollback tracking.
	// DataType: string (entity ID of parent config entity)
	OpsConfigParent = "ops.config.parent"
)

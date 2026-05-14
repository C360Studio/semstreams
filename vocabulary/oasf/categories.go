package oasf

// Top-level OASF skill categories. Each constant maps to a single-digit
// uid declared by the upstream schema at
// github.com/agntcy/oasf/schema/skills/<name>/<name>.json. Coverage is
// the 10 categories most relevant to SemStreams' framework role; other
// published categories (computer vision, audio, tabular/text, multi-
// modal, devops/MLOps) are reachable via extension IDs until a concrete
// internal capability surfaces a need to promote them.
//
// Constants are typed uint32 so they assign directly to the wire field
// without conversion.
const (
	// CategoryNLP is the natural language processing category — the
	// universal home for skills that read or write natural language.
	// Upstream: schema/skills/natural_language_processing.
	CategoryNLP uint32 = 1

	// CategoryAnalyticalSkills covers analysis, reasoning, and inference
	// across structured and unstructured inputs.
	// Upstream: schema/skills/analytical_skills.
	CategoryAnalyticalSkills uint32 = 5

	// CategoryRAG covers retrieval-augmented generation patterns —
	// retrieval pipelines, context assembly, and answer synthesis.
	// Upstream: schema/skills/retrieval_augmented_generation.
	CategoryRAG uint32 = 6

	// CategorySecurityPrivacy covers security analysis, privacy review,
	// and threat assessment skills.
	// Upstream: schema/skills/security_privacy.
	CategorySecurityPrivacy uint32 = 8

	// CategoryDataEngineering covers data ingestion, transformation,
	// validation, and pipeline construction skills.
	// Upstream: schema/skills/data_engineering.
	CategoryDataEngineering uint32 = 9

	// CategoryAgentOrchestration covers multi-agent coordination,
	// delegation, and workflow composition skills.
	// Upstream: schema/skills/agent_orchestration.
	CategoryAgentOrchestration uint32 = 10

	// CategoryEvaluationMonitoring covers quality evaluation,
	// benchmarking, performance monitoring, and anomaly detection.
	// Upstream: schema/skills/evaluation_monitoring.
	CategoryEvaluationMonitoring uint32 = 11

	// CategoryGovernanceCompliance covers policy enforcement,
	// compliance assessment, and audit-trail skills.
	// Upstream: schema/skills/governance_compliance.
	CategoryGovernanceCompliance uint32 = 13

	// CategoryToolInteraction covers external-tool invocation skills
	// (API calls, shell commands, structured tool-use patterns).
	// Upstream: schema/skills/tool_interaction.
	CategoryToolInteraction uint32 = 14

	// CategoryAdvancedReasoningPlanning covers multi-step reasoning,
	// task decomposition, and planning skills.
	// Upstream: schema/skills/advanced_reasoning_planning.
	CategoryAdvancedReasoningPlanning uint32 = 15
)

// categoryNames maps the category uid to its OASF hierarchical name
// (a single path segment for top-level categories). The hierarchical
// name is what the wire record carries in Skill.name; the lookup helpers
// in lookup.go are the public API — operate on these maps via Name and
// ID rather than reading them directly.
var categoryNames = map[uint32]string{
	CategoryNLP:                       "natural_language_processing",
	CategoryAnalyticalSkills:          "analytical_skills",
	CategoryRAG:                       "retrieval_augmented_generation",
	CategorySecurityPrivacy:           "security_privacy",
	CategoryDataEngineering:           "data_engineering",
	CategoryAgentOrchestration:        "agent_orchestration",
	CategoryEvaluationMonitoring:      "evaluation_monitoring",
	CategoryGovernanceCompliance:      "governance_compliance",
	CategoryToolInteraction:           "tool_interaction",
	CategoryAdvancedReasoningPlanning: "advanced_reasoning_planning",
}

// categoryCaptions maps the category uid to its OASF human-readable
// display caption (Skill.caption in the upstream schema). Useful for
// surfacing the standard's preferred display name in operator-facing
// contexts.
var categoryCaptions = map[uint32]string{
	CategoryNLP:                       "Natural Language Processing",
	CategoryAnalyticalSkills:          "Analytical Skills",
	CategoryRAG:                       "Retrieval Augmented Generation",
	CategorySecurityPrivacy:           "Security & Privacy",
	CategoryDataEngineering:           "Data Engineering",
	CategoryAgentOrchestration:        "Agent Orchestration",
	CategoryEvaluationMonitoring:      "Evaluation & Monitoring",
	CategoryGovernanceCompliance:      "Governance & Compliance",
	CategoryToolInteraction:           "Tool Interaction",
	CategoryAdvancedReasoningPlanning: "Advanced Reasoning & Planning",
}

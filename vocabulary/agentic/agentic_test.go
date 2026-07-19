package agentic_test

import (
	"strings"
	"testing"

	"github.com/c360studio/semstreams/vocabulary"
	"github.com/c360studio/semstreams/vocabulary/agentic"
)

func TestPredicateFormat(t *testing.T) {
	// Verify all predicates follow the three-level dotted notation
	predicates := []string{
		// Intent
		agentic.IntentGoal,
		agentic.IntentType,
		agentic.IntentParameter,
		agentic.IntentAuthorized,
		agentic.IntentProduces,
		// Capability
		agentic.CapabilityName,
		agentic.CapabilityDescription,
		agentic.CapabilityExpression,
		agentic.CapabilityConfidence,
		agentic.CapabilitySkill,
		agentic.CapabilityConstraint,
		agentic.CapabilityPermission,
		// Delegation
		agentic.DelegationFrom,
		agentic.DelegationTo,
		agentic.DelegationScope,
		agentic.DelegationCapability,
		agentic.DelegationValidFrom,
		agentic.DelegationValidUntil,
		agentic.DelegationChain,
		// Accountability
		agentic.AccountabilityActor,
		agentic.AccountabilityAction,
		agentic.AccountabilityAssigned,
		agentic.AccountabilityRationale,
		agentic.AccountabilityCompliance,
		agentic.AccountabilityTimestamp,
		// Execution
		agentic.ExecutionEnvironment,
		agentic.ExecutionSecurity,
		agentic.ExecutionConstraint,
		agentic.ExecutionRateLimit,
		agentic.ExecutionBudget,
		agentic.ExecutionInput,
		agentic.ExecutionOutput,
		// Action
		agentic.ActionType,
		agentic.ActionExecutedBy,
		agentic.ActionProduced,
		agentic.ActionContext,
		agentic.ActionTrace,
		// Task
		agentic.TaskAssigned,
		agentic.TaskCapability,
		agentic.TaskSubtask,
		agentic.TaskDependency,
		agentic.TaskStatus,
		// Model
		agentic.ModelProvider,
		agentic.ModelName,
		agentic.ModelMaxTokens,
		agentic.ModelSupportsTools,
		agentic.ModelInputPrice,
		agentic.ModelOutputPrice,
		agentic.ModelEndpointURL,
		agentic.ModelRateLimit,
		// Loop
		agentic.LoopOutcome,
		agentic.LoopTerminalReason,
		agentic.LoopRole,
		agentic.LoopModelUsed,
		agentic.LoopIterations,
		agentic.LoopTokensIn,
		agentic.LoopTokensOut,
		agentic.LoopCostUSD,
		agentic.LoopTask,
		agentic.LoopParent,
		agentic.LoopWorkflow,
		agentic.LoopWorkflowStep,
		agentic.LoopEndedAt,
		agentic.LoopUser,
		agentic.LoopObservedWeb,
		agentic.LoopFetchedWeb,
		// Scratch
		agentic.ScratchID,
		agentic.ScratchText,
		agentic.ScratchCreatedAt,
		agentic.ScratchChars,
		// Web
		agentic.WebURL,
		agentic.WebTitle,
		agentic.WebSnippet,
		agentic.WebText,
		agentic.WebSourceQuery,
		agentic.WebObservedAt,
		agentic.WebFetchedAt,
		agentic.WebObservedBy,
		agentic.WebFetchedBy,
		agentic.WebContentType,
		agentic.WebStatusCode,
		agentic.WebTruncated,
	}

	for _, p := range predicates {
		if !vocabulary.IsValidPredicate(p) {
			t.Errorf("predicate %q does not follow three-level dotted notation", p)
		}
	}
}

func TestPredicateDomainPrefix(t *testing.T) {
	// Verify all predicates use the "agent" domain
	predicates := map[string]string{ // predicate-audit:unrelated {"column":16,"surface":"go-assignment:predicates","value":"","basis":"reviewed:runtime-predicate-metadata-collection"}
		"IntentGoal":           agentic.IntentGoal,
		"CapabilityName":       agentic.CapabilityName,
		"DelegationFrom":       agentic.DelegationFrom,
		"AccountabilityActor":  agentic.AccountabilityActor,
		"ExecutionEnvironment": agentic.ExecutionEnvironment,
		"ActionType":           agentic.ActionType,
		"TaskStatus":           agentic.TaskStatus,
	}

	for name, p := range predicates {
		if !strings.HasPrefix(p, "agent.") {
			t.Errorf("%s: predicate %q does not start with 'agent.'", name, p)
		}
	}
}

func TestIRINamespaces(t *testing.T) {
	// Verify namespace constants are properly formatted
	namespaces := map[string]string{
		"Core":           agentic.CoreNamespace,
		"Intent":         agentic.IntentNamespace,
		"Capability":     agentic.CapabilityNamespace,
		"Delegation":     agentic.DelegationNamespace,
		"Accountability": agentic.AccountabilityNamespace,
		"Execution":      agentic.ExecutionNamespace,
	}

	for name, ns := range namespaces {
		if len(ns) < 10 {
			t.Errorf("%s namespace is too short: %q", name, ns)
		}
		if ns[len(ns)-1] != '#' {
			t.Errorf("%s namespace should end with '#': %q", name, ns)
		}
	}
}

func TestIRIConstants(t *testing.T) {
	// Verify IRI constants use correct namespaces
	tests := []struct {
		name      string
		iri       string
		namespace string
	}{
		{"IriAgent", agentic.IriAgent, agentic.CoreNamespace},
		{"IriIntent", agentic.IriIntent, agentic.CoreNamespace},
		{"IriCapability", agentic.IriCapability, agentic.CoreNamespace},
		{"IriHasIntentType", agentic.IriHasIntentType, agentic.IntentNamespace},
		{"IriHasSkill", agentic.IriHasSkill, agentic.CapabilityNamespace},
		{"IriDelegatedBy", agentic.IriDelegatedBy, agentic.DelegationNamespace},
		{"IriActor", agentic.IriActor, agentic.AccountabilityNamespace},
		{"IriExecutionEnvironment", agentic.IriExecutionEnvironment, agentic.ExecutionNamespace},
	}

	for _, tt := range tests {
		if len(tt.iri) <= len(tt.namespace) {
			t.Errorf("%s: IRI %q is not longer than namespace %q", tt.name, tt.iri, tt.namespace)
			continue
		}
		if tt.iri[:len(tt.namespace)] != tt.namespace {
			t.Errorf("%s: IRI %q does not start with namespace %q", tt.name, tt.iri, tt.namespace)
		}
	}
}

func TestRegistration(t *testing.T) {
	// Clear registry before test
	vocabulary.ClearRegistry()
	defer vocabulary.ClearRegistry()

	// Register all agentic predicates
	agentic.Register()

	// Verify key predicates are registered with correct metadata
	tests := []struct {
		predicate   string
		expectedIRI string
		hasInverse  bool
	}{
		{agentic.IntentGoal, agentic.IriIntent, false},
		{agentic.IntentType, agentic.IriHasIntentType, false},
		{agentic.CapabilityName, agentic.IriCapability, false},
		{agentic.CapabilityConfidence, agentic.IriCapabilityConfidence, false},
		{agentic.DelegationFrom, agentic.IriDelegatedBy, true},
		{agentic.DelegationTo, agentic.IriDelegatesTo, true},
		{agentic.AccountabilityActor, agentic.IriActor, false},
		// agent.lesson.evidence is annotated with the PROV-O derivation IRI
		// (ADR-080; annotation only, constant lives in vocabulary/standards.go).
		{agentic.LessonEvidence, vocabulary.ProvWasDerivedFrom, false},
	}

	for _, tt := range tests {
		meta := vocabulary.GetPredicateMetadata(tt.predicate)
		if meta == nil {
			t.Errorf("predicate %q not registered", tt.predicate)
			continue
		}

		if meta.StandardIRI != tt.expectedIRI {
			t.Errorf("predicate %q: expected IRI %q, got %q", tt.predicate, tt.expectedIRI, meta.StandardIRI)
		}

		if tt.hasInverse && meta.InverseOf == "" {
			t.Errorf("predicate %q: expected inverse to be set", tt.predicate)
		}
	}
}

func TestDelegationInverseRelationship(t *testing.T) {
	vocabulary.ClearRegistry()
	defer vocabulary.ClearRegistry()

	agentic.Register()

	// Verify delegation predicates are properly linked as inverses
	fromMeta := vocabulary.GetPredicateMetadata(agentic.DelegationFrom)
	toMeta := vocabulary.GetPredicateMetadata(agentic.DelegationTo)

	if fromMeta == nil || toMeta == nil {
		t.Fatal("delegation predicates not registered")
	}

	if fromMeta.InverseOf != agentic.DelegationTo {
		t.Errorf("DelegationFrom.InverseOf = %q, want %q", fromMeta.InverseOf, agentic.DelegationTo)
	}

	if toMeta.InverseOf != agentic.DelegationFrom {
		t.Errorf("DelegationTo.InverseOf = %q, want %q", toMeta.InverseOf, agentic.DelegationFrom)
	}
}

func TestCapabilityConfidenceMetadata(t *testing.T) {
	vocabulary.ClearRegistry()
	defer vocabulary.ClearRegistry()

	agentic.Register()

	meta := vocabulary.GetPredicateMetadata(agentic.CapabilityConfidence)
	if meta == nil {
		t.Fatal("CapabilityConfidence not registered")
	}

	if meta.DataType != "float64" {
		t.Errorf("CapabilityConfidence.DataType = %q, want \"float64\"", meta.DataType)
	}

	if meta.Range != "0-1" {
		t.Errorf("CapabilityConfidence.Range = %q, want \"0-1\"", meta.Range)
	}
}

func TestPredicateCount(t *testing.T) {
	vocabulary.ClearRegistry()
	defer vocabulary.ClearRegistry()

	agentic.Register()

	predicates := vocabulary.ListRegisteredPredicates() // predicate-audit:unrelated {"column":16,"surface":"go-assignment:predicates","value":"","basis":"reviewed:runtime-registry-output"}

	// Expected predicates by category:
	// Intent: 5, Capability: 7, Delegation: 7, Accountability: 6, Execution: 7, Action: 5, Task: 5
	// Model: 8, Loop: 17 (15 core + LoopHasStep + LoopDescription + LoopObservedWeb + LoopFetchedWeb)
	// Step: 18 (15 core + tool_status + error_message + error_category), Identity: 6
	// Todo: 5 (ADR-036 — id, content, status, position, updated_at)
	// Scratch: 4 (id, text, created_at, chars)
	// Web: 12 (url, title, snippet, text, source_query, observed_at, fetched_at,
	//   observed_by, fetched_by, content_type, status_code, truncated)
	// Lesson: 13 (ADR-080 — category, polarity, severity, status, summary,
	//   detail, injection-form, evidence, applies-to, observed-role, retired-at,
	//   superseded-by, created-at)
	// Total: 125 predicates
	expectedMin := 125
	if len(predicates) < expectedMin {
		t.Errorf("expected at least %d predicates, got %d", expectedMin, len(predicates))
	}
}

func TestModelPredicatesRegistered(t *testing.T) {
	vocabulary.ClearRegistry()
	defer vocabulary.ClearRegistry()

	agentic.Register()

	modelPredicates := []struct { // predicate-audit:unrelated {"column":21,"surface":"go-assignment:modelPredicates","value":"","basis":"reviewed:runtime-predicate-metadata-collection"}
		name      string
		predicate string
		dataType  string
	}{
		{"ModelProvider", agentic.ModelProvider, "string"},
		{"ModelName", agentic.ModelName, "string"},
		{"ModelMaxTokens", agentic.ModelMaxTokens, "int"},
		{"ModelSupportsTools", agentic.ModelSupportsTools, "bool"},
		{"ModelInputPrice", agentic.ModelInputPrice, "float64"},
		{"ModelOutputPrice", agentic.ModelOutputPrice, "float64"},
		{"ModelEndpointURL", agentic.ModelEndpointURL, "string"},
		{"ModelRateLimit", agentic.ModelRateLimit, "int"},
	}

	for _, tt := range modelPredicates {
		meta := vocabulary.GetPredicateMetadata(tt.predicate)
		if meta == nil {
			t.Errorf("%s (%q): not registered", tt.name, tt.predicate)
			continue
		}
		if meta.DataType != tt.dataType {
			t.Errorf("%s: DataType = %q, want %q", tt.name, meta.DataType, tt.dataType)
		}
	}
}

func TestLoopPredicatesRegistered(t *testing.T) {
	vocabulary.ClearRegistry()
	defer vocabulary.ClearRegistry()

	agentic.Register()

	loopPredicates := []struct { // predicate-audit:unrelated {"column":20,"surface":"go-assignment:loopPredicates","value":"","basis":"reviewed:runtime-predicate-metadata-collection"}
		name      string
		predicate string
		dataType  string
	}{
		{"LoopOutcome", agentic.LoopOutcome, "string"},
		{"LoopTerminalReason", agentic.LoopTerminalReason, "string"},
		{"LoopRole", agentic.LoopRole, "string"},
		{"LoopModelUsed", agentic.LoopModelUsed, "string"},
		{"LoopIterations", agentic.LoopIterations, "int"},
		{"LoopTokensIn", agentic.LoopTokensIn, "int"},
		{"LoopTokensOut", agentic.LoopTokensOut, "int"},
		{"LoopCostUSD", agentic.LoopCostUSD, "float64"},
		{"LoopTask", agentic.LoopTask, "string"},
		{"LoopParent", agentic.LoopParent, "string"},
		{"LoopWorkflow", agentic.LoopWorkflow, "string"},
		{"LoopWorkflowStep", agentic.LoopWorkflowStep, "string"},
		{"LoopEndedAt", agentic.LoopEndedAt, "time.Time"},
		{"LoopUser", agentic.LoopUser, "string"},
		{"LoopHasStep", agentic.LoopHasStep, "string"},
		{"LoopDescription", agentic.LoopDescription, "string"},
		{"LoopObservedWeb", agentic.LoopObservedWeb, "string"},
		{"LoopFetchedWeb", agentic.LoopFetchedWeb, "string"},
	}

	for _, tt := range loopPredicates {
		meta := vocabulary.GetPredicateMetadata(tt.predicate)
		if meta == nil {
			t.Errorf("%s (%q): not registered", tt.name, tt.predicate)
			continue
		}
		if meta.DataType != tt.dataType {
			t.Errorf("%s: DataType = %q, want %q", tt.name, meta.DataType, tt.dataType)
		}
	}
}

func TestStepPredicatesRegistered(t *testing.T) {
	vocabulary.ClearRegistry()
	defer vocabulary.ClearRegistry()

	agentic.Register()

	stepPredicates := []struct { // predicate-audit:unrelated {"column":20,"surface":"go-assignment:stepPredicates","value":"","basis":"reviewed:runtime-predicate-metadata-collection"}
		name      string
		predicate string
		dataType  string
	}{
		{"StepType", agentic.StepType, "string"},
		{"StepIndex", agentic.StepIndex, "int"},
		{"StepLoop", agentic.StepLoop, "string"},
		{"StepTimestamp", agentic.StepTimestamp, "time.Time"},
		{"StepDuration", agentic.StepDuration, "int64"},
		{"StepToolName", agentic.StepToolName, "string"},
		{"StepModel", agentic.StepModel, "string"},
		{"StepTokensIn", agentic.StepTokensIn, "int"},
		{"StepTokensOut", agentic.StepTokensOut, "int"},
		{"StepCapability", agentic.StepCapability, "string"},
		{"StepProvider", agentic.StepProvider, "string"},
		{"StepRetries", agentic.StepRetries, "int"},
		{"StepTokensEvicted", agentic.StepTokensEvicted, "int"},
		{"StepTokensSummarized", agentic.StepTokensSummarized, "int"},
		{"StepUtilization", agentic.StepUtilization, "float64"},
	}

	for _, tt := range stepPredicates {
		meta := vocabulary.GetPredicateMetadata(tt.predicate)
		if meta == nil {
			t.Errorf("%s (%q): not registered", tt.name, tt.predicate)
			continue
		}
		if meta.DataType != tt.dataType {
			t.Errorf("%s: DataType = %q, want %q", tt.name, meta.DataType, tt.dataType)
		}
	}
}

func TestIdentityPredicatesRegistered(t *testing.T) {
	vocabulary.ClearRegistry()
	defer vocabulary.ClearRegistry()

	agentic.Register()

	identityPredicates := []struct { // predicate-audit:unrelated {"column":24,"surface":"go-assignment:identityPredicates","value":"","basis":"reviewed:runtime-predicate-metadata-collection"}
		name      string
		predicate string
		dataType  string
	}{
		{"IdentityDID", agentic.IdentityDID, "string"},
		{"IdentityCredential", agentic.IdentityCredential, "string"},
		{"IdentityIssuer", agentic.IdentityIssuer, "string"},
		{"IdentityVerified", agentic.IdentityVerified, "bool"},
		{"IdentityDisplayName", agentic.IdentityDisplayName, "string"},
		{"IdentityRole", agentic.IdentityRole, "string"},
	}

	for _, tt := range identityPredicates {
		meta := vocabulary.GetPredicateMetadata(tt.predicate)
		if meta == nil {
			t.Errorf("%s (%q): not registered", tt.name, tt.predicate)
			continue
		}
		if meta.DataType != tt.dataType {
			t.Errorf("%s: DataType = %q, want %q", tt.name, meta.DataType, tt.dataType)
		}
	}
}

func TestTodoPredicatesRegistered(t *testing.T) {
	vocabulary.ClearRegistry()
	defer vocabulary.ClearRegistry()

	agentic.Register()

	todoPredicates := []struct { // predicate-audit:unrelated {"column":20,"surface":"go-assignment:todoPredicates","value":"","basis":"reviewed:runtime-predicate-metadata-collection"}
		name       string
		predicate  string
		dataType   string
		ruleOpaque bool
	}{
		{"TodoID", agentic.TodoID, "string", false},
		{"TodoContent", agentic.TodoContent, "string", true},
		{"TodoStatus", agentic.TodoStatus, "string", false},
		{"TodoPosition", agentic.TodoPosition, "int", false},
		{"TodoUpdatedAt", agentic.TodoUpdatedAt, "time.Time", false},
	}

	for _, tt := range todoPredicates {
		meta := vocabulary.GetPredicateMetadata(tt.predicate)
		if meta == nil {
			t.Errorf("%s (%q): not registered", tt.name, tt.predicate)
			continue
		}
		if meta.DataType != tt.dataType {
			t.Errorf("%s: DataType = %q, want %q", tt.name, meta.DataType, tt.dataType)
		}
		if meta.RuleOpaque != tt.ruleOpaque {
			t.Errorf("%s: RuleOpaque = %v, want %v (ADR-036 §Decision Rule 1: rules MUST NOT predicate on opaque content)",
				tt.name, meta.RuleOpaque, tt.ruleOpaque)
		}
	}
}

// TestScratchPredicatesRegistered asserts the 4 scratchpad predicates
// land with the right data types and rule-opaque flags. ScratchText is
// rule-opaque per the LLM-authored-content discipline; the other three
// (id, created_at, chars) stay rule-matchable as structural facts.
func TestScratchPredicatesRegistered(t *testing.T) {
	vocabulary.ClearRegistry()
	defer vocabulary.ClearRegistry()

	agentic.Register()

	scratchPredicates := []struct { // predicate-audit:unrelated {"column":23,"surface":"go-assignment:scratchPredicates","value":"","basis":"reviewed:runtime-predicate-metadata-collection"}
		name       string
		predicate  string
		dataType   string
		ruleOpaque bool
	}{
		{"ScratchID", agentic.ScratchID, "string", false},
		{"ScratchText", agentic.ScratchText, "string", true},
		{"ScratchCreatedAt", agentic.ScratchCreatedAt, "time.Time", false},
		{"ScratchChars", agentic.ScratchChars, "int", false},
	}

	for _, tt := range scratchPredicates {
		meta := vocabulary.GetPredicateMetadata(tt.predicate)
		if meta == nil {
			t.Errorf("%s (%q): not registered", tt.name, tt.predicate)
			continue
		}
		if meta.DataType != tt.dataType {
			t.Errorf("%s: DataType = %q, want %q", tt.name, meta.DataType, tt.dataType)
		}
		if meta.RuleOpaque != tt.ruleOpaque {
			t.Errorf("%s: RuleOpaque = %v, want %v (LLM-authored content must not be rule-predicable — Goodhart feedback loop)",
				tt.name, meta.RuleOpaque, tt.ruleOpaque)
		}
	}
}

// TestWebPredicatesRegistered asserts the 12 web predicates land with the
// right data types and rule-opaque flags. Title, snippet, text, and
// source_query are rule-opaque per the LLM-authored content discipline
// (rules predicating on LLM-sampled values create Goodhart feedback loops).
func TestWebPredicatesRegistered(t *testing.T) {
	vocabulary.ClearRegistry()
	defer vocabulary.ClearRegistry()

	agentic.Register()

	webPredicates := []struct { // predicate-audit:unrelated {"column":19,"surface":"go-assignment:webPredicates","value":"","basis":"reviewed:runtime-predicate-metadata-collection"}
		name       string
		predicate  string
		dataType   string
		ruleOpaque bool
	}{
		{"WebURL", agentic.WebURL, "string", false},
		{"WebTitle", agentic.WebTitle, "string", true},
		{"WebSnippet", agentic.WebSnippet, "string", true},
		{"WebText", agentic.WebText, "string", true},
		{"WebSourceQuery", agentic.WebSourceQuery, "string", true},
		{"WebObservedAt", agentic.WebObservedAt, "time.Time", false},
		{"WebFetchedAt", agentic.WebFetchedAt, "time.Time", false},
		{"WebObservedBy", agentic.WebObservedBy, "string", false},
		{"WebFetchedBy", agentic.WebFetchedBy, "string", false},
		{"WebContentType", agentic.WebContentType, "string", false},
		{"WebStatusCode", agentic.WebStatusCode, "int", false},
		{"WebTruncated", agentic.WebTruncated, "bool", false},
	}

	opaqueCount := 0
	matchableCount := 0
	for _, tt := range webPredicates {
		meta := vocabulary.GetPredicateMetadata(tt.predicate)
		if meta == nil {
			t.Errorf("%s (%q): not registered", tt.name, tt.predicate)
			continue
		}
		if meta.DataType != tt.dataType {
			t.Errorf("%s: DataType = %q, want %q", tt.name, meta.DataType, tt.dataType)
		}
		if meta.RuleOpaque != tt.ruleOpaque {
			t.Errorf("%s: RuleOpaque = %v, want %v (LLM-authored content must not be rule-predicable — Goodhart feedback loop)",
				tt.name, meta.RuleOpaque, tt.ruleOpaque)
		}
		if tt.ruleOpaque {
			opaqueCount++
		} else {
			matchableCount++
		}
	}

	// Aligns with the semteams 2026-05-11 review: 4 rule-opaque + 8 rule-matchable.
	if opaqueCount != 4 {
		t.Errorf("rule-opaque count = %d, want 4 (title, snippet, text, source_query)", opaqueCount)
	}
	if matchableCount != 8 {
		t.Errorf("rule-matchable count = %d, want 8 (url, observed_at, fetched_at, observed_by, fetched_by, content_type, status_code, truncated)", matchableCount)
	}
}

// TestLessonPredicatesRegistered asserts the 13 agent.lesson.* predicates
// (ADR-080) land with the right data types and rule-opaque flags. The three
// authored-text predicates (summary, detail, injection-form) are rule-opaque
// per the LLM-authored-content discipline (rules predicating on model-sampled
// prose create Goodhart feedback loops); the enumerated/structural and
// reference predicates stay rule-matchable so curation rules can gate on them.
func TestLessonPredicatesRegistered(t *testing.T) {
	vocabulary.ClearRegistry()
	defer vocabulary.ClearRegistry()

	agentic.Register()

	lessonPredicates := []struct {
		name       string
		predicate  string
		dataType   string
		ruleOpaque bool
	}{
		{"LessonCategory", agentic.LessonCategory, "string", false},
		{"LessonPolarity", agentic.LessonPolarity, "string", false},
		{"LessonSeverity", agentic.LessonSeverity, "string", false},
		{"LessonStatus", agentic.LessonStatus, "string", false},
		{"LessonSummary", agentic.LessonSummary, "string", true},
		{"LessonDetail", agentic.LessonDetail, "string", true},
		{"LessonInjectionForm", agentic.LessonInjectionForm, "string", true},
		{"LessonEvidence", agentic.LessonEvidence, "string", false},
		{"LessonAppliesTo", agentic.LessonAppliesTo, "string", false},
		{"LessonObservedRole", agentic.LessonObservedRole, "string", false},
		{"LessonRetiredAt", agentic.LessonRetiredAt, "time.Time", false},
		{"LessonSupersededBy", agentic.LessonSupersededBy, "string", false},
		{"LessonCreatedAt", agentic.LessonCreatedAt, "time.Time", false},
	}

	opaqueCount := 0
	matchableCount := 0
	for _, tt := range lessonPredicates {
		meta := vocabulary.GetPredicateMetadata(tt.predicate)
		if meta == nil {
			t.Errorf("%s (%q): not registered", tt.name, tt.predicate)
			continue
		}
		if meta.DataType != tt.dataType {
			t.Errorf("%s: DataType = %q, want %q", tt.name, meta.DataType, tt.dataType)
		}
		if meta.RuleOpaque != tt.ruleOpaque {
			t.Errorf("%s: RuleOpaque = %v, want %v (LLM-authored content must not be rule-predicable — Goodhart feedback loop)",
				tt.name, meta.RuleOpaque, tt.ruleOpaque)
		}
		if tt.ruleOpaque {
			opaqueCount++
		} else {
			matchableCount++
		}
	}

	// ADR-080 rule-visibility split: 3 rule-opaque authored-text
	// (summary, detail, injection-form) + 10 rule-matchable structural/reference.
	if opaqueCount != 3 {
		t.Errorf("rule-opaque count = %d, want 3 (summary, detail, injection-form)", opaqueCount)
	}
	if matchableCount != 10 {
		t.Errorf("rule-matchable count = %d, want 10 (category, polarity, severity, status, evidence, applies-to, observed-role, retired-at, superseded-by, created-at)", matchableCount)
	}

	// agent.lesson.category is the OPEN classifier — rule-matchable, no closed
	// value set enforced at the registry (product taxonomy; ADR-080). The registry
	// has no enum mechanism, so "open" is asserted here as rule-matchable string.
	catMeta := vocabulary.GetPredicateMetadata(agentic.LessonCategory)
	if catMeta == nil {
		t.Fatal("LessonCategory not registered")
	}
	if catMeta.RuleOpaque {
		t.Error("LessonCategory must stay rule-matchable (open product taxonomy, not opaque prose)")
	}

	// agent.lesson.evidence carries the concrete PROV-O derivation IRI
	// prov:wasDerivedFrom (annotation only; constant lives in standards.go).
	evMeta := vocabulary.GetPredicateMetadata(agentic.LessonEvidence)
	if evMeta == nil {
		t.Fatal("LessonEvidence not registered")
	}
	if evMeta.StandardIRI != "http://www.w3.org/ns/prov#wasDerivedFrom" {
		t.Errorf("LessonEvidence.StandardIRI = %q, want %q (prov:wasDerivedFrom)",
			evMeta.StandardIRI, "http://www.w3.org/ns/prov#wasDerivedFrom")
	}
	if evMeta.StandardIRI != vocabulary.ProvWasDerivedFrom {
		t.Errorf("LessonEvidence.StandardIRI = %q, want vocabulary.ProvWasDerivedFrom %q",
			evMeta.StandardIRI, vocabulary.ProvWasDerivedFrom)
	}
}

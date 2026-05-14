package oasfgenerator

import (
	"testing"
	"time"

	"github.com/c360studio/semstreams/message"
	agentic "github.com/c360studio/semstreams/vocabulary/agentic"
	"github.com/c360studio/semstreams/vocabulary/oasf"
)

func TestMapper_MapTriplesToOASF_BasicCapability(t *testing.T) {
	mapper := NewMapper("1.0.0", []string{"system"}, true)

	agentID := "acme.ops.agentic.system.agent.architect"
	capabilityContext := "software-design" // Links related capability triples
	triples := []message.Triple{
		{
			Subject:   agentID,
			Predicate: agentic.CapabilityName,
			Object:    "Software Design",
			Source:    "test",
			Timestamp: time.Now(),
			Context:   capabilityContext,
		},
		{
			Subject:   agentID,
			Predicate: agentic.CapabilityDescription,
			Object:    "Creates software architecture diagrams",
			Source:    "test",
			Timestamp: time.Now(),
			Context:   capabilityContext,
		},
		{
			Subject:   agentID,
			Predicate: agentic.CapabilityExpression,
			Object:    "software-design",
			Source:    "test",
			Timestamp: time.Now(),
			Context:   capabilityContext,
		},
		{
			Subject:   agentID,
			Predicate: agentic.CapabilityConfidence,
			Object:    0.95,
			Source:    "test",
			Timestamp: time.Now(),
			Context:   capabilityContext,
		},
	}

	record, err := mapper.MapTriplesToOASF(agentID, triples)
	if err != nil {
		t.Fatalf("MapTriplesToOASF() error = %v", err)
	}

	if len(record.Skills) == 0 {
		t.Fatal("expected at least one skill")
	}

	// "software-design" is not in the published OASF taxonomy at MVP
	// coverage, so it resolves to an extension class. Locate the skill
	// by its extension ID and assert the new wire shape:
	//   - ID is the deterministic ExtensionID for "software-design"
	//   - Name is the semstreams/-prefixed hierarchical form
	//   - The human display label ("Software Design") survives on
	//     Description (CapabilityName fallback per the mapper contract)
	wantID := oasf.ExtensionID("software-design")
	var skill *OASFSkill
	for i := range record.Skills {
		if record.Skills[i].ID == wantID {
			skill = &record.Skills[i]
			break
		}
	}

	if skill == nil {
		t.Fatalf("expected to find skill with ExtensionID(%q)=%d", "software-design", wantID)
	}
	if !oasf.IsExtension(skill.ID) {
		t.Errorf("skill.ID = %d, want extension-range value", skill.ID)
	}
	if skill.Name != "semstreams/software_design" {
		t.Errorf("skill.Name = %q, want \"semstreams/software_design\"", skill.Name)
	}
	// CapabilityDescription set explicitly → wins over CapabilityName
	// fallback. (When no description is supplied, the mapper preserves
	// CapabilityName on Description — covered by the contract test.)
	if skill.Description != "Creates software architecture diagrams" {
		t.Errorf("skill.Description = %q, want explicit CapabilityDescription", skill.Description)
	}
}

func TestMapper_MapTriplesToOASF_WithPermissions(t *testing.T) {
	mapper := NewMapper("1.0.0", []string{"system"}, true)

	agentID := "acme.ops.agentic.system.agent.editor"
	triples := []message.Triple{
		{
			Subject:   agentID,
			Predicate: agentic.CapabilityExpression,
			Object:    "file-editing",
			Context:   "file-editing",
			Source:    "test",
			Timestamp: time.Now(),
		},
		{
			Subject:   agentID,
			Predicate: agentic.CapabilityName,
			Object:    "File Editing",
			Context:   "file-editing",
			Source:    "test",
			Timestamp: time.Now(),
		},
		{
			Subject:   agentID,
			Predicate: agentic.CapabilityPermission,
			Object:    "file_read",
			Context:   "file-editing",
			Source:    "test",
			Timestamp: time.Now(),
		},
		{
			Subject:   agentID,
			Predicate: agentic.CapabilityPermission,
			Object:    "file_write",
			Context:   "file-editing",
			Source:    "test",
			Timestamp: time.Now(),
		},
	}

	record, err := mapper.MapTriplesToOASF(agentID, triples)
	if err != nil {
		t.Fatalf("MapTriplesToOASF() error = %v", err)
	}

	if len(record.Skills) == 0 {
		t.Fatal("expected at least one skill")
	}

	skill := record.Skills[0]
	if len(skill.Permissions) != 2 {
		t.Fatalf("expected 2 permissions, got %d", len(skill.Permissions))
	}
}

func TestMapper_MapTriplesToOASF_WithIntent(t *testing.T) {
	mapper := NewMapper("1.0.0", []string{"system"}, true)

	agentID := "acme.ops.agentic.system.agent.analyst"
	triples := []message.Triple{
		{
			Subject:   agentID,
			Predicate: agentic.CapabilityName,
			Object:    "Data Analysis",
			Source:    "test",
			Timestamp: time.Now(),
		},
		{
			Subject:   agentID,
			Predicate: agentic.IntentGoal,
			Object:    "Analyze data and provide insights",
			Source:    "test",
			Timestamp: time.Now(),
		},
		{
			Subject:   agentID,
			Predicate: agentic.IntentType,
			Object:    "data-analysis",
			Source:    "test",
			Timestamp: time.Now(),
		},
	}

	record, err := mapper.MapTriplesToOASF(agentID, triples)
	if err != nil {
		t.Fatalf("MapTriplesToOASF() error = %v", err)
	}

	if record.Description != "Analyze data and provide insights" {
		t.Errorf("expected description from intent goal, got %q", record.Description)
	}

	if len(record.Domains) != 1 {
		t.Fatalf("expected 1 domain, got %d", len(record.Domains))
	}

	if record.Domains[0].Name != "data-analysis" {
		t.Errorf("expected domain 'data-analysis', got %q", record.Domains[0].Name)
	}
}

func TestMapper_MapTriplesToOASF_WithExtensions(t *testing.T) {
	mapper := NewMapper("1.0.0", []string{"system"}, true)

	agentID := "acme.ops.agentic.system.agent.builder"
	triples := []message.Triple{
		{
			Subject:   agentID,
			Predicate: agentic.CapabilityName,
			Object:    "Build",
			Source:    "test",
			Timestamp: time.Now(),
		},
		{
			Subject:   agentID,
			Predicate: agentic.ActionType,
			Object:    "tool-call",
			Source:    "test",
			Timestamp: time.Now(),
		},
	}

	record, err := mapper.MapTriplesToOASF(agentID, triples)
	if err != nil {
		t.Fatalf("MapTriplesToOASF() error = %v", err)
	}

	if record.Extensions == nil {
		t.Fatal("expected extensions to be set")
	}

	if record.Extensions["semstreams_entity_id"] != agentID {
		t.Errorf("expected semstreams_entity_id extension, got %v", record.Extensions["semstreams_entity_id"])
	}

	if record.Extensions["source"] != "semstreams" {
		t.Errorf("expected source extension 'semstreams', got %v", record.Extensions["source"])
	}
}

// TestMapper_OASFClassOverride exercises the operator-override path:
// when CapabilityOASFClass is set on a capability, the mapper uses that
// class ID verbatim and resolves the hierarchical name through the
// vocabulary/oasf taxonomy (rather than ExtensionID/ExtensionName
// derived from the source expression).
func TestMapper_OASFClassOverride(t *testing.T) {
	mapper := NewMapper("1.0.0", []string{"system"}, false)

	agentID := "acme.ops.agentic.system.agent.overrider"
	const skillContext = "code-review"
	triples := []message.Triple{
		// Source expression would normally resolve to an extension —
		// but the operator pins this skill to OASF Tool Interaction.
		{Subject: agentID, Predicate: agentic.CapabilityExpression, Object: "code-review", Context: skillContext, Timestamp: time.Now()},
		{Subject: agentID, Predicate: agentic.CapabilityName, Object: "Code Review", Context: skillContext, Timestamp: time.Now()},
		{Subject: agentID, Predicate: agentic.CapabilityOASFClass, Object: int64(oasf.CategoryToolInteraction), Context: skillContext, Timestamp: time.Now()},
	}

	record, err := mapper.MapTriplesToOASF(agentID, triples)
	if err != nil {
		t.Fatalf("MapTriplesToOASF: %v", err)
	}
	if len(record.Skills) != 1 {
		t.Fatalf("expected 1 skill, got %d", len(record.Skills))
	}
	skill := record.Skills[0]

	if skill.ID != oasf.CategoryToolInteraction {
		t.Errorf("skill.ID = %d, want %d (operator override)", skill.ID, oasf.CategoryToolInteraction)
	}
	if oasf.IsExtension(skill.ID) {
		t.Errorf("operator-overridden ID resolved as extension: %d", skill.ID)
	}
	if skill.Name != "tool_interaction" {
		t.Errorf("skill.Name = %q, want \"tool_interaction\" (canonical name from oasf.Name)", skill.Name)
	}
}

// TestMapper_OASFClassOverride_NonCoveredCanonicalIDFails asserts that
// pinning an OASF class ID outside MVP coverage AND outside the
// extension range (e.g., uid=99 — not a SemStreams constant, not
// >= ExtensionBase) is rejected at the generator. This is the
// "operator pinned a hypothetical class" failure mode the previous
// resolver silently accepted (PR-O2 go-reviewer SHOULD-FIX #1).
func TestMapper_OASFClassOverride_NonCoveredCanonicalIDFails(t *testing.T) {
	mapper := NewMapper("1.0.0", []string{"system"}, false)

	agentID := "acme.ops.agentic.system.agent.bad-override"
	const skillContext = "code-review"
	triples := []message.Triple{
		{Subject: agentID, Predicate: agentic.CapabilityExpression, Object: "code-review", Context: skillContext, Timestamp: time.Now()},
		// 99 is not in our MVP constants and not in the extension range.
		{Subject: agentID, Predicate: agentic.CapabilityOASFClass, Object: int64(99), Context: skillContext, Timestamp: time.Now()},
	}

	_, err := mapper.MapTriplesToOASF(agentID, triples)
	if err == nil {
		t.Fatal("expected error for non-covered canonical override ID")
	}
}

// TestMapper_OASFClassOverride_WinsOverExtensionFallback covers the
// case the prior tests left untested: a skill whose expression *would*
// otherwise resolve to an extension ID, while a canonical operator
// override is also set. The override must win — proves the precedence
// in resolveSkillIdentity isn't quietly re-ordered to "extension
// fallback first if anything else fails".
func TestMapper_OASFClassOverride_WinsOverExtensionFallback(t *testing.T) {
	mapper := NewMapper("1.0.0", []string{"system"}, false)

	agentID := "acme.ops.agentic.system.agent.dual"
	const skillContext = "code-review"
	triples := []message.Triple{
		// Without override, "code-review" → ExtensionID (no canonical match).
		{Subject: agentID, Predicate: agentic.CapabilityExpression, Object: "code-review", Context: skillContext, Timestamp: time.Now()},
		// Override pins to NLP — should beat the extension fallback.
		{Subject: agentID, Predicate: agentic.CapabilityOASFClass, Object: int64(oasf.CategoryNLP), Context: skillContext, Timestamp: time.Now()},
	}

	record, err := mapper.MapTriplesToOASF(agentID, triples)
	if err != nil {
		t.Fatalf("MapTriplesToOASF: %v", err)
	}
	if len(record.Skills) != 1 {
		t.Fatalf("expected 1 skill, got %d", len(record.Skills))
	}
	skill := record.Skills[0]

	if skill.ID != oasf.CategoryNLP {
		t.Errorf("skill.ID = %d, want %d (operator override must beat extension fallback)", skill.ID, oasf.CategoryNLP)
	}
	if oasf.IsExtension(skill.ID) {
		t.Errorf("override-resolved ID landed in extension range: %d", skill.ID)
	}
	if skill.Name != "natural_language_processing" {
		t.Errorf("skill.Name = %q, want \"natural_language_processing\" (canonical, not semstreams/code_review)", skill.Name)
	}
}

// TestMapper_OASFClassOverride_ZeroIgnored asserts that an override
// triple carrying zero is treated as "no override" — same behaviour as
// the triple being absent.
func TestMapper_OASFClassOverride_ZeroIgnored(t *testing.T) {
	mapper := NewMapper("1.0.0", []string{"system"}, false)

	agentID := "acme.ops.agentic.system.agent.zero"
	const skillContext = "code-review"
	triples := []message.Triple{
		{Subject: agentID, Predicate: agentic.CapabilityExpression, Object: "code-review", Context: skillContext, Timestamp: time.Now()},
		{Subject: agentID, Predicate: agentic.CapabilityOASFClass, Object: int64(0), Context: skillContext, Timestamp: time.Now()},
	}

	record, err := mapper.MapTriplesToOASF(agentID, triples)
	if err != nil {
		t.Fatalf("MapTriplesToOASF: %v", err)
	}
	if len(record.Skills) != 1 {
		t.Fatalf("expected 1 skill, got %d", len(record.Skills))
	}
	if !oasf.IsExtension(record.Skills[0].ID) {
		t.Errorf("zero override should not suppress extension fallback; got ID=%d", record.Skills[0].ID)
	}
}

func TestMapper_MapTriplesToOASF_NoExtensions(t *testing.T) {
	mapper := NewMapper("1.0.0", []string{"system"}, false)

	agentID := "acme.ops.agentic.system.agent.simple"
	triples := []message.Triple{
		{
			Subject:   agentID,
			Predicate: agentic.CapabilityName,
			Object:    "Simple Task",
			Source:    "test",
			Timestamp: time.Now(),
		},
	}

	record, err := mapper.MapTriplesToOASF(agentID, triples)
	if err != nil {
		t.Fatalf("MapTriplesToOASF() error = %v", err)
	}

	// Extensions should not include semstreams-specific fields
	if record.Extensions != nil && record.Extensions["semstreams_entity_id"] != nil {
		t.Error("expected no semstreams extensions when disabled")
	}
}

func TestMapper_MapTriplesToOASF_EmptyTriples(t *testing.T) {
	mapper := NewMapper("1.0.0", []string{"system"}, true)

	_, err := mapper.MapTriplesToOASF("test.entity", nil)
	if err == nil {
		t.Error("expected error for empty triples")
	}

	_, err = mapper.MapTriplesToOASF("test.entity", []message.Triple{})
	if err == nil {
		t.Error("expected error for empty triples slice")
	}
}

func TestExtractAgentName(t *testing.T) {
	tests := []struct {
		entityID string
		want     string
	}{
		{"acme.ops.agentic.system.agent.architect", "agent-architect"},
		{"org.platform.domain.system.type.instance", "type-instance"},
		{"simple.entity", "simple-entity"},
		{"single", "single"},
	}

	for _, tt := range tests {
		t.Run(tt.entityID, func(t *testing.T) {
			got := extractAgentName(tt.entityID)
			if got != tt.want {
				t.Errorf("extractAgentName(%q) = %q, want %q", tt.entityID, got, tt.want)
			}
		})
	}
}

func TestSupportedPredicates(t *testing.T) {
	predicates := SupportedPredicates()

	if len(predicates) == 0 {
		t.Error("expected supported predicates to be returned")
	}

	// Verify some key predicates are included
	expected := []string{
		agentic.CapabilityName,
		agentic.CapabilityDescription,
		agentic.IntentGoal,
	}

	for _, exp := range expected {
		found := false
		for _, pred := range predicates {
			if pred == exp {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("expected predicate %q to be in supported list", exp)
		}
	}
}

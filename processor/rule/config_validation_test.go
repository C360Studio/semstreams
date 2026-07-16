package rule

import (
	"log/slog"
	"strings"
	"testing"

	"github.com/c360studio/semstreams/internal/semantictest"
	"github.com/c360studio/semstreams/natsclient"
	"github.com/c360studio/semstreams/pkg/errs"
	"github.com/c360studio/semstreams/processor/rule/expression"
	"github.com/c360studio/semstreams/vocabulary"
)

// definitionFromMap is the hot-reload wire-format → Definition decoder.
// The tests here lock the field-level parsing contract so a future
// refactor cannot silently drop a previously-supported field.

// Regression: pre-Chunk-2 the hot-reload path silently dropped the
// `cooldown` field — definitionFromMap forgot to read it, so a rule
// re-applied via ApplyConfigUpdate would lose its cooldown gating
// even though the on-disk config still set it. Chunk 2's fix added
// the parse line; this test pins it. See
// project_cron_rule_implementation.md → "Two deferred SHOULD-FIX items
// from Chunk 2 reviewer".
func TestDefinitionFromMap_PreservesCooldownOnHotReload(t *testing.T) {
	t.Parallel()

	ruleMap := map[string]any{
		"id":       "battery-low",
		"type":     "expression",
		"name":     "Battery low",
		"enabled":  true,
		"cooldown": "5m",
	}

	def, err := definitionFromMap("battery-low", ruleMap)
	if err != nil {
		t.Fatalf("definitionFromMap = %v, want nil", err)
	}
	if def.Cooldown != "5m" {
		t.Errorf("Cooldown = %q, want %q (hot-reload must not drop cooldown)", def.Cooldown, "5m")
	}
}

// Sibling regressions: the cron-side fields (schedule, actions,
// metadata) were added in the same commit that fixed cooldown. Pin
// them too so a future round-trip refactor can't drop them silently.
func TestDefinitionFromMap_PreservesCronFields(t *testing.T) {
	t.Parallel()

	ruleMap := map[string]any{
		"id":       "weekly-planning",
		"type":     "cron",
		"name":     "Weekly planning prompt",
		"enabled":  true,
		"schedule": "0 9 * * MON",
		"actions": []any{
			map[string]any{
				"type":    "publish",
				"subject": "system.cron.heartbeat",
			},
		},
		"metadata": map[string]any{
			"om_entry_id": "weekly-planning-block",
		},
	}

	def, err := definitionFromMap("weekly-planning", ruleMap)
	if err != nil {
		t.Fatalf("definitionFromMap = %v, want nil", err)
	}
	if def.Schedule != "0 9 * * MON" {
		t.Errorf("Schedule = %q, want %q", def.Schedule, "0 9 * * MON")
	}
	if len(def.Actions) != 1 {
		t.Fatalf("len(Actions) = %d, want 1", len(def.Actions))
	}
	if def.Actions[0].Type != "publish" || def.Actions[0].Subject != "system.cron.heartbeat" {
		t.Errorf("Action = %+v, want type=publish subject=system.cron.heartbeat", def.Actions[0])
	}
	if got := def.Metadata["om_entry_id"]; got != "weekly-planning-block" {
		t.Errorf("Metadata[om_entry_id] = %v, want %q", got, "weekly-planning-block")
	}
}

// Empty-cooldown remains the empty string (not the zero-time literal
// or some other sentinel) so the downstream parse step (cron_rule.go
// for cron rules, expression-factory for expression rules) can branch
// on len() == 0.
func TestDefinitionFromMap_AbsentCooldownIsEmptyString(t *testing.T) {
	t.Parallel()

	ruleMap := map[string]any{
		"id":      "no-cooldown",
		"type":    "expression",
		"enabled": true,
	}
	def, err := definitionFromMap("no-cooldown", ruleMap)
	if err != nil {
		t.Fatalf("definitionFromMap = %v, want nil", err)
	}
	if def.Cooldown != "" {
		t.Errorf("Cooldown = %q, want empty string for absent field", def.Cooldown)
	}
}

// TestValidateExpressionRule_RejectsRuleOpaqueField pins ADR-036's Rule 1:
// rules MUST NOT predicate on fields the vocabulary marks RuleOpaque.
// The validator catches this at config-load so a misconfigured rule
// never starts; structural fields (status, position) on the same
// predicate family stay rule-matchable.
func TestValidateExpressionRule_RejectsRuleOpaqueField(t *testing.T) {
	// Snapshot the registry so test-registered predicates don't leak as
	// stub entries to follow-on tests. SnapshotRegistry returns a closure
	// that restores the pre-test state on defer.
	defer vocabulary.SnapshotRegistry()()

	const opaqueField = "test.todo.content"
	const structuralField = "test.todo.status"

	vocabulary.RegisterPredicate(vocabulary.PredicateMetadata{
		Name:       opaqueField,
		DataType:   "string",
		RuleOpaque: true,
	})
	vocabulary.RegisterPredicate(vocabulary.PredicateMetadata{
		Name:     structuralField,
		DataType: "string",
	})

	processor := &Processor{
		natsClient: &natsclient.Client{},
		logger:     slog.Default(),
		rules:      make(map[string]Rule),
	}

	t.Run("rejects condition on opaque field", func(t *testing.T) {
		err := processor.ValidateConfigUpdate(map[string]any{
			"rules": map[string]any{
				"opaque_match": map[string]any{
					"type": "test_rule",
					"conditions": []any{
						map[string]any{
							"field":    semantictest.Predicate(t, "test", "todo", "content"),
							"operator": "eq",
							"value":    "anything",
						},
					},
				},
			},
		})
		if err == nil {
			t.Fatal("expected error rejecting rule-opaque field, got nil")
		}
		if got := err.Error(); !strings.Contains(got, "rule-opaque") {
			t.Errorf("error %q should mention rule-opaque (ADR-036 Rule 1)", got)
		}
		// Pin the WrapInvalid choice — a future refactor that switches to
		// plain fmt.Errorf would silently break callers using errs.IsInvalid.
		if !errs.IsInvalid(err) {
			t.Errorf("expected ErrorInvalid class for rule-opaque rejection, got %T: %v", err, err)
		}
	})

	t.Run("accepts condition on structural field of same family", func(t *testing.T) {
		err := processor.ValidateConfigUpdate(map[string]any{
			"rules": map[string]any{
				"structural_match": map[string]any{
					"type": "test_rule",
					"conditions": []any{
						map[string]any{
							"field":    semantictest.Predicate(t, "test", "todo", "status"),
							"operator": "eq",
							"value":    "completed",
						},
					},
				},
			},
		})
		if err != nil {
			t.Errorf("structural field %q must remain rule-matchable: %v", structuralField, err)
		}
	})

	t.Run("file-load path catches rule-opaque field via ValidateDefinition", func(t *testing.T) {
		// Stage 1.5 — file-load (definitionFromMap → ValidateDefinition)
		// must catch rule-opaque field references the same way the
		// hot-reload path (ValidateConfigUpdate) does. ADR-036's
		// discipline claim depends on both gates being equivalent.
		def := Definition{
			ID:   "file_load_opaque",
			Type: "test_rule",
			Conditions: []expression.ConditionExpression{
				{Field: opaqueField, Operator: "eq", Value: "anything"},
			},
		}
		err := ValidateDefinition(def)
		if err == nil {
			t.Fatal("expected ValidateDefinition to reject rule-opaque field on file-load path, got nil")
		}
		if !strings.Contains(err.Error(), "rule-opaque") {
			t.Errorf("error %q should mention rule-opaque", err.Error())
		}
		if !errs.IsInvalid(err) {
			t.Errorf("expected ErrorInvalid class, got %T: %v", err, err)
		}
	})

	t.Run("file-load path accepts structural sibling via ValidateDefinition", func(t *testing.T) {
		def := Definition{
			ID:   "file_load_structural",
			Type: "test_rule",
			Conditions: []expression.ConditionExpression{
				{Field: structuralField, Operator: "eq", Value: "completed"},
			},
		}
		if err := ValidateDefinition(def); err != nil {
			t.Errorf("ValidateDefinition must accept structural field on file-load path: %v", err)
		}
	})

	t.Run("rejects condition on unregistered graph predicate", func(t *testing.T) {
		err := processor.ValidateConfigUpdate(map[string]any{
			"rules": map[string]any{
				"unregistered_match": map[string]any{
					"type": "test_rule",
					"conditions": []any{
						map[string]any{
							"field":    "totally.new.field",
							"operator": "eq",
							"value":    "x",
						},
					},
				},
			},
		})
		if err == nil {
			t.Fatal("unregistered graph predicate unexpectedly accepted")
		}
		if !strings.Contains(err.Error(), "not declared") {
			t.Fatalf("error %q should identify the missing declaration", err)
		}
	})
}

// --- ADR-053 D4: run_scope validation tests (I2) ---

// TestValidateActionLists_RunScopeValid verifies that valid run_scope values
// ("new", "inherit", "none", "") pass ValidateDefinition without error.
func TestValidateActionLists_RunScopeValid(t *testing.T) {
	t.Parallel()
	for _, scope := range []string{"new", "inherit", "none", ""} {
		scope := scope
		t.Run("scope_"+scope, func(t *testing.T) {
			t.Parallel()
			def := Definition{
				ID:   "run-scope-valid",
				Type: "expression",
				Actions: []Action{
					{
						Type:     ActionTypePublishAgent,
						Subject:  "agent.task.test",
						RunScope: scope,
					},
				},
			}
			if err := ValidateDefinition(def); err != nil {
				t.Errorf("run_scope=%q must be accepted by ValidateDefinition, got: %v", scope, err)
			}
		})
	}
}

// TestValidateActionLists_RunScopeInvalidRejects verifies that an invalid run_scope
// value ("always", "auto", "foo") is rejected by ValidateDefinition. This exercises
// the closed-set validator in validateActionLists (previously unexercised per I2).
func TestValidateActionLists_RunScopeInvalidRejects(t *testing.T) {
	t.Parallel()
	for _, scope := range []string{"always", "auto", "foo", "NEW"} {
		scope := scope
		t.Run("scope_"+scope, func(t *testing.T) {
			t.Parallel()
			def := Definition{
				ID:   "run-scope-invalid",
				Type: "expression",
				Actions: []Action{
					{
						Type:     ActionTypePublishAgent,
						Subject:  "agent.task.test",
						RunScope: scope,
					},
				},
			}
			err := ValidateDefinition(def)
			if err == nil {
				t.Fatalf("run_scope=%q must be rejected by ValidateDefinition, got nil", scope)
			}
			if !strings.Contains(err.Error(), "run_scope") {
				t.Errorf("error %q must mention 'run_scope', got: %v", scope, err)
			}
		})
	}
}

// TestValidateActionLists_RunScopeOnlyCheckedForPublishAgent verifies that
// run_scope is ignored (not rejected) on non-publish_agent action types.
// A publish action with an invalid run_scope should pass because the field
// is only validated for publish_agent.
func TestValidateActionLists_RunScopeOnlyCheckedForPublishAgent(t *testing.T) {
	t.Parallel()
	def := Definition{
		ID:   "run-scope-non-agent",
		Type: "expression",
		Actions: []Action{
			{
				Type:      ActionTypeAddTriple,
				Predicate: "some.test.predicate",
				Object:    "val",
				RunScope:  "invalid-but-not-publish-agent",
			},
		},
	}
	if err := ValidateDefinition(def); err != nil {
		t.Errorf("run_scope on non-publish_agent action must not be validated, got: %v", err)
	}
}

// TestValidateActionLists_FilesystemPolicyValid verifies that valid
// filesystem_policy values pass ValidateDefinition (ADR-067 / gh#445).
func TestValidateActionLists_FilesystemPolicyValid(t *testing.T) {
	t.Parallel()
	for _, policy := range []string{"read_only", "workspace_write", "host_write", ""} {
		policy := policy
		t.Run("policy_"+policy, func(t *testing.T) {
			t.Parallel()
			def := Definition{
				ID:   "fs-policy-valid",
				Type: "expression",
				Actions: []Action{
					{
						Type:             ActionTypePublishAgent,
						Subject:          "agent.task.test",
						FilesystemPolicy: policy,
					},
				},
			}
			if err := ValidateDefinition(def); err != nil {
				t.Errorf("filesystem_policy=%q must be accepted, got: %v", policy, err)
			}
		})
	}
}

// TestValidateActionLists_FilesystemPolicyInvalidRejects verifies an
// unrecognized filesystem_policy (a typo) fails config load, so the misconfig
// surfaces at load time rather than as a silent fail-closed refusal at dispatch.
func TestValidateActionLists_FilesystemPolicyInvalidRejects(t *testing.T) {
	t.Parallel()
	for _, policy := range []string{"readonly", "read-only", "ro", "foo"} {
		policy := policy
		t.Run("policy_"+policy, func(t *testing.T) {
			t.Parallel()
			def := Definition{
				ID:   "fs-policy-invalid",
				Type: "expression",
				Actions: []Action{
					{
						Type:             ActionTypePublishAgent,
						Subject:          "agent.task.test",
						FilesystemPolicy: policy,
					},
				},
			}
			err := ValidateDefinition(def)
			if err == nil {
				t.Fatalf("filesystem_policy=%q must be rejected, got nil", policy)
			}
			if !strings.Contains(err.Error(), "filesystem_policy") {
				t.Errorf("error for %q must mention 'filesystem_policy', got: %v", policy, err)
			}
		})
	}
}

func TestValidateActionListsRelatedLoopsAcrossEveryActionList(t *testing.T) {
	t.Parallel()
	lists := []struct {
		name string
		set  func(*Definition, []Action)
	}{
		{name: "on_enter", set: func(def *Definition, actions []Action) { def.OnEnter = actions }},
		{name: "on_exit", set: func(def *Definition, actions []Action) { def.OnExit = actions }},
		{name: "while_true", set: func(def *Definition, actions []Action) { def.WhileTrue = actions }},
		{name: "on_recovery", set: func(def *Definition, actions []Action) { def.OnRecovery = actions }},
		{name: "cron_actions", set: func(def *Definition, actions []Action) { def.Actions = actions }},
	}
	for _, list := range lists {
		list := list
		t.Run(list.name, func(t *testing.T) {
			t.Parallel()
			def := Definition{ID: "lineage-" + list.name}
			list.set(&def, []Action{{
				Type: ActionTypePublishAgent,
				RelatedLoops: map[string]string{
					"bad_key": "loop-1",
				},
			}})
			err := ValidateDefinition(def)
			if err == nil || !strings.Contains(err.Error(), "related_loops") {
				t.Fatalf("ValidateDefinition error = %v, want related_loops rejection", err)
			}
		})
	}
}

func TestValidateActionListsRelatedLoopsRejectsInvalidDeclarations(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name   string
		action Action
	}{
		{name: "non publish agent", action: Action{Type: ActionTypePublish, RelatedLoops: map[string]string{"researcher": "loop-1"}}},
		{name: "empty map on non publish agent", action: Action{Type: ActionTypePublish, RelatedLoops: map[string]string{}}},
		{name: "empty key", action: Action{Type: ActionTypePublishAgent, RelatedLoops: map[string]string{"": "loop-1"}}},
		{name: "dotted key", action: Action{Type: ActionTypePublishAgent, RelatedLoops: map[string]string{"research.reviewer": "loop-1"}}},
		{name: "uppercase key", action: Action{Type: ActionTypePublishAgent, RelatedLoops: map[string]string{"Researcher": "loop-1"}}},
		{name: "wildcard key", action: Action{Type: ActionTypePublishAgent, RelatedLoops: map[string]string{"*": "loop-1"}}},
		{name: "empty source", action: Action{Type: ActionTypePublishAgent, RelatedLoops: map[string]string{"researcher": ""}}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			err := ValidateDefinition(Definition{ID: "invalid-lineage", OnEnter: []Action{test.action}})
			if err == nil {
				t.Fatal("ValidateDefinition error = nil, want rejection")
			}
		})
	}
}

func TestDefinitionFromMapRejectsDisabledAndCronRelatedLoops(t *testing.T) {
	t.Parallel()
	tests := []map[string]any{
		{
			"type": "expression", "enabled": false,
			"on_enter": []any{map[string]any{"type": ActionTypePublishAgent, "related_loops": map[string]any{"bad_key": "loop-1"}}},
		},
		{
			"type": CronRuleType, "enabled": false, "schedule": "0 * * * *",
			"actions": []any{map[string]any{"type": ActionTypePublishAgent, "related_loops": map[string]any{"bad_key": "loop-1"}}},
		},
	}
	for _, ruleMap := range tests {
		if _, err := definitionFromMap("disabled-lineage", ruleMap); err == nil {
			t.Fatal("definitionFromMap error = nil, want disabled declaration rejected")
		}
	}
}

func TestValidateDefinitionRejectsNoncanonicalActionPredicate(t *testing.T) {
	t.Parallel()

	def := Definition{
		ID: "invalid-action-predicate",
		OnEnter: []Action{{
			Type:      ActionTypeAddTriple,
			Predicate: "workflow.state.next_phase", // predicate-audit:invalid {"kind":"stored-predicate","value":"workflow.state.next_phase","reason":"segment_character"}
		}},
	}
	err := ValidateDefinition(def)
	if err == nil {
		t.Fatal("ValidateDefinition unexpectedly accepted a noncanonical action predicate")
	}
	if !strings.Contains(err.Error(), "segment_character") {
		t.Fatalf("validation error %q does not identify the structural reason", err)
	}
}

func TestValidateDefinitionAcceptsCanonicalActionPredicate(t *testing.T) {
	t.Parallel()

	def := Definition{
		ID: "canonical-action-predicate",
		OnEnter: []Action{{
			Type:      ActionTypeAddTriple,
			Predicate: "workflow.state.next-phase",
		}},
	}
	if err := ValidateDefinition(def); err != nil {
		t.Fatalf("ValidateDefinition rejected canonical predicate: %v", err)
	}
}

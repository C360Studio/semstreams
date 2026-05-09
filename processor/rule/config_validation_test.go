package rule

import (
	"log/slog"
	"strings"
	"testing"

	"github.com/c360studio/semstreams/natsclient"
	"github.com/c360studio/semstreams/pkg/errs"
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
							"field":    opaqueField,
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
							"field":    structuralField,
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

	t.Run("accepts condition on unregistered field (opacity is opt-in)", func(t *testing.T) {
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
		if err != nil {
			t.Errorf("unregistered fields must default to rule-matchable: %v", err)
		}
	})
}

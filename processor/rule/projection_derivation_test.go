package rule

import (
	"encoding/json"
	"reflect"
	"strings"
	"testing"

	"github.com/c360studio/semstreams/pkg/projection"
	"github.com/c360studio/semstreams/vocabulary"
	"github.com/stretchr/testify/require"
)

func TestDeriveEffectiveProjectionContractsScansEveryActionCollectionDeterministically(t *testing.T) {
	t.Parallel()
	vocabulary.Register("test.derive.alpha")
	vocabulary.Register("test.derive.beta")
	vocabulary.Register("test.derive.gamma")
	vocabulary.Register("test.derive.delta")
	vocabulary.Register("test.derive.epsilon")
	definitions := []Definition{
		{
			ID:      "disabled",
			Enabled: false,
			Entity:  EntityConfig{Pattern: "acme.ops.test.system.record.*"},
			OnEnter: []Action{
				{
					Type: ActionTypeReconcilePredicates, ProjectionContract: "zeta",
					ProjectionGroup: "state", Predicate: "test.derive.gamma",
				},
				{Type: ActionTypeAddTriple, Predicate: "test.derive.alpha"},
			},
			OnExit: []Action{{
				Type: ActionTypeReconcilePredicates, ProjectionContract: "alpha",
				ProjectionGroup: "status", Predicate: "test.derive.beta",
			}},
			WhileTrue: []Action{{
				Type: ActionTypeReconcilePredicates, ProjectionContract: "alpha",
				ProjectionGroup: "status", Predicate: "test.derive.alpha",
			}},
			OnRecovery: []Action{{
				Type: ActionTypeReconcilePredicates, ProjectionContract: "alpha",
				ProjectionGroup: "status", Predicate: "test.derive.delta",
			}},
		},
		{
			ID:      "cron",
			Type:    CronRuleType,
			Enabled: true,
			Actions: []Action{{
				Type:               ActionTypeReconcilePredicates,
				ProjectionContract: "cron",
				ProjectionGroup:    "status",
				Predicate:          "test.derive.epsilon",
				Subject:            "acme.ops.test.system.record.literal",
			}},
		},
	}

	got, err := deriveEffectiveProjectionContracts(definitions, nil)
	require.NoError(t, err)
	require.Equal(t, []projection.Contract{
		{
			Name:          "alpha",
			EntityPattern: "acme.ops.test.system.record.*",
			Groups: []projection.PredicateGroup{{
				Name: "status", Mode: projection.ModeReconcile,
				Predicates: []string{
					"test.derive.alpha",
					"test.derive.beta",
					"test.derive.delta",
				},
			}},
		},
		{
			Name:          "cron",
			EntityPattern: "acme.ops.test.system.record.literal",
			Groups: []projection.PredicateGroup{{
				Name: "status", Mode: projection.ModeReconcile,
				Predicates: []string{"test.derive.epsilon"},
			}},
		},
		{
			Name:          "zeta",
			EntityPattern: "acme.ops.test.system.record.*",
			Groups: []projection.PredicateGroup{{
				Name: "state", Mode: projection.ModeReconcile,
				Predicates: []string{"test.derive.gamma"},
			}},
		},
	}, got)
}

func TestDeriveEffectiveProjectionContractsInfersOnlyStaticSubjects(t *testing.T) {
	t.Parallel()
	vocabulary.Register("test.derive.subject")

	base := Definition{
		ID:      "subject",
		Enabled: true,
		Entity:  EntityConfig{Pattern: "acme.ops.test.system.record.*"},
	}
	tests := []struct {
		name        string
		subject     string
		clearEntity bool
		wantPattern string
		wantErr     string
	}{
		{name: "omitted uses entity pattern", wantPattern: base.Entity.Pattern},
		{name: "entity id uses entity pattern", subject: "$entity.id", wantPattern: base.Entity.Pattern},
		{
			name: "literal uses exact pattern", subject: "acme.ops.test.system.record.literal",
			wantPattern: "acme.ops.test.system.record.literal",
		},
		{name: "message path omission unresolved", clearEntity: true, wantErr: "requires an explicit projection_contracts envelope"},
		{name: "entity triple unresolved", subject: "$entity.triple.parent_id", wantErr: "requires an explicit projection_contracts envelope"}, // predicate-audit:invalid {"kind":"stored-predicate","value":"parent_id","reason":"arity"}
		{name: "message unresolved", subject: "$message.entity_id", wantErr: "requires an explicit projection_contracts envelope"},
		{name: "related unresolved", subject: "$related.id", wantErr: "requires an explicit projection_contracts envelope"},
		{name: "iteration unresolved", subject: "$item.entity_id", wantErr: "requires an explicit projection_contracts envelope"},
		{name: "mixed unresolved", subject: "acme.ops.$message.system.record.1", wantErr: "requires an explicit projection_contracts envelope"},
		{name: "malformed static", subject: "acme.ops.too.short", wantErr: "must be a canonical literal entity ID"},
		{name: "wildcard static", subject: "acme.ops.test.system.record.*", wantErr: "must be a canonical literal entity ID"},
	}

	for _, test := range tests {
		test := test
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			definition := base
			if test.clearEntity {
				definition.Entity = EntityConfig{}
			}
			definition.OnEnter = []Action{{
				Type:               ActionTypeReconcilePredicates,
				ProjectionContract: "subject-contract",
				ProjectionGroup:    "state",
				Predicate:          "test.derive.subject",
				Subject:            test.subject,
			}}
			got, err := deriveEffectiveProjectionContracts([]Definition{definition}, nil)
			if test.wantErr != "" {
				require.ErrorContains(t, err, test.wantErr)
				require.Empty(t, got)
				return
			}
			require.NoError(t, err)
			require.Equal(t, test.wantPattern, got[0].EntityPattern)
		})
	}
}

func TestDeriveEffectiveProjectionContractsNeverWidensConflictingPatterns(t *testing.T) {
	t.Parallel()
	vocabulary.Register("test.derive.conflict")
	action := Action{
		Type:               ActionTypeReconcilePredicates,
		ProjectionContract: "conflict",
		ProjectionGroup:    "state",
		Predicate:          "test.derive.conflict",
	}
	definitions := []Definition{
		{ID: "alpha-rule", Entity: EntityConfig{Pattern: "acme.ops.test.alpha.record.*"}, OnEnter: []Action{action}},
		{ID: "zeta-rule", Entity: EntityConfig{Pattern: "acme.ops.test.zeta.record.*"}, OnExit: []Action{action}},
	}

	got, err := deriveEffectiveProjectionContracts(definitions, nil)
	require.Error(t, err)
	require.Empty(t, got)
	require.ErrorContains(t, err, "conflicting target patterns")
	require.ErrorContains(t, err, "alpha-rule on_enter[0]")
	require.ErrorContains(t, err, "zeta-rule on_exit[0]")
	require.NotContains(t, err.Error(), "acme.ops.test.*.record.*")

	declared := []projection.Contract{{
		Name: "conflict", EntityPattern: "acme.ops.test.*.record.*",
		Groups: []projection.PredicateGroup{{
			Name: "state", Mode: projection.ModeReconcile,
			Predicates: []string{"test.derive.conflict"},
		}},
	}}
	got, err = deriveEffectiveProjectionContracts(definitions, declared)
	require.NoError(t, err)
	require.Equal(t, declared, got)
}

func TestDeriveEffectiveProjectionContractsValidatesDeclaredStructuralSuperset(t *testing.T) {
	t.Parallel()
	vocabulary.Register("test.derive.used")
	vocabulary.Register("test.derive.reserved")
	vocabulary.Register("test.derive.birth")

	definition := Definition{
		ID:     "override",
		Entity: EntityConfig{Pattern: "acme.ops.test.system.record.*"},
		OnEnter: []Action{{
			Type:               ActionTypeReconcilePredicates,
			ProjectionContract: "override",
			ProjectionGroup:    "state",
			Predicate:          "test.derive.used",
		}},
	}
	superset := []projection.Contract{{
		Name:            "override",
		MessageType:     "test.override.v1",
		EntityPattern:   "acme.*.test.system.record.*",
		BirthPredicates: []string{"test.derive.birth"},
		IndexingProfile: "control",
		Groups: []projection.PredicateGroup{{
			Name: "state", Mode: projection.ModeReconcile,
			Predicates: []string{"test.derive.reserved", "test.derive.used"},
		}},
	}}

	got, err := deriveEffectiveProjectionContracts([]Definition{definition}, superset)
	require.NoError(t, err)
	require.Equal(t, superset, got)

	tests := []struct {
		name    string
		mutate  func([]projection.Contract) []projection.Contract
		wantErr string
	}{
		{
			name: "explicit empty", mutate: func([]projection.Contract) []projection.Contract {
				return []projection.Contract{}
			}, wantErr: "missing derived contract",
		},
		{
			name: "missing predicate", mutate: func(in []projection.Contract) []projection.Contract {
				in[0].Groups[0].Predicates = []string{"test.derive.reserved"}
				return in
			}, wantErr: "does not cover predicate",
		},
		{
			name: "different mode", mutate: func(in []projection.Contract) []projection.Contract {
				in[0].Groups[0].Mode = projection.ModeAppend
				return in
			}, wantErr: "uses mode",
		},
		{
			name: "narrower pattern", mutate: func(in []projection.Contract) []projection.Contract {
				in[0].EntityPattern = "acme.ops.test.system.record.literal"
				return in
			}, wantErr: "does not contain derived target pattern",
		},
		{
			name: "duplicate contract", mutate: func(in []projection.Contract) []projection.Contract {
				return append(in, in[0])
			}, wantErr: "duplicate projection contract",
		},
	}
	for _, test := range tests {
		test := test
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			declared := cloneProjectionContracts(superset)
			declared = test.mutate(declared)
			_, err := deriveEffectiveProjectionContracts([]Definition{definition}, declared)
			require.ErrorContains(t, err, test.wantErr)
		})
	}
}

func TestDeriveEffectiveProjectionContractsDynamicSubjectNeedsCoveringOverride(t *testing.T) {
	t.Parallel()
	vocabulary.Register("test.derive.dynamic")
	vocabulary.Register("test.derive.other")
	definition := Definition{
		ID: "dynamic",
		OnEnter: []Action{{
			Type:               ActionTypeReconcilePredicates,
			ProjectionContract: "dynamic",
			ProjectionGroup:    "state",
			Predicate:          "test.derive.dynamic",
			Subject:            "$entity.triple.parent_id", // predicate-audit:invalid {"kind":"stored-predicate","value":"parent_id","reason":"arity"}
		}},
	}
	declared := []projection.Contract{{
		Name: "dynamic", EntityPattern: "acme.ops.test.system.record.*",
		Groups: []projection.PredicateGroup{{
			Name: "state", Mode: projection.ModeReconcile,
			Predicates: []string{"test.derive.dynamic"},
		}},
	}}

	got, err := deriveEffectiveProjectionContracts([]Definition{definition}, declared)
	require.NoError(t, err)
	require.Equal(t, declared, got)
	declared[0].Groups[0].Predicates = []string{"test.derive.other"}
	_, err = deriveEffectiveProjectionContracts([]Definition{definition}, declared)
	require.ErrorContains(t, err, "does not cover predicate")

	definition.OnEnter[0].Subject = "acme.ops.test.system.record.*"
	_, err = deriveEffectiveProjectionContracts([]Definition{definition}, declared)
	require.ErrorContains(t, err, "must be a canonical literal entity ID")
}

func TestConfigProjectionContractsPresenceRoundTrip(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name      string
		input     string
		wantField bool
		wantNil   bool
	}{
		{name: "omitted", input: `{"pack_id":"presence"}`, wantNil: true},
		{name: "explicit empty", input: `{"pack_id":"presence","projection_contracts":[]}`, wantField: true},
	}
	for _, test := range tests {
		test := test
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			var config Config
			require.NoError(t, json.Unmarshal([]byte(test.input), &config))
			require.Equal(t, test.wantNil, config.ProjectionContracts == nil)
			encoded, err := json.Marshal(config)
			require.NoError(t, err)
			var object map[string]json.RawMessage
			require.NoError(t, json.Unmarshal(encoded, &object))
			_, present := object["projection_contracts"]
			require.Equal(t, test.wantField, present, string(encoded))
		})
	}
}

func TestProcessorProjectionBindingsExposeEffectiveImmutableSnapshotOnlyAfterPreflight(t *testing.T) {
	t.Parallel()
	vocabulary.Register("test.derive.snapshot")
	config := mustTestConfig(t, "derived-snapshot")
	config.InlineRules = []Definition{{
		ID: "snapshot", Entity: EntityConfig{Pattern: "acme.ops.test.system.record.*"},
		OnEnter: []Action{{
			Type:               ActionTypeReconcilePredicates,
			ProjectionContract: "snapshot",
			ProjectionGroup:    "state",
			Predicate:          "test.derive.snapshot",
		}},
	}}
	processor, err := NewProcessor(nil, &config)
	require.NoError(t, err)

	_, before := processor.ProjectionBindings()
	require.Nil(t, before)
	require.NoError(t, processor.PreflightProjectionMutations())
	_, first := processor.ProjectionBindings()
	require.Len(t, first, 1)
	require.Nil(t, processor.config.ProjectionContracts)

	first[0].Groups[0].Predicates[0] = "mutated.outside.snapshot"
	_, second := processor.ProjectionBindings()
	require.False(t, reflect.DeepEqual(first, second))
	require.Equal(t, "test.derive.snapshot", second[0].Groups[0].Predicates[0])
}

func TestHotReloadUsesFrozenDerivedOrDeclaredEnvelopeWithoutRebinding(t *testing.T) {
	t.Parallel()
	vocabulary.Register("test.derive.boot")
	vocabulary.Register("test.derive.reserved")
	initial := Definition{
		ID:     "initial",
		Type:   "test_rule",
		Entity: EntityConfig{Pattern: "acme.ops.test.system.record.*"},
		OnEnter: []Action{{
			Type:               ActionTypeReconcilePredicates,
			ProjectionContract: "hot-reload",
			ProjectionGroup:    "state",
			Predicate:          "test.derive.boot",
		}},
	}
	hotRule := map[string]any{
		"type": "test_rule",
		"on_enter": []any{map[string]any{
			"type": ActionTypeReconcilePredicates, "projection_contract": "hot-reload",
			"projection_group": "state", "predicate": "test.derive.reserved",
		}},
	}

	minimalConfig := mustTestConfig(t, "minimal-hot-reload")
	minimalConfig.InlineRules = []Definition{initial}
	minimal, err := NewProcessor(nil, &minimalConfig)
	require.NoError(t, err)
	require.NoError(t, minimal.PreflightProjectionMutations())
	_, minimalBefore := minimal.ProjectionBindings()
	err = minimal.ValidateConfigUpdate(map[string]any{"rules": map[string]any{"hot": hotRule}})
	require.ErrorContains(t, err, "outside projection contract")
	_, minimalAfter := minimal.ProjectionBindings()
	require.Equal(t, minimalBefore, minimalAfter)
	require.False(t, minimal.reconcilerConfigured)

	declaredConfig := mustTestConfig(t, "declared-hot-reload")
	declaredConfig.InlineRules = []Definition{initial}
	declaredConfig.ProjectionContracts = []projection.Contract{{
		Name: "hot-reload", EntityPattern: "acme.*.test.system.record.*",
		Groups: []projection.PredicateGroup{{
			Name: "state", Mode: projection.ModeReconcile,
			Predicates: []string{"test.derive.boot", "test.derive.reserved"},
		}},
	}}
	declared, err := NewProcessor(nil, &declaredConfig)
	require.NoError(t, err)
	require.NoError(t, declared.PreflightProjectionMutations())
	_, declaredBefore := declared.ProjectionBindings()
	require.NoError(t, declared.ValidateConfigUpdate(map[string]any{
		"rules": map[string]any{"hot": hotRule},
	}))
	_, declaredAfter := declared.ProjectionBindings()
	require.Equal(t, declaredBefore, declaredAfter)
	require.False(t, declared.reconcilerConfigured)
}

func TestOmittedProjectionContractsRemainAuthoredOmissionAfterPreflight(t *testing.T) {
	t.Parallel()
	vocabulary.Register("test.derive.roundtrip")
	config := mustTestConfig(t, "omitted-roundtrip")
	config.InlineRules = []Definition{{
		ID: "roundtrip", Entity: EntityConfig{Pattern: "acme.ops.test.system.record.*"},
		OnEnter: []Action{{
			Type:               ActionTypeReconcilePredicates,
			ProjectionContract: "roundtrip",
			ProjectionGroup:    "state",
			Predicate:          "test.derive.roundtrip",
		}},
	}}
	processor, err := NewProcessor(nil, &config)
	require.NoError(t, err)
	before, err := json.Marshal(processor.config)
	require.NoError(t, err)
	require.NotContains(t, string(before), "projection_contracts")

	require.NoError(t, processor.PreflightProjectionMutations())
	after, err := json.Marshal(processor.config)
	require.NoError(t, err)
	require.JSONEq(t, string(before), string(after))
	_, effective := processor.ProjectionBindings()
	require.Len(t, effective, 1)
}

func TestPatternContainsPattern(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name     string
		declared string
		derived  string
		want     bool
	}{
		{name: "literal equality", declared: "a.b.c.d.e.f", derived: "a.b.c.d.e.f", want: true},
		{name: "wildcard contains literal", declared: "a.*.c.d.e.*", derived: "a.b.c.d.e.f", want: true},
		{name: "wildcard contains wildcard", declared: "a.*.c.d.e.*", derived: "a.*.c.d.e.*", want: true},
		{name: "literal cannot contain wildcard", declared: "a.b.c.d.e.f", derived: "a.*.c.d.e.*"},
		{name: "different literal", declared: "a.b.c.d.e.f", derived: "a.x.c.d.e.f"},
	}
	for _, test := range tests {
		test := test
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			got, err := projectionPatternContains(test.declared, test.derived)
			require.NoError(t, err)
			require.Equal(t, test.want, got)
		})
	}
}

func TestDerivationDiagnosticsAreStable(t *testing.T) {
	t.Parallel()
	vocabulary.Register("test.derive.diagnostic")
	action := Action{
		Type:               ActionTypeReconcilePredicates,
		ProjectionContract: "diagnostic",
		ProjectionGroup:    "state",
		Predicate:          "test.derive.diagnostic",
		Subject:            "$message.target",
	}
	definitions := []Definition{
		{ID: "zeta", OnExit: []Action{action}},
		{ID: "alpha", OnEnter: []Action{action}},
	}
	_, first := deriveEffectiveProjectionContracts(definitions, nil)
	_, second := deriveEffectiveProjectionContracts(definitions, nil)
	require.EqualError(t, second, first.Error())
	require.True(t,
		strings.Index(first.Error(), "alpha on_enter[0]") < strings.Index(first.Error(), "zeta on_exit[0]"),
		first.Error(),
	)
}

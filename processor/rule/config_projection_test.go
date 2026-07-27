package rule

import (
	"encoding/json"
	"reflect"
	"strings"
	"testing"

	"github.com/c360studio/semstreams/pkg/ownership"
	"github.com/c360studio/semstreams/pkg/projection"
)

// TestConfigPackIDProjectionContractsRoundTrip locks the operator-reachable
// pack_id + projection_contracts fields through the Config custom
// Marshal/Unmarshal alias path. ADR-056 #278 inc 2 +
// [[feedback_polymorphic_config_needs_json_roundtrip_test]]: every
// operator-reachable field must survive a JSON round trip with no shadow
// struct silently dropping it.
func TestConfigPackIDProjectionContractsRoundTrip(t *testing.T) {
	t.Parallel()

	original := Config{
		PackID: "drone-ops-v1",
		ProjectionContracts: []projection.Contract{
			{
				Name:          "drone.status",
				MessageType:   "telemetry.robotics.drone-status.v1",
				EntityPattern: "acme.ops.robotics.gcs.drone.*",
				Groups: []projection.PredicateGroup{
					{
						Name:       "status",
						Mode:       ownership.ModeReplaceOwned,
						Predicates: []string{"drone.status.state", "drone.status.battery"},
					},
					{
						Name:       "notes",
						Mode:       ownership.ModeAppendEvidence,
						Predicates: []string{"drone.status.note"},
					},
				},
				BirthPredicates: []string{"drone.identity.serial", "drone.identity.model"},
				ForeignEdges: []projection.ForeignEdge{
					{
						Predicate:     "test.drone.assigned-to",
						Mode:          ownership.EdgeConditional,
						TargetPattern: "acme.ops.robotics.gcs.mission.*",
					},
				},
				IndexingProfile: "signal",
			},
		},
		InlineRules: []Definition{{
			ID:      "retire-drone",
			Type:    "test_rule",
			Name:    "retire drone",
			Enabled: true,
			OnEnter: []Action{{
				Type:               ActionTypeReplaceOwned,
				ProjectionContract: "drone.status",
				ProjectionGroup:    "status",
				Predicate:          "drone.status.state",
				Object:             "retired",
			}},
		}},
	}

	data, err := json.Marshal(original)
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}

	// pack_id must be present in the wire form (it rides the alias).
	if !strings.Contains(string(data), `"pack_id":"drone-ops-v1"`) {
		t.Errorf("marshaled JSON missing pack_id: %s", data)
	}

	var restored Config
	if err := json.Unmarshal(data, &restored); err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}

	if restored.PackID != original.PackID {
		t.Errorf("PackID round-trip: got %q want %q", restored.PackID, original.PackID)
	}
	if !reflect.DeepEqual(restored.ProjectionContracts, original.ProjectionContracts) {
		t.Errorf("ProjectionContracts round-trip mismatch:\n got %#v\nwant %#v",
			restored.ProjectionContracts, original.ProjectionContracts)
	}
	if !reflect.DeepEqual(restored.InlineRules, original.InlineRules) {
		t.Errorf("InlineRules selector round-trip mismatch:\n got %#v\nwant %#v",
			restored.InlineRules, original.InlineRules)
	}
}

func TestConfigUnmarshalPreservesDefaultsWhileOmissionSelectsDerivation(t *testing.T) {
	t.Parallel()

	config, err := NewConfig("before-overlay")
	if err != nil {
		t.Fatalf("NewConfig: %v", err)
	}
	config.ProjectionContracts = []projection.Contract{{
		Name:          "old-explicit",
		EntityPattern: "acme.ops.test.system.record.*",
		Groups: []projection.PredicateGroup{{
			Name:       "state",
			Mode:       ownership.ModeReplaceOwned,
			Predicates: []string{"test.config.old"},
		}},
	}}

	if err := json.Unmarshal([]byte(`{"pack_id":"after-overlay"}`), &config); err != nil {
		t.Fatalf("Unmarshal partial config: %v", err)
	}
	if config.PackID != "after-overlay" {
		t.Fatalf("PackID = %q, want after-overlay", config.PackID)
	}
	if config.Ports == nil {
		t.Fatal("partial decode erased default ports")
	}
	if config.BufferWindowSize != "10m" {
		t.Fatalf("BufferWindowSize = %q, want preserved default 10m", config.BufferWindowSize)
	}
	if config.AlertCooldownPeriod != "2m" {
		t.Fatalf("AlertCooldownPeriod = %q, want preserved default 2m", config.AlertCooldownPeriod)
	}
	if !config.MessageCache.Enabled {
		t.Fatal("partial decode erased enabled default message cache")
	}
	if config.ProjectionContracts != nil {
		t.Fatalf("omitted projection_contracts = %#v, want nil derivation selection",
			config.ProjectionContracts)
	}
}

func TestConfigUnmarshalProjectionContractsPresenceAndAtomicErrors(t *testing.T) {
	t.Parallel()

	t.Run("explicit empty remains non-nil", func(t *testing.T) {
		config, err := NewConfig("explicit-empty")
		if err != nil {
			t.Fatalf("NewConfig: %v", err)
		}
		if err := json.Unmarshal(
			[]byte(`{"projection_contracts":[]}`),
			&config,
		); err != nil {
			t.Fatalf("Unmarshal explicit empty: %v", err)
		}
		if config.ProjectionContracts == nil || len(config.ProjectionContracts) != 0 {
			t.Fatalf("ProjectionContracts = %#v, want explicit non-nil empty slice",
				config.ProjectionContracts)
		}
	})

	tests := []struct {
		name  string
		input string
	}{
		{name: "type error", input: `{"pack_id":123}`},
		{name: "null contracts", input: `{"projection_contracts":null}`},
	}
	for _, test := range tests {
		test := test
		t.Run(test.name, func(t *testing.T) {
			config, err := NewConfig("atomic-error")
			if err != nil {
				t.Fatalf("NewConfig: %v", err)
			}
			config.ProjectionContracts = []projection.Contract{{
				Name:          "keep",
				EntityPattern: "acme.ops.test.system.record.*",
				Groups: []projection.PredicateGroup{{
					Name:       "state",
					Mode:       ownership.ModeReplaceOwned,
					Predicates: []string{"test.config.keep"},
				}},
			}}
			before := config
			before.ProjectionContracts = cloneProjectionContracts(config.ProjectionContracts)

			err = json.Unmarshal([]byte(test.input), &config)
			if err == nil {
				t.Fatalf("Unmarshal(%s) succeeded, want error", test.input)
			}
			if !reflect.DeepEqual(config, before) {
				t.Fatalf("receiver mutated on decode error:\n got %#v\nwant %#v", config, before)
			}
		})
	}
}

func TestRuleProcessorSchemaExposesReplaceOwnedSelectors(t *testing.T) {
	t.Parallel()

	inlineRules := schema.Properties["inline_rules"]
	if inlineRules.Items == nil {
		t.Fatal("inline_rules schema has no item shape")
	}
	onEnter := inlineRules.Items.Properties["on_enter"]
	if onEnter.Items == nil {
		t.Fatal("inline_rules.on_enter schema has no action item shape")
	}
	for _, selector := range []string{"projection_contract", "projection_group"} {
		property, ok := onEnter.Items.Properties[selector]
		if !ok {
			t.Fatalf("replace_owned selector %q absent from action schema", selector)
		}
		if property.Type != "string" {
			t.Fatalf("%s schema type = %q, want string", selector, property.Type)
		}
	}
	contracts := schema.Properties["projection_contracts"]
	for _, phrase := range []string{
		"omit to derive minimal",
		"explicit projection authorization superset",
		"explicit-only",
	} {
		if !strings.Contains(contracts.Description, phrase) {
			t.Fatalf("projection_contracts schema description %q does not contain %q",
				contracts.Description, phrase)
		}
	}
}

// TestConfigPackIDValidation pins the literal-token guard on pack_id so the
// derived owner key "rule-pack.<pack_id>" always has exactly two positions.
func TestConfigPackIDValidation(t *testing.T) {
	t.Parallel()

	t.Run("valid pack_id passes", func(t *testing.T) {
		t.Parallel()
		cfg := Config{PackID: "my-pack_v1"}
		if err := cfg.Validate(); err != nil {
			t.Errorf("valid pack_id rejected: %v", err)
		}
	})

	t.Run("empty pack_id is always rejected", func(t *testing.T) {
		t.Parallel()
		cfg := Config{PackID: ""}
		if err := cfg.Validate(); err == nil || !strings.Contains(err.Error(), "pack_id is required") {
			t.Fatalf("empty pack_id error = %v, want required error", err)
		}
	})

	t.Run("every config requires explicit pack_id", func(t *testing.T) {
		t.Parallel()
		cfg := Config{EnableGraphIntegration: true}
		if err := cfg.Validate(); err == nil || !strings.Contains(err.Error(), "pack_id is required") {
			t.Fatalf("graph-integrated empty pack_id error = %v, want required error", err)
		}
	})

	t.Run("invalid pack_id rejected with charset + owner id in message", func(t *testing.T) {
		t.Parallel()
		cfg := Config{PackID: "my:pack"}
		err := cfg.Validate()
		if err == nil {
			t.Fatal("invalid pack_id my:pack must be rejected")
		}
		msg := err.Error()
		if !strings.Contains(msg, "rule-pack.my:pack") {
			t.Errorf("error must name the derived owner id rule-pack.my:pack, got: %s", msg)
		}
		if !strings.Contains(msg, "[A-Za-z0-9_=-]") {
			t.Errorf("error must name the allowed charset, got: %s", msg)
		}
	})

	t.Run("dot separator is rejected", func(t *testing.T) {
		t.Parallel()
		err := (Config{PackID: "my-pack.v1"}).Validate()
		if err == nil || !strings.Contains(err.Error(), "one literal KV token") {
			t.Fatalf("dotted pack_id error = %v, want literal-token error", err)
		}
	})

	for _, test := range []struct {
		name    string
		bytes   int
		wantErr bool
	}{
		{"245 bytes", 245, false},
		{"246 bytes", 246, false},
		{"247 bytes", 247, true},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			cfg := Config{PackID: strings.Repeat("p", test.bytes), EnableGraphIntegration: true}
			err := cfg.Validate()
			if test.wantErr && err == nil {
				t.Fatalf("%d-byte pack_id accepted, want error", test.bytes)
			}
			if !test.wantErr && err != nil {
				t.Fatalf("%d-byte pack_id rejected: %v", test.bytes, err)
			}
		})
	}
}

func TestNewConfigRequiresExplicitPackID(t *testing.T) {
	t.Parallel()

	if _, err := NewConfig(""); err == nil || !strings.Contains(err.Error(), "pack_id is required") {
		t.Fatalf("NewConfig empty error = %v, want required error", err)
	}
	cfg, err := NewConfig("config-constructor-test")
	if err != nil {
		t.Fatalf("NewConfig: %v", err)
	}
	if cfg.PackID != "config-constructor-test" {
		t.Fatalf("PackID = %q, want explicit identity", cfg.PackID)
	}
	if err := cfg.Validate(); err != nil {
		t.Fatalf("constructed config invalid: %v", err)
	}
}

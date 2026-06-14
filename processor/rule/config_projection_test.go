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
		PackID: "drone-ops.v1",
		ProjectionContracts: []projection.Contract{
			{
				Name:          "drone.status",
				MessageType:   "telemetry.robotics.drone-status.v1",
				EntityPattern: "acme.ops.robotics.gcs.drone.*",
				Groups: []projection.PredicateGroup{
					{
						Mode:       ownership.ModeReplaceOwned,
						Predicates: []string{"drone.status.state", "drone.status.battery"},
					},
					{
						Mode:       ownership.ModeAppendEvidence,
						Predicates: []string{"drone.status.note"},
					},
				},
				ForeignEdges: []projection.ForeignEdge{
					{
						Predicate:     "drone.assigned_to",
						Mode:          ownership.EdgeConditional,
						TargetPattern: "acme.ops.robotics.gcs.mission.*",
					},
				},
				IndexingProfile: "signal",
			},
		},
	}

	data, err := json.Marshal(original)
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}

	// pack_id must be present in the wire form (it rides the alias).
	if !strings.Contains(string(data), `"pack_id":"drone-ops.v1"`) {
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
}

// TestConfigPackIDValidation pins the subject-safe charset guard on pack_id so
// the derived owner id "rule-pack.<pack_id>" can never be rejected later by
// ownership.RegisterOwner on charset.
func TestConfigPackIDValidation(t *testing.T) {
	t.Parallel()

	t.Run("valid pack_id passes", func(t *testing.T) {
		t.Parallel()
		cfg := Config{PackID: "my-pack.v1"}
		if err := cfg.Validate(); err != nil {
			t.Errorf("valid pack_id rejected: %v", err)
		}
	})

	t.Run("empty pack_id is a no-op", func(t *testing.T) {
		t.Parallel()
		cfg := Config{PackID: ""}
		if err := cfg.Validate(); err != nil {
			t.Errorf("empty pack_id rejected: %v", err)
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
		if !strings.Contains(msg, "[A-Za-z0-9._=-]") {
			t.Errorf("error must name the allowed charset, got: %s", msg)
		}
	})
}

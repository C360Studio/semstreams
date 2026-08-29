package iotsensor

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/c360studio/semstreams/component"
)

// retiredAuthorityConfig returns this component's own DefaultConfig with one
// of the authority keys ADR-102 d2 retired added back. Deriving the fixture
// from DefaultConfig keeps the port block exactly what the component itself
// declares, so the retired key is the only defect in the probe.
func retiredAuthorityConfig(t *testing.T, key, value string) json.RawMessage {
	t.Helper()
	encoded, err := json.Marshal(DefaultConfig())
	if err != nil {
		t.Fatalf("marshal default config: %v", err)
	}
	var raw map[string]any
	if err := json.Unmarshal(encoded, &raw); err != nil {
		t.Fatalf("decode default config: %v", err)
	}
	raw[key] = value
	probe, err := json.Marshal(raw)
	if err != nil {
		t.Fatalf("marshal probe config: %v", err)
	}
	return probe
}

// TestDefaultConfigLoadsWithoutRetiredKeys is the control for the probe: the
// unmodified default must load on both entry paths, so a refusal below is
// attributable to the retired key and not to a malformed fixture.
func TestDefaultConfigLoadsWithoutRetiredKeys(t *testing.T) {
	encoded, err := json.Marshal(DefaultConfig())
	if err != nil {
		t.Fatalf("marshal default config: %v", err)
	}
	if _, err := DeclarePorts(encoded, "iot_sensor"); err != nil {
		t.Fatalf("DeclarePorts refused the unmodified default config: %v", err)
	}
	if _, err := NewComponent(encoded, testDependencies()); err != nil {
		t.Fatalf("NewComponent refused the unmodified default config: %v", err)
	}
}

// TestRetiredAuthorityKeysAreRefused pins the rejection act for ADR-102 d2:
// org_id and platform left this component's operator surface, and
// encoding/json silently DROPS a key with no matching struct field. Without
// this probe an operator upgrading past ADR-102 keeps org_id in their config,
// sees no error, and every entity this processor mints silently changes
// authority to platform.id. Both entry paths that read the raw config —
// DeclarePorts (offline composition validation) and NewComponent (boot) —
// must refuse.
func TestRetiredAuthorityKeysAreRefused(t *testing.T) {
	for _, retired := range []struct{ key, value string }{
		{"org_id", "c360"},
		{"platform", "logistics"},
	} {
		t.Run(retired.key, func(t *testing.T) {
			raw := retiredAuthorityConfig(t, retired.key, retired.value)

			_, declareErr := DeclarePorts(raw, "iot_sensor")
			if declareErr == nil {
				t.Fatalf("DeclarePorts accepted retired key %q", retired.key)
			}
			assertNamesADR102(t, declareErr.Error(), retired.key)

			_, newErr := NewComponent(raw, testDependencies())
			if newErr == nil {
				t.Fatalf("NewComponent accepted retired key %q", retired.key)
			}
			assertNamesADR102(t, newErr.Error(), retired.key)
		})
	}
}

// testDependencies supplies the deployment authority the way the composition
// root does — the only source ADR-102 d2 admits.
func testDependencies() component.Dependencies {
	return component.Dependencies{
		Platform: component.PlatformMeta{Org: "c360", Platform: "semstreams-e2e-structural"},
	}
}

// assertNamesADR102 requires the refusal to name the retired key and the
// decision that retired it, so the operator can act without reading source.
func assertNamesADR102(t *testing.T, message, key string) {
	t.Helper()
	for _, want := range []string{key, "ADR-102"} {
		if !strings.Contains(message, want) {
			t.Errorf("refusal %q does not name %q", message, want)
		}
	}
}

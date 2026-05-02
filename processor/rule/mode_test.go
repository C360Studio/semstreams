// Package rule - Tests for the Mode tristate and Definition.Enabled migration.
package rule

import (
	"encoding/json"
	"testing"
)

// TestMode_Constants verifies the canonical string values for each Mode constant.
func TestMode_Constants(t *testing.T) {
	if ModeEnabled != "enabled" {
		t.Errorf("ModeEnabled = %q, want %q", ModeEnabled, "enabled")
	}
	if ModeDisabled != "disabled" {
		t.Errorf("ModeDisabled = %q, want %q", ModeDisabled, "disabled")
	}
	if ModeShadow != "shadow" {
		t.Errorf("ModeShadow = %q, want %q", ModeShadow, "shadow")
	}
}

// TestDefinition_Mode validates Mode() normalisation across all accepted inputs.
func TestDefinition_Mode(t *testing.T) {
	cases := []struct {
		name    string
		enabled Mode
		want    Mode
	}{
		{"string enabled", "enabled", ModeEnabled},
		{"string disabled", "disabled", ModeDisabled},
		{"string shadow", "shadow", ModeShadow},
		{"legacy true", "true", ModeEnabled},
		{"legacy false", "false", ModeDisabled},
		{"empty string", "", ModeDisabled},
		{"unknown string", "UNKNOWN", ModeEnabled}, // forward-compat: unknown → enabled
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			d := Definition{Enabled: tc.enabled}
			if got := d.Mode(); got != tc.want {
				t.Errorf("Mode() = %q, want %q (Enabled=%q)", got, tc.want, tc.enabled)
			}
		})
	}
}

// TestDefinition_IsActive verifies IsActive returns true for enabled and shadow.
func TestDefinition_IsActive(t *testing.T) {
	cases := []struct {
		enabled Mode
		want    bool
	}{
		{"enabled", true},
		{"shadow", true},
		{"disabled", false},
		{"false", false},
		{"", false},
		{"true", true},
	}
	for _, tc := range cases {
		t.Run(string(tc.enabled), func(t *testing.T) {
			d := Definition{Enabled: tc.enabled}
			if got := d.IsActive(); got != tc.want {
				t.Errorf("IsActive() = %v, want %v (Enabled=%q)", got, tc.want, tc.enabled)
			}
		})
	}
}

// TestDefinition_IsShadow verifies IsShadow returns true only for shadow mode.
func TestDefinition_IsShadow(t *testing.T) {
	cases := []struct {
		enabled Mode
		want    bool
	}{
		{"shadow", true},
		{"enabled", false},
		{"disabled", false},
		{"true", false},
		{"false", false},
		{"", false},
	}
	for _, tc := range cases {
		t.Run(string(tc.enabled), func(t *testing.T) {
			d := Definition{Enabled: tc.enabled}
			if got := d.IsShadow(); got != tc.want {
				t.Errorf("IsShadow() = %v, want %v (Enabled=%q)", got, tc.want, tc.enabled)
			}
		})
	}
}

// TestDefinition_UnmarshalJSON_LegacyBool verifies that existing JSON with
// enabled:true/false unmarshals correctly into the new string tristate.
func TestDefinition_UnmarshalJSON_LegacyBool(t *testing.T) {
	t.Run("true maps to ModeEnabled", func(t *testing.T) {
		raw := `{"id":"r1","type":"expression","enabled":true}`
		var d Definition
		if err := json.Unmarshal([]byte(raw), &d); err != nil {
			t.Fatalf("unmarshal error: %v", err)
		}
		if d.Enabled != ModeEnabled {
			t.Errorf("Enabled = %q, want %q", d.Enabled, ModeEnabled)
		}
		if !d.IsActive() {
			t.Error("IsActive() = false, want true")
		}
	})

	t.Run("false maps to ModeDisabled", func(t *testing.T) {
		raw := `{"id":"r1","type":"expression","enabled":false}`
		var d Definition
		if err := json.Unmarshal([]byte(raw), &d); err != nil {
			t.Fatalf("unmarshal error: %v", err)
		}
		if d.Enabled != ModeDisabled {
			t.Errorf("Enabled = %q, want %q", d.Enabled, ModeDisabled)
		}
		if d.IsActive() {
			t.Error("IsActive() = true, want false")
		}
	})
}

// TestDefinition_UnmarshalJSON_StringTristate verifies the new string values
// round-trip correctly through json.Unmarshal.
func TestDefinition_UnmarshalJSON_StringTristate(t *testing.T) {
	cases := []struct {
		jsonVal  string
		wantMode Mode
	}{
		{`"enabled"`, ModeEnabled},
		{`"disabled"`, ModeDisabled},
		{`"shadow"`, ModeShadow},
	}
	for _, tc := range cases {
		t.Run(string(tc.wantMode), func(t *testing.T) {
			raw := `{"id":"r1","type":"expression","enabled":` + tc.jsonVal + `}`
			var d Definition
			if err := json.Unmarshal([]byte(raw), &d); err != nil {
				t.Fatalf("unmarshal error: %v", err)
			}
			if got := d.Mode(); got != tc.wantMode {
				t.Errorf("Mode() = %q, want %q", got, tc.wantMode)
			}
		})
	}
}

// TestDefinition_MarshalJSON verifies that after an unmarshal→marshal round-trip
// the enabled field is emitted as a string (not a bool), confirming the
// canonical wire format for newly-written configs.
func TestDefinition_MarshalJSON_EmitsString(t *testing.T) {
	// Start from a legacy-bool JSON; after unmarshal the field is a string.
	raw := `{"id":"r1","type":"expression","enabled":true}`
	var d Definition
	if err := json.Unmarshal([]byte(raw), &d); err != nil {
		t.Fatalf("unmarshal error: %v", err)
	}
	out, err := json.Marshal(d)
	if err != nil {
		t.Fatalf("marshal error: %v", err)
	}
	// Verify the marshalled form contains "enabled":"enabled" (string, not bool)
	var m map[string]any
	if err := json.Unmarshal(out, &m); err != nil {
		t.Fatalf("re-unmarshal error: %v", err)
	}
	v, ok := m["enabled"]
	if !ok {
		t.Fatal("enabled field missing from marshalled output")
	}
	if _, isStr := v.(string); !isStr {
		t.Errorf("enabled field type = %T, want string; value = %v", v, v)
	}
}

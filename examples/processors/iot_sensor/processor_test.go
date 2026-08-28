package iotsensor

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/c360studio/semstreams/graph"
	"github.com/c360studio/semstreams/message"
	"github.com/c360studio/semstreams/types"
)

// TestProcessor_Process_JSONTransformation verifies that the processor correctly
// transforms incoming JSON into Graphable payloads.
func TestProcessor_Process_JSONTransformation(t *testing.T) {
	tests := []struct {
		name      string
		authority types.PlatformMeta
		inputJSON string
		wantType  string
		wantValue float64
		wantErr   bool
	}{
		{
			name:      "temperature reading",
			authority: testAuthority,
			inputJSON: `{
				"device_id": "sensor-042",
				"type": "temperature",
				"reading": 23.5,
				"unit": "celsius",
				"location": "warehouse-7",
				"timestamp": "2025-11-26T10:30:00Z"
			}`,
			wantType:  "temperature",
			wantValue: 23.5,
			wantErr:   false,
		},
		{
			name:      "humidity reading",
			authority: secondAuthority,
			inputJSON: `{
				"device_id": "hum-001",
				"type": "humidity",
				"reading": 65.0,
				"unit": "percent",
				"location": "office-3",
				"timestamp": "2025-11-26T11:00:00Z"
			}`,
			wantType:  "humidity",
			wantValue: 65.0,
			wantErr:   false,
		},
		{
			name:      "missing device_id",
			authority: testAuthority,
			inputJSON: `{
				"type": "temperature",
				"reading": 23.5,
				"unit": "celsius",
				"location": "warehouse-7",
				"timestamp": "2025-11-26T10:30:00Z"
			}`,
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			p := NewProcessor(tt.authority)

			var input map[string]any
			if err := json.Unmarshal([]byte(tt.inputJSON), &input); err != nil {
				t.Fatalf("failed to unmarshal test input: %v", err)
			}

			result, err := p.Process(input)

			if tt.wantErr {
				if err == nil {
					t.Error("Process() expected error, got nil")
				}
				return
			}

			if err != nil {
				t.Fatalf("Process() unexpected error: %v", err)
			}

			// Verify result implements Graphable (compile-time check)
			var _ graph.Graphable = result

			// Verify EntityID is valid 6-part format
			entityID := result.EntityID()
			if !message.IsValidEntityID(entityID) {
				t.Errorf("EntityID() = %q is not valid 6-part format", entityID)
			}

			// Verify Triples returns meaningful data
			triples := result.Triples()
			if len(triples) < 3 {
				t.Errorf("Triples() returned %d triples, want at least 3", len(triples))
			}

			// Verify the type and value
			if result.SensorType != tt.wantType {
				t.Errorf("SensorType = %q, want %q", result.SensorType, tt.wantType)
			}
			if result.Value != tt.wantValue {
				t.Errorf("Value = %v, want %v", result.Value, tt.wantValue)
			}
		})
	}
}

// TestProcessor_Process_MintsUnderDeploymentAuthority verifies the processor
// mints positions 1-2 from the deployment authority it was constructed with
// and from nothing else (ADR-102 d2).
func TestProcessor_Process_MintsUnderDeploymentAuthority(t *testing.T) {
	authority := types.PlatformMeta{Org: "testorg", Platform: "testdeployment"}

	p := NewProcessor(authority)

	input := map[string]any{
		"device_id": "sensor-001",
		"type":      "pressure",
		"reading":   1013.25,
		"unit":      "hpa",
		"location":  "lab-1",
		"timestamp": time.Now().Format(time.RFC3339),
	}

	sr, err := p.Process(input)
	if err != nil {
		t.Fatalf("Process() unexpected error: %v", err)
	}

	// Both minted identities carry the deployment authority in positions 1-2.
	wantPrefix := "testorg.testdeployment."
	if !strings.HasPrefix(sr.EntityID(), wantPrefix) {
		t.Errorf("EntityID() = %q, want to start with %q", sr.EntityID(), wantPrefix)
	}
	if !strings.HasPrefix(sr.ZoneEntityID, wantPrefix) {
		t.Errorf("ZoneEntityID = %q, want to start with %q", sr.ZoneEntityID, wantPrefix)
	}
}

// TestProcessor_Process_ZoneEntityID verifies processor computes ZoneEntityID correctly.
func TestProcessor_Process_ZoneEntityID(t *testing.T) {
	tests := []struct {
		name       string
		input      map[string]any
		wantZoneID string
	}{
		{
			name: "default zone type (area)",
			input: map[string]any{
				"device_id": "sensor-001",
				"type":      "temperature",
				"reading":   20.0,
				"unit":      "celsius",
				"location":  "warehouse-7",
			},
			wantZoneID: "acme.dep1.zone.facility.area.warehouse-7",
		},
		{
			name: "explicit zone type",
			input: map[string]any{
				"device_id": "sensor-002",
				"type":      "humidity",
				"reading":   50.0,
				"unit":      "percent",
				"location":  "building-a",
				"zone_type": "building",
			},
			wantZoneID: "acme.dep1.zone.facility.building.building-a",
		},
	}

	p := NewProcessor(testAuthority)

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			sr, err := p.Process(tt.input)
			if err != nil {
				t.Fatalf("Process() error: %v", err)
			}
			if sr.ZoneEntityID != tt.wantZoneID {
				t.Errorf("ZoneEntityID = %q, want %q", sr.ZoneEntityID, tt.wantZoneID)
			}
			// Verify ZoneEntityID is valid 6-part format
			if !message.IsValidEntityID(sr.ZoneEntityID) {
				t.Errorf("ZoneEntityID %q is not valid 6-part format", sr.ZoneEntityID)
			}
		})
	}
}

// TestProcessor_Process_HelpfulErrors verifies that error messages suggest correct field names.
func TestProcessor_Process_HelpfulErrors(t *testing.T) {
	p := NewProcessor(testAuthority)

	tests := []struct {
		name           string
		input          map[string]any
		wantErrContain []string // All of these substrings should appear in the error
	}{
		{
			name: "sensor_type instead of type",
			input: map[string]any{
				"device_id":   "sensor-001",
				"sensor_type": "temperature", // Wrong! Should be "type"
				"reading":     23.5,
				"unit":        "celsius",
				"location":    "warehouse-7",
			},
			wantErrContain: []string{"type", "sensor_type", "did you mean"},
		},
		{
			name: "value instead of reading",
			input: map[string]any{
				"device_id": "sensor-001",
				"type":      "temperature",
				"value":     23.5, // Wrong! Should be "reading"
				"unit":      "celsius",
				"location":  "warehouse-7",
			},
			wantErrContain: []string{"reading", "value", "did you mean"},
		},
		{
			name: "zone_id instead of location",
			input: map[string]any{
				"device_id": "sensor-001",
				"type":      "temperature",
				"reading":   23.5,
				"unit":      "celsius",
				"zone_id":   "warehouse-7", // Wrong! Should be "location"
			},
			wantErrContain: []string{"location", "zone_id", "did you mean"},
		},
		{
			name: "completely wrong field shows available fields",
			input: map[string]any{
				"device_id": "sensor-001",
				"foo":       "bar", // Random wrong field
			},
			wantErrContain: []string{"type", "not found", "available fields"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := p.Process(tt.input)
			if err == nil {
				t.Fatal("Process() expected error, got nil")
			}

			errStr := err.Error()
			for _, want := range tt.wantErrContain {
				if !containsString(errStr, want) {
					t.Errorf("error %q should contain %q", errStr, want)
				}
			}
		})
	}
}

func containsString(s, substr string) bool {
	return len(s) >= len(substr) && (s == substr || len(s) > 0 && containsStringHelper(s, substr))
}

func containsStringHelper(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}

// TestParseZoneEntityIDReadsNamedPositions pins the zone reader against the
// canonical order: a zone entity is `org.platform.zone.facility.<zoneType>.<zoneID>`
// (system = zone, domain = facility), read by named field, and the retired
// order resolves to nothing instead of silently mis-reading (inventory W9).
func TestParseZoneEntityIDReadsNamedPositions(t *testing.T) {
	zoneType, zoneID := ParseZoneEntityID(ZoneEntityID(testAuthority, "area", "cold-storage-1"))
	if zoneType != "area" || zoneID != "cold-storage-1" {
		t.Fatalf("ParseZoneEntityID(canonical) = (%q, %q), want (area, cold-storage-1)", zoneType, zoneID)
	}
	if got := ZoneEntityID(testAuthority, "area", "cold-storage-1"); got != "acme.dep1.zone.facility.area.cold-storage-1" {
		t.Fatalf("ZoneEntityID = %q, want acme.dep1.zone.facility.area.cold-storage-1", got)
	}
	for _, bad := range []string{
		"acme.dep1." + "facility.zone" + ".area.cold-storage-1", // retired order (domain before system)
		"acme.dep1.zone.facility.area",                          // five positions
		"acme.dep1.zone.facility.area.cold.1",                   // seven positions
		"",
	} {
		if zoneType, zoneID := ParseZoneEntityID(bad); zoneType != "" || zoneID != "" {
			t.Fatalf("ParseZoneEntityID(%q) = (%q, %q), want empty", bad, zoneType, zoneID)
		}
	}
}

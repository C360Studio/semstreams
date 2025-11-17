package message

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/c360/semstreams/errors"
)

// SamplePayload is a simple test implementation of the Payload interface
type SamplePayload struct {
	ID    string         `json:"id"`
	Value string         `json:"value"`
	Data  map[string]any `json:"data,omitempty"`
	Loc   *LocationData  `json:"location,omitempty"`
	Time  time.Time      `json:"time,omitempty"`
}

type LocationData struct {
	Lat float64 `json:"lat"`
	Lon float64 `json:"lon"`
}

// Schema implements Payload.Schema
func (p *SamplePayload) Schema() Type {
	return Type{
		Domain:   "test",
		Category: "payload",
		Version:  "v1",
	}
}

// Validate implements Payload.Validate
func (p *SamplePayload) Validate() error {
	if p.ID == "" {
		return errors.WrapInvalid(errors.ErrInvalidData, "SamplePayload", "Validate", "ID is required")
	}
	if p.Loc != nil {
		if p.Loc.Lat < -90 || p.Loc.Lat > 90 {
			return errors.WrapInvalid(errors.ErrInvalidData, "SamplePayload", "Validate", "latitude must be between -90 and 90")
		}
		if p.Loc.Lon < -180 || p.Loc.Lon > 180 {
			return errors.WrapInvalid(errors.ErrInvalidData, "SamplePayload", "Validate", "longitude must be between -180 and 180")
		}
	}
	return nil
}

// MarshalJSON implements json.Marshaler
func (p *SamplePayload) MarshalJSON() ([]byte, error) {
	// Use alias to avoid infinite recursion
	type Alias SamplePayload
	return json.Marshal((*Alias)(p))
}

// UnmarshalJSON implements json.Unmarshaler
func (p *SamplePayload) UnmarshalJSON(data []byte) error {
	// Use alias to avoid infinite recursion
	type Alias SamplePayload
	return json.Unmarshal(data, (*Alias)(p))
}

// Implement behavioral interfaces for testing

// Identifiable implementation
func (p *SamplePayload) EntityID() string {
	return p.ID
}

func (p *SamplePayload) EntityType() EntityType {
	return EntityType{Domain: "test", Type: "test_entity"}
}

// Locatable implementation (only if Loc is set)
func (p *SamplePayload) Location() (lat, lon float64) {
	if p.Loc != nil {
		return p.Loc.Lat, p.Loc.Lon
	}
	return 0, 0
}

// Timeable implementation
func (p *SamplePayload) Timestamp() time.Time {
	return p.Time
}

func TestSamplePayloadSchema(t *testing.T) {
	payload := &SamplePayload{
		ID:    "test-123",
		Value: "test value",
	}

	schema := payload.Schema()
	expected := Type{
		Domain:   "test",
		Category: "payload",
		Version:  "v1",
	}

	if !schema.Equal(expected) {
		t.Errorf("Schema() = %v, want %v", schema, expected)
	}
}

func TestSamplePayloadValidate(t *testing.T) {
	tests := []struct {
		name    string
		payload *SamplePayload
		wantErr bool
		errMsg  string
	}{
		{
			name: "valid payload",
			payload: &SamplePayload{
				ID:    "test-123",
				Value: "valid",
			},
			wantErr: false,
		},
		{
			name: "missing ID",
			payload: &SamplePayload{
				Value: "no id",
			},
			wantErr: true,
			errMsg:  "ID is required",
		},
		{
			name: "invalid latitude",
			payload: &SamplePayload{
				ID: "test-123",
				Loc: &LocationData{
					Lat: 91,
					Lon: 0,
				},
			},
			wantErr: true,
			errMsg:  "latitude must be between -90 and 90",
		},
		{
			name: "invalid longitude",
			payload: &SamplePayload{
				ID: "test-123",
				Loc: &LocationData{
					Lat: 0,
					Lon: 181,
				},
			},
			wantErr: true,
			errMsg:  "longitude must be between -180 and 180",
		},
		{
			name: "valid with location",
			payload: &SamplePayload{
				ID: "test-123",
				Loc: &LocationData{
					Lat: 25.5,
					Lon: -80.1,
				},
			},
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.payload.Validate()
			if (err != nil) != tt.wantErr {
				t.Errorf("Validate() error = %v, wantErr %v", err, tt.wantErr)
			}
			if err != nil && tt.errMsg != "" && !strings.Contains(err.Error(), tt.errMsg) {
				t.Errorf("Validate() error = %v, want to contain %v", err.Error(), tt.errMsg)
			}
		})
	}
}

func TestSamplePayloadMarshalUnmarshal(t *testing.T) {
	original := &SamplePayload{
		ID:    "test-456",
		Value: "test data",
		Data: map[string]any{
			"key1": "value1",
			"key2": 42,
		},
		Loc: &LocationData{
			Lat: 40.7128,
			Lon: -74.0060,
		},
		Time: time.Now().UTC().Truncate(time.Second),
	}

	// Marshal to JSON
	data, err := original.MarshalJSON()
	if err != nil {
		t.Fatalf("MarshalJSON() error = %v", err)
	}

	// Unmarshal back
	restored := &SamplePayload{}
	if err := restored.UnmarshalJSON(data); err != nil {
		t.Fatalf("UnmarshalJSON() error = %v", err)
	}

	// Compare fields
	if restored.ID != original.ID {
		t.Errorf("ID mismatch: got %v, want %v", restored.ID, original.ID)
	}
	if restored.Value != original.Value {
		t.Errorf("Value mismatch: got %v, want %v", restored.Value, original.Value)
	}
	if !restored.Time.Equal(original.Time) {
		t.Errorf("Time mismatch: got %v, want %v", restored.Time, original.Time)
	}
	if restored.Loc.Lat != original.Loc.Lat || restored.Loc.Lon != original.Loc.Lon {
		t.Errorf("Location mismatch: got %v, want %v", restored.Loc, original.Loc)
	}
}

func TestSamplePayloadBehavioralInterfaces(t *testing.T) {
	now := time.Now()
	payload := &SamplePayload{
		ID:    "entity-789",
		Value: "behavioral test",
		Loc: &LocationData{
			Lat: 51.5074,
			Lon: -0.1278,
		},
		Time: now,
	}

	// Test Identifiable
	if id := payload.EntityID(); id != "entity-789" {
		t.Errorf("EntityID() = %v, want entity-789", id)
	}
	expectedType := EntityType{Domain: "test", Type: "test_entity"}
	if dt := payload.EntityType(); dt != expectedType {
		t.Errorf("EntityType() = %v, want %v", dt, expectedType)
	}

	// Test Locatable
	lat, lon := payload.Location()
	if lat != 51.5074 || lon != -0.1278 {
		t.Errorf("Location() = %v, %v, want 51.5074, -0.1278", lat, lon)
	}

	// Test Timeable
	if ts := payload.Timestamp(); !ts.Equal(now) {
		t.Errorf("Timestamp() = %v, want %v", ts, now)
	}
}

func TestSamplePayloadInterfaceCompliance(_ *testing.T) {
	// Ensure SamplePayload implements Payload interface
	var _ Payload = (*SamplePayload)(nil)
}

func TestSamplePayloadBinaryFormat(t *testing.T) {
	payload := &SamplePayload{
		ID:    "binary-test",
		Value: "test",
		Data: map[string]any{
			"nested": map[string]any{
				"field": "value",
			},
		},
	}

	data, err := payload.MarshalJSON()
	if err != nil {
		t.Fatalf("MarshalJSON() error = %v", err)
	}

	// Verify it's valid JSON
	if !json.Valid(data) {
		t.Error("MarshalJSON() did not produce valid JSON")
	}

	// Verify it can be unmarshaled as generic JSON
	var generic map[string]any
	if err := json.Unmarshal(data, &generic); err != nil {
		t.Errorf("Failed to unmarshal as generic JSON: %v", err)
	}

	// Check expected fields exist
	if _, ok := generic["id"]; !ok {
		t.Error("JSON missing 'id' field")
	}
	if _, ok := generic["value"]; !ok {
		t.Error("JSON missing 'value' field")
	}
}

func TestSamplePayloadNilLocationHandling(t *testing.T) {
	payload := &SamplePayload{
		ID:    "no-location",
		Value: "test",
		// Location is nil
	}

	// Should not panic
	lat, lon := payload.Location()
	if lat != 0 || lon != 0 {
		t.Errorf("Location() with nil location = %v, %v, want 0, 0", lat, lon)
	}

	// Should validate successfully
	if err := payload.Validate(); err != nil {
		t.Errorf("Validate() with nil location error = %v", err)
	}
}

func TestSamplePayloadDeterministic(t *testing.T) {
	payload := &SamplePayload{
		ID:    "deterministic",
		Value: "same",
	}

	// Multiple marshals should produce identical output
	data1, err1 := payload.MarshalJSON()
	data2, err2 := payload.MarshalJSON()

	if err1 != nil || err2 != nil {
		t.Fatalf("MarshalJSON() errors: %v, %v", err1, err2)
	}

	if !bytes.Equal(data1, data2) {
		t.Error("MarshalJSON() is not deterministic")
	}
}

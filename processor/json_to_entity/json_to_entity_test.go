package jsontoentity

import (
	"testing"

	"github.com/c360/semstreams/message"
)

func TestConvertToEntity(t *testing.T) {
	tests := []struct {
		name        string
		input       map[string]any
		config      Config
		wantID      string
		wantType    string
		wantPropLen int
		wantErr     bool
	}{
		{
			name: "basic conversion",
			input: map[string]any{
				"entity_id":   "sensor-001",
				"entity_type": "device",
				"temperature": 23.5,
				"status":      "active",
			},
			config:      DefaultConfig(),
			wantID:      "sensor-001",
			wantType:    "device",
			wantPropLen: 2, // temperature + status
			wantErr:     false,
		},
		{
			name: "nested data object",
			input: map[string]any{
				"entity_id":   "drone-001",
				"entity_type": "robotics.drone",
				"data": map[string]any{
					"altitude": 100.5,
					"battery":  85,
				},
			},
			config:      DefaultConfig(),
			wantID:      "drone-001",
			wantType:    "robotics.drone",
			wantPropLen: 1, // "data" nested object (preserved as-is)
			wantErr:     false,
		},
		{
			name: "missing entity_id",
			input: map[string]any{
				"entity_type": "device",
				"temperature": 23.5,
			},
			config:  DefaultConfig(),
			wantErr: true,
		},
		{
			name: "missing entity_type",
			input: map[string]any{
				"entity_id":   "sensor-001",
				"temperature": 23.5,
			},
			config:  DefaultConfig(),
			wantErr: true,
		},
		{
			name: "custom field names",
			input: map[string]any{
				"id":          "custom-001",
				"type":        "custom.type",
				"temperature": 23.5,
			},
			config: Config{
				EntityIDField:   "id",
				EntityTypeField: "type",
				EntityClass:     message.ClassObject,
				EntityRole:      message.RolePrimary,
				SourceField:     "test",
			},
			wantID:      "custom-001",
			wantType:    "custom.type",
			wantPropLen: 1, // only temperature
			wantErr:     false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Create processor instance
			p := &Processor{
				config: tt.config,
			}

			// Create GenericJSON payload
			genericJSON := message.NewGenericJSON(tt.input)

			// Convert to entity
			entity, err := p.convertToEntity(genericJSON)

			// Check error expectation
			if (err != nil) != tt.wantErr {
				t.Errorf("convertToEntity() error = %v, wantErr %v", err, tt.wantErr)
				return
			}

			if tt.wantErr {
				return // Don't check other fields if we expected an error
			}

			// Verify entity fields
			if entity.ID != tt.wantID {
				t.Errorf("entity.ID = %v, want %v", entity.ID, tt.wantID)
			}

			if entity.Type != tt.wantType {
				t.Errorf("entity.Type = %v, want %v", entity.Type, tt.wantType)
			}

			if len(entity.Properties) != tt.wantPropLen {
				t.Errorf("len(entity.Properties) = %v, want %v", len(entity.Properties), tt.wantPropLen)
			}

			// Verify Graphable interface implementation
			if entity.EntityID() != tt.wantID {
				t.Errorf("entity.EntityID() = %v, want %v", entity.EntityID(), tt.wantID)
			}

			// Verify triples are generated
			triples := entity.Triples()
			if len(triples) == 0 {
				t.Error("entity.Triples() returned empty slice")
			}

			// Check for rdf:type triple
			foundTypeTriple := false
			for _, triple := range triples {
				if triple.Predicate == "rdf:type" && triple.Object == tt.wantType {
					foundTypeTriple = true
					break
				}
			}
			if !foundTypeTriple {
				t.Error("entity.Triples() missing rdf:type triple")
			}
		})
	}
}

func TestEntityPayloadGraphableInterface(t *testing.T) {
	// Create test entity
	entity := message.NewEntityPayload(
		"test.entity.001",
		"test.type",
		map[string]any{
			"prop1": "value1",
			"prop2": 42,
		},
	)

	// Verify it implements Graphable
	var _ message.Graphable = entity

	// Test EntityID method
	if id := entity.EntityID(); id != "test.entity.001" {
		t.Errorf("EntityID() = %v, want test.entity.001", id)
	}

	// Test Triples method
	triples := entity.Triples()
	if len(triples) < 3 { // At least rdf:type + 2 properties
		t.Errorf("Triples() returned %d triples, want at least 3", len(triples))
	}

	// Verify all triples have the correct subject
	for i, triple := range triples {
		if triple.Subject != "test.entity.001" {
			t.Errorf("Triple[%d].Subject = %v, want test.entity.001", i, triple.Subject)
		}
	}
}

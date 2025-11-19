package graph

import (
	"testing"
	"time"
)

func TestEvent_Validate_Valid(t *testing.T) {
	now := time.Now()
	validMetadata := EventMetadata{
		RuleName:  "test_rule",
		Timestamp: now,
		Source:    "test_component",
		Reason:    "test reason",
		Version:   "1.0.0",
	}

	tests := []struct {
		name  string
		event Event
	}{
		{
			name: "valid entity update event",
			event: Event{
				Type:       EventEntityUpdate,
				EntityID:   "drone_001",
				Properties: map[string]any{"status": "active"},
				Metadata:   validMetadata,
				Confidence: 0.8,
			},
		},
		{
			name: "valid relationship create event",
			event: Event{
				Type:       EventRelationshipCreate,
				EntityID:   "drone_001",
				TargetID:   "battery_001",
				Properties: map[string]any{"edge_type": "POWERED_BY"},
				Metadata:   validMetadata,
				Confidence: 1.0,
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.event.Validate()
			if err != nil {
				t.Errorf("Event.Validate() error = %v, want nil", err)
			}
		})
	}
}

func TestEvent_Validate_Invalid(t *testing.T) {
	now := time.Now()
	validMetadata := EventMetadata{
		RuleName:  "test_rule",
		Timestamp: now,
		Source:    "test_component",
		Reason:    "test reason",
		Version:   "1.0.0",
	}

	tests := []struct {
		name   string
		event  Event
		errMsg string
	}{
		{
			name: "missing event type",
			event: Event{
				EntityID:   "drone_001",
				Properties: map[string]any{},
				Metadata:   validMetadata,
				Confidence: 1.0,
			},
			errMsg: "event type is required",
		},
		{
			name: "missing entity ID",
			event: Event{
				Type:       EventEntityUpdate,
				Properties: map[string]any{},
				Metadata:   validMetadata,
				Confidence: 1.0,
			},
			errMsg: "entity ID is required",
		},
		{
			name: "confidence too low",
			event: Event{
				Type:       EventEntityUpdate,
				EntityID:   "drone_001",
				Properties: map[string]any{},
				Metadata:   validMetadata,
				Confidence: -0.1,
			},
			errMsg: "confidence must be between 0.0 and 1.0",
		},
		{
			name: "confidence too high",
			event: Event{
				Type:       EventEntityUpdate,
				EntityID:   "drone_001",
				Properties: map[string]any{},
				Metadata:   validMetadata,
				Confidence: 1.1,
			},
			errMsg: "confidence must be between 0.0 and 1.0",
		},
		{
			name: "relationship event missing target ID",
			event: Event{
				Type:       EventRelationshipCreate,
				EntityID:   "drone_001",
				Properties: map[string]any{},
				Metadata:   validMetadata,
				Confidence: 1.0,
			},
			errMsg: "target ID is required for relationship events",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.event.Validate()
			if err == nil {
				t.Errorf("Event.Validate() error = nil, want error")
				return
			}
			if !containsString(err.Error(), tt.errMsg) {
				t.Errorf("Event.Validate() error = %v, want error containing %v", err, tt.errMsg)
			}
		})
	}
}

func TestEvent_Validate_Metadata(t *testing.T) {
	now := time.Now()

	tests := []struct {
		name     string
		metadata EventMetadata
		errMsg   string
	}{
		{
			name: "missing rule name",
			metadata: EventMetadata{
				Timestamp: now,
				Source:    "test_component",
				Reason:    "test reason",
			},
			errMsg: "rule name is required in metadata",
		},
		{
			name: "missing timestamp",
			metadata: EventMetadata{
				RuleName: "test_rule",
				Source:   "test_component",
				Reason:   "test reason",
			},
			errMsg: "timestamp is required in metadata",
		},
		{
			name: "missing source",
			metadata: EventMetadata{
				RuleName:  "test_rule",
				Timestamp: now,
				Reason:    "test reason",
			},
			errMsg: "source is required in metadata",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			event := Event{
				Type:       EventEntityUpdate,
				EntityID:   "drone_001",
				Properties: map[string]any{},
				Metadata:   tt.metadata,
				Confidence: 1.0,
			}
			
			err := event.Validate()
			if err == nil {
				t.Errorf("Event.Validate() error = nil, want error")
				return
			}
			if !containsString(err.Error(), tt.errMsg) {
				t.Errorf("Event.Validate() error = %v, want error containing %v", err, tt.errMsg)
			}
		})
	}
}

func TestEvent_Validate_AutoVersion(t *testing.T) {
	now := time.Now()
	event := Event{
		Type:       EventEntityUpdate,
		EntityID:   "drone_001",
		Properties: map[string]any{},
		Metadata: EventMetadata{
			RuleName:  "test_rule",
			Timestamp: now,
			Source:    "test_component",
			Reason:    "test reason",
			// Version not set
		},
		Confidence: 1.0,
	}

	err := event.Validate()
	if err != nil {
		t.Errorf("Event.Validate() error = %v, want nil", err)
	}
	if event.Metadata.Version != "1.0.0" {
		t.Errorf("Expected version to be set to '1.0.0', got '%s'", event.Metadata.Version)
	}
}

func TestEvent_Subject(t *testing.T) {
	tests := []struct {
		name     string
		event    Event
		expected string
	}{
		{
			name: "entity create event",
			event: Event{
				Type: EventEntityCreate,
			},
			expected: "graph.events.entity.create",
		},
		{
			name: "entity update event",
			event: Event{
				Type: EventEntityUpdate,
			},
			expected: "graph.events.entity.update",
		},
		{
			name: "entity delete event",
			event: Event{
				Type: EventEntityDelete,
			},
			expected: "graph.events.entity.delete",
		},
		{
			name: "relationship create event",
			event: Event{
				Type: EventRelationshipCreate,
			},
			expected: "graph.events.relationship.create",
		},
		{
			name: "relationship delete event",
			event: Event{
				Type: EventRelationshipDelete,
			},
			expected: "graph.events.relationship.delete",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := tt.event.Subject()
			if got != tt.expected {
				t.Errorf("Event.Subject() = %v, want %v", got, tt.expected)
			}
		})
	}
}

func TestNewEntityUpdateEvent(t *testing.T) {
	entityID := "drone_001"
	properties := map[string]any{
		"status":  "active",
		"battery": 85.5,
	}
	metadata := EventMetadata{
		RuleName:  "battery_monitor",
		Timestamp: time.Now(),
		Source:    "rule_engine",
		Reason:    "battery level update",
		Version:   "1.0.0",
	}

	event := NewEntityUpdateEvent(entityID, properties, metadata)

	if event.Type != EventEntityUpdate {
		t.Errorf("Expected event type %v, got %v", EventEntityUpdate, event.Type)
	}
	if event.EntityID != entityID {
		t.Errorf("Expected entity ID %v, got %v", entityID, event.EntityID)
	}
	if event.Confidence != 1.0 {
		t.Errorf("Expected confidence 1.0, got %v", event.Confidence)
	}
	if len(event.Properties) != 2 {
		t.Errorf("Expected 2 properties, got %v", len(event.Properties))
	}
}

func TestNewRelationshipCreateEvent(t *testing.T) {
	fromID := "drone_001"
	toID := "battery_001"
	relationshipType := "POWERED_BY"
	metadata := EventMetadata{
		RuleName:  "power_relationship_detector",
		Timestamp: time.Now(),
		Source:    "rule_engine",
		Reason:    "detected power relationship",
		Version:   "1.0.0",
	}

	event := NewRelationshipCreateEvent(fromID, toID, relationshipType, metadata)

	if event.Type != EventRelationshipCreate {
		t.Errorf("Expected event type %v, got %v", EventRelationshipCreate, event.Type)
	}
	if event.EntityID != fromID {
		t.Errorf("Expected entity ID %v, got %v", fromID, event.EntityID)
	}
	if event.TargetID != toID {
		t.Errorf("Expected target ID %v, got %v", toID, event.TargetID)
	}
	if event.Properties["edge_type"] != relationshipType {
		t.Errorf("Expected edge_type %v, got %v", relationshipType, event.Properties["edge_type"])
	}
}

func TestNewAlertEvent(t *testing.T) {
	alertType := "battery_low"
	entityID := "drone_001"
	properties := map[string]any{
		"battery_level": 15.0,
		"threshold":    20.0,
	}
	metadata := EventMetadata{
		RuleName:  "battery_alert_rule",
		Timestamp: time.Now(),
		Source:    "rule_engine",
		Reason:    "battery below threshold",
		Version:   "1.0.0",
	}

	event := NewAlertEvent(alertType, entityID, properties, metadata)

	if event.Type != EventEntityCreate {
		t.Errorf("Expected event type %v, got %v", EventEntityCreate, event.Type)
	}
	if event.Properties["alert_type"] != alertType {
		t.Errorf("Expected alert_type %v, got %v", alertType, event.Properties["alert_type"])
	}
	if event.Properties["source_entity"] != entityID {
		t.Errorf("Expected source_entity %v, got %v", entityID, event.Properties["source_entity"])
	}
	if event.Properties["status"] != StatusWarning {
		t.Errorf("Expected status %v, got %v", StatusWarning, event.Properties["status"])
	}
	if event.Confidence != 0.8 {
		t.Errorf("Expected confidence 0.8, got %v", event.Confidence)
	}

	// Check that the alert ID was generated properly
	if event.EntityID == "" {
		t.Error("Expected non-empty alert entity ID")
	}
	if !containsString(event.EntityID, "alert_") {
		t.Errorf("Expected alert entity ID to contain 'alert_', got %v", event.EntityID)
	}
}

func TestNewEntityCreateEvent(t *testing.T) {
	entityID := "sensor_001"
	entityType := "sensor:Temperature"
	properties := map[string]any{
		"location": "engine_room",
		"unit":     "celsius",
	}
	metadata := EventMetadata{
		RuleName:  "sensor_discovery",
		Timestamp: time.Now(),
		Source:    "discovery_engine",
		Reason:    "new sensor detected",
		Version:   "1.0.0",
	}

	event := NewEntityCreateEvent(entityID, entityType, properties, metadata)

	if event.Type != EventEntityCreate {
		t.Errorf("Expected event type %v, got %v", EventEntityCreate, event.Type)
	}
	if event.EntityID != entityID {
		t.Errorf("Expected entity ID %v, got %v", entityID, event.EntityID)
	}
	if event.Properties["type"] != entityType {
		t.Errorf("Expected type %v, got %v", entityType, event.Properties["type"])
	}
	if event.Confidence != 1.0 {
		t.Errorf("Expected confidence 1.0, got %v", event.Confidence)
	}
}

func TestNewEntityDeleteEvent(t *testing.T) {
	entityID := "old_sensor_001"
	reason := "sensor offline for 24 hours"
	metadata := EventMetadata{
		RuleName:  "cleanup_rule",
		Timestamp: time.Now(),
		Source:    "cleanup_engine",
		Version:   "1.0.0",
	}

	event := NewEntityDeleteEvent(entityID, reason, metadata)

	if event.Type != EventEntityDelete {
		t.Errorf("Expected event type %v, got %v", EventEntityDelete, event.Type)
	}
	if event.EntityID != entityID {
		t.Errorf("Expected entity ID %v, got %v", entityID, event.EntityID)
	}
	if event.Metadata.Reason != reason {
		t.Errorf("Expected reason %v, got %v", reason, event.Metadata.Reason)
	}
	if event.Confidence != 1.0 {
		t.Errorf("Expected confidence 1.0, got %v", event.Confidence)
	}
}

func TestNewRelationshipDeleteEvent(t *testing.T) {
	fromID := "drone_001"
	toID := "old_battery_001"
	relationshipType := "POWERED_BY"
	metadata := EventMetadata{
		RuleName:  "battery_replacement_rule",
		Timestamp: time.Now(),
		Source:    "rule_engine",
		Reason:    "battery replaced",
		Version:   "1.0.0",
	}

	event := NewRelationshipDeleteEvent(fromID, toID, relationshipType, metadata)

	if event.Type != EventRelationshipDelete {
		t.Errorf("Expected event type %v, got %v", EventRelationshipDelete, event.Type)
	}
	if event.EntityID != fromID {
		t.Errorf("Expected entity ID %v, got %v", fromID, event.EntityID)
	}
	if event.TargetID != toID {
		t.Errorf("Expected target ID %v, got %v", toID, event.TargetID)
	}
	if event.Properties["edge_type"] != relationshipType {
		t.Errorf("Expected edge_type %v, got %v", relationshipType, event.Properties["edge_type"])
	}
}

// Helper function to check if a string contains a substring
func containsString(s, substr string) bool {
	return len(s) >= len(substr) && (s == substr || 
		(len(s) > len(substr) && (s[:len(substr)] == substr || 
		s[len(s)-len(substr):] == substr || 
		findSubstring(s, substr))))
}

func findSubstring(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}
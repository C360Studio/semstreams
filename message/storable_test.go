package message

import (
	"testing"
	"time"
)

// TestStoredEntity implements Storable for testing
type TestStoredEntity struct {
	entityID string
	triples  []Triple
	storage  *StorageReference
}

func (t *TestStoredEntity) EntityID() string {
	return t.entityID
}

func (t *TestStoredEntity) Triples() []Triple {
	return t.triples
}

func (t *TestStoredEntity) StorageRef() *StorageReference {
	return t.storage
}

func TestStorableInterface(t *testing.T) {
	// Create a test entity that implements Storable
	entity := &TestStoredEntity{
		entityID: "c360.platform1.robotics.gcs1.drone.1",
		triples: []Triple{
			{
				Subject:    "c360.platform1.robotics.gcs1.drone.1",
				Predicate:  "robotics.battery.level",
				Object:     85.5,
				Source:     "test",
				Timestamp:  time.Now(),
				Confidence: 1.0,
				Context:    "test-batch-001",
				Datatype:   "xsd:float",
			},
		},
		storage: &StorageReference{
			StorageInstance: "message-store",
			Key:             "2025/01/13/14/msg_abc123",
			ContentType:     "application/json",
			Size:            1024,
		},
	}

	// Verify it implements Storable
	var _ Storable = entity

	// Test Graphable methods
	if entity.EntityID() != "c360.platform1.robotics.gcs1.drone.1" {
		t.Errorf("EntityID() = %v, want %v", entity.EntityID(), "c360.platform1.robotics.gcs1.drone.1")
	}

	if len(entity.Triples()) != 1 {
		t.Errorf("Triples() returned %d triples, want 1", len(entity.Triples()))
	}

	// Test StorageRef method
	ref := entity.StorageRef()
	if ref == nil {
		t.Fatal("StorageRef() returned nil")
	}

	if ref.StorageInstance != "message-store" {
		t.Errorf("StorageInstance = %v, want %v", ref.StorageInstance, "message-store")
	}

	if ref.Key != "2025/01/13/14/msg_abc123" {
		t.Errorf("Key = %v, want %v", ref.Key, "2025/01/13/14/msg_abc123")
	}

	if ref.ContentType != "application/json" {
		t.Errorf("ContentType = %v, want %v", ref.ContentType, "application/json")
	}

	if ref.Size != 1024 {
		t.Errorf("Size = %v, want %v", ref.Size, 1024)
	}
}

func TestStorableWithNilStorage(t *testing.T) {
	// Test that Storable can return nil StorageReference
	entity := &TestStoredEntity{
		entityID: "c360.platform1.robotics.gcs1.drone.2",
		triples:  []Triple{},
		storage:  nil, // No storage reference
	}

	// Should still implement Storable
	var _ Storable = entity

	// StorageRef should return nil without error
	if entity.StorageRef() != nil {
		t.Errorf("StorageRef() = %v, want nil", entity.StorageRef())
	}
}

func TestTripleWithRDFStarFields(t *testing.T) {
	// Test Triple with new Context and Datatype fields
	triple := Triple{
		Subject:    "c360.platform1.robotics.gcs1.drone.1",
		Predicate:  "robotics.position.latitude",
		Object:     37.7749,
		Source:     "gps",
		Timestamp:  time.Now(),
		Confidence: 0.95,
		Context:    "flight-mission-001", // New field
		Datatype:   "geo:lat",            // New field
	}

	// Verify Context field
	if triple.Context != "flight-mission-001" {
		t.Errorf("Context = %v, want %v", triple.Context, "flight-mission-001")
	}

	// Verify Datatype field
	if triple.Datatype != "geo:lat" {
		t.Errorf("Datatype = %v, want %v", triple.Datatype, "geo:lat")
	}
}

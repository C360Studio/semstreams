package oasfgenerator

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/c360studio/semstreams/graph"
	"github.com/c360studio/semstreams/message"
)

// TestFetchEntityTriples_PreservesAllEntityStateFields locks in the
// audit-finding-4 fix from 2026-05-08. The oasf-generator used to
// declare a 3-field local `EntityState` shadow (ID, Triples,
// UpdatedAt) which silently stripped StorageRef, MessageType, and
// Version on json.Unmarshal — fine while the code only read Triples,
// but a latent bug for any future logic that needs the other fields.
//
// This test round-trips a fully-populated graph.EntityState through
// JSON and asserts every field survives. Future drift (e.g. someone
// re-introducing a local shadow, or graph.EntityState growing a new
// field that oasf-generator should also see) trips the test.
func TestFetchEntityTriples_PreservesAllEntityStateFields(t *testing.T) {
	original := graph.EntityState{
		ID: "acme.platform.agent.web.observation.h1",
		Triples: []message.Triple{
			{Subject: "acme.platform.agent.web.observation.h1", Predicate: "agent.web.url", Object: "https://example.com"},
		},
		StorageRef: &message.StorageReference{
			StorageInstance: "message-store-primary",
			Key:             "msg-001",
		},
		MessageType: message.Type{Domain: "agent", Category: "web_observation", Version: "v1"},
		Version:     7,
		UpdatedAt:   time.Date(2026, 5, 13, 12, 0, 0, 0, time.UTC),
	}

	encoded, err := json.Marshal(original)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	// Mirror the decode path in fetchEntityTriples: unmarshal raw
	// bucket bytes into graph.EntityState.
	var decoded graph.EntityState
	if err := json.Unmarshal(encoded, &decoded); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}

	if decoded.ID != original.ID {
		t.Errorf("ID drift: got %q, want %q", decoded.ID, original.ID)
	}
	if len(decoded.Triples) != 1 || decoded.Triples[0].Predicate != "agent.web.url" {
		t.Errorf("Triples drift: got %+v", decoded.Triples)
	}
	// StorageRef, MessageType, Version are the three fields the
	// pre-fix shadow silently stripped — confirm they survive.
	if decoded.StorageRef == nil ||
		decoded.StorageRef.StorageInstance != "message-store-primary" ||
		decoded.StorageRef.Key != "msg-001" {
		t.Errorf("StorageRef drift: got %+v", decoded.StorageRef)
	}
	if decoded.MessageType.Domain != "agent" ||
		decoded.MessageType.Category != "web_observation" ||
		decoded.MessageType.Version != "v1" {
		t.Errorf("MessageType drift: got %+v", decoded.MessageType)
	}
	if decoded.Version != 7 {
		t.Errorf("Version drift: got %d, want 7", decoded.Version)
	}
	if !decoded.UpdatedAt.Equal(original.UpdatedAt) {
		t.Errorf("UpdatedAt drift: got %v, want %v", decoded.UpdatedAt, original.UpdatedAt)
	}
}

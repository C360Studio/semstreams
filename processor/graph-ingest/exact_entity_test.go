package graphingest

import (
	"context"
	"encoding/json"
	"errors"
	"testing"

	"github.com/c360studio/semstreams/graph"
	"github.com/c360studio/semstreams/pkg/errs"
)

func TestHandleQueryEntityReturnsEntityAndSameEntryRevision(t *testing.T) {
	component := createTestComponentWithMockKV(t, withAuthority("acme", "ops"))
	const entityID = "acme.ops.robotics.gcs.drone.001"
	entity := graph.EntityState{ID: entityID, Version: 991}
	data, err := graph.MarshalEntityState(&entity)
	if err != nil {
		t.Fatalf("MarshalEntityState: %v", err)
	}
	revision, err := component.entityBucket.Put(context.Background(), entityID, data)
	if err != nil {
		t.Fatalf("Put: %v", err)
	}

	response, err := component.handleQueryEntityNATS(context.Background(), []byte(`{"id":"`+entityID+`"}`))
	if err != nil {
		t.Fatalf("handleQueryEntityNATS: %v", err)
	}
	var exact graph.ExactEntity
	if err := json.Unmarshal(response, &exact); err != nil {
		t.Fatalf("Unmarshal ExactEntity: %v", err)
	}
	if exact.Entity == nil || exact.Entity.ID != entityID {
		t.Fatalf("entity = %#v", exact.Entity)
	}
	if exact.KVRevision == 0 || exact.KVRevision != revision {
		t.Fatalf("kvRevision = %d, want same-entry revision %d", exact.KVRevision, revision)
	}
	if exact.KVRevision == uint64(exact.Entity.Version) {
		t.Fatalf("logical Version %d was accepted as KV revision", exact.Entity.Version)
	}
}

func TestHandleQueryEntityRejectsMalformedIDBeforeKVLookup(t *testing.T) {
	component := createTestComponentWithMockKV(t, withAuthority("acme", "ops"))
	_, err := component.handleQueryEntityNATS(context.Background(), []byte(`{"id":"not-six-parts"}`))
	if err == nil {
		t.Fatal("malformed entity ID was accepted")
	}
	var classified *errs.ClassifiedError
	if !errors.As(err, &classified) || classified.Code != graph.ErrorCodeInvalidRequest {
		t.Fatalf("error = %v, want invalid_request", err)
	}
}

func TestHandleQueryEntityRejectsAuthorityKeyEntityMismatch(t *testing.T) {
	component := createTestComponentWithMockKV(t, withAuthority("acme", "ops"))
	const (
		requestedID = "acme.ops.robotics.gcs.drone.001"
		storedID    = "acme.ops.robotics.gcs.drone.002"
	)
	data, err := graph.MarshalEntityState(&graph.EntityState{ID: storedID})
	if err != nil {
		t.Fatalf("MarshalEntityState: %v", err)
	}
	if _, err := component.entityBucket.Put(context.Background(), requestedID, data); err != nil {
		t.Fatalf("Put: %v", err)
	}

	_, err = component.handleQueryEntityNATS(context.Background(), []byte(`{"id":"`+requestedID+`"}`))
	if !graph.IsStateContractError(err) {
		t.Fatalf("error = %v, want graph-state poison", err)
	}
}

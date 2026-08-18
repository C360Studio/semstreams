//go:build integration

package flowstore

import (
	"context"
	"testing"

	"github.com/c360studio/semstreams/natsclient"
)

func TestManagerDiagramCRUDAndCAS(t *testing.T) {
	client := natsclient.NewTestClient(t, natsclient.WithJetStream(), natsclient.WithKV()).Client
	store, err := NewManager(context.Background(), client)
	if err != nil {
		t.Fatal(err)
	}
	flow := validTestFlow()
	if err := store.Create(context.Background(), &flow); err != nil {
		t.Fatal(err)
	}
	if flow.Version != 1 || flow.CreatedAt.IsZero() || flow.UpdatedAt.IsZero() {
		t.Fatalf("create did not set audit/version: %#v", flow)
	}

	got, err := store.Get(context.Background(), flow.ID)
	if err != nil {
		t.Fatal(err)
	}
	stale := *got
	got.Name = "Updated"
	if err := store.Update(context.Background(), got); err != nil {
		t.Fatal(err)
	}
	if got.Version != 2 {
		t.Fatalf("update version = %d, want 2", got.Version)
	}
	if err := store.Update(context.Background(), &stale); err == nil {
		t.Fatal("stale update succeeded")
	}

	listed, err := store.List(context.Background())
	if err != nil || len(listed) == 0 {
		t.Fatalf("list: len=%d err=%v", len(listed), err)
	}
	if err := store.Delete(context.Background(), flow.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := store.Get(context.Background(), flow.ID); err == nil {
		t.Fatal("deleted flow still readable")
	}
}

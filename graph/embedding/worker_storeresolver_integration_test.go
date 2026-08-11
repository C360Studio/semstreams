//go:build integration

package embedding

import (
	"context"
	"io"
	"log/slog"
	"testing"
	"time"

	"github.com/c360studio/semstreams/natsclient"
	"github.com/c360studio/semstreams/storage/objectstore"
	"github.com/c360studio/semstreams/storage/storeregistry"
)

// TestIntegration_WorkerResolvesLiveRegistryStatePerFetch proves the registry
// lifecycle against a real NATS ObjectStore. A borrowed store works while its exact
// owner is registered, a live deregistration excludes the body without failure, and
// re-registration is observed on the next fetch without a cached handle or retry loop.
func TestIntegration_WorkerResolvesLiveRegistryStatePerFetch(t *testing.T) {
	testClient := natsclient.NewTestClient(t, natsclient.WithKV())
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()

	store, err := objectstore.NewStoreWithConfig(ctx, testClient.Client, objectstore.Config{
		BucketName:   "EMBEDDING_RESOLVER_LIFECYCLE",
		InstanceName: "content",
	})
	if err != nil {
		t.Fatalf("NewStoreWithConfig: %v", err)
	}
	t.Cleanup(func() { _ = store.Close() })
	if err := store.Put(ctx, "doc/1", []byte("owned body")); err != nil {
		t.Fatalf("Put: %v", err)
	}

	registry := storeregistry.New()
	if err := registry.Register("content", store); err != nil {
		t.Fatalf("Register: %v", err)
	}
	metrics := &countingMetrics{}
	worker := &Worker{
		ctx: ctx, maxSourceTextLen: 100, metrics: metrics, storeResolver: registry,
		logger: slog.New(slog.NewTextHandler(io.Discard, nil)),
	}
	ref := &StorageRef{StorageInstance: "content", Key: "doc/1"}

	body, _, err := worker.fetchTextFromStorage(ref)
	if err != nil || body != "owned body" {
		t.Fatalf("registered fetch = (%q, %v), want owned body", body, err)
	}

	registry.Deregister("content")
	text, err := worker.getSourceText(&Record{StorageRef: ref, IdentityText: "inline identity"})
	if err != nil || text != "inline identity" {
		t.Fatalf("deregistered fetch = (%q, %v), want inline identity exclusion", text, err)
	}
	if metrics.unresolved != 1 || metrics.failed != 0 {
		t.Fatalf("deregistered metrics unresolved=%d failed=%d, want 1/0", metrics.unresolved, metrics.failed)
	}

	if err := registry.Register("content", store); err != nil {
		t.Fatalf("re-register: %v", err)
	}
	body, _, err = worker.fetchTextFromStorage(ref)
	if err != nil || body != "owned body" {
		t.Fatalf("re-registered fetch = (%q, %v), want owned body", body, err)
	}
}

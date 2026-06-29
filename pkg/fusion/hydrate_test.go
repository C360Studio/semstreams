package fusion_test

import (
	"context"
	"errors"
	"testing"

	"github.com/c360studio/semstreams/message"
	"github.com/c360studio/semstreams/pkg/fusion"
)

// fakeStore is a minimal storage.Store for hydration tests. Only Get is
// exercised; the rest satisfy the interface. getErr, when set, is returned from
// Get to exercise the propagation path.
type fakeStore struct {
	data   map[string][]byte
	getErr error
}

func (f *fakeStore) Get(_ context.Context, key string) ([]byte, error) {
	if f.getErr != nil {
		return nil, f.getErr
	}
	b, ok := f.data[key]
	if !ok {
		return nil, errors.New("key not found")
	}
	return b, nil
}
func (f *fakeStore) Put(context.Context, string, []byte) error      { return nil }
func (f *fakeStore) List(context.Context, string) ([]string, error) { return nil, nil }
func (f *fakeStore) Delete(context.Context, string) error           { return nil }

// TestBodyResolver_LocationIndependence proves the crux of ADR-062 increment 4:
// ONE BodyResolver derefs handles across DIFFERENT backends, picked by the
// handle's StorageInstance — the engine never assumes one concrete store. A
// handle into "objectstore-primary" and a handle into "filestore-media" both
// resolve through the same resolver to their own backend's bytes.
func TestBodyResolver_LocationIndependence(t *testing.T) {
	objectStore := &fakeStore{data: map[string][]byte{"code/on_event.go": []byte("package handlers")}}
	fileStore := &fakeStore{data: map[string][]byte{"media/diagram.bin": {0x00, 0xff, 0x00}}}

	r := fusion.NewBodyResolver(fusion.MapStoreResolver{
		"objectstore-primary": objectStore,
		"filestore-media":     fileStore,
	})

	// Handle into the object store → that backend's bytes.
	got, err := r.ResolveBody(context.Background(), &message.StorageReference{
		StorageInstance: "objectstore-primary", Key: "code/on_event.go",
	})
	if err != nil {
		t.Fatalf("ResolveBody(objectstore): %v", err)
	}
	if string(got) != "package handlers" {
		t.Errorf("objectstore body = %q, want %q", got, "package handlers")
	}

	// Handle into the filestore → that backend's (binary, non-UTF-8) bytes,
	// byte-exact (no StoredContent envelope corruption).
	got, err = r.ResolveBody(context.Background(), &message.StorageReference{
		StorageInstance: "filestore-media", Key: "media/diagram.bin",
	})
	if err != nil {
		t.Fatalf("ResolveBody(filestore): %v", err)
	}
	if len(got) != 3 || got[1] != 0xff {
		t.Errorf("filestore body = %v, want [0 255 0] byte-exact", got)
	}
}

func TestBodyResolver_NilRefIsNoBody(t *testing.T) {
	r := fusion.NewBodyResolver(fusion.MapStoreResolver{})
	got, err := r.ResolveBody(context.Background(), nil)
	if err != nil || got != nil {
		t.Errorf("ResolveBody(nil) = (%v, %v), want (nil, nil) — no body is not an error", got, err)
	}
}

func TestBodyResolver_FaultPaths(t *testing.T) {
	store := &fakeStore{data: map[string][]byte{"k": []byte("v")}}
	r := fusion.NewBodyResolver(fusion.MapStoreResolver{"inst": store})

	// Empty StorageInstance on a non-nil handle = producer fault.
	if _, err := r.ResolveBody(context.Background(), &message.StorageReference{Key: "k"}); err == nil {
		t.Error("expected error for empty StorageInstance")
	}
	// Empty Key on a non-nil handle = producer fault (explicit, not a backend
	// not-found) — surfaces a clear message the engine can degrade on.
	if _, err := r.ResolveBody(context.Background(), &message.StorageReference{StorageInstance: "inst"}); err == nil {
		t.Error("expected error for empty Key")
	}
	// Unknown instance = wiring fault.
	if _, err := r.ResolveBody(context.Background(), &message.StorageReference{StorageInstance: "nope", Key: "k"}); err == nil {
		t.Error("expected error for unregistered instance")
	}
	// Store Get error propagates.
	failing := fusion.NewBodyResolver(fusion.MapStoreResolver{"inst": &fakeStore{getErr: errors.New("backend down")}})
	if _, err := failing.ResolveBody(context.Background(), &message.StorageReference{StorageInstance: "inst", Key: "k"}); err == nil {
		t.Error("expected the store Get error to propagate")
	}
}

func TestBodyResolver_NilResolverErrorsOnHandle(t *testing.T) {
	r := fusion.NewBodyResolver(nil)
	// nil handle is still no-body (no resolver needed).
	if _, err := r.ResolveBody(context.Background(), nil); err != nil {
		t.Errorf("nil handle with nil resolver should be (nil,nil), got err %v", err)
	}
	// A real handle with no resolver configured is a wiring fault.
	if _, err := r.ResolveBody(context.Background(), &message.StorageReference{StorageInstance: "inst", Key: "k"}); err == nil {
		t.Error("expected error resolving a handle with no resolver configured")
	}
}

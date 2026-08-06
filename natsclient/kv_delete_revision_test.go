package natsclient

import (
	"context"
	"errors"
	"testing"

	"github.com/c360studio/semstreams/pkg/errs"
	"github.com/nats-io/nats.go/jetstream"
)

type deleteAtRevisionBucket struct {
	jetstream.KeyValue
	deleteErr   error
	deleteCalls int
}

func (b *deleteAtRevisionBucket) Delete(
	_ context.Context,
	_ string,
	_ ...jetstream.KVDeleteOpt,
) error {
	b.deleteCalls++
	return b.deleteErr
}

func TestKVStoreDeleteAtRevisionRejectsInvalidInputBeforeIO(t *testing.T) {
	tests := []struct {
		name     string
		key      string
		revision uint64
	}{
		{name: "zero revision", key: "acme.ops.robotics.gcs.drone.001"},
		{name: "invalid key", key: "acme.*.robotics.gcs.drone.001", revision: 1},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			bucket := &deleteAtRevisionBucket{}
			store := &KVStore{bucket: bucket, options: DefaultKVOptions()}

			err := store.DeleteAtRevision(context.Background(), tt.key, tt.revision)
			if err == nil || !errs.IsInvalid(err) {
				t.Fatalf("DeleteAtRevision() error = %v, want classified invalid", err)
			}
			if bucket.deleteCalls != 0 {
				t.Fatalf("Delete() calls = %d, want 0", bucket.deleteCalls)
			}
		})
	}
}

func TestKVStoreDeleteAtRevisionMapsTypedErrors(t *testing.T) {
	transient := errors.New("permission denied")
	tests := []struct {
		name string
		raw  error
		want error
	}{
		{name: "not found", raw: jetstream.ErrKeyNotFound, want: ErrKVKeyNotFound},
		{name: "revision mismatch", raw: errors.New("wrong last sequence"), want: ErrKVRevisionMismatch},
		{name: "other", raw: transient, want: transient},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			bucket := &deleteAtRevisionBucket{deleteErr: tt.raw}
			store := &KVStore{bucket: bucket, options: DefaultKVOptions()}

			err := store.DeleteAtRevision(context.Background(), "acme.ops.robotics.gcs.drone.001", 42)
			if !errors.Is(err, tt.want) {
				t.Fatalf("DeleteAtRevision() error = %v, want errors.Is(_, %v)", err, tt.want)
			}
			if bucket.deleteCalls != 1 {
				t.Fatalf("Delete() calls = %d, want 1", bucket.deleteCalls)
			}
		})
	}
}

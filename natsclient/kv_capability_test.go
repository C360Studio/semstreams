package natsclient

import (
	"context"
	"errors"
	"testing"

	"github.com/nats-io/nats.go/jetstream"
)

type statusOnlyBucket struct {
	err error
}

func (b statusOnlyBucket) Status(context.Context) (jetstream.KeyValueStatus, error) {
	return nil, b.err
}

func TestBucketLastSeqAcceptsStatusOnlyReader(t *testing.T) {
	want := errors.New("status unavailable")
	_, err := BucketLastSeq(context.Background(), statusOnlyBucket{err: want})
	if !errors.Is(err, want) {
		t.Fatalf("BucketLastSeq error = %v, want wrapped %v", err, want)
	}
}

package graphclustering

import (
	"context"

	"github.com/nats-io/nats.go/jetstream"
)

type minimalEntityBucketReader struct{}

func (minimalEntityBucketReader) Get(context.Context, string) (jetstream.KeyValueEntry, error) {
	return nil, nil
}

func (minimalEntityBucketReader) Keys(context.Context, ...jetstream.WatchOpt) ([]string, error) {
	return nil, nil
}

type minimalOutgoingBucketReader struct{}

func (minimalOutgoingBucketReader) Get(context.Context, string) (jetstream.KeyValueEntry, error) {
	return nil, nil
}

type minimalIncomingBucketReader struct{}

func (minimalIncomingBucketReader) ListKeysFiltered(context.Context, ...string) (jetstream.KeyLister, error) {
	return nil, nil
}

var (
	_ entityBucketReader   = minimalEntityBucketReader{}
	_ outgoingBucketReader = minimalOutgoingBucketReader{}
	_ incomingBucketReader = minimalIncomingBucketReader{}
	_ interface {
		Get(context.Context, string) (jetstream.KeyValueEntry, error)
		Keys(context.Context, ...jetstream.WatchOpt) ([]string, error)
	} = (entityBucketReader)(nil)
	_ interface {
		Get(context.Context, string) (jetstream.KeyValueEntry, error)
	} = (outgoingBucketReader)(nil)
	_ interface {
		ListKeysFiltered(context.Context, ...string) (jetstream.KeyLister, error)
	} = (incomingBucketReader)(nil)
)

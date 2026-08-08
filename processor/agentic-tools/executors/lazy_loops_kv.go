package executors

import (
	"context"

	"github.com/c360studio/semstreams/natsclient"
)

// lazyLoopsKV binds AGENT_LOOPS must-exist at each read operation. Registration
// therefore succeeds before agentic-loop starts and provisions its owned bucket,
// while readers can never create or configure that bucket.
type lazyLoopsKV struct {
	client *natsclient.Client
	bucket string
}

func (l lazyLoopsKV) Get(ctx context.Context, key string) (*natsclient.KVEntry, error) {
	store, err := l.open(ctx)
	if err != nil {
		return nil, err
	}
	return store.Get(ctx, key)
}

func (l lazyLoopsKV) Keys(ctx context.Context) ([]string, error) {
	store, err := l.open(ctx)
	if err != nil {
		return nil, err
	}
	return store.Keys(ctx)
}

func (l lazyLoopsKV) open(ctx context.Context) (*natsclient.KVStore, error) {
	bucket, err := l.client.GetKeyValueBucket(ctx, l.bucket)
	if err != nil {
		return nil, err
	}
	return l.client.NewKVStore(bucket), nil
}

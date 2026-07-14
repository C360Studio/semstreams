package graphclustering

import (
	"context"
	"errors"
	"testing"

	"github.com/nats-io/nats.go/jetstream"
)

func TestCommunityQueryPropagatesCallerCancellation(t *testing.T) {
	t.Parallel()

	bucket := newMockKVBucket()
	bucket.getFunc = func(ctx context.Context, _ string) (jetstream.KeyValueEntry, error) {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		return nil, errors.New("caller context was not propagated")
	}
	c := &Component{communityBucket: bucket}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	_, err := c.handleQueryCommunityNATS(ctx, []byte(`{"id":"community-1"}`))
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("query error = %v, want caller context cancellation", err)
	}
}

func TestLevelQueryDoesNotReturnPartialResultsOnEntryFailure(t *testing.T) {
	t.Parallel()

	backendErr := errors.New("injected community read failure")
	bucket := &queryFailureBucket{mockKVBucket: newMockKVBucket(), keys: []string{"0.community-1"}}
	bucket.getFunc = func(context.Context, string) (jetstream.KeyValueEntry, error) {
		return nil, backendErr
	}
	c := &Component{communityBucket: bucket}

	_, err := c.handleQueryLevelNATS(context.Background(), []byte(`{"level":0}`))
	if !errors.Is(err, backendErr) {
		t.Fatalf("query error = %v, want injected backend failure", err)
	}
}

func TestLevelQueryRejectsMalformedCommunityRecord(t *testing.T) {
	t.Parallel()

	bucket := &queryFailureBucket{mockKVBucket: newMockKVBucket(), keys: []string{"0.community-1"}}
	bucket.getFunc = func(context.Context, string) (jetstream.KeyValueEntry, error) {
		return &mockKVEntry{data: []byte(`{"id":`)}, nil
	}
	c := &Component{communityBucket: bucket}

	if _, err := c.handleQueryLevelNATS(context.Background(), []byte(`{"level":0}`)); err == nil {
		t.Fatal("malformed community record was silently omitted from a successful response")
	}
}

type queryFailureBucket struct {
	*mockKVBucket
	keys []string
}

func (b *queryFailureBucket) ListKeysFiltered(context.Context, ...string) (jetstream.KeyLister, error) {
	keys := make(chan string, len(b.keys))
	for _, key := range b.keys {
		keys <- key
	}
	close(keys)
	return &mockKeyLister{ch: keys}, nil
}

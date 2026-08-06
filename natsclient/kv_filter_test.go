package natsclient

import (
	"context"
	"errors"
	"testing"

	"github.com/nats-io/nats.go/jetstream"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type testKeyLister struct {
	keys    chan string
	stopped bool
}

func (l *testKeyLister) Keys() <-chan string { return l.keys }

func (l *testKeyLister) Stop() error {
	l.stopped = true
	return nil
}

var _ jetstream.KeyLister = (*testKeyLister)(nil)

type filteredKeysOnly struct {
	lister  jetstream.KeyLister
	filters []string
}

func (r *filteredKeysOnly) ListKeysFiltered(
	_ context.Context,
	filters ...string,
) (jetstream.KeyLister, error) {
	r.filters = append([]string(nil), filters...)
	return r.lister, nil
}

func TestCollectFilteredKeys_ReturnsCompleteSnapshot(t *testing.T) {
	lister := &testKeyLister{keys: make(chan string, 2)}
	lister.keys <- "one"
	lister.keys <- "two"
	close(lister.keys)

	keys, err := collectFilteredKeys(context.Background(), lister)
	require.NoError(t, err)
	assert.Equal(t, []string{"one", "two"}, keys)
	assert.True(t, lister.stopped)
}

func TestCollectFilteredKeys_CancellationDiscardsPartialSnapshot(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	lister := &testKeyLister{keys: make(chan string, 1)}
	lister.keys <- "partial"
	close(lister.keys)
	cancel()

	keys, err := collectFilteredKeys(ctx, lister)
	assert.ErrorIs(t, err, context.Canceled)
	assert.Nil(t, keys, "a cancelled listing must never return partial keys")
	assert.True(t, lister.stopped)
}

func TestCollectFilteredKeys_CancellationWhileWaiting(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	lister := &testKeyLister{keys: make(chan string)}
	cancel()

	keys, err := collectFilteredKeys(ctx, lister)
	assert.True(t, errors.Is(err, context.Canceled))
	assert.Nil(t, keys)
	assert.True(t, lister.stopped)
}

func TestFilteredKeysAcceptsMinimalReader(t *testing.T) {
	lister := &testKeyLister{keys: make(chan string, 1)}
	lister.keys <- "one"
	close(lister.keys)
	reader := &filteredKeysOnly{lister: lister}

	keys, err := FilteredKeys(context.Background(), reader, "domain.>")

	require.NoError(t, err)
	assert.Equal(t, []string{"one"}, keys)
	assert.Equal(t, []string{"domain.>"}, reader.filters)
	assert.True(t, lister.stopped)
}

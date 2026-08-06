package executors

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/c360studio/semstreams/graph"
	"github.com/nats-io/nats.go/jetstream"
	"github.com/stretchr/testify/require"
)

type failingGraphQueryReader struct {
	graph.CatalogReader
	err      error
	deadline time.Time
}

func (r *failingGraphQueryReader) Get(ctx context.Context, _ string) (jetstream.KeyValueEntry, error) {
	r.deadline, _ = ctx.Deadline()
	return nil, r.err
}

func TestGetGraphQueryEntryPreservesCallerPolicy(t *testing.T) {
	sentinel := errors.New("read failed")
	reader := &failingGraphQueryReader{err: sentinel}
	started := time.Now()

	_, err := getGraphQueryEntry(context.Background(), reader, "entity-id")

	require.ErrorIs(t, err, sentinel)
	require.ErrorContains(t, err, "kv get entity-id")
	require.False(t, reader.deadline.IsZero())
	require.WithinDuration(t, started.Add(5*time.Second), reader.deadline, time.Second)
}

func TestGetGraphQueryEntryMapsNotFound(t *testing.T) {
	reader := &failingGraphQueryReader{err: jetstream.ErrKeyNotFound}

	_, err := getGraphQueryEntry(context.Background(), reader, "missing")

	require.ErrorIs(t, err, ErrKeyNotFound)
}

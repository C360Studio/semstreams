package agentictools

import (
	"context"
	"log/slog"

	"github.com/c360studio/semstreams/natsclient"
	"github.com/c360studio/semstreams/processor/agentic-tools/executors"
)

// registerGraphQuery wires the query_entity tool (executors.GraphQueryExecutor)
// against the ENTITY_STATES KV bucket. The executor lives in the executors
// package but needs runtime deps (a KV-bucket handle) so registration has
// to happen here rather than at init time.
//
// Failure is non-fatal: if the ENTITY_STATES bucket is unavailable (e.g.
// the flow didn't declare it), we log a warning and skip the registration.
// The rest of the component's tools stay available.
func (c *Component) registerGraphQuery(ctx context.Context) {
	const bucketName = "ENTITY_STATES"
	const toolName = "query_entity"

	bucket, err := c.natsClient.GetKeyValueBucket(ctx, bucketName)
	if err != nil {
		c.logger.Warn("query_entity tool disabled: could not open entity-states bucket",
			slog.String("bucket", bucketName),
			slog.Any("error", err))
		return
	}

	store := c.natsClient.NewKVStore(bucket)
	executor := executors.NewGraphQueryExecutor(&graphQueryKVAdapter{store: store})
	if err := registerGlobalTool(toolName, executor); err != nil {
		c.logger.Warn("Failed to register query_entity tool",
			slog.Any("error", err))
		return
	}
	c.logger.Info("Registered query_entity tool (global)",
		slog.String("bucket", bucketName))
}

// graphQueryKVAdapter bridges natsclient.KVStore to the executors.KVGetter
// shape (method-based Value/Revision). natsclient.KVEntry exposes those as
// fields, so we wrap it in a thin pointer-receiver type that forwards to
// the fields. Local to this file so the adapter stays with its only caller
// and executors stays unaware of natsclient.
type graphQueryKVAdapter struct {
	store *natsclient.KVStore
}

func (a *graphQueryKVAdapter) Get(ctx context.Context, key string) (executors.KVEntry, error) {
	entry, err := a.store.Get(ctx, key)
	if err != nil {
		return nil, err
	}
	return &graphQueryKVEntry{entry: entry}, nil
}

type graphQueryKVEntry struct {
	entry *natsclient.KVEntry
}

func (e *graphQueryKVEntry) Value() []byte    { return e.entry.Value }
func (e *graphQueryKVEntry) Revision() uint64 { return e.entry.Revision }

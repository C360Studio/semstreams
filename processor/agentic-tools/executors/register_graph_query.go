package executors

import (
	"context"
	"log/slog"
	"time"

	"github.com/nats-io/nats.go/jetstream"

	"github.com/c360studio/semstreams/natsclient"
	agentictools "github.com/c360studio/semstreams/processor/agentic-tools"
)

// entityStatesBucket is the KV bucket query_entity reads from. Same
// name any flow using graph-ingest produces, so hardcoding it here is
// safe as long as that contract holds.
const entityStatesBucket = "ENTITY_STATES"

// entityStatesBucketConfig mirrors graph/query/client.go defaults
// (History=3, TTL=24h). Tool registration races with the graph component's
// ensureBuckets — CreateKeyValueBucket is idempotent Create-or-Get, so
// whichever side lands first creates with these values and the other gets
// the existing handle. Config must match the graph component's intent.
var entityStatesBucketConfig = jetstream.KeyValueConfig{
	Bucket:  entityStatesBucket,
	History: 3,
	TTL:     24 * time.Hour,
}

// registerGraphQuery wires the query_entity tool against ENTITY_STATES.
// Failure is non-fatal: if the bucket is unavailable (e.g. the flow
// didn't declare it), we log a warning and skip. The rest of the
// caller's tools stay available.
func registerGraphQuery(ctx context.Context, tools *agentictools.ExecutorRegistry, natsClient *natsclient.Client, logger *slog.Logger) {
	const toolName = "query_entity"

	bucket, err := natsClient.CreateKeyValueBucket(ctx, entityStatesBucketConfig)
	if err != nil {
		logger.Warn("query_entity tool disabled: could not open entity-states bucket",
			slog.String("bucket", entityStatesBucket),
			slog.Any("error", err))
		return
	}

	store := natsClient.NewKVStore(bucket)
	executor := NewGraphQueryExecutor(&graphQueryKVAdapter{store: store})
	if err := tools.RegisterTool(toolName, executor); err != nil {
		logger.Warn("Failed to register query_entity tool", slog.Any("error", err))
		return
	}
	logger.Info("Registered query_entity tool (global)",
		slog.String("bucket", entityStatesBucket))
}

// graphQueryKVAdapter bridges natsclient.KVStore to the KVGetter shape
// GraphQueryExecutor consumes. natsclient.KVEntry has Value/Revision as
// fields; the local wrapper types forward them as methods. Kept in this
// file so the adapter stays next to its only caller.
type graphQueryKVAdapter struct {
	store *natsclient.KVStore
}

func (a *graphQueryKVAdapter) Get(ctx context.Context, key string) (KVEntry, error) {
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

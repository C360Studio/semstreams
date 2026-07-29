package executors

import (
	"context"
	"fmt"
	"log/slog"

	"github.com/nats-io/nats.go/jetstream"

	"github.com/c360studio/semstreams/graph"
	"github.com/c360studio/semstreams/natsclient"
	"github.com/c360studio/semstreams/pkg/retry"
	agentictools "github.com/c360studio/semstreams/processor/agentic-tools"
)

// registerGraphQuery wires GraphQueryExecutor against ENTITY_STATES.
// GraphQueryExecutor exposes five tools (query_entity, query_entities,
// query_relationships, query_neighbors, query_by_type) via ListTools();
// RegisterExecutor maps each advertised name to the executor so dispatch
// resolves any of the five.
//
// Tool registration is a READER of ENTITY_STATES, so it binds must-exist
// through the catalog reader seam — it NEVER creates the bucket (a reader
// create here once raced graph-ingest with a divergent History and made the
// graph's actual config a boot-order coin flip; a stale TTL mirror once
// dead-locked graph-ingest, gh#484). The owner (graph-ingest) provisions the
// bucket through the owner seam.
//
// The open is wrapped in retry.Quick so a transient NATS hiccup at boot
// doesn't silently disable the tool for the process lifetime. After retries
// are exhausted we fall through to warn-and-skip — a flow that doesn't run
// graph-ingest is still a legal deployment. A registry-level failure
// (duplicate name) propagates so RegisterBuiltins can surface it at boot.
func registerGraphQuery(ctx context.Context, tools *agentictools.ExecutorRegistry, natsClient *natsclient.Client, logger *slog.Logger) error {
	bucket, err := retry.DoWithResult(ctx, retry.Quick(), func() (jetstream.KeyValue, error) {
		return graph.OpenCatalogBucket(ctx, natsClient, graph.BucketEntityStates)
	})
	if err != nil {
		logger.Warn("graph query tools disabled: could not open entity-states bucket after retries",
			slog.String("bucket", graph.BucketEntityStates),
			slog.Any("error", err))
		return nil
	}

	store := natsClient.NewKVStore(bucket)
	executor := NewGraphQueryExecutor(&graphQueryKVAdapter{store: store})
	if err := tools.RegisterExecutor(executor); err != nil {
		return fmt.Errorf("register graph query tools: %w", err)
	}
	logger.Info("Registered graph query tools",
		slog.String("bucket", graph.BucketEntityStates),
		slog.Int("count", len(executor.ListTools())))
	return nil
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

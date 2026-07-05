package executors

import (
	"context"
	"fmt"
	"log/slog"

	"github.com/nats-io/nats.go/jetstream"

	"github.com/c360studio/semstreams/natsclient"
	"github.com/c360studio/semstreams/pkg/retry"
	agentictools "github.com/c360studio/semstreams/processor/agentic-tools"
)

// entityStatesBucket is the KV bucket query_entity reads from. Same
// name any flow using graph-ingest produces, so hardcoding it here is
// safe as long as that contract holds.
const entityStatesBucket = "ENTITY_STATES"

// entityStatesBucketConfig mirrors graph/query/client.go defaults
// (History=3, TTL=0). Tool registration races with the graph component's
// ensureBuckets — CreateKeyValueBucket is idempotent Create-or-Get, so
// whichever side lands first creates with these values and the other gets
// the existing handle. Config must match the graph component's intent.
//
// TTL MUST be 0 on the live graph (ADR-068 D1): age-eviction is
// reachability-blind and would silently expire entities with live inbound
// edges. graph-ingest fail-closes at boot if it finds ENTITY_STATES carrying a
// TTL, so a non-zero value here (a stale 24h mirror of the OLD graph/query
// default, gh#484) makes this registration win the race and dead-lock
// graph-ingest. Keep it 0, matching graph/query/client.go:DefaultConfig.
var entityStatesBucketConfig = jetstream.KeyValueConfig{
	Bucket:  entityStatesBucket,
	History: 3,
	TTL:     0,
}

// registerGraphQuery wires GraphQueryExecutor against ENTITY_STATES.
// GraphQueryExecutor exposes five tools (query_entity, query_entities,
// query_relationships, query_neighbors, query_by_type) via ListTools();
// RegisterExecutor maps each advertised name to the executor so dispatch
// resolves any of the five.
//
// The bucket open is wrapped in retry.Quick so a transient NATS hiccup
// at boot doesn't silently disable the tool for the process lifetime.
// After retries are exhausted we fall through to warn-and-skip — a
// flow that doesn't declare the bucket is still a legal deployment.
// A registry-level failure (duplicate name) propagates so
// RegisterBuiltins can surface it at boot.
func registerGraphQuery(ctx context.Context, tools *agentictools.ExecutorRegistry, natsClient *natsclient.Client, logger *slog.Logger) error {
	bucket, err := retry.DoWithResult(ctx, retry.Quick(), func() (jetstream.KeyValue, error) {
		return natsClient.CreateKeyValueBucket(ctx, entityStatesBucketConfig)
	})
	if err != nil {
		logger.Warn("graph query tools disabled: could not open entity-states bucket after retries",
			slog.String("bucket", entityStatesBucket),
			slog.Any("error", err))
		return nil
	}

	store := natsClient.NewKVStore(bucket)
	executor := NewGraphQueryExecutor(&graphQueryKVAdapter{store: store})
	if err := tools.RegisterExecutor(executor); err != nil {
		return fmt.Errorf("register graph query tools: %w", err)
	}
	logger.Info("Registered graph query tools",
		slog.String("bucket", entityStatesBucket),
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

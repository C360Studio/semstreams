package executors

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"sync"

	"github.com/c360studio/semstreams/graph"
	"github.com/c360studio/semstreams/natsclient"
	agentictools "github.com/c360studio/semstreams/processor/agentic-tools"
)

// registerGraphQuery wires GraphQueryExecutor against ENTITY_STATES.
// GraphQueryExecutor exposes five tools (query_entity, query_entities,
// query_relationships, query_neighbors, query_by_type) via ListTools();
// RegisterExecutor maps each advertised name to the executor so dispatch
// resolves any of the five.
//
// Registration is UNCONDITIONAL and the ENTITY_STATES bind is LAZY, resolved
// per execution through the catalog reader seam. Both mains call
// RegisterBuiltins BEFORE Manager.StartAll, and graph-ingest provisions
// ENTITY_STATES inside its component Start — so on a clean deployment the
// bucket does not exist at registration time. The registry is built once per
// process: a registration-time skip would lose all five tools for the process
// lifetime on exactly the first-install path. Instead the executor registers
// immediately and every execution binds must-exist via OpenCatalogBucket
// (cached after the first success), returning the seam's classified not-ready
// error — naming the owner, graph-ingest — until the owner has provisioned
// the bucket. The reader still NEVER creates (a reader create here once raced
// graph-ingest with a divergent History and made the graph's actual config a
// boot-order coin flip; a stale TTL mirror once dead-locked graph-ingest,
// gh#484), and there is zero boot-order coupling.
//
// A registry-level failure (duplicate name) propagates so RegisterBuiltins
// can surface it at boot.
func registerGraphQuery(_ context.Context, tools *agentictools.ExecutorRegistry, natsClient *natsclient.Client, logger *slog.Logger) error {
	executor := NewGraphQueryExecutor(&graphQueryKVAdapter{natsClient: natsClient})
	if err := tools.RegisterExecutor(executor); err != nil {
		return fmt.Errorf("register graph query tools: %w", err)
	}
	logger.Info("Registered graph query tools (lazy must-exist bind at execution time)",
		slog.String("bucket", graph.BucketEntityStates),
		slog.Int("count", len(executor.ListTools())))
	return nil
}

// graphQueryKVAdapter bridges natsclient.KVStore to the KVGetter shape
// GraphQueryExecutor consumes, resolving the ENTITY_STATES bucket LAZILY at
// execution time: bind must-exist through the catalog reader seam, cache the
// store on first success (mutex-guarded), and until then surface the seam's
// classified not-ready error naming the catalog owner. natsclient.KVEntry has
// Value/Revision as fields; the local wrapper types forward them as methods.
// Kept in this file so the adapter stays next to its only caller.
type graphQueryKVAdapter struct {
	natsClient *natsclient.Client

	mu    sync.Mutex
	store *natsclient.KVStore // cached after the first successful bind
}

// bind resolves the ENTITY_STATES store, caching the handle on first success.
// It NEVER creates the bucket: an absent bucket is the seam's classified
// not-ready error (its owner, graph-ingest, has not provisioned it yet), and
// the next execution simply retries the bind.
func (a *graphQueryKVAdapter) bind(ctx context.Context) (*natsclient.KVStore, error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.store != nil {
		return a.store, nil
	}
	bucket, err := graph.OpenCatalogBucket(ctx, a.natsClient, graph.BucketEntityStates)
	if err != nil {
		return nil, err
	}
	a.store = a.natsClient.NewKVStore(bucket)
	return a.store, nil
}

func (a *graphQueryKVAdapter) Get(ctx context.Context, key string) (KVEntry, error) {
	store, err := a.bind(ctx)
	if err != nil {
		return nil, err
	}
	entry, err := store.Get(ctx, key)
	if err != nil {
		if errors.Is(err, natsclient.ErrKVKeyNotFound) {
			// Map the store's sentinel onto the executor's, so a missing
			// entity reports ToolErrorNotFound rather than a network failure
			// (the executor matches ErrKeyNotFound by identity).
			return nil, ErrKeyNotFound
		}
		return nil, err
	}
	return &graphQueryKVEntry{entry: entry}, nil
}

type graphQueryKVEntry struct {
	entry *natsclient.KVEntry
}

func (e *graphQueryKVEntry) Value() []byte    { return e.entry.Value }
func (e *graphQueryKVEntry) Revision() uint64 { return e.entry.Revision }

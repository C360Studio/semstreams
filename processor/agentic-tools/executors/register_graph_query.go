package executors

import (
	"context"
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
// immediately and every execution binds must-exist via OpenCatalogReader
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

// graphQueryKVAdapter bridges graph.CatalogReader to the KVGetter shape
// GraphQueryExecutor consumes, resolving the ENTITY_STATES bucket LAZILY at
// execution time: bind must-exist through the catalog reader seam, cache the
// reader on first success (mutex-guarded), and until then surface the seam's
// classified not-ready error naming the catalog owner. JetStream entries have
// the same Value/Revision method shape the local executor consumes.
type graphQueryKVAdapter struct {
	natsClient *natsclient.Client

	mu     sync.Mutex
	reader graph.CatalogReader // cached after the first successful bind
}

// bind resolves the ENTITY_STATES reader, caching it on first success.
// It NEVER creates the bucket: an absent bucket is the seam's classified
// not-ready error (its owner, graph-ingest, has not provisioned it yet), and
// the next execution simply retries the bind.
func (a *graphQueryKVAdapter) bind(ctx context.Context) (graph.CatalogReader, error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.reader != nil {
		return a.reader, nil
	}
	reader, err := graph.OpenCatalogReader(ctx, a.natsClient, graph.BucketEntityStates)
	if err != nil {
		return nil, err
	}
	a.reader = reader
	return a.reader, nil
}

func (a *graphQueryKVAdapter) Get(ctx context.Context, key string) (KVEntry, error) {
	reader, err := a.bind(ctx)
	if err != nil {
		return nil, err
	}
	entry, err := getGraphQueryEntry(ctx, reader, key)
	if err != nil {
		return nil, err
	}
	return entry, nil
}

// KeysByPattern implements KVKeyLister over the same lazily-bound catalog
// reader Get uses. natsclient.FilteredKeys is the package-level helper the
// framework's other filtered listings run on; it returns (nil, nil) for an
// empty match and rejects a partial list when the context expires, so a
// truncated scan can never be reported as a complete one.
//
// The keys come back in KV scan order — FilteredKeys sorts nothing — and the
// executor sorts them itself before paging (see queryByType).
func (a *graphQueryKVAdapter) KeysByPattern(ctx context.Context, pattern string) ([]string, error) {
	reader, err := a.bind(ctx)
	if err != nil {
		return nil, err
	}
	return listGraphQueryKeys(ctx, reader, pattern)
}

func listGraphQueryKeys(ctx context.Context, reader graph.CatalogReader, pattern string) ([]string, error) {
	ctx, cancel := context.WithTimeout(ctx, natsclient.DefaultKVOptions().Timeout)
	defer cancel()

	keys, err := natsclient.FilteredKeys(ctx, reader, pattern)
	if err != nil {
		return nil, fmt.Errorf("kv keys by pattern %s: %w", pattern, err)
	}
	return keys, nil
}

func getGraphQueryEntry(ctx context.Context, reader graph.CatalogReader, key string) (KVEntry, error) {
	ctx, cancel := context.WithTimeout(ctx, natsclient.DefaultKVOptions().Timeout)
	defer cancel()

	entry, err := reader.Get(ctx, key)
	if err != nil {
		if natsclient.IsKVNotFoundError(err) {
			// Map the store's sentinel onto the executor's, so a missing
			// entity reports ToolErrorNotFound rather than a network failure
			// (the executor matches ErrKeyNotFound by identity).
			return nil, ErrKeyNotFound
		}
		return nil, fmt.Errorf("kv get %s: %w", key, err)
	}
	return entry, nil
}

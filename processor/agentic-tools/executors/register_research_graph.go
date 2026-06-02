package executors

import (
	"context"
	"fmt"
	"log/slog"

	"github.com/nats-io/nats.go/jetstream"

	"github.com/c360studio/semstreams/component"
	"github.com/c360studio/semstreams/natsclient"
	"github.com/c360studio/semstreams/pkg/retry"
	agentictools "github.com/c360studio/semstreams/processor/agentic-tools"
	"github.com/c360studio/semstreams/processor/research-graph-llmwrap"
)

// registerResearchGraph opens (or creates) the AGENT_LOOPS bucket
// and wires the research_graph tool. Bucket config matches
// agentic-loop/component.go initializeKVBuckets (History=10, TTL=24h)
// so whichever side reaches the bucket-create call first wins the
// idempotent Create-or-Get without config drift.
//
// Mirrors register_read_loop_result's retry + warn-and-skip path:
// transient NATS errors at boot don't block the process, but a
// misconfigured deployment gets a loud log line so operators don't
// chase silent disablement. A registry-level failure (duplicate
// name) propagates so RegisterBuiltins surfaces it at boot.
//
// The tool needs the AGENT_LOOPS handle — both for the
// research-pipeline LoopEntity write and for the
// research.requested.<loopID> trigger key R0 watches.
func registerResearchGraph(ctx context.Context, tools *agentictools.ExecutorRegistry, natsClient *natsclient.Client, platform component.PlatformMeta, logger *slog.Logger, bucketName string) error {
	bucket, err := retry.DoWithResult(ctx, retry.Quick(), func() (jetstream.KeyValue, error) {
		return natsClient.CreateKeyValueBucket(ctx, newLoopResultBucketConfig(bucketName))
	})
	if err != nil {
		logger.Warn("research_graph tool disabled: could not open loops bucket after retries",
			slog.String("bucket", bucketName),
			slog.Any("error", err))
		return nil
	}

	store := natsClient.NewKVStore(bucket)
	writer := newNATSResearchKVWriter(store)
	executor := NewResearchGraphExecutor(
		writer,
		platform,
		WithResearchGraphTriplePublisher(llmwrap.NewNATSTriplePublisher(natsClient)),
	)
	executor.SetLogger(logger)
	if err := tools.RegisterTool(ResearchGraphToolName, executor); err != nil {
		return fmt.Errorf("register research_graph: %w", err)
	}
	logger.Info("Registered research_graph tool",
		slog.String("bucket", bucketName))
	return nil
}

// natsResearchKVWriter adapts natsclient.KVStore to the
// ResearchKVWriter interface the executor consumes. Keeps the
// production wiring colocated with the registration shim so the
// executor itself stays test-friendly (the executor only sees the
// narrow interface).
type natsResearchKVWriter struct {
	kv *natsclient.KVStore
}

func newNATSResearchKVWriter(kv *natsclient.KVStore) *natsResearchKVWriter {
	return &natsResearchKVWriter{kv: kv}
}

// CreateLoopEntity uses KVStore.Create (write-if-not-exists) so a
// loopID collision surfaces loudly via ErrKVKeyExists rather than
// silently overwriting prior state.
func (w *natsResearchKVWriter) CreateLoopEntity(ctx context.Context, loopID string, value []byte) error {
	_, err := w.kv.Create(ctx, loopID, value)
	return err
}

// PutResearchTrigger uses KVStore.Put (last-writer-wins). The trigger
// key embeds a freshly minted loopID so a Put-vs-Create distinction
// is moot in practice — Put keeps the implementation simple and lets
// the chain's idempotency live with R0's debounce logic in PR 6.
func (w *natsResearchKVWriter) PutResearchTrigger(ctx context.Context, loopID string, value []byte) error {
	_, err := w.kv.Put(ctx, researchTriggerKeyPrefix+loopID, value)
	return err
}

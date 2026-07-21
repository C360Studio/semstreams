package embedding

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"runtime/debug"
	"strings"
	"sync"

	"github.com/nats-io/nats.go"
	"github.com/nats-io/nats.go/jetstream"

	"github.com/c360studio/semstreams/pkg/errs"
	"github.com/c360studio/semstreams/storage"
)

// defaultMaxSourceTextLen is the safety cap for streaming content reads when
// maxSourceTextLen is 0 (unconfigured). Prevents unbounded memory allocation
// for very large stored content.
const defaultMaxSourceTextLen = 8000

// isExpectedShutdownError returns true if the error is expected during component shutdown.
// These include subscription cleanup errors and consumer not found errors which occur
// when NATS resources are cleaned up before or during Stop().
func isExpectedShutdownError(err error) bool {
	if errors.Is(err, nats.ErrBadSubscription) {
		return true
	}
	if errors.Is(err, jetstream.ErrConsumerNotFound) {
		return true
	}
	// Also check error string for cases where errors.Is doesn't match
	errStr := err.Error()
	return strings.Contains(errStr, "invalid subscription") ||
		strings.Contains(errStr, "consumer not found")
}

// GeneratedCallback is called when an embedding is successfully generated.
// The callback receives the entity ID and the generated embedding vector.
type GeneratedCallback func(entityID string, embedding []float32)

// TerminalCallback is called when a pending embedding reaches ANY terminal outcome
// — generated, failed, or deliberately skipped (no text) — carrying the entity ID
// and the ENTITY_STATES SourceRevision that produced the record. It exists so the
// hop-1 readiness watermark can be completed at the true end of the two-hop pipeline
// (ADR-066 §3). sourceRevision==0 means "unknown" (a legacy record) and the
// completion is a no-op; ^uint64(0) is the max-rev drain used for an unreadable
// (corrupt) record whose revision cannot be recovered.
type TerminalCallback func(entityID string, sourceRevision uint64)

// WorkerMetrics provides metrics callbacks for embedding worker operations.
// This allows the worker to report metrics without direct dependency on prometheus.
type WorkerMetrics interface {
	// IncDedupHits increments the deduplication hits counter
	IncDedupHits()
	// IncFailed increments the failed embeddings counter
	IncFailed()
	// SetPending sets the current pending embeddings gauge
	SetPending(count float64)
	// IncContentResolveError counts a body fetch that FAILED after a store was
	// resolved (an infra fault: read error, deleted bucket) — distinct from the
	// component-side content_unresolved (no store wired at all). Preserves the
	// gh#414 diagnosability the ADR-063 resolver would otherwise blur (M1).
	IncContentResolveError()
	// IncContentResolved counts a body successfully fetched from a resolved store.
	// This is the POSITIVE observable for the ADR-063 H2 behavior change: offloaded
	// bodies that configs without a store-read port previously excluded now embed —
	// a rising value is that inclusion happening, not merely content_unresolved
	// falling (the cost-ledger "make the delta observable" discipline).
	IncContentResolved()
}

// StoreResolver resolves a StorageReference.StorageInstance to its live
// streaming store (ADR-063). *storeregistry.Registry satisfies it. The worker
// resolves per-fetch and never caches the returned handle — it is owned by the
// storage component, not the worker.
type StoreResolver interface {
	Streamable(instance string) (storage.StreamableStore, bool)
}

// Worker processes pending embedding requests asynchronously
type Worker struct {
	mu sync.RWMutex

	// Dependencies
	storage  *Storage
	embedder Embedder // HTTP or BM25 embedder

	// KV watching
	indexBucket jetstream.KeyValue
	watcher     jetstream.KeyWatcher

	// Content store for fetching body text from ObjectStore via streaming.
	// This is the OWNED fallback (built from a store-read port, closed by the
	// component). Federated resolution goes through storeResolver first.
	contentStore storage.StreamableStore

	// storeResolver resolves a StorageRef's StorageInstance to the live store
	// that owns it (ADR-063 shared registry). Primary path; per-fetch; the
	// resolved handle is BORROWED and never cached or closed by the worker.
	storeResolver StoreResolver

	// Callbacks
	onGenerated GeneratedCallback // Called when embedding is generated
	onTerminal  TerminalCallback  // Called at ANY terminal outcome (ADR-066 §3)

	// Metrics
	metrics WorkerMetrics // Optional metrics reporter

	// State
	started  bool
	stopping bool
	ctx      context.Context
	cancel   context.CancelFunc
	wg       sync.WaitGroup

	// Configuration
	workers          int // Number of concurrent workers
	maxSourceTextLen int // Max chars for source text (0 = unlimited)

	// Logger
	logger *slog.Logger
}

// NewWorker creates a new async embedding worker
func NewWorker(
	storage *Storage,
	embedder Embedder,
	indexBucket jetstream.KeyValue,
	logger *slog.Logger,
) *Worker {
	if logger == nil {
		logger = slog.Default()
	}

	return &Worker{
		storage:     storage,
		embedder:    embedder,
		indexBucket: indexBucket,
		workers:     5, // Default concurrent workers
		logger:      logger,
	}
}

// WithWorkers sets the number of concurrent workers.
//
// n is floored at 1: Start spawns exactly n goroutines to drain the KV watcher,
// so a zero or negative count is not "fewer workers", it is a component that
// silently consumes nothing while every health signal stays green. No caller
// ever wants that, so it is corrected here rather than at each call site
// (gh#620).
func (w *Worker) WithWorkers(n int) *Worker {
	w.workers = max(1, n)
	return w
}

// WithContentStore sets the OWNED fallback content store for streaming body text
// retrieval. Used only when the shared resolver cannot resolve a ref's
// StorageInstance (single-bucket / legacy store-read deploys). The component owns
// and closes this store.
func (w *Worker) WithContentStore(store storage.StreamableStore) *Worker {
	w.contentStore = store
	return w
}

// WithStoreResolver sets the shared store resolver (ADR-063). This is the primary
// content-fetch path: a StorageRef's StorageInstance is resolved to the live
// store that owns it, so the worker fetches offloaded bodies from ANY registered
// storage instance, not just one wired bucket. Resolved per-fetch; never cached.
func (w *Worker) WithStoreResolver(r StoreResolver) *Worker {
	w.storeResolver = r
	return w
}

// WithMaxSourceTextLen sets the maximum characters for source text used in
// embedding generation. Text beyond this limit is truncated at a word boundary.
// Default: 0 (unlimited). Recommended: 4000 for BM25, 8000 for neural.
func (w *Worker) WithMaxSourceTextLen(n int) *Worker {
	w.maxSourceTextLen = n
	return w
}

// WithOnGenerated sets a callback that is invoked when an embedding is generated.
// Use this to populate caches or trigger downstream processing.
func (w *Worker) WithOnGenerated(cb GeneratedCallback) *Worker {
	w.onGenerated = cb
	return w
}

// WithOnTerminal sets a callback invoked when a pending embedding reaches any
// terminal outcome (generated, failed, or no-text skip). Used to complete the
// hop-1 readiness watermark (ADR-066 §3).
func (w *Worker) WithOnTerminal(cb TerminalCallback) *Worker {
	w.onTerminal = cb
	return w
}

// WithMetrics sets the metrics reporter for observability.
func (w *Worker) WithMetrics(m WorkerMetrics) *Worker {
	w.metrics = m
	return w
}

// Start begins watching for pending embeddings and processing them
func (w *Worker) Start(ctx context.Context) error {
	w.mu.Lock()
	defer w.mu.Unlock()

	if w.started {
		return fmt.Errorf("embedding worker already started")
	}

	// Create context for the worker
	w.ctx, w.cancel = context.WithCancel(ctx)

	// Start KV watcher for EMBEDDING_INDEX
	watcher, err := w.indexBucket.WatchAll(w.ctx)
	if err != nil {
		w.cancel()
		return errs.WrapTransient(err, "Worker", "Start", "failed to create KV watcher")
	}
	w.watcher = watcher

	// Start worker goroutines
	for i := 0; i < w.workers; i++ {
		w.wg.Add(1)
		go func(workerID int) {
			defer w.wg.Done()
			w.processEmbeddings(workerID)
		}(i)
	}

	w.started = true
	w.logger.Info("Embedding worker started", "workers", w.workers)
	return nil
}

// Stop stops the embedding worker gracefully
func (w *Worker) Stop() error {
	w.mu.Lock()
	defer w.mu.Unlock()

	if !w.started {
		return nil
	}

	w.stopping = true

	// Cancel context to signal all goroutines to stop
	if w.cancel != nil {
		w.cancel()
	}

	// Stop the watcher
	if w.watcher != nil {
		if err := w.watcher.Stop(); err != nil {
			// Expected errors during shutdown: subscription already cleaned up or consumer deleted
			if !isExpectedShutdownError(err) {
				w.logger.Warn("KV watcher stop error", "error", err)
			}
		}
	}

	// Wait for all goroutines to finish
	w.wg.Wait()

	w.started = false
	w.logger.Info("Embedding worker stopped")
	return nil
}

// processEmbeddings watches for KV changes and processes pending embeddings
func (w *Worker) processEmbeddings(workerID int) {
	w.logger.Debug("Embedding worker goroutine started", "worker_id", workerID)

	for {
		select {
		case <-w.ctx.Done():
			w.logger.Debug("Embedding worker context cancelled", "worker_id", workerID)
			return

		case entry, ok := <-w.watcher.Updates():
			if !ok {
				w.logger.Debug("KV watcher updates channel closed", "worker_id", workerID)
				return
			}

			if entry == nil {
				continue
			}

			// Process if this is a new pending record or update to existing pending
			if entry.Operation() == jetstream.KeyValuePut {
				w.handleKVEntrySafe(entry, workerID)
			}
		}
	}
}

// handleKVEntrySafe processes one entry with panic isolation scoped to that entry.
//
// The recovery deliberately lives HERE and not around the for-loop in
// processEmbeddings. Wrapping the loop meant any panic unwound past it, returned
// from processEmbeddings, and permanently retired that worker goroutine — nothing
// respawns it. At the default 5 workers, five poison entries silently reduce the
// embedding pipeline to zero consumers, observable only as ADR-066 watermark lag
// well after the fact. Scoped per entry, a panic costs exactly the one entry.
//
// The entry is not retried: the panicking record keeps its pending status and the
// stack is logged for diagnosis. Re-driving an entry that just crashed the
// decoder would turn one poison record into a hot loop.
func (w *Worker) handleKVEntrySafe(entry jetstream.KeyValueEntry, workerID int) {
	defer func() {
		if r := recover(); r != nil {
			w.logger.Error("Embedding worker panic recovered; skipping entry",
				"worker_id", workerID,
				"entity_id", entry.Key(),
				"panic", r,
				"stack", string(debug.Stack()))
		}
	}()

	w.handleKVEntry(entry, workerID)
}

// handleKVEntry processes a KV entry to check if it needs embedding generation
func (w *Worker) handleKVEntry(entry jetstream.KeyValueEntry, workerID int) {
	// Parse the record to check status
	var record Record
	if err := json.Unmarshal(entry.Value(), &record); err != nil {
		// A corrupt record cannot yield its SourceRevision, so its hop-1 pending
		// entry can never be completed with an exact revision — and unlike a
		// deleted entity, no re-observation is guaranteed. Max-rev-drain the key so
		// one poison record cannot wedge the whole embedding.ready signal into
		// permanent degraded (ADR-066 §3 D3); loud, because it is not expected.
		w.logger.Warn("Failed to unmarshal embedding record; draining readiness watermark for key",
			"key", entry.Key(), "error", err)
		if w.onTerminal != nil {
			w.onTerminal(entry.Key(), ^uint64(0))
		}
		return
	}

	// Only process pending records. A re-delivered generated/failed record (from our
	// own hop-2 writes) lands here and must NOT fire the terminal callback — it was
	// already completed when it first transitioned. So the defer below is registered
	// AFTER this skip, never before it (ADR-066 §3).
	if record.Status != StatusPending {
		return
	}

	entityID := entry.Key()

	// Every path past this point is genuinely terminal — the pipeline never retries;
	// every error is a hard SaveFailed, no-text is a delete, success is SaveGenerated.
	// One deferred completion covers them all uniformly, closing the "missed a
	// terminal site" risk. sourceRevision==0 (legacy record) makes it a no-op.
	defer func() {
		if w.onTerminal != nil {
			w.onTerminal(entityID, record.SourceRevision)
		}
	}()

	w.logger.Debug("Processing pending embedding", "worker_id", workerID, "entity_id", entityID)

	// Get source text - either from record or from ObjectStore via StorageRef
	sourceText, err := w.getSourceText(&record)
	if err != nil {
		w.logger.Error("Failed to get source text", "entity_id", entityID, "error", err)
		w.markFailed(entityID, fmt.Sprintf("text extraction failed: %v", err))
		return
	}

	if sourceText == "" {
		w.logger.Debug("No source text found, skipping embedding", "entity_id", entityID)
		// Not a failure - just nothing to embed. Remove pending record.
		if err := w.storage.DeleteEmbedding(w.ctx, entityID); err != nil {
			w.logger.Debug("Failed to delete pending record for entity with no text", "entity_id", entityID, "error", err)
		}
		return
	}

	// Get or generate embedding vector
	vector, err := w.getOrGenerateEmbedding(entityID, sourceText, record.ContentHash)
	if err != nil {
		return // Error already logged and marked as failed
	}

	// Save and notify
	w.saveAndNotify(entityID, vector)
}

// getOrGenerateEmbedding returns an existing embedding via dedup or generates a new one.
//
// An empty contentHash means the producer could not derive a content-addressed
// key for this record and dedup is DISABLED for it (see
// queueEmbeddingWithStorageRef): generate unconditionally rather than key the
// durable dedup bucket on something that is not the content.
func (w *Worker) getOrGenerateEmbedding(entityID, sourceText, contentHash string) ([]float32, error) {
	// An embedder that cannot state its vector width takes no part in dedup, read
	// or write. DedupKey already withholds a key from an unresolved identity, but
	// that only covers records this process queued: a pending record replayed from
	// an earlier process carries a non-empty ContentHash which hop-2 reuses
	// verbatim. Consulting it would compare against a width of 0, and saving would
	// leave a record stamped 0 that can never match again in a bucket that is
	// never cleared.
	dedupEnabled := contentHash != "" && w.embedder.Dimensions() > 0

	// Check deduplication first
	if dedupEnabled {
		dedupRecord, err := w.storage.GetByContentHash(w.ctx, contentHash)
		if err != nil {
			w.logger.Error("Failed to check dedup", "entity_id", entityID, "error", err)
			w.markFailed(entityID, fmt.Sprintf("dedup check failed: %v", err))
			return nil, err
		}

		if dedupRecord != nil && w.dedupRecordUsable(entityID, contentHash, dedupRecord) {
			w.logger.Debug("Deduplicating embedding", "entity_id", entityID, "content_hash", contentHash)
			if w.metrics != nil {
				w.metrics.IncDedupHits()
			}
			return dedupRecord.Vector, nil
		}
	}

	// Generate new embedding
	w.logger.Debug("Generating new embedding", "entity_id", entityID)
	vectors, err := w.embedder.Generate(w.ctx, []string{sourceText})
	if err != nil {
		w.logger.Error("Failed to generate embedding", "entity_id", entityID, "error", err)
		w.markFailed(entityID, fmt.Sprintf("generation failed: %v", err))
		return nil, err
	}

	if len(vectors) == 0 {
		w.logger.Error("No embedding generated", "entity_id", entityID)
		w.markFailed(entityID, "no embedding returned")
		return nil, fmt.Errorf("no embedding returned")
	}

	vector := vectors[0]

	// Save to dedup bucket. Skipped when dedup is disabled for this record.
	if dedupEnabled {
		if err := w.storage.SaveDedup(
			w.ctx, contentHash, vector, entityID, w.embedder.Model(), w.embedder.Dimensions(),
		); err != nil {
			w.logger.Warn("Failed to save dedup record", "entity_id", entityID, "error", err)
			// Continue anyway - not critical
		}
	}

	return vector, nil
}

// dedupRecordUsable reports whether a dedup hit belongs to the vector space this
// worker's embedder produces.
//
// The dedup KEY already folds in embedder identity (gh#612), so a mismatch here
// means state written by an incompatible key layout survived — a real hit on
// this branch is a bug, not a config change, hence the loud log.
//
// A record with no Model is UNUSABLE, not usable. Old-layout keys remain
// reachable: EMBEDDING_INDEX pending records are durable KV with no TTL, and
// Worker.Start uses WatchAll, which re-delivers every current value on restart.
// handleKVEntry skips only non-pending records, so a pending record written by a
// pre-fix binary replays as pending and hop-2 reuses its stored ContentHash — an
// old-layout ContentHash(text) key — verbatim. Trusting an identityless record
// there is precisely the bm25-vector-stamped-with-a-neural-model-name defect
// gh#612 exists to prevent, on the bm25 -> http upgrade path that motivated it.
// An identityless record predates the key contract, so it is regenerated: one
// re-embed per legacy record, once.
func (w *Worker) dedupRecordUsable(entityID, contentHash string, r *DedupRecord) bool {
	if r.Model == "" {
		w.logger.Warn("Dedup record predates the embedder-identity key contract; regenerating",
			"entity_id", entityID, "content_hash", contentHash)
		return false
	}
	model, dims := w.embedder.Model(), w.embedder.Dimensions()
	if r.Model == model && r.Dimensions == dims {
		return true
	}
	w.logger.Warn("Dedup record belongs to a different vector space; regenerating",
		"entity_id", entityID, "content_hash", contentHash,
		"record_model", r.Model, "record_dimensions", r.Dimensions,
		"embedder_model", model, "embedder_dimensions", dims)
	return false
}

// saveAndNotify saves the generated embedding and notifies callback.
func (w *Worker) saveAndNotify(entityID string, vector []float32) {
	dimensions := len(vector)
	model := w.embedder.Model()
	if err := w.storage.SaveGenerated(w.ctx, entityID, vector, model, dimensions); err != nil {
		// The entity was tombstoned (or its pending record removed) while this
		// vector was being generated. That is a normal race, not a failure: there
		// is nothing to mark failed, and counting it would inflate the failure
		// metric across gh#527 bulk deletion. Return before onGenerated — firing it
		// would push the dropped vector into the query-side cache, resurrecting in
		// memory exactly what gh#614 removed from KV.
		if errors.Is(err, ErrRecordGone) {
			w.logger.Debug("Embedding record removed during generation; dropping vector",
				"entity_id", entityID)
			return
		}
		w.logger.Error("Failed to save generated embedding", "entity_id", entityID, "error", err)
		w.markFailed(entityID, fmt.Sprintf("save failed: %v", err))
		return
	}

	w.logger.Debug("Embedding generated successfully", "entity_id", entityID, "dimensions", dimensions)

	if w.onGenerated != nil {
		w.onGenerated(entityID, vector)
	}
}

// getSourceText extracts text from the record.
// For legacy records, uses SourceText directly.
// For ContentStorable records (with StorageRef), fetches from ObjectStore.
func (w *Worker) getSourceText(record *Record) (string, error) {
	var text string

	// Legacy path: use SourceText if available
	if record.SourceText != "" {
		text = record.SourceText
	} else if record.StorageRef != nil {
		// Streaming path: read raw content from store
		var err error
		text, err = w.fetchTextFromStorage(record.StorageRef)
		if err != nil {
			return "", err
		}
	}

	// Truncate if configured
	if w.maxSourceTextLen > 0 && len(text) > w.maxSourceTextLen {
		text = truncateAtWord(text, w.maxSourceTextLen)
	}

	return text, nil
}

// truncateAtWord truncates text at the last word boundary before maxLen.
func truncateAtWord(text string, maxLen int) string {
	if len(text) <= maxLen {
		return text
	}
	// Find last space before maxLen
	truncated := text[:maxLen]
	lastSpace := strings.LastIndex(truncated, " ")
	if lastSpace > maxLen/2 { // Only use word boundary if it's not too far back
		return truncated[:lastSpace]
	}
	return truncated
}

// resolveStore returns the store that backs a StorageInstance: the shared
// registry first (federated — resolves ANY registered storage instance), then
// the worker's OWNED fallback store (single wired store-read bucket). Resolves
// per-fetch and returns a BORROWED handle from the registry path — callers must
// not close it. Returns nil when neither path can serve the instance.
func (w *Worker) resolveStore(instance string) storage.StreamableStore {
	if w.storeResolver != nil && instance != "" {
		if s, ok := w.storeResolver.Streamable(instance); ok {
			return s
		}
	}
	return w.contentStore // owned fallback (may be nil)
}

// fetchTextFromStorage streams raw content from the store, reading only up to
// maxSourceTextLen bytes. ObjectStore holds raw bytes (plain text, not JSON-wrapped).
// Triples carry metadata (mime type, hash); the store is format-agnostic.
func (w *Worker) fetchTextFromStorage(ref *StorageRef) (string, error) {
	store := w.resolveStore(ref.StorageInstance)
	if store == nil {
		return "", fmt.Errorf("content store not configured")
	}

	reader, err := store.Open(w.ctx, ref.Key)
	if err != nil {
		// A store resolved but the read failed: an infra fault, not a wiring gap.
		// Count it distinctly so it is not confused with content_unresolved (M1).
		if w.metrics != nil {
			w.metrics.IncContentResolveError()
		}
		return "", fmt.Errorf("failed to open content from instance %q: %w", ref.StorageInstance, err)
	}
	defer reader.Close()

	// Read only what we need — no full memory load
	limit := w.maxSourceTextLen
	if limit <= 0 {
		limit = defaultMaxSourceTextLen
	}
	data, err := io.ReadAll(io.LimitReader(reader, int64(limit)))
	if err != nil {
		if w.metrics != nil {
			w.metrics.IncContentResolveError()
		}
		return "", fmt.Errorf("failed to read content from instance %q: %w", ref.StorageInstance, err)
	}

	// Detect likely JSON-wrapped content (StoredContent envelope).
	// Raw text is expected — if it starts with '{', someone probably used
	// StoreContent() instead of Put(). Embeddings will include JSON noise.
	if len(data) > 0 && data[0] == '{' {
		w.logger.Debug("stored content appears JSON-wrapped, expected raw text",
			slog.String("key", ref.Key),
			slog.String("hint", "use Put() for raw body text, not StoreContent()"))
	}

	// Positive observable for the ADR-063 H2 inclusion: an offloaded body was
	// resolved and fetched (previously excluded where no store-read was wired).
	if w.metrics != nil {
		w.metrics.IncContentResolved()
	}

	return string(data), nil
}

// markFailed marks an embedding as failed
func (w *Worker) markFailed(entityID, errorMsg string) {
	// Don't count context cancellation (shutdown) as a failure
	if strings.Contains(errorMsg, "context canceled") {
		w.logger.Debug("Skipping failure metric for context cancellation", "entity_id", entityID)
		return
	}

	if err := w.storage.SaveFailed(w.ctx, entityID, errorMsg); err != nil {
		// A record that is already gone has nothing to annotate. The failure itself
		// is still real and still counted below — only the "could not mark it" log
		// is downgraded, so a tombstone race does not read as an operational fault.
		if errors.Is(err, ErrRecordGone) {
			w.logger.Debug("Embedding record removed before failure could be recorded",
				"entity_id", entityID)
		} else {
			w.logger.Error("Failed to mark embedding as failed", "entity_id", entityID, "error", err)
		}
	}
	if w.metrics != nil {
		w.metrics.IncFailed()
	}
}

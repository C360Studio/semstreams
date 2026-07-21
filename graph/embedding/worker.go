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
	// IncDedupSkipped counts an embedding generated on a condition where the durable
	// dedup bucket was NOT consulted (currently: an embedder whose vector width is
	// unresolved, so no content-addressed key can be derived). It makes the
	// avoided-reuse cost visible rather than inferred (#623): the offloaded-lane
	// re-embed cost Track 0 measured, and, post-fix, its recovery.
	IncDedupSkipped(reason string)
	// IncTruncated counts one source-text truncation at the configured cap, so the
	// bytes actually embedded are discoverable rather than silently dropped (#602).
	IncTruncated()
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
	workers          int    // Number of concurrent workers
	maxSourceTextLen int    // Max chars for source text (0 = unlimited)
	embedderType     string // "bm25" / "http" — the Type axis of the dedup key identity

	// Logger
	logger *slog.Logger
}

// dedupSkipReasonIdentityUnresolved labels a dedup skip caused by an embedder that
// has not yet resolved its vector width (Dimensions() == 0), so DedupKey withholds a
// key. It is the only skip condition after the hop-2 key move restored the offloaded
// lane's dedup.
const dedupSkipReasonIdentityUnresolved = "identity_unresolved"

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

// WithEmbedderType sets the Type axis of the dedup-key identity ("bm25" / "http").
// Hop 2 folds it (with the embedder's live Model/Dimensions and the text cap) into
// the dedup key so a config that flips embedder type cannot serve a vector from the
// prior vector space (gh#612). The worker derives the key itself now, so this is the
// one identity field it cannot read from the Embedder interface.
func (w *Worker) WithEmbedderType(t string) *Worker {
	w.embedderType = t
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

// handleKVEntrySafe processes one entry with panic isolation scoped to that entry,
// and is the SINGLE site that fires the terminal callback.
//
// The recovery deliberately lives HERE and not around the for-loop in
// processEmbeddings. Wrapping the loop meant any panic unwound past it, returned
// from processEmbeddings, and permanently retired that worker goroutine — nothing
// respawns it. At the default 5 workers, five poison entries silently reduce the
// embedding pipeline to zero consumers, observable only as ADR-066 watermark lag
// well after the fact. Scoped per entry, a panic costs exactly the one entry.
//
// The completion fires only on a NORMAL return from handleKVEntry, and that is why
// it cannot be a defer INSIDE handleKVEntry: Go runs deferred functions during
// panic unwinding, so such a defer executes BEFORE the recover() here and drains
// the ADR-066 watermark for a record that is still durably pending — nothing on the
// panic path calls markFailed or SaveGenerated. The watermark would then report
// caught up over stranded work.
//
// That completion defer predates the per-entry recovery, but the recovery changed
// its character: a panic used to kill the worker goroutine, so the watermark
// stalled and readiness degraded — loud, and capped at 5 occurrences. With the
// goroutine surviving, every panic would instead advance the watermark silently and
// without limit. Skipping completion keeps the record and the watermark telling the
// same story: the entry stays pending, the low-water floor stays behind it, and the
// stuck detector degrades truthfully.
//
// The entry is not retried: the panicking record keeps its pending status and the
// stack is logged for diagnosis. Re-driving an entry that just crashed the
// decoder would turn one poison record into a hot loop. A record that panics on
// every delivery therefore pins the watermark until an operator removes it —
// intended, per ADR-084: readiness licenses health, not absence.
func (w *Worker) handleKVEntrySafe(entry jetstream.KeyValueEntry, workerID int) {
	defer func() {
		if r := recover(); r != nil {
			w.logger.Error("Embedding worker panic recovered; entry stays pending and its readiness watermark is NOT advanced",
				"worker_id", workerID,
				"entity_id", entry.Key(),
				"panic", r,
				"stack", string(debug.Stack()))
		}
	}()

	entityID, sourceRevision, terminal := w.handleKVEntry(entry, workerID)
	if terminal && w.onTerminal != nil {
		w.onTerminal(entityID, sourceRevision)
	}
}

// handleKVEntry processes a KV entry to check if it needs embedding generation.
//
// It completes nothing itself; it REPORTS the outcome. terminal==true means this
// call carried the record to an outcome that must drain the hop-1 readiness
// watermark (ADR-066 §3) for entityID at sourceRevision. handleKVEntrySafe fires
// the one callback, and only on a normal return — see its doc comment for why a
// defer here would complete panicking (still-pending) work.
func (w *Worker) handleKVEntry(
	entry jetstream.KeyValueEntry, workerID int,
) (entityID string, sourceRevision uint64, terminal bool) {
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
		return entry.Key(), ^uint64(0), true
	}

	// Only process pending records. A re-delivered generated/failed record (from our
	// own hop-2 writes) lands here and must NOT fire the terminal callback — it was
	// already completed when it first transitioned. So the results below are set
	// AFTER this skip, never before it (ADR-066 §3).
	if record.Status != StatusPending {
		return "", 0, false
	}

	// Every path past this point is genuinely terminal — the pipeline never retries;
	// every error is a hard SaveFailed, no-text is a delete, success is SaveGenerated.
	// Setting the named results ONCE here means every `return` below reports the same
	// completion, closing the "missed a terminal site" risk that the old defer closed
	// — without also completing on the panic path. sourceRevision==0 (a legacy
	// record) makes the completion a no-op.
	entityID, sourceRevision, terminal = entry.Key(), record.SourceRevision, true

	w.logger.Debug("Processing pending embedding", "worker_id", workerID, "entity_id", entityID)

	// Get source text - either from record or from ObjectStore via StorageRef
	sourceText, err := w.getSourceText(&record)
	if err != nil {
		w.logger.Error("Failed to get source text", "entity_id", entityID, "error", err)
		w.markFailed(entityID, fmt.Sprintf("text extraction failed: %v", err), sourceRevision)
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

	// Derive the dedup key HERE, in hop 2, over the exact bytes that get embedded —
	// the resolved and truncated body. This is the single derivation site (#623): it
	// keys the inline and offloaded lanes identically, restoring dedup on the
	// offloaded lane that hop 1 could not key (hop 1 holds only a storage address,
	// not the body), and it folds in the effective text cap for free because the key
	// is over the TRUNCATED text. The hop-1 record.ContentHash is no longer consulted.
	dedupKey := DedupKey(w.embedderIdentity(), sourceText)

	// Get or generate embedding vector
	vector, err := w.getOrGenerateEmbedding(entityID, sourceText, dedupKey, sourceRevision)
	if err != nil {
		return // Error already logged and marked as failed
	}

	// Save and notify
	w.saveAndNotify(entityID, vector, dedupKey, sourceRevision)
	return
}

// embedderIdentity captures everything the dedup key depends on besides the text:
// the embedder Type, its live Model and Dimensions, and the effective text cap. It
// reads Model/Dimensions from the Embedder on each call rather than snapshotting
// them, because HTTPEmbedder resolves its width lazily from the first response and
// the value is documented safe to read concurrently from any worker goroutine (see
// Embedder.Dimensions). MaxTextLen is included so a cap change (which changes WHICH
// bytes are embedded) changes the key.
func (w *Worker) embedderIdentity() EmbedderIdentity {
	id := EmbedderIdentity{
		Type:       w.embedderType,
		MaxTextLen: w.maxSourceTextLen,
	}
	if w.embedder != nil {
		id.Model = w.embedder.Model()
		id.Dimensions = w.embedder.Dimensions()
	}
	return id
}

// getOrGenerateEmbedding returns an existing embedding via dedup or generates a new one.
//
// key is the hop-2 dedup key derived over the resolved+truncated text. An empty key
// means the embedder cannot state its vector width yet (DedupKey withholds a key from
// an unresolved identity), so no content-addressed key exists: dedup is skipped and
// the skip is COUNTED (IncDedupSkipped) so the avoided reuse is visible, not inferred
// (#623). Both hops now derive the key here, so a replayed pending record's stale
// hop-1 ContentHash is never consulted.
func (w *Worker) getOrGenerateEmbedding(entityID, sourceText, key string, sourceRevision uint64) ([]float32, error) {
	dedupEnabled := key != ""

	// Check deduplication first
	if dedupEnabled {
		dedupRecord, err := w.storage.GetByContentHash(w.ctx, key)
		if err != nil {
			w.logger.Error("Failed to check dedup", "entity_id", entityID, "error", err)
			w.markFailed(entityID, fmt.Sprintf("dedup check failed: %v", err), sourceRevision)
			return nil, err
		}

		if dedupRecord != nil && w.dedupRecordUsable(entityID, key, dedupRecord) {
			w.logger.Debug("Deduplicating embedding", "entity_id", entityID, "dedup_key", key)
			if w.metrics != nil {
				w.metrics.IncDedupHits()
			}
			return dedupRecord.Vector, nil
		}
	} else if w.metrics != nil {
		w.metrics.IncDedupSkipped(dedupSkipReasonIdentityUnresolved)
	}

	// Generate new embedding
	w.logger.Debug("Generating new embedding", "entity_id", entityID)
	vectors, err := w.embedder.Generate(w.ctx, []string{sourceText})
	if err != nil {
		w.logger.Error("Failed to generate embedding", "entity_id", entityID, "error", err)
		w.markFailed(entityID, fmt.Sprintf("generation failed: %v", err), sourceRevision)
		return nil, err
	}

	if len(vectors) == 0 {
		w.logger.Error("No embedding generated", "entity_id", entityID)
		w.markFailed(entityID, "no embedding returned", sourceRevision)
		return nil, fmt.Errorf("no embedding returned")
	}

	vector := vectors[0]

	// Save to dedup bucket. Skipped when no key could be derived for this record.
	if dedupEnabled {
		if err := w.storage.SaveDedup(
			w.ctx, key, vector, entityID, w.embedder.Model(), w.embedder.Dimensions(),
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
// dedupRecordUsable is defense-in-depth: hop 2 now derives the dedup key itself
// (DedupKey folds embedder identity + cap), so a returned DedupRecord's identity
// should already match by construction. This re-checks the record's Model and
// Dimensions anyway, and rejects an identityless (legacy) record — a bm25 vector
// stamped with a neural model's name is precisely the gh#612 defect on the
// bm25 -> http upgrade path. A rejected record is regenerated: one re-embed per
// legacy record, once.
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

// saveAndNotify saves the generated embedding and notifies callback. contentHash is
// the hop-2 dedup key of the embedded bytes and sourceRevision is the revision this
// generation completes; both are stored on the record so its ContentHash+Vector
// stay consistent and a late older-revision write is dropped (#614 part 2).
func (w *Worker) saveAndNotify(entityID string, vector []float32, contentHash string, sourceRevision uint64) {
	dimensions := len(vector)
	model := w.embedder.Model()
	if err := w.storage.SaveGenerated(w.ctx, entityID, vector, model, dimensions, contentHash, sourceRevision); err != nil {
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
		// A newer source revision's vector already landed. The vector THIS call holds
		// is the older one; firing onGenerated would push it into a WithOnGenerated
		// consumer's cache, exactly the stale-vector hazard ErrRecordGone guards
		// against. Skip the callback; the newer write is authoritative.
		if errors.Is(err, ErrSupersededRevision) {
			w.logger.Debug("Embedding superseded by a newer revision; dropping vector",
				"entity_id", entityID)
			return
		}
		w.logger.Error("Failed to save generated embedding", "entity_id", entityID, "error", err)
		w.markFailed(entityID, fmt.Sprintf("save failed: %v", err), sourceRevision)
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

	// Truncate if configured. Emit a signal so the bytes actually embedded are
	// discoverable rather than silently dropped (#602) — the cap is part of what the
	// vector depends on, so silent truncation hides that dependence.
	if w.maxSourceTextLen > 0 && len(text) > w.maxSourceTextLen {
		text = truncateAtWord(text, w.maxSourceTextLen)
		if w.metrics != nil {
			w.metrics.IncTruncated()
		}
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

	// Read only what we need — no full memory load. Read one byte PAST the cap so a
	// body that exceeds the cap is detectable: len(data) > limit means the store had
	// more and this fetch truncated it. The offloaded lane truncates here (byte-cut
	// via LimitReader), NOT at getSourceText's word-cut branch, so this is the only
	// place offloaded truncation can be observed — without it, truncation of the
	// primary (ContentStorable) lane is silent, which the #602 spec forbids.
	limit := w.maxSourceTextLen
	if limit <= 0 {
		limit = defaultMaxSourceTextLen
	}
	data, err := io.ReadAll(io.LimitReader(reader, int64(limit)+1))
	if err != nil {
		if w.metrics != nil {
			w.metrics.IncContentResolveError()
		}
		return "", fmt.Errorf("failed to read content from instance %q: %w", ref.StorageInstance, err)
	}
	if len(data) > limit {
		data = data[:limit]
		if w.metrics != nil {
			w.metrics.IncTruncated()
		}
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

// markFailed marks an embedding as failed. sourceRevision is the revision this
// failure completes, so a stale older-revision failure cannot clobber a newer
// success under the storage ordering guard (#614 part 2).
func (w *Worker) markFailed(entityID, errorMsg string, sourceRevision uint64) {
	// Don't count context cancellation (shutdown) as a failure
	if strings.Contains(errorMsg, "context canceled") {
		w.logger.Debug("Skipping failure metric for context cancellation", "entity_id", entityID)
		return
	}

	if err := w.storage.SaveFailed(w.ctx, entityID, errorMsg, sourceRevision); err != nil {
		// A newer source revision already resolved this entity; this older failure is
		// moot. Do NOT count it — the entity is not in a failed state — and return
		// before IncFailed so a superseded older revision cannot inflate the failure
		// gauge.
		if errors.Is(err, ErrSupersededRevision) {
			w.logger.Debug("Embedding failure superseded by a newer revision; not counting",
				"entity_id", entityID)
			return
		}
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

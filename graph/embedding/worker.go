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
	"unicode"
	"unicode/utf8"

	"github.com/nats-io/nats.go"
	"github.com/nats-io/nats.go/jetstream"

	"github.com/c360studio/semstreams/pkg/errs"
	"github.com/c360studio/semstreams/storage"
)

// defaultMaxSourceTextLen is the safety cap for streaming content reads when
// maxSourceTextLen is 0 (unconfigured). Prevents unbounded memory allocation
// for very large stored content.
const defaultMaxSourceTextLen = 8000

// identityBodySeparator joins an offloaded entity's inline identity text (title/
// .signature/.comment) to its resolved body, identity-first, in getSourceText (D1).
//
// It is FROZEN. The separator is part of the bytes actually embedded, and the hop-2
// dedup key (DedupKey over getSourceText's output) is derived over exactly those
// bytes — so changing this constant re-keys EVERY offloaded entity that carries
// identity text and forces a full re-embed of that lane. Do not alter it without
// treating it as a breaking, re-embed-inducing change.
const identityBodySeparator = "\n\n"

// MaxSourceTextLenCeiling is the hard upper bound on the source-text cap, in runes.
// Config.Validate rejects a larger max_text_len, and fetchTextFromStorage clamps to
// it before deriving its byte read budget so a pathological value can never overflow
// utf8.UTFMax*limit+1 into a negative io.LimitReader bound (which reads an empty body
// that hop 2 would then treat as "no source text" and DELETE the pending embedding —
// #628 FIX 2). 1_000_000 characters is far past any real embedding input (neural
// context caps are ~8k) while keeping the worst-case offloaded read bounded at
// utf8.UTFMax MB.
const MaxSourceTextLenCeiling = 1_000_000

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
	// IncOffloadedIdentityIncluded counts one offloaded (StorageRef) entity that
	// embedded inline identity text (title/.signature/.comment, per text_suffixes)
	// AHEAD of its body. Paired with IncOffloadedIdentityAbsent, it makes the
	// text-suffix effect on the offloaded lane observable — a producer tuning
	// text_suffixes can confirm it took effect from /metrics rather than infer it
	// from silence (D5/#601).
	IncOffloadedIdentityIncluded()
	// IncOffloadedIdentityAbsent counts one offloaded (StorageRef) entity processed
	// WITHOUT inline identity text — only its body, if any, is embedded; when it also
	// has no body the entity is skipped without embedding. The symmetric half of
	// IncOffloadedIdentityIncluded, so a producer can tell a config-effect from silence
	// (D5/#601).
	IncOffloadedIdentityAbsent()
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
	vector, wasDedupHit, err := w.getOrGenerateEmbedding(entityID, sourceText, dedupKey, sourceRevision)
	if err != nil {
		return // Error already logged and marked as failed
	}

	// Save and notify. saveAndNotify reports whether the outcome is terminal: a
	// non-converging revision CAS (ErrCASExhausted) leaves the record pending and
	// re-drivable, so it must NOT drain the readiness watermark (ADR-066 §3). Every
	// other outcome is terminal (already set above). wasDedupHit is counted there,
	// on the success path only, so dedup_hits stays a subset of resolutions.
	terminal = w.saveAndNotify(entityID, vector, dedupKey, sourceRevision, wasDedupHit)
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
//
// The returned bool reports whether the vector came from a dedup HIT. The caller
// counts IncDedupHits only once the resolution actually STORES (in saveAndNotify's
// success path), never here: a hit whose save is then dropped as superseded/gone
// does not fire onGenerated, so counting the hit eagerly here would let dedup_hits
// exceed embeddings_generated_total — the exact invariant the e2e queue-health gate
// enforces (dedup hits are a subset of resolutions).
func (w *Worker) getOrGenerateEmbedding(entityID, sourceText, key string, sourceRevision uint64) (vec []float32, wasDedupHit bool, err error) {
	dedupEnabled := key != ""

	// Check deduplication first
	if dedupEnabled {
		dedupRecord, derr := w.storage.GetByContentHash(w.ctx, key)
		if derr != nil {
			w.logger.Error("Failed to check dedup", "entity_id", entityID, "error", derr)
			w.markFailed(entityID, fmt.Sprintf("dedup check failed: %v", derr), sourceRevision)
			return nil, false, derr
		}

		if dedupRecord != nil && w.dedupRecordUsable(entityID, key, dedupRecord) {
			w.logger.Debug("Deduplicating embedding", "entity_id", entityID, "dedup_key", key)
			return dedupRecord.Vector, true, nil
		}
	} else if w.metrics != nil {
		w.metrics.IncDedupSkipped(dedupSkipReasonIdentityUnresolved)
	}

	// Generate new embedding
	w.logger.Debug("Generating new embedding", "entity_id", entityID)
	vectors, gerr := w.embedder.Generate(w.ctx, []string{sourceText})
	if gerr != nil {
		w.logger.Error("Failed to generate embedding", "entity_id", entityID, "error", gerr)
		w.markFailed(entityID, fmt.Sprintf("generation failed: %v", gerr), sourceRevision)
		return nil, false, gerr
	}

	if len(vectors) == 0 {
		w.logger.Error("No embedding generated", "entity_id", entityID)
		w.markFailed(entityID, "no embedding returned", sourceRevision)
		return nil, false, fmt.Errorf("no embedding returned")
	}

	vector := vectors[0]

	// Save to dedup bucket. Skipped when no key could be derived for this record.
	if dedupEnabled {
		if serr := w.storage.SaveDedup(
			w.ctx, key, vector, entityID, w.embedder.Model(), w.embedder.Dimensions(),
		); serr != nil {
			w.logger.Warn("Failed to save dedup record", "entity_id", entityID, "error", serr)
			// Continue anyway - not critical
		}
	}

	return vector, false, nil
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
//
// It returns terminal==false ONLY for a re-drivable transient (ErrCASExhausted): the
// record stays pending and the ENTITY_STATES watcher will re-deliver the entity on its
// next write, so handleKVEntry must NOT advance the readiness watermark past this
// revision (that would report the pipeline caught up over still-pending work). Every
// other outcome — success, a superseded/record-gone drop, or a real save failure —
// is terminal.
func (w *Worker) saveAndNotify(entityID string, vector []float32, contentHash string, sourceRevision uint64, wasDedupHit bool) (terminal bool) {
	dimensions := len(vector)
	model := w.embedder.Model()
	if err := w.storage.SaveGenerated(w.ctx, entityID, vector, model, dimensions, contentHash, sourceRevision); err != nil {
		// The entity was tombstoned (or its pending record removed) while this
		// vector was being generated. That is a normal race, not a failure: there
		// is nothing to mark failed, and counting it would inflate the failure
		// metric across gh#527 bulk deletion. Return before onGenerated — firing it
		// would push the dropped vector into the query-side cache, resurrecting in
		// memory exactly what gh#614 removed from KV. Terminal: the entity is
		// deliberately without an embedding now.
		if errors.Is(err, ErrRecordGone) {
			w.logger.Debug("Embedding record removed during generation; dropping vector",
				"entity_id", entityID)
			return true
		}
		// A newer source revision's vector already landed. The vector THIS call holds
		// is the older one; firing onGenerated would push it into a WithOnGenerated
		// consumer's cache, exactly the stale-vector hazard ErrRecordGone guards
		// against. Skip the callback; the newer write is authoritative. Terminal: the
		// newer outcome already completed this entity.
		if errors.Is(err, ErrSupersededRevision) {
			w.logger.Debug("Embedding superseded by a newer revision; dropping vector",
				"entity_id", entityID)
			return true
		}
		// The revision CAS loop did not converge. This is a documented TRANSIENT,
		// re-drivable condition (effectively unreachable under the SourceRevision
		// ordering guard) — NOT a generation failure. Do NOT SaveFailed and do NOT
		// count IncFailed: a failure write here can later land at the SAME revision and
		// flip a good vector to StatusFailed, and advancing the watermark would report
		// this entity done over a record that is still durably pending. Leave it
		// pending and let the ENTITY_STATES watcher re-deliver it (the natural
		// re-drive); return terminal==false so readiness does not advance past it.
		if errors.Is(err, ErrCASExhausted) {
			w.logger.Warn("Embedding save did not converge under revision CAS; leaving record pending for re-drive",
				"entity_id", entityID)
			return false
		}
		w.logger.Error("Failed to save generated embedding", "entity_id", entityID, "error", err)
		w.markFailed(entityID, fmt.Sprintf("save failed: %v", err), sourceRevision)
		return true
	}

	w.logger.Debug("Embedding generated successfully", "entity_id", entityID, "dimensions", dimensions)

	// The resolution STORED. Count the dedup hit here (not at lookup time) so a hit
	// whose save was dropped as superseded/gone/CAS-exhausted — which skips
	// onGenerated — never inflates dedup_hits above embeddings_generated_total. Both
	// counters fire together on success, keeping dedup_hits a strict subset of
	// resolutions (the e2e queue-health invariant).
	if wasDedupHit && w.metrics != nil {
		w.metrics.IncDedupHits()
	}

	if w.onGenerated != nil {
		w.onGenerated(entityID, vector)
	}
	return true
}

// getSourceText produces the exact bytes hop 2 embeds (and keys the dedup bucket over).
//
// It is re-branched on StorageRef PRIMARY (D1). The discriminator for "this body is
// offloaded" is StorageRef != nil, NOT SourceText: on an offloaded record SourceText
// now means the INLINE identity prefix (title/.signature/.comment), not the whole
// text. Were the old SourceText-primary `else if` kept, an offloaded record carrying
// identity text would take the first branch and embed identity-ONLY, silently dropping
// the body. So:
//
//   - Offloaded (StorageRef != nil): fetch the body, then embed the inline identity
//     text AHEAD of it (identity-first, one vector) when present, so text_suffixes
//     takes effect on offloaded entities exactly as it does inline (D1/D2). Identity
//     first means the cap below trims the body tail and the identity always survives.
//   - Inline (StorageRef == nil): SourceText is the whole text (unchanged).
func (w *Worker) getSourceText(record *Record) (string, error) {
	if record.StorageRef != nil {
		// Streaming path: read raw content from store. fetchTextFromStorage rune-clamps
		// the body to the cap for memory safety, reports whether IT truncated (bodyTruncated),
		// and counts that body truncation once (#602).
		body, bodyTruncated, err := w.fetchTextFromStorage(record.StorageRef)
		if err != nil {
			return "", err
		}

		// Identity-first (D1/D2/FIX 2). Join with the separator ONLY when both an identity
		// and a body are present: an identity with an empty body embeds the identity ALONE
		// (no trailing separator), so it dedup-collapses against an inline entity whose text
		// is that same identity, and carries no cosmetic separator noise.
		text := body
		switch {
		case record.SourceText != "" && body != "":
			text = record.SourceText + identityBodySeparator + body
		case record.SourceText != "":
			text = record.SourceText // empty body: identity alone
		}
		if record.SourceText != "" {
			if w.metrics != nil {
				w.metrics.IncOffloadedIdentityIncluded()
			}
		} else if w.metrics != nil {
			w.metrics.IncOffloadedIdentityAbsent()
		}

		// Re-apply the cap to the COMBINED text (D3): identity-first ordering means the body
		// tail trims and the identity always survives. The hop-2 dedup key (DedupKey over
		// this returned text) therefore covers the combined, truncated bytes automatically —
		// no separate key derivation (D4).
		//
		// Count the truncation EXACTLY ONCE across the two clamps (#602). fetchTextFromStorage
		// already counted iff the BODY exceeded the cap (bodyTruncated). Here we count only
		// the ADDITIONAL case it could not see: the body fit the cap but prepending the
		// identity pushes the COMBINED over it (!bodyTruncated && the combined shortened).
		// So body-over-cap counts once at fetch; identity-tips-over counts once here; neither
		// double-counts.
		if w.maxSourceTextLen > 0 {
			truncated := truncateAtWord(text, w.maxSourceTextLen)
			if !bodyTruncated && len(truncated) < len(text) {
				if w.metrics != nil {
					w.metrics.IncTruncated()
				}
			}
			text = truncated
		}
		return text, nil
	}

	// Inline lane: SourceText is the whole text. Truncate at the cap through the SAME
	// rune-safe routine the offloaded lane uses, so identical content embeds byte-identical
	// text (and derives the same hop-2 dedup key) regardless of lane. This is the inline
	// lane's single truncation-detection site — emit a signal only when it actually
	// shortens the text so the bytes embedded stay discoverable (#602).
	text := record.SourceText
	if w.maxSourceTextLen > 0 {
		truncated := truncateAtWord(text, w.maxSourceTextLen)
		if len(truncated) < len(text) {
			if w.metrics != nil {
				w.metrics.IncTruncated()
			}
		}
		text = truncated
	}

	return text, nil
}

// truncateAtWord truncates text to at most maxLen RUNES (characters), preferring the
// last Unicode-whitespace boundary in the back half of the cap.
//
// It is the single truncation routine for BOTH the inline and the offloaded
// (ContentStorable) lane, so identical over-cap content yields a byte-identical result
// — and therefore the same hop-2 dedup key — no matter which lane delivered it. That
// lane-independence is what makes gh#627 (cross-lane dedup miss) moot for content that
// fits after truncation.
//
// It is rune-safe in the two senses the previous byte-slicing version was not:
//   - it counts and cuts on RUNES, matching Config.MaxTextLen's documented unit
//     ("characters") and the operator mental model, and
//   - it never splits a multibyte rune, so the result is always valid UTF-8 and the
//     bytes DedupKey hashes are exactly the bytes the embedder receives.
//
// The word boundary uses unicode.IsSpace (any Unicode whitespace), not a literal ASCII
// space, so non-breaking and ideographic spaces are honored too.
func truncateAtWord(text string, maxLen int) string {
	if maxLen <= 0 {
		return text
	}

	// Find the byte offset just past the maxLen-th rune. Ranging a string yields the
	// START byte index of each rune, so cut is always on a rune boundary.
	runeCount := 0
	cut := len(text)
	for i := range text {
		if runeCount == maxLen {
			cut = i
			break
		}
		runeCount++
	}
	if cut == len(text) {
		return text // fewer than or exactly maxLen runes present: nothing to truncate
	}
	truncated := text[:cut] // cut is a rune boundary → valid UTF-8

	// Prefer the last whitespace boundary, but only when it is not too far back (past
	// the halfway point measured in RUNES, consistent with the rune cap), mirroring the
	// prior heuristic. LastIndexFunc returns the byte offset of the matched rune's
	// start, so slicing there drops the space and leaves a rune boundary.
	if sp := strings.LastIndexFunc(truncated, unicode.IsSpace); sp > 0 {
		if utf8.RuneCountInString(truncated[:sp]) > maxLen/2 {
			return truncated[:sp]
		}
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
//
// The second return value reports whether the BODY itself was truncated at the cap.
// getSourceText needs it to count truncation exactly once across the two clamps: the
// body truncation is counted HERE (#602), and getSourceText counts only the additional
// case this cannot see — the body fit but the prepended identity tips the combined text
// over the cap.
func (w *Worker) fetchTextFromStorage(ref *StorageRef) (text string, bodyTruncated bool, err error) {
	store := w.resolveStore(ref.StorageInstance)
	if store == nil {
		return "", false, fmt.Errorf("content store not configured")
	}

	reader, err := store.Open(w.ctx, ref.Key)
	if err != nil {
		// A store resolved but the read failed: an infra fault, not a wiring gap.
		// Count it distinctly so it is not confused with content_unresolved (M1).
		if w.metrics != nil {
			w.metrics.IncContentResolveError()
		}
		return "", false, fmt.Errorf("failed to open content from instance %q: %w", ref.StorageInstance, err)
	}
	defer reader.Close()

	// Bound the read — no full memory load. The cap is in RUNES, but a UTF-8 rune is up
	// to utf8.UTFMax bytes, so read up to utf8.UTFMax*limit (+1 sentinel) bytes: enough
	// to always contain at least `limit` full runes when the body has them, so the
	// shared rune-safe truncation below yields the SAME bytes the inline lane would.
	// Byte-cutting the stream here (the old data[:limit]) then handing the fragment to
	// getSourceText is exactly what split the two lanes' embedded bytes and dedup keys
	// (gh#627) and could sever a multibyte rune; the truncation is now rune-safe and
	// lane-independent.
	//
	// `limit` is clamped to MaxSourceTextLenCeiling FIRST so a pathological cap
	// (Config.Validate now rejects one, but a directly-constructed worker could still
	// hold it) cannot overflow utf8.UTFMax*limit+1 into a negative LimitReader bound —
	// the old int64(limit)+1 overflowed to an empty body that hop 2 read as "no source
	// text" and DELETED the pending embedding (#628 FIX 2).
	limit := w.maxSourceTextLen
	if limit <= 0 {
		limit = defaultMaxSourceTextLen
	}
	if limit > MaxSourceTextLenCeiling {
		limit = MaxSourceTextLenCeiling
	}
	readCap := int64(limit)*utf8.UTFMax + 1
	data, err := io.ReadAll(io.LimitReader(reader, readCap))
	if err != nil {
		if w.metrics != nil {
			w.metrics.IncContentResolveError()
		}
		return "", false, fmt.Errorf("failed to read content from instance %q: %w", ref.StorageInstance, err)
	}

	// Detect likely JSON-wrapped content (StoredContent envelope) on the raw bytes.
	// Raw text is expected — if it starts with '{', someone probably used
	// StoreContent() instead of Put(). Embeddings will include JSON noise.
	if len(data) > 0 && data[0] == '{' {
		w.logger.Debug("stored content appears JSON-wrapped, expected raw text",
			slog.String("key", ref.Key),
			slog.String("hint", "use Put() for raw body text, not StoreContent()"))
	}

	// Truncate with the SAME rune-safe routine the inline lane uses, so both lanes embed
	// byte-identical text for identical content. Count the BODY truncation HERE (the
	// offloaded lane's body-detection site, #602). `bodyTruncated` holds iff the body
	// exceeded the cap (in runes), whether the read hit the store's end or the byte budget;
	// it is returned so getSourceText can count the combined truncation exactly once.
	raw := string(data)
	truncated := truncateAtWord(raw, limit)
	bodyTruncated = len(truncated) < len(raw)
	if bodyTruncated {
		if w.metrics != nil {
			w.metrics.IncTruncated()
		}
	}

	// Positive observable for the ADR-063 H2 inclusion: an offloaded body was
	// resolved and fetched (previously excluded where no store-read was wired).
	if w.metrics != nil {
		w.metrics.IncContentResolved()
	}

	return truncated, bodyTruncated, nil
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

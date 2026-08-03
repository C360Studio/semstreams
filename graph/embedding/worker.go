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
	"sync/atomic"
	"unicode"
	"unicode/utf8"

	"github.com/nats-io/nats.go"
	"github.com/nats-io/nats.go/jetstream"
	"golang.org/x/sync/singleflight"

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

// TerminalOutcome classifies HOW a pending embedding reached its terminal state, so
// the readiness component can route its current-failed map (#613): OutcomeFailed adds
// the entity (with its bounded reason), every other outcome removes it. It does NOT
// change the watermark, which advances on ALL terminal outcomes regardless — that
// deadlock-avoidance property (a permanently-failing or no-text entity never pins the
// watermark) is deliberately untouched.
type TerminalOutcome int

const (
	// OutcomeGenerated means a usable vector was stored.
	OutcomeGenerated TerminalOutcome = iota
	// OutcomeFailed means a durable StatusFailed record was written for this entity — the
	// only outcome that ADDS to the current-failed map.
	OutcomeFailed
	// OutcomeSkipped means nothing to embed (no text), a corrupt-record drain, or a
	// failure attempt that did NOT persist a durable failed record (superseded /
	// record-gone / context-cancelled). None is a current failure, so all REMOVE the
	// entity from the current-failed map.
	OutcomeSkipped
	// OutcomeDeleted means the record was tombstoned or superseded by a newer revision
	// mid-generation; like Skipped it is not a current failure and removes the entity
	// from the current-failed map.
	OutcomeDeleted
)

// TerminalCallback is called when a pending embedding reaches ANY terminal outcome
// — generated, failed, or deliberately skipped (no text) — carrying the entity ID,
// the ENTITY_STATES SourceRevision that produced the record, the TerminalOutcome, and
// (for OutcomeFailed) the bounded failure reason. It exists so the hop-1 readiness
// watermark can be completed at the true end of the two-hop pipeline (ADR-066 §3) and
// the current-failed map routed by outcome (#613). sourceRevision==0 means "unknown"
// (a legacy record) and the completion is a no-op; ^uint64(0) is the max-rev drain
// used for an unreadable (corrupt) record whose revision cannot be recovered. reason
// is non-empty only for OutcomeFailed.
type TerminalCallback func(entityID string, sourceRevision uint64, outcome TerminalOutcome, reason string)

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
	// IncOffloadedIdentityIncluded counts one offloaded (StorageRef) entity for which a
	// vector was STORED that included its inline identity text (title/.signature/.comment,
	// per text_suffixes) AHEAD of the body. It fires on the successful-persistence path
	// (with IncDedupHits), not at text production — a dropped save does not count (#635
	// retro F3). Paired with IncOffloadedIdentityAbsent, it makes the text-suffix effect on
	// the offloaded lane observable: a producer tuning text_suffixes confirms it took effect
	// from /metrics rather than inferring it from silence (D5/#601).
	IncOffloadedIdentityIncluded()
	// IncOffloadedIdentityAbsent counts one offloaded (StorageRef) entity for which a
	// vector was STORED from its body ALONE — no inline identity text was present. Like its
	// symmetric half it fires only on the successful-persistence path (#635 retro F3): an
	// offloaded entity whose embed failed, or one with neither identity nor body (deleted
	// before generation), counts neither. Lets a producer tell a config-effect from silence
	// (D5/#601).
	IncOffloadedIdentityAbsent()
	// IncFailed increments the failed embeddings counter
	IncFailed()
	// IncFailedReason increments the reason-labelled failures counter (#613). reason is
	// a value from the BOUNDED failure enum (see the failReason* constants) — never the
	// raw error message, which is unbounded and would blow up label cardinality. It
	// fires on the SAME terminal path as IncFailed so it stays a strict partition of the
	// total (mirrors fusion body_hydration_failures_total{reason}, inc 2).
	IncFailedReason(reason string)
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
	// ReportContentExcluded reports one offloaded body EXCLUDED from its embedding
	// because no store in this process serves the StorageInstance its reference names
	// (gh#875) — the FETCH-time half of the gh#414 exclusion path, reached when a store
	// that resolved at the queue-time gate was deregistered before the read.
	//
	// A report, not merely a counter: the implementation owns BOTH the
	// content_unresolved_total counter and the one-shot operator warning that names the
	// remedy, so there is exactly one place that interprets this condition rather than a
	// second spelling of it in the worker. entityID and storageInstance identify the
	// exclusion in that log line; NEITHER is a metric label (entity IDs are unbounded).
	ReportContentExcluded(entityID, storageInstance string)
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
	// snapshotComplete latches true when the EMBEDDING_INDEX WatchAll initial snapshot
	// has been fully replayed (the nil sentinel). It gates FAILED-record re-processing
	// (#613/D4) to the restart snapshot ONLY: the worker watches the same bucket
	// SaveFailed writes to, so re-processing a LIVE failed write would re-embed → fail →
	// re-write in a hot loop whenever the dependency is down. A restart re-delivers the
	// last failed record once (DeliverLastPerSubject); any re-write during recovery is a
	// live update delivered AFTER this sentinel, so it is skipped and the loop cannot
	// form. New-revision recovery still flows through hop-1's SavePending (a live PENDING
	// write, always processed).
	snapshotComplete atomic.Bool
	// generateGroup collapses concurrent byte-identical embedder calls: K workers holding
	// the same content under the same embedder identity make ONE Generate call and share
	// the vector, then each performs its own SaveGenerated (#630). Process-local only —
	// the dedup key already collapses SEQUENTIAL cross-process dupes; the distributed
	// KV-reservation variant is deferred.
	generateGroup singleflight.Group

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
				// The WatchAll initial-snapshot sentinel: every last-per-subject value has
				// been replayed. Latch it so FAILED records are re-processed only from the
				// restart snapshot, not from live self-writes (see snapshotComplete / D4).
				w.snapshotComplete.Store(true)
				continue
			}

			// Process if this is a new pending record or update to existing pending.
			if entry.Operation() == jetstream.KeyValuePut {
				// Capture the snapshot-vs-live phase HERE, at the single point of dequeue,
				// and carry it into processing as an immutable job property (#613 F3). The
				// flag must NOT be re-read deep inside handleKVEntry: this pool shares one
				// WatchAll channel, so between the moment a worker dequeues a snapshot record
				// and the moment it would re-read the flag, a sibling worker can consume the
				// nil sentinel and flip it — reading it later would then misclassify a
				// snapshot record as live and skip its restart reprocessing. Reading it once,
				// immediately at dequeue, fixes the phase to the value in force when the entry
				// was taken. (The residual sub-instruction window between the channel receive
				// and this load is closed for CORRECTNESS by the hop-1 re-SavePending backstop
				// — see handleKVEntry's reprocessFailed comment.)
				duringSnapshot := !w.snapshotComplete.Load()
				w.handleKVEntrySafe(entry, duringSnapshot, workerID)
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
func (w *Worker) handleKVEntrySafe(entry jetstream.KeyValueEntry, duringSnapshot bool, workerID int) {
	defer func() {
		if r := recover(); r != nil {
			w.logger.Error("Embedding worker panic recovered; entry stays pending and its readiness watermark is NOT advanced",
				"worker_id", workerID,
				"entity_id", entry.Key(),
				"panic", r,
				"stack", string(debug.Stack()))
		}
	}()

	entityID, sourceRevision, terminal, outcome, reason := w.handleKVEntry(entry, duringSnapshot, workerID)
	if terminal && w.onTerminal != nil {
		w.onTerminal(entityID, sourceRevision, outcome, reason)
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
	entry jetstream.KeyValueEntry, duringSnapshot bool, workerID int,
) (entityID string, sourceRevision uint64, terminal bool, outcome TerminalOutcome, reason string) {
	// Parse the record to check status
	var record Record
	if err := json.Unmarshal(entry.Value(), &record); err != nil {
		// A corrupt record cannot yield its SourceRevision, so its hop-1 pending
		// entry can never be completed with an exact revision — and unlike a
		// deleted entity, no re-observation is guaranteed. Max-rev-drain the key so
		// one poison record cannot wedge the whole embedding.ready signal into
		// permanent degraded (ADR-066 §3 D3); loud, because it is not expected. A
		// corrupt record is not a current embedding failure (no bounded reason, no
		// durable failed record), so it is OutcomeSkipped for the current-failed map.
		w.logger.Warn("Failed to unmarshal embedding record; draining readiness watermark for key",
			"key", entry.Key(), "error", err)
		return entry.Key(), ^uint64(0), true, OutcomeSkipped, ""
	}

	// Process PENDING records always (fresh work from hop-1). Also re-process a FAILED
	// record delivered in the RESTART SNAPSHOT (D4/#613): a restart re-delivers the last
	// failed record (DeliverLastPerSubject), so a transient dependency outage recovers
	// without operator action once the dependency returns. A GENERATED record is already
	// done, and a FAILED record delivered LIVE (past the snapshot sentinel) is this
	// worker's own SaveFailed write — re-processing either would fire a spurious callback
	// or spin a hot re-embed loop, so both are skipped (results below are set AFTER this
	// skip, never before — ADR-066 §3).
	//
	// duringSnapshot is the phase captured at dequeue (#613 F3), NOT a re-read of the
	// shared flag: reading the flag here would race a sibling worker consuming the nil
	// sentinel. If that capture ever misclassifies a snapshot failed record as live (the
	// sub-instruction window between the channel receive and the flag load), the record is
	// merely skipped here — it is NOT lost: hop-1 re-delivers the same entity from
	// ENTITY_STATES on restart and re-writes a fresh StatusPending record (a LIVE pending
	// write, always processed above), which regenerates it. So the phase gate is a
	// hot-loop guard whose worst misfire falls back on the always-processed pending lane,
	// never on stranded work.
	reprocessFailed := record.Status == StatusFailed && duringSnapshot
	if record.Status != StatusPending && !reprocessFailed {
		return "", 0, false, OutcomeGenerated, ""
	}

	// Every path past this point is genuinely terminal — the pipeline never retries;
	// every error is a hard SaveFailed, no-text is a delete, success is SaveGenerated.
	// Setting the named results ONCE here means every `return` below reports the same
	// completion, closing the "missed a terminal site" risk that the old defer closed
	// — without also completing on the panic path. sourceRevision==0 (a legacy
	// record) makes the completion a no-op. The default outcome is OutcomeGenerated;
	// each terminal path overwrites it with the outcome the current-failed map needs.
	entityID, sourceRevision, terminal, outcome = entry.Key(), record.SourceRevision, true, OutcomeGenerated

	w.logger.Debug("Processing pending embedding", "worker_id", workerID, "entity_id", entityID)

	// Get source text - either from record or from ObjectStore via StorageRef.
	//
	// qualifier records what this terminal state IS when it is not simply "complete"
	// (gh#875). Two sites can observe the condition and neither re-derives the other's:
	//
	//   - HOP 1 already knew: its gate refused the offloaded lane because no store
	//     serves the instance, so it queued the entity's inline text with the qualifier
	//     on the PENDING record. Hop 2 cannot re-observe that — the pending record it
	//     receives carries no StorageRef at all — so it carries the fact forward.
	//   - HOP 2 observes it itself when the store resolved at the gate and was
	//     deregistered before this fetch (the branch below).
	//
	// Only the known qualifier value is accepted, never an arbitrary inherited string:
	// an unrecognized reason on a pending record fails closed to unqualified rather than
	// propagating into the terminal record.
	var qualifier string
	if record.Reason == ReasonContentExcluded {
		qualifier = ReasonContentExcluded
	}
	sourceText, err := w.getSourceText(&record)
	switch {
	case err == nil:
		// Text in hand; nothing to classify. (Go switch cases do not fall through.)

	case errors.Is(err, errNoStoreForInstance):
		// gh#875: no store in this process serves the instance this reference names.
		// That is a deployment WIRING fact, not an embedding failure: re-delivery
		// cannot register a missing store, so a durable failed record here would pin
		// the index Degraded forever with no exit but deleting the entity. Report the
		// exclusion (metric + the operator-actionable warning, the same gh#414 path
		// hop 1 uses) and carry on with whatever INLINE identity text the record holds,
		// so the entity still reaches a terminal outcome below.
		//
		// The outcome is a QUALIFIED SUCCESS, not a plain one: the vector this produces
		// is real and servable but MISSING ITS BODY, and a record that cannot say so is
		// both unenumerable ("which entities are missing bodies?" has no field to scan)
		// and unrepairable (SavePendingGuarded would skip the re-queue that wiring the
		// store should trigger). The qualifier carries that fact to the stored record.
		w.reportContentExcluded(entityID, &record)
		sourceText = w.capText(record.IdentityText)
		qualifier = ReasonContentExcluded

	default:
		w.logger.Error("Failed to get source text", "entity_id", entityID, "error", err)
		outcome, reason, terminal = w.markFailed(entityID, fmt.Sprintf("text extraction failed: %v", err), failReasonContentError, sourceRevision)
		return
	}

	if sourceText == "" {
		w.logger.Debug("No source text found, skipping embedding", "entity_id", entityID)
		// Not a failure - just nothing to embed. Remove pending record.
		if err := w.storage.DeleteEmbedding(w.ctx, entityID); err != nil {
			w.logger.Debug("Failed to delete pending record for entity with no text", "entity_id", entityID, "error", err)
		}
		outcome = OutcomeSkipped
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
	vector, wasDedupHit, genOutcome, genReason, genTerminal, err := w.getOrGenerateEmbedding(entityID, sourceText, dedupKey, sourceRevision)
	if err != nil {
		// failure already logged, marked, classified. genTerminal is false when the failure
		// could not be durably recorded (persistence miss) — then the record stays pending
		// and must NOT advance the watermark or clear the failed map; it retries on
		// re-delivery (#613 F2).
		outcome, reason, terminal = genOutcome, genReason, genTerminal
		return
	}

	// Save and notify. saveAndNotify reports whether the outcome is terminal: a
	// non-converging revision CAS (ErrCASExhausted) leaves the record pending and
	// re-drivable, so it must NOT drain the readiness watermark (ADR-066 §3). Every
	// other outcome is terminal (already set above). wasDedupHit and the offloaded-
	// identity pair are counted there, on the success path only, so they stay a subset
	// of resolutions (the record is passed so saveAndNotify can derive the offloaded
	// identity state at the successful-persistence site).
	terminal, outcome, reason = w.saveAndNotify(entityID, &record, vector, dedupKey, sourceRevision, wasDedupHit, qualifier)
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
func (w *Worker) getOrGenerateEmbedding(entityID, sourceText, key string, sourceRevision uint64) (vec []float32, wasDedupHit bool, outcome TerminalOutcome, reason string, terminal bool, err error) {
	dedupEnabled := key != ""

	// Check deduplication first
	if dedupEnabled {
		dedupRecord, derr := w.storage.GetByContentHash(w.ctx, key)
		if derr != nil {
			w.logger.Error("Failed to check dedup", "entity_id", entityID, "error", derr)
			outcome, reason, terminal = w.markFailed(entityID, fmt.Sprintf("dedup check failed: %v", derr), failReasonInternal, sourceRevision)
			return nil, false, outcome, reason, terminal, derr
		}

		if dedupRecord != nil && w.dedupRecordUsable(entityID, key, dedupRecord) {
			w.logger.Debug("Deduplicating embedding", "entity_id", entityID, "dedup_key", key)
			return dedupRecord.Vector, true, OutcomeGenerated, "", true, nil
		}
	} else if w.metrics != nil {
		w.metrics.IncDedupSkipped(dedupSkipReasonIdentityUnresolved)
	}

	// Generate new embedding. Collapse concurrent byte-identical generations to ONE
	// embedder call (#630): keyed by the content so K workers holding identical content
	// share the vector, then each performs its own SaveGenerated below. generateShared
	// derives a stable in-process key even when the durable dedup key is withheld
	// (unresolved embedder width), so a cold-start burst also collapses to one call.
	w.logger.Debug("Generating new embedding", "entity_id", entityID)
	vector, gerr := w.generateShared(key, sourceText)
	if gerr != nil {
		if errors.Is(gerr, errNoEmbeddingReturned) {
			w.logger.Error("No embedding generated", "entity_id", entityID)
			outcome, reason, terminal = w.markFailed(entityID, "no embedding returned", failReasonEmbedderError, sourceRevision)
			return nil, false, outcome, reason, terminal, gerr
		}
		w.logger.Error("Failed to generate embedding", "entity_id", entityID, "error", gerr)
		genReason := classifyEmbedErr(gerr)
		outcome, reason, terminal = w.markFailed(entityID, fmt.Sprintf("generation failed: %v", gerr), genReason, sourceRevision)
		return nil, false, outcome, reason, terminal, gerr
	}

	// Save to dedup bucket. Skipped when no key could be derived for this record.
	if dedupEnabled {
		if serr := w.storage.SaveDedup(
			w.ctx, key, vector, entityID, w.embedder.Model(), w.embedder.Dimensions(),
		); serr != nil {
			w.logger.Warn("Failed to save dedup record", "entity_id", entityID, "error", serr)
			// Continue anyway - not critical
		}
	}

	return vector, false, OutcomeGenerated, "", true, nil
}

// errNoEmbeddingReturned marks the "the embedder returned zero vectors" case so the
// caller can classify it (embedder_error) distinctly from a transport error. It is an
// internal sentinel, never surfaced.
var errNoEmbeddingReturned = errors.New("no embedding returned")

// generateShared makes exactly one embedder Generate call for concurrent byte-identical
// content (#630, process-local). Peers holding the same content wait and share the first
// caller's vector; the shared error (semembed down) also reaches every peer, so each
// fails and marks its own entity.
//
// key is the durable dedup key. When it is non-empty (resolved embedder width) it doubles
// as the singleflight key — unchanged behavior. When it is EMPTY (cold start: the width is
// unresolved so DedupKey withheld a durable key), a stable content-derived in-process key
// is used instead of bypassing the singleflight, so a cold-start burst of identical
// content still collapses to one paid call rather than one per worker (#630 cold-start).
// The durable dedup CACHE stays correctly withheld for the empty-key case — only the
// in-flight call is collapsed, never a stored vector.
func (w *Worker) generateShared(key, sourceText string) ([]float32, error) {
	sfKey := key
	if sfKey == "" {
		sfKey = InProcessDedupKey(w.embedderIdentity(), sourceText)
	}
	v, err, _ := w.generateGroup.Do(sfKey, func() (any, error) {
		return w.generateOne(sourceText)
	})
	if err != nil {
		return nil, err
	}
	return v.([]float32), nil
}

// generateOne is the single embedder call, normalizing an empty result set to
// errNoEmbeddingReturned so both the direct and singleflight paths classify it alike.
func (w *Worker) generateOne(sourceText string) ([]float32, error) {
	vectors, err := w.embedder.Generate(w.ctx, []string{sourceText})
	if err != nil {
		return nil, err
	}
	if len(vectors) == 0 {
		return nil, errNoEmbeddingReturned
	}
	return vectors[0], nil
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
// It returns terminal==false for a re-drivable transient that left the record durably
// PENDING: the SaveGenerated CAS did not converge (ErrCASExhausted), OR a real save
// failure whose subsequent markFailed could not persist a durable StatusFailed record
// either (a double persistence miss). In both cases the record stays pending and the
// ENTITY_STATES watcher will re-deliver the entity on its next write, so handleKVEntry
// must NOT advance the readiness watermark past this revision or clear the failed map
// (that would report the pipeline caught up over still-pending work — #613 F2). Every
// other outcome — success, a superseded/record-gone drop, or a save failure that DID
// durably record as failed — is terminal.
//
// record is the pending record being completed; it is read ONLY to derive the offloaded-
// identity observability pair on the success path (StorageRef set => offloaded;
// IdentityText set => identity was embedded). Those counters fire at the SAME
// successful-persistence point as IncDedupHits so they count STORED vectors, not
// attempts (#635 retro F3).
//
// qualifier is the terminal-state qualifier this generation produced (gh#875): "" for a
// complete vector, ReasonContentExcluded when the body was unreachable and only the
// entity's inline text was embedded. It is threaded from the one site that OBSERVED the
// condition (handleKVEntry's fetch branch), never re-derived here.
func (w *Worker) saveAndNotify(entityID string, record *Record, vector []float32, contentHash string, sourceRevision uint64, wasDedupHit bool, qualifier string) (terminal bool, outcome TerminalOutcome, reason string) {
	dimensions := len(vector)
	model := w.embedder.Model()
	if err := w.storage.SaveGenerated(w.ctx, entityID, vector, model, dimensions, contentHash, sourceRevision, qualifier); err != nil {
		// The entity was tombstoned (or its pending record removed) while this
		// vector was being generated. That is a normal race, not a failure: there
		// is nothing to mark failed, and counting it would inflate the failure
		// metric across gh#527 bulk deletion. Return before onGenerated — firing it
		// would push the dropped vector into the query-side cache, resurrecting in
		// memory exactly what gh#614 removed from KV. Terminal: the entity is
		// deliberately without an embedding now — OutcomeDeleted removes it from the
		// current-failed map.
		if errors.Is(err, ErrRecordGone) {
			w.logger.Debug("Embedding record removed during generation; dropping vector",
				"entity_id", entityID)
			return true, OutcomeDeleted, ""
		}
		// A newer source revision's vector already landed. The vector THIS call holds
		// is the older one; firing onGenerated would push it into a WithOnGenerated
		// consumer's cache, exactly the stale-vector hazard ErrRecordGone guards
		// against. Skip the callback; the newer write is authoritative. Terminal: the
		// newer outcome already completed this entity (OutcomeDeleted for the map).
		if errors.Is(err, ErrSupersededRevision) {
			w.logger.Debug("Embedding superseded by a newer revision; dropping vector",
				"entity_id", entityID)
			return true, OutcomeDeleted, ""
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
			return false, OutcomeGenerated, ""
		}
		w.logger.Error("Failed to save generated embedding", "entity_id", entityID, "error", err)
		markOutcome, markReason, markTerminal := w.markFailed(entityID, fmt.Sprintf("save failed: %v", err), failReasonInternal, sourceRevision)
		return markTerminal, markOutcome, markReason
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

	// Count the offloaded-identity observability pair HERE too, on the same
	// successful-persistence path (#635 retro F3), never at text-production time in
	// getSourceText: a later dedup/embedder/CAS/tombstone drop must not confirm a STORED
	// vector that never landed. Offloaded-only (StorageRef set). A body-less/no-text
	// offloaded entity never reaches this site — handleKVEntry deletes its pending record
	// before generation — so it counts neither, and a failed embed returns above without
	// counting either.
	if record != nil && record.StorageRef != nil && w.metrics != nil {
		if record.IdentityText != "" {
			w.metrics.IncOffloadedIdentityIncluded()
		} else {
			w.metrics.IncOffloadedIdentityAbsent()
		}
	}

	if w.onGenerated != nil {
		w.onGenerated(entityID, vector)
	}
	return true, OutcomeGenerated, ""
}

// getSourceText produces the exact bytes hop 2 embeds (and keys the dedup bucket over).
//
// It is re-branched on StorageRef PRIMARY (D1). The discriminator for "this body is
// offloaded" is StorageRef != nil, NOT SourceText: on an offloaded record the inline
// identity prefix (title/.signature/.comment) travels in IdentityText, and SourceText is
// empty. Reading IdentityText (not SourceText) here is what keeps a PRE-#635 worker safe
// — it never sees the identity in SourceText, so it cannot embed identity-only and drop
// the body (#635 retro F1). So:
//
//   - Offloaded (StorageRef != nil): fetch the body, then embed the inline IdentityText
//     AHEAD of it (identity-first, one vector) when present, so text_suffixes takes
//     effect on offloaded entities exactly as it does inline (D1/D2). Identity first
//     means the cap below trims the body tail and the identity always survives.
//   - Inline (StorageRef == nil): SourceText is the whole text (unchanged).
//
// It does NOT count the offloaded-identity observability pair here: text production is
// not proof a vector was stored. saveAndNotify counts included/absent on the successful
// persistence path instead (#635 retro F3), matching the IncDedupHits pattern.
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
		case record.IdentityText != "" && body != "":
			text = record.IdentityText + identityBodySeparator + body
		case record.IdentityText != "":
			text = record.IdentityText // empty body: identity alone
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

	// Inline lane: SourceText is the whole text, capped through the SAME rune-safe
	// routine the offloaded lane uses, so identical content embeds byte-identical text
	// (and derives the same hop-2 dedup key) regardless of lane.
	return w.capText(record.SourceText), nil
}

// capText applies the effective source-text cap and counts the truncation exactly
// once when it actually shortens the text, so the bytes embedded stay discoverable
// (#602). It is the inline lane's single truncation-detection site, and the site for
// the identity-only text an unresolvable storage instance falls back to (gh#875).
//
// The offloaded lane does NOT use it: that lane's cap application must suppress the
// count when fetchTextFromStorage already counted the body truncation, so the two
// clamps never double-count (see getSourceText).
func (w *Worker) capText(text string) string {
	if w.maxSourceTextLen <= 0 {
		return text
	}
	truncated := truncateAtWord(text, w.maxSourceTextLen)
	if len(truncated) < len(text) && w.metrics != nil {
		w.metrics.IncTruncated()
	}
	return truncated
}

// reportContentExcluded reports one offloaded body EXCLUDED from its embedding
// because no store in this process serves the instance its reference names (gh#875).
//
// It does NOT own the reporting: it forwards to the metrics reporter, whose
// component-side implementation is the single place that interprets this condition
// (the content_unresolved_total counter and the one-shot operator warning naming the
// remedy — the gh#414 path hop 1 already uses). A worker with no reporter wired still
// behaves correctly; only the report is lost.
func (w *Worker) reportContentExcluded(entityID string, record *Record) {
	if w.metrics == nil {
		return
	}
	instance := ""
	if record != nil && record.StorageRef != nil {
		instance = record.StorageRef.StorageInstance
	}
	w.metrics.ReportContentExcluded(entityID, instance)
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

// errNoStoreForInstance is the distinguishable condition "no store in this process
// can serve the StorageInstance this reference names" (gh#875). It is a CLASS the
// caller branches on with errors.Is — never a message match, which would make the
// decision by predicting text instead of observing a type.
//
// It is deliberately separate from every other fetch error: a resolved store's Open
// or read failure is an infra fault that genuinely recovers on re-delivery, so it
// stays failReasonContentError with its recovery guarantee. This class cannot recover
// on re-delivery — re-delivery does not register a missing store — so recording it as
// a failed embedding would convert a deployment wiring fact into a permanent Degraded
// verdict. handleKVEntry routes it to the excluded-content path instead.
var errNoStoreForInstance = errors.New("no store for storage instance")

// instanceNamer is the narrow method set a store implements to state which
// StorageInstance its references carry (storage.StreamableStore does not require it;
// *objectstore.Store provides it). Asserted rather than required so the store
// interface stays about I/O.
type instanceNamer interface {
	InstanceName() string
}

// ownedStoreServes reports whether the worker's OWNED fallback store is the store
// that instance names.
//
// Fail-closed on an unknown answer: a fallback store that cannot state its instance
// cannot be shown to serve this reference, so it does not answer for it. The
// alternative (assume it serves) is the gh#875 defect itself — a store answering for
// an instance it never held, whose read then fails and latches a durable failure.
// Excluding is non-destructive by comparison: the entity still embeds its inline text.
func (w *Worker) ownedStoreServes(instance string) bool {
	if w.contentStore == nil || instance == "" {
		return false
	}
	namer, ok := w.contentStore.(instanceNamer)
	if !ok {
		return false
	}
	return namer.InstanceName() == instance
}

// resolveStore returns the store that backs a StorageInstance: the shared
// registry first (federated — resolves ANY registered storage instance), then
// the worker's OWNED fallback store, but ONLY when that store IS the named
// instance (gh#875). Resolves per-fetch and returns a BORROWED handle from the
// registry path — callers must not close it. Returns nil when neither path can
// serve the instance.
//
// The re-check here is not redundant with the component's hop-1 gate. The registry
// contract is explicitly per-fetch with no caching, and deregistration on component
// Stop is a live path — so a store that resolved when hop 1 decided to fetch can be
// gone by the time hop 2 reads. Gating alone would leave that race producing the
// same permanent durable-failed latch, reached by a narrower door.
func (w *Worker) resolveStore(instance string) storage.StreamableStore {
	if w.storeResolver != nil && instance != "" {
		if s, ok := w.storeResolver.Streamable(instance); ok {
			return s
		}
	}
	if w.ownedStoreServes(instance) {
		return w.contentStore
	}
	return nil
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
		// Distinguishable CLASS, not a message (gh#875): the caller routes this to the
		// excluded-content path instead of a durable failed record. Wrapped with %w so
		// errors.Is matches through the caller's error chain.
		return "", false, fmt.Errorf("%w: %q", errNoStoreForInstance, ref.StorageInstance)
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

// Bounded failure-reason enum (#613). These are the ONLY values that ever reach the
// failures_total{reason} metric label — the raw ErrorMsg is unbounded and must never be
// a label. Keep the set small and closed; classifyEmbedErr maps embedder error shapes
// into the network/timeout/dimension buckets, defaulting to embedder_error.
const (
	failReasonContentError      = "content_error"      // source-text extraction failed
	failReasonInternal          = "internal"           // dedup-check / save write fault
	failReasonConnectionRefused = "connection_refused" // embedder endpoint unreachable
	failReasonTimeout           = "timeout"            // embedder call timed out
	failReasonDimensionMismatch = "dimension_mismatch" // embedder returned an off-width vector
	failReasonEmbedderError     = "embedder_error"     // any other embedder-side failure
)

// classifyEmbedErr maps a generation error into the bounded reason enum. It matches on
// the error shape (context deadline) and message substrings rather than concrete error
// types because the embedder call crosses an HTTP/SDK boundary that flattens most causes
// into strings. An unrecognized shape defaults to embedder_error — bounded either way.
func classifyEmbedErr(err error) string {
	if err == nil {
		return failReasonEmbedderError
	}
	if errors.Is(err, context.DeadlineExceeded) {
		return failReasonTimeout
	}
	msg := strings.ToLower(err.Error())
	switch {
	case strings.Contains(msg, "connection refused"):
		return failReasonConnectionRefused
	case strings.Contains(msg, "timeout") || strings.Contains(msg, "deadline exceeded") ||
		strings.Contains(msg, "context deadline"):
		return failReasonTimeout
	case strings.Contains(msg, "dimension"):
		return failReasonDimensionMismatch
	default:
		return failReasonEmbedderError
	}
}

// markFailed records an embedding failure and reports (outcome, reportReason, terminal)
// for the caller to propagate. sourceRevision is the revision this failure completes, so
// a stale older-revision failure cannot clobber a newer success under the storage
// ordering guard (#614 part 2). reason is the BOUNDED classification (see failReason*),
// persisted next to the raw errorMsg and used as the failures_total{reason} label.
//
// The three returns encode a two-axis outcome:
//
//   - outcome + reportReason drive the component's current-failed map. OutcomeFailed (with
//     its reason) is returned ONLY when a DURABLE StatusFailed record was actually written,
//     so the map mirrors durable state; every other path returns OutcomeSkipped with an
//     empty reason (not a current failure).
//   - terminal drives the readiness watermark. It is true when the record reached a
//     DURABLE terminal state (failed, or superseded/record-gone — the entity is resolved
//     one way or another) and on shutdown. It is FALSE only for a PERSISTENCE MISS: the
//     SaveFailed write itself failed transiently (CAS did not converge, KV write error),
//     so the durable record is still PENDING. Advancing the watermark or clearing the map
//     there would report the pipeline caught up over an un-recorded, still-pending entity;
//     instead it stays non-terminal and re-drives on the next ENTITY_STATES delivery
//     (#613 F2), mirroring saveAndNotify's ErrCASExhausted handling.
//
// The failures counters fire on the SAME events as before — every real failure event
// including a record-gone or persistence-miss one, but NOT a superseded or shutdown one —
// so failures_total{reason} stays a strict partition of errors_total (#613).
func (w *Worker) markFailed(entityID, errorMsg, reason string, sourceRevision uint64) (outcome TerminalOutcome, reportReason string, terminal bool) {
	// Don't count context cancellation (shutdown) as a failure. Terminal: the worker is
	// stopping and the in-memory watermark is discarded on restart, so there is no
	// stranded-work hazard to guard against here.
	if strings.Contains(errorMsg, "context canceled") {
		w.logger.Debug("Skipping failure metric for context cancellation", "entity_id", entityID)
		return OutcomeSkipped, "", true
	}

	err := w.storage.SaveFailed(w.ctx, entityID, errorMsg, reason, sourceRevision)
	switch {
	case err == nil:
		// Durable StatusFailed written: a current failure, terminal, counted.
		w.incFailure(reason)
		return OutcomeFailed, reason, true

	case errors.Is(err, ErrSupersededRevision):
		// A newer source revision already resolved this entity; this older failure is
		// moot. Do NOT count it (the entity is not in a failed state) — terminal because
		// the newer outcome already resolved the entity.
		w.logger.Debug("Embedding failure superseded by a newer revision; not counting",
			"entity_id", entityID)
		return OutcomeSkipped, "", true

	case errors.Is(err, ErrRecordGone):
		// The record is already gone (tombstoned / no-text delete). The failure event is
		// still real and counted, but there is no durable failed record, so it does not
		// enter the current-failed map. Terminal: the entity is deliberately without an
		// embedding now.
		w.logger.Debug("Embedding record removed before failure could be recorded",
			"entity_id", entityID)
		w.incFailure(reason)
		return OutcomeSkipped, "", true

	default:
		// PERSISTENCE MISS: SaveFailed itself failed (CAS did not converge, or a transient
		// KV write error), so the durable record did NOT transition — it is still pending.
		// Count the failure event for errors_total, but return terminal==false so the
		// watermark does not advance and the failed map is not cleared; the entity re-drives
		// on the next ENTITY_STATES delivery (#613 F2).
		w.incFailure(reason)
		w.logger.Error("Failed to mark embedding as failed; leaving record pending for re-drive",
			"entity_id", entityID, "error", err)
		return OutcomeSkipped, "", false
	}
}

// incFailure fires the failure counters (total + reason-labelled). reason is a value from
// the bounded failReason* enum, never the raw error message.
func (w *Worker) incFailure(reason string) {
	if w.metrics != nil {
		w.metrics.IncFailed()
		w.metrics.IncFailedReason(reason)
	}
}

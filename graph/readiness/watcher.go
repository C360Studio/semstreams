// Package readiness owns BOTH sides of the ADR-083 readiness distribution contract.
// Producers (publisher.go) create the GRAPH_STATUS KV bucket and write their envelope
// to their own key on every heartbeat tick; consumers (watcher.go) watch that key,
// hold the last-known ADR-066 envelope, and answer "(status, fresh | unknown)" with no
// per-decision NATS round-trip.
//
// It exists because per-decision request/reply died under exactly the load that
// makes readiness interesting: in a single-binary deployment the status request
// travels the same connection as the ENTITY_STATES firehose, so an in-process
// requester pays the saturated stream twice per round-trip, times out, and fails
// closed — indistinguishable in the logs from a genuine not-ready (gh#590). The KV
// twofer inverts it: the producer pays one write per heartbeat and N consumers hold
// state.
//
// Freshness is judged by CONSUMER-LOCAL arrival time. No producer timestamp is ever
// compared against the consumer's clock (ADR-083 D2) — arrival-time freshness needs
// no clock agreement between processes, and cross-process skew would buy nothing.
//
// This package holds the bucket/key identifiers for BOTH sides of the contract, so
// producers and consumers cannot drift onto different names. It deliberately does
// not import natsclient: BucketSource is the narrow method set *natsclient.Client
// already satisfies, which keeps the dependency one-way and the unit tests free of
// a live NATS.
package readiness

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"github.com/c360studio/semstreams/graph"
	"github.com/nats-io/nats.go/jetstream"
)

// The GRAPH_STATUS identifiers, shared by producers and consumers.
const (
	// BucketGraphStatus is the dedicated readiness bucket. It is operational
	// component status, NOT domain entity state: it never routes through
	// graph-ingest, carries no ADR-055 semantic envelope, and is kept out of
	// ENTITY_STATES so the sole-writer invariant and the graph itself stay clean.
	BucketGraphStatus = "GRAPH_STATUS"
	// BucketHistory is the KV history depth for the bucket: enough replay to see
	// the last few transitions after an incident, not enough to hoard.
	BucketHistory = 3
	// KeyGraphIndex is graph-index's status key (one key per producer).
	KeyGraphIndex = "graph-index"
	// KeyGraphEmbedding is graph-embedding's status key.
	KeyGraphEmbedding = "graph-embedding"
)

const (
	// DefaultHeartbeat is the producers' status tick: they publish the envelope
	// every tick unconditionally, so the write doubles as a liveness heartbeat.
	DefaultHeartbeat = 5 * time.Second
	// FreshnessMultiplier is how many heartbeats a held status stays fresh. It is a
	// CONSTANT, not config (owner decision): 3 tolerates a lost write and a slow
	// delivery without letting a dead feed masquerade as live, and one fewer knob
	// is one fewer way to configure a fail-open.
	FreshnessMultiplier = 3
	// defaultRebindDelay is how long the watcher waits before re-opening the bucket
	// or watch after a failure. Well under the freshness window, so a transient
	// bucket outage that resolves quickly never surfaces as unknown.
	defaultRebindDelay = time.Second
)

// BucketSource opens a KV bucket by name. *natsclient.Client satisfies it via
// GetKeyValueBucket — declared here as the narrow method set (with the EXACT
// signature, or the structural match silently breaks) rather than importing
// natsclient, so this package stays a leaf and tests need no live NATS.
type BucketSource interface {
	GetKeyValueBucket(ctx context.Context, name string) (jetstream.KeyValue, error)
}

// FreshnessWindow is how long a status value stays fresh for a producer publishing on
// the given heartbeat. Exported so consumers and their tests express the window in one
// vocabulary instead of re-deriving 3x from the two constants.
func FreshnessWindow(heartbeat time.Duration) time.Duration {
	if heartbeat <= 0 {
		heartbeat = DefaultHeartbeat
	}
	return time.Duration(FreshnessMultiplier) * heartbeat
}

// MinBoundedStaleness is the smallest max-staleness tolerance a consumer can declare
// and still have it satisfiable, given a producer publishing every heartbeat.
//
// The bound is judged against the view's CURRENT age: the staleness the producer
// computed plus how long ago that envelope arrived. Arrival age sweeps 0 -> heartbeat
// between publishes, so a bound at or below one heartbeat is unsatisfiable for part of
// every cycle no matter how caught-up the index is — measured at a 5s heartbeat, a 3s
// bound proceeds on roughly half of ticks against an essentially caught-up view.
//
// The underlying rule is not a tuning preference: YOU CANNOT BOUND A QUANTITY BELOW THE
// INTERVAL AT WHICH YOU LEARN IT. Reading the key per decision does not help either —
// KV Get and Watch are equally stale on a tick-published key (ADR-083), which is why
// the answer is a floor rather than a faster probe.
//
// This is the floor for SATISFIABILITY, not the recommended value. A consumer whose own
// run time scales with its data must also clear its worst-case cycle (gh#605).
func MinBoundedStaleness(heartbeat time.Duration) time.Duration {
	if heartbeat <= 0 {
		heartbeat = DefaultHeartbeat
	}
	return heartbeat
}

// ValidateStalenessBound reports why a declared max-staleness tolerance cannot work at
// this heartbeat, or nil if it can. Zero means "exact catch-up" and is always valid —
// it does not go through the staleness comparison at all.
//
// Exposed on the framework rather than written into one consumer's config validation
// because the constraint belongs to the DISTRIBUTION contract, not to community
// detection: every consumer that declares a bounded freshness inherits it.
func ValidateStalenessBound(bound, heartbeat time.Duration) error {
	if bound <= 0 {
		return nil
	}
	if floor := MinBoundedStaleness(heartbeat); bound <= floor {
		return fmt.Errorf(
			"max-staleness bound %s is at or below the readiness heartbeat %s, so it can never be "+
				"satisfied reliably: the gate judges the envelope's staleness plus its arrival age, "+
				"and arrival age sweeps up to one full heartbeat between publishes. Declare a bound "+
				"above %s (and above this consumer's own worst-case cycle), or 0 for exact catch-up",
			bound, floor, floor)
	}
	return nil
}

// Reading is the consumer-side answer: the last-known envelope plus everything
// needed to attribute a defer without correlating log lines.
//
// Status is authoritative ONLY when Fresh. When Fresh is false the envelope is
// last-known-and-aged (or the zero value if none ever arrived) and is carried for
// DIAGNOSTICS only — a caller must fail closed, never read the stale bits as index
// state. That distinction is the whole point: gh#590 cost three investigation cycles
// because a transport failure wore not-ready's log line.
type Reading struct {
	// Status is the last envelope received on the key.
	Status graph.IndexStatusResponse
	// Raw is the untouched wire value the Status field was decoded from, for a caller
	// that must decode into its OWN field-identical struct (pkg/fusion.IndexStatus).
	// Handing such a caller only the decoded graph.IndexStatusResponse would force a
	// hand-copied field remap, which is exactly the bug that silently dropped
	// IndexedRevision/Lag once before and read downstream as a false caught-up. Nil
	// when !Known.
	Raw []byte
	// Fresh reports that the last update arrived within FreshnessMultiplier
	// heartbeats. False means UNKNOWN — fail closed.
	Fresh bool
	// Known reports that an envelope has been received at least once. It separates
	// "never saw the producer" (absent bucket/key, standalone deployment) from "the
	// feed went quiet", which are different operator problems.
	Known bool
	// Age is how long ago the last update arrived, consumer-local. Zero when
	// !Known.
	Age time.Duration
	// Err is the last bucket/watch/decode failure, if any, for the structured defer
	// log. It is cleared by the next good update.
	Err error
}

// Watcher holds the last-known readiness envelope for ONE producer key. One
// instance serves every gate decision in a consumer: Read is safe from any
// goroutine, while Start and Stop belong to the owning component's lifecycle
// goroutine (Start once, Stop once).
type Watcher struct {
	src    BucketSource
	bucket string
	key    string

	heartbeat   time.Duration
	rebindDelay time.Duration
	now         func() time.Time
	logger      *slog.Logger

	mu          sync.Mutex
	status      graph.IndexStatusResponse
	raw         []byte
	lastArrival time.Time
	known       bool
	// staleOnArrival marks a held envelope that was ALREADY older than the freshness
	// window when it was delivered. See apply for why this one commit-time comparison
	// exists.
	staleOnArrival bool
	lastErr        error

	// first is closed once an envelope has been decoded and held, so a consumer
	// binding lazily can wait for its first answer instead of failing closed on a
	// watch that has simply not delivered yet.
	first     chan struct{}
	firstOnce sync.Once

	// applied is a test-only synchronization hook, signalled after every update is
	// applied (or rejected). Production leaves it nil, so the send is skipped.
	applied chan struct{}

	startOnce sync.Once
	cancel    context.CancelFunc
	done      chan struct{}
}

// Option configures a Watcher.
type Option func(*Watcher)

// WithHeartbeat declares the producer's publish interval, which sets the freshness
// window (FreshnessMultiplier x heartbeat). Defaults to DefaultHeartbeat.
func WithHeartbeat(d time.Duration) Option {
	return func(w *Watcher) {
		if d > 0 {
			w.heartbeat = d
		}
	}
}

// WithLogger sets the logger used for bind/decode failures.
func WithLogger(l *slog.Logger) Option {
	return func(w *Watcher) {
		if l != nil {
			w.logger = l
		}
	}
}

// withClock overrides the freshness clock so tests can advance time without
// sleeping. Unexported: freshness must be wall-clock in production.
func withClock(fn func() time.Time) Option {
	return func(w *Watcher) {
		if fn != nil {
			w.now = fn
		}
	}
}

// withRebindDelay shortens the post-failure rebind wait in tests.
func withRebindDelay(d time.Duration) Option {
	return func(w *Watcher) {
		if d > 0 {
			w.rebindDelay = d
		}
	}
}

// withAppliedHook installs the test synchronization channel.
func withAppliedHook(ch chan struct{}) Option {
	return func(w *Watcher) { w.applied = ch }
}

// NewWatcher builds a watcher for one producer key (KeyGraphIndex,
// KeyGraphEmbedding) in the GRAPH_STATUS bucket. It performs no I/O; call Start.
func NewWatcher(src BucketSource, key string, opts ...Option) *Watcher {
	w := &Watcher{
		src:         src,
		bucket:      BucketGraphStatus,
		key:         key,
		heartbeat:   DefaultHeartbeat,
		rebindDelay: defaultRebindDelay,
		now:         time.Now,
		logger:      slog.Default(),
		first:       make(chan struct{}),
	}
	for _, opt := range opts {
		opt(w)
	}
	return w
}

// Start begins watching in the background and returns immediately. It does NOT
// fail when the bucket or key is absent: a deployment running a consumer without
// its producer is a legitimate configuration whose readiness is simply UNKNOWN, and
// Read reports it as such (fail closed) while the watcher keeps trying to bind. The
// caller's own escape hatch (e.g. allow_ungated_reads) is what covers that case —
// never a fabricated envelope.
//
// Start is idempotent; subsequent calls are no-ops.
func (w *Watcher) Start(ctx context.Context) error {
	if w.src == nil {
		return errors.New("readiness: nil bucket source")
	}
	if w.key == "" {
		return errors.New("readiness: empty status key")
	}
	w.startOnce.Do(func() {
		runCtx, cancel := context.WithCancel(ctx)
		w.cancel = cancel
		w.done = make(chan struct{})
		go w.run(runCtx)
	})
	return nil
}

// Stop cancels the watch and waits for the background goroutine to exit. It is safe
// to call without Start and safe to call more than once.
func (w *Watcher) Stop() {
	if w.cancel == nil {
		return
	}
	w.cancel()
	<-w.done
}

// Read returns the current reading. It never blocks on I/O — the answer comes from
// held state, which is the point of watching rather than polling.
func (w *Watcher) Read() Reading {
	w.mu.Lock()
	defer w.mu.Unlock()
	r := Reading{Status: w.status, Raw: w.raw, Known: w.known, Err: w.lastErr}
	if !w.known {
		return r
	}
	r.Age = w.now().Sub(w.lastArrival)
	// A negative age (clock stepped backwards) is still within the window: the
	// update did arrive, and the comparison is entirely local.
	r.Fresh = !w.staleOnArrival && r.Age <= w.freshnessWindow()
	return r
}

// WaitForFirst blocks until an envelope has been received on the key, or ctx ends. It
// returns nil once something has arrived (fresh or not — Read is what judges that) and
// ctx.Err() otherwise.
//
// It exists for the LAZILY-BOUND consumers (the graph/query client, fusion): they bind
// the watch on their first gate decision, and without a bounded wait that first
// decision would fail closed purely because the watch had not delivered yet — a
// regression against the request/reply this replaces, which always got an answer or an
// error. Waiting once at bind, bounded by the same timeout the request used, keeps the
// worst-case latency of the first gated call unchanged while every later call becomes a
// local read. Callers must wait AT MOST once: with no producer deployed nothing will
// ever arrive, and a per-call wait would turn the standalone deployment into a
// per-call stall.
func (w *Watcher) WaitForFirst(ctx context.Context) error {
	select {
	case <-w.first:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

// freshnessWindow is how long a held status stays fresh.
func (w *Watcher) freshnessWindow() time.Duration {
	return FreshnessWindow(w.heartbeat)
}

// run binds the watch and rebinds after any failure until the context ends.
func (w *Watcher) run(ctx context.Context) {
	defer close(w.done)
	for {
		if err := w.watchOnce(ctx); err != nil && ctx.Err() == nil {
			w.recordErr(err)
			w.logger.Debug("readiness watch unavailable",
				slog.String("bucket", w.bucket), slog.String("key", w.key), slog.Any("error", err))
		}
		select {
		case <-ctx.Done():
			return
		case <-time.After(w.rebindDelay):
		}
	}
}

// watchOnce opens the bucket and the key watch and consumes updates until the watch
// ends or the context is cancelled. A missing bucket is an ordinary error here —
// held state stays unknown and the caller retries.
func (w *Watcher) watchOnce(ctx context.Context) error {
	bucket, err := w.src.GetKeyValueBucket(ctx, w.bucket)
	if err != nil {
		return fmt.Errorf("readiness: open bucket %s: %w", w.bucket, err)
	}
	// No UpdatesOnly: the current value must be delivered immediately on bind, so a
	// restarted consumer is fresh within one delivery rather than one heartbeat.
	watcher, err := bucket.Watch(ctx, w.key)
	if err != nil {
		return fmt.Errorf("readiness: watch %s/%s: %w", w.bucket, w.key, err)
	}
	defer func() {
		if stopErr := watcher.Stop(); stopErr != nil {
			w.logger.Debug("readiness watch stop", slog.Any("error", stopErr))
		}
	}()

	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case entry, ok := <-watcher.Updates():
			if !ok {
				return fmt.Errorf("readiness: watch %s/%s closed", w.bucket, w.key)
			}
			// nil is the end-of-initial-values marker, not a value. An absent key
			// simply means no envelope has ever been published: stay unknown.
			if entry == nil {
				continue
			}
			w.apply(entry)
		}
	}
}

// apply folds one delivered entry into held state and stamps the CONSUMER-LOCAL
// arrival time. A tombstone or an undecodable value clears knownness instead of
// refreshing it: neither is evidence the producer is alive and healthy, and letting
// either stamp an arrival time would make garbage read as fresh.
func (w *Watcher) apply(entry jetstream.KeyValueEntry) {
	defer w.signalApplied()

	if graph.IsKVTombstone(entry.Operation()) {
		w.mu.Lock()
		w.known = false
		w.raw = nil
		w.staleOnArrival = false
		w.lastErr = fmt.Errorf("readiness: status key %s/%s deleted", w.bucket, w.key)
		w.mu.Unlock()
		return
	}

	// The KV value is plain graph.IndexStatusResponse JSON — the same struct fusion
	// decodes — not a payload-registry envelope: this is operational state written
	// to KV, not a published message payload.
	var status graph.IndexStatusResponse
	if err := json.Unmarshal(entry.Value(), &status); err != nil {
		w.recordErr(fmt.Errorf("readiness: decode %s/%s: %w", w.bucket, w.key, err))
		w.logger.Warn("readiness status undecodable",
			slog.String("bucket", w.bucket), slog.String("key", w.key), slog.Any("error", err))
		return
	}

	arrival := w.now()

	// THE ONE COMMIT-TIME COMPARISON, and it applies only to a value that was already
	// old when it reached us. A watch delivers the key's CURRENT value the instant it
	// binds, so a consumer that starts while its producer is dead receives that
	// producer's final envelope — possibly Ready=true, written an hour ago — and pure
	// arrival-time freshness would stamp it as brand new and read it as live for a
	// whole window. That is a fail-OPEN neither the request/reply (no responders) nor
	// the gate it feeds ever had, and the envelope's authoritative-absence license
	// makes it expensive. Steady-state freshness still compares no clocks (D2): once a
	// live producer's heartbeat lands, arrival time governs, and the 3x window absorbs
	// ordinary skew, so no healthy producer is ever rejected here.
	staleOnArrival := false
	var staleErr error
	if created := entry.Created(); !created.IsZero() {
		if age := arrival.Sub(created); age > w.freshnessWindow() {
			staleOnArrival = true
			// Attributable, because this rejection is otherwise INVISIBLE: knownness
			// stays true and the arrival stamp is recent, so a consumer's defer log
			// would read "status_known=true status_age=1ms reason=status_unknown"
			// with nothing explaining it. It also names the one skew-sensitive
			// comparison in the package — a consumer clock far enough ahead of the
			// NATS server rejects a healthy producer's every write, and this is the
			// line that says so instead of blaming the producer.
			staleErr = fmt.Errorf(
				"readiness: %s/%s was already %s old on arrival (window %s) — dead producer, or consumer clock ahead of the server",
				w.bucket, w.key, age.Round(time.Millisecond), w.freshnessWindow())
		}
	}

	w.mu.Lock()
	w.status = status
	w.raw = entry.Value()
	w.lastArrival = arrival
	w.known = true
	w.staleOnArrival = staleOnArrival
	w.lastErr = staleErr
	w.mu.Unlock()

	// Signalled for ANY held envelope, stale or not: a waiting consumer wants the
	// first answer, and "the key holds a dead producer's last word" is an answer. Read
	// is what judges it.
	w.firstOnce.Do(func() { close(w.first) })
}

// recordErr stores the last failure for the structured defer log without touching
// the arrival stamp — an error must never extend freshness.
func (w *Watcher) recordErr(err error) {
	w.mu.Lock()
	w.lastErr = err
	w.mu.Unlock()
}

// signalApplied notifies the test hook, if installed, without ever blocking the
// watch goroutine.
func (w *Watcher) signalApplied() {
	if w.applied == nil {
		return
	}
	select {
	case w.applied <- struct{}{}:
	default:
	}
}

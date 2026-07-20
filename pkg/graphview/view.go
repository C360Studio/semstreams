package graphview

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/nats-io/nats.go/jetstream"
)

// DefaultTickInterval is the fan-out coalescing window used when no
// WithTickInterval option is given.
const DefaultTickInterval = 250 * time.Millisecond

// EntryMeta carries the authoritative KV entry metadata into the validating
// decode: the operation's revision and the server write timestamp
// (entry.Created()). Revision duplicates Delta.Revision deliberately —
// decoded records that embed their own write metadata should not need to
// reach back into the delta lane for it.
type EntryMeta struct {
	// Revision is the KV revision of the write being decoded.
	Revision uint64
	// Created is the KV server timestamp of the write (entry.Created()).
	Created time.Time
}

// DecodeFunc decodes and validates one stored KV value; meta carries the
// entry's authoritative revision and server write time. keep=false means the
// key is not part of the view (non-record keys, present-but-not-ready
// records) and maps it to absence; a non-nil err poisons the key (G6) — the
// value is never delivered as an upsert while other keys keep flowing.
// Decode runs exactly once per delivered write, on the watcher goroutine,
// regardless of subscriber count.
type DecodeFunc[T any] func(key string, value []byte, meta EntryMeta) (decoded T, keep bool, err error)

// WatcherSource is the narrow method set the view needs from a KV bucket.
// jetstream.KeyValue satisfies it; unit tests inject a deterministic fake.
type WatcherSource interface {
	WatchAll(ctx context.Context, opts ...jetstream.WatchOpt) (jetstream.KeyWatcher, error)
}

// Compile-time proof that a real bucket handle is a WatcherSource.
var _ WatcherSource = (jetstream.KeyValue)(nil)

// DeltaOp, Delta, Entry, KeyedEntry, and Snapshot live in delta.go.

// Hooks receives view-internal signals. All callbacks are optional, are
// invoked outside the projection mutex, and MUST be fast and non-blocking —
// they run on the watcher and ticker goroutines. This is the observability
// seam for metrics (ADR-081 build task 2.5); tests use it for deterministic
// synchronization.
type Hooks struct {
	// OnApply fires after every delivered entry has been applied (including
	// skips and tombstones), with the entry's key and KV revision.
	OnApply func(key string, revision uint64)
	// OnCaughtUp fires when the view transitions to caught-up (initial
	// bootstrap and every successful restart).
	OnCaughtUp func()
	// OnWatcherLost fires once per watcher loss with the cause.
	OnWatcherLost func(err error)
	// OnPoison fires per poisoned write with the key and *PoisonError.
	OnPoison func(key string, err error)
	// OnTick fires after every processed tick window with the number of
	// changed keys detached and the number of subscribers fanned out to.
	OnTick func(changedKeys, subscribers int)
	// OnSubscribers fires with the new subscriber count after every attach
	// and every effective detach (Unsubscribe, subscription-context cancel,
	// and the batch detaches at Stop and watcher loss, which report zero).
	// Counts are delivered outside the projection mutex, so under concurrent
	// attach/detach two callbacks may arrive out of order — treat the value
	// as an eventually-correct gauge, not an ordered event stream.
	OnSubscribers func(n int)
	// OnFanOut fires after every tick window that enqueued at least one
	// changed key to at least one subscriber, with the number of pending
	// deltas that were overwritten before delivery across all subscribers in
	// the window (the at-most-once coalescing drop on undrained buffers —
	// the slow-subscriber staleness signal) and the largest per-subscriber
	// pending-buffer size after enqueue.
	OnFanOut func(overwritten, maxPending int)
}

type options struct {
	tickInterval time.Duration
	tickCh       <-chan time.Time
	hooks        Hooks
}

// Option configures a View at construction.
type Option func(*options)

// WithTickInterval sets the fan-out coalescing window (default
// DefaultTickInterval). Non-positive values fall back to the default.
func WithTickInterval(d time.Duration) Option {
	return func(o *options) { o.tickInterval = d }
}

// WithHooks installs observability hooks.
func WithHooks(h Hooks) Option {
	return func(o *options) { o.hooks = h }
}

// withTickChannel replaces the internal ticker with a caller-driven channel
// so unit tests fire tick windows deterministically.
func withTickChannel(ch <-chan time.Time) Option {
	return func(o *options) { o.tickCh = ch }
}

type viewState uint8

const (
	stateNew viewState = iota
	stateBootstrap
	stateLive
	stateFailed
	stateStopped
)

type entryState[T any] struct {
	value    T
	revision uint64
}

// pendingOp is a coalesced per-key operation awaiting the next tick. seq is
// the view apply-sequence of the op, used to filter changes already covered
// by a subscriber's snapshot (op.seq <= attachSeq).
type pendingOp[T any] struct {
	delta Delta[T]
	seq   uint64
}

// View maintains one authoritative in-memory projection of a KV bucket from
// a single WatchAll and fans coalesced deltas out to local subscribers. It
// is explicitly constructed and owned — inject it into consumers; there is
// no process-global registry. All methods are safe for concurrent use.
type View[T any] struct {
	source       WatcherSource
	decode       DecodeFunc[T]
	tickInterval time.Duration
	tickCh       <-chan time.Time
	hooks        Hooks

	ctx    context.Context
	cancel context.CancelFunc
	wg     sync.WaitGroup

	// mu is THE projection mutex: apply, attach (snapshot+register), tick
	// fan-out, and point reads all serialize on it (see package doc).
	mu         sync.Mutex
	state      viewState
	cause      error
	entries    map[string]entryState[T]
	poisoned   map[string]*PoisonError
	pending    map[string]pendingOp[T]
	seq        uint64
	appliedRev uint64
	readyCh    chan struct{}
	subs       map[uint64]*Subscription[T]
	nextSubID  uint64
}

// New constructs a View over source with the injected validating decode.
// The view does not touch the bucket until Start.
func New[T any](source WatcherSource, decode DecodeFunc[T], opts ...Option) (*View[T], error) {
	if source == nil {
		return nil, errors.New("graphview: nil watcher source")
	}
	if decode == nil {
		return nil, errors.New("graphview: nil decode func")
	}
	o := options{tickInterval: DefaultTickInterval}
	for _, opt := range opts {
		opt(&o)
	}
	if o.tickInterval <= 0 {
		o.tickInterval = DefaultTickInterval
	}
	return &View[T]{
		source:       source,
		decode:       decode,
		tickInterval: o.tickInterval,
		tickCh:       o.tickCh,
		hooks:        o.hooks,
		entries:      make(map[string]entryState[T]),
		poisoned:     make(map[string]*PoisonError),
		pending:      make(map[string]pendingOp[T]),
		readyCh:      make(chan struct{}),
		subs:         make(map[uint64]*Subscription[T]),
	}, nil
}

// Start opens the single WatchAll and begins bootstrap. ctx bounds the life
// of the watcher and ticker; its cancellation is a watcher-loss event (fail
// closed), not a clean shutdown — use Stop for that.
func (v *View[T]) Start(ctx context.Context) error {
	v.mu.Lock()
	switch v.state {
	case stateNew:
	case stateStopped:
		v.mu.Unlock()
		return ErrViewStopped
	default:
		v.mu.Unlock()
		return ErrAlreadyStarted
	}
	v.ctx, v.cancel = context.WithCancel(ctx)
	v.state = stateBootstrap
	v.mu.Unlock()

	v.wg.Add(1)
	go v.runTicker()

	return v.openWatcher()
}

// Restart re-bootstraps a failed view: it opens a fresh WatchAll, replays,
// reconciles ghost keys (keys absent from the fresh replay are removed), and
// only then reports caught-up again (G5). Subscribers terminated by the loss
// must re-attach for a coherent snapshot. The new watcher inherits the Start
// context — jetstream binds watcher lifetime to the WatchAll context, so a
// per-call context would kill the watcher when the call returned; if the
// Start context itself was cancelled, Restart fails.
func (v *View[T]) Restart() error {
	v.mu.Lock()
	switch v.state {
	case stateFailed:
	case stateStopped:
		v.mu.Unlock()
		return ErrViewStopped
	default:
		v.mu.Unlock()
		return errors.New("graphview: restart requires a failed view")
	}
	v.state = stateBootstrap
	v.cause = nil
	v.pending = make(map[string]pendingOp[T])
	v.mu.Unlock()
	return v.openWatcher()
}

func (v *View[T]) openWatcher() error {
	watcher, err := v.source.WatchAll(v.ctx)
	if err != nil {
		v.mu.Lock()
		if v.state == stateBootstrap {
			v.state = stateFailed
			v.cause = err
			v.rotateReadyLocked()
		}
		v.mu.Unlock()
		return fmt.Errorf("graphview: open WatchAll: %w", err)
	}
	// Re-check state under the lock: Stop (or another transition) may have
	// won while the WatchAll I/O was in flight. Without this, (a) wg.Add
	// could race a concurrent wg.Wait (WaitGroup reuse window) and (b) a
	// successfully opened watcher would survive Stop as a zombie goroutine
	// holding a live NATS consumer. Spawning under the lock pairs the
	// state-check with the wg.Add, so Stop either sees the registered
	// goroutine and waits for it, or wins outright and the watcher is
	// stopped here instead of leaked.
	v.mu.Lock()
	if v.state != stateBootstrap {
		stateErr := v.usableLocked()
		v.mu.Unlock()
		_ = watcher.Stop()
		if stateErr == nil {
			stateErr = ErrAlreadyStarted
		}
		return fmt.Errorf("graphview: watcher opened after state transition: %w", stateErr)
	}
	v.wg.Add(1)
	go v.runWatcher(watcher)
	v.mu.Unlock()
	return nil
}

// Stop shuts the view down: every subscription receives an explicit terminal
// close (Err reports ErrViewStopped — never a silent hang), the watcher and
// ticker exit, and all further operations return ErrViewStopped.
func (v *View[T]) Stop() {
	v.mu.Lock()
	if v.state == stateStopped {
		v.mu.Unlock()
		return
	}
	v.state = stateStopped
	v.cause = ErrViewStopped
	subs := make([]*Subscription[T], 0, len(v.subs))
	for _, s := range v.subs {
		subs = append(subs, s)
	}
	v.subs = make(map[uint64]*Subscription[T])
	v.pending = make(map[string]pendingOp[T])
	v.rotateReadyLocked()
	cancel := v.cancel
	v.mu.Unlock()

	for _, s := range subs {
		s.terminate(ErrViewStopped)
	}
	if len(subs) > 0 && v.hooks.OnSubscribers != nil {
		v.hooks.OnSubscribers(0)
	}
	if cancel != nil {
		cancel()
	}
	v.wg.Wait()
}

// WaitCaughtUp blocks until the view is caught up (nil), the view fails
// (wrapped ErrWatcherLost), the view stops (ErrViewStopped), or ctx is done.
func (v *View[T]) WaitCaughtUp(ctx context.Context) error {
	for {
		v.mu.Lock()
		switch v.state {
		case stateLive:
			v.mu.Unlock()
			return nil
		case stateFailed:
			err := v.failedErrLocked()
			v.mu.Unlock()
			return err
		case stateStopped:
			v.mu.Unlock()
			return ErrViewStopped
		default:
		}
		ready := v.readyCh
		v.mu.Unlock()
		select {
		case <-ctx.Done():
			return fmt.Errorf("graphview: wait caught up: %w", ctx.Err())
		case <-ready:
		}
	}
}

// CaughtUp reports whether the view is live: initial replay complete and the
// shared watcher healthy (readiness = caught-up, not started).
func (v *View[T]) CaughtUp() bool {
	v.mu.Lock()
	defer v.mu.Unlock()
	return v.state == stateLive
}

// AppliedRevision returns the applied-revision watermark: the highest KV
// revision the view has applied. WatchAll delivery is ordered, so the
// watermark reaching R proves every delivered revision <= R was applied.
func (v *View[T]) AppliedRevision() uint64 {
	v.mu.Lock()
	defer v.mu.Unlock()
	return v.appliedRev
}

// Poisoned returns a copy of the current per-key poison records (G6). It is
// a diagnostic surface and is served in every state.
func (v *View[T]) Poisoned() map[string]*PoisonError {
	v.mu.Lock()
	defer v.mu.Unlock()
	out := make(map[string]*PoisonError, len(v.poisoned))
	for k, p := range v.poisoned {
		out[k] = p
	}
	return out
}

// Get is the coherent point read: it serves the projection under the same
// mutex applies run under, so it can never return a value older than one the
// view already applied. It honors readiness (ErrNotReady before caught-up,
// wrapped ErrWatcherLost after a loss, ErrViewStopped after Stop) and poison
// (a poisoned key returns its *PoisonError, never a stale value).
func (v *View[T]) Get(key string) (T, uint64, error) {
	var zero T
	v.mu.Lock()
	defer v.mu.Unlock()
	if err := v.usableLocked(); err != nil {
		return zero, 0, err
	}
	if p, ok := v.poisoned[key]; ok {
		return zero, p.Revision, p
	}
	e, ok := v.entries[key]
	if !ok {
		return zero, 0, ErrKeyNotFound
	}
	return e.value, e.revision, nil
}

// List returns projection entries whose key has the given prefix (empty
// prefix matches all), sorted by key, truncated to limit when limit > 0. The
// complete match set is sorted before the limit is applied (deterministic
// bounded reads). Poisoned keys are excluded — they have no servable value.
// It honors the same readiness gate as Get.
func (v *View[T]) List(prefix string, limit int) ([]KeyedEntry[T], error) {
	v.mu.Lock()
	defer v.mu.Unlock()
	if err := v.usableLocked(); err != nil {
		return nil, err
	}
	out := make([]KeyedEntry[T], 0, len(v.entries))
	for k, e := range v.entries {
		if prefix != "" && !strings.HasPrefix(k, prefix) {
			continue
		}
		out = append(out, KeyedEntry[T]{Key: k, Value: e.value, Revision: e.revision})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Key < out[j].Key })
	if limit > 0 && len(out) > limit {
		out = out[:limit]
	}
	return out, nil
}

// SnapshotAndSubscribe atomically captures a snapshot and registers the
// subscription at one view sequence S under the projection mutex (G1): the
// snapshot holds every change applied at <= S, the subscription delivers
// exactly the changes > S. Capture holds the lock (bounded copy); delivery
// to the caller happens after unlock. ctx bounds the subscription: its
// cancellation detaches like Unsubscribe. Fails with ErrNotReady until the
// view is caught up.
func (v *View[T]) SnapshotAndSubscribe(ctx context.Context) (Snapshot[T], *Subscription[T], error) {
	v.mu.Lock()
	if err := v.usableLocked(); err != nil {
		v.mu.Unlock()
		return Snapshot[T]{}, nil, err
	}
	snap := Snapshot[T]{
		Entries:  make(map[string]Entry[T], len(v.entries)),
		Poisoned: make(map[string]*PoisonError, len(v.poisoned)),
		Sequence: v.seq,
		Revision: v.appliedRev,
	}
	for k, e := range v.entries {
		snap.Entries[k] = Entry[T]{Value: e.value, Revision: e.revision}
	}
	for k, p := range v.poisoned {
		snap.Poisoned[k] = p
	}
	s := v.attachLocked(ctx)
	n := len(v.subs)
	v.mu.Unlock()
	go s.run()
	if v.hooks.OnSubscribers != nil {
		v.hooks.OnSubscribers(n)
	}
	return snap, s, nil
}

// Subscribe is the delta-only attach for trigger-shaped consumers: no
// snapshot, no second projection copy — the subscription delivers the
// changes after the attach sequence. Same readiness gate as
// SnapshotAndSubscribe.
func (v *View[T]) Subscribe(ctx context.Context) (*Subscription[T], error) {
	v.mu.Lock()
	if err := v.usableLocked(); err != nil {
		v.mu.Unlock()
		return nil, err
	}
	s := v.attachLocked(ctx)
	n := len(v.subs)
	v.mu.Unlock()
	go s.run()
	if v.hooks.OnSubscribers != nil {
		v.hooks.OnSubscribers(n)
	}
	return s, nil
}

// attachLocked registers a subscription at the current apply sequence.
// Caller holds v.mu.
func (v *View[T]) attachLocked(ctx context.Context) *Subscription[T] {
	id := v.nextSubID
	v.nextSubID++
	s := &Subscription[T]{
		view:      v,
		id:        id,
		attachSeq: v.seq,
		ctx:       ctx,
		pending:   make(map[string]Delta[T]),
		notify:    make(chan struct{}, 1),
		deltas:    make(chan []Delta[T], 1),
		done:      make(chan struct{}),
	}
	v.subs[id] = s
	v.wg.Add(1)
	return s
}

func (v *View[T]) removeSub(id uint64) {
	v.mu.Lock()
	before := len(v.subs)
	delete(v.subs, id)
	n := len(v.subs)
	v.mu.Unlock()
	if before != n && v.hooks.OnSubscribers != nil {
		v.hooks.OnSubscribers(n)
	}
}

// usableLocked returns the readiness error for the current state, nil when
// live. Caller holds v.mu.
func (v *View[T]) usableLocked() error {
	switch v.state {
	case stateLive:
		return nil
	case stateStopped:
		return ErrViewStopped
	case stateFailed:
		return v.failedErrLocked()
	default:
		return ErrNotReady
	}
}

// failedErrLocked wraps the fail-closed state so callers can match both
// not-ready semantics (errors.Is ErrNotReady) and the loss itself
// (errors.Is ErrWatcherLost). Caller holds v.mu.
func (v *View[T]) failedErrLocked() error {
	return fmt.Errorf("%w: %w: %w", ErrNotReady, ErrWatcherLost, v.cause)
}

// rotateReadyLocked wakes every WaitCaughtUp waiter on a state transition.
// Caller holds v.mu.
func (v *View[T]) rotateReadyLocked() {
	close(v.readyCh)
	v.readyCh = make(chan struct{})
}

// runWatcher is the single projection writer: it applies every delivered
// entry in watch order and detects the end-of-replay marker and watcher
// loss. One instance runs per watcher generation (Start, then each Restart).
func (v *View[T]) runWatcher(watcher jetstream.KeyWatcher) {
	defer v.wg.Done()
	defer func() { _ = watcher.Stop() }()
	// seen tracks keys delivered during bootstrap replay for ghost-key
	// reconciliation at the caught-up transition; released once live.
	seen := make(map[string]struct{})
	for {
		select {
		case <-v.ctx.Done():
			v.failClosed(fmt.Errorf("graphview: watch context cancelled: %w", v.ctx.Err()))
			return
		case entry, ok := <-watcher.Updates():
			if !ok {
				v.failClosed(errors.New("graphview: watcher updates channel closed"))
				return
			}
			if entry == nil {
				v.markCaughtUp(seen)
				seen = nil
				continue
			}
			v.processEntry(entry, seen)
		}
	}
}

// processEntry decodes (once) and applies one delivered entry. Runs only on
// the watcher goroutine; decode happens outside the projection mutex.
func (v *View[T]) processEntry(entry jetstream.KeyValueEntry, seen map[string]struct{}) {
	key, rev, created := entry.Key(), entry.Revision(), entry.Created()
	switch entry.Operation() {
	case jetstream.KeyValueDelete, jetstream.KeyValuePurge:
		v.apply(Delta[T]{Op: DeltaDelete, Key: key, Revision: rev, Created: created}, nil, seen)
	default:
		val, keep, err := v.decode(key, entry.Value(), EntryMeta{Revision: rev, Created: created})
		switch {
		case err != nil:
			p := &PoisonError{Key: key, Revision: rev, Err: err}
			v.apply(Delta[T]{Op: DeltaPoison, Key: key, Revision: rev, Created: created, Err: p}, p, seen)
		case !keep:
			v.applySkip(key, rev, created)
		default:
			v.apply(Delta[T]{Op: DeltaUpsert, Key: key, Value: val, Revision: rev, Created: created}, nil, seen)
		}
	}
	if v.hooks.OnApply != nil {
		v.hooks.OnApply(key, rev)
	}
}

// apply commits one operation to the projection and, when live, to the
// pending coalescing set (LWW per key — watch order is revision-ascending,
// so overwriting is last-writer-wins).
func (v *View[T]) apply(d Delta[T], p *PoisonError, seen map[string]struct{}) {
	v.mu.Lock()
	v.seq++
	if d.Revision > v.appliedRev {
		v.appliedRev = d.Revision
	}
	switch d.Op {
	case DeltaUpsert:
		v.entries[d.Key] = entryState[T]{value: d.Value, revision: d.Revision}
		delete(v.poisoned, d.Key)
	case DeltaDelete:
		delete(v.entries, d.Key)
		delete(v.poisoned, d.Key)
	case DeltaPoison:
		// The failed value is not servable and the prior value is older than
		// an applied revision — serving either would violate coherence. The
		// key holds a sticky poison record until a newer canonical write
		// replaces it (per-entity heal) or a delete removes it.
		delete(v.entries, d.Key)
		v.poisoned[d.Key] = p
	}
	if seen != nil {
		seen[d.Key] = struct{}{}
	}
	if v.state == stateLive {
		v.pending[d.Key] = pendingOp[T]{delta: d, seq: v.seq}
	}
	v.mu.Unlock()
	if d.Op == DeltaPoison && v.hooks.OnPoison != nil {
		v.hooks.OnPoison(d.Key, p)
	}
}

// applySkip handles keep=false decodes: the key is not part of the view, so
// it maps to absence. A previously-kept key converges subscribers via a
// tombstone at this revision; otherwise only the watermark advances. The key
// is deliberately NOT marked seen during bootstrap — absent is its truth.
func (v *View[T]) applySkip(key string, rev uint64, created time.Time) {
	v.mu.Lock()
	defer v.mu.Unlock()
	v.seq++
	if rev > v.appliedRev {
		v.appliedRev = rev
	}
	_, hadEntry := v.entries[key]
	_, hadPoison := v.poisoned[key]
	if !hadEntry && !hadPoison {
		return
	}
	delete(v.entries, key)
	delete(v.poisoned, key)
	if v.state == stateLive {
		v.pending[key] = pendingOp[T]{delta: Delta[T]{Op: DeltaDelete, Key: key, Revision: rev, Created: created}, seq: v.seq}
	}
}

// markCaughtUp transitions bootstrap -> live at the end-of-replay marker,
// after reconciling ghost keys: any projection key the fresh replay did not
// deliver no longer exists in the bucket (its tombstone may have been
// purge-cleaned while the watcher was down) and is removed BEFORE caught-up
// is reported (G5 re-bootstrap reconciliation).
func (v *View[T]) markCaughtUp(seen map[string]struct{}) {
	v.mu.Lock()
	if v.state != stateBootstrap {
		v.mu.Unlock()
		return
	}
	for k := range v.entries {
		if _, ok := seen[k]; !ok {
			delete(v.entries, k)
		}
	}
	for k := range v.poisoned {
		if _, ok := seen[k]; !ok {
			delete(v.poisoned, k)
		}
	}
	v.state = stateLive
	v.rotateReadyLocked()
	v.mu.Unlock()
	if v.hooks.OnCaughtUp != nil {
		v.hooks.OnCaughtUp()
	}
}

// failClosed marks the view failed and terminates every subscription with an
// explicit staleness signal (G5): the frozen projection is never silently
// served as live, and new attaches observe not-ready semantics.
func (v *View[T]) failClosed(cause error) {
	v.mu.Lock()
	if v.state == stateStopped || v.state == stateFailed {
		v.mu.Unlock()
		return
	}
	v.state = stateFailed
	v.cause = cause
	v.pending = make(map[string]pendingOp[T])
	subs := make([]*Subscription[T], 0, len(v.subs))
	for _, s := range v.subs {
		subs = append(subs, s)
	}
	v.subs = make(map[uint64]*Subscription[T])
	v.rotateReadyLocked()
	v.mu.Unlock()

	term := fmt.Errorf("%w: %w", ErrWatcherLost, cause)
	for _, s := range subs {
		s.terminate(term)
	}
	if len(subs) > 0 && v.hooks.OnSubscribers != nil {
		v.hooks.OnSubscribers(0)
	}
	if v.hooks.OnWatcherLost != nil {
		v.hooks.OnWatcherLost(cause)
	}
}

// runTicker drives the fan-out window.
func (v *View[T]) runTicker() {
	defer v.wg.Done()
	tick := v.tickCh
	if tick == nil {
		t := time.NewTicker(v.tickInterval)
		defer t.Stop()
		tick = t.C
	}
	for {
		select {
		case <-v.ctx.Done():
			return
		case <-tick:
			v.fanOut()
		}
	}
}

// fanOut detaches the coalesced window and enqueues it to every subscriber.
//
// COHERENCE (G1, the tick seam — load-bearing): batch detach, subscriber-set
// iteration, and per-subscriber enqueue are ONE critical section under the
// projection mutex. A detached batch never exists outside the lock, so no
// apply can supersede a captured value and no attach can interleave between
// capture and enqueue — the PR #583 stale-delivery interleaving (batch holds
// K@R5, view applies K@R6, subscriber attaches snapshotting R6, batch then
// delivers R5) is structurally unrepresentable rather than revision-guarded
// at each enqueue site. The work under the lock is bounded map writes; the
// channel sends happen on per-subscriber goroutines outside the lock.
func (v *View[T]) fanOut() {
	v.mu.Lock()
	if v.state != stateLive {
		v.mu.Unlock()
		return
	}
	changed := len(v.pending)
	nsubs := len(v.subs)
	overwritten, maxPending := 0, 0
	if changed > 0 {
		ops := v.pending
		v.pending = make(map[string]pendingOp[T])
		// With no subscribers the detached ops are dropped: any future
		// subscriber attaches at a sequence >= every op.seq here, so the
		// attachSeq filter would suppress them anyway (its snapshot already
		// covers them).
		for _, s := range v.subs {
			ow, pending := v.enqueueLocked(s, ops)
			overwritten += ow
			if pending > maxPending {
				maxPending = pending
			}
		}
	}
	v.mu.Unlock()
	if v.hooks.OnTick != nil {
		v.hooks.OnTick(changed, nsubs)
	}
	if changed > 0 && nsubs > 0 && v.hooks.OnFanOut != nil {
		v.hooks.OnFanOut(overwritten, maxPending)
	}
}

// enqueueLocked merges the detached window into one subscriber's pending
// buffer (LWW per key, bounded by live changed-key cardinality — no
// count-based eviction; values shared, not copied). It reports the number of
// undrained pending deltas it overwrote (the at-most-once coalescing drop)
// and the buffer size after enqueue, for the OnFanOut observability hook.
// Caller holds v.mu; lock order is always v.mu -> s.mu.
func (v *View[T]) enqueueLocked(s *Subscription[T], ops map[string]pendingOp[T]) (overwritten, pending int) {
	s.mu.Lock()
	if s.pending == nil { // terminated concurrently
		s.mu.Unlock()
		return 0, 0
	}
	added := false
	for k, op := range ops {
		if op.seq <= s.attachSeq {
			// Already covered by this subscriber's snapshot (G1: no change
			// at sequence <= S is also delivered as a delta).
			continue
		}
		if _, exists := s.pending[k]; exists {
			overwritten++
		}
		// Blind overwrite is LWW: all enqueues happen under v.mu in apply
		// order, so op carries a revision >= any previously buffered op for
		// this key.
		s.pending[k] = op.delta
		added = true
	}
	pending = len(s.pending)
	s.mu.Unlock()
	if added {
		select {
		case s.notify <- struct{}{}:
		default:
		}
	}
	return overwritten, pending
}

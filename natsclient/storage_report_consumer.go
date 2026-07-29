// storage_report_consumer.go READS the published storage report
// (storage-observability). It is the other half of the KV twofer the report
// bucket was chosen for: storage_report.go writes one key per resource plus the
// reserved per-tier account row, and this file turns a Watch over those keys
// into the in-process view every operator surface serves from.
//
// The direction is the point. Prometheus metrics, health status, the operator
// HTTP route, and any alerting are CONSUMERS of the published bucket, never
// second report-producing paths. Growth, projection, and pressure are derived
// once, at publication, from inputs only the collection holds — the retained
// observation series and the account limits. A surface that recomputed any of
// them from what it happened to have in memory would eventually disagree with
// the bucket, and an operator cannot act on two answers.
//
// Watching this process's OWN writes is deliberate rather than wasteful. Any
// number of processes may publish account-wide, so a surface fed from the local
// collection would report the fraction of the account that one process last
// looked at; a surface fed from the bucket reports the account.
//
// Nothing here writes, rejects, throttles, or degrades anything.

package natsclient

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"sort"
	"sync"
	"time"

	"github.com/nats-io/nats.go/jetstream"

	"github.com/c360studio/semstreams/pkg/errs"
)

// DefaultReportConsumerRetryBackoff is how long the consumer waits before
// re-establishing a watch that could not be created or that ended.
const DefaultReportConsumerRetryBackoff = 5 * time.Second

// ReportWatchStore is the narrow KV capability the report consumer needs.
// jetstream.KeyValue satisfies it.
//
// WatchAll rather than a range read: on restart a consumer needs the current
// report immediately (KV re-delivers every current value, which is correct
// recovery), and several surfaces should each react rather than one dequeuing.
// Deletes are NOT filtered out — a reclaimed row must retract its metric series
// rather than leave a resource reporting forever after it is gone.
type ReportWatchStore interface {
	WatchAll(ctx context.Context, opts ...jetstream.WatchOpt) (jetstream.KeyWatcher, error)
}

// StorageReportObserver receives each change the consumer applies.
//
// It exists so a metrics surface can update ONE series per event instead of
// rebuilding every series on every tick. ForgetResource carries the whole last
// known row rather than a key, because a resource whose name is not a legal KV
// key is addressed by an opaque token — a bare key cannot say which label set a
// gauge was registered under.
//
// Implementations are called from the consumer's watch goroutine and must not
// block on it.
type StorageReportObserver interface {
	ObserveResource(row ResourceReport)
	ForgetResource(row ResourceReport)
	ObserveAccount(row AccountReport)
}

// StorageReportSnapshot is the whole published report as one in-process view.
//
// The counts are a TALLY of what the rows say, never a re-evaluation: pressure
// is derived at publication, and a consumer that recomputed it would be the
// second producer this design exists to prevent.
type StorageReportSnapshot struct {
	// Resources is every published resource row, sorted by resource name.
	Resources []ResourceReport

	// Account is the per-tier comparison, valid only when AccountKnown.
	Account      AccountReport
	AccountKnown bool

	// Synced reports that the watch has delivered every current value at least
	// once. Before it, an empty snapshot means "not read yet" rather than "the
	// account holds nothing" — the same distinction the inventory's Stale flag
	// keeps.
	Synced bool

	// UpdatedAt is when the last change was applied.
	UpdatedAt time.Time

	// PressureCounts tallies the EVALUATED rows by state.
	PressureCounts map[PressureState]int

	// NotEvaluated counts rows carrying no pressure state at all — unbounded or
	// unknown capacity. Counted rather than folded into normal: a surface that
	// filtered on "state != normal" would make exactly the unbounded resources
	// invisible.
	NotEvaluated int

	// WorstPressure is the worst evaluated state, or empty when nothing was
	// evaluated. Empty is NOT normal: an account whose every row declined to
	// evaluate has no pressure verdict, and reporting one would manufacture it.
	WorstPressure PressureState
}

// StorageReportConsumerConfig configures the report consumer.
type StorageReportConsumerConfig struct {
	// Observer receives each applied change. Optional: a consumer without one
	// still maintains its snapshot.
	Observer StorageReportObserver

	// RetryBackoff is the wait before re-establishing a watch. Defaults to
	// DefaultReportConsumerRetryBackoff.
	RetryBackoff time.Duration

	// Logger receives watch failures. Defaults to slog.Default().
	Logger *slog.Logger
}

// StorageReportConsumer maintains the in-process view of the published report.
type StorageReportConsumer struct {
	store    ReportWatchStore
	observer StorageReportObserver
	backoff  time.Duration
	logger   *slog.Logger

	mu       sync.RWMutex
	rows     map[string]ResourceReport
	account  AccountReport
	hasAcct  bool
	synced   bool
	updated  time.Time
	snapshot StorageReportSnapshot
}

// NewStorageReportConsumer builds a consumer over the report bucket.
func NewStorageReportConsumer(
	store ReportWatchStore, cfg StorageReportConsumerConfig,
) (*StorageReportConsumer, error) {
	if store == nil {
		return nil, errs.WrapInvalid(
			errors.New("a ReportWatchStore is required; bind the report bucket through the catalog "+
				"reader seam (graph.OpenCatalogBucket with graph.BucketStorageReport)"),
			"StorageReportConsumer", "New", "validate configuration")
	}
	consumer := &StorageReportConsumer{
		store:    store,
		observer: cfg.Observer,
		backoff:  cfg.RetryBackoff,
		logger:   cfg.Logger,
		rows:     make(map[string]ResourceReport, 32),
	}
	if consumer.backoff <= 0 {
		consumer.backoff = DefaultReportConsumerRetryBackoff
	}
	if consumer.logger == nil {
		consumer.logger = slog.Default()
	}
	consumer.rebuildLocked()
	return consumer, nil
}

// Snapshot returns the current view without doing any I/O. Safe to call from a
// health check, an HTTP handler, or a metrics scrape.
func (c *StorageReportConsumer) Snapshot() StorageReportSnapshot {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.snapshot.clone()
}

// Run watches the report bucket until ctx ends, re-establishing the watch when
// it cannot be created or when it ends.
//
// The retry is not optional resilience. A watch that dies leaves every operator
// surface frozen on its last values, and a frozen report is indistinguishable
// from a calm account — which is the exact failure this capability exists to
// end. Call Run in its own goroutine.
func (c *StorageReportConsumer) Run(ctx context.Context) {
	for {
		if ctx.Err() != nil {
			return
		}
		c.watchOnce(ctx)

		select {
		case <-ctx.Done():
			return
		case <-time.After(c.backoff):
		}
	}
}

// watchOnce establishes one watch and drains it until it ends.
func (c *StorageReportConsumer) watchOnce(ctx context.Context) {
	watcher, err := c.store.WatchAll(ctx)
	if err != nil {
		if ctx.Err() == nil {
			c.logger.Warn("could not watch the storage report; operator surfaces are serving their last values",
				slog.String("error", err.Error()))
		}
		return
	}
	defer func() { _ = watcher.Stop() }()

	updates := watcher.Updates()
	for {
		select {
		case <-ctx.Done():
			return
		case entry, open := <-updates:
			if !open {
				return
			}
			if entry == nil {
				// nats.go sends exactly one nil when every current value has
				// been delivered. It is a marker, not a row.
				c.markSynced()
				continue
			}
			c.apply(entry)
		}
	}
}

// apply folds one watch entry into the view and fans it out.
func (c *StorageReportConsumer) apply(entry jetstream.KeyValueEntry) {
	key := entry.Key()

	if entry.Operation() != jetstream.KeyValuePut {
		c.remove(key)
		return
	}

	// The reserved account key carries a different row kind. Discriminating on
	// the key is sound because a resource key can never contain a dot; see
	// StorageAccountReportKey.
	if key == StorageAccountReportKey {
		var report AccountReport
		if err := json.Unmarshal(entry.Value(), &report); err != nil {
			c.logSkip(key, entry.Revision(), err)
			return
		}
		c.putAccount(report)
		return
	}

	var row ResourceReport
	if err := json.Unmarshal(entry.Value(), &row); err != nil {
		// One undecodable value must not blank the account's whole view; the
		// next publication of that key repairs it.
		c.logSkip(key, entry.Revision(), err)
		return
	}
	c.putResource(key, row)
}

func (c *StorageReportConsumer) logSkip(key string, revision uint64, err error) {
	c.logger.Warn("skipping an undecodable storage report row",
		slog.String("key", key),
		slog.Uint64("revision", revision),
		slog.String("error", err.Error()))
}

func (c *StorageReportConsumer) putResource(key string, row ResourceReport) {
	c.mu.Lock()
	c.rows[key] = row
	c.updated = time.Now()
	c.rebuildLocked()
	c.mu.Unlock()

	if c.observer != nil {
		c.observer.ObserveResource(row)
	}
}

func (c *StorageReportConsumer) putAccount(report AccountReport) {
	c.mu.Lock()
	c.account = report
	c.hasAcct = true
	c.updated = time.Now()
	c.rebuildLocked()
	c.mu.Unlock()

	if c.observer != nil {
		c.observer.ObserveAccount(report)
	}
}

// remove drops a reclaimed row. A tombstone for a key this process never held
// fans out NOTHING: there is no series to retract and no identity to name.
func (c *StorageReportConsumer) remove(key string) {
	c.mu.Lock()
	row, held := c.rows[key]
	if held {
		delete(c.rows, key)
		c.updated = time.Now()
		c.rebuildLocked()
	}
	c.mu.Unlock()

	if held && c.observer != nil {
		c.observer.ForgetResource(row)
	}
}

func (c *StorageReportConsumer) markSynced() {
	c.mu.Lock()
	c.synced = true
	c.rebuildLocked()
	c.mu.Unlock()
}

// rebuildLocked recomputes the published snapshot. Callers hold the write lock,
// except the constructor, which has no concurrent readers yet.
func (c *StorageReportConsumer) rebuildLocked() {
	rows := make([]ResourceReport, 0, len(c.rows))
	counts := make(map[PressureState]int, 4)
	notEvaluated := 0
	worst := PressureState("")

	for _, row := range c.rows {
		rows = append(rows, row)
		if !row.Pressure.Evaluated {
			notEvaluated++
			continue
		}
		counts[row.Pressure.State]++
		if pressureSeverity(row.Pressure.State) > pressureSeverity(worst) || worst == "" {
			worst = row.Pressure.State
		}
	}
	sort.Slice(rows, func(i, j int) bool { return rows[i].Resource.Name < rows[j].Resource.Name })

	c.snapshot = StorageReportSnapshot{
		Resources:      rows,
		Account:        c.account,
		AccountKnown:   c.hasAcct,
		Synced:         c.synced,
		UpdatedAt:      c.updated,
		PressureCounts: counts,
		NotEvaluated:   notEvaluated,
		WorstPressure:  worst,
	}
}

// clone hands out a copy. Rows are value types all the way down apart from the
// pointer fields, which no consumer writes through, so copying the slice and
// the count map is a full copy for the purposes that matter: a caller ranging
// the snapshot cannot mutate the consumer's state under the watch goroutine.
func (s StorageReportSnapshot) clone() StorageReportSnapshot {
	out := s
	out.Resources = append(make([]ResourceReport, 0, len(s.Resources)), s.Resources...)
	out.PressureCounts = make(map[PressureState]int, len(s.PressureCounts))
	for state, count := range s.PressureCounts {
		out.PressureCounts[state] = count
	}
	return out
}

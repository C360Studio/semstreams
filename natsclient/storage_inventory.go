// storage_inventory.go is the ACCOUNT-SCOPED storage view
// (storage-observability): one inventory over every JetStream-backed resource
// the account holds — ordinary streams, KV backing streams, and ObjectStore
// backing streams — whether or not this process created, opened, or published
// to any of them.
//
// It is deliberately NOT the tracked-stream metrics in jetstream_metrics.go,
// which report "only streams and consumers that are created/accessed through
// this client". A resource created by a prior deploy, a sister process, or an
// operator out-of-band is exactly the resource most likely to be the growth
// problem, and it is precisely what a client-touched view cannot see. Both
// surfaces stay: this one is additional.
//
// Four properties are load-bearing and each has a test that fails if it is
// lost:
//
//  1. NO per-resource round-trip. Configuration and state are read from the
//     listing that returns both together, never from a describe call per
//     resource. The cost bound forbids O(N) round-trips, not a second PAGED
//     listing — and one is required for correctness, see reconcile below.
//  2. ACCOUNT-COMPLETE, including what cannot be read. The server moves any
//     stream carrying an offlineReason out of the info listing and into
//     Missing/Offline, which the Go client drops on the floor; the NAME listing
//     does not filter them. Names-minus-infos is therefore exactly the
//     unreadable set, and it is published named rather than silently omitted.
//  3. Attribution is a READ of the descriptor catalog, never a copy. The owner
//     resolver is a function called at collection time, so a bucket removed
//     from the catalog reports unattributed on the next collection and the
//     inventory can never disagree with the acquisition seam about who owns a
//     bucket.
//  4. Unknown, unbounded, and bounded are three distinct capacity states, and
//     unattributed is distinct from not-applicable. A resource reported healthy
//     because its capacity could not be read is worse than one not reported at
//     all, because it manufactures confidence.
//
// Collection is interval-driven with its own timeout and never runs on the
// component-start or health path; the read path returns last-good with the
// timestamp it was collected at. A monitoring surface that can take down the
// system it monitors is a worse bug than the blindness it fixes.

package natsclient

import (
	"context"
	"fmt"
	"log/slog"
	"math"
	"os"
	"sort"
	"strconv"
	"sync"
	"time"

	"github.com/nats-io/nats.go/jetstream"

	"github.com/c360studio/semstreams/pkg/errs"
)

// Collection defaults. Both are configuration: every process polling
// account-wide multiplies cost by deployment size, so an operator running many
// instances turns the interval down rather than losing the view entirely.
const (
	// DefaultStorageInventoryInterval is how often the account is enumerated
	// when the caller does not choose.
	DefaultStorageInventoryInterval = time.Minute
	// DefaultStorageInventoryTimeout bounds ONE collection, both listings
	// included. It is the collector's own bound, not the caller's, so an
	// unbounded caller context cannot make a collection unbounded.
	DefaultStorageInventoryTimeout = 15 * time.Second
)

// StreamLister is the narrow JetStream capability the inventory needs: the two
// paged account listings. jetstream.JetStream satisfies it.
//
// BOTH are required, and neither is a per-resource round-trip. ListStreams
// returns configuration and state together, which is what keeps the inventory
// off an N+1 describe path. StreamNames is the completeness check: the server
// excludes an offline stream from the info listing but not from the name
// listing, so without the second listing the inventory silently omits exactly
// the resources nobody can read.
//
// The interface is this narrow so the collector's classification, ordering,
// reconciliation, degradation, and timeout behavior are all testable without a
// server, while the production path still drives both real listings through an
// integration test.
type StreamLister interface {
	ListStreams(context.Context, ...jetstream.StreamListOpt) jetstream.StreamInfoLister
	StreamNames(context.Context, ...jetstream.StreamListOpt) jetstream.StreamNameLister
}

// StreamListerSource yields the account lister for ONE collection. It is
// resolved per collection rather than captured at construction, so a collector
// built before its client connects starts working when the client does, and one
// that outlives a reconnect never holds a stale JetStream context.
type StreamListerSource func() (StreamLister, error)

// OwnerResolver resolves a KV bucket name to its declared logical owner,
// returning "" for a bucket it does not declare.
//
// This is a FUNCTION, not a map, and that is the point: attribution must be a
// read of the one descriptor catalog at collection time. graph.OwnerOf
// satisfies it directly. A retained copy could disagree with the acquisition
// seam about who owns a bucket, and would keep reporting a former owner after
// the catalog dropped the row.
type OwnerResolver func(bucket string) string

// AccountStreamLister resolves this client's live JetStream context as the
// narrow account-listing capability the storage inventory needs. Pass the
// method value itself as a StreamListerSource so resolution happens per
// collection.
func (m *Client) AccountStreamLister() (StreamLister, error) {
	js, err := m.JetStream()
	if err != nil {
		return nil, err
	}
	return js, nil
}

// StorageInventoryConfig configures account storage collection.
type StorageInventoryConfig struct {
	// Interval is how often Run enumerates the account. Defaults to
	// DefaultStorageInventoryInterval.
	Interval time.Duration

	// Timeout bounds one collection. Defaults to
	// DefaultStorageInventoryTimeout.
	Timeout time.Duration

	// ProducedBy names this process in the report. Defaults to host/pid.
	ProducedBy string

	// OwnerResolver attributes KV resources. REQUIRED: with no resolver every
	// KV resource would report unattributed, which reads as "nothing is
	// framework-owned" rather than as "attribution was never wired", so the
	// constructor fails closed instead of defaulting.
	OwnerResolver OwnerResolver

	// Publisher writes the report after each SUCCESSFUL collection Run makes.
	// Optional: a collector without one keeps its in-process inventory and
	// publishes nothing, which is what the attribution unit tests want.
	//
	// It hangs off the collector rather than running its own timer because the
	// publication cadence IS the observation cadence — the growth series is
	// Δbytes over Δt across published observations, and a second timer could
	// drift from the interval the operator configured.
	Publisher InventoryPublisher

	// Logger receives collection-failure warnings. Defaults to slog.Default().
	Logger *slog.Logger
}

// InventoryPublisher publishes one collection's inventory.
// *StorageReportPublisher satisfies it; the interface keeps the collector from
// depending on the report's whole surface.
type InventoryPublisher interface {
	Publish(ctx context.Context, inv StorageInventory) (PublishResult, error)
}

// StorageInventoryCollector enumerates account storage on an interval and
// publishes the last good result.
//
// Collect performs all of its I/O outside the publication lock and takes the
// write lock only to swap the finished snapshot, so Latest never waits behind a
// collection. Nothing here belongs on a component's Start or health path.
type StorageInventoryCollector struct {
	source    StreamListerSource
	interval  time.Duration
	timeout   time.Duration
	producer  string
	ownerOf   OwnerResolver
	publisher InventoryPublisher
	logger    *slog.Logger

	// collectMu serializes collections so two overlapping calls cannot publish
	// out of order and walk CollectedAt backwards. It is NOT the publication
	// lock: Latest never touches it, so a read still never waits on I/O.
	collectMu sync.Mutex

	mu     sync.RWMutex
	latest StorageInventory
}

// NewStorageInventoryCollector builds a collector. It fails closed on a missing
// lister source or owner resolver rather than degrading to a silently
// unattributed inventory.
func NewStorageInventoryCollector(
	source StreamListerSource,
	cfg StorageInventoryConfig,
) (*StorageInventoryCollector, error) {
	if source == nil {
		return nil, errs.WrapInvalid(
			fmt.Errorf("a StreamListerSource is required"),
			"StorageInventoryCollector", "New", "validate configuration")
	}
	if cfg.OwnerResolver == nil {
		return nil, errs.WrapInvalid(
			fmt.Errorf("an OwnerResolver is required; pass graph.OwnerOf to attribute KV resources "+
				"from the bucket descriptor catalog, or an explicit always-unattributed resolver to "+
				"opt out deliberately"),
			"StorageInventoryCollector", "New", "validate configuration")
	}

	c := &StorageInventoryCollector{
		source:    source,
		interval:  cfg.Interval,
		timeout:   cfg.Timeout,
		producer:  cfg.ProducedBy,
		ownerOf:   cfg.OwnerResolver,
		publisher: cfg.Publisher,
		logger:    cfg.Logger,
	}
	if c.interval <= 0 {
		c.interval = DefaultStorageInventoryInterval
	}
	if c.timeout <= 0 {
		c.timeout = DefaultStorageInventoryTimeout
	}
	if c.producer == "" {
		c.producer = defaultProducerID()
	}
	if c.logger == nil {
		c.logger = slog.Default()
	}

	// The pre-collection window is stale by construction: an empty inventory
	// reported as fresh would claim the account holds nothing.
	startedAt := time.Now()
	c.latest = StorageInventory{
		ProducedBy:  c.producer,
		Stale:       true,
		StaleSince:  &startedAt,
		StaleReason: "no successful collection yet",
	}
	return c, nil
}

// Latest returns the most recent inventory without doing any I/O. It never
// blocks on a collection and never fails, so a health check, a readiness
// evaluation, or an operator report can call it freely.
//
// The returned Resources slice is a COPY. The published slice is built with
// spare capacity, so handing out the same backing array would let two callers
// appending to their own results write the same slots.
func (c *StorageInventoryCollector) Latest() StorageInventory {
	c.mu.RLock()
	defer c.mu.RUnlock()

	inv := c.latest
	if inv.Resources != nil {
		// StorageResource is all value types, so one copy is a full one.
		inv.Resources = append(make([]StorageResource, 0, len(c.latest.Resources)), c.latest.Resources...)
	}
	return inv
}

// Collect enumerates the account once, bounded by the configured timeout, and
// publishes the result. Collections are serialized, so two overlapping callers
// cannot publish out of order and walk CollectedAt backwards.
//
// On failure it returns the last good inventory marked stale ALONGSIDE the
// error, so a caller that drops the error still cannot mistake a failed
// collection for an empty account. A failure caused by the CALLER's context
// ending — a graceful shutdown — leaves the last good result unmarked, because
// "context canceled" is not a storage finding.
func (c *StorageInventoryCollector) Collect(parent context.Context) (StorageInventory, error) {
	c.collectMu.Lock()
	defer c.collectMu.Unlock()

	ctx, cancel := context.WithTimeout(parent, c.timeout)
	defer cancel()

	resources, account, err := c.enumerate(ctx)
	if err != nil {
		if parent.Err() == nil {
			c.markStale(err)
		}
		return c.Latest(), err
	}

	c.mu.Lock()
	c.latest = StorageInventory{
		ProducedBy:  c.producer,
		CollectedAt: time.Now(),
		Resources:   resources,
		Account:     account,
	}
	c.mu.Unlock()
	return c.Latest(), nil
}

// Run collects on the configured interval until ctx ends. Call it in its own
// goroutine — never from a component's Start or from health evaluation.
//
// The first collection happens immediately rather than one interval later, so a
// freshly booted process has a report to serve; because Run is already
// asynchronous, that cannot delay anything.
func (c *StorageInventoryCollector) Run(ctx context.Context) {
	ticker := time.NewTicker(c.interval)
	defer ticker.Stop()

	c.collectAndLog(ctx)
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			c.collectAndLog(ctx)
		}
	}
}

func (c *StorageInventoryCollector) collectAndLog(ctx context.Context) {
	inv, err := c.Collect(ctx)
	if err != nil {
		if ctx.Err() == nil {
			c.logger.Warn("storage inventory collection failed; serving last known result",
				slog.String("error", err.Error()),
				slog.String("produced_by", c.producer))
		}
		// The publisher declines a stale inventory anyway; not calling it at all
		// keeps the failure to one log line.
		return
	}
	c.publish(ctx, inv)
}

// publish writes the report for one collection. A publication failure is
// LOGGED, never returned into the loop: the report is observability, the next
// tick is the recovery, and a monitoring surface that can stop its own
// collection loop is a worse bug than the blindness it fixes.
func (c *StorageInventoryCollector) publish(ctx context.Context, inv StorageInventory) {
	if c.publisher == nil {
		return
	}
	result, err := c.publisher.Publish(ctx, inv)
	switch {
	case err != nil && ctx.Err() == nil:
		c.logger.Warn("storage report publication failed; the report is stale until the next collection",
			slog.String("error", err.Error()),
			slog.String("produced_by", c.producer))
	case result.Skipped:
		c.logger.Warn("storage report publication skipped",
			slog.String("reason", result.SkipReason),
			slog.String("produced_by", c.producer))
	}
}

// enumerate walks both account listings, reconciles them, and compares the
// declarations it found against the account's per-tier limits. All I/O happens
// here, outside the publication lock.
//
// The NAME listing runs FIRST on purpose. The two listings are not a consistent
// snapshot, so one of them has to absorb the skew, and this ordering puts it on
// the harmless side: a stream CREATED between them appears in the info listing
// and yields a complete row, while a stream DELETED between them yields one
// unknown row that resolves on the next collection. Reversing the order would
// turn every newly created stream into a transient phantom instead.
func (c *StorageInventoryCollector) enumerate(
	ctx context.Context,
) ([]StorageResource, AccountReport, error) {
	lister, err := c.source()
	if err != nil {
		return nil, AccountReport{}, errs.WrapTransient(err, "StorageInventoryCollector", "Collect",
			"resolve the account stream lister")
	}

	names, err := c.listNames(ctx, lister)
	if err != nil {
		return nil, AccountReport{}, err
	}

	described, err := c.listInfos(ctx, lister)
	if err != nil {
		return nil, AccountReport{}, err
	}

	// Names the info listing did not describe are the resources the server
	// declined to report on. Publishing them named, with everything it could
	// not tell us marked unknown, is what keeps the inventory account-complete.
	for name := range names {
		if _, ok := described[name]; ok {
			continue
		}
		described[name] = c.undescribable(name)
	}

	resources := make([]StorageResource, 0, len(described))
	for _, res := range described {
		resources = append(resources, res)
	}
	sort.Slice(resources, func(i, j int) bool { return resources[i].Name < resources[j].Name })

	// The account limits are an ENRICHMENT, read after the listings and never
	// able to fail the collection: readAccountTierLimits returns unknown limits
	// rather than an error, so a client that cannot answer costs the
	// over-commitment comparison and nothing else. One call per collection —
	// the same cost bound the listings respect.
	return resources, DeriveAccountReport(resources, readAccountTierLimits(ctx, lister)), nil
}

// listNames walks the account name listing. The server does NOT exclude offline
// streams here, so this is the complete membership set.
func (c *StorageInventoryCollector) listNames(
	ctx context.Context, lister StreamLister,
) (map[string]struct{}, error) {
	const op = "enumerate account storage names"

	walk := lister.StreamNames(ctx)
	ch := walk.Name()
	names := make(map[string]struct{}, 16)

	for open := true; open; {
		select {
		case name, ok := <-ch:
			if !ok {
				open = false
				break
			}
			names[name] = struct{}{}
		case <-ctx.Done():
			return nil, c.walkFailure(ctx.Err(), op)
		}
	}
	if err := walk.Err(); err != nil {
		return nil, c.walkFailure(err, op)
	}
	if err := ctx.Err(); err != nil {
		return nil, c.walkFailure(err, op)
	}
	return names, nil
}

// listInfos walks the account info listing, which carries configuration and
// state together.
//
// Rows are keyed by name, which also DEDUPLICATES. The client advances its page
// offset by the number of entries a page returned, while the server's cursor
// also moved past the entries it excluded for being offline — so an account
// with more than one page plus one offline stream serves overlapping pages, and
// a slice would carry the same resource twice.
func (c *StorageInventoryCollector) listInfos(
	ctx context.Context, lister StreamLister,
) (map[string]StorageResource, error) {
	const op = "enumerate account storage"

	walk := lister.ListStreams(ctx)
	ch := walk.Info()
	described := make(map[string]StorageResource, 16)

	for open := true; open; {
		select {
		case info, ok := <-ch:
			if !ok {
				open = false
				break
			}
			// A nameless entry means a resource IS absent from anything this
			// collection can publish, and no row built from it would be
			// lookupable, alertable, or distinguishable from a sibling. That is
			// the same partial-listing hazard a walk error is, so it is handled
			// the same way: fail, and keep serving last-good.
			if info == nil || info.Config.Name == "" {
				return nil, c.walkFailure(
					fmt.Errorf("account listing returned an entry carrying no stream name"), op)
			}
			described[info.Config.Name] = c.describe(info)
		case <-ctx.Done():
			return nil, c.walkFailure(ctx.Err(), op)
		}
	}
	if err := walk.Err(); err != nil {
		return nil, c.walkFailure(err, op)
	}
	// A walk cut short by the deadline can close its channel without setting
	// Err. Publishing what arrived would report a subset of the account as if
	// it were all of it — the silent-omission failure this inventory exists to
	// prevent.
	if err := ctx.Err(); err != nil {
		return nil, c.walkFailure(err, op)
	}
	return described, nil
}

func (c *StorageInventoryCollector) walkFailure(err error, operation string) error {
	return errs.WrapTransient(err, "StorageInventoryCollector", "Collect", operation)
}

// describe turns one listing entry into a resource row. Configuration and state
// both come from the entry, so there is no follow-up describe call.
func (c *StorageInventoryCollector) describe(info *jetstream.StreamInfo) StorageResource {
	kind, bucket := ClassifyBackingStream(info.Config.Name)
	attribution, owner := c.attribute(kind, bucket)
	return StorageResource{
		Name:        info.Config.Name,
		Kind:        kind,
		Bucket:      bucket,
		Attribution: attribution,
		Owner:       owner,
		Tier:        tierOf(info.Config.Storage),
		Bytes:       NewCapacity(info.Config.MaxBytes, clampToInt64(info.State.Bytes), true),
		Messages:    NewCapacity(info.Config.MaxMsgs, clampToInt64(info.State.Msgs), true),
	}
}

// undescribable builds the row for a resource the account's name listing
// reported but the info listing omitted — the server declining to describe a
// stream carrying an offline reason (a persisted config needing a higher API
// level than the running binary, for instance, after a server rollback).
//
// Everything derivable from the NAME is still reported: kind, bucket, and, for
// a KV bucket, its catalog owner, which is a pure catalog read needing no
// server state. Everything that would have come from the server is unknown.
func (c *StorageInventoryCollector) undescribable(name string) StorageResource {
	kind, bucket := ClassifyBackingStream(name)
	attribution, owner := c.attribute(kind, bucket)
	return StorageResource{
		Name:        name,
		Kind:        kind,
		Bucket:      bucket,
		Attribution: attribution,
		Owner:       owner,
		Tier:        TierUnknown,
		Bytes:       UnknownCapacity(),
		Messages:    UnknownCapacity(),
	}
}

// attribute resolves ownership for one resource. It is the ONE place the
// attribution state and the owner string are decided together, so the two can
// never disagree: a non-empty owner outside the attributed state, and an
// attributed state with no owner, are both unrepresentable.
func (c *StorageInventoryCollector) attribute(kind ResourceKind, bucket string) (AttributionState, string) {
	if kind != ResourceKeyValue {
		// Ownership is not a meaningful question for an ordinary stream or an
		// ObjectStore: no registry declares one, and this inventory reads the
		// account rather than any single process's declarations.
		return AttributionNotApplicable, ""
	}
	// Resolved now, from the catalog, for this collection only.
	owner := c.ownerOf(bucket)
	if owner == "" {
		return AttributionUnattributed, ""
	}
	return AttributionAttributed, owner
}

func (c *StorageInventoryCollector) markStale(err error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	failedAt := time.Now()
	c.latest.Stale = true
	c.latest.StaleSince = &failedAt
	c.latest.StaleReason = err.Error()
}

func tierOf(storage jetstream.StorageType) StorageTier {
	switch storage {
	case jetstream.FileStorage:
		return TierFile
	case jetstream.MemoryStorage:
		return TierMemory
	}
	return TierUnknown
}

// clampToInt64 converts JetStream's unsigned usage counters without wrapping to
// a negative number, which would read as a nonsense measurement.
func clampToInt64(v uint64) int64 {
	if v > math.MaxInt64 {
		return math.MaxInt64
	}
	return int64(v)
}

// defaultProducerID names this process when the caller supplies nothing.
func defaultProducerID() string {
	host, err := os.Hostname()
	if err != nil || host == "" {
		host = "unknown-host"
	}
	return host + "/" + strconv.Itoa(os.Getpid())
}

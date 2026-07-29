// storage_report.go PUBLISHES the account storage report
// (storage-observability): one KV key per inventoried resource, rewritten every
// collection, carrying that resource's attribution, capacity, growth rate,
// projection, and pressure state.
//
// The bucket is the produced truth. Every operator-facing surface — HTTP route,
// CLI, alert rule, metrics exporter — is a CONSUMER of it rather than a second
// report-producing path, so no two surfaces can disagree about what the account
// looks like. The bucket's declaration (name, class, owner-only writes,
// no-lifecycle retention, History depth) lives in the framework KV catalog,
// graph/kvcatalog.go; this file holds no bucket name, which is also what keeps
// natsclient from importing graph.
//
// Three properties are load-bearing.
//
// ONE KEY PER RESOURCE, not one report blob. A blob would be bounded by the
// NATS max payload on a large account — the pressure that would otherwise push
// the report toward an ObjectStore the framework has no bounded lane for. Per
// resource keys remove that ceiling, make history per-resource rather than
// per-snapshot, and make reclamation a KV delete.
//
// A DISAPPEARED RESOURCE IS DELETED, never expired. Reclamation here is a
// semantic decision the collector makes, on the same principle that governs the
// rest of the graph; a retention policy on this bucket would make "this
// resource is gone" and "this row aged out" indistinguishable to every
// consumer, and would put lifecycle eviction on the one bucket whose whole
// argument is that eviction is not the mechanism.
//
// THE PER-KEY HISTORY IS THE GROWTH SERIES. The rate is Δbytes over Δt across
// successive published observations (storage_growth.go), so the observations a
// restarted process needs are already in the bucket — restart-surviving by
// construction, with no separate sample store to build. A fresh publisher seeds
// its baseline from that history on the first publication of each key and works
// from memory afterwards, so the steady state costs one Put per resource.
//
// Nothing here rejects, throttles, degrades, or evicts anything. Pressure is
// report-only: this file writes rows.

package natsclient

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"github.com/nats-io/nats.go/jetstream"

	"github.com/c360studio/semstreams/pkg/errs"
)

// ReportStore is the narrow KV capability the report publisher needs.
// jetstream.KeyValue satisfies it.
//
// History rather than Get: the seed read has to be able to walk PAST an
// observation too close to the current one to measure (several processes may
// publish account-wide, so the newest revisions can all land inside one
// collection interval), and the retained series is what makes that possible.
type ReportStore interface {
	Put(ctx context.Context, key string, value []byte) (uint64, error)
	Delete(ctx context.Context, key string, opts ...jetstream.KVDeleteOpt) error
	History(ctx context.Context, key string, opts ...jetstream.WatchOpt) ([]jetstream.KeyValueEntry, error)
	ListKeys(ctx context.Context, opts ...jetstream.WatchOpt) (jetstream.KeyLister, error)
}

// ThresholdSource yields the pressure thresholds for ONE evaluation.
//
// It is a FUNCTION, and that is the whole point. SemStreams is flow-based and
// its components are runtime-reconfigurable — watchConfigUpdates is launched
// after the boot barrier specifically so post-boot component edits reach
// running components. A threshold captured into a value at composition-root
// construction would apply the stale number successfully and silently after an
// operator edit: nothing errors, the report just answers the wrong question.
// Resolving at the seam, from live configuration, is the same discipline the
// bucket catalog applied at the acquisition seam.
type ThresholdSource func() StoragePressureThresholds

// ResourceReport is one published row: what the collector saw about one
// resource, and what it derived from it.
//
// The row is plain JSON with no BaseMessage wrapper, following the GRAPH_STATUS
// readiness envelope: the payload registry governs polymorphic publishes on
// subjects, where a receiver must discriminate a type it did not choose. This
// is a KV value on a key whose type is fixed by the contract, and wrapping it
// would break every consumer's plain decode for nothing.
type ResourceReport struct {
	// Resource is the inventory row verbatim — name, kind, attribution, owner,
	// tier, and the capacity states. The report is a VIEW of the inventory, not
	// a second opinion about any of it.
	Resource StorageResource `json:"resource"`

	// CollectedAt is when this resource was read from the account. Every key
	// carries its own, because a consumer ranging the bucket sees a mix of
	// revisions rather than an atomic snapshot and needs to see the spread.
	CollectedAt time.Time `json:"collected_at"`

	// ProducedBy names the process that collected and published this row. A
	// fleet of processes each polling account-wide is unreconcilable without
	// it.
	ProducedBy string `json:"produced_by"`

	// Growth is the observed rate of change across successive observations.
	Growth Growth `json:"growth"`

	// Projection is headroom and time-to-threshold, each suppressed on its own
	// terms rather than fabricated.
	Projection Projection `json:"projection"`

	// Pressure is the derived state. Report-only: no write is rejected, no
	// component is throttled, no readiness gate is failed, and no retention is
	// applied because of what it says.
	Pressure Pressure `json:"pressure"`
}

// PublishResult is what one publication did, for the caller that logs it.
//
// It deliberately does NOT carry the published rows. Every operator-facing
// surface must be a CONSUMER of the bucket rather than a second
// report-producing path, and handing an in-process caller the derived values
// directly is exactly the shortcut that would let one surface disagree with
// another.
type PublishResult struct {
	Published int
	Deleted   int

	// Skipped reports that nothing was written at all, with SkipReason saying
	// why. It is not an error: declining to republish a stale inventory is the
	// correct behavior, not a failure.
	Skipped    bool
	SkipReason string
}

// SkipReasonStaleInventory is the SkipReason for an inventory whose most recent
// collection did not succeed.
const SkipReasonStaleInventory = "inventory is stale; last-good is not a new observation"

// StorageReportConfig configures the report publisher.
type StorageReportConfig struct {
	// Thresholds resolves the pressure thresholds per evaluation. REQUIRED:
	// see ThresholdSource.
	Thresholds ThresholdSource

	// Logger receives publication and threshold warnings. Defaults to
	// slog.Default().
	Logger *slog.Logger
}

// StorageReportPublisher writes the account report to its KV bucket.
//
// Safe for concurrent use, though one publication at a time is the intended
// shape: the baseline map is the growth series' in-process cache and two
// overlapping publications would race to advance it.
type StorageReportPublisher struct {
	store      ReportStore
	thresholds ThresholdSource
	logger     *slog.Logger

	mu sync.Mutex
	// baseline is the observation each key's next rate will be measured
	// against. It is seeded from the bucket's own history on a key's first
	// publication in this process, which is what makes the projection survive a
	// restart.
	baseline map[string]Observation
}

// NewStorageReportPublisher builds a publisher. It fails closed on a missing
// store or threshold source rather than evaluating every resource in the
// account against numbers no operator chose.
func NewStorageReportPublisher(store ReportStore, cfg StorageReportConfig) (*StorageReportPublisher, error) {
	if store == nil {
		return nil, errs.WrapInvalid(
			errors.New("a ReportStore is required; acquire the report bucket through the catalog owner seam "+
				"(graph.EnsureCatalogBucket with graph.BucketStorageReport)"),
			"StorageReportPublisher", "New", "validate configuration")
	}
	if cfg.Thresholds == nil {
		return nil, errs.WrapInvalid(
			errors.New("a ThresholdSource is required; pass a closure over live operator configuration, or "+
				"func() StoragePressureThresholds { return DefaultStoragePressureThresholds() } to take the "+
				"documented defaults deliberately"),
			"StorageReportPublisher", "New", "validate configuration")
	}
	logger := cfg.Logger
	if logger == nil {
		logger = slog.Default()
	}
	return &StorageReportPublisher{
		store:      store,
		thresholds: cfg.Thresholds,
		logger:     logger,
		baseline:   make(map[string]Observation, 32),
	}, nil
}

// Publish writes one row per inventoried resource and deletes the key of any
// resource the inventory no longer names.
//
// A STALE inventory publishes nothing. Last-good is what the collector serves
// when a collection fails, and republishing it as a fresh observation would
// inject a duplicate sample into the growth series and move every row's
// collection timestamp forward onto data that is not new.
//
// A resource that fails to publish does not stop the rest of the report: the
// failures are joined and returned, naming the resources they lost, so a
// partial publication is REPORTED rather than silently shrinking the report.
func (p *StorageReportPublisher) Publish(ctx context.Context, inv StorageInventory) (PublishResult, error) {
	if inv.Stale || inv.CollectedAt.IsZero() {
		return PublishResult{Skipped: true, SkipReason: SkipReasonStaleInventory}, nil
	}

	thresholds, thresholdErr := p.thresholds().Resolve()
	if thresholdErr != nil {
		// Report-only, and honest: the capacity truth still publishes, and
		// pressure reports as unavailable naming the configuration rather than
		// falling back to a number the operator did not choose.
		p.logger.Warn("storage pressure thresholds are not usable; publishing the report without pressure states",
			slog.String("error", thresholdErr.Error()))
	}

	var result PublishResult
	published := make(map[string]struct{}, len(inv.Resources))
	var failures []error

	for _, resource := range inv.Resources {
		key, err := StorageReportKey(resource.Name)
		if err != nil {
			// No addressable row exists for this resource, so nothing is
			// claimed and reclamation may remove any row that somehow does.
			failures = append(failures, fmt.Errorf("address resource %q: %w", resource.Name, err))
			continue
		}

		// Claimed HERE, before the write, because this set answers "which
		// resources did the COLLECTION name" — not "which rows did this
		// publication manage to write". A resource whose row fails to write
		// still exists and was still named; deleting its last-good row would
		// make a transient NATS error indistinguishable from a deleted stream,
		// blank a real resource out of the report every operator surface reads,
		// and drop a tombstone into the middle of its growth series (which
		// seedFromHistory correctly refuses to difference across).
		published[key] = struct{}{}

		row := p.derive(ctx, key, resource, inv, thresholds, thresholdErr)
		value, err := json.Marshal(row)
		if err != nil {
			failures = append(failures, fmt.Errorf("encode report row for %q: %w", resource.Name, err))
			continue
		}
		if _, err := p.store.Put(ctx, key, value); err != nil {
			failures = append(failures, fmt.Errorf("publish report row for %q: %w", resource.Name, err))
			continue
		}

		result.Published++
	}

	deleted, err := p.reclaim(ctx, published)
	result.Deleted = deleted
	if err != nil {
		failures = append(failures, err)
	}

	if len(failures) > 0 {
		return result, errs.WrapTransient(errors.Join(failures...),
			"StorageReportPublisher", "Publish", "publish the account storage report")
	}
	return result, nil
}

// derive builds one row and advances that key's growth baseline.
func (p *StorageReportPublisher) derive(
	ctx context.Context,
	key string,
	resource StorageResource,
	inv StorageInventory,
	thresholds ResolvedPressureThresholds,
	thresholdErr error,
) ResourceReport {
	row := ResourceReport{
		Resource:    resource,
		CollectedAt: inv.CollectedAt,
		ProducedBy:  inv.ProducedBy,
		Growth:      p.growthFor(ctx, key, resource, inv.CollectedAt),
	}
	if thresholdErr != nil {
		// No thresholds means no band to compare against and no level to
		// project toward. Both are suppressed together HERE, naming the
		// configuration, rather than being computed against a default.
		reason := fmt.Sprintf("unusable pressure threshold configuration: %v", thresholdErr)
		row.Projection = Projection{HeadroomUnavailable: reason, TimeToThresholdUnavailable: reason}
		row.Pressure = Pressure{Unavailable: reason}
		return row
	}
	row.Projection = Project(resource.Bytes, row.Growth, thresholds)
	row.Pressure = AssessPressure(resource.Bytes, row.Projection, thresholds)
	return row
}

// growthFor measures this resource's rate and advances its baseline.
//
// The baseline advance is the subtle part. It moves to the current observation
// only when the pair was USABLE (or when there was nothing to compare against
// at all, which starts the series). An observation too close to the baseline to
// measure leaves the baseline where it is: advancing it every time would keep
// resetting the target, and a process publishing faster than
// MinGrowthSampleInterval would report an unknown rate forever.
func (p *StorageReportPublisher) growthFor(
	ctx context.Context, key string, resource StorageResource, collectedAt time.Time,
) Growth {
	used, ok := resource.Bytes.Usage()
	if !ok {
		// Nothing to difference. Distinct from "no prior sample": this resource
		// has no readable size at all, so no number of collections will help
		// until the server describes it again.
		return UnknownGrowth(GrowthUnavailableUnknownUsage)
	}
	current := Observation{At: collectedAt, Bytes: used}

	p.mu.Lock()
	defer p.mu.Unlock()

	priors, seeded := p.priorsLocked(ctx, key)
	growth := DeriveGrowth(current, priors)

	switch {
	case growth.State == GrowthKnown, len(priors) == 0:
		// The pair was consumed (or there was no pair at all): the next
		// interval starts here.
		p.baseline[key] = current
	case !seeded:
		// Unusable, and the baseline is already the fixed target this process
		// has been holding. Leave it exactly where it is.
	default:
		// Unusable, seeded from the bucket. Retain the OLDEST observation that
		// is not newer than this one: it is the furthest from the current
		// moment, so it becomes measurable soonest and over the longest span.
		p.baseline[key] = oldestUsableTarget(current, priors)
	}
	return growth
}

// priorsLocked returns the observations this key's rate can be measured
// against, reading the bucket's retained history the first time a key is seen
// in this process. seeded reports that the priors came from the bucket rather
// than from memory.
//
// The history read happens once per key per process: after it, the baseline is
// in memory and the steady state costs one Put per resource per collection.
func (p *StorageReportPublisher) priorsLocked(ctx context.Context, key string) ([]Observation, bool) {
	if baseline, ok := p.baseline[key]; ok {
		return []Observation{baseline}, false
	}
	return p.seedFromHistory(ctx, key), true
}

// seedFromHistory reads the published observations retained for one key.
//
// It walks NEWEST FIRST and stops at a delete marker: observations from before
// a resource disappeared are not successive observations of the resource that
// came back, and differencing across that gap would report a phantom rate.
func (p *StorageReportPublisher) seedFromHistory(ctx context.Context, key string) []Observation {
	entries, err := p.store.History(ctx, key)
	if err != nil {
		if !errors.Is(err, jetstream.ErrKeyNotFound) && !errors.Is(err, jetstream.ErrNoKeysFound) {
			// Not fatal: an unreadable history costs one collection's rate, and
			// the next publication re-seeds. It is logged rather than swallowed
			// because a persistently unreadable history means every rate in the
			// report is unknown for a reason nobody can see.
			p.logger.Warn("could not read the published growth series; this resource reports an unknown rate",
				slog.String("key", key), slog.String("error", err.Error()))
		}
		return nil
	}

	observations := make([]Observation, 0, len(entries))
	for i := len(entries) - 1; i >= 0; i-- {
		entry := entries[i]
		if entry.Operation() != jetstream.KeyValuePut {
			break
		}
		var row ResourceReport
		if err := json.Unmarshal(entry.Value(), &row); err != nil {
			p.logger.Warn("skipping an undecodable published report row in the growth series",
				slog.String("key", key), slog.Uint64("revision", entry.Revision()),
				slog.String("error", err.Error()))
			continue
		}
		bytes, ok := row.Resource.Bytes.Usage()
		if !ok || row.CollectedAt.IsZero() {
			continue
		}
		observations = append(observations, Observation{At: row.CollectedAt, Bytes: bytes})
	}
	return observations
}

// reclaim deletes the key of every resource the current collection did not
// name. This is the semantic deletion the spec requires — the collector
// decides, and no retention policy expires anything.
//
// Several processes may publish this report, and they enumerate the same
// account, so a row one of them reclaims is a row the others have also stopped
// seeing. A row deleted on a timing skew is republished by the next collection,
// which is the same eventually-consistent trade the inventory's two listings
// already make.
func (p *StorageReportPublisher) reclaim(ctx context.Context, published map[string]struct{}) (int, error) {
	lister, err := p.store.ListKeys(ctx)
	if err != nil {
		if errors.Is(err, jetstream.ErrNoKeysFound) {
			return 0, nil
		}
		return 0, fmt.Errorf("list published report keys: %w", err)
	}
	defer func() { _ = lister.Stop() }()

	// Drained fully before deleting: the lister is a live watch, and deleting
	// underneath it would publish into the stream it is reading.
	var stale []string
	for key := range lister.Keys() {
		if _, ok := published[key]; ok {
			continue
		}
		stale = append(stale, key)
	}
	if err := ctx.Err(); err != nil {
		return 0, fmt.Errorf("list published report keys: %w", err)
	}

	deleted := 0
	var failures []error
	for _, key := range stale {
		if err := p.store.Delete(ctx, key); err != nil {
			failures = append(failures, fmt.Errorf("delete report row %q: %w", key, err))
			continue
		}
		deleted++
		p.mu.Lock()
		delete(p.baseline, key)
		p.mu.Unlock()
	}
	return deleted, errors.Join(failures...)
}

// StorageReportKey is the report bucket key for one resource name.
//
// An ordinary name IS its own key, so `nats kv get` reads naturally and an
// operator can address a resource by the name they already know. JetStream
// accepts stream names that are not legal KV keys, though — `$` and `+` among
// others are fine in a stream name and illegal in a key — and the resources
// most likely to be a growth problem are exactly the ones nobody in this
// process created. Dropping them would rebuild the silent omission the
// inventory exists to end, so an illegal name is addressed through the
// repository's opaque key codec instead. The row still carries the real name.
//
// A name that is ITSELF a canonical opaque token is also encoded, so no two
// resources can ever collide on one key.
func StorageReportKey(resource string) (string, error) {
	if err := ValidateKVLiteralToken(resource); err == nil {
		if _, decodable := DecodeKVOpaqueToken(resource); decodable != nil {
			return resource, nil
		}
	}
	return EncodeKVOpaqueToken([]byte(resource))
}

// oldestUsableTarget picks the fixed target to retain when no prior
// observation was far enough from the current one to measure against.
//
// It is the OLDEST observation not newer than the current one: it becomes
// measurable soonest, over the longest interval.
//
// The search starts at the current observation and only ever moves EARLIER, and
// that single property is what excludes an observation from the future (clock
// skew across producers) — such an observation is never before the target, so
// it can never be selected. An explicit future-skip on top of that would be
// unreachable code, and a retained future baseline would wedge this key on an
// unknown rate until wall-clock time caught up with the skew.
func oldestUsableTarget(current Observation, priors []Observation) Observation {
	target := current
	for _, prior := range priors {
		if !prior.At.Before(target.At) {
			continue
		}
		target = prior
	}
	return target
}

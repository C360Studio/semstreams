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

	// AccountPublished reports that the per-tier account row was written. It is
	// a separate field rather than part of Published so that count keeps meaning
	// "resources", which is what every caller logging it reads it as.
	AccountPublished bool

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

	// tierBaseline is the same thing for each STORAGE TIER's account usage,
	// seeded from the account row's history.
	//
	// It is a separate map rather than synthetic entries in baseline because
	// baseline is keyed by published KV key and reclaim() deletes from it by key.
	// A synthetic tier key would be a key no listing can return, so it would sit
	// in a map whose invariant is "every entry is a live published row" and
	// quietly falsify it.
	tierBaseline map[StorageTier]Observation
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
		store:        store,
		thresholds:   cfg.Thresholds,
		logger:       logger,
		baseline:     make(map[string]Observation, 32),
		tierBaseline: make(map[StorageTier]Observation, 3),
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
	published := make(map[string]struct{}, len(inv.Resources)+1)
	var failures []error

	// Claimed BEFORE anything is written, for the same reason a resource key is:
	// the claim answers "does this collection still name this row", and the
	// account row is named by every collection. Without the claim, reclamation
	// would delete it on the very publication that wrote it, because no resource
	// ever addresses it.
	published[StorageAccountReportKey] = struct{}{}

	// The tier ceilings are derived FIRST even though the account row is written
	// LAST. An unbounded resource has no ceiling but its tier's, so its row cannot
	// be built until that verdict exists — and deriving it once here is what
	// guarantees the resource rows and the account row state the same thing about
	// the same tier in the same collection.
	account := inv.Account
	account.CollectedAt = inv.CollectedAt
	account.ProducedBy = inv.ProducedBy
	tiers, stagedTiers := p.deriveTiers(ctx, inv, thresholds, thresholdErr)
	account.Tiers = tiers

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

		row, staged := p.derive(ctx, key, resource, inv, account, thresholds, thresholdErr)
		value, err := json.Marshal(row)
		if err != nil {
			failures = append(failures, fmt.Errorf("encode report row for %q: %w", resource.Name, err))
			continue
		}
		if _, err := p.store.Put(ctx, key, value); err != nil {
			failures = append(failures, fmt.Errorf("publish report row for %q: %w", resource.Name, err))
			continue
		}

		// The baseline advances HERE, and only here: this observation is now in the
		// bucket, so the in-memory cache and the published series agree. A failed
		// write above leaves the baseline where it was, which is what a restarted
		// process would seed from.
		p.commitBaseline(key, staged)
		result.Published++
	}

	// The account row goes LAST, after every resource row this collection could
	// write. It summarizes them, so publishing it first would briefly advertise
	// a comparison over rows a consumer cannot yet see.
	if err := p.publishAccount(ctx, account); err != nil {
		failures = append(failures, err)
	} else {
		// Same rule as the resource baselines: the tier series lives in the account
		// row's history, so a tier baseline may only advance once that row is in the
		// bucket.
		p.commitTierBaselines(stagedTiers)
		result.AccountPublished = true
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

// publishAccount writes the per-tier account row: what each storage tier's
// ceiling is, whether the bounds declared against it fit, and how that ceiling is
// filling.
//
// It is PUBLISHED rather than left for each surface to compute because the
// comparison needs two inputs — the account limits and every resource's declared
// bound — and only the collection holds both at once. A consumer recomputing it
// from the rows it happens to have read would disagree with another consumer
// that read a different mix of revisions, which is the divergence this bucket
// exists to make impossible.
//
// It takes the FINISHED row rather than the inventory: the tier verdicts were
// derived before the resource rows, because unbounded resources carry them, and
// re-deriving them here could publish an account row that disagrees with the rows
// that inherited it.
func (p *StorageReportPublisher) publishAccount(ctx context.Context, row AccountReport) error {
	value, err := json.Marshal(row)
	if err != nil {
		return fmt.Errorf("encode the account tier report: %w", err)
	}
	if _, err := p.store.Put(ctx, StorageAccountReportKey, value); err != nil {
		return fmt.Errorf("publish the account tier report: %w", err)
	}
	return nil
}

// derive builds one row and advances that key's growth baseline.
func (p *StorageReportPublisher) derive(
	ctx context.Context,
	key string,
	resource StorageResource,
	inv StorageInventory,
	account AccountReport,
	thresholds ResolvedPressureThresholds,
	thresholdErr error,
) (ResourceReport, *Observation) {
	growth, staged := p.growthFor(ctx, key, resource, inv.CollectedAt)
	row := ResourceReport{
		Resource:    resource,
		CollectedAt: inv.CollectedAt,
		ProducedBy:  inv.ProducedBy,
		Growth:      growth,
	}
	if thresholdErr != nil {
		// No thresholds means no band to compare against and no level to
		// project toward. Both are suppressed together HERE, naming the
		// configuration, rather than being computed against a default.
		reason := unusableThresholdReason(thresholdErr)
		row.Projection = Projection{HeadroomUnavailable: reason, TimeToThresholdUnavailable: reason}
		row.Pressure = Pressure{Unavailable: reason}
		return row, staged
	}
	row.Projection = Project(resource.Bytes, row.Growth, thresholds)
	row.Pressure = AssessPressure(resource.Bytes, row.Projection, thresholds)

	// A resource with NO BOUND OF ITS OWN is re-evaluated against its storage
	// tier's account ceiling, which is the only ceiling it has.
	//
	// The PROJECTION is deliberately left suppressed as "unbounded". Headroom
	// against a bound this resource does not have would be a fabricated
	// per-resource number, and the tier's headroom is not that number — it is
	// shared with every other resource in the tier and is published on the account
	// row, where it belongs. So the row says "no headroom of my own" and "here is
	// the pressure I inherit from my ceiling", which are both true, in two fields
	// that cannot be confused for one another.
	if resource.Bytes.State == CapacityUnbounded {
		tier, found := account.TierFor(resource.Tier)
		row.Pressure = PressureAgainstAccountTier(tier, found)
	}
	return row, staged
}

// deriveAgainstBaseline measures a rate and decides where the next measuring
// interval starts. advance reports whether the caller should move its retained
// baseline to next.
//
// The baseline advance is the subtle part, and it is shared by the resource and
// tier series so the two cannot diverge on it. The baseline moves to the current
// observation only when the pair was USABLE (or when there was nothing to compare
// against at all, which starts the series). An observation too close to the
// baseline to measure leaves the baseline where it is: advancing it every time
// would keep resetting the target, and a process publishing faster than
// MinGrowthSampleInterval would report an unknown rate forever.
func deriveAgainstBaseline(
	current Observation, priors []Observation, seeded bool,
) (growth Growth, next Observation, advance bool) {
	growth = DeriveGrowth(current, priors)
	switch {
	case growth.State == GrowthKnown, len(priors) == 0:
		// The pair was consumed (or there was no pair at all): the next
		// interval starts here.
		return growth, current, true
	case !seeded:
		// Unusable, and the baseline is already the fixed target this process
		// has been holding. Leave it exactly where it is.
		return growth, Observation{}, false
	default:
		// Unusable, seeded from the bucket. Retain the OLDEST observation that
		// is not newer than this one: it is the furthest from the current
		// moment, so it becomes measurable soonest and over the longest span.
		return growth, oldestUsableTarget(current, priors), true
	}
}

// growthFor measures this resource's rate and advances its baseline.
// It STAGES the baseline rather than committing it: the returned observation is
// applied by commitBaseline only after this row's Put succeeds.
//
// The staging is not bookkeeping tidiness. The rate is defined as Δbytes over Δt
// across successive PUBLISHED observations, and the in-memory baseline is a cache
// of exactly that. Advancing it for a row whose write FAILED puts an observation in
// the cache that is in no history, so the running process measures the next rate
// against a sample nobody can see — while a restarted process, seeding from the
// bucket, measures against the last published one and gets a different answer. The
// projection would then change because a process restarted, which is the one thing
// the published-series design exists to prevent.
func (p *StorageReportPublisher) growthFor(
	ctx context.Context, key string, resource StorageResource, collectedAt time.Time,
) (Growth, *Observation) {
	used, ok := resource.Bytes.Usage()
	if !ok {
		// Nothing to difference. Distinct from "no prior sample": this resource
		// has no readable size at all, so no number of collections will help
		// until the server describes it again.
		return UnknownGrowth(GrowthUnavailableUnknownUsage), nil
	}
	current := Observation{At: collectedAt, Bytes: used}

	p.mu.Lock()
	defer p.mu.Unlock()

	priors, seeded := p.priorsLocked(ctx, key)
	growth, next, advance := deriveAgainstBaseline(current, priors, seeded)
	if !advance {
		return growth, nil
	}
	return growth, &next
}

// commitBaseline applies a staged resource baseline. Called ONLY after the row it
// was derived for has been published.
func (p *StorageReportPublisher) commitBaseline(key string, staged *Observation) {
	if staged == nil {
		return
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	p.baseline[key] = *staged
}

// commitTierBaselines applies staged tier baselines. Called ONLY after the account
// row has been published, for the same reason as commitBaseline: the account row's
// history is the tier series, so a tier baseline advanced past a failed write is an
// observation the bucket does not contain.
func (p *StorageReportPublisher) commitTierBaselines(staged map[StorageTier]Observation) {
	if len(staged) == 0 {
		return
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	for tier, observation := range staged {
		p.tierBaseline[tier] = observation
	}
}

// tierGrowth measures every tier's rate for this collection and advances the tier
// baselines.
//
// It seeds from the account row's history AT MOST ONCE per call, however many
// tiers need it: the tiers share one key, so a per-tier seed would re-read the
// same history two or three times for one collection.
func (p *StorageReportPublisher) tierGrowth(
	ctx context.Context, inv StorageInventory,
) (map[StorageTier]Growth, map[StorageTier]Observation) {
	p.mu.Lock()
	defer p.mu.Unlock()

	growth := make(map[StorageTier]Growth, len(inv.Account.Tiers))
	staged := make(map[StorageTier]Observation, len(inv.Account.Tiers))
	var seeded map[StorageTier][]Observation
	didSeed := false

	for _, comparison := range inv.Account.Tiers {
		used, ok := comparison.Limit.Usage()
		if !ok {
			// The tier's usage is unreadable — an unknown or unbounded-with-no-usage
			// account limit. There is nothing to difference, so this collection
			// contributes no observation.
			//
			// Any baseline already held is KEPT, deliberately, and the consequence
			// is worth stating: when the tier becomes readable again the rate is
			// measured across the blind gap. Both endpoints are genuine account
			// readings, so the number is real rather than fabricated, and
			// Growth.ObservedOver publishes the span it was measured over — a
			// five-hour average is distinguishable from a one-minute one by anyone
			// reading the row. Dropping the baseline instead would make this path
			// diverge from the resource path (growthFor returns early on the same
			// condition and also retains), and two growth series that treat a
			// degraded collection differently is worse than one that treats it
			// visibly.
			growth[comparison.Tier] = UnknownGrowth(GrowthUnavailableUnknownUsage)
			continue
		}
		current := Observation{At: inv.CollectedAt, Bytes: used}

		var priors []Observation
		fromBucket := false
		if baseline, have := p.tierBaseline[comparison.Tier]; have {
			priors = []Observation{baseline}
		} else {
			if !didSeed {
				seeded = p.seedTiersFromHistory(ctx)
				didSeed = true
			}
			priors, fromBucket = seeded[comparison.Tier], true
		}

		measured, next, advance := deriveAgainstBaseline(current, priors, fromBucket)
		if advance {
			staged[comparison.Tier] = next
		}
		growth[comparison.Tier] = measured
	}
	return growth, staged
}

// deriveTiers fills in each tier ceiling's own capacity picture: its rate, its
// projection toward exhaustion, and its pressure state.
//
// This is what makes an unbounded resource evaluable at all — its row carries the
// verdict from here — so it is computed BEFORE any resource row even though the
// account row is published last.
func (p *StorageReportPublisher) deriveTiers(
	ctx context.Context,
	inv StorageInventory,
	thresholds ResolvedPressureThresholds,
	thresholdErr error,
) ([]TierComparison, map[StorageTier]Observation) {
	growth, staged := p.tierGrowth(ctx, inv)

	tiers := make([]TierComparison, 0, len(inv.Account.Tiers))
	for _, comparison := range inv.Account.Tiers {
		comparison.Growth = growth[comparison.Tier]
		if thresholdErr != nil {
			reason := unusableThresholdReason(thresholdErr)
			comparison.Projection = Projection{HeadroomUnavailable: reason, TimeToThresholdUnavailable: reason}
			comparison.Pressure = Pressure{Unavailable: reason}
		} else {
			comparison.Projection = Project(comparison.Limit, comparison.Growth, thresholds)
			comparison.Pressure = AssessPressure(comparison.Limit, comparison.Projection, thresholds).
				AgainstAccountTier()
		}
		tiers = append(tiers, comparison)
	}
	return tiers, staged
}

// unusableThresholdReason is the suppression reason shared by every row when the
// operator's threshold configuration cannot be resolved. One string, so a resource
// row and the account row never explain the same failure differently.
func unusableThresholdReason(err error) string {
	return fmt.Sprintf("unusable pressure threshold configuration: %v", err)
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

// retainedPuts reads one key's retained history and returns its consecutive PUT
// entries, NEWEST FIRST.
//
// It stops at a delete marker: observations from before a resource disappeared
// are not successive observations of the resource that came back, and
// differencing across that gap would report a phantom rate. Both row kinds in
// this bucket read their series through here, so that rule cannot come to mean
// one thing for a resource and another for a tier.
func (p *StorageReportPublisher) retainedPuts(ctx context.Context, key string) []jetstream.KeyValueEntry {
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

	puts := make([]jetstream.KeyValueEntry, 0, len(entries))
	for i := len(entries) - 1; i >= 0; i-- {
		if entries[i].Operation() != jetstream.KeyValuePut {
			break
		}
		puts = append(puts, entries[i])
	}
	return puts
}

// seedFromHistory reads the published observations retained for one resource key.
func (p *StorageReportPublisher) seedFromHistory(ctx context.Context, key string) []Observation {
	entries := p.retainedPuts(ctx, key)
	observations := make([]Observation, 0, len(entries))
	for _, entry := range entries {
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

// seedTiersFromHistory reads the per-tier usage series from the ACCOUNT row's own
// retained history, in ONE read covering every tier.
//
// The series is already there: every collection publishes each tier's
// account-measured usage, so the observations a tier projection needs are
// restart-surviving on exactly the same terms as a resource's — no second sample
// store, and no separate key per tier.
func (p *StorageReportPublisher) seedTiersFromHistory(ctx context.Context) map[StorageTier][]Observation {
	entries := p.retainedPuts(ctx, StorageAccountReportKey)
	priors := make(map[StorageTier][]Observation, 3)
	for _, entry := range entries {
		var row AccountReport
		if err := json.Unmarshal(entry.Value(), &row); err != nil {
			p.logger.Warn("skipping an undecodable published account row in the tier growth series",
				slog.Uint64("revision", entry.Revision()), slog.String("error", err.Error()))
			continue
		}
		if row.CollectedAt.IsZero() {
			continue
		}
		for _, tier := range row.Tiers {
			used, ok := tier.Limit.Usage()
			if !ok {
				continue
			}
			priors[tier.Tier] = append(priors[tier.Tier], Observation{At: row.CollectedAt, Bytes: used})
		}
	}
	return priors
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

// StorageAccountReportKey is the reserved key carrying the per-tier account
// report (AccountReport) rather than a resource row.
//
// It is RESERVED by construction, not by convention. Every resource key is
// exactly ONE key token: a JetStream stream name may not contain a dot, and
// StorageReportKey's fallback opaque token is a single token too. A key
// containing a dot is therefore unreachable from any resource name, however
// hostile — which is what lets one bucket carry two row kinds with no
// possibility of one addressing the other. A consumer discriminates on the key.
const StorageAccountReportKey = "_account.tiers"

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

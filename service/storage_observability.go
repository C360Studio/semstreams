// storage_observability.go hosts the account storage collector as a SERVICE
// (storage-capacity-observability section 4).
//
// It is a service rather than a component because it watches the SUBSTRATE, not
// the flow. Its peers are flow_runtime_health, flow_runtime_metrics,
// metrics_forwarder, log_forwarder, heartbeat and pprof — every one of them
// infrastructure observability. A component is a flow participant with ports
// that processes messages; this has no ports and never receives or emits one,
// and filing it as a component would place substrate monitoring in the data-flow
// graph as though it were a pipeline stage.
//
// # Two halves, one bucket
//
// The service runs a PRODUCER and a CONSUMER, and the seam between them is the
// published report bucket rather than a Go value:
//
//	collector (interval) -> publisher (KV: one key per resource + the account
//	row) -> consumer (Watch) -> Prometheus metrics + health status
//
// The detour through KV is the design, not overhead. Growth, projection and
// pressure are derived once, at publication, from inputs only a collection
// holds; every operator surface then reads the same published rows, so no two
// can disagree. Watching this process's own writes also means each surface
// reports the ACCOUNT rather than the fraction of it that one process last
// enumerated — any number of processes may publish account-wide.
//
// # Report-only, and boot-safe
//
// Nothing here rejects a write, throttles a component, fails a readiness or
// health gate, or applies retention. Health reports pressure in its MESSAGE and
// never in its verdict.
//
// A deployment that does not declare this service in its `services` block is
// untouched — the service manager only constructs what the config names. A
// deployment that DOES declare it must still boot when the account is
// unreadable, so the report bucket is acquired lazily and retried rather than
// resolved in the constructor: a monitoring surface that can take down the
// system it monitors is a worse bug than the blindness it fixes.
//
// # Configuration is applied by RESTART
//
// The collection interval, its timeout, and the pressure thresholds are built
// from rawConfig at construction. A restart is the supported way to change them:
// a stale threshold evaluates old numbers visibly and destroys nothing, unlike a
// stale value reconciling a durable resource to the wrong policy. Restarting is
// cheap precisely because the report and its growth series live in KV, where
// they survive it.

package service

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/nats-io/nats.go/jetstream"

	"github.com/c360studio/semstreams/graph"
	"github.com/c360studio/semstreams/health"
	"github.com/c360studio/semstreams/metric"
	"github.com/c360studio/semstreams/natsclient"
)

// StorageObservabilityServiceName is the key this service is configured and
// registered under.
const StorageObservabilityServiceName = "storage-observability"

// StorageObservabilityConfig is the operator surface for account storage
// observability.
//
// Durations are strings ("1m", "30s") following the repository's service
// configuration convention. Every field is optional and takes its documented
// default; an unusable value is an ERROR at construction rather than a silent
// fallback, because a silently defaulted knob applies a number the operator did
// not choose and is indistinguishable from a working edit.
type StorageObservabilityConfig struct {
	// Interval is how often the account is enumerated. Every process polling
	// account-wide multiplies cost by deployment size, so an operator running
	// many instances turns this down rather than losing the view entirely.
	// Default: 1m.
	Interval string `json:"interval,omitempty" schema:"type:string,description:How often the JetStream account is enumerated,default:1m,category:advanced"`

	// Timeout bounds ONE collection, both account listings included. It must
	// fit inside Interval. Default: 15s.
	Timeout string `json:"timeout,omitempty" schema:"type:string,description:Timeout bounding one account enumeration; must be shorter than the interval,default:15s,category:advanced"`

	// PressureThresholds are the headroom and time-to-threshold bands pressure
	// is derived from. Report-only: nothing is rejected, throttled, or degraded
	// when a band is crossed.
	PressureThresholds natsclient.StoragePressureThresholds `json:"pressure_thresholds,omitempty" schema:"type:object,description:Headroom and time-to-threshold bands used to derive report-only storage pressure,category:advanced"`
}

// appliedStorageObservabilityConfig is the validated form: defaults filled,
// durations parsed, coherence checked. Only applied() produces one, so nothing
// can run against configuration nobody checked.
type appliedStorageObservabilityConfig struct {
	interval   time.Duration
	timeout    time.Duration
	thresholds natsclient.StoragePressureThresholds
}

// applied validates the operator's configuration and fills the documented
// defaults.
func (c StorageObservabilityConfig) applied() (appliedStorageObservabilityConfig, error) {
	interval, err := storageDuration(c.Interval, "interval", natsclient.DefaultStorageInventoryInterval)
	if err != nil {
		return appliedStorageObservabilityConfig{}, err
	}
	timeout, err := storageDuration(c.Timeout, "timeout", natsclient.DefaultStorageInventoryTimeout)
	if err != nil {
		return appliedStorageObservabilityConfig{}, err
	}
	// A timeout wider than the interval quietly serializes collections behind
	// each other: the collector holds a lock for the whole of one collection,
	// so every tick would queue rather than sample.
	if timeout > interval {
		return appliedStorageObservabilityConfig{}, fmt.Errorf(
			"invalid storage-observability config: timeout (%s) must not exceed interval (%s)",
			timeout, interval)
	}
	// Resolved here so a malformed band is a loud construction error rather
	// than a warning logged once a minute for the life of the process.
	if _, err := c.PressureThresholds.Resolve(); err != nil {
		return appliedStorageObservabilityConfig{}, fmt.Errorf(
			"invalid storage-observability pressure thresholds: %w", err)
	}
	return appliedStorageObservabilityConfig{
		interval:   interval,
		timeout:    timeout,
		thresholds: c.PressureThresholds,
	}, nil
}

func storageDuration(value, field string, fallback time.Duration) (time.Duration, error) {
	if value == "" {
		return fallback, nil
	}
	parsed, err := time.ParseDuration(value)
	if err != nil {
		return 0, fmt.Errorf("invalid storage-observability %s %q: %w", field, value, err)
	}
	if parsed <= 0 {
		return 0, fmt.Errorf("invalid storage-observability %s: must be positive, got %q", field, value)
	}
	return parsed, nil
}

// StorageObservabilityService collects the account storage inventory, publishes
// the report, and serves it as metrics and health status.
type StorageObservabilityService struct {
	*BaseService

	config    StorageObservabilityConfig
	collector *natsclient.StorageInventoryCollector
	consumer  *natsclient.StorageReportConsumer
	metrics   *storageObservabilityMetrics
	surface   *storageHealthSurface
	logger    *slog.Logger

	stopOnce sync.Once
	stopChan chan struct{}
	cancel   context.CancelFunc
	wg       sync.WaitGroup
}

// NewStorageObservabilityService builds the service from its config block.
func NewStorageObservabilityService(rawConfig json.RawMessage, deps *Dependencies) (Service, error) {
	var cfg StorageObservabilityConfig
	if len(rawConfig) > 0 {
		if err := json.Unmarshal(rawConfig, &cfg); err != nil {
			return nil, fmt.Errorf("parse storage-observability config: %w", err)
		}
	}
	applied, err := cfg.applied()
	if err != nil {
		return nil, err
	}

	if deps == nil || deps.NATSClient == nil {
		return nil, fmt.Errorf("storage-observability requires a NATS client to enumerate the account")
	}

	logger := slog.Default().With("service", StorageObservabilityServiceName)
	if deps.Logger != nil {
		logger = deps.Logger.With("service", StorageObservabilityServiceName)
	}

	// The bucket is resolved on FIRST USE, not here. A deployment that declares
	// this service must still boot when the account cannot be read.
	bucket := &lazyReportBucket{client: deps.NATSClient, logger: logger}

	metrics := newStorageObservabilityMetrics()
	surface := &storageHealthSurface{}

	consumer, err := natsclient.NewStorageReportConsumer(bucket,
		natsclient.StorageReportConsumerConfig{
			Observer: metrics,
			Logger:   logger,
		})
	if err != nil {
		return nil, fmt.Errorf("build the storage report consumer: %w", err)
	}
	surface.snapshotOf = consumer.Snapshot

	collector, err := natsclient.NewStorageInventoryCollector(
		deps.NATSClient.AccountStreamLister,
		natsclient.StorageInventoryConfig{
			Interval:      applied.interval,
			Timeout:       applied.timeout,
			OwnerResolver: graph.OwnerOf,
			Publisher: &lazyReportPublisher{
				bucket: bucket,
				// The ThresholdSource seam from section 3, wired to the value
				// built from config. A restart applies an edit; nothing depends
				// on resolving it more often than that.
				thresholds: func() natsclient.StoragePressureThresholds { return applied.thresholds },
				logger:     logger,
			},
			Logger: logger,
		})
	if err != nil {
		return nil, fmt.Errorf("build the storage inventory collector: %w", err)
	}
	surface.inventoryOf = collector.Latest

	var opts []Option
	if deps.Logger != nil {
		opts = append(opts, WithLogger(deps.Logger))
	}
	if deps.MetricsRegistry != nil {
		opts = append(opts, WithMetrics(deps.MetricsRegistry))
		// Registered HERE as well as through the Service interface's
		// RegisterMetrics, because nothing in the framework calls that method
		// today — a metric registered only there would never reach the registry
		// the /metrics endpoint scrapes, which is the phantom-signal class this
		// capability exists to remove. Registration is idempotent.
		if err := metrics.register(deps.MetricsRegistry); err != nil {
			return nil, fmt.Errorf("register storage-observability metrics: %w", err)
		}
	}

	return &StorageObservabilityService{
		BaseService: NewBaseServiceWithOptions(StorageObservabilityServiceName, nil, opts...),
		config:      cfg,
		collector:   collector,
		consumer:    consumer,
		metrics:     metrics,
		surface:     surface,
		logger:      logger,
		stopChan:    make(chan struct{}),
	}, nil
}

// Start begins collecting and consuming. Instances are single-use, matching the
// sibling services: once Stop has run its teardown, create a new one.
func (s *StorageObservabilityService) Start(ctx context.Context) error {
	if s.Status() == StatusRunning {
		return fmt.Errorf("storage-observability already running")
	}
	select {
	case <-s.stopChan:
		return fmt.Errorf("storage-observability instance already stopped; create a new instance")
	default:
	}

	if err := s.BaseService.Start(ctx); err != nil {
		return err
	}

	runCtx, cancel := context.WithCancel(ctx)
	s.cancel = cancel

	// Both loops are asynchronous and neither is on the start path. Start
	// NEVER fails on account or bucket state: an unreachable bucket is retried
	// by the loops themselves.
	s.wg.Add(2)
	go func() {
		defer s.wg.Done()
		s.collector.Run(runCtx)
	}()
	go func() {
		defer s.wg.Done()
		s.consumer.Run(runCtx)
	}()

	s.logger.Info("storage observability started",
		"interval", s.collectionInterval(), "report_only", true)
	return nil
}

// Stop halts collection and consumption. Idempotent per the Service contract.
func (s *StorageObservabilityService) Stop(timeout time.Duration) error {
	s.stopOnce.Do(func() {
		if s.cancel != nil {
			s.cancel()
		}
		close(s.stopChan)
	})

	done := make(chan struct{})
	go func() {
		s.wg.Wait()
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(timeout):
		s.logger.Warn("storage observability stop timeout waiting for its loops")
	}

	return s.BaseService.Stop(timeout)
}

// Health reports the storage picture WITHOUT letting it become a verdict.
//
// The lifecycle status comes from BaseService untouched; pressure, capacity and
// over-commitment appear in the message. That separation is the report-only
// guarantee expressed where it would break first: readiness reads Status() and
// IsHealthy(), and /health aggregates Health(), so a pressure-derived verdict at
// any of those points would turn observability into admission control.
func (s *StorageObservabilityService) Health() health.Status {
	return s.surface.describe(s.BaseService.Health())
}

// RegisterMetrics implements the Service interface. The constructor already
// registers through the injected registry, so this is the same idempotent
// registration against whatever registrar a caller supplies.
func (s *StorageObservabilityService) RegisterMetrics(registrar metric.MetricsRegistrar) error {
	if registrar == nil {
		return nil
	}
	return s.metrics.register(registrar)
}

// Snapshot exposes the consumed report for surfaces built on top of this
// service. It is a read of the PUBLISHED report, never a recomputation.
func (s *StorageObservabilityService) Snapshot() natsclient.StorageReportSnapshot {
	return s.consumer.Snapshot()
}

func (s *StorageObservabilityService) collectionInterval() string {
	if s.config.Interval != "" {
		return s.config.Interval
	}
	return natsclient.DefaultStorageInventoryInterval.String()
}

// --- lazy bucket acquisition -------------------------------------------------

// lazyReportBucket acquires the report bucket on first use and caches it.
//
// Acquisition is DEFERRED out of the constructor and out of Start so that a
// deployment declaring this service still boots when JetStream is unreachable,
// the account is unreadable, or the bucket cannot yet be created. Both users
// retry on their own schedule — the collector on its interval, the consumer on
// its watch backoff — so a late-arriving bucket heals without a restart.
type lazyReportBucket struct {
	mu     sync.Mutex
	client *natsclient.Client
	bucket jetstream.KeyValue
	logger *slog.Logger
}

// resolve acquires the bucket through the catalog OWNER seam. This service is
// the STORAGE_REPORT descriptor's declared owner, so it ensures rather than
// opens: create-or-open, reconcile to the declared policy, verify, or fail.
func (b *lazyReportBucket) resolve(ctx context.Context) (jetstream.KeyValue, error) {
	b.mu.Lock()
	defer b.mu.Unlock()

	if b.bucket != nil {
		return b.bucket, nil
	}
	bucket, err := graph.EnsureCatalogBucket(ctx, b.client, graph.BucketStorageReport)
	if err != nil {
		return nil, err
	}
	b.bucket = bucket
	return bucket, nil
}

// WatchAll makes the lazy bucket a natsclient.ReportWatchStore. A resolution
// failure is returned rather than logged here: the consumer's own retry loop
// owns the backoff and the one warning line.
func (b *lazyReportBucket) WatchAll(
	ctx context.Context, opts ...jetstream.WatchOpt,
) (jetstream.KeyWatcher, error) {
	bucket, err := b.resolve(ctx)
	if err != nil {
		return nil, err
	}
	return bucket.WatchAll(ctx, opts...)
}

// lazyReportPublisher is the collector's InventoryPublisher. It builds the real
// publisher on the first collection that finds the bucket reachable, and keeps
// it — the publisher holds the in-process growth baselines, so rebuilding it per
// collection would re-read the bucket history every time.
type lazyReportPublisher struct {
	mu         sync.Mutex
	bucket     *lazyReportBucket
	thresholds natsclient.ThresholdSource
	logger     *slog.Logger
	publisher  *natsclient.StorageReportPublisher
}

func (p *lazyReportPublisher) Publish(
	ctx context.Context, inv natsclient.StorageInventory,
) (natsclient.PublishResult, error) {
	publisher, err := p.resolve(ctx)
	if err != nil {
		return natsclient.PublishResult{}, err
	}
	return publisher.Publish(ctx, inv)
}

func (p *lazyReportPublisher) resolve(ctx context.Context) (*natsclient.StorageReportPublisher, error) {
	p.mu.Lock()
	defer p.mu.Unlock()

	if p.publisher != nil {
		return p.publisher, nil
	}
	bucket, err := p.bucket.resolve(ctx)
	if err != nil {
		return nil, fmt.Errorf("acquire the storage report bucket: %w", err)
	}
	publisher, err := natsclient.NewStorageReportPublisher(bucket, natsclient.StorageReportConfig{
		Thresholds: p.thresholds,
		Logger:     p.logger,
	})
	if err != nil {
		return nil, err
	}
	p.publisher = publisher
	return publisher, nil
}

// --- health rendering --------------------------------------------------------

// storageHealthSurface renders the consumed report as health STATUS text.
//
// It holds no verdict of its own by construction: describe takes the base
// status and only ever rewrites its Message. Pressure never reaches Healthy,
// Status, or a sub-status — not even a nested one. health.Aggregate does not
// recurse today, so a degraded sub-status would be invisible now and become a
// readiness failure the day it does; leaving the verdict axis untouched is the
// version of report-only that cannot rot.
// The two readers are FUNCTIONS rather than the concrete collector and
// consumer, so rendering is exercised directly on the states that matter —
// critical pressure, an over-committed tier, a report nobody has read yet —
// without standing up an account to produce them. Both are read-only accessors
// that do no I/O.
type storageHealthSurface struct {
	snapshotOf  func() natsclient.StorageReportSnapshot
	inventoryOf func() natsclient.StorageInventory
}

func (s *storageHealthSurface) snapshot() natsclient.StorageReportSnapshot {
	if s.snapshotOf == nil {
		return natsclient.StorageReportSnapshot{}
	}
	return s.snapshotOf()
}

// describe appends the storage picture to the base verdict WITHOUT replacing it.
// The base Message is the only field carrying the REASON for a non-healthy
// lifecycle verdict ("Service is stopped", "Service is unhealthy (failed
// checks: N)" — service/base.go), so overwriting it would delete the
// explanation from the exact surface an operator reads while leaving the status
// axis saying something is wrong. That is this capability's own phantom-signal
// class turned on its own health report.
func (s *storageHealthSurface) describe(base health.Status) health.Status {
	picture := s.message()
	if base.Message == "" {
		base.Message = picture
		return base
	}
	base.Message = base.Message + "; " + picture
	return base
}

// message renders the storage picture. It is deliberately one deterministic
// line: health is read by operators and by log scrapers, and an unstable
// rendering is a diff nobody can use.
func (s *storageHealthSurface) message() string {
	snapshot := s.snapshot()
	var parts []string

	if !snapshot.Synced {
		parts = append(parts, "storage report has not been read yet")
	} else {
		counts := snapshot.PressureCounts
		parts = append(parts, fmt.Sprintf(
			"storage pressure (report-only): critical=%d high=%d warning=%d normal=%d not-evaluated=%d",
			counts[natsclient.PressureCritical], counts[natsclient.PressureHigh],
			counts[natsclient.PressureWarning], counts[natsclient.PressureNormal],
			snapshot.NotEvaluated))

		if worst := s.worstResources(snapshot); worst != "" {
			parts = append(parts, "worst: "+worst)
		}
	}

	if snapshot.AccountKnown {
		if tiers := accountFindings(snapshot.Account); tiers != "" {
			parts = append(parts, tiers)
		}
	}

	if collected := s.collectionState(); collected != "" {
		parts = append(parts, collected)
	}
	return strings.Join(parts, "; ")
}

// maxNamedResources bounds the health message. An account can carry hundreds of
// streams, and a message that grew with the account would be unreadable in the
// place operators read it first.
const maxNamedResources = 5

// worstResources names the resources an operator should look at: everything
// above normal, worst first, then the resources carrying no state at all —
// which is what keeps an unbounded resource from being invisible in a summary
// organized by state.
func (s *storageHealthSurface) worstResources(snapshot natsclient.StorageReportSnapshot) string {
	type named struct {
		name     string
		severity int
		label    string
	}
	var candidates []named

	for _, row := range snapshot.Resources {
		switch {
		case row.Pressure.Evaluated && row.Pressure.State != natsclient.PressureNormal:
			candidates = append(candidates, named{
				name:     row.Resource.Name,
				severity: pressureSeverityValue(row.Pressure.State),
				label:    fmt.Sprintf("%s %s (%s)", row.Resource.Name, row.Pressure.State, row.Pressure.RaisedBy),
			})
		case !row.Pressure.Evaluated:
			candidates = append(candidates, named{
				name:     row.Resource.Name,
				severity: -1,
				label:    fmt.Sprintf("%s not-evaluated (%s)", row.Resource.Name, row.Pressure.Unavailable),
			})
		}
	}
	if len(candidates) == 0 {
		return ""
	}

	sort.Slice(candidates, func(i, j int) bool {
		if candidates[i].severity != candidates[j].severity {
			return candidates[i].severity > candidates[j].severity
		}
		return candidates[i].name < candidates[j].name
	})

	labels := make([]string, 0, maxNamedResources)
	for _, candidate := range candidates {
		if len(labels) == maxNamedResources {
			labels = append(labels, fmt.Sprintf("and %d more", len(candidates)-maxNamedResources))
			break
		}
		labels = append(labels, candidate.label)
	}
	return strings.Join(labels, ", ")
}

// accountFindings names the over-committed tiers and the tiers whose comparison
// could not be made. Both are findings; neither is a gate.
func accountFindings(report natsclient.AccountReport) string {
	var findings []string
	for _, comparison := range report.Tiers {
		switch comparison.State {
		case natsclient.OvercommitmentOver:
			findings = append(findings, fmt.Sprintf("%s over-committed (declared %d bytes)",
				comparison.Tier, comparison.DeclaredBytes))
		case natsclient.OvercommitmentNotApplicable:
			if comparison.Unavailable == natsclient.OvercommitmentUnavailableUnknownLimit {
				findings = append(findings, fmt.Sprintf("%s account limit unknown", comparison.Tier))
			}
		}
	}
	if len(findings) == 0 {
		return ""
	}
	return "account: " + strings.Join(findings, ", ")
}

// collectionState reports FRESHNESS on the answer rather than gating on it. A
// collection that failed leaves last-good in place, and how stale that is
// belongs on the report, not in a decision to withhold it.
func (s *storageHealthSurface) collectionState() string {
	if s.inventoryOf == nil {
		return ""
	}
	inventory := s.inventoryOf()
	if inventory.Stale {
		return fmt.Sprintf("last collection did not succeed (%s); serving last known result",
			inventory.StaleReason)
	}
	if inventory.CollectedAt.IsZero() {
		return ""
	}
	return fmt.Sprintf("collected %s ago by %s",
		time.Since(inventory.CollectedAt).Round(time.Second), inventory.ProducedBy)
}

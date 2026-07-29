package service

import (
	"context"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"github.com/c360studio/semstreams/graph"
	"github.com/c360studio/semstreams/internal/builtinprojection"
	"github.com/c360studio/semstreams/metric"
	"github.com/c360studio/semstreams/natsclient"
	"github.com/c360studio/semstreams/pkg/lifecycle"
	"github.com/c360studio/semstreams/pkg/ownership"
	"github.com/c360studio/semstreams/pkg/projection"
	"github.com/c360studio/semstreams/vocabulary"
)

// OwnershipService is the Phase-B service wrapper for the ownership subsystem
// (ADR-058). It runs the two process-lifetime ownership goroutines: the static
// heartbeater (keeps the owner presence key alive in OWNER_PRESENCE) and the
// WatchRevival quiesce watcher (ADR-056 PR-4 — quiesces this process's owners
// if a different incarnation takes them over).
//
// The ownership Registry (the Phase-A identity) is owned by the composition
// root and passed in — this Service NEVER constructs it (ADR-058 R2: identity
// is created once in Phase A, never in Start).
//
// Start is infallible for soft failures (ADR-058 R1): a nil registry means
// ownership is disabled this boot; Start returns nil and the service runs
// idle rather than aborting the whole process via StartAll's first-error gate.
type OwnershipService struct {
	*BaseService
	logger   *slog.Logger        // own logger (mirrors HeartbeatService); set by ctor.
	reg      *ownership.Registry // R2: owned by Phase A, borrowed here.
	staticHB *ownership.Heartbeater
	metrics  *metric.MetricsRegistry // for WatchRevival's owner_revival_quiesce_total counter (ADR-056 PR-4)
	mu       sync.Mutex              // serializes Start/Stop so the re-entrancy guard + launch are atomic
	cancel   context.CancelFunc
	wg       sync.WaitGroup
}

// NewOwnershipService builds the OwnershipService. reg and staticHB may both be
// nil (the "ownership disabled this boot" path); Start detects nil reg and runs
// idle with no goroutines.
func NewOwnershipService(reg *ownership.Registry, staticHB *ownership.Heartbeater, metrics *metric.MetricsRegistry, logger *slog.Logger) *OwnershipService {
	if logger == nil {
		logger = slog.Default()
	}
	return &OwnershipService{
		BaseService: NewBaseServiceWithOptions("ownership", nil,
			WithLogger(logger),
		),
		logger:   logger,
		reg:      reg,
		staticHB: staticHB,
		metrics:  metrics,
	}
}

// Start starts the ownership service. Infallible on soft failures (ADR-058 R1):
// a nil registry logs and returns nil (idle, no goroutines). A double-Start
// returns an error — that is a BUG-CLASS caller error, not a soft failure.
func (s *OwnershipService) Start(ctx context.Context) error {
	// s.mu makes the re-entrancy guard + goroutine launch ATOMIC: BaseService.Start
	// returns nil-if-already-running (not an error), so the Status() check alone is a
	// TOCTOU race under concurrent Start — two callers could both pass it and each
	// launch a goroutine. The lock, not the check, is what serializes. A double-Start
	// is a programming error, NOT an R1 soft failure, so erroring here is correct.
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.Status() == StatusRunning {
		return fmt.Errorf("ownership service already running")
	}
	// Mark the service running — even on the disabled path it is
	// intentionally-disabled-but-healthy, not crashed, so Status/Health report
	// correctly rather than "stopped".
	if err := s.BaseService.Start(ctx); err != nil {
		return err
	}
	if s.reg == nil {
		// R1 (disabled this boot) + R3 (no consumers): idle, no goroutines.
		s.logger.Info("ownership service: no registry — running idle (disabled this boot)")
		return nil
	}
	runCtx, cancel := context.WithCancel(ctx)
	s.cancel = cancel
	s.wg.Add(2)
	go func() { defer s.wg.Done(); s.staticHB.Run(runCtx) }()
	// ADR-056 PR-4: watch the OWNER_CLAIMS epoch and quiesce any of this process's
	// owners a different incarnation takes over. Joined via s.wg on Stop.
	go func() { defer s.wg.Done(); _ = s.reg.WatchRevival(runCtx, s.metrics) }()
	return nil
}

// Stop cancels the running goroutine and waits for it to finish with a timeout.
// Mirrors HeartbeatService.Stop's inline join pattern.
func (s *OwnershipService) Stop(timeout time.Duration) error {
	s.mu.Lock()
	cancel := s.cancel
	s.mu.Unlock()
	if cancel != nil {
		cancel() // signal (nil on the disabled path); join outside the lock below
	}
	// Join with timeout — inline, mirroring HeartbeatService.Stop.
	done := make(chan struct{})
	go func() { s.wg.Wait(); close(done) }()
	select {
	case <-done:
	case <-time.After(timeout):
		s.logger.Warn("ownership service: stop timeout waiting for goroutines")
	}
	return s.BaseService.Stop(timeout)
}

// WireOwnership performs Phase-A ownership wiring (ADR-058): create buckets,
// construct the Registry, attach it to the lifecycle Manager, and make exactly
// one aggregate projection-client binding. Bootstrap or binding failure is a
// boot error: built-in writers have no raw mutation fallback.
func WireOwnership(
	ctx context.Context,
	natsClient *natsclient.Client,
	lcm *lifecycle.Manager,
	logger *slog.Logger,
	contracts ...projection.Contract,
) (*ownership.Registry, *ownership.Heartbeater, *projection.MutationClient, error) {
	if logger == nil {
		logger = slog.Default()
	}

	// ADR-068 D1 pre-start LEGACY-DRIFT BACKSTOP (framework-bucket-catalog).
	// Its ONE honest job: a catalog bucket whose owner is NOT deployed in this
	// composition (e.g. an EMBEDDING_INDEX left by a prior semantic deploy when
	// booting a statistical configuration) never has its acquisition seam
	// called, so this single boot-time pass over the catalog's no-lifecycle
	// descriptors strips prior-boot/out-of-band retention dirt — or fails boot
	// closed — for exactly those owner-absent buckets. Skip-if-absent: a
	// not-yet-provisioned bucket is passed over.
	//
	// Deployed owners need no pass: each reconciles its buckets to the catalog
	// policy INSIDE its own Start via the acquisition seam, which also covers
	// this boot's create-races and post-boot dynamic re-acquisition (there is
	// no post-start sweep anymore). This backstop is a DISTINCT concern from
	// ADR-058 ownership; it is folded into this one shared boot function ON
	// PURPOSE — both cmd/semstreams and cmd/e2e-semstreams call WireOwnership
	// exactly once before StartAll, so wiring it here covers both binaries
	// with no half-migration drift (the beta.18 lesson).
	if err := graph.AssertOwnedBucketsClean(ctx, natsClient, logger); err != nil {
		return nil, nil, nil, fmt.Errorf("assert framework-owned graph buckets retention-clean: %w", err)
	}

	reg, err := ownership.EnsureBuckets(ctx, natsClient, logger, vocabulary.InverseResolver)
	if err != nil {
		return nil, nil, nil, fmt.Errorf("bootstrap ownership buckets: %w", err)
	}
	if lcm == nil {
		return nil, nil, nil, fmt.Errorf("attach ownership: lifecycle manager is required")
	}
	lcm.AttachOwnership(ctx, reg)

	staticHB := reg.NewHeartbeater(ownership.HeartbeatInterval)
	client, err := projection.BindMutationClient(ctx, projection.MutationClientConfig{
		NATS:        natsClient,
		Registry:    reg,
		Heartbeater: staticHB,
		Owner:       builtinprojection.OwnerID,
		Contracts:   contracts,
	})
	if err != nil {
		return nil, nil, nil, fmt.Errorf("bind static projection mutation client: %w", err)
	}
	return reg, staticHB, client, nil
}

// WireOwnershipShutdown is the ADR-058 rollout-step-2 drift-killer. It returns
// the shutdown-cancellable context that governs the lifecycle Manager-internal
// ownership heartbeater (spawned eagerly in Phase A by AttachOwnership inside
// WireOwnership — see manager.go) and a single cleanup func that, on shutdown,
// SIGNALS (cancel) then JOINS the Manager's heartbeat and graph-state guard via
// WaitOwnership, in that order.
//
// Why a shared helper and not a Service: per ADR-058 the lifecycle Manager is
// deliberately NOT wrapped as a service.Service — the heartbeater stays
// Phase-A-spawned (preserving boot-time spawn behavior; an import cycle would
// force a ceremony wrapper anyway). But the cancel+join was hand-rolled
// identically in both mains, which is the beta.18 half-migration drift class
// ADR-058 exists to prevent. Folding it into one call both mains make
// identically removes that prospective drift structurally.
//
// The caller MUST pass the returned ctx to WireOwnership (so AttachOwnership's
// heartbeater binds to it) and `defer` the returned func at the same point the
// hand-rolled hbCancel/WaitOwnership defers lived, so LIFO still runs cancel+join
// BEFORE the earlier-registered NATS Close defer (the gh#279 join, ADR-056 PR-4).
//
// With no registry attached, WaitOwnership still joins a graph-state guard if a
// lifecycle watcher started one; otherwise it returns immediately.
func WireOwnershipShutdown(ctx context.Context, lcm *lifecycle.Manager) (context.Context, func()) {
	hbCtx, hbCancel := context.WithCancel(ctx)
	return hbCtx, func() {
		hbCancel()          // signal the Manager-internal ownership heartbeater
		lcm.WaitOwnership() // join it before NATS Close (gh#279)
	}
}

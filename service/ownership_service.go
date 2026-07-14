package service

import (
	"context"
	"fmt"
	"log/slog"
	"sync"
	"time"

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
// construct the Registry (R2 — once, here), attach it to the lifecycle Manager,
// and bind static projection contracts. Best-effort (R1): a bucket-bootstrap
// failure logs and returns nil, nil; callers treat nil as "ownership disabled
// this boot" and pass it straight to NewOwnershipService (which no-ops on nil).
//
// Returns the Registry and its static heartbeater so the caller can (a) start
// the Phase-B OwnershipService and (b) bind rule-pack contracts AFTER the rule
// processors are constructed.
//
// contracts is the set of static projection contracts to bind at boot (e.g.,
// the loop-execution contract from loopExecutionProjectionContract()). Passing
// them as a variadic keeps the service package free of main-package functions.
func WireOwnership(
	ctx context.Context,
	natsClient *natsclient.Client,
	lcm *lifecycle.Manager,
	logger *slog.Logger,
	contracts ...projection.Contract,
) (*ownership.Registry, *ownership.Heartbeater) {
	if logger == nil {
		logger = slog.Default()
	}
	reg, err := ownership.EnsureBuckets(ctx, natsClient, logger, vocabulary.InverseResolver)
	if err != nil {
		logger.Warn("ownership: bucket bootstrap failed — disabled this boot",
			slog.Any("error", err))
		return nil, nil // R1: degrade, do not abort.
	}
	// nil-safe; spawns the Manager-internal heartbeater, joined via lcm.WaitOwnership().
	lcm.AttachOwnership(ctx, reg)

	staticHB := reg.NewHeartbeater(ownership.HeartbeatInterval)
	for _, contract := range contracts {
		if _, err := projection.BindAndHeartbeat(ctx, reg, staticHB,
			"agentic-loop-graph-writer", contract); err != nil {
			logger.Warn("ownership: projection contract bind failed",
				slog.String("contract", contract.Name),
				slog.Any("error", err))
		}
	}
	return reg, staticHB
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

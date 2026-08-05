package service

import (
	"context"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"github.com/c360studio/semstreams/agentic/agentrun"
	"github.com/c360studio/semstreams/natsclient"
)

// milestoneStarter is the subscriber seam MilestoneService drives. The production
// implementation is *agentrun.MilestoneSubscriber; tests inject a fake so the
// wrapper's Start/Stop lifecycle — including the error-forward + status rollback —
// is unit-testable without a live NATS connection (every Start path otherwise
// requires the subscriber's real durable-consumer setup).
type milestoneStarter interface {
	Start(ctx context.Context, client *natsclient.Client, cfg agentrun.StartConfig) (func(), error)
}

// MilestoneService is the Phase-B service wrapper for the agent-run milestone
// subscriber (ADR-058 rollout step 3). It drives the subscriber's durable
// JetStream consumers (agent.complete.* / agent.failed.*) under the
// ServiceManager's ordered shutdown, replacing the hand-rolled
// NewMilestoneSubscriber + Start + defer-stop block that was duplicated in both
// mains.
//
// Start returns subscriber setup errors so a configured milestone observer never
// reports running without its durable consumers. A genuine consumer-start
// failure is forwarded so StartAll aborts boot. The stream-absent case is already a graceful no-op inside
// the subscriber (gh#246) — it returns a no-op stop with no error — so resourceless
// deploys (no agentic components) still boot.
//
// Shutdown semantics: Start receives the ServiceManager's lifecycle ctx (the
// SIGTERM-derived signal context), which the subscriber binds its consumers to.
// The old inline wiring passed an uncancellable context, so in-flight HandleEvent
// calls ran to completion at shutdown; now reads and handlers observe cancellation.
// Stop() additionally cancels consumption via the
// captured stop func, so delivery halts regardless of ctx.
type MilestoneService struct {
	*BaseService
	logger     *slog.Logger
	subscriber milestoneStarter
	client     *natsclient.Client   // live NATS conn (Phase A), passed to subscriber.Start.
	cfg        agentrun.StartConfig // StreamName: agentrun.AgentStreamName.
	mu         sync.Mutex           // serializes Start/Stop so the re-entrancy guard is atomic.
	stop       func()               // captured stop func from subscriber.Start; nil until started / after Stop.
}

// NewMilestoneService builds the MilestoneService over a pre-built subscriber.
// The composition root constructs the subscriber (R2 — this wrapper never does)
// and passes it plus the live NATS client and StartConfig.
func NewMilestoneService(subscriber milestoneStarter, client *natsclient.Client, cfg agentrun.StartConfig, logger *slog.Logger) *MilestoneService {
	if logger == nil {
		logger = slog.Default()
	}
	return &MilestoneService{
		BaseService: NewBaseServiceWithOptions("milestone", nil, WithLogger(logger)),
		logger:      logger,
		subscriber:  subscriber,
		client:      client,
		cfg:         cfg,
	}
}

// Start starts the subscriber's durable consumers. A double-Start returns an
// error as a bug-class guard. A genuine consumer-start
// failure is FORWARDED — StartAll aborts boot — because the subscriber is a hard
// dependency (see the type doc for why this is deliberately not R1-degraded). The
// subscriber's stream-absent graceful-skip returns a non-nil no-op stop with no
// error, so that path stores the no-op and reports running (boot preserved).
func (s *MilestoneService) Start(ctx context.Context) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.Status() == StatusRunning {
		return fmt.Errorf("milestone service already running")
	}
	if err := s.BaseService.Start(ctx); err != nil {
		return err
	}
	stop, err := s.subscriber.Start(ctx, s.client, s.cfg)
	if err != nil {
		// Forward the error so StartAll does not report an observer with no
		// consumers. Roll back BaseService status after the failed start.
		_ = s.BaseService.Stop(0)
		return fmt.Errorf("milestone subscriber start: %w", err)
	}
	s.stop = stop
	return nil
}

// Stop cancels the subscriber's local consumption (durable offsets persist in
// NATS for restart recovery). The wrapper spawns no goroutine of its own, so —
// there is nothing to join. Idempotent: a second Stop
// (or a Stop before Start) is a no-op.
func (s *MilestoneService) Stop(timeout time.Duration) error {
	s.mu.Lock()
	stop := s.stop
	s.stop = nil
	s.mu.Unlock()
	if stop != nil {
		stop()
	}
	return s.BaseService.Stop(timeout)
}

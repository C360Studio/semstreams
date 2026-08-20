package service

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"sync"

	"github.com/c360studio/semstreams/agentic/agentrun"
	"github.com/c360studio/semstreams/internal/lifecyclecleanup"
	"github.com/c360studio/semstreams/natsclient"
	semerrs "github.com/c360studio/semstreams/pkg/errs"
)

// milestoneStarter is the subscriber seam MilestoneService drives. The production
// implementation is *agentrun.MilestoneSubscriber; tests inject a fake so the
// wrapper's Start/Stop lifecycle — including the error-forward + status rollback —
// is unit-testable without a live NATS connection (every Start path otherwise
// requires the subscriber's real durable-consumer setup).
type milestoneStarter interface {
	Start(ctx context.Context, client *natsclient.Client, cfg agentrun.StartConfig) (func(context.Context) error, error)
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
	logger         *slog.Logger
	subscriber     milestoneStarter
	client         *natsclient.Client   // live NATS conn (Phase A), passed to subscriber.Start.
	cfg            agentrun.StartConfig // StreamName: agentrun.AgentStreamName.
	mu             sync.Mutex
	used           bool
	running        bool
	stopping       bool
	terminal       bool
	cleanupPending bool
	startDone      chan struct{}
	stop           func(context.Context) error
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
	if err := validateLifecycleContext(ctx, "MilestoneService", "Start"); err != nil {
		return err
	}
	if err := ctx.Err(); err != nil {
		return semerrs.WrapInvalid(err, "MilestoneService", "Start", "context already ended")
	}
	s.mu.Lock()
	if s.used {
		s.mu.Unlock()
		return semerrs.WrapFatal(semerrs.ErrAlreadyStarted,
			"MilestoneService", "Start", "service instance already used")
	}
	s.used = true
	s.startDone = make(chan struct{})
	startDone := s.startDone
	s.mu.Unlock()
	defer close(startDone)

	if err := s.BaseService.Start(ctx); err != nil {
		s.mu.Lock()
		s.terminal = true
		s.mu.Unlock()
		return err
	}
	stop, err := s.subscriber.Start(ctx, s.client, s.cfg)
	s.mu.Lock()
	if stop != nil {
		s.stop = stop
	}
	if err == nil {
		s.running = true
		s.mu.Unlock()
		return nil
	}
	s.cleanupPending = stop != nil
	s.mu.Unlock()

	rollbackErr := lifecyclecleanup.RollbackFailedStart(ctx, func(cleanupCtx context.Context) error {
		var subscriberErr error
		if stop != nil {
			subscriberErr = stop(cleanupCtx)
		}
		return errors.Join(subscriberErr, s.BaseService.Stop(cleanupCtx))
	})
	s.mu.Lock()
	if rollbackErr == nil {
		s.stop = nil
		s.cleanupPending = false
		s.terminal = true
	} else {
		s.cleanupPending = true
	}
	s.mu.Unlock()
	return errors.Join(fmt.Errorf("milestone subscriber start: %w", err), rollbackErr)
}

// Stop cancels the subscriber's local consumption (durable offsets persist in
// NATS for restart recovery). Running Stop is terminal and completed repeats
// are nil no-ops. Only failed-Start cleanupPending may retry its retained opaque
// cleanup closure under a later manager Stop context. Stop before Start is a no-op.
func (s *MilestoneService) Stop(ctx context.Context) error {
	if err := validateLifecycleContext(ctx, "MilestoneService", "Stop"); err != nil {
		return err
	}
	for {
		s.mu.Lock()
		startDone := s.startDone
		if startDone != nil {
			select {
			case <-startDone:
			default:
				s.mu.Unlock()
				select {
				case <-startDone:
					continue
				case <-ctx.Done():
					return fmt.Errorf("wait for MilestoneService Start: %w", ctx.Err())
				}
			}
		}
		if s.stopping {
			s.mu.Unlock()
			return semerrs.WrapTransient(errors.New("milestone service stop already in progress"),
				"MilestoneService", "Stop", "concurrent Stop is unsupported")
		}
		if s.terminal {
			s.mu.Unlock()
			return nil
		}
		if !s.used {
			s.used = true
			s.terminal = true
			s.mu.Unlock()
			return s.BaseService.Stop(ctx)
		}
		failedStart := s.cleanupPending
		stop := s.stop
		s.stopping = true
		s.mu.Unlock()

		var subscriberErr error
		if stop != nil {
			subscriberErr = stop(ctx)
		}
		stopErr := errors.Join(subscriberErr, s.BaseService.Stop(ctx))
		s.mu.Lock()
		if failedStart && stopErr != nil {
			s.stopping = false
			s.mu.Unlock()
			return fmt.Errorf("stop milestone subscriber cleanup: %w", stopErr)
		}
		s.stop = nil
		s.running = false
		s.cleanupPending = false
		s.stopping = false
		s.terminal = true
		s.mu.Unlock()
		if stopErr != nil {
			return fmt.Errorf("stop milestone subscriber: %w", stopErr)
		}
		return nil
	}
}

package llmwrap

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/c360studio/semstreams/graph"
	"github.com/c360studio/semstreams/internal/graphmutation"
	"github.com/c360studio/semstreams/message"
	"github.com/c360studio/semstreams/natsclient"
)

// TriplePublisher is the narrow surface the research-graph chain
// components use to stamp orchestration triples on the research-pipeline
// loop entity (e.g., research.classify.complete, research.route.action).
// Each per-stage completion stamps a small atomic batch sharing one
// Subject so graph-ingest's per-Subject CAS path preserves the rule-
// matching contract (R2 / R4 branches read action / sufficient triples
// paired with their respective complete triples — a partial write would
// corrupt the dispatch).
//
// Two emission shapes:
//
//   - Create: birth the pipeline loop with its typed-origin envelope. Append is
//     must-exist, so the research_graph kickoff uses strict entity.create.
//   - Append: append exact tuples onto an existing entity. The wire response
//     reports each subject independently; stage stamps share one subject.
//
// Production satisfies it with *natsClient adapter; tests substitute an
// in-memory recorder so the per-component handler suites don't need a
// live NATS connection.
type TriplePublisher interface {
	Create(ctx context.Context, entityID string, msgType message.Type, triples []message.Triple) error
	Append(ctx context.Context, triples []message.Triple) error
}

const graphMutationTimeout = 5 * time.Second

// natsTriplePublisher adapts natsclient.Client to TriplePublisher. The
// The graph mutation client sends every request once. Transport ambiguity is
// returned to the component; no automatic retry occurs.
type natsTriplePublisher struct {
	client *graphmutation.Client
}

// NewNATSTriplePublisher builds a TriplePublisher backed by the
// graph.mutation NATS surfaces. Returns nil when client is nil — the
// research-graph components treat a nil publisher as "no graph
// emission" and log warn at handler dispatch (the chain becomes
// observability-only rather than fatal).
func NewNATSTriplePublisher(client *natsclient.Client) TriplePublisher {
	if client == nil {
		return nil
	}
	wire, _ := graphmutation.NewClient(client, graphMutationTimeout)
	return &natsTriplePublisher{client: wire}
}

func (p *natsTriplePublisher) Append(ctx context.Context, triples []message.Triple) error {
	if len(triples) == 0 {
		return nil
	}
	if p == nil || p.client == nil {
		return errors.New("graph mutation client is required")
	}
	response, err := p.client.Append(ctx, graph.AppendTriplesRequest{Triples: triples})
	if err != nil {
		return fmt.Errorf("append graph triples: %w", err)
	}
	for _, result := range response.Results {
		switch result.Outcome {
		case graph.MutationApplied, graph.MutationUnchanged:
			continue
		case graph.MutationFailed:
			return fmt.Errorf("append graph triples for %s: %s/%s", result.EntityID, result.Error.Class, result.Error.Code)
		default:
			return fmt.Errorf("append graph triples for %s: %s", result.EntityID, result.Outcome)
		}
	}
	return nil
}

// Create births a graph entity with its typed-origin MessageType and initial
// triples through graph.mutation.entity.create. Append is must-exist, so this is
// the required first write for a research-pipeline loop entity. A collision is
// returned as the classified create conflict; this helper does not retry or
// reinterpret matching authority as its own success.
func (p *natsTriplePublisher) Create(ctx context.Context, entityID string, msgType message.Type, triples []message.Triple) error {
	if p == nil || p.client == nil {
		return errors.New("graph mutation client is required")
	}
	_, err := p.client.Create(ctx, graph.CreateEntityRequest{
		Entity: &graph.EntityState{
			ID:          entityID,
			MessageType: msgType,
		},
		Triples: triples,
	})
	if err != nil {
		return fmt.Errorf("create graph entity: %w", err)
	}
	return nil
}

// StampLogger is the subset of *slog.Logger the StampOrchestrationTriples
// helper needs. Defined to keep the helper unit-testable without forcing
// a slog handler into the test (a *slog.Logger satisfies it directly).
type StampLogger interface {
	Warn(msg string, args ...any)
}

// StampOrchestrationTriples is the common per-stage emission path the
// five research-graph components share: call the TriplePublisher with
// the pre-built triple batch and log warn on failure. Failure is
// non-fatal at the handler level — the per-stage envelope already
// landed in AGENT_LOOPS via the chain's KV writes, so the operator
// sees the chain stall in trajectory data even when triple stamping
// fails. Returning the error lets handler-level metrics count it; the
// helper does not retry beyond the publisher's built-in retry budget.
//
// pub == nil is treated as observability-disabled (degraded mode):
// the helper logs warn and returns nil rather than panicking. This
// matches the contract NewNATSTriplePublisher establishes — nil
// natsclient.Client → nil publisher → degraded chain visibility.
func StampOrchestrationTriples(ctx context.Context, pub TriplePublisher, logger StampLogger, stage, loopID string, triples []message.Triple) error {
	if pub == nil {
		if logger != nil {
			logger.Warn("orchestration triple-stamp skipped: publisher is nil",
				"stage", stage, "loop_id", loopID, "triple_count", len(triples))
		}
		return nil
	}
	if err := pub.Append(ctx, triples); err != nil {
		if logger != nil {
			logger.Warn("orchestration triple-stamp failed; chain rules may not fire",
				"stage", stage, "loop_id", loopID, "triple_count", len(triples), "error", err)
		}
		return err
	}
	return nil
}

// BirthLoopEntityWithTriples births the research-pipeline loop entity with a
// typed-origin MessageType envelope, carrying the kickoff triples atomically via
// entity.create. This is the FIRST stamp on a pipeline loop entity — the
// entity must be CREATED (not auto-vivified) so subsequent per-stage stamps can
// append onto it; a bare add_batch to a never-created entity is rejected by
// graph-ingest's must-exist contract and the chain stalls before any rule fires
// (gh#390).
//
// Same degraded-mode + warn-on-failure contract as StampOrchestrationTriples:
// pub == nil logs warn and returns nil (observability-disabled), and a publish
// failure is logged warn and returned for handler-level metrics. The kickoff
// stays best-effort and non-fatal — a birth failure stalls the chain observably
// (no rule fires; operator sees the gap in trajectory data) rather than crashing
// the parent loop.
func BirthLoopEntityWithTriples(ctx context.Context, pub TriplePublisher, logger StampLogger, stage, loopID, entityID string, msgType message.Type, triples []message.Triple) error {
	if pub == nil {
		if logger != nil {
			logger.Warn("orchestration loop-birth skipped: publisher is nil",
				"stage", stage, "loop_id", loopID, "triple_count", len(triples))
		}
		return nil
	}
	if err := pub.Create(ctx, entityID, msgType, triples); err != nil {
		if logger != nil {
			logger.Warn("orchestration loop-birth failed; chain rules may not fire",
				"stage", stage, "loop_id", loopID, "entity_id", entityID, "triple_count", len(triples), "error", err)
		}
		return err
	}
	return nil
}

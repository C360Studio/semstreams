package llmwrap

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/c360studio/semstreams/graph"
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
//   - AddTriple: single-triple convenience. Available for symmetry with
//     the agentictools.TriplePublisher pattern; the chain's per-stage
//     stamps all batch.
//   - AddTriplesBatch: atomic per-Subject. The graph-ingest handler
//     groups triples by Subject and CAS-applies them per entity, so a
//     batch sharing one Subject is fully atomic.
//
// Production satisfies it with *natsClient adapter; tests substitute an
// in-memory recorder so the per-component handler suites don't need a
// live NATS connection.
type TriplePublisher interface {
	AddTriple(ctx context.Context, triple message.Triple) error
	AddTriplesBatch(ctx context.Context, triples []message.Triple) error
}

const (
	// graphMutationAddSubject is the single-triple graph-ingest path.
	graphMutationAddSubject = "graph.mutation.triple.add"

	// graphMutationAddBatchSubject is the atomic per-Subject CAS path
	// (ADR-036 Stage 2). Used by AddTriplesBatch for co-located triples
	// where partial-write orphans would corrupt downstream rule matching.
	graphMutationAddBatchSubject = "graph.mutation.triple.add_batch"

	// graphMutationTimeout matches the bounds used by decide /
	// agentic-loop / write_todos against the same handler.
	graphMutationTimeout = 5 * time.Second
)

// natsTriplePublisher adapts natsclient.Client to TriplePublisher. The
// Request paths use RequestWithRetry so transient "no responders" errors
// (graph-gateway restart, subscription propagation lag) don't silently
// drop chain-state triples — the chain's dispatch depends on these
// landing.
type natsTriplePublisher struct {
	client *natsclient.Client
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
	return &natsTriplePublisher{client: client}
}

func (p *natsTriplePublisher) AddTriple(ctx context.Context, triple message.Triple) error {
	req := graph.AddTripleRequest{Triple: triple}
	reqData, err := json.Marshal(req)
	if err != nil {
		return fmt.Errorf("marshal add-triple request: %w", err)
	}
	respData, err := p.client.RequestWithRetry(ctx, graphMutationAddSubject, reqData, graphMutationTimeout, natsclient.DefaultRetryConfig())
	if err != nil {
		return fmt.Errorf("request %s: %w", graphMutationAddSubject, err)
	}
	var resp graph.AddTripleResponse
	if err := json.Unmarshal(respData, &resp); err != nil {
		return fmt.Errorf("unmarshal response: %w", err)
	}
	if !resp.Success {
		return fmt.Errorf("graph-ingest rejected triple: %s", resp.Error)
	}
	return nil
}

func (p *natsTriplePublisher) AddTriplesBatch(ctx context.Context, triples []message.Triple) error {
	if len(triples) == 0 {
		return nil
	}
	req := graph.AddTriplesBatchRequest{Triples: triples}
	reqData, err := json.Marshal(req)
	if err != nil {
		return fmt.Errorf("marshal batch-add request: %w", err)
	}
	respData, err := p.client.RequestWithRetry(ctx, graphMutationAddBatchSubject, reqData, graphMutationTimeout, natsclient.DefaultRetryConfig())
	if err != nil {
		return fmt.Errorf("request %s: %w", graphMutationAddBatchSubject, err)
	}
	var resp graph.AddTriplesBatchResponse
	if err := json.Unmarshal(respData, &resp); err != nil {
		return fmt.Errorf("unmarshal batch response: %w", err)
	}
	if !resp.Success {
		return fmt.Errorf("graph-ingest rejected batch: %s", resp.Error)
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
	if err := pub.AddTriplesBatch(ctx, triples); err != nil {
		if logger != nil {
			logger.Warn("orchestration triple-stamp failed; chain rules may not fire",
				"stage", stage, "loop_id", loopID, "triple_count", len(triples), "error", err)
		}
		return err
	}
	return nil
}

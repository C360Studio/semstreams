package llmwrap

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/c360studio/semstreams/graph"
	"github.com/c360studio/semstreams/message"
	"github.com/c360studio/semstreams/natsclient"
	"github.com/c360studio/semstreams/pkg/errs"
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
// Three emission shapes:
//
//   - CreateEntityWithTriples: BIRTH. The FIRST write to a pipeline loop
//     entity must CREATE it with a typed-origin MessageType envelope —
//     graph-ingest enforces must-exist on triple.add / add_batch, so a bare
//     batch to a never-created entity is rejected ("kv: key not found") and
//     the chain stalls before any rule fires (gh#390). The research_graph
//     kickoff uses this; subsequent per-stage stamps append.
//   - AddTriple: single-triple convenience. Available for symmetry with
//     the agentictools.TriplePublisher pattern; the chain's per-stage
//     stamps all batch.
//   - AddTriplesBatch: atomic per-Subject APPEND onto an already-born
//     entity. The graph-ingest handler groups triples by Subject and
//     CAS-applies them per entity, so a batch sharing one Subject is fully
//     atomic.
//
// Production satisfies it with *natsClient adapter; tests substitute an
// in-memory recorder so the per-component handler suites don't need a
// live NATS connection.
type TriplePublisher interface {
	CreateEntityWithTriples(ctx context.Context, entityID string, msgType message.Type, triples []message.Triple) error
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

	// graphMutationCreateWithTriplesSubject births an entity with a
	// typed-origin envelope, carrying triples atomically. The correct verb
	// for the FIRST write to a pipeline loop entity (gh#390) — add / add_batch
	// enforce must-exist and reject a never-created entity.
	graphMutationCreateWithTriplesSubject = "graph.mutation.entity.create_with_triples"

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
	// gh#93 Phase 2: Classified variant surfaces handler errors via err.
	respData, err := p.client.RequestWithRetryClassified(ctx, graphMutationAddSubject, reqData, graphMutationTimeout, natsclient.DefaultRetryConfig())
	if err != nil {
		return fmt.Errorf("request %s: %w", graphMutationAddSubject, err)
	}
	// ADR-060: handler failures arrive as the classified err above.
	var resp graph.AddTripleResponse
	if err := json.Unmarshal(respData, &resp); err != nil {
		return fmt.Errorf("unmarshal response: %w", err)
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
	// gh#93 Phase 2: Classified variant surfaces handler errors via err.
	respData, err := p.client.RequestWithRetryClassified(ctx, graphMutationAddBatchSubject, reqData, graphMutationTimeout, natsclient.DefaultRetryConfig())
	if err != nil {
		return fmt.Errorf("request %s: %w", graphMutationAddBatchSubject, err)
	}
	var resp graph.AddTriplesBatchResponse
	if err := json.Unmarshal(respData, &resp); err != nil {
		return fmt.Errorf("unmarshal batch response: %w", err)
	}
	// ADR-060: a whole-batch failure arrives as the classified err above. A
	// PARTIAL batch (some subjects committed) returns a success body with
	// FailedSubjects populated (per-subject errors in the map), handled here.
	if len(resp.FailedSubjects) > 0 {
		return fmt.Errorf("graph-ingest partial batch (written=%d, failed=%v)",
			resp.WrittenCount, resp.FailedSubjects)
	}
	return nil
}

// CreateEntityWithTriples births a graph entity (ENTITY_STATES) with the given
// typed-origin MessageType envelope, carrying triples atomically via
// graph.mutation.entity.create_with_triples. This is the correct verb for the
// FIRST write to a research-pipeline loop entity: graph-ingest enforces
// must-exist on triple.add / add_batch, so a bare batch to a never-created
// entity is rejected ("kv: key not found"), the whole batch lands written=0,
// and the chain stalls before R0 ever fires (gh#390). Subsequent per-stage
// stamps use AddTriplesBatch to append onto the now-existing entity.
//
// EntityExists is treated as idempotent-success: a freshly minted pipeline loop
// ID should never collide, but a redelivery / retry of the kickoff must not
// fail the chain. (The agentic-loop spawn path additionally reads back to
// confirm typed origin — see graph_writer.go — but a unique-per-dispatch rg_*
// id makes a foreign collision a non-concern here; treating exists as success
// keeps the best-effort, non-fatal kickoff contract.)
//
// PRECONDITION for the blind EntityExists path: the caller MUST mint a
// unique-per-dispatch entityID (the rg_* loop id is a fresh uuid8 per
// researchGraph invocation). A caller passing a STABLE/reused entityID (e.g. a
// config-name or loop+index id) must NOT bless EntityExists blindly — it has to
// read back and verify the typed origin like graph_writer.createEntityWithTriples
// does, or a foreign/auto-vivified entity colliding on that stable id would be
// silently laundered into a "born" pipeline loop.
func (p *natsTriplePublisher) CreateEntityWithTriples(ctx context.Context, entityID string, msgType message.Type, triples []message.Triple) error {
	req := graph.CreateEntityWithTriplesRequest{
		Entity: &graph.EntityState{
			ID:          entityID,
			MessageType: msgType,
			Triples:     triples,
		},
		Triples: triples,
	}
	reqData, err := json.Marshal(req)
	if err != nil {
		return fmt.Errorf("marshal create_with_triples request: %w", err)
	}
	// gh#93 Phase 2: Classified variant surfaces handler errors via err.
	if _, err := p.client.RequestWithRetryClassified(ctx, graphMutationCreateWithTriplesSubject, reqData, graphMutationTimeout, natsclient.DefaultRetryConfig()); err != nil {
		var ce *errs.ClassifiedError
		if errors.As(err, &ce) && ce.Code == graph.ErrorCodeEntityExists {
			return nil
		}
		return fmt.Errorf("request %s: %w", graphMutationCreateWithTriplesSubject, err)
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

// BirthLoopEntityWithTriples births the research-pipeline loop entity with a
// typed-origin MessageType envelope, carrying the kickoff triples atomically via
// create_with_triples. This is the FIRST stamp on a pipeline loop entity — the
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
	if err := pub.CreateEntityWithTriples(ctx, entityID, msgType, triples); err != nil {
		if logger != nil {
			logger.Warn("orchestration loop-birth failed; chain rules may not fire",
				"stage", stage, "loop_id", loopID, "entity_id", entityID, "triple_count", len(triples), "error", err)
		}
		return err
	}
	return nil
}

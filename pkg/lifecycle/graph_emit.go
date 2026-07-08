// Package lifecycle — graph-ingest emit wrapper.
//
// The Manager state-change operations (Create, Transition, UpdateFromOperator,
// Complete, Fail) all funnel through graphEmitter — a thin wrapper over
// natsclient.Request on the graph-ingest mutation subjects. Centralizing
// the request shape + response classification + sentinel translation keeps
// the per-operation code in manager.go focused on validation and projection
// rather than transport plumbing.
//
// Two methods:
//
//   - update: targets graph.mutation.entity.update_with_triples — for
//     existing entities, with optional CAS-on-condition via
//     UpdateEntityWithTriplesRequest.ExpectedRevision.
//   - create: targets graph.mutation.entity.create_with_triples — for
//     entities that don't yet exist. Atomic create-or-fail; returns
//     ErrAlreadyExists when the entity already exists. This is the
//     primitive that closes the Manager.Create concurrent-create race
//     (ADR-049 PR2 reviewer B2).
//
// Error classification reads the stable ErrorCode field on
// MutationResponse — added in this pre-tag follow-up to replace fragile
// substring matching (R1).
package lifecycle

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

// graphEmitter is the abstraction Manager uses to push entity +
// triple updates into graph-ingest. Two implementations live in
// this package: graphEmitterNATS (production) and graphEmitterFake
// (test). The Manager talks to the interface so tests can run
// without NATS.
type graphEmitter interface {
	// update sends an UpdateEntityWithTriplesRequest to graph-ingest
	// and waits for the response. Returns an error matching
	// errs.ErrRevisionMismatch (errors.Is) when the handler signals CAS
	// failed (Manager.Transition retry loop). Returns ErrEntityNotFound
	// when the handler reports the entity doesn't exist.
	update(ctx context.Context, req *graph.UpdateEntityWithTriplesRequest) (*graph.UpdateEntityWithTriplesResponse, error)

	// create sends a CreateEntityWithTriplesRequest to graph-ingest.
	// Atomic create-or-fail. Returns ErrAlreadyExists when the entity
	// is already present in ENTITY_STATES (the per-entity CAS race
	// surface for fresh-create).
	create(ctx context.Context, req *graph.CreateEntityWithTriplesRequest) (*graph.CreateEntityWithTriplesResponse, error)

	// delete sends a DeleteEntityRequest to graph-ingest, reclaiming an
	// entity from ENTITY_STATES (Manager.Despawn). The handler is
	// idempotent: deleting an already-absent entity returns
	// DeleteEntityResponse{Deleted: false} with no error, so there is no
	// not-found sentinel to translate.
	delete(ctx context.Context, req *graph.DeleteEntityRequest) (*graph.DeleteEntityResponse, error)
}

// graphEmitterNATS is the production graphEmitter — sends requests
// via natsclient.Request on the graph-ingest mutation subjects.
type graphEmitterNATS struct {
	client  *natsclient.Client
	timeout time.Duration
}

func newGraphEmitterNATS(client *natsclient.Client, timeout time.Duration) *graphEmitterNATS {
	if timeout <= 0 {
		timeout = 5 * time.Second
	}
	return &graphEmitterNATS{client: client, timeout: timeout}
}

// Subject names for the graph-ingest mutation handlers. Hardcoded here
// rather than imported from processor/graph-ingest to avoid the
// pkg/lifecycle → processor/graph-ingest dependency — the subjects are
// stable wire contracts.
const (
	graphSubjectUpdateWithTriples = "graph.mutation.entity.update_with_triples"
	graphSubjectCreateWithTriples = "graph.mutation.entity.create_with_triples"
	graphSubjectEntityDelete      = "graph.mutation.entity.delete"
)

// lifecycleEmitRetryConfig is the retry budget for Manager emit
// requests. natsclient.DefaultRetryConfig (~700ms total) is tuned for
// subscription propagation latency, but the lifecycle Manager faces a
// wider race surface (gh#170): graph-ingest may not have started yet
// when Manager.Create / Manager.Transition fires from a fast-boot
// path. ~13s total budget (10 retries × 200ms→2s backoff) covers
// Docker cold-start times where graph-ingest's SubscribeForRequests
// can be several seconds behind the lifecycle wire.
//
// Cumulative wait: 200+400+800+1600+2000+2000+2000+2000+2000+2000 ≈
// 13s, capped per-attempt by g.timeout (default 5s).
var lifecycleEmitRetryConfig = natsclient.RetryConfig{
	MaxRetries:        10,
	InitialBackoff:    200 * time.Millisecond,
	MaxBackoff:        2 * time.Second,
	BackoffMultiplier: 2.0,
}

// update marshals an UpdateEntityWithTriplesRequest, fires it as a
// NATS request/reply, and classifies the response via the ADR-060 typed
// error contract.
//
// Uses RequestWithRetryClassified with lifecycleEmitRetryConfig to
// survive the graph-ingest cold-start race (gh#170). Retry is safe here:
// graph-ingest's update_with_triples handler enforces CAS via
// ExpectedRevision, so a duplicate-delivery after a lost response
// surfaces as revision_mismatch, which Manager.Transition's outer CAS
// loop re-reads and re-validates. A handler error-reply is a successful
// round-trip (not a transport failure), so it is NOT retried — only
// cold-start "no responders" consumes the retry budget.
//
// ADR-060: a hard failure arrives as a non-nil *errs.ClassifiedError
// reconstructed from the wire headers (X-Error-Class + X-Error-Code), not
// an in-body Success=false. A revision_mismatch is the control-flow
// sentinel — propagated so Manager's errors.Is(err, errs.ErrRevisionMismatch)
// CAS loop fires; entity_not_found becomes ErrEntityNotFound; anything else
// is ErrEmitFailed. The per-consumer body-unmarshal + ErrorCode switch is
// gone (it collapses into the framework's ClassifyReply).
//
// No outer context.WithTimeout: the retry budget (~13s) controls
// total duration; each retry attempt is bounded internally by
// g.timeout. Capping with an outer 5s deadline would truncate the
// retry budget on cold-start paths — the very class gh#170 captures.
func (g *graphEmitterNATS) update(ctx context.Context, req *graph.UpdateEntityWithTriplesRequest) (*graph.UpdateEntityWithTriplesResponse, error) {
	body, err := json.Marshal(req)
	if err != nil {
		return nil, fmt.Errorf("%w: marshal request: %w", ErrEmitFailed, err)
	}

	respBody, err := g.client.RequestWithRetryClassified(ctx, graphSubjectUpdateWithTriples, body, g.timeout, lifecycleEmitRetryConfig)
	if err != nil {
		// Control-flow sentinel: propagate so Manager's CAS loop re-reads.
		if errors.Is(err, errs.ErrRevisionMismatch) {
			return nil, err
		}
		var ce *errs.ClassifiedError
		if errors.As(err, &ce) && ce.Code == graph.ErrorCodeEntityNotFound {
			return nil, fmt.Errorf("%w: %s", ErrEntityNotFound, err.Error())
		}
		return nil, fmt.Errorf("%w: NATS request to %s: %w", ErrEmitFailed, graphSubjectUpdateWithTriples, err)
	}

	var resp graph.UpdateEntityWithTriplesResponse
	if err := json.Unmarshal(respBody, &resp); err != nil {
		return nil, fmt.Errorf("%w: unmarshal response: %w", ErrEmitFailed, err)
	}
	return &resp, nil
}

// create marshals a CreateEntityWithTriplesRequest, fires it as a NATS
// request/reply, and classifies the response by ErrorCode. Atomic
// create-or-fail: ErrAlreadyExists when graph-ingest reports
// ErrorCodeEntityExists.
//
// Uses RequestWithRetry with lifecycleEmitRetryConfig to survive the
// graph-ingest cold-start race (gh#170). The retry path can in
// principle expose a false-positive ErrAlreadyExists if a first
// attempt succeeded but its response was lost in transit; on cold-
// start (the actual race the issue captures) the error is
// "no responders" before graph-ingest receives anything, so the retry
// is the correct atomic create. Callers that hit ErrAlreadyExists on
// what they expected to be a fresh create should re-read the entity
// rather than treating the error as fatal.
//
// No outer context.WithTimeout: see update() rationale.
func (g *graphEmitterNATS) create(ctx context.Context, req *graph.CreateEntityWithTriplesRequest) (*graph.CreateEntityWithTriplesResponse, error) {
	body, err := json.Marshal(req)
	if err != nil {
		return nil, fmt.Errorf("%w: marshal request: %w", ErrEmitFailed, err)
	}

	respBody, err := g.client.RequestWithRetryClassified(ctx, graphSubjectCreateWithTriples, body, g.timeout, lifecycleEmitRetryConfig)
	if err != nil {
		// ADR-060: entity_already_exists arrives as a classified error
		// (Code on the wire header), not an in-body Success=false.
		var ce *errs.ClassifiedError
		if errors.As(err, &ce) && ce.Code == graph.ErrorCodeEntityExists {
			return nil, fmt.Errorf("%w: %s", ErrAlreadyExists, err.Error())
		}
		return nil, fmt.Errorf("%w: NATS request to %s: %w", ErrEmitFailed, graphSubjectCreateWithTriples, err)
	}

	var resp graph.CreateEntityWithTriplesResponse
	if err := json.Unmarshal(respBody, &resp); err != nil {
		return nil, fmt.Errorf("%w: unmarshal response: %w", ErrEmitFailed, err)
	}
	return &resp, nil
}

// delete marshals a DeleteEntityRequest, fires it as a NATS
// request/reply, and returns the response. Targets
// graph.mutation.entity.delete — the reclaim path for Manager.Despawn.
//
// Uses RequestWithRetryClassified with lifecycleEmitRetryConfig to
// survive the graph-ingest cold-start race (gh#170); retry is safe
// because delete is idempotent — a duplicate delivery after a lost
// response re-deletes an already-absent entity, which the handler
// reports as DeleteEntityResponse{Deleted: false} with no error. There
// is therefore no not-found sentinel to translate (unlike create/update):
// any error here is a genuine transport/handler failure → ErrEmitFailed.
//
// No outer context.WithTimeout: see update() rationale.
func (g *graphEmitterNATS) delete(ctx context.Context, req *graph.DeleteEntityRequest) (*graph.DeleteEntityResponse, error) {
	body, err := json.Marshal(req)
	if err != nil {
		return nil, fmt.Errorf("%w: marshal request: %w", ErrEmitFailed, err)
	}

	respBody, err := g.client.RequestWithRetryClassified(ctx, graphSubjectEntityDelete, body, g.timeout, lifecycleEmitRetryConfig)
	if err != nil {
		return nil, fmt.Errorf("%w: NATS request to %s: %w", ErrEmitFailed, graphSubjectEntityDelete, err)
	}

	var resp graph.DeleteEntityResponse
	if err := json.Unmarshal(respBody, &resp); err != nil {
		return nil, fmt.Errorf("%w: unmarshal response: %w", ErrEmitFailed, err)
	}
	return &resp, nil
}

// triple is a small constructor that builds a message.Triple for the
// harness's audit + projection stamps. Kept short so call sites read
// cleanly in Manager.Transition where multiple deltas are appended
// in one expression.
func triple(subject, predicate string, object any) message.Triple {
	return message.Triple{
		Subject:    subject,
		Predicate:  predicate,
		Object:     object,
		Timestamp:  time.Now(),
		Confidence: 1.0,
	}
}

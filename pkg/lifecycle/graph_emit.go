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
)

// graphEmitter is the abstraction Manager uses to push entity +
// triple updates into graph-ingest. Two implementations live in
// this package: graphEmitterNATS (production) and graphEmitterFake
// (test). The Manager talks to the interface so tests can run
// without NATS.
type graphEmitter interface {
	// update sends an UpdateEntityWithTriplesRequest to graph-ingest
	// and waits for the response. Returns errEmitRevisionMismatch
	// when the handler signals CAS failed (Manager.Transition retry
	// loop). Returns ErrEntityNotFound when the handler reports the
	// entity doesn't exist.
	update(ctx context.Context, req *graph.UpdateEntityWithTriplesRequest) (*graph.UpdateEntityWithTriplesResponse, error)

	// create sends a CreateEntityWithTriplesRequest to graph-ingest.
	// Atomic create-or-fail. Returns ErrAlreadyExists when the entity
	// is already present in ENTITY_STATES (the per-entity CAS race
	// surface for fresh-create).
	create(ctx context.Context, req *graph.CreateEntityWithTriplesRequest) (*graph.CreateEntityWithTriplesResponse, error)
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
)

// errEmitRevisionMismatch is the package-internal sentinel Manager
// branches on for CAS retry. Translated from the handler's
// ErrorCodeRevisionMismatch response.
var errEmitRevisionMismatch = errors.New("lifecycle: emit revision mismatch (CAS conflict)")

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
// NATS request/reply, and classifies the response by ErrorCode.
//
// Uses RequestWithRetry with lifecycleEmitRetryConfig to survive the
// graph-ingest cold-start race (gh#170). Retry is safe here:
// graph-ingest's update_with_triples handler enforces CAS via
// ExpectedRevision, so a duplicate-delivery after a lost response
// surfaces as ErrorCodeRevisionMismatch, which Manager.Transition's
// outer CAS loop re-reads and re-validates.
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

	respBody, err := g.client.RequestWithRetry(ctx, graphSubjectUpdateWithTriples, body, g.timeout, lifecycleEmitRetryConfig)
	if err != nil {
		return nil, fmt.Errorf("%w: NATS request to %s: %w", ErrEmitFailed, graphSubjectUpdateWithTriples, err)
	}

	var resp graph.UpdateEntityWithTriplesResponse
	if err := json.Unmarshal(respBody, &resp); err != nil {
		return nil, fmt.Errorf("%w: unmarshal response: %w", ErrEmitFailed, err)
	}
	if !resp.Success {
		switch resp.ErrorCode {
		case graph.ErrorCodeRevisionMismatch:
			return &resp, errEmitRevisionMismatch
		case graph.ErrorCodeEntityNotFound:
			return &resp, fmt.Errorf("%w: %s", ErrEntityNotFound, resp.Error)
		}
		return &resp, fmt.Errorf("%w: graph-ingest rejected request (code=%q): %s",
			ErrEmitFailed, resp.ErrorCode, resp.Error)
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

	respBody, err := g.client.RequestWithRetry(ctx, graphSubjectCreateWithTriples, body, g.timeout, lifecycleEmitRetryConfig)
	if err != nil {
		return nil, fmt.Errorf("%w: NATS request to %s: %w", ErrEmitFailed, graphSubjectCreateWithTriples, err)
	}

	var resp graph.CreateEntityWithTriplesResponse
	if err := json.Unmarshal(respBody, &resp); err != nil {
		return nil, fmt.Errorf("%w: unmarshal response: %w", ErrEmitFailed, err)
	}
	if !resp.Success {
		if resp.ErrorCode == graph.ErrorCodeEntityExists {
			return &resp, fmt.Errorf("%w: %s", ErrAlreadyExists, resp.Error)
		}
		return &resp, fmt.Errorf("%w: graph-ingest rejected create (code=%q): %s",
			ErrEmitFailed, resp.ErrorCode, resp.Error)
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

// Package lifecycle — graph-ingest emit wrapper.
//
// The Manager state-change operations (Create, Transition, UpdateFromOperator,
// Complete, Fail) all funnel through graphEmitter.emit — a thin wrapper
// over natsclient.Request on the graph-ingest mutation subject. Centralizing
// the request shape + response classification + revision-mismatch sentinel
// translation keeps the per-operation code in manager.go focused on
// validation and projection rather than transport plumbing.
//
// Why an interface, not a free function: the test path injects a fake
// emitter that records the request without crossing NATS — Manager
// unit tests against in-memory graph state work without testcontainers.
package lifecycle

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
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
	// emit sends the request to graph-ingest and waits for the
	// MutationResponse. Returns errEmitRevisionMismatch when the
	// handler signals the CAS failed (used by Manager.Transition's
	// retry loop). Returns ErrEntityNotFound when the handler
	// reports the entity doesn't exist (translated from the
	// handler's "entity not found" error string).
	emit(ctx context.Context, req *graph.UpdateEntityWithTriplesRequest) (*graph.UpdateEntityWithTriplesResponse, error)
}

// graphEmitterNATS is the production graphEmitter — sends the request
// via natsclient.Request on the graph-ingest mutation subject.
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

// graphMutationSubject is the NATS subject the graph-ingest component
// subscribes to for entity-update-with-triples requests. Mirror of
// processor/graph-ingest/mutations.go SubjectEntityUpdateWithTriples.
// Hardcoded here (not imported) to keep pkg/lifecycle from depending
// on processor/graph-ingest — the subject is a stable wire contract.
const graphMutationSubject = "graph.mutation.entity.update_with_triples"

// errEmitRevisionMismatch is the package-internal sentinel Manager
// branches on for CAS retry. Translated from the handler's "revision
// mismatch" error string.
var errEmitRevisionMismatch = errors.New("lifecycle: emit revision mismatch (CAS conflict)")

// emit marshals the request, fires it as a NATS request/reply, and
// classifies the response. The handler error-payload convention is
// covered in [[feedback_natsclient_error_payload_convention]] —
// natsclient.Request returns a successful response containing
// `error: <message>`; the err return is only for transport errors.
func (g *graphEmitterNATS) emit(ctx context.Context, req *graph.UpdateEntityWithTriplesRequest) (*graph.UpdateEntityWithTriplesResponse, error) {
	body, err := json.Marshal(req)
	if err != nil {
		return nil, fmt.Errorf("%w: marshal request: %v", ErrEmitFailed, err)
	}

	ctx, cancel := context.WithTimeout(ctx, g.timeout)
	defer cancel()

	respBody, err := g.client.Request(ctx, graphMutationSubject, body, g.timeout)
	if err != nil {
		return nil, fmt.Errorf("%w: NATS request to %s: %v", ErrEmitFailed, graphMutationSubject, err)
	}

	var resp graph.UpdateEntityWithTriplesResponse
	if err := json.Unmarshal(respBody, &resp); err != nil {
		return nil, fmt.Errorf("%w: unmarshal response: %v", ErrEmitFailed, err)
	}
	if !resp.Success {
		// Classify the handler's error string into the sentinels
		// Manager branches on. The strings mirror the handler's
		// shape — keep in lockstep with processor/graph-ingest/mutations.go.
		if strings.Contains(resp.Error, "revision mismatch") {
			return &resp, errEmitRevisionMismatch
		}
		if strings.Contains(resp.Error, "entity not found") {
			return &resp, fmt.Errorf("%w: %s", ErrEntityNotFound, resp.Error)
		}
		return &resp, fmt.Errorf("%w: graph-ingest rejected request: %s",
			ErrEmitFailed, resp.Error)
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

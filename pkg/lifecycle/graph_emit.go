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
// path. 15s total backoff budget (10 retries × 200ms→2s) covers
// Docker cold-start times where graph-ingest's SubscribeForRequests
// can be several seconds behind the lifecycle wire.
//
// Cumulative wait: 200+400+800+1600+2000+2000+2000+2000+2000+2000 =
// 15s exactly, capped per-attempt by g.timeout (default 5s). (This
// comment said "≈13s" from its first commit; the addends were always
// these ten and they have always summed to 15s.)
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
// round-trip (not a transport failure), so it is NOT retried.
//
// Note the retry budget is consumed by ANY transport failure here, not
// only cold-start "no responders" — a per-attempt timeout re-sends too.
// That is deliberate on this lane and unsafe on create (gh#861): CAS
// makes a duplicate update self-correcting, and Transition's cold-start
// protection depends on retrying a sub-case that presents as a timeout.
// Do not "align" the two lanes without re-deriving that.
//
// ADR-060: a hard failure arrives as a non-nil *errs.ClassifiedError
// reconstructed from the wire headers (X-Error-Class + X-Error-Code), not
// an in-body Success=false. A revision_mismatch is the control-flow
// sentinel — propagated so Manager's errors.Is(err, errs.ErrRevisionMismatch)
// CAS loop fires; entity_not_found becomes ErrEntityNotFound; anything else
// is ErrEmitFailed. The per-consumer body-unmarshal + ErrorCode switch is
// gone (it collapses into the framework's ClassifyReply).
//
// No outer context.WithTimeout: the retry budget (15s) controls
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
	if err := validateMutationResponseEntity(resp.Entity); err != nil {
		return nil, fmt.Errorf("%w: validate update response: %w", ErrEmitFailed, err)
	}
	return &resp, nil
}

// create marshals a CreateEntityWithTriplesRequest, fires it as a NATS
// request/reply, and classifies the response by ErrorCode. Atomic
// create-or-fail: ErrAlreadyExists when graph-ingest reports
// ErrorCodeEntityExists.
//
// CONTRACT (gh#861): create is NOT idempotent, so it re-sends ONLY on
// failures that prove the request never reached a handler — see
// createSafeToResend for the exact set and why each qualifies. The
// motivating member is "no responders", where the server itself reports
// that nothing was subscribed; that is the class gh#170 captured
// (graph-ingest's SubscribeForRequests lagging a fast-boot lifecycle
// participant), and the 15s cold-start budget is preserved for it.
//
// EVERY OTHER transport failure is an UNKNOWN outcome and is returned as
// an error without re-sending. In particular a per-attempt timeout does
// not mean the create failed: the lifecycle per-attempt deadline is 5s
// while graph-ingest's handler deadline is 30s (natsclient.
// DefaultRequestHandlerTimeout), so the client can give up six times over while
// the handler is still executing the create. Re-sending there races a
// non-idempotent create against its own in-flight self, and the second
// delivery answers entity_already_exists for a birth this same request
// made — a request manufacturing a conflict with itself.
//
// Callers resolve an unknown outcome by reading authoritative state; the
// emitter must not guess on their behalf, and Manager.Create must not
// reconstruct ownership from stored state (openspec/specs/lifecycle:
// the answer is derived from the causal mutation response, never from a
// separate read that can observe another writer).
//
// update/delete deliberately keep the wider retry: update's CAS
// (ExpectedRevision) makes a duplicate delivery surface as
// revision_mismatch into Transition's re-read loop, and delete is
// idempotent at the handler. Only create can turn a re-send into a
// wrong answer.
//
// No outer context.WithTimeout: see update() rationale.
func (g *graphEmitterNATS) create(ctx context.Context, req *graph.CreateEntityWithTriplesRequest) (*graph.CreateEntityWithTriplesResponse, error) {
	body, err := json.Marshal(req)
	if err != nil {
		return nil, fmt.Errorf("%w: marshal request: %w", ErrEmitFailed, err)
	}

	respBody, err := g.requestCreateRetryingNoResponders(ctx, body)
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
	if err := validateMutationResponseEntity(resp.Entity); err != nil {
		return nil, fmt.Errorf("%w: validate create response: %w", ErrEmitFailed, err)
	}
	return &resp, nil
}

// createSafeToResend reports whether err permits re-sending a create.
//
// Only failures that prove the request never reached a handler qualify — all
// three below are decided BEFORE conn.RequestMsgWithContext is called, so no
// bytes left the process:
//
//   - no responders — the server reports nothing was subscribed (gh#170's class)
//   - circuit open — the client's own breaker refused to send
//   - not connected — there is no connection to send on
//
// The attempt index separates ENTRY state from a MID-LOOP transition, which is
// how requestMsgWithRetry behaved and what this loop must reproduce. That loop
// evaluated connection and breaker state ONCE before its first attempt and never
// again: an already-open breaker failed fast, while a breaker that opened during
// the loop was ridden through. Attempt 0 here performs the identical entry check
// (RequestClassified -> RequestWithHeaders re-runs both), so failing fast on
// attempt 0 and continuing afterwards is PARITY with the replaced loop, not a
// new policy — and it keeps an already-broken client from consuming a 15s budget
// per create.
//
// The mid-loop transition is not hypothetical (gh#861 review): circuitThreshold
// is 15 CONSECUTIVE failures on the SHARED client and one cold-start create
// burns up to 11, so two concurrent creates — processor/gated-dag/executor.go
// calls Manager.Create per node — trip the breaker partway through the second.
// Without this the loser aborts with ErrCircuitOpen and discards its remaining
// cold-start budget, which is exactly the gh#170 failure the budget exists to
// prevent. The breaker self-closes about a second later (recordFailure schedules
// testCircuit), well inside the budget.
func createSafeToResend(err error, attempt int) bool {
	if natsclient.IsNoResponders(err) {
		return true
	}
	if attempt == 0 {
		return false
	}
	return errors.Is(err, natsclient.ErrCircuitOpen) || errors.Is(err, natsclient.ErrNotConnected)
}

// requestCreateRetryingNoResponders runs the create request under
// lifecycleEmitRetryConfig's schedule but with a retry PREDICATE:
// createSafeToResend.
//
// It cannot be RequestWithRetryClassified — that loop's continue
// condition is "any non-nil error" (natsclient.requestMsgWithRetry),
// which is correct for the idempotent lanes and wrong for a create.
// RequestClassified is the single-attempt classified request, so each
// attempt still returns handler errors as *errs.ClassifiedError (the
// ADR-060 contract create() branches on) and still drives the client's
// circuit breaker.
//
// The backoff schedule intentionally reproduces requestMsgWithRetry's:
// InitialBackoff * BackoffMultiplier^attempt capped at MaxBackoff, 15s
// cumulative, so the gh#170 cold-start budget is unchanged for the classes
// that still retry.
func (g *graphEmitterNATS) requestCreateRetryingNoResponders(ctx context.Context, body []byte) ([]byte, error) {
	cfg := lifecycleEmitRetryConfig
	// One trace for the whole logical request, matching requestMsgWithRetry.
	// Without this each attempt would auto-generate its own trace ID inside
	// RequestClassified and a retried create would be unreconstructable from
	// logs — the exact path an operator most needs to follow.
	if _, ok := natsclient.TraceContextFromContext(ctx); !ok {
		ctx = natsclient.ContextWithTrace(ctx, natsclient.NewTraceContext())
	}
	var lastErr error
	for attempt := 0; attempt <= cfg.MaxRetries; attempt++ {
		respBody, err := g.client.RequestClassified(ctx, graphSubjectCreateWithTriples, body, g.timeout)
		if err == nil {
			return respBody, nil
		}
		// Anything that does not PROVE the request was never sent leaves the
		// outcome unknown — surface it, never re-send.
		if !createSafeToResend(err, attempt) {
			return nil, err
		}
		lastErr = err
		if attempt == cfg.MaxRetries {
			break
		}
		backoff := cfg.InitialBackoff
		for i := 0; i < attempt; i++ {
			backoff = time.Duration(float64(backoff) * cfg.BackoffMultiplier)
		}
		if backoff > cfg.MaxBackoff {
			backoff = cfg.MaxBackoff
		}
		select {
		case <-ctx.Done():
			return nil, errors.Join(lastErr, ctx.Err())
		case <-time.After(backoff):
		}
	}
	return nil, lastErr
}

func validateMutationResponseEntity(entity *graph.EntityState) error {
	if entity == nil {
		return nil
	}
	return graph.ValidateDecodedEntityState(entity)
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

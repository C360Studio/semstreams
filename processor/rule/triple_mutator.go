// Package rule provides triple mutation support via NATS request/response
package rule

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	gtypes "github.com/c360studio/semstreams/graph"
	"github.com/c360studio/semstreams/message"
	"github.com/c360studio/semstreams/natsclient"
)

// NATS subjects for graph mutations (must match processor/graph-ingest/mutations.go)
const (
	SubjectTripleAdd    = "graph.mutation.triple.add"
	SubjectTripleRemove = "graph.mutation.triple.remove"
	// SubjectEntityUpdateWithTriples is the atomic entity+triples update lane
	// used by replace_owned (ADR-056 Decision 3). Must match
	// graphingest.SubjectEntityUpdateWithTriples.
	SubjectEntityUpdateWithTriples = "graph.mutation.entity.update_with_triples"
	MutationTimeout                = 5 * time.Second
)

// tripleMutator implements TripleMutator using NATS request/response.
// It calls the graph processor's mutation handlers and tracks KV revisions
// against the originating ruleID to prevent per-rule feedback loops.
type tripleMutator struct {
	natsClient      *natsclient.Client
	revisionTracker revisionTracker
}

// revisionTracker is the interface for tracking KV revisions we generate.
// This is implemented by the Processor to break per-rule feedback loops.
type revisionTracker interface {
	trackRuleRevision(ruleID, entityID string, revision uint64)
}

// newTripleMutator creates a new TripleMutator that uses NATS request/response.
func newTripleMutator(natsClient *natsclient.Client, tracker revisionTracker) TripleMutator {
	return &tripleMutator{
		natsClient:      natsClient,
		revisionTracker: tracker,
	}
}

// AddTriple adds a triple via NATS request/response and returns the KV revision.
// The ruleID identifies the originating rule so the revision can be tracked
// against that rule for per-rule feedback loop prevention. Pass an empty
// ruleID for ad-hoc mutations that should not be tracked.
func (m *tripleMutator) AddTriple(ctx context.Context, ruleID string, triple message.Triple) (uint64, error) {
	if m.natsClient == nil {
		return 0, fmt.Errorf("NATS client not available")
	}

	// Build request
	req := gtypes.AddTripleRequest{
		Triple: triple,
	}
	reqData, err := json.Marshal(req)
	if err != nil {
		return 0, fmt.Errorf("marshal request: %w", err)
	}

	// RequestWithRetry handles transient "no responders" errors when
	// graph-gateway is restarting or its subscription hasn't yet
	// propagated. Without retry, rule-driven add_triple actions
	// silently fail during graph-gateway startup races. The mutation
	// is idempotent (graph is a set of triples; same triple twice =
	// same state), so retry is safe.
	respData, err := m.natsClient.RequestWithRetry(ctx, SubjectTripleAdd, reqData, MutationTimeout, natsclient.DefaultRetryConfig())
	if err != nil {
		return 0, fmt.Errorf("NATS request failed: %w", err)
	}

	// Parse response
	var resp gtypes.AddTripleResponse
	if err := json.Unmarshal(respData, &resp); err != nil {
		return 0, fmt.Errorf("unmarshal response: %w", err)
	}

	if !resp.Success {
		return 0, fmt.Errorf("mutation failed: %s", resp.Error)
	}

	// Track the revision to prevent this rule from re-triggering on its own write.
	if m.revisionTracker != nil && resp.KVRevision > 0 && ruleID != "" {
		m.revisionTracker.trackRuleRevision(ruleID, triple.Subject, resp.KVRevision)
	}

	return resp.KVRevision, nil
}

// RemoveTriple removes a triple via NATS request/response and returns the KV revision.
// See AddTriple for the meaning of ruleID.
func (m *tripleMutator) RemoveTriple(ctx context.Context, ruleID, subject, predicate string) (uint64, error) {
	if m.natsClient == nil {
		return 0, fmt.Errorf("NATS client not available")
	}

	// Build request
	req := gtypes.RemoveTripleRequest{
		Subject:   subject,
		Predicate: predicate,
	}
	reqData, err := json.Marshal(req)
	if err != nil {
		return 0, fmt.Errorf("marshal request: %w", err)
	}

	// RequestWithRetry: same rationale as AddTriple. Removing
	// already-removed is a no-op success on the responder side, so
	// duplicate retries converge to the same state.
	respData, err := m.natsClient.RequestWithRetry(ctx, SubjectTripleRemove, reqData, MutationTimeout, natsclient.DefaultRetryConfig())
	if err != nil {
		return 0, fmt.Errorf("NATS request failed: %w", err)
	}

	// Parse response
	var resp gtypes.RemoveTripleResponse
	if err := json.Unmarshal(respData, &resp); err != nil {
		return 0, fmt.Errorf("unmarshal response: %w", err)
	}

	if !resp.Success {
		return 0, fmt.Errorf("mutation failed: %s", resp.Error)
	}

	if m.revisionTracker != nil && resp.KVRevision > 0 && ruleID != "" {
		m.revisionTracker.trackRuleRevision(ruleID, subject, resp.KVRevision)
	}

	return resp.KVRevision, nil
}

// ReplaceOwned atomically replaces (or clears) the owned value of a
// single-valued predicate on entityID via ONE update_with_triples mutation
// (ADR-056 Decision 3). The predicate is named in RemoveTriples (dropping any
// prior value) and the new value(s) carried in objects (AddTriples); an empty
// objects slice clears the predicate. This is atomic where update_triple's
// remove-then-add is not — a watcher never sees the predicate absent between
// the two operations.
//
// ExpectedRevision is left zero: this is owned-current-state reconciliation,
// NOT a CAS transition. The ownerToken parameter carries the pre-composed
// lease token "<owner>#<incarnation>", or the EMPTY string when the incarnation
// is unavailable (the two-state contract — an empty token tells the graph-ingest
// lease check to SKIP; it is never a bare "<owner>"). It is stamped directly
// onto the request's OwnerToken field for the lease check (a later increment).
// Token composition is the caller's responsibility (ActionExecutor.executeReplaceOwned).
//
// Unlike AddTriple/RemoveTriple, this checks resp.Success (NOT the body-prefix
// convention) and surfaces the handler's ErrorCode on failure. On a
// non-existent entity the handler returns ErrorCodeEntityNotFound; that is
// returned verbatim as an error (no auto-vivify).
func (m *tripleMutator) ReplaceOwned(ctx context.Context, ruleID, ownerToken, entityID, predicate string, objects []message.Triple) (uint64, error) {
	if m.natsClient == nil {
		return 0, fmt.Errorf("NATS client not available")
	}

	// Build the atomic update: RemoveTriples drops the prior value of the
	// predicate (it runs before the AddTriples merge on the handler side),
	// AddTriples carries the new value(s). ExpectedRevision stays zero —
	// replace_owned is NEVER a CAS write (ADR-056 Decision 3).
	req := gtypes.UpdateEntityWithTriplesRequest{
		Entity:        &gtypes.EntityState{ID: entityID},
		RemoveTriples: []string{predicate},
		AddTriples:    objects,
		OwnerToken:    ownerToken,
	}
	reqData, err := json.Marshal(req)
	if err != nil {
		return 0, fmt.Errorf("marshal request: %w", err)
	}

	// RequestWithRetry: same transient-no-responders rationale as AddTriple.
	// The replace is idempotent (replace-by-predicate converges to the same
	// single-valued state on a duplicate delivery), so retry is safe.
	respData, err := m.natsClient.RequestWithRetry(ctx, SubjectEntityUpdateWithTriples, reqData, MutationTimeout, natsclient.DefaultRetryConfig())
	if err != nil {
		return 0, fmt.Errorf("NATS request failed: %w", err)
	}

	var resp gtypes.UpdateEntityWithTriplesResponse
	if err := json.Unmarshal(respData, &resp); err != nil {
		return 0, fmt.Errorf("unmarshal response: %w", err)
	}

	if !resp.Success {
		// Surface the machine-readable error code so callers (and operators)
		// can distinguish must-exist failures (entity_not_found) from
		// transport/internal errors without substring-matching.
		if resp.ErrorCode != "" {
			return 0, fmt.Errorf("replace_owned mutation failed [%s]: %s", resp.ErrorCode, resp.Error)
		}
		return 0, fmt.Errorf("replace_owned mutation failed: %s", resp.Error)
	}

	if m.revisionTracker != nil && resp.KVRevision > 0 && ruleID != "" {
		m.revisionTracker.trackRuleRevision(ruleID, entityID, resp.KVRevision)
	}

	return resp.KVRevision, nil
}

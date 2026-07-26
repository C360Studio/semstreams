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
	MutationTimeout     = 5 * time.Second
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

	// RequestWithRetryClassified handles transient "no responders" errors when
	// graph-gateway is restarting or its subscription hasn't yet
	// propagated. Without retry, rule-driven add_triple actions
	// silently fail during graph-gateway startup races. The mutation
	// is idempotent (graph is a set of triples; same triple twice =
	// same state), so retry is safe.
	// gh#93 Phase 2: Classified variant surfaces handler errors via err.
	respData, err := m.natsClient.RequestWithRetryClassified(ctx, SubjectTripleAdd, reqData, MutationTimeout, natsclient.DefaultRetryConfig())
	if err != nil {
		return 0, fmt.Errorf("NATS request failed: %w", err)
	}

	// Parse the success response for the KV revision. ADR-060: a handler
	// failure now arrives as the classified err above, so the legacy
	// !resp.Success second check is gone.
	var resp gtypes.AddTripleResponse
	if err := json.Unmarshal(respData, &resp); err != nil {
		return 0, fmt.Errorf("unmarshal response: %w", err)
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

	// RequestWithRetryClassified: same rationale as AddTriple. Removing
	// already-removed is a no-op success on the responder side, so
	// duplicate retries converge to the same state.
	// gh#93 Phase 2: Classified variant surfaces handler errors via err.
	respData, err := m.natsClient.RequestWithRetryClassified(ctx, SubjectTripleRemove, reqData, MutationTimeout, natsclient.DefaultRetryConfig())
	if err != nil {
		return 0, fmt.Errorf("NATS request failed: %w", err)
	}

	// Parse the success response for the KV revision. ADR-060: handler
	// failures arrive as the classified err above.
	var resp gtypes.RemoveTripleResponse
	if err := json.Unmarshal(respData, &resp); err != nil {
		return 0, fmt.Errorf("unmarshal response: %w", err)
	}

	if m.revisionTracker != nil && resp.KVRevision > 0 && ruleID != "" {
		m.revisionTracker.trackRuleRevision(ruleID, subject, resp.KVRevision)
	}

	return resp.KVRevision, nil
}

// Package agentrun — NATS-backed production adapters.
//
// NATSLoopTripleReader satisfies LoopTripleReader by reading entity triples
// directly from the ENTITY_STATES KV bucket via a natsclient.Client.
//
// NATSTriplePublisher satisfies TriplePublisher by writing triples through
// the graph.mutation.triple.add NATS request/response surface (same path as
// the rule engine's TripleMutator). Neither adapter tracks KV revisions for
// feedback-loop prevention — they are event-subscriber side effects, not
// rule-engine transitions.
//
// Both adapters are wired in cmd/semstreams and cmd/e2e-semstreams at startup.
package agentrun

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/c360studio/semstreams/graph"
	"github.com/c360studio/semstreams/message"
	"github.com/c360studio/semstreams/natsclient"
	agvocab "github.com/c360studio/semstreams/vocabulary/agentic"
	"github.com/nats-io/nats.go/jetstream"
)

// natsTripleAddSubject is the NATS request subject for single-triple adds.
// Must match processor/rule/triple_mutator.go:SubjectTripleAdd.
const natsTripleAddSubject = "graph.mutation.triple.add"

// natsTripleMutationTimeout is the round-trip budget for triple mutations.
// Matches the rule engine's MutationTimeout.
const natsTripleMutationTimeout = 5 * time.Second

// NATSTriplePublisher satisfies agentrun.TriplePublisher via the
// graph.mutation.triple.add NATS surface. Used by MilestoneSubscriber
// to stamp milestone triples onto run entities via graph-ingest.
//
// The ruleID argument is accepted to satisfy the interface but is NOT
// forwarded — milestone writes are not rule-engine transitions and need
// no feedback-loop revision tracking.
type NATSTriplePublisher struct {
	client *natsclient.Client
}

// NewNATSTriplePublisher builds a TriplePublisher backed by the shared
// graph.mutation.triple.add NATS surface.
func NewNATSTriplePublisher(client *natsclient.Client) *NATSTriplePublisher {
	return &NATSTriplePublisher{client: client}
}

// AddTriple writes a triple through graph-ingest and returns the KV revision.
func (p *NATSTriplePublisher) AddTriple(ctx context.Context, _ string, triple message.Triple) (uint64, error) {
	req := graph.AddTripleRequest{Triple: triple}
	reqData, err := json.Marshal(req)
	if err != nil {
		return 0, fmt.Errorf("agentrun: NATSTriplePublisher: marshal: %w", err)
	}
	// gh#93 Phase 2: RequestWithRetryClassified surfaces handler errors
	// (legacy "error: " body prefix) as err rather than silently
	// mis-decoding them as success JSON. Migration from RequestWithRetry.
	respData, err := p.client.RequestWithRetryClassified(ctx, natsTripleAddSubject, reqData, natsTripleMutationTimeout, natsclient.DefaultRetryConfig())
	if err != nil {
		return 0, fmt.Errorf("agentrun: NATSTriplePublisher: NATS request: %w", err)
	}
	// ADR-060: a handler failure arrives as the classified err above; decode the
	// success body only for the KV revision.
	var resp graph.AddTripleResponse
	if err := json.Unmarshal(respData, &resp); err != nil {
		return 0, fmt.Errorf("agentrun: NATSTriplePublisher: unmarshal response: %w", err)
	}
	return resp.KVRevision, nil
}

// NATSLoopTripleReader implements LoopTripleReader backed by the ENTITY_STATES
// KV bucket. Reads are lazy-bucket: the bucket is opened on first use and
// cached for subsequent calls.
//
// Satisfies the LoopTripleReader interface required by ResolveRun and
// MilestoneSubscriber.
type NATSLoopTripleReader struct {
	client *natsclient.Client
}

// NewNATSLoopTripleReader constructs a LoopTripleReader backed by a natsclient.
// The client must be connected; the ENTITY_STATES bucket is opened lazily on
// first read.
func NewNATSLoopTripleReader(client *natsclient.Client) *NATSLoopTripleReader {
	return &NATSLoopTripleReader{client: client}
}

// GetLoopRunID reads the agent.loop.run triple from the given loop entity ID.
// Returns ("", false, nil) when the triple is absent (not an error).
// Returns ("", false, err) on NATS or decode failures.
func (r *NATSLoopTripleReader) GetLoopRunID(ctx context.Context, loopEntityID string) (string, bool, error) {
	return r.getStringTriple(ctx, loopEntityID, agvocab.LoopRun)
}

// GetLoopParentEntityID reads the agent.loop.parent triple from the given
// loop entity ID. Returns ("", false, nil) when absent.
func (r *NATSLoopTripleReader) GetLoopParentEntityID(ctx context.Context, loopEntityID string) (string, bool, error) {
	return r.getStringTriple(ctx, loopEntityID, agvocab.LoopParent)
}

// getStringTriple is the shared read path for both triple-reading methods.
// It opens the ENTITY_STATES bucket, fetches the entity JSON, and extracts
// the named predicate's value as a string.
func (r *NATSLoopTripleReader) getStringTriple(ctx context.Context, entityID, predicate string) (string, bool, error) {
	bucket, err := r.client.GetKeyValueBucket(ctx, graph.BucketEntityStates)
	if err != nil {
		return "", false, fmt.Errorf("agentrun: NATSLoopTripleReader: open bucket: %w", err)
	}

	entry, err := bucket.Get(ctx, entityID)
	if err != nil {
		if errors.Is(err, jetstream.ErrKeyNotFound) {
			// Entity not yet in graph — triple is absent.
			return "", false, nil
		}
		return "", false, fmt.Errorf("agentrun: NATSLoopTripleReader: KV get %q: %w", entityID, err)
	}

	var state graph.EntityState
	if err := graph.UnmarshalEntityState(entry.Value(), &state); err != nil {
		return "", false, fmt.Errorf("agentrun: NATSLoopTripleReader: unmarshal entity %q: %w", entityID, err)
	}

	val, ok := state.GetPropertyValue(predicate)
	if !ok {
		return "", false, nil
	}
	s, ok := val.(string)
	if !ok {
		return "", false, fmt.Errorf("agentrun: NATSLoopTripleReader: predicate %q on entity %q has non-string value %T", predicate, entityID, val)
	}
	return s, true, nil
}

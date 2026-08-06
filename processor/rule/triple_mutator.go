package rule

import (
	"context"
	"fmt"
	"time"

	"github.com/c360studio/semstreams/graph"
	"github.com/c360studio/semstreams/internal/graphmutation"
	"github.com/c360studio/semstreams/message"
	"github.com/c360studio/semstreams/natsclient"
)

// MutationTimeout bounds each individual canonical graph request.
const MutationTimeout = 5 * time.Second

type tripleMutator struct {
	mutations       *graphmutation.Client
	reader          graph.ExactEntityReader
	revisionTracker revisionTracker
}

type revisionTracker interface {
	trackRuleRevision(ruleID, entityID string, revision uint64)
}

func newTripleMutator(client *natsclient.Client, tracker revisionTracker) TripleMutator {
	mutations, _ := graphmutation.NewClient(client, MutationTimeout)
	return &tripleMutator{
		mutations:       mutations,
		reader:          graph.NewExactEntityReader(client, MutationTimeout),
		revisionTracker: tracker,
	}
}

func (m *tripleMutator) AddTriple(
	ctx context.Context,
	ruleID string,
	triple message.Triple,
) (uint64, error) {
	if m == nil || m.mutations == nil {
		return 0, fmt.Errorf("graph mutation client not available")
	}
	response, err := m.mutations.Append(ctx, graph.AppendTriplesRequest{Triples: []message.Triple{triple}})
	if err != nil {
		return 0, fmt.Errorf("append triple: %w", err)
	}
	result := response.Results[0]
	if result.Outcome == graph.MutationFailed {
		return 0, fmt.Errorf("append triple for %s failed: %s/%s", triple.Subject, result.Error.Class, result.Error.Code)
	}
	switch result.Outcome {
	case graph.MutationApplied:
		if m.revisionTracker != nil && ruleID != "" {
			m.revisionTracker.trackRuleRevision(ruleID, triple.Subject, result.KVRevision)
		}
		return result.KVRevision, nil
	case graph.MutationUnchanged:
		return result.KVRevision, nil
	case graph.MutationEntityNotFound:
		return 0, fmt.Errorf("entity not found: %s", triple.Subject)
	default:
		return 0, fmt.Errorf("unexpected append outcome %q", result.Outcome)
	}
}

func (m *tripleMutator) RemoveTriple(
	ctx context.Context,
	ruleID string,
	entityID string,
	predicate string,
) (uint64, error) {
	if m == nil || m.mutations == nil || m.reader == nil {
		return 0, fmt.Errorf("graph mutation client not available")
	}
	exact, err := m.reader.ReadExactEntity(ctx, entityID)
	if err != nil {
		return 0, fmt.Errorf("read entity before reconcile: %w", err)
	}
	response, err := m.mutations.Reconcile(ctx, graph.ReconcilePredicatesRequest{
		EntityID: entityID, ExpectedRevision: exact.KVRevision,
		Predicates: []string{predicate}, Desired: nil,
	})
	if err != nil {
		return 0, fmt.Errorf("remove predicate %s from %s: %w", predicate, entityID, err)
	}
	if response.Outcome == graph.MutationApplied && m.revisionTracker != nil && ruleID != "" {
		m.revisionTracker.trackRuleRevision(ruleID, entityID, response.KVRevision)
	}
	return response.KVRevision, nil
}

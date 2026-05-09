// Package graphingest mutation handlers for triple operations via NATS request/reply.
package graphingest

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/c360studio/semstreams/graph"
)

const (
	// SubjectTripleAdd is the NATS subject for add triple requests
	SubjectTripleAdd = "graph.mutation.triple.add"
	// SubjectTripleAddBatch is the NATS subject for batched add-triple
	// requests. Optimised for tools that emit many triples in one call
	// (write_todos per ADR-036): triples sharing a Subject collapse to
	// one CAS round-trip on that entity.
	SubjectTripleAddBatch = "graph.mutation.triple.add_batch"
	// SubjectTripleRemove is the NATS subject for remove triple requests
	SubjectTripleRemove = "graph.mutation.triple.remove"
)

// setupMutationHandlers registers NATS request handlers for triple mutations.
// These handlers allow the rule processor (and other components) to modify
// entity triples via NATS request/reply.
func (c *Component) setupMutationHandlers(ctx context.Context) error {
	sub, err := c.natsClient.SubscribeForRequests(ctx, SubjectTripleAdd, c.handleTripleAdd)
	if err != nil {
		return fmt.Errorf("subscribe triple add: %w", err)
	}
	c.subscriptions = append(c.subscriptions, sub)

	sub, err = c.natsClient.SubscribeForRequests(ctx, SubjectTripleAddBatch, c.handleTripleAddBatch)
	if err != nil {
		return fmt.Errorf("subscribe triple add_batch: %w", err)
	}
	c.subscriptions = append(c.subscriptions, sub)

	sub, err = c.natsClient.SubscribeForRequests(ctx, SubjectTripleRemove, c.handleTripleRemove)
	if err != nil {
		return fmt.Errorf("subscribe triple remove: %w", err)
	}
	c.subscriptions = append(c.subscriptions, sub)

	c.logger.Info("mutation handlers registered",
		"subjects", []string{SubjectTripleAdd, SubjectTripleAddBatch, SubjectTripleRemove})
	return nil
}

// handleTripleAdd handles add triple requests from rule processor and other components
func (c *Component) handleTripleAdd(ctx context.Context, data []byte) ([]byte, error) {
	var req graph.AddTripleRequest
	if err := json.Unmarshal(data, &req); err != nil {
		return json.Marshal(graph.AddTripleResponse{
			MutationResponse: graph.MutationResponse{
				Success:   false,
				Error:     fmt.Sprintf("invalid request: %v", err),
				Timestamp: time.Now().UnixNano(),
			},
		})
	}

	// AddTriple uses triple.Subject as entity ID
	err := c.AddTriple(ctx, req.Triple)
	if err != nil {
		return json.Marshal(graph.AddTripleResponse{
			MutationResponse: graph.MutationResponse{
				Success:   false,
				Error:     err.Error(),
				Timestamp: time.Now().UnixNano(),
			},
		})
	}

	// Get revision after successful mutation for feedback loop prevention
	var kvRevision uint64
	if entry, err := c.entityBucket.Get(ctx, req.Triple.Subject); err == nil {
		kvRevision = entry.Revision
	}

	return json.Marshal(graph.AddTripleResponse{
		MutationResponse: graph.MutationResponse{
			Success:    true,
			Timestamp:  time.Now().UnixNano(),
			KVRevision: kvRevision,
		},
		Triple: &req.Triple,
	})
}

// handleTripleAddBatch handles batched add-triple requests. The
// implementation groups triples by Subject and issues one CAS per
// entity, so a tool emitting many triples on the same loop entity
// (write_todos, ADR-036) sees one round-trip to graph-ingest instead
// of N. On partial failure across multiple entities, the response
// surfaces FailedSubjects so the caller can decide whether to retry
// the failed subset.
func (c *Component) handleTripleAddBatch(ctx context.Context, data []byte) ([]byte, error) {
	var req graph.AddTriplesBatchRequest
	if err := json.Unmarshal(data, &req); err != nil {
		return json.Marshal(graph.AddTriplesBatchResponse{
			MutationResponse: graph.MutationResponse{
				Success:   false,
				Error:     fmt.Sprintf("invalid request: %v", err),
				Timestamp: time.Now().UnixNano(),
			},
		})
	}

	if len(req.Triples) == 0 {
		return json.Marshal(graph.AddTriplesBatchResponse{
			MutationResponse: graph.MutationResponse{
				Success:   true,
				Timestamp: time.Now().UnixNano(),
			},
			WrittenCount: 0,
		})
	}

	written, failed, err := c.AddTriples(ctx, req.Triples)
	if err != nil && len(failed) == 0 {
		// Pre-CAS validation failure (e.g. empty subject/predicate).
		// Whole batch rejected.
		return json.Marshal(graph.AddTriplesBatchResponse{
			MutationResponse: graph.MutationResponse{
				Success:   false,
				Error:     err.Error(),
				Timestamp: time.Now().UnixNano(),
			},
		})
	}

	resp := graph.AddTriplesBatchResponse{
		MutationResponse: graph.MutationResponse{
			Success:   len(failed) == 0,
			Timestamp: time.Now().UnixNano(),
		},
		WrittenCount:   written,
		FailedSubjects: failed,
	}
	if err != nil {
		resp.Error = err.Error()
	}
	return json.Marshal(resp)
}

// handleTripleRemove handles remove triple requests from rule processor and other components
func (c *Component) handleTripleRemove(ctx context.Context, data []byte) ([]byte, error) {
	var req graph.RemoveTripleRequest
	if err := json.Unmarshal(data, &req); err != nil {
		return json.Marshal(graph.RemoveTripleResponse{
			MutationResponse: graph.MutationResponse{
				Success:   false,
				Error:     fmt.Sprintf("invalid request: %v", err),
				Timestamp: time.Now().UnixNano(),
			},
		})
	}

	// RemoveTriple takes subject (entity ID) and predicate
	err := c.RemoveTriple(ctx, req.Subject, req.Predicate)
	if err != nil {
		return json.Marshal(graph.RemoveTripleResponse{
			MutationResponse: graph.MutationResponse{
				Success:   false,
				Error:     err.Error(),
				Timestamp: time.Now().UnixNano(),
			},
		})
	}

	// Get revision after successful mutation for feedback loop prevention
	var kvRevision uint64
	if entry, err := c.entityBucket.Get(ctx, req.Subject); err == nil {
		kvRevision = entry.Revision
	}

	return json.Marshal(graph.RemoveTripleResponse{
		MutationResponse: graph.MutationResponse{
			Success:    true,
			Timestamp:  time.Now().UnixNano(),
			KVRevision: kvRevision,
		},
		Removed: true,
	})
}

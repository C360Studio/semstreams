// Package graphclustering provides embedding-based similarity search for anomaly detection.
package graphclustering

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/c360studio/semstreams/graph"
	"github.com/c360studio/semstreams/graph/clustering"
	"github.com/c360studio/semstreams/graph/inference"
	"github.com/c360studio/semstreams/natsclient"
	"github.com/c360studio/semstreams/pkg/errs"
)

const (
	// similarQuerySubject is the NATS subject for similarity queries
	similarQuerySubject = "graph.embedding.query.similar"

	// similarQueryTimeout is the timeout for similarity query requests
	similarQueryTimeout = 30 * time.Second
)

// querySimilarityFinder implements inference.SimilarityFinder using NATS request/reply.
// It delegates similarity search to graph-embedding via the query path, avoiding
// duplication of brute-force similarity logic.
type querySimilarityFinder struct {
	natsClient *natsclient.Client
	logger     *slog.Logger
}

// Verify interface compliance
var _ inference.SimilarityFinder = (*querySimilarityFinder)(nil)

// newQuerySimilarityFinder creates a new query-based similarity finder.
func newQuerySimilarityFinder(natsClient *natsclient.Client, logger *slog.Logger) *querySimilarityFinder {
	if logger == nil {
		logger = slog.Default()
	}
	return &querySimilarityFinder{
		natsClient: natsClient,
		logger:     logger,
	}
}

// similarRequest matches graphembedding.SimilarRequest
type similarRequest struct {
	EntityID string `json:"entity_id"`
	Limit    int    `json:"limit"`
}

// similarResponse matches graphembedding.SimilarResponse
type similarResponse struct {
	EntityID string          `json:"entity_id"`
	Similar  []similarEntity `json:"similar"`
	Duration string          `json:"duration"`
}

// similarEntity matches graphembedding.SimilarEntity
type similarEntity struct {
	EntityID   string  `json:"entity_id"`
	Similarity float64 `json:"similarity"`
}

// FindSimilar returns entity IDs semantically similar to the given entity.
// Uses NATS request/reply to delegate to graph-embedding's similarity search.
//
// This is anomaly detection's (SemanticGapDetector's) consumer. Its error policy
// is UNCHANGED (B2 §5.2): the finder is optional, so a transport/handler failure
// on the RPC is treated as "no similar neighbors" (blanket fail-open) and the
// detector keeps walking other entities. The clustering-edge consumer needs the
// opposite — to tell a cold-embedding transient from a genuine empty — so it
// calls findSimilarClassified instead (below).
func (f *querySimilarityFinder) FindSimilar(
	ctx context.Context,
	entityID string,
	threshold float64,
	limit int,
) ([]inference.SimilarityResult, error) {
	if f.natsClient == nil {
		return nil, nil
	}

	reqData, err := marshalSimilarRequest(entityID, limit)
	if err != nil {
		return nil, err
	}

	// gh#93 Phase 2: RequestClassified unifies transport + handler errors behind
	// one classified return. Fail-open on any error so the caller
	// (SemanticGapDetector) keeps walking other entities. Most common handler
	// error here is "source entity has no embedding yet" (aggregation/group
	// entities never projected through the embedder).
	respData, err := f.natsClient.RequestClassified(ctx, similarQuerySubject, reqData, similarQueryTimeout)
	if err != nil {
		f.logger.Debug("similarity query failed (transport or handler)",
			slog.String("entity_id", entityID),
			slog.Any("error", err))
		return nil, nil
	}

	results, duration, err := parseSimilarResponse(respData, threshold)
	if err != nil {
		return nil, err
	}

	f.logger.Debug("similarity query complete",
		slog.String("entity_id", entityID),
		slog.Float64("threshold", threshold),
		slog.Int("results", len(results)),
		slog.String("duration", duration))

	return results, nil
}

// findSimilarClassified is the clustering-edge consumer's error-PRESERVING
// variant of FindSimilar: identical RPC, but a RequestClassified failure is
// returned with its classification intact rather than swallowed into a blanket
// fail-open. The semanticFinderAdapter inspects that classification to
// distinguish a cold-embedding ErrorCodeIndexNotReady transient ("could not ask
// this tick") from a genuine empty result ("asked, no semantic neighbors") — the
// #618 distinction the shared blanket fail-open could not make (B2 §5.1). It
// lives here, at the edge-consumer path, so it does NOT change FindSimilar's
// policy for anomaly detection (B2 §5.2).
func (f *querySimilarityFinder) findSimilarClassified(
	ctx context.Context,
	entityID string,
	threshold float64,
	limit int,
) ([]inference.SimilarityResult, error) {
	if f.natsClient == nil {
		return nil, nil
	}

	reqData, err := marshalSimilarRequest(entityID, limit)
	if err != nil {
		return nil, err
	}

	respData, err := f.natsClient.RequestClassified(ctx, similarQuerySubject, reqData, similarQueryTimeout)
	if err != nil {
		// PRESERVE the classification (transient/code) for the readiness-aware
		// wrapper — never swallow it here.
		return nil, err
	}

	results, _, err := parseSimilarResponse(respData, threshold)
	return results, err
}

// marshalSimilarRequest builds the wire request for a similarity query. Shared by
// both consumers so the two error policies differ ONLY in how they treat the RPC
// result, not in how the request is built.
func marshalSimilarRequest(entityID string, limit int) ([]byte, error) {
	req := similarRequest{EntityID: entityID, Limit: limit}
	data, err := json.Marshal(req)
	if err != nil {
		return nil, errs.WrapInvalid(err, "querySimilarityFinder", "FindSimilar", "marshal request")
	}
	return data, nil
}

// parseSimilarResponse decodes a similarity reply and filters it to the
// threshold, returning the handler's reported duration alongside for the
// completion log. Shared by both consumers.
func parseSimilarResponse(respData []byte, threshold float64) ([]inference.SimilarityResult, string, error) {
	var resp similarResponse
	if err := json.Unmarshal(respData, &resp); err != nil {
		return nil, "", errs.WrapInvalid(err, "querySimilarityFinder", "FindSimilar", "unmarshal response")
	}
	var results []inference.SimilarityResult
	for _, s := range resp.Similar {
		if s.Similarity >= threshold {
			results = append(results, inference.SimilarityResult{
				EntityID:   s.EntityID,
				Similarity: s.Similarity,
			})
		}
	}
	return results, resp.Duration, nil
}

// classifiedSimilarityFinder is the error-PRESERVING similarity call the
// clustering-edge consumer needs (to tell a cold-embedding transient from a
// genuine empty). *querySimilarityFinder satisfies it via findSimilarClassified;
// anomaly detection keeps using the swallowing FindSimilar. Declared as a narrow
// interface so the adapter's readiness classification is unit-testable with a
// fake.
type classifiedSimilarityFinder interface {
	findSimilarClassified(ctx context.Context, entityID string, threshold float64, limit int) ([]inference.SimilarityResult, error)
}

// semanticFinderAdapter bridges the component's classified similarity finder to
// the clustering package's narrow SemanticNeighborFinder (which returns bare
// entity IDs and does not import graph/inference). It reuses the SAME
// graph.embedding.query.similar path the anomaly detector already uses — the
// SemanticEdgeProvider issues no second similarity RPC (B2 §1.2).
//
// It is the readiness-aware wrapper of B2 §5.1, living at the clustering-edge
// call site (NOT inside querySimilarityFinder, so anomaly detection's policy is
// untouched — §5.2). A classified ErrorCodeIndexNotReady transient maps to the
// clustering package's ErrSemanticIndexNotReady sentinel so the mutual-kNN build
// ABORTS rather than latching a semantically-blind partition; any other error
// (including a genuine per-entity miss) is a fail-open empty — "asked, got
// nothing."
type semanticFinderAdapter struct {
	finder classifiedSimilarityFinder
}

// Verify the adapter satisfies the clustering-side contract.
var _ clustering.SemanticNeighborFinder = semanticFinderAdapter{}

// SimilarNeighbors returns the entity IDs of the finder's similarity results,
// applying the readiness-aware error policy (B2 §5.1).
func (a semanticFinderAdapter) SimilarNeighbors(ctx context.Context, entityID string, threshold float64, limit int) ([]string, error) {
	results, err := a.finder.findSimilarClassified(ctx, entityID, threshold, limit)
	if err != nil {
		if isProducerWideEmbeddingFault(err) {
			// A PRODUCER-WIDE signal: the embedding index is cold/bootstrapping
			// (transient index_not_ready) OR has issued a sticky reset/fatal fault
			// (graph_state_reset_required, the FATAL class generally). Map onto the
			// package-neutral sentinel so ensureCache ABORTS the whole build rather than
			// latching a hollow cache from an empty per-entity answer (Codex P1#3): a
			// producer-wide fault answers EVERY entity empty, and a build that latched
			// cacheInitialized=true off that would stay permanently hollow across a
			// graph-embedding restart. Aborting retries a later cycle instead.
			return nil, fmt.Errorf("%w: %v", clustering.ErrSemanticIndexNotReady, err)
		}
		// A RECOGNIZED per-entity condition — a genuine handler miss (this entity has
		// no embedding yet: an aggregation/group entity never projected through the
		// embedder), or a single transient transport blip — is "asked, got nothing."
		// Fail-open to empty at THIS site only, so one absent embedding never degrades
		// the whole cycle to structural-only; the shared FindSimilar's blanket policy
		// is unchanged.
		return nil, nil
	}
	ids := make([]string, 0, len(results))
	for _, r := range results {
		ids = append(ids, r.EntityID)
	}
	return ids, nil
}

// isEmbeddingIndexNotReady reports whether err is the classified,
// still-bootstrapping embedding-index transient. It checks the CLASSIFICATION
// (errs.IsTransient) and the stable machine code (graph.ErrorCodeIndexNotReady),
// never message text — the graph-index-readiness classification discipline
// (B2 §5.1). graph-embedding's ensureBootstrapReady stamps exactly this code on a
// mid-bootstrap or watcher-unavailable similarity query.
func isEmbeddingIndexNotReady(err error) bool {
	if !errs.IsTransient(err) {
		return false
	}
	var ce *errs.ClassifiedError
	return errors.As(err, &ce) && ce.Code == graph.ErrorCodeIndexNotReady
}

// isProducerWideEmbeddingFault reports whether a similarity-query error is a
// PRODUCER-WIDE fault that must ABORT the mutual-kNN build (retry a later cycle)
// rather than fail-open to an empty per-entity answer (Codex P1#3). Three disjoint
// shapes qualify, all STRUCTURAL — the errs CLASS, stable machine CODE, or the
// no-responders sentinel — never message text (the graph-index-readiness
// classification discipline, B2 §5.1):
//
//   - a TRANSIENT index_not_ready (the index is cold / still bootstrapping);
//   - NO-RESPONDERS — graph-embedding stopped answering entirely (e.g. crashed
//     mid-build after passing the §4 gate): a producer-wide outage, not a per-
//     entity miss; and
//   - a FATAL *classified* error, which graph-embedding's ensureBootstrapReady
//     stamps as graph_state_reset_required on a sticky producer-wide reset. This
//     disjunct is gated on the CLASSIFIED type so it is a pure class check —
//     errs.IsFatal alone would fall through to a message-TEXT scan for a raw /
//     unclassified error, which this deliberately avoids.
//
// A per-entity miss (entity not found / embedding not ready) is classified
// ErrorInvalid, and a lone transient transport blip is neither fatal nor
// no-responders, so both are left to the caller's per-entity fail-open — a single
// absent embedding never collapses the whole cycle to structural-only.
func isProducerWideEmbeddingFault(err error) bool {
	if isEmbeddingIndexNotReady(err) || natsclient.IsNoResponders(err) {
		return true
	}
	var ce *errs.ClassifiedError
	return errors.As(err, &ce) && ce.Class == errs.ErrorFatal
}

// initQuerySimilarityFinder initializes the query-based similarity finder.
// Called during Start() when EnableAnomalyDetection is true, and by
// wrapSemanticEdges when enable_semantic_edges is true (the finder is
// config-agnostic; both consumers share this construction).
// Returns nil if NATS client is not available.
func (c *Component) initQuerySimilarityFinder() *querySimilarityFinder {
	if c.natsClient == nil {
		c.logger.Warn("NATS client not available, semantic gap detection disabled")
		return nil
	}

	finder := newQuerySimilarityFinder(c.natsClient, c.logger)
	c.logger.Info("query similarity finder initialized",
		slog.String("subject", similarQuerySubject))

	return finder
}

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
// It is the readiness-aware wrapper of B2 §5.1/§7.3, living at the clustering-edge
// call site (NOT inside querySimilarityFinder, so anomaly detection's policy is
// untouched — §5.2). It maps the embedding service's outcomes onto the clustering
// package's sentinels in a CLOSED precedence (classified by class/code only, never
// message text):
//
//   - a PRODUCER-WIDE fault (isProducerWideEmbeddingFault: a classified
//     ErrorCodeIndexNotReady transient — the whole index is cold — OR no-responders
//     OR a FATAL classified error) -> ErrSemanticIndexNotReady, so the mutual-kNN
//     refresh ABORTS the whole build rather than latching a semantically-blind
//     partition from an empty per-entity answer (Codex P1#3);
//   - a RECOGNIZED per-entity miss (graph.ErrorCodeEmbeddingUnavailable: this
//     source entity has no embedding record / no generated embedding / an empty
//     vector) -> a fail-open empty ("asked, got nothing");
//   - EVERYTHING ELSE — a per-entity transient (timeout on this one query), a LOCAL
//     parse/decode error, or any OTHER/unknown classified error -> the
//     ErrSemanticQueryTransient sentinel, so the refresh COUNTS it toward the
//     coverage-threshold abort and re-queries the entity next cycle instead of
//     caching a hollow empty for it (#662 / B2 §7.3 / Codex P1#2).
//
// The last rung is the P1#2 correction: a genuine miss must be recognized
// POSITIVELY by its stable code, because a malformed parseSimilarResponse reply is
// an uncoded Invalid indistinguishable from a miss by class alone — treating it as
// a miss would cache a hollow empty across the corpus and still report
// semantic_edges_applied=1 (recreating #662). Surfacing the transient/unknown class
// separately (rather than swallowing every non-producer-wide error into a bare
// empty, as the pre-#662 adapter did) is what lets the refresh tell "could not ask
// this entity" from "this entity has no semantic neighbors."
type semanticFinderAdapter struct {
	finder classifiedSimilarityFinder
}

// Verify the adapter satisfies the clustering-side contract.
var _ clustering.SemanticNeighborFinder = semanticFinderAdapter{}

// SimilarNeighbors returns the entity IDs of the finder's similarity results,
// applying the readiness-aware error policy (B2 §5.1 / §7.3, Codex P1#2). The
// three precedence rungs are CLOSED and ordered — classified by class/code only,
// never message text:
//
//  1. producer-wide fault -> ErrSemanticIndexNotReady (abort the whole build);
//  2. a RECOGNIZED per-entity embedding miss (ErrorCodeEmbeddingUnavailable) ->
//     fail-open empty ("asked, got nothing");
//  3. EVERYTHING ELSE — a per-entity transient, a LOCAL parse/decode error, or any
//     OTHER/unknown classified error -> ErrSemanticQueryTransient (countable).
//
// Rung 3 is the P1#2 fix: pre-fix, every non-producer-wide/non-transient error
// (including a malformed parseSimilarResponse reply, which is an uncoded Invalid)
// fell through to a bare empty, so a version-skewed/malformed reply across the
// corpus latched a hollow cache and could still report semantic_edges_applied=1
// (recreating #662). Recognizing the miss POSITIVELY by code — and treating
// parse/unknown as countable — makes a corpus-wide malformed reply ABORT on the
// coverage threshold and recover next cycle instead of caching hollow. A local
// decode error must NEVER masquerade as "no neighbors."
func (a semanticFinderAdapter) SimilarNeighbors(ctx context.Context, entityID string, threshold float64, limit int) ([]string, error) {
	results, err := a.finder.findSimilarClassified(ctx, entityID, threshold, limit)
	if err != nil {
		if isProducerWideEmbeddingFault(err) {
			// A PRODUCER-WIDE signal: the embedding index is cold/bootstrapping
			// (transient index_not_ready), has stopped answering entirely (no-responders),
			// OR has issued a sticky reset/fatal fault (graph_state_reset_required, the
			// FATAL class generally). Map onto the package-neutral sentinel so refreshCache
			// ABORTS the whole build rather than latching a hollow cache from an empty
			// per-entity answer (Codex P1#3): a producer-wide fault answers EVERY entity
			// empty, and a build that committed off that would stay permanently hollow
			// across a graph-embedding restart. Aborting retries a later cycle instead.
			return nil, fmt.Errorf("%w: %v", clustering.ErrSemanticIndexNotReady, err)
		}
		if isRecognizedEmbeddingMiss(err) {
			// A RECOGNIZED per-entity miss (ErrorCodeEmbeddingUnavailable: this source
			// entity has no embedding record / no generated embedding / an empty vector —
			// an aggregation/group entity never projected through the embedder) is "asked,
			// got nothing." Fail-open to empty at THIS site only, so one absent embedding
			// never degrades the whole cycle to structural-only; the shared FindSimilar's
			// blanket policy is unchanged. ONLY this stable code fails open — a parse or
			// unknown error is NOT a miss and falls through to the countable rung below.
			return nil, nil
		}
		// EVERYTHING ELSE is countable (Codex P1#2): a per-entity transient (timeout /
		// no-responders on this one query), a LOCAL parseSimilarResponse decode error
		// (an uncoded Invalid — a version-skewed/malformed reply), or any OTHER/unknown
		// classified error. Surface the transient sentinel so refreshCache counts it
		// toward the coverage-threshold abort and re-queries the entity next cycle,
		// instead of caching a hollow empty that would present as a genuine "no semantic
		// neighbors" (#662 / B2 §7.3). A malformed reply across the corpus therefore
		// ABORTS rather than latching a semantically-blind cache.
		return nil, fmt.Errorf("%w: %v", clustering.ErrSemanticQueryTransient, err)
	}
	ids := make([]string, 0, len(results))
	for _, r := range results {
		ids = append(ids, r.EntityID)
	}
	return ids, nil
}

// isRecognizedEmbeddingMiss reports whether err is a RECOGNIZED per-entity
// no-embedding miss — the ONLY error class the adapter fails open to empty for
// (Codex P1#2). It keys on the stable machine code graph-embedding stamps on a
// per-entity miss (ErrorCodeEmbeddingUnavailable), never message text or the bare
// Invalid class: a LOCAL parse error is ALSO Invalid but uncoded, so a class-only
// check would let a malformed reply masquerade as "no neighbors" and cache a hollow
// empty (the #662 shape). A missing/unknown code is deliberately NOT a miss — it
// falls through to the countable transient rung.
func isRecognizedEmbeddingMiss(err error) bool {
	var ce *errs.ClassifiedError
	return errors.As(err, &ce) && ce.Code == graph.ErrorCodeEmbeddingUnavailable
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

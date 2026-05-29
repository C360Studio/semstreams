// Package graphclustering provides embedding-based similarity search for anomaly detection.
package graphclustering

import (
	"context"
	"encoding/json"
	"log/slog"
	"time"

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
func (f *querySimilarityFinder) FindSimilar(
	ctx context.Context,
	entityID string,
	threshold float64,
	limit int,
) ([]inference.SimilarityResult, error) {
	if f.natsClient == nil {
		return nil, nil
	}

	// Build request
	req := similarRequest{
		EntityID: entityID,
		Limit:    limit,
	}
	reqData, err := json.Marshal(req)
	if err != nil {
		return nil, errs.WrapInvalid(err, "querySimilarityFinder", "FindSimilar", "marshal request")
	}

	// gh#93 Phase 2: RequestClassified unifies transport + handler
	// errors behind one classified return. The similarity finder is
	// optional — fail-open on any error so the caller
	// (SemanticGapDetector) keeps walking other entities. Most common
	// handler error here is "source entity has no embedding yet"
	// (aggregation/group entities never projected through the
	// embedder).
	respData, err := f.natsClient.RequestClassified(ctx, similarQuerySubject, reqData, similarQueryTimeout)
	if err != nil {
		f.logger.Debug("similarity query failed (transport or handler)",
			slog.String("entity_id", entityID),
			slog.Any("error", err))
		return nil, nil
	}

	// Parse response
	var resp similarResponse
	if err := json.Unmarshal(respData, &resp); err != nil {
		return nil, errs.WrapInvalid(err, "querySimilarityFinder", "FindSimilar", "unmarshal response")
	}

	// Filter by threshold and convert to inference.SimilarityResult
	var results []inference.SimilarityResult
	for _, s := range resp.Similar {
		if s.Similarity >= threshold {
			results = append(results, inference.SimilarityResult{
				EntityID:   s.EntityID,
				Similarity: s.Similarity,
			})
		}
	}

	f.logger.Debug("similarity query complete",
		slog.String("entity_id", entityID),
		slog.Float64("threshold", threshold),
		slog.Int("results", len(results)),
		slog.String("duration", resp.Duration))

	return results, nil
}

// initQuerySimilarityFinder initializes the query-based similarity finder.
// Called during Start() when EnableAnomalyDetection is true.
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

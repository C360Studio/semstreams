// Package graphquery PathRAG algorithm implementation
package graphquery

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/c360studio/semstreams/graph"
	"github.com/c360studio/semstreams/pkg/errs"
)

// Direction constants for path traversal
const (
	DirectionOutgoing = "outgoing" // Follow edges from entity to targets (default)
	DirectionIncoming = "incoming" // Follow edges from sources to entity
	DirectionBoth     = "both"     // Follow edges in both directions
)

// PathSearchRequest defines the request schema for path search queries
type PathSearchRequest struct {
	StartEntity     string   `json:"start_entity"`
	MaxDepth        int      `json:"max_depth"`
	MaxNodes        int      `json:"max_nodes"`
	IncludeSiblings bool     `json:"include_siblings"`
	Direction       string   `json:"direction,omitempty"`  // "outgoing" (default), "incoming", "both"
	Predicates      []string `json:"predicates,omitempty"` // Filter to specific predicates (empty = all)
	Timeout         string   `json:"timeout,omitempty"`    // Request timeout e.g. "5s" (0 = default)
	MaxPaths        int      `json:"max_paths,omitempty"`  // Limit number of paths returned (0 = unlimited)
}

// PathSearchResponse defines the response for path search
type PathSearchResponse struct {
	Entities  []PathEntity `json:"entities"`
	Paths     [][]PathStep `json:"paths"` // Each path is a sequence of steps from start to entity
	Truncated bool         `json:"truncated"`
}

// PathEntity represents a discovered entity with relevance score
type PathEntity struct {
	ID    string  `json:"id"`
	Type  string  `json:"type"`
	Score float64 `json:"score"`
}

// PathStep represents a single edge traversal
type PathStep struct {
	From      string `json:"from"`
	Predicate string `json:"predicate"`
	To        string `json:"to"`
}

// parentInfo tracks how we reached an entity during BFS
type parentInfo struct {
	parentID  string
	predicate string
}

// RelationshipEntry represents an outgoing relationship from graph-index
type RelationshipEntry struct {
	ToEntityID string `json:"to_entity_id"`
	Predicate  string `json:"predicate"`
}

// PathSearcher executes PathRAG traversal with proper path tracking
type PathSearcher struct {
	nats        natsRequester
	timeout     time.Duration
	maxDepth    int
	decayFactor float64
	logger      *slog.Logger
}

// NewPathSearcher creates a new PathSearcher instance
func NewPathSearcher(nats natsRequester, timeout time.Duration, maxDepth int, logger *slog.Logger) *PathSearcher {
	return &PathSearcher{
		nats:        nats,
		timeout:     timeout,
		maxDepth:    maxDepth,
		decayFactor: 0.8, // Score decreases by 20% per hop
		logger:      logger,
	}
}

// Search performs BFS traversal with path tracking
func (p *PathSearcher) Search(ctx context.Context, req PathSearchRequest) (*PathSearchResponse, error) {
	if req.StartEntity == "" {
		// Classified at source so the handleGlobalSearch wire emits
		// X-Error-Class: invalid instead of defaulting to transient.
		// errs.Classified preserves the historic body shape.
		return nil, errs.Classified(errs.ErrorInvalid, errors.New("invalid request: empty start_entity"))
	}

	// Apply request-level timeout if specified
	if req.Timeout != "" {
		if timeout, err := time.ParseDuration(req.Timeout); err == nil && timeout > 0 {
			var cancel context.CancelFunc
			ctx, cancel = context.WithTimeout(ctx, timeout)
			defer cancel()
		}
	}

	// Apply limits
	maxDepth, maxNodes := p.applyLimits(req.MaxDepth, req.MaxNodes)

	// Verify start entity exists
	if err := p.verifyEntityExists(ctx, req.StartEntity); err != nil {
		return nil, err
	}

	// Build predicate filter map for efficient lookup
	predicateFilter := make(map[string]bool)
	for _, pred := range req.Predicates {
		predicateFilter[pred] = true
	}

	// Normalize direction (default to outgoing)
	direction := req.Direction
	if direction == "" {
		direction = DirectionOutgoing
	}

	// BFS with parent tracking for path reconstruction
	type queueItem struct {
		entityID string
		depth    int
	}

	visited := make(map[string]bool)
	parentMap := make(map[string]parentInfo)
	visited[req.StartEntity] = true

	queue := []queueItem{{entityID: req.StartEntity, depth: 0}}
	entities := []PathEntity{{ID: req.StartEntity, Type: "entity", Score: 1.0}}
	paths := [][]PathStep{{}} // Start entity has empty path
	nodesDiscovered := 0
	pathsCollected := 1 // Start entity counts as first path

	for len(queue) > 0 && nodesDiscovered < maxNodes {
		if ctx.Err() != nil {
			return nil, fmt.Errorf("context cancelled: %w", ctx.Err())
		}

		current := queue[0]
		queue = queue[1:]

		if current.depth >= maxDepth {
			continue
		}

		// Use direction-aware relationship fetching with predicate filtering
		rels := p.getRelationships(ctx, current.entityID, direction, predicateFilter)

		for _, rel := range rels {
			if nodesDiscovered >= maxNodes {
				break
			}

			// Check max_paths limit if specified
			if req.MaxPaths > 0 && pathsCollected >= req.MaxPaths {
				return &PathSearchResponse{Entities: entities, Paths: paths, Truncated: true}, nil
			}

			if !visited[rel.ToEntityID] {
				visited[rel.ToEntityID] = true
				nodesDiscovered++
				pathsCollected++

				parentMap[rel.ToEntityID] = parentInfo{parentID: current.entityID, predicate: rel.Predicate}

				newDepth := current.depth + 1
				score := p.calculateDecayScore(newDepth)

				entities = append(entities, PathEntity{ID: rel.ToEntityID, Type: "entity", Score: score})
				paths = append(paths, p.reconstructPath(rel.ToEntityID, req.StartEntity, parentMap))
				queue = append(queue, queueItem{entityID: rel.ToEntityID, depth: newDepth})
			}
		}
	}

	return &PathSearchResponse{Entities: entities, Paths: paths, Truncated: nodesDiscovered >= maxNodes}, nil
}

// applyLimits applies max depth and max nodes limits.
func (p *PathSearcher) applyLimits(reqDepth, reqNodes int) (maxDepth, maxNodes int) {
	maxDepth = reqDepth
	if maxDepth == 0 || maxDepth > p.maxDepth {
		maxDepth = p.maxDepth
	}
	maxNodes = reqNodes
	if maxNodes == 0 {
		maxNodes = 100
	}
	return maxDepth, maxNodes
}

// verifyEntityExists checks if an entity exists in the graph.
//
// Protocol note: graph-ingest's handleQueryEntityNATS returns a Go
// error on missing-entity, but natsclient.SubscribeForRequests wraps
// gh#93 Phase 2: RequestClassified unifies transport and handler
// errors. Handler-side "not found" classifies as Invalid and surfaces
// as *errs.ClassifiedError; transport failures (no responders,
// context.DeadlineExceeded) arrive as raw errors. We distinguish so
// callers know whether the entity is genuinely absent vs we couldn't
// ask. Previous implementation only branched on the Go err return —
// which never fired on the legacy protocol path — so a missing entity
// silently passed verification. Same fix as ADR-036 Stage 4 H1 in
// processor/agentic-loop/todos.go.
//
// Three-way branch on the classified error class (gh#163):
//   - Invalid   → definitive negative (entity absent / bad request);
//     return verbatim so callers can errs.IsInvalid(err) to detect
//     not-found without treating it as a server failure.
//   - Transient / Fatal → server/availability failure; we COULD NOT
//     check; wrap with a server-error qualifier so BFS callers do not
//     silently prune the path as "entity absent."
//   - raw transport error → same server-error wrapper (no responders,
//     timeout, cancelled context).
func (p *PathSearcher) verifyEntityExists(ctx context.Context, entityID string) error {
	entityReq := map[string]string{"id": entityID}
	entityReqData, _ := json.Marshal(entityReq)
	_, err := p.nats.RequestClassified(ctx, "graph.ingest.query.entity", entityReqData, p.timeout)
	if err != nil {
		// IsInvalid: handler answered definitively — entity absent or
		// bad request. Propagate verbatim; the producer's clean
		// message (e.g. "not found: <id>") surfaces directly to the
		// GraphQL layer. BFS callers check errs.IsInvalid(err) to
		// distinguish this from a server failure.
		if errs.IsInvalid(err) {
			return err
		}
		// IsTransient / IsFatal, or raw transport error (no
		// responders, timeout): we could NOT determine entity
		// existence. Wrap with a server-error qualifier so BFS
		// callers surface an availability error rather than silently
		// pruning the traversal path as if the entity were absent.
		return fmt.Errorf("query entity server error: %w", err)
	}
	return nil
}

// getOutgoingRelationships fetches outgoing relationships for an entity.
func (p *PathSearcher) getOutgoingRelationships(ctx context.Context, entityID string) []RelationshipEntry {
	relsReq := map[string]string{"entity_id": entityID}
	relsReqData, _ := json.Marshal(relsReq)

	relsResponse, err := p.nats.RequestClassified(ctx, "graph.index.query.outgoing", relsReqData, p.timeout)
	if err != nil {
		return nil
	}

	var envelope graph.OutgoingQueryResponse
	if err := json.Unmarshal(relsResponse, &envelope); err != nil {
		return nil
	}

	rels := make([]RelationshipEntry, len(envelope.Data.Relationships))
	for i, r := range envelope.Data.Relationships {
		rels[i] = RelationshipEntry{ToEntityID: r.ToEntityID, Predicate: r.Predicate}
	}
	return rels
}

// getIncomingRelationships fetches incoming relationships for an entity.
// Returns relationships where this entity is the target (sources pointing to us).
func (p *PathSearcher) getIncomingRelationships(ctx context.Context, entityID string) []RelationshipEntry {
	relsReq := map[string]string{"entity_id": entityID}
	relsReqData, _ := json.Marshal(relsReq)

	relsResponse, err := p.nats.RequestClassified(ctx, "graph.index.query.incoming", relsReqData, p.timeout)
	if err != nil {
		return nil
	}

	var envelope graph.IncomingQueryResponse
	if err := json.Unmarshal(relsResponse, &envelope); err != nil {
		return nil
	}

	// Convert incoming entries to relationship entries
	// For incoming, the "neighbor" is the FromEntityID (the entity pointing to us)
	rels := make([]RelationshipEntry, len(envelope.Data.Relationships))
	for i, r := range envelope.Data.Relationships {
		rels[i] = RelationshipEntry{ToEntityID: r.FromEntityID, Predicate: r.Predicate}
	}
	return rels
}

// getRelationships fetches relationships based on direction and predicate filters.
func (p *PathSearcher) getRelationships(ctx context.Context, entityID, direction string, predicateFilter map[string]bool) []RelationshipEntry {
	var rels []RelationshipEntry

	switch direction {
	case DirectionIncoming:
		rels = p.getIncomingRelationships(ctx, entityID)
	case DirectionBoth:
		// Merge outgoing and incoming, deduplicating by target+predicate
		outgoing := p.getOutgoingRelationships(ctx, entityID)
		incoming := p.getIncomingRelationships(ctx, entityID)

		seen := make(map[string]bool)
		rels = make([]RelationshipEntry, 0, len(outgoing)+len(incoming))

		for _, r := range outgoing {
			key := r.ToEntityID + ":" + r.Predicate
			if !seen[key] {
				seen[key] = true
				rels = append(rels, r)
			}
		}
		for _, r := range incoming {
			key := r.ToEntityID + ":" + r.Predicate
			if !seen[key] {
				seen[key] = true
				rels = append(rels, r)
			}
		}
	default: // DirectionOutgoing or empty
		rels = p.getOutgoingRelationships(ctx, entityID)
	}

	// Apply predicate filter if specified
	if len(predicateFilter) > 0 {
		filtered := make([]RelationshipEntry, 0, len(rels))
		for _, r := range rels {
			if predicateFilter[r.Predicate] {
				filtered = append(filtered, r)
			}
		}
		return filtered
	}

	return rels
}

// calculateDecayScore calculates the score based on depth using decay factor.
func (p *PathSearcher) calculateDecayScore(depth int) float64 {
	score := 1.0
	for i := 0; i < depth; i++ {
		score *= p.decayFactor
	}
	return score
}

// reconstructPath builds the full path from start to target entity
func (p *PathSearcher) reconstructPath(target, start string, parentMap map[string]parentInfo) []PathStep {
	if target == start {
		return []PathStep{} // Start entity has empty path
	}

	// Walk backwards from target to start
	path := []PathStep{}
	current := target

	for current != start {
		info, ok := parentMap[current]
		if !ok {
			// Should not happen if BFS is correct
			p.logger.Warn("missing parent info during path reconstruction", "entity", current)
			break
		}

		path = append(path, PathStep{
			From:      info.parentID,
			Predicate: info.predicate,
			To:        current,
		})

		current = info.parentID
	}

	// Reverse path to get start → target order
	for i, j := 0, len(path)-1; i < j; i, j = i+1, j-1 {
		path[i], path[j] = path[j], path[i]
	}

	return path
}

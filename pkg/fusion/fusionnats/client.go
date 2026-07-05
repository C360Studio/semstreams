package fusionnats

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/c360studio/semstreams/graph"
	"github.com/c360studio/semstreams/pkg/errs"
	"github.com/c360studio/semstreams/pkg/fusion"
)

// Public graph query subjects the retrieval client maps onto. These are the
// stable external surface (graph-query / graph-index passthroughs), NOT the
// internal graph.ingest.*/graph.embedding.* subjects, so a standalone fusion
// service reaches them through the same boundary as any other consumer.
const (
	subjectStatus        = "graph.index.query.status"
	subjectByName        = "graph.query.byName"
	subjectPrefix        = "graph.query.prefix"
	subjectSemantic      = "graph.query.semantic"
	subjectEntity        = "graph.query.entity"
	subjectBatch         = "graph.query.batch"
	subjectRelationships = "graph.query.relationships"
)

// defaultTimeout is used when New is given a non-positive timeout. Matches the
// 5s the existing graph/query client uses for index lookups.
const defaultTimeout = 5 * time.Second

// requester is the minimal NATS surface the retrieval client needs: classified
// request/reply. *natsclient.Client satisfies it. Kept as a local interface so
// the client unit-tests against a fake without a live NATS, and so this package
// does not have to import natsclient for its production path.
type requester interface {
	RequestClassified(ctx context.Context, subject string, data []byte, timeout time.Duration) ([]byte, error)
}

// Client is the production NATS implementation of fusion.RetrievalClient. It is
// stateless beyond the request transport and timeout, so it is safe to share
// across goroutines.
type Client struct {
	nats    requester
	timeout time.Duration
}

// Compile-time assertion that Client satisfies the engine's retrieval surface.
var _ fusion.RetrievalClient = (*Client)(nil)

// New builds a retrieval client over the given NATS requester. A non-positive
// timeout falls back to defaultTimeout.
func New(nats requester, timeout time.Duration) *Client {
	if timeout <= 0 {
		timeout = defaultTimeout
	}
	return &Client{nats: nats, timeout: timeout}
}

// Status reports graph readiness for the honesty envelope (graph.index.query.status).
func (c *Client) Status(ctx context.Context) (fusion.IndexStatus, error) {
	// The handler ignores the body; send an empty object for convention parity
	// with the other index query callers.
	raw, err := c.request(ctx, subjectStatus, struct{}{})
	if err != nil {
		return fusion.IndexStatus{}, err
	}
	// Decode straight into the target: graph.IndexStatusResponse and
	// fusion.IndexStatus are field-identical by contract (ADR-066 §5), so a direct
	// unmarshal keeps them changing together — a hand-copied remap silently drops
	// any field added to the wire (as it did for IndexedRevision/Lag before the
	// round-trip test below), which reads as a false-caught-up (Lag==0) downstream.
	var resp fusion.IndexStatus
	if err := json.Unmarshal(raw, &resp); err != nil {
		return fusion.IndexStatus{}, fmt.Errorf("fusionnats: decode status: %w", err)
	}
	return resp, nil
}

// Resolve maps a query to seed entity IDs by mode, most relevant first. The mode
// selects the subject; an unknown mode is an error rather than a silent default
// (ResolveMode is an open string enum).
func (c *Client) Resolve(ctx context.Context, q fusion.ResolveQuery) ([]string, error) {
	switch q.Mode {
	case fusion.ResolveModeSymbol:
		return c.resolveByName(ctx, q.Query, q.Limit)
	case fusion.ResolveModePrefix:
		return c.resolvePrefix(ctx, q.Query, q.Limit)
	case fusion.ResolveModeNL:
		return c.resolveSemantic(ctx, q.Query, q.Scope, q.Limit)
	default:
		return nil, fmt.Errorf("fusionnats: unsupported resolve mode %q", q.Mode)
	}
}

// resolveByName resolves a symbol to ranked entity IDs via graph.query.byName.
func (c *Client) resolveByName(ctx context.Context, query string, limit int) ([]string, error) {
	matches, err := c.byNameMatches(ctx, query, limit)
	if err != nil {
		return nil, err
	}
	ids := make([]string, 0, len(matches))
	for _, m := range matches {
		ids = append(ids, m.EntityID)
	}
	return ids, nil
}

// resolvePrefix resolves an ID prefix to entity IDs via graph.query.prefix. Only
// the first page is taken — resolve seeds are bounded by limit, not exhaustive.
func (c *Client) resolvePrefix(ctx context.Context, query string, limit int) ([]string, error) {
	raw, err := c.request(ctx, subjectPrefix, graph.PrefixQueryRequest{Prefix: query, Limit: limit})
	if err != nil {
		return nil, err
	}
	var resp graph.PrefixQueryResponse
	if err := json.Unmarshal(raw, &resp); err != nil {
		return nil, fmt.Errorf("fusionnats: decode prefix: %w", err)
	}
	ids := make([]string, 0, len(resp.Entities))
	for i := range resp.Entities {
		ids = append(ids, resp.Entities[i].ID)
	}
	return ids, nil
}

// resolveSemantic resolves a natural-language query to embedding-ranked entity
// IDs via graph.query.semantic. A non-empty scope constrains candidates to the
// given entity-ID prefixes at the source (ADR-071); it is inserted into the
// request body ONLY when non-empty, so an unscoped call is byte-identical to the
// pre-scope wire shape (every existing caller, and every symbol/prefix path,
// sends none).
func (c *Client) resolveSemantic(ctx context.Context, query string, scope []string, limit int) ([]string, error) {
	body := map[string]any{"query": query, "limit": limit}
	if len(scope) > 0 {
		body["scope"] = scope
	}
	raw, err := c.request(ctx, subjectSemantic, body)
	if err != nil {
		return nil, err
	}
	var resp struct {
		Results []struct {
			EntityID string `json:"entity_id"`
		} `json:"results"`
	}
	if err := json.Unmarshal(raw, &resp); err != nil {
		return nil, fmt.Errorf("fusionnats: decode semantic: %w", err)
	}
	ids := make([]string, 0, len(resp.Results))
	for _, r := range resp.Results {
		if r.EntityID != "" {
			ids = append(ids, r.EntityID)
		}
	}
	return ids, nil
}

// Entity returns an entity by ID, or (nil, nil) if absent. The graph-ingest
// handler classifies a missing entity as ErrorInvalid with code
// entity_not_found; that is the ONE error we translate to absence — every other
// error propagates so the engine never reads a backend fault as a not-found.
func (c *Client) Entity(ctx context.Context, id string) (*fusion.Entity, error) {
	raw, err := c.request(ctx, subjectEntity, map[string]string{"id": id})
	if err != nil {
		if isNotFound(err) {
			return nil, nil
		}
		return nil, err
	}
	var es graph.EntityState
	if err := json.Unmarshal(raw, &es); err != nil {
		return nil, fmt.Errorf("fusionnats: decode entity %q: %w", id, err)
	}
	return &fusion.Entity{ID: es.ID, Triples: es.Triples}, nil
}

// Entities batch-fetches entities by ID via graph.query.batch. Absent IDs are
// omitted by the handler (partial success); a non-nil error means a backend
// failure, which the engine distinguishes from genuine absence.
func (c *Client) Entities(ctx context.Context, ids []string) ([]*fusion.Entity, error) {
	if len(ids) == 0 {
		return nil, nil
	}
	raw, err := c.request(ctx, subjectBatch, map[string]any{"ids": ids})
	if err != nil {
		return nil, err
	}
	var resp struct {
		Entities []graph.EntityState `json:"entities"`
	}
	if err := json.Unmarshal(raw, &resp); err != nil {
		return nil, fmt.Errorf("fusionnats: decode batch: %w", err)
	}
	out := make([]*fusion.Entity, 0, len(resp.Entities))
	for i := range resp.Entities {
		out = append(out, &fusion.Entity{ID: resp.Entities[i].ID, Triples: resp.Entities[i].Triples})
	}
	return out, nil
}

// Neighbors returns edges from id along the given predicates in a direction via
// graph.query.relationships. The handler returns ALL edges in the direction;
// this filters to the requested predicates (empty predicates means no filter).
func (c *Client) Neighbors(ctx context.Context, id string, predicates []string, dir fusion.Direction) ([]fusion.Edge, error) {
	raw, err := c.request(ctx, subjectRelationships, map[string]string{
		"entity_id": id,
		"direction": directionString(dir),
	})
	if err != nil {
		return nil, err
	}
	var rels []struct {
		FromEntityID string `json:"from_entity_id"`
		ToEntityID   string `json:"to_entity_id"`
		EdgeType     string `json:"edge_type"`
	}
	if err := json.Unmarshal(raw, &rels); err != nil {
		return nil, fmt.Errorf("fusionnats: decode relationships: %w", err)
	}
	want := predicateSet(predicates)
	edges := make([]fusion.Edge, 0, len(rels))
	for _, r := range rels {
		if want != nil && !want[r.EdgeType] {
			continue
		}
		// The target is the OTHER end of the edge: outgoing points to
		// to_entity_id, incoming comes from from_entity_id.
		target := r.ToEntityID
		if dir == fusion.Incoming {
			target = r.FromEntityID
		}
		edges = append(edges, fusion.Edge{Predicate: r.EdgeType, Target: target})
	}
	return edges, nil
}

// Names suggests entity display names near a query for a miss's did_you_mean,
// via graph.query.byName. Multiple entities may share a name; names are
// de-duplicated preserving rank order and capped at limit.
func (c *Client) Names(ctx context.Context, query string, limit int) ([]string, error) {
	matches, err := c.byNameMatches(ctx, query, limit)
	if err != nil {
		return nil, err
	}
	seen := make(map[string]bool, len(matches))
	names := make([]string, 0, len(matches))
	for _, m := range matches {
		if m.MatchedName == "" || seen[m.MatchedName] {
			continue
		}
		seen[m.MatchedName] = true
		names = append(names, m.MatchedName)
		if limit > 0 && len(names) >= limit {
			break
		}
	}
	return names, nil
}

// byNameMatches calls graph.query.byName and returns the ranked matches. Shared
// by Resolve(symbol) (which reads entity IDs) and Names (which reads names).
func (c *Client) byNameMatches(ctx context.Context, query string, limit int) ([]graph.NameMatch, error) {
	raw, err := c.request(ctx, subjectByName, map[string]any{"name": query, "limit": limit})
	if err != nil {
		return nil, err
	}
	var resp graph.QueryResponse[graph.NameData]
	if err := json.Unmarshal(raw, &resp); err != nil {
		return nil, fmt.Errorf("fusionnats: decode byName: %w", err)
	}
	return resp.Data.Matches, nil
}

// request marshals req and issues a classified request/reply on subject.
func (c *Client) request(ctx context.Context, subject string, req any) ([]byte, error) {
	body, err := json.Marshal(req)
	if err != nil {
		return nil, fmt.Errorf("fusionnats: marshal %s request: %w", subject, err)
	}
	return c.nats.RequestClassified(ctx, subject, body, c.timeout)
}

// directionString maps a fusion.Direction to the relationship query's wire value.
func directionString(dir fusion.Direction) string {
	if dir == fusion.Incoming {
		return "incoming"
	}
	return "outgoing"
}

// predicateSet builds a lookup set from predicates, or nil when none are given
// (nil means "no filter" — accept every predicate).
func predicateSet(predicates []string) map[string]bool {
	if len(predicates) == 0 {
		return nil
	}
	set := make(map[string]bool, len(predicates))
	for _, p := range predicates {
		set[p] = true
	}
	return set
}

// isNotFound reports whether err is the graph's entity_not_found classified
// error (the only error Entity treats as absence rather than a fault).
func isNotFound(err error) bool {
	var ce *errs.ClassifiedError
	return errors.As(err, &ce) && ce.Code == graph.ErrorCodeEntityNotFound
}

package service

// graph_triples_http.go — operator-facing read endpoint for graph triples.
//
// Placement: service-manager mux (this file). The graph-gateway component is
// not wired into the ops-agent, crud-tools, or deep-research flows; the
// service-manager mux is present in every flow configuration, requiring no
// additional component wiring.
//
// Operator-facing read endpoint for graph triples; expect low-throughput use
// (e2e tests, ops dashboards, debugging). Not a high-volume API.
// Authentication is delegated to whatever gates the service-manager mux —
// no auth model changes in this file.

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"strconv"
	"time"

	"github.com/c360studio/semstreams/graph"
	"github.com/c360studio/semstreams/message"
)

const (
	graphTriplesDefaultLimit = 100
	graphTriplesMaxLimit     = 1000

	// graphEntityStatesBucket is the KV bucket graph-ingest writes to.
	// Same name the query_entity agent tool reads from — single source of truth.
	graphEntityStatesBucket = "ENTITY_STATES"
)

// TripleResponse is the wire shape returned by GET /graph/triples.
// Field names mirror message.Triple JSON tags so downstream tests and
// dashboards see the same layout as the graph_query agent tool.
type TripleResponse struct {
	Subject    string    `json:"subject"`
	Predicate  string    `json:"predicate"`
	Object     any       `json:"object"`
	Source     string    `json:"source,omitempty"`
	Confidence float64   `json:"confidence"`
	Timestamp  time.Time `json:"timestamp"`
}

// tripleFromMessage converts a message.Triple to the HTTP wire shape.
func tripleFromMessage(t message.Triple) TripleResponse {
	return TripleResponse{
		Subject:    t.Subject,
		Predicate:  t.Predicate,
		Object:     t.Object,
		Source:     t.Source,
		Confidence: t.Confidence,
		Timestamp:  t.Timestamp,
	}
}

// tripleQueryParams holds parsed and validated query parameters.
type tripleQueryParams struct {
	subject   string // empty = match any
	predicate string // empty = match any
	object    string // empty = match any; compared as fmt.Sprintf("%v", triple.Object)
	limit     int
}

// parseTripleQueryParams parses and validates GET /graph/triples query parameters.
// Returns (params, errMsg) where errMsg is non-empty on bad input.
func parseTripleQueryParams(r *http.Request) (tripleQueryParams, string) {
	q := r.URL.Query()

	p := tripleQueryParams{
		subject:   q.Get("subject"),
		predicate: q.Get("predicate"),
		object:    q.Get("object"),
		limit:     graphTriplesDefaultLimit,
	}

	if raw := q.Get("limit"); raw != "" {
		n, err := strconv.Atoi(raw)
		if err != nil {
			return tripleQueryParams{}, fmt.Sprintf("invalid limit %q: must be an integer", raw)
		}
		if n < 0 {
			return tripleQueryParams{}, fmt.Sprintf("invalid limit %d: must be non-negative", n)
		}
		if n == 0 {
			// limit=0 treated as "use default"
			n = graphTriplesDefaultLimit
		}
		if n > graphTriplesMaxLimit {
			n = graphTriplesMaxLimit
		}
		p.limit = n
	}

	return p, ""
}

// tripleMatchesQuery reports whether t satisfies all non-empty filter fields.
// An empty filter field is a wildcard — it matches any value.
func tripleMatchesQuery(t message.Triple, p tripleQueryParams) bool {
	if p.subject != "" && t.Subject != p.subject {
		return false
	}
	if p.predicate != "" && t.Predicate != p.predicate {
		return false
	}
	if p.object != "" {
		// Object is typed as any; compare its string representation.
		if fmt.Sprintf("%v", t.Object) != p.object {
			return false
		}
	}
	return true
}

// handleGraphTriples returns matching triples as JSON.
//
// Query params (all optional):
//
//	subject    - exact match on subject field
//	predicate  - exact match on predicate field
//	object     - string representation match on object field
//	limit      - max results (default 100, clamped to 1000; 0 uses default)
//
// Returns:
//
//	200 + JSON array of triple objects (empty array on no matches, never null)
//	400 + text error on malformed query params
//	405 + text error on non-GET methods
//	500 + text error on backend failure
func (m *Manager) handleGraphTriples(w http.ResponseWriter, r *http.Request) {
	serveGraphTriples(w, r, m.queryGraphTriples, m.logger)
}

// serveGraphTriples is the testable core of handleGraphTriples.
// It accepts an injectable querier function so unit tests can exercise the full
// HTTP layer (param parsing, status codes, JSON encoding) without a live NATS
// connection. Production code calls it via handleGraphTriples with queryGraphTriples.
func serveGraphTriples(
	w http.ResponseWriter,
	r *http.Request,
	querier func(ctx context.Context, params tripleQueryParams) ([]TripleResponse, error),
	logger *slog.Logger,
) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	params, errMsg := parseTripleQueryParams(r)
	if errMsg != "" {
		http.Error(w, errMsg, http.StatusBadRequest)
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
	defer cancel()

	results, err := querier(ctx, params)
	if err != nil {
		if logger != nil {
			logger.Error("graph triples query failed", slog.Any("error", err))
		}
		http.Error(w, "backend query failed", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(results); err != nil && logger != nil {
		logger.Error("failed to encode graph triples response", slog.Any("error", err))
	}
}

// queryGraphTriples scans the ENTITY_STATES KV bucket and returns matching triples
// up to params.limit. Returns an empty (non-nil) slice when there are no matches.
//
// The bucket is opened via m.natsClient on each call; the NATS client caches the
// underlying JetStream handle so this is inexpensive after the first access.
//
// Scanning all entities for a predicate filter is acceptable for low-throughput
// operator use (e2e tests, dashboards). For high-volume use, a predicate index
// would be required — outside current scope.
func (m *Manager) queryGraphTriples(ctx context.Context, params tripleQueryParams) ([]TripleResponse, error) {
	if m.natsClient == nil {
		// NATS not configured — return empty result, not an error.
		// This happens in unit tests and in startup flows that skip NATS.
		return []TripleResponse{}, nil
	}

	bucket, err := m.natsClient.GetKeyValueBucket(ctx, graphEntityStatesBucket)
	if err != nil {
		return nil, fmt.Errorf("open %s bucket: %w", graphEntityStatesBucket, err)
	}

	store := m.natsClient.NewKVStore(bucket)

	// Keys() returns nil, nil when the bucket is empty (KVStore handles ErrNoKeysFound).
	keys, err := store.Keys(ctx)
	if err != nil {
		return nil, fmt.Errorf("list entity keys: %w", err)
	}

	results := make([]TripleResponse, 0)

	for _, key := range keys {
		if len(results) >= params.limit {
			break
		}

		entry, err := store.Get(ctx, key)
		if err != nil {
			// Key may have been deleted between Keys() and Get(); skip gracefully.
			m.logger.Debug("graph triples: skipping key after fetch error",
				slog.String("key", key), slog.Any("error", err))
			continue
		}

		var entity graph.EntityState
		if err := json.Unmarshal(entry.Value, &entity); err != nil {
			m.logger.Debug("graph triples: skipping entity with invalid JSON",
				slog.String("key", key), slog.Any("error", err))
			continue
		}

		for _, t := range entity.Triples {
			if len(results) >= params.limit {
				break
			}
			if tripleMatchesQuery(t, params) {
				results = append(results, tripleFromMessage(t))
			}
		}
	}

	return results, nil
}

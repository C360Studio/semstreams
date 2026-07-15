package query

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/c360studio/semstreams/graph"
	semtypes "github.com/c360studio/semstreams/pkg/types"
)

const prefixQueryTimeout = 30 * time.Second
const prefixQuerySubject = "graph.query.prefix"

// QueryPrefix fetches one page of entities whose IDs share the given prefix.
// Pass graph.PrefixQueryRequest.Cursor from the previous response to page
// forward; leave it empty for the first page.
//
// Error handling: transport errors and handler failures surface as a non-nil
// error (never a silently-empty result decoded from an "error: " body). Use
// errs.IsInvalid / IsTransient to branch on error class.
//
// Class-fidelity caveat: this helper calls the PUBLIC graph.query.prefix
// subject, whose graph-query passthrough re-responds the graph-ingest body with
// plain Request (no X-Error-Class propagation). So a graph-ingest error reaches
// here only via the legacy "error: " body prefix, which ClassifyReply always
// coerces to Invalid — a transient graph-ingest failure is reported as Invalid,
// not Transient. The error is still surfaced (the contract that matters for
// SemOps); only the class can be imprecise. Call graph.ingest.query.prefix
// directly for full class fidelity. Propagating the class through the
// passthrough is a tracked follow-up.
//
// For a multi-page accumulate, use QueryPrefixAll (bounded by a caller cap).
func (qc *natsClient) QueryPrefix(ctx context.Context, req graph.PrefixQueryRequest) (graph.PrefixQueryResponse, error) {
	if err := validatePrefix(req.Prefix); err != nil {
		return graph.PrefixQueryResponse{}, err
	}
	reqData, err := json.Marshal(req)
	if err != nil {
		return graph.PrefixQueryResponse{}, err
	}

	resp, err := qc.natsClient.RequestClassified(ctx, prefixQuerySubject, reqData, prefixQueryTimeout)
	if err != nil {
		return graph.PrefixQueryResponse{}, err
	}

	var result graph.PrefixQueryResponse
	if err := json.Unmarshal(resp, &result); err != nil {
		return graph.PrefixQueryResponse{}, err
	}
	return result, nil
}

// QueryPrefixAll pages through graph.query.prefix following the opaque cursor and
// returns all matching entities, UP TO maxEntities. It exists so callers don't
// hand-roll the cursor loop for prefix-scoped snapshot hydration.
//
// Bounded by design: maxEntities MUST be > 0. There is no unbounded mode — an
// unbounded accumulator would re-introduce the reply-size / OOM risk the
// single-page cursor exists to bound (the caller chooses the bound explicitly).
//
// Returns (entities, truncated, err):
//   - truncated is true when maxEntities was reached while the server still had
//     more results (i.e. entities exist beyond those returned). false means the
//     full result set was returned (it was <= maxEntities). (Edge: if a
//     contract-violating server ever returns an empty page WITH a cursor, the
//     helper stops defensively and reports truncated=false rather than spin.)
//
// req.Cursor is ignored — paging always starts at the beginning of the prefix.
// Set req.Prefix (and optionally req.Limit as the per-page size); the helper
// caps each page to the remaining budget so it never over-fetches past the cap.
//
// No streaming/never-accumulate variant exists yet (this package returns slices);
// add one only if a consumer must process pages without holding the working set.
func (qc *natsClient) QueryPrefixAll(ctx context.Context, req graph.PrefixQueryRequest, maxEntities int) ([]graph.EntityState, bool, error) {
	return pagePrefixAll(ctx, req, maxEntities, qc.QueryPrefix)
}

// pagePrefixAll is the cursor-following accumulator behind QueryPrefixAll,
// factored out so the paging / truncation / per-page-cap logic is unit-testable
// with a scripted fetch function (no NATS).
func pagePrefixAll(
	ctx context.Context,
	req graph.PrefixQueryRequest,
	maxEntities int,
	fetch func(context.Context, graph.PrefixQueryRequest) (graph.PrefixQueryResponse, error),
) ([]graph.EntityState, bool, error) {
	if maxEntities <= 0 {
		return nil, false, fmt.Errorf("QueryPrefixAll: maxEntities must be > 0 (got %d); there is no unbounded mode", maxEntities)
	}
	if err := validatePrefix(req.Prefix); err != nil {
		return nil, false, err
	}

	req.Cursor = "" // always start from the beginning of the prefix
	var all []graph.EntityState
	for {
		// Cap this page to the remaining budget so a large req.Limit (or the
		// server default) can't over-fetch past maxEntities.
		pageReq := req
		remaining := maxEntities - len(all)
		if pageReq.Limit <= 0 || pageReq.Limit > remaining {
			pageReq.Limit = remaining
		}

		page, err := fetch(ctx, pageReq)
		if err != nil {
			return nil, false, err
		}
		all = append(all, page.Entities...)

		if len(all) >= maxEntities {
			// Hit the cap. More exist iff the server still has a cursor.
			return all, page.NextCursor != "", nil
		}
		if page.NextCursor == "" {
			return all, false, nil // result set exhausted
		}
		if len(page.Entities) == 0 {
			// Defensive: a cursor with no entities should not occur (keyset
			// exhaustion clears the cursor). Stop rather than spin.
			return all, false, nil
		}
		req.Cursor = page.NextCursor
	}
}

// validatePrefix preserves the public empty-prefix match-all contract while
// enforcing the canonical literal entity-ID prefix grammar for scoped queries.
func validatePrefix(prefix string) error {
	if prefix == "" {
		return nil
	}
	return semtypes.ValidateEntityIDPrefix(prefix)
}

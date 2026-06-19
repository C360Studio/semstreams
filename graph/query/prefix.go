package query

import (
	"context"
	"encoding/json"
	"time"

	"github.com/c360studio/semstreams/graph"
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
// A future QueryPrefixAll convenience helper (auto-paging accumulator) may be
// added, but is deliberately omitted here to avoid re-introducing unbounded
// accumulation at the call site.
func (qc *natsClient) QueryPrefix(ctx context.Context, req graph.PrefixQueryRequest) (graph.PrefixQueryResponse, error) {
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

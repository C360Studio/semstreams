// Package graphquery — graph summary handler.
package graphquery

import (
	"context"
	"encoding/json"
	"errors"
	"sort"
	"strings"

	"github.com/nats-io/nats.go"

	"github.com/c360studio/semstreams/graph"
	"github.com/c360studio/semstreams/pkg/errs"
)

const (
	// graphSummaryDefaultSampleLimit is the V1 cap on entity IDs the
	// type-distribution scan pulls from graph-ingest. Picked at 2000
	// because: (a) one NATS message of that many short strings is
	// comfortably under the gateway's 100KB body budget, (b) at
	// typical scales (~10k-100k entities) this is large enough to
	// surface the major type buckets, (c) tight enough to keep the
	// per-call latency bounded. Operators tuning for larger graphs
	// override via GraphSummaryRequest.EntitySampleLimit.
	graphSummaryDefaultSampleLimit = 2000

	// graphSummaryDefaultExamplesPerType caps example IDs per type
	// bucket. 2 is the V1 sweet spot: enough that the caller sees
	// the ID shape (helps prevent ID hallucination per the semspec
	// bug-log precedent at docs/bugs/gemini-duplicate-graph-summary.md)
	// while keeping the response compact.
	graphSummaryDefaultExamplesPerType = 2

	// graphSummaryMinEntityIDSegments is the minimum number of
	// dot-separated parts in a canonical entity ID per
	// message.IsValidEntityID. Below this we skip the entity from
	// the type-distribution aggregation (it isn't a real entity ID).
	graphSummaryMinEntityIDSegments = 6
)

// handleQueryGraphSummary composes a graph overview by fanning out to:
//
//   - graph.ingest.query.prefix (prefix="" limit=sample) for the
//     entity-type distribution and per-type example IDs.
//   - graph.index.query.predicateList for predicate counts.
//
// Either subordinate can fail independently; partial responses are
// still useful (a graph with no predicate index should still surface
// entity types). The handler returns the best result it can build
// from whichever subordinates succeeded, with the failing facet
// simply omitted (Predicates nil, EntityTypes empty). Hard transport
// errors propagate so the caller sees the gateway/NATS issue rather
// than an empty response.
//
// Deliberate V1 scope: NO Source distribution (Triple.Source
// aggregation). That facet requires a write-time index over triple
// sources or a full triple walk; both are V2 candidates documented
// in graph/query_summary_types.go.
func (c *Component) handleQueryGraphSummary(ctx context.Context, data []byte) ([]byte, error) {
	req := parseSummaryRequest(data)

	summary := graph.SummaryData{}

	// Entity-type aggregation via the prefix-query orchestration the
	// hierarchyStats handler already uses. Empty prefix scans all
	// entities up to the limit; downstream is responsible for the
	// envelope shape.
	prefixPayload, err := json.Marshal(map[string]any{
		"prefix": "",
		"limit":  req.EntitySampleLimit,
	})
	if err != nil {
		return nil, errs.Wrap(err, "GraphQuery", "handleQueryGraphSummary", "marshal prefix request")
	}
	prefixSubject := c.router.Route("entityPrefix")
	if prefixSubject == "" {
		return nil, errs.WrapTransient(errors.New("entityPrefix query routing not available"), "GraphQuery", "handleQueryGraphSummary", "route prefix query")
	}

	prefixResp, prefixErr := c.natsClient.Request(ctx, prefixSubject, prefixPayload, c.config.QueryTimeout)
	if prefixErr != nil {
		c.recordError(prefixErr)
		if errors.Is(prefixErr, nats.ErrTimeout) {
			return nil, errs.WrapTransient(prefixErr, "GraphQuery", "handleQueryGraphSummary", "prefix request timeout")
		}
		// Non-timeout transport errors from graph-ingest also bubble:
		// they indicate the data plane is unavailable, not a partial-
		// data shape worth returning.
		return nil, errs.WrapTransient(prefixErr, "GraphQuery", "handleQueryGraphSummary", "prefix request")
	}

	entityIDs := extractEntityIDsFromPrefixResponse(prefixResp)
	summary.TotalEntities = len(entityIDs)
	summary.EntitySampleTruncated = len(entityIDs) >= req.EntitySampleLimit
	summary.EntityTypes = aggregateEntityTypes(entityIDs, req.ExamplesPerType)

	// Predicate counts via the existing index. This call is OPTIONAL
	// (caller can suppress with IncludePredicates=false) and the
	// failure mode is non-fatal — if graph-index is degraded, return
	// the entity facet and let the caller note the gap.
	if req.IncludePredicates {
		predicateResp, predErr := c.natsClient.Request(ctx, "graph.index.query.predicateList", []byte("{}"), c.config.QueryTimeout)
		if predErr != nil {
			// Log via the metric path; don't propagate. The caller
			// gets a partial-but-useful response.
			c.recordError(predErr)
		} else {
			predicates := extractPredicates(predicateResp)
			summary.Predicates = predicates
			summary.PredicateTotal = len(predicates)
		}
	}

	responseData, err := json.Marshal(graph.NewQueryResponse(summary))
	if err != nil {
		c.recordError(err)
		return nil, errs.Wrap(err, "GraphQuery", "handleQueryGraphSummary", "marshal response")
	}
	c.recordSuccess(len(data), len(responseData))
	return responseData, nil
}

// parseSummaryRequest decodes the request bytes and applies V1
// defaults. Robust to empty/malformed input — the gateway sometimes
// passes "{}" for variable-less queries.
func parseSummaryRequest(data []byte) graph.SummaryRequest {
	req := graph.SummaryRequest{
		IncludePredicates: true,
		EntitySampleLimit: graphSummaryDefaultSampleLimit,
		ExamplesPerType:   graphSummaryDefaultExamplesPerType,
	}
	if len(data) == 0 {
		return req
	}
	// Parse into a partial struct so a missing field uses the default,
	// not the zero value.
	var partial struct {
		IncludePredicates *bool `json:"include_predicates,omitempty"`
		EntitySampleLimit *int  `json:"entity_sample_limit,omitempty"`
		ExamplesPerType   *int  `json:"examples_per_type,omitempty"`
	}
	if err := json.Unmarshal(data, &partial); err != nil {
		return req
	}
	if partial.IncludePredicates != nil {
		req.IncludePredicates = *partial.IncludePredicates
	}
	if partial.EntitySampleLimit != nil && *partial.EntitySampleLimit > 0 {
		req.EntitySampleLimit = *partial.EntitySampleLimit
	}
	if partial.ExamplesPerType != nil && *partial.ExamplesPerType > 0 {
		req.ExamplesPerType = *partial.ExamplesPerType
	}
	return req
}

// extractEntityIDsFromPrefixResponse pulls the IDs out of the
// graph-ingest prefix response envelope. graph-ingest returns
// {"entities": [{"id": "..."}, ...]} per the existing
// handleQueryPrefixNATS shape. Returns an empty slice on any parse
// failure; callers treat that as "no entities discoverable" rather
// than a hard error so a partial summary still ships.
func extractEntityIDsFromPrefixResponse(data []byte) []string {
	var envelope struct {
		Entities []struct {
			ID string `json:"id"`
		} `json:"entities"`
	}
	if err := json.Unmarshal(data, &envelope); err != nil {
		return nil
	}
	out := make([]string, 0, len(envelope.Entities))
	for _, e := range envelope.Entities {
		if e.ID != "" {
			out = append(out, e.ID)
		}
	}
	return out
}

// aggregateEntityTypes groups entity IDs into domain.system.type
// buckets (segments 2.3.4 of the 6-part ID) and selects up to
// examplesPerType sample IDs per bucket. Returns the buckets sorted
// by count descending (highest-count types first), with ties broken
// alphabetically on the type name so the response is stable across
// calls — load-bearing for prompt-caching downstream.
func aggregateEntityTypes(entityIDs []string, examplesPerType int) []graph.EntityTypeSummary {
	if examplesPerType <= 0 {
		examplesPerType = graphSummaryDefaultExamplesPerType
	}
	type bucket struct {
		count    int
		examples []string
	}
	buckets := make(map[string]*bucket)
	for _, id := range entityIDs {
		segs := strings.Split(id, ".")
		if len(segs) < graphSummaryMinEntityIDSegments {
			continue
		}
		typeKey := segs[2] + "." + segs[3] + "." + segs[4]
		b, ok := buckets[typeKey]
		if !ok {
			b = &bucket{}
			buckets[typeKey] = b
		}
		b.count++
		if len(b.examples) < examplesPerType {
			b.examples = append(b.examples, id)
		}
	}
	out := make([]graph.EntityTypeSummary, 0, len(buckets))
	for t, b := range buckets {
		out = append(out, graph.EntityTypeSummary{Type: t, Count: b.count, Examples: b.examples})
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Count != out[j].Count {
			return out[i].Count > out[j].Count
		}
		return out[i].Type < out[j].Type
	})
	return out
}

// extractPredicates pulls the predicate list out of the
// graph.index.query.predicateList response. Returns an empty slice
// when the response is malformed or carries an error — the summary
// caller treats predicate facet as best-effort.
func extractPredicates(data []byte) []graph.PredicateSummary {
	// Response shape per graph.NewQueryResponse[PredicateListData]:
	// { "data": {"predicates": [...], "total": N}, "error": "" }
	// Error is non-empty on the failure branch (NewQueryError); we
	// treat that as "no predicates" rather than failing the summary.
	var resp graph.QueryResponse[graph.PredicateListData]
	if err := json.Unmarshal(data, &resp); err != nil {
		return nil
	}
	if resp.Error != "" {
		return nil
	}
	return resp.Data.Predicates
}

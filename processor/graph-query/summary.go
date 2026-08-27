// Package graphquery — graph summary handler.
package graphquery

import (
	"context"
	"encoding/json"
	"errors"
	"sort"

	"github.com/c360studio/semstreams/graph"
	"github.com/c360studio/semstreams/pkg/errs"
	semtypes "github.com/c360studio/semstreams/pkg/types"
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
	// envelope shape. Use typed PrefixQueryRequest to participate in
	// the pagination contract.
	prefixPayload, err := json.Marshal(graph.PrefixQueryRequest{
		Prefix: "",
		// entity-id-audit:classify intentional-sentinel "" line=63 column=11 surface=go-field:PrefixQueryRequest.Prefix entity_id_prefix_invalid:empty documented match-all query
		Limit: req.EntitySampleLimit,
	})
	if err != nil {
		return nil, errs.Wrap(err, "GraphQuery", "handleQueryGraphSummary", "marshal prefix request")
	}
	prefixSubject := c.router.Route("entityPrefix")
	if prefixSubject == "" {
		return nil, errs.WrapTransient(errors.New("entityPrefix query routing not available"), "GraphQuery", "handleQueryGraphSummary", "route prefix query")
	}

	// ADR-060: RequestClassified surfaces a handler failure via err. With raw
	// Request, a handler error body ({"message":...}) would decode to an empty
	// entity list in extractEntityIDsFromPrefixResponse and return a bogus
	// TotalEntities=0 summary with nil error. Propagate the classified error
	// UNWRAPPED so the summary handler's wrapper re-stamps its class + code.
	prefixResp, prefixErr := c.natsClient.RequestClassified(ctx, prefixSubject, prefixPayload, c.config.QueryTimeout)
	if prefixErr != nil {
		c.recordError(prefixErr)
		return nil, prefixErr
	}

	entityIDs, err := extractEntityIDsFromPrefixResponse(prefixResp)
	if err != nil {
		c.recordError(err)
		return nil, err
	}
	summary.TotalEntities = len(entityIDs)
	summary.EntitySampleTruncated = len(entityIDs) >= req.EntitySampleLimit
	summary.EntityTypes = aggregateEntityTypes(entityIDs, req.ExamplesPerType)

	// Predicate counts via the existing index. This call is OPTIONAL
	// (caller can suppress with IncludePredicates=false) and the
	// failure mode is non-fatal — if graph-index is degraded, return
	// the entity facet and let the caller note the gap.
	if req.IncludePredicates {
		predicateResp, predErr := c.natsClient.RequestClassified(ctx, "graph.index.query.predicateList", []byte("{}"), c.config.QueryTimeout)
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
// handleQueryPrefixNATS shape. It validates the complete EntityState page
// before projecting IDs; a poisoned authoritative page is a hard graph-state
// failure, not an empty partial-summary facet.
func extractEntityIDsFromPrefixResponse(data []byte) ([]string, error) {
	var envelope graph.PrefixQueryResponse
	if err := json.Unmarshal(data, &envelope); err != nil {
		return nil, errs.WrapInvalid(err, "GraphQuery", "extractEntityIDsFromPrefixResponse", "unmarshal prefix response")
	}
	if err := graph.ValidateDecodedEntityStates(envelope.Entities); err != nil {
		return nil, err
	}
	out := make([]string, 0, len(envelope.Entities))
	for _, e := range envelope.Entities {
		out = append(out, e.ID)
	}
	return out, nil
}

// aggregateEntityTypes groups canonical entity IDs into system.domain.type
// buckets (positions 3-5 of org.platform.system.domain.type.instance, read by
// named field; a value the canonical parser rejects is skipped) and selects up to
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
		parsed, err := semtypes.ParseEntityID(id)
		if err != nil {
			continue
		}
		typeKey := parsed.System + "." + parsed.Domain + "." + parsed.Type
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
// graph.index.query.predicateList success body. Returns an empty slice
// when the body is malformed — the summary caller treats the predicate
// facet as best-effort. Handler errors no longer reach here: the caller
// uses RequestClassified (ADR-060), so failures arrive as a classified
// err, not an in-band Error field on a success body.
func extractPredicates(data []byte) []graph.PredicateSummary {
	var resp graph.QueryResponse[graph.PredicateListData]
	if err := json.Unmarshal(data, &resp); err != nil {
		return nil
	}
	return resp.Data.Predicates
}

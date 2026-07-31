package scenarios

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"
)

// gh#768 — gateway response SHAPE stage, the merge gate for the gh#762 fix.
//
// The double-nesting bug shipped and lived for weeks because no e2e stage
// validated the gateway's response ENVELOPE SHAPE. Every other stage reads
// THROUGH whatever shape exists — they decode into typed structs, and both the
// wrapped and unwrapped shapes decode cleanly — so a shape regression was
// invisible by construction.
//
// This stage therefore asserts over RAW JSON KEYS and never decodes into a
// typed struct. It asserts three things, because fixing one family by breaking
// another is the obvious way to make this stage pass wrongly:
//
//  1. the `graph.query.*` family is UNWRAPPED (the family gh#762 broke),
//  2. the `graph.index.query.*` family is UNCHANGED (regression guard),
//  3. `graph.query.prefix` is UNTOUCHED — PrefixQueryResponse is its own struct,
//     not a QueryResponse[T], and keeps its own separate unwrap (regression
//     guard).
//
// Falsifiability: this stage is RED against a gateway that gates unwrapping on
// the subject prefix, and green after detection lands. That was recorded before
// the fix; a stage never seen red is not a gate.

// shapeProbe is one GraphQL field whose projected shape is asserted.
type shapeProbe struct {
	// name is the stage-local label used in metrics and failure messages.
	name string
	// field is the GraphQL field the gateway projects under `data`.
	field string
	// query is the GraphQL document to POST.
	query string
	// variables are the GraphQL variables, if the document declares any.
	variables map[string]any
	// expectKeys are keys that MUST be present at the TOP level of the
	// projected field. Presence alone is not the assertion — see
	// forbidRepeatedDataHop — but a probe that finds none of its expected keys
	// is querying something other than what it thinks.
	expectKeys []string
	// arrayShaped marks a field projected as a bare JSON array rather than an
	// object (graph.query.prefix after its own unwrap).
	arrayShaped bool
}

func gatewayShapeProbes() []shapeProbe {
	return []shapeProbe{
		{
			// (1) The family gh#762 broke. graph.query.summary returns
			// QueryResponse[SummaryData]; the old prefix gate did not match it,
			// so callers read data.graphSummary.data.total_entities.
			name:       "graph_query_family_unwrapped",
			field:      "graphSummary",
			query:      `{ graphSummary { totalEntities entityTypes { type count } } }`,
			expectKeys: []string{"total_entities"},
		},
		{
			// (2) Regression guard: the family that already worked must keep
			// working. graph.index.query.predicateList returns
			// QueryResponse[PredicateListData].
			name:       "graph_index_query_family_unchanged",
			field:      "predicates",
			query:      `{ predicates { predicates { predicate entityCount } total } }`,
			expectKeys: []string{"predicates"},
		},
		{
			// (3) Regression guard: PrefixQueryResponse is NOT the query
			// envelope and must not be claimed by detection. Its own unwrap
			// still produces a bare entities array.
			name:        "graph_query_prefix_untouched",
			field:       "entitiesByPrefix",
			query:       `query($prefix: String!) { entitiesByPrefix(prefix: $prefix) { id } }`,
			variables:   map[string]any{"prefix": ""},
			arrayShaped: true,
		},
	}
}

// executeValidateGatewayResponseShape is the gh#768 stage.
func (s *TieredScenario) executeValidateGatewayResponseShape(ctx context.Context, result *Result) error {
	probes := gatewayShapeProbes()
	checked := 0

	for _, probe := range probes {
		projected, latency, err := s.probeGatewayFieldShape(ctx, probe)
		if err != nil {
			return fmt.Errorf("gateway shape probe %q: %w", probe.name, err)
		}

		if err := assertProjectedShape(probe, projected); err != nil {
			return fmt.Errorf("gateway shape probe %q: %w", probe.name, err)
		}

		checked++
		result.Metrics[probe.name+"_latency_ms"] = latency.Milliseconds()
	}

	// Count what was actually asserted, against a LITERAL.
	//
	// An earlier version compared `checked != len(probes)`, which is a
	// tautology: the loop returns on the first error, so after it `checked ==
	// len(probes)` always. The one case it claimed to catch — a probe list that
	// shrank or emptied — was exactly the case it missed, since an empty
	// gatewayShapeProbes() gives 0 != 0 false and the stage reports GREEN with
	// zero assertions. Found by review.
	result.Metrics["gateway_shape_probes_checked"] = checked
	if checked != wantShapeProbes {
		return fmt.Errorf(
			"gateway shape: asserted %d probes, expected %d — a shrunken probe set is a "+
				"silent loss of coverage, not a pass", checked, wantShapeProbes)
	}
	return nil
}

// wantShapeProbes is the number of probes this stage must run: the
// graph.query.* family, the graph.index.query.* regression guard, and the
// graph.query.prefix regression guard. Deleting a probe is a stage FAILURE.
const wantShapeProbes = 3

// probeGatewayFieldShape POSTs a GraphQL query and returns the RAW JSON of the
// projected field, without decoding it into any typed struct.
func (s *TieredScenario) probeGatewayFieldShape(
	ctx context.Context, probe shapeProbe,
) (json.RawMessage, time.Duration, error) {
	payload := map[string]any{"query": probe.query}
	if probe.variables != nil {
		payload["variables"] = probe.variables
	}
	queryJSON, err := json.Marshal(payload)
	if err != nil {
		return nil, 0, fmt.Errorf("marshal query: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, "POST", s.config.GraphQLURL, bytes.NewReader(queryJSON))
	if err != nil {
		return nil, 0, fmt.Errorf("create request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	start := time.Now()
	resp, err := http.DefaultClient.Do(req)
	latency := time.Since(start)
	if err != nil {
		return nil, 0, fmt.Errorf("request failed: %w", err)
	}
	defer resp.Body.Close()

	bodyBytes, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, 0, fmt.Errorf("read response: %w", err)
	}

	// Deliberately raw: decoding into a struct is what makes both shapes look
	// identical, which is the whole reason this class went unnoticed.
	var envelope struct {
		Data   map[string]json.RawMessage `json:"data"`
		Errors []struct {
			Message string `json:"message"`
		} `json:"errors"`
	}
	if err := json.Unmarshal(bodyBytes, &envelope); err != nil {
		return nil, latency, fmt.Errorf("parse response %q: %w", truncateForError(bodyBytes), err)
	}
	if len(envelope.Errors) > 0 {
		return nil, latency, fmt.Errorf("graphql error: %s", envelope.Errors[0].Message)
	}

	projected, ok := envelope.Data[probe.field]
	if !ok {
		return nil, latency, fmt.Errorf("field %q absent from response %q", probe.field, truncateForError(bodyBytes))
	}
	return projected, latency, nil
}

// assertProjectedShape is the actual gate: it asserts the ABSENCE of a repeated
// `data` hop, not merely that expected leaves are reachable. The double-nested
// shape ALSO makes the leaves reachable — one hop deeper — so a
// reachability-only assertion cannot distinguish the defect from the fix.
func assertProjectedShape(probe shapeProbe, projected json.RawMessage) error {
	if probe.arrayShaped {
		// SHAPE only, deliberately: an empty array passes. This is not a data
		// assertion and must not be read as one — its job is that a removed or
		// reordered prefix unwrap yields an OBJECT and turns this RED. Entity
		// counts are asserted by the dedicated prefix stages.
		var arr []json.RawMessage
		if err := json.Unmarshal(projected, &arr); err != nil {
			return fmt.Errorf("expected a bare array, got %q", truncateForError(projected))
		}
		return nil
	}

	var fields map[string]json.RawMessage
	if err := json.Unmarshal(projected, &fields); err != nil {
		return fmt.Errorf("expected an object, got %q", truncateForError(projected))
	}

	// The defect, stated directly.
	if _, nested := fields["data"]; nested {
		return fmt.Errorf(
			"DOUBLE-NESTED (gh#762): field carries a repeated `data` hop — "+
				"callers would read data.%s.data.* instead of data.%s.*; got %q",
			probe.field, probe.field, truncateForError(projected))
	}
	// The envelope's other always-present key must be gone too; its presence
	// means an envelope survived even if `data` happened to be absent.
	if _, leaked := fields["timestamp"]; leaked {
		return fmt.Errorf(
			"envelope `timestamp` leaked into the projected field; got %q",
			truncateForError(projected))
	}

	for _, key := range probe.expectKeys {
		if _, ok := fields[key]; !ok {
			return fmt.Errorf(
				"expected key %q at the top level of the projected field; got %q",
				key, truncateForError(projected))
		}
	}
	return nil
}

func truncateForError(b []byte) string {
	const limit = 300
	if len(b) <= limit {
		return string(b)
	}
	return string(b[:limit]) + "…"
}

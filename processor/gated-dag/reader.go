package gateddagexec

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/c360studio/semstreams/graph"
	"github.com/c360studio/semstreams/natsclient"
	"github.com/c360studio/semstreams/pkg/gateddag"
)

// graphReader reads the authoritative whole unit set for one evaluation. It is
// an interface so unit tests inject a scripted reader and the executor never
// caches derived state (ADR-046 requirement #1: derived, never mutated).
type graphReader interface {
	// ReadUnitSet returns the full EntityState set under the configured prefix,
	// triples included, read fresh from the graph.
	ReadUnitSet(ctx context.Context) ([]graph.EntityState, error)
}

// natsGraphReader reads the unit set via the graph.query.prefix contract
// (returns full EntityStates incl. markers). Bounded by maxUnits: it follows the
// opaque cursor up to the cap and logs a truncation warning rather than
// silently dropping units past it.
type natsGraphReader struct {
	nc       *natsclient.Client
	prefix   string
	maxUnits int
	timeout  time.Duration
	onTrunc  func(returned, capN int)
}

// prefixQuerySubject is the authoritative whole-set enumeration contract. The
// executor reads the graph-ingest handler DIRECTLY (graph.ingest.query.prefix)
// rather than the public graph.query.prefix passthrough: it is an internal
// framework component, and the direct subject preserves error-class fidelity
// (the public passthrough coerces a transient graph-ingest failure to Invalid).
const prefixQuerySubject = "graph.ingest.query.prefix"

// ReadUnitSet pages graph.query.prefix following the cursor up to maxUnits.
// Each page gets its own r.timeout budget (not a shared deadline across pages),
// so a large multi-page fan-out is not cancelled mid-paging by a single overall
// deadline; the parent ctx still cancels the whole read on shutdown.
func (r *natsGraphReader) ReadUnitSet(ctx context.Context) ([]graph.EntityState, error) {
	var all []graph.EntityState
	cursor := ""
	for {
		remaining := r.maxUnits - len(all)
		if remaining <= 0 {
			// Hit the cap; surface truncation so a too-large fan-out is visible
			// rather than silently partial.
			if r.onTrunc != nil {
				r.onTrunc(len(all), r.maxUnits)
			}
			return all, nil
		}
		req := graph.PrefixQueryRequest{Prefix: r.prefix, Limit: remaining, Cursor: cursor}
		reqData, err := json.Marshal(req)
		if err != nil {
			return nil, fmt.Errorf("marshal prefix request: %w", err)
		}
		respData, err := r.nc.RequestClassified(ctx, prefixQuerySubject, reqData, r.timeout)
		if err != nil {
			return nil, fmt.Errorf("%s: %w", prefixQuerySubject, err)
		}
		var resp graph.PrefixQueryResponse
		if err := json.Unmarshal(respData, &resp); err != nil {
			return nil, fmt.Errorf("unmarshal prefix response: %w", err)
		}
		all = append(all, resp.Entities...)
		if resp.NextCursor == "" {
			return all, nil // exhausted
		}
		cursor = resp.NextCursor
	}
}

// graphView is the brain input extracted from an authoritative unit-set read,
// plus the executor-only claimed set used for in-flight dedup.
type graphView struct {
	unitIDs   []string
	dependsOn map[string][]string
	markers   gateddag.MarkerSet
	claimed   map[string]bool
}

// extractGraph derives the brain inputs (unit IDs, depends_on edges, marker
// membership) and the executor's claimed set from an authoritative unit-set
// read. Pure — no I/O — so it is exhaustively table-testable.
//
// Marker semantics are presence-based: a unit is in a marker set iff it carries
// at least one triple with the configured predicate (the Object value is
// irrelevant for completed/failed/dirtied/claim). depends_on is the exception —
// its Object is the prerequisite unit ID, collected as a directed edge.
func extractGraph(states []graph.EntityState, cfg Config) graphView {
	view := graphView{
		unitIDs:   make([]string, 0, len(states)),
		dependsOn: make(map[string][]string),
		claimed:   make(map[string]bool),
	}
	var completed, failed, dirtied []string

	for i := range states {
		s := &states[i]
		view.unitIDs = append(view.unitIDs, s.ID)
		for j := range s.Triples {
			t := &s.Triples[j]
			switch t.Predicate {
			case cfg.CompletedPredicate:
				completed = append(completed, s.ID)
			case cfg.FailedPredicate:
				failed = append(failed, s.ID)
			case cfg.DirtiedPredicate:
				dirtied = append(dirtied, s.ID)
			case cfg.ClaimPredicate:
				view.claimed[s.ID] = true
			case cfg.DependsOnPredicate:
				if dep, ok := objectAsEntityID(t.Object); ok {
					view.dependsOn[s.ID] = append(view.dependsOn[s.ID], dep)
				}
			}
		}
	}

	view.markers = gateddag.NewMarkerSet(completed, failed, dirtied)
	return view
}

// objectAsEntityID coerces a triple Object to an entity-ID string. depends_on
// edges store the prerequisite ID as a string; anything else is not a valid edge
// target and is skipped (the substitution layer never produces a non-string
// edge object, but the graph stores Object as any).
func objectAsEntityID(o any) (string, bool) {
	s, ok := o.(string)
	if !ok || s == "" {
		return "", false
	}
	return s, true
}

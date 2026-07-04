package gateddagexec

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/c360studio/semstreams/graph"
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

// requester is the minimal request surface natsGraphReader needs: the
// readiness-gated cold-start read and the steady-state read (both classified).
// *natsclient.Client satisfies it; a unit test injects a fake to assert the
// cold-start → steady-state switch and the never-ready signal.
type requester interface {
	RequestReadyClassified(ctx context.Context, subject string, data []byte, probeTimeout, budget time.Duration) ([]byte, error)
	RequestClassified(ctx context.Context, subject string, data []byte, timeout time.Duration) ([]byte, error)
}

// natsGraphReader reads the unit set via the graph.query.prefix contract
// (returns full EntityStates incl. markers). Bounded by maxUnits: it follows the
// opaque cursor up to the cap and logs a truncation warning rather than
// silently dropping units past it.
type natsGraphReader struct {
	nc       requester
	prefix   string
	maxUnits int
	timeout  time.Duration
	onTrunc  func(returned, capN int)

	// Readiness-gated cold start (gh#420). The executor fires its FIRST
	// authoritative read on boot, racing graph-ingest's query-handler
	// registration — a bare request there degrades to a full-timeout hang. Until
	// this reader has seen graph-ingest answer once (warmed), it reads via
	// RequestReadyClassified: short probes up to readyBudget, succeeding the moment
	// the responder is up. After the first success reads are steady-state
	// (RequestClassified, timeout = real signal) so a LATER hung responder is not
	// masked. See docs/operations/07-nats-request-retry.md.
	readyProbe  time.Duration
	readyBudget time.Duration
	warmed      bool
	// onNeverReady fires when the cold-start read exhausts its readiness budget
	// (graph-ingest never answered) — a distinct, actionable signal so a missing
	// responder is loud, not a silent skipped pass.
	onNeverReady func(err error)
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
		var respData []byte
		if r.warmed {
			respData, err = r.nc.RequestClassified(ctx, prefixQuerySubject, reqData, r.timeout)
		} else {
			respData, err = r.nc.RequestReadyClassified(ctx, prefixQuerySubject, reqData, r.readyProbe, r.readyBudget)
		}
		if err != nil {
			if !r.warmed && r.onNeverReady != nil {
				r.onNeverReady(err)
			}
			return nil, fmt.Errorf("%s: %w", prefixQuerySubject, err)
		}
		// graph-ingest answered — subsequent reads are steady-state.
		r.warmed = true
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
	// claimedAt records each claimed unit's claim-triple timestamp so the
	// stranded-unit detector can age-gate a claim (ADR-070): a claimed unit older
	// than StrandedAfter stops counting as healthy in-flight.
	claimedAt map[string]time.Time
	// stubsSkipped counts entities under the unit prefix that were referential
	// stubs (graph.StubMessageType) and therefore excluded from the unit set
	// this read (gh#429). A sustained nonzero value means a referenced unit's
	// real content never landed on a re-stamping lane — an observable stuck-stub
	// signal rather than a silent never-dispatch.
	stubsSkipped int
}

// extractGraph derives the brain inputs (unit IDs, depends_on edges, marker
// membership) and the executor's claimed set from an authoritative unit-set
// read. Pure — no I/O — so it is exhaustively table-testable.
//
// Marker semantics are presence-based: a unit is in a marker set iff it carries
// at least one triple with the configured predicate (the Object value is
// irrelevant for completed/failed/dirtied/claim). depends_on is the exception —
// its Object is the prerequisite unit ID, collected as a directed edge.
//
// Referential stubs are EXCLUDED (gh#429): graph-ingest materializes an
// entity_id-referenced-but-not-yet-born entity as a graph.StubMessageType stub
// under this same unit prefix. A stub has no depends_on edges, so the brain
// would derive it as a dependency-free, immediately-dispatchable unit and
// dispatch it before its real content lands — bypassing dependency ordering.
// Skipping stubs is safe for the dependency closure: DeriveStatus keys on
// marker membership, so a dependent whose prerequisite is a skipped stub is
// still correctly held (the prerequisite is simply not Done).
func extractGraph(states []graph.EntityState, cfg Config) graphView {
	view := graphView{
		unitIDs:   make([]string, 0, len(states)),
		dependsOn: make(map[string][]string),
		claimed:   make(map[string]bool),
		claimedAt: make(map[string]time.Time),
	}
	var completed, failed, dirtied []string

	for i := range states {
		s := &states[i]
		if s.IsStub() {
			// Not-yet-born placeholder; the real producer re-stamps the envelope
			// at birth and the next read picks the unit up with its real edges.
			view.stubsSkipped++
			continue
		}
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
				view.claimedAt[s.ID] = t.Timestamp
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

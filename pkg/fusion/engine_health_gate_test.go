package fusion_test

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"testing"

	"github.com/c360studio/semstreams/graph"
	"github.com/c360studio/semstreams/pkg/fusion"
	"github.com/stretchr/testify/assert"
)

// TestFuse_GatesOnHealthNotCoverage pins the ADR-084 D1 reversal at fusion's top gate.
//
// Fuse used to hand-roll `!status.Ready` — the one exact-coverage check fusion never
// migrated to the canonical gate. Under continuous write that made a perfectly healthy
// graph return an empty envelope, and semsource's UI fell back to grep on a graph that
// could have answered. Fusion is a read path: it asks the HEALTH question and reports
// staleness rather than withholding evidence.
func TestFuse_GatesOnHealthNotCoverage(t *testing.T) {
	ent := entity("acme.ops.code.repo.symbol.OnEvent", "OnEvent", "on_event.go")

	fuseWith := func(t *testing.T, status fusion.IndexStatus) fusion.Response {
		t.Helper()
		g := &fakeGraph{
			status:   status,
			seeds:    map[string][]string{"OnEvent": {ent.ID}},
			entities: map[string]*fusion.Entity{ent.ID: ent},
		}
		eng := fusion.NewEngine(g, fusion.NewBodyResolver(fusion.MapStoreResolver{}))
		resp, err := eng.Fuse(context.Background(), fusion.Request{Query: "OnEvent"}, refLens{})
		if err != nil {
			t.Fatalf("Fuse: %v", err)
		}
		return resp
	}

	t.Run("healthy but lagging now serves", func(t *testing.T) {
		resp := fuseWith(t, fusion.IndexStatus{
			State: fusion.StateBuilding, BootstrapComplete: true,
			IndexedRevision: 40, TargetRevision: 100, Lag: 60, StalenessMs: 2500,
		})
		if len(resp.Nodes) == 0 {
			t.Fatal("a healthy index under write returned an empty envelope; the caller " +
				"would fall back to grep on a graph that could answer")
		}
		// The honesty envelope still tells the caller exactly how stale the answer is —
		// that is what makes serving it honest rather than sloppy.
		if resp.Index.StalenessMs != 2500 || resp.Index.Lag != 60 {
			t.Errorf("served envelope must carry the view age: got staleness=%d lag=%d",
				resp.Index.StalenessMs, resp.Index.Lag)
		}
		if resp.Index.Ready {
			t.Error("serving under lag must not claim Ready — coverage is reported, not faked")
		}
	})

	t.Run("an unbootstrapped index still withholds", func(t *testing.T) {
		// The gh#474 cutover: this is what the gate is FOR, and lag cannot distinguish
		// it — hence the wire bit.
		resp := fuseWith(t, fusion.IndexStatus{
			State: fusion.StateBuilding, BootstrapComplete: false,
			IndexedRevision: 98, TargetRevision: 100, Lag: 2, StalenessMs: 30,
		})
		assertEmptyHonest(t, resp)
	})

	for _, state := range []fusion.IndexState{fusion.StateDegraded, fusion.StateResetRequired} {
		t.Run(fmt.Sprintf("%s still withholds", state), func(t *testing.T) {
			resp := fuseWith(t, fusion.IndexStatus{State: state, BootstrapComplete: true})
			assertEmptyHonest(t, resp)
		})
	}
}

// TestFuse_StatusUnknownDefersButWiringFails pins ADR-084 D6: the two failures ADR-083
// had collapsed into one error now diverge, because they want different responses.
func TestFuse_StatusUnknownDefersButWiringFails(t *testing.T) {
	ent := entity("acme.ops.code.repo.symbol.OnEvent", "OnEvent", "on_event.go")
	newEngine := func(statusErr error) *fusion.Engine {
		g := &fakeGraph{
			statusErr: statusErr,
			seeds:     map[string][]string{"OnEvent": {ent.ID}},
			entities:  map[string]*fusion.Entity{ent.ID: ent},
		}
		return fusion.NewEngine(g, fusion.NewBodyResolver(fusion.MapStoreResolver{}))
	}

	t.Run("unknown readiness degrades to an honest empty envelope", func(t *testing.T) {
		eng := newEngine(fmt.Errorf("quiet feed: %w", fusion.ErrReadinessUnknown))
		resp, err := eng.Fuse(context.Background(), fusion.Request{Query: "OnEvent"}, refLens{})
		if err != nil {
			t.Fatalf("an unknown feed must degrade, not propagate: %v", err)
		}
		assertEmptyHonest(t, resp)
	})

	t.Run("a wiring failure stays loud", func(t *testing.T) {
		// Deliberately NOT degraded: broken wiring does not heal, and reporting it as
		// "the graph is busy" would let a misconfigured deployment serve honest-looking
		// empty envelopes forever.
		wiring := errors.New("transport cannot watch GRAPH_STATUS")
		eng := newEngine(wiring)
		if _, err := eng.Fuse(context.Background(), fusion.Request{Query: "OnEvent"}, refLens{}); !errors.Is(err, wiring) {
			t.Fatalf("a wiring failure must propagate; got %v", err)
		}
	})

	t.Run("there is no ungated escape", func(t *testing.T) {
		// Deliberate asymmetry with graph/query's allow_ungated_reads: that flag is for
		// a standalone deployment reading its own bucket, while fusion is a shared
		// product surface whose empty answer other people act on. If an escape is ever
		// added, this test is where the decision must be re-argued.
		eng := newEngine(fmt.Errorf("never published: %w", fusion.ErrReadinessUnknown))
		resp, err := eng.Fuse(context.Background(), fusion.Request{Query: "OnEvent"}, refLens{})
		if err != nil {
			t.Fatalf("Fuse: %v", err)
		}
		if len(resp.Nodes) != 0 {
			t.Error("fusion served nodes on unverifiable readiness — no config may enable this")
		}
	})
}

func assertEmptyHonest(t *testing.T, resp fusion.Response) {
	t.Helper()
	// The envelope must stay inside the closed state set. An empty State reads
	// downstream as an unrecognized phase rather than as a defer, and it would render
	// as all-zeros in the one-hot readiness metric — a silent "no data" where an
	// alertable signal belongs.
	assert.Contains(t, []fusion.IndexState{
		fusion.StateBuilding, fusion.StateDegraded, fusion.StateResetRequired, fusion.StateReady,
	}, resp.Index.State, "deferred envelope carried an out-of-set State %q", resp.Index.State)
	if len(resp.Nodes) != 0 {
		t.Errorf("expected an empty-honest envelope, got %d nodes", len(resp.Nodes))
	}
	if len(resp.Misses) != 0 {
		t.Errorf("a defer must not synthesize a miss — a miss claims the graph looked and "+
			"found nothing, which is exactly the absence claim it cannot make; got %+v", resp.Misses)
	}
	if resp.Index.Ready {
		t.Error("a deferred envelope must not read Ready")
	}
}

// TestIndexStatus_GateProjectionCarriesEveryField is the self-defending half of the
// converter fusion.IndexStatus -> graph.IndexStatusResponse.
//
// The two structs are field-identical by contract, and a hand-copied remap between them
// has silently dropped fields before (IndexedRevision/Lag, which read downstream as a
// false caught-up). Here a dropped field would be worse than cosmetic: this projection
// is the GATE INPUT, so forgetting to copy BootstrapComplete or State would change
// whether fusion serves at all, with both structs still looking correct in isolation.
//
// Comparing marshalled JSON rather than listing fields is the point — a field added to
// both structs but forgotten in the converter fails here without anyone remembering to
// extend the test.
func TestIndexStatus_GateProjectionCarriesEveryField(t *testing.T) {
	// Every field non-zero and distinct, so a copy that crosses two fields or drops one
	// cannot coincidentally match.
	src := fusion.IndexStatus{
		Ready: true, State: fusion.StateDegraded,
		Code: "some_code", Reason: "some_reason",
		BootstrapComplete: true,
		IndexedRevision:   11, TargetRevision: 22, Lag: 33, StalenessMs: 44,
		Phase: "indexing", Revision: "11", LastSynced: "2026-07-20T12:00:00Z",
	}

	projected := fusion.ExportReadinessEnvelope(src)

	srcJSON := marshalForCompare(t, src)
	projectedJSON := marshalForCompare(t, projected)
	if srcJSON != projectedJSON {
		t.Errorf("the gate projection lost or altered a field:\n fusion: %s\n  graph: %s",
			srcJSON, projectedJSON)
	}

	// And the fields the GATE actually reads, asserted by name so a failure says which
	// decision broke rather than just "json differs".
	if projected.State != string(fusion.StateDegraded) {
		t.Errorf("State = %q — the hard-stop check would misread it", projected.State)
	}
	if !projected.BootstrapComplete {
		t.Error("BootstrapComplete dropped — every healthy index would defer as mid-cutover")
	}
	if !projected.Ready || projected.StalenessMs != 44 {
		t.Error("the coverage fast path and staleness comparison lost their inputs")
	}
	// Belt and braces: the projection must satisfy the gate the same way the real
	// envelope does.
	if _, reason := graph.EvaluateReadinessGate(
		graph.StatusReading{Status: projected, Fresh: true}, graph.FreshnessNone()); reason != graph.DeferHardStop {
		t.Errorf("gate reason = %q, want hard_stop for a degraded projection", reason)
	}
}

// marshalForCompare renders a value as JSON for field-level comparison between the two
// field-identical envelope structs.
func marshalForCompare(t *testing.T, v any) string {
	t.Helper()
	b, err := json.Marshal(v)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	return string(b)
}

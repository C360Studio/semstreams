package fusion_test

import (
	"context"
	"errors"
	"testing"

	"github.com/c360studio/semstreams/graph"
	"github.com/c360studio/semstreams/pkg/errs"
	"github.com/c360studio/semstreams/pkg/fusion"
)

// indexNotReady builds the classified readiness transient a graph read emits when the
// serving index is not SOUND — its watcher is unavailable, it is degraded by an
// unresolved required write, or its initial build has not completed
// (graph.ErrorCodeIndexNotReady), mirroring processor/graph-index/query.go and
// graph/query/client.go.
//
// ADR-084 narrowed this: the transient no longer fires on ordinary catch-up lag, so the
// window in which Fuse's internal reads hit it is now a health window rather than a
// load window. The DEGRADE behavior under test is unchanged and matters more, not less
// — the transient is rarer now, which makes handling it inconsistently harder to catch
// in the wild.
func indexNotReady() error {
	return errs.ClassifiedCode(errs.ErrorTransient, graph.ErrorCodeIndexNotReady,
		errors.New("index not ready: initial build has not completed"))
}

// TestEngine_ReadinessTransient_Degrades: the semsource shape. The top Ready gate
// passes, then an internal read (Resolve / Entities / neighbor expansion) hits the
// classified ErrorCodeIndexNotReady transient. Fuse MUST degrade to the empty-honest
// envelope (Ready=false, no error) — identical to its top-level !Ready gate — rather
// than propagating a hard error. This is the "passes 5/5 alone, fails under
// full-suite load" bug: the same function degraded here but errored there.
func TestEngine_ReadinessTransient_Degrades(t *testing.T) {
	seed := entity("acme.ops.code.repo.symbol.OnEvent", "OnEvent", "on_event.go")

	cases := map[string]struct {
		graph *fakeGraph
		req   fusion.Request
	}{
		"resolve transient": {
			graph: &fakeGraph{status: readyStatus(), resolveErr: indexNotReady()},
			req:   fusion.Request{Query: "OnEvent"},
		},
		"entities transient": {
			graph: &fakeGraph{
				status: readyStatus(),
				seeds:  map[string][]string{"OnEvent": {seed.ID}},
				entErr: indexNotReady(),
			},
			req: fusion.Request{Query: "OnEvent"},
		},
		"neighbor-expansion transient": {
			graph: &fakeGraph{
				status:       readyStatus(),
				seeds:        map[string][]string{"OnEvent": {seed.ID}},
				entities:     map[string]*fusion.Entity{seed.ID: seed},
				neighborsErr: indexNotReady(),
			},
			req: fusion.Request{Query: "OnEvent", Want: []fusion.Want{fusion.WantRelations}},
		},
	}

	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			eng := fusion.NewEngine(tc.graph, fusion.NewBodyResolver(fusion.MapStoreResolver{}))

			resp, err := eng.Fuse(context.Background(), tc.req, refLens{})
			if err != nil {
				t.Fatalf("readiness transient must degrade, not propagate; got err %v", err)
			}
			if resp.Index.Ready {
				t.Error("expected a not-ready (Ready=false) envelope on the readiness transient")
			}
			if len(resp.Nodes) != 0 || len(resp.Misses) != 0 {
				t.Errorf("degrade must yield no nodes and no misses, got %d nodes / %d misses",
					len(resp.Nodes), len(resp.Misses))
			}
			if resp.ContractVersion != fusion.ContractVersion {
				t.Errorf("ContractVersion = %q, want %q (same shape as the top-gate degrade)",
					resp.ContractVersion, fusion.ContractVersion)
			}
			if resp.Provenance != fusion.ProvenanceDeterministic {
				t.Errorf("Provenance = %q, want ProvenanceDeterministic (same shape as the top-gate degrade)",
					resp.Provenance)
			}
			if resp.Index.State == fusion.StateReady {
				t.Error("a Ready=false envelope must not carry State=ready")
			}
		})
	}
}

// TestEngine_GenuineError_Propagates: a NON-transient (non-IndexNotReady) internal
// read error is a real backend fault and MUST still propagate as an error — never
// degrade to an empty envelope (that would mask a real failure as "not ready").
func TestEngine_GenuineError_Propagates(t *testing.T) {
	cases := map[string]*fakeGraph{
		"resolve genuine error": {status: readyStatus(), resolveErr: errors.New("graph down")},
		"entities genuine error": {
			status: readyStatus(),
			seeds:  map[string][]string{"OnEvent": {"acme.ops.code.repo.symbol.OnEvent"}},
			entErr: errors.New("batch down"),
		},
	}
	for name, g := range cases {
		t.Run(name, func(t *testing.T) {
			eng := fusion.NewEngine(g, fusion.NewBodyResolver(fusion.MapStoreResolver{}))
			if _, err := eng.Fuse(context.Background(), fusion.Request{Query: "OnEvent"}, refLens{}); err == nil {
				t.Error("expected a genuine backend error to propagate, not degrade to an empty envelope")
			}
		})
	}
}

// TestEngine_NeighborGenuineError_Swallowed: a NON-readiness Neighbors error keeps
// today's best-effort behavior — the node still ships with its relations omitted,
// and the fuse does NOT fail. Only the readiness transient degrades the whole
// response; unrelated neighbor faults stay swallowed (scope preserved).
func TestEngine_NeighborGenuineError_Swallowed(t *testing.T) {
	seed := entity("acme.ops.code.repo.symbol.OnEvent", "OnEvent", "on_event.go")
	g := &fakeGraph{
		status:       readyStatus(),
		seeds:        map[string][]string{"OnEvent": {seed.ID}},
		entities:     map[string]*fusion.Entity{seed.ID: seed},
		neighborsErr: errors.New("neighbors backend down"),
	}
	eng := fusion.NewEngine(g, fusion.NewBodyResolver(fusion.MapStoreResolver{}))

	resp, err := eng.Fuse(context.Background(), fusion.Request{Query: "OnEvent", Want: []fusion.Want{fusion.WantRelations}}, refLens{})
	if err != nil {
		t.Fatalf("a non-readiness neighbor fault must NOT fail the fuse, got %v", err)
	}
	if !resp.Index.Ready {
		t.Error("expected the ready envelope to hold (a swallowed neighbor fault is not a readiness degrade)")
	}
	if len(resp.Nodes) != 1 {
		t.Fatalf("expected the node to still ship with relations omitted, got %d nodes", len(resp.Nodes))
	}
	if len(resp.Nodes[0].Relations) != 0 {
		t.Errorf("expected relations omitted on a swallowed neighbor fault, got %+v", resp.Nodes[0].Relations)
	}
}

// TestEngine_ReadinessTransient_ReSamplesCurrentStatus: the degrade envelope carries
// the CURRENT status (re-fetched after the transient), so Lag / IndexedRevision are
// accurate for the caller's self-serve bounded decision — not a stale copy of the
// top-gate status. statusFn returns Ready=true on the first (top-gate) call and a
// not-ready status with real lag on the re-sample.
func TestEngine_ReadinessTransient_ReSamplesCurrentStatus(t *testing.T) {
	g := &fakeGraph{
		resolveErr: indexNotReady(),
		statusFn: func(call int) (fusion.IndexStatus, error) {
			if call == 1 {
				return readyStatus(), nil
			}
			return fusion.IndexStatus{
				Ready: false, State: fusion.StateBuilding,
				IndexedRevision: 10, TargetRevision: 15, Lag: 5,
			}, nil
		},
	}
	eng := fusion.NewEngine(g, fusion.NewBodyResolver(fusion.MapStoreResolver{}))

	resp, err := eng.Fuse(context.Background(), fusion.Request{Query: "OnEvent"}, refLens{})
	if err != nil {
		t.Fatalf("Fuse: %v", err)
	}
	if resp.Index.Ready {
		t.Error("expected Ready=false after the readiness transient")
	}
	if resp.Index.Lag != 5 || resp.Index.IndexedRevision != 10 || resp.Index.TargetRevision != 15 {
		t.Errorf("envelope must carry the re-sampled current status; got Lag=%d IndexedRevision=%d TargetRevision=%d, want 5/10/15",
			resp.Index.Lag, resp.Index.IndexedRevision, resp.Index.TargetRevision)
	}
}

// TestEngine_ReadinessTransient_ReSampleFailure_FallsBack: when the post-transient
// status re-fetch itself fails, Fuse falls back to the top-gate status but forces
// Ready=false (a transient is proof the index is not ready) — never a Ready=true
// envelope, and never a propagated error.
func TestEngine_ReadinessTransient_ReSampleFailure_FallsBack(t *testing.T) {
	g := &fakeGraph{
		resolveErr: indexNotReady(),
		statusFn: func(call int) (fusion.IndexStatus, error) {
			if call == 1 {
				return readyStatus(), nil
			}
			return fusion.IndexStatus{}, errors.New("status unreachable")
		},
	}
	eng := fusion.NewEngine(g, fusion.NewBodyResolver(fusion.MapStoreResolver{}))

	resp, err := eng.Fuse(context.Background(), fusion.Request{Query: "OnEvent"}, refLens{})
	if err != nil {
		t.Fatalf("a re-sample failure must still degrade, not propagate; got %v", err)
	}
	if resp.Index.Ready {
		t.Error("expected Ready forced false even when the status re-sample failed")
	}
}

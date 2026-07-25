package graphclustering

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"log/slog"
	"testing"

	"github.com/c360studio/semstreams/graph"
	"github.com/c360studio/semstreams/graph/clustering"
	"github.com/c360studio/semstreams/graph/inference"
	"github.com/c360studio/semstreams/graph/readiness"
	"github.com/c360studio/semstreams/pkg/errs"
	"github.com/nats-io/nats.go"
	"github.com/prometheus/client_golang/prometheus/testutil"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// This file covers B2 §4/§5 at the component seam: the embedding-readiness gate's
// three-row table, the per-cycle provider toggle + semantic_edges_applied signal,
// the readiness-aware fail-open wrapper's classification, and default-off
// preservation.

// --- §5.1: the readiness-aware fail-open wrapper (semanticFinderAdapter) --------

// fakeClassifiedFinder is a classifiedSimilarityFinder double: it returns a fixed
// (results, err) so the adapter's error policy is testable without live NATS.
type fakeClassifiedFinder struct {
	results []inference.SimilarityResult
	err     error
	calls   int
}

func (f *fakeClassifiedFinder) findSimilarClassified(_ context.Context, _ string, _ float64, _ int) ([]inference.SimilarityResult, error) {
	f.calls++
	return f.results, f.err
}

func TestSemanticFinderAdapter_ClassifiesNotReadyVsGenuineEmpty(t *testing.T) {
	ctx := context.Background()

	t.Run("classified index-not-ready maps to the abort sentinel", func(t *testing.T) {
		// The exact classification graph-embedding's ensureBootstrapReady stamps
		// (transient + index_not_ready). It must NOT be swallowed into an empty set.
		finder := &fakeClassifiedFinder{err: errs.ClassifiedCode(
			errs.ErrorTransient, graph.ErrorCodeIndexNotReady,
			errors.New("embedding index not ready: bootstrap still validating"))}
		adapter := semanticFinderAdapter{finder: finder}

		ids, err := adapter.SimilarNeighbors(ctx, "o.p.d.s.t.a", 0.75, 8)
		require.Error(t, err)
		assert.Nil(t, ids)
		assert.ErrorIs(t, err, clustering.ErrSemanticIndexNotReady,
			"a not-ready transient must map to the abort-not-latch sentinel, never a genuine empty")
	})

	t.Run("producer-wide FATAL reset maps to the abort sentinel", func(t *testing.T) {
		// graph-embedding's ensureBootstrapReady stamps FATAL graph_state_reset_required
		// on a sticky producer-wide reset. It is NOT the transient index_not_ready code,
		// so the pre-fix adapter swallowed it into an empty result — and a build that
		// latched cacheInitialized off that empty answer stayed permanently hollow across
		// a graph-embedding restart (Codex P1#3). It must ABORT, not fail-open empty.
		finder := &fakeClassifiedFinder{err: errs.ClassifiedCode(
			errs.ErrorFatal, graph.ErrorCodeGraphStateResetRequired,
			errors.New("graph state reset required"))}
		adapter := semanticFinderAdapter{finder: finder}

		ids, err := adapter.SimilarNeighbors(ctx, "o.p.d.s.t.a", 0.75, 8)
		require.Error(t, err)
		assert.Nil(t, ids)
		assert.ErrorIs(t, err, clustering.ErrSemanticIndexNotReady,
			"a producer-wide fatal reset must abort-not-latch, never a hollow per-entity empty")
	})

	t.Run("genuine handler miss fails open to empty", func(t *testing.T) {
		// A different classified error (e.g. this entity has no embedding) is "asked,
		// got nothing" — fail-open to empty at THIS site (matching anomaly's per-entity
		// tolerance), NOT the abort sentinel.
		finder := &fakeClassifiedFinder{err: errs.ClassifiedCode(
			errs.ErrorInvalid, "no_embedding", errors.New("source entity has no embedding"))}
		adapter := semanticFinderAdapter{finder: finder}

		ids, err := adapter.SimilarNeighbors(ctx, "o.p.d.s.t.a", 0.75, 8)
		require.NoError(t, err, "a genuine miss is not an error at the edge site")
		assert.Empty(t, ids)
	})

	t.Run("a plain transient without the code fails open, not abort", func(t *testing.T) {
		// A generic transport blip that is NOT the index-not-ready code must not be
		// mistaken for a cold index (that would wrongly abort the whole build).
		finder := &fakeClassifiedFinder{err: errs.WrapTransient(
			errors.New("connection reset"), "x", "y", "z")}
		adapter := semanticFinderAdapter{finder: finder}

		ids, err := adapter.SimilarNeighbors(ctx, "o.p.d.s.t.a", 0.75, 8)
		require.NoError(t, err)
		assert.Empty(t, ids)
	})

	t.Run("genuine results project to entity IDs", func(t *testing.T) {
		finder := &fakeClassifiedFinder{results: []inference.SimilarityResult{
			{EntityID: "o.p.d.s.t.b", Similarity: 0.9},
			{EntityID: "o.p.d.s.t.c", Similarity: 0.8},
		}}
		adapter := semanticFinderAdapter{finder: finder}

		ids, err := adapter.SimilarNeighbors(ctx, "o.p.d.s.t.a", 0.75, 8)
		require.NoError(t, err)
		assert.Equal(t, []string{"o.p.d.s.t.b", "o.p.d.s.t.c"}, ids)
	})

	t.Run("empty result set is a genuine empty, not an error", func(t *testing.T) {
		finder := &fakeClassifiedFinder{results: nil}
		adapter := semanticFinderAdapter{finder: finder}

		ids, err := adapter.SimilarNeighbors(ctx, "o.p.d.s.t.a", 0.75, 8)
		require.NoError(t, err, "asked, no semantic neighbors is not an error")
		assert.Empty(t, ids)
	})
}

// isEmbeddingIndexNotReady must key on the CLASSIFICATION + the stable code, never
// message text — and only the transient index_not_ready shape qualifies.
func TestIsEmbeddingIndexNotReady_ClassificationOnly(t *testing.T) {
	tests := []struct {
		name string
		err  error
		want bool
	}{
		{"transient + index_not_ready -> yes",
			errs.ClassifiedCode(errs.ErrorTransient, graph.ErrorCodeIndexNotReady, errors.New("cold")), true},
		{"fatal + index_not_ready -> no (a hard stop, not retry-next-tick)",
			errs.ClassifiedCode(errs.ErrorFatal, graph.ErrorCodeIndexNotReady, errors.New("reset")), false},
		{"transient + other code -> no",
			errs.ClassifiedCode(errs.ErrorTransient, graph.ErrorCodeGraphStateResetRequired, errors.New("x")), false},
		{"plain transient, no code -> no",
			errs.WrapTransient(errors.New("timeout"), "a", "b", "c"), false},
		{"a message that merely says 'not ready' -> no (no text matching)",
			errs.ClassifiedCode(errs.ErrorInvalid, "", errors.New("index_not_ready: not ready")), false},
		{"nil -> no", nil, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, isEmbeddingIndexNotReady(tt.err))
		})
	}
}

// isProducerWideEmbeddingFault is the ABORT-not-latch classifier (Codex P1#3): the
// transient index_not_ready shape OR any FATAL classified error (of which the reset
// code is the concrete producer-wide instance) aborts; a per-entity ErrorInvalid miss
// or a lone transient blip does not. Keyed on class/code only, never message text.
func TestIsProducerWideEmbeddingFault_ClassOrCode(t *testing.T) {
	tests := []struct {
		name string
		err  error
		want bool
	}{
		{"transient + index_not_ready -> abort",
			errs.ClassifiedCode(errs.ErrorTransient, graph.ErrorCodeIndexNotReady, errors.New("cold")), true},
		{"fatal + graph_state_reset_required -> abort",
			errs.ClassifiedCode(errs.ErrorFatal, graph.ErrorCodeGraphStateResetRequired, errors.New("reset")), true},
		{"fatal, any code -> abort (fatal class is producer-wide)",
			errs.ClassifiedCode(errs.ErrorFatal, "storage_unavailable", errors.New("no storage")), true},
		{"invalid per-entity miss -> NOT abort (fail-open empty)",
			errs.ClassifiedCode(errs.ErrorInvalid, "no_embedding", errors.New("no embedding for entity")), false},
		{"plain transient blip -> NOT abort (per-entity fail-open)",
			errs.WrapTransient(errors.New("connection reset"), "a", "b", "c"), false},
		{"no-responders (producer crashed mid-build) -> abort",
			nats.ErrNoResponders, true},
		{"raw fatal-TEXT error -> NOT abort (structural class check, not message text)",
			errors.New("fatal: disk corrupted"), false},
		{"nil -> NOT abort", nil, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, isProducerWideEmbeddingFault(tt.err))
		})
	}
}

// --- §4.2: the readiness-gate table (the three design rows) ---------------------

// readyIndex / coldIndex are the two index-axis inputs; the embedding axis reuses
// the same envelope shapes on graph-embedding's key.
func healthyStatus() graph.IndexStatusResponse {
	return graph.IndexStatusResponse{State: graph.IndexStateReady, BootstrapComplete: true, Ready: true}
}
func buildingStatus() graph.IndexStatusResponse {
	return graph.IndexStatusResponse{State: graph.IndexStateBuilding, BootstrapComplete: false}
}

// TestSemanticReadinessGate_ThreeRows drives the exact composition evaluateReadiness
// performs: the index axis via graph.EvaluateReadinessGate, the semantic axis via
// semanticActiveFromReading (gated by enable). Each row is a row of design.md's
// table.
func TestSemanticReadinessGate_ThreeRows(t *testing.T) {
	tests := []struct {
		name               string
		index              readiness.Reading
		embedding          readiness.Reading
		semanticEnabled    bool
		wantProceed        bool // index axis: does the whole cycle run?
		wantSemanticActive bool // semantic axis: does the tier apply?
	}{
		{
			name:        "index not ready -> defer the whole cycle (embedding irrelevant)",
			index:       readiness.Reading{Fresh: true, Known: true, Status: buildingStatus()},
			embedding:   readiness.Reading{Fresh: true, Known: true, Status: healthyStatus()},
			wantProceed: false,
		},
		{
			name:               "index ready + embeddings NOT ready + enabled -> structural-only",
			index:              readiness.Reading{Fresh: true, Known: true, Status: healthyStatus()},
			embedding:          readiness.Reading{Fresh: true, Known: true, Status: buildingStatus()},
			semanticEnabled:    true,
			wantProceed:        true,
			wantSemanticActive: false,
		},
		{
			name:               "index ready + embeddings unknown (never received) + enabled -> structural-only",
			index:              readiness.Reading{Fresh: true, Known: true, Status: healthyStatus()},
			embedding:          readiness.Reading{}, // !Fresh -> unknown -> fail closed
			semanticEnabled:    true,
			wantProceed:        true,
			wantSemanticActive: false,
		},
		{
			name:               "both ready + enabled -> full cycle (semantic applies)",
			index:              readiness.Reading{Fresh: true, Known: true, Status: healthyStatus()},
			embedding:          readiness.Reading{Fresh: true, Known: true, Status: healthyStatus()},
			semanticEnabled:    true,
			wantProceed:        true,
			wantSemanticActive: true,
		},
		{
			name:               "both ready but tier DISABLED -> full cycle, semantic n/a (never active)",
			index:              readiness.Reading{Fresh: true, Known: true, Status: healthyStatus()},
			embedding:          readiness.Reading{Fresh: true, Known: true, Status: healthyStatus()},
			semanticEnabled:    false,
			wantProceed:        true,
			wantSemanticActive: false,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			proceed, _ := graph.EvaluateReadinessGate(
				graph.StatusReading{Status: tt.index.Status, Fresh: tt.index.Fresh})
			assert.Equal(t, tt.wantProceed, proceed, "index axis (proceed/defer)")

			// The semantic axis as evaluateReadiness composes it: gated by enable,
			// then the pure reading -> verdict.
			gotSemantic := tt.semanticEnabled && semanticActiveFromReading(tt.embedding)
			assert.Equal(t, tt.wantSemanticActive, gotSemantic, "semantic axis (apply/structural-only)")
		})
	}
}

// --- §4.2: the toggle + the semantic_edges_applied signal -----------------------

type fakeBaseProvider struct{}

func (fakeBaseProvider) GetAllEntityIDs(context.Context) ([]string, error) { return nil, nil }
func (fakeBaseProvider) GetNeighbors(context.Context, string, string) ([]string, error) {
	return nil, nil
}
func (fakeBaseProvider) GetEdgeWeight(context.Context, string, string) (float64, error) {
	return 0, nil
}

type fakeNeighborFinder struct{}

func (fakeNeighborFinder) SimilarNeighbors(context.Context, string, float64, int) ([]string, error) {
	return nil, nil
}

// oneEntityBase is a base provider with a single entity, so a wrapped
// SemanticEdgeProvider's ensureCache actually queries its finder (an empty base would
// build a trivially-complete empty cache and never exercise the abort path).
type oneEntityBase struct{}

func (oneEntityBase) GetAllEntityIDs(context.Context) ([]string, error) {
	return []string{"o.p.d.s.t.a"}, nil
}
func (oneEntityBase) GetNeighbors(context.Context, string, string) ([]string, error) {
	return nil, nil
}
func (oneEntityBase) GetEdgeWeight(context.Context, string, string) (float64, error) {
	return 0, nil
}

// abortingFinder always returns the not-ready sentinel, modelling an embedding index
// that went cold past the §4 gate: a build over it ABORTS and latches structural-only.
type abortingFinder struct{}

func (abortingFinder) SimilarNeighbors(context.Context, string, float64, int) ([]string, error) {
	return nil, fmt.Errorf("%w: embedding index cold mid-build", clustering.ErrSemanticIndexNotReady)
}

// enabledSemanticComponent builds a component whose semantic tier is enabled and
// whose provider handle is a real SemanticEdgeProvider, so applySemanticGate has
// something to toggle.
func enabledSemanticComponent(t *testing.T) (*Component, *clustering.SemanticEdgeProvider) {
	t.Helper()
	c, _ := newLoggedComponent(t, Config{
		Ports:         basePorts(),
		SemanticEdges: &SemanticEdgesConfig{EnableSemanticEdges: true},
	})
	require.True(t, c.config.semanticEdges.enabled)
	eidp := clustering.NewEntityIDProvider(fakeBaseProvider{}, clustering.DefaultEntityIDProviderConfig(), nil)
	sep := clustering.NewSemanticEdgeProvider(eidp, fakeNeighborFinder{},
		clustering.WeightConfig{}, clustering.SemanticEdgeParams{}, nil)
	c.semanticProvider = sep
	return c, sep
}

func TestApplySemanticGate_TogglesProviderAndStampsSignal(t *testing.T) {
	c, sep := enabledSemanticComponent(t)

	// One cycle = applySemanticGate (preflight toggle, BEFORE detection) then
	// recordSemanticMode (stamp the #618 signal from the ACTUAL completed mode, AFTER
	// detection — Codex P1#2). The fake finder never aborts, so here actual == intent.

	// Active cycle: provider active, gauge = 1, an INFO transition line.
	_, bufActive := loggerBufFor(t, c)
	c.applySemanticGate(gateDecision{proceed: true, semanticActive: true})
	assert.True(t, sep.IsActive(), "an active verdict must activate the provider")
	c.recordSemanticMode()
	assert.Equal(t, float64(1), testutil.ToFloat64(c.metrics.semanticEdgesApplied),
		"semantic_edges_applied must read 1 on a full cycle")
	assert.Contains(t, bufActive.String(), `"semantic_edges_applied":true`)

	// Structural-only cycle: provider inactive, gauge = 0, a WARN transition line
	// (the #618 semantically-blind signal, visible at default level).
	_, bufStructural := loggerBufFor(t, c)
	c.applySemanticGate(gateDecision{proceed: true, semanticActive: false})
	assert.False(t, sep.IsActive(), "a not-ready verdict must deactivate the provider (structural-only)")
	c.recordSemanticMode()
	assert.Equal(t, float64(0), testutil.ToFloat64(c.metrics.semanticEdgesApplied),
		"semantic_edges_applied must read 0 on a structural-only cycle")
	structural := bufStructural.String()
	assert.Contains(t, structural, `"level":"WARN"`, "a semantically-blind cycle must be visible without raising the log level")
	assert.Contains(t, structural, `"semantic_edges_applied":false`)
	assert.Contains(t, structural, "STRUCTURAL-ONLY")
}

// TestRecordSemanticMode_ReportsActualNotIntent is the direct Codex P1#2b pin at the
// component seam: when an intended-ACTIVE cycle aborts mid-build (embedding went cold
// past the gate), recordSemanticMode must stamp semantic_edges_applied=0 and the WARN
// transition — the ACTUAL structural-only mode — even though applySemanticGate set the
// provider active. Before the split, applySemanticGate stamped the preflight intent (1)
// and this degraded cycle would have reported a false "semantics applied".
func TestRecordSemanticMode_ReportsActualNotIntent(t *testing.T) {
	c, _ := newLoggedComponent(t, Config{
		Ports:         basePorts(),
		SemanticEdges: &SemanticEdgesConfig{EnableSemanticEdges: true},
	})
	require.True(t, c.config.semanticEdges.enabled)

	// A provider whose finder always reports the not-ready sentinel, over a base with
	// one entity, so a triggered build ABORTS and latches structural-only this cycle.
	eidp := clustering.NewEntityIDProvider(oneEntityBase{}, clustering.DefaultEntityIDProviderConfig(), nil)
	sep := clustering.NewSemanticEdgeProvider(eidp, abortingFinder{},
		clustering.WeightConfig{}, clustering.SemanticEdgeParams{}, nil)
	c.semanticProvider = sep

	// Preflight INTENT: active.
	c.applySemanticGate(gateDecision{proceed: true, semanticActive: true})
	require.True(t, sep.IsActive(), "the preflight verdict set the provider active")

	// Detection reads the provider and aborts the build (the finder is cold).
	_, err := sep.GetNeighbors(context.Background(), "o.p.d.s.t.a", "both")
	require.NoError(t, err, "an abort degrades to structural-only, it does not error")

	// ACTUAL mode after detection: structural-only. The signal must report 0, not the
	// intended 1.
	_, buf := loggerBufFor(t, c)
	c.recordSemanticMode()
	assert.Equal(t, float64(0), testutil.ToFloat64(c.metrics.semanticEdgesApplied),
		"an intended-active cycle that aborted must stamp semantic_edges_applied=0 (actual mode)")
	out := buf.String()
	assert.Contains(t, out, `"level":"WARN"`, "the degraded cycle is the #618 signal, at WARN")
	assert.Contains(t, out, `"semantic_edges_applied":false`)
	assert.Contains(t, out, "STRUCTURAL-ONLY")
}

// A steady state logs once per TRANSITION, not per tick. The transition log now lives
// on recordSemanticMode (the actual-mode stamp), so a full cycle is applySemanticGate
// + recordSemanticMode.
func TestApplySemanticGate_LogsOnlyOnTransition(t *testing.T) {
	c, _ := enabledSemanticComponent(t)

	_, buf := loggerBufFor(t, c)
	c.applySemanticGate(gateDecision{proceed: true, semanticActive: true})
	c.recordSemanticMode()
	assert.Contains(t, buf.String(), `"semantic_edges_applied":true`, "first cycle is a transition and must log")

	_, buf2 := loggerBufFor(t, c)
	c.applySemanticGate(gateDecision{proceed: true, semanticActive: true})
	c.recordSemanticMode()
	assert.NotContains(t, buf2.String(), "semantic_edges_applied",
		"a steady active state must not log a line per tick")
}

// Disabled or unwired: BOTH applySemanticGate and recordSemanticMode are no-ops — no
// panic on a nil provider, and neither touches the gauge or logs (the n/a row).
func TestApplySemanticGate_DisabledOrUnwired_NoOp(t *testing.T) {
	t.Run("tier disabled", func(t *testing.T) {
		c, buf := newLoggedComponent(t, Config{Ports: basePorts()})
		require.False(t, c.config.semanticEdges.enabled)
		c.applySemanticGate(gateDecision{proceed: true, semanticActive: true}) // must not panic
		c.recordSemanticMode()                                                 // must not panic
		assert.Empty(t, buf.String(), "a disabled tier must not log a semantic verdict")
	})

	t.Run("enabled but provider unwired (nil)", func(t *testing.T) {
		c, buf := newLoggedComponent(t, Config{
			Ports:         basePorts(),
			SemanticEdges: &SemanticEdgesConfig{EnableSemanticEdges: true},
		})
		c.semanticProvider = nil
		c.applySemanticGate(gateDecision{proceed: true, semanticActive: true}) // must not panic
		c.recordSemanticMode()                                                 // must not panic
		assert.Empty(t, buf.String())
	})
}

// --- default-off preservation ---------------------------------------------------

// With the tier OFF, evaluateReadiness needs no embedding watcher and yields
// semanticActive=false while the index gate behaves EXACTLY as before (an unknown
// index still fails closed to status_unknown).
func TestEvaluateReadiness_SemanticDisabled_NoEmbeddingAxis(t *testing.T) {
	c, _ := newLoggedComponent(t, Config{Ports: basePorts()})
	require.False(t, c.config.semanticEdges.enabled)
	require.Nil(t, c.embeddingStatusWatcher, "no embedding watcher is bound when the tier is off")

	got := c.evaluateReadiness()
	assert.False(t, got.proceed, "unknown index still fails closed, unchanged")
	assert.Equal(t, graph.DeferStatusUnknown, got.reason)
	assert.False(t, got.semanticActive, "the semantic axis is inert when the tier is disabled")
}

// loggerBufFor swaps the component's logger for a fresh buffer-backed one and
// returns it, so a single test can inspect one applySemanticGate call's output in
// isolation (slog.SetDefault is never touched; this stays local to c).
func loggerBufFor(t *testing.T, c *Component) (*Component, *bytes.Buffer) {
	t.Helper()
	buf := &bytes.Buffer{}
	c.logger = slog.New(slog.NewJSONHandler(buf, &slog.HandlerOptions{Level: slog.LevelDebug}))
	return c, buf
}

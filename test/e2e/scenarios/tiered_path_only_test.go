package scenarios

import (
	"slices"
	"testing"

	"github.com/stretchr/testify/require"
)

// gh#1117 — the semantic path-only run. PBT decision: not applied; the input is
// one fixed stage table crossed with a boolean, so named examples cover it
// (design § Skills, invariants, tests).

// semanticStagesBeforePathOnly is the semantic variant's stage list as it stood
// before this change (dumped at 8bae169b): with PathOnly unset the list must be
// exactly this, in this order.
var semanticStagesBeforePathOnly = []string{
	"verify-components",
	"send-mixed-data",
	"validate-processing",
	"wait-for-embeddings",
	"validate-embedding-queue-health",
	"wait-for-entity-stabilization",
	"graph-roundtrip",
	"validate-hierarchy-inference",
	"verify-entity-count",
	"verify-entity-retrieval",
	"validate-entity-structure",
	"verify-index-population",
	"test-pathrag-sensor",
	"test-pathrag-boundary",
	"test-pathrag-document",
	"test-entityid-hierarchy",
	"test-entities-by-prefix",
	"test-spatial-query",
	"test-temporal-query",
	"test-zone-relationships",
	"validate-llm-enhancement",
	"validate-thematic-answer-eval",
	"validate-partition-colocation",
	"test-nl-path-intent",
	"test-entity-by-alias",
	"test-predicate-list",
	"test-predicate-stats",
	"test-predicate-compound",
	"verify-search-quality",
	"test-http-gateway",
	"validate-gateway-response-shape",
	"test-embedding-fallback",
	"validate-community-structure",
	"validate-authoritative-hierarchy-provenance",
	"validate-incoming-index-predicates",
	"validate-bidirectional-traversal",
	"validate-inverse-edges-materialized",
	"validate-globalsearch-known-answer",
	"validate-batch-read-reconciliation",
	"validate-virtual-edges",
	"wait-for-rule-stabilization",
	"validate-rules",
	"validate-metrics",
	"verify-outputs",
}

func stageNames(stages []stage) []string {
	out := make([]string, 0, len(stages))
	for _, st := range stages {
		out = append(out, st.name)
	}
	return out
}

// spec: e2e-tiered-scenario / The semantic path-only run skips its declared quality stages and logs them as skipped
func TestPathOnlySkips_DeclaredSetIsExactlyTheThreeQualityRows(t *testing.T) {
	want := []string{"validate-llm-enhancement", "validate-thematic-answer-eval", "validate-globalsearch-known-answer"}
	got := make([]string, 0, len(pathOnlySkips))
	for name, reason := range pathOnlySkips {
		got = append(got, name)
		require.NotEmpty(t, reason, "%s is declared quality without a reason", name)
	}
	require.ElementsMatch(t, want, got, "pathOnlySkips must name exactly the three owner-ruled quality stages")

	// I1: every declared name is a real semantic row, so a rename cannot turn
	// the skip into a silent no-op.
	semantic := stageNames((&TieredScenario{}).getStagesForVariant("semantic"))
	for _, name := range want {
		require.Contains(t, semantic, name, "%s is declared quality but is not a semantic stage-table row", name)
	}
}

// spec: e2e-tiered-scenario / The semantic path-only run skips its declared quality stages and logs them as skipped
func TestPathOnlySkips_SemanticListLosesExactlyTheDeclaredRowsInOrder(t *testing.T) {
	full := (&TieredScenario{}).getStagesForVariant("semantic")
	require.Equal(t, semanticStagesBeforePathOnly, stageNames(full),
		"with PathOnly unset the semantic list must be unchanged (I3)")

	kept, skipped := withoutPathOnlySkips(full)

	require.Equal(t, []string{"validate-llm-enhancement", "validate-thematic-answer-eval",
		"validate-globalsearch-known-answer"}, skipped, "skipped names, in stage-table order")
	want := slices.DeleteFunc(slices.Clone(semanticStagesBeforePathOnly), func(n string) bool {
		_, declared := pathOnlySkips[n]
		return declared
	})
	require.Len(t, want, 41)
	require.Equal(t, want, stageNames(kept), "path-only list = full list minus the declared rows, order kept (I1)")
}

// spec: e2e-tiered-scenario / The semantic path-only run skips its declared quality stages and logs them as skipped
func TestPathOnlySkips_NonSemanticVariantsAreUnchanged(t *testing.T) {
	for _, variant := range []string{"structural", "statistical"} {
		full := (&TieredScenario{}).getStagesForVariant(variant)
		kept, skipped := withoutPathOnlySkips(full)
		require.Empty(t, skipped, "%s: no declared quality row runs there", variant)
		require.Equal(t, stageNames(full), stageNames(kept), "%s: list unchanged under PathOnly", variant)
	}
}

// spec: e2e-tiered-scenario / The semantic path-only run skips its declared quality stages and logs them as skipped
// The list Execute runs comes from stagesToRun, so this pins the PathOnly branch
// itself, not only the filter it calls.
func TestStagesToRun_PathOnlySelectsTheFilteredList(t *testing.T) {
	quality := []string{"validate-llm-enhancement", "validate-thematic-answer-eval", "validate-globalsearch-known-answer"}

	full := &TieredScenario{config: &TieredConfig{PathOnly: false}}
	stages, skipped := full.stagesToRun("semantic")
	require.Nil(t, skipped, "PathOnly unset skips nothing")
	require.Equal(t, semanticStagesBeforePathOnly, stageNames(stages), "PathOnly unset runs the full 44 (I3)")

	pathOnly := &TieredScenario{config: &TieredConfig{PathOnly: true}}
	stages, skipped = pathOnly.stagesToRun("semantic")
	require.Equal(t, quality, skipped, "PathOnly skips the declared rows, in stage-table order")
	want := slices.DeleteFunc(slices.Clone(semanticStagesBeforePathOnly), func(n string) bool {
		return slices.Contains(quality, n)
	})
	require.Len(t, stageNames(stages), 41)
	require.Equal(t, want, stageNames(stages), "PathOnly runs the other 41, order kept (I1)")
}

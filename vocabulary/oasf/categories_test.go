package oasf

import (
	"embed"
	"encoding/json"
	"path/filepath"
	"testing"
)

// testdata/categories/*.json are unmodified copies of the upstream
// AGNTCY OASF schema files at
// github.com/agntcy/oasf/tree/main/schema/skills/<name>/<name>.json,
// captured at the time PR-O1 landed. The TestCategoriesMatchUpstream
// test below asserts our Go constants line up with those snapshots so
// any future taxonomy change shows as a failing test rather than as a
// silent wire drift.
//
//go:embed testdata/categories/*.json
var categoryFixtures embed.FS

// upstreamCategory is the subset of the OASF base_skill JSON shape this
// test depends on. The upstream schema has more fields (attributes, etc.)
// but they do not participate in the wire contract we care about pinning.
type upstreamCategory struct {
	UID      uint32 `json:"uid"`
	Name     string `json:"name"`
	Caption  string `json:"caption"`
	Category bool   `json:"category"`
	Extends  string `json:"extends"`
}

// TestCategoriesMatchUpstream pins our category constants against the
// embedded snapshot of the published OASF taxonomy. If the upstream
// schema renumbers or renames a category, refresh the testdata file
// (see vocabulary/oasf/testdata/README.md, or run the regen script
// once it exists) and re-run.
func TestCategoriesMatchUpstream(t *testing.T) {
	for fileBase, wantID := range map[string]uint32{
		"natural_language_processing":    CategoryNLP,
		"analytical_skills":              CategoryAnalyticalSkills,
		"retrieval_augmented_generation": CategoryRAG,
		"security_privacy":               CategorySecurityPrivacy,
		"data_engineering":               CategoryDataEngineering,
		"agent_orchestration":            CategoryAgentOrchestration,
		"evaluation_monitoring":          CategoryEvaluationMonitoring,
		"governance_compliance":          CategoryGovernanceCompliance,
		"tool_interaction":               CategoryToolInteraction,
		"advanced_reasoning_planning":    CategoryAdvancedReasoningPlanning,
	} {
		t.Run(fileBase, func(t *testing.T) {
			path := filepath.Join("testdata", "categories", fileBase+".json")
			raw, err := categoryFixtures.ReadFile(path)
			if err != nil {
				t.Fatalf("read embedded fixture %s: %v", path, err)
			}
			var got upstreamCategory
			if err := json.Unmarshal(raw, &got); err != nil {
				t.Fatalf("unmarshal %s: %v", path, err)
			}

			if got.UID != wantID {
				t.Errorf("upstream uid for %s = %d, our constant = %d",
					fileBase, got.UID, wantID)
			}
			if got.Name != fileBase {
				t.Errorf("upstream name for %s = %q, want %q",
					fileBase, got.Name, fileBase)
			}
			if !got.Category {
				t.Errorf("upstream %s is not marked as a category — schema shape may have changed",
					fileBase)
			}
			if got.Extends != "base_skill" {
				t.Errorf("upstream %s extends = %q, want \"base_skill\" — taxonomy root may have moved",
					fileBase, got.Extends)
			}
			if got.Caption == "" {
				t.Errorf("upstream %s has empty caption — schema shape may have changed",
					fileBase)
			}
			// And: our local caption map matches upstream verbatim.
			if local := Caption(wantID); local != got.Caption {
				t.Errorf("local caption for %s = %q, upstream = %q",
					fileBase, local, got.Caption)
			}
		})
	}
}

// TestCategoryMapsAreInternallyConsistent guards against constant drift
// inside this package: every constant in categories.go must appear in
// both maps, and the IDs in the maps must equal the constants they key.
func TestCategoryMapsAreInternallyConsistent(t *testing.T) {
	ids := []uint32{
		CategoryNLP,
		CategoryAnalyticalSkills,
		CategoryRAG,
		CategorySecurityPrivacy,
		CategoryDataEngineering,
		CategoryAgentOrchestration,
		CategoryEvaluationMonitoring,
		CategoryGovernanceCompliance,
		CategoryToolInteraction,
		CategoryAdvancedReasoningPlanning,
	}
	if got := len(ids); got != 10 {
		t.Errorf("declared constants = %d, want 10 (ADR-042 MVP scope)", got)
	}
	if got := len(categoryNames); got != 10 {
		t.Errorf("len(categoryNames) = %d, want 10", got)
	}
	if got := len(categoryCaptions); got != 10 {
		t.Errorf("len(categoryCaptions) = %d, want 10", got)
	}
	for _, id := range ids {
		if _, ok := categoryNames[id]; !ok {
			t.Errorf("constant uid=%d missing from categoryNames", id)
		}
		if _, ok := categoryCaptions[id]; !ok {
			t.Errorf("constant uid=%d missing from categoryCaptions", id)
		}
	}
}

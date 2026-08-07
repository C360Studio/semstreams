package portgrammarcontrol

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestFrozenLegacyPlanAndDispositions(t *testing.T) {
	plan := loadRepositoryPlan(t)
	if len(plan.Items) != 646 || len(plan.ConfigItems()) != 522 || plan.MechanicalCount() != 448 ||
		len(plan.Dispositions) != 74 || len(plan.GoItems()) != 124 || plan.ConfigDocumentCount() != 24 ||
		plan.GoFileCount() != 34 || plan.GoSourceCount() != 41 {
		t.Fatalf("unexpected frozen counts: items=%d config=%d mechanical=%d dispositions=%d go=%d documents=%d files=%d sources=%d",
			len(plan.Items), len(plan.ConfigItems()), plan.MechanicalCount(), len(plan.Dispositions), len(plan.GoItems()),
			plan.ConfigDocumentCount(), plan.GoFileCount(), plan.GoSourceCount())
	}
	counts, err := plan.ValidateDispositions()
	if err != nil {
		t.Fatal(err)
	}
	if counts.KV != 57 || counts.KVRead != 9 || counts.HTTP != 8 || counts.Deleted != 2 {
		t.Fatalf("unexpected disposition counts: %+v", counts)
	}
	for id, disposition := range plan.Dispositions {
		disposition.TargetData = `{}`
		plan.Dispositions[id] = disposition
		break
	}
	if _, err := plan.ValidateDispositions(); err == nil {
		t.Fatal("mutated reviewed disposition was accepted")
	}
}

func TestCompletenessRejectsMissingExtraAndDuplicateItems(t *testing.T) {
	plan := loadRepositoryPlan(t)
	live := append([]WorkItem(nil), plan.Items...)
	if err := plan.ValidateAgainst(&Population{Items: live[1:]}); err == nil || !strings.Contains(err.Error(), "count changed") {
		t.Fatalf("missing item error = %v", err)
	}
	extra := live[0]
	extra.RecordID += ":extra"
	if err := plan.ValidateAgainst(&Population{Items: append(live, extra)}); err == nil || !strings.Contains(err.Error(), "count changed") {
		t.Fatalf("extra item error = %v", err)
	}
	if err := plan.ValidateAgainst(&Population{Items: append(live, live[0])}); err == nil || !strings.Contains(err.Error(), "duplicate record_id") {
		t.Fatalf("duplicate item error = %v", err)
	}
}

func TestRewriteUsesFrozenFixtureAndIsDeterministic(t *testing.T) {
	root, plan := syntheticRewritePlan(t)
	sourcePath := filepath.Join(root, filepath.FromSlash(plan.ConfigPaths()[0]))
	before, err := os.ReadFile(sourcePath)
	if err != nil {
		t.Fatal(err)
	}
	mechanical, err := Rewrite(root, "", plan, RewriteOptions{DryRun: true})
	if err != nil {
		t.Fatal(err)
	}
	if canonical, legacy := rewrittenRowCounts(t, mechanical); canonical != 1 || legacy != 4 {
		t.Fatalf("mechanical canonical/legacy=%d/%d, want 1/4", canonical, legacy)
	}

	outA, outB := t.TempDir(), t.TempDir()
	first, err := Rewrite(root, outA, plan, RewriteOptions{ApplyDispositions: true})
	if err != nil {
		t.Fatal(err)
	}
	second, err := Rewrite(root, outB, plan, RewriteOptions{ApplyDispositions: true})
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(MarshalOutputs(first), MarshalOutputs(second)) {
		t.Fatal("rewrite differs across output roots")
	}
	if canonical, legacy := rewrittenRowCounts(t, first); canonical != 4 || legacy != 0 {
		t.Fatalf("reviewed canonical/legacy=%d/%d, want 4/0", canonical, legacy)
	}
	if _, err := Rewrite(root, outA, plan, RewriteOptions{ApplyDispositions: true, Check: true}); err != nil {
		t.Fatalf("check deterministic output: %v", err)
	}
	after, err := os.ReadFile(sourcePath)
	if err != nil || !bytes.Equal(before, after) {
		t.Fatalf("source changed: err=%v", err)
	}
}

func TestRewriteRejectsUnsafeOutputRoots(t *testing.T) {
	root, plan := syntheticRewritePlan(t)
	if _, err := Rewrite(root, filepath.Join(root, "preview"), plan, RewriteOptions{}); err == nil || !strings.Contains(err.Error(), "overlaps repository") {
		t.Fatalf("repository overlap error = %v", err)
	}
	nonempty := t.TempDir()
	if err := os.WriteFile(filepath.Join(nonempty, "keep"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := Rewrite(root, nonempty, plan, RewriteOptions{}); err == nil || !strings.Contains(err.Error(), "must be empty") {
		t.Fatalf("nonempty output error = %v", err)
	}
	link := filepath.Join(t.TempDir(), "repo-link")
	if err := os.Symlink(root, link); err != nil {
		t.Skipf("symlink unavailable: %v", err)
	}
	if _, err := Rewrite(root, filepath.Join(link, "preview"), plan, RewriteOptions{}); err == nil || !strings.Contains(err.Error(), "overlaps repository") {
		t.Fatalf("canonical overlap error = %v", err)
	}
}

func syntheticRewritePlan(t *testing.T) (string, *Plan) {
	t.Helper()
	root := t.TempDir()
	path := filepath.Join(root, "configs", "fixture.json")
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	data := `{"components":{"graph-gateway":{"config":{"ports":{"inputs":[{"name":"events","type":"nats","subject":"events.>"},{"name":"http","type":"http","subject":":8080"}],"outputs":[{"name":"index","type":"kv","subject":"INDEX"}]} }},"graph-query":{"config":{"ports":{"kv_read":[{"name":"entity_states","type":"kv-read","bucket":"ENTITY_STATES"}]}}},"agentic-tools":{"config":{"ports":{"kv_read":[{"name":"entity_states","type":"kv-read","bucket":"ENTITY_STATES"}]}}}}}`
	if err := os.WriteFile(path, []byte(data), 0o644); err != nil {
		t.Fatal(err)
	}
	items, err := censusConfigs(root)
	if err != nil {
		t.Fatal(err)
	}
	plan := &Plan{Items: items, Dispositions: map[string]Disposition{}}
	for _, item := range items {
		var disposition Disposition
		switch item.CurrentKind {
		case "http":
			disposition = Disposition{item.RecordID, item.Path, item.Pointer, "rewrite", "inputs", "network", `{"host":"0.0.0.0","port":8080,"protocol":"http"}`, "reviewed-http-listener-network-protocol"}
		case "kv":
			disposition = Disposition{item.RecordID, item.Path, item.Pointer, "rewrite", "outputs", "kv-write", `{"bucket":"INDEX"}`, "reviewed-current-output-writes-kv-resource"}
		case "kv-read":
			if item.Enclosing == "graph-query" {
				disposition = Disposition{item.RecordID, item.Path, item.Pointer, "delete", "<deleted>", "<deleted>", `{"reason":"no-runtime-consumer"}`, "reviewed-dead-graph-query-entity-states-row"}
			} else {
				disposition = Disposition{item.RecordID, item.Path, item.Pointer, "rewrite", "inputs", "kv-read", `{"bucket":"ENTITY_STATES"}`, "reviewed-agentic-tools-exact-read-input"}
			}
		}
		if disposition.RecordID != "" {
			plan.Dispositions[item.RecordID] = disposition
		}
	}
	return root, plan
}

func rewrittenRowCounts(t *testing.T, outputs []Output) (canonical, legacy int) {
	t.Helper()
	for _, output := range outputs {
		var document any
		if err := json.Unmarshal(output.Data, &document); err != nil {
			t.Fatal(err)
		}
		countPortRows(document, "", &canonical, &legacy)
	}
	return canonical, legacy
}

func countPortRows(node any, parent string, canonical, legacy *int) {
	switch value := node.(type) {
	case map[string]any:
		for key, child := range value {
			countPortRows(child, key, canonical, legacy)
		}
	case []any:
		if _, lane := portLanes[parent]; lane {
			for _, raw := range value {
				row, _ := raw.(map[string]any)
				config, _ := row["config"].(map[string]any)
				if stringValue(config["kind"]) != "" {
					*canonical++
				} else if stringValue(row["type"]) != "" {
					*legacy++
				}
			}
			return
		}
		for _, child := range value {
			countPortRows(child, "", canonical, legacy)
		}
	}
}

func loadRepositoryPlan(t *testing.T) *Plan {
	t.Helper()
	plan, err := LoadPlan(repoRoot(t))
	if err != nil {
		t.Fatal(err)
	}
	return plan
}

func repoRoot(t *testing.T) string {
	t.Helper()
	root, err := filepath.Abs(filepath.Join("..", ".."))
	if err != nil {
		t.Fatal(err)
	}
	return root
}

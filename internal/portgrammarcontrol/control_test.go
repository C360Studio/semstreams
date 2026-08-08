package portgrammarcontrol

import (
	"bytes"
	"encoding/json"
	"path/filepath"
	"sort"
	"testing"
)

func TestFrozenMigrationRecordAndDispositions(t *testing.T) {
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

func TestTargetRendererPreservesBytesOutsidePortObjects(t *testing.T) {
	// port-grammar:legacy-fixture exercises migration of the retired shape.
	source := []byte("{\n  \"before\": {\"keep\": true},\n  \"components\": {\n    \"fixture\": {\n      \"config\": {\n        \"ports\": {\"inputs\": [{\"name\": \"events\", \"type\": \"nats\", \"subject\": \"events.>\"}]}\n      }\n    }\n  },\n  \"after\": [1, 2, 3]\n}\n")
	document, err := decodeJSON(source)
	if err != nil {
		t.Fatal(err)
	}
	portsPath := []string{"components", "fixture", "config", "ports"}
	ports := map[string]any{
		"inputs": []any{map[string]any{
			"name":   "events",
			"config": map[string]any{"kind": "nats", "subject": "events.>"},
		}},
	}
	if err := setPointer(document, portsPath, ports); err != nil {
		t.Fatal(err)
	}
	items := []WorkItem{{Pointer: "/components/fixture/config/ports/inputs/0"}}
	rewritten, err := renderRewrittenDocument(source, document, items)
	if err != nil {
		t.Fatal(err)
	}
	if got, want := bytesOutsidePortObjects(t, rewritten, items), bytesOutsidePortObjects(t, source, items); !bytes.Equal(got, want) {
		t.Fatal("target rewrite changed bytes outside the ledger-owned ports object")
	}
	var target any
	if err := json.Unmarshal(rewritten, &target); err != nil {
		t.Fatalf("rewritten target is not JSON: %v", err)
	}
	got, err := getPointer(target, append(portsPath, "inputs", "0", "config", "kind"))
	if err != nil || got != "nats" {
		t.Fatalf("canonical target kind = %v, err=%v", got, err)
	}
}

func bytesOutsidePortObjects(t *testing.T, data []byte, items []WorkItem) []byte {
	t.Helper()
	pointers := map[string][]string{}
	for _, item := range items {
		segments := splitPointer(item.Pointer)
		ports := segments[:len(segments)-2]
		pointers[jsonPointer(ports)] = ports
	}
	type span struct{ start, end int }
	spans := make([]span, 0, len(pointers))
	for _, pointer := range pointers {
		start, end, err := locateJSONPointerSpan(data, pointer)
		if err != nil {
			t.Fatal(err)
		}
		spans = append(spans, span{start, end})
	}
	sort.Slice(spans, func(i, j int) bool { return spans[i].start > spans[j].start })
	result := append([]byte(nil), data...)
	for _, current := range spans {
		result = append(result[:current.start], append([]byte("<ports>"), result[current.end:]...)...)
	}
	return result
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

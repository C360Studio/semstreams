package oasfgenerator

import (
	"encoding/json"
	"flag"
	"os"
	"path/filepath"
	"sort"
	"testing"
	"time"

	"github.com/c360studio/semstreams/message"
	agentic "github.com/c360studio/semstreams/vocabulary/agentic"
	"github.com/google/go-cmp/cmp"
)

var update = flag.Bool("update", false, "rewrite testdata golden files")

// TestOASFContract pins the OASF wire format that downstream AGNTCY consumers
// rely on (directory registration body, A2A agent-card derivation). It exercises
// every predicate the mapper handles and compares the marshalled record against
// a checked-in golden file.
//
// If this test fails, either:
//   - the upstream OASF schema has changed (update golden + CurrentSchemaVersion),
//   - a SemStreams predicate has been renamed (update mapper + golden),
//   - the mapper grew/lost a field (intentional changes need golden update).
//
// Update the golden by running: go test ./processor/oasf-generator/ -run TestOASFContract -update
func TestOASFContract(t *testing.T) {
	mapper := NewMapper("1.0.0", []string{"semstreams-test"}, true)
	triples := contractTriples()

	record, err := mapper.MapTriplesToOASF(contractAgentID, triples)
	if err != nil {
		t.Fatalf("MapTriplesToOASF: %v", err)
	}

	// Pin schema_version explicitly so a rename in oasf_types.go is caught here,
	// not silently in production.
	if record.SchemaVersion != CurrentSchemaVersion {
		t.Errorf("SchemaVersion = %q, want %q", record.SchemaVersion, CurrentSchemaVersion)
	}
	if record.SchemaVersion != "1.0.0" {
		t.Errorf("CurrentSchemaVersion drifted from documented 1.0.0: %q", record.SchemaVersion)
	}

	// CreatedAt is wallclock-derived; validate format then null it for comparison.
	if record.CreatedAt == "" {
		t.Error("CreatedAt is empty")
	} else if _, err := time.Parse(time.RFC3339, record.CreatedAt); err != nil {
		t.Errorf("CreatedAt %q is not RFC-3339: %v", record.CreatedAt, err)
	}
	record.CreatedAt = ""

	// Map iteration order is non-deterministic for skills, domains, and
	// extension lists; normalize before structural comparison.
	normalizeForGolden(record)

	got, err := json.MarshalIndent(record, "", "  ")
	if err != nil {
		t.Fatalf("marshal record: %v", err)
	}

	goldenPath := filepath.Join("testdata", "oasf_record_golden.json")

	if *update {
		writeGolden(t, goldenPath, got)
		return
	}

	want, err := os.ReadFile(goldenPath)
	if err != nil {
		t.Fatalf("read golden: %v (run with -update to create)", err)
	}

	// Compare structurally, not byte-for-byte, so trailing newline / indent
	// differences don't trip the test.
	var gotAny, wantAny any
	if err := json.Unmarshal(got, &gotAny); err != nil {
		t.Fatalf("unmarshal got: %v", err)
	}
	if err := json.Unmarshal(want, &wantAny); err != nil {
		t.Fatalf("unmarshal golden: %v", err)
	}

	if diff := cmp.Diff(wantAny, gotAny); diff != "" {
		t.Errorf("OASF record drift (-golden +got):\n%s\n\nFull got payload:\n%s",
			diff, string(got))
	}
}

const contractAgentID = "acme.ops.agentic.system.agent.contract-test"

// contractTriples is the canonical input that exercises every predicate the
// mapper handles. Keep this list aligned with PredicateMapping; if mapping
// grows, add an entry here AND regenerate the golden.
func contractTriples() []message.Triple {
	ts := time.Unix(0, 0).UTC() // pinned timestamp; mapper doesn't read it but reviewers do
	return []message.Triple{
		// Skill 1: full surface
		{Subject: contractAgentID, Predicate: agentic.CapabilityExpression, Object: "code-review", Context: "code-review", Timestamp: ts},
		{Subject: contractAgentID, Predicate: agentic.CapabilityName, Object: "Code Review", Context: "code-review", Timestamp: ts},
		{Subject: contractAgentID, Predicate: agentic.CapabilityDescription, Object: "Review code for quality and security issues", Context: "code-review", Timestamp: ts},
		{Subject: contractAgentID, Predicate: agentic.CapabilityConfidence, Object: 0.9, Context: "code-review", Timestamp: ts},
		{Subject: contractAgentID, Predicate: agentic.CapabilityPermission, Object: "file_read", Context: "code-review", Timestamp: ts},
		{Subject: contractAgentID, Predicate: agentic.CapabilityPermission, Object: "file_write", Context: "code-review", Timestamp: ts},

		// Skill 2: minimal — exercises generateSkillID fallback when name only
		{Subject: contractAgentID, Predicate: agentic.CapabilityExpression, Object: "documentation", Context: "documentation", Timestamp: ts},
		{Subject: contractAgentID, Predicate: agentic.CapabilityName, Object: "Documentation", Context: "documentation", Timestamp: ts},

		// Intent → description + domains
		{Subject: contractAgentID, Predicate: agentic.IntentGoal, Object: "Help developers review code and ship docs", Timestamp: ts},
		{Subject: contractAgentID, Predicate: agentic.IntentType, Object: "software-development", Timestamp: ts},
		{Subject: contractAgentID, Predicate: agentic.IntentType, Object: "documentation", Timestamp: ts},

		// Action → extensions.action_types (deduped)
		{Subject: contractAgentID, Predicate: agentic.ActionType, Object: "tool-call", Timestamp: ts},
		{Subject: contractAgentID, Predicate: agentic.ActionType, Object: "tool-call", Timestamp: ts},
		{Subject: contractAgentID, Predicate: agentic.ActionType, Object: "model-call", Timestamp: ts},
	}
}

// normalizeForGolden makes the record byte-stable by sorting collections whose
// source-of-truth is a map.
func normalizeForGolden(r *OASFRecord) {
	sort.Slice(r.Skills, func(i, j int) bool { return r.Skills[i].ID < r.Skills[j].ID })
	for i := range r.Skills {
		sort.Strings(r.Skills[i].Permissions)
	}
	sort.Slice(r.Domains, func(i, j int) bool { return r.Domains[i].Name < r.Domains[j].Name })
	if v, ok := r.Extensions["action_types"]; ok {
		if list, ok := v.([]string); ok {
			sort.Strings(list)
			r.Extensions["action_types"] = list
		}
	}
}

func writeGolden(t *testing.T, path string, data []byte) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("mkdir testdata: %v", err)
	}
	// Trailing newline so editors stop fighting us.
	data = append(data, '\n')
	if err := os.WriteFile(path, data, 0o644); err != nil {
		t.Fatalf("write golden: %v", err)
	}
	t.Logf("updated golden %s", path)
}

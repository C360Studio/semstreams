package results

import (
	"bytes"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/c360studio/semstreams/test/e2e/scenarios"
)

func TestWriteRunSeparatesSiblingIdentities(t *testing.T) {
	w := NewWriter(t.TempDir())
	stamp := time.Date(2026, 9, 27, 12, 0, 0, 0, time.UTC)
	first := &TestRun{ID: "run-first", Timestamp: stamp, Config: TestRunConfig{Variant: "statistical"}}
	second := &TestRun{ID: "run-second", Timestamp: stamp, Config: TestRunConfig{Variant: "statistical"}}
	firstPath, err := w.WriteRun(first)
	if err != nil {
		t.Fatal(err)
	}
	secondPath, err := w.WriteRun(second)
	if err != nil {
		t.Fatal(err)
	}
	if firstPath == secondPath || filepath.Dir(firstPath) != filepath.Dir(secondPath) {
		t.Fatalf("sibling run paths collide or diverge: %q, %q", firstPath, secondPath)
	}
	retained, err := w.LoadRun(firstPath)
	if err != nil {
		t.Fatal(err)
	}
	if retained.ID != first.ID {
		t.Fatalf("first run overwritten by sibling: got %q", retained.ID)
	}
}

func completeRunFixture(t testing.TB) *TestRun {
	t.Helper()
	run := CreateTestRun(TestRunConfig{Selection: "tiered:statistical", Variant: "statistical", RequireEvidence: true, RequiredMembers: []string{"tiered:statistical"}}, nil, nil, 0)
	run.Command = []string{"./e2e", "--scenario", "tiered", "--variant", "statistical"}
	run.WorkingDir = t.TempDir()
	child := scenarios.Result{ScenarioName: "tiered"}
	if err := child.DeclareChecks(run.ID, "tiered:statistical", []scenarios.CheckRequirement{{ID: "controlled-search.identity", Required: true}}); err != nil {
		t.Fatal(err)
	}
	if err := child.RecordCheck(scenarios.CheckObservation{ID: "controlled-search.identity", RunID: run.ID, MemberID: "tiered:statistical", Status: "passed"}); err != nil {
		t.Fatal(err)
	}
	if err := child.FinalizeChecks(); err != nil {
		t.Fatal(err)
	}
	run.Scenarios = []scenarios.Result{child}
	run.CompletedAt = run.StartedAt.Add(time.Second)
	zero := 0
	run.ExitCode = &zero
	run.Environment = map[string]string{
		"source_sha":                "0123456789012345678901234567890123456789",
		"source_dirty":              "false",
		"source_patch_sha256":       "not_applicable: clean source",
		"source_untracked_sha256":   "not_applicable: clean source",
		"runner_sha256":             "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef",
		"runner_build":              "go1.26",
		"app_image_id":              "sha256:0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef",
		"app_image_digest":          "unavailable: local image has no registry digest",
		"app_binary_sha256":         "not_applicable: image identified",
		"app_build":                 "test-build",
		"compose_sha256":            "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef",
		"profiles":                  "statistical",
		"config_sha256":             "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef",
		"fixture_sha256":            "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef",
		"effective_settings_sha256": "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef",
	}
	for _, pair := range [][2]string{{"log_path", "log_sha256"}, {"artifact_manifest_path", "artifact_manifest_sha256"}} {
		path := filepath.Join(t.TempDir(), pair[0])
		content := []byte(pair[0] + ": fixture identity\n")
		if err := os.WriteFile(path, content, 0644); err != nil {
			t.Fatal(err)
		}
		run.Environment[pair[0]] = path
		run.Environment[pair[1]] = fmt.Sprintf("%x", sha256.Sum256(content))
	}
	return run
}

func TestWriteRunPreservesInitialAndCompletedIdentity(t *testing.T) {
	w := NewWriter(t.TempDir())
	run := completeRunFixture(t)
	completed := run.Scenarios
	declared := completed[0]
	declared.CheckObservations = nil
	declared.Success = false
	declared.EvidenceStatus = "unattested"
	declared.AssertionsRun = 0
	run.Scenarios = []scenarios.Result{declared}
	run.CompletedAt = time.Time{}
	run.ExitCode = nil
	initialPath, err := w.WriteRun(run)
	if err != nil {
		t.Fatal(err)
	}
	initial, err := w.LoadRun(initialPath)
	if err != nil {
		t.Fatal(err)
	}
	if initial.EvidenceStatus != "unattested" || initial.Summary.RequiredProofPassed {
		t.Fatalf("initial record claimed proof: %+v", initial)
	}
	if initial.ExitCode != nil || !initial.CompletedAt.IsZero() {
		t.Fatalf("initial record synthesized terminal status: %+v", initial)
	}
	run.Scenarios = completed
	run.CompletedAt = run.StartedAt.Add(time.Second)
	zero := 0
	run.ExitCode = &zero
	finalPath, err := w.WriteRun(run)
	if err != nil {
		t.Fatal(err)
	}
	if finalPath != initialPath {
		t.Fatalf("final aggregate moved: %q != %q", finalPath, initialPath)
	}
	final, err := w.LoadRun(finalPath)
	if err != nil {
		t.Fatal(err)
	}
	if final.ID != initial.ID || final.EvidenceStatus != "complete" || !final.Summary.RequiredProofPassed {
		t.Fatalf("final evidence does not match same complete run: %+v", final)
	}
}

func TestWriteRunRefusesMissingOrForeignRequiredMember(t *testing.T) {
	for _, tc := range []struct {
		name   string
		change func(*TestRun)
	}{
		{name: "missing", change: func(run *TestRun) { run.Scenarios = nil }},
		{name: "foreign run", change: func(run *TestRun) { run.Scenarios[0].RunID = "run-old" }},
		{name: "foreign member", change: func(run *TestRun) { run.Scenarios[0].MemberID = "other" }},
		{name: "duplicate", change: func(run *TestRun) { run.Scenarios = append(run.Scenarios, run.Scenarios[0]) }},
		{name: "failed exit", change: func(run *TestRun) { exit := 1; run.ExitCode = &exit }},
		{name: "missing app", change: func(run *TestRun) {
			run.Environment["app_image_id"] = "unavailable: remote"
			run.Environment["app_image_digest"] = "unavailable: remote"
		}},
		{name: "malformed source", change: func(run *TestRun) { run.Environment["source_sha"] = "guessed-main" }},
		{name: "malformed runner", change: func(run *TestRun) { run.Environment["runner_sha256"] = "abc" }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			run := completeRunFixture(t)
			tc.change(run)
			w := NewWriter(t.TempDir())
			path, err := w.WriteRun(run)
			if err != nil {
				t.Fatal(err)
			}
			got, err := w.LoadRun(path)
			if err != nil {
				t.Fatal(err)
			}
			if got.EvidenceStatus == "complete" || got.Summary.RequiredProofPassed {
				t.Fatalf("missing/foreign proof passed: %+v", got)
			}
			if got.ExitCode == nil || *got.ExitCode == 0 {
				t.Fatalf("incomplete required proof retained zero outer exit: %+v", got.ExitCode)
			}
		})
	}
}

func TestWriteRunLegacyExecutionStaysUnattestedWithZeroExit(t *testing.T) {
	run := CreateTestRun(TestRunConfig{Selection: "lessons", Variant: "lessons"}, nil, nil, 0)
	run.Command = []string{"./e2e", "--scenario", "lessons"}
	run.WorkingDir = t.TempDir()
	run.CompletedAt = run.StartedAt.Add(time.Second)
	zero := 0
	run.ExitCode = &zero
	path, err := NewWriter(t.TempDir()).WriteRun(run)
	if err != nil {
		t.Fatal(err)
	}
	got, err := NewWriter(t.TempDir()).LoadRun(path)
	if err != nil {
		t.Fatal(err)
	}
	if got.ExitCode == nil || *got.ExitCode != 0 || got.EvidenceStatus != "unattested" || got.Summary.RequiredProofPassed {
		t.Fatalf("legacy diagnostic execution was reclassified: %+v", got)
	}
}

func TestWriteRunRequiredEmptySetDowngradesOverallExit(t *testing.T) {
	run := completeRunFixture(t)
	run.Config.RequiredMembers = nil
	path, err := NewWriter(t.TempDir()).WriteRun(run)
	if err != nil {
		t.Fatal(err)
	}
	got, err := NewWriter(t.TempDir()).LoadRun(path)
	if err != nil {
		t.Fatal(err)
	}
	if got.ExitCode == nil || *got.ExitCode == 0 || got.EvidenceStatus == "complete" {
		t.Fatalf("empty required set passed: %+v", got)
	}
}

func TestWriteRunRefusesUnknownEnvironmentKey(t *testing.T) {
	run := completeRunFixture(t)
	run.Environment["API_TOKEN"] = "must-not-persist"
	if _, err := NewWriter(t.TempDir()).WriteRun(run); err == nil {
		t.Fatal("unclassified environment key was serialized")
	}
}

func TestWriteRunProjectsFinalFailureIntoTierMetadata(t *testing.T) {
	run := completeRunFixture(t)
	run.Scenarios[0].Success = false
	run.Scenarios[0].Error = "final semantic validation failed"
	run.Scenarios[0].Structured = &scenarios.TieredResults{
		Metadata: scenarios.TestMetadata{Success: true},
	}
	path, err := NewWriter(t.TempDir()).WriteRun(run)
	if err != nil {
		t.Fatal(err)
	}
	loaded, err := NewWriter(t.TempDir()).LoadRun(path)
	if err != nil {
		t.Fatal(err)
	}
	child := loaded.Scenarios[0]
	if child.Success || child.Structured.Metadata.Success || loaded.EvidenceStatus == "complete" {
		t.Fatalf("stale typed success escaped final failure: %+v", loaded)
	}
}

func TestRequiredProofValidationDoesNotMutateTypedOutcome(t *testing.T) {
	run := completeRunFixture(t)
	run.Scenarios[0].Success = false // a final execution failure need not have an Error string
	run.Scenarios[0].Structured = &scenarios.TieredResults{Metadata: scenarios.TestMetadata{Success: false}}
	_, complete := evaluateRequiredProof(run)
	if complete || run.Scenarios[0].Structured.Metadata.Success {
		t.Fatalf("validation changed false execution outcome: %+v", run.Scenarios[0])
	}

	missing := completeRunFixture(t)
	missing.Scenarios[0].CheckObservations = nil
	missing.Scenarios[0].Structured = &scenarios.TieredResults{Metadata: scenarios.TestMetadata{Success: true}}
	_, complete = evaluateRequiredProof(missing)
	if complete || !missing.Scenarios[0].Structured.Metadata.Success {
		t.Fatalf("validation changed original typed projection: %+v", missing.Scenarios[0])
	}
}

func TestWriteRunKeepsFalseExecutionOutcomeAcrossRoundTrip(t *testing.T) {
	run := completeRunFixture(t)
	run.Scenarios[0].Success = false
	run.Scenarios[0].Structured = &scenarios.TieredResults{Metadata: scenarios.TestMetadata{Success: true}}
	w := NewWriter(t.TempDir())
	path, err := w.WriteRun(run)
	if err != nil {
		t.Fatal(err)
	}
	if run.Scenarios[0].Structured.Metadata.Success {
		t.Fatal("writer changed typed execution failure to success")
	}
	loaded, err := w.LoadRun(path)
	if err != nil {
		t.Fatal(err)
	}
	if loaded.Scenarios[0].Success || loaded.Scenarios[0].Structured.Metadata.Success || loaded.EvidenceStatus == "complete" {
		t.Fatalf("round trip contradicted execution failure: %+v", loaded)
	}
}

func TestWriteRunProjectsMissingObservationFailure(t *testing.T) {
	run := completeRunFixture(t)
	run.Scenarios[0].CheckObservations = nil
	run.Scenarios[0].Structured = &scenarios.TieredResults{Metadata: scenarios.TestMetadata{Success: true}}
	w := NewWriter(t.TempDir())
	path, err := w.WriteRun(run)
	if err != nil {
		t.Fatal(err)
	}
	loaded, err := w.LoadRun(path)
	if err != nil {
		t.Fatal(err)
	}
	child := loaded.Scenarios[0]
	if child.Success || child.Structured.Metadata.Success || loaded.EvidenceStatus == "complete" {
		t.Fatalf("missing observation left success in retained projection: %+v", loaded)
	}
}

func TestRequiredRunNeedsRetainedReferences(t *testing.T) {
	for _, key := range []string{"log_path", "log_sha256", "artifact_manifest_path", "artifact_manifest_sha256"} {
		t.Run(key, func(t *testing.T) {
			run := completeRunFixture(t)
			delete(run.Environment, key)
			w := NewWriter(t.TempDir())
			path, err := w.WriteRun(run)
			if err != nil {
				t.Fatal(err)
			}
			loaded, err := w.LoadRun(path)
			if err != nil {
				t.Fatal(err)
			}
			if loaded.EvidenceStatus == "complete" || loaded.ExitCode == nil || *loaded.ExitCode == 0 {
				t.Fatalf("missing %s certified complete: %+v", key, loaded)
			}
		})
	}
}

func TestTerminalRunCannotChangeInitializedCheckRequirements(t *testing.T) {
	for _, tc := range []struct {
		name   string
		change func(*scenarios.Result)
	}{
		{name: "remove", change: func(child *scenarios.Result) {
			child.CheckRequirements = child.CheckRequirements[:1]
			child.CheckObservations = child.CheckObservations[:1]
		}},
		{name: "rename", change: func(child *scenarios.Result) {
			child.CheckRequirements[1].ID = "other.identity"
			child.CheckObservations[1].ID = "other.identity"
		}},
		{name: "demote", change: func(child *scenarios.Result) {
			child.CheckRequirements[1].Required = false
			child.CheckObservations = child.CheckObservations[:1]
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			run := completeRunFixture(t)
			expected := append([]scenarios.CheckRequirement(nil), run.Scenarios[0].CheckRequirements...)
			expected = append(expected, scenarios.CheckRequirement{ID: "graph-roundtrip.identity", Required: true})
			run.Scenarios[0].CheckRequirements = append([]scenarios.CheckRequirement(nil), expected...)
			run.Scenarios[0].CheckObservations = nil
			run.Scenarios[0].Success = false
			run.Scenarios[0].EvidenceStatus = "unattested"
			run.Scenarios[0].AssertionsRun = 0
			run.CompletedAt = time.Time{}
			run.ExitCode = nil
			w := NewWriter(t.TempDir())
			path, err := w.WriteRun(run)
			if err != nil {
				t.Fatal(err)
			}
			child := run.Scenarios[0]
			child.CheckRequirements = append([]scenarios.CheckRequirement(nil), expected...)
			child.CheckObservations = []scenarios.CheckObservation{
				{ID: expected[0].ID, RunID: run.ID, MemberID: child.MemberID, Status: "passed"},
				{ID: expected[1].ID, RunID: run.ID, MemberID: child.MemberID, Status: "passed"},
			}
			child.Success = true
			tc.change(&child)
			run.Scenarios = []scenarios.Result{child}
			run.CompletedAt = run.StartedAt.Add(time.Second)
			zero := 0
			run.ExitCode = &zero
			finalPath, err := w.WriteRun(run)
			if err != nil {
				t.Fatalf("failed terminal artifact was not retained: %v", err)
			}
			if finalPath != path {
				t.Fatalf("terminal run moved: %q != %q", finalPath, path)
			}
			loaded, err := w.LoadRun(path)
			if err != nil {
				t.Fatal(err)
			}
			if loaded.ExitCode == nil || *loaded.ExitCode == 0 || loaded.EvidenceStatus == "complete" ||
				loaded.Summary.RequiredProofPassed {
				t.Fatalf("changed check declaration yielded proof: %+v", loaded)
			}
			if len(loaded.Scenarios) != 1 || len(loaded.Scenarios[0].CheckRequirements) != len(expected) {
				t.Fatalf("initialized obligations disappeared from retained failure: %+v", loaded.Scenarios)
			}
			for i := range expected {
				if loaded.Scenarios[0].CheckRequirements[i] != expected[i] {
					t.Fatalf("initialized requirement %d changed: got %+v want %+v", i, loaded.Scenarios[0].CheckRequirements[i], expected[i])
				}
			}
		})
	}
}

func TestInitializedRunCannotReplaceCheckRequirements(t *testing.T) {
	run := completeRunFixture(t)
	declared := run.Scenarios[0]
	declared.CheckObservations = nil
	declared.Success = false
	declared.EvidenceStatus = "unattested"
	declared.AssertionsRun = 0
	run.Scenarios = []scenarios.Result{declared}
	run.CompletedAt = time.Time{}
	run.ExitCode = nil
	w := NewWriter(t.TempDir())
	path, err := w.WriteRun(run)
	if err != nil {
		t.Fatal(err)
	}
	before, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	run.Scenarios[0].CheckRequirements[0].Required = false
	if _, err := w.WriteRun(run); err == nil {
		t.Fatal("second initialization replaced the declared required check")
	}
	after, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(before, after) {
		t.Fatal("failed reinitialization changed the retained snapshot")
	}
}

func TestMalformedTerminalDoesNotPoisonInitializedMemberCorrection(t *testing.T) {
	for _, tc := range []struct {
		name       string
		malform    func(scenarios.Result) []scenarios.Result
		diagnostic string
	}{
		{name: "foreign run", diagnostic: "run-foreign", malform: func(child scenarios.Result) []scenarios.Result {
			child.RunID = "run-foreign"
			return []scenarios.Result{child}
		}},
		{name: "duplicate member", diagnostic: "duplicate", malform: func(child scenarios.Result) []scenarios.Result {
			other := child
			other.CheckObservations = []scenarios.CheckObservation{{
				ID: child.CheckRequirements[0].ID, RunID: child.RunID, MemberID: child.MemberID,
				Status: "failed", Reason: "duplicate observed failure",
			}}
			return []scenarios.Result{child, other}
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			run := completeRunFixture(t)
			completed := run.Scenarios[0]
			declared := completed
			declared.CheckObservations = nil
			declared.Success = false
			declared.EvidenceStatus = "unattested"
			declared.AssertionsRun = 0
			run.Scenarios = []scenarios.Result{declared}
			run.CompletedAt = time.Time{}
			run.ExitCode = nil
			w := NewWriter(t.TempDir())
			path, err := w.WriteRun(run)
			if err != nil {
				t.Fatal(err)
			}
			run.Scenarios = tc.malform(completed)
			run.CompletedAt = run.StartedAt.Add(time.Second)
			zero := 0
			run.ExitCode = &zero
			if _, err := w.WriteRun(run); err != nil {
				t.Fatalf("malformed terminal evidence was not retained: %v", err)
			}
			failed, err := w.LoadRun(path)
			if err != nil {
				t.Fatal(err)
			}
			if failed.ExitCode == nil || *failed.ExitCode == 0 || failed.EvidenceStatus == "complete" ||
				len(failed.Scenarios) != 1 || failed.Scenarios[0].RunID != run.ID ||
				!slices.Equal(failed.Scenarios[0].CheckRequirements, declared.CheckRequirements) ||
				!strings.Contains(strings.Join(failed.Scenarios[0].Errors, "; "), tc.diagnostic) {
				t.Fatalf("failed terminal did not retain canonical declaration and diagnostic: %+v", failed)
			}
			if tc.name == "duplicate member" &&
				(len(failed.Scenarios[0].CheckObservations) != 2 ||
					failed.Scenarios[0].CheckObservations[1].Status != "failed" ||
					failed.Scenarios[0].CheckObservations[1].Reason != "duplicate observed failure") {
				t.Fatalf("duplicate failed observation disappeared: %+v", failed.Scenarios[0])
			}
			run.Scenarios = []scenarios.Result{completed}
			zero = 0
			run.ExitCode = &zero
			if _, err := w.WriteRun(run); err != nil {
				t.Fatalf("correctly bound aggregate correction failed: %v", err)
			}
			corrected, err := w.LoadRun(path)
			if err != nil {
				t.Fatal(err)
			}
			if corrected.EvidenceStatus != "complete" || corrected.ExitCode == nil || *corrected.ExitCode != 0 ||
				len(corrected.Scenarios) != 1 || !slices.Equal(corrected.Scenarios[0].CheckRequirements, declared.CheckRequirements) {
				t.Fatalf("correctly bound correction did not complete: %+v", corrected)
			}
		})
	}
}

func TestTerminalRunRetainsUnchangedAndMissingOrFailedMembers(t *testing.T) {
	for _, tc := range []struct {
		name         string
		missing      bool
		failed       bool
		wantComplete bool
	}{
		{name: "unchanged complete", wantComplete: true},
		{name: "missing member", missing: true},
		{name: "failed member", failed: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			run := completeRunFixture(t)
			completed := run.Scenarios[0]
			declared := completed
			declared.CheckObservations = nil
			declared.Success = false
			declared.EvidenceStatus = "unattested"
			declared.AssertionsRun = 0
			run.Scenarios = []scenarios.Result{declared}
			run.CompletedAt = time.Time{}
			run.ExitCode = nil
			w := NewWriter(t.TempDir())
			path, err := w.WriteRun(run)
			if err != nil {
				t.Fatal(err)
			}
			if tc.missing {
				run.Scenarios = nil
			} else {
				if tc.failed {
					completed.CheckObservations[0].Status = "failed"
					completed.CheckObservations[0].Reason = "wrong identity"
				}
				run.Scenarios = []scenarios.Result{completed}
			}
			run.CompletedAt = run.StartedAt.Add(time.Second)
			zero := 0
			run.ExitCode = &zero
			if _, err := w.WriteRun(run); err != nil {
				t.Fatalf("terminal evidence not retained: %v", err)
			}
			loaded, err := w.LoadRun(path)
			if err != nil {
				t.Fatal(err)
			}
			if loaded.Summary.RequiredProofPassed != tc.wantComplete || (loaded.ExitCode != nil && *loaded.ExitCode == 0) != tc.wantComplete {
				t.Fatalf("terminal outcome = %+v, want complete %t", loaded, tc.wantComplete)
			}
			if tc.missing && len(loaded.Summary.MissingRequiredMembers) != 1 {
				t.Fatalf("missing member not retained: %+v", loaded.Summary)
			}
			if tc.failed && (len(loaded.Scenarios) != 1 || loaded.Scenarios[0].CheckObservations[0].Status != "failed") {
				t.Fatalf("failed observation not retained: %+v", loaded.Scenarios)
			}
		})
	}
}

func TestLoadRunKeepsLegacyUnattestedAndRejectsFutureSchema(t *testing.T) {
	dir := t.TempDir()
	w := NewWriter(dir)
	legacy := []byte(`{"id":"old","summary":{"all_passed":true},"scenarios":[{"success":true}]}`)
	path := filepath.Join(dir, "legacy.json")
	if err := os.WriteFile(path, legacy, 0644); err != nil {
		t.Fatal(err)
	}
	got, err := w.LoadRun(path)
	if err != nil {
		t.Fatal(err)
	}
	if got.EvidenceStatus != "unattested" || got.Summary.RequiredProofPassed {
		t.Fatalf("legacy aggregate promoted to proof: %+v", got)
	}
	if err := os.WriteFile(path, []byte(`{"schema_version":99,"evidence_status":"complete"}`), 0644); err != nil {
		t.Fatal(err)
	}
	if _, err := w.LoadRun(path); err == nil {
		t.Fatal("future schema was accepted")
	}
	valid := completeRunFixture(t)
	valid.EvidenceStatus = "complete"
	valid.Scenarios = nil
	data, err := json.Marshal(valid)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, data, 0644); err != nil {
		t.Fatal(err)
	}
	if _, err := w.LoadRun(path); err == nil {
		t.Fatal("malformed complete report was accepted")
	}
}

func TestGetLatestRunUsesRecordedStartAcrossSelections(t *testing.T) {
	w := NewWriter(t.TempDir())
	old := &TestRun{ID: "run-z", Timestamp: time.Date(2026, 9, 26, 12, 0, 0, 0, time.UTC), Config: TestRunConfig{Variant: "zz-old"}}
	newer := &TestRun{ID: "run-a", Timestamp: time.Date(2026, 9, 27, 12, 0, 0, 0, time.UTC), Config: TestRunConfig{Variant: "aa-new"}}
	for _, run := range []*TestRun{old, newer} {
		if _, err := w.WriteRun(run); err != nil {
			t.Fatal(err)
		}
	}
	got, err := w.GetLatestRun()
	if err != nil {
		t.Fatal(err)
	}
	if got.ID != newer.ID {
		t.Fatalf("latest by filename = %q, want recorded latest %q", got.ID, newer.ID)
	}
}

func FuzzDecodeRunPreservesRequiredProofIdentity(f *testing.F) {
	valid := completeRunFixture(f)
	if _, err := NewWriter(f.TempDir()).WriteRun(valid); err != nil {
		f.Fatal(err)
	}
	validBytes, err := json.Marshal(valid)
	if err != nil {
		f.Fatal(err)
	}
	f.Add(validBytes)
	f.Add([]byte(`{"id":"old","summary":{"all_passed":true}}`))
	f.Add([]byte(`{"schema_version":99,"evidence_status":"complete"}`))
	f.Add([]byte(`{"schema_version":2,"id":"run-fake","evidence_status":"complete"}`))
	f.Add([]byte(`null`))
	f.Add([]byte(`{"schema_version":2`))
	f.Fuzz(func(t *testing.T, data []byte) {
		if len(data) > 1<<16 {
			t.Skip()
		}
		run, err := decodeRun(data)
		if err != nil {
			return
		}
		if run.SchemaVersion == 0 && (run.EvidenceStatus != "unattested" || run.Summary.RequiredProofPassed) {
			t.Fatalf("legacy report promoted to proof: %+v", run)
		}
		if run.EvidenceStatus != "complete" {
			return
		}
		if run.SchemaVersion != 2 || !run.Config.RequireEvidence || run.ExitCode == nil || *run.ExitCode != 0 ||
			run.CompletedAt.IsZero() || !run.Summary.RequiredProofPassed || len(run.Config.RequiredMembers) == 0 {
			t.Fatalf("complete report lacks required envelope: %+v", run)
		}
		members := make(map[string]bool, len(run.Config.RequiredMembers))
		for _, member := range run.Config.RequiredMembers {
			if members[member] {
				t.Fatalf("duplicate selected member %q", member)
			}
			members[member] = true
		}
		if len(run.Scenarios) != len(members) {
			t.Fatalf("selected/completed member count differs")
		}
		for _, child := range run.Scenarios {
			if !members[child.MemberID] || child.RunID != run.ID || !child.Success || child.EvidenceStatus != "complete" {
				t.Fatalf("foreign or incomplete member: %+v", child)
			}
			delete(members, child.MemberID)
			observed := make(map[string]int)
			for _, check := range child.CheckObservations {
				if check.Status == "passed" && check.RunID == run.ID && check.MemberID == child.MemberID {
					observed[check.ID]++
				}
			}
			for _, required := range child.CheckRequirements {
				if required.Required && observed[required.ID] != 1 {
					t.Fatalf("required check %q not passed exactly once", required.ID)
				}
			}
		}
		if len(members) != 0 {
			t.Fatalf("required members missing: %v", members)
		}
	})
}

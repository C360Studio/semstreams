package results

import (
	"bytes"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/c360studio/semstreams/test/e2e/scenarios"
)

func TestTaskStatusAndParentIdentitySurviveRoundTrip(t *testing.T) {
	run := completeRunFixture(t)
	run.ParentID, run.ParentMemberID = "parent", "statistical"
	run.Config.RequireTaskStatuses = true
	zero := 0
	run.CommandExitCode, run.CleanupExitCode = &zero, &zero
	w := NewWriter(t.TempDir())
	path, err := w.WriteRun(run)
	if err != nil {
		t.Fatal(err)
	}
	loaded, err := w.LoadRun(path)
	if err != nil {
		t.Fatal(err)
	}
	if loaded.EvidenceStatus != "complete" || loaded.ParentID != "parent" || loaded.ParentMemberID != "statistical" ||
		loaded.CommandExitCode == nil || *loaded.CommandExitCode != 0 || loaded.CleanupExitCode == nil || *loaded.CleanupExitCode != 0 {
		t.Fatalf("complete task fields lost: %+v", loaded)
	}

	run = completeRunFixture(t)
	run.Config.RequireTaskStatuses = true
	run.CommandExitCode = &zero
	path, err = NewWriter(t.TempDir()).WriteRun(run)
	if err != nil {
		t.Fatal(err)
	}
	loaded, err = w.LoadRun(path)
	if err != nil {
		t.Fatal(err)
	}
	if loaded.EvidenceStatus == "complete" || loaded.ExitCode == nil || *loaded.ExitCode == 0 {
		t.Fatalf("missing cleanup status passed: %+v", loaded)
	}

	run = completeRunFixture(t)
	run.Config.RequireTaskStatuses = true
	command, cleanup := 3, 4
	run.CommandExitCode, run.CleanupExitCode = &command, &cleanup
	run.ExitCode = &zero
	path, err = NewWriter(t.TempDir()).WriteRun(run)
	if err != nil {
		t.Fatal(err)
	}
	loaded, err = w.LoadRun(path)
	if err != nil {
		t.Fatal(err)
	}
	if loaded.ExitCode == nil || *loaded.ExitCode == 0 || *loaded.CommandExitCode != 3 || *loaded.CleanupExitCode != 4 {
		t.Fatalf("command/cleanup failures lost: %+v", loaded)
	}
}

func TestTaskInitializedScopeCannotChangeAtTerminalWrite(t *testing.T) {
	run := completeRunFixture(t)
	run.Config.ChildExpectations = []ChildExpectation{{ParentMemberID: "tiered:statistical", Selection: "core-scenarios",
		RequiredMembers: []string{"core-health", "core-dataflow"}}}
	run.CompletedAt = time.Time{}
	run.ExitCode = nil
	w := NewWriter(t.TempDir())
	if _, err := w.WriteRun(run); err != nil {
		t.Fatal(err)
	}
	run.Config.ChildExpectations[0].RequiredMembers = []string{"core-health"}
	run.CompletedAt = run.StartedAt.Add(time.Second)
	zero := 0
	run.ExitCode = &zero
	if _, err := w.WriteRun(run); err == nil {
		t.Fatal("terminal write changed initialized child membership")
	}
}

func TestVerifyChildBindsExactParentSlotAndBytes(t *testing.T) {
	child := completeRunFixture(t)
	child.ParentID, child.ParentMemberID = "parent-run", "statistical"
	child.Config.RequireTaskStatuses = true
	zero := 0
	child.CommandExitCode, child.CleanupExitCode = &zero, &zero
	childDir := t.TempDir()
	path, err := NewWriter(childDir).WriteRun(child)
	if err != nil {
		t.Fatal(err)
	}
	expected := ChildExpectation{ParentID: "parent-run", ParentMemberID: "statistical", Selection: child.Config.Selection,
		RequiredMembers: []string{"tiered:statistical"}, RequireTaskStatuses: true}
	w := NewWriter(t.TempDir())
	artifact, err := w.VerifyChild(path, expected)
	if err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if artifact.RunID != child.ID || artifact.SHA256 != fmtSHA256(data) || artifact.Path == "" {
		t.Fatalf("child reference does not bind exact bytes: %+v", artifact)
	}
	for _, mutate := range []func(*ChildExpectation){
		func(e *ChildExpectation) { e.ParentMemberID = "other" },
		func(e *ChildExpectation) { e.Selection = "tiered:semantic" },
		func(e *ChildExpectation) { e.RequiredMembers = []string{"other"} },
		func(e *ChildExpectation) { e.RequireTaskStatuses = false },
	} {
		wrong := expected
		mutate(&wrong)
		if _, err := w.VerifyChild(path, wrong); err == nil {
			t.Fatalf("foreign child accepted for %+v", wrong)
		}
	}
	if _, err := w.VerifyChild(filepath.Join(t.TempDir(), "missing.json"), expected); err == nil {
		t.Fatal("missing child artifact accepted")
	}
}

func TestVerifyChildRequiresRetainedLogAndManifestBytes(t *testing.T) {
	for _, tc := range []struct {
		name string
		key  string
		drop bool
	}{
		{name: "missing log", key: "log_path", drop: true},
		{name: "changed log", key: "log_path"},
		{name: "missing manifest", key: "artifact_manifest_path", drop: true},
		{name: "changed manifest", key: "artifact_manifest_path"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			child := completeRunFixture(t)
			child.ParentID, child.ParentMemberID = "parent-run", "statistical"
			path, err := NewWriter(t.TempDir()).WriteRun(child)
			if err != nil {
				t.Fatal(err)
			}
			referencePath := child.Environment[tc.key]
			if tc.drop {
				if err := os.Remove(referencePath); err != nil {
					t.Fatal(err)
				}
			} else if err := os.WriteFile(referencePath, []byte("changed bytes"), 0644); err != nil {
				t.Fatal(err)
			}
			expected := ChildExpectation{ParentID: "parent-run", ParentMemberID: "statistical",
				Selection: child.Config.Selection, RequiredMembers: []string{"tiered:statistical"}}
			if _, err := NewWriter(t.TempDir()).VerifyChild(path, expected); err == nil {
				t.Fatalf("child with %s passed retained-byte verification", tc.name)
			}
		})
	}
}

func TestWriteManifestBindsExactBytesAndInitializedRun(t *testing.T) {
	run := completeRunFixture(t)
	completed := run.Scenarios[0]
	run.ParentID, run.ParentMemberID = "parent-run", "statistical"
	run.Scenarios[0].CheckObservations = nil
	run.Scenarios[0].Success = false
	run.Scenarios[0].EvidenceStatus = "unattested"
	run.Scenarios[0].AssertionsRun = 0
	run.CompletedAt = time.Time{}
	run.ExitCode = nil
	w := NewWriter(t.TempDir())
	if _, err := w.WriteRun(run); err != nil {
		t.Fatal(err)
	}
	data := []byte(`{"entries":[{"role":"app_phase","phase":"production"},{"role":"app_phase","phase":"fixtures"}]}`)
	reference, err := w.WriteManifest(run, data)
	if err != nil {
		t.Fatal(err)
	}
	if reference.RunID != run.ID || reference.SHA256 != fmtSHA256(data) || filepath.IsAbs(reference.Path) {
		t.Fatalf("manifest reference does not bind exact run-relative bytes: %+v", reference)
	}
	retained, err := os.ReadFile(filepath.Join(run.Environment["output_dir"], reference.Path))
	if err != nil {
		t.Fatal(err)
	}
	if string(retained) != string(data) {
		t.Fatalf("manifest bytes changed: %s", retained)
	}
	if _, err := w.WriteManifest(run, data); err == nil {
		t.Fatal("identical second manifest overwrote first")
	}
	terminal := *run
	terminal.CompletedAt = run.StartedAt.Add(time.Second)
	zero := 0
	terminal.ExitCode = &zero
	if _, err := w.WriteManifest(&terminal, []byte(`{"entries":[]}`)); err == nil {
		t.Fatal("terminal snapshot wrote another manifest")
	}
	retained, err = os.ReadFile(filepath.Join(run.Environment["output_dir"], reference.Path))
	if err != nil || string(retained) != string(data) {
		t.Fatalf("rejected writes changed first manifest: %v %s", err, retained)
	}
	run.Scenarios[0] = completed
	run.Environment["artifact_manifest_path"] = reference.Path
	run.Environment["artifact_manifest_sha256"] = reference.SHA256
	run.CompletedAt, run.ExitCode = terminal.CompletedAt, terminal.ExitCode
	path, err := w.WriteRun(run)
	if err != nil {
		t.Fatal(err)
	}
	if run.EvidenceStatus != "complete" {
		t.Fatalf("retained manifest did not allow terminal proof: %+v", run)
	}
	if _, err := NewWriter(t.TempDir()).VerifyChild(path, ChildExpectation{ParentID: "parent-run", ParentMemberID: "statistical",
		Selection: run.Config.Selection, RequiredMembers: []string{"tiered:statistical"}}); err != nil {
		t.Fatalf("parent could not verify retained manifest: %v", err)
	}
}

func TestWriteManifestRejectsMalformedOrForeignInput(t *testing.T) {
	run := completeRunFixture(t)
	run.Scenarios[0].CheckObservations = nil
	run.Scenarios[0].Success = false
	run.Scenarios[0].EvidenceStatus = "unattested"
	run.Scenarios[0].AssertionsRun = 0
	run.CompletedAt = time.Time{}
	run.ExitCode = nil
	w := NewWriter(t.TempDir())
	if _, err := w.WriteRun(run); err != nil {
		t.Fatal(err)
	}
	for _, bad := range [][]byte{nil, []byte("null"), []byte("[]"), []byte("{"), []byte(`"string"`)} {
		if reference, err := w.WriteManifest(run, bad); err == nil || reference.RunID != "" || reference.Path != "" {
			t.Fatalf("invalid manifest accepted: ref=%+v err=%v", reference, err)
		}
	}
	foreign := *run
	foreign.ID = "other-run"
	if _, err := w.WriteManifest(&foreign, []byte(`{"entries":[]}`)); err == nil {
		t.Fatal("foreign run wrote manifest")
	}
}

func TestVerifyChildReferenceUsesRelativeWriterOutputDir(t *testing.T) {
	child := completeRunFixture(t)
	child.ParentID, child.ParentMemberID = "parent", "statistical"
	path, err := NewWriter(t.TempDir()).WriteRun(child)
	if err != nil {
		t.Fatal(err)
	}
	wd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	parentDir := t.TempDir()
	relativeDir, err := filepath.Rel(wd, parentDir)
	if err != nil {
		t.Fatal(err)
	}
	w := NewWriter(relativeDir)
	expected := ChildExpectation{ParentID: "parent", ParentMemberID: "statistical", Selection: child.Config.Selection,
		RequiredMembers: []string{"tiered:statistical"}}
	reference, err := w.VerifyChild(path, expected)
	if err != nil {
		t.Fatal(err)
	}
	if filepath.IsAbs(reference.Path) || reference.SHA256 == "" {
		t.Fatalf("child reference did not use relative writer output: %+v", reference)
	}
}

func TestWriteMemberIsNoClobberAndPreservesFailedPartial(t *testing.T) {
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
	if _, err := w.WriteRun(run); err != nil {
		t.Fatal(err)
	}
	failed := declared
	if err := failed.RecordCheck(scenarios.CheckObservation{ID: "controlled-search.identity", RunID: run.ID,
		MemberID: declared.MemberID, Status: "failed", Reason: "wrong entity"}); err != nil {
		t.Fatal(err)
	}
	if _, err := w.WriteMember(run, &failed); err != nil {
		t.Fatal(err)
	}
	passed := declared
	if err := passed.RecordCheck(scenarios.CheckObservation{ID: "controlled-search.identity", RunID: run.ID,
		MemberID: declared.MemberID, Status: "passed"}); err != nil {
		t.Fatal(err)
	}
	if _, err := w.WriteMember(run, &passed); err == nil {
		t.Fatal("later pass replaced retained failed member")
	}
	loaded, err := w.LoadMember(run, declared.MemberID)
	if err != nil {
		t.Fatal(err)
	}
	if loaded.Success || len(loaded.CheckObservations) != 1 || loaded.CheckObservations[0].Status != "failed" {
		t.Fatalf("failed partial member lost: %+v", loaded)
	}
	foreign := failed
	foreign.RunID = "other"
	if _, err := w.WriteMember(run, &foreign); err == nil {
		t.Fatal("foreign run member accepted")
	}
}

func TestWriteMemberFinalizesPassingObservationFromDeclaration(t *testing.T) {
	run := completeRunFixture(t)
	run.Config.Selection = "core.scenarios"
	run.Config.RequiredMembers = []string{"fixture.actual-comparison"}
	declared := run.Scenarios[0]
	declared.MemberID = "fixture.actual-comparison"
	declared.CheckObservations = nil
	declared.Success = false // initialized declarations have no execution outcome yet
	declared.EvidenceStatus = "unattested"
	declared.AssertionsRun = 0
	run.Scenarios = []scenarios.Result{declared}
	run.CompletedAt = time.Time{}
	run.ExitCode = nil
	w := NewWriter(t.TempDir())
	if _, err := w.WriteRun(run); err != nil {
		t.Fatal(err)
	}
	observed := declared
	if err := observed.RecordCheck(scenarios.CheckObservation{ID: "controlled-search.identity", RunID: run.ID,
		MemberID: declared.MemberID, Status: "passed"}); err != nil {
		t.Fatal(err)
	}
	if _, err := w.WriteMember(run, &observed); err != nil {
		t.Fatal(err)
	}
	loaded, err := w.LoadMember(run, declared.MemberID)
	if err != nil {
		t.Fatal(err)
	}
	if !loaded.Success || loaded.EvidenceStatus != "complete" || loaded.AssertionsRun != 1 {
		t.Fatalf("passed observed member did not finalize: %+v", loaded)
	}
}

func TestLoadRunRejectsForgedCompleteWithoutRetainedReferences(t *testing.T) {
	run := completeRunFixture(t)
	w := NewWriter(t.TempDir())
	path, err := w.WriteRun(run)
	if err != nil {
		t.Fatal(err)
	}
	for _, key := range []string{"log_path", "log_sha256", "artifact_manifest_path", "artifact_manifest_sha256", "output_dir"} {
		t.Run(key, func(t *testing.T) {
			copyRun := *run
			copyRun.Environment = make(map[string]string, len(run.Environment))
			for k, v := range run.Environment {
				copyRun.Environment[k] = v
			}
			delete(copyRun.Environment, key)
			data, err := json.Marshal(&copyRun)
			if err != nil {
				t.Fatal(err)
			}
			forged := filepath.Join(t.TempDir(), "forged.json")
			if err := os.WriteFile(forged, data, 0644); err != nil {
				t.Fatal(err)
			}
			if _, err := w.LoadRun(forged); err == nil {
				t.Fatalf("forged complete report without %s accepted (source %s)", key, path)
			}
		})
	}
}

func TestCompareKeepsUnattestedDispositionVisible(t *testing.T) {
	baseline := &TestRun{ID: "old", EvidenceStatus: "unattested", Scenarios: []scenarios.Result{{ScenarioName: "tiered", Success: true}}}
	current := &TestRun{ID: "new", EvidenceStatus: "complete", Scenarios: []scenarios.Result{{ScenarioName: "tiered", Success: true}}}
	compared := Compare(baseline, current)
	data, err := json.Marshal(compared)
	if err != nil {
		t.Fatal(err)
	}
	var encoded map[string]any
	if err := json.Unmarshal(data, &encoded); err != nil {
		t.Fatal(err)
	}
	if encoded["baseline_evidence_status"] != "unattested" || encoded["current_evidence_status"] != "complete" {
		t.Fatalf("comparison erased legacy proof limit: %s", data)
	}
}

func FuzzVerifyChildRejectsForeignOrIncompleteBytes(f *testing.F) {
	child := completeRunFixture(f)
	child.ParentID, child.ParentMemberID = "parent-run", "statistical"
	path, err := NewWriter(f.TempDir()).WriteRun(child)
	if err != nil {
		f.Fatal(err)
	}
	valid, err := os.ReadFile(path)
	if err != nil {
		f.Fatal(err)
	}
	f.Add(valid)
	f.Add([]byte(`{"id":"old","summary":{"all_passed":true}}`))
	f.Add([]byte(`{"schema_version":99,"evidence_status":"complete"}`))
	f.Add([]byte(`{"schema_version":2,"parent_id":"foreign","parent_member_id":"statistical"}`))
	f.Add([]byte(`not-json`))
	expected := ChildExpectation{ParentID: "parent-run", ParentMemberID: "statistical", Selection: child.Config.Selection,
		RequiredMembers: []string{"tiered:statistical"}}
	f.Fuzz(func(t *testing.T, data []byte) {
		if len(data) > 1<<16 {
			t.Skip()
		}
		w := NewWriter(t.TempDir())
		path := filepath.Join(t.TempDir(), "child.json")
		if err := os.WriteFile(path, data, 0644); err != nil {
			t.Fatal(err)
		}
		artifact, err := w.VerifyChild(path, expected)
		if err != nil {
			return
		}
		if artifact.SHA256 != fmtSHA256(data) || artifact.RunID == "" || artifact.Path == "" {
			t.Fatalf("verified child does not identify exact bytes: %+v", artifact)
		}
	})
}

func FuzzLoadMemberPreservesInitializedIdentity(f *testing.F) {
	f.Add([]byte("valid-member-fixture"))
	f.Add([]byte(`{"run_id":"other","member_id":"tiered:statistical"}`))
	f.Add([]byte(`{"run_id":"run","member_id":"other"}`))
	f.Add([]byte(`not-json`))
	f.Fuzz(func(t *testing.T, data []byte) {
		if len(data) > 1<<16 {
			t.Skip()
		}
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
		if _, err := w.WriteRun(run); err != nil {
			t.Fatal(err)
		}
		path, err := w.memberPath(run, declared.MemberID)
		if err != nil {
			t.Fatal(err)
		}
		if string(data) == "valid-member-fixture" {
			valid := declared
			if err := valid.RecordCheck(scenarios.CheckObservation{ID: "controlled-search.identity", RunID: run.ID,
				MemberID: declared.MemberID, Status: "passed"}); err != nil {
				t.Fatal(err)
			}
			if err := valid.FinalizeChecks(); err != nil {
				t.Fatal(err)
			}
			data, err = json.Marshal(valid)
			if err != nil {
				t.Fatal(err)
			}
		}
		if err := os.WriteFile(path, data, 0644); err != nil {
			t.Fatal(err)
		}
		member, err := w.LoadMember(run, declared.MemberID)
		if err != nil {
			return
		}
		if member.RunID != run.ID || member.MemberID != declared.MemberID ||
			len(member.CheckRequirements) != len(declared.CheckRequirements) {
			t.Fatalf("loaded member escaped initialized identity: %+v", member)
		}
	})
}

func FuzzWriteManifestObjectBoundary(f *testing.F) {
	f.Add([]byte(`{"entries":[]}`))
	f.Add([]byte(`{"entries":[{"role":"app_phase"}]}`))
	f.Add([]byte(`null`))
	f.Add([]byte(`[]`))
	f.Add([]byte(`"string"`))
	f.Add([]byte(`{`))
	f.Add([]byte{})
	f.Fuzz(func(t *testing.T, data []byte) {
		if len(data) > 1<<16 {
			t.Skip()
		}
		run := completeRunFixture(t)
		run.Scenarios[0].CheckObservations = nil
		run.Scenarios[0].Success = false
		run.Scenarios[0].EvidenceStatus = "unattested"
		run.Scenarios[0].AssertionsRun = 0
		run.CompletedAt = time.Time{}
		run.ExitCode = nil
		w := NewWriter(t.TempDir())
		if _, err := w.WriteRun(run); err != nil {
			t.Fatal(err)
		}
		reference, err := w.WriteManifest(run, data)
		if err != nil {
			if reference.RunID != "" || reference.Path != "" || reference.SHA256 != "" {
				t.Fatalf("failed manifest returned retained reference: %+v", reference)
			}
			return
		}
		trimmed := bytes.TrimSpace(data)
		if !json.Valid(data) || len(trimmed) == 0 || trimmed[0] != '{' || reference.RunID != run.ID || reference.SHA256 != fmtSHA256(data) {
			t.Fatalf("accepted manifest does not bind JSON object bytes: %+v", reference)
		}
		retained, err := os.ReadFile(filepath.Join(run.Environment["output_dir"], reference.Path))
		if err != nil || string(retained) != string(data) {
			t.Fatalf("retained manifest differs: %v", err)
		}
	})
}

func fmtSHA256(data []byte) string {
	sum := sha256.Sum256(data)
	return fmt.Sprintf("%x", sum)
}

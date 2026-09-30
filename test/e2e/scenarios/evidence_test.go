package scenarios

import (
	"strings"
	"testing"

	"pgregory.net/rapid"
)

func TestRequiredChecksUseObservationsRatherThanStageSuccess(t *testing.T) {
	for _, tc := range []struct {
		name     string
		status   string
		observe  bool
		wantPass bool
		wantRun  int
	}{
		{name: "passed", status: "passed", observe: true, wantPass: true, wantRun: 1},
		{name: "failed", status: "failed", observe: true, wantRun: 1},
		{name: "skipped", status: "skipped", observe: true},
		{name: "missing"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r := &Result{Success: true, AssertionsRun: 20}
			if err := r.DeclareChecks("run-1", "member-1", []CheckRequirement{{ID: "tool.request-result", Required: true}}); err != nil {
				t.Fatal(err)
			}
			if tc.observe {
				observation := CheckObservation{ID: "tool.request-result", RunID: "run-1", MemberID: "member-1", Status: tc.status}
				if tc.status != "passed" {
					observation.Reason = "controlled observation failed"
				}
				if err := r.RecordCheck(observation); err != nil {
					t.Fatal(err)
				}
			}
			err := r.FinalizeChecks()
			if tc.wantPass && err != nil || !tc.wantPass && err == nil {
				t.Fatalf("FinalizeChecks error = %v, want success %t", err, tc.wantPass)
			}
			if r.Success != tc.wantPass || r.AssertionsRun != tc.wantRun {
				t.Fatalf("success/count = %t/%d, want %t/%d", r.Success, r.AssertionsRun, tc.wantPass, tc.wantRun)
			}
			if tc.wantPass && r.EvidenceStatus != "complete" || !tc.wantPass && r.EvidenceStatus == "complete" {
				t.Fatalf("evidence status %q with success %t", r.EvidenceStatus, tc.wantPass)
			}
			if !tc.wantPass && !strings.Contains(err.Error(), "tool.request-result") {
				t.Fatalf("failure does not name required check: %v", err)
			}
		})
	}
}

func TestCheckRefusalCannotBeIgnoredOrOverwritten(t *testing.T) {
	for _, tc := range []struct {
		name        string
		observation CheckObservation
		first       *CheckObservation
	}{
		{name: "unknown", observation: CheckObservation{ID: "other", RunID: "run-1", MemberID: "member-1", Status: "passed"}},
		{name: "foreign run", observation: CheckObservation{ID: "required", RunID: "run-old", MemberID: "member-1", Status: "passed"}},
		{name: "foreign member", observation: CheckObservation{ID: "required", RunID: "run-1", MemberID: "other", Status: "passed"}},
		{name: "unknown status", observation: CheckObservation{ID: "required", RunID: "run-1", MemberID: "member-1", Status: "done"}},
		{name: "overwrite failure", first: &CheckObservation{ID: "required", RunID: "run-1", MemberID: "member-1", Status: "failed", Reason: "wrong result"}, observation: CheckObservation{ID: "required", RunID: "run-1", MemberID: "member-1", Status: "passed"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r := &Result{}
			if err := r.DeclareChecks("run-1", "member-1", []CheckRequirement{{ID: "required", Required: true}}); err != nil {
				t.Fatal(err)
			}
			if tc.first != nil {
				if err := r.RecordCheck(*tc.first); err != nil {
					t.Fatal(err)
				}
			}
			if err := r.RecordCheck(tc.observation); err == nil {
				t.Fatal("invalid observation was accepted")
			}
			if tc.first == nil {
				// A later valid observation must not clear the attempted violation.
				_ = r.RecordCheck(CheckObservation{ID: "required", RunID: "run-1", MemberID: "member-1", Status: "passed"})
			}
			if err := r.FinalizeChecks(); err == nil || r.Success {
				t.Fatalf("ignored record error yielded success: %v, %+v", err, r)
			}
			if tc.first != nil && (len(r.CheckObservations) != 1 || r.CheckObservations[0].Status != "failed") {
				t.Fatalf("original failure overwritten: %+v", r.CheckObservations)
			}
		})
	}
}

func TestDiagnosticFailureDoesNotSatisfyOrFailRequiredProof(t *testing.T) {
	r := &Result{}
	checks := []CheckRequirement{{ID: "streaming.chunks", Required: true}, {ID: "ttft", Required: false}}
	if err := r.DeclareChecks("run-1", "agentic", checks); err != nil {
		t.Fatal(err)
	}
	for _, observation := range []CheckObservation{
		{ID: "streaming.chunks", RunID: "run-1", MemberID: "agentic", Status: "passed"},
		{ID: "ttft", RunID: "run-1", MemberID: "agentic", Status: "skipped", Reason: "metric absent"},
	} {
		if err := r.RecordCheck(observation); err != nil {
			t.Fatal(err)
		}
	}
	if err := r.FinalizeChecks(); err != nil || !r.Success || r.AssertionsRun != 1 {
		t.Fatalf("diagnostic changed required proof: err=%v result=%+v", err, r)
	}
	if r.CheckObservations[1].Status != "skipped" || r.CheckObservations[1].Reason != "metric absent" {
		t.Fatalf("diagnostic was lost: %+v", r.CheckObservations)
	}
}

func TestFinalizationKeepsExecutionErrorAndIsRepeatable(t *testing.T) {
	r := &Result{Error: "teardown failed"}
	if err := r.DeclareChecks("run-1", "member-1", []CheckRequirement{{ID: "required", Required: true}}); err != nil {
		t.Fatal(err)
	}
	if err := r.RecordCheck(CheckObservation{ID: "required", RunID: "run-1", MemberID: "member-1", Status: "passed"}); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 2; i++ {
		if err := r.FinalizeChecks(); err == nil || r.Success {
			t.Fatalf("execution error was cleared on pass %d: %+v", i, r)
		}
		if len(r.Errors) != 0 {
			t.Fatalf("derived errors accumulated on pass %d: %+v", i, r.Errors)
		}
	}
}

func TestCheckDeclarationRejectsEmptyDuplicateAndDiagnosticOnlySets(t *testing.T) {
	for _, tc := range []struct {
		name   string
		checks []CheckRequirement
	}{
		{name: "empty"},
		{name: "duplicate", checks: []CheckRequirement{{ID: "required", Required: true}, {ID: "required", Required: true}}},
		{name: "diagnostic only", checks: []CheckRequirement{{ID: "ttft", Required: false}}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r := &Result{Success: true}
			if err := r.DeclareChecks("run-1", "member-1", tc.checks); err == nil {
				t.Fatal("invalid required membership was declared")
			}
			if err := r.FinalizeChecks(); err == nil || r.Success || r.EvidenceStatus == "complete" {
				t.Fatalf("invalid declaration yielded proof: err=%v result=%+v", err, r)
			}
		})
	}
}

// spec: e2e-evidence / Required observations determine success
func TestRequiredCheckSetProperty(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		n := rapid.IntRange(1, 6).Draw(t, "required_count")
		checks := make([]CheckRequirement, n)
		statuses := make([]int, n)
		allPassed := true
		for i := range checks {
			checks[i] = CheckRequirement{ID: "check-" + string(rune('a'+i)), Required: true}
			statuses[i] = rapid.IntRange(0, 3).Draw(t, "status") // passed, failed, skipped, missing
			if statuses[i] != 0 {
				allPassed = false
			}
		}
		r := &Result{}
		if err := r.DeclareChecks("run-1", "member-1", checks); err != nil {
			t.Fatal(err)
		}
		for i, status := range statuses {
			if status == 3 {
				continue
			}
			observation := CheckObservation{ID: checks[i].ID, RunID: "run-1", MemberID: "member-1"}
			switch status {
			case 0:
				observation.Status = "passed"
			case 1:
				observation.Status, observation.Reason = "failed", "wrong value"
			case 2:
				observation.Status, observation.Reason = "skipped", "unavailable"
			}
			if err := r.RecordCheck(observation); err != nil {
				t.Fatal(err)
			}
		}
		err := r.FinalizeChecks()
		if (err == nil) != allPassed || r.Success != allPassed {
			t.Fatalf("required statuses %v: err=%v success=%t want=%t", statuses, err, r.Success, allPassed)
		}
	})
}

// spec: e2e-evidence / Evidence cannot improve by omission
func TestDeletingARequiredObservationCannotCreateSuccess(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		n := rapid.IntRange(1, 6).Draw(t, "required_count")
		remove := rapid.IntRange(0, n-1).Draw(t, "removed_index")
		r := &Result{}
		checks := make([]CheckRequirement, n)
		for i := range checks {
			checks[i] = CheckRequirement{ID: "check-" + string(rune('a'+i)), Required: true}
		}
		if err := r.DeclareChecks("run-1", "member-1", checks); err != nil {
			t.Fatal(err)
		}
		for i := range checks {
			if i == remove {
				continue
			}
			if err := r.RecordCheck(CheckObservation{ID: checks[i].ID, RunID: "run-1", MemberID: "member-1", Status: "passed"}); err != nil {
				t.Fatal(err)
			}
		}
		if err := r.FinalizeChecks(); err == nil || r.Success {
			t.Fatalf("omission of %s passed: %+v", checks[remove].ID, r)
		}
	})
}

func FuzzRecordCheckCannotAcceptForeignOrUnknownOutcome(f *testing.F) {
	f.Add("required", "run-1", "member-1", "passed", "")
	f.Add("required", "run-1", "member-1", "failed", "wrong identity")
	f.Add("required", "run-1", "member-1", "skipped", "observation unavailable")
	f.Add("required", "run-old", "member-1", "passed", "")
	f.Add("other", "run-1", "member-1", "passed", "")
	f.Add("required", "run-1", "member-1", "unknown", "")
	f.Fuzz(func(t *testing.T, id, runID, memberID, status, reason string) {
		if len(id)+len(runID)+len(memberID)+len(status)+len(reason) > 4096 {
			t.Skip()
		}
		r := &Result{}
		if err := r.DeclareChecks("run-1", "member-1", []CheckRequirement{{ID: "required", Required: true}}); err != nil {
			t.Fatal(err)
		}
		_ = r.RecordCheck(CheckObservation{ID: id, RunID: runID, MemberID: memberID, Status: status, Reason: reason})
		err := r.FinalizeChecks()
		want := id == "required" && runID == "run-1" && memberID == "member-1" && status == "passed"
		if (err == nil) != want || r.Success != want {
			t.Fatalf("id=%q run=%q member=%q status=%q: err=%v success=%t want=%t", id, runID, memberID, status, err, r.Success, want)
		}
	})
}

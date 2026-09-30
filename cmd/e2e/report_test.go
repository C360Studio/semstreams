package main

import (
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/c360studio/semstreams/test/e2e/results"
	"github.com/c360studio/semstreams/test/e2e/scenarios"
)

func TestTaskReportScopeDeclaresCoreAtAssertionSites(t *testing.T) {
	scope, err := resolveTaskReportScope("core")
	if err != nil {
		t.Fatal(err)
	}
	want := []string{
		"core.readiness", "core.heartbeat", "core.minted-authority",
		"core.core-scenarios", "core.shutdown.exit", "core.shutdown.log",
		"core.shutdown.listeners", "core.early-cancel.exit",
		"core.early-cancel.no-services", "core.preidentity.refusal",
		"core.preidentity.no-record", "core.graph-roundtrip.identity",
	}
	if got := scope.memberIDs(); !reflect.DeepEqual(got, want) {
		t.Fatalf("core declarations = %v, want %v", got, want)
	}
	for _, member := range scope.members {
		if len(member.checks) != 1 || member.checks[0].ID != member.id || !member.checks[0].Required {
			t.Errorf("member %q has no single named required observation: %+v", member.id, member.checks)
		}
	}
	for _, absent := range []string{"core.preidentity.seed", "core.early-cancel.setup"} {
		for _, id := range scope.memberIDs() {
			if id == absent {
				t.Errorf("setup %q was declared as behavioral proof", absent)
			}
		}
	}
}

// spec: e2e-evidence / Run evidence binds one invocation
func TestCompositeProvenanceUsesVerifiedChildBytes(t *testing.T) {
	root := t.TempDir()
	write := func(name, contents string) string {
		t.Helper()
		path := filepath.Join(root, name)
		if err := os.WriteFile(path, []byte(contents), 0600); err != nil {
			t.Fatal(err)
		}
		return path
	}
	member, err := taskChildReportMember("structural")
	if err != nil {
		t.Fatal(err)
	}
	parent, parentPath, err := initializeTaskReport(root, taskReportScope{
		selection: "fixture-composite", members: []taskReportMember{member},
	}, "", "", []string{"task", "e2e:fixture-composite"})
	if err != nil {
		t.Fatal(err)
	}
	if parent.Environment["command_scope"] != "resolved Task target" ||
		parent.Environment["outer_launcher_argv"] == "" {
		t.Fatalf("Task argv limitation was hidden: %+v", parent.Environment)
	}
	compose := write("compose.yml", "services: {}")
	config := write("config.json", "{}")
	fixture := write("fixture.jsonl", "{}")
	appBinary := write("app.bin", "application bytes")
	logPath := write("child.log", "child finished")
	childManifest := reportManifest{
		Files: []reportManifestFile{
			{Role: "compose", Path: compose, SHA256: testDigest(t, compose)},
			{Role: "config", Path: config, SHA256: testDigest(t, config)},
			{Role: "fixture", Path: fixture, SHA256: testDigest(t, fixture)},
		},
		AppPhases: []reportManifestAppPhase{{
			Name: "production", ImageID: "sha256:" + repeatHex('a'),
			ImageDigest: "unavailable: locally built image has no registry digest",
			BinaryPath:  appBinary, BinarySHA256: testDigest(t, appBinary), Build: "observed build",
		}},
	}
	manifestBytes, err := json.Marshal(childManifest)
	if err != nil {
		t.Fatal(err)
	}
	manifestPath := write("child-manifest.json", string(manifestBytes))
	child := results.CreateTestRun(results.TestRunConfig{
		Selection: "structural", RequireEvidence: true, RequireTaskStatuses: true,
		RequiredMembers: []string{"structural"},
	}, nil, nil, 0)
	child.ParentID, child.ParentMemberID = parent.ID, "structural"
	child.Command = []string{"task", "e2e:structural"}
	child.WorkingDir = root
	proof := scenarios.Result{ScenarioName: "structural"}
	if err := proof.DeclareChecks(child.ID, "structural", []scenarios.CheckRequirement{{ID: "structural", Required: true}}); err != nil {
		t.Fatal(err)
	}
	if err := proof.RecordCheck(scenarios.CheckObservation{ID: "structural", RunID: child.ID,
		MemberID: "structural", Status: "passed"}); err != nil {
		t.Fatal(err)
	}
	if err := proof.FinalizeChecks(); err != nil {
		t.Fatal(err)
	}
	child.Scenarios = []scenarios.Result{proof}
	child.CompletedAt = child.StartedAt.Add(1)
	zero := 0
	child.ExitCode, child.CommandExitCode, child.CleanupExitCode = &zero, &zero, &zero
	child.Environment = map[string]string{
		"source_sha": "0123456789012345678901234567890123456789", "source_dirty": "false",
		"source_patch_sha256":     "not_applicable: clean source",
		"source_untracked_sha256": "not_applicable: clean source",
		"runner_sha256":           repeatHex('b'), "runner_build": "go test",
		"app_image_id":      childManifest.AppPhases[0].ImageID,
		"app_image_digest":  childManifest.AppPhases[0].ImageDigest,
		"app_binary_sha256": testDigest(t, appBinary), "app_build": "observed build",
		"compose_sha256": testDigest(t, compose), "config_sha256": testDigest(t, config),
		"fixture_sha256": testDigest(t, fixture), "profiles": "structural",
		"effective_settings_sha256": repeatHex('c'),
		"log_path":                  logPath, "log_sha256": testDigest(t, logPath),
		"artifact_manifest_path": manifestPath, "artifact_manifest_sha256": testDigest(t, manifestPath),
	}
	childPath, err := results.NewWriter(root).WriteRun(child)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := recordTaskChild(parentPath, "structural", childPath, 0, logPath,
		`["task","e2e:structural"]`); err != nil {
		loaded, _ := results.NewWriter(root).LoadRun(childPath)
		t.Fatalf("%v; child=%+v", err, loaded)
	}
	observed, err := results.NewWriter(root).LoadMember(parent, "structural")
	if err != nil {
		t.Fatal(err)
	}
	input := reportManifestInput{OutputDir: root, Selection: "fixture-composite"}
	if err := appendCompositeProvenance(parent, results.NewWriter(root), []scenarios.Result{*observed}, &input); err != nil {
		t.Fatal(err)
	}
	if len(input.AppPhases) != 1 || input.AppPhases[0].Name != "structural.production" ||
		len(input.Files) != 5 {
		t.Fatalf("verified composite provenance lost child artifacts or constituents: %+v", input)
	}
	if err := os.WriteFile(appBinary, []byte("changed bytes"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := appendCompositeProvenance(parent, results.NewWriter(root), []scenarios.Result{*observed},
		&reportManifestInput{OutputDir: root, Selection: "fixture-composite"}); err == nil {
		t.Fatal("changed child application bytes were accepted")
	}
}

func testDigest(t *testing.T, path string) string {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return fmt.Sprintf("%x", sha256.Sum256(data))
}

func TestTaskReportScopeUsesCLIResolverForChildMembership(t *testing.T) {
	for _, tc := range []struct {
		task      string
		selection string
		members   []string
	}{
		{"structural", "structural", []string{"tiered:structural"}},
		{"statistical", "statistical", []string{"tiered:statistical"}},
		{"semantic", "semantic", []string{"tiered:semantic"}},
		{"agentic", "agentic", []string{"agentic"}},
		{"slow-consumer", "core-slow-consumer", []string{"core-slow-consumer"}},
	} {
		t.Run(tc.task, func(t *testing.T) {
			scope, err := resolveTaskReportScope(tc.task)
			if err != nil {
				t.Fatal(err)
			}
			if len(scope.members) != 1 {
				t.Fatalf("members = %d, want one child", len(scope.members))
			}
			child := scope.members[0].child
			if child == nil || child.selection != tc.selection || !reflect.DeepEqual(child.requiredMembers, tc.members) {
				t.Errorf("child = %+v, want selection %q members %v", child, tc.selection, tc.members)
			}
		})
	}
}

func TestTaskReportCompositeHasExactFiveFamilyScope(t *testing.T) {
	scope, err := resolveTaskReportScope("core-inference-agentic")
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"core", "structural", "statistical", "semantic", "agentic"}
	if got := scope.memberIDs(); !reflect.DeepEqual(got, want) {
		t.Fatalf("composite declarations = %v, want %v", got, want)
	}
	for _, member := range scope.members {
		if member.child == nil || member.child.selection != member.id || !member.child.requireTaskStatuses {
			t.Errorf("composite member %q is not bound to required child Task evidence: %+v", member.id, member.child)
		}
	}
	if len(scope.excluded) == 0 {
		t.Fatal("composite exclusions must be visible")
	}
}

func TestTaskReportScopeRejectsUnknownSelection(t *testing.T) {
	for _, selection := range []string{"", "all", "semantic-fallback", "lessons", "typo"} {
		if _, err := resolveTaskReportScope(selection); err == nil {
			t.Errorf("selection %q was accepted", selection)
		}
	}
}

func TestReportManifestRetainsConstituentsAndTwoCoreAppPhases(t *testing.T) {
	root := t.TempDir()
	file := func(name, contents string) string {
		t.Helper()
		path := filepath.Join(root, name)
		if err := os.WriteFile(path, []byte(contents), 0600); err != nil {
			t.Fatal(err)
		}
		return path
	}
	configPath := file("protocol-flow.json", `{"id":"controlled"}`)
	logPath := file("task.log", "observed run log\n")
	productionBinary := file("semstreams-production", "production bytes")
	fixtureBinary := file("semstreams-fixtures", "fixture bytes")
	input := reportManifestInput{
		OutputDir: root,
		Selection: "core",
		Files: []reportManifestFileInput{
			{Role: "config", Path: configPath},
			{Role: "task_log", Path: logPath},
		},
		AppPhases: []reportManifestAppInput{
			{Name: "production", ImageID: "sha256:" + repeatHex('a'), ImageDigest: "unavailable: locally built image has no registry digest", BinaryPath: productionBinary, Build: "observed production build"},
			{Name: "fixtures", ImageID: "sha256:" + repeatHex('b'), ImageDigest: "unavailable: locally built image has no registry digest", BinaryPath: fixtureBinary, Build: "observed fixture build"},
		},
	}
	manifest, err := buildReportManifest(input)
	if err != nil {
		t.Fatal(err)
	}
	if manifest.OutputDir != root || len(manifest.Files) != 3 || len(manifest.AppPhases) != 2 {
		t.Fatalf("lost output, file, or phase provenance: %+v", manifest)
	}
	wantConfigDigest := fmt.Sprintf("%x", sha256.Sum256([]byte(`{"id":"controlled"}`)))
	if manifest.Files[0].SHA256 != wantConfigDigest || manifest.Files[0].Path != configPath {
		t.Errorf("config constituent = %+v, want path and digest %s", manifest.Files[0], wantConfigDigest)
	}
	wantProductionDigest := fmt.Sprintf("%x", sha256.Sum256([]byte("production bytes")))
	wantFixtureDigest := fmt.Sprintf("%x", sha256.Sum256([]byte("fixture bytes")))
	if manifest.AppPhases[0].BinarySHA256 != wantProductionDigest ||
		manifest.AppPhases[1].BinarySHA256 != wantFixtureDigest ||
		manifest.AppPhases[0].Name == manifest.AppPhases[1].Name {
		t.Errorf("core app phases were lost or conflated: %+v", manifest.AppPhases)
	}
	env := reportManifestEnvironment(manifest)
	if env["app_image_id"] != input.AppPhases[0].ImageID ||
		env["log_path"] != logPath || env["log_sha256"] != manifest.Files[1].SHA256 {
		t.Errorf("aggregate provenance did not preserve selected production/log references: %+v", env)
	}
}

func TestReportManifestRejectsMissingOrGuessedProvenance(t *testing.T) {
	root := t.TempDir()
	binaryPath := filepath.Join(root, "observed-binary")
	if err := os.WriteFile(binaryPath, []byte("observed bytes"), 0600); err != nil {
		t.Fatal(err)
	}
	valid := reportManifestAppInput{Name: "production", ImageID: "sha256:" + repeatHex('a'),
		ImageDigest: "unavailable: locally built image has no registry digest", BinaryPath: binaryPath, Build: "observed build"}
	for _, tc := range []struct {
		name  string
		input reportManifestInput
	}{
		{name: "relative output", input: reportManifestInput{OutputDir: "relative", Selection: "core", AppPhases: []reportManifestAppInput{valid}}},
		{name: "missing constituent", input: reportManifestInput{OutputDir: root, Selection: "core",
			Files: []reportManifestFileInput{{Role: "config", Path: filepath.Join(root, "absent")}}, AppPhases: []reportManifestAppInput{valid}}},
		{name: "guessed tag", input: reportManifestInput{OutputDir: root, Selection: "core",
			AppPhases: []reportManifestAppInput{{Name: "production", ImageID: "c360studio/semstreams:latest", ImageDigest: valid.ImageDigest, BinaryPath: binaryPath, Build: "observed build"}}}},
		{name: "duplicate phase", input: reportManifestInput{OutputDir: root, Selection: "core",
			AppPhases: []reportManifestAppInput{valid, valid}}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if _, err := buildReportManifest(tc.input); err == nil {
				t.Fatal("accepted missing, relative, or guessed provenance")
			}
		})
	}
}

func repeatHex(char byte) string {
	result := make([]byte, 64)
	for i := range result {
		result[i] = char
	}
	return string(result)
}

func TestTaskReportInitAndImmutableObservation(t *testing.T) {
	scope := taskReportScope{selection: "fixture-task", members: []taskReportMember{shellReportMember("fixture.actual-comparison")}}
	run, path, err := initializeTaskReport(t.TempDir(), scope, "", "", []string{"task", "fixture-task"})
	if err != nil {
		t.Fatal(err)
	}
	writer := results.NewWriter(filepath.Dir(path))
	initial, err := writer.LoadRun(path)
	if err != nil {
		t.Fatal(err)
	}
	if initial.ID != run.ID || initial.ExitCode != nil || initial.Config.RequireTaskStatuses != true ||
		len(initial.Scenarios) != 1 || len(initial.Scenarios[0].CheckObservations) != 0 {
		t.Fatalf("report initialization did not retain exact incomplete declarations: %+v", initial)
	}
	observation := scenarios.CheckObservation{ID: "fixture.actual-comparison", RunID: run.ID,
		MemberID: "fixture.actual-comparison", Status: "failed", Reason: "observed value differs",
		Evidence: map[string]string{"expected": "alpha", "observed": "beta"}}
	if _, err := recordTaskObservation(path, observation); err != nil {
		t.Fatal(err)
	}
	stored, err := writer.LoadMember(initial, observation.MemberID)
	if err != nil {
		t.Fatal(err)
	}
	if stored.Success || len(stored.CheckObservations) != 1 || stored.CheckObservations[0].Status != "failed" {
		t.Fatalf("failed comparison was not retained: %+v", stored)
	}
	observation.Status, observation.Reason = "passed", ""
	if _, err := recordTaskObservation(path, observation); err == nil {
		t.Fatal("later pass overwrote failed comparison")
	}
	stored, err = writer.LoadMember(initial, observation.MemberID)
	if err != nil || stored.CheckObservations[0].Status != "failed" {
		t.Fatalf("original failed observation changed after duplicate: %+v, %v", stored, err)
	}
}

func TestTaskReportInitRefusesUnadoptedCLIChildBeforeWriting(t *testing.T) {
	legacy, err := cliReportMember("fixture.legacy", "lessons")
	if err != nil {
		t.Fatal(err)
	}
	scope := taskReportScope{selection: "fixture-task", members: []taskReportMember{legacy}}
	root := t.TempDir()
	if _, _, err := initializeTaskReport(root, scope, "", "", []string{"task", "fixture-task"}); err == nil {
		t.Fatal("unadopted child was accepted before Compose")
	}
	entries, err := os.ReadDir(root)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 0 {
		t.Fatalf("unadopted child wrote an initialized artifact: %v", entries)
	}
}

func TestTaskReportDispatchRecordsTypedObservation(t *testing.T) {
	scope := taskReportScope{selection: "fixture-task", members: []taskReportMember{shellReportMember("fixture.observed")}}
	run, path, err := initializeTaskReport(t.TempDir(), scope, "", "", []string{"task", "fixture-task"})
	if err != nil {
		t.Fatal(err)
	}
	observation := scenarios.CheckObservation{ID: "fixture.observed", RunID: run.ID,
		MemberID: "fixture.observed", Status: "passed",
		Evidence: map[string]string{"expected": "one", "observed": "one"}}
	data, err := json.Marshal(observation)
	if err != nil {
		t.Fatal(err)
	}
	inputPath := filepath.Join(t.TempDir(), "observation.json")
	if err := os.WriteFile(inputPath, data, 0600); err != nil {
		t.Fatal(err)
	}
	handled, code := handleTaskReportCommand(&cliFlags{reportRecord: true,
		reportRunPath: path, reportInputPath: inputPath})
	if !handled || code != 0 {
		t.Fatalf("actual report dispatch rejected valid observation: handled=%v exit=%d", handled, code)
	}
	stored, err := results.NewWriter(filepath.Dir(path)).LoadMember(run, "fixture.observed")
	if err != nil || !stored.Success {
		t.Fatalf("actual report dispatch did not retain passed comparison: %+v, %v", stored, err)
	}
}

// spec: e2e-evidence / Failure reaches the outer gate
// spec: e2e-evidence / Run evidence binds one invocation
func TestTaskReportFinalizeRetainsBothProcessFailuresAndObservedMember(t *testing.T) {
	root := t.TempDir()
	write := func(name, data string) string {
		t.Helper()
		path := filepath.Join(root, name)
		if err := os.WriteFile(path, []byte(data), 0600); err != nil {
			t.Fatal(err)
		}
		return path
	}
	input := reportManifestInput{
		OutputDir: root, Selection: "fixture-task",
		Profiles: "fixture",
		Files: []reportManifestFileInput{
			{Role: "compose", Path: write("compose.yml", "services: {}\n")},
			{Role: "config", Path: write("config.json", "{}\n")},
			{Role: "fixture", Path: write("fixture.json", "{}\n")},
			{Role: "task_log", Path: write("task.log", "command failed then cleanup failed\n")},
		},
		AppPhases: []reportManifestAppInput{{Name: "production", ImageID: "sha256:" + repeatHex('a'),
			ImageDigest: "unavailable: locally built image has no registry digest",
			BinaryPath:  write("app-binary", "observed app bytes"), Build: "observed app build"}},
	}
	scope := taskReportScope{selection: "fixture-task", members: []taskReportMember{shellReportMember("fixture.actual")}}
	run, path, err := initializeTaskReport(root, scope, "", "", []string{"task", "fixture-task"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := recordTaskObservation(path, scenarios.CheckObservation{
		ID: "fixture.actual", RunID: run.ID, MemberID: "fixture.actual", Status: "passed",
		Evidence: map[string]string{"expected": "one", "observed": "one"},
	}); err != nil {
		t.Fatal(err)
	}
	final, err := finalizeTaskReport(path, 3, 7, input)
	if err == nil {
		t.Fatal("command and cleanup failures were accepted")
	}
	if final.ExitCode == 0 || final.Path != path {
		t.Fatalf("lost terminal path or failure: %+v, %v", final, err)
	}
	stored, loadErr := results.NewWriter(root).LoadRun(path)
	if loadErr != nil {
		t.Fatal(loadErr)
	}
	if stored.CommandExitCode == nil || *stored.CommandExitCode != 3 ||
		stored.CleanupExitCode == nil || *stored.CleanupExitCode != 7 ||
		stored.ExitCode == nil || *stored.ExitCode == 0 || stored.Scenarios[0].CheckObservations[0].Status != "passed" {
		t.Fatalf("terminal report lost observed status or member: %+v", stored)
	}
	manifestPath := filepath.Join(root, stored.Environment["artifact_manifest_path"])
	manifestData, readErr := os.ReadFile(manifestPath)
	if readErr != nil {
		t.Fatal(readErr)
	}
	if stored.Environment["artifact_manifest_sha256"] != fmt.Sprintf("%x", sha256.Sum256(manifestData)) {
		t.Fatal("manifest reference digest differs from retained exact bytes")
	}
}

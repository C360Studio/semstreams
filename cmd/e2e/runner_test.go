package main

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/c360studio/semstreams/test/e2e/results"
	scenarios "github.com/c360studio/semstreams/test/e2e/scenarios"
)

type observedFixtureScenario struct {
	runID                 string
	memberID              string
	want                  string
	got                   string
	omit                  bool
	structured            bool
	setupErr              error
	teardownErr           error
	setupCalls            *int
	onSetup               func() error
	forceExecutionFailure bool
}

type legacyEmptyCatalogScenario struct{}

func (legacyEmptyCatalogScenario) Name() string                                    { return "legacy-empty" }
func (legacyEmptyCatalogScenario) Description() string                             { return "legacy empty catalog" }
func (legacyEmptyCatalogScenario) CheckRequirements() []scenarios.CheckRequirement { return nil }
func (legacyEmptyCatalogScenario) Setup(context.Context) error                     { return nil }
func (legacyEmptyCatalogScenario) Execute(context.Context) (*scenarios.Result, error) {
	return &scenarios.Result{ScenarioName: "legacy-empty", Success: true}, nil
}
func (legacyEmptyCatalogScenario) Teardown(context.Context) error { return nil }

func TestEmptyCheckCatalogIsLegacyUnattestedAndRequiredSelectionRefuses(t *testing.T) {
	scenario := legacyEmptyCatalogScenario{}
	checks, adopted := scenarioChecks(scenario)
	if adopted || len(checks) != 0 {
		t.Fatalf("empty catalog was treated as adopted: %v %v", adopted, checks)
	}
	logger := slog.New(slog.NewTextHandler(&bytes.Buffer{}, nil))
	legacy := scenarioSelection{flags: cliFlags{scenarioName: scenario.Name()}, label: scenario.Name(),
		members: []string{scenario.Name()}}
	run := results.CreateTestRun(results.TestRunConfig{Selection: legacy.label}, nil, nil, 0)
	if exit := runSelectedScenarios(t.Context(), logger, legacy, run, []scenarios.Scenario{scenario}); exit != 0 ||
		run.EvidenceStatus != "unattested" {
		t.Fatalf("legacy empty catalog changed behavior: exit=%d status=%q", exit, run.EvidenceStatus)
	}
	legacy.required = true
	legacy.flags.outputDir = t.TempDir()
	run = results.CreateTestRun(results.TestRunConfig{Selection: legacy.label}, nil, nil, 0)
	if exit := runSelectedScenarios(t.Context(), logger, legacy, run, []scenarios.Scenario{scenario}); exit != 1 {
		t.Fatalf("required empty catalog was admitted: exit=%d", exit)
	}
}

func TestTieredFallbackResolverConstructorDeclarationBridge(t *testing.T) {
	for _, tc := range []struct {
		variant  string
		required bool
		adopted  bool
	}{
		{variant: "semantic-fallback", required: false, adopted: false},
		{variant: "structural", required: true, adopted: true},
	} {
		selection, err := resolveScenarioSelection(&cliFlags{scenarioName: "tiered", variant: tc.variant,
			baseURL: "http://localhost:38080"})
		require.NoError(t, err)
		constructed := createScenario(nil, &selection.flags)
		require.NotNil(t, constructed)
		checks, adopted := scenarioChecks(constructed)
		require.Equal(t, tc.required, selection.required)
		require.Equal(t, tc.adopted, adopted)
		if adopted {
			require.NotEmpty(t, checks)
		} else {
			require.Empty(t, checks)
		}
	}
}

func (s observedFixtureScenario) Name() string        { return "fixture" }
func (s observedFixtureScenario) Description() string { return "controlled output fixture" }
func (s observedFixtureScenario) CheckRequirements() []scenarios.CheckRequirement {
	return []scenarios.CheckRequirement{{ID: "fixture.output", Required: true}}
}
func (s observedFixtureScenario) Setup(context.Context) error {
	if s.setupCalls != nil {
		*s.setupCalls++
	}
	if s.onSetup != nil {
		return s.onSetup()
	}
	return s.setupErr
}
func (s observedFixtureScenario) Execute(context.Context) (*scenarios.Result, error) {
	result := &scenarios.Result{ScenarioName: s.Name()}
	if err := result.DeclareChecks(s.runID, s.memberID, s.CheckRequirements()); err != nil {
		return result, err
	}
	if s.structured {
		result.Structured = &scenarios.TieredResults{
			Variant:  scenarios.VariantResults{Name: "fixture"},
			Metadata: scenarios.TestMetadata{Success: true, CompletedAt: time.Now().UTC()},
		}
	}
	if s.omit {
		return result, nil
	}
	status := "passed"
	reason := ""
	if s.got != s.want {
		status = "failed"
		reason = "fixture output differs from controlled input"
	}
	err := result.RecordCheck(scenarios.CheckObservation{
		ID:       "fixture.output",
		RunID:    s.runID,
		MemberID: s.memberID,
		Status:   status,
		Reason:   reason,
		Evidence: map[string]string{"expected": s.want, "observed": s.got},
	})
	if s.forceExecutionFailure {
		result.Success = false
	} else if err == nil && status == "passed" {
		result.Success = true
	}
	return result, err
}
func (s observedFixtureScenario) Teardown(context.Context) error { return s.teardownErr }

func completeFixtureProvenance(t *testing.T, outputDir string) map[string]string {
	t.Helper()
	sha := strings.Repeat("a", 64)
	logData := []byte("fixture task log\n")
	manifestData := []byte("{\"fixture\":\"controlled source and application identity\"}\n")
	logPath := filepath.Join(outputDir, "fixture-task.log")
	manifestPath := filepath.Join(outputDir, "provenance-manifest.json")
	require.NoError(t, os.WriteFile(logPath, logData, 0600))
	require.NoError(t, os.WriteFile(manifestPath, manifestData, 0600))
	return map[string]string{
		"source_sha":                strings.Repeat("b", 40),
		"source_dirty":              "false",
		"source_patch_sha256":       "not_applicable: clean fixture source",
		"source_untracked_sha256":   "not_applicable: clean fixture source",
		"runner_sha256":             sha,
		"runner_build":              "fixture-build",
		"app_image_id":              "sha256:" + sha,
		"app_build":                 "fixture-app-build",
		"compose_sha256":            sha,
		"profiles":                  "fixture-profile",
		"config_sha256":             sha,
		"fixture_sha256":            sha,
		"effective_settings_sha256": sha,
		"output_dir":                outputDir,
		"log_path":                  logPath,
		"log_sha256":                fmt.Sprintf("%x", sha256.Sum256(logData)),
		"artifact_manifest_path":    manifestPath,
		"artifact_manifest_sha256":  fmt.Sprintf("%x", sha256.Sum256(manifestData)),
	}
}

func fixtureSelection(outputDir string) scenarioSelection {
	return scenarioSelection{
		flags:    cliFlags{scenarioName: "fixture", outputDir: outputDir},
		label:    "fixture",
		members:  []string{"fixture"},
		required: true,
	}
}

func TestRequiredCLIInitialAggregateDeclaresSelectedChecksBeforeSetup(t *testing.T) {
	selection := fixtureSelection(t.TempDir())
	run := results.CreateTestRun(results.TestRunConfig{Selection: selection.label}, nil, nil, 0)
	fixture := observedFixtureScenario{runID: run.ID, memberID: "fixture", want: "same", got: "same"}
	reachedSetup := false
	fixture.onSetup = func() error {
		reachedSetup = true
		path := filepath.Join(selection.flags.outputDir, "e2e-results-fixture-"+run.ID+".json")
		initial, err := results.NewWriter(selection.flags.outputDir).LoadRun(path)
		if err != nil {
			t.Error(err)
			return err
		}
		if initial.ExitCode != nil || len(initial.Scenarios) != 1 || initial.Scenarios[0].RunID != run.ID ||
			initial.Scenarios[0].MemberID != "fixture" || len(initial.Scenarios[0].CheckRequirements) != 1 ||
			initial.Scenarios[0].CheckRequirements[0].ID != "fixture.output" ||
			len(initial.Scenarios[0].CheckObservations) != 0 {
			t.Errorf("initial aggregate did not retain selected declaration: %+v", initial)
			return fmt.Errorf("initial aggregate did not retain selected declaration: %+v", initial)
		}
		return nil
	}
	logger := slog.New(slog.NewTextHandler(&bytes.Buffer{}, nil))
	if exit := runSelectedScenarios(t.Context(), logger, selection, run, []scenarios.Scenario{fixture}); exit != 1 {
		t.Fatalf("missing provenance must still fail after declaration check; exit=%d", exit)
	}
	if !reachedSetup {
		t.Fatal("scenario Setup was not reached")
	}
}

func TestSelectedRunnerPreservesExplicitExecutionFailureWithPassedChecks(t *testing.T) {
	selection := fixtureSelection(t.TempDir())
	run := results.CreateTestRun(results.TestRunConfig{Selection: selection.label}, nil, nil, 0)
	run.Environment = completeFixtureProvenance(t, selection.flags.outputDir)
	fixture := observedFixtureScenario{runID: run.ID, memberID: "fixture", want: "same", got: "same",
		forceExecutionFailure: true}
	logger := slog.New(slog.NewTextHandler(&bytes.Buffer{}, nil))
	exit := runSelectedScenarios(t.Context(), logger, selection, run, []scenarios.Scenario{fixture})
	if exit != 1 {
		t.Fatalf("explicit Execute failure was erased by passed checks: exit=%d", exit)
	}
	path := filepath.Join(selection.flags.outputDir, "e2e-results-fixture-"+run.ID+".json")
	stored, err := results.NewWriter(selection.flags.outputDir).LoadRun(path)
	if err != nil {
		t.Fatal(err)
	}
	if stored.EvidenceStatus != "unattested" || stored.Scenarios[0].Success {
		t.Fatalf("explicit Execute failure was erased in terminal aggregate: %+v", stored.Scenarios[0])
	}
}

func TestRequiredCLIChildRetainsClosedLogManifestAndVerifiableParentSlot(t *testing.T) {
	root := t.TempDir()
	write := func(name, data string) string {
		t.Helper()
		path := filepath.Join(root, name)
		require.NoError(t, os.WriteFile(path, []byte(data), 0600))
		return path
	}
	input := reportManifestInput{
		OutputDir: root, Selection: "fixture", ParentID: "parent-fixture", ParentMemberID: "slot.fixture",
		Profiles: "fixture", Files: []reportManifestFileInput{
			{Role: "compose", Path: write("compose.yml", "services: {}\n")},
			{Role: "config", Path: write("config.json", "{}\n")},
			{Role: "fixture", Path: write("fixture.json", "{}\n")},
		},
		AppPhases: []reportManifestAppInput{{Name: "production", ImageID: "sha256:" + strings.Repeat("a", 64),
			ImageDigest: "unavailable: locally built image has no registry digest",
			BinaryPath:  write("app-binary", "actual bytes"), Build: "observed build"}},
	}
	inputData, err := json.Marshal(input)
	require.NoError(t, err)
	inputPath := write("input.json", string(inputData))
	selection := fixtureSelection(root)
	selection.flags.evidenceInputPath = inputPath
	run := results.CreateTestRun(results.TestRunConfig{Selection: selection.label}, nil, nil, 0)
	fixture := observedFixtureScenario{runID: run.ID, memberID: "fixture", want: "same", got: "same"}
	var log bytes.Buffer
	logger := slog.New(slog.NewTextHandler(&log, nil))
	exit := runSelectedScenarios(t.Context(), logger, selection, run, []scenarios.Scenario{fixture})
	require.Zero(t, exit, log.String())
	path := filepath.Join(root, "e2e-results-fixture-"+run.ID+".json")
	stored, err := results.NewWriter(root).LoadRun(path)
	require.NoError(t, err)
	require.Equal(t, "complete", stored.EvidenceStatus)
	require.Equal(t, "parent-fixture", stored.ParentID)
	require.Equal(t, "slot.fixture", stored.ParentMemberID)
	for _, pair := range [][2]string{{"log_path", "log_sha256"}, {"artifact_manifest_path", "artifact_manifest_sha256"}} {
		artifactPath := stored.Environment[pair[0]]
		if !filepath.IsAbs(artifactPath) {
			artifactPath = filepath.Join(root, artifactPath)
		}
		data, readErr := os.ReadFile(artifactPath)
		require.NoError(t, readErr)
		require.Equal(t, fmt.Sprintf("%x", sha256.Sum256(data)), stored.Environment[pair[1]])
	}
	_, err = results.NewWriter(root).VerifyChild(path, results.ChildExpectation{
		ParentID: "parent-fixture", ParentMemberID: "slot.fixture", Selection: "fixture",
		RequiredMembers: []string{"fixture"}})
	require.NoError(t, err)
}

// spec: e2e-evidence / Domain projections follow final outcome
func TestLegacyTypedAnalysisFollowsFinalValidationAndTeardownFailure(t *testing.T) {
	for _, tc := range []struct {
		name        string
		result      *scenarios.Result
		teardownErr error
	}{
		{name: "final validation", result: &scenarios.Result{ScenarioName: "assertion-reporting", Success: false,
			Error: "final validation rejected observed result"}},
		{name: "teardown", result: &scenarios.Result{ScenarioName: "assertion-reporting", Success: true},
			teardownErr: errors.New("teardown failed")},
	} {
		t.Run(tc.name, func(t *testing.T) {
			tc.result.Structured = &scenarios.TieredResults{
				Variant:  scenarios.VariantResults{Name: "fixture"},
				Metadata: scenarios.TestMetadata{Success: true, CompletedAt: time.Now().UTC()},
			}
			selection := scenarioSelection{
				flags: cliFlags{scenarioName: "assertion-reporting", outputDir: t.TempDir()},
				label: "assertion-reporting", members: []string{"assertion-reporting"},
			}
			run := results.CreateTestRun(results.TestRunConfig{Selection: selection.label}, nil, nil, 0)
			var log bytes.Buffer
			logger := slog.New(slog.NewTextHandler(&log, nil))
			exit := runSelectedScenarios(t.Context(), logger, selection, run,
				[]scenarios.Scenario{assertionReportingScenario{result: tc.result, teardownErr: tc.teardownErr}})
			require.Equal(t, 1, exit, log.String())
			path := filepath.Join(selection.flags.outputDir, "e2e-results-"+selection.label+"-"+run.ID+".json")
			stored, err := results.NewWriter(selection.flags.outputDir).LoadRun(path)
			require.NoError(t, err)
			require.False(t, stored.Scenarios[0].Success)
			files, err := filepath.Glob(filepath.Join(selection.flags.outputDir, "fixture-*.json"))
			require.NoError(t, err)
			require.Len(t, files, 1)
			projection, err := scenarios.LoadStructuredResults(files[0])
			require.NoError(t, err)
			require.False(t, projection.Metadata.Success, "standalone typed analysis must match final Result")
		})
	}
}

// spec: e2e-evidence / Selection determines proof scope
func TestLegacyUnattestedDispositionIsAnnouncedBeforeSetup(t *testing.T) {
	selection := scenarioSelection{
		flags: cliFlags{scenarioName: "assertion-reporting"},
		label: "assertion-reporting", members: []string{"assertion-reporting"},
	}
	run := results.CreateTestRun(results.TestRunConfig{Selection: selection.label}, nil, nil, 0)
	var log bytes.Buffer
	logger := slog.New(slog.NewTextHandler(&log, nil))
	exit := runSelectedScenarios(t.Context(), logger, selection, run,
		[]scenarios.Scenario{assertionReportingScenario{setupErr: errors.New("controlled setup failure")}})
	require.Equal(t, 1, exit)
	announcement := strings.Index(log.String(), "unattested")
	setup := strings.Index(log.String(), "Setting up scenario")
	require.NotEqual(t, -1, announcement, log.String())
	require.NotEqual(t, -1, setup, log.String())
	require.Less(t, announcement, setup, log.String())
}

// spec: e2e-evidence / Run evidence binds one invocation
// spec: e2e-evidence / Failure reaches the outer gate
func TestSelectedRunnerPersistsCompletedAndFailedProof(t *testing.T) {
	for _, tc := range []struct {
		name            string
		provenance      bool
		omitObservation bool
		structured      bool
		teardownErr     error
		wantExit        int
		wantStatus      string
	}{
		{name: "observed complete", provenance: true, wantStatus: "complete"},
		{name: "missing provenance", wantExit: 1, wantStatus: "unattested"},
		{name: "missing observation", provenance: true, omitObservation: true,
			structured: true, wantExit: 1, wantStatus: "unattested"},
		{name: "teardown after observation", provenance: true,
			teardownErr: errors.New("cleanup failed"), wantExit: 1, wantStatus: "unattested"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			selection := fixtureSelection(t.TempDir())
			run := results.CreateTestRun(results.TestRunConfig{Selection: selection.label}, nil, nil, 0)
			if tc.provenance {
				run.Environment = completeFixtureProvenance(t, selection.flags.outputDir)
			}
			fixture := observedFixtureScenario{
				runID: run.ID, memberID: "fixture", want: "controlled-value", got: "controlled-value",
				omit: tc.omitObservation, structured: tc.structured, teardownErr: tc.teardownErr,
			}
			var log bytes.Buffer
			logger := slog.New(slog.NewTextHandler(&log, nil))
			exit := runSelectedScenarios(t.Context(), logger, selection, run, []scenarios.Scenario{fixture})
			require.Equal(t, tc.wantExit, exit, log.String())
			path := filepath.Join(selection.flags.outputDir, "e2e-results-"+selection.label+"-"+run.ID+".json")
			stored, err := results.NewWriter(selection.flags.outputDir).LoadRun(path)
			require.NoError(t, err)
			require.Equal(t, tc.wantStatus, stored.EvidenceStatus)
			require.Equal(t, tc.wantExit, *stored.ExitCode)
			require.Len(t, stored.Scenarios, 1)
			require.Equal(t, run.ID, stored.Scenarios[0].RunID)
			require.Equal(t, "fixture", stored.Scenarios[0].MemberID)
			if !tc.omitObservation {
				require.Len(t, stored.Scenarios[0].CheckObservations, 1)
			}
			if tc.structured {
				files, err := filepath.Glob(filepath.Join(selection.flags.outputDir, "fixture-*.json"))
				require.NoError(t, err)
				require.Len(t, files, 1)
				projection, err := scenarios.LoadStructuredResults(files[0])
				require.NoError(t, err)
				require.False(t, projection.Metadata.Success, "typed analysis follows final required outcome")
			}
		})
	}
}

// spec: e2e-evidence / Failure reaches the outer gate
func TestSelectedRunnerRejectsWriterFailureBeforeSetup(t *testing.T) {
	path := filepath.Join(t.TempDir(), "not-a-directory")
	require.NoError(t, os.WriteFile(path, []byte("occupied"), 0600))
	selection := fixtureSelection(path)
	run := results.CreateTestRun(results.TestRunConfig{Selection: selection.label}, nil, nil, 0)
	setupCalls := 0
	fixture := observedFixtureScenario{runID: run.ID, memberID: "fixture", setupCalls: &setupCalls}
	var log bytes.Buffer
	logger := slog.New(slog.NewTextHandler(&log, nil))
	exit := runSelectedScenarios(t.Context(), logger, selection, run, []scenarios.Scenario{fixture})
	require.Equal(t, 1, exit)
	require.Zero(t, setupCalls)
	require.Contains(t, log.String(), "initial evidence")
}

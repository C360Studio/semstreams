package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/c360studio/semstreams/test/e2e/results"
	scenarios "github.com/c360studio/semstreams/test/e2e/scenarios"
)

// checkDeclarer is optional so legacy scenarios retain their execution path.
// Adopted scenarios declare checks before Setup, then record those same checks
// on their Result while executing.
type checkDeclarer interface {
	CheckRequirements() []scenarios.CheckRequirement
}

func scenarioChecks(scenario scenarios.Scenario) ([]scenarios.CheckRequirement, bool) {
	declarer, ok := scenario.(checkDeclarer)
	if !ok {
		return nil, false
	}
	checks := declarer.CheckRequirements()
	if len(checks) == 0 {
		return nil, false
	}
	return append([]scenarios.CheckRequirement(nil), checks...), true
}

func executeScenario(
	ctx context.Context,
	logger *slog.Logger,
	scenario scenarios.Scenario,
	flags *cliFlags,
	expected []scenarios.CheckRequirement,
	adopted bool,
) (*scenarios.Result, int) {
	result := &scenarios.Result{ScenarioName: scenario.Name()}
	if adopted {
		if err := result.DeclareChecks(flags.evidenceRunID, flags.evidenceMemberID, expected); err != nil {
			logger.Error("Invalid required check declaration", "name", scenario.Name(), "error", err)
			return result, 1
		}
	}

	logger.Info("Setting up scenario", "name", scenario.Name())
	setupErr := scenario.Setup(ctx)
	if setupErr != nil {
		addScenarioFailure(result, "setup", setupErr)
		logger.Error("Scenario setup failed", "error", setupErr)
	} else {
		logger.Info("Executing scenario", "name", scenario.Name())
		executed, executeErr := scenario.Execute(ctx)
		if executed == nil {
			if executeErr == nil {
				executeErr = fmt.Errorf("scenario returned nil result")
			}
		} else {
			result = executed
			if adopted && !executed.Success && executeErr == nil {
				addScenarioFailure(result, "execute", fmt.Errorf("scenario returned unsuccessful result"))
			}
		}
		if executeErr != nil {
			addScenarioFailure(result, "execute", executeErr)
			logger.Error("Scenario execution failed", "error", executeErr)
		}
	}

	logger.Info("Tearing down scenario", "name", scenario.Name())
	if err := scenario.Teardown(ctx); err != nil {
		addScenarioFailure(result, "teardown", err)
		logger.Error("Scenario teardown failed", "error", err)
	}

	if adopted {
		if result.RunID != flags.evidenceRunID || result.MemberID != flags.evidenceMemberID ||
			!sameCheckRequirements(expected, result.CheckRequirements) {
			addScenarioFailure(result, "evidence", fmt.Errorf("returned check declaration or run/member identity differs from selection"))
		}
		if err := result.FinalizeChecks(); err != nil {
			logger.Error("Required scenario evidence incomplete", "name", scenario.Name(), "error", err)
			return result, 1
		}
		logger.Info("Required scenario checks passed", "name", scenario.Name(), "assertions_run", result.AssertionsRun)
		return result, 0
	}

	result.EvidenceStatus = "unattested"
	if !result.Success || len(result.Errors) > 0 || result.Error != "" {
		logger.Error("Scenario execution failed", "name", scenario.Name(), "error", result.Error,
			"errors", result.Errors, "assertions_run", result.AssertionsRun)
		return result, 1
	}
	logger.Info("Scenario execution completed with unattested evidence", "name", scenario.Name(),
		"duration", result.Duration, "assertions_run", result.AssertionsRun)
	return result, 0
}

func addScenarioFailure(result *scenarios.Result, phase string, err error) {
	result.Errors = append(result.Errors, fmt.Sprintf("%s: %s", phase, err))
	result.Success = false
}

func sameCheckRequirements(expected, actual []scenarios.CheckRequirement) bool {
	if len(expected) != len(actual) {
		return false
	}
	byID := make(map[string]bool, len(expected))
	for _, check := range expected {
		byID[check.ID] = check.Required
	}
	for _, check := range actual {
		required, ok := byID[check.ID]
		if !ok || required != check.Required {
			return false
		}
		delete(byID, check.ID)
	}
	return len(byID) == 0
}

type selectedRunSetup struct {
	checkSets     [][]scenarios.CheckRequirement
	adopted       []bool
	evidenceInput reportManifestInput
	writer        *results.Writer
	initial       results.TestRun
}

// prepareSelectedRun records every member declaration before scenario setup.
func prepareSelectedRun(
	logger *slog.Logger,
	selection *scenarioSelection,
	run *results.TestRun,
	tests []scenarios.Scenario,
) (*selectedRunSetup, bool) {
	checkSets := make([][]scenarios.CheckRequirement, len(tests))
	adopted := make([]bool, len(tests))
	run.Scenarios = make([]scenarios.Result, len(tests))
	for i, scenario := range tests {
		memberID := selection.members[i]
		name := strings.SplitN(memberID, ":", 2)[0]
		if scenario == nil || scenario.Name() != name {
			logger.Error("Selected member differs from scenario", "member", memberID)
			return nil, false
		}
		checkSets[i], adopted[i] = scenarioChecks(scenario)
		if selection.required && !adopted[i] {
			logger.Error("Required member has no declared checks", "member", memberID)
			return nil, false
		}
		if adopted[i] {
			declaration := &scenarios.Result{}
			if err := declaration.DeclareChecks(run.ID, memberID, checkSets[i]); err != nil {
				logger.Error("Invalid required member declaration", "member", memberID, "error", err)
				return nil, false
			}
			run.Scenarios[i] = *declaration
		} else {
			run.Scenarios[i] = scenarios.Result{ScenarioName: scenario.Name(), RunID: run.ID, MemberID: memberID,
				EvidenceStatus: "unattested"}
		}
	}

	run.Config.Selection = selection.label
	run.Config.RequireEvidence = selection.required
	run.Config.Variant = selection.flags.variant
	run.Config.Scenarios = append([]string(nil), selection.members...)
	run.Config.BaseURL = selection.flags.baseURL
	run.Config.MetricsURL = selection.flags.metricsURL
	var evidenceInput reportManifestInput
	if selection.flags.evidenceInputPath != "" {
		var inputErr error
		evidenceInput, inputErr = readReportManifestInput(selection.flags.evidenceInputPath)
		if inputErr != nil {
			logger.Error("Cannot read typed evidence input", "error", inputErr)
			return nil, false
		}
		if evidenceInput.Selection != selection.label ||
			(evidenceInput.ParentID == "") != (evidenceInput.ParentMemberID == "") {
			logger.Error("Evidence input selection or parent slot differs from selected CLI run")
			return nil, false
		}
		run.ParentID, run.ParentMemberID = evidenceInput.ParentID, evidenceInput.ParentMemberID
	}
	if selection.required {
		run.Config.RequiredMembers = append([]string(nil), selection.members...)
	}
	if len(run.Command) == 0 {
		run.Command = append([]string(nil), os.Args...)
	}
	if run.WorkingDir == "" {
		wd, err := os.Getwd()
		if err != nil {
			logger.Error("Cannot observe working directory", "error", err)
			return nil, false
		}
		run.WorkingDir = wd
	}
	fillUnavailableProvenance(run)

	var writer *results.Writer
	var initial results.TestRun
	if selection.flags.outputDir != "" {
		outputDir, err := filepath.Abs(selection.flags.outputDir)
		if err != nil {
			logger.Error("Cannot resolve evidence output directory", "error", err)
			return nil, false
		}
		selection.flags.outputDir = outputDir
		if selection.flags.evidenceInputPath != "" && evidenceInput.OutputDir != outputDir {
			logger.Error("Evidence input output location differs from selected CLI output")
			return nil, false
		}
		writer = results.NewWriter(outputDir)
		if _, err := writer.WriteRun(run); err != nil {
			logger.Error("Failed to write initial evidence", "path", outputDir, "error", err)
			fmt.Fprintf(os.Stderr, "E2E initial evidence failed at %s: %s\n", outputDir, err)
			return nil, false
		}
		initial = *run
		initial.Scenarios = append([]scenarios.Result(nil), run.Scenarios...)
		initial.Environment = make(map[string]string, len(run.Environment))
		for key, value := range run.Environment {
			initial.Environment[key] = value
		}
	}
	return &selectedRunSetup{
		checkSets: checkSets, adopted: adopted, evidenceInput: evidenceInput,
		writer: writer, initial: initial,
	}, true
}

// runSelectedScenarios persists one aggregate for the resolved invocation.
// Its only execution path is the Scenario interface; Task remains responsible
// for building, starting, stopping and cleaning the services around this CLI.
func runSelectedScenarios(
	ctx context.Context,
	logger *slog.Logger,
	selection scenarioSelection,
	run *results.TestRun,
	tests []scenarios.Scenario,
) int {
	if run == nil || run.ID == "" || len(selection.members) == 0 || len(selection.members) != len(tests) {
		logger.Error("Invalid selected run membership")
		return 1
	}
	if selection.required && selection.flags.outputDir == "" {
		logger.Error("Required selection needs an evidence output directory", "selection", selection.label)
		return 1
	}
	disposition := "unattested"
	if selection.required {
		disposition = "required"
	}
	logger.Info("Resolved scenario selection", "selection", selection.label,
		"members", selection.members, "evidence_status", disposition)

	prepared, ok := prepareSelectedRun(logger, &selection, run, tests)
	if !ok {
		return 1
	}
	checkSets, adopted := prepared.checkSets, prepared.adopted
	evidenceInput, writer, initial := prepared.evidenceInput, prepared.writer, prepared.initial
	originalLogger := logger
	var executionLog *os.File
	var executionLogPath string
	if writer != nil && selection.required && selection.flags.evidenceInputPath != "" {
		executionLogPath = filepath.Join(selection.flags.outputDir, "e2e-log-"+run.ID+".log")
		var logErr error
		executionLog, logErr = os.OpenFile(executionLogPath, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
		if logErr != nil {
			logger.Error("Cannot retain CLI execution log", "error", logErr)
			return failInitializedCLI(writer, run, 1)
		}
		logger = slog.New(dualSlogHandler{first: logger.Handler(), second: slog.NewTextHandler(executionLog, nil)})
		logger.Info("Required CLI execution log started", "selection", selection.label)
	}

	exitCode := 0
	for i, scenario := range tests {
		memberFlags := selection.flags
		memberFlags.evidenceRunID = run.ID
		memberFlags.evidenceMemberID = selection.members[i]
		result, memberExit := executeScenario(ctx, logger, scenario, &memberFlags, checkSets[i], adopted[i])
		if result.RunID == "" {
			result.RunID = run.ID
		}
		if result.MemberID == "" {
			result.MemberID = memberFlags.evidenceMemberID
		}
		run.Scenarios[i] = *result
		if memberExit != 0 {
			exitCode = 1
		}
	}
	if executionLog != nil {
		logger.Info("Required CLI scenario teardown completed", "selection", selection.label, "scenario_exit", exitCode)
		if closeErr := executionLog.Close(); closeErr != nil {
			originalLogger.Error("Cannot close CLI execution log", "error", closeErr)
			exitCode = 1
		}
		logger = originalLogger
		evidenceInput.Files = append(evidenceInput.Files,
			reportManifestFileInput{Role: "task_log", Path: executionLogPath})
		evidenceInput.EffectiveSettings = map[string]string{
			"base_url":     selection.flags.baseURL,
			"metrics_url":  selection.flags.metricsURL,
			"udp_endpoint": selection.flags.udpEndpoint,
			"variant":      selection.flags.variant,
		}
		manifest, manifestErr := buildReportManifest(evidenceInput)
		if manifestErr != nil {
			logger.Error("Cannot prepare CLI evidence manifest", "error", manifestErr)
			exitCode = 1
		} else {
			for key, value := range reportManifestEnvironment(manifest) {
				run.Environment[key] = value
			}
			data, marshalErr := json.MarshalIndent(manifest, "", "  ")
			if marshalErr != nil {
				logger.Error("Cannot marshal CLI evidence manifest", "error", marshalErr)
				exitCode = 1
			} else {
				reference, retainErr := writer.WriteManifest(&initial, data)
				if retainErr != nil {
					logger.Error("Cannot retain CLI evidence manifest", "error", retainErr)
					exitCode = 1
				} else {
					run.Environment["artifact_manifest_path"] = reference.Path
					run.Environment["artifact_manifest_sha256"] = reference.SHA256
				}
			}
		}
	}

	run.CompletedAt = time.Now().UTC()
	run.Duration = run.CompletedAt.Sub(run.StartedAt)
	run.DurationStr = run.Duration.String()
	run.ExitCode = &exitCode
	if writer != nil {
		path, err := writer.WriteRun(run)
		if err != nil {
			logger.Error("Failed to write terminal evidence", "path", selection.flags.outputDir, "error", err)
			fmt.Fprintf(os.Stderr, "E2E terminal evidence failed at %s: %s\n", selection.flags.outputDir, err)
			return 1
		}
		fmt.Fprintf(os.Stdout, "E2E_RESULT_PATH=%s\n", path)
		exitCode = *run.ExitCode
	}
	return finishSelectedRun(logger, selection, run, exitCode)
}

func finishSelectedRun(logger *slog.Logger, selection scenarioSelection, run *results.TestRun, exitCode int) int {
	// The aggregate Writer owns final Result -> typed metadata projection.
	// Serialize optional analysis only after that projection, including legacy
	// final-validation and teardown failures.
	for i := range run.Scenarios {
		memberFlags := selection.flags
		memberFlags.evidenceMemberID = selection.members[i]
		saveScenarioAnalysis(logger, &run.Scenarios[i], &memberFlags, exitCode)
	}
	if selection.required {
		logger.Info("Required selection finalized", "selection", selection.label, "evidence_status", run.EvidenceStatus,
			"exit_code", exitCode)
	} else {
		logger.Info("Legacy selection finalized with unattested evidence", "selection", selection.label,
			"exit_code", exitCode)
	}
	return exitCode
}

func failInitializedCLI(writer *results.Writer, run *results.TestRun, exitCode int) int {
	run.CompletedAt = time.Now().UTC()
	run.Duration = run.CompletedAt.Sub(run.StartedAt)
	run.DurationStr = run.Duration.String()
	run.ExitCode = &exitCode
	if _, err := writer.WriteRun(run); err != nil {
		fmt.Fprintf(os.Stderr, "E2E failed terminal evidence could not be retained: %s\n", err)
	}
	return 1
}

type dualSlogHandler struct{ first, second slog.Handler }

func (h dualSlogHandler) Enabled(ctx context.Context, level slog.Level) bool {
	return h.first.Enabled(ctx, level) || h.second.Enabled(ctx, level)
}

func (h dualSlogHandler) Handle(ctx context.Context, record slog.Record) error {
	if err := h.first.Handle(ctx, record); err != nil {
		return err
	}
	return h.second.Handle(ctx, record)
}

func (h dualSlogHandler) WithAttrs(attrs []slog.Attr) slog.Handler {
	return dualSlogHandler{first: h.first.WithAttrs(attrs), second: h.second.WithAttrs(attrs)}
}

func (h dualSlogHandler) WithGroup(name string) slog.Handler {
	return dualSlogHandler{first: h.first.WithGroup(name), second: h.second.WithGroup(name)}
}

func fillUnavailableProvenance(run *results.TestRun) {
	if run.Environment == nil {
		run.Environment = make(map[string]string)
	}
	for _, key := range []string{
		"source_sha", "source_dirty", "source_patch_sha256", "source_untracked_sha256",
		"runner_sha256", "runner_build", "app_image_id", "app_image_digest", "app_binary_sha256",
		"app_build", "compose_sha256", "profiles", "config_sha256", "fixture_sha256",
		"effective_settings_sha256",
	} {
		if run.Environment[key] == "" {
			run.Environment[key] = "unavailable: not observed by this invocation"
		}
	}
}

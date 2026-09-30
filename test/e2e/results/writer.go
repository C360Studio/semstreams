// Package results provides structured result writing for E2E test scenarios
package results

import (
	"encoding/hex"
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"sort"
	"strings"
	"sync/atomic"
	"time"

	"github.com/c360studio/semstreams/test/e2e/client"
	"github.com/c360studio/semstreams/test/e2e/scenarios"
)

// TestRun represents a complete E2E test run
type TestRun struct {
	SchemaVersion   int                   `json:"schema_version"`
	ID              string                `json:"id"`
	ParentID        string                `json:"parent_id,omitempty"`
	ParentMemberID  string                `json:"parent_member_id,omitempty"`
	EvidenceStatus  string                `json:"evidence_status"`
	Command         []string              `json:"command,omitempty"`
	WorkingDir      string                `json:"working_dir,omitempty"`
	StartedAt       time.Time             `json:"started_at"`
	CompletedAt     time.Time             `json:"completed_at,omitempty"`
	ExitCode        *int                  `json:"exit_code,omitempty"`
	CommandExitCode *int                  `json:"command_exit_code,omitempty"`
	CleanupExitCode *int                  `json:"cleanup_exit_code,omitempty"`
	Timestamp       time.Time             `json:"timestamp"`
	Duration        time.Duration         `json:"duration_ns"`
	DurationStr     string                `json:"duration"`
	Config          TestRunConfig         `json:"config"`
	Scenarios       []scenarios.Result    `json:"scenarios"`
	Metrics         *client.MetricsReport `json:"metrics,omitempty"`
	Summary         Summary               `json:"summary"`
	Environment     map[string]string     `json:"environment,omitempty"`
}

// TestRunConfig captures the configuration for a test run
type TestRunConfig struct {
	Selection           string             `json:"selection,omitempty"`
	RequireEvidence     bool               `json:"require_evidence"`
	RequireTaskStatuses bool               `json:"require_task_statuses,omitempty"`
	RequiredMembers     []string           `json:"required_members,omitempty"`
	ChildExpectations   []ChildExpectation `json:"child_expectations,omitempty"`
	ExcludedMembers     []string           `json:"excluded_members,omitempty"`
	Variant             string             `json:"variant"` // "structural", "statistical", or "semantic"
	MLEnabled           bool               `json:"ml_enabled"`
	Scenarios           []string           `json:"scenarios"`
	BaseURL             string             `json:"base_url"`
	MetricsURL          string             `json:"metrics_url"`
}

// Summary provides high-level statistics for the test run
type Summary struct {
	RequiredProofPassed    bool     `json:"required_proof_passed"`
	MissingRequiredMembers []string `json:"missing_required_members,omitempty"`
	TotalScenarios         int      `json:"total_scenarios"`
	PassedScenarios        int      `json:"passed_scenarios"`
	FailedScenarios        int      `json:"failed_scenarios"`
	SuccessRate            float64  `json:"success_rate"`
	TotalErrors            int      `json:"total_errors"`
	TotalWarnings          int      `json:"total_warnings"`
	AllPassed              bool     `json:"all_passed"`
}

// Comparison represents a comparison between two test runs
type Comparison struct {
	Timestamp              time.Time         `json:"timestamp"`
	BaselineID             string            `json:"baseline_id"`
	CurrentID              string            `json:"current_id"`
	BaselineEvidenceStatus string            `json:"baseline_evidence_status"`
	CurrentEvidenceStatus  string            `json:"current_evidence_status"`
	BaselineDate           time.Time         `json:"baseline_date"`
	CurrentDate            time.Time         `json:"current_date"`
	Diffs                  []ScenarioDiff    `json:"diffs"`
	MetricsDiff            map[string]Diff   `json:"metrics_diff,omitempty"`
	Overall                ComparisonSummary `json:"overall"`
}

// ScenarioDiff represents the difference between scenario results
type ScenarioDiff struct {
	ScenarioName     string          `json:"scenario_name"`
	BaselineSuccess  bool            `json:"baseline_success"`
	CurrentSuccess   bool            `json:"current_success"`
	StatusChanged    bool            `json:"status_changed"`
	DurationChange   time.Duration   `json:"duration_change_ns"`
	DurationChangeMs int64           `json:"duration_change_ms"`
	MetricsDiff      map[string]Diff `json:"metrics_diff,omitempty"`
}

// Diff represents a numeric difference
type Diff struct {
	Baseline  float64 `json:"baseline"`
	Current   float64 `json:"current"`
	Absolute  float64 `json:"absolute"`
	Percent   float64 `json:"percent"`
	Improved  bool    `json:"improved"`
	Regressed bool    `json:"regressed"`
}

// ComparisonSummary provides high-level comparison statistics
type ComparisonSummary struct {
	StatusChanges    int  `json:"status_changes"`
	Improvements     int  `json:"improvements"`
	Regressions      int  `json:"regressions"`
	MetricsImproved  int  `json:"metrics_improved"`
	MetricsRegressed int  `json:"metrics_regressed"`
	OverallImproved  bool `json:"overall_improved"`
}

// Writer handles persisting test results to disk
type Writer struct {
	outputDir string
}

// NewWriter creates a new results writer
func NewWriter(outputDir string) *Writer {
	return &Writer{outputDir: outputDir}
}

// WriteRun writes a complete test run to disk
func (w *Writer) WriteRun(run *TestRun) (string, error) {
	if run == nil {
		return "", fmt.Errorf("nil test run")
	}
	if run.ID == "" {
		run.ID = generateRunID()
	}
	selection := run.Config.Selection
	if selection == "" {
		selection = run.Config.Variant
	}
	if !safeFilePart(run.ID) || !safeLogicalPart(selection) {
		return "", fmt.Errorf("unsafe run identity or selection")
	}
	if run.SchemaVersion == 0 {
		run.SchemaVersion = 2
	}
	if run.SchemaVersion != 2 {
		return "", fmt.Errorf("unsupported test run schema version %d", run.SchemaVersion)
	}
	if (run.ParentID == "") != (run.ParentMemberID == "") ||
		run.ParentID != "" && (!safeFilePart(run.ParentID) || !safeLogicalPart(run.ParentMemberID)) {
		return "", fmt.Errorf("parent run and member identity must be paired")
	}
	if err := validateChildExpectations(run); err != nil {
		return "", err
	}
	if err := validateEnvironmentKeys(run.Environment); err != nil {
		return "", err
	}
	outputDir, err := filepath.Abs(w.outputDir)
	if err != nil {
		return "", fmt.Errorf("resolving output directory: %w", err)
	}
	if run.Environment == nil {
		run.Environment = make(map[string]string)
	}
	run.Environment["output_dir"] = outputDir
	if run.StartedAt.IsZero() {
		if !run.Timestamp.IsZero() {
			run.StartedAt = run.Timestamp.UTC()
		} else {
			run.StartedAt = time.Now().UTC()
		}
	}
	if run.Timestamp.IsZero() {
		run.Timestamp = run.StartedAt
	}
	run.StartedAt = run.StartedAt.UTC()
	run.Timestamp = run.Timestamp.UTC()
	if !run.CompletedAt.IsZero() {
		run.CompletedAt = run.CompletedAt.UTC()
	}
	if run.CompletedAt.IsZero() != (run.ExitCode == nil) {
		return "", fmt.Errorf("partial terminal result has completion without exit or exit without completion")
	}
	// Bind terminal observations to the persisted initial declaration. A
	// mismatch is retained as failed evidence, never an acceptance shortcut.
	filename := fmt.Sprintf("e2e-results-%s-%s.json", strings.ReplaceAll(selection, ":", "_"), run.ID)
	runPath := filepath.Join(w.outputDir, filename)
	var missingAtSubmission []string
	if run.Config.RequireEvidence && run.ExitCode != nil {
		missingAtSubmission, err = reconcileInitializedChecks(runPath, run)
		if err != nil {
			return "", err
		}
	}
	for i := range run.Scenarios {
		child := &run.Scenarios[i]
		if run.Config.RequireEvidence && run.ExitCode != nil {
			executionPassed := child.Success
			_ = child.FinalizeChecks()
			if !executionPassed {
				child.Success = false
				child.EvidenceStatus = "unattested"
			}
		}
		if child.Structured != nil {
			child.Structured.Metadata.Success = child.Success
			child.Structured.Metadata.ErrorCount = len(child.Errors)
			child.Structured.Metadata.Errors = append([]string(nil), child.Errors...)
			if child.Error != "" {
				child.Structured.Metadata.ErrorCount++
				child.Structured.Metadata.Errors = append(child.Structured.Metadata.Errors, child.Error)
			}
			child.Structured.Metadata.WarningCount = len(child.Warnings)
			child.Structured.Metadata.Warnings = append([]string(nil), child.Warnings...)
		}
	}
	run.Summary = computeSummary(run.Scenarios)
	if run.Config.RequireEvidence && run.ExitCode != nil && *run.ExitCode == 0 {
		if _, ready := evaluateRequiredProof(run); !ready {
			failed := 1
			run.ExitCode = &failed
		}
	}
	if run.Config.RequireEvidence {
		run.Summary.MissingRequiredMembers, run.Summary.RequiredProofPassed = evaluateRequiredProof(run)
		for _, member := range missingAtSubmission {
			if !slices.Contains(run.Summary.MissingRequiredMembers, member) {
				run.Summary.MissingRequiredMembers = append(run.Summary.MissingRequiredMembers, member)
			}
		}
		sort.Strings(run.Summary.MissingRequiredMembers)
	}
	run.EvidenceStatus = "unattested"
	if run.Summary.RequiredProofPassed {
		run.EvidenceStatus = "complete"
	}
	// Ensure output directory exists
	if err := os.MkdirAll(w.outputDir, 0755); err != nil {
		return "", fmt.Errorf("creating output directory: %w", err)
	}

	// The run identity keeps sibling invocations distinct, including those that
	// start within one second. Initial and final aggregate writes use this path.
	// Marshal with indentation for readability
	data, err := json.MarshalIndent(run, "", "  ")
	if err != nil {
		return "", fmt.Errorf("marshaling results: %w", err)
	}

	if err := writeRunAtomically(runPath, run, data); err != nil {
		return "", fmt.Errorf("writing results file: %w", err)
	}

	return runPath, nil
}

func reconcileInitializedChecks(path string, run *TestRun) ([]string, error) {
	data, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return nil, nil // A direct terminal write has no previous snapshot.
	}
	if err != nil {
		return nil, fmt.Errorf("reading initialized run: %w", err)
	}
	var prior TestRun
	if err := json.Unmarshal(data, &prior); err != nil {
		return nil, fmt.Errorf("decoding initialized run: %w", err)
	}
	if prior.ID != run.ID || !prior.StartedAt.Equal(run.StartedAt) ||
		prior.ParentID != run.ParentID || prior.ParentMemberID != run.ParentMemberID ||
		!reflect.DeepEqual(prior.Config, run.Config) {
		return nil, fmt.Errorf("run path belongs to another invocation")
	}
	initial := make(map[string]scenarios.Result, len(prior.Scenarios))
	duplicates := make(map[string]bool)
	for _, child := range prior.Scenarios {
		if _, exists := initial[child.MemberID]; exists {
			duplicates[child.MemberID] = true
		}
		initial[child.MemberID] = child
	}
	submitted := make(map[string]bool, len(run.Scenarios))
	canonical := make([]scenarios.Result, 0, len(run.Scenarios))
	position := make(map[string]int, len(run.Scenarios))
	for _, observed := range run.Scenarios {
		child := observed
		submitted[child.MemberID] = true
		if index, duplicate := position[child.MemberID]; duplicate {
			kept := &canonical[index]
			kept.Errors = append(kept.Errors,
				fmt.Sprintf("duplicate submitted member %q with run identity %q", child.MemberID, child.RunID))
			if child.Error != "" {
				kept.Errors = append(kept.Errors, child.Error)
			}
			kept.Errors = append(kept.Errors, child.Errors...)
			kept.Warnings = append(kept.Warnings, child.Warnings...)
			kept.CheckObservations = append(kept.CheckObservations, child.CheckObservations...)
			continue
		}
		declared, found := initial[child.MemberID]
		switch {
		case !found:
			child.CheckRequirements = nil
			child.Errors = append(child.Errors, "member was not declared before execution")
		case duplicates[child.MemberID]:
			child.CheckRequirements = append([]scenarios.CheckRequirement(nil), declared.CheckRequirements...)
			child.Errors = append(child.Errors, "member was declared more than once before execution")
		default:
			if declared.RunID != child.RunID {
				child.Errors = append(child.Errors,
					fmt.Sprintf("submitted member run identity %q differs from initialized %q", child.RunID, declared.RunID))
				child.RunID = declared.RunID
			}
			if !slices.Equal(declared.CheckRequirements, child.CheckRequirements) {
				child.Errors = append(child.Errors, "required checks changed after initialization")
			}
			child.CheckRequirements = append([]scenarios.CheckRequirement(nil), declared.CheckRequirements...)
		}
		position[child.MemberID] = len(canonical)
		canonical = append(canonical, child)
	}
	run.Scenarios = canonical
	var missing []string
	for _, memberID := range run.Config.RequiredMembers {
		if submitted[memberID] {
			continue
		}
		missing = append(missing, memberID)
		if declared, found := initial[memberID]; found {
			run.Scenarios = append(run.Scenarios, scenarios.Result{
				ScenarioName:      declared.ScenarioName,
				RunID:             run.ID,
				MemberID:          memberID,
				CheckRequirements: append([]scenarios.CheckRequirement(nil), declared.CheckRequirements...),
				Errors:            []string{"required member missing from terminal submission"},
			})
		}
	}
	return missing, nil
}

func safeFilePart(value string) bool {
	if value == "" || value == "." || value == ".." {
		return false
	}
	for _, r := range value {
		if r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' || r == '-' || r == '_' || r == '.' {
			continue
		}
		return false
	}
	return true
}

func safeLogicalPart(value string) bool {
	if value == "" || value == "." || value == ".." {
		return false
	}
	for _, r := range value {
		if r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' ||
			r == '-' || r == '_' || r == '.' || r == ':' {
			continue
		}
		return false
	}
	return true
}

func writeRunAtomically(path string, run *TestRun, data []byte) error {
	newPath := false
	if prior, err := os.ReadFile(path); err == nil {
		var existing TestRun
		if err := json.Unmarshal(prior, &existing); err != nil {
			return fmt.Errorf("existing run unreadable: %w", err)
		}
		if existing.ID != run.ID || !existing.StartedAt.Equal(run.StartedAt) ||
			existing.ParentID != run.ParentID || existing.ParentMemberID != run.ParentMemberID ||
			!reflect.DeepEqual(existing.Config, run.Config) {
			return fmt.Errorf("run path belongs to another invocation")
		}
		if run.Config.RequireEvidence && existing.ExitCode != nil && run.ExitCode == nil {
			return fmt.Errorf("terminal run cannot return to initialization")
		}
		if run.Config.RequireEvidence && existing.ExitCode == nil && run.ExitCode == nil &&
			!sameMemberDeclarations(existing.Scenarios, run.Scenarios) {
			return fmt.Errorf("initialized member declarations changed")
		}
	} else if !os.IsNotExist(err) {
		return fmt.Errorf("reading existing run: %w", err)
	} else {
		newPath = true
	}
	tmp, err := os.CreateTemp(filepath.Dir(path), ".e2e-results-*")
	if err != nil {
		return err
	}
	tmpPath := tmp.Name()
	defer os.Remove(tmpPath)
	if _, err := tmp.Write(data); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Sync(); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	if err := os.Chmod(tmpPath, 0644); err != nil {
		return err
	}
	if newPath {
		return os.Link(tmpPath, path)
	}
	return os.Rename(tmpPath, path)
}

func sameMemberDeclarations(left, right []scenarios.Result) bool {
	if len(left) != len(right) {
		return false
	}
	declared := make(map[string]scenarios.Result, len(left))
	for _, member := range left {
		if _, duplicate := declared[member.MemberID]; duplicate {
			return false
		}
		declared[member.MemberID] = member
	}
	seen := make(map[string]bool, len(right))
	for _, member := range right {
		prior, found := declared[member.MemberID]
		if !found || seen[member.MemberID] || prior.RunID != member.RunID ||
			!slices.Equal(prior.CheckRequirements, member.CheckRequirements) {
			return false
		}
		seen[member.MemberID] = true
	}
	return true
}

// WriteLatest writes results and also creates/updates a "latest" symlink
func (w *Writer) WriteLatest(run *TestRun) (string, error) {
	filepath, err := w.WriteRun(run)
	if err != nil {
		return "", err
	}

	// Create/update latest symlink (best effort - may fail on Windows)
	latestLink := filepath + "-latest.json"
	_ = os.Remove(latestLink) // Remove existing link if present
	_ = os.Symlink(filepath, latestLink)

	return filepath, nil
}

// WriteComparison writes a comparison report to disk
func (w *Writer) WriteComparison(comparison *Comparison) (string, error) {
	// Ensure output directory exists
	if err := os.MkdirAll(w.outputDir, 0755); err != nil {
		return "", fmt.Errorf("creating output directory: %w", err)
	}

	filename := fmt.Sprintf("e2e-comparison-%s.json",
		comparison.Timestamp.Format("20060102-150405"))
	filepath := filepath.Join(w.outputDir, filename)

	data, err := json.MarshalIndent(comparison, "", "  ")
	if err != nil {
		return "", fmt.Errorf("marshaling comparison: %w", err)
	}

	if err := os.WriteFile(filepath, data, 0644); err != nil {
		return "", fmt.Errorf("writing comparison file: %w", err)
	}

	return filepath, nil
}

// LoadRun loads a test run from disk
func (w *Writer) LoadRun(filepath string) (*TestRun, error) {
	data, err := os.ReadFile(filepath)
	if err != nil {
		return nil, fmt.Errorf("reading results file: %w", err)
	}

	run, err := decodeRun(data)
	if err != nil {
		return nil, fmt.Errorf("unmarshaling results: %w", err)
	}
	return run, nil
}

func decodeRun(data []byte) (*TestRun, error) {
	var run TestRun
	if err := json.Unmarshal(data, &run); err != nil {
		return nil, err
	}
	switch run.SchemaVersion {
	case 0:
		if run.ID == "" {
			return nil, fmt.Errorf("legacy report has no run ID")
		}
		// Old files have only execution aggregates. Never infer named proof.
		run.EvidenceStatus = "unattested"
		run.Summary.RequiredProofPassed = false
		for i := range run.Scenarios {
			run.Scenarios[i].EvidenceStatus = "unattested"
		}
		return &run, nil
	case 2:
		if (run.ParentID == "") != (run.ParentMemberID == "") ||
			run.ParentID != "" && (!safeFilePart(run.ParentID) || !safeLogicalPart(run.ParentMemberID)) {
			return nil, fmt.Errorf("parent run and member identity must be paired")
		}
		if err := validateChildExpectations(&run); err != nil {
			return nil, err
		}
		if err := validateEnvironmentKeys(run.Environment); err != nil {
			return nil, err
		}
		if run.EvidenceStatus != "unattested" && run.EvidenceStatus != "complete" {
			return nil, fmt.Errorf("unknown evidence status %q", run.EvidenceStatus)
		}
		if !safeFilePart(run.ID) {
			return nil, fmt.Errorf("invalid run ID %q", run.ID)
		}
		_, complete := evaluateRequiredProof(&run)
		if run.CompletedAt.IsZero() != (run.ExitCode == nil) {
			return nil, fmt.Errorf("partial terminal result")
		}
		if run.Config.RequireEvidence && run.ExitCode != nil && *run.ExitCode == 0 && !complete {
			return nil, fmt.Errorf("required run has zero overall exit without complete proof")
		}
		if run.EvidenceStatus == "complete" && !complete {
			return nil, fmt.Errorf("complete report lacks required proof")
		}
		if run.Summary.RequiredProofPassed != (run.EvidenceStatus == "complete") {
			return nil, fmt.Errorf("summary and evidence status disagree")
		}
		return &run, nil
	default:
		return nil, fmt.Errorf("unsupported test run schema version %d", run.SchemaVersion)
	}
}

func evaluateRequiredProof(run *TestRun) ([]string, bool) {
	if !run.Config.RequireEvidence {
		return nil, false
	}
	missing, err := checkRequiredEvidence(run)
	complete := err == nil && !run.CompletedAt.IsZero() && !run.CompletedAt.Before(run.StartedAt) &&
		run.ExitCode != nil && *run.ExitCode == 0
	return missing, complete
}

func checkRequiredEvidence(run *TestRun) ([]string, error) {
	if run == nil {
		return nil, fmt.Errorf("nil test run")
	}
	wanted := make(map[string]bool, len(run.Config.RequiredMembers))
	var problems []string
	if run.SchemaVersion != 2 || !safeFilePart(run.ID) || !safeLogicalPart(run.Config.Selection) {
		problems = append(problems, "unsupported schema or run/selection identity")
	}
	if run.StartedAt.IsZero() || len(run.Command) == 0 || !filepath.IsAbs(run.WorkingDir) {
		problems = append(problems, "run start, argv or absolute working directory missing")
	}
	if len(run.Config.RequiredMembers) == 0 {
		problems = append(problems, "required member set is empty")
	}
	if run.Config.RequireTaskStatuses && (run.CommandExitCode == nil || run.CleanupExitCode == nil) {
		problems = append(problems, "task command or cleanup status missing")
	}
	if run.CommandExitCode != nil && *run.CommandExitCode != 0 ||
		run.CleanupExitCode != nil && *run.CleanupExitCode != 0 {
		problems = append(problems, "task command or cleanup failed")
	}
	for _, member := range run.Config.RequiredMembers {
		if !safeLogicalPart(member) || wanted[member] {
			problems = append(problems, fmt.Sprintf("invalid or duplicate required member %q", member))
		}
		wanted[member] = true
	}
	seen := make(map[string]bool, len(run.Scenarios))
	for i := range run.Scenarios {
		child := &run.Scenarios[i]
		if !wanted[child.MemberID] || seen[child.MemberID] || child.RunID != run.ID ||
			child.EvidenceStatus != "complete" || !child.Success {
			problems = append(problems, fmt.Sprintf("member %q has foreign, duplicate or incomplete proof", child.MemberID))
		}
		seen[child.MemberID] = true
		// Validate persisted observations, not just an author-supplied success bit.
		verified := *child
		// FinalizeChecks updates the typed projection. Validation must not mutate
		// the retained child through this shallow Result copy.
		verified.Structured = nil
		if err := verified.FinalizeChecks(); err != nil || !verified.Success ||
			verified.AssertionsRun != child.AssertionsRun {
			problems = append(problems, fmt.Sprintf("member %q required checks do not finalize", child.MemberID))
		}
		if child.Structured != nil && child.Structured.Metadata.Success != child.Success {
			problems = append(problems, fmt.Sprintf("member %q typed outcome disagrees", child.MemberID))
		}
	}
	missing := make([]string, 0)
	for member := range wanted {
		if !seen[member] {
			missing = append(missing, member)
			problems = append(problems, fmt.Sprintf("required member %q missing", member))
		}
	}
	sort.Strings(missing)
	problems = append(problems, requiredProvenanceProblems(run.Environment)...)
	if len(problems) > 0 {
		sort.Strings(problems)
		return missing, fmt.Errorf("required evidence incomplete: %s", strings.Join(problems, "; "))
	}
	return missing, nil
}

func requiredProvenanceProblems(env map[string]string) []string {
	var problems []string
	if !hexLength(env["source_sha"], 40) || !hexLength(env["runner_sha256"], 64) {
		problems = append(problems, "source or runner identity unavailable or malformed")
	}
	for _, key := range []string{"compose_sha256", "config_sha256", "fixture_sha256", "effective_settings_sha256"} {
		if !hexLength(env[key], 64) {
			problems = append(problems, key+" unavailable or malformed")
		}
	}
	for _, key := range []string{"runner_build", "app_build", "profiles"} {
		if unavailable(env[key]) {
			problems = append(problems, key+" unavailable")
		}
	}
	if !imageDigest(env["app_image_id"]) && !imageDigest(env["app_image_digest"]) &&
		!hexLength(env["app_binary_sha256"], 64) {
		problems = append(problems, "application binary or image identity unavailable")
	}
	if env["source_dirty"] != "true" && env["source_dirty"] != "false" {
		problems = append(problems, "source dirty status unavailable")
	}
	if !digestOrNotApplicable(env["source_patch_sha256"]) ||
		!digestOrNotApplicable(env["source_untracked_sha256"]) {
		problems = append(problems, "source patch or untracked input digest unavailable")
	}
	if env["source_dirty"] == "true" &&
		!hexLength(env["source_patch_sha256"], 64) && !hexLength(env["source_untracked_sha256"], 64) {
		problems = append(problems, "dirty source input digest unavailable")
	}
	if !filepath.IsAbs(env["output_dir"]) {
		problems = append(problems, "absolute output directory unavailable")
	}
	for _, pair := range [][2]string{{"log_path", "log_sha256"}, {"artifact_manifest_path", "artifact_manifest_sha256"}} {
		path := env[pair[0]]
		if unavailable(path) || path == "." || path == ".." || strings.HasPrefix(filepath.Clean(path), ".."+string(filepath.Separator)) {
			problems = append(problems, pair[0]+" unavailable or unsafe")
		}
		if !hexLength(env[pair[1]], 64) {
			problems = append(problems, pair[1]+" unavailable or malformed")
		}
	}
	return problems
}

func hexLength(value string, length int) bool {
	if len(value) != length {
		return false
	}
	_, err := hex.DecodeString(value)
	return err == nil
}

func imageDigest(value string) bool {
	index := strings.LastIndex(value, "sha256:")
	return index >= 0 && hexLength(value[index+len("sha256:"):], 64)
}

func digestOrNotApplicable(value string) bool {
	return hexLength(value, 64) || strings.HasPrefix(value, "not_applicable: ") && len(value) > len("not_applicable: ")
}

var allowedEnvironmentKeys = map[string]bool{
	"source_sha": true, "source_dirty": true, "source_patch_sha256": true,
	"source_untracked_sha256": true, "runner_sha256": true, "runner_build": true,
	"app_image_id": true, "app_image_digest": true, "app_binary_sha256": true,
	"app_build": true, "compose_sha256": true, "profiles": true,
	"config_sha256": true, "fixture_sha256": true, "effective_settings_sha256": true,
	"log_path": true, "log_sha256": true,
	"artifact_manifest_path": true, "artifact_manifest_sha256": true,
	"output_dir": true,
	"command_scope": true, "outer_launcher_argv": true,
}

func validateEnvironmentKeys(env map[string]string) error {
	for key := range env {
		if !allowedEnvironmentKeys[key] {
			return fmt.Errorf("unclassified environment key %q", key)
		}
	}
	return nil
}

func unavailable(value string) bool {
	value = strings.ToLower(strings.TrimSpace(value))
	return value == "" || strings.HasPrefix(value, "unavailable") || strings.HasPrefix(value, "unknown") ||
		strings.HasPrefix(value, "not_applicable")
}

// CreateTestRun creates a new TestRun with computed summary
func CreateTestRun(
	config TestRunConfig,
	scenarioResults []scenarios.Result,
	metrics *client.MetricsReport,
	duration time.Duration,
) *TestRun {
	run := &TestRun{
		SchemaVersion: 2,
		ID:            generateRunID(),
		Timestamp:     time.Now().UTC(),
		Duration:      duration,
		DurationStr:   duration.String(),
		Config:        config,
		Scenarios:     scenarioResults,
		Metrics:       metrics,
	}
	run.StartedAt = run.Timestamp
	run.EvidenceStatus = "unattested"

	// Compute summary
	run.Summary = computeSummary(scenarioResults)

	return run
}

// computeSummary calculates summary statistics from scenario results
func computeSummary(results []scenarios.Result) Summary {
	summary := Summary{
		TotalScenarios: len(results),
	}

	for _, r := range results {
		if r.Success {
			summary.PassedScenarios++
		} else {
			summary.FailedScenarios++
		}
		summary.TotalErrors += len(r.Errors)
		summary.TotalWarnings += len(r.Warnings)
	}

	if summary.TotalScenarios > 0 {
		summary.SuccessRate = float64(summary.PassedScenarios) / float64(summary.TotalScenarios)
	}

	summary.AllPassed = summary.PassedScenarios == summary.TotalScenarios

	return summary
}

// generateRunID creates a unique run identifier
func generateRunID() string {
	return fmt.Sprintf("run-%d-%d-%d", time.Now().UTC().UnixNano(), os.Getpid(), runCounter.Add(1))
}

var runCounter atomic.Uint64

// Compare compares two test runs and produces a comparison report
func Compare(baseline, current *TestRun) *Comparison {
	comparison := &Comparison{
		Timestamp:              time.Now(),
		BaselineID:             baseline.ID,
		CurrentID:              current.ID,
		BaselineEvidenceStatus: baseline.EvidenceStatus,
		CurrentEvidenceStatus:  current.EvidenceStatus,
		BaselineDate:           baseline.Timestamp,
		CurrentDate:            current.Timestamp,
		Diffs:                  make([]ScenarioDiff, 0),
		MetricsDiff:            make(map[string]Diff),
	}

	// Create map of baseline scenarios by name
	baselineMap := make(map[string]scenarios.Result)
	for _, r := range baseline.Scenarios {
		baselineMap[r.ScenarioName] = r
	}

	// Compare each current scenario
	for _, current := range current.Scenarios {
		baseline, hasBaseline := baselineMap[current.ScenarioName]

		diff := ScenarioDiff{
			ScenarioName:   current.ScenarioName,
			CurrentSuccess: current.Success,
		}

		if hasBaseline {
			diff.BaselineSuccess = baseline.Success
			diff.StatusChanged = baseline.Success != current.Success
			diff.DurationChange = current.Duration - baseline.Duration
			diff.DurationChangeMs = diff.DurationChange.Milliseconds()

			// Compare metrics if available
			if baseline.Metrics != nil && current.Metrics != nil {
				diff.MetricsDiff = compareMetricMaps(
					baseline.Metrics,
					current.Metrics,
				)
			}

			if diff.StatusChanged {
				comparison.Overall.StatusChanges++
				if current.Success && !baseline.Success {
					comparison.Overall.Improvements++
				} else if !current.Success && baseline.Success {
					comparison.Overall.Regressions++
				}
			}
		}

		comparison.Diffs = append(comparison.Diffs, diff)
	}

	// Compare overall metrics if available
	if baseline.Metrics != nil && current.Metrics != nil {
		comparison.MetricsDiff = compareMetricReports(
			baseline.Metrics,
			current.Metrics,
		)

		for _, d := range comparison.MetricsDiff {
			if d.Improved {
				comparison.Overall.MetricsImproved++
			}
			if d.Regressed {
				comparison.Overall.MetricsRegressed++
			}
		}
	}

	// Determine overall improvement
	comparison.Overall.OverallImproved = comparison.Overall.Improvements > comparison.Overall.Regressions &&
		comparison.Overall.MetricsImproved >= comparison.Overall.MetricsRegressed

	return comparison
}

// compareMetricMaps compares two metric maps
func compareMetricMaps(baseline, current map[string]any) map[string]Diff {
	diffs := make(map[string]Diff)

	// Get all unique keys
	keys := make(map[string]bool)
	for k := range baseline {
		keys[k] = true
	}
	for k := range current {
		keys[k] = true
	}

	for k := range keys {
		baseVal := toFloat64(baseline[k])
		currVal := toFloat64(current[k])

		if baseVal == 0 && currVal == 0 {
			continue
		}

		diff := Diff{
			Baseline: baseVal,
			Current:  currVal,
			Absolute: currVal - baseVal,
		}

		if baseVal != 0 {
			diff.Percent = ((currVal - baseVal) / baseVal) * 100
		}

		// For most metrics, higher is better (more processed, more hits)
		// For error counts and latencies, lower is better
		isErrorMetric := isNegativeMetric(k)
		if isErrorMetric {
			diff.Improved = currVal < baseVal
			diff.Regressed = currVal > baseVal
		} else {
			diff.Improved = currVal > baseVal
			diff.Regressed = currVal < baseVal
		}

		diffs[k] = diff
	}

	return diffs
}

// compareMetricReports compares two MetricsReports
func compareMetricReports(baseline, current *client.MetricsReport) map[string]Diff {
	diffs := make(map[string]Diff)

	// Compare counters
	for k, currVal := range current.Counters {
		baseVal := baseline.Counters[k]
		diff := createDiff(k, baseVal, currVal)
		diffs["counter:"+k] = diff
	}

	// Compare gauges
	for k, currVal := range current.Gauges {
		baseVal := baseline.Gauges[k]
		diff := createDiff(k, baseVal, currVal)
		diffs["gauge:"+k] = diff
	}

	return diffs
}

// createDiff creates a Diff struct for two values
func createDiff(key string, baseline, current float64) Diff {
	diff := Diff{
		Baseline: baseline,
		Current:  current,
		Absolute: current - baseline,
	}

	if baseline != 0 {
		diff.Percent = ((current - baseline) / baseline) * 100
	}

	isError := isNegativeMetric(key)
	if isError {
		diff.Improved = current < baseline
		diff.Regressed = current > baseline
	} else {
		diff.Improved = current > baseline
		diff.Regressed = current < baseline
	}

	return diff
}

// isNegativeMetric returns true for metrics where lower is better
func isNegativeMetric(name string) bool {
	negativePatterns := []string{
		"error", "fail", "miss", "drop", "reject", "timeout", "latency", "duration",
	}
	lower := strings.ToLower(name)
	for _, pattern := range negativePatterns {
		if strings.Contains(lower, pattern) {
			return true
		}
	}
	return false
}

// toFloat64 converts an interface value to float64
func toFloat64(v any) float64 {
	switch val := v.(type) {
	case float64:
		return val
	case float32:
		return float64(val)
	case int:
		return float64(val)
	case int64:
		return float64(val)
	case int32:
		return float64(val)
	default:
		slog.Debug("unexpected type in toFloat64",
			"type", fmt.Sprintf("%T", v),
			"value", v)
		return 0
	}
}

// ListRuns returns all result files in the output directory
func (w *Writer) ListRuns() ([]string, error) {
	entries, err := os.ReadDir(w.outputDir)
	if err != nil {
		if os.IsNotExist(err) {
			return []string{}, nil
		}
		return nil, fmt.Errorf("reading output directory: %w", err)
	}

	type datedRun struct {
		path    string
		started time.Time
	}
	var runs []datedRun
	for _, entry := range entries {
		if !entry.IsDir() && filepath.Ext(entry.Name()) == ".json" &&
			strings.HasPrefix(entry.Name(), "e2e-results-") && !strings.HasSuffix(entry.Name(), "-latest.json") {
			path := filepath.Join(w.outputDir, entry.Name())
			run, err := w.LoadRun(path)
			if err != nil {
				return nil, fmt.Errorf("listing %s: %w", path, err)
			}
			started := run.StartedAt
			if started.IsZero() {
				started = run.Timestamp
			}
			if started.IsZero() {
				return nil, fmt.Errorf("listing %s: run has no recorded start", path)
			}
			runs = append(runs, datedRun{path: path, started: started})
		}
	}

	// Run-ID filenames no longer sort by time across different selections.
	sort.Slice(runs, func(i, j int) bool {
		if runs[i].started.Equal(runs[j].started) {
			return runs[i].path < runs[j].path
		}
		return runs[i].started.Before(runs[j].started)
	})
	files := make([]string, len(runs))
	for i, run := range runs {
		files[i] = run.path
	}
	return files, nil
}

// GetLatestRun returns the most recent test run
func (w *Writer) GetLatestRun() (*TestRun, error) {
	files, err := w.ListRuns()
	if err != nil {
		return nil, err
	}

	if len(files) == 0 {
		return nil, fmt.Errorf("no test runs found")
	}

	// Files are sorted, last one is most recent
	return w.LoadRun(files[len(files)-1])
}

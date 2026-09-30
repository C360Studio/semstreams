package main

import (
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"runtime/debug"
	"slices"
	"sort"
	"strings"
	"time"

	"github.com/c360studio/semstreams/test/e2e/config"
	"github.com/c360studio/semstreams/test/e2e/results"
	"github.com/c360studio/semstreams/test/e2e/scenarios"
)

// handleTaskReportCommand is a serializer for Task-owned execution. It never
// starts a scenario, container, command, retry or cleanup operation.
func handleTaskReportCommand(flags *cliFlags) (bool, int) {
	if flags == nil {
		return false, 0
	}
	actions := 0
	for _, selected := range []bool{flags.reportInit, flags.reportRecord, flags.reportChild, flags.reportFinalize} {
		if selected {
			actions++
		}
	}
	if actions == 0 {
		return false, 0
	}
	if actions != 1 {
		fmt.Fprintln(os.Stderr, "E2E report requires exactly one report action")
		return true, 1
	}
	if flags.reportInit {
		scope, err := resolveTaskReportScope(flags.reportSelection)
		if err != nil {
			fmt.Fprintf(os.Stderr, "E2E report initialization rejected selection: %s\n", err)
			return true, 1
		}
		var argv []string
		if err := json.Unmarshal([]byte(flags.reportArgvJSON), &argv); err != nil || len(argv) == 0 {
			fmt.Fprintln(os.Stderr, "E2E report initialization requires observed argv JSON array")
			return true, 1
		}
		for _, arg := range argv {
			if arg == "" {
				fmt.Fprintln(os.Stderr, "E2E report argv cannot contain an empty argument")
				return true, 1
			}
		}
		run, path, err := initializeTaskReport(flags.outputDir, scope, flags.reportParentID, flags.reportParentSlot, argv)
		if err != nil {
			fmt.Fprintf(os.Stderr, "E2E report initialization failed at %s: %s\n", path, err)
			return true, 1
		}
		if err := json.NewEncoder(os.Stdout).Encode(struct {
			RunID string `json:"run_id"`
			Path  string `json:"path"`
		}{RunID: run.ID, Path: path}); err != nil {
			fmt.Fprintf(os.Stderr, "E2E report initialization output failed for %s: %s\n", path, err)
			return true, 1
		}
		return true, 0
	}
	if flags.reportRecord {
		file, err := os.Open(flags.reportInputPath)
		if err != nil {
			fmt.Fprintf(os.Stderr, "E2E report observation unavailable: %s\n", err)
			return true, 1
		}
		defer file.Close()
		decoder := json.NewDecoder(file)
		decoder.DisallowUnknownFields()
		var observation scenarios.CheckObservation
		if err := decoder.Decode(&observation); err != nil {
			fmt.Fprintf(os.Stderr, "E2E report observation invalid: %s\n", err)
			return true, 1
		}
		var trailing any
		if err := decoder.Decode(&trailing); err != io.EOF {
			fmt.Fprintln(os.Stderr, "E2E report observation has trailing data")
			return true, 1
		}
		path, err := recordTaskObservation(flags.reportRunPath, observation)
		if err != nil {
			fmt.Fprintf(os.Stderr, "E2E report observation failed at %s: %s\n", path, err)
			return true, 1
		}
		fmt.Fprintln(os.Stdout, path)
		return true, 0
	}
	if flags.reportFinalize {
		input, inputErr := readReportManifestInput(flags.reportManifestInputPath)
		final, err := finalizeTaskReport(flags.reportRunPath, flags.reportCommandExit, flags.reportCleanupExit, input)
		if final.Path != "" {
			fmt.Fprintln(os.Stdout, final.Path)
		}
		if combined := errors.Join(inputErr, err); combined != nil {
			fmt.Fprintf(os.Stderr, "E2E Task report finalization failed at %s: %s\n", flags.reportRunPath, combined)
			return true, 1
		}
		return true, final.ExitCode
	}
	if flags.reportChildExit < 0 {
		fmt.Fprintln(os.Stderr, "E2E child report requires observed nonnegative process exit")
		return true, 1
	}
	path, err := recordTaskChild(flags.reportRunPath, flags.reportMemberID,
		flags.reportChildPath, flags.reportChildExit, flags.reportLogPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "E2E child report failed at %s: %s\n", path, err)
		return true, 1
	}
	fmt.Fprintln(os.Stdout, path)
	return true, 0
}

func readReportManifestInput(path string) (reportManifestInput, error) {
	file, err := os.Open(path)
	if err != nil {
		return reportManifestInput{}, fmt.Errorf("manifest input unavailable: %w", err)
	}
	defer file.Close()
	decoder := json.NewDecoder(file)
	decoder.DisallowUnknownFields()
	var input reportManifestInput
	if err := decoder.Decode(&input); err != nil {
		return reportManifestInput{}, fmt.Errorf("manifest input invalid: %w", err)
	}
	var trailing any
	if err := decoder.Decode(&trailing); err != io.EOF {
		return reportManifestInput{}, fmt.Errorf("manifest input has trailing data")
	}
	return input, nil
}

// reportManifestInput contains observations supplied by the owning Task after
// it has selected the actual files and inspected each running application.
// The reporter only checks and serializes these observations.
type reportManifestInput struct {
	OutputDir         string                    `json:"output_dir"`
	ParentID          string                    `json:"parent_id,omitempty"`
	ParentMemberID    string                    `json:"parent_member_id,omitempty"`
	Selection         string                    `json:"selection"`
	Profiles          string                    `json:"profiles"`
	Files             []reportManifestFileInput `json:"files"`
	AppPhases         []reportManifestAppInput  `json:"app_phases"`
	EffectiveSettings map[string]string         `json:"-"`
}

type reportManifestFileInput struct {
	Role string `json:"role"`
	Path string `json:"path"`
}

type reportManifestAppInput struct {
	Name        string `json:"name"`
	ImageID     string `json:"image_id"`
	ImageDigest string `json:"image_digest"`
	BinaryPath  string `json:"binary_path"`
	Build       string `json:"build"`
}

type reportManifest struct {
	OutputDir             string                   `json:"output_dir"`
	SourceSHA             string                   `json:"source_sha"`
	SourceDirty           string                   `json:"source_dirty"`
	SourcePatchSHA256     string                   `json:"source_patch_sha256"`
	SourceUntrackedSHA256 string                   `json:"source_untracked_sha256"`
	Profiles              string                   `json:"profiles"`
	EffectiveSettings     map[string]string        `json:"effective_settings"`
	Files                 []reportManifestFile     `json:"files"`
	AppPhases             []reportManifestAppPhase `json:"app_phases"`
}

type reportManifestFile struct {
	Role   string `json:"role"`
	Path   string `json:"path"`
	SHA256 string `json:"sha256"`
}

type reportManifestAppPhase struct {
	Name         string `json:"name"`
	ImageID      string `json:"image_id"`
	ImageDigest  string `json:"image_digest"`
	BinaryPath   string `json:"binary_path"`
	BinarySHA256 string `json:"binary_sha256"`
	Build        string `json:"build"`
}

var reportFileRoles = map[string]bool{
	"compose": true, "config": true, "fixture": true,
	"task_log": true, "artifact": true,
}

func buildReportManifest(input reportManifestInput) (reportManifest, error) {
	if !filepath.IsAbs(input.OutputDir) || filepath.Clean(input.OutputDir) != input.OutputDir {
		return reportManifest{}, fmt.Errorf("manifest output directory must be absolute and clean")
	}
	manifest := reportManifest{OutputDir: input.OutputDir, Profiles: input.Profiles}
	if input.Selection == "" {
		return reportManifest{}, fmt.Errorf("manifest selection is absent")
	}
	manifest.EffectiveSettings = map[string]string{
		"selection": input.Selection, "parent_id": input.ParentID,
		"parent_member_id": input.ParentMemberID, "profiles": input.Profiles,
	}
	for key, value := range input.EffectiveSettings {
		manifest.EffectiveSettings[key] = value
	}
	seenFiles := make(map[string]bool, len(input.Files))
	for _, file := range input.Files {
		if !reportFileRoles[file.Role] {
			return reportManifest{}, fmt.Errorf("unclassified manifest file role %q", file.Role)
		}
		key := file.Role + "\x00" + file.Path
		if seenFiles[key] {
			return reportManifest{}, fmt.Errorf("duplicate manifest file %q", file.Path)
		}
		seenFiles[key] = true
		digest, err := digestObservedFile(file.Path)
		if err != nil {
			return reportManifest{}, fmt.Errorf("manifest %s file: %w", file.Role, err)
		}
		manifest.Files = append(manifest.Files, reportManifestFile{Role: file.Role, Path: file.Path, SHA256: digest})
	}
	if len(input.AppPhases) == 0 {
		return reportManifest{}, fmt.Errorf("manifest has no observed application phase")
	}
	seenPhases := make(map[string]bool, len(input.AppPhases))
	for _, app := range input.AppPhases {
		if app.Name == "" || seenPhases[app.Name] {
			return reportManifest{}, fmt.Errorf("empty or duplicate application phase %q", app.Name)
		}
		seenPhases[app.Name] = true
		if !strings.HasPrefix(app.ImageID, "sha256:") || !hex64(strings.TrimPrefix(app.ImageID, "sha256:")) {
			return reportManifest{}, fmt.Errorf("application phase %q lacks observed immutable image ID", app.Name)
		}
		if app.Build == "" {
			return reportManifest{}, fmt.Errorf("application phase %q lacks observed build identity", app.Name)
		}
		if !strings.HasPrefix(app.ImageDigest, "unavailable: ") && !observedImageDigest(app.ImageDigest) {
			return reportManifest{}, fmt.Errorf("application phase %q lacks registry digest disposition", app.Name)
		}
		binaryDigest, err := digestObservedFile(app.BinaryPath)
		if err != nil {
			return reportManifest{}, fmt.Errorf("application phase %q binary: %w", app.Name, err)
		}
		manifest.AppPhases = append(manifest.AppPhases, reportManifestAppPhase{
			Name: app.Name, ImageID: app.ImageID, ImageDigest: app.ImageDigest,
			BinaryPath: app.BinaryPath, BinarySHA256: binaryDigest, Build: app.Build,
		})
	}
	source, err := observeReportSource(input.Files)
	if err != nil {
		return reportManifest{}, err
	}
	manifest.SourceSHA, manifest.SourceDirty = source.SHA, source.Dirty
	manifest.SourcePatchSHA256, manifest.SourceUntrackedSHA256 = source.PatchSHA256, source.UntrackedSHA256
	executable, err := os.Executable()
	if err != nil {
		return reportManifest{}, fmt.Errorf("observing reporter executable: %w", err)
	}
	digest, err := digestObservedFile(executable)
	if err != nil {
		return reportManifest{}, fmt.Errorf("reporter executable: %w", err)
	}
	manifest.Files = append(manifest.Files, reportManifestFile{Role: "runner", Path: executable, SHA256: digest})
	return manifest, nil
}

func observedImageDigest(value string) bool {
	index := strings.LastIndex(value, "sha256:")
	return index >= 0 && hex64(value[index+len("sha256:"):])
}

type reportSourceObservation struct {
	SHA, Dirty, PatchSHA256, UntrackedSHA256 string
}

func observeReportSource(files []reportManifestFileInput) (reportSourceObservation, error) {
	git := func(args ...string) ([]byte, error) {
		output, err := exec.Command("git", args...).Output()
		if err != nil {
			return nil, fmt.Errorf("observing source with git %s: %w", strings.Join(args, " "), err)
		}
		return output, nil
	}
	rootBytes, err := git("rev-parse", "--show-toplevel")
	if err != nil {
		return reportSourceObservation{}, err
	}
	root := strings.TrimSpace(string(rootBytes))
	shaBytes, err := git("rev-parse", "HEAD")
	if err != nil {
		return reportSourceObservation{}, err
	}
	sha := strings.TrimSpace(string(shaBytes))
	if len(sha) != 40 {
		return reportSourceObservation{}, fmt.Errorf("source HEAD is not a full SHA-1 commit")
	}
	status, err := git("status", "--porcelain", "-z", "--untracked-files=all")
	if err != nil {
		return reportSourceObservation{}, err
	}
	patch, err := git("diff", "HEAD", "--binary", "--")
	if err != nil {
		return reportSourceObservation{}, err
	}
	result := reportSourceObservation{SHA: sha, Dirty: fmt.Sprintf("%t", len(status) > 0),
		PatchSHA256:     "not_applicable: no tracked source patch",
		UntrackedSHA256: "not_applicable: no selected untracked input"}
	if len(patch) > 0 {
		result.PatchSHA256 = fmt.Sprintf("%x", sha256.Sum256(patch))
	}
	untracked, err := git("ls-files", "--others", "--exclude-standard", "-z")
	if err != nil {
		return reportSourceObservation{}, err
	}
	known := make(map[string]bool)
	for _, part := range strings.Split(string(untracked), "\x00") {
		if part != "" {
			known[filepath.Join(root, part)] = true
		}
	}
	var selected []reportManifestFile
	for _, file := range files {
		if known[file.Path] {
			digest, err := digestObservedFile(file.Path)
			if err != nil {
				return reportSourceObservation{}, fmt.Errorf("selected untracked input: %w", err)
			}
			selected = append(selected, reportManifestFile{Role: file.Role, Path: file.Path, SHA256: digest})
		}
	}
	if len(selected) > 0 {
		sort.Slice(selected, func(i, j int) bool { return selected[i].Path < selected[j].Path })
		data, err := json.Marshal(selected)
		if err != nil {
			return reportSourceObservation{}, err
		}
		result.UntrackedSHA256 = fmt.Sprintf("%x", sha256.Sum256(data))
	}
	return result, nil
}

func digestObservedFile(path string) (string, error) {
	if !filepath.IsAbs(path) || filepath.Clean(path) != path {
		return "", fmt.Errorf("path %q must be absolute and clean", path)
	}
	file, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer file.Close()
	info, err := file.Stat()
	if err != nil {
		return "", err
	}
	if !info.Mode().IsRegular() {
		return "", fmt.Errorf("path %q is not a regular file", path)
	}
	digest := sha256.New()
	if _, err := io.Copy(digest, file); err != nil {
		return "", err
	}
	return fmt.Sprintf("%x", digest.Sum(nil)), nil
}

func hex64(value string) bool {
	if len(value) != 64 {
		return false
	}
	for _, digit := range value {
		if digit < '0' || digit > '9' && digit < 'a' || digit > 'f' {
			return false
		}
	}
	return true
}

func reportManifestEnvironment(manifest reportManifest) map[string]string {
	env := map[string]string{
		"output_dir":                manifest.OutputDir,
		"source_sha":                manifest.SourceSHA,
		"source_dirty":              manifest.SourceDirty,
		"source_patch_sha256":       manifest.SourcePatchSHA256,
		"source_untracked_sha256":   manifest.SourceUntrackedSHA256,
		"profiles":                  manifest.Profiles,
		"runner_sha256":             reportRoleDigest(manifest.Files, "runner"),
		"compose_sha256":            reportRoleDigest(manifest.Files, "compose"),
		"config_sha256":             reportRoleDigest(manifest.Files, "config"),
		"fixture_sha256":            reportRoleDigest(manifest.Files, "fixture"),
		"effective_settings_sha256": reportSettingsDigest(manifest.EffectiveSettings),
	}
	if info, ok := debug.ReadBuildInfo(); ok {
		env["runner_build"] = info.Main.Path + "@" + info.Main.Version + " " + runtime.Version()
	} else {
		env["runner_build"] = "unavailable: Go build information was not present"
	}
	if len(manifest.AppPhases) > 0 {
		app := manifest.AppPhases[0]
		env["app_image_id"] = app.ImageID
		env["app_image_digest"] = app.ImageDigest
		env["app_binary_sha256"] = app.BinarySHA256
		env["app_build"] = app.Build
	}
	var logs []reportManifestFile
	for _, file := range manifest.Files {
		if file.Role == "task_log" {
			logs = append(logs, file)
		}
	}
	if len(logs) == 1 {
		env["log_path"], env["log_sha256"] = logs[0].Path, logs[0].SHA256
	} else {
		env["log_path"] = "unavailable: Task lifecycle log was not retained exactly once"
		env["log_sha256"] = "unavailable: Task lifecycle log was not retained exactly once"
	}
	return env
}

func reportSettingsDigest(settings map[string]string) string {
	data, err := json.Marshal(settings)
	if err != nil {
		return "unavailable: effective settings could not be serialized"
	}
	return fmt.Sprintf("%x", sha256.Sum256(data))
}

func reportRoleDigest(files []reportManifestFile, role string) string {
	var selected []reportManifestFile
	for _, file := range files {
		if file.Role == role {
			selected = append(selected, file)
		}
	}
	if len(selected) == 0 {
		return "unavailable: no " + role + " constituent was observed"
	}
	if len(selected) == 1 {
		return selected[0].SHA256
	}
	sort.Slice(selected, func(i, j int) bool { return selected[i].Path < selected[j].Path })
	data, _ := json.Marshal(selected)
	sum := sha256.Sum256(data)
	return fmt.Sprintf("%x", sum)
}

// taskReportScope declares only the Task targets adopted for required evidence.
// Task still owns the commands and their order; these declarations name what a
// successful execution must report before its result can be complete.
type taskReportScope struct {
	selection string
	members   []taskReportMember
	excluded  []string
}

type taskReportMember struct {
	id     string
	checks []scenarios.CheckRequirement
	child  *taskReportChild
}

type taskReportChild struct {
	selector            string
	selection           string
	requiredMembers     []string
	requireTaskStatuses bool
	adopted             bool
}

func (s taskReportScope) memberIDs() []string {
	ids := make([]string, 0, len(s.members))
	for _, member := range s.members {
		ids = append(ids, member.id)
	}
	return ids
}

func shellReportMember(id string) taskReportMember {
	return taskReportMember{
		id:     id,
		checks: []scenarios.CheckRequirement{{ID: id, Required: true}},
	}
}

func cliReportMember(id, selector string) (taskReportMember, error) {
	resolved, err := resolveScenarioSelection(&cliFlags{scenarioName: selector})
	if err != nil {
		return taskReportMember{}, err
	}
	return taskReportMember{
		id:     id,
		checks: []scenarios.CheckRequirement{{ID: id, Required: true}},
		child: &taskReportChild{
			selector:        selector,
			selection:       resolved.label,
			requiredMembers: append([]string(nil), resolved.members...),
			adopted:         resolved.required,
		},
	}, nil
}

func taskChildReportMember(id string) (taskReportMember, error) {
	child, err := resolveTaskReportScope(id)
	if err != nil {
		return taskReportMember{}, err
	}
	return taskReportMember{
		id:     id,
		checks: []scenarios.CheckRequirement{{ID: id, Required: true}},
		child: &taskReportChild{
			selection:           child.selection,
			requiredMembers:     child.memberIDs(),
			requireTaskStatuses: true,
			adopted:             true,
		},
	}, nil
}

func resolveTaskReportScope(selection string) (taskReportScope, error) {
	scope := taskReportScope{selection: selection}
	switch selection {
	case "core":
		// Each shell assertion is its own immutable member. A failed comparison
		// can be written once and cannot be replaced by a later cleanup pass.
		for _, id := range []string{"core.readiness", "core.heartbeat"} {
			scope.members = append(scope.members, shellReportMember(id))
		}
		minted, err := cliReportMember("core.minted-authority", "core-minted-authority")
		if err != nil {
			return taskReportScope{}, err
		}
		scope.members = append(scope.members, minted)
		coreCLI, err := cliReportMember("core.core-scenarios", "all")
		if err != nil {
			return taskReportScope{}, err
		}
		scope.members = append(scope.members, coreCLI)
		for _, id := range []string{
			"core.shutdown.exit", "core.shutdown.log", "core.shutdown.listeners",
			"core.early-cancel.exit", "core.early-cancel.no-services",
			"core.preidentity.refusal", "core.preidentity.no-record",
		} {
			scope.members = append(scope.members, shellReportMember(id))
		}
		graph, err := cliReportMember("core.graph-roundtrip.identity", "core-graph-roundtrip")
		if err != nil {
			return taskReportScope{}, err
		}
		scope.members = append(scope.members, graph)
	case "structural", "statistical", "semantic", "agentic", "slow-consumer":
		selector := selection
		if selection == "slow-consumer" {
			selector = "core-slow-consumer"
		}
		child, err := cliReportMember(selection, selector)
		if err != nil {
			return taskReportScope{}, err
		}
		scope.members = []taskReportMember{child}
	case "core-inference-agentic":
		for _, id := range []string{"core", "structural", "statistical", "semantic", "agentic"} {
			member, err := taskChildReportMember(id)
			if err != nil {
				return taskReportScope{}, err
			}
			scope.members = append(scope.members, member)
		}
		scope.excluded = []string{
			"slow-consumer", "lessons", "research-graph direct", "research-graph execute",
			"deep-research", "CRUD-tools", "ops", "lifecycle", "throughput",
			"semantic 8b", "semantic frontier", "semantic fallback", "live OpenAI adapter",
		}
	default:
		return taskReportScope{}, fmt.Errorf("unknown required Task report selection %q", selection)
	}
	return scope, nil
}

// validateTaskReportScope checks the actual selected constructors before Task
// starts Compose. A resolver label alone cannot prove a scenario was adopted.
func validateTaskReportScope(scope taskReportScope) error {
	if scope.selection == "" || len(scope.members) == 0 {
		return fmt.Errorf("empty required Task scope")
	}
	seen := make(map[string]bool, len(scope.members))
	for _, member := range scope.members {
		if member.id == "" || seen[member.id] || len(member.checks) == 0 {
			return fmt.Errorf("invalid or duplicate Task member %q", member.id)
		}
		seen[member.id] = true
		if member.child == nil {
			continue
		}
		if !member.child.adopted || len(member.child.requiredMembers) == 0 {
			return fmt.Errorf("required Task child %q is unadopted or empty", member.id)
		}
		if member.child.requireTaskStatuses {
			childScope, err := resolveTaskReportScope(member.child.selection)
			if err != nil {
				return err
			}
			if !slices.Equal(childScope.memberIDs(), member.child.requiredMembers) {
				return fmt.Errorf("Task child %q differs from selected member snapshot", member.id)
			}
			if err := validateTaskReportScope(childScope); err != nil {
				return err
			}
			continue
		}
		selected, err := resolveScenarioSelection(&cliFlags{
			scenarioName: member.child.selector,
			baseURL:      config.DefaultEndpoints.HTTP,
			udpEndpoint:  config.DefaultEndpoints.UDP,
			metricsURL:   config.DefaultEndpoints.Metrics,
		})
		if err != nil || !selected.required || selected.label != member.child.selection ||
			!slices.Equal(selected.members, member.child.requiredMembers) {
			return fmt.Errorf("CLI child %q differs from required selected scope", member.id)
		}
		for _, selectedID := range selected.members {
			flags := selected.flags
			if flags.scenarioName == "all" {
				flags.scenarioName = selectedID
			}
			child := createScenario(nil, &flags)
			name := strings.SplitN(selectedID, ":", 2)[0]
			if child == nil || child.Name() != name {
				return fmt.Errorf("CLI child %q has no matching constructor for %q", member.id, selectedID)
			}
			checks, adopted := scenarioChecks(child)
			if !adopted {
				return fmt.Errorf("CLI child %q member %q lacks required declarations", member.id, selectedID)
			}
			probe := &scenarios.Result{}
			if err := probe.DeclareChecks("admission-probe", selectedID, checks); err != nil {
				return fmt.Errorf("CLI child %q member %q declaration: %w", member.id, selectedID, err)
			}
		}
	}
	return nil
}

// initializeTaskReport persists one incomplete Task aggregate after Task's
// prerequisite dependencies have succeeded. It never launches the Task body.
func initializeTaskReport(outputDir string, scope taskReportScope, parentID, parentMemberID string,
	command []string) (*results.TestRun, string, error) {
	if err := validateTaskReportScope(scope); err != nil {
		return nil, "", err
	}
	if !filepath.IsAbs(outputDir) || filepath.Clean(outputDir) != outputDir {
		return nil, "", fmt.Errorf("Task output directory must be absolute and clean")
	}
	if len(command) == 0 {
		return nil, "", fmt.Errorf("Task invocation argv was not observed")
	}
	if (parentID == "") != (parentMemberID == "") {
		return nil, "", fmt.Errorf("parent run and slot must be supplied together")
	}
	wd, err := os.Getwd()
	if err != nil {
		return nil, "", fmt.Errorf("observing Task working directory: %w", err)
	}
	run := results.CreateTestRun(results.TestRunConfig{
		Selection: scope.selection, RequireEvidence: true, RequireTaskStatuses: true,
		RequiredMembers: scope.memberIDs(), ExcludedMembers: append([]string(nil), scope.excluded...),
		Scenarios: scope.memberIDs(),
	}, nil, nil, 0)
	run.ParentID, run.ParentMemberID = parentID, parentMemberID
	run.Command = append([]string(nil), command...)
	run.WorkingDir = wd
	for _, member := range scope.members {
		declaration := scenarios.Result{ScenarioName: member.id}
		if err := declaration.DeclareChecks(run.ID, member.id, member.checks); err != nil {
			return nil, "", fmt.Errorf("declaring Task member %q: %w", member.id, err)
		}
		run.Scenarios = append(run.Scenarios, declaration)
		if member.child != nil {
			run.Config.ChildExpectations = append(run.Config.ChildExpectations, results.ChildExpectation{
				ParentMemberID: member.id, Selection: member.child.selection,
				RequiredMembers:     append([]string(nil), member.child.requiredMembers...),
				RequireTaskStatuses: member.child.requireTaskStatuses,
			})
		}
	}
	path, err := results.NewWriter(outputDir).WriteRun(run)
	if err != nil {
		return nil, path, fmt.Errorf("writing initial Task report: %w", err)
	}
	return run, path, nil
}

func loadInitializedTaskReport(path string) (*results.TestRun, *results.Writer, error) {
	if !filepath.IsAbs(path) || filepath.Clean(path) != path {
		return nil, nil, fmt.Errorf("Task report path must be absolute and clean")
	}
	writer := results.NewWriter(filepath.Dir(path))
	run, err := writer.LoadRun(path)
	if err != nil {
		return nil, nil, err
	}
	if run.ExitCode != nil || !run.Config.RequireEvidence || !run.Config.RequireTaskStatuses {
		return nil, nil, fmt.Errorf("Task report is not an initialized required run")
	}
	return run, writer, nil
}

func recordTaskObservation(runPath string, observation scenarios.CheckObservation) (string, error) {
	run, writer, err := loadInitializedTaskReport(runPath)
	if err != nil {
		return "", err
	}
	for _, declaration := range run.Scenarios {
		if declaration.MemberID != observation.MemberID {
			continue
		}
		member := declaration
		if err := member.RecordCheck(observation); err != nil {
			return "", err
		}
		return writer.WriteMember(run, &member)
	}
	return "", fmt.Errorf("unknown Task member %q", observation.MemberID)
}

// recordTaskChild observes the external process status and verifies the exact
// child bytes against the expectation retained at parent initialization.
// A failed verification is itself a failed parent observation, never a pass.
func recordTaskChild(runPath, memberID, childPath string, childExit int, logPath string) (string, error) {
	run, writer, err := loadInitializedTaskReport(runPath)
	if err != nil {
		return "", err
	}
	var expected *results.ChildExpectation
	for i := range run.Config.ChildExpectations {
		if run.Config.ChildExpectations[i].ParentMemberID == memberID {
			expected = &run.Config.ChildExpectations[i]
			break
		}
	}
	if expected == nil {
		return "", fmt.Errorf("member %q has no initialized child expectation", memberID)
	}
	var declaration *scenarios.Result
	for i := range run.Scenarios {
		if run.Scenarios[i].MemberID == memberID {
			declaration = &run.Scenarios[i]
			break
		}
	}
	if declaration == nil || len(declaration.CheckRequirements) != 1 {
		return "", fmt.Errorf("member %q has no exact child proof declaration", memberID)
	}
	evidence := map[string]string{
		"child_path": childPath, "child_exit": fmt.Sprintf("%d", childExit), "log_path": logPath,
	}
	status, reason := "passed", ""
	if childExit != 0 {
		status, reason = "failed", fmt.Sprintf("child process exited %d", childExit)
	} else if childPath == "" {
		status, reason = "failed", "child report path is absent"
	} else {
		verified := *expected
		verified.ParentID = run.ID
		artifact, verifyErr := writer.VerifyChild(childPath, verified)
		if verifyErr != nil {
			status, reason = "failed", fmt.Sprintf("child report verification: %s", verifyErr)
		} else {
			evidence["child_run_id"] = artifact.RunID
			evidence["child_path"] = artifact.Path
			evidence["child_sha256"] = artifact.SHA256
		}
	}
	member := *declaration
	if err := member.RecordCheck(scenarios.CheckObservation{
		ID: declaration.CheckRequirements[0].ID, RunID: run.ID, MemberID: memberID,
		Status: status, Reason: reason, Evidence: evidence,
	}); err != nil {
		return "", err
	}
	path, err := writer.WriteMember(run, &member)
	if err != nil {
		return path, err
	}
	if status != "passed" {
		return path, fmt.Errorf("%s", reason)
	}
	return path, nil
}

type taskReportFinal struct {
	Path     string
	ExitCode int
}

// finalizeTaskReport consumes only the initialized declarations and their
// write-once member files. Task has already completed cleanup and closed its
// lifecycle log before this call.
func finalizeTaskReport(runPath string, commandExit, cleanupExit int,
	input reportManifestInput) (taskReportFinal, error) {
	initial, writer, err := loadInitializedTaskReport(runPath)
	if err != nil {
		return taskReportFinal{ExitCode: 1}, err
	}
	terminal := *initial
	terminal.Scenarios = append([]scenarios.Result(nil), initial.Scenarios...)
	terminal.Environment = make(map[string]string, len(initial.Environment)+20)
	for key, value := range initial.Environment {
		terminal.Environment[key] = value
	}
	var problems []error
	if commandExit < 0 || cleanupExit < 0 {
		problems = append(problems, fmt.Errorf("Task command and cleanup exits must be observed"))
	}
	if input.OutputDir != initial.Environment["output_dir"] {
		problems = append(problems, fmt.Errorf("manifest output location differs from initialized Task run"))
	} else if input.Selection != initial.Config.Selection || input.ParentID != initial.ParentID ||
		input.ParentMemberID != initial.ParentMemberID {
		problems = append(problems, fmt.Errorf("manifest selection or parent slot differs from initialized Task run"))
	} else {
		input.EffectiveSettings = map[string]string{
			"command_exit": fmt.Sprintf("%d", commandExit),
			"cleanup_exit": fmt.Sprintf("%d", cleanupExit),
		}
		manifest, prepareErr := buildReportManifest(input)
		if prepareErr != nil {
			problems = append(problems, fmt.Errorf("preparing Task manifest: %w", prepareErr))
		} else {
			for key, value := range reportManifestEnvironment(manifest) {
				terminal.Environment[key] = value
			}
			data, marshalErr := json.MarshalIndent(manifest, "", "  ")
			if marshalErr != nil {
				problems = append(problems, fmt.Errorf("marshaling Task manifest: %w", marshalErr))
			} else {
				reference, retainErr := writer.WriteManifest(initial, data)
				if retainErr != nil {
					problems = append(problems, fmt.Errorf("retaining Task manifest: %w", retainErr))
				} else {
					terminal.Environment["artifact_manifest_path"] = reference.Path
					terminal.Environment["artifact_manifest_sha256"] = reference.SHA256
				}
			}
		}
	}
	if terminal.Environment["artifact_manifest_path"] == "" {
		terminal.Environment["artifact_manifest_path"] = "unavailable: Task manifest could not be retained"
	}
	if terminal.Environment["artifact_manifest_sha256"] == "" {
		terminal.Environment["artifact_manifest_sha256"] = "unavailable: Task manifest could not be retained"
	}
	for _, memberID := range initial.Config.RequiredMembers {
		index := -1
		for i := range terminal.Scenarios {
			if terminal.Scenarios[i].MemberID == memberID {
				index = i
				break
			}
		}
		if index < 0 {
			problems = append(problems, fmt.Errorf("initialized declaration missing member %q", memberID))
			continue
		}
		member, loadErr := writer.LoadMember(initial, memberID)
		if loadErr != nil {
			terminal.Scenarios[index].Errors = append(terminal.Scenarios[index].Errors,
				fmt.Sprintf("report member %q: %s", memberID, loadErr))
			problems = append(problems, fmt.Errorf("loading report member %q: %w", memberID, loadErr))
			continue
		}
		terminal.Scenarios[index] = *member
	}
	if commandExit >= 0 {
		terminal.CommandExitCode = &commandExit
	}
	if cleanupExit >= 0 {
		terminal.CleanupExitCode = &cleanupExit
	}
	terminal.CompletedAt = time.Now().UTC()
	terminal.Duration = terminal.CompletedAt.Sub(terminal.StartedAt)
	terminal.DurationStr = terminal.Duration.String()
	exit := 0
	if commandExit != 0 || cleanupExit != 0 || len(problems) > 0 {
		exit = 1
	}
	terminal.ExitCode = &exit
	path, writeErr := writer.WriteRun(&terminal)
	if writeErr != nil {
		problems = append(problems, fmt.Errorf("writing terminal Task report at %s: %w", runPath, writeErr))
		return taskReportFinal{Path: runPath, ExitCode: 1}, errors.Join(problems...)
	}
	final := taskReportFinal{Path: path, ExitCode: *terminal.ExitCode}
	if commandExit != 0 {
		problems = append(problems, fmt.Errorf("Task command exited %d", commandExit))
	}
	if cleanupExit != 0 {
		problems = append(problems, fmt.Errorf("Task cleanup exited %d", cleanupExit))
	}
	if final.ExitCode != 0 && len(problems) == 0 {
		problems = append(problems, fmt.Errorf("required Task proof is incomplete"))
	}
	return final, errors.Join(problems...)
}

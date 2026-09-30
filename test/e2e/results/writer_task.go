package results

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"strings"

	"github.com/c360studio/semstreams/test/e2e/scenarios"
)

// ChildExpectation binds a selected child to one parent slot and required set.
type ChildExpectation struct {
	ParentID            string   `json:"parent_id,omitempty"`
	ParentMemberID      string   `json:"parent_member_id"`
	Selection           string   `json:"selection"`
	RequiredMembers     []string `json:"required_members"`
	RequireTaskStatuses bool     `json:"require_task_statuses,omitempty"`
}

// ArtifactReference identifies exact retained bytes belonging to one test run.
type ArtifactReference struct {
	RunID  string `json:"run_id"`
	Path   string `json:"path"`
	SHA256 string `json:"sha256"`
}

func validateChildExpectations(run *TestRun) error {
	if run == nil {
		return fmt.Errorf("nil test run")
	}
	selected := make(map[string]bool, len(run.Config.RequiredMembers))
	for _, member := range run.Config.RequiredMembers {
		selected[member] = true
	}
	seen := make(map[string]bool, len(run.Config.ChildExpectations))
	for _, expected := range run.Config.ChildExpectations {
		if expected.ParentID != "" && expected.ParentID != run.ID {
			return fmt.Errorf("child expectation has foreign parent ID")
		}
		if !safeLogicalPart(expected.ParentMemberID) || !selected[expected.ParentMemberID] || seen[expected.ParentMemberID] {
			return fmt.Errorf("child expectation has invalid, unselected or duplicate slot %q", expected.ParentMemberID)
		}
		seen[expected.ParentMemberID] = true
		if !safeLogicalPart(expected.Selection) || len(expected.RequiredMembers) == 0 {
			return fmt.Errorf("child expectation for %q has invalid selection or empty members", expected.ParentMemberID)
		}
		members := make(map[string]bool, len(expected.RequiredMembers))
		for _, member := range expected.RequiredMembers {
			if !safeLogicalPart(member) || members[member] {
				return fmt.Errorf("child expectation for %q has invalid or duplicate required member %q", expected.ParentMemberID, member)
			}
			members[member] = true
		}
	}
	return nil
}

// VerifyChild reads one child artifact and verifies exact parent, slot and scope.
func (w *Writer) VerifyChild(path string, expected ChildExpectation) (ArtifactReference, error) {
	if !safeFilePart(expected.ParentID) || !safeLogicalPart(expected.ParentMemberID) ||
		!safeLogicalPart(expected.Selection) || len(expected.RequiredMembers) == 0 {
		return ArtifactReference{}, fmt.Errorf("invalid child expectation")
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return ArtifactReference{}, fmt.Errorf("reading child report: %w", err)
	}
	run, err := decodeRun(data)
	if err != nil {
		return ArtifactReference{}, fmt.Errorf("decoding child report: %w", err)
	}
	if run.EvidenceStatus != "complete" || !run.Summary.RequiredProofPassed || run.ExitCode == nil || *run.ExitCode != 0 ||
		run.ParentID != expected.ParentID || run.ParentMemberID != expected.ParentMemberID ||
		run.Config.Selection != expected.Selection || run.Config.RequireTaskStatuses != expected.RequireTaskStatuses ||
		!sameRequiredMembers(run.Config.RequiredMembers, expected.RequiredMembers) {
		return ArtifactReference{}, fmt.Errorf("child report does not match complete expected parent slot and scope")
	}
	for _, pair := range [][2]string{{"log_path", "log_sha256"}, {"artifact_manifest_path", "artifact_manifest_sha256"}} {
		if err := verifyRetainedReference(run.Environment["output_dir"], run.Environment[pair[0]], run.Environment[pair[1]]); err != nil {
			return ArtifactReference{}, fmt.Errorf("child %s: %w", pair[0], err)
		}
	}
	writerDir, err := filepath.Abs(w.outputDir)
	if err != nil {
		return ArtifactReference{}, fmt.Errorf("resolving Writer output directory: %w", err)
	}
	childPath, err := filepath.Abs(path)
	if err != nil {
		return ArtifactReference{}, fmt.Errorf("resolving child report path: %w", err)
	}
	reference, err := filepath.Rel(writerDir, childPath)
	if err != nil {
		return ArtifactReference{}, fmt.Errorf("making child report reference: %w", err)
	}
	sum := sha256.Sum256(data)
	return ArtifactReference{RunID: run.ID, Path: reference, SHA256: fmt.Sprintf("%x", sum)}, nil
}

func verifyRetainedReference(outputDir, path, digest string) error {
	if !filepath.IsAbs(path) {
		path = filepath.Join(outputDir, path)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return fmt.Errorf("reading retained artifact: %w", err)
	}
	sum := sha256.Sum256(data)
	if fmt.Sprintf("%x", sum) != digest {
		return fmt.Errorf("retained artifact digest differs")
	}
	return nil
}

func sameRequiredMembers(actual, expected []string) bool {
	if len(actual) != len(expected) || len(actual) == 0 {
		return false
	}
	selected := make(map[string]bool, len(expected))
	for _, member := range expected {
		if !safeLogicalPart(member) || selected[member] {
			return false
		}
		selected[member] = true
	}
	for _, member := range actual {
		if !selected[member] {
			return false
		}
		delete(selected, member)
	}
	return len(selected) == 0
}

func (w *Writer) memberPath(run *TestRun, memberID string) (string, error) {
	if run == nil || !safeFilePart(run.ID) || !safeLogicalPart(memberID) {
		return "", fmt.Errorf("invalid run or member identity")
	}
	return filepath.Join(w.outputDir, fmt.Sprintf("e2e-member-%s-%s.json", run.ID, hex.EncodeToString([]byte(memberID)))), nil
}

func (w *Writer) declaredMember(run *TestRun, memberID string) (*scenarios.Result, error) {
	if run == nil || !slices.Contains(run.Config.RequiredMembers, memberID) {
		return nil, fmt.Errorf("member %q is not required by this run", memberID)
	}
	var declared *scenarios.Result
	for i := range run.Scenarios {
		member := &run.Scenarios[i]
		if member.MemberID != memberID {
			continue
		}
		if declared != nil {
			return nil, fmt.Errorf("member %q declared more than once", memberID)
		}
		declared = member
	}
	if declared == nil || declared.RunID != run.ID || len(declared.CheckRequirements) == 0 ||
		len(declared.CheckObservations) != 0 {
		return nil, fmt.Errorf("member %q lacks an initialized declaration", memberID)
	}
	return declared, nil
}

func (w *Writer) verifyInitializedSnapshot(run *TestRun) error {
	if run == nil {
		return fmt.Errorf("nil run")
	}
	if run.ExitCode != nil || !run.CompletedAt.IsZero() {
		return fmt.Errorf("run is not an initialized nonterminal snapshot")
	}
	selection := run.Config.Selection
	if !safeLogicalPart(selection) {
		return fmt.Errorf("invalid initialized selection")
	}
	path := filepath.Join(w.outputDir, fmt.Sprintf("e2e-results-%s-%s.json", strings.ReplaceAll(selection, ":", "_"), run.ID))
	stored, err := w.LoadRun(path)
	if err != nil {
		return fmt.Errorf("loading initialized run: %w", err)
	}
	if stored.ID != run.ID || !stored.StartedAt.Equal(run.StartedAt) || stored.ExitCode != nil ||
		stored.ParentID != run.ParentID || stored.ParentMemberID != run.ParentMemberID ||
		!reflect.DeepEqual(stored.Config, run.Config) || len(stored.Scenarios) != len(run.Scenarios) {
		return fmt.Errorf("run is not the initialized snapshot")
	}
	outputDir, err := filepath.Abs(w.outputDir)
	if err != nil {
		return fmt.Errorf("resolving Writer output directory: %w", err)
	}
	if stored.Environment["output_dir"] != outputDir || run.Environment["output_dir"] != outputDir {
		return fmt.Errorf("initialized output directory differs from Writer")
	}
	for i := range stored.Scenarios {
		actual, given := stored.Scenarios[i], run.Scenarios[i]
		if actual.RunID != given.RunID || actual.MemberID != given.MemberID ||
			!slices.Equal(actual.CheckRequirements, given.CheckRequirements) ||
			len(actual.CheckObservations) != 0 || len(given.CheckObservations) != 0 {
			return fmt.Errorf("member declarations differ from initialized snapshot")
		}
	}
	return nil
}

// WriteManifest retains one exact JSON object as this initialized run's manifest.
func (w *Writer) WriteManifest(initialized *TestRun, data []byte) (ArtifactReference, error) {
	if initialized == nil || initialized.SchemaVersion != 2 || !safeFilePart(initialized.ID) ||
		initialized.ExitCode != nil || !initialized.CompletedAt.IsZero() {
		return ArtifactReference{}, fmt.Errorf("manifest needs a nonterminal initialized run")
	}
	if err := w.verifyInitializedSnapshot(initialized); err != nil {
		return ArtifactReference{}, err
	}
	if len(bytes.TrimSpace(data)) == 0 {
		return ArtifactReference{}, fmt.Errorf("manifest JSON is empty")
	}
	var object map[string]json.RawMessage
	if err := json.Unmarshal(data, &object); err != nil || object == nil {
		return ArtifactReference{}, fmt.Errorf("manifest must be a JSON object")
	}
	basename := fmt.Sprintf("e2e-manifest-%s.json", initialized.ID)
	outputDir, err := filepath.Abs(w.outputDir)
	if err != nil {
		return ArtifactReference{}, fmt.Errorf("resolving manifest output directory: %w", err)
	}
	path := filepath.Join(outputDir, basename)
	if err := writeNoClobber(path, data); err != nil {
		return ArtifactReference{}, fmt.Errorf("writing manifest %s: %w", path, err)
	}
	sum := sha256.Sum256(data)
	return ArtifactReference{RunID: initialized.ID, Path: basename, SHA256: fmt.Sprintf("%x", sum)}, nil
}

func writeNoClobber(path string, data []byte) error {
	tmp, err := os.CreateTemp(filepath.Dir(path), ".e2e-artifact-*")
	if err != nil {
		return err
	}
	defer os.Remove(tmp.Name())
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
	return os.Link(tmp.Name(), path)
}

// WriteMember retains one finalized observed member without replacing prior bytes.
func (w *Writer) WriteMember(run *TestRun, member *scenarios.Result) (string, error) {
	if member == nil {
		return "", fmt.Errorf("nil member")
	}
	if err := w.verifyInitializedSnapshot(run); err != nil {
		return "", err
	}
	declared, err := w.declaredMember(run, member.MemberID)
	if err != nil {
		return "", err
	}
	if member.RunID != run.ID || !slices.Equal(member.CheckRequirements, declared.CheckRequirements) {
		return "", fmt.Errorf("member identity or declared checks differ from initialization")
	}
	_ = member.FinalizeChecks()
	path, err := w.memberPath(run, member.MemberID)
	if err != nil {
		return "", err
	}
	data, err := json.MarshalIndent(member, "", "  ")
	if err != nil {
		return "", fmt.Errorf("marshaling member: %w", err)
	}
	if err := writeNoClobber(path, data); err != nil {
		return "", fmt.Errorf("retaining member without overwrite: %w", err)
	}
	return path, nil
}

// LoadMember reads the exact initialized member slot and preserves failed proof.
func (w *Writer) LoadMember(run *TestRun, memberID string) (*scenarios.Result, error) {
	if err := w.verifyInitializedSnapshot(run); err != nil {
		return nil, err
	}
	declared, err := w.declaredMember(run, memberID)
	if err != nil {
		return nil, err
	}
	path, err := w.memberPath(run, memberID)
	if err != nil {
		return nil, err
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("reading member: %w", err)
	}
	var member scenarios.Result
	if err := json.Unmarshal(data, &member); err != nil {
		return nil, fmt.Errorf("decoding member: %w", err)
	}
	if member.RunID != run.ID || member.MemberID != memberID ||
		!slices.Equal(member.CheckRequirements, declared.CheckRequirements) {
		return nil, fmt.Errorf("member identity or declarations do not match initialization")
	}
	return &member, nil
}

package entityidaudit

import (
	"bytes"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
)

// Result contains the concrete value corpus produced from one exact source set.
type Result struct {
	Candidates []Candidate
	Findings   []Finding
}

// AuditRepository audits one Git repository using a single reproducible source
// enumeration. Untracked files participate only when explicitly requested;
// ignored files never participate.
func AuditRepository(root string, includeUntracked bool) ([]Candidate, []Finding, error) {
	result, err := AuditRepositoryFull(root, includeUntracked)
	return result.Candidates, result.Findings, err
}

// AuditRepositoryFull inventories values from one Git-enumerated source set.
func AuditRepositoryFull(root string, includeUntracked bool) (Result, error) {
	args := []string{"-C", root, "ls-files", "-z", "--cached"}
	if includeUntracked {
		args = append(args, "--others", "--exclude-standard")
	}
	output, err := exec.Command("git", args...).Output()
	if err != nil {
		return Result{}, fmt.Errorf("enumerate Git source set: %w", err)
	}
	var files []string
	for _, raw := range bytes.Split(output, []byte{0}) {
		if len(raw) == 0 {
			continue
		}
		relative := filepath.Clean(string(raw))
		if ignoredRepositoryPath(relative) || !supportedExtension(strings.ToLower(filepath.Ext(relative))) {
			continue
		}
		path := filepath.Join(root, relative)
		if _, err := os.Stat(path); os.IsNotExist(err) {
			continue
		} else if err != nil {
			return Result{}, fmt.Errorf("inspect tracked source %s: %w", relative, err)
		}
		files = append(files, path)
	}
	sort.Strings(files)
	candidates, findings, err := auditFiles(files)
	if err != nil {
		return Result{}, err
	}
	if err := normalizeCandidatePaths(root, candidates, findings); err != nil {
		return Result{}, err
	}
	return Result{Candidates: candidates, Findings: findings}, nil
}

func normalizeCandidatePaths(root string, candidates []Candidate, findings []Finding) error {
	rootAbsolute, err := filepath.Abs(root)
	if err != nil {
		return fmt.Errorf("resolve entity-ID audit root: %w", err)
	}
	normalize := func(path string) (string, error) {
		pathAbsolute, err := filepath.Abs(path)
		if err != nil {
			return "", fmt.Errorf("resolve entity-ID candidate path %q: %w", path, err)
		}
		relative, err := filepath.Rel(rootAbsolute, pathAbsolute)
		if err != nil {
			return "", fmt.Errorf("make entity-ID candidate path relative to repository: %w", err)
		}
		if relative == ".." || strings.HasPrefix(relative, ".."+string(filepath.Separator)) {
			return "", fmt.Errorf("entity-ID candidate path escapes repository: %s", path)
		}
		return filepath.ToSlash(relative), nil
	}
	for index := range candidates {
		candidates[index].File, err = normalize(candidates[index].File)
		if err != nil {
			return err
		}
	}
	for index := range findings {
		findings[index].File, err = normalize(findings[index].File)
		if err != nil {
			return err
		}
	}
	return nil
}

func ignoredRepositoryPath(path string) bool {
	clean := filepath.ToSlash(path)
	return strings.HasPrefix(clean, "openspec/changes/archive/")
}

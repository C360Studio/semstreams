package entityidaudit

import (
	"bytes"
	"fmt"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
)

// Result contains the concrete value corpus and the distinct implementation
// surface inventory produced from one exact source set.
type Result struct {
	Candidates []Candidate
	Findings   []Finding
	Surfaces   []AuditedSurface
}

// AuditRepository audits one Git repository using a single reproducible source
// enumeration. Untracked files participate only when explicitly requested;
// ignored files never participate.
func AuditRepository(root string, includeUntracked bool) ([]Candidate, []Finding, error) {
	result, err := AuditRepositoryFull(root, includeUntracked)
	return result.Candidates, result.Findings, err
}

// AuditRepositoryFull inventories values and contract-bearing surfaces from
// one Git-enumerated source set.
func AuditRepositoryFull(root string, includeUntracked bool) (Result, error) {
	result, files, err := inventoryRepositoryFull(root, includeUntracked)
	if err != nil {
		return Result{}, err
	}
	if containsSemStreamsEntityAuthority(files) {
		if err := validateCheckedSurfaceDispositions(result.Surfaces); err != nil {
			return Result{}, err
		}
	}
	return result, nil
}

// InventoryRepositoryFull returns the exact value and surface inventory before
// checked-disposition enforcement. It exists so maintainers can generate a
// review candidate when source drift introduces an unreviewed surface.
func InventoryRepositoryFull(root string, includeUntracked bool) (Result, error) {
	result, _, err := inventoryRepositoryFull(root, includeUntracked)
	return result, err
}

func inventoryRepositoryFull(root string, includeUntracked bool) (Result, []string, error) {
	args := []string{"-C", root, "ls-files", "-z", "--cached"}
	if includeUntracked {
		args = append(args, "--others", "--exclude-standard")
	}
	output, err := exec.Command("git", args...).Output()
	if err != nil {
		return Result{}, nil, fmt.Errorf("enumerate Git source set: %w", err)
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
		files = append(files, filepath.Join(root, relative))
	}
	sort.Strings(files)
	candidates, findings, err := auditFiles(files)
	if err != nil {
		return Result{}, nil, err
	}
	surfaces, err := auditSurfaces(files)
	if err != nil {
		return Result{}, nil, err
	}
	if err := normalizeSurfacePaths(root, surfaces); err != nil {
		return Result{}, nil, err
	}
	applyCheckedSurfaceDispositions(surfaces)
	return Result{Candidates: candidates, Findings: findings, Surfaces: surfaces}, files, nil
}

func containsSemStreamsEntityAuthority(files []string) bool {
	for _, path := range files {
		if strings.HasSuffix(filepath.ToSlash(path), "pkg/types/entity_id.go") {
			return true
		}
	}
	return false
}

func normalizeSurfacePaths(root string, surfaces []AuditedSurface) error {
	rootAbsolute, err := filepath.Abs(root)
	if err != nil {
		return fmt.Errorf("resolve entity-ID audit root: %w", err)
	}
	for index := range surfaces {
		pathAbsolute, err := filepath.Abs(surfaces[index].File)
		if err != nil {
			return fmt.Errorf("resolve entity-ID surface path %q: %w", surfaces[index].File, err)
		}
		relative, err := filepath.Rel(rootAbsolute, pathAbsolute)
		if err != nil {
			return fmt.Errorf("make entity-ID surface path relative to repository: %w", err)
		}
		if relative == ".." || strings.HasPrefix(relative, ".."+string(filepath.Separator)) {
			return fmt.Errorf("entity-ID surface path escapes repository: %s", surfaces[index].File)
		}
		surfaces[index].File = filepath.ToSlash(relative)
	}
	return nil
}

func ignoredRepositoryPath(path string) bool {
	clean := filepath.ToSlash(path)
	return clean == "docs/operations/28-entity-id-source-corpus.json" ||
		strings.HasPrefix(clean, "openspec/changes/archive/")
}

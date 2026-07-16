package entityidaudit

import (
	"bytes"
	_ "embed"
	"encoding/json"
	"fmt"
	"path/filepath"
	"sort"
	"strings"
)

// SurfaceDisposition is one reviewed exact classification for an inventoried
// implementation surface. File, kind, and name form the stable identity.
type SurfaceDisposition struct {
	File           string `json:"file"`
	Kind           string `json:"kind"`
	Name           string `json:"name"`
	Classification string `json:"classification"`
	Basis          string `json:"basis"`
}

//go:embed surface_dispositions.json
var surfaceDispositionData []byte

var checkedSurfaceDispositions = mustLoadSurfaceDispositions(surfaceDispositionData)

func applyCheckedSurfaceDispositions(surfaces []AuditedSurface) {
	for index := range surfaces {
		key := surfaceKey(surfaces[index].File, surfaces[index].Kind, surfaces[index].Name)
		if disposition, ok := checkedSurfaceDispositions[key]; ok {
			surfaces[index].Classification = disposition.Classification + ":" + disposition.Basis
			continue
		}
		surfaces[index].Classification = "unreviewed:missing-disposition"
	}
}

func mustLoadSurfaceDispositions(data []byte) map[string]SurfaceDisposition {
	var entries []SurfaceDisposition
	if err := json.Unmarshal(data, &entries); err != nil {
		panic(fmt.Sprintf("decode checked entity-ID surface dispositions: %v", err))
	}
	out := make(map[string]SurfaceDisposition, len(entries))
	for _, entry := range entries {
		if entry.File == "" || filepath.IsAbs(entry.File) || entry.File != filepath.ToSlash(filepath.Clean(entry.File)) {
			panic(fmt.Sprintf("invalid repository-relative entity-ID surface disposition path %q", entry.File))
		}
		if entry.Classification != "relevant" && entry.Classification != "unrelated" {
			panic(fmt.Sprintf("invalid entity-ID surface disposition classification %q for %s", entry.Classification, surfaceKey(entry.File, entry.Kind, entry.Name)))
		}
		if strings.TrimSpace(entry.Basis) == "" {
			panic(fmt.Sprintf("missing entity-ID surface disposition basis for %s", surfaceKey(entry.File, entry.Kind, entry.Name)))
		}
		key := surfaceKey(entry.File, entry.Kind, entry.Name)
		if _, exists := out[key]; exists {
			panic(fmt.Sprintf("duplicate entity-ID surface disposition %s", key))
		}
		out[key] = entry
	}
	return out
}

func validateCheckedSurfaceDispositions(surfaces []AuditedSurface) error {
	return validateSurfaceDispositionCoverage(surfaces, checkedSurfaceDispositions)
}

func validateSurfaceDispositionCoverage(surfaces []AuditedSurface, dispositions map[string]SurfaceDisposition) error {
	present := make(map[string]bool, len(surfaces))
	for _, surface := range surfaces {
		key := surfaceKey(surface.File, surface.Kind, surface.Name)
		present[key] = true
		if strings.HasPrefix(surface.Classification, "unreviewed:") {
			return fmt.Errorf("unreviewed entity-ID surface: %s", key)
		}
		if !strings.HasPrefix(surface.Classification, "relevant:") && !strings.HasPrefix(surface.Classification, "unrelated:") {
			return fmt.Errorf("invalid entity-ID surface classification %q: %s", surface.Classification, key)
		}
	}
	var stale []string
	for key := range dispositions {
		if !present[key] {
			stale = append(stale, key)
		}
	}
	if len(stale) > 0 {
		sort.Strings(stale)
		return fmt.Errorf("checked entity-ID surface disposition has no inventoried surface: %s", stale[0])
	}
	return nil
}

func validateReviewedReportSurfaces(surfaces []AuditedSurface) error {
	for _, surface := range surfaces {
		if strings.HasPrefix(surface.Classification, "unreviewed:") {
			return fmt.Errorf("report contains unreviewed entity-ID surface: %s", surfaceKey(surface.File, surface.Kind, surface.Name))
		}
	}
	return nil
}

// MarshalSurfaceDispositionCandidates emits the exact current surface set,
// preserving checked decisions and marking every new group unreviewed. The
// result is review input and cannot replace the checked manifest until every
// unreviewed entry receives an explicit disposition and basis.
func MarshalSurfaceDispositionCandidates(surfaces []AuditedSurface) ([]byte, error) {
	ordered := append([]AuditedSurface(nil), surfaces...)
	sort.Slice(ordered, func(i, j int) bool {
		if ordered[i].File != ordered[j].File {
			return ordered[i].File < ordered[j].File
		}
		if ordered[i].Kind != ordered[j].Kind {
			return ordered[i].Kind < ordered[j].Kind
		}
		return ordered[i].Name < ordered[j].Name
	})
	entries := make([]SurfaceDisposition, 0, len(ordered))
	for _, surface := range ordered {
		key := surfaceKey(surface.File, surface.Kind, surface.Name)
		entry, ok := checkedSurfaceDispositions[key]
		if !ok {
			entry = SurfaceDisposition{
				File: surface.File, Kind: surface.Kind, Name: surface.Name,
				Classification: "unreviewed", Basis: "REVIEW REQUIRED",
			}
		}
		entries = append(entries, entry)
	}
	var output bytes.Buffer
	output.WriteString("[\n")
	for index, entry := range entries {
		data, err := json.Marshal(entry)
		if err != nil {
			return nil, fmt.Errorf("encode entity-ID surface disposition candidate %d: %w", index, err)
		}
		output.WriteString("  ")
		output.Write(data)
		if index+1 < len(entries) {
			output.WriteByte(',')
		}
		output.WriteByte('\n')
	}
	output.WriteString("]\n")
	return output.Bytes(), nil
}

package entityidaudit

import (
	"bytes"
	"encoding/json"
	"fmt"
	"sort"
)

// Count is one stable name/count entry in a corpus report summary.
type Count struct {
	Name  string `json:"name"`
	Count int    `json:"count"`
}

// Summary carries deterministic aggregate counts for a corpus report.
type Summary struct {
	CandidateCount         int     `json:"candidate_count"`
	FindingCount           int     `json:"finding_count"`
	SurfaceGroupCount      int     `json:"surface_group_count"`
	SurfaceOccurrenceCount int     `json:"surface_occurrence_count"`
	CandidatesByLanguage   []Count `json:"candidates_by_language"`
	FindingsByLanguage     []Count `json:"findings_by_language"`
	FindingsByReason       []Count `json:"findings_by_reason"`
	SurfacesByKind         []Count `json:"surfaces_by_kind"`
}

// ValueCorpus separates concrete values and their required changes from the
// implementation-surface inventory.
type ValueCorpus struct {
	Candidates []Candidate `json:"candidates"`
}

// Report is the checked-in machine-readable local entity-ID source corpus.
// Findings are also the exact source/configuration change list still required.
type Report struct {
	Version          int              `json:"version"`
	Roots            []string         `json:"roots"`
	SourceSet        string           `json:"source_set"`
	Summary          Summary          `json:"summary"`
	SurfaceRules     []SurfaceRule    `json:"surface_rules"`
	ValueCorpus      ValueCorpus      `json:"value_corpus"`
	SurfaceInventory []AuditedSurface `json:"surface_inventory"`
}

// BuildReport constructs a stable versioned report from an Audit result.
func BuildReport(roots []string, sourceSet string, result Result) Report {
	roots = append([]string(nil), roots...)
	sort.Strings(roots)
	if result.Candidates == nil {
		result.Candidates = []Candidate{}
	}
	if result.Findings == nil {
		result.Findings = []Finding{}
	}
	if result.Surfaces == nil {
		result.Surfaces = []AuditedSurface{}
	}
	return Report{
		Version:          1,
		Roots:            roots,
		SourceSet:        sourceSet,
		Summary:          summarize(result),
		SurfaceRules:     SurfaceRules(),
		ValueCorpus:      ValueCorpus{Candidates: result.Candidates},
		SurfaceInventory: result.Surfaces,
	}
}

// MarshalReport serializes a report in the canonical checked-in form.
func MarshalReport(report Report) ([]byte, error) {
	if err := validateReviewedReportSurfaces(report.SurfaceInventory); err != nil {
		return nil, err
	}
	var output bytes.Buffer
	writeJSON := func(value any) error {
		data, err := json.Marshal(value)
		if err != nil {
			return err
		}
		output.Write(data)
		return nil
	}
	output.WriteString("{\n  \"version\": ")
	if err := writeJSON(report.Version); err != nil {
		return nil, err
	}
	output.WriteString(",\n  \"roots\": ")
	if err := writeJSON(report.Roots); err != nil {
		return nil, err
	}
	output.WriteString(",\n  \"source_set\": ")
	if err := writeJSON(report.SourceSet); err != nil {
		return nil, err
	}
	output.WriteString(",\n  \"summary\": ")
	if err := writeJSON(report.Summary); err != nil {
		return nil, err
	}
	output.WriteString(",\n  \"surface_rules\": [\n")
	if err := writeCompactEntries(&output, report.SurfaceRules); err != nil {
		return nil, err
	}
	output.WriteString("  ],\n  \"value_corpus\": {\n    \"candidates\": [\n")
	if err := writeCompactEntries(&output, report.ValueCorpus.Candidates); err != nil {
		return nil, err
	}
	output.WriteString("    ]\n  },\n  \"surface_inventory\": [\n")
	if err := writeCompactEntries(&output, report.SurfaceInventory); err != nil {
		return nil, err
	}
	output.WriteString("  ]\n}\n")
	return output.Bytes(), nil
}

func writeCompactEntries[T any](output *bytes.Buffer, entries []T) error {
	for index, entry := range entries {
		data, err := json.Marshal(entry)
		if err != nil {
			return fmt.Errorf("marshal report entry %d: %w", index, err)
		}
		output.WriteString("    ")
		output.Write(data)
		if index+1 < len(entries) {
			output.WriteByte(',')
		}
		output.WriteByte('\n')
	}
	return nil
}

func summarize(result Result) Summary {
	candidateLanguages := make(map[string]int)
	findingLanguages := make(map[string]int)
	findingReasons := make(map[string]int)
	surfaceKinds := make(map[string]int)
	surfaceOccurrences := 0
	for _, candidate := range result.Candidates {
		candidateLanguages[string(candidate.Language)]++
	}
	for _, finding := range result.Findings {
		findingLanguages[string(finding.Language)]++
		findingReasons[finding.Reason]++
	}
	for _, surface := range result.Surfaces {
		surfaceKinds[surface.Kind]++
		surfaceOccurrences += len(surface.Locations)
	}
	return Summary{
		CandidateCount:         len(result.Candidates),
		FindingCount:           len(result.Findings),
		SurfaceGroupCount:      len(result.Surfaces),
		SurfaceOccurrenceCount: surfaceOccurrences,
		CandidatesByLanguage:   counts(candidateLanguages),
		FindingsByLanguage:     counts(findingLanguages),
		FindingsByReason:       counts(findingReasons),
		SurfacesByKind:         counts(surfaceKinds),
	}
}

func counts(values map[string]int) []Count {
	names := make([]string, 0, len(values))
	for name := range values {
		names = append(names, name)
	}
	sort.Strings(names)
	out := make([]Count, 0, len(names))
	for _, name := range names {
		out = append(out, Count{Name: name, Count: values[name]})
	}
	return out
}

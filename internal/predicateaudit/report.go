package predicateaudit

import (
	"encoding/json"
	"sort"
)

// PredicateAuditReportVersion is the machine-report schema version.
const PredicateAuditReportVersion = 1

// ReportCounts summarizes every report collection.
type ReportCounts struct {
	Candidates      int `json:"candidates"`
	Classifications int `json:"classifications"`
	Findings        int `json:"findings"`
}

// Classification records one accepted exact unrelated disposition.
type Classification struct {
	File    string `json:"file"`
	Line    int    `json:"line"`
	Column  int    `json:"column"`
	Surface string `json:"surface"`
	Value   string `json:"value"`
	Basis   string `json:"basis"`
}

// Report is the deterministic production predicate-audit report.
type Report struct {
	Version         int              `json:"version"`
	Roots           []string         `json:"roots"`
	Counts          ReportCounts     `json:"counts"`
	Candidates      []Candidate      `json:"candidates"`
	Classifications []Classification `json:"classifications"`
	Findings        []Finding        `json:"findings"`
}

// BuildReport constructs a sorted, versioned report.
func BuildReport(roots []string, candidates []Candidate, findings []Finding) Report {
	roots = canonicalRootLabels(roots)
	candidates = append([]Candidate(nil), candidates...)
	if candidates == nil {
		candidates = []Candidate{}
	}
	sort.Slice(candidates, func(i, j int) bool {
		return candidateLess(candidates[i], candidates[j])
	})
	findings = append([]Finding(nil), findings...)
	if findings == nil {
		findings = []Finding{}
	}
	sortFindings(findings)
	classifications := make([]Classification, 0)
	for _, candidate := range candidates {
		if candidate.Status != CandidateStatusClassifiedUnrelated {
			continue
		}
		classifications = append(classifications, Classification{
			File: candidate.File, Line: candidate.Line, Column: candidate.Column,
			Surface: candidate.Surface, Value: candidate.Predicate, Basis: candidate.ClassificationBasis,
		})
	}
	return Report{
		Version: PredicateAuditReportVersion,
		Roots:   roots,
		Counts: ReportCounts{
			Candidates: len(candidates), Classifications: len(classifications), Findings: len(findings),
		},
		Candidates: candidates, Classifications: classifications, Findings: findings,
	}
}

// MarshalReport serializes a report with stable field and entry ordering.
func MarshalReport(report Report) ([]byte, error) {
	return json.MarshalIndent(report, "", "  ")
}

func candidateLess(left, right Candidate) bool {
	if left.File != right.File {
		return left.File < right.File
	}
	if left.Line != right.Line {
		return left.Line < right.Line
	}
	if left.Column != right.Column {
		return left.Column < right.Column
	}
	if left.Surface != right.Surface {
		return left.Surface < right.Surface
	}
	return left.Predicate < right.Predicate
}

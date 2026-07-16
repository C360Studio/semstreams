// Command entity-id-audit validates structurally identified entity-ID source candidates.
package main

import (
	"bytes"
	"flag"
	"fmt"
	"os"

	"github.com/c360studio/semstreams/internal/entityidaudit"
)

func main() {
	format := flag.String("format", "text", "output format: text or json")
	writeReport := flag.String("write-report", "", "write the canonical report to this path and exit")
	verifyReport := flag.String("verify-report", "", "verify this canonical report without requiring zero findings")
	writeDispositionCandidates := flag.String("write-surface-disposition-candidates", "", "write the exact current disposition review candidate and exit")
	includeUntracked := flag.Bool("include-untracked", false, "include untracked, non-ignored source files (never for the checked report)")
	flag.Parse()
	selectedWrites := 0
	for _, value := range []string{*writeReport, *verifyReport, *writeDispositionCandidates} {
		if value != "" {
			selectedWrites++
		}
	}
	if selectedWrites > 1 {
		fmt.Fprintln(os.Stderr, "entity ID audit: report and disposition output modes are mutually exclusive")
		os.Exit(2)
	}
	roots := flag.Args()
	if len(roots) == 0 {
		roots = []string{"."}
	}
	if len(roots) != 1 {
		fmt.Fprintln(os.Stderr, "entity ID audit: exactly one Git repository root is required")
		os.Exit(2)
	}
	if *includeUntracked && (*writeReport != "" || *verifyReport != "" || *writeDispositionCandidates != "") {
		fmt.Fprintln(os.Stderr, "entity ID audit: checked report generation/verification is tracked-source only")
		os.Exit(2)
	}

	var result entityidaudit.Result
	var err error
	if *writeDispositionCandidates != "" {
		result, err = entityidaudit.InventoryRepositoryFull(roots[0], false)
	} else {
		result, err = entityidaudit.AuditRepositoryFull(roots[0], *includeUntracked)
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, "entity ID audit:", err)
		os.Exit(2)
	}
	candidates, findings := result.Candidates, result.Findings
	if *writeDispositionCandidates != "" {
		data, err := entityidaudit.MarshalSurfaceDispositionCandidates(result.Surfaces)
		if err != nil {
			fmt.Fprintln(os.Stderr, "entity ID audit:", err)
			os.Exit(2)
		}
		if err := os.WriteFile(*writeDispositionCandidates, data, 0o644); err != nil {
			fmt.Fprintln(os.Stderr, "entity ID audit: write disposition candidates:", err)
			os.Exit(2)
		}
		fmt.Printf("entity ID surface disposition candidates written: %s (%d groups)\n", *writeDispositionCandidates, len(result.Surfaces))
		return
	}
	sourceSet := "tracked"
	if *includeUntracked {
		sourceSet = "tracked+untracked-nonignored"
	}
	report := entityidaudit.BuildReport(roots, sourceSet, result)
	reportData, err := entityidaudit.MarshalReport(report)
	if err != nil {
		fmt.Fprintln(os.Stderr, "entity ID audit: encode report:", err)
		os.Exit(2)
	}
	if *writeReport != "" {
		if err := os.WriteFile(*writeReport, reportData, 0o644); err != nil {
			fmt.Fprintln(os.Stderr, "entity ID audit: write report:", err)
			os.Exit(2)
		}
		fmt.Printf("entity ID report written: %s (%d candidates, %d findings, %d surfaces)\n", *writeReport, len(candidates), len(findings), len(result.Surfaces))
		return
	}
	if *verifyReport != "" {
		committed, err := os.ReadFile(*verifyReport)
		if err != nil {
			fmt.Fprintln(os.Stderr, "entity ID audit: read report:", err)
			os.Exit(2)
		}
		if !bytes.Equal(committed, reportData) {
			fmt.Fprintf(os.Stderr, "entity ID audit: report drift: regenerate %s with --write-report\n", *verifyReport)
			os.Exit(1)
		}
		fmt.Printf("entity ID report verified: %s (%d candidates, %d findings, %d surfaces)\n", *verifyReport, len(candidates), len(findings), len(result.Surfaces))
		return
	}
	switch *format {
	case "text":
		for _, finding := range findings {
			fmt.Fprintf(os.Stderr, "%s:%d: %s %s: %q: %s\n",
				finding.File, finding.Line, finding.Language, finding.Surface, finding.Value, finding.Reason)
		}
		if len(findings) == 0 {
			fmt.Printf("entity ID audit passed: %d structured candidates and %d audited surfaces across %d roots\n", len(candidates), len(result.Surfaces), len(roots))
		} else {
			fmt.Fprintf(os.Stderr, "entity ID audit failed: %d invalid or unclassified candidates (%d extracted)\n",
				len(findings), len(candidates))
		}
	case "json":
		if _, err := os.Stdout.Write(reportData); err != nil {
			fmt.Fprintln(os.Stderr, "entity ID audit: write report output:", err)
			os.Exit(2)
		}
	default:
		fmt.Fprintln(os.Stderr, "entity ID audit: --format must be text or json")
		os.Exit(2)
	}
	if len(findings) > 0 {
		os.Exit(1)
	}
}

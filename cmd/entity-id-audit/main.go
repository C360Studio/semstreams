// Command entity-id-audit runs a bounded fixture-hygiene lint over statically identifiable
// entity-ID-shaped source candidates. It does not prove implementation-surface coverage or enforcement.
package main

import (
	"flag"
	"fmt"
	"os"

	"github.com/c360studio/semstreams/internal/entityidaudit"
)

func main() {
	format := flag.String("format", "text", "output format: text or json")
	includeUntracked := flag.Bool("include-untracked", false, "include untracked, non-ignored source files")
	flag.Parse()
	roots := flag.Args()
	if len(roots) == 0 {
		roots = []string{"."}
	}
	if len(roots) != 1 {
		fmt.Fprintln(os.Stderr, "entity ID audit: exactly one Git repository root is required")
		os.Exit(2)
	}
	result, err := entityidaudit.AuditRepositoryFull(roots[0], *includeUntracked)
	if err != nil {
		fmt.Fprintln(os.Stderr, "entity ID audit:", err)
		os.Exit(2)
	}
	candidates, findings := result.Candidates, result.Findings
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
	switch *format {
	case "text":
		for _, finding := range findings {
			fmt.Fprintf(os.Stderr, "%s:%d: %s %s: %q: %s\n",
				finding.File, finding.Line, finding.Language, finding.Surface, finding.Value, finding.Reason)
		}
		if len(findings) == 0 {
			fmt.Printf("entity ID audit passed: %d structured candidates across %d roots\n", len(candidates), len(roots))
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

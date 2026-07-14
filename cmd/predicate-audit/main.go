// Command predicate-audit validates structured predicate candidates in owned repositories.
package main

import (
	"fmt"
	"os"

	"github.com/c360studio/semstreams/internal/predicateaudit"
)

func main() {
	roots := os.Args[1:]
	if len(roots) == 0 {
		roots = []string{"."}
	}
	candidates, findings, err := predicateaudit.Audit(roots...)
	if err != nil {
		fmt.Fprintln(os.Stderr, "predicate audit:", err)
		os.Exit(2)
	}
	for _, finding := range findings {
		fmt.Fprintf(os.Stderr, "%s:%d: %s: %q: %s\n",
			finding.File, finding.Line, finding.Surface, finding.Predicate, finding.Reason)
	}
	if len(findings) > 0 {
		fmt.Fprintf(os.Stderr, "predicate audit failed: %d invalid or unclassified candidates (%d extracted)\n", len(findings), len(candidates))
		os.Exit(1)
	}
	fmt.Printf("predicate audit passed: %d structured candidates across %d roots\n", len(candidates), len(roots))
}

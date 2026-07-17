// Command predicate-test-audit validates the complementary predicate corpus
// in Go tests and structured testdata without changing the production audit.
package main

import (
	"flag"
	"fmt"
	"os"

	"github.com/c360studio/semstreams/internal/predicateaudit"
)

func main() {
	manifest := flag.String(
		"manifest",
		"internal/predicateaudit/test_fixture_invalids.json",
		"checked exact classifications for commentless structured testdata",
	)
	flag.Parse()
	roots := flag.Args()
	if len(roots) == 0 {
		roots = []string{"."}
	}

	result, err := predicateaudit.AuditTestFixtures(*manifest, roots...)
	if err != nil {
		fmt.Fprintln(os.Stderr, "predicate test-fixture audit:", err)
		os.Exit(2)
	}
	for _, finding := range result.Findings {
		location := finding.Location
		if location == "" && finding.Line > 0 {
			location = fmt.Sprintf("line:%d", finding.Line)
		}
		if finding.Document > 0 {
			location = fmt.Sprintf(
				"%s [document=%d record=%d occurrence=%d]",
				location,
				finding.Document,
				finding.Record,
				finding.Occurrence,
			)
		}
		fmt.Fprintf(
			os.Stderr,
			"%s:%s: %s: %q: %s\n",
			finding.File,
			location,
			finding.Code,
			finding.Predicate,
			finding.Message,
		)
	}
	if len(result.Findings) > 0 {
		fmt.Fprintf(
			os.Stderr,
			"predicate test-fixture audit failed: %d findings (%d candidates, %d exact classifications)\n",
			len(result.Findings),
			len(result.Candidates),
			result.Classifications,
		)
		os.Exit(1)
	}
	fmt.Printf(
		"predicate test-fixture audit passed: %d candidates, %d exact classifications across %d roots\n",
		len(result.Candidates),
		result.Classifications,
		len(roots),
	)
}

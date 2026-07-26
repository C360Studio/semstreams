// Command predicate-audit validates structured predicate candidates in owned repositories.
package main

import (
	"flag"
	"fmt"
	"io"
	"os"

	"github.com/c360studio/semstreams/internal/predicateaudit"
)

func main() {
	os.Exit(run(os.Args[1:], os.Stdout, os.Stderr))
}

func run(args []string, stdout, stderr io.Writer) int {
	return runWithEncoder(args, stdout, stderr, predicateaudit.MarshalReport)
}

func runWithEncoder(
	args []string,
	stdout io.Writer,
	stderr io.Writer,
	encode func(predicateaudit.Report) ([]byte, error),
) int {
	flags := flag.NewFlagSet("predicate-audit", flag.ContinueOnError)
	flags.SetOutput(stderr)
	format := flags.String("format", "text", "output format: text or json")
	if err := flags.Parse(args); err != nil {
		return 2
	}
	if *format != "text" && *format != "json" {
		fmt.Fprintln(stderr, "predicate audit: --format must be text or json")
		return 2
	}
	roots := flags.Args()
	if len(roots) == 0 {
		roots = []string{"."}
	}
	candidates, findings, err := predicateaudit.Audit(roots...)
	if err != nil {
		fmt.Fprintln(stderr, "predicate audit:", err)
		return 2
	}
	switch *format {
	case "text":
		for _, finding := range findings {
			if _, err := fmt.Fprintf(stderr, "%s:%d: %s: %q: %s\n",
				finding.File, finding.Line, finding.Surface, finding.Predicate, finding.Reason); err != nil {
				fmt.Fprintln(stderr, "predicate audit: write text output:", err)
				return 2
			}
		}
		if len(findings) > 0 {
			if _, err := fmt.Fprintf(
				stderr,
				"predicate audit failed: %d invalid or unclassified candidates (%d extracted)\n",
				len(findings),
				len(candidates),
			); err != nil {
				return 2
			}
		} else if _, err := fmt.Fprintf(
			stdout,
			"predicate audit passed: %d structured candidates across %d roots\n",
			len(candidates),
			len(roots),
		); err != nil {
			fmt.Fprintln(stderr, "predicate audit: write text output:", err)
			return 2
		}
	case "json":
		reportData, err := encode(predicateaudit.BuildReport(roots, candidates, findings))
		if err != nil {
			fmt.Fprintln(stderr, "predicate audit: encode report:", err)
			return 2
		}
		reportData = append(reportData, '\n')
		if _, err := stdout.Write(reportData); err != nil {
			fmt.Fprintln(stderr, "predicate audit: write report output:", err)
			return 2
		}
	}
	if len(findings) > 0 {
		return 1
	}
	return 0
}

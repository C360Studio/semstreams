// Command measure-injection-classifier runs the Phase 2 ADR-043
// measurement harness: load one or more JSONL corpora, classify
// each record against the loaded corpus, and emit a markdown table
// of precision / recall per signal bucket plus latency stats.
//
// Two modes:
//
//   - --self-eval: classify the corpus against itself. This is the
//     smoke mode (`task adr043:measure`) and produces the noise
//     floor for the vendored seed. Useful as a regression target
//     for the loader and classifier.
//
//   - --eval-corpus FILE: classify FILE against the configured
//     --corpus. This is the production mode operators use with
//     their own holdout eval set and benign-OSINT slice. The
//     classifier corpus and the eval corpus are independent.
//
// The harness does not call any LLM. Phase 2's T2-vs-T3-only
// comparison protocol is documented in
// docs/operations/18-injection-classifier-measurements.md and run
// by operators with their own LLM endpoint configured.
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"slices"
	"sort"
	"time"

	"github.com/c360studio/semstreams/graph/query"
	injectioncorpus "github.com/c360studio/semstreams/processor/agentic-governance/injection_corpus"
)

// flags collects all CLI options in one place for the help message.
type flags struct {
	corpus     stringList
	evalCorpus string
	threshold  float64
	selfEval   bool
	outPath    string
}

// stringList is a flag.Value that appends each --flag occurrence
// rather than overwriting. Lets operators pass --corpus a.jsonl
// --corpus b.jsonl for aggregation.
type stringList []string

func (s *stringList) String() string {
	if s == nil {
		return ""
	}
	return fmt.Sprintf("%v", []string(*s))
}

func (s *stringList) Set(v string) error {
	*s = append(*s, v)
	return nil
}

func main() {
	var f flags
	flag.Var(&f.corpus, "corpus", "JSONL corpus file for the classifier (repeatable)")
	flag.StringVar(&f.evalCorpus, "eval-corpus", "", "JSONL holdout eval file; mutually exclusive with --self-eval")
	flag.Float64Var(&f.threshold, "threshold", 0.30, "cosine similarity threshold (matches Phase 2 default for the small seed)")
	flag.BoolVar(&f.selfEval, "self-eval", false, "classify the corpus against itself (smoke mode)")
	flag.StringVar(&f.outPath, "out", "", "write report to this path (default: stdout)")
	flag.Parse()

	if err := run(f); err != nil {
		fmt.Fprintf(os.Stderr, "measure-injection-classifier: %v\n", err)
		os.Exit(1)
	}
}

func run(f flags) error {
	if len(f.corpus) == 0 {
		return errors.New("at least one --corpus is required")
	}
	if !f.selfEval && f.evalCorpus == "" {
		return errors.New("either --self-eval or --eval-corpus FILE is required")
	}
	if f.selfEval && f.evalCorpus != "" {
		return errors.New("--self-eval and --eval-corpus are mutually exclusive")
	}

	classifierSources := make([]injectioncorpus.Source, 0, len(f.corpus))
	for i, path := range f.corpus {
		classifierSources = append(classifierSources, injectioncorpus.Source{
			Domain:  fmt.Sprintf("corpus-%d", i),
			Version: "measure",
			Path:    path,
		})
	}

	classifierDomains, err := injectioncorpus.Load(classifierSources)
	if err != nil {
		return fmt.Errorf("load classifier corpus: %w", err)
	}

	classifier := query.NewEmbeddingClassifier(classifierDomains, f.threshold)

	var evalRecords []injectioncorpus.Record
	if f.selfEval {
		evalRecords, err = readRecords(f.corpus)
		if err != nil {
			return fmt.Errorf("read self-eval records: %w", err)
		}
	} else {
		evalRecords, err = readRecords([]string{f.evalCorpus})
		if err != nil {
			return fmt.Errorf("read eval records: %w", err)
		}
	}

	report := measure(classifier, evalRecords, f.threshold)

	out := os.Stdout
	if f.outPath != "" {
		fh, err := os.Create(f.outPath)
		if err != nil {
			return fmt.Errorf("open out path: %w", err)
		}
		defer func() { _ = fh.Close() }()
		out = fh
	}

	return writeReport(out, report)
}

// report aggregates per-signal precision/recall plus latency
// percentiles into a structure suitable for markdown rendering.
type report struct {
	threshold     float64
	classifierN   int
	evalN         int
	perSignal     map[string]signalStats
	latencyP50    time.Duration
	latencyP99    time.Duration
	overallExact  int
	overallTotal  int
	matchAttempts int
}

// signalStats holds the counts needed for precision/recall on one
// bucket. tp = true positive (predicted == truth), fp = false
// positive (predicted bucket, truth was different), fn = false
// negative (truth was this bucket, predicted something else).
type signalStats struct {
	tp int
	fp int
	fn int
}

func (s signalStats) precision() float64 {
	denom := s.tp + s.fp
	if denom == 0 {
		return 0
	}
	return float64(s.tp) / float64(denom)
}

func (s signalStats) recall() float64 {
	denom := s.tp + s.fn
	if denom == 0 {
		return 0
	}
	return float64(s.tp) / float64(denom)
}

// measure runs the classifier on each eval record and aggregates
// results. The classifier is invoked through the exact same
// FindBestMatch path the runtime filter uses, so the numbers
// reflect production behavior modulo threshold tuning.
func measure(classifier *query.EmbeddingClassifier, eval []injectioncorpus.Record, threshold float64) report {
	rpt := report{
		threshold: threshold,
		evalN:     len(eval),
		perSignal: make(map[string]signalStats),
	}

	latencies := make([]time.Duration, 0, len(eval))

	for _, rec := range eval {
		start := time.Now()
		match, score := classifier.FindBestMatch(context.Background(), rec.Text)
		latencies = append(latencies, time.Since(start))
		rpt.matchAttempts++

		// Below-threshold or no-match → predict "no_match" so
		// the confusion matrix has a coherent fallback bucket.
		predicted := "no_match"
		if match != nil && score >= threshold {
			predicted = match.Intent
		}

		s := rpt.perSignal[rec.Signal]
		if predicted == rec.Signal {
			s.tp++
			rpt.overallExact++
		} else {
			s.fn++
			// Charge the false positive to the predicted bucket.
			ps := rpt.perSignal[predicted]
			ps.fp++
			rpt.perSignal[predicted] = ps
		}
		rpt.perSignal[rec.Signal] = s
		rpt.overallTotal++
	}

	// Latency percentiles.
	slices.Sort(latencies)
	if len(latencies) > 0 {
		rpt.latencyP50 = latencies[len(latencies)/2]
		idxP99 := int(float64(len(latencies)) * 0.99)
		if idxP99 >= len(latencies) {
			idxP99 = len(latencies) - 1
		}
		rpt.latencyP99 = latencies[idxP99]
	}

	return rpt
}

// readRecords loads raw Records from one or more files using the
// same JSONL contract the classifier uses; bypasses the
// DomainExamples wrapping so we have access to the original signal
// label for ground-truth comparison.
func readRecords(paths []string) ([]injectioncorpus.Record, error) {
	sources := make([]injectioncorpus.Source, 0, len(paths))
	for i, p := range paths {
		sources = append(sources, injectioncorpus.Source{
			Domain:  fmt.Sprintf("eval-%d", i),
			Version: "measure",
			Path:    p,
		})
	}

	domains, err := injectioncorpus.Load(sources)
	if err != nil {
		return nil, err
	}

	var out []injectioncorpus.Record
	for _, d := range domains {
		for _, ex := range d.Examples {
			id, _ := ex.Options[injectioncorpus.OptionKeyID].(string)
			out = append(out, injectioncorpus.Record{
				ID:     id,
				Text:   ex.Query,
				Signal: ex.Intent,
			})
		}
	}

	return out, nil
}

func writeReport(w io.Writer, r report) error {
	if _, err := fmt.Fprintf(w, "# Injection Classifier Measurement\n\n"); err != nil {
		return err
	}
	fmt.Fprintf(w, "- Threshold: %.2f\n", r.threshold)
	fmt.Fprintf(w, "- Eval records: %d\n", r.evalN)
	fmt.Fprintf(w, "- Overall exact-match: %d / %d (%.1f%%)\n", r.overallExact, r.overallTotal, percent(r.overallExact, r.overallTotal))
	fmt.Fprintf(w, "- Latency p50: %s\n", r.latencyP50)
	fmt.Fprintf(w, "- Latency p99: %s\n\n", r.latencyP99)

	fmt.Fprintf(w, "## Per-signal precision / recall\n\n")
	fmt.Fprintf(w, "| Signal | TP | FP | FN | Precision | Recall |\n")
	fmt.Fprintf(w, "|---|---:|---:|---:|---:|---:|\n")

	// Stable ordering: signal buckets sorted alphabetically.
	signals := make([]string, 0, len(r.perSignal))
	for k := range r.perSignal {
		signals = append(signals, k)
	}
	sort.Strings(signals)
	for _, sig := range signals {
		s := r.perSignal[sig]
		fmt.Fprintf(w, "| `%s` | %d | %d | %d | %.2f | %.2f |\n",
			sig, s.tp, s.fp, s.fn, s.precision(), s.recall())
	}

	return nil
}

func percent(n, d int) float64 {
	if d == 0 {
		return 0
	}
	return 100 * float64(n) / float64(d)
}

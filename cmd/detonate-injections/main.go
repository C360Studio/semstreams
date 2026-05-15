// Command detonate-injections runs the ADR-043 Phase 3 detonator
// against a batch of unlabeled inputs and writes corpus records
// suitable for the Phase 2 classifier corpus loader.
//
// Input is JSONL with `{id, text}` records (one per line).
// Output is JSONL in the injectioncorpus.Record shape — drops
// directly into a Phase 2 corpus source entry.
//
// Idempotent across re-runs: if the output file already contains
// a record with an ID matching this run's input, the input is
// skipped. Operators interrupt mid-batch with SIGINT and resume by
// re-running with the same flags.
//
// LLM endpoint: operators bring their own via --endpoint-config
// pointing at a JSON file in the model.EndpointConfig shape. Cost
// caps are CLI-tunable (--max-turns, --max-tool-calls, --max-tokens,
// --timeout). Concurrency via --concurrency (default 2) — keep
// modest unless your LLM provider tolerates parallelism.
package main

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"os/signal"
	"sync"
	"syscall"
	"time"

	"github.com/c360studio/semstreams/model"
	agenticdetonator "github.com/c360studio/semstreams/processor/agentic-detonator"
	injectioncorpus "github.com/c360studio/semstreams/processor/agentic-governance/injection_corpus"
	agenticmodel "github.com/c360studio/semstreams/processor/agentic-model"
)

// InputRecord is the unlabeled-text JSONL shape the CLI reads.
// Distinct from injectioncorpus.Record (which requires Signal) so
// the unlabeled→labeled flow is type-checked.
//
// Intentionally minimal: only Text is required. The output record's
// ID is sha256(text) per the Detonation.ID contract; operator-
// supplied IDs would be silently overwritten and were dropped from
// the input shape to remove that footgun (Phase 3c review S2-1).
type InputRecord struct {
	Text string `json:"text"`
}

type flags struct {
	inputPath        string
	outputPath       string
	endpointConfig   string
	systemPromptFile string
	maxTurns         int
	maxToolCalls     int
	maxTokens        int
	timeout          time.Duration
	concurrency      int
	limit            int
	verbose          bool
}

func main() {
	var f flags
	flag.StringVar(&f.inputPath, "input", "", "JSONL input file ({text} per line); '-' for stdin")
	flag.StringVar(&f.outputPath, "output", "", "JSONL output file (appended; idempotent across re-runs)")
	flag.StringVar(&f.endpointConfig, "endpoint-config", "", "JSON file with model.EndpointConfig (the canary model)")
	flag.StringVar(&f.systemPromptFile, "system-prompt-file", "", "optional file with custom canary persona (Phase 3b review #4)")
	flag.IntVar(&f.maxTurns, "max-turns", 6, "per-detonation outer loop bound")
	flag.IntVar(&f.maxToolCalls, "max-tool-calls", 12, "per-detonation cumulative tool-call cap")
	flag.IntVar(&f.maxTokens, "max-tokens", 1024, "per-turn output token budget")
	flag.DurationVar(&f.timeout, "timeout", 60*time.Second, "per-detonation total deadline")
	flag.IntVar(&f.concurrency, "concurrency", 2, "number of parallel canary workers")
	flag.IntVar(&f.limit, "limit", 0, "process at most N inputs (0 = no limit)")
	flag.BoolVar(&f.verbose, "verbose", false, "log per-detonation progress")
	flag.Parse()

	if err := run(f); err != nil {
		fmt.Fprintf(os.Stderr, "detonate-injections: %v\n", err)
		os.Exit(1)
	}
}

func run(f flags) error {
	if f.inputPath == "" {
		return errors.New("--input is required")
	}
	if f.outputPath == "" {
		return errors.New("--output is required")
	}
	if f.endpointConfig == "" {
		return errors.New("--endpoint-config is required")
	}
	if f.concurrency < 1 {
		return errors.New("--concurrency must be at least 1")
	}

	endpoint, err := loadEndpoint(f.endpointConfig)
	if err != nil {
		return fmt.Errorf("load endpoint: %w", err)
	}

	systemPrompt := ""
	if f.systemPromptFile != "" {
		raw, err := os.ReadFile(f.systemPromptFile)
		if err != nil {
			return fmt.Errorf("read system prompt: %w", err)
		}
		systemPrompt = string(raw)
	}

	client, err := agenticmodel.NewClient(endpoint)
	if err != nil {
		return fmt.Errorf("build model client: %w", err)
	}

	canaryCfg := agenticdetonator.CanaryConfig{
		Model:        endpoint.Model,
		MaxTurns:     f.maxTurns,
		MaxToolCalls: f.maxToolCalls,
		MaxTokens:    f.maxTokens,
		Timeout:      f.timeout,
		SystemPrompt: systemPrompt,
	}

	// Construct the canary once. NewCanary failure is per-process
	// (config-driven), not per-input — fail loud here rather than
	// log the same error N times once per worker. Canary is
	// concurrent-safe per its doc contract.
	canary, err := agenticdetonator.NewCanary(client, canaryCfg)
	if err != nil {
		return fmt.Errorf("build canary: %w", err)
	}

	inputs, err := readInputs(f.inputPath)
	if err != nil {
		return fmt.Errorf("read inputs: %w", err)
	}

	existing, err := loadExistingOutputIDs(f.outputPath)
	if err != nil {
		return fmt.Errorf("load existing output: %w", err)
	}

	out, err := os.OpenFile(f.outputPath, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
	if err != nil {
		return fmt.Errorf("open output: %w", err)
	}
	defer func() { _ = out.Close() }()

	ctx, cancel := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer cancel()

	return processBatch(ctx, f, canary, inputs, existing, out)
}

// silence the agenticmodel import in tests-only builds where the
// CLI's main entry never runs. The import remains needed for run()
// in the production path.
var _ agenticdetonator.ModelInvoker = (*agenticmodel.Client)(nil)

// loadEndpoint parses an EndpointConfig from a JSON file. Operators
// configure the canary model via the same shape used everywhere
// else in semstreams — no new config schema.
func loadEndpoint(path string) (*model.EndpointConfig, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var cfg model.EndpointConfig
	if err := json.Unmarshal(raw, &cfg); err != nil {
		return nil, fmt.Errorf("parse endpoint json: %w", err)
	}
	if cfg.Model == "" {
		return nil, errors.New("endpoint config: model required")
	}
	if cfg.Provider == "" {
		return nil, errors.New("endpoint config: provider required")
	}
	return &cfg, nil
}

// readInputs streams the JSONL input into a slice. We materialise
// rather than stream-pipeline so the worker pool can fan out
// idempotency-aware work without coordinating reader lifecycle.
// Batch sizes are bounded by operator memory; in practice the
// limit is the LLM-cost budget, not Go RAM.
func readInputs(path string) ([]InputRecord, error) {
	var rdr io.Reader
	if path == "-" {
		rdr = os.Stdin
	} else {
		fh, err := os.Open(path)
		if err != nil {
			return nil, err
		}
		defer func() { _ = fh.Close() }()
		rdr = fh
	}

	var out []InputRecord
	sc := bufio.NewScanner(rdr)
	sc.Buffer(make([]byte, 0, 64*1024), 1<<20)
	line := 0
	for sc.Scan() {
		line++
		raw := sc.Bytes()
		if len(raw) == 0 || raw[0] == '#' {
			continue
		}
		var rec InputRecord
		if err := json.Unmarshal(raw, &rec); err != nil {
			return nil, fmt.Errorf("line %d: %w", line, err)
		}
		if rec.Text == "" {
			return nil, fmt.Errorf("line %d: text empty", line)
		}
		out = append(out, rec)
	}
	return out, sc.Err()
}

// loadExistingOutputIDs scans the output file (if it exists) and
// returns the set of record IDs already written. Used to skip
// already-labeled inputs on re-run. Missing file is not an error —
// fresh run.
func loadExistingOutputIDs(path string) (map[string]struct{}, error) {
	seen := make(map[string]struct{})
	fh, err := os.Open(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return seen, nil
		}
		return nil, err
	}
	defer func() { _ = fh.Close() }()

	sc := bufio.NewScanner(fh)
	sc.Buffer(make([]byte, 0, 64*1024), 1<<20)
	for sc.Scan() {
		raw := sc.Bytes()
		if len(raw) == 0 {
			continue
		}
		var rec injectioncorpus.Record
		if err := json.Unmarshal(raw, &rec); err != nil {
			// A malformed line in our own output is operator-
			// surprising — could be a previous CLI crash mid-write.
			// We can't safely recover by skipping (might re-detonate
			// inputs that already cost money). Surface clearly.
			return nil, fmt.Errorf("malformed existing output: %w (delete or repair %s before resuming)", err, path)
		}
		if rec.ID != "" {
			seen[rec.ID] = struct{}{}
		}
	}
	return seen, sc.Err()
}

// processBatch runs the worker pool and writes outputs. The single
// writer goroutine owns the output file handle so we don't need a
// mutex around the JSONL append.
//
// The canary is constructed in run() and passed in; it's
// concurrent-safe by contract so all workers share one instance.
// Tests construct a canary backed by a scriptedInvoker mock.
//
// existing is a startup snapshot of already-labeled IDs. The
// producer goroutine ALSO writes into it (immediately on enqueue)
// to dedup duplicate inputs within a single batch — paying for the
// same content twice in one run would be operator-hostile, and the
// corpus loader rejects duplicate IDs at load anyway. Worker and
// writer goroutines never touch existing; producer is the only
// runtime writer, so no synchronization needed.
func processBatch(
	ctx context.Context,
	f flags,
	canary *agenticdetonator.Canary,
	inputs []InputRecord,
	existing map[string]struct{},
	out io.Writer,
) error {
	work := make(chan workItem)
	results := make(chan injectioncorpus.Record)
	failures := make(chan workFailure)

	var wg sync.WaitGroup
	for i := 0; i < f.concurrency; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for item := range work {
				if ctx.Err() != nil {
					return
				}
				det, derr := canary.Detonate(ctx, item.text)
				if derr != nil {
					failures <- workFailure{text: item.text, err: derr, det: det}
					continue
				}
				results <- det.ToRecord()
			}
		}()
	}

	// Producer + in-batch dedup. existing is read-only by workers
	// and the result loop; the producer is the only writer here.
	go func() {
		defer close(work)
		processed := 0
		for _, rec := range inputs {
			if f.limit > 0 && processed >= f.limit {
				return
			}
			id := computeID(rec.Text)
			if _, dup := existing[id]; dup {
				if f.verbose {
					fmt.Fprintf(os.Stderr, "skip already-labeled: %s\n", id)
				}
				continue
			}
			// Mark as in-flight so a later duplicate input in the
			// same batch is skipped rather than paid for twice.
			existing[id] = struct{}{}
			select {
			case <-ctx.Done():
				return
			case work <- workItem{text: rec.Text, id: id}:
				processed++
			}
		}
	}()

	// Result writer + failure logger. Closes channels when workers
	// finish.
	go func() {
		wg.Wait()
		close(results)
		close(failures)
	}()

	var processed, skipped int
	enc := json.NewEncoder(out)
	for results != nil || failures != nil {
		select {
		case rec, ok := <-results:
			if !ok {
				results = nil
				continue
			}
			if err := enc.Encode(&rec); err != nil {
				return fmt.Errorf("write record: %w", err)
			}
			processed++
			if f.verbose {
				fmt.Fprintf(os.Stderr, "labeled %s → %s\n", rec.ID[:12], rec.Signal)
			}
		case fail, ok := <-failures:
			if !ok {
				failures = nil
				continue
			}
			skipped++
			fmt.Fprintf(os.Stderr, "detonation failed: input=%s err=%v\n",
				truncate(fail.text, 60), fail.err)
		}
	}

	fmt.Fprintf(os.Stderr, "detonate-injections: processed=%d skipped=%d existing=%d\n",
		processed, skipped, len(existing))
	return nil
}

// workItem is the producer→worker payload. Carries the input text
// plus its precomputed content-hash ID so workers don't re-hash.
type workItem struct {
	text string
	id   string
}

// workFailure carries a per-input failure to the result writer.
// Partial Detonation is preserved (det may be non-nil) so a future
// CLI option can persist partial labels — Phase 3 default is to
// drop and let operators re-detonate.
type workFailure struct {
	text string
	err  error
	det  *agenticdetonator.Detonation
}

// computeID generates the detonation ID from the input text. Same
// sha256(input) shape as Detonation.ID so the output records line
// up regardless of whether the operator provided an ID upstream.
func computeID(text string) string {
	tmp := &agenticdetonator.Detonation{Input: text}
	return tmp.ID()
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "…"
}

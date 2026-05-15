// Package injectioncorpus loads labeled injection examples into the
// shape consumed by graph/query.EmbeddingClassifier.
//
// Phase 2 of ADR-043: one bootstrap source, JSONL on disk, normalized
// to []*query.DomainExamples on read. JSONL is chosen over single-JSON
// so the Phase 3 detonator can append labels without rewriting the
// file.
//
// One record per line:
//
//	{"id": "sha256-hex", "text": "...", "signal": "instruction-override", "source": "internal-seed-v0"}
//
// The `text` field becomes Example.Query (the substrate predates
// injection use; the field name is a query-router legacy). The
// `signal` field becomes Example.Intent — that is the surface rules
// match on via governance.injection.signal.
package injectioncorpus

import (
	"bufio"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/c360studio/semstreams/graph/query"
)

// OptionKey constants name the keys used inside Example.Options for
// corpus-loaded records. Phase 2b runtime-side accessors read these
// when building governance.injection.top_match_id triples; centralising
// the key names here keeps the contract typo-proof.
const (
	OptionKeyID     = "id"
	OptionKeySource = "source"
)

// Source describes one corpus file plus the domain label that will be
// attached to its examples. The domain label is metadata only — the
// classifier aggregates examples across domains.
type Source struct {
	// Domain is a free-form tag (e.g., "injection-internal-seed",
	// "injection-deepset"). Surfaces in DomainExamples.Domain.
	Domain string

	// Version is the corpus revision tag (e.g., "v0.1").
	Version string

	// Path is the JSONL file to load.
	Path string
}

// Record is the on-disk JSONL shape. Kept in this package rather than
// graph/query so the corpus format can evolve without touching the
// query-router substrate.
type Record struct {
	// ID is a stable identifier for the record. Recommended: hex
	// sha256 of the text. Becomes governance.injection.top_match_id
	// when this record is the nearest neighbor.
	ID string `json:"id"`

	// Text is the labeled input (the injection attempt, or a
	// benign counter-example).
	Text string `json:"text"`

	// Signal is the bucket the text belongs to (one of the values
	// enumerated in ADR-043 line 206). The classifier emits this
	// as governance.injection.signal on match.
	Signal string `json:"signal"`

	// Source identifies the origin of the record (e.g.,
	// "internal-seed-v0", "deepset/prompt-injections@<sha>").
	// Persisted for provenance; not used in classification.
	Source string `json:"source,omitempty"`
}

// Load reads one or more JSONL corpus files and returns the result as
// the []*query.DomainExamples shape the classifier consumes.
//
// Aggregates errors across files via errors.Join so a misconfigured
// deployment sees every problem on a single boot. Also detects
// duplicate record IDs across sources — Phase 3 detonator workers and
// vendored public corpora overlapping the internal seed are realistic
// collision paths, and silent dedup hides which record actually
// became the nearest-neighbor.
func Load(sources []Source) ([]*query.DomainExamples, error) {
	if len(sources) == 0 {
		return nil, fmt.Errorf("no corpus sources configured")
	}

	out := make([]*query.DomainExamples, 0, len(sources))
	var errs []error
	// firstSeen maps record ID → "<domain>:<path>" for the first
	// source that introduced it. Used to surface duplicates with a
	// pointer to the prior occurrence.
	firstSeen := make(map[string]string)

	for _, src := range sources {
		domain, err := loadOne(src)
		if err != nil {
			errs = append(errs, fmt.Errorf("source %q (%s): %w", src.Domain, src.Path, err))
			continue
		}

		origin := src.Domain + ":" + src.Path
		for _, ex := range domain.Examples {
			id, _ := ex.Options[OptionKeyID].(string)
			if id == "" {
				continue
			}
			if prior, ok := firstSeen[id]; ok && prior != origin {
				errs = append(errs, fmt.Errorf("duplicate record id %q across sources: first seen in %s, also in %s", id, prior, origin))
				continue
			}
			firstSeen[id] = origin
		}
		out = append(out, domain)
	}

	if len(errs) > 0 {
		return nil, errors.Join(errs...)
	}

	return out, nil
}

// loadOne reads a single JSONL file into one DomainExamples.
func loadOne(src Source) (*query.DomainExamples, error) {
	if src.Path == "" {
		return nil, fmt.Errorf("source path empty")
	}
	if src.Domain == "" {
		return nil, fmt.Errorf("source domain empty")
	}

	f, err := os.Open(src.Path)
	if err != nil {
		return nil, fmt.Errorf("open: %w", err)
	}
	defer func() { _ = f.Close() }()

	examples, err := parseJSONL(f)
	if err != nil {
		return nil, err
	}

	if len(examples) == 0 {
		return nil, fmt.Errorf("corpus contained no usable records")
	}

	return &query.DomainExamples{
		Domain:   src.Domain,
		Version:  src.Version,
		Examples: examples,
	}, nil
}

// parseJSONL parses one JSON record per line, skipping blank lines
// and lines starting with '#' (so corpus authors can embed inline
// comments). Returns parse errors with the offending line number.
// Detects duplicate IDs within a single source — Phase 3 detonator
// writer bugs are the realistic failure mode and silent dedup hides
// which record became the nearest neighbor.
func parseJSONL(r io.Reader) ([]query.Example, error) {
	scanner := bufio.NewScanner(r)
	// Allow long lines — some injections are paragraph-scale.
	// 1 MiB is large enough for plausible attack inputs; oversized
	// lines surface as a clear scan error rather than a silent
	// truncation.
	const maxLine = 1 << 20 // 1 MiB
	scanner.Buffer(make([]byte, 0, 64*1024), maxLine)

	var examples []query.Example
	// seenLine maps id → line number it was first observed at so
	// duplicates surface with both line numbers in the error.
	seenLine := make(map[string]int)
	lineNum := 0
	for scanner.Scan() {
		lineNum++
		raw := strings.TrimSpace(scanner.Text())
		if raw == "" || strings.HasPrefix(raw, "#") {
			continue
		}

		var rec Record
		if err := json.Unmarshal([]byte(raw), &rec); err != nil {
			return nil, fmt.Errorf("line %d: %w", lineNum, err)
		}

		if err := validateRecord(&rec, lineNum); err != nil {
			return nil, err
		}

		if prior, ok := seenLine[rec.ID]; ok {
			return nil, fmt.Errorf("line %d: duplicate id %q (first seen line %d)", lineNum, rec.ID, prior)
		}
		seenLine[rec.ID] = lineNum

		examples = append(examples, query.Example{
			Query:  rec.Text,
			Intent: rec.Signal,
			Options: map[string]any{
				OptionKeyID:     rec.ID,
				OptionKeySource: rec.Source,
			},
		})
	}

	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("scan: %w", err)
	}

	return examples, nil
}

// validateRecord enforces the minimal record contract — id, text,
// signal all non-empty. Provenance (source) is optional. We do not
// validate the signal against the ADR-043 enum here so that operators
// can add new buckets without code changes; the classifier surfaces
// whatever it loads.
//
// Normalizes Text by trimming surrounding whitespace at parse time so
// hand-authored corpus files don't embed accidental whitespace into
// embeddings.
func validateRecord(rec *Record, line int) error {
	if rec.ID == "" {
		return fmt.Errorf("line %d: id empty", line)
	}
	rec.Text = strings.TrimSpace(rec.Text)
	if rec.Text == "" {
		return fmt.Errorf("line %d: text empty", line)
	}
	if rec.Signal == "" {
		return fmt.Errorf("line %d: signal empty", line)
	}
	return nil
}

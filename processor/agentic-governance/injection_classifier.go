package agenticgovernance

import (
	"context"
	"fmt"
	"time"

	"github.com/c360studio/semstreams/graph/query"
	injectioncorpus "github.com/c360studio/semstreams/processor/agentic-governance/injection_corpus"
	"github.com/c360studio/semstreams/vocabulary/governance"
)

// signalUnknown is the metric-label fallback when a corpus record
// carries a signal bucket that isn't in the ADR-043 enum. Keeps
// Prometheus label cardinality bounded.
const signalUnknown = "unknown"

// knownSignals enumerates the ADR-043 signal-bucket vocabulary that
// the classifier may emit. Any corpus signal outside this set is
// reported as signalUnknown on the metric so a typoed bucket
// surfaces in dashboards rather than ballooning label cardinality.
var knownSignals = map[string]struct{}{
	"instruction-override": {},
	"network-egress":       {},
	"data-access":          {},
	"code-exec":            {},
	"filesystem-read":      {},
	"exfil-email":          {},
	"secret-access":        {},
	"cred-enum":            {},
	"benign":               {},
}

// InjectionClassifierFilter wraps a graph/query.EmbeddingClassifier
// with corpus-loaded injection examples. Sits as a peer filter
// alongside the regex-based InjectionFilter — the regex stays as the
// effective T0 (fast path for the obvious 5% of attacks) and this
// classifier is T1/T2 per ADR-043. ClassifierChain wrapping is
// deferred until Phase 4 when neural and multi-tenant land.
//
// Thread-safe: embeds the classifier whose FindBestMatch is
// concurrent-safe by design.
type InjectionClassifierFilter struct {
	classifier  *query.EmbeddingClassifier
	threshold   float64
	shadowMode  bool
	corpusCount int
	metrics     *governanceMetrics
}

// NewInjectionClassifierFilter loads the configured corpus and
// returns a runnable filter. Returns an error if the corpus cannot
// be loaded — operators should see boot-time failure rather than a
// silently-empty classifier that always passes.
//
// When metrics is nil the filter still works (test path), it just
// skips the Prom emission.
func NewInjectionClassifierFilter(cfg *InjectionClassifierConfig, metrics *governanceMetrics) (*InjectionClassifierFilter, error) {
	if cfg == nil {
		return nil, fmt.Errorf("classifier config is nil")
	}
	if len(cfg.CorpusSources) == 0 {
		return nil, fmt.Errorf("no corpus sources configured")
	}

	sources := make([]injectioncorpus.Source, 0, len(cfg.CorpusSources))
	for _, s := range cfg.CorpusSources {
		sources = append(sources, injectioncorpus.Source{
			Domain:  s.Domain,
			Version: s.Version,
			Path:    s.Path,
		})
	}

	domains, err := injectioncorpus.Load(sources)
	if err != nil {
		return nil, fmt.Errorf("load corpus: %w", err)
	}

	threshold := cfg.Threshold
	if threshold == 0 {
		// Conservative default if operator left zero. The
		// Phase 2 measurement pass tunes this per deployment.
		threshold = 0.70
	}

	classifier := query.NewEmbeddingClassifier(domains, threshold)

	// Count records for metadata exposed to operators / tests.
	total := 0
	for _, d := range domains {
		total += len(d.Examples)
	}

	return &InjectionClassifierFilter{
		classifier:  classifier,
		threshold:   threshold,
		shadowMode:  cfg.ShadowMode,
		corpusCount: total,
		metrics:     metrics,
	}, nil
}

// Name returns the filter identifier used in violation records and
// metric labels. Distinct from "injection_detection" (regex) so
// dashboards can attribute matches to the right tier.
func (f *InjectionClassifierFilter) Name() string {
	return "injection_classifier"
}

// CorpusSize returns the number of records loaded. Useful for boot
// logging and for tests that pin corpus shape.
func (f *InjectionClassifierFilter) CorpusSize() int {
	return f.corpusCount
}

// Process runs the embedding classifier on msg.Content.Text and
// returns a verdict.
//
// Three outcomes:
//   - No match (score below threshold OR signal is "benign"): allow.
//   - Match in shadow mode: allow, but attach a Violation with
//     action=Flagged so downstream observability sees the verdict
//     without blocking.
//   - Match in enforce mode: block, attach Violation with
//     action=Blocked.
//
// Violation Details carry the four governance.injection.* fields
// (signal, tier, score, top_match_id) which Phase 2c+ rule emission
// surfaces as triples per the vocabulary registered in this package.
func (f *InjectionClassifierFilter) Process(ctx context.Context, msg *Message) (*FilterResult, error) {
	if msg == nil {
		return nil, fmt.Errorf("message is nil")
	}
	text := msg.Content.Text
	if text == "" {
		return NewFilterResult(true).WithConfidence(1.0), nil
	}

	start := time.Now()
	match, score := f.classifier.FindBestMatch(ctx, text)
	if f.metrics != nil {
		f.metrics.recordFilterLatency(f.Name(), time.Since(start).Seconds())
	}

	// Below threshold OR matched a benign exemplar → allow.
	// Treating benign-matches as no-violation is intentional: the
	// nearest-neighbor was a legitimate phrasing, the classifier
	// confidently said "this is fine."
	if match == nil || match.Intent == "benign" {
		signal := "no_match"
		if match != nil {
			signal = "benign"
		}
		f.recordDecision("allow", signal, score)
		// Confidence semantics: always the match score. Direction
		// of "confidence in what" is carried by Allowed + the
		// signal label, not by inverting the number.
		return NewFilterResult(true).WithConfidence(score), nil
	}

	signal := match.Intent
	topMatchID := stringFromOptions(match.Options, injectioncorpus.OptionKeyID)

	verdictLabel := "block"
	if f.shadowMode {
		verdictLabel = "shadow"
	}
	f.recordDecision(verdictLabel, signal, score)

	violation := f.buildViolation(msg, match, score, topMatchID)
	metadata := f.buildMetadata(signal, score, topMatchID)

	if f.shadowMode {
		// Shadow mode: surface the verdict without blocking. We
		// construct FilterResult manually because WithViolation
		// forces Allowed=false, which is the wrong shape for
		// shadow. The chain-level Process now collects this
		// violation regardless of Allowed (ADR-043 Phase 2b
		// review fix) so the audit + publish path still fires.
		return &FilterResult{
			Allowed:    true,
			Violation:  violation,
			Confidence: score,
			Metadata:   metadata,
		}, nil
	}

	// Enforce mode: block. Use the helper to keep metadata shape
	// identical between modes.
	return &FilterResult{
		Allowed:    false,
		Violation:  violation,
		Confidence: score,
		Metadata:   metadata,
	}, nil
}

// buildMetadata returns the per-decision metadata bag attached to
// the FilterResult. Identical shape in shadow and enforce so
// downstream consumers don't have to discover field availability
// per-mode. The shadow_mode flag inside the bag distinguishes the
// two paths cleanly.
func (f *InjectionClassifierFilter) buildMetadata(signal string, score float64, topMatchID string) map[string]any {
	return map[string]any{
		governance.InjectionSignal:      signal,
		governance.InjectionTier:        1,
		governance.InjectionScore:       score,
		governance.InjectionTopMatchID:  topMatchID,
		"shadow_mode":                   f.shadowMode,
		"injection_classifier_corpus_n": f.corpusCount,
	}
}

// buildViolation packages the classifier verdict into the shared
// Violation shape. Details carry the four governance.injection.*
// surface fields plus structural diagnostics; OriginalContent is
// truncated for audit by the existing helper.
func (f *InjectionClassifierFilter) buildViolation(msg *Message, match *query.Example, score float64, topMatchID string) *Violation {
	action := ViolationActionBlocked
	severity := SeverityHigh
	if f.shadowMode {
		action = ViolationActionFlagged
		severity = SeverityMedium
	}

	v := NewViolation(f.Name(), severity, msg).
		WithAction(action).
		WithConfidence(score).
		WithDetail(governance.InjectionSignal, match.Intent).
		WithDetail(governance.InjectionTier, 1).
		WithDetail(governance.InjectionScore, score).
		WithDetail(governance.InjectionTopMatchID, topMatchID).
		WithDetail("threshold", f.threshold).
		WithDetail("source", stringFromOptions(match.Options, injectioncorpus.OptionKeySource)).
		WithOriginalContent(truncateForAudit(msg.Content.Text, 200))

	return v
}

// recordDecision emits the Phase 2 classifier decision metric.
// Signal label is normalised through signalForLabel to keep
// Prometheus label cardinality bounded.
func (f *InjectionClassifierFilter) recordDecision(verdict, signal string, score float64) {
	if f.metrics == nil {
		return
	}
	f.metrics.recordClassifierDecision(verdict, signalForLabel(signal))
	f.metrics.recordClassifierScore(score)
}

// signalForLabel collapses a corpus-loaded signal bucket to the
// label used in the bounded-cardinality classifier-decision metric.
// Known buckets (ADR-043 enum + "benign") pass through; the
// distinguished "no_match" sentinel also passes through; anything
// else maps to "unknown" so a typoed corpus bucket surfaces in
// dashboards rather than ballooning label cardinality.
func signalForLabel(signal string) string {
	if signal == "no_match" {
		return signal
	}
	if _, ok := knownSignals[signal]; ok {
		return signal
	}
	return signalUnknown
}

// stringFromOptions extracts a string value from an Example.Options
// map. Returns "" on missing key or wrong type; callers treat empty
// as "unknown".
func stringFromOptions(opts map[string]any, key string) string {
	if opts == nil {
		return ""
	}
	v, ok := opts[key]
	if !ok {
		return ""
	}
	s, ok := v.(string)
	if !ok {
		return ""
	}
	return s
}

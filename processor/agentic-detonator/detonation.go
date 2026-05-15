package agenticdetonator

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"time"

	injectioncorpus "github.com/c360studio/semstreams/processor/agentic-governance/injection_corpus"
)

// Detonation aggregates the observable outputs of one canary run
// over one input. The fields are populated incrementally by the
// canary loop (Phase 3b); Phase 3a treats it as a pure data
// structure that converts cleanly into an injectioncorpus.Record
// for round-trip into the Phase 2 classifier corpus.
//
// Why this lives in 3a: the Record-conversion contract IS the
// detonator's product. Getting the shape right before the canary
// loop is written keeps 3b focused on the loop control flow and
// not on figuring out how to package its findings.
type Detonation struct {
	// Input is the untrusted text the canary was asked to process.
	// Becomes the corpus record's Text field.
	Input string

	// CanaryModel records which model.Registry endpoint ran the
	// canary. Persisted into the corpus record's source provenance
	// so operators can audit which model produced which labels.
	CanaryModel string

	// StartedAt is the canary's invocation wallclock. Latency and
	// trace correlation derive from this.
	StartedAt time.Time

	// Duration is the canary's wall time end-to-end. Recorded so
	// the Phase 3 cost accounting can audit per-detonation budget
	// compliance.
	Duration time.Duration

	// Turns is the number of canary LLM turns consumed before the
	// detonation terminated (success or cost-cap hit).
	Turns int

	// ToolCalls is the executor's trajectory in observation order.
	ToolCalls []RecordedCall

	// URLs is the markdown-URL scanner output. Populated by the
	// Phase 3b canary aggregator (it accumulates each turn's raw
	// text and scans the concatenation); Phase 3a treats this as
	// pre-aggregated input.
	URLs []ScannedURL
}

// PrimarySignal returns the dominant signal bucket the detonation
// produced, applying the ToolCalls signal taxonomy first and
// falling back to network-egress when only URLs are present. Empty
// trajectory + empty URLs → SignalBenign (the canary saw the input
// and did not bite).
func (d *Detonation) PrimarySignal() string {
	signals := SignalsFromTrajectory(d.ToolCalls)
	if u := SignalFromScannedURLs(d.URLs); u != "" {
		// URL signal is treated as an additional contributor;
		// PrimarySignal applies its own priority order.
		signals = append(signals, u)
	}
	return PrimarySignal(signals)
}

// AllSignals returns the deduped set of signals observed. Useful
// for multi-label exports (Phase 4 corpus extension per ADR-043
// line 268-272) even though Phase 3 writes single-label records.
func (d *Detonation) AllSignals() []string {
	signals := SignalsFromTrajectory(d.ToolCalls)
	if u := SignalFromScannedURLs(d.URLs); u != "" {
		// Dedup: append only if not already present.
		present := false
		for _, s := range signals {
			if s == u {
				present = true
				break
			}
		}
		if !present {
			signals = append(signals, u)
		}
	}
	if len(signals) == 0 {
		return []string{SignalBenign}
	}
	return signals
}

// ID returns the stable identifier for this detonation: hex
// sha256 of the input text. Becomes the corpus record's ID field
// so the runtime classifier's top_match_id surfaces a value that
// uniquely re-locates the source detonation.
//
// Hashing only the input (not the trajectory) means re-detonating
// the same input under a different model produces the same ID. The
// corpus loader's duplicate-ID detection will then surface the
// collision at load time, forcing the operator to choose which
// model's labels to trust — a feature, not a bug, per the
// "feedback_warning_not_fail_masks_integration_drift" discipline.
func (d *Detonation) ID() string {
	sum := sha256.Sum256([]byte(d.Input))
	return hex.EncodeToString(sum[:])
}

// ToRecord converts the detonation into the corpus-loader Record
// format consumed by Phase 2's processor/agentic-governance/
// injection_corpus loader. Single-label by design (Phase 4 may
// extend); the chosen label is PrimarySignal.
//
// Source provenance is structured: "detonator/<model>/<date>".
// Operators see at a glance which canary produced which corpus
// entries.
func (d *Detonation) ToRecord() injectioncorpus.Record {
	source := fmt.Sprintf("detonator/%s/%s",
		nonEmpty(d.CanaryModel, "unknown-model"),
		d.StartedAt.UTC().Format("2006-01-02"),
	)
	return injectioncorpus.Record{
		ID:     d.ID(),
		Text:   d.Input,
		Signal: d.PrimarySignal(),
		Source: source,
	}
}

// nonEmpty returns s when non-empty, fallback otherwise. Keeps the
// provenance string well-formed even when the canary model name
// wasn't populated (test paths, misconfigured runs).
func nonEmpty(s, fallback string) string {
	if s == "" {
		return fallback
	}
	return s
}

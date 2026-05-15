package agenticgovernance

import (
	"fmt"
	"regexp"

	"github.com/c360studio/semstreams/pkg/errs"
)

// InjectionFilterConfig holds injection detection filter configuration
type InjectionFilterConfig struct {
	ConfidenceThreshold float64               `json:"confidence_threshold" schema:"type:float,description:Confidence threshold for blocking (0.0-1.0),category:basic,default:0.80"`
	Patterns            []InjectionPatternDef `json:"patterns,omitempty" schema:"type:array,description:Injection patterns to detect,category:advanced"`
	EnabledPatterns     []string              `json:"enabled_patterns,omitempty" schema:"type:array,description:Built-in pattern names to enable,category:basic"`
}

// InjectionClassifierConfig configures the embedding-classifier filter
// added by Phase 2 of ADR-043. It is a peer to InjectionFilterConfig
// — operators add a separate `injection_classifier` filter entry
// alongside the existing `injection_detection` entry. The regex
// filter and the classifier filter run in their own chain slots so
// the chain can decide ordering and operators can disable either
// tier independently.
//
// The classifier is gated by the outer FilterConfig.Enabled flag;
// this struct does not duplicate that knob. ShadowMode is the only
// runtime mode toggle — default true so operators must explicitly
// opt into enforcement once they've completed the per-deployment
// calibration pass.
//
// See ADR-043 for the full design; the project_adr_043_phase_2_plan
// memory documents the rule-opacity split and measurement protocol.
type InjectionClassifierConfig struct {
	// Threshold is the cosine-similarity floor for a positive
	// match. Below this score, the verdict is "no match" and the
	// filter falls through. Operators tune this after running
	// `task adr043:measure` against their benign slice.
	Threshold float64 `json:"threshold" schema:"type:float,description:Cosine-similarity floor for a positive match (0.0-1.0),category:basic,default:0.70"`

	// ShadowMode emits the verdict (metrics + violation record)
	// but does not block. The recommended posture during the
	// first deployment until the operator has confirmed the
	// false-positive rate is acceptable.
	ShadowMode bool `json:"shadow_mode" schema:"type:bool,description:Emit verdict but never block (calibration mode),category:basic,default:true"`

	// CorpusSources lists the JSONL corpus files to load. Each
	// entry produces one DomainExamples. Phase 2 ships a single
	// vendored seed; Phase 3+ adds detonator-written corpora and
	// vendored public sources. At least one source is required;
	// validation rejects empty lists at boot.
	CorpusSources []InjectionCorpusSource `json:"corpus_sources,omitempty" schema:"type:array,description:Corpus files to load,category:advanced"`
}

// InjectionCorpusSource is the operator-configurable form of
// injectioncorpus.Source. Kept in this package to avoid pulling
// processor-side types into config; the runtime adapter translates.
type InjectionCorpusSource struct {
	Domain  string `json:"domain" schema:"type:string,description:Tag identifying this corpus,category:basic"`
	Version string `json:"version" schema:"type:string,description:Corpus revision,category:basic"`
	Path    string `json:"path" schema:"type:string,description:JSONL file path,category:basic"`
}

// InjectionPatternDef defines an injection detection pattern
type InjectionPatternDef struct {
	Name        string   `json:"name" schema:"type:string,description:Pattern identifier,category:basic"`
	Pattern     string   `json:"pattern" schema:"type:string,description:Regex pattern,category:basic"`
	Description string   `json:"description" schema:"type:string,description:Pattern description,category:basic"`
	Severity    Severity `json:"severity" schema:"type:string,description:Violation severity,category:basic,default:high"`
	Confidence  float64  `json:"confidence" schema:"type:float,description:Detection confidence,category:advanced,default:0.90"`
}

// Severity levels for violations
type Severity string

// Severity levels define the threat level of violations.
const (
	SeverityCritical Severity = "critical"
	SeverityHigh     Severity = "high"
	SeverityMedium   Severity = "medium"
	SeverityLow      Severity = "low"
)

// Validate checks injection filter configuration
func (c *InjectionFilterConfig) Validate() error {
	if c.ConfidenceThreshold < 0 || c.ConfidenceThreshold > 1 {
		return errs.WrapInvalid(fmt.Errorf("confidence_threshold must be between 0.0 and 1.0"), "InjectionFilterConfig", "Validate", "validate confidence_threshold")
	}

	// Validate patterns compile
	for i, pattern := range c.Patterns {
		if _, err := regexp.Compile(pattern.Pattern); err != nil {
			return errs.WrapInvalid(err, "InjectionFilterConfig", "Validate", fmt.Sprintf("validate patterns[%d] regex", i))
		}
	}

	return nil
}

// Validate checks the embedding-classifier sub-config. Only the
// runtime invariants — threshold range, at least one corpus source,
// well-formed source entries. Missing corpus files surface at load
// time in the loader; the validator does not stat the filesystem.
//
// Outer FilterConfig.Enabled gates the filter as a whole; this
// validator only runs when the operator has chosen to configure
// the classifier (the chain filter case requires non-nil
// ClassifierConfig). So "corpus required" is unconditional here —
// a configured classifier without a corpus is a misconfiguration,
// not a valid empty state.
func (c *InjectionClassifierConfig) Validate() error {
	if c.Threshold < 0 || c.Threshold > 1 {
		return errs.WrapInvalid(fmt.Errorf("threshold must be between 0.0 and 1.0"), "InjectionClassifierConfig", "Validate", "validate threshold")
	}
	if len(c.CorpusSources) == 0 {
		return errs.WrapInvalid(fmt.Errorf("corpus_sources is required and must contain at least one entry"), "InjectionClassifierConfig", "Validate", "validate corpus_sources")
	}
	for i, src := range c.CorpusSources {
		if src.Domain == "" {
			return errs.WrapInvalid(fmt.Errorf("corpus_sources[%d].domain empty", i), "InjectionClassifierConfig", "Validate", "validate corpus_sources entry")
		}
		if src.Path == "" {
			return errs.WrapInvalid(fmt.Errorf("corpus_sources[%d].path empty", i), "InjectionClassifierConfig", "Validate", "validate corpus_sources entry")
		}
	}
	return nil
}

// DefaultInjectionConfig returns default injection filter configuration
func DefaultInjectionConfig() *InjectionFilterConfig {
	return &InjectionFilterConfig{
		ConfidenceThreshold: 0.80,
		EnabledPatterns:     []string{"instruction_override", "jailbreak_persona", "system_injection", "delimiter_injection", "role_confusion"},
	}
}

// Package inference provides structural anomaly detection and inference
// for identifying potential missing relationships in the knowledge graph.
package inference

import (
	"bytes"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/c360studio/semstreams/graph/llm"
	"github.com/c360studio/semstreams/pkg/errs"
)

// Config configures the structural inference system
type Config struct {
	// Enabled activates inference detection
	Enabled bool `json:"enabled"`

	// RunWithCommunityDetection triggers inference after community detection completes
	RunWithCommunityDetection bool `json:"run_with_community_detection"`

	// MaxAnomaliesPerRun limits total anomalies detected per run to prevent runaway detection
	MaxAnomaliesPerRun int `json:"max_anomalies_per_run"`

	// Detector-specific configurations
	SemanticGap  SemanticGapConfig  `json:"semantic_gap"`
	CoreAnomaly  CoreAnomalyConfig  `json:"core_anomaly"`
	Transitivity TransitivityConfig `json:"transitivity"`

	// Review configuration for LLM-assisted and human review
	Review ReviewConfig `json:"review"`

	// VirtualEdges configuration for auto-applying high-confidence gaps as edges
	VirtualEdges VirtualEdgeConfig `json:"virtual_edges"`

	// Storage configuration
	Storage StorageConfig `json:"storage"`

	// Operation timeouts
	DetectionTimeout time.Duration `json:"detection_timeout"`
}

// SemanticGapConfig configures the semantic-structural gap detector
type SemanticGapConfig struct {
	// Enabled activates semantic-structural gap detection
	Enabled bool `json:"enabled"`

	// MinSemanticSimilarity is the minimum embedding similarity to consider (0.0-1.0)
	MinSemanticSimilarity float64 `json:"min_semantic_similarity"`

	// MinStructuralDistance is the minimum graph distance for flagging (hops)
	MinStructuralDistance int `json:"min_structural_distance"`

	// MaxGapsPerEntity limits gaps detected per entity to prevent noise
	MaxGapsPerEntity int `json:"max_gaps_per_entity"`

	// MaxCandidatesPerEntity limits semantic search candidates per entity
	MaxCandidatesPerEntity int `json:"max_candidates_per_entity"`
}

// CoreAnomalyConfig configures the k-core based anomaly detectors
type CoreAnomalyConfig struct {
	// Enabled activates core-based anomaly detection (isolation + demotion)
	Enabled bool `json:"enabled"`

	// MinCoreForHubAnalysis sets minimum k-core level for hub isolation analysis
	MinCoreForHubAnalysis int `json:"min_core_for_hub_analysis"`

	// HubIsolationThreshold is the peer connectivity ratio below which hub is "isolated"
	// Value between 0.0-1.0: ratio of actual same-core connections to expected
	HubIsolationThreshold float64 `json:"hub_isolation_threshold"`

	// TrackCoreDemotions enables detection of entities that dropped k-core level
	TrackCoreDemotions bool `json:"track_core_demotions"`

	// MinDemotionDelta is the minimum core level drop to flag (e.g., 2 means core 5->3 flagged)
	MinDemotionDelta int `json:"min_demotion_delta"`
}

// TransitivityConfig configures the transitivity gap detector
type TransitivityConfig struct {
	// Enabled activates transitivity gap detection
	Enabled bool `json:"enabled"`

	// MaxIntermediateHops is the maximum hops in A->...->B->...->C chains to analyze
	MaxIntermediateHops int `json:"max_intermediate_hops"`

	// MinExpectedTransitivity is the maximum expected A-C distance when A->B->C exists
	// Gaps where actual distance > this are flagged
	MinExpectedTransitivity int `json:"min_expected_transitivity"`

	// TransitivePredicates lists predicates that should be transitive
	// e.g., ["member_of", "part_of", "located_in"]
	TransitivePredicates []string `json:"transitive_predicates"`
}

// ReviewConfig configures the LLM-assisted review pipeline
type ReviewConfig struct {
	// Enabled activates the review worker
	Enabled bool `json:"enabled"`

	// Workers is the number of concurrent review workers
	Workers int `json:"workers"`

	// AutoApproveThreshold: LLM can auto-approve anomalies at or above this confidence
	AutoApproveThreshold float64 `json:"auto_approve_threshold"`

	// AutoRejectThreshold: LLM can auto-reject anomalies at or below this confidence
	AutoRejectThreshold float64 `json:"auto_reject_threshold"`

	// FallbackToHuman escalates uncertain cases (between thresholds) to human review
	FallbackToHuman bool `json:"fallback_to_human"`

	// BatchSize is the number of anomalies to process in each review batch
	BatchSize int `json:"batch_size"`

	// ReviewTimeout is the timeout for individual LLM review calls
	ReviewTimeout time.Duration `json:"review_timeout"`

	// LLM configuration for the review model
	LLM llm.Config `json:"llm"`
}

// VirtualEdgeConfig configures automatic edge creation from high-confidence anomalies
type VirtualEdgeConfig struct {
	// AutoApply configures automatic edge creation for high-confidence gaps
	AutoApply AutoApplyConfig `json:"auto_apply"`

	// ReviewQueue configures lower-confidence gaps that need LLM/human review
	ReviewQueue ReviewQueueConfig `json:"review_queue"`
}

// AutoApplyConfig configures automatic edge creation for high-confidence semantic gaps
type AutoApplyConfig struct {
	// Enabled activates automatic edge creation
	Enabled bool `json:"enabled"`

	// MinConfidence is the minimum confidence score to auto-apply (0.0-1.0)
	// Confidence is a composite score: similarity + distance boost + core boost
	// Gaps at or above this threshold are automatically converted to edges
	MinConfidence float64 `json:"min_confidence"`

	// PredicateTemplate is the predicate format for created edges
	// Supports {band} placeholder: "inferred.semantic.{band}"
	// Band values: "high" (>=0.95), "medium" (>=0.85), "related" (else)
	PredicateTemplate string `json:"predicate_template"`
}

// ReviewQueueConfig configures gaps that need LLM or human review before edge creation
type ReviewQueueConfig struct {
	// Enabled activates the review queue for lower-confidence gaps
	Enabled bool `json:"enabled"`

	// MinConfidence is the minimum confidence score for review queue (0.0-1.0)
	// Gaps at or above this but below AutoApply.MinConfidence go to review
	MinConfidence float64 `json:"min_confidence"`

	// MaxConfidence is the maximum confidence score for review queue (0.0-1.0)
	// Should match AutoApply.MinConfidence for seamless handoff
	MaxConfidence float64 `json:"max_confidence"`

	// RequireLLMClassification requires LLM to suggest relationship type
	RequireLLMClassification bool `json:"require_llm_classification"`
}

// StorageConfig configures anomaly storage
type StorageConfig struct {
	// BucketName is the NATS KV bucket for storing anomalies
	BucketName string `json:"bucket_name"`

	// RetentionDays is how long to keep resolved anomalies (applied/rejected)
	RetentionDays int `json:"retention_days"`

	// CleanupInterval is how often to run cleanup of old anomalies
	CleanupInterval time.Duration `json:"cleanup_interval"`
}

// DefaultConfig returns a Config with sensible defaults
func DefaultConfig() Config {
	return Config{
		Enabled:                   false, // Opt-in feature
		RunWithCommunityDetection: true,
		MaxAnomaliesPerRun:        100,
		SemanticGap: SemanticGapConfig{
			Enabled:                true,
			MinSemanticSimilarity:  0.7,
			MinStructuralDistance:  3,
			MaxGapsPerEntity:       5,
			MaxCandidatesPerEntity: 50,
		},
		CoreAnomaly: CoreAnomalyConfig{
			Enabled:               true,
			MinCoreForHubAnalysis: 3,
			HubIsolationThreshold: 0.5,
			TrackCoreDemotions:    true,
			MinDemotionDelta:      2,
		},
		Transitivity: TransitivityConfig{
			Enabled:                 true,
			MaxIntermediateHops:     2,
			MinExpectedTransitivity: 3,
			TransitivePredicates:    []string{"member_of", "part_of", "located_in", "belongs_to"},
		},
		Review: ReviewConfig{
			Enabled:              false, // Requires LLM setup
			Workers:              2,
			AutoApproveThreshold: 0.9,
			AutoRejectThreshold:  0.3,
			FallbackToHuman:      true,
			BatchSize:            10,
			ReviewTimeout:        30 * time.Second,
		},
		VirtualEdges: VirtualEdgeConfig{
			AutoApply: AutoApplyConfig{
				Enabled:           false, // Opt-in feature
				MinConfidence:     0.95,  // High confidence = auto-apply
				PredicateTemplate: "inferred.semantic.{band}",
			},
			ReviewQueue: ReviewQueueConfig{
				Enabled:                  false, // Requires LLM setup
				MinConfidence:            0.7,   // Review queue lower bound
				MaxConfidence:            0.95,  // Below auto-apply threshold
				RequireLLMClassification: true,
			},
		},
		Storage: StorageConfig{
			BucketName:      "ANOMALY_INDEX",
			RetentionDays:   30,
			CleanupInterval: 24 * time.Hour,
		},
		DetectionTimeout: 5 * time.Minute,
	}
}

// RejectUnknownKeys strict-decodes a raw anomaly-config JSON object into Config
// and returns a descriptive error if it carries any key that does not bind to a
// struct field. encoding/json silently discards such keys at runtime — the
// silent-drop bug class fixed in ADR-054 (e.g. the legacy similarity_threshold
// and min_core_level keys, whose values vanished while appearing to be set).
// Callers decoding operator-supplied config should run this so a mistyped knob
// fails loudly at load instead of binding to nothing. Empty input is a no-op.
func RejectUnknownKeys(raw json.RawMessage) error {
	if len(raw) == 0 {
		return nil
	}
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.DisallowUnknownFields()
	var probe Config
	if err := dec.Decode(&probe); err != nil {
		msg := fmt.Sprintf("anomaly_config has a key that does not bind and would be "+
			"silently ignored at runtime (ADR-054; legacy renames: "+
			"similarity_threshold→min_semantic_similarity, "+
			"min_core_level→min_core_for_hub_analysis): %v", err)
		return errs.WrapInvalid(errs.ErrInvalidConfig, "inference", "RejectUnknownKeys", msg)
	}
	return nil
}

// Validate checks if the configuration is valid
func (c *Config) Validate() error {
	if !c.Enabled {
		return nil // Skip validation if disabled
	}

	if err := c.validateGlobalSettings(); err != nil {
		return err
	}

	if err := c.validateSemanticGap(); err != nil {
		return err
	}

	if err := c.validateCoreAnomaly(); err != nil {
		return err
	}

	if err := c.validateTransitivity(); err != nil {
		return err
	}

	if err := c.validateReview(); err != nil {
		return err
	}

	if err := c.validateVirtualEdges(); err != nil {
		return err
	}

	return c.validateStorage()
}

// validateGlobalSettings validates top-level configuration
func (c *Config) validateGlobalSettings() error {
	if c.MaxAnomaliesPerRun <= 0 {
		msg := fmt.Sprintf("max_anomalies_per_run must be positive, got %d", c.MaxAnomaliesPerRun)
		return errs.WrapInvalid(errs.ErrInvalidConfig, "inference", "Validate", msg)
	}

	if c.DetectionTimeout <= 0 {
		msg := fmt.Sprintf("detection_timeout must be positive, got %v", c.DetectionTimeout)
		return errs.WrapInvalid(errs.ErrInvalidConfig, "inference", "Validate", msg)
	}

	// At least one detector must be enabled
	if !c.SemanticGap.Enabled && !c.CoreAnomaly.Enabled && !c.Transitivity.Enabled {
		msg := "at least one detector must be enabled when inference is enabled"
		return errs.WrapInvalid(errs.ErrInvalidConfig, "inference", "Validate", msg)
	}

	return nil
}

// validateSemanticGap validates semantic gap detector configuration
func (c *Config) validateSemanticGap() error {
	if !c.SemanticGap.Enabled {
		return nil
	}

	if c.SemanticGap.MinSemanticSimilarity < 0 || c.SemanticGap.MinSemanticSimilarity > 1 {
		msg := fmt.Sprintf(
			"semantic_gap.min_semantic_similarity must be between 0 and 1, got %f",
			c.SemanticGap.MinSemanticSimilarity,
		)
		return errs.WrapInvalid(errs.ErrInvalidConfig, "inference", "Validate", msg)
	}

	if c.SemanticGap.MinStructuralDistance <= 0 {
		msg := fmt.Sprintf(
			"semantic_gap.min_structural_distance must be positive, got %d",
			c.SemanticGap.MinStructuralDistance,
		)
		return errs.WrapInvalid(errs.ErrInvalidConfig, "inference", "Validate", msg)
	}

	if c.SemanticGap.MaxGapsPerEntity <= 0 {
		msg := fmt.Sprintf(
			"semantic_gap.max_gaps_per_entity must be positive, got %d",
			c.SemanticGap.MaxGapsPerEntity,
		)
		return errs.WrapInvalid(errs.ErrInvalidConfig, "inference", "Validate", msg)
	}

	if c.SemanticGap.MaxCandidatesPerEntity <= 0 {
		msg := fmt.Sprintf(
			"semantic_gap.max_candidates_per_entity must be positive, got %d",
			c.SemanticGap.MaxCandidatesPerEntity,
		)
		return errs.WrapInvalid(errs.ErrInvalidConfig, "inference", "Validate", msg)
	}

	return nil
}

// validateCoreAnomaly validates core anomaly detector configuration
func (c *Config) validateCoreAnomaly() error {
	if !c.CoreAnomaly.Enabled {
		return nil
	}

	if c.CoreAnomaly.MinCoreForHubAnalysis < 1 {
		msg := fmt.Sprintf(
			"core_anomaly.min_core_for_hub_analysis must be at least 1, got %d",
			c.CoreAnomaly.MinCoreForHubAnalysis,
		)
		return errs.WrapInvalid(errs.ErrInvalidConfig, "inference", "Validate", msg)
	}

	if c.CoreAnomaly.HubIsolationThreshold < 0 || c.CoreAnomaly.HubIsolationThreshold > 1 {
		msg := fmt.Sprintf(
			"core_anomaly.hub_isolation_threshold must be between 0 and 1, got %f",
			c.CoreAnomaly.HubIsolationThreshold,
		)
		return errs.WrapInvalid(errs.ErrInvalidConfig, "inference", "Validate", msg)
	}

	if c.CoreAnomaly.TrackCoreDemotions && c.CoreAnomaly.MinDemotionDelta <= 0 {
		msg := fmt.Sprintf(
			"core_anomaly.min_demotion_delta must be positive when tracking demotions, got %d",
			c.CoreAnomaly.MinDemotionDelta,
		)
		return errs.WrapInvalid(errs.ErrInvalidConfig, "inference", "Validate", msg)
	}

	return nil
}

// validateTransitivity validates transitivity gap detector configuration
func (c *Config) validateTransitivity() error {
	if !c.Transitivity.Enabled {
		return nil
	}

	if c.Transitivity.MaxIntermediateHops <= 0 {
		msg := fmt.Sprintf(
			"transitivity.max_intermediate_hops must be positive, got %d",
			c.Transitivity.MaxIntermediateHops,
		)
		return errs.WrapInvalid(errs.ErrInvalidConfig, "inference", "Validate", msg)
	}

	if c.Transitivity.MinExpectedTransitivity <= 0 {
		msg := fmt.Sprintf(
			"transitivity.min_expected_transitivity must be positive, got %d",
			c.Transitivity.MinExpectedTransitivity,
		)
		return errs.WrapInvalid(errs.ErrInvalidConfig, "inference", "Validate", msg)
	}

	return nil
}

// validateReview validates review pipeline configuration
func (c *Config) validateReview() error {
	if !c.Review.Enabled {
		return nil
	}

	if c.Review.Workers <= 0 {
		msg := fmt.Sprintf("review.workers must be positive, got %d", c.Review.Workers)
		return errs.WrapInvalid(errs.ErrInvalidConfig, "inference", "Validate", msg)
	}

	if c.Review.Workers > 10 {
		msg := fmt.Sprintf(
			"review.workers should not exceed 10 for resource management, got %d",
			c.Review.Workers,
		)
		return errs.WrapInvalid(errs.ErrInvalidConfig, "inference", "Validate", msg)
	}

	if c.Review.AutoApproveThreshold < 0 || c.Review.AutoApproveThreshold > 1 {
		msg := fmt.Sprintf(
			"review.auto_approve_threshold must be between 0 and 1, got %f",
			c.Review.AutoApproveThreshold,
		)
		return errs.WrapInvalid(errs.ErrInvalidConfig, "inference", "Validate", msg)
	}

	if c.Review.AutoRejectThreshold < 0 || c.Review.AutoRejectThreshold > 1 {
		msg := fmt.Sprintf(
			"review.auto_reject_threshold must be between 0 and 1, got %f",
			c.Review.AutoRejectThreshold,
		)
		return errs.WrapInvalid(errs.ErrInvalidConfig, "inference", "Validate", msg)
	}

	if c.Review.AutoRejectThreshold >= c.Review.AutoApproveThreshold {
		msg := fmt.Sprintf(
			"review.auto_reject_threshold (%f) must be less than auto_approve_threshold (%f)",
			c.Review.AutoRejectThreshold,
			c.Review.AutoApproveThreshold,
		)
		return errs.WrapInvalid(errs.ErrInvalidConfig, "inference", "Validate", msg)
	}

	if c.Review.BatchSize <= 0 {
		msg := fmt.Sprintf("review.batch_size must be positive, got %d", c.Review.BatchSize)
		return errs.WrapInvalid(errs.ErrInvalidConfig, "inference", "Validate", msg)
	}

	if c.Review.ReviewTimeout <= 0 {
		msg := fmt.Sprintf("review.review_timeout must be positive, got %v", c.Review.ReviewTimeout)
		return errs.WrapInvalid(errs.ErrInvalidConfig, "inference", "Validate", msg)
	}

	return nil
}

// validateVirtualEdges validates virtual edge configuration
func (c *Config) validateVirtualEdges() error {
	// Validate AutoApply config
	if c.VirtualEdges.AutoApply.Enabled {
		if c.VirtualEdges.AutoApply.MinConfidence < 0 || c.VirtualEdges.AutoApply.MinConfidence > 1 {
			msg := fmt.Sprintf(
				"virtual_edges.auto_apply.min_confidence must be between 0 and 1, got %f",
				c.VirtualEdges.AutoApply.MinConfidence,
			)
			return errs.WrapInvalid(errs.ErrInvalidConfig, "inference", "Validate", msg)
		}

		if c.VirtualEdges.AutoApply.PredicateTemplate == "" {
			msg := "virtual_edges.auto_apply.predicate_template cannot be empty"
			return errs.WrapInvalid(errs.ErrInvalidConfig, "inference", "Validate", msg)
		}
	}

	// Validate ReviewQueue config
	if c.VirtualEdges.ReviewQueue.Enabled {
		if c.VirtualEdges.ReviewQueue.MinConfidence < 0 || c.VirtualEdges.ReviewQueue.MinConfidence > 1 {
			msg := fmt.Sprintf(
				"virtual_edges.review_queue.min_confidence must be between 0 and 1, got %f",
				c.VirtualEdges.ReviewQueue.MinConfidence,
			)
			return errs.WrapInvalid(errs.ErrInvalidConfig, "inference", "Validate", msg)
		}

		if c.VirtualEdges.ReviewQueue.MaxConfidence < 0 || c.VirtualEdges.ReviewQueue.MaxConfidence > 1 {
			msg := fmt.Sprintf(
				"virtual_edges.review_queue.max_confidence must be between 0 and 1, got %f",
				c.VirtualEdges.ReviewQueue.MaxConfidence,
			)
			return errs.WrapInvalid(errs.ErrInvalidConfig, "inference", "Validate", msg)
		}

		if c.VirtualEdges.ReviewQueue.MinConfidence >= c.VirtualEdges.ReviewQueue.MaxConfidence {
			msg := fmt.Sprintf(
				"virtual_edges.review_queue.min_confidence (%f) must be less than max_confidence (%f)",
				c.VirtualEdges.ReviewQueue.MinConfidence,
				c.VirtualEdges.ReviewQueue.MaxConfidence,
			)
			return errs.WrapInvalid(errs.ErrInvalidConfig, "inference", "Validate", msg)
		}
	}

	return nil
}

// validateStorage validates storage configuration
func (c *Config) validateStorage() error {
	if c.Storage.BucketName == "" {
		msg := "storage.bucket_name cannot be empty"
		return errs.WrapInvalid(errs.ErrInvalidConfig, "inference", "Validate", msg)
	}

	if len(c.Storage.BucketName) > 64 {
		msg := fmt.Sprintf(
			"storage.bucket_name is too long (max 64 chars): %s",
			c.Storage.BucketName,
		)
		return errs.WrapInvalid(errs.ErrInvalidConfig, "inference", "Validate", msg)
	}

	if c.Storage.RetentionDays <= 0 {
		msg := fmt.Sprintf("storage.retention_days must be positive, got %d", c.Storage.RetentionDays)
		return errs.WrapInvalid(errs.ErrInvalidConfig, "inference", "Validate", msg)
	}

	if c.Storage.CleanupInterval <= 0 {
		msg := fmt.Sprintf("storage.cleanup_interval must be positive, got %v", c.Storage.CleanupInterval)
		return errs.WrapInvalid(errs.ErrInvalidConfig, "inference", "Validate", msg)
	}

	return nil
}

// ApplyDefaults fills in any zero values with defaults
func (c *Config) ApplyDefaults() {
	defaults := DefaultConfig()

	c.applyGlobalDefaults(defaults)
	c.applySemanticGapDefaults(defaults)
	c.applyCoreAnomalyDefaults(defaults)
	c.applyTransitivityDefaults(defaults)
	c.applyReviewDefaults(defaults)
	c.applyVirtualEdgesDefaults(defaults)
	c.applyStorageDefaults(defaults)
}

func (c *Config) applyGlobalDefaults(defaults Config) {
	if c.MaxAnomaliesPerRun == 0 {
		c.MaxAnomaliesPerRun = defaults.MaxAnomaliesPerRun
	}
	if c.DetectionTimeout == 0 {
		c.DetectionTimeout = defaults.DetectionTimeout
	}
}

func (c *Config) applySemanticGapDefaults(defaults Config) {
	if c.SemanticGap.MinSemanticSimilarity == 0 {
		c.SemanticGap.MinSemanticSimilarity = defaults.SemanticGap.MinSemanticSimilarity
	}
	if c.SemanticGap.MinStructuralDistance == 0 {
		c.SemanticGap.MinStructuralDistance = defaults.SemanticGap.MinStructuralDistance
	}
	if c.SemanticGap.MaxGapsPerEntity == 0 {
		c.SemanticGap.MaxGapsPerEntity = defaults.SemanticGap.MaxGapsPerEntity
	}
	if c.SemanticGap.MaxCandidatesPerEntity == 0 {
		c.SemanticGap.MaxCandidatesPerEntity = defaults.SemanticGap.MaxCandidatesPerEntity
	}
}

func (c *Config) applyCoreAnomalyDefaults(defaults Config) {
	if c.CoreAnomaly.MinCoreForHubAnalysis == 0 {
		c.CoreAnomaly.MinCoreForHubAnalysis = defaults.CoreAnomaly.MinCoreForHubAnalysis
	}
	if c.CoreAnomaly.HubIsolationThreshold == 0 {
		c.CoreAnomaly.HubIsolationThreshold = defaults.CoreAnomaly.HubIsolationThreshold
	}
	if c.CoreAnomaly.MinDemotionDelta == 0 {
		c.CoreAnomaly.MinDemotionDelta = defaults.CoreAnomaly.MinDemotionDelta
	}
}

func (c *Config) applyTransitivityDefaults(defaults Config) {
	if c.Transitivity.MaxIntermediateHops == 0 {
		c.Transitivity.MaxIntermediateHops = defaults.Transitivity.MaxIntermediateHops
	}
	if c.Transitivity.MinExpectedTransitivity == 0 {
		c.Transitivity.MinExpectedTransitivity = defaults.Transitivity.MinExpectedTransitivity
	}
	if len(c.Transitivity.TransitivePredicates) == 0 {
		c.Transitivity.TransitivePredicates = defaults.Transitivity.TransitivePredicates
	}
}

func (c *Config) applyReviewDefaults(defaults Config) {
	if c.Review.Workers == 0 {
		c.Review.Workers = defaults.Review.Workers
	}
	if c.Review.AutoApproveThreshold == 0 {
		c.Review.AutoApproveThreshold = defaults.Review.AutoApproveThreshold
	}
	if c.Review.AutoRejectThreshold == 0 {
		c.Review.AutoRejectThreshold = defaults.Review.AutoRejectThreshold
	}
	if c.Review.BatchSize == 0 {
		c.Review.BatchSize = defaults.Review.BatchSize
	}
	if c.Review.ReviewTimeout == 0 {
		c.Review.ReviewTimeout = defaults.Review.ReviewTimeout
	}
}

func (c *Config) applyVirtualEdgesDefaults(defaults Config) {
	// AutoApply defaults
	if c.VirtualEdges.AutoApply.MinConfidence == 0 {
		c.VirtualEdges.AutoApply.MinConfidence = defaults.VirtualEdges.AutoApply.MinConfidence
	}
	if c.VirtualEdges.AutoApply.PredicateTemplate == "" {
		c.VirtualEdges.AutoApply.PredicateTemplate = defaults.VirtualEdges.AutoApply.PredicateTemplate
	}

	// ReviewQueue defaults
	if c.VirtualEdges.ReviewQueue.MinConfidence == 0 {
		c.VirtualEdges.ReviewQueue.MinConfidence = defaults.VirtualEdges.ReviewQueue.MinConfidence
	}
	if c.VirtualEdges.ReviewQueue.MaxConfidence == 0 {
		c.VirtualEdges.ReviewQueue.MaxConfidence = defaults.VirtualEdges.ReviewQueue.MaxConfidence
	}
}

func (c *Config) applyStorageDefaults(defaults Config) {
	if c.Storage.BucketName == "" {
		c.Storage.BucketName = defaults.Storage.BucketName
	}
	if c.Storage.RetentionDays == 0 {
		c.Storage.RetentionDays = defaults.Storage.RetentionDays
	}
	if c.Storage.CleanupInterval == 0 {
		c.Storage.CleanupInterval = defaults.Storage.CleanupInterval
	}
}

// GetEnabledDetectors returns a list of enabled detector names
func (c *Config) GetEnabledDetectors() []string {
	var enabled []string
	if c.SemanticGap.Enabled {
		enabled = append(enabled, "semantic_gap")
	}
	if c.CoreAnomaly.Enabled {
		enabled = append(enabled, "core_anomaly")
	}
	if c.Transitivity.Enabled {
		enabled = append(enabled, "transitivity")
	}
	return enabled
}

// IsDetectorEnabled checks if a specific detector is enabled
func (c *Config) IsDetectorEnabled(detector string) bool {
	switch detector {
	case "semantic_gap":
		return c.SemanticGap.Enabled
	case "core_anomaly":
		return c.CoreAnomaly.Enabled
	case "transitivity":
		return c.Transitivity.Enabled
	default:
		return false
	}
}

// BuildPredicate generates the predicate string from the template based on confidence.
// Template supports {band} placeholder which is replaced with:
//   - "high" for confidence >= 0.95
//   - "medium" for confidence >= 0.85
//   - "related" for lower confidence scores
func (c *AutoApplyConfig) BuildPredicate(confidence float64) string {
	band := "related"
	if confidence >= 0.95 {
		band = "high"
	} else if confidence >= 0.85 {
		band = "medium"
	}

	return strings.Replace(c.PredicateTemplate, "{band}", band, 1)
}

// ShouldAutoApply returns true if the anomaly meets auto-apply criteria based on confidence.
// Confidence is a composite score (similarity + distance boost + core boost) capped at 1.0.
func (c *AutoApplyConfig) ShouldAutoApply(confidence float64) bool {
	if !c.Enabled {
		return false
	}
	return confidence >= c.MinConfidence
}

// ShouldQueue returns true if the anomaly should go to the review queue based on confidence.
// Anomalies with confidence >= MinConfidence but < MaxConfidence go to review.
func (c *ReviewQueueConfig) ShouldQueue(confidence float64) bool {
	if !c.Enabled {
		return false
	}
	return confidence >= c.MinConfidence && confidence < c.MaxConfidence
}

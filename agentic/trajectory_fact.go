package agentic

import (
	"crypto/sha256"
	"encoding/base32"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/c360studio/semstreams/message"
)

const (
	// TrajectoryBucketName is the immutable fact-log KV bucket.
	TrajectoryBucketName = "AGENT_TRAJECTORIES"
	// TrajectorySchemaV1 is the wire version for immutable trajectory facts and evidence.
	TrajectorySchemaV1 = "v1"
	// TrajectoryFactMaxBytes is the framework-owned upper bound for one fact envelope.
	TrajectoryFactMaxBytes        = 8 * 1024
	trajectoryPreviewMaxBytes     = 512
	trajectoryCorrelationMaxBytes = 128
)

// TrajectoryKind is the closed v1 observation vocabulary.
type TrajectoryKind string

const (
	TrajectoryKindLoopStarted      TrajectoryKind = "loop.started"
	TrajectoryKindModelRequested   TrajectoryKind = "model.requested"
	TrajectoryKindModelCompleted   TrajectoryKind = "model.completed"
	TrajectoryKindToolRequested    TrajectoryKind = "tool.requested"
	TrajectoryKindToolCompleted    TrajectoryKind = "tool.completed"
	TrajectoryKindContextCompacted TrajectoryKind = "context.compacted"
	TrajectoryKindLoopTerminal     TrajectoryKind = "loop.terminal"
)

// TrajectorySourceKind classifies optional correlation without making it identity.
type TrajectorySourceKind string

const (
	TrajectorySourceTask       TrajectorySourceKind = "task"
	TrajectorySourceRequest    TrajectorySourceKind = "request"
	TrajectorySourceToolCall   TrajectorySourceKind = "tool_call"
	TrajectorySourceSignal     TrajectorySourceKind = "signal"
	TrajectorySourceMessage    TrajectorySourceKind = "message"
	TrajectorySourceCompaction TrajectorySourceKind = "compaction"
)

// TrajectoryPhase gives causal ordering a stable rank independent of timestamps.
type TrajectoryPhase string

const (
	TrajectoryPhaseLoopStart    TrajectoryPhase = "loop_start"
	TrajectoryPhaseModelRequest TrajectoryPhase = "model_request"
	TrajectoryPhaseModelResult  TrajectoryPhase = "model_result"
	TrajectoryPhaseToolRequest  TrajectoryPhase = "tool_request"
	TrajectoryPhaseToolResult   TrajectoryPhase = "tool_result"
	TrajectoryPhaseCompaction   TrajectoryPhase = "compaction"
	TrajectoryPhaseTerminal     TrajectoryPhase = "terminal"
)

// TrajectoryStatus is bounded display metadata, not loop authority.
type TrajectoryStatus string

const (
	TrajectoryStatusRequested TrajectoryStatus = "requested"
	TrajectoryStatusCompleted TrajectoryStatus = "completed"
	TrajectoryStatusFailed    TrajectoryStatus = "failed"
	TrajectoryStatusCancelled TrajectoryStatus = "cancelled"
)

// TrajectoryErrorCategory is safe bounded metadata; raw errors stay in evidence.
type TrajectoryErrorCategory string

const (
	TrajectoryErrorUnknown    TrajectoryErrorCategory = "unknown"
	TrajectoryErrorModel      TrajectoryErrorCategory = "model"
	TrajectoryErrorTool       TrajectoryErrorCategory = "tool"
	TrajectoryErrorTimeout    TrajectoryErrorCategory = "timeout"
	TrajectoryErrorPermission TrajectoryErrorCategory = "permission"
	TrajectoryErrorValidation TrajectoryErrorCategory = "validation"
)

// TrajectoryEvidenceCapture reports whether the referenced body was durably verified.
type TrajectoryEvidenceCapture string

const (
	TrajectoryEvidenceNone    TrajectoryEvidenceCapture = "none"
	TrajectoryEvidenceStored  TrajectoryEvidenceCapture = "stored"
	TrajectoryEvidenceMissing TrajectoryEvidenceCapture = "missing"
)

// TrajectoryEvidenceFailure is the closed explanation for a missing body.
type TrajectoryEvidenceFailure string

const (
	TrajectoryEvidenceFailureProviderUnavailable TrajectoryEvidenceFailure = "provider_unavailable"
	TrajectoryEvidenceFailureRead                TrajectoryEvidenceFailure = "read_failed"
	TrajectoryEvidenceFailureWrite               TrajectoryEvidenceFailure = "write_failed"
	TrajectoryEvidenceFailureIntegrity           TrajectoryEvidenceFailure = "integrity_conflict"
)

// TrajectoryFactV1 is one immutable, finite observation. Bodies and arbitrary
// collections are deliberately absent and live only in TrajectoryEvidenceV1.
type TrajectoryFactV1 struct {
	SchemaVersion     string                    `json:"schema_version"`
	LoopDigest        string                    `json:"loop_digest"`
	AttemptID         string                    `json:"attempt_id"`
	AttemptOrdinal    uint64                    `json:"attempt_ordinal"`
	Kind              TrajectoryKind            `json:"kind"`
	SourceKind        TrajectorySourceKind      `json:"source_kind,omitempty"`
	SourceCorrelation string                    `json:"source_correlation,omitempty"`
	CausalIteration   uint32                    `json:"causal_iteration"`
	CausalPhase       TrajectoryPhase           `json:"causal_phase"`
	CausalOrdinal     uint32                    `json:"causal_ordinal"`
	ObservedAt        time.Time                 `json:"observed_at"`
	ElapsedMS         int64                     `json:"elapsed_ms,omitempty"`
	Status            TrajectoryStatus          `json:"status,omitempty"`
	TokensIn          uint64                    `json:"tokens_in,omitempty"`
	TokensOut         uint64                    `json:"tokens_out,omitempty"`
	MessageCount      uint32                    `json:"message_count,omitempty"`
	ToolCount         uint32                    `json:"tool_count,omitempty"`
	URLCount          uint32                    `json:"url_count,omitempty"`
	ModelPreview      string                    `json:"model_preview,omitempty"`
	ProviderPreview   string                    `json:"provider_preview,omitempty"`
	ToolPreview       string                    `json:"tool_preview,omitempty"`
	CapabilityPreview string                    `json:"capability_preview,omitempty"`
	ErrorCategory     TrajectoryErrorCategory   `json:"error_category,omitempty"`
	EvidenceDigest    string                    `json:"evidence_digest,omitempty"`
	EvidenceSize      uint64                    `json:"evidence_size,omitempty"`
	Evidence          *message.StorageReference `json:"evidence,omitempty"`
	EvidenceCapture   TrajectoryEvidenceCapture `json:"evidence_capture"`
	EvidenceFailure   TrajectoryEvidenceFailure `json:"evidence_failure,omitempty"`
}

// TrajectoryLoopDigest returns the lowercase SHA-256 digest used inside facts.
func TrajectoryLoopDigest(loopID string) string {
	sum := sha256.Sum256([]byte(loopID))
	return hex.EncodeToString(sum[:])
}

// TrajectoryFactPrefix returns the native filtered-list prefix for one loop.
func TrajectoryFactPrefix(loopID string) string {
	sum := sha256.Sum256([]byte(loopID))
	encoded := base32.StdEncoding.WithPadding(base32.NoPadding).EncodeToString(sum[:])
	return "v1." + strings.ToLower(encoded) + "."
}

// TrajectoryFactKey builds a bounded NATS-safe immutable fact key.
func TrajectoryFactKey(loopID, attemptID string) (string, error) {
	if loopID == "" {
		return "", fmt.Errorf("loop ID required")
	}
	if !validAttemptID(attemptID) {
		return "", fmt.Errorf("attempt ID must be 1-64 alphanumeric characters")
	}
	return TrajectoryFactPrefix(loopID) + attemptID, nil
}

// CanonicalBytes validates, bounds previews, and deterministically encodes the fact.
func (f TrajectoryFactV1) CanonicalBytes() ([]byte, error) {
	f.ModelPreview = boundedTrajectoryText(f.ModelPreview, trajectoryPreviewMaxBytes)
	f.ProviderPreview = boundedTrajectoryText(f.ProviderPreview, trajectoryPreviewMaxBytes)
	f.ToolPreview = boundedTrajectoryText(f.ToolPreview, trajectoryPreviewMaxBytes)
	f.CapabilityPreview = boundedTrajectoryText(f.CapabilityPreview, trajectoryPreviewMaxBytes)
	f.SourceCorrelation = boundedCorrelation(f.SourceCorrelation)
	f.ObservedAt = f.ObservedAt.UTC()
	if err := f.validate(); err != nil {
		return nil, err
	}
	encoded, err := json.Marshal(f)
	if err != nil {
		return nil, fmt.Errorf("encode trajectory fact: %w", err)
	}
	if len(encoded) >= TrajectoryFactMaxBytes {
		return nil, fmt.Errorf("trajectory fact size %d exceeds internal bound", len(encoded))
	}
	return encoded, nil
}

func (f TrajectoryFactV1) validate() error {
	if f.SchemaVersion != TrajectorySchemaV1 {
		return fmt.Errorf("schema_version must be %q", TrajectorySchemaV1)
	}
	if len(f.LoopDigest) != sha256.Size*2 {
		return fmt.Errorf("loop_digest must be sha256")
	}
	if _, err := hex.DecodeString(f.LoopDigest); err != nil {
		return fmt.Errorf("loop_digest must be sha256: %w", err)
	}
	if !validAttemptID(f.AttemptID) || f.AttemptOrdinal == 0 {
		return fmt.Errorf("attempt identity and positive ordinal required")
	}
	if !f.Kind.known() || !f.CausalPhase.known() || f.ObservedAt.IsZero() {
		return fmt.Errorf("known kind, causal phase, and observed_at required")
	}
	if f.SourceKind != "" && !f.SourceKind.known() {
		return fmt.Errorf("unknown source_kind %q", f.SourceKind)
	}
	if f.Status != "" && !f.Status.known() {
		return fmt.Errorf("unknown status %q", f.Status)
	}
	if f.ErrorCategory != "" && !f.ErrorCategory.known() {
		return fmt.Errorf("unknown error_category %q", f.ErrorCategory)
	}
	if !f.EvidenceCapture.known() {
		return fmt.Errorf("unknown evidence_capture %q", f.EvidenceCapture)
	}
	if f.EvidenceFailure != "" && !f.EvidenceFailure.known() {
		return fmt.Errorf("unknown evidence_failure %q", f.EvidenceFailure)
	}
	if f.EvidenceDigest != "" {
		if len(f.EvidenceDigest) != sha256.Size*2 {
			return fmt.Errorf("evidence_digest must be sha256")
		}
		if _, err := hex.DecodeString(f.EvidenceDigest); err != nil {
			return fmt.Errorf("evidence_digest must be sha256: %w", err)
		}
	}
	if f.EvidenceCapture == TrajectoryEvidenceStored && (f.Evidence == nil || f.EvidenceDigest == "" || f.EvidenceSize == 0) {
		return fmt.Errorf("stored evidence requires digest, size, and reference")
	}
	if f.EvidenceCapture == TrajectoryEvidenceMissing && f.Evidence != nil {
		return fmt.Errorf("missing evidence cannot carry a reference")
	}
	return nil
}

func validAttemptID(value string) bool {
	if len(value) == 0 || len(value) > 64 {
		return false
	}
	for _, r := range value {
		if !((r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9')) {
			return false
		}
	}
	return true
}

func boundedCorrelation(value string) string {
	if len(value) <= trajectoryCorrelationMaxBytes {
		return value
	}
	sum := sha256.Sum256([]byte(value))
	return "sha256:" + hex.EncodeToString(sum[:])
}

func boundedTrajectoryText(value string, maxBytes int) string {
	if len(value) <= maxBytes {
		return value
	}
	value = value[:maxBytes]
	for !utf8.ValidString(value) {
		value = value[:len(value)-1]
	}
	return value
}

func (k TrajectoryKind) known() bool {
	switch k {
	case TrajectoryKindLoopStarted, TrajectoryKindModelRequested, TrajectoryKindModelCompleted,
		TrajectoryKindToolRequested, TrajectoryKindToolCompleted, TrajectoryKindContextCompacted,
		TrajectoryKindLoopTerminal:
		return true
	default:
		return false
	}
}

func (k TrajectorySourceKind) known() bool {
	switch k {
	case TrajectorySourceTask, TrajectorySourceRequest, TrajectorySourceToolCall,
		TrajectorySourceSignal, TrajectorySourceMessage, TrajectorySourceCompaction:
		return true
	default:
		return false
	}
}

func (p TrajectoryPhase) known() bool { return trajectoryPhaseRank(p) >= 0 }

func (s TrajectoryStatus) known() bool {
	switch s {
	case TrajectoryStatusRequested, TrajectoryStatusCompleted, TrajectoryStatusFailed, TrajectoryStatusCancelled:
		return true
	default:
		return false
	}
}

func (c TrajectoryErrorCategory) known() bool {
	switch c {
	case TrajectoryErrorUnknown, TrajectoryErrorModel, TrajectoryErrorTool, TrajectoryErrorTimeout,
		TrajectoryErrorPermission, TrajectoryErrorValidation:
		return true
	default:
		return false
	}
}

func (c TrajectoryEvidenceCapture) known() bool {
	return c == TrajectoryEvidenceNone || c == TrajectoryEvidenceStored || c == TrajectoryEvidenceMissing
}

func (f TrajectoryEvidenceFailure) known() bool {
	return f == TrajectoryEvidenceFailureProviderUnavailable || f == TrajectoryEvidenceFailureRead ||
		f == TrajectoryEvidenceFailureWrite || f == TrajectoryEvidenceFailureIntegrity
}

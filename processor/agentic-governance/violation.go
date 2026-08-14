package agenticgovernance

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"log/slog"
	"time"

	"github.com/c360studio/semstreams/component"
	"github.com/c360studio/semstreams/natsclient"
	"github.com/c360studio/semstreams/pkg/errs"
)

// Violation represents a detected policy violation
type Violation struct {
	// ID is unique violation identifier
	ID string `json:"violation_id"`

	// FilterName indicates which filter detected violation
	FilterName string `json:"filter_type"`

	// Severity indicates threat/impact level
	Severity Severity `json:"severity"`

	// Confidence in detection (0.0-1.0)
	Confidence float64 `json:"confidence"`

	// Timestamp when violation occurred
	Timestamp time.Time `json:"timestamp"`

	// UserID of the violating user
	UserID string `json:"user_id"`

	// SessionID of the session
	SessionID string `json:"session_id"`

	// ChannelID where violation occurred
	ChannelID string `json:"channel_id"`

	// OriginalContent is the content that violated policy (redacted for audit)
	OriginalContent string `json:"original_content,omitempty"`

	// Details contains filter-specific violation information
	Details map[string]any `json:"details,omitempty"`

	// Action taken in response
	Action ViolationAction `json:"action_taken"`

	// Metadata for context
	Metadata map[string]any `json:"metadata,omitempty"`
}

// ViolationAction describes how violation was handled
type ViolationAction string

// Violation actions define the response taken for a detected violation.
const (
	ViolationActionBlocked  ViolationAction = "blocked"
	ViolationActionRedacted ViolationAction = "redacted"
	ViolationActionFlagged  ViolationAction = "flagged"
	ViolationActionLogged   ViolationAction = "logged"
)

// ViolationHandler processes detected violations. It holds a copy of the
// component's output port definitions so the violation event can honor its
// accepted port override.
type ViolationHandler struct {
	config     ViolationConfig
	natsClient *natsclient.Client
	logger     *slog.Logger
	metrics    *governanceMetrics
	outputs    []component.PortDefinition
}

// NewViolationHandler creates a new violation handler. outputs must be the
// component's output port slice so the violation event resolves via port
// config rather than a hardcoded subject.
func NewViolationHandler(config ViolationConfig, nc *natsclient.Client, logger *slog.Logger, metrics *governanceMetrics, outputs []component.PortDefinition) *ViolationHandler {
	return &ViolationHandler{
		config:     config,
		natsClient: nc,
		logger:     logger,
		metrics:    metrics,
		outputs:    outputs,
	}
}

// Handle processes a violation. Shadow-mode violations (Action ==
// ViolationActionFlagged, used by ADR-043 Phase 2 classifier shadow
// mode) still emit the audit event + publish so observability and
// downstream rule consumers can see the verdict, but skip admin alerts —
// operators are not yet ready to act on shadow-mode results.
func (h *ViolationHandler) Handle(ctx context.Context, violation *Violation) error {
	shadow := violation.Action == ViolationActionFlagged

	// Record metrics
	if h.metrics != nil {
		h.metrics.recordViolation(violation.FilterName, violation.Severity)
	}

	// Log the violation
	logFn := h.logger.Warn
	if shadow {
		// Shadow verdicts are info-level: operators expect them
		// during calibration and they don't pollute Warn-watchers.
		logFn = h.logger.Info
	}
	logFn("Policy violation detected",
		"violation_id", violation.ID,
		"filter", violation.FilterName,
		"severity", violation.Severity,
		"user_id", violation.UserID,
		"action", violation.Action,
		"shadow", shadow,
	)

	// Skip NATS operations if client is nil (unit testing)
	if h.natsClient == nil {
		return nil
	}

	// Store in KV if configured. Shadow-mode verdicts still land
	// in the audit store — the whole point of calibration is to
	// review what would have been blocked.
	if h.config.Store != "" {
		if err := h.storeViolation(ctx, violation); err != nil {
			h.logger.Error("Failed to store violation", "error", err, "violation_id", violation.ID)
			// Don't fail on storage errors; continue with remaining audit paths.
		}
	}

	if !shadow {
		// Alert admins if severity warrants. Skipped in shadow
		// mode — admin paging on calibration noise defeats the
		// purpose.
		if h.shouldAlertAdmin(violation.Severity) {
			if err := h.alertAdmin(ctx, violation); err != nil {
				h.logger.Error("Failed to alert admin", "error", err, "violation_id", violation.ID)
			}
		}
	}

	// Publish violation event via output port (defaults to
	// governance.violation.{filter}.{user} when no port override is set).
	subject, err := component.ResolveSubject(h.outputs, "violations", violation.FilterName+"."+violation.UserID)
	if err != nil {
		return errs.WrapInvalid(err, "ViolationHandler", "Handle", "resolve violation subject")
	}
	violationJSON, err := json.Marshal(violation)
	if err != nil {
		return errs.Wrap(err, "ViolationHandler", "Handle", "marshal violation")
	}

	if err := h.natsClient.Publish(ctx, subject, violationJSON); err != nil {
		return errs.WrapTransient(err, "ViolationHandler", "Handle", "publish violation")
	}

	return nil
}

// storeViolation saves to KV bucket
func (h *ViolationHandler) storeViolation(ctx context.Context, violation *Violation) error {
	key := fmt.Sprintf("violation.%s", violation.ID)
	if err := natsclient.ValidateKVLiteralKey(key); err != nil {
		return errs.WrapInvalid(err, "ViolationHandler", "storeViolation", "validate violation audit key")
	}
	kv, err := h.natsClient.GetKeyValueBucket(ctx, h.config.Store)
	if err != nil {
		return errs.WrapTransient(err, "ViolationHandler", "storeViolation", "get KV bucket")
	}

	value, err := json.Marshal(violation)
	if err != nil {
		return errs.Wrap(err, "ViolationHandler", "storeViolation", "marshal violation")
	}

	_, err = kv.Put(ctx, key, value)
	return err
}

// alertAdmin sends notification to administrators
func (h *ViolationHandler) alertAdmin(ctx context.Context, violation *Violation) error {
	alert := map[string]any{
		"type":         "governance_alert",
		"timestamp":    violation.Timestamp,
		"violation_id": violation.ID,
		"filter":       violation.FilterName,
		"severity":     violation.Severity,
		"confidence":   violation.Confidence,
		"user_id":      violation.UserID,
		"session_id":   violation.SessionID,
		"details":      violation.Details,
	}

	alertJSON, err := json.Marshal(alert)
	if err != nil {
		return errs.Wrap(err, "ViolationHandler", "alertAdmin", "marshal alert")
	}

	subject := h.config.AdminSubject
	if subject == "" {
		subject = "admin.governance.alert"
	}

	return errs.WrapTransient(h.natsClient.Publish(ctx, subject, alertJSON), "ViolationHandler", "alertAdmin", "publish alert")
}

// shouldAlertAdmin checks if severity requires admin notification
func (h *ViolationHandler) shouldAlertAdmin(severity Severity) bool {
	for _, s := range h.config.NotifyAdminSeverity {
		if s == severity {
			return true
		}
	}
	return false
}

// GenerateViolationID creates a unique violation ID
func GenerateViolationID() string {
	b := make([]byte, 8)
	if _, err := rand.Read(b); err != nil {
		// Fallback to timestamp-based ID
		return fmt.Sprintf("viol_%d", time.Now().UnixNano())
	}
	return "viol_" + hex.EncodeToString(b)
}

// NewViolation creates a new violation with common fields populated
func NewViolation(filterName string, severity Severity, msg *Message) *Violation {
	return &Violation{
		ID:         GenerateViolationID(),
		FilterName: filterName,
		Severity:   severity,
		Confidence: 1.0,
		Timestamp:  time.Now(),
		UserID:     msg.UserID,
		SessionID:  msg.SessionID,
		ChannelID:  msg.ChannelID,
		Details:    make(map[string]any),
		Metadata:   make(map[string]any),
	}
}

// WithAction sets the action on the violation
func (v *Violation) WithAction(action ViolationAction) *Violation {
	v.Action = action
	return v
}

// WithConfidence sets the confidence on the violation
func (v *Violation) WithConfidence(confidence float64) *Violation {
	v.Confidence = confidence
	return v
}

// WithDetail adds a detail to the violation
func (v *Violation) WithDetail(key string, value any) *Violation {
	if v.Details == nil {
		v.Details = make(map[string]any)
	}
	v.Details[key] = value
	return v
}

// WithOriginalContent sets the original content (should be redacted for audit)
func (v *Violation) WithOriginalContent(content string) *Violation {
	// Truncate long content for audit
	if len(content) > 500 {
		content = content[:500] + "...[TRUNCATED]"
	}
	v.OriginalContent = content
	return v
}

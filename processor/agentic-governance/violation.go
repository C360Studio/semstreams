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
// component's output port definitions so publish subjects can honor port
// overrides — mirroring what every other agentic component does for its
// outputs.
type ViolationHandler struct {
	config     ViolationConfig
	natsClient *natsclient.Client
	logger     *slog.Logger
	metrics    *governanceMetrics
	outputs    []component.PortDefinition
}

// NewViolationHandler creates a new violation handler. outputs must be the
// component's output port slice so the violations and user_errors publish
// subjects resolve via port config rather than hardcoded fmt.Sprintf.
func NewViolationHandler(config ViolationConfig, nc *natsclient.Client, logger *slog.Logger, metrics *governanceMetrics, outputs []component.PortDefinition) *ViolationHandler {
	return &ViolationHandler{
		config:     config,
		natsClient: nc,
		logger:     logger,
		metrics:    metrics,
		outputs:    outputs,
	}
}

// Handle processes a violation
func (h *ViolationHandler) Handle(ctx context.Context, violation *Violation) error {
	// Record metrics
	if h.metrics != nil {
		h.metrics.recordViolation(violation.FilterName, violation.Severity)
	}

	// Log the violation
	h.logger.Warn("Policy violation detected",
		"violation_id", violation.ID,
		"filter", violation.FilterName,
		"severity", violation.Severity,
		"user_id", violation.UserID,
		"action", violation.Action,
	)

	// Skip NATS operations if client is nil (unit testing)
	if h.natsClient == nil {
		return nil
	}

	// Store in KV if configured
	if h.config.Store != "" {
		if err := h.storeViolation(ctx, violation); err != nil {
			h.logger.Error("Failed to store violation", "error", err, "violation_id", violation.ID)
			// Don't fail on storage errors, continue with notifications
		}
	}

	// Notify user if configured
	if h.config.NotifyUser {
		if err := h.notifyUser(ctx, violation); err != nil {
			h.logger.Error("Failed to notify user", "error", err, "violation_id", violation.ID)
		}
	}

	// Alert admins if severity warrants
	if h.shouldAlertAdmin(violation.Severity) {
		if err := h.alertAdmin(ctx, violation); err != nil {
			h.logger.Error("Failed to alert admin", "error", err, "violation_id", violation.ID)
		}
	}

	// Publish violation event via output port (defaults to
	// governance.violation.{filter}.{user} when no port override is set).
	subject := component.ResolveSubject(h.outputs, "violations", violation.FilterName+"."+violation.UserID)
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
	kv, err := h.natsClient.GetKeyValueBucket(ctx, h.config.Store)
	if err != nil {
		return errs.WrapTransient(err, "ViolationHandler", "storeViolation", "get KV bucket")
	}

	key := fmt.Sprintf("violation:%s", violation.ID)
	value, err := json.Marshal(violation)
	if err != nil {
		return errs.Wrap(err, "ViolationHandler", "storeViolation", "marshal violation")
	}

	_, err = kv.Put(ctx, key, value)
	return err
}

// notifyUser sends error message to user
func (h *ViolationHandler) notifyUser(ctx context.Context, violation *Violation) error {
	notification := map[string]any{
		"type":      "error",
		"timestamp": violation.Timestamp,
		"message":   h.formatUserMessage(violation),
		"severity":  violation.Severity,
		"details": map[string]any{
			"violation_id": violation.ID,
			"filter":       violation.FilterName,
		},
	}

	notificationJSON, err := json.Marshal(notification)
	if err != nil {
		return errs.Wrap(err, "ViolationHandler", "notifyUser", "marshal notification")
	}

	// User-notification subject resolved via output port; defaults to
	// user.response.{channel}.{user} when no port override is set.
	subject := component.ResolveSubject(h.outputs, "user_errors", violation.ChannelID+"."+violation.UserID)
	return errs.WrapTransient(h.natsClient.Publish(ctx, subject, notificationJSON), "ViolationHandler", "notifyUser", "publish notification")
}

// formatUserMessage creates user-friendly error message
func (h *ViolationHandler) formatUserMessage(violation *Violation) string {
	switch violation.FilterName {
	case "pii_redaction":
		return "Your message contained sensitive information that was automatically redacted."
	case "injection_detection":
		return "Your message was blocked due to detected security concerns. Please rephrase your request."
	case "content_moderation":
		return "Your message violates content policy and cannot be processed."
	case "rate_limiting":
		return "Rate limit exceeded. Please wait before sending more requests."
	default:
		return "Your message could not be processed due to policy restrictions."
	}
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

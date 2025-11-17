// Package health provides health monitoring functionality for components and systems
package health

import (
	"regexp"
	"strings"
	"time"

	"github.com/c360/semstreams/component"
)

// Pre-compiled regexes for error message sanitization (performance optimization)
var (
	httpURLRegex     = regexp.MustCompile(`https?://[^\s]+`)
	natsURLRegex     = regexp.MustCompile(`nats://[^\s]+`)
	wsURLRegex       = regexp.MustCompile(`wss?://[^\s]+`)
	unixPathRegex    = regexp.MustCompile(`/[a-zA-Z0-9/_.-]+`)
	windowsPathRegex = regexp.MustCompile(`[A-Z]:\\[^:\s]+`)
	ipAddrRegex      = regexp.MustCompile(`\b\d{1,3}\.\d{1,3}\.\d{1,3}\.\d{1,3}\b`)
	portRegex        = regexp.MustCompile(`:\d{2,5}\b`)
	credentialRegex  = regexp.MustCompile(`(?i)(password|token|key|secret|credential)[^a-zA-Z]*[:=][^,\s}]+`)
)

// Status represents the health state of a component or system
type Status struct {
	Component   string    `json:"component"`
	Healthy     bool      `json:"healthy"` // true if status is "healthy"
	Status      string    `json:"status"`  // "healthy", "unhealthy", "degraded"
	Message     string    `json:"message"`
	Timestamp   time.Time `json:"timestamp"`
	SubStatuses []Status  `json:"sub_statuses,omitempty"`
	Metrics     *Metrics  `json:"metrics,omitempty"`
}

// Metrics contains health-related metrics
type Metrics struct {
	Uptime            time.Duration `json:"uptime"`
	ErrorCount        int           `json:"error_count"`
	MessagesProcessed int64         `json:"messages_processed,omitempty"`
	LastActivity      time.Time     `json:"last_activity,omitempty"`
}

// IsHealthy returns true if the status is healthy
func (s Status) IsHealthy() bool {
	return s.Status == "healthy"
}

// IsDegraded returns true if the status is degraded
func (s Status) IsDegraded() bool {
	return s.Status == "degraded"
}

// IsUnhealthy returns true if the status is unhealthy
func (s Status) IsUnhealthy() bool {
	return s.Status == "unhealthy"
}

// WithMetrics returns a copy of the status with metrics attached
func (s Status) WithMetrics(metrics *Metrics) Status {
	s.Metrics = metrics
	return s
}

// WithSubStatus adds a sub-status and returns a copy
func (s Status) WithSubStatus(subStatus Status) Status {
	// Create a new slice to avoid sharing the underlying array
	newSubStatuses := make([]Status, len(s.SubStatuses), len(s.SubStatuses)+1)
	copy(newSubStatuses, s.SubStatuses)
	s.SubStatuses = append(newSubStatuses, subStatus)
	return s
}

// sanitizeErrorMessage removes potentially sensitive information from error messages.
// This function is called automatically by FromComponentHealth to prevent accidental
// exposure of sensitive data in health status messages.
//
// Sanitization patterns:
//   - URLs (http://, https://, nats://, ws://, wss://) → [URL]
//   - File paths (Unix: /path/to/file, Windows: C:\path\to\file) → [PATH]
//   - IP addresses (192.168.1.100) → [IP]
//   - Port numbers (:8080) → [PORT]
//   - Credentials (password=X, token=X, key=X, secret=X) → [REDACTED]
func sanitizeErrorMessage(err string) string {
	if err == "" {
		return ""
	}

	sanitized := err

	// Remove URLs first (before paths, as they contain paths)
	sanitized = httpURLRegex.ReplaceAllString(sanitized, "[URL]")
	sanitized = natsURLRegex.ReplaceAllString(sanitized, "[URL]")
	sanitized = wsURLRegex.ReplaceAllString(sanitized, "[URL]")

	// Remove file paths (Unix and Windows)
	sanitized = unixPathRegex.ReplaceAllString(sanitized, "[PATH]")
	sanitized = windowsPathRegex.ReplaceAllString(sanitized, "[PATH]")

	// Remove IP addresses
	sanitized = ipAddrRegex.ReplaceAllString(sanitized, "[IP]")

	// Remove port numbers
	sanitized = portRegex.ReplaceAllString(sanitized, "[PORT]")

	// Remove potential credentials (basic patterns) - check against lowercase but replace in original case
	lowerSanitized := strings.ToLower(sanitized)
	if strings.Contains(lowerSanitized, "password") || strings.Contains(lowerSanitized, "token") ||
		strings.Contains(lowerSanitized, "key") || strings.Contains(lowerSanitized, "secret") ||
		strings.Contains(lowerSanitized, "credential") {
		sanitized = credentialRegex.ReplaceAllString(sanitized, "[REDACTED]")
	}

	return sanitized
}

// FromComponentHealth converts a component.HealthStatus to a health.Status
func FromComponentHealth(name string, ch component.HealthStatus) Status {
	status := "unhealthy"
	if ch.Healthy {
		status = "healthy"
	}

	message := "Component healthy"
	if ch.LastError != "" {
		message = sanitizeErrorMessage(ch.LastError)
	}

	metrics := &Metrics{
		Uptime:       ch.Uptime,
		ErrorCount:   ch.ErrorCount,
		LastActivity: ch.LastCheck,
	}

	return Status{
		Component: name,
		Healthy:   ch.Healthy,
		Status:    status,
		Message:   message,
		Timestamp: time.Now(),
		Metrics:   metrics,
	}
}

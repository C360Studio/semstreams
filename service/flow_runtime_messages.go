package service

import (
	"context"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/c360/semstreams/errors"
	"github.com/c360/semstreams/flowstore"
)

// RuntimeMessage represents a formatted message entry for UI consumption
type RuntimeMessage struct {
	Timestamp   string         `json:"timestamp"`
	Subject     string         `json:"subject"`
	MessageID   string         `json:"message_id"`
	Component   string         `json:"component"`
	Direction   string         `json:"direction"`
	Summary     string         `json:"summary"`
	Metadata    map[string]any `json:"metadata,omitempty"`
	MessageType string         `json:"message_type,omitempty"`
}

// RuntimeMessagesResponse represents the response structure for runtime messages
type RuntimeMessagesResponse struct {
	Timestamp string           `json:"timestamp"`
	Messages  []RuntimeMessage `json:"messages"`
	Total     int              `json:"total"`
	Limit     int              `json:"limit"`
	Note      string           `json:"note,omitempty"`
}

// handleRuntimeMessages handles GET /flowbuilder/flows/{id}/runtime/messages
// Returns filtered message logger entries for the flow's components
func (fs *FlowService) handleRuntimeMessages(w http.ResponseWriter, r *http.Request) {
	// Extract flow ID from path
	flowID := r.PathValue("id")
	if flowID == "" {
		fs.writeJSONError(w, "Missing flow ID", http.StatusBadRequest)
		return
	}

	// Create context with 90ms timeout (target <100ms response)
	ctx, cancel := context.WithTimeout(r.Context(), 90*time.Millisecond)
	defer cancel()

	// Parse limit parameter (default 100, max 1000)
	limit := 100
	if limitStr := r.URL.Query().Get("limit"); limitStr != "" {
		if parsedLimit, err := strconv.Atoi(limitStr); err == nil {
			limit = parsedLimit
			if limit < 1 {
				limit = 100
			} else if limit > 1000 {
				limit = 1000
			}
		}
	}

	// Get flow definition to determine component subjects
	flow, err := fs.flowStore.Get(ctx, flowID)
	if err != nil {
		wrappedErr := errors.WrapTransient(err, "FlowService", "handleRuntimeMessages", "get flow")
		fs.logger.Error("Failed to get flow for runtime messages",
			"flow_id", flowID,
			"error", wrappedErr)
		fs.writeJSONError(w, "Flow not found", http.StatusNotFound)
		return
	}

	// Generate subject patterns for flow components
	subjects := getFlowMessageSubjects(flow)

	// Get message logger service
	msgLoggerSvc, exists := fs.serviceMgr.GetService("message-logger")
	if !exists {
		// Message logger unavailable - return empty response with note
		fs.writeJSON(w, RuntimeMessagesResponse{
			Timestamp: time.Now().UTC().Format(time.RFC3339Nano),
			Messages:  []RuntimeMessage{},
			Total:     0,
			Limit:     limit,
			Note:      "Message logger service is not available",
		})
		return
	}

	// Type assert to MessageLogger
	msgLogger, ok := msgLoggerSvc.(*MessageLogger)
	if !ok {
		// This is a programming error - service registry has wrong type
		err := fmt.Errorf("message logger has unexpected type %T", msgLoggerSvc)
		wrappedErr := errors.WrapFatal(err, "FlowService", "handleRuntimeMessages", "type assertion")
		fs.logger.Error("Message logger service has unexpected type",
			"flow_id", flowID,
			"error", wrappedErr)
		fs.writeJSON(w, RuntimeMessagesResponse{
			Timestamp: time.Now().UTC().Format(time.RFC3339Nano),
			Messages:  []RuntimeMessage{},
			Total:     0,
			Limit:     limit,
			Note:      "Message logger service unavailable",
		})
		return
	}

	// Get all log entries
	allEntries := msgLogger.GetLogEntries(0)

	// Filter entries by flow subjects
	filteredEntries := filterEntriesBySubjects(allEntries, subjects)

	// Limit results
	if len(filteredEntries) > limit {
		filteredEntries = filteredEntries[:limit]
	}

	// Format entries for UI
	messages := formatMessageEntries(filteredEntries, flow)

	// Build response
	response := RuntimeMessagesResponse{
		Timestamp: time.Now().UTC().Format(time.RFC3339Nano),
		Messages:  messages,
		Total:     len(messages),
		Limit:     limit,
	}

	fs.writeJSON(w, response)
}

// getFlowMessageSubjects generates NATS subject patterns for all components in a flow
func getFlowMessageSubjects(flow *flowstore.Flow) []string {
	var subjects []string

	for _, node := range flow.Nodes {
		// Component type determines subject prefix
		prefix := getSubjectPrefix(node.Type)
		// Use NATS wildcard '>' for hierarchical matching
		subject := fmt.Sprintf("%s.%s.>", prefix, node.Name)
		subjects = append(subjects, subject)
	}

	return subjects
}

// getSubjectPrefix maps component type to NATS subject prefix
func getSubjectPrefix(componentType string) string {
	// Normalize component type to lowercase for matching
	ct := strings.ToLower(componentType)

	// Check output patterns first (more specific)
	if strings.Contains(ct, "output") || strings.Contains(ct, "sink") ||
		strings.Contains(ct, "publish") || strings.Contains(ct, "writer") {
		return "output"
	}

	// Check for common processor component patterns
	if strings.Contains(ct, "processor") || strings.Contains(ct, "filter") ||
		strings.Contains(ct, "transform") || strings.Contains(ct, "graph") ||
		strings.Contains(ct, "json") || strings.Contains(ct, "semantic") {
		return "process"
	}

	// Check for common input component patterns (checked last since protocols like http/mqtt can be input or output)
	if strings.Contains(ct, "input") || strings.Contains(ct, "source") ||
		strings.Contains(ct, "udp") || strings.Contains(ct, "tcp") ||
		strings.Contains(ct, "http") || strings.Contains(ct, "mqtt") {
		return "input"
	}

	// Default to events for unknown types
	return "events"
}

// filterEntriesBySubjects filters log entries by matching subjects
func filterEntriesBySubjects(entries []MessageLogEntry, subjects []string) []MessageLogEntry {
	if len(subjects) == 0 {
		return entries
	}

	var filtered []MessageLogEntry
	for _, entry := range entries {
		if matchesAnySubject(entry.Subject, subjects) {
			filtered = append(filtered, entry)
		}
	}

	return filtered
}

// matchesAnySubject checks if a subject matches any of the patterns
func matchesAnySubject(subject string, patterns []string) bool {
	for _, pattern := range patterns {
		if matchesSubject(subject, pattern) {
			return true
		}
	}
	return false
}

// matchesSubject checks if a subject matches a NATS wildcard pattern
// Supports '>' for hierarchical matching (e.g., "input.udp.>" matches "input.udp.data")
func matchesSubject(subject, pattern string) bool {
	// Handle '>' wildcard (matches everything from this level down)
	if strings.HasSuffix(pattern, ".>") {
		prefix := strings.TrimSuffix(pattern, ".>")
		return strings.HasPrefix(subject, prefix+".")
	}

	// Handle '*' wildcard (matches single token) - for future extension
	if strings.Contains(pattern, "*") {
		// Simple implementation: split and match tokens
		subjectParts := strings.Split(subject, ".")
		patternParts := strings.Split(pattern, ".")

		if len(subjectParts) != len(patternParts) {
			return false
		}

		for i := range patternParts {
			if patternParts[i] != "*" && patternParts[i] != subjectParts[i] {
				return false
			}
		}
		return true
	}

	// Exact match
	return subject == pattern
}

// formatMessageEntries converts log entries to UI-friendly format
func formatMessageEntries(entries []MessageLogEntry, flow *flowstore.Flow) []RuntimeMessage {
	messages := make([]RuntimeMessage, 0, len(entries))

	// Build component name map for quick lookup
	componentMap := make(map[string]string) // subject prefix -> component name
	for _, node := range flow.Nodes {
		prefix := getSubjectPrefix(node.Type)
		key := fmt.Sprintf("%s.%s", prefix, node.Name)
		componentMap[key] = node.Name
	}

	for _, entry := range entries {
		// Extract component name from subject
		component := extractComponentFromSubject(entry.Subject, componentMap)

		// Default direction to "published" if not specified
		direction := "published"
		if dirVal, ok := entry.Metadata["direction"]; ok {
			if dirStr, ok := dirVal.(string); ok {
				direction = dirStr
			}
		}

		// Build runtime message
		msg := RuntimeMessage{
			Timestamp:   entry.Timestamp.UTC().Format(time.RFC3339Nano),
			Subject:     entry.Subject,
			MessageID:   entry.MessageID,
			Component:   component,
			Direction:   direction,
			Summary:     entry.Summary,
			MessageType: entry.MessageType,
		}

		// Copy metadata if present
		if len(entry.Metadata) > 0 {
			// Pre-allocate with expected capacity (all entries minus direction)
			msg.Metadata = make(map[string]any, len(entry.Metadata)-1)
			for k, v := range entry.Metadata {
				// Skip direction since we already extracted it
				if k != "direction" {
					msg.Metadata[k] = v
				}
			}
		}

		messages = append(messages, msg)
	}

	return messages
}

// extractComponentFromSubject extracts the component name from a NATS subject
// Example: "process.json-processor.data" -> "json-processor"
func extractComponentFromSubject(subject string, componentMap map[string]string) string {
	parts := strings.SplitN(subject, ".", 3)
	if len(parts) < 2 {
		return "unknown"
	}

	// Try to match prefix.component
	key := fmt.Sprintf("%s.%s", parts[0], parts[1])
	if component, exists := componentMap[key]; exists {
		return component
	}

	// Fallback to second part of subject
	return parts[1]
}

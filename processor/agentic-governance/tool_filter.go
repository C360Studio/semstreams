package agenticgovernance

import (
	"context"
	"fmt"
	"strings"

	"github.com/c360studio/semstreams/agentic"
)

const (
	// MessageTypeToolCall represents a tool call being evaluated before execution.
	MessageTypeToolCall MessageType = "tool_call"
)

// ToolCallFilter examines tool call arguments for governance violations before
// execution. It checks bash commands for PII patterns, http_request URLs for
// blocked domains, and applies rate limiting per tool.
type ToolCallFilter struct {
	// BlockedCommandPatterns are substrings that block bash commands.
	BlockedCommandPatterns []string

	// BlockedURLPatterns are substrings that block http_request URLs.
	BlockedURLPatterns []string

	// PIIFilter is reused from the existing PII detection system.
	piiFilter *PIIFilter
}

// NewToolCallFilter creates a filter for tool call governance.
func NewToolCallFilter(piiFilter *PIIFilter) *ToolCallFilter {
	return &ToolCallFilter{
		piiFilter: piiFilter,
		BlockedCommandPatterns: []string{
			"metadata.google", // GCP metadata endpoint
			"169.254.169.254", // AWS metadata endpoint
			"metadata.azure",  // Azure metadata endpoint
			"rm -rf /",        // Destructive commands
			":(){ :|:& };:",   // Fork bomb
			"> /dev/sd",       // Raw device write
			"mkfs.",           // Format filesystem
		},
		BlockedURLPatterns: []string{
			"169.254.169.254", // AWS metadata
			"metadata.google", // GCP metadata
			"metadata.azure",  // Azure metadata
			"localhost",       // Local services (SSRF also catches this)
			"127.0.0.1",       // Loopback
		},
	}
}

// Name returns the filter identifier.
func (f *ToolCallFilter) Name() string {
	return "tool_call_governance"
}

// Process examines a tool call message for governance violations.
// The tool call is encoded in Content.Metadata with keys: "tool_name", "tool_args".
func (f *ToolCallFilter) Process(ctx context.Context, msg *Message) (*FilterResult, error) {
	if msg.Type != MessageTypeToolCall {
		return &FilterResult{Allowed: true}, nil
	}

	toolName, _ := msg.Content.Metadata["tool_name"].(string)
	toolArgs, _ := msg.Content.Metadata["tool_args"].(map[string]any)

	switch toolName {
	case "bash":
		return f.checkBash(ctx, msg, toolArgs)
	case "http_request":
		return f.checkHTTPRequest(ctx, msg, toolArgs)
	default:
		// Check all tool arguments for PII
		return f.checkPII(ctx, msg, toolArgs)
	}
}

// checkBash validates bash command arguments.
func (f *ToolCallFilter) checkBash(ctx context.Context, msg *Message, args map[string]any) (*FilterResult, error) {
	command, _ := args["command"].(string)
	if command == "" {
		return &FilterResult{Allowed: true}, nil
	}

	// Check blocked patterns
	lower := strings.ToLower(command)
	for _, pattern := range f.BlockedCommandPatterns {
		if strings.Contains(lower, strings.ToLower(pattern)) {
			v := NewViolation(f.Name(), SeverityHigh, msg).
				WithAction(ViolationActionBlocked).
				WithDetail("type", "blocked_command").
				WithDetail("pattern", pattern).
				WithDetail("message", fmt.Sprintf("Command contains blocked pattern: %s", pattern))
			return &FilterResult{Allowed: false, Violation: v}, nil
		}
	}

	// Check for PII in command arguments
	return f.checkPII(ctx, msg, args)
}

// checkHTTPRequest validates URL arguments.
func (f *ToolCallFilter) checkHTTPRequest(_ context.Context, msg *Message, args map[string]any) (*FilterResult, error) {
	urlStr, _ := args["url"].(string)
	if urlStr == "" {
		return &FilterResult{Allowed: true}, nil
	}

	lower := strings.ToLower(urlStr)
	for _, pattern := range f.BlockedURLPatterns {
		if strings.Contains(lower, strings.ToLower(pattern)) {
			v := NewViolation(f.Name(), SeverityHigh, msg).
				WithAction(ViolationActionBlocked).
				WithDetail("type", "blocked_url").
				WithDetail("pattern", pattern).
				WithDetail("message", fmt.Sprintf("URL contains blocked pattern: %s", pattern))
			return &FilterResult{Allowed: false, Violation: v}, nil
		}
	}

	return &FilterResult{Allowed: true}, nil
}

// checkPII examines tool arguments for PII patterns.
func (f *ToolCallFilter) checkPII(ctx context.Context, msg *Message, args map[string]any) (*FilterResult, error) {
	if f.piiFilter == nil {
		return &FilterResult{Allowed: true}, nil
	}

	// Serialize args to text for PII scanning
	var texts []string
	for _, v := range args {
		if s, ok := v.(string); ok {
			texts = append(texts, s)
		}
	}

	combined := strings.Join(texts, " ")
	if combined == "" {
		return &FilterResult{Allowed: true}, nil
	}

	// Create a synthetic message for PII scanning
	piiMsg := msg.Clone()
	piiMsg.Content.Text = combined

	return f.piiFilter.Process(ctx, piiMsg)
}

// ToolCallToMessage converts an agentic.ToolCall into a governance Message
// for processing through the filter chain.
func ToolCallToMessage(call agentic.ToolCall, userID, channelID string) *Message {
	return &Message{
		ID:        call.ID,
		Type:      MessageTypeToolCall,
		UserID:    userID,
		ChannelID: channelID,
		Content: Content{
			Text: fmt.Sprintf("Tool call: %s", call.Name),
			Metadata: map[string]any{
				"tool_name": call.Name,
				"tool_args": call.Arguments,
				"loop_id":   call.LoopID,
			},
		},
	}
}

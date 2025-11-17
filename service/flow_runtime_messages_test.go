package service

import (
	"testing"
	"time"

	"github.com/c360/semstreams/flowstore"
)

func TestGetSubjectPrefix(t *testing.T) {
	tests := []struct {
		name          string
		componentType string
		expected      string
	}{
		// Input components
		{"UDP source", "udp", "input"},
		{"TCP source", "tcp-source", "input"},
		{"HTTP input", "http-input", "input"},
		{"MQTT source", "mqtt-source", "input"},
		{"Generic input", "input-component", "input"},

		// Processor components
		{"JSON processor", "json-processor", "process"},
		{"Graph processor", "graph-processor", "process"},
		{"Semantic processor", "semantic-transform", "process"},
		{"Filter", "data-filter", "process"},
		{"Transform", "message-transform", "process"},

		// Output components
		{"NATS sink", "nats-sink", "output"},
		{"HTTP output", "http-output", "output"},
		{"Writer", "file-writer", "output"},
		{"Publisher", "mqtt-publisher", "output"},
		{"Generic sink", "output-sink", "output"},

		// Unknown types
		{"Unknown", "custom-component", "events"},
		{"Empty", "", "events"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := getSubjectPrefix(tt.componentType)
			if result != tt.expected {
				t.Errorf("getSubjectPrefix(%q) = %q, want %q", tt.componentType, result, tt.expected)
			}
		})
	}
}

func TestGetFlowMessageSubjects(t *testing.T) {
	tests := []struct {
		name     string
		flow     *flowstore.Flow
		expected []string
	}{
		{
			name: "Flow with mixed components",
			flow: &flowstore.Flow{
				Nodes: []flowstore.FlowNode{
					{Name: "udp-source", Type: "udp"},
					{Name: "json-proc", Type: "json-processor"},
					{Name: "nats-out", Type: "nats-sink"},
				},
			},
			expected: []string{
				"input.udp-source.>",
				"process.json-proc.>",
				"output.nats-out.>",
			},
		},
		{
			name: "Flow with single component",
			flow: &flowstore.Flow{
				Nodes: []flowstore.FlowNode{
					{Name: "processor-1", Type: "processor"},
				},
			},
			expected: []string{
				"process.processor-1.>",
			},
		},
		{
			name:     "Empty flow",
			flow:     &flowstore.Flow{Nodes: []flowstore.FlowNode{}},
			expected: []string{},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := getFlowMessageSubjects(tt.flow)
			if len(result) != len(tt.expected) {
				t.Errorf("getFlowMessageSubjects() returned %d subjects, want %d", len(result), len(tt.expected))
				return
			}
			for i, subject := range result {
				if subject != tt.expected[i] {
					t.Errorf("getFlowMessageSubjects()[%d] = %q, want %q", i, subject, tt.expected[i])
				}
			}
		})
	}
}

func TestMatchesSubject(t *testing.T) {
	tests := []struct {
		name    string
		subject string
		pattern string
		matches bool
	}{
		// Hierarchical wildcard ('>') tests
		{"Exact match with wildcard", "input.udp.data", "input.udp.>", true},
		{"Deeper hierarchy match", "input.udp.data.raw", "input.udp.>", true},
		{"Prefix mismatch", "process.udp.data", "input.udp.>", false},
		{"Root wildcard", "input.udp.data", "input.>", true},

		// Exact match tests
		{"Exact match", "input.udp.data", "input.udp.data", true},
		{"Exact mismatch", "input.udp.data", "input.udp.raw", false},

		// Single token wildcard ('*') tests
		{"Single wildcard match", "input.udp.data", "input.*.data", true},
		{"Single wildcard mismatch", "input.udp.data.raw", "input.*.data", false},
		{"Multiple wildcards", "input.udp.data", "*.*.data", true},

		// Edge cases
		{"Empty pattern", "input.udp.data", "", false},
		{"Empty subject", "", "input.udp.>", false},
		{"Both empty", "", "", true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := matchesSubject(tt.subject, tt.pattern)
			if result != tt.matches {
				t.Errorf("matchesSubject(%q, %q) = %v, want %v", tt.subject, tt.pattern, result, tt.matches)
			}
		})
	}
}

func TestFilterEntriesBySubjects(t *testing.T) {
	entries := []MessageLogEntry{
		{Subject: "input.udp.data", Timestamp: time.Now()},
		{Subject: "process.json.transform", Timestamp: time.Now()},
		{Subject: "output.nats.publish", Timestamp: time.Now()},
		{Subject: "events.system.startup", Timestamp: time.Now()},
		{Subject: "input.udp.stats", Timestamp: time.Now()},
	}

	tests := []struct {
		name          string
		subjects      []string
		expectedCount int
		expectedSubjs []string
	}{
		{
			name:          "Filter by single component",
			subjects:      []string{"input.udp.>"},
			expectedCount: 2,
			expectedSubjs: []string{"input.udp.data", "input.udp.stats"},
		},
		{
			name:          "Filter by multiple components",
			subjects:      []string{"input.udp.>", "process.json.>"},
			expectedCount: 3,
			expectedSubjs: []string{"input.udp.data", "process.json.transform", "input.udp.stats"},
		},
		{
			name:          "Filter with no matches",
			subjects:      []string{"input.tcp.>"},
			expectedCount: 0,
			expectedSubjs: []string{},
		},
		{
			name:          "Empty subject list",
			subjects:      []string{},
			expectedCount: 5,
			expectedSubjs: []string{"input.udp.data", "process.json.transform", "output.nats.publish", "events.system.startup", "input.udp.stats"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := filterEntriesBySubjects(entries, tt.subjects)
			if len(result) != tt.expectedCount {
				t.Errorf("filterEntriesBySubjects() returned %d entries, want %d", len(result), tt.expectedCount)
				return
			}
			for i, entry := range result {
				if entry.Subject != tt.expectedSubjs[i] {
					t.Errorf("filterEntriesBySubjects()[%d].Subject = %q, want %q", i, entry.Subject, tt.expectedSubjs[i])
				}
			}
		})
	}
}

func TestExtractComponentFromSubject(t *testing.T) {
	componentMap := map[string]string{
		"input.udp-source":  "udp-source",
		"process.json-proc": "json-proc",
		"output.nats-sink":  "nats-sink",
		"events.system":     "system",
	}

	tests := []struct {
		name     string
		subject  string
		expected string
	}{
		{"Valid input subject", "input.udp-source.data", "udp-source"},
		{"Valid process subject", "process.json-proc.transform", "json-proc"},
		{"Valid output subject", "output.nats-sink.publish", "nats-sink"},
		{"Valid events subject", "events.system.startup", "system"},
		{"Unknown component", "input.unknown.data", "unknown"},
		{"Single part subject", "input", "unknown"},
		{"Empty subject", "", "unknown"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := extractComponentFromSubject(tt.subject, componentMap)
			if result != tt.expected {
				t.Errorf("extractComponentFromSubject(%q) = %q, want %q", tt.subject, result, tt.expected)
			}
		})
	}
}

func TestFormatMessageEntries(t *testing.T) {
	now := time.Now().UTC()

	flow := &flowstore.Flow{
		Nodes: []flowstore.FlowNode{
			{Name: "udp-source", Type: "udp"},
			{Name: "json-proc", Type: "json-processor"},
		},
	}

	entries := []MessageLogEntry{
		{
			Timestamp:   now,
			Subject:     "input.udp-source.data",
			MessageID:   "msg-001",
			MessageType: "SensorData",
			Summary:     "Temperature reading: 22.5C",
			Metadata: map[string]any{
				"direction":  "received",
				"size_bytes": 128,
			},
		},
		{
			Timestamp:   now.Add(1 * time.Second),
			Subject:     "process.json-proc.transform",
			MessageID:   "msg-002",
			MessageType: "ProcessedData",
			Summary:     "JSON filter applied",
			Metadata:    map[string]any{},
		},
	}

	result := formatMessageEntries(entries, flow)

	if len(result) != 2 {
		t.Fatalf("formatMessageEntries() returned %d messages, want 2", len(result))
	}

	// Check first message
	msg1 := result[0]
	if msg1.Component != "udp-source" {
		t.Errorf("msg1.Component = %q, want %q", msg1.Component, "udp-source")
	}
	if msg1.Direction != "received" {
		t.Errorf("msg1.Direction = %q, want %q", msg1.Direction, "received")
	}
	if msg1.MessageID != "msg-001" {
		t.Errorf("msg1.MessageID = %q, want %q", msg1.MessageID, "msg-001")
	}
	if msg1.Summary != "Temperature reading: 22.5C" {
		t.Errorf("msg1.Summary = %q, want %q", msg1.Summary, "Temperature reading: 22.5C")
	}
	if msg1.Metadata["size_bytes"] != 128 {
		t.Errorf("msg1.Metadata[size_bytes] = %v, want 128", msg1.Metadata["size_bytes"])
	}
	if _, exists := msg1.Metadata["direction"]; exists {
		t.Errorf("msg1.Metadata should not contain 'direction' key (already extracted)")
	}

	// Check second message (default direction)
	msg2 := result[1]
	if msg2.Component != "json-proc" {
		t.Errorf("msg2.Component = %q, want %q", msg2.Component, "json-proc")
	}
	if msg2.Direction != "published" {
		t.Errorf("msg2.Direction = %q, want %q (default)", msg2.Direction, "published")
	}
}

func TestFormatMessageEntriesEmptyInput(t *testing.T) {
	flow := &flowstore.Flow{
		Nodes: []flowstore.FlowNode{
			{Name: "test-component", Type: "processor"},
		},
	}

	result := formatMessageEntries([]MessageLogEntry{}, flow)

	if len(result) != 0 {
		t.Errorf("formatMessageEntries([]) returned %d messages, want 0", len(result))
	}
}

func TestMatchesAnySubject(t *testing.T) {
	patterns := []string{
		"input.udp.>",
		"process.json.>",
		"output.nats.>",
	}

	tests := []struct {
		name    string
		subject string
		matches bool
	}{
		{"Matches first pattern", "input.udp.data", true},
		{"Matches second pattern", "process.json.transform", true},
		{"Matches third pattern", "output.nats.publish", true},
		{"No match", "events.system.startup", false},
		{"Empty subject", "", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := matchesAnySubject(tt.subject, patterns)
			if result != tt.matches {
				t.Errorf("matchesAnySubject(%q, patterns) = %v, want %v", tt.subject, result, tt.matches)
			}
		})
	}
}

func TestMatchesAnySubjectEmptyPatterns(t *testing.T) {
	result := matchesAnySubject("input.udp.data", []string{})
	if result {
		t.Errorf("matchesAnySubject with empty patterns should return false")
	}
}

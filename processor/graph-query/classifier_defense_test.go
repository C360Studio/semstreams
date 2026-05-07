package graphquery

import (
	"log/slog"
	"testing"
)

func TestClassifierTemplatePattern(t *testing.T) {
	tests := []struct {
		name  string
		value string
		match bool
	}{
		{"angle-bracket placeholder", "<entity_type>", true},
		{"angle-bracket open only", "<unclosed", true},
		{"curly-brace placeholder", "{type}", true},
		{"dollar-sign placeholder", "$type_filter", true},
		{"plain entity type", "sensor", false},
		{"plain numeric type", "drone", false},
		{"empty string", "", false},
		{"contains brackets in middle", "thing<x>", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := classifierTemplatePattern.MatchString(tt.value)
			if got != tt.match {
				t.Errorf("MatchString(%q) = %v, want %v", tt.value, got, tt.match)
			}
		})
	}
}

func TestFilterClassifierTemplates(t *testing.T) {
	logger := slog.Default()

	tests := []struct {
		name string
		in   []string
		want []string
	}{
		{"all valid", []string{"sensor", "drone"}, []string{"sensor", "drone"}},
		{"all template", []string{"<entity_type>", "{type}"}, []string{}},
		{"mixed — drop templates only", []string{"sensor", "<entity_type>", "drone"}, []string{"sensor", "drone"}},
		{"empty", []string{}, []string{}},
		{"whitespace-padded template", []string{"  <entity_type>  "}, []string{}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Use nil metrics to skip the increment path; we're testing
			// the filtering logic, not metrics emission.
			got := filterClassifierTemplates(tt.in, logger, nil)
			if len(got) != len(tt.want) {
				t.Fatalf("got %v (len=%d), want %v (len=%d)", got, len(got), tt.want, len(tt.want))
			}
			for i := range got {
				if got[i] != tt.want[i] {
					t.Errorf("got[%d] = %q, want %q", i, got[i], tt.want[i])
				}
			}
		})
	}
}

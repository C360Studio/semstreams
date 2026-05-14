package oasf

import (
	"strings"
	"testing"
)

func TestNameAndIDRoundTrip(t *testing.T) {
	for id, name := range categoryNames {
		if got := Name(id); got != name {
			t.Errorf("Name(%d) = %q, want %q", id, got, name)
		}
		if got := ID(name); got != id {
			t.Errorf("ID(%q) = %d, want %d", name, got, id)
		}
	}
}

func TestNameUnknownReturnsEmpty(t *testing.T) {
	if got := Name(999); got != "" {
		t.Errorf("Name(999) = %q, want empty (uid 999 not in MVP coverage)", got)
	}
	if got := ID("not_a_category"); got != 0 {
		t.Errorf("ID(\"not_a_category\") = %d, want 0", got)
	}
}

func TestCaptionForKnownAndUnknown(t *testing.T) {
	if got := Caption(CategoryNLP); got != "Natural Language Processing" {
		t.Errorf("Caption(NLP) = %q, want \"Natural Language Processing\"", got)
	}
	if got := Caption(0); got != "" {
		t.Errorf("Caption(0) = %q, want empty", got)
	}
}

func TestIsCanonicalAndIsExtension(t *testing.T) {
	cases := []struct {
		name          string
		id            uint32
		wantCanonical bool
		wantExtension bool
	}{
		{"covered-category", CategoryNLP, true, false},
		{"uncovered-canonical-id", 12, false, false}, // OASF uid 12 (devops_mlops), not in our MVP
		{"zero-sentinel", 0, false, false},
		{"extension-floor", ExtensionBase, false, true},
		{"extension-above-floor", ExtensionBase + 42, false, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := IsCanonical(tc.id); got != tc.wantCanonical {
				t.Errorf("IsCanonical(%d) = %v, want %v", tc.id, got, tc.wantCanonical)
			}
			if got := IsExtension(tc.id); got != tc.wantExtension {
				t.Errorf("IsExtension(%d) = %v, want %v", tc.id, got, tc.wantExtension)
			}
		})
	}
}

func TestExtensionIDIsDeterministicAndAboveFloor(t *testing.T) {
	const expr = "code-review"
	first := ExtensionID(expr)
	if first < ExtensionBase {
		t.Errorf("ExtensionID(%q) = %d, want >= %d", expr, first, ExtensionBase)
	}
	// Stability across calls — same input must yield same ID across
	// processes (the FNV hash is stable, but guard against accidental
	// algorithm changes).
	if second := ExtensionID(expr); second != first {
		t.Errorf("ExtensionID(%q) is non-deterministic: %d vs %d", expr, first, second)
	}
	// Case + whitespace normalization.
	if got := ExtensionID("  Code-Review  "); got != first {
		t.Errorf("ExtensionID is not whitespace/case stable: %d vs %d", got, first)
	}
}

func TestExtensionIDEmptyExpression(t *testing.T) {
	if got := ExtensionID(""); got != 0 {
		t.Errorf("ExtensionID(\"\") = %d, want 0", got)
	}
	if got := ExtensionID("   "); got != 0 {
		t.Errorf("ExtensionID(whitespace) = %d, want 0", got)
	}
}

func TestExtensionNameShape(t *testing.T) {
	cases := map[string]string{
		"code-review":   "semstreams/code_review",
		"Code Review":   "semstreams/code review", // spaces preserved
		"documentation": "semstreams/documentation",
		"  trimmed  ":   "semstreams/trimmed",
	}
	for input, want := range cases {
		t.Run(input, func(t *testing.T) {
			got := ExtensionName(input)
			if got != want {
				t.Errorf("ExtensionName(%q) = %q, want %q", input, got, want)
			}
			if !strings.HasPrefix(got, "semstreams/") && got != "semstreams" {
				t.Errorf("ExtensionName(%q) = %q, missing semstreams/ prefix", input, got)
			}
		})
	}
}

func TestExtensionNameEmpty(t *testing.T) {
	if got := ExtensionName(""); got != "semstreams" {
		t.Errorf("ExtensionName(\"\") = %q, want %q", got, "semstreams")
	}
}

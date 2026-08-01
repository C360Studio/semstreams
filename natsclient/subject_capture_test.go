package natsclient

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestSubjectFilterCaptures covers NATS token wildcard semantics in both
// directions. The false cases carry the weight: a guard that over-matches
// invents collisions and blocks valid deployments, which is a worse outcome
// than the silent capture it exists to prevent — an operator can work around a
// missing warning, not a refusal to boot on a correct config.
func TestSubjectFilterCaptures(t *testing.T) {
	tests := []struct {
		name    string
		filter  string
		subject string
		want    bool
	}{
		// The gh#810 instance.
		{"the gh#810 collision", "tool.>", "tool.list", true},
		{"exact match", "tool.list", "tool.list", true},
		{"single-token star", "tool.*", "tool.list", true},
		{"star mid-filter", "graph.*.entity", "graph.query.entity", true},
		{"gt captures deeper subjects", "tool.>", "tool.list.v2", true},
		{"gt at root", ">", "tool.list", true},

		// Prefix-test errors, direction 1: `>` follows a TOKEN boundary.
		{"gt does not cross a token boundary", "tool.>", "toolbox.list", false},
		{"literal does not prefix-match", "tool", "toolbox", false},
		// Prefix-test errors, direction 2: `*` is EXACTLY one token.
		{"star is exactly one token", "tool.*", "tool.list.v2", false},
		{"star does not match zero tokens", "tool.*", "tool", false},

		{"different root", "agent.>", "tool.list", false},
		{"filter longer than subject", "tool.list.v2", "tool.list", false},
		{"subject longer than literal filter", "tool.list", "tool.list.v2", false},
		{"empty filter", "", "tool.list", false},
		{"empty subject", "tool.>", "", false},
		// `>` is a wildcard only as the final token.
		{"gt not final is literal", "tool.>.x", "tool.list.x", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, SubjectFilterCaptures(tt.filter, tt.subject),
				"SubjectFilterCaptures(%q, %q)", tt.filter, tt.subject)
		})
	}
}

// TestFindSubjectCaptures_ClassClosure pins the property that makes this fix
// close the class rather than the instance: a subject declared AFTER the stream
// is still caught, because the answer is derived from both sets rather than
// looked up in a table of known-bad pairs.
func TestFindSubjectCaptures_ClassClosure(t *testing.T) {
	t.Run("the shipped TOOL stream shape captures discovery", func(t *testing.T) {
		got := FindSubjectCaptures("TOOL", []string{"tool.>"}, []string{"tool.list"})
		require.Len(t, got, 1)
		assert.Equal(t, "TOOL", got[0].Stream)
		assert.Equal(t, "tool.>", got[0].Filter)
		assert.Equal(t, "tool.list", got[0].Subject)
		// The message must carry all three facts plus the remedy.
		assert.Contains(t, got[0].Error(), "TOOL")
		assert.Contains(t, got[0].Error(), "tool.list")
		assert.Contains(t, got[0].Error(), "narrow the stream's subjects")
	})

	t.Run("a subject added after the stream is still caught", func(t *testing.T) {
		// Nobody updated the stream declaration; the new subject simply exists.
		got := FindSubjectCaptures("TOOL", []string{"tool.>"}, []string{"tool.list", "tool.describe"})
		assert.Len(t, got, 2, "both declared subjects are captured, including one the stream author never saw")
	})

	t.Run("a stream covering nothing declared reports nothing", func(t *testing.T) {
		got := FindSubjectCaptures("AGENT", []string{"agent.>", "loop.*"}, []string{"tool.list", "graph.query.entity"})
		assert.Empty(t, got, "a non-overlapping stream must not be reported — false positives block valid deployments")
	})

	t.Run("no declared subjects means nothing to capture", func(t *testing.T) {
		assert.Empty(t, FindSubjectCaptures("TOOL", []string{"tool.>"}, nil))
	})
}

package flowgraph

import "testing"

func TestSubjectCoversIsDirectionalCover(t *testing.T) {
	cases := []struct {
		filter, pattern string
		want            bool
	}{
		{"data.>", "data.raw", true},
		{"data.>", "data.*", true},
		{"data.>", "data.a.b", true},
		{"data.>", "data", false},
		{"data.*", "data.raw", true},
		{"data.*", "data.>", false},
		{"data.*", "data.a.b", false},
		{"data.raw", "data.raw", true},
		{"data.raw", "data.*", false},
		{"*.raw", "data.raw", true},
		{"data.raw", "*.raw", false},
		{">", "anything.at.all", true},
		{"data.>", "other.raw", false},
		{"", "data.raw", false},
		{"data.>", "data..raw", false},
	}
	for _, c := range cases {
		if got := SubjectCovers(c.filter, c.pattern); got != c.want {
			t.Errorf("SubjectCovers(%q, %q) = %v, want %v", c.filter, c.pattern, got, c.want)
		}
	}
	// Overlap is symmetric where cover is not.
	if !SubjectMatches("data.*", "data.raw") || SubjectCovers("data.raw", "data.*") {
		t.Fatal("cover must be stricter than overlap")
	}
}

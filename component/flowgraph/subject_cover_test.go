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
		// Two filters whose concrete subject sets intersect (foo.baz.bar
		// matches both) but where neither covers the other. The retired
		// SubjectMatches advertised overlap and answered false for this pair;
		// cover is the question the validator asks, and false is correct here.
		{"foo.*.bar", "foo.baz.*", false},
		{"foo.baz.*", "foo.*.bar", false},
		{"foo.*.bar", "foo.baz.bar", true},
	}
	for _, c := range cases {
		if got := SubjectCovers(c.filter, c.pattern); got != c.want {
			t.Errorf("SubjectCovers(%q, %q) = %v, want %v", c.filter, c.pattern, got, c.want)
		}
	}
	// Cover is stricter than the direct match edge derivation uses: the two
	// agree that `data.*` reaches `data.raw`, and only cover refuses the
	// reverse.
	if !matchNATSPattern("data.*", "data.raw") || !matchNATSPattern("data.raw", "data.*") {
		t.Fatal("direct match is symmetric on this pair")
	}
	if SubjectCovers("data.raw", "data.*") {
		t.Fatal("cover must be stricter than the direct match")
	}
}

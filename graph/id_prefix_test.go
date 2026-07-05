package graph

import "testing"

func TestMatchesAnyIDPrefix(t *testing.T) {
	tests := []struct {
		name     string
		id       string
		prefixes []string
		want     bool
	}{
		{"nil prefixes match all", "a.b.c.d.e.1", nil, true},
		{"empty prefixes match all", "a.b.c.d.e.1", []string{}, true},
		{"empty-string element matches all", "a.b.c.d.e.1", []string{""}, true},
		{"exact match", "c360.semspec.source.doc", []string{"c360.semspec.source.doc"}, true},
		{"prefix on dot boundary", "c360.semspec.source.doc.readme", []string{"c360.semspec.source.doc"}, true},
		{"boundary guard: docker is not under doc", "c360.semspec.source.docker.compose", []string{"c360.semspec.source.doc"}, false},
		{"no match", "c360.semspec.source.code.main", []string{"c360.semspec.source.doc"}, false},
		{"multi-prefix OR: second matches", "acme.web.python.svc.class.User", []string{"acme.web.golang", "acme.web.python"}, true},
		{"multi-prefix OR: none match", "acme.web.ts.svc.fn.render", []string{"acme.web.golang", "acme.web.python"}, false},
		{"id shorter than prefix", "a.b", []string{"a.b.c"}, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := MatchesAnyIDPrefix(tt.id, tt.prefixes); got != tt.want {
				t.Errorf("MatchesAnyIDPrefix(%q, %v) = %v, want %v", tt.id, tt.prefixes, got, tt.want)
			}
		})
	}
}

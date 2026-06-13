package ownership

import (
	"reflect"
	"testing"
)

func TestPatternsIntersect(t *testing.T) {
	tests := []struct {
		name string
		a, b string
		want bool
	}{
		{"identical", "c360.semconnect.systems.csapi.system.x", "c360.semconnect.systems.csapi.system.x", true},
		{"same-prefix-wildcard-tail", "c360.semconnect.systems.csapi.system.*", "c360.semconnect.systems.csapi.system.drone-001", true},
		{"both-wildcard-tail", "c360.semconnect.systems.csapi.system.*", "c360.semconnect.systems.csapi.system.*", true},
		{"wildcard-vs-wildcard-mid", "*.semconnect.systems.csapi.system.*", "c360.*.systems.csapi.system.*", true},
		// cs-api disjoint id-spaces: System vs Deployment differ at segment 5.
		{"csapi-system-vs-deployment-disjoint", "c360.semconnect.systems.csapi.system.*", "c360.semconnect.systems.csapi.deployment.*", false},
		{"one-literal-segment-differs", "a.b.c.d.e.f", "a.b.c.d.e.g", false},
		{"literal-differs-under-wildcards", "a.b.*.d.e.f", "a.b.*.d.e.g", false},
		{"differing-arity", "a.b.c.d.e.f", "a.b.c.d.e", false},
		{"bare-star-left", "*", "a.b.c.d.e.f", true},
		{"bare-star-right", "a.b.c.d.e.f", "*", true},
		{"empty-left", "", "a.b.c.d.e.f", false},
		{"empty-right", "a.b.c.d.e.f", "", false},
		{"wildcard-bridges-mismatch-elsewhere", "a.*.c.d.e.f", "a.b.c.d.e.g", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := patternsIntersect(tt.a, tt.b); got != tt.want {
				t.Errorf("patternsIntersect(%q, %q) = %v, want %v", tt.a, tt.b, got, tt.want)
			}
			// Intersection is symmetric.
			if got := patternsIntersect(tt.b, tt.a); got != tt.want {
				t.Errorf("patternsIntersect(%q, %q) [reversed] = %v, want %v", tt.b, tt.a, got, tt.want)
			}
		})
	}
}

// TestPatternsIntersectConsistentWithMatch is the load-bearing invariant: if a
// concrete id matches BOTH patterns, the patterns MUST intersect. A registry
// that violated this would let two owners co-own a cell some entity falls into.
func TestPatternsIntersectConsistentWithMatch(t *testing.T) {
	patterns := []string{
		"c360.semconnect.systems.csapi.system.*",
		"c360.semconnect.systems.csapi.*.drone-001",
		"*.semconnect.systems.csapi.system.drone-001",
		"a.b.c.d.e.f",
		"a.b.c.d.e.*",
	}
	ids := []string{
		"c360.semconnect.systems.csapi.system.drone-001",
		"c360.semconnect.systems.csapi.system.x",
		"a.b.c.d.e.f",
		"a.b.c.d.e.zzz",
	}
	for _, a := range patterns {
		for _, b := range patterns {
			for _, id := range ids {
				if patternMatches(a, id) && patternMatches(b, id) && !patternsIntersect(a, b) {
					t.Errorf("id %q matches both %q and %q but patternsIntersect=false", id, a, b)
				}
			}
		}
	}
}

func TestPatternMatches(t *testing.T) {
	tests := []struct {
		pattern, id string
		want        bool
	}{
		{"c360.semconnect.systems.csapi.system.*", "c360.semconnect.systems.csapi.system.drone-001", true},
		{"c360.semconnect.systems.csapi.system.*", "c360.semconnect.systems.csapi.deployment.d1", false},
		{"a.b.c.d.e.f", "a.b.c.d.e.f", true},
		{"a.b.c.d.e.f", "a.b.c.d.e.g", false},
		{"a.b.c.d.e", "a.b.c.d.e.f", false}, // arity
		{"*", "anything.at.all", true},
		{"", "a.b.c.d.e.f", false},
	}
	for _, tt := range tests {
		if got := patternMatches(tt.pattern, tt.id); got != tt.want {
			t.Errorf("patternMatches(%q, %q) = %v, want %v", tt.pattern, tt.id, got, tt.want)
		}
	}
}

func TestPredicatesIntersection(t *testing.T) {
	tests := []struct {
		name string
		a, b []string
		want []string
	}{
		{"overlap-sorted", []string{"b.p", "a.p", "c.p"}, []string{"c.p", "a.p"}, []string{"a.p", "c.p"}},
		{"no-overlap", []string{"a.p"}, []string{"b.p"}, nil},
		{"dedupe-from-b", []string{"a.p"}, []string{"a.p", "a.p"}, []string{"a.p"}},
		{"empty", nil, nil, nil},
		{"identical", []string{"x", "y"}, []string{"y", "x"}, []string{"x", "y"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := predicatesIntersection(tt.a, tt.b); !reflect.DeepEqual(got, tt.want) {
				t.Errorf("predicatesIntersection(%v, %v) = %v, want %v", tt.a, tt.b, got, tt.want)
			}
		})
	}
}

func TestValidPattern(t *testing.T) {
	if !validPattern("a.b.c.d.e.f") {
		t.Error("6-part pattern should be valid")
	}
	if validPattern("a.b.c.d.e") {
		t.Error("5-part pattern should be invalid")
	}
	if validPattern("*") {
		t.Error("bare * should not be a valid 6-part owner pattern")
	}
}

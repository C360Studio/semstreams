package vocabulary

import "testing"

func TestDiscoverLabelPredicates_IncludesTitleExcludesResolvable(t *testing.T) {
	labels := DiscoverLabelPredicates()

	// dc.terms.title is registered as AliasTypeLabel and must appear.
	if _, ok := labels[DCTermsTitle]; !ok {
		t.Fatalf("DiscoverLabelPredicates missing %q; got %v", DCTermsTitle, labels)
	}

	// Label predicates must NOT appear in the alias (resolvable) set, and vice
	// versa — the two sets are disjoint by AliasType.
	aliases := DiscoverAliasPredicates()
	if _, ok := aliases[DCTermsTitle]; ok {
		t.Fatalf("%q is a label predicate and must not be alias-resolvable", DCTermsTitle)
	}
	for name := range labels {
		if _, ok := aliases[name]; ok {
			t.Fatalf("predicate %q appears in BOTH label and alias sets — AliasType is ambiguous", name)
		}
	}
}

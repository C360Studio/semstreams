package graphmutation

import "testing"

func TestResolveSubjectAdmitsOnlyFourCanonicalOperations(t *testing.T) {
	want := map[Operation]string{
		CreateEntity:        "graph.mutation.entity.create",
		ReconcilePredicates: "graph.mutation.entity.reconcile",
		AppendTriples:       "graph.mutation.triple.append",
		DeleteEntity:        "graph.mutation.entity.delete",
	}
	for operation, subject := range want {
		got, err := ResolveSubject(SubjectFamily, operation)
		if err != nil {
			t.Fatalf("ResolveSubject(%q): %v", operation, err)
		}
		if got != subject {
			t.Fatalf("ResolveSubject(%q) = %q, want %q", operation, got, subject)
		}
	}

	for _, retired := range []Operation{
		"entity.create_with_triples",
		"entity.update",
		"entity.update_with_triples",
		"triple.add",
		"triple.add_batch",
		"triple.remove",
	} {
		if subject, err := ResolveSubject(SubjectFamily, retired); err == nil {
			t.Fatalf("retired operation %q resolved to %q", retired, subject)
		}
	}
}

func TestResolveSubjectRejectsNonCanonicalFamily(t *testing.T) {
	for _, family := range []string{"graph.mutation.*", "graph.mutation", "mutations.>"} {
		if subject, err := ResolveSubject(family, CreateEntity); err == nil {
			t.Fatalf("non-canonical family %q resolved to %q", family, subject)
		}
	}
}

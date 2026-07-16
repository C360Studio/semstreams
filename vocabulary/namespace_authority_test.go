package vocabulary

import (
	"errors"
	"testing"
)

func TestParsePredicateNamespace(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		namespace string
		want      PredicateNamespace
		wantErr   bool
	}{
		{name: "domain", namespace: "research", want: PredicateNamespace{Domain: "research"}},
		{name: "domain category", namespace: "research.result", want: PredicateNamespace{Domain: "research", Category: "result"}},
		{name: "property is too specific", namespace: "research.result.complete", wantErr: true},
		{name: "wildcard", namespace: "research.*", wantErr: true},
		{name: "underscore", namespace: "research.search_result", wantErr: true},
		{name: "empty", namespace: "", wantErr: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got, err := ParsePredicateNamespace(tt.namespace)
			if tt.wantErr {
				if err == nil {
					t.Fatalf("ParsePredicateNamespace(%q) unexpectedly succeeded", tt.namespace)
				}
				return
			}
			if err != nil {
				t.Fatalf("ParsePredicateNamespace(%q): %v", tt.namespace, err)
			}
			if got != tt.want || got.String() != tt.namespace {
				t.Fatalf("ParsePredicateNamespace(%q) = %#v (%q), want %#v", tt.namespace, got, got.String(), tt.want)
			}
		})
	}
}

func TestPredicateAuthority(t *testing.T) {
	const registered = "authority-test.declared.value"
	Register(registered)

	authority, err := NewPredicateAuthority(
		NamespaceDelegation{Producer: "domain-producer", Namespace: "product"},
		NamespaceDelegation{Producer: "category-producer", Namespace: "shared.metrics"},
	)
	if err != nil {
		t.Fatalf("NewPredicateAuthority: %v", err)
	}

	tests := []struct {
		name      string
		producer  string
		predicate string
		wantErr   bool
	}{
		{name: "registered named", producer: "unknown", predicate: "authority-test.declared.value"},
		{name: "registered anonymous", predicate: "authority-test.declared.value"},
		{name: "domain delegation", producer: "domain-producer", predicate: "product.any-category.new-value"},
		{name: "category delegation", producer: "category-producer", predicate: "shared.metrics.new-value"},
		{name: "category delegation does not cross category", producer: "category-producer", predicate: "shared.other.new-value", wantErr: true},
		{name: "anonymous cannot use delegation", predicate: "product.any-category.new-value", wantErr: true},
		{name: "other producer cannot use delegation", producer: "other", predicate: "product.any-category.new-value", wantErr: true},
		{name: "malformed remains invalid", producer: "domain-producer", predicate: "product.any_category.new-value", wantErr: true}, // predicate-audit:invalid {"kind":"stored-predicate","value":"product.any_category.new-value","reason":"segment_character"}
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := authority.Authorize(tt.producer, tt.predicate)
			if tt.wantErr && err == nil {
				t.Fatalf("Authorize(%q, %q) unexpectedly succeeded", tt.producer, tt.predicate)
			}
			if !tt.wantErr && err != nil {
				t.Fatalf("Authorize(%q, %q): %v", tt.producer, tt.predicate, err)
			}
			if tt.name == "malformed remains invalid" {
				var validationErr *PredicateValidationError
				if !errors.As(err, &validationErr) {
					t.Fatalf("malformed authorization error = %T, want *PredicateValidationError", err)
				}
			}
		})
	}
}

func TestNewPredicateAuthorityRejectsInvalidDelegation(t *testing.T) {
	t.Parallel()

	if _, err := NewPredicateAuthority(NamespaceDelegation{Namespace: "product"}); err == nil {
		t.Fatal("empty producer delegation unexpectedly accepted")
	}
	if _, err := NewPredicateAuthority(NamespaceDelegation{Producer: "producer", Namespace: "product.*"}); err == nil {
		t.Fatal("wildcard delegation unexpectedly accepted")
	}
}

func TestRequireDeclaredPredicate(t *testing.T) {
	declared := "authority-test.registered.value"
	Register(declared)
	if err := RequireDeclaredPredicate(declared); err != nil {
		t.Fatalf("registered predicate rejected: %v", err)
	}
	if err := RequireDeclaredPredicate("authority-test.undeclared.value"); err == nil {
		t.Fatal("canonical but undeclared predicate unexpectedly accepted")
	}
	if err := RequireDeclaredPredicate("authority-test.invalid_value"); err == nil {
		t.Fatal("malformed predicate unexpectedly accepted")
	}
}

// Package semantictest provides grammar-only helpers for positive semantic
// test fixtures. It intentionally does not construct graph entities, triples,
// or other behavior-bearing values.
//
// Grammar-authority tests in pkg/types and vocabulary remain raw fixtures: an
// import of this package from either authority would create a package cycle.
package semantictest

import (
	"strings"
	"testing"

	semtypes "github.com/c360studio/semstreams/pkg/types"
	"github.com/c360studio/semstreams/vocabulary"
)

// EntityID joins the six explicit semantic positions in the canonical order
// org.platform.system.domain.type.instance without rewriting any byte and
// returns the value only when the canonical entity-ID authority accepts it.
func EntityID(
	t testing.TB,
	organization string,
	platform string,
	system string,
	domain string,
	entityType string,
	instance string,
) string {
	t.Helper()

	value, err := validateEntityIDFixture(organization, platform, system, domain, entityType, instance)
	if err != nil {
		t.Fatalf("invalid canonical entity-ID fixture %q: %v", value, err)
	}
	return value
}

// Predicate joins the three explicit semantic positions without rewriting
// any byte and returns the value only when the canonical predicate authority
// accepts it.
func Predicate(t testing.TB, domain, category, property string) string {
	t.Helper()

	value, err := validatePredicateFixture(domain, category, property)
	if err != nil {
		t.Fatalf("invalid canonical predicate fixture %q: %v", value, err)
	}
	return value
}

func validateEntityIDFixture(
	organization string,
	platform string,
	system string,
	domain string,
	entityType string,
	instance string,
) (string, error) {
	value := strings.Join([]string{organization, platform, system, domain, entityType, instance}, ".")
	return value, semtypes.ValidateEntityID(value)
}

func validatePredicateFixture(domain, category, property string) (string, error) {
	value := strings.Join([]string{domain, category, property}, ".")
	_, err := vocabulary.ParsePredicate(value)
	return value, err
}

package oms

import (
	"maps"
	"strings"
)

// iris is the canonical set of IRIs this package surfaces, indexed
// by their compact form. Adding a constant in iris.go requires
// adding it here too; the contract test in iris_test.go fails loud
// if these drift apart.
var iris = map[string]string{
	// Classes
	Prefix + ":Observation":        Observation,
	Prefix + ":ObservableProperty": ObservableProperty,
	Prefix + ":Procedure":          Procedure,
	Prefix + ":FeatureOfInterest":  FeatureOfInterest,
	Prefix + ":Result":             Result,

	// Predicates
	Prefix + ":resultTime":           ResultTime,
	Prefix + ":phenomenonTime":       PhenomenonTime,
	Prefix + ":hasResult":            HasResult,
	Prefix + ":hasFeatureOfInterest": HasFeatureOfInterest,
	Prefix + ":observedProperty":     ObservedProperty,
	Prefix + ":usedProcedure":        UsedProcedure,
}

var reverseIRIs = func() map[string]string {
	m := make(map[string]string, len(iris))
	for compact, iri := range iris {
		m[iri] = compact
	}
	return m
}()

// IRIs returns a copy of the full set of compact → IRI mappings
// covered by this package.
func IRIs() map[string]string {
	out := make(map[string]string, len(iris))
	maps.Copy(out, iris)
	return out
}

// IsKnown reports whether the given IRI is part of this package's
// coverage.
func IsKnown(iri string) bool {
	_, ok := reverseIRIs[iri]
	return ok
}

// LocalName returns the local part of an OMS IRI, or the empty
// string if the IRI is not in the OMS namespace.
func LocalName(iri string) string {
	if strings.HasPrefix(iri, Namespace) {
		return iri[len(Namespace):]
	}
	return ""
}

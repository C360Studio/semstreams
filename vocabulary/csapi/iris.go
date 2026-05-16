package csapi

import (
	"maps"
	"strings"
)

// CS API namespace identifiers. Pinned to the spec-rooted stem used
// by OGC API Connected Systems v1.0. The spec is a working draft;
// when canonical IRIs publish, this constant gets a one-shot swap.
const (
	// Prefix is the CS API short token used when compacting IRIs.
	Prefix = "csapi"

	// Namespace is the CS API v1.0 IRI stem.
	Namespace = "http://www.opengis.net/spec/ogcapi-connectedsystems-1/1.0/"
)

// CS API class IRIs. Use as the object of an rdf:type triple, or
// anywhere a Connected-Systems-aware encoder references the type.
const (
	// Datastream — a stream of Observations produced by one System
	// (sensor or system of systems) for one ObservableProperty, with
	// declared temporal bounds (PhenomenonTimeRange,
	// ResultTimeRange) and a result-type discriminator (ResultType).
	// CS API v1.0 §10.
	Datastream = Namespace + "Datastream"
)

// iris is the canonical set of IRIs this package surfaces, indexed
// by their compact form. Adding a constant in iris.go or
// predicates.go requires adding it here too; the contract test in
// iris_test.go fails loud if these drift apart.
var iris = map[string]string{
	// Classes
	Prefix + ":Datastream": Datastream,

	// Predicates
	Prefix + ":producedBy":          ProducedBy,
	Prefix + ":resultTimeRange":     ResultTimeRange,
	Prefix + ":phenomenonTimeRange": PhenomenonTimeRange,
	Prefix + ":resultType":          ResultType,
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

// LocalName returns the local part of a CS API IRI, or the empty
// string if the IRI is not in the CS API namespace.
func LocalName(iri string) string {
	if strings.HasPrefix(iri, Namespace) {
		return iri[len(Namespace):]
	}
	return ""
}

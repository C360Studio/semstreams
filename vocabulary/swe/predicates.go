package swe

import (
	"maps"
	"strings"
)

// iris is the canonical set of IRIs this package surfaces, indexed
// by their compact form. Adding a constant in iris.go requires
// adding it here too; the contract test in iris_test.go fails loud
// if these drift apart.
var iris = map[string]string{
	// Types
	Prefix + ":Quantity":      Quantity,
	Prefix + ":Category":      Category,
	Prefix + ":Time":          Time,
	Prefix + ":Count":         Count,
	Prefix + ":Boolean":       Boolean,
	Prefix + ":Text":          Text,
	Prefix + ":QuantityRange": QuantityRange,
	Prefix + ":DataRecord":    DataRecord,
	Prefix + ":DataArray":     DataArray,
	Prefix + ":DataChoice":    DataChoice,
	Prefix + ":Vector":        Vector,

	// Structural roles
	Prefix + ":label":          Label,
	Prefix + ":definition":     Definition,
	Prefix + ":uom":            UoM,
	Prefix + ":value":          Value,
	Prefix + ":nilValue":       NilValue,
	Prefix + ":referenceFrame": ReferenceFrame,
}

var reverseIRIs = func() map[string]string {
	m := make(map[string]string, len(iris))
	for compact, iri := range iris {
		m[iri] = compact
	}
	return m
}()

// IRIs returns a copy of the full set of compact → IRI mappings
// covered by this package. The returned map is safe for the caller
// to mutate without affecting the package state.
func IRIs() map[string]string {
	out := make(map[string]string, len(iris))
	maps.Copy(out, iris)
	return out
}

// IsKnown reports whether the given IRI is part of this package's
// coverage. Returns false for swe: IRIs that have not been promoted
// to constants yet.
func IsKnown(iri string) bool {
	_, ok := reverseIRIs[iri]
	return ok
}

// LocalName returns the local part of a SWE IRI (the segment after
// the namespace stem), or the empty string if the IRI is not in
// the SWE namespace.
func LocalName(iri string) string {
	if strings.HasPrefix(iri, Namespace) {
		return iri[len(Namespace):]
	}
	return ""
}

package sosa

import (
	"maps"
	"strings"
)

// iris is the canonical set of IRIs this package surfaces, indexed
// by their compact form ("sosa:observes", "ssn:hasDeployment"). The
// map is the source of truth for IsKnown / IRIs / LocalName lookups
// and for the contract test that verifies round-trip compaction.
//
// Adding a new constant in iris.go requires adding it here too; the
// contract test fails loud if these drift apart.
var iris = map[string]string{
	// SOSA classes
	Prefix + ":Sensor":             Sensor,
	Prefix + ":Observation":        Observation,
	Prefix + ":FeatureOfInterest":  FeatureOfInterest,
	Prefix + ":ObservableProperty": ObservableProperty,
	Prefix + ":Platform":           Platform,
	Prefix + ":Procedure":          Procedure,
	Prefix + ":Sample":             Sample,
	Prefix + ":SamplingFeature":    SamplingFeature,
	Prefix + ":Sampler":            Sampler,
	Prefix + ":Result":             Result,
	Prefix + ":Actuator":           Actuator,
	Prefix + ":Actuation":          Actuation,
	Prefix + ":ActuatableProperty": ActuatableProperty,

	// SOSA predicates
	Prefix + ":observes":             Observes,
	Prefix + ":hasFeatureOfInterest": HasFeatureOfInterest,
	Prefix + ":madeBySensor":         MadeBySensor,
	Prefix + ":madeObservation":      MadeObservation,
	Prefix + ":usedProcedure":        UsedProcedure,
	Prefix + ":hasSimpleResult":      HasSimpleResult,
	Prefix + ":hasResult":            HasResult,
	Prefix + ":resultTime":           ResultTime,
	Prefix + ":phenomenonTime":       PhenomenonTime,
	Prefix + ":hosts":                Hosts,
	Prefix + ":isHostedBy":           IsHostedBy,
	Prefix + ":observedProperty":     ObservedProperty,
	Prefix + ":hasLocation":          HasLocation,

	// SSN classes
	SSNPrefix + ":System":     SSNSystem,
	SSNPrefix + ":Deployment": SSNDeployment,

	// SSN predicates
	SSNPrefix + ":hasDeployment":  SSNHasDeployment,
	SSNPrefix + ":deployedSystem": SSNDeployedSystem,
	SSNPrefix + ":hasSubSystem":   SSNHasSubSystem,
	SSNPrefix + ":hasInput":       SSNHasInput,
	SSNPrefix + ":hasOutput":      SSNHasOutput,
}

// reverseIRIs maps full IRI back to compact form, built once at init.
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
// coverage. Returns false for IRIs in the sosa: or ssn: namespace
// that we have not yet added a constant for — callers that need
// to accept any well-formed SOSA/SSN IRI should pair this with a
// namespace check.
func IsKnown(iri string) bool {
	_, ok := reverseIRIs[iri]
	return ok
}

// LocalName returns the local part of a SOSA or SSN IRI (the segment
// after the namespace stem), or the empty string if the IRI is not
// in either namespace.
//
// Distinct from IsKnown: an IRI in the SOSA namespace that this
// package has not promoted to a constant still returns a non-empty
// local name from this function, because the split is purely
// lexical.
func LocalName(iri string) string {
	if strings.HasPrefix(iri, Namespace) {
		return iri[len(Namespace):]
	}
	if strings.HasPrefix(iri, SSNNamespace) {
		return iri[len(SSNNamespace):]
	}
	return ""
}

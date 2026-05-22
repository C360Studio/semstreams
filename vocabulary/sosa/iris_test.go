package sosa

import (
	"strings"
	"testing"
)

// TestIRIsCoverConstants is the contract test that catches drift
// between the package-level constants and the iris map. If a new
// constant is added in iris.go without a matching entry in
// predicates.go, this test fails loud.
func TestIRIsCoverConstants(t *testing.T) {
	wantPairs := map[string]string{
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

		SSNPrefix + ":System":         SSNSystem,
		SSNPrefix + ":Deployment":     SSNDeployment,
		SSNPrefix + ":hasDeployment":  SSNHasDeployment,
		SSNPrefix + ":deployedSystem": SSNDeployedSystem,
		SSNPrefix + ":hasSubSystem":   SSNHasSubSystem,
		SSNPrefix + ":hasInput":       SSNHasInput,
		SSNPrefix + ":hasOutput":      SSNHasOutput,
	}
	got := IRIs()
	if len(got) != len(wantPairs) {
		t.Fatalf("IRIs(): want %d entries, got %d", len(wantPairs), len(got))
	}
	for compact, iri := range wantPairs {
		if gotIRI, ok := got[compact]; !ok {
			t.Errorf("IRIs() missing %q", compact)
		} else if gotIRI != iri {
			t.Errorf("IRIs()[%q] = %q, want %q", compact, gotIRI, iri)
		}
	}
}

// TestConstantsLiveInDeclaredNamespace catches typos: every SOSA
// constant must start with Namespace; every SSN constant with
// SSNNamespace.
func TestConstantsLiveInDeclaredNamespace(t *testing.T) {
	sosaConsts := []string{
		Sensor, Observation, FeatureOfInterest, ObservableProperty,
		Platform, Procedure, Sample, SamplingFeature, Sampler, Result, Actuator,
		Actuation, ActuatableProperty,
		Observes, HasFeatureOfInterest, MadeBySensor, MadeObservation,
		UsedProcedure, HasSimpleResult, HasResult, ResultTime,
		PhenomenonTime, Hosts, IsHostedBy, ObservedProperty, HasLocation,
	}
	for _, c := range sosaConsts {
		if !strings.HasPrefix(c, Namespace) {
			t.Errorf("%q does not start with SOSA namespace %q", c, Namespace)
		}
	}
	ssnConsts := []string{
		SSNSystem, SSNDeployment,
		SSNHasDeployment, SSNDeployedSystem, SSNHasSubSystem,
		SSNHasInput, SSNHasOutput,
	}
	for _, c := range ssnConsts {
		if !strings.HasPrefix(c, SSNNamespace) {
			t.Errorf("%q does not start with SSN namespace %q", c, SSNNamespace)
		}
	}
}

func TestIsKnown(t *testing.T) {
	cases := []struct {
		iri  string
		want bool
	}{
		{Observes, true},
		{SSNHasDeployment, true},
		{Namespace + "unmappedButValidSosa", false},
		{"http://example.org/not-sosa", false},
		{"", false},
	}
	for _, c := range cases {
		if got := IsKnown(c.iri); got != c.want {
			t.Errorf("IsKnown(%q) = %v, want %v", c.iri, got, c.want)
		}
	}
}

func TestLocalName(t *testing.T) {
	cases := []struct {
		iri  string
		want string
	}{
		{Observes, "observes"},
		{Sensor, "Sensor"},
		{SSNHasDeployment, "hasDeployment"},
		{SSNSystem, "System"},
		{Namespace + "unmappedButValidSosa", "unmappedButValidSosa"},
		{"http://example.org/not-sosa", ""},
		{"", ""},
	}
	for _, c := range cases {
		if got := LocalName(c.iri); got != c.want {
			t.Errorf("LocalName(%q) = %q, want %q", c.iri, got, c.want)
		}
	}
}

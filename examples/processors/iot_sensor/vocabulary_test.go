package iotsensor

import (
	"testing"

	"github.com/c360studio/semstreams/vocabulary"
)

func TestRegisterVocabularyDeclaresReferenceRuleOutputs(t *testing.T) {
	RegisterVocabulary()

	meta := vocabulary.GetPredicateMetadata(PredicateAlertStateActive)
	if meta == nil {
		t.Fatalf("reference rule predicate %q is not registered", PredicateAlertStateActive)
	}
	if meta.DataType != "string" {
		t.Fatalf("reference rule predicate datatype = %q, want string", meta.DataType)
	}
}

func TestPressureRuleUnitProducesDeclaredPredicate(t *testing.T) {
	reading := &SensorReading{
		DeviceID:      "pressure-001",
		SensorType:    "pressure",
		Value:         12.5,
		Unit:          "psi",
		EntityIDValue: SensorReadingEntityID(testAuthority, "pressure", "pressure-001"),
	}
	if err := reading.Validate(); err != nil {
		t.Fatalf("Validate() failed for pressure rule unit: %v", err)
	}

	triples := reading.Triples()
	if got := triples[0].Predicate; got != PredicateMeasurementPSI {
		t.Fatalf("pressure triple predicate = %q, want %q", got, PredicateMeasurementPSI)
	}
	if vocabulary.GetPredicateMetadata(PredicateMeasurementPSI) == nil {
		t.Fatalf("pressure rule predicate %q is not registered", PredicateMeasurementPSI)
	}
}

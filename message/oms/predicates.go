package oms

import (
	"github.com/c360studio/semstreams/vocabulary"
	"github.com/c360studio/semstreams/vocabulary/sosa"
)

// Dotted-name predicate constants for the SemStreams convention.
// Each is registered to its SOSA IRI at init time so RDF/Turtle
// export through vocabulary/export emits the compacted sosa:
// forms. Mirrors parser/sensorml's predicates.go pattern.
const (
	// PredType is the rdf:type assertion (sosa:Observation IRI).
	PredType = "oms.observation.type"

	// PredHasFeatureOfInterest binds the observation to the
	// SOSA FeatureOfInterest it is about. Maps to
	// sosa:hasFeatureOfInterest.
	PredHasFeatureOfInterest = "oms.observation.hasFeatureOfInterest"

	// PredObservedProperty binds the observation to the SOSA
	// ObservableProperty it measured. Maps to
	// sosa:observedProperty.
	PredObservedProperty = "oms.observation.observedProperty"

	// PredUsedProcedure binds the observation to the SOSA
	// Procedure that produced it. Maps to sosa:usedProcedure.
	PredUsedProcedure = "oms.observation.usedProcedure"

	// PredResultTime carries the ISO 8601 resultTime. Maps to
	// sosa:resultTime.
	PredResultTime = "oms.observation.resultTime"

	// PredPhenomenonTime carries the ISO 8601 phenomenonTime.
	// Maps to sosa:phenomenonTime.
	PredPhenomenonTime = "oms.observation.phenomenonTime"

	// PredHasSimpleResult carries the simple literal Result.
	// Maps to sosa:hasSimpleResult.
	PredHasSimpleResult = "oms.observation.hasSimpleResult"
)

// init registers every dotted predicate this package emits with
// the global vocabulary registry, binding each to its SOSA IRI.
// Idempotent — vocabulary.Register collides loudly only when a
// different IRI was previously bound to the same dotted name.
func init() {
	vocabulary.Register(PredType,
		vocabulary.WithIRI("http://www.w3.org/1999/02/22-rdf-syntax-ns#type"))
	vocabulary.Register(PredHasFeatureOfInterest, vocabulary.WithIRI(sosa.HasFeatureOfInterest))
	vocabulary.Register(PredObservedProperty, vocabulary.WithIRI(sosa.ObservedProperty))
	vocabulary.Register(PredUsedProcedure, vocabulary.WithIRI(sosa.UsedProcedure))
	vocabulary.Register(PredResultTime, vocabulary.WithIRI(sosa.ResultTime))
	vocabulary.Register(PredPhenomenonTime, vocabulary.WithIRI(sosa.PhenomenonTime))
	vocabulary.Register(PredHasSimpleResult, vocabulary.WithIRI(sosa.HasSimpleResult))
}

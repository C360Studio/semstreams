package graph

import (
	"fmt"
	"testing"
	"time"

	"github.com/c360studio/semstreams/internal/semantictest"
	"github.com/c360studio/semstreams/message"
)

// benchmarkEntityStateBytes builds a representative stored EntityState —
// roughly the shape the semboids load generator produces per boid (gh#562):
// a 6-part ID and ~24 triples with realistic three-part predicates — and
// returns its MarshalEntityState bytes.
func benchmarkEntityStateBytes(b *testing.B, prototypes []message.Triple) []byte {
	b.Helper()
	const entityID = "c360.sim.robotics.swarm.boid.017"
	now := time.Date(2026, 7, 1, 12, 0, 0, 0, time.UTC)

	triples := append([]message.Triple(nil), prototypes...)
	for i := range triples {
		triples[i].Subject = entityID
		triples[i].Object = fmt.Sprintf("value-%d", i)
		triples[i].Timestamp = now
		triples[i].Confidence = 1.0
	}

	data, err := MarshalEntityState(&EntityState{
		ID:          entityID,
		Triples:     triples,
		MessageType: message.Type{Domain: "boid", Category: "telemetry", Version: "v1"},
		Version:     42,
		UpdatedAt:   now,
	})
	if err != nil {
		b.Fatalf("MarshalEntityState benchmark fixture: %v", err)
	}
	return data
}

// BenchmarkUnmarshalEntityState measures the validating decoder — the cost
// the per-key-serialized RMW read paid before gh#562.
func BenchmarkUnmarshalEntityState(b *testing.B) {
	data := benchmarkEntityStateBytes(b, []message.Triple{
		{Predicate: semantictest.Predicate(b, "boid", "telemetry", "position-x")},
		{Predicate: semantictest.Predicate(b, "boid", "telemetry", "position-y")},
		{Predicate: semantictest.Predicate(b, "boid", "telemetry", "position-z")},
		{Predicate: semantictest.Predicate(b, "boid", "telemetry", "velocity-x")},
		{Predicate: semantictest.Predicate(b, "boid", "telemetry", "velocity-y")},
		{Predicate: semantictest.Predicate(b, "boid", "telemetry", "velocity-z")},
		{Predicate: semantictest.Predicate(b, "boid", "telemetry", "heading")},
		{Predicate: semantictest.Predicate(b, "boid", "telemetry", "speed")},
		{Predicate: semantictest.Predicate(b, "boid", "state", "phase")},
		{Predicate: semantictest.Predicate(b, "boid", "state", "battery-level")},
		{Predicate: semantictest.Predicate(b, "boid", "state", "flock-role")},
		{Predicate: semantictest.Predicate(b, "boid", "state", "neighbor-count")},
		{Predicate: semantictest.Predicate(b, "entity", "system", "status")},
		{Predicate: semantictest.Predicate(b, "entity", "system", "last-seen")},
		{Predicate: semantictest.Predicate(b, "entity", "indexing", "profile")},
		{Predicate: semantictest.Predicate(b, "boid", "mission", "assignment")},
		{Predicate: semantictest.Predicate(b, "boid", "mission", "waypoint-index")},
		{Predicate: semantictest.Predicate(b, "boid", "mission", "progress")},
		{Predicate: semantictest.Predicate(b, "boid", "sensor", "proximity-min")},
		{Predicate: semantictest.Predicate(b, "boid", "sensor", "proximity-mean")},
		{Predicate: semantictest.Predicate(b, "boid", "config", "max-speed")},
		{Predicate: semantictest.Predicate(b, "boid", "config", "separation-weight")},
		{Predicate: semantictest.Predicate(b, "boid", "config", "alignment-weight")},
		{Predicate: semantictest.Predicate(b, "boid", "config", "cohesion-weight")},
	})
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		var state EntityState
		if err := UnmarshalEntityState(data, &state); err != nil {
			b.Fatal(err)
		}
	}
}

// BenchmarkUnmarshalEntityStateTrusted measures the trusted decoder used by
// the ENTITY_STATES owner's own RMW reads after gh#562. The canonical
// prototypes intentionally remain direct benchmark fixtures: hiding
// semantictest.Predicate behind the byte-builder would evade fixture authority.
func BenchmarkUnmarshalEntityStateTrusted(b *testing.B) {
	data := benchmarkEntityStateBytes(b, []message.Triple{
		{Predicate: semantictest.Predicate(b, "boid", "telemetry", "position-x")},
		{Predicate: semantictest.Predicate(b, "boid", "telemetry", "position-y")},
		{Predicate: semantictest.Predicate(b, "boid", "telemetry", "position-z")},
		{Predicate: semantictest.Predicate(b, "boid", "telemetry", "velocity-x")},
		{Predicate: semantictest.Predicate(b, "boid", "telemetry", "velocity-y")},
		{Predicate: semantictest.Predicate(b, "boid", "telemetry", "velocity-z")},
		{Predicate: semantictest.Predicate(b, "boid", "telemetry", "heading")},
		{Predicate: semantictest.Predicate(b, "boid", "telemetry", "speed")},
		{Predicate: semantictest.Predicate(b, "boid", "state", "phase")},
		{Predicate: semantictest.Predicate(b, "boid", "state", "battery-level")},
		{Predicate: semantictest.Predicate(b, "boid", "state", "flock-role")},
		{Predicate: semantictest.Predicate(b, "boid", "state", "neighbor-count")},
		{Predicate: semantictest.Predicate(b, "entity", "system", "status")},
		{Predicate: semantictest.Predicate(b, "entity", "system", "last-seen")},
		{Predicate: semantictest.Predicate(b, "entity", "indexing", "profile")},
		{Predicate: semantictest.Predicate(b, "boid", "mission", "assignment")},
		{Predicate: semantictest.Predicate(b, "boid", "mission", "waypoint-index")},
		{Predicate: semantictest.Predicate(b, "boid", "mission", "progress")},
		{Predicate: semantictest.Predicate(b, "boid", "sensor", "proximity-min")},
		{Predicate: semantictest.Predicate(b, "boid", "sensor", "proximity-mean")},
		{Predicate: semantictest.Predicate(b, "boid", "config", "max-speed")},
		{Predicate: semantictest.Predicate(b, "boid", "config", "separation-weight")},
		{Predicate: semantictest.Predicate(b, "boid", "config", "alignment-weight")},
		{Predicate: semantictest.Predicate(b, "boid", "config", "cohesion-weight")},
	})
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		var state EntityState
		if err := UnmarshalEntityStateTrusted(data, &state); err != nil {
			b.Fatal(err)
		}
	}
}

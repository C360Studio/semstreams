package graph

import (
	"fmt"
	"testing"
	"time"

	"github.com/c360studio/semstreams/message"
)

// benchmarkEntityStateBytes builds a representative stored EntityState —
// roughly the shape the semboids load generator produces per boid (gh#562):
// a 6-part ID and ~24 triples with realistic three-part predicates — and
// returns its MarshalEntityState bytes.
func benchmarkEntityStateBytes(b *testing.B) []byte {
	b.Helper()
	const entityID = "c360.sim.robotics.swarm.boid.017"
	now := time.Date(2026, 7, 1, 12, 0, 0, 0, time.UTC)

	predicates := []string{
		"boid.telemetry.position-x",
		"boid.telemetry.position-y",
		"boid.telemetry.position-z",
		"boid.telemetry.velocity-x",
		"boid.telemetry.velocity-y",
		"boid.telemetry.velocity-z",
		"boid.telemetry.heading",
		"boid.telemetry.speed",
		"boid.state.phase",
		"boid.state.battery-level",
		"boid.state.flock-role",
		"boid.state.neighbor-count",
		"entity.system.status",
		"entity.system.last-seen",
		"entity.indexing.profile",
		"boid.mission.assignment",
		"boid.mission.waypoint-index",
		"boid.mission.progress",
		"boid.sensor.proximity-min",
		"boid.sensor.proximity-mean",
		"boid.config.max-speed",
		"boid.config.separation-weight",
		"boid.config.alignment-weight",
		"boid.config.cohesion-weight",
	}

	triples := make([]message.Triple, 0, len(predicates))
	for i, predicate := range predicates {
		triples = append(triples, message.Triple{
			Subject:    entityID,
			Predicate:  predicate,
			Object:     fmt.Sprintf("value-%d", i),
			Timestamp:  now,
			Confidence: 1.0,
		})
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
	data := benchmarkEntityStateBytes(b)
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
// the ENTITY_STATES owner's own RMW reads after gh#562.
func BenchmarkUnmarshalEntityStateTrusted(b *testing.B) {
	data := benchmarkEntityStateBytes(b)
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		var state EntityState
		if err := UnmarshalEntityStateTrusted(data, &state); err != nil {
			b.Fatal(err)
		}
	}
}

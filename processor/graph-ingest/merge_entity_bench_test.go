package graphingest

// merge_entity_bench_test.go — gh#562 follow-up: write-path contract-validation
// cost on the per-key-serialized ingest hot path (ADR-072).
//
// semboids' firehose attribution (gh#562, 2026-07-18): MergeEntity's write path
// runs multiple full contract passes per mutation — the explicit
// ValidateEntityStateContract on the candidate at the top of MergeEntity plus
// the MarshalEntityState write gate re-validating the committed output — and
// the merged-result pass is O(all resident triples) on accumulating entities.
//
// BenchmarkMergeEntityCycle measures the full MergeEntity RMW closure cycle
// against the in-package mock KV bucket (no network): trusted decode + merge +
// contract validation + JSON encode + CAS write. Two representative cases:
//
//   - update_24resident_12delta: the semboids boid shape (~24 resident
//     triples, 12-triple telemetry delta).
//   - update_200resident_15delta: an accumulating high-fan-out entity (200
//     resident triples, 15-triple delta) — the O(resident) worst case.
//
// BenchmarkEntityContractValidation isolates the contract-validation share of
// one cycle by calling the validation functions directly on the same shapes:
// full_N is one ValidateEntityStateContract pass over an N-triple entity;
// predicates_N is the ValidateEntityPredicates (ParsePredicate-per-triple)
// share of that pass. Before the gh#562 write-cost fix, one merge cycle pays
// full_delta (top-of-MergeEntity pass) + full_merged (MarshalEntityState
// write gate); the create branch pays full_candidate twice the same way.
import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/c360studio/semstreams/graph"
	"github.com/c360studio/semstreams/internal/semantictest"
	"github.com/c360studio/semstreams/message"
	"github.com/c360studio/semstreams/natsclient"
	"github.com/stretchr/testify/require"

	json "encoding/json"
)

// benchEntityID is a canonical 6-part ID matching the semboids fixture shape.
const benchEntityID = "c360.sim.robotics.swarm.boid.017"

// benchPredicates returns n distinct canonical three-part predicates. The
// first 24 mirror the semboids boid catalog (graph/entity_state_decode_bench);
// beyond 24 the catalog is extended with generated telemetry predicates so the
// accumulating case exercises distinct-predicate validation (the worst case
// for any predicate-level caching).
func benchPredicates(n int) []string {
	catalog := []string{
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
	if n <= len(catalog) {
		return catalog[:n]
	}
	out := make([]string, 0, n)
	out = append(out, catalog...)
	for i := len(catalog); i < n; i++ {
		out = append(out, fmt.Sprintf("boid.metric.reading-%d", i))
	}
	return out
}

// benchEntityState wraps triples constructed authoritatively by the benchmark
// entrypoint in the common entity envelope.
func benchEntityState(triples []message.Triple, version uint64, updatedAt time.Time) *graph.EntityState {
	return &graph.EntityState{
		ID:          benchEntityID,
		Triples:     triples,
		MessageType: message.Type{Domain: "boid", Category: "telemetry", Version: "v1"},
		Version:     version,
		UpdatedAt:   updatedAt,
	}
}

// newBenchComponent mirrors createTestComponentWithMockKVBucket for
// benchmarks: a production-constructed Component whose entityBucket is the
// in-package mock KV (CAS-faithful, in-memory).
func newBenchComponent(b *testing.B) (*Component, *mockKVBucket) {
	b.Helper()

	mockBucket := newMockKVBucket()
	natsClient, err := natsclient.NewClient("nats://localhost:4222")
	require.NoError(b, err)

	configJSON, err := json.Marshal(DefaultConfig())
	require.NoError(b, err)

	comp, err := CreateGraphIngest(configJSON, testDependencies(b, natsClient, withAuthority("c360", "sim")))
	require.NoError(b, err)

	c := comp.(*Component)
	c.entityBucket = natsClient.NewKVStore(mockBucket)
	return c, mockBucket
}

// seedResidentEntity writes the resident entity through the authoritative
// marshal gate directly into the mock bucket.
func seedResidentEntity(b *testing.B, bucket *mockKVBucket, resident *graph.EntityState) {
	b.Helper()
	data, err := graph.MarshalEntityState(resident)
	require.NoError(b, err)
	bucket.mu.Lock()
	bucket.data[resident.ID] = mockKVData{value: data, revision: 1}
	bucket.mu.Unlock()
}

// BenchmarkMergeEntityCycle measures one full MergeEntity RMW cycle
// (trusted decode + predicate-level merge + contract validation + JSON encode
// + CAS write) against the mock KV bucket.
func BenchmarkMergeEntityCycle(b *testing.B) {
	cases := []struct {
		name     string
		resident int
		delta    int
	}{
		{name: "update_24resident_12delta", resident: 24, delta: 12},
		{name: "update_200resident_15delta", resident: 200, delta: 15},
	}
	for _, tc := range cases {
		residentValues := benchPredicates(tc.resident)
		residentTriples := make([]message.Triple, 0, len(residentValues))
		residentTime := time.Date(2026, 7, 1, 12, 0, 0, 0, time.UTC)
		for i, value := range residentValues {
			parts := strings.Split(value, ".")
			require.Len(b, parts, 3)
			object := any(fmt.Sprintf("value-%d", i))
			if value == "entity.indexing.profile" {
				object = "signal"
			}
			residentTriples = append(residentTriples, message.Triple{
				Subject:    benchEntityID,
				Predicate:  semantictest.Predicate(b, parts[0], parts[1], parts[2]),
				Object:     object,
				Timestamp:  residentTime,
				Confidence: 1.0,
			})
		}

		deltaTriples := make([]message.Triple, 0, tc.delta)
		deltaTime := time.Date(2026, 7, 1, 12, 5, 0, 0, time.UTC)
		for _, value := range benchPredicates(tc.delta + 1) {
			if value == "entity.indexing.profile" {
				continue
			}
			parts := strings.Split(value, ".")
			require.Len(b, parts, 3)
			deltaTriples = append(deltaTriples, message.Triple{
				Subject:    benchEntityID,
				Predicate:  semantictest.Predicate(b, parts[0], parts[1], parts[2]),
				Object:     fmt.Sprintf("value-%d", len(deltaTriples)),
				Timestamp:  deltaTime,
				Confidence: 1.0,
			})
			if len(deltaTriples) == tc.delta {
				break
			}
		}

		b.Run(tc.name, func(b *testing.B) {
			c, bucket := newBenchComponent(b)
			seedResidentEntity(b, bucket, benchEntityState(residentTriples, 42, residentTime))
			delta := benchEntityState(deltaTriples, 1, deltaTime)
			ctx := context.Background()

			b.ReportAllocs()
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				if err := c.MergeEntity(ctx, delta); err != nil {
					b.Fatal(err)
				}
			}
		})
	}
}

// BenchmarkEntityContractValidation isolates the contract-validation share of
// one merge cycle. full_N = one complete ValidateEntityStateContract pass over
// an N-triple entity (root ID + per-triple subject + per-triple reference +
// per-triple predicate). predicates_N = the ValidateEntityPredicates
// (ParsePredicate-per-triple) share alone; full_N minus predicates_N
// approximates the entity-ID/subject share.
func BenchmarkEntityContractValidation(b *testing.B) {
	entities := make(map[int]*graph.EntityState)
	for _, n := range []int{12, 15, 24, 200} {
		values := benchPredicates(n)
		triples := make([]message.Triple, 0, len(values))
		now := time.Date(2026, 7, 1, 12, 0, 0, 0, time.UTC)
		for i, value := range values {
			parts := strings.Split(value, ".")
			require.Len(b, parts, 3)
			object := any(fmt.Sprintf("value-%d", i))
			if value == "entity.indexing.profile" {
				object = "signal"
			}
			triples = append(triples, message.Triple{
				Subject:    benchEntityID,
				Predicate:  semantictest.Predicate(b, parts[0], parts[1], parts[2]),
				Object:     object,
				Timestamp:  now,
				Confidence: 1.0,
			})
		}
		entity := benchEntityState(triples, 42, now)
		entities[n] = entity
		b.Run(fmt.Sprintf("full_%d", n), func(b *testing.B) {
			b.ReportAllocs()
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				if err := graph.ValidateEntityStateContract(entity); err != nil {
					b.Fatal(err)
				}
			}
		})
		b.Run(fmt.Sprintf("predicates_%d", n), func(b *testing.B) {
			b.ReportAllocs()
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				if err := graph.ValidateEntityPredicates(entity); err != nil {
					b.Fatal(err)
				}
			}
		})
	}
	// The write gate itself (validation + JSON encode) at merged sizes, so the
	// encode-vs-validate split inside MarshalEntityState is visible.
	for _, n := range []int{24, 200} {
		entity := entities[n]
		b.Run(fmt.Sprintf("marshal_%d", n), func(b *testing.B) {
			b.ReportAllocs()
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				if _, err := graph.MarshalEntityState(entity); err != nil {
					b.Fatal(err)
				}
			}
		})
	}
}

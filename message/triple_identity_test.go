package message

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func identityBaseTriple() Triple {
	return Triple{
		Subject:    "c360.platform.robotics.mav1.drone.001",
		Predicate:  "robotics.battery.level",
		Object:     85,
		Source:     "mavlink_battery",
		Context:    "inference.hierarchy",
		Datatype:   "xsd:int",
		Timestamp:  time.Date(2026, 7, 30, 12, 0, 0, 0, time.UTC),
		Confidence: 1.0,
	}
}

// Task 1.5: the six-field predicate is exactly the six fields — the three
// excluded fields must not split the key, and each of the six must.
func TestAppendIdentityKey_ExcludedFieldsDoNotSplitTheKey(t *testing.T) {
	later := time.Date(2027, 1, 1, 0, 0, 0, 0, time.UTC)
	earlier := time.Date(2020, 1, 1, 0, 0, 0, 0, time.UTC)

	tests := []struct {
		name   string
		mutate func(*Triple)
	}{
		{
			name:   "confidence only",
			mutate: func(tr *Triple) { tr.Confidence = 0.42 },
		},
		{
			name:   "timestamp only",
			mutate: func(tr *Triple) { tr.Timestamp = later },
		},
		{
			name:   "expiry set from nil",
			mutate: func(tr *Triple) { tr.ExpiresAt = &later },
		},
		{
			name: "expiry changed",
			mutate: func(tr *Triple) {
				tr.ExpiresAt = &earlier
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			left := identityBaseTriple()
			right := identityBaseTriple()
			if tt.name == "expiry changed" {
				left.ExpiresAt = &later
			}
			tt.mutate(&right)

			assert.Equal(t, AppendIdentityKey(left), AppendIdentityKey(right),
				"%s must not change add-lane identity", tt.name)
			assert.True(t, SameAppendTuple(left, right),
				"SameAppendTuple must agree with key equality")
		})
	}
}

func TestAppendIdentityKey_EachOfTheSixFieldsSplitsTheKey(t *testing.T) {
	tests := []struct {
		name   string
		mutate func(*Triple)
	}{
		{"subject", func(tr *Triple) { tr.Subject = "c360.platform.robotics.mav1.drone.002" }},
		{"predicate", func(tr *Triple) { tr.Predicate = "robotics.battery.voltage" }},
		{"object", func(tr *Triple) { tr.Object = 86 }},
		{"source", func(tr *Triple) { tr.Source = "mavlink_status" }},
		{"context", func(tr *Triple) { tr.Context = "inference.other" }},
		{"datatype", func(tr *Triple) { tr.Datatype = "xsd:float" }},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			left := identityBaseTriple()
			right := identityBaseTriple()
			tt.mutate(&right)

			assert.NotEqual(t, AppendIdentityKey(left), AppendIdentityKey(right),
				"a differing %s must be a different assertion", tt.name)
			assert.False(t, SameAppendTuple(left, right))
		})
	}
}

// Task 1.3: int(85) and float64(85) are the same stored fact once the triple
// has round-tripped through JSON into ENTITY_STATES, so they must be one key.
func TestAppendIdentityKey_ObjectCanonicalizationMatchesNumericWidening(t *testing.T) {
	stored := identityBaseTriple()
	stored.Object = float64(85) // what a JSON round-trip yields
	incoming := identityBaseTriple()
	incoming.Object = int(85) // what a Go producer emits

	assert.Equal(t, AppendIdentityKey(stored), AppendIdentityKey(incoming),
		"int(85) and float64(85) must share one key, not split the keyspace")

	different := identityBaseTriple()
	different.Object = float64(85.5)
	assert.NotEqual(t, AppendIdentityKey(stored), AppendIdentityKey(different))

	// A numeric 85 and the STRING "85" are different assertions.
	stringy := identityBaseTriple()
	stringy.Object = "85"
	assert.NotEqual(t, AppendIdentityKey(stored), AppendIdentityKey(stringy),
		"a numeric object and its decimal string are not the same fact")
}

// Task 1.4: the key is length-prefixed, so no field content can be arranged to
// collide with a different field split. A dot- or pipe-joined key would fail
// every case here — this is the gh#741 raw-key-collision class.
func TestAppendIdentityKey_NoCollisionAcrossFieldBoundaries(t *testing.T) {
	tests := []struct {
		name  string
		left  func(*Triple)
		right func(*Triple)
	}{
		{
			name:  "dot delimiter embedded in context",
			left:  func(tr *Triple) { tr.Source = "a.b"; tr.Context = "c" },
			right: func(tr *Triple) { tr.Source = "a"; tr.Context = "b.c" },
		},
		{
			name:  "pipe delimiter embedded in context",
			left:  func(tr *Triple) { tr.Source = "a|b"; tr.Context = "c" },
			right: func(tr *Triple) { tr.Source = "a"; tr.Context = "b|c" },
		},
		{
			name:  "NUL delimiter embedded in context",
			left:  func(tr *Triple) { tr.Source = "a\x00b"; tr.Context = "c" },
			right: func(tr *Triple) { tr.Source = "a"; tr.Context = "b\x00c" },
		},
		{
			name:  "digits adjacent to a length prefix",
			left:  func(tr *Triple) { tr.Source = "1"; tr.Context = "2x" },
			right: func(tr *Triple) { tr.Source = "1\x002"; tr.Context = "x" },
		},
		{
			name:  "empty field versus shifted content",
			left:  func(tr *Triple) { tr.Source = ""; tr.Context = "ab" },
			right: func(tr *Triple) { tr.Source = "ab"; tr.Context = "" },
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			left := identityBaseTriple()
			right := identityBaseTriple()
			tt.left(&left)
			tt.right(&right)

			require.NotEqual(t, left.Source+"\x1f"+left.Context, "",
				"fixture sanity: fields are populated")
			assert.NotEqual(t, AppendIdentityKey(left), AppendIdentityKey(right),
				"%s must not collide — a delimiter-joined key would", tt.name)
			assert.False(t, SameAppendTuple(left, right))
		})
	}
}

func TestDedupeAppendTriples(t *testing.T) {
	base := identityBaseTriple()
	other := identityBaseTriple()
	other.Predicate = "robotics.battery.voltage"
	third := identityBaseTriple()
	third.Predicate = "robotics.battery.temperature"

	// A duplicate whose EXCLUDED fields differ still collapses.
	restamped := identityBaseTriple()
	restamped.Timestamp = time.Now()
	restamped.Confidence = 0.5

	tests := []struct {
		name              string
		stored            []Triple
		incoming          []Triple
		wantSurvivors     []string // predicates, in expected order
		wantSuppressed    int
		wantSurvivorCount int
	}{
		{
			name:              "nothing stored, nothing duplicated",
			incoming:          []Triple{base, other},
			wantSurvivors:     []string{base.Predicate, other.Predicate},
			wantSuppressed:    0,
			wantSurvivorCount: 2,
		},
		{
			name:              "all already stored",
			stored:            []Triple{base, other},
			incoming:          []Triple{restamped, other},
			wantSurvivors:     nil,
			wantSuppressed:    2,
			wantSurvivorCount: 0,
		},
		{
			name:              "within-request repeats collapse preserving first-input order",
			incoming:          []Triple{base, other, base, third, base},
			wantSurvivors:     []string{base.Predicate, other.Predicate, third.Predicate},
			wantSuppressed:    2,
			wantSurvivorCount: 3,
		},
		{
			name:              "partial: one stored, one new",
			stored:            []Triple{base},
			incoming:          []Triple{base, other},
			wantSurvivors:     []string{other.Predicate},
			wantSuppressed:    1,
			wantSurvivorCount: 1,
		},
		{
			name:              "empty incoming",
			stored:            []Triple{base},
			incoming:          nil,
			wantSurvivors:     nil,
			wantSuppressed:    0,
			wantSurvivorCount: 0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			survivors, suppressed := DedupeAppendTriples(tt.stored, tt.incoming)
			assert.Equal(t, tt.wantSuppressed, suppressed)
			require.Len(t, survivors, tt.wantSurvivorCount)
			for i, predicate := range tt.wantSurvivors {
				assert.Equal(t, predicate, survivors[i].Predicate, "survivor order must follow first input")
			}
			assert.Equal(t, len(tt.incoming), len(survivors)+suppressed,
				"written plus suppressed must account for the whole request")
		})
	}
}

// A pre-existing entity carrying DUPLICATE stored triples still suppresses a
// re-assert (the no-backfill posture: old duplicates are readable, not grown).
func TestDedupeAppendTriples_StoredDuplicatesStillSuppress(t *testing.T) {
	base := identityBaseTriple()
	survivors, suppressed := DedupeAppendTriples([]Triple{base, base}, []Triple{base})
	assert.Empty(t, survivors)
	assert.Equal(t, 1, suppressed)
}

func TestCanonicalObjectKey_UnmarshalableFallbackStaysDistinct(t *testing.T) {
	left := identityBaseTriple()
	left.Object = make(chan int)
	right := identityBaseTriple()
	right.Object = make(chan int)

	// Conservative: two unmarshalable values are NOT collapsed, so nothing is
	// silently suppressed on a path that could not have been persisted anyway.
	assert.NotEqual(t, AppendIdentityKey(left), AppendIdentityKey(right))
	assert.Equal(t, AppendIdentityKey(left), AppendIdentityKey(left))
}

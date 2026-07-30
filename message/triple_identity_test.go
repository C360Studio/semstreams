package message

import (
	"encoding/json"
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

// zebraFirst declares Zebra before Alpha, so encoding/json emits it in that
// order while the map[string]any it decodes into emits sorted keys.
type zebraFirst struct {
	Zebra string `json:"zebra"`
	Alpha int    `json:"alpha"`
}

// A structured Object must key identically whether it arrives as a Go struct
// or as the map[string]any form a JSON round-trip through ENTITY_STATES
// produces. Without normalization a producer replaying a struct-valued triple
// re-appends it on every restart — the exact corruption this key prevents.
func TestAppendIdentityKey_StructAndItsPersistedFormShareOneKey(t *testing.T) {
	structValue := zebraFirst{Zebra: "z", Alpha: 1}

	encoded, err := json.Marshal(structValue)
	require.NoError(t, err)
	var persisted any
	require.NoError(t, json.Unmarshal(encoded, &persisted))

	require.NotEqual(t, string(encoded), mustMarshalString(t, persisted),
		"fixture sanity: the struct and map encodings must actually differ in field order, "+
			"or this test proves nothing")

	inProcess := identityBaseTriple()
	inProcess.Object = structValue
	stored := identityBaseTriple()
	stored.Object = persisted

	assert.Equal(t, AppendIdentityKey(stored), AppendIdentityKey(inProcess),
		"a struct and its persisted map form are one fact and must share one key")
	assert.True(t, SameAppendTuple(stored, inProcess))

	// Normalization must not erase real differences.
	different := identityBaseTriple()
	different.Object = zebraFirst{Zebra: "z", Alpha: 2}
	assert.NotEqual(t, AppendIdentityKey(inProcess), AppendIdentityKey(different))
}

// Nested and slice-valued objects normalize too, and slices keep their order
// (order IS meaning in a list, unlike map key order).
func TestAppendIdentityKey_NestedAndSliceObjectsNormalize(t *testing.T) {
	nested := map[string]any{"outer": zebraFirst{Zebra: "z", Alpha: 1}}
	encoded, err := json.Marshal(nested)
	require.NoError(t, err)
	var persisted any
	require.NoError(t, json.Unmarshal(encoded, &persisted))

	left := identityBaseTriple()
	left.Object = nested
	right := identityBaseTriple()
	right.Object = persisted
	assert.Equal(t, AppendIdentityKey(left), AppendIdentityKey(right),
		"a nested struct must normalize at every level")

	ordered := identityBaseTriple()
	ordered.Object = []string{"a", "b"}
	reversed := identityBaseTriple()
	reversed.Object = []string{"b", "a"}
	assert.NotEqual(t, AppendIdentityKey(ordered), AppendIdentityKey(reversed),
		"slice order is meaning and must not be normalized away")
}

func mustMarshalString(t *testing.T, v any) string {
	t.Helper()
	data, err := json.Marshal(v)
	require.NoError(t, err)
	return string(data)
}

// A scalar must key identically to its own persisted form, exactly as a value
// inside a container does. int64 above 2^53 is where a fast path that skips
// normalization diverges: the raw value encodes as 9007199254740993, while the
// same value read back out of ENTITY_STATES has been through JSON's float64
// and encodes as 9007199254740992. Two keys for one fact means a producer
// replaying that triple re-appends it on every restart — gh#713's failure mode
// surviving for one value class.
func TestAppendIdentityKey_ScalarKeysMatchTheirPersistedForm(t *testing.T) {
	persistedForm := func(object any) any {
		encoded, err := json.Marshal(object)
		require.NoError(t, err)
		var decoded any
		require.NoError(t, json.Unmarshal(encoded, &decoded))
		return decoded
	}

	tests := []struct {
		name   string
		object any
	}{
		{"int64 above float64's exact range", int64(9007199254740993)},
		{"int above float64's exact range", int(9007199254740993)},
		{"uint64 above float64's exact range", uint64(18446744073709551615)},
		{"max int64", int64(9223372036854775807)},
		{"small int", 85},
		{"float64", 85.5},
		{"string", "c360.platform.robotics.mav1.drone.001"},
		{"bool", true},
		{"nil", nil},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			inProcess := identityBaseTriple()
			inProcess.Object = tt.object
			stored := identityBaseTriple()
			stored.Object = persistedForm(tt.object)

			assert.Equal(t, AppendIdentityKey(stored), AppendIdentityKey(inProcess),
				"a value and its persisted form are one fact and must share one key")
		})
	}
}

// The scalar fast path and the normalizing path must not disagree: the same
// value must key the same whether it arrives bare or inside a container.
func TestAppendIdentityKey_ScalarAndInContainerAgree(t *testing.T) {
	big := int64(9007199254740993)

	bare := identityBaseTriple()
	bare.Object = big
	bareStored := identityBaseTriple()
	bareStored.Object = float64(big) // what the store returns

	inSlice := identityBaseTriple()
	inSlice.Object = []any{big}
	inSliceStored := identityBaseTriple()
	inSliceStored.Object = []any{float64(big)}

	bareMatches := AppendIdentityKey(bare) == AppendIdentityKey(bareStored)
	sliceMatches := AppendIdentityKey(inSlice) == AppendIdentityKey(inSliceStored)
	assert.Equal(t, sliceMatches, bareMatches,
		"a bare scalar and the same scalar inside a container must follow the same rule; "+
			"one path suppressing and the other not is a contradiction")
	assert.True(t, bareMatches, "and both must suppress")
}

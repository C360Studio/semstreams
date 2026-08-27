package types

import (
	"errors"
	"regexp"
	"strings"
	"testing"

	"github.com/c360studio/semstreams/pkg/errs"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestValidateEntityIDContract(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		value      string
		valid      bool
		wantReason string
		wantIndex  any
	}{
		{name: "canonical", value: "Acme.ops2.robotics.gcs_1.drone-type.001", valid: true},
		{name: "allowed remaining bytes", value: "a.A0_-.d.s.t.i", valid: true},
		{name: "empty", value: "", wantReason: EntityIDReasonEmpty},
		{name: "five parts", value: "a.b.c.d.e", wantReason: EntityIDReasonArity},
		{name: "seven parts", value: "a.b.c.d.e.f.g", wantReason: EntityIDReasonArity},
		{name: "empty segment", value: "a..c.d.e.f", wantReason: EntityIDReasonEmptySegment, wantIndex: 1},
		{name: "leading underscore", value: "a._b.c.d.e.f", wantReason: EntityIDReasonFirstByte, wantIndex: 1},
		{name: "leading hyphen", value: "a.b.-c.d.e.f", wantReason: EntityIDReasonFirstByte, wantIndex: 2},
		{name: "unicode", value: "a.b.c.d.e.fé", wantReason: EntityIDReasonAlphabet, wantIndex: 5},
		{name: "space", value: "a.b.c.d.e.f x", wantReason: EntityIDReasonAlphabet, wantIndex: 5},
		{name: "slash", value: "a.b.c.d.e.f/x", wantReason: EntityIDReasonAlphabet, wantIndex: 5},
		{name: "complete star", value: "a.b.c.d.e.*", wantReason: EntityIDReasonFirstByte, wantIndex: 5},
		{name: "complete greater", value: "a.b.c.d.e.>", wantReason: EntityIDReasonFirstByte, wantIndex: 5},
		{name: "embedded star", value: "a.b.c.d.e.f*", wantReason: EntityIDReasonAlphabet, wantIndex: 5},
		{name: "embedded greater", value: "a.b.c.d.e.f>", wantReason: EntityIDReasonAlphabet, wantIndex: 5},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := ValidateEntityID(tt.value)
			assert.Equal(t, tt.valid, err == nil)
			assert.Equal(t, tt.valid, IsValidEntityID(tt.value))
			if tt.valid {
				return
			}
			assertEntityIDContractError(t, err, ErrorCodeEntityIDInvalid, tt.wantReason, tt.wantIndex)
		})
	}
}

func TestValidateEntityIDByteBoundary(t *testing.T) {
	t.Parallel()

	for _, size := range []int{255, 256} {
		value := entityIDWithBytes(size)
		require.Len(t, value, size)
		require.NoError(t, ValidateEntityID(value))
		parsed, err := ParseEntityID(value)
		require.NoError(t, err)
		assert.Equal(t, value, parsed.Key())
		assert.Equal(t, size-10, len(parsed.Instance), "only the serialized total is bounded")
	}

	value := entityIDWithBytes(257)
	err := ValidateEntityID(value)
	assertEntityIDContractError(t, err, ErrorCodeEntityIDInvalid, EntityIDReasonBytes, nil)
	var classified *errs.ClassifiedError
	require.ErrorAs(t, err, &classified)
	assert.Equal(t, 257, classified.Detail[EntityIDDetailMeasuredBytes])
	assert.Equal(t, MaxEntityIDBytes, classified.Detail[EntityIDDetailAllowedBytes])
}

func TestEntityIDFailurePrecedence(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		value      string
		wantReason string
		wantIndex  any
	}{
		{name: "empty before all", value: "", wantReason: EntityIDReasonEmpty},
		{name: "bytes before arity and alphabet", value: strings.Repeat("x", 257) + ".*", wantReason: EntityIDReasonBytes},
		{name: "arity before segment faults", value: "a..c.*", wantReason: EntityIDReasonArity},
		{name: "empty segment before first byte", value: "a..c.-d.e.f", wantReason: EntityIDReasonEmptySegment, wantIndex: 1},
		{name: "first byte before later alphabet", value: "a._b.c.d.e.f/", wantReason: EntityIDReasonFirstByte, wantIndex: 1},
		{name: "leftmost alphabet", value: "a.b/c.d.e.f.g>", wantReason: EntityIDReasonAlphabet, wantIndex: 1},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := ValidateEntityID(tt.value)
			assertEntityIDContractError(t, err, ErrorCodeEntityIDInvalid, tt.wantReason, tt.wantIndex)
			var classified *errs.ClassifiedError
			require.ErrorAs(t, err, &classified)
			assert.NotContains(t, classified.Detail, tt.value)
		})
	}
}

func TestParseEntityIDAndStructValidationShareAuthority(t *testing.T) {
	t.Parallel()

	original := "Acme.ops.robotics.gcs_1.drone-type.001"
	parsed, err := ParseEntityID(original)
	require.NoError(t, err)
	assert.Equal(t, original, parsed.String())
	assert.True(t, parsed.IsValid())
	assert.False(t, (EntityID{Org: "-bad", Platform: "p", System: "s", Domain: "d", Type: "t", Instance: "i"}).IsValid()) // entity-id-audit:classify intentional-malformed "-bad.p.s.d.t.i" line=112 column=19 surface=go-constructor:EntityID entity_id_invalid:first_byte constructor rejection fixture

	_, err = ParseEntityID("a.b.c.d.e") // entity-id-audit:classify intentional-malformed "a.b.c.d.e" line=114 column=25 surface=go-call:ParseEntityID entity_id_invalid:arity parser rejection fixture
	assertEntityIDContractError(t, err, ErrorCodeEntityIDInvalid, EntityIDReasonArity, nil)
}

func TestValidateEntityIDPattern(t *testing.T) {
	t.Parallel()

	tests := []struct {
		value      string
		valid      bool
		wantReason string
	}{
		{value: "acme.*.robotics.gcs.drone.*", valid: true},
		{value: "a.b.c.d.e.f", valid: true},
		{value: entityIDWithBytes(256), valid: true},
		{value: entityIDWithBytes(257), wantReason: EntityIDReasonBytes},
		{value: "a.b.c.d.e", wantReason: EntityIDReasonArity},
		{value: "a.b.c.d.e.>", wantReason: EntityIDReasonFirstByte},
		{value: "a.b.c.d.e.foo*", wantReason: EntityIDReasonAlphabet},
		{value: "a.b.c.d.e.*bar", wantReason: EntityIDReasonFirstByte},
		{value: "a.b.c.d..*", wantReason: EntityIDReasonEmptySegment},
		{value: "a.b.c.d.é.*", wantReason: EntityIDReasonFirstByte},
	}
	for _, tt := range tests {
		t.Run(tt.value, func(t *testing.T) {
			err := ValidateEntityIDPattern(tt.value)
			assert.Equal(t, tt.valid, err == nil)
			if tt.valid {
				if !strings.Contains(tt.value, "*") {
					assert.NoError(t, ValidateEntityID(tt.value))
				}
				return
			}
			assertEntityIDContractError(t, err, ErrorCodeEntityIDPatternInvalid, tt.wantReason, nil)
		})
	}
	assert.Error(t, ValidateEntityID("acme.*.robotics.gcs.drone.*")) // entity-id-audit:classify intentional-malformed "acme.*.robotics.gcs.drone.*" line=150 column=35 surface=go-call:ValidateEntityID entity_id_invalid:first_byte concrete ID rejects pattern fixture
}

func TestMatchEntityIDPattern(t *testing.T) {
	t.Parallel()

	matched, err := MatchEntityIDPattern(
		"acme.*.robotics.*.drone.*",
		"acme.prod.robotics.gcs.drone.d007",
	)
	require.NoError(t, err)
	require.True(t, matched)

	matched, err = MatchEntityIDPattern(
		"acme.*.environmental.*.sensor.*",
		"acme.prod.robotics.gcs.drone.d007",
	)
	require.NoError(t, err)
	require.False(t, matched)
}

func TestValidateEntityIDPrefix(t *testing.T) {
	t.Parallel()

	for _, valid := range []string{"a", "a.b", "a.b.c", "a.b.c.d", "a.b.c.d.e", "a.b.c.d.e.f", entityIDWithBytes(256)} {
		require.NoError(t, ValidateEntityIDPrefix(valid), valid)
	}
	tests := []struct {
		value      string
		wantReason string
	}{
		{value: "", wantReason: EntityIDReasonEmpty},
		{value: entityIDWithBytes(257), wantReason: EntityIDReasonBytes},
		{value: "a.b.c.d.e.f.g", wantReason: EntityIDReasonArity},
		{value: "a.b.", wantReason: EntityIDReasonEmptySegment},
		{value: "a.*", wantReason: EntityIDReasonFirstByte},
		{value: "a.foo*", wantReason: EntityIDReasonAlphabet},
		{value: "a.é", wantReason: EntityIDReasonFirstByte},
	}
	for _, tt := range tests {
		t.Run(tt.value, func(t *testing.T) {
			err := ValidateEntityIDPrefix(tt.value)
			assertEntityIDContractError(t, err, ErrorCodeEntityIDPrefixInvalid, tt.wantReason, nil)
		})
	}
}

func TestEntityIDSchemaPatterns(t *testing.T) {
	t.Parallel()

	full := regexp.MustCompile(EntityIDLiteralPattern)
	declaration := regexp.MustCompile(EntityIDDeclarationPattern)
	prefix := regexp.MustCompile(EntityIDLiteralPrefixPattern)
	optional := regexp.MustCompile(OptionalEntityIDLiteralPattern)
	require.True(t, declaration.MatchString("acme.*.robotics.gcs.drone.*"))
	require.False(t, declaration.MatchString("acme.ops.robotics.>"))
	require.False(t, declaration.MatchString("acme.ops.robotics.gcs.drøne.1"))

	for _, value := range []string{"a.b.c.d.e.f", "Acme.ops2.robotics.gcs_1.drone-type.001", entityIDWithBytes(256)} {
		assert.True(t, full.MatchString(value), value)
		assert.True(t, optional.MatchString(value), value)
		require.NoError(t, ValidateEntityID(value))
	}
	for _, value := range []string{"", "a.b.c", "a.b.c.d.e.*", `a\.b.c.d.e.f`} {
		assert.False(t, full.MatchString(value), value)
	}
	assert.True(t, optional.MatchString(""), "optional schema sentinel")

	for _, value := range []string{"a", "a.b.c", "a.b.c.d.e.f", entityIDWithBytes(256)} {
		assert.True(t, prefix.MatchString(value), value)
		require.NoError(t, ValidateEntityIDPrefix(value))
	}
	for _, value := range []string{"", "a.*", "a.b.c.d.e.f.g", `a\.b`} {
		assert.False(t, prefix.MatchString(value), value)
	}
}

func FuzzParseEntityIDRoundTrip(f *testing.F) {
	for _, seed := range []string{"a.b.c.d.e.f", entityIDWithBytes(256), "a..c.d.e.f", "a.b.c.d.e.fé", ""} {
		f.Add(seed)
	}
	f.Fuzz(func(t *testing.T, input string) {
		first := ValidateEntityID(input)
		second := ValidateEntityID(input)
		assert.Equal(t, contractErrorShape(first), contractErrorShape(second))
		parsed, err := ParseEntityID(input)
		assert.Equal(t, first == nil, err == nil)
		assert.Equal(t, first == nil, parsed.IsValid())
		if err == nil {
			assert.Equal(t, input, parsed.Key())
		}
	})
}

func entityIDWithBytes(size int) string {
	return "a.a.a.a.a." + strings.Repeat("x", size-10)
}

func assertEntityIDContractError(t *testing.T, err error, code, reason string, index any) {
	t.Helper()
	require.Error(t, err)
	assert.True(t, errs.IsInvalid(err))
	var classified *errs.ClassifiedError
	require.True(t, errors.As(err, &classified))
	assert.Equal(t, code, classified.Code)
	assert.Equal(t, reason, classified.Detail[EntityIDDetailReason])
	if index != nil {
		assert.Equal(t, index, classified.Detail[EntityIDDetailSegmentIndex])
	}
}

func contractErrorShape(err error) any {
	if err == nil {
		return nil
	}
	var classified *errs.ClassifiedError
	if !errors.As(err, &classified) {
		return err.Error()
	}
	return struct {
		Code   string
		Detail string
	}{Code: classified.Code, Detail: classified.Detail[EntityIDDetailReason].(string)}
}

package types

import (
	"errors"
	"strings"
	"testing"

	"github.com/c360studio/semstreams/pkg/errs"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestAuthorityRejectionIsCodedAndIdentityFree pins the coded authority
// rejection: distinct from structural rejection, details carry exactly
// reason, segment_index and lane, and no detail echoes identity bytes.
func TestAuthorityRejectionIsCodedAndIdentityFree(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		candidate  string
		org        string
		platform   string
		importLane bool
		wantReason string
		wantIndex  int
		wantLane   string
	}{
		{name: "foreign platform on local lane", candidate: "acme.dep2.src.git.commit.a1", org: "acme", platform: "dep1", wantReason: EntityIDReasonForeignAuthority, wantIndex: 1, wantLane: EntityIDLaneLocal},
		{name: "foreign org on local lane", candidate: "other.dep1.src.git.commit.a1", org: "acme", platform: "dep1", wantReason: EntityIDReasonForeignAuthority, wantIndex: 0, wantLane: EntityIDLaneLocal},
		{name: "local on local lane", candidate: "acme.dep1.src.git.commit.a1", org: "acme", platform: "dep1"},
		{name: "foreign on import lane", candidate: "acme.dep2.src.git.commit.a1", org: "acme", platform: "dep1", importLane: true},
		{name: "case is identity", candidate: "Acme.dep1.src.git.commit.a1", org: "acme", platform: "dep1", wantReason: EntityIDReasonForeignAuthority, wantIndex: 0, wantLane: EntityIDLaneLocal},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			err := ValidateEntityIDAuthority(tt.candidate, tt.org, tt.platform, tt.importLane)
			if tt.wantReason == "" {
				require.NoError(t, err)
				return
			}
			require.Error(t, err)
			var classified *errs.ClassifiedError
			require.True(t, errors.As(err, &classified))
			assert.Equal(t, ErrorCodeEntityIDAuthorityInvalid, classified.Code)
			assert.Equal(t, errs.ErrorInvalid, classified.Class)
			assert.Equal(t, tt.wantReason, classified.Detail[EntityIDDetailReason])
			assert.Equal(t, tt.wantIndex, classified.Detail[EntityIDDetailSegmentIndex])
			assert.Equal(t, tt.wantLane, classified.Detail[EntityIDDetailLane])
			assert.Len(t, classified.Detail, 3, "details are exactly reason, segment_index, lane: %v", classified.Detail)
			for key, value := range classified.Detail {
				if text, ok := value.(string); ok {
					assert.False(t, strings.Contains(text, "."), "detail %s=%q carries a dot-joined identity", key, text)
					assert.NotContains(t, text, tt.candidate)
				}
			}
			assert.NotContains(t, err.Error(), tt.candidate, "error text must not echo the identity")
		})
	}
}

// TestAuthorityRejectionLocalClaimOnImportLane pins that a candidate equal to
// the local pair arriving on an import lane is rejected with
// local_authority_claimed.
func TestAuthorityRejectionLocalClaimOnImportLane(t *testing.T) {
	t.Parallel()

	err := ValidateEntityIDAuthority("acme.dep1.src.git.commit.a1", "acme", "dep1", true)
	require.Error(t, err)
	var classified *errs.ClassifiedError
	require.True(t, errors.As(err, &classified))
	assert.Equal(t, ErrorCodeEntityIDAuthorityInvalid, classified.Code)
	assert.Equal(t, EntityIDReasonLocalAuthorityClaimed, classified.Detail[EntityIDDetailReason])
	assert.Equal(t, EntityIDLaneImport, classified.Detail[EntityIDDetailLane])
	assert.Equal(t, 1, classified.Detail[EntityIDDetailSegmentIndex])
}

// TestAuthorityValidationRunsStructuralFirst pins that an authority reason
// never masks a structural one, and that an empty local pair is a caller
// fault, not a wildcard.
func TestAuthorityValidationRunsStructuralFirst(t *testing.T) {
	t.Parallel()

	err := ValidateEntityIDAuthority("acme.dep2.src.git.commit", "acme", "dep1", false)
	assertEntityIDContractError(t, err, ErrorCodeEntityIDInvalid, EntityIDReasonArity, nil)

	err = ValidateEntityIDAuthority("acme.dep1.src.git.commit.a1", "", "dep1", false)
	require.Error(t, err)
	var classified *errs.ClassifiedError
	require.True(t, errors.As(err, &classified))
	assert.Equal(t, ErrorCodeEntityIDAuthorityInvalid, classified.Code)
	assert.Equal(t, EntityIDReasonForeignAuthority, classified.Detail[EntityIDDetailReason])
}

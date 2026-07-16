package graphingest

import (
	"errors"
	"strings"
	"testing"

	"github.com/c360studio/semstreams/pkg/errs"
	semtypes "github.com/c360studio/semstreams/pkg/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestValidateEntityIDDelegatesCanonicalBoundary(t *testing.T) {
	t.Parallel()

	valid := "a.a.a.a.a." + strings.Repeat("x", 246)
	require.Len(t, valid, 256)
	assert.NoError(t, validateEntityID(valid))

	invalid := valid + "x"
	err := validateEntityID(invalid)
	require.Error(t, err)
	var classified *errs.ClassifiedError
	require.True(t, errors.As(err, &classified))
	assert.Equal(t, semtypes.ErrorCodeEntityIDInvalid, classified.Code)
	assert.Equal(t, semtypes.EntityIDReasonBytes, classified.Detail[semtypes.EntityIDDetailReason])
	assert.Equal(t, semtypes.IsValidEntityID(invalid), err == nil)
}

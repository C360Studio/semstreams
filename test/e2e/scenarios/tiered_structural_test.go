package scenarios

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestValidateCompoundPredicateCoverage(t *testing.T) {
	const known = "c360.logistics.environmental.sensor.temperature.temp-sensor-001"
	t.Run("non-empty intersection", func(t *testing.T) {
		require.NoError(t, validateCompoundPredicateCoverage(10, 3, []string{known, "other"}, known))
	})
	t.Run("empty intersection is not coverage", func(t *testing.T) {
		require.Error(t, validateCompoundPredicateCoverage(10, 0, nil, known))
	})
	t.Run("intersection cannot exceed union", func(t *testing.T) {
		require.Error(t, validateCompoundPredicateCoverage(2, 3, []string{known}, known))
	})
	t.Run("known fixture must be present", func(t *testing.T) {
		require.Error(t, validateCompoundPredicateCoverage(10, 3, []string{"other"}, known))
	})
}

package scenarios

import (
	"testing"

	"github.com/c360studio/semstreams/test/e2e/config"
	"github.com/stretchr/testify/require"
)

func TestValidateCompoundPredicateCoverage(t *testing.T) {
	known := config.TierEntityID(config.VariantStructural, "sensor.environmental.temperature.temp-sensor-001")
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

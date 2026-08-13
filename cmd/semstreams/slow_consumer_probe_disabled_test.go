//go:build !e2e_slow_consumer

package main

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestSlowConsumerProbeIsInertWithoutTag(t *testing.T) {
	require.NoError(t, runSlowConsumerProbe(context.Background(), nil))
}

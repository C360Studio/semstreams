package errs

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestNewShutdownErrorPreservesOwnerPhaseAndCause(t *testing.T) {
	err := NewShutdownError("graph-gateway", PhaseShutdownListener, context.DeadlineExceeded)
	var shutdownErr *ShutdownError
	require.ErrorAs(t, err, &shutdownErr)
	require.Equal(t, "graph-gateway", shutdownErr.Owner)
	require.Equal(t, PhaseShutdownListener, shutdownErr.Phase)
	require.ErrorIs(t, err, context.DeadlineExceeded)
}

func TestNewShutdownErrorRejectsInvalidShape(t *testing.T) {
	wantErr := errors.New("failed")
	for _, test := range []struct {
		name  string
		owner string
		phase ShutdownPhase
	}{
		{name: "empty owner", phase: PhaseJoinRuntime},
		{name: "empty phase", owner: "owner"},
		{name: "unknown phase", owner: "owner", phase: ShutdownPhase("unknown")},
	} {
		t.Run(test.name, func(t *testing.T) {
			err := NewShutdownError(test.owner, test.phase, wantErr)
			require.Error(t, err)
			var shutdownErr *ShutdownError
			require.NotErrorAs(t, err, &shutdownErr)
		})
	}
}

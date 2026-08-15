package service

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func startPostBootComponentManager(t *testing.T, cm *ComponentManager) {
	t.Helper()
	cm.initialized.Store(true)
	require.NoError(t, cm.Start(t.Context()))
	t.Cleanup(func() {
		stopCtx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer cancel()
		_ = cm.Stop(stopCtx)
	})
}

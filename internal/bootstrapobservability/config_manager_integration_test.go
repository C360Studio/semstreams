//go:build integration

package bootstrapobservability

import (
	"context"
	"io"
	"log/slog"
	"testing"
	"time"

	"github.com/c360studio/semstreams/config"
	"github.com/c360studio/semstreams/natsclient"
	"github.com/c360studio/semstreams/types"
	"github.com/stretchr/testify/require"
)

func TestStartValidatedConfigManagerPropagatesForeignPlatformIdentityMismatch(t *testing.T) {
	testClient := natsclient.NewTestClient(t, natsclient.WithJetStream(), natsclient.WithKV())
	ctx, cancel := context.WithTimeout(t.Context(), 20*time.Second)
	defer cancel()
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))

	foreign := &config.Config{
		Version:  "1.0.0",
		Platform: config.PlatformConfig{Org: "foreign", ID: "existing", Type: "test"},
		Services: make(types.ServiceConfigs),
	}
	seed, err := config.NewConfigManager(foreign, testClient.Client, logger)
	require.NoError(t, err)
	require.NoError(t, seed.Start(ctx))
	require.NoError(t, seed.Stop(5*time.Second))

	local := &config.Config{
		Version:  "1.0.0",
		Platform: config.PlatformConfig{Org: "local", ID: "candidate", Type: "test"},
		Services: make(types.ServiceConfigs),
	}
	manager, effective, err := StartValidatedConfigManager(ctx, local, testClient.Client, logger)
	require.Nil(t, manager)
	require.Nil(t, effective)
	require.ErrorContains(t, err, "start config manager: config bucket platform identity mismatch")
	require.ErrorContains(t, err, `local org="local" platform="candidate"`)
	require.ErrorContains(t, err, `stored org="foreign" platform="existing"`)
}

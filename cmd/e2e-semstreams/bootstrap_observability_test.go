package main

import (
	"bytes"
	"encoding/json"
	"log/slog"
	"testing"

	"github.com/c360studio/semstreams/config"
	"github.com/c360studio/semstreams/internal/bootstrapobservability"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestCreateNATSClientUsesE2EPhaseAObservability(t *testing.T) {
	t.Setenv("SEMSTREAMS_NATS_URLS", "")
	var output bytes.Buffer
	metrics, phase, err := bootstrapobservability.NewE2EPhaseA(
		&output, "debug", "json",
		[]slog.Attr{slog.String("service", "e2e-semstreams"), slog.String("version", "test")},
	)
	require.NoError(t, err)

	client, err := createNATSClient(&config.Config{}, phase.Client, metrics)
	require.NoError(t, err)
	require.NotNil(t, client)

	var record map[string]any
	require.NoError(t, json.Unmarshal(bytes.TrimSpace(output.Bytes()), &record))
	assert.Equal(t, "Created NATS client", record["msg"])
	assert.Equal(t, "natsclient", record["component"])
	assert.Equal(t, "e2e-semstreams", record["service"])
}

func TestCreateNATSClientFailureUsesE2EPhaseALoggerExactlyOnce(t *testing.T) {
	t.Setenv("SEMSTREAMS_NATS_URLS", "")
	var output bytes.Buffer
	local, err := bootstrapobservability.NewLocalHandler(&output, "info", "json")
	require.NoError(t, err)
	logger := slog.New(local).With("service", "e2e-semstreams", "component", "natsclient")

	_, err = createNATSClient(&config.Config{}, logger, nil)
	require.ErrorContains(t, err, "client metrics registry cannot be nil")

	var record map[string]any
	require.NoError(t, json.Unmarshal(bytes.TrimSpace(output.Bytes()), &record))
	assert.Equal(t, "Boot phase failed", record["msg"])
	assert.Equal(t, "client-create", record["boot_stage"])
	assert.Equal(t, "natsclient", record["component"])
}

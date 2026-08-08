package service

import (
	"context"
	"encoding/json"
	"net"
	"testing"

	"github.com/c360studio/semstreams/metric"
	"github.com/stretchr/testify/require"
)

func TestMetricsStartReportsServerBindFailureBeforeReturning(t *testing.T) {
	occupied, err := net.Listen("tcp", ":0")
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, occupied.Close()) })
	port := occupied.Addr().(*net.TCPAddr).Port

	rawConfig, err := json.Marshal(MetricsConfig{Port: port, Path: "/metrics"})
	require.NoError(t, err)
	svc, err := NewMetrics(rawConfig, &Dependencies{MetricsRegistry: metric.NewMetricsRegistry()})
	require.NoError(t, err)

	err = svc.Start(context.Background())
	require.Error(t, err)
	require.ErrorContains(t, err, "start metrics server")
	require.ErrorContains(t, err, "address already in use")
	require.Equal(t, StatusStopped, svc.(*Metrics).Status())
}

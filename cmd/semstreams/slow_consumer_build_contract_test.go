package main

import (
	"os"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestSlowConsumerE2EBuildDoesNotReplaceProductionTarget(t *testing.T) {
	dockerfile, err := os.ReadFile("../../docker/Dockerfile")
	require.NoError(t, err)
	source := string(dockerfile)
	production := strings.Index(source, "FROM alpine:latest AS production")
	taggedBuilder := strings.Index(source, "FROM builder AS slow-consumer-builder")
	taggedTarget := strings.Index(source, "FROM production AS e2e-slow-consumer")
	require.GreaterOrEqual(t, production, 0)
	require.Greater(t, taggedBuilder, production,
		"ordinary production target must not depend on the later tagged builder")
	require.Greater(t, taggedTarget, taggedBuilder)
	assert.Contains(t, source[taggedBuilder:taggedTarget], "-tags=e2e_slow_consumer")
	assert.Contains(t, source[taggedBuilder:taggedTarget], "./cmd/semstreams")
	assert.NotContains(t, source[taggedBuilder:taggedTarget], "./cmd/e2e-semstreams")
}

func TestSlowConsumerHookRunsBetweenConnectionAndConfigArbitration(t *testing.T) {
	mainSource, err := os.ReadFile("main.go")
	require.NoError(t, err)
	source := string(mainSource)
	connect := strings.LastIndex(source, "bootstrapobservability.ConnectClient(")
	hook := strings.Index(source, "runSlowConsumerProbe(ctx, natsClient)")
	completion := strings.LastIndex(source, "spinner.Stop()")
	require.GreaterOrEqual(t, connect, 0)
	require.Greater(t, hook, connect)
	require.Greater(t, completion, hook)
}

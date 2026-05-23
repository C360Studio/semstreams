//go:build integration

package config

import (
	"bytes"
	"context"
	"log/slog"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/c360studio/semstreams/natsclient"
)

// TestVerifyJetStreamLimits_ZeroIsNoOp verifies that when neither
// MaxMemory nor MaxFileStore is set, the verification returns nil
// without making a server round-trip. Back-compat: operators who
// don't surface JetStream intent in the framework config see no
// behavior change.
func TestVerifyJetStreamLimits_ZeroIsNoOp(t *testing.T) {
	client := natsclient.NewTestClient(t, natsclient.WithJetStream())
	var buf bytes.Buffer
	logger := slog.New(slog.NewTextHandler(&buf, &slog.HandlerOptions{Level: slog.LevelDebug}))
	sm := NewStreamsManager(client.Client, logger)

	cfg := &Config{
		NATS: NATSConfig{
			JetStream: JetStreamConfig{
				Enabled: true,
				// MaxMemory and MaxFileStore both unset (0)
			},
		},
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	err := sm.VerifyJetStreamLimits(ctx, cfg)
	require.NoError(t, err)

	// No log line should mention "exceeds server limit" — nothing to
	// verify means nothing to warn about.
	assert.NotContains(t, buf.String(), "exceeds server limit")
}

// TestVerifyJetStreamLimits_UnlimitedServerSkipsWarn verifies the
// -1-is-unlimited contract: when the server reports MaxStore=-1
// (or MaxMemory=-1), even an absurd operator-configured limit produces
// no Warn. -1 is the JetStream AccountLimits sentinel for "no limit",
// so any positive configured value is trivially satisfied. The
// testcontainers nats-server reports -1 by default (no nats.conf
// constraints on the test container) — exercises this path.
func TestVerifyJetStreamLimits_UnlimitedServerSkipsWarn(t *testing.T) {
	client := natsclient.NewTestClient(t, natsclient.WithJetStream())
	var buf bytes.Buffer
	logger := slog.New(slog.NewTextHandler(&buf, &slog.HandlerOptions{Level: slog.LevelDebug}))
	sm := NewStreamsManager(client.Client, logger)

	const oneAbsurdPiB = int64(1024 * 1024 * 1024 * 1024 * 1024)
	cfg := &Config{
		NATS: NATSConfig{
			JetStream: JetStreamConfig{
				Enabled:      true,
				MaxMemory:    oneAbsurdPiB,
				MaxFileStore: oneAbsurdPiB,
			},
		},
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	err := sm.VerifyJetStreamLimits(ctx, cfg)
	require.NoError(t, err)

	logs := buf.String()
	// On an unlimited server, neither limit predicate fires. Both must
	// take the -1-skip branch (otherwise the absurd PiB values would
	// trigger Warns).
	assert.False(t, strings.Contains(logs, "exceeds server limit"),
		"expected NO warn against unlimited server; got logs: %s", logs)
}

// TestVerifyJetStreamLimits_NoWarnWhenWithinBudget verifies that a
// configured limit at or below the server's actual budget produces no
// Warn. Uses a deliberately tiny value (1 byte) to ensure the test
// container's defaults exceed it regardless of OS / docker setup.
func TestVerifyJetStreamLimits_NoWarnWhenWithinBudget(t *testing.T) {
	client := natsclient.NewTestClient(t, natsclient.WithJetStream())
	var buf bytes.Buffer
	logger := slog.New(slog.NewTextHandler(&buf, &slog.HandlerOptions{Level: slog.LevelDebug}))
	sm := NewStreamsManager(client.Client, logger)

	cfg := &Config{
		NATS: NATSConfig{
			JetStream: JetStreamConfig{
				Enabled:      true,
				MaxFileStore: 1, // 1 byte — trivially below any server limit
			},
		},
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	err := sm.VerifyJetStreamLimits(ctx, cfg)
	require.NoError(t, err)

	assert.NotContains(t, buf.String(), "exceeds server limit",
		"trivial 1-byte limit should never trip the gap warning")
}

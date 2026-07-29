//go:build integration

package config

import (
	"context"
	"io"
	"log/slog"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/c360studio/semstreams/natsclient"
)

// TestCreateStream_DuplicatesWindow verifies the duplicate-detection window
// plumbs from StreamConfig.Duplicates into the JetStream stream, that an
// explicit window change is applied to an existing stream, and that an unset
// window preserves the existing one rather than resetting it to the server
// default (ADR-055 §5 "T1"; the review's existing-stream-update concern).
func TestCreateStream_DuplicatesWindow(t *testing.T) {
	client := natsclient.NewTestClient(t, natsclient.WithJetStream())
	sm := NewStreamsManager(client.Client, slog.New(slog.NewTextHandler(io.Discard, nil)))

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	js, err := client.Client.JetStream()
	require.NoError(t, err)

	const name = "DUPWINDOW"
	windowOf := func() time.Duration {
		stream, serr := js.Stream(ctx, name)
		require.NoError(t, serr)
		info, ierr := stream.Info(ctx)
		require.NoError(t, ierr)
		return info.Config.Duplicates
	}

	// Bounds are complete throughout: an ordinary stream owes a finite max_age,
	// a finite max_bytes, and a discard policy, and this test is about the
	// duplicate-detection window rather than that contract.
	dupStream := func(window string) streamDeclaration {
		return streamDeclaration{
			name:   name,
			source: "test",
			cfg: StreamConfig{
				Subjects:   []string{"dupwindow.>"},
				Storage:    "memory",
				MaxAge:     "24h",
				MaxBytes:   1024 * 1024,
				Discard:    StreamDiscardOld,
				Duplicates: window,
			},
		}
	}

	// 1. Create with an explicit 30m window.
	require.NoError(t, sm.createStream(ctx, dupStream("30m")))
	assert.Equal(t, 30*time.Minute, windowOf(), "explicit window must plumb through to the stream")

	// 2. Re-create with a different explicit window → applied via UpdateStream.
	require.NoError(t, sm.createStream(ctx, dupStream("1h")))
	assert.Equal(t, time.Hour, windowOf(), "explicit window drift must be applied")

	// 3. Re-create with NO window set → existing window preserved, not reset.
	require.NoError(t, sm.createStream(ctx, dupStream("")))
	assert.Equal(t, time.Hour, windowOf(),
		"unset window must preserve the existing window, not reset to the server default")
}

// TestCreateStream_DuplicatesWindow_ClampedToMaxAge verifies that a window
// larger than MaxAge is clamped down rather than aborting boot — the NATS
// server rejects (does not clamp) an explicit window > MaxAge, so createStream
// must clamp client-side (go-reviewer S1).
func TestCreateStream_DuplicatesWindow_ClampedToMaxAge(t *testing.T) {
	client := natsclient.NewTestClient(t, natsclient.WithJetStream())
	sm := NewStreamsManager(client.Client, slog.New(slog.NewTextHandler(io.Discard, nil)))

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	require.NoError(t, sm.createStream(ctx, streamDeclaration{
		name:   "DUPCLAMP",
		source: "test",
		cfg: StreamConfig{
			Subjects:   []string{"dupclamp.>"},
			Storage:    "memory",
			MaxAge:     "5m",
			MaxBytes:   1024 * 1024,
			Discard:    StreamDiscardOld,
			Duplicates: "30m", // larger than MaxAge → must clamp, not reject
		},
	}), "a window larger than max_age must clamp, not fail stream creation")

	js, err := client.Client.JetStream()
	require.NoError(t, err)
	stream, err := js.Stream(ctx, "DUPCLAMP")
	require.NoError(t, err)
	info, err := stream.Info(ctx)
	require.NoError(t, err)
	assert.Equal(t, 5*time.Minute, info.Config.Duplicates, "window must be clamped to max_age")
	assert.Equal(t, 5*time.Minute, info.Config.MaxAge)
}

// TestCreateStream_DuplicatesWindow_InvalidFallsBackToDefault verifies a
// malformed duration does not fail stream creation — it falls back to the
// server default (zero window) with a warning.
func TestCreateStream_DuplicatesWindow_InvalidFallsBackToDefault(t *testing.T) {
	client := natsclient.NewTestClient(t, natsclient.WithJetStream())
	sm := NewStreamsManager(client.Client, slog.New(slog.NewTextHandler(io.Discard, nil)))

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	require.NoError(t, sm.createStream(ctx, streamDeclaration{
		name:   "DUPBAD",
		source: "test",
		cfg: StreamConfig{
			Subjects:   []string{"dupbad.>"},
			Storage:    "memory",
			MaxAge:     "24h",
			MaxBytes:   1024 * 1024,
			Discard:    StreamDiscardOld,
			Duplicates: "not-a-duration",
		},
	}), "an invalid duplicates window must not fail stream creation")
}

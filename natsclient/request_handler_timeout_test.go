package natsclient

import (
	"testing"
	"time"
)

// The per-message request-handler context timeout (applied inside
// SubscribeForRequests) was a hardcoded 30s. Slow LLM handlers on the
// globalSearch path (8B answer synthesis) exceed it and get cancelled
// mid-generation. These tests pin the configurable replacement: default
// stays 30s (unchanged for CI), an option and an env var can raise it.

func TestRequestHandlerTimeout_DefaultIs30s(t *testing.T) {
	c, err := NewClient("nats://localhost:4222")
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}
	if got := c.requestHandlerTimeout; got != DefaultRequestHandlerTimeout {
		t.Fatalf("default requestHandlerTimeout = %v, want %v", got, DefaultRequestHandlerTimeout)
	}
	if DefaultRequestHandlerTimeout != 30*time.Second {
		t.Fatalf("DefaultRequestHandlerTimeout = %v, want 30s (CI default must not change)", DefaultRequestHandlerTimeout)
	}
}

func TestRequestHandlerTimeout_Option(t *testing.T) {
	c, err := NewClient("nats://localhost:4222", WithRequestHandlerTimeout(90*time.Second))
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}
	if got := c.requestHandlerTimeout; got != 90*time.Second {
		t.Fatalf("WithRequestHandlerTimeout: requestHandlerTimeout = %v, want 90s", got)
	}
}

func TestRequestHandlerTimeout_EnvOverride(t *testing.T) {
	t.Setenv("SEMSTREAMS_NATS_REQUEST_HANDLER_TIMEOUT", "150s")
	c, err := NewClient("nats://localhost:4222")
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}
	if got := c.requestHandlerTimeout; got != 150*time.Second {
		t.Fatalf("env override: requestHandlerTimeout = %v, want 150s", got)
	}
}

func TestRequestHandlerTimeout_EnvInvalidFallsBackToDefault(t *testing.T) {
	t.Setenv("SEMSTREAMS_NATS_REQUEST_HANDLER_TIMEOUT", "not-a-duration")
	c, err := NewClient("nats://localhost:4222")
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}
	if got := c.requestHandlerTimeout; got != DefaultRequestHandlerTimeout {
		t.Fatalf("invalid env: requestHandlerTimeout = %v, want default %v", got, DefaultRequestHandlerTimeout)
	}
}

// An explicit option must win over the env var — option is the in-process
// authority; env is the deployment default.
func TestRequestHandlerTimeout_OptionBeatsEnv(t *testing.T) {
	t.Setenv("SEMSTREAMS_NATS_REQUEST_HANDLER_TIMEOUT", "150s")
	c, err := NewClient("nats://localhost:4222", WithRequestHandlerTimeout(42*time.Second))
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}
	if got := c.requestHandlerTimeout; got != 42*time.Second {
		t.Fatalf("option-over-env: requestHandlerTimeout = %v, want 42s", got)
	}
}

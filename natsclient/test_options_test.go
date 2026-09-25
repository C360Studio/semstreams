package natsclient

import (
	"testing"
	"time"
)

func TestWithFastStartup_ConfiguresConnectionBudget(t *testing.T) {
	t.Parallel()

	cfg := defaultTestConfig()
	WithFastStartup()(cfg)
	if cfg.timeout != 2*time.Second {
		t.Fatalf("connection timeout = %s, want 2s", cfg.timeout)
	}
	if cfg.startTimeout != defaultTestContainerStartTimeout {
		t.Fatalf("container start timeout = %s, want default %s", cfg.startTimeout, defaultTestContainerStartTimeout)
	}
}

func TestWithMinimalFeatures_PreservesExplicitContainerStartTimeout(t *testing.T) {
	t.Parallel()

	const explicitStartTimeout = 47 * time.Second
	cfg := defaultTestConfig()
	WithKV()(cfg)
	WithStartTimeout(explicitStartTimeout)(cfg)
	WithMinimalFeatures()(cfg)

	if cfg.startTimeout != explicitStartTimeout {
		t.Fatalf("container start timeout = %s, want explicit %s", cfg.startTimeout, explicitStartTimeout)
	}
	if cfg.timeout != time.Second {
		t.Fatalf("client timeout = %s, want 1s", cfg.timeout)
	}
	if cfg.jetstream {
		t.Fatal("JetStream enabled after applying WithMinimalFeatures")
	}
	if cfg.kv {
		t.Fatal("KV enabled after applying WithMinimalFeatures")
	}
}

func TestWithTestMaxPayload_NonPositivePreservesServerDefault(t *testing.T) {
	t.Parallel()

	for _, value := range []int64{0, -1} {
		cfg := defaultTestConfig()
		WithTestMaxPayload(value)(cfg)
		request := newTestContainerRequest(cfg)
		if len(request.ContainerRequest.Files) != 0 {
			t.Errorf("WithTestMaxPayload(%d) installed a broker config, want server default", value)
		}
	}
}

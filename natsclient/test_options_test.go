package natsclient

import (
	"testing"
	"time"
)

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

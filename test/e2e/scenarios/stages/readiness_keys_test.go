package stages

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

// TestReadinessKeys_EveryTierRunsEveryDeclaredProducer is a config-drift guard.
//
// The stage waits for every key in ReadinessKeys to report caught-up. If a tier stops
// running one of those producers, its key is never published, the watcher reports
// unknown, and the stage waits out its ENTIRE validation timeout before failing — a
// slow, confusing red that looks like an ingest problem and is actually a config
// change. This fails fast and points at the config instead.
//
// It maps GRAPH_STATUS keys to the component NAMES that publish them, which are
// different namespaces: the rule processor is configured as "rule-processor" and
// publishes under the key "rule".
func TestReadinessKeys_EveryTierRunsEveryDeclaredProducer(t *testing.T) {
	keyToComponent := map[string]string{
		"graph-ingest": "graph-ingest",
		"graph-index":  "graph-index",
		"rule":         "rule-processor",
	}

	// Every config this stage's tiers run on.
	tiers := []string{
		"e2e-structural.json",
		"structural.json",
		"statistical.json",
		"semantic.json",
		"semantic-8b.json",
		"semantic-frontier.json",
	}

	repoRoot := filepath.Join("..", "..", "..", "..")

	for _, tier := range tiers {
		t.Run(tier, func(t *testing.T) {
			path := filepath.Join(repoRoot, "configs", tier)
			raw, err := os.ReadFile(path) //nolint:gosec // fixed test-local config path
			if err != nil {
				t.Fatalf("read %s: %v", path, err)
			}

			var cfg struct {
				Components map[string]struct {
					Name    string `json:"name"`
					Enabled *bool  `json:"enabled"`
				} `json:"components"`
			}
			if err := json.Unmarshal(raw, &cfg); err != nil {
				t.Fatalf("parse %s: %v", tier, err)
			}

			enabled := make(map[string]bool)
			for _, c := range cfg.Components {
				// Absent "enabled" defaults to true, matching config loading.
				if c.Enabled == nil || *c.Enabled {
					enabled[c.Name] = true
				}
			}

			for _, key := range ReadinessKeys {
				component, known := keyToComponent[key]
				if !known {
					t.Fatalf("ReadinessKeys declares %q with no component mapping in this "+
						"test — add it, or the drift guard silently stops covering that key", key)
				}
				if !enabled[component] {
					t.Errorf("readiness key %q is declared but its producer %q is not enabled "+
						"in %s: the stage would wait out its full timeout on an absent key",
						key, component, tier)
				}
			}
		})
	}
}

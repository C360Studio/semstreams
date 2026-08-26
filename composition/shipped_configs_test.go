package composition_test

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sort"
	"testing"

	"github.com/c360studio/semstreams/cmd/e2e-semstreams/mission"
	"github.com/c360studio/semstreams/component"
	"github.com/c360studio/semstreams/componentregistry"
	"github.com/c360studio/semstreams/composition"
	"github.com/c360studio/semstreams/config"
	"github.com/c360studio/semstreams/examples/processors/document"
	"github.com/c360studio/semstreams/examples/processors/iot_sensor"
	optionalotel "github.com/c360studio/semstreams/frameworkadapters/otel"
	"github.com/c360studio/semstreams/frameworkcapabilities/graphresearch"
)

// shippedRegistry is the union of what the two shipped binaries compose:
// cmd/semstreams (core + graph-research + OTEL) and cmd/e2e-semstreams (the
// same plus the bundled example components), so every checked-in config
// validates against the catalog its binary would give it.
func shippedRegistry(t *testing.T) *component.Registry {
	t.Helper()
	registry := component.NewRegistry()
	for name, register := range map[string]func(*component.Registry) error{
		"core":           componentregistry.Register,
		"graph-research": graphresearch.RegisterComponents,
		"otel":           optionalotel.Register,
		"iot_sensor":     iotsensor.Register,
		"document":       document.Register,
		"mission":        mission.Register,
	} {
		if err := register(registry); err != nil {
			t.Fatalf("register %s: %v", name, err)
		}
	}
	return registry
}

// shippedConfigs walks every checked-in JSON document under configs/ and
// returns the ones that are config.Config documents (a top-level
// platform.org), keyed by repo-relative path.
func shippedConfigs(t *testing.T) map[string]*config.Config {
	t.Helper()
	root := filepath.Join("..", "configs")
	found := map[string]*config.Config{}
	err := filepath.WalkDir(root, func(path string, entry os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.IsDir() || filepath.Ext(path) != ".json" {
			return nil
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		var probe struct {
			Platform struct {
				Org string `json:"org"`
			} `json:"platform"`
		}
		if json.Unmarshal(data, &probe) != nil || probe.Platform.Org == "" {
			return nil
		}
		cfg, err := config.NewLoader().LoadFile(path)
		if err != nil {
			t.Fatalf("load %s: %v", path, err)
		}
		found[filepath.ToSlash(path)] = cfg
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(found) == 0 {
		t.Fatal("no shipped configuration documents found under configs/")
	}
	return found
}

// TestValidateShippedConfigsHaveNoErrorFindings is the unit-test form of the
// P5 precondition: every shipped composition validates with no error finding
// against the registry its binary composes.
func TestValidateShippedConfigsHaveNoErrorFindings(t *testing.T) {
	registry := shippedRegistry(t)
	configs := shippedConfigs(t)
	paths := make([]string, 0, len(configs))
	for path := range configs {
		paths = append(paths, path)
	}
	sort.Strings(paths)
	for _, path := range paths {
		t.Run(path, func(t *testing.T) {
			result, err := composition.Validate(registry, configs[path])
			if err != nil {
				t.Fatalf("Validate: %v", err)
			}
			t.Logf("%s: status=%s errors=%d warnings=%d nodes=%d edges=%d",
				path, result.Status, len(result.Errors), len(result.Warnings), len(result.Graph.Nodes), len(result.Graph.Edges))
			for _, finding := range result.Errors {
				t.Errorf("error finding %s on %s/%s: %s", finding.Type, finding.Component, finding.Port, finding.Message)
			}
		})
	}
}

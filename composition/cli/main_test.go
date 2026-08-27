package cli_test

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/c360studio/semstreams/component"
	"github.com/c360studio/semstreams/componentregistry"
	"github.com/c360studio/semstreams/composition"
	"github.com/c360studio/semstreams/composition/cli"
	"github.com/c360studio/semstreams/config"
	optionalotel "github.com/c360studio/semstreams/frameworkadapters/otel"
	"github.com/c360studio/semstreams/frameworkcapabilities/graphresearch"
)

func frameworkRegistry(t *testing.T) *component.Registry {
	t.Helper()
	registry := component.NewRegistry()
	if err := componentregistry.Register(registry); err != nil {
		t.Fatal(err)
	}
	if err := graphresearch.RegisterComponents(registry); err != nil {
		t.Fatal(err)
	}
	if err := optionalotel.Register(registry); err != nil {
		t.Fatal(err)
	}
	return registry
}

func writeConfig(t *testing.T, name, body string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), name)
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

const configWithError = `{
  "version": "1.0.0",
  "platform": {"org": "cli", "id": "test", "environment": "test"},
  "components": {
    "ghost": {"type": "processor", "name": "no-such-factory", "enabled": true, "config": {}}
  }
}`

// graph-index alone yields warnings only: its KV watch input and KV write
// outputs are optional observation points, so nothing is an error.
const configWithWarnings = `{
  "version": "1.0.0",
  "platform": {"org": "cli", "id": "test", "environment": "test"},
  "components": {
    "index": {"type": "processor", "name": "graph-index", "enabled": true, "config": {}}
  }
}`

const configWithEdges = `{
  "version": "1.0.0",
  "platform": {"org": "cli", "id": "test", "environment": "test"},
  "components": {
    "udp": {"type": "input", "name": "udp", "enabled": true, "config": {}},
    "wrap": {"type": "processor", "name": "json_generic", "enabled": true, "config": {
      "ports": {
        "inputs": [{"name": "in", "required": true, "config": {"kind": "jetstream", "stream_name": "UDP", "subjects": ["input.udp.mavlink"]}}],
        "outputs": [{"name": "out", "required": true, "config": {"kind": "nats", "subject": "generic.messages"}}]
      }
    }},
    "disk": {"type": "output", "name": "file", "enabled": true, "config": {
      "directory": "/tmp/cli-test", "file_prefix": "out", "format": "jsonl",
      "ports": {"inputs": [{"name": "in", "required": true, "config": {"kind": "nats", "subject": "generic.messages"}}]}
    }}
  }
}`

func TestCLIValidateExitsNonZeroOnErrorFindings(t *testing.T) {
	registry := frameworkRegistry(t)

	var stdout, stderr bytes.Buffer
	code := cli.Main([]string{"validate", writeConfig(t, "errors.json", configWithError)}, registry, &stdout, &stderr)
	if code == 0 {
		t.Fatalf("validate exited 0 for a configuration with an error finding; stdout=%s stderr=%s", stdout.String(), stderr.String())
	}
	var result composition.Result
	if err := json.Unmarshal(stdout.Bytes(), &result); err != nil {
		t.Fatalf("validate output is not a composition.Result: %v\n%s", err, stdout.String())
	}
	if result.Status != composition.StatusErrors || len(result.Errors) == 0 {
		t.Fatalf("validate printed status %q with %d errors", result.Status, len(result.Errors))
	}
	if result.Errors[0].Type != composition.TypeUnknownComponent || result.Errors[0].Component != "ghost" {
		t.Fatalf("printed finding %+v, want unknown_component on ghost", result.Errors[0])
	}

	stdout.Reset()
	stderr.Reset()
	code = cli.Main([]string{"validate", writeConfig(t, "warnings.json", configWithWarnings)}, registry, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("validate exited %d for a warnings-only configuration; stdout=%s stderr=%s", code, stdout.String(), stderr.String())
	}
	if err := json.Unmarshal(stdout.Bytes(), &result); err != nil {
		t.Fatalf("validate output is not a composition.Result: %v\n%s", err, stdout.String())
	}
	if result.Status != composition.StatusWarnings || len(result.Warnings) == 0 || len(result.Errors) != 0 {
		t.Fatalf("validate printed status %q errors=%d warnings=%d, want warnings only", result.Status, len(result.Errors), len(result.Warnings))
	}
}

func TestCLICatalogPrintsEveryRegisteredFactory(t *testing.T) {
	registry := frameworkRegistry(t)
	var stdout, stderr bytes.Buffer
	if code := cli.Main([]string{"catalog"}, registry, &stdout, &stderr); code != 0 {
		t.Fatalf("catalog exited %d: %s", code, stderr.String())
	}
	var entries []map[string]any
	if err := json.Unmarshal(stdout.Bytes(), &entries); err != nil {
		t.Fatalf("catalog output is not a JSON array: %v\n%s", err, stdout.String())
	}
	if got, want := len(entries), len(registry.ListFactories()); got != want || want != 33 {
		t.Fatalf("catalog printed %d entries, registry has %d factories (want 33)", got, want)
	}
	for _, entry := range entries {
		id, _ := entry["id"].(string)
		if _, ok := entry["schema"]; !ok {
			t.Errorf("entry %s lacks schema", id)
		}
		_, hasDefaults := entry["default_ports"]
		requires, _ := entry["ports_require_config"].(bool)
		if hasDefaults == requires {
			t.Errorf("entry %s: default_ports=%v ports_require_config=%v, want exactly one", id, hasDefaults, requires)
		}
	}
}

func TestCLIGraphMermaidRendersEveryEdge(t *testing.T) {
	registry := frameworkRegistry(t)
	path := writeConfig(t, "edges.json", configWithEdges)
	cfg, err := config.NewLoader().LoadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	expected, err := composition.Validate(registry, cfg)
	if err != nil {
		t.Fatal(err)
	}
	if len(expected.Graph.Edges) < 2 {
		t.Fatalf("fixture derives %d edges, want at least 2: %+v", len(expected.Graph.Edges), expected)
	}

	var stdout, stderr bytes.Buffer
	if code := cli.Main([]string{"graph", path, "--mermaid"}, registry, &stdout, &stderr); code != 0 {
		t.Fatalf("graph --mermaid exited %d: %s", code, stderr.String())
	}
	rendered := stdout.String()
	if got := strings.Count(rendered, "-->"); got != len(expected.Graph.Edges) {
		t.Fatalf("Mermaid renders %d edges, validation derived %d:\n%s", got, len(expected.Graph.Edges), rendered)
	}
	for _, node := range expected.Graph.Nodes {
		if !strings.Contains(rendered, node.Instance) {
			t.Errorf("Mermaid output lacks node %s:\n%s", node.Instance, rendered)
		}
	}

	stdout.Reset()
	if code := cli.Main([]string{"graph", path}, registry, &stdout, &stderr); code != 0 {
		t.Fatalf("graph exited %d: %s", code, stderr.String())
	}
	var graph composition.Graph
	if err := json.Unmarshal(stdout.Bytes(), &graph); err != nil {
		t.Fatalf("graph output is not a composition.Graph: %v\n%s", err, stdout.String())
	}
	if len(graph.Edges) != len(expected.Graph.Edges) || len(graph.Nodes) != len(expected.Graph.Nodes) {
		t.Fatalf("graph JSON has %d nodes/%d edges, validation derived %d/%d", len(graph.Nodes), len(graph.Edges), len(expected.Graph.Nodes), len(expected.Graph.Edges))
	}
}

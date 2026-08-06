package referenceconfigs_test

import (
	"encoding/json"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/c360studio/semstreams/component"
	"github.com/c360studio/semstreams/internal/graphmutation"
)

var graphMutationRequesters = map[string]struct{}{
	"agentic-loop":              {},
	"agentic-tools":             {},
	"gated-dag":                 {},
	"graph-clustering":          {},
	"lifecycle-gateway":         {},
	"research-graph-assess":     {},
	"research-graph-classify":   {},
	"research-graph-execute":    {},
	"research-graph-route":      {},
	"research-graph-synthesize": {},
	"rule-processor":            {},
}

type shippedComponent struct {
	Name   string `json:"name"`
	Config struct {
		Ports component.PortConfig `json:"ports"`
	} `json:"config"`
}

type shippedComposition struct {
	Components map[string]shippedComponent `json:"components"`
	Ports      component.PortConfig        `json:"ports"`
}

func TestShippedConfigs_UseCanonicalGraphMutationPorts(t *testing.T) {
	paths := allShippedJSONPaths(t)
	require.NotEmpty(t, paths)

	for _, path := range paths {
		t.Run(mustRel(t, path), func(t *testing.T) {
			data, err := os.ReadFile(path)
			require.NoError(t, err)

			var cfg shippedComposition
			require.NoError(t, json.Unmarshal(data, &cfg), "%s does not decode", path)

			for instance, declared := range cfg.Components {
				assertMutationPortContract(t, path, instance, declared.Name, declared.Config.Ports)
			}

			// Rule-pack fragments under configs/rules are merged into a rule
			// component rather than carrying a top-level components map.
			if len(cfg.Components) == 0 && hasMutationPort(cfg.Ports.Outputs) {
				assertCanonicalMutationPort(t, path, "rule-pack", cfg.Ports.Outputs)
			}
		})
	}
}

func assertMutationPortContract(
	t *testing.T,
	path, instance, componentName string,
	ports component.PortConfig,
) {
	t.Helper()

	switch componentName {
	case "graph-ingest":
		assertCanonicalMutationPort(t, path, instance, ports.Inputs)
	case "graph-gateway":
		for _, port := range ports.Outputs {
			require.False(t, strings.HasPrefix(port.Subject, "graph.mutation."),
				"%s component %q exposes a mutation output but graph-gateway is read-only", path, instance)
		}
	default:
		if _, requester := graphMutationRequesters[componentName]; requester {
			assertCanonicalMutationPort(t, path, instance, ports.Outputs)
		}
	}
}

func assertCanonicalMutationPort(t *testing.T, path, instance string, ports []component.PortDefinition) {
	t.Helper()

	var matches []component.PortDefinition
	for _, port := range ports {
		if strings.HasPrefix(port.Subject, "graph.mutation.") || port.Interface == graphmutation.InterfaceType {
			matches = append(matches, port)
		}
	}
	require.Len(t, matches, 1, "%s component %q must declare exactly one graph mutation port", path, instance)

	port := matches[0]
	require.Equal(t, "nats-request", port.Type, "%s component %q mutation port type", path, instance)
	require.Equal(t, graphmutation.SubjectFamily, port.Subject, "%s component %q mutation family", path, instance)
	require.Equal(t, graphmutation.InterfaceType, port.Interface, "%s component %q mutation interface", path, instance)
	require.True(t, port.Required, "%s component %q mutation port must be required", path, instance)

	effective := component.BuildPortFromDefinition(port, component.DirectionOutput)
	requestPort, ok := effective.Config.(component.NATSRequestPort)
	require.True(t, ok, "%s component %q mutation port must build as NATSRequestPort", path, instance)
	require.NotNil(t, requestPort.Interface, "%s component %q mutation interface was discarded", path, instance)
	require.Equal(t, graphmutation.InterfaceType, requestPort.Interface.Type)
	require.Equal(t, graphmutation.InterfaceVersion, requestPort.Interface.Version)
}

func hasMutationPort(ports []component.PortDefinition) bool {
	for _, port := range ports {
		if strings.HasPrefix(port.Subject, "graph.mutation.") || port.Interface == graphmutation.InterfaceType {
			return true
		}
	}
	return false
}

func allShippedJSONPaths(t *testing.T) []string {
	t.Helper()

	var paths []string
	err := filepath.WalkDir(configsDir, func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".json") {
			return nil
		}
		paths = append(paths, path)
		return nil
	})
	require.NoError(t, err)
	return paths
}

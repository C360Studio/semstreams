//go:build integration

package composition_test

import (
	"fmt"
	"io"
	"log/slog"
	"sort"
	"strings"
	"testing"

	"github.com/c360studio/semstreams/composition"
	flowengine "github.com/c360studio/semstreams/engine"
	"github.com/c360studio/semstreams/flowstore"
	"github.com/c360studio/semstreams/natsclient"
)

// finding identity for the parity comparison: (type, component, port).
type findingKey struct{ typ, component, port string }

func (k findingKey) String() string { return fmt.Sprintf("%s %s/%s", k.typ, k.component, k.port) }

// engineDependencyGuards are the construction refusals the retiring engine
// hits because it constructs every node with only a NATS client. The offline
// validator declares ports without constructing, so it never sees them; the
// boot parity check (P1) is what covers that seam. Recorded in tasks 3.2.
var engineDependencyGuards = []string{
	"ModelRegistry is required",
	"model registry is required",
	"LifecycleManager is nil",
}

func isDependencyGuard(message string) bool {
	for _, guard := range engineDependencyGuards {
		if strings.Contains(message, guard) {
			return true
		}
	}
	return false
}

// TestValidateMatchesEngineFindingsForShippedConfigs is the dropped-step
// detector for the move of engine/validator.go:300-623 into composition:
// both validators run over every shipped config and must agree on the
// (type, component, port) set of the findings the engine emits, after the two
// renames (empty_flow→empty_composition, graph_build_error→
// port_declaration_error). The engine is the oracle; nothing is mapped away
// except the dependency-guard class named above. Deleted with the engine
// (#1093).
func TestValidateMatchesEngineFindingsForShippedConfigs(t *testing.T) {
	testClient := natsclient.NewTestClient(t, natsclient.WithJetStream(), natsclient.WithKV())
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	registry := shippedRegistry(t)
	engine := flowengine.NewEngine(registry, testClient.Client, logger, nil)
	rename := map[string]string{
		"empty_flow":        composition.TypeEmptyComposition,
		"graph_build_error": composition.TypePortDeclarationError,
	}
	engineTypes := map[string]bool{
		"disconnected_node": true, "orphaned_port": true, "interface_mismatch": true, "missing_interface": true,
		"unknown_component": true, "empty_flow": true, "graph_build_error": true,
	}

	configs := shippedConfigs(t)
	paths := make([]string, 0, len(configs))
	for path := range configs {
		paths = append(paths, path)
	}
	sort.Strings(paths)
	for _, path := range paths {
		t.Run(path, func(t *testing.T) {
			cfg := configs[path]
			flow, err := flowstore.FromComponentConfigs("parity", cfg.Components)
			if err != nil {
				t.Fatalf("FromComponentConfigs: %v", err)
			}
			engineResult, err := engine.ValidateFlowDefinition(flow)
			if err != nil {
				t.Fatalf("engine.ValidateFlowDefinition: %v", err)
			}
			result, err := composition.Validate(registry, cfg)
			if err != nil {
				t.Fatalf("composition.Validate: %v", err)
			}

			engineSet := map[findingKey]string{}
			for _, issue := range append(append([]flowengine.ValidationIssue(nil), engineResult.Errors...), engineResult.Warnings...) {
				typ := issue.Type
				if renamed, ok := rename[typ]; ok {
					typ = renamed
				}
				engineSet[findingKey{typ, issue.ComponentName, issue.PortName}] = issue.Message
			}
			compositionSet := map[findingKey]string{}
			for _, finding := range append(append([]composition.Finding(nil), result.Errors...), result.Warnings...) {
				compositionSet[findingKey{finding.Type, finding.Component, finding.Port}] = finding.Message
			}

			for key, message := range engineSet {
				if _, ok := compositionSet[key]; ok {
					continue
				}
				if key.typ == composition.TypePortDeclarationError && isDependencyGuard(message) {
					t.Logf("disposition dependency-guard: engine %s (%s) has no composition counterpart by design", key, message)
					continue
				}
				t.Errorf("engine emitted %s (%s); composition did not", key, message)
			}
			for key, message := range compositionSet {
				original := key.typ
				for from, to := range rename {
					if to == key.typ {
						original = from
					}
				}
				if !engineTypes[original] {
					continue // composition-only vocabulary; the engine never emitted it
				}
				if _, ok := engineSet[key]; !ok {
					t.Errorf("composition emitted %s (%s); engine did not", key, message)
				}
			}
		})
	}
}

//go:build integration

package composition_test

import (
	"fmt"
	"io"
	"log/slog"
	"sort"
	"strings"
	"testing"

	"github.com/c360studio/semstreams/component"
	"github.com/c360studio/semstreams/composition"
	flowengine "github.com/c360studio/semstreams/engine"
	"github.com/c360studio/semstreams/flowstore"
	"github.com/c360studio/semstreams/model"
	"github.com/c360studio/semstreams/natsclient"
	"github.com/c360studio/semstreams/payloadbuiltins"
	"github.com/c360studio/semstreams/payloadregistry"
	"github.com/c360studio/semstreams/pkg/lifecycle"
	agentictools "github.com/c360studio/semstreams/processor/agentic-tools"
	"github.com/c360studio/semstreams/types"
)

// finding identity for the parity comparison: (type, component, port).
type findingKey struct{ typ, component, port string }

func (k findingKey) String() string { return fmt.Sprintf("%s %s/%s", k.typ, k.component, k.port) }

// engineDependencies gives the retiring engine every dependency the shipped
// factories guard on, so it constructs every node and runs its connectivity
// pass (engine/validator.go:120-133 returns build errors only and skips
// connectivity when any node fails to construct — an oracle that bails is no
// oracle). Recorded in tasks 3.2.
func engineDependencies(t *testing.T, testClient *natsclient.TestClient, logger *slog.Logger) component.Dependencies {
	t.Helper()
	payloads := payloadregistry.New()
	if err := payloadbuiltins.Register(payloads); err != nil {
		t.Fatal(err)
	}
	return component.Dependencies{
		NATSClient:       testClient.Client,
		Logger:           logger,
		Platform:         types.PlatformMeta{Org: "parity", Platform: "engine"},
		ModelRegistry:    &model.Registry{Endpoints: map[string]*model.EndpointConfig{"parity": {URL: "http://parity.invalid", Model: "parity"}}},
		ToolRegistry:     agentictools.NewExecutorRegistry(),
		PayloadRegistry:  payloads,
		LifecycleManager: lifecycle.NewManager(testClient.Client, logger),
	}
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
	validator := flowengine.NewValidatorWithDependencies(registry, engineDependencies(t, testClient, logger), logger)
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
			engineResult, err := validator.ValidateFlow(flow)
			if err != nil {
				t.Fatalf("engine ValidateFlow: %v", err)
			}
			// A build error makes the engine skip connectivity entirely
			// (validator.go:120-133); parity is then unverifiable, which is a
			// failure to surface, never a pass.
			for _, issue := range engineResult.Errors {
				if issue.Type == "graph_build_error" {
					t.Errorf("engine could not build %s (%s): connectivity parity unverifiable", issue.ComponentName, issue.Message)
				}
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
			// Inputs declared fed from outside the composition (the external-
			// boundary marker, owner ruling 2026-08-26) raise no no-publisher
			// orphan in the new validator; the engine predates the marker and
			// still reports one. That is the one ruled departure from the
			// oracle, recorded in tasks 3.2, and it is scoped to exactly that
			// finding on exactly those ports.
			external := map[findingKey]bool{}
			for _, node := range result.Graph.Nodes {
				for _, input := range node.Inputs {
					if input.External {
						external[findingKey{composition.TypeOrphanedPort, node.Instance, input.Name}] = true
					}
				}
			}

			for key, message := range engineSet {
				if _, ok := compositionSet[key]; ok {
					continue
				}
				if external[key] && strings.Contains(message, "no_publishers") {
					t.Logf("disposition external-boundary marker: engine %s (%s) is suppressed by ruling", key, message)
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

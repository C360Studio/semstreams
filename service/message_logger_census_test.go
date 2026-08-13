package service_test

import (
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/c360studio/semstreams/cmd/e2e-semstreams/mission"
	"github.com/c360studio/semstreams/component"
	"github.com/c360studio/semstreams/componentregistry"
	"github.com/c360studio/semstreams/config"
	document "github.com/c360studio/semstreams/examples/processors/document"
	iotsensor "github.com/c360studio/semstreams/examples/processors/iot_sensor"
	"github.com/c360studio/semstreams/frameworkcapabilities/graphresearch"
	"github.com/c360studio/semstreams/internal/componentadmission"
	"github.com/c360studio/semstreams/metric"
	"github.com/c360studio/semstreams/model"
	"github.com/c360studio/semstreams/natsclient"
	"github.com/c360studio/semstreams/payloadregistry"
	"github.com/c360studio/semstreams/pkg/lifecycle"
	"github.com/stretchr/testify/require"
)

type subjectCensusCounts struct {
	Rows               int `json:"rows"`
	PerConfigExactKeys int `json:"per_config_exact_keys"`
	GlobalStrings      int `json:"global_strings"`
	Removals           int `json:"removals"`
}

type exactCollapseCounts struct {
	LoopDispatch int `json:"loop_dispatch"`
	Governance   int `json:"governance"`
	Total        int `json:"total"`
}

type subjectCensusArtifact struct {
	Version     int    `json:"version"`
	BaselineSHA string `json:"baseline_sha"`
	Ruling      struct {
		Authority                     string   `json:"authority"`
		Date                          string   `json:"date"`
		Disposition                   string   `json:"disposition"`
		RetiredConfigs                []string `json:"retired_configs"`
		ProhibitedSubstitutes         []string `json:"prohibited_substitutes"`
		ProductionPrerequisiteRepairs []string `json:"production_prerequisite_repairs"`
	} `json:"ruling"`
	Scope               []string            `json:"scope"`
	Raw                 subjectCensusCounts `json:"raw"`
	Effective           subjectCensusCounts `json:"effective"`
	Delta               subjectCensusCounts `json:"delta"`
	ExactCollapses      exactCollapseCounts `json:"exact_collapses"`
	AddedKinds          map[string]int      `json:"added_kinds"`
	AffectedConfigs     []string            `json:"affected_configs"`
	ContainmentOverlaps []struct {
		Config  string `json:"config"`
		Broader string `json:"broader"`
		Covered string `json:"covered"`
	} `json:"containment_overlaps"`
}

func TestMessageLoggerShippedSubjectCensusArtifactIsCompleteAndExact(t *testing.T) {
	data, err := os.ReadFile("testdata/message_logger_subject_census.json")
	require.NoError(t, err)
	var census subjectCensusArtifact
	require.NoError(t, json.Unmarshal(data, &census))

	require.Equal(t, 2, census.Version)
	require.Equal(t, "f2b7c4506ae78b1b8ace9fbc581994a2d14f1d55", census.BaselineSHA)
	require.Equal(t, "owner-approved Slice C ruling", census.Ruling.Authority)
	require.Equal(t, "2026-08-09", census.Ruling.Date)
	require.Equal(t, "retire enabled configs whose factories have no production registration", census.Ruling.Disposition)
	require.Equal(t, []string{
		"configs/examples/bm25-semantic-search.json",
		"configs/examples/pathrag-graph-traversal.json",
		"configs/http-gateway-semantic-search.json",
		"configs/semantic-basic.json",
	}, census.Ruling.RetiredConfigs)
	require.Equal(t, []string{"aliases", "synthetic factories", "substitute configurations"},
		census.Ruling.ProhibitedSubstitutes)
	require.Equal(t, []string{
		"configs/cloud-federation.json@1.0.2: explicit ws_control plus documented WebSocket duration decoding",
		"configs/hello-world.json@1.1.1: explicit ALIAS_INDEX graph-index output",
		"configs/lifecycle-flow.json@1.1.1: mission-command ports declare actual core NATS behavior",
	}, census.Ruling.ProductionPrerequisiteRepairs)
	require.Len(t, census.Scope, 21)
	require.True(t, slices.IsSorted(census.Scope), "frozen config scope must be deterministic")
	var discovered []string
	for _, pattern := range []string{"../configs/*.json", "../configs/examples/*.json", "../configs/flows/*.json"} {
		paths, globErr := filepath.Glob(pattern)
		require.NoError(t, globErr)
		for _, path := range paths {
			configData, readErr := os.ReadFile(path)
			require.NoError(t, readErr, path)
			var shape map[string]json.RawMessage
			require.NoError(t, json.Unmarshal(configData, &shape), path)
			var components map[string]json.RawMessage
			if raw, ok := shape["components"]; ok && json.Unmarshal(raw, &components) == nil && components != nil {
				discovered = append(discovered, filepath.ToSlash(path[3:]))
			}
		}
	}
	slices.Sort(discovered)
	require.Equal(t, census.Scope, discovered, "frozen census scope must name every shipped component config exactly")
	for _, path := range census.Scope {
		configData, readErr := os.ReadFile(filepath.Join("..", path))
		require.NoError(t, readErr, path)
		var configShape struct {
			Components map[string]json.RawMessage `json:"components"`
		}
		require.NoError(t, json.Unmarshal(configData, &configShape), path)
		require.NotNil(t, configShape.Components, "%s must remain a shipped component configuration", path)
	}
	for _, path := range census.Ruling.RetiredConfigs {
		_, statErr := os.Stat(filepath.Join("..", path))
		require.ErrorIs(t, statErr, os.ErrNotExist, "%s must remain retired", path)
	}

	computed := computeMessageLoggerSubjectCensus(t, census.Scope)
	require.Empty(t, computed.ConstructionFailures,
		"every enabled component in the shipped census must construct through a production-registered factory")
	t.Logf("computed shipped subject census: raw=%+v effective=%+v delta=%+v exact_collapses=%+v added_kinds=%v overlaps=%v",
		computed.Raw, computed.Effective, computed.Delta, computed.ExactCollapses,
		computed.AddedKinds, computed.ContainmentOverlaps)

	require.Equal(t, subjectCensusCounts{Rows: 395, PerConfigExactKeys: 243, GlobalStrings: 54}, census.Raw)
	require.Equal(t, subjectCensusCounts{Rows: 580, PerConfigExactKeys: 380, GlobalStrings: 70}, census.Effective)
	require.Equal(t, subjectCensusCounts{Rows: 185, PerConfigExactKeys: 137, GlobalStrings: 16}, census.Delta)
	require.Equal(t, census.Raw.Rows+census.Delta.Rows, census.Effective.Rows)
	require.Equal(t, census.Raw.PerConfigExactKeys+census.Delta.PerConfigExactKeys, census.Effective.PerConfigExactKeys)
	require.Equal(t, census.Raw.GlobalStrings+census.Delta.GlobalStrings, census.Effective.GlobalStrings)
	require.Zero(t, census.Delta.Removals)
	require.Equal(t, 48, census.ExactCollapses.Total)
	require.Equal(t, census.ExactCollapses.LoopDispatch+census.ExactCollapses.Governance, census.ExactCollapses.Total)
	require.Equal(t, census.Raw.PerConfigExactKeys+census.Delta.Rows-census.ExactCollapses.Total,
		census.Effective.PerConfigExactKeys)
	require.Equal(t, 185, census.AddedKinds["jetstream_inputs"]+census.AddedKinds["jetstream_outputs"]+
		census.AddedKinds["nats_inputs"]+census.AddedKinds["nats_outputs"]+census.AddedKinds["nats_request_inputs"])
	require.Len(t, census.AffectedConfigs, 9)
	for _, path := range census.AffectedConfigs {
		require.True(t, slices.Contains(census.Scope, path), "affected config %s is outside frozen scope", path)
	}
	require.Len(t, census.ContainmentOverlaps, 3)
	for _, overlap := range census.ContainmentOverlaps {
		require.Equal(t, "configs/agentic.json", overlap.Config)
	}
	require.Equal(t, []string{
		"agent.toolcall.proposed.>", "agent.toolcall.approved.>", "agent.toolcall.rejected.>",
	}, []string{
		census.ContainmentOverlaps[0].Broader,
		census.ContainmentOverlaps[1].Broader,
		census.ContainmentOverlaps[2].Broader,
	})

	require.Equal(t, census.Raw, computed.Raw)
	require.Equal(t, census.Effective, computed.Effective)
	require.Equal(t, census.Delta, computed.Delta)
	require.Equal(t, census.ExactCollapses, computed.ExactCollapses)
	require.Equal(t, 47, computed.ExactCollapses.LoopDispatch)
	require.Equal(t, 1, computed.ExactCollapses.Governance)
	require.Equal(t, census.AddedKinds, computed.AddedKinds)
	require.Equal(t, census.ContainmentOverlaps, computed.ContainmentOverlaps)
	require.Equal(t, map[string]int{
		"agentic-tools": 9, "graph-gateway": 8, "graph-query": 11,
		"research-graph-classify": 2, "research-graph-execute": 2,
	}, map[string]int{
		"agentic-tools":           computed.FactoryInstances["agentic-tools"],
		"graph-gateway":           computed.FactoryInstances["graph-gateway"],
		"graph-query":             computed.FactoryInstances["graph-query"],
		"research-graph-classify": computed.FactoryInstances["research-graph-classify"],
		"research-graph-execute":  computed.FactoryInstances["research-graph-execute"],
	})
	require.Equal(t, 11, computed.GraphQueryProviderRows)
	require.Equal(t, 8, computed.GatewayGraphQueryRows)
	require.Equal(t, 2, computed.ResearchClassifyQueryRows)
	require.Equal(t, 8, computed.ResearchExecuteQueryRows)
	require.Zero(t, computed.AgenticQueryRows)
}

func TestMessageLoggerCensusRejectsUnknownEnabledFactory(t *testing.T) {
	cfg, err := config.NewLoader().LoadFromBytes([]byte(`{
		"version":"1.0.0",
		"platform":{
			"org":"test","id":"census","type":"test","region":"local",
			"instance_id":"census-001","environment":"test"
		},
		"components":{
			"future-unknown":{
				"type":"processor","name":"future-unknown","enabled":true,"config":{}
			}
		}
	}`))
	require.NoError(t, err)

	registry := newMessageLoggerCensusRegistry(t)
	_, err = registry.CreateComponent(
		"future-unknown", cfg.Components["future-unknown"], messageLoggerCensusDependencies())
	require.Error(t, err)
	require.ErrorContains(t, err, "future-unknown")
}

func TestExactCollapseAttributionComesFromAddedProductionRows(t *testing.T) {
	loop := censusRow{Factory: "agentic-loop", Component: "loop", Subject: "agent.loop.start"}
	governance := censusRow{
		Factory: "agentic-governance", Component: "governance", Subject: "agent.toolcall.proposed.*",
	}
	rawRows := map[censusRow]int{loop: 1, governance: 1}
	effectiveRows := map[censusRow]int{loop: 3, governance: 2}
	rawKeys := map[string]struct{}{loop.Subject: {}, governance.Subject: {}}
	require.Equal(t, exactCollapseCounts{LoopDispatch: 2, Governance: 1, Total: 3},
		deriveExactCollapses(t, rawRows, effectiveRows, rawKeys))
}

type computedSubjectCensus struct {
	Raw                       subjectCensusCounts
	Effective                 subjectCensusCounts
	Delta                     subjectCensusCounts
	ExactCollapses            exactCollapseCounts
	AddedKinds                map[string]int
	FactoryInstances          map[string]int
	GraphQueryProviderRows    int
	GatewayGraphQueryRows     int
	ResearchClassifyQueryRows int
	ResearchExecuteQueryRows  int
	AgenticQueryRows          int
	ConstructionFailures      []string
	ContainmentOverlaps       []struct {
		Config  string `json:"config"`
		Broader string `json:"broader"`
		Covered string `json:"covered"`
	}
}

type censusRow struct {
	Component string
	Factory   string
	Direction component.Direction
	Port      string
	Kind      component.PortKind
	Subject   string
}

func computeMessageLoggerSubjectCensus(t *testing.T, scope []string) computedSubjectCensus {
	t.Helper()
	computed := computedSubjectCensus{AddedKinds: map[string]int{
		"jetstream_inputs": 0, "jetstream_outputs": 0,
		"nats_inputs": 0, "nats_outputs": 0, "nats_request_inputs": 0,
	}, FactoryInstances: make(map[string]int)}
	rawGlobal := make(map[string]struct{})
	effectiveGlobal := make(map[string]struct{})
	deps := messageLoggerCensusDependencies()

	for _, path := range scope {
		data, err := os.ReadFile(filepath.Join("..", path))
		require.NoError(t, err, path)
		cfg, err := config.NewLoader().LoadFromBytes(data)
		require.NoError(t, err, path)

		registry := newMessageLoggerCensusRegistry(t)

		rawRows := make(map[censusRow]int)
		effectiveRows := make(map[censusRow]int)
		rawKeys := make(map[string]struct{})
		effectiveKeys := make(map[string]struct{})
		names := make([]string, 0, len(cfg.Components))
		for name := range cfg.Components {
			names = append(names, name)
		}
		slices.Sort(names)
		for _, instanceName := range names {
			componentConfig := cfg.Components[instanceName]
			if !componentConfig.Enabled {
				continue
			}
			collectRawCensusRows(t, instanceName, componentConfig.Name,
				componentConfig.Config, rawRows, rawKeys, rawGlobal)
			if _, err := registry.CreateComponent(instanceName, componentConfig, deps); err != nil {
				// Every enabled factory failure, including an unknown future factory, is census-fatal.
				// Accumulate all of them so one stale config cannot mask its siblings.
				computed.ConstructionFailures = append(computed.ConstructionFailures,
					fmt.Sprintf("%s component %s factory %s: %v", path, instanceName, componentConfig.Name, err))
			}
		}
		for _, snapshot := range registry.Snapshots(componentadmission.Access{}) {
			computed.FactoryInstances[snapshot.Factory()]++
			collectEffectiveCensusRows(snapshot.Name(), snapshot.Factory(), component.DirectionInput,
				snapshot.Inputs(), snapshot.InputDeclarationFacts(), effectiveRows, effectiveKeys, effectiveGlobal)
			collectEffectiveCensusRows(snapshot.Name(), snapshot.Factory(), component.DirectionOutput,
				snapshot.Outputs(), snapshot.OutputDeclarationFacts(), effectiveRows, effectiveKeys, effectiveGlobal)
		}
		for row, count := range effectiveRows {
			switch {
			case row.Factory == "graph-query" && row.Direction == component.DirectionInput && row.Subject == "graph.query.*":
				computed.GraphQueryProviderRows += count
			case row.Factory == "graph-gateway" && row.Direction == component.DirectionOutput && row.Subject == "graph.query.*":
				computed.GatewayGraphQueryRows += count
			case row.Factory == "research-graph-classify" && row.Direction == component.DirectionOutput && row.Subject == "graph.query.searchGraph":
				computed.ResearchClassifyQueryRows += count
			case row.Factory == "research-graph-execute" && row.Direction == component.DirectionOutput && strings.HasPrefix(row.Subject, "graph.query."):
				computed.ResearchExecuteQueryRows += count
			case row.Factory == "agentic-tools" && row.Direction == component.DirectionOutput &&
				(row.Subject == "graph.query.searchGraph" || row.Subject == "graph.query.summary"):
				computed.AgenticQueryRows += count
			}
		}

		computed.Raw.Rows += censusRowCount(rawRows)
		computed.Raw.PerConfigExactKeys += len(rawKeys)
		computed.Effective.Rows += censusRowCount(effectiveRows)
		computed.Effective.PerConfigExactKeys += len(effectiveKeys)
		for row, count := range rawRows {
			if effectiveRows[row] < count {
				computed.Delta.Removals += count - effectiveRows[row]
			}
		}
		for row, count := range effectiveRows {
			added := count - rawRows[row]
			if added <= 0 {
				continue
			}
			computed.Delta.Rows += added
			computed.AddedKinds[censusKindKey(t, row)] += added
		}
		collapses := deriveExactCollapses(t, rawRows, effectiveRows, rawKeys)
		computed.ExactCollapses.LoopDispatch += collapses.LoopDispatch
		computed.ExactCollapses.Governance += collapses.Governance
		computed.ExactCollapses.Total += collapses.Total
		for _, pair := range [][2]string{
			{"agent.toolcall.proposed.>", "agent.toolcall.proposed.*"},
			{"agent.toolcall.approved.>", "agent.toolcall.approved.*"},
			{"agent.toolcall.rejected.>", "agent.toolcall.rejected.*"},
		} {
			_, broad := effectiveKeys[pair[0]]
			_, covered := effectiveKeys[pair[1]]
			if broad && covered {
				computed.ContainmentOverlaps = append(computed.ContainmentOverlaps, struct {
					Config  string `json:"config"`
					Broader string `json:"broader"`
					Covered string `json:"covered"`
				}{Config: path, Broader: pair[0], Covered: pair[1]})
			}
		}
	}
	computed.Raw.GlobalStrings = len(rawGlobal)
	computed.Effective.GlobalStrings = len(effectiveGlobal)
	computed.Delta.PerConfigExactKeys = computed.Effective.PerConfigExactKeys - computed.Raw.PerConfigExactKeys
	computed.Delta.GlobalStrings = computed.Effective.GlobalStrings - computed.Raw.GlobalStrings
	return computed
}

func newMessageLoggerCensusRegistry(t *testing.T) *component.Registry {
	t.Helper()
	registry := component.NewRegistry()
	require.NoError(t, componentregistry.Register(registry))
	require.NoError(t, graphresearch.RegisterComponents(registry))
	require.NoError(t, document.Register(registry))
	require.NoError(t, iotsensor.Register(registry))
	require.NoError(t, mission.Register(registry))
	return registry
}

func messageLoggerCensusDependencies() component.Dependencies {
	client := &natsclient.Client{}
	return component.Dependencies{
		NATSClient: client, Logger: slog.Default(), MetricsRegistry: metric.NewMetricsRegistry(),
		ModelRegistry: &model.Registry{}, PayloadRegistry: payloadregistry.New(),
		LifecycleManager: lifecycle.NewManager(client, slog.Default()),
	}
}

func collectRawCensusRows(
	t *testing.T,
	instance string,
	factory string,
	raw json.RawMessage,
	rows map[censusRow]int,
	keys, global map[string]struct{},
) {
	t.Helper()
	var cfg struct {
		Ports component.PortConfig `json:"ports"`
	}
	require.NoError(t, json.Unmarshal(raw, &cfg), instance)
	collectRawDefinitions(t, instance, factory, component.DirectionInput, cfg.Ports.Inputs, rows, keys, global)
	collectRawDefinitions(t, instance, factory, component.DirectionOutput, cfg.Ports.Outputs, rows, keys, global)
}

func collectRawDefinitions(
	t *testing.T,
	instance string,
	factory string,
	direction component.Direction,
	definitions []component.PortDefinition,
	rows map[censusRow]int,
	keys, global map[string]struct{},
) {
	t.Helper()
	for _, definition := range definitions {
		port, err := definition.Resolve(direction)
		require.NoError(t, err, "%s/%s", instance, definition.Name)
		facts, err := port.Facts()
		require.NoError(t, err, "%s/%s", instance, definition.Name)
		collectCensusFacts(instance, factory, direction, definition.Name, facts, rows, keys, global)
	}
}

func collectEffectiveCensusRows(
	instance string,
	factory string,
	direction component.Direction,
	ports []component.Port,
	facts []component.PortFacts,
	rows map[censusRow]int,
	keys, global map[string]struct{},
) {
	for index, port := range ports {
		collectCensusFacts(instance, factory, direction, port.Name, facts[index], rows, keys, global)
	}
}

func collectCensusFacts(
	instance string,
	factory string,
	direction component.Direction,
	portName string,
	facts component.PortFacts,
	rows map[censusRow]int,
	keys, global map[string]struct{},
) {
	switch facts.Kind() {
	case component.PortKindNATS, component.PortKindNATSRequest, component.PortKindJetStream:
	default:
		return
	}
	for _, subject := range facts.NATSSubjects() {
		rows[censusRow{
			Component: instance, Factory: factory, Direction: direction,
			Port: portName, Kind: facts.Kind(), Subject: subject,
		}]++
		keys[subject] = struct{}{}
		global[subject] = struct{}{}
	}
}

func deriveExactCollapses(
	t *testing.T,
	rawRows, effectiveRows map[censusRow]int,
	rawKeys map[string]struct{},
) exactCollapseCounts {
	t.Helper()
	rows := make([]censusRow, 0, len(effectiveRows))
	for row := range effectiveRows {
		rows = append(rows, row)
	}
	slices.SortFunc(rows, func(left, right censusRow) int {
		return strings.Compare(fmt.Sprintf("%s\x00%s\x00%s\x00%s\x00%s\x00%s",
			left.Subject, left.Factory, left.Component, left.Direction, left.Port, left.Kind),
			fmt.Sprintf("%s\x00%s\x00%s\x00%s\x00%s\x00%s",
				right.Subject, right.Factory, right.Component, right.Direction, right.Port, right.Kind))
	})
	novelSubjectClaimed := make(map[string]bool)
	result := exactCollapseCounts{}
	for _, row := range rows {
		added := effectiveRows[row] - rawRows[row]
		for range added {
			_, existedRaw := rawKeys[row.Subject]
			if !existedRaw && !novelSubjectClaimed[row.Subject] {
				novelSubjectClaimed[row.Subject] = true
				continue
			}
			switch row.Factory {
			case "agentic-loop", "agentic-dispatch":
				result.LoopDispatch++
			case "agentic-governance":
				result.Governance++
			default:
				t.Fatalf("unexpected exact subject collapse from factory %q in row %+v", row.Factory, row)
			}
			result.Total++
		}
	}
	return result
}

func censusRowCount(rows map[censusRow]int) int {
	total := 0
	for _, count := range rows {
		total += count
	}
	return total
}

func censusKindKey(t *testing.T, row censusRow) string {
	t.Helper()
	key := strings.ReplaceAll(string(row.Kind), "-", "_") + "_" + string(row.Direction) + "s"
	if _, ok := map[string]struct{}{
		"jetstream_inputs": {}, "jetstream_outputs": {}, "nats_inputs": {},
		"nats_outputs": {}, "nats_request_inputs": {},
	}[key]; !ok {
		t.Fatalf("unexpected added census kind %s for %+v", key, row)
	}
	return key
}

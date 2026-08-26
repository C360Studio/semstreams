package composition_test

import (
	"bytes"
	"encoding/json"
	"errors"
	"reflect"
	"strings"
	"testing"

	"github.com/c360studio/semstreams/component"
	"github.com/c360studio/semstreams/composition"
	"github.com/c360studio/semstreams/config"
	"github.com/c360studio/semstreams/types"
)

var allFindingTypes = []string{
	composition.TypeConfigInvalid,
	composition.TypeUnknownComponent,
	composition.TypeComponentTypeMismatch,
	composition.TypeComponentConfigInvalid,
	composition.TypePortDeclarationError,
	composition.TypeExclusiveResourceConflict,
	composition.TypeConnectionPatternError,
	composition.TypeStreamRequirement,
	composition.TypeDisconnectedNode,
	composition.TypeOrphanedPort,
	composition.TypeInterfaceMismatch,
	composition.TypeMissingInterface,
	composition.TypeEmptyComposition,
}

// TestValidateFindingsVocabularyIsClosed drives compositions that exhibit each
// of the thirteen conditions and asserts every emitted type is one of the
// thirteen constants and every constant is emitted at least once.
func TestValidateFindingsVocabularyIsClosed(t *testing.T) {
	registry := fakeRegistry(t,
		fakeSpec{name: "src", typ: "input", outputs: []component.PortDefinition{natsOut("out", "data.raw", nil)}},
		fakeSpec{name: "sink", typ: "output", inputs: []component.PortDefinition{jetStreamIn("in", "DATA", "data.raw", true)}},
		fakeSpec{name: "typed-src", typ: "input", outputs: []component.PortDefinition{natsOut("out", "typed.raw", &component.InterfaceContract{Type: "a.v1"})}},
		fakeSpec{name: "typed-sink", typ: "output", inputs: []component.PortDefinition{natsIn("in", "typed.raw", true, &component.InterfaceContract{Type: "b.v1"})}},
		fakeSpec{name: "untyped-src", typ: "input", outputs: []component.PortDefinition{natsOut("out", "untyped.raw", nil)}},
		fakeSpec{name: "requiring-sink", typ: "output", inputs: []component.PortDefinition{natsIn("in", "untyped.raw", true, &component.InterfaceContract{Type: "c.v1"})}},
		fakeSpec{name: "lonely", typ: "processor", inputs: []component.PortDefinition{natsIn("in", "nobody.publishes", false, nil)}},
		fakeSpec{name: "needy", typ: "processor", inputs: []component.PortDefinition{jetStreamIn("in", "NOBODY", "nobody.streams", true)}},
		fakeSpec{name: "listener", typ: "input", inputs: []component.PortDefinition{{Name: "sock", Required: true, Config: component.NetworkPort{Protocol: "udp", Host: "0.0.0.0", Port: 14550}}}},
		fakeSpec{name: "writer", typ: "processor", outputs: []component.PortDefinition{{Name: "idx", Config: component.KVWritePort{Bucket: "SHARED"}}}},
		fakeSpec{name: "broken", typ: "processor", declareErr: errors.New("declarer refused")},
	)
	cases := map[string]*config.Config{
		"config_invalid": {Platform: config.PlatformConfig{ID: "x"}, Components: config.ComponentConfigs{"a": instance("src", types.ComponentTypeInput)}},
		"unknown":        compositionOf(config.ComponentConfigs{"ghost": instance("nope", types.ComponentTypeProcessor)}),
		"type_mismatch":  compositionOf(config.ComponentConfigs{"a": instance("src", types.ComponentTypeProcessor)}),
		"config_bad":     compositionOf(config.ComponentConfigs{"a": {Name: "src", Type: "widget", Enabled: true}}),
		"declarer":       compositionOf(config.ComponentConfigs{"a": instance("broken", types.ComponentTypeProcessor)}),
		"exclusive":      compositionOf(config.ComponentConfigs{"a": instance("listener", types.ComponentTypeInput), "b": instance("listener", types.ComponentTypeInput)}),
		"pattern":        compositionOf(config.ComponentConfigs{"a": instance("writer", types.ComponentTypeProcessor), "b": instance("writer", types.ComponentTypeProcessor)}),
		"stream":         compositionOf(config.ComponentConfigs{"a": instance("src", types.ComponentTypeInput), "b": instance("sink", types.ComponentTypeOutput)}),
		"disconnected":   compositionOf(config.ComponentConfigs{"a": instance("lonely", types.ComponentTypeProcessor)}),
		"orphaned":       compositionOf(config.ComponentConfigs{"a": instance("needy", types.ComponentTypeProcessor)}),
		"interface":      compositionOf(config.ComponentConfigs{"a": instance("typed-src", types.ComponentTypeInput), "b": instance("typed-sink", types.ComponentTypeOutput)}),
		"missing":        compositionOf(config.ComponentConfigs{"a": instance("untyped-src", types.ComponentTypeInput), "b": instance("requiring-sink", types.ComponentTypeOutput)}),
		"empty":          compositionOf(config.ComponentConfigs{}),
	}
	allowed := map[string]bool{}
	for _, typ := range allFindingTypes {
		allowed[typ] = true
	}
	emitted := map[string]bool{}
	for name, cfg := range cases {
		result := validateRoundTrip(t, registry, cfg)
		for _, finding := range append(append([]composition.Finding(nil), result.Errors...), result.Warnings...) {
			if !allowed[finding.Type] {
				t.Errorf("case %s emitted a type outside the vocabulary: %q", name, finding.Type)
			}
			if finding.Component == "" || finding.Message == "" || finding.Suggestions == nil {
				t.Errorf("case %s finding %+v lacks component, message, or a non-nil suggestions array", name, finding)
			}
			emitted[finding.Type] = true
		}
		if result.Errors == nil || result.Warnings == nil || result.Graph.Nodes == nil || result.Graph.Edges == nil {
			t.Errorf("case %s decoded with a nil array: %+v", name, result)
		}
	}
	for _, typ := range allFindingTypes {
		if !emitted[typ] {
			t.Errorf("no case emitted %q", typ)
		}
	}
}

func TestValidateReportsUnknownComponent(t *testing.T) {
	registry := fakeRegistry(t,
		fakeSpec{name: "src", typ: "input", outputs: []component.PortDefinition{natsOut("out", "data.raw", nil)}},
		fakeSpec{name: "sink", typ: "output", inputs: []component.PortDefinition{natsIn("in", "data.raw", true, nil)}},
	)
	result := validateRoundTrip(t, registry, compositionOf(config.ComponentConfigs{
		"a":     instance("src", types.ComponentTypeInput),
		"b":     instance("sink", types.ComponentTypeOutput),
		"ghost": instance("nope", types.ComponentTypeProcessor),
	}))
	unknown := findingsOfType(result.Errors, composition.TypeUnknownComponent)
	if len(unknown) != 1 {
		t.Fatalf("unknown_component findings = %d, want 1: %+v", len(unknown), result.Errors)
	}
	if unknown[0].Component != "ghost" || !strings.Contains(unknown[0].Message, "nope") {
		t.Fatalf("unknown_component names %q / %q, want instance ghost and factory nope", unknown[0].Component, unknown[0].Message)
	}
	if len(result.Graph.Nodes) != 2 || len(result.Graph.Edges) != 1 {
		t.Fatalf("the other components were not analyzed: nodes=%d edges=%d", len(result.Graph.Nodes), len(result.Graph.Edges))
	}
	if result.Status != composition.StatusErrors {
		t.Fatalf("status = %q, want errors", result.Status)
	}
}

func TestValidateReportsRequiredStreamInputWithoutPublisher(t *testing.T) {
	registry := fakeRegistry(t,
		fakeSpec{name: "consumer", typ: "processor", inputs: []component.PortDefinition{
			jetStreamIn("must", "MUST", "must.have", true),
			jetStreamIn("maybe", "MAYBE", "maybe.have", false),
		}},
	)
	result := validateRoundTrip(t, registry, compositionOf(config.ComponentConfigs{
		"c": instance("consumer", types.ComponentTypeProcessor),
	}))
	errs := findingsOfType(result.Errors, composition.TypeOrphanedPort)
	if len(errs) != 1 || errs[0].Component != "c" || errs[0].Port != "must" {
		t.Fatalf("orphaned_port errors = %+v, want one naming c/must", errs)
	}
	warns := findingsOfType(result.Warnings, composition.TypeOrphanedPort)
	if len(warns) != 1 || warns[0].Component != "c" || warns[0].Port != "maybe" {
		t.Fatalf("orphaned_port warnings = %+v, want one naming c/maybe", warns)
	}
}

func TestValidateReportsInterfaceMismatch(t *testing.T) {
	registry := fakeRegistry(t,
		fakeSpec{name: "typed-src", typ: "input", outputs: []component.PortDefinition{natsOut("out", "typed.raw", &component.InterfaceContract{Type: "a.v1"})}},
		fakeSpec{name: "typed-sink", typ: "output", inputs: []component.PortDefinition{natsIn("in", "typed.raw", true, &component.InterfaceContract{Type: "b.v1"})}},
		fakeSpec{name: "untyped-src", typ: "input", outputs: []component.PortDefinition{natsOut("out", "untyped.raw", nil)}},
		fakeSpec{name: "requiring-sink", typ: "output", inputs: []component.PortDefinition{natsIn("in", "untyped.raw", true, &component.InterfaceContract{Type: "c.v1"})}},
	)
	result := validateRoundTrip(t, registry, compositionOf(config.ComponentConfigs{
		"ts": instance("typed-src", types.ComponentTypeInput),
		"tk": instance("typed-sink", types.ComponentTypeOutput),
		"us": instance("untyped-src", types.ComponentTypeInput),
		"rk": instance("requiring-sink", types.ComponentTypeOutput),
	}))
	mismatch := findingsOfType(result.Errors, composition.TypeInterfaceMismatch)
	if len(mismatch) != 1 || !strings.Contains(mismatch[0].Component, "ts") || !strings.Contains(mismatch[0].Component, "tk") {
		t.Fatalf("interface_mismatch = %+v, want one naming ts and tk", mismatch)
	}
	missing := findingsOfType(result.Warnings, composition.TypeMissingInterface)
	if len(missing) != 1 || !strings.Contains(missing[0].Component, "us") || !strings.Contains(missing[0].Component, "rk") {
		t.Fatalf("missing_interface = %+v, want one naming us and rk", missing)
	}
}

func TestValidateReportsStreamRequirement(t *testing.T) {
	registry := fakeRegistry(t,
		fakeSpec{name: "core-src", typ: "input", outputs: []component.PortDefinition{natsOut("out", "data.raw", nil)}},
		fakeSpec{name: "js-sink", typ: "output", inputs: []component.PortDefinition{jetStreamIn("in", "DATA", "data.raw", true)}},
	)
	result := validateRoundTrip(t, registry, compositionOf(config.ComponentConfigs{
		"pub1": instance("core-src", types.ComponentTypeInput),
		"pub2": instance("core-src", types.ComponentTypeInput),
		"sub":  instance("js-sink", types.ComponentTypeOutput),
	}))
	findings := findingsOfType(result.Errors, composition.TypeStreamRequirement)
	if len(findings) != 1 || findings[0].Component != "sub" || findings[0].Port != "in" {
		t.Fatalf("stream_requirement = %+v, want one naming sub/in", findings)
	}
	for _, publisher := range []string{"pub1", "pub2"} {
		if !strings.Contains(findings[0].Message, publisher) {
			t.Errorf("stream_requirement message %q does not name publisher %s", findings[0].Message, publisher)
		}
	}
}

func TestValidateReportsConnectionPatternConflict(t *testing.T) {
	registry := fakeRegistry(t,
		fakeSpec{name: "writer", typ: "processor", outputs: []component.PortDefinition{{Name: "idx", Config: component.KVWritePort{Bucket: "SHARED"}}}},
	)
	result := validateRoundTrip(t, registry, compositionOf(config.ComponentConfigs{
		"w1": instance("writer", types.ComponentTypeProcessor),
		"w2": instance("writer", types.ComponentTypeProcessor),
	}))
	findings := findingsOfType(result.Errors, composition.TypeConnectionPatternError)
	if len(findings) != 1 || findings[0].Severity != composition.SeverityError {
		t.Fatalf("connection_pattern_error = %+v, want exactly one error", findings)
	}
}

func TestValidateReportsExclusiveResourceConflict(t *testing.T) {
	registry := fakeRegistry(t,
		fakeSpec{name: "listener", typ: "input", inputs: []component.PortDefinition{{Name: "sock", Required: true, Config: component.NetworkPort{Protocol: "udp", Host: "0.0.0.0", Port: 14550}}}},
	)
	result := validateRoundTrip(t, registry, compositionOf(config.ComponentConfigs{
		"l1": instance("listener", types.ComponentTypeInput),
		"l2": instance("listener", types.ComponentTypeInput),
	}))
	findings := findingsOfType(result.Errors, composition.TypeExclusiveResourceConflict)
	if len(findings) != 1 || findings[0].Severity != composition.SeverityError || findings[0].Component != "l2" {
		t.Fatalf("exclusive_resource_conflict = %+v, want exactly one error naming l2", findings)
	}
	if !strings.Contains(findings[0].Message, "l1") {
		t.Fatalf("conflict message %q does not name the other holder l1", findings[0].Message)
	}
}

func TestValidateIsDeterministic(t *testing.T) {
	registry := fakeRegistry(t,
		fakeSpec{name: "src", typ: "input", outputs: []component.PortDefinition{jetStreamOut("out", "DATA", "data.>")}},
		fakeSpec{name: "proc", typ: "processor",
			inputs:  []component.PortDefinition{jetStreamIn("in", "DATA", "data.>", true)},
			outputs: []component.PortDefinition{natsOut("out", "proc.done", nil), {Name: "idx", Config: component.KVWritePort{Bucket: "IDX"}}}},
		fakeSpec{name: "sink", typ: "output", inputs: []component.PortDefinition{natsIn("in", "proc.done", true, nil), {Name: "watch", Config: component.KVWatchPort{Bucket: "IDX"}}}},
	)
	cfg := compositionOf(config.ComponentConfigs{
		"zeta-src":  instance("src", types.ComponentTypeInput),
		"alpha-src": instance("src", types.ComponentTypeInput),
		"proc-1":    instance("proc", types.ComponentTypeProcessor),
		"proc-2":    instance("proc", types.ComponentTypeProcessor),
		"sink-1":    instance("sink", types.ComponentTypeOutput),
		"sink-2":    instance("sink", types.ComponentTypeOutput),
	})
	var runs [][]byte
	for i := 0; i < 5; i++ {
		result, err := composition.Validate(registry, cfg)
		if err != nil {
			t.Fatal(err)
		}
		data, err := json.Marshal(result)
		if err != nil {
			t.Fatal(err)
		}
		runs = append(runs, data)
	}
	for i := 1; i < len(runs); i++ {
		if !bytes.Equal(runs[0], runs[i]) {
			t.Fatalf("run %d differs from run 0:\n%s\n---\n%s", i, runs[0], runs[i])
		}
	}
	var decoded composition.Result
	if err := json.Unmarshal(runs[0], &decoded); err != nil {
		t.Fatal(err)
	}
	instances := make([]string, 0, len(decoded.Graph.Nodes))
	for _, node := range decoded.Graph.Nodes {
		instances = append(instances, node.Instance)
	}
	if want := []string{"alpha-src", "proc-1", "proc-2", "sink-1", "sink-2", "zeta-src"}; !reflect.DeepEqual(instances, want) {
		t.Fatalf("nodes are not in instance-name order: %v", instances)
	}
}

// TestValidateStreamRequirementSatisfiedByExplicitStream — a JetStream
// subscriber fed only by core-NATS publishers is NOT a stream_requirement
// finding when an explicit `streams` declaration covers its subjects: the
// stream exists and captures the core-NATS publishes, so the subscriber is fed.
func TestValidateStreamRequirementSatisfiedByExplicitStream(t *testing.T) {
	registry := fakeRegistry(t,
		fakeSpec{name: "core-src", typ: "input", outputs: []component.PortDefinition{natsOut("out", "data.raw", nil)}},
		fakeSpec{name: "js-sink", typ: "output", inputs: []component.PortDefinition{jetStreamIn("in", "DATA", "data.raw", true)}},
	)
	cfg := compositionOf(config.ComponentConfigs{
		"pub": instance("core-src", types.ComponentTypeInput),
		"sub": instance("js-sink", types.ComponentTypeOutput),
	})
	cfg.Streams = config.StreamConfigs{"DATA": {Subjects: []string{"data.>"}}}

	result := validateRoundTrip(t, registry, cfg)
	if findings := findingsOfType(result.Errors, composition.TypeStreamRequirement); len(findings) != 0 {
		t.Fatalf("stream_requirement reported although streams.DATA covers data.raw: %+v", findings)
	}
	if len(result.Graph.Edges) != 1 {
		t.Fatalf("edges = %d, want the pub→sub edge to still be derived", len(result.Graph.Edges))
	}

	// A stream that does not cover the subscriber's subjects does not satisfy it.
	cfg.Streams = config.StreamConfigs{"OTHER": {Subjects: []string{"other.>"}}}
	result = validateRoundTrip(t, registry, cfg)
	if findings := findingsOfType(result.Errors, composition.TypeStreamRequirement); len(findings) != 1 {
		t.Fatalf("stream_requirement findings = %d with a non-covering stream, want 1: %+v", len(findings), result.Errors)
	}
}

package component

import (
	"encoding/json"
	"errors"
	"reflect"
	"testing"

	"github.com/c360studio/semstreams/internal/componentadmission"
)

func TestRegistryCapturesPortsOnceAndReturnsDefensiveDeclarationSnapshots(t *testing.T) {
	registry := NewRegistry()
	created := &declarationTestComponent{outputs: []Port{
		declarationTestPort("events.created"),
		{
			Name: "listener", Direction: DirectionOutput, Required: true,
			Config: NetworkPort{Protocol: "tcp", Host: "127.0.0.1", Port: 8080},
		},
	}}
	requireNoError(t, registry.RegisterWithConfig(RegistrationConfig{
		Name: "declaration-test", Type: "processor",
		Factory: func(json.RawMessage, Dependencies) (Discoverable, error) { return created, nil },
		Ports:   created.declarePorts,
	}))

	got, err := registry.CreateComponent(
		componentadmission.Access{}, "worker", declarationTestConfig("declaration-test", `{}`), declarationTestDeps(), nil)
	requireNoError(t, err)
	if got != created {
		t.Fatal("CreateComponent returned a different component")
	}
	if created.inputCalls != 1 || created.outputCalls != 1 {
		t.Fatalf("port capture calls = input:%d output:%d, want exactly one each", created.inputCalls, created.outputCalls)
	}

	single, ok := registry.Snapshot(componentadmission.Access{}, "worker")
	if !ok {
		t.Fatal("Snapshot did not return admitted worker")
	}
	assertWorkerDeclarationUnchanged(t, single)
	mutateDeclarationSnapshot(single)

	rereadSingle, ok := registry.Snapshot(componentadmission.Access{}, "worker")
	if !ok {
		t.Fatal("second Snapshot did not return admitted worker")
	}
	assertWorkerDeclarationUnchanged(t, rereadSingle)

	all := registry.Snapshots(componentadmission.Access{})
	if len(all) != 1 {
		t.Fatalf("Snapshots length = %d, want 1", len(all))
	}
	assertWorkerDeclarationUnchanged(t, all[0])
	mutateDeclarationSnapshot(all[0])

	rereadAll := registry.Snapshots(componentadmission.Access{})
	if len(rereadAll) != 1 {
		t.Fatalf("second Snapshots length = %d, want 1", len(rereadAll))
	}
	assertWorkerDeclarationUnchanged(t, rereadAll[0])
	if created.inputCalls != 1 || created.outputCalls != 1 {
		t.Fatalf("port capture calls after defensive reads = input:%d output:%d, want exactly one each",
			created.inputCalls, created.outputCalls)
	}
}

func mutateDeclarationSnapshot(snapshot declarationSnapshot) {
	outputs := snapshot.Outputs()
	outputJetstream := outputs[0].Config.(JetStreamPort)
	outputJetstream.Subjects[0] = "output-method.mutated"
	facts := snapshot.OutputDeclarationFacts()
	facts[0].natsSubjects[0] = "facts-method.mutated"
	facts[0].stream.subjects[0] = "stream-facts-method.mutated"

	jetstream := snapshot.record.OutputPorts[0].Config.(JetStreamPort)
	jetstream.Subjects[0] = "events.mutated"
	snapshot.record.OutputFacts[0].natsSubjects[0] = "facts.mutated"
	snapshot.record.OutputFacts[0].stream.subjects[0] = "stream-facts.mutated"
	snapshot.record.ExclusiveResources[0] = "tcp:mutated:1"
}

func assertWorkerDeclarationUnchanged(t *testing.T, snapshot declarationSnapshot) {
	t.Helper()
	if snapshot.Name() != "worker" || snapshot.Factory() != "declaration-test" {
		t.Fatalf("snapshot identity = %q/%q, want worker/declaration-test", snapshot.Name(), snapshot.Factory())
	}
	outputs := snapshot.Outputs()
	if len(outputs) != 2 {
		t.Fatalf("output ports = %#v, want two retained declarations", outputs)
	}
	jetstream := outputs[0].Config.(JetStreamPort)
	if !reflect.DeepEqual(jetstream.Subjects, []string{"events.created"}) {
		t.Fatalf("port subjects = %v, want events.created", jetstream.Subjects)
	}
	facts := snapshot.OutputDeclarationFacts()
	if got := facts[0].NATSSubjects(); !reflect.DeepEqual(got, []string{"events.created"}) {
		t.Fatalf("fact subjects = %v, want events.created", got)
	}
	stream, ok := facts[0].Stream()
	if !ok || !reflect.DeepEqual(stream.Subjects(), []string{"events.created"}) {
		t.Fatalf("stream fact subjects = %v, present=%v, want events.created", stream.Subjects(), ok)
	}
	if !reflect.DeepEqual(snapshot.record.ExclusiveResources, []string{"tcp:127.0.0.1:8080"}) {
		t.Fatalf("exclusive resources = %v, want tcp:127.0.0.1:8080", snapshot.record.ExclusiveResources)
	}
}

func TestRegistryHasNoLiveHandleOrReplacementSurface(t *testing.T) {
	registryType := reflect.TypeOf((*Registry)(nil))
	for _, retiredMethod := range []string{
		"Component", "GetComponent", "ListComponents", "ReplaceComponent",
		"RemoveComponent", "UnregisterInstance", "ReserveReplacement",
	} {
		if _, ok := registryType.MethodByName(retiredMethod); ok {
			t.Errorf("Registry retains retired live-handle method %q", retiredMethod)
		}
	}
}

func TestRegistryManagedPreparationFailurePublishesNothing(t *testing.T) {
	registry := NewRegistry()
	requireNoError(t, registry.RegisterWithConfig(RegistrationConfig{
		Name: "declaration-test", Type: "processor",
		Factory: func(json.RawMessage, Dependencies) (Discoverable, error) {
			return &declarationTestComponent{}, nil
		},
		Ports: noPorts,
	}))
	want := errors.New("initialize failed")
	_, err := registry.CreateComponent(
		componentadmission.Access{}, "worker", declarationTestConfig("declaration-test", `{}`), declarationTestDeps(),
		func(Discoverable) error { return want },
	)
	if !errors.Is(err, want) {
		t.Fatalf("CreateComponent error = %v, want %v", err, want)
	}
	if _, ok := registry.declaration("worker"); ok {
		t.Fatal("failed preparation published a declaration")
	}
}

func TestRegistrySnapshotReportsCompleteBootSetAndSealRejectsLaterAdmission(t *testing.T) {
	registry := NewRegistry()
	factoryCalls := 0
	requireNoError(t, registry.RegisterWithConfig(RegistrationConfig{
		Name: "declaration-test", Type: "processor",
		Factory: func(json.RawMessage, Dependencies) (Discoverable, error) {
			factoryCalls++
			return &declarationTestComponent{}, nil
		},
		Ports: noPorts,
	}))
	_, err := registry.CreateComponent(
		componentadmission.Access{}, "worker-a", declarationTestConfig("declaration-test", `{}`), declarationTestDeps(), nil,
	)
	requireNoError(t, err)
	_, err = registry.CreateComponent(
		componentadmission.Access{}, "worker-b", declarationTestConfig("declaration-test", `{}`), declarationTestDeps(), nil,
	)
	requireNoError(t, err)
	admitted := registry.Snapshots(componentadmission.Access{})
	if len(admitted) != 2 || admitted[0].Name() != "worker-a" || admitted[1].Name() != "worker-b" {
		t.Fatalf("boot declaration snapshot = %#v, want complete worker-a/worker-b set", admitted)
	}

	registry.SealComposition(componentadmission.Access{})
	_, err = registry.CreateComponent(
		componentadmission.Access{}, "late", declarationTestConfig("declaration-test", `{}`), declarationTestDeps(), nil,
	)
	if err == nil {
		t.Fatal("post-seal component admission succeeded")
	}
	if factoryCalls != 2 {
		t.Fatalf("factory calls after rejected post-seal admission = %d, want 2", factoryCalls)
	}
	if snapshots := registry.Snapshots(componentadmission.Access{}); len(snapshots) != 2 {
		t.Fatalf("post-seal snapshots = %#v, want unchanged two-declaration set", snapshots)
	}
}

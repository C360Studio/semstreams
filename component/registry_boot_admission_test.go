package component

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/c360studio/semstreams/internal/componentadmission"
)

func TestRegistryRetainsDeclarationWithoutLiveComponent(t *testing.T) {
	registry := NewRegistry()
	created := &generationTestComponent{outputs: []Port{generationTestPort("events.created")}}
	requireNoError(t, registry.RegisterWithConfig(RegistrationConfig{
		Name: "generation-test", Type: "processor",
		Factory: func(json.RawMessage, Dependencies) (Discoverable, error) { return created, nil },
	}))

	got, err := registry.CreateComponent(
		componentadmission.Access{}, "worker", generationTestConfig("generation-test", `{}`), generationTestDeps(), nil)
	requireNoError(t, err)
	if got != created {
		t.Fatal("CreateComponent returned a different component")
	}
	if created.inputCalls != 1 || created.outputCalls != 1 {
		t.Fatalf("port capture calls = input:%d output:%d, want exactly one each", created.inputCalls, created.outputCalls)
	}

	generation, ok := registry.generation("worker")
	if !ok || generation.FactoryIdentity != "generation-test" || generation.Generation == 0 {
		t.Fatalf("generation identity = %#v, present=%v", generation, ok)
	}
	if len(generation.OutputPorts) != 1 || len(generation.OutputFacts) != 1 {
		t.Fatalf("generation declaration = %#v", generation)
	}
}

func TestRegistryManagedPreparationFailurePublishesNothing(t *testing.T) {
	registry := NewRegistry()
	requireNoError(t, registry.RegisterWithConfig(RegistrationConfig{
		Name: "generation-test", Type: "processor",
		Factory: func(json.RawMessage, Dependencies) (Discoverable, error) {
			return &generationTestComponent{}, nil
		},
	}))
	want := errors.New("initialize failed")
	_, err := registry.CreateComponent(
		componentadmission.Access{}, "worker", generationTestConfig("generation-test", `{}`), generationTestDeps(),
		func(Discoverable) error { return want },
	)
	if !errors.Is(err, want) {
		t.Fatalf("CreateComponent error = %v, want %v", err, want)
	}
	if _, ok := registry.generation("worker"); ok {
		t.Fatal("failed preparation published a generation")
	}
}

func TestRegistryObserverReportsCompleteBootSetAndSealRejectsLaterAdmission(t *testing.T) {
	registry := NewRegistry()
	factoryCalls := 0
	requireNoError(t, registry.RegisterWithConfig(RegistrationConfig{
		Name: "generation-test", Type: "processor",
		Factory: func(json.RawMessage, Dependencies) (Discoverable, error) {
			factoryCalls++
			return &generationTestComponent{}, nil
		},
	}))
	ctx, cancel := context.WithTimeout(t.Context(), time.Second)
	defer cancel()
	updates := registry.ObserveSnapshots(ctx, componentadmission.Access{})

	if initial := receiveGenerationSnapshots(t, updates); len(initial) != 0 {
		t.Fatalf("initial observer snapshot = %#v, want empty", initial)
	}
	_, err := registry.CreateComponent(
		componentadmission.Access{}, "worker-a", generationTestConfig("generation-test", `{}`), generationTestDeps(), nil,
	)
	requireNoError(t, err)
	_, err = registry.CreateComponent(
		componentadmission.Access{}, "worker-b", generationTestConfig("generation-test", `{}`), generationTestDeps(), nil,
	)
	requireNoError(t, err)
	admitted := receiveGenerationSnapshots(t, updates)
	if len(admitted) != 2 || admitted[0].Name() != "worker-a" || admitted[1].Name() != "worker-b" {
		t.Fatalf("coalesced observer snapshot = %#v, want complete worker-a/worker-b set", admitted)
	}

	registry.SealComposition(componentadmission.Access{})
	_, err = registry.CreateComponent(
		componentadmission.Access{}, "late", generationTestConfig("generation-test", `{}`), generationTestDeps(), nil,
	)
	if err == nil {
		t.Fatal("post-seal component admission succeeded")
	}
	if factoryCalls != 2 {
		t.Fatalf("factory calls after rejected post-seal admission = %d, want 2", factoryCalls)
	}
	if snapshots := registry.Snapshots(componentadmission.Access{}); len(snapshots) != 2 {
		t.Fatalf("post-seal snapshots = %#v, want unchanged two-generation set", snapshots)
	}

	cancel()
	if _, ok := <-updates; ok {
		t.Fatal("observer channel remained open after cancellation")
	}
	registry.mu.RLock()
	observerCount := len(registry.observers)
	registry.mu.RUnlock()
	if observerCount != 0 {
		t.Fatalf("observer resources after cancellation = %d, want 0", observerCount)
	}
}

func receiveGenerationSnapshots(t *testing.T, updates <-chan []generationSnapshot) []generationSnapshot {
	t.Helper()
	select {
	case snapshots, ok := <-updates:
		if !ok {
			t.Fatal("observer channel closed before snapshot")
		}
		return snapshots
	case <-t.Context().Done():
		t.Fatal("observer snapshot timed out")
		return nil
	}
}

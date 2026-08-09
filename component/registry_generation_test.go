package component

import (
	"context"
	"encoding/json"
	"errors"
	"reflect"
	"testing"
	"time"

	"github.com/c360studio/semstreams/internal/componentadmission"
	"github.com/c360studio/semstreams/natsclient"
	"github.com/c360studio/semstreams/types"
)

type generationTestComponent struct {
	inputs      []Port
	outputs     []Port
	inputCalls  int
	outputCalls int
}

func (c *generationTestComponent) Meta() Metadata             { return Metadata{Name: "generation-test"} }
func (c *generationTestComponent) ConfigSchema() ConfigSchema { return ConfigSchema{} }
func (c *generationTestComponent) Health() HealthStatus       { return HealthStatus{} }
func (c *generationTestComponent) DataFlow() FlowMetrics      { return FlowMetrics{} }
func (c *generationTestComponent) InputPorts() []Port {
	c.inputCalls++
	return c.inputs
}
func (c *generationTestComponent) OutputPorts() []Port {
	c.outputCalls++
	return c.outputs
}

func generationTestPort(subject string) Port {
	return Port{
		Name:      "events",
		Direction: DirectionOutput,
		Required:  true,
		Config: JetStreamPort{
			Subjects: []string{subject},
		},
	}
}

func generationTestConfig(factory string, raw string) types.ComponentConfig {
	return types.ComponentConfig{
		Type:    types.ComponentTypeProcessor,
		Name:    factory,
		Enabled: true,
		Config:  json.RawMessage(raw),
	}
}

func generationTestDeps() Dependencies {
	return Dependencies{NATSClient: new(natsclient.Client)}
}

func TestRegistryGenerationCapturesPortsOnceAndReturnsDefensiveClones(t *testing.T) {
	registry := NewRegistry()
	created := &generationTestComponent{outputs: []Port{generationTestPort("events.created")}}
	requireNoError(t, registry.RegisterWithConfig(RegistrationConfig{
		Name: "generation-test", Type: "processor",
		Factory: func(json.RawMessage, Dependencies) (Discoverable, error) { return created, nil },
	}))

	got, err := registry.CreateComponent(
		"worker", generationTestConfig("generation-test", `{}`), generationTestDeps())
	requireNoError(t, err)
	if got != created {
		t.Fatal("CreateComponent returned a different component")
	}
	if created.inputCalls != 1 || created.outputCalls != 1 {
		t.Fatalf("port capture calls = input:%d output:%d, want exactly one each", created.inputCalls, created.outputCalls)
	}

	generation, ok := registry.generation("worker")
	if !ok {
		t.Fatal("admitted generation is absent")
	}
	if generation.FactoryIdentity != "generation-test" || generation.Component != created || generation.Generation == 0 {
		t.Fatalf("generation identity = %#v", generation)
	}
	if len(generation.OutputPorts) != 1 || len(generation.OutputFacts) != 1 {
		t.Fatalf("generation declaration = %#v", generation)
	}

	port := generation.OutputPorts[0]
	port.Config.(JetStreamPort).Subjects[0] = "mutated"
	generation.OutputPorts[0] = Port{}
	generation.OutputFacts[0] = PortFacts{}
	generation.ExclusiveResources = append(generation.ExclusiveResources, "mutated")

	again, ok := registry.generation("worker")
	if !ok {
		t.Fatal("generation disappeared")
	}
	if got := again.OutputFacts[0].NATSSubjects(); len(got) != 1 || got[0] != "events.created" {
		t.Fatalf("reader mutation changed retained facts: %#v", got)
	}
	if got := again.OutputPorts[0].Config.(JetStreamPort).Subjects; len(got) != 1 || got[0] != "events.created" {
		t.Fatalf("reader mutation changed retained ports: %#v", got)
	}
	if created.inputCalls != 1 || created.outputCalls != 1 {
		t.Fatalf("reads re-called component ports = input:%d output:%d", created.inputCalls, created.outputCalls)
	}
}

func TestRegistryFailedAdmissionPublishesNoGeneration(t *testing.T) {
	registry := NewRegistry()
	invalid := &generationTestComponent{outputs: []Port{{
		Name: "broken", Direction: DirectionOutput, Config: NATSPort{},
	}}}
	requireNoError(t, registry.RegisterWithConfig(RegistrationConfig{
		Name: "invalid", Type: "processor",
		Factory: func(json.RawMessage, Dependencies) (Discoverable, error) { return invalid, nil },
	}))

	if _, err := registry.CreateComponent("broken", generationTestConfig("invalid", `{}`), generationTestDeps()); err == nil {
		t.Fatal("invalid declaration was admitted")
	}
	if _, ok := registry.generation("broken"); ok {
		t.Fatal("failed admission published a generation")
	}
	if got := registry.generationsSnapshot(); len(got) != 0 {
		t.Fatalf("failed admission left complete-set state: %#v", got)
	}
}

func TestRegistryDisabledAndConflictingAdmissionPublishNoGeneration(t *testing.T) {
	registry := NewRegistry()
	factoryCalls := 0
	requireNoError(t, registry.RegisterWithConfig(RegistrationConfig{
		Name: "exclusive", Type: "processor",
		Factory: func(json.RawMessage, Dependencies) (Discoverable, error) {
			factoryCalls++
			return &generationTestComponent{outputs: []Port{{
				Name: "listener", Direction: DirectionOutput,
				Config: NetworkPort{Protocol: "tcp", Host: "0.0.0.0", Port: 8080},
			}}}, nil
		},
	}))
	disabled := generationTestConfig("exclusive", `{}`)
	disabled.Enabled = false
	if _, err := registry.CreateComponent("disabled", disabled, generationTestDeps()); err == nil {
		t.Fatal("disabled component was admitted")
	}
	if factoryCalls != 0 {
		t.Fatalf("disabled admission invoked factory %d times", factoryCalls)
	}

	_, err := registry.CreateComponent(
		"owner", generationTestConfig("exclusive", `{}`), generationTestDeps())
	requireNoError(t, err)
	if _, err := registry.CreateComponent(
		"rival", generationTestConfig("exclusive", `{}`), generationTestDeps(),
	); err == nil {
		t.Fatal("exclusive-resource conflict was admitted")
	}
	if _, ok := registry.generation("rival"); ok {
		t.Fatal("conflict exposed a partial rival generation")
	}
	owner, ok := registry.generation("owner")
	if !ok || len(owner.ExclusiveResources) != 1 || owner.ExclusiveResources[0] != "tcp:0.0.0.0:8080" {
		t.Fatalf("owner resource projection = %#v", owner.ExclusiveResources)
	}
}

func TestComponentGenerationShapeIsAdmissionOnlyAndGroupNeutral(t *testing.T) {
	typeOfGeneration := reflect.TypeOf(componentGeneration{})
	for _, forbidden := range []string{
		"Enabled", "Started", "Healthy", "Ready", "ProviderPhase", "Group", "Cohort", "Orchestration",
	} {
		if _, ok := typeOfGeneration.FieldByName(forbidden); ok {
			t.Fatalf("ComponentGeneration exposes forbidden %s state", forbidden)
		}
	}
}

func TestRegistryObserverStartsEmptyCoalescesAndCancels(t *testing.T) {
	registry := NewRegistry()
	ctx, cancel := context.WithCancel(context.Background())
	updates := registry.observeGenerations(ctx)

	initial := <-updates
	if len(initial) != 0 {
		t.Fatalf("initial generation set = %#v, want empty", initial)
	}

	for index, subject := range []string{"one", "two", "three"} {
		factory := subject
		component := &generationTestComponent{outputs: []Port{generationTestPort(subject)}}
		requireNoError(t, registry.RegisterWithConfig(RegistrationConfig{
			Name: factory, Type: "processor",
			Factory: func(json.RawMessage, Dependencies) (Discoverable, error) { return component, nil },
		}))
		_, err := registry.CreateComponent(
			string(rune('a'+index)), generationTestConfig(factory, `{}`), generationTestDeps())
		requireNoError(t, err)
	}

	latest := <-updates
	if len(latest) != 3 {
		t.Fatalf("coalesced generation set has %d records, want newest complete set of 3", len(latest))
	}

	cancel()
	select {
	case _, ok := <-updates:
		if ok {
			t.Fatal("observer delivered after cancellation")
		}
	case <-time.After(time.Second):
		t.Fatal("observer resources were not released on cancellation")
	}
}

func TestRegistryReplacementFailurePreservesOldAndSuccessSwapsWholeGeneration(t *testing.T) {
	registry := NewRegistry()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	updates := registry.observeGenerations(ctx)
	<-updates // initial empty set
	old := &generationTestComponent{outputs: []Port{generationTestPort("old")}}
	replacement := &generationTestComponent{outputs: []Port{generationTestPort("new")}}
	requireNoError(t, registry.RegisterWithConfig(RegistrationConfig{
		Name: "old-factory", Type: "processor",
		Factory: func(json.RawMessage, Dependencies) (Discoverable, error) { return old, nil },
	}))
	requireNoError(t, registry.RegisterWithConfig(RegistrationConfig{
		Name: "new-factory", Type: "processor",
		Factory: func(json.RawMessage, Dependencies) (Discoverable, error) { return replacement, nil },
	}))
	_, err := registry.CreateComponent(
		"worker", generationTestConfig("old-factory", `{}`), generationTestDeps())
	requireNoError(t, err)
	before, _ := registry.generation("worker")
	if admitted := <-updates; len(admitted) != 1 || admitted[0].Generation != before.Generation {
		t.Fatalf("observer admission set = %#v", admitted)
	}

	prepareErr := errors.New("prepare failed")
	_, err = registry.ReplaceComponent(
		componentadmission.Access{}, "worker", generationTestConfig("new-factory", `{}`), generationTestDeps(),
		func(Discoverable) (func() error, error) { return nil, prepareErr },
	)
	if !errors.Is(err, prepareErr) {
		t.Fatalf("ReplaceComponent error = %v, want prepare failure", err)
	}
	afterFailure, _ := registry.generation("worker")
	if afterFailure.Component != old || afterFailure.FactoryIdentity != before.FactoryIdentity ||
		afterFailure.Generation != before.Generation || afterFailure.OutputFacts[0].NATSSubjects()[0] != "old" {
		t.Fatalf("failed preparation changed generation: before=%#v after=%#v", before, afterFailure)
	}

	got, err := registry.ReplaceComponent(
		componentadmission.Access{}, "worker", generationTestConfig("new-factory", `{}`), generationTestDeps(), nil)
	requireNoError(t, err)
	if got != replacement {
		t.Fatal("replacement returned a different component")
	}
	afterSuccess, _ := registry.generation("worker")
	if afterSuccess.Component != replacement || afterSuccess.FactoryIdentity != "new-factory" ||
		afterSuccess.Generation == before.Generation || afterSuccess.OutputFacts[0].NATSSubjects()[0] != "new" {
		t.Fatalf("successful replacement did not swap one whole generation: %#v", afterSuccess)
	}
	if replaced := <-updates; len(replaced) != 1 ||
		replaced[0].Generation != afterSuccess.Generation || replaced[0].OutputFacts[0].NATSSubjects()[0] != "new" {
		t.Fatalf("observer replacement set = %#v", replaced)
	}

	registry.UnregisterInstance("worker")
	if removed := <-updates; len(removed) != 0 {
		t.Fatalf("observer removal set = %#v", removed)
	}
	if _, ok := registry.generation("worker"); ok || registry.Component("worker") != nil {
		t.Fatal("removal left residual generation state")
	}
}

func TestRegistryReplacementConflictIsRejectedBeforeInitialization(t *testing.T) {
	registry := NewRegistry()
	owner := &generationTestComponent{outputs: []Port{{
		Name: "owner", Direction: DirectionOutput,
		Config: NetworkPort{Protocol: "tcp", Host: "0.0.0.0", Port: 18080},
	}}}
	target := &generationTestComponent{outputs: []Port{generationTestPort("target.old")}}
	conflicting := &generationTestComponent{outputs: []Port{{
		Name: "candidate", Direction: DirectionOutput,
		Config: NetworkPort{Protocol: "tcp", Host: "0.0.0.0", Port: 18080},
	}}}
	for name, candidate := range map[string]*generationTestComponent{
		"owner-factory": owner, "target-factory": target, "candidate-factory": conflicting,
	} {
		candidate := candidate
		requireNoError(t, registry.RegisterWithConfig(RegistrationConfig{
			Name: name, Type: "processor",
			Factory: func(json.RawMessage, Dependencies) (Discoverable, error) { return candidate, nil },
		}))
	}
	_, err := registry.CreateComponent("owner", generationTestConfig("owner-factory", `{}`), generationTestDeps())
	requireNoError(t, err)
	_, err = registry.CreateComponent("target", generationTestConfig("target-factory", `{}`), generationTestDeps())
	requireNoError(t, err)

	initializeCalls := 0
	_, err = registry.ReplaceComponent(
		componentadmission.Access{}, "target", generationTestConfig("candidate-factory", `{}`), generationTestDeps(),
		func(Discoverable) (func() error, error) {
			initializeCalls++
			return nil, nil
		},
	)
	if err == nil {
		t.Fatal("resource-conflicting replacement was admitted")
	}
	if initializeCalls != 0 {
		t.Fatalf("resource-conflicting candidate initialized %d times", initializeCalls)
	}
	if got := registry.Component("target"); got != target {
		t.Fatalf("conflicting replacement changed incumbent to %T", got)
	}
}

func TestRegistryFailedReplacementReservationReleasesResourceForSuccessor(t *testing.T) {
	registry := NewRegistry()
	old := &generationTestComponent{outputs: []Port{generationTestPort("target.old")}}
	claim := &generationTestComponent{outputs: []Port{{
		Name: "listener", Direction: DirectionOutput,
		Config: NetworkPort{Protocol: "udp", Host: "0.0.0.0", Port: 18081},
	}}}
	for name, candidate := range map[string]*generationTestComponent{"old": old, "claim": claim} {
		candidate := candidate
		requireNoError(t, registry.RegisterWithConfig(RegistrationConfig{
			Name: name, Type: "processor",
			Factory: func(json.RawMessage, Dependencies) (Discoverable, error) { return candidate, nil },
		}))
	}
	_, err := registry.CreateComponent("target", generationTestConfig("old", `{}`), generationTestDeps())
	requireNoError(t, err)
	prepareErr := errors.New("initialize failed")
	_, err = registry.ReplaceComponent(
		componentadmission.Access{}, "target", generationTestConfig("claim", `{}`), generationTestDeps(),
		func(Discoverable) (func() error, error) { return nil, prepareErr },
	)
	if !errors.Is(err, prepareErr) {
		t.Fatalf("ReplaceComponent error = %v, want initialize failure", err)
	}
	if got := registry.Component("target"); got != old {
		t.Fatal("failed replacement did not preserve incumbent")
	}
	_, err = registry.CreateComponent("successor", generationTestConfig("claim", `{}`), generationTestDeps())
	requireNoError(t, err)
}

func TestRegistryCanceledReplacementReservationIsInvisibleAndCausallyRejected(t *testing.T) {
	registry := NewRegistry()
	old := &generationTestComponent{outputs: []Port{generationTestPort("target.old")}}
	claim := &generationTestComponent{outputs: []Port{{
		Name: "listener", Direction: DirectionOutput,
		Config: NetworkPort{Protocol: "udp", Host: "0.0.0.0", Port: 18082},
	}}}
	for name, candidate := range map[string]*generationTestComponent{"old": old, "claim": claim} {
		candidate := candidate
		requireNoError(t, registry.RegisterWithConfig(RegistrationConfig{
			Name: name, Type: "processor",
			Factory: func(json.RawMessage, Dependencies) (Discoverable, error) { return candidate, nil },
		}))
	}
	_, err := registry.CreateComponent("target", generationTestConfig("old", `{}`), generationTestDeps())
	requireNoError(t, err)

	prepareEntered := make(chan struct{})
	releasePrepare := make(chan struct{})
	cleanupEntered := make(chan struct{})
	releaseCleanup := make(chan struct{})
	replaceResult := make(chan error, 1)
	go func() {
		_, replaceErr := registry.ReplaceComponent(
			componentadmission.Access{}, "target", generationTestConfig("claim", `{}`), generationTestDeps(),
			func(Discoverable) (func() error, error) {
				close(prepareEntered)
				<-releasePrepare
				return func() error {
					close(cleanupEntered)
					<-releaseCleanup
					return nil
				}, nil
			},
		)
		replaceResult <- replaceErr
	}()
	<-prepareEntered
	if got := registry.Component("target"); got != old {
		t.Fatal("in-flight reservation exposed candidate before commit")
	}

	registry.UnregisterInstance("target")
	close(releasePrepare)
	<-cleanupEntered
	_, err = registry.CreateComponent("successor", generationTestConfig("claim", `{}`), generationTestDeps())
	if err == nil {
		t.Fatal("successor admitted while canceled candidate cleanup was still running")
	}
	if _, err := registry.CreateComponent(
		"target", generationTestConfig("claim", `{}`), generationTestDeps(),
	); err == nil {
		t.Fatal("same-name recreation bypassed canceled candidate quarantine")
	}
	close(releaseCleanup)
	if err := <-replaceResult; err == nil {
		t.Fatal("replacement committed after its incumbent generation was removed")
	}
	_, err = registry.CreateComponent("successor", generationTestConfig("claim", `{}`), generationTestDeps())
	requireNoError(t, err)
	if registry.Component("target") != nil || registry.Component("successor") != claim {
		t.Fatal("causal rejection disturbed successor admission")
	}
}

func TestRegistryFailedReplacementCleanupKeepsResourceQuarantined(t *testing.T) {
	registry := NewRegistry()
	old := &generationTestComponent{outputs: []Port{generationTestPort("target.old")}}
	claim := &generationTestComponent{outputs: []Port{{
		Name: "listener", Direction: DirectionOutput,
		Config: NetworkPort{Protocol: "udp", Host: "0.0.0.0", Port: 18083},
	}}}
	for name, candidate := range map[string]*generationTestComponent{"old": old, "claim": claim} {
		candidate := candidate
		requireNoError(t, registry.RegisterWithConfig(RegistrationConfig{
			Name: name, Type: "processor",
			Factory: func(json.RawMessage, Dependencies) (Discoverable, error) { return candidate, nil },
		}))
	}
	_, err := registry.CreateComponent("target", generationTestConfig("old", `{}`), generationTestDeps())
	requireNoError(t, err)
	prepareEntered := make(chan struct{})
	releasePrepare := make(chan struct{})
	cleanupErr := errors.New("candidate still owns socket")
	replaceResult := make(chan error, 1)
	go func() {
		_, replaceErr := registry.ReplaceComponent(
			componentadmission.Access{}, "target", generationTestConfig("claim", `{}`), generationTestDeps(),
			func(Discoverable) (func() error, error) {
				close(prepareEntered)
				<-releasePrepare
				return func() error { return cleanupErr }, nil
			},
		)
		replaceResult <- replaceErr
	}()
	<-prepareEntered
	registry.UnregisterInstance("target")
	close(releasePrepare)
	replaceErr := <-replaceResult
	if !errors.Is(replaceErr, cleanupErr) {
		t.Fatalf("replacement error = %v, want cleanup failure surfaced", replaceErr)
	}
	if _, err := registry.CreateComponent(
		"successor", generationTestConfig("claim", `{}`), generationTestDeps(),
	); err == nil {
		t.Fatal("successor admitted after failed cleanup released no resource")
	}
	if _, err := registry.CreateComponent(
		"target", generationTestConfig("claim", `{}`), generationTestDeps(),
	); err == nil {
		t.Fatal("same-name recreation bypassed failed-cleanup quarantine")
	}
}

func TestRegistryDeclarationProofRejectsReplacementGeneration(t *testing.T) {
	registry := NewRegistry()
	old := &generationTestComponent{outputs: []Port{generationTestPort("same")}}
	replacement := &generationTestComponent{outputs: []Port{generationTestPort("same")}}
	for name, candidate := range map[string]*generationTestComponent{"old": old, "new": replacement} {
		candidate := candidate
		requireNoError(t, registry.RegisterWithConfig(RegistrationConfig{
			Name: name, Type: "processor",
			Factory: func(json.RawMessage, Dependencies) (Discoverable, error) { return candidate, nil },
		}))
	}
	_, err := registry.CreateComponent("target", generationTestConfig("old", `{}`), generationTestDeps())
	requireNoError(t, err)
	proof, err := registry.ValidateDeclarationUpdate(
		componentadmission.Access{}, "target", generationTestConfig("old", `{}`), generationTestDeps())
	requireNoError(t, err)
	_, err = registry.ReplaceComponent(
		componentadmission.Access{}, "target", generationTestConfig("new", `{}`), generationTestDeps(), nil)
	requireNoError(t, err)
	if err := registry.ConfirmDeclarationUpdate(componentadmission.Access{}, proof); err == nil {
		t.Fatal("proof for retired generation remained valid")
	}
}

func TestRegistryDeclarationChangeIsTypedAndCheckedBeforeMutation(t *testing.T) {
	registry := NewRegistry()
	requireNoError(t, registry.RegisterWithConfig(RegistrationConfig{
		Name: "configurable", Type: "processor",
		Factory: func(raw json.RawMessage, _ Dependencies) (Discoverable, error) {
			var cfg struct {
				Subject string `json:"subject"`
			}
			if err := json.Unmarshal(raw, &cfg); err != nil {
				return nil, err
			}
			return &generationTestComponent{outputs: []Port{generationTestPort(cfg.Subject)}}, nil
		},
	}))
	_, err := registry.CreateComponent(
		"worker", generationTestConfig("configurable", `{"subject":"same"}`), generationTestDeps())
	requireNoError(t, err)

	proof, err := registry.ValidateDeclarationUpdate(
		componentadmission.Access{}, "worker", generationTestConfig("configurable", `{"subject":"same"}`), generationTestDeps())
	requireNoError(t, err)
	requireNoError(t, registry.ConfirmDeclarationUpdate(componentadmission.Access{}, proof))
	_, err = registry.ValidateDeclarationUpdate(
		componentadmission.Access{}, "worker", generationTestConfig("configurable", `{"subject":"changed"}`), generationTestDeps())
	var typed *DeclarationChangeRequiresReplacementError
	if !errors.As(err, &typed) || typed.Code != "declaration_change_requires_replacement" {
		t.Fatalf("declaration change error = %#v, want typed replacement refusal", err)
	}
}

func requireNoError(t *testing.T, err error) {
	t.Helper()
	if err != nil {
		t.Fatal(err)
	}
}

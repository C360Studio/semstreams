package service

import (
	"context"
	"errors"
	"reflect"
	"testing"

	"github.com/c360studio/semstreams/config"
	"github.com/c360studio/semstreams/types"
)

type recordingComponentConfigPublisher struct {
	failedName string
	writes     []string
	current    config.ComponentConfigs
	skipApply  bool
	nilConfig  bool
}

func (p *recordingComponentConfigPublisher) GetConfig() *config.SafeConfig {
	if p.nilConfig {
		return nil
	}
	return config.NewSafeConfig(&config.Config{Components: p.current})
}

func (p *recordingComponentConfigPublisher) PutComponentToKV(
	_ context.Context,
	name string,
	configValue types.ComponentConfig,
) error {
	p.writes = append(p.writes, name)
	if name == p.failedName {
		return errors.New("injected persistence failure")
	}
	if !p.skipApply {
		if p.current == nil {
			p.current = make(config.ComponentConfigs)
		}
		p.current[name] = configValue
	}
	return nil
}

func TestPublishCompiledComponentConfigsPrevalidatesNamesBeforeWriting(t *testing.T) {
	tests := []struct {
		name    string
		configs config.ComponentConfigs
	}{
		{
			name: "sanitized key collision",
			configs: config.ComponentConfigs{
				"sensor one": {},
				"sensor_one": {},
			},
		},
		{
			name: "dot would create a property-level config key",
			configs: config.ComponentConfigs{
				"sensor.one": {},
				"z-valid":    {},
			},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			publisher := &recordingComponentConfigPublisher{}
			service := &FlowService{configMgr: publisher}

			response, err := service.publishCompiledComponentConfigs(context.Background(), test.configs)
			if err == nil {
				t.Fatal("publishCompiledComponentConfigs() error = nil, want name rejection")
			}
			if len(publisher.writes) != 0 {
				t.Fatalf("writes = %v, want none before full prevalidation", publisher.writes)
			}
			if len(response.PersistedComponents) != 0 || response.RestartRequired {
				t.Fatalf("response = %+v, want zero persistence progress", response)
			}
		})
	}
}

func TestPublishCompiledComponentConfigsDetectsUnobservedWriteWhenDesiredStateDiffers(t *testing.T) {
	publisher := &recordingComponentConfigPublisher{
		current:   config.ComponentConfigs{},
		skipApply: true,
	}
	service := &FlowService{configMgr: publisher}

	response, err := service.publishCompiledComponentConfigs(context.Background(), config.ComponentConfigs{
		"candidate": {Name: "udp", Enabled: true},
	})
	if err == nil {
		t.Fatal("publishCompiledComponentConfigs() error = nil, want unapplied-write failure")
	}
	if got, want := publisher.writes, []string{"candidate"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("writes = %v, want %v", got, want)
	}
	if len(response.PersistedComponents) != 0 || response.FailedComponent != "candidate" {
		t.Fatalf("response = %+v, want candidate failure with zero persisted", response)
	}
}

func TestPublishCompiledComponentConfigsTreatsMissingObservationAsFailure(t *testing.T) {
	publisher := &recordingComponentConfigPublisher{nilConfig: true}
	service := &FlowService{configMgr: publisher}

	response, err := service.publishCompiledComponentConfigs(context.Background(), config.ComponentConfigs{
		"candidate": {Name: "udp", Enabled: true},
	})
	if err == nil {
		t.Fatal("publishCompiledComponentConfigs() error = nil, want missing-observation failure")
	}
	if len(response.PersistedComponents) != 0 || response.FailedComponent != "candidate" {
		t.Fatalf("response = %+v, want candidate failure with zero persisted", response)
	}
}

func TestPublishCompiledComponentConfigsSortsAndReportsExactPartialProgress(t *testing.T) {
	publisher := &recordingComponentConfigPublisher{failedName: "middle"}
	service := &FlowService{configMgr: publisher}
	configs := config.ComponentConfigs{
		"z-last":  {},
		"middle":  {},
		"a-first": {},
	}

	response, err := service.publishCompiledComponentConfigs(context.Background(), configs)
	if err == nil {
		t.Fatal("publishCompiledComponentConfigs() error = nil, want persistence failure")
	}
	if got, want := publisher.writes, []string{"a-first", "middle"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("write order = %v, want %v", got, want)
	}
	if got, want := response.PersistedComponents, []string{"a-first"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("persisted components = %v, want %v", got, want)
	}
	if response.FailedComponent != "middle" {
		t.Fatalf("failed component = %q, want middle", response.FailedComponent)
	}
	if !response.RuntimeUnchanged {
		t.Fatal("runtime_unchanged = false, want true")
	}
	if !response.RestartRequired {
		t.Fatal("restart_required = false after a persisted component, want true")
	}
}

func TestPublishCompiledComponentConfigsRetryReportsItsOwnExactProgress(t *testing.T) {
	publisher := &recordingComponentConfigPublisher{failedName: "middle"}
	service := &FlowService{configMgr: publisher}
	configs := config.ComponentConfigs{"z-last": {}, "middle": {}, "a-first": {}}

	first, err := service.publishCompiledComponentConfigs(context.Background(), configs)
	if err == nil {
		t.Fatal("first publish error = nil")
	}
	if got, want := first.PersistedComponents, []string{"a-first"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("first progress = %v, want %v", got, want)
	}

	publisher.failedName = ""
	publisher.writes = nil
	retry, err := service.publishCompiledComponentConfigs(context.Background(), configs)
	if err != nil {
		t.Fatalf("retry error = %v", err)
	}
	if got, want := publisher.writes, []string{"a-first", "middle", "z-last"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("retry writes = %v, want %v", got, want)
	}
	if got, want := retry.PersistedComponents, []string{"a-first", "middle", "z-last"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("retry progress = %v, want %v", got, want)
	}
}

func TestPublishCompiledComponentConfigsSuccessAlwaysRequiresRestart(t *testing.T) {
	publisher := &recordingComponentConfigPublisher{}
	service := &FlowService{configMgr: publisher}

	response, err := service.publishCompiledComponentConfigs(context.Background(), config.ComponentConfigs{
		"component": {},
	})
	if err != nil {
		t.Fatalf("publishCompiledComponentConfigs() error = %v", err)
	}
	if !response.RuntimeUnchanged {
		t.Fatal("runtime_unchanged = false, want true")
	}
	if !response.RestartRequired {
		t.Fatal("restart_required = false, want true")
	}
}

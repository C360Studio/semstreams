//go:build integration

package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os"
	"os/exec"
	"sort"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/c360studio/semstreams/component"
	"github.com/c360studio/semstreams/config"
	"github.com/c360studio/semstreams/natsclient"
	"github.com/c360studio/semstreams/types"
)

const (
	configBootProcessRoleEnv = "SEMSTREAMS_TEST_CONFIG_BOOT_PROCESS_ROLE"
	configBootProcessURLenv  = "SEMSTREAMS_TEST_CONFIG_BOOT_PROCESS_NATS_URL"

	configBootProcessRoleWriter = "writer"
	configBootProcessRoleReader = "reader"

	configBootProcessFactory = "config-boot-process-proof"
	configBootInitialName    = "initial-component"
	configBootDesiredName    = "desired-component"
)

type configBootProcessComponent struct {
	marker  string
	started atomic.Bool
}

func (c *configBootProcessComponent) Meta() component.Metadata {
	return component.Metadata{
		Name: configBootProcessFactory, Type: "processor", Version: "1.0.0",
		Description: "process-boundary configuration activation proof",
	}
}

func (*configBootProcessComponent) InputPorts() []component.Port  { return nil }
func (*configBootProcessComponent) OutputPorts() []component.Port { return nil }
func (*configBootProcessComponent) ConfigSchema() component.ConfigSchema {
	return component.ConfigSchema{}
}
func (c *configBootProcessComponent) Health() component.HealthStatus {
	return component.HealthStatus{Healthy: c.started.Load(), LastCheck: time.Now()}
}
func (*configBootProcessComponent) DataFlow() component.FlowMetrics {
	return component.FlowMetrics{LastActivity: time.Now()}
}
func (*configBootProcessComponent) Initialize() error { return nil }
func (c *configBootProcessComponent) Start(context.Context) error {
	c.started.Store(true)
	return nil
}
func (c *configBootProcessComponent) Stop(context.Context) error {
	c.started.Store(false)
	return nil
}

var _ component.LifecycleComponent = (*configBootProcessComponent)(nil)

// TestIntegration_ConfigBootActivationRequiresProcessRestart proves the boot
// boundary with two distinct operating-system processes. The parent keeps one
// file-backed NATS server alive while writer and reader subprocesses boot in
// sequence against the same durable configuration bucket.
func TestIntegration_ConfigBootActivationRequiresProcessRestart(t *testing.T) {
	testClient := natsclient.NewTestClient(t, natsclient.WithKV(), natsclient.WithFileStorage())
	executable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}

	for _, role := range []string{configBootProcessRoleWriter, configBootProcessRoleReader} {
		ctx, cancel := context.WithTimeout(t.Context(), 20*time.Second)
		cmd := exec.CommandContext(ctx, executable, "-test.run=^TestConfigBootProcessRestartHelper$", "-test.v")
		cmd.Env = append(os.Environ(),
			configBootProcessRoleEnv+"="+role,
			configBootProcessURLenv+"="+testClient.URL,
		)
		output, runErr := cmd.CombinedOutput()
		cancel()
		if runErr != nil {
			t.Fatalf("%s process failed: %v\n%s", role, runErr, output)
		}
	}
}

// TestConfigBootProcessRestartHelper is entered only by the parent test's
// subprocesses. Process exit is the synchronization boundary: the reader is
// not launched until the writer's acknowledged config write and clean teardown
// have completed.
func TestConfigBootProcessRestartHelper(t *testing.T) {
	role := os.Getenv(configBootProcessRoleEnv)
	if role == "" {
		t.Skip("subprocess helper")
	}
	url := os.Getenv(configBootProcessURLenv)
	if url == "" {
		t.Fatal("missing subprocess NATS URL")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	if err := runConfigBootProcessRole(ctx, url, role); err != nil {
		t.Fatal(err)
	}
}

func runConfigBootProcessRole(ctx context.Context, url, role string) (result error) {
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	client, err := natsclient.NewClient(url,
		natsclient.WithTimeout(5*time.Second),
		natsclient.WithMaxReconnects(0),
		natsclient.WithHealthInterval(0),
	)
	if err != nil {
		return fmt.Errorf("create NATS client: %w", err)
	}
	if err := client.Connect(ctx); err != nil {
		return fmt.Errorf("connect NATS client: %w", err)
	}
	defer func() {
		closeCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		result = errors.Join(result, client.Close(closeCtx))
	}()

	configManager, err := config.NewConfigManager(configBootInitialConfig(), client, logger)
	if err != nil {
		return fmt.Errorf("construct Config Manager: %w", err)
	}
	if err := configManager.Start(ctx); err != nil {
		return fmt.Errorf("start Config Manager: %w", err)
	}
	defer func() {
		result = errors.Join(result, configManager.Stop(5*time.Second))
	}()

	registry := component.NewRegistry()
	if err := registry.RegisterWithConfig(component.RegistrationConfig{
		Name: configBootProcessFactory,
		Type: string(types.ComponentTypeProcessor),
		Factory: func(raw json.RawMessage, _ component.Dependencies) (component.Discoverable, error) {
			var cfg struct {
				Marker string `json:"marker"`
			}
			if err := json.Unmarshal(raw, &cfg); err != nil {
				return nil, fmt.Errorf("decode proof component config: %w", err)
			}
			if cfg.Marker == "" {
				return nil, errors.New("proof component marker is required")
			}
			return &configBootProcessComponent{marker: cfg.Marker}, nil
		},
		Ports: func(json.RawMessage, string) (component.PortConfig, error) {
			probe := &configBootProcessComponent{}
			return component.PortConfigFrom(probe.InputPorts(), probe.OutputPorts()), nil
		},
	}); err != nil {
		return fmt.Errorf("register proof component: %w", err)
	}

	serviceValue, err := NewComponentManager(json.RawMessage(`{}`), &Dependencies{
		NATSClient:        client,
		Manager:           configManager,
		Logger:            logger,
		ComponentRegistry: registry,
		Platform: types.PlatformMeta{
			Org: "test", Platform: "config-boot-process-proof",
		},
	})
	if err != nil {
		return fmt.Errorf("construct ComponentManager: %w", err)
	}
	componentManager := serviceValue.(*ComponentManager)
	if err := componentManager.Start(ctx); err != nil {
		return fmt.Errorf("start ComponentManager: %w", err)
	}
	defer func() {
		stopCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		result = errors.Join(result, componentManager.Stop(stopCtx))
	}()

	switch role {
	case configBootProcessRoleWriter:
		if err := assertConfigBootComposition(componentManager, configBootInitialName); err != nil {
			return fmt.Errorf("writer initial composition: %w", err)
		}
		if err := configManager.PutComponentToKV(ctx, configBootDesiredName,
			configBootComponentConfig("desired")); err != nil {
			return fmt.Errorf("persist desired component: %w", err)
		}
		if _, ok := configManager.GetConfig().Get().Components[configBootDesiredName]; !ok {
			return errors.New("Config Manager did not expose acknowledged desired component write")
		}
		if err := assertConfigBootComposition(componentManager, configBootInitialName); err != nil {
			return fmt.Errorf("writer composition after desired write: %w", err)
		}
	case configBootProcessRoleReader:
		if _, ok := configManager.GetConfig().Get().Components[configBootDesiredName]; !ok {
			return errors.New("fresh Config Manager did not load persisted desired component")
		}
		if err := assertConfigBootComposition(
			componentManager, configBootInitialName, configBootDesiredName,
		); err != nil {
			return fmt.Errorf("reader reboot composition: %w", err)
		}
	default:
		return fmt.Errorf("unknown subprocess role %q", role)
	}
	return nil
}

func configBootInitialConfig() *config.Config {
	return &config.Config{
		Version: "1.0.0",
		Platform: config.PlatformConfig{
			Org: "test", ID: "config-boot-process-proof", InstanceID: "proof-slot", Environment: "test",
		},
		Components: config.ComponentConfigs{
			configBootInitialName: configBootComponentConfig("initial"),
		},
	}
}

func configBootComponentConfig(marker string) types.ComponentConfig {
	raw, err := json.Marshal(struct {
		Marker string `json:"marker"`
	}{Marker: marker})
	if err != nil {
		panic(err)
	}
	return types.ComponentConfig{
		Type: types.ComponentTypeProcessor, Name: configBootProcessFactory, Enabled: true, Config: raw,
	}
}

func assertConfigBootComposition(manager *ComponentManager, expected ...string) error {
	status := manager.GetComponentStatus()
	actual := make([]string, 0, len(status))
	for name, componentStatus := range status {
		actual = append(actual, name)
		if componentStatus.State != component.StateStarted {
			return fmt.Errorf("component %q state = %s, want started", name, componentStatus.State)
		}
	}
	sort.Strings(actual)
	sort.Strings(expected)
	if strings.Join(actual, "\x00") != strings.Join(expected, "\x00") {
		return fmt.Errorf("running composition = %v, want %v", actual, expected)
	}
	return nil
}

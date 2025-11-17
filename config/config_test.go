package config

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/c360/semstreams/types"
)

// Helper function to extract enabled flag from service config
func getServiceEnabled(serviceConfig types.ServiceConfig) bool {
	return serviceConfig.Enabled
}

// Test basic config structure
func TestConfig_Structure(t *testing.T) {
	cfg := &Config{
		Platform: PlatformConfig{
			Org:          "c360",
			ID:           "test-platform",
			Type:         "vessel",
			Region:       "gulf_mexico",
			Capabilities: []string{"radar", "ctd"},
		},
		NATS: NATSConfig{
			URLs:          []string{"nats://localhost:4222"},
			MaxReconnects: -1,
			ReconnectWait: 2 * time.Second,
		},
	}

	assert.Equal(t, "test-platform", cfg.Platform.ID)
	assert.Equal(t, "vessel", cfg.Platform.Type)
	assert.Contains(t, cfg.Platform.Capabilities, "radar")
}

// Test loading config from JSON file
func TestLoader_LoadJSON(t *testing.T) {
	// Create test config file
	testConfig := `{
		"platform": {
			"org": "c360",
			"id": "rv_walton_smith",
			"type": "vessel",
			"region": "gulf_mexico",
			"capabilities": ["radar", "ctd", "deployment"]
		},
		"nats": {
			"urls": ["nats://localhost:4222", "nats://localhost:4223"],
			"max_reconnects": 10,
			"reconnect_wait": "5s"
		},
		"services": {
			"message_logger": {"enabled": true},
			"discovery": {"enabled": true}
		}
	}`

	tmpDir := t.TempDir()
	configFile := filepath.Join(tmpDir, "config.json")
	err := os.WriteFile(configFile, []byte(testConfig), 0644)
	require.NoError(t, err)

	// Load config
	loader := NewLoader()
	cfg, err := loader.LoadFile(configFile)
	require.NoError(t, err)
	require.NotNil(t, cfg)

	// Verify loaded values
	assert.Equal(t, "rv_walton_smith", cfg.Platform.ID)
	assert.Equal(t, "vessel", cfg.Platform.Type)
	assert.Equal(t, "gulf_mexico", cfg.Platform.Region)
	assert.Len(t, cfg.Platform.Capabilities, 3)
	assert.Len(t, cfg.NATS.URLs, 2)
	assert.Equal(t, 10, cfg.NATS.MaxReconnects)
	assert.Equal(t, 5*time.Second, cfg.NATS.ReconnectWait)
	// Parse service config from types.ServiceConfigs
	msgLoggerEnabled := getServiceEnabled(cfg.Services["message-logger"])
	discoveryEnabled := getServiceEnabled(cfg.Services["discovery"])
	assert.True(t, msgLoggerEnabled)
	assert.True(t, discoveryEnabled)
}

// Test default values
func TestLoader_Defaults(t *testing.T) {
	// Minimal config with missing fields
	testConfig := `{
		"platform": {
			"org": "c360",
			"id": "test-platform",
			"type": "shore"
		}
	}`

	tmpDir := t.TempDir()
	configFile := filepath.Join(tmpDir, "config.json")
	err := os.WriteFile(configFile, []byte(testConfig), 0644)
	require.NoError(t, err)

	loader := NewLoader()
	cfg, err := loader.LoadFile(configFile)
	require.NoError(t, err)

	// Check defaults were applied
	assert.Equal(t, "gulf_mexico", cfg.Platform.Region)               // default region
	assert.Equal(t, []string{"nats://localhost:4222"}, cfg.NATS.URLs) // default URL
	assert.Equal(t, -1, cfg.NATS.MaxReconnects)                       // default infinite reconnects
	assert.Equal(t, 2*time.Second, cfg.NATS.ReconnectWait)            // default wait
	// Parse service config from types.ServiceConfigs
	msgLoggerEnabled := getServiceEnabled(cfg.Services["message-logger"])
	discoveryEnabled := getServiceEnabled(cfg.Services["discovery"])
	assert.True(t, msgLoggerEnabled)           // default enabled
	assert.False(t, discoveryEnabled)          // dormant by default
	assert.True(t, cfg.NATS.JetStream.Enabled) // default enabled
	// ObjectStore moved to components
	// assert.True(t, cfg.ObjectStore.Enabled) // default enabled
	// assert.Equal(t, "messages", cfg.ObjectStore.BucketName) // default bucket
}

// Test environment variable overrides
func TestLoader_EnvOverrides(t *testing.T) {
	// Set environment variables
	_ = os.Setenv("STREAMKIT_PLATFORM_ID", "env-platform")
	_ = os.Setenv("STREAMKIT_NATS_USERNAME", "testuser")
	_ = os.Setenv("STREAMKIT_NATS_PASSWORD", "testpass")
	defer func() {
		_ = os.Unsetenv("STREAMKIT_PLATFORM_ID")
		_ = os.Unsetenv("STREAMKIT_NATS_USERNAME")
		_ = os.Unsetenv("STREAMKIT_NATS_PASSWORD")
	}()

	// Base config
	testConfig := `{
		"platform": {
			"org": "c360",
			"id": "json-platform",
			"type": "vessel"
		}
	}`

	tmpDir := t.TempDir()
	configFile := filepath.Join(tmpDir, "config.json")
	err := os.WriteFile(configFile, []byte(testConfig), 0644)
	require.NoError(t, err)

	loader := NewLoader()
	cfg, err := loader.LoadFile(configFile)
	require.NoError(t, err)

	// Env vars should override JSON
	assert.Equal(t, "env-platform", cfg.Platform.ID)
	assert.Equal(t, "testuser", cfg.NATS.Username)
	assert.Equal(t, "testpass", cfg.NATS.Password)

	// JSON value should remain when no env override
	assert.Equal(t, "vessel", cfg.Platform.Type)
}

// Test validation
func TestLoader_Validation(t *testing.T) {
	tests := []struct {
		name      string
		config    string
		wantError string
	}{
		{
			name: "missing org",
			config: `{
				"platform": {
					"id": "platform1",
					"type": "vessel"
				},
				"object_store": {
					"bucket_name": "test"
				}
			}`,
			wantError: "platform.org is required",
		},
		{
			name: "missing platform ID",
			config: `{
				"platform": {
					"org": "c360",
					"type": "vessel"
				},
				"object_store": {
					"bucket_name": "test"
				}
			}`,
			wantError: "platform.id is required",
		},
		{
			name: "invalid component config - empty component name",
			config: `{
				"platform": {
					"org": "c360",
					"id": "test",
					"type": "vessel"
				},
				"components": {
					"test-component": {
						"type": "input",
						"name": "",
						"enabled": true
					}
				},
				"object_store": {
					"bucket_name": "test"
				}
			}`,
			wantError: "component factory name cannot be empty",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tmpDir := t.TempDir()
			configFile := filepath.Join(tmpDir, "config.json")
			err := os.WriteFile(configFile, []byte(tt.config), 0644)
			require.NoError(t, err)

			loader := NewLoader()
			loader.EnableValidation(true)

			_, err = loader.LoadFile(configFile)
			assert.Error(t, err)
			assert.Contains(t, err.Error(), tt.wantError)
		})
	}
}

// Test components configuration
// TODO: Update this test for new ComponentConfigs structure
// The old structure with Components.Enabled is obsolete
/*
func TestConfig_Components(t *testing.T) {
	// OLD FORMAT - NO LONGER SUPPORTED
	// components.enabled array has been replaced with
	// components as a map[string]types.ComponentConfig
}
*/

// Test merging configurations
func TestLoader_MergeConfigs(t *testing.T) {
	loader := NewLoader()

	base := &Config{
		Platform: PlatformConfig{
			Type:   "generic",
			Region: "gulf_mexico",
		},
		NATS: NATSConfig{
			URLs:          []string{"nats://localhost:4222"},
			MaxReconnects: -1,
		},
		Services: types.ServiceConfigs{
			"message_logger": types.ServiceConfig{
				Name:    "message_logger",
				Enabled: true,
				Config:  json.RawMessage(`{}`),
			},
		},
	}

	override := &Config{
		Platform: PlatformConfig{
			ID:           "test-platform",
			Type:         "vessel",
			Capabilities: []string{"radar"},
		},
		NATS: NATSConfig{
			MaxReconnects: 5,
			Username:      "testuser",
		},
		Services: types.ServiceConfigs{
			"discovery": types.ServiceConfig{
				Name:    "discovery",
				Enabled: true,
				Config:  json.RawMessage(`{}`),
			},
		},
	}

	merged := loader.mergeConfigs(base, override)

	// Check merged values
	assert.Equal(t, "test-platform", merged.Platform.ID)             // from override
	assert.Equal(t, "vessel", merged.Platform.Type)                  // from override
	assert.Equal(t, "gulf_mexico", merged.Platform.Region)           // from base
	assert.Equal(t, []string{"radar"}, merged.Platform.Capabilities) // from override

	assert.Equal(t, []string{"nats://localhost:4222"}, merged.NATS.URLs) // from base
	assert.Equal(t, 5, merged.NATS.MaxReconnects)                        // from override
	assert.Equal(t, "testuser", merged.NATS.Username)                    // from override

	// Parse service config from types.ServiceConfigs
	msgLoggerEnabled := getServiceEnabled(merged.Services["message_logger"])
	discoveryEnabled := getServiceEnabled(merged.Services["discovery"])
	assert.True(t, msgLoggerEnabled) // from base
	assert.True(t, discoveryEnabled) // from override
}

// Test saving configuration back to file
func TestConfig_Save(t *testing.T) {
	cfg := &Config{
		Platform: PlatformConfig{
			ID:           "save-test",
			Type:         "vessel",
			Region:       "atlantic",
			Capabilities: []string{"radar", "sonar"},
		},
		NATS: NATSConfig{
			URLs:          []string{"nats://server1:4222", "nats://server2:4222"},
			MaxReconnects: 10,
		},
		Services: types.ServiceConfigs{
			"message-logger": types.ServiceConfig{
				Name:    "message-logger",
				Enabled: true,
				Config:  json.RawMessage(`{}`),
			},
			"discovery": types.ServiceConfig{
				Name:    "discovery",
				Enabled: true,
				Config:  json.RawMessage(`{}`),
			},
		},
	}

	tmpDir := t.TempDir()
	saveFile := filepath.Join(tmpDir, "saved.json")

	err := cfg.SaveToFile(saveFile)
	require.NoError(t, err)

	// Load it back
	loader := NewLoader()
	loaded, err := loader.LoadFile(saveFile)
	require.NoError(t, err)

	assert.Equal(t, cfg.Platform.ID, loaded.Platform.ID)
	assert.Equal(t, cfg.Platform.Type, loaded.Platform.Type)
	assert.Equal(t, cfg.Platform.Region, loaded.Platform.Region)
	assert.Equal(t, cfg.Platform.Capabilities, loaded.Platform.Capabilities)
	assert.Equal(t, cfg.NATS.URLs, loaded.NATS.URLs)
	assert.Equal(t, cfg.NATS.MaxReconnects, loaded.NATS.MaxReconnects)
	// Parse service configs from types.ServiceConfigs
	cfgMsgLoggerEnabled := getServiceEnabled(cfg.Services["message-logger"])
	cfgDiscoveryEnabled := getServiceEnabled(cfg.Services["discovery"])
	loadedMsgLoggerEnabled := getServiceEnabled(loaded.Services["message-logger"])
	loadedDiscoveryEnabled := getServiceEnabled(loaded.Services["discovery"])
	assert.Equal(t, cfgMsgLoggerEnabled, loadedMsgLoggerEnabled)
	assert.Equal(t, cfgDiscoveryEnabled, loadedDiscoveryEnabled)
}

// Test loading the example config
func TestLoader_ExampleConfig(t *testing.T) {
	// Load the example config from the current directory
	loader := NewLoader()
	cfg, err := loader.LoadFile("example_config.json")
	require.NoError(t, err)

	// Verify it matches our expected MAVLink demo structure
	assert.Equal(t, "mavlink-demo", cfg.Platform.ID)
	assert.Equal(t, "ground-station", cfg.Platform.Type)
	// Parse service config from types.ServiceConfigs
	msgLoggerEnabled := getServiceEnabled(cfg.Services["message-logger"])
	discoveryEnabled := getServiceEnabled(cfg.Services["discovery"])
	assert.True(t, msgLoggerEnabled)
	assert.True(t, discoveryEnabled)

	// Check components are properly configured
	assert.Equal(t, 5, len(cfg.Components), "should have 5 components configured")

	// Verify udp-input component
	udpInput, exists := cfg.Components["udp-input"]
	assert.True(t, exists, "should have udp-input component")
	assert.Equal(t, types.ComponentType("input"), udpInput.Type)
	assert.Equal(t, "udp", udpInput.Name)
	assert.True(t, udpInput.Enabled)

	// Verify mavlink-processor component
	mavlinkProc, exists := cfg.Components["mavlink-processor"]
	assert.True(t, exists, "should have mavlink-processor component")
	assert.Equal(t, types.ComponentType("processor"), mavlinkProc.Type)
	assert.Equal(t, "robotics", mavlinkProc.Name)
	assert.True(t, mavlinkProc.Enabled)

	// Verify websocket-output component
	wsOutput, exists := cfg.Components["websocket-output"]
	assert.True(t, exists, "should have websocket-output component")
	assert.Equal(t, types.ComponentType("output"), wsOutput.Type)
	assert.Equal(t, "websocket", wsOutput.Name)
	assert.True(t, wsOutput.Enabled)
}

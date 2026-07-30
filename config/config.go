package config

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"
	"unicode"

	"github.com/c360studio/semstreams/model"
	"github.com/c360studio/semstreams/pkg/platform"
	"github.com/c360studio/semstreams/pkg/security"
	"github.com/c360studio/semstreams/types"
)

// PlatformConfig is an alias for platform.Config.
//
// PlatformConfig was promoted to its own leaf package so message/ (and
// any future consumer of platform identity) can reference it without
// importing all of config. Keeping the alias preserves config.PlatformConfig
// usage at every call site that already had it. New code should prefer
// platform.Config directly. Removing the alias would be a
// mechanical rename across ~10 files; defer until there's a reason.
type PlatformConfig = platform.Config

// Storage mode constants
const (
	StorageModeMemory = "memory" // In-memory only (original implementation)
	StorageModeKV     = "kv"     // NATS KV only (no local cache)
	StorageModeHybrid = "hybrid" // KV + local cache (recommended for production)
)

// ComponentConfigs holds component instance configurations.
// The map key is the instance name (e.g., "udp-sensor-main").
// Components are only created if both:
// 1. Their factory has been registered via init()
// 2. They have an entry in this config map with enabled=true
type ComponentConfigs map[string]types.ComponentConfig

// Config represents the complete application configuration
// Simplified to 6 fields: Version (semver), Platform (identity), Security (TLS), NATS (connection), Services, Components
type Config struct {
	Version       string               `json:"version"` // Semantic version (e.g., "1.0.0") for KV sync control
	Platform      PlatformConfig       `json:"platform"`
	Security      security.Config      `json:"security,omitempty"` // Platform-wide security configuration
	NATS          NATSConfig           `json:"nats"`
	Services      types.ServiceConfigs `json:"services"`                 // Map of service configs
	Components    ComponentConfigs     `json:"components"`               // Map of component instance configs
	Streams       StreamConfigs        `json:"streams,omitempty"`        // Explicit JetStream stream definitions
	ModelRegistry *model.Registry      `json:"model_registry,omitempty"` // Unified model endpoint registry

	// StreamMigrationOverrides admits named ordinary streams that predate the
	// bounds contract, each for a declared period. Readiness reports every
	// active override and FAILS once one expires; an override with no expiry is
	// rejected at validation, so a bridge cannot become permanent.
	StreamMigrationOverrides StreamMigrationOverrides `json:"stream_migration_overrides,omitempty"`

	// ArchivalStreams declares ordinary streams whose contract is permanence.
	// Structurally separate from StreamMigrationOverrides: an archive that could
	// only be expressed as a renewed override would train operators to renew
	// without reading, which is what makes genuinely time-limited overrides
	// invisible.
	ArchivalStreams ArchivalStreams `json:"archival_streams,omitempty"`
}

// SafeConfig provides thread-safe access to configuration
type SafeConfig struct {
	mu     sync.RWMutex
	config *Config
}

// NewSafeConfig creates a new thread-safe config wrapper
func NewSafeConfig(cfg *Config) *SafeConfig {
	if cfg == nil {
		cfg = &Config{}
	}
	return &SafeConfig{
		config: cfg,
	}
}

// Get returns a deep copy of the current configuration
func (sc *SafeConfig) Get() *Config {
	sc.mu.RLock()
	defer sc.mu.RUnlock()
	return sc.config.Clone()
}

// Update atomically updates the configuration after validation
func (sc *SafeConfig) Update(cfg *Config) error {
	if cfg == nil {
		return fmt.Errorf("config cannot be nil")
	}

	// Validate before updating
	if err := cfg.Validate(); err != nil {
		return fmt.Errorf("config validation failed: %w", err)
	}

	sc.mu.Lock()
	defer sc.mu.Unlock()
	sc.config = cfg
	return nil
}

// Mutate atomically applies a read-modify-write to the configuration. It holds
// the write lock across the WHOLE clone → mutate → validate → swap sequence, so
// two concurrent mutations cannot lose one another's change (gh#515). Without
// this, a caller doing Get() (a clone) → mutate → Update() races another doing
// the same: each starts from the same base and the second swap clobbers the
// first. -race does not flag it — each Get/Update is individually locked; the
// atomicity violation is at the compound level.
//
// fn receives a private deep copy it may mutate freely; returning a non-nil error
// aborts the mutation with no change. The result is validated before the swap.
//
// Re-entrancy contract: fn MUST NOT call any SafeConfig method (Get/Update/Mutate)
// — it operates only on the draft it is handed, or it self-deadlocks on sc.mu. Do
// post-mutation side effects (KV writes, subscriber notification) AFTER Mutate
// returns; capture any value they need into a variable inside fn.
//
// Unlike Update, validation runs INSIDE the write lock (it must, to keep the whole
// RMW atomic). Validate does filesystem I/O (os.Stat) when TLS is configured, so a
// mutation briefly blocks concurrent Get() readers — acceptable because config
// mutations are rare and off the hot path.
func (sc *SafeConfig) Mutate(fn func(*Config) error) error {
	sc.mu.Lock()
	defer sc.mu.Unlock()

	draft := sc.config.Clone()
	if err := fn(draft); err != nil {
		return err
	}
	if err := draft.Validate(); err != nil {
		return fmt.Errorf("config validation failed: %w", err)
	}
	sc.config = draft
	return nil
}

// Clone creates a deep copy of the configuration
func (c *Config) Clone() *Config {
	if c == nil {
		return &Config{}
	}

	// Use JSON marshaling/unmarshaling for deep copy
	data, err := json.Marshal(c)
	if err != nil {
		// Fallback to shallow copy if marshaling fails
		copied := *c
		return &copied
	}

	var clone Config
	if err := json.Unmarshal(data, &clone); err != nil {
		// Fallback to shallow copy if unmarshaling fails
		copied := *c
		return &copied
	}

	return &clone
}

// NATSConfig defines NATS connection settings
type NATSConfig struct {
	URLs          []string        `json:"urls,omitempty"`
	MaxReconnects int             `json:"max_reconnects,omitempty"`
	ReconnectWait time.Duration   `json:"reconnect_wait,omitempty"`
	Username      string          `json:"username,omitempty"`
	Password      string          `json:"password,omitempty"`
	Token         string          `json:"token,omitempty"`
	TLS           NATSTLSConfig   `json:"tls,omitempty"`
	JetStream     JetStreamConfig `json:"jetstream,omitempty"`
}

// NATSTLSConfig for secure NATS connections
type NATSTLSConfig struct {
	Enabled  bool   `json:"enabled"`
	CertFile string `json:"cert_file,omitempty"`
	KeyFile  string `json:"key_file,omitempty"`
	CAFile   string `json:"ca_file,omitempty"`
}

// JetStreamConfig for JetStream settings. Per #101, fields below are
// either verification hints (MaxMemory/MaxFileStore) or reserved for
// future plumbing (Domain/RetentionPolicy/ReplicationFactor); JetStream
// account limits are exclusively server-side configuration via
// nats.conf — the nats.go SDK exposes AccountInfo as read-only and
// offers no client-side mutation surface, so the framework cannot push
// operator intent. What it CAN do is compare configured-vs-server at
// boot and Warn on gap (see StreamsManager.VerifyJetStreamLimits) so an
// operator who set max_file_store: 10GB in the framework config but
// didn't update nats.conf isn't left wondering why stream-create
// eventually fails with "insufficient storage resources".
type JetStreamConfig struct {
	Enabled bool   `json:"enabled"`
	Domain  string `json:"domain,omitempty"`
	// MaxMemory is the operator's expected server-side
	// max_memory_store limit. VerifyJetStreamLimits Warns at boot if
	// this exceeds the server's actual AccountInfo.Limits.MaxMemory.
	// 0 disables the check (the default for operators who manage
	// JetStream sizing entirely via nats.conf without surfacing intent
	// in the framework config).
	MaxMemory int64 `json:"max_memory,omitempty"`
	// MaxFileStore is the operator's expected server-side
	// max_file_store limit. Same Warn-on-gap semantics as MaxMemory.
	MaxFileStore int64 `json:"max_file_store,omitempty"`
	// RetentionPolicy and ReplicationFactor are reserved for future
	// per-stream-default plumbing — they're per-stream concepts in
	// JetStream, not account-level, so the framework would have to
	// route them through StreamConfig defaults rather than at this
	// level. Not currently honored anywhere.
	RetentionPolicy   string `json:"retention_policy,omitempty"`
	ReplicationFactor int    `json:"replication_factor,omitempty"`
}

// Validate checks if the config is valid
func (c *Config) Validate() error {
	// Validate and normalize org
	if c.Platform.Org == "" {
		return errors.New("platform.org is required")
	}

	// Normalize org to lowercase
	c.Platform.Org = strings.ToLower(c.Platform.Org)

	// Validate org is NATS-subject compatible
	if !isValidNATSSubjectPart(c.Platform.Org) {
		return fmt.Errorf(
			"platform.org '%s' is not valid for NATS subjects (must be alphanumeric with dots, dashes, underscore s)",
			c.Platform.Org,
		)
	}

	if c.Platform.ID == "" {
		return errors.New("platform.id is required")
	}

	// Validate Security Configuration
	if err := c.validateSecurity(); err != nil {
		return fmt.Errorf("security configuration: %w", err)
	}

	// Validate Components
	for instanceName, config := range c.Components {
		if instanceName == "" {
			return errors.New("component instance name cannot be empty")
		}
		if err := config.Validate(); err != nil {
			return fmt.Errorf("component %s: %w", instanceName, err)
		}
	}

	// Validate stream declarations: the backing-stream prohibition, and the
	// bounds contract every ordinary stream owes. This runs here, and not only
	// in the provisioner, because `semstreams --validate` prints
	// "✓ Configuration is valid" and exits BEFORE streams are ensured
	// (cmd/semstreams/main.go) — so without it a config that hard-fails boot
	// would be greenlit by the very tool an operator uses to avoid that.
	// Resolution is pure and I/O-free precisely so it can run here, which is
	// also what lets the port-derived lane be checked at validation rather than
	// only at boot.
	if _, err := ValidateStreamDeclarations(c); err != nil {
		return fmt.Errorf("streams: %w", err)
	}

	// Validate model registry if present
	if c.ModelRegistry != nil {
		if err := c.ModelRegistry.Validate(); err != nil {
			return fmt.Errorf("model_registry: %w", err)
		}
	}

	return nil
}

// isValidNATSSubjectPart checks if a string is valid for use in NATS subjects.
// Valid characters are alphanumeric, dots, dashes, and underscore s.
func isValidNATSSubjectPart(s string) bool {
	if len(s) == 0 {
		return false
	}

	for _, r := range s {
		if !unicode.IsLetter(r) && !unicode.IsDigit(r) &&
			r != '-' && r != '_' && r != '.' {
			return false
		}
	}
	return true
}

// validateSecurity validates the security configuration
func (c *Config) validateSecurity() error {
	// Validate Server TLS
	if c.Security.TLS.Server.Enabled {
		if c.Security.TLS.Server.CertFile == "" {
			return errors.New("tls.server.cert_file is required when TLS is enabled")
		}
		if c.Security.TLS.Server.KeyFile == "" {
			return errors.New("tls.server.key_file is required when TLS is enabled")
		}

		// Check if cert file exists
		if _, err := os.Stat(c.Security.TLS.Server.CertFile); err != nil {
			return fmt.Errorf("tls.server.cert_file: %w", err)
		}

		// Check if key file exists
		if _, err := os.Stat(c.Security.TLS.Server.KeyFile); err != nil {
			return fmt.Errorf("tls.server.key_file: %w", err)
		}

		// Validate MinVersion if specified
		if c.Security.TLS.Server.MinVersion != "" {
			if err := validateTLSVersion(c.Security.TLS.Server.MinVersion); err != nil {
				return fmt.Errorf("tls.server.min_version: %w", err)
			}
		}
	}

	// Validate Client TLS
	// Check all CA files exist
	for i, caFile := range c.Security.TLS.Client.CAFiles {
		if _, err := os.Stat(caFile); err != nil {
			return fmt.Errorf("tls.client.ca_files[%d]: %w", i, err)
		}
	}

	// Warn if InsecureSkipVerify is enabled
	if c.Security.TLS.Client.InsecureSkipVerify {
		_, _ = fmt.Fprintf(
			os.Stderr,
			"WARNING: TLS certificate verification is disabled (insecure_skip_verify=true). This should only be used in development/testing!\n",
		)
	}

	// Validate MinVersion if specified
	if c.Security.TLS.Client.MinVersion != "" {
		if err := validateTLSVersion(c.Security.TLS.Client.MinVersion); err != nil {
			return fmt.Errorf("tls.client.min_version: %w", err)
		}
	}

	return nil
}

// validateTLSVersion checks if a TLS version string is valid
func validateTLSVersion(version string) error {
	switch version {
	case "1.2", "1.3":
		return nil
	default:
		return fmt.Errorf("invalid TLS version %q (must be \"1.2\" or \"1.3\")", version)
	}
}

// BucketConfig defines configuration for a single KV bucket
type BucketConfig struct {
	Name     string        `json:"name,omitempty"`      // Override default name if needed
	TTL      time.Duration `json:"ttl"`                 // 0 = no expiration
	History  int           `json:"history"`             // Number of versions to keep
	MaxBytes int64         `json:"max_bytes,omitempty"` // Size limit (0 = unlimited)
	Replicas int           `json:"replicas,omitempty"`  // Replication factor
}

// Loader handles configuration loading with layers and overrides
type Loader struct {
	layers     []string
	validation bool
	envPrefix  string
}

// NewLoader creates a new configuration loader
func NewLoader() *Loader {
	return &Loader{
		layers:     []string{},
		validation: false,
		envPrefix:  "STREAMKIT",
	}
}

// AddLayer adds a configuration file layer
func (l *Loader) AddLayer(path string) {
	l.layers = append(l.layers, path)
}

// EnableValidation enables or disables configuration validation
func (l *Loader) EnableValidation(enable bool) {
	l.validation = enable
}

// LoadFile loads configuration from a single file
func (l *Loader) LoadFile(path string) (*Config, error) {
	l.layers = []string{path}
	return l.Load()
}

// Load loads and merges all configuration layers
func (l *Loader) Load() (*Config, error) {
	// Start with defaults
	cfg := l.getDefaults()

	// Load each layer and merge using map-based approach
	for _, path := range l.layers {
		rawConfig, err := l.loadRawJSON(path)
		if err != nil {
			return nil, fmt.Errorf("failed to load %s: %w", path, err)
		}
		cfg = l.mergeFromMap(cfg, rawConfig)
	}

	// Apply environment overrides
	l.applyEnvOverrides(cfg)

	// Validate if enabled
	if l.validation {
		if err := l.validate(cfg); err != nil {
			return nil, err
		}
	}

	return cfg, nil
}

// LoadFromBytes loads configuration from JSON bytes.
// This is useful when you need to pre-process the configuration (e.g., environment
// variable expansion) before loading.
//
// The data is validated and merged with defaults, just like LoadFile.
func (l *Loader) LoadFromBytes(data []byte) (*Config, error) {
	// Start with defaults
	cfg := l.getDefaults()

	// Parse and merge the provided JSON data
	rawConfig, err := l.loadRawJSONFromBytes(data)
	if err != nil {
		return nil, fmt.Errorf("failed to parse config: %w", err)
	}
	cfg = l.mergeFromMap(cfg, rawConfig)

	// Apply environment overrides
	l.applyEnvOverrides(cfg)

	// Validate if enabled
	if l.validation {
		if err := l.validate(cfg); err != nil {
			return nil, err
		}
	}

	return cfg, nil
}

// loadRawJSONFromBytes parses JSON bytes into a configuration map.
// This follows the same validation as loadRawJSON but without file I/O.
func (l *Loader) loadRawJSONFromBytes(data []byte) (map[string]any, error) {
	// Validate size to prevent DoS
	if len(data) > maxConfigSize {
		return nil, fmt.Errorf("config data too large: %d bytes > %d", len(data), maxConfigSize)
	}

	// Validate JSON depth to prevent DoS
	if err := validateJSONDepth(data); err != nil {
		return nil, fmt.Errorf("invalid JSON structure: %w", err)
	}

	// Unmarshal into map
	var rawConfig map[string]any
	if err := json.Unmarshal(data, &rawConfig); err != nil {
		return nil, err
	}

	// Convert duration strings
	l.parseDurations(rawConfig)

	return rawConfig, nil
}

// getDefaults returns default configuration
func (l *Loader) getDefaults() *Config {
	return &Config{
		Platform: PlatformConfig{
			Region: "gulf_mexico",
		},
		NATS: NATSConfig{
			URLs:          []string{"nats://localhost:4222"},
			MaxReconnects: -1,
			ReconnectWait: 2 * time.Second,
			JetStream: JetStreamConfig{
				Enabled: true,
			},
		},
		Services: types.ServiceConfigs{
			"message-logger": types.ServiceConfig{
				Name:    "message-logger",
				Enabled: true,
				Config:  json.RawMessage(`{"monitor_subjects": ["*"], "sample_rate": 1}`),
			},
			"discovery": types.ServiceConfig{
				Name:    "discovery",
				Enabled: false, // Dormant by default
				Config:  json.RawMessage(`{}`),
			},
			"heartbeat": types.ServiceConfig{
				Name:    "heartbeat",
				Enabled: true,
				Config:  json.RawMessage(`{"interval": "30s"}`),
			},
		},
		// Graph and ObjectStore configuration moved to components
	}
}

// loadRawJSON loads configuration from a JSON file as a map
func (l *Loader) loadRawJSON(path string) (map[string]any, error) {
	// Use secure file reading with validation
	data, err := safeReadFile(path)
	if err != nil {
		return nil, err
	}

	// Validate JSON depth to prevent DoS
	if err := validateJSONDepth(data); err != nil {
		return nil, fmt.Errorf("invalid JSON structure: %w", err)
	}

	// Unmarshal into map
	var rawConfig map[string]any
	if err := json.Unmarshal(data, &rawConfig); err != nil {
		return nil, err
	}

	// Convert duration strings
	l.parseDurations(rawConfig)

	return rawConfig, nil
}

// mergeFromMap merges configuration from a raw map, only overriding fields present in the map
func (l *Loader) mergeFromMap(base *Config, override map[string]any) *Config {
	if override == nil {
		return base
	}

	// Marshal the base config to JSON then to map
	baseJSON, err := json.Marshal(base)
	if err != nil {
		return base
	}

	var baseMap map[string]any
	if err := json.Unmarshal(baseJSON, &baseMap); err != nil {
		return base
	}

	// Deep merge the maps
	mergedMap := l.deepMergeMaps(baseMap, override)

	// Convert back to Config
	mergedJSON, err := json.Marshal(mergedMap)
	if err != nil {
		return base
	}

	var merged Config
	if err := json.Unmarshal(mergedJSON, &merged); err != nil {
		return base
	}

	return &merged
}

// deepMergeMaps recursively merges two maps, with override taking precedence
func (l *Loader) deepMergeMaps(base, override map[string]any) map[string]any {
	result := make(map[string]any)

	// Copy base values
	for k, v := range base {
		result[k] = v
	}

	// Override with values from override map
	for k, v := range override {
		if v == nil {
			continue
		}

		// If both base and override have maps at this key, merge them
		if baseMap, baseOk := base[k].(map[string]any); baseOk {
			if overrideMap, overrideOk := v.(map[string]any); overrideOk {
				result[k] = l.deepMergeMaps(baseMap, overrideMap)
				continue
			}
		}

		// Otherwise, override takes precedence
		result[k] = v
	}

	return result
}

// loadJSONFile loads configuration from a JSON file (kept for compatibility)
func (l *Loader) loadJSONFile(path string) (*Config, error) {
	rawConfig, err := l.loadRawJSON(path)
	if err != nil {
		return nil, err
	}

	// Marshal to JSON and unmarshal into Config
	processedData, err := json.Marshal(rawConfig)
	if err != nil {
		return nil, err
	}

	var cfg Config
	if err := json.Unmarshal(processedData, &cfg); err != nil {
		return nil, err
	}

	return &cfg, nil
}

// parseDurations converts duration strings to nanoseconds for json unmarshaling
func (l *Loader) parseDurations(data map[string]any) {
	// Handle NATS reconnect_wait
	if nats, ok := data["nats"].(map[string]any); ok {
		if wait, ok := nats["reconnect_wait"].(string); ok {
			if d, err := time.ParseDuration(wait); err == nil {
				nats["reconnect_wait"] = d.Nanoseconds()
			}
		}
	}

	// Handle Graph bucket TTLs
	if graph, ok := data["graph"].(map[string]any); ok {
		l.parseBucketDurations(graph, "entity_states")
		l.parseBucketDurations(graph, "spatial_index")
		l.parseBucketDurations(graph, "temporal_index")
		l.parseBucketDurations(graph, "incoming_index")
	}
}

// parseDurationWithDays parses durations that may include days (e.g., "14d")
func parseDurationWithDays(s string) (time.Duration, error) {
	if strings.HasSuffix(s, "d") {
		days := strings.TrimSuffix(s, "d")
		n, err := strconv.Atoi(days)
		if err != nil {
			return 0, err
		}
		return time.Duration(n) * 24 * time.Hour, nil
	}
	return time.ParseDuration(s)
}

// parseBucketDurations parses TTL duration for a bucket configuration
func (l *Loader) parseBucketDurations(graph map[string]any, bucketName string) {
	if bucket, ok := graph[bucketName].(map[string]any); ok {
		if ttl, ok := bucket["ttl"].(string); ok {
			if d, err := parseDurationWithDays(ttl); err == nil {
				bucket["ttl"] = d.Nanoseconds()
			}
		}
	}
}

// mergeConfigs merges configuration layers
// This is primarily used for testing - the main Load() uses mergeFromMap
func (l *Loader) mergeConfigs(base, override *Config) *Config {
	if override == nil {
		return base
	}

	// Convert both to maps and use the map-based merge
	baseJSON, err := json.Marshal(base)
	if err != nil {
		return base
	}
	var baseMap map[string]any
	if err := json.Unmarshal(baseJSON, &baseMap); err != nil {
		return base
	}

	overrideJSON, err := json.Marshal(override)
	if err != nil {
		return base
	}
	var overrideMap map[string]any
	if err := json.Unmarshal(overrideJSON, &overrideMap); err != nil {
		return base
	}

	// Remove nil values from override map (these are zero values in Go structs)
	l.removeNilValues(overrideMap)

	// Merge and convert back
	mergedMap := l.deepMergeMaps(baseMap, overrideMap)
	mergedJSON, err := json.Marshal(mergedMap)
	if err != nil {
		return base
	}

	var merged Config
	if err := json.Unmarshal(mergedJSON, &merged); err != nil {
		return base
	}

	return &merged
}

// removeNilValues recursively removes nil values from a map
func (l *Loader) removeNilValues(m map[string]any) {
	for k, v := range m {
		if v == nil {
			delete(m, k)
		} else if nested, ok := v.(map[string]any); ok {
			l.removeNilValues(nested)
		}
	}
}

// applyEnvOverrides applies environment variable overrides
func (l *Loader) applyEnvOverrides(cfg *Config) {
	// Platform overrides
	if val := os.Getenv(l.envPrefix + "_PLATFORM_ID"); val != "" {
		cfg.Platform.ID = val
	}
	if val := os.Getenv(l.envPrefix + "_PLATFORM_TYPE"); val != "" {
		cfg.Platform.Type = val
	}
	if val := os.Getenv(l.envPrefix + "_PLATFORM_REGION"); val != "" {
		cfg.Platform.Region = val
	}

	// NATS overrides
	if val := os.Getenv(l.envPrefix + "_NATS_URLS"); val != "" {
		cfg.NATS.URLs = strings.Split(val, ",")
	}
	if val := os.Getenv(l.envPrefix + "_NATS_USERNAME"); val != "" {
		cfg.NATS.Username = val
	}
	if val := os.Getenv(l.envPrefix + "_NATS_PASSWORD"); val != "" {
		cfg.NATS.Password = val
	}
	if val := os.Getenv(l.envPrefix + "_NATS_TOKEN"); val != "" {
		cfg.NATS.Token = val
	}
}

// validate validates the configuration
func (l *Loader) validate(cfg *Config) error {
	// Use the config's own validation method
	return cfg.Validate()
}

// SaveToFile saves the configuration to a JSON file
func (c *Config) SaveToFile(path string) error {
	data, err := json.MarshalIndent(c, "", "  ")
	if err != nil {
		return err
	}

	// Use secure file writing with validation
	return safeWriteFile(path, data)
}

// GetOrg returns the organization from platform config
func (c *Config) GetOrg() string {
	return c.Platform.Org
}

// GetPlatform returns the platform identifier (prefer instance_id over id)
func (c *Config) GetPlatform() string {
	if c.Platform.InstanceID != "" {
		return c.Platform.InstanceID
	}
	return c.Platform.ID
}

// String returns a JSON representation of the config
func (c *Config) String() string {
	data, _ := json.MarshalIndent(c, "", "  ")
	return string(data)
}

// CompareVersions compares two semver version strings
// Returns:
//
//	-1 if v1 < v2
//	 0 if v1 == v2
//	 1 if v1 > v2
//	error if either version is invalid
func CompareVersions(v1, v2 string) (int, error) {
	// Parse version 1
	major1, minor1, patch1, err := parseSemVer(v1)
	if err != nil {
		return 0, fmt.Errorf("invalid version '%s': %w", v1, err)
	}

	// Parse version 2
	major2, minor2, patch2, err := parseSemVer(v2)
	if err != nil {
		return 0, fmt.Errorf("invalid version '%s': %w", v2, err)
	}

	// Compare major
	if major1 != major2 {
		if major1 > major2 {
			return 1, nil
		}
		return -1, nil
	}

	// Compare minor
	if minor1 != minor2 {
		if minor1 > minor2 {
			return 1, nil
		}
		return -1, nil
	}

	// Compare patch
	if patch1 != patch2 {
		if patch1 > patch2 {
			return 1, nil
		}
		return -1, nil
	}

	// Equal
	return 0, nil
}

// parseSemVer parses a semantic version string (e.g., "1.2.3")
// Returns major, minor, patch, error
func parseSemVer(version string) (int, int, int, error) {
	if version == "" {
		return 0, 0, 0, errors.New("version cannot be empty")
	}

	// Remove 'v' prefix if present
	version = strings.TrimPrefix(version, "v")

	// Split into parts
	parts := strings.Split(version, ".")
	if len(parts) != 3 {
		return 0, 0, 0, fmt.Errorf("version must be in format 'major.minor.patch', got '%s'", version)
	}

	// Parse major
	major, err := strconv.Atoi(parts[0])
	if err != nil {
		return 0, 0, 0, fmt.Errorf("invalid major version '%s': %w", parts[0], err)
	}

	// Parse minor
	minor, err := strconv.Atoi(parts[1])
	if err != nil {
		return 0, 0, 0, fmt.Errorf("invalid minor version '%s': %w", parts[1], err)
	}

	// Parse patch
	patch, err := strconv.Atoi(parts[2])
	if err != nil {
		return 0, 0, 0, fmt.Errorf("invalid patch version '%s': %w", parts[2], err)
	}

	return major, minor, patch, nil
}

// UnmarshalJSON DEPRECATED - GraphConfig moved to component
// func (g *GraphConfig) UnmarshalJSON(data []byte) error {
// 	// Define a temporary struct that matches the legacy format
// 	type LegacyGraphConfig struct {
// 		EntityStates  BucketConfig `json:"entity_states"`
// 		SpatialIndex  BucketConfig `json:"spatial_index"`
// 		TemporalIndex BucketConfig `json:"temporal_index"`
// 		IncomingIndex BucketConfig `json:"incoming_index"`
// 		EntityCache   cache.Config `json:"entity_cache"`
//
// 		// Legacy fields that should be mapped to EntityCache
// 		CacheSize    int  `json:"cache_size,omitempty"`
// 		CacheEnabled bool `json:"cache_enabled,omitempty"`
// 	}
//
// 	var legacy LegacyGraphConfig
// 	if err := json.Unmarshal(data, &legacy); err != nil {
// 		return err
// 	}
//
// 	// Copy the proper fields
// 	g.EntityStates = legacy.EntityStates
// 	g.SpatialIndex = legacy.SpatialIndex
// 	g.TemporalIndex = legacy.TemporalIndex
// 	g.IncomingIndex = legacy.IncomingIndex
// 	g.EntityCache = legacy.EntityCache
//
// 	// Handle legacy cache fields if present
// 	if legacy.CacheSize > 0 && g.EntityCache.MaxSize == 0 {
// 		g.EntityCache.MaxSize = legacy.CacheSize
// 	}
//
// 	if legacy.CacheEnabled && !g.EntityCache.Enabled {
// 		g.EntityCache.Enabled = legacy.CacheEnabled
// 	}
//
// 	return nil
// }

// UnmarshalJSON implements custom JSON unmarshaling for Config
func (c *Config) UnmarshalJSON(data []byte) error {
	type Alias Config
	aux := &struct {
		NATS struct {
			URLs          []string        `json:"urls"`
			MaxReconnects int             `json:"max_reconnects"`
			ReconnectWait any             `json:"reconnect_wait"`
			Username      string          `json:"username,omitempty"`
			Password      string          `json:"password,omitempty"`
			Token         string          `json:"token,omitempty"`
			TLS           NATSTLSConfig   `json:"tls,omitempty"`
			JetStream     JetStreamConfig `json:"jetstream"`
		} `json:"nats"`
		*Alias
	}{
		Alias: (*Alias)(c),
	}

	if err := json.Unmarshal(data, &aux); err != nil {
		return err
	}

	// Handle NATS config
	c.NATS.URLs = aux.NATS.URLs
	c.NATS.MaxReconnects = aux.NATS.MaxReconnects
	c.NATS.Username = aux.NATS.Username
	c.NATS.Password = aux.NATS.Password
	c.NATS.Token = aux.NATS.Token
	c.NATS.TLS = aux.NATS.TLS
	c.NATS.JetStream = aux.NATS.JetStream

	// Parse ReconnectWait
	switch v := aux.NATS.ReconnectWait.(type) {
	case string:
		d, err := time.ParseDuration(v)
		if err != nil {
			return err
		}
		c.NATS.ReconnectWait = d
	case float64:
		c.NATS.ReconnectWait = time.Duration(v)
	}

	return nil
}

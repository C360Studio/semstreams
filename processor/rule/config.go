package rule

import (
	"encoding/json"
	"fmt"
	"time"

	"github.com/c360studio/semstreams/component"
	"github.com/c360studio/semstreams/pkg/cache"
	"github.com/c360studio/semstreams/pkg/projection"
)

// Config holds configuration for the RuleProcessor
type Config struct {
	// Port configuration for inputs and outputs
	Ports *component.PortConfig `json:"ports" schema:"type:ports,description:Port configuration for inputs (KV watch: ENTITY_STATES PREDICATE_INDEX) and outputs (NATS: control commands),category:basic"`

	// Rule configuration sources
	RulesFiles  []string     `json:"rules_files" schema:"type:array,description:Paths to JSON rule definition files,default:[],category:basic"`
	InlineRules []Definition `json:"inline_rules,omitempty" schema:"type:array,description:Inline rule definitions (alternative to files),category:basic"`

	// Message cache configuration (not exposed in schema - internal config)
	MessageCache cache.Config `json:"message_cache"`

	// Buffer window size for time-window analysis
	BufferWindowSize string `json:"buffer_window_size" schema:"type:string,description:Time window for message buffering (e.g. '10m'),default:10m,category:advanced"`

	// Alert cooldown period to prevent spam
	AlertCooldownPeriod string `json:"alert_cooldown_period" schema:"type:string,description:Minimum time between repeated alerts (e.g. '2m'),default:2m,category:advanced"`

	// Graph processor integration
	EnableGraphIntegration bool `json:"enable_graph_integration" schema:"type:bool,description:Enable graph entity creation from rules,default:true,category:basic"`

	// NATS KV patterns to watch for entity changes (e.g., 'telemetry.robotics.>')
	// DEPRECATED: Use EntityWatchBuckets for multi-bucket support. This field is still
	// supported for backwards compatibility and applies to ENTITY_STATES bucket.
	EntityWatchPatterns []string `json:"entity_watch_patterns" schema:"type:array,description:NATS KV patterns to watch for entity changes (e.g. 'telemetry.robotics.>'),category:advanced"`

	// EntityWatchBuckets maps bucket names to watch patterns.
	// This enables rules to observe operational results from multiple components.
	// Example: {"ENTITY_STATES": ["telemetry.>"], "WORKFLOW_EXECUTIONS": ["COMPLETE_*"]}
	// If not specified, falls back to EntityWatchPatterns for ENTITY_STATES bucket.
	EntityWatchBuckets map[string][]string `json:"entity_watch_buckets" schema:"type:object,description:Map of bucket names to watch patterns for multi-bucket observability,category:advanced"`

	// Debounce delay for rule evaluation (settling time for entity state)
	// Default is 0 (disabled) to ensure rules evaluate against each state change.
	// Set to a positive value (e.g., 100) to batch rapid updates and evaluate final state only.
	DebounceDelayMs time.Duration `json:"debounce_delay_ms" schema:"type:int,description:Debounce delay in milliseconds for rule evaluation (0=disabled),default:0,category:advanced"`

	// JetStream consumer configuration (not exposed in schema - internal config)
	Consumer struct {
		Enabled        bool   `json:"enabled"`          // Enable JetStream consumer
		AckWaitSeconds int    `json:"ack_wait_seconds"` // Acknowledgment timeout
		MaxDeliver     int    `json:"max_deliver"`      // Max delivery attempts
		ReplayPolicy   string `json:"replay_policy"`    // "instant" or "original"
	} `json:"consumer"`

	// PackID identifies this rule pack as a graph-projection PRODUCER
	// (ADR-056 #278 inc 2). When set, the composition root binds the pack's
	// ProjectionContracts under the ownership substrate as owner
	// "rule-pack.<PackID>". The id must be subject-safe (see Validate); it is
	// read ONCE at bind time, before the watcher starts — pack-level and
	// STATIC, never per-rule and never re-derived on hot-reload.
	PackID string `json:"pack_id,omitempty" schema:"type:string,category:advanced,description:owner = rule-pack.<pack_id>"`

	// ProjectionContracts are the graph-projection contracts this rule pack
	// owns (ADR-056 Decision 6). Bound to owner "rule-pack.<PackID>" at the
	// composition root before StartAll. Pack-level and static: the binding is
	// derived once and is NOT re-bound when rules hot-reload.
	ProjectionContracts []projection.Contract `json:"projection_contracts,omitempty" schema:"type:array,category:advanced"`
}

// MarshalJSON implements custom JSON marshaling for Config
func (c Config) MarshalJSON() ([]byte, error) {
	type Alias Config
	return json.Marshal(&struct {
		DebounceDelayMs int `json:"debounce_delay_ms"`
		*Alias
	}{
		DebounceDelayMs: int(c.DebounceDelayMs / time.Millisecond),
		Alias:           (*Alias)(&c),
	})
}

// UnmarshalJSON implements custom JSON unmarshaling for Config
func (c *Config) UnmarshalJSON(data []byte) error {
	type Alias Config
	aux := &struct {
		DebounceDelayMs int `json:"debounce_delay_ms"`
		*Alias
	}{
		Alias: (*Alias)(c),
	}
	if err := json.Unmarshal(data, &aux); err != nil {
		return err
	}
	c.DebounceDelayMs = time.Duration(aux.DebounceDelayMs) * time.Millisecond
	return nil
}

// packIDCharset is the subject-safe charset a PackID may use. It mirrors
// pkg/ownership.validOwnerID exactly (glob.go) so a config that passes this
// check can never be rejected later by RegisterOwner — the owner id is
// "rule-pack.<PackID>", and the "rule-pack." prefix plus any char in this set
// is itself subject-safe.
const packIDCharset = "[A-Za-z0-9._=-]"

// Validate checks pack-level invariants on the rule config. Today it only
// guards PackID: a non-empty PackID must be subject-safe so the derived owner
// id "rule-pack.<PackID>" is usable directly as a NATS KV key segment (no
// hashing — ownership identity is compared as the canonical string). An empty
// PackID is valid (the pack declares no projection ownership).
func (c Config) Validate() error {
	for _, r := range c.PackID {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9':
		case r == '-', r == '_', r == '=', r == '.':
		default:
			return fmt.Errorf(
				"rule config: invalid pack_id %q — owner id rule-pack.%s must use only %s (offending char %q)",
				c.PackID, c.PackID, packIDCharset, string(r))
		}
	}
	return nil
}

// DefaultConfig returns sensible defaults
func DefaultConfig() Config {
	return Config{
		Ports: &component.PortConfig{
			Inputs: []component.PortDefinition{
				{
					Name:        "entity_states",
					Type:        "kv-watch",
					Required:    true,
					Description: "Watch entity state changes from ENTITY_STATES KV bucket",
				},
				{
					Name:        "predicate_index",
					Type:        "kv-watch",
					Required:    false,
					Description: "Watch predicate index changes for pattern-based rules",
				},
			},
			Outputs: []component.PortDefinition{
				{
					Name:        "control_commands",
					Type:        "nats",
					Subject:     "control.*.commands",
					Required:    false,
					Description: "Control commands based on rules",
				},
			},
		},
		MessageCache: cache.Config{
			Enabled:         true,
			Strategy:        cache.StrategyTTL,
			MaxSize:         1000,
			TTL:             30 * time.Second,
			CleanupInterval: 15 * time.Second,
			StatsInterval:   30 * time.Second,
		},
		BufferWindowSize:       "10m",
		AlertCooldownPeriod:    "2m",
		EnableGraphIntegration: true,
		DebounceDelayMs:        0, // Disabled by default for real-time rule evaluation
		Consumer: struct {
			Enabled        bool   `json:"enabled"`
			AckWaitSeconds int    `json:"ack_wait_seconds"`
			MaxDeliver     int    `json:"max_deliver"`
			ReplayPolicy   string `json:"replay_policy"`
		}{
			Enabled:        true,
			AckWaitSeconds: 30,
			MaxDeliver:     3,
			ReplayPolicy:   "instant",
		},
	}
}

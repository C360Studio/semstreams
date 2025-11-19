package rule

import (
	"time"

	"github.com/c360/semstreams/component"
	"github.com/c360/semstreams/pkg/cache"
)

// Config holds configuration for the RuleProcessor
type Config struct {
	// Port configuration for inputs and outputs
	Ports *component.PortConfig `json:"ports" schema:"type:ports,description:Port configuration for inputs (KV watch: ENTITY_STATES PREDICATE_INDEX) and outputs (NATS: control commands),category:basic"`

	// Rule configuration sources
	RulesFiles   []string         `json:"rules_files" schema:"type:array,description:Paths to JSON rule definition files,default:[],category:basic"`
	InlineRules  []RuleDefinition `json:"inline_rules,omitempty" schema:"type:array,description:Inline rule definitions (alternative to files),category:basic"`
	EnabledRules []string         `json:"enabled_rules" schema:"type:array,description:List of rule IDs to enable for processing (deprecated: use rules_files),category:basic"`

	// Message cache configuration (not exposed in schema - internal config)
	MessageCache cache.Config `json:"message_cache"`

	// Buffer window size for time-window analysis
	BufferWindowSize string `json:"buffer_window_size" schema:"type:string,description:Time window for message buffering (e.g. '10m'),default:10m,category:advanced"`

	// Alert cooldown period to prevent spam
	AlertCooldownPeriod string `json:"alert_cooldown_period" schema:"type:string,description:Minimum time between repeated alerts (e.g. '2m'),default:2m,category:advanced"`

	// Graph processor integration
	EnableGraphIntegration bool `json:"enable_graph_integration" schema:"type:bool,description:Enable graph entity creation from rules,default:true,category:basic"`

	// NATS KV patterns to watch for entity changes (e.g., 'telemetry.robotics.>')
	EntityWatchPatterns []string `json:"entity_watch_patterns" schema:"type:array,description:NATS KV patterns to watch for entity changes (e.g. 'telemetry.robotics.>'),category:advanced"`

	// JetStream consumer configuration (not exposed in schema - internal config)
	Consumer struct {
		Enabled        bool   `json:"enabled"`          // Enable JetStream consumer
		AckWaitSeconds int    `json:"ack_wait_seconds"` // Acknowledgment timeout
		MaxDeliver     int    `json:"max_deliver"`      // Max delivery attempts
		ReplayPolicy   string `json:"replay_policy"`    // "instant" or "original"
	} `json:"consumer"`
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
		EnabledRules: []string{}, // Rules loaded from rules_files or inline_rules
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

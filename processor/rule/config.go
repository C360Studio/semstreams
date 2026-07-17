package rule

import (
	"encoding/json"
	"fmt"
	"time"

	"github.com/c360studio/semstreams/component"
	"github.com/c360studio/semstreams/pkg/cache"
	"github.com/c360studio/semstreams/pkg/projection"
	rulepackcontract "github.com/c360studio/semstreams/pkg/rulepack"
)

// Config holds configuration for the RuleProcessor
type Config struct {
	// Port configuration for inputs and outputs
	Ports *component.PortConfig `json:"ports" schema:"type:ports,description:Port configuration for ENTITY_STATES and message inputs plus action outputs,category:basic"`

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
	EnableGraphIntegration bool `json:"enable_graph_integration" schema:"type:bool,description:Enable graph entity creation from rules; every rule processor requires an explicit pack_id,default:false,category:basic"`

	// EntityWatchBuckets declares exact six-position entity ID patterns for the
	// typed EntityState evaluator. ENTITY_STATES is the only supported bucket.
	// Operational KV values need a separately designed typed decoder/evaluator.
	EntityWatchBuckets map[string][]string `json:"entity_watch_buckets" schema:"type:object,description:ENTITY_STATES patterns for the typed EntityState evaluator,category:advanced"`

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

	// PackID identifies both this rule pack's graph-projection owner and its
	// graph-event producer identity. Replicas use the same stable value so their
	// rule-trigger events converge; independently composed packs use distinct
	// values so processor-local rule IDs cannot collide. Every processor must
	// declare an explicit non-empty value; there is no default pack identity.
	PackID string `json:"pack_id" schema:"type:string,category:basic,description:Required stable rule-pack projection owner and graph-event producer identity"`

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

// packIDCharset is the exact literal-token charset a PackID may use. The
// derived owner key is always the fixed two-position key
// "rule-pack.<PackID>"; PackID cannot inject another dot position.
const (
	packIDCharset      = rulepackcontract.PackIDCharset
	packIDPattern      = "^" + packIDCharset + "+$"
	maxRulePackIDBytes = rulepackcontract.MaxPackIDBytes
)

// Validate checks pack ownership and KV-watch declarations before activation.
// PackID must be present and one literal KV token so the derived owner key
// "rule-pack.<PackID>" has fixed arity and every
// rule-trigger event has a stable producer identity. ENTITY_STATES watch
// patterns must satisfy the canonical entity ID pattern contract.
func (c Config) Validate() error {
	if err := validatePackID(c.PackID); err != nil {
		return err
	}
	if err := validateEntityWatchBuckets(c.EntityWatchBuckets); err != nil {
		return fmt.Errorf("rule config: %w", err)
	}
	for index, definition := range c.InlineRules {
		if err := ValidateDefinition(definition); err != nil {
			return fmt.Errorf("rule config: inline_rules[%d]: %w", index, err)
		}
	}
	return nil
}

func validatePackID(packID string) error {
	return rulepackcontract.ValidateID(packID)
}

// NewConfig returns the rule defaults with the caller's explicit stable pack
// identity. There is intentionally no exported identity-free default.
func NewConfig(packID string) (Config, error) {
	config := defaultConfig()
	config.PackID = packID
	if err := config.Validate(); err != nil {
		return Config{}, err
	}
	return config, nil
}

// defaultConfig provides the internal overlay base used by the component
// factory before it applies the operator's required pack_id.
func defaultConfig() Config {
	return Config{
		Ports: &component.PortConfig{
			Inputs: []component.PortDefinition{
				{
					Name:        "entity_states",
					Type:        "kv-watch",
					Required:    true,
					Description: "Watch entity state changes from ENTITY_STATES KV bucket",
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
		EnableGraphIntegration: false,
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

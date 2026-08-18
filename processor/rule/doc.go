// Package rule provides rule-based processing of semantic message streams
// with support for entity state watching and event generation.
//
// # Overview
//
// The rule processor evaluates conditions against streaming messages and
// entity state changes, generating graph events when rules trigger. Rules
// are defined using JSON configuration and implemented using the factory pattern.
//
// # Architecture
//
// Rules consist of three main components:
//   - Definition: JSON configuration (conditions, logic, metadata)
//   - Factory: Creates Rule instances from definitions
//   - Rule: Evaluates conditions and generates events
//
// # Rule Interface
//
// Rules must implement this interface:
//
//	type Rule interface {
//	    Name() string
//	    Subscribe() []string
//	    Evaluate(messages []message.Message) bool
//	    ExecuteEvents(messages []message.Message) ([]rtypes.Event, error)
//	}
//
// # Creating Custom Rules
//
// 1. Create a RuleFactory:
//
//	type MyRuleFactory struct {
//	    ruleType string
//	}
//
//	func (f *MyRuleFactory) Create(id string, def Definition, deps Dependencies) (rtypes.Rule, error) {
//	    return &MyRule{...}, nil
//	}
//
//	func (f *MyRuleFactory) Validate(def Definition) error {
//	    // Validate rule configuration
//	    return nil
//	}
//
// 2. Implement the Rule interface:
//
//	type MyRule struct {
//	    id              string
//	    triggerEntityID string // canonical six-part ID supplied by configuration
//	    conditions      []expression.ConditionExpression
//	}
//
//	func (r *MyRule) ExecuteEvents(messages []message.Message) ([]rtypes.Event, error) {
//	    event, err := gtypes.NewEntityUpdateEvent(
//	        r.triggerEntityID,
//	        map[string]interface{}{"triggered": true, "timestamp": time.Now()},
//	        gtypes.EventMetadata{
//	            RuleName: r.id,
//	            Timestamp: time.Now(),
//	            Source: "my-rule",
//	            Reason: "rule triggered",
//	        },
//	    )
//	    if err != nil { return nil, err }
//	    return []rtypes.Event{event}, nil
//	}
//
// 3. Register the factory:
//
//	func init() {
//	    RegisterRuleFactory("my_rule", &MyRuleFactory{ruleType: "my_rule"})
//	}
//
// # Entity State Watching
//
// Rules can watch entity state changes via NATS KV buckets:
//
//	{
//	  "id": "my-rule",
//	  "type": "my_rule",
//	  "entity": {
//	    "pattern": "c360.*.robotics.*.drone.*",
//	    "watch_buckets": ["ENTITY_STATES"]
//	  }
//	}
//
// The processor watches ENTITY_STATES using canonical six-position KV
// patterns and evaluates declared entity rules directly against EntityState.
// An omitted entity.pattern declares a message-path rule instead.
//
// # Runtime Configuration
//
// Rule definitions support dynamic runtime updates via ApplyConfigUpdate(ctx, changes):
//   - Add/remove rules without restart
//   - Update rule conditions and metadata
//   - Enable/disable rules on the fly
//
// Component configuration, including ports, entity-watch patterns, graph
// integration, and pack identity, remains immutable until the next boot.
//
// # Event Generation
//
// Rules generate gtypes.Event directly (no template system):
//   - Events contain: Type, EntityID, Properties, Metadata
//   - Published to NATS graph subjects
//   - Consumed by graph processor for entity storage
//
// # Metrics
//
// The processor exposes Prometheus metrics:
//   - semstreams_rule_evaluations_total
//   - semstreams_rule_triggers_total
//   - semstreams_rule_evaluation_duration_seconds
//   - semstreams_rule_errors_total
//
// # Example Usage
//
//	config, err := rule.NewConfig("example-pack")
//	config.Ports = &component.PortConfig{
//	    Inputs: []component.PortDefinition{
//	        {
//	            Name:   "semantic_messages",
//	            Config: component.NATSPort{Subject: "process.>"},
//	        },
//	    },
//	}
//
//	processor, err := rule.NewProcessor(natsClient, &config)
//	if err != nil {
//	    log.Fatal(err)
//	}
//	err = processor.Initialize()
//	err = processor.Start(ctx)
//
// For comprehensive documentation, see:
//   - /Users/coby/Code/c360/semdocs/docs/guides/rules-engine.md
//   - /Users/coby/Code/c360/semdocs/docs/specs/SPEC-001-generic-rules-engine.md
package rule

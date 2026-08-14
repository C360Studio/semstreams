package agenticdispatch

import (
	"fmt"

	"github.com/c360studio/semstreams/component"
	"github.com/c360studio/semstreams/pkg/errs"
)

// Config represents the configuration for the router processor.
// Model selection is resolved from the unified model registry (component.Dependencies.ModelRegistry).
type Config struct {
	DefaultRole                string                `json:"default_role" schema:"type:string,description:Default role for new tasks,default:general,category:basic,required"`
	AutoContinue               bool                  `json:"auto_continue" schema:"type:bool,description:Automatically continue last active loop,default:true,category:basic"` // Continue last loop if exists
	Permissions                PermissionConfig      `json:"permissions" schema:"type:object,description:Permission configuration,category:advanced"`
	StreamName                 string                `json:"stream_name" schema:"type:string,description:NATS stream name for user messages,default:USER,category:advanced"`
	ConsumerNameSuffix         string                `json:"consumer_name_suffix,omitempty" schema:"type:string,description:Suffix appended to consumer names for uniqueness,category:advanced"`
	DeleteConsumerOnStop       bool                  `json:"delete_consumer_on_stop,omitempty" schema:"type:bool,description:Delete durable consumers on Stop (use for tests only),category:advanced,default:false"`
	Ports                      *component.PortConfig `json:"ports,omitempty" schema:"type:ports,description:Port configuration for inputs and outputs,category:basic"`
	EnableIntentClassification bool                  `json:"enable_intent_classification,omitempty" schema:"type:bool,description:Enable LLM-assisted intent classification for ambiguous messages,category:advanced,default:false"`
	// DefaultTools is the tool allowlist for initial-user-message tasks
	// (the first loop in a chain, before any rule has fired). Names are
	// resolved against the agentictools global registry at dispatch time;
	// unknown names are logged and dropped. Empty/nil leaves
	// TaskMessage.Tools unset so the spawned loop falls back to global
	// discovery — matching existing behaviour. Downstream agents get their
	// tool allowlist from the rule that spawns them (publish_agent.tools),
	// keeping role→tools decisions in the workflow config.
	DefaultTools []string `json:"default_tools,omitempty" schema:"type:array,description:Tool names granted to initial user-message tasks (resolved at dispatch; nil/empty falls back to global discovery),category:advanced"`
}

// PermissionConfig defines permission rules for the router
type PermissionConfig struct {
	View       []string `json:"view"`        // Who can view status, loops, history
	SubmitTask []string `json:"submit_task"` // Who can submit new tasks
	CancelOwn  bool     `json:"cancel_own"`  // Users can cancel their own loops
	CancelAny  []string `json:"cancel_any"`  // Who can cancel any loop
	Approve    []string `json:"approve"`     // Who can approve results
}

// Validate validates the configuration
func (c Config) Validate() error {
	if c.DefaultRole == "" {
		return errs.WrapInvalid(fmt.Errorf("default_role is required"), "Config", "Validate", "check default_role")
	}
	return nil
}

// DefaultConfig returns the default configuration
func DefaultConfig() Config {
	return Config{
		DefaultRole:  "general",
		AutoContinue: true,
		StreamName:   "USER",
		Permissions: PermissionConfig{
			View:       []string{"*"}, // Everyone can view
			SubmitTask: []string{"*"}, // Everyone can submit
			CancelOwn:  true,          // Users can cancel their own
			CancelAny:  []string{},    // No one can cancel others by default
			Approve:    []string{"*"}, // Everyone can approve
		},
		Ports: &component.PortConfig{
			Inputs: []component.PortDefinition{
				{
					Name: "user.message", Config: component.JetStreamPort{Subjects: []string{"user.message.>"}, StreamName: "USER"}, Required: true,
					Description: "User messages from all channels",
				},
				{
					Name: "agent.complete", Config: component.JetStreamPort{Subjects: []string{"agent.complete.*"}, StreamName: "AGENT"}, Required: true,
					Description: "Agent task completions",
				},
				{
					Name: "agent.created", Config: component.JetStreamPort{Subjects: []string{"agent.created.*"}, StreamName: "AGENT"}, Required: false,
					Description: "Loop creation events",
				},
				{
					Name: "agent.failed", Config: component.JetStreamPort{Subjects: []string{"agent.failed.*"}, StreamName: "AGENT"}, Required: false,
					Description: "Loop failure events",
				},
				{
					Name: "agent.approval_pending", Config: component.JetStreamPort{Subjects: []string{"agent.approval_pending.*"}, StreamName: "AGENT"}, Required: false,
					Description: "Approval-pending events used to populate the dispatch HTTP approval handler's CallID lookup",
				},
			},
			Outputs: []component.PortDefinition{
				{
					Name: "agent.task", Config: component.JetStreamPort{Subjects: []string{"agent.task.*"}, StreamName: "AGENT"}, Description: "Agent task requests",
				},
				{
					Name: "agent.signal", Config: component.JetStreamPort{Subjects: []string{"agent.signal.*"}, StreamName: "AGENT"}, Description: "Agent control signals",
				},
				{
					Name: "user.response", Config: component.JetStreamPort{
						Subjects: []string{"user.response.>"}, StreamName: "USER",
						Interface: &component.InterfaceContract{Type: "agentic.user_response", Version: "v1"},
					}, Description: "Typed responses back to users",
				},
				{
					Name: "agent.approval_response", Config: component.JetStreamPort{Subjects: []string{"agent.approval_response.*"}, StreamName: "AGENT"}, Description: "Approval responses submitted via the dispatch HTTP /loops/{id}/approval endpoint, consumed by agentic-loop's approval-response handler",
				},
			},
		},
	}
}

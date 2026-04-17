package agenticdispatch

import (
	"fmt"

	"github.com/c360studio/semstreams/component"
	"github.com/c360studio/semstreams/pkg/errs"
)

// Config represents the configuration for the router processor.
// Model selection is resolved from the unified model registry (component.Dependencies.ModelRegistry).
type Config struct {
	DefaultRole          string                `json:"default_role" schema:"type:string,description:Default role for new tasks,default:general,category:basic,required"`
	AutoContinue         bool                  `json:"auto_continue" schema:"type:bool,description:Automatically continue last active loop,default:true,category:basic"` // Continue last loop if exists
	Permissions          PermissionConfig      `json:"permissions" schema:"type:object,description:Permission configuration,category:advanced"`
	StreamName           string                `json:"stream_name" schema:"type:string,description:NATS stream name for user messages,default:USER,category:advanced"`
	ConsumerNameSuffix   string                `json:"consumer_name_suffix,omitempty" schema:"type:string,description:Suffix appended to consumer names for uniqueness,category:advanced"`
	DeleteConsumerOnStop bool                  `json:"delete_consumer_on_stop,omitempty" schema:"type:bool,description:Delete durable consumers on Stop (use for tests only),category:advanced,default:false"`
	Ports                *component.PortConfig `json:"ports,omitempty" schema:"type:ports,description:Port configuration for inputs and outputs,category:basic"`
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
					Name:        "user.message",
					Type:        "jetstream",
					Subject:     "user.message.>",
					StreamName:  "USER",
					Required:    true,
					Description: "User messages from all channels",
				},
				{
					Name:        "agent.complete",
					Type:        "jetstream",
					Subject:     "agent.complete.*",
					StreamName:  "AGENT",
					Required:    true,
					Description: "Agent task completions",
				},
				{
					Name:        "agent.created",
					Type:        "jetstream",
					Subject:     "agent.created.*",
					StreamName:  "AGENT",
					Required:    false,
					Description: "Loop creation events",
				},
				{
					Name:        "agent.failed",
					Type:        "jetstream",
					Subject:     "agent.failed.*",
					StreamName:  "AGENT",
					Required:    false,
					Description: "Loop failure events",
				},
			},
			Outputs: []component.PortDefinition{
				{
					Name:        "agent.task",
					Type:        "jetstream",
					Subject:     "agent.task.*",
					StreamName:  "AGENT",
					Description: "Agent task requests",
				},
				{
					Name:        "agent.signal",
					Type:        "jetstream",
					Subject:     "agent.signal.*",
					StreamName:  "AGENT",
					Description: "Agent control signals",
				},
				{
					Name:        "user.response",
					Type:        "jetstream",
					Subject:     "user.response.>",
					StreamName:  "USER",
					Description: "Responses back to users",
				},
			},
		},
	}
}

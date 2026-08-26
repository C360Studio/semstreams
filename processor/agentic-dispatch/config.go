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

// agentLoopsPortName is the declared KV read port whose bucket dispatch
// OBSERVES for every persisted-loop read. agentic-tools declares the same
// port name for the same bucket; before gh#1094 dispatch predicted the name
// with a constant, so a deployment running a non-default loops bucket lost
// every terminal route without saying so.
const agentLoopsPortName = "agent_loops"

// loopsBucketFromPorts resolves the loops bucket from a declared port set
// through the canonical port projection, so the name is OBSERVED from
// configuration. It is the only place the bucket is obtained; readers carry
// no default of their own, and an undeclared or non-KV-read port is an error
// rather than a silent fallback to the default name.
func loopsBucketFromPorts(ports *component.PortConfig) (string, error) {
	if ports == nil {
		return "", fmt.Errorf("port %q not declared", agentLoopsPortName)
	}
	for _, definition := range ports.Inputs {
		if definition.Name != agentLoopsPortName {
			continue
		}
		port, err := definition.Resolve(component.DirectionInput)
		if err != nil {
			return "", fmt.Errorf("resolve port %q: %w", agentLoopsPortName, err)
		}
		facts, err := port.Facts()
		if err != nil {
			return "", fmt.Errorf("project port %q: %w", agentLoopsPortName, err)
		}
		bucket, ok := facts.KVReadBucket()
		if !ok {
			return "", fmt.Errorf("port %q does not declare a KV read bucket", agentLoopsPortName)
		}
		return bucket, nil
	}
	return "", fmt.Errorf("port %q not declared", agentLoopsPortName)
}

// loopsBucketName resolves the loops bucket for this component's ports.
func (c *Component) loopsBucketName() (string, error) {
	return loopsBucketFromPorts(c.config.Ports)
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
				{
					Name: agentLoopsPortName, Config: component.KVReadPort{Bucket: "AGENT_LOOPS"}, Required: false,
					Description: "Persisted loop records read for terminal route reconciliation, workflow origin resolution, and the /activity stream",
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

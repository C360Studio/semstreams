package agenticdispatch

import (
	"encoding/json"
	"testing"

	"github.com/c360studio/semstreams/component"
)

// TestConsumerName_WithSuffix confirms ConsumerNameSuffix is appended to
// the base name. This is the mechanism that lets two dispatches coexist
// in one process without colliding on JetStream consumer names.
func TestConsumerName_WithSuffix(t *testing.T) {
	c := &Component{config: Config{ConsumerNameSuffix: "ops"}}
	got := c.consumerName("agentic-dispatch-user-message")
	want := "agentic-dispatch-user-message-ops"
	if got != want {
		t.Errorf("consumerName = %q, want %q", got, want)
	}
}

// TestConsumerName_EmptySuffix_ReturnsBase is the backward-compat guarantee —
// existing deployments with no suffix configured must keep today's consumer names.
func TestConsumerName_EmptySuffix_ReturnsBase(t *testing.T) {
	c := &Component{config: Config{ConsumerNameSuffix: ""}}
	got := c.consumerName("agentic-dispatch-user-message")
	want := "agentic-dispatch-user-message"
	if got != want {
		t.Errorf("consumerName = %q, want %q", got, want)
	}
}

// TestConsumerName_MultipleInstancesDistinct documents the multi-instance
// invariant: two components with different suffixes produce different names.
func TestConsumerName_MultipleInstancesDistinct(t *testing.T) {
	a := &Component{config: Config{ConsumerNameSuffix: "ops"}}
	b := &Component{config: Config{ConsumerNameSuffix: "research"}}
	base := "agentic-dispatch-user-message"
	if a.consumerName(base) == b.consumerName(base) {
		t.Errorf("two dispatches with distinct suffixes produced the same consumer name: %q", a.consumerName(base))
	}
}

// TestInputPortSubject_Override confirms a configured input port subject
// wins over the fallback. The fallback fires only when no port matches.
func TestInputPortSubject_Override(t *testing.T) {
	c := &Component{config: Config{
		Ports: &component.PortConfig{
			Inputs: []component.PortDefinition{
				{Name: "user.message", Subject: "user.message.ops.>", StreamName: "USER"},
			},
		},
	}}
	got := c.inputPortSubject("user.message", "user.message.>")
	if got != "user.message.ops.>" {
		t.Errorf("inputPortSubject = %q, want port override", got)
	}
}

// TestInputPortSubject_MissingPort_ReturnsFallback confirms the fallback path
// for default-config deployments (no ports block).
func TestInputPortSubject_MissingPort_ReturnsFallback(t *testing.T) {
	c := &Component{config: Config{}}
	got := c.inputPortSubject("user.message", "user.message.>")
	if got != "user.message.>" {
		t.Errorf("inputPortSubject = %q, want fallback", got)
	}
}

// TestInputPortStream_Override confirms a configured stream name on the port
// is honored (so a downstream product can split agent events onto a custom stream).
func TestInputPortStream_Override(t *testing.T) {
	c := &Component{config: Config{
		Ports: &component.PortConfig{
			Inputs: []component.PortDefinition{
				{Name: "agent.complete", Subject: "agent.complete.*", StreamName: "OPS_AGENT"},
			},
		},
	}}
	got := c.inputPortStream("agent.complete", "AGENT")
	if got != "OPS_AGENT" {
		t.Errorf("inputPortStream = %q, want port override OPS_AGENT", got)
	}
}

// TestInputPortStream_MissingPort_ReturnsFallback covers the default-config case.
func TestInputPortStream_MissingPort_ReturnsFallback(t *testing.T) {
	c := &Component{config: Config{}}
	got := c.inputPortStream("agent.complete", "AGENT")
	if got != "AGENT" {
		t.Errorf("inputPortStream = %q, want fallback AGENT", got)
	}
}

// TestInputPortStream_PortWithEmptyStream_ReturnsFallback — a port that
// declares subject but no stream falls back to the caller's default rather
// than substituting empty-string (which would fail consumer creation).
func TestInputPortStream_PortWithEmptyStream_ReturnsFallback(t *testing.T) {
	c := &Component{config: Config{
		Ports: &component.PortConfig{
			Inputs: []component.PortDefinition{
				{Name: "agent.complete", Subject: "agent.complete.*"},
			},
		},
	}}
	got := c.inputPortStream("agent.complete", "AGENT")
	if got != "AGENT" {
		t.Errorf("inputPortStream with empty port.StreamName = %q, want fallback AGENT", got)
	}
}

// TestUserResponseSubject_UsesOutputPortDef confirms the publish path
// resolves via ResolveSubject + the user.response output port definition,
// matching today's "user.response.{type}.{id}" shape.
func TestUserResponseSubject_UsesOutputPortDef(t *testing.T) {
	outputs := []component.PortDefinition{
		{Name: "user.response", Subject: "user.response.>", StreamName: "USER"},
	}
	got := component.ResolveSubject(outputs, "user.response", "cli.channel-123")
	want := "user.response.cli.channel-123"
	if got != want {
		t.Errorf("ResolveSubject = %q, want %q", got, want)
	}
}

// TestUserResponseSubject_OverriddenPort confirms an override (e.g., a
// product that routes user responses onto a role-specific stream subject)
// takes effect. This is the semspec/semteams multi-role escape hatch.
func TestUserResponseSubject_OverriddenPort(t *testing.T) {
	outputs := []component.PortDefinition{
		{Name: "user.response", Subject: "user.response.ops.>", StreamName: "USER_OPS"},
	}
	got := component.ResolveSubject(outputs, "user.response", "cli.channel-123")
	want := "user.response.ops.cli.channel-123"
	if got != want {
		t.Errorf("ResolveSubject with override = %q, want %q", got, want)
	}
}

// TestLoopInfo_RoleField_PropagatedThroughTrack confirms the Role field
// survives the Track → Get round-trip. This is what UIs and test harnesses
// need when listing loops via GET /loops — previously Role lived only on
// TaskMessage and LoopCompletedEvent, never surfacing through /loops.
func TestLoopInfo_RoleField_PropagatedThroughTrack(t *testing.T) {
	tracker := NewLoopTracker()
	tracker.Track(&LoopInfo{LoopID: "l1", TaskID: "t1", Role: "coordinator", UserID: "u1"})
	tracker.Track(&LoopInfo{LoopID: "l2", TaskID: "t2", Role: "ops", UserID: "u2"})

	got := tracker.Get("l1")
	if got == nil || got.Role != "coordinator" {
		t.Errorf("Get(l1).Role = %v, want coordinator", got)
	}
	got = tracker.Get("l2")
	if got == nil || got.Role != "ops" {
		t.Errorf("Get(l2).Role = %v, want ops", got)
	}
}

// TestLoopInfo_RoleField_Serialized confirms Role lands in the JSON that
// HTTP /loops returns. Uses json tag "role,omitempty" so unset roles don't
// clutter old clients' responses.
func TestLoopInfo_RoleField_Serialized(t *testing.T) {
	info := &LoopInfo{LoopID: "l1", TaskID: "t1", Role: "research", UserID: "u1"}
	data, err := json.Marshal(info)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var round map[string]any
	if err := json.Unmarshal(data, &round); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if round["role"] != "research" {
		t.Errorf("role in JSON = %v, want research", round["role"])
	}
}

// TestLoopInfo_RoleField_OmitEmpty confirms an empty role does NOT appear
// in the JSON. Backward-compat for clients that haven't learned about the
// field yet.
func TestLoopInfo_RoleField_OmitEmpty(t *testing.T) {
	info := &LoopInfo{LoopID: "l1", TaskID: "t1"}
	data, err := json.Marshal(info)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var round map[string]any
	if err := json.Unmarshal(data, &round); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if _, present := round["role"]; present {
		t.Errorf("empty role should be omitted from JSON, got: %v", round)
	}
}

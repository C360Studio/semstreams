package agenticloop

import (
	"testing"

	"github.com/c360studio/semstreams/component"
)

// TestHandlers_OutputSubjects_DefaultsMatchPriorHardcode verifies that the
// canonical default declarations resolve to the exact strings previously
// hardcoded by the package.
func TestHandlers_OutputSubjects_DefaultsMatchPriorHardcode(t *testing.T) {
	cfg := DefaultConfig()
	loopID := "loop-abc"
	cases := []struct {
		port string
		want string
	}{
		{"agent.request", "agent.request.loop-abc"},
		{"agent.created", "agent.created.loop-abc"},
		{"agent.complete", "agent.complete.loop-abc"},
		{"agent.failed", "agent.failed.loop-abc"},
	}
	for _, tc := range cases {
		t.Run(tc.port, func(t *testing.T) {
			got, err := component.ResolveSubject(cfg.Ports.Outputs, tc.port, loopID)
			if err != nil {
				t.Fatal(err)
			}
			if got != tc.want {
				t.Errorf("ResolveSubject(%q, %q) = %q, want %q", tc.port, loopID, got, tc.want)
			}
		})
	}
}

// TestHandlers_OutputSubjects_PortOverrides verifies that a product replacing
// an output port definition (e.g., routing agent.request onto an isolated
// subject namespace) sees their override flow through to the publish subject.
func TestHandlers_OutputSubjects_PortOverrides(t *testing.T) {
	ports := []component.PortDefinition{
		{Name: "agent.request", Config: component.JetStreamPort{Subjects: []string{"agent.ops.request.*"}, StreamName: "AGENT_OPS"}},
		{Name: "agent.complete", Config: component.JetStreamPort{Subjects: []string{"agent.ops.complete.*"}, StreamName: "AGENT_OPS"}},
	}
	cases := []struct {
		port string
		want string
	}{
		{"agent.request", "agent.ops.request.loop-xyz"},
		{"agent.complete", "agent.ops.complete.loop-xyz"},
	}
	for _, tc := range cases {
		t.Run(tc.port, func(t *testing.T) {
			got, err := component.ResolveSubject(ports, tc.port, "loop-xyz")
			if err != nil {
				t.Fatal(err)
			}
			if got != tc.want {
				t.Errorf("ResolveSubject(override, %q) = %q, want %q", tc.port, got, tc.want)
			}
		})
	}
}

// TestHandlers_OutputSubjects_ToolExecute verifies the tool.execute subject
// composition preserves the tool-name suffix shape (not a loop ID). Critical
// because the tool-dispatch subject key is the tool name, not the loop.
func TestHandlers_OutputSubjects_ToolExecute(t *testing.T) {
	cfg := DefaultConfig()
	got, err := component.ResolveSubject(cfg.Ports.Outputs, "tool.execute", "web_search")
	if err != nil {
		t.Fatal(err)
	}
	want := "tool.execute.web_search"
	if got != want {
		t.Errorf("ResolveSubject(tool.execute, web_search) = %q, want %q", got, want)
	}
}

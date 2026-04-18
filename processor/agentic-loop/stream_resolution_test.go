package agenticloop

import (
	"testing"

	"github.com/c360studio/semstreams/component"
)

// TestResolveStreamName covers the three-way fall-through established by
// ADR-029 step 2's setupConsumer fix: per-port stream_name wins over
// component-wide default wins over the hardcoded AGENT fallback.
//
// Background: before the fix, agentic-loop.setupConsumer ignored the
// per-port stream_name on a component.Port and always used
// c.config.StreamName. This broke the intended AGENT/TOOL split in the
// flow configs — tool.execute messages went to the TOOL stream (via
// subject routing) but the consumer was bound to AGENT, so deliveries
// never happened. Deep-research and agentic e2es both regressed; this
// test guards the fix.
func TestResolveStreamName(t *testing.T) {
	tests := []struct {
		name                string
		portConfig          component.Portable
		componentStreamName string
		want                string
	}{
		{
			name: "per-port stream_name wins over component default",
			portConfig: component.JetStreamPort{
				StreamName: "TOOL",
				Subjects:   []string{"tool.execute.>"},
			},
			componentStreamName: "AGENT",
			want:                "TOOL",
		},
		{
			name: "per-port empty stream_name falls through to component default",
			portConfig: component.JetStreamPort{
				StreamName: "",
				Subjects:   []string{"agent.task.*"},
			},
			componentStreamName: "AGENT",
			want:                "AGENT",
		},
		{
			name:                "component default used when port is not a JetStreamPort",
			portConfig:          component.NATSPort{Subject: "some.subject"},
			componentStreamName: "AGENT",
			want:                "AGENT",
		},
		{
			name:                "both empty falls back to hardcoded AGENT",
			portConfig:          component.JetStreamPort{},
			componentStreamName: "",
			want:                "AGENT",
		},
		{
			name: "per-port name honoured even when component default is also set to something else",
			portConfig: component.JetStreamPort{
				StreamName: "WORKFLOW",
				Subjects:   []string{"workflow.step.>"},
			},
			componentStreamName: "AGENT",
			want:                "WORKFLOW",
		},
		{
			name:                "nil-ish empty everywhere falls to AGENT fallback",
			portConfig:          nil,
			componentStreamName: "",
			want:                "AGENT",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			port := component.Port{Config: tc.portConfig}
			got := resolveStreamName(port, tc.componentStreamName)
			if got != tc.want {
				t.Errorf("resolveStreamName = %q, want %q (componentDefault=%q, portConfig=%T)",
					got, tc.want, tc.componentStreamName, tc.portConfig)
			}
		})
	}
}

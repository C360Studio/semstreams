package agenticloop

import (
	"fmt"
	"strings"
	"testing"

	"github.com/c360studio/semstreams/component"
	"github.com/c360studio/semstreams/pkg/errs"
)

func TestAgenticLoopConsumerPolicyOwnsMaxAckPending(t *testing.T) {
	for _, tt := range []struct {
		portName string
		fixed    int
	}{
		{portName: "agent.task", fixed: 1},
		{portName: "agent.response", fixed: 1},
		{portName: "tool.result", fixed: 1},
		{portName: "agent.signal", fixed: 10},
	} {
		for _, requested := range []int{0, 4, -1} {
			t.Run(fmt.Sprintf("%s_requested_%d", tt.portName, requested), func(t *testing.T) {
				port, err := (component.PortDefinition{
					Name: tt.portName,
					Config: component.JetStreamPort{
						StreamName: "AGENT", Subjects: []string{tt.portName + ".*"}, MaxAckPending: requested,
					},
				}).Resolve(component.DirectionInput)
				if err != nil {
					t.Fatal(err)
				}

				cfg, fixed, err := agenticLoopConsumerPolicy(port)
				if fixed != tt.fixed {
					t.Fatalf("fixed = %d, want %d", fixed, tt.fixed)
				}
				if requested == 0 {
					if err != nil || cfg.MaxAckPending != 0 {
						t.Fatalf("policy = %+v, error = %v; want fixed component policy accepted", cfg, err)
					}
					return
				}
				if !errs.IsInvalid(err) || !strings.Contains(err.Error(), tt.portName) ||
					!strings.Contains(err.Error(), "max_ack_pending") ||
					!strings.Contains(err.Error(), fmt.Sprintf("at %d", tt.fixed)) {
					t.Fatalf("error = %v, want invalid error naming port, field, and fixed value", err)
				}
			})
		}
	}
}

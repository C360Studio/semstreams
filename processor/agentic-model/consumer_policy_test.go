package agenticmodel

import (
	"strings"
	"testing"

	"github.com/c360studio/semstreams/component"
	"github.com/c360studio/semstreams/pkg/errs"
)

func TestAgenticModelConsumerPolicyOwnsMaxAckPending(t *testing.T) {
	for _, requested := range []int{0, 4, -1} {
		t.Run(testNameForMaxAckPending(requested), func(t *testing.T) {
			port, err := (component.PortDefinition{
				Name: "agent.request",
				Config: component.JetStreamPort{
					StreamName: "AGENT", Subjects: []string{"agent.request.*"}, MaxAckPending: requested,
				},
			}).Resolve(component.DirectionInput)
			if err != nil {
				t.Fatal(err)
			}

			cfg, err := agenticModelConsumerPolicy(port)
			if requested == 0 {
				if err != nil || cfg.MaxAckPending != 0 {
					t.Fatalf("policy = %+v, error = %v; want fixed component policy accepted", cfg, err)
				}
				return
			}
			if !errs.IsInvalid(err) || !strings.Contains(err.Error(), "agent.request") ||
				!strings.Contains(err.Error(), "max_ack_pending") || !strings.Contains(err.Error(), "at 1") {
				t.Fatalf("error = %v, want invalid error naming port, field, and fixed value", err)
			}
		})
	}
}

func testNameForMaxAckPending(value int) string {
	switch value {
	case 0:
		return "omitted"
	case -1:
		return "unlimited_rejected"
	default:
		return "positive_rejected"
	}
}

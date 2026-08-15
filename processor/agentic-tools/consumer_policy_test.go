package agentictools

import (
	"fmt"
	"strings"
	"testing"

	"github.com/c360studio/semstreams/component"
	"github.com/c360studio/semstreams/pkg/errs"
)

func TestAgenticToolsConsumerPolicyOwnsMaxAckPending(t *testing.T) {
	for _, requested := range []int{0, 4, -1} {
		t.Run(fmt.Sprintf("requested_%d", requested), func(t *testing.T) {
			port, err := (component.PortDefinition{
				Name: "tool.execute",
				Config: component.JetStreamPort{
					StreamName: "AGENT", Subjects: []string{"tool.execute.*"}, MaxAckPending: requested,
				},
			}).Resolve(component.DirectionInput)
			if err != nil {
				t.Fatal(err)
			}

			cfg, err := agenticToolsConsumerPolicy(port)
			if requested == 0 {
				if err != nil || cfg.MaxAckPending != 0 {
					t.Fatalf("policy = %+v, error = %v; want fixed component policy accepted", cfg, err)
				}
				return
			}
			if !errs.IsInvalid(err) || !strings.Contains(err.Error(), "tool.execute") ||
				!strings.Contains(err.Error(), "max_ack_pending") || !strings.Contains(err.Error(), "at 3") {
				t.Fatalf("error = %v, want invalid error naming port, field, and fixed value", err)
			}
		})
	}
}

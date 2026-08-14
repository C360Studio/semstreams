package agenticgovernance

import (
	"encoding/json"
	"testing"

	"github.com/c360studio/semstreams/component"
	"github.com/stretchr/testify/require"
)

func TestNewComponentRejectsRetiredNotifyUserPresenceBeforeConstruction(t *testing.T) {
	for _, value := range []string{"true", "false", "null"} {
		t.Run(value, func(t *testing.T) {
			raw := json.RawMessage(`{
				"filter_chain":{"policy":"not-a-valid-policy","filters":[]},
				"violations":{"notify_user":` + value + `},
				"ports":{"outputs":[{"name":"unknown","config":{"kind":"nats","subject":"bad.subject"}}]}
			}`)

			created, err := NewComponent(raw, component.Dependencies{})
			require.Nil(t, created)
			require.Error(t, err)
			require.ErrorContains(t, err, "violations.notify_user")
			require.ErrorContains(t, err, "removed")
			require.NotContains(t, err.Error(), "not-a-valid-policy")
			require.NotContains(t, err.Error(), "unknown override port")
		})
	}
}

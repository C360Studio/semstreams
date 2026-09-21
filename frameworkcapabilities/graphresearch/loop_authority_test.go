package graphresearch

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/require"
)

// spec: agentic-loop / Loop-state authority has one port declaration
func TestResearchAgreementObservesLoopPort(t *testing.T) {
	for _, row := range []struct {
		name, raw, others string
		wantError         bool
	}{
		{"default", `{}`, "AGENT_LOOPS", false},
		{"custom agreement", `{"ports":{"outputs":[{"name":"loops","config":{"kind":"kv-write","bucket":"RESEARCH_LOOPS"}}]}}`, "RESEARCH_LOOPS", false},
		{"actual port mismatch", `{"ports":{"outputs":[{"name":"loops","config":{"kind":"kv-write","bucket":"OTHER_LOOPS"}}]}}`, "AGENT_LOOPS", true},
		{"retired key", `{"loops_bucket":"AGENT_LOOPS"}`, "AGENT_LOOPS", true},
		{"invalid port", `{"ports":{"outputs":[{"name":"loops","config":{"kind":"nats","subject":"other"}}]}}`, "AGENT_LOOPS", true},
	} {
		t.Run(row.name, func(t *testing.T) {
			configs := map[string][]json.RawMessage{"agentic-loop": {json.RawMessage(row.raw)}}
			for _, name := range append([]string{"agentic-tools"}, stageFactories...) {
				raw, err := json.Marshal(map[string]string{"loops_bucket": row.others})
				require.NoError(t, err)
				configs[name] = []json.RawMessage{raw}
			}
			err := validateLoopsBuckets(configs)
			if row.wantError {
				require.Error(t, err)
				if row.name == "actual port mismatch" {
					require.ErrorContains(t, err, "common loops_bucket")
					require.ErrorContains(t, err, "OTHER_LOOPS")
				} else {
					require.ErrorContains(t, err, "agentic-loop")
				}
			} else {
				require.NoError(t, err)
			}
		})
	}
}

package rule

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/c360studio/semstreams/message"
	"github.com/c360studio/semstreams/payloadregistry"
	"github.com/stretchr/testify/require"
)

// spec: agentic-governance / Governance verdicts use one registered wire and typed handoff
func TestPublishGovernanceVerdictUsesRegisteredCarrier(t *testing.T) {
	for _, decision := range []string{"approved", "rejected"} {
		t.Run(decision, func(t *testing.T) {
			pub := &mockPublisher{}
			executor := NewActionExecutorFull(nil, nil, pub, testExecutorPlatform())
			ec := &ExecutionContext{EntityID: "acme.ops.test.svc.entity.001", RelatedID: "related", MessageData: map[string]any{
				"execution_id": "exec-1", "loop_id": "loop-1", "request_id": "request-1", "proposal_fingerprint": "fingerprint",
			}}
			action := Action{Type: ActionTypePublish, Subject: "agent.toolcall." + decision + ".$message.execution_id", Properties: map[string]any{
				"decision": decision, "execution_id": "$message.execution_id", "loop_id": "$message.loop_id",
				"request_id": "$message.request_id", "proposal_fingerprint": "$message.proposal_fingerprint",
				"reason": "policy", "rule_id": "rule", "priority": 3, "sticky": true, "nested": map[string]any{"x": "y"},
			}}
			require.NoError(t, executor.executePublish(t.Context(), action, ec))
			require.Len(t, pub.published, 1)
			decoder := message.NewDecoder(payloadregistry.NewWithSubset(t, message.RegisterPayloads))
			decoded, err := decoder.Decode(pub.published[0].data)
			require.NoError(t, err, "verdict publication must use the registered carrier")
			require.NoError(t, decoded.Validate())
			payload := decoded.Payload().(*message.GenericJSONPayload).Data
			require.Len(t, payload, 6, "complete publish wrapper must survive")
			require.Equal(t, "acme.ops.test.svc.entity.001", payload["entity_id"])
			require.Equal(t, "related", payload["related_id"])
			require.Equal(t, pub.published[0].subject, payload["subject"])
			require.Equal(t, "rule_engine", payload["source"])
			_, err = time.Parse(time.RFC3339Nano, payload["timestamp"].(string))
			require.NoError(t, err)
			require.Equal(t, map[string]any{
				"decision": decision, "execution_id": "exec-1", "loop_id": "loop-1", "request_id": "request-1",
				"proposal_fingerprint": "fingerprint", "reason": "policy", "rule_id": "rule", "priority": float64(3),
				"sticky": true, "nested": map[string]any{"x": "y"},
			}, payload["properties"])
		})
	}
}

// spec: agentic-governance / Governance verdicts use one registered wire and typed handoff
func TestPublishOutsideVerdictFamiliesKeepsWire(t *testing.T) {
	for _, subject := range []string{"other.topic", "agent.toolcall.approval.exec", "agent.toolcall.approved", "agent.toolcall.rejected"} {
		t.Run(subject, func(t *testing.T) {
			pub := &mockPublisher{}
			executor := NewActionExecutorFull(nil, nil, pub, testExecutorPlatform())
			require.NoError(t, executor.executePublish(t.Context(), Action{Type: ActionTypePublish, Subject: subject,
				Properties: map[string]any{"value": "unchanged"}}, &ExecutionContext{EntityID: "acme.ops.test.svc.entity.001"}))
			require.Len(t, pub.published, 1)
			var payload map[string]any
			require.NoError(t, json.Unmarshal(pub.published[0].data, &payload))
			require.Equal(t, subject, payload["subject"])
			require.Equal(t, map[string]any{"value": "unchanged"}, payload["properties"])
			require.NotContains(t, payload, "type")
		})
	}
}

//go:build integration

package agenticloop

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/c360studio/semstreams/agentic"
	"github.com/c360studio/semstreams/component"
	"github.com/c360studio/semstreams/internal/semantictest"
	"github.com/c360studio/semstreams/natsclient"
	"github.com/c360studio/semstreams/payloadbuiltins"
	ruleprocessor "github.com/c360studio/semstreams/processor/rule"
	"github.com/c360studio/semstreams/types"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
)

type taskLoopIDStreamPublisher struct {
	client  *natsclient.Client
	subject string
	data    []byte
}

func (p *taskLoopIDStreamPublisher) Publish(ctx context.Context, subject string, data []byte) error {
	p.subject = subject
	p.data = append([]byte(nil), data...)
	return p.client.PublishToStream(ctx, subject, data)
}

// spec: entity-id-contract / A loop instance token is minted at its framework birth seam
// spec: rule-agent-publishing / Publish-agent preserves the registered payload boundary
// spec: agentic-loop / Loop task, request, and tool work use only required correlation
func TestIntegrationRuleTaskBytesKeepOneLoopIdentityAcrossProcessReplacement(t *testing.T) {
	ctx := t.Context()
	tc := natsclient.NewTestClient(t, natsclient.WithStreams(
		natsclient.TestStreamConfig{Name: "AGENT", Subjects: []string{"agent.>"}},
	))
	bucket, err := tc.CreateKVBucket(ctx, "AGENT_LOOPS_TASK3A_REPLACEMENT")
	require.NoError(t, err)
	registry := payloadbuiltins.NewTestRegistry(t)

	publisher := &taskLoopIDStreamPublisher{client: tc.Client}
	executor := ruleprocessor.NewActionExecutorFull(nil, nil, publisher, types.PlatformMeta{
		Org: "c360", Platform: "test",
	})
	entityID := semantictest.EntityID(t, "c360", "test", "sensor", "weather", "station", "one")
	require.NoError(t, executor.Execute(ctx, ruleprocessor.Action{
		Type:    ruleprocessor.ActionTypePublishAgent,
		Subject: "agent.task.replacement",
		Role:    "general",
		Model:   "test-model",
		Prompt:  "inspect replacement behavior",
	}, &ruleprocessor.ExecutionContext{EntityID: entityID}))

	stream, err := tc.Client.GetStream(ctx, "AGENT")
	require.NoError(t, err)
	stored, err := stream.GetLastMsgForSubject(ctx, publisher.subject)
	require.NoError(t, err)
	require.Equal(t, publisher.data, stored.Data, "the loop must receive the exact registered bytes emitted by the rule")

	decoded, err := payloadbuiltins.NewTestDecoder(t).Decode(stored.Data)
	require.NoError(t, err)
	task, ok := decoded.Payload().(*agentic.TaskMessage)
	require.Truef(t, ok, "expected registered *agentic.TaskMessage, got %T", decoded.Payload())
	require.NoError(t, task.Validate())
	parsedLoopID, err := uuid.Parse(task.LoopID)
	require.NoError(t, err)
	require.Equal(t, uuid.Version(4), parsedLoopID.Version())

	newProcess := func() *Component {
		discoverable, componentErr := NewComponent([]byte(`{}`), component.Dependencies{
			NATSClient: tc.Client, PayloadRegistry: registry,
		})
		require.NoError(t, componentErr)
		c := discoverable.(*Component)
		c.loopsBucket = bucket
		c.graphWriter = nil
		return c
	}

	first := newProcess()
	decision, err := first.handleTaskMessage(ctx, stored.Data)
	require.NoError(t, err)
	require.Equal(t, natsclient.DeliveryDecisionAck, decision)
	firstEntity, err := first.handler.loopManager.GetLoop(task.LoopID)
	require.NoError(t, err)
	require.Equal(t, task.TaskID, firstEntity.TaskID)
	require.Len(t, first.handler.loopManager.loops, 1)

	second := newProcess()
	decision, err = second.handleTaskMessage(ctx, stored.Data)
	require.NoError(t, err)
	require.Equal(t, natsclient.DeliveryDecisionAck, decision)
	secondEntity, err := second.handler.loopManager.GetLoop(task.LoopID)
	require.NoError(t, err)
	require.Equal(t, firstEntity.ID, secondEntity.ID)
	require.Equal(t, firstEntity.TaskID, secondEntity.TaskID)
	require.Len(t, second.handler.loopManager.loops, 1)

	durable, err := bucket.Get(ctx, task.LoopID)
	require.NoError(t, err)
	var durableEntity agentic.LoopEntity
	require.NoError(t, json.Unmarshal(durable.Value(), &durableEntity))
	require.Equal(t, task.LoopID, durableEntity.ID)
	require.Equal(t, task.TaskID, durableEntity.TaskID)
}

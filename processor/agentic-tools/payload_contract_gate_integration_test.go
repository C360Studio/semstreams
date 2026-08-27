//go:build integration

package agentictools_test

import (
	"context"
	"encoding/json"
	"log/slog"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/c360studio/semstreams/agentic"
	"github.com/c360studio/semstreams/graph"
	"github.com/c360studio/semstreams/message"
	agentictools "github.com/c360studio/semstreams/processor/agentic-tools"
	"github.com/c360studio/semstreams/types"
)

// TestMalformedDiagnosisNeverReachesTheGraph (Codex round, HIGH): the payload's
// Validate IS the writer's contract, and it gates both lanes a finding can
// take. Mutation lane: the emit_diagnosis tool, against a real graph-ingest,
// refuses the Codex repro shape (no finding, recommendation, evidence,
// severity; confidence 2) and births nothing. Fact lane: the same shape
// cannot be published — BaseMessage.MarshalJSON refuses it — so no producer
// using the framework's publish path can put it on the ENTITY stream.
//
// Boundary (stated in the payload-registry delta, filed as #1112): the fact
// lane's consumer decodes WITHOUT calling Validate, so hand-crafted wire bytes
// that bypass MarshalJSON are not gated here; the decoded payload still
// carries the contract, as the last assertion shows.
func TestMalformedDiagnosisNeverReachesTheGraph(t *testing.T) {
	ctx := context.Background()
	client := graphMutationTestClient(t)
	startGraphIngestForMutationTest(t, client)

	executor := agentictools.NewEmitDiagnosisExecutor(
		agentictools.NewNATSTriplePublisher(client),
		types.PlatformMeta{Org: "acme", Platform: "ops"},
		slog.Default(),
	)
	result, err := executor.Execute(ctx, agentic.ToolCall{
		ID: "call-1", Name: agentictools.EmitDiagnosisToolName, LoopID: "loop-gate",
		Arguments: map[string]any{"confidence": 2.0},
	})
	require.NoError(t, err, "a contract rejection is a tool result, not an executor error")
	assert.Equal(t, agentic.ToolErrorInvalidArgs, result.ErrorKind)
	assert.Contains(t, result.Error, "finding")

	js, err := client.JetStream()
	require.NoError(t, err)
	kv, err := js.KeyValue(ctx, graph.BucketEntityStates)
	require.NoError(t, err)
	keys, err := kv.Keys(ctx)
	if err == nil {
		for _, key := range keys {
			assert.NotContains(t, key, ".ops.diagnosis.finding.", "a malformed finding was born: %s", key)
		}
	}

	// Fact lane: the same shape is unpublishable through the framework's wrap.
	malformed := &agentic.OpsDiagnosisEntity{Org: "acme", Platform: "ops", ID: "finding-1", Confidence: 2}
	_, marshalErr := json.Marshal(message.NewBaseMessage(malformed.Schema(), malformed, "test"))
	require.Error(t, marshalErr, "BaseMessage.MarshalJSON must refuse a payload that fails its contract")

	// The consumer's information is intact even where the call is missing (#1112).
	require.Error(t, malformed.Validate())
}

//go:build integration

package executors

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/c360studio/semstreams/agentic"
	"github.com/c360studio/semstreams/natsclient"
	agentictools "github.com/c360studio/semstreams/processor/agentic-tools"
	"github.com/c360studio/semstreams/storage/objectstore"
)

// TestIntegration_ReadLoopResult_NonDefaultContentBucket is the HIGH-5
// registration-level contract (payload-size-chokepoints): read_loop_result
// resolves the hydration bucket from the ref ITSELF
// (StorageReference.StorageInstance), so a loop configured with a NON-default
// content_bucket reads back correctly even though registration only knew the
// default fallback. The pre-fix wiring bound one boot-time bucket, so a
// non-default deployment hydrated from the wrong store.
func TestIntegration_ReadLoopResult_NonDefaultContentBucket(t *testing.T) {
	tc := natsclient.NewTestClient(t, natsclient.WithKV())
	client := tc.Client
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	const (
		loopsBucket      = "AGENT_LOOPS_HIGH5"
		nonDefaultBucket = "AGENT_CONTENT_HIGH5" // NOT the registration fallback
		loopID           = "loop-high5-001"
	)

	// Register through the PRODUCTION wire with the DEFAULT fallback content
	// bucket — the non-default bucket is deliberately unknown to registration.
	reg := agentictools.NewExecutorRegistry()
	require.NoError(t, RegisterBuiltins(ctx, reg, ToolDependencies{
		NATSClient:   client,
		LoopsBucket:  loopsBucket,
		SkipBuiltins: skipAllBut("read_loop_result"),
	}))

	// Write the offloaded body the way agentic-loop's offload path does: an
	// objectstore.Store on the NON-default bucket (no InstanceName, so the
	// stamped StorageInstance IS the bucket name) storing a LoopResultEntity.
	store, err := objectstore.NewStoreWithConfig(ctx, client, objectstore.Config{
		BucketName: nonDefaultBucket,
	})
	require.NoError(t, err)

	body := strings.Repeat("high-five payload body ", 200)
	entity, err := agentic.NewLoopResultEntity("c360", "ops", loopID, body)
	require.NoError(t, err)
	ref, err := store.StoreContent(ctx, entity)
	require.NoError(t, err)
	require.Equal(t, nonDefaultBucket, ref.StorageInstance,
		"the ref must carry the writer's bucket identity — the premise per-ref resolution rides on")

	// Write the ref-bearing COMPLETE_ value into the loops bucket.
	completion := &agentic.LoopCompletedEvent{
		LoopID:        loopID,
		TaskID:        "task-high5",
		Outcome:       agentic.OutcomeSuccess,
		Role:          "coordinator",
		ResultRef:     ref,
		ResultPreview: body[:64],
		ResultSize:    len(body),
	}
	data, err := json.Marshal(completion)
	require.NoError(t, err)
	loopsKV, err := client.GetKeyValueBucket(ctx, loopsBucket)
	require.NoError(t, err)
	_, err = client.NewKVStore(loopsKV).Put(ctx, "COMPLETE_"+loopID, data)
	require.NoError(t, err)

	// Execute the registered tool: hydration must follow the ref to the
	// non-default bucket and return the FULL body through paging.
	executor := reg.GetTool(agentictools.ReadLoopResultToolName)
	require.NotNil(t, executor, "read_loop_result must be registered")

	var got strings.Builder
	offset := 0
	for {
		res, execErr := executor.Execute(ctx, agentic.ToolCall{
			ID:   "call-high5",
			Name: agentictools.ReadLoopResultToolName,
			Arguments: map[string]any{
				"loop_id":   loopID,
				"max_bytes": 1024,
				"offset":    offset,
			},
		})
		require.NoError(t, execErr)
		require.Empty(t, res.Error, "hydration from the ref-named bucket must succeed")
		got.WriteString(res.Content)
		hasMore, _ := res.Metadata[agentic.MetadataKeyHasMore].(bool)
		if !hasMore {
			break
		}
		next, _ := res.Metadata[agentic.MetadataKeyNextOffset].(int)
		require.Greater(t, next, offset, "paging must advance")
		offset = next
	}
	require.Equal(t, body, got.String(),
		"full body must reassemble byte-exact from the NON-default bucket the ref names")

}

//go:build integration

package agentictools_test

import (
	"context"
	"encoding/json"
	"fmt"
	"sync/atomic"
	"testing"
	"time"

	"github.com/c360studio/semstreams/agentic"
	"github.com/c360studio/semstreams/component"
	"github.com/c360studio/semstreams/payloadbuiltins"
	agentictools "github.com/c360studio/semstreams/processor/agentic-tools"
	"github.com/nats-io/nats.go"
)

type sliceF2RegistrySpy struct {
	dispatches atomic.Int64
}

func (s *sliceF2RegistrySpy) ListTools() []agentic.ToolDefinition { return []agentic.ToolDefinition{} }

func (s *sliceF2RegistrySpy) Execute(_ context.Context, call agentic.ToolCall) (agentic.ToolResult, error) {
	s.dispatches.Add(1)
	return agentic.ToolResult{
		CallID: call.ID, Error: fmt.Sprintf("tool %q not found", call.Name), ErrorKind: agentic.ToolErrorNotFound,
	}, fmt.Errorf("sliceF2RegistrySpy.Execute: %w: %q", agentic.ErrToolNotFound, call.Name)
}

func TestSliceF2ApprovalRequiredFormerNameInterceptsBeforeRegistryMiss(t *testing.T) {
	natsClient := getSharedNATSClient(t)
	formerNames := []string{"search_graph", "summarize_graph"}
	config := agentictools.DefaultConfig()
	config.ConsumerNameSuffix = "slice-f2-approval"
	config.DeleteConsumerOnStop = true
	config.AllowedTools = formerNames
	config.ApprovalRequired = formerNames
	rawConfig, err := json.Marshal(config)
	if err != nil {
		t.Fatalf("marshal config: %v", err)
	}
	spy := &sliceF2RegistrySpy{}
	created, err := agentictools.NewComponent(rawConfig, component.Dependencies{
		NATSClient: natsClient, PayloadRegistry: payloadbuiltins.NewTestRegistry(t), ToolRegistry: spy,
	})
	if err != nil {
		t.Fatalf("NewComponent: %v", err)
	}
	comp := created.(*agentictools.Component)
	if err := comp.Initialize(); err != nil {
		t.Fatalf("Initialize: %v", err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	if err := comp.Start(ctx); err != nil {
		t.Fatalf("Start: %v", err)
	}
	defer comp.Stop(5 * time.Second)

	results := make(chan agentic.ToolResult, 4)
	decoder := payloadbuiltins.NewTestDecoder(t)
	subscription, err := natsClient.Subscribe(ctx, "tool.result.*", func(_ context.Context, message *nats.Msg) {
		decoded, decodeErr := decoder.Decode(message.Data)
		if decodeErr != nil {
			return
		}
		result, ok := decoded.Payload().(*agentic.ToolResult)
		if ok {
			results <- *result
		}
	})
	if err != nil {
		t.Fatalf("subscribe results: %v", err)
	}
	defer subscription.Unsubscribe()

	for index, name := range formerNames {
		unapprovedID := fmt.Sprintf("slice-f2-unapproved-%d", index)
		publishToolCallMessage(t, natsClient, "tool.execute.slice-f2-unapproved", &agentic.ToolCall{
			ID: unapprovedID, Name: name, LoopID: "loop-slice-f2",
			Metadata: map[string]any{agentic.MetadataKeyAdvertisedTools: formerNames},
		})
		unapproved := sliceF2AwaitResult(t, results, unapprovedID)
		if unapproved.ErrorKind != agentic.ToolErrorPermission || !agentic.IsApprovalRequired(unapproved.Error) {
			t.Fatalf("unapproved %q result = %#v, want permission approval/pause signal", name, unapproved)
		}
		if got := spy.dispatches.Load(); got != int64(index) {
			t.Fatalf("unapproved %q reached registry: dispatches=%d, want %d", name, got, index)
		}

		approvedID := fmt.Sprintf("slice-f2-approved-%d", index)
		publishToolCallMessage(t, natsClient, "tool.execute.slice-f2-approved", &agentic.ToolCall{
			ID: approvedID, Name: name, LoopID: "loop-slice-f2", ApprovedBy: "owner",
			Metadata: map[string]any{agentic.MetadataKeyAdvertisedTools: formerNames},
		})
		approved := sliceF2AwaitResult(t, results, approvedID)
		if approved.ErrorKind != agentic.ToolErrorNotFound {
			t.Fatalf("approved %q result = %#v, want typed not-found", name, approved)
		}
		if got := spy.dispatches.Load(); got != int64(index+1) {
			t.Fatalf("approved %q dispatches=%d, want %d", name, got, index+1)
		}
	}
}

func sliceF2AwaitResult(t *testing.T, results <-chan agentic.ToolResult, callID string) agentic.ToolResult {
	t.Helper()
	timer := time.NewTimer(5 * time.Second)
	defer timer.Stop()
	for {
		select {
		case result := <-results:
			if result.CallID == callID {
				return result
			}
		case <-timer.C:
			t.Fatalf("timed out waiting for result %s", callID)
			return agentic.ToolResult{}
		}
	}
}

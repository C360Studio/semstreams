package agenticloop_test

import (
	"reflect"
	"testing"

	"github.com/c360studio/semstreams/agentic"
	agenticloop "github.com/c360studio/semstreams/processor/agentic-loop"
)

// spec: agentic-loop / LoopEntity has one operational state contract
func TestOperationalManagerInstallationAndCancellation(t *testing.T) {
	manager := agenticloop.NewLoopManager()
	loopID, err := manager.CreateLoop("task", "general", "model")
	if err != nil {
		t.Fatal(err)
	}
	entity, err := manager.GetLoop(loopID)
	if err != nil {
		t.Fatal(err)
	}
	before := entity
	entity.State = agentic.LoopStateAwaitingApproval
	if err := manager.UpdateLoop(entity); err == nil {
		t.Error("installed awaiting state without gate")
	}
	current, err := manager.GetLoop(loopID)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(current, before) {
		t.Error("refused installation changed current entry")
	}
	entity.PendingApproval = &agentic.PendingApprovalState{CallID: "call", ToolName: "tool"}
	entity.Result = "preserved result"
	entity.PendingToolResults = map[string]agentic.ToolResult{"call": {CallID: "call"}}
	if err := manager.UpdateLoop(entity); err != nil {
		t.Fatal("locally valid gate required delivery evidence", err)
	}
	cancelled, err := manager.CancelLoop(loopID, "user")
	if err != nil {
		t.Fatal(err)
	}
	if cancelled.State != agentic.LoopStateCancelled || cancelled.PendingApproval != nil {
		t.Error("cancel did not atomically close local gate")
	}
	if cancelled.CancelledBy != "user" || cancelled.CancelledAt.IsZero() || cancelled.CompletedAt != cancelled.CancelledAt ||
		cancelled.Outcome != agentic.OutcomeCancelled || cancelled.Error != "cancelled by user" ||
		cancelled.Result != entity.Result || !reflect.DeepEqual(cancelled.PendingToolResults, entity.PendingToolResults) {
		t.Errorf("cancel metadata/results changed: %+v", cancelled)
	}
}

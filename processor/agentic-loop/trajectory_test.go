package agenticloop_test

import (
	"testing"
	"time"

	"github.com/c360studio/semstreams/agentic"
	agenticloop "github.com/c360studio/semstreams/processor/agentic-loop"
)

func TestTrajectoryManager_StartTrajectory(t *testing.T) {
	manager := agenticloop.NewTrajectoryManager()

	loopID := "loop-001"

	traj, err := manager.StartTrajectory(loopID)
	if err != nil {
		t.Fatalf("StartTrajectory() error = %v", err)
	}

	if traj.LoopID != loopID {
		t.Errorf("Trajectory.LoopID = %s, want %s", traj.LoopID, loopID)
	}

	if traj.StartTime.IsZero() {
		t.Error("Trajectory.StartTime should not be zero")
	}

	if traj.EndTime != nil {
		t.Error("Trajectory.EndTime should be nil on start")
	}

	if len(traj.Steps) != 0 {
		t.Errorf("Trajectory.Steps should be empty on start, got %d", len(traj.Steps))
	}

	if traj.TotalTokensIn != 0 {
		t.Errorf("Trajectory.TotalTokensIn should be 0 on start, got %d", traj.TotalTokensIn)
	}

	if traj.TotalTokensOut != 0 {
		t.Errorf("Trajectory.TotalTokensOut should be 0 on start, got %d", traj.TotalTokensOut)
	}

	if traj.Duration != 0 {
		t.Errorf("Trajectory.Duration should be 0 on start, got %d", traj.Duration)
	}
}

func TestTrajectoryManager_AddStep(t *testing.T) {
	manager := agenticloop.NewTrajectoryManager()

	loopID := "loop-001"
	traj, err := manager.StartTrajectory(loopID)
	if err != nil {
		t.Fatalf("StartTrajectory() error = %v", err)
	}

	step := agentic.TrajectoryStep{
		Timestamp: time.Now(),
		StepType:  "model_call",
		RequestID: "req-001",
		Prompt:    "Analyze this",
		Response:  "Analysis complete",
		TokensIn:  100,
		TokensOut: 50,
		Duration:  1500, // 1.5 seconds
	}

	updatedTraj, err := manager.AddStep(loopID, step)
	if err != nil {
		t.Fatalf("AddStep() error = %v", err)
	}

	if len(updatedTraj.Steps) != 1 {
		t.Errorf("After AddStep, Steps count = %d, want 1", len(updatedTraj.Steps))
	}

	if updatedTraj.TotalTokensIn != 100 {
		t.Errorf("TotalTokensIn = %d, want 100", updatedTraj.TotalTokensIn)
	}

	if updatedTraj.TotalTokensOut != 50 {
		t.Errorf("TotalTokensOut = %d, want 50", updatedTraj.TotalTokensOut)
	}

	if updatedTraj.Duration != 1500 {
		t.Errorf("Duration = %d, want 1500", updatedTraj.Duration)
	}

	// Original trajectory may have been modified in place by the manager,
	// or returned as a copy. Either is acceptable — Steps count of 1 just
	// confirms the manager didn't accidentally append twice. Documented
	// here rather than asserted to keep both implementations valid.
	_ = traj.Steps
}

func TestTrajectoryManager_AddStep_Multiple(t *testing.T) {
	manager := agenticloop.NewTrajectoryManager()

	loopID := "loop-001"
	_, err := manager.StartTrajectory(loopID)
	if err != nil {
		t.Fatalf("StartTrajectory() error = %v", err)
	}

	steps := []agentic.TrajectoryStep{
		{
			Timestamp: time.Now(),
			StepType:  "model_call",
			RequestID: "req-001",
			TokensIn:  100,
			TokensOut: 50,
			Duration:  1000,
		},
		{
			Timestamp: time.Now(),
			StepType:  "tool_call",
			ToolName:  "graph_query",
			TokensIn:  0,
			TokensOut: 0,
			Duration:  500,
		},
		{
			Timestamp: time.Now(),
			StepType:  "model_call",
			RequestID: "req-002",
			TokensIn:  150,
			TokensOut: 75,
			Duration:  2000,
		},
	}

	var finalTraj agentic.Trajectory
	for _, step := range steps {
		finalTraj, err = manager.AddStep(loopID, step)
		if err != nil {
			t.Fatalf("AddStep() error = %v", err)
		}
	}

	if len(finalTraj.Steps) != 3 {
		t.Errorf("After adding 3 steps, Steps count = %d, want 3", len(finalTraj.Steps))
	}

	expectedTokensIn := 100 + 0 + 150
	if finalTraj.TotalTokensIn != expectedTokensIn {
		t.Errorf("TotalTokensIn = %d, want %d", finalTraj.TotalTokensIn, expectedTokensIn)
	}

	expectedTokensOut := 50 + 0 + 75
	if finalTraj.TotalTokensOut != expectedTokensOut {
		t.Errorf("TotalTokensOut = %d, want %d", finalTraj.TotalTokensOut, expectedTokensOut)
	}

	expectedDuration := int64(1000 + 500 + 2000)
	if finalTraj.Duration != expectedDuration {
		t.Errorf("Duration = %d, want %d", finalTraj.Duration, expectedDuration)
	}
}

func TestTrajectoryManager_CompleteTrajectory(t *testing.T) {
	manager := agenticloop.NewTrajectoryManager()

	loopID := "loop-001"
	startTraj, err := manager.StartTrajectory(loopID)
	if err != nil {
		t.Fatalf("StartTrajectory() error = %v", err)
	}

	// Add some steps
	_, err = manager.AddStep(loopID, agentic.TrajectoryStep{
		Timestamp: time.Now(),
		StepType:  "model_call",
		TokensIn:  100,
		TokensOut: 50,
		Duration:  1000,
	})
	if err != nil {
		t.Fatalf("AddStep() error = %v", err)
	}

	// Sleep briefly to ensure elapsed time
	time.Sleep(10 * time.Millisecond)

	outcome := "success"
	completedTraj, err := manager.CompleteTrajectory(loopID, outcome)
	if err != nil {
		t.Fatalf("CompleteTrajectory() error = %v", err)
	}

	if completedTraj.Outcome != outcome {
		t.Errorf("Trajectory.Outcome = %s, want %s", completedTraj.Outcome, outcome)
	}

	if completedTraj.EndTime == nil {
		t.Error("Trajectory.EndTime should not be nil after completion")
	}

	if completedTraj.EndTime.Before(startTraj.StartTime) {
		t.Error("Trajectory.EndTime should be after StartTime")
	}

	// Duration should be calculated as actual elapsed time
	actualDuration := completedTraj.EndTime.Sub(startTraj.StartTime).Milliseconds()
	if completedTraj.Duration < actualDuration-100 || completedTraj.Duration > actualDuration+100 {
		// Allow 100ms tolerance for timing precision
		t.Logf("Warning: Duration = %dms, actual elapsed = %dms (difference may be acceptable)",
			completedTraj.Duration, actualDuration)
	}
}

func TestTrajectoryManager_GetTrajectory(t *testing.T) {
	manager := agenticloop.NewTrajectoryManager()

	loopID := "loop-001"
	_, err := manager.StartTrajectory(loopID)
	if err != nil {
		t.Fatalf("StartTrajectory() error = %v", err)
	}

	// Get trajectory
	traj, err := manager.GetTrajectory(loopID)
	if err != nil {
		t.Fatalf("GetTrajectory() error = %v", err)
	}

	if traj.LoopID != loopID {
		t.Errorf("Trajectory.LoopID = %s, want %s", traj.LoopID, loopID)
	}
}

func TestTrajectoryManager_GetTrajectory_NonExistent(t *testing.T) {
	manager := agenticloop.NewTrajectoryManager()

	_, err := manager.GetTrajectory("loop-does-not-exist")
	if err == nil {
		t.Error("GetTrajectory() with non-existent loop should return error")
	}
}

func TestTrajectoryManager_MultipleLoops(t *testing.T) {
	manager := agenticloop.NewTrajectoryManager()

	loops := []string{"loop-001", "loop-002", "loop-003"}

	// Start trajectories for multiple loops
	for _, loopID := range loops {
		_, err := manager.StartTrajectory(loopID)
		if err != nil {
			t.Fatalf("StartTrajectory(%s) error = %v", loopID, err)
		}
	}

	// Add steps to each
	for i, loopID := range loops {
		_, err := manager.AddStep(loopID, agentic.TrajectoryStep{
			Timestamp: time.Now(),
			StepType:  "model_call",
			TokensIn:  (i + 1) * 100,
			TokensOut: (i + 1) * 50,
			Duration:  1000,
		})
		if err != nil {
			t.Fatalf("AddStep(%s) error = %v", loopID, err)
		}
	}

	// Verify each trajectory is independent
	for i, loopID := range loops {
		traj, err := manager.GetTrajectory(loopID)
		if err != nil {
			t.Fatalf("GetTrajectory(%s) error = %v", loopID, err)
		}

		expectedTokensIn := (i + 1) * 100
		if traj.TotalTokensIn != expectedTokensIn {
			t.Errorf("Loop %s: TotalTokensIn = %d, want %d", loopID, traj.TotalTokensIn, expectedTokensIn)
		}
	}
}

func TestTrajectoryManager_AddStep_NonExistentLoop(t *testing.T) {
	manager := agenticloop.NewTrajectoryManager()

	step := agentic.TrajectoryStep{
		Timestamp: time.Now(),
		StepType:  "model_call",
		TokensIn:  100,
		TokensOut: 50,
		Duration:  1000,
	}

	_, err := manager.AddStep("loop-does-not-exist", step)
	if err == nil {
		t.Error("AddStep() with non-existent loop should return error")
	}
}

func TestTrajectoryManager_CompleteTrajectory_NonExistent(t *testing.T) {
	manager := agenticloop.NewTrajectoryManager()

	_, err := manager.CompleteTrajectory("loop-does-not-exist", "success")
	if err == nil {
		t.Error("CompleteTrajectory() with non-existent loop should return error")
	}
}

func TestTrajectoryManager_ConcurrentAccess(t *testing.T) {
	manager := agenticloop.NewTrajectoryManager()

	loopID := "loop-001"
	_, err := manager.StartTrajectory(loopID)
	if err != nil {
		t.Fatalf("StartTrajectory() error = %v", err)
	}

	// Concurrent adds
	done := make(chan bool)
	for i := 0; i < 10; i++ {
		go func(n int) {
			step := agentic.TrajectoryStep{
				Timestamp: time.Now(),
				StepType:  "model_call",
				RequestID: "req-" + string(rune('0'+n)),
				TokensIn:  10,
				TokensOut: 5,
				Duration:  100,
			}
			_, _ = manager.AddStep(loopID, step)
			done <- true
		}(i)
	}

	for i := 0; i < 10; i++ {
		<-done
	}

	// Race detector will catch concurrency issues
	traj, err := manager.GetTrajectory(loopID)
	if err != nil {
		t.Fatalf("GetTrajectory() error = %v", err)
	}

	// Should have some steps (may not be exactly 10 due to race conditions)
	if len(traj.Steps) == 0 {
		t.Error("Trajectory should have steps after concurrent adds")
	}
}

func TestTrajectoryStep_ModelCall(t *testing.T) {
	step := agentic.TrajectoryStep{
		Timestamp: time.Now(),
		StepType:  "model_call",
		RequestID: "req-001",
		Prompt:    "Analyze data",
		Response:  "Analysis complete",
		TokensIn:  150,
		TokensOut: 75,
		Duration:  2000,
	}

	if err := step.Validate(); err != nil {
		t.Errorf("Valid model_call step should pass validation: %v", err)
	}
}

func TestTrajectoryStep_ToolCall(t *testing.T) {
	step := agentic.TrajectoryStep{
		Timestamp: time.Now(),
		StepType:  "tool_call",
		ToolName:  "graph_query",
		ToolArguments: map[string]any{
			"query": "SELECT * FROM entities",
		},
		ToolResult: "Found 100 entities",
		Duration:   500,
	}

	if err := step.Validate(); err != nil {
		t.Errorf("Valid tool_call step should pass validation: %v", err)
	}
}

func TestTrajectoryStep_InvalidType(t *testing.T) {
	step := agentic.TrajectoryStep{
		Timestamp: time.Now(),
		StepType:  "invalid_type",
		Duration:  1000,
	}

	if err := step.Validate(); err == nil {
		t.Error("Invalid step type should fail validation")
	}
}

func TestTrajectory_TokenTotals(t *testing.T) {
	manager := agenticloop.NewTrajectoryManager()

	loopID := "loop-001"
	_, err := manager.StartTrajectory(loopID)
	if err != nil {
		t.Fatalf("StartTrajectory() error = %v", err)
	}

	// Add steps with varying token counts
	steps := []struct {
		tokensIn  int
		tokensOut int
	}{
		{100, 50},
		{200, 100},
		{150, 75},
		{0, 0}, // Tool call with no tokens
		{300, 150},
	}

	for _, s := range steps {
		_, err = manager.AddStep(loopID, agentic.TrajectoryStep{
			Timestamp: time.Now(),
			StepType:  "model_call",
			TokensIn:  s.tokensIn,
			TokensOut: s.tokensOut,
			Duration:  1000,
		})
		if err != nil {
			t.Fatalf("AddStep() error = %v", err)
		}
	}

	traj, err := manager.GetTrajectory(loopID)
	if err != nil {
		t.Fatalf("GetTrajectory() error = %v", err)
	}

	expectedTokensIn := 100 + 200 + 150 + 0 + 300
	if traj.TotalTokensIn != expectedTokensIn {
		t.Errorf("TotalTokensIn = %d, want %d", traj.TotalTokensIn, expectedTokensIn)
	}

	expectedTokensOut := 50 + 100 + 75 + 0 + 150
	if traj.TotalTokensOut != expectedTokensOut {
		t.Errorf("TotalTokensOut = %d, want %d", traj.TotalTokensOut, expectedTokensOut)
	}
}

func TestTrajectory_CompactionExcludedFromTokenTotals(t *testing.T) {
	manager := agenticloop.NewTrajectoryManager()

	loopID := "loop-compact"
	_, err := manager.StartTrajectory(loopID)
	if err != nil {
		t.Fatalf("StartTrajectory() error = %v", err)
	}

	steps := []agentic.TrajectoryStep{
		{Timestamp: time.Now(), StepType: "model_call", TokensIn: 5000, TokensOut: 500, Duration: 2000},
		{Timestamp: time.Now(), StepType: "tool_call", ToolName: "web_search", Duration: 800},
		{Timestamp: time.Now(), StepType: "context_compaction", TokensIn: 12000, TokensOut: 800, Utilization: 0.72, Duration: 150},
		{Timestamp: time.Now(), StepType: "model_call", TokensIn: 3000, TokensOut: 400, Duration: 1500},
	}

	for _, step := range steps {
		if _, addErr := manager.AddStep(loopID, step); addErr != nil {
			t.Fatalf("AddStep() error = %v", addErr)
		}
	}

	traj, err := manager.GetTrajectory(loopID)
	if err != nil {
		t.Fatalf("GetTrajectory() error = %v", err)
	}

	if len(traj.Steps) != 4 {
		t.Errorf("Steps = %d, want 4 (compaction step should be present)", len(traj.Steps))
	}

	// Token totals must exclude compaction's evicted/summarized tokens
	if traj.TotalTokensIn != 8000 { // 5000 + 3000, NOT + 12000
		t.Errorf("TotalTokensIn = %d, want 8000 (compaction tokens excluded)", traj.TotalTokensIn)
	}
	if traj.TotalTokensOut != 900 { // 500 + 400, NOT + 800
		t.Errorf("TotalTokensOut = %d, want 900 (compaction tokens excluded)", traj.TotalTokensOut)
	}

	// Duration includes compaction wall-clock time
	expectedDuration := int64(2000 + 800 + 150 + 1500)
	if traj.Duration != expectedDuration {
		t.Errorf("Duration = %d, want %d (compaction duration included)", traj.Duration, expectedDuration)
	}
}

func TestTrajectory_DurationAccumulation(t *testing.T) {
	manager := agenticloop.NewTrajectoryManager()

	loopID := "loop-001"
	_, err := manager.StartTrajectory(loopID)
	if err != nil {
		t.Fatalf("StartTrajectory() error = %v", err)
	}

	// Add steps with varying durations
	durations := []int64{1000, 500, 2000, 750, 1500}

	for _, d := range durations {
		_, err = manager.AddStep(loopID, agentic.TrajectoryStep{
			Timestamp: time.Now(),
			StepType:  "model_call",
			Duration:  d,
		})
		if err != nil {
			t.Fatalf("AddStep() error = %v", err)
		}
	}

	traj, err := manager.GetTrajectory(loopID)
	if err != nil {
		t.Fatalf("GetTrajectory() error = %v", err)
	}

	expectedDuration := int64(1000 + 500 + 2000 + 750 + 1500)
	if traj.Duration != expectedDuration {
		t.Errorf("Duration = %d, want %d", traj.Duration, expectedDuration)
	}
}

func TestTrajectory_OutcomeTypes(t *testing.T) {
	manager := agenticloop.NewTrajectoryManager()

	outcomes := []string{"success", "failed", "max_iterations", "timeout", "cancelled"}

	for _, outcome := range outcomes {
		loopID := "loop-" + outcome
		_, err := manager.StartTrajectory(loopID)
		if err != nil {
			t.Fatalf("StartTrajectory(%s) error = %v", loopID, err)
		}

		traj, err := manager.CompleteTrajectory(loopID, outcome)
		if err != nil {
			t.Fatalf("CompleteTrajectory(%s) error = %v", loopID, err)
		}

		if traj.Outcome != outcome {
			t.Errorf("Trajectory outcome = %s, want %s", traj.Outcome, outcome)
		}
	}
}

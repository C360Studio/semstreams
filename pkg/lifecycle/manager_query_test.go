package lifecycle

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"
)

// --- List ---

func TestManager_List_EmptyBucketReturnsEmpty(t *testing.T) {
	mgr, _ := newTestManager(t)
	got, err := mgr.List(context.Background(), "mission", ListOptions{})
	if err != nil {
		t.Fatalf("List on empty bucket: %v", err)
	}
	if len(got) != 0 {
		t.Errorf("List on empty bucket returned %d entries, want 0", len(got))
	}
}

func TestManager_List_HappyPath(t *testing.T) {
	mgr, _ := newTestManager(t)
	mustCreate(t, mgr, "l-1", "planning")
	mustCreate(t, mgr, "l-2", "planning")
	mustCreate(t, mgr, "l-3", "planning")

	got, err := mgr.List(context.Background(), "mission", ListOptions{})
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(got) != 3 {
		t.Errorf("List returned %d entries, want 3", len(got))
	}
	seen := make(map[string]bool)
	for _, p := range got {
		seen[p.EntityID()] = true
	}
	for _, want := range []string{"l-1", "l-2", "l-3"} {
		if !seen[want] {
			t.Errorf("List missing entity %q", want)
		}
	}
}

func TestManager_List_PhaseFilter(t *testing.T) {
	mgr, _ := newTestManager(t)
	mustCreate(t, mgr, "p-planning", "planning")
	mustCreate(t, mgr, "p-flying", "planning")
	must(t, mgr.Transition(context.Background(), "mission", "p-flying", "flying", TransitionSourceRule, ""))

	got, err := mgr.List(context.Background(), "mission", ListOptions{Phase: "flying"})
	if err != nil {
		t.Fatalf("List with Phase filter: %v", err)
	}
	if len(got) != 1 || got[0].EntityID() != "p-flying" {
		t.Errorf("Phase filter returned %d entries: %+v", len(got), got)
	}
}

func TestManager_List_ActiveFilter(t *testing.T) {
	mgr, _ := newTestManager(t)
	mustCreate(t, mgr, "a-active", "planning")
	mustCreate(t, mgr, "a-terminal", "planning")
	must(t, mgr.Transition(context.Background(), "mission", "a-terminal", "aborted", TransitionSourceOperator, ""))

	got, err := mgr.List(context.Background(), "mission", ListOptions{Active: true})
	if err != nil {
		t.Fatalf("List with Active filter: %v", err)
	}
	if len(got) != 1 || got[0].EntityID() != "a-active" {
		t.Errorf("Active=true should exclude terminals; got %d entries: %+v", len(got), got)
	}
}

func TestManager_List_MatchFilter(t *testing.T) {
	mgr, _ := newTestManager(t)
	must(t, mgr.Create(context.Background(), &missionState{EntityIDF: "m-acme", PhaseF: "planning", OwnerOrgIDF: "acme"}))
	must(t, mgr.Create(context.Background(), &missionState{EntityIDF: "m-other", PhaseF: "planning", OwnerOrgIDF: "other"}))

	got, err := mgr.List(context.Background(), "mission", ListOptions{
		Match: map[string]any{"owner_org_id": "acme"},
	})
	if err != nil {
		t.Fatalf("List with Match: %v", err)
	}
	if len(got) != 1 || got[0].EntityID() != "m-acme" {
		t.Errorf("Match filter returned %d entries: %+v", len(got), got)
	}
}

func TestManager_List_RejectsUnknownMatchKey(t *testing.T) {
	mgr, _ := newTestManager(t)
	mustCreate(t, mgr, "x", "planning")
	_, err := mgr.List(context.Background(), "mission", ListOptions{
		Match: map[string]any{"nonexistent_field": "value"},
	})
	if err == nil {
		t.Fatal("Match with unknown key must error (typos should surface loudly)")
	}
	if !strings.Contains(err.Error(), "nonexistent_field") {
		t.Errorf("error should mention the unknown key %q, got %q", "nonexistent_field", err)
	}
}

func TestManager_List_LimitOffset(t *testing.T) {
	mgr, _ := newTestManager(t)
	for i := range 5 {
		mustCreate(t, mgr, fmt.Sprintf("lo-%d", i), "planning")
	}

	got, err := mgr.List(context.Background(), "mission", ListOptions{Limit: 2, Offset: 1})
	if err != nil {
		t.Fatalf("List with Limit/Offset: %v", err)
	}
	if len(got) != 2 {
		t.Errorf("Limit=2 should cap at 2 entries, got %d", len(got))
	}
}

func TestManager_List_UnknownWorkflow(t *testing.T) {
	mgr, _ := newTestManager(t)
	_, err := mgr.List(context.Background(), "never-registered", ListOptions{})
	if !errors.Is(err, ErrWorkflowNotRegistered) {
		t.Fatalf("List on unregistered workflow should error ErrWorkflowNotRegistered, got %v", err)
	}
}

// --- Watch ---

func TestManager_Watch_DeliversBootstrapSnapshot(t *testing.T) {
	mgr, _ := newTestManager(t)
	mustCreate(t, mgr, "w-1", "planning")
	mustCreate(t, mgr, "w-2", "planning")

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	ch, err := mgr.Watch(ctx, "mission")
	if err != nil {
		t.Fatalf("Watch: %v", err)
	}

	// Consume snapshot — bounded wait so a broken Watch doesn't
	// hang the test for the suite's default timeout.
	seen := make(map[string]bool)
	timeout := time.After(2 * time.Second)
	for len(seen) < 2 {
		select {
		case p, ok := <-ch:
			if !ok {
				t.Fatalf("Watch channel closed early; got %d/%d", len(seen), 2)
			}
			seen[p.EntityID()] = true
		case <-timeout:
			t.Fatalf("timed out waiting for snapshot; got %d/%d so far", len(seen), 2)
		}
	}
	if !seen["w-1"] || !seen["w-2"] {
		t.Errorf("snapshot missing entries: %+v", seen)
	}
}

func TestManager_Watch_StopsOnCtxCancel(t *testing.T) {
	mgr, _ := newTestManager(t)
	ctx, cancel := context.WithCancel(context.Background())
	ch, err := mgr.Watch(ctx, "mission")
	if err != nil {
		t.Fatalf("Watch: %v", err)
	}
	cancel()
	deadline := time.After(time.Second)
	for {
		select {
		case _, ok := <-ch:
			if !ok {
				return // closed — pass
			}
			// Drain pre-cancel snapshot items.
		case <-deadline:
			t.Fatal("Watch channel did not close within 1s after ctx cancel")
		}
	}
}

// --- History ---

func TestManager_History_HappyPath(t *testing.T) {
	mgr, _ := newTestManager(t)
	mustCreate(t, mgr, "h-1", "planning")
	must(t, mgr.Transition(context.Background(), "mission", "h-1", "flying", TransitionSourceRule, ""))
	must(t, mgr.Transition(context.Background(), "mission", "h-1", "capturing", TransitionSourceRule, ""))

	events, err := mgr.History(context.Background(), "mission", "h-1")
	if err != nil {
		t.Fatalf("History: %v", err)
	}
	// Expected: Create (""→"planning"), planning→flying, flying→capturing
	want := []struct{ from, to string }{
		{"", "planning"},
		{"planning", "flying"},
		{"flying", "capturing"},
	}
	if len(events) != len(want) {
		t.Fatalf("expected %d events, got %d: %+v", len(want), len(events), events)
	}
	for i, w := range want {
		if events[i].From != w.from || events[i].To != w.to {
			t.Errorf("event %d: got %q→%q, want %q→%q",
				i, events[i].From, events[i].To, w.from, w.to)
		}
	}
}

func TestManager_History_ReturnsNotFoundForMissingEntity(t *testing.T) {
	mgr, _ := newTestManager(t)
	_, err := mgr.History(context.Background(), "mission", "never-existed")
	if !errors.Is(err, ErrEntityNotFound) {
		t.Fatalf("History on missing entity should error ErrEntityNotFound, got %v", err)
	}
}

func TestManager_History_SkipsNonPhaseUpdates(t *testing.T) {
	// Update that mutates owner_org_id but not Phase should NOT
	// appear in History — History surfaces phase-transition
	// events specifically, not every state mutation.
	mgr, _ := newTestManager(t)
	mustCreate(t, mgr, "h-2", "planning")
	must(t, mgr.Update(context.Background(), "mission", "h-2", func(p Participant) error {
		p.(*missionState).OwnerOrgIDF = "first-update"
		return nil
	}))
	must(t, mgr.Update(context.Background(), "mission", "h-2", func(p Participant) error {
		p.(*missionState).OwnerOrgIDF = "second-update"
		return nil
	}))

	events, err := mgr.History(context.Background(), "mission", "h-2")
	if err != nil {
		t.Fatalf("History: %v", err)
	}
	// Only the Create event should be there — the two Updates
	// didn't change Phase.
	if len(events) != 1 {
		t.Fatalf("expected 1 event (Create only — Updates without phase change should be skipped), got %d: %+v",
			len(events), events)
	}
	if events[0].From != "" || events[0].To != "planning" {
		t.Errorf("Create event wrong: %+v", events[0])
	}
}

func TestManager_History_UnknownWorkflow(t *testing.T) {
	mgr, _ := newTestManager(t)
	_, err := mgr.History(context.Background(), "never-registered", "x")
	if !errors.Is(err, ErrWorkflowNotRegistered) {
		t.Fatalf("History on unregistered workflow should error ErrWorkflowNotRegistered, got %v", err)
	}
}

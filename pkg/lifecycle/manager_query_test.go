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

// --- UpdateFromOperator ---

func TestManager_UpdateFromOperator_HappyPath(t *testing.T) {
	mgr, _ := newTestManager(t)
	mustCreate(t, mgr, "ufo-1", "planning")

	err := mgr.UpdateFromOperator(context.Background(), "mission", "ufo-1", map[string]any{
		"owner_org_id": "newcorp",
	})
	if err != nil {
		t.Fatalf("UpdateFromOperator: %v", err)
	}
	got, _ := mgr.Get(context.Background(), "mission", "ufo-1")
	if got.(*missionState).OwnerOrgIDF != "newcorp" {
		t.Errorf("patch didn't apply: %+v", got)
	}
}

func TestManager_UpdateFromOperator_RejectsNonOperatorWritableField(t *testing.T) {
	// Phase is tagged readonly (implicit via lifecycle:"phase").
	// Patch must be rejected with ErrFieldNotOperatorWritable.
	mgr, _ := newTestManager(t)
	mustCreate(t, mgr, "ufo-2", "planning")

	err := mgr.UpdateFromOperator(context.Background(), "mission", "ufo-2", map[string]any{
		"phase": "flying", // not operator_writable
	})
	if !errors.Is(err, ErrFieldNotOperatorWritable) {
		t.Fatalf("operator patch of phase should error ErrFieldNotOperatorWritable, got %v", err)
	}
}

func TestManager_UpdateFromOperator_RejectsUnknownKey(t *testing.T) {
	mgr, _ := newTestManager(t)
	mustCreate(t, mgr, "ufo-3", "planning")

	err := mgr.UpdateFromOperator(context.Background(), "mission", "ufo-3", map[string]any{
		"frobnicator_count": 42,
	})
	if err == nil || !strings.Contains(err.Error(), "frobnicator_count") {
		t.Fatalf("unknown patch key should reject naming the key, got %v", err)
	}
}

func TestManager_UpdateFromOperator_RejectsTypeMismatch(t *testing.T) {
	mgr, _ := newTestManager(t)
	mustCreate(t, mgr, "ufo-4", "planning")

	err := mgr.UpdateFromOperator(context.Background(), "mission", "ufo-4", map[string]any{
		"owner_org_id": 42, // int, but field is string
	})
	if err == nil || !strings.Contains(err.Error(), "cannot assign") {
		t.Fatalf("type-mismatched patch should reject with assign-mention, got %v", err)
	}
}

func TestManager_UpdateFromOperator_EmptyPatchIsNoOp(t *testing.T) {
	mgr, _ := newTestManager(t)
	mustCreate(t, mgr, "ufo-5", "planning")

	if err := mgr.UpdateFromOperator(context.Background(), "mission", "ufo-5", nil); err != nil {
		t.Errorf("nil patch should be no-op, got %v", err)
	}
	if err := mgr.UpdateFromOperator(context.Background(), "mission", "ufo-5", map[string]any{}); err != nil {
		t.Errorf("empty patch should be no-op, got %v", err)
	}
}

// --- Children ---

func TestManager_Children_FindsByParentEntityID(t *testing.T) {
	mgr, _ := newTestManager(t)
	must(t, mgr.Create(context.Background(), &missionState{EntityIDF: "parent", PhaseF: "planning"}))
	must(t, mgr.Create(context.Background(), &missionState{EntityIDF: "child-1", PhaseF: "planning", ParentMissionF: "parent"}))
	must(t, mgr.Create(context.Background(), &missionState{EntityIDF: "child-2", PhaseF: "planning", ParentMissionF: "parent"}))
	must(t, mgr.Create(context.Background(), &missionState{EntityIDF: "orphan", PhaseF: "planning"}))

	got, err := mgr.Children(context.Background(), "parent")
	if err != nil {
		t.Fatalf("Children: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("expected 2 children, got %d: %+v", len(got), got)
	}
	seen := make(map[string]bool)
	for _, p := range got {
		seen[p.EntityID()] = true
	}
	for _, want := range []string{"child-1", "child-2"} {
		if !seen[want] {
			t.Errorf("Children missing %q", want)
		}
	}
}

func TestManager_Children_EmptyWhenNoChildren(t *testing.T) {
	mgr, _ := newTestManager(t)
	mustCreate(t, mgr, "alone", "planning")
	got, err := mgr.Children(context.Background(), "alone")
	if err != nil {
		t.Fatalf("Children: %v", err)
	}
	if len(got) != 0 {
		t.Errorf("expected 0 children, got %d", len(got))
	}
}

// --- Ancestors ---

func TestManager_Ancestors_WalksParentChain(t *testing.T) {
	mgr, _ := newTestManager(t)
	must(t, mgr.Create(context.Background(), &missionState{EntityIDF: "root", PhaseF: "planning"}))
	must(t, mgr.Create(context.Background(), &missionState{EntityIDF: "mid", PhaseF: "planning", ParentMissionF: "root"}))
	must(t, mgr.Create(context.Background(), &missionState{EntityIDF: "leaf", PhaseF: "planning", ParentMissionF: "mid"}))

	got, err := mgr.Ancestors(context.Background(), "leaf")
	if err != nil {
		t.Fatalf("Ancestors: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("expected 2 ancestors (mid, root), got %d: %+v", len(got), got)
	}
	if got[0].EntityID() != "mid" || got[1].EntityID() != "root" {
		t.Errorf("Ancestors order wrong; want [mid, root], got [%s, %s]",
			got[0].EntityID(), got[1].EntityID())
	}
}

func TestManager_Ancestors_EmptyForRoot(t *testing.T) {
	mgr, _ := newTestManager(t)
	mustCreate(t, mgr, "rootless", "planning")
	got, err := mgr.Ancestors(context.Background(), "rootless")
	if err != nil {
		t.Fatalf("Ancestors: %v", err)
	}
	if len(got) != 0 {
		t.Errorf("expected 0 ancestors for root entity, got %d", len(got))
	}
}

func TestManager_Ancestors_NotFoundForMissingEntity(t *testing.T) {
	mgr, _ := newTestManager(t)
	_, err := mgr.Ancestors(context.Background(), "never-existed")
	if !errors.Is(err, ErrEntityNotFound) {
		t.Fatalf("Ancestors on missing entity should error ErrEntityNotFound, got %v", err)
	}
}

func TestManager_Ancestors_TruncatesOnBrokenChain(t *testing.T) {
	// Parent reference points to an entity that doesn't exist —
	// walk truncates loudly via Warn log, returns what it found.
	mgr, _ := newTestManager(t)
	must(t, mgr.Create(context.Background(), &missionState{EntityIDF: "orphan", PhaseF: "planning", ParentMissionF: "missing-parent"}))
	got, err := mgr.Ancestors(context.Background(), "orphan")
	if err != nil {
		t.Fatalf("Ancestors with broken chain should not error, got %v", err)
	}
	if len(got) != 0 {
		t.Errorf("broken-chain walk should return empty, got %d entries", len(got))
	}
}

// --- WorkflowDefinition introspection ---

func TestManager_GetWorkflowDefinition_HappyPath(t *testing.T) {
	mgr, _ := newTestManager(t)
	def, err := mgr.GetWorkflowDefinition("mission")
	if err != nil {
		t.Fatalf("GetWorkflowDefinition: %v", err)
	}
	if def.Workflow != "mission" {
		t.Errorf("Workflow wrong: %q", def.Workflow)
	}
	if def.KVBucket != "MISSIONS" {
		t.Errorf("KVBucket wrong: %q", def.KVBucket)
	}
	if len(def.Transitions) != len(missionTransitions) {
		t.Errorf("Transitions count wrong: %d vs %d", len(def.Transitions), len(missionTransitions))
	}
	// Sorted output for operator-dashboard stability (PR 1a lock-in).
	wantFields := []string{"owner_org_id"}
	if len(def.OperatorWritableFields) != len(wantFields) {
		t.Errorf("OperatorWritableFields count wrong: got %v, want %v",
			def.OperatorWritableFields, wantFields)
	}
}

func TestManager_GetWorkflowDefinition_UnknownWorkflow(t *testing.T) {
	mgr, _ := newTestManager(t)
	_, err := mgr.GetWorkflowDefinition("never-registered")
	if !errors.Is(err, ErrWorkflowNotRegistered) {
		t.Fatalf("expected ErrWorkflowNotRegistered, got %v", err)
	}
}

func TestManager_ListWorkflows_SortedByName(t *testing.T) {
	// Register two workflows, verify sorted output.
	store := newKVMockStore(nil)
	mgr := newManagerForTest(nil, func(_ string) (kvStore, error) {
		return store, nil
	})
	must(t, mgr.Register("mission", missionFactory, missionTransitions))
	must(t, mgr.Register("alt", func() Participant { return &altParticipant{} },
		Transitions{"a": {"b"}, "b": {}}))

	defs := mgr.ListWorkflows()
	if len(defs) != 2 {
		t.Fatalf("expected 2 workflow defs, got %d", len(defs))
	}
	if defs[0].Workflow != "alt" || defs[1].Workflow != "mission" {
		t.Errorf("ListWorkflows should be sorted by name; got [%q, %q]",
			defs[0].Workflow, defs[1].Workflow)
	}
}

// altParticipant is a minimal second Participant impl for the
// ListWorkflows-sorted-output test. Shares the bucket with
// mission (deliberate — keeps the test fixture simple); apps
// in production would use separate buckets. Top-level fields
// only — parseStructTags doesn't recurse into nested struct
// types per the foundation contract.
type altParticipant struct {
	EntityIDF string `json:"entity_id" lifecycle:"id"`
	PhaseF    string `json:"phase" lifecycle:"phase"`
}

func (a *altParticipant) EntityID() string             { return a.EntityIDF }
func (a *altParticipant) Workflow() string             { return "alt" }
func (a *altParticipant) Phase() string                { return a.PhaseF }
func (a *altParticipant) IsTerminal() bool             { return a.PhaseF == "b" }
func (a *altParticipant) KVBucket() string             { return "MISSIONS" }
func (a *altParticipant) KVKey(entityID string) string { return "alt." + entityID }
func (a *altParticipant) ParentEntityID() string       { return "" }

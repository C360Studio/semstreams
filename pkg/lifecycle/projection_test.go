package lifecycle

import (
	"reflect"
	"testing"
	"time"

	"github.com/c360studio/semstreams/internal/semantictest"
	"github.com/c360studio/semstreams/message"
)

// fixtureMission is the projection-layer test fixture. Field tags
// exercise: id, phase+predicate, operator_writable+predicate,
// reference+predicate, readonly audit time.Time, and an undeclared
// field that projection should ignore.
type fixtureMission struct {
	ID         string    `json:"entity_id" lifecycle:"id"`
	PhaseF     string    `json:"phase" lifecycle:"phase,predicate=mission.lifecycle.phase"`
	OwnerOrgID string    `json:"owner_org_id,omitempty" lifecycle:"operator_writable,predicate=mission.identity.owner-org-id"`
	Note       string    `json:"note,omitempty" lifecycle:"operator_writable,predicate=mission.annotation.note"`
	DroneID    string    `json:"drone_id,omitempty" lifecycle:"reference,predicate=mission.assignment.drone"`
	LastAt     time.Time `json:"last_at,omitempty" lifecycle:"readonly,predicate=mission.transition.at"`
	Untagged   string    `json:"untagged,omitempty"`
}

func (m *fixtureMission) EntityID() string       { return m.ID }
func (m *fixtureMission) Workflow() string       { return "fixture" }
func (m *fixtureMission) Phase() string          { return m.PhaseF }
func (m *fixtureMission) IsTerminal() bool       { return false }
func (m *fixtureMission) ParentEntityID() string { return "" }

func TestParseSchemaType_RecognizesPredicateAndReference(t *testing.T) {
	t.Parallel()
	sm, err := parseSchemaType(reflect.TypeOf(fixtureMission{}))
	if err != nil {
		t.Fatalf("parseSchemaType: %v", err)
	}
	if sm.IDField == nil {
		t.Fatal("IDField missing")
	}
	if sm.PhaseField == nil || sm.PhaseField.Predicate != "mission.lifecycle.phase" {
		t.Fatalf("PhaseField predicate wrong: %+v", sm.PhaseField)
	}
	if _, ok := sm.FieldsByPredicate["mission.identity.owner-org-id"]; !ok {
		t.Fatal("owner_org_id missing from FieldsByPredicate")
	}
	ref := sm.FieldsByPredicate["mission.assignment.drone"]
	if ref == nil || !ref.IsReference || !ref.ReadOnly {
		t.Fatalf("reference field shape wrong: %+v", ref)
	}
	at := sm.FieldsByPredicate["mission.transition.at"]
	if at == nil || !at.ReadOnly {
		t.Fatalf("audit field shape wrong: %+v", at)
	}
}

func TestParseSchemaType_RejectsOperatorWritableWithoutPredicate(t *testing.T) {
	t.Parallel()
	type bad struct {
		ID    string `json:"id" lifecycle:"id"`
		Phase string `json:"phase" lifecycle:"phase,predicate=fixture.lifecycle.phase"`
		Owner string `json:"owner" lifecycle:"operator_writable"`
	}
	if _, err := parseSchemaType(reflect.TypeOf(bad{})); err == nil {
		t.Fatal("expected error for operator_writable without predicate, got nil")
	}
}

func TestParseSchemaType_RejectsDuplicatePredicate(t *testing.T) {
	t.Parallel()
	type bad struct {
		ID    string `json:"id" lifecycle:"id"`
		Phase string `json:"phase" lifecycle:"phase,predicate=fixture.lifecycle.phase"`
		A     string `json:"a" lifecycle:"operator_writable,predicate=fixture.value.x"`
		B     string `json:"b" lifecycle:"operator_writable,predicate=fixture.value.x"`
	}
	if _, err := parseSchemaType(reflect.TypeOf(bad{})); err == nil {
		t.Fatal("expected error for duplicate predicate, got nil")
	}
}

func TestParseSchemaTypeRejectsNoncanonicalTagPredicate(t *testing.T) {
	t.Parallel()
	type bad struct {
		ID    string `json:"id" lifecycle:"id"`
		Phase string `json:"phase" lifecycle:"phase,predicate=mission.phase"` // predicate-audit:invalid {"kind":"stored-predicate","value":"mission.phase","reason":"arity"}
	}
	if _, err := parseSchemaType(reflect.TypeOf(bad{})); err == nil {
		t.Fatal("expected noncanonical lifecycle tag predicate to be rejected")
	}
}

func TestProjectTriples_PopulatesScalarAndTime(t *testing.T) {
	t.Parallel()
	sm, err := parseSchemaType(reflect.TypeOf(fixtureMission{}))
	if err != nil {
		t.Fatalf("parseSchemaType: %v", err)
	}
	now := time.Date(2026, 5, 28, 16, 0, 0, 0, time.UTC)
	entityID := semantictest.EntityID(t, "test", "semstreams", "lifecycle", "projection", "mission", "one")
	droneID := semantictest.EntityID(t, "test", "semstreams", "lifecycle", "projection", "drone", "one")
	triples := []message.Triple{
		{Subject: entityID, Predicate: "mission.lifecycle.phase", Object: "flying"},
		{Subject: entityID, Predicate: "mission.identity.owner-org-id", Object: "acme"},
		{Subject: entityID, Predicate: "mission.transition.at", Object: now.Format(time.RFC3339Nano)},
		{Subject: entityID, Predicate: "mission.assignment.drone", Object: droneID},
		{Subject: entityID, Predicate: "some.other.predicate", Object: "ignored"},
	}
	target := &fixtureMission{}
	if err := projectTriples(sm, entityID, triples, target); err != nil {
		t.Fatalf("projectTriples: %v", err)
	}
	if target.ID != entityID {
		t.Errorf("ID not populated from entityID: %q", target.ID)
	}
	if target.PhaseF != "flying" {
		t.Errorf("Phase wrong: %q", target.PhaseF)
	}
	if target.OwnerOrgID != "acme" {
		t.Errorf("OwnerOrgID wrong: %q", target.OwnerOrgID)
	}
	if !target.LastAt.Equal(now) {
		t.Errorf("LastAt wrong: %v vs %v", target.LastAt, now)
	}
	if target.DroneID != droneID {
		t.Errorf("DroneID wrong: %q", target.DroneID)
	}
}

func TestProjectStructToTriples_SkipsReadonlyAndID(t *testing.T) {
	t.Parallel()
	sm, err := parseSchemaType(reflect.TypeOf(fixtureMission{}))
	if err != nil {
		t.Fatalf("parseSchemaType: %v", err)
	}
	src := &fixtureMission{
		ID:         semantictest.EntityID(t, "test", "semstreams", "lifecycle", "projection", "mission", "two"),
		PhaseF:     "planning",
		OwnerOrgID: "acme",
		Note:       "test",
		DroneID:    semantictest.EntityID(t, "test", "semstreams", "lifecycle", "projection", "drone", "two"),
		LastAt:     time.Now(),
	}
	emitted := projectStructToTriples(sm, src.ID, src)
	gotPreds := map[string]bool{}
	for _, tr := range emitted {
		gotPreds[tr.Predicate] = true
	}
	if !gotPreds["mission.lifecycle.phase"] {
		t.Error("mission.lifecycle.phase missing from initial triples")
	}
	if !gotPreds["mission.identity.owner-org-id"] {
		t.Error("mission.identity.owner-org-id missing from initial triples")
	}
	if gotPreds["mission.assignment.drone"] {
		t.Error("reference field should not be emitted from struct projection")
	}
	if gotPreds["mission.transition.at"] {
		t.Error("readonly audit field should not be emitted from struct projection")
	}
}

func TestProjectPatchToTriples_RejectsNonOperatorWritable(t *testing.T) {
	t.Parallel()
	sm, err := parseSchemaType(reflect.TypeOf(fixtureMission{}))
	if err != nil {
		t.Fatalf("parseSchemaType: %v", err)
	}
	_, _, err = projectPatchToTriples(sm, "x", map[string]any{"phase": "flying"})
	if err == nil {
		t.Fatal("expected error patching phase field, got nil")
	}
	adds, removes, err := projectPatchToTriples(sm, "x", map[string]any{
		"owner_org_id": "acme",
		"note":         nil,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(adds) != 1 || adds[0].Predicate != "mission.identity.owner-org-id" || adds[0].Object != "acme" {
		t.Errorf("adds wrong: %+v", adds)
	}
	if len(removes) != 1 || removes[0] != "mission.annotation.note" {
		t.Errorf("removes wrong: %+v", removes)
	}
}

func TestMatchPattern(t *testing.T) {
	t.Parallel()
	tests := []struct {
		pattern, id string
		want        bool
	}{
		{"*.lifecycle.gcs.mission.*", "c360.lifecycle.gcs.mission.001", true}, // 5 vs 5 parts
		{"*.lifecycle.gcs.mission.*", "c360.lifecycle.gcs.mission.001.extra", false},
		{"*.*.gcs.lifecycle.mission.*", "c360.x.gcs.lifecycle.mission.001", true}, // 6 vs 6
		{"*.lifecycle.gcs.mission.*.*", "c360.lifecycle.gcs.mission.x.001", true},
		{"*.lifecycle.gcs.mission.*.*", "c360.lifecycle.gcs.drone.x.001", false},
		{"c360.*.*.*.*.001", "c360.a.b.c.d.001", true},
		{"c360.*.*.*.*.001", "acme.a.b.c.d.001", false},
		{"*", "anything", true},
	}
	for _, tt := range tests {
		if got := matchPattern(tt.pattern, tt.id); got != tt.want {
			t.Errorf("matchPattern(%q, %q) = %v, want %v", tt.pattern, tt.id, got, tt.want)
		}
	}
}

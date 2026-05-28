package lifecycle

import (
	"reflect"
	"testing"
	"time"

	"github.com/c360studio/semstreams/message"
)

// fixtureMission is the projection-layer test fixture. Field tags
// exercise: id, phase+predicate, operator_writable+predicate,
// reference+predicate, readonly audit time.Time, and an undeclared
// field that projection should ignore.
type fixtureMission struct {
	ID         string    `json:"entity_id" lifecycle:"id"`
	PhaseF     string    `json:"phase" lifecycle:"phase,predicate=mission.phase"`
	OwnerOrgID string    `json:"owner_org_id,omitempty" lifecycle:"operator_writable,predicate=mission.owner_org_id"`
	Note       string    `json:"note,omitempty" lifecycle:"operator_writable,predicate=mission.note"`
	DroneID    string    `json:"drone_id,omitempty" lifecycle:"reference,predicate=mission.assigned_drone"`
	LastAt     time.Time `json:"last_at,omitempty" lifecycle:"readonly,predicate=mission.last_transition_at"`
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
	if sm.PhaseField == nil || sm.PhaseField.Predicate != "mission.phase" {
		t.Fatalf("PhaseField predicate wrong: %+v", sm.PhaseField)
	}
	if _, ok := sm.FieldsByPredicate["mission.owner_org_id"]; !ok {
		t.Fatal("owner_org_id missing from FieldsByPredicate")
	}
	ref := sm.FieldsByPredicate["mission.assigned_drone"]
	if ref == nil || !ref.IsReference || !ref.ReadOnly {
		t.Fatalf("reference field shape wrong: %+v", ref)
	}
	at := sm.FieldsByPredicate["mission.last_transition_at"]
	if at == nil || !at.ReadOnly {
		t.Fatalf("audit field shape wrong: %+v", at)
	}
}

func TestParseSchemaType_RejectsOperatorWritableWithoutPredicate(t *testing.T) {
	t.Parallel()
	type bad struct {
		ID    string `json:"id" lifecycle:"id"`
		Phase string `json:"phase" lifecycle:"phase,predicate=p.phase"`
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
		Phase string `json:"phase" lifecycle:"phase,predicate=p.phase"`
		A     string `json:"a" lifecycle:"operator_writable,predicate=p.x"`
		B     string `json:"b" lifecycle:"operator_writable,predicate=p.x"`
	}
	if _, err := parseSchemaType(reflect.TypeOf(bad{})); err == nil {
		t.Fatal("expected error for duplicate predicate, got nil")
	}
}

func TestProjectTriples_PopulatesScalarAndTime(t *testing.T) {
	t.Parallel()
	sm, err := parseSchemaType(reflect.TypeOf(fixtureMission{}))
	if err != nil {
		t.Fatalf("parseSchemaType: %v", err)
	}
	now := time.Date(2026, 5, 28, 16, 0, 0, 0, time.UTC)
	triples := []message.Triple{
		{Subject: "x", Predicate: "mission.phase", Object: "flying"},
		{Subject: "x", Predicate: "mission.owner_org_id", Object: "acme"},
		{Subject: "x", Predicate: "mission.last_transition_at", Object: now.Format(time.RFC3339Nano)},
		{Subject: "x", Predicate: "mission.assigned_drone", Object: "c360.x.fleet.x.drone.001"},
		{Subject: "x", Predicate: "some.other.predicate", Object: "ignored"},
	}
	target := &fixtureMission{}
	if err := projectTriples(sm, "x.lifecycle.gcs.mission.001", triples, target); err != nil {
		t.Fatalf("projectTriples: %v", err)
	}
	if target.ID != "x.lifecycle.gcs.mission.001" {
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
	if target.DroneID != "c360.x.fleet.x.drone.001" {
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
		ID:         "x.lifecycle.gcs.mission.001",
		PhaseF:     "planning",
		OwnerOrgID: "acme",
		Note:       "test",
		DroneID:    "should-not-emit",
		LastAt:     time.Now(),
	}
	emitted := projectStructToTriples(sm, src.ID, src)
	gotPreds := map[string]bool{}
	for _, tr := range emitted {
		gotPreds[tr.Predicate] = true
	}
	if !gotPreds["mission.phase"] {
		t.Error("mission.phase missing from initial triples")
	}
	if !gotPreds["mission.owner_org_id"] {
		t.Error("mission.owner_org_id missing from initial triples")
	}
	if gotPreds["mission.assigned_drone"] {
		t.Error("reference field should not be emitted from struct projection")
	}
	if gotPreds["mission.last_transition_at"] {
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
	if len(adds) != 1 || adds[0].Predicate != "mission.owner_org_id" || adds[0].Object != "acme" {
		t.Errorf("adds wrong: %+v", adds)
	}
	if len(removes) != 1 || removes[0] != "mission.note" {
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
		{"*.*.lifecycle.gcs.mission.*", "c360.x.lifecycle.gcs.mission.001", true}, // 6 vs 6
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

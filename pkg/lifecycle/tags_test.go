package lifecycle

import (
	"errors"
	"reflect"
	"testing"
)

// canonicalState is the ADR-047 example state struct used to exercise
// the full happy-path of struct-tag parsing. Mirrors the shape from
// doc.go's usage example so failures here also catch documentation
// drift.
type canonicalState struct {
	EntityIDF  string `json:"entity_id"    lifecycle:"id"`
	PhaseF     string `json:"phase"        lifecycle:"phase,readonly"`
	OwnerOrgID string `json:"owner_org_id" lifecycle:"operator_writable"`
	DeployedTo string `json:"deployed_to,omitempty" lifecycle:"operator_writable,indexable"`
	Internal   string `json:"internal,omitempty"` // no tag = default-deny
	unexported string //nolint:unused // intentionally present for the parser-skip test
}

func TestParseStructTags_HappyPath(t *testing.T) {
	sm, err := parseStructTags(reflect.ValueOf(&canonicalState{}))
	if err != nil {
		t.Fatalf("canonical struct must parse, got %v", err)
	}

	if sm.IDField == nil || sm.IDField.JSONName != "entity_id" {
		t.Errorf("IDField wrong: %+v", sm.IDField)
	}
	if sm.PhaseField == nil || sm.PhaseField.JSONName != "phase" {
		t.Errorf("PhaseField wrong: %+v", sm.PhaseField)
	}
	if !sm.PhaseField.ReadOnly {
		t.Error("PhaseField should be ReadOnly (implicit via id/phase tags)")
	}

	owner := sm.FieldsByJSONName["owner_org_id"]
	if owner == nil || !owner.OperatorWritable || owner.Indexable {
		t.Errorf("owner_org_id should be OperatorWritable + not Indexable, got %+v", owner)
	}

	deployed := sm.FieldsByJSONName["deployed_to"]
	if deployed == nil || !deployed.OperatorWritable || !deployed.Indexable {
		t.Errorf("deployed_to should be OperatorWritable + Indexable, got %+v", deployed)
	}

	internal := sm.FieldsByJSONName["internal"]
	if internal == nil || internal.OperatorWritable {
		t.Errorf("internal (no tag) must default-deny OperatorWritable, got %+v", internal)
	}

	if _, ok := sm.FieldsByJSONName["unexported"]; ok {
		t.Error("unexported fields must be skipped — operator JSON can't reach them")
	}
}

func TestParseStructTags_OperatorWritableFields_Sorted(t *testing.T) {
	sm, err := parseStructTags(reflect.ValueOf(&canonicalState{}))
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	got := sm.OperatorWritableJSONNames()
	want := []string{"deployed_to", "owner_org_id"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("OperatorWritableJSONNames = %v, want %v (sorted)", got, want)
	}
}

func TestParseStructTags_MissingID(t *testing.T) {
	type noID struct {
		Phase string `json:"phase" lifecycle:"phase"`
	}
	_, err := parseStructTags(reflect.ValueOf(&noID{}))
	if !errors.Is(err, ErrMissingIDField) {
		t.Fatalf("expected ErrMissingIDField, got %v", err)
	}
}

func TestParseStructTags_MissingPhase(t *testing.T) {
	type noPhase struct {
		EntityID string `json:"entity_id" lifecycle:"id"`
	}
	_, err := parseStructTags(reflect.ValueOf(&noPhase{}))
	if !errors.Is(err, ErrMissingPhaseField) {
		t.Fatalf("expected ErrMissingPhaseField, got %v", err)
	}
}

func TestParseStructTags_DuplicateID(t *testing.T) {
	type doubleID struct {
		A     string `json:"a" lifecycle:"id"`
		B     string `json:"b" lifecycle:"id"`
		Phase string `json:"phase" lifecycle:"phase"`
	}
	_, err := parseStructTags(reflect.ValueOf(&doubleID{}))
	if err == nil || !contains(err.Error(), "multiple fields tagged") {
		t.Fatalf("expected duplicate-id error, got %v", err)
	}
}

func TestParseStructTags_DuplicatePhase(t *testing.T) {
	type doublePhase struct {
		ID string `json:"id" lifecycle:"id"`
		A  string `json:"a" lifecycle:"phase"`
		B  string `json:"b" lifecycle:"phase"`
	}
	_, err := parseStructTags(reflect.ValueOf(&doublePhase{}))
	if err == nil || !contains(err.Error(), "multiple fields tagged") {
		t.Fatalf("expected duplicate-phase error, got %v", err)
	}
}

func TestParseStructTags_UnknownTagValue(t *testing.T) {
	type weirdTag struct {
		ID    string `json:"id" lifecycle:"id"`
		Phase string `json:"phase" lifecycle:"phase"`
		Junk  string `json:"junk" lifecycle:"frobnicate"`
	}
	_, err := parseStructTags(reflect.ValueOf(&weirdTag{}))
	if err == nil || !contains(err.Error(), "frobnicate") {
		t.Fatalf("expected unknown-tag-value error mentioning 'frobnicate', got %v", err)
	}
}

func TestParseStructTags_IDPlusOperatorWritableRejected(t *testing.T) {
	type idPlusWritable struct {
		ID    string `json:"id" lifecycle:"id,operator_writable"`
		Phase string `json:"phase" lifecycle:"phase"`
	}
	_, err := parseStructTags(reflect.ValueOf(&idPlusWritable{}))
	if err == nil {
		t.Fatal("id + operator_writable must be rejected as a wiring contradiction")
	}
}

func TestParseStructTags_PhasePlusOperatorWritableRejected(t *testing.T) {
	type phasePlusWritable struct {
		ID    string `json:"id" lifecycle:"id"`
		Phase string `json:"phase" lifecycle:"phase,operator_writable"`
	}
	_, err := parseStructTags(reflect.ValueOf(&phasePlusWritable{}))
	if err == nil {
		t.Fatal("phase + operator_writable must be rejected as a wiring contradiction")
	}
}

func TestParseStructTags_JSONNameFallbacks(t *testing.T) {
	// Field with no json tag → lowercased Go name.
	// Field with `json:"-"` → still indexable via lowercased Go name
	//   (operator-patch JSON can't reach it but Match still can).
	// Field with `json:",omitempty"` (empty name) → lowercased Go name.
	type tricky struct {
		ID         string `json:"id" lifecycle:"id"`
		Phase      string `json:"phase" lifecycle:"phase"`
		NoTag      string
		HyphenName string `json:"-"`
		EmptyName  string `json:",omitempty"`
	}
	sm, err := parseStructTags(reflect.ValueOf(&tricky{}))
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	for _, want := range []string{"notag", "hyphenname", "emptyname"} {
		if _, ok := sm.FieldsByJSONName[want]; !ok {
			t.Errorf("expected JSON name fallback %q in FieldsByJSONName, missing", want)
		}
	}
}

func TestParseStructTags_RejectsNonStruct(t *testing.T) {
	x := 42
	_, err := parseStructTags(reflect.ValueOf(&x))
	if err == nil {
		t.Fatal("non-struct must be rejected")
	}
}

package lifecycle

import (
	"context"
	"errors"
	"reflect"
	"slices"
	"testing"

	"github.com/c360studio/semstreams/pkg/ownership"
)

// deriveOwnerRegistration is the load-bearing seam of the ADR-056 Decision 5
// embed: it must claim EXACTLY what the Manager authors, split by write mode,
// and the result must pass ownership.Registration.Validate (or RegisterOwner
// would reject a real workflow at boot). These are pure-logic assertions — no
// NATS — over the same fixture the projection/manager tests use.
func TestDeriveOwnerRegistration_SplitsByModeAndExcludesUnauthored(t *testing.T) {
	t.Parallel()
	w := lifecycle{}.fixtureWorkflow()
	meta, err := parseSchemaType(w.Schema)
	if err != nil {
		t.Fatalf("parseSchemaType: %v", err)
	}

	regn := deriveOwnerRegistration(w, meta)

	if regn.Owner != "fixture" {
		t.Errorf("registration owner = %q, want %q (the workflow Name)", regn.Owner, "fixture")
	}
	// A real workflow must derive a VALID registration, or RegisterOwner fails boot.
	if err := regn.Validate(); err != nil {
		t.Fatalf("derived registration must be valid: %v", err)
	}

	var casClaim, replaceClaim *ownership.OwnerClaim
	for i := range regn.Claims {
		c := &regn.Claims[i]
		if c.Pattern != w.EntityIDPattern {
			t.Errorf("claim pattern = %q, want %q", c.Pattern, w.EntityIDPattern)
		}
		if c.Owner != "fixture" {
			t.Errorf("claim owner = %q, want fixture", c.Owner)
		}
		switch c.Mode {
		case ownership.ModeCASTransition:
			casClaim = c
		case ownership.ModeReplaceOwned:
			replaceClaim = c
		default:
			t.Errorf("unexpected claim mode %q", c.Mode)
		}
	}

	if casClaim == nil {
		t.Fatal("expected a cas-transition claim for the phase predicate")
	}
	if got := casClaim.Predicates; !slices.Equal(got, []string{"mission.phase"}) {
		t.Errorf("cas claim predicates = %v, want [mission.phase]", got)
	}

	if replaceClaim == nil {
		t.Fatal("expected a replace-owned claim for audit + writable fields")
	}
	// Audit predicates (4) + writable projected fields (owner_org_id, note).
	// EXCLUDED: mission.phase (claim 1), mission.assigned_drone (reference),
	// and the bare readonly LastAt predicate (it equals AuditPredicates.At, so
	// it survives only via the audit add, not the field — and is not duplicated).
	want := []string{
		"mission.last_transition_at",
		"mission.last_transition_from",
		"mission.last_transition_note",
		"mission.last_transition_source",
		"mission.note",
		"mission.owner_org_id",
	}
	if got := replaceClaim.Predicates; !slices.Equal(got, want) {
		t.Errorf("replace claim predicates =\n  %v\nwant (sorted)\n  %v", got, want)
	}

	// The reference predicate must NOT be claimed — the Manager never authors it.
	for _, c := range regn.Claims {
		if slices.Contains(c.Predicates, "mission.assigned_drone") {
			t.Errorf("reference predicate mission.assigned_drone must not be claimed (mode %q)", c.Mode)
		}
	}
}

// A phase-only workflow (no audit, no writable scalar fields) derives a single
// cas-transition claim and no empty replace-owned claim (OwnerClaim.Validate
// rejects an empty predicate set).
func TestDeriveOwnerRegistration_PhaseOnlyWorkflowYieldsSingleClaim(t *testing.T) {
	t.Parallel()
	w := Workflow{
		Name:            "phaseonly",
		EntityIDPattern: "*.*.lifecycle.gcs.phaseonly.*",
		Transitions:     Transitions{"a": {"b"}, "b": {}},
		PhasePredicate:  "phaseonly.phase",
		Schema:          reflect.TypeOf(phaseOnlyState{}),
	}
	meta, err := parseSchemaType(w.Schema)
	if err != nil {
		t.Fatalf("parseSchemaType: %v", err)
	}

	regn := deriveOwnerRegistration(w, meta)
	if err := regn.Validate(); err != nil {
		t.Fatalf("derived registration must be valid: %v", err)
	}
	if len(regn.Claims) != 1 {
		t.Fatalf("phase-only workflow: got %d claims, want 1", len(regn.Claims))
	}
	if regn.Claims[0].Mode != ownership.ModeCASTransition {
		t.Errorf("sole claim mode = %q, want cas-transition", regn.Claims[0].Mode)
	}
}

type phaseOnlyState struct {
	ID     string `json:"entity_id" lifecycle:"id"`
	PhaseF string `json:"phase" lifecycle:"phase,predicate=phaseonly.phase"`
}

func (s *phaseOnlyState) EntityID() string       { return s.ID }
func (s *phaseOnlyState) Workflow() string       { return "phaseonly" }
func (s *phaseOnlyState) Phase() string          { return s.PhaseF }
func (s *phaseOnlyState) IsTerminal() bool       { return false }
func (s *phaseOnlyState) ParentEntityID() string { return "" }

// With no registry attached (the default, every nil-client / unmigrated-deploy
// path), Register is a pure no-op on the ownership axis: it still validates,
// commits the local registration, and never touches NATS. This is the
// graceful-skip contract — a deploy that did not call AttachOwnership behaves
// exactly as it did pre-ADR-056.
func TestRegister_NoOwnershipAttached_IsNoOp(t *testing.T) {
	t.Parallel()
	// nil client, nil emitter, nil bucket — a Manager that cannot do any NATS
	// I/O. If Register tried to register ownership it would panic on the nil
	// registry; the no-op path must leave it untouched.
	mgr := newManagerForTest(nil, nil, nil)
	if mgr.ownerRegistry != nil {
		t.Fatal("a Manager built without AttachOwnership must have a nil ownerRegistry")
	}
	if err := mgr.Register(lifecycle{}.fixtureWorkflow()); err != nil {
		t.Fatalf("Register with no ownership must succeed: %v", err)
	}
	if _, err := mgr.lookupByWorkflow("fixture"); err != nil {
		t.Fatalf("workflow must be locally registered: %v", err)
	}
}

// AttachOwnership(nil) is a no-op — callers may pass the result of EnsureBuckets
// unconditionally, and a resourceless deploy that skipped bucket bootstrap
// passes a nil registry.
func TestAttachOwnership_NilRegistryIsNoOp(t *testing.T) {
	t.Parallel()
	mgr := newManagerForTest(nil, nil, nil)
	mgr.AttachOwnership(context.Background(), nil)
	if mgr.ownerRegistry != nil || mgr.heartbeater != nil {
		t.Fatal("AttachOwnership(nil) must leave the Manager unwired")
	}
}

// Guard the sentinel the Manager branches on for observe-only vs fatal, so a
// future errors.go refactor that renames it trips here rather than silently
// turning a cross-owner overlap into a fatal boot (or vice-versa).
func TestOwnershipSentinelsExist(t *testing.T) {
	t.Parallel()
	if !errors.Is(&ownership.OverlapError{}, ownership.ErrOwnershipOverlap) {
		t.Error("OverlapError must match ErrOwnershipOverlap via errors.Is")
	}
}

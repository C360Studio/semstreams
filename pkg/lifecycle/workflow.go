// Package lifecycle — Workflow schema declaration types (ADR-049).
//
// A Workflow declaration is the contract a registered workflow type
// satisfies with the harness: identity pattern, phases, transitions,
// the phase predicate, operator-writable predicates, audit predicates,
// owned child workflows, and reference predicates. Apps construct one
// Workflow value per workflow type and pass it to Manager.Register.
//
// Per ADR-049 the harness is a schema-and-discipline layer over
// ENTITY_STATES — Manager does NOT own a private KV bucket. The
// Workflow struct declares what triples belong to the workflow; the
// projection layer maps those triples into / out of the app's
// Participant struct.
package lifecycle

import (
	"fmt"
	"reflect"
	"strings"

	"github.com/c360studio/semstreams/vocabulary"
)

// Workflow declares a workflow type to Manager.Register. All fields
// except Name + Phases + Transitions + PhasePredicate + Schema are
// optional — the harness degrades gracefully when richer schema
// information is absent (composed gateway view falls back to the
// bare entity; History stamps a synthetic source if no AuditPredicates
// are declared).
type Workflow struct {
	// Name is the workflow type identifier (e.g. "mission",
	// "csapi-system"). Must match what the Participant's Workflow()
	// method returns. Required.
	Name string

	// EntityIDPattern is the 6-part federated-ID glob pattern that
	// identifies entities of this workflow type. Used by List, Watch,
	// and the cross-workflow LookupByEntityID path to filter
	// ENTITY_STATES by entity ID. Example: "*.lifecycle.gcs.mission.*"
	// — the org and instance segments are wildcarded; the platform /
	// domain / system / type segments are pinned. Required.
	EntityIDPattern string

	// Phases lists the workflow's declared lifecycle phases.
	// Informational — the authoritative phase graph is Transitions.
	// Optional but recommended for operator dashboards rendering
	// state-machine diagrams.
	Phases []string

	// Transitions declares the phase graph (which phases can move to
	// which). Terminal phases declare an empty out-edge list.
	// Validated against Transitions.Validate at Register time.
	// Required.
	Transitions Transitions

	// PhasePredicate is the triple predicate that carries the
	// entity's current phase (e.g. "mission.lifecycle.phase"). Manager.Transition
	// writes this predicate atomically with the audit predicates;
	// projection reads it to populate the Participant struct's
	// `lifecycle:"phase"` field. Required.
	PhasePredicate string

	// Schema is the reflect.Type of the app's Participant struct.
	// Used by the projection layer to allocate fresh instances
	// (reflect.New) and to populate fields by index. Required;
	// typically `reflect.TypeOf(MissionState{})`.
	Schema reflect.Type

	// OperatorWritablePredicates lists triple predicates that the
	// operator API (POST /workflows/{type}/{id}/state) may patch via
	// Manager.UpdateFromOperator. Default-deny: predicates not in
	// this list are protected. Optional — omit for workflows that
	// have no operator-writable surface (e.g. fully rule-driven).
	OperatorWritablePredicates []string

	// AuditPredicates declares the framework-stamped audit-trail
	// predicates. Manager.Transition writes these atomically with
	// the phase change; Manager.History reads them back to
	// reconstruct TransitionEvent records with source attribution.
	// Optional — omit to disable audit-triple stamping (History
	// then synthesizes Triggered=framework from KV revision
	// timestamps only).
	AuditPredicates AuditSpec

	// ChildWorkflows declares workflow types that this workflow OWNS
	// as children. Each ChildSpec declares the LinkPredicate that
	// establishes the parent→child relationship via a triple on the
	// parent. Optional — omit for workflows without owned children.
	ChildWorkflows []ChildSpec

	// ReferencePredicates declares predicates linking this workflow
	// to OTHER entities WITHOUT lifecycle ownership (the target has
	// its own lifetime). Manager.References returns light stubs for
	// these; operator dashboards render them as cross-workflow
	// hyperlinks. Optional.
	ReferencePredicates []ReferenceSpec
}

// AuditSpec declares the four framework-stamped audit predicates
// Manager.Transition writes on every phase change. All four are
// optional — omit a field to skip stamping that aspect.
//
// On every successful transition, Manager.Transition stamps:
//
//	Source : transition source (rule | operator | component | framework)
//	At     : RFC3339Nano timestamp of the write
//	From   : the previous phase (the phase the entity transitioned out of)
//	Note   : the optional free-text note carried in the Transition call
//
// History reads these back at each KV revision to populate
// TransitionEvent records. With no AuditPredicates declared,
// History still surfaces phase changes — Triggered defaults to
// framework, From is reconstructed from the previous revision's
// PhasePredicate, Note is empty.
type AuditSpec struct {
	Source string // typically "<workflow>.last_transition_source"
	At     string // typically "<workflow>.last_transition_at"
	From   string // typically "<workflow>.last_transition_from"
	Note   string // typically "<workflow>.last_transition_note"
}

// ChildSpec declares a workflow type owned as a child of the
// declaring workflow. LinkPredicate is the triple predicate that
// carries the child's EntityID as its Object — Manager.Children
// enumerates triples matching this predicate on the parent and
// loads each child via the child workflow's projection.
type ChildSpec struct {
	// Workflow is the registered child workflow type name. Must
	// match the Name of a separately-registered Workflow. Children
	// is best-effort across the registered set: if a workflow named
	// in ChildSpec.Workflow isn't registered at Children call time,
	// the child is skipped with a Warn log rather than failing the
	// whole call (operator dashboards render missing-child
	// indicators).
	Workflow string

	// LinkPredicate is the triple predicate establishing the
	// parent→child link. Cardinality is many (a parent may own
	// multiple children of the same type); Manager.Children
	// enumerates every triple with this predicate.
	LinkPredicate string
}

// ReferenceSpec declares a predicate linking this workflow to
// another entity WITHOUT lifecycle ownership. Manager.References
// projects matching triples as ReferenceStub records — light
// pointers, not full entity loads.
type ReferenceSpec struct {
	// Predicate is the triple predicate carrying the target
	// entity's EntityID as Object.
	Predicate string

	// TargetPattern is an optional 6-part glob narrowing what
	// shape of entity-ID is expected as the target (e.g.
	// "*.fleet.drone.*"). Informational today — Manager.References
	// uses it to determine whether the target is itself
	// lifecycle-managed (matches some registered workflow's
	// EntityIDPattern) so the stub can carry the target's phase.
	// Optional.
	TargetPattern string
}

// validate checks the Workflow declaration for structural correctness
// at Register time. Catches typos and missing required fields before
// the workflow goes live. All failures wrap ErrInvalidWorkflow —
// ErrWorkflowNotRegistered is reserved for lookup-time misses on the
// Manager surface.
func (w *Workflow) validate() error {
	if w.Name == "" {
		return fmt.Errorf("%w: workflow Name is required", ErrInvalidWorkflow)
	}
	if w.EntityIDPattern == "" {
		return fmt.Errorf("%w: workflow %q EntityIDPattern is required",
			ErrInvalidWorkflow, w.Name)
	}
	// EntityIDPattern must be 6-segment to match the federated EntityID
	// contract (org.platform.domain.system.type.instance — pkg/types.EntityID
	// + CLAUDE.md). Per ADR-049 reviewer B5 a 5-segment pattern silently
	// fails to match any real entity ID; the prior code hit this on the
	// e2e mission pattern. Fail loudly at Register time.
	if segs := strings.Split(w.EntityIDPattern, "."); len(segs) != 6 {
		return fmt.Errorf("%w: workflow %q EntityIDPattern %q has %d segments (expected 6 per federated EntityID contract org.platform.domain.system.type.instance)",
			ErrInvalidWorkflow, w.Name, w.EntityIDPattern, len(segs))
	}
	if w.PhasePredicate == "" {
		return fmt.Errorf("%w: workflow %q PhasePredicate is required",
			ErrInvalidWorkflow, w.Name)
	}
	if err := validateWorkflowPredicate(w.Name, "PhasePredicate", w.PhasePredicate); err != nil {
		return err
	}
	if w.Schema == nil {
		return fmt.Errorf("%w: workflow %q Schema is required",
			ErrInvalidWorkflow, w.Name)
	}
	if err := w.Transitions.Validate(); err != nil {
		return err
	}
	for _, declared := range []struct {
		field     string
		predicate string
	}{
		{"AuditPredicates.Source", w.AuditPredicates.Source},
		{"AuditPredicates.At", w.AuditPredicates.At},
		{"AuditPredicates.From", w.AuditPredicates.From},
		{"AuditPredicates.Note", w.AuditPredicates.Note},
	} {
		if declared.predicate == "" {
			continue
		}
		if err := validateWorkflowPredicate(w.Name, declared.field, declared.predicate); err != nil {
			return err
		}
	}
	for i, predicate := range w.OperatorWritablePredicates {
		if err := validateWorkflowPredicate(w.Name,
			fmt.Sprintf("OperatorWritablePredicates[%d]", i), predicate); err != nil {
			return err
		}
	}
	// Catch obvious duplicates in ChildWorkflows that would silently
	// make the second registration shadow the first.
	seen := map[string]bool{}
	for _, ch := range w.ChildWorkflows {
		if ch.Workflow == "" {
			return fmt.Errorf("%w: workflow %q has ChildWorkflows entry with empty Workflow",
				ErrInvalidWorkflow, w.Name)
		}
		if ch.LinkPredicate == "" {
			return fmt.Errorf("%w: workflow %q ChildSpec for %q has empty LinkPredicate",
				ErrInvalidWorkflow, w.Name, ch.Workflow)
		}
		if err := validateWorkflowPredicate(w.Name,
			fmt.Sprintf("ChildWorkflows[%q].LinkPredicate", ch.Workflow), ch.LinkPredicate); err != nil {
			return err
		}
		if seen[ch.LinkPredicate] {
			return fmt.Errorf("%w: workflow %q has duplicate ChildSpec LinkPredicate %q",
				ErrInvalidWorkflow, w.Name, ch.LinkPredicate)
		}
		seen[ch.LinkPredicate] = true
	}
	for i, ref := range w.ReferencePredicates {
		if ref.Predicate == "" {
			return fmt.Errorf("%w: workflow %q has ReferenceSpec with empty Predicate",
				ErrInvalidWorkflow, w.Name)
		}
		if err := validateWorkflowPredicate(w.Name,
			fmt.Sprintf("ReferencePredicates[%d].Predicate", i), ref.Predicate); err != nil {
			return err
		}
	}
	return nil
}

func validateWorkflowPredicate(workflow, field, predicate string) error {
	if err := vocabulary.RequireDeclaredPredicate(predicate); err != nil {
		return fmt.Errorf("%w: workflow %q %s: %w",
			ErrInvalidWorkflow, workflow, field, err)
	}
	return nil
}

// validateDisjointness asserts that no single-valued predicate (phase, audit,
// or a scalar projection field) collides with a cardinality-many predicate
// (a ChildSpec.LinkPredicate or a ReferenceSpec.Predicate). It runs at Register
// time AFTER the Schema is parsed, because the projection-field predicates live
// in the parsed structMeta, not on the Workflow struct.
//
// Why this matters (gh#234): Manager.Transition replaces rather than appends —
// it writes RemoveTriples for every predicate in the transition delta (phase +
// audit). For a VALID schema that is correct (all delta predicates are
// single-valued). But if a delta predicate ALSO names a child-link or reference
// predicate, the Transition would delete those many-cardinality triples — silent
// data loss. No valid schema regresses; this fails the malformed schema loudly
// at Register rather than dangerously at the first Transition.
func (w *Workflow) validateDisjointness(meta *structMeta) error {
	// Cardinality-many predicates: child links and references. A predicate
	// declared as BOTH (child-link and reference) is itself a collision —
	// Manager.Children and Manager.References would both enumerate the same
	// triples, double-projecting the entity as an owned child AND an unowned
	// reference. validate()'s dup check only guards child-link-vs-child-link.
	many := make(map[string]string) // predicate -> kind (for the error message)
	for _, ch := range w.ChildWorkflows {
		many[ch.LinkPredicate] = "child-link"
	}
	for _, ref := range w.ReferencePredicates {
		if existing, ok := many[ref.Predicate]; ok {
			return fmt.Errorf("%w: workflow %q predicate %q is declared both as %s and reference — both are cardinality-many and would double-project the same triples",
				ErrInvalidWorkflow, w.Name, ref.Predicate, existing)
		}
		many[ref.Predicate] = "reference"
	}
	if len(many) == 0 {
		return nil // nothing many-valued to collide with
	}

	check := func(pred, kind string) error {
		if pred == "" {
			return nil
		}
		if mk, ok := many[pred]; ok {
			return fmt.Errorf("%w: workflow %q predicate %q is declared both as %s (single-valued) and %s (cardinality-many) — a Transition would delete the %s triples",
				ErrInvalidWorkflow, w.Name, pred, kind, mk, mk)
		}
		return nil
	}

	if err := check(w.PhasePredicate, "phase"); err != nil {
		return err
	}
	for _, p := range []string{w.AuditPredicates.Source, w.AuditPredicates.At, w.AuditPredicates.From, w.AuditPredicates.Note} {
		if err := check(p, "audit"); err != nil {
			return err
		}
	}
	if meta != nil {
		for pred, fm := range meta.FieldsByPredicate {
			// References are themselves cardinality-many (already in `many`);
			// the phase field is checked above via PhasePredicate. Only scalar
			// projection fields are single-valued.
			if fm.IsReference || fm.IsPhase {
				continue
			}
			if err := check(pred, "projection-field"); err != nil {
				return err
			}
		}
	}
	return nil
}

// ChildOptions filters the result of Manager.Children. Empty options
// returns every child of the parent across all declared ChildWorkflows.
type ChildOptions struct {
	// Workflow narrows to a specific child workflow type. Empty
	// means all child types.
	Workflow string

	// Limit bounds the result count (after Offset). Zero means
	// unbounded; operators paginating large subtrees should always
	// set a Limit.
	Limit int

	// Offset skips this many entries before applying Limit. Used
	// for paged subtree rendering.
	Offset int
}

// ChildResult is one entry in the Manager.Children result. Carries
// the child's workflow type name + projected Participant.
type ChildResult struct {
	// Workflow is the child's workflow type (the value of the
	// matching ChildSpec.Workflow).
	Workflow string `json:"workflow"`

	// State is the projected child Participant. Type is whatever
	// the child workflow registered as its Schema.
	State Participant `json:"state"`
}

// ReferenceStub is one entry in the Manager.References result. Light
// pointer to a referenced entity; the target is NOT loaded into
// memory beyond an identity-and-phase peek.
type ReferenceStub struct {
	// EntityID is the target entity's ID.
	EntityID string `json:"entity_id"`

	// Predicate is the reference predicate (the value of the
	// matching ReferenceSpec.Predicate).
	Predicate string `json:"predicate"`

	// Workflow is the target's workflow type IF the target is
	// itself lifecycle-managed (matches a registered EntityIDPattern).
	// Empty otherwise — operator dashboards render the bare
	// entity_id and link to graph-gateway.
	Workflow string `json:"workflow,omitempty"`

	// Phase is the target's current phase IF the target is
	// lifecycle-managed. Empty otherwise.
	Phase string `json:"phase,omitempty"`
}

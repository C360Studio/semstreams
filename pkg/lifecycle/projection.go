// Package lifecycle — reflection-driven projection: triples ↔ Participant struct.
//
// On every Get the Manager:
//
//  1. Reads the entity from ENTITY_STATES (1 KV.Get via graph-gateway / direct bucket)
//  2. Allocates a fresh Participant via reflect.New(Workflow.Schema)
//  3. Walks the entity's triples, finds each predicate in the schema's
//     FieldsByPredicate index, and SetX's the corresponding field
//
// On every Transition / UpdateFromOperator / Create the Manager:
//
//  1. Builds a small slice of message.Triple from the changes
//  2. Emits via UpdateEntityWithTriplesRequest{AddTriples: ...}
//     — graph-ingest does the per-predicate latest-wins merge into
//     ENTITY_STATES on its side, so we never need to read-then-overwrite
//
// The projection layer never owns whole-entity JSON. The Participant
// struct is a typed VIEW over triples, not a copy of them. Triples
// on the entity that the schema doesn't declare are preserved by
// graph-ingest (delta semantics) and ignored by projection.
package lifecycle

import (
	"fmt"
	"reflect"
	"time"

	"github.com/c360studio/semstreams/message"
)

// projectTriples walks the entity's triples and populates the
// Participant struct's declared fields via reflection. Identity
// (the `lifecycle:"id"` field) is populated from the entityID
// argument (the entity-state key, NOT a triple).
//
// Missing triples leave the corresponding field at zero-value —
// not an error. Apps can render mid-transition states this way
// (entity has phase but not yet owner_org_id).
//
// Triples whose predicate isn't declared in the schema are
// silently ignored. The entity may have triples from other writers
// (e.g. a processor stamping `mission.command`); they round-trip
// through graph-ingest's delta semantics and never reach the
// projection — Manager.Transition's AddTriples only carries the
// declared deltas.
func projectTriples(sm *structMeta, entityID string, triples []message.Triple, target Participant) error {
	rv := reflect.ValueOf(target)
	if rv.Kind() != reflect.Pointer || rv.IsNil() {
		return fmt.Errorf("lifecycle: projectTriples requires non-nil pointer, got %v", rv.Kind())
	}
	rv = rv.Elem()
	if rv.Kind() != reflect.Struct {
		return fmt.Errorf("lifecycle: projectTriples target must point to a struct, got %v", rv.Kind())
	}

	// Populate the ID field from the entityID arg.
	if sm.IDField != nil && entityID != "" {
		idField := rv.FieldByIndex(sm.IDField.FieldIndex)
		if idField.CanSet() && idField.Kind() == reflect.String {
			idField.SetString(entityID)
		}
	}

	// Walk triples once. Per-predicate latest-wins is already
	// handled by graph-ingest at write time, but the in-memory
	// EntityState.Triples slice could in principle carry multiple
	// triples with the same predicate (cardinality-many predicates
	// like ChildSpec.LinkPredicate). For scalar projection fields
	// we take the LAST matching triple — matching the per-predicate
	// latest-wins semantic. For projection FIELDS this is fine;
	// cardinality-many predicates are NOT projection-mapped (they
	// surface via Manager.Children, not the struct view).
	for _, t := range triples {
		meta, ok := sm.FieldsByPredicate[t.Predicate]
		if !ok {
			continue
		}
		field := rv.FieldByIndex(meta.FieldIndex)
		if !field.CanSet() {
			continue
		}
		if err := setFieldFromObject(field, t.Object, meta.FieldName); err != nil {
			return err
		}
	}
	return nil
}

// setFieldFromObject writes the triple's Object value into the
// target field, coercing known wire shapes:
//
//   - string → reflect.String field
//   - RFC3339Nano string → time.Time field (for audit predicates)
//   - bool, numeric, slice — direct assignment if AssignableTo
//   - JSON-native types from graph-ingest's per-predicate validators
//
// Unsupported assignments error out — better to surface a wire-shape
// mismatch loudly at projection time than to silently drop the value.
func setFieldFromObject(field reflect.Value, object any, fieldName string) error {
	if object == nil {
		field.SetZero()
		return nil
	}

	// time.Time field — accept RFC3339[Nano] strings, time.Time, or
	// numeric unix-nanos. Audit predicates use RFC3339Nano strings.
	if field.Type() == reflect.TypeOf(time.Time{}) {
		switch v := object.(type) {
		case string:
			parsed, err := time.Parse(time.RFC3339Nano, v)
			if err != nil {
				// Try plain RFC3339 as a fallback.
				parsed2, err2 := time.Parse(time.RFC3339, v)
				if err2 != nil {
					return fmt.Errorf("lifecycle: project field %q: parse time %q: %v",
						fieldName, v, err)
				}
				parsed = parsed2
			}
			field.Set(reflect.ValueOf(parsed))
			return nil
		case time.Time:
			field.Set(reflect.ValueOf(v))
			return nil
		}
	}

	src := reflect.ValueOf(object)
	if src.Type().AssignableTo(field.Type()) {
		field.Set(src)
		return nil
	}
	if src.Type().ConvertibleTo(field.Type()) {
		field.Set(src.Convert(field.Type()))
		return nil
	}
	return fmt.Errorf("lifecycle: project field %q: cannot assign %v to %v",
		fieldName, src.Type(), field.Type())
}

// extractTripleScalar returns the Object of the LAST triple matching
// the predicate AND subject, as a string. Empty when no matching triple
// exists or the Object isn't string-shaped.
//
// Subject scoping (per ADR-049 reviewer B3): triples carry a Subject;
// only those belonging to entityID are considered. Triples with an
// empty Subject are treated as entity-scoped (entry from the legacy
// pre-Subject pipeline). This is defensive — graph-ingest stores
// triples under per-entity KV keys today, but future cross-entity
// triple storage would silently mis-project without this check.
//
// Used by Manager.Transition's read-validate path (extract current
// phase before validating the transition) and by Manager.History
// (extract audit-predicate values from each revision).
func extractTripleScalar(triples []message.Triple, entityID, predicate string) string {
	last := ""
	for _, t := range triples {
		if t.Predicate != predicate {
			continue
		}
		if t.Subject != "" && t.Subject != entityID {
			continue
		}
		switch v := t.Object.(type) {
		case string:
			last = v
		case fmt.Stringer:
			last = v.String()
		default:
			last = fmt.Sprintf("%v", v)
		}
	}
	return last
}

// hasTriple returns true when any triple in the slice has the given
// subject (or empty subject) and predicate. Used by Manager.Create
// to detect "lifecycle already attached to this entity."
func hasTriple(triples []message.Triple, entityID, predicate string) bool {
	for _, t := range triples {
		if t.Predicate != predicate {
			continue
		}
		if t.Subject != "" && t.Subject != entityID {
			continue
		}
		return true
	}
	return false
}

// projectStructToTriples extracts the projection-declared fields
// from the Participant struct as triples. Used by Manager.Create
// to stamp the initial state: phase + operator-writable defaults +
// reference fields + audit fields. Skips ID (lives in the bucket
// key) and skips fields with no declared predicate.
//
// Skips zero-value fields by default — Create's intent is "stamp
// the non-empty initial state," not "stamp empty defaults for every
// declared predicate." Apps that want to stamp an empty string
// can pre-populate the field with a sentinel.
func projectStructToTriples(sm *structMeta, entityID string, source Participant) []message.Triple {
	rv := reflect.ValueOf(source)
	if rv.Kind() == reflect.Pointer {
		rv = rv.Elem()
	}
	var out []message.Triple
	for _, meta := range sm.FieldsByPredicate {
		if meta.IsID || meta.ReadOnly && !meta.IsPhase {
			// Skip ID (no predicate target).
			// Skip readonly non-phase fields (audit + reference
			// targets are stamped by the framework / upstream
			// emitters, not by Create-from-struct).
			continue
		}
		field := rv.FieldByIndex(meta.FieldIndex)
		if !field.IsValid() || field.IsZero() {
			continue
		}
		out = append(out, triple(entityID, meta.Predicate, field.Interface()))
	}
	return out
}

// projectPatchToTriples translates a JSON-name-keyed operator patch
// map into a slice of triples to emit. Each key must resolve to a
// schema field tagged operator_writable; UpdateFromOperator validates
// this before calling. Zero / nil values map to a triple removal
// (callers use RemoveTriples in the request to delete a predicate).
func projectPatchToTriples(sm *structMeta, entityID string, patch map[string]any) (adds []message.Triple, removes []string, err error) {
	for jsonName, value := range patch {
		meta, ok := sm.FieldsByJSONName[jsonName]
		if !ok {
			return nil, nil, fmt.Errorf("%w: field %q not declared on schema",
				ErrFieldNotOperatorWritable, jsonName)
		}
		if !meta.OperatorWritable {
			return nil, nil, fmt.Errorf("%w: field %q is not operator_writable",
				ErrFieldNotOperatorWritable, jsonName)
		}
		if value == nil {
			removes = append(removes, meta.Predicate)
			continue
		}
		adds = append(adds, triple(entityID, meta.Predicate, value))
	}
	return adds, removes, nil
}

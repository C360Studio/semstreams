package vocabulary

import (
	"fmt"
	"strings"
	"testing"
)

// The closed vocabulary and its legacy spellings, written out from design
// §2.3's mapping table rather than read back from dataTypeCanonicalization.
// A test that enumerated the map could not see a row deleted from it.

var canonicalDataTypeDomain = []string{
	"string", "entity_id", "int", "float", "bool", "datetime", "json",
}

var legacyDataTypeDomain = map[string]string{
	"float64":    "float",
	"number":     "float",
	"double":     "float",
	"time.Time":  "datetime",
	"timestamp":  "datetime",
	"int64":      "int",
	"array":      "json",
	"entity_ref": "entity_id",
	"reference":  "entity_id",
	"boolean":    "bool",
}

// Spellings that must be refused: semdragon's Go payload struct names, the
// semantic-web names ADR-107 keeps at the export edge, and near-misses.
var refusedDataTypeSamples = []string{
	"PeerReviewPayload", "ReviewDecision", "map[string]any", "[]string",
	"integer", "dateTime", "@id", "rdf:JSON", "xsd:string", "decimal", "date",
	"duration", "anyURI", "String", "INT", "float32", "uint", "any", " string",
}

// TestCanonicalDataTypeAcceptsTheClosedVocabularyUnchanged pins the first
// scenario: a value already in the closed vocabulary registers as itself.
//
// spec: predicate-contract / A declared predicate datatype comes from one closed pragmatic vocabulary
func TestCanonicalDataTypeAcceptsTheClosedVocabularyUnchanged(t *testing.T) {
	for _, canonical := range canonicalDataTypeDomain {
		got, err := canonicalDataType(canonical)
		if err != nil {
			t.Errorf("canonicalDataType(%q): unexpected error %v", canonical, err)
			continue
		}
		if got != canonical {
			t.Errorf("canonicalDataType(%q) = %q, want it unchanged", canonical, got)
		}
		if !IsValidDataType(canonical) {
			t.Errorf("IsValidDataType(%q) = false, want true", canonical)
		}
	}
	if len(canonicalDataTypeDomain) != 7 {
		t.Fatalf("closed vocabulary is %d values, not 7 — the export mapping guard in "+
			"test/contract/ and the completeness guard walk this set and must be updated with it",
			len(canonicalDataTypeDomain))
	}
}

// TestCanonicalDataTypeNormalizesEveryRecognizedLegacySpelling pins the second
// scenario, and the totality half of I3: canonical is total on the legacy set.
//
// spec: predicate-contract / A declared predicate datatype comes from one closed pragmatic vocabulary
func TestCanonicalDataTypeNormalizesEveryRecognizedLegacySpelling(t *testing.T) {
	for legacy, want := range legacyDataTypeDomain {
		got, err := canonicalDataType(legacy)
		if err != nil {
			t.Errorf("canonicalDataType(%q): unexpected error %v", legacy, err)
			continue
		}
		if got != want {
			t.Errorf("canonicalDataType(%q) = %q, want %q", legacy, got, want)
		}
		if IsValidDataType(legacy) {
			t.Errorf("IsValidDataType(%q) = true, but a legacy spelling is never canonical", legacy)
		}
	}
}

// TestCanonicalDataTypeIsIdempotentOverItsWholeDomain pins I2, the invariant
// that makes amend-registration safe: Register amends rather than replaces
// (gh#410), so an already-normalized value is re-validated on every later
// registration of the same predicate.
//
// spec: predicate-contract / A declared predicate datatype comes from one closed pragmatic vocabulary
func TestCanonicalDataTypeIsIdempotentOverItsWholeDomain(t *testing.T) {
	domain := append([]string{""}, canonicalDataTypeDomain...)
	for legacy := range legacyDataTypeDomain {
		domain = append(domain, legacy)
	}

	for _, declared := range domain {
		once, err := canonicalDataType(declared)
		if err != nil {
			t.Errorf("canonicalDataType(%q): unexpected error %v", declared, err)
			continue
		}
		twice, err := canonicalDataType(once)
		if err != nil {
			t.Errorf("canonicalDataType(canonicalDataType(%q)) = _, %v; want no error", declared, err)
			continue
		}
		if twice != once {
			t.Errorf("canonicalDataType is not idempotent at %q: %q then %q", declared, once, twice)
		}
	}
}

// TestCanonicalDataTypeRefusesAnythingOutsideItsDomain pins the third
// scenario and the refusal half of I3: there is no third outcome, and the
// message names both the offending value and the accepted vocabulary.
//
// spec: predicate-contract / A declared predicate datatype comes from one closed pragmatic vocabulary
func TestCanonicalDataTypeRefusesAnythingOutsideItsDomain(t *testing.T) {
	for _, refused := range refusedDataTypeSamples {
		got, err := canonicalDataType(refused)
		if err == nil {
			t.Errorf("canonicalDataType(%q) = %q, want a refusal", refused, got)
			continue
		}
		if got != "" {
			t.Errorf("canonicalDataType(%q) returned %q alongside its error; a refusal returns no value",
				refused, got)
		}
		if !strings.Contains(err.Error(), refused) {
			t.Errorf("refusal of %q does not name the offending value: %v", refused, err)
		}
		for _, canonical := range canonicalDataTypeDomain {
			if !strings.Contains(err.Error(), canonical) {
				t.Errorf("refusal of %q does not name accepted value %q: %v", refused, canonical, err)
			}
		}
		if IsValidDataType(refused) {
			t.Errorf("IsValidDataType(%q) = true, want false", refused)
		}
	}
}

// TestCanonicalDataTypeAcceptsAbsence pins the fourth scenario. Three sister
// repositories register PredicateMetadata{Name: predicate} with no datatype at
// all; refusing absence would panic their boot for no benefit.
//
// spec: predicate-contract / A declared predicate datatype comes from one closed pragmatic vocabulary
func TestCanonicalDataTypeAcceptsAbsence(t *testing.T) {
	got, err := canonicalDataType("")
	if err != nil {
		t.Fatalf("canonicalDataType(\"\"): unexpected error %v", err)
	}
	if got != "" {
		t.Fatalf("canonicalDataType(\"\") = %q, want the absent value substituted with nothing", got)
	}
	if IsValidDataType("") {
		t.Error("IsValidDataType(\"\") = true; absence is accepted at registration but is not a value")
	}
}

// registrationPanic runs fn and returns the panic value as a string, or "" if
// fn returned normally. Registration refusal is a panic, matching every other
// refusal this validator raises.
func registrationPanic(fn func()) (msg string) {
	defer func() {
		if r := recover(); r != nil {
			msg = fmt.Sprint(r)
		}
	}()
	fn()
	return ""
}

// TestRegistrationNormalizesALegacySpellingOnce pins that the registry stores
// only the canonical value, so no reader ever observes a legacy spelling.
//
// spec: predicate-contract / A declared predicate datatype comes from one closed pragmatic vocabulary
func TestRegistrationNormalizesALegacySpellingOnce(t *testing.T) {
	defer SnapshotRegistry()()

	Register("datatype.normalize.optionpath", WithDataType("float64"))
	if got := GetPredicateMetadata("datatype.normalize.optionpath").DataType; got != DataTypeFloat {
		t.Errorf("option path: DataType = %q, want %q", got, DataTypeFloat)
	}

	RegisterPredicate(PredicateMetadata{Name: "datatype.normalize.structpath", DataType: "time.Time"})
	if got := GetPredicateMetadata("datatype.normalize.structpath").DataType; got != DataTypeDateTime {
		t.Errorf("struct-literal path: DataType = %q, want %q", got, DataTypeDateTime)
	}
}

// TestAmendingReRegistrationKeepsTheInheritedCanonicalValue pins the amend
// path (gh#410): Register seeds from the existing registration, so a
// re-registration that never mentions the datatype re-validates an inherited,
// already-normalized value. Without idempotent normalization this panics.
//
// spec: predicate-contract / A declared predicate datatype comes from one closed pragmatic vocabulary
func TestAmendingReRegistrationKeepsTheInheritedCanonicalValue(t *testing.T) {
	defer SnapshotRegistry()()

	Register("datatype.amend.inherited", WithDataType("entity_ref"))
	if got := GetPredicateMetadata("datatype.amend.inherited").DataType; got != DataTypeEntityID {
		t.Fatalf("first registration: DataType = %q, want %q", got, DataTypeEntityID)
	}

	if msg := registrationPanic(func() {
		Register("datatype.amend.inherited", WithDescription("amended, datatype not mentioned"))
	}); msg != "" {
		t.Fatalf("amending re-registration panicked on its own inherited value: %s", msg)
	}

	meta := GetPredicateMetadata("datatype.amend.inherited")
	if meta.DataType != DataTypeEntityID {
		t.Errorf("after amend: DataType = %q, want the inherited %q", meta.DataType, DataTypeEntityID)
	}
	if meta.Description != "amended, datatype not mentioned" {
		t.Errorf("after amend: Description = %q, want the amended value", meta.Description)
	}
}

// TestBothRegistrationEntryPointsRefuseTheSameValue pins that neither path
// accepts a value the other refuses, and that refusal names both the offending
// value and the accepted vocabulary.
//
// spec: predicate-contract / A declared predicate datatype comes from one closed pragmatic vocabulary
func TestBothRegistrationEntryPointsRefuseTheSameValue(t *testing.T) {
	defer SnapshotRegistry()()

	const refused = "PeerReviewPayload"
	paths := map[string]func(){
		"functional option": func() { Register("datatype.refuse.optionpath", WithDataType(refused)) },
		"direct metadata": func() {
			RegisterPredicate(PredicateMetadata{Name: "datatype.refuse.structpath", DataType: refused})
		},
	}

	for name, register := range paths {
		msg := registrationPanic(register)
		if msg == "" {
			t.Errorf("%s: accepted %q, want a refusal", name, refused)
			continue
		}
		if !strings.Contains(msg, refused) {
			t.Errorf("%s: refusal does not name the offending value: %s", name, msg)
		}
		for _, canonical := range canonicalDataTypeDomain {
			if !strings.Contains(msg, canonical) {
				t.Errorf("%s: refusal does not name accepted value %q: %s", name, canonical, msg)
			}
		}
	}

	if GetPredicateMetadata("datatype.refuse.optionpath") != nil ||
		GetPredicateMetadata("datatype.refuse.structpath") != nil {
		t.Error("a refused registration was still stored")
	}
}

// TestRegistrationAcceptsAnAbsentDataType pins that declarations carrying no
// datatype stay legal. Three sister repositories register nothing but a
// predicate name; refusing absence would panic their boot for no benefit.
//
// spec: predicate-contract / A declared predicate datatype comes from one closed pragmatic vocabulary
func TestRegistrationAcceptsAnAbsentDataType(t *testing.T) {
	defer SnapshotRegistry()()

	if msg := registrationPanic(func() {
		RegisterPredicate(PredicateMetadata{Name: "datatype.absent.structpath"})
	}); msg != "" {
		t.Fatalf("registering with no datatype panicked: %s", msg)
	}
	if msg := registrationPanic(func() {
		Register("datatype.absent.optionpath", WithDescription("no datatype declared"))
	}); msg != "" {
		t.Fatalf("registering with no datatype panicked: %s", msg)
	}

	for _, predicate := range []string{"datatype.absent.structpath", "datatype.absent.optionpath"} {
		meta := GetPredicateMetadata(predicate)
		if meta == nil {
			t.Fatalf("%s: not registered", predicate)
		}
		if meta.DataType != "" {
			t.Errorf("%s: DataType = %q, want it absent rather than a substituted default",
				predicate, meta.DataType)
		}
	}
}

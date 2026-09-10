package vocabulary

import (
	"fmt"
	"strings"
	"testing"
)

// The closed vocabulary, written out from design §2.3's table rather than read
// back from the validator. A test that enumerated the implementation could not
// see a value dropped from it.

var canonicalDataTypeDomain = []string{
	"string", "entity_id", "int", "float", "bool", "datetime", "json",
}

// retiredLegacySpellings are the ten spellings an earlier draft of this change
// normalized. Owner ruling 2026-09-09 (gh#1267, option (d) no-legacy) deleted
// that map, so every one of them is now refused at registration and the
// adopter migrates instead. They are pinned here as their own corpus, apart
// from the never-accepted samples below, because these are the values whose
// treatment the ruling REVERSED — a regression that quietly reintroduced the
// map would still refuse a Go struct name while accepting these again.
var retiredLegacySpellings = []string{
	"float64", "number", "double", "time.Time", "timestamp",
	"int64", "array", "entity_ref", "reference", "boolean",
}

// Spellings that must be refused: semdragon's Go payload struct names, the
// semantic-web names ADR-107 keeps at the export edge, and near-misses.
var refusedDataTypeSamples = []string{
	"PeerReviewPayload", "ReviewDecision", "map[string]any", "[]string",
	"integer", "dateTime", "@id", "rdf:JSON", "xsd:string", "decimal", "date",
	"duration", "anyURI", "String", "INT", "float32", "uint", "any", " string",
}

// TestValidateDataTypeAcceptsTheClosedVocabulary pins the first scenario: a
// value in the closed vocabulary is accepted.
//
// spec: predicate-contract / A declared predicate datatype comes from one closed pragmatic vocabulary
func TestValidateDataTypeAcceptsTheClosedVocabulary(t *testing.T) {
	for _, canonical := range canonicalDataTypeDomain {
		if err := validateDataType(canonical); err != nil {
			t.Errorf("validateDataType(%q): unexpected error %v", canonical, err)
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

// TestValidateDataTypeRefusesEveryRetiredLegacySpelling pins the owner's
// no-legacy ruling at the seam that enforces it. The framework normalizes
// nothing: a value that is not canonical is refused, whatever it once meant.
//
// spec: predicate-contract / A declared predicate datatype comes from one closed pragmatic vocabulary
func TestValidateDataTypeRefusesEveryRetiredLegacySpelling(t *testing.T) {
	for _, legacy := range retiredLegacySpellings {
		err := validateDataType(legacy)
		if err == nil {
			t.Errorf("validateDataType(%q) accepted a retired legacy spelling; option (d) refuses it", legacy)
			continue
		}
		if !strings.Contains(err.Error(), legacy) {
			t.Errorf("refusal of %q does not name the offending value: %v", legacy, err)
		}
		if IsValidDataType(legacy) {
			t.Errorf("IsValidDataType(%q) = true, but a legacy spelling is never canonical", legacy)
		}
	}
}

// TestValidateDataTypeRefusesAnythingOutsideTheClosedVocabulary pins the
// refusal half of I3: there is no third outcome, and the message names both
// the offending value and the accepted vocabulary, because the adopter who
// wrote a Go struct name needs to be told what to write instead.
//
// spec: predicate-contract / A declared predicate datatype comes from one closed pragmatic vocabulary
func TestValidateDataTypeRefusesAnythingOutsideTheClosedVocabulary(t *testing.T) {
	for _, refused := range refusedDataTypeSamples {
		err := validateDataType(refused)
		if err == nil {
			t.Errorf("validateDataType(%q) = nil, want a refusal", refused)
			continue
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

// TestValidateDataTypeAcceptsAbsence pins the fourth scenario. Three sister
// repositories register PredicateMetadata{Name: predicate} with no datatype at
// all; refusing absence would panic their boot for no benefit.
//
// spec: predicate-contract / A declared predicate datatype comes from one closed pragmatic vocabulary
func TestValidateDataTypeAcceptsAbsence(t *testing.T) {
	if err := validateDataType(""); err != nil {
		t.Fatalf("validateDataType(\"\"): unexpected error %v", err)
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

// TestRegistrationStoresTheDeclaredValueUnchanged pins the property that
// replaces normalization idempotence under option (d): nothing rewrites a
// declared datatype, so what an adopter wrote is what every reader observes.
// Under the deleted normalizer this test could not have been written — the
// registry stored a value the declaration never contained.
//
// spec: predicate-contract / A declared predicate datatype comes from one closed pragmatic vocabulary
func TestRegistrationStoresTheDeclaredValueUnchanged(t *testing.T) {
	defer SnapshotRegistry()()

	// Predicate names are three-part and their segments reject underscores,
	// so the canonical value goes in the middle segment with any underscore
	// stripped ("entity_id" -> "entityid").
	for _, canonical := range canonicalDataTypeDomain {
		segment := strings.ReplaceAll(canonical, "_", "")
		option := "datatype." + segment + ".optionpath"
		structPath := "datatype." + segment + ".structpath"

		Register(option, WithDataType(canonical))
		if got := GetPredicateMetadata(option).DataType; got != canonical {
			t.Errorf("option path for %q: DataType = %q, want the declared value", canonical, got)
		}

		RegisterPredicate(PredicateMetadata{Name: structPath, DataType: canonical})
		if got := GetPredicateMetadata(structPath).DataType; got != canonical {
			t.Errorf("struct-literal path for %q: DataType = %q, want the declared value", canonical, got)
		}
	}
}

// TestRegistrationRefusesALegacySpellingOnBothPaths pins the no-legacy ruling
// at the registration seam rather than at the validator, and on both entry
// points: before the ruling, each of these calls succeeded and silently stored
// a different value than the one written.
//
// spec: predicate-contract / A declared predicate datatype comes from one closed pragmatic vocabulary
func TestRegistrationRefusesALegacySpellingOnBothPaths(t *testing.T) {
	defer SnapshotRegistry()()

	for _, legacy := range retiredLegacySpellings {
		option := "datatype.legacy.optionpath"
		structPath := "datatype.legacy.structpath"

		if msg := registrationPanic(func() {
			Register(option, WithDataType(legacy))
		}); msg == "" {
			t.Errorf("option path accepted retired spelling %q, want a refusal", legacy)
		}
		if msg := registrationPanic(func() {
			RegisterPredicate(PredicateMetadata{Name: structPath, DataType: legacy})
		}); msg == "" {
			t.Errorf("struct-literal path accepted retired spelling %q, want a refusal", legacy)
		}
		if GetPredicateMetadata(option) != nil || GetPredicateMetadata(structPath) != nil {
			t.Fatalf("a registration refused for %q was still stored", legacy)
		}
	}
}

// TestAmendingReRegistrationKeepsTheInheritedValue pins the amend path
// (gh#410): Register seeds from the existing registration, so a
// re-registration that never mentions the datatype re-validates an inherited
// value. That inherited value must still pass the validator, or every amending
// registration in the family panics on a datatype it did not set.
//
// spec: predicate-contract / A declared predicate datatype comes from one closed pragmatic vocabulary
func TestAmendingReRegistrationKeepsTheInheritedValue(t *testing.T) {
	defer SnapshotRegistry()()

	Register("datatype.amend.inherited", WithDataType(DataTypeEntityID))
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

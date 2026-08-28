package community

import (
	"testing"

	"github.com/c360studio/semstreams/graph/clustering"
)

func TestValidator_AllInSameCommunity(t *testing.T) {
	communities := []*clustering.Community{
		{
			ID:      "comm-1",
			Members: []string{"sensor-temp-001", "sensor-temp-002", "sensor-temp-003"},
		},
		{
			ID:      "comm-2",
			Members: []string{"doc-hr-001", "doc-hr-002"},
		},
	}

	expectations := []Expectation{
		{
			Name:        "temperature_sensors",
			MustContain: []string{"sensor-temp-001", "sensor-temp-002"},
		},
	}

	validator := NewValidator(expectations)
	result := validator.Validate(communities)

	if !result.Passed() {
		t.Errorf("expected validation to pass, got violations: %v", result.Violations)
	}
	if result.ExpectationsPassed != 1 {
		t.Errorf("expected 1 expectation passed, got %d", result.ExpectationsPassed)
	}
}

func TestValidator_EntitiesInDifferentCommunities(t *testing.T) {
	communities := []*clustering.Community{
		{
			ID:      "comm-1",
			Members: []string{"sensor-temp-001"},
		},
		{
			ID:      "comm-2",
			Members: []string{"sensor-temp-002", "sensor-temp-003"},
		},
	}

	expectations := []Expectation{
		{
			Name:        "temperature_sensors",
			MustContain: []string{"sensor-temp-001", "sensor-temp-002"},
		},
	}

	validator := NewValidator(expectations)
	result := validator.Validate(communities)

	if result.Passed() {
		t.Errorf("expected validation to fail")
	}
	if len(result.Violations) != 1 {
		t.Errorf("expected 1 violation, got %d", len(result.Violations))
	}
	if result.Violations[0].Type != ViolationNotGrouped {
		t.Errorf("expected ViolationNotGrouped, got %v", result.Violations[0].Type)
	}
}

func TestValidator_EntityNotFound(t *testing.T) {
	communities := []*clustering.Community{
		{
			ID:      "comm-1",
			Members: []string{"sensor-temp-001"},
		},
	}

	expectations := []Expectation{
		{
			Name:        "missing_entity",
			MustContain: []string{"sensor-temp-001", "nonexistent-entity"},
		},
	}

	validator := NewValidator(expectations)
	result := validator.Validate(communities)

	if result.Passed() {
		t.Errorf("expected validation to fail")
	}
	if len(result.Violations) != 1 {
		t.Errorf("expected 1 violation, got %d", len(result.Violations))
	}
	if result.Violations[0].Type != ViolationEntityNotFound {
		t.Errorf("expected ViolationEntityNotFound, got %v", result.Violations[0].Type)
	}
}

func TestValidator_UnexpectedGrouping(t *testing.T) {
	communities := []*clustering.Community{
		{
			ID:      "comm-1",
			Members: []string{"sensor-temp-001", "sensor-temp-002", "doc-hr-001"},
		},
	}

	expectations := []Expectation{
		{
			Name:           "temperature_sensors",
			MustContain:    []string{"sensor-temp-001", "sensor-temp-002"},
			MustNotContain: []string{"doc-hr"},
		},
	}

	validator := NewValidator(expectations)
	result := validator.Validate(communities)

	if result.Passed() {
		t.Errorf("expected validation to fail")
	}
	if len(result.Violations) != 1 {
		t.Errorf("expected 1 violation, got %d", len(result.Violations))
	}
	if result.Violations[0].Type != ViolationUnexpectedGrouped {
		t.Errorf("expected ViolationUnexpectedGrouped, got %v", result.Violations[0].Type)
	}
}

func TestValidator_MustNotContainInDifferentCommunity(t *testing.T) {
	communities := []*clustering.Community{
		{
			ID:      "comm-1",
			Members: []string{"sensor-temp-001", "sensor-temp-002"},
		},
		{
			ID:      "comm-2",
			Members: []string{"doc-hr-001"},
		},
	}

	expectations := []Expectation{
		{
			Name:           "temperature_sensors",
			MustContain:    []string{"sensor-temp-001"},
			MustNotContain: []string{"doc-hr"},
		},
	}

	validator := NewValidator(expectations)
	result := validator.Validate(communities)

	if !result.Passed() {
		t.Errorf("expected validation to pass (doc-hr in different community), got violations: %v", result.Violations)
	}
}

func TestValidator_PatternMatching(t *testing.T) {
	communities := []*clustering.Community{
		{
			ID: "comm-1",
			Members: []string{
				"c360.logistics.document.sensor.temperature.sensor-temp-001",
				"c360.logistics.document.sensor.temperature.sensor-temp-002",
			},
		},
	}

	expectations := []Expectation{
		{
			Name:        "temperature_sensors",
			MustContain: []string{"sensor-temp-001", "sensor-temp-002"}, // Patterns, not full IDs
		},
	}

	validator := NewValidator(expectations)
	result := validator.Validate(communities)

	if !result.Passed() {
		t.Errorf("expected validation to pass with pattern matching, got violations: %v", result.Violations)
	}
}

func TestValidator_MultipleExpectations(t *testing.T) {
	communities := []*clustering.Community{
		{
			ID:      "comm-1",
			Members: []string{"sensor-temp-001", "sensor-temp-002"},
		},
		{
			ID:      "comm-2",
			Members: []string{"maint-001", "maint-002"},
		},
		{
			ID:      "comm-3",
			Members: []string{"doc-hr-001"},
		},
	}

	expectations := []Expectation{
		{
			Name:        "temperature_sensors",
			MustContain: []string{"sensor-temp-001", "sensor-temp-002"},
		},
		{
			Name:        "maintenance_records",
			MustContain: []string{"maint-001", "maint-002"},
		},
	}

	validator := NewValidator(expectations)
	result := validator.Validate(communities)

	if !result.Passed() {
		t.Errorf("expected all expectations to pass, got violations: %v", result.Violations)
	}
	if result.ExpectationsPassed != 2 {
		t.Errorf("expected 2 expectations passed, got %d", result.ExpectationsPassed)
	}
	if result.ExpectationsTotal != 2 {
		t.Errorf("expected 2 total expectations, got %d", result.ExpectationsTotal)
	}
}

func TestValidator_EmptyCommunities(t *testing.T) {
	var communities []*clustering.Community

	expectations := []Expectation{
		{
			Name:        "temperature_sensors",
			MustContain: []string{"sensor-temp-001"},
		},
	}

	validator := NewValidator(expectations)
	result := validator.Validate(communities)

	if result.Passed() {
		t.Errorf("expected validation to fail with empty communities")
	}
	if result.Violations[0].Type != ViolationEntityNotFound {
		t.Errorf("expected ViolationEntityNotFound, got %v", result.Violations[0].Type)
	}
}

func TestDefaultExpectations(t *testing.T) {
	expectations := DefaultExpectations()

	if len(expectations) == 0 {
		t.Error("expected default expectations to be non-empty")
	}

	for _, exp := range expectations {
		if exp.Name == "" {
			t.Error("expectation name should not be empty")
		}
		if len(exp.MustContain) == 0 {
			t.Errorf("expectation %q should have MustContain", exp.Name)
		}
	}
}

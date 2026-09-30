package scenarios

import (
	"fmt"
	"strings"

	"github.com/c360studio/semstreams/test/e2e/scenarios/search"
)

// CheckRequirements names the bounded proof set selected by this variant.
func (s *TieredScenario) CheckRequirements() []CheckRequirement {
	variant := s.config.Variant
	if variant != "structural" && variant != "statistical" && variant != "semantic" {
		return nil
	}
	checks := []CheckRequirement{
		{ID: variant + ".components", Required: true},
		{ID: variant + ".graph-roundtrip.identity", Required: true},
	}
	switch variant {
	case "structural":
		checks = append(checks, CheckRequirement{ID: "structural.zero-embeddings", Required: true},
			CheckRequirement{ID: "structural.zero-clustering-runs", Required: true})
	case "statistical":
		checks = append(checks, CheckRequirement{ID: "statistical.controlled-search.identity", Required: true})
	case "semantic":
		checks = append(checks, CheckRequirement{ID: "semantic.semembed", Required: true},
			CheckRequirement{ID: "semantic.controlled-search.identity", Required: true})
	}
	return checks
}

func (s *TieredScenario) recordTieredCheck(result *Result, id string, checkErr error, evidence map[string]string) error {
	if result.RunID == "" {
		return checkErr
	}
	observation := CheckObservation{ID: id, RunID: result.RunID, MemberID: result.MemberID,
		Status: "passed", Evidence: evidence}
	if checkErr != nil {
		observation.Status = "failed"
		observation.Reason = checkErr.Error()
	}
	if err := result.RecordCheck(observation); err != nil {
		return err
	}
	return checkErr
}

// controlledSearchObservation compares the actual GraphQL consumer result with
// the complete ID of one deterministic input fixture under the observed authority.
func (s *TieredScenario) controlledSearchObservation(stats *search.Stats) CheckObservation {
	expected := s.effectiveAuthority + ".document.content.operations.doc-ops-001"
	observation := CheckObservation{Status: "failed", Evidence: map[string]string{
		"expected_entity_id": expected, "query": search.DefaultQueries()[0].Text,
	}}
	if s.effectiveAuthority == "" {
		observation.Reason = "effective fixture authority unavailable"
		return observation
	}
	if stats == nil {
		observation.Reason = "controlled query result unavailable"
		return observation
	}
	for _, query := range stats.Results {
		if query.Query != search.DefaultQueries()[0].Text {
			continue
		}
		if query.Error != "" {
			observation.Reason = "controlled query error: " + query.Error
			return observation
		}
		for _, hit := range query.Hits {
			if hit.EntityID == expected {
				observation.Status = "passed"
				observation.Reason = ""
				observation.Evidence["actual_entity_id"] = hit.EntityID
				return observation
			}
		}
		observed := make([]string, 0, len(query.Hits))
		for _, hit := range query.Hits {
			observed = append(observed, hit.EntityID)
		}
		observation.Reason = fmt.Sprintf("expected fixture identity missing from %d query hits", len(query.Hits))
		observation.Evidence["observed_entity_ids"] = strings.Join(observed, ",")
		return observation
	}
	observation.Reason = "controlled query result unavailable"
	return observation
}

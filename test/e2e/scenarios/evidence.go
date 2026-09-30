package scenarios

import (
	"fmt"
	"sort"
	"strings"
)

// CheckRequirement names one selected behavioral observation.
type CheckRequirement struct {
	ID       string `json:"id"`
	Required bool   `json:"required"`
}

// CheckObservation records a completed comparison, or why it could not be made.
// Evidence holds bounded identity/value summaries and artifact references, not payloads.
type CheckObservation struct {
	ID       string            `json:"id"`
	RunID    string            `json:"run_id"`
	MemberID string            `json:"member_id"`
	Status   string            `json:"status"`
	Reason   string            `json:"reason,omitempty"`
	Evidence map[string]string `json:"evidence,omitempty"`
}

// DeclareChecks fixes the run/member and the selected checks before observation.
func (r *Result) DeclareChecks(runID, memberID string, checks []CheckRequirement) error {
	if r == nil {
		return fmt.Errorf("nil result")
	}
	if r.RunID != "" || r.MemberID != "" || r.CheckRequirements != nil {
		return r.refuseCheck("checks already declared")
	}
	if err := validateCheckRequirements(runID, memberID, checks); err != nil {
		return r.refuseCheck(err.Error())
	}
	r.RunID, r.MemberID = runID, memberID
	r.CheckRequirements = append([]CheckRequirement(nil), checks...)
	r.EvidenceStatus = "unattested"
	return nil
}

// RecordCheck appends one observation for a previously declared check.
// A refusal remains a source error even if its caller ignores the returned error.
func (r *Result) RecordCheck(observation CheckObservation) error {
	if r == nil {
		return fmt.Errorf("nil result")
	}
	if len(r.CheckRequirements) == 0 {
		return r.refuseCheck("checks were not declared")
	}
	if observation.RunID != r.RunID || observation.MemberID != r.MemberID {
		return r.refuseCheck(fmt.Sprintf("check %q has foreign run/member identity", observation.ID))
	}
	if !validCheckID(observation.ID) {
		return r.refuseCheck("check observation has invalid ID")
	}
	declared := false
	for _, check := range r.CheckRequirements {
		if check.ID == observation.ID {
			declared = true
			break
		}
	}
	if !declared {
		return r.refuseCheck(fmt.Sprintf("check %q was not declared", observation.ID))
	}
	if err := validateCheckObservation(observation); err != nil {
		return r.refuseCheck(err.Error())
	}
	for _, prior := range r.CheckObservations {
		if prior.ID == observation.ID {
			return r.refuseCheck(fmt.Sprintf("check %q observed more than once", observation.ID))
		}
	}
	r.CheckObservations = append(r.CheckObservations, observation)
	return nil
}

// FinalizeChecks derives success from the declared required set and observations.
// It also validates directly constructed or decoded Results, which may not have
// passed through DeclareChecks and RecordCheck.
func (r *Result) FinalizeChecks() error {
	if r == nil {
		return fmt.Errorf("nil result")
	}
	sourceError := r.Error
	if sourceError == r.finalizationError {
		sourceError = ""
	}
	var problems []string
	if err := validateCheckRequirements(r.RunID, r.MemberID, r.CheckRequirements); err != nil {
		problems = append(problems, err.Error())
	}
	required := make(map[string]bool, len(r.CheckRequirements))
	for _, check := range r.CheckRequirements {
		required[check.ID] = check.Required
	}
	observed := make(map[string]CheckObservation, len(r.CheckObservations))
	r.AssertionsRun = 0
	for _, observation := range r.CheckObservations {
		if observation.RunID != r.RunID || observation.MemberID != r.MemberID {
			problems = append(problems, fmt.Sprintf("check %q has foreign run/member identity", observation.ID))
		}
		isRequired, known := required[observation.ID]
		if !known {
			problems = append(problems, fmt.Sprintf("check %q was not declared", observation.ID))
		}
		if err := validateCheckObservation(observation); err != nil {
			problems = append(problems, err.Error())
		}
		if _, duplicate := observed[observation.ID]; duplicate {
			problems = append(problems, fmt.Sprintf("check %q observed more than once", observation.ID))
		} else {
			observed[observation.ID] = observation
			if known && isRequired && (observation.Status == "passed" || observation.Status == "failed") {
				r.AssertionsRun++
			}
		}
	}
	for _, check := range r.CheckRequirements {
		if !check.Required {
			continue
		}
		observation, ok := observed[check.ID]
		if !ok {
			problems = append(problems, fmt.Sprintf("required check %q missing", check.ID))
		} else if observation.Status != "passed" {
			problems = append(problems, fmt.Sprintf("required check %q %s: %s", check.ID, observation.Status, observation.Reason))
		}
	}
	if sourceError != "" {
		problems = append(problems, sourceError)
	}
	if len(r.Errors) > 0 {
		problems = append(problems, r.Errors...)
	}
	if len(problems) > 0 {
		sort.Strings(problems)
		err := fmt.Errorf("required evidence incomplete: %s", strings.Join(problems, "; "))
		r.Success = false
		r.EvidenceStatus = "unattested"
		if sourceError == "" {
			r.Error = err.Error()
			r.finalizationError = r.Error
		}
		r.syncStructuredOutcome()
		return err
	}
	r.Success = true
	r.EvidenceStatus = "complete"
	if r.finalizationError != "" && r.Error == r.finalizationError {
		r.Error = ""
	}
	r.finalizationError = ""
	r.syncStructuredOutcome()
	return nil
}

func (r *Result) syncStructuredOutcome() {
	if r.Structured == nil {
		return
	}
	r.Structured.Metadata.Success = r.Success
	r.Structured.Metadata.Errors = append([]string(nil), r.Errors...)
	r.Structured.Metadata.Warnings = append([]string(nil), r.Warnings...)
	r.Structured.Metadata.ErrorCount = len(r.Errors)
	if r.Error != "" {
		r.Structured.Metadata.Errors = append(r.Structured.Metadata.Errors, r.Error)
		r.Structured.Metadata.ErrorCount++
	}
	r.Structured.Metadata.WarningCount = len(r.Warnings)
}

func (r *Result) refuseCheck(reason string) error {
	err := fmt.Errorf("required evidence: %s", reason)
	r.Errors = append(r.Errors, err.Error())
	r.Success = false
	r.EvidenceStatus = "unattested"
	return err
}

func validateCheckRequirements(runID, memberID string, checks []CheckRequirement) error {
	if strings.TrimSpace(runID) == "" || strings.TrimSpace(memberID) == "" {
		return fmt.Errorf("run and member identity are required")
	}
	if len(checks) == 0 {
		return fmt.Errorf("required check membership is empty")
	}
	seen := make(map[string]bool, len(checks))
	requiredCount := 0
	for _, check := range checks {
		if !validCheckID(check.ID) {
			return fmt.Errorf("check declaration has invalid ID %q", check.ID)
		}
		if seen[check.ID] {
			return fmt.Errorf("check %q declared more than once", check.ID)
		}
		seen[check.ID] = true
		if check.Required {
			requiredCount++
		}
	}
	if requiredCount == 0 {
		return fmt.Errorf("required check membership is empty")
	}
	return nil
}

func validCheckID(id string) bool {
	return id != "" && strings.TrimSpace(id) == id && !strings.ContainsAny(id, " \t\n\r")
}

func validateCheckObservation(observation CheckObservation) error {
	if !validCheckID(observation.ID) {
		return fmt.Errorf("check observation has invalid ID %q", observation.ID)
	}
	switch observation.Status {
	case "passed":
	case "failed", "skipped":
		if strings.TrimSpace(observation.Reason) == "" {
			return fmt.Errorf("check %q %s without reason", observation.ID, observation.Status)
		}
	default:
		return fmt.Errorf("check %q has unknown status %q", observation.ID, observation.Status)
	}
	return nil
}

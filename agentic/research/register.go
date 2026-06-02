package research

import (
	"errors"
	"fmt"

	"github.com/c360studio/semstreams/payloadregistry"
)

// RegisterPayloads registers the three research payload types with the
// supplied registry. Called from payloadbuiltins.Register at process
// bootstrap so every production binary picks up the types without
// extra wiring.
//
// Mirrors the agentic.RegisterPayloads + agentic/operating-model
// shape so the bootstrap aggregator can call this uniformly.
func RegisterPayloads(reg *payloadregistry.Registry) error {
	registrations := []*payloadregistry.Registration{
		{
			Domain:      Domain,
			Category:    CategoryIntent,
			Version:     SchemaVersion,
			Description: "ADR-045 graph-search chain research intent: topic + hints + budget",
			Factory:     func() any { return &Intent{} },
		},
		{
			Domain:      Domain,
			Category:    CategoryResult,
			Version:     SchemaVersion,
			Description: "ADR-045 graph-search chain terminal search result with evidence + synthesis",
			Factory:     func() any { return &SearchResult{} },
		},
		{
			Domain:      Domain,
			Category:    CategoryRouteDecision,
			Version:     SchemaVersion,
			Description: "ADR-045 route_search component decision: one of four routing actions",
			Factory:     func() any { return &RouteDecision{} },
		},
		{
			Domain:      Domain,
			Category:    CategoryClassifierOutput,
			Version:     SchemaVersion,
			Description: "ADR-045 nl_classify component output: classifier hints + initial candidate set",
			Factory:     func() any { return &ClassifierOutput{} },
		},
		{
			Domain:      Domain,
			Category:    CategoryExecutionOutput,
			Version:     SchemaVersion,
			Description: "ADR-045 execute_subqueries component output: dedup'd + ranked + budget-enforced evidence array + provenance",
			Factory:     func() any { return &ExecutionOutput{} },
		},
		{
			Domain:      Domain,
			Category:    CategoryAssessmentOutput,
			Version:     SchemaVersion,
			Description: "ADR-045 assess_sufficiency component output: sufficient/refine decision + refined queries",
			Factory:     func() any { return &AssessmentOutput{} },
		},
	}

	var errs []error
	for _, r := range registrations {
		if err := reg.Register(r); err != nil {
			errs = append(errs, fmt.Errorf("register %s.%s.%s: %w", r.Domain, r.Category, r.Version, err))
		}
	}
	return errors.Join(errs...)
}

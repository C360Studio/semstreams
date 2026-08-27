package research

import (
	"errors"
	"fmt"

	"github.com/c360studio/semstreams/payloadregistry"
	"github.com/c360studio/semstreams/vocabulary"
)

// RegisterPayloads registers the complete research payload family with the
// supplied registry, each with its ADR-054 indexing-profile floor. Floors
// exist per binary because registrations do (ADR-103, O-14): a deployment
// that does not select graph research neither decodes nor births these types,
// so it holds no floor for them. Production composition roots call it through
// graphresearch.RegisterPayloads only when graphresearch.Selected reports that
// the deployment selected the graph-research capability; it is intentionally
// absent from the unconditional payloadbuiltins registry.
func RegisterPayloads(reg *payloadregistry.Registry) error {
	registrations := []*payloadregistry.Registration{
		{
			Domain:          Domain,
			Category:        CategoryIntent,
			Version:         SchemaVersion,
			Description:     "ADR-045 graph-search chain research intent: topic + hints + budget",
			Factory:         func() any { return &Intent{} },
			IndexingProfile: vocabulary.IndexingProfileControl,
		},
		{
			Domain:          Domain,
			Category:        CategoryResult,
			Version:         SchemaVersion,
			Description:     "ADR-045 graph-search chain terminal search result with evidence + synthesis",
			Factory:         func() any { return &SearchResult{} },
			IndexingProfile: vocabulary.IndexingProfileContent,
		},
		{
			Domain:          Domain,
			Category:        CategoryRouteDecision,
			Version:         SchemaVersion,
			Description:     "ADR-045 route_search component decision: one of four routing actions",
			Factory:         func() any { return &RouteDecision{} },
			IndexingProfile: vocabulary.IndexingProfileTrace,
		},
		{
			Domain:          Domain,
			Category:        CategoryClassifierOutput,
			Version:         SchemaVersion,
			Description:     "ADR-045 nl_classify component output: classifier hints + initial candidate set",
			Factory:         func() any { return &ClassifierOutput{} },
			IndexingProfile: vocabulary.IndexingProfileTrace,
		},
		{
			Domain:          Domain,
			Category:        CategoryExecutionOutput,
			Version:         SchemaVersion,
			Description:     "ADR-045 execute_subqueries component output: dedup'd + ranked + budget-enforced evidence array + provenance",
			Factory:         func() any { return &ExecutionOutput{} },
			IndexingProfile: vocabulary.IndexingProfileTrace,
		},
		{
			Domain:          Domain,
			Category:        CategoryAssessmentOutput,
			Version:         SchemaVersion,
			Description:     "ADR-045 assess_sufficiency component output: sufficient/refine decision + refined queries",
			Factory:         func() any { return &AssessmentOutput{} },
			IndexingProfile: vocabulary.IndexingProfileTrace,
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

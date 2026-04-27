package githubprworkflow

import (
	"errors"
	"fmt"

	"github.com/c360studio/semstreams/payloadregistry"
)

// init populates the package-level global registry for transitional
// compat. Removed in C4 along with the global itself.
func init() {
	if err := RegisterPayloads(payloadregistry.Global()); err != nil {
		panic("github-pr-workflow.init: " + err.Error())
	}
}

// RegisterPayloads registers all GitHub PR workflow payload types
// with the supplied registry. Called by binaries that load this
// example workflow.
func RegisterPayloads(reg *payloadregistry.Registry) error {
	registrations := []*payloadregistry.Registration{
		{
			Domain:      domainGitHub,
			Category:    categoryIssueEntity,
			Version:     schemaVersion,
			Description: "GitHub issue graph entity",
			Factory:     func() any { return &GitHubIssueEntity{} },
		},
		{
			Domain:      domainGitHub,
			Category:    categoryPREntity,
			Version:     schemaVersion,
			Description: "GitHub pull request graph entity",
			Factory:     func() any { return &GitHubPREntity{} },
		},
		{
			Domain:      domainGitHub,
			Category:    categoryReviewEntity,
			Version:     schemaVersion,
			Description: "GitHub code review graph entity",
			Factory:     func() any { return &GitHubReviewEntity{} },
		},
		{
			Domain:      domainGitHub,
			Category:    "workflow_complete",
			Version:     schemaVersion,
			Description: "GitHub issue-to-PR workflow completion summary",
			Factory:     func() any { return &WorkflowCompletionPayload{} },
		},
	}

	var errs []error
	for _, r := range registrations {
		if err := reg.Register(r); err != nil {
			errs = append(errs, fmt.Errorf("register %s: %w", r.MessageType(), err))
		}
	}
	return errors.Join(errs...)
}

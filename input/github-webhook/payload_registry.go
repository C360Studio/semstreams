package githubwebhook

import (
	"errors"
	"fmt"

	"github.com/c360studio/semstreams/payloadregistry"
)

// RegisterPayloads registers github webhook payload types
// (IssueEvent, PREvent, ReviewEvent, CommentEvent) with the supplied
// registry. Called from payloadbuiltins.Register at process bootstrap.
func RegisterPayloads(reg *payloadregistry.Registry) error {
	registrations := []*payloadregistry.Registration{
		{
			Domain:      "github",
			Category:    "issue_event",
			Version:     "v1",
			Description: "GitHub issue webhook event",
			Factory:     func() any { return &IssueEvent{} },
		},
		{
			Domain:      "github",
			Category:    "pr_event",
			Version:     "v1",
			Description: "GitHub PR webhook event",
			Factory:     func() any { return &PREvent{} },
		},
		{
			Domain:      "github",
			Category:    "review_event",
			Version:     "v1",
			Description: "GitHub review webhook event",
			Factory:     func() any { return &ReviewEvent{} },
		},
		{
			Domain:      "github",
			Category:    "comment_event",
			Version:     "v1",
			Description: "GitHub comment webhook event",
			Factory:     func() any { return &CommentEvent{} },
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

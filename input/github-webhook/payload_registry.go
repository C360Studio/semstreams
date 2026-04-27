package githubwebhook

import "github.com/c360studio/semstreams/payloadregistry"

func init() {
	registrations := []payloadregistry.Registration{
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

	for idx := range registrations {
		r := &registrations[idx]
		if err := payloadregistry.Register(r); err != nil {
			panic("failed to register github payload: " + err.Error())
		}
	}
}

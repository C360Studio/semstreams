package e2eboot

import (
	"context"
	"fmt"
	"io"
	"log/slog"

	"github.com/c360studio/semstreams/internal/boot"
	"github.com/c360studio/semstreams/natsclient"
	"github.com/c360studio/semstreams/pkg/projection"
	agentictools "github.com/c360studio/semstreams/processor/agentic-tools"
	"github.com/c360studio/semstreams/test/e2e/harness/lessoncuration"
)

// enableLessonCuration subscribes the ops tier's lesson-curation control
// responder, the only consumer of lessoncuration.SubjectPromote.
func enableLessonCuration(opts *boot.Options, _ string) {
	opts.Responders = append(opts.Responders, subscribeLessonCuration)
}

func subscribeLessonCuration(
	ctx context.Context,
	client *natsclient.Client,
	mutationClient *projection.MutationClient,
	logger *slog.Logger,
) (io.Closer, error) {
	curator := agentictools.NewLessonCurator(mutationClient, mutationClient, logger)
	subscription, err := client.SubscribeForRequests(
		ctx, lessoncuration.SubjectPromote, lessoncuration.Handler(curator),
	)
	if err != nil {
		return nil, fmt.Errorf("subscribe E2E lesson curation control: %w", err)
	}
	return unsubscriber{subscription}, nil
}

// unsubscriber closes a request subscription by unsubscribing it.
type unsubscriber struct{ subscription *natsclient.Subscription }

func (u unsubscriber) Close() error { return u.subscription.Unsubscribe() }

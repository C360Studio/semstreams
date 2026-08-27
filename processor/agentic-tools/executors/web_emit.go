package executors

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"

	"github.com/c360studio/semstreams/agentic"
	"github.com/c360studio/semstreams/graph"
	"github.com/c360studio/semstreams/message"
	"github.com/c360studio/semstreams/pkg/errs"
	agentictools "github.com/c360studio/semstreams/processor/agentic-tools"
)

// webEmitTimeout bounds how long the per-tool emission path will wait
// for graph-ingest before giving up. The emission ctx is detached from
// the caller's ctx (context.WithoutCancel) so an upstream agent-loop
// cancellation doesn't half-write batches; this timeout prevents a
// wedged graph-ingest from leaking the goroutine. Per the 2026-05-11
// design decision: emission is opportunistic substrate observation,
// not the LLM-facing contract.
const webEmitTimeout = 5 * time.Second

// webEmitFailuresTotal counts per-result triple emission failures from
// the web_search and http_request executors, labelled by tool and
// failure class. Operators alert on rate-of-change here: a non-zero
// entity_id rate signals malformed URLs the LLM is producing (worth
// surfacing to persona tuning); a non-zero publish rate signals
// graph-ingest / NATS infrastructure trouble (worth paging).
//
// Reasons are a closed set:
//   - "entity_id" — TryWebObservationEntityID rejected the URL
//   - "validate"  — WebObservationEntity.Validate rejected the observation (ADR-103)
//   - "publish"   — TriplePublisher.AddTriple returned an error
//
// Per the 2026-05-11 design decision, both tools log+counter+continue
// rather than failing the tool — graph emission is additive observation,
// not the primary contract.
var webEmitFailuresTotal = promauto.NewCounterVec(
	prometheus.CounterOpts{
		Namespace: "agentic_tool_web",
		Name:      "emit_failures_total",
		Help:      "Number of per-result triple emission failures from web_search/http_request, labelled by tool and reason. Non-zero entity_id rate suggests malformed URLs (persona tuning); non-zero publish rate suggests graph-ingest infra (page).",
	},
	[]string{"tool", "reason"},
)

// publishWebObservation makes entity lifecycle explicit. A definite create
// conflict means the stable URL entity already exists, so this component
// appends the new observation. Transport ambiguity is returned without a
// second mutation attempt.
func publishWebObservation(
	ctx context.Context,
	publisher agentictools.TriplePublisher,
	entityID string,
	triples []message.Triple,
) error {
	err := publisher.Create(ctx, entityID, agentic.WebObservationMessageType(), triples)
	if err == nil {
		return nil
	}
	var classified *errs.ClassifiedError
	if !errors.As(err, &classified) || classified.Code != graph.ErrorCodeEntityExists {
		return fmt.Errorf("create web observation: %w", err)
	}
	if err := publisher.Append(ctx, triples); err != nil {
		return fmt.Errorf("append existing web observation: %w", err)
	}
	return nil
}

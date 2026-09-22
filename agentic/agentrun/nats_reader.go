// Package agentrun contains the NATS-backed exact-read adapter used to resolve
// agent-run ancestry from graph authority.
package agentrun

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/c360studio/semstreams/graph"
	"github.com/c360studio/semstreams/natsclient"
	semerrs "github.com/c360studio/semstreams/pkg/errs"
	agvocab "github.com/c360studio/semstreams/vocabulary/agentic"
)

const natsExactReadTimeout = 5 * time.Second

// NATSLoopTripleReader implements LoopTripleReader through the exact authority
// operation. It owns no KV handle and has no raw-storage fallback.
//
// Satisfies the LoopTripleReader interface required by ResolveRun and
// MilestoneSubscriber.
type NATSLoopTripleReader struct {
	reader graph.ExactEntityReader
}

// NewNATSLoopTripleReader constructs a LoopTripleReader backed by the exact
// graph authority operation.
func NewNATSLoopTripleReader(client *natsclient.Client) *NATSLoopTripleReader {
	return &NATSLoopTripleReader{reader: graph.NewExactEntityReader(client, natsExactReadTimeout)}
}

// GetLoopRunID reads the agent.loop.run triple from the given loop entity ID.
// Returns ("", false, nil) when the triple is absent (not an error).
// Returns ("", false, err) on NATS or decode failures.
func (r *NATSLoopTripleReader) GetLoopRunID(ctx context.Context, loopEntityID string) (string, bool, error) {
	return r.getStringTriple(ctx, loopEntityID, agvocab.LoopRun)
}

// GetLoopParentEntityID reads the agent.loop.parent triple from the given
// loop entity ID. Returns ("", false, nil) when absent.
func (r *NATSLoopTripleReader) GetLoopParentEntityID(ctx context.Context, loopEntityID string) (string, bool, error) {
	return r.getStringTriple(ctx, loopEntityID, agvocab.LoopParent)
}

// getStringTriple is the shared exact-read path for both triple-reading methods.
func (r *NATSLoopTripleReader) getStringTriple(ctx context.Context, entityID, predicate string) (string, bool, error) {
	if r == nil || r.reader == nil {
		return "", false, errors.New("agentrun: NATSLoopTripleReader: exact reader is required")
	}
	exact, err := r.reader.ReadExactEntity(ctx, entityID)
	if err != nil {
		var classified *semerrs.ClassifiedError
		if errors.As(err, &classified) && classified.Code == graph.ErrorCodeEntityNotFound {
			// Entity not yet in graph — triple is absent.
			return "", false, nil
		}
		return "", false, fmt.Errorf("agentrun: NATSLoopTripleReader: exact read %q: %w", entityID, err)
	}

	val, ok := exact.Entity.GetPropertyValue(predicate)
	if !ok {
		return "", false, nil
	}
	s, ok := val.(string)
	if !ok {
		// Deterministic AND matchable (invariant I7): the same stored triple
		// decodes to the same non-string value on every attempt, so the
		// delivery that read it terminates instead of retrying to its
		// MaxDeliver. Unwrapped it would reach errs.Classify's unknown
		// default and settle as a bounded Retry, and IsTransient's substring
		// pass would make even that depend on whether the interpolated entity
		// ID happened to contain "network" or "timeout".
		return "", false, semerrs.WrapInvalid(
			fmt.Errorf("agentrun: NATSLoopTripleReader: predicate %q on entity %q has non-string value %T",
				predicate, entityID, val),
			"agentrun", "NATSLoopTripleReader", "read string triple")
	}
	return s, true, nil
}

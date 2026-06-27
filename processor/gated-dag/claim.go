package gateddagexec

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/c360studio/semstreams/graph"
	"github.com/c360studio/semstreams/message"
	"github.com/c360studio/semstreams/natsclient"
)

// subjectUpdateWithTriples is the mutation subject used to commit the durable
// claim marker. It is the same atomic replace-by-predicate path replace_owned
// uses (graph-ingest SubjectEntityUpdateWithTriples).
const subjectUpdateWithTriples = "graph.mutation.entity.update_with_triples"

// claimMutationTimeout bounds a single claim round-trip.
const claimMutationTimeout = 5 * time.Second

// claimer commits the durable in-flight claim marker on a unit BEFORE its
// dispatch is published (ADR-046 invariant #2). It is an interface so unit
// tests record claim ordering against a fake.
type claimer interface {
	// Claim writes the claim marker on unitID. Returns an error if the write
	// fails (the caller then does NOT publish — the unit is retried next eval).
	Claim(ctx context.Context, unitID string) error
}

// natsClaimer writes the claim via the atomic replace-by-predicate mutation
// (RemoveTriples drops any prior claim, AddTriples sets the new one — a watcher
// never sees the predicate absent between the two).
type natsClaimer struct {
	nc        *natsclient.Client
	predicate string
}

// Claim commits the durable claim marker.
//
// ExpectedRevision is left zero: this is owned-current-state reconciliation
// (last-write-wins), NOT a CAS transition. Mutual exclusion comes from
// single-flight execution + one instance per fan-out (ADR-046 invariant #1), not
// from this write.
//
// CAS-UPGRADE POINT: if cross-writer / multi-instance exclusion is ever needed,
// switch this to an ExpectedRevision-based CAS write — populate
// graph.UpdateEntityWithTriplesRequest.ExpectedRevision with the unit's read
// revision and let the graph-ingest handler reject a stale claim. The brain and
// the rest of the executor are unaffected.
func (c *natsClaimer) Claim(ctx context.Context, unitID string) error {
	req := graph.UpdateEntityWithTriplesRequest{
		Entity:        &graph.EntityState{ID: unitID},
		RemoveTriples: []string{c.predicate},
		AddTriples: []message.Triple{{
			Subject:    unitID,
			Predicate:  c.predicate,
			Object:     unitID,
			Source:     "gateddag-executor",
			Timestamp:  time.Now(),
			Confidence: 1.0,
		}},
		// OwnerToken empty: the lease check is skipped (two-state contract). We do
		// not rely on the lease for exclusion — see invariant #1.
	}
	reqData, err := json.Marshal(req)
	if err != nil {
		return fmt.Errorf("marshal claim request: %w", err)
	}
	// Retry-classified: the replace is idempotent (replace-by-predicate converges
	// to the same single-valued state on a duplicate delivery), so retry is safe.
	respData, err := c.nc.RequestWithRetryClassified(ctx, subjectUpdateWithTriples, reqData, claimMutationTimeout, natsclient.DefaultRetryConfig())
	if err != nil {
		return fmt.Errorf("claim mutation failed for %s: %w", unitID, err)
	}
	var resp graph.UpdateEntityWithTriplesResponse
	if err := json.Unmarshal(respData, &resp); err != nil {
		return fmt.Errorf("unmarshal claim response for %s: %w", unitID, err)
	}
	return nil
}

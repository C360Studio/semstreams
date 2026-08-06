package gateddagexec

import (
	"context"
	"fmt"
	"time"

	"github.com/c360studio/semstreams/graph"
	"github.com/c360studio/semstreams/internal/graphmutation"
	"github.com/c360studio/semstreams/message"
	"github.com/c360studio/semstreams/natsclient"
)

const claimMutationTimeout = 5 * time.Second

type claimer interface {
	Claim(context.Context, string) error
	Unclaim(context.Context, string) error
}

type natsClaimer struct {
	mutations *graphmutation.Client
	reader    graph.ExactEntityReader
	predicate string
}

func newNATSClaimer(client *natsclient.Client, predicate string) *natsClaimer {
	mutations, _ := graphmutation.NewClient(client, claimMutationTimeout)
	return &natsClaimer{
		mutations: mutations,
		reader:    graph.NewExactEntityReader(client, claimMutationTimeout),
		predicate: predicate,
	}
}

func (c *natsClaimer) Claim(ctx context.Context, unitID string) error {
	return c.reconcile(ctx, unitID, []message.Triple{{
		Subject: unitID, Predicate: c.predicate, Object: unitID,
		Source: "gateddag-executor", Timestamp: time.Now(), Confidence: 1.0,
	}})
}

func (c *natsClaimer) Unclaim(ctx context.Context, unitID string) error {
	return c.reconcile(ctx, unitID, nil)
}

func (c *natsClaimer) reconcile(ctx context.Context, unitID string, desired []message.Triple) error {
	if c == nil || c.mutations == nil || c.reader == nil {
		return fmt.Errorf("claim mutation client is unavailable")
	}
	exact, err := c.reader.ReadExactEntity(ctx, unitID)
	if err != nil {
		return fmt.Errorf("read unit %s before claim reconcile: %w", unitID, err)
	}
	_, err = c.mutations.Reconcile(ctx, graph.ReconcilePredicatesRequest{
		EntityID: unitID, ExpectedRevision: exact.KVRevision,
		Predicates: []string{c.predicate}, Desired: desired,
	})
	if err != nil {
		return fmt.Errorf("claim reconcile failed for %s: %w", unitID, err)
	}
	return nil
}

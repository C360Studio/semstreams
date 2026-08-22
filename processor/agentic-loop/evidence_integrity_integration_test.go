//go:build integration

package agenticloop

import (
	"context"
	"encoding/json"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/c360studio/semstreams/agentic"
	gtypes "github.com/c360studio/semstreams/graph"
	"github.com/c360studio/semstreams/message"
	"github.com/c360studio/semstreams/natsclient"
	"github.com/c360studio/semstreams/types"
	agvocab "github.com/c360studio/semstreams/vocabulary/agentic"
	"github.com/stretchr/testify/require"
)

// appendCollector answers the canonical graph.mutation.triple.append
// operation and records each request whole, so a test can assert not just
// "the condition was written" but "it was written on the SAME mutation as
// agent.loop.outcome".
//
// Deliberately internal (package agenticloop): these tests drive the
// component seam — observation -> per-loop marker -> terminal stamp — which
// needs a Component wired to a real graphWriter, and adding an exported
// test constructor for that would put scaffolding on the production
// package's surface.
type appendCollector struct {
	mu       sync.Mutex
	requests [][]message.Triple
}

func (a *appendCollector) handler(_ context.Context, data []byte) ([]byte, error) {
	var req gtypes.AppendTriplesRequest
	if err := json.Unmarshal(data, &req); err != nil {
		return nil, err
	}

	a.mu.Lock()
	a.requests = append(a.requests, req.Triples)
	a.mu.Unlock()

	seen := make(map[string]struct{})
	results := make([]gtypes.AppendSubjectResult, 0, len(req.Triples))
	for _, triple := range req.Triples {
		if _, ok := seen[triple.Subject]; ok {
			continue
		}
		seen[triple.Subject] = struct{}{}
		results = append(results, gtypes.AppendSubjectResult{
			EntityID: triple.Subject, Outcome: gtypes.MutationApplied, KVRevision: 1,
		})
	}
	return json.Marshal(gtypes.AppendTriplesResponse{Results: results})
}

func (a *appendCollector) subscribe(t *testing.T, ctx context.Context, client *natsclient.Client) {
	t.Helper()
	_, err := client.SubscribeForRequests(ctx, "graph.mutation.triple.append", a.handler)
	require.NoError(t, err, "subscribe append")
}

// conditionRequests returns the recorded append requests that carry
// agent.loop.outcome, plus how many of those also carry the
// evidence-integrity condition.
func (a *appendCollector) outcomeRequests() (outcome int, withCondition int, conditionValues []any) {
	a.mu.Lock()
	defer a.mu.Unlock()
	for _, triples := range a.requests {
		var hasOutcome bool
		var conditions []any
		for _, tr := range triples {
			switch tr.Predicate {
			case agvocab.LoopOutcome:
				hasOutcome = true
			case agvocab.LoopEvidenceIntegrity:
				conditions = append(conditions, tr.Object)
			}
		}
		if !hasOutcome {
			continue
		}
		outcome++
		if len(conditions) > 0 {
			withCondition++
			conditionValues = append(conditionValues, conditions...)
		}
	}
	return outcome, withCondition, conditionValues
}

func newStampTestComponent(t *testing.T, client *natsclient.Client, config Config) *Component {
	t.Helper()
	c := &Component{
		config:  config,
		handler: NewMessageHandler(config),
		logger:  discardLogger(),
	}
	c.graphWriter = &graphWriter{
		natsClient: client,
		platform:   types.PlatformMeta{Org: "acme", Platform: "ops"},
		logger:     discardLogger(),
	}
	return c
}

func observeAuditLoss(t *testing.T, c *Component, loopID string) {
	t.Helper()
	c.reportTrajectoryAuditFailure(trajectoryAuditFailure{
		Stage:  trajectoryStageEvidencePut,
		Kind:   agentic.TrajectoryKindToolCompleted,
		Reason: trajectoryReasonBackend,
		LoopID: loopID,
		Err:    errors.New("object store unavailable"),
	})
	require.True(t, c.trajectoryAuditLoss.observed(loopID))
}

// The wire test for the whole seam: an audit failure observed during a
// loop reaches that loop's terminal graph mutation as
// agent.loop.evidence-integrity="incomplete", on the same append request
// as agent.loop.outcome. Deleting the component's read of the per-loop
// marker (passing a constant to the writer instead) fails this and only
// this class of test — the builder tests cannot see that far.
func TestTerminalStampCarriesObservedAuditLoss_Integration(t *testing.T) {
	t.Run("completion", func(t *testing.T) {
		tc := natsclient.NewTestClient(t, natsclient.WithFastStartup())
		ctx := context.Background()
		collector := &appendCollector{}
		collector.subscribe(t, ctx, tc.Client)

		c := newStampTestComponent(t, tc.Client, DefaultConfig())
		const loopID = "loop-complete-audit"
		_, err := c.handler.trajectoryManager.startTrajectory(loopID)
		require.NoError(t, err)
		observeAuditLoss(t, c, loopID)

		c.persistHandlerResult(ctx, HandlerResult{
			LoopID: loopID,
			State:  agentic.LoopStateComplete,
			CompletionState: &agentic.LoopCompletedEvent{
				LoopID: loopID, Outcome: agentic.OutcomeSuccess, CompletedAt: time.Now(),
			},
		})

		outcome, withCondition, values := collector.outcomeRequests()
		require.Equal(t, 1, outcome, "expected exactly one terminal append carrying agent.loop.outcome")
		require.Equal(t, 1, withCondition, "terminal mutation did not carry the observed-audit-loss condition")
		require.Equal(t, []any{"incomplete"}, values)
	})

	t.Run("failure", func(t *testing.T) {
		tc := natsclient.NewTestClient(t, natsclient.WithFastStartup())
		ctx := context.Background()
		collector := &appendCollector{}
		collector.subscribe(t, ctx, tc.Client)

		c := newStampTestComponent(t, tc.Client, DefaultConfig())
		loopID, err := c.handler.loopManager.CreateLoopWithID("loop-failed-audit", "task", "role", "model")
		require.NoError(t, err)
		_, err = c.handler.trajectoryManager.startTrajectory(loopID)
		require.NoError(t, err)
		entity, err := c.handler.loopManager.GetLoop(loopID)
		require.NoError(t, err)
		observeAuditLoss(t, c, loopID)

		c.handleLoopFailure(ctx, loopID, entity, "test_failure", errors.New("boom"))

		outcome, withCondition, values := collector.outcomeRequests()
		require.Equal(t, 1, outcome, "expected exactly one terminal append carrying agent.loop.outcome")
		require.Equal(t, 1, withCondition, "failed loop's terminal mutation dropped the condition")
		require.Equal(t, []any{"incomplete"}, values)
	})

	t.Run("cancellation", func(t *testing.T) {
		tc := natsclient.NewTestClient(t,
			natsclient.WithFastStartup(),
			natsclient.WithStreams(natsclient.TestStreamConfig{Name: "AGENT", Subjects: []string{"agent.>"}}),
		)
		ctx := context.Background()
		collector := &appendCollector{}
		collector.subscribe(t, ctx, tc.Client)

		c := newStampTestComponent(t, tc.Client, DefaultConfig())
		c.natsClient = tc.Client
		loopID, err := c.handler.loopManager.CreateLoopWithID("loop-cancelled-audit", "task", "role", "model")
		require.NoError(t, err)
		_, err = c.handler.trajectoryManager.startTrajectory(loopID)
		require.NoError(t, err)
		observeAuditLoss(t, c, loopID)

		c.handleCancelSignal(ctx, agentic.UserSignal{
			LoopID: loopID, Type: agentic.SignalCancel, UserID: "operator",
		})

		outcome, withCondition, values := collector.outcomeRequests()
		require.Equal(t, 1, outcome, "expected exactly one terminal append carrying agent.loop.outcome")
		require.Equal(t, 1, withCondition, "cancelled loop's terminal mutation dropped the condition")
		require.Equal(t, []any{"incomplete"}, values)
	})
}

// The negative half of the same seam: a loop with no observed audit
// failure reaches its terminal mutation carrying no claim at all. Guards
// against a stamp that fires unconditionally, which would make the
// predicate meaningless.
func TestTerminalStampOmitsConditionWithoutObservedLoss_Integration(t *testing.T) {
	tc := natsclient.NewTestClient(t, natsclient.WithFastStartup())
	ctx := context.Background()
	collector := &appendCollector{}
	collector.subscribe(t, ctx, tc.Client)

	c := newStampTestComponent(t, tc.Client, DefaultConfig())
	const loopID = "loop-clean-audit"
	_, err := c.handler.trajectoryManager.startTrajectory(loopID)
	require.NoError(t, err)

	c.persistHandlerResult(ctx, HandlerResult{
		LoopID: loopID,
		State:  agentic.LoopStateComplete,
		CompletionState: &agentic.LoopCompletedEvent{
			LoopID: loopID, Outcome: agentic.OutcomeSuccess, CompletedAt: time.Now(),
		},
	})

	outcome, withCondition, _ := collector.outcomeRequests()
	require.Equal(t, 1, outcome, "expected exactly one terminal append carrying agent.loop.outcome")
	require.Zero(t, withCondition, "clean loop was stamped with an audit-loss condition")
}

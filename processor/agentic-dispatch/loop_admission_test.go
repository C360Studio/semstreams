package agenticdispatch

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"strings"
	"testing"

	"github.com/c360studio/semstreams/agentic"
	"github.com/c360studio/semstreams/metric"
	"github.com/c360studio/semstreams/pkg/errs"
	"github.com/nats-io/nats.go/jetstream"
	"github.com/prometheus/client_golang/prometheus/testutil"
	"github.com/stretchr/testify/require"
)

// Canonical loop tokens for the gate tests. Fixed rather than minted so a
// failure names the same token every run.
const (
	admissionLoopA = "3f2504e0-4f89-41d3-9a0c-0305e82c3301"
	admissionLoopB = "6ba7b810-9dad-11d1-80b4-00c04fd430c8"
	// admissionMalformed is the shape #1228 reports: a truncated, prefixed
	// token that is not a canonical UUID.
	admissionMalformed = "loop_ab12cd34"
)

func admissionTestComponent(t *testing.T) *Component {
	t.Helper()
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	return &Component{
		config:  DefaultConfig(),
		logger:  logger,
		metrics: getMetrics(metric.NewMetricsRegistry()),
	}
}

// absentRecord is what a real AGENT_LOOPS read returns for a key that is not
// there, wrapped exactly as loadPersistedLoop wraps it.
func absentRecord(loopID string) error {
	return fmt.Errorf("loop state %q not yet observable: %w", loopID, jetstream.ErrKeyNotFound)
}

func withPersistedLoops(c *Component, records map[string]*agentic.LoopEntity) {
	c.loadPersistedLoopFn = func(_ context.Context, loopID string) (*agentic.LoopEntity, error) {
		if record, ok := records[loopID]; ok {
			return record, nil
		}
		return nil, absentRecord(loopID)
	}
}

// requireRefusal asserts the error is the gate's classified refusal carrying
// the given code, and that exactly ONE refusal was counted with the given
// reason label (I3). CollectAndCount proves no second series moved.
func requireRefusal(t *testing.T, c *Component, err error, wantCode, wantReason, wantSeam string) {
	t.Helper()
	require.Error(t, err)

	var classified *errs.ClassifiedError
	require.ErrorAs(t, err, &classified, "every refusal is a classified error")
	require.Equal(t, wantCode, classified.Code)
	require.Equal(t, wantSeam, classified.Detail[detailSeam])
	require.NotEmpty(t, classified.Detail[detailField], "a refusal names the failing field")

	require.Equal(t, 1, testutil.CollectAndCount(c.metrics.loopAdmissionRefusals),
		"exactly one refusal series moved")
	require.Equal(t, 1.0,
		testutil.ToFloat64(c.metrics.loopAdmissionRefusals.WithLabelValues(wantSeam, wantReason)),
		"exactly one increment on %s/%s", wantSeam, wantReason)
}

// spec: agentic-dispatch / One gate admits every request that names an existing loop
// I1: a malformed token is refused with the form reason whatever the loop's
// existence or ownership. Both later checks would ALSO refuse this request, so
// the test only passes if form runs first.
func TestFormRefusalPrecedesExistenceRefusal(t *testing.T) {
	c := admissionTestComponent(t)
	withPersistedLoops(c, nil) // nothing exists: existence would refuse too

	_, err := c.admitLoopRequest(context.Background(), loopAdmissionRequest{
		Seam:      "channel_submission",
		Field:     "reply_to",
		Operation: loopOpContinue,
		LoopID:    admissionMalformed,
		Requester: "user-a",
	})

	requireRefusal(t, c, err, codeLoopTokenInvalid, reasonFormMalformed, "channel_submission")
	require.NotContains(t, err.Error(), "names no loop",
		"a malformed token is never answered as not found")
}

// spec: agentic-dispatch / One gate admits every request that names an existing loop
// I2: an absent loop is refused as absent whatever the requester. The requester
// owns no such loop, so the ownership check would ALSO refuse.
func TestExistenceRefusalPrecedesOwnershipRefusal(t *testing.T) {
	c := admissionTestComponent(t)
	withPersistedLoops(c, nil)

	_, err := c.admitLoopRequest(context.Background(), loopAdmissionRequest{
		Seam:      "http_submission",
		Field:     "reply_to",
		Operation: loopOpContinue,
		LoopID:    admissionLoopA,
		Requester: "stranger",
	})

	requireRefusal(t, c, err, codeLoopNotFound, reasonExistenceAbsent, "http_submission")
	require.NotContains(t, err.Error(), "does not own",
		"an absent loop is never answered as not yours")
}

// spec: agentic-dispatch / One gate admits every request that names an existing loop
// I3: each refusal emitted by the exact-authority gate moves exactly one series
// by exactly one and produces one diagnostic log.
func TestGateRefusalIsCountedExactlyOnce(t *testing.T) {
	owned := &agentic.LoopEntity{ID: admissionLoopA, UserID: "user-a", State: agentic.LoopStateExecuting, MaxIterations: 5}
	settled := &agentic.LoopEntity{ID: admissionLoopA, UserID: "user-a", State: agentic.LoopStateComplete, MaxIterations: 5}

	cases := []struct {
		name    string
		arrange func(*Component)
		req     loopAdmissionRequest
		code    string
		reason  string
	}{
		{
			name:    "malformed token",
			arrange: func(c *Component) { withPersistedLoops(c, nil) },
			req:     loopAdmissionRequest{Operation: loopOpContinue, LoopID: admissionMalformed, Requester: "user-a"},
			code:    codeLoopTokenInvalid, reason: reasonFormMalformed,
		},
		{
			name:    "absent loop",
			arrange: func(c *Component) { withPersistedLoops(c, nil) },
			req:     loopAdmissionRequest{Operation: loopOpContinue, LoopID: admissionLoopA, Requester: "user-a"},
			code:    codeLoopNotFound, reason: reasonExistenceAbsent,
		},
		{
			name: "unreadable record",
			arrange: func(c *Component) {
				c.loadPersistedLoopFn = func(context.Context, string) (*agentic.LoopEntity, error) {
					return nil, errors.New("nats unavailable")
				}
			},
			req:  loopAdmissionRequest{Operation: loopOpContinue, LoopID: admissionLoopA, Requester: "user-a"},
			code: codeLoopUnreadable, reason: reasonExistenceUnreadable,
		},
		{
			name: "invalid record identity",
			arrange: func(c *Component) {
				withPersistedLoops(c, map[string]*agentic.LoopEntity{admissionLoopA: {
					ID: admissionLoopB, UserID: "user-a", State: agentic.LoopStateExecuting, MaxIterations: 5,
				}})
			},
			req:  loopAdmissionRequest{Operation: loopOpContinue, LoopID: admissionLoopA, Requester: "user-a"},
			code: codeLoopUnreadable, reason: reasonExistenceUnreadable,
		},
		{
			name: "terminal loop",
			arrange: func(c *Component) {
				withPersistedLoops(c, map[string]*agentic.LoopEntity{admissionLoopA: settled})
			},
			req:  loopAdmissionRequest{Operation: loopOpContinue, LoopID: admissionLoopA, Requester: "user-a"},
			code: codeLoopTerminal, reason: reasonStateTerminal,
		},
		{
			name: "not owned",
			arrange: func(c *Component) {
				withPersistedLoops(c, map[string]*agentic.LoopEntity{admissionLoopA: owned})
			},
			req:  loopAdmissionRequest{Operation: loopOpContinue, LoopID: admissionLoopA, Requester: "user-b"},
			code: codeLoopNotOwned, reason: reasonOwnershipNotOwner,
		},
		{
			name: "not permitted",
			arrange: func(c *Component) {
				c.config.Permissions.Approve = []string{"reviewer-b"}
				withPersistedLoops(c, map[string]*agentic.LoopEntity{admissionLoopA: owned})
			},
			req:  loopAdmissionRequest{Operation: loopOpApprove, LoopID: admissionLoopA, Requester: "stranger-c"},
			code: codeLoopNotPermitted, reason: reasonOwnershipNotPermitted,
		},
		{
			name: "unknown operation fails closed",
			arrange: func(c *Component) {
				withPersistedLoops(c, map[string]*agentic.LoopEntity{admissionLoopA: owned})
			},
			req:  loopAdmissionRequest{Operation: "rename", LoopID: admissionLoopA, Requester: "user-a"},
			code: codeLoopNotPermitted, reason: reasonOwnershipNotPermitted,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			c := admissionTestComponent(t)
			var logs bytes.Buffer
			c.logger = slog.New(slog.NewTextHandler(&logs, nil))
			tc.arrange(c)
			req := tc.req
			req.Seam = "seam_under_test"
			req.Field = "reply_to"

			_, err := c.admitLoopRequest(context.Background(), req)

			requireRefusal(t, c, err, tc.code, tc.reason, "seam_under_test")
			require.Equal(t, 1, strings.Count(logs.String(), loopAdmissionRefusalLogMessage))
			require.Contains(t, logs.String(), "reason="+tc.reason)
			require.Contains(t, logs.String(), "seam=seam_under_test")
			require.Contains(t, logs.String(), "field=reply_to")
		})
	}
}

// spec: agentic-dispatch / Loop existence and ownership are merged facts, never process memory alone
func TestContinuationAfterReplacementIsAdmittedFromDurableRecord(t *testing.T) {
	c := admissionTestComponent(t)
	// A replacement process needs only the exact current authority.
	withPersistedLoops(c, map[string]*agentic.LoopEntity{admissionLoopA: {
		ID: admissionLoopA, UserID: "user-a", ChannelType: "slack", ChannelID: "C1",
		State: agentic.LoopStateExecuting, MaxIterations: 5,
	}})

	facts, err := c.admitLoopRequest(context.Background(), loopAdmissionRequest{
		Seam: "channel_submission", Field: "reply_to", Operation: loopOpContinue,
		LoopID: admissionLoopA, Requester: "user-a",
	})

	require.NoError(t, err)
	require.Equal(t, "user-a", facts.UserID)
	require.Equal(t, "slack", facts.ChannelType)
	require.Equal(t, "C1", facts.ChannelID)
	require.Equal(t, agentic.LoopStateExecuting, facts.State)
	require.False(t, facts.Terminal)
	require.Equal(t, 0, testutil.CollectAndCount(c.metrics.loopAdmissionRefusals))
}

// spec: agentic-dispatch / Loop existence and ownership are merged facts, never process memory alone
func TestPreviouslyObservedLoopWithoutDurableRecordIsRefused(t *testing.T) {
	c := admissionTestComponent(t)
	records := map[string]*agentic.LoopEntity{admissionLoopA: {
		ID: admissionLoopA, UserID: "user-a", ChannelType: "cli", ChannelID: "s1",
		State: agentic.LoopStateExecuting, MaxIterations: 5,
	}}
	withPersistedLoops(c, records)
	req := loopAdmissionRequest{
		Seam: "channel_submission", Field: "reply_to", Operation: loopOpContinue,
		LoopID: admissionLoopA, Requester: "user-a",
	}
	_, err := c.admitLoopRequest(context.Background(), req)
	require.NoError(t, err)
	delete(records, admissionLoopA)

	facts, err := c.admitLoopRequest(context.Background(), req)

	requireRefusal(t, c, err, codeLoopNotFound, reasonExistenceAbsent, "channel_submission")
	require.Equal(t, loopFacts{}, facts, "a prior observation cannot establish current existence")
}

// spec: agentic-dispatch / Loop existence and ownership are merged facts, never process memory alone
func TestUnreadableDurableRecordRefusesTransient(t *testing.T) {
	c := admissionTestComponent(t)
	c.loadPersistedLoopFn = func(context.Context, string) (*agentic.LoopEntity, error) {
		return nil, errors.New("access AGENT_LOOPS: connection refused")
	}

	_, err := c.admitLoopRequest(context.Background(), loopAdmissionRequest{
		Seam: "http_submission", Field: "reply_to", Operation: loopOpContinue,
		LoopID: admissionLoopA, Requester: "user-a",
	})

	requireRefusal(t, c, err, codeLoopUnreadable, reasonExistenceUnreadable, "http_submission")
	require.True(t, errs.IsTransient(err), "an unread record is answerable later, not a hard refusal")

	var classified *errs.ClassifiedError
	require.ErrorAs(t, err, &classified)
	require.NotEqual(t, codeLoopNotFound, classified.Code, "an outage is never answered as not found")
}

// spec: agentic-dispatch / Loop existence and ownership are merged facts, never process memory alone
func TestPriorAdmissionDoesNotBypassADurableReadFailure(t *testing.T) {
	c := admissionTestComponent(t)
	withPersistedLoops(c, map[string]*agentic.LoopEntity{admissionLoopA: {
		ID: admissionLoopA, UserID: "user-a", State: agentic.LoopStateExecuting, MaxIterations: 5,
	}})
	req := loopAdmissionRequest{
		Seam: "channel_submission", Field: "reply_to", Operation: loopOpContinue,
		LoopID: admissionLoopA, Requester: "user-a",
	}
	_, err := c.admitLoopRequest(context.Background(), req)
	require.NoError(t, err)
	c.loadPersistedLoopFn = func(context.Context, string) (*agentic.LoopEntity, error) {
		return nil, errors.New("access AGENT_LOOPS: connection refused")
	}

	facts, err := c.admitLoopRequest(context.Background(), req)

	requireRefusal(t, c, err, codeLoopUnreadable, reasonExistenceUnreadable, "channel_submission")
	require.True(t, errs.IsTransient(err))
	require.Equal(t, loopFacts{}, facts, "a prior admission cannot provide fallback ownership")
}

// spec: agentic-dispatch / Loop existence and ownership are merged facts, never process memory alone
func TestCurrentOwnerReplacesPreviouslyObservedOwner(t *testing.T) {
	c := admissionTestComponent(t)
	records := map[string]*agentic.LoopEntity{admissionLoopA: {
		ID: admissionLoopA, UserID: "user-a", State: agentic.LoopStateExecuting, MaxIterations: 5,
	}}
	withPersistedLoops(c, records)
	req := loopAdmissionRequest{
		Seam: seamCancelCommand, Field: "loop_id", Operation: loopOpCancel,
		LoopID: admissionLoopA, Requester: "user-a",
	}
	_, err := c.admitLoopRequest(context.Background(), req)
	require.NoError(t, err)
	records[admissionLoopA] = &agentic.LoopEntity{
		ID: admissionLoopA, UserID: "user-b", State: agentic.LoopStateExecuting, MaxIterations: 5,
	}

	_, err = c.admitLoopRequest(context.Background(), req)
	requireRefusal(t, c, err, codeLoopNotOwned, reasonOwnershipNotOwner, seamCancelCommand)

	req.Requester = "user-b"
	facts, err := c.admitLoopRequest(context.Background(), req)
	require.NoError(t, err)
	require.Equal(t, "user-b", facts.UserID, "only the current authority establishes ownership")
}

// The gate's ownership model, exercised directly on an EXISTING loop. The seam
// tests in section 4 prove each seam routes here; these prove what "here"
// decides.
func TestGateOwnershipModel(t *testing.T) {
	const owner = "user-a"

	cases := []struct {
		name      string
		operation string
		requester string
		// loopOwner is the owner recorded in current authority; "" is the
		// system-lane (ownerless) loop.
		loopOwner string
		cancelAny []string
		approve   []string
		wantCode  string
	}{
		{name: "owner continues", operation: loopOpContinue, requester: owner, loopOwner: owner},
		{name: "second holder cannot continue", operation: loopOpContinue, requester: "user-b", loopOwner: owner, wantCode: codeLoopNotOwned},
		{name: "owner cancels", operation: loopOpCancel, requester: owner, loopOwner: owner},
		{name: "non-owner cannot cancel", operation: loopOpCancel, requester: "user-b", loopOwner: owner, wantCode: codeLoopNotOwned},
		{name: "cancel_any cancels another user's loop", operation: loopOpCancel, requester: "ops", loopOwner: owner, cancelAny: []string{"ops"}},

		// Approval is deliberately NOT owner-scoped: a second-party reviewer is
		// the entire point.
		{name: "approver who does not own is admitted", operation: loopOpApprove, requester: "reviewer-b", loopOwner: owner, approve: []string{"reviewer-b"}},
		{name: "caller outside the approve list is refused", operation: loopOpApprove, requester: "stranger-c", loopOwner: owner, approve: []string{"reviewer-b"}, wantCode: codeLoopNotPermitted},
		{name: "default approve list admits everyone", operation: loopOpApprove, requester: "anyone", loopOwner: owner, approve: []string{"*"}},

		// Read checks form and existence only.
		{name: "read is not owner-scoped", operation: loopOpRead, requester: "user-b", loopOwner: owner},

		// Unknown owner fails closed for every operation that consults it.
		{name: "ownerless loop refuses continue", operation: loopOpContinue, requester: owner, loopOwner: "", wantCode: codeLoopNotOwned},
		{name: "ownerless loop refuses cancel", operation: loopOpCancel, requester: owner, loopOwner: "", wantCode: codeLoopNotOwned},
		// ... but approve and read never consult the owner, so an autonomously
		// spawned loop's tool call is still approvable and its record readable.
		{name: "ownerless loop still approvable", operation: loopOpApprove, requester: "reviewer-b", loopOwner: "", approve: []string{"reviewer-b"}},
		{name: "ownerless loop still readable", operation: loopOpRead, requester: "reviewer-b", loopOwner: ""},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			c := admissionTestComponent(t)
			c.config.Permissions.CancelAny = tc.cancelAny
			if tc.approve != nil {
				c.config.Permissions.Approve = tc.approve
			}
			withPersistedLoops(c, map[string]*agentic.LoopEntity{admissionLoopA: {
				ID: admissionLoopA, UserID: tc.loopOwner, State: agentic.LoopStateExecuting, MaxIterations: 5,
			}})

			facts, err := c.admitLoopRequest(context.Background(), loopAdmissionRequest{
				Seam: "seam_under_test", Field: "id", Operation: tc.operation,
				LoopID: admissionLoopA, Requester: tc.requester,
			})

			if tc.wantCode == "" {
				require.NoError(t, err)
				require.Equal(t, admissionLoopA, facts.LoopID)
				require.Equal(t, 0, testutil.CollectAndCount(c.metrics.loopAdmissionRefusals))
				return
			}
			var classified *errs.ClassifiedError
			require.ErrorAs(t, err, &classified)
			require.Equal(t, tc.wantCode, classified.Code)
			require.Equal(t, 1, testutil.CollectAndCount(c.metrics.loopAdmissionRefusals))
		})
	}
}

// Permissions.CancelOwn keeps exactly one home — the /cancel command's declared
// permission — and this gate is not it (owner ruling R2). With CancelOwn false,
// the gate still admits the owner's own cancel.
func TestGateDoesNotConsultCancelOwn(t *testing.T) {
	c := admissionTestComponent(t)
	c.config.Permissions.CancelOwn = false
	withPersistedLoops(c, map[string]*agentic.LoopEntity{admissionLoopA: {
		ID: admissionLoopA, UserID: "user-a", State: agentic.LoopStateExecuting, MaxIterations: 5,
	}})

	_, err := c.admitLoopRequest(context.Background(), loopAdmissionRequest{
		Seam: "seam_under_test", Field: "id", Operation: loopOpCancel,
		LoopID: admissionLoopA, Requester: "user-a",
	})
	require.NoError(t, err)
}

// spec: agentic-dispatch / Loop existence and ownership are merged facts, never process memory alone
// Terminal authority refuses continuation, but remains readable and controllable.
func TestGateTerminalAuthorityRefusesContinuation(t *testing.T) {
	for _, state := range []agentic.LoopState{
		agentic.LoopStateComplete, agentic.LoopStateFailed, agentic.LoopStateCancelled,
	} {
		t.Run(state.String(), func(t *testing.T) {
			c := admissionTestComponent(t)
			withPersistedLoops(c, map[string]*agentic.LoopEntity{admissionLoopA: {
				ID: admissionLoopA, UserID: "user-a", State: state, MaxIterations: 5,
			}})

			_, err := c.admitLoopRequest(context.Background(), loopAdmissionRequest{
				Seam: "channel_submission", Field: "reply_to", Operation: loopOpContinue,
				LoopID: admissionLoopA, Requester: "user-a",
			})
			requireRefusal(t, c, err, codeLoopTerminal, reasonStateTerminal, "channel_submission")

			for _, operation := range []string{loopOpRead, loopOpCancel, loopOpApprove} {
				facts, operationErr := c.admitLoopRequest(context.Background(), loopAdmissionRequest{
					Seam: "seam_under_test", Field: "id", Operation: operation,
					LoopID: admissionLoopA, Requester: "user-a",
				})
				require.NoError(t, operationErr, operation)
				require.Equal(t, state, facts.State, operation)
				require.True(t, facts.Terminal, operation)
			}
		})
	}
}

// spec: agentic-dispatch / Loop existence and ownership are merged facts, never process memory alone
func TestGateReportsExactCurrentStateWithoutMutatingAuthority(t *testing.T) {
	for _, state := range []agentic.LoopState{
		agentic.LoopStateExecuting, agentic.LoopStateAwaitingApproval,
		agentic.LoopStateComplete, agentic.LoopStateFailed, agentic.LoopStateCancelled,
	} {
		t.Run(state.String(), func(t *testing.T) {
			c := admissionTestComponent(t)
			record := &agentic.LoopEntity{
				ID: admissionLoopA, UserID: "user-a", State: state, MaxIterations: 5,
			}
			before := *record
			withPersistedLoops(c, map[string]*agentic.LoopEntity{admissionLoopA: record})

			facts, err := c.admitLoopRequest(context.Background(), loopAdmissionRequest{
				Seam: seamStatusCommand, Field: "id", Operation: loopOpRead,
				LoopID: admissionLoopA, Requester: "user-a",
			})

			require.NoError(t, err)
			require.Equal(t, state, facts.State)
			require.Equal(t, before, *record)
			require.Zero(t, testutil.CollectAndCount(c.metrics.loopAdmissionRefusals))
		})
	}
}

// spec: agentic-dispatch / Loop existence and ownership are merged facts, never process memory alone
func TestGateRefusesInvalidCurrentAuthorityBeforeOwnership(t *testing.T) {
	for _, tc := range []struct {
		name   string
		record *agentic.LoopEntity
	}{
		{"missing value", nil},
		{"wrong identity", &agentic.LoopEntity{ID: admissionLoopB, State: agentic.LoopStateExecuting, MaxIterations: 5}},
		{"malformed identity", &agentic.LoopEntity{ID: admissionMalformed, State: agentic.LoopStateExecuting, MaxIterations: 5}},
		{"missing state", &agentic.LoopEntity{ID: admissionLoopA, MaxIterations: 5}},
		{"unknown state", &agentic.LoopEntity{ID: admissionLoopA, State: "unknown", MaxIterations: 5}},
		{"zero iteration budget", &agentic.LoopEntity{ID: admissionLoopA, State: agentic.LoopStateExecuting}},
		{"negative iteration budget", &agentic.LoopEntity{ID: admissionLoopA, State: agentic.LoopStateExecuting, MaxIterations: -1}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			c := admissionTestComponent(t)
			withPersistedLoops(c, map[string]*agentic.LoopEntity{admissionLoopA: tc.record})

			facts, err := c.admitLoopRequest(context.Background(), loopAdmissionRequest{
				Seam: seamChannelSubmission, Field: "reply_to", Operation: loopOpContinue,
				LoopID: admissionLoopA, Requester: "stranger",
			})

			requireRefusal(t, c, err, codeLoopUnreadable, reasonExistenceUnreadable, seamChannelSubmission)
			require.Equal(t, loopFacts{}, facts)
			require.NotContains(t, err.Error(), "does not own", "invalid authority cannot establish ownership")
		})
	}
}

// Every refusal code maps to exactly one reason label, and nothing else does.
// This is the "one home" property the mapper exists for.
func TestLoopAdmissionMetricReasonHasOneHomePerCode(t *testing.T) {
	want := map[string]string{
		codeLoopTokenInvalid:  reasonFormMalformed,
		codeLoopNotFound:      reasonExistenceAbsent,
		codeLoopUnreadable:    reasonExistenceUnreadable,
		codeLoopOwnerConflict: reasonExistenceConflict,
		codeLoopTerminal:      reasonStateTerminal,
		codeLoopNotOwned:      reasonOwnershipNotOwner,
		codeLoopNotPermitted:  reasonOwnershipNotPermitted,
	}
	seen := make(map[string]string, len(want))
	for code, reason := range want {
		got, ok := loopAdmissionMetricReason(
			errs.ClassifiedCodeDetail(errs.ErrorInvalid, code, nil, errors.New("refused")))
		require.True(t, ok, code)
		require.Equal(t, reason, got, code)
		require.NotContains(t, seen, reason, "two codes share reason %q", reason)
		seen[reason] = code
	}

	_, ok := loopAdmissionMetricReason(errors.New("some other failure"))
	require.False(t, ok, "an unclassified error is not an admission refusal")

	_, ok = loopAdmissionMetricReason(
		errs.ClassifiedCodeDetail(errs.ErrorInvalid, "entity_not_found", nil, errors.New("other")))
	require.False(t, ok, "a foreign classified code is not an admission refusal")
}

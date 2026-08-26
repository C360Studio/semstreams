package agenticdispatch

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"

	"github.com/c360studio/semstreams/agentic"
	"github.com/c360studio/semstreams/component"
	"github.com/c360studio/semstreams/internal/agentterminal"
	"github.com/c360studio/semstreams/message"
	"github.com/nats-io/nats.go/jetstream"
)

const terminalResponseIDPrefix = "terminal-user-response:"

type permanentTerminalError struct{ err error }

func (e *permanentTerminalError) Error() string { return e.err.Error() }
func (e *permanentTerminalError) Unwrap() error { return e.err }

func permanentTerminal(format string, args ...any) error {
	return &permanentTerminalError{err: fmt.Errorf(format, args...)}
}

func isPermanentTerminal(err error) bool {
	var target *permanentTerminalError
	return errors.As(err, &target)
}

type terminalRoute struct {
	ChannelType string
	ChannelID   string
	UserID      string
}

func mergeRouteField(name string, values ...string) (string, error) {
	var merged string
	for _, value := range values {
		if value == "" {
			continue
		}
		if merged == "" {
			merged = value
			continue
		}
		if merged != value {
			return "", permanentTerminal("conflicting nonempty %s values", name)
		}
	}
	return merged, nil
}

func reconcileTerminalRoute(tracker *LoopInfo, event agentterminal.Event, persisted *agentic.LoopEntity) (terminalRoute, error) {
	var trackerRoute, persistedRoute terminalRoute
	if tracker != nil {
		trackerRoute = terminalRoute{ChannelType: tracker.ChannelType, ChannelID: tracker.ChannelID, UserID: tracker.UserID}
	}
	if persisted != nil {
		persistedRoute = terminalRoute{ChannelType: persisted.ChannelType, ChannelID: persisted.ChannelID, UserID: persisted.UserID}
	}

	channelType, err := mergeRouteField("channel_type", trackerRoute.ChannelType, event.ChannelType, persistedRoute.ChannelType)
	if err != nil {
		return terminalRoute{}, err
	}
	channelID, err := mergeRouteField("channel_id", trackerRoute.ChannelID, event.ChannelID, persistedRoute.ChannelID)
	if err != nil {
		return terminalRoute{}, err
	}
	userID, err := mergeRouteField("user_id", trackerRoute.UserID, event.UserID, persistedRoute.UserID)
	if err != nil {
		return terminalRoute{}, err
	}
	if (channelType == "") != (channelID == "") {
		return terminalRoute{}, permanentTerminal("malformed partial terminal route")
	}
	return terminalRoute{ChannelType: channelType, ChannelID: channelID, UserID: userID}, nil
}

func (c *Component) loadPersistedLoop(ctx context.Context, loopID string) (*agentic.LoopEntity, error) {
	if c.loadPersistedLoopFn != nil {
		return c.loadPersistedLoopFn(ctx, loopID)
	}
	if c.natsClient == nil {
		return nil, fmt.Errorf("AGENT_LOOPS client unavailable")
	}
	bucket, err := c.loopsBucketName()
	if err != nil {
		return nil, permanentTerminal("resolve agent loops bucket: %w", err)
	}
	kv, err := c.natsClient.GetKeyValueBucket(ctx, bucket)
	if err != nil {
		return nil, fmt.Errorf("access %s: %w", bucket, err)
	}
	entry, err := kv.Get(ctx, loopID)
	if err != nil {
		if isLoopRecordAbsent(err) {
			return nil, fmt.Errorf("loop state %q not yet observable: %w", loopID, err)
		}
		return nil, fmt.Errorf("read %s/%s: %w", bucket, loopID, err)
	}
	var persisted agentic.LoopEntity
	if err := json.Unmarshal(entry.Value(), &persisted); err != nil {
		return nil, permanentTerminal("malformed %s/%s: %w", bucket, loopID, err)
	}
	if persisted.ID != loopID {
		return nil, permanentTerminal("%s/%s contains loop id %q", bucket, loopID, persisted.ID)
	}
	return &persisted, nil
}

func terminalResponse(event agentterminal.Event, route terminalRoute) agentic.UserResponse {
	responseType := agentic.ResponseTypeStatus
	content := fmt.Sprintf("Loop %s cancelled.", event.LoopID)
	switch event.Class {
	case agentterminal.ClassSucceeded:
		responseType = agentic.ResponseTypeResult
		content = event.Result
		if decision := event.Decision; decision != nil && agentic.IsUserFacingDecideAction(decision.Action) {
			// ADR-101: a reply decision's user-facing content IS its reason;
			// Result keeps the full decision JSON for read_loop_result.
			content = decision.Reason
			if decision.Action == agentic.DecideActionAskUser {
				responseType = agentic.ResponseTypePrompt
			}
		}
		if content == "" {
			content = fmt.Sprintf("Loop %s completed.", event.LoopID)
		}
	case agentterminal.ClassFailed:
		responseType = agentic.ResponseTypeError
		content = fmt.Sprintf("Loop %s failed: %s", event.LoopID, event.Error)
	}
	return agentic.UserResponse{
		ResponseID:  terminalResponseIDPrefix + event.SourceMessageID,
		ChannelType: route.ChannelType,
		ChannelID:   route.ChannelID,
		UserID:      route.UserID,
		InReplyTo:   event.LoopID,
		Type:        responseType,
		Content:     content,
		Timestamp:   event.TerminalAt,
	}
}

func (c *Component) publishTerminalResponse(ctx context.Context, response agentic.UserResponse, msgID string) error {
	if c.sendTerminalResponseFn != nil {
		return c.sendTerminalResponseFn(ctx, response, msgID)
	}
	responseMessage := message.NewBaseMessage(response.Schema(), &response, "agentic-dispatch")
	data, err := json.Marshal(responseMessage)
	if err != nil {
		return permanentTerminal("marshal terminal response: %w", err)
	}
	subject, err := component.ResolveSubject(c.outputPortDefs(), "user.response", response.ChannelType+"."+response.ChannelID)
	if err != nil {
		return permanentTerminal("resolve terminal response subject: %w", err)
	}
	if err := c.natsClient.PublishToStreamWithMsgID(ctx, subject, data, msgID); err != nil {
		return fmt.Errorf("publish terminal response: %w", err)
	}
	return nil
}

func (c *Component) settleAgentTerminal(ctx context.Context, data []byte) (settleErr error) {
	reason := ""
	defer func() { c.metrics.recordTerminalSettlement(reason) }()

	event, err := agentterminal.Decode(c.decoder, data)
	if err != nil {
		reason = string(agentterminal.ErrorReason(err))
		return permanentTerminal("normalize terminal: %w", err)
	}

	tracker := c.loopTracker.getSnapshot(event.LoopID)
	persisted, err := c.loadPersistedLoop(ctx, event.LoopID)
	if err != nil {
		if isPermanentTerminal(err) {
			reason = "routing_malformed"
		} else {
			reason = "routing_read_transient"
		}
		return err
	}
	route, err := reconcileTerminalRoute(tracker, event, persisted)
	if err != nil {
		reason = "routing_collision_or_malformed"
		return err
	}

	trackerChanged := false
	if tracker != nil {
		trackerChanged, err = c.loopTracker.updateCompletionAt(event.LoopID, event.Outcome, event.Result, event.Error, event.TerminalAt)
		if err != nil {
			reason = "tracker_projection_collision"
			return permanentTerminal("project tracker terminal: %w", err)
		}
	}
	if trackerChanged {
		c.metrics.recordLoopEnded()
	}
	// Terminal selection follows the typed decision, never route ownership
	// (ADR-101 D2). A decision that is not a reserved reply action is a
	// handoff to a rule chain: it publishes nothing, even when the deciding
	// loop owns a channel — that root handoff being delivered as the user's
	// answer is the defect gh#1094 fixes.
	if decision := event.Decision; decision != nil && !agentic.IsUserFacingDecideAction(decision.Action) {
		c.metrics.recordCompletionReceived(event.Outcome)
		reason = reasonHandoffSettled
		// INFO, not Debug: this is one line per workflow and the only
		// log-visible trace of gh#1094's one behaviour change — a product
		// whose answer action is not one of the two reserved names sees its
		// answer settle here instead of reaching the user, and the metric
		// reason alone does not name the action.
		c.logger.Info("agent terminal settled as a handoff",
			slog.String("loop_id", event.LoopID),
			slog.String("action", decision.Action))
		return nil
	}

	if route.ChannelType == "" {
		userFacing := event.Decision != nil && agentic.IsUserFacingDecideAction(event.Decision.Action)
		if !userFacing {
			c.metrics.recordCompletionReceived(event.Outcome)
			reason = reasonRouteLessSettled
			return nil
		}
		resolved, resolveErr := c.resolveOriginRoute(ctx, persisted)
		if resolveErr != nil {
			if isPermanentTerminal(resolveErr) {
				reason = "routing_malformed"
			} else {
				reason = "routing_read_transient"
			}
			return resolveErr
		}
		if resolved.reason != "" {
			c.metrics.recordCompletionReceived(event.Outcome)
			reason = resolved.reason
			if resolved.reason == reasonOriginUnresolvable {
				c.logger.Warn("origin_unresolvable: "+resolved.detail,
					slog.String("loop_id", event.LoopID),
					slog.String("action", event.Decision.Action))
			}
			return nil
		}
		route = resolved.route
	}

	response := terminalResponse(event, route)
	if err := c.publishTerminalResponse(ctx, response, response.ResponseID); err != nil {
		if isPermanentTerminal(err) {
			reason = "routing_malformed"
			return err
		}
		reason = "response_publish_transient"
		return err
	}
	c.metrics.recordCompletionReceived(event.Outcome)
	reason = "response_settled"
	return nil
}

// maxOriginHops bounds the ancestry walk. Mirrors agentrun.maxAncestryHops:
// the two walkers observe the same ancestry on two planes and must agree on
// how deep a legitimate chain can be.
const maxOriginHops = 32

// Terminal settlement reasons owned by origin resolution (gh#1094). Both are
// fixed labels; the decision action, loop IDs, and run anchors appear only in
// log lines.
const (
	// reasonHandoffSettled: the terminal carried a decision that is not a
	// reserved reply action. It is a handoff to a rule chain, so nothing is
	// published to any channel — including a channel the loop owns.
	reasonHandoffSettled = "handoff_settled"

	// reasonOriginUnresolvable: a durable ancestry link pointed at a record
	// that could not be observed (expired 24h key, or a best-effort Put that
	// never succeeded), or the walk hit a cycle or the hop bound. Recorded
	// ONLY after the parent chain AND every encountered run anchor are
	// exhausted. A retention/persistence alert, unlike route_less_settled.
	reasonOriginUnresolvable = "origin_unresolvable"

	// reasonRouteLessSettled: there was no origin. Pre-existing label, now
	// also the answer for a reply decision whose walk ended at a record with
	// no links and no route (a route-less bus-submitted root, or ancestry
	// severed by a non-loop-entity trigger).
	reasonRouteLessSettled = "route_less_settled"
)

// isLoopRecordAbsent reports whether an AGENT_LOOPS read failed because the
// key is not there — expired after the 24h TTL, or never written because
// persistLoopState is best-effort. Absence is a WALK signal (try the other
// durable link, then settle); every other read failure is transient and is
// redelivered.
func isLoopRecordAbsent(err error) bool {
	return errors.Is(err, jetstream.ErrKeyNotFound) || errors.Is(err, jetstream.ErrKeyDeleted)
}

// recordRoute projects a persisted loop record's route. A record carrying
// exactly one of ChannelType/ChannelID is malformed and is permanently
// rejected, exactly as a partial route on the terminal's own record is.
func recordRoute(record *agentic.LoopEntity) (terminalRoute, bool, error) {
	if record == nil {
		return terminalRoute{}, false, nil
	}
	if (record.ChannelType == "") != (record.ChannelID == "") {
		return terminalRoute{}, false, permanentTerminal("malformed partial route on AGENT_LOOPS/%s", record.ID)
	}
	if record.ChannelType == "" {
		return terminalRoute{}, false, nil
	}
	return terminalRoute{ChannelType: record.ChannelType, ChannelID: record.ChannelID, UserID: record.UserID}, true, nil
}

// originResolution is the outcome of an ancestry walk: either a publishable
// route, or the bounded reason the walk produced instead, with the detail the
// log line needs. Exactly one of route/reason is meaningful.
type originResolution struct {
	route  terminalRoute
	reason string
	detail string
}

// resolveOriginRoute finds the channel a route-less reply decision belongs to
// by observing persisted ancestry in AGENT_LOOPS — the plane that already
// holds both the ancestry and the origin route (ADR-101 D3). It reads only
// persisted records: the process-local tracker is never consulted for an
// ancestor, so a restarted process resolves the same origin.
//
// Order (R4′, mirroring agentrun.ResolveRun's typed-first shape), and it never
// settles while an untried durable link remains:
//
//  1. Typed-first. When the terminal record names a RunID other than itself,
//     read that run root's record: routed -> origin; present but route-less ->
//     continue the parent walk FROM THE ROOT (a routed loop may sit above a
//     product-minted run); absent -> note it and walk from the terminal.
//  2. Parent walk to the nearest routed ancestor. At every hop whose parent
//     key is ABSENT, try the current record's RunID first when it is nonempty,
//     not self, and not yet tried — an intermediate record can carry a run
//     anchor the terminal did not.
//  3. Bounded at maxOriginHops with a visited set.
//
// Walk end with no links, no untried run anchor, and nothing absent is
// route_less_settled — there was no origin. If any durable link pointed at an
// absent record, the answer is origin_unresolvable and the detail names both
// exhaustions.
func (c *Component) resolveOriginRoute(ctx context.Context, terminal *agentic.LoopEntity) (originResolution, error) {
	if terminal == nil {
		return originResolution{reason: reasonRouteLessSettled}, nil
	}

	exhausted := originExhaustion{}
	tried := map[string]struct{}{terminal.ID: {}}
	current := terminal

	// Step 1 — typed-first through the terminal's run anchor.
	if anchor := terminal.RunID; anchor != "" && anchor != terminal.ID {
		tried[anchor] = struct{}{}
		root, err := c.loadPersistedLoop(ctx, anchor)
		switch {
		case err != nil && !isLoopRecordAbsent(err):
			return originResolution{}, err
		case err != nil:
			exhausted.runAnchor = anchor
			exhausted.runAnchorAbsent = true
		default:
			route, routed, routeErr := recordRoute(root)
			if routeErr != nil {
				return originResolution{}, routeErr
			}
			if routed {
				return originResolution{route: route}, nil
			}
			current = root
		}
	}

	// Steps 2 and 3 — parent walk from the start record.
	visited := make(map[string]struct{}, maxOriginHops)
	for hop := 0; hop <= maxOriginHops; hop++ {
		if hop == maxOriginHops {
			return originResolution{
				reason: reasonOriginUnresolvable,
				detail: fmt.Sprintf("ancestry exceeded %d hops at %s", maxOriginHops, current.ID),
			}, nil
		}
		if _, seen := visited[current.ID]; seen {
			return originResolution{
				reason: reasonOriginUnresolvable,
				detail: fmt.Sprintf("ancestry cycles at %s", current.ID),
			}, nil
		}
		visited[current.ID] = struct{}{}

		route, routed, routeErr := recordRoute(current)
		if routeErr != nil {
			return originResolution{}, routeErr
		}
		if routed {
			return originResolution{route: route}, nil
		}

		if parentID := current.ParentLoopID; parentID != "" {
			parent, err := c.loadPersistedLoop(ctx, parentID)
			if err == nil {
				current = parent
				continue
			}
			if !isLoopRecordAbsent(err) {
				return originResolution{}, err
			}
			exhausted.parentID = parentID
			exhausted.parentAbsent = true
		}

		// The parent link is empty or its key is absent: try this record's
		// own run anchor before anything settles.
		anchor := current.RunID
		if anchor == "" || anchor == current.ID {
			return exhausted.settle(), nil
		}
		if _, alreadyTried := tried[anchor]; alreadyTried {
			return exhausted.settle(), nil
		}
		tried[anchor] = struct{}{}
		root, err := c.loadPersistedLoop(ctx, anchor)
		if err != nil {
			if !isLoopRecordAbsent(err) {
				return originResolution{}, err
			}
			exhausted.runAnchor = anchor
			exhausted.runAnchorAbsent = true
			return exhausted.settle(), nil
		}
		rootRoute, rootRouted, rootErr := recordRoute(root)
		if rootErr != nil {
			return originResolution{}, rootErr
		}
		if rootRouted {
			return originResolution{route: rootRoute}, nil
		}
		current = root
	}
	// Unreachable: the loop returns at hop == maxOriginHops.
	return originResolution{reason: reasonOriginUnresolvable, detail: "ancestry walk did not terminate"}, nil
}

// originExhaustion accumulates what the walk could not observe, so the
// settling reason distinguishes "there was no origin" from "the origin could
// not be observed", and the log line names both exhaustions (C2).
type originExhaustion struct {
	parentID        string
	parentAbsent    bool
	runAnchor       string
	runAnchorAbsent bool
}

func (e originExhaustion) settle() originResolution {
	if !e.parentAbsent && !e.runAnchorAbsent {
		// Every link the walk followed resolved; it simply ran out of links.
		return originResolution{reason: reasonRouteLessSettled}
	}
	parentClause := "parent chain ended with no further link"
	if e.parentAbsent {
		parentClause = "parent chain ended at absent " + e.parentID
	}
	anchorClause := "run anchor none"
	if e.runAnchorAbsent {
		anchorClause = "run anchor " + e.runAnchor + " absent"
	}
	return originResolution{
		reason: reasonOriginUnresolvable,
		detail: parentClause + "; " + anchorClause,
	}
}

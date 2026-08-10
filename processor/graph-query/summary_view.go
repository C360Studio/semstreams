package graphquery

import (
	"context"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/c360studio/semstreams/graph/clustering"
	"github.com/c360studio/semstreams/pkg/graphview"
)

func decodeCommunitySummaryRecord(
	key string,
	value []byte,
	_ graphview.EntryMeta,
) (clustering.CommunitySummaryRecord, bool, error) {
	var zero clustering.CommunitySummaryRecord
	level, membershipHash, err := parseSummaryKey(key)
	if err != nil {
		return zero, false, err
	}

	var record clustering.CommunitySummaryRecord
	if err := json.Unmarshal(value, &record); err != nil {
		return zero, false, fmt.Errorf("decode community summary %q: %w", key, err)
	}
	if !canonicalMembershipHash(record.MembershipHash) {
		return zero, false, fmt.Errorf("community summary %q has noncanonical membership hash", key)
	}
	if record.Level != level || record.MembershipHash != membershipHash ||
		clustering.SummaryKey(record.Level, record.MembershipHash) != key {
		return zero, false, fmt.Errorf("community summary %q payload identity does not match key", key)
	}

	switch record.Status {
	case clustering.SummaryStatusEnhanced:
		if record.LLMSummary == "" {
			return zero, false, fmt.Errorf("community summary %q is enhanced with empty content", key)
		}
		return record, true, nil
	case clustering.SummaryStatusFailed:
		return zero, false, nil
	default:
		return zero, false, fmt.Errorf("community summary %q has unknown status %q", key, record.Status)
	}
}

func parseSummaryKey(key string) (int, string, error) {
	if strings.Count(key, ".") != 1 {
		return 0, "", fmt.Errorf("community summary key %q must contain exactly one dot", key)
	}
	levelText, membershipHash, _ := strings.Cut(key, ".")
	level, err := strconv.Atoi(levelText)
	if err != nil || level < 0 || strconv.Itoa(level) != levelText {
		return 0, "", fmt.Errorf("community summary key %q has noncanonical level", key)
	}
	if !canonicalMembershipHash(membershipHash) {
		return 0, "", fmt.Errorf("community summary key %q has noncanonical membership hash", key)
	}
	return level, membershipHash, nil
}

func canonicalMembershipHash(hash string) bool {
	if len(hash) != 64 {
		return false
	}
	for _, char := range hash {
		if (char < '0' || char > '9') && (char < 'a' || char > 'f') {
			return false
		}
	}
	return true
}

func signalSummaryViewLoss(loss chan<- struct{}) {
	select {
	case loss <- struct{}{}:
	default:
	}
}

type summaryPoisonNotice struct {
	key string
	err error
}

func signalSummaryViewPoison(poison chan<- summaryPoisonNotice, notice summaryPoisonNotice) {
	select {
	case poison <- notice:
	default:
	}
}

func waitSummaryViewRetry(ctx context.Context, interval time.Duration) bool {
	timer := time.NewTimer(interval)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return false
	case <-timer.C:
		return true
	}
}

func (c *Component) superviseSummaryView(ctx context.Context) {
	if c.openSummaryReader == nil {
		c.logger.Error("community summary view supervisor has no catalog reader opener")
		return
	}
	waitRetry := c.waitSummaryRetry
	if waitRetry == nil {
		waitRetry = waitSummaryViewRetry
	}

	for ctx.Err() == nil {
		reader, err := c.openSummaryReader(ctx)
		if err != nil {
			if ctx.Err() != nil {
				return
			}
			c.logger.Warn("COMMUNITY_SUMMARIES unavailable; using statistical summaries",
				"error", err, "retry_interval", c.config.RecheckInterval)
			if !waitRetry(ctx, c.config.RecheckInterval) {
				return
			}
			continue
		}
		if ctx.Err() != nil {
			return
		}

		loss := make(chan struct{}, 1)
		poison := make(chan summaryPoisonNotice, 1)
		view, err := graphview.New[clustering.CommunitySummaryRecord](
			reader,
			decodeCommunitySummaryRecord,
			graphview.WithHooks(graphview.Hooks{
				OnApply: func(key string, revision uint64) {
					c.notifySummaryViewApplied(key, revision)
				},
				OnWatcherLost: func(error) { signalSummaryViewLoss(loss) },
				OnPoison: func(key string, poisonErr error) {
					signalSummaryViewPoison(poison, summaryPoisonNotice{key: key, err: poisonErr})
				},
			}),
		)
		if err != nil {
			c.logger.Warn("construct COMMUNITY_SUMMARIES view failed; using statistical summaries",
				"error", err, "retry_interval", c.config.RecheckInterval)
			if !waitRetry(ctx, c.config.RecheckInterval) {
				return
			}
			continue
		}
		c.notifySummaryViewConstructed(view)
		if err := view.Start(ctx); err != nil {
			view.Stop()
			c.notifySummaryViewStopped(view)
			if ctx.Err() != nil {
				return
			}
			c.logger.Warn("start COMMUNITY_SUMMARIES view failed; using statistical summaries",
				"error", err, "retry_interval", c.config.RecheckInterval)
			if !waitRetry(ctx, c.config.RecheckInterval) {
				return
			}
			continue
		}
		if ctx.Err() != nil {
			view.Stop()
			c.notifySummaryViewStopped(view)
			return
		}

		c.publishSummaryView(view)
		c.logger.Info("COMMUNITY_SUMMARIES view attached")

		lost := c.awaitSummaryViewExit(ctx, loss, poison)

		c.clearSummaryView(view)
		view.Stop()
		c.notifySummaryViewStopped(view)
		if ctx.Err() != nil {
			return
		}
		if lost {
			c.logger.Warn("COMMUNITY_SUMMARIES view lost; using statistical summaries until replacement")
		}
		if !waitRetry(ctx, c.config.RecheckInterval) {
			return
		}
	}
}

func (c *Component) awaitSummaryViewExit(
	ctx context.Context,
	loss <-chan struct{},
	poison <-chan summaryPoisonNotice,
) bool {
	poisonInput := poison
	var warningTimer *time.Timer
	var warningReady <-chan time.Time
	defer func() {
		if warningTimer != nil {
			warningTimer.Stop()
		}
	}()

	for {
		// Give terminal control signals priority over a poison warning that was
		// already coalesced before this iteration.
		select {
		case <-ctx.Done():
			return false
		case <-loss:
			return true
		default:
		}

		select {
		case <-ctx.Done():
			return false
		case <-loss:
			return true
		case notice := <-poisonInput:
			warningTimer = time.NewTimer(c.config.RecheckInterval)
			warningReady = warningTimer.C
			poisonInput = nil
			c.logger.Warn("COMMUNITY_SUMMARIES record poisoned", "key", notice.key, "error", notice.err)
		case <-warningReady:
			warningTimer = nil
			warningReady = nil
			poisonInput = poison
		}
	}
}

func (c *Component) publishSummaryView(view *graphview.View[clustering.CommunitySummaryRecord]) {
	c.summaryViewMu.Lock()
	c.summaryView = view
	c.summaryViewMu.Unlock()
	c.notifySummaryViewChanged(view)
}

func (c *Component) clearSummaryView(view *graphview.View[clustering.CommunitySummaryRecord]) {
	c.summaryViewMu.Lock()
	if c.summaryView != view {
		c.summaryViewMu.Unlock()
		return
	}
	c.summaryView = nil
	c.summaryViewMu.Unlock()
	c.notifySummaryViewChanged(nil)
}

func (c *Component) notifySummaryViewConstructed(view *graphview.View[clustering.CommunitySummaryRecord]) {
	if c.summaryViewConstructed != nil {
		c.summaryViewConstructed(view)
	}
}

func (c *Component) notifySummaryViewChanged(view *graphview.View[clustering.CommunitySummaryRecord]) {
	if c.summaryViewChanged != nil {
		c.summaryViewChanged(view)
	}
}

func (c *Component) notifySummaryViewApplied(key string, revision uint64) {
	if c.summaryViewApplied != nil {
		c.summaryViewApplied(key, revision)
	}
}

func (c *Component) notifySummaryViewStopped(view *graphview.View[clustering.CommunitySummaryRecord]) {
	if c.summaryViewStopped != nil {
		c.summaryViewStopped(view)
	}
}

func (c *Component) summaryFor(comm *clustering.Community) (string, bool) {
	if comm == nil || len(comm.Members) == 0 {
		return "", false
	}

	c.summaryViewMu.RLock()
	view := c.summaryView
	c.summaryViewMu.RUnlock()
	if view == nil {
		return "", false
	}

	key := clustering.SummaryKey(comm.Level, clustering.MembershipHash(comm.Members))
	record, _, err := view.Get(key)
	if err != nil {
		return "", false
	}
	return record.LLMSummary, true
}

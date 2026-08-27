// Package rule - Tests for per-rule feedback loop prevention (Gap 1)
package rule

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

// TestPerRuleRevisionTracking verifies that tracking a revision against rule A
// causes only rule A to skip when the watcher later delivers that revision.
// Sibling rules watching the same entity still evaluate — this is the core
// behaviour that unblocks cross-rule KV triggering on shared buckets.
func TestPerRuleRevisionTracking(t *testing.T) {
	p := &Processor{
		ownRevisions: make(map[ruleRevKey]map[uint64]time.Time),
	}

	entityID := "org.platform.system.domain.type.001"

	// Rule A writes a triple and reports revision 42.
	p.trackRuleRevision("rule-a", entityID, 42)

	// Watcher delivers revision 42 — rule A should skip (self-generated).
	assert.True(t, p.shouldSkipRule("rule-a", entityID, 42),
		"rule A must skip the revision it generated")

	// Sibling rule B watching the same entity must still evaluate.
	// NB: we have to re-track because the first skip consumed the revision.
	p.trackRuleRevision("rule-a", entityID, 42)
	assert.False(t, p.shouldSkipRule("rule-b", entityID, 42),
		"rule B must not be skipped by rule A's tracked revision")

	// After rule B's check, rule A's tracked revision is still present and
	// will be consumed on rule A's next check.
	assert.True(t, p.shouldSkipRule("rule-a", entityID, 42),
		"rule A's tracked revision should still be consumable")
}

// TestRevisionSkipIsOneTime verifies that after a rule skips a tracked
// revision, the same rule will not skip that revision again on subsequent
// watcher deliveries (e.g. for a later unrelated update). Without this,
// stale tracking would indefinitely suppress rule evaluation.
func TestRevisionSkipIsOneTime(t *testing.T) {
	p := &Processor{
		ownRevisions: make(map[ruleRevKey]map[uint64]time.Time),
	}

	p.trackRuleRevision("rule-a", "test.rule.revision.tracking.entity.entity-1", 7)

	assert.True(t, p.shouldSkipRule("rule-a", "test.rule.revision.tracking.entity.entity-1", 7),
		"first check must skip")
	assert.False(t, p.shouldSkipRule("rule-a", "test.rule.revision.tracking.entity.entity-1", 7),
		"second check must not skip (tracking consumed)")
}

// TestMultipleRevisionsPerRule verifies that when a rule writes multiple
// times in quick succession, every resulting revision is tracked so the
// watcher delivery of any of them is suppressed for that rule.
func TestMultipleRevisionsPerRule(t *testing.T) {
	p := &Processor{
		ownRevisions: make(map[ruleRevKey]map[uint64]time.Time),
	}

	p.trackRuleRevision("rule-a", "test.rule.revision.tracking.entity.entity-1", 10)
	p.trackRuleRevision("rule-a", "test.rule.revision.tracking.entity.entity-1", 11)
	p.trackRuleRevision("rule-a", "test.rule.revision.tracking.entity.entity-1", 12)

	// Watcher delivers them in any order; all three must be suppressed.
	assert.True(t, p.shouldSkipRule("rule-a", "test.rule.revision.tracking.entity.entity-1", 11))
	assert.True(t, p.shouldSkipRule("rule-a", "test.rule.revision.tracking.entity.entity-1", 10))
	assert.True(t, p.shouldSkipRule("rule-a", "test.rule.revision.tracking.entity.entity-1", 12))
	// Fourth arrival is a non-self revision.
	assert.False(t, p.shouldSkipRule("rule-a", "test.rule.revision.tracking.entity.entity-1", 13))
}

// TestTrackRuleRevisionIgnoresZeroArgs verifies that tracking is a no-op for
// zero-value inputs. Zero revisions are produced by fallback/no-mutator code
// paths and must not pollute the tracking map.
func TestTrackRuleRevisionIgnoresZeroArgs(t *testing.T) {
	p := &Processor{
		ownRevisions: make(map[ruleRevKey]map[uint64]time.Time),
	}

	p.trackRuleRevision("", "test.rule.revision.tracking.entity.entity-1", 1)
	p.trackRuleRevision("rule-a", "", 1)
	p.trackRuleRevision("rule-a", "test.rule.revision.tracking.entity.entity-1", 0)

	assert.Empty(t, p.ownRevisions, "no-op inputs must not create entries")
}

// TestShouldSkipRuleHandlesMissingEntries verifies the lookup returns false
// and does not panic for unknown rule/entity/revision combinations.
func TestShouldSkipRuleHandlesMissingEntries(t *testing.T) {
	p := &Processor{
		ownRevisions: make(map[ruleRevKey]map[uint64]time.Time),
	}

	assert.False(t, p.shouldSkipRule("rule-a", "test.rule.revision.tracking.entity.entity-1", 5))
	assert.False(t, p.shouldSkipRule("", "test.rule.revision.tracking.entity.entity-1", 5))
	assert.False(t, p.shouldSkipRule("rule-a", "", 5))
	assert.False(t, p.shouldSkipRule("rule-a", "test.rule.revision.tracking.entity.entity-1", 0))
}

// TestPruneStaleRevisions verifies the sweeper removes entries older than
// the given maxAge, keeps fresh ones, and cleans up (ruleID,entityID) entries
// whose revision sets are fully drained.
func TestPruneStaleRevisions(t *testing.T) {
	p := &Processor{
		ownRevisions: make(map[ruleRevKey]map[uint64]time.Time),
	}

	now := time.Now()
	// Inject directly so we can control timestamps (trackRuleRevision uses
	// time.Now internally, which doesn't let us simulate age).
	stale := ruleRevKey{ruleID: "rule-a", entityID: "test.rule.revision.tracking.entity.entity-1"}
	fresh := ruleRevKey{ruleID: "rule-a", entityID: "test.rule.revision.tracking.entity.entity-2"}
	mixed := ruleRevKey{ruleID: "rule-b", entityID: "test.rule.revision.tracking.entity.entity-1"}

	p.ownRevisions[stale] = map[uint64]time.Time{
		10: now.Add(-10 * time.Minute), // stale
		11: now.Add(-8 * time.Minute),  // stale
	}
	p.ownRevisions[fresh] = map[uint64]time.Time{
		20: now.Add(-30 * time.Second), // fresh
	}
	p.ownRevisions[mixed] = map[uint64]time.Time{
		30: now.Add(-20 * time.Minute), // stale
		31: now.Add(-15 * time.Second), // fresh
	}

	pruned := p.pruneStaleRevisions(5 * time.Minute)
	assert.Equal(t, 3, pruned, "two stale under 'stale' + one under 'mixed' = 3")

	// Fully-drained entries are removed from the outer map.
	_, hasStale := p.ownRevisions[stale]
	assert.False(t, hasStale, "outer entry with no remaining revisions must be deleted")

	// Fresh-only entry is untouched.
	freshSet, ok := p.ownRevisions[fresh]
	assert.True(t, ok)
	assert.Contains(t, freshSet, uint64(20))

	// Mixed entry keeps only the fresh revision.
	mixedSet, ok := p.ownRevisions[mixed]
	assert.True(t, ok)
	assert.NotContains(t, mixedSet, uint64(30))
	assert.Contains(t, mixedSet, uint64(31))

	// Total count reflects only the two surviving revisions.
	assert.Equal(t, 2, p.trackedRevisionCount())
}

// TestPruneStaleRevisions_NoopWhenEmpty verifies prune returns 0 on an empty
// tracker and does not touch the map.
func TestPruneStaleRevisions_NoopWhenEmpty(t *testing.T) {
	p := &Processor{
		ownRevisions: make(map[ruleRevKey]map[uint64]time.Time),
	}
	assert.Equal(t, 0, p.pruneStaleRevisions(time.Minute))
	assert.Empty(t, p.ownRevisions)
}

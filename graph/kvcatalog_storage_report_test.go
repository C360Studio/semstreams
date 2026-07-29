package graph

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/c360studio/semstreams/natsclient"
)

// TestKVCatalog_StorageReportIsDeclaredNotImprovised pins the STORAGE_REPORT
// row's declared policy against the two ways it could silently stop working.
//
// The retention assertions are the load-bearing pair. The published report
// reclaims a disappeared resource by DELETING its key — a semantic decision the
// collector makes, on the same principle that governs the rest of the graph. A
// TTL or a byte ceiling on this bucket would expire rows the collector never
// decided to drop, which would make an operator's "this resource is gone" and
// "this row aged out" indistinguishable, and would put a retention policy on
// exactly the bucket whose whole argument is that retention is not the
// reclamation mechanism.
func TestKVCatalog_StorageReportIsDeclaredNotImprovised(t *testing.T) {
	spec, ok := SpecFor(BucketStorageReport)
	require.True(t, ok, "the storage report bucket must be declared in the catalog, not created ad hoc")
	require.NoError(t, spec.Validate())

	assert.Equal(t, natsclient.ClassOperational, spec.Class,
		"the report is framework operational state, not graph data")
	assert.Equal(t, natsclient.WriteOwnerOnly, spec.Write)
	assert.True(t, IsFrameworkOwnedBucket(BucketStorageReport),
		"a generic rule update_kv must not be able to forge a storage report row")
	assert.Equal(t, natsclient.PostureOwnerCreates, spec.Posture)

	assert.Equal(t, natsclient.RetentionNoLifecycle, spec.Retention.Kind,
		"a disappeared resource is reclaimed by DELETE, never by expiry")
	assert.Zero(t, spec.Retention.TTL)
}

// TestKVCatalog_StorageReportHistoryCarriesTheGrowthSeries is the cross-pin
// that makes the History depth a contract instead of a number.
//
// The per-key history IS the growth series: the rate is Δbytes over Δt across
// successive PUBLISHED observations, so an editor who reconciles this row's
// depth down is not shrinking a log — they are deleting the baselines a
// restarting process reads. The floor is the two observations the derivation
// consumes; the declared depth is well above it so a crash loop or several
// account-wide publishers cannot fill the whole retained window with rows too
// close together to measure against (see the row's comment, and
// natsclient.TestStorageReportPublisher_HistoryDepthSurvivesACrashLoop for the
// behavior a shallow row produces).
func TestKVCatalog_StorageReportHistoryCarriesTheGrowthSeries(t *testing.T) {
	spec, ok := SpecFor(BucketStorageReport)
	require.True(t, ok)

	assert.GreaterOrEqual(t, spec.History, natsclient.StorageReportGrowthObservations,
		"History must retain at least the observations the growth derivation consumes")
	assert.GreaterOrEqual(t, spec.History, uint8(4),
		"a depth at the bare floor leaves no room to walk past too-close rows in a restart loop")
}

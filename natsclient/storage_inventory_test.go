package natsclient

import (
	"context"
	"encoding/json"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/nats-io/nats.go/jetstream"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// --- fakes -------------------------------------------------------------------

// fakeStreamInfoLister stands in for jetstream's paged info listing. It
// reproduces the three properties of the real lister the collector must handle:
// the channel is created ONCE and the same one is returned on every Info() call
// (nats.go hands back a pre-made field), the channel closes when the walk ends,
// and Err() is only meaningful after that close.
//
// The sync.Once matters. A collector that called Info() twice would drain an
// already-closed channel in production and silently report an empty account; a
// fake minting a fresh channel per call would hide that entirely.
type fakeStreamInfoLister struct {
	infos []*jetstream.StreamInfo
	// failWith is published through Err() after the channel closes.
	failWith error
	// hold, when non-nil, blocks the walk until it is closed. Used to prove the
	// collection timeout binds and that Latest() never waits behind a
	// collection.
	hold <-chan struct{}

	once sync.Once
	ch   chan *jetstream.StreamInfo
	err  error
}

func (f *fakeStreamInfoLister) Info() <-chan *jetstream.StreamInfo {
	f.once.Do(func() {
		f.ch = make(chan *jetstream.StreamInfo)
		go func() {
			defer close(f.ch)
			if f.hold != nil {
				<-f.hold
			}
			for _, info := range f.infos {
				f.ch <- info
			}
			// Written before close: the close is the happens-before edge, as in
			// nats.go's streamLister.
			f.err = f.failWith
		}()
	})
	return f.ch
}

func (f *fakeStreamInfoLister) Err() error { return f.err }

// fakeStreamNameLister is the same contract for the paged NAME listing — the
// one the server does not filter offline streams out of.
type fakeStreamNameLister struct {
	names    []string
	failWith error
	hold     <-chan struct{}

	once sync.Once
	ch   chan string
	err  error
}

func (f *fakeStreamNameLister) Name() <-chan string {
	f.once.Do(func() {
		f.ch = make(chan string)
		go func() {
			defer close(f.ch)
			if f.hold != nil {
				<-f.hold
			}
			for _, name := range f.names {
				f.ch <- name
			}
			f.err = f.failWith
		}()
	})
	return f.ch
}

func (f *fakeStreamNameLister) Err() error { return f.err }

// fakeLister is a StreamLister serving both listings, each rebuilt per call from
// whatever the test's script says the account holds right now.
type fakeLister struct {
	nextInfos func() *fakeStreamInfoLister
	nextNames func() *fakeStreamNameLister
	calls     atomic.Int64
}

func (f *fakeLister) ListStreams(_ context.Context, _ ...jetstream.StreamListOpt) jetstream.StreamInfoLister {
	f.calls.Add(1)
	return f.nextInfos()
}

func (f *fakeLister) StreamNames(_ context.Context, _ ...jetstream.StreamListOpt) jetstream.StreamNameLister {
	return f.nextNames()
}

func (f *fakeLister) source() StreamListerSource {
	return func() (StreamLister, error) { return f, nil }
}

func infoNames(infos []*jetstream.StreamInfo) []string {
	names := make([]string, 0, len(infos))
	for _, info := range infos {
		if info != nil && info.Config.Name != "" {
			names = append(names, info.Config.Name)
		}
	}
	return names
}

// consistentAccount builds a lister whose two listings AGREE — every described
// resource is also named and vice versa. The healthy-server case.
func consistentAccount(infos ...*jetstream.StreamInfo) *fakeLister {
	names := infoNames(infos)
	return &fakeLister{
		nextInfos: func() *fakeStreamInfoLister { return &fakeStreamInfoLister{infos: infos} },
		nextNames: func() *fakeStreamNameLister { return &fakeStreamNameLister{names: names} },
	}
}

func listerOf(infos ...*jetstream.StreamInfo) StreamListerSource {
	return consistentAccount(infos...).source()
}

// accountWithUndescribable builds the offline case: the name listing reports
// every resource, the info listing omits the ones the server declines to
// describe.
func accountWithUndescribable(undescribable []string, infos ...*jetstream.StreamInfo) *fakeLister {
	names := append(infoNames(infos), undescribable...)
	return &fakeLister{
		nextInfos: func() *fakeStreamInfoLister { return &fakeStreamInfoLister{infos: infos} },
		nextNames: func() *fakeStreamNameLister { return &fakeStreamNameLister{names: names} },
	}
}

// streamInfo builds a listing entry the way the server returns it: config and
// state together, so the collector never needs a follow-up describe.
func streamInfo(name string, storage jetstream.StorageType, maxBytes int64, usedBytes uint64) *jetstream.StreamInfo {
	return &jetstream.StreamInfo{
		Config: jetstream.StreamConfig{Name: name, Storage: storage, MaxBytes: maxBytes},
		State:  jetstream.StreamState{Bytes: usedBytes, Msgs: usedBytes},
	}
}

// resolverFrom builds an OwnerResolver over a mutable fixture table, so a test
// can REMOVE a bucket between collections and prove attribution re-reads the
// table rather than a retained copy of it.
func resolverFrom(table map[string]string) OwnerResolver {
	return func(bucket string) string { return table[bucket] }
}

func newTestCollector(t *testing.T, source StreamListerSource, resolver OwnerResolver) *StorageInventoryCollector {
	t.Helper()
	c, err := NewStorageInventoryCollector(source, StorageInventoryConfig{
		OwnerResolver: resolver,
		Timeout:       2 * time.Second,
		ProducedBy:    "unit-test",
	})
	require.NoError(t, err)
	return c
}

// byName indexes an inventory and asserts it carries no duplicate names, so a
// collapsing index can never hide a duplication bug.
func byName(t *testing.T, inv StorageInventory) map[string]StorageResource {
	t.Helper()
	out := make(map[string]StorageResource, len(inv.Resources))
	for _, r := range inv.Resources {
		require.NotContains(t, out, r.Name, "the inventory must not carry duplicate resource names")
		out[r.Name] = r
	}
	return out
}

// --- 2.2 / E attribution -----------------------------------------------------

// TestStorageInventory_Attribution covers the KV owner-attribution contract in
// one table: a catalog bucket reports its catalog owner, a KV bucket the catalog
// does not declare reports UNATTRIBUTED, a kind with no owner concept reports
// NOT-APPLICABLE, and exactly one leading KV_ is stripped.
//
// The fixture table deliberately contains an owner for "FOO". A collector that
// over-strips KV_KV_FOO down to FOO would resolve that entry and report
// "over-stripped-owner" — so the doubled-prefix case fails loudly rather than
// silently passing on an empty owner.
func TestStorageInventory_Attribution(t *testing.T) {
	catalog := map[string]string{
		"ENTITY_STATES": "graph-ingest",
		"FOO":           "over-stripped-owner",
	}

	c := newTestCollector(t, listerOf(
		streamInfo(KVStreamPrefix+"ENTITY_STATES", jetstream.FileStorage, 0, 10),
		streamInfo(KVStreamPrefix+"SEMSOURCE_DOCUMENTS", jetstream.FileStorage, 0, 20),
		streamInfo(KVStreamPrefix+KVStreamPrefix+"FOO", jetstream.FileStorage, 0, 30),
		streamInfo(ObjectStoreStreamPrefix+"MESSAGES", jetstream.FileStorage, 0, 40),
		streamInfo("LOGS", jetstream.FileStorage, 0, 50),
	), resolverFrom(catalog))

	inv, err := c.Collect(context.Background())
	require.NoError(t, err)
	got := byName(t, inv)
	require.Len(t, got, 5, "every account resource appears, attributed or not")

	tests := []struct {
		stream          string
		wantKind        ResourceKind
		wantBucket      string
		wantAttribution AttributionState
		wantOwner       string
	}{
		{KVStreamPrefix + "ENTITY_STATES", ResourceKeyValue, "ENTITY_STATES",
			AttributionAttributed, "graph-ingest"},
		{KVStreamPrefix + "SEMSOURCE_DOCUMENTS", ResourceKeyValue, "SEMSOURCE_DOCUMENTS",
			AttributionUnattributed, ""},
		{KVStreamPrefix + KVStreamPrefix + "FOO", ResourceKeyValue, KVStreamPrefix + "FOO",
			AttributionUnattributed, ""},
		{ObjectStoreStreamPrefix + "MESSAGES", ResourceObjectStore, "MESSAGES",
			AttributionNotApplicable, ""},
		{"LOGS", ResourceOrdinaryStream, "",
			AttributionNotApplicable, ""},
	}
	for _, tt := range tests {
		t.Run(tt.stream, func(t *testing.T) {
			res, ok := got[tt.stream]
			require.True(t, ok, "resource must never be omitted from the inventory")
			assert.Equal(t, tt.wantKind, res.Kind)
			assert.Equal(t, tt.wantBucket, res.Bucket, "exactly one leading prefix is stripped")
			assert.Equal(t, tt.wantAttribution, res.Attribution)
			assert.Equal(t, tt.wantOwner, res.Owner)
			assert.Equal(t, tt.wantAttribution == AttributionAttributed, res.Attributed())
		})
	}
}

// TestStorageInventory_AttributionStatesNeverCollapse is the attribution-side
// companion to the capacity anti-collapse proof. "This bucket escaped the
// catalog" and "this kind of resource has no owner concept" are different facts
// and only the first is a finding, so an operator filtering for escaped buckets
// must not have to wade through every ordinary stream in the account.
func TestStorageInventory_AttributionStatesNeverCollapse(t *testing.T) {
	c := newTestCollector(t, listerOf(
		streamInfo(KVStreamPrefix+"ENTITY_STATES", jetstream.FileStorage, 0, 1),
		streamInfo(KVStreamPrefix+"ESCAPED_BUCKET", jetstream.FileStorage, 0, 1),
		streamInfo(ObjectStoreStreamPrefix+"CONTENT", jetstream.FileStorage, 0, 1),
		streamInfo("LOGS", jetstream.FileStorage, 0, 1),
	), resolverFrom(map[string]string{"ENTITY_STATES": "graph-ingest"}))

	inv, err := c.Collect(context.Background())
	require.NoError(t, err)
	got := byName(t, inv)

	attributed := got[KVStreamPrefix+"ENTITY_STATES"]
	escaped := got[KVStreamPrefix+"ESCAPED_BUCKET"]
	objectStore := got[ObjectStoreStreamPrefix+"CONTENT"]
	ordinary := got["LOGS"]

	assert.Equal(t, AttributionAttributed, attributed.Attribution)
	assert.Equal(t, AttributionUnattributed, escaped.Attribution)
	assert.Equal(t, AttributionNotApplicable, objectStore.Attribution)
	assert.Equal(t, AttributionNotApplicable, ordinary.Attribution)

	assert.NotEqual(t, escaped.Attribution, ordinary.Attribution,
		"an escaped KV bucket is a finding; an ordinary stream having no owner is not")
	assert.NotEqual(t, escaped.Attribution, attributed.Attribution)
	assert.NotEqual(t, attributed.Attribution, ordinary.Attribution)

	t.Run("only an attributed resource carries an owner", func(t *testing.T) {
		for _, res := range inv.Resources {
			if res.Attribution == AttributionAttributed {
				assert.NotEmpty(t, res.Owner, "an attributed resource must name its owner")
				continue
			}
			assert.Empty(t, res.Owner, "a non-attributed resource must not carry an owner string")
			assert.False(t, res.Attributed())
		}
	})

	t.Run("the JSON encoding keeps the states distinct", func(t *testing.T) {
		encode := func(r StorageResource) string {
			raw, err := json.Marshal(r)
			require.NoError(t, err)
			return string(raw)
		}
		assert.Contains(t, encode(escaped), `"attribution":"unattributed"`)
		assert.Contains(t, encode(ordinary), `"attribution":"not-applicable"`)
		assert.Contains(t, encode(attributed), `"attribution":"attributed"`)
	})
}

// TestStorageInventory_AttributionFollowsCatalogNotACopy is the spec scenario
// that exists to catch a retained bucket-to-owner map: remove a bucket from the
// catalog and the NEXT collection must report it unattributed.
func TestStorageInventory_AttributionFollowsCatalogNotACopy(t *testing.T) {
	catalog := map[string]string{"ENTITY_STATES": "graph-ingest"}
	c := newTestCollector(t, listerOf(
		streamInfo(KVStreamPrefix+"ENTITY_STATES", jetstream.FileStorage, 0, 10),
	), resolverFrom(catalog))

	first, err := c.Collect(context.Background())
	require.NoError(t, err)
	require.Equal(t, "graph-ingest", byName(t, first)[KVStreamPrefix+"ENTITY_STATES"].Owner)

	delete(catalog, "ENTITY_STATES")

	second, err := c.Collect(context.Background())
	require.NoError(t, err)
	res := byName(t, second)[KVStreamPrefix+"ENTITY_STATES"]
	assert.Equal(t, AttributionUnattributed, res.Attribution,
		"attribution must re-read the catalog, not a copy of it")
	assert.Equal(t, "", res.Owner)
	assert.False(t, res.Attributed())
	assert.Equal(t, res, byName(t, c.Latest())[KVStreamPrefix+"ENTITY_STATES"],
		"the published inventory must carry the same unattributed result")
}

// TestNewStorageInventoryCollector_RequiresOwnerResolver fails closed on the
// wiring mistake that would otherwise be silent: with no resolver EVERY KV
// resource reports unattributed, which reads as "nothing is framework-owned"
// rather than as "attribution was never wired".
func TestNewStorageInventoryCollector_RequiresOwnerResolver(t *testing.T) {
	_, err := NewStorageInventoryCollector(listerOf(), StorageInventoryConfig{})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "OwnerResolver")

	_, err = NewStorageInventoryCollector(nil, StorageInventoryConfig{OwnerResolver: resolverFrom(nil)})
	require.Error(t, err)
}

// --- 2.3 three distinct capacity states --------------------------------------

// TestNewCapacity_ThreeStatesNeverCollapse is the anti-collapse proof. Each pair
// of states is checked for distinctness, and the two collapse shapes that would
// manufacture confidence are named explicitly: an unreadable resource must not
// become "unlimited", and neither unknown nor unbounded may present a zero limit
// that reads as a real bound.
func TestNewCapacity_ThreeStatesNeverCollapse(t *testing.T) {
	bounded := NewCapacity(1024, 256, true)
	unboundedZero := NewCapacity(0, 256, true) // JetStream's "no limit" as 0
	unboundedNeg := NewCapacity(-1, 256, true) // ...and as the -1 sentinel
	unknown := NewCapacity(1024, 256, false)   // limit/usage could not be read
	unknownAlt := NewCapacity(0, 0, false)     // same state from different input

	t.Run("bounded reports its limit and usage", func(t *testing.T) {
		assert.Equal(t, CapacityBounded, bounded.State)
		limit, ok := bounded.Limit()
		require.True(t, ok)
		assert.Equal(t, int64(1024), limit)
		used, ok := bounded.Usage()
		require.True(t, ok)
		assert.Equal(t, int64(256), used)
	})

	t.Run("unbounded has usage but no limit", func(t *testing.T) {
		for _, c := range []Capacity{unboundedZero, unboundedNeg} {
			assert.Equal(t, CapacityUnbounded, c.State)
			_, ok := c.Limit()
			assert.False(t, ok, "an unbounded resource must never present a limit value")
			used, ok := c.Usage()
			require.True(t, ok, "usage is still observable without a bound")
			assert.Equal(t, int64(256), used)
		}
	})

	t.Run("unknown has neither, from any input", func(t *testing.T) {
		for _, c := range []Capacity{unknown, unknownAlt} {
			assert.Equal(t, CapacityUnknown, c.State)
			_, ok := c.Limit()
			assert.False(t, ok)
			_, ok = c.Usage()
			assert.False(t, ok, "an unreadable usage must not read back as zero")
		}
	})

	t.Run("no two states collapse", func(t *testing.T) {
		assert.NotEqual(t, unknown.State, unboundedZero.State,
			"unreadable is not unlimited — collapsing these manufactures confidence")
		assert.NotEqual(t, unknown.State, bounded.State)
		assert.NotEqual(t, unboundedZero.State, bounded.State)
		assert.NotEqual(t, unknown, unboundedZero)
		assert.NotEqual(t, unknown, bounded)
		assert.NotEqual(t, unboundedZero, bounded)
	})

	t.Run("headroom is projectable only when bounded", func(t *testing.T) {
		assert.True(t, bounded.Bounded())
		assert.False(t, unboundedZero.Bounded(), "no bound means no headroom to project")
		assert.False(t, unknown.Bounded(), "unknown suppresses projection rather than fabricating it")
	})

	t.Run("the JSON encoding keeps them distinct", func(t *testing.T) {
		encode := func(c Capacity) string {
			raw, err := json.Marshal(c)
			require.NoError(t, err)
			return string(raw)
		}
		b, ub, uk := encode(bounded), encode(unboundedZero), encode(unknown)
		assert.NotEqual(t, b, ub)
		assert.NotEqual(t, ub, uk)
		assert.NotEqual(t, b, uk)
		assert.NotContains(t, uk, "used", "an unknown capacity must not serialize a usage number")
		assert.NotContains(t, uk, "configured_limit")
		assert.NotContains(t, ub, "configured_limit", "unbounded must not serialize a zero limit")
		assert.Contains(t, ub, `"used"`)
	})
}

// TestStorageInventory_StorageTierIsReadNotGuessed pins the per-tier fact that
// the account-limit comparison later depends on: memory and file are separate
// tiers and must never be conflated.
func TestStorageInventory_StorageTierIsReadNotGuessed(t *testing.T) {
	c := newTestCollector(t, listerOf(
		streamInfo("HEALTH", jetstream.MemoryStorage, 0, 1),
		streamInfo("LOGS", jetstream.FileStorage, 0, 1),
	), resolverFrom(nil))

	inv, err := c.Collect(context.Background())
	require.NoError(t, err)
	got := byName(t, inv)
	assert.Equal(t, TierMemory, got["HEALTH"].Tier)
	assert.Equal(t, TierFile, got["LOGS"].Tier)
	assert.False(t, got["LOGS"].Undescribable(), "a described resource is not an undescribable one")
}

// --- 2.7 reconciling the info listing against the name listing ---------------

// TestStorageInventory_UndescribableResourceIsNamedNotOmitted is the whole point
// of the second listing. The server moves any stream carrying an offline reason
// out of the info listing (into Missing/Offline, which the Go client drops) but
// NOT out of the name listing. A collector reading only the info listing
// publishes a complete-looking inventory that silently omits exactly the
// resources nobody can read, bound, or watch grow — reachable on a standalone
// single-server deploy after a NATS image rollback, which is precisely when an
// operator needs the storage view most.
func TestStorageInventory_UndescribableResourceIsNamedNotOmitted(t *testing.T) {
	lister := accountWithUndescribable(
		[]string{"OFFLINE_EVENTS", KVStreamPrefix + "ENTITY_STATES"},
		streamInfo("LOGS", jetstream.FileStorage, 4096, 100),
		streamInfo(ObjectStoreStreamPrefix+"CONTENT", jetstream.FileStorage, 0, 5),
	)
	c := newTestCollector(t, lister.source(),
		resolverFrom(map[string]string{"ENTITY_STATES": "graph-ingest"}))

	inv, err := c.Collect(context.Background())
	require.NoError(t, err)
	got := byName(t, inv)
	require.Len(t, got, 4, "the inventory must not report itself complete while omitting the unreadable")

	t.Run("an undescribable ordinary stream keeps its real name", func(t *testing.T) {
		res, ok := got["OFFLINE_EVENTS"]
		require.True(t, ok, "a resource the server declines to describe must still be named")
		assert.Equal(t, "OFFLINE_EVENTS", res.Name, "the name is real, not a placeholder")
		assert.Equal(t, ResourceOrdinaryStream, res.Kind, "kind is derivable from the name alone")
		assert.Equal(t, TierUnknown, res.Tier)
		assert.Equal(t, CapacityUnknown, res.Bytes.State)
		assert.Equal(t, CapacityUnknown, res.Messages.State)
		assert.False(t, res.Bytes.Bounded(), "no projection is fabricated for it")
		assert.True(t, res.Undescribable())
		assert.Equal(t, AttributionNotApplicable, res.Attribution)
	})

	t.Run("an undescribable KV bucket is still attributed from its name", func(t *testing.T) {
		res, ok := got[KVStreamPrefix+"ENTITY_STATES"]
		require.True(t, ok)
		assert.Equal(t, ResourceKeyValue, res.Kind)
		assert.Equal(t, "ENTITY_STATES", res.Bucket)
		assert.Equal(t, AttributionAttributed, res.Attribution,
			"attribution is a catalog read, so it survives the server declining to describe")
		assert.Equal(t, "graph-ingest", res.Owner)
		assert.True(t, res.Undescribable())
		assert.Equal(t, TierUnknown, res.Tier)
	})

	t.Run("described resources are unaffected", func(t *testing.T) {
		logs := got["LOGS"]
		assert.Equal(t, CapacityBounded, logs.Bytes.State)
		assert.Equal(t, TierFile, logs.Tier)
		assert.False(t, logs.Undescribable())
		assert.False(t, got[ObjectStoreStreamPrefix+"CONTENT"].Undescribable())
	})
}

// TestStorageInventory_InfoListingOnlyWouldOmitTheUnreadable states the
// regression directly: if the collector ever stops reading the name listing, the
// account's unreadable resources vanish from a report that still calls itself
// complete. The name listing carries a resource the info listing does not, and
// the inventory must be strictly larger than the info listing alone.
func TestStorageInventory_InfoListingOnlyWouldOmitTheUnreadable(t *testing.T) {
	described := []*jetstream.StreamInfo{
		streamInfo("LOGS", jetstream.FileStorage, 0, 1),
	}
	lister := accountWithUndescribable([]string{"ONLY_IN_THE_NAME_LISTING"}, described...)
	c := newTestCollector(t, lister.source(), resolverFrom(nil))

	inv, err := c.Collect(context.Background())
	require.NoError(t, err)

	assert.Len(t, inv.Resources, len(described)+1,
		"the inventory must exceed the info listing by exactly the undescribable resources")
	got := byName(t, inv)
	require.Contains(t, got, "ONLY_IN_THE_NAME_LISTING",
		"a collector reading only ListStreams would silently omit this resource")
	assert.True(t, got["ONLY_IN_THE_NAME_LISTING"].Undescribable())
}

// TestStorageInventory_DeduplicatesOverlappingPages covers the second-order
// paging bug: nats.go advances its page offset by the number of entries a page
// returned, while the server's cursor also moved past the entries it excluded
// for being offline. An account past one page with an offline stream therefore
// serves OVERLAPPING pages, and the same resource arrives twice.
func TestStorageInventory_DeduplicatesOverlappingPages(t *testing.T) {
	// The same stream twice, as two overlapping pages would deliver it, with
	// different observed usage so a last-wins merge is visible.
	lister := accountWithUndescribable(nil,
		streamInfo("PAGED", jetstream.FileStorage, 4096, 100),
		streamInfo("OTHER", jetstream.FileStorage, 0, 1),
		streamInfo("PAGED", jetstream.FileStorage, 4096, 200),
	)
	c := newTestCollector(t, lister.source(), resolverFrom(nil))

	inv, err := c.Collect(context.Background())
	require.NoError(t, err)

	// byName itself fails on a duplicate; the length check states the intent.
	got := byName(t, inv)
	assert.Len(t, inv.Resources, 2, "an overlapping page must not produce a duplicate row")
	require.Contains(t, got, "PAGED")
	used, ok := got["PAGED"].Bytes.Usage()
	require.True(t, ok)
	assert.Equal(t, int64(200), used, "the later observation of a duplicated resource wins")
}

// TestStorageInventory_TransientPhantomResolvesOnNextCollection covers the trade
// the spec accepts explicitly: the two listings are not a consistent snapshot,
// so a resource deleted between them appears once as unknown. A phantom row that
// resolves is strictly better than a silent omission that never does — but it
// must actually resolve.
func TestStorageInventory_TransientPhantomResolvesOnNextCollection(t *testing.T) {
	survivor := streamInfo("SURVIVOR", jetstream.FileStorage, 0, 1)
	deletedBetweenListings := true
	lister := &fakeLister{
		nextInfos: func() *fakeStreamInfoLister {
			return &fakeStreamInfoLister{infos: []*jetstream.StreamInfo{survivor}}
		},
		nextNames: func() *fakeStreamNameLister {
			names := []string{"SURVIVOR"}
			if deletedBetweenListings {
				// Named by the first listing, gone by the time the info listing
				// ran.
				names = append(names, "DELETED_MID_COLLECTION")
			}
			return &fakeStreamNameLister{names: names}
		},
	}
	c := newTestCollector(t, lister.source(), resolverFrom(nil))

	first, err := c.Collect(context.Background())
	require.NoError(t, err)
	phantom, ok := byName(t, first)["DELETED_MID_COLLECTION"]
	require.True(t, ok, "the skew is reported as unknown rather than dropped")
	assert.True(t, phantom.Undescribable())

	deletedBetweenListings = false

	second, err := c.Collect(context.Background())
	require.NoError(t, err)
	assert.NotContains(t, byName(t, second), "DELETED_MID_COLLECTION",
		"a transient phantom MUST resolve on a later collection")
	assert.Len(t, second.Resources, 1)
}

// TestStorageInventory_NameListingFailureFailsTheCollection keeps the second
// listing load-bearing rather than best-effort: if it cannot be read, the
// inventory cannot claim completeness, so the collection degrades to last-good
// exactly as an info-listing failure does.
func TestStorageInventory_NameListingFailureFailsTheCollection(t *testing.T) {
	namesBroken := errors.New("name listing unavailable")
	failing := false
	lister := &fakeLister{
		nextInfos: func() *fakeStreamInfoLister {
			return &fakeStreamInfoLister{infos: []*jetstream.StreamInfo{
				streamInfo("LOGS", jetstream.FileStorage, 0, 1),
			}}
		},
		nextNames: func() *fakeStreamNameLister {
			if failing {
				return &fakeStreamNameLister{failWith: namesBroken}
			}
			return &fakeStreamNameLister{names: []string{"LOGS"}}
		},
	}
	c := newTestCollector(t, lister.source(), resolverFrom(nil))

	good, err := c.Collect(context.Background())
	require.NoError(t, err)
	require.Len(t, good.Resources, 1)

	failing = true
	_, err = c.Collect(context.Background())
	require.ErrorIs(t, err, namesBroken)
	assert.Len(t, c.Latest().Resources, 1, "last-good survives a name-listing failure")
	assert.True(t, c.Latest().Stale)
}

// --- a nameless listing entry fails the collection ---------------------------

// TestStorageInventory_NamelessEntryFailsTheCollection: a nameless entry means a
// real resource IS absent from anything this collection can publish, and a row
// named "" is unlookupable, unalertable, and indistinguishable from a sibling.
// That is the same partial-listing hazard a walk error is, so it degrades to
// last-good with a reason naming the malformed listing — which is still "not
// silently omitted".
func TestStorageInventory_NamelessEntryFailsTheCollection(t *testing.T) {
	for _, tc := range []struct {
		name string
		bad  *jetstream.StreamInfo
	}{
		{"nil entry", nil},
		{"entry with an empty name", streamInfo("", jetstream.FileStorage, 0, 0)},
	} {
		t.Run(tc.name, func(t *testing.T) {
			healthy := true
			lister := &fakeLister{
				nextInfos: func() *fakeStreamInfoLister {
					infos := []*jetstream.StreamInfo{streamInfo("LOGS", jetstream.FileStorage, 4096, 100)}
					if !healthy {
						// Last, so the fake's walk goroutine finishes rather
						// than blocking on an abandoned send.
						infos = append(infos, tc.bad)
					}
					return &fakeStreamInfoLister{infos: infos}
				},
				nextNames: func() *fakeStreamNameLister {
					return &fakeStreamNameLister{names: []string{"LOGS"}}
				},
			}
			c := newTestCollector(t, lister.source(), resolverFrom(nil))

			good, err := c.Collect(context.Background())
			require.NoError(t, err)
			require.Len(t, good.Resources, 1)

			healthy = false
			_, err = c.Collect(context.Background())
			require.Error(t, err, "a malformed listing entry must fail the collection")
			assert.Contains(t, err.Error(), "no stream name",
				"the reason must name the malformed listing")

			degraded := c.Latest()
			assert.True(t, degraded.Stale)
			assert.Contains(t, degraded.StaleReason, "no stream name")
			assert.Len(t, degraded.Resources, 1, "last-good survives")
			for _, res := range degraded.Resources {
				assert.NotEmpty(t, res.Name, "no unlookupable ghost row is ever published")
			}
		})
	}
}

// --- 2.4 bounded collection --------------------------------------------------

// TestStorageInventory_LatestBeforeFirstCollectionIsNotHealthy keeps the
// pre-collection window honest: an empty inventory reported as fresh would say
// "the account holds nothing", which is the phantom-confidence failure.
func TestStorageInventory_LatestBeforeFirstCollectionIsNotHealthy(t *testing.T) {
	c := newTestCollector(t, listerOf(), resolverFrom(nil))

	inv := c.Latest()
	assert.Empty(t, inv.Resources)
	assert.True(t, inv.Stale, "no collection has succeeded yet")
	assert.NotEmpty(t, inv.StaleReason)
	assert.True(t, inv.CollectedAt.IsZero(), "nothing has been collected, so there is no collection time")
	assert.Equal(t, "unit-test", inv.ProducedBy, "the report names the producing process")
}

// TestStorageInventory_LatestReturnsAnIndependentSlice guards a sharing bug
// -race cannot see today because no caller writes to its returned slice: a
// shallow struct copy shares the published backing array, so any reader holding
// an inventory can reach into and corrupt the collector's own state, and two
// readers can scribble over each other.
//
// The ELEMENT WRITE below is the load-bearing assertion, and it is ordered
// first deliberately. Appending is a weaker probe: the published slice happens
// to be allocated at exactly its length, so an append reallocates and silently
// masks the aliasing. Reintroducing spare capacity would make the append
// dangerous too, but the element write catches the aliasing either way.
func TestStorageInventory_LatestReturnsAnIndependentSlice(t *testing.T) {
	c := newTestCollector(t, listerOf(
		streamInfo("LOGS", jetstream.FileStorage, 0, 1),
		streamInfo("METRICS", jetstream.MemoryStorage, 0, 1),
	), resolverFrom(nil))
	_, err := c.Collect(context.Background())
	require.NoError(t, err)

	first := c.Latest()
	second := c.Latest()
	require.Len(t, first.Resources, 2)
	require.Len(t, second.Resources, 2)

	first.Resources[0].Name = "MUTATED"
	first.Resources[0].Owner = "IMPERSONATED"

	assert.Equal(t, "LOGS", second.Resources[0].Name,
		"one reader must not mutate another reader's row")
	assert.Equal(t, "LOGS", c.Latest().Resources[0].Name,
		"a reader must not be able to corrupt published inventory state")
	assert.Empty(t, c.Latest().Resources[0].Owner)

	first.Resources = append(first.Resources, StorageResource{Name: "INJECTED"})
	assert.Len(t, second.Resources, 2, "a second reader must not see the first reader's append")
	assert.Len(t, c.Latest().Resources, 2)
}

// TestStorageInventory_DegradesToLastGoodWithTimestamp is the spec's
// last-good-with-timestamp requirement: a failed collection must not blank or
// truncate the inventory, and the reported timestamp must stay the one the good
// data was actually collected at.
func TestStorageInventory_DegradesToLastGoodWithTimestamp(t *testing.T) {
	failing := errors.New("account listing unavailable")
	broken := false
	lister := &fakeLister{
		nextInfos: func() *fakeStreamInfoLister {
			if broken {
				return &fakeStreamInfoLister{failWith: failing}
			}
			return &fakeStreamInfoLister{infos: []*jetstream.StreamInfo{
				streamInfo("LOGS", jetstream.FileStorage, 4096, 100),
			}}
		},
		nextNames: func() *fakeStreamNameLister {
			return &fakeStreamNameLister{names: []string{"LOGS"}}
		},
	}
	c := newTestCollector(t, lister.source(), resolverFrom(nil))

	good, err := c.Collect(context.Background())
	require.NoError(t, err)
	require.Len(t, good.Resources, 1)
	require.False(t, good.Stale)
	collectedAt := good.CollectedAt
	require.False(t, collectedAt.IsZero())

	broken = true
	_, err = c.Collect(context.Background())
	require.ErrorIs(t, err, failing)

	degraded := c.Latest()
	assert.Len(t, degraded.Resources, 1, "a failed collection must not blank the inventory")
	assert.Equal(t, collectedAt, degraded.CollectedAt, "the timestamp stays the one the data was collected at")
	assert.True(t, degraded.Stale)
	assert.Contains(t, degraded.StaleReason, failing.Error())
	assert.False(t, degraded.StaleSince.IsZero(), "the failure has its own timestamp")
	assert.Equal(t, "unit-test", degraded.ProducedBy)

	// A recovered collection clears the degradation rather than latching it.
	broken = false
	fresh, err := c.Collect(context.Background())
	require.NoError(t, err)
	assert.False(t, fresh.Stale)
	assert.Empty(t, fresh.StaleReason)
	assert.False(t, fresh.CollectedAt.Before(collectedAt))
}

// TestStorageInventory_ShutdownDoesNotStampTheLastGoodResult keeps a graceful
// shutdown out of the operator's storage findings: a collection aborted because
// the process is stopping is not evidence about storage, so it must not stamp
// "context canceled" onto an otherwise good inventory.
func TestStorageInventory_ShutdownDoesNotStampTheLastGoodResult(t *testing.T) {
	c := newTestCollector(t, listerOf(
		streamInfo("LOGS", jetstream.FileStorage, 0, 1),
	), resolverFrom(nil))

	good, err := c.Collect(context.Background())
	require.NoError(t, err)
	require.False(t, good.Stale)

	shuttingDown, cancel := context.WithCancel(context.Background())
	cancel()
	_, err = c.Collect(shuttingDown)
	require.Error(t, err)

	after := c.Latest()
	assert.False(t, after.Stale, "a shutdown-cancelled collection is not a storage finding")
	assert.Empty(t, after.StaleReason)
	assert.Equal(t, good.CollectedAt, after.CollectedAt)
}

// TestStorageInventory_PartialListingIsNeverPublished guards the silent-omission
// hazard: a listing that errors PART WAY through has already yielded rows, and
// publishing them would report a subset of the account as if it were all of it.
func TestStorageInventory_PartialListingIsNeverPublished(t *testing.T) {
	truncated := errors.New("listing truncated")
	broken := false
	lister := &fakeLister{
		nextInfos: func() *fakeStreamInfoLister {
			if broken {
				return &fakeStreamInfoLister{
					infos:    []*jetstream.StreamInfo{streamInfo("A", jetstream.FileStorage, 0, 1)},
					failWith: truncated,
				}
			}
			return &fakeStreamInfoLister{infos: []*jetstream.StreamInfo{
				streamInfo("A", jetstream.FileStorage, 0, 1),
				streamInfo("B", jetstream.FileStorage, 0, 1),
				streamInfo("C", jetstream.FileStorage, 0, 1),
			}}
		},
		nextNames: func() *fakeStreamNameLister {
			return &fakeStreamNameLister{names: []string{"A", "B", "C"}}
		},
	}
	c := newTestCollector(t, lister.source(), resolverFrom(nil))

	full, err := c.Collect(context.Background())
	require.NoError(t, err)
	require.Len(t, full.Resources, 3)

	broken = true
	_, err = c.Collect(context.Background())
	require.ErrorIs(t, err, truncated)
	assert.Len(t, c.Latest().Resources, 3,
		"a truncated listing must not replace the inventory with its partial rows")
}

// TestStorageInventory_CollectionIsBoundedByTimeout proves the collection cannot
// hang forever on a lister that never finishes its walk. The bound is the
// collector's own configured timeout, not the caller's context, so an unbounded
// caller context cannot make collection unbounded.
func TestStorageInventory_CollectionIsBoundedByTimeout(t *testing.T) {
	hold := make(chan struct{})
	t.Cleanup(func() { close(hold) })

	lister := &fakeLister{
		nextInfos: func() *fakeStreamInfoLister { return &fakeStreamInfoLister{hold: hold} },
		nextNames: func() *fakeStreamNameLister { return &fakeStreamNameLister{hold: hold} },
	}
	c, err := NewStorageInventoryCollector(lister.source(),
		StorageInventoryConfig{OwnerResolver: resolverFrom(nil), Timeout: 100 * time.Millisecond})
	require.NoError(t, err)

	done := make(chan error, 1)
	go func() {
		_, collectErr := c.Collect(context.Background())
		done <- collectErr
	}()

	select {
	case collectErr := <-done:
		require.Error(t, collectErr)
		assert.ErrorIs(t, collectErr, context.DeadlineExceeded)
	// 5s is ~50x the configured 100ms timeout: generous enough that a loaded CI
	// host cannot flake it, tight enough that an unbounded collection fails.
	case <-time.After(5 * time.Second):
		t.Fatal("collection was not bounded by its configured timeout")
	}
}

// TestStorageInventory_LatestNeverWaitsOnCollection is the "never blocks start or
// health" scenario. A monitoring surface that can stall the system it monitors is
// a worse bug than the blindness it fixes, so the read path must never sit behind
// the collection's I/O.
func TestStorageInventory_LatestNeverWaitsOnCollection(t *testing.T) {
	hold := make(chan struct{})
	blocked := false
	lister := &fakeLister{
		nextInfos: func() *fakeStreamInfoLister {
			if blocked {
				return &fakeStreamInfoLister{hold: hold}
			}
			return &fakeStreamInfoLister{infos: []*jetstream.StreamInfo{
				streamInfo("LOGS", jetstream.FileStorage, 0, 7),
			}}
		},
		nextNames: func() *fakeStreamNameLister {
			return &fakeStreamNameLister{names: []string{"LOGS"}}
		},
	}
	c, err := NewStorageInventoryCollector(lister.source(), StorageInventoryConfig{
		OwnerResolver: resolverFrom(nil), Timeout: 10 * time.Second, ProducedBy: "unit-test",
	})
	require.NoError(t, err)

	good, err := c.Collect(context.Background())
	require.NoError(t, err)
	require.Len(t, good.Resources, 1)

	blocked = true
	collecting := make(chan struct{})
	go func() {
		close(collecting)
		_, _ = c.Collect(context.Background())
	}()
	<-collecting

	read := make(chan StorageInventory, 1)
	go func() { read <- c.Latest() }()

	select {
	case inv := <-read:
		assert.Len(t, inv.Resources, 1, "the read path returns last-good while a collection is in flight")
		assert.Equal(t, good.CollectedAt, inv.CollectedAt)
	// 3s is far longer than a mutex read needs and far shorter than the 10s
	// collection timeout, so only a read that waits on the collection fails.
	case <-time.After(3 * time.Second):
		t.Fatal("Latest() blocked behind an in-flight collection")
	}

	close(hold)
}

// TestStorageInventory_ConcurrentCollectionsPublishInOrder covers the ordering
// hazard two overlapping collections would otherwise create: the slower one
// finishing last would publish the older reading and walk CollectedAt backwards,
// so a report could get LESS fresh over time.
func TestStorageInventory_ConcurrentCollectionsPublishInOrder(t *testing.T) {
	c := newTestCollector(t, listerOf(
		streamInfo("LOGS", jetstream.FileStorage, 0, 1),
		streamInfo("METRICS", jetstream.MemoryStorage, 0, 1),
	), resolverFrom(nil))

	const collectors = 8
	stamps := make([]time.Time, collectors)
	var wg sync.WaitGroup
	for i := range collectors {
		wg.Add(1)
		go func() {
			defer wg.Done()
			inv, err := c.Collect(context.Background())
			assert.NoError(t, err)
			stamps[i] = inv.CollectedAt
		}()
	}
	wg.Wait()

	latest := c.Latest()
	require.Len(t, latest.Resources, 2, "concurrent collections must not corrupt the published set")
	for i, stamp := range stamps {
		assert.False(t, latest.CollectedAt.Before(stamp),
			"the published timestamp must not predate collection %d's own result", i)
	}
}

// TestStorageInventory_RunCollectsOnItsInterval proves collection is
// interval-driven and that the first sample does not wait a full interval.
func TestStorageInventory_RunCollectsOnItsInterval(t *testing.T) {
	lister := consistentAccount(streamInfo("LOGS", jetstream.FileStorage, 0, 1))
	c, err := NewStorageInventoryCollector(lister.source(), StorageInventoryConfig{
		OwnerResolver: resolverFrom(nil),
		Interval:      20 * time.Millisecond,
		Timeout:       time.Second,
	})
	require.NoError(t, err)

	ctx, cancel := context.WithCancel(context.Background())
	stopped := make(chan struct{})
	go func() { defer close(stopped); c.Run(ctx) }()

	// 3s budget for at least two 20ms ticks: 150x the nominal wait, so only a
	// loop that never ticks fails.
	require.Eventually(t, func() bool {
		return c.Latest().Resources != nil && lister.calls.Load() >= 2
	}, 3*time.Second, 5*time.Millisecond, "Run must collect on its interval")

	cancel()
	select {
	case <-stopped:
	case <-time.After(3 * time.Second):
		t.Fatal("Run did not return when its context was cancelled")
	}
}

// TestStorageInventory_NamesTheProducingProcess covers the design's "every
// process polling account-wide multiplies cost by deployment size" note: a
// report that does not say who produced it cannot be reconciled across a fleet,
// so the field is never empty.
func TestStorageInventory_NamesTheProducingProcess(t *testing.T) {
	explicit := newTestCollector(t, listerOf(), resolverFrom(nil))
	inv, err := explicit.Collect(context.Background())
	require.NoError(t, err)
	assert.Equal(t, "unit-test", inv.ProducedBy)

	defaulted, err := NewStorageInventoryCollector(listerOf(),
		StorageInventoryConfig{OwnerResolver: resolverFrom(nil)})
	require.NoError(t, err)
	inv, err = defaulted.Collect(context.Background())
	require.NoError(t, err)
	assert.NotEmpty(t, inv.ProducedBy, "an anonymous report cannot be reconciled across a fleet")
}

// TestStorageInventory_ResourcesAreDeterministicallyOrdered keeps the report
// diffable across collections: neither listing's page order is a contract, and
// the reconciliation walks a map.
func TestStorageInventory_ResourcesAreDeterministicallyOrdered(t *testing.T) {
	lister := accountWithUndescribable([]string{"BRAVO"},
		streamInfo("ZULU", jetstream.FileStorage, 0, 1),
		streamInfo(KVStreamPrefix+"ALPHA", jetstream.FileStorage, 0, 1),
		streamInfo("MIKE", jetstream.FileStorage, 0, 1),
	)
	c := newTestCollector(t, lister.source(), resolverFrom(nil))

	want := []string{"BRAVO", KVStreamPrefix + "ALPHA", "MIKE", "ZULU"}
	for range 3 {
		inv, err := c.Collect(context.Background())
		require.NoError(t, err)
		names := make([]string, 0, len(inv.Resources))
		for _, r := range inv.Resources {
			names = append(names, r.Name)
		}
		assert.Equal(t, want, names)
	}
}

// TestStorageInventory_ListerSourceResolvedPerCollection proves the collector
// does not capture a JetStream context at construction: a collector built before
// the client connects must start working once it does.
func TestStorageInventory_ListerSourceResolvedPerCollection(t *testing.T) {
	notReady := errors.New("JetStream not initialized")
	ready := false
	lister := consistentAccount(streamInfo("LOGS", jetstream.FileStorage, 0, 1))
	c := newTestCollector(t, func() (StreamLister, error) {
		if !ready {
			return nil, notReady
		}
		return lister, nil
	}, resolverFrom(nil))

	_, err := c.Collect(context.Background())
	require.ErrorIs(t, err, notReady)
	assert.True(t, c.Latest().Stale)

	ready = true
	inv, err := c.Collect(context.Background())
	require.NoError(t, err)
	assert.Len(t, inv.Resources, 1)
	assert.False(t, inv.Stale)
}

// --- name classification -----------------------------------------------------

// TestClassifyBackingStream pins the physical-name-to-logical-resource mapping
// that the provisioning guard, the inventory's attribution, and the
// undescribable-row builder all read, so they cannot disagree about what a name
// means.
func TestClassifyBackingStream(t *testing.T) {
	tests := []struct {
		stream     string
		wantKind   ResourceKind
		wantBucket string
	}{
		{KVStreamPrefix + "ENTITY_STATES", ResourceKeyValue, "ENTITY_STATES"},
		{KVStreamPrefix + KVStreamPrefix + "FOO", ResourceKeyValue, KVStreamPrefix + "FOO"},
		{ObjectStoreStreamPrefix + "MESSAGES", ResourceObjectStore, "MESSAGES"},
		{ObjectStoreStreamPrefix + ObjectStoreStreamPrefix + "X", ResourceObjectStore, ObjectStoreStreamPrefix + "X"},
		{"LOGS", ResourceOrdinaryStream, ""},
		{"MY_KV_STREAM", ResourceOrdinaryStream, ""},
		{"SOMEOBJ_THING", ResourceOrdinaryStream, ""},
		{"KVSTATES", ResourceOrdinaryStream, ""},
		{"", ResourceOrdinaryStream, ""},
	}
	for _, tt := range tests {
		t.Run(tt.stream, func(t *testing.T) {
			kind, bucket := ClassifyBackingStream(tt.stream)
			assert.Equal(t, tt.wantKind, kind)
			assert.Equal(t, tt.wantBucket, bucket)
		})
	}
}

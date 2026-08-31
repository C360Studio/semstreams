//go:build integration

package config

import (
	"context"
	"encoding/json"
	"errors"
	"regexp"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/nats-io/nats.go/jetstream"
	"github.com/stretchr/testify/require"

	"github.com/c360studio/semstreams/natsclient"
	semtypes "github.com/c360studio/semstreams/pkg/types"
	"github.com/c360studio/semstreams/types"
)

// mustBucket returns a started manager's acquired KV handle.
func mustBucket(t *testing.T, manager *Manager) jetstream.KeyValue {
	t.Helper()
	kv, err := manager.bucket()
	require.NoError(t, err)
	return kv
}

// mustStore returns a started manager's acquired KVStore.
func mustStore(t *testing.T, manager *Manager) *natsclient.KVStore {
	t.Helper()
	kvStore, err := manager.store()
	require.NoError(t, err)
	return kvStore
}

// suffixedID matches an identifier the framework minted: a stem, a separator,
// and exactly six lowercase hex bytes (ADR-104).
var suffixedID = regexp.MustCompile(`^[a-z0-9][a-zA-Z0-9_-]*-[0-9a-f]{6}$`)

func identityTestConfig(org, id string) *Config {
	return &Config{
		Version:    "1.0.0",
		Platform:   PlatformConfig{Org: org, ID: id, Type: "test"},
		Services:   make(types.ServiceConfigs),
		Components: make(ComponentConfigs),
	}
}

func newIdentityManager(t *testing.T, tc *natsclient.TestClient, org, id string) *Manager {
	t.Helper()
	manager, err := NewConfigManager(identityTestConfig(org, id), tc.Client, nil)
	require.NoError(t, err)
	return manager
}

// readIdentityRecord reads the durable identity record straight from KV, the
// way an adopter without Go bindings must (ADR-104 cross-repo contract).
func readIdentityRecord(t *testing.T, ctx context.Context, manager *Manager) platformIdentityRecord {
	t.Helper()
	entry, err := mustStore(t, manager).Get(ctx, platformIdentityKVKey)
	require.NoError(t, err)
	var record platformIdentityRecord
	require.NoError(t, json.Unmarshal(entry.Value, &record))
	return record
}

// directKVStore opens the shared configuration bucket independently of the
// manager's lifecycle. Since the constructor no longer performs I/O (the
// context hard rule), a manager has no bucket until Start acquires one, so a
// test that seeds the bucket BEFORE Start opens it itself.
func directKVStore(t *testing.T, ctx context.Context, manager *Manager) *natsclient.KVStore {
	t.Helper()
	kv, err := manager.natsClient.CreateKeyValueBucket(ctx, jetstream.KeyValueConfig{
		Bucket:      configBucketName,
		Description: "SemStreams runtime configuration",
		History:     5,
	})
	require.NoError(t, err)
	return manager.natsClient.NewKVStore(kv)
}

func seedIdentityRecord(t *testing.T, ctx context.Context, manager *Manager, record platformIdentityRecord) {
	t.Helper()
	data, err := json.Marshal(record)
	require.NoError(t, err)
	_, err = directKVStore(t, ctx, manager).Create(ctx, platformIdentityKVKey, data)
	require.NoError(t, err)
}

// seedDeclaredIdentity records, unsuffixed, the identity a manager's own
// configuration declares. Tests that fabricate an "already configured" bucket
// need it: since ADR-104 a bucket holding configuration and no identity record
// is refused as predating identity minting, which is the whole point of the
// third branch.
func seedDeclaredIdentity(t *testing.T, ctx context.Context, manager *Manager) {
	t.Helper()
	declared := manager.GetConfig().Get().Platform
	seedIdentityRecord(t, ctx, manager, platformIdentityRecord{
		Org: declared.Org, Stem: declared.ID, ID: declared.ID,
	})
}

// TestConfigManagerFirstBootMintsPlatformIdentity pins ADR-104 decisions 1-3 on
// a genuine first boot: the entropy suffix is minted, the record carries
// exactly org/stem/id, the effective configuration adopts the suffixed value,
// the published `platform` mirror carries it too, and the boot is STILL treated
// as a first boot — the identity record is not configuration, so it must not
// flip first-boot detection and skip the initial PushToKV.
func TestConfigManagerFirstBootMintsPlatformIdentity(t *testing.T) {
	tc := natsclient.NewTestClient(t, natsclient.WithJetStream(), natsclient.WithKV())
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	manager := newIdentityManager(t, tc, "acme", "dep")
	require.NoError(t, manager.Start(ctx))
	defer manager.Stop(5 * time.Second)

	record := readIdentityRecord(t, ctx, manager)
	require.Equal(t, "acme", record.Org)
	require.Equal(t, "dep", record.Stem)
	require.Regexp(t, suffixedID, record.ID)
	require.Equal(t, "dep", record.ID[:len("dep")])

	// Exactly three fields — the record shape is a cross-repo contract.
	entry, err := mustStore(t, manager).Get(ctx, platformIdentityKVKey)
	require.NoError(t, err)
	var fields map[string]any
	require.NoError(t, json.Unmarshal(entry.Value, &fields))
	require.ElementsMatch(t, []string{"org", "stem", "id"}, mapKeys(fields))

	require.Equal(t, record.ID, manager.GetConfig().Get().Platform.ID,
		"the effective configuration must carry the minted identifier")

	// First boot still pushed the file configuration, mirror included.
	platformEntry, err := mustStore(t, manager).Get(ctx, "platform")
	require.NoError(t, err, "a genuine first boot must still push the file configuration to KV")
	var mirrored PlatformConfig
	require.NoError(t, json.Unmarshal(platformEntry.Value, &mirrored))
	require.Equal(t, record.ID, mirrored.ID, "the published platform mirror carries the effective identifier")
}

func mapKeys(m map[string]any) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	return keys
}

// TestConfigManagerAdoptsPersistedPlatformIdentity pins the adopt branch: a
// later boot or a co-process takes the recorded identifier when the file
// declares the record's STEM, and refuses otherwise — a different identifier, a
// different organization, or the minted identifier itself (which is not a
// declarable value; see TestFileDeclaringTheMintedIdentifierIsRefusedWithGuidance). The org comparison is the
// reason this mechanism is correct WITHOUT the gh#459 guard, which #1188
// retires.
func TestConfigManagerAdoptsPersistedPlatformIdentity(t *testing.T) {
	cases := []struct {
		name      string
		fileOrg   string
		fileID    string
		wantID    string
		wantError string
	}{
		{name: "file declares the stem", fileOrg: "acme", fileID: "dep", wantID: "dep-7f3a9c"},
		{name: "file declares the minted identifier", fileOrg: "acme", fileID: "dep-7f3a9c", wantError: "declare the stem"},
		{name: "file declares another identifier", fileOrg: "acme", fileID: "other", wantError: "platform identity mismatch"},
		{name: "file declares another organization", fileOrg: "otherorg", fileID: "dep", wantError: "platform identity mismatch"},
	}
	for _, tt := range cases {
		t.Run(tt.name, func(t *testing.T) {
			tc := natsclient.NewTestClient(t, natsclient.WithJetStream(), natsclient.WithKV())
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()

			seeder := newIdentityManager(t, tc, "acme", "dep")
			seedIdentityRecord(t, ctx, seeder, platformIdentityRecord{Org: "acme", Stem: "dep", ID: "dep-7f3a9c"})

			manager := newIdentityManager(t, tc, tt.fileOrg, tt.fileID)
			err := manager.Start(ctx)
			if tt.wantError != "" {
				require.ErrorContains(t, err, tt.wantError)
				require.ErrorContains(t, err, "dep-7f3a9c", "the refusal names the recorded identity")
				return
			}
			require.NoError(t, err)
			defer manager.Stop(5 * time.Second)
			require.Equal(t, tt.wantID, manager.GetConfig().Get().Platform.ID)
			require.Equal(t, "dep-7f3a9c", readIdentityRecord(t, ctx, manager).ID,
				"adopting must not rewrite the record")
		})
	}
}

// TestConfigManagerConcurrentFirstBootConvergesOnOneIdentity pins the atomic
// Create: two processes booting at once against one bucket produce ONE record
// and one identifier. A Put here would give each its own minted value and split
// the deployment's authority permanently, which ADR-102 d7 makes unrepairable.
func TestConfigManagerConcurrentFirstBootConvergesOnOneIdentity(t *testing.T) {
	tc := natsclient.NewTestClient(t, natsclient.WithJetStream(), natsclient.WithKV())
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	managerA := newIdentityManager(t, tc, "acme", "dep")
	managerB := newIdentityManager(t, tc, "acme", "dep")

	var wg sync.WaitGroup
	start := make(chan struct{})
	errs := make([]error, 2)
	for i, manager := range []*Manager{managerA, managerB} {
		wg.Add(1)
		go func(i int, manager *Manager) {
			defer wg.Done()
			<-start
			errs[i] = manager.Start(ctx)
		}(i, manager)
	}
	close(start)
	wg.Wait()
	defer managerA.Stop(5 * time.Second)
	defer managerB.Stop(5 * time.Second)

	require.NoError(t, errs[0])
	require.NoError(t, errs[1])

	idA := managerA.GetConfig().Get().Platform.ID
	idB := managerB.GetConfig().Get().Platform.ID
	require.Equal(t, idA, idB, "two co-processes must converge on one authority")
	require.Equal(t, idA, readIdentityRecord(t, ctx, managerA).ID)

	keys, err := mustStore(t, managerA).Keys(ctx)
	require.NoError(t, err)
	count := 0
	for _, key := range keys {
		if key == platformIdentityKVKey {
			count++
		}
	}
	require.Equal(t, 1, count, "exactly one identity record")
}

// TestFirstBootMintsDistinctSuffixesPerDeployment pins the property the change
// exists for: two deployments booted from byte-identical configuration against
// their own storage do not share an authority.
func TestFirstBootMintsDistinctSuffixesPerDeployment(t *testing.T) {
	boot := func(t *testing.T) string {
		t.Helper()
		tc := natsclient.NewTestClient(t, natsclient.WithJetStream(), natsclient.WithKV())
		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()
		manager := newIdentityManager(t, tc, "acme", "dep")
		require.NoError(t, manager.Start(ctx))
		defer manager.Stop(5 * time.Second)
		return manager.GetConfig().Get().Platform.ID
	}
	first := boot(t)
	second := boot(t)
	require.Regexp(t, suffixedID, first)
	require.Regexp(t, suffixedID, second)
	require.NotEqual(t, first, second, "one template must not mint one authority twice")
}

// TestPreCreatedIdentityRecordIsAdoptedUnsuffixed pins the knobless opt-out
// (owner ruling, 2026-08-30): an operator who owns global uniqueness
// pre-creates the record with id == stem. It is per-deployment by construction,
// so cloning the configuration template cannot clone it.
func TestPreCreatedIdentityRecordIsAdoptedUnsuffixed(t *testing.T) {
	tc := natsclient.NewTestClient(t, natsclient.WithJetStream(), natsclient.WithKV())
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	seeder := newIdentityManager(t, tc, "acme", "field-ops-7")
	seedIdentityRecord(t, ctx, seeder, platformIdentityRecord{Org: "acme", Stem: "field-ops-7", ID: "field-ops-7"})

	manager := newIdentityManager(t, tc, "acme", "field-ops-7")
	require.NoError(t, manager.Start(ctx))
	defer manager.Stop(5 * time.Second)

	require.Equal(t, "field-ops-7", manager.GetConfig().Get().Platform.ID)
	require.Equal(t, "field-ops-7", readIdentityRecord(t, ctx, manager).ID)
}

// TestBootWithOnlyAnIdentityRecordIsStillAFirstBoot pins the second half of
// collision 2: the identity record is not configuration, so a bucket holding
// nothing else is still an empty bucket for first-boot detection. Counting it
// sends the boot down the subsequent-boot branch, where equal versions select
// syncFromKV — which resets the in-memory service map before repopulating it
// from a bucket that has nothing to repopulate it with. The deployment then
// runs with no services AND never publishes its own.
func TestBootWithOnlyAnIdentityRecordIsStillAFirstBoot(t *testing.T) {
	tc := natsclient.NewTestClient(t, natsclient.WithJetStream(), natsclient.WithKV())
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	cfg := identityTestConfig("acme", "dep")
	// Equal file and KV versions are what select syncFromKV on the
	// subsequent-boot branch; getKVVersion reports "0.0.0" for a bucket with no
	// version key.
	cfg.Version = "0.0.0"
	cfg.Services = types.ServiceConfigs{
		"metrics": {Enabled: true, Config: json.RawMessage(`{"port": 9090}`)},
	}
	manager, err := NewConfigManager(cfg, tc.Client, nil)
	require.NoError(t, err)
	seedIdentityRecord(t, ctx, manager, platformIdentityRecord{Org: "acme", Stem: "dep", ID: "dep"})

	require.NoError(t, manager.Start(ctx))
	defer manager.Stop(5 * time.Second)

	require.Contains(t, manager.GetConfig().Get().Services, "metrics",
		"a first boot must keep the services its file declared")
	_, err = mustStore(t, manager).Get(ctx, "services.metrics")
	require.NoError(t, err, "a first boot must publish its file configuration to the bucket")
}

// TestPreIdentityBucketRefusesStartWithoutMinting pins the third branch and the
// fourth collision the architect revision round found: a configuration bucket
// written before identity minting must be refused NAMING THAT CAUSE, and the
// refusal must mint nothing and create nothing. Minting into such a bucket
// would durably record an identifier the deployment's own guard then rejects
// for the wrong reason, permanently — ADR-102 d7 forbids the rewrite that would
// repair it.
func TestPreIdentityBucketRefusesStartWithoutMinting(t *testing.T) {
	tc := natsclient.NewTestClient(t, natsclient.WithJetStream(), natsclient.WithKV())
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	manager := newIdentityManager(t, tc, "acme", "dep")
	putConfigValue(t, ctx, manager, "version", "1.0.0")
	putConfigValue(t, ctx, manager, "platform", PlatformConfig{Org: "acme", ID: "dep", Type: "test"})

	err := manager.Start(ctx)
	require.Error(t, err)
	require.ErrorContains(t, err, "predates framework-minted platform identity")
	require.ErrorContains(t, err, platformIdentityKVKey)
	// The refusal names WHICH keys it found, so an operator can tell the two
	// causes apart — a carried-over bucket from a second writer's fresh one.
	require.ErrorContains(t, err, "platform, version")
	require.ErrorContains(t, err, "processor/rule")

	_, getErr := mustStore(t, manager).Get(ctx, platformIdentityKVKey)
	require.ErrorIs(t, getErr, natsclient.ErrKVKeyNotFound,
		"a refused pre-identity bucket must be left with no identity record")
	require.Equal(t, "dep", manager.GetConfig().Get().Platform.ID,
		"nothing was minted, so the effective identifier is untouched")
}

// TestVersionArbitrationNeverOverwritesPlatformIdentity pins that the record is
// outside the configuration synchronization contract: arbitration neither
// pushes it nor applies it.
func TestVersionArbitrationNeverOverwritesPlatformIdentity(t *testing.T) {
	tc := natsclient.NewTestClient(t, natsclient.WithJetStream(), natsclient.WithKV())
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	first := newIdentityManager(t, tc, "acme", "dep")
	require.NoError(t, first.Start(ctx))
	minted := first.GetConfig().Get().Platform.ID
	require.NoError(t, first.Stop(5*time.Second))

	// A later boot whose file version is newer takes the PushToKV branch.
	later, err := NewConfigManager(&Config{
		Version:    "9.9.9",
		Platform:   PlatformConfig{Org: "acme", ID: "dep", Type: "test"},
		Services:   make(types.ServiceConfigs),
		Components: make(ComponentConfigs),
	}, tc.Client, nil)
	require.NoError(t, err)
	require.NoError(t, later.Start(ctx))
	defer later.Stop(5 * time.Second)

	require.Equal(t, minted, later.GetConfig().Get().Platform.ID)
	require.Equal(t, minted, readIdentityRecord(t, ctx, later).ID)
}

// TestKVPlatformKeyIsAMirrorNotASource pins collision 1: the KV `platform` key
// is published for readers and never applied back over the running authority.
// Before this change updateConfig's `case "platform"` unmarshalled it straight
// over the effective platform block, platform.ID included, so any writer to the
// shared bucket could move the authority every identity is minted under.
func TestKVPlatformKeyIsAMirrorNotASource(t *testing.T) {
	tc := natsclient.NewTestClient(t, natsclient.WithJetStream(), natsclient.WithKV())
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	manager := newIdentityManager(t, tc, "acme", "dep")
	require.NoError(t, manager.Start(ctx))
	defer manager.Stop(5 * time.Second)
	minted := manager.GetConfig().Get().Platform.ID

	updates := manager.OnChange("platform")
	select {
	case <-updates:
	case <-time.After(time.Second):
		t.Fatal("no initial configuration from OnChange")
	}

	putConfigValue(t, ctx, manager, "platform", PlatformConfig{Org: "otherorg", ID: "other", Type: "test"})
	select {
	case <-updates:
	case <-time.After(2 * time.Second):
		t.Fatal("the platform mirror write was never observed")
	}

	require.Equal(t, minted, manager.GetConfig().Get().Platform.ID,
		"a KV platform write must not move the running authority")
	require.Equal(t, "acme", manager.GetConfig().Get().Platform.Org)
}

// TestMaximumDeclarablePairMintsAndStarts is the invariant HIGH-1 refuted: a
// declared pair at exactly the declarable budget must LOAD, MINT, and START.
// The 7-byte reserve is a fact about a DECLARATION, so bounding the effective
// pair — which already carries the suffix — against the declarable budget
// reserves it twice and hard-fails Start for every pair in (156, 163].
func TestMaximumDeclarablePairMintsAndStarts(t *testing.T) {
	tc := natsclient.NewTestClient(t, natsclient.WithJetStream(), natsclient.WithKV())
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	declarable := maxDeclarableAuthorityPairBytes()
	org := strings.Repeat("o", 60)
	id := strings.Repeat("p", declarable-len(org))
	require.Equal(t, declarable, len(org)+len(id), "the fixture must sit exactly on the declarable budget")

	// It loads.
	require.NoError(t, writeAndLoadAuthorityPair(t, org, id))

	// It mints and starts.
	manager := newIdentityManager(t, tc, org, id)
	require.NoError(t, manager.Start(ctx), "a pair at the declarable budget must boot")
	defer manager.Stop(5 * time.Second)

	effective := manager.GetConfig().Get().Platform.ID
	require.Regexp(t, suffixedID, effective)
	require.Equal(t, id, effective[:len(effective)-7], "the declared id must survive as the stem")
	require.LessOrEqual(t, len(org)+len(effective), semtypes.MaxAuthorityPairBytes(),
		"the effective pair must fit the family-table budget")
	require.Equal(t, effective, readIdentityRecord(t, ctx, manager).ID)
}

// preCreateConfigBucket creates `semstreams_config` with an explicit policy
// BEFORE any Manager exists, the way another writer on a shared NATS server
// would — processor/rule's ConfigManager creates the same bucket
// (processor/rule/kv_config_integration.go), and CreateKeyValueBucket returns
// an existing bucket unchanged rather than reconciling its policy.
func preCreateConfigBucket(t *testing.T, ctx context.Context, tc *natsclient.TestClient, cfg jetstream.KeyValueConfig) {
	t.Helper()
	cfg.Bucket = configBucketName
	_, err := tc.Client.CreateKeyValueBucket(ctx, cfg)
	require.NoError(t, err)
}

// TestIdentityUnderAnEvictingBucketNeverRemints is the Codex B1 reproduction.
//
// `platform_identity` is create-once correctness state, but it inherits
// whatever policy the bucket was created with. Codex pre-created
// `semstreams_config` with TTL 250ms, booted, let the record expire, and booted
// again:
//
//	Minted platform identity ... platform=dep-a8fd5f
//	Minted platform identity ... platform=dep-1d0995
//
// Two authorities for one deployment — ADR-102 d7 violated, unrepairably. The
// framework must never mint into a bucket whose policy can delete what it
// minted, so BOTH boots refuse here and no second authority can exist.
func TestIdentityUnderAnEvictingBucketNeverRemints(t *testing.T) {
	tc := natsclient.NewTestClient(t, natsclient.WithJetStream(), natsclient.WithKV())
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	preCreateConfigBucket(t, ctx, tc, jetstream.KeyValueConfig{TTL: 250 * time.Millisecond})

	first := newIdentityManager(t, tc, "acme", "dep")
	firstErr := first.Start(ctx)
	firstAuthority := first.GetConfig().Get().Platform.ID
	if firstErr == nil {
		defer first.Stop(5 * time.Second)
	}

	// Wait for the CONDITION the remint depends on — the record actually gone —
	// rather than for a duration. Post-fix the first boot refuses, so nothing
	// was ever written and this is true immediately; pre-fix it waited out the
	// TTL, exactly as Codex did.
	probe := directKVStore(t, ctx, first)
	require.Eventually(t, func() bool {
		_, err := probe.Get(ctx, platformIdentityKVKey)
		return errors.Is(err, natsclient.ErrKVKeyNotFound)
	}, 10*time.Second, 25*time.Millisecond,
		"the identity record never left the bucket, so this cannot observe a remint")

	second := newIdentityManager(t, tc, "acme", "dep")
	secondErr := second.Start(ctx)
	secondAuthority := second.GetConfig().Get().Platform.ID
	if secondErr == nil {
		defer second.Stop(5 * time.Second)
	}

	// The invariant, stated as the thing that must never happen: one deployment,
	// two authorities. Before the fix both boots succeeded and this compared
	// dep-a8fd5f against dep-1d0995.
	require.Equal(t, firstAuthority, secondAuthority,
		"a deployment must never end up under two authorities (first boot err=%v, second boot err=%v)",
		firstErr, secondErr)
}

// TestEvictingConfigBucketRefusesStart pins the guarantee B1 asks for: a bucket
// whose policy can silently delete keys is refused at acquisition, before
// anything is minted or created, and the refusal names the offending value.
func TestEvictingConfigBucketRefusesStart(t *testing.T) {
	for _, tt := range []struct {
		name   string
		policy jetstream.KeyValueConfig
		names  string
	}{
		{name: "TTL evicts by age", policy: jetstream.KeyValueConfig{TTL: 250 * time.Millisecond}, names: "250ms"},
		{name: "MaxBytes evicts by size", policy: jetstream.KeyValueConfig{MaxBytes: 1024}, names: "1024"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			tc := natsclient.NewTestClient(t, natsclient.WithJetStream(), natsclient.WithKV())
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()

			preCreateConfigBucket(t, ctx, tc, tt.policy)

			manager := newIdentityManager(t, tc, "acme", "dep")
			err := manager.Start(ctx)
			require.Error(t, err, "a bucket that can evict the identity record must not be minted into")
			require.ErrorContains(t, err, configBucketName)
			require.ErrorContains(t, err, tt.names, "the refusal must name the offending policy value")

			// Nothing was minted and nothing was created.
			require.Equal(t, "dep", manager.GetConfig().Get().Platform.ID)
			bucket, bErr := tc.Client.GetKeyValueBucket(ctx, configBucketName)
			require.NoError(t, bErr)
			_, gErr := bucket.Get(ctx, platformIdentityKVKey)
			require.Error(t, gErr, "a refused acquisition must create no identity record")
		})
	}
}

// TestConcurrentFirstBootRefusesASecondEnvironment is the Codex B2
// reproduction. Two managers with the same org and stem but DIFFERENT
// `environment` values, released together against an empty bucket: the winner
// Creates the identity record and the loser adopts it through ErrKeyExists —
// but both then took Start's first-boot branch, where the (org, id, environment)
// guard does not run, and both published their configuration over each other's.
// Codex reproduced two nil errors and two "First boot detected" lines 10/10.
func TestConcurrentFirstBootRefusesASecondEnvironment(t *testing.T) {
	tc := natsclient.NewTestClient(t, natsclient.WithJetStream(), natsclient.WithKV())
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	prodCfg := identityTestConfig("acme", "dep")
	prodCfg.Platform.Environment = "prod"
	devCfg := identityTestConfig("acme", "dep")
	devCfg.Platform.Environment = "dev"

	prod, err := NewConfigManager(prodCfg, tc.Client, nil)
	require.NoError(t, err)
	dev, err := NewConfigManager(devCfg, tc.Client, nil)
	require.NoError(t, err)

	var wg sync.WaitGroup
	start := make(chan struct{})
	errs := make([]error, 2)
	for i, manager := range []*Manager{prod, dev} {
		wg.Add(1)
		go func(i int, manager *Manager) {
			defer wg.Done()
			<-start
			errs[i] = manager.Start(ctx)
		}(i, manager)
	}
	close(start)
	wg.Wait()
	defer prod.Stop(5 * time.Second)
	defer dev.Stop(5 * time.Second)

	succeeded := 0
	var refusal error
	for _, err := range errs {
		if err == nil {
			succeeded++
			continue
		}
		refusal = err
	}
	require.Equal(t, 1, succeeded,
		"at most one environment may establish against one configuration bucket; prod=%v dev=%v", errs[0], errs[1])
	require.ErrorContains(t, refusal, "prod")
	require.ErrorContains(t, refusal, "dev")
}

// TestFileDeclaringTheMintedIdentifierIsRefusedWithGuidance is the Codex B3
// reproduction, from the other side of the contradiction it names.
//
// The adopt arm accepted a file whose platform.id equalled the record's stem OR
// its full identifier. But the load boundary treats every configured value as a
// STEM and reserves seven bytes for the suffix, so at the legal boundary — a
// 163-byte stem minting to a 170-byte identifier — putting that identifier in
// the file is rejected at load and never reaches adopt. One field, two admitted
// kinds, and the ADR's "no path sees both kinds" claim false.
//
// Resolved by making configuration always declare the stem. The refusal stays
// observation-based: it compares against the STORED identifier, never detects a
// minted value by grammar, and tells the operator what to write instead.
func TestFileDeclaringTheMintedIdentifierIsRefusedWithGuidance(t *testing.T) {
	tc := natsclient.NewTestClient(t, natsclient.WithJetStream(), natsclient.WithKV())
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	seeder := newIdentityManager(t, tc, "acme", "dep")
	require.NoError(t, seeder.Start(ctx))
	minted := seeder.GetConfig().Get().Platform.ID
	require.NoError(t, seeder.Stop(5*time.Second))

	manager := newIdentityManager(t, tc, "acme", minted)
	err := manager.Start(ctx)
	require.Error(t, err, "configuration declares the stem; the minted identifier is not a declarable value")
	require.ErrorContains(t, err, "declare the stem")
	require.ErrorContains(t, err, "dep")
	require.ErrorContains(t, err, minted)
}

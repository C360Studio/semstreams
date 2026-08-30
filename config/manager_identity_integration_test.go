//go:build integration

package config

import (
	"context"
	"encoding/json"
	"regexp"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/c360studio/semstreams/natsclient"
	"github.com/c360studio/semstreams/types"
)

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
	entry, err := manager.kvStore.Get(ctx, platformIdentityKVKey)
	require.NoError(t, err)
	var record platformIdentityRecord
	require.NoError(t, json.Unmarshal(entry.Value, &record))
	return record
}

func seedIdentityRecord(t *testing.T, ctx context.Context, manager *Manager, record platformIdentityRecord) {
	t.Helper()
	data, err := json.Marshal(record)
	require.NoError(t, err)
	_, err = manager.kvStore.Create(ctx, platformIdentityKVKey, data)
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
	entry, err := manager.kvStore.Get(ctx, platformIdentityKVKey)
	require.NoError(t, err)
	var fields map[string]any
	require.NoError(t, json.Unmarshal(entry.Value, &fields))
	require.ElementsMatch(t, []string{"org", "stem", "id"}, mapKeys(fields))

	require.Equal(t, record.ID, manager.GetConfig().Get().Platform.ID,
		"the effective configuration must carry the minted identifier")

	// First boot still pushed the file configuration, mirror included.
	platformEntry, err := manager.kvStore.Get(ctx, "platform")
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
// later boot or a co-process takes the recorded identifier, whether the file
// declares the stem or the full identifier, and refuses when the file declares
// a different identifier or a different organization. The org comparison is the
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
		{name: "file declares the minted identifier", fileOrg: "acme", fileID: "dep-7f3a9c", wantID: "dep-7f3a9c"},
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

	keys, err := managerA.kvStore.Keys(ctx)
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
	_, err = manager.kvStore.Get(ctx, "services.metrics")
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
	require.ErrorContains(t, err, "predates")
	require.ErrorContains(t, err, platformIdentityKVKey)

	_, getErr := manager.kvStore.Get(ctx, platformIdentityKVKey)
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

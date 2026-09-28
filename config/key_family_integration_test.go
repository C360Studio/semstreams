//go:build integration

package config

import (
	"context"
	"errors"
	"log/slog"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/c360studio/semstreams/natsclient"
	"github.com/c360studio/semstreams/pkg/errs"
)

// skippedKeyWarnings records the "key" attribute of every Warn a Manager logs
// about a key family key it skips. Manager goroutines log concurrently, so the
// record is guarded.
type skippedKeyWarnings struct {
	mu   sync.Mutex
	keys []string
}

func (w *skippedKeyWarnings) Enabled(context.Context, slog.Level) bool { return true }
func (w *skippedKeyWarnings) WithAttrs([]slog.Attr) slog.Handler       { return w }
func (w *skippedKeyWarnings) WithGroup(string) slog.Handler            { return w }
func (w *skippedKeyWarnings) Handle(_ context.Context, record slog.Record) error {
	if record.Level != slog.LevelWarn || record.Message != "Configuration key under a key family is not a member; skipped" {
		return nil
	}
	record.Attrs(func(attr slog.Attr) bool {
		if attr.Key == "key" {
			w.mu.Lock()
			w.keys = append(w.keys, attr.Value.String())
			w.mu.Unlock()
		}
		return true
	})
	return nil
}

func (w *skippedKeyWarnings) snapshot() []string {
	w.mu.Lock()
	defer w.mu.Unlock()
	return append([]string(nil), w.keys...)
}

// collectingFamily returns a family whose handler forwards every entry to a
// buffered channel, so a test synchronizes on delivery rather than sleeping.
func collectingFamily(t *testing.T, prefix string) (*KeyFamily, <-chan KeyFamilyEntry) {
	t.Helper()
	entries := make(chan KeyFamilyEntry, 16)
	family, err := NewKeyFamily(prefix, func(entry KeyFamilyEntry) { entries <- entry })
	require.NoError(t, err)
	return family, entries
}

func nextEntry(t *testing.T, entries <-chan KeyFamilyEntry) KeyFamilyEntry {
	t.Helper()
	select {
	case entry := <-entries:
		return entry
	case <-time.After(5 * time.Second):
		t.Fatal("no key family entry delivered within 5s")
		return KeyFamilyEntry{}
	}
}

// spec: component-runtime-config / Config Manager delivers a registered key family to its owner
func TestKeyFamilyDeliversSnapshotThenChanges(t *testing.T) {
	tc := natsclient.NewTestClient(t, natsclient.WithJetStream(), natsclient.WithKV())
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	// Establish the deployment first: a bucket holding rules.* keys and no
	// identity record is a pre-identity bucket and is refused.
	first := newIdentityManager(t, tc, "acme", "dep")
	require.NoError(t, first.Start(ctx))
	store := mustStore(t, first)
	_, err := store.Put(ctx, "rules.a", []byte(`"a1"`))
	require.NoError(t, err)
	require.NoError(t, first.Stop(5*time.Second))

	family, entries := collectingFamily(t, "rules")
	require.ErrorIs(t, family.Put(ctx, "early", []byte(`"x"`)), errFamilyNotRegistered,
		"a family must refuse writes before the manager that registered it has started")

	warnings := &skippedKeyWarnings{}
	manager, err := NewConfigManager(identityTestConfig("acme", "dep"), tc.Client, slog.New(warnings), WithKeyFamily(family))
	require.NoError(t, err)
	require.NoError(t, manager.Start(ctx))
	defer func() { _ = manager.Stop(5 * time.Second) }()

	snapshot := nextEntry(t, entries)
	require.Equal(t, KeyFamilyEntry{Name: "a", Value: []byte(`"a1"`), Operation: KeyFamilyPut, Initial: true}, snapshot)

	// A key outside the family must never be delivered: it is written first,
	// so if the family leaked it, it would arrive before rules.b.
	_, err = store.Put(ctx, "other.a", []byte(`"no"`))
	require.NoError(t, err)
	require.NoError(t, family.Put(ctx, "b", []byte(`"b1"`)))
	require.NoError(t, family.Delete(ctx, "a"))

	require.Equal(t, KeyFamilyEntry{Name: "b", Value: []byte(`"b1"`), Operation: KeyFamilyPut}, nextEntry(t, entries))
	deleted := nextEntry(t, entries)
	require.Equal(t, "a", deleted.Name)
	require.Equal(t, KeyFamilyDelete, deleted.Operation)
	require.False(t, deleted.Initial)
	require.Empty(t, deleted.Value)

	names, err := family.Names(ctx)
	require.NoError(t, err)
	require.Equal(t, []string{"b"}, names)
	value, err := family.Get(ctx, "b")
	require.NoError(t, err)
	require.Equal(t, []byte(`"b1"`), value)
	require.ErrorIs(t, family.Create(ctx, "b", []byte(`"b2"`)), natsclient.ErrKVKeyExists)

	// A nested name is not a member: the watch "rules.*" would never deliver
	// it. The family refuses it before any I/O, and a nested key another writer
	// stored is neither listed, read, nor deleted through the family.
	require.True(t, errs.IsInvalid(family.Put(ctx, "dotted.name", []byte(`"d"`))))
	require.True(t, errs.IsInvalid(family.Create(ctx, "dotted.name", []byte(`"d"`))))
	_, err = store.Get(ctx, "rules.dotted.name")
	require.ErrorIs(t, err, natsclient.ErrKVKeyNotFound, "a refused write must not reach the bucket")
	_, err = store.Put(ctx, "rules.nested.name", []byte(`"n"`))
	require.NoError(t, err)
	_, err = family.Get(ctx, "nested.name")
	require.True(t, errs.IsInvalid(err))
	require.True(t, errs.IsInvalid(family.Delete(ctx, "nested.name")))
	_, err = store.Get(ctx, "rules.nested.name")
	require.NoError(t, err, "a refused delete must not reach the bucket")
	// The nested put was written before rules.c, so had the watch delivered
	// it, it would arrive first.
	require.NoError(t, family.Put(ctx, "c", []byte(`"c1"`)))
	require.Equal(t, KeyFamilyEntry{Name: "c", Value: []byte(`"c1"`), Operation: KeyFamilyPut}, nextEntry(t, entries))
	names, err = family.Names(ctx)
	require.NoError(t, err)
	require.ElementsMatch(t, []string{"b", "c"}, names, "a nested key is not a family member")
	// The skip is declared: listing names the nested key it leaves out, once
	// per listing, and nothing else.
	require.Equal(t, []string{"rules.nested.name"}, warnings.snapshot(),
		"Names must warn about the nested key it skips")

	// A refused Start binds nothing: a manager whose bucket records a foreign
	// identity leaves its family unable to write, exactly like its own writers.
	refusedFamily, _ := collectingFamily(t, "rules")
	refused, err := NewConfigManager(identityTestConfig("acme", "other"), tc.Client, nil, WithKeyFamily(refusedFamily))
	require.NoError(t, err)
	seedIdentityRecord(t, ctx, refused, platformIdentityRecord{Org: "acme", Stem: "dep", ID: "dep-0a1b2c"})
	startErr := refused.Start(ctx)
	require.Error(t, startErr)
	require.True(t, errors.Is(refusedFamily.Put(ctx, "c", []byte(`"c"`)), errFamilyNotRegistered),
		"a refused Start must leave the family disarmed")
}

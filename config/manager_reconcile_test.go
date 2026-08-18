package config

import (
	"encoding/json"
	"io"
	"log/slog"
	"testing"

	"github.com/c360studio/semstreams/types"
)

func classifierManager() *Manager {
	return &Manager{
		config: NewSafeConfig(&Config{
			Platform: PlatformConfig{Org: "test", ID: "test"}, Components: make(ComponentConfigs),
		}),
		pendingLocal: make(map[string]pendingLocalWrite),
		logger:       slog.New(slog.NewTextHandler(io.Discard, nil)),
	}
}

func encodedComponent(t *testing.T, name string) []byte {
	t.Helper()
	data, err := json.Marshal(types.ComponentConfig{Name: name, Type: types.ComponentTypeProcessor})
	if err != nil {
		t.Fatal(err)
	}
	return data
}

func TestPendingLocalClassificationIsPerKey(t *testing.T) {
	manager := classifierManager()
	manager.pendingLocal["components.local"] = pendingLocalWrite{revision: 11}
	manager.handleUpdate("components.external", encodedComponent(t, "external"), 10)
	if got := manager.config.Get().Components["external"].Name; got != "external" {
		t.Fatalf("mixed-key external revision was hidden: %q", got)
	}
	if _, ok := manager.pendingLocal["components.local"]; !ok {
		t.Fatal("unrelated external write cleared local pending entry")
	}
}

func TestPendingPutSkipsSupersededAndExactEchoThenAppliesLaterExternal(t *testing.T) {
	manager := classifierManager()
	key := "components.worker"
	manager.pendingLocal[key] = pendingLocalWrite{revision: 11}
	manager.handleUpdate(key, encodedComponent(t, "stale"), 10)
	if _, ok := manager.pendingLocal[key]; !ok {
		t.Fatal("superseded entry cleared pending write")
	}
	manager.handleUpdate(key, encodedComponent(t, "local"), 11)
	if _, ok := manager.pendingLocal[key]; ok {
		t.Fatal("exact local echo did not clear pending write")
	}
	manager.handleUpdate(key, encodedComponent(t, "external"), 12)
	if got := manager.config.Get().Components["worker"].Name; got != "external" {
		t.Fatalf("later external write not applied: %q", got)
	}
}

func TestPendingDeleteSkipsEarlierPutUntilTombstoneThenAllowsRecreate(t *testing.T) {
	manager := classifierManager()
	key := "components.worker"
	manager.pendingLocal[key] = pendingLocalWrite{delete: true}
	manager.handleUpdate(key, encodedComponent(t, "stale"), 20)
	if _, ok := manager.config.Get().Components["worker"]; ok {
		t.Fatal("put ordered before local tombstone was applied")
	}
	manager.handleUpdate(key, nil, 21)
	if _, ok := manager.pendingLocal[key]; ok {
		t.Fatal("ordered tombstone did not clear pending delete")
	}
	manager.handleUpdate(key, encodedComponent(t, "recreated"), 22)
	if got := manager.config.Get().Components["worker"].Name; got != "recreated" {
		t.Fatalf("post-tombstone recreate not applied: %q", got)
	}
}

func TestPendingMapEmptiesAfterChurnConverges(t *testing.T) {
	manager := classifierManager()
	for revision := uint64(1); revision <= 100; revision++ {
		key := "components.worker"
		manager.pendingLocal[key] = pendingLocalWrite{revision: revision}
		manager.handleUpdate(key, encodedComponent(t, "local"), revision)
	}
	if len(manager.pendingLocal) != 0 {
		t.Fatalf("pending writes after convergence = %#v", manager.pendingLocal)
	}
}
